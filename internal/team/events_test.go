package team

import (
	"slices"
	"strings"
	"testing"
	"time"

	"foreman/internal/mcp"
	"foreman/internal/runner"
	"foreman/internal/store"
)

func TestTaskEventTruncatesText(t *testing.T) {
	f := setup(t)
	task := store.Task{ProjectID: f.p.ID, Title: "t", AssigneeID: f.rook.ID}
	if err := f.st.CreateTask(&task); err != nil {
		t.Fatal(err)
	}
	f.tm.taskEvent(task, "rejected", "Vera", strings.Repeat("ă", 300))
	f.tm.taskEvent(task, "created", "You", "short")
	evs, err := f.st.ListTaskEvents(task.ID)
	if err != nil || len(evs) != 2 {
		t.Fatalf("%+v %v", evs, err)
	}
	if r := []rune(evs[0].Text); len(r) != 203 || !strings.HasSuffix(evs[0].Text, "...") {
		t.Fatalf("lungime %d", len(r))
	}
	if evs[1].Text != "short" || evs[1].Actor != "You" || evs[1].Kind != "created" {
		t.Fatalf("%+v", evs[1])
	}
}

// wantEvent cere ca istoricul task-ului sa contina un eveniment de felul kind,
// al lui actor, cu textul text (substring).
func wantEvent(t *testing.T, f *fixture, taskID int64, kind, actor, text string) {
	t.Helper()
	evs, err := f.st.ListTaskEvents(taskID)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range evs {
		if e.Kind == kind && e.Actor == actor && strings.Contains(e.Text, text) {
			return
		}
	}
	t.Fatalf("lipseste %s/%s %q in %+v", kind, actor, text, evs)
}

// eventKinds intoarce felurile evenimentelor, in ordine.
func eventKinds(t *testing.T, f *fixture, taskID int64) []string {
	t.Helper()
	evs, err := f.st.ListTaskEvents(taskID)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range evs {
		out = append(out, e.Kind)
	}
	return out
}

// drumul complet fara etape: creat, pornit, raportat, trimis inapoi, acceptat
func TestEventsLeadFlow(t *testing.T) {
	f := setup(t)
	if _, err := f.tm.CreateTask(f.leadCaller(), "Login", "", "Rook"); err != nil {
		t.Fatal(err)
	}
	task := f.wantStatus(t, 1, store.StatusQueued)
	wantEvent(t, f, task.ID, "created", "Lead", "Created by Lead")
	wantEvent(t, f, task.ID, "assigned", "Lead", "Assigned to Rook")

	mustReconcile(t, f)
	wantEvent(t, f, task.ID, "started", "Rook", "Rook started working")
	rook1 := f.fs.last(t, f.rook)
	if _, err := f.tm.ReportDone(f.caller(t, rook1), "", "gata"); err != nil {
		t.Fatal(err)
	}
	wantEvent(t, f, task.ID, "reported", "Rook", "Rook reported it done")
	f.fs.finish(rook1.ID, runner.Outcome{Status: "done"})

	if _, err := f.tm.SendBack(f.leadCaller(), "T-1", "lipsesc testele"); err != nil {
		t.Fatal(err)
	}
	wantEvent(t, f, task.ID, "sent_back", "Lead", "Lead sent it back: lipsesc testele")

	task = f.wantStatus(t, 1, store.StatusQueued)
	task.Status, task.ReviewStage = store.StatusReview, store.StageLead
	f.st.SaveTask(&task)
	if _, err := f.tm.AcceptTask(f.leadCaller(), "T-1", ""); err != nil {
		t.Fatal(err)
	}
	wantEvent(t, f, task.ID, "accepted", "Lead", "Lead accepted")
	want := []string{"created", "assigned", "started", "reported", "sent_back", "accepted"}
	if got := eventKinds(t, f, task.ID); !slices.Equal(got, want) {
		t.Fatalf("ordinea: %v", got)
	}
}

func TestEventsBlockedAfterSendBacks(t *testing.T) {
	f := setup(t)
	task := f.task(t, "A", f.rook, store.StatusReview)
	for i := 0; i < MaxSendBacks; i++ {
		task, _ = f.st.GetTask(task.ID)
		task.Status, task.ReviewStage = store.StatusReview, store.StageLead
		f.st.SaveTask(&task)
		if _, err := f.tm.SendBack(f.leadCaller(), "T-1", "nu"); err != nil {
			t.Fatal(err)
		}
	}
	f.wantStatus(t, 1, store.StatusBlocked)
	wantEvent(t, f, task.ID, "blocked", "Foreman", "Blocked after 3 send-backs")

	if err := f.tm.Unblock(task.ID); err != nil {
		t.Fatal(err)
	}
	wantEvent(t, f, task.ID, "unblocked", "You", "You unblocked it")
}

