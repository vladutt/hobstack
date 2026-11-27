package team

import (
	"encoding/json"
	"strings"
	"testing"

	"hobstack/internal/store"
)

func TestRememberDefaultsToRunTask(t *testing.T) {
	f := setup(t)
	task := f.task(t, "Dates", f.rook, store.StatusInProgress)
	events, cancel := f.tm.hub.Subscribe(f.p.ID)
	defer cancel()
	text, err := f.tm.Remember(f.workerCaller(f.rook, task.ID), "UI Dates", "show dates as DD.MM.YYYY", "", "")
	if err != nil || text != "Saved ui-dates." {
		t.Fatalf("%q %v", text, err)
	}
	m, _ := f.st.GetMemoryByKey(f.p.ID, "ui-dates")
	if m.TaskID != task.ID || m.AuthorName != "Rook" || m.AuthorAgentID != f.rook.ID {
		t.Fatalf("%+v", m)
	}
	evs, _ := f.st.ListTaskEvents(task.ID)
	last := evs[len(evs)-1]
	// Body nevid: pasul apare si in Conversation, nu doar in Activity
	if last.Kind != "memory" || last.Text != "Rook saved memory ui-dates: show dates as DD.MM.YYYY" || last.Body == "" {
		t.Fatalf("%+v", last)
	}
	types := map[string]bool{}
	for len(events) > 0 {
		var ev struct{ Type string }
		json.Unmarshal(<-events, &ev)
		types[ev.Type] = true
	}
	if !types["memory"] || !types["task"] {
		t.Fatalf("events: %v", types)
	}
	if _, err := f.tm.Forget(f.workerCaller(f.rook, task.ID), "ui-dates", "we use ISO now"); err != nil {
		t.Fatal(err)
	}
	evs, _ = f.st.ListTaskEvents(task.ID)
	if last := evs[len(evs)-1]; last.Kind != "memory" || last.Text != "Rook forgot memory ui-dates: we use ISO now" {
		t.Fatalf("forget event: %+v", last)
	}
}

func TestRememberExplicitTask(t *testing.T) {
	f := setup(t)
	f.task(t, "One", f.rook, store.StatusDone)
	two := f.task(t, "Two", f.rook, store.StatusDone)
	c := f.workerCaller(f.rook, 0)
	if _, err := f.tm.Remember(c, "a.b", "x", "", two.Ref()); err != nil {
		t.Fatal(err)
	}
	if m, _ := f.st.GetMemoryByKey(f.p.ID, "a.b"); m.TaskID != two.ID {
		t.Fatalf("%+v", m)
	}
	if _, err := f.tm.Remember(c, "a.c", "x", "", "T-99"); err == nil || !strings.Contains(err.Error(), "T-99 does not exist") {
		t.Fatalf("%v", err)
	}
}

func TestRememberLockedRefused(t *testing.T) {
	f := setup(t)
	m, _ := f.st.CreateMemory(f.p.ID, store.MemoryWrite{Key: "ui.dates", Summary: "a", AuthorName: "You"}, true)
	c := f.workerCaller(f.rook, 0)
	_, err := f.tm.Remember(c, "ui.dates", "b", "", "")
	if err == nil || err.Error() != "ui.dates is locked by the user. If it is wrong, tell the user (ask_user or your report)." {
		t.Fatalf("%v", err)
	}
	if _, err := f.tm.Forget(c, "ui.dates", "old"); err == nil || !strings.Contains(err.Error(), "locked by the user") {
		t.Fatalf("%v", err)
	}
	if got, _ := f.st.GetMemory(m.ID); got.Summary != "a" || got.DeletedAt != nil {
		t.Fatalf("%+v", got)
	}
}

func TestRememberRevivesUserDeleted(t *testing.T) {
	f := setup(t)
	m, _ := f.st.CreateMemory(f.p.ID, store.MemoryWrite{Key: "ui.dates", Summary: "a", AuthorName: "You"}, false)
	f.st.ForgetMemory(m.ID, store.MemoryWrite{AuthorName: "You"}, "we use ISO now", true)
	text, err := f.tm.Remember(f.workerCaller(f.rook, 0), "ui.dates", "b", "", "")
	if err != nil || !strings.Contains(text, "was deleted by You on") || !strings.Contains(text, "we use ISO now") {
		t.Fatalf("%q %v", text, err)
	}
}

