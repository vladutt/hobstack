# Project memory Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** O memorie per proiect (cheie, rezumat, text, autor, data, blocare, istoric) pe care agentii o scriu prin MCP si o vad ca index in fiecare tura, plus tab-ul Memory in UI.

**Architecture:** Tabelele `project_memory` + `project_memory_versions` (migrarea 043) in internal/store; tool-urile MCP `remember` / `recall` / `search_memory` / `forget` declarate in internal/mcp si implementate in internal/team/memory.go; indexul construit de `prompt.MemoryIndex` si adaugat in tura in `team.startRunOpts` (langa liniile Library, nu in system prompt); regula `howMemory` in system prompt; API REST in internal/web/api_memory.go si tab-ul Memory (static/js/memory.js).

**Tech Stack:** Go (stdlib, modernc sqlite), JS vanilla.

**Spec:** docs/superpowers/specs/2026-11-27-hobstack-project-memory-design.md

## Global Constraints

- Cheia: `^[a-z0-9][a-z0-9._-]{0,59}$` dupa normalizare (TrimSpace, lowercase, spatii -> `-`).
- summary obligatoriu, un rand, max 140 caractere (runes); body max 4000; max 200 intrari active per proiect.
- Indexul: max 40 randuri si max 3000 caractere; blocate primele, apoi updated_at descrescator; gol = fara bloc.
- Nimic din memorie nu intra in system prompt (doar regula statica howMemory).
- Tool-urile lipsesc in run-urile de draft, wizard si brainstorming; indexul intra si in brainstorming (fara mentiune de recall).
- UI, erori si prompturi in engleza; comentariile in romana.
- `go vet ./...`, `go build -race ./...`, `GOOS=windows go build ./cmd/hobstack`, `GOOS=darwin GOARCH=arm64 go build ./cmd/hobstack`; JS verificat ca .mjs cu `node --check`.
- Fara commit / push fara acordul utilizatorului; push doar `git push hobstack`.

## Review Focus

1. Un agent scrie pe o cheie blocata (inclusiv prin remember pe o cheie stearsa care fusese blocata) — trebuie refuz doar cat e activa si blocata; stergerea scoate blocarea.
2. Doi agenti scriu aceeasi cheie aproape simultan — un singur rand (UPSERT pe UNIQUE), doua versiuni, fara eroare de constrangere.
3. Index cu summary-uri lungi / caractere multi-byte — limita de 3000 se masoara in runes si "N more" e corect.
4. Agent sters dupa ce a scris — author_name ramane, UI-ul nu crapa pe author_agent_id inexistent.
5. Brainstorming: indexul apare, dar tool-urile nu (allowed() refuza remember pe BrainstormCardID).

---

### Task 1: Store — tabele si operatii

**Files:**
- Create: `internal/store/migrations/043_project_memory.sql`, `internal/store/memory.go`, `internal/store/memory_test.go`
- Modify: `internal/store/store_test.go` (user_version 43)

**Interfaces:**
- Produces:
  ```go
  type Memory struct {
      ID, ProjectID int64; Key, Summary, Body string; Locked bool
      AuthorAgentID int64 /*0 = user*/; AuthorName string; TaskID int64
      TaskRef string // "T-212" din JOIN pe tasks (gol fara task)
      CreatedAt, UpdatedAt time.Time; DeletedAt *time.Time
  }   // json: id, projectId, key, summary, body, locked, authorAgentId, authorName, taskId, taskRef, createdAt, updatedAt, deletedAt
  type MemoryVersion struct {
      ID, MemoryID int64; Action, Summary, Body, AuthorName, Reason string
      AuthorAgentID, TaskID int64; At time.Time
  }
  type MemoryWrite struct { Key, Summary, Body string; AuthorAgentID int64; AuthorName string; TaskID int64 }
  const MaxMemoryEntries = 200
  var ErrMemoryLocked, ErrMemoryFull, ErrMemoryExists error
  func NormalizeMemoryKey(s string) (string, error)
  func FoldText(s string) string // lowercase + fara diacritice (tabel manual, fara dependente noi)
  func (s *Store) SetMemory(pid int64, w MemoryWrite, allowLocked bool) (m Memory, prev *Memory, changed bool, err error)
  func (s *Store) CreateMemory(pid int64, w MemoryWrite, locked bool) (Memory, error) // utilizatorul: ErrMemoryExists pe cheie activa
  func (s *Store) GetMemoryByKey(pid int64, key string) (Memory, error)               // si sterse; ErrNotFound
  func (s *Store) GetMemory(id int64) (Memory, error)
  func (s *Store) ListMemory(pid int64, deleted bool) ([]Memory, error)               // blocate, apoi updated_at desc
  func (s *Store) SearchMemory(pid int64, query string, limit int) ([]Memory, error)  // doar active
  func (s *Store) ForgetMemory(id int64, by MemoryWrite, reason string, allowLocked bool) error
  func (s *Store) RestoreMemory(id int64, by MemoryWrite) error
  func (s *Store) LockMemory(id int64, locked bool) error
  func (s *Store) MemoryHistory(id int64) ([]MemoryVersion, error)                    // cele noi primele
  ```

