// Package setup implementeaza wizard-ul "Proiect nou cu AI": un analist Claude
// citeste un folder si propune proiectul si echipa prin tool-ul MCP propose_setup.
package setup

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"

	"foreman/internal/models"
	"foreman/internal/prompt"
	"foreman/internal/store"
)

// Proposal e propunerea analistului (si, dupa editare, cea trimisa la creare).
// ProposedFolder: un folder al proiectului, cu numele si contextul propuse.
type ProposedFolder struct {
	Path    string `json:"path"`
	Name    string `json:"name"`
	Context string `json:"context"`
}

type Proposal struct {
	Name      string           `json:"name"`
	Goal      string           `json:"goal"`
	Context   string           `json:"context"`
	Rationale string           `json:"rationale"`
	Folders   []ProposedFolder `json:"folders"`
	Agents    []ProposedAgent  `json:"agents"`
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
	Folder           string   `json:"folder"` // numele folderului; "" = primul
}

// ValidationError: propunerea nu e valida; mesajul ajunge ca atare la analist
// (prin MCP) sau in UI (400).
type ValidationError string

func (e ValidationError) Error() string { return string(e) }

const (
	maxAgents  = 12
	maxFolders = 6
)

// safeTools: ce poate propune analistul singur; restul le bifeaza utilizatorul.
var safeTools = []string{"write", "git"}

func tooLong(label, v string, max int) error {
	if utf8.RuneCountInString(v) > max {
		return ValidationError(fmt.Sprintf("%s: at most %d characters", label, max))
	}
	return nil
}

// ValidateProposal verifica si normalizeaza propunerea pe loc. strictTools
// limiteaza tool-urile la write si git (pentru propunerea venita de la AI).
// external: numele agentilor care exista deja in proiect (modul folder); nil
// pentru un proiect nou.
func ValidateProposal(p *Proposal, strictTools bool, external []string) error {
	ext := map[string]bool{} // agenti existenti, litere mici
	for _, n := range external {
		ext[strings.ToLower(strings.TrimSpace(n))] = true
	}
	p.Name = strings.TrimSpace(p.Name)
	p.Goal = strings.TrimSpace(p.Goal)
	p.Context = strings.TrimSpace(p.Context)
	p.Rationale = strings.TrimSpace(p.Rationale)
	if p.Name == "" {
		return ValidationError("the project name is required")
	}
	for _, err := range []error{
		tooLong("project name", p.Name, 80),
		tooLong("goal", p.Goal, 2000), // aceleasi limite ca formularul de proiect
		tooLong("context", p.Context, 20000),
		tooLong("rationale", p.Rationale, 4000),
	} {
		if err != nil {
			return err
		}
	}
	if len(p.Folders) > maxFolders {
		return ValidationError(fmt.Sprintf("at most %d folders", maxFolders))
	}
	folderNames := map[string]bool{}
	for i := range p.Folders {
		f := &p.Folders[i]
		f.Path, f.Name, f.Context = strings.TrimSpace(f.Path), strings.TrimSpace(f.Name), strings.TrimSpace(f.Context)
		if f.Path != "" {
			f.Path = filepath.Clean(f.Path)
		}
		if f.Name == "" {
			f.Name = store.FolderName(f.Path) // fara cale: "Main"
		}
		for _, err := range []error{tooLong("folder name", f.Name, 40), tooLong("context of folder "+f.Name, f.Context, 20000)} {
			if err != nil {
				return err
			}
		}
		if folderNames[strings.ToLower(f.Name)] {
			return ValidationError("two folders named " + f.Name)
		}
		folderNames[strings.ToLower(f.Name)] = true
	}
	if len(p.Agents) == 0 {
		return ValidationError("the proposal must have at least one agent")
	}
	if len(p.Agents) > maxAgents {
		return ValidationError(fmt.Sprintf("at most %d agents", maxAgents))
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
		a.Folder = strings.TrimSpace(a.Folder)
		if a.Folder != "" && !folderNames[strings.ToLower(a.Folder)] {
			return ValidationError(fmt.Sprintf("the folder of %s (%s) does not exist in the proposal", a.Name, a.Folder))
		}
		if a.Name == "" {
			return ValidationError("every agent must have a name")
		}
		key := strings.ToLower(a.Name)
		if _, dup := index[key]; dup {
			return ValidationError("there is already an agent named " + a.Name + " in the proposal")
		}
		if ext[key] {
			return ValidationError("there is already an agent named " + a.Name + " in the project; choose another name")
		}
		index[key] = i
		for _, err := range []error{
			tooLong("name of agent "+a.Name, a.Name, 40),
			tooLong("role of "+a.Name, a.Role, 80),
			tooLong("responsibilities of "+a.Name, a.Responsibilities, 2000),
			tooLong("background of "+a.Name, a.Background, 10000),
			tooLong("instructions of "+a.Name, a.Instructions, 10000),
			tooLong("definition of done of "+a.Name, a.DoneDef, 4000),
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
					return ValidationError("you may propose only the write and git tools (" + a.Name + ": " + t + ")")
				}
				return ValidationError("unknown tool: " + t)
			}
			if !slices.Contains(tools, t) {
				tools = append(tools, t)
			}
		}
		a.Tools = tools
	}

	// legaturile: nume din propunere sau (in modul folder) agenti existenti,
	// fara auto-legaturi si fara cicluri
	roots := 0 // agenti noi legati de utilizator sau de un agent existent
	for _, a := range p.Agents {
		for _, ref := range []struct{ label, name string }{{"boss", a.ReportsTo}, {"reviewer", a.ReviewedBy}} {
			if ref.name == "" {
				continue
			}
			if _, ok := index[strings.ToLower(ref.name)]; !ok && !ext[strings.ToLower(ref.name)] {
				return ValidationError(fmt.Sprintf("the %s of %s (%s) does not exist in the proposal", ref.label, a.Name, ref.name))
			}
		}
		if strings.EqualFold(a.ReportsTo, a.Name) {
			return ValidationError(a.Name + " cannot be their own boss")
		}
		if strings.EqualFold(a.ReviewedBy, a.Name) {
			return ValidationError(a.Name + " cannot be their own reviewer")
		}
		if a.ReportsTo == "" || ext[strings.ToLower(a.ReportsTo)] {
			roots++
		}
	}
	for i := range p.Agents {
		cur, steps := i, 0
		for p.Agents[cur].ReportsTo != "" {
			next, ok := index[strings.ToLower(p.Agents[cur].ReportsTo)]
			if !ok {
				break // sef existent: lantul iese din propunere, deci fara ciclu
			}
			cur = next
			if steps++; cur == i || steps > len(p.Agents) {
				return ValidationError("the link would create a cycle (" + p.Agents[i].Name + ")")
			}
		}
	}
	if roots == 0 {
		if len(ext) > 0 {
			return ValidationError("at least one new agent must report to the user (empty reportsTo) or to an existing agent")
		}
		return ValidationError("at least one agent must report to the user (empty reportsTo)")
	}
	return nil
}

