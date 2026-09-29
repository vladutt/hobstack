# Foreman: echipe ca graf (ierarhie + review) si editor canvas — plan de implementare

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:**
- Echipa unui proiect devine o padure de arbori (`parent_id`) plus legaturi de review (`reviewer_id`), in locul modelului „un lead + workeri”.
- Mai multi lideri (coordonator, sub-lideri, lideri independenti), sub-task-uri, o etapa de review inainte de decizia liderului.
- Un editor canvas in SVG, fara biblioteci, in care utilizatorul muta nodurile si trage legaturile.

**Architecture:**
- `store` tine legaturile si pozitiile in coloane noi (migrarea 003), verifica invariantii (acelasi proiect, fara auto-legaturi, fara cicluri) si ofera functii pure pentru graf (`RolesOf`, `Children`, `IsAncestor`).
- Rolurile (lider, radacina, reviewer) nu se stocheaza: se calculeaza din legaturi la fiecare citire.
- `prompt` compune „Cum lucrezi” din roluri si pune in tura echipa directa, pe cine revizuiesti si task-urile deschise.
- `mcp` da fiecarui run reuniunea tool-urilor pentru rolurile lui; `team` revalideaza din DB la fiecare apel.
- `team` generalizeaza scheduler-ul: orice agent cu task in Queued primeste run pe task, altfel run de notificari; `max_parallel` se aplica doar agentilor cu sef.
- `web` expune campurile noi, rolurile derivate si un endpoint ieftin pentru pozitie. UI-ul are un modul nou `canvas.js`.

**Tech Stack:** Go (net/http, database/sql), modernc.org/sqlite, JS vanilla + SVG. Verificarea UI: Chrome headless prin CDP, cu un script Node 22 (fara pachete npm).

**Spec:** `docs/superpowers/specs/2026-09-29-foreman-team-graph-design.md`

## Global Constraints

- Texte pentru utilizator si agenti in romana, **fara diacritice**. Comentariile in cod, la fel.
- Nicio dependenta noua.
- Dupa fiecare task trec `go vet ./...`, `go build -race ./...`, `go test -race ./...` si `gofmt -l .` (nu trebuie sa afiseze nimic). Cross-compile-ul (`GOOS=windows` si `GOOS=darwin GOARCH=arm64`, `-o /dev/null`) trebuie sa treaca.
- Cererile non-GET sub `/api/` accepta `Content-Type: application/json`, sau `multipart/form-data` insotit de header-ul `X-Foreman: 1`. Orice altceva primeste 415.
- Codul trebuie sa fie simplu si idiomatic, pentru ca autorul e incepator in Go.
- In stage intra doar fisierele atinse, niciodata binare.

**Verificare standard** (ruleaza-o la finalul fiecarui task, din radacina repo-ului):

```bash
go vet ./...
go build -race ./...
go test -race ./...
test -z "$(gofmt -l .)" || gofmt -l .
GOOS=windows go build -o /dev/null ./cmd/foreman
GOOS=darwin GOARCH=arm64 go build -o /dev/null ./cmd/foreman
```

Scripturile de verificare (CDP, e2e) si binarele de test stau intr-un folder temporar din afara repo-ului (`UI=$(mktemp -d)`), niciodata in repo.

## Review Focus

- **Baza de date existenta la upgrade.** Un task care era in `review` inainte de migrare trebuie sa ramana decidabil de fostul lead (`creator_id` = lead, `review_stage = 'lead'`), altfel `accept_task` il refuza si task-ul ramane agatat. Test: Task 1, `TestMigration003ConvertsOldProject`.
- **O legatura trasa pe canvas care inchide un cerc** (de exemplu delegare de la un worker catre coordonatorul lui). Serverul trebuie sa intoarca 400 `legatura ar crea un ciclu`, fara sa salveze nimic, iar UI-ul arata mesajul si redeseneaza starea veche. Teste: Task 1, `TestLinksInvariants`; Task 5, `TestAgentLinksValidation`; Task 6, pasul CDP „ciclu”.
- **Stergerea unui lider din mijloc.** Copiii trec la seful lui, legaturile de review catre el dispar, iar task-urile create de el raman decidabile de stramosii asignatului (`creator_id` devine NULL). Teste: Task 1, `TestDeleteAgentReparents`; Task 3, `TestAcceptWithoutCreatorByAncestor`.
- **Stop sau restart cat timp un sub-lider asteapta sub-task-urile.** Task-ul parinte trebuie sa ramana `in_progress` (nu `queued`), altfel sub-liderul e pornit din nou pe el si creeaza sub-task-uri duplicate. Teste: Task 1, `TestRequeueKeepsWaitingParent`; Task 4, `TestStopKeepsWaitingParentTask`.
- **Legaturi schimbate in timpul unui run.** `Caller` e calculat la pornire; daca utilizatorul muta agentul intre timp, tool-ul trebuie refuzat pe baza starii din DB, nu a flag-urilor vechi. Test: Task 3, `TestCreateTaskDirectReportsOnly` (cazul „lider vechi fara copii”).

## Fisiere

```
internal/store/migrations/003_team_graph.sql   coloane noi, drop agents_one_lead, conversie
internal/store/agents.go        Agent: ParentID, ReviewerID, Responsibilities, PosX, PosY; checkLinks;
                                DeleteAgent cu re-parentare; SetAgentPosition; ListAgents radacini primele
internal/store/tasks.go         Task: CreatorID, ParentTaskID, ReviewStage; Subtasks; Requeue* sar parintii care asteapta
internal/store/graph.go         Roles, RolesOf, Children, IsAncestor, FindAgent, LinkError (nou)
internal/store/projects.go      CreateProject: radacina "Lead" fara Position
internal/store/store_test.go, tasks_test.go, graph_test.go (nou)
internal/prompt/prompt.go       Links, LinksFor, Member, OpenTask, TurnContext, Context, TaskTurn, NotifyTurn, System nou
internal/prompt/prompt_test.go
internal/mcp/server.go          Caller{IsLead, HasParent, IsReviewer}; Backend cu ReportDone(task), ReviewApprove, ReviewReject
internal/mcp/tools.go           leadTools, reportTool, reviewTools, toolsFor = reuniunea
internal/mcp/server_test.go
internal/team/tools.go          create_task doar pentru echipa directa, report_done -> reviewer/decident,
                                review_*, accept/send_back de creator sau stramos, afterDone
internal/team/run.go            startRun(p, a, agents, ...), Caller din roluri, recordOutcome cu sub-task-uri
internal/team/reconcile.go      Reconcile generalizat, turnContext, subtaskLines, texts, UserMessage catre sef,
                                resolveBlocked, wakeForDecisions
internal/team/fake_test.go, tools_test.go, reconcile_test.go, graph_test.go (nou)
internal/web/api_agents.go      agentInput cu legaturi, setAgentPosition, promptPreview cu legaturi
internal/web/api_projects.go    agentView cu isLead/isRoot/isReviewer, memberBrief fara position
internal/web/server.go          ruta PUT /api/agents/{id}/position, LinkError -> 400
internal/web/server_test.go, graph_test.go (nou)
internal/web/static/js/ui.js        svg(), kindOf(), agentBody()
internal/web/static/js/canvas.js    teamCanvas (nou)
internal/web/static/js/workspace.js canvas in sectiunea Echipa, eticheta de review pe board, consola
internal/web/static/js/agent.js     Ce ia, Raporteaza lui, Verificat de; fara "Pozitie in echipa"
internal/web/static/js/projects.js  avatar dupa kindOf, hint max_parallel
internal/web/static/app.css         stiluri canvas, meniu legaturi, overlay, eticheta review
README.md
```

## Nume folosite in tot planul

Ca sa ramana consistente intre task-uri:

| Nume | Unde | Ce e |
|---|---|---|
| `store.Roles{IsLead, IsRoot, IsReviewer bool}` | store/graph.go | rolurile derivate |
| `store.RolesOf(a Agent, team []Agent) Roles` | store/graph.go | calculul rolurilor |
| `store.Children(id int64, team []Agent) []Agent` | store/graph.go | echipa directa, in ordinea din `team` |
| `store.IsAncestor(anc, id int64, team []Agent) bool` | store/graph.go | `anc` e sef (direct sau indirect) al lui `id` |
| `store.FindAgent(team []Agent, id int64) (Agent, bool)` | store/graph.go | cautare in lista |
| `store.LinkError` | store/graph.go | eroare de invariant, devine 400 in web |
| `store.StageNone/StageReviewer/StageLead` | store/tasks.go | `""`, `"reviewer"`, `"lead"` |
| `prompt.Links{BossName string; IsLead, IsReviewer bool}` | prompt | ce intra in system prompt |
| `prompt.LinksFor(a store.Agent, team []store.Agent) Links` | prompt | folosit de team si web |
| `prompt.TurnContext{Direct []Member; Reviewees []string; OpenTasks []OpenTask}` | prompt | partea dinamica comuna |
| `mcp.Caller{..., IsLead, HasParent, IsReviewer bool}` | mcp | identitatea run-ului |
| `team.decider(task, agents) (store.Agent, bool)` | team/tools.go | cine decide: creatorul, altfel seful asignatului |
| `team.canDecide(callerID, task, agents) bool` | team/tools.go | creator sau stramos al creatorului |
| `(*Team).toReview`, `(*Team).afterDone` | team/tools.go | trecerea in Review, notificarea „toate gata” |
| `kindOf(a)`, `agentBody(a)`, `svg(...)` | static/js/ui.js | utilitare UI |

---

### Task 1: store (migrarea 003, campuri noi, invarianti, re-parentare)

**Files:** `internal/store/migrations/003_team_graph.sql`, `internal/store/agents.go`, `internal/store/tasks.go`, `internal/store/runs.go` (`nullPtr`), `internal/store/graph.go` (nou), `internal/store/projects.go`, `internal/store/store_test.go`, `internal/store/tasks_test.go`, `internal/store/graph_test.go` (nou), `internal/team/fake_test.go` (doar `setup` si helper nou), `internal/team/reconcile_test.go` (doar `quill` din `TestUnlimitedParallel`), `internal/web/server_test.go` (doar `addWorker` si `TestDeleteAgentConflicts`)

**Interfaces:**

Consumes: schema din 001/002, `migrate()` (aplica fisierele in ordinea numelui).

Produces:
- `store.Agent` primeste:
  ```go
  ParentID         *int64   `json:"parentId"`         // nil = raporteaza utilizatorului (radacina)
  ReviewerID       *int64   `json:"reviewerId"`       // nil = nu il verifica nimeni
  Responsibilities string   `json:"responsibilities"` // "Ce ia"
  PosX             *float64 `json:"posX"`             // nil = asezare automata pe canvas
  PosY             *float64 `json:"posY"`
  ```
  `Position` si constantele `PositionLead`/`PositionWorker` **raman temporar** (comentariu `// Deprecated: rolul reiese din legaturi. Se sterge in Task 5.`), ca pachetele team/prompt/web sa compileze pana le muta pe legaturi.
- `store.Task` primeste:
  ```go
  CreatorID    *int64 `json:"creatorId"`    // liderul care decide; nil la task-uri al caror creator a fost sters
  ParentTaskID *int64 `json:"parentTaskId"` // task-ul din care a fost spart
  ReviewStage  string `json:"reviewStage"`  // "", "reviewer", "lead"
  ```
  si constantele `StageNone = ""`, `StageReviewer = "reviewer"`, `StageLead = "lead"`.
- `type LinkError string` cu `func (e LinkError) Error() string`. Mesajele exacte:
  `un agent nu poate fi propriul sef`, `seful ales nu e in acest proiect`, `legatura ar crea un ciclu`, `un agent nu poate fi propriul reviewer`, `reviewer-ul ales nu e in acest proiect`.
- `CreateAgent(a *Agent) error` si `UpdateAgent(a Agent) error` verifica legaturile (`checkLinks`) si scriu `parent_id`, `reviewer_id`, `responsibilities`, in aceeasi tranzactie. `UpdateAgent` nu atinge `pos_x`/`pos_y`. Nume duplicat: tot `ErrConflict`.
- `SetAgentPosition(id int64, x, y float64) error` (`ErrNotFound` daca nu exista).
- `DeleteAgent(id int64) error`: `ErrConflict` daca agentul are task-uri asignate **sau** e ultimul agent din proiect. Altfel copiii trec la seful lui, `reviewer_id` care il indica devine NULL, apoi randul se sterge. Radacina se poate sterge (copiii ei devin radacini).
- `ListAgents(projectID)`: radacinile primele, apoi restul, fiecare grup dupa `id` (`ORDER BY parent_id IS NOT NULL, id`).
- `Lead(projectID) (Agent, error)` ramane temporar si intoarce **prima radacina**. Se sterge in Task 5.
- `CreateTask(t *Task)` scrie `creator_id`, `parent_task_id`; `review_stage` porneste `''`. `SaveTask` scrie si `review_stage`.
- `Subtasks(parentTaskID int64) ([]Task, error)`: ordonate dupa `number`, mereu non-nil.
- `RequeueInProgress` si `RequeueAllInProgress` **sar** task-urile care au sub-task-uri nefinalizate (raman `in_progress`).
- In `graph.go`, functii pure (fara DB): `Roles`, `RolesOf`, `Children`, `IsAncestor`, `FindAgent` (vezi tabelul de nume).

- [ ] **Step 1: Teste**

In `store_test.go`:
- `TestOpenAppliesMigrationsOnce`: `user_version` asteptat devine **3**.
- helper-e noi (inlocuiesc `newWorker` vechi):
  ```go
  func ptr(v int64) *int64 { return &v }

  // newAgent adauga un agent cu seful dat (nil = radacina).
  func newAgent(t *testing.T, s *Store, projectID int64, name string, parent *int64) Agent {
  	t.Helper()
  	a := Agent{ProjectID: projectID, Name: name, Role: "Dev", Model: "sonnet", Tools: []string{"write"}, ParentID: parent}
  	if err := s.CreateAgent(&a); err != nil {
  		t.Fatal(err)
  	}
  	return a
  }

  // newWorker adauga un agent sub prima radacina a proiectului.
  func newWorker(t *testing.T, s *Store, projectID int64, name string) Agent {
  	t.Helper()
  	agents, err := s.ListAgents(projectID)
  	if err != nil || len(agents) == 0 {
  		t.Fatalf("proiect fara agenti: %v", err)
  	}
  	return newAgent(t, s, projectID, name, &agents[0].ID)
  }
  ```
- `TestOnlyOneLead` se **sterge** (indexul nu mai exista). In locul lui: `TestSeveralRootsAllowed` creeaza al doilea agent cu `ParentID: nil` si asteapta `err == nil`; `ListAgents` intoarce ambele radacini inaintea oricarui worker.
- `TestDeleteLeadRejected` se inlocuieste cu `TestDeleteLastAgentRejected`: proiect doar cu lead-ul, `DeleteAgent(lead.ID)` da `ErrConflict`.
- `TestCreateProjectAddsLeadAndDefaults`: in loc de `lead.Position`, verifica `lead.ParentID == nil`; restul ramane.
- In literalele de `Agent` din testele existente sterge `Position: ...` (inserarea pune singura `worker`).

Teste noi:

```go
func TestMigration003ConvertsOldProject(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vechi.db")
	// o baza "veche": doar migrarile 001 si 002, cu un proiect lead + worker
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"001_init.sql", "002_attachments.sql"} {
		body, _ := migrations.ReadFile("migrations/" + name)
		if _, err := db.Exec(string(body)); err != nil {
			t.Fatal(err)
		}
	}
	for _, q := range []string{
		`PRAGMA user_version = 2`,
		`INSERT INTO projects (id, name, workdir, created_at) VALUES (1, 'Vechi', '/tmp', '2026-01-01T00:00:00Z')`,
		`INSERT INTO agents (id, project_id, name, position, created_at) VALUES (1, 1, 'Lead', 'lead', '2026-01-01T00:00:00Z'), (2, 1, 'Rook', 'worker', '2026-01-01T00:00:00Z')`,
		`INSERT INTO tasks (id, project_id, number, title, assignee_id, status, created_at, updated_at) VALUES
			(1, 1, 1, 'A', 2, 'review', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z'),
			(2, 1, 2, 'B', 2, 'queued', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	db.Close()

	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	lead, _ := s.GetAgent(1)
	rook, _ := s.GetAgent(2)
	if lead.ParentID != nil || rook.ParentID == nil || *rook.ParentID != 1 {
		t.Fatalf("parent_id: lead=%v rook=%v", lead.ParentID, rook.ParentID)
	}
	a, _ := s.GetTask(1)
	b, _ := s.GetTask(2)
	if a.CreatorID == nil || *a.CreatorID != 1 || a.ReviewStage != StageLead {
		t.Fatalf("task in review dupa migrare: %+v", a)
	}
	if b.CreatorID == nil || *b.CreatorID != 1 || b.ReviewStage != StageNone {
		t.Fatalf("task in queued dupa migrare: %+v", b)
	}
	// agents_one_lead nu mai exista: o a doua radacina e permisa
	second := Agent{ProjectID: 1, Name: "Al doilea", Model: "sonnet"}
	if err := s.CreateAgent(&second); err != nil {
		t.Fatalf("a doua radacina: %v", err)
	}
}

func TestLinksInvariants(t *testing.T) {
	s := newStore(t)
	p, lead := newProject(t, s)
	a := newAgent(t, s, p.ID, "A", &lead.ID)
	b := newAgent(t, s, p.ID, "B", &a.ID)
	_, otherLead := newProject(t, s)

	cases := []struct {
		name string
		edit func() Agent
		want string
	}{
		{"propriul sef", func() Agent { x := a; x.ParentID = &a.ID; return x }, "un agent nu poate fi propriul sef"},
		{"ciclu", func() Agent { x := lead; x.ParentID = &b.ID; return x }, "legatura ar crea un ciclu"},
		{"sef din alt proiect", func() Agent { x := a; x.ParentID = &otherLead.ID; return x }, "seful ales nu e in acest proiect"},
		{"sef inexistent", func() Agent { x := a; x.ParentID = ptr(9999); return x }, "seful ales nu e in acest proiect"},
		{"propriul reviewer", func() Agent { x := a; x.ReviewerID = &a.ID; return x }, "un agent nu poate fi propriul reviewer"},
		{"reviewer din alt proiect", func() Agent { x := a; x.ReviewerID = &otherLead.ID; return x }, "reviewer-ul ales nu e in acest proiect"},
	}
	for _, c := range cases {
		err := s.UpdateAgent(c.edit())
		var le LinkError
		if !errors.As(err, &le) || le.Error() != c.want {
			t.Errorf("%s: %v, vreau %q", c.name, err, c.want)
		}
	}
	// nimic nu s-a salvat
	if got, _ := s.GetAgent(lead.ID); got.ParentID != nil {
		t.Fatal("legatura invalida a fost salvata")
	}
	// review impotriva ierarhiei e permis: A il verifica pe B si B pe A
	a.ReviewerID = &b.ID
	b.ReviewerID = &a.ID
	if err := s.UpdateAgent(a); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateAgent(b); err != nil {
		t.Fatal(err)
	}
	// agent nou cu reviewer din alt proiect
	bad := Agent{ProjectID: p.ID, Name: "X", Model: "sonnet", ReviewerID: &otherLead.ID}
	var le LinkError
	if err := s.CreateAgent(&bad); !errors.As(err, &le) {
		t.Fatalf("creare cu reviewer strain: %v", err)
	}
	a.Responsibilities = "API si baze de date"
	s.UpdateAgent(a)
	if got, _ := s.GetAgent(a.ID); got.Responsibilities != "API si baze de date" || *got.ReviewerID != b.ID {
		t.Fatalf("dupa update: %+v", got)
	}
}

func TestDeleteAgentReparents(t *testing.T) {
	s := newStore(t)
	p, lead := newProject(t, s)
	mid := newAgent(t, s, p.ID, "Mid", &lead.ID)
	w1 := newAgent(t, s, p.ID, "W1", &mid.ID)
	w2 := newAgent(t, s, p.ID, "W2", &mid.ID)
	rev := newAgent(t, s, p.ID, "Rev", &lead.ID)
	w1.ReviewerID = &rev.ID
	s.UpdateAgent(w1)
	task := Task{ProjectID: p.ID, Title: "A", AssigneeID: w1.ID, CreatorID: &mid.ID}
	s.CreateTask(&task)

	if err := s.DeleteAgent(mid.ID); err != nil {
		t.Fatal(err)
	}
	for _, id := range []int64{w1.ID, w2.ID} {
		if got, _ := s.GetAgent(id); got.ParentID == nil || *got.ParentID != lead.ID {
			t.Fatalf("agentul %d nu a trecut la lead: %+v", id, got.ParentID)
		}
	}
	if got, _ := s.GetTask(task.ID); got.CreatorID != nil {
		t.Fatalf("creator_id trebuia NULL dupa stergerea creatorului: %v", *got.CreatorID)
	}
	if err := s.DeleteAgent(rev.ID); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.GetAgent(w1.ID); got.ReviewerID != nil {
		t.Fatal("reviewer_id trebuia NULL dupa stergerea reviewer-ului")
	}
	// radacina se poate sterge: copiii ei devin radacini
	if err := s.DeleteAgent(lead.ID); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.GetAgent(w2.ID); got.ParentID != nil {
		t.Fatal("w2 trebuia sa devina radacina")
	}
}

func TestSetAgentPosition(t *testing.T) {
	s := newStore(t)
	_, lead := newProject(t, s)
	if lead.PosX != nil || lead.PosY != nil {
		t.Fatal("un agent nou nu are pozitie")
	}
	if err := s.SetAgentPosition(lead.ID, 120.5, 80); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetAgent(lead.ID)
	if got.PosX == nil || *got.PosX != 120.5 || *got.PosY != 80 {
		t.Fatalf("pozitie: %v %v", got.PosX, got.PosY)
	}
	if err := s.SetAgentPosition(9999, 1, 1); !errors.Is(err, ErrNotFound) {
		t.Fatalf("agent inexistent: %v", err)
	}
}
```

In `tasks_test.go`:

```go
func TestRequeueKeepsWaitingParent(t *testing.T) {
	s := newStore(t)
	p, lead := newProject(t, s)
	sub := newAgent(t, s, p.ID, "Sub", &lead.ID)
	w := newAgent(t, s, p.ID, "W", &sub.ID)
	parent := Task{ProjectID: p.ID, Title: "Import", AssigneeID: sub.ID, CreatorID: &lead.ID}
	s.CreateTask(&parent)
	parent.Status = StatusInProgress
	s.SaveTask(&parent)
	child := Task{ProjectID: p.ID, Title: "Parser", AssigneeID: w.ID, CreatorID: &sub.ID, ParentTaskID: &parent.ID}
	s.CreateTask(&child)
	child.Status, child.ReviewStage = StatusInProgress, StageNone
	s.SaveTask(&child)

	subs, err := s.Subtasks(parent.ID)
	if err != nil || len(subs) != 1 || subs[0].ID != child.ID || *subs[0].CreatorID != sub.ID {
		t.Fatalf("Subtasks = %+v, %v", subs, err)
	}
	if err := s.RequeueInProgress(p.ID); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.GetTask(parent.ID); got.Status != StatusInProgress {
		t.Fatalf("parintele care asteapta a fost mutat in %s", got.Status)
	}
	if got, _ := s.GetTask(child.ID); got.Status != StatusQueued {
		t.Fatalf("sub-task-ul trebuia in queued: %s", got.Status)
	}
	child.Status = StatusDone
	s.SaveTask(&child)
	if err := s.RequeueAllInProgress(); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.GetTask(parent.ID); got.Status != StatusQueued {
		t.Fatalf("fara sub-task-uri deschise, parintele revine in queued: %s", got.Status)
	}
	if subs, _ := s.Subtasks(child.ID); subs == nil || len(subs) != 0 {
		t.Fatal("Subtasks trebuie sa intoarca [] nu nil")
	}
}

func TestReviewStageSaved(t *testing.T) {
	s := newStore(t)
	p, _ := newProject(t, s)
	w := newWorker(t, s, p.ID, "Rook")
	task := Task{ProjectID: p.ID, Title: "A", AssigneeID: w.ID}
	s.CreateTask(&task)
	if task.ReviewStage != StageNone || task.CreatorID != nil || task.ParentTaskID != nil {
		t.Fatalf("task nou: %+v", task)
	}
	task.Status, task.ReviewStage = StatusReview, StageReviewer
	s.SaveTask(&task)
	if got, _ := s.GetTask(task.ID); got.ReviewStage != StageReviewer {
		t.Fatalf("review_stage = %q", got.ReviewStage)
	}
}
```

`graph_test.go`:

```go
package store

import "testing"

func TestGraphHelpers(t *testing.T) {
	one, two := int64(1), int64(2)
	team := []Agent{
		{ID: 1, Name: "Coord"},
		{ID: 2, Name: "Sub", ParentID: &one},
		{ID: 3, Name: "Wes", ParentID: &two, ReviewerID: &one},
		{ID: 4, Name: "Solo"},
	}
	want := []Roles{
		{IsLead: true, IsRoot: true, IsReviewer: true},
		{IsLead: true},
		{},
		{IsRoot: true},
	}
	for i, a := range team {
		if got := RolesOf(a, team); got != want[i] {
			t.Errorf("RolesOf(%s) = %+v, vreau %+v", a.Name, got, want[i])
		}
	}
	if c := Children(1, team); len(c) != 1 || c[0].Name != "Sub" {
		t.Fatalf("Children(1) = %+v", c)
	}
	checks := []struct {
		anc, id int64
		want    bool
	}{{1, 3, true}, {2, 3, true}, {3, 1, false}, {4, 3, false}, {3, 3, false}, {1, 1, false}}
	for _, c := range checks {
		if got := IsAncestor(c.anc, c.id, team); got != c.want {
			t.Errorf("IsAncestor(%d, %d) = %v", c.anc, c.id, got)
		}
	}
	if _, ok := FindAgent(team, 9); ok {
		t.Fatal("FindAgent(9) a gasit ceva")
	}
}
```

Ruleaza `go test ./internal/store/`. Trebuie sa pice la compilare.

- [ ] **Step 2: Migrarea `internal/store/migrations/003_team_graph.sql`**

```sql
-- ierarhie si review: legaturile agentilor
ALTER TABLE agents ADD COLUMN parent_id INTEGER REFERENCES agents(id) ON DELETE SET NULL;
ALTER TABLE agents ADD COLUMN reviewer_id INTEGER REFERENCES agents(id) ON DELETE SET NULL;
ALTER TABLE agents ADD COLUMN responsibilities TEXT NOT NULL DEFAULT '';
ALTER TABLE agents ADD COLUMN pos_x REAL;
ALTER TABLE agents ADD COLUMN pos_y REAL;

-- mai multi lideri pe proiect; coloana position ramane, dar nu mai e folosita
DROP INDEX IF EXISTS agents_one_lead;

-- proiectele vechi: workerii raporteaza fostului lead
UPDATE agents
   SET parent_id = (SELECT l.id FROM agents l WHERE l.project_id = agents.project_id AND l.position = 'lead')
 WHERE position = 'worker';

ALTER TABLE tasks ADD COLUMN creator_id INTEGER REFERENCES agents(id) ON DELETE SET NULL;
ALTER TABLE tasks ADD COLUMN parent_task_id INTEGER REFERENCES tasks(id) ON DELETE SET NULL;
ALTER TABLE tasks ADD COLUMN review_stage TEXT NOT NULL DEFAULT '' CHECK (review_stage IN ('', 'reviewer', 'lead'));

-- task-urile vechi au fost create de lead, iar cele din review asteapta decizia lui
UPDATE tasks
   SET creator_id = (SELECT l.id FROM agents l WHERE l.project_id = tasks.project_id AND l.position = 'lead');
UPDATE tasks SET review_stage = 'lead' WHERE status = 'review';

CREATE INDEX agents_parent ON agents(parent_id);
CREATE INDEX tasks_parent ON tasks(parent_task_id);
```

Note pentru implementare:
- SQLite accepta `ADD COLUMN ... REFERENCES` cu FK active doar daca valoarea implicita e NULL. Asa e aici.
- `ON DELETE SET NULL` e plasa de siguranta: codul re-parenteaza explicit inainte de stergere, dar `tasks.creator_id` se bazeaza pe ea.

- [ ] **Step 3: Implementare**

`agents.go`:
- `agentCols` capata la final `, parent_id, reviewer_id, responsibilities, pos_x, pos_y`; `scanAgent` scaneaza direct in `&a.ParentID`, `&a.ReviewerID`, `&a.Responsibilities`, `&a.PosX`, `&a.PosY`. `database/sql` stie sa scaneze NULL intr-un pointer-la-pointer (pune nil) si o valoare (aloca).
- La scriere nu te baza pe driver pentru pointeri. Adauga in `runs.go`, langa `nullID`:
  ```go
  // nullPtr transforma un pointer nil in NULL pentru coloanele optionale.
  func nullPtr(p *int64) any {
  	if p == nil {
  		return nil
  	}
  	return *p
  }
  ```
- `insertAgent`: scrie si `parent_id, reviewer_id, responsibilities`; `position` primeste `a.Position` sau `PositionWorker` daca e gol (coloana are CHECK si NOT NULL).
- Interfata folosita de `checkLinks`, implementata si de `*sql.DB`, si de `*sql.Tx`:
  ```go
  type queryer interface {
  	QueryRow(query string, args ...any) *sql.Row
  }
  ```
- `checkLinks`, exact:
  ```go
  // checkLinks verifica invariantii legaturilor inainte de salvare: seful si
  // reviewer-ul sunt din acelasi proiect, nimeni nu e propriul sef sau
  // reviewer, iar lantul de sefi nu face cerc.
  func checkLinks(q queryer, a Agent) error {
  	if a.ParentID != nil {
  		if *a.ParentID == a.ID {
  			return LinkError("un agent nu poate fi propriul sef")
  		}
  		if !inProject(q, *a.ParentID, a.ProjectID) {
  			return LinkError("seful ales nu e in acest proiect")
  		}
  		// Urcam pe lantul de sefi pornind de la noul sef. Daca ajungem la
  		// agentul editat, legatura ar inchide un cerc. Un agent nou (ID 0) nu e
  		// inca seful nimanui, deci nu poate crea un ciclu.
  		if a.ID != 0 {
  			cur := *a.ParentID
  			for steps := 0; steps < 10000; steps++ { // limita: protectie la date deja stricate
  				if cur == a.ID {
  					return LinkError("legatura ar crea un ciclu")
  				}
  				var next sql.NullInt64
  				if err := q.QueryRow(`SELECT parent_id FROM agents WHERE id = ?`, cur).Scan(&next); err != nil {
  					return err
  				}
  				if !next.Valid {
  					break
  				}
  				cur = next.Int64
  			}
  		}
  	}
  	if a.ReviewerID != nil {
  		if *a.ReviewerID == a.ID {
  			return LinkError("un agent nu poate fi propriul reviewer")
  		}
  		if !inProject(q, *a.ReviewerID, a.ProjectID) {
  			return LinkError("reviewer-ul ales nu e in acest proiect")
  		}
  	}
  	return nil
  }

  func inProject(q queryer, agentID, projectID int64) bool {
  	var n int
  	q.QueryRow(`SELECT COUNT(*) FROM agents WHERE id = ? AND project_id = ?`, agentID, projectID).Scan(&n)
  	return n == 1
  }
  ```
- `CreateAgent`: tranzactie → `checkLinks(tx, *a)` → `insertAgent(tx, a)` → commit. `UpdateAgent`: tranzactie → `checkLinks(tx, a)` → `UPDATE agents SET name, role, model, background, instructions, done_def, tools, responsibilities, parent_id, reviewer_id WHERE id = ?` → commit. Respecta comentariul din `Open`: in tranzactie doar `tx`, niciodata `s.db`.
- `DeleteAgent`, exact:
  ```go
  // DeleteAgent sterge un agent. Copiii lui trec la seful lui (sau devin
  // radacini), iar cei pe care ii verifica raman fara reviewer. Agentii cu
  // task-uri si ultimul agent din proiect nu se pot sterge (ErrConflict).
  func (s *Store) DeleteAgent(id int64) error {
  	tx, err := s.db.Begin()
  	if err != nil {
  		return err
  	}
  	defer tx.Rollback()
  	a, err := scanAgent(tx.QueryRow(`SELECT `+agentCols+` FROM agents WHERE id = ?`, id))
  	if err != nil {
  		return err
  	}
  	var tasks, others int
  	if err := tx.QueryRow(`SELECT COUNT(*) FROM tasks WHERE assignee_id = ?`, id).Scan(&tasks); err != nil {
  		return err
  	}
  	if err := tx.QueryRow(`SELECT COUNT(*) FROM agents WHERE project_id = ? AND id != ?`, a.ProjectID, id).Scan(&others); err != nil {
  		return err
  	}
  	if tasks > 0 || others == 0 {
  		return ErrConflict
  	}
  	if _, err := tx.Exec(`UPDATE agents SET parent_id = ? WHERE parent_id = ?`, nullPtr(a.ParentID), id); err != nil {
  		return err
  	}
  	if _, err := tx.Exec(`UPDATE agents SET reviewer_id = NULL WHERE reviewer_id = ?`, id); err != nil {
  		return err
  	}
  	if err := mustAffect(tx.Exec(`DELETE FROM agents WHERE id = ?`, id)); err != nil {
  		return err
  	}
  	return tx.Commit()
  }
  ```
- `ListAgents`: `ORDER BY parent_id IS NOT NULL, id`. Comentariul devine „radacinile primele, apoi restul, in ordinea adaugarii”.
- `Lead`: `WHERE project_id = ? AND parent_id IS NULL ORDER BY id LIMIT 1`, cu comentariul de deprecare.
- `SetAgentPosition`: `mustAffect(s.db.Exec("UPDATE agents SET pos_x = ?, pos_y = ? WHERE id = ?", x, y, id))`.

`projects.go`: lead-ul implicit ramane `Agent{ProjectID, Name: "Lead", Role: "Lead", Position: PositionLead, Model: "sonnet"}` pana in Task 5; `ParentID` e nil, deci e radacina.

`tasks.go`:
- `taskCols` capata `, creator_id, parent_task_id, review_stage`; scanarea in `&t.CreatorID, &t.ParentTaskID, &t.ReviewStage`.
- `CreateTask` insereaza `nullPtr(t.CreatorID), nullPtr(t.ParentTaskID)` si pune `t.ReviewStage = StageNone`.
- `SaveTask`: `SET status, result, feedback, send_backs, review_stage, updated_at`.
- Requeue, exact:
  ```go
  // waitingParent: un task cu sub-task-uri nefinalizate nu are nevoie de un run
  // nou; il tine in lucru sub-liderul pana cand toate sub-task-urile sunt Done.
  const notWaitingParent = ` AND NOT EXISTS (SELECT 1 FROM tasks sub WHERE sub.parent_task_id = tasks.id AND sub.status != 'done')`

  func (s *Store) RequeueInProgress(projectID int64) error {
  	_, err := s.db.Exec(`UPDATE tasks SET status = 'queued', updated_at = ?
  		WHERE project_id = ? AND status = 'in_progress'`+notWaitingParent, now(), projectID)
  	return err
  }
  ```
  La fel `RequeueAllInProgress`, fara filtrul de proiect.

`graph.go`:
```go
// Rolurile unui agent nu se stocheaza: reies din legaturi.
type Roles struct {
	IsLead     bool `json:"isLead"`     // are cel putin un agent direct sub el
	IsRoot     bool `json:"isRoot"`     // raporteaza utilizatorului
	IsReviewer bool `json:"isReviewer"` // verifica munca cel putin unui agent
}

func RolesOf(a Agent, team []Agent) Roles {
	r := Roles{IsRoot: a.ParentID == nil}
	for _, o := range team {
		if o.ParentID != nil && *o.ParentID == a.ID {
			r.IsLead = true
		}
		if o.ReviewerID != nil && *o.ReviewerID == a.ID {
			r.IsReviewer = true
		}
	}
	return r
}
```
Plus `Children` (filtreaza `team` pastrand ordinea), `FindAgent` si `IsAncestor`. `IsAncestor` urca de la seful lui `id`, cu maxim `len(team)` pasi (protectie la cicluri in date stricate). `IsAncestor(x, x)` e false.

Actualizari in afara pachetului (doar ca testele existente sa ramana verzi si realiste):
- `internal/team/fake_test.go`, `setup`: `rook` si `vera` primesc `ParentID: &lead.ID` si pierd `Position`. Adauga helper-ul:
  ```go
  // agent adauga un agent in proiectul fixture-ului; parent/reviewer pot fi nil.
  func (f *fixture) agent(t *testing.T, name string, parent, reviewer *store.Agent) store.Agent {
  	t.Helper()
  	a := store.Agent{ProjectID: f.p.ID, Name: name, Role: "Dev", Model: "haiku"}
  	if parent != nil {
  		a.ParentID = &parent.ID
  	}
  	if reviewer != nil {
  		a.ReviewerID = &reviewer.ID
  	}
  	if err := f.st.CreateAgent(&a); err != nil {
  		t.Fatal(err)
  	}
  	return a
  }
  ```
- `internal/team/reconcile_test.go`, `TestUnlimitedParallel`: `quill` primeste `ParentID: &f.lead.ID` si pierde `Position`.
- `internal/web/server_test.go`, `addWorker`: pune `ParentID` = id-ul primei radacini (`e.st.ListAgents(projectID)`, primul element) si scoate `Position`.
- `TestDeleteAgentConflicts`: cazul „lead” devine „ultimul agent”: un al doilea proiect fara workeri, `DELETE` pe lead-ul lui da 409. Cazul „worker cu task-uri” ramane.

- [ ] **Step 4: Verificare si commit**

`go test -race ./internal/store/ ./internal/team/ ./internal/web/`, apoi Verificarea standard.

```bash
git add internal/store/migrations/003_team_graph.sql internal/store/agents.go internal/store/tasks.go internal/store/graph.go internal/store/projects.go internal/store/runs.go internal/store/store_test.go internal/store/tasks_test.go internal/store/graph_test.go internal/team/fake_test.go internal/team/reconcile_test.go internal/web/server_test.go
git commit -m "store: ierarhie si review in schema (migrarea 003)"
```

---

### Task 2: prompt (roluri compuse, echipa directa, task-uri deschise) + legarea in team

**Files:** `internal/prompt/prompt.go`, `internal/prompt/prompt_test.go`, `internal/team/run.go` (doar `startRun`), `internal/team/reconcile.go` (doar construirea turelor), `internal/team/reconcile_test.go` (asteptarile de text), `internal/team/graph_test.go` (nou)

**Interfaces:**

Consumes: `store.RolesOf`, `store.Children`, `store.FindAgent`, campurile noi din `store.Agent`/`store.Task` (Task 1).

Produces (in `prompt`):
```go
// Links descrie locul agentului in echipa; intra in system prompt.
type Links struct {
	BossName   string // "" = raporteaza utilizatorului
	IsLead     bool
	IsReviewer bool
}

// LinksFor calculeaza Links din lista echipei. Merge si pentru un agent nou
// (ID 0): atunci doar BossName poate fi setat.
func LinksFor(a store.Agent, team []store.Agent) Links

// Member e un rand din "Echipa ta directa".
type Member struct {
	Name, Role, Responsibilities, Status string
}

// OpenTask e un task primit de un lider si inca in lucru.
type OpenTask struct {
	Ref, Title  string
	Done, Total int // sub-task-uri gata / total
}

type TurnContext struct {
	Direct    []Member   // doar pentru lideri: agentii direct sub el
	Reviewees []string   // numele agentilor pe care ii verifica
	OpenTasks []OpenTask // task-urile primite de lider, in lucru (fara cel curent)
}

func System(p store.Project, a store.Agent, l Links) string
func Team(members []Member) string
func Context(c TurnContext) string
func TaskTurn(c TurnContext, t store.Task, subtasks, notes []string) string
func NotifyTurn(c TurnContext, notes []string) string
```
Se sterg: `Member.Position`, `WorkerTurn`, `DirectTurn`, `LeadTurn`.

Produces (in `team`):
- `startRun(p store.Project, a store.Agent, agents []store.Agent, taskID int64, turn string) error`. System prompt-ul foloseste `prompt.System(p, a, prompt.LinksFor(a, agents))`. `Caller` ramane `Lead: a.Position == store.PositionLead` pana in Task 3.
- `(*Team) turnContext(a store.Agent, agents []store.Agent, tasks []store.Task, currentTaskID int64) prompt.TurnContext`
- `subtaskLines(taskID int64, tasks []store.Task, byID map[int64]store.Agent) []string` (functie simpla), randuri `T-5 [done] Lexer -> Wes`.
- `(*Team) texts(projectID int64, msgs []store.Message) []string`: parametrul `labelUser` dispare; mesajele utilizatorului sunt mereu prefixate cu `Utilizatorul: `.

**Formatele exacte:**
- linia de rol: `Esti <Name>, <Role sau "agent">, in proiectul <Proiect>. Raportezi lui <Boss>.` sau `... Raportezi utilizatorului.`;
- „Cand termini” (radacina): `# Cand termini\nCand obiectivul e atins, scrie utilizatorului un rezumat.` plus ` Rezumatul contine: <done>` daca exista; cu sef: `# Cand termini un task\nApeleaza mcp__team__report_done cu un rezumat al muncii.` sau `... cu: <done>`;
- „Cum lucrezi” se compune, blocurile fiind separate prin `\n\n`:
  - lider: `howLead`; daca are si sef, `howSubLead`; daca e radacina, `howRootLead`;
  - are sef si nu e lider: `howWorker` (textul de acum);
  - reviewer: `howReviewer`;
  - radacina fara copii si fara review: `howSolo`.

  ```go
  const howLead = `Esti lider: ai o echipa directa, listata in mesajul turei. Sparge munca in task-uri mici, clare si verificabile ` +
  	`si creeaza-le cu mcp__team__create_task, doar pentru agentii din echipa ta directa, alegand dupa campul "Ce ia" al fiecaruia. ` +
  	`Nu face tu munca lor. Cand primesti rezultate, verifica-le (poti citi fisierele modificate) si apeleaza ` +
  	`mcp__team__accept_task sau mcp__team__send_back cu feedback concret. Foloseste mcp__team__list_tasks ca sa vezi starea. ` +
  	`Nu folosi alte liste de task-uri in afara de tool-urile mcp__team__.`
  const howSubLead = `Cand primesti un task de la seful tau, sparge-l in sub-task-uri pentru echipa ta si opreste-te fara report_done. ` +
  	`Cand primesti notificarea ca toate sub-task-urile sunt gata, verifica rezultatul si apeleaza mcp__team__report_done ` +
  	`cu task-ul (ex. task: "T-3") si rezumatul.`
  const howRootLead = `Cand obiectivul e atins, scrie utilizatorului un rezumat.`
  const howReviewer = `Verifici munca agentilor din "Revizuiesti munca lui" (din mesajul turei). Cand primesti un task de verificat, ` +
  	`citeste rezultatul si fisierele schimbate, apoi apeleaza mcp__team__review_approve daca e bun, sau ` +
  	`mcp__team__review_reject cu feedback concret: ce e gresit si ce trebuie schimbat.`
  const howSolo = `Vorbesti direct cu utilizatorul si nu ai echipa. Raspunde la mesajele lui si fa munca ceruta.`
  ```
- `Team`: `Echipa ta directa:` apoi, pentru fiecare membru, `\n- <Name>`, plus `: <Role>, <Status>` (doar partile nevide, unite cu `, `), plus `\n  Ce ia: <Responsibilities>` daca exista. O lista goala da `""`.
- `Context`: blocurile nevide, unite cu `\n\n`: `Team(Direct)`; `Revizuiesti munca lui: A, B`; `Task-urile tale deschise:` cu randuri `- T-1: Import (sub-task-uri gata: 1 din 2)`, sau `- T-4: Export (fara sub-task-uri inca)` cand `Total == 0`.
- `TaskTurn`: partile nevide, unite cu `\n\n`:
  - `Context(c)`;
  - `Task T-3: Titlu` si, pe randul urmator, descrierea;
  - `bullets("Sub-task-uri deja create pentru acest task (nu le crea din nou):", subtasks)`;
  - `Feedback la incercarea anterioara:\n<feedback>`;
  - `bullets("Notificari:", notes)`.
- `NotifyTurn`: `Context(c)`, apoi `bullets("Notificari:", notes)`, unite cu `\n\n` (sau doar bullet-urile, daca contextul e gol).
- In `team.turnContext`:
  - `Direct` = `store.Children(a.ID, agents)`, cu `Status: t.statusOf(child, tasks, false)`;
  - `Reviewees` = numele agentilor cu `ReviewerID == a.ID`, in ordinea din `agents`;
  - `OpenTasks` (doar daca `a` e lider) = task-urile asignate lui `a` cu status `in_progress`, fara `currentTaskID`, cu `Done`/`Total` numarate din `tasks` dupa `ParentTaskID`.

- [ ] **Step 1: Teste `prompt_test.go`**

Inlocuieste `TestSystemWorker`, `TestSystemLead`, `TestTeam`, `TestWorkerTurn`, `TestDirectAndLeadTurn` si sterge variabila `team` de la nivel de pachet (foloseste `Member.Position`). Testele noi:

```go
func TestSystemRoles(t *testing.T) {
	a := store.Agent{Name: "Sub", Role: "Tech Lead", Background: "Stie Laravel.", DoneDef: "rezumat si fisiere schimbate"}
	cases := []struct {
		name      string
		links     Links
		want, not []string
	}{
		{"worker", Links{BossName: "Atlas"},
			[]string{"# Rol\nEsti Sub, Tech Lead, in proiectul ApiUnit. Raportezi lui Atlas.",
				"# Cand termini un task\nApeleaza mcp__team__report_done cu: rezumat si fisiere schimbate",
				"# Cum lucrezi\nLucreaza doar pe task-ul primit", "# Background\nStie Laravel."},
			[]string{"create_task", "review_approve"}},
		{"sub-lider", Links{BossName: "Atlas", IsLead: true},
			[]string{"mcp__team__create_task", "opreste-te fara report_done", `"Ce ia"`},
			[]string{"Lucreaza doar pe task-ul primit", "review_approve"}},
		{"radacina lider", Links{IsLead: true},
			[]string{"Raportezi utilizatorului.", "mcp__team__create_task",
				"# Cand termini\nCand obiectivul e atins, scrie utilizatorului un rezumat. Rezumatul contine: rezumat si fisiere schimbate"},
			[]string{"report_done"}},
		{"reviewer", Links{BossName: "Atlas", IsReviewer: true},
			[]string{"mcp__team__review_approve", "mcp__team__review_reject", "Lucreaza doar pe task-ul primit"},
			[]string{"create_task"}},
		{"singur", Links{},
			[]string{"Raportezi utilizatorului.", "Vorbesti direct cu utilizatorul"},
			[]string{"mcp__team__"}},
	}
	for _, c := range cases {
		got := System(proj, a, c.links)
		for _, w := range c.want {
			if !strings.Contains(got, w) {
				t.Errorf("%s: lipseste %q din:\n%s", c.name, w, got)
			}
		}
		for _, n := range c.not {
			if strings.Contains(got, n) {
				t.Errorf("%s: nu trebuia %q in:\n%s", c.name, n, got)
			}
		}
	}
	if got := System(store.Project{Name: "X"}, store.Agent{Name: "A"}, Links{}); !strings.Contains(got, "Esti A, agent, in proiectul X.") {
		t.Errorf("rol gol: %s", got)
	}
}

func TestLinksFor(t *testing.T) {
	one, two := int64(1), int64(2)
	lead := store.Agent{ID: 1, Name: "Atlas"}
	sub := store.Agent{ID: 2, Name: "Sub", ParentID: &one}
	wes := store.Agent{ID: 3, Name: "Wes", ParentID: &two, ReviewerID: &one}
	team := []store.Agent{lead, sub, wes}
	cases := map[string]struct {
		a    store.Agent
		want Links
	}{
		"coordonator": {lead, Links{IsLead: true, IsReviewer: true}},
		"sub-lider":   {sub, Links{BossName: "Atlas", IsLead: true}},
		"worker":      {wes, Links{BossName: "Sub"}},
		"agent nou":   {store.Agent{ParentID: &two}, Links{BossName: "Sub"}},
	}
	for name, c := range cases {
		if got := LinksFor(c.a, team); got != c.want {
			t.Errorf("%s: %+v, vreau %+v", name, got, c.want)
		}
	}
}

func TestTeamBlock(t *testing.T) {
	got := Team([]Member{{Name: "Rook", Role: "Backend Dev", Responsibilities: "API si baze de date", Status: "Idle"}, {Name: "Vera"}})
	if got != "Echipa ta directa:\n- Rook: Backend Dev, Idle\n  Ce ia: API si baze de date\n- Vera" {
		t.Fatalf("Team = %q", got)
	}
	if Team(nil) != "" {
		t.Fatal("echipa goala trebuie sa dea text gol")
	}
}

func TestContextAndTurns(t *testing.T) {
	c := TurnContext{
		Direct:    []Member{{Name: "Wes", Role: "Dev", Status: "Idle"}},
		Reviewees: []string{"Ana", "Ion"},
		OpenTasks: []OpenTask{{Ref: "T-1", Title: "Import", Done: 1, Total: 2}, {Ref: "T-4", Title: "Export"}},
	}
	ctx := "Echipa ta directa:\n- Wes: Dev, Idle" +
		"\n\nRevizuiesti munca lui: Ana, Ion" +
		"\n\nTask-urile tale deschise:\n- T-1: Import (sub-task-uri gata: 1 din 2)\n- T-4: Export (fara sub-task-uri inca)"
	if got := Context(c); got != ctx {
		t.Fatalf("Context =\n%s", got)
	}
	if Context(TurnContext{}) != "" {
		t.Fatal("context gol")
	}
	task := store.Task{Number: 3, Title: "Parser", Description: "Doar local.", Feedback: "Lipsesc testele."}
	got := TaskTurn(c, task, []string{"T-5 [done] Lexer -> Wes"}, []string{"Utilizatorul: grabeste-te"})
	want := ctx + "\n\nTask T-3: Parser\nDoar local." +
		"\n\nSub-task-uri deja create pentru acest task (nu le crea din nou):\n- T-5 [done] Lexer -> Wes" +
		"\n\nFeedback la incercarea anterioara:\nLipsesc testele." +
		"\n\nNotificari:\n- Utilizatorul: grabeste-te"
	if got != want {
		t.Fatalf("TaskTurn =\n%s\nvreau\n%s", got, want)
	}
	if got := TaskTurn(TurnContext{}, store.Task{Number: 1, Title: "A"}, nil, nil); got != "Task T-1: A" {
		t.Fatalf("simplu: %q", got)
	}
	if got := NotifyTurn(TurnContext{}, []string{"salut"}); got != "Notificari:\n- salut" {
		t.Fatalf("NotifyTurn gol: %q", got)
	}
	if got := NotifyTurn(c, []string{"x"}); got != ctx+"\n\nNotificari:\n- x" {
		t.Fatalf("NotifyTurn: %q", got)
	}
	// mesajele turei nu au voie sa inceapa cu "-": claude le-ar lua drept flag
	for _, s := range []string{NotifyTurn(TurnContext{}, []string{"-x"}), TaskTurn(TurnContext{}, store.Task{Title: "-x"}, nil, nil)} {
		if strings.HasPrefix(s, "-") {
			t.Fatalf("mesaj care incepe cu '-': %q", s)
		}
	}
}
```

- [ ] **Step 2: Teste `team`**

In `reconcile_test.go`, actualizeaza asteptarile de text:
- `TestLeadWakesOnUserMessage`: tura incepe cu `Echipa ta directa:` si contine `Utilizatorul: Fa importul`;
- `TestRunArguments`: system prompt-ul contine `Esti Rook, Backend Dev, in proiectul ApiUnit. Raportezi lui Lead.`;
- `TestFullFlow`: `Feedback la incercarea anterioara:\nLipsesc testele`;
- `TestDirectMessageToWorker`: tura lui Rook contine `Notificari:\n- Utilizatorul: ce faci?`.

`internal/team/graph_test.go` (nou):

```go
package team

import (
	"strings"
	"testing"

	"foreman/internal/store"
)

func TestSubLeadTurnShowsDirectTeamAndSubtasks(t *testing.T) {
	f := setup(t)
	sub := f.agent(t, "Sub", &f.lead, nil)
	wes := f.agent(t, "Wes", &sub, nil)
	parent := f.task(t, "Import", sub, store.StatusQueued)
	child := store.Task{ProjectID: f.p.ID, Title: "Lexer", AssigneeID: wes.ID, CreatorID: &sub.ID, ParentTaskID: &parent.ID}
	f.st.CreateTask(&child)
	child.Status = store.StatusDone
	f.st.SaveTask(&child)

	mustReconcile(t, f)
	spec := f.fs.last(t, sub)
	got := turn(spec)
	if !strings.HasPrefix(got, "Echipa ta directa:\n- Wes") || strings.Contains(got, "Rook") {
		t.Fatalf("echipa directa gresita:\n%s", got)
	}
	if !strings.Contains(got, "Sub-task-uri deja create pentru acest task (nu le crea din nou):\n- T-2 [done] Lexer -> Wes") {
		t.Fatalf("lipsesc sub-task-urile:\n%s", got)
	}
	if !strings.Contains(argValue(spec.Args, "--append-system-prompt"), "Raportezi lui Lead.") {
		t.Fatal("sub-liderul trebuie sa raporteze lui Lead")
	}
}
```

- [ ] **Step 3: Implementare** conform Interfaces. In `reconcile.go` pastreaza deocamdata structura veche (pasii 1-3 cu `lead, workers`); schimba doar:
  - apelurile catre `prompt.*Turn`: pasul 1 foloseste `TaskTurn(t.turnContext(w, agents, tasks, task.ID), *task, subtaskLines(task.ID, tasks, byID), t.texts(p.ID, msgs))`; pasii 2 si 3 folosesc `NotifyTurn(t.turnContext(x, agents, tasks, 0), t.texts(p.ID, msgs))`;
  - apelurile `startRun(p, x, agents, taskID, turn)`;
  - stergerea lui `members`.
- [ ] **Step 4: Verificare si commit**

`go test -race ./internal/prompt/ ./internal/team/`, apoi Verificarea standard.

```bash
git add internal/prompt/prompt.go internal/prompt/prompt_test.go internal/team/run.go internal/team/reconcile.go internal/team/reconcile_test.go internal/team/graph_test.go
git commit -m "prompt: roluri compuse, echipa directa si task-uri deschise"
```

---

### Task 3: mcp + team tools (tool-uri dupa roluri, review, sub-task-uri)

MCP si backend-ul se schimba impreuna: interfata `mcp.Backend` primeste metode noi, iar `team.Team` trebuie sa le implementeze ca sa compileze.

**Files:** `internal/mcp/server.go`, `internal/mcp/tools.go`, `internal/mcp/server_test.go`, `internal/team/tools.go`, `internal/team/run.go` (Caller si `recordOutcome`), `internal/team/fake_test.go`, `internal/team/tools_test.go`, `internal/team/reconcile_test.go` (semnatura `ReportDone`)

**Interfaces:**

Consumes: `store.RolesOf`, `store.Children`, `store.IsAncestor`, `store.FindAgent`, `store.Subtasks`, `StageReviewer`/`StageLead` (Task 1).

Produces (in `mcp`):
```go
type Caller struct {
	ProjectID  int64
	AgentID    int64
	RunID      string
	TaskID     int64 // 0 = run fara task
	IsLead     bool  // are agenti direct sub el
	HasParent  bool  // are sef, deci raporteaza cu report_done
	IsReviewer bool  // verifica munca cuiva
}

type Backend interface {
	Resolve(token string) (Caller, bool)
	CreateTask(c Caller, title, description, assignee string) (string, error)
	ListTasks(c Caller, status string) (string, error)
	AcceptTask(c Caller, task, note string) (string, error)
	SendBack(c Caller, task, feedback string) (string, error)
	ReportDone(c Caller, task, summary string) (string, error)
	ReviewApprove(c Caller, task, note string) (string, error)
	ReviewReject(c Caller, task, feedback string) (string, error)
}
```
Tool-urile per rol, exact:
```go
// toolsFor intoarce reuniunea tool-urilor pentru rolurile agentului.
// Un agent radacina fara copii nu primeste niciun tool.
func toolsFor(c Caller) []Tool {
	out := []Tool{}
	if c.IsLead {
		out = append(out, leadTools...) // create_task, list_tasks, accept_task, send_back
	}
	if c.HasParent {
		out = append(out, reportTool) // report_done
	}
	if c.IsReviewer {
		out = append(out, reviewTools...) // review_approve, review_reject
	}
	return out
}
```
Schemele noi (JSON exact, prin helper-ele `schema`/`str` existente):
- `report_done`: required `["summary"]`; `summary` ramane; `task`: `str("Task-ul raportat, ex. T-3. Intr-un run pe task e implicit task-ul curent; altfel e obligatoriu.")`. Descriere: `Raporteaza sefului ca ai terminat un task (sau ca esti blocat). Task-ul trece in Review, la reviewer-ul tau daca ai unul.`
- `review_approve`: required `["task"]`; `task`: `str("Task-ul verificat, ex. T-3")`; `note`: `str("Optional: o nota pentru lider")`. Descriere: `Aproba munca unui agent pe care il verifici. Task-ul trece la liderul care l-a creat, pentru decizia finala.`
- `review_reject`: required `["task", "feedback"]`; `feedback`: `str("Ce e gresit si ce trebuie schimbat, concret")`. Descriere: `Respinge munca unui agent pe care il verifici, cu feedback concret. Task-ul revine agentului. Dupa 3 respingeri sau trimiteri inapoi, task-ul se blocheaza si decide utilizatorul.`
- In `call`: `report_done` → `ReportDone(c, arg("task"), arg("summary"))`; `review_approve` → `ReviewApprove(c, arg("task"), arg("note"))`; `review_reject` → `ReviewReject(c, arg("task"), arg("feedback"))`.

Produces (in `team`):
- `startRun` construieste `Caller` din `roles := store.RolesOf(a, agents)`: `IsLead: roles.IsLead, HasParent: !roles.IsRoot, IsReviewer: roles.IsReviewer`.
- Helper-e in `tools.go`:
  ```go
  // decider intoarce liderul care decide un task din Review: creatorul, iar
  // daca a fost sters, seful asignatului.
  func decider(task store.Task, agents []store.Agent) (store.Agent, bool) {
  	if task.CreatorID != nil {
  		if a, ok := store.FindAgent(agents, *task.CreatorID); ok {
  			return a, true
  		}
  	}
  	if w, ok := store.FindAgent(agents, task.AssigneeID); ok && w.ParentID != nil {
  		return store.FindAgent(agents, *w.ParentID)
  	}
  	return store.Agent{}, false
  }

  // canDecide: accept_task si send_back sunt permise creatorului si oricarui
  // sef de-al lui. Fara creator (sters), oricarui sef al asignatului.
  func canDecide(callerID int64, task store.Task, agents []store.Agent) bool {
  	if task.CreatorID != nil {
  		return *task.CreatorID == callerID || store.IsAncestor(callerID, *task.CreatorID, agents)
  	}
  	return store.IsAncestor(callerID, task.AssigneeID, agents)
  }
  ```
- `(*Team) toReview(task *store.Task, assignee store.Agent, agents []store.Agent, headline string) (store.Agent, error)`:
  - apelantul a pus deja `task.Result`; functia pune `Status = review`;
  - daca asignatul are reviewer: `ReviewStage = reviewer`, iar reviewer-ul primeste nota nelivrata `<headline>. Verifica-l cu review_approve sau review_reject: <result>` (cu `clip`);
  - altfel `ReviewStage = lead`, iar decidentul primeste nota nelivrata `<headline> (review): <result>` (cu `clip`);
  - fara decident: asignatul primeste in thread nota livrata `Nu are cine sa verifice <T-x>: agentul nu are sef. Leaga-l de un lider pe canvas.`;
  - salveaza, emite `task` si intoarce agentul notificat (zero daca nu exista).
- `(*Team) afterDone(task store.Task) error`, apelata dupa orice trecere in Done (accept de lider si, in Task 4, accept de utilizator):
  ```go
  // afterDone: cand ultimul sub-task al unui task ajunge in Done, asignatul
  // task-ului parinte (sub-liderul) e trezit sa raporteze in sus.
  func (t *Team) afterDone(task store.Task) error {
  	if task.ParentTaskID == nil {
  		return nil
  	}
  	parent, err := t.st.GetTask(*task.ParentTaskID)
  	if err != nil || parent.Status != store.StatusInProgress {
  		return err
  	}
  	subs, err := t.st.Subtasks(parent.ID)
  	if err != nil {
  		return err
  	}
  	for _, s := range subs {
  		if s.Status != store.StatusDone {
  			return nil
  		}
  	}
  	// daca sub-liderul lucreaza chiar acum pe parinte, finalul run-ului lui
  	// muta task-ul in Review; o notificare in plus l-ar incurca
  	t.mu.Lock()
  	ar, busy := t.active[parent.AssigneeID]
  	t.mu.Unlock()
  	if busy && ar.taskID == parent.ID {
  		return nil
  	}
  	text := fmt.Sprintf("Toate sub-task-urile pentru %s sunt gata. Verifica rezultatul si raporteaza cu report_done(task: %q, summary).", parent.Ref(), parent.Ref())
  	return t.note(parent.ProjectID, parent.AssigneeID, text, false)
  }
  ```

**Regulile tool-urilor** (fiecare revalideaza din DB: `agents, _ := t.st.ListAgents(c.ProjectID)`; flag-urile din `Caller` servesc doar la lista de tool-uri):
- **`CreateTask`**:
  - `children := store.Children(c.AgentID, agents)`; lista goala → eroare `doar un lider (un agent cu echipa directa) poate crea task-uri`;
  - asignatul, cautat cu `FindAgentByName`, trebuie sa fie in `children`. Altfel eroare `"<nume>" nu e in echipa ta directa. Echipa ta directa: A, B` (`%q`, numele in ordinea din `children`);
  - `CreatorID = &c.AgentID`;
  - `ParentTaskID`:
    - `c.TaskID` daca e nenul;
    - altfel, daca apelantul are exact un task `in_progress` asignat, acela (vezi „Ambiguitati”);
    - altfel nil.
  - Notele si raspunsul raman ca acum.
- **`ReportDone(c, ref, summary)`**:
  - apelantul fara sef in DB → eroare `raportezi direct utilizatorului: scrie rezumatul in raspuns, fara report_done`;
  - `summary` gol → `summary e obligatoriu`;
  - task-ul: `ref` gol si `c.TaskID == 0` → eroare `spune pentru ce task raportezi: report_done(task: "T-3", summary)`. `ref` gol → `GetTask(c.TaskID)`. Altfel `findTask(ref)`;
  - task-ul trebuie sa fie asignat apelantului (`<T-x> nu e asignat tie`) si `in_progress` (`<T-x> nu mai e in lucru (status <s>)`);
  - sub-task-uri nefinalizate → eroare `<T-x> mai are sub-task-uri deschise: T-5, T-6. Raporteaza dupa ce sunt gata.`;
  - `task.Result = summary`, apoi `who := t.toReview(&task, worker, agents, "<Worker> a terminat <T-x>")`. Nota in thread-ul worker-ului: `A raportat <T-x>` (livrata). `Kick`;
  - raspuns: `<T-x> e in Review. <who.Name> va verifica.`, sau `<T-x> e in Review.` daca nu exista `who`.
- **`ReviewApprove(c, ref, note)`**:
  - asignatul trebuie sa aiba `ReviewerID == c.AgentID`, altfel `nu verifici munca lui <Asignat>`;
  - task-ul trebuie sa fie in `review` cu `stage=reviewer`, altfel `<T-x> nu asteapta review (status <s>)`;
  - `ReviewStage = lead`, salveaza;
  - decidentul primeste nota nelivrata `<Rev> a aprobat <T-x> lucrat de <Asignat> (review): <result>`, plus ` Nota: <note>` daca exista (cu `clip`);
  - reviewer-ul primeste in thread nota livrata `A aprobat <T-x>`. `Kick`;
  - raspuns: `<T-x> a trecut la <Decident> pentru decizie.`
- **`ReviewReject(c, ref, feedback)`**:
  - aceleasi verificari ca la approve; feedback gol → `feedback e obligatoriu: spune concret ce trebuie schimbat`;
  - `SendBacks++`, `Feedback = feedback`, `ReviewStage = ""`;
  - la `SendBacks >= MaxSendBacks`:
    - `blocked`;
    - note livrate in thread-ul reviewer-ului si al decidentului: `<T-x> e blocat dupa 3 respingeri sau trimiteri inapoi. Decide utilizatorul.`;
    - raspunsul e acelasi text ca la `send_back`.
  - altfel:
    - `queued`;
    - nota livrata la asignat: `<T-x> respins la review de <Rev>: <feedback>`;
    - nota livrata la reviewer: `A respins <T-x>: <feedback>`;
    - `Kick`;
    - raspuns: `<T-x> trimis inapoi lui <Asignat> (n/3).`
- **`AcceptTask`**:
  - `canDecide` fals → eroare `doar liderul care a creat <T-x> (sau un sef de-al lui) decide`;
  - `review` cu `stage=reviewer` → eroare `<T-x> e inca la review la <Reviewer>`;
  - se accepta doar `review` cu `stage=lead`, sau `blocked` (mesajul existent pentru alte statusuri);
  - dupa `Done`, `ReviewStage = ""`, nota ca acum, apoi `t.afterDone(task)` si `t.Kick(c.ProjectID)`.
- **`SendBack`**:
  - aceeasi verificare `canDecide`;
  - doar `review` cu `stage=lead`. Cu `stage=reviewer` → aceeasi eroare ca la accept;
  - `ReviewStage = ""`; restul ramane ca acum.
- `ListTasks` nu se schimba. `workerNames` se sterge.
- `recordOutcome` (in `run.go`), pentru task-urile inca `in_progress` la final:
  - `RunStopped` → `queued`, ca acum;
  - `RunFailed` → `review` cu `stage=lead`, `Result = "run esuat: ..."`; nota nelivrata catre `decider` (textul existent `Run-ul lui ... a esuat: ...`);
  - altfel → `task.Result = strings.TrimSpace(o.Result)` si `t.toReview(&task, a, agents, "<Nume> a terminat <T-x> fara report_done")`.

  `st.Lead` nu mai e folosit in `tools.go` si nici in `recordOutcome`.

- [ ] **Step 1: Teste mcp (`server_test.go`)**

- `fakeBackend`: `ReportDone(c, task, summary)` inregistreaza `"report_done|"+task+"|"+summary`; `ReviewApprove` → `"review_approve|"+task+"|"+note`; `ReviewReject` → `"review_reject|"+task+"|"+feedback`.
- `setup`, callers:
  ```go
  "lead":     {ProjectID: 1, AgentID: 1, IsLead: true},
  "worker":   {ProjectID: 1, AgentID: 2, TaskID: 5, HasParent: true},
  "sublead":  {ProjectID: 1, AgentID: 3, IsLead: true, HasParent: true},
  "reviewer": {ProjectID: 1, AgentID: 4, HasParent: true, IsReviewer: true},
  "solo":     {ProjectID: 1, AgentID: 5},
  ```
- `TestToolsListByRole` asteapta exact:
  - `lead`: `create_task,list_tasks,accept_task,send_back`
  - `worker`: `report_done`
  - `sublead`: `create_task,list_tasks,accept_task,send_back,report_done`
  - `reviewer`: `report_done,review_approve,review_reject`
  - `solo`: sir gol, iar `result.tools` e un array JSON gol (`[]`), nu `null`.
- `TestCallReportDoneWithTask`: `{"name":"report_done","arguments":{"task":"T-4","summary":"gata"}}` pe `sublead` → apelul `report_done|T-4|gata`.
- `TestCallReviewTools`: pe `reviewer`, `review_approve` cu `{"task":3,"note":"ok"}` → `review_approve|3|ok`; `review_reject` cu `{"task":"T-3","feedback":"lipsesc teste"}` → `review_reject|T-3|lipsesc teste`.
- `TestWorkerCannotReview`: `worker` apeleaza `review_approve` → `isError` si `nu e disponibil`, iar backend-ul nu e apelat.

- [ ] **Step 2: Teste team**

`fake_test.go`:
- `leadCaller()` → `mcp.Caller{ProjectID, AgentID: lead, IsLead: true}`;
- `workerCaller(a, taskID)` → `{..., TaskID: taskID, HasParent: true}`;
- `f.task` pune `CreatorID: &f.lead.ID` la creare si, daca `status == store.StatusReview`, `ReviewStage = store.StageLead` inainte de `SaveTask`.

Teste existente actualizate in `tools_test.go`: toate apelurile devin `ReportDone(c, "", summary)`. In `TestReportDoneErrors`, cazul „lead” asteapta eroarea `raportezi direct utilizatorului`. In `reconcile_test.go`, `TestFullFlow` foloseste `ReportDone(f.caller(t, rook1), "", "A e gata")`. Textele asteptate raman aceleasi (`T-1 e in Review. Lead va verifica.`, `Rook a terminat T-1 (review): A e gata`, `Rook a terminat T-3 fara report_done (review): C e gata`).

Teste noi in `tools_test.go`:

```go
func TestCreateTaskDirectReportsOnly(t *testing.T) {
	f := setup(t)
	sub := f.agent(t, "Sub", &f.lead, nil)
	wes := f.agent(t, "Wes", &sub, nil)
	_, err := f.tm.CreateTask(f.leadCaller(), "A", "", "Wes")
	if err == nil || !strings.Contains(err.Error(), `"Wes" nu e in echipa ta directa. Echipa ta directa: Rook, Vera, Sub`) {
		t.Fatalf("worker indirect: %v", err)
	}
	// flag vechi: Rook a fost lider cand a pornit run-ul, dar acum nu mai are copii
	stale := mcp.Caller{ProjectID: f.p.ID, AgentID: f.rook.ID, IsLead: true, HasParent: true}
	if _, err := f.tm.CreateTask(stale, "A", "", "Vera"); err == nil || !strings.Contains(err.Error(), "doar un lider") {
		t.Fatalf("lider fara copii: %v", err)
	}
	parent := f.task(t, "Import", sub, store.StatusInProgress)
	sc := mcp.Caller{ProjectID: f.p.ID, AgentID: sub.ID, IsLead: true, HasParent: true, TaskID: parent.ID}
	if _, err := f.tm.CreateTask(sc, "Parser", "", "wes"); err != nil {
		t.Fatal(err)
	}
	got := f.wantStatus(t, 2, store.StatusQueued)
	if got.AssigneeID != wes.ID || *got.CreatorID != sub.ID || got.ParentTaskID == nil || *got.ParentTaskID != parent.ID {
		t.Fatalf("sub-task: %+v", got)
	}
	// run de notificari (fara task), dar cu un singur task in lucru: tot sub-task al lui
	sc.TaskID = 0
	f.tm.CreateTask(sc, "Lexer", "", "Wes")
	if got := f.wantStatus(t, 3, store.StatusQueued); got.ParentTaskID == nil || *got.ParentTaskID != parent.ID {
		t.Fatalf("T-3 parinte: %+v", got.ParentTaskID)
	}
}

func TestReportDoneTaskParam(t *testing.T) {
	f := setup(t)
	sub := f.agent(t, "Sub", &f.lead, nil)
	wes := f.agent(t, "Wes", &sub, nil)
	parent := f.task(t, "Import", sub, store.StatusInProgress)
	child := store.Task{ProjectID: f.p.ID, Title: "Parser", AssigneeID: wes.ID, CreatorID: &sub.ID, ParentTaskID: &parent.ID}
	f.st.CreateTask(&child)
	sc := mcp.Caller{ProjectID: f.p.ID, AgentID: sub.ID, IsLead: true, HasParent: true} // run de notificari
	errs := map[string]string{"": "spune pentru ce task", "T-1": "mai are sub-task-uri deschise: T-2", "T-2": "nu e asignat tie"}
	for ref, want := range errs {
		if _, err := f.tm.ReportDone(sc, ref, "x"); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("ref %q: %v, vreau %q", ref, err, want)
		}
	}
	child.Status = store.StatusDone
	f.st.SaveTask(&child)
	text, err := f.tm.ReportDone(sc, "t-1", "Import gata")
	if err != nil || text != "T-1 e in Review. Lead va verifica." {
		t.Fatalf("%q %v", text, err)
	}
	if got := f.wantStatus(t, 1, store.StatusReview); got.ReviewStage != store.StageLead || got.Result != "Import gata" {
		t.Fatalf("T-1: %+v", got)
	}
}

func TestReviewApproveAndReject(t *testing.T) {
	f := setup(t)
	f.rook.ReviewerID = &f.vera.ID
	if err := f.st.UpdateAgent(f.rook); err != nil {
		t.Fatal(err)
	}
	task := f.task(t, "A", f.rook, store.StatusInProgress)
	text, err := f.tm.ReportDone(f.workerCaller(f.rook, task.ID), "", "A e gata")
	if err != nil || text != "T-1 e in Review. Vera va verifica." {
		t.Fatalf("%q %v", text, err)
	}
	if got := f.wantStatus(t, 1, store.StatusReview); got.ReviewStage != store.StageReviewer {
		t.Fatalf("stage = %q", got.ReviewStage)
	}
	pending, _ := f.st.Undelivered(f.vera.ID)
	if len(pending) != 1 || pending[0].Text != "Rook a terminat T-1. Verifica-l cu review_approve sau review_reject: A e gata" {
		t.Fatalf("notificari Vera: %+v", pending)
	}
	if p, _ := f.st.Undelivered(f.lead.ID); len(p) != 0 {
		t.Fatal("lead-ul nu trebuie notificat inainte de review")
	}
	if _, err := f.tm.AcceptTask(f.leadCaller(), "T-1", ""); err == nil || !strings.Contains(err.Error(), "inca la review la Vera") {
		t.Fatalf("accept inainte de review: %v", err)
	}
	vc := mcp.Caller{ProjectID: f.p.ID, AgentID: f.vera.ID, HasParent: true, IsReviewer: true}
	other := mcp.Caller{ProjectID: f.p.ID, AgentID: f.lead.ID, IsLead: true, IsReviewer: true}
	if _, err := f.tm.ReviewApprove(other, "T-1", ""); err == nil || !strings.Contains(err.Error(), "nu verifici munca lui Rook") {
		t.Fatalf("alt reviewer: %v", err)
	}
	if _, err := f.tm.ReviewReject(vc, "T-1", "  "); err == nil {
		t.Fatal("feedback gol trebuia refuzat")
	}

	text, err = f.tm.ReviewReject(vc, "T-1", "Lipsesc testele")
	if err != nil || text != "T-1 trimis inapoi lui Rook (1/3)." {
		t.Fatalf("reject: %q %v", text, err)
	}
	got := f.wantStatus(t, 1, store.StatusQueued)
	if got.Feedback != "Lipsesc testele" || got.SendBacks != 1 || got.ReviewStage != store.StageNone {
		t.Fatalf("dupa reject: %+v", got)
	}
	if !f.hasMessage(f.rook.ID, "T-1 respins la review de Vera: Lipsesc testele") {
		t.Fatal("Rook trebuie sa vada respingerea")
	}

	got.Status, got.ReviewStage = store.StatusReview, store.StageReviewer
	f.st.SaveTask(&got)
	text, err = f.tm.ReviewApprove(vc, "T-1", "arata bine")
	if err != nil || text != "T-1 a trecut la Lead pentru decizie." {
		t.Fatalf("approve: %q %v", text, err)
	}
	if got := f.wantStatus(t, 1, store.StatusReview); got.ReviewStage != store.StageLead {
		t.Fatalf("stage dupa approve = %q", got.ReviewStage)
	}
	pending, _ = f.st.Undelivered(f.lead.ID)
	last := pending[len(pending)-1].Text
	if !strings.HasPrefix(last, "Vera a aprobat T-1 lucrat de Rook (review): A e gata") || !strings.HasSuffix(last, "Nota: arata bine") {
		t.Fatalf("notificare lead: %q", last)
	}
	if _, err := f.tm.ReviewApprove(vc, "T-1", ""); err == nil {
		t.Fatal("a doua aprobare trebuia refuzata")
	}
}

func TestReviewRejectBlocksAtThree(t *testing.T) {
	f := setup(t)
	f.rook.ReviewerID = &f.vera.ID
	f.st.UpdateAgent(f.rook)
	task := f.task(t, "A", f.rook, store.StatusReview)
	vc := mcp.Caller{ProjectID: f.p.ID, AgentID: f.vera.ID, HasParent: true, IsReviewer: true}
	for i := 1; i <= 3; i++ {
		task, _ = f.st.GetTask(task.ID)
		task.Status, task.ReviewStage = store.StatusReview, store.StageReviewer
		f.st.SaveTask(&task)
		if _, err := f.tm.ReviewReject(vc, "T-1", "tot nu"); err != nil {
			t.Fatal(err)
		}
	}
	f.wantStatus(t, 1, store.StatusBlocked)
	if !f.hasMessage(f.lead.ID, "T-1 e blocat dupa 3 respingeri sau trimiteri inapoi") {
		t.Fatal("decidentul trebuie anuntat de blocare")
	}
}

func TestAcceptOnlyByCreatorOrAncestor(t *testing.T) {
	f := setup(t)
	sub := f.agent(t, "Sub", &f.lead, nil)
	wes := f.agent(t, "Wes", &sub, nil)
	task := store.Task{ProjectID: f.p.ID, Title: "A", AssigneeID: wes.ID, CreatorID: &sub.ID}
	f.st.CreateTask(&task)
	task.Status, task.ReviewStage = store.StatusReview, store.StageLead
	f.st.SaveTask(&task)
	stranger := mcp.Caller{ProjectID: f.p.ID, AgentID: f.rook.ID, IsLead: true, HasParent: true}
	if _, err := f.tm.AcceptTask(stranger, "T-1", ""); err == nil || !strings.Contains(err.Error(), "doar liderul care a creat T-1") {
		t.Fatalf("accept strain: %v", err)
	}
	if _, err := f.tm.SendBack(stranger, "T-1", "x"); err == nil {
		t.Fatal("send_back strain trebuia refuzat")
	}
	if _, err := f.tm.AcceptTask(f.leadCaller(), "T-1", ""); err != nil {
		t.Fatalf("stramosul creatorului: %v", err)
	}
}

func TestAcceptWithoutCreatorByAncestor(t *testing.T) {
	f := setup(t)
	sub := f.agent(t, "Sub", &f.lead, nil)
	wes := f.agent(t, "Wes", &sub, nil)
	task := store.Task{ProjectID: f.p.ID, Title: "A", AssigneeID: wes.ID, CreatorID: &sub.ID}
	f.st.CreateTask(&task)
	task.Status, task.ReviewStage = store.StatusReview, store.StageLead
	f.st.SaveTask(&task)
	if err := f.st.DeleteAgent(sub.ID); err != nil { // Wes trece la Lead, creator_id devine NULL
		t.Fatal(err)
	}
	stranger := mcp.Caller{ProjectID: f.p.ID, AgentID: f.rook.ID, IsLead: true, HasParent: true}
	if _, err := f.tm.AcceptTask(stranger, "T-1", ""); err == nil {
		t.Fatal("Rook nu e sef al lui Wes")
	}
	if _, err := f.tm.AcceptTask(f.leadCaller(), "T-1", ""); err != nil {
		t.Fatalf("seful asignatului: %v", err)
	}
}

func TestSubtaskAcceptNotifiesParentAssignee(t *testing.T) {
	f := setup(t)
	sub := f.agent(t, "Sub", &f.lead, nil)
	wes := f.agent(t, "Wes", &sub, nil)
	parent := f.task(t, "Import", sub, store.StatusInProgress)
	for _, title := range []string{"A", "B"} {
		c := store.Task{ProjectID: f.p.ID, Title: title, AssigneeID: wes.ID, CreatorID: &sub.ID, ParentTaskID: &parent.ID}
		f.st.CreateTask(&c)
		c.Status, c.ReviewStage = store.StatusReview, store.StageLead
		f.st.SaveTask(&c)
	}
	sc := mcp.Caller{ProjectID: f.p.ID, AgentID: sub.ID, IsLead: true, HasParent: true}
	f.tm.AcceptTask(sc, "T-2", "")
	if f.hasMessage(sub.ID, "Toate sub-task-urile") {
		t.Fatal("T-3 e inca deschis: prea devreme")
	}
	f.tm.AcceptTask(sc, "T-3", "")
	pending, _ := f.st.Undelivered(sub.ID)
	if len(pending) != 1 || !strings.HasPrefix(pending[0].Text, "Toate sub-task-urile pentru T-1 sunt gata.") {
		t.Fatalf("notificare sub-lider: %+v", pending)
	}
}
```
(`tools_test.go` importa acum si `foreman/internal/mcp`.)

- [ ] **Step 3: Implementare** conform Interfaces si reguli.
- [ ] **Step 4: Verificare si commit**

`go test -race ./internal/mcp/ ./internal/team/`, apoi Verificarea standard.

```bash
git add internal/mcp/server.go internal/mcp/tools.go internal/mcp/server_test.go internal/team/tools.go internal/team/run.go internal/team/fake_test.go internal/team/tools_test.go internal/team/reconcile_test.go
git commit -m "mcp + team: tool-uri dupa roluri, review si sub-task-uri"
```

---

### Task 4: team scheduler (Reconcile generalizat, sub-task-uri la final de run, radacini fara limita)

**Files:** `internal/team/reconcile.go`, `internal/team/run.go` (`recordOutcome`), `internal/team/graph_test.go`, `internal/team/reconcile_test.go`

**Interfaces:**

Consumes: `turnContext`, `subtaskLines`, `texts` (Task 2); `decider`, `toReview`, `afterDone` (Task 3); `store.Subtasks`, `Requeue*` (Task 1).

Produces:
- `Reconcile(projectID int64) error` generalizat (codul de mai jos).
- `recordOutcome` pastreaza in lucru task-urile cu sub-task-uri deschise.
- `UserMessage` trimite copia catre **seful** agentului (nu catre lead); o radacina nu produce copie.
- `resolveBlocked` anunta `decider(task)` (nu lead-ul) si, la accept, apeleaza `t.afterDone(task)`.
- `wakeLeadForDecisions` devine `wakeForDecisions(projectID int64) error`: anunta fiecare agent care are ceva de decis.
- `startFailed(p store.Project, notify, a store.Agent, cause error) error` pastreaza semnatura; `notify` = prima radacina (`agents[0]`).
- Dupa acest task, `st.Lead` si `store.PositionLead`/`PositionWorker` nu mai sunt folosite in `internal/team`.

- [ ] **Step 1: Teste (`graph_test.go`)**

```go
func TestGraphFullFlow(t *testing.T) {
	f := setup(t)
	sub := f.agent(t, "Sub", &f.lead, nil)
	rev := f.agent(t, "Rev", &f.lead, nil)
	wes := f.agent(t, "Wes", &sub, &rev)
	ok := func(sid string) runner.Outcome { return runner.Outcome{Status: "done", SessionID: sid, Result: "ok"} }

	// 1. utilizator -> coordonator -> T-1 pentru sub-lider
	f.tm.UserMessage(f.lead.ID, "Fa importul", nil)
	mustReconcile(t, f)
	lead1 := f.fs.last(t, f.lead)
	if _, err := f.tm.CreateTask(f.caller(t, lead1), "Import", "", "Sub"); err != nil {
		t.Fatal(err)
	}
	f.fs.finish(lead1.ID, ok("s-lead"))

	// 2. sub-liderul sparge T-1 in T-2 pentru Wes si se opreste fara report_done
	mustReconcile(t, f)
	sub1 := f.fs.last(t, sub)
	sc := f.caller(t, sub1)
	if !sc.IsLead || !sc.HasParent || sc.IsReviewer || !strings.Contains(turn(sub1), "Task T-1: Import") {
		t.Fatalf("sub1: %+v\n%s", sc, turn(sub1))
	}
	if _, err := f.tm.CreateTask(sc, "Parser", "", "Wes"); err != nil {
		t.Fatal(err)
	}
	f.fs.finish(sub1.ID, runner.Outcome{Status: "done", SessionID: "s-sub", Result: "Am delegat."})
	f.wantStatus(t, 1, store.StatusInProgress) // asteapta T-2, nu trece in review
	if !f.hasMessage(sub.ID, "T-1 asteapta sub-task-urile: T-2.") {
		t.Fatal("lipseste nota de asteptare")
	}

	// 3. Wes raporteaza: T-2 merge la Rev
	mustReconcile(t, f)
	wes1 := f.fs.last(t, wes)
	if _, err := f.tm.ReportDone(f.caller(t, wes1), "", "Parser gata"); err != nil {
		t.Fatal(err)
	}
	f.fs.finish(wes1.ID, ok("s-wes"))

	// 4. Rev e trezit cu run de notificari si aproba
	mustReconcile(t, f)
	rev1 := f.fs.last(t, rev)
	rc := f.caller(t, rev1)
	if !rc.IsReviewer || rc.IsLead || !strings.Contains(turn(rev1), "Revizuiesti munca lui: Wes") ||
		!strings.Contains(turn(rev1), "Wes a terminat T-2. Verifica-l") {
		t.Fatalf("rev1: %+v\n%s", rc, turn(rev1))
	}
	if _, err := f.tm.ReviewApprove(rc, "T-2", ""); err != nil {
		t.Fatal(err)
	}
	f.fs.finish(rev1.ID, ok("s-rev"))

	// 5. Sub accepta T-2 si primeste "toate gata"
	mustReconcile(t, f)
	sub2 := f.fs.last(t, sub)
	if sub2.ID == sub1.ID || !strings.Contains(turn(sub2), "Rev a aprobat T-2 lucrat de Wes") {
		t.Fatalf("sub2:\n%s", turn(sub2))
	}
	if _, err := f.tm.AcceptTask(f.caller(t, sub2), "T-2", ""); err != nil {
		t.Fatal(err)
	}
	f.fs.finish(sub2.ID, ok("s-sub"))
	mustReconcile(t, f)
	sub3 := f.fs.last(t, sub)
	got := turn(sub3)
	if !strings.Contains(got, "Toate sub-task-urile pentru T-1 sunt gata.") ||
		!strings.Contains(got, "Task-urile tale deschise:\n- T-1: Import (sub-task-uri gata: 1 din 1)") {
		t.Fatalf("sub3:\n%s", got)
	}
	if _, err := f.tm.ReportDone(f.caller(t, sub3), "", "x"); err == nil {
		t.Fatal("in run de notificari, task-ul e obligatoriu")
	}
	if _, err := f.tm.ReportDone(f.caller(t, sub3), "T-1", "Import gata"); err != nil {
		t.Fatal(err)
	}
	f.fs.finish(sub3.ID, ok("s-sub"))

	// 6. coordonatorul accepta T-1
	mustReconcile(t, f)
	lead2 := f.fs.last(t, f.lead)
	if !strings.Contains(turn(lead2), "Sub a terminat T-1 (review): Import gata") {
		t.Fatalf("lead2:\n%s", turn(lead2))
	}
	if _, err := f.tm.AcceptTask(f.caller(t, lead2), "T-1", ""); err != nil {
		t.Fatal(err)
	}
	f.wantStatus(t, 1, store.StatusDone)
	f.wantStatus(t, 2, store.StatusDone)
}

func TestCreateTaskRefusedOutsideDirectTeamInRun(t *testing.T) {
	f := setup(t)
	sub := f.agent(t, "Sub", &f.lead, nil)
	f.agent(t, "Wes", &sub, nil)
	f.tm.UserMessage(f.lead.ID, "Fa importul", nil)
	mustReconcile(t, f)
	if _, err := f.tm.CreateTask(f.caller(t, f.fs.last(t, f.lead)), "A", "", "Wes"); err == nil {
		t.Fatal("coordonatorul nu poate da task direct lui Wes")
	}
}

func TestStopKeepsWaitingParentTask(t *testing.T) {
	f := setup(t)
	sub := f.agent(t, "Sub", &f.lead, nil)
	wes := f.agent(t, "Wes", &sub, nil)
	parent := f.task(t, "Import", sub, store.StatusInProgress)
	child := store.Task{ProjectID: f.p.ID, Title: "Parser", AssigneeID: wes.ID, CreatorID: &sub.ID, ParentTaskID: &parent.ID}
	f.st.CreateTask(&child)
	mustReconcile(t, f) // Wes porneste pe T-2
	if err := f.tm.Stop(f.p.ID); err != nil {
		t.Fatal(err)
	}
	f.wantStatus(t, 1, store.StatusInProgress)
	f.wantStatus(t, 2, store.StatusQueued)
	f.fs.finish(f.fs.last(t, wes).ID, runner.Outcome{Status: "stopped"})
	f.tm.Resume(f.p.ID)
	mustReconcile(t, f)
	for _, s := range f.fs.specs {
		if s.Key == fmt.Sprintf("agent-%d", sub.ID) {
			t.Fatal("sub-liderul nu trebuie repornit pe T-1 cat timp T-2 e deschis")
		}
	}
	if err := f.tm.Recover(); err != nil {
		t.Fatal(err)
	}
	f.wantStatus(t, 1, store.StatusInProgress)
}

func TestRootsIgnoreMaxParallel(t *testing.T) {
	f := setup(t)
	f.p.MaxParallel = 1
	f.st.UpdateProject(f.p)
	solo := f.agent(t, "Solo", nil, nil)
	f.tm.UserMessage(f.lead.ID, "salut", nil)
	f.tm.UserMessage(solo.ID, "salut", nil)
	f.task(t, "A", f.rook, store.StatusQueued)
	f.task(t, "B", f.vera, store.StatusQueued)
	mustReconcile(t, f)
	if f.fs.count() != 3 {
		t.Fatalf("run-uri: %d, vreau 3 (doua radacini + un worker)", f.fs.count())
	}
	f.wantStatus(t, 2, store.StatusQueued)
	c := f.caller(t, f.fs.last(t, solo))
	if c.IsLead || c.HasParent || c.IsReviewer {
		t.Fatalf("radacina singura: %+v", c)
	}
	if got := turn(f.fs.last(t, solo)); got != "Notificari:\n- Utilizatorul: salut" {
		t.Fatalf("tura Solo: %q", got)
	}
}

func TestUserMessageCopyGoesToParent(t *testing.T) {
	f := setup(t)
	sub := f.agent(t, "Sub", &f.lead, nil)
	wes := f.agent(t, "Wes", &sub, nil)
	f.tm.UserMessage(wes.ID, "ce faci?", nil)
	if !f.hasMessage(sub.ID, "Utilizatorul -> Wes: ce faci?") || f.hasMessage(f.lead.ID, "Utilizatorul -> Wes") {
		t.Fatal("copia trebuie sa ajunga doar la seful direct")
	}
	f.tm.UserMessage(f.lead.ID, "salut", nil)
	if msgs, _ := f.st.ListMessages(f.lead.ID); len(msgs) != 1 {
		t.Fatalf("o radacina nu primeste copie: %+v", msgs)
	}
}

func TestAcceptBlockedSubtaskNotifiesParent(t *testing.T) {
	f := setup(t)
	sub := f.agent(t, "Sub", &f.lead, nil)
	wes := f.agent(t, "Wes", &sub, nil)
	parent := f.task(t, "Import", sub, store.StatusInProgress)
	child := store.Task{ProjectID: f.p.ID, Title: "Parser", AssigneeID: wes.ID, CreatorID: &sub.ID, ParentTaskID: &parent.ID}
	f.st.CreateTask(&child)
	child.Status = store.StatusBlocked
	f.st.SaveTask(&child)
	if err := f.tm.AcceptBlocked(child.ID); err != nil {
		t.Fatal(err)
	}
	if !f.hasMessage(sub.ID, "Utilizatorul a acceptat T-2.") || !f.hasMessage(sub.ID, "Toate sub-task-urile pentru T-1 sunt gata.") {
		t.Fatal("sub-liderul (creator si asignatul parintelui) trebuie anuntat")
	}
}

func TestResumeWakesReviewer(t *testing.T) {
	f := setup(t)
	f.rook.ReviewerID = &f.vera.ID
	f.st.UpdateAgent(f.rook)
	task := f.task(t, "A", f.rook, store.StatusReview)
	task.ReviewStage = store.StageReviewer
	f.st.SaveTask(&task)
	f.tm.Stop(f.p.ID)
	f.tm.Resume(f.p.ID)
	pending, _ := f.st.Undelivered(f.vera.ID)
	if len(pending) != 1 || pending[0].Text != "Echipa a fost repornita. Asteapta decizia ta: T-1." {
		t.Fatalf("Vera: %+v", pending)
	}
	if p, _ := f.st.Undelivered(f.lead.ID); len(p) != 0 {
		t.Fatalf("lead-ul nu are nimic de decis: %+v", p)
	}
}
```
(`graph_test.go` importa `fmt`, `strings`, `testing`, `runner`, `store`.)

Testele existente raman valabile (`TestMaxParallel`, `TestUnlimitedParallel`, `TestResumeWakesLeadForReview`, `TestMissingWorkdirPausesProject`, `TestDirectMessageToWorker`, `TestPostMessageToWorker` din web). Daca unul pica, repara codul, nu testul.

- [ ] **Step 2: Implementare**

`Reconcile`, exact:
```go
// Reconcile porneste run-urile care trebuie pornite in proiect:
//  1. orice agent inactiv cu un task in Queued primeste un run pe task,
//     in ordinea numarului;
//  2. altfel, un agent inactiv cu mesaje nelivrate primeste un run de notificari.
// max_parallel se aplica doar agentilor care au sef; radacinile vorbesc cu
// utilizatorul si pornesc mereu. E sigur de apelat oricand si de oricate ori.
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
	if err != nil || len(agents) == 0 {
		return err
	}
	tasks, err := t.st.ListTasks(projectID)
	if err != nil {
		return err
	}
	byID := map[int64]store.Agent{}
	for _, a := range agents {
		byID[a.ID] = a
	}
	running := 0
	for _, a := range agents {
		if a.ParentID != nil && t.isActive(a.ID) {
			running++
		}
	}
	full := func(a store.Agent) bool {
		return a.ParentID != nil && p.MaxParallel > 0 && running >= p.MaxParallel
	}
	started := func(a store.Agent) {
		if a.ParentID != nil {
			running++
		}
	}
	notify := agents[0] // prima radacina: aici apar erorile de pornire

	// 1. task-urile din Queued
	hasQueued := map[int64]bool{}
	for i := range tasks {
		task := &tasks[i]
		if task.Status != store.StatusQueued {
			continue
		}
		hasQueued[task.AssigneeID] = true
		a, ok := byID[task.AssigneeID]
		if !ok || t.isActive(a.ID) || full(a) {
			continue
		}
		msgs, err := t.st.Undelivered(a.ID)
		if err != nil {
			return err
		}
		task.Status = store.StatusInProgress
		if err := t.st.SaveTask(task); err != nil {
			return err
		}
		turn := prompt.TaskTurn(t.turnContext(a, agents, tasks, task.ID), *task,
			subtaskLines(task.ID, tasks, byID), t.texts(p.ID, msgs))
		if err := t.startRun(p, a, agents, task.ID, turn); err != nil {
			task.Status = store.StatusQueued
			if err := t.st.SaveTask(task); err != nil {
				return err
			}
			return t.startFailed(p, notify, a, err)
		}
		if err := t.delivered(p.ID, msgs); err != nil {
			return err
		}
		t.emit(p.ID, "task", *task)
		started(a)
	}

	// 2. run-uri de notificari
	for _, a := range agents {
		if t.isActive(a.ID) || hasQueued[a.ID] || full(a) {
			continue
		}
		notes, err := t.st.Undelivered(a.ID)
		if err != nil {
			return err
		}
		if len(notes) == 0 {
			continue
		}
		turn := prompt.NotifyTurn(t.turnContext(a, agents, tasks, 0), t.texts(p.ID, notes))
		if err := t.startRun(p, a, agents, 0, turn); err != nil {
			return t.startFailed(p, notify, a, err)
		}
		if err := t.delivered(p.ID, notes); err != nil {
			return err
		}
		started(a)
	}
	return nil
}
```

`recordOutcome`: dupa ce s-a verificat ca task-ul e inca `in_progress`, inainte de `switch`:
```go
	subs, err := t.st.Subtasks(task.ID)
	if err != nil {
		return err
	}
	var open []string
	for _, s := range subs {
		if s.Status != store.StatusDone {
			open = append(open, s.Ref())
		}
	}
	// Un sub-lider care a spart task-ul se opreste fara report_done. Task-ul
	// ramane in lucru (oricum s-ar fi terminat run-ul) pana cand toate
	// sub-task-urile ajung in Done; atunci afterDone il trezeste.
	if len(open) > 0 {
		if o.Status == store.RunStopped {
			return nil
		}
		return t.note(ar.projectID, a.ID, fmt.Sprintf("%s asteapta sub-task-urile: %s.", task.Ref(), strings.Join(open, ", ")), true)
	}
```

`wakeForDecisions`, exact:
```go
// wakeForDecisions: la Resume, notificarile livrate unui run oprit s-au
// pierdut. Fiecare agent care are ceva de decis primeste o nota noua:
// reviewer-ul pentru task-urile la review, decidentul pentru cele la lider
// sau blocate, sub-liderul pentru un task ale carui sub-task-uri sunt gata.
func (t *Team) wakeForDecisions(projectID int64) error {
	agents, err := t.st.ListAgents(projectID)
	if err != nil {
		return err
	}
	tasks, err := t.st.ListTasks(projectID)
	if err != nil {
		return err
	}
	waiting := map[int64][]string{}
	for _, task := range tasks {
		var who int64
		switch {
		case task.Status == store.StatusReview && task.ReviewStage == store.StageReviewer:
			if w, ok := store.FindAgent(agents, task.AssigneeID); ok && w.ReviewerID != nil {
				who = *w.ReviewerID
			}
		case task.Status == store.StatusReview || task.Status == store.StatusBlocked:
			if d, ok := decider(task, agents); ok {
				who = d.ID
			}
		case task.Status == store.StatusInProgress && allSubtasksDone(task.ID, tasks):
			who = task.AssigneeID
		}
		if who != 0 {
			waiting[who] = append(waiting[who], task.Ref())
		}
	}
	for _, a := range agents {
		refs := waiting[a.ID]
		if len(refs) == 0 || t.isActive(a.ID) {
			continue
		}
		pending, err := t.st.Undelivered(a.ID)
		if err != nil {
			return err
		}
		if len(pending) == 0 {
			t.note(projectID, a.ID, "Echipa a fost repornita. Asteapta decizia ta: "+strings.Join(refs, ", ")+".", false)
		}
	}
	return nil
}
```
`allSubtasksDone(taskID, tasks) bool` intoarce true doar daca exista cel putin un sub-task si toate sunt `done`.

`UserMessage`: daca `a.ParentID != nil`, nota `Utilizatorul -> <Nume>: <text>` (cu atasamente) merge la `*a.ParentID`. `resolveBlocked`: nota merge la `decider(task, agents)`, daca exista; la accept, `ReviewStage = ""`, apoi `t.afterDone(task)` si `Kick`.

- [ ] **Step 3: Verificare si commit**

`go test -race -count=3 ./internal/team/` (de 3 ori, ca sa prinda curse), apoi Verificarea standard. `grep -nE "st\.Lead\(|Position(Lead|Worker)|\.Position\b" internal/team/*.go | grep -v _test.go` nu trebuie sa afiseze nimic.

```bash
git add internal/team/reconcile.go internal/team/run.go internal/team/graph_test.go internal/team/reconcile_test.go
git commit -m "team: scheduler generalizat pentru ierarhie"
```

---

### Task 5: web API (campuri, validare, pozitie, prompt-preview) + curatarea `position`

**Files:** `internal/web/api_agents.go`, `internal/web/api_projects.go`, `internal/web/server.go`, `internal/web/server_test.go`, `internal/web/graph_test.go` (nou), `internal/store/agents.go`, `internal/store/projects.go`, `internal/store/store_test.go` (stergerea referintelor la `Position`/`Lead`)

**Interfaces:**

Consumes: `store.LinkError`, `store.RolesOf`, `SetAgentPosition`, `prompt.LinksFor`.

Produces (forma API-ului, exact ca in spec):
- `agentInput` primeste:
  ```go
  Responsibilities string `json:"responsibilities"`
  ParentID         *int64 `json:"parentId"`   // null sau lipsa = radacina
  ReviewerID       *int64 `json:"reviewerId"` // null sau lipsa = fara reviewer
  ```
  `apply` le copiaza in agent si valideaza `maxLen("responsabilitatile", ..., 2000)`. `PUT /api/agents/{id}` inlocuieste toate campurile editabile, deci si legaturile. UI-ul trimite mereu corpul complet (`agentBody`).
- `fail`: `errors.As(err, &le)` pentru `store.LinkError` → 400 cu `le.Error()`.
- `GET /api/projects/{id}`:
  - fiecare agent: campurile din `store.Agent` (`parentId`, `reviewerId`, `responsibilities`, `posX`, `posY`), plus `isLead`, `isRoot`, `isReviewer` (prin `store.Roles` incorporat in `agentView`), `status`, `activeRunId`, `currentTask`;
  - fiecare task: `creatorId`, `parentTaskId`, `reviewStage`;
  - `position` nu mai apare nicaieri.
- `memberBrief` din `GET /api/projects` devine `{id, name, isLead, isRoot}`.
- `PUT /api/agents/{id}/position` cu body `{"x": number, "y": number}`:
  - valori in afara `[-100000, 100000]` → 400 `pozitie invalida`;
  - agent inexistent → 404;
  - succes → 204, fara eveniment SSE.
- `POST /api/prompt-preview` cu body `{projectId, agentId, name, role, background, instructions, doneDef, parentId}`:
  - `agentId` 0 inseamna agent nou;
  - campul `position` se ignora (nu mai exista);
  - un `parentId` din alt proiect → 400 `seful ales nu e in acest proiect`;
  - se construieste `a := store.Agent{ID: agentId, ProjectID, Name, Role, ..., ParentID: parentId}` si se apeleaza `prompt.System(p, a, prompt.LinksFor(a, agents))`. Asa, „Raporteaza lui” din editor se vede imediat, iar lider/reviewer vin din legaturile salvate.
- `createAgent` nu mai seteaza `Position`. `deleteAgent`: mesajul 409 devine `agentii care au task-uri si ultimul agent din proiect nu pot fi stersi`.
- In `store`:
  - se sterg campul `Agent.Position`, constantele `PositionLead`/`PositionWorker` si `Lead()`;
  - `agentCols` nu mai contine `position`;
  - `insertAgent` scrie literalul `'worker'` in coloana `position` (cu comentariul `// coloana veche, pastrata doar pentru ca e NOT NULL`);
  - `CreateProject` creeaza `Agent{ProjectID, Name: "Lead", Role: "Lead", Model: "sonnet"}`.

- [ ] **Step 1: Teste**

`server_test.go`:
- `detail`:
  - `Team` devine `[]struct{ Name string; IsLead bool \`json:"isLead"\`; IsRoot bool \`json:"isRoot"\` }`;
  - `Agents` pierde `Position` si primeste `ParentID *int64 \`json:"parentId"\``, `ReviewerID *int64 \`json:"reviewerId"\``, `Responsibilities string`, `IsLead`, `IsRoot`, `IsReviewer`.
- Helper nou `func (e *env) root(t *testing.T, projectID int64) store.Agent` (primul din `ListAgents`), folosit in locul lui `e.st.Lead(...)` in `TestPostMessageToWorker` si `TestDeleteActiveProject409`.
- `TestCreateProjectCreatesLead`: `d.Agents[0].IsRoot && !d.Agents[0].IsLead && d.Agents[0].ParentID == nil`.
- `TestAgentCRUD`: in loc de `a.Position != "worker"`, creeaza cu `"parentId": root.ID` si verifica `a.ParentID != nil && *a.ParentID == root.ID`.
- `TestPromptPreview`: body cu `"parentId": root.ID` (fara `position`); asteapta `Esti Nova, Frontend Developer, in proiectul ApiUnit. Raportezi lui Lead.`.

`graph_test.go` (nou):

```go
package web

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"foreman/internal/store"
)

func TestAgentLinksValidation(t *testing.T) {
	e := setup(t)
	p := e.createProject(t, t.TempDir())
	root := e.root(t, p.ID)
	path := "/api/projects/" + itoa(p.ID) + "/agents"

	code, raw := e.do(t, "POST", path, map[string]any{"name": "Sub", "model": "haiku", "parentId": root.ID, "responsibilities": "API"})
	var sub store.Agent
	json.Unmarshal(raw, &sub)
	if code != http.StatusCreated || sub.ParentID == nil || *sub.ParentID != root.ID || sub.Responsibilities != "API" {
		t.Fatalf("creare Sub: %d %s", code, raw)
	}
	cycle := map[string]any{"name": "Lead", "model": "sonnet", "parentId": sub.ID}
	if code, raw := e.do(t, "PUT", "/api/agents/"+itoa(root.ID), cycle); code != 400 || !strings.Contains(string(raw), "legatura ar crea un ciclu") {
		t.Fatalf("ciclu: %d %s", code, raw)
	}
	other := e.createProject(t, t.TempDir())
	foreign := map[string]any{"name": "X", "model": "haiku", "parentId": e.root(t, other.ID).ID}
	if code, raw := e.do(t, "POST", path, foreign); code != 400 || !strings.Contains(string(raw), "seful ales nu e in acest proiect") {
		t.Fatalf("sef strain: %d %s", code, raw)
	}
	self := map[string]any{"name": "Sub", "model": "haiku", "parentId": root.ID, "reviewerId": sub.ID}
	if code, raw := e.do(t, "PUT", "/api/agents/"+itoa(sub.ID), self); code != 400 || !strings.Contains(string(raw), "propriul reviewer") {
		t.Fatalf("propriul reviewer: %d %s", code, raw)
	}
	long := map[string]any{"name": "Y", "model": "haiku", "responsibilities": strings.Repeat("x", 2001)}
	if code, _ := e.do(t, "POST", path, long); code != 400 {
		t.Fatalf("responsabilitati prea lungi: %d", code)
	}
	if code, _ := e.do(t, "POST", path, map[string]any{"name": "Solo", "model": "haiku"}); code != 201 {
		t.Fatalf("radacina noua: %d", code)
	}
	d := e.detail(t, p.ID)
	roles := map[string][3]bool{}
	for _, a := range d.Agents {
		roles[a.Name] = [3]bool{a.IsRoot, a.IsLead, a.IsReviewer}
	}
	if roles["Lead"] != [3]bool{true, true, false} || roles["Sub"] != [3]bool{false, false, false} || roles["Solo"] != [3]bool{true, false, false} {
		t.Fatalf("roluri: %+v", roles)
	}
}

func TestProjectDetailShape(t *testing.T) {
	e := setup(t)
	p := e.createProject(t, t.TempDir())
	root := e.root(t, p.ID)
	w := e.addWorker(t, p.ID, "Rook")
	e.st.CreateTask(&store.Task{ProjectID: p.ID, Title: "A", AssigneeID: w.ID, CreatorID: &root.ID})
	_, raw := e.do(t, "GET", "/api/projects/"+itoa(p.ID), nil)
	body := string(raw)
	for _, want := range []string{`"parentId":null`, `"reviewerId":null`, `"responsibilities":""`, `"posX":null`,
		`"isRoot":true`, `"isLead":true`, `"isReviewer":false`, `"creatorId":` + itoa(root.ID), `"parentTaskId":null`, `"reviewStage":""`} {
		if !strings.Contains(body, want) {
			t.Errorf("lipseste %s", want)
		}
	}
	if strings.Contains(body, `"position"`) {
		t.Error("position nu mai trebuie sa apara")
	}
	_, raw = e.do(t, "GET", "/api/projects", nil)
	if strings.Contains(string(raw), `"position"`) || !strings.Contains(string(raw), `"isRoot":true`) {
		t.Errorf("lista proiecte: %s", raw)
	}
}

func TestAgentPositionEndpoint(t *testing.T) {
	e := setup(t)
	p := e.createProject(t, t.TempDir())
	root := e.root(t, p.ID)
	path := "/api/agents/" + itoa(root.ID) + "/position"
	if code, raw := e.do(t, "PUT", path, map[string]any{"x": 120.5, "y": 80}); code != http.StatusNoContent {
		t.Fatalf("%d %s", code, raw)
	}
	if a, _ := e.st.GetAgent(root.ID); a.PosX == nil || *a.PosX != 120.5 || *a.PosY != 80 {
		t.Fatalf("pozitie: %+v", a)
	}
	if code, raw := e.do(t, "PUT", path, map[string]any{"x": 1e7, "y": 0}); code != 400 || !strings.Contains(string(raw), "pozitie invalida") {
		t.Fatalf("pozitie mare: %d %s", code, raw)
	}
	if code, _ := e.do(t, "PUT", "/api/agents/9999/position", map[string]any{"x": 1, "y": 1}); code != 404 {
		t.Fatalf("agent inexistent: %d", code)
	}
}

func TestPromptPreviewUsesLinks(t *testing.T) {
	e := setup(t)
	p := e.createProject(t, t.TempDir())
	root := e.root(t, p.ID)
	e.addWorker(t, p.ID, "Rook") // Lead devine lider
	preview := func(body map[string]any) string {
		body["projectId"], body["name"] = p.ID, "Nova"
		code, raw := e.do(t, "POST", "/api/prompt-preview", body)
		if code != 200 {
			t.Fatalf("%d %s", code, raw)
		}
		return string(raw)
	}
	if got := preview(map[string]any{"parentId": root.ID}); !strings.Contains(got, "Raportezi lui Lead.") {
		t.Errorf("agent nou sub Lead: %s", got)
	}
	if got := preview(map[string]any{"parentId": nil}); !strings.Contains(got, "Raportezi utilizatorului.") {
		t.Errorf("agent nou radacina: %s", got)
	}
	if got := preview(map[string]any{"agentId": root.ID, "parentId": nil}); !strings.Contains(got, "mcp__team__create_task") {
		t.Errorf("Lead are echipa: %s", got)
	}
}

func TestDeleteAgentReparentsViaAPI(t *testing.T) {
	e := setup(t)
	p := e.createProject(t, t.TempDir())
	root := e.root(t, p.ID)
	mid := e.addWorker(t, p.ID, "Mid")
	leaf := store.Agent{ProjectID: p.ID, Name: "Leaf", Model: "haiku", ParentID: &mid.ID}
	e.st.CreateAgent(&leaf)
	if code, _ := e.do(t, "DELETE", "/api/agents/"+itoa(mid.ID), nil); code != http.StatusNoContent {
		t.Fatalf("stergere: %d", code)
	}
	if a, _ := e.st.GetAgent(leaf.ID); a.ParentID == nil || *a.ParentID != root.ID {
		t.Fatalf("Leaf nu a trecut la Lead: %+v", a.ParentID)
	}
}
```

- [ ] **Step 2: Implementare** conform Interfaces. `agentView`:
  ```go
  type agentView struct {
  	store.Agent
  	store.Roles
  	Status      string      `json:"status"`
  	ActiveRunID string      `json:"activeRunId"`
  	CurrentTask *store.Task `json:"currentTask"`
  }
  ```
  Handler-ul de pozitie foloseste `decode` si `math.Abs(in.X) > 100000 || math.Abs(in.Y) > 100000` → `badRequest("pozitie invalida")`. Ruta se inregistreaza ca `mux.HandleFunc("PUT /api/agents/{id}/position", s.setAgentPosition)`.
- [ ] **Step 3: Verificare si commit**

`go test -race ./internal/web/ ./internal/store/`, apoi Verificarea standard. `grep -rnE "\.Position\b|Position(Lead|Worker)|\.Lead\(" --include='*.go' internal cmd` nu trebuie sa afiseze nimic (`SetAgentPosition` nu se potriveste cu tiparul).

Verificare manuala, pe un binar construit in `$UI` (HOME temporar, port 7788, oprit la final dupa PID):
```bash
UI=$(mktemp -d); go build -o "$UI/foreman" ./cmd/foreman
HOME="$UI/home" "$UI/foreman" start --port 7788 --no-open & echo $! > "$UI/pid"
curl -s -X POST localhost:7788/api/projects -H 'Content-Type: application/json' -d '{"name":"Test"}'
curl -s -X PUT localhost:7788/api/agents/1/position -H 'Content-Type: application/json' -d '{"x":10,"y":20}' -o /dev/null -w '%{http_code}\n'   # 204
curl -s localhost:7788/api/projects/1 | grep -o '"posX":10'
kill "$(cat "$UI/pid")"
```

```bash
git add internal/web/api_agents.go internal/web/api_projects.go internal/web/server.go internal/web/server_test.go internal/web/graph_test.go internal/store/agents.go internal/store/projects.go internal/store/store_test.go
git commit -m "web: legaturi, pozitie pe canvas si roluri in API"
```

---

### Task 6: UI — canvas-ul echipei in workspace

**Files:** `internal/web/static/js/canvas.js` (nou), `internal/web/static/js/ui.js`, `internal/web/static/js/workspace.js`, `internal/web/static/app.css`

**Interfaces:**

Consumes: `GET /api/projects/{id}` (Task 5), `PUT /api/agents/{id}`, `PUT /api/agents/{id}/position`.

Produces:
- In `ui.js`:
  ```js
  const NS = "http://www.w3.org/2000/svg";
  // svg("g", { class: "node" }, copii...) creeaza un element SVG. Textul se
  // pune ca text, niciodata ca HTML.
  export function svg(tag, attrs = {}, ...children) { /* ca el(), cu createElementNS(NS, tag) si setAttribute pentru toate atributele */ }

  // kindOf: culoarea avatarului; amber pentru lideri si radacini, teal in rest.
  export const kindOf = (a) => (a && (a.isLead || a.isRoot) ? "lead" : "worker");

  // agentBody: corpul complet pentru PUT /api/agents/{id}; patch suprascrie campuri.
  export function agentBody(a, patch = {}) {
    return {
      name: a.name, role: a.role, model: a.model, background: a.background,
      instructions: a.instructions, doneDef: a.doneDef, tools: a.tools,
      responsibilities: a.responsibilities, parentId: a.parentId, reviewerId: a.reviewerId,
      ...patch,
    };
  }
  ```
- `canvas.js`:
  ```js
  // teamCanvas deseneaza echipa ca graf: nodul "Tu" sus, agentii ca noduri,
  // delegari (linie plina cu sageata) si review-uri (linie punctata amber).
  // onSelect(id): click pe nod; onEdit(id): dublu-click; onChanged(): dupa o
  // schimbare de legaturi (workspace-ul reincarca proiectul).
  export function teamCanvas({ onSelect, onEdit, onChanged }) {
    return { el, render(project, selectedId), destroy() };
  }
  ```
- Hook-urile DOM pe care se bazeaza verificarea (nu le redenumi):
  - `.canvas-wrap` (cu `tabindex="0"`) contine `.canvas-bar` si `svg.team-canvas`;
  - un nod de agent e `g.node[data-agent="<id>"]`; nodul „Tu” e `g.node-you`; fiecare are `circle.handle`;
  - liniile vizibile: `path.edge.delegate[data-link="p:<childId>"]` si `path.edge.review[data-link="r:<revieweeId>"]`; peste ele, zonele de click: `path.edge-hit[data-link=...]`. Delegarile Tu → radacina au `data-link="p:<rootId>"`, dar **nu** au `edge-hit` (nu se pot sterge);
  - meniul de legatura: `.link-menu` cu `button[data-kind="delegate"]`, `button[data-kind="review"]` (lipseste cand sursa e „Tu”) si `button[data-kind="cancel"]`;
  - bara: `button.canvas-delete` (dezactivat fara legatura selectata) si `button.canvas-full-toggle` (textul `Ecran mare` sau `Inchide`).

**Cerinte si codul pentru partile neevidente:**
- **Dimensiuni:** nod 184×64, „Tu” 64×30, `svg` cu inaltimea 420px (`.canvas-wrap.full`: `position: fixed; inset: 16px`, iar `svg` ia inaltimea ramasa sub bara).
- **Continutul nodului**, construit numai cu `svg()`:
  - un `rect.card` cu `rx=10`;
  - avatar (cerc cu initiala, clasa `lead`/`worker` dupa `kindOf`);
  - numele (`text`);
  - rolul si „Ce ia: ...” scurtate (`text.sub`). Scurtarea: peste 26 de caractere, taie si adauga `...`;
  - punctul de status (`circle.status-dot.<status lowercase>`);
  - `circle.handle` cu r=6, jos, pe centru.

  Nodul selectat are clasa `selected`.
- **Asezare automata** (doar pentru agentii fara `posX`/`posY`):
  ```js
  const NODE_W = 184, NODE_H = 64, YOU_W = 64, YOU_H = 30, TOP = 16, LEVEL = 110;
  function autoLayout(agents, width) {
    const byId = new Map(agents.map((a) => [a.id, a]));
    const depth = (a) => {
      let d = 0, cur = a;
      while (cur.parentId && byId.has(cur.parentId) && d < agents.length) { cur = byId.get(cur.parentId); d++; }
      return d;
    };
    const pos = new Map(), levels = [];
    for (const a of agents) {
      if (a.posX != null && a.posY != null) pos.set(a.id, { x: a.posX, y: a.posY });
      else (levels[depth(a)] ||= []).push(a);
    }
    levels.forEach((list, d) => list.forEach((a, i) => {
      const step = Math.max(NODE_W + 16, width / (list.length + 1));
      pos.set(a.id, { x: step * (i + 1) - NODE_W / 2, y: TOP + YOU_H + 50 + d * LEVEL });
    }));
    return pos;
  }
  ```
  „Tu” sta la `x = width/2 - YOU_W/2`, `y = TOP`. `viewBox` = reuniunea dreptunghiului `[0, 0, width, height]` cu marginile tuturor nodurilor plus 24px, cu `preserveAspectRatio="xMidYMin meet"`. Asa, tot ce e pe canvas ramane vizibil, eventual micsorat. `width`/`height` vin din `wrap.clientWidth` si din inaltimea `svg`; un `ResizeObserver` pe `.canvas-wrap` redeseneaza.
- **Coordonate:** evenimentele se convertesc in coordonate SVG:
  ```js
  function toSvg(e) {
    const pt = svgEl.createSVGPoint();
    pt.x = e.clientX; pt.y = e.clientY;
    return pt.matrixTransform(svgEl.getScreenCTM().inverse());
  }
  ```
- **Linii:** delegarea merge de la mijlocul de jos al sefului (sau al lui „Tu”) la mijlocul de sus al copilului: `M x1 y1 C x1 ym, x2 ym, x2 y2`, cu `ym = (y1 + y2) / 2` si `marker-end="url(#arrow)"`. Review-ul se ancoreaza pe laturile orientate una spre alta (sus/jos daca `|dy| > |dx|`, altfel stanga/dreapta), cu aceeasi curba. Markerul, in `<defs>`: `marker#arrow` cu `viewBox="0 0 10 10" refX="9" refY="5" markerWidth="7" markerHeight="7" orient="auto-start-reverse"` si `path.arrow` `M0 0 L10 5 L0 10 z`.
- **Mutare:** `pointerdown` pe nod (nu pe handle) → `setPointerCapture`, se retine pozitia de start. La `pointermove`, peste 4px devine drag: nodul se muta prin `transform` si liniile se redeseneaza. La `pointerup`:
  - daca a fost drag: `PUT /api/agents/{id}/position` cu `{x, y}` rotunjite la intreg; pe eroare, `toast` si redesenare cu pozitia veche;
  - altfel: `onSelect(id)`.

  `dblclick` pe nod → `onEdit(id)`. Cat timp un drag e activ, `render()` doar retine proiectul nou (`pendingProject`) si il deseneaza la `pointerup`, ca SSE-ul sa nu smulga nodul din mana.
- **Legare:** `pointerdown` pe `circle.handle` (`stopPropagation`) porneste o linie temporara (`pointer-events: none`) care urmeaza cursorul. La `pointerup`, tinta se afla asa:
  ```js
  const hit = document.elementFromPoint(e.clientX, e.clientY)?.closest("[data-agent]");
  const targetId = hit ? Number(hit.dataset.agent) : null;
  if (targetId && targetId !== sourceId) showMenu(sourceId, targetId, e);
  ```
  `sourceId` e `"you"` pentru „Tu”. Meniul `.link-menu` e un `div` HTML pozitionat absolut in `.canvas-wrap`, la cursor. Butoanele: `Delegheaza catre <Tinta>` si `Verifica munca lui <Tinta>` (doar daca sursa nu e „Tu”), plus `Renunta`. Escape sau click in afara il inchide.
- **Salvarea legaturilor** (acelasi drum pentru meniu si pentru stergere):
  ```js
  async function saveLinks(target, patch) {
    try {
      await api(`/api/agents/${target.id}`, { method: "PUT", body: agentBody(target, patch) });
    } catch (err) {
      toast(err.message); // ex. "legatura ar crea un ciclu"
    }
    await onChanged(); // si pe eroare: redesenam starea reala
  }
  // Delegheaza: patch = { parentId: sourceId === "you" ? null : sourceId }
  // Verifica:   patch = { reviewerId: sourceId }
  ```
- **Selectarea si stergerea unei legaturi:** click pe `path.edge-hit` → `selectedLink = data-link`. Linia primeste clasa `selected`, iar `button.canvas-delete` devine activ. Delete sau Backspace (pe `.canvas-wrap`, care are focus), ori butonul, sterge legatura:
  - `p:<id>` → `saveLinks(agent, { parentId: null })` (tinta devine radacina);
  - `r:<id>` → `saveLinks(agent, { reviewerId: null })`.

  Click pe fundal deselecteaza.
- **Ecran mare:** `button.canvas-full-toggle` comuta clasa `full` pe `.canvas-wrap`; Escape inchide. Redesenarea vine din `ResizeObserver`.
- **Integrarea in `workspace.js`:**
  - `team` (grid-ul de carduri) se inlocuieste cu `canvas.el`. `renderTeam()` devine `canvas.render(state.project, state.selected)`;
  - `onSelect` = `select`; `onEdit` = `location.hash = \`#/p/${projectId}/agent/${id}\``; `onChanged` = `reload`;
  - hint-ul sectiunii: `Trage de ● ca sa legi agentii. Dublu-click deschide editorul.` Legenda din `.canvas-bar`: `Delegare` (linie plina) si `Review` (linie punctata);
  - `memberCard` se sterge; `avatar(a.name, a.position, ...)` devine `avatar(a.name, kindOf(a), ...)` peste tot in fisier;
  - `canvas.destroy()` intra in functia de curatare.
- **CSS:**
  - `.canvas-wrap` (relative, border `--line`, fundal `--panel`), `.canvas-wrap svg` (`display:block; width:100%; height:420px; touch-action:none; user-select:none`);
  - `.canvas-wrap.full` (fixed, `inset:16px`, `z-index:20`, fundal `--bg`);
  - `.node .card` (fill `--card-2`, stroke `--line-2`), `.node.selected .card` (stroke `--amber`, fill `--amber-bg`), `.node text` (fill `--text`), `.node text.sub` (fill `--muted`, 11px);
  - `.handle` (fill `--muted`, `cursor: crosshair`, amber la hover);
  - `.edge` (`fill:none; stroke-width:1.6`), `.edge.delegate` (stroke `--muted`), `.edge.review` (stroke `--amber`, `stroke-dasharray: 5 4`), `.edge.selected` (`stroke-width:3`), `.edge-hit` (`stroke: transparent; stroke-width:12; pointer-events: stroke; cursor:pointer`), `.arrow` (fill `--muted`);
  - `.status-dot` pe `working`/`queued`/`idle`, cu culorile de la `.dot`;
  - `.link-menu` (card mic, butoane pe coloana).

  Sterge clasele care nu mai sunt folosite: `.team`, `.team-workers`, `.member-cell`, `.member`, `.member-body`, `.edit-link`. Pastreaza `.member-name`, `.status`, `.dot`, `.tag`.

- [ ] **Step 1: Sintaxa**

```bash
for f in internal/web/static/js/*.js; do cp "$f" "$UI/check.mjs"; node --check "$UI/check.mjs" || echo "EROARE $f"; done
```
Nu trebuie sa apara nicio linie `EROARE`.

- [ ] **Step 2: Masuratori si screenshot-uri prin Chrome headless (CDP)**

Porneste serverul si Chrome (ambele oprite la final dupa PID):
```bash
UI=$(mktemp -d); go build -o "$UI/foreman" ./cmd/foreman
HOME="$UI/home" "$UI/foreman" start --port 7788 --no-open > "$UI/server.log" 2>&1 & echo $! > "$UI/server.pid"
CHROME="/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
"$CHROME" --headless=new --remote-debugging-port=9333 --user-data-dir="$UI/chrome" --no-first-run --hide-scrollbars about:blank > /dev/null 2>&1 & echo $! > "$UI/chrome.pid"
until curl -s 127.0.0.1:9333/json/version > /dev/null; do sleep 0.2; done
```
**Nu** folosi `--screenshot` cu `--virtual-time-budget`: SSE-ul tine pagina ocupata si comanda se blocheaza.

`$UI/cdp.mjs` (Node 22: `fetch` si `WebSocket` globale, fara pachete):
```js
import fs from "node:fs";

export async function connect(port) {
  const pages = await (await fetch(`http://127.0.0.1:${port}/json/list`)).json();
  const ws = new WebSocket(pages.find((p) => p.type === "page").webSocketDebuggerUrl);
  await new Promise((r) => ws.addEventListener("open", r, { once: true }));
  let seq = 0;
  const waiting = new Map();
  ws.addEventListener("message", (e) => {
    const m = JSON.parse(e.data);
    if (m.id && waiting.has(m.id)) { waiting.get(m.id)(m); waiting.delete(m.id); }
  });
  const send = (method, params = {}) => new Promise((resolve, reject) => {
    const id = ++seq;
    waiting.set(id, (m) => (m.error ? reject(new Error(method + ": " + JSON.stringify(m.error))) : resolve(m.result)));
    ws.send(JSON.stringify({ id, method, params }));
  });
  const evaluate = async (expr) => {
    const r = await send("Runtime.evaluate", { expression: expr, returnByValue: true, awaitPromise: true });
    if (r.exceptionDetails) throw new Error(expr + ": " + JSON.stringify(r.exceptionDetails));
    return r.result.value;
  };
  const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
  const waitFor = async (expr, ms = 10000) => {
    const end = Date.now() + ms;
    while (Date.now() < end) { if (await evaluate(expr)) return; await sleep(150); }
    throw new Error("timeout: " + expr);
  };
  const viewport = (width, height) => send("Emulation.setDeviceMetricsOverride", { width, height, deviceScaleFactor: 1, mobile: false });
  const shot = async (file) => fs.writeFileSync(file, Buffer.from((await send("Page.captureScreenshot", { format: "png" })).data, "base64"));
  const mouse = (type, x, y) => send("Input.dispatchMouseEvent", {
    type, x, y, button: "left", buttons: type === "mouseReleased" ? 0 : 1, clickCount: type === "mouseMoved" ? 0 : 1,
  });
  const drag = async (from, to) => {
    await mouse("mouseMoved", from.x, from.y);
    await mouse("mousePressed", from.x, from.y);
    for (let i = 1; i <= 8; i++) await mouse("mouseMoved", from.x + ((to.x - from.x) * i) / 8, from.y + ((to.y - from.y) * i) / 8);
    await mouse("mouseReleased", to.x, to.y);
  };
  const click = async (p) => { await mouse("mousePressed", p.x, p.y); await mouse("mouseReleased", p.x, p.y); };
  // centrul unui element, in coordonate de ecran
  const center = (sel) => evaluate(`(() => { const b = document.querySelector(${JSON.stringify(sel)}).getBoundingClientRect(); return { x: b.x + b.width / 2, y: b.y + b.height / 2 }; })()`);
  return { send, evaluate, waitFor, viewport, shot, drag, click, center, sleep, close: () => ws.close() };
}

