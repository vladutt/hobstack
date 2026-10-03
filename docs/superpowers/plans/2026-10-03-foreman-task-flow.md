# Flow (drumul unui task): plan de implementare

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Tab-ul "Flow" din modala de task arata tot arborele de delegare al task-ului (de la radacina), cu pasii fiecarui task in ordine si sub-task-urile intercalate unde au fost create, plus pasul nou "requested" (mesajul utilizatorului din chat care a pornit task-ul).

**Architecture:** Store: `TaskFlow(taskID)` construieste arborele din `tasks.parent_task_id` + `task_events` (fara schema noua). Web: `GET /api/tasks/{id}/flow`. Team: run-urile de chat retin in memorie ultimul mesaj al utilizatorului livrat, iar `create_task` din acel run adauga evenimentul `requested`. UI: fisier nou `flow.js` + al patrulea tab in `task-modal.js`.

**Tech Stack:** Go (stdlib), SQLite (modernc.org/sqlite), vanilla JS.

**Spec:** `docs/superpowers/specs/2026-10-03-foreman-task-flow-design.md`

## Global Constraints

- `go vet ./...`, `go build -race ./cmd/foreman`, `go test ./...` trec; cross-compile `GOOS=darwin GOARCH=arm64 go build ./cmd/foreman` si `GOOS=windows go build ./cmd/foreman`.
- JS verificat ca modul: `cp f.js $TMP/f.mjs && node --check $TMP/f.mjs`. `cond ? x : null` pentru copii dati lui `el()`; `replaceChildren`/`append` native nu aplatizeaza array-uri si scriu `null` ca text (filtreaza).
- SQLite cu o singura conexiune: nicio alta interogare cat timp un `*sql.Rows` e deschis (`TaskFlow` cheama `ListTaskEvents` / `Subtasks`, care isi inchid singure rows inainte sa intoarca).
- Team: tot ce schimba starea ruleaza sub `t.work`; campurile din `t.active` se citesc/scriu sub `t.mu`.
- Texte vizibile in engleza, exact ca in spec ("Asked in chat", "No delegation: this task was done by one agent.", "Show", "Hide"); comentarii in romana fara diacritice; dependente noi: niciuna.
- Fara schimbari de schema (nicio migrare noua).
- Textul venit de la agenti / utilizator se pune doar prin `el()` sau `renderMarkdown` (niciodata HTML).

## Review Focus

1. **Deschis dintr-un sub-task adanc:** se vede tot arborele de la radacina, cu nodul curent marcat. Test in Task 1 (`TestTaskFlowFromSubtask`).
2. **Ordinea pasilor:** `requested` (ora mesajului) apare inaintea lui `created` desi e salvat dupa el. Test in Task 1 (`TestTaskFlowSortsByTime`) si Task 2 (asertiunea pe `At`).
3. **Date stricate (ciclu in `parent_task_id`)** nu blocheaza serverul. Test in Task 1 (`TestTaskFlowCycle`).
4. **Run de chat fara mesajul utilizatorului** (doar note de la agenti) nu inventeaza un `requested`. Test in Task 2 (`TestRequestedOnlyWithUserMessage`).
5. **Refresh live cat tab-ul e deschis** nu inchide pasii desfasurati si nu sare scroll-ul. Verificat in browser in Task 3 (pas desfasurat, apoi un eveniment SSE).

---

### Task 1: Store `TaskFlow` + ruta web

**Files:**
- Create: `internal/store/flow.go`, `internal/store/flow_test.go`
- Modify: `internal/web/server.go` (ruta), `internal/web/api_tasks.go` (handler)
- Test: `internal/web/flow_test.go`

**Interfaces:**
- Produces:
  - `store.FlowStep{At time.Time; Kind, Actor, To, Text, Body string}` (JSON `at, kind, actor, to, text, body`)
  - `store.FlowNode{ID int64; Number int; Title, Status, Assignee string; CreatedAt time.Time; DoneAt *time.Time; Steps []FlowStep; Children []FlowNode}` (JSON `id, number, title, status, assignee, createdAt, doneAt, steps, children`; `steps` si `children` mereu non-nil)
  - `store.Flow{FocusID int64; Root FlowNode}` (JSON `focusId, root`)
  - `func (s *Store) TaskFlow(taskID int64) (Flow, error)` (`ErrNotFound` pentru task inexistent)
  - `GET /api/tasks/{id}/flow` -> 200 `Flow`, 404 pentru task inexistent.

- [ ] **Step 1: Testele store** (`flow_test.go`, `package store`):