// CheckFolderPaths: folderele propuse de analist sunt exact cele alese de
// utilizator (workdirs, deja curatate). Cu un folder sau niciunul, folders
// poate lipsi sau avea un singur element, cu calea aleasa sau fara cale.
func CheckFolderPaths(p *Proposal, workdirs []string) error {
	if len(workdirs) <= 1 {
		if len(p.Folders) > 1 {
			return ValidationError("there is only one folder: put at most one entry in folders")
		}
		if len(p.Folders) == 1 && p.Folders[0].Path != "" && (len(workdirs) == 0 || filepath.Clean(p.Folders[0].Path) != workdirs[0]) {
			return ValidationError("the folder path must be " + strings.Join(workdirs, "") + " (or empty)")
		}
		return nil
	}
	list := strings.Join(workdirs, ", ")
	if len(p.Folders) != len(workdirs) {
		return ValidationError(fmt.Sprintf("put exactly these %d folders in folders: %s", len(workdirs), list))
	}
	want := map[string]bool{}
	for _, w := range workdirs {
		want[w] = true
	}
	for _, f := range p.Folders {
		path := filepath.Clean(f.Path)
		if !want[path] {
			return ValidationError("unknown or repeated folder path " + f.Path + "; use exactly: " + list)
		}
		delete(want, path) // a doua aparitie a aceleiasi cai e refuzata
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
	folderAt := map[string]int{}
	for i, f := range p.Folders {
		folderAt[strings.ToLower(f.Name)] = i
	}
	out := make([]store.NewMember, len(p.Agents))
	for i, a := range p.Agents {
		out[i] = store.NewMember{
			Agent: store.Agent{
				Name: a.Name, Role: a.Role, Model: a.Model, Responsibilities: a.Responsibilities,
				Background: a.Background, Instructions: a.Instructions, DoneDef: a.DoneDef, Tools: a.Tools,
			},
			Parent: at(a.ReportsTo), Reviewer: at(a.ReviewedBy),
			Folder: folderAt[strings.ToLower(a.Folder)], // lipsa sau gol = primul folder
		}
	}
	return out
}

// CheckFolderProposal: in modul folder, folders lipseste sau contine exact
// folderul analizat (folderPath, deja curatat).
func CheckFolderProposal(p *Proposal, folderPath string) error {
	if len(p.Folders) == 0 {
		return nil
	}
	if p.Folders[0].Path == "" && len(p.Folders) == 1 {
		return nil // fara path: e folderul analizat
	}
	if len(p.Folders) > 1 || filepath.Clean(p.Folders[0].Path) != folderPath {
		return ValidationError("put exactly this folder in folders: " + folderPath)
	}
	return nil
}

// MembersIn: ca Members, dar pentru un proiect existent. Un sef sau reviewer
// care nu e in propunere e un agent din team (ParentID/ReviewerID).
func (p *Proposal) MembersIn(team []store.Agent) []store.NewMember {
	byName := map[string]int64{}
	for _, a := range team {
		byName[strings.ToLower(a.Name)] = a.ID
	}
	out := p.Members()
	for i, a := range p.Agents {
		if id, ok := byName[strings.ToLower(a.ReportsTo)]; ok && out[i].Parent == -1 && a.ReportsTo != "" {
			out[i].ParentID = &id
		}
		if id, ok := byName[strings.ToLower(a.ReviewedBy)]; ok && out[i].Reviewer == -1 && a.ReviewedBy != "" {
			out[i].ReviewerID = &id
		}
		out[i].Folder = 0 // in modul folder, folderul vine din AddAgents
	}
	return out
}
