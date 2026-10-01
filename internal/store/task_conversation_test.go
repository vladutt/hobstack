package store

import "testing"

func TestListTaskRunMessages(t *testing.T) {
	s := newStore(t)
	p, _ := newProject(t, s)
	w := newWorker(t, s, p.ID, "Rook")
	t1 := Task{ProjectID: p.ID, Title: "one", AssigneeID: w.ID}
	t2 := Task{ProjectID: p.ID, Title: "two", AssigneeID: w.ID}
	for _, tk := range []*Task{&t1, &t2} {
		if err := s.CreateTask(tk); err != nil {
			t.Fatal(err)
		}
	}
	for _, r := range []Run{{ID: "r1", AgentID: w.ID, TaskID: t1.ID}, {ID: "r2", AgentID: w.ID, TaskID: t1.ID}, {ID: "r3", AgentID: w.ID, TaskID: t2.ID}} {
		r := r
		if err := s.CreateRun(&r); err != nil {
			t.Fatal(err)
		}
	}
	add := func(kind, author, text, run string) {
		t.Helper()
		m := Message{ProjectID: p.ID, AgentID: w.ID, Kind: kind, Author: author, Text: text, RunID: run}
		if err := s.AddMessage(&m); err != nil {
			t.Fatal(err)
		}
	}
	add(KindMsg, "Rook", "a1", "r1")
	add(KindMsg, "Rook", "other task", "r3")
	add(KindMsg, AuthorUser, "user with run", "r1")
	add(KindEvent, "Rook", "event", "r1")
	add(KindMsg, "Rook", "no run", "")
	add(KindMsg, "Rook", "a2", "r2")

	got, err := s.ListTaskRunMessages(t1.ID)
	if err != nil || len(got) != 2 || got[0].Text != "a1" || got[1].Text != "a2" {
		t.Fatalf("%+v %v", got, err)
	}
	if got[0].Attachments == nil {
		t.Fatal("Attachments trebuie sa fie non-nil")
	}
	if got, err := s.ListTaskRunMessages(t2.ID); err != nil || len(got) != 1 || got[0].Text != "other task" {
		t.Fatalf("%+v %v", got, err)
	}
	if got, err := s.ListTaskRunMessages(9999); err != nil || got == nil || len(got) != 0 {
		t.Fatalf("lista goala non-nil: %+v %v", got, err)
	}
}
