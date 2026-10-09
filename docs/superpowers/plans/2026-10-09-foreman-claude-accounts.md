# Cont Claude per proiect: plan de implementare

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Un proiect poate rula pe alt cont Claude (abonament, `CLAUDE_CODE_OAUTH_TOKEN`) decat CLI-ul logat; conturile se gestioneaza in Settings, se aleg pe proiect si in wizard, iar o eroare de autentificare pune proiectul pe pauza si apare in "Needs you".

**Architecture:** Tabel nou `claude_accounts` + `projects.account_id` (store). `runner.Spec.Env` pune token-ul in mediul procesului (si scoate variabilele de cont mostenite); `runner.Manager.Check` / `AuthStatus` ruleaza `claude` direct pentru verificare. `team.startRun` citeste contul proiectului; `team.authFailed` trateaza run-urile cazute la autentificare. Web: `/api/accounts*`, `accountId` pe proiect si in wizard; UI: sectiune in Settings, select in formularul de proiect si in wizard, badge, card in "Needs you".

**Tech Stack:** Go (stdlib), SQLite (modernc.org/sqlite), JS vanilla (module ES).

**Spec:** `docs/superpowers/specs/2026-10-09-foreman-claude-accounts-design.md`

## Global Constraints

- Tot ce vede utilizatorul si textele pentru agenti sunt in engleza; comentariile din cod in romana.
- `go vet ./...`, `go build -race ./...` si `go test ./...` trec.
- JS: copiaza fisierul ca `.mjs` si ruleaza `node --check` (pe `.js` nu prinde erorile).
- Cross-compile: `GOOS=darwin GOARCH=arm64 go build ./cmd/foreman` si `GOOS=windows go build ./cmd/foreman`.
- Fara dependente noi.
- **Migrarile se numara dupa pozitie, nu dupa numele fisierului** (`store.migrate`: `user_version` = indexul in lista).
  Pe `main` ultima e `024_task_files.sql`, deci fisierul nou e `025_claude_accounts.sql`. Branch-ul `task-move-delete`
  are si el o migrare 025 planificata: **cine ajunge al doilea pe main isi redenumeste migrarea in 026** (inainte de
  merge), altfel o baza de date deja migrata sare peste una din ele. Nu exista inca utilizatori cu 025 aplicat.
- Token-ul nu apare niciodata intr-un raspuns API, in log sau in linia de comanda (doar in mediul procesului `claude`).
- Fail closed: un proiect cu cont nu ruleaza niciodata pe contul implicit.
- Commit-urile adauga DOAR fisierele task-ului (`git add <cai>`).

## Abateri fata de spec (decise la scrierea planului)

- **Mediul mostenit:** spec-ul scotea mereu `CLAUDE_CODE_OAUTH_TOKEN` / `ANTHROPIC_API_KEY`. Planul le scoate **doar pentru
  proiectele cu cont** (plus `ANTHROPIC_AUTH_TOKEN`, `ANTHROPIC_BASE_URL`, `CLAUDE_CODE_USE_BEDROCK`, `CLAUDE_CODE_USE_VERTEX`,
  care altfel ar castiga in fata token-ului sau l-ar trimite altundeva). Pe Default mediul ramane neatins: "Default" inseamna
  exact ce ar face `claude` pornit din acelasi shell (inclusiv o cheie API pusa intentionat).
- `runner.IsAuthError(msg string)` primeste textul erorii (Outcome.Error contine deja `result` cand `is_error`), cu
  markerii `authentication_error`, `invalid api key`, `oauth token`, `not logged in`, `/login`, `api error: 401`
  (fara "401" simplu: ar prinde si "T-401").
- Ramura "contul nu are token" dispare: `AddAccount`/`UpdateAccount` nu accepta token gol, iar FK-ul impiedica stergerea
  unui cont folosit. Orice eroare la citirea contului face run-ul sa nu porneasca (`startFailed`), deci tot fail closed.
- `RenameAccount` + `UpdateAccountToken` devin un singur `UpdateAccount(id, name, token)` (gol = neschimbat).
- Contul implicit are ruta lui, `GET /api/accounts/default` (ruleaza `claude auth status`, ~0.5 s), ca lista sa ramana rapida.

## Review Focus

1. **Token nou pus peste unul expirat** (Replace token): `last_error` si `checked_at` se golesc, Check-ul ruleaza din nou, iar
   cardul din "Needs you" dispare pe toate proiectele contului. Test in Task 1 (store) si Task 4 (API, evenimentul pe proiect).
2. **Shell-ul care a pornit foreman are `ANTHROPIC_API_KEY`**: proiectul cu cont nu o vede (altfel cheia castiga si munca
   ajunge pe contul gresit); proiectul pe Default o vede. Test in Task 2.
3. **Schimbarea contului cat echipa lucreaza**: 409 "Stop the team first", nimic salvat (nici celelalte campuri). Test in Task 4.
4. **Proiect din wizard cu cont ales**: analiza ruleaza pe cont, iar proiectul creat il pastreaza (inclusiv cand pagina
   wizard-ului a fost redeschisa din card). Test in Task 5.
5. **Run cazut la autentificare in timp ce lucra la un task**: task-ul revine in Queued (nu in Review cu "run failed"),
   proiectul e pe pauza, nota in thread-ul agentului. Test in Task 3.

---

### Task 1: Migrarea 025 + store (conturi, contul proiectului)

**Files:**
- Create: `internal/store/migrations/025_claude_accounts.sql`
- Create: `internal/store/accounts.go`
- Create: `internal/store/accounts_test.go`
- Modify: `internal/store/projects.go` (struct `Project`, `projectCols`, `scanProject`, cele doua INSERT-uri)
- Modify: `internal/store/chat.go` (`StartNewChat` foloseste `newChatTx`)

**Interfaces:**
- Produces:
  - `type Account struct { ID int64; Name string; TokenHint string; CreatedAt time.Time; CheckedAt *time.Time; LastError string; Projects int }` (+ `token` privat)
  - `func (a Account) Env() []string` → `["CLAUDE_CODE_OAUTH_TOKEN=<token>"]`
  - `var ErrAccountInUse error`
  - `func (s *Store) Accounts() ([]Account, error)`
  - `func (s *Store) Account(id int64) (Account, error)` (`ErrNotFound`)
  - `func (s *Store) AddAccount(name, token string) (Account, error)` (`ErrConflict` la nume dublat)
  - `func (s *Store) UpdateAccount(id int64, name, token string) error` (gol = neschimbat)
  - `func (s *Store) DeleteAccount(id int64) error` (`ErrAccountInUse`)
  - `func (s *Store) SetAccountCheck(id int64, errText string) error` (gol = reusit)
  - `func (s *Store) ProjectAccount(projectID int64) (*Account, error)` (nil = Default)
  - `func (s *Store) AccountProjects(id int64) ([]int64, error)`
  - `func (s *Store) SetProjectAccount(projectID int64, accountID *int64, note string) (changed bool, err error)`
  - `Project.AccountID *int64` (json `accountId`)

- [ ] **Step 1: Scrie testele**

`internal/store/accounts_test.go`:

```go
package store

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestAccountsCRUD(t *testing.T) {
	s := newStore(t)
	a, err := s.AddAccount("Work", "sk-ant-oat01-secret-ABCD")
	if err != nil {
		t.Fatal(err)
	}
	if a.ID == 0 || a.Name != "Work" || a.TokenHint != "ABCD" || a.CheckedAt != nil || a.LastError != "" {
		t.Fatalf("cont nou: %+v", a)
	}
	raw, _ := json.Marshal(a)
	if strings.Contains(string(raw), "secret") {
		t.Fatalf("token-ul a ajuns in JSON: %s", raw)
	}
	if env := a.Env(); len(env) != 1 || env[0] != "CLAUDE_CODE_OAUTH_TOKEN=sk-ant-oat01-secret-ABCD" {
		t.Fatalf("Env = %v", env)
	}
	if _, err := s.AddAccount("work", "alt-token-1234"); !errors.Is(err, ErrConflict) {
		t.Fatalf("nume dublat (alt caz): %v", err)
	}

	if err := s.SetAccountCheck(a.ID, "OAuth token has expired"); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Account(a.ID); got.LastError != "OAuth token has expired" || got.CheckedAt != nil {
		t.Fatalf("dupa eroare: %+v", got)
	}
	if err := s.SetAccountCheck(a.ID, ""); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Account(a.ID); got.LastError != "" || got.CheckedAt == nil {
		t.Fatalf("dupa verificare reusita: %+v", got)
	}

	// token nou: verificarea veche nu mai spune nimic despre el
	if err := s.SetAccountCheck(a.ID, "expired"); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateAccount(a.ID, "Acme", "sk-ant-oat01-new-WXYZ"); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Account(a.ID)
	if got.Name != "Acme" || got.TokenHint != "WXYZ" || got.LastError != "" || got.CheckedAt != nil {
		t.Fatalf("dupa UpdateAccount: %+v", got)
	}
	// doar numele: token-ul si verificarea raman
	s.SetAccountCheck(a.ID, "")
	if err := s.UpdateAccount(a.ID, "Acme Team", ""); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Account(a.ID); got.TokenHint != "WXYZ" || got.CheckedAt == nil {
		t.Fatalf("redenumirea a atins token-ul: %+v", got)
	}
	if err := s.UpdateAccount(999, "x", ""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cont inexistent: %v", err)
	}

	list, err := s.Accounts()
	if err != nil || len(list) != 1 || list[0].ID != a.ID {
		t.Fatalf("Accounts = %+v, %v", list, err)
	}
}

func TestDeleteAccountInUse(t *testing.T) {
	s := newStore(t)
	p, _ := newProject(t, s)
	a, _ := s.AddAccount("Work", "tok-1234567890")
	if _, err := s.SetProjectAccount(p.ID, &a.ID, "switched"); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Account(a.ID); got.Projects != 1 {
		t.Fatalf("Projects = %d", got.Projects)
	}
	if err := s.DeleteAccount(a.ID); !errors.Is(err, ErrAccountInUse) {
		t.Fatalf("stergere cont folosit: %v", err)
	}
	if _, err := s.SetProjectAccount(p.ID, nil, "back"); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteAccount(a.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Account(a.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("contul trebuia sters: %v", err)
	}
}

func TestSetProjectAccountResetsSessions(t *testing.T) {
	s := newStore(t)
	p, lead := newProject(t, s)
	worker := newWorker(t, s, p.ID, "Rook")
	for _, ag := range []Agent{lead, worker} {
		if err := s.SetSession(ag.ID, "sess-old", "hash"); err != nil {
			t.Fatal(err)
		}
	}
	a, _ := s.AddAccount("Work", "tok-1234567890")

	changed, err := s.SetProjectAccount(p.ID, &a.ID, "Claude account changed to Work; starting a new session.")
	if err != nil || !changed {
		t.Fatalf("changed=%v err=%v", changed, err)
	}
	for _, ag := range []Agent{lead, worker} {
		got, _ := s.GetAgent(ag.ID)
		if got.SessionID != "" || got.PromptHash != "" {
			t.Fatalf("%s pastreaza sesiunea: %+v", ag.Name, got)
		}
		msgs, earlier, _ := s.ListChat(ag.ID)
		if len(msgs) != 1 || !strings.Contains(msgs[0].Text, "changed to Work") || earlier != 0 {
			t.Fatalf("chat %s: %+v (earlier %d)", ag.Name, msgs, earlier)
		}
	}
	got, err := s.GetProject(p.ID)
	if err != nil || got.AccountID == nil || *got.AccountID != a.ID {
		t.Fatalf("AccountID = %v, %v", got.AccountID, err)
	}
	acct, err := s.ProjectAccount(p.ID)
	if err != nil || acct == nil || acct.ID != a.ID {
		t.Fatalf("ProjectAccount = %+v, %v", acct, err)
	}
	if ids, _ := s.AccountProjects(a.ID); len(ids) != 1 || ids[0] != p.ID {
		t.Fatalf("AccountProjects = %v", ids)
	}

	// acelasi cont: nimic nu se schimba, nici sesiunile
	s.SetSession(lead.ID, "sess-new", "hash")
	if changed, err := s.SetProjectAccount(p.ID, &a.ID, "x"); err != nil || changed {
		t.Fatalf("acelasi cont: changed=%v err=%v", changed, err)
	}
	if got, _ := s.GetAgent(lead.ID); got.SessionID != "sess-new" {
		t.Fatal("acelasi cont a sters sesiunea")
	}

	// inapoi pe Default
	if changed, err := s.SetProjectAccount(p.ID, nil, "Claude account changed to Default; starting a new session."); err != nil || !changed {
		t.Fatalf("Default: changed=%v err=%v", changed, err)
	}
	if acct, err := s.ProjectAccount(p.ID); err != nil || acct != nil {
		t.Fatalf("Default: %+v, %v", acct, err)
	}
	if _, err := s.SetProjectAccount(999, nil, "x"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("proiect inexistent: %v", err)
	}
}

func TestCreateProjectWithAccount(t *testing.T) {
	s := newStore(t)
	a, _ := s.AddAccount("Work", "tok-1234567890")
	p := Project{Name: "Acme", Workdir: t.TempDir(), AccountID: &a.ID}
	if _, err := s.CreateProject(&p); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetProject(p.ID)
	if got.AccountID == nil || *got.AccountID != a.ID {
		t.Fatalf("CreateProject: %v", got.AccountID)
	}
	q := Project{Name: "Team", Workdir: t.TempDir(), AccountID: &a.ID}
	if _, err := s.CreateProjectWithTeam(&q, []NewMember{{Agent: Agent{Name: "Ana", Role: "Lead", Model: "sonnet"}, Parent: -1, Reviewer: -1}}); err != nil {
		t.Fatal(err)
	}
	got, _ = s.GetProject(q.ID)
	if got.AccountID == nil || *got.AccountID != a.ID {
		t.Fatalf("CreateProjectWithTeam: %v", got.AccountID)
	}
	// UpdateProject nu atinge contul (se schimba doar prin SetProjectAccount)
	got.Name = "Team 2"
	got.AccountID = nil
	if err := s.UpdateProject(got); err != nil {
		t.Fatal(err)
	}
	if again, _ := s.GetProject(q.ID); again.AccountID == nil {
		t.Fatal("UpdateProject a sters contul")
	}
}
```

