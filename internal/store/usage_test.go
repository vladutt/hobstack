package store

import (
	"fmt"
	"testing"
	"time"
)

func TestProjectUsageAndRunTokens(t *testing.T) {
	s := newStore(t)
	p, lead := newProject(t, s)
	other, otherLead := newProject(t, s)
	runs := []struct {
		id     string
		agent  int64
		cost   float64
		tokens int64
	}{
		{"r1", lead.ID, 1.5, 1000},
		{"r2", lead.ID, 0.25, -1}, // necunoscut: nu scade din total
		{"r3", otherLead.ID, 9, 50},
	}
	for _, r := range runs {
		run := Run{ID: r.id, AgentID: r.agent}
		if err := s.CreateRun(&run); err != nil {
			t.Fatal(err)
		}
		if err := s.FinishRun(r.id, RunDone, r.cost, ""); err != nil {
			t.Fatal(err)
		}
	}
	// inainte de citire, toate run-urile terminate asteapta tokenii
	if ids, err := s.RunsWithoutTokens(); err != nil || len(ids) != 3 {
		t.Fatalf("RunsWithoutTokens = %v %v", ids, err)
	}
	for _, r := range runs {
		if err := s.SetRunTokens(r.id, r.tokens); err != nil {
			t.Fatal(err)
		}
	}
	if ids, _ := s.RunsWithoutTokens(); len(ids) != 0 {
		t.Fatalf("dupa citire raman %v", ids)
	}
	cost, tokens, err := s.ProjectUsage(p.ID)
	if err != nil || cost != 1.75 || tokens != 1000 {
		t.Fatalf("ProjectUsage = %v %v %v", cost, tokens, err)
	}
	if cost, tokens, _ := s.ProjectUsage(other.ID); cost != 9 || tokens != 50 {
		t.Fatalf("alt proiect: %v %v", cost, tokens)
	}
}

func TestSetFavorite(t *testing.T) {
	s := newStore(t)
	p, _ := newProject(t, s)
	if p.Favorite {
		t.Fatal("un proiect nou nu e favorit")
	}
	if err := s.SetFavorite(p.ID, true); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetProject(p.ID)
	if !got.Favorite {
		t.Fatal("steluta nu s-a salvat")
	}
	// salvarea proiectului din formular nu atinge steluta
	if err := s.UpdateProject(got); err != nil {
		t.Fatal(err)
	}
	if got, _ = s.GetProject(p.ID); !got.Favorite {
		t.Fatal("UpdateProject a sters steluta")
	}
	if err := s.SetFavorite(999, true); err != ErrNotFound {
		t.Fatalf("proiect inexistent: %v", err)
	}
}

func TestProjectActivity(t *testing.T) {
	s := newStore(t)
	p, lead := newProject(t, s)
	at := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	// run-uri azi (2), acum 2 zile (1) si acum 10 zile (in afara ferestrei)
	for i, started := range []time.Time{at, at.Add(-time.Hour), at.AddDate(0, 0, -2), at.AddDate(0, 0, -10)} {
		if _, err := s.db.Exec(`INSERT INTO runs (id, agent_id, status, started_at) VALUES (?, ?, 'done', ?)`,
			fmt.Sprint("a", i), lead.ID, started.Format(time.RFC3339Nano)); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.ProjectActivity(p.ID, 7, at)
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(got) != "[0 0 0 0 1 0 2]" {
		t.Fatalf("activity = %v", got)
	}
}

func TestTaskUsage(t *testing.T) {
	s := newStore(t)
	p, lead := newProject(t, s)
	task := Task{ProjectID: p.ID, Title: "x", AssigneeID: lead.ID, CreatorID: &lead.ID}
	if err := s.CreateTask(&task); err != nil {
		t.Fatal(err)
	}
	for i, r := range []struct {
		cost   float64
		tokens int64
		mins   int
	}{{1, 100, 5}, {0.5, -1, 10}} {
		start := time.Date(2026, 10, 8, 10, 0, 0, 0, time.UTC)
		if _, err := s.db.Exec(`INSERT INTO runs (id, agent_id, task_id, status, started_at, ended_at, cost_usd, tokens) VALUES (?, ?, ?, 'done', ?, ?, ?, ?)`,
			fmt.Sprint("t", i), lead.ID, task.ID, start.Format(time.RFC3339Nano), start.Add(time.Duration(r.mins)*time.Minute).Format(time.RFC3339Nano), r.cost, r.tokens); err != nil {
			t.Fatal(err)
		}
	}
	u, err := s.TaskUsage(task.ID)
	if err != nil || u.Runs != 2 || u.CostUSD != 1.5 || u.Tokens != 100 || u.Seconds != 900 {
		t.Fatalf("%+v %v", u, err)
	}
}
