package store

import "testing"

func TestTodos(t *testing.T) {
	s := newStore(t)
	p, lead := newProject(t, s)
	task := Task{ProjectID: p.ID, Title: "Terms page", AssigneeID: lead.ID, CreatorID: &lead.ID}
	if err := s.CreateTask(&task); err != nil {
		t.Fatal(err)
	}
	if n, err := s.AddTodos(task.ID, []string{"Company name", "Registration number"}, "value"); err != nil || n != 2 {
		t.Fatalf("%d %v", n, err)
	}
	// al doilea raport: textul existent nu se dubleaza, cel nou vine la coada
	if n, _ := s.AddTodos(task.ID, []string{"Company name", "Contact e-mail"}, "value"); n != 1 {
		t.Fatalf("adaugate %d, vreau 1", n)
	}
	list, _ := s.ListTodos(task.ID)
	if len(list) != 3 || list[2].Position != 3 || list[2].Text != "Contact e-mail" {
		t.Fatalf("%+v", list)
	}
	if err := s.SetTodo(task.ID, 1, true, "You"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetTodo(task.ID, 9, true, "You"); err != ErrNotFound {
		t.Fatalf("punct inexistent: %v", err)
	}
	fills, _ := s.ListFillIns(p.ID)
	if len(fills) != 1 || fills[0].Open != 2 || fills[0].Total != 3 || fills[0].First != "Registration number" {
		t.Fatalf("%+v", fills)
	}
	s.SetTodo(task.ID, 2, true, "Lead")
	s.SetTodo(task.ID, 3, true, "Lead")
	if fills, _ := s.ListFillIns(p.ID); len(fills) != 0 {
		t.Fatalf("toate bifate, dar: %+v", fills)
	}
	// debifare
	s.SetTodo(task.ID, 2, false, "You")
	if l, _ := s.ListTodos(task.ID); l[1].DoneAt != nil {
		t.Fatal("debifarea nu s-a salvat")
	}
}
