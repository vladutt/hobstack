# Reguli pentru comenzi: plan de implementare

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** lista fixa de comenzi blocate devine reguli editabile (Allow / Ask / Block) in Settings, cu override pe
proiect, iar comenzile pe Ask ajung in Needs you cu Allow once / Allow in this project / Block / Dismiss.

**Architecture:** store-ul tine regulile (`command_rules`, `project_id = 0` = global), grant-urile „Allow once”
(`command_grants`) si prefixul pe `access_requests` (migrarea 023). Pachetul `prompt` primeste functii pure de
potrivire (`internal/prompt/commands.go`) si `Settings` primeste lista de prefixe de pus in deny. `team` calculeaza
regulile efective la fiecare run, consuma grant-urile, transforma refuzurile pe Ask in cereri cu prefix si aplica
deciziile utilizatorului (cu trezirea agentului). `web` expune API-ul; UI-ul adauga `#/settings`, sectiunea din
setarile proiectului si butoanele noi pe cardul de acces.

**Tech Stack:** Go 1.22+, SQLite (modernc.org/sqlite), JS vanilla (ES modules).

**Spec:** `docs/superpowers/specs/2026-10-08-foreman-command-rules-design.md`

## Global Constraints

- Tot ce vede utilizatorul sau agentul e in engleza; comentariile din cod in romana (fara diacritice).
- `go vet ./...`, `go build -race ./cmd/foreman`, `GOOS=darwin GOARCH=arm64 go build ./cmd/foreman` si
  `GOOS=windows go build ./cmd/foreman` trec. Fara dependente noi. JS verificat cu `node --check` pe o copie `.mjs`.
- Stari: exact `allow`, `ask`, `block`. Global = `project_id 0`.
- Prefix valid: 1..80 caractere dupa normalizarea spatiilor (spatii simple, fara spatii la capete), fara caracterele
  `& | ; $ \` ( ) < > * " '` si fara newline. Case-sensitive.
- Regulile implicite (migrare si Reset): `git push` = ask, `rm -rf` = ask, `brew install` = ask, `sudo` = block.
- Raman fixe in deny: `TaskCreate`, `TaskUpdate`, `TaskList`, `TaskGet`, `TodoWrite`, `Agent`; instalarile de pachete
  (`packageRules`) raman in deny fara tool-ul `packages`. Regulile pentru comenzi se aplica doar agentilor cu `shell`.
- Potrivire: comanda se taie pe `&`, `|`, `;`; pentru fiecare bucata castiga cel mai lung prefix potrivit (cuvinte
  intregi); oricare bucata Block -> block; altfel oricare Ask -> ask; altfel allow.
- Full autonomy nu aproba cereri Ask.
- `TestOpenAppliesMigrationsOnce` trece la 23.
- Comenzile se ruleaza din `agentdash/`. Nu atinge aplicatia de pe 7777 si nici `~/.foreman`.

## Review Focus

1. Comanda legata (`echo ok && git push origin main`): bucata a doua prinde regula Ask, cererea are prefixul `git push` (test in Task 2).
2. Regula proiectului `git push = allow` cu globala `git push = ask`: `git push` nu mai e in deny pentru acel proiect, dar ramane in altul (test in Task 3).
3. Allow once urmat de doua run-uri: primul are `git push` scos din deny, al doilea il are iar (test in Task 3).
4. Prefix cu caractere de shell (`git push; rm`), gol sau de 81 de caractere: refuzat cu 400 si mesaj clar, nimic salvat (test in Task 4).
5. Agent fara Shell refuzat la `git push`: cererea ramane „Enable Shell” (nu una de comanda), iar cu `git push = block` „Enable Shell” nu mai e oferit (test in Task 4).

---

### Task 1: store: reguli, grant-uri, prefix pe cereri

**Files:**
- Create: `internal/store/migrations/023_command_rules.sql`
- Create: `internal/store/commands.go`
- Create: `internal/store/commands_test.go`
- Modify: `internal/store/access.go` (campul `Prefix`, `RecordAccessDenial` cu prefix)
- Modify: `internal/store/projects.go` (`DeleteProject` sterge regulile proiectului)
- Modify: `internal/store/store_test.go` (23 de migrari)
- Modify: toate apelurile `RecordAccessDenial` (`internal/team/run.go`, teste) pentru noul parametru

**Interfaces:**
- Produces:

```go
const (CmdAllow = "allow"; CmdAsk = "ask"; CmdBlock = "block")
type CommandRule struct {
	Prefix string `json:"prefix"`
	Mode   string `json:"mode"`
}
var DefaultCommandRules = []CommandRule{{"git push", CmdAsk}, {"rm -rf", CmdAsk}, {"brew install", CmdAsk}, {"sudo", CmdBlock}}
func (s *Store) CommandRules(projectID int64) ([]CommandRule, error)            // 0 = globale; dupa prefix
func (s *Store) SetCommandRules(projectID int64, rules []CommandRule) error      // inlocuieste lista nivelului
func (s *Store) ResetGlobalCommandRules() error                                  // DefaultCommandRules
func (s *Store) AddCommandGrant(projectID, agentID int64, prefix string) error
func (s *Store) TakeCommandGrants(agentID int64) ([]string, error)               // intoarce si marcheaza folosite
// AccessRequest primeste: Prefix string `json:"prefix"` ("" = cerere de tool, ca azi)
func (s *Store) RecordAccessDenial(projectID, agentID int64, taskID *int64, tool, detail, prefix string) error
```

Validarea prefixelor NU e in store (e in `prompt`, Task 2); store-ul salveaza ce primeste.

- [ ] **Step 1: testul**

`internal/store/commands_test.go`:

```go
package store

import "testing"

func TestCommandRulesAndGrants(t *testing.T) {
	s := newStore(t)
	p, lead := newProject(t, s)
	global, err := s.CommandRules(0)
	if err != nil || len(global) != 4 {
		t.Fatalf("regulile implicite: %+v %v", global, err)
	}
	if err := s.SetCommandRules(p.ID, []CommandRule{{"git push", CmdAllow}, {"docker", CmdBlock}}); err != nil {
		t.Fatal(err)
	}
	proj, _ := s.CommandRules(p.ID)
	if len(proj) != 2 || proj[0].Prefix != "docker" || proj[1].Mode != CmdAllow {
		t.Fatalf("%+v", proj)
	}
	// inlocuire: lista noua o sterge pe cea veche
	s.SetCommandRules(p.ID, []CommandRule{{"kubectl", CmdAsk}})
	if proj, _ = s.CommandRules(p.ID); len(proj) != 1 || proj[0].Prefix != "kubectl" {
		t.Fatalf("%+v", proj)
	}
	s.SetCommandRules(0, nil)
	if g, _ := s.CommandRules(0); len(g) != 0 {
		t.Fatalf("%+v", g)
	}
	s.ResetGlobalCommandRules()
	if g, _ := s.CommandRules(0); len(g) != 4 {
		t.Fatalf("reset: %+v", g)
	}
	// grant-uri: luate o singura data
	s.AddCommandGrant(p.ID, lead.ID, "git push")
	if g, _ := s.TakeCommandGrants(lead.ID); len(g) != 1 || g[0] != "git push" {
		t.Fatalf("%v", g)
	}
	if g, _ := s.TakeCommandGrants(lead.ID); len(g) != 0 {
		t.Fatalf("grant folosit de doua ori: %v", g)
	}
	// cereri: acelasi agent + tool + prefix se aduna; alt prefix = alta cerere
	s.RecordAccessDenial(p.ID, lead.ID, nil, "Bash", "git push origin main", "git push")
	s.RecordAccessDenial(p.ID, lead.ID, nil, "Bash", "git push -f", "git push")
	s.RecordAccessDenial(p.ID, lead.ID, nil, "Bash", "rm -rf build", "rm -rf")
	reqs, _ := s.ListOpenAccessRequests(p.ID)
	if len(reqs) != 2 || reqs[0].Count != 2 || reqs[0].Prefix != "git push" || reqs[0].Detail != "git push -f" {
		t.Fatalf("%+v", reqs)
	}
	// stergerea proiectului ia si regulile lui
	if err := s.DeleteProject(p.ID); err != nil {
		t.Fatal(err)
	}
	var n int
	s.db.QueryRow(`SELECT COUNT(*) FROM command_rules WHERE project_id = ?`, p.ID).Scan(&n)
	if n != 0 {
		t.Fatalf("reguli ramase: %d", n)
	}
}
```

