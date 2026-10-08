# Library (tools, skills, MCP): plan de implementare

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** un loc comun pentru scripturi, skill-uri si servere MCP facute de agenti (sau de utilizator): agentii
propun, utilizatorul publica, iar proiectele le instaleaza si le dau agentilor la fiecare run.

**Architecture:** store-ul primeste tabelele Library (migrarea 021). Pachetul nou `internal/library` copiaza
fisierele (propunere -> versiune), le valideaza si construieste, per proiect, un plugin Claude Code
(`~/.foreman/library/build/<project-id>/`) cu `skills/` si `bin/`. `team.startRun` adauga `--plugin-dir` cu acel
folder, pune serverele MCP instalate in JSON-ul nostru de `--mcp-config` (cu secretele proiectului in `env` /
`headers`), le permite in `--settings` si listeaza tool-urile in mesajul turei. Agentii propun prin tool-ul MCP
`propose_library_item`; propunerile apar in "Needs you" si in pagina Library. UI-ul adauga `#/library/...`.

**Tech Stack:** Go 1.22+, SQLite, JS vanilla. Claude Code 2.1.x (`--plugin-dir`).

**Spec:** `docs/superpowers/specs/2026-10-08-foreman-library-design.md`

## Rezultatele spike-ului (2026-10-08, claude 2.1.x, cu exact flag-urile noastre)

- `--plugin-dir <dir>` cu `--setting-sources user`: skill-urile din `skills/` se incarca (apar ca `foreman-library:<nume>`)
  si agentul le poate folosi fara regula `Skill` in allow. **Merge.**
- `.mcp.json` din plugin cu `--strict-mcp-config`: serverul NU se incarca (`mcp_servers: []`). **Nu merge.**
- Server local pus in JSON-ul nostru de `--mcp-config` + `"allow": ["mcp__<server>__<tool>"]`: conectat, tool-ul a raspuns.
  **Asa facem MCP.** Regula de permisiune folosita: `mcp__<server>` (toate tool-urile serverului).
- Observatie (in afara planului): cu `--setting-sources user`, run-urile agentilor incarca si plugin-urile / skill-urile
  personale ale utilizatorului (din `~/.claude`). Existent si azi; de discutat separat.

## Global Constraints

- Tot ce vede utilizatorul sau agentul e in engleza; comentariile din cod in romana (fara diacritice).
- `go vet ./...`, `go build -race ./cmd/foreman`, cross-compile darwin/arm64 si windows trec. Fara dependente noi.
- Nume de item: `^[a-z][a-z0-9-]{1,39}$`, unic per kind. Descriere 1..300. Propunere: max 20 fisiere, max 1 MB total,
  doar din folderul agentului (cale relativa, fara `..`, fara symlink-uri). Max 3 propuneri deschise per proiect.
- Secretele (nume `^[A-Z][A-Z0-9_]{1,63}$`) stau doar in `library_secrets`; nu intra in prompt, in log-ul din UI, in
  raspunsurile API (GET intoarce doar numele si `set: true/false`) si nici in propunerea copiata.
- Fisierele unei versiuni sunt read-only dupa publicare (`~/.foreman/library/items/<id>/<versiune>/`).
- Pe Windows, wrapper-ele cu secrete pentru scripturi nu se genereaza (scriptul ruleaza fara ele; UI-ul spune asta).
- `TestOpenAppliesMigrationsOnce` trece la 21 (sau la urmatorul numar, daca planul Left for you a luat 020).
- Comenzile se ruleaza din `agentdash/`. Nu atinge aplicatia de pe 7777 si nici `~/.foreman`.

## Review Focus

1. Propunere cu o cale care iese din folderul agentului (`../../.ssh/id_rsa`, symlink spre afara): refuzata (test in Task 2).
2. Un proiect instaleaza un item, apoi item-ul e unpublished: proiectul il pastreaza la versiunea instalata (test in Task 1/3).
3. Secret lipsa pentru un MCP instalat: run-ul porneste fara acel server si agentul afla de ce (linie in tura), nu esueaza (test in Task 3).
4. Doua run-uri in paralel in acelasi proiect: build-ul pluginului nu se calca (scriere in folder temporar + rename; test in Task 2).
5. Un item sters de pe disc (cineva a curatat `~/.foreman/library`): build-ul il sare si logheaza, run-ul merge (test in Task 2).

---

### Task 1: store: tabelele Library

**Files:**
- Create: `internal/store/migrations/021_library.sql`
- Create: `internal/store/library.go`
- Create: `internal/store/library_test.go`
- Modify: `internal/store/store_test.go` (numarul de migrari)

**Interfaces:**
- Produces:

```go
const (KindTool = "tool"; KindSkill = "skill"; KindMCP = "mcp")
const (ItemProposed = "proposed"; ItemPublished = "published"; ItemDeclined = "declined"; ItemUnpublished = "unpublished")
type LibraryItem struct {
	ID int64 `json:"id"`; Kind string `json:"kind"`; Name string `json:"name"`; Description string `json:"description"`
	Status string `json:"status"`; SourceProjectID int64 `json:"sourceProjectId"`; SourceTaskID int64 `json:"sourceTaskId"`
	MadeByAgentID int64 `json:"madeByAgentId"`; MadeBy string `json:"madeBy"` // numele la momentul propunerii, "You" pentru utilizator
	Needs []string `json:"needs"`; Secrets []string `json:"secrets"`
	Transport string `json:"transport"` // mcp: "local" | "remote"
	URL string `json:"url"`; Command string `json:"command"`; Args []string `json:"args"`
	Entry string `json:"entry"` // tool: fisierul care se ruleaza (ex. "pdf-offer.sh")
	ToolNames []string `json:"toolNames"` // mcp: tool-urile expuse (din probe)
	CreatedAt time.Time `json:"createdAt"`; PublishedAt *time.Time `json:"publishedAt"`
}
type LibraryVersion struct { ID, ItemID int64; Version string; Dir string; Files int; Lines int; Note string; CreatedAt time.Time } // cu tag-uri json camelCase
type LibraryInstall struct { ProjectID, ItemID, VersionID int64; Enabled bool }
func (s *Store) AddLibraryItem(it *LibraryItem) error
func (s *Store) GetLibraryItem(id int64) (LibraryItem, error)
func (s *Store) FindLibraryItem(kind, name string) (LibraryItem, error) // ErrNotFound
func (s *Store) ListLibraryItems(kind string) ([]LibraryItem, error)   // kind "" = toate; fara declined
func (s *Store) SetLibraryStatus(id int64, status string) error
func (s *Store) AddLibraryVersion(v *LibraryVersion) error
func (s *Store) ListLibraryVersions(itemID int64) ([]LibraryVersion, error) // cea mai noua prima
func (s *Store) Install(projectID, itemID, versionID int64) error          // upsert, enabled
func (s *Store) Uninstall(projectID, itemID int64) error
func (s *Store) ListInstalls(projectID int64) ([]LibraryInstall, error)
func (s *Store) SetSecret(projectID, itemID int64, name, value string) error
func (s *Store) Secrets(projectID, itemID int64) (map[string]string, error)
func (s *Store) CountOpenProposals(projectID int64) (int, error)
func (s *Store) AddLibraryUse(itemID, taskID, agentID int64) error
func (s *Store) RecentLibraryUses(itemID int64, n int) ([]LibraryUse, error) // LibraryUse{TaskID, Number, Title, Agent, At}
```

- [ ] **Step 1: testul**

`internal/store/library_test.go`:

```go
package store

import "testing"

func TestLibraryLifecycle(t *testing.T) {
	s := newStore(t)
	p, lead := newProject(t, s)
	other, _ := newProject(t, s)
	it := LibraryItem{Kind: KindTool, Name: "pdf-offer", Description: "Builds a PDF offer", Status: ItemProposed,
		SourceProjectID: p.ID, MadeByAgentID: lead.ID, MadeBy: "Lead", Needs: []string{"write"}, Secrets: []string{"BRAND_KEY"}, Entry: "pdf-offer.sh"}
	if err := s.AddLibraryItem(&it); err != nil {
		t.Fatal(err)
	}
	dup := it
	dup.ID = 0
	if err := s.AddLibraryItem(&dup); err == nil {
		t.Fatal("acelasi nume si kind de doua ori")
	}
	if n, _ := s.CountOpenProposals(p.ID); n != 1 {
		t.Fatalf("propuneri deschise = %d", n)
	}
	v := LibraryVersion{ItemID: it.ID, Version: "1.0", Dir: "/x/1.0", Files: 2, Lines: 40}
	if err := s.AddLibraryVersion(&v); err != nil {
		t.Fatal(err)
	}
	if err := s.SetLibraryStatus(it.ID, ItemPublished); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetLibraryItem(it.ID)
	if got.Status != ItemPublished || got.PublishedAt == nil || got.Secrets[0] != "BRAND_KEY" {
		t.Fatalf("%+v", got)
	}
	if err := s.Install(other.ID, it.ID, v.ID); err != nil {
		t.Fatal(err)
	}
	s.SetSecret(other.ID, it.ID, "BRAND_KEY", "s3cret")
	if sec, _ := s.Secrets(other.ID, it.ID); sec["BRAND_KEY"] != "s3cret" {
		t.Fatalf("%v", sec)
	}
	// unpublish: instalarea ramane
	s.SetLibraryStatus(it.ID, ItemUnpublished)
	if ins, _ := s.ListInstalls(other.ID); len(ins) != 1 || ins[0].VersionID != v.ID {
		t.Fatalf("%+v", ins)
	}
	if list, _ := s.ListLibraryItems(KindTool); len(list) != 1 {
		t.Fatalf("%+v", list)
	}
	s.Uninstall(other.ID, it.ID)
	if ins, _ := s.ListInstalls(other.ID); len(ins) != 0 {
		t.Fatal("dezinstalarea nu a mers")
	}
}
```

