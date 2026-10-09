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
	Autonomy    bool      `json:"autonomy"` // full autonomy: agentii nu pun intrebari; decide radacina
	BudgetUSD   float64   `json:"budgetUsd"`
	Favorite    bool      `json:"favorite"`  // steluta din lista de proiecte; se schimba doar prin SetFavorite
	AccountID   *int64    `json:"accountId"` // contul Claude; nil = Default (CLI-ul logat). Se schimba doar prin SetProjectAccount
	CreatedAt   time.Time `json:"createdAt"`
	Folders     []Folder  `json:"folders"` // cel putin unul; primul e cel implicit
}

const projectCols = `id, name, goal, context, workdir, paused, max_parallel, budget_usd, autonomy, favorite, created_at, account_id`

func scanProject(sc scanner) (Project, error) {
	var p Project
	var created string
	var account sql.NullInt64
	err := sc.Scan(&p.ID, &p.Name, &p.Goal, &p.Context, &p.Workdir, &p.Paused, &p.MaxParallel, &p.BudgetUSD, &p.Autonomy, &p.Favorite, &created, &account)
	if errors.Is(err, sql.ErrNoRows) {
		return Project{}, ErrNotFound
	}
	if err != nil {
		return Project{}, err
	}
	p.CreatedAt = parseTime(created)
	if account.Valid {
		p.AccountID = &account.Int64
	}
	return p, nil
}

