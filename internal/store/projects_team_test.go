package store

import "testing"

func TestCreateProjectWithTeam(t *testing.T) {
	s := newStore(t)
	p := Project{Name: "Shop", Goal: "g", Workdir: t.TempDir()}
	team := []NewMember{
		{Agent: Agent{Name: "Ana", Role: "Lead", Model: "sonnet"}, Parent: -1, Reviewer: -1},
		{Agent: Agent{Name: "Bob", Role: "Backend", Model: "haiku", Tools: []string{"write"}}, Parent: 0, Reviewer: 2},
		{Agent: Agent{Name: "Cia", Role: "QA", Model: "haiku"}, Parent: 0, Reviewer: -1},
	}
	agents, err := s.CreateProjectWithTeam(&p, team)
	if err != nil {
		t.Fatal(err)
	}
	if p.ID == 0 || len(agents) != 3 {
		t.Fatalf("proiect %d, agenti %d", p.ID, len(agents))
	}
	list, _ := s.ListAgents(p.ID)
	if len(list) != 3 {
		t.Fatalf("vreau 3 agenti (fara Lead implicit), am %d", len(list))
	}
	bob, _ := s.FindAgentByName(p.ID, "Bob")
	if bob.ParentID == nil || *bob.ParentID != agents[0].ID || bob.ReviewerID == nil || *bob.ReviewerID != agents[2].ID {
		t.Fatalf("legaturi gresite: %+v", bob)
	}
}

func TestCreateProjectWithTeamRollsBack(t *testing.T) {
	s := newStore(t)
	p := Project{Name: "X", Workdir: t.TempDir()}
	team := []NewMember{
		{Agent: Agent{Name: "Ana", Model: "sonnet"}, Parent: -1, Reviewer: -1},
		{Agent: Agent{Name: "ana", Model: "sonnet"}, Parent: -1, Reviewer: -1}, // duplicat
	}
	if _, err := s.CreateProjectWithTeam(&p, team); err != ErrConflict {
		t.Fatalf("vreau ErrConflict, am %v", err)
	}
	if ps, _ := s.ListProjects(); len(ps) != 0 {
		t.Fatalf("proiectul nu trebuia creat: %d", len(ps))
	}
}
