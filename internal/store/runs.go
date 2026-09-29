package store

import (
	"database/sql"
	"errors"
	"time"
)

const (
	RunRunning     = "running"
	RunDone        = "done"
	RunFailed      = "failed"
	RunStopped     = "stopped"
	RunInterrupted = "interrupted"
)

// Run e o executie `claude -p` a unui agent. Logul brut e in logs/runs/<id>.jsonl.
type Run struct {
	ID        string     `json:"id"`
	AgentID   int64      `json:"agentId"`
	TaskID    int64      `json:"taskId"` // 0 = run fara task (lead sau mesaj direct)
	Status    string     `json:"status"`
	StartedAt time.Time  `json:"startedAt"`
	EndedAt   *time.Time `json:"endedAt"`
	CostUSD   float64    `json:"costUsd"`
	Error     string     `json:"error"`
}

// nullID transforma 0 in NULL pentru coloanele cu cheie straina optionala.
func nullID(id int64) any {
	if id == 0 {
		return nil
	}
	return id
}

func (s *Store) CreateRun(r *Run) error {
	ts := now()
	r.Status = RunRunning
	_, err := s.db.Exec(`INSERT INTO runs (id, agent_id, task_id, status, started_at) VALUES (?, ?, ?, ?, ?)`,
		r.ID, r.AgentID, nullID(r.TaskID), r.Status, ts)
	r.StartedAt = parseTime(ts)
	return err
}

func (s *Store) FinishRun(id, status string, costUSD float64, errText string) error {
	return mustAffect(s.db.Exec(`UPDATE runs SET status = ?, ended_at = ?, cost_usd = ?, error = ? WHERE id = ?`,
		status, now(), costUSD, errText, id))
}

func (s *Store) GetRun(id string) (Run, error) {
	var r Run
	var task sql.NullInt64
	var started string
	var ended sql.NullString
	var cost sql.NullFloat64
	err := s.db.QueryRow(`SELECT id, agent_id, task_id, status, started_at, ended_at, cost_usd, error FROM runs WHERE id = ?`, id).
		Scan(&r.ID, &r.AgentID, &task, &r.Status, &started, &ended, &cost, &r.Error)
	if errors.Is(err, sql.ErrNoRows) {
		return Run{}, ErrNotFound
	}
	if err != nil {
		return Run{}, err
	}
	r.TaskID = task.Int64
	r.StartedAt = parseTime(started)
	if ended.Valid {
		t := parseTime(ended.String)
		r.EndedAt = &t
	}
	r.CostUSD = cost.Float64
	return r, nil
}

// InterruptRunning marcheaza run-urile ramase "running" dupa un restart:
// procesele lor au murit odata cu serverul.
func (s *Store) InterruptRunning() error {
	_, err := s.db.Exec(`UPDATE runs SET status = 'interrupted', ended_at = ? WHERE status = 'running'`, now())
	return err
}