func TestEventsAcceptBlocked(t *testing.T) {
	f := setup(t)
	task := f.task(t, "A", f.rook, store.StatusBlocked)
	if err := f.tm.AcceptBlocked(task.ID); err != nil {
		t.Fatal(err)
	}
	wantEvent(t, f, task.ID, "user_accepted", "You", "You accepted")
}

func TestEventsReviewer(t *testing.T) {
	f := setup(t)
	rev := f.agent(t, "Rev", &f.lead, nil)
	wes := f.agent(t, "Wes", &f.lead, &rev)
	for _, title := range []string{"A", "B"} {
		if _, err := f.tm.CreateTask(f.leadCaller(), title, "", "Wes"); err != nil {
			t.Fatal(err)
		}
	}
	for _, n := range []int{1, 2} {
		task := f.wantStatus(t, n, store.StatusQueued)
		task.Status = store.StatusInProgress
		f.st.SaveTask(&task)
		if _, err := f.tm.ReportDone(f.workerCaller(wes, task.ID), "", "gata"); err != nil {
			t.Fatal(err)
		}
	}
	rc := mcp.Caller{ProjectID: f.p.ID, AgentID: rev.ID, HasParent: true, IsReviewer: true}
	if _, err := f.tm.ReviewApprove(rc, "T-1", ""); err != nil {
		t.Fatal(err)
	}
	t1 := f.wantStatus(t, 1, store.StatusReview)
	wantEvent(t, f, t1.ID, "approved", "Rev", "Rev approved")
	if _, err := f.tm.ReviewReject(rc, "T-2", "fara teste"); err != nil {
		t.Fatal(err)
	}
	t2 := f.wantStatus(t, 2, store.StatusQueued)
	wantEvent(t, f, t2.ID, "rejected", "Rev", "Rev rejected: fara teste")
}

// fara decident: run terminat fara report_done (toReview) -> blocat, cu motivul
func TestEventsBlockedWithoutDecider(t *testing.T) {
	f := setup(t)
	wes := f.agent(t, "Wes", nil, nil)
	task := store.Task{ProjectID: f.p.ID, Title: "A", AssigneeID: wes.ID} // fara creator
	if err := f.st.CreateTask(&task); err != nil {
		t.Fatal(err)
	}
	mustReconcile(t, f)
	f.fs.finish(f.fs.last(t, wes).ID, runner.Outcome{Status: "done", Result: "gata"})
	f.wantStatus(t, 1, store.StatusBlocked)
	wantEvent(t, f, task.ID, "reported", "Wes", "Wes finished without report_done")
	wantEvent(t, f, task.ID, "blocked", "Foreman", "no one can check it")
	want := []string{"started", "reported", "blocked"}
	if got := eventKinds(t, f, task.ID); !slices.Equal(got, want) {
		t.Fatalf("ordinea: %v", got)
	}
}

func TestEventsUserTaskAndAssign(t *testing.T) {
	f := setup(t)
	task, err := f.tm.UserCreateTask(f.p.ID, "A", "", f.rook.ID)
	if err != nil {
		t.Fatal(err)
	}
	wantEvent(t, f, task.ID, "created", "You", "Created by you")
	wantEvent(t, f, task.ID, "assigned", "You", "Assigned to Rook")
	if err := f.tm.AssignTask(task.ID, f.vera.ID); err != nil {
		t.Fatal(err)
	}
	wantEvent(t, f, task.ID, "assigned", "You", "Assigned to Vera")

	task.Status, task.ReviewStage, task.AssigneeID = store.StatusReview, store.StageLead, f.vera.ID
	f.st.SaveTask(&task)
	if err := f.tm.UserDecide(task.ID, false, "mai incearca"); err != nil {
		t.Fatal(err)
	}
	wantEvent(t, f, task.ID, "sent_back", "You", "You sent it back: mai incearca")
	task, _ = f.st.GetTask(task.ID)
	task.Status, task.ReviewStage = store.StatusReview, store.StageLead
	f.st.SaveTask(&task)
	if err := f.tm.UserDecide(task.ID, true, ""); err != nil {
		t.Fatal(err)
	}
	wantEvent(t, f, task.ID, "user_accepted", "You", "You accepted")
}

