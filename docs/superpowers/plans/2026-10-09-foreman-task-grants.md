# Aprobari de comenzi pe task — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** O aprobare de comanda din "Needs you" tine pe tot task-ul (sau pe chat pana la New chat), merge si la agentii fara Shell si trezeste mereu agentul.

**Architecture:** `command_grants` primeste `task_id` si nu se mai consuma; la pornirea run-ului grant-urile task-ului (sau de chat) intra in `--settings` ca `Bash(<prefix>:*)` in allow si scot prefixele Ask din deny. Refuzurile Bash ale agentilor fara Shell se impart pe bucati in cereri pe prefix (`prompt.NoShellRequests`), cu un fel nou de cerere (`access_requests.kind = 'command'`). Enable Shell / Allow package installs trec in team si trezesc agentul.

**Tech Stack:** Go (stdlib, modernc.org/sqlite), JS vanilla in `internal/web/static/js`.

**Spec:** `docs/superpowers/specs/2026-10-09-foreman-task-grants-design.md`

## Global Constraints

- `go vet ./...` si `go build -race ./...` trebuie sa treaca; `go test ./...` verde.
- JS: copiaza fisierul ca `.mjs` si ruleaza `node --check` (pe `.js` nu prinde erorile de sintaxa).
- Cross-compile: `GOOS=darwin GOARCH=arm64 go build ./cmd/foreman` si `GOOS=windows go build ./cmd/foreman`.
- Tot ce vede utilizatorul si prompturile agentilor: engleza. Comentariile din cod: romana, fara diacritice.
- Dependente noi: niciuna.
- Block si refuzurile fixe (`denyFor`) nu pot fi ocolite de un grant: deny castiga in Claude Code.
- Commit-uri in stilul repo-ului: `zona: descriere scurta in romana`.

## Review Focus

1. Un grant pe task NU trebuie sa ajunga in run-urile agentului pe alt task sau in chat (si invers: grant-ul de chat nu intra pe task-uri) — test in Task 1.
2. O comanda compusa la agent fara Shell (`cd app && git stash push`) — bucatile deja permise nu fac cerere, restul fac cate o cerere; fara nicio bucata recunoscuta ramane cererea de tool (Enable Shell) — test in Task 2 (`TestNoShellRequests`).
3. Aprobarea unui prefix calculat care intre timp a ajuns pe Block (sau e un refuz fix, ex. `npm install` fara `packages`) e refuzata cu 409, nu creeaza grant — test in Task 3.
4. "Allow in this project" / "Block" pe o cerere `command` (agent fara Shell) intorc 409 cu mesaj clar, nu scriu o regula fara efect — test in Task 3.
5. Un task In progress cu o cerere de acces deschisa nu e impins de detectorul de blocaje (altfel agentul e refuzat iar) — test in Task 4.

---

### Task 1: Grant-uri pe task (store, run, Allow on T-N)

**Files:**
- Create: `internal/store/migrations/028_task_grants.sql`
- Modify: `internal/store/commands.go` (AddCommandGrant, CommandGrants inlocuieste TakeCommandGrants)
- Modify: `internal/store/access.go` (constante AccessTool/AccessAsk/AccessCommand, campul Kind, RecordCommandRequest, HasOpenAccessRequest)
- Modify: `internal/store/chat.go` (`newChatTx` sterge grant-urile de chat)
- Modify: `internal/team/run.go:154-186` (grant-urile la pornire, fara restaurare)
- Modify: `internal/team/commands.go` (AllowCommandOnce -> AllowCommandForTask)
- Modify: `internal/web/server.go:147`, `internal/web/api_commands.go:119-121` (ruta allow-task)
- Modify: `internal/web/static/js/access.js:30` (eticheta + ruta)
- Test: `internal/store/commands_test.go`, `internal/team/commands_test.go`, `internal/web/commands_test.go`

**Interfaces:**
- Produces:
  - `store.AccessTool = ""`, `store.AccessAsk = "ask"`, `store.AccessCommand = "command"`; `AccessRequest.Kind string` (json `kind`)
  - `(*Store) AddCommandGrant(projectID, agentID int64, taskID *int64, prefix string) error`
  - `(*Store) CommandGrants(agentID, taskID int64) ([]string, error)` — taskID 0 = grant-urile de chat
  - `(*Store) RecordCommandRequest(projectID, agentID int64, taskID *int64, detail, prefix string) error`
  - `(*Store) HasOpenAccessRequest(agentID, taskID int64) (bool, error)`
  - `(*Team) AllowCommandForTask(accessID int64) error`
  - `POST /api/access/{id}/allow-task` (inlocuieste `allow-once`)

- [ ] **Step 1: Migrarea**

`internal/store/migrations/028_task_grants.sql`:

```sql
-- Grant-urile din "Needs you" tin pe tot task-ul (task_id) sau pe chat (NULL, pana la New chat); nu se mai consuma.
DROP INDEX command_grants_open;
DELETE FROM command_grants WHERE used_at IS NOT NULL;
ALTER TABLE command_grants DROP COLUMN used_at;
ALTER TABLE command_grants ADD COLUMN task_id INTEGER REFERENCES tasks(id) ON DELETE CASCADE;
CREATE INDEX command_grants_agent ON command_grants(agent_id, task_id);

-- felul cererii de acces: '' = tool refuzat, 'ask' = comanda pe o regula Ask,
-- 'command' = comanda in afara tool-urilor unui agent fara Shell (prefix calculat)
ALTER TABLE access_requests ADD COLUMN kind TEXT NOT NULL DEFAULT '';
UPDATE access_requests SET kind = 'ask' WHERE prefix <> '';
```

- [ ] **Step 2: Testele de store (esueaza)**

In `internal/store/commands_test.go`, in `TestCommandRulesAndGrants` inlocuieste blocul `// grant-uri: luate o singura data` (cele doua `TakeCommandGrants`) cu:

```go
	// grant-uri: pe task sau pe chat (task 0), fara consum, fara duplicate
	task := Task{ProjectID: p.ID, Title: "t", AssigneeID: lead.ID}
	if err := s.CreateTask(&task); err != nil {
		t.Fatal(err)
	}
	s.AddCommandGrant(p.ID, lead.ID, &task.ID, "git push")
	s.AddCommandGrant(p.ID, lead.ID, &task.ID, "git push")
	s.AddCommandGrant(p.ID, lead.ID, nil, "git stash")
	for i := 0; i < 2; i++ {
		if g, _ := s.CommandGrants(lead.ID, task.ID); len(g) != 1 || g[0] != "git push" {
			t.Fatalf("task, citirea %d: %v", i, g)
		}
	}
	if g, _ := s.CommandGrants(lead.ID, 0); len(g) != 1 || g[0] != "git stash" {
		t.Fatalf("chat: %v", g)
	}
	if g, _ := s.CommandGrants(lead.ID, task.ID+999); len(g) != 0 {
		t.Fatalf("alt task: %v", g)
	}
	// New chat sterge doar grant-urile de chat
	if _, err := s.StartNewChat(lead.ID, "New chat started."); err != nil {
		t.Fatal(err)
	}
	if g, _ := s.CommandGrants(lead.ID, 0); len(g) != 0 {
		t.Fatalf("chat dupa New chat: %v", g)
	}
	if g, _ := s.CommandGrants(lead.ID, task.ID); len(g) != 1 {
		t.Fatalf("task dupa New chat: %v", g)
	}
```

