package team

import (
	"fmt"
	"strings"
	"testing"

	"hobstack/internal/prompt"
	"hobstack/internal/runner"
	"hobstack/internal/store"
)

func TestSubLeadTurnShowsDirectTeamAndSubtasks(t *testing.T) {
	f := setup(t)
	sub := f.agent(t, "Sub", &f.lead, nil)
	wes := f.agent(t, "Wes", &sub, nil)
	parent := f.task(t, "Import", sub, store.StatusQueued)
	child := store.Task{ProjectID: f.p.ID, Title: "Lexer", AssigneeID: wes.ID, CreatorID: &sub.ID, ParentTaskID: &parent.ID}
	f.st.CreateTask(&child)
	child.Status = store.StatusDone
	f.st.SaveTask(&child)

	mustReconcile(t, f)
	spec := f.fs.last(t, sub)
	got := turn(spec)
	if !strings.HasPrefix(got, "Your direct team:\n- Wes") || strings.Contains(got, "Rook") {
		t.Fatalf("echipa directa gresita:\n%s", got)
	}
	if !strings.Contains(got, "Subtasks already created for this task (do not create them again):\n- T-2 [done] Lexer -> Wes") {
		t.Fatalf("lipsesc sub-task-urile:\n%s", got)
	}
	if !strings.Contains(argValue(spec.Args, "--append-system-prompt"), "You report to Lead.") {
		t.Fatal("sub-liderul trebuie sa raporteze lui Lead")
	}
}

func TestGraphFullFlow(t *testing.T) {
	f := setup(t)
	sub := f.agent(t, "Sub", &f.lead, nil)
	rev := f.agent(t, "Rev", &f.lead, nil)
	wes := f.agent(t, "Wes", &sub, &rev)
	ok := func(sid string) runner.Outcome { return runner.Outcome{Status: "done", SessionID: sid, Result: "ok"} }

	// 1. utilizator -> coordonator -> T-1 pentru sub-lider
	f.tm.UserMessage(f.lead.ID, "Fa importul", nil)
	mustReconcile(t, f)
	lead1 := f.fs.last(t, f.lead)
	if _, err := f.tm.CreateTask(f.caller(t, lead1), "Import", "", "Sub", ""); err != nil {
		t.Fatal(err)
	}
	f.fs.finish(lead1.ID, ok("s-lead"))

	// 2. sub-liderul sparge T-1 in T-2 pentru Wes si se opreste fara report_done
	mustReconcile(t, f)
	sub1 := f.fs.last(t, sub)
	sc := f.caller(t, sub1)
	if !sc.IsLead || !sc.HasParent || sc.IsReviewer || !strings.Contains(turn(sub1), "Task T-1: Import") {
		t.Fatalf("sub1: %+v\n%s", sc, turn(sub1))
	}
	if _, err := f.tm.CreateTask(sc, "Parser", "", "Wes", ""); err != nil {
		t.Fatal(err)
	}
	f.fs.finish(sub1.ID, runner.Outcome{Status: "done", SessionID: "s-sub", Result: "Am delegat."})
	f.wantStatus(t, 1, store.StatusInProgress) // asteapta T-2, nu trece in review
	if !f.hasMessage(sub.ID, "T-1 is waiting for its subtasks: T-2.") {
		t.Fatal("lipseste nota de asteptare")
	}

	// 3. Wes raporteaza: T-2 merge la Rev
	mustReconcile(t, f)
	wes1 := f.fs.last(t, wes)
	if _, err := f.tm.ReportDone(f.caller(t, wes1), "", "Parser gata"); err != nil {
		t.Fatal(err)
	}
	f.fs.finish(wes1.ID, ok("s-wes"))

	// 4. Rev e trezit cu run de notificari si aproba
	mustReconcile(t, f)
	rev1 := f.fs.last(t, rev)
	rc := f.caller(t, rev1)
	if !rc.IsReviewer || rc.IsLead || !strings.Contains(turn(rev1), "You review the work of: Wes") ||
		!strings.Contains(turn(rev1), "Wes finished T-2. Check it") {
		t.Fatalf("rev1: %+v\n%s", rc, turn(rev1))
	}
	if _, err := f.tm.ReviewApprove(rc, "T-2", ""); err != nil {
		t.Fatal(err)
	}
	f.fs.finish(rev1.ID, ok("s-rev"))

	// 5. Sub accepta T-2; T-1 revine in coada si Sub primeste un run pe el
	mustReconcile(t, f)
	sub2 := f.fs.last(t, sub)
	if sub2.ID == sub1.ID || !strings.Contains(turn(sub2), "Rev approved T-2, done by Wes") {
		t.Fatalf("sub2:\n%s", turn(sub2))
	}
	if _, err := f.tm.AcceptTask(f.caller(t, sub2), "T-2", ""); err != nil {
		t.Fatal(err)
	}
	f.wantStatus(t, 1, store.StatusQueued)
	f.fs.finish(sub2.ID, ok("s-sub"))
	mustReconcile(t, f)
	sub3 := f.fs.last(t, sub)
	got := turn(sub3)
	if !strings.Contains(got, "Task T-1: Import") || !strings.Contains(got, "- T-2 [done] Parser -> Wes") {
		t.Fatalf("sub3:\n%s", got)
	}
	if _, err := f.tm.ReportDone(f.caller(t, sub3), "", "Import gata"); err != nil {
		t.Fatal(err)
	}
	f.fs.finish(sub3.ID, ok("s-sub"))

	// 6. coordonatorul accepta T-1
	mustReconcile(t, f)
	lead2 := f.fs.last(t, f.lead)
	if !strings.Contains(turn(lead2), "Sub finished T-1 (review): Import gata") {
		t.Fatalf("lead2:\n%s", turn(lead2))
	}
	if _, err := f.tm.AcceptTask(f.caller(t, lead2), "T-1", ""); err != nil {
		t.Fatal(err)
	}
	f.wantStatus(t, 1, store.StatusDone)
	f.wantStatus(t, 2, store.StatusDone)
}