// api: apeluri JSON catre serverul de test
export async function call(method, path, body) {
  const res = await fetch("http://127.0.0.1:7788" + path, {
    method, headers: { "Content-Type": "application/json" }, body: body && JSON.stringify(body),
  });
  const text = await res.text();
  if (!res.ok) throw new Error(`${method} ${path}: ${res.status} ${text}`);
  return text ? JSON.parse(text) : null;
}
```

`$UI/canvas-check.mjs` face, in ordine, si opreste la prima abatere (`throw`):
1. **Date:**
   - `POST /api/projects {name:"Canvas"}`; `coord` = primul agent (radacina implicita, cu numele `Lead`);
   - agentii noi, toti `model:"haiku"`, `role:"Dev"`, `responsibilities:"fisiere <nume>"`: `Sub` (parent `coord`), `Rev` (parent `coord`), `Wes` (parent `Sub`);
   - `PUT` pe `Wes` cu `agentBody` + `reviewerId: Rev`;
   - `agentBody` e copiat in script.
2. **Pentru fiecare viewport** `[1440, 900]` si `[1280, 720]`:
   - `viewport(w, h)`, apoi `Page.navigate` la `http://127.0.0.1:7788/#/p/<id>`;
   - `waitFor` pana cand exista 4 `.canvas-wrap [data-agent]`;
   - masoara cu `evaluate` si verifica:
     - `svg` are inaltimea intre 380 si 460, latimea ≥ 300 si `top < innerHeight`;
     - cele 4 dreptunghiuri de nod sunt in interiorul dreptunghiului `svg` si nu se suprapun doua cate doua;
     - `.edge.delegate` = 4 (Tu→Coord, Coord→Sub, Coord→Rev, Sub→Wes) si `.edge.review` = 1;
     - `.composer textarea` are `top ≥ 0` si `bottom ≤ innerHeight` (chat-ul ramane vizibil);
     - `document.documentElement.scrollWidth ≤ innerWidth`;
   - `shot("$UI/ws-<w>x<h>.png")`.