```go
package store

import (
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func flowFixture(t *testing.T) (*Store, Project, Agent, Agent) {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	p := Project{Name: "P", Workdir: t.TempDir(), MaxParallel: 1}
	lead, err := s.CreateProject(&p)
	if err != nil {
		t.Fatal(err)
	}
	dev := Agent{ProjectID: p.ID, Name: "Dev", ParentID: &lead.ID, Model: "haiku"}
	if err := s.CreateAgent(&dev); err != nil {
		t.Fatal(err)
	}
	return s, p, lead, dev
}

func mkTask(t *testing.T, s *Store, p Project, who Agent, title string, parent *Task) Task {
	t.Helper()
	task := Task{ProjectID: p.ID, Title: title, AssigneeID: who.ID}
	if parent != nil {
		task.ParentTaskID = &parent.ID
	}
	if err := s.CreateTask(&task); err != nil {
		t.Fatal(err)
	}
	return task
}

func event(t *testing.T, s *Store, task Task, at time.Time, kind, actor, to, text, body string) {
	t.Helper()
	if err := s.AddTaskEvent(&TaskEvent{TaskID: task.ID, At: at, Kind: kind, Actor: actor, To: to, Text: text, Body: body}); err != nil {
		t.Fatal(err)
	}
}

func TestTaskFlowFromSubtask(t *testing.T) {
	s, p, lead, dev := flowFixture(t)
	t0 := time.Date(2026, 10, 3, 9, 0, 0, 0, time.UTC)
	root := mkTask(t, s, p, lead, "Root", nil)
	mid := mkTask(t, s, p, dev, "Mid", &root)
	leaf := mkTask(t, s, p, dev, "Leaf", &mid)
	other := mkTask(t, s, p, dev, "Other", &root)
	event(t, s, root, t0, "created", "Foreman", lead.Name, "Created by Foreman", "brief")
	event(t, s, mid, t0.Add(time.Minute), "created", lead.Name, "Dev", "Created by "+lead.Name, "")
	event(t, s, leaf, t0.Add(2*time.Minute), "created", "Dev", "Dev", "Created by Dev", "")
	event(t, s, root, t0.Add(10*time.Minute), "accepted", "Foreman", "", "Foreman accepted", "")
	root.Status = StatusDone
	if err := s.SaveTask(&root); err != nil {
		t.Fatal(err)
	}

	f, err := s.TaskFlow(leaf.ID)
	if err != nil {
		t.Fatal(err)
	}
	if f.FocusID != leaf.ID || f.Root.ID != root.ID || f.Root.Assignee != lead.Name {
		t.Fatalf("radacina / focus: %+v", f)
	}
	if len(f.Root.Children) != 2 || f.Root.Children[0].ID != mid.ID || f.Root.Children[1].ID != other.ID {
		t.Fatalf("copiii radacinii in ordinea numarului: %+v", f.Root.Children)
	}
	if kids := f.Root.Children[0].Children; len(kids) != 1 || kids[0].ID != leaf.ID || kids[0].Children == nil {
		t.Fatalf("nepotul: %+v", kids)
	}
	st := f.Root.Steps
	if len(st) != 2 || st[0].Kind != "created" || st[0].To != lead.Name || st[0].Body != "brief" {
		t.Fatalf("pasii radacinii: %+v", st)
	}
	if f.Root.DoneAt == nil || !f.Root.DoneAt.Equal(t0.Add(10*time.Minute)) {
		t.Fatalf("doneAt: %v", f.Root.DoneAt)
	}
	if f.Root.Children[0].DoneAt != nil {
		t.Fatalf("task nefinalizat fara doneAt: %v", f.Root.Children[0].DoneAt)
	}
}

func TestTaskFlowSortsByTime(t *testing.T) {
	s, p, lead, _ := flowFixture(t)
	t0 := time.Date(2026, 10, 3, 9, 0, 0, 0, time.UTC)
	task := mkTask(t, s, p, lead, "T", nil)
	event(t, s, task, t0.Add(time.Minute), "created", lead.Name, "", "Created", "")
	event(t, s, task, t0, "requested", "You", lead.Name, "Asked in chat", "please do it") // salvat dupa, dar mai devreme
	f, err := s.TaskFlow(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Root.Steps) != 2 || f.Root.Steps[0].Kind != "requested" || f.Root.Steps[1].Kind != "created" {
		t.Fatalf("ordinea dupa ora: %+v", f.Root.Steps)
	}
}

func TestTaskFlowWithoutEvents(t *testing.T) {
	s, p, lead, _ := flowFixture(t)
	task := mkTask(t, s, p, lead, "Old", nil)
	f, err := s.TaskFlow(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Root.Steps) != 1 || f.Root.Steps[0].Kind != "created" || f.Root.Steps[0].Text != "Created" || !f.Root.Steps[0].At.Equal(task.CreatedAt) {
		t.Fatalf("pas sintetic: %+v", f.Root.Steps)
	}
	if f.Root.Children == nil {
		t.Fatal("children trebuie sa fie non-nil")
	}
	if _, err := s.TaskFlow(99999); !errors.Is(err, ErrNotFound) {
		t.Fatalf("inexistent: %v", err)
	}
}

func TestTaskFlowCycle(t *testing.T) {
	s, p, lead, dev := flowFixture(t)
	a := mkTask(t, s, p, lead, "A", nil)
	b := mkTask(t, s, p, dev, "B", &a)
	// date stricate: A devine copilul lui B
	if _, err := s.db.Exec(`UPDATE tasks SET parent_task_id = ? WHERE id = ?`, b.ID, a.ID); err != nil {
		t.Fatal(err)
	}
	done := make(chan Flow, 1)
	go func() {
		f, err := s.TaskFlow(a.ID)
		if err != nil {
			t.Error(err)
		}
		done <- f
	}()
	select {
	case f := <-done:
		if f.FocusID != a.ID {
			t.Fatalf("%+v", f)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("TaskFlow a intrat in bucla pe un ciclu")
	}
}
```