Si adauga dupa blocul `// cereri: ...` (inainte de `DeleteProject`):

```go
	// felul cererii: Ask pentru prefix, tool fara prefix, command prin RecordCommandRequest
	s.RecordCommandRequest(p.ID, lead.ID, &task.ID, "git stash push -u", "git stash")
	s.RecordCommandRequest(p.ID, lead.ID, &task.ID, "git stash pop", "git stash")
	s.RecordAccessDenial(p.ID, lead.ID, nil, "WebFetch", "https://x.dev", "")
	reqs, _ = s.ListOpenAccessRequests(p.ID)
	kinds := map[string]string{}
	for _, r := range reqs {
		kinds[r.Tool+"|"+r.Prefix] = r.Kind
	}
	if kinds["Bash|git push"] != AccessAsk || kinds["Bash|git stash"] != AccessCommand || kinds["WebFetch|"] != AccessTool || len(reqs) != 4 {
		t.Fatalf("%+v", reqs)
	}
	if ok, _ := s.HasOpenAccessRequest(lead.ID, task.ID); !ok {
		t.Fatal("cererea pe task nu e gasita")
	}
	if ok, _ := s.HasOpenAccessRequest(lead.ID, task.ID+999); ok {
		t.Fatal("cerere pe alt task")
	}
```

- [ ] **Step 3: Ruleaza, verifica ca esueaza**

Run: `go test ./internal/store/ -run TestCommandRulesAndGrants`
Expected: FAIL la compilare (`CommandGrants`, `RecordCommandRequest`, `HasOpenAccessRequest`, `AccessAsk` nedefinite).

- [ ] **Step 4: Implementarea in store**

`internal/store/commands.go` — inlocuieste `AddCommandGrant` si `TakeCommandGrants`:

```go
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
```

`internal/store/access.go`:
- dupa `import`, adauga:

```go
// Felurile unei cereri de acces (access_requests.kind).
const (
	AccessTool    = ""        // un tool refuzat (Bash fara Shell, WebFetch...): Enable Shell / packages
	AccessAsk     = "ask"     // o comanda pe o regula Ask: prefixul regulii
	AccessCommand = "command" // o comanda in afara tool-urilor unui agent fara Shell: prefix calculat
)
```

- in `AccessRequest` adauga dupa `Prefix`: `Kind string `json:"kind"` // AccessTool / AccessAsk / AccessCommand`
- `accessCols` devine `id, project_id, agent_id, task_id, tool, detail, prefix, kind, count, dismissed, created_at, updated_at`; in `scanAccessRequest` scaneaza `&r.Kind` dupa `&r.Prefix`.
- `RecordAccessDenial` devine un wrapper, iar logica merge in `recordAccess` cu `kind` in cautare si insert:

```go
// RecordAccessDenial noteaza un refuz de tool (prefix "") sau de comanda pe o regula Ask (prefixul regulii).
func (s *Store) RecordAccessDenial(projectID, agentID int64, taskID *int64, tool, detail, prefix string) error {
	kind := AccessTool
	if prefix != "" {
		kind = AccessAsk
	}
	return s.recordAccess(projectID, agentID, taskID, tool, detail, prefix, kind)
}

// RecordCommandRequest noteaza o comanda refuzata unui agent fara Shell, pe prefixul calculat.
func (s *Store) RecordCommandRequest(projectID, agentID int64, taskID *int64, detail, prefix string) error {
	return s.recordAccess(projectID, agentID, taskID, "Bash", detail, prefix, AccessCommand)
}
```

`recordAccess` = corpul vechi al lui `RecordAccessDenial`, cu `AND kind = ?` in SELECT si `kind` in INSERT
(`INSERT INTO access_requests (project_id, agent_id, task_id, tool, detail, prefix, kind, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`).

- la final:

```go
// HasOpenAccessRequest spune daca agentul asteapta o aprobare pe task (cerere deschisa).
func (s *Store) HasOpenAccessRequest(agentID, taskID int64) (bool, error) {
	var ok bool
	err := s.db.QueryRow(`SELECT EXISTS(SELECT 1 FROM access_requests WHERE agent_id = ? AND task_id = ? AND dismissed = 0)`,
		agentID, taskID).Scan(&ok)
	return ok, err
}
```

`internal/store/chat.go`, in `newChatTx`, dupa `UPDATE agents ...`:

```go
	// grant-urile de chat tin pana la New chat (si la schimbarea contului, care reseteaza sesiunile)
	if _, err := tx.Exec(`DELETE FROM command_grants WHERE agent_id = ? AND task_id IS NULL`, agentID); err != nil {
		return 0, "", err
	}
```

- [ ] **Step 5: Ruleaza testele de store**

Run: `go test ./internal/store/`
Expected: PASS.

- [ ] **Step 6: Testele de team (esueaza)**

In `internal/team/commands_test.go`:
- sterge `TestGrantRestoredWhenStartFails` (nu se mai consuma nimic);
- in `TestAskDenialAllowOnceAndProject`: `f.tm.AllowCommandOnce` -> `f.tm.AllowCommandForTask`, iar `"allowed once"` -> `"allowed on T-1"`;
- in `TestBlockCommandAndNonCommandRequest` si `TestAllowOnceGrantLifecycle`: `AllowCommandOnce` -> `AllowCommandForTask`;
- in `TestAllowRefusedWhenRuleNoLongerAsk`: lista de actiuni devine `{f.tm.AllowCommandForTask, f.tm.AllowCommandInProject}`, `f.st.TakeCommandGrants(f.rook.ID)` -> `f.st.CommandGrants(f.rook.ID, task.ID)`, iar `f.st.AddCommandGrant(f.p.ID, f.rook.ID, "git push")` -> `f.st.AddCommandGrant(f.p.ID, f.rook.ID, &task2.ID, "git push")` (muta `task2 := ...` inainte si sterge `_ = task2`);
- inlocuieste finalul lui `TestAllowOnceGrantLifecycle` (de la `f.fs.finish(run.ID, ...)` pana la sfarsit) cu:

```go
	f.fs.finish(run.ID, runner.Outcome{Status: store.RunDone})
	// un run in chat (fara task) nu primeste grant-ul task-ului
	f.tm.work.Lock()
	_, err := f.tm.addUserMessage(f.rook, "again", nil, false, false)
	f.tm.work.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if err := f.tm.Reconcile(f.p.ID); err != nil {
		t.Fatal(err)
	}
	chat := f.fs.last(t, f.rook)
	if chat.ID == run.ID {
		t.Fatal("nu a pornit run-ul de chat")
	}
	if !strings.Contains(argAfter(chat.Args, "--settings"), "Bash(git push:*)") {
		t.Fatal("grant-ul task-ului a ajuns in chat")
	}
	f.fs.finish(chat.ID, runner.Outcome{Status: store.RunDone})
	// al doilea run pe acelasi task: grant-ul tine
	got, _ := f.st.GetTask(task.ID)
	got.Status = store.StatusQueued
	f.st.SaveTask(&got)
	if err := f.tm.Reconcile(f.p.ID); err != nil {
		t.Fatal(err)
	}
	again := f.fs.last(t, f.rook)
	if again.ID == chat.ID || strings.Contains(argAfter(again.Args, "--settings"), "Bash(git push:*)") {
		t.Fatal("grant-ul nu a tinut pe al doilea run al task-ului")
	}
```

