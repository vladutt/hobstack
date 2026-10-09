package store

import (
	"errors"
	"testing"
	"time"
)

func TestTaskNumbersPerProject(t *testing.T) {
	s := newStore(t)
	p1, _ := newProject(t, s)
	p2, _ := newProject(t, s)
	w1 := newWorker(t, s, p1.ID, "Rook")
	w2 := newWorker(t, s, p2.ID, "Rook")

	var got []int
	for _, x := range []struct {
		pid, aid int64
	}{{p1.ID, w1.ID}, {p1.ID, w1.ID}, {p2.ID, w2.ID}} {
		task := Task{ProjectID: x.pid, Title: "t", AssigneeID: x.aid}
		if err := s.CreateTask(&task); err != nil {
			t.Fatal(err)
		}
		if task.Status != StatusQueued {
			t.Fatalf("status initial = %q", task.Status)
		}
		got = append(got, task.Number)
	}
	if got[0] != 1 || got[1] != 2 || got[2] != 1 {
		t.Fatalf("numere = %v, vreau [1 2 1]", got)
	}
	task, err := s.GetTaskByNumber(p1.ID, 2)
	if err != nil || task.Ref() != "T-2" {
		t.Fatalf("GetTaskByNumber = %+v, %v", task, err)
	}
	if _, err := s.GetTaskByNumber(p1.ID, 9); !errors.Is(err, ErrNotFound) {
		t.Fatalf("task inexistent: %v", err)
	}
}

func TestSaveTaskAndRequeue(t *testing.T) {
	s := newStore(t)
	p, _ := newProject(t, s)
	w := newWorker(t, s, p.ID, "Rook")
	a := Task{ProjectID: p.ID, Title: "A", AssigneeID: w.ID}
	b := Task{ProjectID: p.ID, Title: "B", AssigneeID: w.ID}
	s.CreateTask(&a)
	s.CreateTask(&b)

	a.Status, a.Result, a.Feedback, a.SendBacks = StatusInProgress, "r", "f", 2
	if err := s.SaveTask(&a); err != nil {
		t.Fatal(err)
	}
	b.Status = StatusReview
	s.SaveTask(&b)

	if _, err := s.RequeueInProgress(p.ID); err != nil {
		t.Fatal(err)
	}
	gotA, _ := s.GetTask(a.ID)
	gotB, _ := s.GetTask(b.ID)
	if gotA.Status != StatusQueued || gotA.Result != "r" || gotA.Feedback != "f" || gotA.SendBacks != 2 {
		t.Fatalf("A dupa requeue: %+v", gotA)
	}
	if gotB.Status != StatusReview {
		t.Fatalf("B nu trebuia atins: %+v", gotB)
	}
	tasks, _ := s.ListTasks(p.ID)
	if len(tasks) != 2 || tasks[0].Title != "A" {
		t.Fatalf("ListTasks = %+v", tasks)
	}
}

func TestDeleteAgentWithTasksRejected(t *testing.T) {
	s := newStore(t)
	p, _ := newProject(t, s)
	w := newWorker(t, s, p.ID, "Rook")
	s.CreateTask(&Task{ProjectID: p.ID, Title: "A", AssigneeID: w.ID})
	if err := s.DeleteAgent(w.ID); !errors.Is(err, ErrConflict) {
		t.Fatalf("stergere worker cu task-uri: %v", err)
	}
	free := newWorker(t, s, p.ID, "Vera")
	if err := s.DeleteAgent(free.ID); err != nil {
		t.Fatalf("stergere worker liber: %v", err)
	}
}

func TestRunsLifecycle(t *testing.T) {
	s := newStore(t)
	p, lead := newProject(t, s)
	w := newWorker(t, s, p.ID, "Rook")
	task := Task{ProjectID: p.ID, Title: "A", AssigneeID: w.ID}
	s.CreateTask(&task)

	r1 := Run{ID: "r1", AgentID: w.ID, TaskID: task.ID}
	r2 := Run{ID: "r2", AgentID: lead.ID}
	if err := s.CreateRun(&r1); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateRun(&r2); err != nil {
		t.Fatal(err)
	}
	if err := s.FinishRun("r1", RunFailed, 0.75, "buget depasit"); err != nil {
		t.Fatal(err)
	}
	if err := s.InterruptRunning(); err != nil {
		t.Fatal(err)
	}
	got1, _ := s.GetRun("r1")
	got2, _ := s.GetRun("r2")
	if got1.Status != RunFailed || got1.CostUSD != 0.75 || got1.Error != "buget depasit" || got1.EndedAt == nil || got1.TaskID != task.ID {
		t.Fatalf("r1 = %+v", got1)
	}
	if got2.Status != RunInterrupted || got2.TaskID != 0 {
		t.Fatalf("r2 = %+v", got2)
	}
	if err := s.FinishRun("nu-exista", RunDone, 0, ""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("run inexistent: %v", err)
	}
}

