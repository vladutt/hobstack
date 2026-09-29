// Package setup implementeaza wizard-ul "Proiect nou cu AI": un analist Claude
// citeste un folder si propune proiectul si echipa prin tool-ul MCP propose_setup.
package setup

import (
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"foreman/internal/models"
	"foreman/internal/prompt"
	"foreman/internal/store"
)

// Proposal e propunerea analistului (si, dupa editare, cea trimisa la creare).
type Proposal struct {
	Name      string          `json:"name"`
	Goal      string          `json:"goal"`
	Context   string          `json:"context"`
	Rationale string          `json:"rationale"`
	Agents    []ProposedAgent `json:"agents"`
}

type ProposedAgent struct {
	Name             string   `json:"name"`
	Role             string   `json:"role"`
	Model            string   `json:"model"`
	Responsibilities string   `json:"responsibilities"`
	ReportsTo        string   `json:"reportsTo"`  // numele sefului; "" = utilizatorul
	ReviewedBy       string   `json:"reviewedBy"` // numele reviewer-ului; "" = nimeni
	Background       string   `json:"background"`
	Instructions     string   `json:"instructions"`
	DoneDef          string   `json:"doneDef"`
	Tools            []string `json:"tools"`
}

// ValidationError: propunerea nu e valida; mesajul ajunge ca atare la analist
// (prin MCP) sau in UI (400).
type ValidationError string

func (e ValidationError) Error() string { return string(e) }

const maxAgents = 12

// safeTools: ce poate propune analistul singur; restul le bifeaza utilizatorul.
var safeTools = []string{"write", "git"}

func tooLong(label, v string, max int) error {
	if utf8.RuneCountInString(v) > max {
		return ValidationError(fmt.Sprintf("%s: maxim %d de caractere", label, max))
	}
	return nil
}

// ValidateProposal verifica si normalizeaza propunerea pe loc. strictTools
// limiteaza tool-urile la write si git (pentru propunerea venita de la AI).
func ValidateProposal(p *Proposal, strictTools bool) error {
	p.Name = strings.TrimSpace(p.Name)
	p.Goal = strings.TrimSpace(p.Goal)
	p.Context = strings.TrimSpace(p.Context)
	p.Rationale = strings.TrimSpace(p.Rationale)
	if p.Name == "" {
		return ValidationError("numele proiectului e obligatoriu")
	}
	for _, err := range []error{
		tooLong("numele proiectului", p.Name, 80),
		tooLong("obiectivul", p.Goal, 2000), // aceleasi limite ca formularul de proiect
		tooLong("contextul", p.Context, 20000),
		tooLong("explicatia", p.Rationale, 4000),
	} {
		if err != nil {
			return err
		}
	}
	if len(p.Agents) == 0 {
		return ValidationError("propunerea trebuie sa aiba cel putin un agent")
	}
	if len(p.Agents) > maxAgents {
		return ValidationError(fmt.Sprintf("maxim %d agenti", maxAgents))
	}

	index := map[string]int{} // nume (litere mici) -> pozitie
	for i := range p.Agents {
		a := &p.Agents[i]
		a.Name = strings.TrimSpace(a.Name)
		a.Role = strings.TrimSpace(a.Role)
		a.Responsibilities = strings.TrimSpace(a.Responsibilities)
		a.ReportsTo = strings.TrimSpace(a.ReportsTo)
		a.ReviewedBy = strings.TrimSpace(a.ReviewedBy)
		a.Background = strings.TrimSpace(a.Background)
		a.Instructions = strings.TrimSpace(a.Instructions)
		a.DoneDef = strings.TrimSpace(a.DoneDef)
		if a.Name == "" {
			return ValidationError("fiecare agent trebuie sa aiba nume")
		}
		key := strings.ToLower(a.Name)
		if _, dup := index[key]; dup {
			return ValidationError("exista deja un agent cu numele " + a.Name + " in propunere")
		}
		index[key] = i
		for _, err := range []error{
			tooLong("numele agentului "+a.Name, a.Name, 40),
			tooLong("rolul lui "+a.Name, a.Role, 80),
			tooLong("responsabilitatile lui "+a.Name, a.Responsibilities, 2000),
			tooLong("background-ul lui "+a.Name, a.Background, 10000),
			tooLong("instructiunile lui "+a.Name, a.Instructions, 10000),
			tooLong("definition of done pentru "+a.Name, a.DoneDef, 4000),
		} {
			if err != nil {
				return err
			}
		}
		if !models.Valid(a.Model) {
			a.Model = "sonnet"
		}
		tools := []string{}
		for _, t := range a.Tools {
			allowed := prompt.ValidTools
			if strictTools {
				allowed = safeTools
			}
			if !slices.Contains(allowed, t) {
				if strictTools {
					return ValidationError("poti propune doar tool-urile write si git (" + a.Name + ": " + t + ")")
				}
				return ValidationError("tool necunoscut: " + t)
			}
			if !slices.Contains(tools, t) {
				tools = append(tools, t)
			}
		}
		a.Tools = tools
	}

	// legaturile: nume din propunere, fara auto-legaturi si fara cicluri
	roots := 0
	for _, a := range p.Agents {
		for _, ref := range []struct{ label, name string }{{"seful", a.ReportsTo}, {"reviewer-ul", a.ReviewedBy}} {
			if ref.name == "" {
				continue
			}
			if _, ok := index[strings.ToLower(ref.name)]; !ok {
				return ValidationError(fmt.Sprintf("%s lui %s (%s) nu exista in propunere", ref.label, a.Name, ref.name))
			}
		}
		if strings.EqualFold(a.ReportsTo, a.Name) {
			return ValidationError(a.Name + " nu poate fi propriul sef")
		}
		if strings.EqualFold(a.ReviewedBy, a.Name) {
			return ValidationError(a.Name + " nu poate fi propriul reviewer")
		}
		if a.ReportsTo == "" {
			roots++
		}
	}
	for i := range p.Agents {
		cur, steps := i, 0
		for p.Agents[cur].ReportsTo != "" {
			cur = index[strings.ToLower(p.Agents[cur].ReportsTo)]
			if steps++; cur == i || steps > len(p.Agents) {
				return ValidationError("legatura ar crea un ciclu (" + p.Agents[i].Name + ")")
			}
		}
	}
	if roots == 0 {
		return ValidationError("cel putin un agent trebuie sa raporteze utilizatorului (reportsTo gol)")
	}
	return nil
}

// Members transforma propunerea (deja validata) in agenti pentru store.
func (p *Proposal) Members() []store.NewMember {
	index := map[string]int{}
	for i, a := range p.Agents {
		index[strings.ToLower(a.Name)] = i
	}
	at := func(name string) int {
		if i, ok := index[strings.ToLower(name)]; ok && name != "" {
			return i
		}
		return -1
	}
	out := make([]store.NewMember, len(p.Agents))
	for i, a := range p.Agents {
		out[i] = store.NewMember{
			Agent: store.Agent{
				Name: a.Name, Role: a.Role, Model: a.Model, Responsibilities: a.Responsibilities,
				Background: a.Background, Instructions: a.Instructions, DoneDef: a.DoneDef, Tools: a.Tools,
			},
			Parent: at(a.ReportsTo), Reviewer: at(a.ReviewedBy),
		}
	}
	return out
}
