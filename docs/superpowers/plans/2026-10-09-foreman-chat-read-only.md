# Chat doar de citire, modificarile pe task — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Un run fara task (chat, notificari, comentarii) e doar de citire; agentul porneste un task pentru orice modificare, cu tool-ul nou `start_task`.

**Architecture:** In `startRun`, cand `taskID == 0`, tool-urile agentului trec prin `prompt.ReadOnlyTools` doar pentru `--settings` (system prompt-ul ramane acelasi, ca sesiunea sa nu se reseteze), grant-urile nu se aplica si tura primeste linia `prompt.ReadOnlyTurn`. Refuzurile de scriere din aceste run-uri nu fac cereri de acces. `start_task` (MCP, doar `TaskID == 0`) creeaza un task Queued pentru agentul care cheama.

**Tech Stack:** Go (stdlib, modernc.org/sqlite).

**Spec:** `docs/superpowers/specs/2026-10-09-foreman-chat-read-only-design.md`

## Global Constraints

- `go vet ./...` si `go build -race ./...` trebuie sa treaca; `go test ./...` verde.
- Cross-compile: `GOOS=darwin GOARCH=arm64 go build ./cmd/foreman` si `GOOS=windows go build ./cmd/foreman`.
- Tot ce vede utilizatorul si prompturile agentilor: engleza. Comentariile din cod: romana, fara diacritice.
- Dependente noi: niciuna. Fara migrare noua.
- System prompt-ul (`prompt.System`) NU depinde de felul run-ului: altfel hash-ul sesiunii se schimba intre chat si task si
  agentul pierde contextul la fiecare trecere.
- Commit-uri: `zona: descriere scurta in romana`.

## Review Focus

1. Trecerea chat -> task pastreaza sesiunea: system prompt-ul si hash-ul sunt identice intre un run fara task si unul pe task — test in Task 2 (`TestReadOnlyRunKeepsSession`).
2. Un lider care decide (Accept / Send back) intr-un run fara task inca isi poate folosi tool-urile MCP (create_task, accept_task, send_back) — doar Edit/Write/Bash sunt taiate; test in Task 2 verifica ca `mcp__team__*` ramane in allow.
3. Un agent fara sef si fara echipa (solo) primeste `start_task`; intr-un run pe task nu il primeste nimeni — test in Task 3 (lista de tool-uri).
4. `start_task` nu face agentul propriul decident (`CreatorID` gol) si respecta `ByUser` doar pentru mesaje din chat (nu raspunsuri/comentarii) — test in Task 3.
5. Refuzurile `Read` / `WebFetch` dintr-un run fara task tot fac cereri (doar Edit/Write/NotebookEdit/Bash se ignora) — test in Task 2.

---

### Task 1: Prompt — tool-urile doar de citire si linia din tura

**Files:**
- Modify: `internal/prompt/settings.go` (toolRules: `git-read`; `ReadOnlyTools`)
- Modify: `internal/prompt/prompt.go` (`ReadOnlyTurn`, `howSolo`)
- Test: `internal/prompt/settings_test.go`, `internal/prompt/prompt_test.go`

**Interfaces:**
- Produces:
  - `prompt.ReadOnlyTools(tools []string) []string`
  - `const prompt.ReadOnlyTurn string`

- [ ] **Step 1: Testele (esueaza)**

In `internal/prompt/settings_test.go`:

