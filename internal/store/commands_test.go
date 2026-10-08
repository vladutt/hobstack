package store

import "testing"

func TestCommandRulesAndGrants(t *testing.T) {
	s := newStore(t)
	p, lead := newProject(t, s)
	global, err := s.CommandRules(0)
	if err != nil || len(global) != 4 {
		t.Fatalf("regulile implicite: %+v %v", global, err)
	}
	if err := s.SetCommandRules(p.ID, []CommandRule{{"git push", CmdAllow}, {"docker", CmdBlock}}); err != nil {
		t.Fatal(err)
	}
	proj, _ := s.CommandRules(p.ID)
	if len(proj) != 2 || proj[0].Prefix != "docker" || proj[1].Mode != CmdAllow {
		t.Fatalf("%+v", proj)
	}
	// inlocuire: lista noua o sterge pe cea veche
	s.SetCommandRules(p.ID, []CommandRule{{"kubectl", CmdAsk}})
	if proj, _ = s.CommandRules(p.ID); len(proj) != 1 || proj[0].Prefix != "kubectl" {
		t.Fatalf("%+v", proj)
	}
	s.SetCommandRules(0, nil)
	if g, _ := s.CommandRules(0); len(g) != 0 {
		t.Fatalf("%+v", g)
	}
	s.ResetGlobalCommandRules()
	if g, _ := s.CommandRules(0); len(g) != 4 {
		t.Fatalf("reset: %+v", g)
	}
	// grant-uri: luate o singura data
	s.AddCommandGrant(p.ID, lead.ID, "git push")
	if g, _ := s.TakeCommandGrants(lead.ID); len(g) != 1 || g[0] != "git push" {
		t.Fatalf("%v", g)
	}
	if g, _ := s.TakeCommandGrants(lead.ID); len(g) != 0 {
		t.Fatalf("grant folosit de doua ori: %v", g)
	}
	// cereri: acelasi agent + tool + prefix se aduna; alt prefix = alta cerere
	s.RecordAccessDenial(p.ID, lead.ID, nil, "Bash", "git push origin main", "git push")
	s.RecordAccessDenial(p.ID, lead.ID, nil, "Bash", "git push -f", "git push")
	s.RecordAccessDenial(p.ID, lead.ID, nil, "Bash", "rm -rf build", "rm -rf")
	reqs, _ := s.ListOpenAccessRequests(p.ID)
	if len(reqs) != 2 || reqs[0].Count != 2 || reqs[0].Prefix != "git push" || reqs[0].Detail != "git push -f" {
		t.Fatalf("%+v", reqs)
	}
	// stergerea proiectului ia si regulile lui
	if err := s.DeleteProject(p.ID); err != nil {
		t.Fatal(err)
	}
	var n int
	s.db.QueryRow(`SELECT COUNT(*) FROM command_rules WHERE project_id = ?`, p.ID).Scan(&n)
	if n != 0 {
		t.Fatalf("reguli ramase: %d", n)
	}
}

func TestDeleteProjectKeepsGlobalRules(t *testing.T) {
	s := newStore(t)
	p, _ := newProject(t, s)
	q, _ := newProject(t, s)
	s.SetCommandRules(p.ID, []CommandRule{{"docker", CmdBlock}})
	s.SetCommandRules(q.ID, []CommandRule{{"kubectl", CmdAsk}})
	if err := s.DeleteProject(0); err != ErrNotFound {
		t.Fatalf("err = %v", err)
	}
	if g, _ := s.CommandRules(0); len(g) != 4 {
		t.Fatalf("regulile globale s-au pierdut: %+v", g)
	}
	if err := s.DeleteProject(p.ID); err != nil {
		t.Fatal(err)
	}
	if r, _ := s.CommandRules(p.ID); len(r) != 0 {
		t.Fatalf("%+v", r)
	}
	if r, _ := s.CommandRules(q.ID); len(r) != 1 {
		t.Fatalf("regulile altui proiect: %+v", r)
	}
	if g, _ := s.CommandRules(0); len(g) != 4 {
		t.Fatalf("%+v", g)
	}
	if err := s.DeleteProject(99999); err != ErrNotFound {
		t.Fatalf("err = %v", err)
	}
}
