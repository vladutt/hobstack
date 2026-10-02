package store

import (
	"database/sql"
	"encoding/json"
	"time"
)

// Hunk e un bloc de diff (aceleasi campuri ca runner.Hunk).
type Hunk struct {
	OldStart int      `json:"oldStart"`
	OldLines int      `json:"oldLines"`
	NewStart int      `json:"newStart"`
	NewLines int      `json:"newLines"`
	Lines    []string `json:"lines"`
}

// TaskChange e o modificare de fisier sau o comanda a unui agent, pe un task.
type TaskChange struct {
	ID        int64
	TaskID    int64
	RunID     string
	AgentID   *int64
	AgentName string
	At        time.Time
	Kind      string // create, update, edit, command
	Tool      string
	Path      string
	Patch     []Hunk
	Command   string
	Truncated bool
}

// AddTaskChanges salveaza toate modificarile intr-o singura tranzactie.
func (s *Store) AddTaskChanges(changes []TaskChange) error {
	if len(changes) == 0 {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, c := range changes {
		patch := ""
		if len(c.Patch) > 0 {
			b, err := json.Marshal(c.Patch)
			if err != nil {
				return err
			}
			patch = string(b)
		}
		at := c.At
		if at.IsZero() {
			at = time.Now()
		}
		var agent sql.NullInt64
		if c.AgentID != nil {
			agent = sql.NullInt64{Int64: *c.AgentID, Valid: true}
		}
		if _, err := tx.Exec(`INSERT INTO task_changes (task_id, run_id, agent_id, agent_name, created_at, kind, tool, path, patch, command, truncated)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			c.TaskID, c.RunID, agent, c.AgentName, at.UTC().Format(time.RFC3339Nano), c.Kind, c.Tool, c.Path, patch, c.Command, c.Truncated); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ListTaskChanges intoarce modificarile task-ului in ordinea inregistrarii; mereu non-nil.
func (s *Store) ListTaskChanges(taskID int64) ([]TaskChange, error) {
	rows, err := s.db.Query(`SELECT id, task_id, run_id, agent_id, agent_name, created_at, kind, tool, path, patch, command, truncated
		FROM task_changes WHERE task_id = ? ORDER BY id`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TaskChange{}
	for rows.Next() {
		var c TaskChange
		var agent sql.NullInt64
		var at, patch string
		if err := rows.Scan(&c.ID, &c.TaskID, &c.RunID, &agent, &c.AgentName, &at, &c.Kind, &c.Tool, &c.Path, &patch, &c.Command, &c.Truncated); err != nil {
			return nil, err
		}
		if agent.Valid {
			id := agent.Int64
			c.AgentID = &id
		}
		c.At = parseTime(at)
		if patch != "" {
			if err := json.Unmarshal([]byte(patch), &c.Patch); err != nil {
				return nil, err
			}
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// CountChangedFiles intoarce, per task al proiectului, numarul de cai distincte
// modificate (comenzile nu conteaza). Task-urile fara modificari lipsesc.
func (s *Store) CountChangedFiles(projectID int64) (map[int64]int, error) {
	rows, err := s.db.Query(`SELECT c.task_id, COUNT(DISTINCT c.path) FROM task_changes c
		JOIN tasks t ON t.id = c.task_id
		WHERE t.project_id = ? AND c.kind != 'command' GROUP BY c.task_id`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]int{}
	for rows.Next() {
		var id int64
		var n int
		if err := rows.Scan(&id, &n); err != nil {
			return nil, err
		}
		out[id] = n
	}
	return out, rows.Err()
}
