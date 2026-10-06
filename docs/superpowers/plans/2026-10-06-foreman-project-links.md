# Proiecte legate: plan de implementare

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** utilizatorul leaga proiecte intre ele; agentii pun intrebari proiectelor legate si le cer munca, care se
intoarce ca raport la cel care a cerut.

**Architecture:** store-ul primeste `project_links`, `project_requests` si coloanele `request_id` pe `tasks` si
`messages` (migrarea 017). Pachetul `mcp` expune patru tool-uri noi, listate dupa flag-urile `Caller.HasLinks`,
`IsRoot`, `FromRequest`. Regulile sunt in `internal/team/links.go`, sub `t.work`: intrebarile merg ca note la radacina
tinta, cererile de lucru devin task-uri pe drumul `userCreateTask`, iar orice task venit din alt proiect isi livreaza
rezultatul din `afterDone` (orice drum spre Done). Web-ul si UI-ul adauga legaturile si cardul din "Needs you".

**Tech Stack:** Go 1.22+ (net/http ServeMux cu pattern-uri), SQLite prin modernc.org/sqlite, JS vanilla (ES modules).

**Spec:** `docs/superpowers/specs/2026-10-06-foreman-project-links-design.md`

## Global Constraints

- Tot ce vede utilizatorul sau agentul (UI, erori, raspunsuri de tool, prompt) e in engleza; comentariile din cod in romana (fara diacritice).
- `go vet ./...` si `go build -race ./cmd/foreman` trec; cross-compile `GOOS=darwin GOARCH=arm64 go build -o <tmp> ./cmd/foreman` si `GOOS=windows go build -o <tmp> ./cmd/foreman`.
- Fara dependente noi.
- JS-ul se verifica copiind fisierul ca `.mjs` si rulind `node --check`.
- Limite: intrebare 1..2000, raspuns 1..10000, titlu 1..200, descriere 0..10000 caractere; `MaxOpenQuestions = 3` intrebari deschise per agent.
- Referinte: `Q-<id>` (intrebari), `W-<id>` (cereri de lucru), acelasi sir de id-uri.
- Cererile de lucru: directe DOAR intr-un run pornit de un mesaj real al utilizatorului din chat (`t.requestOf`); Full autonomy NU le face directe.
- Mesajele catre agenti din alt proiect sunt note (`t.note(..., false)` sau `store.Message{Kind: KindEvent}`), niciodata `addUserMessage`.
- Comenzile se ruleaza din `agentdash/`. Nu atinge aplicatia care ruleaza pe portul 7777 si nici `~/.foreman`.

## Review Focus

1. O intrebare livrata radacinii intr-un run pe task (nu doar intr-un run de note) trebuie sa activeze si ea anti-bucla:
   ambele locuri care livreaza note (`t.delivered(` in `reconcile.go`, ~138 si ~261) marcheaza run-ul (Task 3, Step 4 + test).
2. Radacina tinta termina run-ul pe task fara `report_done`: task-ul ajunge in Review la utilizator; cand utilizatorul
   il accepta, rezultatul tot trebuie sa ajunga inapoi (livrarea e in `afterDone`; test in Task 4 cu acceptul utilizatorului).
3. Approve dupa ce legatura a fost stearsa sau cererea a fost deja aprobata (dublu-click): 409, nu doua task-uri (test in Task 4).
4. Proiectul tinta are etape: task-ul intra in prima etapa (fara asignat), iar livrarea se face tot la Done (test in Task 4).
5. Un proiect sters care avea cereri deschise sau aprobate catre el: cei care asteptau primesc o nota (test in Task 4).

---

### Task 1: Store: legaturi, cereri, `request_id`

**Files:**
- Create: `internal/store/migrations/017_project_links.sql`
- Create: `internal/store/project_links.go`
- Create: `internal/store/project_links_test.go`
- Modify: `internal/store/tasks.go` (`Task`, `taskCols`, `scanTask`, `CreateTask`)
- Modify: `internal/store/messages.go` (`Message`, `messageCols`, `scanMessage`, `AddMessage`)
- Modify: `internal/store/store_test.go` (user_version 16 -> 17, ambele locuri)

**Interfaces:**
- Produces:
  - `var ErrSameProject = errors.New("a project cannot be linked with itself")`
  - `type LinkedProject struct { ID int64; Name, Goal string }` (json `id, name, goal`)
  - `(*Store) LinkProjects(a, b int64) error` (ErrSameProject; ErrConflict daca exista)
  - `(*Store) UnlinkProjects(a, b int64) error` (ErrNotFound daca nu exista)
  - `(*Store) LinkedProjects(projectID int64) ([]LinkedProject, error)` (non-nil, dupa nume NOCASE)
  - `(*Store) AreLinked(a, b int64) (bool, error)`
  - constante `RequestQuestion = "question"`, `RequestWork = "work"`, `ReqOpen = "open"`, `ReqAnswered = "answered"`, `ReqClosed = "closed"`, `ReqPending = "pending"`, `ReqApproved = "approved"`, `ReqDone = "done"`, `ReqDismissed = "dismissed"`
  - `type ProjectRequest struct { ID int64; Kind string; FromProject int64; FromAgent, FromTask *int64; ToProject int64; Title, Text, Status, Answer string; ToTask *int64; CreatedAt time.Time; ClosedAt *time.Time }` cu `func (r ProjectRequest) Ref() string`
  - `(*Store) AddProjectRequest(r *ProjectRequest) error` (statusul il da apelantul)
  - `(*Store) GetProjectRequest(id int64) (ProjectRequest, error)` (ErrNotFound)
  - `(*Store) TransitionProjectRequest(id int64, from, to, answer string, toTask *int64) error` (doar din `from`; altfel ErrConflict / ErrNotFound; `answer` gol pastreaza raspunsul; `toTask` nil pastreaza task-ul; seteaza `closed_at` pentru answered/closed/done/dismissed)
  - `(*Store) ListPendingWork(toProject int64) ([]ProjectRequest, error)` (non-nil, dupa id)
  - `(*Store) CountPendingWork(toProject int64) (int, error)`
  - `(*Store) CountOpenQuestionsFrom(agentID int64) (int, error)`
  - `(*Store) OpenRequestsBetween(a, b int64) ([]ProjectRequest, error)` (open/pending, ambele sensuri)
  - `(*Store) OpenRequestsTo(projectID int64) ([]ProjectRequest, error)` (open/pending/approved catre proiect)
  - `Task.RequestID *int64` (json `requestId`), scris de `CreateTask`
  - `Message.RequestID *int64` (json `requestId`), scris de `AddMessage`

- [ ] **Step 1: Migrarea** `017_project_links.sql`:

```sql
-- legaturi intre proiecte si cererile dintre ele (intrebari si cereri de lucru)
CREATE TABLE project_links (
  id         INTEGER PRIMARY KEY,
  project_a  INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE, -- mereu id-ul mai mic
  project_b  INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  created_at TEXT NOT NULL,
  UNIQUE (project_a, project_b),
  CHECK (project_a < project_b)
);

CREATE TABLE project_requests (
  id           INTEGER PRIMARY KEY,
  kind         TEXT NOT NULL CHECK (kind IN ('question', 'work')),
  from_project INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  from_agent   INTEGER REFERENCES agents(id) ON DELETE SET NULL,
  from_task    INTEGER REFERENCES tasks(id) ON DELETE SET NULL,
  to_project   INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  title        TEXT NOT NULL DEFAULT '',
  text         TEXT NOT NULL,
  status       TEXT NOT NULL CHECK (status IN ('open', 'answered', 'closed', 'pending', 'approved', 'done', 'dismissed')),
  answer       TEXT NOT NULL DEFAULT '',
  to_task      INTEGER REFERENCES tasks(id) ON DELETE SET NULL,
  created_at   TEXT NOT NULL,
  closed_at    TEXT
);
CREATE INDEX project_requests_to ON project_requests(to_project, status);
CREATE INDEX project_requests_from ON project_requests(from_agent, kind, status);

-- task-ul creat dintr-o cerere de lucru; nota cu o intrebare primita (anti-bucla)
ALTER TABLE tasks ADD COLUMN request_id INTEGER REFERENCES project_requests(id) ON DELETE SET NULL;
ALTER TABLE messages ADD COLUMN request_id INTEGER REFERENCES project_requests(id) ON DELETE SET NULL;
```

- [ ] **Step 2: Testele** (`project_links_test.go`, `package store`, cu `newStore`, `newProject`, `newAgent`, `ptr` din `store_test.go`):

```go
package store

import (
	"errors"
	"testing"
)

// twoProjects: proiectul fixture-ului si inca unul, fiecare cu liderul lui.
func twoProjects(t *testing.T, s *Store) (Project, Agent, Project, Agent) {
	t.Helper()
	p1, l1 := newProject(t, s)
	p2 := Project{Name: "Backend", Goal: "Serve the API", Workdir: t.TempDir()}
	l2, err := s.CreateProject(&p2)
	if err != nil {
		t.Fatal(err)
	}
	return p1, l1, p2, l2
}

func TestProjectLinks(t *testing.T) {
	s := newStore(t)
	p1, _, p2, _ := twoProjects(t, s)
	if err := s.LinkProjects(p1.ID, p1.ID); !errors.Is(err, ErrSameProject) {
		t.Fatalf("acelasi proiect: %v", err)
	}
	// ordinea nu conteaza: (p2, p1) e aceeasi pereche
	if err := s.LinkProjects(p2.ID, p1.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.LinkProjects(p1.ID, p2.ID); !errors.Is(err, ErrConflict) {
		t.Fatalf("dublura: %v", err)
	}
	if ok, _ := s.AreLinked(p1.ID, p2.ID); !ok {
		t.Fatal("trebuie legate")
	}
	l, err := s.LinkedProjects(p1.ID)
	if err != nil || len(l) != 1 || l[0].ID != p2.ID || l[0].Name != "Backend" || l[0].Goal != "Serve the API" {
		t.Fatalf("%+v %v", l, err)
	}
	if l, _ := s.LinkedProjects(p2.ID); len(l) != 1 || l[0].ID != p1.ID {
		t.Fatalf("din celalalt capat: %+v", l)
	}
	if err := s.UnlinkProjects(p1.ID, p2.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.UnlinkProjects(p1.ID, p2.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a doua stergere: %v", err)
	}
	if l, _ := s.LinkedProjects(p1.ID); l == nil || len(l) != 0 {
		t.Fatalf("lista goala non-nil: %#v", l)
	}
}

func TestLinksCascadeOnProjectDelete(t *testing.T) {
	s := newStore(t)
	p1, l1, p2, _ := twoProjects(t, s)
	if err := s.LinkProjects(p1.ID, p2.ID); err != nil {
		t.Fatal(err)
	}
	r := ProjectRequest{Kind: RequestQuestion, FromProject: p1.ID, FromAgent: &l1.ID, ToProject: p2.ID, Text: "?", Status: ReqOpen}
	if err := s.AddProjectRequest(&r); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteProject(p2.ID); err != nil {
		t.Fatal(err)
	}
	if l, _ := s.LinkedProjects(p1.ID); len(l) != 0 {
		t.Fatalf("legatura trebuia stearsa: %+v", l)
	}
	if _, err := s.GetProjectRequest(r.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cererea trebuia stearsa: %v", err)
	}
}

func TestProjectRequestLifecycle(t *testing.T) {
	s := newStore(t)
	p1, l1, p2, _ := twoProjects(t, s)
	q := ProjectRequest{Kind: RequestQuestion, FromProject: p1.ID, FromAgent: &l1.ID, ToProject: p2.ID, Text: "Which format?", Status: ReqOpen}
	if err := s.AddProjectRequest(&q); err != nil {
		t.Fatal(err)
	}
	if q.Ref() != "Q-"+itoa(q.ID) {
		t.Fatalf("ref: %s", q.Ref())
	}
	if n, _ := s.CountOpenQuestionsFrom(l1.ID); n != 1 {
		t.Fatalf("open = %d", n)
	}
	if err := s.TransitionProjectRequest(q.ID, ReqOpen, ReqAnswered, "JSON", nil); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetProjectRequest(q.ID)
	if got.Status != ReqAnswered || got.Answer != "JSON" || got.ClosedAt == nil {
		t.Fatalf("%+v", got)
	}
	if err := s.TransitionProjectRequest(q.ID, ReqOpen, ReqClosed, "", nil); !errors.Is(err, ErrConflict) {
		t.Fatalf("din alta stare: %v", err)
	}
	if err := s.TransitionProjectRequest(9999, ReqOpen, ReqClosed, "", nil); !errors.Is(err, ErrNotFound) {
		t.Fatalf("inexistenta: %v", err)
	}
	if n, _ := s.CountOpenQuestionsFrom(l1.ID); n != 0 {
		t.Fatalf("dupa raspuns = %d", n)
	}

	w := ProjectRequest{Kind: RequestWork, FromProject: p1.ID, FromAgent: &l1.ID, ToProject: p2.ID, Title: "Add /export", Text: "CSV", Status: ReqPending}
	if err := s.AddProjectRequest(&w); err != nil {
		t.Fatal(err)
	}
	if w.Ref() != "W-"+itoa(w.ID) {
		t.Fatalf("ref: %s", w.Ref())
	}
	if list, _ := s.ListPendingWork(p2.ID); len(list) != 1 || list[0].Title != "Add /export" {
		t.Fatalf("%+v", list)
	}
	if n, _ := s.CountPendingWork(p2.ID); n != 1 {
		t.Fatalf("pending = %d", n)
	}
	task := Task{ProjectID: p2.ID, Title: "Add /export", AssigneeID: l1.ID, RequestID: &w.ID}
	// asignatul trebuie sa fie din proiectul 2; folosim radacina lui
	l2s, _ := s.ListAgents(p2.ID)
	task.AssigneeID = l2s[0].ID
	if err := s.CreateTask(&task); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.GetTask(task.ID); got.RequestID == nil || *got.RequestID != w.ID {
		t.Fatalf("request_id pe task: %+v", got.RequestID)
	}
	if err := s.TransitionProjectRequest(w.ID, ReqPending, ReqApproved, "", &task.ID); err != nil {
		t.Fatal(err)
	}
	got, _ = s.GetProjectRequest(w.ID)
	if got.ToTask == nil || *got.ToTask != task.ID || got.ClosedAt != nil {
		t.Fatalf("aprobata: %+v", got)
	}
	if err := s.TransitionProjectRequest(w.ID, ReqApproved, ReqDone, "Done: /export", nil); err != nil {
		t.Fatal(err)
	}
	got, _ = s.GetProjectRequest(w.ID)
	if got.Status != ReqDone || got.Answer != "Done: /export" || got.ToTask == nil || got.ClosedAt == nil {
		t.Fatalf("livrata: %+v", got)
	}
}

func TestOpenRequestsBetweenAndTo(t *testing.T) {
	s := newStore(t)
	p1, l1, p2, l2 := twoProjects(t, s)
	add := func(kind string, from, to int64, agent int64, status string) ProjectRequest {
		r := ProjectRequest{Kind: kind, FromProject: from, FromAgent: &agent, ToProject: to, Text: "x", Status: status}
		if err := s.AddProjectRequest(&r); err != nil {
			t.Fatal(err)
		}
		return r
	}
	add(RequestQuestion, p1.ID, p2.ID, l1.ID, ReqOpen)
	add(RequestWork, p2.ID, p1.ID, l2.ID, ReqPending)
	add(RequestWork, p1.ID, p2.ID, l1.ID, ReqApproved)
	add(RequestQuestion, p1.ID, p2.ID, l1.ID, ReqAnswered)
	if list, _ := s.OpenRequestsBetween(p2.ID, p1.ID); len(list) != 2 {
		t.Fatalf("intre: %+v", list)
	}
	if list, _ := s.OpenRequestsTo(p2.ID); len(list) != 2 { // open + approved catre p2
		t.Fatalf("catre p2: %+v", list)
	}
}

func TestMessageRequestID(t *testing.T) {
	s := newStore(t)
	p1, l1, p2, _ := twoProjects(t, s)
	r := ProjectRequest{Kind: RequestQuestion, FromProject: p2.ID, ToProject: p1.ID, Text: "?", Status: ReqOpen}
	if err := s.AddProjectRequest(&r); err != nil {
		t.Fatal(err)
	}
	m := Message{ProjectID: p1.ID, AgentID: l1.ID, Kind: KindEvent, Text: "Question", RequestID: &r.ID}
	if err := s.AddMessage(&m); err != nil {
		t.Fatal(err)
	}
	msgs, _ := s.Undelivered(l1.ID)
	if len(msgs) == 0 || msgs[len(msgs)-1].RequestID == nil || *msgs[len(msgs)-1].RequestID != r.ID {
		t.Fatalf("request_id pe mesaj: %+v", msgs)
	}
}
```