- [ ] **Step 2: ruleaza, trebuie sa pice** — `go test ./internal/store/ -run TestLibraryLifecycle` -> FAIL.

- [ ] **Step 3: implementarea**

`021_library.sql`:

```sql
-- Library: tool-uri (scripturi), skill-uri si servere MCP refolosite intre proiecte
CREATE TABLE library_items (
  id                INTEGER PRIMARY KEY,
  kind              TEXT NOT NULL CHECK (kind IN ('tool', 'skill', 'mcp')),
  name              TEXT NOT NULL,
  description       TEXT NOT NULL,
  status            TEXT NOT NULL CHECK (status IN ('proposed', 'published', 'declined', 'unpublished')),
  source_project_id INTEGER REFERENCES projects(id) ON DELETE SET NULL,
  source_task_id    INTEGER REFERENCES tasks(id) ON DELETE SET NULL,
  made_by_agent_id  INTEGER REFERENCES agents(id) ON DELETE SET NULL,
  made_by           TEXT NOT NULL DEFAULT '',
  needs             TEXT NOT NULL DEFAULT '[]',
  secrets           TEXT NOT NULL DEFAULT '[]',
  transport         TEXT NOT NULL DEFAULT '',
  url               TEXT NOT NULL DEFAULT '',
  command           TEXT NOT NULL DEFAULT '',
  args              TEXT NOT NULL DEFAULT '[]',
  entry             TEXT NOT NULL DEFAULT '',
  tool_names        TEXT NOT NULL DEFAULT '[]',
  created_at        TEXT NOT NULL,
  published_at      TEXT,
  UNIQUE (kind, name)
);
CREATE TABLE library_versions (
  id         INTEGER PRIMARY KEY,
  item_id    INTEGER NOT NULL REFERENCES library_items(id) ON DELETE CASCADE,
  version    TEXT NOT NULL,
  dir        TEXT NOT NULL,
  files      INTEGER NOT NULL DEFAULT 0,
  lines      INTEGER NOT NULL DEFAULT 0,
  note       TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  UNIQUE (item_id, version)
);
CREATE TABLE library_installs (
  project_id INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  item_id    INTEGER NOT NULL REFERENCES library_items(id) ON DELETE CASCADE,
  version_id INTEGER NOT NULL REFERENCES library_versions(id) ON DELETE CASCADE,
  enabled    INTEGER NOT NULL DEFAULT 1,
  PRIMARY KEY (project_id, item_id)
);
CREATE TABLE library_secrets (
  project_id INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  item_id    INTEGER NOT NULL REFERENCES library_items(id) ON DELETE CASCADE,
  name       TEXT NOT NULL,
  value      TEXT NOT NULL,
  PRIMARY KEY (project_id, item_id, name)
);
CREATE TABLE library_uses (
  id       INTEGER PRIMARY KEY,
  item_id  INTEGER NOT NULL REFERENCES library_items(id) ON DELETE CASCADE,
  task_id  INTEGER REFERENCES tasks(id) ON DELETE SET NULL,
  agent_id INTEGER REFERENCES agents(id) ON DELETE SET NULL,
  at       TEXT NOT NULL
);
CREATE INDEX library_uses_item ON library_uses(item_id, at);
```

`library.go`: CRUD-urile din Interfaces, in stilul din `project_links.go` (`scanLibraryItem(sc scanner)`, listele JSON
cu `json.Marshal`/`json.Unmarshal`, `nullID` pentru cheile optionale, `mustAffect` pe UPDATE/DELETE,
`SetLibraryStatus` pune `published_at = now()` la primul `published`). `ListLibraryItems` exclude `declined`, ordonat
dupa `status = 'proposed' DESC, name`. `CountOpenProposals`: `status = 'proposed' AND source_project_id = ?`.
`RecentLibraryUses` face JOIN cu `tasks` (numar, titlu) si `agents` (nume), ultimele `n` dupa `at DESC`.

- [ ] **Step 4:** `go test ./internal/store/` -> PASS (cu numarul de migrari actualizat).
- [ ] **Step 5: commit** `store: tabelele Library (migrarea 021)`.

### Task 2: pachetul `internal/library`: propuneri, versiuni si build-ul pluginului

