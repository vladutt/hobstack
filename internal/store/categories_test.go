package store

import "testing"

func TestProjectCategory(t *testing.T) {
	s := newStore(t)
	a := Project{Name: "Acme", Workdir: t.TempDir(), Category: "Work"}
	if _, err := s.CreateProject(&a); err != nil {
		t.Fatal(err)
	}
	// alt caz de litere: se foloseste scrierea categoriei existente
	b := Project{Name: "Beta", Workdir: t.TempDir(), Category: "work"}
	if _, err := s.CreateProjectWithTeam(&b, []NewMember{{Agent: Agent{Name: "Ana", Role: "Lead", Model: "sonnet"}, Parent: -1, Reviewer: -1}}); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.GetProject(b.ID); got.Category != "Work" {
		t.Fatalf("Category = %q, vreau Work", got.Category)
	}
	if b.Category != "Work" {
		t.Fatalf("p.Category dupa creare = %q", b.Category)
	}

	got, _ := s.GetProject(a.ID)
	got.Category = "Personal"
	if err := s.UpdateProject(got); err != nil {
		t.Fatal(err)
	}
	// un proiect poate pastra scrierea proprie cand e singurul din categorie
	got.Category = "personal"
	if err := s.UpdateProject(got); err != nil {
		t.Fatal(err)
	}
	if again, _ := s.GetProject(a.ID); again.Category != "personal" {
		t.Fatalf("redenumire proprie: %q", again.Category)
	}
	got.Category = "WORK"
	s.UpdateProject(got)
	if again, _ := s.GetProject(a.ID); again.Category != "Work" {
		t.Fatalf("UpdateProject: %q, vreau Work", again.Category)
	}
	got.Category = ""
	s.UpdateProject(got)
	if again, _ := s.GetProject(a.ID); again.Category != "" {
		t.Fatalf("fara categorie: %q", again.Category)
	}
}