(Daca `DeleteProject` refuza un proiect cu agenti, sterge intai ce cere functia, cum fac testele existente pentru
`DeleteProject`; verifica in `projects_test.go`.)

- [ ] **Step 2: ruleaza, trebuie sa pice**

Run: `go test ./internal/store/ -run TestCommandRulesAndGrants`
Expected: FAIL (`CommandRules` nedefinit).

- [ ] **Step 3: implementarea**

`023_command_rules.sql`:

```sql
-- Reguli pentru comenzile agentilor cu Shell: globale (project_id 0) si per proiect
CREATE TABLE command_rules (
  project_id INTEGER NOT NULL,
  prefix     TEXT NOT NULL,
  mode       TEXT NOT NULL CHECK (mode IN ('allow', 'ask', 'block')),
  updated_at TEXT NOT NULL,
  PRIMARY KEY (project_id, prefix)
);
INSERT INTO command_rules (project_id, prefix, mode, updated_at) VALUES
  (0, 'git push', 'ask', strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  (0, 'rm -rf', 'ask', strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  (0, 'brew install', 'ask', strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  (0, 'sudo', 'block', strftime('%Y-%m-%dT%H:%M:%fZ', 'now'));
-- "Allow once": un singur run al agentului are voie la prefix
CREATE TABLE command_grants (
  id         INTEGER PRIMARY KEY,
  project_id INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  agent_id   INTEGER NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
  prefix     TEXT NOT NULL,
  created_at TEXT NOT NULL,
  used_at    TEXT
);
CREATE INDEX command_grants_open ON command_grants(agent_id, used_at);
-- cererile de acces pentru o comanda anume (prefixul regulii Ask); '' = cerere de tool
ALTER TABLE access_requests ADD COLUMN prefix TEXT NOT NULL DEFAULT '';
```

(Verifica formatul de timp folosit de `now()` in `store.go`; daca e altul decat RFC3339 cu milisecunde, potriveste
`strftime` cu el ca `parseTime` sa citeasca valorile.)

`commands.go`:

```go
package store

import "time"

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
		time.Now().UTC().Format(time.RFC3339Nano), agentID); err != nil {
		return nil, err
	}
	return out, tx.Commit()
}
```

(Foloseste `now()` din store in loc de `time.Now()` daca `now()` intoarce string; scoate importul `time` atunci.)

`access.go`: campul `Prefix string `json:"prefix"`` in `AccessRequest`, `prefix` in `accessCols` si in
`scanAccessRequest`; `RecordAccessDenial(..., tool, detail, prefix string)` cauta randul deschis dupa
`agent_id = ? AND tool = ? AND prefix = ? AND dismissed = 0` si il insereaza cu `prefix`.

`projects.go` `DeleteProject`: inainte de `DELETE FROM projects`, `DELETE FROM command_rules WHERE project_id = ?`
(in aceeasi tranzactie daca functia foloseste una; altfel un `Exec` inainte).

`store_test.go`: `if v != 22` -> `if v != 23`, mesajul `vreau 23`.

Apelurile existente `RecordAccessDenial(...)` (in `internal/team/run.go` si in teste: `grep -rn RecordAccessDenial`)
primesc `""` ca ultim argument (Task 3 schimba apelul din run.go).

- [ ] **Step 4: ruleaza**

Run: `go test ./internal/store/ && go build ./... && go vet ./...`
Expected: PASS

- [ ] **Step 5: commit**

```bash
git add internal/store internal/team/run.go
git commit -m "store: reguli pentru comenzi, grant-uri Allow once, prefix pe cereri (migrarea 023)"
```

### Task 2: prompt: potrivire, deny si linia din prompt

**Files:**
- Create: `internal/prompt/commands.go`
- Create: `internal/prompt/commands_test.go`
- Modify: `internal/prompt/settings.go` (`Settings` cu `cmdDeny`; `denyAlways` fara comenzile Bash; `AlwaysDeniedBash` cu reguli)
- Modify: `internal/prompt/settings_test.go`
- Modify: `internal/prompt/prompt.go` (`Links.BlockedCommands`, linia in „How you work”)
- Modify: `internal/prompt/prompt_test.go`

**Interfaces:**
- Consumes: `store.CommandRule`, `store.CmdAllow/CmdAsk/CmdBlock` (Task 1).
- Produces:

```go
func NormalizePrefix(p string) (string, error)                              // validare + normalizare
func ValidateRules(rules []store.CommandRule) ([]store.CommandRule, error)  // normalizate, fara duplicate, stari valide
func EffectiveRules(global, project []store.CommandRule) []store.CommandRule // proiectul inlocuieste acelasi prefix; dupa prefix
func MatchCommand(command string, rules []store.CommandRule) (mode, prefix string) // "" , "" = nicio regula
func DenyPrefixes(rules []store.CommandRule, granted []string) []string     // prefixele Ask/Block, minus grant-urile
func BlockedPrefixes(rules []store.CommandRule) []string
func RuleWarnings(rules []store.CommandRule) []string
func Settings(tools []string, home string, extraAllow, cmdDeny []string) string
func AlwaysDeniedBash(command string, tools []string, rules []store.CommandRule) bool
// Links primeste: BlockedCommands []string (doar pentru agentii cu shell; setat in team/run.go)
```

- [ ] **Step 1: testele**

`internal/prompt/commands_test.go`:

```go
package prompt

import (
	"strings"
	"testing"

	"foreman/internal/store"
)

func rules(pairs ...string) []store.CommandRule {
	var out []store.CommandRule
	for i := 0; i < len(pairs); i += 2 {
		out = append(out, store.CommandRule{Prefix: pairs[i], Mode: pairs[i+1]})
	}
	return out
}

func TestNormalizePrefix(t *testing.T) {
	if p, err := NormalizePrefix("  git   push "); err != nil || p != "git push" {
		t.Fatalf("%q %v", p, err)
	}
	for _, bad := range []string{"", "   ", "git push; rm", "a && b", "echo $HOME", "x|y", "`id`", "a\nb", "rm *", `say "hi"`, strings.Repeat("a", 81)} {
		if _, err := NormalizePrefix(bad); err == nil {
			t.Fatalf("acceptat: %q", bad)
		}
	}
}

func TestValidateRules(t *testing.T) {
	if _, err := ValidateRules(rules("git push", "ask", "git  push", "allow")); err == nil {
		t.Fatal("duplicat dupa normalizare acceptat")
	}
	if _, err := ValidateRules(rules("git push", "maybe")); err == nil {
		t.Fatal("stare necunoscuta acceptata")
	}
	got, err := ValidateRules(rules(" docker ", "block"))
	if err != nil || got[0].Prefix != "docker" {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestEffectiveAndMatch(t *testing.T) {
	global := rules("git push", "ask", "rm -rf", "ask", "sudo", "block", "git", "allow")
	project := rules("git push", "allow", "docker", "block")
	eff := EffectiveRules(global, project)
	cases := []struct{ cmd, mode, prefix string }{
		{"git push origin main", "allow", "git push"}, // proiectul a inlocuit regula globala
		{"git status", "allow", "git"},
		{"git pushx", "allow", "git"}, // cuvinte intregi: "git push" nu prinde "git pushx"
		{"echo ok && sudo rm x", "block", "sudo"}, // comanda legata: bucata a doua
		{"ls ; rm -rf build", "ask", "rm -rf"},
		{"docker ps | grep x", "block", "docker"},
		{"ls -la", "", ""},
	}
	for _, c := range cases {
		mode, prefix := MatchCommand(c.cmd, eff)
		if mode != c.mode || prefix != c.prefix {
			t.Errorf("%q -> %s %s, vreau %s %s", c.cmd, mode, prefix, c.mode, c.prefix)
		}
	}
	// Block castiga peste Ask intre bucati
	if mode, _ := MatchCommand("rm -rf x && sudo y", eff); mode != "block" {
		t.Fatalf("mode = %s", mode)
	}
}

func TestDenyBlockedWarnings(t *testing.T) {
	eff := rules("git push", "ask", "sudo", "block", "git", "block", "git status", "allow")
	deny := DenyPrefixes(eff, []string{"git push"})
	if strings.Join(deny, ",") != "git,sudo" {
		t.Fatalf("deny = %v", deny)
	}
	if strings.Join(BlockedPrefixes(eff), ",") != "git,sudo" {
		t.Fatalf("blocked = %v", BlockedPrefixes(eff))
	}
	w := RuleWarnings(eff)
	if len(w) != 1 || !strings.Contains(w[0], "git status") || !strings.Contains(w[0], `"git"`) {
		t.Fatalf("warnings = %v", w)
	}
}
```

