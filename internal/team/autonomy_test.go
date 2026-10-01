package team

import (
	"strings"
	"testing"

	"foreman/internal/runner"
	"foreman/internal/store"
)

// autonomous porneste modul full autonomy pe proiectul fixture-ului.
func autonomous(t *testing.T, f *fixture) {
	t.Helper()
	f.p.Autonomy = true
	if err := f.st.UpdateProject(f.p); err != nil {
		t.Fatal(err)
	}
}

// hasEvent cauta in istoricul task-ului un eveniment cu kind, actor si text date.
func hasEvent(t *testing.T, f *fixture, taskID int64, kind, actor, text string) bool {
	t.Helper()
	evs, err := f.st.ListTaskEvents(taskID)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range evs {
		if e.Kind == kind && e.Actor == actor && e.Text == text {
			return true
		}
	}
	return false
}

// userTaskInReview: task creat din board pentru who, raportat (in Review la lider).
func userTaskInReview(t *testing.T, f *fixture, who store.Agent) store.Task {
	t.Helper()
	task, err := f.tm.UserCreateTask(f.p.ID, "A", "", who.ID)
	if err != nil {
		t.Fatal(err)
	}
	task.Status = store.StatusInProgress
	if err := f.st.SaveTask(&task); err != nil {
		t.Fatal(err)
	}
	if _, err := f.tm.ReportDone(f.workerCaller(who, task.ID), "", "A e gata"); err != nil {
		t.Fatal(err)
	}
	return f.wantStatus(t, task.Number, store.StatusReview)
}

const blockedRootNote = "T-1 is blocked after 3 rejections or send-backs. As the root lead in full autonomy, decide: " +
	"accept_task to take it as it is, or send_back with clear feedback for one more try."

func TestAutonomyAskUserAnswersWithoutQuestion(t *testing.T) {
	f := setup(t)
	autonomous(t, f)
	task := f.task(t, "A", f.rook, store.StatusInProgress)
	text, err := f.tm.AskUser(f.workerCaller(f.rook, task.ID), "Ce baza de date?", []string{"MySQL", "SQLite"})
	want := "Full autonomy is on: the user will not answer. Decide yourself, state your assumption, and continue."
	if err != nil || text != want {
		t.Fatalf("%q %v", text, err)
	}
	if qs, _ := f.st.ListOpenQuestions(f.p.ID); len(qs) != 0 {
		t.Fatalf("nicio intrebare in modul autonom: %+v", qs)
	}
	if f.hasMessage(f.rook.ID, "Asked you") {
		t.Fatal("nicio nota in modul autonom")
	}
	if waiting, _ := f.st.HasOpenQuestionForTask(task.ID); waiting {
		t.Fatal("task-ul nu trece in asteptare")
	}
}

func TestAutonomyUserTaskDecidedByRoot(t *testing.T) {
	f := setup(t)
	autonomous(t, f)
	task := userTaskInReview(t, f, f.rook)
	if !f.hasMessage(f.lead.ID, "(review)") {
		t.Fatal("radacina primeste nota de review")
	}
	if f.tm.UserDecides(task, mustAgents(t, f)) {
		t.Fatal("in modul autonom decide radacina, nu utilizatorul")
	}
	if text, err := f.tm.AcceptTask(f.leadCaller(), "T-1", ""); err != nil || text != "T-1 is Done." {
		t.Fatalf("radacina accepta: %q %v", text, err)
	}
	f.wantStatus(t, 1, store.StatusDone)

	// send_back de la radacina
	userTaskInReview(t, f, f.rook)
	if _, err := f.tm.SendBack(f.workerCaller(f.vera, 0), "T-2", "x"); err == nil {
		t.Fatal("un worker nu decide")
	}
	if text, err := f.tm.SendBack(f.leadCaller(), "T-2", "mai bine"); err != nil || !strings.Contains(text, "sent back to Rook (1/3)") {
		t.Fatalf("radacina trimite inapoi: %q %v", text, err)
	}
	f.wantStatus(t, 2, store.StatusQueued)

	// utilizatorul tot poate decide din board
	t3 := userTaskInReview(t, f, f.rook)
	if err := f.tm.UserDecide(t3.ID, true, ""); err != nil {
		t.Fatalf("utilizatorul decide si in modul autonom: %v", err)
	}
	f.wantStatus(t, 3, store.StatusDone)
}

