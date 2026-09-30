# Coloane proprii (etape de lucru) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Proiectele isi pot defini coloanele board-ului ca etape de lucru, cu agenti per etapa. Task-urile trec singure dintr-o etapa in alta (primul agent liber le ia), iar la final decide liderul.

**Architecture:**
- Etapa se adauga peste statusurile existente, prin `tasks.stage_id`.
- `store` primeste tabelele `stages` si `stage_agents`.
- `team` alege agentul din etapa la pornirea run-ului si muta task-ul intre etape in `ReportDone`, `ReviewApprove`, `ReviewReject`, `SendBack` si in finalul run-ului.
- `mcp.Caller.StageKind` adauga tool-urile potrivite etapei.
- `web` primeste `PUT /stages`, `POST /tasks` si `POST /decide`.
- UI-ul primeste un editor de coloane si board-ul dinamic.

**Tech Stack:** Go (stdlib, modernc.org/sqlite), JS vanilla.

**Spec:** `docs/superpowers/specs/2026-09-30-foreman-stages-design.md`

## Global Constraints

- `go vet ./...` si `go test -race ./...` trec; `gofmt -l internal cmd` nu listeaza nimic.
- Cross-compile: `GOOS=darwin GOARCH=arm64 go build ./cmd/foreman` si `GOOS=windows go build ./cmd/foreman`.
- Mesajele pentru utilizator sunt in romana, fara diacritice. Codul e simplu si comentat unde nu e evident.
- UI-ul se construieste doar cu `el()` (fara `innerHTML`).
- Proiectele fara etape se comporta exact ca inainte: toate testele existente din `team` trec neschimbate.
- **Ruling fata de spec:** coloana noua `tasks.by_user`. Un task creat din UI are `creator_id` NULL, iar `decider` ar cadea pe seful asignatului; `by_user=1` spune ca decide utilizatorul.
- **Ruling fata de spec:** stergerea unui agent care e „loc tinut” (`assignee_id`) pe un task ramane refuzata (409, regula de azi). Spec-ul presupunea ca merge. Utilizatorul il scoate intai din etapa, iar scheduler-ul muta task-ul pe altcineva.

## Review Focus

- **Etapa de verificare fara nicio etapa `work` inainte:** respingerea duce task-ul in prima etapa (care poate fi chiar ea). Nu trebuie sa apara panic sau un index -1.
- **Reordonare sau stergere de etape cu task-uri `queued` in etapa stearsa:** stergerea e refuzata (409). Reordonarea nu strica `stage_id`.
- **Doi agenti in aceeasi etapa si doua task-uri `queued`:** fiecare agent ia cate unul in aceeasi reconciliere, nu acelasi agent pe amandoua.
- **Proiect cu etape, lider fara copii (agentii sunt doar in etape):** liderul radacina primeste `create_task` si il poate apela.
- **Task creat de utilizator care ajunge la final:** apare in „La lider” cu butoane pentru utilizator. Nu il decide seful asignatului.

---

### Task 1: store (etape si stage_id)

**Files:**
- Create: `internal/store/migrations/005_stages.sql`, `internal/store/stages.go`, `internal/store/stages_test.go`
- Modify: `internal/store/tasks.go`, `internal/store/store_test.go` (user_version 4 → 5)

**Interfaces (Produces):**

```go
const (KindWork = "work"; KindCheck = "check")
type Stage struct {
    ID        int64   `json:"id"`
    ProjectID int64   `json:"projectId"`
    Position  int     `json:"position"`
    Name      string  `json:"name"`
    Kind      string  `json:"kind"`
    AgentIDs  []int64 `json:"agentIds"`
}
type StageInUseError string // mesajul contine numele etapei
func (s *Store) ListStages(projectID int64) ([]Stage, error)
func (s *Store) SaveStages(projectID int64, stages []Stage) ([]Stage, error)
func (s *Store) CountStageTasks(stageID int64) (int, error)
// Task primeste: StageID *int64 `json:"stageId"`, ByUser bool `json:"byUser"`
// CreateTask insereaza stage_id si by_user; SaveTask salveaza si assignee_id si stage_id.
```

- [ ] **Step 1: Test** (`internal/store/stages_test.go`)

```go
package store

import (
	"errors"
	"testing"
)

func TestSaveStagesLifecycle(t *testing.T) {
	s := newStore(t)
	p, lead := newProject(t, s)
	bob := newAgent(t, s, p.ID, "Bob", &lead.ID)
	vera := newAgent(t, s, p.ID, "Vera", &lead.ID)

	got, err := s.SaveStages(p.ID, []Stage{
		{Name: "Dev", Kind: KindWork, AgentIDs: []int64{bob.ID}},
		{Name: "QA", Kind: KindCheck, AgentIDs: []int64{vera.ID, bob.ID}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Position != 0 || got[1].Position != 1 || got[1].AgentIDs[0] != vera.ID {
		t.Fatalf("salvare: %+v", got)
	}
	// reordonare + redenumire pastreaza id-urile
	dev, qa := got[0], got[1]
	qa.Name = "Test"
	got, err = s.SaveStages(p.ID, []Stage{qa, dev})
	if err != nil || got[0].ID != qa.ID || got[0].Name != "Test" || got[1].ID != dev.ID {
		t.Fatalf("reordonare: %+v %v", got, err)
	}
	// o etapa cu task-uri nu se sterge
	task := Task{ProjectID: p.ID, Title: "x", AssigneeID: bob.ID, StageID: &dev.ID}
	if err := s.CreateTask(&task); err != nil {
		t.Fatal(err)
	}
	var inUse StageInUseError
	if _, err := s.SaveStages(p.ID, []Stage{qa}); !errors.As(err, &inUse) {
		t.Fatalf("vreau StageInUseError, am %v", err)
	}
	list, _ := s.ListStages(p.ID)
	if len(list) != 2 {
		t.Fatalf("nimic sters dupa eroare: %d", len(list))
	}
	// stage_id si by_user se citesc; SaveTask salveaza assignee si stage
	task.ByUser = true
	task.AssigneeID = vera.ID
	task.StageID = &qa.ID
	if err := s.SaveTask(&task); err != nil {
		t.Fatal(err)
	}
	back, _ := s.GetTask(task.ID)
	if back.AssigneeID != vera.ID || back.StageID == nil || *back.StageID != qa.ID {
		t.Fatalf("SaveTask: %+v", back)
	}
	// stergerea agentului il scoate din etape (cascada)
	if err := s.DeleteAgent(bob.ID); err != nil && !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
}

func TestCreateTaskByUser(t *testing.T) {
	s := newStore(t)
	p, lead := newProject(t, s)
	task := Task{ProjectID: p.ID, Title: "x", AssigneeID: lead.ID, ByUser: true}
	if err := s.CreateTask(&task); err != nil {
		t.Fatal(err)
	}
	if back, _ := s.GetTask(task.ID); !back.ByUser || back.StageID != nil {
		t.Fatalf("%+v", back)
	}
}
```

Inainte de Step 2, verifica semnaturile `newProject`/`newAgent` din `store_test.go`. Planul presupune `newProject(t, s) (Project, Agent)` si `newAgent(t, s, projectID, name, parent *int64) Agent`, deja existente.

- [ ] **Step 2: Run to fail**

Run: `go test ./internal/store/`
Expected: FAIL (`undefined: SaveStages`).

- [ ] **Step 3: Implement**

`005_stages.sql`:

```sql
-- coloane proprii pe board: etape de lucru cu agentii lor
CREATE TABLE stages (
  id         INTEGER PRIMARY KEY,
  project_id INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  position   INTEGER NOT NULL,
  name       TEXT NOT NULL,
  kind       TEXT NOT NULL DEFAULT 'work' CHECK (kind IN ('work', 'check'))
);
CREATE INDEX stages_project ON stages(project_id, position);

CREATE TABLE stage_agents (
  stage_id INTEGER NOT NULL REFERENCES stages(id) ON DELETE CASCADE,
  agent_id INTEGER NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
  PRIMARY KEY (stage_id, agent_id)
);

-- etapa curenta a task-ului (NULL = proiect fara etape, sau a trecut de ultima)
ALTER TABLE tasks ADD COLUMN stage_id INTEGER REFERENCES stages(id);
-- task creat de utilizator din board: la final decide tot utilizatorul
ALTER TABLE tasks ADD COLUMN by_user INTEGER NOT NULL DEFAULT 0;
```

