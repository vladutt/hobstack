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
	CreatedBy   *int64        `json:"createdBy"` // agentul care a creat-o la cererea utilizatorului; nil = utilizatorul
}

// runAtLayout are lungime fixa: next_run_at se compara ca text in DueRecurring.
const runAtLayout = "2006-01-02T15:04:05.000000000Z07:00"

func runAt(t time.Time) string { return t.UTC().Format(runAtLayout) }

const recurringCols = `id, project_id, title, description, assignee_id, kind, every_hours, at, weekdays,
	auto_accept, enabled, next_run_at, last_run_at, last_task_id, last_note, created_at, created_by`

func scanRecurring(sc scanner) (RecurringTask, error) {
	var r RecurringTask
	var next, created string
	var last sql.NullString
	err := sc.Scan(&r.ID, &r.ProjectID, &r.Title, &r.Description, &r.AssigneeID,
		&r.Schedule.Kind, &r.Schedule.EveryHours, &r.Schedule.At, &r.Schedule.Weekdays,
		&r.AutoAccept, &r.Enabled, &next, &last, &r.LastTaskID, &r.LastNote, &created, &r.CreatedBy)
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
		auto_accept, enabled, next_run_at, created_at, created_by) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.ProjectID, r.Title, r.Description, nullPtr(r.AssigneeID), r.Schedule.Kind, r.Schedule.EveryHours, r.Schedule.At,
		r.Schedule.Weekdays, r.AutoAccept, r.Enabled, runAt(r.NextRunAt), ts, nullPtr(r.CreatedBy))
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

// RecurringCounts: rularile unei sarcini recurente, pe stari (open = inca nu e Done, fara arhivate).
type RecurringCounts struct {
	Total    int `json:"total"`
	Done     int `json:"done"`
	Open     int `json:"open"`
	Archived int `json:"archived"`
}

func (s *Store) RecurringCounts(id int64) (RecurringCounts, error) {
	var c RecurringCounts
	err := s.db.QueryRow(`SELECT COUNT(*), COALESCE(SUM(status = 'done'), 0),
		COALESCE(SUM(status NOT IN ('done', 'archived')), 0), COALESCE(SUM(status = 'archived'), 0)
		FROM tasks WHERE recurring_id = ?`, id).Scan(&c.Total, &c.Done, &c.Open, &c.Archived)
	return c, err
}

// RecurringRun: un task creat de sarcina, cu numarul fisierelor lui si ale sub-task-urilor (cu versiuni).
type RecurringRun struct {
	Task
	Files int `json:"files"`
}

// RecurringHistory: rularile sarcinii (si cele arhivate), cea mai noua prima. before > 0 intoarce
// doar rularile mai vechi decat el (paginare stabila cand apar rulari noi); bool = mai sunt.
func (s *Store) RecurringHistory(id, before int64, limit int) ([]RecurringRun, bool, error) {
	tasks, err := s.recurringTasks(id, before, limit+1)
	if err != nil {
		return nil, false, err
	}
	more := len(tasks) > limit
	if more {
		tasks = tasks[:limit]
	}
	out := make([]RecurringRun, 0, len(tasks))
	for _, t := range tasks {
		n, err := s.countRunFiles(t.ID)
		if err != nil {
			return nil, false, err
		}
		out = append(out, RecurringRun{Task: t, Files: n})
	}
	return out, more, nil
}

// recurringTasks citeste tot inainte de alte interogari (rows inchis la iesire).
func (s *Store) recurringTasks(id, before int64, n int) ([]Task, error) {
	rows, err := s.db.Query(`SELECT `+taskCols+` FROM tasks WHERE recurring_id = ? AND (? = 0 OR id < ?)
		ORDER BY id DESC LIMIT ?`, id, before, before, n)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Task
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// runTreeStep: pasul recursiv peste sub-task-uri. Sub-task-urile arhivate se sar, mai putin cele
// arhivate odata cu rularea (acelasi archived_root): o rulare stearsa isi pastreaza fisierele in istoric.
const runTreeStep = `UNION ALL SELECT t.id, sub.aroot FROM tasks t JOIN sub ON t.parent_task_id = sub.id
	WHERE t.status != 'archived' OR (sub.aroot IS NOT NULL AND t.archived_root = sub.aroot)`

// RecurringFiles: fisierele rularilor (si ale celor arhivate) si ale sub-task-urilor lor, cel mai nou primul.
func (s *Store) RecurringFiles(id int64) ([]TaskFile, error) {
	return s.queryTaskFiles(`WITH RECURSIVE sub(id, aroot) AS (
			SELECT id, archived_root FROM tasks WHERE recurring_id = ? `+runTreeStep+`)
		SELECT `+taskFileCols+` FROM task_files WHERE task_id IN (SELECT id FROM sub) ORDER BY id DESC`, id)
}

// countRunFiles: randurile de fisiere ale unei rulari si ale sub-task-urilor ei (cu versiuni).
func (s *Store) countRunFiles(taskID int64) (int, error) {
	var n int
	err := s.db.QueryRow(`WITH RECURSIVE sub(id, aroot) AS (
			SELECT id, archived_root FROM tasks WHERE id = ? `+runTreeStep+`)
		SELECT COUNT(*) FROM task_files WHERE task_id IN (SELECT id FROM sub)`, taskID).Scan(&n)
	return n, err
}

// RecurringRefs: id -> "T-N" pentru rularile sarcinii si sub-task-urile lor (aceeasi multime ca RecurringFiles).
func (s *Store) RecurringRefs(id int64) (map[int64]string, error) {
	rows, err := s.db.Query(`WITH RECURSIVE sub(id, aroot) AS (
			SELECT id, archived_root FROM tasks WHERE recurring_id = ? `+runTreeStep+`)
		SELECT id, number FROM tasks WHERE id IN (SELECT id FROM sub)`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]string{}
	for rows.Next() {
		var tid int64
		var n int
		if err := rows.Scan(&tid, &n); err != nil {
			return nil, err
		}
		out[tid] = Task{Number: n}.Ref()
	}
	return out, rows.Err()
}
