package store

import (
	"database/sql"
	"strings"
	"time"
)

type Todo struct {
	TaskID   int64      `json:"taskId"`
	Position int        `json:"position"`
	Text     string     `json:"text"`
	DoneAt   *time.Time `json:"doneAt"`
	DoneBy   string     `json:"doneBy"`
	Kind     string     `json:"kind"` // "value" (de completat) | "step" (de facut)
}

type FillIn struct {
	TaskID  int64  `json:"taskId"`
	Number  int    `json:"number"`
	Title   string `json:"title"`
	AgentID int64  `json:"agentId"`
	Open    int    `json:"open"`
	Total   int    `json:"total"`
	First   string `json:"first"` // primul punct deschis
}

// AddTodos adauga punctele la coada listei task-ului; un text deja prezent (fara diferenta de litere) se sare.
func (s *Store) AddTodos(taskID int64, texts []string, kind string) (int, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	existing := map[string]bool{}
	last := 0
	rows, err := tx.Query(`SELECT position, text FROM task_todos WHERE task_id = ?`, taskID)
	if err != nil {
		return 0, err
	}
	for rows.Next() {
		var pos int
		var text string
		if err := rows.Scan(&pos, &text); err != nil {
			rows.Close()
			return 0, err
		}
		existing[strings.ToLower(text)] = true
		last = max(last, pos)
	}
	rows.Close()
	added := 0
	for _, text := range texts {
		key := strings.ToLower(strings.TrimSpace(text))
		if key == "" || existing[key] {
			continue
		}
		existing[key] = true
		last++
		if _, err := tx.Exec(`INSERT INTO task_todos (task_id, position, text, created_at, kind) VALUES (?, ?, ?, ?, ?)`,
			taskID, last, strings.TrimSpace(text), now(), kind); err != nil {
			return 0, err
		}
		added++
	}
	return added, tx.Commit()
}

func (s *Store) ListTodos(taskID int64) ([]Todo, error) {
	rows, err := s.db.Query(`SELECT task_id, position, text, done_at, done_by, kind FROM task_todos WHERE task_id = ? ORDER BY position`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Todo{}
	for rows.Next() {
		var t Todo
		var done sql.NullString
		if err := rows.Scan(&t.TaskID, &t.Position, &t.Text, &done, &t.DoneBy, &t.Kind); err != nil {
			return nil, err
		}
		if done.Valid {
			at := parseTime(done.String)
			t.DoneAt = &at
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *Store) SetTodo(taskID int64, position int, done bool, by string) error {
	if done {
		return mustAffect(s.db.Exec(`UPDATE task_todos SET done_at = ?, done_by = ? WHERE task_id = ? AND position = ?`, now(), by, taskID, position))
	}
	return mustAffect(s.db.Exec(`UPDATE task_todos SET done_at = NULL, done_by = '' WHERE task_id = ? AND position = ?`, taskID, position))
}

// withSteps=false (Full autonomy): doar punctele de completat ("value"), fara pasii "step"
func (s *Store) ListFillIns(projectID int64, withSteps bool) ([]FillIn, error) {
	rows, err := s.db.Query(`SELECT t.id, t.number, t.title, t.assignee_id,
		SUM(CASE WHEN d.done_at IS NULL THEN 1 ELSE 0 END), COUNT(*),
		(SELECT text FROM task_todos x WHERE x.task_id = t.id AND x.done_at IS NULL AND (?1 OR x.kind != 'step') ORDER BY position LIMIT 1)
		FROM tasks t JOIN task_todos d ON d.task_id = t.id AND (?1 OR d.kind != 'step')
		WHERE t.project_id = ?2 AND t.status != 'archived' GROUP BY t.id HAVING SUM(CASE WHEN d.done_at IS NULL THEN 1 ELSE 0 END) > 0
		ORDER BY t.number`, withSteps, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []FillIn{}
	for rows.Next() {
		var f FillIn
		var agent sql.NullInt64
		if err := rows.Scan(&f.TaskID, &f.Number, &f.Title, &agent, &f.Open, &f.Total, &f.First); err != nil {
			return nil, err
		}
		f.AgentID = agent.Int64
		out = append(out, f)
	}
	return out, rows.Err()
}
