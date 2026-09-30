package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

// Starile unei intrebari puse de un agent utilizatorului.
const (
	QuestionOpen      = "open"
	QuestionAnswered  = "answered"
	QuestionDismissed = "dismissed"
)

// Question e o intrebare a unui agent catre utilizator (tool-ul ask_user).
type Question struct {
	ID        int64      `json:"id"`
	ProjectID int64      `json:"projectId"`
	AgentID   int64      `json:"agentId"`
	TaskID    *int64     `json:"taskId"` // task-ul run-ului care a intrebat; nil = fara task
	RunID     string     `json:"runId"`
	Text      string     `json:"text"`
	Options   []string   `json:"options"` // raspunsuri scurte propuse; mereu non-nil
	Answer    string     `json:"answer"`
	Status    string     `json:"status"`
	CreatedAt time.Time  `json:"createdAt"`
	ClosedAt  *time.Time `json:"closedAt"`
}

const questionCols = `id, project_id, agent_id, task_id, run_id, text, options, answer, status, created_at, closed_at`

func scanQuestion(sc scanner) (Question, error) {
	var q Question
	var opts, created string
	var closed sql.NullString
	err := sc.Scan(&q.ID, &q.ProjectID, &q.AgentID, &q.TaskID, &q.RunID, &q.Text, &opts, &q.Answer, &q.Status, &created, &closed)
	if errors.Is(err, sql.ErrNoRows) {
		return Question{}, ErrNotFound
	}
	if err != nil {
		return Question{}, err
	}
	if err := json.Unmarshal([]byte(opts), &q.Options); err != nil || q.Options == nil {
		q.Options = []string{}
	}
	q.CreatedAt = parseTime(created)
	if closed.Valid {
		t := parseTime(closed.String)
		q.ClosedAt = &t
	}
	return q, nil
}

// AddQuestion salveaza o intrebare deschisa.
func (s *Store) AddQuestion(q *Question) error {
	if q.Options == nil {
		q.Options = []string{}
	}
	opts, _ := json.Marshal(q.Options)
	created := now()
	res, err := s.db.Exec(`INSERT INTO questions (project_id, agent_id, task_id, run_id, text, options, status, created_at)
		VALUES (?, ?, ?, ?, ?, ?, 'open', ?)`, q.ProjectID, q.AgentID, nullPtr(q.TaskID), q.RunID, q.Text, string(opts), created)
	if err != nil {
		return err
	}
	if q.ID, err = res.LastInsertId(); err != nil {
		return err
	}
	q.Status, q.CreatedAt = QuestionOpen, parseTime(created)
	return nil
}

func (s *Store) GetQuestion(id int64) (Question, error) {
	return scanQuestion(s.db.QueryRow(`SELECT `+questionCols+` FROM questions WHERE id = ?`, id))
}

// ListOpenQuestions intoarce intrebarile deschise ale proiectului, cele mai vechi primele.
func (s *Store) ListOpenQuestions(projectID int64) ([]Question, error) {
	rows, err := s.db.Query(`SELECT `+questionCols+` FROM questions WHERE project_id = ? AND status = 'open' ORDER BY id`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Question{}
	for rows.Next() {
		q, err := scanQuestion(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, q)
	}
	return out, rows.Err()
}

func (s *Store) CountOpenQuestions(projectID int64) (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM questions WHERE project_id = ? AND status = 'open'`, projectID).Scan(&n)
	return n, err
}

// CloseQuestion inchide o intrebare deschisa (raspuns sau dismiss). O intrebare
// deja inchisa intoarce ErrConflict, una inexistenta ErrNotFound.
func (s *Store) CloseQuestion(id int64, status, answer string) error {
	res, err := s.db.Exec(`UPDATE questions SET status = ?, answer = ?, closed_at = ? WHERE id = ? AND status = 'open'`,
		status, answer, now(), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 1 {
		return nil
	}
	if _, err := s.GetQuestion(id); err != nil {
		return err
	}
	return ErrConflict
}

// HasOpenQuestionForTask: task-ul are cel putin o intrebare deschisa. Regula:
// un task asteapta raspunsul utilizatorului cat timp are ORICE intrebare deschisa.
func (s *Store) HasOpenQuestionForTask(taskID int64) (bool, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM questions WHERE task_id = ? AND status = 'open'`, taskID).Scan(&n)
	return n > 0, err
}

// HasQuestionForRun: run-ul a pus cel putin o intrebare (deschisa sau inchisa).
func (s *Store) HasQuestionForRun(runID string) (bool, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM questions WHERE run_id = ? AND run_id != ''`, runID).Scan(&n)
	return n > 0, err
}
