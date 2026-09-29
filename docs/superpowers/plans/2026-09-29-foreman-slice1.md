# Foreman felia 1: plan de implementare

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Transforma agentdash in Foreman. Aplicatia are proiecte si echipe de agenti Claude Code in care un lead creeaza task-uri prin MCP, workerii le executa si raporteaza, iar totul se vede live pe un board si persista in SQLite.

**Architecture:**
- Un binar Go serveste UI-ul (HTML + JS vanilla embedded), API-ul REST + SSE si un server MCP minimal (JSON-RPC pe `POST /mcp/{token}`).
- Pachetul `team` e singurul care leaga `store` (SQLite), `runner` (procesele `claude -p`), `mcp` si `prompt`. Tot ce schimba starea trece printr-un singur mutex, iar sursa de adevar e baza de date.
- Run-urile se pornesc prin reconcilierea proiectului la fiecare eveniment (`Kick`).

**Tech Stack:**
- Go 1.22+ (routing din `net/http` cu `{param}`);
- `modernc.org/sqlite` (pure Go, fara CGO);
- Claude Code CLI 2.1.x in mod headless (`-p --output-format stream-json`);
- HTML, CSS si JS vanilla (ES modules), fara build step.

**Spec:** `docs/superpowers/specs/2026-09-29-foreman-slice1-design.md`

**Verificat inainte de predare (2026-09-29):**
- tot codul Go din plan a fost extras intr-o copie separata, unde trec `gofmt`, `go vet`, `go test -race ./...` (3 rulari fara esecuri intermitente) si cross-compile-ul darwin/arm64 si windows;
- fisierele JS trec `node --check`, iar importurile dintre module se rezolva.

## Global Constraints

- Mesajele pentru utilizator (UI, erori API, texte catre agenti) sunt in romana, **fara diacritice**.
- Singura dependenta noua este `modernc.org/sqlite`. Nu se adauga alte biblioteci (nici MCP SDK, nici router, nici framework JS).
- Serverul asculta doar pe `127.0.0.1`, fara autentificare.
- Dupa fiecare task trebuie sa treaca `go vet ./...` si `go build -race ./...`. La final se verifica si cross-compile-ul: `GOOS=darwin GOARCH=arm64 go build ./cmd/foreman` si `GOOS=windows go build ./cmd/foreman`.
- Numele aplicatiei exista intr-un singur loc: `config.AppName = "foreman"`. Folderul de date e `~/.foreman`, baza de date `~/.foreman/foreman.db`, logurile `~/.foreman/logs/runs/<id>.jsonl`.
- Modelele permise sunt exact `opus`, `sonnet` si `haiku`. Tool-urile agentului sunt exact `write`, `shell`, `git` si `web`.
- `permissions.deny` fix (vezi spec 6): `TaskCreate`, `TaskUpdate`, `TaskList`, `TaskGet`, `TodoWrite`, `Agent`, `Bash(git push:*)`, `Bash(rm -rf:*)`, `Bash(sudo:*)`, `Bash(npm install:*)`, `Bash(composer require:*)`, `Bash(go get:*)`, `Bash(pip install:*)`, `Bash(brew install:*)`.
- Argumentele fixe ale fiecarui run: `--output-format stream-json --verbose --setting-sources user --strict-mcp-config`. Pe langa ele: `--model`, `--append-system-prompt`, `--settings` (JSON inline), `--mcp-config` (JSON inline, server `team` de tip `http`), `--max-budget-usd` si, cand e cazul, `--resume`.
- Maxim 3 trimiteri inapoi per task (`MaxSendBacks = 3`). Implicit: `max_parallel = 2`, `budget_usd = 1`.
- La pornirea serverului toate proiectele sunt puse pe pauza. Nu porneste nimic automat.
- Autorul e incepator in Go: cod simplu si idiomatic, comentat acolo unde nu e evident. Comentariile sunt in romana, fara diacritice.

## Review Focus

- **Numele worker-ului scris diferit de lead** (`" rook "` in loc de `Rook`). Trebuie gasit fara diferenta intre litere mari si mici si fara spatii. Test in Task 8 (`TestCreateTaskAssigneeLoose`).
- **Rezultate foarte lungi de la workeri.** Notificarile lead-ului ajung ca argument de linie de comanda, iar sistemul de operare limiteaza lungimea (ARG_MAX), deci se taie la 4000 de caractere. Test in Task 8 (`TestLongSummaryIsClipped`). Campurile mari din UI (context, background, instructiuni) au limite de lungime, cu test in Task 10 (`TestProjectFieldLimits`).
- **Folder de lucru cu spatii in cale** (ex. `/Users/yzy/YzY/AI Dashboard`). Trebuie acceptat. Test in Task 10 (`TestCreateProjectWorkdirWithSpaces`).
- **Browser lent sau mai multe tab-uri deschise pe acelasi proiect.** Publicarea evenimentelor nu are voie sa blocheze scheduler-ul. Test in Task 6 (`TestSlowSubscriberDoesNotBlock`).
- **Stergerea unui proiect in timp ce echipa lucreaza** trebuie refuzata cu 409, nu sa lase procese orfane. Test in Task 10 (`TestDeleteActiveProject409`).

## Structura fisierelor

```
agentdash/                            (folderul repo-ului ramane asa; modulul Go devine "foreman")
  go.mod                              module foreman + modernc.org/sqlite
  cmd/foreman/main.go                 CLI start: store, runner, team, mcp, web; oprire curata
  internal/config/config.go           AppName, Paths{Root, DB, Logs}
  internal/config/config_test.go
  internal/store/store.go             Open, migrari, helpers (now, parseTime, mustAffect)
  internal/store/migrations/001_init.sql
  internal/store/projects.go          Project + CRUD (CreateProject creeaza si lead-ul)
  internal/store/agents.go            Agent + CRUD, SetSession
  internal/store/tasks.go             Task + CRUD, numerotare T-n, requeue
  internal/store/runs.go              Run + create/finish/interrupt
  internal/store/messages.go          Message + thread, livrare
  internal/store/*_test.go
  internal/prompt/prompt.go           System, Hash, Team, WorkerTurn, DirectTurn, LeadTurn
  internal/prompt/settings.go         ValidTools, Settings (JSON), MCPConfig (JSON)
  internal/prompt/*_test.go
  internal/runner/runner.go           Manager.Start(Spec, onDone) -> Outcome; stream-json, log, SSE
  internal/runner/tracker.go          extrage session_id / result / cost / erori din stream
  internal/runner/proc_unix.go        (neschimbat)
  internal/runner/proc_windows.go     (neschimbat)
  internal/runner/testdata/fakeclaude.sh
  internal/runner/*_test.go
  internal/events/hub.go              pub/sub per proiect pentru SSE
  internal/events/hub_test.go
  internal/mcp/server.go              JSON-RPC: initialize, tools/list, tools/call
  internal/mcp/tools.go               definitiile tool-urilor pentru lead si worker
  internal/mcp/server_test.go
  internal/team/team.go               Team, Starter, tokeni, Kick, emit, note, status
  internal/team/tools.go              implementarea mcp.Backend (create_task, ...)
  internal/team/reconcile.go          Run (bucla), Reconcile, Stop, Resume, Recover, actiuni user
  internal/team/run.go                startRun (argumente claude), finish
  internal/team/*_test.go             fake_test.go (Starter fals), tools_test.go, reconcile_test.go
  internal/web/server.go              rute, helpers JSON/erori
  internal/web/api_projects.go
  internal/web/api_agents.go          agenti, mesaje, prompt-preview
  internal/web/api_tasks.go
  internal/web/sse.go                 stream run + stream proiect
  internal/web/server_test.go
  internal/web/static/index.html
  internal/web/static/app.css
  internal/web/static/js/{app,api,ui,projects,agent,workspace,live}.js
  internal/web/static/fonts/*.woff2
  README.md, CLAUDE.md                actualizate in Task 13
```

Se sterg: `internal/agent/` si `cmd/agentdash/` (mutat).

---

### Task 1: Redenumire in foreman si `config.AppName`

**Files:**
- Modify: `go.mod`
- Move: `cmd/agentdash/` -> `cmd/foreman/`
- Modify: toate fisierele `.go` (importurile `agentdash/...` devin `foreman/...`)
- Modify: `internal/config/config.go`
- Modify: `cmd/foreman/main.go`
- Create: `internal/config/config_test.go`

**Interfaces:**
- Produces: `config.AppName` (const string `"foreman"`) si `config.Paths{Root, DB, Logs, Agents string}`. `Agents` e temporar si dispare in Task 5. Plus `config.Default() (Paths, error)` si `(Paths) Ensure() error`.

- [ ] **Step 1: Redenumeste modulul si folderul cmd**

```bash
cd "/Users/yzy/YzY/AI Dashboard/agentdash"
go mod edit -module foreman
git mv cmd/agentdash cmd/foreman
grep -rl '"agentdash/' --include=*.go . | xargs sed -i '' 's#"agentdash/#"foreman/#g'
```

- [ ] **Step 2: Scrie testul pentru config**

`internal/config/config_test.go`:

```go
package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultPaths(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	p, err := Default()
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(home, ".foreman")
	if p.Root != root {
		t.Errorf("Root = %q, vreau %q", p.Root, root)
	}
	if p.DB != filepath.Join(root, "foreman.db") {
		t.Errorf("DB = %q", p.DB)
	}
	if p.Logs != filepath.Join(root, "logs", "runs") {
		t.Errorf("Logs = %q", p.Logs)
	}
	if err := p.Ensure(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p.Logs); err != nil {
		t.Errorf("Ensure nu a creat %s: %v", p.Logs, err)
	}
}
```

- [ ] **Step 3: Ruleaza testul ca sa vezi ca pica**

Run: `go test ./internal/config/`
Expected: FAIL la compilare (`p.DB undefined`).

- [ ] **Step 4: Rescrie `internal/config/config.go`**

```go
package config

import (
	"os"
	"path/filepath"
)

// AppName e numele aplicatiei. Din el se deriva folderul de date, fisierul
// bazei de date si textele afisate. Ca sa redenumesti aplicatia: schimbi
// constanta, redenumesti cmd/<nume> si rulezi `go mod edit -module <nume>`
// plus un replace pe importuri.
const AppName = "foreman"

// Paths tine toate locatiile de pe disc folosite de aplicatie.
type Paths struct {
	Root   string // ~/.foreman
	DB     string // ~/.foreman/foreman.db
	Logs   string // ~/.foreman/logs/runs
	Agents string // ~/.foreman/agents (modelul vechi cu foldere, dispare in Task 5)
}

func Default() (Paths, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Paths{}, err
	}
	root := filepath.Join(home, "."+AppName)
	return Paths{
		Root:   root,
		DB:     filepath.Join(root, AppName+".db"),
		Logs:   filepath.Join(root, "logs", "runs"),
		Agents: filepath.Join(root, "agents"),
	}, nil
}

// Ensure creeaza folderele daca nu exista.
func (p Paths) Ensure() error {
	for _, d := range []string{p.Root, p.Logs, p.Agents} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return err
		}
	}
	return nil
}
```

- [ ] **Step 5: Foloseste AppName in `cmd/foreman/main.go`**

Inlocuieste constanta `usage` si locurile unde apare textul `agentdash`:

```go
const usage = `%[1]s - echipe de agenti Claude Code, local

Utilizare:
  %[1]s start [--port 7777] [--no-open]
`
```

In `main()`, ambele `fmt.Print(usage)` devin `fmt.Printf(usage, config.AppName)`. In `start()`:

```go
	log.Printf("%s ruleaza pe %s", config.AppName, url)
```

si comentariul `// Shutdown ... (la Ctrl+C pe agentdash)` din runner devine `(la Ctrl+C)`.

- [ ] **Step 6: Verifica**

Run: `go test ./internal/config/ && go vet ./... && go build -race ./... && go run ./cmd/foreman`
Expected: PASS pe test si vet. Ultima comanda afiseaza usage-ul `foreman - echipe de agenti Claude Code, local` si iese cu codul 1.

- [ ] **Step 7: Commit**

```bash
git add -A
git commit -m "Redenumire in foreman, AppName intr-un singur loc"
```

---

### Task 2: `store`: SQLite, migrari, proiecte si agenti

**Files:**
- Create: `internal/store/store.go`
- Create: `internal/store/migrations/001_init.sql`
- Create: `internal/store/projects.go`
- Create: `internal/store/agents.go`
- Create: `internal/store/store_test.go`
- Modify: `go.mod`, `go.sum` (prin `go get`)

**Interfaces:**
- Produces:
  - `store.Open(path string) (*Store, error)`, `(*Store) Close() error`
  - `store.ErrNotFound`, `store.ErrConflict`
  - `store.PositionLead = "lead"`, `store.PositionWorker = "worker"`
  - `type Project struct { ID int64; Name, Goal, Context, Workdir string; Paused bool; MaxParallel int; BudgetUSD float64; CreatedAt time.Time }` (JSON: `id,name,goal,context,workdir,paused,maxParallel,budgetUsd,createdAt`)
  - `type Agent struct { ID, ProjectID int64; Name, Role, Position, Model, Background, Instructions, DoneDef string; Tools []string; SessionID, PromptHash string; CreatedAt time.Time }` (JSON: `id,projectId,name,role,position,model,background,instructions,doneDef,tools,sessionId,createdAt`; `PromptHash` nu apare in JSON)
  - Pe `*Store`:
    - `CreateProject(p *Project) (Agent, error)`, care creeaza si lead-ul implicit (`Lead`, `sonnet`, fara tool-uri)
    - `GetProject(id) (Project, error)`, `ListProjects() ([]Project, error)`, `UpdateProject(p Project) error`, `DeleteProject(id) error`, `SetPaused(id int64, paused bool) error`, `PauseAll() error`
    - `CreateAgent(a *Agent) error`, `GetAgent(id) (Agent, error)`, `ListAgents(projectID) ([]Agent, error)` (lead-ul primul), `Lead(projectID) (Agent, error)`, `FindAgentByName(projectID int64, name string) (Agent, error)`, `UpdateAgent(a Agent) error`, `DeleteAgent(id) error`, `SetSession(id int64, sessionID, promptHash string) error`
  - Helpere interne folosite de Task 3: `now() string`, `parseTime(string) time.Time`, `mustAffect(sql.Result, error) error`, `isUnique(error) bool`, interfetele `execer` si `scanner`.

- [ ] **Step 1: Adauga driverul SQLite**

```bash
go get modernc.org/sqlite@v1.60.0
```

Expected: `go.mod` contine `modernc.org/sqlite v1.60.0`. Directiva `go` se poate ridica automat, si e in regula.

- [ ] **Step 2: Scrie schema `internal/store/migrations/001_init.sql`**

```sql
CREATE TABLE projects (
  id           INTEGER PRIMARY KEY,
  name         TEXT NOT NULL,
  goal         TEXT NOT NULL DEFAULT '',
  context      TEXT NOT NULL DEFAULT '',
  workdir      TEXT NOT NULL,
  paused       INTEGER NOT NULL DEFAULT 0,
  max_parallel INTEGER NOT NULL DEFAULT 2,
  budget_usd   REAL    NOT NULL DEFAULT 1,
  created_at   TEXT NOT NULL
);

CREATE TABLE agents (
  id           INTEGER PRIMARY KEY,
  project_id   INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  name         TEXT NOT NULL,
  role         TEXT NOT NULL DEFAULT '',
  position     TEXT NOT NULL CHECK (position IN ('lead','worker')),
  model        TEXT NOT NULL DEFAULT 'sonnet',
  background   TEXT NOT NULL DEFAULT '',
  instructions TEXT NOT NULL DEFAULT '',
  done_def     TEXT NOT NULL DEFAULT '',
  tools        TEXT NOT NULL DEFAULT '[]',
  session_id   TEXT NOT NULL DEFAULT '',
  prompt_hash  TEXT NOT NULL DEFAULT '',
  created_at   TEXT NOT NULL
);
-- exact un lead per proiect
CREATE UNIQUE INDEX agents_one_lead ON agents(project_id) WHERE position = 'lead';
-- nume unic in proiect, fara diferenta intre litere mari si mici
CREATE UNIQUE INDEX agents_name ON agents(project_id, name COLLATE NOCASE);

CREATE TABLE tasks (
  id          INTEGER PRIMARY KEY,
  project_id  INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  number      INTEGER NOT NULL,
  title       TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT '',
  assignee_id INTEGER NOT NULL REFERENCES agents(id),
  status      TEXT NOT NULL CHECK (status IN ('queued','in_progress','review','done','blocked')),
  result      TEXT NOT NULL DEFAULT '',
  feedback    TEXT NOT NULL DEFAULT '',
  send_backs  INTEGER NOT NULL DEFAULT 0,
  created_at  TEXT NOT NULL,
  updated_at  TEXT NOT NULL,
  UNIQUE (project_id, number)
);
CREATE INDEX tasks_assignee ON tasks(assignee_id);

CREATE TABLE runs (
  id         TEXT PRIMARY KEY,
  agent_id   INTEGER NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
  task_id    INTEGER REFERENCES tasks(id),
  status     TEXT NOT NULL CHECK (status IN ('running','done','failed','stopped','interrupted')),
  started_at TEXT NOT NULL,
  ended_at   TEXT,
  cost_usd   REAL,
  error      TEXT NOT NULL DEFAULT ''
);

CREATE TABLE messages (
  id         INTEGER PRIMARY KEY,
  project_id INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  agent_id   INTEGER NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
  kind       TEXT NOT NULL CHECK (kind IN ('msg','event')),
  author     TEXT NOT NULL,
  text       TEXT NOT NULL,
  run_id     TEXT,
  delivered  INTEGER NOT NULL DEFAULT 1,
  created_at TEXT NOT NULL
);
CREATE INDEX messages_agent ON messages(agent_id, id);
```

- [ ] **Step 3: Scrie testele `internal/store/store_test.go`**

```go
package store

import (
	"errors"
	"path/filepath"
	"testing"
)

func newStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func newProject(t *testing.T, s *Store) (Project, Agent) {
	t.Helper()
	p := Project{Name: "ApiUnit", Goal: "Import OpenAPI", Workdir: t.TempDir()}
	lead, err := s.CreateProject(&p)
	if err != nil {
		t.Fatal(err)
	}
	return p, lead
}

func newWorker(t *testing.T, s *Store, projectID int64, name string) Agent {
	t.Helper()
	a := Agent{ProjectID: projectID, Name: name, Role: "Dev", Position: PositionWorker, Model: "sonnet", Tools: []string{"write"}}
	if err := s.CreateAgent(&a); err != nil {
		t.Fatal(err)
	}
	return a
}

func TestOpenAppliesMigrationsOnce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x.db")
	for i := 0; i < 2; i++ { // a doua deschidere nu trebuie sa reaplice schema
		s, err := Open(path)
		if err != nil {
			t.Fatalf("deschiderea %d: %v", i+1, err)
		}
		var v int
		if err := s.db.QueryRow("PRAGMA user_version").Scan(&v); err != nil {
			t.Fatal(err)
		}
		if v != 1 {
			t.Fatalf("user_version = %d, vreau 1", v)
		}
		s.Close()
	}
}

func TestCreateProjectAddsLeadAndDefaults(t *testing.T) {
	s := newStore(t)
	p, lead := newProject(t, s)
	if p.ID == 0 || p.MaxParallel != 2 || p.BudgetUSD != 1 {
		t.Fatalf("proiect = %+v", p)
	}
	if lead.Position != PositionLead || lead.Name != "Lead" || lead.Model != "sonnet" || len(lead.Tools) != 0 {
		t.Fatalf("lead = %+v", lead)
	}
	got, err := s.Lead(p.ID)
	if err != nil || got.ID != lead.ID {
		t.Fatalf("Lead() = %+v, %v", got, err)
	}
}

func TestUpdateAndDeleteProject(t *testing.T) {
	s := newStore(t)
	p, _ := newProject(t, s)
	p.Name, p.MaxParallel, p.BudgetUSD = "Nou", 4, 2.5
	if err := s.UpdateProject(p); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetProject(p.ID)
	if got.Name != "Nou" || got.MaxParallel != 4 || got.BudgetUSD != 2.5 {
		t.Fatalf("dupa update: %+v", got)
	}
	if err := s.DeleteProject(p.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetProject(p.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetProject dupa stergere: %v", err)
	}
	if err := s.DeleteProject(p.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a doua stergere: %v", err)
	}
}

func TestPauseAll(t *testing.T) {
	s := newStore(t)
	p1, _ := newProject(t, s)
	p2, _ := newProject(t, s)
	if err := s.SetPaused(p1.ID, true); err != nil {
		t.Fatal(err)
	}
	if err := s.SetPaused(p1.ID, false); err != nil {
		t.Fatal(err)
	}
	if err := s.PauseAll(); err != nil {
		t.Fatal(err)
	}
	for _, id := range []int64{p1.ID, p2.ID} {
		p, _ := s.GetProject(id)
		if !p.Paused {
			t.Errorf("proiectul %d nu e pe pauza", id)
		}
	}
}

func TestAgentNameUniqueCaseInsensitive(t *testing.T) {
	s := newStore(t)
	p, _ := newProject(t, s)
	newWorker(t, s, p.ID, "Rook")
	dup := Agent{ProjectID: p.ID, Name: "rook", Position: PositionWorker, Model: "sonnet"}
	if err := s.CreateAgent(&dup); !errors.Is(err, ErrConflict) {
		t.Fatalf("nume duplicat: %v", err)
	}
	other, _ := newProject(t, s)
	newWorker(t, s, other.ID, "Rook") // alt proiect: e voie
}

func TestOnlyOneLead(t *testing.T) {
	s := newStore(t)
	p, _ := newProject(t, s)
	second := Agent{ProjectID: p.ID, Name: "Al doilea", Position: PositionLead, Model: "sonnet"}
	if err := s.CreateAgent(&second); !errors.Is(err, ErrConflict) {
		t.Fatalf("al doilea lead: %v", err)
	}
}

func TestListAgentsLeadFirstAndFind(t *testing.T) {
	s := newStore(t)
	p, lead := newProject(t, s)
	rook := newWorker(t, s, p.ID, "Rook")
	vera := newWorker(t, s, p.ID, "Vera")
	agents, err := s.ListAgents(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(agents) != 3 || agents[0].ID != lead.ID || agents[1].ID != rook.ID || agents[2].ID != vera.ID {
		t.Fatalf("ordine gresita: %+v", agents)
	}
	got, err := s.FindAgentByName(p.ID, "VERA")
	if err != nil || got.ID != vera.ID {
		t.Fatalf("FindAgentByName = %+v, %v", got, err)
	}
	if _, err := s.FindAgentByName(p.ID, "Nimeni"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("agent inexistent: %v", err)
	}
}

func TestUpdateAgentAndSession(t *testing.T) {
	s := newStore(t)
	p, _ := newProject(t, s)
	a := newWorker(t, s, p.ID, "Rook")
	a.Role, a.Model, a.Tools = "Backend", "opus", []string{"write", "shell"}
	if err := s.UpdateAgent(a); err != nil {
		t.Fatal(err)
	}
	if err := s.SetSession(a.ID, "sess-1", "hash-1"); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetAgent(a.ID)
	if got.Role != "Backend" || got.Model != "opus" || len(got.Tools) != 2 || got.Tools[1] != "shell" {
		t.Fatalf("dupa update: %+v", got)
	}
	if got.SessionID != "sess-1" || got.PromptHash != "hash-1" {
		t.Fatalf("sesiune: %q %q", got.SessionID, got.PromptHash)
	}
}

func TestDeleteLeadRejected(t *testing.T) {
	s := newStore(t)
	_, lead := newProject(t, s)
	if err := s.DeleteAgent(lead.ID); !errors.Is(err, ErrConflict) {
		t.Fatalf("stergere lead: %v", err)
	}
}
```

- [ ] **Step 4: Ruleaza testele ca sa vezi ca pica**

Run: `go test ./internal/store/`
Expected: FAIL la compilare (`undefined: Open`).

- [ ] **Step 5: Scrie `internal/store/store.go`**

```go
// Package store tine toata starea aplicatiei in SQLite: proiecte, agenti,
// task-uri, run-uri si mesaje.
package store

import (
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite" // driver SQLite in Go pur; se inregistreaza sub numele "sqlite"
)

var (
	ErrNotFound = errors.New("nu exista")
	ErrConflict = errors.New("conflict")
)

//go:embed migrations/*.sql
var migrations embed.FS

type Store struct {
	db *sql.DB
}

// Open deschide (sau creeaza) baza de date si aplica migrarile lipsa.
func Open(path string) (*Store, error) {
	dsn := "file:" + path + "?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	// O singura conexiune: SQLite are oricum un singur writer, iar asa nu
	// apar erori "database is locked" intre goroutine.
	// ATENTIE: in interiorul unei tranzactii folosim doar tx, niciodata s.db,
	// altfel asteptam dupa conexiunea pe care o tinem chiar noi (deadlock).
	db.SetMaxOpenConns(1)
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrare baza de date: %w", err)
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

// migrate aplica fisierele din migrations/ in ordinea numelui.
// Numarul ultimei migrari aplicate se tine in PRAGMA user_version.
func (s *Store) migrate() error {
	var version int
	if err := s.db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		return err
	}
	entries, err := migrations.ReadDir("migrations") // deja sortate dupa nume
	if err != nil {
		return err
	}
	for i, e := range entries {
		n := i + 1
		if n <= version {
			continue
		}
		body, err := migrations.ReadFile("migrations/" + e.Name())
		if err != nil {
			return err
		}
		tx, err := s.db.Begin()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(string(body)); err != nil {
			tx.Rollback()
			return fmt.Errorf("%s: %w", e.Name(), err)
		}
		if _, err := tx.Exec(fmt.Sprintf("PRAGMA user_version = %d", n)); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

// execer e implementat si de *sql.DB si de *sql.Tx.
type execer interface {
	Exec(query string, args ...any) (sql.Result, error)
}

// scanner e implementat si de *sql.Row si de *sql.Rows.
type scanner interface {
	Scan(dest ...any) error
}

// Timpii se salveaza ca text RFC3339 in UTC; asa se sorteaza corect si ca text.
func now() string { return time.Now().UTC().Format(time.RFC3339Nano) }

func parseTime(s string) time.Time {
	t, _ := time.Parse(time.RFC3339Nano, s)
	return t
}

func isUnique(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}

// mustAffect intoarce ErrNotFound daca un UPDATE/DELETE n-a atins niciun rand.
func mustAffect(res sql.Result, err error) error {
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}
```

- [ ] **Step 6: Scrie `internal/store/projects.go`**

```go
package store

import (
	"database/sql"
	"errors"
	"time"
)

type Project struct {
	ID          int64     `json:"id"`
	Name        string    `json:"name"`
	Goal        string    `json:"goal"`
	Context     string    `json:"context"`
	Workdir     string    `json:"workdir"`
	Paused      bool      `json:"paused"`
	MaxParallel int       `json:"maxParallel"`
	BudgetUSD   float64   `json:"budgetUsd"`
	CreatedAt   time.Time `json:"createdAt"`
}

const projectCols = `id, name, goal, context, workdir, paused, max_parallel, budget_usd, created_at`

func scanProject(sc scanner) (Project, error) {
	var p Project
	var created string
	err := sc.Scan(&p.ID, &p.Name, &p.Goal, &p.Context, &p.Workdir, &p.Paused, &p.MaxParallel, &p.BudgetUSD, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return Project{}, ErrNotFound
	}
	if err != nil {
		return Project{}, err
	}
	p.CreatedAt = parseTime(created)
	return p, nil
}

// CreateProject salveaza proiectul si ii creeaza lead-ul implicit in aceeasi
// tranzactie, ca un proiect sa nu existe niciodata fara lead.
func (s *Store) CreateProject(p *Project) (Agent, error) {
	if p.MaxParallel <= 0 {
		p.MaxParallel = 2
	}
	if p.BudgetUSD <= 0 {
		p.BudgetUSD = 1
	}
	created := now()

	tx, err := s.db.Begin()
	if err != nil {
		return Agent{}, err
	}
	defer tx.Rollback() // nu face nimic dupa Commit

	res, err := tx.Exec(`INSERT INTO projects (name, goal, context, workdir, paused, max_parallel, budget_usd, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		p.Name, p.Goal, p.Context, p.Workdir, p.Paused, p.MaxParallel, p.BudgetUSD, created)
	if err != nil {
		return Agent{}, err
	}
	if p.ID, err = res.LastInsertId(); err != nil {
		return Agent{}, err
	}
	p.CreatedAt = parseTime(created)

	lead := Agent{ProjectID: p.ID, Name: "Lead", Role: "Lead", Position: PositionLead, Model: "sonnet"}
	if err := insertAgent(tx, &lead); err != nil {
		return Agent{}, err
	}
	return lead, tx.Commit()
}

func (s *Store) GetProject(id int64) (Project, error) {
	return scanProject(s.db.QueryRow(`SELECT `+projectCols+` FROM projects WHERE id = ?`, id))
}