- [ ] **Step 2: Ruleaza testele, trebuie sa pice**

Run: `go test ./internal/store/ -run 'Account|ProjectAccount' -count=1`
Expected: FAIL la compilare (`undefined: AddAccount`, `unknown field AccountID`).

- [ ] **Step 3: Migrarea**

`internal/store/migrations/025_claude_accounts.sql`:

```sql
-- Conturi Claude (abonament, CLAUDE_CODE_OAUTH_TOKEN din `claude setup-token`) alese per proiect.
-- projects.account_id NULL = Default: CLI-ul logat pe masina.
CREATE TABLE claude_accounts (
  id          INTEGER PRIMARY KEY,
  name        TEXT NOT NULL UNIQUE COLLATE NOCASE,
  token       TEXT NOT NULL,
  created_at  TEXT NOT NULL,
  checked_at  TEXT NOT NULL DEFAULT '',  -- ultima verificare reusita; '' = niciodata
  last_error  TEXT NOT NULL DEFAULT ''   -- ultima eroare de autentificare; '' = merge
);
ALTER TABLE projects ADD COLUMN account_id INTEGER REFERENCES claude_accounts(id);
```

- [ ] **Step 4: `internal/store/accounts.go`**

```go
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
```

- [ ] **Step 5: `newChatTx` in `internal/store/chat.go`**

Inlocuieste corpul lui `StartNewChat` dintre `if err := tx.QueryRow(...project_id...)` si `tx.Commit()` cu apelul helper-ului,
si adauga helper-ul sub functie:

```go
func (s *Store) StartNewChat(agentID int64, text string) (Message, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return Message{}, err
	}
	defer tx.Rollback()

	var projectID int64
	if err := tx.QueryRow(`SELECT project_id FROM agents WHERE id = ?`, agentID).Scan(&projectID); err != nil {
		return Message{}, notFound(err)
	}
	id, ts, err := newChatTx(tx, projectID, agentID, text)
	if err != nil {
		return Message{}, err
	}
	if err := tx.Commit(); err != nil {
		return Message{}, err
	}
	return Message{ID: id, ProjectID: projectID, AgentID: agentID, Kind: KindEvent, Text: text,
		Delivered: true, CreatedAt: parseTime(ts), Attachments: []Attachment{}}, nil
}

// newChatTx scrie nota in thread-ul agentului, ii sterge sesiunea Claude si muta inceputul
// chat-ului la nota (in tranzactia data). Intoarce id-ul notei si momentul ei.
func newChatTx(tx *sql.Tx, projectID, agentID int64, text string) (int64, string, error) {
	ts := now()
	res, err := tx.Exec(`INSERT INTO messages (project_id, agent_id, kind, author, text, run_id, delivered, created_at)
		VALUES (?, ?, ?, '', ?, NULL, 1, ?)`, projectID, agentID, KindEvent, text, ts)
	if err != nil {
		return 0, "", err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, "", err
	}
	if _, err := tx.Exec(`UPDATE agents SET session_id = '', prompt_hash = '', chat_from = ? WHERE id = ?`, id, agentID); err != nil {
		return 0, "", err
	}
	return id, ts, nil
}
```

- [ ] **Step 6: `Project.AccountID` in `internal/store/projects.go`**

In struct, dupa `Favorite`:

```go
	AccountID   *int64    `json:"accountId"` // contul Claude; nil = Default (CLI-ul logat). Se schimba doar prin SetProjectAccount
```

`projectCols` si `scanProject`:

```go
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
```

In `CreateProject` si `CreateProjectWithTeam`, ambele INSERT-uri devin:

```go
	res, err := tx.Exec(`INSERT INTO projects (name, goal, context, workdir, paused, max_parallel, budget_usd, autonomy, created_at, account_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.Name, p.Goal, p.Context, p.Workdir, p.Paused, p.MaxParallel, p.BudgetUSD, p.Autonomy, created, nullPtr(p.AccountID))
```

`UpdateProject` ramane neschimbat (nu scrie `account_id`).

- [ ] **Step 7: Ruleaza testele store**

Run: `go test ./internal/store/ -count=1`
Expected: PASS (inclusiv `TestOpenAppliesMigrationsOnce` si testele vechi de chat).

- [ ] **Step 8: Commit**

```bash
git add internal/store/migrations/025_claude_accounts.sql internal/store/accounts.go internal/store/accounts_test.go internal/store/projects.go internal/store/chat.go
git commit -m "store: conturi Claude (claude_accounts) si contul proiectului"
```

---

### Task 2: runner: mediul procesului, Check, AuthStatus, IsAuthError

**Files:**
- Create: `internal/runner/env.go`
- Create: `internal/runner/check.go`
- Create: `internal/runner/env_test.go` (fara build tag: teste pure)
- Create: `internal/runner/check_test.go` (`//go:build !windows`, foloseste `testdata/fakeclaude.sh`)
- Modify: `internal/runner/runner.go` (`Spec.Env`, `cmd.Env` in `Start`)
- Modify: `internal/runner/testdata/fakeclaude.sh` (modurile `auth` si `env`)

**Interfaces:**
- Produces:
  - `Spec.Env []string`: perechi `K=V`; nevid = variabilele de cont mostenite sunt scoase si inlocuite cu acestea
  - `func IsAuthError(msg string) bool`
  - `func (m *Manager) Check(ctx context.Context, env []string) error`
  - `type AuthStatus struct { LoggedIn bool; AuthMethod, Email, SubscriptionType string }` (json `loggedIn`, `authMethod`, `email`, `subscriptionType`)
  - `func (m *Manager) AuthStatus(ctx context.Context) (AuthStatus, error)`

- [ ] **Step 1: Extinde `fakeclaude.sh`**

Pune la inceputul fisierului, dupa comentariu (inainte de `case`):

```sh
# `claude auth status --json`: login-ul fals al masinii
if [ "$1" = "auth" ]; then
  echo '{"loggedIn":true,"authMethod":"claude.ai","email":"me@example.com","subscriptionType":"max"}'
  exit 0
fi
```

si in `case`, inainte de `*)`:

```sh
env)
  # arata ce cont ar folosi procesul (testele pentru Spec.Env)
  echo "{\"type\":\"result\",\"subtype\":\"success\",\"is_error\":false,\"result\":\"token=$CLAUDE_CODE_OAUTH_TOKEN key=$ANTHROPIC_API_KEY\",\"total_cost_usd\":0,\"session_id\":\"s\"}"
  ;;
```

- [ ] **Step 2: Scrie testele**

`internal/runner/env_test.go`:

```go
package runner

import (
	"slices"
	"testing"
)

func TestCleanEnv(t *testing.T) {
	in := []string{"PATH=/bin", "ANTHROPIC_API_KEY=k", "CLAUDE_CODE_OAUTH_TOKEN=t", "ANTHROPIC_BASE_URL=http://gw",
		"ANTHROPIC_AUTH_TOKEN=a", "CLAUDE_CODE_USE_BEDROCK=1", "CLAUDE_CODE_USE_VERTEX=1", "HOME=/h", "ANTHROPIC_MODEL=x"}
	got := cleanEnv(in)
	want := []string{"PATH=/bin", "HOME=/h", "ANTHROPIC_MODEL=x"}
	if !slices.Equal(got, want) {
		t.Fatalf("cleanEnv = %v", got)
	}
}

func TestProcEnv(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "inherited")
	if procEnv(nil) != nil {
		t.Fatal("fara env propriu procesul mosteneste tot (nil)")
	}
	env := procEnv([]string{"CLAUDE_CODE_OAUTH_TOKEN=work"})
	if slices.Contains(env, "ANTHROPIC_API_KEY=inherited") || !slices.Contains(env, "CLAUDE_CODE_OAUTH_TOKEN=work") {
		t.Fatalf("procEnv = %v", env)
	}
}

func TestIsAuthError(t *testing.T) {
	yes := []string{
		`API Error: 401 {"type":"error","error":{"type":"authentication_error","message":"OAuth token has expired."}}`,
		"Not logged in · Please run /login",
		"Invalid API key · Fix external API key",
		"OAuth token revoked",
	}
	no := []string{"", "Reached maximum budget ($1)", "the folder Client does not exist", "fix T-401 first", "claude exited without a result"}
	for _, m := range yes {
		if !IsAuthError(m) {
			t.Errorf("trebuia recunoscut: %q", m)
		}
	}
	for _, m := range no {
		if IsAuthError(m) {
			t.Errorf("nu e eroare de autentificare: %q", m)
		}
	}
}
```

`internal/runner/check_test.go`:

```go
//go:build !windows

package runner

import (
	"context"
	"strings"
	"testing"
)

func TestSpecEnvReplacesInheritedAuth(t *testing.T) {
	t.Setenv("FAKE_MODE", "env")
	t.Setenv("ANTHROPIC_API_KEY", "inherited")
	t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "") // shell-ul dezvoltatorului poate avea unul
	m := fakeManager(t)

	_, done := start(t, m, Spec{ID: "e1", Key: "agent-1", Dir: t.TempDir(), Env: []string{"CLAUDE_CODE_OAUTH_TOKEN=work-token"}})
	if o := wait(t, done); o.Result != "token=work-token key=" {
		t.Fatalf("cu cont: %q", o.Result)
	}
	_, done = start(t, m, Spec{ID: "e2", Key: "agent-1", Dir: t.TempDir()})
	if o := wait(t, done); o.Result != "token= key=inherited" {
		t.Fatalf("Default trebuie sa mosteneasca mediul: %q", o.Result)
	}
}

func TestCheck(t *testing.T) {
	m := fakeManager(t)
	t.Setenv("FAKE_MODE", "")
	if err := m.Check(context.Background(), []string{"CLAUDE_CODE_OAUTH_TOKEN=x"}); err != nil {
		t.Fatalf("verificare reusita: %v", err)
	}
	t.Setenv("FAKE_MODE", "notlogged")
	if err := m.Check(context.Background(), []string{"CLAUDE_CODE_OAUTH_TOKEN=x"}); err == nil || !strings.Contains(err.Error(), "Not logged in") {
		t.Fatalf("token respins: %v", err)
	}
	t.Setenv("FAKE_MODE", "crash")
	if err := m.Check(context.Background(), nil); err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("crash: %v", err)
	}
	missing := NewManager("/nu/exista/claude", t.TempDir())
	if err := missing.Check(context.Background(), nil); err == nil || !strings.Contains(err.Error(), "cannot run claude") {
		t.Fatalf("binar lipsa: %v", err)
	}
}

func TestAuthStatus(t *testing.T) {
	st, err := fakeManager(t).AuthStatus(context.Background())
	if err != nil || !st.LoggedIn || st.Email != "me@example.com" || st.SubscriptionType != "max" {
		t.Fatalf("AuthStatus = %+v, %v", st, err)
	}
}
```

- [ ] **Step 3: Ruleaza testele, trebuie sa pice**

Run: `go test ./internal/runner/ -count=1`
Expected: FAIL la compilare (`undefined: cleanEnv`, `unknown field Env`).

- [ ] **Step 4: `internal/runner/env.go`**

```go
package runner

import (
	"os"
	"slices"
	"strings"
)

// authVars aleg contul (sau furnizorul) cu care ruleaza claude. Un run cu cont propriu
// (Spec.Env) nu le mosteneste din shell-ul care a pornit foreman: o cheie API sau un
// gateway mostenit ar castiga in fata token-ului contului, iar munca ar ajunge pe alt cont.
var authVars = []string{"ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN", "ANTHROPIC_BASE_URL",
	"CLAUDE_CODE_OAUTH_TOKEN", "CLAUDE_CODE_USE_BEDROCK", "CLAUDE_CODE_USE_VERTEX"}

// cleanEnv scoate authVars din mediu (pe Windows numele nu tin cont de litere mari/mici).
func cleanEnv(env []string) []string {
	out := make([]string, 0, len(env))
	for _, kv := range env {
		k, _, _ := strings.Cut(kv, "=")
		if slices.ContainsFunc(authVars, func(v string) bool { return strings.EqualFold(v, k) }) {
			continue
		}
		out = append(out, kv)
	}
	return out
}

// procEnv: mediul procesului claude. Fara env propriu (Default) mosteneste tot (nil);
// cu env propriu scoate variabilele de cont mostenite si le adauga pe cele date.
func procEnv(extra []string) []string {
	if len(extra) == 0 {
		return nil
	}
	return append(cleanEnv(os.Environ()), extra...)
}

// authMarkers: fragmente din erorile de autentificare ale lui claude (token expirat sau
// revocat, delogat, cheie invalida). Comparate fara diferenta intre litere mari/mici.
var authMarkers = []string{"authentication_error", "invalid api key", "oauth token", "not logged in", "/login", "api error: 401"}

// IsAuthError spune daca eroarea unui run vine de la autentificare.
func IsAuthError(msg string) bool {
	m := strings.ToLower(msg)
	for _, s := range authMarkers {
		if strings.Contains(m, s) {
			return true
		}
	}
	return false
}
```

- [ ] **Step 5: `Spec.Env` si `cmd.Env` in `internal/runner/runner.go`**

In `Spec`, dupa `Prompt`:

```go
	Env    []string // K=V adaugate in mediu; nevid = variabilele de cont mostenite sunt scoase (vezi procEnv)
```

In `Start`, dupa `cmd.Dir = spec.Dir`:

```go
	cmd.Env = procEnv(spec.Env)
```

- [ ] **Step 6: `internal/runner/check.go`**

```go
package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Check trimite un mesaj scurt cu contul dat (env) si spune daca autentificarea merge.
// Ruleaza intr-un folder temporar gol, fara tool-uri (o singura tura), direct, nu prin
// Start: fara log si fara SSE. Costa cat un mesaj scurt cu haiku.
func (m *Manager) Check(ctx context.Context, env []string) error {
	dir, err := os.MkdirTemp("", "foreman-check-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	cmd := exec.CommandContext(ctx, m.claudeBin, "-p", "Reply with OK", "--model", "haiku",
		"--output-format", "json", "--setting-sources", "user", "--tools", "")
	cmd.Dir = dir
	cmd.Env = procEnv(env)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, runErr := cmd.Output()
	if res, ok := lastResult(out); ok {
		if !res.IsError {
			return nil
		}
		for _, msg := range []string{strings.TrimSpace(res.Result), strings.Join(res.Errors, "; "), res.Subtype} {
			if msg != "" {
				return errors.New(msg)
			}
		}
		return errors.New("the check failed")
	}
	if ctx.Err() != nil {
		return errors.New("claude did not answer in time")
	}
	if msg := strings.TrimSpace(stderr.String()); msg != "" {
		return errors.New(msg)
	}
	if runErr != nil {
		return fmt.Errorf("cannot run claude: %w", runErr)
	}
	return errors.New("claude exited without a result")
}

type checkResult struct {
	Type    string   `json:"type"`
	Subtype string   `json:"subtype"`
	IsError bool     `json:"is_error"`
	Result  string   `json:"result"`
	Errors  []string `json:"errors"`
}

// lastResult cauta ultimul eveniment "result" din output (json sau stream-json).
func lastResult(out []byte) (checkResult, bool) {
	var found checkResult
	ok := false
	for _, line := range bytes.Split(out, []byte("\n")) {
		var r checkResult
		if json.Unmarshal(bytes.TrimSpace(line), &r) == nil && r.Type == "result" {
			found, ok = r, true
		}
	}
	return found, ok
}

// AuthStatus: contul implicit, din `claude auth status --json` (mediul mostenit, ca la Default).
type AuthStatus struct {
	LoggedIn         bool   `json:"loggedIn"`
	AuthMethod       string `json:"authMethod"`
	Email            string `json:"email"`
	SubscriptionType string `json:"subscriptionType"`
}

func (m *Manager) AuthStatus(ctx context.Context) (AuthStatus, error) {
	out, err := exec.CommandContext(ctx, m.claudeBin, "auth", "status", "--json").Output()
	var st AuthStatus
	// delogat: claude poate iesi cu cod nenul, dar tot scrie JSON-ul
	if jerr := json.Unmarshal(out, &st); jerr != nil {
		if err != nil {
			return AuthStatus{}, err
		}
		return AuthStatus{}, jerr
	}
	return st, nil
}
```

Nota: in `budget` / `notlogged` fakeclaude scrie `"errors":["..."]` sau `"result":null`; `Errors []string` si `Result string`
le accepta pe amandoua (null lasa campul gol).

- [ ] **Step 7: Ruleaza testele runner**

Run: `go test ./internal/runner/ -count=1 -race`
Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/runner/env.go internal/runner/check.go internal/runner/env_test.go internal/runner/check_test.go internal/runner/runner.go internal/runner/testdata/fakeclaude.sh
git commit -m "runner: env per run (cont Claude), Check, AuthStatus, IsAuthError"
```

---

### Task 3: team: run-urile pe contul proiectului, erori de autentificare

**Files:**
- Create: `internal/team/account.go`
- Create: `internal/team/account_test.go`
- Modify: `internal/team/team.go` (`activeRun.accountID`)
- Modify: `internal/team/run.go` (`startRun`, `recordOutcome`)

**Interfaces:**
- Consumes: `store.ProjectAccount`, `store.Account.Env`, `store.SetAccountCheck`, `store.SetProjectAccount` (teste), `runner.IsAuthError`, `runner.Spec.Env`
- Produces: `func (t *Team) authFailed(ar activeRun, a store.Agent, msg string) error` (intern)

- [ ] **Step 1: Scrie testele**

`internal/team/account_test.go`:

```go
package team

import (
	"slices"
	"strings"
	"testing"

	"foreman/internal/runner"
	"foreman/internal/store"
)

