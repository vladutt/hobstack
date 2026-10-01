package store

import "testing"

func TestTaskEventsAddListCascade(t *testing.T) {
	s := newStore(t)
	p, _ := newProject(t, s)
	w := newWorker(t, s, p.ID, "Rook")
	task := Task{ProjectID: p.ID, Title: "t", AssigneeID: w.ID}
	if err := s.CreateTask(&task); err != nil {
		t.Fatal(err)
	}
	if evs, err := s.ListTaskEvents(task.ID); err != nil || evs == nil || len(evs) != 0 {
		t.Fatalf("lista goala trebuie sa fie non-nil: %v %v", evs, err)
	}
	for _, k := range []string{"created", "assigned"} {
		e := TaskEvent{TaskID: task.ID, Kind: k, Actor: "You", Text: k}
		if err := s.AddTaskEvent(&e); err != nil {
			t.Fatal(err)
		}
		if e.ID == 0 || e.At.IsZero() {
			t.Fatalf("id/at nesetate: %+v", e)
		}
	}
	evs, err := s.ListTaskEvents(task.ID)
	if err != nil || len(evs) != 2 || evs[0].Kind != "created" || evs[1].Kind != "assigned" || evs[0].TaskID != task.ID {
		t.Fatalf("evenimente: %+v %v", evs, err)
	}

	// stergerea task-ului sterge istoricul
	if _, err := s.db.Exec(`DELETE FROM tasks WHERE id = ?`, task.ID); err != nil {
		t.Fatal(err)
	}
	if evs, _ := s.ListTaskEvents(task.ID); len(evs) != 0 {
		t.Fatalf("dupa stergerea task-ului: %+v", evs)
	}

	// stergerea proiectului sterge istoricul
	t2 := Task{ProjectID: p.ID, Title: "t2", AssigneeID: w.ID}
	if err := s.CreateTask(&t2); err != nil {
		t.Fatal(err)
	}
	s.AddTaskEvent(&TaskEvent{TaskID: t2.ID, Kind: "created", Text: "x"})
	if err := s.DeleteProject(p.ID); err != nil {
		t.Fatal(err)
	}
	if evs, _ := s.ListTaskEvents(t2.ID); len(evs) != 0 {
		t.Fatalf("dupa stergerea proiectului: %+v", evs)
	}
}

func TestTaskEventToBodyRoundTrip(t *testing.T) {
	s := newStore(t)
	p, _ := newProject(t, s)
	w := newWorker(t, s, p.ID, "Rook")
	task := Task{ProjectID: p.ID, Title: "t", AssigneeID: w.ID}
	if err := s.CreateTask(&task); err != nil {
		t.Fatal(err)
	}
	e := TaskEvent{TaskID: task.ID, Kind: "reported", Actor: "Rook", To: "Vera", Text: "short", Body: "full report"}
	if err := s.AddTaskEvent(&e); err != nil {
		t.Fatal(err)
	}
	plain := TaskEvent{TaskID: task.ID, Kind: "created", Actor: "You", Text: "x"}
	if err := s.AddTaskEvent(&plain); err != nil {
		t.Fatal(err)
	}
	evs, err := s.ListTaskEvents(task.ID)
	if err != nil || len(evs) != 2 {
		t.Fatalf("%+v %v", evs, err)
	}
	if evs[0].To != "Vera" || evs[0].Body != "full report" || evs[1].To != "" || evs[1].Body != "" {
		t.Fatalf("to/body: %+v", evs)
	}
}
