package store

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Etapele de review ale unui task aflat in Review.
const (
	StageNone     = ""
	StageReviewer = "reviewer"
	StageLead     = "lead"
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

	CreatorID    *int64 `json:"creatorId"`    // liderul care decide; nil la task-uri al caror creator a fost sters
	ParentTaskID *int64 `json:"parentTaskId"` // task-ul din care a fost spart
	ReviewStage  string `json:"reviewStage"`  // "", "reviewer", "lead"
	StageID      *int64 `json:"stageId"`      // etapa curenta (coloana proprie); nil = fara etape sau dupa ultima
	ByUser       bool   `json:"byUser"`       // creat de utilizator din board: la final decide el

	// NudgedAt: cand l-a impins ultima data detectorul de task-uri blocate;
	// nil = neimpins de la ultima schimbare de stare.
	NudgedAt *time.Time `json:"nudgedAt"`
}

// Ref e forma afisata a task-ului: T-3.
func (t Task) Ref() string { return fmt.Sprintf("T-%d", t.Number) }

const taskCols = `id, project_id, number, title, description, assignee_id, status, result, feedback, send_backs, created_at, updated_at, creator_id, parent_task_id, review_stage, stage_id, by_user, nudged_at`

func scanTask(sc scanner) (Task, error) {
	var t Task
	var created, updated string
	var nudged sql.NullString
	err := sc.Scan(&t.ID, &t.ProjectID, &t.Number, &t.Title, &t.Description, &t.AssigneeID,
		&t.Status, &t.Result, &t.Feedback, &t.SendBacks, &created, &updated,
		&t.CreatorID, &t.ParentTaskID, &t.ReviewStage, &t.StageID, &t.ByUser, &nudged)
	if errors.Is(err, sql.ErrNoRows) {
		return Task{}, ErrNotFound
	}
	if err != nil {
		return Task{}, err
	}
	t.CreatedAt, t.UpdatedAt = parseTime(created), parseTime(updated)
	if nudged.Valid {
		at := parseTime(nudged.String)
		t.NudgedAt = &at
	}
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
	t.ReviewStage = StageNone
	res, err := tx.Exec(`INSERT INTO tasks (project_id, number, title, description, assignee_id, status, creator_id, parent_task_id, stage_id, by_user, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, t.ProjectID, t.Number, t.Title, t.Description, t.AssigneeID, t.Status,
		nullPtr(t.CreatorID), nullPtr(t.ParentTaskID), nullPtr(t.StageID), t.ByUser, ts, ts)
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

// SaveTask salveaza starea unui task: status, rezultat, feedback, trimiteri
// inapoi, etapa de review, asignatul si etapa de lucru (coloana). Orice
// schimbare de stare inseamna activitate, deci curata si nudged_at.
func (s *Store) SaveTask(t *Task) error {
	ts := now()
	err := mustAffect(s.db.Exec(`UPDATE tasks SET status = ?, result = ?, feedback = ?, send_backs = ?, review_stage = ?,
		assignee_id = ?, stage_id = ?, updated_at = ?, nudged_at = NULL
		WHERE id = ?`, t.Status, t.Result, t.Feedback, t.SendBacks, t.ReviewStage, t.AssigneeID, nullPtr(t.StageID), ts, t.ID))
	if err != nil {
		return err
	}
	t.UpdatedAt = parseTime(ts)
	t.NudgedAt = nil
	return nil
}

// SetTaskNudged noteaza momentul in care detectorul a impins task-ul.
// Nu atinge updated_at: impingerea nu e o schimbare de stare.
func (s *Store) SetTaskNudged(id int64, at time.Time) error {
	return mustAffect(s.db.Exec(`UPDATE tasks SET nudged_at = ? WHERE id = ?`, at.UTC().Format(time.RFC3339Nano), id))
}

// Subtasks intoarce task-urile sparte din parentTaskID, in ordinea numarului.
func (s *Store) Subtasks(parentTaskID int64) ([]Task, error) {
	rows, err := s.db.Query(`SELECT `+taskCols+` FROM tasks WHERE parent_task_id = ? ORDER BY number`, parentTaskID)
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

// notWaitingParent: un task cu sub-task-uri nefinalizate nu are nevoie de un run
// nou; il tine in lucru sub-liderul pana cand toate sub-task-urile sunt Done.
const notWaitingParent = ` AND NOT EXISTS (SELECT 1 FROM tasks sub WHERE sub.parent_task_id = tasks.id AND sub.status != 'done')`

// notWaitingAnswer: un task al carui agent asteapta raspunsul utilizatorului
// ramane In progress; raspunsul il pune inapoi in coada.
const notWaitingAnswer = ` AND id NOT IN (SELECT task_id FROM questions WHERE status = 'open' AND task_id IS NOT NULL)`

// RequeueInProgress muta task-urile In progress ale proiectului inapoi in Queued
// (la Stop team: procesele au fost oprite, munca se reia mai tarziu) si curata
// nudged_at, ca orice schimbare de stare. Cele care
// asteapta sub-task-uri sau un raspuns de la utilizator raman In progress.
func (s *Store) RequeueInProgress(projectID int64) error {
	_, err := s.db.Exec(`UPDATE tasks SET status = 'queued', updated_at = ?, nudged_at = NULL
		WHERE project_id = ? AND status = 'in_progress'`+notWaitingParent+notWaitingAnswer, now(), projectID)
	return err
}

// RequeueAllInProgress face acelasi lucru pentru toate proiectele (la pornirea serverului).
func (s *Store) RequeueAllInProgress() error {
	_, err := s.db.Exec(`UPDATE tasks SET status = 'queued', updated_at = ?, nudged_at = NULL
		WHERE status = 'in_progress'`+notWaitingParent+notWaitingAnswer, now())
	return err
}