```go
func TestReadOnlyTools(t *testing.T) {
	got := ReadOnlyTools([]string{"write", "shell", "packages", "git", "web"})
	if !slices.Equal(got, []string{"git-read", "web"}) {
		t.Fatalf("%v", got)
	}
	if got := ReadOnlyTools(nil); len(got) != 0 {
		t.Fatalf("%v", got)
	}
	raw := Settings(ReadOnlyTools([]string{"write", "shell", "git"}), "/w", nil, nil, nil)
	var cfg struct {
		Permissions struct{ Allow, Deny []string } `json:"permissions"`
	}
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"Edit", "Write", "Bash", "Bash(git add:*)", "Bash(git commit:*)"} {
		for _, a := range cfg.Permissions.Allow {
			if a == bad || strings.HasPrefix(a, "Edit(") || strings.HasPrefix(a, "Write(") {
				t.Fatalf("%s in allow: %s", bad, raw)
			}
		}
	}
	for _, want := range []string{"Read", "mcp__team__*", "Bash(git status:*)", "Bash(git diff:*)", "Bash(git log:*)", "Bash(git show:*)", "Bash(git blame:*)"} {
		if !slices.Contains(cfg.Permissions.Allow, want) {
			t.Fatalf("lipseste %s: %s", want, raw)
		}
	}
}
```

In `internal/prompt/prompt_test.go`:

```go
func TestReadOnlyTurnAndSolo(t *testing.T) {
	if !strings.Contains(ReadOnlyTurn, "mcp__team__start_task") || !strings.Contains(ReadOnlyTurn, "read-only") {
		t.Fatal(ReadOnlyTurn)
	}
	got := System(store.Project{Name: "X"}, store.Agent{Name: "A"}, Links{})
	if strings.Contains(got, "do the requested work") || !strings.Contains(got, "mcp__team__start_task") {
		t.Fatalf("%s", got)
	}
}
```

- [ ] **Step 2: Ruleaza, verifica ca esueaza**

Run: `go test ./internal/prompt/ -run 'ReadOnly'`
Expected: FAIL la compilare (`ReadOnlyTools`, `ReadOnlyTurn` nedefinite).

- [ ] **Step 3: Implementarea**

`internal/prompt/settings.go`, in `toolRules` adauga (dupa `"git"`):

```go
	// git-read: git doar pentru citire, in run-urile fara task (nume intern, nu apare in UI)
	"git-read": {"Bash(git status:*)", "Bash(git diff:*)", "Bash(git log:*)", "Bash(git show:*)", "Bash(git blame:*)"},
```

In `Settings`, bucla `for _, name := range ValidTools` nu stie de `git-read`; schimba doar capul buclei, ca ordinea
veche sa ramana:

```go
	for _, name := range append(slices.Clone(ValidTools), "git-read") {
```

(restul buclei, cu `if !slices.Contains(tools, name) { continue }` si tratarea lui `write`, ramane la fel.)

La final, functia noua:

```go
// ReadOnlyTools: tool-urile unui run fara task (chat, notificari): fara write, shell si packages,
// git doar pentru citire; web ramane. Modificarile se fac doar pe task.
func ReadOnlyTools(tools []string) []string {
	out := []string{}
	for _, name := range tools {
		switch name {
		case "git":
			out = append(out, "git-read")
		case "web":
			out = append(out, "web")
		}
	}
	return out
}
```

`internal/prompt/prompt.go`:

```go
const howSolo = `You talk directly with the user and have no team. Answer their messages; any change to the project is done in a task you start with mcp__team__start_task.`

// ReadOnlyTurn: linia din tura unui run fara task (chat, notificari, comentarii). E in tura, nu in system
// prompt, ca sesiunea sa fie aceeasi pentru run-urile cu si fara task.
const ReadOnlyTurn = `This conversation is read-only: you can read and analyze, but not change files or run commands that change anything. ` +
	`When the user asks for a change, start a task (mcp__team__start_task for yourself, mcp__team__create_task for your team) ` +
	`and stop: the work continues in the task. For analysis or questions, answer here; create a task only if the user asks for one. ` +
	`If a comment on a finished task needs changes, start a follow-up task with continues set to that task.`
```

- [ ] **Step 4: Ruleaza testele**

Run: `go test ./internal/prompt/`
Expected: PASS (daca un test vechi cauta textul "do the requested work", actualizeaza-l la noul `howSolo`).

- [ ] **Step 5: Commit**

