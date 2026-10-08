# Draft AI pentru agent nou: plan de implementare

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** pe pagina Agent nou, utilizatorul scrie ce lipseste din echipa si un run scurt Claude ii completeaza
formularul (nume, rol, responsabilitati, model, sef, reviewer, tool-uri, instructiuni).

**Architecture:** pachet nou `internal/draft`, construit ca `internal/setup` dar mult mai mic: sesiuni in memorie,
un run `claude -p` doar citire cu un singur tool MCP, `propose_agent`. Token-urile MCP ale draft-urilor se rezolva
inaintea celor din wizard si ale echipei (`draft.Wrap(sm.Wrap(tm))`). Web-ul porneste draft-ul si il expune pentru
polling; pagina `new-agent.js` completeaza doar campurile neatinse.

**Tech Stack:** Go 1.22+, JS vanilla (ES modules).

**Spec:** `docs/superpowers/specs/2026-10-08-foreman-agent-draft-design.md`

## Global Constraints

- Tot ce vede utilizatorul sau agentul e in engleza; comentariile din cod in romana (fara diacritice).
- `go vet ./...`, `go build -race ./cmd/foreman`, cross-compile darwin/arm64 si windows trec.
- Fara dependente noi. JS verificat cu `node --check` pe o copie `.mjs`.
- Costul draft-ului NU intra in "Total cost" (nu se scrie in `runs`): decizia din spec.
- Un singur draft activ per proiect; timeout 90 s (dupa care run-ul se opreste si draft-ul e `failed`).
- Cererea: 1..1000 caractere. Propunerea: nume 1..40, unic in proiect (fara diferenta de litere); rol <= 80;
  responsabilitati <= 2000; instructiuni/background <= 4000; tool-uri din `write, shell, packages, git, web`;
  model din `models.Valid`; `reportsTo`/`reviewedBy` = nume existent sau gol.
- Comenzile se ruleaza din `agentdash/`. Nu atinge aplicatia de pe 7777 si nici `~/.foreman`.

## Review Focus

1. Folderul proiectului nu exista (inca) pe disc: draft-ul ruleaza intr-un folder temporar, nu esueaza (test in Task 2).
2. Agentul cheama `propose_agent` de doua ori: a doua propunere o inlocuieste pe prima (test in Task 2).
3. Utilizatorul a scris deja numele: draft-ul nu il suprascrie (verificat in Task 4, pas manual + logica `touched`).
4. Draft pornit si pagina inchisa: run-ul se opreste singur la 90 s, nu ramane agatat (test cu ceas injectat in Task 2).
5. Al doilea "Draft" in acelasi proiect cat primul ruleaza: 409 cu mesaj clar (test in Task 3).

---

### Task 1: tool-ul MCP `propose_agent`

**Files:**
- Modify: `internal/mcp/server.go` (Caller.DraftID, Backend.ProposeAgent, case in `call`)
- Modify: `internal/mcp/tools.go` (proposeAgentTool, ramura in `toolsFor`)
- Modify: `internal/team/team.go` (Team.ProposeAgent: eroare, ca ProposeSetup)
- Modify: `internal/mcp/server_test.go` (fakeBackend.ProposeAgent + test)

**Interfaces:**
- Produces: `mcp.Caller.DraftID string`; `Backend.ProposeAgent(c Caller, args json.RawMessage) (string, error)`;
  tool `propose_agent` cu argumentele `name, role, responsibilities, model, reportsTo, reviewedBy, tools[], instructions, background, doneDef`.

- [ ] **Step 1: testul**

In `internal/mcp/server_test.go`, langa `fakeBackend.ProposeSetup` (fake-ul tine apelurile in `f.calls`):

```go
func (f *fakeBackend) ProposeAgent(c Caller, args json.RawMessage) (string, error) {
	f.calls = append(f.calls, "propose_agent|"+string(args))
	return "ok", f.err
}

func TestDraftCallerSeesOnlyProposeAgent(t *testing.T) {
	tools := toolsFor(Caller{DraftID: "d1"})
	if len(tools) != 1 || tools[0].Name != "propose_agent" {
		t.Fatalf("draft tools = %+v", tools)
	}
	if allowed(Caller{AgentID: 1, HasParent: true}, "propose_agent") {
		t.Fatal("un agent al echipei nu are propose_agent")
	}
}
```

- [ ] **Step 2: ruleaza, trebuie sa pice**

Run: `go test ./internal/mcp/ -run TestDraftCallerSeesOnlyProposeAgent`
Expected: FAIL (`unknown field DraftID`).

- [ ] **Step 3: implementarea**

`server.go`, in `Caller` dupa `SetupID`:

```go
	DraftID      string // != "" = draft-ul AI al unui agent nou; vede doar propose_agent
```

in `Backend`, dupa `ProposeSetup`:

```go
	ProposeAgent(c Caller, args json.RawMessage) (string, error)
```

