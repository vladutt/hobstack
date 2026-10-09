package team

import (
	"errors"
	"strings"
	"testing"
	"time"

	"hobstack/internal/schedule"
	"hobstack/internal/store"
)

var daily9 = schedule.Spec{Kind: schedule.KindDaily, At: "09:00"}

func at(s string) time.Time {
	v, err := time.ParseInLocation("2006-01-02 15:04", s, time.Local)
	if err != nil {
		panic(err)
	}
	return v
}

func (f *fixture) recurring(t *testing.T, in RecurringInput, now time.Time) store.RecurringTask {
	t.Helper()
	f.tm.now = func() time.Time { return now }
	r, err := f.tm.CreateRecurring(f.p.ID, in)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestCreateRecurringValidates(t *testing.T) {
	f := setup(t)
	var re RecurringError
	if _, err := f.tm.CreateRecurring(f.p.ID, RecurringInput{Title: "x", Schedule: daily9, Enabled: true}); !errors.As(err, &re) {
		t.Fatalf("fara etape agentul e obligatoriu: %v", err)
	}
	if _, err := f.tm.CreateRecurring(f.p.ID, RecurringInput{Title: "x", AssigneeID: f.rook.ID, Schedule: schedule.Spec{Kind: "monthly"}}); !errors.As(err, &re) {
		t.Fatalf("program invalid: %v", err)
	}
	if _, err := f.tm.CreateRecurring(f.p.ID, RecurringInput{Title: "x", AssigneeID: 99999, Schedule: daily9}); err == nil {
		t.Fatal("agent din alt proiect / inexistent")
	}
	r := f.recurring(t, RecurringInput{Title: "Tests", AssigneeID: f.rook.ID, Schedule: daily9, Enabled: true}, at("2026-10-05 10:00"))
	if !r.NextRunAt.Equal(at("2026-10-06 09:00")) {
		t.Fatalf("next = %v", r.NextRunAt)
	}
}

func TestRunDueCreatesTask(t *testing.T) {
	f := setup(t)
	r := f.recurring(t, RecurringInput{Title: "Tests", Description: "run", AssigneeID: f.rook.ID, Schedule: daily9, Enabled: true, AutoAccept: true}, at("2026-10-05 08:00"))
	if err := f.tm.RunDue(at("2026-10-05 08:59")); err != nil {
		t.Fatal(err)
	}
	if tasks, _ := f.st.ListTasks(f.p.ID); len(tasks) != 0 {
		t.Fatalf("prea devreme: %+v", tasks)
	}
	if err := f.tm.RunDue(at("2026-10-05 09:00")); err != nil {
		t.Fatal(err)
	}
	task := f.wantStatus(t, 1, store.StatusQueued)
	if task.RecurringID == nil || *task.RecurringID != r.ID || !task.AutoAccept || !task.ByUser || task.AssigneeID != f.rook.ID {
		t.Fatalf("%+v", task)
	}
	got, _ := f.st.GetRecurring(r.ID)
	if got.LastTaskID == nil || *got.LastTaskID != task.ID || got.LastNote != "" || !got.NextRunAt.Equal(at("2026-10-06 09:00")) {
		t.Fatalf("%+v", got)
	}
	evs, _ := f.st.ListTaskEvents(task.ID)
	if len(evs) == 0 || evs[0].Text != `Created by recurring task "Tests"` {
		t.Fatalf("%+v", evs)
	}
}

func TestRunDueMissedRunsCreateOne(t *testing.T) {
	f := setup(t)
	r := f.recurring(t, RecurringInput{Title: "Hourly", AssigneeID: f.rook.ID, Schedule: schedule.Spec{Kind: schedule.KindEveryHours, EveryHours: 1}, Enabled: true}, at("2026-10-05 08:30"))
	// HobStack a fost inchis doua zile
	now := at("2026-10-07 10:15")
	if err := f.tm.RunDue(now); err != nil {
		t.Fatal(err)
	}
	if err := f.tm.RunDue(now); err != nil { // al doilea tur in acelasi minut: nimic nou
		t.Fatal(err)
	}
	if tasks, _ := f.st.ListTasks(f.p.ID); len(tasks) != 1 {
		t.Fatalf("vreau un singur task, am %d", len(tasks))
	}
	if got, _ := f.st.GetRecurring(r.ID); !got.NextRunAt.Equal(at("2026-10-07 11:00")) {
		t.Fatalf("next = %v", got.NextRunAt)
	}
}

func TestRunDueSkipsWhileOpen(t *testing.T) {
	f := setup(t)
	r := f.recurring(t, RecurringInput{Title: "Hourly", AssigneeID: f.rook.ID, Schedule: schedule.Spec{Kind: schedule.KindEveryHours, EveryHours: 1}, Enabled: true}, at("2026-10-05 08:30"))
	f.tm.RunDue(at("2026-10-05 10:00"))
	f.tm.RunDue(at("2026-10-05 11:00"))
	if tasks, _ := f.st.ListTasks(f.p.ID); len(tasks) != 1 {
		t.Fatalf("task-ul precedent e deschis: vreau 1, am %d", len(tasks))
	}
	got, _ := f.st.GetRecurring(r.ID)
	if got.LastNote != "Skipped at 11:00: T-1 still open" || !got.NextRunAt.Equal(at("2026-10-05 12:00")) {
		t.Fatalf("%+v", got)
	}
	task := f.wantStatus(t, 1, store.StatusQueued)
	task.Status = store.StatusDone
	f.st.SaveTask(&task)
	f.tm.RunDue(at("2026-10-05 12:00"))
	if tasks, _ := f.st.ListTasks(f.p.ID); len(tasks) != 2 {
		t.Fatalf("dupa Done urmeaza alt task: %d", len(tasks))
	}
}

func TestRunDueAssigneeDeleted(t *testing.T) {
	f := setup(t)
	wes := f.agent(t, "Wes", &f.lead, nil)
	r := f.recurring(t, RecurringInput{Title: "x", AssigneeID: wes.ID, Schedule: daily9, Enabled: true}, at("2026-10-05 08:00"))
	if err := f.st.DeleteAgent(wes.ID); err != nil {
		t.Fatal(err)
	}
	if err := f.tm.RunDue(at("2026-10-05 09:00")); err != nil {
		t.Fatal(err)
	}
	got, _ := f.st.GetRecurring(r.ID)
	if got.Enabled || got.LastNote != "Turned off: choose an agent (no assignee and the project has no stages)" {
		t.Fatalf("%+v", got)
	}
}

func TestRunDueErrorDoesNotStopOthers(t *testing.T) {
	f := setup(t)
	// cu etape, un agent care nu e in prima etapa da AssignError la creare
	stages, err := f.st.SaveStages(f.p.ID, []store.Stage{{Name: "Dev", Kind: store.KindWork, AgentIDs: []int64{f.rook.ID}}})
	if err != nil {
		t.Fatal(err)
	}
	bad := f.recurring(t, RecurringInput{Title: "bad", AssigneeID: f.rook.ID, Schedule: daily9, Enabled: true}, at("2026-10-05 08:00"))
	stages[0].AgentIDs = []int64{f.vera.ID} // Rook scos din etapa dupa creare (ID-ul etapei pastrat)
	if _, err := f.st.SaveStages(f.p.ID, stages); err != nil {
		t.Fatal(err)
	}
	good := f.recurring(t, RecurringInput{Title: "good", Schedule: daily9, Enabled: true}, at("2026-10-05 08:00"))
	if err := f.tm.RunDue(at("2026-10-05 09:00")); err != nil {
		t.Fatal(err)
	}
	b, _ := f.st.GetRecurring(bad.ID)
	if b.LastNote == "" || !b.Enabled || !b.NextRunAt.Equal(at("2026-10-06 09:00")) {
		t.Fatalf("eroarea in nota, programul mutat: %+v", b)
	}
	if g, _ := f.st.GetRecurring(good.ID); g.LastTaskID == nil {
		t.Fatalf("cealalta sarcina trebuia sa ruleze: %+v", g)
	}
}

func TestRunDueRereadsUnderLock(t *testing.T) {
	f := setup(t)
	r := f.recurring(t, RecurringInput{Title: "x", AssigneeID: f.rook.ID, Schedule: daily9, Enabled: true}, at("2026-10-05 08:00"))
	due, _ := f.st.DueRecurring(at("2026-10-05 09:00"))
	// utilizatorul o opreste intre citire si rulare
	off := RecurringInput{Title: "x", AssigneeID: f.rook.ID, Schedule: daily9, Enabled: false}
	if _, err := f.tm.UpdateRecurring(r.ID, off); err != nil {
		t.Fatal(err)
	}
	if err := f.tm.runRecurring(due[0].ID, at("2026-10-05 09:00"), true); err != nil {
		t.Fatal(err)
	}
	if tasks, _ := f.st.ListTasks(f.p.ID); len(tasks) != 0 {
		t.Fatalf("sarcina oprita nu creeaza task: %+v", tasks)
	}
}

func TestRunRecurringNow(t *testing.T) {
	f := setup(t)
	r := f.recurring(t, RecurringInput{Title: "x", AssigneeID: f.rook.ID, Schedule: daily9, Enabled: false}, at("2026-10-05 08:00"))
	task, err := f.tm.RunRecurringNow(r.ID)
	if err != nil || task.Number != 1 {
		t.Fatalf("%+v %v", task, err)
	}
	var re RecurringError
	if _, err := f.tm.RunRecurringNow(r.ID); !errors.As(err, &re) || !strings.Contains(err.Error(), "T-1 is still open") {
		t.Fatalf("suprapunere: %v", err)
	}
	if got, _ := f.st.GetRecurring(r.ID); !got.NextRunAt.Equal(r.NextRunAt) {
		t.Fatalf("Run now nu muta programul: %v -> %v", r.NextRunAt, got.NextRunAt)
	}
}

func TestUpdateRecurringRecomputesNext(t *testing.T) {
	f := setup(t)
	r := f.recurring(t, RecurringInput{Title: "x", AssigneeID: f.rook.ID, Schedule: daily9, Enabled: true}, at("2026-10-05 10:00"))
	f.tm.now = func() time.Time { return at("2026-10-05 11:00") }
	in := RecurringInput{Title: "y", AssigneeID: f.rook.ID, Schedule: schedule.Spec{Kind: schedule.KindDaily, At: "12:00"}, Enabled: true}
	got, err := f.tm.UpdateRecurring(r.ID, in)
	if err != nil || got.Title != "y" || !got.NextRunAt.Equal(at("2026-10-05 12:00")) {
		t.Fatalf("%+v %v", got, err)
	}
	in.Title = "z" // doar titlul: programul ramane
	got, _ = f.tm.UpdateRecurring(r.ID, in)
	if !got.NextRunAt.Equal(at("2026-10-05 12:00")) {
		t.Fatalf("%v", got.NextRunAt)
	}
	if err := f.tm.DeleteRecurring(r.ID); err != nil {
		t.Fatal(err)
	}
}

// recurringTask creeaza prin scheduler un task pentru agent, cu auto-accept dat
func (f *fixture) recurringTask(t *testing.T, who store.Agent, auto bool) store.Task {
	t.Helper()
	r := f.recurring(t, RecurringInput{Title: "Daily", AssigneeID: who.ID, Schedule: daily9, Enabled: true, AutoAccept: auto}, at("2026-10-05 08:00"))
	if _, err := f.tm.RunRecurringNow(r.ID); err != nil {
		t.Fatal(err)
	}
	tasks, _ := f.st.ListTasks(f.p.ID)
	task := tasks[len(tasks)-1]
	task.Status = store.StatusInProgress
	f.st.SaveTask(&task)
	return task
}

func lastEventText(t *testing.T, f *fixture, taskID int64) string {
	t.Helper()
	evs, _ := f.st.ListTaskEvents(taskID)
	return evs[len(evs)-1].Text
}

func TestAutoAcceptWithoutReviewer(t *testing.T) {
	f := setup(t)
	task := f.recurringTask(t, f.rook, true)
	if _, err := f.tm.ReportDone(f.workerCaller(f.rook, task.ID), "", "all green"); err != nil {
		t.Fatal(err)
	}
	f.wantStatus(t, task.Number, store.StatusDone)
	if got := lastEventText(t, f, task.ID); got != "Accepted automatically (recurring task)" {
		t.Fatalf("%q", got)
	}
}

func TestAutoAcceptAfterReviewer(t *testing.T) {
	f := setup(t)
	wes := f.agent(t, "Wes", &f.lead, &f.vera)
	task := f.recurringTask(t, wes, true)
	f.tm.ReportDone(f.workerCaller(wes, task.ID), "", "gata")
	f.wantStatus(t, task.Number, store.StatusReview) // intai reviewer-ul
	vc := f.workerCaller(f.vera, 0)
	vc.IsReviewer = true
	if _, err := f.tm.ReviewApprove(vc, task.Ref(), ""); err != nil {
		t.Fatal(err)
	}
	f.wantStatus(t, task.Number, store.StatusDone)
}

func TestAutoAcceptAfterLastStage(t *testing.T) {
	f := setup(t)
	if _, err := f.st.SaveStages(f.p.ID, []store.Stage{{Name: "Dev", Kind: store.KindWork, AgentIDs: []int64{f.rook.ID}}}); err != nil {
		t.Fatal(err)
	}
	task := f.recurringTask(t, f.rook, true)
	if _, err := f.tm.ReportDone(f.workerCaller(f.rook, task.ID), "", "gata"); err != nil {
		t.Fatal(err)
	}
	f.wantStatus(t, task.Number, store.StatusDone)
}

func TestNoAutoAcceptWaitsForUser(t *testing.T) {
	f := setup(t)
	task := f.recurringTask(t, f.rook, false)
	f.tm.ReportDone(f.workerCaller(f.rook, task.ID), "", "gata")
	got := f.wantStatus(t, task.Number, store.StatusReview)
	if !f.tm.UserDecides(got, mustAgents(t, f), false) {
		t.Fatalf("fara auto-accept decide utilizatorul: %+v", got)
	}
}

func TestUpdateRecurringDisableBrokenAssignee(t *testing.T) {
	var re RecurringError
	// agent detasat: oprirea merge, pornirea nu
	f := setup(t)
	r := f.recurring(t, RecurringInput{Title: "x", AssigneeID: f.rook.ID, Schedule: daily9, Enabled: true}, at("2026-10-05 08:00"))
	wes := f.agent(t, "Wes", &f.lead, nil)
	r = f.recurring(t, RecurringInput{Title: "x", AssigneeID: wes.ID, Schedule: daily9, Enabled: true}, at("2026-10-05 08:00"))
	wes.ParentID, wes.Detached = nil, true
	if err := f.st.UpdateAgent(wes); err != nil {
		t.Fatal(err)
	}
	in := RecurringInput{Title: "x", AssigneeID: wes.ID, Schedule: daily9, Enabled: false}
	if got, err := f.tm.UpdateRecurring(r.ID, in); err != nil || got.Enabled {
		t.Fatalf("oprire: %+v %v", got, err)
	}
	in.Enabled = true
	if _, err := f.tm.UpdateRecurring(r.ID, in); !errors.As(err, &re) {
		t.Fatalf("pornire cu agent detasat: %v", err)
	}
	// scos din prima etapa
	f = setup(t)
	if _, err := f.st.SaveStages(f.p.ID, []store.Stage{{Name: "Dev", Kind: store.KindWork, AgentIDs: []int64{f.rook.ID, f.vera.ID}}}); err != nil {
		t.Fatal(err)
	}
	r = f.recurring(t, RecurringInput{Title: "x", AssigneeID: f.rook.ID, Schedule: daily9, Enabled: true}, at("2026-10-05 08:00"))
	if _, err := f.st.SaveStages(f.p.ID, []store.Stage{{Name: "Dev", Kind: store.KindWork, AgentIDs: []int64{f.vera.ID}}}); err != nil {
		t.Fatal(err)
	}
	in = RecurringInput{Title: "x", AssigneeID: f.rook.ID, Schedule: daily9, Enabled: false}
	if got, err := f.tm.UpdateRecurring(r.ID, in); err != nil || got.Enabled {
		t.Fatalf("oprire (scos din etapa): %+v %v", got, err)
	}
	in.Enabled = true
	if _, err := f.tm.UpdateRecurring(r.ID, in); !errors.As(err, &re) {
		t.Fatalf("pornire (scos din etapa): %v", err)
	}
	// agent din alt proiect nu trece nici oprit
	if _, err := f.tm.UpdateRecurring(r.ID, RecurringInput{Title: "x", AssigneeID: 99999, Schedule: daily9}); err == nil {
		t.Fatal("agent inexistent / din alt proiect")
	}
}

func TestUpdateRecurringDisableAssigneeDeleted(t *testing.T) {
	f := setup(t)
	wes := f.agent(t, "Wes", &f.lead, nil)
	r := f.recurring(t, RecurringInput{Title: "x", AssigneeID: wes.ID, Schedule: daily9, Enabled: true}, at("2026-10-05 08:00"))
	if err := f.st.DeleteAgent(wes.ID); err != nil {
		t.Fatal(err)
	}
	got, err := f.tm.UpdateRecurring(r.ID, RecurringInput{Title: "x", Schedule: daily9, Enabled: false})
	if err != nil || got.Enabled || got.AssigneeID != nil {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestUpdateRecurringReenableClearsNote(t *testing.T) {
	f := setup(t)
	r := f.recurring(t, RecurringInput{Title: "x", AssigneeID: f.rook.ID, Schedule: daily9, Enabled: false}, at("2026-10-05 08:00"))
	r.LastNote = "Turned off: something"
	if err := f.st.SaveRecurring(&r); err != nil {
		t.Fatal(err)
	}
	got, err := f.tm.UpdateRecurring(r.ID, RecurringInput{Title: "x", AssigneeID: f.rook.ID, Schedule: daily9, Enabled: true})
	if err != nil || got.LastNote != "" {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestRunRecurringNowNoAssignee(t *testing.T) {
	f := setup(t)
	wes := f.agent(t, "Wes", &f.lead, nil)
	r := f.recurring(t, RecurringInput{Title: "x", AssigneeID: wes.ID, Schedule: daily9, Enabled: true}, at("2026-10-05 08:00"))
	if err := f.st.DeleteAgent(wes.ID); err != nil {
		t.Fatal(err)
	}
	var re RecurringError
	_, err := f.tm.RunRecurringNow(r.ID)
	if !errors.As(err, &re) || err.Error() != "choose an agent first: the recurring task has no assignee" {
		t.Fatalf("%v", err)
	}
}

func TestReconcileReviewerGoneAutoAccept(t *testing.T) {
	f := setup(t)
	wes := f.agent(t, "Wes", nil, &f.vera) // radacina cu reviewer: nimeni deasupra
	task := store.Task{ProjectID: f.p.ID, Title: "A", AssigneeID: wes.ID, AutoAccept: true}
	if err := f.st.CreateTask(&task); err != nil {
		t.Fatal(err)
	}
	task.Status, task.ReviewStage = store.StatusReview, store.StageReviewer
	f.st.SaveTask(&task)
	wes.ReviewerID = nil
	if err := f.st.UpdateAgent(wes); err != nil {
		t.Fatal(err)
	}
	if err := f.tm.LinksChanged(f.p.ID); err != nil {
		t.Fatal(err)
	}
	f.wantStatus(t, task.Number, store.StatusDone)
	if got := lastEventText(t, f, task.ID); got != "Accepted automatically (recurring task)" {
		t.Fatalf("%q", got)
	}
}
