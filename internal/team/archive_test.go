package team

import (
	"errors"
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
	// decidentul primeste nota in coada (nelivrata): o citeste la urmatorul tur
	msgs, _ := f.st.ListMessages(f.lead.ID)
	if last := msgs[len(msgs)-1]; !strings.Contains(last.Text, "The user deleted "+sub.Ref()) || last.Delivered {
		t.Fatalf("nota decidentului trebuia sa fie in coada: %+v", last)
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

func TestArchivedCurrentTaskRejectedByTools(t *testing.T) {
	f := setup(t)
	task := f.task(t, "Build page", f.rook, store.StatusInProgress)
	if _, err := f.tm.ArchiveTask(task.ID); err != nil {
		t.Fatal(err)
	}
	c := f.workerCaller(f.rook, task.ID)
	want := task.Ref() + " was deleted by the user"
	if _, err := f.tm.AskUser(c, "Which color?", nil); err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("ask_user: %v", err)
	}
	if qs, _ := f.st.ListOpenQuestions(f.p.ID); len(qs) != 0 {
		t.Fatalf("intrebare orfana: %+v", qs)
	}
	if _, err := f.tm.LeaveForUser(c, "", []string{"check it"}); err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("leave_for_user: %v", err)
	}
	if _, err := f.tm.TickLeftForUser(c, "", []int{1}); err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("tick_left_for_user: %v", err)
	}
	if _, _, err := f.tm.parentTaskFor(c); err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("parentTaskFor: %v", err)
	}
}

// un task Done sters e doar curatenie: liderul afla, dar nota nu porneste un run
func TestArchiveDoneSubtaskNoteIsDelivered(t *testing.T) {
	f := setup(t)
	sub := store.Task{ProjectID: f.p.ID, Title: "Sub", AssigneeID: f.rook.ID, CreatorID: &f.lead.ID}
	if err := f.st.CreateTask(&sub); err != nil {
		t.Fatal(err)
	}
	sub.Status = store.StatusDone
	if err := f.st.SaveTask(&sub); err != nil {
		t.Fatal(err)
	}
	if _, err := f.tm.ArchiveTask(sub.ID); err != nil {
		t.Fatal(err)
	}
	if !f.hasMessage(f.lead.ID, "The user deleted "+sub.Ref()) {
		t.Fatal("liderul nu a fost anuntat")
	}
	if pending, _ := f.st.Undelivered(f.lead.ID); len(pending) != 0 {
		t.Fatalf("nota pentru un task Done nu trebuia pusa in coada: %+v", pending)
	}
}

// un task sters nu mai primeste comentarii si bife (agentul ar porni degeaba)
func TestArchivedTaskRejectsCommentAndTodo(t *testing.T) {
	f := setup(t)
	task := f.task(t, "Terms page", f.rook, store.StatusDone)
	f.st.AddTodos(task.ID, []string{"VAT"}, "value")
	if _, err := f.tm.ArchiveTask(task.ID); err != nil {
		t.Fatal(err)
	}
	before, _ := f.st.Undelivered(f.rook.ID)
	if err := f.tm.UserComment(task.ID, "Company is Meridian."); !errors.Is(err, ErrArchived) || !strings.Contains(err.Error(), task.Ref()+" was deleted; restore it first") {
		t.Fatalf("comentariu pe sters: %v", err)
	}
	if err := f.tm.UserSetTodo(task.ID, 1, true); !errors.Is(err, ErrArchived) {
		t.Fatalf("bifa pe sters: %v", err)
	}
	if after, _ := f.st.Undelivered(f.rook.ID); len(after) != len(before) {
		t.Fatalf("agentul a primit mesaj: %+v", after)
	}
	if todos, _ := f.st.ListTodos(task.ID); len(todos) != 1 || todos[0].DoneAt != nil {
		t.Fatalf("bifa aplicata: %+v", todos)
	}
}