```bash
git add internal/prompt
git commit -m "prompt: tool-urile doar de citire pentru run-urile fara task si linia read-only din tura"
```

---

### Task 2: Run-urile fara task sunt doar de citire

**Files:**
- Modify: `internal/team/run.go` (`startRun`, `recordDenials`)
- Test: `internal/team/readonly_test.go` (nou)

**Interfaces:**
- Consumes: `prompt.ReadOnlyTools`, `prompt.ReadOnlyTurn` (Task 1)

- [ ] **Step 1: Testele (esueaza)**

`internal/team/readonly_test.go`:

```go
package team

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"foreman/internal/prompt"
	"foreman/internal/runner"
	"foreman/internal/store"
)

// allowOf intoarce permissions.allow din --settings-ul unui run.
func allowOf(t *testing.T, spec runner.Spec) []string {
	t.Helper()
	var cfg struct {
		Permissions struct{ Allow []string } `json:"permissions"`
	}
	if err := json.Unmarshal([]byte(argAfter(spec.Args, "--settings")), &cfg); err != nil {
		t.Fatal(err)
	}
	return cfg.Permissions.Allow
}

func chatRun(t *testing.T, f *fixture, a store.Agent, text string) runner.Spec {
	t.Helper()
	f.tm.work.Lock()
	_, err := f.tm.addUserMessage(a, text, nil, false, false)
	f.tm.work.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	mustReconcile(t, f)
	return f.fs.last(t, a)
}

func TestChatRunIsReadOnly(t *testing.T) {
	f := setup(t)
	f.rook.Tools = []string{"write", "shell", "git"}
	f.st.UpdateAgent(f.rook)
	f.st.AddCommandGrant(f.p.ID, f.rook.ID, nil, "make deploy") // grant de chat vechi: nu se mai aplica
	run := chatRun(t, f, f.rook, "change the discount rule")
	allow := allowOf(t, run)
	for _, a := range allow {
		if a == "Bash" || strings.HasPrefix(a, "Edit") || strings.HasPrefix(a, "Write") || a == "Bash(make deploy:*)" || a == "Bash(git commit:*)" {
			t.Fatalf("%s in allow la un run de chat: %v", a, allow)
		}
	}
	if !slices.Contains(allow, "mcp__team__*") || !slices.Contains(allow, "Bash(git log:*)") {
		t.Fatalf("%v", allow)
	}
	if !strings.Contains(argAfter(run.Args, "-p"), prompt.ReadOnlyTurn) {
		t.Fatal("lipseste linia read-only din tura")
	}
	f.fs.finish(run.ID, runner.Outcome{Status: store.RunDone})
	// run-ul pe task are tool-urile intregi
	f.task(t, "Change", f.rook, store.StatusQueued)
	mustReconcile(t, f)
	work := f.fs.last(t, f.rook)
	if !slices.Contains(allowOf(t, work), "Bash") || strings.Contains(argAfter(work.Args, "-p"), prompt.ReadOnlyTurn) {
		t.Fatalf("run pe task: %v", allowOf(t, work))
	}
}

// chat -> task: acelasi system prompt, deci aceeasi sesiune Claude (--resume)
func TestReadOnlyRunKeepsSession(t *testing.T) {
	f := setup(t)
	f.rook.Tools = []string{"write", "shell"}
	f.st.UpdateAgent(f.rook)
	run := chatRun(t, f, f.rook, "hi")
	f.fs.finish(run.ID, runner.Outcome{Status: store.RunDone, SessionID: "s-1"})
	f.task(t, "Change", f.rook, store.StatusQueued)
	mustReconcile(t, f)
	work := f.fs.last(t, f.rook)
	if argAfter(work.Args, "--append-system-prompt") != argAfter(run.Args, "--append-system-prompt") || argAfter(work.Args, "--resume") != "s-1" {
		t.Fatalf("sesiunea nu continua: resume=%q", argAfter(work.Args, "--resume"))
	}
}

// refuzurile de scriere dintr-un run fara task sunt intentionate; WebFetch tot face cerere
func TestChatRunWriteDenialsMakeNoRequest(t *testing.T) {
	f := setup(t)
	f.rook.Tools = []string{"write", "shell"}
	f.st.UpdateAgent(f.rook)
	f.tm.work.Lock()
	f.tm.recordDenials(activeRun{projectID: f.p.ID}, f.rook, []runner.Denial{
		{Tool: "Edit", Detail: "app/x.php"}, {Tool: "Write", Detail: "a"}, {Tool: "NotebookEdit"}, {Tool: "Bash", Detail: "php artisan test"},
		{Tool: "WebFetch", Detail: "https://x.dev"},
	})
	f.tm.work.Unlock()
	reqs, _ := f.st.ListOpenAccessRequests(f.p.ID)
	if len(reqs) != 1 || reqs[0].Tool != "WebFetch" {
		t.Fatalf("%+v", reqs)
	}
}
```