3. **Mutare** (la 1440×900):
   - `drag` de la centrul lui `Wes` cu (+60, +30);
   - `waitFor` prin `call("GET", ...)` pana cand `Wes.posX != null`;
   - dupa `reload`, nodul e tot la noua pozitie (±2px fata de masuratoarea dinaintea reload-ului).
4. **Review nou:**
   - `drag` de la `[data-agent="<Rev>"] .handle` la centrul lui `Sub`;
   - `waitFor(".link-menu")` si click pe `button[data-kind="review"]`;
   - API: `Sub.reviewerId === Rev.id`; `.edge.review` = 2.
5. **Ciclu:**
   - `drag` de la handle-ul lui `Wes` la `Coord`, click pe `button[data-kind="delegate"]`;
   - `waitFor` ca `#toast` sa fie vizibil si sa contina `legatura ar crea un ciclu`;
   - API: `Coord.parentId === null`.
6. **Stergere legatura:**
   - calculeaza mijlocul lui `path.edge-hit[data-link="r:<Wes>"]` in coordonate de ecran:
     ```js
     (() => { const p = document.querySelector('path.edge-hit[data-link="r:WES"]'); const m = p.getPointAtLength(p.getTotalLength() / 2);
       const s = new DOMPoint(m.x, m.y).matrixTransform(p.getScreenCTM()); return { x: s.x, y: s.y }; })()
     ```
     (cu `WES` inlocuit prin id);
   - `click` acolo, apoi `Input.dispatchKeyEvent` `keyDown`/`keyUp` cu `key:"Delete", code:"Delete", windowsVirtualKeyCode:46`;
   - API: `Wes.reviewerId === null`.
