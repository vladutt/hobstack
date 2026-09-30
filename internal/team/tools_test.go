package team

import (
	"strings"
	"testing"
	"unicode/utf8"

	"foreman/internal/mcp"
	"foreman/internal/store"
)

func TestCreateTaskAssignsWorker(t *testing.T) {
	f := setup(t)
	text, err := f.tm.CreateTask(f.leadCaller(), "Parser $ref", "Doar local", "Rook")
	if err != nil || text != "Created T-1 for Rook. They will receive it automatically." {
		t.Fatalf("%q %v", text, err)
	}
	task := f.wantStatus(t, 1, store.StatusQueued)
	if task.AssigneeID != f.rook.ID || task.Description != "Doar local" {
		t.Fatalf("task = %+v", task)
	}
	if !f.hasMessage(f.lead.ID, "Created T-1 for Rook: Parser $ref") {
		t.Error("lipseste evenimentul din thread-ul lead-ului")
	}
	if !f.hasMessage(f.rook.ID, "T-1 assigned by Lead: Parser $ref") {
		t.Error("lipseste evenimentul din thread-ul lui Rook")
	}
}

func TestCreateTaskAssigneeLoose(t *testing.T) {
	f := setup(t)
	if _, err := f.tm.CreateTask(f.leadCaller(), "A", "", "  rook "); err != nil {
		t.Fatal(err)
	}
	if task := f.wantStatus(t, 1, store.StatusQueued); task.AssigneeID != f.rook.ID {
		t.Fatalf("asignat lui %d", task.AssigneeID)
	}
}

func TestCreateTaskErrors(t *testing.T) {
	f := setup(t)
	if _, err := f.tm.CreateTask(f.leadCaller(), "A", "", "Nimeni"); err == nil || !strings.Contains(err.Error(), "Rook, Vera") {
		t.Errorf("worker necunoscut: %v", err)
	}
	if _, err := f.tm.CreateTask(f.leadCaller(), "A", "", "Lead"); err == nil {
		t.Error("task asignat lead-ului trebuia refuzat")
	}
	if _, err := f.tm.CreateTask(f.leadCaller(), "  ", "", "Rook"); err == nil {
		t.Error("titlu gol trebuia refuzat")
	}
	if _, err := f.tm.CreateTask(f.workerCaller(f.rook, 0), "A", "", "Vera"); err == nil {
		t.Error("un worker nu poate crea task-uri")
	}
	if tasks, _ := f.st.ListTasks(f.p.ID); len(tasks) != 0 {
		t.Fatalf("s-au creat task-uri: %+v", tasks)
	}
}

func TestListTasks(t *testing.T) {
	f := setup(t)
	if text, _ := f.tm.ListTasks(f.leadCaller(), ""); text != "No tasks." {
		t.Fatalf("gol: %q", text)
	}
	f.task(t, "A", f.rook, store.StatusQueued)
	b := f.task(t, "B", f.vera, store.StatusReview)
	b.Result = "gata"
	f.st.SaveTask(&b)

	all, err := f.tm.ListTasks(f.leadCaller(), "")
	if err != nil || all != "T-1 [queued] A -> Rook\nT-2 [review] B -> Vera\n  Result: gata" {
		t.Fatalf("toate: %q %v", all, err)
	}
	review, _ := f.tm.ListTasks(f.leadCaller(), "review")
	if review != "T-2 [review] B -> Vera\n  Result: gata" {
		t.Fatalf("review: %q", review)
	}
	if _, err := f.tm.ListTasks(f.leadCaller(), "gresit"); err == nil {
		t.Fatal("status necunoscut trebuia refuzat")
	}
}