(`mustReconcile`, `f.fs.last`, `f.fs.finish`, `argAfter` exista in testele team; `runner.Outcome.SessionID` exista; tura e
argumentul de dupa `-p`.)

- [ ] **Step 2: Ruleaza, verifica ca esueaza**

Run: `go test ./internal/team/ -run 'ChatRun|ReadOnlyRun'`
Expected: FAIL pe aserturi (allow are `Bash`, lipseste linia; refuzurile fac cereri).

- [ ] **Step 3: `startRun`**

In `internal/team/run.go`, `startRun`, imediat dupa ce `turn` primeste blocul cu fisiere si Library (inainte de
`linkedProjects`), adauga:

```go
	// run fara task (chat, notificari, comentarii): doar citire; modificarile se fac pe task (start_task / create_task).
	// Doar permisiunile se schimba: system prompt-ul ramane acelasi, ca sesiunea sa continue intre chat si task.
	runTools := a.Tools
	if taskID == 0 {
		runTools = prompt.ReadOnlyTools(a.Tools)
		turn += "\n\n" + prompt.ReadOnlyTurn
	}
```

Apoi:
- grant-urile: `granted, err := t.st.CommandGrants(a.ID, taskID)` devine conditionat:

```go
	// grant-urile din "Needs you": ale task-ului run-ului; un run fara task nu primeste nimic
	var granted []string
	if taskID != 0 {
		if granted, err = t.st.CommandGrants(a.ID, taskID); err != nil {
			log.Printf("agent %d: command grants: %v", a.ID, err)
			granted = nil
		}
	}
```

- `CanWrite: slices.Contains(a.Tools, "write")` din `mcp.Caller` devine `CanWrite: slices.Contains(runTools, "write")`.
- `t.args(...)` primeste agentul cu tool-urile run-ului: inainte de `spec := runner.Spec{`:

```go
	runAgent := a
	runAgent.Tools = runTools
```

si `Args: append(t.args(p, runAgent, system, turn, mcpCfg, resume, libBuild, cmdDeny, granted), extraArgs...)`.
`system`, `hash` si `pl.BlockedCommands` raman calculate din `a` (tool-urile intregi).

- [ ] **Step 4: `recordDenials`**

In bucla din `recordDenials`, dupa verificarea `if d.Tool == "Task" || prompt.AlwaysDeniedTool(d.Tool) { continue }`:

```go
		// run fara task: scrierea e taiata intentionat (agentul porneste un task), nu e o cerere de acces
		if ar.taskID == 0 && slices.Contains([]string{"Edit", "Write", "NotebookEdit", "Bash"}, d.Tool) {
			continue
		}
```

- [ ] **Step 5: Ruleaza testele**

