package team

import (
	"strings"
	"testing"

	"foreman/internal/mcp"
	"foreman/internal/store"
)

func twoQuestions() []mcp.DecisionInput {
	return []mcp.DecisionInput{
		{Question: "Open a follow-up?", Options: []string{"Yes", "No"}},
		{Question: "Which?", Options: []string{"A", "B", "C"}, Multi: true},
	}
}

func TestAskDecisionsSavesWithAgentName(t *testing.T) {
	f := setup(t)
	task := f.task(t, "Build page", f.rook, store.StatusInProgress)
	c := f.workerCaller(f.rook, task.ID)
	if _, err := f.tm.ReportDone(c, "", "done"); err != nil {
		t.Fatal(err)
	}
	out, err := f.tm.AskDecisions(c, "", twoQuestions())
	if err != nil || !strings.Contains(out, "2 decisions waiting for the user.") {
		t.Fatalf("%q %v", out, err)
	}
	list, _ := f.st.ListDecisions(task.ID)
	if len(list) != 2 || list[0].AskedBy != f.rook.Name || list[1].Question != "Which?" || !list[1].Multi || len(list[1].Options) != 3 {
		t.Fatalf("%+v", list)
	}
	// un agent strain nu poate pune intrebari pe task
	if _, err := f.tm.AskDecisions(f.workerCaller(f.vera, 0), task.Ref(), twoQuestions()); err == nil {
		t.Fatal("un agent strain a pus intrebari")
	}
}

func TestAskDecisionsArchivedTask(t *testing.T) {
	f := setup(t)
	task := f.task(t, "Build page", f.rook, store.StatusInProgress)
	if _, err := f.tm.ArchiveTask(task.ID); err != nil {
		t.Fatal(err)
	}
	_, err := f.tm.AskDecisions(f.workerCaller(f.rook, task.ID), "", twoQuestions())
	if err == nil || !strings.Contains(err.Error(), "was deleted") {
		t.Fatalf("%v", err)
	}
}

func TestLeaveStepsSavesAsSteps(t *testing.T) {
	f := setup(t)
	task := f.task(t, "Build page", f.rook, store.StatusInProgress)
	c := f.workerCaller(f.rook, task.ID)
	f.tm.ReportDone(c, "", "done")
	out, err := f.tm.LeaveSteps(c, "", []string{"Set .env", "Restart"})
	if err != nil || !strings.Contains(out, "2 steps") {
		t.Fatalf("%q %v", out, err)
	}
	l, _ := f.st.ListTodos(task.ID)
	if len(l) != 2 || l[0].Kind != "step" {
		t.Fatalf("%+v", l)
	}
}

// task decis de un lider: intrebarile ajung la el in nota de raport
func TestAskDecisionsReachesLeader(t *testing.T) {
	f := setup(t)
	task := f.task(t, "Build page", f.rook, store.StatusInProgress)
	c := f.workerCaller(f.rook, task.ID)
	f.tm.ReportDone(c, "", "done")
	if _, err := f.tm.AskDecisions(c, "", twoQuestions()); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Decisions for the user:", "Open a follow-up?", "Yes / No", "A / B / C"} {
		if !f.hasMessage(f.lead.ID, want) {
			t.Fatalf("liderul nu a primit %q", want)
		}
	}
}

// task creat din board: decide utilizatorul, liderul nu primeste nimic
func TestAskDecisionsUserDecidesNoNote(t *testing.T) {
	f := setup(t)
	task := store.Task{ProjectID: f.p.ID, Title: "Build page", AssigneeID: f.rook.ID, ByUser: true, Status: store.StatusInProgress}
	if err := f.st.CreateTask(&task); err != nil {
		t.Fatal(err)
	}
	task.Status = store.StatusInProgress
	f.st.SaveTask(&task)
	c := f.workerCaller(f.rook, task.ID)
	f.tm.ReportDone(c, "", "done")
	if _, err := f.tm.AskDecisions(c, "", twoQuestions()); err != nil {
		t.Fatal(err)
	}
	if f.hasMessage(f.lead.ID, "Decisions for the user:") {
		t.Fatal("liderul a primit intrebari desi decide utilizatorul")
	}
}
