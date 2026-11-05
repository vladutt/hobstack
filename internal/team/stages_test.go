package team

import (
	"errors"
	"strconv"
	"strings"
	"testing"

	"hobstack/internal/runner"
	"hobstack/internal/store"
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
		if _, err := f.tm.CreateTask(lc, title, "", "", "", "Dev"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.tm.CreateTask(lc, "C", "", "Vera", "", "Dev"); err == nil || !strings.Contains(err.Error(), "is not in stage Dev") {
		t.Fatalf("assignee din alta etapa: %v", err)
	}
	mustReconcile(t, f)
	rook1, wes1 := f.fs.last(t, f.rook), f.fs.last(t, wes)
	if !strings.Contains(turn(rook1), "Task T-1: A") || !strings.Contains(turn(wes1), "Task T-2: B") {
		t.Fatalf("primul liber: %q / %q", turn(rook1), turn(wes1))
	}
	if !strings.Contains(turn(rook1), "Stage: Dev (work)") || !strings.Contains(turn(rook1), "goes to QA") {
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

func TestUserCreateTaskInStages(t *testing.T) {
	f, _, stages := stagesFixture(t)
	task, err := f.tm.UserCreateTask(f.p.ID, "Login", "cu parola", 0, 0)
	if err != nil || !task.ByUser || task.CreatorID != nil || task.StageID == nil || *task.StageID != stages[0].ID {
		t.Fatalf("%+v %v", task, err)
	}
	var ae AssignError
	if _, err := f.tm.UserCreateTask(f.p.ID, "X", "", f.vera.ID, stages[0].ID); !errors.As(err, &ae) {
		t.Fatalf("Vera nu e in prima etapa: %v", err)
	}
	// fara etapa aleasa, task-ul merge pe drumul standard, la agentul ales
	std, err := f.tm.UserCreateTask(f.p.ID, "Y", "", f.vera.ID, 0)
	if err != nil || std.StageID != nil || std.ViaStages || std.AssigneeID != f.vera.ID {
		t.Fatalf("drum standard: %+v %v", std, err)
	}
	// o etapa anume (nu doar prima)
	in2, err := f.tm.UserCreateTask(f.p.ID, "Z", "", 0, stages[1].ID)
	if err != nil || in2.StageID == nil || *in2.StageID != stages[1].ID || !in2.ViaStages {
		t.Fatalf("etapa a doua: %+v %v", in2, err)
	}
	if _, err := f.tm.UserCreateTask(f.p.ID, "W", "", 0, 99999); !errors.As(err, &ae) {
		t.Fatalf("etapa inexistenta: %v", err)
	}
}

func TestStageWithoutAgentsWaits(t *testing.T) {
	f := setup(t)
	if _, err := f.st.SaveStages(f.p.ID, []store.Stage{{Name: "Gol", Kind: store.KindWork}}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.tm.CreateTask(f.leadCaller(), "A", "", "", "", "Gol"); err != nil {
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
	if _, err := f.tm.CreateTask(c, "A", "", "", "", "Dev"); err != nil {
		t.Fatal(err)
	}
}

func TestStageFullFlow(t *testing.T) {
	f, _, stages := stagesFixture(t)
	if _, err := f.tm.CreateTask(f.leadCaller(), "Login", "", "Rook", "", "Dev"); err != nil {
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
	if vc.StageKind != store.KindCheck || !strings.Contains(turn(vera1), "Stage: QA (check)") {
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
	f.tm.CreateTask(f.leadCaller(), "A", "", "", "", "Dev")
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
	task, _ := f.tm.UserCreateTask(f.p.ID, "A", "", 0, 0)
	// sarim direct la finalul etapelor
	task.StageID, task.Status, task.ReviewStage = nil, store.StatusReview, store.StageLead
	f.st.SaveTask(&task)
	if id := decisionOwner(task, mustAgents(t, f)); id != 0 {
		t.Fatalf("un task al utilizatorului nu il decide agentul %d", id)
	}
	if err := f.tm.UserDecide(task.ID, false, "", nil); err == nil {
		t.Fatal("trimis inapoi fara feedback trebuia refuzat")
	}
	if err := f.tm.UserDecide(task.ID, false, "mai incearca", nil); err != nil {
		t.Fatal(err)
	}
	back := f.wantStatus(t, 1, store.StatusQueued)
	if back.StageID == nil || *back.StageID != stages[0].ID || back.Feedback != "mai incearca" {
		t.Fatalf("%+v", back)
	}
	back.StageID, back.Status, back.ReviewStage = nil, store.StatusReview, store.StageLead
	f.st.SaveTask(&back)
	if err := f.tm.UserDecide(back.ID, true, "", nil); err != nil {
		t.Fatal(err)
	}
	f.wantStatus(t, 1, store.StatusDone)
}

func TestStageRunWithoutReportGoesToLead(t *testing.T) {
	f, _, stages := stagesFixture(t)
	f.tm.CreateTask(f.leadCaller(), "A", "", "Rook", "", "Dev")
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

// Cursa din e2e: Dev raporteaza, task-ul trece in QA si Vera il preia inainte
// ca procesul lui Rook sa se termine. Finalul run-ului lui Rook nu are voie
// sa ia task-ul de la Vera.
func TestStageOldRunEndDoesNotStealTask(t *testing.T) {
	f, _, stages := stagesFixture(t)
	f.tm.CreateTask(f.leadCaller(), "A", "", "Rook", "", "Dev")
	mustReconcile(t, f)
	rook1 := f.fs.last(t, f.rook)
	if _, err := f.tm.ReportDone(f.caller(t, rook1), "", "gata"); err != nil {
		t.Fatal(err)
	}
	mustReconcile(t, f) // Vera preia T-1 in QA
	vera1 := f.fs.last(t, f.vera)
	f.fs.finish(rook1.ID, runner.Outcome{Status: "done", Result: "mesajul final al lui Rook"})
	task := f.wantStatus(t, 1, store.StatusInProgress)
	if task.AssigneeID != f.vera.ID || *task.StageID != stages[1].ID {
		t.Fatalf("task-ul trebuia sa ramana la Vera in QA: %+v", task)
	}
	if _, err := f.tm.ReviewApprove(f.caller(t, vera1), "T-1", ""); err != nil {
		t.Fatalf("aprobarea Verei: %v", err)
	}
}

// liderul unui proiect cu etape vede etapele si cine lucreaza in ele
func TestLeadTurnListsStages(t *testing.T) {
	f, _, _ := stagesFixture(t)
	f.tm.UserMessage(f.lead.ID, "fa ceva", nil)
	mustReconcile(t, f)
	lead := f.fs.last(t, f.lead)
	if !strings.Contains(turn(lead), "Board stages") || !strings.Contains(turn(lead), "QA (check): Vera") {
		t.Fatalf("tura liderului: %q", turn(lead))
	}
	if c := f.caller(t, lead); !c.HasStages {
		t.Fatal("HasStages lipseste")
	}
}

// Critical din review: un task blocat dupa decizia finala ramane intr-o etapa
// (altfel dispare de pe board si nu se mai poate debloca)
func TestBlockedAfterFinalSendBackKeepsStage(t *testing.T) {
	f, _, stages := stagesFixture(t)
	f.tm.CreateTask(f.leadCaller(), "A", "", "", "", "Dev")
	task, _ := f.st.GetTaskByNumber(f.p.ID, 1)
	task.StageID, task.Status, task.ReviewStage, task.SendBacks = nil, store.StatusReview, store.StageLead, MaxSendBacks-1
	f.st.SaveTask(&task)
	if _, err := f.tm.SendBack(f.leadCaller(), "T-1", "tot nu"); err != nil {
		t.Fatal(err)
	}
	back := f.wantStatus(t, 1, store.StatusBlocked)
	if back.StageID == nil || *back.StageID != stages[0].ID {
		t.Fatalf("blocat fara etapa: %+v", back)
	}
}

// Important din review: seful asignatului nu decide un task creat de utilizator
func TestLeadCannotDecideUserTask(t *testing.T) {
	f, _, _ := stagesFixture(t)
	task, _ := f.tm.UserCreateTask(f.p.ID, "A", "", 0, 0)
	task.StageID, task.Status, task.ReviewStage, task.AssigneeID = nil, store.StatusReview, store.StageLead, f.vera.ID
	f.st.SaveTask(&task)
	if _, err := f.tm.AcceptTask(f.leadCaller(), "T-1", ""); err == nil {
		t.Fatal("liderul a acceptat un task al utilizatorului")
	}
	f.wantStatus(t, 1, store.StatusReview)
}

// un agent nelegat nu lucreaza, chiar daca e bifat intr-o etapa
func TestDetachedStageAgentDoesNotWork(t *testing.T) {
	f := setup(t)
	solo := store.Agent{ProjectID: f.p.ID, Name: "Banca", Model: "haiku", Detached: true}
	if err := f.st.CreateAgent(&solo); err != nil {
		t.Fatal(err)
	}
	if _, err := f.st.SaveStages(f.p.ID, []store.Stage{{Name: "Dev", Kind: store.KindWork, AgentIDs: []int64{solo.ID}}}); err != nil {
		t.Fatal(err)
	}
	f.tm.CreateTask(f.leadCaller(), "A", "", "", "", "Dev")
	n := f.fs.count()
	mustReconcile(t, f)
	f.wantStatus(t, 1, store.StatusQueued)
	if f.fs.count() != n {
		t.Fatal("agentul nelegat a primit un run")
	}
}

// Important din review: un task de etapa fara decident il decide utilizatorul
func TestUserDecidesWhenNoDecider(t *testing.T) {
	f, _, _ := stagesFixture(t)
	task := store.Task{ProjectID: f.p.ID, Title: "A", AssigneeID: f.lead.ID, Status: store.StatusReview, ReviewStage: store.StageLead}
	f.st.CreateTask(&task)
	task.Status, task.ReviewStage = store.StatusReview, store.StageLead
	f.st.SaveTask(&task)
	if !f.tm.UserDecides(task, mustAgents(t, f), f.p.Autonomy) {
		t.Fatal("fara creator si cu asignat radacina, decide utilizatorul")
	}
}

// Etapele sunt optionale: fara stage, create_task merge pe drumul standard, si intr-un proiect cu etape.
func TestCreateTaskWithoutStageInStagedProject(t *testing.T) {
	f, _, stages := stagesFixture(t)
	if _, err := f.tm.CreateTask(f.leadCaller(), "Std", "", "Vera", "", ""); err != nil {
		t.Fatal(err)
	}
	std := f.wantStatus(t, 1, store.StatusQueued)
	if std.StageID != nil || std.ViaStages || std.AssigneeID != f.vera.ID {
		t.Fatalf("drum standard: %+v", std)
	}
	// fara asignat si fara etapa: regula echipei directe
	if _, err := f.tm.CreateTask(f.leadCaller(), "X", "", "", "", ""); err == nil {
		t.Fatal("fara asignat si fara etapa trebuia refuzat")
	}
	if _, err := f.tm.CreateTask(f.leadCaller(), "X", "", "", "", "Nope"); err == nil || !strings.Contains(err.Error(), "Dev, QA") {
		t.Fatalf("etapa necunoscuta: %v", err)
	}
	// etapa a doua, numele fara diferente de litere
	if _, err := f.tm.CreateTask(f.leadCaller(), "Check", "", "", "", "qa"); err != nil {
		t.Fatal(err)
	}
	qa := f.wantStatus(t, 2, store.StatusQueued)
	if qa.StageID == nil || *qa.StageID != stages[1].ID || !qa.ViaStages {
		t.Fatalf("in QA: %+v", qa)
	}
}

// Un task de pe drumul standard trimis inapoi ramane la asignat, nu intra in prima etapa.
func TestSendBackStandardTaskInStagedProject(t *testing.T) {
	f, _, _ := stagesFixture(t)
	task := f.task(t, "Std", f.vera, store.StatusReview)
	if _, err := f.tm.SendBack(f.leadCaller(), task.Ref(), "mai mult"); err != nil {
		t.Fatal(err)
	}
	got := f.wantStatus(t, task.Number, store.StatusQueued)
	if got.StageID != nil || got.AssigneeID != f.vera.ID || got.Feedback != "mai mult" {
		t.Fatalf("lider: %+v", got)
	}
	// la fel cand decide utilizatorul (drag pe Queued)
	user, _ := f.tm.UserCreateTask(f.p.ID, "U", "", f.vera.ID, 0)
	user.Status, user.ReviewStage = store.StatusReview, store.StageLead
	if err := f.st.SaveTask(&user); err != nil {
		t.Fatal(err)
	}
	if err := f.tm.MoveTask(user.ID, "queued", "refa"); err != nil {
		t.Fatal(err)
	}
	if got := f.wantStatus(t, user.Number, store.StatusQueued); got.StageID != nil || got.AssigneeID != f.vera.ID {
		t.Fatalf("utilizator: %+v", got)
	}
}

// Un task dintr-o etapa tras pe Queued iese din etape si ramane la asignat.
func TestMoveStageTaskToQueued(t *testing.T) {
	f, _, stages := stagesFixture(t)
	task, err := f.tm.UserCreateTask(f.p.ID, "A", "", f.rook.ID, stages[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.tm.MoveTask(task.ID, "queued", ""); err != nil {
		t.Fatal(err)
	}
	got := f.wantStatus(t, task.Number, store.StatusQueued)
	if got.StageID != nil || got.ViaStages || got.AssigneeID != f.rook.ID {
		t.Fatalf("iesit din etape: %+v", got)
	}
	if err := f.tm.MoveTask(task.ID, "queued", ""); err == nil {
		t.Fatal("deja in Queued")
	}
	// si inapoi intr-o etapa
	if err := f.tm.MoveTask(task.ID, "stage:"+strconv.FormatInt(stages[1].ID, 10), ""); err != nil {
		t.Fatal(err)
	}
	if got := f.wantStatus(t, task.Number, store.StatusQueued); got.StageID == nil || *got.StageID != stages[1].ID || !got.ViaStages {
		t.Fatalf("in QA: %+v", got)
	}
}