- [ ] **Step 2:** `go test ./internal/store/ -run TaskFlow` -> FAIL (`TaskFlow` nu exista).

- [ ] **Step 3: `flow.go`**:

```go
package store

import (
	"errors"
	"sort"
	"time"
)

// FlowStep e un pas din drumul unui task (un eveniment din istoric).
type FlowStep struct {
	At    time.Time `json:"at"`
	Kind  string    `json:"kind"`
	Actor string    `json:"actor"`
	To    string    `json:"to"`
	Text  string    `json:"text"`
	Body  string    `json:"body"`
}

// FlowNode e un task din arborele de delegare, cu pasii si sub-task-urile lui.
type FlowNode struct {
	ID        int64      `json:"id"`
	Number    int        `json:"number"`
	Title     string     `json:"title"`
	Status    string     `json:"status"`
	Assignee  string     `json:"assignee"`
	CreatedAt time.Time  `json:"createdAt"`
	DoneAt    *time.Time `json:"doneAt"` // ultimul "accepted"/"user_accepted", doar pentru Done
	Steps     []FlowStep `json:"steps"`
	Children  []FlowNode `json:"children"`
}

// Flow e arborele task-ului cerut, pornit de la radacina lui.
type Flow struct {
	FocusID int64    `json:"focusId"`
	Root    FlowNode `json:"root"`
}

// TaskFlow construieste arborele din care face parte task-ul: urca pe
// parent_task_id pana la radacina, apoi coboara prin sub-task-uri. Un ciclu
// (date stricate) sau un parinte sters opreste urcarea.
func (s *Store) TaskFlow(taskID int64) (Flow, error) {
	task, err := s.GetTask(taskID)
	if err != nil {
		return Flow{}, err
	}
	root := task
	seen := map[int64]bool{task.ID: true}
	for root.ParentTaskID != nil {
		p, err := s.GetTask(*root.ParentTaskID)
		if errors.Is(err, ErrNotFound) {
			break
		}
		if err != nil {
			return Flow{}, err
		}
		if seen[p.ID] {
			break
		}
		seen[p.ID] = true
		root = p
	}
	agents, err := s.ListAgents(task.ProjectID)
	if err != nil {
		return Flow{}, err
	}
	names := make(map[int64]string, len(agents))
	for _, a := range agents {
		names[a.ID] = a.Name
	}
	node, err := s.flowNode(root, names, map[int64]bool{})
	if err != nil {
		return Flow{}, err
	}
	return Flow{FocusID: taskID, Root: node}, nil
}

// flowNode: nodul unui task si, recursiv, al sub-task-urilor; visited opreste ciclurile.
func (s *Store) flowNode(t Task, names map[int64]string, visited map[int64]bool) (FlowNode, error) {
	visited[t.ID] = true
	n := FlowNode{ID: t.ID, Number: t.Number, Title: t.Title, Status: t.Status, Assignee: names[t.AssigneeID],
		CreatedAt: t.CreatedAt, Steps: []FlowStep{}, Children: []FlowNode{}}
	events, err := s.ListTaskEvents(t.ID)
	if err != nil {
		return FlowNode{}, err
	}
	for _, e := range events {
		n.Steps = append(n.Steps, FlowStep{At: e.At, Kind: e.Kind, Actor: e.Actor, To: e.To, Text: e.Text, Body: e.Body})
	}
	if len(n.Steps) == 0 { // task vechi, fara istoric: un pas sintetic (ca in getTask)
		n.Steps = append(n.Steps, FlowStep{At: t.CreatedAt, Kind: "created", Text: "Created"})
	}
	// dupa ora; la egalitate ramane ordinea inregistrarii ("requested" are ora mesajului)
	sort.SliceStable(n.Steps, func(i, j int) bool { return n.Steps[i].At.Before(n.Steps[j].At) })
	if t.Status == StatusDone {
		for i := len(n.Steps) - 1; i >= 0; i-- {
			if k := n.Steps[i].Kind; k == "accepted" || k == "user_accepted" {
				at := n.Steps[i].At
				n.DoneAt = &at
				break
			}
		}
	}
	subs, err := s.Subtasks(t.ID)
	if err != nil {
		return FlowNode{}, err
	}
	for _, sub := range subs {
		if visited[sub.ID] {
			continue
		}
		child, err := s.flowNode(sub, names, visited)
		if err != nil {
			return FlowNode{}, err
		}
		n.Children = append(n.Children, child)
	}
	return n, nil
}
```

