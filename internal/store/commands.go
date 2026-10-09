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

// AddCommandGrant permite prefixul agentului pe un task (taskID nil = in chat, pana la New chat).
// Acelasi grant de doua ori nu face un rand nou.
func (s *Store) AddCommandGrant(projectID, agentID int64, taskID *int64, prefix string) error {
	_, err := s.db.Exec(`INSERT INTO command_grants (project_id, agent_id, task_id, prefix, created_at)
		SELECT ?, ?, ?, ?, ? WHERE NOT EXISTS
		(SELECT 1 FROM command_grants WHERE agent_id = ? AND task_id IS ? AND prefix = ?)`,
		projectID, agentID, nullPtr(taskID), prefix, now(), agentID, nullPtr(taskID), prefix)
	return err
}

// CommandGrants intoarce prefixele permise agentului pentru un run pe task-ul dat
// (taskID 0 = run fara task, adica grant-urile de chat). Nu se consuma: tin cat task-ul.
func (s *Store) CommandGrants(agentID, taskID int64) ([]string, error) {
	var tid any // NULL pentru chat
	if taskID != 0 {
		tid = taskID
	}
	rows, err := s.db.Query(`SELECT DISTINCT prefix FROM command_grants WHERE agent_id = ? AND task_id IS ? ORDER BY prefix`,
		agentID, tid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