func TestCreateTaskRefusedOutsideDirectTeamInRun(t *testing.T) {
	f := setup(t)
	sub := f.agent(t, "Sub", &f.lead, nil)
	f.agent(t, "Wes", &sub, nil)
	f.tm.UserMessage(f.lead.ID, "Fa importul", nil)
	mustReconcile(t, f)
	if _, err := f.tm.CreateTask(f.caller(t, f.fs.last(t, f.lead)), "A", "", "Wes", ""); err == nil {
		t.Fatal("coordonatorul nu poate da task direct lui Wes")
	}
}

func TestStopKeepsWaitingParentTask(t *testing.T) {
	f := setup(t)
	sub := f.agent(t, "Sub", &f.lead, nil)
	wes := f.agent(t, "Wes", &sub, nil)
	parent := f.task(t, "Import", sub, store.StatusInProgress)
	child := store.Task{ProjectID: f.p.ID, Title: "Parser", AssigneeID: wes.ID, CreatorID: &sub.ID, ParentTaskID: &parent.ID}
	f.st.CreateTask(&child)
	mustReconcile(t, f) // Wes porneste pe T-2
	if err := f.tm.Stop(f.p.ID); err != nil {
		t.Fatal(err)
	}
	f.wantStatus(t, 1, store.StatusInProgress)
	f.wantStatus(t, 2, store.StatusQueued)
	f.fs.finish(f.fs.last(t, wes).ID, runner.Outcome{Status: "stopped"})
	f.tm.Resume(f.p.ID)
	mustReconcile(t, f)
	for _, s := range f.fs.specs {
		if s.Key == fmt.Sprintf("agent-%d", sub.ID) {
			t.Fatal("sub-liderul nu trebuie repornit pe T-1 cat timp T-2 e deschis")
		}
	}
	if err := f.tm.Recover(); err != nil {
		t.Fatal(err)
	}
	f.wantStatus(t, 1, store.StatusInProgress)
}