In `settings_test.go` adauga:

```go
func TestSettingsCommandDeny(t *testing.T) {
	got := Settings([]string{"shell"}, "/w", nil, []string{"git push", "sudo"})
	for _, want := range []string{`"Bash(git push:*)"`, `"Bash(sudo:*)"`, `"TaskCreate"`, `"Bash"`} {
		if !strings.Contains(got, want) {
			t.Fatalf("lipseste %s din %s", want, got)
		}
	}
	if strings.Contains(got, "rm -rf") {
		t.Fatal("rm -rf nu mai e fix: vine doar din reguli")
	}
}
```

si actualizeaza testele existente care asteptau `Bash(git push:*)` / `Bash(rm -rf:*)` din lista fixa: dau acum
prefixele prin `cmdDeny`. Testele pentru `AlwaysDeniedBash` primesc reguli:

```go
func TestAlwaysDeniedBashWithRules(t *testing.T) {
	eff := []store.CommandRule{{Prefix: "git push", Mode: store.CmdAsk}, {Prefix: "sudo", Mode: store.CmdBlock}}
	if !AlwaysDeniedBash("sudo ls", []string{"shell"}, eff) || !AlwaysDeniedBash("git push", nil, eff) {
		t.Fatal("regulile Ask/Block trebuie sa conteze")
	}
	if AlwaysDeniedBash("ls", nil, eff) || !AlwaysDeniedBash("npm install x", nil, eff) || AlwaysDeniedBash("npm install x", []string{"packages"}, eff) {
		t.Fatal("pachetele raman legate de tool-ul packages")
	}
}
```

In `prompt_test.go`:

```go
func TestSystemBlockedCommands(t *testing.T) {
	p := store.Project{Name: "Shop"}
	a := store.Agent{Name: "Rook", Tools: []string{"shell"}}
	got := System(p, a, Links{BossName: "Lead", BlockedCommands: []string{"sudo", "docker"}})
	if !strings.Contains(got, "These commands are blocked in this project; do not try them: sudo, docker.") {
		t.Fatal(got)
	}
	if strings.Contains(System(p, a, Links{BossName: "Lead"}), "blocked in this project") {
		t.Fatal("fara comenzi blocate nu apare linia")
	}
}
```

- [ ] **Step 2: ruleaza, trebuie sa pice**

Run: `go test ./internal/prompt/`
Expected: FAIL (functiile nu exista).

- [ ] **Step 3: implementarea**

`internal/prompt/commands.go`:

```go
package prompt

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"foreman/internal/store"
)

// forbiddenPrefixChars: caracterele de shell care ar face din prefix altceva decat inceputul unei comenzi.
const forbiddenPrefixChars = "&|;$`()<>*\"'\n\r"

// NormalizePrefix curata spatiile si valideaza un prefix de comanda.
func NormalizePrefix(p string) (string, error) {
	if strings.ContainsAny(p, forbiddenPrefixChars) {
		return "", fmt.Errorf("%q: use only words separated by spaces (no & | ; $ ` ( ) < > * or quotes)", strings.TrimSpace(p))
	}
	p = strings.Join(strings.Fields(p), " ")
	if p == "" {
		return "", errors.New("the command can't be empty")
	}
	if len([]rune(p)) > 80 {
		return "", fmt.Errorf("%q: at most 80 characters", p)
	}
	return p, nil
}

// ValidateRules normalizeaza prefixele si refuza duplicatele si starile necunoscute.
func ValidateRules(rules []store.CommandRule) ([]store.CommandRule, error) {
	out := make([]store.CommandRule, 0, len(rules))
	seen := map[string]bool{}
	for _, r := range rules {
		p, err := NormalizePrefix(r.Prefix)
		if err != nil {
			return nil, err
		}
		if r.Mode != store.CmdAllow && r.Mode != store.CmdAsk && r.Mode != store.CmdBlock {
			return nil, fmt.Errorf("%q: the rule must be allow, ask or block", p)
		}
		if seen[p] {
			return nil, fmt.Errorf("%q appears twice", p)
		}
		seen[p] = true
		out = append(out, store.CommandRule{Prefix: p, Mode: r.Mode})
	}
	return out, nil
}

// EffectiveRules: regulile globale, cu cele ale proiectului peste ele (acelasi prefix = al proiectului).
func EffectiveRules(global, project []store.CommandRule) []store.CommandRule {
	by := map[string]string{}
	for _, r := range global {
		by[r.Prefix] = r.Mode
	}
	for _, r := range project {
		by[r.Prefix] = r.Mode
	}
	out := make([]store.CommandRule, 0, len(by))
	for p, m := range by {
		out = append(out, store.CommandRule{Prefix: p, Mode: m})
	}
	slices.SortFunc(out, func(a, b store.CommandRule) int { return strings.Compare(a.Prefix, b.Prefix) })
	return out
}

// matchPart: regula cu cel mai lung prefix pentru o singura comanda (cuvinte intregi).
func matchPart(cmd string, rules []store.CommandRule) (store.CommandRule, bool) {
	var best store.CommandRule
	found := false
	for _, r := range rules {
		if cmd == r.Prefix || strings.HasPrefix(cmd, r.Prefix+" ") {
			if !found || len(r.Prefix) > len(best.Prefix) {
				best, found = r, true
			}
		}
	}
	return best, found
}

// MatchCommand intoarce starea si prefixul regulii care decide comanda (Block > Ask > Allow intre bucati);
// "", "" daca nicio regula nu se potriveste.
func MatchCommand(command string, rules []store.CommandRule) (mode, prefix string) {
	rank := map[string]int{store.CmdAllow: 1, store.CmdAsk: 2, store.CmdBlock: 3}
	parts := strings.FieldsFunc(command, func(r rune) bool { return r == '&' || r == '|' || r == ';' })
	for _, part := range parts {
		cmd := strings.Join(strings.Fields(part), " ")
		if r, ok := matchPart(cmd, rules); ok && rank[r.Mode] > rank[mode] {
			mode, prefix = r.Mode, r.Prefix
		}
	}
	return mode, prefix
}

// DenyPrefixes: prefixele Ask si Block, fara cele cu grant (Allow once), sortate.
func DenyPrefixes(rules []store.CommandRule, granted []string) []string {
	out := []string{}
	for _, r := range rules {
		if (r.Mode == store.CmdAsk || r.Mode == store.CmdBlock) && !slices.Contains(granted, r.Prefix) {
			out = append(out, r.Prefix)
		}
	}
	slices.Sort(out)
	return out
}