Daca `itoa` nu exista in testele din `store`, adauga in `project_links_test.go`: `func itoa(n int64) string { return strconv.FormatInt(n, 10) }` (import `strconv`).

- [ ] **Step 3:** `go test ./internal/store/` -> FAIL (tipuri lipsa).

- [ ] **Step 4: `tasks.go` si `messages.go`.**
  - `Task`: dupa `AutoAccept`, `RequestID *int64 \`json:"requestId"\` // cererea de lucru din alt proiect care l-a creat; nil = altfel`. `taskCols` primeste la final `, request_id`; `scanTask` scaneaza la final `&t.RequestID`; INSERT-ul din `CreateTask` primeste coloana `request_id` dupa `auto_accept` si valoarea `nullPtr(t.RequestID)` (inca un `?`). `SaveTask` nu o atinge.
  - `Message`: dupa `Reply`, `RequestID *int64 \`json:"requestId"\` // nota cu o intrebare primita din alt proiect (anti-bucla); nil = altfel`. `messageCols` primeste dupa `reply` coloana `request_id`; `scanMessage` scaneaza `&m.RequestID` in aceeasi pozitie; INSERT-ul din `AddMessage` primeste `request_id` si `nullPtr(m.RequestID)`.
  - Verifica cu `grep -n "INSERT INTO messages\|INSERT INTO tasks" internal/store/*.go` ca nu mai exista alte INSERT-uri care trebuie sa ramana compatibile (cele fara coloana folosesc default-ul NULL, deci sunt in regula).

- [ ] **Step 5: `project_links.go`:**

```go
package store

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// ErrSameProject: un proiect nu se leaga cu el insusi.
var ErrSameProject = errors.New("a project cannot be linked with itself")

// LinkedProject e un proiect legat, asa cum il vad agentii si UI-ul.
type LinkedProject struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
	Goal string `json:"goal"`
}

// linkPair ordoneaza perechea: in tabel project_a e mereu id-ul mai mic.
func linkPair(a, b int64) (int64, int64) {
	if a > b {
		return b, a
	}
	return a, b
}

// LinkProjects leaga doua proiecte (fara sens). ErrConflict daca sunt deja legate.
func (s *Store) LinkProjects(a, b int64) error {
	if a == b {
		return ErrSameProject
	}
	x, y := linkPair(a, b)
	_, err := s.db.Exec(`INSERT INTO project_links (project_a, project_b, created_at) VALUES (?, ?, ?)`, x, y, now())
	if isUnique(err) {
		return ErrConflict
	}
	return err
}

// UnlinkProjects sterge legatura. ErrNotFound daca nu exista.
func (s *Store) UnlinkProjects(a, b int64) error {
	x, y := linkPair(a, b)
	return mustAffect(s.db.Exec(`DELETE FROM project_links WHERE project_a = ? AND project_b = ?`, x, y))
}

// AreLinked: cele doua proiecte sunt legate.
func (s *Store) AreLinked(a, b int64) (bool, error) {
	x, y := linkPair(a, b)
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM project_links WHERE project_a = ? AND project_b = ?`, x, y).Scan(&n)
	return n > 0, err
}