7. **Din „Tu”:**
   - `drag` de la `.node-you .handle` la `Sub`, apoi `delegate`;
   - API: `Sub.parentId === null`;
   - `.edge.delegate[data-link="p:<Sub>"]` exista.
8. **Click si dublu-click:**
   - click simplu pe `Rev` → `.console-head` contine `Rev`;
   - dublu-click pe `Coord` (doua perechi press/release, a doua cu `clickCount: 2`) → `location.hash` devine `#/p/<id>/agent/<Coord>`;
   - `Page.navigate` inapoi.
9. **Ecran mare:**
   - click pe `.canvas-full-toggle` → `svg` are latimea ≥ `innerWidth - 60` si inaltimea ≥ `innerHeight - 120`;
   - `shot("$UI/full.png")`;
   - `keyDown Escape` → inaltimea revine la 420 ± 2.
10. Printeaza masuratorile (JSON) si `OK`.

Ruleaza `node "$UI/canvas-check.mjs"`, apoi deschide fiecare PNG cu Read si descrie in raport ce se vede: noduri, linii, sageti, chat, suprapuneri. Nu afirma nimic ce nu apare in masuratori sau in imagini. La final:
```bash
kill "$(cat "$UI/chrome.pid")" "$(cat "$UI/server.pid")"
```

- [ ] **Step 3: Verificare si commit**

`go test ./internal/web/` si Verificarea standard.

```bash
git add internal/web/static/js/canvas.js internal/web/static/js/ui.js internal/web/static/js/workspace.js internal/web/static/app.css
git commit -m "UI: canvas pentru echipa, cu legaturi trase cu mouse-ul"
```

---

### Task 7: UI — editorul agentului, board-ul cu etapa de review, consola, lista de proiecte

**Files:** `internal/web/static/js/agent.js`, `internal/web/static/js/workspace.js`, `internal/web/static/js/projects.js`, `internal/web/static/app.css`

**Interfaces:**

Consumes: `agentBody`, `kindOf` (Task 6); API-ul din Task 5.

Cerinte:
- **`agent.js`:**
  - campuri noi:
    - `Ce ia (responsabilitati)`: textarea cu `rows=3`, `maxlength=2000` si placeholder-ul `ex. Endpoint-uri API si migrari`;
    - `Raporteaza lui`: select cu `Tu` (valoare `""`), apoi ceilalti agenti ai proiectului (fara agentul editat);
    - `Verificat de`: select cu `nimeni` (`""`), apoi ceilalti agenti.
  - Textul fix „Pozitie in echipa” dispare.
  - Pentru un agent nou, `Raporteaza lui` porneste pe **prima radacina** (`project.agents.find((x) => x.isRoot)`), iar `Verificat de` pe `nimeni`.
  - `save`:
    - trimite `agentBody`-ul campurilor, cu `parentId: select.value ? Number(select.value) : null` si `reviewerId` la fel;
    - o eroare 400 (ex. ciclu) apare in `toast`, iar formularul ramane deschis.
  - `updatePreview`:
    - trimite `{projectId, agentId: agentId || 0, name, role, background, instructions, doneDef, parentId}`;
    - se reapeleaza si la `change` pe `Raporteaza lui`.
  - `isLead`:
    - devine `const isRoot = !parentSelect.value`; se recalculeaza la `change`;
    - schimba eticheta si placeholder-ul lui `doneDef`: radacina → `Rezumatul final: ce iti raporteaza`, altfel `Definition of done: ce ii preda sefului`.
  - Butonul `Sterge` apare pentru orice agent existent. Serverul refuza cu 409 agentii cu task-uri si ultimul agent.
  - Hint-ul de la Nume devine `Liderul asigneaza task-uri dupa acest nume.`
