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
				MCPServers map[string]struct {
					URL string `json:"url"`
				} `json:"mcpServers"`
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