func (s *Store) ListProjects() ([]Project, error) {
	rows, err := s.db.Query(`SELECT ` + projectCols + ` FROM projects ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Project{}
	for rows.Next() {
		p, err := scanProject(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// UpdateProject salveaza campurile editabile din UI (nu si paused).
func (s *Store) UpdateProject(p Project) error {
	return mustAffect(s.db.Exec(`UPDATE projects SET name = ?, goal = ?, context = ?, workdir = ?, max_parallel = ?, budget_usd = ?
		WHERE id = ?`, p.Name, p.Goal, p.Context, p.Workdir, p.MaxParallel, p.BudgetUSD, p.ID))
}

// DeleteProject sterge proiectul si, in cascada, agentii, task-urile, run-urile
// si mesajele lui. Folderul de lucru de pe disc nu e atins.
func (s *Store) DeleteProject(id int64) error {
	return mustAffect(s.db.Exec(`DELETE FROM projects WHERE id = ?`, id))
}

func (s *Store) SetPaused(id int64, paused bool) error {
	return mustAffect(s.db.Exec(`UPDATE projects SET paused = ? WHERE id = ?`, paused, id))
}

// PauseAll pune toate proiectele pe pauza (la pornirea serverului).
func (s *Store) PauseAll() error {
	_, err := s.db.Exec(`UPDATE projects SET paused = 1`)
	return err
}
```

- [ ] **Step 7: Scrie `internal/store/agents.go`**

```go
package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

const (
	PositionLead   = "lead"
	PositionWorker = "worker"
)

type Agent struct {
	ID           int64     `json:"id"`
	ProjectID    int64     `json:"projectId"`
	Name         string    `json:"name"`
	Role         string    `json:"role"`
	Position     string    `json:"position"`
	Model        string    `json:"model"`
	Background   string    `json:"background"`
	Instructions string    `json:"instructions"`
	DoneDef      string    `json:"doneDef"`
	Tools        []string  `json:"tools"`
	SessionID    string    `json:"sessionId"`
	PromptHash   string    `json:"-"`
	CreatedAt    time.Time `json:"createdAt"`
}

const agentCols = `id, project_id, name, role, position, model, background, instructions, done_def, tools, session_id, prompt_hash, created_at`

func scanAgent(sc scanner) (Agent, error) {
	var a Agent
	var tools, created string
	err := sc.Scan(&a.ID, &a.ProjectID, &a.Name, &a.Role, &a.Position, &a.Model, &a.Background,
		&a.Instructions, &a.DoneDef, &tools, &a.SessionID, &a.PromptHash, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return Agent{}, ErrNotFound
	}
	if err != nil {
		return Agent{}, err
	}
	if err := json.Unmarshal([]byte(tools), &a.Tools); err != nil {
		return Agent{}, err
	}
	if a.Tools == nil {
		a.Tools = []string{}
	}
	a.CreatedAt = parseTime(created)
	return a, nil
}

func toolsJSON(tools []string) string {
	if tools == nil {
		tools = []string{}
	}
	b, _ := json.Marshal(tools)
	return string(b)
}

func insertAgent(ex execer, a *Agent) error {
	created := now()
	res, err := ex.Exec(`INSERT INTO agents (project_id, name, role, position, model, background, instructions, done_def, tools, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		a.ProjectID, a.Name, a.Role, a.Position, a.Model, a.Background, a.Instructions, a.DoneDef, toolsJSON(a.Tools), created)
	if isUnique(err) {
		return ErrConflict
	}
	if err != nil {
		return err
	}
	if a.ID, err = res.LastInsertId(); err != nil {
		return err
	}
	if a.Tools == nil {
		a.Tools = []string{}
	}
	a.CreatedAt = parseTime(created)
	return nil
}

// CreateAgent adauga un agent. Intoarce ErrConflict daca numele exista deja in
// proiect (fara diferenta intre litere mari si mici) sau daca ar fi al doilea lead.
func (s *Store) CreateAgent(a *Agent) error { return insertAgent(s.db, a) }

func (s *Store) GetAgent(id int64) (Agent, error) {
	return scanAgent(s.db.QueryRow(`SELECT `+agentCols+` FROM agents WHERE id = ?`, id))
}

// ListAgents intoarce echipa proiectului: lead-ul primul, apoi workerii in
// ordinea in care au fost adaugati.
func (s *Store) ListAgents(projectID int64) ([]Agent, error) {
	rows, err := s.db.Query(`SELECT `+agentCols+` FROM agents WHERE project_id = ?
		ORDER BY position = 'lead' DESC, id`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Agent{}
	for rows.Next() {
		a, err := scanAgent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Store) Lead(projectID int64) (Agent, error) {
	return scanAgent(s.db.QueryRow(`SELECT `+agentCols+` FROM agents WHERE project_id = ? AND position = 'lead'`, projectID))
}

// FindAgentByName cauta dupa nume fara diferenta intre litere mari si mici.
func (s *Store) FindAgentByName(projectID int64, name string) (Agent, error) {
	return scanAgent(s.db.QueryRow(`SELECT `+agentCols+` FROM agents
		WHERE project_id = ? AND name = ? COLLATE NOCASE`, projectID, name))
}

// UpdateAgent salveaza campurile editabile. Pozitia si proiectul nu se schimba.
func (s *Store) UpdateAgent(a Agent) error {
	res, err := s.db.Exec(`UPDATE agents SET name = ?, role = ?, model = ?, background = ?, instructions = ?, done_def = ?, tools = ?
		WHERE id = ?`, a.Name, a.Role, a.Model, a.Background, a.Instructions, a.DoneDef, toolsJSON(a.Tools), a.ID)
	if isUnique(err) {
		return ErrConflict
	}
	return mustAffect(res, err)
}

// DeleteAgent sterge un worker. Lead-ul si agentii care au task-uri nu se pot
// sterge (ErrConflict): task-urile ar ramane fara asignat.
func (s *Store) DeleteAgent(id int64) error {
	a, err := s.GetAgent(id)
	if err != nil {
		return err
	}
	if a.Position == PositionLead {
		return ErrConflict
	}
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM tasks WHERE assignee_id = ?`, id).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return ErrConflict
	}
	return mustAffect(s.db.Exec(`DELETE FROM agents WHERE id = ?`, id))
}

// SetSession retine sesiunea Claude a agentului (pentru --resume) si hash-ul
// system prompt-ului cu care a fost creata.
func (s *Store) SetSession(id int64, sessionID, promptHash string) error {
	return mustAffect(s.db.Exec(`UPDATE agents SET session_id = ?, prompt_hash = ? WHERE id = ?`, sessionID, promptHash, id))
}
```

- [ ] **Step 8: Ruleaza testele**

Run: `go test ./internal/store/ -v`
Expected: PASS pe toate cele 9 teste.

- [ ] **Step 9: Verifica si fa commit**

```bash
go vet ./... && go build -race ./...
git add -A
git commit -m "store: SQLite cu migrari, proiecte si agenti"
```

---

### Task 3: `store`: task-uri, run-uri, mesaje

**Files:**
- Create: `internal/store/tasks.go`
- Create: `internal/store/runs.go`
- Create: `internal/store/messages.go`
- Create: `internal/store/tasks_test.go`

**Interfaces:**
- Consumes (Task 2): `now`, `parseTime`, `mustAffect`, `scanner`, `ErrNotFound`, `ErrConflict`, `newStore`, `newProject`, `newWorker` (helpere de test).
- Produces:
  - Constante: `StatusQueued = "queued"`, `StatusInProgress = "in_progress"`, `StatusReview = "review"`, `StatusDone = "done"`, `StatusBlocked = "blocked"`; `RunRunning = "running"`, `RunDone = "done"`, `RunFailed = "failed"`, `RunStopped = "stopped"`, `RunInterrupted = "interrupted"`; `KindMsg = "msg"`, `KindEvent = "event"`, `AuthorUser = "user"`
  - `type Task struct { ID, ProjectID int64; Number int; Title, Description string; AssigneeID int64; Status, Result, Feedback string; SendBacks int; CreatedAt, UpdatedAt time.Time }` si `(Task) Ref() string` (intoarce `"T-3"`)
  - `type Run struct { ID string; AgentID, TaskID int64; Status string; StartedAt time.Time; EndedAt *time.Time; CostUSD float64; Error string }`, unde `TaskID` 0 inseamna fara task
  - `type Message struct { ID, ProjectID, AgentID int64; Kind, Author, Text, RunID string; Delivered bool; CreatedAt time.Time }`
  - Pe `*Store`:
    - `CreateTask(t *Task) error` (pune `Number` si `Status = queued`), `GetTask(id)`, `GetTaskByNumber(projectID int64, n int)`, `ListTasks(projectID) ([]Task, error)`, `SaveTask(t *Task) error`, `RequeueInProgress(projectID int64) error`, `RequeueAllInProgress() error`
    - `CreateRun(r *Run) error`, `FinishRun(id, status string, costUSD float64, errText string) error`, `GetRun(id string) (Run, error)`, `InterruptRunning() error`
    - `AddMessage(m *Message) error`, `ListMessages(agentID) ([]Message, error)`, `Undelivered(agentID) ([]Message, error)`, `HasUndelivered(agentID) (bool, error)`, `MarkDelivered(ids []int64) error`

- [ ] **Step 1: Scrie testele `internal/store/tasks_test.go`**

```go
package store

import (
	"errors"
	"testing"
)

func TestTaskNumbersPerProject(t *testing.T) {
	s := newStore(t)
	p1, _ := newProject(t, s)
	p2, _ := newProject(t, s)
	w1 := newWorker(t, s, p1.ID, "Rook")
	w2 := newWorker(t, s, p2.ID, "Rook")

	var got []int
	for _, x := range []struct {
		pid, aid int64
	}{{p1.ID, w1.ID}, {p1.ID, w1.ID}, {p2.ID, w2.ID}} {
		task := Task{ProjectID: x.pid, Title: "t", AssigneeID: x.aid}
		if err := s.CreateTask(&task); err != nil {
			t.Fatal(err)
		}
		if task.Status != StatusQueued {
			t.Fatalf("status initial = %q", task.Status)
		}
		got = append(got, task.Number)
	}
	if got[0] != 1 || got[1] != 2 || got[2] != 1 {
		t.Fatalf("numere = %v, vreau [1 2 1]", got)
	}
	task, err := s.GetTaskByNumber(p1.ID, 2)
	if err != nil || task.Ref() != "T-2" {
		t.Fatalf("GetTaskByNumber = %+v, %v", task, err)
	}
	if _, err := s.GetTaskByNumber(p1.ID, 9); !errors.Is(err, ErrNotFound) {
		t.Fatalf("task inexistent: %v", err)
	}
}

func TestSaveTaskAndRequeue(t *testing.T) {
	s := newStore(t)
	p, _ := newProject(t, s)
	w := newWorker(t, s, p.ID, "Rook")
	a := Task{ProjectID: p.ID, Title: "A", AssigneeID: w.ID}
	b := Task{ProjectID: p.ID, Title: "B", AssigneeID: w.ID}
	s.CreateTask(&a)
	s.CreateTask(&b)

	a.Status, a.Result, a.Feedback, a.SendBacks = StatusInProgress, "r", "f", 2
	if err := s.SaveTask(&a); err != nil {
		t.Fatal(err)
	}
	b.Status = StatusReview
	s.SaveTask(&b)

	if err := s.RequeueInProgress(p.ID); err != nil {
		t.Fatal(err)
	}
	gotA, _ := s.GetTask(a.ID)
	gotB, _ := s.GetTask(b.ID)
	if gotA.Status != StatusQueued || gotA.Result != "r" || gotA.Feedback != "f" || gotA.SendBacks != 2 {
		t.Fatalf("A dupa requeue: %+v", gotA)
	}
	if gotB.Status != StatusReview {
		t.Fatalf("B nu trebuia atins: %+v", gotB)
	}
	tasks, _ := s.ListTasks(p.ID)
	if len(tasks) != 2 || tasks[0].Title != "A" {
		t.Fatalf("ListTasks = %+v", tasks)
	}
}

func TestDeleteAgentWithTasksRejected(t *testing.T) {
	s := newStore(t)
	p, _ := newProject(t, s)
	w := newWorker(t, s, p.ID, "Rook")
	s.CreateTask(&Task{ProjectID: p.ID, Title: "A", AssigneeID: w.ID})
	if err := s.DeleteAgent(w.ID); !errors.Is(err, ErrConflict) {
		t.Fatalf("stergere worker cu task-uri: %v", err)
	}
	free := newWorker(t, s, p.ID, "Vera")
	if err := s.DeleteAgent(free.ID); err != nil {
		t.Fatalf("stergere worker liber: %v", err)
	}
}

func TestRunsLifecycle(t *testing.T) {
	s := newStore(t)
	p, lead := newProject(t, s)
	w := newWorker(t, s, p.ID, "Rook")
	task := Task{ProjectID: p.ID, Title: "A", AssigneeID: w.ID}
	s.CreateTask(&task)

	r1 := Run{ID: "r1", AgentID: w.ID, TaskID: task.ID}
	r2 := Run{ID: "r2", AgentID: lead.ID}
	if err := s.CreateRun(&r1); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateRun(&r2); err != nil {
		t.Fatal(err)
	}
	if err := s.FinishRun("r1", RunFailed, 0.75, "buget depasit"); err != nil {
		t.Fatal(err)
	}
	if err := s.InterruptRunning(); err != nil {
		t.Fatal(err)
	}
	got1, _ := s.GetRun("r1")
	got2, _ := s.GetRun("r2")
	if got1.Status != RunFailed || got1.CostUSD != 0.75 || got1.Error != "buget depasit" || got1.EndedAt == nil || got1.TaskID != task.ID {
		t.Fatalf("r1 = %+v", got1)
	}
	if got2.Status != RunInterrupted || got2.TaskID != 0 {
		t.Fatalf("r2 = %+v", got2)
	}
	if err := s.FinishRun("nu-exista", RunDone, 0, ""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("run inexistent: %v", err)
	}
}

func TestMessagesDelivery(t *testing.T) {
	s := newStore(t)
	p, lead := newProject(t, s)
	m1 := Message{ProjectID: p.ID, AgentID: lead.ID, Kind: KindMsg, Author: AuthorUser, Text: "salut", Delivered: false}
	m2 := Message{ProjectID: p.ID, AgentID: lead.ID, Kind: KindEvent, Text: "info", Delivered: true}
	m3 := Message{ProjectID: p.ID, AgentID: lead.ID, Kind: KindEvent, Text: "Rook a terminat", Delivered: false, RunID: "r1"}
	for _, m := range []*Message{&m1, &m2, &m3} {
		if err := s.AddMessage(m); err != nil {
			t.Fatal(err)
		}
	}
	pending, err := s.Undelivered(lead.ID)
	if err != nil || len(pending) != 2 || pending[0].ID != m1.ID || pending[1].RunID != "r1" {
		t.Fatalf("Undelivered = %+v, %v", pending, err)
	}
	if has, _ := s.HasUndelivered(lead.ID); !has {
		t.Fatal("HasUndelivered = false")
	}
	if err := s.MarkDelivered([]int64{m1.ID, m3.ID}); err != nil {
		t.Fatal(err)
	}
	if has, _ := s.HasUndelivered(lead.ID); has {
		t.Fatal("HasUndelivered = true dupa MarkDelivered")
	}
	all, _ := s.ListMessages(lead.ID)
	if len(all) != 3 || all[0].Text != "salut" || !all[0].Delivered {
		t.Fatalf("ListMessages = %+v", all)
	}
}

func TestDeleteProjectCascades(t *testing.T) {
	s := newStore(t)
	p, lead := newProject(t, s)
	w := newWorker(t, s, p.ID, "Rook")
	task := Task{ProjectID: p.ID, Title: "A", AssigneeID: w.ID}
	s.CreateTask(&task)
	s.CreateRun(&Run{ID: "r1", AgentID: w.ID, TaskID: task.ID})
	s.AddMessage(&Message{ProjectID: p.ID, AgentID: lead.ID, Kind: KindMsg, Author: AuthorUser, Text: "x"})

	if err := s.DeleteProject(p.ID); err != nil {
		t.Fatalf("stergere cu cascada: %v", err)
	}
	for _, table := range []string{"agents", "tasks", "runs", "messages"} {
		var n int
		s.db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&n)
		if n != 0 {
			t.Errorf("%s mai are %d randuri", table, n)
		}
	}
}
```

- [ ] **Step 2: Ruleaza testele ca sa vezi ca pica**

Run: `go test ./internal/store/`
Expected: FAIL la compilare (`undefined: Task`).

- [ ] **Step 3: Scrie `internal/store/tasks.go`**

```go
package store

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

const (
	StatusQueued     = "queued"
	StatusInProgress = "in_progress"
	StatusReview     = "review"
	StatusDone       = "done"
	StatusBlocked    = "blocked"
)

type Task struct {
	ID          int64     `json:"id"`
	ProjectID   int64     `json:"projectId"`
	Number      int       `json:"number"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	AssigneeID  int64     `json:"assigneeId"`
	Status      string    `json:"status"`
	Result      string    `json:"result"`
	Feedback    string    `json:"feedback"`
	SendBacks   int       `json:"sendBacks"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

// Ref e forma afisata a task-ului: T-3.
func (t Task) Ref() string { return fmt.Sprintf("T-%d", t.Number) }

const taskCols = `id, project_id, number, title, description, assignee_id, status, result, feedback, send_backs, created_at, updated_at`

func scanTask(sc scanner) (Task, error) {
	var t Task
	var created, updated string
	err := sc.Scan(&t.ID, &t.ProjectID, &t.Number, &t.Title, &t.Description, &t.AssigneeID,
		&t.Status, &t.Result, &t.Feedback, &t.SendBacks, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return Task{}, ErrNotFound
	}
	if err != nil {
		return Task{}, err
	}
	t.CreatedAt, t.UpdatedAt = parseTime(created), parseTime(updated)
	return t, nil
}

// CreateTask adauga un task in Queued, cu urmatorul numar din proiect (T-1, T-2...).
func (s *Store) CreateTask(t *Task) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if err := tx.QueryRow(`SELECT COALESCE(MAX(number), 0) + 1 FROM tasks WHERE project_id = ?`, t.ProjectID).Scan(&t.Number); err != nil {
		return err
	}
	ts := now()
	t.Status = StatusQueued
	res, err := tx.Exec(`INSERT INTO tasks (project_id, number, title, description, assignee_id, status, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, t.ProjectID, t.Number, t.Title, t.Description, t.AssigneeID, t.Status, ts, ts)
	if err != nil {
		return err
	}
	if t.ID, err = res.LastInsertId(); err != nil {
		return err
	}
	t.CreatedAt, t.UpdatedAt = parseTime(ts), parseTime(ts)
	return tx.Commit()
}

func (s *Store) GetTask(id int64) (Task, error) {
	return scanTask(s.db.QueryRow(`SELECT `+taskCols+` FROM tasks WHERE id = ?`, id))
}

func (s *Store) GetTaskByNumber(projectID int64, n int) (Task, error) {
	return scanTask(s.db.QueryRow(`SELECT `+taskCols+` FROM tasks WHERE project_id = ? AND number = ?`, projectID, n))
}

// ListTasks intoarce task-urile proiectului in ordinea numarului.
func (s *Store) ListTasks(projectID int64) ([]Task, error) {
	rows, err := s.db.Query(`SELECT `+taskCols+` FROM tasks WHERE project_id = ? ORDER BY number`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Task{}
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// SaveTask salveaza starea unui task: status, rezultat, feedback, trimiteri inapoi.
func (s *Store) SaveTask(t *Task) error {
	ts := now()
	err := mustAffect(s.db.Exec(`UPDATE tasks SET status = ?, result = ?, feedback = ?, send_backs = ?, updated_at = ?
		WHERE id = ?`, t.Status, t.Result, t.Feedback, t.SendBacks, ts, t.ID))
	if err != nil {
		return err
	}
	t.UpdatedAt = parseTime(ts)
	return nil
}

// RequeueInProgress muta task-urile In progress ale proiectului inapoi in Queued
// (la Stop team: procesele au fost oprite, munca se reia mai tarziu).
func (s *Store) RequeueInProgress(projectID int64) error {
	_, err := s.db.Exec(`UPDATE tasks SET status = 'queued', updated_at = ? WHERE project_id = ? AND status = 'in_progress'`, now(), projectID)
	return err
}

// RequeueAllInProgress face acelasi lucru pentru toate proiectele (la pornirea serverului).
func (s *Store) RequeueAllInProgress() error {
	_, err := s.db.Exec(`UPDATE tasks SET status = 'queued', updated_at = ? WHERE status = 'in_progress'`, now())
	return err
}
```

- [ ] **Step 4: Scrie `internal/store/runs.go`**

```go
package store

import (
	"database/sql"
	"errors"
	"time"
)

const (
	RunRunning     = "running"
	RunDone        = "done"
	RunFailed      = "failed"
	RunStopped     = "stopped"
	RunInterrupted = "interrupted"
)

// Run e o executie `claude -p` a unui agent. Logul brut e in logs/runs/<id>.jsonl.
type Run struct {
	ID        string     `json:"id"`
	AgentID   int64      `json:"agentId"`
	TaskID    int64      `json:"taskId"` // 0 = run fara task (lead sau mesaj direct)
	Status    string     `json:"status"`
	StartedAt time.Time  `json:"startedAt"`
	EndedAt   *time.Time `json:"endedAt"`
	CostUSD   float64    `json:"costUsd"`
	Error     string     `json:"error"`
}

// nullID transforma 0 in NULL pentru coloanele cu cheie straina optionala.
func nullID(id int64) any {
	if id == 0 {
		return nil
	}
	return id
}

func (s *Store) CreateRun(r *Run) error {
	ts := now()
	r.Status = RunRunning
	_, err := s.db.Exec(`INSERT INTO runs (id, agent_id, task_id, status, started_at) VALUES (?, ?, ?, ?, ?)`,
		r.ID, r.AgentID, nullID(r.TaskID), r.Status, ts)
	r.StartedAt = parseTime(ts)
	return err
}

func (s *Store) FinishRun(id, status string, costUSD float64, errText string) error {
	return mustAffect(s.db.Exec(`UPDATE runs SET status = ?, ended_at = ?, cost_usd = ?, error = ? WHERE id = ?`,
		status, now(), costUSD, errText, id))
}

func (s *Store) GetRun(id string) (Run, error) {
	var r Run
	var task sql.NullInt64
	var started string
	var ended sql.NullString
	var cost sql.NullFloat64
	err := s.db.QueryRow(`SELECT id, agent_id, task_id, status, started_at, ended_at, cost_usd, error FROM runs WHERE id = ?`, id).
		Scan(&r.ID, &r.AgentID, &task, &r.Status, &started, &ended, &cost, &r.Error)
	if errors.Is(err, sql.ErrNoRows) {
		return Run{}, ErrNotFound
	}
	if err != nil {
		return Run{}, err
	}
	r.TaskID = task.Int64
	r.StartedAt = parseTime(started)
	if ended.Valid {
		t := parseTime(ended.String)
		r.EndedAt = &t
	}
	r.CostUSD = cost.Float64
	return r, nil
}

// InterruptRunning marcheaza run-urile ramase "running" dupa un restart:
// procesele lor au murit odata cu serverul.
func (s *Store) InterruptRunning() error {
	_, err := s.db.Exec(`UPDATE runs SET status = 'interrupted', ended_at = ? WHERE status = 'running'`, now())
	return err
}
```

- [ ] **Step 5: Scrie `internal/store/messages.go`**

```go
package store

import (
	"database/sql"
	"time"
)

const (
	KindMsg    = "msg"   // mesaj scris de user sau de agent
	KindEvent  = "event" // rand informativ: "a creat T-4 pentru Rook"
	AuthorUser = "user"
)

// Message e un rand din thread-ul unui agent. Delivered=false inseamna ca
// agentul inca nu l-a primit; il primeste la urmatorul lui run.
type Message struct {
	ID        int64     `json:"id"`
	ProjectID int64     `json:"projectId"`
	AgentID   int64     `json:"agentId"`
	Kind      string    `json:"kind"`
	Author    string    `json:"author"`
	Text      string    `json:"text"`
	RunID     string    `json:"runId"`
	Delivered bool      `json:"delivered"`
	CreatedAt time.Time `json:"createdAt"`
}

const messageCols = `id, project_id, agent_id, kind, author, text, run_id, delivered, created_at`

func scanMessage(sc scanner) (Message, error) {
	var m Message
	var run sql.NullString
	var created string
	if err := sc.Scan(&m.ID, &m.ProjectID, &m.AgentID, &m.Kind, &m.Author, &m.Text, &run, &m.Delivered, &created); err != nil {
		return Message{}, err
	}
	m.RunID = run.String
	m.CreatedAt = parseTime(created)
	return m, nil
}

func (s *Store) AddMessage(m *Message) error {
	ts := now()
	var run any
	if m.RunID != "" {
		run = m.RunID
	}
	res, err := s.db.Exec(`INSERT INTO messages (project_id, agent_id, kind, author, text, run_id, delivered, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, m.ProjectID, m.AgentID, m.Kind, m.Author, m.Text, run, m.Delivered, ts)
	if err != nil {
		return err
	}
	if m.ID, err = res.LastInsertId(); err != nil {
		return err
	}
	m.CreatedAt = parseTime(ts)
	return nil
}

func (s *Store) queryMessages(where string, args ...any) ([]Message, error) {
	rows, err := s.db.Query(`SELECT `+messageCols+` FROM messages WHERE `+where+` ORDER BY id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Message{}
	for rows.Next() {
		m, err := scanMessage(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// ListMessages intoarce thread-ul agentului, cronologic.
func (s *Store) ListMessages(agentID int64) ([]Message, error) {
	return s.queryMessages(`agent_id = ?`, agentID)
}

// Undelivered intoarce ce trebuie trimis agentului la urmatorul run.
func (s *Store) Undelivered(agentID int64) ([]Message, error) {
	return s.queryMessages(`agent_id = ? AND delivered = 0`, agentID)
}

func (s *Store) HasUndelivered(agentID int64) (bool, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM messages WHERE agent_id = ? AND delivered = 0`, agentID).Scan(&n)
	return n > 0, err
}

func (s *Store) MarkDelivered(ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, id := range ids {
		if _, err := tx.Exec(`UPDATE messages SET delivered = 1 WHERE id = ?`, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}
```

- [ ] **Step 6: Ruleaza testele**

Run: `go test ./internal/store/ -v`
Expected: PASS pe toate testele (9 din Task 2 + 6 noi).

- [ ] **Step 7: Verifica si fa commit**

```bash
go vet ./... && go build -race ./...
git add -A
git commit -m "store: task-uri, run-uri si mesaje"
```

---

### Task 4: `prompt`: system prompt, mesajul turei, permisiuni, config MCP

**Files:**
- Create: `internal/prompt/prompt.go`
- Create: `internal/prompt/settings.go`
- Create: `internal/prompt/prompt_test.go`

**Interfaces:**
- Consumes (Task 2-3): `store.Project`, `store.Agent`, `store.Task`, `store.PositionLead`.
- Produces:
  - `type Member struct { Name, Role, Position, Status string }`
  - `System(p store.Project, a store.Agent, leadName string) string`
  - `Hash(s string) string` (sha256 in hex)
  - `Team(members []Member) string`
  - `WorkerTurn(members []Member, t store.Task, leadName string, userMsgs []string) string`
  - `DirectTurn(userMsgs []string) string`
  - `LeadTurn(members []Member, notes []string) string`
  - `var ValidTools = []string{"write", "shell", "git", "web"}`
  - `Settings(tools []string) string` (JSON pentru `--settings`)
  - `MCPConfig(url string) string` (JSON pentru `--mcp-config`)

- [ ] **Step 1: Scrie testele `internal/prompt/prompt_test.go`**

```go
package prompt

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"foreman/internal/store"
)

var proj = store.Project{Name: "ApiUnit", Goal: "Import din OpenAPI 3.1", Context: "Stack: PHP 8.3"}

func TestSystemWorker(t *testing.T) {
	a := store.Agent{Name: "Rook", Role: "Backend Dev", Position: store.PositionWorker,
		Background: "Stie Laravel.", DoneDef: "rezumat si fisiere schimbate"}
	got := System(proj, a, "Atlas")
	for _, want := range []string{
		"# Rol\nEsti Rook, Backend Dev, worker in proiectul ApiUnit. Raportezi lui Atlas (lead).",
		"# Background\nStie Laravel.",
		"# Cand termini un task\nApeleaza mcp__team__report_done cu: rezumat si fisiere schimbate",
		"# Proiect\nObiectiv: Import din OpenAPI 3.1\n\nContext comun:\nStack: PHP 8.3",
		"# Cum lucrezi\nLucreaza doar pe task-ul primit",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("lipseste %q din:\n%s", want, got)
		}
	}
	if strings.Contains(got, "# Instructiuni") {
		t.Error("sectiunea goala Instructiuni nu trebuia sa apara")
	}
}

func TestSystemLead(t *testing.T) {
	a := store.Agent{Name: "Atlas", Position: store.PositionLead}
	got := System(store.Project{Name: "X"}, a, "Atlas")
	for _, want := range []string{
		"Esti Atlas, lead, lead in proiectul X. Raportezi utilizatorului.",
		"# Cand termini\nCand obiectivul e atins, scrie utilizatorului un rezumat.",
		"mcp__team__create_task",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("lipseste %q din:\n%s", want, got)
		}
	}
	if strings.Contains(got, "# Proiect") {
		t.Error("proiect fara obiectiv si context: sectiunea nu trebuia sa apara")
	}
}

func TestHashStable(t *testing.T) {
	if Hash("a") != Hash("a") || Hash("a") == Hash("b") || len(Hash("a")) != 64 {
		t.Fatal("Hash nu e sha256 hex stabil")
	}
}

var team = []Member{
	{Name: "Atlas", Role: "Tech Lead", Position: "lead", Status: "Working"},
	{Name: "Rook", Role: "Backend Dev", Position: "worker", Status: "Idle"},
	{Name: "Vera", Position: "worker"},
}

func TestTeam(t *testing.T) {
	want := "Echipa:\n- Atlas: lead, Tech Lead, Working\n- Rook: worker, Backend Dev, Idle\n- Vera: worker"
	if got := Team(team); got != want {
		t.Fatalf("Team =\n%s\nvreau\n%s", got, want)
	}
}

func TestWorkerTurn(t *testing.T) {
	task := store.Task{Number: 3, Title: "Parser $ref", Description: "Doar referinte locale.", Feedback: "Lipsesc testele."}
	got := WorkerTurn(team, task, "Atlas", []string{"Grabeste-te"})
	want := Team(team) +
		"\n\nTask T-3: Parser $ref\nDoar referinte locale." +
		"\n\nFeedback de la Atlas la incercarea anterioara:\nLipsesc testele." +
		"\n\nMesaje noi de la utilizator:\n- Grabeste-te"
	if got != want {
		t.Fatalf("WorkerTurn =\n%s\nvreau\n%s", got, want)
	}
	plain := WorkerTurn(team, store.Task{Number: 1, Title: "A"}, "Atlas", nil)
	if plain != Team(team)+"\n\nTask T-1: A" {
		t.Fatalf("fara extra: %q", plain)
	}
}

func TestDirectAndLeadTurn(t *testing.T) {
	if got := DirectTurn([]string{"salut"}); got != "Mesaje de la utilizator (nu ai un task activ, raspunde direct):\n- salut" {
		t.Fatalf("DirectTurn = %q", got)
	}
	got := LeadTurn(team, []string{"Utilizatorul: fa X", "Rook a terminat T-1"})
	if got != Team(team)+"\n\nNotificari:\n- Utilizatorul: fa X\n- Rook a terminat T-1" {
		t.Fatalf("LeadTurn = %q", got)
	}
	// mesajele turei nu au voie sa inceapa cu "-": claude le-ar lua drept flag
	for _, s := range []string{got, DirectTurn([]string{"-x"}), WorkerTurn(team, store.Task{Title: "-x"}, "A", nil)} {
		if strings.HasPrefix(s, "-") {
			t.Fatalf("mesaj care incepe cu '-': %q", s)
		}
	}
}

func TestSettings(t *testing.T) {
	var cfg struct {
		Permissions struct {
			Allow []string `json:"allow"`
			Deny  []string `json:"deny"`
		} `json:"permissions"`
	}
	if err := json.Unmarshal([]byte(Settings([]string{"git", "write"})), &cfg); err != nil {
		t.Fatal(err)
	}
	allow := cfg.Permissions.Allow
	for _, want := range []string{"Read", "Glob", "Grep", "mcp__team__*", "Edit", "Write", "Bash(git commit:*)"} {
		if !slices.Contains(allow, want) {
			t.Errorf("allow nu contine %s: %v", want, allow)
		}
	}
	for _, not := range []string{"Bash", "WebSearch"} {
		if slices.Contains(allow, not) {
			t.Errorf("allow nu trebuia sa contina %s", not)
		}
	}
	for _, want := range []string{"TaskCreate", "TodoWrite", "Agent", "Bash(git push:*)", "Bash(rm -rf:*)", "Bash(npm install:*)"} {
		if !slices.Contains(cfg.Permissions.Deny, want) {
			t.Errorf("deny nu contine %s", want)
		}
	}
	// fara tool-uri: doar citire + echipa
	json.Unmarshal([]byte(Settings(nil)), &cfg)
	if len(cfg.Permissions.Allow) != 4 {
		t.Errorf("allow fara tool-uri = %v", cfg.Permissions.Allow)
	}
}

func TestMCPConfig(t *testing.T) {
	want := `{"mcpServers":{"team":{"type":"http","url":"http://127.0.0.1:7777/mcp/abc"}}}`
	if got := MCPConfig("http://127.0.0.1:7777/mcp/abc"); got != want {
		t.Fatalf("MCPConfig = %s", got)
	}
}
```

- [ ] **Step 2: Ruleaza testele ca sa vezi ca pica**

Run: `go test ./internal/prompt/`
Expected: FAIL la compilare (`undefined: System`).

- [ ] **Step 3: Scrie `internal/prompt/prompt.go`**

```go
// Package prompt construieste tot ce primeste claude la un run: system prompt-ul
// agentului, mesajul turei, permisiunile si config-ul MCP. Sunt doar functii
// pure, fara acces la baza de date, ca sa fie usor de testat.
package prompt

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"foreman/internal/store"
)

// Member e un rand din lista echipei trimisa in mesajul turei.
type Member struct {
	Name     string
	Role     string
	Position string // store.PositionLead sau store.PositionWorker
	Status   string // "Working", "Idle", "Queued"; gol = nu se afiseaza
}

const howLead = `Esti coordonatorul echipei. Analizeaza obiectivul si citeste codul daca ai nevoie de context. ` +
	`Sparge munca in task-uri mici, clare si verificabile si creeaza-le cu mcp__team__create_task, ` +
	`fiecare asignat unui worker potrivit rolului. Nu face tu munca workerilor. ` +
	`Cand primesti rezultate, verifica-le (poti citi fisierele modificate) si apeleaza ` +
	`mcp__team__accept_task sau mcp__team__send_back cu feedback concret. ` +
	`Foloseste mcp__team__list_tasks ca sa vezi starea. Nu folosi alte liste de task-uri ` +
	`in afara de tool-urile mcp__team__. Cand obiectivul e atins, scrie utilizatorului un rezumat.`

const howWorker = `Lucreaza doar pe task-ul primit si nu modifica lucruri din afara lui. ` +
	`Cand termini, apeleaza mcp__team__report_done cu rezumatul cerut. ` +
	`Daca ceva e neclar sau blocat, apeleaza mcp__team__report_done explicand problema, in loc sa ghicesti.`

// System compileaza system prompt-ul agentului. Contine doar date stabile:
// Claude Code il inregistreaza la prima cerere a sesiunii si il retrimite
// neschimbat la --resume. Starea care se schimba (echipa, task-uri) merge in
// mesajul turei.
func System(p store.Project, a store.Agent, leadName string) string {
	var b strings.Builder
	isLead := a.Position == store.PositionLead

	role := a.Role
	if role == "" {
		role = a.Position
	}
	reports := fmt.Sprintf("Raportezi lui %s (lead).", leadName)
	if isLead {
		reports = "Raportezi utilizatorului."
	}
	section(&b, "Rol", fmt.Sprintf("Esti %s, %s, %s in proiectul %s. %s", a.Name, role, a.Position, p.Name, reports))
	section(&b, "Background", a.Background)
	section(&b, "Instructiuni", a.Instructions)

	done := strings.TrimSpace(a.DoneDef)
	if isLead {
		text := "Cand obiectivul e atins, scrie utilizatorului un rezumat."
		if done != "" {
			text += " Rezumatul contine: " + done
		}
		section(&b, "Cand termini", text)
	} else {
		text := "Apeleaza mcp__team__report_done cu un rezumat al muncii."
		if done != "" {
			text = "Apeleaza mcp__team__report_done cu: " + done
		}
		section(&b, "Cand termini un task", text)
	}

	var proj []string
	if g := strings.TrimSpace(p.Goal); g != "" {
		proj = append(proj, "Obiectiv: "+g)
	}
	if c := strings.TrimSpace(p.Context); c != "" {
		proj = append(proj, "Context comun:\n"+c)
	}
	section(&b, "Proiect", strings.Join(proj, "\n\n"))

	if isLead {
		section(&b, "Cum lucrezi", howLead)
	} else {
		section(&b, "Cum lucrezi", howWorker)
	}
	return strings.TrimSpace(b.String())
}

// section scrie "# Titlu\ncontinut"; sectiunile goale se omit.
func section(b *strings.Builder, title, body string) {
	body = strings.TrimSpace(body)
	if body == "" {
		return
	}
	fmt.Fprintf(b, "# %s\n%s\n\n", title, body)
}

// Hash identifica un system prompt; daca se schimba, pornim o sesiune noua.
func Hash(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// Team formateaza lista echipei pentru mesajul turei.
func Team(members []Member) string {
	var b strings.Builder
	b.WriteString("Echipa:")
	for _, m := range members {
		fmt.Fprintf(&b, "\n- %s: %s", m.Name, m.Position)
		if m.Role != "" {
			b.WriteString(", " + m.Role)
		}
		if m.Status != "" {
			b.WriteString(", " + m.Status)
		}
	}
	return b.String()
}

// WorkerTurn e mesajul cu care porneste un worker pe un task.
func WorkerTurn(members []Member, t store.Task, leadName string, userMsgs []string) string {
	task := fmt.Sprintf("Task %s: %s", t.Ref(), t.Title)
	if d := strings.TrimSpace(t.Description); d != "" {
		task += "\n" + d
	}
	parts := []string{Team(members), task}
	if f := strings.TrimSpace(t.Feedback); f != "" {
		parts = append(parts, fmt.Sprintf("Feedback de la %s la incercarea anterioara:\n%s", leadName, f))
	}
	if len(userMsgs) > 0 {
		parts = append(parts, bullets("Mesaje noi de la utilizator:", userMsgs))
	}
	return strings.Join(parts, "\n\n")
}

// DirectTurn e mesajul pentru un worker fara task, cand utilizatorul ii scrie direct.
func DirectTurn(userMsgs []string) string {
	return bullets("Mesaje de la utilizator (nu ai un task activ, raspunde direct):", userMsgs)
}

// LeadTurn trezeste lead-ul cu tot ce s-a strans de la ultimul lui run.
func LeadTurn(members []Member, notes []string) string {
	return Team(members) + "\n\n" + bullets("Notificari:", notes)
}

func bullets(title string, items []string) string {
	var b strings.Builder
	b.WriteString(title)
	for _, it := range items {
		b.WriteString("\n- " + strings.TrimSpace(it))
	}
	return b.String()
}
```

- [ ] **Step 4: Scrie `internal/prompt/settings.go`**

```go
package prompt

import (
	"encoding/json"
	"slices"
)

// ValidTools sunt tool-urile pe care le poti bifa la un agent, in ordinea din UI.
var ValidTools = []string{"write", "shell", "git", "web"}

// toolRules traduce fiecare tool in reguli permissions.allow din Claude Code.
var toolRules = map[string][]string{
	"write": {"Edit", "Write", "NotebookEdit"},
	"shell": {"Bash"},
	"git": {"Bash(git status:*)", "Bash(git diff:*)", "Bash(git log:*)", "Bash(git add:*)",
		"Bash(git commit:*)", "Bash(git checkout -b:*)"},
	"web": {"WebSearch", "WebFetch"},
}

// alwaysAllow: citirea si tool-urile echipei sunt permise oricarui agent.
var alwaysAllow = []string{"Read", "Glob", "Grep", "mcp__team__*"}

// denyAlways blocheaza actiunile riscante (pana vin aprobarile) si tool-urile
// interne Claude Code care se suprapun cu echipa. Fara ele, lead-ul foloseste
// lista interna TaskCreate in loc de mcp__team__create_task (verificat 2026-09-29).
var denyAlways = []string{
	"TaskCreate", "TaskUpdate", "TaskList", "TaskGet", "TodoWrite", "Agent",
	"Bash(git push:*)", "Bash(rm -rf:*)", "Bash(sudo:*)",
	"Bash(npm install:*)", "Bash(composer require:*)", "Bash(go get:*)",
	"Bash(pip install:*)", "Bash(brew install:*)",
}

// Settings intoarce JSON-ul pentru --settings. In modul -p, ce lipseste din
// allow sau apare in deny e refuzat automat, fara sa intrebe pe nimeni.
func Settings(tools []string) string {
	allow := slices.Clone(alwaysAllow)
	for _, name := range ValidTools {
		if slices.Contains(tools, name) {
			allow = append(allow, toolRules[name]...)
		}
	}
	b, _ := json.Marshal(map[string]any{
		"permissions": map[string]any{"allow": allow, "deny": denyAlways},
	})
	return string(b)
}

// MCPConfig intoarce JSON-ul pentru --mcp-config: un singur server, "team",
// care e chiar aplicatia noastra.
func MCPConfig(url string) string {
	b, _ := json.Marshal(map[string]any{
		"mcpServers": map[string]any{
			"team": map[string]any{"type": "http", "url": url},
		},
	})
	return string(b)
}
```

- [ ] **Step 5: Ruleaza testele**

Run: `go test ./internal/prompt/ -v`
Expected: PASS pe toate cele 8 teste.

- [ ] **Step 6: Verifica si fa commit**

```bash
go vet ./... && go build -race ./...
git add -A
git commit -m "prompt: system prompt, mesajul turei, permisiuni, config MCP"
```

---
### Task 5: `runner` generic (Spec -> Outcome) si scoaterea modelului vechi cu foldere

**Files:**
- Modify (rescriere): `internal/runner/runner.go`
- Create: `internal/runner/tracker.go`
- Create: `internal/runner/testdata/fakeclaude.sh`
- Create: `internal/runner/runner_test.go`
- Modify (rescriere): `internal/web/server.go`
- Modify (rescriere temporara): `internal/web/static/index.html`
- Modify: `cmd/foreman/main.go`
- Modify: `internal/config/config.go` (scoate `Agents`)
- Delete: `internal/agent/`

**Interfaces:**
- Produces:
  - `type Spec struct { ID, Key, Dir string; Args []string; Prompt string }`
  - `type Outcome struct { RunID, Status, SessionID, Result string; CostUSD float64; Error string }`, unde `Status` e `"done"`, `"failed"` sau `"stopped"` (aceleasi valori ca `store.RunDone/RunFailed/RunStopped`)
  - `NewManager(claudeBin, logDir string) *Manager`
  - `(*Manager) Start(spec Spec, onDone func(Outcome)) (*Run, error)`, cu eroarea `ErrBusy` daca `Key` are deja un run activ. `onDone` e apelat o singura data, dintr-o goroutine, dupa ce procesul s-a terminat.
  - `(*Manager) Get(id string) (*Run, error)`, `(*Manager) Stop(id string) error`, `(*Manager) Shutdown()`
  - `(*Run) Subscribe() (replay [][]byte, ch chan []byte, done bool)`, `(*Run) Unsubscribe(ch)`, `(*Run) Status() string`
  - `NewID() string`
  - Evenimente proprii in stream (in afara de stream-json): `{"type":"app_start","prompt":...}`, `{"type":"app_stderr","text":...}` si `{"type":"app_done","status":...,"error":...}`
  - `web.New(runs *runner.Manager) *Server`, temporar: serveste doar UI-ul static si `GET /api/runs/{id}/events`. Se extinde in Task 10.
  - `config.Paths{Root, DB, Logs}` (fara `Agents`)

- [ ] **Step 1: Scrie claude-ul fals `internal/runner/testdata/fakeclaude.sh`**

```sh
#!/bin/sh
# claude fals pentru teste: scrie linii stream-json in functie de FAKE_MODE.
case "$FAKE_MODE" in
slow)
  echo '{"type":"system","subtype":"init","session_id":"sess-slow"}'
  sleep 30
  ;;
budget)
  echo '{"type":"system","subtype":"init","session_id":"sess-b"}'
  echo '{"type":"result","subtype":"error_max_budget_usd","is_error":true,"total_cost_usd":1.2,"errors":["Reached maximum budget ($1)"],"result":null,"session_id":"sess-b"}'
  ;;
crash)
  echo "boom" >&2
  exit 3
  ;;
*)
  echo '{"type":"system","subtype":"init","session_id":"sess-1"}'
  echo "un avertisment" >&2
  echo "{\"type\":\"result\",\"subtype\":\"success\",\"is_error\":false,\"result\":\"gata: $*\",\"total_cost_usd\":0.25,\"session_id\":\"sess-1\"}"
  ;;
esac
```

```bash
chmod +x internal/runner/testdata/fakeclaude.sh
```

- [ ] **Step 2: Scrie testele `internal/runner/runner_test.go`**

```go
//go:build !windows

package runner

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func fakeManager(t *testing.T) *Manager {
	t.Helper()
	bin, err := filepath.Abs("testdata/fakeclaude.sh")
	if err != nil {
		t.Fatal(err)
	}
	return NewManager(bin, t.TempDir())
}

// start porneste un run si intoarce un channel pe care vine Outcome-ul.
func start(t *testing.T, m *Manager, spec Spec) (*Run, chan Outcome) {
	t.Helper()
	done := make(chan Outcome, 1)
	run, err := m.Start(spec, func(o Outcome) { done <- o })
	if err != nil {
		t.Fatal(err)
	}
	return run, done
}

func wait(t *testing.T, done chan Outcome) Outcome {
	t.Helper()
	select {
	case o := <-done:
		return o
	case <-time.After(10 * time.Second):
		t.Fatal("run-ul nu s-a terminat in 10s")
		return Outcome{}
	}
}

func TestRunSuccess(t *testing.T) {
	t.Setenv("FAKE_MODE", "")
	m := fakeManager(t)
	run, done := start(t, m, Spec{ID: "r1", Key: "agent-1", Dir: t.TempDir(), Args: []string{"-p", "hello"}, Prompt: "hello"})
	o := wait(t, done)
	if o.RunID != "r1" || o.Status != "done" || o.SessionID != "sess-1" || o.CostUSD != 0.25 || !strings.Contains(o.Result, "hello") || o.Error != "" {
		t.Fatalf("outcome = %+v", o)
	}
	if run.Status() != "done" {
		t.Fatalf("Status() = %q", run.Status())
	}
	replay, _, finished := run.Subscribe()
	all := string(joinLines(replay))
	for _, want := range []string{`"type":"app_start"`, `"type":"app_stderr"`, `"type":"result"`, `"type":"app_done"`} {
		if !strings.Contains(all, want) {
			t.Errorf("istoricul nu contine %s", want)
		}
	}
	if !finished {
		t.Error("Subscribe dupa final trebuie sa intoarca done=true")
	}
	logBytes, err := os.ReadFile(filepath.Join(m.logDir, "r1.jsonl"))
	if err != nil || strings.Count(string(logBytes), "\n") < 4 {
		t.Fatalf("log incomplet: %v\n%s", err, logBytes)
	}
}

func TestRunBudgetExceeded(t *testing.T) {
	t.Setenv("FAKE_MODE", "budget")
	o := wait(t, second(start(t, fakeManager(t), Spec{Key: "a", Dir: t.TempDir()})))
	if o.Status != "failed" || !strings.Contains(o.Error, "Reached maximum budget") || o.CostUSD != 1.2 || o.Result != "" {
		t.Fatalf("outcome = %+v", o)
	}
}

func TestRunCrash(t *testing.T) {
	t.Setenv("FAKE_MODE", "crash")
	o := wait(t, second(start(t, fakeManager(t), Spec{Key: "a", Dir: t.TempDir()})))
	if o.Status != "failed" || o.Error == "" {
		t.Fatalf("outcome = %+v", o)
	}
}

func TestRunBusyAndStop(t *testing.T) {
	t.Setenv("FAKE_MODE", "slow")
	m := fakeManager(t)
	run, done := start(t, m, Spec{Key: "agent-7", Dir: t.TempDir()})
	if _, err := m.Start(Spec{Key: "agent-7", Dir: t.TempDir()}, nil); !errors.Is(err, ErrBusy) {
		t.Fatalf("al doilea run pe aceeasi cheie: %v", err)
	}
	if err := m.Stop(run.ID); err != nil {
		t.Fatal(err)
	}
	if o := wait(t, done); o.Status != "stopped" {
		t.Fatalf("dupa Stop: %+v", o)
	}
	t.Setenv("FAKE_MODE", "")
	if o := wait(t, second(start(t, m, Spec{Key: "agent-7", Dir: t.TempDir()}))); o.Status != "done" {
		t.Fatalf("dupa eliberare: %+v", o)
	}
}

func TestTrackerWithoutResult(t *testing.T) {
	var tr tracker
	tr.observe([]byte("nu e json"))
	tr.observe([]byte(`{"type":"system","session_id":"s"}`))
	o := tr.outcome("r", nil, false)
	if o.Status != "failed" || o.SessionID != "s" || o.Error == "" {
		t.Fatalf("outcome = %+v", o)
	}
}

func second(_ *Run, ch chan Outcome) chan Outcome { return ch }

func joinLines(lines [][]byte) []byte {
	var out []byte
	for _, l := range lines {
		out = append(out, l...)
		out = append(out, '\n')
	}
	return out
}
```

- [ ] **Step 3: Ruleaza testele ca sa vezi ca pica**

Run: `go test ./internal/runner/`
Expected: FAIL la compilare (`undefined: Spec`).

- [ ] **Step 4: Scrie `internal/runner/tracker.go`**

```go
package runner

import (
	"encoding/json"
	"fmt"
	"strings"
)

// tracker aduna din stream-json ce ne trebuie la finalul run-ului:
// sesiunea (pentru --resume), textul final, costul si eventualele erori.
type tracker struct {
	sessionID string
	result    string
	cost      float64
	hasResult bool
	isError   bool
	errText   string
}

func (t *tracker) observe(line []byte) {
	var ev struct {
		Type      string  `json:"type"`
		Subtype   string  `json:"subtype"`
		SessionID string  `json:"session_id"`
		Result    string  `json:"result"`
		IsError   bool    `json:"is_error"`
		Cost      float64 `json:"total_cost_usd"`
		Errors    []any   `json:"errors"`
	}
	if json.Unmarshal(line, &ev) != nil {
		return
	}
	if ev.SessionID != "" {
		t.sessionID = ev.SessionID
	}
	if ev.Type != "result" {
		return
	}
	t.hasResult = true
	t.result = ev.Result
	t.cost = ev.Cost
	t.isError = ev.IsError
	var errs []string
	for _, e := range ev.Errors {
		errs = append(errs, fmt.Sprint(e))
	}
	t.errText = strings.Join(errs, "; ")
	if t.errText == "" && ev.IsError {
		t.errText = ev.Subtype
	}
}

// outcome decide starea finala. Ordinea conteaza: oprirea manuala castiga,
// apoi eroarea raportata de claude (ex. buget depasit), apoi codul de iesire.
func (t *tracker) outcome(runID string, waitErr error, stopped bool) Outcome {
	o := Outcome{RunID: runID, Status: "done", SessionID: t.sessionID, Result: t.result, CostUSD: t.cost}
	switch {
	case stopped:
		o.Status, o.Error = "stopped", "oprit"
	case t.isError:
		o.Status, o.Error = "failed", t.errText
	case waitErr != nil:
		o.Status, o.Error = "failed", waitErr.Error()
	case !t.hasResult:
		o.Status, o.Error = "failed", "claude s-a oprit fara rezultat"
	}
	return o
}
```

- [ ] **Step 5: Rescrie `internal/runner/runner.go`**

```go
// Package runner porneste procese `claude -p`, le citeste output-ul stream-json,
// il scrie in log si il transmite live abonatilor (SSE). Nu stie nimic despre
// proiecte sau agenti: primeste argumentele gata construite.
package runner

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"
)

var (
	ErrBusy     = errors.New("agentul are deja un run activ")
	ErrNotFound = errors.New("run inexistent")
)

// Spec descrie un run: ce argumente primeste claude si unde ruleaza.
type Spec struct {
	ID     string   // id-ul run-ului (NewID()); daca e gol se genereaza
	Key    string   // un singur run activ per cheie (ex. "agent-12")
	Dir    string   // working directory
	Args   []string // argumentele pentru claude, fara binar
	Prompt string   // mesajul turei, doar pentru evenimentul de start
}

// Outcome e rezultatul unui run terminat, extras din stream-json.
type Outcome struct {
	RunID     string
	Status    string // "done", "failed" sau "stopped"
	SessionID string // pentru --resume
	Result    string // textul final al agentului
	CostUSD   float64
	Error     string
}

// Run este o singura executie `claude -p`. Tine istoricul evenimentelor in
// memorie, ca un client SSE care se conecteaza tarziu sa primeasca tot.
type Run struct {
	ID        string    `json:"id"`
	Key       string    `json:"key"`
	Prompt    string    `json:"prompt"`
	StartedAt time.Time `json:"startedAt"`

	mu      sync.Mutex
	history [][]byte
	subs    map[chan []byte]struct{}
	done    bool
	status  string
	stopped bool
	log     *os.File
	cancel  context.CancelFunc
}

// publish adauga un eveniment in istoric si in log si il trimite abonatilor.
// Se apeleaza din doua goroutine (stdout si stderr), de aceea e sub mutex.
func (r *Run) publish(line []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.history = append(r.history, line)
	if r.log != nil {
		r.log.Write(line)
		r.log.Write([]byte("\n"))
	}
	for ch := range r.subs {
		select {
		case ch <- line:
		default:
			// clientul nu tine pasul; un refresh reincarca tot istoricul
		}
	}
}

// Subscribe intoarce istoricul de pana acum si un channel pentru ce urmeaza.
// Daca run-ul s-a terminat deja, channel-ul e nil si done e true.
func (r *Run) Subscribe() (replay [][]byte, ch chan []byte, done bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	replay = append([][]byte(nil), r.history...)
	if r.done {
		return replay, nil, true
	}
	ch = make(chan []byte, 1024)
	r.subs[ch] = struct{}{}
	return replay, ch, false
}

func (r *Run) Unsubscribe(ch chan []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.subs[ch]; ok {
		delete(r.subs, ch)
		close(ch)
	}
}

func (r *Run) finish(status string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.done = true
	r.status = status
	for ch := range r.subs {
		delete(r.subs, ch)
		close(ch)
	}
	if r.log != nil {
		r.log.Close()
		r.log = nil
	}
}

func (r *Run) Status() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.done {
		return "running"
	}
	return r.status
}

// Manager tine evidenta run-urilor din procesul curent.
type Manager struct {
	claudeBin string
	logDir    string

	mu     sync.Mutex
	runs   map[string]*Run
	active map[string]string // Key -> id-ul run-ului activ
}

func NewManager(claudeBin, logDir string) *Manager {
	return &Manager{
		claudeBin: claudeBin,
		logDir:    logDir,
		runs:      map[string]*Run{},
		active:    map[string]string{},
	}
}

func (m *Manager) Get(id string) (*Run, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.runs[id]
	if !ok {
		return nil, ErrNotFound
	}
	return r, nil
}

// Start porneste claude cu argumentele din spec. onDone (poate fi nil) e
// apelat o singura data, dintr-o goroutine, dupa ce procesul s-a terminat.
func (m *Manager) Start(spec Spec, onDone func(Outcome)) (*Run, error) {
	if spec.ID == "" {
		spec.ID = NewID()
	}
	m.mu.Lock()
	if _, busy := m.active[spec.Key]; busy {
		m.mu.Unlock()
		return nil, ErrBusy
	}
	m.active[spec.Key] = spec.ID // rezervam locul inainte sa deblocam
	m.mu.Unlock()

	release := func() {
		m.mu.Lock()
		delete(m.active, spec.Key)
		m.mu.Unlock()
	}

	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, m.claudeBin, spec.Args...)
	cmd.Dir = spec.Dir
	setProcessGroup(cmd)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		release()
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		cancel()
		release()
		return nil, err
	}
	logFile, err := os.Create(filepath.Join(m.logDir, spec.ID+".jsonl"))
	if err != nil {
		cancel()
		release()
		return nil, err
	}

	run := &Run{
		ID:        spec.ID,
		Key:       spec.Key,
		Prompt:    spec.Prompt,
		StartedAt: time.Now(),
		subs:      map[chan []byte]struct{}{},
		log:       logFile,
		cancel:    cancel,
	}

	if err := cmd.Start(); err != nil {
		logFile.Close()
		cancel()
		release()
		return nil, fmt.Errorf("nu pot porni claude: %w", err)
	}

	m.mu.Lock()
	m.runs[run.ID] = run
	m.mu.Unlock()

	run.publish(mustJSON(map[string]any{"type": "app_start", "prompt": spec.Prompt}))
	go m.pump(run, cmd, stdout, stderr, onDone)
	return run, nil
}

func (m *Manager) pump(run *Run, cmd *exec.Cmd, stdout, stderr io.Reader, onDone func(Outcome)) {
	var tr tracker // folosit doar de goroutine-ul de stdout, citit dupa wg.Wait
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		sc := bufio.NewScanner(stdout)
		// o linie stream-json poate depasi 64KB (ex. un fisier citit intreg)
		sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
		for sc.Scan() {
			line := append([]byte(nil), sc.Bytes()...)
			tr.observe(line)
			run.publish(line)
		}
	}()

	go func() {
		defer wg.Done()
		sc := bufio.NewScanner(stderr)
		for sc.Scan() {
			run.publish(mustJSON(map[string]any{"type": "app_stderr", "text": sc.Text()}))
		}
	}()

	wg.Wait()
	waitErr := cmd.Wait()

	run.mu.Lock()
	stopped := run.stopped
	run.mu.Unlock()

	out := tr.outcome(run.ID, waitErr, stopped)
	done := map[string]any{"type": "app_done", "status": out.Status}
	if out.Error != "" {
		done["error"] = out.Error
	}
	run.publish(mustJSON(done))
	run.finish(out.Status)

	m.mu.Lock()
	if m.active[run.Key] == run.ID {
		delete(m.active, run.Key)
	}
	m.mu.Unlock()

	if onDone != nil {
		onDone(out)
	}
}

// Stop opreste run-ul (tot process group-ul). Rezultatul vine prin onDone.
func (m *Manager) Stop(id string) error {
	run, err := m.Get(id)
	if err != nil {
		return err
	}
	run.mu.Lock()
	run.stopped = true
	run.mu.Unlock()
	run.cancel()
	return nil
}

// Shutdown opreste toate run-urile active (la Ctrl+C).
func (m *Manager) Shutdown() {
	m.mu.Lock()
	ids := make([]string, 0, len(m.active))
	for _, id := range m.active {
		ids = append(ids, id)
	}
	m.mu.Unlock()
	for _, id := range ids {
		m.Stop(id)
	}
}

// NewID genereaza un id de run sortabil dupa timp: 20260929-130056-af8b6e.
func NewID() string {
	b := make([]byte, 3)
	rand.Read(b)
	return time.Now().Format("20060102-150405") + "-" + hex.EncodeToString(b)
}

func mustJSON(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}
```

- [ ] **Step 6: Ruleaza testele runner-ului**

Run: `go test ./internal/runner/ -v -race`
Expected: PASS pe toate cele 5 teste. `TestRunBusyAndStop` dureaza sub 6s.

- [ ] **Step 7: Scoate modelul vechi: config, web, main**

```bash
git rm -r internal/agent
```

In `internal/config/config.go`, scoate campul `Agents`, linia `Agents: filepath.Join(root, "agents"),` si `p.Agents` din lista din `Ensure`:

```go
type Paths struct {
	Root string // ~/.foreman
	DB   string // ~/.foreman/foreman.db
	Logs string // ~/.foreman/logs/runs
}
```

```go
	return Paths{
		Root: root,
		DB:   filepath.Join(root, AppName+".db"),
		Logs: filepath.Join(root, "logs", "runs"),
	}, nil
```

```go
	for _, d := range []string{p.Root, p.Logs} {
```

Rescrie `internal/web/server.go` (temporar: doar static si stream-ul unui run; API-ul complet vine in Task 10):

```go
package web

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"time"

	"foreman/internal/runner"
)

//go:embed static
var staticFS embed.FS

type Server struct {
	runs *runner.Manager
}

func New(runs *runner.Manager) *Server {
	return &Server{runs: runs}
}

func (s *Server) Handler() http.Handler {
	static, _ := fs.Sub(staticFS, "static")
	mux := http.NewServeMux()
	mux.Handle("GET /", http.FileServerFS(static))
	mux.HandleFunc("GET /api/runs/{id}/events", s.streamRun)
	return mux
}

// streamRun trimite evenimentele unui run prin Server-Sent Events:
// mai intai tot istoricul, apoi live pana se termina run-ul.
func (s *Server) streamRun(w http.ResponseWriter, r *http.Request) {
	run, err := s.runs.Get(r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeErr(w, http.StatusInternalServerError, "streaming nesuportat")
		return
	}
	sseHeaders(w)

	replay, ch, done := run.Subscribe()
	for _, line := range replay {
		fmt.Fprintf(w, "data: %s\n\n", line)
	}
	flusher.Flush()
	if done {
		return
	}
	defer run.Unsubscribe(ch)

	// ping periodic, ca browserul sa nu inchida conexiunea
	ping := time.NewTicker(15 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ping.C:
			fmt.Fprint(w, ": ping\n\n")
			flusher.Flush()
		case line, ok := <-ch:
			if !ok {
				return
			}
			fmt.Fprintf(w, "data: %s\n\n", line)
			flusher.Flush()
		}
	}
}

func sseHeaders(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
```

Rescrie `internal/web/static/index.html` (placeholder pana la Task 11):

```html
<!doctype html>
<html lang="ro">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Foreman</title>
</head>
<body style="background:#121514;color:#ecebe6;font-family:system-ui;padding:40px">
<h1>Foreman</h1>
<p>UI-ul nou e in lucru.</p>
</body>
</html>
```

In `cmd/foreman/main.go`, in `start()`, sterge blocul `agent.EnsureDemo(...)`, importul `foreman/internal/agent` si linia `log.Printf("agenti: %s", paths.Agents)`. Constructorul serverului devine:

```go
		Handler:           web.New(mgr).Handler(),
```

- [ ] **Step 8: Verifica tot**

Run: `go vet ./... && go build -race ./... && go test -race ./...`
Expected: totul compileaza si toate testele trec (config, store, prompt, runner).

- [ ] **Step 9: Commit**

```bash
git add -A
git commit -m "runner generic (Spec -> Outcome), scos modelul vechi cu foldere"
```

---

### Task 6: `events`: pub/sub per proiect pentru SSE

**Files:**
- Create: `internal/events/hub.go`
- Create: `internal/events/hub_test.go`

**Interfaces:**
- Produces:
  - `NewHub() *Hub`
  - `(*Hub) Publish(projectID int64, typ string, data any)`, care nu blocheaza niciodata. Payload-ul e JSON `{"type": typ, "data": data}`.
  - `(*Hub) Subscribe(projectID int64) (<-chan []byte, func())`. Functia intoarsa dezaboneaza si inchide channel-ul. Se poate apela de mai multe ori.

- [ ] **Step 1: Scrie testele `internal/events/hub_test.go`**

```go
package events

import (
	"encoding/json"
	"testing"
	"time"
)

func TestPublishOnlyToProjectSubscribers(t *testing.T) {
	h := NewHub()
	a, cancelA := h.Subscribe(1)
	defer cancelA()
	b, cancelB := h.Subscribe(2)
	defer cancelB()

	h.Publish(1, "task", map[string]int{"id": 7})

	select {
	case raw := <-a:
		var ev struct {
			Type string         `json:"type"`
			Data map[string]int `json:"data"`
		}
		if err := json.Unmarshal(raw, &ev); err != nil || ev.Type != "task" || ev.Data["id"] != 7 {
			t.Fatalf("eveniment = %s (%v)", raw, err)
		}
	case <-time.After(time.Second):
		t.Fatal("abonatul proiectului 1 nu a primit nimic")
	}
	select {
	case raw := <-b:
		t.Fatalf("abonatul proiectului 2 a primit %s", raw)
	default:
	}
}

func TestCancelClosesChannel(t *testing.T) {
	h := NewHub()
	ch, cancel := h.Subscribe(1)
	cancel()
	cancel() // a doua oara nu trebuie sa dea panic
	if _, ok := <-ch; ok {
		t.Fatal("channel-ul trebuia inchis")
	}
	h.Publish(1, "task", nil) // fara abonati: nu trebuie sa dea panic
}

func TestSlowSubscriberDoesNotBlock(t *testing.T) {
	h := NewHub()
	ch, cancel := h.Subscribe(1)
	defer cancel()
	finished := make(chan struct{})
	go func() {
		for i := 0; i < buffer*3; i++ { // nimeni nu citeste
			h.Publish(1, "message", i)
		}
		close(finished)
	}()
	select {
	case <-finished:
	case <-time.After(2 * time.Second):
		t.Fatal("Publish a blocat din cauza unui abonat lent")
	}
	if len(ch) != buffer {
		t.Fatalf("in buffer: %d, vreau %d", len(ch), buffer)
	}
}
```

- [ ] **Step 2: Ruleaza testele ca sa vezi ca pica**

Run: `go test ./internal/events/`
Expected: FAIL la compilare (`undefined: NewHub`).

- [ ] **Step 3: Scrie `internal/events/hub.go`**

```go
// Package events trimite notificari live catre browserele deschise pe un
// proiect: task schimbat, mesaj nou, agent pornit/oprit. Transportul e SSE (in web).
package events

import (
	"encoding/json"
	"sync"
)

// buffer: cate evenimente poate avea in asteptare un abonat inainte sa pierdem.
const buffer = 64

type Hub struct {
	mu   sync.Mutex
	subs map[int64]map[chan []byte]struct{}
}

func NewHub() *Hub {
	return &Hub{subs: map[int64]map[chan []byte]struct{}{}}
}

// Publish trimite {type, data} abonatilor proiectului. Nu blocheaza niciodata:
// un abonat lent pierde evenimentul, iar UI-ul oricum reincarca starea completa.
func (h *Hub) Publish(projectID int64, typ string, data any) {
	payload, err := json.Marshal(map[string]any{"type": typ, "data": data})
	if err != nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.subs[projectID] {
		select {
		case ch <- payload:
		default:
		}
	}
}

// Subscribe inregistreaza un abonat. Functia intoarsa il scoate si inchide
// channel-ul; e sigur sa fie apelata de mai multe ori.
func (h *Hub) Subscribe(projectID int64) (<-chan []byte, func()) {
	ch := make(chan []byte, buffer)
	h.mu.Lock()
	if h.subs[projectID] == nil {
		h.subs[projectID] = map[chan []byte]struct{}{}
	}
	h.subs[projectID][ch] = struct{}{}
	h.mu.Unlock()

	var once sync.Once
	cancel := func() {
		once.Do(func() {
			h.mu.Lock()
			delete(h.subs[projectID], ch)
			if len(h.subs[projectID]) == 0 {
				delete(h.subs, projectID)
			}
			h.mu.Unlock()
			// inchidem dupa ce l-am scos, sub lock nu se mai trimite nimic pe el
			close(ch)
		})
	}
	return ch, cancel
}
```

- [ ] **Step 4: Ruleaza testele**

Run: `go test ./internal/events/ -v -race`
Expected: PASS pe toate cele 3 teste.

- [ ] **Step 5: Commit**

```bash
go vet ./... && go build -race ./...
git add -A
git commit -m "events: pub/sub per proiect pentru SSE"
```

---

### Task 7: `mcp`: server JSON-RPC cu tool-urile echipei

**Files:**
- Create: `internal/mcp/server.go`
- Create: `internal/mcp/tools.go`
- Create: `internal/mcp/server_test.go`

**Interfaces:**
- Produces:
  - `type Caller struct { ProjectID, AgentID int64; RunID string; TaskID int64; Lead bool }`
  - `type Backend interface`, cu metodele:
    - `Resolve(token string) (Caller, bool)`
    - `CreateTask(c Caller, title, description, assignee string) (string, error)`
    - `ListTasks(c Caller, status string) (string, error)`
    - `AcceptTask(c Caller, task, note string) (string, error)`
    - `SendBack(c Caller, task, feedback string) (string, error)`
    - `ReportDone(c Caller, summary string) (string, error)`
  - `NewServer(b Backend) *Server`, care implementeaza `http.Handler`. Se monteaza pe `POST /mcp/{token}` si `GET /mcp/{token}`, citeste `r.PathValue("token")`, raspunde 404 pentru token necunoscut si 405 pentru orice alta metoda HTTP decat POST.
  - Numele tool-urilor: lead-ul are `create_task`, `list_tasks`, `accept_task`, `send_back`; worker-ul are `report_done`.

- [ ] **Step 1: Scrie testele `internal/mcp/server_test.go`**

```go
package mcp

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type fakeBackend struct {
	callers map[string]Caller
	calls   []string
	err     error
}

func (f *fakeBackend) Resolve(tok string) (Caller, bool) { c, ok := f.callers[tok]; return c, ok }
func (f *fakeBackend) CreateTask(c Caller, title, desc, assignee string) (string, error) {
	f.calls = append(f.calls, "create_task|"+title+"|"+desc+"|"+assignee)
	return "Creat T-1 pentru " + assignee, f.err
}
func (f *fakeBackend) ListTasks(c Caller, status string) (string, error) {
	f.calls = append(f.calls, "list_tasks|"+status)
	return "Nu exista task-uri.", f.err
}
func (f *fakeBackend) AcceptTask(c Caller, task, note string) (string, error) {
	f.calls = append(f.calls, "accept_task|"+task+"|"+note)
	return "ok", f.err
}
func (f *fakeBackend) SendBack(c Caller, task, feedback string) (string, error) {
	f.calls = append(f.calls, "send_back|"+task+"|"+feedback)
	return "ok", f.err
}
func (f *fakeBackend) ReportDone(c Caller, summary string) (string, error) {
	f.calls = append(f.calls, "report_done|"+summary)
	return "ok", f.err
}

func setup(t *testing.T) (*fakeBackend, *httptest.Server) {
	t.Helper()
	fb := &fakeBackend{callers: map[string]Caller{
		"lead":   {ProjectID: 1, AgentID: 1, Lead: true},
		"worker": {ProjectID: 1, AgentID: 2, TaskID: 5},
	}}
	mux := http.NewServeMux()
	srv := NewServer(fb)
	mux.Handle("POST /mcp/{token}", srv)
	mux.Handle("GET /mcp/{token}", srv)
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return fb, ts
}

// rpc trimite un request JSON-RPC si intoarce statusul HTTP si raspunsul decodat.
func rpc(t *testing.T, ts *httptest.Server, token, body string) (int, map[string]any) {
	t.Helper()
	res, err := http.Post(ts.URL+"/mcp/"+token, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	var out map[string]any
	if len(raw) > 0 && res.Header.Get("Content-Type") == "application/json" {
		if err := json.Unmarshal(raw, &out); err != nil {
			t.Fatalf("raspuns invalid %s: %v", raw, err)
		}
	}
	return res.StatusCode, out
}

func result(t *testing.T, resp map[string]any) map[string]any {
	t.Helper()
	r, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("fara result: %v", resp)
	}
	return r
}

// toolText intoarce textul si isError dintr-un raspuns tools/call.
func toolText(t *testing.T, resp map[string]any) (string, bool) {
	t.Helper()
	r := result(t, resp)
	content := r["content"].([]any)
	return content[0].(map[string]any)["text"].(string), r["isError"].(bool)
}

func TestInitializeEchoesProtocolVersion(t *testing.T) {
	_, ts := setup(t)
	code, resp := rpc(t, ts, "lead", `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25"}}`)
	r := result(t, resp)
	if code != 200 || r["protocolVersion"] != "2025-11-25" || resp["id"].(float64) != 1 {
		t.Fatalf("%d %v", code, resp)
	}
	if _, ok := r["capabilities"].(map[string]any)["tools"]; !ok {
		t.Fatalf("lipseste capabilities.tools: %v", r)
	}
}

func TestNotificationAccepted(t *testing.T) {
	_, ts := setup(t)
	if code, _ := rpc(t, ts, "lead", `{"jsonrpc":"2.0","method":"notifications/initialized"}`); code != http.StatusAccepted {
		t.Fatalf("notificare: %d", code)
	}
}

func TestUnknownTokenAndGet(t *testing.T) {
	_, ts := setup(t)
	if code, _ := rpc(t, ts, "nimeni", `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`); code != http.StatusNotFound {
		t.Fatalf("token necunoscut: %d", code)
	}
	res, err := http.Get(ts.URL + "/mcp/lead")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("GET: %d", res.StatusCode)
	}
}

func TestUnknownMethod(t *testing.T) {
	_, ts := setup(t)
	_, resp := rpc(t, ts, "lead", `{"jsonrpc":"2.0","id":"server-discover-probe-1","method":"server/discover"}`)
	e, ok := resp["error"].(map[string]any)
	if !ok || e["code"].(float64) != -32601 || resp["id"] != "server-discover-probe-1" {
		t.Fatalf("server/discover: %v", resp)
	}
}

func TestToolsListByRole(t *testing.T) {
	_, ts := setup(t)
	names := func(token string) []string {
		_, resp := rpc(t, ts, token, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)
		var out []string
		for _, tool := range result(t, resp)["tools"].([]any) {
			m := tool.(map[string]any)
			if _, ok := m["inputSchema"].(map[string]any); !ok {
				t.Fatalf("tool fara inputSchema: %v", m)
			}
			out = append(out, m["name"].(string))
		}
		return out
	}
	if got := strings.Join(names("lead"), ","); got != "create_task,list_tasks,accept_task,send_back" {
		t.Fatalf("lead: %s", got)
	}
	if got := strings.Join(names("worker"), ","); got != "report_done" {
		t.Fatalf("worker: %s", got)
	}
}

func TestCallCreateTask(t *testing.T) {
	fb, ts := setup(t)
	_, resp := rpc(t, ts, "lead", `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"create_task","arguments":{"title":"Parser","description":"d","assignee":"Rook"}}}`)
	text, isErr := toolText(t, resp)
	if isErr || text != "Creat T-1 pentru Rook" {
		t.Fatalf("%q %v", text, isErr)
	}
	if len(fb.calls) != 1 || fb.calls[0] != "create_task|Parser|d|Rook" {
		t.Fatalf("apeluri: %v", fb.calls)
	}
}

func TestWorkerCannotCallLeadTool(t *testing.T) {
	fb, ts := setup(t)
	_, resp := rpc(t, ts, "worker", `{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"create_task","arguments":{"title":"x","assignee":"Rook"}}}`)
	if text, isErr := toolText(t, resp); !isErr || !strings.Contains(text, "nu e disponibil") {
		t.Fatalf("%q %v", text, isErr)
	}
	if len(fb.calls) != 0 {
		t.Fatalf("backend-ul nu trebuia apelat: %v", fb.calls)
	}
}

func TestBackendErrorBecomesToolError(t *testing.T) {
	fb, ts := setup(t)
	fb.err = errors.New("nu exista task-ul T-9")
	_, resp := rpc(t, ts, "worker", `{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"report_done","arguments":{"summary":"gata"}}}`)
	if text, isErr := toolText(t, resp); !isErr || text != "nu exista task-ul T-9" {
		t.Fatalf("%q %v", text, isErr)
	}
}

func TestNumericTaskArgument(t *testing.T) {
	fb, ts := setup(t)
	rpc(t, ts, "lead", `{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"accept_task","arguments":{"task":3}}}`)
	if len(fb.calls) != 1 || fb.calls[0] != "accept_task|3|" {
		t.Fatalf("apeluri: %v", fb.calls)
	}
}

func TestInvalidJSON(t *testing.T) {
	_, ts := setup(t)
	_, resp := rpc(t, ts, "lead", `{nu e json`)
	if e, ok := resp["error"].(map[string]any); !ok || e["code"].(float64) != -32700 {
		t.Fatalf("JSON invalid: %v", resp)
	}
}
```

- [ ] **Step 2: Ruleaza testele ca sa vezi ca pica**

Run: `go test ./internal/mcp/`
Expected: FAIL la compilare (`undefined: NewServer`).

- [ ] **Step 3: Scrie `internal/mcp/tools.go`**

```go
package mcp

// Tool e definitia unui tool asa cum o trimitem la tools/list.
type Tool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

func schema(required []string, props map[string]any) map[string]any {
	return map[string]any{"type": "object", "properties": props, "required": required}
}

func str(desc string) map[string]any {
	return map[string]any{"type": "string", "description": desc}
}

var leadTools = []Tool{
	{
		Name:        "create_task",
		Description: "Creeaza un task nou in Queued si il asigneaza unui worker din echipa. Worker-ul il primeste automat.",
		InputSchema: schema([]string{"title", "assignee"}, map[string]any{
			"title":       str("Titlu scurt si clar"),
			"description": str("Ce trebuie facut, criterii de acceptare, fisiere relevante"),
			"assignee":    str("Numele worker-ului, exact ca in lista echipei"),
		}),
	},
	{
		Name:        "list_tasks",
		Description: "Lista task-urilor proiectului, cu status, worker si rezultat.",
		InputSchema: schema([]string{}, map[string]any{
			"status": map[string]any{
				"type":        "string",
				"enum":        []string{"queued", "in_progress", "review", "done", "blocked"},
				"description": "Optional: doar task-urile cu acest status",
			},
		}),
	},
	{
		Name:        "accept_task",
		Description: "Accepta un task din Review (sau blocat) si il muta in Done.",
		InputSchema: schema([]string{"task"}, map[string]any{
			"task": str("Task-ul, ex. T-3"),
			"note": str("Optional: o nota pentru istoric"),
		}),
	},
	{
		Name:        "send_back",
		Description: "Trimite un task din Review inapoi worker-ului, cu feedback concret. Dupa 3 trimiteri task-ul se blocheaza si decide utilizatorul.",
		InputSchema: schema([]string{"task", "feedback"}, map[string]any{
			"task":     str("Task-ul, ex. T-3"),
			"feedback": str("Ce trebuie schimbat, concret"),
		}),
	},
}

var workerTools = []Tool{
	{
		Name:        "report_done",
		Description: "Raporteaza lead-ului ca ai terminat task-ul curent (sau ca esti blocat). Task-ul trece in Review.",
		InputSchema: schema([]string{"summary"}, map[string]any{
			"summary": str("Rezumatul cerut in definition of done, sau problema care te blocheaza"),
		}),
	},
}

func toolsFor(c Caller) []Tool {
	if c.Lead {
		return leadTools
	}
	return workerTools
}

func allowed(c Caller, name string) bool {
	for _, t := range toolsFor(c) {
		if t.Name == name {
			return true
		}
	}
	return false
}
```

- [ ] **Step 4: Scrie `internal/mcp/server.go`**

```go
// Package mcp implementeaza partea de server MCP de care au nevoie agentii:
// JSON-RPC 2.0 peste HTTP (request JSON, raspuns JSON, fara stream), cu
// metodele initialize, tools/list si tools/call. Logica tool-urilor e in Backend.
package mcp

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
)

// Caller identifica agentul care apeleaza, pe baza token-ului din URL.
type Caller struct {
	ProjectID int64
	AgentID   int64
	RunID     string
	TaskID    int64 // 0 = run fara task
	Lead      bool
}

// Backend face munca propriu-zisa. Erorile intoarse ajung la agent ca
// rezultat cu isError, deci trebuie sa fie mesaje clare pentru el.
type Backend interface {
	Resolve(token string) (Caller, bool)
	CreateTask(c Caller, title, description, assignee string) (string, error)
	ListTasks(c Caller, status string) (string, error)
	AcceptTask(c Caller, task, note string) (string, error)
	SendBack(c Caller, task, feedback string) (string, error)
	ReportDone(c Caller, summary string) (string, error)
}

type Server struct {
	backend Backend
}

func NewServer(b Backend) *Server { return &Server{backend: b} }

// fallbackProtocol se foloseste doar daca clientul nu cere o versiune.
const fallbackProtocol = "2025-06-18"

type request struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	caller, ok := s.backend.Resolve(r.PathValue("token"))
	if !ok {
		http.Error(w, "token necunoscut", http.StatusNotFound)
		return
	}
	// Claude deschide si un GET pentru mesaje server -> client; nu avem asa ceva.
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "doar POST", http.StatusMethodNotAllowed)
		return
	}

	var req request
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		writeJSON(w, response{JSONRPC: "2.0", ID: json.RawMessage("null"), Error: &rpcError{-32700, "JSON invalid"}})
		return
	}
	// notificare (fara id): nu se asteapta raspuns
	if len(req.ID) == 0 || string(req.ID) == "null" {
		w.WriteHeader(http.StatusAccepted)
		return
	}

	resp := response{JSONRPC: "2.0", ID: req.ID}
	switch req.Method {
	case "initialize":
		resp.Result = initialize(req.Params)
	case "ping":
		resp.Result = map[string]any{}
	case "tools/list":
		resp.Result = map[string]any{"tools": toolsFor(caller)}
	case "tools/call":
		resp.Result = s.call(caller, req.Params)
	default:
		resp.Error = &rpcError{-32601, "metoda necunoscuta: " + req.Method}
	}
	writeJSON(w, resp)
}

func initialize(params json.RawMessage) any {
	var p struct {
		ProtocolVersion string `json:"protocolVersion"`
	}
	json.Unmarshal(params, &p)
	version := p.ProtocolVersion
	if version == "" {
		version = fallbackProtocol
	}
	return map[string]any{
		"protocolVersion": version,
		"capabilities":    map[string]any{"tools": map[string]any{}},
		"serverInfo":      map[string]any{"name": "team", "version": "1"},
	}
}

func (s *Server) call(c Caller, params json.RawMessage) any {
	var p struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return toolResult("parametri invalizi", true)
	}
	if !allowed(c, p.Name) {
		return toolResult("tool-ul "+p.Name+" nu e disponibil pentru rolul tau", true)
	}
	arg := func(key string) string { return argString(p.Arguments, key) }

	var text string
	var err error
	switch p.Name {
	case "create_task":
		text, err = s.backend.CreateTask(c, arg("title"), arg("description"), arg("assignee"))
	case "list_tasks":
		text, err = s.backend.ListTasks(c, arg("status"))
	case "accept_task":
		text, err = s.backend.AcceptTask(c, arg("task"), arg("note"))
	case "send_back":
		text, err = s.backend.SendBack(c, arg("task"), arg("feedback"))
	case "report_done":
		text, err = s.backend.ReportDone(c, arg("summary"))
	}
	if err != nil {
		return toolResult(err.Error(), true)
	}
	return toolResult(text, false)
}

func toolResult(text string, isError bool) map[string]any {
	return map[string]any{
		"content": []map[string]any{{"type": "text", "text": text}},
		"isError": isError,
	}
}

// argString citeste un argument ca text. Agentii trimit uneori numere
// (ex. "task": 3 in loc de "T-3").
func argString(args map[string]any, key string) string {
	switch v := args[key].(type) {
	case nil:
		return ""
	case string:
		return v
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	default:
		return fmt.Sprint(v)
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}
```

- [ ] **Step 5: Ruleaza testele**

Run: `go test ./internal/mcp/ -v -race`
Expected: PASS pe toate cele 9 teste.

- [ ] **Step 6: Commit**

```bash
go vet ./... && go build -race ./...
git add -A
git commit -m "mcp: server JSON-RPC cu tool-urile echipei"
```

---
### Task 8: `team`: nucleul si tool-urile MCP (mcp.Backend)

**Files:**
- Create: `internal/team/team.go`
- Create: `internal/team/tools.go`
- Create: `internal/team/fake_test.go`
- Create: `internal/team/tools_test.go`

**Interfaces:**
- Consumes:
  - `store` (Task 2-3), cu `CreateTask`, `GetTask`, `GetTaskByNumber`, `ListTasks`, `SaveTask`, `FindAgentByName`, `GetAgent`, `Lead`, `ListAgents`, `AddMessage`, `ListMessages`, `Undelivered` si `HasUndelivered`;
  - `runner.Spec`, `runner.Outcome`, `runner.Run` (Task 5);
  - `events.Hub` (Task 6);
  - `mcp.Caller`, `mcp.Backend` (Task 7).
- Produces:
  - `type Starter interface { Start(spec runner.Spec, onDone func(runner.Outcome)) (*runner.Run, error); Stop(runID string) error }`
  - `New(st *store.Store, runs Starter, hub *events.Hub, baseURL string) *Team`
  - `(*Team) Resolve(token string) (mcp.Caller, bool)`
  - `(*Team) Kick(projectID int64)`
  - `(*Team) ActiveRunID(agentID int64) string`, `(*Team) ProjectActive(projectID int64) bool`
  - `(*Team) AgentStatus(a store.Agent, tasks []store.Task) (status string, current *store.Task)`, cu statusurile `StatusWorking = "Working"`, `StatusQueued = "Queued"`, `StatusIdle = "Idle"`
  - `MaxSendBacks = 3`
  - metodele `mcp.Backend`: `CreateTask`, `ListTasks`, `AcceptTask`, `SendBack`, `ReportDone`
  - interne, pentru Task 9: campurile `work sync.Mutex`, `mu sync.Mutex`, `active map[int64]activeRun`, `tokens map[string]mcp.Caller`, `pending map[int64]bool`, `wake chan struct{}`; functiile `isActive(agentID) bool`, `emit(projectID, typ, data)`, `note(projectID, agentID, text, delivered) error`, `statusOf(a, tasks, pending) string`, `newToken() string`, `clip(s string) string`
  - pentru teste (Task 9 si 10 le refolosesc): `fakeStarter`, `setup(t) *fixture`, `argValue`, `hasFlag`

- [ ] **Step 1: Scrie `internal/team/fake_test.go`** (infrastructura de test, folosita si in Task 9)

```go
package team

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"foreman/internal/events"
	"foreman/internal/mcp"
	"foreman/internal/runner"
	"foreman/internal/store"
)

// fakeStarter inlocuieste runner-ul: inregistreaza ce s-ar fi pornit, iar
// testul decide cand si cum se termina fiecare run (finish).
type fakeStarter struct {
	mu      sync.Mutex
	specs   []runner.Spec
	onDone  map[string]func(runner.Outcome)
	stopped []string
}

func newFakeStarter() *fakeStarter {
	return &fakeStarter{onDone: map[string]func(runner.Outcome){}}
}

func (f *fakeStarter) Start(spec runner.Spec, onDone func(runner.Outcome)) (*runner.Run, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.specs = append(f.specs, spec)
	f.onDone[spec.ID] = onDone
	return nil, nil
}

func (f *fakeStarter) Stop(id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stopped = append(f.stopped, id)
	return nil
}

func (f *fakeStarter) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.specs)
}