// LinkedProjects intoarce proiectele legate de projectID, dupa nume.
func (s *Store) LinkedProjects(projectID int64) ([]LinkedProject, error) {
	rows, err := s.db.Query(`SELECT p.id, p.name, p.goal FROM projects p JOIN project_links l
		ON (l.project_a = ? AND p.id = l.project_b) OR (l.project_b = ? AND p.id = l.project_a)
		ORDER BY p.name COLLATE NOCASE, p.id`, projectID, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []LinkedProject{}
	for rows.Next() {
		var l LinkedProject
		if err := rows.Scan(&l.ID, &l.Name, &l.Goal); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// Tipurile si starile unei cereri intre proiecte.
const (
	RequestQuestion = "question"
	RequestWork     = "work"

	ReqOpen      = "open"     // intrebare fara raspuns
	ReqAnswered  = "answered" // intrebare cu raspuns
	ReqClosed    = "closed"   // inchisa fara raspuns (legatura sau proiectul au disparut)
	ReqPending   = "pending"  // cerere de lucru care asteapta aprobarea
	ReqApproved  = "approved" // cerere de lucru cu task in proiectul tinta
	ReqDone      = "done"     // cerere de lucru livrata
	ReqDismissed = "dismissed"
)

// ProjectRequest e o intrebare sau o cerere de lucru de la un agent catre un proiect legat.
type ProjectRequest struct {
	ID          int64      `json:"id"`
	Kind        string     `json:"kind"`
	FromProject int64      `json:"fromProject"`
	FromAgent   *int64     `json:"fromAgent"` // nil = agentul a fost sters
	FromTask    *int64     `json:"fromTask"`  // task-ul run-ului care a cerut; nil = fara task
	ToProject   int64      `json:"toProject"`
	Title       string     `json:"title"` // doar work
	Text        string     `json:"text"`  // intrebarea sau descrierea
	Status      string     `json:"status"`
	Answer      string     `json:"answer"` // raspunsul sau raportul
	ToTask      *int64     `json:"toTask"` // task-ul creat in proiectul tinta
	CreatedAt   time.Time  `json:"createdAt"`
	ClosedAt    *time.Time `json:"closedAt"`
}

// Ref: Q-7 pentru intrebari, W-7 pentru cereri de lucru.
func (r ProjectRequest) Ref() string {
	if r.Kind == RequestWork {
		return fmt.Sprintf("W-%d", r.ID)
	}
	return fmt.Sprintf("Q-%d", r.ID)
}

const projectRequestCols = `id, kind, from_project, from_agent, from_task, to_project, title, text, status, answer, to_task, created_at, closed_at`

func scanProjectRequest(sc scanner) (ProjectRequest, error) {
	var r ProjectRequest
	var created string
	var closed sql.NullString
	err := sc.Scan(&r.ID, &r.Kind, &r.FromProject, &r.FromAgent, &r.FromTask, &r.ToProject, &r.Title, &r.Text,
		&r.Status, &r.Answer, &r.ToTask, &created, &closed)
	if errors.Is(err, sql.ErrNoRows) {
		return ProjectRequest{}, ErrNotFound
	}
	if err != nil {
		return ProjectRequest{}, err
	}
	r.CreatedAt = parseTime(created)
	if closed.Valid {
		t := parseTime(closed.String)
		r.ClosedAt = &t
	}
	return r, nil
}

func (s *Store) queryProjectRequests(where string, args ...any) ([]ProjectRequest, error) {
	rows, err := s.db.Query(`SELECT `+projectRequestCols+` FROM project_requests WHERE `+where+` ORDER BY id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ProjectRequest{}
	for rows.Next() {
		r, err := scanProjectRequest(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// AddProjectRequest salveaza o cerere; statusul (open sau pending) il da apelantul.
func (s *Store) AddProjectRequest(r *ProjectRequest) error {
	created := now()
	res, err := s.db.Exec(`INSERT INTO project_requests (kind, from_project, from_agent, from_task, to_project, title, text, status, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, r.Kind, r.FromProject, nullPtr(r.FromAgent), nullPtr(r.FromTask), r.ToProject,
		r.Title, r.Text, r.Status, created)
	if err != nil {
		return err
	}
	if r.ID, err = res.LastInsertId(); err != nil {
		return err
	}
	r.CreatedAt = parseTime(created)
	return nil
}

func (s *Store) GetProjectRequest(id int64) (ProjectRequest, error) {
	return scanProjectRequest(s.db.QueryRow(`SELECT `+projectRequestCols+` FROM project_requests WHERE id = ?`, id))
}

// TransitionProjectRequest muta cererea din starea from in to. answer gol
// pastreaza raspunsul, toTask nil pastreaza task-ul. Starile finale primesc
// closed_at. Din alta stare: ErrConflict; cerere inexistenta: ErrNotFound.
func (s *Store) TransitionProjectRequest(id int64, from, to, answer string, toTask *int64) error {
	var closed any
	switch to {
	case ReqAnswered, ReqClosed, ReqDone, ReqDismissed:
		closed = now()
	}
	res, err := s.db.Exec(`UPDATE project_requests SET status = ?,
		answer = CASE WHEN ? = '' THEN answer ELSE ? END,
		to_task = COALESCE(?, to_task),
		closed_at = COALESCE(?, closed_at)
		WHERE id = ? AND status = ?`, to, answer, answer, nullPtr(toTask), closed, id, from)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 1 {
		return nil
	}
	if _, err := s.GetProjectRequest(id); err != nil {
		return err
	}
	return ErrConflict
}

// ListPendingWork: cererile de lucru care asteapta aprobarea, catre proiect.
func (s *Store) ListPendingWork(toProject int64) ([]ProjectRequest, error) {
	return s.queryProjectRequests(`to_project = ? AND kind = 'work' AND status = 'pending'`, toProject)
}

func (s *Store) CountPendingWork(toProject int64) (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM project_requests WHERE to_project = ? AND kind = 'work' AND status = 'pending'`, toProject).Scan(&n)
	return n, err
}

// CountOpenQuestionsFrom: intrebarile fara raspuns trimise de agent altor proiecte.
func (s *Store) CountOpenQuestionsFrom(agentID int64) (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM project_requests WHERE from_agent = ? AND kind = 'question' AND status = 'open'`, agentID).Scan(&n)
	return n, err
}

// OpenRequestsBetween: intrebarile deschise si cererile neaprobate dintre doua proiecte, in ambele sensuri.
func (s *Store) OpenRequestsBetween(a, b int64) ([]ProjectRequest, error) {
	return s.queryProjectRequests(`status IN ('open', 'pending') AND
		((from_project = ? AND to_project = ?) OR (from_project = ? AND to_project = ?))`, a, b, b, a)
}

// OpenRequestsTo: cererile catre proiect care inca asteapta ceva (raspuns, aprobare sau livrare).
func (s *Store) OpenRequestsTo(projectID int64) ([]ProjectRequest, error) {
	return s.queryProjectRequests(`to_project = ? AND status IN ('open', 'pending', 'approved')`, projectID)
}
```

- [ ] **Step 6:** `store_test.go`: `16` -> `17` (ambele locuri). `go test ./internal/store/` -> PASS; `gofmt -l internal/store` gol; `go build ./...`.
- [ ] **Step 7:** commit: `store: legaturi intre proiecte si cererile dintre ele (migrarea 017)`

---

### Task 2: MCP: tool-urile pentru proiecte legate

**Files:**
- Modify: `internal/mcp/server.go` (`Caller`, `Backend`, `call`)
- Modify: `internal/mcp/tools.go` (tool-uri, `toolsFor`)
- Modify: `internal/mcp/server_test.go`
- Create: `internal/team/links.go` (doar stub-uri; Task 3 le inlocuieste)
- Modify: `internal/team/run.go` (flag-urile noi)

**Interfaces:**
- Consumes: Task 1 (`store.LinkedProjects`, `Task.RequestID`).
- Produces:
  - `Caller.HasLinks`, `Caller.IsRoot`, `Caller.FromRequest` (bool).
  - `Backend.ListLinkedProjects(c Caller) (string, error)`
  - `Backend.AskProject(c Caller, project, question string) (string, error)`
  - `Backend.ReplyToProject(c Caller, request, answer string) (string, error)`
  - `Backend.RequestProjectWork(c Caller, project, title, description string) (string, error)`
  - In `team/run.go`: variabila locala `hasLinks bool`, folosita si de Task 5 la prompt.

- [ ] **Step 1: Testele** (`server_test.go`). In `fakeBackend`:

```go
func (f *fakeBackend) ListLinkedProjects(c Caller) (string, error) {
	f.calls = append(f.calls, "list_linked_projects")
	return "Linked projects: Backend", f.err
}
func (f *fakeBackend) AskProject(c Caller, project, question string) (string, error) {
	f.calls = append(f.calls, "ask_project|"+project+"|"+question)
	return "ok", f.err
}
func (f *fakeBackend) ReplyToProject(c Caller, request, answer string) (string, error) {
	f.calls = append(f.calls, "reply_to_project|"+request+"|"+answer)
	return "ok", f.err
}
func (f *fakeBackend) RequestProjectWork(c Caller, project, title, description string) (string, error) {
	f.calls = append(f.calls, "request_project_work|"+project+"|"+title+"|"+description)
	return "ok", f.err
}
```

In `setup`, la `callers`:

```go
		"linkedworker": {ProjectID: 1, AgentID: 20, HasParent: true, HasLinks: true},
		"linkedlead":   {ProjectID: 1, AgentID: 21, IsLead: true, CanAddAgents: true, IsRoot: true, HasLinks: true},
		"rootfromreq":  {ProjectID: 1, AgentID: 22, IsRoot: true, CanAddAgents: true, TaskID: 9, FromRequest: true},
```

Teste noi:

```go
func TestLinkedProjectTools(t *testing.T) {
	_, ts := setup(t)
	names := func(token string) string {
		_, resp := rpc(t, ts, token, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)
		var out []string
		for _, tool := range result(t, resp)["tools"].([]any) {
			out = append(out, tool.(map[string]any)["name"].(string))
		}
		return strings.Join(out, ",")
	}
	if got := names("linkedworker"); got != "report_done,list_linked_projects,ask_project,ask_user" {
		t.Fatalf("linkedworker: %s", got)
	}
	if got := names("linkedlead"); got != "create_task,list_tasks,accept_task,send_back,add_agent,remove_agent,list_linked_projects,ask_project,reply_to_project,request_project_work,ask_user" {
		t.Fatalf("linkedlead: %s", got)
	}
	// radacina intr-un run pe un task venit din alt proiect: report_done chiar fara sef
	if got := names("rootfromreq"); !strings.Contains(got, "report_done") || strings.Contains(got, "ask_project") {
		t.Fatalf("rootfromreq: %s", got)
	}
	for _, token := range []string{"lead", "worker", "setup"} {
		if got := names(token); strings.Contains(got, "ask_project") {
			t.Fatalf("%s fara legaturi nu are ask_project: %s", token, got)
		}
	}
}

func TestCallLinkedProjectTools(t *testing.T) {
	fb, ts := setup(t)
	calls := []string{
		`{"name":"list_linked_projects","arguments":{}}`,
		`{"name":"ask_project","arguments":{"project":"Backend","question":"Which format?"}}`,
		`{"name":"reply_to_project","arguments":{"request":"Q-7","answer":"JSON"}}`,
		`{"name":"request_project_work","arguments":{"project":"Backend","title":"Add /export","description":"CSV"}}`,
	}
	for i, c := range calls {
		_, resp := rpc(t, ts, "linkedlead", fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"method":"tools/call","params":%s}`, i+3, c))
		if _, isErr := toolText(t, resp); isErr {
			t.Fatalf("%s: %v", c, resp)
		}
	}
	want := "list_linked_projects,ask_project|Backend|Which format?,reply_to_project|Q-7|JSON,request_project_work|Backend|Add /export|CSV"
	if got := strings.Join(fb.calls, ","); got != want {
		t.Fatalf("%s", got)
	}
	// un worker nu poate cere munca si nu raspunde in numele proiectului
	for _, c := range calls[2:] {
		_, resp := rpc(t, ts, "linkedworker", `{"jsonrpc":"2.0","id":9,"method":"tools/call","params":`+c+`}`)
		if _, isErr := toolText(t, resp); !isErr {
			t.Fatalf("worker: %s", c)
		}
	}
}
```

(`fmt` e deja importat de testele din Task-ul anterior al proiectului; daca nu, adauga-l.)

- [ ] **Step 2:** `go test ./internal/mcp/` -> FAIL (nu compileaza).

- [ ] **Step 3: `server.go`.** In `Caller`, dupa `CanAddAgents`:

```go
	HasLinks     bool // proiectul are legaturi cu alte proiecte
	IsRoot       bool // raporteaza utilizatorului: primeste intrebarile proiectelor legate
	FromRequest  bool // run pe un task cerut de un proiect legat: are report_done chiar fara sef
```

In `Backend`, dupa `RemoveAgent`:

```go
	ListLinkedProjects(c Caller) (string, error)
	AskProject(c Caller, project, question string) (string, error)
	ReplyToProject(c Caller, request, answer string) (string, error)
	RequestProjectWork(c Caller, project, title, description string) (string, error)
```

In `call`, dupa `case "remove_agent"`:

```go
	case "list_linked_projects":
		text, err = s.backend.ListLinkedProjects(c)
	case "ask_project":
		text, err = s.backend.AskProject(c, arg("project"), arg("question"))
	case "reply_to_project":
		text, err = s.backend.ReplyToProject(c, arg("request"), arg("answer"))
	case "request_project_work":
		text, err = s.backend.RequestProjectWork(c, arg("project"), arg("title"), arg("description"))
```

- [ ] **Step 4: `tools.go`.** Dupa `addAgentTools`:

```go
var linkedProjectsTool = Tool{
	Name:        "list_linked_projects",
	Description: "Lists the projects linked with yours: name, goal and root agent. Questions and work requests go to their root agent.",
	InputSchema: schema([]string{}, map[string]any{}),
}

var askProjectTool = Tool{
	Name:        "ask_project",
	Description: "Asks a linked project a question; its root agent answers. The answer arrives later as a message, so continue meanwhile. Only ask what your own team cannot answer.",
	InputSchema: schema([]string{"project", "question"}, map[string]any{
		"project":  str("The linked project's name, exactly as in list_linked_projects"),
		"question": str("The question, clear and self-contained"),
	}),
}

var replyToProjectTool = Tool{
	Name:        "reply_to_project",
	Description: "Answers a question a linked project asked your project (it arrives as a message with a reference like Q-7).",
	InputSchema: schema([]string{"request", "answer"}, map[string]any{
		"request": str("The question's reference, e.g. Q-7"),
		"answer":  str("Your answer"),
	}),
}

var requestWorkTool = Tool{
	Name:        "request_project_work",
	Description: "Asks a linked project to do work for you. It becomes a task for its root agent (usually after the user approves it); its report comes back to you as a message.",
	InputSchema: schema([]string{"project", "title"}, map[string]any{
		"project":     str("The linked project's name, exactly as in list_linked_projects"),
		"title":       str("Short, clear title"),
		"description": str("What is needed, acceptance criteria, how you will use it"),
	}),
}
```

In `toolsFor`, imediat inainte de `out = append(out, askUserTool)`:

```go
	// proiecte legate: intrebari pentru toti, raspunsuri pentru radacini, cereri de lucru pentru lideri
	if c.HasLinks {
		out = append(out, linkedProjectsTool, askProjectTool)
		if c.IsRoot {
			out = append(out, replyToProjectTool)
		}
		if c.CanAddAgents {
			out = append(out, requestWorkTool)
		}
	}
	// radacina livreaza cu report_done munca ceruta de un proiect legat
	if c.FromRequest && !has(reportTool.Name) {
		out = append(out, reportTool)
	}
```

- [ ] **Step 5: Stub-uri in `internal/team/links.go`:**

```go
package team

import (
	"errors"

	"foreman/internal/mcp"
)

// Tool-urile pentru proiecte legate (implementarea completa vine in pasul urmator al planului).

func (t *Team) ListLinkedProjects(c mcp.Caller) (string, error) {
	return "", errors.New("not available yet")
}

func (t *Team) AskProject(c mcp.Caller, project, question string) (string, error) {
	return "", errors.New("not available yet")
}

func (t *Team) ReplyToProject(c mcp.Caller, request, answer string) (string, error) {
	return "", errors.New("not available yet")
}

func (t *Team) RequestProjectWork(c mcp.Caller, project, title, description string) (string, error) {
	return "", errors.New("not available yet")
}
```

- [ ] **Step 6: `run.go`.** In `startRun`:
  - Inainte de `system := prompt.System(...)`: 
    ```go
    	linkedProjects, err := t.st.LinkedProjects(p.ID)
    	if err != nil {
    		return err
    	}
    	hasLinks := len(linkedProjects) > 0 // proiectul are legaturi: agentii primesc tool-urile pentru proiecte legate
    ```
  - Unde se citeste task-ul pentru `stageKind` (`if task, err := t.st.GetTask(taskID); err == nil {`), retine si `fromRequest = task.RequestID != nil` (declara `fromRequest := false` langa `stageKind`).
  - In literalul `mcp.Caller`: `HasLinks: hasLinks, IsRoot: roles.IsRoot, FromRequest: fromRequest`.
  - Pana la Task 5, `hasLinks` e folosit doar aici (nu lasa variabile nefolosite).

- [ ] **Step 7:** `go test ./internal/mcp/ ./internal/team/ ./internal/setup/` -> PASS; `go vet ./...`; `gofmt -l internal/mcp internal/team` gol.
- [ ] **Step 8:** commit: `mcp: tool-urile pentru proiecte legate`

---

### Task 3: Team: intrebari intre proiecte

**Files:**
- Modify: `internal/team/links.go` (inlocuieste stub-urile `ListLinkedProjects`, `AskProject`, `ReplyToProject`; `RequestProjectWork` ramane stub pana la Task 4)
- Modify: `internal/team/team.go` (`activeRun.answering`, `setAnswering`, `answeringOf`)
- Modify: `internal/team/reconcile.go` (marcarea anti-bucla la ambele livrari de note)
- Create: `internal/team/links_test.go`

**Interfaces:**
- Consumes: Task 1 (store), Task 2 (`mcp.Caller` flags, metodele Backend).
- Produces (folosite in Task 4 si Task 6):
  - `const MaxOpenQuestions = 3`
  - `type ProjectRequestError string` (cu `Error()`), pentru erorile care in web devin 409
  - `func (t *Team) linkedTarget(from int64, name string) (store.LinkedProject, error)`
  - `func (t *Team) rootOf(projectID int64) (store.Agent, bool, error)`
  - `func (t *Team) projectName(id int64) string`
  - `func (t *Team) agentNameOf(id *int64) string`
  - `func parseRequestRef(ref, prefix string) (int64, error)`
  - `func (t *Team) emitRequest(r store.ProjectRequest)` (eveniment SSE `project_request` in ambele proiecte)

- [ ] **Step 1: Testele** (`links_test.go`, `package team`; fixture-ul `setup(t)` din `fake_test.go`: proiectul "ApiUnit" cu radacina `f.lead` ("Lead") si workerii Rook, Vera; `mustReconcile`, `autonomous` exista in testele pachetului):

```go
package team

import (
	"errors"
	"strings"
	"testing"

	"foreman/internal/mcp"
	"foreman/internal/runner"
	"foreman/internal/store"
)

// linked creeaza proiectul Backend (cu radacina lui, "Lead") si il leaga de proiectul fixture-ului.
func linked(t *testing.T, f *fixture) (store.Project, store.Agent) {
	t.Helper()
	p := store.Project{Name: "Backend", Goal: "Serve the API", Workdir: t.TempDir(), MaxParallel: 2}
	root, err := f.st.CreateProject(&p)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.st.LinkProjects(f.p.ID, p.ID); err != nil {
		t.Fatal(err)
	}
	return p, root
}

func rootCaller(p store.Project, root store.Agent) mcp.Caller {
	return mcp.Caller{ProjectID: p.ID, AgentID: root.ID, IsRoot: true, CanAddAgents: true, HasLinks: true}
}

func lastMessage(t *testing.T, f *fixture, agentID int64) store.Message {
	t.Helper()
	msgs, err := f.st.ListMessages(agentID)
	if err != nil || len(msgs) == 0 {
		t.Fatalf("niciun mesaj: %v", err)
	}
	return msgs[len(msgs)-1]
}

func TestListLinkedProjects(t *testing.T) {
	f := setup(t)
	if text, err := f.tm.ListLinkedProjects(f.leadCaller()); err != nil || text != "This project has no linked projects." {
		t.Fatalf("%q %v", text, err)
	}
	linked(t, f)
	text, err := f.tm.ListLinkedProjects(f.leadCaller())
	if err != nil || !strings.Contains(text, "Backend (root: Lead): Serve the API") {
		t.Fatalf("%q %v", text, err)
	}
}

func TestAskProjectAndReply(t *testing.T) {
	f := setup(t)
	bp, broot := linked(t, f)
	c := f.workerCaller(f.rook, 0)
	text, err := f.tm.AskProject(c, "backend", "Which format does /users return?")
	if err != nil || !strings.Contains(text, "Question sent to Backend as Q-") || strings.Contains(text, "paused") {
		t.Fatalf("%q %v", text, err)
	}
	note := lastMessage(t, f, broot.ID)
	if note.Kind != store.KindEvent || note.Author == store.AuthorUser || note.Delivered || note.RequestID == nil ||
		!strings.Contains(note.Text, "Question from ApiUnit (Rook) [Q-") || !strings.Contains(note.Text, "Which format does /users return?") {
		t.Fatalf("nota la radacina: %+v", note)
	}
	ref := "Q-" + itoa(*note.RequestID)
	reply, err := f.tm.ReplyToProject(rootCaller(bp, broot), strings.ToLower(ref), "JSON with id and name")
	if err != nil || !strings.Contains(reply, "Answer sent for "+ref) {
		t.Fatalf("%q %v", reply, err)
	}
	ans := lastMessage(t, f, f.rook.ID)
	if ans.Kind != store.KindEvent || ans.Delivered || ans.Text != "Answer from Backend to "+ref+": JSON with id and name" {
		t.Fatalf("raspunsul: %+v", ans)
	}
	r, _ := f.st.GetProjectRequest(*note.RequestID)
	if r.Status != store.ReqAnswered || r.Answer != "JSON with id and name" {
		t.Fatalf("%+v", r)
	}
	if _, err := f.tm.ReplyToProject(rootCaller(bp, broot), ref, "again"); err == nil || !strings.Contains(err.Error(), "already closed") {
		t.Fatalf("a doua oara: %v", err)
	}
}

func TestAskProjectRefusals(t *testing.T) {
	f := setup(t)
	c := f.workerCaller(f.rook, 0)
	if _, err := f.tm.AskProject(c, "Backend", "?"); err == nil || !strings.Contains(err.Error(), "no linked projects") {
		t.Fatalf("fara legaturi: %v", err)
	}
	bp, broot := linked(t, f)
	if _, err := f.tm.AskProject(c, "Nope", "?"); err == nil || !strings.Contains(err.Error(), `"Nope" is not a linked project. Linked projects: Backend`) {
		t.Fatalf("nelegat: %v", err)
	}
	if _, err := f.tm.AskProject(c, "Backend", "  "); err == nil || !strings.Contains(err.Error(), "question is required") {
		t.Fatalf("goala: %v", err)
	}
	if _, err := f.tm.AskProject(c, "Backend", strings.Repeat("x", 2001)); err == nil || !strings.Contains(err.Error(), "at most 2000") {
		t.Fatalf("lunga: %v", err)
	}
	for i := 0; i < MaxOpenQuestions; i++ {
		if _, err := f.tm.AskProject(c, "Backend", "q"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.tm.AskProject(c, "Backend", "q"); err == nil || !strings.Contains(err.Error(), "already have 3 open questions") {
		t.Fatalf("limita: %v", err)
	}
	// un worker din Backend nu raspunde in numele proiectului
	w := store.Agent{ProjectID: bp.ID, Name: "Ivo", Role: "Dev", Model: "haiku", ParentID: &broot.ID}
	if err := f.st.CreateAgent(&w); err != nil {
		t.Fatal(err)
	}
	if _, err := f.tm.ReplyToProject(mcp.Caller{ProjectID: bp.ID, AgentID: w.ID, HasParent: true}, "Q-1", "x"); err == nil ||
		!strings.Contains(err.Error(), "only the root agent") {
		t.Fatalf("worker: %v", err)
	}
	// o intrebare a altui proiect
	if _, err := f.tm.ReplyToProject(f.leadCaller(), "Q-1", "x"); err == nil || !strings.Contains(err.Error(), "is not a question to this project") {
		t.Fatalf("alt proiect: %v", err)
	}
	if _, err := f.tm.ReplyToProject(rootCaller(bp, broot), "W-x", "x"); err == nil || !strings.Contains(err.Error(), "invalid reference") {
		t.Fatalf("referinta: %v", err)
	}
}

func TestAskProjectNoRoot(t *testing.T) {
	f := setup(t)
	_, broot := linked(t, f)
	broot.Detached = true
	if err := f.st.UpdateAgent(broot); err != nil {
		t.Fatal(err)
	}
	if _, err := f.tm.AskProject(f.workerCaller(f.rook, 0), "Backend", "?"); err == nil || !strings.Contains(err.Error(), `"Backend" has no root agent to receive it`) {
		t.Fatalf("%v", err)
	}
}

func TestAskProjectPausedTarget(t *testing.T) {
	f := setup(t)
	bp, _ := linked(t, f)
	if err := f.st.SetPaused(bp.ID, true); err != nil {
		t.Fatal(err)
	}
	text, err := f.tm.AskProject(f.workerCaller(f.rook, 0), "Backend", "?")
	if err != nil || !strings.Contains(text, "Backend is paused: it answers when the user starts it.") {
		t.Fatalf("%q %v", text, err)
	}
}

func TestAskProjectAntiLoop(t *testing.T) {
	f := setup(t)
	bp, broot := linked(t, f)
	if _, err := f.tm.AskProject(f.workerCaller(f.rook, 0), "Backend", "Which format?"); err != nil {
		t.Fatal(err)
	}
	// radacina Backend primeste intrebarea intr-un run de note
	if err := f.tm.Reconcile(bp.ID); err != nil {
		t.Fatal(err)
	}
	run := f.fs.last(t, broot)
	c := f.caller(t, run)
	if !c.HasLinks || !c.IsRoot {
		t.Fatalf("flag-uri: %+v", c)
	}
	if _, err := f.tm.AskProject(c, "ApiUnit", "and you?"); err == nil || !strings.Contains(err.Error(), "you are answering a question from another project") {
		t.Fatalf("anti-bucla: %v", err)
	}
	f.fs.finish(run.ID, runner.Outcome{Status: "done"})
	// un run nou, fara intrebari primite, poate intreba
	if _, err := f.tm.AskProject(rootCaller(bp, broot), "ApiUnit", "ok now?"); err != nil {
		t.Fatalf("fara intrebare primita: %v", err)
	}
}

func TestAskProjectAntiLoopInTaskRun(t *testing.T) {
	f := setup(t)
	bp, broot := linked(t, f)
	// radacina Backend are un task in coada: intrebarea ii ajunge in run-ul pe task
	task := store.Task{ProjectID: bp.ID, Title: "Something", AssigneeID: broot.ID, ByUser: true}
	if err := f.st.CreateTask(&task); err != nil {
		t.Fatal(err)
	}
	if _, err := f.tm.AskProject(f.workerCaller(f.rook, 0), "Backend", "Which format?"); err != nil {
		t.Fatal(err)
	}
	if err := f.tm.Reconcile(bp.ID); err != nil {
		t.Fatal(err)
	}
	run := f.fs.last(t, broot)
	c := f.caller(t, run)
	if _, err := f.tm.AskProject(c, "ApiUnit", "and you?"); err == nil || !strings.Contains(err.Error(), "you are answering") {
		t.Fatalf("anti-bucla in run pe task: %v (task run %d)", err, c.TaskID)
	}
	f.fs.finish(run.ID, runner.Outcome{Status: "done"})
}

var _ = errors.Is // folosit in testele din Task 4
```

Daca `itoa` nu exista in pachetul `team`, adauga in `links_test.go` `func itoa(n int64) string { return strconv.FormatInt(n, 10) }`. Daca `TestAskProjectAntiLoopInTaskRun` arata ca, in acest proiect, run-ul pe task NU livreaza notele nelivrate, citeste `reconcile.go` in jurul livrarii de la ~261, si adapteaza doar pregatirea testului (de ex. livrarea se face in run-ul pe task cand exista note) pastrand asertiunea: orice run in care s-a livrat o nota cu `RequestID` nu poate chema `ask_project`.

- [ ] **Step 2:** `go test ./internal/team/ -run 'Linked|AskProject'` -> FAIL.

- [ ] **Step 3: `team.go`.** In `activeRun`, dupa `request`:

```go
	answering bool // run-ul a primit o intrebare din alt proiect: nu poate intreba la randul lui (anti-bucla)
```

Langa `setRequest` / `requestOf`:

```go
// setAnswering retine ca run-ul activ al agentului a primit o intrebare dintr-un proiect legat.
func (t *Team) setAnswering(agentID int64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if ar, ok := t.active[agentID]; ok {
		ar.answering = true
		t.active[agentID] = ar
	}
}

// answeringOf: run-ul runID al agentului (inca activ) a primit o intrebare dintr-un proiect legat.
func (t *Team) answeringOf(agentID int64, runID string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	ar, ok := t.active[agentID]
	return ok && ar.runID == runID && ar.answering
}

// markAnswering: dupa ce run-ul a pornit cu notele date, retine daca printre ele e o intrebare primita.
func (t *Team) markAnswering(agentID int64, notes []store.Message) {
	for _, n := range notes {
		if n.RequestID != nil {
			t.setAnswering(agentID)
			return
		}
	}
}
```

- [ ] **Step 4: `reconcile.go`.** La ambele livrari de note (`if err := t.delivered(p.ID, notes); err != nil {` in `notesRun`, ~138, si `if err := t.delivered(p.ID, msgs); err != nil {` ~261), imediat inainte de `t.delivered(...)`, adauga `t.markAnswering(a.ID, notes)` respectiv `t.markAnswering(<agentul run-ului>, msgs)` (foloseste variabila agentului din acel bloc; citeste blocul de la ~261 ca sa vezi cum se numeste). Ambele se intampla dupa `startRun`, deci `t.active` are deja run-ul.

- [ ] **Step 5: `links.go`** (pastreaza stub-ul `RequestProjectWork`; restul fisierului):

```go
package team

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"foreman/internal/mcp"
	"foreman/internal/store"
)

// MaxOpenQuestions: cate intrebari fara raspuns poate avea un agent catre alte proiecte.
const MaxOpenQuestions = 3

// ProjectRequestError: cererea nu mai poate fi aprobata (de ex. legatura a fost stearsa).
type ProjectRequestError string

func (e ProjectRequestError) Error() string { return string(e) }

// linkedTarget gaseste, dupa nume (fara diferenta de litere), un proiect legat de proiectul from.
func (t *Team) linkedTarget(from int64, name string) (store.LinkedProject, error) {
	links, err := t.st.LinkedProjects(from)
	if err != nil {
		return store.LinkedProject{}, err
	}
	if len(links) == 0 {
		return store.LinkedProject{}, errors.New("this project has no linked projects")
	}
	name = strings.TrimSpace(name)
	names := make([]string, 0, len(links))
	for _, l := range links {
		if strings.EqualFold(l.Name, name) {
			return l, nil
		}
		names = append(names, l.Name)
	}
	return store.LinkedProject{}, fmt.Errorf("%q is not a linked project. Linked projects: %s", name, strings.Join(names, ", "))
}

// rootOf intoarce prima radacina a proiectului (ListAgents pune radacinile primele).
func (t *Team) rootOf(projectID int64) (store.Agent, bool, error) {
	agents, err := t.st.ListAgents(projectID)
	if err != nil {
		return store.Agent{}, false, err
	}
	for _, a := range agents {
		if store.RolesOf(a, agents).IsRoot {
			return a, true, nil
		}
	}
	return store.Agent{}, false, nil
}

// projectName: numele proiectului, pentru texte; un proiect sters devine "a deleted project".
func (t *Team) projectName(id int64) string {
	p, err := t.st.GetProject(id)
	if err != nil {
		return "a deleted project"
	}
	return p.Name
}

// agentNameOf: numele agentului (nil sau sters = "a deleted agent").
func (t *Team) agentNameOf(id *int64) string {
	if id == nil {
		return "a deleted agent"
	}
	a, err := t.st.GetAgent(*id)
	if err != nil {
		return "a deleted agent"
	}
	return a.Name
}

// parseRequestRef citeste "Q-7", "q-7" sau "7".
func parseRequestRef(ref, prefix string) (int64, error) {
	s := strings.ToUpper(strings.TrimSpace(ref))
	s = strings.TrimPrefix(s, prefix+"-")
	id, err := strconv.ParseInt(s, 10, 64)
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("invalid reference %q: use %s-<number>", ref, prefix)
	}
	return id, nil
}

// emitRequest anunta UI-ul ambelor proiecte ca o cerere s-a schimbat.
func (t *Team) emitRequest(r store.ProjectRequest) {
	t.emit(r.FromProject, "project_request", r)
	t.emit(r.ToProject, "project_request", r)
}

// ListLinkedProjects e tool-ul list_linked_projects.
func (t *Team) ListLinkedProjects(c mcp.Caller) (string, error) {
	links, err := t.st.LinkedProjects(c.ProjectID)
	if err != nil {
		return "", err
	}
	if len(links) == 0 {
		return "This project has no linked projects.", nil
	}
	var b strings.Builder
	b.WriteString("Linked projects:")
	for _, l := range links {
		root := "no root agent"
		if r, ok, err := t.rootOf(l.ID); err == nil && ok {
			root = "root: " + r.Name
		}
		fmt.Fprintf(&b, "\n- %s (%s): %s", l.Name, root, orDefault(strings.TrimSpace(l.Goal), "no goal set"))
	}
	return b.String(), nil
}

// AskProject e tool-ul ask_project: intrebarea ajunge ca nota la radacina proiectului legat.
func (t *Team) AskProject(c mcp.Caller, project, question string) (string, error) {
	t.work.Lock()
	defer t.work.Unlock()
	if t.answeringOf(c.AgentID, c.RunID) {
		return "", errors.New("you are answering a question from another project; answer with what you know, or say you don't know")
	}
	question = strings.TrimSpace(question)
	if question == "" {
		return "", errors.New("question is required")
	}
	if utf8.RuneCountInString(question) > 2000 {
		return "", errors.New("the question is too long: at most 2000 characters")
	}
	target, err := t.linkedTarget(c.ProjectID, project)
	if err != nil {
		return "", err
	}
	asker, err := t.st.GetAgent(c.AgentID)
	if err != nil {
		return "", errors.New("your agent no longer exists")
	}
	n, err := t.st.CountOpenQuestionsFrom(asker.ID)
	if err != nil {
		return "", err
	}
	if n >= MaxOpenQuestions {
		return "", fmt.Errorf("you already have %d open questions to other projects; wait for the answers", MaxOpenQuestions)
	}
	root, ok, err := t.rootOf(target.ID)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", fmt.Errorf("%q has no root agent to receive it", target.Name)
	}
	askerID := asker.ID
	r := store.ProjectRequest{Kind: store.RequestQuestion, FromProject: c.ProjectID, FromAgent: &askerID,
		ToProject: target.ID, Text: question, Status: store.ReqOpen}
	if c.TaskID != 0 {
		id := c.TaskID
		r.FromTask = &id
	}
	if err := t.st.AddProjectRequest(&r); err != nil {
		return "", err
	}
	text := fmt.Sprintf("Question from %s (%s) [%s]: %s\nAnswer it with reply_to_project(request: %q, answer).",
		t.projectName(c.ProjectID), asker.Name, r.Ref(), question, r.Ref())
	m := store.Message{ProjectID: target.ID, AgentID: root.ID, Kind: store.KindEvent, Text: clip(text), RequestID: &r.ID}
	if err := t.st.AddMessage(&m); err != nil {
		return "", err
	}
	t.emit(target.ID, "message", m)
	t.emitRequest(r)
	t.Kick(target.ID)
	out := fmt.Sprintf("Question sent to %s as %s. The answer arrives as a message; continue meanwhile.", target.Name, r.Ref())
	if p, err := t.st.GetProject(target.ID); err == nil && p.Paused {
		out += fmt.Sprintf(" %s is paused: it answers when the user starts it.", target.Name)
	}
	return out, nil
}

// ReplyToProject e tool-ul reply_to_project: raspunsul ajunge ca nota la cel care a intrebat.
func (t *Team) ReplyToProject(c mcp.Caller, request, answer string) (string, error) {
	t.work.Lock()
	defer t.work.Unlock()
	id, err := parseRequestRef(request, "Q")
	if err != nil {
		return "", err
	}
	answer = strings.TrimSpace(answer)
	if answer == "" {
		return "", errors.New("answer is required")
	}
	if utf8.RuneCountInString(answer) > 10000 {
		return "", errors.New("the answer is too long: at most 10000 characters")
	}
	agents, err := t.st.ListAgents(c.ProjectID)
	if err != nil {
		return "", err
	}
	me, ok := store.FindAgent(agents, c.AgentID)
	if !ok || !store.RolesOf(me, agents).IsRoot {
		return "", errors.New("only the root agent of this project answers questions from linked projects")
	}
	r, err := t.st.GetProjectRequest(id)
	if errors.Is(err, store.ErrNotFound) {
		return "", fmt.Errorf("there is no question Q-%d", id)
	}
	if err != nil {
		return "", err
	}
	if r.Kind != store.RequestQuestion || r.ToProject != c.ProjectID {
		return "", fmt.Errorf("%s is not a question to this project", r.Ref())
	}
	if r.Status != store.ReqOpen {
		return "", fmt.Errorf("%s is already closed (%s)", r.Ref(), r.Status)
	}
	if err := t.st.TransitionProjectRequest(r.ID, store.ReqOpen, store.ReqAnswered, answer, nil); err != nil {
		return "", err
	}
	r.Status, r.Answer = store.ReqAnswered, answer
	if r.FromAgent != nil {
		t.note(r.FromProject, *r.FromAgent, clip(fmt.Sprintf("Answer from %s to %s: %s", t.projectName(c.ProjectID), r.Ref(), answer)), false)
		t.Kick(r.FromProject)
	}
	t.emitRequest(r)
	return fmt.Sprintf("Answer sent for %s.", r.Ref()), nil
}
```

Note: `orDefault` si `clip` exista deja in pachet. Ordinea verificarilor in `ReplyToProject` (rolul inainte de cerere) e ceruta de `TestAskProjectRefusals` (workerul primeste "only the root agent" chiar pentru o referinta oarecare). `Kick` ia doar `t.mu`, deci se poate chema sub `t.work`.

- [ ] **Step 6:** `go test ./internal/team/` si `go test -race ./internal/team/` -> PASS (inclusiv testele existente).
- [ ] **Step 7:** `gofmt -l internal/team` gol; commit: `team: intrebari intre proiecte legate`

---

### Task 4: Team: cereri de lucru, livrare si ciclul de viata

**Files:**
- Modify: `internal/team/links.go` (inlocuieste stub-ul `RequestProjectWork`; adauga restul)
- Modify: `internal/team/stages.go` (`userCreateTask` primeste `taskOrigin`)
- Modify: `internal/team/recurring.go` (cele doua apeluri `userCreateTask`)
- Modify: `internal/team/tools.go` (`afterDone` livreaza; `ReportDone` pentru radacina pe task cerut)
- Modify: `internal/team/links_test.go`

**Interfaces:**
- Consumes: Task 1, Task 3 (`linkedTarget`, `rootOf`, `projectName`, `agentNameOf`, `emitRequest`, `ProjectRequestError`).
- Produces:
  - `type taskOrigin struct { recurring *store.RecurringTask; request *store.ProjectRequest; from, by string }`
  - `func (t *Team) userCreateTask(projectID int64, title, description string, agentID int64, origin taskOrigin) (store.Task, error)`
  - `(*Team) RequestProjectWork(c mcp.Caller, project, title, description string) (string, error)`
  - `(*Team) ApproveProjectRequest(id int64) error` (`store.ErrNotFound`; `store.ErrConflict` daca nu e o cerere de lucru `pending`; `ProjectRequestError` daca legatura nu mai exista sau tinta nu are radacina)
  - `(*Team) DismissProjectRequest(id int64) error`
  - `(*Team) UnlinkProjects(a, b int64) error` (`store.ErrNotFound`)
  - `(*Team) BeforeDeleteProject(id int64) error`

- [ ] **Step 1: Testele** (adauga in `links_test.go`; scoate linia `var _ = errors.Is` din Task 3):

```go
func TestRequestWorkNeedsApproval(t *testing.T) {
	f := setup(t)
	bp, broot := linked(t, f)
	autonomous(t, f) // Full autonomy nu face cererea directa
	text, err := f.tm.RequestProjectWork(f.leadCaller(), "Backend", "Add /export", "CSV with id and name")
	if err != nil || !strings.Contains(text, "sent to Backend; the user approves it first") {
		t.Fatalf("%q %v", text, err)
	}
	reqs, _ := f.st.ListPendingWork(bp.ID)
	if len(reqs) != 1 || reqs[0].Title != "Add /export" || reqs[0].Status != store.ReqPending {
		t.Fatalf("%+v", reqs)
	}
	if tasks, _ := f.st.ListTasks(bp.ID); len(tasks) != 0 {
		t.Fatal("fara aprobare nu exista task")
	}
	if err := f.tm.ApproveProjectRequest(reqs[0].ID); err != nil {
		t.Fatal(err)
	}
	tasks, _ := f.st.ListTasks(bp.ID)
	if len(tasks) != 1 {
		t.Fatalf("un task: %+v", tasks)
	}
	task := tasks[0]
	if task.AssigneeID != broot.ID || task.RequestID == nil || *task.RequestID != reqs[0].ID || task.Title != "Add /export" ||
		!strings.HasSuffix(task.Description, "Requested by ApiUnit (Lead) as "+reqs[0].Ref()+".") {
		t.Fatalf("task: %+v", task)
	}
	if !hasEvent(t, f, task.ID, "created", "ApiUnit", "Requested by ApiUnit (Lead)") {
		t.Fatal("pasul created")
	}
	r, _ := f.st.GetProjectRequest(reqs[0].ID)
	if r.Status != store.ReqApproved || r.ToTask == nil || *r.ToTask != task.ID {
		t.Fatalf("%+v", r)
	}
	if !f.hasMessage(f.lead.ID, "Backend accepted "+r.Ref()+" (task "+task.Ref()+" there).") {
		t.Fatal("nota la lider")
	}
	// dublu-click
	if err := f.tm.ApproveProjectRequest(reqs[0].ID); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("a doua aprobare: %v", err)
	}
}

func TestRequestWorkDirectFromChat(t *testing.T) {
	f := setup(t)
	bp, _ := linked(t, f)
	if _, err := f.tm.UserMessage(f.lead.ID, "ask Backend to add /export", nil); err != nil {
		t.Fatal(err)
	}
	mustReconcile(t, f)
	run := f.fs.last(t, f.lead)
	text, err := f.tm.RequestProjectWork(f.caller(t, run), "Backend", "Add /export", "")
	if err != nil || !strings.Contains(text, "Backend got your request as W-") || !strings.Contains(text, "(task T-1)") {
		t.Fatalf("%q %v", text, err)
	}
	if tasks, _ := f.st.ListTasks(bp.ID); len(tasks) != 1 {
		t.Fatalf("task direct: %+v", tasks)
	}
	f.fs.finish(run.ID, runner.Outcome{Status: "done"})
}

func TestRequestWorkNotDirectAfterQuestionReply(t *testing.T) {
	f := setup(t)
	bp, _ := linked(t, f)
	if _, err := f.tm.AskUser(f.leadCaller(), "Which DB?", nil); err != nil {
		t.Fatal(err)
	}
	qs, _ := f.st.ListOpenQuestions(f.p.ID)
	if err := f.tm.DismissQuestion(qs[0].ID); err != nil {
		t.Fatal(err)
	}
	mustReconcile(t, f)
	run := f.fs.last(t, f.lead)
	text, err := f.tm.RequestProjectWork(f.caller(t, run), "Backend", "Add /export", "")
	if err != nil || !strings.Contains(text, "the user approves it first") {
		t.Fatalf("%q %v", text, err)
	}
	if tasks, _ := f.st.ListTasks(bp.ID); len(tasks) != 0 {
		t.Fatal("un raspuns la ask_user nu e cerere din chat")
	}
	f.fs.finish(run.ID, runner.Outcome{Status: "done"})
}

func TestRequestWorkRefusals(t *testing.T) {
	f := setup(t)
	linked(t, f)
	if _, err := f.tm.RequestProjectWork(f.workerCaller(f.rook, 0), "Backend", "x", ""); err == nil || !strings.Contains(err.Error(), "only a lead") {
		t.Fatalf("worker: %v", err)
	}
	if _, err := f.tm.RequestProjectWork(f.leadCaller(), "Backend", " ", ""); err == nil || !strings.Contains(err.Error(), "title is required") {
		t.Fatalf("titlu: %v", err)
	}
	if _, err := f.tm.RequestProjectWork(f.leadCaller(), "Backend", strings.Repeat("x", 201), ""); err == nil || !strings.Contains(err.Error(), "at most 200") {
		t.Fatalf("titlu lung: %v", err)
	}
	if _, err := f.tm.RequestProjectWork(f.leadCaller(), "Nope", "x", ""); err == nil || !strings.Contains(err.Error(), "is not a linked project") {
		t.Fatalf("nelegat: %v", err)
	}
}

// approvedTask face o cerere, o aproba si intoarce task-ul din Backend (pus In progress, ca intr-un run pe el).
func approvedTask(t *testing.T, f *fixture, c mcp.Caller) (store.Project, store.Agent, store.Task, store.ProjectRequest) {
	t.Helper()
	bp, broot := linked(t, f)
	if _, err := f.tm.RequestProjectWork(c, "Backend", "Add /export", "CSV"); err != nil {
		t.Fatal(err)
	}
	reqs, _ := f.st.ListPendingWork(bp.ID)
	if err := f.tm.ApproveProjectRequest(reqs[0].ID); err != nil {
		t.Fatal(err)
	}
	tasks, _ := f.st.ListTasks(bp.ID)
	task := tasks[0]
	task.Status = store.StatusInProgress
	if err := f.st.SaveTask(&task); err != nil {
		t.Fatal(err)
	}
	r, _ := f.st.GetProjectRequest(reqs[0].ID)
	return bp, broot, task, r
}

func TestRootReportDoneDelivers(t *testing.T) {
	f := setup(t)
	lt := f.task(t, "Export page", f.lead, store.StatusInProgress)
	c := f.leadCaller()
	c.TaskID = lt.ID
	bp, broot, task, r := approvedTask(t, f, c)
	rc := mcp.Caller{ProjectID: bp.ID, AgentID: broot.ID, TaskID: task.ID, IsRoot: true, FromRequest: true}
	text, err := f.tm.ReportDone(rc, "", "Endpoint /export added")
	if err != nil || !strings.Contains(text, task.Ref()+" is Done; the report went back to ApiUnit.") {
		t.Fatalf("%q %v", text, err)
	}
	got, _ := f.st.GetTask(task.ID)
	if got.Status != store.StatusDone || got.Result != "Endpoint /export added" {
		t.Fatalf("%+v", got)
	}
	r, _ = f.st.GetProjectRequest(r.ID)
	if r.Status != store.ReqDone || r.Answer != "Endpoint /export added" {
		t.Fatalf("%+v", r)
	}
	want := "Backend finished " + r.Ref() + ": Endpoint /export added (your task " + lt.Ref() + ")"
	if !f.hasMessage(f.lead.ID, want) {
		t.Fatalf("nota la lider: %q", want)
	}
}

func TestRootReportDoneWaitsForSubtasks(t *testing.T) {
	f := setup(t)
	bp, broot, task, _ := approvedTask(t, f, f.leadCaller())
	sub := store.Task{ProjectID: bp.ID, Title: "part", AssigneeID: broot.ID, ParentTaskID: &task.ID}
	if err := f.st.CreateTask(&sub); err != nil {
		t.Fatal(err)
	}
	rc := mcp.Caller{ProjectID: bp.ID, AgentID: broot.ID, TaskID: task.ID, IsRoot: true, FromRequest: true}
	if _, err := f.tm.ReportDone(rc, "", "x"); err == nil || !strings.Contains(err.Error(), "still has open subtasks") {
		t.Fatalf("%v", err)
	}
}

func TestUserAcceptDeliversToo(t *testing.T) {
	f := setup(t)
	_, _, task, r := approvedTask(t, f, f.leadCaller())
	// radacina a terminat fara report_done: task-ul e in Review la utilizator, care il accepta
	task.Status, task.ReviewStage, task.Result = store.StatusReview, store.StageLead, "Done without report_done"
	if err := f.st.SaveTask(&task); err != nil {
		t.Fatal(err)
	}
	if err := f.tm.UserDecide(task.ID, "accept", ""); err != nil {
		t.Fatalf("accept: %v", err)
	}
	r, _ = f.st.GetProjectRequest(r.ID)
	if r.Status != store.ReqDone || !f.hasMessage(f.lead.ID, "Backend finished "+r.Ref()+": Done without report_done") {
		t.Fatalf("livrare la accept: %+v", r)
	}
}

func TestRequestWorkStagedTarget(t *testing.T) {
	f := setup(t)
	bp, broot := linked(t, f)
	if _, err := f.st.SaveStages(bp.ID, []store.Stage{{Name: "Build", Kind: "work", AgentIDs: []int64{broot.ID}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.tm.RequestProjectWork(f.leadCaller(), "Backend", "Add /export", ""); err != nil {
		t.Fatal(err)
	}
	reqs, _ := f.st.ListPendingWork(bp.ID)
	if err := f.tm.ApproveProjectRequest(reqs[0].ID); err != nil {
		t.Fatal(err)
	}
	tasks, _ := f.st.ListTasks(bp.ID)
	if len(tasks) != 1 || tasks[0].StageID == nil || tasks[0].RequestID == nil {
		t.Fatalf("task in prima etapa: %+v", tasks)
	}
}

func TestDismissProjectRequest(t *testing.T) {
	f := setup(t)
	bp, _ := linked(t, f)
	f.tm.RequestProjectWork(f.leadCaller(), "Backend", "Add /export", "")
	reqs, _ := f.st.ListPendingWork(bp.ID)
	if err := f.tm.DismissProjectRequest(reqs[0].ID); err != nil {
		t.Fatal(err)
	}
	if !f.hasMessage(f.lead.ID, "The user dismissed your request "+reqs[0].Ref()+" to Backend.") {
		t.Fatal("nota la lider")
	}
	if err := f.tm.DismissProjectRequest(reqs[0].ID); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("a doua oara: %v", err)
	}
}

func TestApproveAfterUnlink(t *testing.T) {
	f := setup(t)
	bp, _ := linked(t, f)
	f.tm.RequestProjectWork(f.leadCaller(), "Backend", "Add /export", "")
	reqs, _ := f.st.ListPendingWork(bp.ID)
	// legatura stearsa direct din store (fara ciclul de viata): aprobarea trebuie refuzata
	if err := f.st.UnlinkProjects(f.p.ID, bp.ID); err != nil {
		t.Fatal(err)
	}
	var pre ProjectRequestError
	if err := f.tm.ApproveProjectRequest(reqs[0].ID); !errors.As(err, &pre) {
		t.Fatalf("%v", err)
	}
}

func TestUnlinkClosesOpenRequests(t *testing.T) {
	f := setup(t)
	bp, _ := linked(t, f)
	f.tm.AskProject(f.workerCaller(f.rook, 0), "Backend", "Which format?")
	f.tm.RequestProjectWork(f.leadCaller(), "Backend", "Add /export", "")
	if err := f.tm.UnlinkProjects(bp.ID, f.p.ID); err != nil {
		t.Fatal(err)
	}
	if ok, _ := f.st.AreLinked(f.p.ID, bp.ID); ok {
		t.Fatal("legatura trebuia stearsa")
	}
	if !f.hasMessage(f.rook.ID, "to Backend was closed: the projects are no longer linked.") ||
		!f.hasMessage(f.lead.ID, "to Backend was closed: the projects are no longer linked.") {
		t.Fatal("notele")
	}
	if n, _ := f.st.CountPendingWork(bp.ID); n != 0 {
		t.Fatal("cererea neaprobata trebuia respinsa")
	}
	if err := f.tm.UnlinkProjects(bp.ID, f.p.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("a doua oara: %v", err)
	}
}

func TestBeforeDeleteProjectNotifies(t *testing.T) {
	f := setup(t)
	bp, _ := linked(t, f)
	f.tm.AskProject(f.workerCaller(f.rook, 0), "Backend", "Which format?")
	if err := f.tm.BeforeDeleteProject(bp.ID); err != nil {
		t.Fatal(err)
	}
	if !f.hasMessage(f.rook.ID, "to Backend was closed: the project was deleted.") {
		t.Fatal("nota")
	}
}
```

Inainte de Step 2 verifica numele reale si adapteaza DOAR apelurile (nu asertiunile): actiunea utilizatorului de accept din board (cauta in `internal/team` functia chemata de `POST /api/tasks/{id}/decide`; testul o numeste `UserDecide(taskID, "accept", "")`), `store.SaveStages` / campurile `store.Stage`, `f.tm.AskUser` si `f.tm.DismissQuestion`.

- [ ] **Step 2:** `go test ./internal/team/ -run 'RequestWork|RootReport|UserAccept|Dismiss|Unlink|BeforeDelete|ApproveAfter'` -> FAIL.

- [ ] **Step 3: `taskOrigin` in `stages.go`.** Inlocuieste parametrul `recurring *store.RecurringTask` al lui `userCreateTask` cu `origin taskOrigin`:

```go
// taskOrigin: de unde vine un task creat pe drumul utilizatorului.
// Gol = din board; recurring = o sarcina recurenta; request = o cerere de lucru
// dintr-un proiect legat (from = proiectul sursa, by = agentul care a cerut).
type taskOrigin struct {
	recurring *store.RecurringTask
	request   *store.ProjectRequest
	from, by  string
}
```

In corp: `created := "Created by you"` si `actor := actorUser`; blocul recurent devine `if origin.recurring != nil { task.RecurringID, task.AutoAccept = &origin.recurring.ID, origin.recurring.AutoAccept; created = ... origin.recurring.Title }`; adauga:

```go
	if origin.request != nil {
		task.RequestID = &origin.request.ID
		created = fmt.Sprintf("Requested by %s (%s)", origin.from, origin.by)
		actor = origin.from
	}
```

iar `t.taskMsg(task, "created", actorUser, to, created, ...)` foloseste `actor`. `UserCreateTask` cheama `t.userCreateTask(projectID, title, description, agentID, taskOrigin{})`; in `recurring.go` cele doua apeluri devin `taskOrigin{recurring: &r}`.

- [ ] **Step 4: `links.go`** (inlocuieste stub-ul si adauga):

```go
// RequestProjectWork e tool-ul request_project_work: direct intr-un run pornit
// din chat-ul utilizatorului, altfel o cerere in "Needs you" la proiectul tinta.
func (t *Team) RequestProjectWork(c mcp.Caller, project, title, description string) (string, error) {
	t.work.Lock()
	defer t.work.Unlock()
	agents, err := t.st.ListAgents(c.ProjectID)
	if err != nil {
		return "", err
	}
	lead, ok := store.FindAgent(agents, c.AgentID)
	if !ok {
		return "", errors.New("your agent no longer exists")
	}
	if !canAddAgents(lead, agents) {
		return "", errors.New("only a lead or an agent that reports to the user can request work from another project")
	}
	title, description = strings.TrimSpace(title), strings.TrimSpace(description)
	switch {
	case title == "":
		return "", errors.New("title is required")
	case utf8.RuneCountInString(title) > 200:
		return "", errors.New("title: at most 200 characters")
	case utf8.RuneCountInString(description) > 10000:
		return "", errors.New("description: at most 10000 characters")
	}
	target, err := t.linkedTarget(c.ProjectID, project)
	if err != nil {
		return "", err
	}
	if _, ok, err := t.rootOf(target.ID); err != nil {
		return "", err
	} else if !ok {
		return "", fmt.Errorf("%q has no root agent to receive it", target.Name)
	}
	leadID := lead.ID
	r := store.ProjectRequest{Kind: store.RequestWork, FromProject: c.ProjectID, FromAgent: &leadID,
		ToProject: target.ID, Title: title, Text: description, Status: store.ReqPending}
	if c.TaskID != 0 {
		id := c.TaskID
		r.FromTask = &id
	}
	if err := t.st.AddProjectRequest(&r); err != nil {
		return "", err
	}
	// doar o cerere scrisa de utilizator in chat merge direct (Full autonomy nu)
	if _, fromChat := t.requestOf(c.AgentID, c.RunID); fromChat {
		task, err := t.startRequestedWork(&r)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("%s got your request as %s (task %s). You will get its report as a message.", target.Name, r.Ref(), task.Ref()), nil
	}
	t.emitRequest(r)
	return fmt.Sprintf("Request %s sent to %s; the user approves it first. You will get a message either way.", r.Ref(), target.Name), nil
}

// startRequestedWork creeaza task-ul cererii in proiectul tinta (pentru radacina,
// sau in prima etapa) si marcheaza cererea aprobata. Se cheama cu t.work tinut.
func (t *Team) startRequestedWork(r *store.ProjectRequest) (store.Task, error) {
	root, ok, err := t.rootOf(r.ToProject)
	if err != nil {
		return store.Task{}, err
	}
	if !ok {
		return store.Task{}, ProjectRequestError(fmt.Sprintf("%s has no root agent to receive it", t.projectName(r.ToProject)))
	}
	stages, err := t.st.ListStages(r.ToProject)
	if err != nil {
		return store.Task{}, err
	}
	agentID := root.ID
	if len(stages) > 0 {
		agentID = 0 // intra in prima etapa, ca un task din board
	}
	from, by := t.projectName(r.FromProject), t.agentNameOf(r.FromAgent)
	desc := strings.TrimSpace(r.Text + fmt.Sprintf("\n\nRequested by %s (%s) as %s.", from, by, r.Ref()))
	task, err := t.userCreateTask(r.ToProject, r.Title, desc, agentID, taskOrigin{request: r, from: from, by: by})
	if err != nil {
		return store.Task{}, err
	}
	if err := t.st.TransitionProjectRequest(r.ID, store.ReqPending, store.ReqApproved, "", &task.ID); err != nil {
		return store.Task{}, err
	}
	r.Status, r.ToTask = store.ReqApproved, &task.ID
	t.emitRequest(*r)
	return task, nil
}

// ApproveProjectRequest: utilizatorul aproba o cerere de lucru din "Needs you".
func (t *Team) ApproveProjectRequest(id int64) error {
	t.work.Lock()
	defer t.work.Unlock()
	r, err := t.st.GetProjectRequest(id)
	if err != nil {
		return err
	}
	if r.Kind != store.RequestWork || r.Status != store.ReqPending {
		return store.ErrConflict
	}
	linked, err := t.st.AreLinked(r.FromProject, r.ToProject)
	if err != nil {
		return err
	}
	if !linked {
		return ProjectRequestError("the projects are no longer linked")
	}
	task, err := t.startRequestedWork(&r)
	if err != nil {
		return err
	}
	if r.FromAgent != nil {
		t.note(r.FromProject, *r.FromAgent, fmt.Sprintf("%s accepted %s (task %s there).", t.projectName(r.ToProject), r.Ref(), task.Ref()), false)
		t.Kick(r.FromProject)
	}
	return nil
}

// DismissProjectRequest: utilizatorul respinge o cerere de lucru.
func (t *Team) DismissProjectRequest(id int64) error {
	t.work.Lock()
	defer t.work.Unlock()
	r, err := t.st.GetProjectRequest(id)
	if err != nil {
		return err
	}
	if r.Kind != store.RequestWork {
		return store.ErrConflict
	}
	if err := t.st.TransitionProjectRequest(r.ID, store.ReqPending, store.ReqDismissed, "", nil); err != nil {
		return err
	}
	r.Status = store.ReqDismissed
	if r.FromAgent != nil {
		t.note(r.FromProject, *r.FromAgent, fmt.Sprintf("The user dismissed your request %s to %s.", r.Ref(), t.projectName(r.ToProject)), false)
		t.Kick(r.FromProject)
	}
	t.emitRequest(r)
	return nil
}

// deliverRequest: un task cerut de un proiect legat a ajuns in Done; raportul
// pleaca inapoi la cel care a cerut. Se cheama din afterDone, cu t.work tinut.
func (t *Team) deliverRequest(task store.Task) error {
	r, err := t.st.GetProjectRequest(*task.RequestID)
	if errors.Is(err, store.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if r.Status != store.ReqApproved {
		return nil
	}
	report := orDefault(strings.TrimSpace(task.Result), "(no report)")
	if err := t.st.TransitionProjectRequest(r.ID, store.ReqApproved, store.ReqDone, report, nil); err != nil {
		return err
	}
	r.Status, r.Answer = store.ReqDone, report
	if r.FromAgent != nil {
		text := fmt.Sprintf("%s finished %s: %s", t.projectName(task.ProjectID), r.Ref(), report)
		if r.FromTask != nil {
			if ft, err := t.st.GetTask(*r.FromTask); err == nil {
				text += fmt.Sprintf(" (your task %s)", ft.Ref())
			}
		}
		t.note(r.FromProject, *r.FromAgent, clip(text), false)
		t.Kick(r.FromProject)
	}
	t.emitRequest(r)
	return nil
}

// finishRequestedTask: radacina livreaza cu report_done un task cerut de un
// proiect legat. Nu are sef, deci task-ul trece direct in Done. Cu t.work tinut.
func (t *Team) finishRequestedTask(task *store.Task, root store.Agent, summary string) (string, error) {
	subs, err := t.st.Subtasks(task.ID)
	if err != nil {
		return "", err
	}
	var open []string
	for _, s := range subs {
		if s.Status != store.StatusDone {
			open = append(open, s.Ref())
		}
	}
	if len(open) > 0 {
		return "", fmt.Errorf("%s still has open subtasks: %s. Report after they are done.", task.Ref(), strings.Join(open, ", "))
	}
	r, err := t.st.GetProjectRequest(*task.RequestID)
	if err != nil {
		return "", err
	}
	from := t.projectName(r.FromProject)
	task.Result = summary
	markDone(task)
	if err := t.st.SaveTask(task); err != nil {
		return "", err
	}
	t.taskMsg(*task, "reported", root.Name, from, root.Name+" reported it done", summary)
	t.note(task.ProjectID, root.ID, "Reported "+task.Ref()+" to "+from, true)
	t.emit(task.ProjectID, "task", *task)
	if err := t.afterDone(*task); err != nil {
		log.Printf("after report %s: %v", task.Ref(), err)
	}
	t.Kick(task.ProjectID)
	return fmt.Sprintf("%s is Done; the report went back to %s.", task.Ref(), from), nil
}

// UnlinkProjects sterge legatura; intrebarile deschise se inchid, cererile
// neaprobate se resping, iar cei care au cerut afla. Cererile aprobate continua.
func (t *Team) UnlinkProjects(a, b int64) error {
	t.work.Lock()
	defer t.work.Unlock()
	reqs, err := t.st.OpenRequestsBetween(a, b)
	if err != nil {
		return err
	}
	if err := t.st.UnlinkProjects(a, b); err != nil {
		return err
	}
	for _, r := range reqs {
		t.closeOpenRequest(r, "the projects are no longer linked")
	}
	return nil
}

// BeforeDeleteProject anunta pe cei care asteptau ceva de la proiect (intrebari,
// cereri neaprobate sau in lucru); se cheama inainte de stergerea proiectului.
func (t *Team) BeforeDeleteProject(id int64) error {
	t.work.Lock()
	defer t.work.Unlock()
	reqs, err := t.st.OpenRequestsTo(id)
	if err != nil {
		return err
	}
	for _, r := range reqs {
		t.closeOpenRequest(r, "the project was deleted")
	}
	return nil
}

// closeOpenRequest inchide o cerere care inca astepta (open/approved -> closed,
// pending -> dismissed) si ii spune celui care a cerut de ce. Cu t.work tinut.
func (t *Team) closeOpenRequest(r store.ProjectRequest, why string) {
	to := store.ReqClosed
	if r.Status == store.ReqPending {
		to = store.ReqDismissed
	}
	if err := t.st.TransitionProjectRequest(r.ID, r.Status, to, "", nil); err != nil {
		log.Printf("close %s: %v", r.Ref(), err)
		return
	}
	r.Status = to
	if r.FromAgent != nil {
		t.note(r.FromProject, *r.FromAgent, fmt.Sprintf("%s to %s was closed: %s.", r.Ref(), t.projectName(r.ToProject), why), false)
		t.Kick(r.FromProject)
	}
	t.emitRequest(r)
}
```

(adauga `"log"` la importuri.)

- [ ] **Step 5: `tools.go`.**
  - In `afterDone`, primele linii (inainte de `if task.ParentTaskID == nil`):
    ```go
    	// un task cerut de un proiect legat isi trimite raportul inapoi, pe orice drum ar ajunge in Done
    	if task.RequestID != nil {
    		if err := t.deliverRequest(task); err != nil {
    			log.Printf("deliver %s: %v", task.Ref(), err)
    		}
    	}
    ```
  - In `ReportDone`, imediat inainte de `if worker.ParentID == nil {` (cel de dupa blocul cu etape):
    ```go
    	// radacina livreaza munca ceruta de un proiect legat: nu are sef, task-ul trece direct in Done
    	if worker.ParentID == nil && task.RequestID != nil {
    		return t.finishRequestedTask(&task, worker, summary)
    	}
    ```
  - Verifica toate drumurile care pun un task in Done fara `afterDone` (cauta `StatusDone` in `internal/team`, de ex. `stages.go` ~312): daca vreunul nu ajunge la `afterDone`, raporteaza-l ca DONE_WITH_CONCERNS (nu schimba comportamentul etapelor in acest task).

- [ ] **Step 6:** `go test ./internal/team/` si `go test -race ./internal/team/` -> PASS (inclusiv recurentele si testele existente de create task).
- [ ] **Step 7:** `gofmt -l internal/team` gol; commit: `team: cereri de lucru intre proiecte, livrare si ciclul de viata`

---

### Task 5: Prompt: paragrafele pentru proiecte legate

**Files:**
- Modify: `internal/prompt/prompt.go` (`Links.Linked`, `System`)
- Modify: `internal/prompt/prompt_test.go`
- Modify: `internal/team/run.go` (`Linked` din `hasLinks`)

**Interfaces:**
- Consumes: Task 2 (`hasLinks` in `run.go`).
- Produces: `Links.Linked bool`.

- [ ] **Step 1: Testul** (in stilul lui `TestSystemAddAgents`, cu `ptrID`):

```go
func TestSystemLinked(t *testing.T) {
	p := store.Project{Name: "P", Goal: "G"}
	lead := store.Agent{ID: 1, Name: "Lead"}
	worker := store.Agent{ID: 2, Name: "Rook", ParentID: ptrID(1)}
	team := []store.Agent{lead, worker}
	ll := LinksFor(lead, team)
	if s := System(p, lead, ll); strings.Contains(s, "list_linked_projects") {
		t.Fatal("fara legaturi nu apare nimic")
	}
	ll.Linked = true
	s := System(p, lead, ll)
	for _, want := range []string{"mcp__team__list_linked_projects", "mcp__team__ask_project", "mcp__team__request_project_work", "mcp__team__reply_to_project"} {
		if !strings.Contains(s, want) {
			t.Fatalf("radacina/liderul trebuie sa afle de %s:\n%s", want, s)
		}
	}
	wl := LinksFor(worker, team)
	wl.Linked = true
	s = System(p, worker, wl)
	if !strings.Contains(s, "mcp__team__ask_project") || strings.Contains(s, "request_project_work") || strings.Contains(s, "reply_to_project") {
		t.Fatalf("workerul doar intreaba:\n%s", s)
	}
}
```

- [ ] **Step 2:** `go test ./internal/prompt/` -> FAIL.
- [ ] **Step 3: Implementarea.** In `Links`, dupa `CanAddAgents`: `Linked bool // proiectul are legaturi cu alte proiecte (se seteaza in team/run.go)`. Langa `howAddAgents`:

```go
const howLinked = `This project is linked with other projects. Use mcp__team__list_linked_projects to see them. ` +
	`To ask one of them something, use mcp__team__ask_project; the answer arrives later as a message. ` +
	`Only ask what your own team cannot answer.`

const howLinkedLead = `To have a linked project do work for you, use mcp__team__request_project_work.`

const howLinkedRoot = `Questions from linked projects arrive as messages with a reference like Q-7: answer each with ` +
	`mcp__team__reply_to_project. Work requested by a linked project arrives as a task: when it is done, call ` +
	`mcp__team__report_done on it.`
```

In `System`, dupa blocul `if l.CanAddAgents { how = append(how, howAddAgents) }`:

```go
	if l.Linked {
		how = append(how, howLinked)
		if l.CanAddAgents {
			how = append(how, howLinkedLead)
		}
		if isRoot {
			how = append(how, howLinkedRoot)
		}
	}
```

(`isRoot` e variabila deja calculata in `System`; verifica numele exact.) In `team/run.go` inlocuieste `system := prompt.System(p, a, prompt.LinksFor(a, agents))` cu:

```go
	pl := prompt.LinksFor(a, agents)
	pl.Linked = hasLinks // hasLinks e calculat mai sus din linkedProjects
	system := prompt.System(p, a, pl)
```

- [ ] **Step 4:** `go test ./internal/prompt/ ./internal/team/` -> PASS; `go vet ./...`.
- [ ] **Step 5:** commit: `prompt: agentii afla de proiectele legate`

---

### Task 6: Web: legaturi, cereri, campuri noi

**Files:**
- Create: `internal/web/api_links.go`
- Create: `internal/web/links_test.go`
- Modify: `internal/web/server.go` (rute, `fail`)
- Modify: `internal/web/api_projects.go` (card, detaliu, `taskView`, `deleteProject`)

**Interfaces:**
- Consumes: Task 1 (store), Task 4 (`team.UnlinkProjects`, `ApproveProjectRequest`, `DismissProjectRequest`, `BeforeDeleteProject`, `ProjectRequestError`).
- Produces:
  - `GET /api/projects/{id}/links` -> `[{projectId, name}]`
  - `POST /api/projects/{id}/links` `{projectId}` -> 201 `{projectId, name}`; 400 acelasi proiect; 404 inexistent; 409 deja legate
  - `DELETE /api/projects/{id}/links/{other}` -> 204; 404 fara legatura
  - `POST /api/project-requests/{id}/approve` / `dismiss` -> 204; 404; 409
  - detaliu: `links`, `workRequests: [{id, ref, fromProject, fromAgent, title, text, createdAt}]`; card: `workRequests` (numar); task-uri: `requestId`, `fromProject`

- [ ] **Step 1: Testele** (`links_test.go`, cu `setup`, `e.do`, `e.createProject`, `e.root`, `itoa` din `server_test.go`):

```go
package web

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"foreman/internal/mcp"
)

func TestProjectLinksAPI(t *testing.T) {
	e := setup(t)
	a := e.createProject(t, t.TempDir())
	code, raw := e.do(t, "POST", "/api/projects", map[string]any{"name": "Backend", "goal": "API", "workdir": t.TempDir()})
	if code != http.StatusCreated {
		t.Fatalf("%d %s", code, raw)
	}
	var b struct{ ID int64 }
	json.Unmarshal(raw, &b)

	if code, _ := e.do(t, "POST", "/api/projects/"+itoa(a.ID)+"/links", map[string]any{"projectId": a.ID}); code != http.StatusBadRequest {
		t.Fatalf("acelasi proiect: %d", code)
	}
	if code, _ := e.do(t, "POST", "/api/projects/"+itoa(a.ID)+"/links", map[string]any{"projectId": 9999}); code != http.StatusNotFound {
		t.Fatalf("inexistent: %d", code)
	}
	code, raw = e.do(t, "POST", "/api/projects/"+itoa(a.ID)+"/links", map[string]any{"projectId": b.ID})
	if code != http.StatusCreated || !strings.Contains(string(raw), `"name":"Backend"`) {
		t.Fatalf("%d %s", code, raw)
	}
	if code, _ := e.do(t, "POST", "/api/projects/"+itoa(b.ID)+"/links", map[string]any{"projectId": a.ID}); code != http.StatusConflict {
		t.Fatalf("dublura: %d", code)
	}
	_, raw = e.do(t, "GET", "/api/projects/"+itoa(b.ID)+"/links", nil)
	if !strings.Contains(string(raw), `"projectId":`+itoa(a.ID)) {
		t.Fatalf("din celalalt capat: %s", raw)
	}
	_, raw = e.do(t, "GET", "/api/projects/"+itoa(a.ID), nil)
	if !strings.Contains(string(raw), `"links":[{"projectId":`+itoa(b.ID)) {
		t.Fatalf("detaliu: %s", raw)
	}
	if code, _ := e.do(t, "DELETE", "/api/projects/"+itoa(a.ID)+"/links/"+itoa(b.ID), nil); code != http.StatusNoContent {
		t.Fatalf("stergere: %d", code)
	}
	if code, _ := e.do(t, "DELETE", "/api/projects/"+itoa(a.ID)+"/links/"+itoa(b.ID), nil); code != http.StatusNotFound {
		t.Fatalf("a doua stergere: %d", code)
	}
}

func TestWorkRequestsAPI(t *testing.T) {
	e := setup(t)
	a := e.createProject(t, t.TempDir())
	_, raw := e.do(t, "POST", "/api/projects", map[string]any{"name": "Backend", "goal": "API", "workdir": t.TempDir()})
	var b struct{ ID int64 }
	json.Unmarshal(raw, &b)
	e.do(t, "POST", "/api/projects/"+itoa(a.ID)+"/links", map[string]any{"projectId": b.ID})
	lead := e.root(t, a.ID)
	c := mcp.Caller{ProjectID: a.ID, AgentID: lead.ID, IsRoot: true, CanAddAgents: true, HasLinks: true}
	if _, err := e.tm.RequestProjectWork(c, "Backend", "Add /export", "CSV"); err != nil {
		t.Fatal(err)
	}
	var d struct {
		WorkRequests []struct {
			ID          int64  `json:"id"`
			Ref         string `json:"ref"`
			FromProject string `json:"fromProject"`
			FromAgent   string `json:"fromAgent"`
			Title       string `json:"title"`
		} `json:"workRequests"`
	}
	_, raw = e.do(t, "GET", "/api/projects/"+itoa(b.ID), nil)
	json.Unmarshal(raw, &d)
	if len(d.WorkRequests) != 1 || d.WorkRequests[0].FromProject != "ApiUnit" || d.WorkRequests[0].FromAgent != lead.Name ||
		d.WorkRequests[0].Title != "Add /export" || !strings.HasPrefix(d.WorkRequests[0].Ref, "W-") {
		t.Fatalf("%s", raw)
	}
	_, raw = e.do(t, "GET", "/api/projects", nil)
	if !strings.Contains(string(raw), `"workRequests":1`) {
		t.Fatalf("cardul: %s", raw)
	}
	id := itoa(d.WorkRequests[0].ID)
	if code, raw := e.do(t, "POST", "/api/project-requests/"+id+"/approve", nil); code != http.StatusNoContent {
		t.Fatalf("%d %s", code, raw)
	}
	if code, _ := e.do(t, "POST", "/api/project-requests/"+id+"/approve", nil); code != http.StatusConflict {
		t.Fatalf("a doua: %d", code)
	}
	if code, _ := e.do(t, "POST", "/api/project-requests/9999/dismiss", nil); code != http.StatusNotFound {
		t.Fatalf("inexistenta: %d", code)
	}
	_, raw = e.do(t, "GET", "/api/projects/"+itoa(b.ID), nil)
	if !strings.Contains(string(raw), `"fromProject":"ApiUnit"`) || !strings.Contains(string(raw), `"requestId":`) {
		t.Fatalf("task-ul cu fromProject: %s", raw)
	}
}

func TestApproveAfterUnlinkAPI(t *testing.T) {
	e := setup(t)
	a := e.createProject(t, t.TempDir())
	_, raw := e.do(t, "POST", "/api/projects", map[string]any{"name": "Backend", "goal": "API", "workdir": t.TempDir()})
	var b struct{ ID int64 }
	json.Unmarshal(raw, &b)
	e.do(t, "POST", "/api/projects/"+itoa(a.ID)+"/links", map[string]any{"projectId": b.ID})
	lead := e.root(t, a.ID)
	e.tm.RequestProjectWork(mcp.Caller{ProjectID: a.ID, AgentID: lead.ID, IsRoot: true, CanAddAgents: true, HasLinks: true}, "Backend", "x", "")
	reqs, _ := e.st.ListPendingWork(b.ID)
	e.st.UnlinkProjects(a.ID, b.ID)
	code, raw := e.do(t, "POST", "/api/project-requests/"+itoa(reqs[0].ID)+"/approve", nil)
	if code != http.StatusConflict || !strings.Contains(string(raw), "no longer linked") {
		t.Fatalf("%d %s", code, raw)
	}
}
```

Numele proiectului creat de `e.createProject` e "ApiUnit" (vezi helper-ul). Verifica forma raspunsului `POST /api/projects` (campul `id`).

- [ ] **Step 2:** `go test ./internal/web/ -run 'Links|WorkRequests|ApproveAfterUnlink'` -> FAIL.

- [ ] **Step 3: `api_links.go`:**

```go
package web

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"foreman/internal/store"
)

// linkView e un proiect legat, asa cum il vede UI-ul.
type linkView struct {
	ProjectID int64  `json:"projectId"`
	Name      string `json:"name"`
}

// workRequestView e o cerere de lucru care asteapta aprobarea, la proiectul tinta.
type workRequestView struct {
	ID          int64     `json:"id"`
	Ref         string    `json:"ref"`
	FromProject string    `json:"fromProject"`
	FromAgent   string    `json:"fromAgent"`
	Title       string    `json:"title"`
	Text        string    `json:"text"`
	CreatedAt   time.Time `json:"createdAt"`
}

// linkViews intoarce legaturile proiectului (lista e mereu non-nil).
func (s *Server) linkViews(projectID int64) ([]linkView, error) {
	links, err := s.st.LinkedProjects(projectID)
	if err != nil {
		return nil, err
	}
	out := make([]linkView, 0, len(links))
	for _, l := range links {
		out = append(out, linkView{ProjectID: l.ID, Name: l.Name})
	}
	return out, nil
}

// workRequestViews intoarce cererile de lucru in asteptare catre proiect (mereu non-nil).
func (s *Server) workRequestViews(projectID int64) ([]workRequestView, error) {
	reqs, err := s.st.ListPendingWork(projectID)
	if err != nil {
		return nil, err
	}
	out := make([]workRequestView, 0, len(reqs))
	for _, r := range reqs {
		v := workRequestView{ID: r.ID, Ref: r.Ref(), Title: r.Title, Text: r.Text, CreatedAt: r.CreatedAt,
			FromProject: "a deleted project", FromAgent: "a deleted agent"}
		if p, err := s.st.GetProject(r.FromProject); err == nil {
			v.FromProject = p.Name
		}
		if r.FromAgent != nil {
			if a, err := s.st.GetAgent(*r.FromAgent); err == nil {
				v.FromAgent = a.Name
			}
		}
		out = append(out, v)
	}
	return out, nil
}

func (s *Server) listLinks(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		fail(w, err)
		return
	}
	if _, err := s.st.GetProject(id); err != nil {
		fail(w, err)
		return
	}
	views, err := s.linkViews(id)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, views)
}

func (s *Server) createLink(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		fail(w, err)
		return
	}
	var in struct {
		ProjectID int64 `json:"projectId"`
	}
	if err := decode(r, &in); err != nil {
		fail(w, err)
		return
	}
	if in.ProjectID == id {
		writeErr(w, http.StatusBadRequest, store.ErrSameProject.Error())
		return
	}
	if _, err := s.st.GetProject(id); err != nil {
		fail(w, err)
		return
	}
	other, err := s.st.GetProject(in.ProjectID)
	if err != nil {
		fail(w, err)
		return
	}
	if err := s.st.LinkProjects(id, other.ID); errors.Is(err, store.ErrConflict) {
		writeErr(w, http.StatusConflict, "the projects are already linked")
		return
	} else if err != nil {
		fail(w, err)
		return
	}
	s.hub.Publish(id, "project", map[string]int64{"id": id})
	s.hub.Publish(other.ID, "project", map[string]int64{"id": other.ID})
	writeJSON(w, http.StatusCreated, linkView{ProjectID: other.ID, Name: other.Name})
}

func (s *Server) deleteLink(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		fail(w, err)
		return
	}
	other, err := strconv.ParseInt(r.PathValue("other"), 10, 64)
	if err != nil {
		fail(w, badRequest("invalid id"))
		return
	}
	if err := s.team.UnlinkProjects(id, other); err != nil {
		fail(w, err)
		return
	}
	s.hub.Publish(id, "project", map[string]int64{"id": id})
	s.hub.Publish(other, "project", map[string]int64{"id": other})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) approveProjectRequest(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		fail(w, err)
		return
	}
	if err := s.team.ApproveProjectRequest(id); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) dismissProjectRequest(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		fail(w, err)
		return
	}
	if err := s.team.DismissProjectRequest(id); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
```

- [ ] **Step 4: Legaturile.**
  - `server.go`, langa rutele de proiect:
    ```go
    	mux.HandleFunc("GET /api/projects/{id}/links", s.listLinks)
    	mux.HandleFunc("POST /api/projects/{id}/links", s.createLink)
    	mux.HandleFunc("DELETE /api/projects/{id}/links/{other}", s.deleteLink)
    	mux.HandleFunc("POST /api/project-requests/{id}/approve", s.approveProjectRequest)
    	mux.HandleFunc("POST /api/project-requests/{id}/dismiss", s.dismissProjectRequest)
    ```
  - `fail`: `var pre team.ProjectRequestError`; caz nou inainte de `errors.Is(err, store.ErrNotFound)`: `case errors.As(err, &pre): writeErr(w, http.StatusConflict, pre.Error())`; si `case errors.Is(err, store.ErrSameProject): writeErr(w, http.StatusBadRequest, err.Error())`.
  - `api_projects.go`:
    - `projectCard`, dupa `AgentRequests`: `// WorkRequests: cereri de lucru de la proiecte legate, in asteptarea aprobarii` + `WorkRequests int \`json:"workRequests"\``; in `card`: `if c.WorkRequests, err = s.st.CountPendingWork(p.ID); err != nil { return projectCard{}, nil, nil, nil, err }`.
    - `projectDetail`, dupa `AgentRequests`: `Links []linkView \`json:"links"\`` si `WorkRequests []workRequestView \`json:"workRequests"\`` (cu comentarii; campul din detaliu e mai putin adanc decat numarul din card, deci il ascunde in JSON, ca la `agentRequests`). In `getProject` le calculeaza cu `s.linkViews(p.ID)` / `s.workRequestViews(p.ID)` (cu `fail` la eroare) si le pune in literal.
    - `taskView`: `FromProject string \`json:"fromProject"\` // proiectul care a cerut task-ul ("" = nimeni)`. In `getProject`, la construirea `views`: pentru task-urile cu `RequestID`, citeste cererea (`s.st.GetProjectRequest`) si numele proiectului sursa, cu un `map[int64]string` ca cache pe proiect; erorile de citire lasa campul gol.
    - `deleteProject`: inainte de `s.st.DeleteProject(id)`: `if err := s.team.BeforeDeleteProject(id); err != nil { fail(w, err); return }`.

- [ ] **Step 5:** `go test ./internal/web/` -> PASS; `go vet ./...`.
- [ ] **Step 6:** commit: `web: API pentru proiecte legate si cererile lor`

---

### Task 7: UI: "Linked projects", cererile in "Needs you", "From" pe board; verificarea finala

**Files:**
- Create: `internal/web/static/js/project-requests.js`
- Modify: `internal/web/static/js/projects.js` (sectiunea din pagina Project instructions; numarul de pe card)
- Modify: `internal/web/static/js/workspace.js` ("Needs you", eticheta de pe card)
- Modify: `internal/web/static/css/app.css`
- Modify: `CLAUDE.md` (`agentdash/CLAUDE.md`, sectiunea Stare)

**Interfaces:**
- Consumes: Task 6 (JSON si rute).

- [ ] **Step 1: `project-requests.js`** (in stilul lui `agent-requests.js`):

```js
import { api } from "./api.js";
import { el, toast } from "./ui.js";

// workRequestBox: un proiect legat cere munca ("Frontend (Lead) asks for work: Add /export"),
// cu descrierea restransa si Approve / Dismiss. onDone se cheama dupa o actiune reusita;
// la eroare (ex. legatura a fost stearsa) apare un toast si cardul ramane.
export function workRequestBox(r, { onDone }) {
  const buttons = [];
  const act = (label, cls, path) => {
    const b = el("button", { type: "button", class: cls, onclick: async () => {
      for (const x of buttons) x.disabled = true;
      try { await api(path, { method: "POST" }); await onDone?.(); }
      catch (err) { toast(err.message); for (const x of buttons) x.disabled = false; }
    } }, label);
    buttons.push(b);
    return b;
  };
  return el("div", { class: "question work-request" },
    el("div", { class: "question-head" },
      el("strong", {}, `${r.fromProject} (${r.fromAgent}) asks for work: ${r.title}`),
      ` · ${r.ref}`),
    r.text ? el("details", { class: "agent-request-more" },
      el("summary", {}, "Show description"),
      el("p", { class: "question-text" }, r.text)) : null,
    el("div", { class: "actions" },
      act("Approve", "btn small primary", `/api/project-requests/${r.id}/approve`),
      act("Dismiss", "btn small", `/api/project-requests/${r.id}/dismiss`)));
}
```

- [ ] **Step 2: `workspace.js`.**
  - Import `workRequestBox` din `./project-requests.js`.
  - Langa `agentReqList`: `const workReqList = el("div", { class: "needs-work-requests" });`, adaugat in `needsBox` dupa `agentReqList`; langa `agentReqKey`: `let workReqKey = null; // id-urile cererilor de lucru desenate ultima data`.
  - In `renderNeeds`: `const workReqs = state.project.workRequests || [];`, adaugat la `total`, si la final:
    ```js
        const wKey = workReqs.map((r) => r.id).join(",");
        if (wKey !== workReqKey) {
          workReqKey = wKey;
          workReqList.replaceChildren(...workReqs.map((r) => workRequestBox(r, { onDone: reload })));
        }
    ```
  - In `taskCard`, langa span-ul `task-recurring` (`t.recurringId ? el("span", { class: "task-recurring", ...`): `t.fromProject ? el("span", { class: "task-from", title: \`Requested by ${t.fromProject}\` }, \`From ${t.fromProject}\`) : null`.
- [ ] **Step 3: `projects.js`.**
  - Cardul de proiect (`const asks = ...`): adauga `+ (p.workRequests || 0)` si actualizeaza comentariul.
  - In `projectFormView`, doar pentru `id` (proiect salvat), o sectiune noua dupa sectiunea "Folders": `id && linksSection(id, p.links || [])`. Functia (in acelasi fisier, sub `projectFormView`):

```js
// linksSection: proiectele legate (agentii lor isi pot pune intrebari si cere munca).
// Legaturile se salveaza imediat, nu odata cu formularul.
function linksSection(projectId, initial) {
  let links = initial;
  const list = el("div", { class: "link-rows" });
  const select = el("select", {});
  const addBtn = el("button", { type: "button", class: "btn small", onclick: add }, "Link");

  async function refresh() {
    let all = [];
    try { all = await api("/api/projects"); } catch (err) { toast(err.message); }
    const linked = new Set(links.map((l) => l.projectId));
    const free = all.filter((p) => p.id !== projectId && !linked.has(p.id));
    select.replaceChildren(el("option", { value: "" }, free.length ? "Link a project..." : "No other projects"),
      ...free.map((p) => el("option", { value: p.id }, p.name)));
    select.disabled = addBtn.disabled = !free.length;
    list.replaceChildren(...(links.length ? links.map(row) : [el("p", { class: "hint" }, "No linked projects.")]));
  }

  function row(l) {
    return el("div", { class: "link-row" },
      el("a", { href: `#/p/${l.projectId}` }, l.name),
      el("button", { type: "button", class: "btn small danger", onclick: () => remove(l) }, "Remove"));
  }

  async function add() {
    if (!select.value) return;
    try {
      const l = await api(`/api/projects/${projectId}/links`, { method: "POST", body: { projectId: Number(select.value) } });
      links = [...links, l];
      await refresh();
    } catch (err) { toast(err.message); }
  }

  async function remove(l) {
    if (!confirm(`Unlink ${l.name}? Open questions and unapproved requests between the two projects are closed.`)) return;
    try {
      await api(`/api/projects/${projectId}/links/${l.projectId}`, { method: "DELETE" });
      links = links.filter((x) => x.projectId !== l.projectId);
      await refresh();
    } catch (err) { toast(err.message); }
  }

  refresh();
  return el("section", { class: "section" },
    el("h2", {}, "Linked projects"),
    el("p", { class: "hint" }, "Agents of linked projects can ask each other questions; leads can request work, which you approve in Needs you."),
    list,
    el("div", { class: "row" }, select, addBtn));
}
```

  Verifica importurile din `projects.js` (`api`, `el`, `toast`).
- [ ] **Step 4: `app.css`** (la final, comentariu `/* proiecte legate */`): `.needs-work-requests` in lista cu `display: contents` (linia cu `.needs-questions, .needs-stuck, .needs-access, .needs-agent-requests`); `.task-from { color: var(--muted); font-size: 12px; margin-right: 6px; }`; `.link-rows { display: grid; gap: 6px; margin-bottom: 10px; }`; `.link-row { display: flex; align-items: center; justify-content: space-between; gap: 12px; border: 1px solid var(--line); border-radius: 9px; padding: 8px 12px; background: var(--card-2); min-width: 0; }`. Foloseste variabilele de culoare existente (verifica numele in `app.css`).
- [ ] **Step 5: Verificare JS:** copiaza `project-requests.js`, `workspace.js`, `projects.js` ca `.mjs` intr-un folder temporar si ruleaza `node --check` pe fiecare.
- [ ] **Step 6: Verificare in browser** (aplicatia reala ruleaza pe 7777 cu `~/.foreman`: nu o opri, nu scrie acolo, nu folosi 7777). Construieste binarul intr-un folder temporar (`go build -o <tmp>/foreman ./cmd/foreman`), copiaza `~/.foreman/foreman.db` intr-un HOME separat (`<tmp>/home/.foreman/`), porneste cu `HOME=<tmp>/home ./foreman start --no-open` pe portul 7790 (vezi flag-ul de port in `cmd/foreman`). Pe copie: leaga doua proiecte din UI, insereaza cu `sqlite3` o cerere `pending` (`kind='work'`) catre unul si un task cu `request_id`. Screenshot-uri cu Chrome headless prin CDP (`--headless=new --remote-debugging-port=9333 --user-data-dir=<nou>`, script Node 22 cu WebSocket global; `--screenshot` cu `--virtual-time-budget` se blocheaza din cauza SSE), la 1440x900 si 1000x700: sectiunea "Linked projects", cardul din "Needs you" (si "Show description" deschis), eticheta "From ..." pe board. Apasa Approve prin CDP si verifica ca apare task-ul pe board-ul proiectului tinta si cardul dispare. Opreste serverul de test si Chrome dupa PID.
- [ ] **Step 7: `CLAUDE.md`**, sectiunea Stare, dupa paragraful "liderul adauga agenti":

```
Gata (proiecte legate, spec in docs/superpowers/specs/2026-10-06-foreman-project-links-design.md): utilizatorul leaga
proiecte (fara sens); agentii pun intrebari radacinii proiectului legat (ask_project / reply_to_project, max 3 deschise,
anti-bucla) si liderii cer munca (request_project_work: task pentru radacina tinta, aprobat in "Needs you" la tinta,
direct doar la cererea utilizatorului din chat); raportul se intoarce la Done (afterDone).
```

- [ ] **Step 8: Verificarea completa:** `go vet ./...`, `go build -race -o <tmp> ./cmd/foreman`, `go test ./...`, cele doua cross-compile cu `-o <tmp>`; `git status` arata doar `foreman.prev` netrackuit.
- [ ] **Step 9:** commit: `ui: proiecte legate, cererile de lucru in Needs you, "From" pe board`
