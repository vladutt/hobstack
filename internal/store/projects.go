package store

import (
	"database/sql"
	"errors"
	"time"
)

// Project: MaxParallel = 0 inseamna workeri in paralel fara limita,
// BudgetUSD = 0 inseamna fara buget per run.
type Project struct {
	ID          int64     `json:"id"`
	Name        string    `json:"name"`
	Goal        string    `json:"goal"`
	Context     string    `json:"context"`
	Workdir     string    `json:"workdir"`
	Paused      bool      `json:"paused"`
	MaxParallel int       `json:"maxParallel"`
	BudgetUSD   float64   `json:"budgetUsd"`
	CreatedAt   time.Time `json:"createdAt"`
}

const projectCols = `id, name, goal, context, workdir, paused, max_parallel, budget_usd, created_at`

func scanProject(sc scanner) (Project, error) {
	var p Project
	var created string
	err := sc.Scan(&p.ID, &p.Name, &p.Goal, &p.Context, &p.Workdir, &p.Paused, &p.MaxParallel, &p.BudgetUSD, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return Project{}, ErrNotFound
	}
	if err != nil {
		return Project{}, err
	}
	p.CreatedAt = parseTime(created)
	return p, nil
}

// CreateProject salveaza proiectul si ii creeaza lead-ul implicit in aceeasi
// tranzactie, ca un proiect sa nu existe niciodata fara lead.
func (s *Store) CreateProject(p *Project) (Agent, error) {
	created := now()

	tx, err := s.db.Begin()
	if err != nil {
		return Agent{}, err
	}
	defer tx.Rollback() // nu face nimic dupa Commit

	res, err := tx.Exec(`INSERT INTO projects (name, goal, context, workdir, paused, max_parallel, budget_usd, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		p.Name, p.Goal, p.Context, p.Workdir, p.Paused, p.MaxParallel, p.BudgetUSD, created)
	if err != nil {
		return Agent{}, err
	}
	if p.ID, err = res.LastInsertId(); err != nil {
		return Agent{}, err
	}
	p.CreatedAt = parseTime(created)

	lead := Agent{ProjectID: p.ID, Name: "Lead", Role: "Lead", Model: "sonnet"}
	if err := insertAgent(tx, &lead); err != nil {
		return Agent{}, err
	}
	return lead, tx.Commit()
}

func (s *Store) GetProject(id int64) (Project, error) {
	return scanProject(s.db.QueryRow(`SELECT `+projectCols+` FROM projects WHERE id = ?`, id))
}

func (s *Store) ListProjects() ([]Project, error) {
	rows, err := s.db.Query(`SELECT ` + projectCols + ` FROM projects ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Project{}
	for rows.Next() {
		p, err := scanProject(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// UpdateProject salveaza campurile editabile din UI (nu si paused).
func (s *Store) UpdateProject(p Project) error {
	return mustAffect(s.db.Exec(`UPDATE projects SET name = ?, goal = ?, context = ?, workdir = ?, max_parallel = ?, budget_usd = ?
		WHERE id = ?`, p.Name, p.Goal, p.Context, p.Workdir, p.MaxParallel, p.BudgetUSD, p.ID))
}

// DeleteProject sterge proiectul si, in cascada, agentii, task-urile, run-urile
// si mesajele lui. Folderul de lucru de pe disc nu e atins.
func (s *Store) DeleteProject(id int64) error {
	return mustAffect(s.db.Exec(`DELETE FROM projects WHERE id = ?`, id))
}

func (s *Store) SetPaused(id int64, paused bool) error {
	return mustAffect(s.db.Exec(`UPDATE projects SET paused = ? WHERE id = ?`, paused, id))
}

// PauseAll pune toate proiectele pe pauza (la pornirea serverului).
func (s *Store) PauseAll() error {
	_, err := s.db.Exec(`UPDATE projects SET paused = 1`)
	return err
}