func TestRootsIgnoreMaxParallel(t *testing.T) {
	f := setup(t)
	f.p.MaxParallel = 1
	f.st.UpdateProject(f.p)
	solo := f.agent(t, "Solo", nil, nil)
	f.tm.UserMessage(f.lead.ID, "salut", nil)
	f.tm.UserMessage(solo.ID, "salut", nil)
	f.task(t, "A", f.rook, store.StatusQueued)
	f.task(t, "B", f.vera, store.StatusQueued)
	mustReconcile(t, f)
	if f.fs.count() != 3 {
		t.Fatalf("run-uri: %d, vreau 3 (doua radacini + un worker)", f.fs.count())
	}
	f.wantStatus(t, 2, store.StatusQueued)
	c := f.caller(t, f.fs.last(t, solo))
	if c.IsLead || c.HasParent || c.IsReviewer {
		t.Fatalf("radacina singura: %+v", c)
	}
	if got := turn(f.fs.last(t, solo)); got != "Notifications:\n- User: salut\n\n"+prompt.ReadOnlyTurn {
		t.Fatalf("tura Solo: %q", got)
	}
}

func TestUserMessageCopyGoesToParent(t *testing.T) {
	f := setup(t)
	sub := f.agent(t, "Sub", &f.lead, nil)
	wes := f.agent(t, "Wes", &sub, nil)
	f.tm.UserMessage(wes.ID, "ce faci?", nil)
	if !f.hasMessage(sub.ID, "User -> Wes: ce faci?") || f.hasMessage(f.lead.ID, "User -> Wes") {
		t.Fatal("copia trebuie sa ajunga doar la seful direct")
	}
	f.tm.UserMessage(f.lead.ID, "salut", nil)
	if msgs, _ := f.st.ListMessages(f.lead.ID); len(msgs) != 1 {
		t.Fatalf("o radacina nu primeste copie: %+v", msgs)
	}
}

func TestAcceptBlockedSubtaskNotifiesParent(t *testing.T) {
	f := setup(t)
	sub := f.agent(t, "Sub", &f.lead, nil)
	wes := f.agent(t, "Wes", &sub, nil)
	parent := f.task(t, "Import", sub, store.StatusInProgress)
	child := store.Task{ProjectID: f.p.ID, Title: "Parser", AssigneeID: wes.ID, CreatorID: &sub.ID, ParentTaskID: &parent.ID}
	f.st.CreateTask(&child)
	child.Status = store.StatusBlocked
	f.st.SaveTask(&child)
	if err := f.tm.AcceptBlocked(child.ID); err != nil {
		t.Fatal(err)
	}
	if !f.hasMessage(sub.ID, "The user accepted T-2.") || !f.hasMessage(sub.ID, "All subtasks for T-1 are done") {
		t.Fatal("sub-liderul (creator si asignatul parintelui) trebuie anuntat")
	}
	f.wantStatus(t, 1, store.StatusQueued)
}

func TestResumeWakesReviewer(t *testing.T) {
	f := setup(t)
	f.rook.ReviewerID = &f.vera.ID
	f.st.UpdateAgent(f.rook)
	task := f.task(t, "A", f.rook, store.StatusReview)
	task.ReviewStage = store.StageReviewer
	f.st.SaveTask(&task)
	f.tm.Stop(f.p.ID)
	f.tm.Resume(f.p.ID)
	pending, _ := f.st.Undelivered(f.vera.ID)
	if len(pending) != 1 || pending[0].Text != "The team was restarted. Waiting for your decision: T-1." {
		t.Fatalf("Vera: %+v", pending)
	}
	if p, _ := f.st.Undelivered(f.lead.ID); len(p) != 0 {
		t.Fatalf("lead-ul nu are nimic de decis: %+v", p)
	}
}

