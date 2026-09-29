package team

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"foreman/internal/events"
	"foreman/internal/mcp"
	"foreman/internal/runner"
	"foreman/internal/store"
)

// fakeStarter inlocuieste runner-ul: inregistreaza ce s-ar fi pornit, iar
// testul decide cand si cum se termina fiecare run (finish).
type fakeStarter struct {
	mu      sync.Mutex
	specs   []runner.Spec
	onDone  map[string]func(runner.Outcome)
	stopped []string
}

func newFakeStarter() *fakeStarter {
	return &fakeStarter{onDone: map[string]func(runner.Outcome){}}
}

func (f *fakeStarter) Start(spec runner.Spec, onDone func(runner.Outcome)) (*runner.Run, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.specs = append(f.specs, spec)
	f.onDone[spec.ID] = onDone
	return nil, nil
}

func (f *fakeStarter) Stop(id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stopped = append(f.stopped, id)
	return nil
}

func (f *fakeStarter) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.specs)
}

// last intoarce ultimul run pornit pentru agent.
func (f *fakeStarter) last(t *testing.T, a store.Agent) runner.Spec {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	key := fmt.Sprintf("agent-%d", a.ID)
	for i := len(f.specs) - 1; i >= 0; i-- {
		if f.specs[i].Key == key {
			return f.specs[i]
		}
	}
	t.Fatalf("niciun run pentru %s", a.Name)
	return runner.Spec{}
}

// finish simuleaza terminarea procesului claude.
func (f *fakeStarter) finish(id string, o runner.Outcome) {
	f.mu.Lock()
	cb := f.onDone[id]
	f.mu.Unlock()
	o.RunID = id
	cb(o)
}

type fixture struct {
	st   *store.Store
	tm   *Team
	fs   *fakeStarter
	p    store.Project
	lead store.Agent
	rook store.Agent
	vera store.Agent
}

// setup: proiect cu lead + doi workeri (Rook poate scrie fisiere, Vera nu).
func setup(t *testing.T) *fixture {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })

	p := store.Project{Name: "ApiUnit", Goal: "Import OpenAPI", Workdir: t.TempDir(), MaxParallel: 2}
	lead, err := st.CreateProject(&p)
	if err != nil {
		t.Fatal(err)
	}
	rook := store.Agent{ProjectID: p.ID, Name: "Rook", Role: "Backend Dev", ParentID: &lead.ID, Model: "sonnet", Tools: []string{"write"}}
	vera := store.Agent{ProjectID: p.ID, Name: "Vera", Role: "QA", ParentID: &lead.ID, Model: "haiku"}
	for _, a := range []*store.Agent{&rook, &vera} {
		if err := st.CreateAgent(a); err != nil {
			t.Fatal(err)
		}
	}
	fs := newFakeStarter()
	tm := New(st, fs, events.NewHub(), "http://127.0.0.1:7777")
	return &fixture{st: st, tm: tm, fs: fs, p: p, lead: lead, rook: rook, vera: vera}
}

// agent adauga un agent in proiectul fixture-ului; parent/reviewer pot fi nil.
func (f *fixture) agent(t *testing.T, name string, parent, reviewer *store.Agent) store.Agent {
	t.Helper()
	a := store.Agent{ProjectID: f.p.ID, Name: name, Role: "Dev", Model: "haiku"}
	if parent != nil {
		a.ParentID = &parent.ID
	}
	if reviewer != nil {
		a.ReviewerID = &reviewer.ID
	}
	if err := f.st.CreateAgent(&a); err != nil {
		t.Fatal(err)
	}
	return a
}

func (f *fixture) leadCaller() mcp.Caller {
	return mcp.Caller{ProjectID: f.p.ID, AgentID: f.lead.ID, Lead: true}
}

func (f *fixture) workerCaller(a store.Agent, taskID int64) mcp.Caller {
	return mcp.Caller{ProjectID: f.p.ID, AgentID: a.ID, TaskID: taskID}
}

// caller gaseste identitatea MCP a unui run pornit, din --mcp-config.
func (f *fixture) caller(t *testing.T, spec runner.Spec) mcp.Caller {
	t.Helper()
	var cfg struct {
		MCPServers map[string]struct {
			URL string `json:"url"`
		} `json:"mcpServers"`
	}
	if err := json.Unmarshal([]byte(argValue(spec.Args, "--mcp-config")), &cfg); err != nil {
		t.Fatal(err)
	}
	url := cfg.MCPServers["team"].URL
	c, ok := f.tm.Resolve(url[strings.LastIndex(url, "/")+1:])
	if !ok {
		t.Fatalf("token inactiv pentru run-ul %s", spec.ID)
	}
	return c
}

// task creeaza un task direct in store, cu statusul dorit.
func (f *fixture) task(t *testing.T, title string, who store.Agent, status string) store.Task {
	t.Helper()
	task := store.Task{ProjectID: f.p.ID, Title: title, AssigneeID: who.ID}
	if err := f.st.CreateTask(&task); err != nil {
		t.Fatal(err)
	}
	if status != store.StatusQueued {
		task.Status = status
		if err := f.st.SaveTask(&task); err != nil {
			t.Fatal(err)
		}
	}
	return task
}

func (f *fixture) wantStatus(t *testing.T, number int, status string) store.Task {
	t.Helper()
	task, err := f.st.GetTaskByNumber(f.p.ID, number)
	if err != nil {
		t.Fatal(err)
	}
	if task.Status != status {
		t.Fatalf("T-%d are status %q, vreau %q", number, task.Status, status)
	}
	return task
}

// hasMessage cauta un mesaj care contine textul in thread-ul agentului.
func (f *fixture) hasMessage(agentID int64, text string) bool {
	msgs, _ := f.st.ListMessages(agentID)
	for _, m := range msgs {
		if strings.Contains(m.Text, text) {
			return true
		}
	}
	return false
}

func argValue(args []string, flag string) string {
	for i, a := range args {
		if a == flag && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}

func hasFlag(args []string, flag string) bool {
	for _, a := range args {
		if a == flag {
			return true
		}
	}
	return false
}