// useAccount muta proiectul fixture-ului pe un cont nou.
func useAccount(t *testing.T, f *fixture) store.Account {
	t.Helper()
	a, err := f.st.AddAccount("Work", "sk-ant-oat01-work-1234")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.st.SetProjectAccount(f.p.ID, &a.ID, "switched"); err != nil {
		t.Fatal(err)
	}
	return a
}

func TestRunUsesProjectAccount(t *testing.T) {
	f := setup(t)
	f.task(t, "A", f.rook, store.StatusQueued)
	mustReconcile(t, f)
	if env := f.fs.last(t, f.rook).Env; env != nil {
		t.Fatalf("Default: Env = %v", env)
	}

	g := setup(t)
	useAccount(t, g)
	g.task(t, "A", g.rook, store.StatusQueued)
	mustReconcile(t, g)
	env := g.fs.last(t, g.rook).Env
	if !slices.Equal(env, []string{"CLAUDE_CODE_OAUTH_TOKEN=sk-ant-oat01-work-1234"}) {
		t.Fatalf("cu cont: Env = %v", env)
	}
	if strings.Contains(strings.Join(g.fs.last(t, g.rook).Args, " "), "sk-ant") {
		t.Fatal("token-ul a ajuns in linia de comanda")
	}
}

func TestAuthErrorPausesProject(t *testing.T) {
	f := setup(t)
	acct := useAccount(t, f)
	f.task(t, "A", f.rook, store.StatusQueued)
	mustReconcile(t, f)
	spec := f.fs.last(t, f.rook)
	msg := `API Error: 401 {"type":"error","error":{"type":"authentication_error","message":"OAuth token has expired."}}`
	f.fs.finish(spec.ID, runner.Outcome{Status: store.RunFailed, Error: msg})

	if p, _ := f.st.GetProject(f.p.ID); !p.Paused {
		t.Fatal("proiectul trebuia pus pe pauza")
	}
	if got, _ := f.st.Account(acct.ID); !strings.Contains(got.LastError, "OAuth token has expired") {
		t.Fatalf("LastError = %q", got.LastError)
	}
	f.wantStatus(t, 1, store.StatusQueued) // nu Review cu "run failed": munca nu e vina agentului
	if !f.hasMessage(f.rook.ID, "Claude account Work failed to sign in") {
		t.Fatal("lipseste nota din thread")
	}
	before := f.fs.count()
	mustReconcile(t, f) // pe pauza: nu se reincearca in bucla
	if f.fs.count() != before {
		t.Fatal("un proiect pe pauza a pornit run-uri")
	}
}

func TestAuthErrorOnDefaultPauses(t *testing.T) {
	f := setup(t)
	f.task(t, "A", f.rook, store.StatusQueued)
	mustReconcile(t, f)
	f.fs.finish(f.fs.last(t, f.rook).ID, runner.Outcome{Status: store.RunFailed, Error: "Not logged in · Please run /login"})
	if p, _ := f.st.GetProject(f.p.ID); !p.Paused {
		t.Fatal("proiectul trebuia pus pe pauza")
	}
	f.wantStatus(t, 1, store.StatusQueued)
	if !f.hasMessage(f.rook.ID, "The logged-in Claude CLI failed to sign in") {
		t.Fatal("lipseste nota din thread")
	}
}
```

- [ ] **Step 2: Ruleaza testele, trebuie sa pice**

Run: `go test ./internal/team/ -run 'Account|AuthError' -count=1`
Expected: FAIL (`Env` nil pe proiectul cu cont; proiectul nu e pe pauza).

- [ ] **Step 3: `activeRun.accountID` in `internal/team/team.go`**

In `type activeRun struct`, dupa `token string`:

```go
	accountID int64          // contul Claude al run-ului; 0 = Default
```

- [ ] **Step 4: `startRun` in `internal/team/run.go`**

Dupa verificarea folderului (`if info, err := os.Stat(home.Path)...`), adauga:

```go
	// contul Claude al proiectului; fail closed: un proiect cu cont nu ruleaza pe cel implicit
	acct, err := t.st.ProjectAccount(p.ID)
	if err != nil {
		return fmt.Errorf("Claude account: %w", err)
	}
	var env []string
	var accountID int64
	if acct != nil {
		env, accountID = acct.Env(), acct.ID
	}
```

Atentie: mai jos in functie exista `linkedProjects, err := ...` (cu `:=`) — cu `err` deja declarat acolo merge in continuare,
pentru ca `linkedProjects` e nou. Nu schimba alte declaratii.

Seteaza `accountID` pe run-ul activ (linia cu `t.active[a.ID] = activeRun{...}`):

```go
	t.active[a.ID] = activeRun{runID: runID, projectID: p.ID, taskID: taskID, token: token, accountID: accountID}
```

si `Env` in spec:

```go
	spec := runner.Spec{
		ID:     runID,
		Key:    fmt.Sprintf("agent-%d", a.ID),
		Dir:    home.Path,
		Args:   append(t.args(p, a, system, turn, mcpCfg, resume, libBuild, cmdDeny), extraArgs...),
		Prompt: turn,
		Env:    env,
	}
