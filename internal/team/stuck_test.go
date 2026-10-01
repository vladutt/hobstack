package team

import (
	"errors"
	"strings"
	"testing"
	"time"

	"foreman/internal/mcp"
	"foreman/internal/store"
)

// markDelivered goleste coada agentului, ca sa fie idle pentru detector.
func markDelivered(t *testing.T, f *fixture, a store.Agent) {
	t.Helper()
	msgs, err := f.st.Undelivered(a.ID)
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]int64, len(msgs))
	for i, m := range msgs {
		ids[i] = m.ID
	}
	if err := f.st.MarkDelivered(ids); err != nil {
		t.Fatal(err)
	}
}

// undelivered intoarce mesajele din coada agentului.
func undelivered(t *testing.T, f *fixture, a store.Agent) []store.Message {
	t.Helper()
	msgs, err := f.st.Undelivered(a.ID)
	if err != nil {
		t.Fatal(err)
	}
	return msgs
}

// decision: Rook a raportat T-1 (review, stage lead), Lead e idle -> o impingere, apoi vizibil
func TestStuckDecisionNudgedOnceThenVisible(t *testing.T) {
	f := setup(t)
	f.st.SetPaused(f.p.ID, false)
	task := f.task(t, "A", f.rook, store.StatusReview)
	if task.ReviewStage != store.StageLead {
		t.Fatalf("stage: %q", task.ReviewStage)
	}
	markDelivered(t, f, f.lead)
	t0 := task.UpdatedAt.Add(6 * time.Minute)
	if err := f.tm.CheckStuck(t0); err != nil {
		t.Fatal(err)
	}
	notes := undelivered(t, f, f.lead)
	if len(notes) != 1 || !strings.Contains(notes[0].Text, "T-1 is waiting for your decision: call accept_task or send_back") {
		t.Fatalf("Lead trebuia trezit cu o nota: %+v", notes)
	}
	if !f.hasMessage(f.lead.ID, "Foreman nudged "+f.lead.Name+": T-1 is waiting for a decision.") {
		t.Fatal("lipseste nota livrata din thread")
	}
	got, _ := f.st.GetTask(task.ID)
	if got.NudgedAt == nil || !got.NudgedAt.Equal(t0) {
		t.Fatalf("nudged_at: %v", got.NudgedAt)
	}
	if got.Status != store.StatusReview || !got.UpdatedAt.Equal(task.UpdatedAt) {
		t.Fatalf("impingerea nu schimba starea: %+v", got)
	}

	// imediat dupa: nu e vizibil si nu se mai trimite nimic
	markDelivered(t, f, f.lead)
	if err := f.tm.CheckStuck(t0.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if ok, _ := f.st.HasUndelivered(f.lead.ID); ok {
		t.Fatal("a doua impingere automata")
	}
	if st, _ := f.tm.StuckTasks(f.p.ID, t0.Add(time.Minute)); len(st) != 0 {
		t.Fatalf("prea devreme: %+v", st)
	}

	// dupa inca 5 min: vizibil, tot fara impingere
	f.tm.CheckStuck(t0.Add(6 * time.Minute))
	if ok, _ := f.st.HasUndelivered(f.lead.ID); ok {
		t.Fatal("impingere automata dupa ce e vizibil")
	}
	st, err := f.tm.StuckTasks(f.p.ID, t0.Add(6*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if len(st) != 1 || st[0].Kind != "decision" || st[0].AgentID != f.lead.ID || st[0].AgentName != f.lead.Name ||
		st[0].TaskID != task.ID || st[0].Ref != "T-1" || st[0].Title != "A" || !st[0].Since.Equal(task.UpdatedAt) {
		t.Fatalf("%+v", st)
	}

	// o schimbare de stare (SaveTask) curata impingerea: nu mai e vizibil
	got.Status = store.StatusDone
	f.st.SaveTask(&got)
	if st, _ := f.tm.StuckTasks(f.p.ID, t0.Add(20*time.Minute)); st == nil || len(st) != 0 {
		t.Fatalf("vreau lista goala non-nil: %#v", st)
	}
}

// TestStuckSkips: in fiecare caz detectorul nu face nimic.
func TestStuckSkips(t *testing.T) {
	cases := []struct {
		name    string
		prepare func(t *testing.T, f *fixture) (store.Task, time.Duration)
	}{
		{"proiect pe pauza", func(t *testing.T, f *fixture) (store.Task, time.Duration) {
			task := f.task(t, "A", f.rook, store.StatusReview)
			f.st.SetPaused(f.p.ID, true)
			return task, 6 * time.Minute
		}},
		{"mesaje nelivrate", func(t *testing.T, f *fixture) (store.Task, time.Duration) {
			task := f.task(t, "A", f.rook, store.StatusReview)
			f.tm.note(f.p.ID, f.lead.ID, "Rook finished T-1", false)
			return task, 6 * time.Minute
		}},
		{"intrebare deschisa", func(t *testing.T, f *fixture) (store.Task, time.Duration) {
			task := f.task(t, "A", f.rook, store.StatusReview)
			c := mcp.Caller{ProjectID: f.p.ID, AgentID: f.lead.ID, IsLead: true}
			if _, err := f.tm.AskUser(c, "Accept T-1?", nil); err != nil {
				t.Fatal(err)
			}
			return task, 6 * time.Minute
		}},
		{"decide utilizatorul", func(t *testing.T, f *fixture) (store.Task, time.Duration) {
			task, err := f.tm.UserCreateTask(f.p.ID, "A", "", f.rook.ID)
			if err != nil {
				t.Fatal(err)
			}
			task.Status, task.ReviewStage = store.StatusReview, store.StageLead
			f.st.SaveTask(&task)
			return task, 6 * time.Minute
		}},
		{"blocat", func(t *testing.T, f *fixture) (store.Task, time.Duration) {
			return f.task(t, "A", f.rook, store.StatusBlocked), 6 * time.Minute
		}},
		{"prea recent", func(t *testing.T, f *fixture) (store.Task, time.Duration) {
			return f.task(t, "A", f.rook, store.StatusReview), 4 * time.Minute
		}},
		{"run activ", func(t *testing.T, f *fixture) (store.Task, time.Duration) {
			task := f.task(t, "A", f.rook, store.StatusReview)
			if _, err := f.tm.UserMessage(f.lead.ID, "Salut", nil); err != nil {
				t.Fatal(err)
			}
			mustReconcile(t, f) // porneste run-ul Lead-ului; nu-l terminam
			if f.tm.ActiveRunID(f.lead.ID) == "" {
				t.Fatal("Lead trebuia sa ruleze")
			}
			return task, 6 * time.Minute
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := setup(t)
			markDelivered(t, f, f.lead)
			task, after := c.prepare(t, f)
			before := len(undelivered(t, f, f.lead))
			now := task.UpdatedAt.Add(after)
			if err := f.tm.CheckStuck(now); err != nil {
				t.Fatal(err)
			}
			if n := len(undelivered(t, f, f.lead)); n != before {
				t.Fatalf("note noi pentru Lead: %d -> %d", before, n)
			}
			got, _ := f.st.GetTask(task.ID)
			if got.NudgedAt != nil || got.Status != task.Status {
				t.Fatalf("task atins: %+v", got)
			}
			if st, err := f.tm.StuckTasks(f.p.ID, now.Add(time.Hour)); err != nil || len(st) != 0 {
				t.Fatalf("%+v %v", st, err)
			}
			if err := f.tm.Nudge(task.ID, now); !errors.Is(err, ErrNotStuck) && c.name != "prea recent" {
				t.Fatalf("Nudge: %v", err)
			}
		})
	}
}

// orfan: In progress la Rook fara run activ -> inapoi in coada, impins
func TestStuckOrphanRequeued(t *testing.T) {
	f := setup(t)
	task := f.task(t, "A", f.rook, store.StatusInProgress)
	now := task.UpdatedAt.Add(6 * time.Minute)
	if err := f.tm.CheckStuck(now); err != nil {
		t.Fatal(err)
	}
	got, _ := f.st.GetTask(task.ID)
	if got.Status != store.StatusQueued || got.AssigneeID != f.rook.ID {
		t.Fatalf("%+v", got)
	}
	if got.NudgedAt == nil || !got.NudgedAt.Equal(now) {
		t.Fatalf("nudged_at: %v (requeue trebuia facut inainte de SetTaskNudged)", got.NudgedAt)
	}
	if !f.hasMessage(f.rook.ID, "Foreman requeued T-1: it was in progress with nothing running.") {
		t.Fatal("lipseste nota de requeue")
	}
	// queued nu e blocat: nimic vizibil
	if st, _ := f.tm.StuckTasks(f.p.ID, now.Add(time.Hour)); len(st) != 0 {
		t.Fatalf("%+v", st)
	}
	// Kick a fost cerut: reconcilierea il porneste pe Rook
	mustReconcile(t, f)
	if f.tm.ActiveRunID(f.rook.ID) == "" {
		t.Fatal("Rook trebuia repornit")
	}
}

// un orfan cu intrebare deschisa pe task asteapta utilizatorul: nu e blocat
func TestStuckOrphanWaitingForAnswer(t *testing.T) {
	f := setup(t)
	task := f.task(t, "A", f.rook, store.StatusInProgress)
	c := mcp.Caller{ProjectID: f.p.ID, AgentID: f.rook.ID, TaskID: task.ID, HasParent: true}
	if _, err := f.tm.AskUser(c, "Which DB?", nil); err != nil {
		t.Fatal(err)
	}
	f.tm.CheckStuck(task.UpdatedAt.Add(6 * time.Minute))
	if got, _ := f.st.GetTask(task.ID); got.Status != store.StatusInProgress || got.NudgedAt != nil {
		t.Fatalf("%+v", got)
	}
}

// sub-liderul care asteapta sub-task-uri nu e orfan
func TestStuckSubLeadWaitingForSubtasks(t *testing.T) {
	f := setup(t)
	sam := f.agent(t, "Sam", &f.lead, nil)
	wes := f.agent(t, "Wes", &sam, nil)
	parent := f.task(t, "A", sam, store.StatusInProgress)
	sub := store.Task{ProjectID: f.p.ID, Title: "A1", AssigneeID: wes.ID, CreatorID: &sam.ID, ParentTaskID: &parent.ID}
	if err := f.st.CreateTask(&sub); err != nil {
		t.Fatal(err)
	}
	now := parent.UpdatedAt.Add(6 * time.Minute)
	if err := f.tm.CheckStuck(now); err != nil {
		t.Fatal(err)
	}
	if got, _ := f.st.GetTask(parent.ID); got.Status != store.StatusInProgress || got.NudgedAt != nil {
		t.Fatalf("%+v", got)
	}
	if err := f.tm.Nudge(parent.ID, now); !errors.Is(err, ErrNotStuck) {
		t.Fatalf("Nudge: %v", err)
	}
}

// doua task-uri in review la Lead -> o singura nota care le contine pe amandoua
func TestStuckOneNotePerAgent(t *testing.T) {
	f := setup(t)
	a := f.task(t, "A", f.rook, store.StatusReview)
	b := f.task(t, "B", f.vera, store.StatusReview)
	markDelivered(t, f, f.lead)
	if err := f.tm.CheckStuck(b.UpdatedAt.Add(6 * time.Minute)); err != nil {
		t.Fatal(err)
	}
	notes := undelivered(t, f, f.lead)
	if len(notes) != 1 || !strings.Contains(notes[0].Text, "T-1, T-2 are waiting for your decision") {
		t.Fatalf("%+v", notes)
	}
	if !f.hasMessage(f.lead.ID, "Foreman nudged "+f.lead.Name+": T-1, T-2 are waiting for a decision.") {
		t.Fatal("lipseste nota livrata")
	}
	for _, task := range []store.Task{a, b} {
		if got, _ := f.st.GetTask(task.ID); got.NudgedAt == nil {
			t.Fatalf("%s neimpins", got.Ref())
		}
	}
}

// reviewer-ul primeste textul de review, nu pe cel de decizie
func TestStuckReviewerText(t *testing.T) {
	f := setup(t)
	wes := f.agent(t, "Wes", &f.lead, &f.vera)
	task := f.task(t, "A", wes, store.StatusReview)
	task.ReviewStage = store.StageReviewer
	f.st.SaveTask(&task)
	if err := f.tm.CheckStuck(task.UpdatedAt.Add(6 * time.Minute)); err != nil {
		t.Fatal(err)
	}
	notes := undelivered(t, f, f.vera)
	if len(notes) != 1 || !strings.Contains(notes[0].Text, "T-1 is waiting for your review: call review_approve or review_reject") {
		t.Fatalf("%+v", notes)
	}
}

func TestNudgeManual(t *testing.T) {
	f := setup(t)
	task := f.task(t, "A", f.rook, store.StatusReview)
	markDelivered(t, f, f.lead)
	now := task.UpdatedAt.Add(time.Minute) // sub prag: manual merge oricum
	if err := f.tm.Nudge(task.ID, now); err != nil {
		t.Fatal(err)
	}
	if notes := undelivered(t, f, f.lead); len(notes) != 1 || !strings.Contains(notes[0].Text, "T-1 is waiting for your decision") {
		t.Fatalf("%+v", notes)
	}
	if got, _ := f.st.GetTask(task.ID); got.NudgedAt == nil || !got.NudgedAt.Equal(now) {
		t.Fatalf("nudged_at: %v", got.NudgedAt)
	}

	// task Done: nu e blocat
	done := f.task(t, "B", f.vera, store.StatusDone)
	if err := f.tm.Nudge(done.ID, now); !errors.Is(err, ErrNotStuck) {
		t.Fatalf("Nudge pe done: %v", err)
	}

	// proiect pe pauza: nu e blocat
	markDelivered(t, f, f.lead)
	f.st.SetPaused(f.p.ID, true)
	if err := f.tm.Nudge(task.ID, now); !errors.Is(err, ErrNotStuck) {
		t.Fatalf("Nudge pe pauza: %v", err)
	}

	// task inexistent
	if err := f.tm.Nudge(9999, now); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("Nudge inexistent: %v", err)
	}
}