// BlockedPrefixes: prefixele pe Block (pentru linia din prompt), sortate.
func BlockedPrefixes(rules []store.CommandRule) []string {
	out := []string{}
	for _, r := range rules {
		if r.Mode == store.CmdBlock {
			out = append(out, r.Prefix)
		}
	}
	slices.Sort(out)
	return out
}

// RuleWarnings: un Allow mai lung sub un Ask/Block mai scurt nu poate fi respectat (in claude deny castiga).
func RuleWarnings(rules []store.CommandRule) []string {
	out := []string{}
	for _, a := range rules {
		if a.Mode != store.CmdAllow {
			continue
		}
		for _, d := range rules {
			if d.Mode != store.CmdAllow && strings.HasPrefix(a.Prefix, d.Prefix+" ") {
				out = append(out, fmt.Sprintf("%q can't be allowed while %q is %s: the shorter rule wins.", a.Prefix, d.Prefix, d.Mode))
			}
		}
	}
	return out
}
```

`settings.go`:
- `denyAlways` ramane doar cu `"TaskCreate", "TaskUpdate", "TaskList", "TaskGet", "TodoWrite", "Agent"` (comenzile
  Bash vin acum din reguli); comentariul se actualizeaza.
- `Settings(tools []string, home string, extraAllow, cmdDeny []string) string`: deny = `denyFor(tools)` +
  `"Bash(" + p + ":*)"` pentru fiecare `p` din `cmdDeny`, pentru orice agent (la cei fara Bash nu schimba nimic,
  dar tine regula simpla).
- `AlwaysDeniedBash(command string, tools []string, rules []store.CommandRule) bool`: adevarat daca
  `bashMatches(command, denyFor(tools))` sau `MatchCommand(command, rules)` intoarce `ask` / `block`.
- Importa `foreman/internal/store` (verifica sa nu creeze un ciclu: `prompt` importa deja `store` in `prompt.go`).

`prompt.go`: in `Links` campul `BlockedCommands []string // comenzile pe Block (doar agentii cu shell; setat in team/run.go)`;
in `System`, dupa calculul lui `how` si inainte de `section(&b, "How you work", ...)`:

```go
	if len(l.BlockedCommands) > 0 {
		how = append(how, "These commands are blocked in this project; do not try them: "+strings.Join(l.BlockedCommands, ", ")+".")
	}
```

Apelul `prompt.Settings(...)` din `internal/team/run.go` primeste temporar `nil` ca ultim argument (Task 3 il
completeaza). Apelul `prompt.AlwaysDeniedBash` din `internal/web/api_access.go` primeste temporar `nil` (Task 4).

- [ ] **Step 4: ruleaza**

Run: `go test ./internal/prompt/ && go build ./... && go vet ./...`
Expected: PASS

- [ ] **Step 5: commit**

```bash
git add internal/prompt internal/team/run.go internal/web/api_access.go
git commit -m "prompt: potrivirea regulilor pentru comenzi, deny din reguli, comenzile blocate in prompt"
```

### Task 3: team: regulile la run, refuzurile pe Ask si deciziile utilizatorului

**Files:**
- Create: `internal/team/commands.go`
- Create: `internal/team/commands_test.go`
- Modify: `internal/team/run.go` (`startRun`: reguli efective, grant-uri, `BlockedCommands`; `args` cu `cmdDeny`; `recordDenials` cu prefix)

**Interfaces:**
- Consumes: Task 1 (`CommandRules`, `TakeCommandGrants`, `AddCommandGrant`, `SetCommandRules`, `RecordAccessDenial` cu prefix, `GetAccessRequest`, `DismissAccessRequest`), Task 2 (`EffectiveRules`, `MatchCommand`, `DenyPrefixes`, `BlockedPrefixes`, `Settings`).
- Produces:

```go
func (t *Team) EffectiveCommandRules(projectID int64) ([]store.CommandRule, error)
func (t *Team) AllowCommandOnce(accessID int64) error
func (t *Team) AllowCommandInProject(accessID int64) error
func (t *Team) BlockCommandInProject(accessID int64) error
var ErrNotCommandRequest = errors.New("this request is not about a command")
```

- [ ] **Step 1: testele**

`internal/team/commands_test.go`, cu fixture-ul din `fake_test.go` (`setup(t)`; `f.rook` are tool-uri: da-i
`shell` cu `f.st.UpdateAgent`; `f.fs.last(t, agent)` intoarce ultimul `runner.Spec`; vezi `library_test.go` pentru
cum se porneste un run si cum se citesc `--settings` / `--append-system-prompt` din `spec.Args`):

```go
package team

import (
	"strings"
	"testing"

	"foreman/internal/runner"
	"foreman/internal/store"
)

func argAfter(args []string, flag string) string {
	for i, a := range args {
		if a == flag && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}

func TestCommandRulesInRun(t *testing.T) {
	f := setup(t)
	f.rook.Tools = []string{"shell"}
	f.st.UpdateAgent(f.rook)
	f.st.SetCommandRules(f.p.ID, []store.CommandRule{{Prefix: "git push", Mode: store.CmdAllow}, {Prefix: "docker", Mode: store.CmdBlock}})
	f.task(t, "Ship", f.rook, store.StatusQueued)
	f.tm.Kick(f.p.ID) // sau apelul folosit de testele existente ca sa porneasca run-ul lui Rook
	spec := f.fs.last(t, f.rook)
	settings := argAfter(spec.Args, "--settings")
	if strings.Contains(settings, "git push") || !strings.Contains(settings, "Bash(docker:*)") || !strings.Contains(settings, "Bash(rm -rf:*)") {
		t.Fatalf("settings = %s", settings)
	}
	if !strings.Contains(argAfter(spec.Args, "--append-system-prompt"), "do not try them: docker, sudo.") {
		t.Fatal("linia cu comenzile blocate lipseste")
	}
}

func TestAskDenialAllowOnceAndProject(t *testing.T) {
	f := setup(t)
	f.rook.Tools = []string{"shell"}
	f.st.UpdateAgent(f.rook)
	task := f.task(t, "Ship", f.rook, store.StatusInProgress)
	ar := activeRun{projectID: f.p.ID, taskID: task.ID}
	f.tm.work.Lock()
	f.tm.recordDenials(ar, f.rook, []runner.Denial{
		{Tool: "Bash", Detail: "echo ok && git push origin main"},
		{Tool: "Bash", Detail: "sudo rm x"},
	})
	f.tm.work.Unlock()
	reqs, _ := f.st.ListOpenAccessRequests(f.p.ID)
	if len(reqs) != 1 || reqs[0].Prefix != "git push" {
		t.Fatalf("doar git push (Ask) face cerere, sudo (Block) nu: %+v", reqs)
	}
	if err := f.tm.AllowCommandOnce(reqs[0].ID); err != nil {
		t.Fatal(err)
	}
	pending, _ := f.st.Undelivered(f.rook.ID)
	if len(pending) == 0 || !strings.Contains(pending[len(pending)-1].Text, "allowed once") {
		t.Fatalf("Rook nu a fost trezit: %+v", pending)
	}
	if got, _ := f.st.GetTask(task.ID); got.Status != store.StatusQueued && got.Status != store.StatusInProgress {
		t.Fatalf("status = %s", got.Status)
	}
	if r, _ := f.st.ListOpenAccessRequests(f.p.ID); len(r) != 0 {
		t.Fatal("cererea a ramas deschisa")
	}
	// grant-ul: Kick poate porni deja run-ul lui Rook (si consuma grant-ul); atunci --settings al acelui run
	// nu are git push in deny, altfel grant-ul e inca acolo
	if spec, ok := f.fs.lastOK(f.rook); ok {
		if strings.Contains(argAfter(spec.Args, "--settings"), "git push") {
			t.Fatal("run-ul de dupa Allow once are git push in deny")
		}
	} else if g, _ := f.st.TakeCommandGrants(f.rook.ID); len(g) != 1 || g[0] != "git push" {
		t.Fatalf("%v", g)
	}
	// Allow in this project scrie regula proiectului
	f.tm.work.Lock()
	f.tm.recordDenials(ar, f.rook, []runner.Denial{{Tool: "Bash", Detail: "git push"}})
	f.tm.work.Unlock()
	reqs, _ = f.st.ListOpenAccessRequests(f.p.ID)
	if err := f.tm.AllowCommandInProject(reqs[0].ID); err != nil {
		t.Fatal(err)
	}
	eff, _ := f.tm.EffectiveCommandRules(f.p.ID)
	for _, r := range eff {
		if r.Prefix == "git push" && r.Mode != store.CmdAllow {
			t.Fatalf("%+v", eff)
		}
	}
	// alt proiect ramane pe regula globala (Ask)
	other, _ := f.tm.EffectiveCommandRules(f.p.ID + 999)
	for _, r := range other {
		if r.Prefix == "git push" && r.Mode != store.CmdAsk {
			t.Fatalf("%+v", other)
		}
	}
}

func TestBlockCommandAndNonCommandRequest(t *testing.T) {
	f := setup(t)
	f.rook.Tools = []string{"shell"}
	f.st.UpdateAgent(f.rook)
	f.tm.work.Lock()
	f.tm.recordDenials(activeRun{projectID: f.p.ID}, f.rook, []runner.Denial{{Tool: "Bash", Detail: "rm -rf build"}})
	f.tm.work.Unlock()
	reqs, _ := f.st.ListOpenAccessRequests(f.p.ID)
	if err := f.tm.BlockCommandInProject(reqs[0].ID); err != nil {
		t.Fatal(err)
	}
	proj, _ := f.st.CommandRules(f.p.ID)
	if len(proj) != 1 || proj[0].Prefix != "rm -rf" || proj[0].Mode != store.CmdBlock {
		t.Fatalf("%+v", proj)
	}
	// cerere de tool (agent fara Shell): actiunile pe comanda o refuza
	f.vera.Tools = nil
	f.st.UpdateAgent(f.vera)
	f.st.RecordAccessDenial(f.p.ID, f.vera.ID, nil, "Bash", "ls", "")
	reqs, _ = f.st.ListOpenAccessRequests(f.p.ID)
	if err := f.tm.AllowCommandOnce(reqs[len(reqs)-1].ID); err != ErrNotCommandRequest {
		t.Fatalf("err = %v", err)
	}
}
```

