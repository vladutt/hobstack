package store

import (
	"database/sql"
	"errors"
	"time"
)

// Account e un cont Claude (abonament) pe care ruleaza proiectele care il aleg, prin
// CLAUDE_CODE_OAUTH_TOKEN. Token-ul e camp privat: nu ajunge niciodata in JSON.
type Account struct {
	ID        int64      `json:"id"`
	Name      string     `json:"name"`
	TokenHint string     `json:"tokenHint"` // ultimele 4 caractere, ca utilizatorul sa-l recunoasca
	CreatedAt time.Time  `json:"createdAt"`
	CheckedAt *time.Time `json:"checkedAt"` // ultima verificare reusita; nil = niciodata
	LastError string     `json:"lastError"` // ultima eroare de autentificare; gol = merge
	Projects  int        `json:"projects"`  // cate proiecte il folosesc
	token     string
}

// ErrAccountInUse: contul e ales de proiecte; se sterge doar dupa ce le muti pe alt cont.
var ErrAccountInUse = errors.New("the account is used by projects; move them to another account first")

// Env: mediul pentru run-urile care folosesc contul.
func (a Account) Env() []string { return []string{"CLAUDE_CODE_OAUTH_TOKEN=" + a.token} }

const accountCols = `id, name, token, created_at, checked_at, last_error,
	(SELECT COUNT(*) FROM projects WHERE projects.account_id = claude_accounts.id)`

func scanAccount(sc scanner) (Account, error) {
	var a Account
	var created, checked string
	err := sc.Scan(&a.ID, &a.Name, &a.token, &created, &checked, &a.LastError, &a.Projects)
	if errors.Is(err, sql.ErrNoRows) {
		return Account{}, ErrNotFound
	}
	if err != nil {
		return Account{}, err
	}
	a.CreatedAt = parseTime(created)
	if checked != "" {
		t := parseTime(checked)
		a.CheckedAt = &t
	}
	a.TokenHint = tokenHint(a.token)
	return a, nil
}

// tokenHint: ultimele 4 caractere; un token scurt nu primeste indiciu (ar fi aproape tot).
func tokenHint(token string) string {
	r := []rune(token)
	if len(r) < 12 {
		return ""
	}
	return string(r[len(r)-4:])
}

func (s *Store) Accounts() ([]Account, error) {
	rows, err := s.db.Query(`SELECT ` + accountCols + ` FROM claude_accounts ORDER BY name COLLATE NOCASE`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Account{}
	for rows.Next() {
		a, err := scanAccount(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Store) Account(id int64) (Account, error) {
	return scanAccount(s.db.QueryRow(`SELECT `+accountCols+` FROM claude_accounts WHERE id = ?`, id))
}

// AddAccount salveaza un cont nou. Numele e unic (fara diferenta intre litere mari si mici).
// Validarea (nume, format token) e in web.
func (s *Store) AddAccount(name, token string) (Account, error) {
	res, err := s.db.Exec(`INSERT INTO claude_accounts (name, token, created_at) VALUES (?, ?, ?)`, name, token, now())
	if isUnique(err) {
		return Account{}, ErrConflict
	}
	if err != nil {
		return Account{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Account{}, err
	}
	return s.Account(id)
}

// UpdateAccount schimba numele si/sau token-ul (gol = neschimbat). Un token nou sterge
// rezultatul verificarii vechi: nu mai spune nimic despre el.
func (s *Store) UpdateAccount(id int64, name, token string) error {
	if _, err := s.Account(id); err != nil {
		return err
	}
	if name != "" {
		_, err := s.db.Exec(`UPDATE claude_accounts SET name = ? WHERE id = ?`, name, id)
		if isUnique(err) {
			return ErrConflict
		}
		if err != nil {
			return err
		}
	}
	if token != "" {
		if _, err := s.db.Exec(`UPDATE claude_accounts SET token = ?, checked_at = '', last_error = '' WHERE id = ?`, token, id); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) DeleteAccount(id int64) error {
	a, err := s.Account(id)
	if err != nil {
		return err
	}
	if a.Projects > 0 {
		return ErrAccountInUse
	}
	return mustAffect(s.db.Exec(`DELETE FROM claude_accounts WHERE id = ?`, id))
}

// SetAccountCheck salveaza rezultatul unei verificari (Check sau run cazut la autentificare).
// errText gol = reusit: checked_at devine acum si eroarea se sterge.
func (s *Store) SetAccountCheck(id int64, errText string) error {
	if errText == "" {
		return mustAffect(s.db.Exec(`UPDATE claude_accounts SET checked_at = ?, last_error = '' WHERE id = ?`, now(), id))
	}
	return mustAffect(s.db.Exec(`UPDATE claude_accounts SET last_error = ? WHERE id = ?`, errText, id))
}

// ProjectAccount: contul pe care ruleaza proiectul; nil = Default (CLI-ul logat).
func (s *Store) ProjectAccount(projectID int64) (*Account, error) {
	var id sql.NullInt64
	if err := s.db.QueryRow(`SELECT account_id FROM projects WHERE id = ?`, projectID).Scan(&id); err != nil {
		return nil, notFound(err)
	}
	if !id.Valid {
		return nil, nil
	}
	a, err := s.Account(id.Int64)
	if err != nil {
		return nil, err
	}
	return &a, nil
}

// AccountProjects: id-urile proiectelor care folosesc contul.
func (s *Store) AccountProjects(id int64) ([]int64, error) {
	rows, err := s.db.Query(`SELECT id FROM projects WHERE account_id = ? ORDER BY id`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var pid int64
		if err := rows.Scan(&pid); err != nil {
			return nil, err
		}
		out = append(out, pid)
	}
	return out, rows.Err()
}

// SetProjectAccount muta proiectul pe alt cont (nil = Default). Fiecare agent incepe un chat nou
// (ca "New chat", cu nota data in thread), ca o conversatie sa nu treaca dintr-un cont in altul
// prin --resume. Acelasi cont: nu se schimba nimic (changed = false).
func (s *Store) SetProjectAccount(projectID int64, accountID *int64, note string) (bool, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var cur sql.NullInt64
	if err := tx.QueryRow(`SELECT account_id FROM projects WHERE id = ?`, projectID).Scan(&cur); err != nil {
		return false, notFound(err)
	}
	if (accountID == nil && !cur.Valid) || (accountID != nil && cur.Valid && cur.Int64 == *accountID) {
		return false, nil
	}
	if _, err := tx.Exec(`UPDATE projects SET account_id = ? WHERE id = ?`, nullPtr(accountID), projectID); err != nil {
		return false, err
	}
	// sesiunile de brainstorming ale proiectului pleaca si ele (alt cont, alta conversatie)
	if _, err := tx.Exec(`DELETE FROM brainstorm_sessions WHERE topic_id IN (SELECT id FROM brainstorm_topics WHERE project_id = ?)`, projectID); err != nil {
		return false, err
	}
	rows, err := tx.Query(`SELECT id FROM agents WHERE project_id = ? ORDER BY id`, projectID)
	if err != nil {
		return false, err
	}
	var agents []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return false, err
		}
		agents = append(agents, id)
	}
	rows.Close() // o singura conexiune: inchidem cursorul inainte de alte comenzi
	if err := rows.Err(); err != nil {
		return false, err
	}
	for _, id := range agents {
		if _, _, err := newChatTx(tx, projectID, id, note); err != nil {
			return false, err
		}
	}
	return true, tx.Commit()
}
