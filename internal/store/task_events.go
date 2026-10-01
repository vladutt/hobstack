package store

import "time"

// TaskEvent e un pas din istoricul unui task.
type TaskEvent struct {
	ID     int64     `json:"id"`
	TaskID int64     `json:"taskId"`
	At     time.Time `json:"at"`
	Kind   string    `json:"kind"`
	Actor  string    `json:"actor"`
	Text   string    `json:"text"`
}

// AddTaskEvent salveaza un eveniment; At zero = acum.
func (s *Store) AddTaskEvent(e *TaskEvent) error {
	if e.At.IsZero() {
		e.At = time.Now().UTC()
	}
	res, err := s.db.Exec(`INSERT INTO task_events (task_id, created_at, kind, actor, text) VALUES (?, ?, ?, ?, ?)`,
		e.TaskID, e.At.UTC().Format(time.RFC3339Nano), e.Kind, e.Actor, e.Text)
	if err != nil {
		return err
	}
	e.ID, err = res.LastInsertId()
	return err
}

// ListTaskEvents intoarce istoricul task-ului in ordinea inregistrarii; mereu non-nil.
func (s *Store) ListTaskEvents(taskID int64) ([]TaskEvent, error) {
	rows, err := s.db.Query(`SELECT id, task_id, created_at, kind, actor, text FROM task_events WHERE task_id = ? ORDER BY id`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TaskEvent{}
	for rows.Next() {
		var e TaskEvent
		var at string
		if err := rows.Scan(&e.ID, &e.TaskID, &at, &e.Kind, &e.Actor, &e.Text); err != nil {
			return nil, err
		}
		e.At = parseTime(at)
		out = append(out, e)
	}
	return out, rows.Err()
}