func TestRememberUpdateReply(t *testing.T) {
	f := setup(t)
	c := f.workerCaller(f.rook, 0)
	f.tm.Remember(c, "ui.dates", "old", "", "")
	if text, _ := f.tm.Remember(c, "ui.dates", "new", "", ""); text != "Updated ui.dates (was: old)." {
		t.Fatalf("%q", text)
	}
	if text, _ := f.tm.Remember(c, "ui.dates", "new", "", ""); text != "ui.dates is already saved with this text." {
		t.Fatalf("%q", text)
	}
}

func TestRecall(t *testing.T) {
	f := setup(t)
	task := f.task(t, "Dates", f.rook, store.StatusInProgress)
	c := f.workerCaller(f.rook, task.ID)
	f.tm.Remember(c, "ui.dates", "DD.MM.YYYY", "Everywhere, also in exports.", "")
	text, err := f.tm.Recall(c, "ui.dates")
	for _, want := range []string{"ui.dates: DD.MM.YYYY", "Everywhere, also in exports.", "Saved by Rook", task.Ref()} {
		if err != nil || !strings.Contains(text, want) {
			t.Fatalf("missing %q in %q (%v)", want, text, err)
		}
	}
	f.tm.Forget(c, "ui.dates", "replaced by ISO")
	if text, _ := f.tm.Recall(c, "ui.dates"); !strings.Contains(text, "was forgotten by Rook on") || !strings.Contains(text, "replaced by ISO") {
		t.Fatalf("%q", text)
	}
	f.tm.Remember(c, "ui.times", "24h", "", "")
	if text, _ := f.tm.Recall(c, "ui.time"); text != "No memory ui.time. Closest keys: ui.times" {
		t.Fatalf("%q", text)
	}
}

func TestSearchAndForgetTools(t *testing.T) {
	f := setup(t)
	c := f.workerCaller(f.rook, 0)
	f.tm.Remember(c, "ui.dates", "DD.MM.YYYY", "", "")
	f.tm.Remember(c, "api.auth", "tokens in Redis", "", "")
	if text, _ := f.tm.SearchMemory(c, "redis"); text != "- api.auth: tokens in Redis" {
		t.Fatalf("%q", text)
	}
	if text, _ := f.tm.SearchMemory(c, "nothing"); text != "No memory matches \"nothing\"." {
		t.Fatalf("%q", text)
	}
	if text, err := f.tm.Forget(c, "ui.dates", "wrong"); err != nil || text != "Forgot ui.dates." {
		t.Fatalf("%q %v", text, err)
	}
	if _, err := f.tm.Forget(c, "ui.nope", ""); err == nil {
		t.Fatal("forgot an unknown key")
	}
}

func TestRunTurnHasMemoryIndex(t *testing.T) {
	f := setup(t)
	// fara memorie: fara bloc
	if _, err := f.tm.UserMessage(f.lead.ID, "hello", nil); err != nil {
		t.Fatal(err)
	}
	mustReconcile(t, f)
	if p := f.fs.last(t, f.lead).Prompt; strings.Contains(p, "Project memory") {
		t.Fatalf("empty memory in turn: %s", p)
	}
	f.st.CreateMemory(f.p.ID, store.MemoryWrite{Key: "ui.dates", Summary: "DD.MM.YYYY", AuthorName: "You"}, false)
	f.task(t, "Dates", f.rook, store.StatusQueued)
	mustReconcile(t, f)
	if p := f.fs.last(t, f.rook).Prompt; !strings.Contains(p, "mcp__team__recall):\n- ui.dates: DD.MM.YYYY (You)") {
		t.Fatalf("task turn: %s", p)
	}
	// brainstorming: indexul fara recall (run-ul n-are tool-urile de memorie)
	tp := topicWith(t, f, f.vera)
	spec := startCard(t, f, f.vera, roundFor(t, f, tp, f.vera))
	if !strings.Contains(spec.Prompt, "Project memory (decisions and facts saved by the team):\n- ui.dates") ||
		strings.Contains(spec.Prompt, "mcp__team__recall") {
		t.Fatalf("brainstorm turn: %s", spec.Prompt)
	}
}