`ListTaskEvents` si `Subtasks` isi inchid `rows` inainte sa intoarca, deci recursia nu tine niciun `*sql.Rows` deschis.

- [ ] **Step 4:** `go test ./internal/store/ -run TaskFlow` -> PASS.

- [ ] **Step 5: Ruta web.** Test (`internal/web/flow_test.go`), cu helper-ele din `server_test.go` (`setup`, `e.do`, `e.createProject`, `e.root`, `itoa`, `e.st`):

```go
package web

import (
	"encoding/json"
	"net/http"
	"testing"

	"foreman/internal/store"
)

func TestTaskFlowAPI(t *testing.T) {
	e := setup(t)
	p := e.createProject(t, t.TempDir())
	root := e.root(t, p.ID)
	parent := store.Task{ProjectID: p.ID, Title: "Parent", AssigneeID: root.ID}
	if err := e.st.CreateTask(&parent); err != nil {
		t.Fatal(err)
	}
	child := store.Task{ProjectID: p.ID, Title: "Child", AssigneeID: root.ID, ParentTaskID: &parent.ID}
	if err := e.st.CreateTask(&child); err != nil {
		t.Fatal(err)
	}
	code, raw := e.do(t, "GET", "/api/tasks/"+itoa(child.ID)+"/flow", nil)
	var f store.Flow
	if err := json.Unmarshal(raw, &f); err != nil || code != http.StatusOK {
		t.Fatalf("%d %s %v", code, raw, err)
	}
	if f.FocusID != child.ID || f.Root.ID != parent.ID || len(f.Root.Children) != 1 || f.Root.Children[0].ID != child.ID {
		t.Fatalf("%s", raw)
	}
	if code, _ := e.do(t, "GET", "/api/tasks/99999/flow", nil); code != http.StatusNotFound {
		t.Fatalf("inexistent: %d", code)
	}
}
```

Handler in `api_tasks.go` (langa `getTaskChanges`):

```go
// getTaskFlow intoarce arborele de delegare al task-ului (tab-ul Flow).
func (s *Server) getTaskFlow(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		fail(w, err)
		return
	}
	f, err := s.st.TaskFlow(id)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, f)
}
```

Ruta in `server.go`, dupa `GET /api/tasks/{id}/changes`: `mux.HandleFunc("GET /api/tasks/{id}/flow", s.getTaskFlow)`.

- [ ] **Step 6:** `go test ./internal/store/ ./internal/web/` si `go vet ./...` -> PASS. Commit: `store+web: arborele de delegare al unui task (flow)`

---

### Task 2: Team: pasul `requested`

**Files:**
- Modify: `internal/team/team.go` (`activeRun`), `internal/team/reconcile.go` (`notifyRun`, ~124-142), `internal/team/events.go` (`taskMsg` -> `taskMsgAt`), `internal/team/tools.go` (`CreateTask` ~65-70, `createStageTask` ~105-110)
- Test: `internal/team/flow_test.go`

**Interfaces:**
- Consumes: nimic din Task 1 (independent).
- Produces: evenimentul `task_events` cu `Kind = "requested"`, `Actor = "You"`, `To` = numele liderului care a creat task-ul, `Text = "Asked in chat"`, `Body` = mesajul utilizatorului, `At` = `Message.CreatedAt`.

- [ ] **Step 1: Testele** (`flow_test.go`, `package team`), cu fixture-ul `setup(t)` din `fake_test.go` (lead radacina + Rook, Vera sub el), `mustReconcile`, `f.fs.last`, `f.caller`:

```go
package team

import (
	"testing"

	"foreman/internal/runner"
	"foreman/internal/store"
)

func eventsOfKind(t *testing.T, f *fixture, taskID int64, kind string) []store.TaskEvent {
	t.Helper()
	evs, err := f.st.ListTaskEvents(taskID)
	if err != nil {
		t.Fatal(err)
	}
	var out []store.TaskEvent
	for _, e := range evs {
		if e.Kind == kind {
			out = append(out, e)
		}
	}
	return out
}

func TestRequestedFromChat(t *testing.T) {
	f := setup(t)
	m, err := f.tm.UserMessage(f.lead.ID, "add a share button", nil)
	if err != nil {
		t.Fatal(err)
	}
	mustReconcile(t, f)
	run := f.fs.last(t, f.lead)
	if _, err := f.tm.CreateTask(f.caller(t, run), "Share", "do it", "Rook"); err != nil {
		t.Fatal(err)
	}
	task := f.wantStatus(t, 1, store.StatusQueued)
	req := eventsOfKind(t, f, task.ID, "requested")
	if len(req) != 1 {
		t.Fatalf("vreau un requested, am %+v", req)
	}
	r := req[0]
	if r.Actor != "You" || r.To != f.lead.Name || r.Text != "Asked in chat" || r.Body != "add a share button" || !r.At.Equal(m.CreatedAt) {
		t.Fatalf("%+v", r)
	}
	created := eventsOfKind(t, f, task.ID, "created")
	if len(created) != 1 || r.At.After(created[0].At) {
		t.Fatalf("requested trebuie sa fie la ora mesajului, inainte de created: %+v %+v", r, created)
	}
	f.fs.finish(run.ID, runner.Outcome{Status: "done"})
}

func TestNoRequestedFromTaskRun(t *testing.T) {
	f := setup(t)
	wes := f.agent(t, "Wes", &f.rook, nil) // Rook devine sub-lider
	parent, err := f.tm.UserCreateTask(f.p.ID, "Big", "", f.rook.ID)
	if err != nil {
		t.Fatal(err)
	}
	mustReconcile(t, f)
	run := f.fs.last(t, f.rook) // run pe task
	if _, err := f.tm.CreateTask(f.caller(t, run), "Part", "", wes.Name); err != nil {
		t.Fatal(err)
	}
	sub := f.wantStatus(t, 2, store.StatusQueued)
	if sub.ParentTaskID == nil || *sub.ParentTaskID != parent.ID {
		t.Fatalf("sub-task: %+v", sub)
	}
	if req := eventsOfKind(t, f, sub.ID, "requested"); len(req) != 0 {
		t.Fatalf("un run pe task nu are requested: %+v", req)
	}
	f.fs.finish(run.ID, runner.Outcome{Status: "done"})
}

func TestRequestedOnlyWithUserMessage(t *testing.T) {
	f := setup(t)
	// doar o nota (nu mesaj al utilizatorului) in coada liderului
	if err := f.tm.note(f.p.ID, f.lead.ID, "Rook finished T-9", false); err != nil {
		t.Fatal(err)
	}
	mustReconcile(t, f)
	run := f.fs.last(t, f.lead)
	if _, err := f.tm.CreateTask(f.caller(t, run), "Follow-up", "", "Rook"); err != nil {
		t.Fatal(err)
	}
	task := f.wantStatus(t, 1, store.StatusQueued)
	if req := eventsOfKind(t, f, task.ID, "requested"); len(req) != 0 {
		t.Fatalf("fara mesaj al utilizatorului nu exista requested: %+v", req)
	}
	f.fs.finish(run.ID, runner.Outcome{Status: "done"})
}
```

Verifica in `internal/team/` ca `f.tm.note` are semnatura `(projectID, agentID int64, text string, delivered bool) error` (team.go:197) si ca `UserMessage` intoarce `(store.Message, error)` cu `CreatedAt` completat (reconcile.go:~568). Daca `CreatedAt` intors de `UserMessage` nu e cel din baza de date (rotunjiri), compara cu mesajul recitit prin `f.st.ListMessages(f.lead.ID)`.

- [ ] **Step 2:** `go test ./internal/team/ -run Requested` -> FAIL (nu exista evenimentul).