Run: `go test ./internal/team/`
Expected: PASS. Teste vechi care se pot schimba: cele care fac cereri de acces din run-uri fara task
(`activeRun{projectID: f.p.ID}` fara `taskID` in `commands_test.go`: `TestBlockCommandAndNonCommandRequest`,
`TestDenyingRuleFallback`, `TestDenialClassifiedOnFullCommand`, `TestBlockInProjectDismissesSamePrefix`,
`TestAllowInProjectSkipsAgentsWithoutShell`) si `TestAllowOnceGrantLifecycle` (run-ul de chat). Pentru fiecare: da-i un
task (`task := f.task(t, "X", <agent>, store.StatusInProgress)` si `activeRun{projectID: f.p.ID, taskID: task.ID}`),
fiindca regula testata e despre run-uri pe task. In `TestAllowOnceGrantLifecycle`, run-ul de chat trebuie acum sa aiba
`Bash(git push:*)` in deny si nici `Bash` in allow (read-only) — aserturea existenta "grant-ul task-ului a ajuns in chat"
ramane valabila.

- [ ] **Step 6: Commit**

```bash
git add internal/team
git commit -m "team: run-urile fara task sunt doar de citire; refuzurile de scriere din ele nu fac cereri"
```

---

### Task 3: Tool-ul `start_task`

**Files:**
- Modify: `internal/mcp/tools.go` (definitia, `toolsFor`)
- Modify: `internal/mcp/server.go` (`Backend.StartTask`, dispatch)
- Modify: `internal/mcp/server_test.go` (fakeBackend, listele de tool-uri)
- Modify: `internal/store/tasks.go` (`CountSelfStarted`)
- Create: `internal/team/start_task.go`
- Test: `internal/team/start_task_test.go`, `internal/store/tasks_test.go`

**Interfaces:**
- Produces:
  - MCP `start_task(title, description?, continues?)`
  - `mcp.Backend.StartTask(c Caller, title, description, continues string) (string, error)`
  - `(*Team) StartTask(c mcp.Caller, title, description, continues string) (string, error)`
  - `(*Store) CountSelfStarted(agentID int64) (int, error)`
  - `team.MaxSelfStarted = 3`

- [ ] **Step 1: Testele (esueaza)**

`internal/team/start_task_test.go`:

```go
package team

import (
	"strings"
	"testing"

	"foreman/internal/mcp"
	"foreman/internal/store"
)

func TestStartTaskFromChat(t *testing.T) {
	f := setup(t)
	run := chatRun(t, f, f.lead, "add the discount id to AWBDiscounts")
	c := f.caller(t, run)
	out, err := f.tm.StartTask(c, "Save DISCOUNTID", "on AWB generation", "")
	if err != nil || !strings.Contains(out, "Started T-1") {
		t.Fatalf("%q %v", out, err)
	}
	task := f.wantStatus(t, 1, store.StatusQueued)
	if task.AssigneeID != f.lead.ID || task.CreatorID != nil || !task.ByUser || task.StageID != nil {
		t.Fatalf("%+v", task)
	}
	if !f.hasMessage(f.lead.ID, "Started T-1: Save DISCOUNTID") {
		t.Fatal("lipseste nota din chat")
	}
	evs, _ := f.st.ListTaskEvents(task.ID)
	kinds := []string{}
	for _, e := range evs {
		kinds = append(kinds, e.Kind)
	}
	if strings.Join(kinds, ",") != "requested,created,self_started" {
		t.Fatalf("%v", kinds)
	}
}

func TestStartTaskRules(t *testing.T) {
	f := setup(t)
	// intr-un run pe task: refuzat
	task := f.task(t, "A", f.rook, store.StatusInProgress)
	if _, err := f.tm.StartTask(mcp.Caller{ProjectID: f.p.ID, AgentID: f.rook.ID, TaskID: task.ID}, "x", "", ""); err == nil || !strings.Contains(err.Error(), "already working on T-1") {
		t.Fatalf("%v", err)
	}
	// fara mesaj al utilizatorului (notificare): ByUser fals, decide seful
	c := mcp.Caller{ProjectID: f.p.ID, AgentID: f.rook.ID}
	if _, err := f.tm.StartTask(c, "Fix", "", ""); err != nil {
		t.Fatal(err)
	}
	got := f.wantStatus(t, 2, store.StatusQueued)
	if got.ByUser || got.CreatorID != nil {
		t.Fatalf("%+v", got)
	}
	// titlu gol
	if _, err := f.tm.StartTask(c, "  ", "", ""); err == nil {
		t.Fatal("titlu gol acceptat")
	}
	// limita: max 3 deschise pornite de agent
	f.tm.StartTask(c, "Two", "", "")
	f.tm.StartTask(c, "Three", "", "")
	if _, err := f.tm.StartTask(c, "Four", "", ""); err == nil || !strings.Contains(err.Error(), "3 open tasks") {
		t.Fatalf("%v", err)
	}
}

func TestStartTaskIgnoresStages(t *testing.T) {
	f := setup(t)
	if _, err := f.st.SaveStages(f.p.ID, []store.Stage{{Name: "Build", Kind: "work", AgentIDs: []int64{f.rook.ID}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.tm.StartTask(mcp.Caller{ProjectID: f.p.ID, AgentID: f.lead.ID}, "Mine", "", ""); err != nil {
		t.Fatal(err)
	}
	if got := f.wantStatus(t, 1, store.StatusQueued); got.StageID != nil || got.AssigneeID != f.lead.ID {
		t.Fatalf("%+v", got)
	}
}
```