func TestAutonomyReviewerApprovedUserTaskGoesToRoot(t *testing.T) {
	f := setup(t)
	autonomous(t, f)
	wes := f.agent(t, "Wes", &f.lead, &f.vera)
	userTaskInReview(t, f, wes)
	vc := f.workerCaller(f.vera, 0)
	vc.IsReviewer = true
	text, err := f.tm.ReviewApprove(vc, "T-1", "")
	if err != nil || text != "T-1 went to Lead for a decision." {
		t.Fatalf("%q %v", text, err)
	}
	got := f.wantStatus(t, 1, store.StatusReview)
	if f.tm.UserDecides(got, mustAgents(t, f)) || !f.hasMessage(f.lead.ID, "Vera approved T-1") {
		t.Fatalf("dupa reviewer decide radacina: %+v", got)
	}
}

func TestAutonomyRootAssigneeAutoDone(t *testing.T) {
	f := setup(t)
	autonomous(t, f)
	if _, err := f.tm.UserCreateTask(f.p.ID, "A", "", f.lead.ID); err != nil {
		t.Fatal(err)
	}
	mustReconcile(t, f)
	run := f.fs.last(t, f.lead)
	f.fs.finish(run.ID, runner.Outcome{Status: store.RunDone, Result: "gata"})
	task := f.wantStatus(t, 1, store.StatusDone)
	if !hasEvent(t, f, task.ID, "accepted", actorForeman, "Accepted automatically (full autonomy)") {
		t.Fatal("lipseste evenimentul de accept automat")
	}

	// task creat de un lider sters, asignat unei radacini: tot Done automat
	solo := f.agent(t, "Solo", nil, nil)
	t2 := f.task(t, "B", solo, store.StatusInProgress)
	t2.CreatorID = nil
	t2.Result = "gata"
	if _, err := f.tm.toReview(&t2, solo, mustAgents(t, f), "Solo a terminat", taskNote{}); err != nil {
		t.Fatal(err)
	}
	f.wantStatus(t, 2, store.StatusDone)
	if f.hasMessage(solo.ID, "The user decides") {
		t.Fatal("fara nota pentru utilizator in modul autonom")
	}
}

func TestAutonomyRootRunFailedAutoDone(t *testing.T) {
	f := setup(t)
	autonomous(t, f)
	if _, err := f.tm.UserCreateTask(f.p.ID, "A", "", f.lead.ID); err != nil {
		t.Fatal(err)
	}
	mustReconcile(t, f)
	run := f.fs.last(t, f.lead)
	f.fs.finish(run.ID, runner.Outcome{Status: store.RunFailed, Error: "boom"})
	f.wantStatus(t, 1, store.StatusDone)
}

func TestAutonomyBlockedEscalatesToRoot(t *testing.T) {
	f := setup(t)
	autonomous(t, f)
	f.task(t, "A", f.rook, store.StatusReview)
	review := func() {
		got, _ := f.st.GetTaskByNumber(f.p.ID, 1)
		got.Status, got.ReviewStage = store.StatusReview, store.StageLead
		f.st.SaveTask(&got)
	}
	for i := 0; i < 2; i++ {
		if _, err := f.tm.SendBack(f.leadCaller(), "T-1", "mai bine"); err != nil {
			t.Fatal(err)
		}
		review()
	}
	text, err := f.tm.SendBack(f.leadCaller(), "T-1", "tot nu")
	if err != nil || text != "T-1 was sent back 3 times and is now blocked; Lead decides." {
		t.Fatalf("a treia trimitere: %q %v", text, err)
	}
	f.wantStatus(t, 1, store.StatusBlocked)
	if !f.hasMessage(f.lead.ID, blockedRootNote) || f.hasMessage(f.lead.ID, "The user decides") {
		t.Fatal("radacina primeste nota de autonomie")
	}

	// radacina trimite inapoi un task blocat: inca o incercare
	text, err = f.tm.SendBack(f.leadCaller(), "T-1", "ultima incercare")
	if err != nil || !strings.Contains(text, "sent back to Rook (2/3)") {
		t.Fatalf("send_back pe Blocked: %q %v", text, err)
	}
	got := f.wantStatus(t, 1, store.StatusQueued)
	if !got.Escalated || got.SendBacks != MaxSendBacks-1 || got.Feedback != "ultima incercare" {
		t.Fatalf("%+v", got)
	}

	// al doilea blocaj: Done automat, fara bucla
	review()
	text, err = f.tm.SendBack(f.leadCaller(), "T-1", "tot nu")
	if err != nil || !strings.Contains(text, "accepted automatically") {
		t.Fatalf("al doilea blocaj: %q %v", text, err)
	}
	done := f.wantStatus(t, 1, store.StatusDone)
	if !hasEvent(t, f, done.ID, "accepted", actorForeman, "Accepted automatically after repeated send-backs (full autonomy)") {
		t.Fatal("lipseste evenimentul de accept automat")
	}
	if !f.hasMessage(f.lead.ID, "accepted automatically after repeated send-backs") {
		t.Fatal("radacina afla de accept")
	}
}