```

- [ ] **Step 5: `recordOutcome` in `internal/team/run.go`**

Dupa `t.recordLibraryUses(ar, a, o)` si inainte de `if o.Status == store.RunFailed {`:

```go
	if o.Status == store.RunFailed && runner.IsAuthError(o.Error) {
		return t.authFailed(ar, a, o.Error)
	}
```

- [ ] **Step 6: `internal/team/account.go`**

```go
package team

import (
	"fmt"

	"foreman/internal/store"
)

// authFailed: run-ul a cazut la autentificare (token expirat sau revocat, CLI delogat).
// Fail closed: proiectul trece pe pauza (anti-bucla, ca la startFailed), eroarea ramane pe
// cont (cardul din "Needs you"), iar task-ul revine in coada: nu e vina agentului.
// Repornirea o face utilizatorul (Start team), dupa ce rezolva contul in Settings.
func (t *Team) authFailed(ar activeRun, a store.Agent, msg string) error {
	label := "The logged-in Claude CLI"
	if ar.accountID != 0 {
		if err := t.st.SetAccountCheck(ar.accountID, msg); err != nil {
			return err
		}
		if acct, err := t.st.Account(ar.accountID); err == nil {
			label = "Claude account " + acct.Name
		}
	}
	if err := t.st.SetPaused(ar.projectID, true); err != nil {
		return err
	}
	if ar.taskID != 0 {
		task, err := t.st.GetTask(ar.taskID)
		if err != nil {
			return err
		}
		if task.Status == store.StatusInProgress && task.AssigneeID == a.ID {
			task.Status = store.StatusQueued
			if err := t.st.SaveTask(&task); err != nil {
				return err
			}
		}
	}
	t.emit(ar.projectID, "project", map[string]int64{"id": ar.projectID})
	return t.note(ar.projectID, a.ID, fmt.Sprintf("%s failed to sign in: %s. The project is paused; fix the account in Settings, then Start team.",
		label, clip(msg)), true)
}
```

`clip` (din `team.go`) taie textul la `noteMax` caractere.

- [ ] **Step 7: Ruleaza testele team**

Run: `go test ./internal/team/ -count=1 -race`
Expected: PASS (inclusiv `TestFailedWorkerRunGoesToReview`: eroarea lui nu e de autentificare).

- [ ] **Step 8: Commit**

```bash
git add internal/team/account.go internal/team/account_test.go internal/team/team.go internal/team/run.go
git commit -m "team: run-urile pe contul proiectului; eroare de autentificare = pauza + task inapoi in coada"
```

---

### Task 4: API conturi + contul pe proiect

**Files:**
- Create: `internal/web/api_accounts.go`
- Create: `internal/web/accounts_test.go`
- Create: `internal/web/accounts_check_test.go` (`//go:build !windows`)
- Modify: `internal/web/server.go` (rute; `fail` pentru `ErrAccountInUse`)
- Modify: `internal/web/api_projects.go` (`projectInput.AccountID`, `createProject`, `updateProject`, `projectCard`, `card`)

**Interfaces:**
- Consumes: Task 1 (store), Task 2 (`runs.Check`, `runs.AuthStatus`)
- Produces:
  - `GET /api/accounts` → `[]store.Account`; `GET /api/accounts/default` → `runner.AuthStatus`
  - `POST /api/accounts` `{name, token}` → 201 Account (verificat); `PUT /api/accounts/{id}` `{name?, token?}` → 200 Account
  - `DELETE /api/accounts/{id}` → 204 / 409; `POST /api/accounts/{id}/check` → 200 Account
  - proiect: `accountId` in POST/PUT (absent la PUT = neschimbat; 0 = Default); `accountName`, `accountError` pe card si detaliu
  - `func (s *Server) accountRef(id int64) (*int64, error)` si `func (s *Server) projectEnv(projectID int64) ([]string, error)` (folosite in Task 5)

- [ ] **Step 1: Scrie testele**

`internal/web/accounts_test.go`:

```go
package web

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func (e *env) addAccount(t *testing.T, name, token string) int64 {
	t.Helper()
	code, raw := e.do(t, "POST", "/api/accounts", map[string]any{"name": name, "token": token})
	if code != http.StatusCreated {
		t.Fatalf("POST /api/accounts: %d %s", code, raw)
	}
	var a struct{ ID int64 }
	json.Unmarshal(raw, &a)
	return a.ID
}

func TestAccountsAPIHidesToken(t *testing.T) {
	e := setup(t) // runner cu binar inexistent: verificarea pica, contul se salveaza oricum
	const token = "sk-ant-oat01-SECRETPART-ABCD"
	code, raw := e.do(t, "POST", "/api/accounts", map[string]any{"name": "Work", "token": token})
	if code != http.StatusCreated || strings.Contains(string(raw), "SECRETPART") {
		t.Fatalf("POST: %d %s", code, raw)
	}
	var a struct {
		ID        int64
		TokenHint string
		LastError string
	}
	json.Unmarshal(raw, &a)
	if a.TokenHint != "ABCD" || a.LastError == "" {
		t.Fatalf("cont: %+v", a)
	}
	for _, path := range []string{"/api/accounts", "/api/accounts/" + itoa(a.ID) + "/check"} {
		method := "GET"
		if strings.HasSuffix(path, "/check") {
			method = "POST"
		}
		if code, raw := e.do(t, method, path, nil); code != 200 || strings.Contains(string(raw), "SECRETPART") {
			t.Fatalf("%s %s: %d %s", method, path, code, raw)
		}
	}
	if code, raw := e.do(t, "PUT", "/api/accounts/"+itoa(a.ID), map[string]any{"name": "Acme"}); code != 200 || !strings.Contains(string(raw), `"Acme"`) {
		t.Fatalf("PUT nume: %d %s", code, raw)
	}
	if code, _ := e.do(t, "POST", "/api/accounts", map[string]any{"name": "acme", "token": token}); code != http.StatusConflict {
		t.Fatalf("nume dublat: %d", code)
	}
	for _, bad := range []map[string]any{
		{"name": "", "token": token},
		{"name": "X", "token": "short"},
		{"name": "X", "token": "sk-ant oat01 with spaces"},
		{"name": strings.Repeat("n", 41), "token": token},
	} {
		if code, _ := e.do(t, "POST", "/api/accounts", bad); code != http.StatusBadRequest {
			t.Fatalf("%v: %d", bad, code)
		}
	}
	if code, _ := e.do(t, "PUT", "/api/accounts/999", map[string]any{"name": "Y"}); code != http.StatusNotFound {
		t.Fatalf("PUT inexistent: %d", code)
	}
}

func TestProjectAccountAPI(t *testing.T) {
	e := setup(t)
	acct := e.addAccount(t, "Work", "sk-ant-oat01-token-1234")
	code, raw := e.do(t, "POST", "/api/projects", map[string]any{"name": "Acme", "workdir": t.TempDir(), "accountId": acct})
	var p struct {
		ID        int64
		AccountID *int64
	}
	json.Unmarshal(raw, &p)
	if code != http.StatusCreated || p.AccountID == nil || *p.AccountID != acct {
		t.Fatalf("POST proiect: %d %s", code, raw)
	}
	var card struct{ AccountName, AccountError string }
	_, raw = e.do(t, "GET", "/api/projects/"+itoa(p.ID), nil)
	json.Unmarshal(raw, &card)
	if card.AccountName != "Work" || card.AccountError == "" { // verificarea a picat (binar inexistent)
		t.Fatalf("detaliu: %+v", card)
	}
	if code, _ := e.do(t, "DELETE", "/api/accounts/"+itoa(acct), nil); code != http.StatusConflict {
		t.Fatalf("stergere cont folosit: %d", code)
	}

	// schimbarea contului sterge sesiunile agentilor
	lead := e.root(t, p.ID)
	e.st.SetSession(lead.ID, "sess-old", "h")
	if code, raw := e.do(t, "PUT", "/api/projects/"+itoa(p.ID), map[string]any{"name": "Acme", "accountId": 0}); code != 200 {
		t.Fatalf("PUT Default: %d %s", code, raw)
	}
	if got, _ := e.st.GetAgent(lead.ID); got.SessionID != "" {
		t.Fatal("sesiunea trebuia stearsa")
	}
	// fara accountId in corp: contul ramane
	e.do(t, "PUT", "/api/projects/"+itoa(p.ID), map[string]any{"name": "Acme", "accountId": acct})
	e.st.SetSession(lead.ID, "sess-2", "h")
	if code, _ := e.do(t, "PUT", "/api/projects/"+itoa(p.ID), map[string]any{"name": "Acme 2"}); code != 200 {
		t.Fatal("PUT fara cont")
	}
	if got, _ := e.st.GetProject(p.ID); got.AccountID == nil || *got.AccountID != acct {
		t.Fatalf("PUT fara accountId a schimbat contul: %v", got.AccountID)
	}
	if got, _ := e.st.GetAgent(lead.ID); got.SessionID != "sess-2" {
		t.Fatal("PUT fara schimbare de cont a sters sesiunea")
	}
	if code, _ := e.do(t, "PUT", "/api/projects/"+itoa(p.ID), map[string]any{"name": "Acme", "accountId": 999}); code != http.StatusBadRequest {
		t.Fatalf("cont necunoscut: %d", code)
	}
}

func TestProjectAccountChange409WhenActive(t *testing.T) {
	e := setup(t)
	acct := e.addAccount(t, "Work", "sk-ant-oat01-token-1234")
	p := e.createProject(t, t.TempDir())
	lead := e.root(t, p.ID)
	e.tm.UserMessage(lead.ID, "porneste", nil)
	if err := e.tm.Reconcile(p.ID); err != nil {
		t.Fatal(err)
	}
	code, raw := e.do(t, "PUT", "/api/projects/"+itoa(p.ID), map[string]any{"name": "Renamed", "accountId": acct})
	if code != http.StatusConflict || !strings.Contains(string(raw), "Stop the team first") {
		t.Fatalf("schimbare cu echipa activa: %d %s", code, raw)
	}
	if got, _ := e.st.GetProject(p.ID); got.AccountID != nil || got.Name == "Renamed" {
		t.Fatalf("nu trebuia salvat nimic: %+v", got)
	}
}
```

`internal/web/accounts_check_test.go`:

```go
//go:build !windows

package web

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"foreman/internal/runner"
)

// withFakeClaude: runner-ul serverului foloseste claude-ul fals din runner/testdata.
func withFakeClaude(t *testing.T, e *env) {
	t.Helper()
	bin, err := filepath.Abs("../runner/testdata/fakeclaude.sh")
	if err != nil {
		t.Fatal(err)
	}
	e.srv.runs = runner.NewManager(bin, t.TempDir())
}

func TestAccountCheckAndDefault(t *testing.T) {
	e := setup(t)
	withFakeClaude(t, e)
	t.Setenv("FAKE_MODE", "")
	id := e.addAccount(t, "Work", "sk-ant-oat01-token-1234")
	var a struct {
		LastError string
		CheckedAt *string
	}
	_, raw := e.do(t, "GET", "/api/accounts", nil)
	var list []json.RawMessage
	json.Unmarshal(raw, &list)
	json.Unmarshal(list[0], &a)
	if a.LastError != "" || a.CheckedAt == nil {
		t.Fatalf("verificare reusita: %s", list[0])
	}

	t.Setenv("FAKE_MODE", "notlogged")
	_, raw = e.do(t, "PUT", "/api/accounts/"+itoa(id), map[string]any{"token": "sk-ant-oat01-other-5678"})
	json.Unmarshal(raw, &a)
	if a.LastError != "Not logged in · Please run /login" {
		t.Fatalf("token nou respins: %s", raw)
	}

	_, raw = e.do(t, "GET", "/api/accounts/default", nil)
	var def runner.AuthStatus
	json.Unmarshal(raw, &def)
	if !def.LoggedIn || def.Email != "me@example.com" {
		t.Fatalf("default: %s", raw)
	}
}
```

- [ ] **Step 2: Ruleaza testele, trebuie sa pice**

Run: `go test ./internal/web/ -run 'Account' -count=1`
Expected: FAIL (404 pe `/api/accounts`, `accountId` ignorat).

- [ ] **Step 3: `internal/web/api_accounts.go`**

```go
package web

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"foreman/internal/runner"
	"foreman/internal/store"
)

// checkTimeout: cat asteptam raspunsul la verificarea unui cont
const checkTimeout = 60 * time.Second

type accountInput struct {
	Name  *string `json:"name"`
	Token *string `json:"token"`
}

func validAccountName(name string) error {
	if name == "" {
		return badRequest("the account needs a name")
	}
	return maxLen("account name", name, 40)
}

// validToken: token-ul din `claude setup-token`, lipit intreg (fara spatii in el).
func validToken(token string) error {
	if len(token) < 20 || len(token) > 2000 || strings.ContainsAny(token, " \t\r\n") {
		return badRequest("paste the whole token printed by `claude setup-token`")
	}
	return nil
}

func (s *Server) listAccounts(w http.ResponseWriter, r *http.Request) {
	list, err := s.st.Accounts()
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// defaultAccount: ce cont foloseste "Default" (CLI-ul logat), doar informativ.
func (s *Server) defaultAccount(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	st, err := s.runs.AuthStatus(ctx)
	if err != nil {
		log.Printf("claude auth status: %v", err)
		st = runner.AuthStatus{} // loggedIn false: UI-ul spune ca nu stim
	}
	writeJSON(w, http.StatusOK, st)
}

func (s *Server) createAccount(w http.ResponseWriter, r *http.Request) {
	var in accountInput
	if err := decode(r, &in); err != nil {
		fail(w, err)
		return
	}
	var name, token string
	if in.Name != nil {
		name = strings.TrimSpace(*in.Name)
	}
	if in.Token != nil {
		token = strings.TrimSpace(*in.Token)
	}
	for _, err := range []error{validAccountName(name), validToken(token)} {
		if err != nil {
			fail(w, err)
			return
		}
	}
	a, err := s.st.AddAccount(name, token)
	if errors.Is(err, store.ErrConflict) {
		writeErr(w, http.StatusConflict, "an account named "+name+" already exists")
		return
	}
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, s.checkAccount(a))
}

func (s *Server) updateAccount(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		fail(w, err)
		return
	}
	var in accountInput
	if err := decode(r, &in); err != nil {
		fail(w, err)
		return
	}
	var name, token string
	if in.Name != nil {
		name = strings.TrimSpace(*in.Name)
		if err := validAccountName(name); err != nil {
			fail(w, err)
			return
		}
	}
	if in.Token != nil {
		token = strings.TrimSpace(*in.Token)
		if err := validToken(token); err != nil {
			fail(w, err)
			return
		}
	}
	err = s.st.UpdateAccount(id, name, token)
	if errors.Is(err, store.ErrConflict) {
		writeErr(w, http.StatusConflict, "an account named "+name+" already exists")
		return
	}
	if err != nil {
		fail(w, err)
		return
	}
	a, err := s.st.Account(id)
	if err != nil {
		fail(w, err)
		return
	}
	if token != "" {
		a = s.checkAccount(a) // token nou: il verificam imediat
	}
	writeJSON(w, http.StatusOK, a)
}

func (s *Server) deleteAccount(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		fail(w, err)
		return
	}
	if err := s.st.DeleteAccount(id); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) checkAccountRoute(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		fail(w, err)
		return
	}
	a, err := s.st.Account(id)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, s.checkAccount(a))
}

// checkAccount verifica token-ul cu un mesaj scurt si salveaza rezultatul pe cont. Proiectele
// contului primesc un eveniment: cardul din "Needs you" apare sau dispare. Contextul nu vine
// din cerere: o pagina inchisa nu trebuie sa lase o eroare falsa ("did not answer in time").
func (s *Server) checkAccount(a store.Account) store.Account {
	ctx, cancel := context.WithTimeout(context.Background(), checkTimeout)
	defer cancel()
	msg := ""
	if err := s.runs.Check(ctx, a.Env()); err != nil {
		msg = err.Error()
	}
	if err := s.st.SetAccountCheck(a.ID, msg); err != nil {
		log.Printf("account %d: saving the check: %v", a.ID, err)
	}
	if ids, err := s.st.AccountProjects(a.ID); err == nil {
		for _, pid := range ids {
			s.hub.Publish(pid, "project", map[string]int64{"id": pid})
		}
	}
	if fresh, err := s.st.Account(a.ID); err == nil {
		return fresh
	}
	return a
}

// accountRef: 0 = Default (nil); altfel contul trebuie sa existe.
func (s *Server) accountRef(id int64) (*int64, error) {
	if id == 0 {
		return nil, nil
	}
	if _, err := s.st.Account(id); errors.Is(err, store.ErrNotFound) {
		return nil, badRequest("unknown Claude account; reload the page")
	} else if err != nil {
		return nil, err
	}
	return &id, nil
}

// projectEnv: mediul run-urilor pornite din web pentru proiect (draft, wizard pe folder).
func (s *Server) projectEnv(projectID int64) ([]string, error) {
	a, err := s.st.ProjectAccount(projectID)
	if err != nil {
		return nil, fmt.Errorf("Claude account: %w", err)
	}
	if a == nil {
		return nil, nil
	}
	return a.Env(), nil
}
```

- [ ] **Step 4: Rute si `fail` in `internal/web/server.go`**

Dupa rutele `/api/settings/commands...`:

```go
	mux.HandleFunc("GET /api/accounts", s.listAccounts)
	mux.HandleFunc("GET /api/accounts/default", s.defaultAccount)
	mux.HandleFunc("POST /api/accounts", s.createAccount)
	mux.HandleFunc("PUT /api/accounts/{id}", s.updateAccount)
	mux.HandleFunc("DELETE /api/accounts/{id}", s.deleteAccount)
	mux.HandleFunc("POST /api/accounts/{id}/check", s.checkAccountRoute)
```

In `fail`, inainte de `case errors.Is(err, store.ErrNotFound):`:

```go
	case errors.Is(err, store.ErrAccountInUse):
		writeErr(w, http.StatusConflict, err.Error())
```

- [ ] **Step 5: Proiectul in `internal/web/api_projects.go`**

In `projectInput`, dupa `Autonomy`:

```go
	// AccountID: contul Claude; absent = neschimbat (PUT) / Default (POST); 0 = Default
	AccountID *int64 `json:"accountId"`
```

In `createProject`, dupa `p, err := in.toProject(...)` + verificarea erorii:

```go
	if in.AccountID != nil {
		if p.AccountID, err = s.accountRef(*in.AccountID); err != nil {
			fail(w, err)
			return
		}
	}
```

In `updateProject`, dupa blocul `if in.Autonomy == nil {...}` si **inainte** de `s.st.UpdateProject(p)`:

```go
	// contul se schimba doar cu echipa oprita: run-urile active sunt pe contul vechi
	changeAccount := false
	var account *int64
	if in.AccountID != nil {
		if account, err = s.accountRef(*in.AccountID); err != nil {
			fail(w, err)
			return
		}
		same := (account == nil && existing.AccountID == nil) ||
			(account != nil && existing.AccountID != nil && *account == *existing.AccountID)
		if !same {
			if s.team.ProjectActive(id) {
				writeErr(w, http.StatusConflict, "Stop the team first: agents are working on the current Claude account.")
				return
			}
			changeAccount = true
		}
	}
```

si dupa `s.st.UpdateProject(p)` reusit:

```go
	if changeAccount {
		name := "Default"
		if account != nil {
			if a, err := s.st.Account(*account); err == nil {
				name = a.Name
			}
		}
		if _, err := s.st.SetProjectAccount(id, account, "Claude account changed to "+name+"; starting a new session."); err != nil {
			fail(w, err)
			return
		}
	}
```

In `projectCard`, dupa `FillIns`:

```go
	// AccountName / AccountError: contul Claude al proiectului (gol = Default) si ultima lui eroare ("Needs you")
	AccountName  string `json:"accountName"`
	AccountError string `json:"accountError"`
```

In `card`, dupa `c.FillIns = len(fills)` / `c.fills = fills`:

```go
	if acct, err := s.st.ProjectAccount(p.ID); err != nil {
		return projectCard{}, nil, nil, nil, err
	} else if acct != nil {
		c.AccountName, c.AccountError = acct.Name, acct.LastError
	}
```

- [ ] **Step 6: Ruleaza testele web**

Run: `go test ./internal/web/ -count=1 -race`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/web/api_accounts.go internal/web/accounts_test.go internal/web/accounts_check_test.go internal/web/server.go internal/web/api_projects.go
git commit -m "web: API conturi Claude (Check, default) si contul pe proiect"
```

---

### Task 5: wizard si draft pe contul ales

**Files:**
- Modify: `internal/setup/manager.go` (`Account`, `StartAs`, `Session.AccountID`, `env`, `FolderInput.Env`, trei `runner.Spec`)
- Modify: `internal/setup/manager_test.go` (test nou)
- Modify: `internal/draft/draft.go` (`Input.Env`)
- Modify: `internal/draft/draft_test.go` (test nou)
- Modify: `internal/web/api_setup.go` (`startSetup`, `startFolderSetup`, `createProjectFromSetup`)
- Modify: `internal/web/api_draft.go` (`startDraft`)
- Modify: `internal/web/setup_test.go` (test nou)

**Interfaces:**
- Consumes: `store.Account.Env`, `Server.accountRef`, `Server.projectEnv` (Task 4)
- Produces:
  - `type setup.Account struct { ID int64; Env []string }`
  - `func (m *setup.Manager) StartAs(acct Account, workdirs []string, description, model string) (Session, error)` (`Start` = `StartAs(Account{}, ...)`)
  - `setup.Session.AccountID int64` (json `accountId`); `setup.FolderInput.Env []string`; `draft.Input.Env []string`
  - `POST /api/setup` accepta `accountId`

- [ ] **Step 1: Scrie testele**

In `internal/setup/manager_test.go`, la sfarsit:

```go
func TestStartAsCarriesAccount(t *testing.T) {
	m, f := newTest(t)
	env := []string{"CLAUDE_CODE_OAUTH_TOKEN=work"}
	s, err := m.StartAs(Account{ID: 7, Env: env}, nil, "notes app", "sonnet")
	if err != nil {
		t.Fatal(err)
	}
	if s.AccountID != 7 || len(f.specs) != 1 || len(f.specs[0].Env) != 1 || f.specs[0].Env[0] != env[0] {
		t.Fatalf("session=%+v env=%v", s, f.specs[0].Env)
	}
	if _, err := m.Start(nil, "notes", "sonnet"); err != nil {
		t.Fatal(err)
	}
	if f.specs[1].Env != nil {
		t.Fatalf("Start fara cont: Env = %v", f.specs[1].Env)
	}
	dir := t.TempDir()
	in := FolderInput{Project: store.Project{ID: 3, Name: "Acme", Folders: []store.Folder{{ID: 1, Name: "app", Path: dir}}},
		Folder: store.Folder{ID: 1, Name: "app", Path: dir}, Env: env}
	if _, err := m.StartFolder(in, "", "sonnet"); err != nil {
		t.Fatal(err)
	}
	if got := f.specs[2].Env; len(got) != 1 || got[0] != env[0] {
		t.Fatalf("StartFolder: Env = %v", got)
	}
}
```

Chat-ul (`m.Chat`) foloseste acelasi `s.env`; in testul din `chat_test.go` care porneste un chat dupa `Start`, adauga o
verificare similara doar daca acel test foloseste deja `StartAs`; altfel e acoperit de cod (vezi Step 3, `Chat`).

In `internal/draft/draft_test.go`, la sfarsit:

```go
func TestDraftCarriesEnv(t *testing.T) {
	m, f := newTest(t)
	env := []string{"CLAUDE_CODE_OAUTH_TOKEN=work"}
	if _, err := m.Start(Input{ProjectID: 1, Dir: t.TempDir(), Ask: "a QA agent", Model: "haiku", Env: env}); err != nil {
		t.Fatal(err)
	}
	if got := f.specs[0].Env; len(got) != 1 || got[0] != env[0] {
		t.Fatalf("Env = %v", got)
	}
}
```

In `internal/web/setup_test.go`, la sfarsit:

```go
func TestSetupWithAccount(t *testing.T) {
	e := withSetup(t)
	acct := e.addAccount(t, "Work", "sk-ant-oat01-token-1234")
	code, raw := e.do(t, "POST", "/api/setup", map[string]any{"description": "notes app", "model": "sonnet", "accountId": acct})
	var sess struct {
		ID        string
		AccountID int64
	}
	json.Unmarshal(raw, &sess)
	if code != http.StatusCreated || sess.AccountID != acct {
		t.Fatalf("POST /api/setup: %d %s", code, raw)
	}
	if code, _ := e.do(t, "POST", "/api/setup", map[string]any{"description": "x", "model": "sonnet", "accountId": 999}); code != http.StatusBadRequest {
		t.Fatalf("cont necunoscut: %d", code)
	}
	proposal := map[string]any{"name": "Notes", "goal": "g", "context": "c",
		"agents": []map[string]any{{"name": "Ana", "role": "Lead", "model": "sonnet", "reportsTo": ""}}}
	code, raw = e.do(t, "POST", "/api/projects/setup", map[string]any{"setupId": sess.ID, "workdir": t.TempDir(), "proposal": proposal})
	var p struct {
		ID        int64
		AccountID *int64
	}
	json.Unmarshal(raw, &p)
	if code != http.StatusCreated || p.AccountID == nil || *p.AccountID != acct {
		t.Fatalf("proiect din wizard: %d %s", code, raw)
	}
}
```

Daca `setup_test.go` nu importa deja `net/http`, adauga-l (sau foloseste `201` / `400` ca restul fisierului).

- [ ] **Step 2: Ruleaza testele, trebuie sa pice**

Run: `go test ./internal/setup/ ./internal/draft/ ./internal/web/ -run 'Account|Env' -count=1`
Expected: FAIL la compilare (`undefined: Account`, `unknown field Env`).

- [ ] **Step 3: `internal/setup/manager.go`**

Dupa `type Starter interface {...}`:

```go
// Account: contul Claude pe care ruleaza analistul (ID 0 + Env nil = Default, CLI-ul logat).
type Account struct {
	ID  int64
	Env []string
}
```

In `Session`, dupa `FolderID`:

```go
	AccountID   int64     `json:"accountId"` // contul ales la pasul 1 (0 = Default); devine contul proiectului creat
```

si in partea privata, dupa `addDirs`:

```go
	env     []string // mediul run-urilor (token-ul contului); nu iese in JSON
```

In `FolderInput`, dupa `Team`:

```go
	Env     []string // contul proiectului (Server.projectEnv)
```

`Start` devine un wrapper; corpul vechi trece in `StartAs`:

```go
// Start porneste analistul pe contul implicit.
func (m *Manager) Start(workdirs []string, description, model string) (Session, error) {
	return m.StartAs(Account{}, workdirs, description, model)
}

// StartAs valideaza cererea si porneste analistul pe contul dat.
func (m *Manager) StartAs(acct Account, workdirs []string, description, model string) (Session, error) {
	// ... corpul vechi al lui Start, neschimbat, cu doua modificari:
}
```

Modificarile in corpul mutat:
1. la crearea sesiunii, `s := &Session{...}` primeste `AccountID: acct.ID,`, iar imediat dupa: `s.env = acct.Env`;
2. `spec := runner.Spec{...}` primeste `Env: s.env,`.

In `StartFolder`: la crearea sesiunii adauga `env: in.Env,` in literalul `&Session{...}`, iar spec-ul devine:

```go
	spec := runner.Spec{ID: s.RunID, Key: "setup-" + s.ID, Dir: s.cwd, Args: m.args(s, model, turnPrompt(s)), Prompt: turnPrompt(s), Env: s.env}
```

In `Chat`:

```go
	spec := runner.Spec{ID: s.ChatRunID, Key: "setup-" + s.ID, Dir: s.cwd, Args: args, Prompt: turn, Env: s.env}
```

Cauta si alte `runner.Spec{` in pachet (`grep -n "runner.Spec{" internal/setup/*.go`): fiecare primeste `Env: s.env`.

- [ ] **Step 4: `internal/draft/draft.go`**

In `Input`, dupa `Ask, Model string`:

```go
	Env        []string // contul Claude al proiectului (nil = Default)
```

In `Start`:

```go
	spec := runner.Spec{ID: d.runID, Key: "draft-" + d.ID, Dir: dir, Args: m.args(d, in.Model, turn), Prompt: turn, Env: in.Env}
```

- [ ] **Step 5: Web**

`internal/web/api_setup.go`, `startSetup`: structul de intrare primeste

```go
		AccountID   int64    `json:"accountId"` // 0 = Default
```

si apelul devine:

```go
	var acct wizard.Account
	if in.AccountID != 0 {
		a, err := s.st.Account(in.AccountID)
		if errors.Is(err, store.ErrNotFound) {
			fail(w, badRequest("unknown Claude account; reload the page"))
			return
		} else if err != nil {
			fail(w, err)
			return
		}
		acct = wizard.Account{ID: a.ID, Env: a.Env()}
	}
	sess, err := s.wizard.StartAs(acct, in.Workdirs, in.Description, in.Model)
```

(adauga importurile `errors` si `foreman/internal/store` daca lipsesc).

`startFolderSetup`, inainte de `s.wizard.StartFolder(...)`:

```go
	env, err := s.projectEnv(p.ID)
	if err != nil {
		fail(w, err)
		return
	}
	sess, err := s.wizard.StartFolder(wizard.FolderInput{Project: p, Folder: folder, Team: team, Env: env}, in.Description, in.Model)
```

`createProjectFromSetup`, inainte de `s.st.CreateProjectWithTeam(&p, ...)`:

```go
	// contul ales la pasul 1 ramane pe sesiune (si cand pagina wizard-ului a fost redeschisa)
	if s.wizard != nil && in.SetupID != "" {
		if sess, ok := s.wizard.Get(in.SetupID); ok && sess.AccountID != 0 {
			ref, err := s.accountRef(sess.AccountID)
			if err != nil {
				fail(w, err)
				return
			}
			p.AccountID = ref
		}
	}
```

`internal/web/api_draft.go`, `startDraft`, inainte de `s.drafts.Start(...)`:

```go
	env, err := s.projectEnv(id)
	if err != nil {
		fail(w, err)
		return
	}
```

si `draft.Input{...}` primeste `Env: env`.

- [ ] **Step 6: Ruleaza testele**

Run: `go test ./internal/setup/ ./internal/draft/ ./internal/web/ -count=1 -race`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/setup/manager.go internal/setup/manager_test.go internal/draft/draft.go internal/draft/draft_test.go internal/web/api_setup.go internal/web/api_draft.go internal/web/setup_test.go
git commit -m "wizard si draft: ruleaza pe contul Claude ales / al proiectului"
```

---

### Task 6: UI (Settings, formular, wizard, badge, Needs you)

**Files:**
- Create: `internal/web/static/js/accounts.js`
- Modify: `internal/web/static/js/settings.js`
- Modify: `internal/web/static/js/projects.js` (`projectFormView`, randul din tabel)
- Modify: `internal/web/static/js/setup.js` (pasul 1)
- Modify: `internal/web/static/js/workspace.js` (`renderHeader`, `renderNeeds`)
- Modify: `internal/web/static/app.css`

**Interfaces:**
- Consumes: rutele din Task 4 si 5; campurile `accountId`, `accountName`, `accountError` ale proiectului.
- Produces: `export function accountsSettings()` (accounts.js)

- [ ] **Step 1: `internal/web/static/js/accounts.js`**

```js
import { api } from "./api.js";
import { el, toast } from "./ui.js";

// ago: "5 min ago" / "2h ago" / "3d ago", pentru ultima verificare
function ago(iso) {
  const s = Math.max(0, (Date.now() - new Date(iso).getTime()) / 1000);
  if (s < 3600) return `${Math.max(1, Math.round(s / 60))} min ago`;
  if (s < 86400) return `${Math.round(s / 3600)}h ago`;
  return `${Math.round(s / 86400)}d ago`;
}

// accountsSettings: conturile Claude din Settings. "Default" e CLI-ul logat (doar informativ);
// celelalte au token din `claude setup-token` si se aleg pe proiect.
export function accountsSettings() {
  const box = el("section", { class: "section accounts" });
  let list = [];
  let def = null;      // claude auth status; null = inca nu stim
  let busy = 0;        // id-ul contului verificat/salvat acum (-1 = Add)
  let replacing = 0;   // contul cu formularul "Replace token" deschis
  let adding = false;

  async function load() {
    try { list = await api("/api/accounts"); } catch (err) { toast(err.message); }
    draw();
  }
  async function loadDefault() {
    try { def = await api("/api/accounts/default"); } catch { def = { loggedIn: false }; }
    draw();
  }
  // run: o actiune pe un cont; butoanele stau oprite pana termina (Check poate dura cateva secunde)
  async function run(id, fn) {
    busy = id; draw();
    try { await fn(); } catch (err) { toast(err.message); }
    busy = 0;
    await load();
  }

  function state(a) {
    if (busy === a.id) return el("span", { class: "acct-state" }, "Checking…");
    if (a.lastError) return el("span", { class: "acct-state bad", title: a.lastError }, a.lastError);
    if (a.checkedAt) return el("span", { class: "acct-state ok" }, `Works · checked ${ago(a.checkedAt)}`);
    return el("span", { class: "acct-state" }, "Not checked yet");
  }

  function tokenInput() {
    return el("input", { type: "password", autocomplete: "off", spellcheck: "false", placeholder: "sk-ant-oat01-…", "aria-label": "Token" });
  }

  function replaceForm(a) {
    const token = tokenInput();
    const save = () => run(a.id, async () => {
      await api(`/api/accounts/${a.id}`, { method: "PUT", body: { token: token.value.trim() } });
      replacing = 0;
    });
    token.addEventListener("keydown", (e) => { if (e.key === "Enter") { e.preventDefault(); save(); } });
    queueMicrotask(() => token.focus());
    return el("div", { class: "acct-form" }, token,
      el("button", { type: "button", class: "btn small primary", disabled: !!busy, onclick: save }, "Save and check"),
      el("button", { type: "button", class: "btn small", onclick: () => { replacing = 0; draw(); } }, "Cancel"));
  }

  function row(a) {
    const used = a.projects ? `Used by ${a.projects} ${a.projects === 1 ? "project" : "projects"}` : "Not used";
    return el("div", { class: "acct-row" },
      el("div", { class: "acct-main" },
        el("strong", {}, a.name),
        el("code", { class: "acct-hint" }, a.tokenHint ? `••••${a.tokenHint}` : "••••"),
        state(a)),
      el("span", { class: "hint" }, used),
      el("span", { class: "grow" }),
      el("button", { type: "button", class: "btn small", disabled: !!busy,
        onclick: () => run(a.id, () => api(`/api/accounts/${a.id}/check`, { method: "POST" })) }, "Check"),
      el("button", { type: "button", class: "btn small", disabled: !!busy,
        onclick: () => { replacing = replacing === a.id ? 0 : a.id; draw(); } }, "Replace token"),
      el("button", { type: "button", class: "link-btn danger", disabled: !!busy || a.projects > 0,
        title: a.projects ? "Move its projects to another account first" : null,
        onclick: () => { if (confirm(`Delete the Claude account ${a.name}?`)) run(a.id, () => api(`/api/accounts/${a.id}`, { method: "DELETE" })); } }, "Delete"),
      replacing === a.id ? replaceForm(a) : null);
  }

  function defaultRow() {
    let text = "…";
    let cls = "acct-state";
    if (def && def.loggedIn) {
      text = [def.email || def.authMethod, def.subscriptionType].filter(Boolean).join(" · ");
      cls += " ok";
    } else if (def) {
      text = "Not logged in: run claude in a terminal and log in";
      cls += " bad";
    }
    return el("div", { class: "acct-row" },
      el("div", { class: "acct-main" }, el("strong", {}, "Default"), el("span", { class: "hint" }, "the logged-in Claude CLI"),
        el("span", { class: cls }, text)));
  }

  function addForm() {
    const name = el("input", { type: "text", maxlength: 40, placeholder: "e.g. Work", "aria-label": "Account name" });
    const token = tokenInput();
    const save = () => run(-1, async () => {
      await api("/api/accounts", { method: "POST", body: { name: name.value.trim(), token: token.value.trim() } });
      adding = false;
    });
    queueMicrotask(() => name.focus());
    return el("div", { class: "acct-add" },
      el("p", { class: "hint" }, "In a terminal, run ", el("code", {}, "claude setup-token"),
        " and sign in with the other account in the browser. Paste the token it prints. It is stored in Foreman's database on this machine and never shown again."),
      el("div", { class: "acct-form" }, name, token,
        el("button", { type: "button", class: "btn small primary", disabled: !!busy, onclick: save }, busy === -1 ? "Checking…" : "Add and check"),
        el("button", { type: "button", class: "btn small", onclick: () => { adding = false; draw(); } }, "Cancel")));
  }

  function draw() {
    box.replaceChildren(
      el("h2", {}, "Claude accounts"),
      el("p", { class: "hint" }, "Each project runs on Default or on one of these accounts (pick it in the project's Instructions). Use this to keep work projects on the company subscription."),
      el("div", { class: "acct-table" }, defaultRow(), list.map(row)),
      adding ? addForm() : el("div", { class: "cmd-add" },
        el("button", { type: "button", class: "btn small", disabled: !!busy, onclick: () => { adding = true; draw(); } }, "+ Add account")));
  }

  draw();
  load();
  loadDefault();
  return box;
}
```

- [ ] **Step 2: `settings.js`**

```js
import { el, topbar } from "./ui.js";
import { globalCommands } from "./commands.js";
import { accountsSettings } from "./accounts.js";

// settingsView: setarile aplicatiei: conturile Claude si regulile pentru comenzi.
export function settingsView(root) {
  root.replaceChildren(
    topbar("settings"),
    el("main", { class: "settings-page" },
      el("h1", {}, "Settings"),
      accountsSettings(),
      globalCommands()));
}
```

- [ ] **Step 3: Formularul de proiect (`projects.js`, `projectFormView`)**

Dupa `const autonomy = ...`:

```js
  // contul Claude: Default (CLI-ul logat) sau un cont din Settings
  let accounts = [];
  try { accounts = await api("/api/accounts"); } catch { /* doar Default */ }
  const currentAccount = String(p.accountId || 0);
  const account = el("select", { value: currentAccount },
    el("option", { value: "0" }, "Default · the logged-in CLI"),
    accounts.map((a) => el("option", { value: String(a.id) }, a.name)));
  const accountNote = el("p", { class: "hint warn", hidden: true },
    "Agents start new Claude sessions on the new account. Earlier messages stay saved. Stop the team first if it is working.");
  account.addEventListener("change", () => { accountNote.hidden = !id || account.value === currentAccount; });
```

In sectiunea "How the team works", dupa `el("p", { class: "hint" }, "Both are optional: ...")`:

```js
            field("Claude account", account, accounts.length ? "Which subscription the agents use." : "Add other accounts in Settings."),
            accountNote,
```

In `save`, in `body`, dupa `autonomy: autonomy.checked,`:

```js
      accountId: Number(account.value),
```

- [ ] **Step 4: Badge in tabelul Proiecte (`projects.js`)**

`asks` primeste si eroarea contului:

```js
  const asks = (p.openQuestions || 0) + (p.stuckTasks || 0) + (p.accessRequests || 0) + (p.agentRequests || 0) + (p.workRequests || 0) + (p.fillIns || 0) + (p.libraryProposals || 0) + (p.accountError ? 1 : 0); // intrebari, task-uri blocate, cereri de acces, cereri de agenti noi, cereri de lucru, contul Claude cazut
```

Dupa badge-ul `autonomous`:

```js
        p.accountName ? el("span", { class: "badge-account", title: "Claude account used by this project" }, p.accountName) : null),