(Verifica numele din fixture: `f.p`, `f.rook`, `f.vera`, `f.fs.last`, `f.wantStatus`, `f.task`; `f.fs.lastOK(agent)`
e un helper nou, mic, in `fake_test.go`: ultimul spec al agentului si `true`, sau `false` daca n-a pornit niciun run;
dupa Allow once task-ul poate fi deja In progress din nou daca run-ul a pornit, deci `wantStatus` accepta Queued sau
In progress in acel caz; `runner.Denial`
are campurile `Tool`, `Detail`; adapteaza daca difera. Daca `recordDenials` cere si alte campuri din `activeRun`,
completeaza-le.)

- [ ] **Step 2: ruleaza, trebuie sa pice**

Run: `go test ./internal/team/ -run 'TestCommandRulesInRun|TestAskDenial|TestBlockCommand'`
Expected: FAIL.

- [ ] **Step 3: implementarea**

`internal/team/commands.go`:

```go
package team

import (
	"errors"
	"fmt"
	"slices"

	"foreman/internal/prompt"
	"foreman/internal/store"
)

var ErrNotCommandRequest = errors.New("this request is not about a command")

// EffectiveCommandRules: regulile globale cu override-urile proiectului.
func (t *Team) EffectiveCommandRules(projectID int64) ([]store.CommandRule, error) {
	global, err := t.st.CommandRules(0)
	if err != nil {
		return nil, err
	}
	project, err := t.st.CommandRules(projectID)
	if err != nil {
		return nil, err
	}
	return prompt.EffectiveRules(global, project), nil
}

func (t *Team) AllowCommandOnce(accessID int64) error {
	return t.decideCommand(accessID, func(r store.AccessRequest) (string, error) {
		return "allowed once", t.st.AddCommandGrant(r.ProjectID, r.AgentID, r.Prefix)
	}, true)
}

func (t *Team) AllowCommandInProject(accessID int64) error {
	return t.decideCommand(accessID, func(r store.AccessRequest) (string, error) {
		return "allowed in this project", t.setProjectRule(r.ProjectID, r.Prefix, store.CmdAllow)
	}, true)
}

func (t *Team) BlockCommandInProject(accessID int64) error {
	return t.decideCommand(accessID, func(r store.AccessRequest) (string, error) {
		return "", t.setProjectRule(r.ProjectID, r.Prefix, store.CmdBlock)
	}, false)
}

// setProjectRule pune (sau inlocuieste) o regula a proiectului, pastrand restul.
func (t *Team) setProjectRule(projectID int64, prefix, mode string) error {
	rules, err := t.st.CommandRules(projectID)
	if err != nil {
		return err
	}
	rules = slices.DeleteFunc(rules, func(r store.CommandRule) bool { return r.Prefix == prefix })
	rules = append(rules, store.CommandRule{Prefix: prefix, Mode: mode})
	return t.st.SetCommandRules(projectID, rules)
}

// decideCommand aplica decizia pe o cerere de comanda, o inchide si, la Allow, trezeste agentul
// (mesaj nelivrat) si pune task-ul inapoi in coada.
func (t *Team) decideCommand(accessID int64, apply func(store.AccessRequest) (string, error), wake bool) error {
	t.work.Lock()
	r, err := t.st.GetAccessRequest(accessID)
	if err != nil {
		t.work.Unlock()
		return err
	}
	if r.Prefix == "" {
		t.work.Unlock()
		return ErrNotCommandRequest
	}
	how, err := apply(r)
	if err != nil {
		t.work.Unlock()
		return err
	}
	if err := t.st.DismissAccessRequest(r.ID); err != nil {
		t.work.Unlock()
		return err
	}
	if wake {
		a, err := t.st.GetAgent(r.AgentID)
		if err != nil {
			t.work.Unlock()
			return err
		}
		if r.TaskID != nil {
			if err := t.requeueTaskFor(*r.TaskID, a); err != nil {
				t.work.Unlock()
				return err
			}
		}
		msg := fmt.Sprintf("You may now run %q (%s by the user). Run it again if you still need it.", r.Detail, how)
		if _, err := t.addUserMessage(a, clip(msg), nil, true, false); err != nil {
			t.work.Unlock()
			return err
		}
	}
	t.emit(r.ProjectID, "access", map[string]int64{"agentId": r.AgentID})
	t.work.Unlock()
	t.Kick(r.ProjectID)
	return nil
}

// requeueTaskFor pune task-ul in coada daca e In progress la agent, nu ruleaza chiar acum si nu asteapta
// o intrebare deschisa (aceleasi conditii ca requeueAnswered). Se apeleaza cu t.work blocat.
func (t *Team) requeueTaskFor(taskID int64, a store.Agent) error {
	if t.activeTaskID(a.ID) == taskID {
		return nil
	}
	task, err := t.st.GetTask(taskID)
	if errors.Is(err, store.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if task.Status != store.StatusInProgress || task.AssigneeID != a.ID {
		return nil
	}
	if waiting, err := t.st.HasOpenQuestionForTask(task.ID); err != nil || waiting {
		return err
	}
	task.Status = store.StatusQueued
	if err := t.st.SaveTask(&task); err != nil {
		return err
	}
	t.emit(task.ProjectID, "task", task)
	return nil
}
```

(Verifica semnatura actuala a lui `addUserMessage` in `reconcile.go`: dupa Left for you are 5 argumente
`(a, text, attachmentIDs, reply, skipParent)`; foloseste `skipParent = false`. Daca `requeueAnswered` poate fi
rescris sa cheme `requeueTaskFor`, fa-o, ca sa nu existe doua copii ale acelorasi conditii.)