In `internal/store/tasks_test.go`:

```go
func TestCountSelfStarted(t *testing.T) {
	s := newStore(t)
	p, lead := newProject(t, s)
	for i, kind := range []string{"self_started", "assigned", "self_started"} {
		task := Task{ProjectID: p.ID, Title: "t", AssigneeID: lead.ID}
		s.CreateTask(&task)
		s.AddTaskEvent(&TaskEvent{TaskID: task.ID, Kind: kind, Text: "x"})
		if i == 2 { // al treilea e Done: nu mai conteaza
			task.Status = StatusDone
			s.SaveTask(&task)
		}
	}
	if n, err := s.CountSelfStarted(lead.ID); err != nil || n != 1 {
		t.Fatalf("%d %v", n, err)
	}
}
```

In `internal/mcp/server_test.go`:
- `fakeBackend` primeste:

```go
func (f *fakeBackend) StartTask(c Caller, title, desc, continues string) (string, error) {
	f.calls = append(f.calls, "start_task|"+title+"|"+desc+"|"+continues)
	return "Started T-1", f.err
}
```

- in `TestToolsListByRole`, listele asteptate pentru callerii fara task (`lead`, `sublead`, `reviewer`, `solo`) primesc
  `start_task` inainte de `ask_user`: `"create_task,list_tasks,accept_task,send_back,start_task,ask_user"`,
  `"create_task,list_tasks,accept_task,send_back,report_done,tick_left_for_user,comment_task,start_task,ask_user"`,
  `"report_done,review_approve,review_reject,tick_left_for_user,comment_task,start_task,ask_user"`, `"start_task,ask_user"`;
  `worker` (TaskID 5) ramane neschimbat. In celelalte teste care compara liste exacte (`TestStageTools`,
  `TestAddAgentTools` etc.), adauga `,start_task` inainte de `,ask_user` doar unde callerul are `TaskID == 0`;
  `DraftID` / `SetupID` nu primesc `start_task`.
- un test de apel:

```go
func TestCallStartTask(t *testing.T) {
	fb, ts := setup(t)
	rpc(t, ts, "solo", `{"jsonrpc":"2.0","id":9,"method":"tools/call","params":{"name":"start_task","arguments":{"title":"Fix","description":"d","continues":"T-2"}}}`)
	if len(fb.calls) != 1 || fb.calls[0] != "start_task|Fix|d|T-2" {
		t.Fatalf("%v", fb.calls)
	}
	_, resp := rpc(t, ts, "worker", `{"jsonrpc":"2.0","id":10,"method":"tools/call","params":{"name":"start_task","arguments":{"title":"x"}}}`)
	if raw, _ := json.Marshal(resp); !strings.Contains(string(raw), "not available") {
		t.Fatalf("worker pe task: %s", raw)
	}
}
```

