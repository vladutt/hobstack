package team

import (
	"errors"
	"strconv"
	"strings"
	"testing"

	"foreman/internal/mcp"
	"foreman/internal/runner"
	"foreman/internal/store"
)

// linked creeaza proiectul Backend (cu radacina lui, "Lead") si il leaga de proiectul fixture-ului.
func linked(t *testing.T, f *fixture) (store.Project, store.Agent) {
	t.Helper()
	p := store.Project{Name: "Backend", Goal: "Serve the API", Workdir: t.TempDir(), MaxParallel: 2}
	root, err := f.st.CreateProject(&p)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.st.LinkProjects(f.p.ID, p.ID); err != nil {
		t.Fatal(err)
	}
	return p, root
}

func rootCaller(p store.Project, root store.Agent) mcp.Caller {
	return mcp.Caller{ProjectID: p.ID, AgentID: root.ID, IsRoot: true, CanAddAgents: true, HasLinks: true}
}

func lastMessage(t *testing.T, f *fixture, agentID int64) store.Message {
	t.Helper()
	msgs, err := f.st.ListMessages(agentID)
	if err != nil || len(msgs) == 0 {
		t.Fatalf("niciun mesaj: %v", err)
	}
	return msgs[len(msgs)-1]
}

func TestListLinkedProjects(t *testing.T) {
	f := setup(t)
	if text, err := f.tm.ListLinkedProjects(f.leadCaller()); err != nil || text != "This project has no linked projects." {
		t.Fatalf("%q %v", text, err)
	}
	linked(t, f)
	text, err := f.tm.ListLinkedProjects(f.leadCaller())
	if err != nil || !strings.Contains(text, "Backend (root: Lead): Serve the API") {
		t.Fatalf("%q %v", text, err)
	}
}

func TestAskProjectAndReply(t *testing.T) {
	f := setup(t)
	bp, broot := linked(t, f)
	c := f.workerCaller(f.rook, 0)
	text, err := f.tm.AskProject(c, "backend", "Which format does /users return?")
	if err != nil || !strings.Contains(text, "Question sent to Backend as Q-") || strings.Contains(text, "paused") {
		t.Fatalf("%q %v", text, err)
	}
	note := lastMessage(t, f, broot.ID)
	if note.Kind != store.KindEvent || note.Author == store.AuthorUser || note.Delivered || note.RequestID == nil ||
		!strings.Contains(note.Text, "Question from ApiUnit (Rook) [Q-") || !strings.Contains(note.Text, "Which format does /users return?") {
		t.Fatalf("nota la radacina: %+v", note)
	}
	ref := "Q-" + itoa(*note.RequestID)
	reply, err := f.tm.ReplyToProject(rootCaller(bp, broot), strings.ToLower(ref), "JSON with id and name")
	if err != nil || !strings.Contains(reply, "Answer sent for "+ref) {
		t.Fatalf("%q %v", reply, err)
	}
	ans := lastMessage(t, f, f.rook.ID)
	if ans.Kind != store.KindEvent || ans.Delivered || ans.Text != "Answer from Backend to "+ref+": JSON with id and name" {
		t.Fatalf("raspunsul: %+v", ans)
	}
	r, _ := f.st.GetProjectRequest(*note.RequestID)
	if r.Status != store.ReqAnswered || r.Answer != "JSON with id and name" {
		t.Fatalf("%+v", r)
	}
	if _, err := f.tm.ReplyToProject(rootCaller(bp, broot), ref, "again"); err == nil || !strings.Contains(err.Error(), "already closed") {
		t.Fatalf("a doua oara: %v", err)
	}
}

