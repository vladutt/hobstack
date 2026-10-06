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
