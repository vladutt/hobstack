package store

import (
	"database/sql"
	"errors"
	"time"

	"hobstack/internal/schedule"
)

// RecurringTask e o sarcina care creeaza singura task-uri pe un program.
type RecurringTask struct {
	ID          int64         `json:"id"`
	ProjectID   int64         `json:"projectId"`
	Title       string        `json:"title"`
	Description string        `json:"description"`
	AssigneeID  *int64        `json:"assigneeId"` // nil = prima etapa (sau agentul a fost sters)
	Schedule    schedule.Spec `json:"schedule"`
	AutoAccept  bool          `json:"autoAccept"`
	Enabled     bool          `json:"enabled"`
	NextRunAt   time.Time     `json:"nextRunAt"`
	LastRunAt   *time.Time    `json:"lastRunAt"`
	LastTaskID  *int64        `json:"lastTaskId"`
	LastNote    string        `json:"lastNote"`
	CreatedAt   time.Time     `json:"createdAt"`
}

// runAtLayout are lungime fixa: next_run_at se compara ca text in DueRecurring.
const runAtLayout = "2006-01-02T15:04:05.000000000Z07:00"

func runAt(t time.Time) string { return t.UTC().Format(runAtLayout) }

const recurringCols = `id, project_id, title, description, assignee_id, kind, every_hours, at, weekdays,
	auto_accept, enabled, next_run_at, last_run_at, last_task_id, last_note, created_at`

func scanRecurring(sc scanner) (RecurringTask, error) {
	var r RecurringTask
	var next, created string
	var last sql.NullString
	err := sc.Scan(&r.ID, &r.ProjectID, &r.Title, &r.Description, &r.AssigneeID,
		&r.Schedule.Kind, &r.Schedule.EveryHours, &r.Schedule.At, &r.Schedule.Weekdays,
		&r.AutoAccept, &r.Enabled, &next, &last, &r.LastTaskID, &r.LastNote, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return RecurringTask{}, ErrNotFound
	}
	if err != nil {
		return RecurringTask{}, err
	}
	r.NextRunAt, r.CreatedAt = parseTime(next), parseTime(created)
	if last.Valid {
		at := parseTime(last.String)
		r.LastRunAt = &at
	}
	return r, nil
}

func (s *Store) CreateRecurring(r *RecurringTask) error {
	ts := now()
	res, err := s.db.Exec(`INSERT INTO recurring_tasks (project_id, title, description, assignee_id, kind, every_hours, at, weekdays,
		auto_accept, enabled, next_run_at, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.ProjectID, r.Title, r.Description, nullPtr(r.AssigneeID), r.Schedule.Kind, r.Schedule.EveryHours, r.Schedule.At,
		r.Schedule.Weekdays, r.AutoAccept, r.Enabled, runAt(r.NextRunAt), ts)
	if err != nil {
		return err
	}
	r.CreatedAt = parseTime(ts)
	r.ID, err = res.LastInsertId()
	return err
}

func (s *Store) GetRecurring(id int64) (RecurringTask, error) {
	return scanRecurring(s.db.QueryRow(`SELECT `+recurringCols+` FROM recurring_tasks WHERE id = ?`, id))
}

// SaveRecurring scrie tot ce se poate schimba (editare din UI sau o rulare).
func (s *Store) SaveRecurring(r *RecurringTask) error {
	var last any
	if r.LastRunAt != nil {
		last = runAt(*r.LastRunAt)
	}
	return mustAffect(s.db.Exec(`UPDATE recurring_tasks SET title = ?, description = ?, assignee_id = ?, kind = ?, every_hours = ?,
		at = ?, weekdays = ?, auto_accept = ?, enabled = ?, next_run_at = ?, last_run_at = ?, last_task_id = ?, last_note = ?
		WHERE id = ?`, r.Title, r.Description, nullPtr(r.AssigneeID), r.Schedule.Kind, r.Schedule.EveryHours, r.Schedule.At,
		r.Schedule.Weekdays, r.AutoAccept, r.Enabled, runAt(r.NextRunAt), last, nullPtr(r.LastTaskID), r.LastNote, r.ID))
}

func (s *Store) DeleteRecurring(id int64) error {
	return mustAffect(s.db.Exec(`DELETE FROM recurring_tasks WHERE id = ?`, id))
}

func (s *Store) ListRecurring(projectID int64) ([]RecurringTask, error) {
	return s.listRecurring(`SELECT `+recurringCols+` FROM recurring_tasks WHERE project_id = ? ORDER BY id`, projectID)
}

// DueRecurring: sarcinile pornite al caror moment a venit, din toate proiectele.
func (s *Store) DueRecurring(now time.Time) ([]RecurringTask, error) {
	return s.listRecurring(`SELECT `+recurringCols+` FROM recurring_tasks WHERE enabled = 1 AND next_run_at <= ? ORDER BY next_run_at, id`, runAt(now))
}

func (s *Store) listRecurring(q string, args ...any) ([]RecurringTask, error) {
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []RecurringTask{}
	for rows.Next() {
		r, err := scanRecurring(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
