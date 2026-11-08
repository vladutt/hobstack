package store

import (
	"database/sql"
	"strings"
	"time"
)

// TaskFile e un fisier predat de un agent pe un task (o copie facuta la report_done).
type TaskFile struct {
	ID        int64     `json:"id"`
	TaskID    int64     `json:"taskId"`
	ProjectID int64     `json:"projectId"`
	AgentID   *int64    `json:"agentId"`
	AgentName string    `json:"agentName"` // numele la momentul predarii
	Path      string    `json:"path"`      // relativa la folderul agentului cand e in el, altfel absoluta
	Name      string    `json:"name"`
	Stored    string    `json:"-"` // numele de pe disc, in task-files/<task>/
	Size      int64     `json:"size"`
	Mime      string    `json:"mime"`
	CreatedAt time.Time `json:"createdAt"`
	Source    string    `json:"-"` // calea originala, absoluta (doar fisierele agentilor); "" = doar copia
}

// UserFileAuthor: agent_name pentru fisierele puse de utilizator pe task (agent_id ramane NULL).
const UserFileAuthor = "You"

// ReservedAgentName: "You" (oricum scris) e utilizatorul: in istoric, conversatii si fisiere.
func ReservedAgentName(name string) bool {
	return strings.EqualFold(strings.TrimSpace(name), UserFileAuthor)
}

// ErrReservedName: textul refuzului pentru un agent numit "You".
const ErrReservedName = `"You" is reserved for the user; choose another name`

// FromUser: fisierul l-a pus utilizatorul (nu e livrabilul unui agent).
// Un agent sters are tot agent_id NULL, dar numele lui ramane in agent_name.
func (f TaskFile) FromUser() bool { return f.AgentID == nil && f.AgentName == UserFileAuthor }

const taskFileCols = `id, task_id, project_id, agent_id, agent_name, path, name, stored, size, mime, created_at, source`

func scanTaskFile(row interface{ Scan(...any) error }) (TaskFile, error) {
	var f TaskFile
	var agent sql.NullInt64
	var at string
	err := row.Scan(&f.ID, &f.TaskID, &f.ProjectID, &agent, &f.AgentName, &f.Path, &f.Name, &f.Stored, &f.Size, &f.Mime, &at, &f.Source)
	if agent.Valid {
		id := agent.Int64
		f.AgentID = &id
	}
	f.CreatedAt = parseTime(at)
	return f, err
}

func (s *Store) AddTaskFile(f *TaskFile) error {
	ts := now()
	var agent sql.NullInt64
	if f.AgentID != nil {
		agent = sql.NullInt64{Int64: *f.AgentID, Valid: true}
	}
	res, err := s.db.Exec(`INSERT INTO task_files (task_id, project_id, agent_id, agent_name, path, name, stored, size, mime, created_at, source)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		f.TaskID, f.ProjectID, agent, f.AgentName, f.Path, f.Name, f.Stored, f.Size, f.Mime, ts, f.Source)
	if err != nil {
		return err
	}
	f.ID, err = res.LastInsertId()
	f.CreatedAt = parseTime(ts)
	return err
}

// AddTaskFiles adauga mai multe fisiere intr-o tranzactie: ori toate, ori niciunul.
func (s *Store) AddTaskFiles(files []*TaskFile) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	ts := now()
	for _, f := range files {
		var agent sql.NullInt64
		if f.AgentID != nil {
			agent = sql.NullInt64{Int64: *f.AgentID, Valid: true}
		}
		res, err := tx.Exec(`INSERT INTO task_files (task_id, project_id, agent_id, agent_name, path, name, stored, size, mime, created_at, source)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			f.TaskID, f.ProjectID, agent, f.AgentName, f.Path, f.Name, f.Stored, f.Size, f.Mime, ts, f.Source)
		if err != nil {
			return err
		}
		if f.ID, err = res.LastInsertId(); err != nil {
			return err
		}
		f.CreatedAt = parseTime(ts)
	}
	return tx.Commit()
}

func (s *Store) GetTaskFile(id int64) (TaskFile, error) {
	f, err := scanTaskFile(s.db.QueryRow(`SELECT `+taskFileCols+` FROM task_files WHERE id = ?`, id))
	if err == sql.ErrNoRows {
		return TaskFile{}, ErrNotFound
	}
	return f, err
}

// ListTaskFiles: fisierele task-ului, cel mai nou primul; lista e mereu non-nil.
func (s *Store) ListTaskFiles(taskID int64) ([]TaskFile, error) {
	return s.queryTaskFiles(`SELECT `+taskFileCols+` FROM task_files WHERE task_id = ? ORDER BY id DESC`, taskID)
}

// ListUserTaskFiles: fisierele puse de utilizator pe task, cel mai nou primul.
func (s *Store) ListUserTaskFiles(taskID int64) ([]TaskFile, error) {
	return s.queryTaskFiles(`SELECT `+taskFileCols+` FROM task_files WHERE task_id = ? AND agent_id IS NULL AND agent_name = ? ORDER BY id DESC`,
		taskID, UserFileAuthor)
}

// ListDescendantFiles: fisierele sub-task-urilor (recursiv), in ordinea numerelor de task.
func (s *Store) ListDescendantFiles(taskID int64) ([]TaskFile, error) {
	return s.queryTaskFiles(`WITH RECURSIVE sub(id) AS (
			SELECT id FROM tasks WHERE parent_task_id = ? AND status != 'archived'
			UNION ALL SELECT t.id FROM tasks t JOIN sub ON t.parent_task_id = sub.id WHERE t.status != 'archived')
		SELECT `+prefixCols("f.", taskFileCols)+` FROM task_files f JOIN tasks t ON t.id = f.task_id
		WHERE f.task_id IN (SELECT id FROM sub) ORDER BY t.number, f.id DESC`, taskID)
}

func (s *Store) queryTaskFiles(q string, args ...any) ([]TaskFile, error) {
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TaskFile{}
	for rows.Next() {
		f, err := scanTaskFile(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// CountTaskFiles: per task al proiectului, cate cai distincte au fost predate (fara versiuni).
func (s *Store) CountTaskFiles(projectID int64) (map[int64]int, error) {
	rows, err := s.db.Query(`SELECT task_id, COUNT(DISTINCT path) FROM task_files WHERE project_id = ? GROUP BY task_id`, projectID)
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

// CountTaskFilesTree: randurile task-ului si ale descendentilor (amprenta ieftina pentru tab-ul Files).
func (s *Store) CountTaskFilesTree(taskID int64) (int, error) {
	var n int
	err := s.db.QueryRow(`WITH RECURSIVE sub(id) AS (
			SELECT ? UNION ALL SELECT t.id FROM tasks t JOIN sub ON t.parent_task_id = sub.id WHERE t.status != 'archived')
		SELECT COUNT(*) FROM task_files WHERE task_id IN (SELECT id FROM sub)`, taskID).Scan(&n)
	return n, err
}

// prefixCols: "a, b" -> "f.a, f.b" (pentru JOIN-uri)
func prefixCols(prefix, cols string) string {
	parts := strings.Split(cols, ",")
	for i, c := range parts {
		parts[i] = prefix + strings.TrimSpace(c)
	}
	return strings.Join(parts, ", ")
}