`run.go`:
- In `startRun`, inainte de `prompt.LinksFor`: `rules, err := t.EffectiveCommandRules(p.ID)` (eroare -> return err);
  `granted, err := t.st.TakeCommandGrants(a.ID)` (eroare -> log si `granted = nil`); dupa `pl := prompt.LinksFor(...)`:
  `if slices.Contains(a.Tools, "shell") { pl.BlockedCommands = prompt.BlockedPrefixes(rules) }`.
  `cmdDeny := prompt.DenyPrefixes(rules, granted)` transmis lui `t.args`.
- `args(..., lb library.Build, cmdDeny []string)` foloseste `prompt.Settings(a.Tools, home, lb.Allow, cmdDeny)`.
- `recordDenials`: la inceput `rules, _ := t.EffectiveCommandRules(ar.projectID)`; pentru un refuz `d.Tool == "Bash"`
  al unui agent cu `shell`: `mode, prefix := prompt.MatchCommand(d.Detail, rules)`;
  `block` -> nu se creeaza cerere (doar evenimentul din istoricul task-ului, ca azi); `ask` -> cerere cu `prefix`;
  altfel (nicio regula) -> cerere de tool, ca azi (`prefix = ""`). Cheia de deduplicare `seen` devine `d.Tool+"|"+prefix`.
  Pentru agentii fara `shell` comportamentul ramane neschimbat (`prefix = ""`).

- [ ] **Step 4: ruleaza**

Run: `go test -race ./internal/team/ ./internal/prompt/ ./internal/store/ && go vet ./...`
Expected: PASS

- [ ] **Step 5: commit**

```bash
git add internal/team
git commit -m "team: regulile pentru comenzi la fiecare run; refuzurile pe Ask devin cereri; Allow once / in project / Block"
```

### Task 4: API

**Files:**
- Create: `internal/web/api_commands.go`
- Create: `internal/web/commands_test.go`
- Modify: `internal/web/server.go` (rute)
- Modify: `internal/web/api_access.go` (`accessView.Prefix`; `CanEnableShell` cu reguli)

**Interfaces:**
- Consumes: Task 1-3.
- Produces:

```
GET  /api/settings/commands                     -> 200 [{prefix, mode}]
PUT  /api/settings/commands  [{prefix, mode}]   -> 204 | 400 {error}
POST /api/settings/commands/reset               -> 204
GET  /api/projects/{id}/commands                -> 200 {global, project, effective, warnings}
PUT  /api/projects/{id}/commands [{prefix, mode}] -> 204 | 400
POST /api/access/{id}/allow-once                -> 204 | 409 (nu e cerere de comanda)
POST /api/access/{id}/allow-project             -> 204 | 409
POST /api/access/{id}/block-project             -> 204 | 409
accessView: + prefix string
```

- [ ] **Step 1: testul**

`internal/web/commands_test.go` (helper-ele `setup`, `e.do`, `e.createProject`, `e.addWorker` din `server_test.go`):

```go
package web

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"foreman/internal/store"
)

func TestCommandSettingsAPI(t *testing.T) {
	e := setup(t)
	code, raw := e.do(t, "GET", "/api/settings/commands", nil)
	var global []store.CommandRule
	json.Unmarshal(raw, &global)
	if code != http.StatusOK || len(global) != 4 {
		t.Fatalf("%d %s", code, raw)
	}
	for _, bad := range []any{
		[]map[string]string{{"prefix": "git push; rm", "mode": "ask"}},
		[]map[string]string{{"prefix": "", "mode": "ask"}},
		[]map[string]string{{"prefix": strings.Repeat("a", 81), "mode": "ask"}},
		[]map[string]string{{"prefix": "docker", "mode": "maybe"}},
	} {
		if code, _ := e.do(t, "PUT", "/api/settings/commands", bad); code != http.StatusBadRequest {
			t.Fatalf("acceptat %v: %d", bad, code)
		}
	}
	if _, raw := e.do(t, "GET", "/api/settings/commands", nil); !strings.Contains(string(raw), "git push") {
		t.Fatal("o cerere invalida a schimbat lista")
	}
	if code, _ := e.do(t, "PUT", "/api/settings/commands", []map[string]string{{"prefix": " docker ", "mode": "block"}}); code != http.StatusNoContent {
		t.Fatalf("%d", code)
	}
	e.do(t, "POST", "/api/settings/commands/reset", nil)

	p := e.createProject(t, t.TempDir())
	path := "/api/projects/" + strconv.FormatInt(p.ID, 10) + "/commands"
	e.do(t, "PUT", path, []map[string]string{{"prefix": "git", "mode": "block"}, {"prefix": "git status", "mode": "allow"}})
	_, raw = e.do(t, "GET", path, nil)
	var d struct {
		Effective []store.CommandRule `json:"effective"`
		Warnings  []string            `json:"warnings"`
	}
	json.Unmarshal(raw, &d)
	if len(d.Effective) != 6 || len(d.Warnings) != 1 {
		t.Fatalf("%s", raw)
	}
}

func TestAccessCommandActionsAPI(t *testing.T) {
	e := setup(t)
	p := e.createProject(t, t.TempDir())
	w := e.addWorker(t, p.ID, "Rook")
	w.Tools = []string{"shell"}
	e.st.UpdateAgent(w)
	e.st.RecordAccessDenial(p.ID, w.ID, nil, "Bash", "git push origin main", "git push")
	e.st.RecordAccessDenial(p.ID, w.ID, nil, "Bash", "ls", "")
	reqs, _ := e.st.ListOpenAccessRequests(p.ID)
	cmdID, toolID := strconv.FormatInt(reqs[0].ID, 10), strconv.FormatInt(reqs[1].ID, 10)
	if code, _ := e.do(t, "POST", "/api/access/"+toolID+"/allow-once", nil); code != http.StatusConflict {
		t.Fatalf("cerere de tool: %d", code)
	}
	if code, raw := e.do(t, "POST", "/api/access/"+cmdID+"/allow-project", nil); code != http.StatusNoContent {
		t.Fatalf("%d %s", code, raw)
	}
	_, raw := e.do(t, "GET", "/api/projects/"+strconv.FormatInt(p.ID, 10), nil)
	if strings.Contains(string(raw), `"prefix":"git push"`) {
		t.Fatal("cererea a ramas in Needs you")
	}
	// agent fara Shell refuzat la sudo (Block): "Enable Shell" nu mai e oferit
	v := e.addWorker(t, p.ID, "Vera")
	e.st.RecordAccessDenial(p.ID, v.ID, nil, "Bash", "sudo ls", "")
	_, raw = e.do(t, "GET", "/api/projects/"+strconv.FormatInt(p.ID, 10), nil)
	if strings.Contains(string(raw), `"canEnableShell":true`) {
		t.Fatalf("Enable Shell oferit pentru o comanda blocata: %s", raw)
	}
}
```

(Verifica: `e.st` exista in `env`; altfel foloseste helper-ul existent pentru store. Daca `do` cu body slice merge.)

- [ ] **Step 2: ruleaza, trebuie sa pice**

Run: `go test ./internal/web/ -run 'TestCommandSettingsAPI|TestAccessCommandActionsAPI'`
Expected: FAIL (rute lipsa).

- [ ] **Step 3: implementarea**

`api_commands.go`:

```go
package web

import (
	"errors"
	"net/http"

	"foreman/internal/prompt"
	"foreman/internal/store"
	"foreman/internal/team"
)

func (s *Server) getGlobalCommands(w http.ResponseWriter, r *http.Request) {
	rules, err := s.st.CommandRules(0)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, rules)
}

func (s *Server) putGlobalCommands(w http.ResponseWriter, r *http.Request) {
	s.putCommands(w, r, 0)
}

func (s *Server) resetGlobalCommands(w http.ResponseWriter, r *http.Request) {
	if err := s.st.ResetGlobalCommandRules(); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// putCommands valideaza si inlocuieste lista unui nivel (0 = global).
func (s *Server) putCommands(w http.ResponseWriter, r *http.Request, projectID int64) {
	var in []store.CommandRule
	if err := decode(r, &in); err != nil {
		fail(w, err)
		return
	}
	rules, err := prompt.ValidateRules(in)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.st.SetCommandRules(projectID, rules); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) getProjectCommands(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		fail(w, err)
		return
	}
	if _, err := s.st.GetProject(id); err != nil {
		fail(w, err)
		return
	}
	global, err := s.st.CommandRules(0)
	if err != nil {
		fail(w, err)
		return
	}
	project, err := s.st.CommandRules(id)
	if err != nil {
		fail(w, err)
		return
	}
	eff := prompt.EffectiveRules(global, project)
	writeJSON(w, http.StatusOK, map[string]any{"global": global, "project": project, "effective": eff, "warnings": prompt.RuleWarnings(eff)})
}

func (s *Server) putProjectCommands(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		fail(w, err)
		return
	}
	if _, err := s.st.GetProject(id); err != nil {
		fail(w, err)
		return
	}
	s.putCommands(w, r, id)
}

func (s *Server) accessCommand(w http.ResponseWriter, r *http.Request, act func(int64) error) {
	req, err := s.getAccess(r)
	if err != nil {
		fail(w, err)
		return
	}
	if err := act(req.ID); err != nil {
		if errors.Is(err, team.ErrNotCommandRequest) {
			writeErr(w, http.StatusConflict, err.Error())
			return
		}
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) allowCommandOnce(w http.ResponseWriter, r *http.Request) {
	s.accessCommand(w, r, s.team.AllowCommandOnce)
}
func (s *Server) allowCommandProject(w http.ResponseWriter, r *http.Request) {
	s.accessCommand(w, r, s.team.AllowCommandInProject)
}
func (s *Server) blockCommandProject(w http.ResponseWriter, r *http.Request) {
	s.accessCommand(w, r, s.team.BlockCommandInProject)
}
```

Rute in `server.go`, langa cele `/api/access`:

```go
	mux.HandleFunc("GET /api/settings/commands", s.getGlobalCommands)
	mux.HandleFunc("PUT /api/settings/commands", s.putGlobalCommands)
	mux.HandleFunc("POST /api/settings/commands/reset", s.resetGlobalCommands)
	mux.HandleFunc("GET /api/projects/{id}/commands", s.getProjectCommands)
	mux.HandleFunc("PUT /api/projects/{id}/commands", s.putProjectCommands)
	mux.HandleFunc("POST /api/access/{id}/allow-once", s.allowCommandOnce)
	mux.HandleFunc("POST /api/access/{id}/allow-project", s.allowCommandProject)
	mux.HandleFunc("POST /api/access/{id}/block-project", s.blockCommandProject)
```

`api_access.go`: `accessView` primeste `Prefix string `json:"prefix"``; `accessViews` calculeaza o data
`rules, err := s.team.EffectiveCommandRules(projectID)` si `CanEnableShell = r.Prefix == "" && r.Tool == "Bash" &&
!has(shell) && !prompt.AlwaysDeniedBash(r.Detail, a.Tools, rules)`; `CanEnablePackages` doar pentru `r.Prefix == ""`.
`enableTool` refuza cu 409 o cerere cu prefix (acolo nu ajuta un tool).

- [ ] **Step 4: ruleaza**

Run: `go test ./internal/web/ && go vet ./... && go build -race ./cmd/foreman`
Expected: PASS

- [ ] **Step 5: commit**

```bash
git add internal/web/api_commands.go internal/web/commands_test.go internal/web/server.go internal/web/api_access.go
git commit -m "web: API pentru regulile de comenzi si actiunile pe cererile de comanda"
```

### Task 5: UI: Settings, sectiunea din proiect, cardul din Needs you

**Files:**
- Create: `internal/web/static/js/commands.js` (editorul de reguli, folosit de Settings si de proiect)
- Create: `internal/web/static/js/settings.js` (pagina `#/settings`)
- Modify: `internal/web/static/js/app.js` (ruta `#/settings`)
- Modify: `internal/web/static/js/ui.js` (navigatia: `Projects | Library | Settings`)
- Modify: `internal/web/static/js/projects.js` (sectiunea „Commands” in formularul proiectului)
- Modify: `internal/web/static/js/access.js` (butoanele pentru cererile cu `prefix`)
- Modify: `internal/web/static/app.css`

**Interfaces:**
- Consumes: rutele din Task 4.

- [ ] **Step 1: editorul si pagina Settings**

`commands.js`:

```js
import { api } from "./api.js";
import { el, toast } from "./ui.js";

const MODES = [["allow", "Allow"], ["ask", "Ask"], ["block", "Block"]];

// modeSwitch: comutatorul Allow / Ask / Block; onChange(mode)
function modeSwitch(current, onChange, { inherit = null } = {}) {
  const opts = inherit ? [["", `Inherit (${inherit})`], ...MODES] : MODES;
  return el("div", { class: "seg", role: "group" }, opts.map(([v, label]) =>
    el("button", { type: "button", class: "seg-btn" + (v === current ? " active" : ""), "aria-pressed": String(v === current),
      onclick: () => onChange(v) }, label)));
}

// globalCommands: lista din Settings (salvare imediata a intregii liste).
export function globalCommands() {
  const box = el("section", { class: "section cmd-rules" });
  let rules = [];
  async function load() {
    try { rules = await api("/api/settings/commands"); draw(); } catch (err) { toast(err.message); }
  }
  async function save(next) {
    try { await api("/api/settings/commands", { method: "PUT", body: next }); rules = next; draw(); }
    catch (err) { toast(err.message); draw(); }
  }
  function draw() {
    const input = el("input", { type: "text", maxlength: 80, placeholder: "e.g. docker", "aria-label": "Command" });
    const add = () => { const v = input.value.trim(); if (v) save([...rules, { prefix: v, mode: "ask" }]); };
    input.addEventListener("keydown", (e) => { if (e.key === "Enter") { e.preventDefault(); add(); } });
    box.replaceChildren(
      el("h2", {}, "Commands"),
      el("p", { class: "hint" }, "Rules for terminal commands of agents with Shell. Allow runs it, Ask refuses it and asks you in Needs you, Block refuses it. Rules match how a command starts, so an agent with Shell can get around them (for example with bash -c): they are a safety net, not a sandbox."),
      el("div", { class: "cmd-table" }, rules.map((r) => el("div", { class: "cmd-row" },
        el("code", { class: "cmd-prefix" }, r.prefix),
        modeSwitch(r.mode, (m) => save(rules.map((x) => x.prefix === r.prefix ? { ...x, mode: m } : x))),
        el("button", { type: "button", class: "link-btn danger", "aria-label": `Remove ${r.prefix}`,
          onclick: () => save(rules.filter((x) => x.prefix !== r.prefix)) }, "Remove")))),
      el("div", { class: "cmd-add" }, input, el("button", { type: "button", class: "btn small", onclick: add }, "+ Add command"),
        el("span", { class: "grow" }),
        el("button", { type: "button", class: "link-btn", onclick: async () => {
          if (!confirm("Reset the command rules to the defaults?")) return;
          try { await api("/api/settings/commands/reset", { method: "POST" }); load(); } catch (err) { toast(err.message); }
        } }, "Reset to defaults")));
  }
  load();
  return box;
}

// projectCommands: regulile globale mostenite (Inherit / Allow / Ask / Block) + comenzile doar ale proiectului.
export function projectCommands(projectId) {
  const box = el("section", { class: "section cmd-rules" });
  let data = { global: [], project: [], warnings: [] };
  async function load() {
    try { data = await api(`/api/projects/${projectId}/commands`); draw(); } catch (err) { toast(err.message); }
  }
  async function save(next) {
    try { await api(`/api/projects/${projectId}/commands`, { method: "PUT", body: next }); load(); }
    catch (err) { toast(err.message); draw(); }
  }
  const setMode = (prefix, mode) => {
    const rest = data.project.filter((x) => x.prefix !== prefix);
    save(mode ? [...rest, { prefix, mode }] : rest);
  };
  function draw() {
    const own = data.project.filter((r) => !data.global.some((g) => g.prefix === r.prefix));
    const input = el("input", { type: "text", maxlength: 80, placeholder: "e.g. docker", "aria-label": "Command" });
    const add = () => { const v = input.value.trim(); if (v) setMode(v, "ask"); };
    input.addEventListener("keydown", (e) => { if (e.key === "Enter") { e.preventDefault(); add(); } });
    box.replaceChildren(
      el("h2", {}, "Commands"),
      el("p", { class: "hint" }, "Defaults come from Settings. Override them here for this project, or add commands only this project has."),
      el("div", { class: "cmd-table" },
        data.global.map((g) => {
          const o = data.project.find((x) => x.prefix === g.prefix);
          return el("div", { class: "cmd-row" }, el("code", { class: "cmd-prefix" }, g.prefix),
            modeSwitch(o ? o.mode : "", (m) => setMode(g.prefix, m), { inherit: g.mode }));
        }),
        own.map((r) => el("div", { class: "cmd-row" }, el("code", { class: "cmd-prefix" }, r.prefix),
          modeSwitch(r.mode, (m) => setMode(r.prefix, m)),
          el("button", { type: "button", class: "link-btn danger", onclick: () => setMode(r.prefix, "") }, "Remove")))),
      data.warnings.length ? el("ul", { class: "cmd-warnings" }, data.warnings.map((w) => el("li", {}, w))) : null,
      el("div", { class: "cmd-add" }, input, el("button", { type: "button", class: "btn small", onclick: add }, "+ Add command")));
  }
  load();
  return box;
}
```

