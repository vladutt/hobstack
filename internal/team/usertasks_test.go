package team

import (
	"errors"
	"testing"

	"foreman/internal/runner"
	"foreman/internal/store"
)

// proiect fara etape: utilizatorul da un task lui Rook, Rook il face, decide utilizatorul
func TestUserTaskWithoutStages(t *testing.T) {
	f := setup(t)
	var ae AssignError
	if _, err := f.tm.UserCreateTask(f.p.ID, "A", "", 0); !errors.As(err, &ae) {
		t.Fatalf("fara etape agentul e obligatoriu: %v", err)
	}
	task, err := f.tm.UserCreateTask(f.p.ID, "A", "fa A", f.rook.ID)
	if err != nil || !task.ByUser || task.AssigneeID != f.rook.ID || task.StageID != nil {
		t.Fatalf("%+v %v", task, err)
	}
	mustReconcile(t, f)
	rook1 := f.fs.last(t, f.rook)
	if _, err := f.tm.ReportDone(f.caller(t, rook1), "", "A e gata"); err != nil {
		t.Fatal(err)
	}
	got := f.wantStatus(t, 1, store.StatusReview)
	if !f.tm.UserDecides(got, mustAgents(t, f)) {
		t.Fatalf("trebuia sa decida utilizatorul: %+v", got)
	}
	if err := f.tm.UserDecide(got.ID, false, "mai adauga teste"); err != nil {
		t.Fatal(err)
	}
	if back := f.wantStatus(t, 1, store.StatusQueued); back.AssigneeID != f.rook.ID || back.Feedback != "mai adauga teste" {
		t.Fatalf("%+v", back)
	}
	f.fs.finish(rook1.ID, runner.Outcome{Status: "done"})
}

// un task al utilizatorului trece pe la reviewer-ul agentului, apoi ajunge tot la utilizator
func TestUserTaskThroughReviewer(t *testing.T) {
	f := setup(t)
	wes := f.agent(t, "Wes", &f.lead, &f.vera) // Vera verifica munca lui Wes
	task, _ := f.tm.UserCreateTask(f.p.ID, "A", "", wes.ID)
	task.Status = store.StatusInProgress
	f.st.SaveTask(&task)
	if _, err := f.tm.ReportDone(f.workerCaller(wes, task.ID), "", "gata"); err != nil {
		t.Fatal(err)
	}
	vc := f.workerCaller(f.vera, 0)
	vc.IsReviewer = true
	if _, err := f.tm.ReviewApprove(vc, "T-1", ""); err != nil {
		t.Fatal(err)
	}
	got := f.wantStatus(t, 1, store.StatusReview)
	if !f.tm.UserDecides(got, mustAgents(t, f)) {
		t.Fatalf("dupa reviewer decide utilizatorul: %+v", got)
	}
}

func TestAssignTask(t *testing.T) {
	f := setup(t)
	queued := f.task(t, "A", f.rook, store.StatusQueued)
	if err := f.tm.AssignTask(queued.ID, f.vera.ID); err != nil {
		t.Fatal(err)
	}
	if got := f.wantStatus(t, 1, store.StatusQueued); got.AssigneeID != f.vera.ID {
		t.Fatalf("%+v", got)
	}
	blocked := f.task(t, "B", f.rook, store.StatusBlocked)
	blocked.SendBacks = MaxSendBacks
	f.st.SaveTask(&blocked)
	if err := f.tm.AssignTask(blocked.ID, f.vera.ID); err != nil {
		t.Fatal(err)
	}
	if got := f.wantStatus(t, 2, store.StatusQueued); got.AssigneeID != f.vera.ID || got.SendBacks != 0 {
		t.Fatalf("deblocat si mutat: %+v", got)
	}
	running := f.task(t, "C", f.rook, store.StatusInProgress)
	if err := f.tm.AssignTask(running.ID, f.vera.ID); !errors.Is(err, ErrTaskBusy) {
		t.Fatalf("in lucru: %v", err)
	}
	solo := store.Agent{ProjectID: f.p.ID, Name: "Banca", Model: "haiku", Detached: true}
	f.st.CreateAgent(&solo)
	var ae AssignError
	if err := f.tm.AssignTask(queued.ID, solo.ID); !errors.As(err, &ae) {
		t.Fatalf("nelegat: %v", err)
	}
}

func TestAssignTaskInStageNeedsStageAgent(t *testing.T) {
	f, wes, _ := stagesFixture(t)
	task, _ := f.tm.UserCreateTask(f.p.ID, "A", "", 0)
	var ae AssignError
	if err := f.tm.AssignTask(task.ID, f.vera.ID); !errors.As(err, &ae) {
		t.Fatalf("Vera nu e in Dev: %v", err)
	}
	if err := f.tm.AssignTask(task.ID, wes.ID); err != nil {
		t.Fatal(err)
	}
	mustReconcile(t, f)
	f.fs.last(t, wes) // Wes il preia primul, fiindca a fost ales
}
