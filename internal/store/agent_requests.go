package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

// Starile unei cereri de agent nou (add_agent cand liderul nu poate adauga direct).
const (
	AgentRequestOpen      = "open"
	AgentRequestApproved  = "approved"
	AgentRequestDismissed = "dismissed"
)

// AgentSpec e agentul propus de un lider; se salveaza ca JSON in agent_requests.spec.
type AgentSpec struct {
	Name         string   `json:"name"`
	Role         string   `json:"role"`
	Model        string   `json:"model"`
	Background   string   `json:"background"`
	Instructions string   `json:"instructions"`
	DoneDef      string   `json:"doneDef"`
	Tools        []string `json:"tools"` // mereu non-nil
}

// AgentRequest e o cerere a unui lider de a adauga un agent, in asteptarea utilizatorului.
type AgentRequest struct {
	ID        int64      `json:"id"`
	ProjectID int64      `json:"projectId"`
	LeadID    int64      `json:"leadId"`
	TaskID    *int64     `json:"taskId"` // task-ul run-ului care a cerut; nil = fara task
	RunID     string     `json:"runId"`
	Spec      AgentSpec  `json:"spec"`
	Reason    string     `json:"reason"`
	Status    string     `json:"status"`
	AgentID   *int64     `json:"agentId"` // agentul creat la aprobare
	CreatedAt time.Time  `json:"createdAt"`
	ClosedAt  *time.Time `json:"closedAt"`
}

const agentRequestCols = `id, project_id, lead_id, task_id, run_id, spec, reason, status, agent_id, created_at, closed_at`

func scanAgentRequest(sc scanner) (AgentRequest, error) {
	var r AgentRequest
	var spec, created string
	var closed sql.NullString
	err := sc.Scan(&r.ID, &r.ProjectID, &r.LeadID, &r.TaskID, &r.RunID, &spec, &r.Reason, &r.Status, &r.AgentID, &created, &closed)
	if errors.Is(err, sql.ErrNoRows) {
		return AgentRequest{}, ErrNotFound
	}
	if err != nil {
		return AgentRequest{}, err
	}
	if err := json.Unmarshal([]byte(spec), &r.Spec); err != nil {
		return AgentRequest{}, err
	}
	if r.Spec.Tools == nil {
		r.Spec.Tools = []string{}
	}
	r.CreatedAt = parseTime(created)
	if closed.Valid {
		t := parseTime(closed.String)
		r.ClosedAt = &t
	}
	return r, nil
}

// AddAgentRequest salveaza o cerere deschisa.
func (s *Store) AddAgentRequest(r *AgentRequest) error {
	if r.Spec.Tools == nil {
		r.Spec.Tools = []string{}
	}
	spec, err := json.Marshal(r.Spec)
	if err != nil {
		return err
	}
	created := now()
	res, err := s.db.Exec(`INSERT INTO agent_requests (project_id, lead_id, task_id, run_id, spec, reason, status, created_at)
		VALUES (?, ?, ?, ?, ?, ?, 'open', ?)`, r.ProjectID, r.LeadID, nullPtr(r.TaskID), r.RunID, string(spec), r.Reason, created)
	if err != nil {
		return err
	}
	if r.ID, err = res.LastInsertId(); err != nil {
		return err
	}
	r.Status, r.CreatedAt = AgentRequestOpen, parseTime(created)
	return nil
}

func (s *Store) GetAgentRequest(id int64) (AgentRequest, error) {
	return scanAgentRequest(s.db.QueryRow(`SELECT `+agentRequestCols+` FROM agent_requests WHERE id = ?`, id))
}

// ListOpenAgentRequests intoarce cererile deschise ale proiectului, cele mai vechi primele.
func (s *Store) ListOpenAgentRequests(projectID int64) ([]AgentRequest, error) {
	rows, err := s.db.Query(`SELECT `+agentRequestCols+` FROM agent_requests WHERE project_id = ? AND status = 'open' ORDER BY id`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AgentRequest{}
	for rows.Next() {
		r, err := scanAgentRequest(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) CountOpenAgentRequests(projectID int64) (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM agent_requests WHERE project_id = ? AND status = 'open'`, projectID).Scan(&n)
	return n, err
}

// OpenAgentRequestNamed: exista o cerere deschisa in proiect pentru un agent cu numele dat
// (fara diferenta intre litere mari si mici). Un nume cerut e ocupat pana la decizie.
func (s *Store) OpenAgentRequestNamed(projectID int64, name string) (bool, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM agent_requests WHERE project_id = ? AND status = 'open'
		AND json_extract(spec, '$.name') = ? COLLATE NOCASE`, projectID, name).Scan(&n)
	return n > 0, err
}

// CountAddedBy numara ce ocupa din limita unui lider: agentii adaugati de el
// care sunt inca in echipa (nedetasati) plus cererile lui deschise.
func (s *Store) CountAddedBy(leadID int64) (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT
		(SELECT COUNT(*) FROM agents WHERE created_by = ? AND detached = 0) +
		(SELECT COUNT(*) FROM agent_requests WHERE lead_id = ? AND status = 'open')`, leadID, leadID).Scan(&n)
	return n, err
}

// ApproveAgentRequest creeaza agentul a si inchide cererea, intr-o tranzactie.
// O cerere deja inchisa sau un nume luat intorc ErrConflict (nu se schimba nimic).
func (s *Store) ApproveAgentRequest(id int64, a *Agent) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var status string
	err = tx.QueryRow(`SELECT status FROM agent_requests WHERE id = ?`, id).Scan(&status)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if status != AgentRequestOpen {
		return ErrConflict
	}
	if err := checkLinks(tx, *a); err != nil {
		return err
	}
	if err := insertAgent(tx, a); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE agent_requests SET status = 'approved', agent_id = ?, closed_at = ? WHERE id = ?`, a.ID, now(), id); err != nil {
		return err
	}
	return tx.Commit()
}

// DismissAgentRequest inchide o cerere deschisa fara agent. Una deja inchisa
// intoarce ErrConflict, una inexistenta ErrNotFound.
func (s *Store) DismissAgentRequest(id int64) error {
	res, err := s.db.Exec(`UPDATE agent_requests SET status = 'dismissed', closed_at = ? WHERE id = ? AND status = 'open'`, now(), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 1 {
		return nil
	}
	if _, err := s.GetAgentRequest(id); err != nil {
		return err
	}
	return ErrConflict
}
