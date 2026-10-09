package team

import (
	"errors"
	"strings"
	"testing"

	"foreman/internal/store"
)

func TestMoveTable(t *testing.T) {
	f := setup(t)

	// Queued -> Done = Close
	q := f.task(t, "Close me", f.rook, store.StatusQueued)
	if err := f.tm.MoveTask(q.ID, "done", ""); err != nil {
		t.Fatal(err)
	}
	if got, _ := f.st.GetTask(q.ID); got.Status != store.StatusDone || !strings.Contains(got.Result, "Closed by the user") {
		t.Fatalf("close: %+v", got)
	}

	// Done -> Queued = Reopen (motiv obligatoriu)
	if err := f.tm.MoveTask(q.ID, "queued", " "); !errors.Is(err, ErrReasonRequired) {
		t.Fatalf("reopen fara motiv: %v", err)
	}
	if err := f.tm.MoveTask(q.ID, "queued", "Missing the footer"); err != nil {
		t.Fatal(err)
	}
	got, _ := f.st.GetTask(q.ID)
	if got.Status != store.StatusQueued || got.Feedback != "Missing the footer" || got.AssigneeID != f.rook.ID {
		t.Fatalf("reopen: %+v", got)
	}
	if !f.hasMessage(f.rook.ID, "The user reopened "+q.Ref()+": Missing the footer") {
		t.Fatal("asignatul n-a primit motivul redeschiderii")
	}

	// In progress: refuzat in ambele directii
	ip := f.task(t, "Busy", f.rook, store.StatusInProgress)
	for _, to := range []string{"done", "queued"} {
		var me MoveError
		if err := f.tm.MoveTask(ip.ID, to, "x"); !errors.As(err, &me) {
			t.Fatalf("%s: %v", to, err)
		}
	}

	// Review decis de lider: utilizatorul trece peste el
	rv := f.task(t, "Review me", f.rook, store.StatusReview)
	if err := f.tm.MoveTask(rv.ID, "done", ""); err != nil {
		t.Fatal(err)
	}
	if got, _ := f.st.GetTask(rv.ID); got.Status != store.StatusDone {
		t.Fatalf("accept peste lider: %s", got.Status)
	}
	events, _ := f.st.ListTaskEvents(rv.ID)
	if last := events[len(events)-1]; !strings.Contains(last.Text, "instead of "+f.lead.Name) {
		t.Fatalf("eveniment: %+v", last)
	}
	// liderul afla ca utilizatorul a decis in locul lui
	if !f.hasMessage(f.lead.ID, "The user decided "+rv.Ref()+" for you: accepted.") {
		t.Fatal("liderul n-a primit nota deciziei")
	}

	// Review -> Queued = Send back, motiv obligatoriu
	rv2 := f.task(t, "Send me back", f.rook, store.StatusReview)
	if err := f.tm.MoveTask(rv2.ID, "queued", ""); !errors.Is(err, ErrReasonRequired) {
		t.Fatalf("send back fara motiv: %v", err)
	}
	if err := f.tm.MoveTask(rv2.ID, "queued", "Use the brand colors"); err != nil {
		t.Fatal(err)
	}
	if got, _ := f.st.GetTask(rv2.ID); got.Status != store.StatusQueued || got.Feedback != "Use the brand colors" {
		t.Fatalf("send back: %+v", got)
	}

	// Blocked -> Queued = Unblock; Blocked -> Done = Accept
	b1 := f.task(t, "Blocked 1", f.rook, store.StatusBlocked)
	if err := f.tm.MoveTask(b1.ID, "queued", ""); err != nil {
		t.Fatal(err)
	}
	if got, _ := f.st.GetTask(b1.ID); got.Status != store.StatusQueued {
		t.Fatalf("unblock: %s", got.Status)
	}
	b2 := f.task(t, "Blocked 2", f.rook, store.StatusBlocked)
	if err := f.tm.MoveTask(b2.ID, "done", ""); err != nil {
		t.Fatal(err)
	}
	if got, _ := f.st.GetTask(b2.ID); got.Status != store.StatusDone {
		t.Fatalf("accept blocat: %s", got.Status)
	}

	// tinta necunoscuta, task sters
	var me MoveError
	if err := f.tm.MoveTask(b2.ID, "review", ""); !errors.As(err, &me) {
		t.Fatalf("tinta review: %v", err)
	}
	if _, err := f.tm.ArchiveTask(q.ID); err != nil {
		t.Fatal(err)
	}
	if err := f.tm.MoveTask(q.ID, "done", ""); !errors.As(err, &me) {
		t.Fatalf("mutare pe sters: %v", err)
	}
}

func TestMoveBetweenStages(t *testing.T) {
	f := setup(t)
	stages, err := f.st.SaveStages(f.p.ID, []store.Stage{
		{Name: "Build", Kind: store.KindWork, AgentIDs: []int64{f.rook.ID}},
		{Name: "QA", Kind: store.KindCheck, AgentIDs: []int64{f.vera.ID}},
	})
	if err != nil {
		t.Fatal(err)
	}
	task := store.Task{ProjectID: f.p.ID, Title: "Page", AssigneeID: f.rook.ID, CreatorID: &f.lead.ID, StageID: &stages[0].ID}
	if err := f.st.CreateTask(&task); err != nil {
		t.Fatal(err)
	}
	if err := f.tm.MoveTask(task.ID, "stage:"+itoa(stages[1].ID), ""); err != nil {
		t.Fatal(err)
	}
	got, _ := f.st.GetTask(task.ID)
	if got.StageID == nil || *got.StageID != stages[1].ID || got.Status != store.StatusQueued {
		t.Fatalf("%+v", got)
	}
	var me MoveError
	if err := f.tm.MoveTask(task.ID, "stage:"+itoa(stages[1].ID), ""); !errors.As(err, &me) {
		t.Fatalf("aceeasi etapa: %v", err)
	}
	if err := f.tm.MoveTask(task.ID, "stage:999999", ""); !errors.As(err, &me) {
		t.Fatalf("etapa inexistenta: %v", err)
	}
}
