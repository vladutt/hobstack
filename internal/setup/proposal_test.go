package setup

import (
	"strings"
	"testing"

	"foreman/internal/store"
)

func valid() *Proposal {
	return &Proposal{
		Name: " Shop ", Goal: "g", Context: "c",
		Agents: []ProposedAgent{
			{Name: "Ana", Role: "Lead", Model: "sonnet", Responsibilities: "imparte munca"},
			{Name: "Bob", Role: "Backend", Model: "model-inexistent", ReportsTo: "ana", Tools: []string{"write", "write", "git"}},
		},
	}
}

func TestValidateProposalNormalizes(t *testing.T) {
	p := valid()
	if err := ValidateProposal(p, true, nil); err != nil {
		t.Fatal(err)
	}
	if p.Name != "Shop" || p.Agents[1].Model != "sonnet" || len(p.Agents[1].Tools) != 2 {
		t.Fatalf("normalizare: %+v", p)
	}
	m := p.Members()
	if m[1].Parent != 0 || m[0].Parent != -1 || m[1].Reviewer != -1 {
		t.Fatalf("members: %+v", m)
	}
}

func TestValidateProposalErrors(t *testing.T) {
	cases := map[string]func(p *Proposal){
		"the link would create a cycle":  func(p *Proposal) { p.Agents[0].ReportsTo = "Bob" },
		"there is already an agent":      func(p *Proposal) { p.Agents[1].Name = "ANA" },
		"does not exist in the proposal": func(p *Proposal) { p.Agents[1].ReviewedBy = "Zed" },
		"their own reviewer":             func(p *Proposal) { p.Agents[1].ReviewedBy = "Bob" },
		"only the write and git tools":   func(p *Proposal) { p.Agents[1].Tools = []string{"shell"} },
		"project name":                   func(p *Proposal) { p.Name = "  " },
		`"You" is reserved for the user`: func(p *Proposal) { p.Agents[1].Name = "you" },
		"at least one agent":             func(p *Proposal) { p.Agents = nil },
		"at most 12 agents": func(p *Proposal) {
			for i := 0; i < 11; i++ {
				p.Agents = append(p.Agents, ProposedAgent{Name: "X" + string(rune('a'+i)), ReportsTo: "Ana"})
			}
		},
	}
	for want, mutate := range cases {
		p := valid()
		mutate(p)
		err := ValidateProposal(p, true, nil)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: am %v", want, err)
		}
	}
	// fara strictTools, shell e acceptat
	p := valid()
	p.Agents[1].Tools = []string{"shell", "web"}
	if err := ValidateProposal(p, false, nil); err != nil {
		t.Fatal(err)
	}
}

func TestValidateProposalSingleRoot(t *testing.T) {
	p := valid()
	p.Agents = []ProposedAgent{{Name: "Solo", Role: "Dev"}}
	if err := ValidateProposal(p, true, nil); err != nil {
		t.Fatalf("un singur agent radacina e valid: %v", err)
	}
}

// limitele sunt aceleasi ca in formularul de proiect, ca proiectul creat din
// wizard sa poata fi salvat si din formular
func TestValidateProposalGoalLimit(t *testing.T) {
	p := valid()
	p.Goal = strings.Repeat("x", 2001)
	if err := ValidateProposal(p, true, nil); err == nil || !strings.Contains(err.Error(), "goal") {
		t.Fatalf("am %v", err)
	}
}

func TestProposalFolders(t *testing.T) {
	p := valid()
	p.Folders = []ProposedFolder{{Path: "/c/client", Name: "Client", Context: "Go"}, {Path: "/c/landing/"}}
	p.Agents[1].Folder = "landing" // numele implicit, fara diferenta de litere
	if err := ValidateProposal(p, true, nil); err != nil {
		t.Fatal(err)
	}
	if p.Folders[1].Name != "landing" {
		t.Fatalf("nume implicit: %+v", p.Folders[1])
	}
	if m := p.Members(); m[0].Folder != 0 || m[1].Folder != 1 {
		t.Fatalf("members: %+v", m)
	}
	for want, mutate := range map[string]func(p *Proposal){
		"two folders named":              func(p *Proposal) { p.Folders[1].Name = "CLIENT" },
		"does not exist in the proposal": func(p *Proposal) { p.Agents[1].Folder = "Zed" },
	} {
		q := valid()
		q.Folders = []ProposedFolder{{Path: "/c/client", Name: "Client"}, {Path: "/c/landing", Name: "Landing"}}
		mutate(q)
		if err := ValidateProposal(q, true, nil); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: am %v", want, err)
		}
	}
}