- [ ] **Step 1: Write the failing tests** in `memory_test.go`:
  - `TestNormalizeMemoryKey`: `" API Auth "` -> `"api-auth"`; `""`, `"-x"`, 61 de caractere, `"a/b"` -> eroare.
  - `TestSetMemoryUpsertAndVersions`: SetMemory de doua ori pe `ui.dates` -> acelasi ID, `prev.Summary` = primul, 2 versiuni `set`; a treia oara identic -> `changed == false`, tot 2 versiuni.
  - `TestSetMemoryLocked`: dupa LockMemory, SetMemory(allowLocked=false) -> `ErrMemoryLocked`; ForgetMemory(allowLocked=false) -> `ErrMemoryLocked`; cu allowLocked=true trece.
  - `TestForgetClearsLockAndRevive`: blocata, ForgetMemory(allowLocked=true, reason "old") -> DeletedAt != nil, Locked false; SetMemory de agent -> acelasi ID, DeletedAt nil, neblocata; history: set, lock, forget (reason "old"), set.
  - `TestMemoryLimit`: 200 de chei, a 201-a -> `ErrMemoryFull`; actualizarea uneia existente trece.
  - `TestCreateMemoryExists`: CreateMemory pe cheie activa -> `ErrMemoryExists`; pe cheie stearsa -> reinviere.
  - `TestListMemoryTaskRef`: o intrare cu TaskID are TaskRef "T-<number>".
  - `TestSearchMemory`: cauta in key/summary/body fara diacritice si litere mari ("Daté" gaseste "date"), exclude sterse, ordinea ca la List, respecta limit.
- [ ] **Step 2: Run** `go test ./internal/store -run Memory` — Expected: FAIL (tipuri nedefinite).
- [ ] **Step 3: Implement** migrarea (coloanele din spec; UNIQUE(project_id, key); index pe project_memory_versions(memory_id)) si `memory.go`. SetMemory intr-o tranzactie: citeste randul (si sters), verifica lock (doar pe rand activ), limita (doar la insert / reinviere), UPSERT, versiunea `set`. SearchMemory filtreaza in Go randurile active ale proiectului (max 200) cu FoldText. FoldText: lowercase + tabel manual pentru diacriticele romanesti si latine uzuale (ă â î ș ş ț ţ é è ê ë á à ä ö ü ó ñ ç), fara dependente noi. Actualizeaza `store_test.go` la 43.
- [ ] **Step 4: Run** `go test ./internal/store` — Expected: PASS.
- [ ] **Step 5: Commit** `git add internal/store && git commit -m "Project memory: store and migration 043"`

### Task 2: Prompt — indexul si regula

**Files:**
- Create: `internal/prompt/memory.go`, `internal/prompt/memory_test.go`
- Modify: `internal/prompt/prompt.go` (howMemory in `how`, inainte de howVisuals)

**Interfaces:**
- Consumes: `store.Memory`
- Produces: `func MemoryIndex(entries []store.Memory, withTools bool) string`; `const howMemory` (textul exact din spec, sectiunea Prompt).