`settings.js`:

```js
import { el, topbar } from "./ui.js";
import { globalCommands } from "./commands.js";

// settingsView: setarile aplicatiei (deocamdata regulile pentru comenzi).
export function settingsView(root) {
  root.replaceChildren(
    topbar("settings"),
    el("main", { class: "settings-page" },
      el("h1", {}, "Settings"),
      globalCommands()));
}
```

(Verifica semnatura lui `topbar` in `ui.js`: dupa Library primeste un string `"projects"` / `"library"` pentru
navigatia principala; adauga `["settings", "Settings", "#/settings"]` in lista din `ui.js` si potriveste apelul.
In `app.js` adauga ruta `{ pattern: /^#\/settings\/?$/, view: (root) => settingsView(root) }` in stilul rutelor
existente; verifica cum primesc view-urile `root`.)

- [ ] **Step 2: sectiunea din proiect si cardul din Needs you**

`projects.js`: in `form-side`, dupa `id && linksSection(...)`, adauga `id && projectCommands(id)` (importat din
`./commands.js`). Butoanele din `commands.js` au `type="button"`, deci nu trimit formularul proiectului; input-ul
are Enter cu `preventDefault`.

`access.js`: pentru `r.prefix`:

```js
  if (r.prefix) {
    return el("div", { class: "question access cmd-ask" },
      el("div", { class: "question-head" }, agent ? avatar(agent.name, kindOf(agent), 20) : null,
        el("span", { class: "label" }, "COMMAND · ASK"),
        el("strong", {}, `${name} wants to run`),
        r.count > 1 ? ` (${r.count}×)` : null,
        r.ref ? ` · ${r.ref}` : null),
      el("p", { class: "question-text" }, el("code", {}, r.detail || r.prefix), " · ",
        el("span", { class: "stuck-age", "data-since": r.updatedAt }, stuckAge(r.updatedAt)), " ago"),
      el("div", { class: "actions" },
        act("Allow once", "btn small primary", `/api/access/${r.id}/allow-once`),
        act(`Allow "${r.prefix}" in this project`, "btn small", `/api/access/${r.id}/allow-project`),
        act("Block", "btn small", `/api/access/${r.id}/block-project`),
        act("Dismiss", "btn small", `/api/access/${r.id}/dismiss`)));
  }
```

pus la inceputul lui `accessBox`, dupa definirea lui `act` (refoloseste helper-ul existent).

`app.css`:

```css
.settings-page { max-width: 880px; margin: 0 auto; padding: 28px 24px 48px; display: grid; gap: 20px; }
.cmd-table { display: grid; gap: 6px; }
.cmd-row { display: flex; align-items: center; gap: 12px; flex-wrap: wrap; padding: 6px 0; border-bottom: 1px solid var(--line); }
.cmd-prefix { flex: 1 1 160px; font-family: var(--mono); }
.seg { display: inline-flex; border: 1px solid var(--line-2); border-radius: 8px; overflow: hidden; }
.seg-btn { background: none; border: 0; padding: 5px 10px; color: var(--muted); font: inherit; font-size: 13px; cursor: pointer; }
.seg-btn.active { background: var(--col); color: var(--text); }
.cmd-add { display: flex; gap: 8px; align-items: center; flex-wrap: wrap; }
.cmd-add input { max-width: 260px; }
.cmd-warnings { color: var(--amber); font-size: 13px; margin: 0; padding-left: 18px; }
```

(Verifica ca `--mono`, `--line-2`, `--col`, `.link-btn.danger` exista; foloseste token-urile existente.)

- [ ] **Step 3: verificare**

`node --check` pe copii `.mjs` ale fisierelor JS schimbate (in scratchpad). Apoi pe o instanta separata
(`HOME=<scratchpad>/chome PATH=<scratchpad>/fakebin:$PATH ./foreman start --port 7799 --no-open`, `fakebin/claude`
doar asteapta): Settings arata cele 4 reguli si salveaza o schimbare; formularul unui proiect arata „Commands” cu
Inherit; o cerere cu prefix inserata cu sqlite3 (`INSERT INTO access_requests (..., prefix) ...`) apare in Needs you
cu cele 4 butoane, iar Allow once o inchide. Screenshot-uri cu Chrome headless prin CDP (1440x900 si 420x900), citite
si comparate. Opreste procesele pornite.

- [ ] **Step 4: commit**

```bash
git add internal/web/static
git commit -m "ui: Settings cu regulile pentru comenzi, Commands in proiect, cererile de comanda in Needs you"
```

### Task 6: documentatie

**Files:**
- Modify: `docs/guide/safety.md`, `docs/guide/needs-you.md`
- Modify: `CLAUDE.md` (Stare)
- Modify: `docs/superpowers/specs/2026-10-08-foreman-command-rules-design.md` (Status)

- [ ] **Step 1:** `safety.md`: sectiunea „Always blocked” devine „Command rules”: tabelul cu regulile implicite
  (git push, rm -rf, brew install = Ask; sudo = Block), unde se schimba (Settings si setarile proiectului), ce
  inseamna Allow / Ask / Block, ca regulile se aplica doar agentilor cu Shell, ca raman fixe tool-urile interne
  Claude Code, si limita (prefixe, `bash -c`). Scoate fraza „Approvals for risky actions are planned”.
- [ ] **Step 2:** `needs-you.md`: cererile „wants to run” cu Allow once / Allow in this project / Block / Dismiss.
- [ ] **Step 3:** `CLAUDE.md`, dupa linia „Gata (Library”: `Gata (reguli pentru comenzi, spec in docs/superpowers/specs/2026-10-08-foreman-command-rules-design.md): command_rules (globale + override pe proiect, migrarea 023), Allow / Ask / Block pe prefixe pentru agentii cu Shell, pagina Settings; refuzurile pe Ask ajung in Needs you cu Allow once (command_grants) / Allow in this project / Block.` In „Urmeaza”, punctul 3 devine „Aprobari live (--permission-prompt-tool), feed global "Live activity"”.
- [ ] **Step 4:** spec: `Status: implementat (2026-10-08).`
- [ ] **Step 5: commit** `docs: reguli pentru comenzi`.