// last intoarce ultimul run pornit pentru agent.
func (f *fakeStarter) last(t *testing.T, a store.Agent) runner.Spec {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	key := fmt.Sprintf("agent-%d", a.ID)
	for i := len(f.specs) - 1; i >= 0; i-- {
		if f.specs[i].Key == key {
			return f.specs[i]
		}
	}
	t.Fatalf("niciun run pentru %s", a.Name)
	return runner.Spec{}
}

// finish simuleaza terminarea procesului claude.
func (f *fakeStarter) finish(id string, o runner.Outcome) {
	f.mu.Lock()
	cb := f.onDone[id]
	f.mu.Unlock()
	o.RunID = id
	cb(o)
}

type fixture struct {
	st   *store.Store
	tm   *Team
	fs   *fakeStarter
	p    store.Project
	lead store.Agent
	rook store.Agent
	vera store.Agent
}

// setup: proiect cu lead + doi workeri (Rook poate scrie fisiere, Vera nu).
func setup(t *testing.T) *fixture {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })

	p := store.Project{Name: "ApiUnit", Goal: "Import OpenAPI", Workdir: t.TempDir(), MaxParallel: 2}
	lead, err := st.CreateProject(&p)
	if err != nil {
		t.Fatal(err)
	}
	rook := store.Agent{ProjectID: p.ID, Name: "Rook", Role: "Backend Dev", Position: store.PositionWorker, Model: "sonnet", Tools: []string{"write"}}
	vera := store.Agent{ProjectID: p.ID, Name: "Vera", Role: "QA", Position: store.PositionWorker, Model: "haiku"}
	for _, a := range []*store.Agent{&rook, &vera} {
		if err := st.CreateAgent(a); err != nil {
			t.Fatal(err)
		}
	}
	fs := newFakeStarter()
	tm := New(st, fs, events.NewHub(), "http://127.0.0.1:7777")
	return &fixture{st: st, tm: tm, fs: fs, p: p, lead: lead, rook: rook, vera: vera}
}