- [ ] **Step 1: Write the failing tests**:
  - `TestMemoryIndexEmpty`: nil -> `""`.
  - `TestMemoryIndexLines`: randul `- ui.dates: show dates as DD.MM.YYYY (Backend Bob, T-212)` (T-N din `TaskRef`); blocata `(locked, You)`; antetul cu tools: `Project memory (decisions and facts saved by the team; read the full text with mcp__team__recall):`; fara tools: `Project memory (decisions and facts saved by the team):`.
  - `TestMemoryIndexLimits`: 50 de intrari -> 40 de randuri + `- ... 10 more: search with mcp__team__search_memory`; summary-uri de 140 runes multi-byte -> totalul <= 3000 runes si "N more" numara corect; fara tools linia finala e `- ... N more`.
  - `TestSystemHasMemoryRule`: System(...) pentru un worker, un lider si o radacina contine "Project memory holds decisions".
- [ ] **Step 2: Run** `go test ./internal/prompt -run Memory` — Expected: FAIL.
- [ ] **Step 3: Implement** (ordinea e cea primita; apelantul da lista din ListMemory).
- [ ] **Step 4: Run** `go test ./internal/prompt` — Expected: PASS (testele existente care compara system prompt-ul exact se actualizeaza daca e cazul).
- [ ] **Step 5: Commit**.

### Task 3: Team — tool-urile si indexul in tura

**Files:**
- Create: `internal/team/memory.go`, `internal/team/memory_test.go`
- Modify: `internal/team/run.go` (dupa blocul Library), `internal/mcp/server.go` (Backend + dispatch), `internal/mcp/tools.go` (memoryTools in toolsFor), fake-urile din `internal/mcp/server_test.go`

**Interfaces:**
- Consumes: Task 1, `prompt.MemoryIndex`
- Produces (Backend):
  ```go
  Remember(c Caller, key, summary, text, task string) (string, error)
  Recall(c Caller, key string) (string, error)
  SearchMemory(c Caller, query string) (string, error)
  Forget(c Caller, key, reason string) (string, error)
  ```
  Tool-uri: `remember` (required key, summary; optional text, task), `recall` (key), `search_memory` (query), `forget` (key, reason). Adaugate in toolsFor pentru orice caller care ajunge la `out = append(out, askUserTool, ...)` (deci nu brainstorm / draft / setup).

- [ ] **Step 1: Write the failing tests** (`memory_test.go`, cu fake-ul existent din team):
  - `TestRememberDefaultsToRunTask`: Caller cu TaskID -> Memory.TaskID = task-ul; task_event `memory` cu textul `<agent> saved memory ui.dates: ...`; eveniment SSE `memory`.
  - `TestRememberExplicitTask`: task "T-2" -> TaskID-ul lui T-2; "T-99" -> eroarea lui findTask.
  - `TestRememberLockedRefused`: mesajul `ui.dates is locked by the user. If it is wrong, tell the user (ask_user or your report).`
  - `TestRememberRevivesUserDeleted`: sters de utilizator cu motiv -> raspunsul contine `was deleted by You on` si motivul.
  - `TestRememberUpdateReply`: a doua scriere -> `Updated ui.dates (was: <vechi>)`; identic -> `ui.dates is already saved with this text.`
  - `TestRecall`: activa -> summary, text, autor, data, T-N; stearsa -> `was forgotten by <autor> on <data>: <motiv>`; necunoscuta -> `No memory ui.date. Closest keys: ui.dates`.
  - `TestSearchMemoryTool`, `TestForgetTool` (si refuzul pe blocata).
  - `TestRunTurnHasMemoryIndex`: un run pe task si unul de chat au in tura blocul "Project memory"; un run de brainstorming il are fara "recall"; proiect fara memorie -> fara bloc.
  - in `internal/mcp`: `TestToolsForMemory`: worker / lider / chat au cele 4; BrainstormCardID / DraftID / SetupID nu.
- [ ] **Step 2: Run** `go test ./internal/team ./internal/mcp -run Memory` — Expected: FAIL.
- [ ] **Step 3: Implement**. Scrierile sub `t.work.Lock()` (ca CommentTask). Agentul = `t.st.GetAgent(c.AgentID)` pentru AuthorName. Erorile store (Full, chei invalide) -> mesaje pentru agent. In run.go: `ListMemory(p.ID, false)` -> `prompt.MemoryIndex(list, card == nil)`; o eroare se logheaza, run-ul merge.
- [ ] **Step 4: Run** `go test ./internal/team ./internal/mcp` — Expected: PASS.
- [ ] **Step 5: Commit**.