func TestAskProjectRefusals(t *testing.T) {
	f := setup(t)
	c := f.workerCaller(f.rook, 0)
	if _, err := f.tm.AskProject(c, "Backend", "?"); err == nil || !strings.Contains(err.Error(), "no linked projects") {
		t.Fatalf("fara legaturi: %v", err)
	}
	bp, broot := linked(t, f)
	if _, err := f.tm.AskProject(c, "Nope", "?"); err == nil || !strings.Contains(err.Error(), `"Nope" is not a linked project. Linked projects: Backend`) {
		t.Fatalf("nelegat: %v", err)
	}
	if _, err := f.tm.AskProject(c, "Backend", "  "); err == nil || !strings.Contains(err.Error(), "question is required") {
		t.Fatalf("goala: %v", err)
	}
	if _, err := f.tm.AskProject(c, "Backend", strings.Repeat("x", 2001)); err == nil || !strings.Contains(err.Error(), "at most 2000") {
		t.Fatalf("lunga: %v", err)
	}
	for i := 0; i < MaxOpenQuestions; i++ {
		if _, err := f.tm.AskProject(c, "Backend", "q"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.tm.AskProject(c, "Backend", "q"); err == nil || !strings.Contains(err.Error(), "you already have 3 open questions to other projects (Q-1, Q-2, Q-3); wait for the answers") {
		t.Fatalf("limita: %v", err)
	}
	// un worker din Backend nu raspunde in numele proiectului
	w := store.Agent{ProjectID: bp.ID, Name: "Ivo", Role: "Dev", Model: "haiku", ParentID: &broot.ID}
	if err := f.st.CreateAgent(&w); err != nil {
		t.Fatal(err)
	}
	if _, err := f.tm.ReplyToProject(mcp.Caller{ProjectID: bp.ID, AgentID: w.ID, HasParent: true}, "Q-1", "x"); err == nil ||
		!strings.Contains(err.Error(), "only the root agent") {
		t.Fatalf("worker: %v", err)
	}
	// o intrebare a altui proiect
	if _, err := f.tm.ReplyToProject(f.leadCaller(), "Q-1", "x"); err == nil || !strings.Contains(err.Error(), "is not a question to this project") {
		t.Fatalf("alt proiect: %v", err)
	}
	if _, err := f.tm.ReplyToProject(rootCaller(bp, broot), "W-x", "x"); err == nil || !strings.Contains(err.Error(), "invalid reference") {
		t.Fatalf("referinta: %v", err)
	}
}

func TestAskProjectNoRoot(t *testing.T) {
	f := setup(t)
	_, broot := linked(t, f)
	broot.Detached = true
	if err := f.st.UpdateAgent(broot); err != nil {
		t.Fatal(err)
	}
	if _, err := f.tm.AskProject(f.workerCaller(f.rook, 0), "Backend", "?"); err == nil || !strings.Contains(err.Error(), `"Backend" has no root agent to receive it`) {
		t.Fatalf("%v", err)
	}
}

func TestAskProjectPausedTarget(t *testing.T) {
	f := setup(t)
	bp, _ := linked(t, f)
	if err := f.st.SetPaused(bp.ID, true); err != nil {
		t.Fatal(err)
	}
	text, err := f.tm.AskProject(f.workerCaller(f.rook, 0), "Backend", "?")
	if err != nil || !strings.Contains(text, "Backend is paused: it answers when the user starts it.") {
		t.Fatalf("%q %v", text, err)
	}
}

func TestAskProjectAntiLoop(t *testing.T) {
	f := setup(t)
	bp, broot := linked(t, f)
	if _, err := f.tm.AskProject(f.workerCaller(f.rook, 0), "Backend", "Which format?"); err != nil {
		t.Fatal(err)
	}
	// radacina Backend primeste intrebarea intr-un run de note
	if err := f.tm.Reconcile(bp.ID); err != nil {
		t.Fatal(err)
	}
	run := f.fs.last(t, broot)
	c := f.caller(t, run)
	if !c.HasLinks || !c.IsRoot {
		t.Fatalf("flag-uri: %+v", c)
	}
	if _, err := f.tm.AskProject(c, "ApiUnit", "and you?"); err == nil || !strings.Contains(err.Error(), "you are answering a question from another project") {
		t.Fatalf("anti-bucla: %v", err)
	}
	f.fs.finish(run.ID, runner.Outcome{Status: "done"})
	// un run nou, real, pornit de o nota fara intrebare, poate intreba
	if err := f.tm.note(bp.ID, broot.ID, "hello", false); err != nil {
		t.Fatal(err)
	}
	if err := f.tm.Reconcile(bp.ID); err != nil {
		t.Fatal(err)
	}
	run2 := f.fs.last(t, broot)
	if run2.ID == run.ID {
		t.Fatal("nu s-a pornit un run nou")
	}
	if _, err := f.tm.AskProject(f.caller(t, run2), "ApiUnit", "ok now?"); err != nil {
		t.Fatalf("fara intrebare primita: %v", err)
	}
	f.fs.finish(run2.ID, runner.Outcome{Status: "done"})
}

func TestAskProjectAntiLoopInTaskRun(t *testing.T) {
	f := setup(t)
	bp, broot := linked(t, f)
	// radacina Backend are un task in coada: intrebarea ii ajunge in run-ul pe task
	task := store.Task{ProjectID: bp.ID, Title: "Something", AssigneeID: broot.ID, ByUser: true}
	if err := f.st.CreateTask(&task); err != nil {
		t.Fatal(err)
	}
	if _, err := f.tm.AskProject(f.workerCaller(f.rook, 0), "Backend", "Which format?"); err != nil {
		t.Fatal(err)
	}
	if err := f.tm.Reconcile(bp.ID); err != nil {
		t.Fatal(err)
	}
	run := f.fs.last(t, broot)
	c := f.caller(t, run)
	if _, err := f.tm.AskProject(c, "ApiUnit", "and you?"); err == nil || !strings.Contains(err.Error(), "you are answering") {
		t.Fatalf("anti-bucla in run pe task: %v (task run %d)", err, c.TaskID)
	}
	f.fs.finish(run.ID, runner.Outcome{Status: "done"})
}

func TestRequestWorkNeedsApproval(t *testing.T) {
	f := setup(t)
	bp, broot := linked(t, f)
	autonomous(t, f) // Full autonomy nu face cererea directa
	text, err := f.tm.RequestProjectWork(f.leadCaller(), "Backend", "Add /export", "CSV with id and name")
	if err != nil || !strings.Contains(text, "sent to Backend; the user approves it first") {
		t.Fatalf("%q %v", text, err)
	}
	reqs, _ := f.st.ListPendingWork(bp.ID)
	if len(reqs) != 1 || reqs[0].Title != "Add /export" || reqs[0].Status != store.ReqPending {
		t.Fatalf("%+v", reqs)
	}
	if tasks, _ := f.st.ListTasks(bp.ID); len(tasks) != 0 {
		t.Fatal("fara aprobare nu exista task")
	}
	if err := f.tm.ApproveProjectRequest(reqs[0].ID); err != nil {
		t.Fatal(err)
	}
	tasks, _ := f.st.ListTasks(bp.ID)
	if len(tasks) != 1 {
		t.Fatalf("un task: %+v", tasks)
	}
	task := tasks[0]
	if task.AssigneeID != broot.ID || task.RequestID == nil || *task.RequestID != reqs[0].ID || task.Title != "Add /export" ||
		!strings.HasSuffix(task.Description, "Requested by ApiUnit (Lead) as "+reqs[0].Ref()+".") {
		t.Fatalf("task: %+v", task)
	}
	if !hasEvent(t, f, task.ID, "created", "ApiUnit", "Requested by ApiUnit (Lead)") {
		t.Fatal("pasul created")
	}
	r, _ := f.st.GetProjectRequest(reqs[0].ID)
	if r.Status != store.ReqApproved || r.ToTask == nil || *r.ToTask != task.ID {
		t.Fatalf("%+v", r)
	}
	if !f.hasMessage(f.lead.ID, "Backend accepted "+r.Ref()+" (task "+task.Ref()+" there).") {
		t.Fatal("nota la lider")
	}
	// radacina Backend afla cine a cerut task-ul (nu "assigned by the user")
	if !f.hasMessage(broot.ID, task.Ref()+" requested by ApiUnit (Lead): Add /export. When it is done, call report_done on it.") ||
		f.hasMessage(broot.ID, "assigned by the user") {
		t.Fatal("nota la radacina tinta")
	}
	if !hasEvent(t, f, task.ID, "assigned", "ApiUnit", "Assigned to Lead") {
		t.Fatal("pasul assigned")
	}
	// dublu-click
	if err := f.tm.ApproveProjectRequest(reqs[0].ID); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("a doua aprobare: %v", err)
	}
}

func TestRequestWorkDirectFromChat(t *testing.T) {
	f := setup(t)
	bp, _ := linked(t, f)
	if _, err := f.tm.UserMessage(f.lead.ID, "ask Backend to add /export", nil); err != nil {
		t.Fatal(err)
	}
	mustReconcile(t, f)
	run := f.fs.last(t, f.lead)
	text, err := f.tm.RequestProjectWork(f.caller(t, run), "Backend", "Add /export", "")
	if err != nil || !strings.Contains(text, "Backend got your request as W-") || !strings.Contains(text, "(task T-1)") {
		t.Fatalf("%q %v", text, err)
	}
	if tasks, _ := f.st.ListTasks(bp.ID); len(tasks) != 1 {
		t.Fatalf("task direct: %+v", tasks)
	}
	f.fs.finish(run.ID, runner.Outcome{Status: "done"})
}

func TestRequestWorkNotDirectAfterQuestionReply(t *testing.T) {
	f := setup(t)
	bp, _ := linked(t, f)
	if _, err := f.tm.AskUser(f.leadCaller(), "Which DB?", nil); err != nil {
		t.Fatal(err)
	}
	qs, _ := f.st.ListOpenQuestions(f.p.ID)
	if err := f.tm.DismissQuestion(qs[0].ID); err != nil {
		t.Fatal(err)
	}
	mustReconcile(t, f)
	run := f.fs.last(t, f.lead)
	text, err := f.tm.RequestProjectWork(f.caller(t, run), "Backend", "Add /export", "")
	if err != nil || !strings.Contains(text, "the user approves it first") {
		t.Fatalf("%q %v", text, err)
	}
	if tasks, _ := f.st.ListTasks(bp.ID); len(tasks) != 0 {
		t.Fatal("un raspuns la ask_user nu e cerere din chat")
	}
	f.fs.finish(run.ID, runner.Outcome{Status: "done"})
}

func TestRequestWorkRefusals(t *testing.T) {
	f := setup(t)
	linked(t, f)
	if _, err := f.tm.RequestProjectWork(f.workerCaller(f.rook, 0), "Backend", "x", ""); err == nil || !strings.Contains(err.Error(), "only a lead") {
		t.Fatalf("worker: %v", err)
	}
	if _, err := f.tm.RequestProjectWork(f.leadCaller(), "Backend", " ", ""); err == nil || !strings.Contains(err.Error(), "title is required") {
		t.Fatalf("titlu: %v", err)
	}
	if _, err := f.tm.RequestProjectWork(f.leadCaller(), "Backend", strings.Repeat("x", 201), ""); err == nil || !strings.Contains(err.Error(), "at most 200") {
		t.Fatalf("titlu lung: %v", err)
	}
	if _, err := f.tm.RequestProjectWork(f.leadCaller(), "Nope", "x", ""); err == nil || !strings.Contains(err.Error(), "is not a linked project") {
		t.Fatalf("nelegat: %v", err)
	}
}

// approvedTask face o cerere, o aproba si intoarce task-ul din Backend (pus In progress, ca intr-un run pe el).
func approvedTask(t *testing.T, f *fixture, c mcp.Caller) (store.Project, store.Agent, store.Task, store.ProjectRequest) {
	t.Helper()
	bp, broot := linked(t, f)
	if _, err := f.tm.RequestProjectWork(c, "Backend", "Add /export", "CSV"); err != nil {
		t.Fatal(err)
	}
	reqs, _ := f.st.ListPendingWork(bp.ID)
	if err := f.tm.ApproveProjectRequest(reqs[0].ID); err != nil {
		t.Fatal(err)
	}
	tasks, _ := f.st.ListTasks(bp.ID)
	task := tasks[0]
	task.Status = store.StatusInProgress
	if err := f.st.SaveTask(&task); err != nil {
		t.Fatal(err)
	}
	r, _ := f.st.GetProjectRequest(reqs[0].ID)
	return bp, broot, task, r
}

func TestRootReportDoneDelivers(t *testing.T) {
	f := setup(t)
	lt := f.task(t, "Export page", f.lead, store.StatusInProgress)
	c := f.leadCaller()
	c.TaskID = lt.ID
	bp, broot, task, r := approvedTask(t, f, c)
	rc := mcp.Caller{ProjectID: bp.ID, AgentID: broot.ID, TaskID: task.ID, IsRoot: true, FromRequest: true}
	text, err := f.tm.ReportDone(rc, "", "Endpoint /export added")
	if err != nil || !strings.Contains(text, task.Ref()+" is Done; the report went back to ApiUnit.") {
		t.Fatalf("%q %v", text, err)
	}
	got, _ := f.st.GetTask(task.ID)
	if got.Status != store.StatusDone || got.Result != "Endpoint /export added" {
		t.Fatalf("%+v", got)
	}
	r, _ = f.st.GetProjectRequest(r.ID)
	if r.Status != store.ReqDone || r.Answer != "Endpoint /export added" {
		t.Fatalf("%+v", r)
	}
	want := "Backend finished " + r.Ref() + ": Endpoint /export added (your task " + lt.Ref() + ")"
	if !f.hasMessage(f.lead.ID, want) {
		t.Fatalf("nota la lider: %q", want)
	}
}

func TestRootReportDoneWaitsForSubtasks(t *testing.T) {
	f := setup(t)
	bp, broot, task, _ := approvedTask(t, f, f.leadCaller())
	sub := store.Task{ProjectID: bp.ID, Title: "part", AssigneeID: broot.ID, ParentTaskID: &task.ID}
	if err := f.st.CreateTask(&sub); err != nil {
		t.Fatal(err)
	}
	rc := mcp.Caller{ProjectID: bp.ID, AgentID: broot.ID, TaskID: task.ID, IsRoot: true, FromRequest: true}
	if _, err := f.tm.ReportDone(rc, "", "x"); err == nil || !strings.Contains(err.Error(), "still has open subtasks") {
		t.Fatalf("%v", err)
	}
}

func TestUserAcceptDeliversToo(t *testing.T) {
	f := setup(t)
	_, _, task, r := approvedTask(t, f, f.leadCaller())
	// radacina a terminat fara report_done: task-ul e in Review la utilizator, care il accepta
	task.Status, task.ReviewStage, task.Result = store.StatusReview, store.StageLead, "Done without report_done"
	if err := f.st.SaveTask(&task); err != nil {
		t.Fatal(err)
	}
	if err := f.tm.UserDecide(task.ID, true, "", nil); err != nil {
		t.Fatalf("accept: %v", err)
	}
	r, _ = f.st.GetProjectRequest(r.ID)
	if r.Status != store.ReqDone || !f.hasMessage(f.lead.ID, "Backend finished "+r.Ref()+": Done without report_done") {
		t.Fatalf("livrare la accept: %+v", r)
	}
}

func TestRequestWorkStagedTarget(t *testing.T) {
	f := setup(t)
	bp, broot := linked(t, f)
	if _, err := f.st.SaveStages(bp.ID, []store.Stage{{Name: "Build", Kind: "work", AgentIDs: []int64{broot.ID}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.tm.RequestProjectWork(f.leadCaller(), "Backend", "Add /export", ""); err != nil {
		t.Fatal(err)
	}
	reqs, _ := f.st.ListPendingWork(bp.ID)
	if err := f.tm.ApproveProjectRequest(reqs[0].ID); err != nil {
		t.Fatal(err)
	}
	tasks, _ := f.st.ListTasks(bp.ID)
	if len(tasks) != 1 || tasks[0].StageID == nil || tasks[0].RequestID == nil {
		t.Fatalf("task in prima etapa: %+v", tasks)
	}
}

func TestDismissProjectRequest(t *testing.T) {
	f := setup(t)
	bp, _ := linked(t, f)
	f.tm.RequestProjectWork(f.leadCaller(), "Backend", "Add /export", "")
	reqs, _ := f.st.ListPendingWork(bp.ID)
	if err := f.tm.DismissProjectRequest(reqs[0].ID); err != nil {
		t.Fatal(err)
	}
	if !f.hasMessage(f.lead.ID, "The user dismissed your request "+reqs[0].Ref()+" to Backend.") {
		t.Fatal("nota la lider")
	}
	if err := f.tm.DismissProjectRequest(reqs[0].ID); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("a doua oara: %v", err)
	}
}

func TestApproveAfterUnlink(t *testing.T) {
	f := setup(t)
	bp, _ := linked(t, f)
	f.tm.RequestProjectWork(f.leadCaller(), "Backend", "Add /export", "")
	reqs, _ := f.st.ListPendingWork(bp.ID)
	// legatura stearsa direct din store (fara ciclul de viata): aprobarea trebuie refuzata
	if err := f.st.UnlinkProjects(f.p.ID, bp.ID); err != nil {
		t.Fatal(err)
	}
	var pre ProjectRequestError
	if err := f.tm.ApproveProjectRequest(reqs[0].ID); !errors.As(err, &pre) {
		t.Fatalf("%v", err)
	}
}

func TestUnlinkClosesOpenRequests(t *testing.T) {
	f := setup(t)
	bp, _ := linked(t, f)
	f.tm.AskProject(f.workerCaller(f.rook, 0), "Backend", "Which format?")
	f.tm.RequestProjectWork(f.leadCaller(), "Backend", "Add /export", "")
	if err := f.tm.UnlinkProjects(bp.ID, f.p.ID); err != nil {
		t.Fatal(err)
	}
	if ok, _ := f.st.AreLinked(f.p.ID, bp.ID); ok {
		t.Fatal("legatura trebuia stearsa")
	}
	if !f.hasMessage(f.rook.ID, "to Backend was closed: the projects are no longer linked.") ||
		!f.hasMessage(f.lead.ID, "to Backend was closed: the projects are no longer linked.") {
		t.Fatal("notele")
	}
	if n, _ := f.st.CountPendingWork(bp.ID); n != 0 {
		t.Fatal("cererea neaprobata trebuia respinsa")
	}
	if err := f.tm.UnlinkProjects(bp.ID, f.p.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("a doua oara: %v", err)
	}
}

func TestBeforeDeleteProjectNotifies(t *testing.T) {
	f := setup(t)
	bp, _ := linked(t, f)
	f.tm.AskProject(f.workerCaller(f.rook, 0), "Backend", "Which format?")
	if err := f.tm.BeforeDeleteProject(bp.ID); err != nil {
		t.Fatal(err)
	}
	if !f.hasMessage(f.rook.ID, "to Backend was closed: the project was deleted.") {
		t.Fatal("nota")
	}
}

func TestUnlinkKeepsApprovedRequests(t *testing.T) {
	f := setup(t)
	lt := f.task(t, "Export page", f.lead, store.StatusInProgress)
	c := f.leadCaller()
	c.TaskID = lt.ID
	bp, broot, task, r := approvedTask(t, f, c)
	if err := f.tm.UnlinkProjects(f.p.ID, bp.ID); err != nil {
		t.Fatal(err)
	}
	if r, _ = f.st.GetProjectRequest(r.ID); r.Status != store.ReqApproved {
		t.Fatalf("cererea aprobata trebuia sa ramana: %+v", r)
	}
	rc := mcp.Caller{ProjectID: bp.ID, AgentID: broot.ID, TaskID: task.ID, IsRoot: true, FromRequest: true}
	if _, err := f.tm.ReportDone(rc, "", "Endpoint /export added"); err != nil {
		t.Fatal(err)
	}
	r, _ = f.st.GetProjectRequest(r.ID)
	if r.Status != store.ReqDone || !f.hasMessage(f.lead.ID, "Backend finished "+r.Ref()+": Endpoint /export added") {
		t.Fatalf("livrare dupa unlink: %+v", r)
	}
}

// questionRun: Rook intreaba Backend, iar radacina Backend primeste intrebarea intr-un run de note.
func questionRun(t *testing.T, f *fixture) (store.Project, store.Agent, runner.Spec, store.ProjectRequest) {
	t.Helper()
	bp, broot := linked(t, f)
	if _, err := f.tm.AskProject(f.workerCaller(f.rook, 0), "Backend", "Which format?"); err != nil {
		t.Fatal(err)
	}
	note := lastMessage(t, f, broot.ID)
	if err := f.tm.Reconcile(bp.ID); err != nil {
		t.Fatal(err)
	}
	r, _ := f.st.GetProjectRequest(*note.RequestID)
	return bp, broot, f.fs.last(t, broot), r
}

func TestQuestionAnsweredByRunReply(t *testing.T) {
	f := setup(t)
	_, _, run, r := questionRun(t, f)
	// radacina raspunde in textul run-ului, fara reply_to_project
	f.fs.finish(run.ID, runner.Outcome{Status: "done", Result: "  JSON  "})
	r, _ = f.st.GetProjectRequest(r.ID)
	if r.Status != store.ReqAnswered || r.Answer != "JSON" {
		t.Fatalf("%+v", r)
	}
	ans := lastMessage(t, f, f.rook.ID)
	if ans.Delivered || ans.Text != "Answer from Backend to "+r.Ref()+": JSON" {
		t.Fatalf("raspunsul: %+v", ans)
	}
}

func TestQuestionClosedWhenRunFailsSilently(t *testing.T) {
	f := setup(t)
	_, _, run, r := questionRun(t, f)
	f.fs.finish(run.ID, runner.Outcome{Status: store.RunFailed, Error: "boom"})
	r, _ = f.st.GetProjectRequest(r.ID)
	if r.Status != store.ReqClosed {
		t.Fatalf("%+v", r)
	}
	if !f.hasMessage(f.rook.ID, r.Ref()+" to Backend was closed: its root did not answer.") {
		t.Fatal("nota la cel care a intrebat")
	}
	// limita nu mai blocheaza: intrebarea inchisa nu se mai numara
	if n, _ := f.st.OpenQuestionsFrom(f.rook.ID); len(n) != 0 {
		t.Fatalf("deschise: %+v", n)
	}
}

func TestQuestionAnsweredByToolNotTouchedAtRunEnd(t *testing.T) {
	f := setup(t)
	_, _, run, r := questionRun(t, f)
	if _, err := f.tm.ReplyToProject(f.caller(t, run), r.Ref(), "XML"); err != nil {
		t.Fatal(err)
	}
	f.fs.finish(run.ID, runner.Outcome{Status: "done", Result: "Answered Q with XML"})
	r, _ = f.st.GetProjectRequest(r.ID)
	if r.Status != store.ReqAnswered || r.Answer != "XML" {
		t.Fatalf("%+v", r)
	}
	if f.hasMessage(f.rook.ID, "Answered Q with XML") {
		t.Fatal("raspunsul din tool nu trebuia dublat")
	}
}

func TestReplyToProjectMaxLength(t *testing.T) {
	f := setup(t)
	bp, broot, run, r := questionRun(t, f)
	if _, err := f.tm.ReplyToProject(rootCaller(bp, broot), r.Ref(), strings.Repeat("x", 3801)); err == nil ||
		err.Error() != "the answer is too long: at most 3800 characters" {
		t.Fatalf("%v", err)
	}
	if _, err := f.tm.ReplyToProject(rootCaller(bp, broot), r.Ref(), strings.Repeat("x", 3800)); err != nil {
		t.Fatalf("3800: %v", err)
	}
	if strings.Contains(lastMessage(t, f, f.rook.ID).Text, "[...]") {
		t.Fatal("raspunsul de 3800 nu trebuia taiat")
	}
	f.fs.finish(run.ID, runner.Outcome{Status: "done"})
}

func TestRootReportDoneGoesThroughReviewer(t *testing.T) {
	f := setup(t)
	bp, broot, task, r := approvedTask(t, f, f.leadCaller())
	rev := store.Agent{ProjectID: bp.ID, Name: "Rhea", Role: "Reviewer", Model: "haiku", ParentID: &broot.ID}
	if err := f.st.CreateAgent(&rev); err != nil {
		t.Fatal(err)
	}
	broot.ReviewerID = &rev.ID
	if err := f.st.UpdateAgent(broot); err != nil {
		t.Fatal(err)
	}
	rc := mcp.Caller{ProjectID: bp.ID, AgentID: broot.ID, TaskID: task.ID, IsRoot: true, FromRequest: true}
	text, err := f.tm.ReportDone(rc, "", "Endpoint /export added")
	want := task.Ref() + " is in Review. Rhea will check it; the report goes back to ApiUnit when it is accepted."
	if err != nil || text != want {
		t.Fatalf("%q %v", text, err)
	}
	got, _ := f.st.GetTask(task.ID)
	if got.Status != store.StatusReview || got.ReviewStage != store.StageReviewer || got.Result != "Endpoint /export added" {
		t.Fatalf("%+v", got)
	}
	if r, _ = f.st.GetProjectRequest(r.ID); r.Status != store.ReqApproved {
		t.Fatalf("cererea inca in lucru: %+v", r)
	}
	if !f.hasMessage(rev.ID, "Check it with review_approve or review_reject: Endpoint /export added") {
		t.Fatal("nota la reviewer")
	}
	// reviewer-ul aproba: task-ul utilizatorului (creat din cerere) merge la decizia lui
	rvc := mcp.Caller{ProjectID: bp.ID, AgentID: rev.ID, HasParent: true, IsReviewer: true}
	if text, err := f.tm.ReviewApprove(rvc, task.Ref(), ""); err != nil || !strings.Contains(text, "went to the user for a decision") {
		t.Fatalf("%q %v", text, err)
	}
	if r, _ = f.st.GetProjectRequest(r.ID); r.Status != store.ReqApproved {
		t.Fatalf("inca nelivrata: %+v", r)
	}
	if err := f.tm.UserDecide(task.ID, true, "", nil); err != nil {
		t.Fatalf("accept: %v", err)
	}
	r, _ = f.st.GetProjectRequest(r.ID)
	if r.Status != store.ReqDone || !f.hasMessage(f.lead.ID, "Backend finished "+r.Ref()+": Endpoint /export added") {
		t.Fatalf("livrare dupa review: %+v", r)
	}
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }
