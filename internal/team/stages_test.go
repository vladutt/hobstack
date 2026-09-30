package team

import (
	"strings"
	"testing"

	"foreman/internal/runner"
	"foreman/internal/store"
)

// stagesFixture: Lead; Dev = Rook + Wes (lucru), QA = Vera (verificare).
func stagesFixture(t *testing.T) (*fixture, store.Agent, []store.Stage) {
	f := setup(t)
	wes := f.agent(t, "Wes", &f.lead, nil)
	stages, err := f.st.SaveStages(f.p.ID, []store.Stage{
		{Name: "Dev", Kind: store.KindWork, AgentIDs: []int64{f.rook.ID, wes.ID}},
		{Name: "QA", Kind: store.KindCheck, AgentIDs: []int64{f.vera.ID}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return f, wes, stages
}

func TestStageCreateAndFirstFree(t *testing.T) {
	f, wes, stages := stagesFixture(t)
	lc := f.leadCaller()
	for _, title := range []string{"A", "B"} {
		if _, err := f.tm.CreateTask(lc, title, "", ""); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.tm.CreateTask(lc, "C", "", "Vera"); err == nil || !strings.Contains(err.Error(), "nu e in etapa Dev") {
		t.Fatalf("assignee din alta etapa: %v", err)
	}
	mustReconcile(t, f)
	rook1, wes1 := f.fs.last(t, f.rook), f.fs.last(t, wes)
	if !strings.Contains(turn(rook1), "Task T-1: A") || !strings.Contains(turn(wes1), "Task T-2: B") {
		t.Fatalf("primul liber: %q / %q", turn(rook1), turn(wes1))
	}
	if !strings.Contains(turn(rook1), "Etapa: Dev (lucru)") || !strings.Contains(turn(rook1), "merge la QA") {
		t.Fatalf("linia etapei: %q", turn(rook1))
	}
	if c := f.caller(t, rook1); c.StageKind != store.KindWork {
		t.Fatalf("StageKind = %q", c.StageKind)
	}
	task := f.wantStatus(t, 2, store.StatusInProgress)
	if task.AssigneeID != wes.ID || task.StageID == nil || *task.StageID != stages[0].ID {
		t.Fatalf("T-2: %+v", task)
	}
	_ = runner.Outcome{}
}

func TestUserCreateTaskNeedsStages(t *testing.T) {
	f := setup(t)
	if _, err := f.tm.UserCreateTask(f.p.ID, "x", ""); err != ErrNoStages {
		t.Fatalf("fara etape: %v", err)
	}
	f2, _, stages := stagesFixture(t)
	task, err := f2.tm.UserCreateTask(f2.p.ID, "Login", "cu parola")
	if err != nil || !task.ByUser || task.CreatorID != nil || task.StageID == nil || *task.StageID != stages[0].ID {
		t.Fatalf("%+v %v", task, err)
	}
}

func TestStageWithoutAgentsWaits(t *testing.T) {
	f := setup(t)
	if _, err := f.st.SaveStages(f.p.ID, []store.Stage{{Name: "Gol", Kind: store.KindWork}}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.tm.CreateTask(f.leadCaller(), "A", "", ""); err != nil {
		t.Fatal(err)
	}
	n := f.fs.count()
	mustReconcile(t, f)
	f.wantStatus(t, 1, store.StatusQueued)
	if f.fs.count() != n {
		t.Fatal("nu trebuia pornit niciun run")
	}
}

// intr-un proiect cu etape, liderul radacina fara copii tot poate crea task-uri
func TestRootWithoutChildrenCreatesInStages(t *testing.T) {
	f := setup(t)
	solo := f.agent(t, "Solo", nil, nil)
	if _, err := f.st.SaveStages(f.p.ID, []store.Stage{{Name: "Dev", Kind: store.KindWork, AgentIDs: []int64{f.rook.ID}}}); err != nil {
		t.Fatal(err)
	}
	f.tm.UserMessage(solo.ID, "fa ceva", nil)
	mustReconcile(t, f)
	c := f.caller(t, f.fs.last(t, solo))
	if !c.IsLead {
		t.Fatal("radacina dintr-un proiect cu etape trebuia sa primeasca create_task")
	}
	if _, err := f.tm.CreateTask(c, "A", "", ""); err != nil {
		t.Fatal(err)
	}
}

func TestStageFullFlow(t *testing.T) {
	f, _, stages := stagesFixture(t)
	if _, err := f.tm.CreateTask(f.leadCaller(), "Login", "", "Rook"); err != nil {
		t.Fatal(err)
	}
	mustReconcile(t, f)
	rook1 := f.fs.last(t, f.rook)
	if _, err := f.tm.ReportDone(f.caller(t, rook1), "", "login facut"); err != nil {
		t.Fatal(err)
	}
	task := f.wantStatus(t, 1, store.StatusQueued)
	if task.StageID == nil || *task.StageID != stages[1].ID || task.Result != "login facut" {
		t.Fatalf("dupa Dev: %+v", task)
	}
	f.fs.finish(rook1.ID, runner.Outcome{Status: "done"})
	mustReconcile(t, f)
	vera1 := f.fs.last(t, f.vera)
	vc := f.caller(t, vera1)
	if vc.StageKind != store.KindCheck || !strings.Contains(turn(vera1), "Etapa: QA (verificare)") {
		t.Fatalf("QA: %+v %q", vc, turn(vera1))
	}
	// respinge -> inapoi in Dev, cu feedback
	if _, err := f.tm.ReviewReject(vc, "T-1", "lipseste validarea"); err != nil {
		t.Fatal(err)
	}
	task = f.wantStatus(t, 1, store.StatusQueued)
	if *task.StageID != stages[0].ID || task.Feedback != "lipseste validarea" || task.SendBacks != 1 {
		t.Fatalf("dupa respingere: %+v", task)
	}
	f.fs.finish(vera1.ID, runner.Outcome{Status: "done"})
	mustReconcile(t, f)
	rook2 := f.fs.last(t, f.rook)
	if !strings.Contains(turn(rook2), "lipseste validarea") {
		t.Fatalf("feedback in tura: %q", turn(rook2))
	}
	f.tm.ReportDone(f.caller(t, rook2), "", "validare adaugata")
	f.fs.finish(rook2.ID, runner.Outcome{Status: "done"})
	mustReconcile(t, f)
	vera2 := f.fs.last(t, f.vera)
	if _, err := f.tm.ReviewApprove(f.caller(t, vera2), "T-1", "ok"); err != nil {
		t.Fatal(err)
	}
	task = f.wantStatus(t, 1, store.StatusReview)
	if task.StageID != nil || task.ReviewStage != store.StageLead {
		t.Fatalf("la lider: %+v", task)
	}
	f.fs.finish(vera2.ID, runner.Outcome{Status: "done"})
	mustReconcile(t, f)
	lead := f.fs.last(t, f.lead)
	if _, err := f.tm.AcceptTask(f.caller(t, lead), "T-1", ""); err != nil {
		t.Fatal(err)
	}
	f.wantStatus(t, 1, store.StatusDone)
}

func TestStageRejectLimitBlocks(t *testing.T) {
	f, _, stages := stagesFixture(t)
	f.tm.CreateTask(f.leadCaller(), "A", "", "")
	for i := 0; i < MaxSendBacks; i++ {
		// il aducem direct in QA, in lucru la Vera
		task, _ := f.st.GetTaskByNumber(f.p.ID, 1)
		task.StageID, task.AssigneeID, task.Status = &stages[1].ID, f.vera.ID, store.StatusInProgress
		f.st.SaveTask(&task)
		c := f.workerCaller(f.vera, task.ID)
		c.StageKind = store.KindCheck
		if _, err := f.tm.ReviewReject(c, "T-1", "nu"); err != nil {
			t.Fatal(err)
		}
	}
	task := f.wantStatus(t, 1, store.StatusBlocked)
	if task.StageID == nil || *task.StageID != stages[1].ID {
		t.Fatalf("blocat in QA: %+v", task)
	}
}

func TestUserTaskDecidedByUser(t *testing.T) {
	f, _, stages := stagesFixture(t)
	task, _ := f.tm.UserCreateTask(f.p.ID, "A", "")
	// sarim direct la finalul etapelor
	task.StageID, task.Status, task.ReviewStage = nil, store.StatusReview, store.StageLead
	f.st.SaveTask(&task)
	if id := decisionOwner(task, mustAgents(t, f)); id != 0 {
		t.Fatalf("un task al utilizatorului nu il decide agentul %d", id)
	}
	if err := f.tm.UserDecide(task.ID, false, ""); err == nil {
		t.Fatal("trimis inapoi fara feedback trebuia refuzat")
	}
	if err := f.tm.UserDecide(task.ID, false, "mai incearca"); err != nil {
		t.Fatal(err)
	}
	back := f.wantStatus(t, 1, store.StatusQueued)
	if back.StageID == nil || *back.StageID != stages[0].ID || back.Feedback != "mai incearca" {
		t.Fatalf("%+v", back)
	}
	back.StageID, back.Status, back.ReviewStage = nil, store.StatusReview, store.StageLead
	f.st.SaveTask(&back)
	if err := f.tm.UserDecide(back.ID, true, ""); err != nil {
		t.Fatal(err)
	}
	f.wantStatus(t, 1, store.StatusDone)
}

func TestStageRunWithoutReportGoesToLead(t *testing.T) {
	f, _, stages := stagesFixture(t)
	f.tm.CreateTask(f.leadCaller(), "A", "", "Rook")
	mustReconcile(t, f)
	rook1 := f.fs.last(t, f.rook)
	f.fs.finish(rook1.ID, runner.Outcome{Status: "done", Result: "am uitat de raport"})
	task := f.wantStatus(t, 1, store.StatusReview)
	if task.StageID == nil || *task.StageID != stages[0].ID || task.ReviewStage != store.StageLead {
		t.Fatalf("%+v", task)
	}
	mustReconcile(t, f)
	lead := f.fs.last(t, f.lead)
	if _, err := f.tm.SendBack(f.caller(t, lead), "T-1", "raporteaza"); err != nil {
		t.Fatal(err)
	}
	if back := f.wantStatus(t, 1, store.StatusQueued); *back.StageID != stages[0].ID {
		t.Fatalf("%+v", back)
	}
}

func mustAgents(t *testing.T, f *fixture) []store.Agent {
	agents, err := f.st.ListAgents(f.p.ID)
	if err != nil {
		t.Fatal(err)
	}
	return agents
}
