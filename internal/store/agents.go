package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

// LinkError: legatura ceruta intre agenti incalca un invariant (sef, reviewer, ciclu).
type LinkError string

func (e LinkError) Error() string { return string(e) }

type Agent struct {
	ID           int64     `json:"id"`
	ProjectID    int64     `json:"projectId"`
	Name         string    `json:"name"`
	Role         string    `json:"role"`
	Model        string    `json:"model"`
	Background   string    `json:"background"`
	Instructions string    `json:"instructions"`
	DoneDef      string    `json:"doneDef"`
	Tools        []string  `json:"tools"`
	SessionID    string    `json:"sessionId"`
	PromptHash   string    `json:"-"`
	CreatedAt    time.Time `json:"createdAt"`

	ParentID         *int64   `json:"parentId"`         // nil = raporteaza utilizatorului (radacina), daca nu e Detached
	Detached         bool     `json:"detached"`         // fara sef si nelegat de utilizator: nu lucreaza pana il legi
	ReviewerID       *int64   `json:"reviewerId"`       // nil = nu il verifica nimeni
	Responsibilities string   `json:"responsibilities"` // "Ce ia"
	PosX             *float64 `json:"posX"`             // nil = asezare automata pe canvas
	PosY             *float64 `json:"posY"`
}

const agentCols = `id, project_id, name, role, model, background, instructions, done_def, tools, session_id, prompt_hash, created_at, parent_id, reviewer_id, responsibilities, pos_x, pos_y, detached`