```

(atentie la paranteza de inchidere: badge-ul `autonomous` era ultimul copil al lui `el("a", ...)`; muta `)` dupa noul badge).

- [ ] **Step 5: Header si "Needs you" (`workspace.js`)**

In `renderHeader`, dupa chip-ul `Autonomous`:

```js
        p.accountName ? el("span", { class: "status-chip account", title: "Claude account used by this project's agents" }, p.accountName) : null,
```

Langa celelalte liste din "Needs you" (dupa `const libraryList = ...`):

```js
  const accountList = el("div", { class: "needs-account" });
```

`needsBox` primeste `accountList` primul (o eroare de cont opreste tot proiectul):

```js
  const needsBox = el("section", { class: "needs-you", "aria-live": "polite", hidden: true },
    needsTitle, accountList, questionsBox, fillList, stuckList, accessList, agentReqList, workReqList, libraryList);
```

Langa celelalte chei (`let accessKey = null;`):

```js
  let accountKey = null;   // eroarea contului desenata ultima data
```

In `renderNeeds`, `total` primeste `+ (state.project.accountError ? 1 : 0)`, iar la sfarsit:

```js
    const acKey = state.project.accountError ? `${state.project.accountName}:${state.project.accountError}` : "";
    if (acKey !== accountKey) {
      accountKey = acKey;
      accountList.replaceChildren(...(acKey ? [accountNode(state.project)] : []));
    }