func TestReportDone(t *testing.T) {
	f := setup(t)
	task := f.task(t, "A", f.rook, store.StatusInProgress)
	text, err := f.tm.ReportDone(f.workerCaller(f.rook, task.ID), "", "A e gata")
	if err != nil || text != "T-1 is in Review. Lead will check it." {
		t.Fatalf("%q %v", text, err)
	}
	if got := f.wantStatus(t, 1, store.StatusReview); got.Result != "A e gata" {
		t.Fatalf("rezultat = %q", got.Result)
	}
	pending, _ := f.st.Undelivered(f.lead.ID)
	if len(pending) != 1 || pending[0].Text != "Rook finished T-1 (review): A e gata" {
		t.Fatalf("notificari lead: %+v", pending)
	}
}

func TestReportDoneErrors(t *testing.T) {
	f := setup(t)
	queued := f.task(t, "A", f.rook, store.StatusQueued)
	running := f.task(t, "B", f.rook, store.StatusInProgress)
	cases := map[string]error{}
	_, cases["fara task"] = f.tm.ReportDone(f.workerCaller(f.rook, 0), "", "x")
	_, cases["lead"] = f.tm.ReportDone(f.leadCaller(), "", "x") // raportezi direct utilizatorului
	_, cases["task care nu e in lucru"] = f.tm.ReportDone(f.workerCaller(f.rook, queued.ID), "", "x")
	_, cases["rezumat gol"] = f.tm.ReportDone(f.workerCaller(f.rook, running.ID), "", "  ")
	for name, err := range cases {
		if err == nil {
			t.Errorf("%s: trebuia eroare", name)
		}
	}
	if err := cases["lead"]; err == nil || !strings.Contains(err.Error(), "you report directly to the user") {
		t.Errorf("lead: %v", err)
	}
}

func TestLongSummaryIsClipped(t *testing.T) {
	f := setup(t)
	task := f.task(t, "A", f.rook, store.StatusInProgress)
	long := strings.Repeat("x", 10000)
	if _, err := f.tm.ReportDone(f.workerCaller(f.rook, task.ID), "", long); err != nil {
		t.Fatal(err)
	}
	if got := f.wantStatus(t, 1, store.StatusReview); got.Result != long {
		t.Fatal("rezultatul complet trebuie pastrat pe task")
	}
	pending, _ := f.st.Undelivered(f.lead.ID)
	if n := utf8.RuneCountInString(pending[0].Text); n > noteMax+100 {
		t.Fatalf("notificarea are %d caractere", n)
	}
}

func TestSendBackThenBlock(t *testing.T) {
	f := setup(t)
	f.task(t, "A", f.rook, store.StatusReview)
	for i := 1; i <= 2; i++ {
		text, err := f.tm.SendBack(f.leadCaller(), "T-1", "mai bine")
		want := "T-1 sent back to Rook (" + string(rune('0'+i)) + "/3)."
		if err != nil || text != want {
			t.Fatalf("trimiterea %d: %q %v", i, text, err)
		}
		got := f.wantStatus(t, 1, store.StatusQueued)
		if got.Feedback != "mai bine" || got.SendBacks != i {
			t.Fatalf("dupa trimiterea %d: %+v", i, got)
		}
		got.Status, got.ReviewStage = store.StatusReview, store.StageLead // worker-ul a raportat din nou
		f.st.SaveTask(&got)
	}
	text, err := f.tm.SendBack(f.leadCaller(), "1", "tot nu")
	if err != nil || !strings.Contains(text, "blocked") {
		t.Fatalf("a treia trimitere: %q %v", text, err)
	}
	f.wantStatus(t, 1, store.StatusBlocked)
}

func TestSendBackErrors(t *testing.T) {
	f := setup(t)
	f.task(t, "A", f.rook, store.StatusQueued)
	if _, err := f.tm.SendBack(f.leadCaller(), "T-1", "x"); err == nil {
		t.Error("task din queued nu se poate trimite inapoi")
	}
	if _, err := f.tm.SendBack(f.leadCaller(), "T-1", " "); err == nil {
		t.Error("feedback gol trebuia refuzat")
	}
	if _, err := f.tm.SendBack(f.leadCaller(), "T-99", "x"); err == nil || !strings.Contains(err.Error(), "T-99") {
		t.Errorf("task inexistent: %v", err)
	}
	if _, err := f.tm.SendBack(f.leadCaller(), "abc", "x"); err == nil {
		t.Error("referinta invalida trebuia refuzata")
	}
}