// CreateProject salveaza proiectul si ii creeaza lead-ul implicit in aceeasi
// tranzactie, ca un proiect sa nu existe niciodata fara lead.
func (s *Store) CreateProject(p *Project) (Agent, error) {
	defaultFolders(p)
	created := now()

	tx, err := s.db.Begin()
	if err != nil {
		return Agent{}, err
	}
	defer tx.Rollback() // nu face nimic dupa Commit

	res, err := tx.Exec(`INSERT INTO projects (name, goal, context, workdir, paused, max_parallel, budget_usd, autonomy, created_at, account_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.Name, p.Goal, p.Context, p.Workdir, p.Paused, p.MaxParallel, p.BudgetUSD, p.Autonomy, created, nullPtr(p.AccountID))
	if err != nil {
		return Agent{}, err
	}
	if p.ID, err = res.LastInsertId(); err != nil {
		return Agent{}, err
	}
	p.CreatedAt = parseTime(created)
	if err := saveFolders(tx, p.ID, p.Folders); err != nil {
		return Agent{}, err
	}

	lead := Agent{ProjectID: p.ID, Name: "Lead", Role: "Lead", Model: "sonnet"}
	if err := insertAgent(tx, &lead); err != nil {
		return Agent{}, err
	}
	return lead, tx.Commit()
}

// NewMember e un agent din echipa unui proiect nou. Parent si Reviewer sunt
// indecsi in aceeasi lista (-1 = niciunul); id-urile nu exista inca.
type NewMember struct {
	Agent                Agent
	Parent, Reviewer     int    // index in aceeasi lista; -1 = niciunul (sau agent existent, vezi mai jos)
	Folder               int    // index in p.Folders; 0 = primul folder (salvat ca NULL)
	ParentID, ReviewerID *int64 // agent existent in proiect, cand Parent/Reviewer = -1 (doar AddAgents)
}

// CreateProjectWithTeam creeaza proiectul si echipa data (fara Lead-ul
// implicit) intr-o singura tranzactie: la orice eroare nu ramane nimic.
// Validarea legaturilor (cicluri, radacina) se face inainte, de apelant.
func (s *Store) CreateProjectWithTeam(p *Project, team []NewMember) ([]Agent, error) {
	defaultFolders(p)
	created := now()
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	res, err := tx.Exec(`INSERT INTO projects (name, goal, context, workdir, paused, max_parallel, budget_usd, autonomy, created_at, account_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.Name, p.Goal, p.Context, p.Workdir, p.Paused, p.MaxParallel, p.BudgetUSD, p.Autonomy, created, nullPtr(p.AccountID))
	if err != nil {
		return nil, err
	}
	if p.ID, err = res.LastInsertId(); err != nil {
		return nil, err
	}
	p.CreatedAt = parseTime(created)
	if err := saveFolders(tx, p.ID, p.Folders); err != nil {
		return nil, err
	}

	// intai toti agentii, fara legaturi; apoi legaturile, cand avem id-urile
	agents := make([]Agent, len(team))
	for i, m := range team {
		a := m.Agent
		a.ProjectID, a.ParentID, a.ReviewerID, a.Detached = p.ID, nil, nil, false
		a.FolderID = nil // index 0 (sau invalid) = primul folder
		if m.Folder > 0 && m.Folder < len(p.Folders) {
			id := p.Folders[m.Folder].ID
			a.FolderID = &id
		}
		if err := insertAgent(tx, &a); err != nil {
			return nil, err
		}
		agents[i] = a
	}
	idAt := func(i int) *int64 {
		if i < 0 || i >= len(agents) {
			return nil
		}
		id := agents[i].ID
		return &id
	}
	for i, m := range team {
		agents[i].ParentID, agents[i].ReviewerID = idAt(m.Parent), idAt(m.Reviewer)
		if _, err := tx.Exec(`UPDATE agents SET parent_id = ?, reviewer_id = ? WHERE id = ?`,
			nullPtr(agents[i].ParentID), nullPtr(agents[i].ReviewerID), agents[i].ID); err != nil {
			return nil, err
		}
	}
	return agents, tx.Commit()
}

func (s *Store) GetProject(id int64) (Project, error) {
	p, err := scanProject(s.db.QueryRow(`SELECT `+projectCols+` FROM projects WHERE id = ?`, id))
	if err != nil {
		return p, err
	}
	p.Folders, err = s.ListFolders(p.ID)
	return p, err
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
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// o singura conexiune: inchidem cursorul inainte de alte interogari
	rows.Close()
	for i := range out {
		if out[i].Folders, err = s.ListFolders(out[i].ID); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// UpdateProject salveaza campurile editabile din UI (nu si paused). Cu
// p.Folders nevid, inlocuieste si folderele (vezi saveFolders).
func (s *Store) UpdateProject(p Project) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if len(p.Folders) > 0 {
		p.Workdir = p.Folders[0].Path
	}
	if err := mustAffect(tx.Exec(`UPDATE projects SET name = ?, goal = ?, context = ?, workdir = ?, max_parallel = ?, budget_usd = ?, autonomy = ?
		WHERE id = ?`, p.Name, p.Goal, p.Context, p.Workdir, p.MaxParallel, p.BudgetUSD, p.Autonomy, p.ID)); err != nil {
		return err
	}
	if len(p.Folders) > 0 {
		if err := saveFolders(tx, p.ID, p.Folders); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// DeleteProject sterge proiectul si, in cascada, agentii, task-urile, run-urile
// si mesajele lui. Folderul de lucru de pe disc nu e atins.
func (s *Store) DeleteProject(id int64) error {
	if id <= 0 { // 0 e cheia regulilor globale de comenzi
		return ErrNotFound
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := mustAffect(tx.Exec(`DELETE FROM projects WHERE id = ?`, id)); err != nil {
		return err
	}
	// command_rules nu are FK catre projects, deci le stergem explicit, doar dupa ce proiectul exista
	if _, err := tx.Exec(`DELETE FROM command_rules WHERE project_id = ?`, id); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) SetPaused(id int64, paused bool) error {
	return mustAffect(s.db.Exec(`UPDATE projects SET paused = ? WHERE id = ?`, paused, id))
}

// SetFavorite pune sau scoate steluta unui proiect.
func (s *Store) SetFavorite(id int64, on bool) error {
	return mustAffect(s.db.Exec(`UPDATE projects SET favorite = ? WHERE id = ?`, on, id))
}
