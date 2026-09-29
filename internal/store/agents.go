package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

const (
	PositionLead   = "lead"
	PositionWorker = "worker"
)

type Agent struct {
	ID           int64     `json:"id"`
	ProjectID    int64     `json:"projectId"`
	Name         string    `json:"name"`
	Role         string    `json:"role"`
	Position     string    `json:"position"`
	Model        string    `json:"model"`
	Background   string    `json:"background"`
	Instructions string    `json:"instructions"`
	DoneDef      string    `json:"doneDef"`
	Tools        []string  `json:"tools"`
	SessionID    string    `json:"sessionId"`
	PromptHash   string    `json:"-"`
	CreatedAt    time.Time `json:"createdAt"`
}

const agentCols = `id, project_id, name, role, position, model, background, instructions, done_def, tools, session_id, prompt_hash, created_at`

func scanAgent(sc scanner) (Agent, error) {
	var a Agent
	var tools, created string
	err := sc.Scan(&a.ID, &a.ProjectID, &a.Name, &a.Role, &a.Position, &a.Model, &a.Background,
		&a.Instructions, &a.DoneDef, &tools, &a.SessionID, &a.PromptHash, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return Agent{}, ErrNotFound
	}
	if err != nil {
		return Agent{}, err
	}
	if err := json.Unmarshal([]byte(tools), &a.Tools); err != nil {
		return Agent{}, err
	}
	if a.Tools == nil {
		a.Tools = []string{}
	}
	a.CreatedAt = parseTime(created)
	return a, nil
}

func toolsJSON(tools []string) string {
	if tools == nil {
		tools = []string{}
	}
	b, _ := json.Marshal(tools)
	return string(b)
}

func insertAgent(ex execer, a *Agent) error {
	created := now()
	res, err := ex.Exec(`INSERT INTO agents (project_id, name, role, position, model, background, instructions, done_def, tools, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		a.ProjectID, a.Name, a.Role, a.Position, a.Model, a.Background, a.Instructions, a.DoneDef, toolsJSON(a.Tools), created)
	if isUnique(err) {
		return ErrConflict
	}
	if err != nil {
		return err
	}
	if a.ID, err = res.LastInsertId(); err != nil {
		return err
	}
	if a.Tools == nil {
		a.Tools = []string{}
	}
	a.CreatedAt = parseTime(created)
	return nil
}

// CreateAgent adauga un agent. Intoarce ErrConflict daca numele exista deja in
// proiect (fara diferenta intre litere mari si mici) sau daca ar fi al doilea lead.
func (s *Store) CreateAgent(a *Agent) error { return insertAgent(s.db, a) }

func (s *Store) GetAgent(id int64) (Agent, error) {
	return scanAgent(s.db.QueryRow(`SELECT `+agentCols+` FROM agents WHERE id = ?`, id))
}

// ListAgents intoarce echipa proiectului: lead-ul primul, apoi workerii in
// ordinea in care au fost adaugati.
func (s *Store) ListAgents(projectID int64) ([]Agent, error) {
	rows, err := s.db.Query(`SELECT `+agentCols+` FROM agents WHERE project_id = ?
		ORDER BY position = 'lead' DESC, id`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Agent{}
	for rows.Next() {
		a, err := scanAgent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Store) Lead(projectID int64) (Agent, error) {
	return scanAgent(s.db.QueryRow(`SELECT `+agentCols+` FROM agents WHERE project_id = ? AND position = 'lead'`, projectID))
}

// FindAgentByName cauta dupa nume fara diferenta intre litere mari si mici.
func (s *Store) FindAgentByName(projectID int64, name string) (Agent, error) {
	return scanAgent(s.db.QueryRow(`SELECT `+agentCols+` FROM agents
		WHERE project_id = ? AND name = ? COLLATE NOCASE`, projectID, name))
}

// UpdateAgent salveaza campurile editabile. Pozitia si proiectul nu se schimba.
func (s *Store) UpdateAgent(a Agent) error {
	res, err := s.db.Exec(`UPDATE agents SET name = ?, role = ?, model = ?, background = ?, instructions = ?, done_def = ?, tools = ?
		WHERE id = ?`, a.Name, a.Role, a.Model, a.Background, a.Instructions, a.DoneDef, toolsJSON(a.Tools), a.ID)
	if isUnique(err) {
		return ErrConflict
	}
	return mustAffect(res, err)
}

// DeleteAgent sterge un worker. Lead-ul si agentii care au task-uri nu se pot
// sterge (ErrConflict): task-urile ar ramane fara asignat.
func (s *Store) DeleteAgent(id int64) error {
	a, err := s.GetAgent(id)
	if err != nil {
		return err
	}
	if a.Position == PositionLead {
		return ErrConflict
	}
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM tasks WHERE assignee_id = ?`, id).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return ErrConflict
	}
	return mustAffect(s.db.Exec(`DELETE FROM agents WHERE id = ?`, id))
}

// SetSession retine sesiunea Claude a agentului (pentru --resume) si hash-ul
// system prompt-ului cu care a fost creata.
func (s *Store) SetSession(id int64, sessionID, promptHash string) error {
	return mustAffect(s.db.Exec(`UPDATE agents SET session_id = ?, prompt_hash = ? WHERE id = ?`, sessionID, promptHash, id))
}