func (f *fixture) leadCaller() mcp.Caller {
	return mcp.Caller{ProjectID: f.p.ID, AgentID: f.lead.ID, Lead: true}
}

func (f *fixture) workerCaller(a store.Agent, taskID int64) mcp.Caller {
	return mcp.Caller{ProjectID: f.p.ID, AgentID: a.ID, TaskID: taskID}
}

// caller gaseste identitatea MCP a unui run pornit, din --mcp-config.
func (f *fixture) caller(t *testing.T, spec runner.Spec) mcp.Caller {
	t.Helper()
	var cfg struct {
		MCPServers map[string]struct {
			URL string `json:"url"`
		} `json:"mcpServers"`
	}
	if err := json.Unmarshal([]byte(argValue(spec.Args, "--mcp-config")), &cfg); err != nil {
		t.Fatal(err)
	}
	url := cfg.MCPServers["team"].URL
	c, ok := f.tm.Resolve(url[strings.LastIndex(url, "/")+1:])
	if !ok {
		t.Fatalf("token inactiv pentru run-ul %s", spec.ID)
	}
	return c
}

// task creeaza un task direct in store, cu statusul dorit.
func (f *fixture) task(t *testing.T, title string, who store.Agent, status string) store.Task {
	t.Helper()
	task := store.Task{ProjectID: f.p.ID, Title: title, AssigneeID: who.ID}
	if err := f.st.CreateTask(&task); err != nil {
		t.Fatal(err)
	}
	if status != store.StatusQueued {
		task.Status = status
		if err := f.st.SaveTask(&task); err != nil {
			t.Fatal(err)
		}
	}
	return task
}

func (f *fixture) wantStatus(t *testing.T, number int, status string) store.Task {
	t.Helper()
	task, err := f.st.GetTaskByNumber(f.p.ID, number)
	if err != nil {
		t.Fatal(err)
	}
	if task.Status != status {
		t.Fatalf("T-%d are status %q, vreau %q", number, task.Status, status)
	}
	return task
}

// hasMessage cauta un mesaj care contine textul in thread-ul agentului.
func (f *fixture) hasMessage(agentID int64, text string) bool {
	msgs, _ := f.st.ListMessages(agentID)
	for _, m := range msgs {
		if strings.Contains(m.Text, text) {
			return true
		}
	}
	return false
}

func argValue(args []string, flag string) string {
	for i, a := range args {
		if a == flag && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}

func hasFlag(args []string, flag string) bool {
	for _, a := range args {
		if a == flag {
			return true
		}
	}
	return false
}
```

- [ ] **Step 2: Scrie testele `internal/team/tools_test.go`**

```go
package team

import (
	"strings"
	"testing"
	"unicode/utf8"

	"foreman/internal/store"
)

func TestCreateTaskAssignsWorker(t *testing.T) {
	f := setup(t)
	text, err := f.tm.CreateTask(f.leadCaller(), "Parser $ref", "Doar local", "Rook")
	if err != nil || text != "Creat T-1 pentru Rook. Il va primi automat." {
		t.Fatalf("%q %v", text, err)
	}
	task := f.wantStatus(t, 1, store.StatusQueued)
	if task.AssigneeID != f.rook.ID || task.Description != "Doar local" {
		t.Fatalf("task = %+v", task)
	}
	if !f.hasMessage(f.lead.ID, "A creat T-1 pentru Rook: Parser $ref") {
		t.Error("lipseste evenimentul din thread-ul lead-ului")
	}
	if !f.hasMessage(f.rook.ID, "T-1 asignat de Lead: Parser $ref") {
		t.Error("lipseste evenimentul din thread-ul lui Rook")
	}
}

func TestCreateTaskAssigneeLoose(t *testing.T) {
	f := setup(t)
	if _, err := f.tm.CreateTask(f.leadCaller(), "A", "", "  rook "); err != nil {
		t.Fatal(err)
	}
	if task := f.wantStatus(t, 1, store.StatusQueued); task.AssigneeID != f.rook.ID {
		t.Fatalf("asignat lui %d", task.AssigneeID)
	}
}

func TestCreateTaskErrors(t *testing.T) {
	f := setup(t)
	if _, err := f.tm.CreateTask(f.leadCaller(), "A", "", "Nimeni"); err == nil || !strings.Contains(err.Error(), "Rook, Vera") {
		t.Errorf("worker necunoscut: %v", err)
	}
	if _, err := f.tm.CreateTask(f.leadCaller(), "A", "", "Lead"); err == nil {
		t.Error("task asignat lead-ului trebuia refuzat")
	}
	if _, err := f.tm.CreateTask(f.leadCaller(), "  ", "", "Rook"); err == nil {
		t.Error("titlu gol trebuia refuzat")
	}
	if _, err := f.tm.CreateTask(f.workerCaller(f.rook, 0), "A", "", "Vera"); err == nil {
		t.Error("un worker nu poate crea task-uri")
	}
	if tasks, _ := f.st.ListTasks(f.p.ID); len(tasks) != 0 {
		t.Fatalf("s-au creat task-uri: %+v", tasks)
	}
}

func TestListTasks(t *testing.T) {
	f := setup(t)
	if text, _ := f.tm.ListTasks(f.leadCaller(), ""); text != "Nu exista task-uri." {
		t.Fatalf("gol: %q", text)
	}
	f.task(t, "A", f.rook, store.StatusQueued)
	b := f.task(t, "B", f.vera, store.StatusReview)
	b.Result = "gata"
	f.st.SaveTask(&b)

	all, err := f.tm.ListTasks(f.leadCaller(), "")
	if err != nil || all != "T-1 [queued] A -> Rook\nT-2 [review] B -> Vera\n  Rezultat: gata" {
		t.Fatalf("toate: %q %v", all, err)
	}
	review, _ := f.tm.ListTasks(f.leadCaller(), "review")
	if review != "T-2 [review] B -> Vera\n  Rezultat: gata" {
		t.Fatalf("review: %q", review)
	}
	if _, err := f.tm.ListTasks(f.leadCaller(), "gresit"); err == nil {
		t.Fatal("status necunoscut trebuia refuzat")
	}
}

func TestReportDone(t *testing.T) {
	f := setup(t)
	task := f.task(t, "A", f.rook, store.StatusInProgress)
	text, err := f.tm.ReportDone(f.workerCaller(f.rook, task.ID), "A e gata")
	if err != nil || text != "T-1 e in Review. Lead va verifica." {
		t.Fatalf("%q %v", text, err)
	}
	if got := f.wantStatus(t, 1, store.StatusReview); got.Result != "A e gata" {
		t.Fatalf("rezultat = %q", got.Result)
	}
	pending, _ := f.st.Undelivered(f.lead.ID)
	if len(pending) != 1 || pending[0].Text != "Rook a terminat T-1 (review): A e gata" {
		t.Fatalf("notificari lead: %+v", pending)
	}
}

func TestReportDoneErrors(t *testing.T) {
	f := setup(t)
	queued := f.task(t, "A", f.rook, store.StatusQueued)
	running := f.task(t, "B", f.rook, store.StatusInProgress)
	cases := map[string]error{}
	_, cases["fara task"] = f.tm.ReportDone(f.workerCaller(f.rook, 0), "x")
	_, cases["lead"] = f.tm.ReportDone(f.leadCaller(), "x")
	_, cases["task care nu e in lucru"] = f.tm.ReportDone(f.workerCaller(f.rook, queued.ID), "x")
	_, cases["rezumat gol"] = f.tm.ReportDone(f.workerCaller(f.rook, running.ID), "  ")
	for name, err := range cases {
		if err == nil {
			t.Errorf("%s: trebuia eroare", name)
		}
	}
}

func TestLongSummaryIsClipped(t *testing.T) {
	f := setup(t)
	task := f.task(t, "A", f.rook, store.StatusInProgress)
	long := strings.Repeat("x", 10000)
	if _, err := f.tm.ReportDone(f.workerCaller(f.rook, task.ID), long); err != nil {
		t.Fatal(err)
	}
	if got := f.wantStatus(t, 1, store.StatusReview); got.Result != long {
		t.Fatal("rezultatul complet trebuie pastrat pe task")
	}
	pending, _ := f.st.Undelivered(f.lead.ID)
	if n := utf8.RuneCountInString(pending[0].Text); n > noteMax+100 {
		t.Fatalf("notificarea are %d caractere", n)
	}
}

func TestSendBackThenBlock(t *testing.T) {
	f := setup(t)
	f.task(t, "A", f.rook, store.StatusReview)
	for i := 1; i <= 2; i++ {
		text, err := f.tm.SendBack(f.leadCaller(), "T-1", "mai bine")
		want := "T-1 trimis inapoi lui Rook (" + string(rune('0'+i)) + "/3)."
		if err != nil || text != want {
			t.Fatalf("trimiterea %d: %q %v", i, text, err)
		}
		got := f.wantStatus(t, 1, store.StatusQueued)
		if got.Feedback != "mai bine" || got.SendBacks != i {
			t.Fatalf("dupa trimiterea %d: %+v", i, got)
		}
		got.Status = store.StatusReview // worker-ul a raportat din nou
		f.st.SaveTask(&got)
	}
	text, err := f.tm.SendBack(f.leadCaller(), "1", "tot nu")
	if err != nil || !strings.Contains(text, "blocat") {
		t.Fatalf("a treia trimitere: %q %v", text, err)
	}
	f.wantStatus(t, 1, store.StatusBlocked)
}

func TestSendBackErrors(t *testing.T) {
	f := setup(t)
	f.task(t, "A", f.rook, store.StatusQueued)
	if _, err := f.tm.SendBack(f.leadCaller(), "T-1", "x"); err == nil {
		t.Error("task din queued nu se poate trimite inapoi")
	}
	if _, err := f.tm.SendBack(f.leadCaller(), "T-1", " "); err == nil {
		t.Error("feedback gol trebuia refuzat")
	}
	if _, err := f.tm.SendBack(f.leadCaller(), "T-99", "x"); err == nil || !strings.Contains(err.Error(), "T-99") {
		t.Errorf("task inexistent: %v", err)
	}
	if _, err := f.tm.SendBack(f.leadCaller(), "abc", "x"); err == nil {
		t.Error("referinta invalida trebuia refuzata")
	}
}

func TestAcceptTask(t *testing.T) {
	f := setup(t)
	f.task(t, "A", f.rook, store.StatusReview)
	f.task(t, "B", f.rook, store.StatusQueued)
	if text, err := f.tm.AcceptTask(f.leadCaller(), "t-1", "bun"); err != nil || text != "T-1 e Done." {
		t.Fatalf("%q %v", text, err)
	}
	f.wantStatus(t, 1, store.StatusDone)
	if !f.hasMessage(f.lead.ID, "A acceptat T-1: bun") {
		t.Error("lipseste evenimentul de accept")
	}
	if _, err := f.tm.AcceptTask(f.leadCaller(), "T-2", ""); err == nil {
		t.Error("task din queued nu se poate accepta")
	}
}

func TestResolveUnknownToken(t *testing.T) {
	f := setup(t)
	if _, ok := f.tm.Resolve("nimic"); ok {
		t.Fatal("token necunoscut rezolvat")
	}
}
```

- [ ] **Step 3: Ruleaza testele ca sa vezi ca pica**

Run: `go test ./internal/team/`
Expected: FAIL la compilare (`undefined: New`).

- [ ] **Step 4: Scrie `internal/team/team.go`**

```go
// Package team coordoneaza echipele de agenti: implementeaza tool-urile MCP
// (create_task, report_done...), porneste run-urile cand e ceva de facut si
// actualizeaza starea cand se termina. E singurul pachet care leaga store,
// runner, mcp si prompt. Sursa de adevar e baza de date; in memorie tinem
// doar run-urile active si token-urile lor.
package team

import (
	"crypto/rand"
	"encoding/hex"
	"sync"

	"foreman/internal/events"
	"foreman/internal/mcp"
	"foreman/internal/runner"
	"foreman/internal/store"
)

// Statusurile unui agent, afisate in UI si in lista echipei.
const (
	StatusWorking = "Working"
	StatusQueued  = "Queued"
	StatusIdle    = "Idle"
)

// Starter porneste si opreste procese claude. In productie e *runner.Manager,
// in teste un fals care doar inregistreaza apelurile.
type Starter interface {
	Start(spec runner.Spec, onDone func(runner.Outcome)) (*runner.Run, error)
	Stop(runID string) error
}

type Team struct {
	st      *store.Store
	runs    Starter
	hub     *events.Hub
	baseURL string // ex. http://127.0.0.1:7777, pentru URL-ul serverului MCP

	// work serializeaza toate schimbarile de stare: tool-uri MCP, final de run,
	// reconciliere, actiuni din UI. E mai simplu decat sa analizam fiecare
	// combinatie de goroutine, iar operatiile sunt oricum scurte.
	work sync.Mutex

	mu      sync.Mutex          // protejeaza campurile de mai jos
	active  map[int64]activeRun // agentID -> run-ul lui activ
	tokens  map[string]mcp.Caller
	pending map[int64]bool // proiecte care trebuie reconciliate
	wake    chan struct{}
}

type activeRun struct {
	runID     string
	projectID int64
	taskID    int64
	token     string
}

func New(st *store.Store, runs Starter, hub *events.Hub, baseURL string) *Team {
	return &Team{
		st:      st,
		runs:    runs,
		hub:     hub,
		baseURL: baseURL,
		active:  map[int64]activeRun{},
		tokens:  map[string]mcp.Caller{},
		pending: map[int64]bool{},
		wake:    make(chan struct{}, 1),
	}
}

// Resolve gaseste agentul dupa token-ul din URL-ul MCP (implementeaza mcp.Backend).
func (t *Team) Resolve(token string) (mcp.Caller, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	c, ok := t.tokens[token]
	return c, ok
}

// Kick cere reconcilierea proiectului. Nu blocheaza: bucla din Run (Task 9)
// preia cererea; mai multe Kick-uri la rand se aduna intr-o singura reconciliere.
func (t *Team) Kick(projectID int64) {
	t.mu.Lock()
	t.pending[projectID] = true
	t.mu.Unlock()
	select {
	case t.wake <- struct{}{}:
	default: // exista deja un semnal in asteptare
	}
}

// ActiveRunID intoarce run-ul activ al agentului sau "".
func (t *Team) ActiveRunID(agentID int64) string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.active[agentID].runID
}

// ProjectActive spune daca vreun agent al proiectului ruleaza acum.
func (t *Team) ProjectActive(projectID int64) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, ar := range t.active {
		if ar.projectID == projectID {
			return true
		}
	}
	return false
}

func (t *Team) isActive(agentID int64) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	_, ok := t.active[agentID]
	return ok
}

// AgentStatus intoarce statusul afisat si task-ul curent (in lucru sau urmatorul).
func (t *Team) AgentStatus(a store.Agent, tasks []store.Task) (string, *store.Task) {
	pending, _ := t.st.HasUndelivered(a.ID)
	var current *store.Task
	for _, want := range []string{store.StatusInProgress, store.StatusQueued} {
		for i := range tasks {
			if current == nil && tasks[i].AssigneeID == a.ID && tasks[i].Status == want {
				current = &tasks[i]
			}
		}
	}
	return t.statusOf(a, tasks, pending), current
}

func (t *Team) statusOf(a store.Agent, tasks []store.Task, pending bool) string {
	if t.isActive(a.ID) {
		return StatusWorking
	}
	if pending {
		return StatusQueued
	}
	for _, task := range tasks {
		if task.AssigneeID == a.ID && task.Status == store.StatusQueued {
			return StatusQueued
		}
	}
	return StatusIdle
}

func (t *Team) emit(projectID int64, typ string, data any) {
	if t.hub != nil {
		t.hub.Publish(projectID, typ, data)
	}
}

// note adauga un eveniment in thread-ul unui agent. delivered=false il pune in
// coada agentului: il primeste la urmatorul run (asa afla lead-ul de rezultate).
func (t *Team) note(projectID, agentID int64, text string, delivered bool) error {
	m := store.Message{ProjectID: projectID, AgentID: agentID, Kind: store.KindEvent, Text: text, Delivered: delivered}
	if err := t.st.AddMessage(&m); err != nil {
		return err
	}
	t.emit(projectID, "message", m)
	return nil
}

func newToken() string {
	b := make([]byte, 32)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// noteMax limiteaza textele puse in notificari: mesajul turei ajunge ca
// argument de linie de comanda, iar sistemul de operare limiteaza lungimea lor.
const noteMax = 4000

func clip(s string) string {
	r := []rune(s)
	if len(r) <= noteMax {
		return s
	}
	return string(r[:noteMax]) + " [...]"
}
```

- [ ] **Step 5: Scrie `internal/team/tools.go`**

```go
package team

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"foreman/internal/mcp"
	"foreman/internal/store"
)

// MaxSendBacks: dupa atatea trimiteri inapoi, task-ul se blocheaza si decide utilizatorul.
const MaxSendBacks = 3

var _ mcp.Backend = (*Team)(nil) // compilatorul verifica ca Team implementeaza interfata

func (t *Team) CreateTask(c mcp.Caller, title, description, assignee string) (string, error) {
	t.work.Lock()
	defer t.work.Unlock()

	if !c.Lead {
		return "", errors.New("doar lead-ul poate crea task-uri")
	}
	title = strings.TrimSpace(title)
	if title == "" {
		return "", errors.New("title e obligatoriu")
	}
	w, err := t.st.FindAgentByName(c.ProjectID, strings.TrimSpace(assignee))
	if errors.Is(err, store.ErrNotFound) || (err == nil && w.Position != store.PositionWorker) {
		return "", fmt.Errorf("%q nu e un worker din echipa. Workeri: %s", assignee, t.workerNames(c.ProjectID))
	}
	if err != nil {
		return "", err
	}
	lead, err := t.st.GetAgent(c.AgentID)
	if err != nil {
		return "", err
	}

	task := store.Task{ProjectID: c.ProjectID, Title: title, Description: strings.TrimSpace(description), AssigneeID: w.ID}
	if err := t.st.CreateTask(&task); err != nil {
		return "", err
	}
	t.note(c.ProjectID, lead.ID, fmt.Sprintf("A creat %s pentru %s: %s", task.Ref(), w.Name, title), true)
	t.note(c.ProjectID, w.ID, fmt.Sprintf("%s asignat de %s: %s", task.Ref(), lead.Name, title), true)
	t.emit(c.ProjectID, "task", task)
	t.Kick(c.ProjectID)
	return fmt.Sprintf("Creat %s pentru %s. Il va primi automat.", task.Ref(), w.Name), nil
}