- [ ] **Step 3: Implementarea.**
  - `team.go`, `activeRun`:
    ```go
    type activeRun struct {
    	runID     string
    	projectID int64
    	taskID    int64
    	token     string
    	request   *store.Message // ultimul mesaj al utilizatorului livrat la pornire (doar run-urile de chat)
    }
    ```
    plus doua helper-e (sub `t.mu`):
    ```go
    // setRequest retine mesajul utilizatorului care a pornit run-ul activ al agentului.
    func (t *Team) setRequest(agentID int64, m store.Message) {
    	t.mu.Lock()
    	defer t.mu.Unlock()
    	if ar, ok := t.active[agentID]; ok {
    		ar.request = &m
    		t.active[agentID] = ar
    	}
    }

    // requestOf intoarce mesajul retinut pentru run-ul runID al agentului, daca e inca activ.
    func (t *Team) requestOf(agentID int64, runID string) (store.Message, bool) {
    	t.mu.Lock()
    	defer t.mu.Unlock()
    	ar, ok := t.active[agentID]
    	if !ok || ar.runID != runID || ar.request == nil {
    		return store.Message{}, false
    	}
    	return *ar.request, true
    }
    ```
  - `reconcile.go`, `notifyRun`, dupa `startRun` reusit si inainte de `t.delivered`:
    ```go
    		// ultimul mesaj al utilizatorului: din el pot porni task-uri (pasul "requested" din Flow)
    		for i := len(notes) - 1; i >= 0; i-- {
    			if notes[i].Author == store.AuthorUser {
    				t.setRequest(a.ID, notes[i])
    				break
    			}
    		}
    ```
  - `events.go`: `taskMsg` devine un apel catre
    ```go
    // taskMsgAt e taskMsg cu ora data (pasul "requested" are ora mesajului utilizatorului).
    func (t *Team) taskMsgAt(task store.Task, at time.Time, kind, from, to, text, body string) {
    ```
    cu corpul de azi al lui `taskMsg`, dar `At: at`; iar `taskMsg(...)` = `t.taskMsgAt(task, t.now(), kind, from, to, text, body)`. Adauga importul `time` daca lipseste.
  - `tools.go`: un helper chemat in `CreateTask` si `createStageTask`, imediat dupa `t.taskMsg(task, "created", ...)`:
    ```go
    // recordRequest: un task creat dintr-un run de chat primeste pasul "requested"
    // cu mesajul utilizatorului care a pornit run-ul. Se cheama cu t.work tinut.
    func (t *Team) recordRequest(c mcp.Caller, task store.Task, leadName string) {
    	if c.TaskID != 0 {
    		return // run pe task: task-ul vine din delegare, nu din chat
    	}
    	if m, ok := t.requestOf(c.AgentID, c.RunID); ok {
    		t.taskMsgAt(task, m.CreatedAt, "requested", actorUser, leadName, "Asked in chat", m.Text)
    	}
    }
    ```
    apelat cu `t.recordRequest(c, task, lead.Name)` in ambele functii.
- [ ] **Step 4:** `go test ./internal/team/`, `go test -race ./internal/team/`, `go vet ./...` -> PASS. Commit: `team: pasul requested (mesajul din chat care a pornit task-ul)`

---

### Task 3: UI: tab-ul Flow

**Files:**
- Create: `internal/web/static/js/flow.js`
- Modify: `internal/web/static/js/task-modal.js` (tab-uri, `applyTab`, `refresh`), `internal/web/static/app.css`, `CLAUDE.md` (Stare)

**Interfaces:**
- Consumes: `GET /api/tasks/{id}/flow` (Task 1): `{focusId, root: {id, number, title, status, assignee, createdAt, doneAt, steps: [{at, kind, actor, to, text, body}], children: [...]}}`; pasul `requested` (Task 2); din `ui.js`: `el`, `clock`; din `md.js`: `renderMarkdown`; din `api.js`: `api`.
- Produces: `export function flowView({ projectId })` -> `{ el, show(data), error(msg) }`.

- [ ] **Step 1: `flow.js`** (in stilul lui `changes.js`):