**Files:**
- Create: `internal/library/library.go` (Lib, CopyProposal, Publish, Build)
- Create: `internal/library/probe.go` (ProbeMCP pentru servere locale)
- Create: `internal/library/library_test.go`, `internal/library/probe_test.go`

**Interfaces:**
- Consumes: store-ul din Task 1.
- Produces:

```go
type Lib struct { Root string; St *store.Store } // Root = ~/.foreman/library
func New(root string, st *store.Store) *Lib
// CopyProposal copiaza fisierele din folderul agentului in Root/proposals/<item-id>/ (validare: Global Constraints)
func (l *Lib) CopyProposal(itemID int64, srcDir string, files []string) (dir string, nfiles, lines int, err error)
// Publish face din propunere (sau din versiunea curenta + fisierele noi) o versiune read-only si o instaleaza in proiectul sursa
func (l *Lib) Publish(itemID int64, note string) (store.LibraryVersion, error)
// Build: pluginul proiectului; intoarce ce trebuie pus pe linia de comanda si in tura
type Build struct {
	PluginDir  string            // "" daca proiectul nu are skill-uri / tool-uri instalate
	MCPServers map[string]any    // intra in mcpServers din --mcp-config
	Allow      []string          // "mcp__<server>" pentru serverele active
	TurnLines  []string          // "Library tools (run them with Bash): /.../bin/pdf-offer", "stock-db: missing secret DATABASE_URL" ...
	ToolPaths  map[string]int64  // calea comenzii -> item (pentru "Recent use")
}
func (l *Lib) Build(projectID int64) (Build, error)
func ProbeMCP(command string, args []string, env map[string]string, timeout time.Duration) ([]string, error) // tools/list
```

- [ ] **Step 1: testele**

`library_test.go` (fragmente esentiale; scrie-le complet):

```go
func TestCopyProposalRejectsEscapes(t *testing.T) {
	st, lib, src := newEnv(t) // store temporar, Lib pe un Root temporar, folder sursa cu fisiere
	os.WriteFile(filepath.Join(src, "run.sh"), []byte("#!/bin/sh\necho hi\n"), 0o755)
	os.Symlink("/etc/hosts", filepath.Join(src, "link"))
	it := addItem(t, st, store.KindTool, "hi")
	for _, bad := range [][]string{{"../outside"}, {"/etc/hosts"}, {"link"}, {}} {
		if _, _, _, err := lib.CopyProposal(it.ID, src, bad); err == nil {
			t.Fatalf("acceptat: %v", bad)
		}
	}
	dir, n, lines, err := lib.CopyProposal(it.ID, src, []string{"run.sh"})
	if err != nil || n != 1 || lines != 2 {
		t.Fatalf("%s %d %d %v", dir, n, lines, err)
	}
}

func TestBuildSkillsToolsAndMCP(t *testing.T) {
	st, lib, src := newEnv(t)
	p := project(t, st)
	// skill publicat si instalat
	os.MkdirAll(filepath.Join(src, "ro"), 0o755)
	os.WriteFile(filepath.Join(src, "ro", "SKILL.md"), []byte("---\nname: ro-copy\ndescription: Romanian copy\n---\nUse ș.\n"), 0o644)
	skill := addItem(t, st, store.KindSkill, "ro-copy")
	lib.CopyProposal(skill.ID, filepath.Join(src, "ro"), []string{"SKILL.md"})
	v, _ := lib.Publish(skill.ID, "")
	st.Install(p.ID, skill.ID, v.ID)
	// mcp local cu un secret lipsa
	m := store.LibraryItem{Kind: store.KindMCP, Name: "stock-db", Description: "x", Status: store.ItemPublished,
		Transport: "local", Command: "python3", Args: []string{"server.py"}, Secrets: []string{"DATABASE_URL"}}
	st.AddLibraryItem(&m)
	mv := store.LibraryVersion{ItemID: m.ID, Version: "1.0", Dir: t.TempDir()}
	st.AddLibraryVersion(&mv)
	st.Install(p.ID, m.ID, mv.ID)

	b, err := lib.Build(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(b.PluginDir, "skills", "ro-copy", "SKILL.md")); err != nil {
		t.Fatal("skill-ul lipseste din plugin")
	}
	if _, err := os.Stat(filepath.Join(b.PluginDir, ".claude-plugin", "plugin.json")); err != nil {
		t.Fatal("plugin.json lipseste")
	}
	if len(b.MCPServers) != 0 || !strings.Contains(strings.Join(b.TurnLines, "\n"), "missing secret DATABASE_URL") {
		t.Fatalf("server fara secret pornit: %+v %v", b.MCPServers, b.TurnLines)
	}
	st.SetSecret(p.ID, m.ID, "DATABASE_URL", "postgres://x")
	b, _ = lib.Build(p.ID)
	srv := b.MCPServers["stock-db"].(map[string]any)
	if srv["env"].(map[string]string)["DATABASE_URL"] != "postgres://x" || b.Allow[0] != "mcp__stock-db" {
		t.Fatalf("%+v %v", srv, b.Allow)
	}
	// item sters de pe disc: sarit, fara eroare
	os.RemoveAll(v.Dir)
	if _, err := lib.Build(p.ID); err != nil {
		t.Fatal(err)
	}
}

func TestBuildConcurrent(t *testing.T) {
	st, lib, _ := newEnv(t)
	p := project(t, st)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); if _, err := lib.Build(p.ID); err != nil { t.Error(err) } }()
	}
	wg.Wait()
}
```

