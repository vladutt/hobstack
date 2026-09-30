package store

import (
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
)

// o baza "veche" (migrarile 001-005) primeste la deschidere cate un folder per proiect
func TestMigration006AddsFolderFromWorkdir(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vechi.db")
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"001_init.sql", "002_attachments.sql", "003_team_graph.sql", "004_detached.sql", "005_stages.sql"} {
		body, _ := migrations.ReadFile("migrations/" + name)
		if _, err := db.Exec(string(body)); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
	for _, q := range []string{
		`PRAGMA user_version = 5`,
		`INSERT INTO projects (id, name, workdir, created_at) VALUES (1, 'Probe', '/Users/ana/code/probe-client/', '2026-01-01T00:00:00Z'), (2, 'Root', '/', '2026-01-01T00:00:00Z')`,
		`INSERT INTO agents (id, project_id, name, position, created_at) VALUES (1, 1, 'Lead', 'worker', '2026-01-01T00:00:00Z')`,
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
	p, err := s.GetProject(1)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Folders) != 1 || p.Folders[0].Name != "probe-client" || p.Folders[0].Path != "/Users/ana/code/probe-client/" || p.Folders[0].Position != 0 {
		t.Fatalf("foldere dupa migrare: %+v", p.Folders)
	}
	if root, _ := s.GetProject(2); len(root.Folders) != 1 || root.Folders[0].Name != "Main" {
		t.Fatalf("folderul '/' trebuia sa se numeasca Main: %+v", root.Folders)
	}
	if a, _ := s.GetAgent(1); a.FolderID != nil {
		t.Fatalf("agentul vechi trebuia sa ramana pe primul folder (NULL): %v", *a.FolderID)
	}
}

func TestCreateProjectMakesFirstFolder(t *testing.T) {
	s := newStore(t)
	p := Project{Name: "Shop", Workdir: "/tmp/shop"}
	if _, err := s.CreateProject(&p); err != nil {
		t.Fatal(err)
	}
	if len(p.Folders) != 1 || p.Folders[0].ID == 0 || p.Folders[0].Name != "shop" {
		t.Fatalf("p.Folders dupa creare: %+v", p.Folders)
	}
	got, _ := s.GetProject(p.ID)
	if len(got.Folders) != 1 || got.Folders[0].Path != "/tmp/shop" {
		t.Fatalf("GetProject: %+v", got.Folders)
	}
	list, _ := s.ListProjects()
	if len(list) != 1 || len(list[0].Folders) != 1 {
		t.Fatalf("ListProjects fara foldere: %+v", list)
	}
}

// doua foldere; agentul din al doilea; apoi il stergem pe al doilea
func TestSaveFoldersRemovesAndMovesAgents(t *testing.T) {
	s := newStore(t)
	p := Project{Name: "Probe", Folders: []Folder{
		{Name: "Client", Path: "/code/client", Context: "Go"},
		{Name: "Landing", Path: "/code/landing"},
	}}
	lead, err := s.CreateProject(&p)
	if err != nil {
		t.Fatal(err)
	}
	if p.Workdir != "/code/client" {
		t.Fatalf("workdir = %q", p.Workdir)
	}
	landing := p.Folders[1].ID
	lead.FolderID = &landing
	if err := s.UpdateAgent(lead); err != nil {
		t.Fatal(err)
	}

	// ordinea inversata: Landing devine primul, iar workdir urmeaza primul folder
	p.Folders = []Folder{p.Folders[1], p.Folders[0]}
	if err := s.UpdateProject(p); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetProject(p.ID)
	if got.Workdir != "/code/landing" || got.Folders[0].Name != "Landing" || got.Folders[1].Context != "Go" {
		t.Fatalf("dupa reordonare: %+v", got)
	}

	// Landing sters: agentul trece pe primul folder (NULL)
	got.Folders = got.Folders[1:]
	if err := s.UpdateProject(got); err != nil {
		t.Fatal(err)
	}
	a, _ := s.GetAgent(lead.ID)
	if a.FolderID != nil {
		t.Fatalf("agentul trebuia mutat pe primul folder, are %d", *a.FolderID)
	}
	if again, _ := s.GetProject(p.ID); len(again.Folders) != 1 || again.Workdir != "/code/client" {
		t.Fatalf("dupa stergere: %+v", again)
	}
}

func TestAgentFolderMustBeInProject(t *testing.T) {
	s := newStore(t)
	p1, lead := newProject(t, s)
	p2 := Project{Name: "Altul", Workdir: "/tmp/altul"}
	if _, err := s.CreateProject(&p2); err != nil {
		t.Fatal(err)
	}
	foreign := p2.Folders[0].ID
	lead.FolderID = &foreign
	var le LinkError
	if err := s.UpdateAgent(lead); !errors.As(err, &le) {
		t.Fatalf("folder din alt proiect: vreau LinkError, am %v", err)
	}
	own := p1.Folders[0].ID
	lead.FolderID = &own
	if err := s.UpdateAgent(lead); err != nil {
		t.Fatal(err)
	}
}

func TestHomeFolderAndName(t *testing.T) {
	folders := []Folder{{ID: 7, Name: "Client"}, {ID: 9, Name: "Landing"}}
	nine, missing := int64(9), int64(99)
	if HomeFolder(Agent{}, folders).ID != 7 || HomeFolder(Agent{FolderID: &nine}, folders).ID != 9 || HomeFolder(Agent{FolderID: &missing}, folders).ID != 7 {
		t.Fatal("HomeFolder: fara folder sau cu folder inexistent = primul")
	}
	if HomeFolder(Agent{}, nil).ID != 0 {
		t.Fatal("fara foldere: Folder gol")
	}
	for in, want := range map[string]string{"/a/b/probe": "probe", "/a/b/probe/": "probe", "/": "Main", "": "Main"} {
		if got := FolderName(in); got != want {
			t.Errorf("FolderName(%q) = %q, vreau %q", in, got, want)
		}
	}
}

