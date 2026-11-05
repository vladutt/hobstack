package team

import (
	"strings"
	"testing"
	"time"

	"hobstack/internal/mcp"
	"hobstack/internal/runner"
	"hobstack/internal/store"
)

// convMsgs intoarce comunicarile task-ului (evenimentele cu body), in ordine.
func convMsgs(t *testing.T, f *fixture, taskID int64) []store.TaskEvent {
	t.Helper()
	evs, err := f.st.ListTaskEvents(taskID)
	if err != nil {
		t.Fatal(err)
	}
	var out []store.TaskEvent
	for _, e := range evs {
		if e.Body != "" {
			out = append(out, e)
		}
	}
	return out
}

// wantMsg cere exact o comunicare de felul kind pe task, cu from, to si body date.
func wantMsg(t *testing.T, f *fixture, taskID int64, kind, from, to, body string) store.TaskEvent {
	t.Helper()
	var found []store.TaskEvent
	for _, e := range convMsgs(t, f, taskID) {
		if e.Kind == kind {
			found = append(found, e)
		}
	}
	if len(found) != 1 {
		t.Fatalf("vreau o comunicare %s, am %d: %+v", kind, len(found), convMsgs(t, f, taskID))
	}
	e := found[0]
	if e.Actor != from || e.To != to || e.Body != body {
		t.Fatalf("%s: from %q to %q body %q; vreau %q -> %q %q", kind, e.Actor, e.To, e.Body, from, to, body)
	}
	return e
}

// wantMsgKinds cere ca felurile comunicarilor sa fie exact cele date (cate una per comunicare).
func wantMsgKinds(t *testing.T, f *fixture, taskID int64, kinds ...string) {
	t.Helper()
	var got []string
	for _, e := range convMsgs(t, f, taskID) {
		got = append(got, e.Kind)
	}
	if strings.Join(got, ",") != strings.Join(kinds, ",") {
		t.Fatalf("comunicari %v, vreau %v", got, kinds)
	}
}

// inProgress pune task-ul In progress (ca sa poata fi raportat).
func inProgress(t *testing.T, f *fixture, task store.Task) store.Task {
	t.Helper()
	task, _ = f.st.GetTask(task.ID)
	task.Status = store.StatusInProgress
	if err := f.st.SaveTask(&task); err != nil {
		t.Fatal(err)
	}
	return task
}

// toLeadReview pune task-ul in Review la lider (ca sa poata fi decis).
func toLeadReview(t *testing.T, f *fixture, task store.Task) {
	t.Helper()
	task, _ = f.st.GetTask(task.ID)
	task.Status, task.ReviewStage = store.StatusReview, store.StageLead
	if err := f.st.SaveTask(&task); err != nil {
		t.Fatal(err)
	}
}

func TestConvCreateFromMCP(t *testing.T) {
	f := setup(t)
	if _, err := f.tm.CreateTask(f.leadCaller(), "Login", "Use JWT", "Rook", "", ""); err != nil {
		t.Fatal(err)
	}
	task := f.wantStatus(t, 1, store.StatusQueued)
	wantMsg(t, f, task.ID, "created", "Lead", "Rook", "Login\n\nUse JWT")
	wantMsgKinds(t, f, task.ID, "created")
	// History ramane identic
	wantEvent(t, f, task.ID, "created", "Lead", "Created by Lead")

	// fara descriere: doar titlul
	f.tm.CreateTask(f.leadCaller(), "Logout", "", "Rook", "", "")
	t2 := f.wantStatus(t, 2, store.StatusQueued)
	wantMsg(t, f, t2.ID, "created", "Lead", "Rook", "Logout")
}

