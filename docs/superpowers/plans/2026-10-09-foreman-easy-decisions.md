# Decizii usor de dat pe task: plan de implementare

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** La un task care asteapta decizia utilizatorului: intrebari cu optiuni (radio/checkbox + Other), pasii lui ca checklist si Accept / Send back cu motive rapide si un camp de nota; raspunsurile ajung la agent.

**Architecture:** `report_done` primeste `decisions` si `user_steps`. Intrebarile stau in tabelul nou `task_decisions`; pasii refolosesc `task_todos` cu o coloana `kind`. `POST /api/tasks/{id}/decide` primeste `answers`; team salveaza raspunsurile, construieste textul lor si il pune in nota catre agent (Accept) sau in feedback (Send back). UI: sectiunea "Decide" si zona de actiune noua in modala.

**Tech Stack:** Go, SQLite (modernc), JS vanilla.

**Spec:** `docs/superpowers/specs/2026-10-09-foreman-easy-decisions-design.md`

## Global Constraints

- UI, mesaje, erori si textele pentru agenti in engleza; comentariile din cod in romana.
- `go vet ./...`, `go build -race ./...`, `go test ./...` trec; cross-compile `GOOS=darwin GOARCH=arm64 go build ./cmd/foreman` si `GOOS=windows go build ./cmd/foreman`; JS cu `node --check` pe o copie `.mjs`.
- Limite: `decisions` max 5, intrebare 1-300 caractere, optiuni 2-5 a cate 1-120, `multi` implicit false; `user_steps` max 10, fiecare 1-300.
- Migrarea noua = ultima de pe main + 1 (azi 030); se verifica din nou la merge (alte sesiuni adauga migrari in paralel).
- Tot textul in UI prin `el()` (fara innerHTML).
- Verificarea vizuala doar pe un HOME gol cu `claude` fals in PATH (`/private/tmp/claude-501/fakebin`), niciodata pe `~/.foreman` sau o copie.

## Review Focus

1. **Raport nou dupa Send back**: intrebarile fara raspuns ale aceluiasi agent se inlocuiesc; cele cu raspuns raman. Test in Task 1.
2. **Alegere care nu e in optiuni / doua alegeri la `multi: false`**: 400, nimic salvat, task neschimbat. Test in Task 3.
3. **Accept fara niciun raspuns si fara nota**: Done, fara nota catre agent (nu se plateste un run degeaba). Test in Task 3.
4. **Lider care decide (nu utilizatorul)**: intrebarile ajung in nota de raport ca text; UI-ul de raspuns nu apare. Test in Task 3 (server) si Task 4 (UI).
5. **Send back fara text si fara niciun raspuns**: refuzat ca azi ("write what needs to change"). Test in Task 3.

---

### Task 1: Migrare + store

**Files:**
- Create: `internal/store/migrations/030_task_decisions.sql`, `internal/store/decisions.go`, `internal/store/decisions_test.go`
- Modify: `internal/store/todos.go` (Todo.Kind, AddTodos cu kind, ListTodos), `internal/store/store_test.go` (user_version)

**Interfaces (Produces):**
- `type Decision struct { ID, TaskID int64; Position int; Question string; Options []string; Multi bool; AskedBy string; AskedAt time.Time; Answer []string; Other string; AnsweredAt *time.Time }` (JSON: `id, taskId, position, question, options, multi, askedBy, askedAt, answer, other, answeredAt`)
- `func (s *Store) SetDecisions(taskID int64, askedBy string, qs []Decision) error` — sterge intrebarile FARA raspuns ale lui askedBy pe task, apoi le insereaza pe cele noi (pozitii dupa ultima existenta); tranzactie.
- `func (s *Store) ListDecisions(taskID int64) ([]Decision, error)` — toate, in ordinea pozitiei; non-nil.
- `func (s *Store) AnswerDecisions(taskID int64, answers []DecisionAnswer) error` — `type DecisionAnswer struct { ID int64; Choices []string; Other string }`; valideaza fiecare: apartine task-ului, nu are deja raspuns, alegerile sunt din Options, `multi=false` => max o alegere; eroare `ErrBadAnswer` (wrap cu detaliul) si nimic salvat (tranzactie). Un raspuns gol (fara alegeri si fara other) se salveaza ca raspuns gol cu answered_at setat.
- `Todo.Kind string json:"kind"` ("value" | "step"); `AddTodos(taskID int64, texts []string, kind string)` (actualizeaza apelantii existenti cu "value").

- [ ] **Step 1: Migrarea**

```sql
-- Intrebarile cu optiuni puse de agent la report_done (decisions) si raspunsurile utilizatorului
CREATE TABLE task_decisions (
  id          INTEGER PRIMARY KEY,
  task_id     INTEGER NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
  position    INTEGER NOT NULL,
  question    TEXT NOT NULL,
  options     TEXT NOT NULL,               -- JSON []string
  multi       INTEGER NOT NULL DEFAULT 0,
  asked_by    TEXT NOT NULL DEFAULT '',
  asked_at    TEXT NOT NULL,
  answer      TEXT NOT NULL DEFAULT '[]',  -- JSON []string, alegerile
  other       TEXT NOT NULL DEFAULT '',
  answered_at TEXT
);
CREATE INDEX task_decisions_task ON task_decisions(task_id, position);
-- pasii utilizatorului (user_steps) stau langa valorile de completat (left_for_user)
ALTER TABLE task_todos ADD COLUMN kind TEXT NOT NULL DEFAULT 'value';
```
`store_test.go`: user_version asteptat 29 -> 30.