(`Bash(git push:*)` in settings = in deny: fara grant apare, cu grant dispare. Pasul cu mesajul "again" e cel din testul vechi: dupa primul run task-ul nu mai e Queued, deci `Reconcile` porneste run-ul de chat.)

- [ ] **Step 7: Ruleaza, verifica ca esueaza**

Run: `go test ./internal/team/ -run 'Allow|Grant|Ask|Block'`
Expected: FAIL la compilare (`AllowCommandForTask` nedefinit; `TakeCommandGrants` nu mai exista in `run.go`).

- [ ] **Step 8: Implementarea in team**

`internal/team/run.go` — inlocuieste blocul de la comentariul `// grant-urile "Allow once" ...` pana la `cmdDeny := ...`:

```go
	// grant-urile din "Needs you": ale task-ului run-ului, sau de chat (taskID 0); nu se consuma
	granted, err := t.st.CommandGrants(a.ID, taskID)
	if err != nil {
		log.Printf("agent %d: command grants: %v", a.ID, err)
		granted = nil
	}
	cmdDeny := prompt.DenyPrefixes(rules, granted)
```

si sterge bucla `for _, g := range granted { ... AddCommandGrant ... }` din ramura de eroare a lui `t.runs.Start`.

`internal/team/commands.go` — inlocuieste `AllowCommandOnce`:

```go
// AllowCommandForTask permite prefixul agentului pe task-ul cererii (fara task: in chat, pana la New chat).
func (t *Team) AllowCommandForTask(accessID int64) error {
	return t.decideCommand(accessID, func(r store.AccessRequest) (string, error) {
		how := "allowed in this chat"
		if r.TaskID != nil {
			task, err := t.st.GetTask(*r.TaskID)
			if err != nil {
				return "", err
			}
			how = "allowed on " + task.Ref()
		}
		return how, t.st.AddCommandGrant(r.ProjectID, r.AgentID, r.TaskID, r.Prefix)
	}, true, false)
}
```

- [ ] **Step 9: Web + UI**

`internal/web/server.go:147`: `mux.HandleFunc("POST /api/access/{id}/allow-task", s.allowCommandTask)`.
`internal/web/api_commands.go`: redenumeste `allowCommandOnce` in `allowCommandTask`, cu `s.team.AllowCommandForTask`.
`internal/web/commands_test.go`: inlocuieste `allow-once` cu `allow-task` (liniile 58 si 73).
`internal/web/static/js/access.js:30`:

```js
        act(`Allow "${r.prefix}" ${r.ref ? `on ${r.ref}` : "in this chat"}`, "btn small primary", `/api/access/${r.id}/allow-task`),
```

- [ ] **Step 10: Verificare**

Run: `go vet ./... && go test ./internal/store/ ./internal/team/ ./internal/web/ && cp internal/web/static/js/access.js /tmp/access.mjs && node --check /tmp/access.mjs`
Expected: PASS, fara iesire de la node.

- [ ] **Step 11: Commit**

```bash
git add internal/store internal/team internal/web
git commit -m "comenzi: grant-urile din Needs you tin pe tot task-ul (sau pe chat pana la New chat)"
```

---

### Task 2: Prompt — prefixul calculat si grant-urile in allow

**Files:**
- Modify: `internal/prompt/commands.go` (CommandPrefix, BashAllowed, Request, NoShellRequests)
- Modify: `internal/prompt/settings.go` (`Settings` primeste `granted`)
- Modify: `internal/team/run.go` (`args` paseaza `granted`)
- Test: `internal/prompt/commands_test.go`, `internal/prompt/settings_test.go`, `internal/prompt/prompt_test.go`, `internal/prompt/folders_test.go`

**Interfaces:**
- Consumes: `store.AccessTool`, `store.AccessAsk`, `store.AccessCommand` (Task 1)
- Produces:
  - `prompt.CommandPrefix(cmd string) string`
  - `prompt.BashAllowed(cmd string, tools, granted []string) bool`
  - `type prompt.Request struct{ Prefix, Kind string }`
  - `prompt.NoShellRequests(cmd string, tools []string, rules []store.CommandRule, granted []string) (reqs []Request, blocked bool)`
  - `prompt.Settings(tools []string, home string, extraAllow, cmdDeny, granted []string) string`

- [ ] **Step 1: Testele (esueaza)**

Adauga in `internal/prompt/commands_test.go`:

```go
func TestCommandPrefix(t *testing.T) {
	for in, want := range map[string]string{
		`git stash push -u -m "x" -- app/`: "git stash",
		"ls -la":                           "ls",
		"php artisan migrate --force":      "php artisan migrate",
		"php -v":                           "php",
		"git -C dir status":                "git",
		"npx prettier --write .":           "npx prettier",
		"./vendor/bin/phpunit --filter X":  "./vendor/bin/phpunit",
		"":                                 "",
	} {
		if got := CommandPrefix(in); got != want {
			t.Errorf("%q: %q, vrea %q", in, got, want)
		}
	}
}

func TestNoShellRequests(t *testing.T) {
	rules := []store.CommandRule{{Prefix: "git push", Mode: store.CmdAsk}, {Prefix: "sudo", Mode: store.CmdBlock}}
	git := []string{"git"}
	cases := []struct {
		cmd     string
		tools   []string
		granted []string
		want    []Request
		blocked bool
	}{
		// git stash nu e in tool-ul git: prefix calculat
		{"git stash push -u", git, nil, []Request{{"git stash", store.AccessCommand}}, false},
		// bucata permisa (git status) nu face cerere
		{"git status && git stash", git, nil, []Request{{"git stash", store.AccessCommand}}, false},
		// cu grant: nimic de cerut, ramane cererea de tool (Enable Shell)
		{"git stash", git, []string{"git stash"}, []Request{{"", store.AccessTool}}, false},
		// regula Ask: prefixul regulii
		{"git push origin main", git, nil, []Request{{"git push", store.AccessAsk}}, false},
		// Block: fara cerere
		{"sudo ls", git, nil, nil, true},
		// refuz fix (instalare fara packages): cerere de tool
		{"npm install x", git, nil, []Request{{"", store.AccessTool}}, false},
		// doua bucati necunoscute, fara duplicate
		{"cd app && make build && make test", nil, nil, []Request{{"cd", store.AccessCommand}, {"make build", store.AccessCommand}, {"make test", store.AccessCommand}}, false},
	}
	for _, c := range cases {
		got, blocked := NoShellRequests(c.cmd, c.tools, rules, c.granted)
		if !slices.Equal(got, c.want) || blocked != c.blocked {
			t.Errorf("%q: %+v %v, vrea %+v %v", c.cmd, got, blocked, c.want, c.blocked)
		}
	}
}
```

(adauga `"slices"` la import daca lipseste.)

Adauga in `internal/prompt/settings_test.go`:

```go
func TestSettingsGranted(t *testing.T) {
	raw := Settings([]string{"git"}, "", nil, []string{"sudo"}, []string{"git stash", "sudo", "bad)*"})
	var cfg struct {
		Permissions struct{ Allow, Deny []string } `json:"permissions"`
	}
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(cfg.Permissions.Allow, "Bash(git stash:*)") {
		t.Fatalf("grant-ul lipseste din allow: %s", raw)
	}
	// Block ramane in deny chiar daca apare si in grant-uri (deny castiga in claude)
	if !slices.Contains(cfg.Permissions.Deny, "Bash(sudo:*)") {
		t.Fatalf("%s", raw)
	}
	if strings.Contains(raw, "bad)") {
		t.Fatalf("prefix invalid acceptat: %s", raw)
	}
}
```

(adauga `"encoding/json"` si `"slices"` la importurile din `settings_test.go` daca lipsesc.)

Si in toate apelurile existente `Settings(a, b, c, d)` din `prompt_test.go`, `settings_test.go`, `folders_test.go` adauga al cincilea argument `nil`.

- [ ] **Step 2: Ruleaza, verifica ca esueaza**

Run: `go test ./internal/prompt/`
Expected: FAIL la compilare (`CommandPrefix`, `NoShellRequests`, `Request` nedefinite; `Settings` cu prea multe argumente).

- [ ] **Step 3: Implementarea**

`internal/prompt/commands.go` (adauga `"regexp"` la import daca lipseste):

```go
// subcommandTools: unelte la care al doilea cuvant e subcomanda (git stash, npm run...).
var subcommandTools = map[string]bool{
	"git": true, "npm": true, "npx": true, "pnpm": true, "yarn": true, "composer": true,
	"go": true, "docker": true, "cargo": true, "make": true, "kubectl": true,
}

var subcommandWord = regexp.MustCompile(`^[a-z][a-z0-9:-]*$`)

// CommandPrefix: prefixul pe care il aprobi pentru o comanda (o bucata, fara && / |):
// primul cuvant, plus subcomanda la uneltele cu subcomenzi ("git stash", "php artisan migrate").
// Un flag pe locul subcomenzii ("git -C dir ...") lasa doar primul cuvant.
func CommandPrefix(cmd string) string {
	words := strings.Fields(cmd)
	if len(words) == 0 {
		return ""
	}
	n := 1
	switch {
	case words[0] == "php" && len(words) > 1 && words[1] == "artisan":
		n = 2
	case !subcommandTools[words[0]]:
		return words[0]
	}
	if len(words) > n && subcommandWord.MatchString(words[n]) {
		n++
	}
	return strings.Join(words[:n], " ")
}

// BashAllowed spune daca o bucata de comanda e permisa agentului: cu Shell orice,
// altfel ce permit tool-urile lui (git -> git status...) si grant-urile.
func BashAllowed(cmd string, tools, granted []string) bool {
	if slices.Contains(tools, "shell") {
		return true
	}
	var rules []string
	for _, name := range tools {
		rules = append(rules, toolRules[name]...)
	}
	for _, g := range granted {
		rules = append(rules, "Bash("+g+":*)")
	}
	return bashMatches(cmd, rules)
}

// Request: o cerere pentru "Needs you" dintr-o comanda refuzata (Kind = store.Access*).
type Request struct {
	Prefix string
	Kind   string
}

// NoShellRequests imparte comanda refuzata unui agent fara Shell in cereri, pe bucati:
// regula Block = fara cerere (blocked); regula Ask = prefixul regulii (daca nu are deja grant);
// refuz fix (npm install fara packages) = cerere de tool; bucata permisa = nimic;
// altfel prefixul calculat. Fara nicio cerere si fara Block ramane cererea de tool (Enable Shell),
// ca utilizatorul sa vada refuzul.
func NoShellRequests(cmd string, tools []string, rules []store.CommandRule, granted []string) (reqs []Request, blocked bool) {
	add := func(r Request) {
		if !slices.Contains(reqs, r) {
			reqs = append(reqs, r)
		}
	}
	for _, part := range splitCommand(cmd) {
		mode, pfx := MatchCommand(part, rules)
		if mode != store.CmdBlock && mode != store.CmdAsk {
			mode, pfx = DenyingRule(part, rules)
		}
		switch {
		case mode == store.CmdBlock:
			blocked = true
		case mode == store.CmdAsk:
			if !slices.Contains(granted, pfx) {
				add(Request{pfx, store.AccessAsk})
			}
		case bashMatches(part, denyFor(tools)):
			add(Request{"", store.AccessTool})
		case BashAllowed(part, tools, granted):
			// deja permisa
		default:
			add(Request{CommandPrefix(part), store.AccessCommand})
		}
	}
	if len(reqs) == 0 && !blocked {
		reqs = []Request{{"", store.AccessTool}}
	}
	return reqs, blocked
}
```

`internal/prompt/settings.go` — semnatura si comentariul lui `Settings`:

```go
// granted: prefixele aprobate din "Needs you" pentru acest run, puse in allow ca Bash(prefix:*)
// (si agentilor fara Shell). Un deny (Block, refuz fix) tot castiga.
func Settings(tools []string, home string, extraAllow, cmdDeny, granted []string) string {
```

si, dupa bucla `for _, name := range ValidTools { ... }`:

```go
	for _, g := range granted {
		// prefixele vin din reguli sau din CommandPrefix; nimic care ar putea iesi din Bash(...:*)
		if g != "" && !strings.ContainsAny(g, "()*") {
			allow = append(allow, "Bash("+g+":*)")
		}
	}
```

`internal/team/run.go`: `t.args(...)` primeste `granted []string` dupa `cmdDeny` (apelul din `startRun`: `t.args(p, a, system, turn, mcpCfg, resume, libBuild, cmdDeny, granted)`) si il paseaza la `prompt.Settings(a.Tools, store.HomeFolder(a, p.Folders).Path, lb.Allow, cmdDeny, granted)`. Cauta si alte apeluri ale lui `t.args` (`grep -n "t.args(" internal/team/*.go`) si adauga `nil`.

- [ ] **Step 4: Ruleaza testele**

Run: `go test ./internal/prompt/ ./internal/team/`
Expected: PASS.

- [ ] **Step 5: Test team: grant la agent fara Shell ajunge in allow**

Adauga in `internal/team/commands_test.go`:

```go
func TestTaskGrantAllowsCommandWithoutShell(t *testing.T) {
	f := setup(t)
	f.rook.Tools = []string{"git"}
	f.st.UpdateAgent(f.rook)
	task := f.task(t, "Stash", f.rook, store.StatusQueued)
	f.st.AddCommandGrant(f.p.ID, f.rook.ID, &task.ID, "git stash")
	if err := f.tm.Reconcile(f.p.ID); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(argAfter(f.fs.last(t, f.rook).Args, "--settings"), `"Bash(git stash:*)"`) {
		t.Fatal("grant-ul nu e in allow")
	}
}
```

