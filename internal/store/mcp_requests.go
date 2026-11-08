package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

// Starile unei cereri install_mcp.
const (
	MCPRequestOpen      = "open"
	MCPRequestInstalled = "installed"
	MCPRequestDeclined  = "declined"
)

// MCPRequest: un agent cere un server MCP pentru proiectul lui; il instaleaza doar utilizatorul (Needs you).
type MCPRequest struct {
	ID          int64      `json:"id"`
	ProjectID   int64      `json:"projectId"`
	AgentID     int64      `json:"agentId"`
	TaskID      *int64     `json:"taskId"`
	Name        string     `json:"name"`
	Description string     `json:"description"`
	Transport   string     `json:"transport"` // local | remote
	Command     string     `json:"command"`
	Args        []string   `json:"args"`    // mereu non-nil
	URL         string     `json:"url"`     // remote
	Secrets     []string   `json:"secrets"` // mereu non-nil
	ItemID      *int64     `json:"itemId"`  // element Library existent (se activeaza in proiect)
	Reason      string     `json:"reason"`
	Status      string     `json:"status"`
	CreatedAt   time.Time  `json:"createdAt"`
	ClosedAt    *time.Time `json:"closedAt"`
}

const mcpRequestCols = `id, project_id, agent_id, task_id, name, description, transport, command, args, url, secrets, item_id, reason, status, created_at, closed_at`

func scanMCPRequest(sc scanner) (MCPRequest, error) {
	var r MCPRequest
	var args, secrets, created string
	var closed sql.NullString
	err := sc.Scan(&r.ID, &r.ProjectID, &r.AgentID, &r.TaskID, &r.Name, &r.Description, &r.Transport, &r.Command, &args,
		&r.URL, &secrets, &r.ItemID, &r.Reason, &r.Status, &created, &closed)
	if errors.Is(err, sql.ErrNoRows) {
		return MCPRequest{}, ErrNotFound
	}
	if err != nil {
		return MCPRequest{}, err
	}
	if err := json.Unmarshal([]byte(args), &r.Args); err != nil || r.Args == nil {
		r.Args = []string{}
	}
	if err := json.Unmarshal([]byte(secrets), &r.Secrets); err != nil || r.Secrets == nil {
		r.Secrets = []string{}
	}
	r.CreatedAt = parseTime(created)
	if closed.Valid {
		t := parseTime(closed.String)
		r.ClosedAt = &t
	}
	return r, nil
}

// AddMCPRequest salveaza o cerere deschisa.
func (s *Store) AddMCPRequest(r *MCPRequest) error {
	if r.Args == nil {
		r.Args = []string{}
	}
	if r.Secrets == nil {
		r.Secrets = []string{}
	}
	args, _ := json.Marshal(r.Args)
	secrets, _ := json.Marshal(r.Secrets)
	created := now()
	res, err := s.db.Exec(`INSERT INTO mcp_requests (project_id, agent_id, task_id, name, description, transport, command, args, url, secrets, item_id, reason, status, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'open', ?)`, r.ProjectID, r.AgentID, nullPtr(r.TaskID), r.Name, r.Description,
		r.Transport, r.Command, string(args), r.URL, string(secrets), nullPtr(r.ItemID), r.Reason, created)
	if err != nil {
		return err
	}
	if r.ID, err = res.LastInsertId(); err != nil {
		return err
	}
	r.Status, r.CreatedAt = MCPRequestOpen, parseTime(created)
	return nil
}

func (s *Store) GetMCPRequest(id int64) (MCPRequest, error) {
	return scanMCPRequest(s.db.QueryRow(`SELECT `+mcpRequestCols+` FROM mcp_requests WHERE id = ?`, id))
}

// ListOpenMCPRequests: cererile deschise ale proiectului, cele mai vechi primele (lista e mereu non-nil).
func (s *Store) ListOpenMCPRequests(projectID int64) ([]MCPRequest, error) {
	rows, err := s.db.Query(`SELECT `+mcpRequestCols+` FROM mcp_requests WHERE project_id = ? AND status = 'open' ORDER BY id`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []MCPRequest{}
	for rows.Next() {
		r, err := scanMCPRequest(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) CountOpenMCPRequests(projectID int64) (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM mcp_requests WHERE project_id = ? AND status = 'open'`, projectID).Scan(&n)
	return n, err
}

// OpenMCPRequestNamed: exista o cerere deschisa in proiect pentru un server cu numele dat (fara diferente de litere).
func (s *Store) OpenMCPRequestNamed(projectID int64, name string) (bool, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM mcp_requests WHERE project_id = ? AND status = 'open' AND name = ? COLLATE NOCASE`,
		projectID, name).Scan(&n)
	return n > 0, err
}

// CloseMCPRequest inchide o cerere deschisa (installed / declined); ErrConflict daca era deja inchisa.
func (s *Store) CloseMCPRequest(id int64, status string) error {
	res, err := s.db.Exec(`UPDATE mcp_requests SET status = ?, closed_at = ? WHERE id = ? AND status = 'open'`, status, now(), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		if _, err := s.GetMCPRequest(id); err != nil {
			return err
		}
		return ErrConflict
	}
	return nil
}