func TestEventsStages(t *testing.T) {
	f, _, _ := stagesFixture(t)
	if _, err := f.tm.CreateTask(f.leadCaller(), "A", "", ""); err != nil {
		t.Fatal(err)
	}
	task := f.wantStatus(t, 1, store.StatusQueued)
	wantEvent(t, f, task.ID, "created", "Lead", "Created by Lead")
	wantEvent(t, f, task.ID, "assigned", "Lead", "Queued for stage Dev")
	u, _ := f.tm.UserCreateTask(f.p.ID, "B", "", 0)
	wantEvent(t, f, u.ID, "created", "You", "Created by you")
	wantEvent(t, f, u.ID, "assigned", "You", "Queued for stage Dev")

	mustReconcile(t, f)
	rook1 := f.fs.last(t, f.rook)
	if _, err := f.tm.ReportDone(f.caller(t, rook1), "", "gata"); err != nil {
		t.Fatal(err)
	}
	wantEvent(t, f, task.ID, "reported", "Rook", "Rook reported it done")
	wantEvent(t, f, task.ID, "stage", "Rook", "Moved to stage QA")
	f.fs.finish(rook1.ID, runner.Outcome{Status: "done"})
	mustReconcile(t, f)
	vera1 := f.fs.last(t, f.vera)
	wantEvent(t, f, task.ID, "started", "Vera", "Vera started working")
	if _, err := f.tm.ReviewReject(f.caller(t, vera1), "T-1", "nu merge"); err != nil {
		t.Fatal(err)
	}
	wantEvent(t, f, task.ID, "rejected", "Vera", "Vera rejected: nu merge")
	wantEvent(t, f, task.ID, "stage", "Vera", "Moved to stage Dev")
	want := []string{"created", "assigned", "started", "reported", "stage", "started", "rejected", "stage"}
	if got := eventKinds(t, f, task.ID); !slices.Equal(got, want) {
		t.Fatalf("ordinea: %v", got)
	}
}

func TestEventsStageApprove(t *testing.T) {
	f, _, stages := stagesFixture(t)
	f.tm.CreateTask(f.leadCaller(), "A", "", "")
	task, _ := f.st.GetTaskByNumber(f.p.ID, 1)
	task.StageID, task.AssigneeID, task.Status = &stages[1].ID, f.vera.ID, store.StatusInProgress
	f.st.SaveTask(&task)
	c := f.workerCaller(f.vera, task.ID)
	if _, err := f.tm.ReviewApprove(c, "T-1", ""); err != nil {
		t.Fatal(err)
	}
	f.wantStatus(t, 1, store.StatusReview)
	wantEvent(t, f, task.ID, "approved", "Vera", "Vera approved")
}

func TestEventsQuestions(t *testing.T) {
	f := setup(t)
	task, _ := askOnTask(t, f, "A", "Ce baza de date?")
	wantEvent(t, f, task.ID, "asked", "Rook", "Rook asked you: Ce baza de date?")
	ids := openQuestionIDs(t, f)
	if err := f.tm.AnswerQuestion(ids[0], "Postgres"); err != nil {
		t.Fatal(err)
	}
	wantEvent(t, f, task.ID, "answered", "You", "You answered")

	f2 := setup(t)
	task2, _ := askOnTask(t, f2, "B", "Continui?")
	if err := f2.tm.DismissQuestion(openQuestionIDs(t, f2)[0]); err != nil {
		t.Fatal(err)
	}
	wantEvent(t, f2, task2.ID, "answered", "You", "You dismissed the question")
}

func TestEventsRunOutcomes(t *testing.T) {
	f := setup(t)
	a := f.task(t, "A", f.rook, store.StatusQueued)
	mustReconcile(t, f)
	f.fs.finish(f.fs.last(t, f.rook).ID, runner.Outcome{Status: "failed", Error: "Reached maximum budget"})
	wantEvent(t, f, a.ID, "failed", "Rook", "Rook's run failed: Reached maximum budget")

	b := f.task(t, "B", f.vera, store.StatusQueued)
	mustReconcile(t, f)
	f.fs.finish(f.fs.last(t, f.vera).ID, runner.Outcome{Status: "done", Result: "am terminat"})
	f.wantStatus(t, 2, store.StatusReview)
	wantEvent(t, f, b.ID, "reported", "Vera", "Vera finished without report_done")
}

func TestEventsWaitingSubtasks(t *testing.T) {
	f := setup(t)
	sub := f.agent(t, "Sub", &f.lead, nil)
	f.agent(t, "Wes", &sub, nil)
	parent := f.task(t, "Import", sub, store.StatusQueued)
	mustReconcile(t, f)
	sub1 := f.fs.last(t, sub)
	if _, err := f.tm.CreateTask(f.caller(t, sub1), "Parser", "", "Wes"); err != nil {
		t.Fatal(err)
	}
	f.fs.finish(sub1.ID, runner.Outcome{Status: "done"})
	wantEvent(t, f, parent.ID, "waiting_subtasks", "Sub", "Waiting for subtasks T-2")

	// sub-task-ul acceptat: parintele revine in coada
	t2 := f.wantStatus(t, 2, store.StatusQueued)
	t2.Status, t2.ReviewStage = store.StatusReview, store.StageLead
	f.st.SaveTask(&t2)
	sc := mcp.Caller{ProjectID: f.p.ID, AgentID: sub.ID, HasParent: true, IsLead: true}
	if _, err := f.tm.AcceptTask(sc, "T-2", ""); err != nil {
		t.Fatal(err)
	}
	f.wantStatus(t, 1, store.StatusQueued)
	wantEvent(t, f, parent.ID, "requeued", "Foreman", "All subtasks are done")
}