Run: `go test ./internal/team/ -run TestTaskGrantAllowsCommandWithoutShell`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/prompt internal/team
git commit -m "comenzi: prefixul calculat (CommandPrefix, NoShellRequests) si grant-urile in allow"
```

---

### Task 3: Cereri pe prefix pentru agentii fara Shell (team, API, card)

**Files:**
- Modify: `internal/team/run.go:390-449` (`recordDenials`)
- Modify: `internal/team/commands.go` (verificarea la Allow pe kind, `ErrNoProjectRule`)
- Modify: `internal/web/api_access.go` (`accessView`: Kind, HasShell, CanEnableShell)
- Modify: `internal/web/api_commands.go` (409 pentru `ErrNoProjectRule`)
- Modify: `internal/web/static/js/access.js` (cardul pe prefix)
- Test: `internal/team/commands_test.go`, `internal/web/commands_test.go`

**Interfaces:**
- Consumes: `prompt.NoShellRequests`, `prompt.Request` (Task 2); `store.RecordCommandRequest`, `store.CommandGrants`, `AccessRequest.Kind` (Task 1); `AllowCommandForTask` (Task 1)
- Produces: `team.ErrNoProjectRule`; JSON `accessView` cu `kind`, `hasShell`

- [ ] **Step 1: Testele de team (esueaza)**

```go
func TestNoShellDenialAllowOnTask(t *testing.T) {
	f := setup(t)
	f.rook.Tools = []string{"git"}
	f.st.UpdateAgent(f.rook)
	task := f.task(t, "Stash", f.rook, store.StatusInProgress)
	f.tm.work.Lock()
	f.tm.recordDenials(activeRun{projectID: f.p.ID, taskID: task.ID}, f.rook, []runner.Denial{
		{Tool: "Bash", Detail: `git stash push -u -m "wip" -- app/`},
	})
	f.tm.work.Unlock()
	reqs, _ := f.st.ListOpenAccessRequests(f.p.ID)
	if len(reqs) != 1 || reqs[0].Prefix != "git stash" || reqs[0].Kind != store.AccessCommand || reqs[0].TaskID == nil || *reqs[0].TaskID != task.ID {
		t.Fatalf("%+v", reqs)
	}
	// Allow in this project / Block nu au sens fara Shell
	for _, act := range []func(int64) error{f.tm.AllowCommandInProject, f.tm.BlockCommandInProject} {
		if err := act(reqs[0].ID); !errors.Is(err, ErrNoProjectRule) {
			t.Fatalf("err = %v", err)
		}
	}
	if err := f.tm.AllowCommandForTask(reqs[0].ID); err != nil {
		t.Fatal(err)
	}
	if g, _ := f.st.CommandGrants(f.rook.ID, task.ID); len(g) != 1 || g[0] != "git stash" {
		t.Fatalf("grants = %v", g)
	}
	pending, _ := f.st.Undelivered(f.rook.ID)
	want := `You may now run commands starting with "git stash" (allowed on T-1 by the user). Run your command again if you still need it.`
	if len(pending) == 0 || pending[len(pending)-1].Text != want {
		t.Fatalf("%+v", pending)
	}
	f.wantStatus(t, task.Number, store.StatusQueued)
}

func TestNoShellAllowRefusedWhenBlockedNow(t *testing.T) {
	f := setup(t)
	f.rook.Tools = []string{"git"}
	f.st.UpdateAgent(f.rook)
	task := f.task(t, "Stash", f.rook, store.StatusInProgress)
	f.tm.work.Lock()
	f.tm.recordDenials(activeRun{projectID: f.p.ID, taskID: task.ID}, f.rook, []runner.Denial{{Tool: "Bash", Detail: "git stash"}})
	f.tm.work.Unlock()
	reqs, _ := f.st.ListOpenAccessRequests(f.p.ID)
	f.st.SetCommandRules(f.p.ID, []store.CommandRule{{Prefix: "git stash", Mode: store.CmdBlock}})
	if err := f.tm.AllowCommandForTask(reqs[0].ID); !errors.Is(err, ErrRuleChanged) {
		t.Fatalf("err = %v", err)
	}
	if g, _ := f.st.CommandGrants(f.rook.ID, task.ID); len(g) != 0 {
		t.Fatalf("grants = %v", g)
	}
}

func TestNoShellDenialSkipsGrantedAndBlocked(t *testing.T) {
	f := setup(t)
	f.rook.Tools = []string{"git"}
	f.st.UpdateAgent(f.rook)
	task := f.task(t, "Stash", f.rook, store.StatusInProgress)
	f.st.AddCommandGrant(f.p.ID, f.rook.ID, &task.ID, "git stash")
	f.tm.work.Lock()
	f.tm.recordDenials(activeRun{projectID: f.p.ID, taskID: task.ID}, f.rook, []runner.Denial{
		{Tool: "Bash", Detail: "sudo ls"},
		{Tool: "Bash", Detail: "git stash && make build"},
	})
	f.tm.work.Unlock()
	reqs, _ := f.st.ListOpenAccessRequests(f.p.ID)
	if len(reqs) != 1 || reqs[0].Prefix != "make build" {
		t.Fatalf("doar make build: %+v", reqs)
	}
}
```

- [ ] **Step 2: Ruleaza, verifica ca esueaza**

Run: `go test ./internal/team/ -run NoShell`
Expected: FAIL la compilare (`ErrNoProjectRule` nedefinit), apoi pe aserturi.

- [ ] **Step 3: `recordDenials`**

In `internal/team/run.go`, inainte de bucla `for _, d := range denials`, dupa `hasShell := ...`:

```go
	granted, err := t.st.CommandGrants(a.ID, ar.taskID)
	if err != nil {
		log.Printf("agent %d: command grants: %v", a.ID, err)
	}
```

Inlocuieste corpul buclei (de la `prefix := ""` pana la `t.denialEvent(task, a, d)` inclusiv) cu:

```go
		// cererile pentru acest refuz; implicit cererea de tool (Enable Shell, WebFetch...)
		reqs := []prompt.Request{{Prefix: "", Kind: store.AccessTool}}
		if d.Tool == "Bash" {
			cmd := d.Command // comanda intreaga; Detail e scurtat la 120 de caractere
			if cmd == "" {
				cmd = d.Detail
			}
			if hasShell {
				// agent cu shell: regulile decid (block = fara cerere, ask = cerere pe prefix)
				mode, pfx := prompt.MatchCommand(cmd, rules)
				if mode != store.CmdBlock && mode != store.CmdAsk {
					// claude a refuzat desi regula decisiva e Allow (sau lipseste): cautam deny-ul care a lucrat
					mode, pfx = prompt.DenyingRule(cmd, rules)
				}
				if mode == store.CmdBlock {
					t.denialEvent(task, a, d)
					continue
				}
				if mode == store.CmdAsk {
					reqs = []prompt.Request{{Prefix: pfx, Kind: store.AccessAsk}}
				}
			} else {
				// agent fara shell: cate o cerere pe fiecare prefix care lipseste
				var blocked bool
				reqs, blocked = prompt.NoShellRequests(cmd, a.Tools, rules, granted)
				if len(reqs) == 0 {
					if blocked {
						t.denialEvent(task, a, d)
					}
					continue
				}
			}
		}
		added := false
		for _, r := range reqs {
			key := d.Tool + "|" + r.Kind + "|" + r.Prefix
			if seen[key] {
				continue
			}
			seen[key] = true
			var err error
			if r.Kind == store.AccessCommand {
				err = t.st.RecordCommandRequest(ar.projectID, a.ID, tid, d.Detail, r.Prefix)
			} else {
				err = t.st.RecordAccessDenial(ar.projectID, a.ID, tid, d.Tool, d.Detail, r.Prefix)
			}
			if err != nil {
				log.Printf("agent %d: access denial: %v", a.ID, err)
				continue
			}
			added = true
		}
		if added {
			recorded = true
			t.denialEvent(task, a, d)
		}
