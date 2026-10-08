package store

const (
	CmdAllow = "allow"
	CmdAsk   = "ask"
	CmdBlock = "block"
)

// CommandRule: un prefix de comanda (ex. "git push") si ce se intampla cu el.
type CommandRule struct {
	Prefix string `json:"prefix"`
	Mode   string `json:"mode"`
}

// DefaultCommandRules: ce era blocat fix inainte; trei pot fi aprobate din Needs you.
var DefaultCommandRules = []CommandRule{
	{Prefix: "git push", Mode: CmdAsk},
	{Prefix: "rm -rf", Mode: CmdAsk},
	{Prefix: "brew install", Mode: CmdAsk},
	{Prefix: "sudo", Mode: CmdBlock},
}

// CommandRules intoarce regulile unui nivel (0 = globale), dupa prefix; lista e mereu non-nil.
func (s *Store) CommandRules(projectID int64) ([]CommandRule, error) {
	rows, err := s.db.Query(`SELECT prefix, mode FROM command_rules WHERE project_id = ? ORDER BY prefix`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CommandRule{}
	for rows.Next() {
		var r CommandRule
		if err := rows.Scan(&r.Prefix, &r.Mode); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// SetCommandRules inlocuieste toate regulile nivelului intr-o tranzactie.
func (s *Store) SetCommandRules(projectID int64, rules []CommandRule) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM command_rules WHERE project_id = ?`, projectID); err != nil {
		return err
	}
	ts := now()
	for _, r := range rules {
		if _, err := tx.Exec(`INSERT INTO command_rules (project_id, prefix, mode, updated_at) VALUES (?, ?, ?, ?)`,
			projectID, r.Prefix, r.Mode, ts); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) ResetGlobalCommandRules() error { return s.SetCommandRules(0, DefaultCommandRules) }

func (s *Store) AddCommandGrant(projectID, agentID int64, prefix string) error {
	_, err := s.db.Exec(`INSERT INTO command_grants (project_id, agent_id, prefix, created_at) VALUES (?, ?, ?, ?)`,
		projectID, agentID, prefix, now())
	return err
}

// TakeCommandGrants intoarce prefixele cu grant nefolosit ale agentului si le marcheaza folosite (un run).
func (s *Store) TakeCommandGrants(agentID int64) ([]string, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	rows, err := tx.Query(`SELECT prefix FROM command_grants WHERE agent_id = ? AND used_at IS NULL`, agentID)
	if err != nil {
		return nil, err
	}
	out := []string{}
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, p)
	}
	rows.Close()
	if _, err := tx.Exec(`UPDATE command_grants SET used_at = ? WHERE agent_id = ? AND used_at IS NULL`,
		now(), agentID); err != nil {
		return nil, err
	}
	return out, tx.Commit()
}