func scanAgent(sc scanner) (Agent, error) {
	var a Agent
	var tools, created string
	err := sc.Scan(&a.ID, &a.ProjectID, &a.Name, &a.Role, &a.Model, &a.Background,
		&a.Instructions, &a.DoneDef, &tools, &a.SessionID, &a.PromptHash, &created,
		&a.ParentID, &a.ReviewerID, &a.Responsibilities, &a.PosX, &a.PosY, &a.Detached)
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
	a.Detached = a.Detached && a.ParentID == nil // un agent cu sef e mereu legat
	res, err := ex.Exec(`INSERT INTO agents (project_id, name, role, position, model, background, instructions, done_def, tools, parent_id, reviewer_id, responsibilities, detached, created_at)
		VALUES (?, ?, ?, 'worker', ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, // 'worker': coloana veche, pastrata doar pentru ca e NOT NULL
		a.ProjectID, a.Name, a.Role, a.Model, a.Background, a.Instructions, a.DoneDef, toolsJSON(a.Tools),
		nullPtr(a.ParentID), nullPtr(a.ReviewerID), a.Responsibilities, a.Detached, created)
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

// queryer e implementat si de *sql.DB si de *sql.Tx.
type queryer interface {
	QueryRow(query string, args ...any) *sql.Row
}

// checkLinks verifica invariantii legaturilor inainte de salvare: seful si
// reviewer-ul sunt din acelasi proiect, nimeni nu e propriul sef sau
// reviewer, iar lantul de sefi nu face cerc.
func checkLinks(q queryer, a Agent) error {
	if a.ParentID != nil {
		if *a.ParentID == a.ID {
			return LinkError("an agent cannot be its own boss")
		}
		if !inProject(q, *a.ParentID, a.ProjectID) {
			return LinkError("the chosen boss is not in this project")
		}
		// Urcam pe lantul de sefi pornind de la noul sef. Daca ajungem la
		// agentul editat, legatura ar inchide un cerc. Un agent nou (ID 0) nu e
		// inca seful nimanui, deci nu poate crea un ciclu.
		if a.ID != 0 {
			cur := *a.ParentID
			for steps := 0; steps < 10000; steps++ { // limita: protectie la date deja stricate
				if cur == a.ID {
					return LinkError("the link would create a cycle")
				}
				var next sql.NullInt64
				if err := q.QueryRow(`SELECT parent_id FROM agents WHERE id = ?`, cur).Scan(&next); err != nil {
					return err
				}
				if !next.Valid {
					break
				}
				cur = next.Int64
			}
		}
	}
	if a.ReviewerID != nil {
		if *a.ReviewerID == a.ID {
			return LinkError("an agent cannot be its own reviewer")
		}
		if !inProject(q, *a.ReviewerID, a.ProjectID) {
			return LinkError("the chosen reviewer is not in this project")
		}
	}
	return nil
}

func inProject(q queryer, agentID, projectID int64) bool {
	var n int
	q.QueryRow(`SELECT COUNT(*) FROM agents WHERE id = ? AND project_id = ?`, agentID, projectID).Scan(&n)
	return n == 1
}

// CreateAgent adauga un agent dupa ce verifica legaturile (LinkError). Intoarce
// ErrConflict daca numele exista deja in proiect (fara diferenta intre litere
// mari si mici).
func (s *Store) CreateAgent(a *Agent) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := checkLinks(tx, *a); err != nil {
		return err
	}
	if err := insertAgent(tx, a); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) GetAgent(id int64) (Agent, error) {
	return scanAgent(s.db.QueryRow(`SELECT `+agentCols+` FROM agents WHERE id = ?`, id))
}

// ListAgents intoarce echipa proiectului: radacinile primele, apoi nelegatii,
// apoi restul, in ordinea adaugarii.
func (s *Store) ListAgents(projectID int64) ([]Agent, error) {
	rows, err := s.db.Query(`SELECT `+agentCols+` FROM agents WHERE project_id = ?
		ORDER BY parent_id IS NOT NULL, detached, id`, projectID)
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

// FindAgentByName cauta dupa nume fara diferenta intre litere mari si mici.
func (s *Store) FindAgentByName(projectID int64, name string) (Agent, error) {
	return scanAgent(s.db.QueryRow(`SELECT `+agentCols+` FROM agents
		WHERE project_id = ? AND name = ? COLLATE NOCASE`, projectID, name))
}

// UpdateAgent salveaza campurile editabile si legaturile (dupa checkLinks).
// Proiectul si pozitia pe canvas nu se schimba aici.
func (s *Store) UpdateAgent(a Agent) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := checkLinks(tx, a); err != nil {
		return err
	}
	a.Detached = a.Detached && a.ParentID == nil // un agent cu sef e mereu legat
	res, err := tx.Exec(`UPDATE agents SET name = ?, role = ?, model = ?, background = ?, instructions = ?, done_def = ?, tools = ?,
		responsibilities = ?, parent_id = ?, reviewer_id = ?, detached = ?
		WHERE id = ?`, a.Name, a.Role, a.Model, a.Background, a.Instructions, a.DoneDef, toolsJSON(a.Tools),
		a.Responsibilities, nullPtr(a.ParentID), nullPtr(a.ReviewerID), a.Detached, a.ID)
	if isUnique(err) {
		return ErrConflict
	}
	if err := mustAffect(res, err); err != nil {
		return err
	}
	return tx.Commit()
}

// SetAgentPosition retine pozitia agentului pe canvas.
func (s *Store) SetAgentPosition(id int64, x, y float64) error {
	return mustAffect(s.db.Exec(`UPDATE agents SET pos_x = ?, pos_y = ? WHERE id = ?`, x, y, id))
}

// DeleteAgent sterge un agent. Copiii lui trec la seful lui; daca nu avea sef,
// raman nelegati. Cei pe care ii verifica raman fara reviewer. Agentii cu
// task-uri si ultimul agent din proiect nu se pot sterge (ErrConflict).
func (s *Store) DeleteAgent(id int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	a, err := scanAgent(tx.QueryRow(`SELECT `+agentCols+` FROM agents WHERE id = ?`, id))
	if err != nil {
		return err
	}
	var tasks, others int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM tasks WHERE assignee_id = ?`, id).Scan(&tasks); err != nil {
		return err
	}
	if err := tx.QueryRow(`SELECT COUNT(*) FROM agents WHERE project_id = ? AND id != ?`, a.ProjectID, id).Scan(&others); err != nil {
		return err
	}
	if tasks > 0 || others == 0 {
		return ErrConflict
	}
	if _, err := tx.Exec(`UPDATE agents SET parent_id = ?, detached = ? WHERE parent_id = ?`, nullPtr(a.ParentID), a.ParentID == nil, id); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE agents SET reviewer_id = NULL WHERE reviewer_id = ?`, id); err != nil {
		return err
	}
	if err := mustAffect(tx.Exec(`DELETE FROM agents WHERE id = ?`, id)); err != nil {
		return err
	}
	return tx.Commit()
}

// SetSession retine sesiunea Claude a agentului (pentru --resume) si hash-ul
// system prompt-ului cu care a fost creata.
func (s *Store) SetSession(id int64, sessionID, promptHash string) error {
	return mustAffect(s.db.Exec(`UPDATE agents SET session_id = ?, prompt_hash = ? WHERE id = ?`, sessionID, promptHash, id))
}