func TestMessagesDelivery(t *testing.T) {
	s := newStore(t)
	p, lead := newProject(t, s)
	m1 := Message{ProjectID: p.ID, AgentID: lead.ID, Kind: KindMsg, Author: AuthorUser, Text: "salut", Delivered: false}
	m2 := Message{ProjectID: p.ID, AgentID: lead.ID, Kind: KindEvent, Text: "info", Delivered: true}
	m3 := Message{ProjectID: p.ID, AgentID: lead.ID, Kind: KindEvent, Text: "Rook a terminat", Delivered: false, RunID: "r1"}
	for _, m := range []*Message{&m1, &m2, &m3} {
		if err := s.AddMessage(m); err != nil {
			t.Fatal(err)
		}
	}
	pending, err := s.Undelivered(lead.ID)
	if err != nil || len(pending) != 2 || pending[0].ID != m1.ID || pending[1].RunID != "r1" {
		t.Fatalf("Undelivered = %+v, %v", pending, err)
	}
	if has, _ := s.HasUndelivered(lead.ID); !has {
		t.Fatal("HasUndelivered = false")
	}
	if err := s.MarkDelivered([]int64{m1.ID, m3.ID}); err != nil {
		t.Fatal(err)
	}
	if has, _ := s.HasUndelivered(lead.ID); has {
		t.Fatal("HasUndelivered = true dupa MarkDelivered")
	}
	all, _ := s.ListMessages(lead.ID)
	if len(all) != 3 || all[0].Text != "salut" || !all[0].Delivered {
		t.Fatalf("ListMessages = %+v", all)
	}
}

func TestDeleteProjectCascades(t *testing.T) {
	s := newStore(t)
	p, lead := newProject(t, s)
	w := newWorker(t, s, p.ID, "Rook")
	task := Task{ProjectID: p.ID, Title: "A", AssigneeID: w.ID}
	s.CreateTask(&task)
	s.CreateRun(&Run{ID: "r1", AgentID: w.ID, TaskID: task.ID})
	s.AddMessage(&Message{ProjectID: p.ID, AgentID: lead.ID, Kind: KindMsg, Author: AuthorUser, Text: "x"})

	if err := s.DeleteProject(p.ID); err != nil {
		t.Fatalf("stergere cu cascada: %v", err)
	}
	for _, table := range []string{"agents", "tasks", "runs", "messages"} {
		var n int
		s.db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&n)
		if n != 0 {
			t.Errorf("%s mai are %d randuri", table, n)
		}
	}
}

func TestRequeueKeepsWaitingParent(t *testing.T) {
	s := newStore(t)
	p, lead := newProject(t, s)
	sub := newAgent(t, s, p.ID, "Sub", &lead.ID)
	w := newAgent(t, s, p.ID, "W", &sub.ID)
	parent := Task{ProjectID: p.ID, Title: "Import", AssigneeID: sub.ID, CreatorID: &lead.ID}
	s.CreateTask(&parent)
	parent.Status = StatusInProgress
	s.SaveTask(&parent)
	child := Task{ProjectID: p.ID, Title: "Parser", AssigneeID: w.ID, CreatorID: &sub.ID, ParentTaskID: &parent.ID}
	s.CreateTask(&child)
	child.Status, child.ReviewStage = StatusInProgress, StageNone
	s.SaveTask(&child)

	subs, err := s.Subtasks(parent.ID)
	if err != nil || len(subs) != 1 || subs[0].ID != child.ID || *subs[0].CreatorID != sub.ID {
		t.Fatalf("Subtasks = %+v, %v", subs, err)
	}
	if _, err := s.RequeueInProgress(p.ID); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.GetTask(parent.ID); got.Status != StatusInProgress {
		t.Fatalf("parintele care asteapta a fost mutat in %s", got.Status)
	}
	if got, _ := s.GetTask(child.ID); got.Status != StatusQueued {
		t.Fatalf("sub-task-ul trebuia in queued: %s", got.Status)
	}
	child.Status = StatusDone
	s.SaveTask(&child)
	if _, err := s.RequeueAllInProgress(); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.GetTask(parent.ID); got.Status != StatusQueued {
		t.Fatalf("fara sub-task-uri deschise, parintele revine in queued: %s", got.Status)
	}
	if subs, _ := s.Subtasks(child.ID); subs == nil || len(subs) != 0 {
		t.Fatal("Subtasks trebuie sa intoarca [] nu nil")
	}
}