- [ ] **Step 2: Testul care pica** (`decisions_test.go`): creeaza un task; `SetDecisions` cu 2 intrebari (una multi); `ListDecisions` le intoarce in ordine cu Options decodate; `AnswerDecisions` cu o alegere invalida -> eroare `ErrBadAnswer` si nimic salvat; cu doua alegeri la multi=false -> eroare; raspuns valid -> Answer/Other/AnsweredAt setate; `SetDecisions` din nou de acelasi agent -> intrebarea cu raspuns ramane, cea fara raspuns e inlocuita de cele noi; `AddTodos(..., "step")` + `ListTodos` intoarce Kind "step", iar cele vechi "value".

- [ ] **Step 3: RED** — `go test ./internal/store -run 'Decision|Todo'` pica (simboluri lipsa).

- [ ] **Step 4: Implementare** `decisions.go` + schimbarile din `todos.go` (Kind in SELECT si INSERT; apelantii `AddTodos` din team trimit "value").

- [ ] **Step 5: GREEN** — `go test ./internal/store ./internal/team && go vet ./...`.

- [ ] **Step 6: Commit** — `store: task_decisions (intrebari cu optiuni) si kind pe task_todos`.

---

### Task 2: MCP + team la raport + prompt

**Files:**
- Modify: `internal/mcp/tools.go` (reportTool: `decisions`, `user_steps`), `internal/mcp/server.go` (validare + apel), `internal/mcp/server_test.go` (fakeBackend)
- Create/Modify: `internal/team/decisions.go` (+ test), `internal/team/todos.go` (LeaveForUser cu kind)
- Modify: `internal/prompt/prompt.go` (+ test)

**Interfaces:**
- Consumes: `store.SetDecisions`, `store.AddTodos(…, kind)`.
- Produces: in `mcp.Backend`: `AskDecisions(c Caller, task string, qs []DecisionInput) (string, error)` cu `type DecisionInput struct { Question string; Options []string; Multi bool }`; `LeaveSteps(c Caller, task string, steps []string) (string, error)` (sau `LeaveForUser` cu un parametru kind — alege una si foloseste-o consecvent).

- [ ] **Step 1: Teste care pica**
  - mcp: `report_done` cu `decisions` valide -> backend apelat cu intrebarile (in ordine, multi citit) si textul contine `2 decisions waiting for the user.`; 6 intrebari -> eroare `at most 5`; o intrebare cu 1 optiune -> eroare; optiune de 121 caractere -> eroare; `user_steps` cu 11 -> eroare; in toate erorile raportul NU ruleaza (ca la `left_for_user`).
  - team: `AskDecisions` pe task-ul curent al agentului salveaza intrebarile cu `asked_by` = numele agentului; pe un task strain -> eroare (aceeasi regula ca `todoTask`); pe un task arhivat -> eroare "was deleted". Daca task-ul NU e decis de utilizator (`UserDecides` false), nota de raport catre decident contine `Decisions for the user:` cu intrebarile si optiunile.
  - prompt: textul pentru worker / sub-lead contine indicatia despre `decisions` si `user_steps`.
- [ ] **Step 2: RED**, apoi implementare:
  - validare `validDecisions` / `validSteps` in server.go (limitele din Global Constraints); argumentul `decisions` vine ca `[]any` de obiecte — citeste `question`, `options`, `multi` (bool sau lipsa).
  - ordinea in `case "report_done"`: validari -> `ReportDone` -> `LeaveForUser` -> `LeaveSteps` -> `AskDecisions` -> `AttachFiles`; un esec la cele de dupa raport nu anuleaza raportul (nota in text, ca la celelalte).
  - descrierea campurilor in `reportTool` (engleza): decisions = "Optional: up to 5 questions the user answers by picking an option (e.g. whether to open a follow-up task). Use these instead of asking in the summary."; user_steps = "Optional: up to 10 things only the user can do (set .env variables, restart a container, run a seeder, a manual check). They appear as a checklist for the user."
  - prompt: langa `howLeftForUser` o fraza: "If you have a question for the user at the end, or steps only the user can do, put them in decisions / user_steps when you call report_done, not in the summary text."
- [ ] **Step 3: GREEN** — `go test ./internal/mcp ./internal/team ./internal/prompt && go vet ./...`.
- [ ] **Step 4: Commit** — `mcp: report_done decisions si user_steps; prompt`.

---

### Task 3: team.Decide cu raspunsuri + web

**Files:**
- Modify: `internal/team/stages.go` (UserDecide primeste raspunsuri), `internal/web/api_stages.go` (decideTask), `internal/web/api_tasks.go` (GET task: `decisions`)
- Create: teste in `internal/team/decisions_test.go`, `internal/web/decisions_test.go`