func TestAutonomyOnlyRootSendsBackBlocked(t *testing.T) {
	f := setup(t)
	autonomous(t, f)
	mid := f.agent(t, "Mid", &f.lead, nil)
	w := f.agent(t, "W", &mid, nil)
	task := f.task(t, "A", w, store.StatusBlocked)
	task.CreatorID = &mid.ID
	task.SendBacks = MaxSendBacks
	f.st.SaveTask(&task)
	midCaller := f.workerCaller(mid, 0)
	if _, err := f.tm.SendBack(midCaller, "T-1", "x"); err == nil {
		t.Fatal("un sef intermediar nu trimite inapoi un task blocat")
	}
	f.wantStatus(t, 1, store.StatusBlocked)
	if _, err := f.tm.SendBack(f.leadCaller(), "T-1", "x"); err != nil {
		t.Fatalf("radacina poate: %v", err)
	}
	if got := f.wantStatus(t, 1, store.StatusQueued); !got.Escalated {
		t.Fatalf("%+v", got)
	}
}

// acelasi scenariu fara autonomie: comportamentul de azi
func TestAutonomyOffUnchanged(t *testing.T) {
	f := setup(t)
	task := userTaskInReview(t, f, f.rook)
	if !f.tm.UserDecides(task, mustAgents(t, f)) {
		t.Fatal("fara autonomie decide utilizatorul")
	}
	if _, err := f.tm.AcceptTask(f.leadCaller(), "T-1", ""); err == nil {
		t.Fatal("fara autonomie radacina nu decide task-urile utilizatorului")
	}
	if text, _ := f.tm.AskUser(f.workerCaller(f.rook, task.ID), "Ce?", nil); !strings.Contains(text, "Question sent to the user") {
		t.Fatalf("ask_user normal: %q", text)
	}

	f.task(t, "B", f.rook, store.StatusReview)
	for i := 0; i < 3; i++ {
		if _, err := f.tm.SendBack(f.leadCaller(), "T-2", "x"); err != nil {
			t.Fatal(err)
		}
		got, _ := f.st.GetTaskByNumber(f.p.ID, 2)
		if got.Status == store.StatusBlocked {
			break
		}
		got.Status, got.ReviewStage = store.StatusReview, store.StageLead
		f.st.SaveTask(&got)
	}
	f.wantStatus(t, 2, store.StatusBlocked)
	if !f.hasMessage(f.lead.ID, "T-2 is blocked after 3 rejections or send-backs. The user decides.") {
		t.Fatal("nota de azi")
	}
	if _, err := f.tm.SendBack(f.leadCaller(), "T-2", "x"); err == nil || !strings.Contains(err.Error(), "only tasks in review can be sent back") {
		t.Fatalf("fara autonomie send_back pe Blocked e refuzat: %v", err)
	}
}

func TestRootOf(t *testing.T) {
	f := setup(t)
	mid := f.agent(t, "Mid", &f.lead, nil)
	w := f.agent(t, "W", &mid, nil)
	agents := mustAgents(t, f)
	if r, ok := rootOf(w.ID, agents); !ok || r.ID != f.lead.ID {
		t.Fatalf("%+v %v", r, ok)
	}
	if r, ok := rootOf(f.lead.ID, agents); !ok || r.ID != f.lead.ID {
		t.Fatalf("radacina e chiar ea: %+v %v", r, ok)
	}
	if _, ok := rootOf(9999, agents); ok {
		t.Fatal("agent inexistent")
	}
}