```js
import { el, clock } from "./ui.js";
import { renderMarkdown } from "./md.js";

// Etichetele starilor (ca in task-modal.js).
const STATUS = {
  queued: "Queued", in_progress: "In progress", review: "Review", blocked: "Blocked", done: "Done",
};

// Culoarea unui pas dupa tip.
const TONE = {
  accepted: "ok", user_accepted: "ok", approved: "ok",
  sent_back: "bad", rejected: "bad", blocked: "bad", failed: "bad",
  asked: "warn", requested: "user",
};

// when: "HH:MM" azi, altfel "Sep 30 14:02"
function when(iso) {
  const d = new Date(iso);
  if (d.toDateString() === new Date().toDateString()) return clock(iso);
  return `${d.toLocaleDateString("en-US", { month: "short", day: "numeric" })} ${clock(iso)}`;
}

// span: "42m", "2h 05m", "3d 4h"
function span(from, to) {
  const min = Math.max(0, Math.round((new Date(to) - new Date(from)) / 60000));
  if (min < 60) return `${min}m`;
  const h = Math.floor(min / 60);
  if (h < 24) return `${h}h ${String(min % 60).padStart(2, "0")}m`;
  return `${Math.floor(h / 24)}d ${h % 24}h`;
}

// flowView: tab-ul "Flow" al unui task. Pasii desfasurati (cheie: task + ora + tip)
// raman intre reincarcari. show(data) cu raspunsul GET /api/tasks/{id}/flow.
export function flowView({ projectId }) {
  const root = el("div", { class: "flow" }, el("p", { class: "hint" }, "Loading..."));
  const open = new Set();
  let focusId = null;

  function stepNode(task, s) {
    const key = `${task.id}|${s.at}|${s.kind}`;
    const body = s.body ? el("div", { class: "flow-body", hidden: !open.has(key) }) : null;
    let built = false;
    const build = () => { if (!built && body) { built = true; body.append(renderMarkdown(s.body)); } };
    if (body && open.has(key)) build();
    const btn = body ? el("button", { type: "button", class: "btn small flow-more", "aria-expanded": String(open.has(key)) },
      open.has(key) ? "Hide" : "Show") : null;
    btn?.addEventListener("click", () => {
      if (open.has(key)) open.delete(key); else open.add(key);
      build();
      body.hidden = !open.has(key);
      btn.textContent = open.has(key) ? "Hide" : "Show";
      btn.setAttribute("aria-expanded", String(open.has(key)));
    });
    return el("li", { class: `flow-step${TONE[s.kind] ? " tone-" + TONE[s.kind] : ""}` },
      el("time", { datetime: s.at, title: new Date(s.at).toLocaleString() }, when(s.at)),
      el("span", { class: "flow-who" }, s.actor || "?", s.to ? el("span", { class: "flow-arrow", "aria-hidden": "true" }, " → ") : null, s.to || null),
      el("span", { class: "flow-text" }, s.kind === "requested" ? "asked in chat" : s.text),
      btn, body);
  }

  // childAt: ora la care a fost creat sub-task-ul (pasul "created", altfel createdAt)
  const childAt = (c) => (c.steps.find((s) => s.kind === "created") || { at: c.createdAt }).at;

  function taskNode(task) {
    const focus = task.id === focusId;
    const title = [el("span", { class: "task-id" }, `T-${task.number}`), task.title];
    const head = focus
      ? el("div", { class: "flow-head" }, el("span", { class: "flow-title" }, title))
      : el("a", { class: "flow-head", href: `#/p/${projectId}/tasks/${task.id}` }, el("span", { class: "flow-title" }, title));
    head.append(...[
      el("span", { class: `pill pill-task-${task.status}` }, STATUS[task.status] || task.status),
      task.assignee ? el("span", { class: "hint" }, task.assignee) : null,
      task.doneAt ? el("span", { class: "hint" }, span(task.createdAt, task.doneAt)) : null,
    ].filter(Boolean));
    // pasii si sub-task-urile, intercalate dupa ora
    const items = [
      ...task.steps.map((s) => ({ at: s.at, node: () => stepNode(task, s) })),
      ...task.children.map((c) => ({ at: childAt(c), node: () => el("li", { class: "flow-child" }, taskNode(c)) })),
    ].sort((a, b) => new Date(a.at) - new Date(b.at));
    const list = el("ol", { class: "flow-steps" });
    for (const it of items) list.append(it.node());
    return el("section", { class: `flow-task${focus ? " focus" : ""}` }, head, list);
  }

  return {
    el: root,
    show(data) {
      focusId = data.focusId;
      const nodes = [taskNode(data.root)];
      if (!data.root.children.length) nodes.push(el("p", { class: "hint" }, "No delegation: this task was done by one agent."));
      root.replaceChildren(...nodes);
    },
    error(msg) {
      root.replaceChildren(el("p", { class: "hint" }, msg));
    },
  };
}
```

Atentie: `Array.prototype.sort` e stabil, deci la ore egale pasii raman inaintea copiilor in ordinea data.

- [ ] **Step 2: `task-modal.js`.**
  - import: `import { flowView } from "./flow.js";`
  - stare: `let flowSeq = 0;` si `const flow = flowView({ projectId });` langa `changes`.
  - pane si tab, dupa `chgPane`/`tabChg`:
    ```js
    const flowPane = el("div", { id: `flow-${taskId}`, role: "tabpanel", hidden: true }, flow.el);
    const tabFlow = el("button", { type: "button", class: "tab", role: "tab", "aria-controls": flowPane.id, onclick: () => setTab("flow") }, "Flow");
    ```
    adaugate in `modal-tabs` (dupa `tabChg`) si in `tabsSection` (dupa `chgPane`).
  - comentariul lui `tab`: `"conversation" | "history" | "changes" | "flow"`.
  - `applyTab`: `names`, `tabs`, `panes` primesc `"flow"`, `tabFlow`, `flowPane`; la final, dupa `syncChanges()`, `syncFlow();`.
  - functie noua:
    ```js
    // syncFlow: cat timp tab-ul e deschis, arborele se cere la fiecare desenare a modalei
    // (un sub-task se poate schimba fara ca task-ul curent sa se miste)
    async function syncFlow() {
      if (tab !== "flow" || !data) return;
      const my = ++flowSeq;
      try {
        const res = await api(`/api/tasks/${taskId}/flow`);
        if (destroyed || my !== flowSeq) return;
        flow.show(res);
      } catch (err) {
        if (destroyed || my !== flowSeq) return;
        flow.error(err.message);
      }
    }
    ```
    `applyTab()` e deja chemat din `render()` la fiecare refresh, deci `syncFlow` ruleaza la fiecare reincarcare cat tab-ul e deschis.
- [ ] **Step 3: `app.css`** (la final, comentariu `/* tab-ul Flow */`), doar tokenuri existente:
  ```css
  .flow { display: grid; gap: 10px; min-width: 0; }
  .flow-task { display: grid; gap: 6px; min-width: 0; border: 1px solid var(--line); border-radius: 9px; background: var(--card-2); padding: 8px 10px; }
  .flow-task.focus { border-color: var(--amber); }
  .flow-head { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; color: var(--text); text-decoration: none; font-weight: 600; min-width: 0; }
  a.flow-head:hover .flow-title { text-decoration: underline; }
  a.flow-head:focus-visible { outline: 2px solid var(--amber); outline-offset: 2px; border-radius: 4px; }
  .flow-title { min-width: 0; overflow-wrap: anywhere; }
  .flow-steps { list-style: none; margin: 0; padding: 0 0 0 10px; border-left: 2px solid var(--line); display: grid; gap: 4px; min-width: 0; }
  .flow-step { display: flex; flex-wrap: wrap; align-items: baseline; gap: 4px 8px; font-size: 12px; color: var(--soft); min-width: 0; }
  .flow-step time { color: var(--muted); font-family: var(--mono); font-variant-ligatures: none; }
  .flow-who { color: var(--text); font-weight: 600; }
  .flow-arrow { color: var(--muted); font-weight: 400; }
  .flow-text { min-width: 0; overflow-wrap: anywhere; }
  .flow-step.tone-ok .flow-text { color: var(--green); }
  .flow-step.tone-bad .flow-text { color: var(--red); }
  .flow-step.tone-warn .flow-text { color: var(--amber); }
  .flow-step.tone-user .flow-text { color: var(--soft); font-style: italic; }
  .flow-body { flex-basis: 100%; font-size: 13px; background: var(--bg); border: 1px solid var(--line); border-radius: 7px; padding: 8px 10px; min-width: 0; }
  .flow-body[hidden] { display: none; }
  .flow-child { list-style: none; min-width: 0; }
  @media (max-width: 560px) { .flow-steps { padding-left: 6px; } .flow-task { padding: 6px 8px; } }
  ```
- [ ] **Step 4:** `node --check` pe copiile `.mjs` ale lui `flow.js` si `task-modal.js`; `go build ./cmd/foreman`.
- [ ] **Step 5: Verificare in browser** (copie izolata: HOME separat cu `sqlite3 ~/.foreman/foreman.db ".backup …"`, port 7790, `--user-data-dir` nou, server oprit dupa PID; Foreman-ul utilizatorului de pe 7777 si `~/.foreman` nu se ating):
  1. proiectul 5 (Luxury Motors), task-ul radacina cu sub-task (ex. T-7/T-8: gaseste-le cu `sqlite3` pe copie): tab-ul Flow din radacina si din sub-task — acelasi arbore, nodul curent cu bordura amber, celalalt nod e link care muta modala;
  2. un pas cu text (raport) desfasurat cu Show, apoi un refresh al modalei (ex. un `POST` pe copie care emite un eveniment, sau asteapta un SSE): pasul ramane deschis;
  3. un task fara sub-task-uri: hint-ul "No delegation: this task was done by one agent.";
  4. latime 390px: `document.documentElement.scrollWidth <= innerWidth`.
- [ ] **Step 6: `CLAUDE.md`**, sectiunea Stare, dupa "sarcini recurente": `Gata (Flow, spec in docs/superpowers/specs/2026-10-03-foreman-task-flow-design.md): tab-ul Flow din modala de task arata arborele de delegare de la radacina (store.TaskFlow, GET /api/tasks/{id}/flow), cu pasii din task_events si sub-task-urile intercalate; pasul "requested" leaga task-ul de mesajul din chat care l-a pornit (activeRun.request, doar in memorie).`
- [ ] **Step 7:** verificarea completa (`go vet ./...`, `go build -race ./cmd/foreman`, `go test ./...`, cele doua cross-compile). Commit: `ui: tab-ul Flow in modala de task`