func TestConvCreateFromBoard(t *testing.T) {
	f := setup(t)
	task, err := f.tm.UserCreateTask(f.p.ID, "A", "do it", f.rook.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	wantMsg(t, f, task.ID, "created", "You", "Rook", "A\n\ndo it")
	wantEvent(t, f, task.ID, "created", "You", "Created by you")
}

func TestConvCreateInStageQueue(t *testing.T) {
	f, _, _ := stagesFixture(t)
	f.tm.CreateTask(f.leadCaller(), "A", "d", "", "", "Dev")
	t1 := f.wantStatus(t, 1, store.StatusQueued)
	wantMsg(t, f, t1.ID, "created", "Lead", "", "A\n\nd")
	f.tm.CreateTask(f.leadCaller(), "B", "", "Wes", "", "Dev")
	t2 := f.wantStatus(t, 2, store.StatusQueued)
	wantMsg(t, f, t2.ID, "created", "Lead", "Wes", "B")
	u, err := f.tm.UserCreateTask(f.p.ID, "C", "", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	wantMsg(t, f, u.ID, "created", "You", "", "C")
}

func TestConvReportToReviewerThenApproveReject(t *testing.T) {
	f := setup(t)
	rev := f.agent(t, "Rev", &f.lead, nil)
	wes := f.agent(t, "Wes", &f.lead, &rev)
	for _, title := range []string{"A", "B", "C"} {
		f.tm.CreateTask(f.leadCaller(), title, "", "Wes", "", "")
	}
	for n := 1; n <= 3; n++ {
		task := inProgress(t, f, f.wantStatus(t, n, store.StatusQueued))
		if _, err := f.tm.ReportDone(f.workerCaller(wes, task.ID), "", "gata "+task.Title); err != nil {
			t.Fatal(err)
		}
		wantMsg(t, f, task.ID, "reported", "Wes", "Rev", "gata "+task.Title)
	}
	rc := mcp.Caller{ProjectID: f.p.ID, AgentID: rev.ID, HasParent: true, IsReviewer: true}
	if _, err := f.tm.ReviewApprove(rc, "T-1", "looks good"); err != nil {
		t.Fatal(err)
	}
	t1 := f.wantStatus(t, 1, store.StatusReview)
	wantMsg(t, f, t1.ID, "approved", "Rev", "Lead", "looks good")
	wantEvent(t, f, t1.ID, "approved", "Rev", "Rev approved")
	if _, err := f.tm.ReviewApprove(rc, "T-2", ""); err != nil {
		t.Fatal(err)
	}
	t2 := f.wantStatus(t, 2, store.StatusReview)
	wantMsg(t, f, t2.ID, "approved", "Rev", "Lead", "Approved")

	if _, err := f.tm.ReviewReject(rc, "T-3", "fara teste"); err != nil {
		t.Fatal(err)
	}
	t3 := f.wantStatus(t, 3, store.StatusQueued)
	wantMsg(t, f, t3.ID, "rejected", "Rev", "Wes", "fara teste")
	wantMsgKinds(t, f, t3.ID, "created", "reported", "rejected")
}

func TestConvReportToLead(t *testing.T) {
	f := setup(t)
	f.tm.CreateTask(f.leadCaller(), "A", "", "Rook", "", "")
	task := inProgress(t, f, f.wantStatus(t, 1, store.StatusQueued))
	if _, err := f.tm.ReportDone(f.workerCaller(f.rook, task.ID), "", "the report"); err != nil {
		t.Fatal(err)
	}
	wantMsg(t, f, task.ID, "reported", "Rook", "Lead", "the report")
}

func TestConvReportToUser(t *testing.T) {
	f := setup(t)
	task := userTaskInReview(t, f, f.rook)
	wantMsg(t, f, task.ID, "reported", "Rook", "You", "A e gata")
}

// reviewer pe un task din board, fara autonomie: dupa aprobare decide utilizatorul
func TestConvApproveUserTaskToYou(t *testing.T) {
	f := setup(t)
	rev := f.agent(t, "Rev", &f.lead, nil)
	wes := f.agent(t, "Wes", &f.lead, &rev)
	task, _ := f.tm.UserCreateTask(f.p.ID, "A", "", wes.ID, 0)
	task = inProgress(t, f, task)
	f.tm.ReportDone(f.workerCaller(wes, task.ID), "", "gata")
	rc := mcp.Caller{ProjectID: f.p.ID, AgentID: rev.ID, HasParent: true, IsReviewer: true}
	if _, err := f.tm.ReviewApprove(rc, "T-1", ""); err != nil {
		t.Fatal(err)
	}
	wantMsg(t, f, task.ID, "approved", "Rev", "You", "Approved")
}

func TestConvReportToRootInAutonomy(t *testing.T) {
	f := setup(t)
	autonomous(t, f)
	task := userTaskInReview(t, f, f.rook)
	wantMsg(t, f, task.ID, "reported", "Rook", "Lead", "A e gata")
}

func TestConvFinishedWithoutReport(t *testing.T) {
	f := setup(t)
	task := f.task(t, "B", f.vera, store.StatusQueued)
	mustReconcile(t, f)
	f.fs.finish(f.fs.last(t, f.vera).ID, runner.Outcome{Status: "done", Result: "am terminat"})
	f.wantStatus(t, 1, store.StatusReview)
	wantMsg(t, f, task.ID, "reported", "Vera", "Lead", "am terminat")
	wantEvent(t, f, task.ID, "reported", "Vera", "Vera finished without report_done")
}

// task-ul propriu al unei radacini: raportul merge la utilizator, task-ul trece singur in Done
func TestConvFinishedWithoutReportNoDecider(t *testing.T) {
	f := setup(t)
	wes := f.agent(t, "Wes", nil, nil)
	task := store.Task{ProjectID: f.p.ID, Title: "A", AssigneeID: wes.ID}
	f.st.CreateTask(&task)
	mustReconcile(t, f)
	f.fs.finish(f.fs.last(t, wes).ID, runner.Outcome{Status: "done", Result: "gata"})
	f.wantStatus(t, 1, store.StatusDone)
	wantMsg(t, f, task.ID, "reported", "Wes", "You", "gata")
}

func TestConvAccept(t *testing.T) {
	f := setup(t)
	a := f.task(t, "A", f.rook, store.StatusReview)
	b := f.task(t, "B", f.vera, store.StatusReview)
	if _, err := f.tm.AcceptTask(f.leadCaller(), "T-1", "nice work"); err != nil {
		t.Fatal(err)
	}
	wantMsg(t, f, a.ID, "accepted", "Lead", "Rook", "nice work")
	wantEvent(t, f, a.ID, "accepted", "Lead", "Lead accepted")
	if _, err := f.tm.AcceptTask(f.leadCaller(), "T-2", ""); err != nil {
		t.Fatal(err)
	}
	wantMsg(t, f, b.ID, "accepted", "Lead", "Vera", "Accepted")
}

func TestConvSendBackByLead(t *testing.T) {
	f := setup(t)
	task := f.task(t, "A", f.rook, store.StatusReview)
	if _, err := f.tm.SendBack(f.leadCaller(), "T-1", "lipsesc testele"); err != nil {
		t.Fatal(err)
	}
	wantMsg(t, f, task.ID, "sent_back", "Lead", "Rook", "lipsesc testele")
	wantEvent(t, f, task.ID, "sent_back", "Lead", "Lead sent it back: lipsesc testele")
}

// modul autonom: radacina da task-ului blocat inca o incercare
func TestConvSendBackBlockedInAutonomy(t *testing.T) {
	f := setup(t)
	autonomous(t, f)
	task := f.task(t, "A", f.rook, store.StatusBlocked)
	if _, err := f.tm.SendBack(f.leadCaller(), "T-1", "one more try"); err != nil {
		t.Fatal(err)
	}
	wantMsg(t, f, task.ID, "sent_back", "Lead", "Rook", "one more try")
}

func TestConvUserDecide(t *testing.T) {
	f := setup(t)
	task := userTaskInReview(t, f, f.rook)
	if err := f.tm.UserDecide(task.ID, false, "mai incearca", nil); err != nil {
		t.Fatal(err)
	}
	wantMsg(t, f, task.ID, "sent_back", "You", "Rook", "mai incearca")
	toLeadReview(t, f, task)
	if err := f.tm.UserDecide(task.ID, true, "", nil); err != nil {
		t.Fatal(err)
	}
	wantMsg(t, f, task.ID, "user_accepted", "You", "Rook", "Accepted")
	wantMsgKinds(t, f, task.ID, "created", "reported", "sent_back", "user_accepted")
}

func TestConvAcceptBlockedByUser(t *testing.T) {
	f := setup(t)
	task := f.task(t, "A", f.rook, store.StatusBlocked)
	if err := f.tm.AcceptBlocked(task.ID); err != nil {
		t.Fatal(err)
	}
	wantMsg(t, f, task.ID, "user_accepted", "You", "Rook", "Accepted")
}

func TestConvAskAnswer(t *testing.T) {
	f := setup(t)
	task, spec := askOnTask(t, f, "A")
	if _, err := f.tm.AskUser(f.caller(t, spec), "Which DB?", []string{"Postgres", " MySQL "}); err != nil {
		t.Fatal(err)
	}
	wantMsg(t, f, task.ID, "asked", "Rook", "You", "Which DB?\n- Postgres\n- MySQL")
	wantEvent(t, f, task.ID, "asked", "Rook", "Rook asked you: Which DB?")
	if err := f.tm.AnswerQuestion(openQuestionIDs(t, f)[0], "Postgres"); err != nil {
		t.Fatal(err)
	}
	wantMsg(t, f, task.ID, "answered", "You", "Rook", "Postgres")
	wantEvent(t, f, task.ID, "answered", "You", "You answered")

	f2 := setup(t)
	task2, _ := askOnTask(t, f2, "B", "Continui?")
	wantMsg(t, f2, task2.ID, "asked", "Rook", "You", "Continui?")
	if err := f2.tm.DismissQuestion(openQuestionIDs(t, f2)[0]); err != nil {
		t.Fatal(err)
	}
	wantMsg(t, f2, task2.ID, "answered", "You", "Rook", "Dismissed")
}

func TestConvNudge(t *testing.T) {
	f := setup(t)
	f.st.SetPaused(f.p.ID, false)
	a := f.task(t, "A", f.rook, store.StatusReview)
	b := f.task(t, "B", f.vera, store.StatusReview)
	c := f.task(t, "C", f.vera, store.StatusInProgress) // orfan: doar istoric
	markDelivered(t, f, f.lead)
	if err := f.tm.CheckStuck(c.UpdatedAt.Add(6 * time.Minute)); err != nil {
		t.Fatal(err)
	}
	notes := undelivered(t, f, f.lead)
	if len(notes) != 1 {
		t.Fatalf("o nota pentru Lead: %+v", notes)
	}
	for _, task := range []store.Task{a, b} {
		e := wantMsg(t, f, task.ID, "nudged", "HobStack", "Lead", notes[0].Text)
		if !strings.Contains(e.Body, "T-1, T-2") {
			t.Fatalf("body: %q", e.Body)
		}
		wantEvent(t, f, task.ID, "nudged", "HobStack", "HobStack nudged Lead")
	}
	wantMsgKinds(t, f, c.ID)
}

func TestConvStages(t *testing.T) {
	f, _, _ := stagesFixture(t)
	f.tm.CreateTask(f.leadCaller(), "A", "", "", "", "Dev")
	task := f.wantStatus(t, 1, store.StatusQueued)
	mustReconcile(t, f)
	rook1 := f.fs.last(t, f.rook)
	if _, err := f.tm.ReportDone(f.caller(t, rook1), "", "dev gata"); err != nil {
		t.Fatal(err)
	}
	// raportul trece in coada etapei urmatoare: fara destinatar anume
	wantMsg(t, f, task.ID, "reported", "Rook", "", "dev gata")
	f.fs.finish(rook1.ID, runner.Outcome{Status: "done"})
	mustReconcile(t, f)
	vera1 := f.fs.last(t, f.vera)
	if _, err := f.tm.ReviewReject(f.caller(t, vera1), "T-1", "nu merge"); err != nil {
		t.Fatal(err)
	}
	// inapoi in coada etapei Dev: fara destinatar anume
	wantMsg(t, f, task.ID, "rejected", "Vera", "", "nu merge")
	wantMsgKinds(t, f, task.ID, "created", "reported", "rejected")
}

func TestConvStageApproveLastToLead(t *testing.T) {
	f, _, stages := stagesFixture(t)
	f.tm.CreateTask(f.leadCaller(), "A", "", "", "", "Dev")
	task, _ := f.st.GetTaskByNumber(f.p.ID, 1)
	task.StageID, task.AssigneeID, task.Status = &stages[1].ID, f.vera.ID, store.StatusInProgress
	f.st.SaveTask(&task)
	if _, err := f.tm.ReviewApprove(f.workerCaller(f.vera, task.ID), "T-1", ""); err != nil {
		t.Fatal(err)
	}
	f.wantStatus(t, 1, store.StatusReview)
	wantMsg(t, f, task.ID, "approved", "Vera", "Lead", "Approved")

	// trimis inapoi de lider dupa ultima etapa: in coada etapei, fara destinatar anume
	if _, err := f.tm.SendBack(f.leadCaller(), "T-1", "redo"); err != nil {
		t.Fatal(err)
	}
	wantMsg(t, f, task.ID, "sent_back", "Lead", "", "redo")
}

// un run pe etapa terminat fara raport: la lider
func TestConvStageFinishedWithoutReport(t *testing.T) {
	f, _, _ := stagesFixture(t)
	f.tm.CreateTask(f.leadCaller(), "A", "", "", "", "Dev")
	task := f.wantStatus(t, 1, store.StatusQueued)
	mustReconcile(t, f)
	f.fs.finish(f.fs.last(t, f.rook).ID, runner.Outcome{Status: "done", Result: "partial"})
	wantMsg(t, f, task.ID, "reported", "Rook", "Lead", "partial")
}