func TestAcceptTask(t *testing.T) {
	f := setup(t)
	f.task(t, "A", f.rook, store.StatusReview)
	f.task(t, "B", f.rook, store.StatusQueued)
	if text, err := f.tm.AcceptTask(f.leadCaller(), "t-1", "bun"); err != nil || text != "T-1 is Done." {
		t.Fatalf("%q %v", text, err)
	}
	f.wantStatus(t, 1, store.StatusDone)
	if !f.hasMessage(f.lead.ID, "Accepted T-1: bun") {
		t.Error("lipseste evenimentul de accept")
	}
	if _, err := f.tm.AcceptTask(f.leadCaller(), "T-2", ""); err == nil {
		t.Error("task din queued nu se poate accepta")
	}
}

func TestResolveUnknownToken(t *testing.T) {
	f := setup(t)
	if _, ok := f.tm.Resolve("nimic"); ok {
		t.Fatal("token necunoscut rezolvat")
	}
}

func TestCreateTaskDirectReportsOnly(t *testing.T) {
	f := setup(t)
	sub := f.agent(t, "Sub", &f.lead, nil)
	wes := f.agent(t, "Wes", &sub, nil)
	_, err := f.tm.CreateTask(f.leadCaller(), "A", "", "Wes")
	if err == nil || !strings.Contains(err.Error(), `"Wes" is not on your direct team. Your direct team: Rook, Vera, Sub`) {
		t.Fatalf("worker indirect: %v", err)
	}
	// flag vechi: Rook a fost lider cand a pornit run-ul, dar acum nu mai are copii
	stale := mcp.Caller{ProjectID: f.p.ID, AgentID: f.rook.ID, IsLead: true, HasParent: true}
	if _, err := f.tm.CreateTask(stale, "A", "", "Vera"); err == nil || !strings.Contains(err.Error(), "only a lead") {
		t.Fatalf("lider fara copii: %v", err)
	}
	parent := f.task(t, "Import", sub, store.StatusInProgress)
	sc := mcp.Caller{ProjectID: f.p.ID, AgentID: sub.ID, IsLead: true, HasParent: true, TaskID: parent.ID}
	if _, err := f.tm.CreateTask(sc, "Parser", "", "wes"); err != nil {
		t.Fatal(err)
	}
	got := f.wantStatus(t, 2, store.StatusQueued)
	if got.AssigneeID != wes.ID || *got.CreatorID != sub.ID || got.ParentTaskID == nil || *got.ParentTaskID != parent.ID {
		t.Fatalf("sub-task: %+v", got)
	}
	// run de notificari (fara task), dar cu un singur task in lucru: tot sub-task al lui
	sc.TaskID = 0
	f.tm.CreateTask(sc, "Lexer", "", "Wes")
	if got := f.wantStatus(t, 3, store.StatusQueued); got.ParentTaskID == nil || *got.ParentTaskID != parent.ID {
		t.Fatalf("T-3 parinte: %+v", got.ParentTaskID)
	}
}

func TestReportDoneTaskParam(t *testing.T) {
	f := setup(t)
	sub := f.agent(t, "Sub", &f.lead, nil)
	wes := f.agent(t, "Wes", &sub, nil)
	parent := f.task(t, "Import", sub, store.StatusInProgress)
	child := store.Task{ProjectID: f.p.ID, Title: "Parser", AssigneeID: wes.ID, CreatorID: &sub.ID, ParentTaskID: &parent.ID}
	f.st.CreateTask(&child)
	sc := mcp.Caller{ProjectID: f.p.ID, AgentID: sub.ID, IsLead: true, HasParent: true} // run de notificari
	errs := map[string]string{"": "say which task you are reporting on", "T-1": "still has open subtasks: T-2", "T-2": "is not assigned to you"}
	for ref, want := range errs {
		if _, err := f.tm.ReportDone(sc, ref, "x"); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("ref %q: %v, vreau %q", ref, err, want)
		}
	}
	child.Status = store.StatusDone
	f.st.SaveTask(&child)
	text, err := f.tm.ReportDone(sc, "t-1", "Import gata")
	if err != nil || text != "T-1 is in Review. Lead will check it." {
		t.Fatalf("%q %v", text, err)
	}
	if got := f.wantStatus(t, 1, store.StatusReview); got.ReviewStage != store.StageLead || got.Result != "Import gata" {
		t.Fatalf("T-1: %+v", got)
	}
}

func TestReviewApproveAndReject(t *testing.T) {
	f := setup(t)
	f.rook.ReviewerID = &f.vera.ID
	if err := f.st.UpdateAgent(f.rook); err != nil {
		t.Fatal(err)
	}
	task := f.task(t, "A", f.rook, store.StatusInProgress)
	text, err := f.tm.ReportDone(f.workerCaller(f.rook, task.ID), "", "A e gata")
	if err != nil || text != "T-1 is in Review. Vera will check it." {
		t.Fatalf("%q %v", text, err)
	}
	if got := f.wantStatus(t, 1, store.StatusReview); got.ReviewStage != store.StageReviewer {
		t.Fatalf("stage = %q", got.ReviewStage)
	}
	pending, _ := f.st.Undelivered(f.vera.ID)
	if len(pending) != 1 || pending[0].Text != "Rook finished T-1. Check it with review_approve or review_reject: A e gata" {
		t.Fatalf("notificari Vera: %+v", pending)
	}
	if p, _ := f.st.Undelivered(f.lead.ID); len(p) != 0 {
		t.Fatal("lead-ul nu trebuie notificat inainte de review")
	}
	if _, err := f.tm.AcceptTask(f.leadCaller(), "T-1", ""); err == nil || !strings.Contains(err.Error(), "still in review with Vera") {
		t.Fatalf("accept inainte de review: %v", err)
	}
	vc := mcp.Caller{ProjectID: f.p.ID, AgentID: f.vera.ID, HasParent: true, IsReviewer: true}
	other := mcp.Caller{ProjectID: f.p.ID, AgentID: f.lead.ID, IsLead: true, IsReviewer: true}
	if _, err := f.tm.ReviewApprove(other, "T-1", ""); err == nil || !strings.Contains(err.Error(), "you do not review the work of Rook") {
		t.Fatalf("alt reviewer: %v", err)
	}
	if _, err := f.tm.ReviewReject(vc, "T-1", "  "); err == nil {
		t.Fatal("feedback gol trebuia refuzat")
	}

	text, err = f.tm.ReviewReject(vc, "T-1", "Lipsesc testele")
	if err != nil || text != "T-1 sent back to Rook (1/3)." {
		t.Fatalf("reject: %q %v", text, err)
	}
	got := f.wantStatus(t, 1, store.StatusQueued)
	if got.Feedback != "Lipsesc testele" || got.SendBacks != 1 || got.ReviewStage != store.StageNone {
		t.Fatalf("dupa reject: %+v", got)
	}
	if !f.hasMessage(f.rook.ID, "T-1 rejected in review by Vera: Lipsesc testele") {
		t.Fatal("Rook trebuie sa vada respingerea")
	}

	got.Status, got.ReviewStage = store.StatusReview, store.StageReviewer
	f.st.SaveTask(&got)
	text, err = f.tm.ReviewApprove(vc, "T-1", "arata bine")
	if err != nil || text != "T-1 went to Lead for a decision." {
		t.Fatalf("approve: %q %v", text, err)
	}
	if got := f.wantStatus(t, 1, store.StatusReview); got.ReviewStage != store.StageLead {
		t.Fatalf("stage dupa approve = %q", got.ReviewStage)
	}
	pending, _ = f.st.Undelivered(f.lead.ID)
	last := pending[len(pending)-1].Text
	if !strings.HasPrefix(last, "Vera approved T-1, done by Rook (review): A e gata") || !strings.HasSuffix(last, "Note: arata bine") {
		t.Fatalf("notificare lead: %q", last)
	}
	if _, err := f.tm.ReviewApprove(vc, "T-1", ""); err == nil {
		t.Fatal("a doua aprobare trebuia refuzata")
	}
}

func TestReviewRejectBlocksAtThree(t *testing.T) {
	f := setup(t)
	f.rook.ReviewerID = &f.vera.ID
	f.st.UpdateAgent(f.rook)
	task := f.task(t, "A", f.rook, store.StatusReview)
	vc := mcp.Caller{ProjectID: f.p.ID, AgentID: f.vera.ID, HasParent: true, IsReviewer: true}
	for i := 1; i <= 3; i++ {
		task, _ = f.st.GetTask(task.ID)
		task.Status, task.ReviewStage = store.StatusReview, store.StageReviewer
		f.st.SaveTask(&task)
		if _, err := f.tm.ReviewReject(vc, "T-1", "tot nu"); err != nil {
			t.Fatal(err)
		}
	}
	f.wantStatus(t, 1, store.StatusBlocked)
	if !f.hasMessage(f.lead.ID, "T-1 is blocked after 3 rejections or send-backs") {
		t.Fatal("decidentul trebuie anuntat de blocare")
	}
}

func TestReviewWithoutDeciderBlocksTask(t *testing.T) {
	f := setup(t)
	// task-ul ajunge in review prin run-ul terminat fara report_done, dupa ce
	// asignatul a ramas fara sef si fara creator
	wes := f.agent(t, "Wes", nil, nil)
	task := f.task(t, "A", wes, store.StatusInProgress)
	task.CreatorID = nil
	task.Result = "gata"
	f.st.SaveTask(&task)
	agents, _ := f.st.ListAgents(f.p.ID)
	who, err := f.tm.toReview(&task, wes, agents, "Wes a terminat T-1")
	if err != nil {
		t.Fatal(err)
	}
	if who.ID != 0 {
		t.Fatalf("nu trebuia notificat nimeni: %+v", who)
	}
	got := f.wantStatus(t, 1, store.StatusBlocked)
	if got.ReviewStage != store.StageNone {
		t.Fatalf("stage = %q", got.ReviewStage)
	}
	if !f.hasMessage(wes.ID, "No one can check T-1. The user decides from the board (Accept / Unblock).") {
		t.Fatal("asignatul trebuie anuntat")
	}
}

func TestAcceptOnlyByCreatorOrAncestor(t *testing.T) {
	f := setup(t)
	sub := f.agent(t, "Sub", &f.lead, nil)
	wes := f.agent(t, "Wes", &sub, nil)
	task := store.Task{ProjectID: f.p.ID, Title: "A", AssigneeID: wes.ID, CreatorID: &sub.ID}
	f.st.CreateTask(&task)
	task.Status, task.ReviewStage = store.StatusReview, store.StageLead
	f.st.SaveTask(&task)
	stranger := mcp.Caller{ProjectID: f.p.ID, AgentID: f.rook.ID, IsLead: true, HasParent: true}
	if _, err := f.tm.AcceptTask(stranger, "T-1", ""); err == nil || !strings.Contains(err.Error(), "only the lead who created T-1") {
		t.Fatalf("accept strain: %v", err)
	}
	if _, err := f.tm.SendBack(stranger, "T-1", "x"); err == nil {
		t.Fatal("send_back strain trebuia refuzat")
	}
	if _, err := f.tm.AcceptTask(f.leadCaller(), "T-1", ""); err != nil {
		t.Fatalf("stramosul creatorului: %v", err)
	}
}

