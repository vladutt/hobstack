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

// nullPtr transforma un pointer nil in NULL pentru coloanele optionale.
func nullPtr(p *int64) any {
	if p == nil {
		return nil
	}
	return *p
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

// SetRunTokens salveaza tokenii unui run (-1 = necunoscut, ex. log lipsa).
func (s *Store) SetRunTokens(id string, tokens int64) error {
	_, err := s.db.Exec(`UPDATE runs SET tokens = ? WHERE id = ?`, tokens, id)
	return err
}

// RunsWithoutTokens: run-urile terminate al caror numar de tokeni nu a fost citit inca
// (cele dinainte de coloana tokens); se completeaza din loguri la pornire.
func (s *Store) RunsWithoutTokens() ([]string, error) {
	rows, err := s.db.Query(`SELECT id FROM runs WHERE tokens = 0 AND status != 'running'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// ProjectUsage: costul si tokenii tuturor run-urilor agentilor unui proiect.
// Run-urile agentilor stersi au plecat odata cu ei, deci nu mai intra in total.
func (s *Store) ProjectUsage(projectID int64) (costUSD float64, tokens int64, err error) {
	err = s.db.QueryRow(`SELECT COALESCE(SUM(r.cost_usd), 0), COALESCE(SUM(MAX(r.tokens, 0)), 0)
		FROM runs r JOIN agents a ON a.id = r.agent_id WHERE a.project_id = ?`, projectID).Scan(&costUSD, &tokens)
	return costUSD, tokens, err
}

// ProjectActivity: cate run-uri au pornit agentii proiectului in fiecare din ultimele
// `days` zile (ora locala), de la cea mai veche la azi. Coloana "Activity" din Proiecte.
func (s *Store) ProjectActivity(projectID int64, days int, at time.Time) ([]int, error) {
	out := make([]int, days)
	today := time.Date(at.Year(), at.Month(), at.Day(), 0, 0, 0, 0, at.Location())
	from := today.AddDate(0, 0, -(days - 1))
	rows, err := s.db.Query(`SELECT r.started_at FROM runs r JOIN agents a ON a.id = r.agent_id
		WHERE a.project_id = ? AND r.started_at >= ?`, projectID, from.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var started string
		if err := rows.Scan(&started); err != nil {
			return nil, err
		}
		t := parseTime(started).In(at.Location())
		day := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, at.Location())
		if i := int(day.Sub(from).Hours()/24 + 0.5); i >= 0 && i < days {
			out[i]++
		}
	}
	return out, rows.Err()
}

// TaskUsage: ce au consumat run-urile unui task (toti agentii care au lucrat pe el).
type TaskUsage struct {
	Runs    int     `json:"runs"`
	CostUSD float64 `json:"costUsd"`
	Tokens  int64   `json:"tokens"`
	Seconds int64   `json:"seconds"` // suma duratelor run-urilor terminate (timp de lucru, nu de asteptare)
}

func (s *Store) TaskUsage(taskID int64) (TaskUsage, error) {
	var u TaskUsage
	rows, err := s.db.Query(`SELECT started_at, ended_at, COALESCE(cost_usd, 0), tokens FROM runs WHERE task_id = ?`, taskID)
	if err != nil {
		return u, err
	}
	defer rows.Close()
	for rows.Next() {
		var started string
		var ended sql.NullString
		var cost float64
		var tokens int64
		if err := rows.Scan(&started, &ended, &cost, &tokens); err != nil {
			return u, err
		}
		u.Runs++
		u.CostUSD += cost
		if tokens > 0 {
			u.Tokens += tokens
		}
		if ended.Valid {
			if d := parseTime(ended.String).Sub(parseTime(started)); d > 0 {
				u.Seconds += int64(d.Seconds())
			}
		}
	}
	return u, rows.Err()
}
