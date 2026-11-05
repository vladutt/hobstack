package store

import (
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
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

func ptr(v int64) *int64 { return &v }

// newAgent adauga un agent cu seful dat (nil = radacina).
func newAgent(t *testing.T, s *Store, projectID int64, name string, parent *int64) Agent {
	t.Helper()
	a := Agent{ProjectID: projectID, Name: name, Role: "Dev", Model: "sonnet", Tools: []string{"write"}, ParentID: parent}
	if err := s.CreateAgent(&a); err != nil {
		t.Fatal(err)
	}
	return a
}

// newWorker adauga un agent sub prima radacina a proiectului.
func newWorker(t *testing.T, s *Store, projectID int64, name string) Agent {
	t.Helper()
	agents, err := s.ListAgents(projectID)
	if err != nil || len(agents) == 0 {
		t.Fatalf("proiect fara agenti: %v", err)
	}
	return newAgent(t, s, projectID, name, &agents[0].ID)
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
		if v != 33 {
			t.Fatalf("user_version = %d, vreau 33", v)
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
	if lead.ParentID != nil || lead.Name != "Lead" || lead.Model != "sonnet" || len(lead.Tools) != 0 {
		t.Fatalf("lead = %+v", lead)
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

func TestSetPaused(t *testing.T) {
	s := newStore(t)
	p1, _ := newProject(t, s)
	p2, _ := newProject(t, s)
	if err := s.SetPaused(p1.ID, true); err != nil {
		t.Fatal(err)
	}
	if p, _ := s.GetProject(p1.ID); !p.Paused {
		t.Error("p1 trebuie sa fie pe pauza")
	}
	if p, _ := s.GetProject(p2.ID); p.Paused {
		t.Error("p2 nu a fost atins")
	}
	if err := s.SetPaused(p1.ID, false); err != nil {
		t.Fatal(err)
	}
	if p, _ := s.GetProject(p1.ID); p.Paused {
		t.Error("p1 trebuie sa fie pornit")
	}
}

func TestAgentNameUniqueCaseInsensitive(t *testing.T) {
	s := newStore(t)
	p, _ := newProject(t, s)
	newWorker(t, s, p.ID, "Rook")
	dup := Agent{ProjectID: p.ID, Name: "rook", Model: "sonnet"}
	if err := s.CreateAgent(&dup); !errors.Is(err, ErrConflict) {
		t.Fatalf("nume duplicat: %v", err)
	}
	other, _ := newProject(t, s)
	newWorker(t, s, other.ID, "Rook") // alt proiect: e voie
}

func TestSeveralRootsAllowed(t *testing.T) {
	s := newStore(t)
	p, lead := newProject(t, s)
	w := newWorker(t, s, p.ID, "Rook")
	second := Agent{ProjectID: p.ID, Name: "Al doilea", Model: "sonnet"}
	if err := s.CreateAgent(&second); err != nil {
		t.Fatalf("a doua radacina: %v", err)
	}
	agents, _ := s.ListAgents(p.ID)
	if len(agents) != 3 || agents[0].ID != lead.ID || agents[1].ID != second.ID || agents[2].ID != w.ID {
		t.Fatalf("radacinile trebuie sa fie prima: %+v", agents)
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

func TestDeleteLastAgentRejected(t *testing.T) {
	s := newStore(t)
	_, lead := newProject(t, s)
	if err := s.DeleteAgent(lead.ID); !errors.Is(err, ErrConflict) {
		t.Fatalf("stergere ultimul agent: %v", err)
	}
}

func TestMigration003ConvertsOldProject(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vechi.db")
	// o baza "veche": doar migrarile 001 si 002, cu un proiect lead + worker
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"001_init.sql", "002_attachments.sql"} {
		body, _ := migrations.ReadFile("migrations/" + name)
		if _, err := db.Exec(string(body)); err != nil {
			t.Fatal(err)
		}
	}
	for _, q := range []string{
		`PRAGMA user_version = 2`,
		`INSERT INTO projects (id, name, workdir, created_at) VALUES (1, 'Vechi', '/tmp', '2026-01-01T00:00:00Z')`,
		`INSERT INTO agents (id, project_id, name, position, created_at) VALUES (1, 1, 'Lead', 'lead', '2026-01-01T00:00:00Z'), (2, 1, 'Rook', 'worker', '2026-01-01T00:00:00Z')`,
		`INSERT INTO tasks (id, project_id, number, title, assignee_id, status, created_at, updated_at) VALUES
			(1, 1, 1, 'A', 2, 'review', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z'),
			(2, 1, 2, 'B', 2, 'queued', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	db.Close()

	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	lead, _ := s.GetAgent(1)
	rook, _ := s.GetAgent(2)
	if lead.ParentID != nil || rook.ParentID == nil || *rook.ParentID != 1 {
		t.Fatalf("parent_id: lead=%v rook=%v", lead.ParentID, rook.ParentID)
	}
	a, _ := s.GetTask(1)
	b, _ := s.GetTask(2)
	if a.CreatorID == nil || *a.CreatorID != 1 || a.ReviewStage != StageLead {
		t.Fatalf("task in review dupa migrare: %+v", a)
	}
	if b.CreatorID == nil || *b.CreatorID != 1 || b.ReviewStage != StageNone {
		t.Fatalf("task in queued dupa migrare: %+v", b)
	}
	// agents_one_lead nu mai exista: o a doua radacina e permisa
	second := Agent{ProjectID: 1, Name: "Al doilea", Model: "sonnet"}
	if err := s.CreateAgent(&second); err != nil {
		t.Fatalf("a doua radacina: %v", err)
	}
}

func TestLinksInvariants(t *testing.T) {
	s := newStore(t)
	p, lead := newProject(t, s)
	a := newAgent(t, s, p.ID, "A", &lead.ID)
	b := newAgent(t, s, p.ID, "B", &a.ID)
	_, otherLead := newProject(t, s)

	cases := []struct {
		name string
		edit func() Agent
		want string
	}{
		{"propriul sef", func() Agent { x := a; x.ParentID = &a.ID; return x }, "an agent cannot be its own boss"},
		{"ciclu", func() Agent { x := lead; x.ParentID = &b.ID; return x }, "the link would create a cycle"},
		{"sef din alt proiect", func() Agent { x := a; x.ParentID = &otherLead.ID; return x }, "the chosen boss is not in this project"},
		{"sef inexistent", func() Agent { x := a; x.ParentID = ptr(9999); return x }, "the chosen boss is not in this project"},
		{"propriul reviewer", func() Agent { x := a; x.ReviewerID = &a.ID; return x }, "an agent cannot be its own reviewer"},
		{"reviewer din alt proiect", func() Agent { x := a; x.ReviewerID = &otherLead.ID; return x }, "the chosen reviewer is not in this project"},
	}
	for _, c := range cases {
		err := s.UpdateAgent(c.edit())
		var le LinkError
		if !errors.As(err, &le) || le.Error() != c.want {
			t.Errorf("%s: %v, vreau %q", c.name, err, c.want)
		}
	}
	// nimic nu s-a salvat
	if got, _ := s.GetAgent(lead.ID); got.ParentID != nil {
		t.Fatal("legatura invalida a fost salvata")
	}
	// review impotriva ierarhiei e permis: A il verifica pe B si B pe A
	a.ReviewerID = &b.ID
	b.ReviewerID = &a.ID
	if err := s.UpdateAgent(a); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateAgent(b); err != nil {
		t.Fatal(err)
	}
	// agent nou cu reviewer din alt proiect
	bad := Agent{ProjectID: p.ID, Name: "X", Model: "sonnet", ReviewerID: &otherLead.ID}
	var le LinkError
	if err := s.CreateAgent(&bad); !errors.As(err, &le) {
		t.Fatalf("creare cu reviewer strain: %v", err)
	}
	a.Responsibilities = "API si baze de date"
	s.UpdateAgent(a)
	if got, _ := s.GetAgent(a.ID); got.Responsibilities != "API si baze de date" || *got.ReviewerID != b.ID {
		t.Fatalf("dupa update: %+v", got)
	}
}

func TestDeleteAgentReparents(t *testing.T) {
	s := newStore(t)
	p, lead := newProject(t, s)
	mid := newAgent(t, s, p.ID, "Mid", &lead.ID)
	w1 := newAgent(t, s, p.ID, "W1", &mid.ID)
	w2 := newAgent(t, s, p.ID, "W2", &mid.ID)
	rev := newAgent(t, s, p.ID, "Rev", &lead.ID)
	w1.ReviewerID = &rev.ID
	s.UpdateAgent(w1)
	task := Task{ProjectID: p.ID, Title: "A", AssigneeID: w1.ID, CreatorID: &mid.ID}
	s.CreateTask(&task)

	if err := s.DeleteAgent(mid.ID); err != nil {
		t.Fatal(err)
	}
	for _, id := range []int64{w1.ID, w2.ID} {
		if got, _ := s.GetAgent(id); got.ParentID == nil || *got.ParentID != lead.ID {
			t.Fatalf("agentul %d nu a trecut la lead: %+v", id, got.ParentID)
		}
	}
	if got, _ := s.GetTask(task.ID); got.CreatorID != nil {
		t.Fatalf("creator_id trebuia NULL dupa stergerea creatorului: %v", *got.CreatorID)
	}
	if err := s.DeleteAgent(rev.ID); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.GetAgent(w1.ID); got.ReviewerID != nil {
		t.Fatal("reviewer_id trebuia NULL dupa stergerea reviewer-ului")
	}
	// radacina se poate sterge: copiii ei devin radacini
	if err := s.DeleteAgent(lead.ID); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.GetAgent(w2.ID); got.ParentID != nil {
		t.Fatal("w2 trebuia sa devina radacina")
	}
}

func TestSetAgentPosition(t *testing.T) {
	s := newStore(t)
	_, lead := newProject(t, s)
	if lead.PosX != nil || lead.PosY != nil {
		t.Fatal("un agent nou nu are pozitie")
	}
	if err := s.SetAgentPosition(lead.ID, 120.5, 80); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetAgent(lead.ID)
	if got.PosX == nil || *got.PosX != 120.5 || *got.PosY != 80 {
		t.Fatalf("pozitie: %v %v", got.PosX, got.PosY)
	}
	if err := s.SetAgentPosition(9999, 1, 1); !errors.Is(err, ErrNotFound) {
		t.Fatalf("agent inexistent: %v", err)
	}
}

// Migrarea 020 scoate o data toate proiectele de pe pauza: pana acum pauza o punea
// fiecare restart, nu utilizatorul.
func TestMigrationUnpausesProjects(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	for n := 1; n <= 19; n++ {
		matches, _ := fs.Glob(migrations, fmt.Sprintf("migrations/%03d_*.sql", n))
		if len(matches) != 1 {
			t.Fatalf("migrarea %d: %v", n, matches)
		}
		body, _ := migrations.ReadFile(matches[0])
		if _, err := db.Exec(string(body)); err != nil {
			t.Fatalf("%s: %v", matches[0], err)
		}
	}
	for _, q := range []string{
		`PRAGMA user_version = 19`,
		`INSERT INTO projects (id, name, workdir, paused, created_at) VALUES (1, 'Probe', '/tmp/probe', 1, '2026-01-01T00:00:00Z')`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	db.Close()

	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if p, err := s.GetProject(1); err != nil || p.Paused {
		t.Fatalf("dupa migrare: %+v %v", p, err)
	}
}

// migrarea 029 pe o baza veche (pana la 028), cu date: grant-urile folosite dispar,
// cele nefolosite devin de chat, cererile cu prefix devin 'ask'
func TestMigrationTaskGrantsOnOldData(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	entries, _ := migrations.ReadDir("migrations")
	for i, e := range entries[:28] {
		body, _ := migrations.ReadFile("migrations/" + e.Name())
		if _, err := db.Exec(string(body)); err != nil {
			t.Fatalf("%s: %v", e.Name(), err)
		}
		db.Exec(fmt.Sprintf("PRAGMA user_version = %d", i+1))
	}
	ts := "2026-10-01T00:00:00Z"
	for _, q := range []string{
		`INSERT INTO projects (id, name, workdir, created_at) VALUES (1, 'P', '/tmp/p', '` + ts + `')`,
		`INSERT INTO agents (id, project_id, name, position, created_at) VALUES (1, 1, 'Rook', 'worker', '` + ts + `')`,
		`INSERT INTO command_grants (project_id, agent_id, prefix, created_at, used_at) VALUES (1, 1, 'git push', '` + ts + `', '` + ts + `')`,
		`INSERT INTO command_grants (project_id, agent_id, prefix, created_at) VALUES (1, 1, 'rm -rf', '` + ts + `')`,
		`INSERT INTO access_requests (project_id, agent_id, tool, detail, prefix, created_at, updated_at) VALUES (1, 1, 'Bash', 'git push', 'git push', '` + ts + `', '` + ts + `')`,
		`INSERT INTO access_requests (project_id, agent_id, tool, detail, prefix, created_at, updated_at) VALUES (1, 1, 'WebFetch', 'x', '', '` + ts + `', '` + ts + `')`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	db.Close()

	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if g, _ := s.CommandGrants(1, 0); len(g) != 1 || g[0] != "rm -rf" {
		t.Fatalf("grants de chat: %v", g)
	}
	reqs, _ := s.ListOpenAccessRequests(1)
	if len(reqs) != 2 || reqs[0].Kind != AccessAsk || reqs[1].Kind != AccessTool {
		t.Fatalf("%+v", reqs)
	}
}
