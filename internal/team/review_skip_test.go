package team

import (
	"testing"

	"hobstack/internal/mcp"
	"hobstack/internal/store"
)

// reviewSkipSetup: Wes are reviewer pe Rev; T-1 a fost raportat (run r1, cu modificari), aprobat de Rev,
// apoi trimis inapoi de lider si e iar In progress. Intoarce fixture-ul, pe Wes si task-ul.
func reviewSkipSetup(t *testing.T, approve bool) (*fixture, store.Agent, store.Task) {
	t.Helper()
	f := setup(t)
	rev := f.agent(t, "Rev", &f.lead, nil)
	wes := f.agent(t, "Wes", &f.lead, &rev)
	f.tm.CreateTask(f.leadCaller(), "A", "", "Wes", "")
	task := inProgress(t, f, f.wantStatus(t, 1, store.StatusQueued))
	f.fs.setActivity("r1", 3)
	c := f.workerCaller(wes, task.ID)
	c.RunID = "r1"
	if _, err := f.tm.ReportDone(c, "", "gata"); err != nil {
		t.Fatal(err)
	}
	rc := mcp.Caller{ProjectID: f.p.ID, AgentID: rev.ID, HasParent: true, IsReviewer: true}
	if approve {
		if _, err := f.tm.ReviewApprove(rc, "T-1", ""); err != nil {
			t.Fatal(err)
		}
		if _, err := f.tm.SendBack(f.leadCaller(), "T-1", "is it done?"); err != nil {
			t.Fatal(err)
		}
	} else if _, err := f.tm.ReviewReject(rc, "T-1", "missing tests"); err != nil {
		t.Fatal(err)
	}
	return f, wes, inProgress(t, f, task)
}

func reportIn(t *testing.T, f *fixture, wes store.Agent, task store.Task, runID string) store.Task {
	t.Helper()
	c := f.workerCaller(wes, task.ID)
	c.RunID = runID
	if _, err := f.tm.ReportDone(c, "", "nothing to change"); err != nil {
		t.Fatal(err)
	}
	got, _ := f.st.GetTask(task.ID)
	return got
}

// run fara nicio modificare dupa o aprobare a reviewer-ului: raportul merge direct la decident
func TestReviewSkippedWhenNothingChanged(t *testing.T) {
	f, wes, task := reviewSkipSetup(t, true)
	f.fs.setActivity("r2", 0)
	got := reportIn(t, f, wes, task, "r2")
	if got.Status != store.StatusReview || got.ReviewStage != store.StageLead {
		t.Fatalf("vreau Review la lider, am %s / %s", got.Status, got.ReviewStage)
	}
	evs, _ := f.st.ListTaskEvents(task.ID)
	var last store.TaskEvent
	for _, e := range evs {
		if e.Kind == "reported" {
			last = e
		}
	}
	if last.To != "Lead" || last.Body != "nothing to change" {
		t.Fatalf("ultimul raport: %+v", last)
	}
	wantEvent(t, f, task.ID, "review_skipped", actorHobStack, "Review skipped: no changes since Rev approved it")
}

// cu modificari in run-ul nou: tot la reviewer
func TestReviewNotSkippedWithChanges(t *testing.T) {
	f, wes, task := reviewSkipSetup(t, true)
	f.fs.setActivity("r2", 1)
	if got := reportIn(t, f, wes, task, "r2"); got.ReviewStage != store.StageReviewer {
		t.Fatalf("vreau etapa reviewer, am %s", got.ReviewStage)
	}
}

// ultima decizie a reviewer-ului a fost o respingere: un raport fara modificari nu-l ocoleste
func TestReviewNotSkippedAfterReject(t *testing.T) {
	f, wes, task := reviewSkipSetup(t, false)
	f.fs.setActivity("r2", 0)
	if got := reportIn(t, f, wes, task, "r2"); got.ReviewStage != store.StageReviewer {
		t.Fatalf("vreau etapa reviewer, am %s", got.ReviewStage)
	}
}

// primul raport, fara modificari si fara aprobare inainte: reviewer-ul il vede
func TestReviewNotSkippedOnFirstReport(t *testing.T) {
	f := setup(t)
	rev := f.agent(t, "Rev", &f.lead, nil)
	wes := f.agent(t, "Wes", &f.lead, &rev)
	f.tm.CreateTask(f.leadCaller(), "A", "", "Wes", "")
	task := inProgress(t, f, f.wantStatus(t, 1, store.StatusQueued))
	f.fs.setActivity("r1", 0)
	if got := reportIn(t, f, wes, task, "r1"); got.ReviewStage != store.StageReviewer {
		t.Fatalf("vreau etapa reviewer, am %s", got.ReviewStage)
	}
}

// activitatea run-ului necunoscuta (run-ul nu mai e in runner): tot la reviewer
func TestReviewNotSkippedWhenActivityUnknown(t *testing.T) {
	f, wes, task := reviewSkipSetup(t, true)
	if got := reportIn(t, f, wes, task, "r-unknown"); got.ReviewStage != store.StageReviewer {
		t.Fatalf("vreau etapa reviewer, am %s", got.ReviewStage)
	}
}