`stages.go`:

```go
package store

import "database/sql"

// Tipurile de etapa: de lucru (report_done) sau de verificare (review_approve/reject).
const (
	KindWork  = "work"
	KindCheck = "check"
)

// Stage e o coloana a board-ului: o etapa prin care trece un task.
type Stage struct {
	ID        int64   `json:"id"`
	ProjectID int64   `json:"projectId"`
	Position  int     `json:"position"`
	Name      string  `json:"name"`
	Kind      string  `json:"kind"`
	AgentIDs  []int64 `json:"agentIds"` // in ordinea in care iau task-uri
}

// StageInUseError: o etapa scoasa din lista inca are task-uri.
type StageInUseError string

func (e StageInUseError) Error() string {
	return "etapa " + string(e) + " are task-uri: termina-le sau muta-le intai"
}

// ListStages intoarce etapele proiectului in ordinea de pe board.
func (s *Store) ListStages(projectID int64) ([]Stage, error) {
	return listStages(s.db, projectID)
}

type querier interface {
	Query(query string, args ...any) (*sql.Rows, error)
}

func listStages(q querier, projectID int64) ([]Stage, error) {
	rows, err := q.Query(`SELECT id, project_id, position, name, kind FROM stages WHERE project_id = ? ORDER BY position, id`, projectID)
	if err != nil {
		return nil, err
	}
	out := []Stage{}
	for rows.Next() {
		var st Stage
		if err := rows.Scan(&st.ID, &st.ProjectID, &st.Position, &st.Name, &st.Kind); err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, st)
	}
	rows.Close()
	for i := range out {
		ar, err := q.Query(`SELECT agent_id FROM stage_agents WHERE stage_id = ? ORDER BY rowid`, out[i].ID)
		if err != nil {
			return nil, err
		}
		out[i].AgentIDs = []int64{}
		for ar.Next() {
			var id int64
			if err := ar.Scan(&id); err != nil {
				ar.Close()
				return nil, err
			}
			out[i].AgentIDs = append(out[i].AgentIDs, id)
		}
		ar.Close()
	}
	return out, nil
}

// CountStageTasks: cate task-uri sunt acum in etapa.
func (s *Store) CountStageTasks(stageID int64) (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM tasks WHERE stage_id = ?`, stageID).Scan(&n)
	return n, err
}

// SaveStages inlocuieste etapele proiectului cu lista data, in ordinea ei.
// Etapele cu ID se actualizeaza, cele fara ID se creeaza, iar cele care lipsesc
// se sterg doar daca nu au task-uri (altfel StageInUseError si nu se schimba nimic).
// Validarea numelor si a agentilor o face apelantul.
func (s *Store) SaveStages(projectID int64, stages []Stage) ([]Stage, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	old, err := listStages(tx, projectID)
	if err != nil {
		return nil, err
	}
	keep := map[int64]bool{}
	for _, st := range stages {
		if st.ID != 0 {
			keep[st.ID] = true
		}
	}
	for _, o := range old {
		if keep[o.ID] {
			continue
		}
		var n int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM tasks WHERE stage_id = ?`, o.ID).Scan(&n); err != nil {
			return nil, err
		}
		if n > 0 {
			return nil, StageInUseError(o.Name)
		}
		if _, err := tx.Exec(`DELETE FROM stages WHERE id = ?`, o.ID); err != nil {
			return nil, err
		}
	}
	for i := range stages {
		st := &stages[i]
		st.ProjectID, st.Position = projectID, i
		if st.ID != 0 {
			if err := mustAffect(tx.Exec(`UPDATE stages SET position = ?, name = ?, kind = ? WHERE id = ? AND project_id = ?`,
				st.Position, st.Name, st.Kind, st.ID, projectID)); err != nil {
				return nil, err
			}
		} else {
			res, err := tx.Exec(`INSERT INTO stages (project_id, position, name, kind) VALUES (?, ?, ?, ?)`,
				projectID, st.Position, st.Name, st.Kind)
			if err != nil {
				return nil, err
			}
			if st.ID, err = res.LastInsertId(); err != nil {
				return nil, err
			}
		}
		if _, err := tx.Exec(`DELETE FROM stage_agents WHERE stage_id = ?`, st.ID); err != nil {
			return nil, err
		}
		for _, aid := range st.AgentIDs {
			if _, err := tx.Exec(`INSERT OR IGNORE INTO stage_agents (stage_id, agent_id) VALUES (?, ?)`, st.ID, aid); err != nil {
				return nil, err
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.ListStages(projectID)
}
```

`tasks.go`:
- adauga campurile `StageID *int64 \`json:"stageId"\`` si `ByUser bool \`json:"byUser"\`` in `Task`;
- adauga `, stage_id, by_user` la `taskCols` si `&t.StageID, &t.ByUser` la scan;
- `CreateTask` insereaza `stage_id, by_user` (`nullPtr(t.StageID), t.ByUser`);
- `SaveTask` salveaza si `assignee_id = ?, stage_id = ?`.

`store_test.go`: `user_version` asteptat devine 5.

- [ ] **Step 4: Run to pass**

Run: `go test -race ./internal/store/ ./internal/team/ ./internal/web/`
Expected: PASS (proiectele fara etape nu se schimba).

- [ ] **Step 5: Commit**

`git commit -m "store: etape (stages, stage_agents) si stage_id/by_user pe task"`

---

### Task 2: MCP (StageKind)

**Files:** `internal/mcp/server.go`, `internal/mcp/tools.go`, `internal/mcp/server_test.go`

**Interfaces (Produces):** `Caller.StageKind string`, cu valorile `""`, `"work"` sau `"check"`.

- [ ] **Step 1: Test** (in `server_test.go`)
  - adauga caller-ele `"stagework": {ProjectID: 1, AgentID: 6, TaskID: 9, StageKind: "work"}` si `"stagecheck": {ProjectID: 1, AgentID: 7, TaskID: 9, StageKind: "check"}`;
  - adauga testul de mai jos.

```go
func TestStageTools(t *testing.T) {
	_, ts := setup(t)
	names := func(token string) string {
		_, resp := rpc(t, ts, token, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)
		var out []string
		for _, tool := range result(t, resp)["tools"].([]any) {
			out = append(out, tool.(map[string]any)["name"].(string))
		}
		return strings.Join(out, ",")
	}
	if got := names("stagework"); got != "report_done" {
		t.Fatalf("work: %s", got)
	}
	if got := names("stagecheck"); got != "report_done,review_approve,review_reject" {
		t.Fatalf("check: %s", got)
	}
}
```

- [ ] **Step 2: Run to fail.** Run: `go test ./internal/mcp/`. Expected: FAIL (`unknown field StageKind`).

- [ ] **Step 3: Implement**
  - adauga `StageKind string // etapa task-ului curent: "work", "check" sau "" (fara etape)` in `Caller`;
  - in `toolsFor`, dupa blocurile de rol:

```go
	// un run pe un task dintr-o etapa primeste tool-urile etapei, fara dubluri
	has := func(name string) bool {
		for _, t := range out {
			if t.Name == name {
				return true
			}
		}
		return false
	}
	if c.StageKind != "" && !has(reportTool.Name) {
		out = append(out, reportTool)
	}
	if c.StageKind == "check" && !has("review_approve") {
		out = append(out, reviewTools...)
	}
```

- [ ] **Step 4: Run to pass.** Run: `go test -race ./internal/mcp/`
- [ ] **Step 5: Commit.** `git commit -m "mcp: tool-urile etapei (StageKind)"`

---

### Task 3: team (creare si preluare in etape)

**Files:**
- Create: `internal/team/stages.go`, `internal/team/stages_test.go`
- Modify: `internal/team/tools.go` (`CreateTask`), `internal/team/reconcile.go` (bucla queued + `turnContext`), `internal/team/run.go` (`Caller.StageKind`, `IsLead` pentru radacini), `internal/prompt/prompt.go` (`TurnContext.Stage`)

**Interfaces:**
- Consumes: `store.Stage`, `ListStages`, `Task.StageID/ByUser`, `Caller.StageKind`.
- Produces:
  - `func (t *Team) UserCreateTask(projectID int64, title, description string) (store.Task, error)`;
  - `var ErrNoStages = errors.New("task-uri din board se pot crea doar in proiectele cu etape")`;
  - in `stages.go`, helper-ele `stageIndex`, `nextStage`, `backStage`, `placeholder`, `stageLine`.

- [ ] **Step 1: Test** (`internal/team/stages_test.go`)

```go
package team

import (
	"strings"
	"testing"

	"foreman/internal/runner"
	"foreman/internal/store"
)

// stagesFixture: Lead; Dev = Rook + Wes (lucru), QA = Vera (verificare).
func stagesFixture(t *testing.T) (*fixture, store.Agent, []store.Stage) {
	f := setup(t)
	wes := f.agent(t, "Wes", &f.lead, nil)
	stages, err := f.st.SaveStages(f.p.ID, []store.Stage{
		{Name: "Dev", Kind: store.KindWork, AgentIDs: []int64{f.rook.ID, wes.ID}},
		{Name: "QA", Kind: store.KindCheck, AgentIDs: []int64{f.vera.ID}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return f, wes, stages
}

func TestStageCreateAndFirstFree(t *testing.T) {
	f, wes, stages := stagesFixture(t)
	lc := f.leadCaller()
	for _, title := range []string{"A", "B"} {
		if _, err := f.tm.CreateTask(lc, title, "", ""); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.tm.CreateTask(lc, "C", "", "Vera"); err == nil || !strings.Contains(err.Error(), "nu e in etapa Dev") {
		t.Fatalf("assignee din alta etapa: %v", err)
	}
	mustReconcile(t, f)
	rook1, wes1 := f.fs.last(t, f.rook), f.fs.last(t, wes)
	if !strings.Contains(turn(rook1), "Task T-1: A") || !strings.Contains(turn(wes1), "Task T-2: B") {
		t.Fatalf("primul liber: %q / %q", turn(rook1), turn(wes1))
	}
	if !strings.Contains(turn(rook1), "Etapa: Dev (lucru)") || !strings.Contains(turn(rook1), "merge la QA") {
		t.Fatalf("linia etapei: %q", turn(rook1))
	}
	if c := f.caller(t, rook1); c.StageKind != store.KindWork {
		t.Fatalf("StageKind = %q", c.StageKind)
	}
	task := f.wantStatus(t, 2, store.StatusInProgress)
	if task.AssigneeID != wes.ID || task.StageID == nil || *task.StageID != stages[0].ID {
		t.Fatalf("T-2: %+v", task)
	}
	_ = runner.Outcome{}
}

func TestUserCreateTaskNeedsStages(t *testing.T) {
	f := setup(t)
	if _, err := f.tm.UserCreateTask(f.p.ID, "x", ""); err != ErrNoStages {
		t.Fatalf("fara etape: %v", err)
	}
	f2, _, stages := stagesFixture(t)
	task, err := f2.tm.UserCreateTask(f2.p.ID, "Login", "cu parola")
	if err != nil || !task.ByUser || task.CreatorID != nil || task.StageID == nil || *task.StageID != stages[0].ID {
		t.Fatalf("%+v %v", task, err)
	}
}

func TestStageWithoutAgentsWaits(t *testing.T) {
	f := setup(t)
	if _, err := f.st.SaveStages(f.p.ID, []store.Stage{{Name: "Gol", Kind: store.KindWork}}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.tm.CreateTask(f.leadCaller(), "A", "", ""); err != nil {
		t.Fatal(err)
	}
	n := f.fs.count()
	mustReconcile(t, f)
	f.wantStatus(t, 1, store.StatusQueued)
	if f.fs.count() != n {
		t.Fatal("nu trebuia pornit niciun run")
	}
}

// intr-un proiect cu etape, liderul radacina fara copii tot poate crea task-uri
func TestRootWithoutChildrenCreatesInStages(t *testing.T) {
	f := setup(t)
	solo := f.agent(t, "Solo", nil, nil)
	if _, err := f.st.SaveStages(f.p.ID, []store.Stage{{Name: "Dev", Kind: store.KindWork, AgentIDs: []int64{f.rook.ID}}}); err != nil {
		t.Fatal(err)
	}
	f.tm.UserMessage(solo.ID, "fa ceva", nil)
	mustReconcile(t, f)
	c := f.caller(t, f.fs.last(t, solo))
	if !c.IsLead {
		t.Fatal("radacina dintr-un proiect cu etape trebuia sa primeasca create_task")
	}
	if _, err := f.tm.CreateTask(c, "A", "", ""); err != nil {
		t.Fatal(err)
	}
}
```

Verifica ca `mustReconcile`, `turn`, `f.fs.last`, `f.fs.count`, `f.wantStatus` si `f.agent` exista in fisierele de test ale pachetului `team` (le foloseste deja `reconcile_test.go`). Nu le duplica.

- [ ] **Step 2: Run to fail.** Run: `go test ./internal/team/ -run Stage`. Expected: FAIL (`UserCreateTask undefined`).

- [ ] **Step 3: Implement**

`internal/prompt/prompt.go`:
- in `TurnContext`, adauga `Stage string // linia etapei pentru un run pe task dintr-o etapa`;
- in `Context`, pune `c.Stage` primul in `parts` cand nu e gol.

`internal/team/stages.go`:

```go
package team

import (
	"errors"
	"fmt"

	"foreman/internal/store"
)

var ErrNoStages = errors.New("task-uri din board se pot crea doar in proiectele cu etape")

// stageIndex intoarce pozitia etapei id in lista (-1 daca nu exista).
func stageIndex(stages []store.Stage, id *int64) int {
	if id == nil {
		return -1
	}
	for i, s := range stages {
		if s.ID == *id {
			return i
		}
	}
	return -1
}

// nextStage intoarce etapa de dupa cea curenta; false daca era ultima.
func nextStage(stages []store.Stage, cur *int64) (store.Stage, bool) {
	i := stageIndex(stages, cur)
	if i < 0 || i+1 >= len(stages) {
		return store.Stage{}, false
	}
	return stages[i+1], true
}

// backStage: unde intoarce o respingere din etapa cur: cea mai apropiata
// etapa de lucru dinainte, altfel prima etapa.
func backStage(stages []store.Stage, cur *int64) store.Stage {
	for i := stageIndex(stages, cur) - 1; i >= 0; i-- {
		if stages[i].Kind == store.KindWork {
			return stages[i]
		}
	}
	return stages[0]
}

// placeholder alege asignatul "loc tinut" cand task-ul intra intr-o etapa:
// primul agent al etapei, altfel fallback. Scheduler-ul il inlocuieste cu
// primul agent liber inainte sa porneasca run-ul.
func placeholder(st store.Stage, fallback int64) int64 {
	if len(st.AgentIDs) > 0 {
		return st.AgentIDs[0]
	}
	return fallback
}

// stageLine e linia din tura agentului care lucreaza intr-o etapa.
func stageLine(stages []store.Stage, task store.Task) string {
	i := stageIndex(stages, task.StageID)
	if i < 0 {
		return ""
	}
	st := stages[i]
	next := "liderul, care decide"
	if n, ok := nextStage(stages, task.StageID); ok {
		next = n.Name
	}
	if st.Kind == store.KindCheck {
		return fmt.Sprintf("Etapa: %s (verificare). Verifica munca (citeste fisierele, nu doar rezumatul) si apeleaza "+
			"mcp__team__review_approve daca e buna (merge la %s) sau mcp__team__review_reject cu feedback concret.", st.Name, next)
	}
	return fmt.Sprintf("Etapa: %s (lucru). Cand termini, apeleaza mcp__team__report_done cu rezumatul; task-ul merge la %s.", st.Name, next)
}

// UserCreateTask: task creat de utilizator din board, in prima etapa. La
// final decide tot utilizatorul (ByUser).
func (t *Team) UserCreateTask(projectID int64, title, description string) (store.Task, error) {
	t.work.Lock()
	defer t.work.Unlock()
	stages, err := t.st.ListStages(projectID)
	if err != nil {
		return store.Task{}, err
	}
	if len(stages) == 0 {
		return store.Task{}, ErrNoStages
	}
	agents, err := t.st.ListAgents(projectID)
	if err != nil || len(agents) == 0 {
		return store.Task{}, fmt.Errorf("proiectul nu are agenti: %v", err)
	}
	first := stages[0]
	task := store.Task{ProjectID: projectID, Title: title, Description: description, ByUser: true,
		StageID: &first.ID, AssigneeID: placeholder(first, agents[0].ID)}
	if err := t.st.CreateTask(&task); err != nil {
		return store.Task{}, err
	}
	t.emit(projectID, "task", task)
	t.Kick(projectID)
	return task, nil
}
```

`tools.go`, in `CreateTask`:
- dupa `ListAgents`, citeste `stages, err := t.st.ListStages(c.ProjectID)`;
- daca `len(stages) > 0`, apeleaza `t.createStageTask(c, agents, stages, title, description, assignee)` inainte de verificarea `children`, si intoarce rezultatul.

```go
// createStageTask: intr-un proiect cu etape task-ul intra in prima etapa;
// assignee e optional si trebuie sa fie din acea etapa. Poate crea orice lider
// sau radacina (etapele decid cine lucreaza). Se apeleaza cu t.work blocat.
func (t *Team) createStageTask(c mcp.Caller, agents []store.Agent, stages []store.Stage, title, description, assignee string) (string, error) {
	lead, ok := store.FindAgent(agents, c.AgentID)
	if !ok {
		return "", errors.New("agentul tau nu mai exista")
	}
	roles := store.RolesOf(lead, agents)
	if !roles.IsLead && !roles.IsRoot {
		return "", errors.New("doar un lider sau un agent care raporteaza utilizatorului poate crea task-uri")
	}
	title = strings.TrimSpace(title)
	if title == "" {
		return "", errors.New("title e obligatoriu")
	}
	first := stages[0]
	who := placeholder(first, lead.ID)
	if name := strings.TrimSpace(assignee); name != "" {
		w, err := t.st.FindAgentByName(c.ProjectID, name)
		if err != nil || !slices.Contains(first.AgentIDs, w.ID) {
			return "", fmt.Errorf("%q nu e in etapa %s (prima etapa); lasa assignee gol ca sa-l ia primul liber", name, first.Name)
		}
		who = w.ID
	}
	parentID, _, err := t.parentTaskFor(c)
	if err != nil {
		return "", err
	}
	task := store.Task{ProjectID: c.ProjectID, Title: title, Description: strings.TrimSpace(description),
		AssigneeID: who, CreatorID: &lead.ID, ParentTaskID: parentID, StageID: &first.ID}
	if err := t.st.CreateTask(&task); err != nil {
		return "", err
	}
	t.note(c.ProjectID, lead.ID, fmt.Sprintf("A creat %s in etapa %s: %s", task.Ref(), first.Name, title), true)
	t.emit(c.ProjectID, "task", task)
	t.Kick(c.ProjectID)
	return fmt.Sprintf("Creat %s in etapa %s. Il ia primul agent liber din etapa.", task.Ref(), first.Name), nil
}
```

Importa `slices` in `tools.go`.

`reconcile.go`, bucla „1. task-urile din Queued”:
- la inceputul lui `Reconcile`, citeste `stages, err := t.st.ListStages(projectID)`;
- in bucla, pentru un task cu `StageID != nil`, inlocuieste blocul `a, ok := byID[task.AssigneeID] ...` cu alegerea agentului:

```go
		var a store.Agent
		if task.StageID != nil {
			i := stageIndex(stages, task.StageID)
			if i < 0 {
				continue // etapa a disparut (nu se poate: stergerea e refuzata)
			}
			// primul agent liber din etapa; asignatul de acum are prioritate
			// (a fost ales la create_task sau il continua dupa o respingere)
			ids := append([]int64{task.AssigneeID}, stages[i].AgentIDs...)
			found := false
			for _, id := range ids {
				cand, ok := byID[id]
				if ok && slices.Contains(stages[i].AgentIDs, id) && !t.isActive(id) && !full(cand) {
					a, found = cand, true
					break
				}
			}
			if !found {
				continue
			}
			task.AssigneeID = a.ID
		} else {
			hasQueued[task.AssigneeID] = true
			var ok bool
			a, ok = byID[task.AssigneeID]
			if !ok || t.isActive(a.ID) || full(a) {
				continue
			}
		}
		hasQueued[a.ID] = true
```

Restul buclei ramane cum e. `SaveTask` salveaza acum si `assignee_id`. Pune linia etapei in context:

```go
		ctx := t.turnContext(a, agents, tasks, task.ID)
		ctx.Stage = stageLine(stages, *task)
		turn := prompt.TaskTurn(ctx, *task, subtaskLines(task.ID, tasks, byID), t.texts(p.ID, msgs))
```

Nota: linia existenta `hasQueued[task.AssigneeID] = true` se muta in ramura `else`, ca in codul de mai sus. Pentru task-urile din etape se marcheaza doar agentul ales.

`run.go`, in `startRun`, inainte de a construi `mcp.Caller`:

```go
	stageKind := ""
	stages, err := t.st.ListStages(p.ID)
	if err != nil {
		return err
	}
	if taskID != 0 {
		if task, err := t.st.GetTask(taskID); err == nil {
			if i := stageIndex(stages, task.StageID); i >= 0 {
				stageKind = stages[i].Kind
			}
		}
	}
	// intr-un proiect cu etape, radacinile creeaza task-uri chiar fara copii
	isLead := roles.IsLead || (len(stages) > 0 && roles.IsRoot)
```

In `Caller`, foloseste `IsLead: isLead` si adauga `StageKind: stageKind`.

- [ ] **Step 4: Run to pass**

Run: `go test -race ./internal/team/ ./internal/prompt/`
Expected: PASS, inclusiv testele vechi.

- [ ] **Step 5: Commit.** `git commit -m "team: task-uri in etape (creare, primul agent liber, linia etapei)"`

---

### Task 4: team (trecerea intre etape si decizia finala)

**Files:**
- Modify: `internal/team/tools.go` (`ReportDone`, `reviewTarget`/`ReviewApprove`/`ReviewReject`, `toReview`, `AcceptTask`, `SendBack`, `decider`), `internal/team/run.go` (`recordOutcome`, cazul failed), `internal/team/reconcile.go` (`resolveBlocked` accept: `StageID` nil), `internal/team/stages.go` (`advance`, `UserDecide`)
- Test: `internal/team/stages_test.go`

**Interfaces (Produces):**
- `func (t *Team) UserDecide(taskID int64, accept bool, feedback string) error`;
- `var ErrNotUserDecision = errors.New("task-ul nu asteapta decizia ta")`.

- [ ] **Step 1: Test**

```go
func TestStageFullFlow(t *testing.T) {
	f, _, stages := stagesFixture(t)
	if _, err := f.tm.CreateTask(f.leadCaller(), "Login", "", "Rook"); err != nil {
		t.Fatal(err)
	}
	mustReconcile(t, f)
	rook1 := f.fs.last(t, f.rook)
	if _, err := f.tm.ReportDone(f.caller(t, rook1), "", "login facut"); err != nil {
		t.Fatal(err)
	}
	task := f.wantStatus(t, 1, store.StatusQueued)
	if task.StageID == nil || *task.StageID != stages[1].ID || task.Result != "login facut" {
		t.Fatalf("dupa Dev: %+v", task)
	}
	f.fs.finish(rook1.ID, runner.Outcome{Status: "done"})
	mustReconcile(t, f)
	vera1 := f.fs.last(t, f.vera)
	vc := f.caller(t, vera1)
	if vc.StageKind != store.KindCheck || !strings.Contains(turn(vera1), "Etapa: QA (verificare)") {
		t.Fatalf("QA: %+v %q", vc, turn(vera1))
	}
	// respinge -> inapoi in Dev, cu feedback
	if _, err := f.tm.ReviewReject(vc, "T-1", "lipseste validarea"); err != nil {
		t.Fatal(err)
	}
	task = f.wantStatus(t, 1, store.StatusQueued)
	if *task.StageID != stages[0].ID || task.Feedback != "lipseste validarea" || task.SendBacks != 1 {
		t.Fatalf("dupa respingere: %+v", task)
	}
	f.fs.finish(vera1.ID, runner.Outcome{Status: "done"})
	mustReconcile(t, f)
	rook2 := f.fs.last(t, f.rook)
	if !strings.Contains(turn(rook2), "lipseste validarea") {
		t.Fatalf("feedback in tura: %q", turn(rook2))
	}
	f.tm.ReportDone(f.caller(t, rook2), "", "validare adaugata")
	f.fs.finish(rook2.ID, runner.Outcome{Status: "done"})
	mustReconcile(t, f)
	vera2 := f.fs.last(t, f.vera)
	if _, err := f.tm.ReviewApprove(f.caller(t, vera2), "T-1", "ok"); err != nil {
		t.Fatal(err)
	}
	task = f.wantStatus(t, 1, store.StatusReview)
	if task.StageID != nil || task.ReviewStage != store.StageLead {
		t.Fatalf("la lider: %+v", task)
	}
	f.fs.finish(vera2.ID, runner.Outcome{Status: "done"})
	mustReconcile(t, f)
	lead := f.fs.last(t, f.lead)
	if _, err := f.tm.AcceptTask(f.caller(t, lead), "T-1", ""); err != nil {
		t.Fatal(err)
	}
	f.wantStatus(t, 1, store.StatusDone)
}

func TestStageRejectLimitBlocks(t *testing.T) {
	f, _, stages := stagesFixture(t)
	f.tm.CreateTask(f.leadCaller(), "A", "", "")
	for i := 0; i < MaxSendBacks; i++ {
		// il aducem direct in QA, in lucru la Vera
		task, _ := f.st.GetTaskByNumber(f.p.ID, 1)
		task.StageID, task.AssigneeID, task.Status = &stages[1].ID, f.vera.ID, store.StatusInProgress
		f.st.SaveTask(&task)
		c := f.workerCaller(f.vera, task.ID)
		c.StageKind = store.KindCheck
		if _, err := f.tm.ReviewReject(c, "T-1", "nu"); err != nil {
			t.Fatal(err)
		}
	}
	task := f.wantStatus(t, 1, store.StatusBlocked)
	if task.StageID == nil || *task.StageID != stages[1].ID {
		t.Fatalf("blocat in QA: %+v", task)
	}
}

func TestUserTaskDecidedByUser(t *testing.T) {
	f, _, stages := stagesFixture(t)
	task, _ := f.tm.UserCreateTask(f.p.ID, "A", "")
	// sarim direct la finalul etapelor
	task.StageID, task.Status, task.ReviewStage = nil, store.StatusReview, store.StageLead
	f.st.SaveTask(&task)
	if id := decisionOwner(task, mustAgents(t, f)); id != 0 {
		t.Fatalf("un task al utilizatorului nu il decide agentul %d", id)
	}
	if err := f.tm.UserDecide(task.ID, false, ""); err == nil {
		t.Fatal("trimis inapoi fara feedback trebuia refuzat")
	}
	if err := f.tm.UserDecide(task.ID, false, "mai incearca"); err != nil {
		t.Fatal(err)
	}
	back := f.wantStatus(t, 1, store.StatusQueued)
	if back.StageID == nil || *back.StageID != stages[0].ID || back.Feedback != "mai incearca" {
		t.Fatalf("%+v", back)
	}
	back.StageID, back.Status, back.ReviewStage = nil, store.StatusReview, store.StageLead
	f.st.SaveTask(&back)
	if err := f.tm.UserDecide(back.ID, true, ""); err != nil {
		t.Fatal(err)
	}
	f.wantStatus(t, 1, store.StatusDone)
}

func TestStageRunWithoutReportGoesToLead(t *testing.T) {
	f, _, stages := stagesFixture(t)
	f.tm.CreateTask(f.leadCaller(), "A", "", "Rook")
	mustReconcile(t, f)
	rook1 := f.fs.last(t, f.rook)
	f.fs.finish(rook1.ID, runner.Outcome{Status: "done", Result: "am uitat de raport"})
	task := f.wantStatus(t, 1, store.StatusReview)
	if task.StageID == nil || *task.StageID != stages[0].ID || task.ReviewStage != store.StageLead {
		t.Fatalf("%+v", task)
	}
	mustReconcile(t, f)
	lead := f.fs.last(t, f.lead)
	if _, err := f.tm.SendBack(f.caller(t, lead), "T-1", "raporteaza"); err != nil {
		t.Fatal(err)
	}
	if back := f.wantStatus(t, 1, store.StatusQueued); *back.StageID != stages[0].ID {
		t.Fatalf("%+v", back)
	}
}
```

Adauga helper-ul `mustAgents` in `stages_test.go`:

```go
func mustAgents(t *testing.T, f *fixture) []store.Agent {
	agents, err := f.st.ListAgents(f.p.ID)
	if err != nil {
		t.Fatal(err)
	}
	return agents
}
```

- [ ] **Step 2: Run to fail.** Run: `go test ./internal/team/ -run 'Stage|UserTask'`. Expected: FAIL.

- [ ] **Step 3: Implement**

In `stages.go`, adauga:

```go
var ErrNotUserDecision = errors.New("task-ul nu asteapta decizia ta")

// advance muta un task (cu Result pus) in etapa urmatoare, sau la lider dupa
// ultima etapa. Se apeleaza cu t.work blocat. Intoarce textul pentru apelant.
func (t *Team) advance(task *store.Task, stages []store.Stage, agents []store.Agent, byName string) (string, error) {
	task.Feedback, task.ReviewStage = "", store.StageNone
	if next, ok := nextStage(stages, task.StageID); ok {
		task.StageID = &next.ID
		task.Status = store.StatusQueued
		task.AssigneeID = placeholder(next, task.AssigneeID)
		if err := t.st.SaveTask(task); err != nil {
			return "", err
		}
		t.emit(task.ProjectID, "task", *task)
		t.Kick(task.ProjectID)
		return fmt.Sprintf("%s a trecut in etapa %s.", task.Ref(), next.Name), nil
	}
	task.StageID = nil
	task.Status, task.ReviewStage = store.StatusReview, store.StageLead
	if err := t.st.SaveTask(task); err != nil {
		return "", err
	}
	t.emit(task.ProjectID, "task", *task)
	if boss, ok := decider(*task, agents); ok {
		t.note(task.ProjectID, boss.ID, clip(fmt.Sprintf("%s a trecut prin toate etapele (ultimul: %s). Decide cu accept_task sau send_back: %s", task.Ref(), byName, task.Result)), false)
	}
	t.Kick(task.ProjectID)
	return fmt.Sprintf("%s a trecut prin toate etapele si asteapta decizia liderului.", task.Ref()), nil
}

// sendToStage pune un task inapoi, Queued, in etapa st, cu feedback (numara
// trimiterea; la MaxSendBacks se blocheaza in etapa curenta). Se apeleaza cu t.work blocat.
func (t *Team) sendToStage(task *store.Task, st store.Stage, feedback string, agents []store.Agent, by int64) (bool, error) {
	if task.SendBacks+1 < MaxSendBacks {
		task.StageID = &st.ID
		task.AssigneeID = placeholder(st, task.AssigneeID)
	}
	return t.rejectTask(task, feedback, agents, by)
}

// UserDecide: utilizatorul decide un task al lui (creat din board) ajuns la final.
func (t *Team) UserDecide(taskID int64, accept bool, feedback string) error {
	t.work.Lock()
	defer t.work.Unlock()
	task, err := t.st.GetTask(taskID)
	if err != nil {
		return err
	}
	agents, err := t.st.ListAgents(task.ProjectID)
	if err != nil {
		return err
	}
	if task.Status != store.StatusReview || task.ReviewStage != store.StageLead || decisionOwner(task, agents) != 0 {
		return ErrNotUserDecision
	}
	if accept {
		task.Status, task.ReviewStage, task.StageID = store.StatusDone, store.StageNone, nil
		if err := t.st.SaveTask(&task); err != nil {
			return err
		}
		t.emit(task.ProjectID, "task", task)
		t.Kick(task.ProjectID)
		return nil
	}
	feedback = strings.TrimSpace(feedback)
	if feedback == "" {
		return errors.New("scrie ce trebuie schimbat")
	}
	stages, err := t.st.ListStages(task.ProjectID)
	if err != nil || len(stages) == 0 {
		return ErrNoStages
	}
	target := stages[0]
	if i := stageIndex(stages, task.StageID); i >= 0 {
		target = stages[i]
	}
	if _, err := t.sendToStage(&task, target, feedback, agents, 0); err != nil {
		return err
	}
	t.Kick(task.ProjectID)
	return nil
}
```

Importa `strings` in `stages.go`. Apoi, in `tools.go`:

1. **`decider`:** la inceput, `if task.ByUser { return store.Agent{}, false } // decide utilizatorul`.

2. **`ReportDone`:**
   - muta verificarea `worker.ParentID == nil` dupa gasirea task-ului si pastreaz-o doar pentru `task.StageID == nil`;
   - dupa verificarile de asignat si status (si inainte de sub-task-uri), adauga:

```go
	if task.StageID != nil {
		stages, err := t.st.ListStages(c.ProjectID)
		if err != nil {
			return "", err
		}
		task.Result = summary
		t.note(c.ProjectID, worker.ID, "A terminat "+task.Ref()+" in etapa lui", true)
		return t.advance(&task, stages, agents, worker.Name)
	}
```

   Ordinea verificarilor: summary gol → eroare; gaseste task-ul; `AssigneeID == worker.ID`; `Status == in_progress`; ramura etapei de mai sus; abia apoi regula `ParentID == nil` si sub-task-urile, ca azi.

3. **Verificarea in etape:** o functie noua `stageCheckTarget(c, ref)` intoarce `(task, stages, agents, err)` si cere:
   - `task.StageID != nil`;
   - etapa sa fie `check`;
   - `task.AssigneeID == c.AgentID`;
   - `task.Status == in_progress`.

   In `ReviewApprove` si `ReviewReject`, inainte de `reviewTarget`: gaseste task-ul cu `findTask`; daca `task.StageID != nil`, mergi pe ramura etapei:
   - **approve:** `t.advance(&task, stages, agents, reviewer.Name)`;
   - **reject:** feedback obligatoriu, apoi `blocked, err := t.sendToStage(&task, backStage(stages, task.StageID), feedback, agents, c.AgentID)`. Daca `blocked`, intoarce `blockedReply`; altfel `fmt.Sprintf("%s trimis inapoi in etapa %s (%d/%d).", ...)`.

4. **`toReview`:** la inceput, pentru task-urile din etape (run terminat fara raport):

```go
	if task.StageID != nil {
		task.Status, task.ReviewStage = store.StatusReview, store.StageLead
		if err := t.st.SaveTask(task); err != nil {
			return store.Agent{}, err
		}
		t.emit(task.ProjectID, "task", *task)
		boss, ok := decider(*task, agents)
		if ok {
			t.note(boss.ProjectID, boss.ID, clip(fmt.Sprintf("%s (review): %s", headline, task.Result)), false)
		}
		return boss, nil
	}
```

5. **`AcceptTask`:** la `done`, seteaza si `task.StageID = nil`.

6. **`SendBack`:** dupa verificari, daca proiectul are etape:
   - tinta e etapa `task.StageID`, daca e setata si exista; altfel prima etapa;
   - `blocked, err := t.sendToStage(&task, target, feedback, agents, lead.ID)`;
   - notele: „A trimis inapoi %s in etapa %s: %s”;
   - raspunsul: „%s trimis inapoi in etapa %s (%d/%d).”.

7. **`run.go`, `recordOutcome`:**
   - in ramura `RunFailed`, blocarea `!hasBoss` se aplica doar pentru `task.StageID == nil && !task.ByUser`;
   - un task din etape fara decident ramane `review`/`lead` si il decide utilizatorul.

8. **`reconcile.go`, `resolveBlocked`:** la accept, seteaza si `task.StageID = nil`.

- [ ] **Step 4: Run to pass**

Run: `go vet ./... && go test -race ./internal/team/`
Expected: PASS, inclusiv toate testele vechi.

- [ ] **Step 5: Commit.** `git commit -m "team: trecerea intre etape, respingere, decizia finala (lider sau utilizator)"`

---

### Task 5: web

**Files:**
- Create: `internal/web/api_stages.go`, `internal/web/stages_test.go`
- Modify: `internal/web/api_projects.go` (`projectDetail.Stages`), `internal/web/server.go` (rute), `internal/web/api_tasks.go` (erori noi → 400/409)

**Interfaces (Produces):**
- rutele `PUT /api/projects/{id}/stages`, `POST /api/projects/{id}/tasks` si `POST /api/tasks/{id}/decide`;
- evenimentul SSE `stages`;
- `projectDetail.Stages []store.Stage \`json:"stages"\``.

- [ ] **Step 1: Test** (`internal/web/stages_test.go`)

```go
package web

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"foreman/internal/store"
)

func TestStagesAPI(t *testing.T) {
	e := setup(t)
	p := e.createProject(t, t.TempDir())
	root := e.root(t, p.ID)
	path := "/api/projects/" + itoa(p.ID)

	// fara etape: POST tasks -> 400
	if code, _ := e.do(t, "POST", path+"/tasks", map[string]any{"title": "x"}); code != 400 {
		t.Fatalf("task fara etape: %d", code)
	}
	bad := []map[string]any{
		{"stages": []map[string]any{{"name": "", "kind": "work"}}},
		{"stages": []map[string]any{{"name": "Dev", "kind": "work"}, {"name": "dev", "kind": "work"}}},
		{"stages": []map[string]any{{"name": "Dev", "kind": "altceva"}}},
		{"stages": []map[string]any{{"name": "Dev", "kind": "work", "agentIds": []int64{99999}}}},
	}
	for _, b := range bad {
		if code, raw := e.do(t, "PUT", path+"/stages", b); code != 400 {
			t.Errorf("%v: %d %s", b, code, raw)
		}
	}
	code, raw := e.do(t, "PUT", path+"/stages", map[string]any{"stages": []map[string]any{
		{"name": "Dev", "kind": "work", "agentIds": []int64{root.ID}},
		{"name": "QA", "kind": "check"},
	}})
	var saved []store.Stage
	json.Unmarshal(raw, &saved)
	if code != 200 || len(saved) != 2 {
		t.Fatalf("salvare: %d %s", code, raw)
	}
	code, raw = e.do(t, "POST", path+"/tasks", map[string]any{"title": "Login", "description": "d"})
	if code != http.StatusCreated {
		t.Fatalf("task: %d %s", code, raw)
	}
	// detaliul contine etapele si stageId
	_, raw = e.do(t, "GET", path, nil)
	if !strings.Contains(string(raw), `"stages":[{`) || !strings.Contains(string(raw), `"stageId":`) {
		t.Fatalf("detaliu: %s", raw)
	}
	// stergerea etapei cu task -> 409
	if code, raw := e.do(t, "PUT", path+"/stages", map[string]any{"stages": []map[string]any{{"id": saved[1].ID, "name": "QA", "kind": "check"}}}); code != 409 || !strings.Contains(string(raw), "Dev") {
		t.Fatalf("409: %d %s", code, raw)
	}
	// decide pe un task care nu asteapta decizia utilizatorului -> 409
	var task store.Task
	tasks, _ := e.st.ListTasks(p.ID)
	task = tasks[0]
	if code, _ := e.do(t, "POST", "/api/tasks/"+itoa(task.ID)+"/decide", map[string]any{"action": "accept"}); code != 409 {
		t.Fatalf("decide: %d", code)
	}
}
```

- [ ] **Step 2: Run to fail.** Run: `go test ./internal/web/ -run Stages`. Expected: FAIL (404 pe rute).

- [ ] **Step 3: Implement** (`api_stages.go`)

```go
package web

import (
	"errors"
	"net/http"
	"strings"

	"foreman/internal/store"
	"foreman/internal/team"
)

func (s *Server) saveStages(w http.ResponseWriter, r *http.Request) {
	pid, err := pathID(r)
	if err != nil {
		fail(w, err)
		return
	}
	if _, err := s.st.GetProject(pid); err != nil {
		fail(w, err)
		return
	}
	var in struct {
		Stages []store.Stage `json:"stages"`
	}
	if err := decode(r, &in); err != nil {
		fail(w, err)
		return
	}
	agents, err := s.st.ListAgents(pid)
	if err != nil {
		fail(w, err)
		return
	}
	inProject := map[int64]bool{}
	for _, a := range agents {
		inProject[a.ID] = true
	}
	if len(in.Stages) > 12 {
		fail(w, badRequest("maxim 12 etape"))
		return
	}
	seen := map[string]bool{}
	for i := range in.Stages {
		st := &in.Stages[i]
		st.Name = strings.TrimSpace(st.Name)
		if st.Name == "" {
			fail(w, badRequest("fiecare etapa are nevoie de nume"))
			return
		}
		if err := maxLen("numele etapei", st.Name, 40); err != nil {
			fail(w, err)
			return
		}
		key := strings.ToLower(st.Name)
		if seen[key] {
			fail(w, badRequest("doua etape cu numele "+st.Name))
			return
		}
		seen[key] = true
		if st.Kind != store.KindWork && st.Kind != store.KindCheck {
			fail(w, badRequest("tip de etapa necunoscut: "+st.Kind))
			return
		}
		for _, id := range st.AgentIDs {
			if !inProject[id] {
				fail(w, badRequest("un agent ales nu e in acest proiect"))
				return
			}
		}
	}
	saved, err := s.st.SaveStages(pid, in.Stages)
	var inUse store.StageInUseError
	if errors.As(err, &inUse) {
		writeErr(w, http.StatusConflict, inUse.Error())
		return
	}
	if err != nil {
		fail(w, err)
		return
	}
	s.hub.Publish(pid, "stages", map[string]int64{"id": pid})
	s.team.Kick(pid)
	writeJSON(w, http.StatusOK, saved)
}

func (s *Server) createUserTask(w http.ResponseWriter, r *http.Request) {
	pid, err := pathID(r)
	if err != nil {
		fail(w, err)
		return
	}
	var in struct {
		Title       string `json:"title"`
		Description string `json:"description"`
	}
	if err := decode(r, &in); err != nil {
		fail(w, err)
		return
	}
	title := strings.TrimSpace(in.Title)
	if title == "" {
		fail(w, badRequest("titlul e obligatoriu"))
		return
	}
	for _, err := range []error{maxLen("titlul", title, 200), maxLen("descrierea", in.Description, 10000)} {
		if err != nil {
			fail(w, err)
			return
		}
	}
	task, err := s.team.UserCreateTask(pid, title, strings.TrimSpace(in.Description))
	if errors.Is(err, team.ErrNoStages) {
		fail(w, badRequest(err.Error()))
		return
	}
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, task)
}

func (s *Server) decideTask(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		fail(w, err)
		return
	}
	var in struct {
		Action   string `json:"action"`
		Feedback string `json:"feedback"`
	}
	if err := decode(r, &in); err != nil {
		fail(w, err)
		return
	}
	if in.Action != "accept" && in.Action != "send_back" {
		fail(w, badRequest("action trebuie sa fie accept sau send_back"))
		return
	}
	err = s.team.UserDecide(id, in.Action == "accept", in.Feedback)
	switch {
	case errors.Is(err, team.ErrNotUserDecision):
		writeErr(w, http.StatusConflict, err.Error())
	case err != nil && strings.Contains(err.Error(), "scrie ce trebuie"):
		fail(w, badRequest(err.Error()))
	case err != nil:
		fail(w, err)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}
```

Rute in `server.go`:
- `PUT /api/projects/{id}/stages` → `s.saveStages`;
- `POST /api/projects/{id}/tasks` → `s.createUserTask`;
- `POST /api/tasks/{id}/decide` → `s.decideTask`.

In `getProject`, citeste `stages, err := s.st.ListStages(p.ID)` si pune-l in `projectDetail{..., Stages: stages}` (camp nou, tag `json:"stages"`).

Verifica titlul maxim din `CreateTask`-ul MCP (sau din store) si foloseste aceeasi limita. Daca nu exista una, 200 e ok.

- [ ] **Step 4: Run to pass.** Run: `gofmt -l internal; go vet ./... && go test -race ./...`
- [ ] **Step 5: Commit.** `git commit -m "web: etape (PUT stages), task-uri din board, decizia utilizatorului"`

---

### Task 6: UI (editor de coloane si board-ul cu etape)

**Files:**
- Create: `internal/web/static/js/stages.js`
- Modify: `internal/web/static/js/workspace.js` (`renderBoard`, `taskCard`, header-ul board-ului, evenimentul `stages`), `internal/web/static/app.css`

**Interfaces:** consuma `project.stages` (`[{id, name, kind, position, agentIds}]`), `task.stageId`, `task.byUser` si rutele din Task 5.

- [ ] **Step 1: `stages.js`**

```js
import { api } from "./api.js";
import { el, toast } from "./ui.js";

// stagesEditor: panoul in care utilizatorul isi face coloanele (etapele).
// Lucreaza pe o copie; Salveaza trimite toata lista (PUT /stages).
export function stagesEditor({ project, onSaved, onClose }) {
  const busy = new Set(project.tasks.filter((t) => t.stageId).map((t) => t.stageId)); // etape cu task-uri
  let list = project.stages.map((s) => ({ ...s, agentIds: [...s.agentIds] }));
  const rows = el("div", { class: "stage-rows" });
  const box = el("div", { class: "stages-editor section" },
    el("div", { class: "section-head" },
      el("h2", {}, "Coloane (etape de lucru)"),
      el("span", { class: "hint" }, "Task-ul trece prin etape in ordine; il ia primul agent liber din etapa. Dupa ultima, decide liderul.")),
    rows,
    el("div", { class: "actions" },
      el("button", { type: "button", class: "btn", onclick: () => { list.push({ name: "", kind: "work", agentIds: [] }); draw(); } }, "+ Etapa"),
      el("span", { class: "grow" }),
      el("button", { type: "button", class: "btn", onclick: onClose }, "Renunta"),
      el("button", { type: "button", class: "btn primary", onclick: save }, "Salveaza")));

  function draw() {
    rows.replaceChildren(...list.map((s, i) => {
      const name = el("input", { type: "text", value: s.name, maxlength: 40, placeholder: "ex. Dev", oninput: (e) => { s.name = e.target.value; } });
      const kind = el("select", { value: s.kind, onchange: (e) => { s.kind = e.target.value; } },
        el("option", { value: "work" }, "Lucru"), el("option", { value: "check" }, "Verificare"));
      const agents = project.agents.map((a) => el("label", { class: "check" },
        el("input", { type: "checkbox", checked: s.agentIds.includes(a.id), onchange: (e) => {
          s.agentIds = e.target.checked ? [...s.agentIds, a.id] : s.agentIds.filter((x) => x !== a.id);
        } }), a.name));
      const move = (d) => { const j = i + d; if (j < 0 || j >= list.length) return; [list[i], list[j]] = [list[j], list[i]]; draw(); };
      const locked = s.id && busy.has(s.id);
      return el("div", { class: "stage-row" },
        el("div", { class: "stage-row-head" },
          el("span", { class: "stage-num" }, String(i + 1)), name, kind,
          el("button", { type: "button", class: "btn small", "aria-label": "Mai sus", disabled: i === 0, onclick: () => move(-1) }, "↑"),
          el("button", { type: "button", class: "btn small", "aria-label": "Mai jos", disabled: i === list.length - 1, onclick: () => move(1) }, "↓"),
          el("button", { type: "button", class: "btn small danger", "aria-label": "Sterge etapa", disabled: locked,
            title: locked ? "are task-uri" : "Sterge etapa", onclick: () => { list.splice(i, 1); draw(); } }, "×")),
        el("div", { class: "stage-agents" }, agents.length ? agents : el("span", { class: "hint" }, "Proiectul nu are agenti.")));
    }));
    if (!list.length) rows.append(el("p", { class: "hint" }, "Nicio etapa: board-ul foloseste coloanele implicite (In asteptare, In lucru, Review, Gata)."));
  }

  async function save() {
    try {
      await api(`/api/projects/${project.id}/stages`, { method: "PUT", body: {
        stages: list.map((s) => ({ id: s.id, name: s.name.trim(), kind: s.kind, agentIds: s.agentIds })),
      } });
      toast("Coloane salvate.", "ok");
      onSaved();
    } catch (err) {
      toast(err.message);
    }
  }

  draw();
  return box;
}
```

- [ ] **Step 2: `workspace.js`, board-ul**
  - importa `stagesEditor` din `./stages.js`;
  - in sectiunea Task-uri, header-ul devine:

```js
  const boardTools = el("div", { class: "actions" });
  const editorSlot = el("div");
  // ... in bodies["task-uri"]:
  //   el("div", { class: "section-head" }, el("h2", {}, "Task-uri"), count, boardTools),
  //   editorSlot, board
```

In `renderBoard()`:

```js
  function renderBoard() {
    const p = state.project;
    const byId = new Map(p.agents.map((a) => [a.id, a]));
    const stages = p.stages || [];
    count.textContent = `${p.done} din ${p.total} gata`;
    boardTools.replaceChildren(
      stages.length > 0 && el("button", { type: "button", class: "btn small", onclick: newTask }, "+ Task"),
      el("button", { type: "button", class: "btn small", onclick: openStages }, "Coloane"));
    if (!stages.length) {
      board.className = "board";
      board.replaceChildren(...COLUMNS.map((c) => column(c.title, p.tasks.filter((t) => c.statuses.includes(t.status)), byId)));
      boardHint.hidden = false;
      return;
    }
    boardHint.hidden = true;
    board.className = "board stages-board";
    const inStage = (s) => p.tasks.filter((t) => t.stageId === s.id && t.status !== "review" && t.status !== "done");
    board.replaceChildren(
      ...stages.map((s) => column(s.name, inStage(s), byId, s)),
      column("La lider", p.tasks.filter((t) => t.status === "review"), byId),
      column("Gata", p.tasks.filter((t) => t.status === "done"), byId));
  }

  function column(title, items, byId, stage) {
    const agents = stage ? stage.agentIds.map((id) => byId.get(id)).filter(Boolean) : [];
    return el("div", { class: "col" },
      el("div", { class: "col-head" },
        el("span", {}, title, stage?.kind === "check" && el("span", { class: "tag col-kind" }, "verificare")),
        el("span", {}, items.length)),
      stage && el("div", { class: "col-agents" },
        agents.length ? agents.map((a) => avatar(a.name, kindOf(a), 18)) : el("span", { class: "badge-blocked" }, "fara agenti")),
      items.map((t) => taskCard(t, byId.get(t.assigneeId), byId)));
  }
```

`boardHint` e randul discret de sub board:

```js
  const boardHint = el("p", { class: "hint board-hint" }, "Fiecare proiect e diferit: fa-ti propriile coloane (etape de lucru) din butonul Coloane.");
```

In `taskCard`:
- pentru un task `queued` cu `stageId`, afiseaza „in asteptare” in loc de avatar si nume;
- pentru un task `review` cu `byUser`, afiseaza butoanele **Accepta** si **Trimite inapoi** (cu un `prompt()` pentru feedback), prin `POST /api/tasks/{id}/decide`.

```js
    const waiting = t.stageId && t.status === "queued";
    const mine = t.status === "review" && t.byUser;
    // in summary: waiting ? el("span", { class: "hint" }, "in asteptare") : [who && avatar(...), who ? who.name : "?"]
    // in task-detail, dupa butoanele de blocat:
    mine && el("div", { class: "actions" },
      el("button", { type: "button", class: "btn small primary", onclick: () => decide(t, "accept") }, "Accepta"),
      el("button", { type: "button", class: "btn small", onclick: () => decide(t, "send_back") }, "Trimite inapoi"))
```

```js
  async function decide(t, action) {
    let feedback = "";
    if (action === "send_back") {
      feedback = prompt(`Ce trebuie schimbat la ${ref(t)}?`) || "";
      if (!feedback.trim()) return;
    }
    try {
      await api(`/api/tasks/${t.id}/decide`, { method: "POST", body: { action, feedback } });
      await reload();
    } catch (err) { toast(err.message); }
  }

  function openStages() {
    editorSlot.replaceChildren(stagesEditor({
      project: state.project,
      onSaved: async () => { editorSlot.replaceChildren(); await reload(); },
      onClose: () => editorSlot.replaceChildren(),
    }));
  }

  async function newTask() {
    const title = prompt("Titlul task-ului:");
    if (!title || !title.trim()) return;
    const description = prompt("Descriere (optional):") || "";
    try {
      await api(`/api/projects/${projectId}/tasks`, { method: "POST", body: { title: title.trim(), description } });
      await reload();
    } catch (err) { toast(err.message); }
  }
```

**Evenimentul `stages`:** handler-ul SSE deja cheama `scheduleReload()` la orice eveniment, deci nu mai e nimic de facut.

- [ ] **Step 3: CSS**

```css
/* ---------- etape (coloane proprii) ---------- */
.board.stages-board { grid-template-columns: none; grid-auto-flow: column; grid-auto-columns: minmax(220px, 1fr); overflow-x: auto; padding-bottom: 6px; }
.col-agents { display: flex; gap: 4px; flex-wrap: wrap; padding: 0 4px; }
.col-kind { margin-left: 6px; text-transform: none; letter-spacing: 0; }
.board-hint { margin: 8px 0 0; }
.stages-editor { margin-bottom: 12px; }
.stage-rows { display: grid; gap: 10px; }
.stage-row { border: 1px solid var(--line); border-radius: 10px; padding: 10px; display: grid; gap: 8px; background: var(--card-2); }
.stage-row-head { display: flex; gap: 8px; align-items: center; }
.stage-row-head input { flex: 1; min-width: 0; }
.stage-row-head select { width: auto; }
.stage-num { font-family: var(--mono); color: var(--muted); width: 18px; text-align: center; }
.stage-agents { display: flex; flex-wrap: wrap; gap: 12px; }
```

Scoate media query-urile care pun `.board` pe 2 sau 1 coloana pentru `.stages-board`: board-ul cu etape ramane mereu pe scroll orizontal. De exemplu, adauga `.board.stages-board { grid-template-columns: none !important; }` dupa ele. Evita `!important` daca selectorul mai specific ajunge.

- [ ] **Step 4: Verificare**
  - ruleaza `node --check` pe toate fisierele JS si reporneste serverul;
  - fa un proiect de test si ruleaza un script CDP (ca in memoria „verify-ui-with-screenshots”) la 1440×900 si 1280×720:
    - fara etape: board-ul implicit si randul cu indemnul;
    - „Coloane”: adaugi Dev (lucru, un agent) si QA (verificare), apoi Salveaza; board-ul arata Dev, QA, La lider, Gata;
    - „+ Task” (prompt-ul se inlocuieste in CDP cu `window.prompt = () => "Login"`): task-ul apare in Dev;
    - stergerea lui Dev in editor e dezactivata (are task-uri);
  - citeste capturile si sterge proiectul de test.

- [ ] **Step 5: Commit.** `git commit -m "UI: coloane proprii (editor de etape, board cu etape, task-uri si decizii din board)"`

---

### Task 7: e2e real si documentatie

- [ ] **Step 1: e2e cu haiku**
  - prin API, fa un proiect nou (folder temporar in scratchpad) cu Lead (radacina), Dev1 (sub Lead, tool `write`) si QA1 (sub Lead);
  - salveaza etapele Dev = [Dev1] (lucru) si QA = [QA1] (verificare);
  - schimba modelele pe haiku cu `PUT /api/agents/{id}`;
  - trimite lead-ului mesajul „Creeaza un fisier hello.txt cu textul salut. Un singur task.”;
  - urmareste `GET /api/projects/{id}` pana cand T-1 e `done` sau trec 10 minute.

  Expected: T-1 trece Dev → QA → La lider → Gata, iar `hello.txt` exista. Daca se opreste undeva, citeste log-urile run-urilor si repara.

- [ ] **Step 2:** Sterge proiectul de test si folderul lui.

- [ ] **Step 3: CLAUDE.md.** In „Stare”, adauga:

```
Gata (coloane proprii, spec in docs/superpowers/specs/2026-09-30-foreman-stages-design.md): etape de lucru per
proiect (lucru/verificare, agenti per etapa, primul agent liber ia task-ul); dupa ultima etapa decide liderul
(sau utilizatorul pentru task-urile create din board). Proiectele fara etape merg ca inainte.
```

La „Urmeaza” punctul 4, scoate „Modurile collab/pipeline”, pentru ca etapele acopera pipeline-ul.

- [ ] **Step 4: Commit.** `git commit -m "docs: coloane proprii in CLAUDE.md"`