`probe_test.go`: foloseste tiparul "helper process" din biblioteca standard (testul se re-porneste ca server MCP):

```go
func TestHelperMCP(t *testing.T) {
	if os.Getenv("FOREMAN_HELPER_MCP") != "1" {
		return
	}
	sc := bufio.NewScanner(os.Stdin)
	for sc.Scan() {
		var m struct{ ID any `json:"id"`; Method string `json:"method"` }
		json.Unmarshal(sc.Bytes(), &m)
		if m.ID == nil {
			continue
		}
		var r any = map[string]any{}
		switch m.Method {
		case "initialize":
			r = map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{"tools": map[string]any{}}, "serverInfo": map[string]any{"name": "t", "version": "1"}}
		case "tools/list":
			r = map[string]any{"tools": []any{map[string]any{"name": "list_vehicles"}, map[string]any{"name": "get_vehicle"}}}
		}
		out, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": m.ID, "result": r})
		fmt.Println(string(out))
	}
	os.Exit(0)
}

func TestProbeMCP(t *testing.T) {
	names, err := ProbeMCP(os.Args[0], []string{"-test.run=TestHelperMCP"}, map[string]string{"FOREMAN_HELPER_MCP": "1"}, 5*time.Second)
	if err != nil || strings.Join(names, ",") != "list_vehicles,get_vehicle" {
		t.Fatalf("%v %v", names, err)
	}
	if _, err := ProbeMCP("sleep", []string{"30"}, nil, 300*time.Millisecond); err == nil {
		t.Fatal("un server care nu raspunde trebuia sa expire")
	}
}
```

- [ ] **Step 2:** `go test ./internal/library/` -> FAIL.

- [ ] **Step 3: implementarea**

`library.go`, puncte cheie (scrie codul complet dupa ele):

```go
// CopyProposal: fiecare cale e relativa la srcDir, curatata cu filepath.Clean; refuzam "..", cai absolute,
// symlink-uri (os.Lstat), foldere, peste 20 de fisiere sau 1 MB in total. Destinatia se sterge si se recreeaza.
// lines = numarul de linii din toate fisierele (bytes.Count(data, "\n"), +1 daca ultimul nu se termina cu \n).

// Publish: versiunea urmatoare ("1.0", apoi "1.1"...): copiaza Root/proposals/<id>/ in Root/items/<id>/<versiune>/,
// pune fisierele read-only (0o444, scripturile 0o555), AddLibraryVersion, SetLibraryStatus(published) si Install
// in proiectul sursa (daca exista). Propunerea copiata se sterge dupa publicare.

// Build: un mutex per proiect (map[int64]*sync.Mutex sub un mutex global) + scriere in Root/build/<p>.tmp-<rand>
// urmata de os.RemoveAll(final) si os.Rename(tmp, final). Pentru fiecare instalare activa:
//   skill -> copiaza dir-ul versiunii in skills/<name>/
//   tool  -> copiaza in bin/<name>/; daca are secrete si runtime.GOOS != "windows", scrie bin/<name>/run (0o700):
//            "#!/bin/sh\nexport K='v'...\nexec \"$(dirname \"$0\")/<entry>\" \"$@\"\n" (valorile cu ' scapate ca '\''),
//            altfel comanda e direct entry-ul; ToolPaths[cale] = item; TurnLines: "- <name>: <descriere> — run: <cale>"
//   mcp   -> daca lipseste un secret: TurnLines "Library server <name> is off: missing secret <S>. Ask the user to add it in the project's Library settings.";
//            local: {"command": cmd, "args": args (cu ${DIR} inlocuit cu dir-ul versiunii), "env": secrete}
//            remote: {"type": "http", "url": url, "headers": {"Authorization": "Bearer " + secretul AUTH_TOKEN daca exista}}
//            Allow += "mcp__<name>"
// Un dir de versiune lipsa: log.Printf si skip. Fara skill-uri si tool-uri: PluginDir = "".
// plugin.json: {"name": "foreman-library", "version": "1", "description": "Foreman library for project <id>"}
```