### Task 4: Web API

**Files:**
- Create: `internal/web/api_memory.go`, `internal/web/memory_test.go`
- Modify: `internal/web/server.go` (rute)

**Interfaces:**
- Consumes: Task 1.
- Produces: rutele din spec (GET/POST `/api/projects/{id}/memory[?deleted=1]`, PUT/DELETE `/api/memory/{id}`, PUT `/api/memory/{id}/lock`, POST `/api/memory/{id}/restore`, GET `/api/memory/{id}/history`). Autorul utilizatorului: AuthorAgentID 0, AuthorName `store.UserFileAuthor` ("You"). Fiecare schimbare: `s.hub.Publish(pid, "memory", map[string]int64{"id": id})`.

- [ ] **Step 1: Write the failing test** `TestMemoryAPI`: POST creeaza (201 + JSON), POST pe aceeasi cheie -> 409, PUT editeaza o blocata (utilizatorul poate), lock, DELETE, GET ?deleted=1 o arata, restore, history are actiunile in ordine inversa; cheie invalida -> 400; summary gol -> 400.
- [ ] **Step 2: Run** `go test ./internal/web -run Memory` — Expected: FAIL.
- [ ] **Step 3: Implement** (pathID / decode / fail / writeErr ca in api_library_always.go; ErrMemoryExists -> 409, erori de validare -> 400).
- [ ] **Step 4: Run** `go test ./internal/web` — Expected: PASS.
- [ ] **Step 5: Commit**.

### Task 5: UI — tab-ul Memory

**Files:**
- Create: `internal/web/static/js/memory.js`
- Modify: `internal/web/static/js/workspace.js` (TABS: `{ id: "memory", label: "Memory" }` intre files si brainstorm; ruta `#/p/<pid>/memory/<id>`; evenimentul SSE `memory` -> `memory.refresh()`), `internal/web/static/app.css`

**Interfaces:**
- Produces: `memoryView(projectId, opts) -> { el, load(), open(id), refresh(), destroy() }` (ca statsView / brainstormView).

- [ ] **Step 1: Implement** lista (cautare fara diacritice ca taskSearch, rand: cheie mono, summary, autor cu ui.toneOf / "You", timp relativ, T-N -> modala task-ului, lacat), "+ Add" (key, summary, text, Lock), intrarea in modala (markdown prin md.js, Edit, Lock / Unlock, Delete cu toast Undo = restore, History), filtrul "Deleted (N)" cu Restore, starea goala: "Decisions and facts your agents must remember. Agents add them as they work; you can add, edit, lock or delete them here."
- [ ] **Step 2: Verify** `node --check` pe copiile .mjs ale memory.js si workspace.js — Expected: fara erori.
- [ ] **Step 3: Verify in browser** pe instanta de test (port 7790, Chrome headless 9333): screenshot-uri pentru lista goala, lista cu 3 intrari (una blocata, una cu T-N), modala cu History, Deleted (N); clic prin Add -> Edit -> Lock -> Delete -> Undo.
- [ ] **Step 4: Commit**.

### Task 6: Verificare cap-coada si documentatie

**Files:**
- Modify: `CLAUDE.md` (paragraful "Gata (memoria proiectului, spec in docs/superpowers/specs/2026-11-27-hobstack-project-memory-design.md)")

- [ ] **Step 1: Run** `go test ./... && go vet ./... && go build -race ./... && GOOS=windows go build ./cmd/hobstack && GOOS=darwin GOARCH=arm64 go build ./cmd/hobstack` — Expected: totul trece.
- [ ] **Step 2: Run real** pe instanta de test: un proiect cu un lider si un worker; liderul face send_back cu o regula generala ("dates must be DD.MM.YYYY everywhere"); se verifica in Memory ca un agent a salvat-o; New chat pe worker + un task nou care afiseaza o data -> tura contine indexul si rezultatul respecta regula. Costul se noteaza.
- [ ] **Step 3: Update** CLAUDE.md, apoi commit.