- [ ] **Step 2: Ruleaza, verifica ca esueaza**

Run: `go test ./internal/mcp/ ./internal/store/ ./internal/team/ -run 'StartTask|SelfStarted|ToolsList'`
Expected: FAIL la compilare (`StartTask`, `CountSelfStarted` nedefinite).

- [ ] **Step 3: MCP**

`internal/mcp/tools.go`:

```go
// startTaskTool: in run-urile fara task (chat), agentul isi porneste un task pentru o modificare ceruta.
var startTaskTool = Tool{
	Name:        "start_task",
	Description: "Starts a task assigned to you for a change the user asked for in this conversation. Changes are made only in tasks: call it, then stop; the task starts right after this run with the same conversation.",
	InputSchema: schema([]string{"title"}, map[string]any{
		"title":       str("Short, clear title"),
		"description": str("What needs to be done, acceptance criteria, relevant files"),
		"continues":   str(continuesArg),
	}),
}
```

In `toolsFor`, chiar inainte de `out = append(out, askUserTool)`:

```go
	// run fara task (chat, notificari): modificarile se fac doar pe task, pornit de agent
	if c.TaskID == 0 {
		out = append(out, startTaskTool)
	}
```

`internal/mcp/server.go`: in interfata `Backend`, dupa `CreateTask`:
`StartTask(c Caller, title, description, continues string) (string, error)`; in `call`, dupa `case "create_task":`:

```go
	case "start_task":
		text, err = s.backend.StartTask(c, arg("title"), arg("description"), arg("continues"))
```

- [ ] **Step 4: Store**

In `internal/store/tasks.go`:

```go
// CountSelfStarted: task-urile deschise (nu Done / arhivate) pornite de agent cu start_task (evenimentul self_started).
func (s *Store) CountSelfStarted(agentID int64) (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM tasks t WHERE t.assignee_id = ? AND t.status NOT IN ('done', 'archived')
		AND EXISTS (SELECT 1 FROM task_events e WHERE e.task_id = t.id AND e.kind = 'self_started')`, agentID).Scan(&n)
	return n, err
}
```

- [ ] **Step 5: Team**

`internal/team/start_task.go`:

```go
package team

import (
	"errors"
	"fmt"
	"strings"

	"foreman/internal/mcp"
	"foreman/internal/store"
)

// MaxSelfStarted: cate task-uri deschise pornite cu start_task poate avea un agent (anti-bucla).
const MaxSelfStarted = 3