func (t *Team) ListTasks(c mcp.Caller, status string) (string, error) {
	t.work.Lock()
	defer t.work.Unlock()

	status = strings.TrimSpace(status)
	switch status {
	case "", store.StatusQueued, store.StatusInProgress, store.StatusReview, store.StatusDone, store.StatusBlocked:
	default:
		return "", fmt.Errorf("status necunoscut %q", status)
	}
	tasks, err := t.st.ListTasks(c.ProjectID)
	if err != nil {
		return "", err
	}
	agents, err := t.st.ListAgents(c.ProjectID)
	if err != nil {
		return "", err
	}
	names := map[int64]string{}
	for _, a := range agents {
		names[a.ID] = a.Name
	}

	var b strings.Builder
	for _, task := range tasks {
		if status != "" && task.Status != status {
			continue
		}
		fmt.Fprintf(&b, "%s [%s] %s -> %s\n", task.Ref(), task.Status, task.Title, names[task.AssigneeID])
		if (task.Status == store.StatusReview || task.Status == store.StatusBlocked) && task.Result != "" {
			fmt.Fprintf(&b, "  Rezultat: %s\n", clip(task.Result))
		}
	}
	if b.Len() == 0 {
		return "Nu exista task-uri.", nil
	}
	return strings.TrimRight(b.String(), "\n"), nil
}

func (t *Team) AcceptTask(c mcp.Caller, ref, note string) (string, error) {
	t.work.Lock()
	defer t.work.Unlock()

	if !c.Lead {
		return "", errors.New("doar lead-ul poate accepta task-uri")
	}
	task, err := t.findTask(c.ProjectID, ref)
	if err != nil {
		return "", err
	}
	if task.Status != store.StatusReview && task.Status != store.StatusBlocked {
		return "", fmt.Errorf("%s e %s; se accepta doar task-uri din review sau blocked", task.Ref(), task.Status)
	}
	task.Status = store.StatusDone
	if err := t.st.SaveTask(&task); err != nil {
		return "", err
	}
	text := "A acceptat " + task.Ref()
	if n := strings.TrimSpace(note); n != "" {
		text += ": " + n
	}
	t.note(c.ProjectID, c.AgentID, text, true)
	t.emit(c.ProjectID, "task", task)
	return task.Ref() + " e Done.", nil
}

func (t *Team) SendBack(c mcp.Caller, ref, feedback string) (string, error) {
	t.work.Lock()
	defer t.work.Unlock()

	if !c.Lead {
		return "", errors.New("doar lead-ul poate trimite task-uri inapoi")
	}
	feedback = strings.TrimSpace(feedback)
	if feedback == "" {
		return "", errors.New("feedback e obligatoriu: spune concret ce trebuie schimbat")
	}
	task, err := t.findTask(c.ProjectID, ref)
	if err != nil {
		return "", err
	}
	if task.Status != store.StatusReview {
		return "", fmt.Errorf("%s e %s; se trimit inapoi doar task-uri din review", task.Ref(), task.Status)
	}
	worker, err := t.st.GetAgent(task.AssigneeID)
	if err != nil {
		return "", err
	}
	lead, err := t.st.GetAgent(c.AgentID)
	if err != nil {
		return "", err
	}

	task.SendBacks++
	task.Feedback = feedback
	if task.SendBacks >= MaxSendBacks {
		task.Status = store.StatusBlocked
		if err := t.st.SaveTask(&task); err != nil {
			return "", err
		}
		t.note(c.ProjectID, lead.ID, fmt.Sprintf("%s e blocat dupa %d trimiteri inapoi. Decide utilizatorul.", task.Ref(), MaxSendBacks), true)
		t.emit(c.ProjectID, "task", task)
		return fmt.Sprintf("%s a fost trimis inapoi de %d ori si e acum blocat. Utilizatorul decide ce se intampla cu el.", task.Ref(), MaxSendBacks), nil
	}

	task.Status = store.StatusQueued
	if err := t.st.SaveTask(&task); err != nil {
		return "", err
	}
	t.note(c.ProjectID, lead.ID, fmt.Sprintf("A trimis inapoi %s lui %s: %s", task.Ref(), worker.Name, feedback), true)
	t.note(c.ProjectID, worker.ID, fmt.Sprintf("%s trimis inapoi de %s: %s", task.Ref(), lead.Name, feedback), true)
	t.emit(c.ProjectID, "task", task)
	t.Kick(c.ProjectID)
	return fmt.Sprintf("%s trimis inapoi lui %s (%d/%d).", task.Ref(), worker.Name, task.SendBacks, MaxSendBacks), nil
}

func (t *Team) ReportDone(c mcp.Caller, summary string) (string, error) {
	t.work.Lock()
	defer t.work.Unlock()

	if c.Lead {
		return "", errors.New("report_done e doar pentru workeri")
	}
	if c.TaskID == 0 {
		return "", errors.New("nu lucrezi pe niciun task acum; raspunde direct in mesaj")
	}
	summary = strings.TrimSpace(summary)
	if summary == "" {
		return "", errors.New("summary e obligatoriu")
	}
	task, err := t.st.GetTask(c.TaskID)
	if err != nil {
		return "", err
	}
	if task.Status != store.StatusInProgress {
		return "", fmt.Errorf("%s nu mai e in lucru (status %s)", task.Ref(), task.Status)
	}
	worker, err := t.st.GetAgent(c.AgentID)
	if err != nil {
		return "", err
	}
	lead, err := t.st.Lead(c.ProjectID)
	if err != nil {
		return "", err
	}

	task.Status = store.StatusReview
	task.Result = summary
	if err := t.st.SaveTask(&task); err != nil {
		return "", err
	}
	t.note(c.ProjectID, worker.ID, fmt.Sprintf("A raportat %s", task.Ref()), true)
	t.note(c.ProjectID, lead.ID, clip(fmt.Sprintf("%s a terminat %s (review): %s", worker.Name, task.Ref(), summary)), false)
	t.emit(c.ProjectID, "task", task)
	t.Kick(c.ProjectID)
	return fmt.Sprintf("%s e in Review. %s va verifica.", task.Ref(), lead.Name), nil
}

// findTask accepta "T-3", "t-3" sau "3".
func (t *Team) findTask(projectID int64, ref string) (store.Task, error) {
	clean := strings.TrimSpace(ref)
	clean = strings.TrimPrefix(strings.TrimPrefix(clean, "T-"), "t-")
	n, err := strconv.Atoi(clean)
	if err != nil {
		return store.Task{}, fmt.Errorf("task invalid %q; foloseste formatul T-3", ref)
	}
	task, err := t.st.GetTaskByNumber(projectID, n)
	if errors.Is(err, store.ErrNotFound) {
		return store.Task{}, fmt.Errorf("nu exista task-ul T-%d", n)
	}
	return task, err
}

func (t *Team) workerNames(projectID int64) string {
	agents, _ := t.st.ListAgents(projectID)
	var names []string
	for _, a := range agents {
		if a.Position == store.PositionWorker {
			names = append(names, a.Name)
		}
	}
	if len(names) == 0 {
		return "(niciunul)"
	}
	return strings.Join(names, ", ")
}
```

- [ ] **Step 6: Ruleaza testele**

Run: `go test ./internal/team/ -v -race`
Expected: PASS pe toate cele 11 teste.

- [ ] **Step 7: Commit**

```bash
go vet ./... && go build -race ./...
git add -A
git commit -m "team: nucleul si tool-urile MCP ale echipei"
```

---

### Task 9: `team`: scheduler-ul (reconciliere, run-uri, stop/resume, recuperare)

**Files:**
- Create: `internal/team/run.go`
- Create: `internal/team/reconcile.go`
- Create: `internal/team/reconcile_test.go`

**Interfaces:**
- Consumes (Task 8): `Team` cu campurile si helperele lui, `fakeStarter`, `setup`, `argValue`, `hasFlag`.
- Consumes (Task 4): `prompt.System`, `prompt.Hash`, `prompt.Settings`, `prompt.MCPConfig`, `prompt.WorkerTurn`, `prompt.DirectTurn`, `prompt.LeadTurn`, `prompt.Member`.
- Produces:
  - `(*Team) Run(ctx context.Context)`, bucla care proceseaza `Kick`-urile (porneste din main)
  - `(*Team) Reconcile(projectID int64) error`
  - `(*Team) Stop(projectID int64) error`, `(*Team) Resume(projectID int64) error`, `(*Team) Recover() error`
  - `(*Team) UserMessage(agentID int64, text string) (store.Message, error)`
  - `(*Team) AcceptBlocked(taskID int64) error`, `(*Team) Unblock(taskID int64) error`, `ErrNotBlocked`
  - Fiecare run are `runner.Spec.Key = fmt.Sprintf("agent-%d", agentID)`.

- [ ] **Step 1: Scrie testele `internal/team/reconcile_test.go`**

```go
package team

import (
	"errors"
	"os"
	"strings"
	"testing"

	"foreman/internal/runner"
	"foreman/internal/store"
)

func mustReconcile(t *testing.T, f *fixture) {
	t.Helper()
	if err := f.tm.Reconcile(f.p.ID); err != nil {
		t.Fatal(err)
	}
}

func turn(spec runner.Spec) string { return argValue(spec.Args, "-p") }

func TestLeadWakesOnUserMessage(t *testing.T) {
	f := setup(t)
	if _, err := f.tm.UserMessage(f.lead.ID, "Fa importul"); err != nil {
		t.Fatal(err)
	}
	mustReconcile(t, f)
	if f.fs.count() != 1 {
		t.Fatalf("run-uri pornite: %d", f.fs.count())
	}
	spec := f.fs.last(t, f.lead)
	if !strings.Contains(turn(spec), "Utilizatorul: Fa importul") || !strings.HasPrefix(turn(spec), "Echipa:") {
		t.Fatalf("mesajul turei: %q", turn(spec))
	}
	if spec.Dir != f.p.Workdir || argValue(spec.Args, "--model") != "sonnet" || hasFlag(spec.Args, "--resume") {
		t.Fatalf("spec = %+v", spec)
	}
	mustReconcile(t, f) // mesajul e livrat: nu se mai porneste nimic
	if f.fs.count() != 1 {
		t.Fatalf("a doua reconciliere a pornit ceva: %d", f.fs.count())
	}
}

func TestRunArguments(t *testing.T) {
	f := setup(t)
	f.task(t, "A", f.rook, store.StatusQueued)
	mustReconcile(t, f)
	args := f.fs.last(t, f.rook).Args
	for _, flag := range []string{"--strict-mcp-config", "--verbose"} {
		if !hasFlag(args, flag) {
			t.Errorf("lipseste %s", flag)
		}
	}
	checks := map[string]string{
		"--output-format":   "stream-json",
		"--setting-sources": "user",
		"--max-budget-usd":  "1.00",
		"--model":           "sonnet",
	}
	for flag, want := range checks {
		if got := argValue(args, flag); got != want {
			t.Errorf("%s = %q, vreau %q", flag, got, want)
		}
	}
	if !strings.Contains(argValue(args, "--settings"), `"Edit"`) {
		t.Error("Rook are tool-ul write: --settings trebuia sa permita Edit")
	}
	if !strings.Contains(argValue(args, "--mcp-config"), `"url":"http://127.0.0.1:7777/mcp/`) {
		t.Errorf("--mcp-config = %s", argValue(args, "--mcp-config"))
	}
	if !strings.Contains(argValue(args, "--append-system-prompt"), "Esti Rook, Backend Dev, worker in proiectul ApiUnit") {
		t.Error("system prompt gresit")
	}
}

func TestFullFlow(t *testing.T) {
	f := setup(t)
	f.tm.UserMessage(f.lead.ID, "Fa importul")
	mustReconcile(t, f)
	lead1 := f.fs.last(t, f.lead)
	lc := f.caller(t, lead1)
	for _, x := range [][2]string{{"A", "Rook"}, {"B", "Vera"}, {"C", "rook"}} {
		if _, err := f.tm.CreateTask(lc, x[0], "", x[1]); err != nil {
			t.Fatal(err)
		}
	}
	f.fs.finish(lead1.ID, runner.Outcome{Status: "done", SessionID: "s-lead", Result: "Am delegat."})
	if !f.hasMessage(f.lead.ID, "Am delegat.") {
		t.Fatal("raspunsul final al lead-ului lipseste din thread")
	}

	// doi workeri in paralel; T-3 asteapta dupa Rook
	mustReconcile(t, f)
	rook1, vera1 := f.fs.last(t, f.rook), f.fs.last(t, f.vera)
	if !strings.Contains(turn(rook1), "Task T-1: A") || !strings.Contains(turn(vera1), "Task T-2: B") {
		t.Fatalf("ture: %q / %q", turn(rook1), turn(vera1))
	}
	f.wantStatus(t, 1, store.StatusInProgress)
	f.wantStatus(t, 3, store.StatusQueued)

	// Rook raporteaza T-1: lead-ul e trezit cu --resume si notificarea
	if _, err := f.tm.ReportDone(f.caller(t, rook1), "A e gata"); err != nil {
		t.Fatal(err)
	}
	mustReconcile(t, f)
	lead2 := f.fs.last(t, f.lead)
	if lead2.ID == lead1.ID || !strings.Contains(turn(lead2), "Rook a terminat T-1 (review): A e gata") {
		t.Fatalf("lead-ul nu a fost trezit corect: %q", turn(lead2))
	}
	if argValue(lead2.Args, "--resume") != "s-lead" {
		t.Fatalf("--resume = %q", argValue(lead2.Args, "--resume"))
	}

	// procesul lui Rook se termina: preia T-3, continuand sesiunea
	f.fs.finish(rook1.ID, runner.Outcome{Status: "done", SessionID: "s-rook", Result: "gata"})
	mustReconcile(t, f)
	rook2 := f.fs.last(t, f.rook)
	if !strings.Contains(turn(rook2), "Task T-3: C") || argValue(rook2.Args, "--resume") != "s-rook" {
		t.Fatalf("rook2: %q resume=%q", turn(rook2), argValue(rook2.Args, "--resume"))
	}

	// lead-ul trimite T-1 inapoi; Rook termina T-3 fara report_done
	if _, err := f.tm.SendBack(f.caller(t, lead2), "T-1", "Lipsesc testele"); err != nil {
		t.Fatal(err)
	}
	f.fs.finish(lead2.ID, runner.Outcome{Status: "done", SessionID: "s-lead"})
	f.fs.finish(rook2.ID, runner.Outcome{Status: "done", SessionID: "s-rook", Result: "C e gata"})
	if got := f.wantStatus(t, 3, store.StatusReview); got.Result != "C e gata" {
		t.Fatalf("T-3 rezultat = %q", got.Result)
	}

	mustReconcile(t, f)
	rook3 := f.fs.last(t, f.rook)
	if !strings.Contains(turn(rook3), "Task T-1: A") || !strings.Contains(turn(rook3), "Feedback de la Lead la incercarea anterioara:\nLipsesc testele") {
		t.Fatalf("rook3: %q", turn(rook3))
	}
	lead3 := f.fs.last(t, f.lead)
	if !strings.Contains(turn(lead3), "Rook a terminat T-3 fara report_done (review): C e gata") {
		t.Fatalf("lead3: %q", turn(lead3))
	}
	if _, err := f.tm.AcceptTask(f.caller(t, lead3), "T-3", ""); err != nil {
		t.Fatal(err)
	}
	f.wantStatus(t, 3, store.StatusDone)
}

func TestMaxParallel(t *testing.T) {
	f := setup(t)
	f.p.MaxParallel = 1
	f.st.UpdateProject(f.p)
	f.task(t, "A", f.rook, store.StatusQueued)
	f.task(t, "B", f.vera, store.StatusQueued)
	mustReconcile(t, f)
	if f.fs.count() != 1 {
		t.Fatalf("run-uri pornite: %d, vreau 1", f.fs.count())
	}
	f.wantStatus(t, 2, store.StatusQueued)
}

func TestFailedWorkerRunGoesToReview(t *testing.T) {
	f := setup(t)
	f.task(t, "A", f.rook, store.StatusQueued)
	mustReconcile(t, f)
	f.fs.finish(f.fs.last(t, f.rook).ID, runner.Outcome{Status: "failed", Error: "Reached maximum budget ($1)"})
	if got := f.wantStatus(t, 1, store.StatusReview); got.Result != "run esuat: Reached maximum budget ($1)" {
		t.Fatalf("rezultat = %q", got.Result)
	}
	pending, _ := f.st.Undelivered(f.lead.ID)
	if len(pending) != 1 || !strings.Contains(pending[0].Text, "Run-ul lui Rook pe T-1 a esuat") {
		t.Fatalf("notificari lead: %+v", pending)
	}
	if !f.hasMessage(f.rook.ID, "Run esuat: Reached maximum budget") {
		t.Error("lipseste evenimentul din thread-ul worker-ului")
	}
}

func TestStopAndResume(t *testing.T) {
	f := setup(t)
	f.task(t, "A", f.rook, store.StatusQueued)
	mustReconcile(t, f)
	run := f.fs.last(t, f.rook)

	if err := f.tm.Stop(f.p.ID); err != nil {
		t.Fatal(err)
	}
	if len(f.fs.stopped) != 1 || f.fs.stopped[0] != run.ID {
		t.Fatalf("oprite: %v", f.fs.stopped)
	}
	f.wantStatus(t, 1, store.StatusQueued)
	if p, _ := f.st.GetProject(f.p.ID); !p.Paused {
		t.Fatal("proiectul nu e pe pauza")
	}
	f.fs.finish(run.ID, runner.Outcome{Status: "stopped", Error: "oprit"})
	mustReconcile(t, f)
	if f.fs.count() != 1 {
		t.Fatal("pe pauza nu trebuie sa porneasca nimic")
	}

	if err := f.tm.Resume(f.p.ID); err != nil {
		t.Fatal(err)
	}
	mustReconcile(t, f)
	if f.fs.count() != 2 {
		t.Fatalf("dupa Resume: %d run-uri", f.fs.count())
	}
}

func TestRecover(t *testing.T) {
	f := setup(t)
	task := f.task(t, "A", f.rook, store.StatusInProgress)
	f.st.CreateRun(&store.Run{ID: "r-vechi", AgentID: f.rook.ID, TaskID: task.ID})
	if err := f.tm.Recover(); err != nil {
		t.Fatal(err)
	}
	if r, _ := f.st.GetRun("r-vechi"); r.Status != store.RunInterrupted {
		t.Fatalf("run vechi: %q", r.Status)
	}
	f.wantStatus(t, 1, store.StatusQueued)
	if p, _ := f.st.GetProject(f.p.ID); !p.Paused {
		t.Fatal("dupa Recover proiectul trebuie sa fie pe pauza")
	}
}

func TestMissingWorkdirPausesProject(t *testing.T) {
	f := setup(t)
	f.tm.UserMessage(f.lead.ID, "salut")
	os.RemoveAll(f.p.Workdir)
	mustReconcile(t, f)
	if f.fs.count() != 0 {
		t.Fatal("nu trebuia pornit nimic")
	}
	if p, _ := f.st.GetProject(f.p.ID); !p.Paused {
		t.Fatal("proiectul trebuia pus pe pauza")
	}
	if !f.hasMessage(f.lead.ID, "Nu pot porni Lead") {
		t.Fatal("lipseste explicatia din thread-ul lead-ului")
	}
}

func TestPromptChangeStartsNewSession(t *testing.T) {
	f := setup(t)
	f.st.SetSession(f.rook.ID, "s-vechi", "hash-vechi")
	f.tm.UserMessage(f.rook.ID, "salut")
	mustReconcile(t, f)
	if hasFlag(f.fs.last(t, f.rook).Args, "--resume") {
		t.Fatal("prompt schimbat: nu trebuia --resume")
	}
	if !f.hasMessage(f.rook.ID, "sesiune noua") {
		t.Fatal("lipseste evenimentul de sesiune noua")
	}
}

func TestDirectMessageToWorker(t *testing.T) {
	f := setup(t)
	f.tm.UserMessage(f.rook.ID, "ce faci?")
	mustReconcile(t, f)
	if got := turn(f.fs.last(t, f.rook)); !strings.Contains(got, "Mesaje de la utilizator") || !strings.Contains(got, "ce faci?") {
		t.Fatalf("worker: %q", got)
	}
	if got := turn(f.fs.last(t, f.lead)); !strings.Contains(got, "Utilizatorul -> Rook: ce faci?") {
		t.Fatalf("lead: %q", got)
	}
	msgs, _ := f.st.ListMessages(f.rook.ID)
	if !msgs[0].Delivered {
		t.Fatal("mesajul trebuia marcat livrat")
	}
}

func TestBlockedTaskActions(t *testing.T) {
	f := setup(t)
	a := f.task(t, "A", f.rook, store.StatusBlocked)
	b := f.task(t, "B", f.rook, store.StatusBlocked)
	c := f.task(t, "C", f.rook, store.StatusQueued)
	if err := f.tm.Unblock(a.ID); err != nil {
		t.Fatal(err)
	}
	if got := f.wantStatus(t, 1, store.StatusQueued); got.SendBacks != 0 {
		t.Fatalf("send_backs = %d", got.SendBacks)
	}
	if err := f.tm.AcceptBlocked(b.ID); err != nil {
		t.Fatal(err)
	}
	f.wantStatus(t, 2, store.StatusDone)
	if err := f.tm.AcceptBlocked(c.ID); !errors.Is(err, ErrNotBlocked) {
		t.Fatalf("task neblocat: %v", err)
	}
	if !f.hasMessage(f.lead.ID, "Utilizatorul a deblocat T-1") {
		t.Fatal("lead-ul trebuie anuntat")
	}
}
```

- [ ] **Step 2: Ruleaza testele ca sa vezi ca pica**

Run: `go test ./internal/team/`
Expected: FAIL la compilare (`f.tm.Reconcile undefined`).

- [ ] **Step 3: Scrie `internal/team/run.go`**

```go
package team

import (
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"

	"foreman/internal/mcp"
	"foreman/internal/prompt"
	"foreman/internal/runner"
	"foreman/internal/store"
)

// startRun porneste un run pentru agent. taskID = 0 inseamna run fara task
// (lead sau mesaj direct). Se apeleaza cu t.work blocat.
func (t *Team) startRun(p store.Project, a, lead store.Agent, taskID int64, turn string) error {
	if info, err := os.Stat(p.Workdir); err != nil || !info.IsDir() {
		return fmt.Errorf("folderul de lucru %s nu exista", p.Workdir)
	}

	system := prompt.System(p, a, lead.Name)
	hash := prompt.Hash(system)
	// Claude retine system prompt-ul de la inceputul sesiunii. Daca s-a schimbat
	// (agent sau proiect editat), --resume l-ar ignora, deci pornim sesiune noua.
	resume := a.SessionID != "" && a.PromptHash == hash
	if a.SessionID != "" && !resume {
		t.note(p.ID, a.ID, "Instructiunile s-au schimbat, pornesc o sesiune noua.", true)
	}

	runID := runner.NewID()
	token := newToken()
	if err := t.st.CreateRun(&store.Run{ID: runID, AgentID: a.ID, TaskID: taskID}); err != nil {
		return err
	}

	t.mu.Lock()
	t.tokens[token] = mcp.Caller{ProjectID: p.ID, AgentID: a.ID, RunID: runID, TaskID: taskID, Lead: a.Position == store.PositionLead}
	t.active[a.ID] = activeRun{runID: runID, projectID: p.ID, taskID: taskID, token: token}
	t.mu.Unlock()

	spec := runner.Spec{
		ID:     runID,
		Key:    fmt.Sprintf("agent-%d", a.ID),
		Dir:    p.Workdir,
		Args:   t.args(p, a, system, turn, token, resume),
		Prompt: turn,
	}
	agentID := a.ID
	if _, err := t.runs.Start(spec, func(o runner.Outcome) { t.finish(agentID, hash, o) }); err != nil {
		t.mu.Lock()
		delete(t.tokens, token)
		delete(t.active, a.ID)
		t.mu.Unlock()
		t.st.FinishRun(runID, store.RunFailed, 0, err.Error())
		return err
	}
	t.emit(p.ID, "agent", map[string]int64{"id": a.ID})
	return nil
}

// args construieste linia de comanda pentru claude.
func (t *Team) args(p store.Project, a store.Agent, system, turn, token string, resume bool) []string {
	args := []string{
		"-p", turn,
		"--output-format", "stream-json", "--verbose",
		"--model", a.Model,
		"--append-system-prompt", system,
		// setarile globale ale utilizatorului raman; .claude/settings.json din
		// repo-ul proiectului nu se incarca (permisiunile vin doar de la noi)
		"--setting-sources", "user",
		"--settings", prompt.Settings(a.Tools),
		"--mcp-config", prompt.MCPConfig(t.baseURL + "/mcp/" + token),
		"--strict-mcp-config",
		"--max-budget-usd", strconv.FormatFloat(p.BudgetUSD, 'f', 2, 64),
	}
	if resume {
		args = append(args, "--resume", a.SessionID)
	}
	return args
}

// finish e apelata de runner cand procesul claude s-a terminat.
func (t *Team) finish(agentID int64, hash string, o runner.Outcome) {
	t.work.Lock()
	defer t.work.Unlock()

	t.mu.Lock()
	ar, ok := t.active[agentID]
	if ok && ar.runID == o.RunID {
		delete(t.active, agentID)
		delete(t.tokens, ar.token)
	}
	t.mu.Unlock()
	if !ok || ar.runID != o.RunID {
		log.Printf("run %s terminat, dar nu era activ", o.RunID)
		return
	}

	if err := t.recordOutcome(ar, agentID, hash, o); err != nil {
		log.Printf("finalul run-ului %s: %v", o.RunID, err)
	}
	t.emit(ar.projectID, "agent", map[string]int64{"id": agentID})
	t.Kick(ar.projectID)
}

func (t *Team) recordOutcome(ar activeRun, agentID int64, hash string, o runner.Outcome) error {
	if err := t.st.FinishRun(o.RunID, o.Status, o.CostUSD, o.Error); err != nil {
		return err
	}
	if o.SessionID != "" {
		if err := t.st.SetSession(agentID, o.SessionID, hash); err != nil {
			return err
		}
	}
	a, err := t.st.GetAgent(agentID)
	if err != nil {
		return err
	}
	if text := strings.TrimSpace(o.Result); text != "" {
		m := store.Message{ProjectID: ar.projectID, AgentID: a.ID, Kind: store.KindMsg, Author: a.Name, Text: text, RunID: o.RunID, Delivered: true}
		if err := t.st.AddMessage(&m); err != nil {
			return err
		}
		t.emit(ar.projectID, "message", m)
	}
	if o.Status == store.RunFailed {
		t.note(ar.projectID, a.ID, "Run esuat: "+o.Error, true)
	}
	if ar.taskID == 0 {
		return nil
	}

	task, err := t.st.GetTask(ar.taskID)
	if err != nil {
		return err
	}
	if task.Status != store.StatusInProgress {
		return nil // a apelat report_done sau Stop team l-a pus inapoi in Queued
	}
	var noteText string
	switch o.Status {
	case store.RunStopped:
		task.Status = store.StatusQueued
	case store.RunFailed:
		task.Status = store.StatusReview
		task.Result = "run esuat: " + o.Error
		noteText = fmt.Sprintf("Run-ul lui %s pe %s a esuat: %s", a.Name, task.Ref(), o.Error)
	default:
		task.Status = store.StatusReview
		task.Result = strings.TrimSpace(o.Result)
		noteText = fmt.Sprintf("%s a terminat %s fara report_done (review): %s", a.Name, task.Ref(), task.Result)
	}
	if err := t.st.SaveTask(&task); err != nil {
		return err
	}
	t.emit(ar.projectID, "task", task)
	if noteText == "" {
		return nil
	}
	lead, err := t.st.Lead(ar.projectID)
	if err != nil {
		return err
	}
	return t.note(ar.projectID, lead.ID, clip(noteText), false)
}
```

- [ ] **Step 4: Scrie `internal/team/reconcile.go`**

```go
package team

import (
	"context"
	"errors"
	"fmt"
	"log"

	"foreman/internal/prompt"
	"foreman/internal/store"
)

var ErrNotBlocked = errors.New("task-ul nu e blocat")

// Run proceseaza cererile Kick pana cand ctx e anulat. Porneste din main.
func (t *Team) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.wake:
		}
		t.mu.Lock()
		ids := make([]int64, 0, len(t.pending))
		for id := range t.pending {
			ids = append(ids, id)
		}
		t.pending = map[int64]bool{}
		t.mu.Unlock()

		for _, id := range ids {
			if err := t.Reconcile(id); err != nil {
				log.Printf("proiectul %d: %v", id, err)
			}
		}
	}
}

// Reconcile porneste run-urile care trebuie pornite in proiect, in ordinea:
// workerii pentru task-urile din Queued, workerii cu mesaje directe, apoi
// lead-ul daca are notificari. E sigur de apelat oricand si de oricate ori.
func (t *Team) Reconcile(projectID int64) error {
	t.work.Lock()
	defer t.work.Unlock()

	p, err := t.st.GetProject(projectID)
	if errors.Is(err, store.ErrNotFound) {
		return nil // proiectul a fost sters intre timp
	}
	if err != nil {
		return err
	}
	if p.Paused {
		return nil
	}
	agents, err := t.st.ListAgents(projectID)
	if err != nil {
		return err
	}
	if len(agents) == 0 || agents[0].Position != store.PositionLead {
		return fmt.Errorf("proiectul %d nu are lead", projectID)
	}
	lead, workers := agents[0], agents[1:]
	tasks, err := t.st.ListTasks(projectID)
	if err != nil {
		return err
	}
	byID := map[int64]store.Agent{}
	for _, a := range agents {
		byID[a.ID] = a
	}
	running := 0
	for _, w := range workers {
		if t.isActive(w.ID) {
			running++
		}
	}

	// 1. task-urile din Queued, in ordinea numarului
	hasQueued := map[int64]bool{}
	for i := range tasks {
		task := &tasks[i]
		if task.Status != store.StatusQueued {
			continue
		}
		hasQueued[task.AssigneeID] = true
		if running >= p.MaxParallel || t.isActive(task.AssigneeID) {
			continue
		}
		w := byID[task.AssigneeID]
		msgs, err := t.st.Undelivered(w.ID)
		if err != nil {
			return err
		}
		task.Status = store.StatusInProgress
		if err := t.st.SaveTask(task); err != nil {
			return err
		}
		turn := prompt.WorkerTurn(t.members(agents, tasks), *task, lead.Name, texts(msgs, false))
		if err := t.startRun(p, w, lead, task.ID, turn); err != nil {
			task.Status = store.StatusQueued
			if err := t.st.SaveTask(task); err != nil {
				return err
			}
			return t.startFailed(p, lead, w, err)
		}
		if err := t.delivered(p.ID, msgs); err != nil {
			return err
		}
		t.emit(p.ID, "task", *task)
		running++
	}

	// 2. workeri fara task, carora utilizatorul le-a scris direct
	for _, w := range workers {
		if running >= p.MaxParallel {
			break
		}
		if t.isActive(w.ID) || hasQueued[w.ID] {
			continue
		}
		msgs, err := t.st.Undelivered(w.ID)
		if err != nil {
			return err
		}
		if len(msgs) == 0 {
			continue
		}
		if err := t.startRun(p, w, lead, 0, prompt.DirectTurn(texts(msgs, false))); err != nil {
			return t.startFailed(p, lead, w, err)
		}
		if err := t.delivered(p.ID, msgs); err != nil {
			return err
		}
		running++
	}

	// 3. lead-ul, daca are notificari (mesaje de la utilizator, rezultate)
	if t.isActive(lead.ID) {
		return nil
	}
	notes, err := t.st.Undelivered(lead.ID)
	if err != nil || len(notes) == 0 {
		return err
	}
	if err := t.startRun(p, lead, lead, 0, prompt.LeadTurn(t.members(agents, tasks), texts(notes, true))); err != nil {
		return t.startFailed(p, lead, lead, err)
	}
	return t.delivered(p.ID, notes)
}

// members construieste lista echipei cu statusul curent al fiecaruia.
func (t *Team) members(agents []store.Agent, tasks []store.Task) []prompt.Member {
	out := make([]prompt.Member, 0, len(agents))
	for _, a := range agents {
		out = append(out, prompt.Member{Name: a.Name, Role: a.Role, Position: a.Position, Status: t.statusOf(a, tasks, false)})
	}
	return out
}

// texts extrage textele mesajelor; pentru lead marcam ce a scris utilizatorul.
func texts(msgs []store.Message, labelUser bool) []string {
	out := make([]string, 0, len(msgs))
	for _, m := range msgs {
		if labelUser && m.Author == store.AuthorUser {
			out = append(out, "Utilizatorul: "+m.Text)
		} else {
			out = append(out, m.Text)
		}
	}
	return out
}

// delivered marcheaza mesajele ca livrate si anunta UI-ul (dispare "in asteptare").
func (t *Team) delivered(projectID int64, msgs []store.Message) error {
	ids := make([]int64, len(msgs))
	for i, m := range msgs {
		ids[i] = m.ID
	}
	if err := t.st.MarkDelivered(ids); err != nil {
		return err
	}
	for _, m := range msgs {
		m.Delivered = true
		t.emit(projectID, "message", m)
	}
	return nil
}

// startFailed: un run nu a putut porni (ex. folderul de lucru a disparut).
// Punem proiectul pe pauza ca sa nu reincercam in bucla si explicam in thread.
func (t *Team) startFailed(p store.Project, lead, a store.Agent, cause error) error {
	if err := t.st.SetPaused(p.ID, true); err != nil {
		return err
	}
	t.note(p.ID, lead.ID, fmt.Sprintf("Nu pot porni %s: %v. Proiectul e pe pauza.", a.Name, cause), true)
	t.emit(p.ID, "project", map[string]int64{"id": p.ID})
	return nil
}

