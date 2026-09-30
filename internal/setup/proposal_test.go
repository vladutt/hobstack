package setup

import (
	"strings"
	"testing"
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
	if err := ValidateProposal(p, true); err != nil {
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
		err := ValidateProposal(p, true)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: am %v", want, err)
		}
	}
	// fara strictTools, shell e acceptat
	p := valid()
	p.Agents[1].Tools = []string{"shell", "web"}
	if err := ValidateProposal(p, false); err != nil {
		t.Fatal(err)
	}
}

func TestValidateProposalSingleRoot(t *testing.T) {
	p := valid()
	p.Agents = []ProposedAgent{{Name: "Solo", Role: "Dev"}}
	if err := ValidateProposal(p, true); err != nil {
		t.Fatalf("un singur agent radacina e valid: %v", err)
	}
}

// limitele sunt aceleasi ca in formularul de proiect, ca proiectul creat din
// wizard sa poata fi salvat si din formular
func TestValidateProposalGoalLimit(t *testing.T) {
	p := valid()
	p.Goal = strings.Repeat("x", 2001)
	if err := ValidateProposal(p, true); err == nil || !strings.Contains(err.Error(), "goal") {
		t.Fatalf("am %v", err)
	}
}
