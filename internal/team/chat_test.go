package team

import (
	"errors"
	"testing"

	"hobstack/internal/runner"
)

func TestNewChatStartsFreshSession(t *testing.T) {
	f := setup(t)
	if _, err := f.tm.UserMessage(f.lead.ID, "plan the sprint", nil); err != nil {
		t.Fatal(err)
	}
	mustReconcile(t, f)
	run1 := f.fs.last(t, f.lead)

	// cat timp lucreaza, New chat e refuzat
	if _, err := f.tm.NewChat(f.lead.ID); !errors.Is(err, ErrAgentBusy) {
		t.Fatalf("vreau ErrAgentBusy, am %v", err)
	}
	f.fs.finish(run1.ID, runner.Outcome{Status: "done", SessionID: "s-lead", Result: "ok"})

	m, err := f.tm.NewChat(f.lead.ID)
	if err != nil {
		t.Fatal(err)
	}
	if m.Text != "New chat started." {
		t.Fatalf("nota: %q", m.Text)
	}
	if _, err := f.tm.UserMessage(f.lead.ID, "fresh start", nil); err != nil {
		t.Fatal(err)
	}
	mustReconcile(t, f)
	run2 := f.fs.last(t, f.lead)
	if run2.ID == run1.ID || hasFlag(run2.Args, "--resume") {
		t.Fatalf("vreau sesiune noua, fara --resume: %v", run2.Args)
	}
	msgs, earlier, err := f.st.ListChat(f.lead.ID)
	if err != nil || earlier == 0 || msgs[0].Text != "New chat started." {
		t.Fatalf("chat: %d ascunse, primul %+v, %v", earlier, msgs, err)
	}
}