func TestAcceptWithoutCreatorByAncestor(t *testing.T) {
	f := setup(t)
	sub := f.agent(t, "Sub", &f.lead, nil)
	wes := f.agent(t, "Wes", &sub, nil)
	task := store.Task{ProjectID: f.p.ID, Title: "A", AssigneeID: wes.ID, CreatorID: &sub.ID}
	f.st.CreateTask(&task)
	task.Status, task.ReviewStage = store.StatusReview, store.StageLead
	f.st.SaveTask(&task)
	if err := f.st.DeleteAgent(sub.ID); err != nil { // Wes trece la Lead, creator_id devine NULL
		t.Fatal(err)
	}
	stranger := mcp.Caller{ProjectID: f.p.ID, AgentID: f.rook.ID, IsLead: true, HasParent: true}
	if _, err := f.tm.AcceptTask(stranger, "T-1", ""); err == nil {
		t.Fatal("Rook nu e sef al lui Wes")
	}
	if _, err := f.tm.AcceptTask(f.leadCaller(), "T-1", ""); err != nil {
		t.Fatalf("seful asignatului: %v", err)
	}
}

func TestSubtaskAcceptRequeuesParent(t *testing.T) {
	f := setup(t)
	sub := f.agent(t, "Sub", &f.lead, nil)
	wes := f.agent(t, "Wes", &sub, nil)
	parent := f.task(t, "Import", sub, store.StatusInProgress)
	for _, title := range []string{"A", "B"} {
		c := store.Task{ProjectID: f.p.ID, Title: title, AssigneeID: wes.ID, CreatorID: &sub.ID, ParentTaskID: &parent.ID}
		f.st.CreateTask(&c)
		c.Status, c.ReviewStage = store.StatusReview, store.StageLead
		f.st.SaveTask(&c)
	}
	sc := mcp.Caller{ProjectID: f.p.ID, AgentID: sub.ID, IsLead: true, HasParent: true}
	f.tm.AcceptTask(sc, "T-2", "")
	f.wantStatus(t, 1, store.StatusInProgress) // T-3 e inca deschis
	f.tm.AcceptTask(sc, "T-3", "")
	f.wantStatus(t, 1, store.StatusQueued)
	if !f.hasMessage(sub.ID, "All subtasks for T-1 are done") {
		t.Fatal("lipseste nota din thread")
	}
	if pending, _ := f.st.Undelivered(sub.ID); len(pending) != 0 {
		t.Fatalf("nu mai trebuie notificare separata: %+v", pending)
	}
	// sub-liderul primeste un run pe task-ul parinte, cu lista sub-task-urilor
	mustReconcile(t, f)
	spec := f.fs.last(t, sub)
	got := turn(spec)
	if f.caller(t, spec).TaskID != parent.ID || !strings.Contains(got, "Task T-1: Import") ||
		!strings.Contains(got, "- T-2 [done] A -> Wes\n- T-3 [done] B -> Wes") {
		t.Fatalf("tura sub-liderului:\n%s", got)
	}
}

func TestCreateTaskAmbiguousParent(t *testing.T) {
	f := setup(t)
	sub := f.agent(t, "Sub", &f.lead, nil)
	f.agent(t, "Wes", &sub, nil)
	f.task(t, "Unu", sub, store.StatusInProgress)
	f.task(t, "Doi", sub, store.StatusInProgress)
	sc := mcp.Caller{ProjectID: f.p.ID, AgentID: sub.ID, IsLead: true, HasParent: true} // run de notificari
	text, err := f.tm.CreateTask(sc, "Parser", "", "Wes")
	if err != nil || text != "Created T-3 for Wes. They will receive it automatically. Created without a parent task: you have several tasks in progress." {
		t.Fatalf("%q %v", text, err)
	}
	if got := f.wantStatus(t, 3, store.StatusQueued); got.ParentTaskID != nil {
		t.Fatalf("parinte: %v", *got.ParentTaskID)
	}
}