```

Langa `accessNode`:

```js
  // accountNode: contul Claude al proiectului a cazut la autentificare; proiectul e pe pauza
  function accountNode(p) {
    return el("div", { class: "question access" },
      el("div", { class: "question-head" },
        el("span", { class: "label" }, "CLAUDE ACCOUNT"),
        el("strong", {}, `Claude account ${p.accountName} failed`)),
      el("p", { class: "question-text" }, el("code", {}, p.accountError)),
      el("p", { class: "question-text hint" }, "The project is paused. Replace the token or check the account in Settings, then Start team."),
      el("div", { class: "actions" }, el("a", { class: "btn small primary", href: "#/settings" }, "Open Settings")));
  }
```

- [ ] **Step 6: Wizard (`setup.js`, pasul 1)**

Dupa `const info = await api("/api/info");`:

```js
  let accounts = [];
  try { accounts = await api("/api/accounts"); } catch { /* doar Default */ }
```

`form` primeste `accountId: 0`:

```js
  const form = { workdirs: [""], description: "", model: "sonnet", accountId: 0 };
```

In `stepChoose`, dupa `const model = modelSelect();`:

```js
    // contul pe care ruleaza analiza; devine contul proiectului creat
    const account = accounts.length ? el("select", { value: String(form.accountId) },
      el("option", { value: "0" }, "Default · the logged-in CLI"),
      accounts.map((a) => el("option", { value: String(a.id) }, a.name))) : null;
