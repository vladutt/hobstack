package setup

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"foreman/internal/runner"
)

// readySession: o analiza terminata cu o propunere (Ana + Bob) si sesiunea Claude "sid-1".
func readySession(t *testing.T) (*Manager, *fakeStarter, Session) {
	t.Helper()
	m, f := newTest(t)
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module x"), 0o644)
	s, err := m.Start([]string{dir}, "a shop", "sonnet")
	if err != nil {
		t.Fatal(err)
	}
	b := m.Wrap(innerBackend{})
	c, _ := b.Resolve(tokenOf(t, f.specs[0]))
	prop := `{"name":"Shop","goal":"g","context":"c","agents":[{"name":"Ana","role":"Lead","reportsTo":""},{"name":"Bob","role":"Dev","reportsTo":"Ana","tools":["write"]}]}`
	if _, err := b.ProposeSetup(c, json.RawMessage(prop)); err != nil {
		t.Fatal(err)
	}
	f.done[s.RunID](runner.Outcome{RunID: s.RunID, Status: "done", SessionID: "sid-1"})
	got, _ := m.Get(s.ID)
	if got.Status != StatusReady {
		t.Fatalf("%+v", got)
	}
	return m, f, got
}

func TestChatResumesWithCurrentProposal(t *testing.T) {
	m, f, s := readySession(t)
	cur := *s.Proposal
	cur.Agents[1].Tools = []string{"write", "shell"} // utilizatorul a bifat shell la Bob
	cur.Context = "edited by the user"
	got, err := m.Chat(s.ID, "add a QA agent", cur)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Busy || got.ChatRunID == "" || len(got.Chat) != 1 || got.Chat[0].Role != "user" {
		t.Fatalf("%+v", got)
	}
	spec := f.specs[len(f.specs)-1]
	if argAfter(spec.Args, "--resume") != "sid-1" || spec.Dir != s.Workdirs[0] {
		t.Fatalf("resume=%q dir=%q", argAfter(spec.Args, "--resume"), spec.Dir)
	}
	if !strings.Contains(spec.Prompt, "edited by the user") || !strings.Contains(spec.Prompt, "User: add a QA agent") {
		t.Fatalf("tura:\n%s", spec.Prompt)
	}
	if _, err := m.Chat(s.ID, "again", cur); !errors.Is(err, ErrBusy) {
		t.Fatalf("busy: %v", err)
	}

	// Foreman propune: pastreaza shell la Bob (ok); adauga shell la Vera (refuzat)
	b := m.Wrap(innerBackend{})
	c, _ := b.Resolve(tokenOf(t, spec))
	bad := `{"name":"Shop","goal":"g","context":"edited by the user","agents":[{"name":"Ana","role":"Lead","reportsTo":""},{"name":"Bob","role":"Dev","reportsTo":"Ana","tools":["write","shell"]},{"name":"Vera","role":"QA","reportsTo":"Ana","tools":["shell"]}]}`
	if _, err := b.ProposeSetup(c, json.RawMessage(bad)); err == nil || !strings.Contains(err.Error(), "shell") {
		t.Fatalf("shell nou: %v", err)
	}
	good := strings.Replace(bad, `"tools":["shell"]}`, `"tools":["write"]}`, 1)
	if _, err := b.ProposeSetup(c, json.RawMessage(good)); err != nil {
		t.Fatal(err)
	}
	f.done[spec.ID](runner.Outcome{RunID: spec.ID, Status: "done", SessionID: "sid-2", Result: "Added Vera (QA)."})
	got, _ = m.Get(s.ID)
	if got.Busy || got.Status != StatusReady || len(got.Proposal.Agents) != 3 || got.Version != s.Version+1 {
		t.Fatalf("dupa chat: %+v", got)
	}
	if last := got.Chat[len(got.Chat)-1]; last.Role != "foreman" || last.Text != "Added Vera (QA)." {
		t.Fatalf("%+v", last)
	}
	// Undo readuce propunerea trimisa de utilizator (cu editarile lui)
	u, err := m.Undo(s.ID)
	if err != nil || len(u.Proposal.Agents) != 2 || u.Proposal.Context != "edited by the user" {
		t.Fatalf("undo: %v %+v", err, u.Proposal)
	}
	// al doilea chat reia sesiunea noua
	m.Chat(s.ID, "rename Bob", *u.Proposal)
	if argAfter(f.specs[len(f.specs)-1].Args, "--resume") != "sid-2" {
		t.Fatal("trebuia reluata sesiunea sid-2")
	}
}

// un run de chat fara schimbare (doar o intrebare) nu lasa o intrare falsa pentru Undo
func TestChatQuestionOnlyKeepsUndo(t *testing.T) {
	m, f, s := readySession(t)
	m.Chat(s.ID, "why Bob?", *s.Proposal)
	spec := f.specs[len(f.specs)-1]
	f.done[spec.ID](runner.Outcome{RunID: spec.ID, Status: "done", Result: "Because."})
	if _, err := m.Undo(s.ID); err == nil {
		t.Fatal("nu exista nicio schimbare de anulat")
	}
	// un run de chat esuat: statusul ramane ready, eroarea apare in chat
	m.Chat(s.ID, "x", *s.Proposal)
	spec = f.specs[len(f.specs)-1]
	f.done[spec.ID](runner.Outcome{RunID: spec.ID, Status: "failed", Error: "boom"})
	got, _ := m.Get(s.ID)
	if got.Status != StatusReady || got.Busy || !strings.Contains(got.Chat[len(got.Chat)-1].Text, "boom") {
		t.Fatalf("%+v", got)
	}
}

func TestChatValidation(t *testing.T) {
	m, _, s := readySession(t)
	if _, err := m.Chat(s.ID, "  ", *s.Proposal); err == nil {
		t.Fatal("mesaj gol")
	}
	if _, err := m.Chat(s.ID, strings.Repeat("x", 4001), *s.Proposal); err == nil {
		t.Fatal("mesaj prea lung")
	}
	bad := *s.Proposal
	bad.Agents = nil
	if _, err := m.Chat(s.ID, "hi", bad); err == nil {
		t.Fatal("propunere invalida")
	}
	if _, err := m.Chat("nu-exista", "hi", *s.Proposal); !errors.Is(err, ErrNotFound) {
		t.Fatalf("%v", err)
	}
	// o analiza care ruleaza inca nu are chat
	m2, _ := newTest(t)
	running, _ := m2.Start(nil, "notes app", "sonnet")
	if _, err := m2.Chat(running.ID, "hi", *s.Proposal); !errors.Is(err, ErrNotReady) {
		t.Fatalf("%v", err)
	}
}

// modul folder: chat-ul reia sesiunea in folderul analizat, cu celelalte foldere ca --add-dir;
// numele existente raman interzise
func TestChatFolderMode(t *testing.T) {
	m, f := newTest(t)
	in, client, landing := folderInput(t)
	s, err := m.StartFolder(in, "", "sonnet")
	if err != nil {
		t.Fatal(err)
	}
	b := m.Wrap(innerBackend{})
	c, _ := b.Resolve(tokenOf(t, f.specs[0]))
	prop := `{"agents":[{"name":"Lia","role":"Landing lead","reportsTo":"Lead"}]}`
	if _, err := b.ProposeSetup(c, json.RawMessage(prop)); err != nil {
		t.Fatal(err)
	}
	f.done[s.RunID](runner.Outcome{RunID: s.RunID, Status: "done", SessionID: "sid-f"})
	got, _ := m.Get(s.ID)
	if _, err := m.Chat(s.ID, "add a worker", *got.Proposal); err != nil {
		t.Fatal(err)
	}
	spec := f.specs[len(f.specs)-1]
	if argAfter(spec.Args, "--resume") != "sid-f" || spec.Dir != landing || !hasArgPair(spec.Args, "--add-dir", client) || countFlag(spec.Args, "--add-dir") != 1 {
		t.Fatalf("dir=%s args=%v", spec.Dir, spec.Args)
	}
	if sys := argAfter(spec.Args, "--append-system-prompt"); !strings.Contains(sys, "EXISTING project") {
		t.Fatal("system prompt fara modul folder")
	}
	if !hasArgPair(spec.Args, "--permission-mode", "dontAsk") {
		t.Fatal("fara --permission-mode dontAsk")
	}
	c, _ = b.Resolve(tokenOf(t, spec))
	taken := `{"agents":[{"name":"Lia","role":"x","reportsTo":"Lead"},{"name":"rook","role":"x","reportsTo":"Lia"}]}`
	if _, err := b.ProposeSetup(c, json.RawMessage(taken)); err == nil || !strings.Contains(err.Error(), "already an agent") {
		t.Fatalf("nume luat: %v", err)
	}
	okProp := `{"agents":[{"name":"Lia","role":"x","reportsTo":"Lead"},{"name":"Max","role":"Dev","reportsTo":"Lia"}]}`
	if _, err := b.ProposeSetup(c, json.RawMessage(okProp)); err != nil {
		t.Fatal(err)
	}
	f.done[spec.ID](runner.Outcome{RunID: spec.ID, Status: "done", Result: "Added Max."})
	got, _ = m.Get(s.ID)
	if got.Proposal.Name != "Probe" || len(got.Proposal.Agents) != 2 || got.Title != "Probe · Landing" {
		t.Fatalf("%+v", got)
	}
}

// Stop in timpul chat-ului opreste run-ul de chat, nu analiza
func TestChatStop(t *testing.T) {
	m, f, s := readySession(t)
	got, _ := m.Chat(s.ID, "hi", *s.Proposal)
	if err := m.Stop(s.ID); err != nil || f.stopped[len(f.stopped)-1] != got.ChatRunID {
		t.Fatalf("stop: %v %v", err, f.stopped)
	}
	f.done[got.ChatRunID](runner.Outcome{RunID: got.ChatRunID, Status: "stopped"})
	after, _ := m.Get(s.ID)
	if after.Status != StatusReady || after.Busy || after.Chat[len(after.Chat)-1].Text != "Stopped." {
		t.Fatalf("%+v", after)
	}
	// un chat nou merge dupa stop
	if _, err := m.Chat(s.ID, "again", *after.Proposal); err != nil {
		t.Fatal(err)
	}
}

// fara folder: chat-ul ruleaza in acelasi folder temporar ca analiza
func TestChatSameTempDir(t *testing.T) {
	m, f := newTest(t)
	s, _ := m.Start(nil, "notes app", "sonnet")
	b := m.Wrap(innerBackend{})
	c, _ := b.Resolve(tokenOf(t, f.specs[0]))
	if _, err := b.ProposeSetup(c, json.RawMessage(good)); err != nil {
		t.Fatal(err)
	}
	f.done[s.RunID](runner.Outcome{RunID: s.RunID, Status: "done", SessionID: "sid-t"})
	got, _ := m.Get(s.ID)
	if len(got.Chat) != 0 || got.Chat == nil {
		t.Fatalf("chat trebuia gol, non-nil: %#v", got.Chat)
	}
	if _, err := m.Chat(s.ID, "hi", *got.Proposal); err != nil {
		t.Fatal(err)
	}
	if f.specs[1].Dir != f.specs[0].Dir {
		t.Fatalf("cwd diferit: %s vs %s", f.specs[1].Dir, f.specs[0].Dir)
	}
	if _, err := os.Stat(f.specs[1].Dir); err != nil {
		t.Fatal(err)
	}
}
