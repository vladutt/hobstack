package team

import (
	"encoding/json"
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
	// un al doilea tick cat sta in coada nu-l repune iar si nu mai scrie nota
	if err := f.tm.CheckStuck(now.Add(6 * time.Minute)); err != nil {
		t.Fatal(err)
	}
	again, _ := f.st.GetTask(task.ID)
	if again.Status != store.StatusQueued || again.NudgedAt == nil || !again.NudgedAt.Equal(now) || !again.UpdatedAt.Equal(got.UpdatedAt) {
		t.Fatalf("repus din nou: %+v", again)
	}
	if n := countMessages(t, f, f.rook, "Foreman requeued T-1"); n != 1 {
		t.Fatalf("note de requeue: %d", n)
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

// countMessages numara mesajele agentului care contin textul.
func countMessages(t *testing.T, f *fixture, a store.Agent, text string) int {
	t.Helper()
	msgs, err := f.st.ListMessages(a.ID)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, m := range msgs {
		if strings.Contains(m.Text, text) {
			n++
		}
	}
	return n
}

// orfan repus, pornit de reconciliere, ramas iar orfan: nu se mai repune
// automat, ci apare utilizatorului (kind "orphan") dupa inca stuckAfter
func TestStuckOrphanTwiceBecomesVisible(t *testing.T) {
	f := setup(t)
	task := f.task(t, "A", f.rook, store.StatusInProgress)
	t0 := task.UpdatedAt.Add(6 * time.Minute)
	if err := f.tm.CheckStuck(t0); err != nil {
		t.Fatal(err)
	}
	if got, _ := f.st.GetTask(task.ID); got.Status != store.StatusQueued {
		t.Fatalf("trebuia repus: %+v", got)
	}

	// reconcilierea il porneste: In progress, dar impingerea ramane notata
	mustReconcile(t, f)
	if f.tm.ActiveRunID(f.rook.ID) == "" {
		t.Fatal("Rook trebuia repornit")
	}
	started, _ := f.st.GetTask(task.ID)
	if started.Status != store.StatusInProgress || started.NudgedAt == nil || !started.NudgedAt.Equal(t0) {
		t.Fatalf("nudged_at trebuia pastrat la pornire: %+v", started)
	}

	// run-ul dispare fara sa miste task-ul (ex. procesul a murit ciudat):
	// task-ul ramane In progress fara run
	f.tm.mu.Lock()
	ar := f.tm.active[f.rook.ID]
	delete(f.tm.active, f.rook.ID)
	delete(f.tm.tokens, ar.token)
	f.tm.mu.Unlock()

	t1 := started.UpdatedAt.Add(6 * time.Minute)
	if t1.Before(t0) {
		t1 = t0
	}
	if err := f.tm.CheckStuck(t1); err != nil {
		t.Fatal(err)
	}
	got, _ := f.st.GetTask(task.ID)
	if got.Status != store.StatusInProgress {
		t.Fatalf("repus a doua oara: %+v", got)
	}
	if n := countMessages(t, f, f.rook, "Foreman requeued T-1"); n != 1 {
		t.Fatalf("note de requeue: %d", n)
	}
	st, err := f.tm.StuckTasks(f.p.ID, t1.Add(stuckAfter))
	if err != nil {
		t.Fatal(err)
	}
	if len(st) != 1 || st[0].Kind != "orphan" || st[0].TaskID != task.ID || st[0].AgentID != f.rook.ID {
		t.Fatalf("%+v", st)
	}

	// sfarsitul normal al unui run (SaveTask in review) curata impingerea
	got.Status, got.ReviewStage = store.StatusReview, store.StageLead
	if err := f.st.SaveTask(&got); err != nil {
		t.Fatal(err)
	}
	if again, _ := f.st.GetTask(task.ID); again.NudgedAt != nil {
		t.Fatalf("nudged_at trebuia curatat: %v", again.NudgedAt)
	}
}

// la trecerea pragului de vizibilitate, CheckStuck trimite un eveniment "task",
// ca pagina deschisa sa se reincarce; inainte si dupa acel tick, nu
func TestStuckEmitsWhenItBecomesVisible(t *testing.T) {
	f := setup(t)
	f.st.SetPaused(f.p.ID, false)
	task := f.task(t, "A", f.rook, store.StatusReview)
	markDelivered(t, f, f.lead)
	t0 := task.UpdatedAt.Add(6 * time.Minute)
	if err := f.tm.CheckStuck(t0); err != nil {
		t.Fatal(err)
	}
	markDelivered(t, f, f.lead)

	events, cancel := f.tm.hub.Subscribe(f.p.ID)
	defer cancel()
	taskEvents := func() int {
		n := 0
		for {
			select {
			case raw := <-events:
				var ev struct {
					Type string     `json:"type"`
					Data store.Task `json:"data"`
				}
				if err := json.Unmarshal(raw, &ev); err != nil {
					t.Fatal(err)
				}
				if ev.Type == "task" && ev.Data.ID == task.ID {
					n++
				}
			default:
				return n
			}
		}
	}

	// tick-uri inainte de prag: nimic
	f.tm.CheckStuck(t0.Add(4 * time.Minute))
	if n := taskEvents(); n != 0 {
		t.Fatalf("eveniment prea devreme: %d", n)
	}
	// tick-ul care trece pragul: un eveniment
	f.tm.CheckStuck(t0.Add(stuckAfter + 30*time.Second))
	if n := taskEvents(); n != 1 {
		t.Fatalf("evenimente la prag: %d", n)
	}
	// tick-ul urmator: nimic (o singura data)
	f.tm.CheckStuck(t0.Add(stuckAfter + 90*time.Second))
	if n := taskEvents(); n != 0 {
		t.Fatalf("eveniment repetat: %d", n)
	}
}

// un orfan care asteapta o aprobare de comanda nu e blocat
func TestStuckOrphanWaitingForAccess(t *testing.T) {
	f := setup(t)
	task := f.task(t, "A", f.rook, store.StatusInProgress)
	f.st.RecordCommandRequest(f.p.ID, f.rook.ID, &task.ID, "git stash", "git stash")
	f.tm.CheckStuck(task.UpdatedAt.Add(6 * time.Minute))
	if got, _ := f.st.GetTask(task.ID); got.Status != store.StatusInProgress || got.NudgedAt != nil {
		t.Fatalf("%+v", got)
	}
}