```

dupa `field("Model for the analysis", ...)`:

```js
      account && field("Claude account", account, "The analysis and the new project's agents run on this account."),
```

si in `start`, `Object.assign(form, {...})` primeste `accountId: Number(account?.value || 0),`.

Modul folder (`stepChooseFolder`) nu primeste select: foloseste contul proiectului.

- [ ] **Step 7: CSS (`app.css`)**

Dupa `.badge-auto`:

```css
.badge-account { font-size: 11px; font-weight: 600; color: var(--purple); background: rgba(199, 155, 242, .13); padding: 1px 8px; border-radius: 999px; margin-left: 6px; }
```

Dupa `.status-chip.auto`:

```css
.status-chip.account { font-size: 12px; font-weight: 600; color: var(--purple); background: rgba(199, 155, 242, .13); padding: 2px 9px; border-radius: 999px; }
```

Dupa `.cmd-row`:

```css
.acct-table { display: grid; gap: 6px; }
.acct-row { display: flex; align-items: center; gap: 12px; flex-wrap: wrap; padding: 8px 0; border-bottom: 1px solid var(--line); }
.acct-main { display: flex; align-items: baseline; gap: 10px; flex-wrap: wrap; min-width: 0; }
.acct-hint { color: var(--muted); font-size: 12px; }
.acct-state { font-size: 12px; color: var(--muted); }
.acct-state.ok { color: var(--teal); }
.acct-state.bad { color: var(--red); max-width: 48ch; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.acct-form { display: flex; gap: 8px; flex-wrap: wrap; width: 100%; }
.acct-form input { flex: 1 1 220px; min-width: 0; }
.acct-add { display: grid; gap: 8px; }
.hint.warn { color: var(--amber); }
```

Verifica inainte ca `.hint.warn` nu exista deja (`grep -n "hint.warn" internal/web/static/app.css`); daca exista, nu-l dubla.

- [ ] **Step 8: Verifica JS-ul**

```bash
for f in accounts settings projects setup workspace; do cp internal/web/static/js/$f.js /tmp/check-$f.mjs && node --check /tmp/check-$f.mjs || echo "FAIL $f"; done
```

Expected: nicio linie `FAIL`.

- [ ] **Step 9: Verificare in browser (Chrome headless, cf. memoria proiectului)**

Porneste aplicatia cu un HOME temporar (datele stau in `$HOME/.foreman`; nu atinge baza reala):

```bash
go build -o /tmp/foreman-acct ./cmd/foreman && HOME=$(mktemp -d) /tmp/foreman-acct start --port 7788 --no-open &
```

Creeaza un proiect si adauga un cont cu un token fals prin API:

```bash
curl -s -X POST localhost:7788/api/accounts -H 'Content-Type: application/json' -d '{"name":"Work","token":"sk-ant-oat01-fake-token-ABCD"}'
```

Fa screenshot-uri (Chrome headless prin CDP) la: `#/settings` (Default + Work cu eroarea rosie), formularul unui proiect
(`#/p/<id>/edit`, select-ul "Claude account"), pagina proiectului mutat pe Work (chip-ul in header + cardul "Claude account
Work failed" in Needs you), tabelul Proiecte (badge) si pasul 1 din wizard. Verifica la 1280px si la 400px ca randurile se
rup corect si nimic nu iese din ecran.

- [ ] **Step 10: Commit**

```bash
git add internal/web/static/js/accounts.js internal/web/static/js/settings.js internal/web/static/js/projects.js internal/web/static/js/setup.js internal/web/static/js/workspace.js internal/web/static/app.css
git commit -m "ui: conturi Claude in Settings, pe proiect, in wizard si in Needs you"
```

---

### Task 7: Verificare finala si documentatie

**Files:**
- Modify: `CLAUDE.md` (sectiunea "Stare")
- Modify: `docs/superpowers/specs/2026-10-09-foreman-claude-accounts-design.md` (Status)

- [ ] **Step 1: Verificarile din conventii**

```bash
go vet ./... && go build -race ./... && go test ./... -count=1 && GOOS=darwin GOARCH=arm64 go build -o /dev/null ./cmd/foreman && GOOS=windows go build -o /dev/null ./cmd/foreman
```

Expected: totul trece.

- [ ] **Step 2: Test real cu un token adevarat (manual, cu utilizatorul)**

Utilizatorul ruleaza `claude setup-token` cu contul Team, adauga contul in Settings si apasa Check ("Works"), muta un
proiect de test pe el, trimite un mesaj radacinii si confirma ca run-ul merge. Optional: un token gresit → Check rosu.

- [ ] **Step 3: `CLAUDE.md`**

Dupa paragraful "Gata (fisiere pe task, ...)":

```
Gata (cont Claude per proiect, spec in docs/superpowers/specs/2026-10-09-foreman-claude-accounts-design.md):
claude_accounts (migrarea 025; token din `claude setup-token`, nu iese din API) + projects.account_id (NULL = Default,
CLI-ul logat). runner.Spec.Env pune CLAUDE_CODE_OAUTH_TOKEN si scoate variabilele de cont mostenite (doar pe proiectele
cu cont); Check (mesaj scurt cu haiku) si claude auth status in Settings > Claude accounts. Schimbarea contului cere echipa
oprita si reseteaza sesiunile (ca New chat); o eroare de autentificare pune proiectul pe pauza, task-ul inapoi in Queued si
cardul "Claude account failed" in Needs you. Wizard-ul alege contul la pasul 1.
```

- [ ] **Step 4: Spec**

`Status: implementat (<data>).`

- [ ] **Step 5: Commit**

```bash
git add CLAUDE.md docs/superpowers/specs/2026-10-09-foreman-claude-accounts-design.md
git commit -m "docs: cont Claude per proiect implementat"
```
