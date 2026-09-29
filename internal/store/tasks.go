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
