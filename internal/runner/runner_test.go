//go:build !windows

package runner

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func fakeManager(t *testing.T) *Manager {
	t.Helper()
	bin, err := filepath.Abs("testdata/fakeclaude.sh")
	if err != nil {
		t.Fatal(err)
	}
	return NewManager(bin, t.TempDir())
}

// start porneste un run si intoarce un channel pe care vine Outcome-ul.
func start(t *testing.T, m *Manager, spec Spec) (*Run, chan Outcome) {
	t.Helper()
	done := make(chan Outcome, 1)
	run, err := m.Start(spec, func(o Outcome) { done <- o })
	if err != nil {
		t.Fatal(err)
	}
	return run, done
}

func wait(t *testing.T, done chan Outcome) Outcome {
	t.Helper()
	select {
	case o := <-done:
		return o
	case <-time.After(10 * time.Second):
		t.Fatal("run-ul nu s-a terminat in 10s")
		return Outcome{}
	}
}

func TestRunSuccess(t *testing.T) {
	t.Setenv("FAKE_MODE", "")
	m := fakeManager(t)
	run, done := start(t, m, Spec{ID: "r1", Key: "agent-1", Dir: t.TempDir(), Args: []string{"-p", "hello"}, Prompt: "hello"})
	o := wait(t, done)
	if o.RunID != "r1" || o.Status != "done" || o.SessionID != "sess-1" || o.CostUSD != 0.25 || !strings.Contains(o.Result, "hello") || o.Error != "" {
		t.Fatalf("outcome = %+v", o)
	}
	if run.Status() != "done" {
		t.Fatalf("Status() = %q", run.Status())
	}
	replay, _, finished := run.Subscribe()
	all := string(joinLines(replay))
	for _, want := range []string{`"type":"app_start"`, `"type":"app_stderr"`, `"type":"result"`, `"type":"app_done"`} {
		if !strings.Contains(all, want) {
			t.Errorf("istoricul nu contine %s", want)
		}
	}
	if !finished {
		t.Error("Subscribe dupa final trebuie sa intoarca done=true")
	}
	logBytes, err := os.ReadFile(filepath.Join(m.logDir, "r1.jsonl"))
	if err != nil || strings.Count(string(logBytes), "\n") < 4 {
		t.Fatalf("log incomplet: %v\n%s", err, logBytes)
	}
}

func TestRunBudgetExceeded(t *testing.T) {
	t.Setenv("FAKE_MODE", "budget")
	o := wait(t, second(start(t, fakeManager(t), Spec{Key: "a", Dir: t.TempDir()})))
	if o.Status != "failed" || !strings.Contains(o.Error, "Reached maximum budget") || o.CostUSD != 1.2 || o.Result != "" {
		t.Fatalf("outcome = %+v", o)
	}
}

func TestRunCrash(t *testing.T) {
	t.Setenv("FAKE_MODE", "crash")
	o := wait(t, second(start(t, fakeManager(t), Spec{Key: "a", Dir: t.TempDir()})))
	if o.Status != "failed" || o.Error == "" {
		t.Fatalf("outcome = %+v", o)
	}
}

func TestRunBusyAndStop(t *testing.T) {
	t.Setenv("FAKE_MODE", "slow")
	m := fakeManager(t)
	run, done := start(t, m, Spec{Key: "agent-7", Dir: t.TempDir()})
	if _, err := m.Start(Spec{Key: "agent-7", Dir: t.TempDir()}, nil); !errors.Is(err, ErrBusy) {
		t.Fatalf("al doilea run pe aceeasi cheie: %v", err)
	}
	if err := m.Stop(run.ID); err != nil {
		t.Fatal(err)
	}
	if o := wait(t, done); o.Status != "stopped" {
		t.Fatalf("dupa Stop: %+v", o)
	}
	t.Setenv("FAKE_MODE", "")
	if o := wait(t, second(start(t, m, Spec{Key: "agent-7", Dir: t.TempDir()}))); o.Status != "done" {
		t.Fatalf("dupa eliberare: %+v", o)
	}
}

func TestTrackerWithoutResult(t *testing.T) {
	var tr tracker
	tr.observe([]byte("nu e json"))
	tr.observe([]byte(`{"type":"system","session_id":"s"}`))
	o := tr.outcome("r", nil, false)
	if o.Status != "failed" || o.SessionID != "s" || o.Error == "" {
		t.Fatalf("outcome = %+v", o)
	}
}

func second(_ *Run, ch chan Outcome) chan Outcome { return ch }

func joinLines(lines [][]byte) []byte {
	var out []byte
	for _, l := range lines {
		out = append(out, l...)
		out = append(out, '\n')
	}
	return out
}