```

(variabila `err` din bucla umbreste pe cea de sus intentionat; daca `go vet` se plange, redenumeste in `rerr`.)

- [ ] **Step 4: Verificarea la Allow si `ErrNoProjectRule`**

In `internal/team/commands.go`:

```go
// ErrNoProjectRule: cererea e pe o comanda a unui agent fara Shell; o regula de proiect nu i-ar folosi.
var ErrNoProjectRule = errors.New("this agent has no Shell; allow the command on the task or enable Shell")
```

In `decideCommand`, dupa verificarea `r.Prefix == ""`:

```go
	if project && r.Kind != store.AccessAsk {
		t.work.Unlock()
		return ErrNoProjectRule
	}
```

si inlocuieste blocul `if allow { ... }` (verificarea regulii) cu:

```go
	if allow {
		// regula s-a putut schimba dupa cerere (de ex. in Block): un Allow nu are voie sa o ocoleasca
		rules, err := t.EffectiveCommandRules(r.ProjectID)
		if err != nil {
			t.work.Unlock()
			return err
		}
		ok := slices.Contains(rules, store.CommandRule{Prefix: r.Prefix, Mode: store.CmdAsk})
		if r.Kind == store.AccessCommand {
			// prefix calculat: permis cat timp nu e pe Block si nu e un refuz fix pentru tool-urile agentului
			a, err := t.st.GetAgent(r.AgentID)
			if err != nil {
				t.work.Unlock()
				return err
			}
			ok = !prompt.AlwaysDeniedBash(r.Prefix, a.Tools, rules)
		}
		if !ok {
			t.work.Unlock()
			return ruleChangedError{r.Prefix}
		}
	}
```

- [ ] **Step 5: Ruleaza testele de team**

Run: `go test ./internal/team/`
Expected: PASS.

- [ ] **Step 6: API (test esueaza, apoi implementare)**

Adauga in `internal/web/commands_test.go`:

```go
func TestAccessCommandKindAPI(t *testing.T) {
	e := setup(t)
	p := e.createProject(t, t.TempDir())
	w := e.addWorker(t, p.ID, "Beacon")
	w.Tools = []string{"git"}
	e.st.UpdateAgent(w)
	e.st.RecordCommandRequest(p.ID, w.ID, nil, "git stash push", "git stash")
	reqs, _ := e.st.ListOpenAccessRequests(p.ID)
	id := itoa(reqs[0].ID)
	code, raw := e.do(t, "GET", "/api/projects/"+itoa(p.ID), nil)
	if code != 200 || !strings.Contains(string(raw), `"kind":"command"`) || !strings.Contains(string(raw), `"hasShell":false`) || !strings.Contains(string(raw), `"canEnableShell":true`) {
		t.Fatalf("%d %s", code, raw)
	}
	if code, raw := e.do(t, "POST", "/api/access/"+id+"/allow-project", nil); code != http.StatusConflict || !strings.Contains(string(raw), "no Shell") {
		t.Fatalf("allow-project: %d %s", code, raw)
	}
	if code, raw := e.do(t, "POST", "/api/access/"+id+"/allow-task", nil); code != http.StatusNoContent {
		t.Fatalf("allow-task: %d %s", code, raw)
	}
	if g, _ := e.st.CommandGrants(w.ID, 0); len(g) != 1 || g[0] != "git stash" {
		t.Fatalf("grants = %v", g)
	}
}
```

(`GET /api/projects/{id}` intoarce `access`, ca in `TestAccessInResponses`.)

`internal/web/api_access.go`, in `accessView` adauga:

```go
	Kind     string `json:"kind"`     // "" (tool) / "ask" / "command"
	HasShell bool   `json:"hasShell"` // fara Shell, "Allow in this project" nu are efect
```

si in `accessViews`, in bucla pe agenti:

```go
				hasShell := slices.Contains(a.Tools, "shell")
				v.AgentName, v.HasShell = a.Name, hasShell
				// Enable Shell ajuta la o cerere de tool Bash (daca nu e refuz fix) si la o comanda calculata
				v.CanEnableShell = r.Tool == "Bash" && !hasShell &&
					(r.Kind == store.AccessCommand || (r.Kind == store.AccessTool && !prompt.AlwaysDeniedBash(r.Detail, a.Tools, rules)))
				v.CanEnablePackages = r.Kind == store.AccessTool && r.Tool == "Bash" && !slices.Contains(a.Tools, "packages") && prompt.NeedsPackages(r.Detail)
```

(si `Kind: r.Kind` in initializarea lui `v`).

`internal/web/api_commands.go`, in `accessCommand`, adauga `|| errors.Is(err, team.ErrNoProjectRule)` la conditia de 409.

Run: `go test ./internal/web/`
Expected: PASS.

- [ ] **Step 7: Cardul**

In `internal/web/static/js/access.js`, inlocuieste ramura `if (r.prefix) { ... }`:

```js
  if (r.prefix) {
    // ask = regula Ask (cu Shell); command = comanda in afara tool-urilor unui agent fara Shell
    const where = r.ref ? `on ${r.ref}` : "in this chat";
    return el("div", { class: "question access cmd-ask" },
      el("div", { class: "question-head" }, agent ? avatar(agent.name, kindOf(agent), 20) : null,
        el("span", { class: "label" }, r.kind === "ask" ? "COMMAND · ASK" : "COMMAND"),
        el("strong", {}, `${name} wants to run`),
        el("span", {}, (r.count > 1 ? ` (${r.count}×)` : "") + (r.ref ? ` · ${r.ref}` : ""))),
      el("p", { class: "question-text" }, el("code", {}, r.detail || r.prefix), " · ",
        el("span", { class: "stuck-age", "data-since": r.updatedAt }, stuckAge(r.updatedAt)), " ago"),
      el("div", { class: "actions" },
        act(`Allow "${r.prefix}" ${where}`, "btn small primary", `/api/access/${r.id}/allow-task`),
        r.kind === "ask" && r.hasShell ? act(`Allow "${r.prefix}" in this project`, "btn small", `/api/access/${r.id}/allow-project`) : null,
        r.kind === "ask" ? act("Block", "btn small", `/api/access/${r.id}/block-project`) : null,
        r.canEnableShell ? act(`Enable Shell for ${name}`, "btn small", `/api/access/${r.id}/enable-shell`) : null,
        act("Dismiss", "btn small", `/api/access/${r.id}/dismiss`)));
  }
