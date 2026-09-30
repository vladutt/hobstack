package setup

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

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

// innerBackend: backend-ul echipei; aici nu rezolva nimic.
type innerBackend struct{ mcp.Backend }

func (innerBackend) Resolve(string) (mcp.Caller, bool) { return mcp.Caller{}, false }

func newTest(t *testing.T) (*Manager, *fakeStarter) {
	f := &fakeStarter{done: map[string]func(runner.Outcome){}}
	return New(f, "http://127.0.0.1:1", t.TempDir()), f
}

// tokenOf scoate token-ul MCP din argumentul --mcp-config (ultima parte a URL-ului).
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

const good = `{"name":"Shop","goal":"g","context":"c","agents":[{"name":"Ana","role":"Lead","responsibilities":"x","reportsTo":""}]}`

func TestManagerHappyPath(t *testing.T) {
	m, f := newTest(t)
	dir := t.TempDir()
	s, err := m.Start(dir, "un magazin", "haiku")
	if err != nil {
		t.Fatal(err)
	}
	spec := f.specs[0]
	if spec.Dir != dir || spec.ID != s.RunID || !strings.Contains(strings.Join(spec.Args, " "), "mcp__team__propose_setup") || !strings.Contains(strings.Join(spec.Args, " "), "--permission-mode dontAsk") {
		t.Fatalf("spec: %+v", spec)
	}
	b := m.Wrap(innerBackend{})
	c, ok := b.Resolve(tokenOf(t, spec))
	if !ok || c.SetupID != s.ID {
		t.Fatalf("resolve: %+v %v", c, ok)
	}
	if _, err := b.ProposeSetup(c, json.RawMessage(`{"name":"","agents":[]}`)); err == nil {
		t.Fatal("propunerea invalida trebuia refuzata")
	}
	if _, err := b.ProposeSetup(c, json.RawMessage(good)); err != nil {
		t.Fatal(err)
	}
	// a doua propunere valida o inlocuieste pe prima
	if _, err := b.ProposeSetup(c, json.RawMessage(strings.Replace(good, `"Shop"`, `"Shop 2"`, 1))); err != nil {
		t.Fatal(err)
	}
	f.done[spec.ID](runner.Outcome{RunID: spec.ID, Status: "done"})
	got, _ := m.Get(s.ID)
	if got.Status != StatusReady || got.Proposal == nil || got.Proposal.Name != "Shop 2" {
		t.Fatalf("sesiune: %+v", got)
	}
	if _, ok := b.Resolve(tokenOf(t, spec)); ok {
		t.Fatal("token-ul trebuia sters la final")
	}
}

func TestManagerNoProposalFails(t *testing.T) {
	m, f := newTest(t)
	s, _ := m.Start("", "o aplicatie de notite", "sonnet")
	spec := f.specs[0]
	if _, err := os.Stat(spec.Dir); err != nil || !strings.HasPrefix(spec.Dir, m.tempRoot) {
		t.Fatalf("fara folder trebuia un folder temporar: %s %v", spec.Dir, err)
	}
	f.done[spec.ID](runner.Outcome{RunID: spec.ID, Status: "done"})
	got, _ := m.Get(s.ID)
	if got.Status != StatusFailed || !strings.Contains(got.Error, "did not propose anything") {
		t.Fatalf("%+v", got)
	}
	if _, err := os.Stat(spec.Dir); !os.IsNotExist(err) {
		t.Fatal("folderul temporar trebuia sters")
	}
}