func TestTurnContext(t *testing.T) {
	f := setup(t)
	sub := f.agent(t, "Sub", &f.lead, nil)
	wes := f.agent(t, "Wes", &sub, nil)
	rev := f.agent(t, "Rev", &f.lead, nil)
	wes.ReviewerID = &rev.ID
	f.st.UpdateAgent(wes)

	cur := f.task(t, "Curent", sub, store.StatusInProgress)
	other := f.task(t, "Altul", sub, store.StatusInProgress)
	f.task(t, "Coada", sub, store.StatusQueued) // nu e in lucru: nu apare
	for _, st := range []string{store.StatusDone, store.StatusQueued} {
		c := store.Task{ProjectID: f.p.ID, Title: "Sub", AssigneeID: wes.ID, CreatorID: &sub.ID, ParentTaskID: &other.ID}
		f.st.CreateTask(&c)
		c.Status = st
		f.st.SaveTask(&c)
	}
	agents, _ := f.st.ListAgents(f.p.ID)
	tasks, _ := f.st.ListTasks(f.p.ID)

	got := f.tm.turnContext(sub, agents, tasks, cur.ID, nil)
	if len(got.Direct) != 1 || got.Direct[0].Name != "Wes" {
		t.Fatalf("Direct: %+v", got.Direct)
	}
	if len(got.OpenTasks) != 1 || got.OpenTasks[0].Ref != other.Ref() || got.OpenTasks[0].Done != 1 || got.OpenTasks[0].Total != 2 {
		t.Fatalf("OpenTasks: %+v", got.OpenTasks)
	}
	if r := f.tm.turnContext(rev, agents, tasks, 0, nil).Reviewees; len(r) != 1 || r[0] != "Wes" {
		t.Fatalf("Reviewees: %v", r)
	}
}

func TestRootAssigneeWithoutDeciderGetsUserNote(t *testing.T) {
	f := setup(t)
	solo := f.agent(t, "Solo", nil, nil)
	task := f.task(t, "A", solo, store.StatusInProgress)
	agents, _ := f.st.ListAgents(f.p.ID)
	task.CreatorID = nil
	task.Result = "gata"
	if _, err := f.tm.toReview(&task, solo, agents, "Solo a terminat", taskNote{}, true); err != nil {
		t.Fatal(err)
	}
	// task-ul propriu al radacinii trece singur in Done, fara nota "decide utilizatorul"
	f.wantStatus(t, task.Number, store.StatusDone)
	if f.hasMessage(solo.ID, "The user decides") || f.hasMessage(solo.ID, "Link it") {
		t.Fatal("radacina nu trebuia trimisa la board")
	}
	// un agent cu sef sters ramane cu sfatul vechi
	orphan := f.agent(t, "Orph", nil, nil)
	orphan.ParentID = nil
	t2 := f.task(t, "B", orphan, store.StatusInProgress)
	t2.CreatorID = nil
	orphan.ParentID = &f.lead.ID
	if _, err := f.tm.toReview(&t2, orphan, nil, "x", taskNote{}, true); err != nil {
		t.Fatal(err)
	}
	if !f.hasMessage(orphan.ID, "Link it to a lead on the canvas") {
		t.Fatal("non-radacina fara decident primeste sfatul cu liderul")
	}
}

// atReviewer pune T-1 (al lui Rook) in etapa reviewer, cu Vera ca reviewer,
// si marcheaza notificarea Vera ca livrata (ca dupa run-ul ei).
func atReviewer(t *testing.T, f *fixture) store.Task {
	t.Helper()
	f.rook.ReviewerID = &f.vera.ID
	f.st.UpdateAgent(f.rook)
	task := f.task(t, "A", f.rook, store.StatusInProgress)
	if _, err := f.tm.ReportDone(f.workerCaller(f.rook, task.ID), "", "A e gata"); err != nil {
		t.Fatal(err)
	}
	pending, _ := f.st.Undelivered(f.vera.ID)
	for _, m := range pending {
		f.st.MarkDelivered([]int64{m.ID})
	}
	return task
}

func TestLeadDecidesWhenReviewerLinkGone(t *testing.T) {
	f := setup(t)
	atReviewer(t, f)
	f.rook.ReviewerID = nil
	f.st.UpdateAgent(f.rook)
	// fara reviewer, etapa "reviewer" nu mai blocheaza decizia liderului
	if _, err := f.tm.AcceptTask(f.leadCaller(), "T-1", ""); err != nil {
		t.Fatal(err)
	}
	f.wantStatus(t, 1, store.StatusDone)
}

func TestResumeWakesDeciderWhenReviewerGone(t *testing.T) {
	f := setup(t)
	atReviewer(t, f)
	f.rook.ReviewerID = nil
	f.st.UpdateAgent(f.rook)
	f.tm.Stop(f.p.ID)
	f.tm.Resume(f.p.ID)
	pending, _ := f.st.Undelivered(f.lead.ID)
	if len(pending) != 1 || pending[0].Text != "The team was restarted. Waiting for your decision: T-1." {
		t.Fatalf("Lead: %+v", pending)
	}
}