// StartTask e tool-ul start_task: intr-un run fara task, agentul isi porneste un task pentru
// modificarea ceruta. CreatorID ramane gol (altfel si-ar decide singur task-ul): decide seful,
// iar pentru o cerere din chat (run-ul are request) decide utilizatorul. Fara etape.
func (t *Team) StartTask(c mcp.Caller, title, description, continues string) (string, error) {
	t.work.Lock()
	defer t.work.Unlock()
	if c.TaskID != 0 {
		ref := "a task"
		if task, err := t.st.GetTask(c.TaskID); err == nil {
			ref = task.Ref()
		}
		return "", fmt.Errorf("you are already working on %s; do the work here", ref)
	}
	title = strings.TrimSpace(title)
	if title == "" {
		return "", errors.New("title is required")
	}
	a, err := t.st.GetAgent(c.AgentID)
	if err != nil {
		return "", errors.New("your agent no longer exists")
	}
	open, err := t.st.CountSelfStarted(a.ID)
	if err != nil {
		return "", err
	}
	if open >= MaxSelfStarted {
		return "", fmt.Errorf("you already have %d open tasks you started yourself; finish those first", open)
	}
	threadID, err := t.threadFor(c.ProjectID, continues)
	if err != nil {
		return "", err
	}
	_, fromChat := t.requestOf(c.AgentID, c.RunID)
	task := store.Task{ProjectID: c.ProjectID, Title: title, Description: strings.TrimSpace(description),
		AssigneeID: a.ID, ThreadID: threadID, ByUser: fromChat}
	if err := t.st.CreateTask(&task); err != nil {
		return "", err
	}
	t.recordRequest(c, task, a.Name)
	t.taskMsg(task, "created", a.Name, a.Name, "Started by "+a.Name, briefBody(task.Title, task.Description))
	t.taskEvent(task, "self_started", a.Name, a.Name+" works on it")
	t.note(c.ProjectID, a.ID, fmt.Sprintf("Started %s: %s", task.Ref(), title), true)
	t.emit(c.ProjectID, "task", task)
	t.Kick(c.ProjectID)
	return fmt.Sprintf("Started %s. Stop now; the work continues in the task.", task.Ref()), nil
}
```

(Verifica: `t.note(..., true)` = nota livrata/vizibila in chat, ca la `CreateTask`; `recordRequest` si `briefBody` exista in `tools.go`.)

- [ ] **Step 6: Ruleaza testele**

Run: `go vet ./... && go test ./internal/mcp/ ./internal/store/ ./internal/team/`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/mcp internal/store internal/team
git commit -m "mcp: start_task porneste un task pentru agent din chat (max 3 deschise, fara etape)"
```

---

### Task 4: Verificare, verificare reala si documentatie

**Files:**
- Modify: `CLAUDE.md`, spec-ul (Status)

- [ ] **Step 1: Verificarile din conventii**

```bash
go vet ./... && go build -race ./... && go test ./... \
 && GOOS=darwin GOARCH=arm64 go build -o /dev/null ./cmd/foreman && GOOS=windows go build -o /dev/null ./cmd/foreman
```
Expected: totul trece.

- [ ] **Step 2: Verificare cu claude real (haiku, sub 1 cent)**

Intr-un repo temporar, ruleaza claude cu settings-ul unui run de chat (`prompt.Settings(prompt.ReadOnlyTools([]string{"write","shell","git"}), dir, nil, nil, nil)`, scos cu un mic program Go sau copiat dintr-un test) si `--permission-mode dontAsk`, cerand: `Edit README.md`, `echo x > a.txt`, `git log --oneline -1`, `git commit --allow-empty -m x`. Asteptat: Edit, redirectarea si commit refuzate (in `permission_denials`), `git log` permis. Daca redirectarea `>` trece, scrie in ledger si raporteaza (o comanda "de citire" ar putea scrie).

- [ ] **Step 3: Documentatie**

`CLAUDE.md`, dupa intrarea "aprobari pe task":

```
Gata (chat doar de citire, spec in docs/superpowers/specs/2026-10-09-foreman-chat-read-only-design.md): un run fara task
(chat, notificari, comentarii) primeste prompt.ReadOnlyTools (fara write/shell/packages, git doar citire) si linia
prompt.ReadOnlyTurn; system prompt-ul ramane acelasi (sesiunea continua). Modificarile se fac pe task: tool-ul MCP
start_task (doar in run-urile fara task) creeaza un task Queued pentru agent, fara etape, CreatorID gol, ByUser pentru
cererile din chat, max 3 deschise (evenimentul self_started). Refuzurile Edit/Write/Bash din run-urile fara task nu fac
cereri de acces.
```

Spec: `Status: implementat (2026-10-09).`

- [ ] **Step 4: Commit**

```bash
git add CLAUDE.md docs/superpowers/specs/2026-10-09-foreman-chat-read-only-design.md
git commit -m "docs: chat doar de citire, modificarile pe task"
```