func TestCheckFolderPaths(t *testing.T) {
	two := []string{"/c/client", "/c/landing"}
	ok := &Proposal{Folders: []ProposedFolder{{Path: "/c/landing/"}, {Path: "/c/client"}}}
	if err := CheckFolderPaths(ok, two); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []*Proposal{
		{},
		{Folders: []ProposedFolder{{Path: "/c/client"}}},
		{Folders: []ProposedFolder{{Path: "/c/client"}, {Path: "/c/other"}}},
		{Folders: []ProposedFolder{{Path: "/c/client"}, {Path: "/c/client"}}},
	} {
		if err := CheckFolderPaths(bad, two); err == nil {
			t.Errorf("trebuia refuzat: %+v", bad.Folders)
		}
	}
	// un singur folder: folders poate lipsi sau avea calea lui
	if err := CheckFolderPaths(&Proposal{}, []string{"/c/a"}); err != nil {
		t.Fatal(err)
	}
	if err := CheckFolderPaths(&Proposal{Folders: []ProposedFolder{{Path: "/c/b"}}}, []string{"/c/a"}); err == nil {
		t.Fatal("calea gresita trebuia refuzata")
	}
	if err := CheckFolderPaths(&Proposal{Folders: []ProposedFolder{{Name: "Main"}}}, nil); err != nil {
		t.Fatal(err)
	}
}

// agenti noi pentru un folder, sub Lead-ul existent
func folderProposal() *Proposal {
	return &Proposal{Name: "Probe", Agents: []ProposedAgent{
		{Name: "Lia", Role: "Landing lead", ReportsTo: "lead"}, // sef existent, alte litere
		{Name: "Max", Role: "Web dev", ReportsTo: "Lia", ReviewedBy: "Vera"},
	}}
}

func TestValidateProposalExternalBoss(t *testing.T) {
	existing := []string{"Lead", "Vera"}
	p := folderProposal()
	if err := ValidateProposal(p, true, existing); err != nil {
		t.Fatal(err)
	}
	// fara nume externe, "lead" nu exista in propunere
	if err := ValidateProposal(folderProposal(), true, nil); err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("fara external: %v", err)
	}
	cases := map[string]func(p *Proposal){
		// nume nou identic cu unul existent
		"already an agent named LEAD": func(p *Proposal) { p.Agents[0].Name = "LEAD"; p.Agents[1].ReportsTo = "LEAD" },
		// echipa noua nelegata (nimeni nu raporteaza tie sau unui agent existent) e mereu un ciclu
		"the link would create a cycle": func(p *Proposal) { p.Agents[0].ReportsTo = "Max" },
		// un lant nou -> sef extern -> nu se urmareste mai departe (fara ciclu fals)
		"does not exist in the proposal": func(p *Proposal) { p.Agents[0].ReportsTo = "Zed" },
	}
	for want, mutate := range cases {
		q := folderProposal()
		mutate(q)
		if err := ValidateProposal(q, true, existing); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: am %v", want, err)
		}
	}
	// un agent nou poate raporta direct utilizatorului
	q := folderProposal()
	q.Agents[0].ReportsTo = ""
	if err := ValidateProposal(q, true, existing); err != nil {
		t.Fatal(err)
	}
}

func TestCheckFolderProposal(t *testing.T) {
	ok := []*Proposal{{}, {Folders: []ProposedFolder{{Path: "/c/landing/"}}}, {Folders: []ProposedFolder{{Name: "Landing"}}}}
	for _, p := range ok {
		if err := CheckFolderProposal(p, "/c/landing"); err != nil {
			t.Fatalf("%+v: %v", p.Folders, err)
		}
	}
	bad := []*Proposal{
		{Folders: []ProposedFolder{{Path: "/c/client"}}},
		{Folders: []ProposedFolder{{Path: "/c/landing"}, {Path: "/c/client"}}},
	}
	for _, p := range bad {
		if err := CheckFolderProposal(p, "/c/landing"); err == nil {
			t.Fatalf("trebuia refuzat: %+v", p.Folders)
		}
	}
}

func TestMembersIn(t *testing.T) {
	p := folderProposal()
	if err := ValidateProposal(p, true, []string{"Lead", "Vera"}); err != nil {
		t.Fatal(err)
	}
	team := []store.Agent{{ID: 7, Name: "Lead"}, {ID: 9, Name: "Vera"}}
	m := p.MembersIn(team)
	if m[0].Parent != -1 || m[0].ParentID == nil || *m[0].ParentID != 7 {
		t.Fatalf("Lia: %+v", m[0])
	}
	if m[1].Parent != 0 || m[1].ParentID != nil || m[1].Reviewer != -1 || m[1].ReviewerID == nil || *m[1].ReviewerID != 9 {
		t.Fatalf("Max: %+v", m[1])
	}
}