func TestReviewStageSaved(t *testing.T) {
	s := newStore(t)
	p, _ := newProject(t, s)
	w := newWorker(t, s, p.ID, "Rook")
	task := Task{ProjectID: p.ID, Title: "A", AssigneeID: w.ID}
	s.CreateTask(&task)
	if task.ReviewStage != StageNone || task.CreatorID != nil || task.ParentTaskID != nil {
		t.Fatalf("task nou: %+v", task)
	}
	task.Status, task.ReviewStage = StatusReview, StageReviewer
	s.SaveTask(&task)
	if got, _ := s.GetTask(task.ID); got.ReviewStage != StageReviewer {
		t.Fatalf("review_stage = %q", got.ReviewStage)
	}
}

func TestTaskNudgedAt(t *testing.T) {
	s := newStore(t)
	p, lead := newProject(t, s)
	task := Task{ProjectID: p.ID, Title: "A", AssigneeID: lead.ID}
	if err := s.CreateTask(&task); err != nil {
		t.Fatal(err)
	}
	if task.NudgedAt != nil {
		t.Fatal("un task nou nu e impins")
	}
	at := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	if err := s.SetTaskNudged(task.ID, at); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetTask(task.ID)
	if got.NudgedAt == nil || !got.NudgedAt.Equal(at) {
		t.Fatalf("%v", got.NudgedAt)
	}
	got.Status = StatusReview
	if err := s.SaveTask(&got); err != nil {
		t.Fatal(err)
	}
	if again, _ := s.GetTask(task.ID); again.NudgedAt != nil {
		t.Fatal("SaveTask trebuia sa curete nudged_at")
	}
	if err := s.SetTaskNudged(9999, at); err == nil {
		t.Fatal("task inexistent: vreau eroare")
	}
}

// repunerea in coada (Stop team, pornirea serverului) e o schimbare de stare: curata nudged_at
func TestRequeueClearsNudgedAt(t *testing.T) {
	s := newStore(t)
	p, lead := newProject(t, s)
	at := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	for _, requeue := range []func() error{
		func() error { _, err := s.RequeueInProgress(p.ID); return err },
		func() error { _, err := s.RequeueAllInProgress(); return err },
	} {
		task := Task{ProjectID: p.ID, Title: "A", AssigneeID: lead.ID}
		if err := s.CreateTask(&task); err != nil {
			t.Fatal(err)
		}
		task.Status = StatusInProgress
		if err := s.SaveTask(&task); err != nil {
			t.Fatal(err)
		}
		if err := s.SetTaskNudged(task.ID, at); err != nil {
			t.Fatal(err)
		}
		if err := requeue(); err != nil {
			t.Fatal(err)
		}
		got, _ := s.GetTask(task.ID)
		if got.Status != StatusQueued || got.NudgedAt != nil {
			t.Fatalf("%s %v", got.Status, got.NudgedAt)
		}
	}
}

// thread_id: firul de lucru se salveaza la creare, se citeste inapoi si devine
// NULL cand task-ul care a pornit firul e sters.
func TestTaskThreadID(t *testing.T) {
	s := newStore(t)
	p, _ := newProject(t, s)
	w := newWorker(t, s, p.ID, "Rook")
	start := Task{ProjectID: p.ID, Title: "Audit", AssigneeID: w.ID}
	if err := s.CreateTask(&start); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.GetTask(start.ID); got.ThreadID != nil {
		t.Fatalf("task fara fir: %v", *got.ThreadID)
	}
	next := Task{ProjectID: p.ID, Title: "Fix", AssigneeID: w.ID, ThreadID: &start.ID}
	if err := s.CreateTask(&next); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetTask(next.ID)
	if err != nil || got.ThreadID == nil || *got.ThreadID != start.ID {
		t.Fatalf("thread_id = %v, %v", got.ThreadID, err)
	}
	if list, _ := s.ListTasks(p.ID); len(list) != 2 || list[1].ThreadID == nil || *list[1].ThreadID != start.ID {
		t.Fatalf("ListTasks: %+v", list)
	}
	if _, err := s.db.Exec(`DELETE FROM tasks WHERE id = ?`, start.ID); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.GetTask(next.ID); got.ThreadID != nil {
		t.Fatalf("dupa stergere thread_id = %v", *got.ThreadID)
	}
}

func TestCountSelfStarted(t *testing.T) {
	s := newStore(t)
	p, lead := newProject(t, s)
	for i, kind := range []string{"self_started", "assigned", "self_started"} {
		task := Task{ProjectID: p.ID, Title: "t", AssigneeID: lead.ID}
		s.CreateTask(&task)
		s.AddTaskEvent(&TaskEvent{TaskID: task.ID, Kind: kind, Text: "x"})
		if i == 2 { // al treilea e Done: nu mai conteaza
			task.Status = StatusDone
			s.SaveTask(&task)
		}
	}
	if n, err := s.CountSelfStarted(lead.ID); err != nil || n != 1 {
		t.Fatalf("%d %v", n, err)
	}
}