func TestEventsStuck(t *testing.T) {
	f := setup(t)
	f.st.SetPaused(f.p.ID, false)
	a := f.task(t, "A", f.rook, store.StatusReview)
	b := f.task(t, "B", f.vera, store.StatusInProgress)
	markDelivered(t, f, f.lead)
	if err := f.tm.CheckStuck(b.UpdatedAt.Add(6 * time.Minute)); err != nil {
		t.Fatal(err)
	}
	wantEvent(t, f, a.ID, "nudged", "Foreman", "Foreman nudged Lead")
	wantEvent(t, f, b.ID, "requeued", "Foreman", "Foreman requeued it")
}

// countKind numara evenimentele de felul kind ale task-ului.
func countKind(t *testing.T, f *fixture, taskID int64, kind string) int {
	t.Helper()
	n := 0
	for _, k := range eventKinds(t, f, taskID) {
		if k == kind {
			n++
		}
	}
	return n
}

func TestStopRecordsRequeuedOnce(t *testing.T) {
	f := setup(t)
	task := f.task(t, "A", f.rook, store.StatusQueued)
	mustReconcile(t, f)
	run := f.fs.last(t, f.rook)
	if err := f.tm.Stop(f.p.ID); err != nil {
		t.Fatal(err)
	}
	wantEvent(t, f, task.ID, "requeued", "Foreman", "Stopped; back in the queue")
	// finish(stopped) vine dupa Stop: task-ul nu mai e In progress, nu se dubleaza
	f.fs.finish(run.ID, runner.Outcome{Status: "stopped"})
	if n := countKind(t, f, task.ID, "requeued"); n != 1 {
		t.Fatalf("requeued: %d", n)
	}
}

func TestRunStoppedRecordsRequeued(t *testing.T) {
	f := setup(t)
	task := f.task(t, "A", f.rook, store.StatusQueued)
	mustReconcile(t, f)
	// run-ul se opreste singur (fara Stop pe echipa)
	f.fs.finish(f.fs.last(t, f.rook).ID, runner.Outcome{Status: "stopped"})
	f.wantStatus(t, 1, store.StatusQueued)
	if n := countKind(t, f, task.ID, "requeued"); n != 1 {
		t.Fatalf("requeued: %d", n)
	}
}

func TestRecoverRecordsRequeuedSkipsWaiting(t *testing.T) {
	f := setup(t)
	sub := f.agent(t, "Sub", &f.lead, nil)
	wes := f.agent(t, "Wes", &sub, nil)
	parent := f.task(t, "Import", sub, store.StatusInProgress)
	child := store.Task{ProjectID: f.p.ID, Title: "Parser", AssigneeID: wes.ID, CreatorID: &sub.ID, ParentTaskID: &parent.ID}
	f.st.CreateTask(&child)
	plain := f.task(t, "B", f.rook, store.StatusInProgress)
	if err := f.tm.Recover(); err != nil {
		t.Fatal(err)
	}
	wantEvent(t, f, plain.ID, "requeued", "Foreman", "back in the queue")
	if n := countKind(t, f, parent.ID, "requeued"); n != 0 {
		t.Fatalf("task-ul care asteapta sub-task-uri nu se pune in coada: %d", n)
	}
}

func TestTaskMsgTruncates(t *testing.T) {
	f := setup(t)
	task := store.Task{ProjectID: f.p.ID, Title: "t", AssigneeID: f.rook.ID}
	if err := f.st.CreateTask(&task); err != nil {
		t.Fatal(err)
	}
	f.tm.taskMsg(task, "reported", "Rook", "Vera", strings.Repeat("a", 300), strings.Repeat("ă", 20005))
	f.tm.taskMsg(task, "created", "You", "Rook", "short", "full")
	evs, err := f.st.ListTaskEvents(task.ID)
	if err != nil || len(evs) != 2 {
		t.Fatalf("%+v %v", evs, err)
	}
	if r := []rune(evs[0].Text); len(r) != 203 {
		t.Fatalf("text %d", len(r))
	}
	if !strings.HasSuffix(evs[0].Body, " [...]") || len([]rune(evs[0].Body)) != 20006 || evs[0].To != "Vera" {
		t.Fatalf("body %d to %q", len([]rune(evs[0].Body)), evs[0].To)
	}
	if evs[1].Body != "full" || evs[1].To != "Rook" {
		t.Fatalf("%+v", evs[1])
	}
}
