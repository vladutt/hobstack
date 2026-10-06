package store

import (
	"errors"
	"testing"
)

func newRequest(t *testing.T, s *Store, projectID, leadID int64, name string) AgentRequest {
	t.Helper()
	r := AgentRequest{ProjectID: projectID, LeadID: leadID, RunID: "run-1", Reason: "need QA",
		Spec: AgentSpec{Name: name, Role: "QA", Model: "sonnet", Instructions: "test it", Tools: []string{"write"}}}
	if err := s.AddAgentRequest(&r); err != nil {
		t.Fatal(err)
	}
	return r
}

func TestCreatedByRoundTrip(t *testing.T) {
	s := newStore(t)
	p, lead := newProject(t, s)
	a := Agent{ProjectID: p.ID, Name: "Quinn", Model: "sonnet", ParentID: &lead.ID, CreatedBy: &lead.ID}
	if err := s.CreateAgent(&a); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetAgent(a.ID)
	if err != nil || got.CreatedBy == nil || *got.CreatedBy != lead.ID {
		t.Fatalf("created_by: %v %v", got.CreatedBy, err)
	}
	if lead2, _ := s.GetAgent(lead.ID); lead2.CreatedBy != nil {
		t.Fatal("liderul creat cu proiectul nu are created_by")
	}
}

func TestAgentRequestLifecycle(t *testing.T) {
	s := newStore(t)
	p, lead := newProject(t, s)
	r := newRequest(t, s, p.ID, lead.ID, "Quinn")
	got, err := s.GetAgentRequest(r.ID)
	if err != nil || got.Status != AgentRequestOpen || got.Spec.Name != "Quinn" || got.Spec.Tools[0] != "write" || got.Reason != "need QA" {
		t.Fatalf("%+v %v", got, err)
	}
	if n, _ := s.CountOpenAgentRequests(p.ID); n != 1 {
		t.Fatalf("open = %d", n)
	}
	if open, _ := s.OpenAgentRequestNamed(p.ID, "quinn"); !open {
		t.Fatal("numele trebuie gasit fara diferenta de litere")
	}
	a := Agent{ProjectID: p.ID, Name: "Quinn", Model: "sonnet", ParentID: &lead.ID, CreatedBy: &lead.ID}
	if err := s.ApproveAgentRequest(r.ID, &a); err != nil {
		t.Fatal(err)
	}
	got, _ = s.GetAgentRequest(r.ID)
	if got.Status != AgentRequestApproved || got.AgentID == nil || *got.AgentID != a.ID || got.ClosedAt == nil {
		t.Fatalf("dupa approve: %+v", got)
	}
	// a doua aprobare (dublu-click) nu creeaza alt agent
	b := Agent{ProjectID: p.ID, Name: "Quinn2", Model: "sonnet", ParentID: &lead.ID}
	if err := s.ApproveAgentRequest(r.ID, &b); !errors.Is(err, ErrConflict) {
		t.Fatalf("a doua aprobare: %v", err)
	}
	if _, err := s.FindAgentByName(p.ID, "Quinn2"); !errors.Is(err, ErrNotFound) {
		t.Fatal("agentul celei de-a doua aprobari nu trebuie sa existe")
	}
	if list, _ := s.ListOpenAgentRequests(p.ID); len(list) != 0 {
		t.Fatalf("open: %+v", list)
	}
}

func TestApproveAgentRequestNameTaken(t *testing.T) {
	s := newStore(t)
	p, lead := newProject(t, s)
	r := newRequest(t, s, p.ID, lead.ID, "Quinn")
	newAgent(t, s, p.ID, "Quinn", &lead.ID)
	a := Agent{ProjectID: p.ID, Name: "Quinn", Model: "sonnet", ParentID: &lead.ID}
	if err := s.ApproveAgentRequest(r.ID, &a); !errors.Is(err, ErrConflict) {
		t.Fatalf("nume luat: %v", err)
	}
	if got, _ := s.GetAgentRequest(r.ID); got.Status != AgentRequestOpen {
		t.Fatal("cererea ramane deschisa")
	}
}

func TestDismissAgentRequest(t *testing.T) {
	s := newStore(t)
	p, lead := newProject(t, s)
	r := newRequest(t, s, p.ID, lead.ID, "Quinn")
	if err := s.DismissAgentRequest(r.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.DismissAgentRequest(r.ID); !errors.Is(err, ErrConflict) {
		t.Fatalf("a doua oara: %v", err)
	}
	if err := s.DismissAgentRequest(9999); !errors.Is(err, ErrNotFound) {
		t.Fatalf("inexistenta: %v", err)
	}
	if open, _ := s.OpenAgentRequestNamed(p.ID, "Quinn"); open {
		t.Fatal("o cerere inchisa nu mai ocupa numele")
	}
}

func TestCountAddedBy(t *testing.T) {
	s := newStore(t)
	p, lead := newProject(t, s)
	for _, name := range []string{"A1", "A2"} {
		a := Agent{ProjectID: p.ID, Name: name, Model: "sonnet", ParentID: &lead.ID, CreatedBy: &lead.ID}
		if err := s.CreateAgent(&a); err != nil {
			t.Fatal(err)
		}
	}
	newAgent(t, s, p.ID, "Manual", &lead.ID) // creat de utilizator: nu se numara
	newRequest(t, s, p.ID, lead.ID, "R1")
	closed := newRequest(t, s, p.ID, lead.ID, "R2")
	s.DismissAgentRequest(closed.ID)
	if n, err := s.CountAddedBy(lead.ID); err != nil || n != 3 {
		t.Fatalf("CountAddedBy = %d %v, vreau 3", n, err)
	}
	// un agent detasat nu mai conteaza
	a1, _ := s.FindAgentByName(p.ID, "A1")
	a1.ParentID, a1.Detached = nil, true
	if err := s.UpdateAgent(a1); err != nil {
		t.Fatal(err)
	}
	if n, _ := s.CountAddedBy(lead.ID); n != 2 {
		t.Fatalf("dupa detasare = %d, vreau 2", n)
	}
}
