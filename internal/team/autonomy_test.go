package team

import (
	"strings"
	"testing"
	"time"

	"hobstack/internal/runner"
	"hobstack/internal/store"
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
	task, err := f.tm.UserCreateTask(f.p.ID, "A", "", who.ID, 0)
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
	"accept_task to take it as it is, or send_back with clear feedback for one more try. " +
	"If it is blocked again it will be accepted automatically."

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
	if f.tm.UserDecides(task, mustAgents(t, f), f.p.Autonomy) {
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
	if err := f.tm.UserDecide(t3.ID, true, "", nil); err != nil {
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
	if f.tm.UserDecides(got, mustAgents(t, f), f.p.Autonomy) || !f.hasMessage(f.lead.ID, "Vera approved T-1") {
		t.Fatalf("dupa reviewer decide radacina: %+v", got)
	}
}

// task creat din board pentru radacina: nu se accepta automat nici in modul
// autonom (cu sau fara report_done), decide utilizatorul
func TestAutonomyUserTaskOnRootStaysForUser(t *testing.T) {
	f := setup(t)
	autonomous(t, f)
	if _, err := f.tm.UserCreateTask(f.p.ID, "A", "", f.lead.ID, 0); err != nil {
		t.Fatal(err)
	}
	mustReconcile(t, f)
	run := f.fs.last(t, f.lead)
	f.fs.finish(run.ID, runner.Outcome{Status: store.RunDone, Result: "Varianta 1 sau 2? Am nevoie de domeniu."})
	got := f.wantStatus(t, 1, store.StatusReview)
	if !f.tm.UserDecides(got, mustAgents(t, f), f.p.Autonomy) {
		t.Fatalf("decide utilizatorul: %+v", got)
	}
	if hasEvent(t, f, got.ID, "accepted", actorHobStack, autoAcceptText) {
		t.Fatal("fara accept automat")
	}

	// radacina cu reviewer: dupa aprobare decide utilizatorul
	solo := f.agent(t, "Solo", nil, &f.vera)
	if _, err := f.tm.UserCreateTask(f.p.ID, "B", "", solo.ID, 0); err != nil {
		t.Fatal(err)
	}
	mustReconcile(t, f)
	f.fs.finish(f.fs.last(t, solo).ID, runner.Outcome{Status: store.RunDone, Result: "gata"})
	f.wantStatus(t, 2, store.StatusReview)
	vc := f.workerCaller(f.vera, 0)
	vc.IsReviewer = true
	if text, err := f.tm.ReviewApprove(vc, "T-2", ""); err != nil || text != "T-2 went to the user for a decision." {
		t.Fatalf("%q %v", text, err)
	}
	got = f.wantStatus(t, 2, store.StatusReview)
	if !f.tm.UserDecides(got, mustAgents(t, f), f.p.Autonomy) {
		t.Fatalf("dupa reviewer decide utilizatorul: %+v", got)
	}
}

// task care nu e din board, asignat unei radacini fara decident: Done automat;
// cu reported = false (un agent care s-a oprit fara report_done) ramane la utilizator
func TestAutonomyRootAssigneeAutoDone(t *testing.T) {
	f := setup(t)
	autonomous(t, f)
	solo := f.agent(t, "Solo", nil, nil)
	t1 := f.task(t, "A", solo, store.StatusInProgress)
	t1.CreatorID = nil // creat de un lider sters
	t1.Result = "gata"
	if _, err := f.tm.toReview(&t1, solo, mustAgents(t, f), "Solo a terminat", taskNote{}, true); err != nil {
		t.Fatal(err)
	}
	got := f.wantStatus(t, 1, store.StatusDone)
	if !hasEvent(t, f, got.ID, "accepted", actorHobStack, autoAcceptText) {
		t.Fatal("lipseste evenimentul de accept automat")
	}
	if f.hasMessage(solo.ID, "The user decides") {
		t.Fatal("fara nota pentru utilizator in modul autonom")
	}

	t2 := f.task(t, "B", solo, store.StatusInProgress)
	t2.CreatorID = nil
	t2.Result = "intrebare"
	if _, err := f.tm.toReview(&t2, solo, mustAgents(t, f), "Solo a terminat", taskNote{}, false); err != nil {
		t.Fatal(err)
	}
	f.wantStatus(t, 2, store.StatusBlocked)
}

// un run esuat nu se accepta niciodata automat: fara decident ramane la utilizator
func TestAutonomyRootRunFailedStaysForUser(t *testing.T) {
	f := setup(t)
	autonomous(t, f)
	if _, err := f.tm.UserCreateTask(f.p.ID, "A", "", f.lead.ID, 0); err != nil {
		t.Fatal(err)
	}
	mustReconcile(t, f)
	run := f.fs.last(t, f.lead)
	f.fs.finish(run.ID, runner.Outcome{Status: store.RunFailed, Error: "boom"})
	got := f.wantStatus(t, 1, store.StatusReview)
	if !f.tm.UserDecides(got, mustAgents(t, f), f.p.Autonomy) {
		t.Fatalf("run esuat fara decident: decide utilizatorul: %+v", got)
	}
}

// in modul autonom un task blocat il decide radacina (si nu creatorul intermediar)
func TestAutonomyBlockedOwnerIsRoot(t *testing.T) {
	f := setup(t)
	mid := f.agent(t, "Mid", &f.lead, nil)
	w := f.agent(t, "W", &mid, nil)
	task := f.task(t, "A", w, store.StatusBlocked)
	task.CreatorID = &mid.ID
	agents := mustAgents(t, f)
	if id := decisionOwnerIn(task, agents, true); id != f.lead.ID {
		t.Fatalf("autonom: %d, vreau radacina %d", id, f.lead.ID)
	}
	if id := decisionOwnerIn(task, agents, false); id != mid.ID {
		t.Fatalf("fara autonomie: %d, vreau creatorul %d", id, mid.ID)
	}
}

// send back din board nu declanseaza acceptul automat, nici pe un task escaladat
func TestAutonomyUserSendBackNeverAutoDone(t *testing.T) {
	f := setup(t)
	autonomous(t, f)
	task := userTaskInReview(t, f, f.rook)
	task.SendBacks, task.Escalated = MaxSendBacks-1, true
	if err := f.st.SaveTask(&task); err != nil {
		t.Fatal(err)
	}
	if err := f.tm.UserDecide(task.ID, false, "inca o data", nil); err != nil {
		t.Fatal(err)
	}
	f.wantStatus(t, 1, store.StatusBlocked)
	if hasEvent(t, f, task.ID, "accepted", actorHobStack, autoAcceptAfterText) {
		t.Fatal("decizia utilizatorului nu accepta automat")
	}
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
	if !hasEvent(t, f, done.ID, "accepted", actorHobStack, "Accepted automatically after repeated send-backs (full autonomy)") {
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
	if !f.tm.UserDecides(task, mustAgents(t, f), f.p.Autonomy) {
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

// in modul autonom un task blocat e o decizie a radacinii: detectorul o impinge;
// fara autonomie detectorul nu face nimic (decide utilizatorul)
func TestAutonomyBlockedTaskNudgesRoot(t *testing.T) {
	for _, auto := range []bool{true, false} {
		f := setup(t)
		if auto {
			autonomous(t, f)
		}
		f.st.SetPaused(f.p.ID, false)
		task := f.task(t, "A", f.rook, store.StatusBlocked)
		markDelivered(t, f, f.lead)
		if err := f.tm.CheckStuck(task.UpdatedAt.Add(10 * time.Minute)); err != nil {
			t.Fatal(err)
		}
		notes := undelivered(t, f, f.lead)
		got, _ := f.st.GetTask(task.ID)
		if auto {
			if len(notes) != 1 || !strings.Contains(notes[0].Text, "T-1 is waiting for your decision: call accept_task or send_back") || got.NudgedAt == nil {
				t.Fatalf("autonom: radacina impinsa: %+v %+v", notes, got)
			}
		} else if len(notes) != 0 || got.NudgedAt != nil {
			t.Fatalf("fara autonomie nimic: %+v %+v", notes, got)
		}
	}
}

// un run esuat nu se accepta niciodata automat: task escaladat, run-ul
// worker-ului esueaza, radacina il trimite inapoi -> Blocked la utilizator
func TestAutonomyEscalatedFailedRunNotAutoDone(t *testing.T) {
	f := setup(t)
	autonomous(t, f)
	task := f.task(t, "A", f.rook, store.StatusBlocked)
	task.SendBacks = MaxSendBacks
	f.st.SaveTask(&task)
	if _, err := f.tm.SendBack(f.leadCaller(), "T-1", "inca o data"); err != nil {
		t.Fatal(err)
	}
	f.wantStatus(t, 1, store.StatusQueued)
	mustReconcile(t, f)
	f.fs.finish(f.fs.last(t, f.rook).ID, runner.Outcome{Status: store.RunFailed, Error: "boom"})
	f.wantStatus(t, 1, store.StatusReview)

	text, err := f.tm.SendBack(f.leadCaller(), "T-1", "tot nu")
	if err != nil || text != "T-1 was sent back 3 times and is now blocked. The user decides what happens to it." {
		t.Fatalf("%q %v", text, err)
	}
	got := f.wantStatus(t, 1, store.StatusBlocked)
	if hasEvent(t, f, got.ID, "accepted", actorHobStack, autoAcceptAfterText) {
		t.Fatal("un run esuat nu se accepta automat")
	}
	if !hasEvent(t, f, got.ID, "blocked", actorHobStack, "Blocked after 3 send-backs: the last run failed, you decide") {
		t.Fatal("lipseste evenimentul de blocare cu motivul")
	}
	if !f.hasMessage(f.lead.ID, "T-1 is blocked after 3 rejections or send-backs. The user decides.") {
		t.Fatal("nota ca fara autonomie")
	}
	// radacina nu mai poate da inca o incercare, iar detectorul nu o impinge
	if _, err := f.tm.SendBack(f.leadCaller(), "T-1", "x"); err == nil || !strings.Contains(err.Error(), "the user decides") {
		t.Fatalf("send_back pe Blocked dupa run esuat: %v", err)
	}
	f.st.SetPaused(f.p.ID, false)
	markDelivered(t, f, f.lead)
	if err := f.tm.CheckStuck(got.UpdatedAt.Add(10 * time.Minute)); err != nil {
		t.Fatal(err)
	}
	if ok, _ := f.st.HasUndelivered(f.lead.ID); ok {
		t.Fatal("detectorul nu impinge radacina pentru un task lasat utilizatorului")
	}
}

// intr-o etapa, un agent care se opreste fara report_done nu e acceptat
// automat in modul autonom, chiar daca nu are cine sa decida: ramane la utilizator
func TestAutonomyStageRunWithoutReportNotAutoDone(t *testing.T) {
	f := setup(t)
	autonomous(t, f)
	solo := f.agent(t, "Solo", nil, nil)
	stages, err := f.st.SaveStages(f.p.ID, []store.Stage{{Name: "Dev", Kind: store.KindWork, AgentIDs: []int64{solo.ID}}})
	if err != nil {
		t.Fatal(err)
	}
	task := store.Task{ProjectID: f.p.ID, Title: "A", AssigneeID: solo.ID, StageID: &stages[0].ID}
	if err := f.st.CreateTask(&task); err != nil {
		t.Fatal(err)
	}
	mustReconcile(t, f)
	f.fs.finish(f.fs.last(t, solo).ID, runner.Outcome{Status: store.RunDone, Result: "care varianta?"})
	got := f.wantStatus(t, 1, store.StatusReview)
	if hasEvent(t, f, got.ID, "accepted", actorHobStack, autoAcceptText) {
		t.Fatal("fara accept automat")
	}
}

// sarcina recurenta cu auto-accept pe radacina: raspunsul radacinii e raportul,
// deci se accepta automat ca inainte
func TestRecurringAutoAcceptOnRoot(t *testing.T) {
	f := setup(t)
	r := f.recurring(t, RecurringInput{Title: "Daily", AssigneeID: f.lead.ID, Schedule: daily9, Enabled: true, AutoAccept: true}, at("2026-10-05 08:00"))
	if _, err := f.tm.RunRecurringNow(r.ID); err != nil {
		t.Fatal(err)
	}
	mustReconcile(t, f)
	f.fs.finish(f.fs.last(t, f.lead).ID, runner.Outcome{Status: store.RunDone, Result: "gata"})
	got := f.wantStatus(t, 1, store.StatusDone)
	if !hasEvent(t, f, got.ID, "accepted", actorHobStack, recurringAcceptText) {
		t.Fatal("lipseste acceptul automat")
	}
}