in `call`, langa `case "propose_setup":`:

```go
	case "propose_agent":
		raw, _ := json.Marshal(p.Arguments)
		text, err = s.backend.ProposeAgent(c, raw)
```

`tools.go`:

```go
var proposeAgentTool = Tool{
	Name:        "propose_agent",
	Description: "Submits the proposed agent. Call it once, at the end. If you get an error, fix the proposal and call it again.",
	InputSchema: schema([]string{"name", "role", "responsibilities"}, map[string]any{
		"name":             str("Short, unique name, e.g. Herald"),
		"role":             str("The role, e.g. E-mail & Notifications Developer"),
		"responsibilities": str("Takes: what it is responsible for, and what it does not take"),
		"model":            map[string]any{"type": "string", "enum": []string{"opus", "sonnet", "haiku"}, "description": "The Claude model"},
		"reportsTo":        str("The name of its boss in the team; empty = reports to the user"),
		"reviewedBy":       str("Optional: the name of the agent who reviews its work"),
		"tools": map[string]any{
			"type":  "array",
			"items": map[string]any{"type": "string", "enum": []string{"write", "shell", "packages", "git", "web"}},
		},
		"instructions": str("How it works and what it never does"),
		"background":   str("What it knows about this project"),
		"doneDef":      str("What it hands over when it finishes"),
	}),
}
```

in `toolsFor`, primul rand:

```go
	if c.DraftID != "" {
		return []Tool{proposeAgentTool}
	}
```

`internal/team/team.go`, langa `ProposeSetup`:

```go
// ProposeAgent: doar draft-ul AI de agent are tool-ul (vezi internal/draft).
func (t *Team) ProposeAgent(mcp.Caller, json.RawMessage) (string, error) {
	return "", errors.New("propose_agent is only for agent drafts")
}
```

- [ ] **Step 4: ruleaza testele pachetelor atinse**

Run: `go test ./internal/mcp/ ./internal/team/ ./internal/setup/`
Expected: PASS (setup inglobeaza `mcp.Backend`, deci primeste metoda automat).

- [ ] **Step 5: commit**

```bash
git add internal/mcp internal/team/team.go
git commit -m "mcp: tool-ul propose_agent pentru draft-ul AI de agent"
```

### Task 2: pachetul `internal/draft`

**Files:**
- Create: `internal/draft/draft.go`
- Create: `internal/draft/draft_test.go`

**Interfaces:**
- Consumes: `mcp.Caller.DraftID`, `mcp.Backend.ProposeAgent` (Task 1); `runner.Spec`, `runner.Outcome`; `prompt.MCPConfig(url)`; `models.Valid`.
- Produces:

```go
type Input struct {
	ProjectID int64
	Dir       string        // folderul implicit al proiectului (poate lipsi de pe disc)
	Brief     string        // proiectul si echipa, text (construit de web)
	Names     []string      // agentii existenti (unicitate, reportsTo, reviewedBy)
	Ask, Model string
}
type Proposal struct {
	Name, Role, Responsibilities, Model, ReportsTo, ReviewedBy, Instructions, Background, DoneDef string
	Tools []string
}
type Draft struct {
	ID string `json:"id"`; ProjectID int64 `json:"projectId"`; Status string `json:"status"` // running|ready|failed|stopped
	Proposal *Proposal `json:"proposal"`; Error string `json:"error"`
}
func New(st Starter, baseURL, tempRoot string) *Manager
func (m *Manager) Start(in Input) (Draft, error)   // ErrBusy daca proiectul are deja un draft running
func (m *Manager) Get(id string) (Draft, bool)
func (m *Manager) Stop(id string) error
func (m *Manager) Wrap(inner mcp.Backend) mcp.Backend
var ErrBusy = errors.New("a draft is already running for this project")
```

JSON pentru `Proposal`: campuri camelCase (`name, role, responsibilities, model, reportsTo, reviewedBy, instructions, background, doneDef, tools`).

- [ ] **Step 1: testele**

`internal/draft/draft_test.go`:

```go
package draft

import (
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"foreman/internal/mcp"
	"foreman/internal/runner"
)

type fakeStarter struct {
	mu      sync.Mutex
	specs   []runner.Spec
	done    map[string]func(runner.Outcome)
	stopped []string
}

func (f *fakeStarter) Start(s runner.Spec, d func(runner.Outcome)) (*runner.Run, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.specs = append(f.specs, s)
	f.done[s.ID] = d
	return nil, nil
}
func (f *fakeStarter) Stop(id string) error {
	f.mu.Lock()
	f.stopped = append(f.stopped, id)
	f.mu.Unlock()
	return nil
}

type inner struct{ mcp.Backend }

func (inner) Resolve(string) (mcp.Caller, bool) { return mcp.Caller{}, false }

func newTest(t *testing.T) (*Manager, *fakeStarter) {
	f := &fakeStarter{done: map[string]func(runner.Outcome){}}
	return New(f, "http://127.0.0.1:1", t.TempDir()), f
}

func tokenOf(t *testing.T, s runner.Spec) string {
	t.Helper()
	for i, a := range s.Args {
		if a == "--mcp-config" {
			var cfg struct {
				MCPServers map[string]struct{ URL string `json:"url"` } `json:"mcpServers"`
			}
			json.Unmarshal([]byte(s.Args[i+1]), &cfg)
			url := cfg.MCPServers["team"].URL
			return url[strings.LastIndex(url, "/")+1:]
		}
	}
	t.Fatal("fara --mcp-config")
	return ""
}

func input(dir string) Input {
	return Input{ProjectID: 1, Dir: dir, Brief: "Project: Shop", Names: []string{"Foreman", "Forge"}, Ask: "someone for e-mails", Model: "sonnet"}
}

func TestDraftProposeAndReady(t *testing.T) {
	m, f := newTest(t)
	d, err := m.Start(input(t.TempDir()))
	if err != nil || d.Status != StatusRunning {
		t.Fatalf("%+v %v", d, err)
	}
	b := m.Wrap(inner{})
	c, ok := b.Resolve(tokenOf(t, f.specs[0]))
	if !ok || c.DraftID != d.ID {
		t.Fatalf("token nerezolvat: %+v", c)
	}
	bad, _ := json.Marshal(map[string]any{"name": "forge", "role": "x", "responsibilities": "y"})
	if _, err := b.ProposeAgent(c, bad); err == nil || !strings.Contains(err.Error(), "already") {
		t.Fatalf("nume duplicat acceptat: %v", err)
	}
	good, _ := json.Marshal(map[string]any{"name": "Herald", "role": "Mail dev", "responsibilities": "e-mails", "reportsTo": "Foreman", "tools": []string{"write"}, "model": "sonnet"})
	if _, err := b.ProposeAgent(c, good); err != nil {
		t.Fatal(err)
	}
	// a doua propunere o inlocuieste pe prima
	again, _ := json.Marshal(map[string]any{"name": "Courier", "role": "Mail dev", "responsibilities": "e-mails"})
	b.ProposeAgent(c, again)
	f.done[f.specs[0].ID](runner.Outcome{RunID: f.specs[0].ID, Status: "done"})
	got, _ := m.Get(d.ID)
	if got.Status != StatusReady || got.Proposal.Name != "Courier" {
		t.Fatalf("%+v", got)
	}
	if _, ok := b.Resolve(tokenOf(t, f.specs[0])); ok {
		t.Fatal("token-ul ramane valid dupa run")
	}
}

func TestDraftBusyMissingDirAndTimeout(t *testing.T) {
	m, f := newTest(t)
	fire := make(chan func(), 1)
	m.after = func(d time.Duration, fn func()) { fire <- fn } // ceasul testului
	d, err := m.Start(input("/nu/exista/deloc"))
	if err != nil {
		t.Fatal(err)
	}
	if f.specs[0].Dir == "/nu/exista/deloc" {
		t.Fatal("folderul lipsa trebuia inlocuit cu unul temporar")
	}
	if _, err := m.Start(input(t.TempDir())); err != ErrBusy {
		t.Fatalf("al doilea draft: %v", err)
	}
	(<-fire)() // au trecut 90 s
	if len(f.stopped) != 1 {
		t.Fatalf("run-ul nu a fost oprit: %v", f.stopped)
	}
	f.done[f.specs[0].ID](runner.Outcome{RunID: f.specs[0].ID, Status: "stopped"})
	if got, _ := m.Get(d.ID); got.Status != StatusFailed || !strings.Contains(got.Error, "too long") {
		t.Fatalf("%+v", got)
	}
}
```

- [ ] **Step 2: ruleaza, trebuie sa pice**

Run: `go test ./internal/draft/`
Expected: FAIL (pachetul nu are cod).

- [ ] **Step 3: implementarea**

`internal/draft/draft.go`:

```go
// Package draft: "Let AI draft this agent" de pe pagina Agent nou. Un run scurt,
// doar citire, cu un singur tool MCP (propose_agent). Sesiunile traiesc doar in memorie.
package draft

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"foreman/internal/mcp"
	"foreman/internal/models"
	"foreman/internal/prompt"
	"foreman/internal/runner"
)

const (
	StatusRunning = "running"
	StatusReady   = "ready"
	StatusFailed  = "failed"
	StatusStopped = "stopped"
)

// Timeout: cat are voie sa dureze un draft
const Timeout = 90 * time.Second

var (
	ErrBusy     = errors.New("a draft is already running for this project")
	ErrNotFound = errors.New("the draft does not exist (maybe the server was restarted)")
)

type Starter interface {
	Start(spec runner.Spec, onDone func(runner.Outcome)) (*runner.Run, error)
	Stop(runID string) error
}

type Input struct {
	ProjectID  int64
	Dir        string
	Brief      string
	Names      []string
	Ask, Model string
}

type Proposal struct {
	Name             string   `json:"name"`
	Role             string   `json:"role"`
	Responsibilities string   `json:"responsibilities"`
	Model            string   `json:"model"`
	ReportsTo        string   `json:"reportsTo"`
	ReviewedBy       string   `json:"reviewedBy"`
	Tools            []string `json:"tools"`
	Instructions     string   `json:"instructions"`
	Background       string   `json:"background"`
	DoneDef          string   `json:"doneDef"`
}

type Draft struct {
	ID        string    `json:"id"`
	ProjectID int64     `json:"projectId"`
	Status    string    `json:"status"`
	Proposal  *Proposal `json:"proposal"`
	Error     string    `json:"error"`

	runID, token, tempDir string
	names                 []string
	timedOut              bool
}

type Manager struct {
	starter  Starter
	baseURL  string
	tempRoot string
	// after: ceasul (inlocuit in teste); porneste fn dupa d
	after func(d time.Duration, fn func())

	mu     sync.Mutex
	drafts map[string]*Draft
	tokens map[string]string
}

func New(st Starter, baseURL, tempRoot string) *Manager {
	return &Manager{starter: st, baseURL: baseURL, tempRoot: tempRoot,
		after:  func(d time.Duration, fn func()) { time.AfterFunc(d, fn) },
		drafts: map[string]*Draft{}, tokens: map[string]string{}}
}

func randomHex(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func (m *Manager) Start(in Input) (Draft, error) {
	ask := strings.TrimSpace(in.Ask)
	if ask == "" || len([]rune(ask)) > 1000 {
		return Draft{}, errors.New("say in 1-1000 characters what the team is missing")
	}
	if !models.Valid(in.Model) {
		return Draft{}, errors.New("unknown model: " + in.Model)
	}
	m.mu.Lock()
	for _, d := range m.drafts {
		if d.ProjectID == in.ProjectID && d.Status == StatusRunning {
			m.mu.Unlock()
			return Draft{}, ErrBusy
		}
	}
	d := &Draft{ID: randomHex(8), ProjectID: in.ProjectID, Status: StatusRunning, runID: runner.NewID(), token: randomHex(16), names: in.Names}
	m.drafts[d.ID] = d
	m.tokens[d.token] = d.ID
	m.mu.Unlock()

	dir := in.Dir
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		// proiect fara folder pe disc (inca): ruleaza intr-un folder gol
		dir = filepath.Join(m.tempRoot, "draft-"+d.ID)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			m.fail(d.ID, err.Error())
			return Draft{}, err
		}
		d.tempDir = dir
	}
	turn := turnPrompt(in.Brief, ask)
	spec := runner.Spec{ID: d.runID, Key: "draft-" + d.ID, Dir: dir, Args: m.args(d, in.Model, turn), Prompt: turn}
	if _, err := m.starter.Start(spec, func(o runner.Outcome) { m.finish(d.ID, o) }); err != nil {
		m.fail(d.ID, err.Error())
		return Draft{}, err
	}
	m.after(Timeout, func() {
		m.mu.Lock()
		running := d.Status == StatusRunning
		if running {
			d.timedOut = true
		}
		m.mu.Unlock()
		if running {
			m.starter.Stop(d.runID)
		}
	})
	return m.snapshot(d), nil
}

func (m *Manager) args(d *Draft, model, turn string) []string {
	settings, _ := json.Marshal(map[string]any{"permissions": map[string]any{
		"allow": []string{"Read", "Glob", "Grep", "LS", "mcp__team__propose_agent"},
		"deny": []string{"Write", "Edit", "MultiEdit", "NotebookEdit", "Bash", "WebFetch", "WebSearch",
			"Agent", "TaskCreate", "TaskUpdate", "TaskList", "TaskGet", "TodoWrite"},
	}})
	return []string{
		"-p", turn,
		"--output-format", "stream-json", "--verbose",
		"--model", model,
		"--append-system-prompt", systemPrompt,
		"--setting-sources", "user",
		"--permission-mode", "dontAsk",
		"--settings", string(settings),
		"--mcp-config", prompt.MCPConfig(m.baseURL + "/mcp/" + d.token),
		"--strict-mcp-config",
	}
}

func (m *Manager) fail(id, msg string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if d, ok := m.drafts[id]; ok {
		d.Status, d.Error = StatusFailed, msg
		delete(m.tokens, d.token)
	}
}

func (m *Manager) finish(id string, o runner.Outcome) {
	m.mu.Lock()
	defer m.mu.Unlock()
	d, ok := m.drafts[id]
	if !ok || d.Status != StatusRunning {
		return
	}
	delete(m.tokens, d.token)
	if d.tempDir != "" {
		os.RemoveAll(d.tempDir)
	}
	switch {
	case d.Proposal != nil:
		d.Status = StatusReady
	case d.timedOut:
		d.Status, d.Error = StatusFailed, "The draft took too long. Try again with a shorter request."
	case o.Status == "stopped":
		d.Status = StatusStopped
	case o.Status == "failed":
		d.Status, d.Error = StatusFailed, firstNonEmpty(o.Error, o.Result, "the draft failed")
	default:
		d.Status, d.Error = StatusFailed, "The AI did not propose an agent. Try again."
	}
}

func (m *Manager) Get(id string) (Draft, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	d, ok := m.drafts[id]
	if !ok {
		return Draft{}, false
	}
	return m.snapshot(d), true
}

func (m *Manager) Stop(id string) error {
	m.mu.Lock()
	d, ok := m.drafts[id]
	m.mu.Unlock()
	if !ok {
		return ErrNotFound
	}
	return m.starter.Stop(d.runID)
}

// snapshot: o copie fara campurile interne (se cheama cu sau fara lock; citeste doar)
func (m *Manager) snapshot(d *Draft) Draft {
	out := Draft{ID: d.ID, ProjectID: d.ProjectID, Status: d.Status, Error: d.Error}
	if d.Proposal != nil {
		p := *d.Proposal
		out.Proposal = &p
	}
	return out
}

var allowedTools = []string{"write", "shell", "packages", "git", "web"}

func (m *Manager) propose(id string, raw json.RawMessage) (string, error) {
	var p Proposal
	if err := json.Unmarshal(raw, &p); err != nil {
		return "", errors.New("invalid proposal: " + err.Error())
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	d, ok := m.drafts[id]
	if !ok || d.Status != StatusRunning {
		return "", errors.New("this draft is no longer running")
	}
	p.Name = strings.TrimSpace(p.Name)
	has := func(name string) bool {
		return slices.ContainsFunc(d.names, func(n string) bool { return strings.EqualFold(n, strings.TrimSpace(name)) })
	}
	switch {
	case p.Name == "" || len([]rune(p.Name)) > 40:
		return "", errors.New("name: 1-40 characters")
	case has(p.Name):
		return "", fmt.Errorf("an agent named %s already exists; pick another name", p.Name)
	case strings.TrimSpace(p.Role) == "" || len([]rune(p.Role)) > 80:
		return "", errors.New("role: 1-80 characters")
	case len([]rune(p.Responsibilities)) > 2000:
		return "", errors.New("responsibilities: at most 2000 characters")
	case len([]rune(p.Instructions)) > 4000 || len([]rune(p.Background)) > 4000 || len([]rune(p.DoneDef)) > 4000:
		return "", errors.New("instructions, background and doneDef: at most 4000 characters each")
	case p.ReportsTo != "" && !has(p.ReportsTo):
		return "", fmt.Errorf("reportsTo: %s is not in the team; use one of %s or leave it empty", p.ReportsTo, strings.Join(d.names, ", "))
	case p.ReviewedBy != "" && !has(p.ReviewedBy):
		return "", fmt.Errorf("reviewedBy: %s is not in the team", p.ReviewedBy)
	case p.Model != "" && !models.Valid(p.Model):
		return "", errors.New("model: opus, sonnet or haiku")
	}
	for _, t := range p.Tools {
		if !slices.Contains(allowedTools, t) {
			return "", fmt.Errorf("unknown tool %q; use %s", t, strings.Join(allowedTools, ", "))
		}
	}
	d.Proposal = &p
	return "Proposal received. You are done: do nothing else.", nil
}

func (m *Manager) Wrap(inner mcp.Backend) mcp.Backend { return &backend{Backend: inner, m: m} }

type backend struct {
	mcp.Backend
	m *Manager
}

func (b *backend) Resolve(token string) (mcp.Caller, bool) {
	b.m.mu.Lock()
	id, ok := b.m.tokens[token]
	b.m.mu.Unlock()
	if ok {
		return mcp.Caller{DraftID: id}, true
	}
	return b.Backend.Resolve(token)
}

func (b *backend) ProposeAgent(c mcp.Caller, args json.RawMessage) (string, error) {
	if c.DraftID == "" {
		return "", errors.New("propose_agent is only for agent drafts")
	}
	return b.m.propose(c.DraftID, args)
}

func firstNonEmpty(s ...string) string {
	for _, x := range s {
		if strings.TrimSpace(x) != "" {
			return x
		}
	}
	return ""
}

const systemPrompt = `You design one new agent for an existing team of Claude Code agents. Read the project briefly ` +
	`(README, CLAUDE.md, the main folders) only if it helps; do not change anything. Then call ` +
	`mcp__team__propose_agent once. Rules: the name is short and not used by anyone in the team; the ` +
	`responsibilities do not overlap with other agents and say what the agent does not take; reportsTo is the lead ` +
	`whose area it fits (empty only if it should report to the user); tools: only what the work needs ` +
	`(write for code, shell for commands and tests, git for commits, web for research).`

func turnPrompt(brief, ask string) string {
	return brief + "\n\nWhat the user says the team is missing:\n" + ask
}
```

Verifica: `runner.NewID()` exista (`grep -n "func NewID" internal/runner`); daca are alt nume, foloseste-l. `models.Valid` e in `internal/models`.

- [ ] **Step 4: ruleaza testele**

Run: `go test ./internal/draft/ && go vet ./internal/draft/`
Expected: PASS

- [ ] **Step 5: commit**

```bash
git add internal/draft
git commit -m "draft: run scurt care propune un agent nou (propose_agent)"
```

### Task 3: API-ul si legarea in main

**Files:**
- Create: `internal/web/api_draft.go`
- Modify: `internal/web/server.go` (camp `drafts *draft.Manager`, `SetDraft`, rute)
- Modify: `cmd/foreman/main.go` (draft.New, Wrap, SetDraft)
- Test: `internal/web/draft_test.go`

**Interfaces:**
- Consumes: `draft.Manager.Start/Get/Stop`, `draft.ErrBusy`, `draft.Input` (Task 2); `s.st.GetProject`, `s.st.ListAgents`.
- Produces: `POST /api/projects/{id}/agents/draft {ask, model}` -> 201 `Draft`; `GET /api/drafts/{id}` -> `Draft`;
  `POST /api/drafts/{id}/stop` -> 204. 409 la `ErrBusy`, 404 la draft lipsa, 501 daca `drafts == nil`.