`probe.go`: porneste procesul (`exec.Command`, `cmd.Env = append(os.Environ(), env...)`), scrie pe stdin
`initialize`, `notifications/initialized`, `tools/list` (JSON pe linii), citeste linii pana la raspunsul cu `id` 2,
cu `time.AfterFunc(timeout, kill)`; intoarce numele tool-urilor in ordine.

- [ ] **Step 4:** `go test ./internal/library/ -race` -> PASS.
- [ ] **Step 5: commit** `library: propuneri, versiuni, build-ul pluginului per proiect si probe MCP`.

### Task 3: integrarea in run-uri

**Files:**
- Modify: `internal/prompt/settings.go` (`MCPConfig(url string, extra map[string]any)`, `Settings(tools, folder, extraAllow)`)
- Modify: `internal/team/run.go` (Build -> `--plugin-dir`, MCP, allow, linii in tura)
- Modify: `internal/team/team.go` (`SetLibrary(*library.Lib)`)
- Modify: `internal/setup/manager.go`, `internal/draft/draft.go` (apelurile noi ale `MCPConfig`, cu `nil`)
- Modify: `cmd/foreman/main.go` (`lib := library.New(filepath.Join(paths.Root, "library"), st)`; `tm.SetLibrary(lib)`)
- Test: `internal/team/library_test.go`, `internal/prompt/settings_test.go`

**Interfaces:**
- Consumes: `library.Lib.Build` (Task 2).
- Produces: un run al unui agent dintr-un proiect cu item-uri instalate are `--plugin-dir <dir>`, serverele in
  `--mcp-config`, `mcp__<server>` in allow si sectiunea "Library" in tura.

- [ ] **Step 1: testele**

`settings_test.go`:

```go
func TestMCPConfigExtra(t *testing.T) {
	got := MCPConfig("http://x/mcp/t", map[string]any{"stock-db": map[string]any{"command": "python3"}})
	if !strings.Contains(got, `"team"`) || !strings.Contains(got, `"stock-db"`) {
		t.Fatal(got)
	}
	if !strings.Contains(Settings([]string{"write"}, "/w", []string{"mcp__stock-db"}), `"mcp__stock-db"`) {
		t.Fatal("allow-ul extra lipseste")
	}
}
```

`internal/team/library_test.go`: fixture-ul `setup(t)`, un skill publicat si instalat in `f.p` (prin `library.New`
pe un Root temporar + `CopyProposal`/`Publish`), `f.tm.SetLibrary(lib)`, porneste un run pentru Rook (cum fac testele
existente, de ex. un task queued + `f.tm.Kick`/apelul folosit in `reconcile_test.go`), apoi:

```go
	spec := f.fs.last(t, f.rook)
	if !hasFlag(spec.Args, "--plugin-dir") {
		t.Fatal("fara --plugin-dir")
	}
	if !strings.Contains(spec.Args[1], "Library") { // -p <tura>
		t.Fatalf("tura nu mentioneaza Library: %s", spec.Args[1])
	}
```

- [ ] **Step 2:** testele pica.
- [ ] **Step 3: implementarea**

`MCPConfig`:

```go
// MCPConfig: serverul echipei plus serverele din Library instalate in proiect (extra poate fi nil)
func MCPConfig(url string, extra map[string]any) string {
	servers := map[string]any{"team": map[string]any{"type": "http", "url": url}}
	for name, cfg := range extra {
		if name != "team" {
			servers[name] = cfg
		}
	}
	b, _ := json.Marshal(map[string]any{"mcpServers": servers})
	return string(b)
}
```

`Settings` primeste `extraAllow []string` adaugat la `allow`. Toate apelurile existente primesc `nil`.

`team/run.go`, in `startRun` dupa blocul cu fisierele proiectului:

```go
	// Library: pluginul proiectului (skill-uri, scripturi) si serverele MCP instalate
	var libBuild library.Build
	if t.lib != nil {
		if b, err := t.lib.Build(p.ID); err != nil {
			log.Printf("library build for project %d: %v", p.ID, err) // run-ul merge si fara
		} else {
			libBuild = b
		}
	}
	if libBuild.PluginDir != "" {
		extraArgs = append(extraArgs, "--plugin-dir", libBuild.PluginDir)
	}
	if len(libBuild.TurnLines) > 0 {
		turn += "\n\nLibrary (shared tools, skills and servers installed in this project):\n" + strings.Join(libBuild.TurnLines, "\n")
	}
```

iar `args` primeste `libBuild` (sau doar `MCPServers`/`Allow`) si foloseste `prompt.Settings(a.Tools, home, b.Allow)` si
`prompt.MCPConfig(url, b.MCPServers)`. Atentie: `args` e apelat din mai multe locuri; cauta `t.args(` si transmite
`library.Build{}` unde nu e un run de agent obisnuit.