- **`workspace.js`:**
  - **board:** in `taskCard`, pentru `t.status === "review"`, un `span.task-stage` cu `la <Nume>`:
    - daca `t.reviewStage === "reviewer"`, numele e al reviewer-ului asignatului (`byId.get(who?.reviewerId)`);
    - altfel e al creatorului (`byId.get(t.creatorId)`), cu fallback la seful asignatului;
    - daca nu exista nimeni, span-ul lipseste.
  - **consola:**
    - agentul selectat implicit e `state.project.agents.find((a) => a.isRoot)?.id ?? state.project.agents[0]?.id`, la pornire si dupa stergerea agentului selectat;
    - hint-ul pentru o radacina: `Thread direct. Ii dai obiective aici, iar el delegheaza echipei lui.`; pentru un agent cu sef: `Thread direct. <Sef> primeste in copie ce ii scrii.`
- **`projects.js`:**
  - `avatar(m.name, kindOf(m), 28)`;
  - hint-ul de limite: `Lead-ul nu intra in limita de workeri.` devine `Agentii care iti raporteaza direct nu intra in limita.`
- **CSS:** `.task-stage` (`font-size:11px`, `color: var(--amber)`, `margin-left:auto`).

- [ ] **Step 1: Sintaxa** — bucla `node --check` din Task 6, Step 1.
- [ ] **Step 2: CDP** (acelasi mediu si `cdp.mjs` ca in Task 6; porneste un server si un Chrome noi, cu `HOME="$UI/home"` intr-un `UI` nou, si ruleaza scriptul cu `UI="$UI" node "$UI/editor-check.mjs"`)