func TestManagerStopAndValidation(t *testing.T) {
	m, f := newTest(t)
	if _, err := m.Start("", "  ", "sonnet"); err == nil {
		t.Fatal("folder si descriere goale: trebuia eroare")
	}
	if _, err := m.Start(filepath.Join(t.TempDir(), "nu-exista"), "", "sonnet"); err == nil {
		t.Fatal("folder inexistent: trebuia eroare")
	}
	if _, err := m.Start("", "x", "gpt-5"); err == nil {
		t.Fatal("model invalid: trebuia eroare")
	}
	s, _ := m.Start("", "x", "sonnet")
	if err := m.Stop(s.ID); err != nil || len(f.stopped) != 1 {
		t.Fatalf("stop: %v %v", err, f.stopped)
	}
	f.done[s.RunID](runner.Outcome{RunID: s.RunID, Status: "stopped"})
	if got, _ := m.Get(s.ID); got.Status != StatusStopped {
		t.Fatalf("%+v", got)
	}
	if err := m.Stop("inexistent"); err != ErrNotFound {
		t.Fatalf("%v", err)
	}
}

// un folder gol fara descriere nu are ce analiza
func TestManagerEmptyFolderNeedsDescription(t *testing.T) {
	m, f := newTest(t)
	if _, err := m.Start(t.TempDir(), "", "sonnet"); err == nil || !strings.Contains(err.Error(), "the folder is empty") {
		t.Fatalf("am %v", err)
	}
	if len(f.specs) != 0 {
		t.Fatal("nu trebuia pornit niciun run")
	}
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main"), 0o644)
	if _, err := m.Start(dir, "", "sonnet"); err != nil {
		t.Fatalf("folder cu fisiere fara descriere e ok: %v", err)
	}
}

// lista pentru pagina Proiecte: cele mai noi primele, fara cele ascunse
func TestManagerListAndDismiss(t *testing.T) {
	m, f := newTest(t)
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main"), 0o644)
	a, _ := m.Start(dir, "", "sonnet")
	b, _ := m.Start("", "un blog despre motoare", "haiku")
	if a.Title != filepath.Base(dir) || b.Title != "un blog despre motoare" || b.Model != "haiku" {
		t.Fatalf("titluri: %q %q, model %q", a.Title, b.Title, b.Model)
	}
	list := m.List()
	if len(list) != 2 || list[0].ID != b.ID {
		t.Fatalf("lista: %+v", list)
	}

	// propunerea da titlul
	c, _ := m.Wrap(innerBackend{}).Resolve(tokenOf(t, f.specs[0]))
	if _, err := m.Wrap(innerBackend{}).ProposeSetup(c, json.RawMessage(good)); err != nil {
		t.Fatal(err)
	}
	if got, _ := m.Get(a.ID); got.Title != "Shop" {
		t.Fatalf("titlu dupa propunere: %q", got.Title)
	}

	// ascunderea unei analize care ruleaza o si opreste
	if err := m.Dismiss(b.ID); err != nil {
		t.Fatal(err)
	}
	if len(f.stopped) != 1 || f.stopped[0] != b.RunID {
		t.Fatalf("stop: %v", f.stopped)
	}
	if list := m.List(); len(list) != 1 || list[0].ID != a.ID {
		t.Fatalf("dupa dismiss: %+v", list)
	}
	// o sesiune ascunsa se poate inca deschide dupa id
	if _, ok := m.Get(b.ID); !ok {
		t.Fatal("sesiunea ascunsa trebuia sa existe inca")
	}

	// o sesiune terminata se ascunde fara stop
	f.done[a.RunID](runner.Outcome{RunID: a.RunID, Status: "done"})
	if err := m.Dismiss(a.ID); err != nil || len(f.stopped) != 1 {
		t.Fatalf("dismiss terminata: %v %v", err, f.stopped)
	}
	if len(m.List()) != 0 {
		t.Fatal("lista trebuia goala")
	}
	if err := m.Dismiss("inexistent"); err != ErrNotFound {
		t.Fatalf("%v", err)
	}
}

func TestTitleFromLongDescription(t *testing.T) {
	m, _ := newTest(t)
	s, _ := m.Start("", strings.Repeat("abcde ", 30)+"\nlinia doi", "sonnet")
	if len([]rune(s.Title)) > 61 || !strings.HasSuffix(s.Title, "…") {
		t.Fatalf("titlu: %q", s.Title)
	}
}