- [ ] **Step 4:** `go test ./internal/... && go vet ./...` -> PASS.
- [ ] **Step 5: commit** `team: run-urile primesc Library (plugin-dir, MCP, linii in tura)`.

### Task 4: `propose_library_item` (agentii propun)

**Files:**
- Modify: `internal/mcp/tools.go`, `internal/mcp/server.go`, `internal/mcp/server_test.go`
- Create: `internal/team/library.go`, `internal/team/library_propose_test.go`

**Interfaces:**
- Produces: `Backend.ProposeLibraryItem(c Caller, in LibraryInput) (string, error)` cu

```go
type LibraryInput struct {
	Kind, Name, Description, Entry, Command, URL string
	Files, Args, Needs, Secrets []string
}
```

tool-ul (doar agentilor cu tool-ul `write`; `Caller.CanWrite bool`, completat in `team.Resolve` din `a.Tools`):

```go
var proposeLibraryTool = Tool{
	Name: "propose_library_item",
	Description: "Proposes something you built here for the shared Library, so other projects can use it: a script (tool), " +
		"instructions (skill, a folder with SKILL.md) or an MCP server. The user reviews and publishes it; keep using it in this project meanwhile.",
	InputSchema: schema([]string{"kind", "name", "description", "files"}, map[string]any{
		"kind":        map[string]any{"type": "string", "enum": []string{"tool", "skill", "mcp"}},
		"name":        str("lowercase-with-dashes, e.g. pdf-offer"),
		"description": str("One sentence: what it does"),
		"files":       map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Paths relative to your folder (max 20 files, 1 MB)"},
		"entry":       str("tool: the file to run, e.g. pdf-offer.sh"),
		"command":     str("mcp (local): the command that starts the server, e.g. python3"),
		"args":        map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "mcp (local): its arguments; ${DIR} is the server's folder"},
		"url":         str("mcp (remote): the server URL"),
		"needs":       map[string]any{"type": "array", "items": map[string]any{"type": "string", "enum": []string{"network", "write", "browser", "shell"}}},
		"secrets":     map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Names of secrets each project provides, e.g. META_TOKEN"},
	}),
}
```

Regulile in `team.ProposeLibraryItem` (sub `t.work`): validari din Global Constraints; max 3 propuneri deschise per
proiect; `skill` cere `SKILL.md` in `files`; `tool` cere `entry` din `files`; `mcp` local: `CopyProposal` apoi
`library.ProbeMCP(command, args cu ${DIR}=folderul propunerii, secrete goale, 10 s)` — un server care nu raspunde e
refuzat cu eroarea probe-ului; `ToolNames` = rezultatul. Creeaza item-ul `proposed`, `MadeBy` = numele agentului,
`SourceTaskID` = `c.TaskID`. Mesaj in chat-ul agentului (nota livrata) si eveniment SSE `library`.
Raspuns: `Proposed <name> to the library. The user decides in Needs you; keep using it in this project meanwhile.`

- [ ] **Step 1: teste** (mcp: tool-ul apare doar cu `CanWrite`; team: o propunere de skill trece, o a 4-a e refuzata,
  `files: ["../x"]` e refuzat, `tool` fara `entry` e refuzat, un `mcp` local spre helper-ul din Task 2 trece si salveaza `ToolNames`).
- [ ] **Step 2-4:** implementare, `go test ./internal/... -race`, `go vet ./...`.
- [ ] **Step 5: commit** `mcp+team: propose_library_item`.

### Task 5: API-ul Library

**Files:**
- Create: `internal/web/api_library.go`, `internal/web/library_test.go`
- Modify: `internal/web/server.go` (rute; `SetLibrary`), `internal/web/api_projects.go` (`libraryProposals` in proiect, numarate in "need you")

**Interfaces:**
- Produces:

```
GET    /api/library?kind=tool|skill|mcp          -> []itemView (item + version curenta + installs count; fara valori de secrete)
GET    /api/library/{id}                         -> itemView + versions + recentUses + files (nume, marime) + installedIn
GET    /api/library/{id}/files/{path...}         -> text/plain (fisierul din versiunea curenta sau din propunere; max 512 KB)
POST   /api/library/{id}/publish {note}          -> 200 version
POST   /api/library/{id}/decline                 -> 204 (status declined, ramane instalat in sursa: "Keep in <project>")
POST   /api/library/{id}/unpublish               -> 204
POST   /api/library                              -> 201 (utilizatorul: skill din text, tool din text + entry, mcp remote/local)
PUT    /api/projects/{id}/library/{item}         {installed: bool, secrets: {NAME: value}} -> 204
GET    /api/projects/{id}/library                -> [{item, installed, secrets: [{name, set}]}]
```

