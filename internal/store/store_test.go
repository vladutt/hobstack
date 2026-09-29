package store

import (
	"errors"
	"path/filepath"
	"testing"
)

func newStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func newProject(t *testing.T, s *Store) (Project, Agent) {
	t.Helper()
	p := Project{Name: "ApiUnit", Goal: "Import OpenAPI", Workdir: t.TempDir()}
	lead, err := s.CreateProject(&p)
	if err != nil {
		t.Fatal(err)
	}
	return p, lead
}

func newWorker(t *testing.T, s *Store, projectID int64, name string) Agent {
	t.Helper()
	a := Agent{ProjectID: projectID, Name: name, Role: "Dev", Position: PositionWorker, Model: "sonnet", Tools: []string{"write"}}
	if err := s.CreateAgent(&a); err != nil {
		t.Fatal(err)
	}
	return a
}

func TestOpenAppliesMigrationsOnce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x.db")
	for i := 0; i < 2; i++ { // a doua deschidere nu trebuie sa reaplice schema
		s, err := Open(path)
		if err != nil {
			t.Fatalf("deschiderea %d: %v", i+1, err)
		}
		var v int
		if err := s.db.QueryRow("PRAGMA user_version").Scan(&v); err != nil {
			t.Fatal(err)
		}
		if v != 2 {
			t.Fatalf("user_version = %d, vreau 2", v)
		}
		s.Close()
	}
}

func TestCreateProjectAddsLeadAndDefaults(t *testing.T) {
	s := newStore(t)
	p, lead := newProject(t, s)
	if p.ID == 0 || p.MaxParallel != 0 || p.BudgetUSD != 0 {
		t.Fatalf("proiect = %+v", p)
	}
	if lead.Position != PositionLead || lead.Name != "Lead" || lead.Model != "sonnet" || len(lead.Tools) != 0 {
		t.Fatalf("lead = %+v", lead)
	}
	got, err := s.Lead(p.ID)
	if err != nil || got.ID != lead.ID {
		t.Fatalf("Lead() = %+v, %v", got, err)
	}
}

func TestUpdateAndDeleteProject(t *testing.T) {
	s := newStore(t)
	p, _ := newProject(t, s)
	p.Name, p.MaxParallel, p.BudgetUSD = "Nou", 4, 2.5
	if err := s.UpdateProject(p); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetProject(p.ID)
	if got.Name != "Nou" || got.MaxParallel != 4 || got.BudgetUSD != 2.5 {
		t.Fatalf("dupa update: %+v", got)
	}
	if err := s.DeleteProject(p.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetProject(p.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetProject dupa stergere: %v", err)
	}
	if err := s.DeleteProject(p.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a doua stergere: %v", err)
	}
}

func TestPauseAll(t *testing.T) {
	s := newStore(t)
	p1, _ := newProject(t, s)
	p2, _ := newProject(t, s)
	if err := s.SetPaused(p1.ID, true); err != nil {
		t.Fatal(err)
	}
	if err := s.SetPaused(p1.ID, false); err != nil {
		t.Fatal(err)
	}
	if err := s.PauseAll(); err != nil {
		t.Fatal(err)
	}
	for _, id := range []int64{p1.ID, p2.ID} {
		p, _ := s.GetProject(id)
		if !p.Paused {
			t.Errorf("proiectul %d nu e pe pauza", id)
		}
	}
}

func TestAgentNameUniqueCaseInsensitive(t *testing.T) {
	s := newStore(t)
	p, _ := newProject(t, s)
	newWorker(t, s, p.ID, "Rook")
	dup := Agent{ProjectID: p.ID, Name: "rook", Position: PositionWorker, Model: "sonnet"}
	if err := s.CreateAgent(&dup); !errors.Is(err, ErrConflict) {
		t.Fatalf("nume duplicat: %v", err)
	}
	other, _ := newProject(t, s)
	newWorker(t, s, other.ID, "Rook") // alt proiect: e voie
}

func TestOnlyOneLead(t *testing.T) {
	s := newStore(t)
	p, _ := newProject(t, s)
	second := Agent{ProjectID: p.ID, Name: "Al doilea", Position: PositionLead, Model: "sonnet"}
	if err := s.CreateAgent(&second); !errors.Is(err, ErrConflict) {
		t.Fatalf("al doilea lead: %v", err)
	}
}

func TestListAgentsLeadFirstAndFind(t *testing.T) {
	s := newStore(t)
	p, lead := newProject(t, s)
	rook := newWorker(t, s, p.ID, "Rook")
	vera := newWorker(t, s, p.ID, "Vera")
	agents, err := s.ListAgents(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(agents) != 3 || agents[0].ID != lead.ID || agents[1].ID != rook.ID || agents[2].ID != vera.ID {
		t.Fatalf("ordine gresita: %+v", agents)
	}
	got, err := s.FindAgentByName(p.ID, "VERA")
	if err != nil || got.ID != vera.ID {
		t.Fatalf("FindAgentByName = %+v, %v", got, err)
	}
	if _, err := s.FindAgentByName(p.ID, "Nimeni"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("agent inexistent: %v", err)
	}
}

func TestUpdateAgentAndSession(t *testing.T) {
	s := newStore(t)
	p, _ := newProject(t, s)
	a := newWorker(t, s, p.ID, "Rook")
	a.Role, a.Model, a.Tools = "Backend", "opus", []string{"write", "shell"}
	if err := s.UpdateAgent(a); err != nil {
		t.Fatal(err)
	}
	if err := s.SetSession(a.ID, "sess-1", "hash-1"); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetAgent(a.ID)
	if got.Role != "Backend" || got.Model != "opus" || len(got.Tools) != 2 || got.Tools[1] != "shell" {
		t.Fatalf("dupa update: %+v", got)
	}
	if got.SessionID != "sess-1" || got.PromptHash != "hash-1" {
		t.Fatalf("sesiune: %q %q", got.SessionID, got.PromptHash)
	}
}

func TestDeleteLeadRejected(t *testing.T) {
	s := newStore(t)
	_, lead := newProject(t, s)
	if err := s.DeleteAgent(lead.ID); !errors.Is(err, ErrConflict) {
		t.Fatalf("stergere lead: %v", err)
	}
}