`$UI/editor-check.mjs`:
1. **Date:** ca in Task 6 (Coord, Sub, Rev, Wes, cu Wes verificat de Rev).
2. **Task in review:** doua task-uri direct in baza de date, cu serverul pornit (WAL + busy_timeout permit asta):
   ```js
   import { execFileSync } from "node:child_process";
   const db = `${process.env.UI}/home/.foreman/foreman.db`;
   const ts = new Date().toISOString();
   execFileSync("sqlite3", [db, `INSERT INTO tasks (project_id, number, title, assignee_id, status, review_stage, creator_id, created_at, updated_at)
     VALUES (${p.id}, 1, 'Parser', ${wes.id}, 'review', 'reviewer', ${sub.id}, '${ts}', '${ts}'),
            (${p.id}, 2, 'Import', ${sub.id}, 'review', 'lead', ${coord.id}, '${ts}', '${ts}')`]);
   ```
3. **Board** (1440×900, `#/p/<id>`):
   - `waitFor` pe 2 `.task-stage`;
   - textele sunt exact `la Rev` (pe T-1) si `la Lead` (pe T-2; numele lui Coord e `Lead`);
   - `shot("$UI/board.png")`.
4. **Consola:** la incarcare, `.console-head` contine `Lead` (prima radacina); dupa click pe `Wes` in canvas, hint-ul contine `Sub primeste in copie`.
5. **Editor** (`#/p/<id>/agent/<Wes>`):
   - exista etichetele `Ce ia (responsabilitati)`, `Raporteaza lui` si `Verificat de`; textul `Pozitie in echipa` nu apare nicaieri (`document.body.innerText`);
   - selecturile au valorile `String(sub.id)` si `String(rev.id)`;
   - seteaza `Raporteaza lui` pe `Tu`, cu `dispatchEvent(new Event("change"))`;
   - `waitFor` ca `.preview` sa contina `Raportezi utilizatorului.`; `shot("$UI/editor.png")`.
6. **Ciclu din editor** (`#/p/<id>/agent/<Coord>`): alege `Raporteaza lui` = `Wes`, trimite formularul (`form.requestSubmit()`); `#toast` contine `legatura ar crea un ciclu`, iar hash-ul ramane pe editor.
7. **Agent nou** (`#/p/<id>/agent/new`): `Raporteaza lui` are valoarea `String(coord.id)`.
8. **Lista de proiecte** (`#/`): avatarul lui `Lead` are clasa `avatar lead`.

Deschide PNG-urile cu Read si descrie ce se vede. Opreste Chrome si serverul dupa PID.

- [ ] **Step 3: Verificare si commit**

`go test ./internal/web/` si Verificarea standard.

```bash
git add internal/web/static/js/agent.js internal/web/static/js/workspace.js internal/web/static/js/projects.js internal/web/static/app.css
git commit -m "UI: legaturi in editorul de agent, review pe board, consola pe prima radacina"
```

---

### Task 8: documentatie + test cap-coada cu Claude real (haiku), prin API

**Files:** `README.md`

- [ ] **README.md:**
  - Primul paragraf: echipa unui proiect e o ierarhie: tu dai obiective agentilor radacina, liderii impart munca doar echipei lor directe, iar munca trece pe la reviewer (daca exista), apoi la liderul care a creat task-ul.
  - In „Cum functioneaza”, bullet-ul despre tool-uri devine lista pe roluri:
    - liderii au `create_task`, `list_tasks`, `accept_task`, `send_back`;
    - agentii cu sef au `report_done`;
    - reviewerii au `review_approve` si `review_reject`;
    - un agent radacina fara echipa nu are tool-uri.
  - Bullet nou despre sub-task-uri: un sub-lider sparge task-ul primit si il raporteaza in sus dupa ce toate sub-task-urile sunt gata.
  - Bullet nou despre canvas: nodurile se muta cu mouse-ul, legaturile se trag din ●, dublu-click deschide editorul.
  - Bullet nou: `max_parallel` se aplica doar agentilor care au sef.
- [ ] **Test cap-coada** cu HOME-ul real (autentificarea `claude` sta acolo), port 7788, `--no-open`, binar in `$UI`, oprit dupa PID. Scriptul `$UI/e2e.mjs` foloseste `call` si `agentBody` din Task 6:
  1. `POST /api/projects {name:"E2E graf", goal:"Doua fisiere mici, facute de doua echipe", maxParallel: 3}` (fara workdir: `~/Foreman/e2e-graf`).
  2. Lead-ul radacina devine prin `PUT`: `name "Coord"`, `role "Coordonator"`, `model "haiku"`, `tools []`, `responsibilities "Imparte obiectivul intre echipe"`.
  3. Agenti noi, toti pe `haiku`:
     - `Front` (parent Coord, `Sub-lider frontend`, `Tot ce tine de HTML`, tools `[]`);
     - `Back` (parent Coord, `Sub-lider backend`, `Tot ce tine de JSON si date`, tools `[]`);
     - `Rev` (parent Coord, `Reviewer`, `Verifica munca workerilor`, tools `[]`);
     - `Ana` (parent Front, `Dev HTML`, `Scrie fisiere HTML`, tools `["write"]`);
     - `Ion` (parent Back, `Dev JSON`, `Scrie fisiere JSON`, tools `["write"]`).

     Apoi `PUT` pe Ana si Ion cu `reviewerId: Rev`.
  4. `POST /api/projects/{id}/resume`, apoi mesaj catre Coord:
     `Obiectiv: fisierul index.html cu un titlu h1 "Salut" (echipa Front) si fisierul date.json cu {"ok": true} (echipa Back). Da fiecare parte sub-liderului potrivit. Nu scrie tu fisierele.`
  5. **Polling** la 3 secunde, maxim 20 de minute:
     - la fiecare schimbare de `status/reviewStage`, printeaza un rand `HH:MM:SS T-n status/stage asignat=<nume> creator=<nume> parinte=<T-m sau ->`;
     - opreste-te cand exista cel putin 4 task-uri, toate `done`, si toti agentii sunt `Idle`;
     - daca proiectul trece pe pauza sau expira timpul, opreste echipa (`POST /stop`), printeaza ultimele 15 mesaje din thread-ul fiecarui agent si raporteaza esecul. Nu repara nimic in acest task; problema se duce inapoi la task-ul responsabil.
  6. **Verificari finale** (fiecare cu PASS/FAIL in raport):
     - cel putin un task creat de Coord pentru Front si unul pentru Back, ambele cu `parentTaskId` null;
     - cel putin un sub-task pentru Ana cu `parentTaskId` = task-ul lui Front, si unul pentru Ion cu `parentTaskId` = task-ul lui Back;
     - pentru fiecare task al Anei si al lui Ion, thread-ul lui Rev (`GET /api/agents/<Rev>/messages`) contine `a terminat T-n. Verifica-l` si `A aprobat T-n`;
     - thread-ul lui Front si al lui Back contine `Toate sub-task-urile pentru T-`;
     - `<workdir>/index.html` contine `Salut`, iar `<workdir>/date.json` se parseaza si are `ok === true`;
     - ultimul mesaj al lui Coord catre utilizator e un rezumat (se printeaza).
  7. La final: `POST /stop`, apoi `DELETE /api/projects/{id}`. Folderul `~/Foreman/e2e-graf` ramane pe disc, iar asta e corect.

  In raport: tabelul tranzitiilor, verificarile PASS/FAIL, durata totala si orice comportament neasteptat (de exemplu, un sub-lider care a raportat fara sa astepte sau un reviewer care n-a fost trezit).
- [ ] Commit:
```bash
git add README.md
git commit -m "docs: echipe ca graf, review si editor canvas"
```

---

## Acoperirea spec-ului

| Sectiune din spec | Task |
|---|---|
| Modelul de date: coloane noi, drop index, conversie | 1 |
| Roluri derivate | 1 (`RolesOf`), 5 (API) |
| Invarianti (acelasi proiect, auto-legaturi, ciclu → 400) | 1, 5 |
| Stergerea agentului (re-parentare, reviewer NULL, 409 cu task-uri) | 1, 5 |
| Proiect nou cu radacina „Lead”; agent nou sub prima radacina | 1, 5, 7 |
| Crearea task-urilor: doar echipa directa, `creator_id`, `parent_task_id` | 3 |
| Task-ul sub-liderului ramane `in_progress` cat timp are sub-task-uri deschise | 4 (+ 1 pentru Requeue) |
| Notificarea „toate sub-task-urile ... sunt gata” | 3 (`afterDone`), 4 (accept de utilizator) |
| `report_done(summary, task?)` si rutarea reviewer/creator | 3 |
| `review_approve`, `review_reject`, limita de 3 | 3 |
| `accept_task`/`send_back` doar pentru creator sau stramos | 3 |
| Scheduler generalizat, `max_parallel` fara radacini | 4 |
| Stop, resume si restart raman ca acum | 4 (`wakeForDecisions`, teste existente) |
| Prompturi: linia de rol, „Cum lucrezi” compus, echipa directa, reviewer, task-uri deschise | 2 |
| Schimbarea legaturilor porneste o sesiune noua (hash) | 2 (system prompt-ul contine legaturile) |
| MCP: `Caller` cu roluri, lista de tool-uri pe roluri, revalidare din DB | 3 |
| API: campuri, `position` eliminat, `PUT .../position`, prompt-preview cu legaturi | 5 |
| UI canvas (Tu, noduri, drag, legaturi, meniu, stergere, ecran mare, click/dublu-click) | 6 |
| Editorul agentului, board-ul (la reviewer/lider), consola pe prima radacina | 7 |
| Testare UI (CDP la 1440×900 si 1280×720), e2e cu haiku | 6, 7, 8 |

## Ambiguitati rezolvate in plan

- **`report_done` cu sub-task-uri deschise** e refuzat (`... mai are sub-task-uri deschise`), ca un sub-lider sa nu poata raporta in sus inainte de timp.
- **`create_task` intr-un run de notificari:** daca liderul are exact un task `in_progress`, noul task devine sub-task al lui. Spec-ul acopera doar run-ul pe task.
- **Stop si restart** nu mai trec in Queued un task care asteapta sub-task-uri (`Requeue*` il sar). E consecinta regulii din `finish`; altfel sub-liderul ar fi repornit si ar duplica munca.
- **Rezultatul unui task fara `report_done`**, fara sub-task-uri deschise, trece prin reviewer ca un `report_done` obisnuit. Un run esuat merge direct la lider (`stage=lead`).
- **Creatorul sters:** decide oricare sef al asignatului, iar notificarile merg la seful direct al asignatului.
- **Copia mesajelor utilizatorului** merge la seful direct, nu la „lead”. O radacina nu primeste copie.
- **Workerii** (agenti fara echipa) nu primesc bloc „Echipa ta directa” in tura. Blocul apare doar pentru lideri, cum cere spec-ul.
- **Absenta lui `parentId`** in `POST/PUT` inseamna radacina (la fel ca `null`). Implicitul „sub prima radacina” pentru un agent nou e aplicat de editor.
- **Stergerea agentilor:** orice agent se poate sterge, inclusiv o radacina (copiii ei devin radacini), cu exceptia celor cu task-uri si a ultimului agent din proiect.
- **`prompt-preview`:** seful vine din body (ca editorul sa arate imediat schimbarea), iar rolurile de lider si reviewer vin din legaturile salvate ale lui `agentId`.
- **Planul are 8 task-uri:** `mcp` si tool-urile din `team` sunt comise impreuna, pentru ca interfata `mcp.Backend` si implementarea ei trebuie sa compileze in acelasi pas. UI-ul e impartit in doua task-uri: canvas-ul, apoi editorul si board-ul.
