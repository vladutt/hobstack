package store

import (
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func flowFixture(t *testing.T) (*Store, Project, Agent, Agent) {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	p := Project{Name: "P", Workdir: t.TempDir(), MaxParallel: 1}
	lead, err := s.CreateProject(&p)
	if err != nil {
		t.Fatal(err)
	}
	dev := Agent{ProjectID: p.ID, Name: "Dev", ParentID: &lead.ID, Model: "haiku"}
	if err := s.CreateAgent(&dev); err != nil {
		t.Fatal(err)
	}
	return s, p, lead, dev
}

func mkTask(t *testing.T, s *Store, p Project, who Agent, title string, parent *Task) Task {
	t.Helper()
	task := Task{ProjectID: p.ID, Title: title, AssigneeID: who.ID}
	if parent != nil {
		task.ParentTaskID = &parent.ID
	}
	if err := s.CreateTask(&task); err != nil {
		t.Fatal(err)
	}
	return task
}

func event(t *testing.T, s *Store, task Task, at time.Time, kind, actor, to, text, body string) {
	t.Helper()
	if err := s.AddTaskEvent(&TaskEvent{TaskID: task.ID, At: at, Kind: kind, Actor: actor, To: to, Text: text, Body: body}); err != nil {
		t.Fatal(err)
	}
}

func TestTaskFlowFromSubtask(t *testing.T) {
	s, p, lead, dev := flowFixture(t)
	t0 := time.Date(2026, 10, 3, 9, 0, 0, 0, time.UTC)
	root := mkTask(t, s, p, lead, "Root", nil)
	mid := mkTask(t, s, p, dev, "Mid", &root)
	leaf := mkTask(t, s, p, dev, "Leaf", &mid)
	other := mkTask(t, s, p, dev, "Other", &root)
	event(t, s, root, t0, "created", "HobStack", lead.Name, "Created by HobStack", "brief")
	event(t, s, mid, t0.Add(time.Minute), "created", lead.Name, "Dev", "Created by "+lead.Name, "")
	event(t, s, leaf, t0.Add(2*time.Minute), "created", "Dev", "Dev", "Created by Dev", "")
	event(t, s, root, t0.Add(10*time.Minute), "accepted", "HobStack", "", "HobStack accepted", "")
	root.Status = StatusDone
	if err := s.SaveTask(&root); err != nil {
		t.Fatal(err)
	}

	f, err := s.TaskFlow(leaf.ID)
	if err != nil {
		t.Fatal(err)
	}
	if f.FocusID != leaf.ID || f.Root.ID != root.ID || f.Root.Assignee != lead.Name {
		t.Fatalf("radacina / focus: %+v", f)
	}
	if len(f.Root.Children) != 2 || f.Root.Children[0].ID != mid.ID || f.Root.Children[1].ID != other.ID {
		t.Fatalf("copiii radacinii in ordinea numarului: %+v", f.Root.Children)
	}
	if kids := f.Root.Children[0].Children; len(kids) != 1 || kids[0].ID != leaf.ID || kids[0].Children == nil {
		t.Fatalf("nepotul: %+v", kids)
	}
	st := f.Root.Steps
	if len(st) != 2 || st[0].Kind != "created" || st[0].To != lead.Name || st[0].Body != "brief" {
		t.Fatalf("pasii radacinii: %+v", st)
	}
	if f.Root.DoneAt == nil || !f.Root.DoneAt.Equal(t0.Add(10*time.Minute)) {
		t.Fatalf("doneAt: %v", f.Root.DoneAt)
	}
	if f.Root.Children[0].DoneAt != nil {
		t.Fatalf("task nefinalizat fara doneAt: %v", f.Root.Children[0].DoneAt)
	}
}

func TestTaskFlowSortsByTime(t *testing.T) {
	s, p, lead, _ := flowFixture(t)
	t0 := time.Date(2026, 10, 3, 9, 0, 0, 0, time.UTC)
	task := mkTask(t, s, p, lead, "T", nil)
	event(t, s, task, t0.Add(time.Minute), "created", lead.Name, "", "Created", "")
	event(t, s, task, t0, "requested", "You", lead.Name, "Asked in chat", "please do it") // salvat dupa, dar mai devreme
	f, err := s.TaskFlow(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Root.Steps) != 2 || f.Root.Steps[0].Kind != "requested" || f.Root.Steps[1].Kind != "created" {
		t.Fatalf("ordinea dupa ora: %+v", f.Root.Steps)
	}
}

func TestTaskFlowWithoutEvents(t *testing.T) {
	s, p, lead, _ := flowFixture(t)
	task := mkTask(t, s, p, lead, "Old", nil)
	f, err := s.TaskFlow(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Root.Steps) != 1 || f.Root.Steps[0].Kind != "created" || f.Root.Steps[0].Text != "Created" || !f.Root.Steps[0].At.Equal(task.CreatedAt) {
		t.Fatalf("pas sintetic: %+v", f.Root.Steps)
	}
	if f.Root.Children == nil {
		t.Fatal("children trebuie sa fie non-nil")
	}
	if _, err := s.TaskFlow(99999); !errors.Is(err, ErrNotFound) {
		t.Fatalf("inexistent: %v", err)
	}
}

func TestTaskFlowCycle(t *testing.T) {
	s, p, lead, dev := flowFixture(t)
	a := mkTask(t, s, p, lead, "A", nil)
	b := mkTask(t, s, p, dev, "B", &a)
	// date stricate: A devine copilul lui B
	if _, err := s.db.Exec(`UPDATE tasks SET parent_task_id = ? WHERE id = ?`, b.ID, a.ID); err != nil {
		t.Fatal(err)
	}
	done := make(chan Flow, 1)
	go func() {
		f, err := s.TaskFlow(a.ID)
		if err != nil {
			t.Error(err)
		}
		done <- f
	}()
	select {
	case f := <-done:
		if f.FocusID != a.ID {
			t.Fatalf("%+v", f)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("TaskFlow a intrat in bucla pe un ciclu")
	}
}
