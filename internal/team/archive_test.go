package team

import (
	"strings"
	"testing"
	"time"

	"foreman/internal/runner"
	"foreman/internal/schedule"
	"foreman/internal/store"
)

func TestArchiveStopsRunAndRestores(t *testing.T) {
	f := setup(t)
	task := f.task(t, "Build page", f.rook, store.StatusQueued)
	mustReconcile(t, f)
	run := f.fs.last(t, f.rook)

	n, err := f.tm.ArchiveTask(task.ID)
	if err != nil || n != 1 {
		t.Fatalf("%d %v", n, err)
	}
	if len(f.fs.stopped) != 1 || f.fs.stopped[0] != run.ID {
		t.Fatalf("run neoprit: %v", f.fs.stopped)
	}
	// procesul se termina dupa Stop: finish nu readuce task-ul
	f.fs.finish(run.ID, runner.Outcome{Status: store.RunStopped})
	if got, _ := f.st.GetTask(task.ID); got.Status != store.StatusArchived {
		t.Fatalf("finish a scos task-ul din arhiva: %s", got.Status)
	}
	if !f.hasMessage(f.rook.ID, "The user deleted "+task.Ref()) {
		t.Fatal("asignatul nu a fost anuntat")
	}
	// tool-urile MCP vad task-ul ca sters
	if _, err := f.tm.ReportDone(f.workerCaller(f.rook, 0), task.Ref(), "done"); err == nil || !strings.Contains(err.Error(), "deleted") {
		t.Fatalf("report_done pe sters: %v", err)
	}

	if err := f.tm.RestoreTask(task.ID); err != nil {
		t.Fatal(err)
	}
	if got, _ := f.st.GetTask(task.ID); got.Status != store.StatusQueued {
		t.Fatalf("restore: %s", got.Status)
	}
	events, _ := f.st.ListTaskEvents(task.ID)
	kinds := []string{}
	for _, e := range events {
		kinds = append(kinds, e.Kind)
	}
	if s := strings.Join(kinds, ","); !strings.Contains(s, "deleted") || !strings.Contains(s, "restored") {
		t.Fatalf("istoric: %s", s)
	}
}

func TestArchiveSubtaskUnblocksParent(t *testing.T) {
	f := setup(t)
	parent := f.task(t, "Parent", f.lead, store.StatusInProgress)
	sub := store.Task{ProjectID: f.p.ID, Title: "Sub", AssigneeID: f.rook.ID, CreatorID: &f.lead.ID, ParentTaskID: &parent.ID}
	if err := f.st.CreateTask(&sub); err != nil {
		t.Fatal(err)
	}
	if _, err := f.tm.ArchiveTask(sub.ID); err != nil {
		t.Fatal(err)
	}
	if subs, _ := f.st.Subtasks(parent.ID); len(subs) != 0 {
		t.Fatalf("sub-task-ul sters inca numarat: %+v", subs)
	}
	if !f.hasMessage(f.lead.ID, "The user deleted "+sub.Ref()) {
		t.Fatal("liderul nu a fost anuntat")
	}
	// parintele se arhiveaza separat; restaurarea sub-task-ului cere intai parintele
	if _, err := f.tm.ArchiveTask(parent.ID); err != nil {
		t.Fatal(err)
	}
	if err := f.tm.RestoreTask(sub.ID); err == nil || !strings.Contains(err.Error(), "Restore "+parent.Ref()+" first") {
		t.Fatalf("%v", err)
	}
}

func TestRecurringNotBlockedByDeletedOccurrence(t *testing.T) {
	f := setup(t)
	r := store.RecurringTask{ProjectID: f.p.ID, Title: "Scout", AssigneeID: &f.rook.ID, Enabled: true,
		Schedule: schedule.Spec{Kind: "every_hours", EveryHours: 1}, NextRunAt: time.Now().Add(time.Hour)}
	if err := f.st.CreateRecurring(&r); err != nil {
		t.Fatal(err)
	}
	first, err := f.tm.RunRecurringNow(r.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.tm.ArchiveTask(first.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.tm.RunRecurringNow(r.ID); err != nil {
		t.Fatalf("aparitia stearsa blocheaza: %v", err)
	}
}