// Stop opreste echipa: pauza, procesele active sunt oprite, task-urile in lucru
// revin in Queued. Procesele se opresc asincron; finish vine dupa.
func (t *Team) Stop(projectID int64) error {
	t.work.Lock()
	defer t.work.Unlock()

	if err := t.st.SetPaused(projectID, true); err != nil {
		return err
	}
	t.mu.Lock()
	var ids []string
	for _, ar := range t.active {
		if ar.projectID == projectID {
			ids = append(ids, ar.runID)
		}
	}
	t.mu.Unlock()
	for _, id := range ids {
		if err := t.runs.Stop(id); err != nil {
			log.Printf("oprire run %s: %v", id, err)
		}
	}
	if err := t.st.RequeueInProgress(projectID); err != nil {
		return err
	}
	t.emit(projectID, "project", map[string]int64{"id": projectID})
	return nil
}

func (t *Team) Resume(projectID int64) error {
	t.work.Lock()
	err := t.st.SetPaused(projectID, false)
	t.work.Unlock()
	if err != nil {
		return err
	}
	t.emit(projectID, "project", map[string]int64{"id": projectID})
	t.Kick(projectID)
	return nil
}

// Recover aduce baza de date intr-o stare curata la pornirea serverului:
// run-urile ramase "running" (procesele au murit odata cu serverul) devin
// "interrupted", task-urile in lucru revin in Queued si totul e pe pauza,
// ca nimic sa nu porneasca (si sa coste) fara stirea utilizatorului.
func (t *Team) Recover() error {
	t.work.Lock()
	defer t.work.Unlock()
	if err := t.st.InterruptRunning(); err != nil {
		return err
	}
	if err := t.st.RequeueAllInProgress(); err != nil {
		return err
	}
	return t.st.PauseAll()
}

// UserMessage salveaza un mesaj de la utilizator catre un agent. Agentul il
// primeste la urmatorul run; pentru un worker, lead-ul primeste o copie.
func (t *Team) UserMessage(agentID int64, text string) (store.Message, error) {
	t.work.Lock()
	defer t.work.Unlock()

	a, err := t.st.GetAgent(agentID)
	if err != nil {
		return store.Message{}, err
	}
	m := store.Message{ProjectID: a.ProjectID, AgentID: a.ID, Kind: store.KindMsg, Author: store.AuthorUser, Text: text, Delivered: false}
	if err := t.st.AddMessage(&m); err != nil {
		return store.Message{}, err
	}
	t.emit(a.ProjectID, "message", m)
	if a.Position == store.PositionWorker {
		lead, err := t.st.Lead(a.ProjectID)
		if err != nil {
			return store.Message{}, err
		}
		t.note(a.ProjectID, lead.ID, clip(fmt.Sprintf("Utilizatorul -> %s: %s", a.Name, text)), false)
	}
	t.Kick(a.ProjectID)
	return m, nil
}

func (t *Team) AcceptBlocked(taskID int64) error { return t.resolveBlocked(taskID, true) }
func (t *Team) Unblock(taskID int64) error       { return t.resolveBlocked(taskID, false) }

// resolveBlocked aplica decizia utilizatorului pe un task blocat si il anunta pe lead.
func (t *Team) resolveBlocked(taskID int64, accept bool) error {
	t.work.Lock()
	defer t.work.Unlock()

	task, err := t.st.GetTask(taskID)
	if err != nil {
		return err
	}
	if task.Status != store.StatusBlocked {
		return ErrNotBlocked
	}
	lead, err := t.st.Lead(task.ProjectID)
	if err != nil {
		return err
	}
	text := "Utilizatorul a acceptat " + task.Ref() + "."
	if accept {
		task.Status = store.StatusDone
	} else {
		task.Status = store.StatusQueued
		task.SendBacks = 0
		text = "Utilizatorul a deblocat " + task.Ref() + "; worker-ul il reia cu ultimul feedback."
	}
	if err := t.st.SaveTask(&task); err != nil {
		return err
	}
	t.note(task.ProjectID, lead.ID, text, false)
	t.emit(task.ProjectID, "task", task)
	t.Kick(task.ProjectID)
	return nil
}
```

- [ ] **Step 5: Ruleaza testele**

Run: `go test ./internal/team/ -v -race`
Expected: PASS pe toate testele (11 din Task 8 + 12 noi).

- [ ] **Step 6: Commit**

```bash
go vet ./... && go build -race ./...
git add -A
git commit -m "team: scheduler, run-uri, stop/resume, recuperare la pornire"
```

---

### Task 10: API web complet si legarea in main

**Files:**
- Modify (rescriere): `internal/web/server.go`
- Create: `internal/web/api_projects.go`
- Create: `internal/web/api_agents.go`
- Create: `internal/web/api_tasks.go`
- Create: `internal/web/sse.go`
- Create: `internal/web/server_test.go`
- Modify (rescriere): `cmd/foreman/main.go`

**Interfaces:**
- Consumes:
  - `store` (Task 2-3);
  - din `team` (Task 8-9): `New`, `Kick`, `ActiveRunID`, `ProjectActive`, `AgentStatus`, `Stop`, `Resume`, `Recover`, `Run`, `UserMessage`, `AcceptBlocked`, `Unblock` si `ErrNotBlocked`;
  - `mcp.NewServer` (Task 7), `events.Hub` (Task 6), `runner.Manager` (Task 5), `prompt.System`/`prompt.ValidTools` (Task 4).
- Produces:
  - `web.New(st *store.Store, tm *team.Team, runs *runner.Manager, hub *events.Hub, mcpHandler http.Handler) *Server`
  - Rutele din tabelul de mai jos. Toate intorc JSON, iar erorile au forma `{"error": "..."}`.
  - Evenimentele SSE de proiect au forma `{"type": "task"|"message"|"agent"|"project", "data": ...}`. La `message`, `data` e un `store.Message`, iar la celelalte e un obiect `{id}` sau un `store.Task`.

| Ruta | Raspuns |
|---|---|
| `GET /api/info` | `{"name":"foreman"}` |
| `GET /api/projects` | `[projectCard]`: campurile proiectului + `status` (`Running`/`Idle`/`Paused`), `team:[{id,name,position}]`, `done`, `total` |
| `POST /api/projects` | 201 cu proiectul (lead-ul se creeaza automat) |
| `GET /api/projects/{id}` | `projectCard` + `agents:[agent + status, activeRunId, currentTask]` + `tasks:[task]` |
| `PUT /api/projects/{id}` | 200 cu proiectul |
| `DELETE /api/projects/{id}` | 204; 409 daca echipa lucreaza |
| `POST /api/projects/{id}/stop` si `/resume` | 204 |
| `GET /api/projects/{id}/events` | SSE |
| `POST /api/projects/{id}/agents` | 201 cu agentul (mereu worker) |
| `PUT /api/agents/{id}` | 200 cu agentul |
| `DELETE /api/agents/{id}` | 204; 409 daca lucreaza, e lead sau are task-uri |
| `GET /api/agents/{id}/messages` | `[message]` |
| `POST /api/agents/{id}/messages` | 201 cu mesajul |
| `POST /api/prompt-preview` | `{"prompt": "..."}` |
| `POST /api/tasks/{id}/accept` si `/unblock` | 204; 409 daca nu e blocat |
| `GET /api/runs/{id}/events` | SSE (ca in Task 5) |
| `POST`/`GET /mcp/{token}` | serverul MCP |

- [ ] **Step 1: Scrie testele `internal/web/server_test.go`**

```go
package web

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"foreman/internal/events"
	"foreman/internal/mcp"
	"foreman/internal/runner"
	"foreman/internal/store"
	"foreman/internal/team"
)

// nopStarter: run-urile "pornesc" dar nu se termina niciodata.
type nopStarter struct{}

func (nopStarter) Start(runner.Spec, func(runner.Outcome)) (*runner.Run, error) { return nil, nil }
func (nopStarter) Stop(string) error                                            { return nil }

type env struct {
	st *store.Store
	tm *team.Team
	ts *httptest.Server
}

func setup(t *testing.T) *env {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	hub := events.NewHub()
	tm := team.New(st, nopStarter{}, hub, "http://127.0.0.1:0")
	srv := New(st, tm, runner.NewManager("claude-inexistent", t.TempDir()), hub, mcp.NewServer(tm))
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return &env{st: st, tm: tm, ts: ts}
}

func (e *env) do(t *testing.T, method, path string, body any) (int, []byte) {
	t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, e.ts.URL+path, rd)
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	return res.StatusCode, raw
}

func (e *env) createProject(t *testing.T, workdir string) store.Project {
	t.Helper()
	code, raw := e.do(t, "POST", "/api/projects", map[string]any{"name": "ApiUnit", "goal": "Import", "workdir": workdir})
	if code != http.StatusCreated {
		t.Fatalf("creare proiect: %d %s", code, raw)
	}
	var p store.Project
	json.Unmarshal(raw, &p)
	return p
}

func (e *env) addWorker(t *testing.T, projectID int64, name string) store.Agent {
	t.Helper()
	a := store.Agent{ProjectID: projectID, Name: name, Position: store.PositionWorker, Model: "sonnet"}
	if err := e.st.CreateAgent(&a); err != nil {
		t.Fatal(err)
	}
	return a
}

type detail struct {
	Status string `json:"status"`
	Paused bool   `json:"paused"`
	Done   int    `json:"done"`
	Total  int    `json:"total"`
	Team   []struct {
		Name     string `json:"name"`
		Position string `json:"position"`
	} `json:"team"`
	Agents []struct {
		ID          int64  `json:"id"`
		Name        string `json:"name"`
		Position    string `json:"position"`
		Status      string `json:"status"`
		ActiveRunID string `json:"activeRunId"`
	} `json:"agents"`
	Tasks []store.Task `json:"tasks"`
}

func (e *env) detail(t *testing.T, id int64) detail {
	t.Helper()
	code, raw := e.do(t, "GET", "/api/projects/"+itoa(id), nil)
	if code != 200 {
		t.Fatalf("GET proiect: %d %s", code, raw)
	}
	var d detail
	json.Unmarshal(raw, &d)
	return d
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

func TestInfoAndStatic(t *testing.T) {
	e := setup(t)
	if code, raw := e.do(t, "GET", "/api/info", nil); code != 200 || !strings.Contains(string(raw), `"name":"foreman"`) {
		t.Fatalf("%d %s", code, raw)
	}
	if code, raw := e.do(t, "GET", "/", nil); code != 200 || !strings.Contains(string(raw), "<title>") {
		t.Fatalf("index: %d", code)
	}
}

func TestCreateProjectValidation(t *testing.T) {
	e := setup(t)
	cases := []map[string]any{
		{"name": "", "workdir": t.TempDir()},
		{"name": "X", "workdir": "relativ/folder"},
		{"name": "X", "workdir": filepath.Join(t.TempDir(), "nu-exista")},
		{"name": "X", "workdir": t.TempDir(), "maxParallel": 20},
		{"name": "X", "workdir": t.TempDir(), "budgetUsd": -1},
	}
	for _, body := range cases {
		if code, raw := e.do(t, "POST", "/api/projects", body); code != http.StatusBadRequest || !strings.Contains(string(raw), `"error"`) {
			t.Errorf("%v: %d %s", body, code, raw)
		}
	}
}

func TestCreateProjectWorkdirWithSpaces(t *testing.T) {
	e := setup(t)
	dir := filepath.Join(t.TempDir(), "AI Dashboard")
	os.Mkdir(dir, 0o755)
	if p := e.createProject(t, dir); p.Workdir != dir {
		t.Fatalf("workdir = %q", p.Workdir)
	}
}

func TestProjectFieldLimits(t *testing.T) {
	e := setup(t)
	body := map[string]any{"name": "X", "workdir": t.TempDir(), "context": strings.Repeat("a", 20001)}
	if code, _ := e.do(t, "POST", "/api/projects", body); code != http.StatusBadRequest {
		t.Fatalf("context prea lung: %d", code)
	}
}

func TestCreateProjectCreatesLead(t *testing.T) {
	e := setup(t)
	p := e.createProject(t, t.TempDir())
	d := e.detail(t, p.ID)
	if d.Status != "Idle" || len(d.Agents) != 1 || d.Agents[0].Position != "lead" || d.Agents[0].Status != "Idle" || len(d.Tasks) != 0 {
		t.Fatalf("detaliu = %+v", d)
	}
}

func TestProjectsListProgress(t *testing.T) {
	e := setup(t)
	p := e.createProject(t, t.TempDir())
	w := e.addWorker(t, p.ID, "Rook")
	a := store.Task{ProjectID: p.ID, Title: "A", AssigneeID: w.ID}
	b := store.Task{ProjectID: p.ID, Title: "B", AssigneeID: w.ID}
	e.st.CreateTask(&a)
	e.st.CreateTask(&b)
	a.Status = store.StatusDone
	e.st.SaveTask(&a)

	code, raw := e.do(t, "GET", "/api/projects", nil)
	var list []detail
	json.Unmarshal(raw, &list)
	if code != 200 || len(list) != 1 || list[0].Done != 1 || list[0].Total != 2 || list[0].Status != "Running" || len(list[0].Team) != 2 {
		t.Fatalf("%d %s", code, raw)
	}
}

func TestAgentCRUD(t *testing.T) {
	e := setup(t)
	p := e.createProject(t, t.TempDir())
	path := "/api/projects/" + itoa(p.ID) + "/agents"
	good := map[string]any{"name": "Rook", "role": "Backend", "model": "sonnet", "tools": []string{"write", "write", "git"}}

	code, raw := e.do(t, "POST", path, good)
	var a store.Agent
	json.Unmarshal(raw, &a)
	if code != 201 || a.Position != "worker" || len(a.Tools) != 2 {
		t.Fatalf("creare: %d %s", code, raw)
	}
	if code, _ := e.do(t, "POST", path, map[string]any{"name": "rook", "model": "sonnet"}); code != http.StatusConflict {
		t.Errorf("nume duplicat: %d", code)
	}
	if code, _ := e.do(t, "POST", path, map[string]any{"name": "X", "model": "gpt"}); code != http.StatusBadRequest {
		t.Errorf("model gresit: %d", code)
	}
	if code, _ := e.do(t, "POST", path, map[string]any{"name": "X", "model": "haiku", "tools": []string{"root"}}); code != http.StatusBadRequest {
		t.Errorf("tool gresit: %d", code)
	}
	good["role"] = "QA"
	if code, raw := e.do(t, "PUT", "/api/agents/"+itoa(a.ID), good); code != 200 || !strings.Contains(string(raw), `"role":"QA"`) {
		t.Errorf("update: %d %s", code, raw)
	}
	if code, _ := e.do(t, "DELETE", "/api/agents/"+itoa(a.ID), nil); code != http.StatusNoContent {
		t.Errorf("stergere: %d", code)
	}
}

func TestDeleteAgentConflicts(t *testing.T) {
	e := setup(t)
	p := e.createProject(t, t.TempDir())
	w := e.addWorker(t, p.ID, "Rook")
	e.st.CreateTask(&store.Task{ProjectID: p.ID, Title: "A", AssigneeID: w.ID})
	if code, _ := e.do(t, "DELETE", "/api/agents/"+itoa(w.ID), nil); code != http.StatusConflict {
		t.Errorf("worker cu task-uri: %d", code)
	}
	lead, _ := e.st.Lead(p.ID)
	if code, _ := e.do(t, "DELETE", "/api/agents/"+itoa(lead.ID), nil); code != http.StatusConflict {
		t.Errorf("lead: %d", code)
	}
}

func TestPromptPreview(t *testing.T) {
	e := setup(t)
	p := e.createProject(t, t.TempDir())
	code, raw := e.do(t, "POST", "/api/prompt-preview", map[string]any{
		"projectId": p.ID, "name": "Nova", "role": "Frontend Developer", "position": "worker", "background": "Vue 3",
	})
	if code != 200 || !strings.Contains(string(raw), "Esti Nova, Frontend Developer, worker in proiectul ApiUnit. Raportezi lui Lead (lead).") {
		t.Fatalf("%d %s", code, raw)
	}
}

func TestPostMessageToWorker(t *testing.T) {
	e := setup(t)
	p := e.createProject(t, t.TempDir())
	w := e.addWorker(t, p.ID, "Rook")
	if code, _ := e.do(t, "POST", "/api/agents/"+itoa(w.ID)+"/messages", map[string]any{"text": "  "}); code != http.StatusBadRequest {
		t.Errorf("mesaj gol: %d", code)
	}
	code, raw := e.do(t, "POST", "/api/agents/"+itoa(w.ID)+"/messages", map[string]any{"text": "salut"})
	if code != 201 || !strings.Contains(string(raw), `"delivered":false`) {
		t.Fatalf("%d %s", code, raw)
	}
	lead, _ := e.st.Lead(p.ID)
	_, raw = e.do(t, "GET", "/api/agents/"+itoa(lead.ID)+"/messages", nil)
	var msgs []store.Message
	json.Unmarshal(raw, &msgs)
	if len(msgs) != 1 || msgs[0].Text != "Utilizatorul -> Rook: salut" || msgs[0].Delivered {
		t.Fatalf("copia pentru lead lipseste: %s", raw)
	}
}

func TestStopResumeAndBlocked(t *testing.T) {
	e := setup(t)
	p := e.createProject(t, t.TempDir())
	id := itoa(p.ID)
	if code, _ := e.do(t, "POST", "/api/projects/"+id+"/stop", nil); code != http.StatusNoContent {
		t.Fatalf("stop: %d", code)
	}
	if d := e.detail(t, p.ID); !d.Paused || d.Status != "Paused" {
		t.Fatalf("dupa stop: %+v", d)
	}
	e.do(t, "POST", "/api/projects/"+id+"/resume", nil)
	if d := e.detail(t, p.ID); d.Paused {
		t.Fatal("dupa resume inca e pe pauza")
	}
	w := e.addWorker(t, p.ID, "Rook")
	task := store.Task{ProjectID: p.ID, Title: "A", AssigneeID: w.ID}
	e.st.CreateTask(&task)
	if code, _ := e.do(t, "POST", "/api/tasks/"+itoa(task.ID)+"/accept", nil); code != http.StatusConflict {
		t.Errorf("accept pe task neblocat: %d", code)
	}
}

func TestDeleteActiveProject409(t *testing.T) {
	e := setup(t)
	p := e.createProject(t, t.TempDir())
	lead, _ := e.st.Lead(p.ID)
	e.tm.UserMessage(lead.ID, "porneste")
	if err := e.tm.Reconcile(p.ID); err != nil {
		t.Fatal(err)
	}
	if code, _ := e.do(t, "DELETE", "/api/projects/"+itoa(p.ID), nil); code != http.StatusConflict {
		t.Fatalf("stergere cu echipa activa: %d", code)
	}
	if d := e.detail(t, p.ID); d.Agents[0].ActiveRunID == "" || d.Agents[0].Status != "Working" {
		t.Fatalf("lead-ul trebuia sa apara activ: %+v", d.Agents[0])
	}
}

func TestMCPMounted(t *testing.T) {
	e := setup(t)
	if code, _ := e.do(t, "POST", "/mcp/token-necunoscut", map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/list"}); code != http.StatusNotFound {
		t.Fatalf("mcp: %d", code)
	}
}
```

- [ ] **Step 2: Ruleaza testele ca sa vezi ca pica**

Run: `go test ./internal/web/`
Expected: FAIL la compilare (`too many arguments in call to New`).

- [ ] **Step 3: Rescrie `internal/web/server.go`**

```go
// Package web serveste UI-ul, API-ul REST, stream-urile SSE si serverul MCP.
package web

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"strconv"
	"unicode/utf8"

	"foreman/internal/config"
	"foreman/internal/events"
	"foreman/internal/runner"
	"foreman/internal/store"
	"foreman/internal/team"
)

//go:embed static
var staticFS embed.FS

type Server struct {
	st   *store.Store
	team *team.Team
	runs *runner.Manager
	hub  *events.Hub
	mcp  http.Handler
}

func New(st *store.Store, tm *team.Team, runs *runner.Manager, hub *events.Hub, mcpHandler http.Handler) *Server {
	return &Server{st: st, team: tm, runs: runs, hub: hub, mcp: mcpHandler}
}

func (s *Server) Handler() http.Handler {
	static, _ := fs.Sub(staticFS, "static")
	mux := http.NewServeMux()
	mux.Handle("GET /", http.FileServerFS(static))
	mux.HandleFunc("GET /api/info", s.info)

	mux.HandleFunc("GET /api/projects", s.listProjects)
	mux.HandleFunc("POST /api/projects", s.createProject)
	mux.HandleFunc("GET /api/projects/{id}", s.getProject)
	mux.HandleFunc("PUT /api/projects/{id}", s.updateProject)
	mux.HandleFunc("DELETE /api/projects/{id}", s.deleteProject)
	mux.HandleFunc("POST /api/projects/{id}/stop", s.stopProject)
	mux.HandleFunc("POST /api/projects/{id}/resume", s.resumeProject)
	mux.HandleFunc("GET /api/projects/{id}/events", s.projectEvents)

	mux.HandleFunc("POST /api/projects/{id}/agents", s.createAgent)
	mux.HandleFunc("PUT /api/agents/{id}", s.updateAgent)
	mux.HandleFunc("DELETE /api/agents/{id}", s.deleteAgent)
	mux.HandleFunc("GET /api/agents/{id}/messages", s.listMessages)
	mux.HandleFunc("POST /api/agents/{id}/messages", s.postMessage)
	mux.HandleFunc("POST /api/prompt-preview", s.promptPreview)

	mux.HandleFunc("POST /api/tasks/{id}/accept", s.acceptTask)
	mux.HandleFunc("POST /api/tasks/{id}/unblock", s.unblockTask)

	mux.HandleFunc("GET /api/runs/{id}/events", s.streamRun)

	// MCP: POST pentru JSON-RPC; GET doar ca sa raspunda 405 (Claude incearca si GET).
	// Rutele sunt separate pe metoda ca sa nu intre in conflict cu "GET /".
	mux.Handle("POST /mcp/{token}", s.mcp)
	mux.Handle("GET /mcp/{token}", s.mcp)
	return mux
}

func (s *Server) info(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"name": config.AppName})
}

// badRequest e o eroare de validare; mesajul ajunge ca atare in UI (400).
type badRequest string

func (e badRequest) Error() string { return string(e) }

func maxLen(label, value string, max int) error {
	if utf8.RuneCountInString(value) > max {
		return badRequest(fmt.Sprintf("%s: maxim %d de caractere", label, max))
	}
	return nil
}

// fail traduce o eroare intr-un raspuns HTTP.
func fail(w http.ResponseWriter, err error) {
	var br badRequest
	switch {
	case errors.As(err, &br):
		writeErr(w, http.StatusBadRequest, br.Error())
	case errors.Is(err, store.ErrNotFound):
		writeErr(w, http.StatusNotFound, "nu exista")
	case errors.Is(err, team.ErrNotBlocked):
		writeErr(w, http.StatusConflict, err.Error())
	case errors.Is(err, store.ErrConflict):
		writeErr(w, http.StatusConflict, "conflict cu starea curenta")
	default:
		log.Printf("eroare API: %v", err)
		writeErr(w, http.StatusInternalServerError, "eroare interna, vezi logul serverului")
	}
}

func pathID(r *http.Request) (int64, error) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		return 0, badRequest("id invalid")
	}
	return id, nil
}

func decode(r *http.Request, v any) error {
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(v); err != nil {
		return badRequest("JSON invalid")
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
```

- [ ] **Step 4: Scrie `internal/web/sse.go`**

```go
package web

import (
	"fmt"
	"net/http"
	"time"
)

func sseHeaders(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
}

// streamRun trimite evenimentele unui run: tot istoricul, apoi live pana la final.
func (s *Server) streamRun(w http.ResponseWriter, r *http.Request) {
	run, err := s.runs.Get(r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeErr(w, http.StatusInternalServerError, "streaming nesuportat")
		return
	}
	sseHeaders(w)

	replay, ch, done := run.Subscribe()
	for _, line := range replay {
		fmt.Fprintf(w, "data: %s\n\n", line)
	}
	flusher.Flush()
	if done {
		return
	}
	defer run.Unsubscribe(ch)

	ping := time.NewTicker(15 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ping.C:
			fmt.Fprint(w, ": ping\n\n")
			flusher.Flush()
		case line, ok := <-ch:
			if !ok {
				return
			}
			fmt.Fprintf(w, "data: %s\n\n", line)
			flusher.Flush()
		}
	}
}

// projectEvents trimite schimbarile unui proiect (task, mesaj, agent, proiect).
// UI-ul reincarca starea completa la reconectare, deci nu trimitem istoric.
func (s *Server) projectEvents(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		fail(w, err)
		return
	}
	if _, err := s.st.GetProject(id); err != nil {
		fail(w, err)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeErr(w, http.StatusInternalServerError, "streaming nesuportat")
		return
	}
	sseHeaders(w)
	ch, cancel := s.hub.Subscribe(id)
	defer cancel()
	fmt.Fprint(w, ": conectat\n\n")
	flusher.Flush()

	ping := time.NewTicker(15 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ping.C:
			fmt.Fprint(w, ": ping\n\n")
			flusher.Flush()
		case payload, ok := <-ch:
			if !ok {
				return
			}
			fmt.Fprintf(w, "data: %s\n\n", payload)
			flusher.Flush()
		}
	}
}
```

- [ ] **Step 5: Scrie `internal/web/api_projects.go`**

```go
package web

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"foreman/internal/store"
)

type projectInput struct {
	Name        string  `json:"name"`
	Goal        string  `json:"goal"`
	Context     string  `json:"context"`
	Workdir     string  `json:"workdir"`
	MaxParallel int     `json:"maxParallel"`
	BudgetUSD   float64 `json:"budgetUsd"`
}

// toProject valideaza input-ul. Limitele de lungime exista pentru ca aceste
// texte ajung in argumentele lui claude, iar sistemul le limiteaza lungimea.
func (in projectInput) toProject() (store.Project, error) {
	p := store.Project{
		Name:        strings.TrimSpace(in.Name),
		Goal:        strings.TrimSpace(in.Goal),
		Context:     strings.TrimSpace(in.Context),
		MaxParallel: in.MaxParallel,
		BudgetUSD:   in.BudgetUSD,
	}
	if p.Name == "" {
		return p, badRequest("numele proiectului e obligatoriu")
	}
	for _, err := range []error{
		maxLen("numele proiectului", p.Name, 80),
		maxLen("obiectivul", p.Goal, 2000),
		maxLen("contextul comun", p.Context, 20000),
	} {
		if err != nil {
			return p, err
		}
	}
	dir, err := expandHome(strings.TrimSpace(in.Workdir))
	if err != nil {
		return p, err
	}
	if !filepath.IsAbs(dir) {
		return p, badRequest("folderul de lucru trebuie sa fie o cale absoluta, ex. /Users/tu/proiect sau ~/proiect")
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return p, badRequest("folderul de lucru nu exista: " + dir)
	}
	p.Workdir = filepath.Clean(dir)
	if p.MaxParallel == 0 {
		p.MaxParallel = 2
	}
	if p.MaxParallel < 1 || p.MaxParallel > 8 {
		return p, badRequest("workeri in paralel: intre 1 si 8")
	}
	if p.BudgetUSD == 0 {
		p.BudgetUSD = 1
	}
	if p.BudgetUSD < 0.05 || p.BudgetUSD > 100 {
		return p, badRequest("bugetul per run: intre 0.05 si 100 USD")
	}
	return p, nil
}

// expandHome transforma "~/proiect" in cale absoluta.
func expandHome(path string) (string, error) {
	if path == "~" || strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, strings.TrimPrefix(path, "~")), nil
	}
	return path, nil
}

type memberBrief struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Position string `json:"position"`
}

type projectCard struct {
	store.Project
	Status string        `json:"status"` // Running | Idle | Paused
	Team   []memberBrief `json:"team"`
	Done   int           `json:"done"`
	Total  int           `json:"total"`
}

type agentView struct {
	store.Agent
	Status      string      `json:"status"`
	ActiveRunID string      `json:"activeRunId"`
	CurrentTask *store.Task `json:"currentTask"`
}

type projectDetail struct {
	projectCard
	Agents []agentView  `json:"agents"`
	Tasks  []store.Task `json:"tasks"`
}

// card calculeaza ce se afiseaza pe cardul proiectului.
func (s *Server) card(p store.Project) (projectCard, []store.Agent, []store.Task, error) {
	agents, err := s.st.ListAgents(p.ID)
	if err != nil {
		return projectCard{}, nil, nil, err
	}
	tasks, err := s.st.ListTasks(p.ID)
	if err != nil {
		return projectCard{}, nil, nil, err
	}
	c := projectCard{Project: p, Team: []memberBrief{}, Total: len(tasks), Status: "Idle"}
	for _, a := range agents {
		c.Team = append(c.Team, memberBrief{ID: a.ID, Name: a.Name, Position: a.Position})
	}
	busy := s.team.ProjectActive(p.ID)
	for _, t := range tasks {
		switch t.Status {
		case store.StatusDone:
			c.Done++
		case store.StatusQueued, store.StatusInProgress:
			busy = true
		}
	}
	switch {
	case p.Paused:
		c.Status = "Paused"
	case busy:
		c.Status = "Running"
	}
	return c, agents, tasks, nil
}

func (s *Server) listProjects(w http.ResponseWriter, r *http.Request) {
	projects, err := s.st.ListProjects()
	if err != nil {
		fail(w, err)
		return
	}
	out := []projectCard{}
	for _, p := range projects {
		c, _, _, err := s.card(p)
		if err != nil {
			fail(w, err)
			return
		}
		out = append(out, c)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) getProject(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		fail(w, err)
		return
	}
	p, err := s.st.GetProject(id)
	if err != nil {
		fail(w, err)
		return
	}
	c, agents, tasks, err := s.card(p)
	if err != nil {
		fail(w, err)
		return
	}
	d := projectDetail{projectCard: c, Agents: []agentView{}, Tasks: tasks}
	for _, a := range agents {
		status, current := s.team.AgentStatus(a, tasks)
		d.Agents = append(d.Agents, agentView{Agent: a, Status: status, ActiveRunID: s.team.ActiveRunID(a.ID), CurrentTask: current})
	}
	writeJSON(w, http.StatusOK, d)
}

func (s *Server) createProject(w http.ResponseWriter, r *http.Request) {
	var in projectInput
	if err := decode(r, &in); err != nil {
		fail(w, err)
		return
	}
	p, err := in.toProject()
	if err != nil {
		fail(w, err)
		return
	}
	if _, err := s.st.CreateProject(&p); err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, p)
}

func (s *Server) updateProject(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		fail(w, err)
		return
	}
	existing, err := s.st.GetProject(id)
	if err != nil {
		fail(w, err)
		return
	}
	var in projectInput
	if err := decode(r, &in); err != nil {
		fail(w, err)
		return
	}
	p, err := in.toProject()
	if err != nil {
		fail(w, err)
		return
	}
	p.ID, p.Paused, p.CreatedAt = existing.ID, existing.Paused, existing.CreatedAt
	if err := s.st.UpdateProject(p); err != nil {
		fail(w, err)
		return
	}
	s.hub.Publish(id, "project", map[string]int64{"id": id})
	s.team.Kick(id) // poate a crescut numarul de workeri in paralel
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) deleteProject(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		fail(w, err)
		return
	}
	if s.team.ProjectActive(id) {
		writeErr(w, http.StatusConflict, "echipa lucreaza acum; opreste echipa inainte sa stergi proiectul")
		return
	}
	if err := s.st.DeleteProject(id); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) stopProject(w http.ResponseWriter, r *http.Request) {
	s.projectAction(w, r, s.team.Stop)
}

func (s *Server) resumeProject(w http.ResponseWriter, r *http.Request) {
	s.projectAction(w, r, s.team.Resume)
}

func (s *Server) projectAction(w http.ResponseWriter, r *http.Request, action func(int64) error) {
	id, err := pathID(r)
	if err != nil {
		fail(w, err)
		return
	}
	if err := action(id); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
```

- [ ] **Step 6: Scrie `internal/web/api_agents.go`**

```go
package web

import (
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"foreman/internal/prompt"
	"foreman/internal/store"
)

var models = []string{"opus", "sonnet", "haiku"}

type agentInput struct {
	Name         string   `json:"name"`
	Role         string   `json:"role"`
	Model        string   `json:"model"`
	Background   string   `json:"background"`
	Instructions string   `json:"instructions"`
	DoneDef      string   `json:"doneDef"`
	Tools        []string `json:"tools"`
}

// apply valideaza input-ul si il copiaza in agent (fara pozitie si proiect).
func (in agentInput) apply(a *store.Agent) error {
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return badRequest("numele agentului e obligatoriu")
	}
	if !slices.Contains(models, in.Model) {
		return badRequest("model necunoscut; alege opus, sonnet sau haiku")
	}
	tools := []string{}
	for _, tool := range in.Tools {
		if !slices.Contains(prompt.ValidTools, tool) {
			return badRequest("tool necunoscut: " + tool)
		}
		if !slices.Contains(tools, tool) {
			tools = append(tools, tool)
		}
	}
	a.Name, a.Role, a.Model, a.Tools = name, strings.TrimSpace(in.Role), in.Model, tools
	a.Background = strings.TrimSpace(in.Background)
	a.Instructions = strings.TrimSpace(in.Instructions)
	a.DoneDef = strings.TrimSpace(in.DoneDef)
	for _, err := range []error{
		maxLen("numele agentului", a.Name, 40),
		maxLen("rolul", a.Role, 80),
		maxLen("background", a.Background, 10000),
		maxLen("instructiunile", a.Instructions, 10000),
		maxLen("definition of done", a.DoneDef, 4000),
	} {
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *Server) createAgent(w http.ResponseWriter, r *http.Request) {
	pid, err := pathID(r)
	if err != nil {
		fail(w, err)
		return
	}
	if _, err := s.st.GetProject(pid); err != nil {
		fail(w, err)
		return
	}
	var in agentInput
	if err := decode(r, &in); err != nil {
		fail(w, err)
		return
	}
	a := store.Agent{ProjectID: pid, Position: store.PositionWorker}
	if err := in.apply(&a); err != nil {
		fail(w, err)
		return
	}
	if err := s.st.CreateAgent(&a); errors.Is(err, store.ErrConflict) {
		writeErr(w, http.StatusConflict, fmt.Sprintf("exista deja un agent cu numele %s in proiect", a.Name))
		return
	} else if err != nil {
		fail(w, err)
		return
	}
	s.hub.Publish(pid, "agent", map[string]int64{"id": a.ID})
	writeJSON(w, http.StatusCreated, a)
}

func (s *Server) updateAgent(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		fail(w, err)
		return
	}
	a, err := s.st.GetAgent(id)
	if err != nil {
		fail(w, err)
		return
	}
	var in agentInput
	if err := decode(r, &in); err != nil {
		fail(w, err)
		return
	}
	if err := in.apply(&a); err != nil {
		fail(w, err)
		return
	}
	if err := s.st.UpdateAgent(a); errors.Is(err, store.ErrConflict) {
		writeErr(w, http.StatusConflict, fmt.Sprintf("exista deja un agent cu numele %s in proiect", a.Name))
		return
	} else if err != nil {
		fail(w, err)
		return
	}
	s.hub.Publish(a.ProjectID, "agent", map[string]int64{"id": a.ID})
	writeJSON(w, http.StatusOK, a)
}

func (s *Server) deleteAgent(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		fail(w, err)
		return
	}
	a, err := s.st.GetAgent(id)
	if err != nil {
		fail(w, err)
		return
	}
	if s.team.ActiveRunID(id) != "" {
		writeErr(w, http.StatusConflict, "agentul lucreaza acum; opreste echipa inainte sa il stergi")
		return
	}
	if err := s.st.DeleteAgent(id); errors.Is(err, store.ErrConflict) {
		writeErr(w, http.StatusConflict, "lead-ul si agentii care au task-uri nu pot fi stersi")
		return
	} else if err != nil {
		fail(w, err)
		return
	}
	s.hub.Publish(a.ProjectID, "agent", map[string]int64{"id": id})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) listMessages(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		fail(w, err)
		return
	}
	if _, err := s.st.GetAgent(id); err != nil {
		fail(w, err)
		return
	}
	msgs, err := s.st.ListMessages(id)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, msgs)
}

func (s *Server) postMessage(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		fail(w, err)
		return
	}
	var in struct {
		Text string `json:"text"`
	}
	if err := decode(r, &in); err != nil {
		fail(w, err)
		return
	}
	text := strings.TrimSpace(in.Text)
	if text == "" {
		fail(w, badRequest("mesajul e gol"))
		return
	}
	if err := maxLen("mesajul", text, 10000); err != nil {
		fail(w, err)
		return
	}
	m, err := s.team.UserMessage(id, text)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, m)
}

// promptPreview compileaza system prompt-ul din campurile nesalvate ale editorului.
func (s *Server) promptPreview(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ProjectID    int64  `json:"projectId"`
		Name         string `json:"name"`
		Role         string `json:"role"`
		Position     string `json:"position"`
		Background   string `json:"background"`
		Instructions string `json:"instructions"`
		DoneDef      string `json:"doneDef"`
	}
	if err := decode(r, &in); err != nil {
		fail(w, err)
		return
	}
	p, err := s.st.GetProject(in.ProjectID)
	if err != nil {
		fail(w, err)
		return
	}
	a := store.Agent{
		Name: strings.TrimSpace(in.Name), Role: strings.TrimSpace(in.Role), Position: in.Position,
		Background: in.Background, Instructions: in.Instructions, DoneDef: in.DoneDef,
	}
	if a.Name == "" {
		a.Name = "Agent"
	}
	leadName := a.Name
	if a.Position != store.PositionLead {
		a.Position = store.PositionWorker
		lead, err := s.st.Lead(p.ID)
		if err != nil {
			fail(w, err)
			return
		}
		leadName = lead.Name
	}
	writeJSON(w, http.StatusOK, map[string]string{"prompt": prompt.System(p, a, leadName)})
}
```

- [ ] **Step 7: Scrie `internal/web/api_tasks.go`**

```go
package web

import "net/http"

// Butoanele de pe task-urile blocate: utilizatorul decide.
func (s *Server) acceptTask(w http.ResponseWriter, r *http.Request) {
	s.taskAction(w, r, s.team.AcceptBlocked)
}

func (s *Server) unblockTask(w http.ResponseWriter, r *http.Request) {
	s.taskAction(w, r, s.team.Unblock)
}

func (s *Server) taskAction(w http.ResponseWriter, r *http.Request, action func(int64) error) {
	id, err := pathID(r)
	if err != nil {
		fail(w, err)
		return
	}
	if err := action(id); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
```

- [ ] **Step 8: Rescrie `cmd/foreman/main.go`**

```go
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"foreman/internal/config"
	"foreman/internal/events"
	"foreman/internal/mcp"
	"foreman/internal/runner"
	"foreman/internal/store"
	"foreman/internal/team"
	"foreman/internal/web"
)

const usage = `%[1]s - echipe de agenti Claude Code, local

Utilizare:
  %[1]s start [--port 7777] [--no-open]
`

func main() {
	if len(os.Args) < 2 {
		fmt.Printf(usage, config.AppName)
		os.Exit(1)
	}
	var err error
	switch os.Args[1] {
	case "start":
		err = start(os.Args[2:])
	default:
		fmt.Printf(usage, config.AppName)
		os.Exit(1)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "eroare:", err)
		os.Exit(1)
	}
}

func start(args []string) error {
	fl := flag.NewFlagSet("start", flag.ExitOnError)
	port := fl.Int("port", 7777, "portul pe care asculta UI-ul")
	noOpen := fl.Bool("no-open", false, "nu deschide browserul automat")
	fl.Parse(args)

	paths, err := config.Default()
	if err != nil {
		return err
	}
	if err := paths.Ensure(); err != nil {
		return fmt.Errorf("nu pot crea %s: %w", paths.Root, err)
	}
	claudeBin, err := exec.LookPath("claude")
	if err != nil {
		return errors.New("nu gasesc 'claude' in PATH. Instaleaza Claude Code si incearca din nou")
	}
	st, err := store.Open(paths.DB)
	if err != nil {
		return err
	}
	defer st.Close()

	// contextul se anuleaza la Ctrl+C / SIGTERM si ajunge in toate request-urile,
	// deci conexiunile SSE se inchid singure la oprire
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// deschidem portul primul, ca sa esuam clar daca e ocupat
	addr := fmt.Sprintf("127.0.0.1:%d", *port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("portul %d e ocupat, incearca --port: %w", *port, err)
	}
	url := "http://" + addr

	hub := events.NewHub()
	mgr := runner.NewManager(claudeBin, paths.Logs)
	tm := team.New(st, mgr, hub, url)
	// dupa un restart: run-urile vechi devin "interrupted" si totul sta pe pauza
	if err := tm.Recover(); err != nil {
		return fmt.Errorf("recuperarea starii: %w", err)
	}
	go tm.Run(ctx)

	srv := &http.Server{
		Handler:           web.New(st, tm, mgr, hub, mcp.NewServer(tm)).Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		BaseContext:       func(net.Listener) context.Context { return ctx },
	}

	log.Printf("%s ruleaza pe %s", config.AppName, url)
	log.Printf("date: %s", paths.Root)
	log.Printf("claude: %s", claudeBin)
	if !*noOpen {
		openBrowser(url)
	}

	errCh := make(chan error, 1)
	go func() { errCh <- srv.Serve(ln) }()

	select {
	case <-ctx.Done():
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	}

	log.Println("opresc agentii activi...")
	mgr.Shutdown()
	shCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return srv.Shutdown(shCtx)
}

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	if err := cmd.Start(); err != nil {
		log.Printf("deschide manual: %s", url)
	}
}
```

- [ ] **Step 9: Ruleaza toate testele**

Run: `go vet ./... && go build -race ./... && go test -race ./...`
Expected: PASS peste tot (web: 13 teste).

- [ ] **Step 10: Verificare manuala a binarului (fara Claude real)**

Serverul se porneste cu `HOME` temporar, ca sa nu atingi `~/.foreman` real:

```bash
go build -o /tmp/foreman-test ./cmd/foreman
TESTHOME=$(mktemp -d); mkdir -p "$TESTHOME/proiect"
HOME=$TESTHOME /tmp/foreman-test start --port 7788 --no-open &
sleep 1
curl -s localhost:7788/api/info
curl -s -X POST localhost:7788/api/projects -d "{\"name\":\"Test\",\"workdir\":\"$TESTHOME/proiect\"}"
curl -s -X POST localhost:7788/api/projects/1/agents -d '{"name":"Rook","model":"haiku","tools":["write"]}'
curl -s localhost:7788/api/projects/1 | head -c 600; echo
curl -s -o /dev/null -w "mcp necunoscut: %{http_code}\n" -X POST localhost:7788/mcp/x -d '{}'
kill %1
```

Expected:
- `{"name":"foreman"}`;
- proiectul cu `"id":1`;
- agentul Rook cu `"position":"worker"`;
- detaliul cu doi agenti, status `Idle`;
- `mcp necunoscut: 404`.

In log apare `foreman ruleaza pe http://127.0.0.1:7788`.

- [ ] **Step 11: Commit**

```bash
git add -A
git commit -m "web: API complet (proiecte, agenti, mesaje, task-uri, SSE) si legarea in main"
```

---
### Task 11: UI: schelet, stil, proiecte, formular proiect, editor agent

**Files:**
- Modify (rescriere): `internal/web/static/index.html`
- Create: `internal/web/static/app.css`
- Create: `internal/web/static/js/api.js`
- Create: `internal/web/static/js/ui.js`
- Create: `internal/web/static/js/app.js`
- Create: `internal/web/static/js/projects.js`
- Create: `internal/web/static/js/agent.js`
- Create: `internal/web/static/fonts/*.woff2` (descarcate)

**Interfaces:**
- Consumes (Task 10): rutele API, cu campurile JSON `id`, `name`, `goal`, `context`, `workdir`, `paused`, `maxParallel`, `budgetUsd`, `status`, `team[{id,name,position}]`, `done` si `total`. Pentru agent: `agents[{id,name,role,position,model,background,instructions,doneDef,tools,status,activeRunId,currentTask}]`. Plus `tasks[]` si `POST /api/prompt-preview` care intoarce `{prompt}`.
- Produces (pentru Task 12):
  - in `ui.js`: `el(tag, attrs, ...children)`, `avatar(name, position, size)`, `pill(status)`, `field(label, input, hint)`, `toast(message)`, `clock(iso)`, `MODELS`, `TOOLS`;
  - in `api.js`: `api(path, {method, body})`;
  - in `app.js`: lista `routes` de perechi `[regex, (root, match) => view(...)]`. Routerul apeleaza functia de curatare intoarsa de view cand se schimba ruta.
- Workspace-ul (`#/p/{id}`) apare in Task 12. Pana atunci, ruta necunoscuta te duce inapoi la lista de proiecte.

- [ ] **Step 1: Descarca fonturile (fara ele UI-ul foloseste fonturile sistemului)**

```bash
cd "/Users/yzy/YzY/AI Dashboard/agentdash"
F=internal/web/static/fonts; mkdir -p $F
B=https://cdn.jsdelivr.net/npm/@fontsource
curl -fsSL -o $F/space-grotesk-600.woff2  $B/space-grotesk@5/files/space-grotesk-latin-600-normal.woff2
curl -fsSL -o $F/ibm-plex-sans-400.woff2  $B/ibm-plex-sans@5/files/ibm-plex-sans-latin-400-normal.woff2
curl -fsSL -o $F/ibm-plex-sans-600.woff2  $B/ibm-plex-sans@5/files/ibm-plex-sans-latin-600-normal.woff2
curl -fsSL -o $F/jetbrains-mono-400.woff2 $B/jetbrains-mono@5/files/jetbrains-mono-latin-400-normal.woff2
ls -la $F
```

Expected: 4 fisiere de 13-25 KB fiecare.

- [ ] **Step 2: Scrie `internal/web/static/index.html`**

```html
<!doctype html>
<html lang="ro">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Foreman</title>
<link rel="stylesheet" href="app.css">
<script type="module" src="js/app.js"></script>
</head>
<body>
<div class="shell">
  <aside class="sidebar">
    <a class="brand" href="#/"><span class="logo" aria-hidden="true">F</span><span id="app-name">Foreman</span></a>
    <nav><a class="nav-item" href="#/" aria-current="page">Proiecte</a></nav>
  </aside>
  <main id="view" class="view"></main>
</div>
<div id="toast" class="toast" role="status" hidden></div>
</body>
</html>
```

- [ ] **Step 3: Scrie `internal/web/static/app.css`**

```css
@font-face { font-family: "Space Grotesk"; src: url("fonts/space-grotesk-600.woff2") format("woff2"); font-weight: 600; font-display: swap; }
@font-face { font-family: "IBM Plex Sans"; src: url("fonts/ibm-plex-sans-400.woff2") format("woff2"); font-weight: 400; font-display: swap; }
@font-face { font-family: "IBM Plex Sans"; src: url("fonts/ibm-plex-sans-600.woff2") format("woff2"); font-weight: 600; font-display: swap; }
@font-face { font-family: "JetBrains Mono"; src: url("fonts/jetbrains-mono-400.woff2") format("woff2"); font-weight: 400; font-display: swap; }

/* paleta din mockup-urile Foreman: amber = lead, teal = worker */
:root {
  --bg: #121514; --panel: #181b1a; --card: #1a1d1c; --card-2: #1c201f;
  --line: #2a2f2d; --line-2: #3a403d;
  --text: #ecebe6; --soft: #c9cbc4; --muted: #9aa19d; --dim: #5b625e;
  --amber: #e5a54b; --amber-bg: #211e17; --amber-ink: #1a1408;
  --teal: #6cc3b4; --teal-ink: #0d1a18; --red: #e0736a;
  --me: #2b2519; --them: #1f2322;
  --radius: 12px;
  --sans: "IBM Plex Sans", system-ui, sans-serif;
  --display: "Space Grotesk", var(--sans);
  --mono: "JetBrains Mono", ui-monospace, monospace;
}
* { box-sizing: border-box; }
html, body { margin: 0; min-height: 100%; }
body { background: var(--bg); color: var(--text); font: 14px/1.5 var(--sans); }
a { color: inherit; }
h1, h2 { font-family: var(--display); font-weight: 600; margin: 0; }
h1 { font-size: 26px; }
h2 { font-size: 16px; }
.muted { color: var(--muted); }
.small { font-size: 12px; }
.hint { font-size: 12px; color: var(--muted); }

/* schelet */
.shell { display: grid; grid-template-columns: 220px minmax(0, 1fr); min-height: 100vh; }
.sidebar { background: var(--panel); border-right: 1px solid var(--line); padding: 18px 14px; display: flex; flex-direction: column; gap: 18px; }
.brand { display: flex; align-items: center; gap: 10px; text-decoration: none; font-family: var(--display); font-weight: 600; font-size: 18px; }
.logo { width: 30px; height: 30px; border-radius: 8px; background: var(--amber); color: var(--amber-ink); display: grid; place-items: center; }
.nav-item { display: block; padding: 8px 10px; border-radius: 8px; text-decoration: none; color: var(--soft); }
.nav-item[aria-current="page"] { background: var(--amber-bg); color: var(--text); }
.view { padding: 28px 32px; min-width: 0; }

.page-head { display: flex; justify-content: space-between; align-items: flex-end; gap: 16px; margin-bottom: 22px; flex-wrap: wrap; }
.page-head p { margin: 6px 0 0; }
.crumbs { font-size: 13px; color: var(--muted); margin-bottom: 6px; }
.crumbs a { color: var(--muted); text-decoration: none; }
.crumbs a:hover { color: var(--text); }
.actions { display: flex; gap: 8px; align-items: center; flex-wrap: wrap; }

.btn { display: inline-flex; align-items: center; gap: 6px; height: 36px; padding: 0 14px; border-radius: 9px; border: 1px solid var(--line-2); background: var(--card-2); color: var(--text); font: 600 13px var(--sans); text-decoration: none; cursor: pointer; white-space: nowrap; }
.btn:hover { border-color: var(--muted); }
.btn.primary { background: var(--amber); border-color: var(--amber); color: var(--amber-ink); }
.btn.danger { color: var(--red); }
.btn.small { height: 28px; padding: 0 10px; font-size: 12px; }
.btn:disabled { opacity: .5; cursor: default; }

/* proiecte */
.grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(290px, 1fr)); gap: 16px; }
.card { background: var(--card); border: 1px solid var(--line); border-radius: var(--radius); padding: 16px; text-decoration: none; display: flex; flex-direction: column; gap: 12px; }
a.card:hover { border-color: var(--line-2); }
.card-top { display: flex; justify-content: space-between; align-items: center; gap: 8px; }
.card-title { font-family: var(--display); font-size: 17px; font-weight: 600; }
.clamp { display: -webkit-box; -webkit-line-clamp: 2; -webkit-box-orient: vertical; overflow: hidden; margin: 0; }
.card-new { align-items: center; justify-content: center; border-style: dashed; text-align: center; min-height: 190px; }
.plus { width: 40px; height: 40px; border-radius: 50%; border: 1px solid var(--line-2); display: grid; place-items: center; font-size: 22px; color: var(--amber); }
.pill { display: inline-flex; align-items: center; height: 24px; padding: 0 10px; border-radius: 12px; font-size: 12px; font-weight: 600; white-space: nowrap; }
.pill-running { background: rgba(108, 195, 180, .14); color: var(--teal); }
.pill-idle { background: #262b29; color: var(--muted); }
.pill-paused { background: rgba(229, 165, 75, .14); color: var(--amber); }
.avatar { --size: 32px; width: var(--size); height: var(--size); flex-shrink: 0; border-radius: 50%; display: inline-grid; place-items: center; font-weight: 600; font-size: calc(var(--size) * .42); }
.avatar.lead { background: var(--amber); color: var(--amber-ink); }
.avatar.worker { background: var(--teal); color: var(--teal-ink); }
.avatars { display: flex; align-items: center; gap: 10px; }
.stack { display: flex; }
.stack .avatar + .avatar { margin-left: -8px; box-shadow: 0 0 0 2px var(--card); }
.progress-row { display: grid; gap: 6px; }
.bar { height: 6px; border-radius: 3px; background: #262b29; overflow: hidden; }
.bar-fill { height: 100%; background: var(--dim); border-radius: 3px; }
.bar-fill.live { background: var(--teal); }

/* formulare */
.form { display: grid; gap: 18px; max-width: 1100px; }
.form-cols { display: grid; grid-template-columns: repeat(auto-fit, minmax(320px, 1fr)); gap: 18px; align-items: start; }
.section { background: var(--card); border: 1px solid var(--line); border-radius: var(--radius); padding: 18px; display: grid; gap: 14px; min-width: 0; }
.section-head { display: flex; justify-content: space-between; align-items: center; gap: 10px; flex-wrap: wrap; }
.field { display: grid; gap: 6px; }
.field label, .label { font-size: 13px; color: var(--soft); font-weight: 600; }
input[type=text], input[type=number], textarea, select { width: 100%; background: var(--bg); color: var(--text); border: 1px solid var(--line-2); border-radius: 9px; padding: 9px 11px; font: 14px var(--sans); }
textarea { min-height: 90px; resize: vertical; }
input:focus, textarea:focus, select:focus { outline: none; border-color: var(--amber); }
.row { display: grid; grid-template-columns: 1fr 1fr; gap: 14px; }
.check { display: flex; gap: 10px; align-items: center; cursor: pointer; }
.check input { accent-color: var(--amber); width: 16px; height: 16px; }
.preview { font-family: var(--mono); font-size: 12px; white-space: pre-wrap; background: var(--bg); border: 1px solid var(--line); border-radius: 9px; padding: 12px; max-height: 460px; overflow: auto; color: var(--soft); margin: 0; }

/* workspace (Task 12) */
.ws { display: grid; grid-template-columns: minmax(0, 1fr) 380px; gap: 18px; align-items: start; }
.ws-main { display: grid; gap: 18px; min-width: 0; }
.team { display: grid; gap: 12px; }
.team-workers { display: grid; grid-template-columns: repeat(auto-fill, minmax(210px, 1fr)); gap: 10px; }
.member-cell { display: grid; gap: 4px; min-width: 0; }
.member-cell.lead { max-width: 380px; }
.member { display: flex; gap: 10px; align-items: flex-start; text-align: left; background: var(--card-2); border: 1px solid var(--line); border-radius: var(--radius); padding: 12px; color: var(--text); font: inherit; cursor: pointer; width: 100%; }
.member[aria-pressed="true"] { border-color: var(--amber); background: var(--amber-bg); }
.member-body { display: grid; gap: 3px; min-width: 0; flex: 1; }
.member-name { font-weight: 600; display: flex; gap: 6px; align-items: center; }
.tag { font-size: 11px; padding: 1px 7px; border-radius: 9px; background: rgba(229, 165, 75, .16); color: var(--amber); font-weight: 600; }
.status { display: flex; gap: 6px; align-items: center; font-size: 12px; color: var(--soft); min-width: 0; }
.status span:last-child { overflow: hidden; white-space: nowrap; text-overflow: ellipsis; }
.dot { width: 7px; height: 7px; border-radius: 50%; background: var(--dim); flex-shrink: 0; }
.dot.working { background: var(--teal); box-shadow: 0 0 0 3px rgba(108, 195, 180, .18); }
.dot.queued { background: var(--muted); }
.edit-link { font-size: 12px; color: var(--muted); justify-self: end; }
.board { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); gap: 12px; }
.col { background: var(--panel); border: 1px solid var(--line); border-radius: var(--radius); padding: 10px; display: flex; flex-direction: column; gap: 8px; min-height: 120px; min-width: 0; }
.col-head { display: flex; justify-content: space-between; font-size: 12px; font-weight: 600; color: var(--soft); text-transform: uppercase; letter-spacing: .04em; padding: 2px 4px; }
.task { background: var(--card-2); border: 1px solid var(--line); border-radius: 10px; padding: 10px; }
.task summary { list-style: none; cursor: pointer; display: grid; gap: 8px; }
.task summary::-webkit-details-marker { display: none; }
.task-title { font-size: 13px; overflow-wrap: anywhere; }
.task-id { font-family: var(--mono); font-size: 11px; color: var(--muted); margin-right: 6px; }
.task-who { display: flex; gap: 6px; align-items: center; font-size: 12px; color: var(--muted); }
.task-detail { font-size: 12px; color: var(--soft); white-space: pre-wrap; overflow-wrap: anywhere; border-top: 1px solid var(--line); margin-top: 8px; padding-top: 8px; display: grid; gap: 6px; }
.badge-blocked { font-size: 11px; font-weight: 600; color: var(--red); margin-left: auto; }
.console { position: sticky; top: 16px; background: var(--card); border: 1px solid var(--line); border-radius: var(--radius); display: flex; flex-direction: column; height: calc(100vh - 56px); min-height: 420px; }
.console-head { display: flex; gap: 10px; align-items: center; padding: 14px; border-bottom: 1px solid var(--line); }
.thread { flex: 1; overflow-y: auto; padding: 14px; display: flex; flex-direction: column; gap: 10px; }
.msg { display: flex; flex-direction: column; gap: 4px; align-items: flex-start; }
.msg.me { align-items: flex-end; }
.msg-meta { font-size: 11px; color: var(--muted); }
.bubble { max-width: 320px; padding: 9px 12px; border-radius: 12px; background: var(--them); white-space: pre-wrap; overflow-wrap: anywhere; }
.msg.me .bubble { background: var(--me); color: #f1e6d3; }
.pending { font-size: 11px; color: var(--amber); }
.event { font-size: 12px; color: var(--muted); text-align: center; padding: 2px 8px; overflow-wrap: anywhere; }
.live { border: 1px dashed var(--line-2); border-radius: 10px; padding: 10px; display: grid; gap: 6px; font-size: 12px; }
.live-head { color: var(--teal); font-weight: 600; }
.live-text { white-space: pre-wrap; overflow-wrap: anywhere; }
.live details { background: var(--bg); border-radius: 6px; padding: 4px 8px; }
.live summary { cursor: pointer; font-family: var(--mono); font-size: 11px; color: var(--soft); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.live pre { margin: 6px 0 0; white-space: pre-wrap; font-size: 11px; max-height: 240px; overflow: auto; }
.err { color: var(--red); }
.composer { border-top: 1px solid var(--line); padding: 12px; display: grid; gap: 6px; }
.composer form { display: flex; gap: 8px; align-items: flex-end; }
.composer textarea { min-height: 40px; height: 40px; }
.empty { color: var(--muted); text-align: center; margin: auto; padding: 20px; }

.toast { position: fixed; bottom: 20px; right: 20px; background: #3a2220; color: #f3d6d2; border: 1px solid #5c302c; padding: 10px 14px; border-radius: 10px; max-width: 420px; z-index: 10; }

@media (max-width: 1100px) {
  .ws { grid-template-columns: minmax(0, 1fr); }
  .console { position: static; height: 560px; }
  .board { grid-template-columns: repeat(2, minmax(0, 1fr)); }
}
@media (max-width: 720px) {
  .shell { grid-template-columns: minmax(0, 1fr); }
  .sidebar { flex-direction: row; align-items: center; }
  .view { padding: 18px 16px; }
  .board, .row { grid-template-columns: minmax(0, 1fr); }
}
```

- [ ] **Step 4: Scrie `internal/web/static/js/api.js`**

```js
// api face un request JSON catre server si arunca o eroare cu mesajul
// serverului (in romana) daca raspunsul nu e ok.
export async function api(path, { method = "GET", body } = {}) {
  const res = await fetch(path, {
    method,
    headers: body ? { "Content-Type": "application/json" } : {},
    body: body ? JSON.stringify(body) : undefined,
  });
  if (res.status === 204 || res.status === 202) return null;
  const data = await res.json().catch(() => ({}));
  if (!res.ok) throw new Error(data.error || res.statusText);
  return data;
}
```

- [ ] **Step 5: Scrie `internal/web/static/js/ui.js`**

```js
// Utilitare mici pentru construit DOM fara framework.

// el("div", { class: "x", onclick: fn }, copii...) creeaza un element.
// Textul se pune mereu ca text, nu ca HTML, deci continutul venit de la
// agenti nu poate injecta cod in pagina.
export function el(tag, attrs = {}, ...children) {
  const node = document.createElement(tag);
  let value;
  for (const [key, val] of Object.entries(attrs || {})) {
    if (val == null || val === false) continue;
    if (key === "class") node.className = val;
    else if (key === "value") value = val; // se seteaza dupa copii (conteaza la <select>)
    else if (key.startsWith("on")) node.addEventListener(key.slice(2), val);
    else node.setAttribute(key, val === true ? "" : val);
  }
  for (const child of children.flat(Infinity)) {
    if (child == null || child === false) continue;
    node.append(child instanceof Node ? child : String(child));
  }
  if (value !== undefined) node.value = value;
  return node;
}

export function avatar(name, position, size = 32) {
  return el("span", {
    class: `avatar ${position === "lead" ? "lead" : "worker"}`,
    style: `--size:${size}px`,
    "aria-hidden": "true",
  }, (name || "?").charAt(0).toUpperCase());
}

export const STATUS_LABELS = {
  Running: "Ruleaza", Idle: "Inactiv", Paused: "Pe pauza",
  Working: "Lucreaza", Queued: "In asteptare",
};

export function pill(status) {
  return el("span", { class: `pill pill-${status.toLowerCase()}` }, STATUS_LABELS[status] || status);
}

let fieldSeq = 0;
export function field(label, input, hint) {
  if (!input.id) input.id = `f-${++fieldSeq}`;
  return el("div", { class: "field" },
    el("label", { for: input.id }, label),
    input,
    hint && el("span", { class: "hint" }, hint));
}

let toastTimer;
export function toast(message) {
  const box = document.getElementById("toast");
  box.textContent = message;
  box.hidden = false;
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => { box.hidden = true; }, 5000);
}

export function clock(iso) {
  return new Date(iso).toLocaleTimeString("ro-RO", { hour: "2-digit", minute: "2-digit" });
}

export const MODELS = ["opus", "sonnet", "haiku"];

export const TOOLS = [
  { id: "write", label: "Scrie fisiere" },
  { id: "shell", label: "Comenzi shell" },
  { id: "git", label: "Git: status, diff, commit, branch" },
  { id: "web", label: "Cautare web" },
];
```

- [ ] **Step 6: Scrie `internal/web/static/js/app.js`**

```js
import { api } from "./api.js";
import { toast } from "./ui.js";
import { projectsView, projectFormView } from "./projects.js";
import { agentView } from "./agent.js";

// Rute in hash: #/, #/new, #/p/3/edit, #/p/3/agent/new, #/p/3/agent/7.
// Fiecare view primeste elementul in care deseneaza si poate intoarce o
// functie de curatare (inchide stream-uri, opreste timere).
const routes = [
  [/^#\/?$/, (root) => projectsView(root)],
  [/^#\/new$/, (root) => projectFormView(root, null)],
  [/^#\/p\/(\d+)\/edit$/, (root, m) => projectFormView(root, Number(m[1]))],
  [/^#\/p\/(\d+)\/agent\/new$/, (root, m) => agentView(root, Number(m[1]), null)],
  [/^#\/p\/(\d+)\/agent\/(\d+)$/, (root, m) => agentView(root, Number(m[1]), Number(m[2]))],
];

let cleanup = null;

async function route() {
  if (cleanup) {
    cleanup();
    cleanup = null;
  }
  const root = document.getElementById("view");
  root.replaceChildren();
  const hash = location.hash || "#/";
  for (const [pattern, view] of routes) {
    const m = hash.match(pattern);
    if (!m) continue;
    try {
      cleanup = (await view(root, m)) || null;
    } catch (err) {
      toast(err.message);
    }
    return;
  }
  location.hash = "#/";
}

window.addEventListener("hashchange", route);
route();

api("/api/info").then(({ name }) => {
  const title = name.charAt(0).toUpperCase() + name.slice(1);
  document.title = title;
  document.getElementById("app-name").textContent = title;
}).catch(() => {});
```

- [ ] **Step 7: Scrie `internal/web/static/js/projects.js`**

```js
import { api } from "./api.js";
import { el, avatar, pill, field, toast } from "./ui.js";

export async function projectsView(root) {
  const projects = await api("/api/projects");
  root.append(
    el("header", { class: "page-head" },
      el("div", {},
        el("h1", {}, "Proiecte"),
        el("p", { class: "muted" }, "Fiecare proiect are echipa lui. Lead-ul planifica, delegheaza si verifica.")),
      el("a", { class: "btn primary", href: "#/new" }, "+ Proiect nou")),
    el("div", { class: "grid" },
      projects.map(projectCard),
      el("a", { class: "card card-new", href: "#/new" },
        el("span", { class: "plus" }, "+"),
        el("strong", {}, "Proiect nou"),
        el("span", { class: "hint" }, "Incepi cu un lead, adaugi echipa dupa"))));
}

function projectCard(p) {
  const pct = p.total ? Math.round((p.done / p.total) * 100) : 0;
  return el("a", { class: "card", href: `#/p/${p.id}` },
    el("div", { class: "card-top" }, el("span", { class: "card-title" }, p.name), pill(p.status)),
    el("p", { class: "muted clamp" }, p.goal || "Fara obiectiv inca."),
    el("div", { class: "avatars" },
      el("span", { class: "stack" }, p.team.map((m) => avatar(m.name, m.position, 28))),
      el("span", { class: "hint" }, p.team.length === 1 ? "1 agent" : `${p.team.length} agenti`)),
    el("div", { class: "progress-row" },
      el("span", { class: "hint" }, p.total ? `${p.done} din ${p.total} task-uri gata` : "Niciun task"),
      el("div", { class: "bar" },
        el("div", { class: "bar-fill" + (p.status === "Running" ? " live" : ""), style: `width:${pct}%` }))));
}

export async function projectFormView(root, id) {
  const p = id
    ? await api(`/api/projects/${id}`)
    : { name: "", goal: "", context: "", workdir: "", maxParallel: 2, budgetUsd: 1 };

  const name = el("input", { type: "text", value: p.name, placeholder: "ex. HoReCa QR", required: true, maxlength: 80 });
  const goal = el("textarea", { value: p.goal, placeholder: "Un MVP si un prototip clickable pentru inscrieri prin QR." });
  const context = el("textarea", { value: p.context, rows: 7, placeholder: "Stack, conventii, ce e deja decis, ce e interzis." });
  const workdir = el("input", { type: "text", value: p.workdir, placeholder: "~/projects/horeca-qr", required: true });
  const parallel = el("input", { type: "number", min: 1, max: 8, value: p.maxParallel });
  const budget = el("input", { type: "number", min: 0.05, max: 100, step: 0.05, value: p.budgetUsd });

  root.append(el("form", { class: "form", onsubmit: save },
    el("header", { class: "page-head" },
      el("div", {},
        el("div", { class: "crumbs" },
          el("a", { href: "#/" }, "Proiecte"), " / ",
          id ? el("a", { href: `#/p/${id}` }, p.name) : "Proiect nou"),
        el("h1", {}, id ? "Instructiuni proiect" : "Proiect nou")),
      el("div", { class: "actions" },
        id && el("button", { type: "button", class: "btn danger", onclick: remove }, "Sterge proiectul"),
        el("a", { class: "btn", href: id ? `#/p/${id}` : "#/" }, "Renunta"),
        el("button", { type: "submit", class: "btn primary" }, id ? "Salveaza" : "Creeaza proiectul"))),
    el("div", { class: "form-cols" },
      el("section", { class: "section" },
        el("h2", {}, "Brief"),
        field("Nume", name),
        field("Obiectiv: cum arata gata", goal),
        field("Context comun: il citeste fiecare agent", context),
        field("Folder de lucru", workdir, "Toata echipa lucreaza in acest folder. Trebuie sa existe.")),
      el("section", { class: "section" },
        el("h2", {}, "Cum lucreaza echipa"),
        el("div", { class: "row" },
          field("Workeri in paralel", parallel),
          field("Buget per run (USD)", budget)),
        el("p", { class: "hint" }, "Fiecare run al unui agent se opreste cand atinge bugetul. Lead-ul nu intra in limita de workeri."),
        !id && el("p", { class: "hint" }, "Primesti un lead implicit. Workerii ii adaugi din pagina proiectului.")))));
  name.focus();

  async function save(e) {
    e.preventDefault();
    const body = {
      name: name.value, goal: goal.value, context: context.value, workdir: workdir.value,
      maxParallel: Number(parallel.value), budgetUsd: Number(budget.value),
    };
    try {
      const saved = id
        ? await api(`/api/projects/${id}`, { method: "PUT", body })
        : await api("/api/projects", { method: "POST", body });
      location.hash = `#/p/${saved.id}`;
    } catch (err) {
      toast(err.message);
    }
  }

  async function remove() {
    if (!confirm(`Stergi proiectul ${p.name}? Task-urile si conversatiile se pierd. Folderul de lucru ramane neatins.`)) return;
    try {
      await api(`/api/projects/${id}`, { method: "DELETE" });
      location.hash = "#/";
    } catch (err) {
      toast(err.message);
    }
  }
}
```

- [ ] **Step 8: Scrie `internal/web/static/js/agent.js`**

```js
import { api } from "./api.js";
import { el, field, toast, MODELS, TOOLS } from "./ui.js";

export async function agentView(root, projectId, agentId) {
  const project = await api(`/api/projects/${projectId}`);
  const existing = agentId ? project.agents.find((a) => a.id === agentId) : null;
  if (agentId && !existing) throw new Error("Agentul nu exista.");
  const a = existing || {
    name: "", role: "", position: "worker", model: "sonnet",
    background: "", instructions: "", doneDef: "", tools: ["write"],
  };
  const isLead = a.position === "lead";

  const name = el("input", { type: "text", value: a.name, placeholder: "ex. Rook", required: true, maxlength: 40 });
  const role = el("input", { type: "text", value: a.role, placeholder: "ex. Backend Developer", maxlength: 80 });
  const model = el("select", { value: a.model }, MODELS.map((m) => el("option", { value: m }, m)));
  const background = el("textarea", { value: a.background, rows: 5, placeholder: "Cine e agentul si ce stie." });
  const instructions = el("textarea", { value: a.instructions, rows: 5, placeholder: "Cum lucreaza si ce nu are voie sa faca." });
  const doneDef = el("textarea", {
    value: a.doneDef, rows: 3,
    placeholder: isLead ? "Ce contine rezumatul final pentru tine." : "Rezumat, fisiere schimbate, cum se verifica.",
  });
  const tools = TOOLS.map((t) => ({ ...t, box: el("input", { type: "checkbox", checked: a.tools.includes(t.id) }) }));
  const preview = el("pre", { class: "preview" }, "...");

  let timer;
  const refresh = () => {
    clearTimeout(timer);
    timer = setTimeout(updatePreview, 300);
  };
  for (const input of [name, role, background, instructions, doneDef]) input.addEventListener("input", refresh);

  root.append(el("form", { class: "form", onsubmit: save },
    el("header", { class: "page-head" },
      el("div", {},
        el("div", { class: "crumbs" },
          el("a", { href: "#/" }, "Proiecte"), " / ",
          el("a", { href: `#/p/${projectId}` }, project.name), " / ",
          existing ? a.name : "Agent nou"),
        el("h1", {}, existing ? a.name : "Agent nou")),
      el("div", { class: "actions" },
        existing && !isLead && el("button", { type: "button", class: "btn danger", onclick: remove }, "Sterge"),
        el("a", { class: "btn", href: `#/p/${projectId}` }, "Renunta"),
        el("button", { type: "submit", class: "btn primary" }, existing ? "Salveaza" : "Adauga in echipa"))),
    el("div", { class: "form-cols" },
      el("div", { class: "form" },
        el("section", { class: "section" },
          el("h2", {}, "Identitate"),
          field("Nume", name, "Lead-ul asigneaza task-uri dupa acest nume."),
          field("Rol", role),
          el("div", { class: "field" },
            el("span", { class: "label" }, "Pozitie in echipa"),
            el("span", {}, isLead
              ? "Lead: planifica, imparte task-uri si verifica rezultatele."
              : "Worker: primeste task-uri de la lead si raporteaza inapoi."))),
        el("section", { class: "section" },
          el("h2", {}, "Model"),
          field("Model Claude", model, "opus: cel mai capabil. sonnet: echilibrat. haiku: rapid si ieftin.")),
        el("section", { class: "section" },
          el("h2", {}, "Tool-uri"),
          el("p", { class: "hint" }, "Citirea fisierelor e mereu permisa. git push, rm -rf, sudo si instalarile de pachete sunt mereu blocate."),
          tools.map((t) => el("label", { class: "check" }, t.box, t.label)))),
      el("div", { class: "form" },
        el("section", { class: "section" },
          el("h2", {}, "Background si instructiuni"),
          field("Background: cine e si ce stie", background),
          field("Instructiuni: cum lucreaza si ce nu face niciodata", instructions),
          field(isLead ? "Rezumatul final: ce iti raporteaza" : "Definition of done: ce ii preda lead-ului", doneDef)),
        el("section", { class: "section" },
          el("div", { class: "section-head" }, el("h2", {}, "Prompt compilat"), el("span", { class: "hint" }, "Preview live")),
          preview)))));
  updatePreview();
  if (!existing) name.focus();

  async function updatePreview() {
    try {
      const res = await api("/api/prompt-preview", {
        method: "POST",
        body: {
          projectId, name: name.value, role: role.value, position: a.position,
          background: background.value, instructions: instructions.value, doneDef: doneDef.value,
        },
      });
      preview.textContent = res.prompt;
    } catch (err) {
      preview.textContent = err.message;
    }
  }

  async function save(e) {
    e.preventDefault();
    const body = {
      name: name.value, role: role.value, model: model.value,
      background: background.value, instructions: instructions.value, doneDef: doneDef.value,
      tools: tools.filter((t) => t.box.checked).map((t) => t.id),
    };
    try {
      if (existing) await api(`/api/agents/${agentId}`, { method: "PUT", body });
      else await api(`/api/projects/${projectId}/agents`, { method: "POST", body });
      location.hash = `#/p/${projectId}`;
    } catch (err) {
      toast(err.message);
    }
  }

  async function remove() {
    if (!confirm(`Stergi agentul ${a.name}?`)) return;
    try {
      await api(`/api/agents/${agentId}`, { method: "DELETE" });
      location.hash = `#/p/${projectId}`;
    } catch (err) {
      toast(err.message);
    }
  }

  return () => clearTimeout(timer);
}
```

- [ ] **Step 9: Verifica sintaxa JS si build-ul**

```bash
cd "/Users/yzy/YzY/AI Dashboard/agentdash"
for f in internal/web/static/js/*.js; do
  cp "$f" "${TMPDIR:-/tmp}/check.mjs" && node --check "${TMPDIR:-/tmp}/check.mjs" || echo "EROARE de sintaxa in $f"
done
go vet ./... && go build -race ./... && go test ./internal/web/
```

Expected: nicio linie `EROARE`, iar testele web trec (`TestInfoAndStatic` gaseste `<title>`).

- [ ] **Step 10: Verificare manuala in browser**

```bash
go build -o /tmp/foreman-test ./cmd/foreman
TESTHOME=$(mktemp -d); mkdir -p "$TESTHOME/proiect"; echo "$TESTHOME/proiect"
HOME=$TESTHOME /tmp/foreman-test start --port 7788 --no-open &
```

Deschide `http://127.0.0.1:7788` si verifica:
1. Sidebar-ul are logo „F” amber si titlul „Foreman”, iar tab-ul browserului se numeste „Foreman”.
2. Pe pagina „Proiecte” apare doar cardul „Proiect nou”.
3. Deschide „Proiect nou” si creeaza un proiect cu folderul afisat mai sus. Dupa salvare esti redirectionat la lista (workspace-ul vine in Task 12). Cardul are pill „Inactiv”, un avatar amber si „Niciun task”.
4. Un folder inexistent arata toast-ul rosu „folderul de lucru nu exista: ...”.
5. `#/p/1/agent/new`: preview-ul se actualizeaza cand scrii in Nume sau Background. „Adauga in echipa” salveaza, iar un nume duplicat arata toast-ul de conflict.
6. `#/p/1/edit`: campurile sunt precompletate, iar „Salveaza” pastreaza modificarile.

Opreste serverul: `kill %1`.

- [ ] **Step 11: Commit**

```bash
git add -A
git commit -m "UI: schelet Foreman, lista si formular proiecte, editor agent"
```

---

### Task 12: UI: workspace-ul proiectului (echipa, board, consola live)

**Files:**
- Create: `internal/web/static/js/workspace.js`
- Create: `internal/web/static/js/live.js`
- Modify: `internal/web/static/js/app.js` (ruta `#/p/{id}`)

**Interfaces:**
- Consumes:
  - din Task 11: `el`, `avatar`, `pill`, `toast`, `clock`, `api`;
  - din Task 10: SSE `/api/projects/{id}/events` (`{type, data}`), SSE `/api/runs/{id}/events` (linii stream-json plus `app_start`, `app_stderr`, `app_done`), `GET/POST /api/agents/{id}/messages`, `POST /api/projects/{id}/stop|resume`, `POST /api/tasks/{id}/accept|unblock`.
- Produces: `workspaceView(root, projectId)`, care intoarce functia de curatare, si `liveRenderer(box)`, care intoarce `render(event)`.

- [ ] **Step 1: Scrie `internal/web/static/js/live.js`**

```js
import { el } from "./ui.js";

// liveRenderer intoarce o functie care adauga in box evenimentele stream-json
// ale unui run: textul agentului si tool call-urile, restranse pe cate un rand.
export function liveRenderer(box) {
  const tools = new Map(); // tool_use_id -> <details>
  return function render(ev) {
    switch (ev.type) {
      case "app_start":
        box.append(el("details", {},
          el("summary", {}, "Mesajul primit de agent"),
          el("pre", {}, ev.prompt || "")));
        break;
      case "assistant":
        for (const block of ev.message?.content || []) {
          if (block.type === "text" && block.text.trim()) {
            box.append(el("div", { class: "live-text" }, block.text));
          } else if (block.type === "tool_use") {
            const d = el("details", {},
              el("summary", {}, `${toolName(block.name)} ${hint(block.input)}`),
              el("pre", {}, JSON.stringify(block.input, null, 2)));
            tools.set(block.id, d);
            box.append(d);
          }
        }
        break;
      case "user":
        for (const block of ev.message?.content || []) {
          if (block.type !== "tool_result") continue;
          const d = tools.get(block.tool_use_id);
          if (!d) continue;
          if (block.is_error) d.querySelector("summary").classList.add("err");
          d.append(el("pre", {}, truncate(resultText(block.content), 3000)));
        }
        break;
      case "result":
        if (ev.is_error) {
          box.append(el("div", { class: "err" }, "Eroare: " + ((ev.errors || []).join("; ") || ev.subtype)));
        } else if (ev.total_cost_usd != null) {
          box.append(el("div", { class: "hint" }, `Gata, $${ev.total_cost_usd.toFixed(4)}`));
        }
        break;
      case "app_stderr":
        box.append(el("div", { class: "err" }, ev.text));
        break;
    }
  };
}

function toolName(name) {
  return name.replace(/^mcp__team__/, "echipa: ");
}

// un indiciu scurt langa numele tool-ului: fisierul, comanda, titlul task-ului
function hint(input = {}) {
  const h = input.file_path || input.command || input.pattern || input.title || input.task || input.url || "";
  return truncate(String(h).split("\n")[0], 80);
}

function resultText(content) {
  if (typeof content === "string") return content;
  if (Array.isArray(content)) return content.map((c) => c.text ?? JSON.stringify(c)).join("\n");
  return JSON.stringify(content, null, 2);
}

function truncate(s, n) {
  return s.length > n ? s.slice(0, n) + " [...]" : s;
}
```

- [ ] **Step 2: Scrie `internal/web/static/js/workspace.js`**

```js
import { api } from "./api.js";
import { el, avatar, pill, toast, clock } from "./ui.js";
import { liveRenderer } from "./live.js";

const COLUMNS = [
  { title: "In asteptare", statuses: ["queued"] },
  { title: "In lucru", statuses: ["in_progress"] },
  { title: "Review", statuses: ["review", "blocked"] },
  { title: "Gata", statuses: ["done"] },
];

const ref = (t) => `T-${t.number}`;

export async function workspaceView(root, projectId) {
  const state = {
    project: await api(`/api/projects/${projectId}`),
    selected: null,   // agentul din consola
    seen: new Set(),  // id-urile mesajelor afisate in thread
    open: new Set(),  // task-urile deschise pe board (raman deschise la redesenare)
    liveRunId: null,
    liveStream: null,
  };
  state.selected = state.project.agents[0]?.id ?? null;

  const header = el("div");
  const team = el("div", { class: "team" });
  const board = el("div", { class: "board" });
  const count = el("span", { class: "hint" });
  const consoleHead = el("div", { class: "console-head" });
  const thread = el("div", { class: "thread", "aria-live": "polite" });
  const input = el("textarea", { rows: 1, "aria-label": "Mesaj" });
  const note = el("span", { class: "hint" });
  const composer = el("form", { onsubmit: send },
    input, el("button", { type: "submit", class: "btn primary" }, "Trimite"));
  input.addEventListener("keydown", (e) => {
    if (e.key === "Enter" && !e.shiftKey) {
      e.preventDefault();
      composer.requestSubmit();
    }
  });

  root.append(
    header,
    el("div", { class: "ws" },
      el("div", { class: "ws-main" },
        el("section", { class: "section" },
          el("div", { class: "section-head" },
            el("div", {}, el("h2", {}, "Echipa"), el("span", { class: "hint" }, "Selecteaza un agent ca sa vorbesti direct cu el")),
            el("a", { class: "btn small", href: `#/p/${projectId}/agent/new` }, "+ Adauga agent")),
          team),
        el("section", { class: "section" },
          el("div", { class: "section-head" }, el("h2", {}, "Task-uri"), count),
          board)),
      el("aside", { class: "console" },
        consoleHead, thread, el("div", { class: "composer" }, composer, note))));

  // ---------- desenare ----------
  function renderAll() {
    renderHeader();
    renderTeam();
    renderBoard();
    renderConsoleHead();
  }

  function renderHeader() {
    const p = state.project;
    header.replaceChildren(el("header", { class: "page-head" },
      el("div", {},
        el("div", { class: "crumbs" }, el("a", { href: "#/" }, "Proiecte"), " / ", p.name),
        el("div", { class: "actions" }, el("h1", {}, p.name), pill(p.status)),
        p.goal && el("p", { class: "muted" }, "Obiectiv: " + p.goal),
        p.paused && el("p", { class: "hint" }, "Echipa e pe pauza. Mesajele raman in asteptare pana o pornesti.")),
      el("div", { class: "actions" },
        el("a", { class: "btn", href: `#/p/${projectId}/edit` }, "Instructiuni proiect"),
        p.paused
          ? el("button", { class: "btn primary", onclick: () => projectAction("resume") }, "Porneste echipa")
          : el("button", { class: "btn", onclick: () => projectAction("stop") }, "Opreste echipa"))));
  }

  function renderTeam() {
    const [lead, ...workers] = state.project.agents;
    team.replaceChildren(
      memberCard(lead, true),
      workers.length
        ? el("div", { class: "team-workers" }, workers.map((a) => memberCard(a, false)))
        : el("p", { class: "hint" }, "Niciun worker inca. Adauga agenti ca lead-ul sa aiba cui delega."));
  }

  function memberCard(a, isLead) {
    const task = a.currentTask ? `${ref(a.currentTask)} ${a.currentTask.title}` : "";
    const statusText = a.status === "Working" ? (task || "Lucreaza")
      : a.status === "Queued" ? (task ? "Urmeaza: " + task : "Are mesaje in asteptare")
      : "Liber";
    return el("div", { class: "member-cell" + (isLead ? " lead" : "") },
      el("button", {
        type: "button", class: "member",
        "aria-pressed": String(a.id === state.selected),
        onclick: () => select(a.id),
      },
        avatar(a.name, a.position, isLead ? 40 : 32),
        el("span", { class: "member-body" },
          el("span", { class: "member-name" }, a.name, isLead && el("span", { class: "tag" }, "Lead")),
          el("span", { class: "hint" }, [a.role, a.model].filter(Boolean).join(" · ")),
          el("span", { class: "status" }, el("span", { class: "dot " + a.status.toLowerCase() }), el("span", {}, statusText)))),
      el("a", { class: "edit-link", href: `#/p/${projectId}/agent/${a.id}` }, "Editeaza"));
  }

  function renderBoard() {
    const byId = new Map(state.project.agents.map((a) => [a.id, a]));
    count.textContent = `${state.project.done} din ${state.project.total} gata`;
    board.replaceChildren(...COLUMNS.map((c) => {
      const items = state.project.tasks.filter((t) => c.statuses.includes(t.status));
      return el("div", { class: "col" },
        el("div", { class: "col-head" }, el("span", {}, c.title), el("span", {}, items.length)),
        items.map((t) => taskCard(t, byId.get(t.assigneeId))));
    }));
  }

  function taskCard(t, who) {
    const blocked = t.status === "blocked";
    return el("details", {
      class: "task",
      open: state.open.has(t.id),
      ontoggle: (e) => (e.target.open ? state.open.add(t.id) : state.open.delete(t.id)),
    },
      el("summary", {},
        el("span", { class: "task-title" }, el("span", { class: "task-id" }, ref(t)), t.title),
        el("span", { class: "task-who" },
          who && avatar(who.name, who.position, 20), who ? who.name : "?",
          blocked && el("span", { class: "badge-blocked" }, "Blocat"))),
      el("div", { class: "task-detail" },
        t.description && el("div", {}, t.description),
        t.feedback && el("div", {}, el("strong", {}, "Feedback: "), t.feedback),
        t.result && el("div", {}, el("strong", {}, "Rezultat: "), t.result),
        !t.description && !t.feedback && !t.result && el("div", { class: "hint" }, "Fara detalii inca."),
        blocked && el("div", { class: "actions" },
          el("button", { type: "button", class: "btn small primary", onclick: () => taskAction(t, "accept") }, "Accepta"),
          el("button", { type: "button", class: "btn small", onclick: () => taskAction(t, "unblock") }, "Deblocheaza"))));
  }

  function selectedAgent() {
    return state.project.agents.find((a) => a.id === state.selected);
  }

  function renderConsoleHead() {
    const a = selectedAgent();
    if (!a) return;
    const isLead = a.position === "lead";
    consoleHead.replaceChildren(
      avatar(a.name, a.position, 36),
      el("div", {},
        el("div", { class: "member-name" }, a.name, a.role && el("span", { class: "hint" }, "· " + a.role)),
        el("div", { class: "hint" }, isLead
          ? "Thread-ul lead-ului. Ii dai obiective aici, el delegheaza echipei."
          : "Thread direct. Lead-ul primeste in copie ce ii scrii.")));
    input.placeholder = isLead ? `Da-i lui ${a.name} un obiectiv sau cere un status` : `Mesaj pentru ${a.name}`;
    note.textContent = state.project.paused ? "Echipa e pe pauza: mesajul asteapta pana o pornesti." : "";
  }

  // ---------- thread ----------
  async function select(id) {
    state.selected = id;
    renderTeam();
    renderConsoleHead();
    await loadThread();
    syncLive();
  }

  async function loadThread() {
    const id = state.selected;
    if (!id) return;
    const messages = await api(`/api/agents/${id}/messages`);
    if (id !== state.selected) return; // s-a schimbat selectia intre timp
    stopLive();
    state.seen = new Set();
    thread.replaceChildren();
    if (!messages.length) thread.append(el("div", { class: "empty" }, "Niciun mesaj inca."));
    messages.forEach(addMessage);
    scrollDown(true);
  }

  function addMessage(m) {
    if (state.seen.has(m.id)) {
      // acelasi mesaj, stare noua (ex. livrat): il redesenam pe loc
      thread.querySelector(`[data-id="${m.id}"]`)?.replaceWith(messageNode(m));
      return;
    }
    state.seen.add(m.id);
    thread.querySelector(".empty")?.remove();
    const node = messageNode(m);
    const live = thread.querySelector(".live");
    if (live) thread.insertBefore(node, live); // blocul live ramane ultimul
    else thread.append(node);
    scrollDown();
  }

  function messageNode(m) {
    if (m.kind === "event") return el("div", { class: "event", "data-id": m.id }, m.text);
    const mine = m.author === "user";
    return el("div", { class: "msg" + (mine ? " me" : ""), "data-id": m.id },
      el("span", { class: "msg-meta" }, `${mine ? "Tu" : m.author} · ${clock(m.createdAt)}`),
      el("div", { class: "bubble" }, m.text),
      mine && !m.delivered && el("span", { class: "pending" }, "in asteptare"));
  }

  function scrollDown(force) {
    const nearBottom = thread.scrollHeight - thread.scrollTop - thread.clientHeight < 120;
    if (force || nearBottom) thread.scrollTop = thread.scrollHeight;
  }

  async function send(e) {
    e.preventDefault();
    const text = input.value.trim();
    if (!text || !state.selected) return;
    try {
      const m = await api(`/api/agents/${state.selected}/messages`, { method: "POST", body: { text } });
      input.value = "";
      addMessage(m);
      scrollDown(true);
    } catch (err) {
      toast(err.message);
    }
  }

  // ---------- run-ul live al agentului selectat ----------
  function syncLive() {
    const a = selectedAgent();
    const runId = a?.activeRunId || null;
    if (runId === state.liveRunId) return;
    stopLive();
    if (!runId) return;
    state.liveRunId = runId;
    const box = el("div", { class: "live" }, el("div", { class: "live-head" }, `${a.name} lucreaza acum`));
    thread.append(box);
    scrollDown();
    const render = liveRenderer(box);
    const es = new EventSource(`/api/runs/${runId}/events`);
    state.liveStream = es;
    es.onmessage = (msg) => {
      let ev;
      try { ev = JSON.parse(msg.data); } catch { return; }
      if (ev.type === "app_done") {
        stopLive(); // raspunsul final vine ca mesaj normal in thread
        return;
      }
      render(ev);
      scrollDown();
    };
    es.onerror = () => {
      if (es.readyState === EventSource.CLOSED) stopLive();
    };
  }

  function stopLive() {
    state.liveStream?.close();
    state.liveStream = null;
    state.liveRunId = null;
    thread.querySelector(".live")?.remove();
  }

  // ---------- actiuni ----------
  async function projectAction(what) {
    try {
      await api(`/api/projects/${projectId}/${what}`, { method: "POST" });
      await reload();
    } catch (err) {
      toast(err.message);
    }
  }

  async function taskAction(t, what) {
    try {
      await api(`/api/tasks/${t.id}/${what}`, { method: "POST" });
      await reload();
    } catch (err) {
      toast(err.message);
    }
  }

  // ---------- sincronizare live cu serverul ----------
  let reloadTimer;
  async function reload() {
    try {
      state.project = await api(`/api/projects/${projectId}`);
    } catch (err) {
      toast(err.message);
      return;
    }
    if (!selectedAgent()) state.selected = state.project.agents[0]?.id ?? null; // agent sters
    renderAll();
    syncLive();
  }
  const scheduleReload = () => {
    clearTimeout(reloadTimer);
    reloadTimer = setTimeout(reload, 150);
  };

  let hadError = false;
  const events = new EventSource(`/api/projects/${projectId}/events`);
  events.onmessage = (msg) => {
    let ev;
    try { ev = JSON.parse(msg.data); } catch { return; }
    if (ev.type === "message" && ev.data.agentId === state.selected) addMessage(ev.data);
    scheduleReload();
  };
  events.onerror = () => { hadError = true; };
  events.onopen = async () => {
    // dupa o reconectare (ex. restart server) reincarcam tot, am putut pierde evenimente
    if (!hadError) return;
    hadError = false;
    await reload();
    await loadThread();
    syncLive();
  };

  renderAll();
  await loadThread();
  syncLive();

  return () => {
    events.close();
    stopLive();
    clearTimeout(reloadTimer);
  };
}
```

- [ ] **Step 3: Inregistreaza ruta in `internal/web/static/js/app.js`**

Adauga importul langa celelalte:

```js
import { workspaceView } from "./workspace.js";
```

si, in lista `routes`, dupa ruta `#\/p\/(\d+)\/edit`, adauga:

```js
  [/^#\/p\/(\d+)$/, (root, m) => workspaceView(root, Number(m[1]))],
```

- [ ] **Step 4: Verifica sintaxa si build-ul**

```bash
for f in internal/web/static/js/*.js; do
  cp "$f" "${TMPDIR:-/tmp}/check.mjs" && node --check "${TMPDIR:-/tmp}/check.mjs" || echo "EROARE de sintaxa in $f"
done
go vet ./... && go build -race ./... && go test -race ./...
```

Expected: nicio linie `EROARE`, toate testele trec.

- [ ] **Step 5: Verificare manuala in browser (fara Claude real)**

Serverul ruleaza cu `HOME` temporar, deci `claude` nu e autentificat. Run-urile vor esua repede, si e bine asa: testam UI-ul, nu modelul.

```bash
go build -o /tmp/foreman-test ./cmd/foreman
TESTHOME=$(mktemp -d); mkdir -p "$TESTHOME/proiect"; echo "$TESTHOME/proiect"
HOME=$TESTHOME /tmp/foreman-test start --port 7788 --no-open &
```

In browser, pe `http://127.0.0.1:7788`:
1. Creeaza un proiect si ajungi in workspace, cu cardul Lead selectat (margine amber) si butonul „Opreste echipa”.
2. „+ Adauga agent”: adauga Rook, apoi revii in workspace si Rook apare in grila de workeri.
3. Scrie un mesaj lui Rook. Apare in dreapta cu „in asteptare”, iar in thread-ul Lead apare evenimentul „Utilizatorul -> Rook: ...”.
4. Un run porneste: apare blocul „Rook lucreaza acum”, cu „Mesajul primit de agent”. Esecul vine din lipsa autentificarii. Blocul live dispare, apare „Run esuat: ...” si „in asteptare” dispare.
5. „Opreste echipa” schimba pill-ul in „Pe pauza” si butonul in „Porneste echipa”. Un mesaj trimis acum ramane „in asteptare” si nu porneste niciun run.
6. Reincarca pagina (F5): starea se pastreaza.
7. Opreste serverul (`kill %1`), porneste-l din nou cu aceeasi comanda si reincarca pagina: proiectul e „Pe pauza”.

Opreste serverul: `kill %1`.

- [ ] **Step 6: Commit**

```bash
git add -A
git commit -m "UI: workspace cu echipa, board de task-uri si consola live"
```

---

### Task 13: Documentatie si test cap-coada cu Claude real

**Files:**
- Modify (rescriere): `README.md`
- Modify: `CLAUDE.md` (cel din `agentdash/`)
- Modify: `../CLAUDE.md` (cel din `AI Dashboard/`, in afara repo-ului, cu aceleasi sectiuni)

**Interfaces:**
- Consumes: tot ce e mai sus.
- Produces: documentatia la zi si o verificare reala cap-coada.

- [ ] **Step 1: Rescrie `README.md`**

````markdown
# Foreman

Echipe de agenti Claude Code, local. Un singur binar Go, UI in browser pe 127.0.0.1.

Fiecare proiect are o echipa: un **lead** care primeste obiectivul, il sparge in task-uri
si le da **workerilor**, apoi verifica ce vine inapoi. Tu vorbesti cu lead-ul sau direct
cu orice agent. Totul se vede live pe un board si ramane salvat in SQLite.

## Rulare

    go run ./cmd/foreman start

Deschide http://127.0.0.1:7777. Optiuni: `--port 8080`, `--no-open`.
Ai nevoie de `claude` (Claude Code) in PATH, autentificat.

## Build

    go build -o foreman ./cmd/foreman
    GOOS=darwin GOARCH=arm64 go build -o foreman-mac ./cmd/foreman

## Cum functioneaza

- Agentii ruleaza ca `claude -p` headless, cu folderul proiectului ca working directory.
- Aplicatia expune un server MCP pe `/mcp/<token>`. Lead-ul are tool-urile `create_task`,
  `list_tasks`, `accept_task` si `send_back`, iar workerii au `report_done`.
- Cand un worker termina, lead-ul e trezit (`--resume`) cu rezultatul.
- Permisiunile vin doar din aplicatie (`--settings` inline, `--setting-sources user`). `CLAUDE.md`
  din repo-ul proiectului se citeste in continuare. `git push`, `rm -rf`, `sudo` si instalarile
  de pachete sunt mereu blocate.
- La pornire toate proiectele sunt pe pauza: nimic nu ruleaza (si nu costa) fara sa apesi „Porneste echipa”.

## Date

    ~/.foreman/foreman.db           proiecte, agenti, task-uri, run-uri, mesaje
    ~/.foreman/logs/runs/<id>.jsonl logul brut al fiecarui run

## Structura

    cmd/foreman/        CLI: start, oprire curata la Ctrl+C
    internal/config/    AppName si caile de pe disc
    internal/store/     SQLite (modernc.org/sqlite, fara CGO)
    internal/prompt/    system prompt, mesajul turei, permisiuni, config MCP
    internal/runner/    porneste claude, parseaza stream-json, log + broadcast
    internal/mcp/       server MCP minimal (JSON-RPC peste HTTP)
    internal/team/      scheduler-ul: tool-urile echipei, pornire/final de run-uri
    internal/events/    evenimente live per proiect (SSE)
    internal/web/       API, SSE, UI embedded

## Redenumire

Numele e in `internal/config/config.go` (`AppName`). Il schimbi acolo, redenumesti
`cmd/<nume>`, rulezi `go mod edit -module <nume>` si faci replace pe importuri.
````

- [ ] **Step 2: Actualizeaza `CLAUDE.md` (ambele copii)**

Titlul devine `# foreman (fost agentdash)`. In sectiunea `## Decizii luate`, inlocuieste bullet-ul despre folderele agentilor cu:

```markdown
- Agentii traiesc in SQLite (~/.foreman/foreman.db), per proiect: un lead + workeri. Toti ruleaza cu folderul proiectului ca cwd.
- Orchestrare prin MCP: aplicatia e server MCP (/mcp/<token>), lead-ul creeaza task-uri, workerii raporteaza. Scheduler in internal/team.
- Permisiuni doar din aplicatie: --settings inline + --setting-sources user; deny fix pe actiuni riscante si pe TaskCreate/TodoWrite/Agent.
- Numele aplicatiei e config.AppName ("foreman"), usor de schimbat.
```

Inlocuieste sectiunea `## MVP` cu:

```markdown
## Stare
Gata (felia 1, spec in docs/superpowers/specs/2026-09-29-foreman-slice1-design.md): proiecte, echipe lead + workeri,
orchestrare prin MCP, board live, chat per agent, SQLite, pauza la pornire.
Urmeaza (sub-proiecte separate, fiecare cu spec + plan):
2. Librarie de agenti refolositi intre proiecte, template-uri de echipa, reviewer separat
3. Aprobari pentru actiuni riscante (--permission-prompt-tool), feed global "Live activity"
4. Modurile collab/pipeline, Runs & logs, Settings
Dupa: cron-uri, service install, goreleaser, notificari, git worktrees.
```

In `## Conventii`, comenzile de cross-compile devin `./cmd/foreman`.

- [ ] **Step 3: Test cap-coada cu Claude real (cu `HOME`-ul tau real, costa cativa centi)**

```bash
go build -o /tmp/foreman ./cmd/foreman
mkdir -p /tmp/foreman-e2e && cd /tmp/foreman-e2e && git init -q 2>/dev/null; cd -
/tmp/foreman start --port 7788 --no-open &
```

In browser, pe `http://127.0.0.1:7788`:
1. Proiect nou:
   - Nume „E2E”;
   - Obiectiv „Creeaza doua fisiere: poezie.txt cu o strofa despre toamna si haiku.txt cu un haiku despre mare.”;
   - Folder `/tmp/foreman-e2e`;
   - Buget 0.5.
2. Lead-ul: editeaza-l si pune model `haiku` (ieftin).
3. Adauga doi workeri, `Poet` si `Haijin`, ambii cu `haiku` si tool-ul „Scrie fisiere”.
4. „Porneste echipa” (daca proiectul e pe pauza), apoi scrie-i lead-ului „Incepe.”
5. Verifica, in ordine:
   - lead-ul lucreaza (bloc live) si creeaza T-1 si T-2 prin `echipa: create_task`;
   - pe board apar doua task-uri, ambele trec in „In lucru”, iar cardurile workerilor arata „Lucreaza”;
   - fiecare worker scrie fisierul si apeleaza `echipa: report_done`, iar task-ul trece in „Review”;
   - lead-ul e trezit cu notificarile si accepta task-urile (sau trimite inapoi cu feedback);
   - cele doua task-uri ajung in „Gata”, iar lead-ul scrie un rezumat in thread.
6. `ls /tmp/foreman-e2e` arata `poezie.txt` si `haiku.txt`.
7. Costul: `sqlite3 ~/.foreman/foreman.db "select agent_id, status, cost_usd from runs"`, sau citeste logurile din `~/.foreman/logs/runs/`.

Daca lead-ul nu apeleaza tool-urile echipei (de exemplu, face el munca), noteaza exact ce a facut, din log. Ajustarea textului „Cum lucrezi” din `internal/prompt/prompt.go` intra intr-un commit separat.

Opreste serverul: `kill %1`.

- [ ] **Step 4: Verificarile finale din conventii**

```bash
go vet ./... && go build -race ./... && go test -race ./...
GOOS=darwin GOARCH=arm64 go build -o /dev/null ./cmd/foreman
GOOS=windows go build -o /dev/null ./cmd/foreman
```

Expected: toate trec.

- [ ] **Step 5: Commit**

```bash
git add -A
git commit -m "Documentatie Foreman felia 1"
```