**Interfaces:**
- `func (t *Team) UserDecide(taskID int64, accept bool, feedback string, answers []store.DecisionAnswer) error` — pastreaza comportamentul de azi cand `answers` e gol; actualizeaza apelantii existenti (MoveTask foloseste `decideAsUser` direct — nu se schimba).
- `func answersText(ds []store.Decision) string` — `Q1 <intrebare>: <alegeri unite cu ", ">[; other: <text>]`, cate o linie; fara alegeri si fara other -> `no answer`.

- [ ] **Step 1: Teste care pica**
  - team: task in Review decis de utilizator cu 2 intrebari puse de Rook; `UserDecide(accept, "", answers pentru Q1)` -> Done; Rook are o nota nelivrata care contine `The user accepted T-x.`, `Q1 ...: Yes` si `Q2 ...: no answer`; eveniment `decided` cu corpul raspunsurilor. Accept fara raspunsuri si fara nota -> Done si NICIO nota noua. Send back cu doar raspunsuri (fara text) -> acceptat, feedback-ul task-ului contine raspunsurile. Send back fara text si fara raspunsuri -> eroarea de azi. Raspuns invalid -> eroare, task-ul ramane in Review, nimic salvat.
  - web: `POST /decide` cu `answers` -> 204 si GET task are `decisions[0].answer`; alegere invalida -> 400; GET task intoarce `decisions` (lista goala cand nu sunt) si `todos[].kind`.
- [ ] **Step 2: RED**, apoi implementare: in `UserDecide`, sub `t.work`: verificarea de azi (ErrNotUserDecision) -> `AnswerDecisions` (eroare -> return, mapata in web la 400 prin `errors.Is(err, store.ErrBadAnswer)`) -> recitesti deciziile -> textul raspunsurilor (doar pentru intrebarile din acest apel + cele ramase fara raspuns ale task-ului) -> accept: `decideAsUser(accept)` apoi nota catre `asked_by` (agentul cu acel nume din proiect, delivered=false) doar daca exista raspunsuri nevide sau feedback; send back: feedback = nota + "\n\n" + raspunsuri (oricare poate lipsi; ambele goale -> eroarea de azi) si `decideAsUser(send back)`. Evenimentul `decided` (actor You, corp = raspunsuri + nota) se scrie in ambele cazuri cand exista ceva de spus.
- [ ] **Step 3: GREEN** — `go test ./internal/team ./internal/web && go vet ./...`.
- [ ] **Step 4: Commit** — `team/web: decide cu raspunsurile la intrebari, nota catre agent`.

---

### Task 4: UI

**Files:**
- Create: `internal/web/static/js/decide.js` (sectiunea Decide + zona de actiune, testabila separat)
- Modify: `internal/web/static/js/task-modal.js` (renderTodos: eticheta "step", titlul "Your steps"; renderActions cand decide utilizatorul), `internal/web/static/app.css`

**Interfaces:**
- `export function decideBox({ decisions, onSubmit })` -> `{ el, answers() }`; `answers()` intoarce `[{ id, choices, other }]` doar pentru intrebarile fara raspuns.
- Quick reasons: `["Missing tests", "Doesn't match the mockup", "Needs more work", "Wrong approach"]`.

- [ ] **Step 1:** `decide.js` + un test node pentru `answers()` (construieste starea fara DOM real: separa logica de colectare intr-o functie pura `collectAnswers(decisions, state)` si testeaz-o cu node; vezi testul `moveAction` din board-move pentru stil).
- [ ] **Step 2:** modala: cand decide utilizatorul (conditia de azi din `renderActions`), deasupra raportului: Decide (doar intrebarile fara raspuns), apoi caseta todos ("Your steps" daca exista vreun `kind === "step"`, pasii cu eticheta "step"); zona de actiune: textarea "Anything to add?", butoanele de motive rapide (adauga textul la sfarsitul campului, pe linie noua), Accept si Send back mari. Accept / Send back trimit `{ action, feedback, answers }`. Erorile (400/409) apar ca toast. Raspunsurile deja date apar read-only (ex. "You answered: Yes") in Conversation (din eveniment) si nu mai apar in Decide.
- [ ] **Step 3:** `node --check` pe fisierele schimbate; verificare vizuala pe instanta goala (task in Review creat din board, intrebari si pasi inserate cu sqlite), 1440 si 390: Decide cu radio + checkbox + Other, Your steps, motive rapide, Accept / Send back; dupa Accept, nota agentului in DB contine raspunsurile.
- [ ] **Step 4: Commit** — `ui: Decide cu optiuni, Your steps, Accept/Send back cu motive rapide si nota`.

---

### Task 5: CLAUDE.md + verificare finala

- [ ] `go vet ./...`, `go build -race ./...`, `go test ./...`, ambele cross-compile.
- [ ] CLAUDE.md: un paragraf "Gata (decizii usor de dat, spec in docs/superpowers/specs/2026-10-09-foreman-easy-decisions-design.md): ..." dupa ultimul "Gata"; spec: `Status: implementat (2026-10-09).`
- [ ] Commit — `docs: decizii usor de dat gata`.