- [ ] **Step 1: teste** pentru fiecare ruta (inclusiv: GET nu contine valoarea secretului; publish de doua ori creeaza 1.0 apoi 1.1; un fisier cu `..` in cale da 400).
- [ ] **Step 2-4:** implementare si `go test ./internal/web/`.
- [ ] **Step 5: commit** `web: API Library`.

### Task 6: UI Library

**Files:**
- Create: `internal/web/static/js/library.js` (paginile Tools / Skills / MCP si pagina unui item)
- Modify: `internal/web/static/js/app.js` (rute `#/library/tools|skills|mcp`, `#/library/item/<id>`)
- Modify: `internal/web/static/js/ui.js` (`topbar` primeste navigatia `Projects | Library`)
- Modify: `internal/web/static/js/workspace.js` (propunerile in "Needs you"; cardul din chat pentru o propunere)
- Modify: `internal/web/static/js/projects.js` (formularul Instructions: sectiunea "Library" cu instalari si secrete)
- Modify: `internal/web/static/app.css`

Continut (dupa mockup-urile 09-13):
- **Tools** (09): titlurile mari `Tools  Skills  MCP` (linkuri), subtitlul "Made by agents in one project, shared with
  every project. N tools · M waiting for you", filtrele All / Mine / Recently added, sectiunea "Proposed · needs you"
  cu carduri (nume mono, eticheta tipului, "2 h ago", descriere, autor, needs, Review code / Decline / Publish), apoi
  tabelul Tool / What it does / Made by / Version.
- **Skills** (10): grila de carduri (icon ✦, nume, Proposed / vN, descriere, avatare, `SKILL.md · 84 lines`) si panoul din
  dreapta cu inceputul fisierului SKILL.md, "No tools needed" / needs, Publish / Edit / Keep in <project>.
- **MCP** (11): cardul propunerii cu "Exposes" (ToolNames) si "Each project provides" (secretele ca pastile), "Tested in
  <project> · N/N tools respond"; tabelul Server / Description / Runs (Local/Remote) / Tools / Made by; filtrele All /
  By agents / External.
- **Item** (12): breadcrumb `Tools / <name>`, titlu mono, descriere, tab-urile Usage (cum il cheama agentii: comanda din
  build) / Code (fisierele, cu `md-pre`) / Versions N; "Recent use" (T-nr, titlu, agent); dreapta: Type, Made by,
  Approved by, Version, Needs; jos Unpublish.
- **Chat** (13): cand evenimentul SSE `library` vine pentru o propunere a agentului selectat, sub mesajul lui apare
  cardul "Share to Tool library?" (nume, tip, `N files · M lines`, needs/secrete, nota despre secrete, Publish / Review code / Keep in this project).
- **Needs you**: un card per propunere a proiectului ("LIBRARY · TOOL", nume, descriere, Review / Publish).
- **Instructions > Library**: item-urile publicate, cu comutator Installed si campuri pentru secrete (tip password,
  "set" daca exista deja); salvare imediata (PUT).

- [ ] **Step 1:** implementeaza paginile (refoloseste `topbar`, `.ptable`, `.np-card`, `.label-tag`, `.md-pre`).
- [ ] **Step 2:** `node --check` pe copii `.mjs`; pe o copie a bazei de date (7799) creeaza din UI un skill si un MCP
  remote, publica, instaleaza in alt proiect, pune un secret; screenshot-uri la 1440 si 420.
- [ ] **Step 3: commit** `ui: Library (tools, skills, MCP), propuneri in Needs you si in chat`.

### Task 7: "Recent use" si documentatie

**Files:**
- Modify: `internal/runner/tracker.go` (colecteaza numele tool_use: `Outcome.ToolUses []string`)
- Modify: `internal/team/run.go` (`recordOutcome`: comenzile Bash care incep cu o cale din `ToolPaths` si tool-urile `mcp__<server>__*` -> `AddLibraryUse`)
- Modify: `CLAUDE.md`, spec-ul (sectiunea "Cum ajung la agenti" cu rezultatele spike-ului)

- [ ] **Step 1: test** in `internal/team/library_test.go`: un Outcome cu `Commands: [{Text: "<cale>/bin/pdf-offer/pdf-offer.sh --data x"}]` si `ToolUses: ["mcp__stock-db__list_vehicles"]` produce doua randuri in `library_uses`.
- [ ] **Step 2-4:** implementare, `go test ./... -race`, `go vet ./...`, cross-compile.
- [ ] **Step 5:** linie in `CLAUDE.md` / Stare + spec actualizat; commit `team+docs: Recent use pentru Library`.