- [ ] **Step 1: testul**

`internal/web/draft_test.go`:

```go
package web

import (
	"encoding/json"
	"net/http"
	"strconv"
	"testing"

	"foreman/internal/draft"
	"foreman/internal/runner"
)

type draftStarter struct{ specs []runner.Spec }

func (d *draftStarter) Start(s runner.Spec, _ func(runner.Outcome)) (*runner.Run, error) {
	d.specs = append(d.specs, s)
	return nil, nil
}
func (d *draftStarter) Stop(string) error { return nil }

func TestDraftAPI(t *testing.T) {
	e := setup(t)
	st := &draftStarter{}
	e.srv.SetDraft(draft.New(st, "http://127.0.0.1:1", t.TempDir()))
	p := e.createProject(t, t.TempDir())
	path := "/api/projects/" + strconv.FormatInt(p.ID, 10) + "/agents/draft"
	code, raw := e.do(t, "POST", path, map[string]string{"ask": "someone for e-mails", "model": "sonnet"})
	if code != http.StatusCreated {
		t.Fatalf("%d %s", code, raw)
	}
	var d draft.Draft
	json.Unmarshal(raw, &d)
	if len(st.specs) != 1 || st.specs[0].Args[1] == "" {
		t.Fatal("run-ul nu a pornit")
	}
	if code, _ := e.do(t, "POST", path, map[string]string{"ask": "again", "model": "sonnet"}); code != http.StatusConflict {
		t.Fatalf("al doilea draft: %d", code)
	}
	if code, raw := e.do(t, "GET", "/api/drafts/"+d.ID, nil); code != http.StatusOK {
		t.Fatalf("%d %s", code, raw)
	}
	if code, _ := e.do(t, "GET", "/api/drafts/nope", nil); code != http.StatusNotFound {
		t.Fatalf("draft lipsa: %d", code)
	}
}
```

- [ ] **Step 2: ruleaza, trebuie sa pice**

Run: `go test ./internal/web/ -run TestDraftAPI`
Expected: FAIL (`SetDraft` nedefinit).

- [ ] **Step 3: implementarea**

`server.go`: in `Server` campul `drafts *draft.Manager` (import `"foreman/internal/draft"`), dupa `SetSetup`:

```go
// SetDraft activeaza "Let AI draft this agent".
func (s *Server) SetDraft(m *draft.Manager) { s.drafts = m }
```

rutele, langa cele de setup:

```go
	mux.HandleFunc("POST /api/projects/{id}/agents/draft", s.startDraft)
	mux.HandleFunc("GET /api/drafts/{id}", s.getDraft)
	mux.HandleFunc("POST /api/drafts/{id}/stop", s.stopDraft)
```

`api_draft.go`:

```go
package web

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"foreman/internal/draft"
	"foreman/internal/store"
)

func (s *Server) startDraft(w http.ResponseWriter, r *http.Request) {
	if s.drafts == nil {
		writeErr(w, http.StatusNotImplemented, "drafts are not available")
		return
	}
	id, err := pathID(r)
	if err != nil {
		fail(w, err)
		return
	}
	var in struct{ Ask, Model string }
	if err := decode(r, &in); err != nil {
		fail(w, err)
		return
	}
	p, err := s.st.GetProject(id)
	if err != nil {
		fail(w, err)
		return
	}
	agents, err := s.st.ListAgents(id)
	if err != nil {
		fail(w, err)
		return
	}
	d, err := s.drafts.Start(draft.Input{ProjectID: id, Dir: p.Workdir, Brief: draftBrief(p, agents),
		Names: agentNames(agents), Ask: in.Ask, Model: in.Model})
	switch {
	case errors.Is(err, draft.ErrBusy):
		writeErr(w, http.StatusConflict, err.Error())
	case err != nil:
		writeErr(w, http.StatusBadRequest, err.Error())
	default:
		writeJSON(w, http.StatusCreated, d)
	}
}

func (s *Server) getDraft(w http.ResponseWriter, r *http.Request) {
	if s.drafts == nil {
		writeErr(w, http.StatusNotFound, draft.ErrNotFound.Error())
		return
	}
	d, ok := s.drafts.Get(r.PathValue("id"))
	if !ok {
		writeErr(w, http.StatusNotFound, draft.ErrNotFound.Error())
		return
	}
	writeJSON(w, http.StatusOK, d)
}

func (s *Server) stopDraft(w http.ResponseWriter, r *http.Request) {
	if s.drafts == nil {
		writeErr(w, http.StatusNotFound, draft.ErrNotFound.Error())
		return
	}
	if err := s.drafts.Stop(r.PathValue("id")); err != nil {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func agentNames(agents []store.Agent) []string {
	out := make([]string, len(agents))
	for i, a := range agents {
		out[i] = a.Name
	}
	return out
}

// draftBrief: proiectul si echipa, pe scurt, pentru mesajul draft-ului
func draftBrief(p store.Project, agents []store.Agent) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Project: %s\nGoal: %s\n", p.Name, p.Goal)
	if p.Context != "" {
		ctx := []rune(p.Context)
		if len(ctx) > 3000 {
			ctx = ctx[:3000]
		}
		fmt.Fprintf(&b, "Shared context:\n%s\n", string(ctx))
	}
	b.WriteString("\nTeam:\n")
	byID := map[int64]string{}
	for _, a := range agents {
		byID[a.ID] = a.Name
	}
	for _, a := range agents {
		boss := "the user"
		if a.ParentID != nil {
			boss = byID[*a.ParentID]
		}
		fmt.Fprintf(&b, "- %s (%s), reports to %s. Takes: %s\n", a.Name, a.Role, boss, a.Responsibilities)
	}
	return b.String()
}
```

Verifica numele helper-elor (`writeErr`, `fail`, `decode`, `pathID`, `writeJSON`) in `internal/web/server.go`; sunt cele folosite in `api_setup.go`.

`cmd/foreman/main.go`, dupa `sm := setup.New(...)`:

```go
	// "Let AI draft this agent": token-urile lui se rezolva inaintea wizard-ului si a echipei
	dm := draft.New(mgr, url, filepath.Join(paths.Root, "setup"))
```

`mcp.NewServer(sm.Wrap(tm))` devine `mcp.NewServer(dm.Wrap(sm.Wrap(tm)))`, iar dupa `webSrv.SetSetup(sm)`: `webSrv.SetDraft(dm)`.
Import `"foreman/internal/draft"`.

- [ ] **Step 4: ruleaza**

Run: `go test ./internal/web/ ./internal/draft/ && go build ./cmd/foreman`
Expected: PASS

- [ ] **Step 5: commit**

```bash
git add internal/web/api_draft.go internal/web/draft_test.go internal/web/server.go cmd/foreman/main.go
git commit -m "web: API pentru draft-ul AI de agent"
```

### Task 4: caseta "Let AI draft this agent" pe pagina Agent nou

**Files:**
- Modify: `internal/web/static/js/new-agent.js`
- Modify: `internal/web/static/app.css`

**Interfaces:**
- Consumes: `POST /api/projects/{id}/agents/draft`, `GET /api/drafts/{id}`, `POST /api/drafts/{id}/stop` (Task 3).

- [ ] **Step 1: caseta si completarea formularului**

In `newAgentView`, dupa crearea campurilor si inainte de `form`:

```js
  // campurile atinse de utilizator nu se suprascriu cu propunerea AI
  const touched = new Set();
  for (const [key, node] of Object.entries({ name, role, takes, model, instructions, background, doneDef, parent, reviewer })) {
    node.addEventListener(node.tagName === "SELECT" ? "change" : "input", () => touched.add(key));
  }
  const ask = el("input", { type: "text", maxlength: 1000, placeholder: "e.g. someone to own the e-mail templates", "aria-label": "What is the team missing" });
  const draftBtn = el("button", { type: "button", class: "link-btn na-draft-btn", onclick: runDraft }, "Draft");
  const draftNote = el("span", { class: "hint" });
  const draftBox = el("section", { class: "na-draft" },
    el("span", { class: "np-spark", "aria-hidden": "true" }, "✦"),
    el("div", { class: "na-draft-text" }, el("strong", {}, "Let AI draft this agent"),
      el("span", { class: "hint" }, "Say what's missing from the team; it proposes name, role, responsibilities and where it reports."), draftNote),
    el("div", { class: "na-draft-ask" }, ask, draftBtn));
  ask.addEventListener("keydown", (e) => { if (e.key === "Enter") { e.preventDefault(); runDraft(); } });
  let draftId = null, timer = null;

  async function runDraft() {
    if (draftId) { // al doilea click opreste draft-ul in curs
      api(`/api/drafts/${draftId}/stop`, { method: "POST" }).catch(() => {});
      return;
    }
    if (!ask.value.trim()) { ask.focus(); return; }
    try {
      const d = await api(`/api/projects/${projectId}/agents/draft`, { method: "POST", body: { ask: ask.value.trim(), model: "sonnet" } });
      draftId = d.id;
      draftBtn.textContent = "Stop";
      draftNote.textContent = "Drafting…";
      poll();
    } catch (err) { toast(err.message); }
  }
  async function poll() {
    clearTimeout(timer);
    if (!draftId || !root.isConnected) return;
    let d;
    try { d = await api(`/api/drafts/${draftId}`); } catch (err) { finishDraft(err.message); return; }
    if (d.status === "running") { timer = setTimeout(poll, 1000); return; }
    if (d.status === "ready" && d.proposal) { fillFrom(d.proposal); finishDraft("Drafted. Check the fields, then Add to team."); return; }
    finishDraft(d.error || (d.status === "stopped" ? "Stopped." : "The draft failed."));
  }
  function finishDraft(msg) {
    draftId = null;
    draftBtn.textContent = "Draft";
    draftNote.textContent = msg;
  }
  // fillFrom: pune propunerea doar in campurile neatinse; sef/reviewer dupa nume
  function fillFrom(p) {
    const set = (key, node, value) => { if (value && !touched.has(key)) node.value = value; };
    set("name", name, p.name); set("role", role, p.role); set("takes", takes, p.responsibilities);
    set("model", model, p.model); set("instructions", instructions, p.instructions);
    set("background", background, p.background); set("doneDef", doneDef, p.doneDef);
    const idOf = (n) => agents.find((a) => a.name.toLowerCase() === (n || "").toLowerCase())?.id;
    if (!touched.has("parent")) parent.value = p.reportsTo ? String(idOf(p.reportsTo) ?? parent.value) : "";
    if (!touched.has("reviewer") && p.reviewedBy && idOf(p.reviewedBy)) reviewer.value = String(idOf(p.reviewedBy));
    if (Array.isArray(p.tools) && p.tools.length) for (const t of tools) t.box.checked = p.tools.includes(t.id);
    syncFolder();
    draw();
  }
```

si `draftBox` devine primul copil din `el("div", { class: "np-main" }, draftBox, ...)`.

`app.css`:

```css
.na-draft { display: flex; align-items: center; gap: 16px; padding: 18px 20px; border: 1.5px dashed var(--amber-line); border-radius: 14px; background: rgba(242, 181, 68, .025); flex-wrap: wrap; }
.na-draft-text { display: grid; gap: 2px; flex: 1 1 280px; min-width: 0; }
.na-draft-ask { display: flex; align-items: center; gap: 12px; flex: 1 1 360px; }
.na-draft-ask input { background: var(--deep); }
.na-draft-btn { color: var(--amber); font-weight: 700; font-size: 15px; }
```

- [ ] **Step 2: verificare**

Run: `cp internal/web/static/js/new-agent.js /tmp/na.mjs && node --check /tmp/na.mjs` (sau in scratchpad).
Apoi manual, pe o copie a bazei de date (`HOME=<tmp> ./foreman start --port 7799`): pagina Agent nou, scrie in caseta,
Draft; dupa ~30 s campurile se completeaza; un camp scris inainte nu se schimba. Screenshot ca in memoria
"verify-ui-with-screenshots".

- [ ] **Step 3: commit**

```bash
git add internal/web/static/js/new-agent.js internal/web/static/app.css
git commit -m "ui: Let AI draft this agent pe pagina Agent nou"
```

### Task 5: CLAUDE.md

- [ ] **Step 1:** in `CLAUDE.md`, sectiunea Stare, o linie:
  `Gata (draft AI pentru agent nou, spec in docs/superpowers/specs/2026-10-08-foreman-agent-draft-design.md): pachetul internal/draft, run scurt doar citire cu tool-ul propose_agent; pagina Agent nou completeaza campurile neatinse. Costul nu intra in Total cost.`
- [ ] **Step 2:** `git add CLAUDE.md` (doar linia ta: `git add -p` nu merge aici; foloseste `git hash-object` + `git update-index` daca fisierul are si alte modificari necomise) si commit `docs: draft AI in Stare`.