func TestLinksChangedMovesOrphanReviewToLead(t *testing.T) {
	f := setup(t)
	atReviewer(t, f)
	f.rook.ReviewerID = nil
	f.st.UpdateAgent(f.rook)
	if err := f.tm.LinksChanged(f.p.ID); err != nil {
		t.Fatal(err)
	}
	if got := f.wantStatus(t, 1, store.StatusReview); got.ReviewStage != store.StageLead {
		t.Fatalf("stage = %q", got.ReviewStage)
	}
	pending, _ := f.st.Undelivered(f.lead.ID)
	if len(pending) != 1 || !strings.Contains(pending[0].Text, "Rook finished T-1") || !strings.Contains(pending[0].Text, "A e gata") {
		t.Fatalf("Lead: %+v", pending)
	}
	if p, _ := f.st.Undelivered(f.vera.ID); len(p) != 0 {
		t.Fatalf("Vera nu mai verifica: %+v", p)
	}
	// a doua apelare nu mai schimba nimic
	f.tm.LinksChanged(f.p.ID)
	if p, _ := f.st.Undelivered(f.lead.ID); len(p) != 1 {
		t.Fatalf("nota dubla: %+v", p)
	}
}

// reviewer-ul radacinii a disparut: task-ul ei propriu trece singur in Done,
// cel din board ramane la utilizator (blocat)
func TestLinksChangedRootWithoutDecider(t *testing.T) {
	f := setup(t)
	solo := f.agent(t, "Solo", nil, &f.vera)
	task := store.Task{ProjectID: f.p.ID, Title: "A", AssigneeID: solo.ID} // fara creator
	f.st.CreateTask(&task)
	task.Status, task.ReviewStage = store.StatusReview, store.StageReviewer
	f.st.SaveTask(&task)
	solo.ReviewerID = nil
	f.st.UpdateAgent(solo)
	if err := f.tm.LinksChanged(f.p.ID); err != nil {
		t.Fatal(err)
	}
	f.wantStatus(t, 1, store.StatusDone)
	if got := lastEventText(t, f, task.ID); got != ownRootAcceptText {
		t.Fatalf("%q", got)
	}

	solo.ReviewerID = &f.vera.ID
	f.st.UpdateAgent(solo)
	mine := store.Task{ProjectID: f.p.ID, Title: "B", AssigneeID: solo.ID, ByUser: true}
	f.st.CreateTask(&mine)
	mine.Status, mine.ReviewStage = store.StatusReview, store.StageReviewer
	f.st.SaveTask(&mine)
	solo.ReviewerID = nil
	f.st.UpdateAgent(solo)
	if err := f.tm.LinksChanged(f.p.ID); err != nil {
		t.Fatal(err)
	}
	f.wantStatus(t, 2, store.StatusBlocked)
	if !f.hasMessage(solo.ID, "The user decides from the board") {
		t.Fatal("radacina trebuie trimisa la board")
	}
}

func TestLinksChangedNotifiesNewReviewer(t *testing.T) {
	f := setup(t)
	atReviewer(t, f)
	rev := f.agent(t, "Rev", &f.lead, nil)
	f.rook.ReviewerID = &rev.ID
	f.st.UpdateAgent(f.rook)
	if err := f.tm.LinksChanged(f.p.ID); err != nil {
		t.Fatal(err)
	}
	pending, _ := f.st.Undelivered(rev.ID)
	if len(pending) != 1 || !strings.HasPrefix(pending[0].Text, "Rook finished T-1 and is waiting for your review.") {
		t.Fatalf("Rev: %+v", pending)
	}
	if got := f.wantStatus(t, 1, store.StatusReview); got.ReviewStage != store.StageReviewer {
		t.Fatalf("stage = %q", got.ReviewStage)
	}
	rc := f.workerCaller(rev, 0)
	if _, err := f.tm.ReviewApprove(rc, "T-1", ""); err != nil {
		t.Fatal(err)
	}
}