```

Actualizeaza si comentariul de sus al fisierului (butoanele cardului pe prefix).

Run: `cp internal/web/static/js/access.js /tmp/access.mjs && node --check /tmp/access.mjs`
Expected: fara iesire.

- [ ] **Step 8: Commit**

```bash
git add internal/team internal/web
git commit -m "comenzi: cereri pe prefix pentru agentii fara Shell, Allow on T-N in Needs you"
```

---

### Task 4: Enable Shell trezeste agentul, task-urile care asteapta aprobare, promptul

**Files:**
- Modify: `internal/team/commands.go` (`EnableTool`, `wake`)
- Modify: `internal/web/api_access.go` (`enableTool` cheama team)
- Modify: `internal/team/stuck.go` (cazul In progress)
- Modify: `internal/prompt/prompt.go` (`howAsk`, `howDenied`)
- Modify: `internal/mcp/tools.go:191` (descrierea ask_user)
- Test: `internal/team/commands_test.go`, `internal/team/stuck_test.go`, `internal/prompt/prompt_test.go`

**Interfaces:**
- Consumes: `store.HasOpenAccessRequest`, `AccessRequest.Kind` (Task 1)
- Produces: `(*Team) EnableTool(accessID int64, tool string) error`, `team.ErrNotToolRequest`

- [ ] **Step 1: Testele (esueaza)**

In `internal/team/commands_test.go`:

```go
func TestEnableShellWakesAgent(t *testing.T) {
	f := setup(t)
	f.rook.Tools = []string{"git"}
	f.st.UpdateAgent(f.rook)
	task := f.task(t, "Stash", f.rook, store.StatusInProgress)
	f.st.RecordCommandRequest(f.p.ID, f.rook.ID, &task.ID, "git stash", "git stash")
	f.st.RecordAccessDenial(f.p.ID, f.rook.ID, nil, "WebFetch", "https://x.dev", "")
	reqs, _ := f.st.ListOpenAccessRequests(f.p.ID)
	if err := f.tm.EnableTool(reqs[1].ID, "shell"); !errors.Is(err, ErrNotToolRequest) {
		t.Fatalf("WebFetch: %v", err)
	}
	if err := f.tm.EnableTool(reqs[0].ID, "packages"); !errors.Is(err, ErrNotToolRequest) {
		t.Fatalf("packages pe o comanda: %v", err)
	}
	if err := f.tm.EnableTool(reqs[0].ID, "shell"); err != nil {
		t.Fatal(err)
	}
	got, _ := f.st.GetAgent(f.rook.ID)
	if !slices.Contains(got.Tools, "shell") || !slices.Contains(got.Tools, "git") {
		t.Fatalf("%v", got.Tools)
	}
	if !f.hasMessage(f.rook.ID, "You now have the Shell tool (enabled by the user).") {
		t.Fatal("Rook nu a fost trezit")
	}
	f.wantStatus(t, task.Number, store.StatusQueued)
	if err := f.tm.EnableTool(reqs[0].ID, "shell"); !errors.Is(err, ErrRequestClosed) {
		t.Fatalf("a doua oara: %v", err)
	}
}
```

(adauga `"slices"` la import.)

In `internal/team/stuck_test.go`:

```go
// un orfan care asteapta o aprobare de comanda nu e blocat
func TestStuckOrphanWaitingForAccess(t *testing.T) {
	f := setup(t)
	task := f.task(t, "A", f.rook, store.StatusInProgress)
	f.st.RecordCommandRequest(f.p.ID, f.rook.ID, &task.ID, "git stash", "git stash")
	f.tm.CheckStuck(task.UpdatedAt.Add(6 * time.Minute))
	if got, _ := f.st.GetTask(task.ID); got.Status != store.StatusInProgress || got.NudgedAt != nil {
		t.Fatalf("%+v", got)
	}
}
```

In `internal/prompt/prompt_test.go`:

```go
// refuzurile: in toate modurile, fara ask_user (TestSystemAutonomy cere ca modul autonom sa nu-l pomeneasca)
func TestSystemDenied(t *testing.T) {
	for _, p := range []store.Project{{Name: "X"}, {Name: "X", Autonomy: true}} {
		got := System(p, store.Agent{Name: "A"}, Links{BossName: "Boss"})
		if !strings.Contains(got, "Command and tool permissions are set in Foreman") || strings.Contains(got, "a decision, a permission") {
			t.Errorf("autonomy=%v:\n%s", p.Autonomy, got)
		}
	}
}
```

- [ ] **Step 2: Ruleaza, verifica ca esueaza**

Run: `go test ./internal/team/ ./internal/prompt/ -run 'EnableShell|WaitingForAccess|Prompt'`
Expected: FAIL (`EnableTool`, `ErrNotToolRequest` nedefinite).

- [ ] **Step 3: `EnableTool` si `wake`**

In `internal/team/commands.go`:

```go
// ErrNotToolRequest: tool-ul nu se poate porni din aceasta cerere.
var ErrNotToolRequest = errors.New("only shell access can be enabled from here")

// EnableTool adauga tool-ul ("shell" sau "packages") agentului cererii, inchide cererea si il trezeste.
// Shell: pentru o cerere de tool Bash sau o comanda calculata; packages: doar pentru o cerere de tool Bash.
func (t *Team) EnableTool(accessID int64, tool string) error {
	t.work.Lock()
	r, err := t.st.GetAccessRequest(accessID)
	if errors.Is(err, sql.ErrNoRows) {
		err = store.ErrNotFound
	}
	if err != nil {
		t.work.Unlock()
		return err
	}
	if r.Dismissed {
		t.work.Unlock()
		return ErrRequestClosed
	}
	if r.Tool != "Bash" || r.Kind == store.AccessAsk || (tool == "packages" && r.Kind != store.AccessTool) {
		t.work.Unlock()
		return ErrNotToolRequest
	}
	a, err := t.st.GetAgent(r.AgentID)
	if err != nil {
		t.work.Unlock()
		return err
	}
	if !slices.Contains(a.Tools, tool) {
		a.Tools = append(a.Tools, tool)
		if err := t.st.UpdateAgent(a); err != nil {
			t.work.Unlock()
			return err
		}
		t.emit(a.ProjectID, "agent", map[string]int64{"id": a.ID})
	}
	if err := t.st.DismissAccessRequest(r.ID); err != nil {
		t.work.Unlock()
		return err
	}
	msg := "You now have the Shell tool (enabled by the user). Run your command again if you still need it."
	if tool == "packages" {
		msg = "You may now install packages (enabled by the user). Run your command again if you still need it."
	}
	if err := t.wake(r, msg); err != nil {
		t.work.Unlock()
		return err
	}
	t.emit(r.ProjectID, "access", map[string]int64{"agentId": r.AgentID})
	t.work.Unlock()
	t.Kick(r.ProjectID)
	return nil
}
```

Imparte `wakeForCommand` in doua:

```go
// wakeForCommand: wake cu mesajul pentru un prefix permis. Se apeleaza cu t.work blocat.
func (t *Team) wakeForCommand(r store.AccessRequest, how string) error {
	return t.wake(r, fmt.Sprintf("You may now run commands starting with %q (%s by the user). Run your command again if you still need it.", r.Prefix, how))
}

// wake pune task-ul cererii inapoi in coada si lasa agentului un mesaj nelivrat. Se apeleaza cu t.work blocat.
func (t *Team) wake(r store.AccessRequest, msg string) error {
	a, err := t.st.GetAgent(r.AgentID)
	if err != nil {
		return err
	}
	if r.TaskID != nil {
		if err := t.requeueTaskFor(*r.TaskID, a); err != nil {
			return err
		}
	}
	_, err = t.addUserMessage(a, clip(msg), nil, true, true)
	return err
}
```

- [ ] **Step 4: Web**

In `internal/web/api_access.go`, inlocuieste corpul lui `enableTool` cu:

```go
// enableTool porneste tool-ul ("shell" sau "packages") prin team, care trezeste agentul.
func (s *Server) enableTool(w http.ResponseWriter, r *http.Request, tool string) {
	id, err := pathID(r)
	if err != nil {
		fail(w, err)
		return
	}
	if err := s.team.EnableTool(id, tool); err != nil {
		if errors.Is(err, team.ErrNotToolRequest) || errors.Is(err, team.ErrRequestClosed) {
			writeErr(w, http.StatusConflict, err.Error())
			return
		}
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
```

(import `foreman/internal/team`; scoate importurile ramase nefolosite. `fail` intoarce 404 pentru `store.ErrNotFound` — verifica testul `TestAccessDismiss` cu `enable-shell` pe 99999.)

`TestAccessEnableShell` (web): a doua apasare pe acelasi rand nu mai e posibila (cererea e inchisa), dar testul foloseste un rand nou (`make`) — ramane 204. Ruleaza si vezi.

- [ ] **Step 5: Detectorul de blocaje**

In `internal/team/stuck.go`, cazul `store.StatusInProgress`, dupa bucla pe `open` (intrebari), inainte de `return stuckOrphan, a, true`:

```go
		// o cerere de acces deschisa pe task = asteapta aprobarea utilizatorului (e deja in "Needs you")
		if waiting, err := t.st.HasOpenAccessRequest(a.ID, task.ID); err != nil || waiting {
			return "", store.Agent{}, false
		}
```

- [ ] **Step 6: Promptul**

`internal/prompt/prompt.go`:

```go
const howAsk = `If you need a decision or information that only the user can give, call mcp__team__ask_user with a clear question (and short options if it is a choice), then stop. Do not guess, and do not just end your message with a question.`

// howDenied: permisiunile vin din Foreman; un refuz ajunge singur in "Needs you".
const howDenied = `Command and tool permissions are set in Foreman, not in Claude Code settings. If something is denied, do not ask the user to allow it and do not suggest editing settings files or changing the permission mode: the denial already appears in the user's Needs you list, and approving it wakes you up. Say in one line what was denied and why you need it, then stop without calling report_done for that work. When you are told it is now allowed, run it again.`
```

si dupa `switch { case p.Autonomy: ... }` (inainte de `if len(l.BlockedCommands) > 0`): `how = append(how, howDenied)`.

`internal/mcp/tools.go:191`: `"Ask the user a question when you need a decision or information only they can give (not for denied commands: those reach the user on their own). After calling it, stop: the answer arrives later as a message. Do not call report_done while you wait."`

- [ ] **Step 7: Ruleaza totul**

Run: `go vet ./... && go test ./...`
Expected: PASS. Daca un test de prompt compara textul exact al sectiunii "How you work", actualizeaza asteptarea cu `howDenied`.

- [ ] **Step 8: Commit**

```bash
git add internal/team internal/web internal/prompt internal/mcp
git commit -m "comenzi: Enable Shell trezeste agentul; agentii nu mai cer aprobari in chat"
```

---

### Task 5: Verificare finala si documentatie

**Files:**
- Modify: `CLAUDE.md` (sectiunea Stare)
- Modify: `docs/superpowers/specs/2026-10-09-foreman-task-grants-design.md` (Status)

- [ ] **Step 1: Verificarile din conventii**

Run:
```bash
go vet ./... && go build -race ./... && go test ./... \
 && GOOS=darwin GOARCH=arm64 go build -o /dev/null ./cmd/foreman \
 && GOOS=windows go build -o /dev/null ./cmd/foreman \
 && cp internal/web/static/js/access.js /tmp/access.mjs && node --check /tmp/access.mjs
```
Expected: totul trece.

- [ ] **Step 2: Screenshot (memoria: verifica UI-ul cu screenshot-uri)**

Porneste aplicatia pe o baza temporara, creeaza un proiect cu un agent cu tool-ul `git`, introdu din `sqlite3` o cerere `command` (`INSERT INTO access_requests (project_id, agent_id, task_id, tool, detail, prefix, kind, created_at, updated_at) VALUES (...,'Bash','git stash push -u','git stash','command', datetime('now'), datetime('now'))`) si una `ask` pentru un agent cu Shell. Fa screenshot cu Chrome headless prin CDP pe board-ul proiectului (coloana Needs you) si verifica: "Allow "git stash" on T-1" primar, Enable Shell, Dismiss la cardul `command`; Allow / Allow in this project / Block / Dismiss la cardul `ask`.

- [ ] **Step 3: Verificare optionala cu claude real (`cd`)**

Cu un agent `git` pe un task care cere `cd <subfolder> && git stash list`, vezi daca refuzul include `cd` (card "cd" separat). Daca da, raporteaza utilizatorului inainte sa adaugi `Bash(cd:*)` in `alwaysAllow` (decizie separata, nu in acest plan).

- [ ] **Step 4: Documentatie**

In `CLAUDE.md`, dupa intrarea "reguli pentru comenzi", adauga:

```
Gata (aprobari pe task, spec in docs/superpowers/specs/2026-10-09-foreman-task-grants-design.md): un grant din Needs you
(command_grants.task_id, migrarea 028) tine pe toate run-urile agentului pe task (NULL = chat, pana la New chat) si intra in
--settings ca Bash(prefix:*) in allow; agentii fara Shell primesc cereri pe prefix calculat (prompt.NoShellRequests,
access_requests.kind 'command'); Enable Shell / Allow package installs trezesc agentul (team.EnableTool); un task cu cerere
de acces deschisa nu e "stuck"; promptul (howDenied) spune ca permisiunile vin din Foreman.
```

In spec: `Status: implementat (2026-10-09).`

- [ ] **Step 5: Commit**

```bash
git add CLAUDE.md docs/superpowers/specs/2026-10-09-foreman-task-grants-design.md
git commit -m "docs: aprobari de comenzi pe task"
```
