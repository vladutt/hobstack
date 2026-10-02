package store

import (
	"reflect"
	"testing"
)

func TestTaskChangesAddListCountCascade(t *testing.T) {
	s := newStore(t)
	p, _ := newProject(t, s)
	w := newWorker(t, s, p.ID, "Rook")
	other := newWorker(t, s, p.ID, "Vera")
	task := Task{ProjectID: p.ID, Title: "t", AssigneeID: other.ID}
	if err := s.CreateTask(&task); err != nil {
		t.Fatal(err)
	}
	if cs, err := s.ListTaskChanges(task.ID); err != nil || cs == nil || len(cs) != 0 {
		t.Fatalf("lista goala trebuie sa fie non-nil: %v %v", cs, err)
	}
	hunks := []Hunk{{OldStart: 1, OldLines: 1, NewStart: 1, NewLines: 2, Lines: []string{"-a", "+b", "+c"}}}
	in := []TaskChange{
		{TaskID: task.ID, RunID: "r1", AgentID: &w.ID, AgentName: "Rook", Kind: "edit", Tool: "Edit", Path: "a.go", Patch: hunks, Truncated: true},
		{TaskID: task.ID, RunID: "r1", AgentID: &w.ID, AgentName: "Rook", Kind: "edit", Tool: "Edit", Path: "a.go"},
		{TaskID: task.ID, RunID: "r1", AgentID: &w.ID, AgentName: "Rook", Kind: "create", Tool: "Write", Path: "b.go"},
		{TaskID: task.ID, RunID: "r1", AgentID: &w.ID, AgentName: "Rook", Kind: "command", Tool: "Bash", Command: "go test", OK: true},
		{TaskID: task.ID, RunID: "r1", AgentID: &w.ID, AgentName: "Rook", Kind: "command", Tool: "Bash", Command: "false"},
	}
	if err := s.AddTaskChanges(in); err != nil {
		t.Fatal(err)
	}
	got, err := s.ListTaskChanges(task.ID)
	if err != nil || len(got) != 5 {
		t.Fatalf("%+v %v", got, err)
	}
	if got[0].ID == 0 || got[0].At.IsZero() || !reflect.DeepEqual(got[0].Patch, hunks) || !got[0].Truncated || got[0].Path != "a.go" {
		t.Fatalf("patch round-trip: %+v", got[0])
	}
	if got[3].Kind != "command" || got[3].Command != "go test" || got[3].Patch != nil || !got[3].OK || got[4].OK || !got[0].OK {
		t.Fatalf("comanda: %+v", got[3])
	}

	if n, err := s.CountTaskChanges(task.ID); err != nil || n != 5 {
		t.Fatalf("CountTaskChanges = %d %v", n, err)
	}
	if n, err := s.CountTaskChanges(task.ID + 999); err != nil || n != 0 {
		t.Fatalf("CountTaskChanges fara randuri = %d %v", n, err)
	}

	// alt proiect cu un task care are si el modificari: nu se amesteca
	p2, _ := newProject(t, s)
	w2 := newWorker(t, s, p2.ID, "Rook")
	task2 := Task{ProjectID: p2.ID, Title: "t2", AssigneeID: w2.ID}
	if err := s.CreateTask(&task2); err != nil {
		t.Fatal(err)
	}
	if err := s.AddTaskChanges([]TaskChange{{TaskID: task2.ID, RunID: "r2", Kind: "edit", Tool: "Edit", Path: "x.go"}}); err != nil {
		t.Fatal(err)
	}
	counts, err := s.CountChangedFiles(p.ID)
	if err != nil || len(counts) != 1 || counts[task.ID] != 2 {
		t.Fatalf("count: %v %v", counts, err)
	}

	// agent sters: AgentID nil, numele ramane
	if _, err := s.db.Exec(`DELETE FROM agents WHERE id = ?`, w.ID); err != nil {
		t.Fatal(err)
	}
	got, _ = s.ListTaskChanges(task.ID)
	if got[0].AgentID != nil || got[0].AgentName != "Rook" {
		t.Fatalf("agent sters: %+v", got[0])
	}

	// stergerea task-ului sterge modificarile
	if _, err := s.db.Exec(`DELETE FROM tasks WHERE id = ?`, task.ID); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.ListTaskChanges(task.ID); len(got) != 0 {
		t.Fatalf("cascade: %+v", got)
	}
}