func TestCreateProjectWithTeamFolders(t *testing.T) {
	s := newStore(t)
	p := Project{Name: "Probe", Folders: []Folder{{Name: "Client", Path: "/c"}, {Name: "Landing", Path: "/l"}}}
	agents, err := s.CreateProjectWithTeam(&p, []NewMember{
		{Agent: Agent{Name: "Ana", Model: "sonnet"}, Parent: -1, Reviewer: -1},
		{Agent: Agent{Name: "Lia", Model: "haiku"}, Parent: 0, Reviewer: -1, Folder: 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	if agents[0].FolderID != nil || agents[1].FolderID == nil || *agents[1].FolderID != p.Folders[1].ID {
		t.Fatalf("foldere agenti: %v %v", agents[0].FolderID, agents[1].FolderID)
	}
	if got, _ := s.GetAgent(agents[1].ID); got.FolderID == nil || *got.FolderID != p.Folders[1].ID {
		t.Fatalf("folder salvat: %+v", got.FolderID)
	}
}

func TestAddAgentsToExistingProject(t *testing.T) {
	s := newStore(t)
	p := Project{Name: "Probe", Folders: []Folder{{Name: "Client", Path: "/c"}, {Name: "Landing", Path: "/l"}}}
	lead, err := s.CreateProject(&p)
	if err != nil {
		t.Fatal(err)
	}
	landing := p.Folders[1]
	ctx := "Hugo site"
	agents, err := s.AddAgents(p.ID, landing.ID, []NewMember{
		{Agent: Agent{Name: "Lia", Model: "sonnet"}, Parent: -1, Reviewer: -1, ParentID: &lead.ID},
		{Agent: Agent{Name: "Max", Model: "haiku", Tools: []string{"write"}}, Parent: 0, Reviewer: -1},
	}, &ctx)
	if err != nil {
		t.Fatal(err)
	}
	lia, _ := s.GetAgent(agents[0].ID)
	max, _ := s.GetAgent(agents[1].ID)
	if lia.ParentID == nil || *lia.ParentID != lead.ID || max.ParentID == nil || *max.ParentID != lia.ID {
		t.Fatalf("legaturi: lia=%v max=%v", lia.ParentID, max.ParentID)
	}
	if lia.FolderID == nil || *lia.FolderID != landing.ID || max.FolderID == nil || *max.FolderID != landing.ID {
		t.Fatalf("foldere: %v %v", lia.FolderID, max.FolderID)
	}
	if got, _ := s.GetProject(p.ID); got.Folders[1].Context != "Hugo site" || got.Folders[0].Context != "" {
		t.Fatalf("context: %+v", got.Folders)
	}
	if l, _ := s.GetAgent(lead.ID); l.Name != "Lead" || l.ParentID != nil {
		t.Fatalf("lead-ul existent s-a schimbat: %+v", l)
	}

	// in primul folder: folder_id NULL; fara context: neschimbat
	more, err := s.AddAgents(p.ID, p.Folders[0].ID, []NewMember{{Agent: Agent{Name: "Cleo", Model: "sonnet"}, Parent: -1, Reviewer: -1}}, nil)
	if err != nil || more[0].FolderID != nil {
		t.Fatalf("primul folder: %v %v", err, more[0].FolderID)
	}
	if got, _ := s.GetProject(p.ID); got.Folders[1].Context != "Hugo site" {
		t.Fatal("contextul nu trebuia atins")
	}
}

func TestAddAgentsRollsBack(t *testing.T) {
	s := newStore(t)
	p, lead := newProject(t, s)
	before, _ := s.ListAgents(p.ID)
	// al doilea nume e luat ("lead"): nu se creeaza nimic
	_, err := s.AddAgents(p.ID, p.Folders[0].ID, []NewMember{
		{Agent: Agent{Name: "Nou", Model: "sonnet"}, Parent: -1, Reviewer: -1, ParentID: &lead.ID},
		{Agent: Agent{Name: "lead", Model: "sonnet"}, Parent: -1, Reviewer: -1},
	}, nil)
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("vreau ErrConflict, am %v", err)
	}
	if after, _ := s.ListAgents(p.ID); len(after) != len(before) {
		t.Fatalf("s-au creat agenti: %d -> %d", len(before), len(after))
	}

	// sef din alt proiect sau folder din alt proiect: LinkError
	p2 := Project{Name: "Altul", Workdir: "/tmp/altul"}
	lead2, _ := s.CreateProject(&p2)
	var le LinkError
	if _, err := s.AddAgents(p.ID, p.Folders[0].ID, []NewMember{{Agent: Agent{Name: "X", Model: "sonnet"}, Parent: -1, Reviewer: -1, ParentID: &lead2.ID}}, nil); !errors.As(err, &le) {
		t.Fatalf("sef strain: %v", err)
	}
	if _, err := s.AddAgents(p.ID, p2.Folders[0].ID, []NewMember{{Agent: Agent{Name: "Y", Model: "sonnet"}, Parent: -1, Reviewer: -1}}, nil); !errors.As(err, &le) {
		t.Fatalf("folder strain: %v", err)
	}
}
