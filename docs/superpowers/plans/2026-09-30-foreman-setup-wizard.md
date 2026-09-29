# Wizard „Proiect nou cu AI” Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Un wizard in care un analist Claude citeste un folder (si/sau o descriere) si propune proiectul si echipa; utilizatorul editeaza propunerea si creeaza proiectul dintr-o singura cerere.

**Architecture:** Pachet nou `internal/setup`: tipurile propunerii, validarea si `Manager` (sesiuni in memorie, run-uri `claude -p` prin runner-ul existent, token-uri MCP proprii). Rezultatul structurat vine prin tool-ul MCP `propose_setup`. Un wrapper peste `mcp.Backend` rezolva token-urile de setup inaintea celor ale echipei. Store-ul primeste `CreateProjectWithTeam`, iar web-ul primeste rutele `/api/setup*` si `/api/projects/setup`. UI-ul e un view nou, `setup.js`, pe ruta `#/setup`.

**Tech Stack:** Go 1.2x (stdlib, modernc.org/sqlite deja folosit), JS vanilla ES modules, SSE.

**Spec:** `docs/superpowers/specs/2026-09-30-foreman-setup-wizard-design.md`

## Global Constraints

- `go vet ./...` si `go test -race ./...` trec; `gofmt -l internal cmd` nu listeaza nimic.
- Cross-compile: `GOOS=darwin GOARCH=arm64 go build ./cmd/foreman` si `GOOS=windows go build ./cmd/foreman`.
- Mesajele pentru utilizator sunt in romana, fara diacritice.
- Cod simplu si comentat unde nu e evident; autorul e incepator in Go.
- Nicio dependenta noua.
- UI-ul se construieste cu `el()` din `ui.js` si niciodata cu `innerHTML`.
- **Ruling fata de spec:** serverul MCP se numeste `team` in `prompt.MCPConfig`, deci regula de permisiune e `mcp__team__propose_setup`, nu `mcp__foreman__propose_setup`.

## Review Focus

- **Analistul apeleaza `propose_setup` de doua ori:** ultima propunere valida o inlocuieste pe prima, fara eroare. E testat in Task 4.
- **Run-ul se termina inainte ca UI-ul sa se aboneze la SSE:** SSE-ul reda istoricul, iar UI-ul face oricum `GET /api/setup/{id}` la `app_done` si la eroarea SSE. E testat manual in Task 6.
- **Agent debifat in pasul 3:** copiii lui trec la seful lui, iar reviewer-ul devine nimeni; nimic nu trimite nume inexistente. E testat in Task 6 prin CDP.
- **O propunere editata cu ciclu sau nume duplicat:** 400 si niciun proiect creat. E testat in Task 5.
- **Folder dat inexistent la analiza:** 400 si niciun run pornit. E testat in Task 5.

---

### Task 1: store.CreateProjectWithTeam

**Files:**
- Modify: `internal/store/projects.go`
- Test: `internal/store/projects_team_test.go` (nou)

**Interfaces:**
- Produces:
  - `type NewMember struct { Agent Agent; Parent, Reviewer int }`: indecsi in slice, -1 inseamna niciunul;
  - `func (s *Store) CreateProjectWithTeam(p *Project, team []NewMember) ([]Agent, error)`.

- [ ] **Step 1: Write the failing test** (`internal/store/projects_team_test.go`)

```go
package store

import "testing"

func TestCreateProjectWithTeam(t *testing.T) {
	s := openTest(t)
	p := Project{Name: "Shop", Goal: "g", Workdir: t.TempDir()}
	team := []NewMember{
		{Agent: Agent{Name: "Ana", Role: "Lead", Model: "sonnet"}, Parent: -1, Reviewer: -1},
		{Agent: Agent{Name: "Bob", Role: "Backend", Model: "haiku", Tools: []string{"write"}}, Parent: 0, Reviewer: 2},
		{Agent: Agent{Name: "Cia", Role: "QA", Model: "haiku"}, Parent: 0, Reviewer: -1},
	}
	agents, err := s.CreateProjectWithTeam(&p, team)
	if err != nil {
		t.Fatal(err)
	}
	if p.ID == 0 || len(agents) != 3 {
		t.Fatalf("proiect %d, agenti %d", p.ID, len(agents))
	}
	list, _ := s.ListAgents(p.ID)
	if len(list) != 3 {
		t.Fatalf("vreau 3 agenti (fara Lead implicit), am %d", len(list))
	}
	bob, _ := s.FindAgentByName(p.ID, "Bob")
	if bob.ParentID == nil || *bob.ParentID != agents[0].ID || bob.ReviewerID == nil || *bob.ReviewerID != agents[2].ID {
		t.Fatalf("legaturi gresite: %+v", bob)
	}
}

func TestCreateProjectWithTeamRollsBack(t *testing.T) {
	s := openTest(t)
	p := Project{Name: "X", Workdir: t.TempDir()}
	team := []NewMember{
		{Agent: Agent{Name: "Ana", Model: "sonnet"}, Parent: -1, Reviewer: -1},
		{Agent: Agent{Name: "ana", Model: "sonnet"}, Parent: -1, Reviewer: -1}, // duplicat
	}
	if _, err := s.CreateProjectWithTeam(&p, team); err != ErrConflict {
		t.Fatalf("vreau ErrConflict, am %v", err)
	}
	if ps, _ := s.ListProjects(); len(ps) != 0 {
		t.Fatalf("proiectul nu trebuia creat: %d", len(ps))
	}
}
```

Inainte de Step 2, verifica numele helper-ului de test din `internal/store/store_test.go` (ex. `openTest(t)`). Daca are alt nume, foloseste-l pe acela.

- [ ] **Step 2: Run to fail**

Run: `go test ./internal/store/ -run CreateProjectWithTeam`
Expected: FAIL, `undefined: NewMember`.

- [ ] **Step 3: Implement** (in `internal/store/projects.go`, dupa `CreateProject`)

```go
// NewMember e un agent din echipa unui proiect nou. Parent si Reviewer sunt
// indecsi in aceeasi lista (-1 = niciunul); id-urile nu exista inca.
type NewMember struct {
	Agent            Agent
	Parent, Reviewer int
}

// CreateProjectWithTeam creeaza proiectul si echipa data (fara Lead-ul
// implicit) intr-o singura tranzactie: la orice eroare nu ramane nimic.
// Validarea legaturilor (cicluri, radacina) se face inainte, de apelant.
func (s *Store) CreateProjectWithTeam(p *Project, team []NewMember) ([]Agent, error) {
	created := now()
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	res, err := tx.Exec(`INSERT INTO projects (name, goal, context, workdir, paused, max_parallel, budget_usd, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		p.Name, p.Goal, p.Context, p.Workdir, p.Paused, p.MaxParallel, p.BudgetUSD, created)
	if err != nil {
		return nil, err
	}
	if p.ID, err = res.LastInsertId(); err != nil {
		return nil, err
	}
	p.CreatedAt = parseTime(created)

	// intai toti agentii, fara legaturi; apoi legaturile, cand avem id-urile
	agents := make([]Agent, len(team))
	for i, m := range team {
		a := m.Agent
		a.ProjectID, a.ParentID, a.ReviewerID, a.Detached = p.ID, nil, nil, false
		if err := insertAgent(tx, &a); err != nil {
			return nil, err
		}
		agents[i] = a
	}
	idAt := func(i int) *int64 {
		if i < 0 || i >= len(agents) {
			return nil
		}
		id := agents[i].ID
		return &id
	}
	for i, m := range team {
		agents[i].ParentID, agents[i].ReviewerID = idAt(m.Parent), idAt(m.Reviewer)
		if _, err := tx.Exec(`UPDATE agents SET parent_id = ?, reviewer_id = ? WHERE id = ?`,
			nullPtr(agents[i].ParentID), nullPtr(agents[i].ReviewerID), agents[i].ID); err != nil {
			return nil, err
		}
	}
	return agents, tx.Commit()
}
```

- [ ] **Step 4: Run to pass**

Run: `go test -race ./internal/store/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/store/projects.go internal/store/projects_team_test.go
git commit -m "store: CreateProjectWithTeam (proiect + echipa intr-o tranzactie)"
```

---

### Task 2: setup.Proposal si ValidateProposal

**Files:**
- Create: `internal/setup/proposal.go`
- Test: `internal/setup/proposal_test.go`

**Interfaces:**
- Produces:
  - `type Proposal`, `type ProposedAgent` (campurile si tag-urile JSON exact ca in spec);
  - `func ValidateProposal(p *Proposal, strictTools bool) error`. Normalizeaza pe loc: trim, model necunoscut → `sonnet`, tool-uri fara duplicate. Intoarce `ValidationError` (string);
  - `func (p *Proposal) Members() []store.NewMember`. Il apelezi numai dupa `ValidateProposal`.

- [ ] **Step 1: Write the failing test**

```go
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
		"legatura ar crea un ciclu": func(p *Proposal) { p.Agents[0].ReportsTo = "Bob" },
		"exista deja un agent":      func(p *Proposal) { p.Agents[1].Name = "ANA" },
		"nu exista in propunere":    func(p *Proposal) { p.Agents[1].ReviewedBy = "Zed" },
		"propriul reviewer":         func(p *Proposal) { p.Agents[1].ReviewedBy = "Bob" },
		"doar tool-urile write si git": func(p *Proposal) { p.Agents[1].Tools = []string{"shell"} },
		"numele proiectului":        func(p *Proposal) { p.Name = "  " },
		"cel putin un agent":        func(p *Proposal) { p.Agents = nil },
		"maxim 12 agenti": func(p *Proposal) {
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
```

- [ ] **Step 2: Run to fail**

Run: `go test ./internal/setup/`
Expected: FAIL, pachetul nu exista.

- [ ] **Step 3: Implement** (`internal/setup/proposal.go`)

```go
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
		tooLong("obiectivul", p.Goal, 4000),
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
```

- [ ] **Step 4: Run to pass**

Run: `go test -race ./internal/setup/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/setup
git commit -m "setup: propunerea de proiect si validarea ei"
```

---

### Task 3: MCP, tool-ul propose_setup

**Files:**
- Modify: `internal/mcp/server.go`, `internal/mcp/tools.go`, `internal/team/team.go` (stub), `internal/mcp/*_test.go` (fakeBackend + test nou)

**Interfaces:**
- Produces:
  - `Caller.SetupID string`;
  - `Backend.ProposeSetup(c Caller, args json.RawMessage) (string, error)`;
  - tool-ul `propose_setup`.
- Consumes: nimic din task-urile anterioare. Schema reflecta `setup.Proposal`, dar `mcp` nu importa `setup`.

- [ ] **Step 1: Write the failing test**

In fisierul de test al pachetului `mcp` (cel cu `fakeBackend`):
- adauga metoda de mai jos;
- adauga in `setup()` caller-ul `"setup": {SetupID: "s1"}`;
- adauga testul.

```go
func (f *fakeBackend) ProposeSetup(c Caller, args json.RawMessage) (string, error) {
	f.calls = append(f.calls, "propose_setup|"+c.SetupID+"|"+string(args))
	return "Propunere primita.", f.err
}

func TestSetupCallerSeesOnlyProposeSetup(t *testing.T) {
	fb, ts := setup(t)
	names := toolNames(t, ts, "setup")
	if len(names) != 1 || names[0] != "propose_setup" {
		t.Fatalf("tools pentru setup: %v", names)
	}
	res := callTool(t, ts, "setup", "propose_setup", map[string]any{"name": "Shop", "agents": []any{}})
	if res.IsError || len(fb.calls) != 1 || !strings.HasPrefix(fb.calls[0], `propose_setup|s1|{`) {
		t.Fatalf("apel: %+v %v", res, fb.calls)
	}
	// un lead nu vede propose_setup
	for _, n := range toolNames(t, ts, "lead") {
		if n == "propose_setup" {
			t.Fatal("lead-ul nu trebuie sa vada propose_setup")
		}
	}
}
```

Foloseste helper-ele existente din test pentru `tools/list` si `tools/call`. Daca au alte nume sau semnaturi decat `toolNames` si `callTool`, adapteaza apelurile la ele si nu duplica logica.

- [ ] **Step 2: Run to fail**

Run: `go test ./internal/mcp/`
Expected: FAIL (lipsesc `SetupID` si `ProposeSetup`).

- [ ] **Step 3: Implement**

In `server.go`:
- adauga `SetupID string // != "" = analistul din wizard; vede doar propose_setup` in `Caller`;
- adauga `ProposeSetup(c Caller, args json.RawMessage) (string, error)` in `Backend`;
- in `call`, adauga un case nou:

```go
	case "propose_setup":
		raw, _ := json.Marshal(p.Arguments)
		text, err = s.backend.ProposeSetup(c, raw)
```

In `tools.go`, adauga tool-ul si ramura din `toolsFor`:

```go
var proposeSetupTool = Tool{
	Name:        "propose_setup",
	Description: "Trimite propunerea de proiect si echipa. Apeleaza-l o singura data, la final. Daca primesti o eroare, corecteaza propunerea si apeleaza din nou.",
	InputSchema: schema([]string{"name", "goal", "context", "agents"}, map[string]any{
		"name":      str("Numele proiectului, scurt"),
		"goal":      str("Obiectivul: cum arata gata"),
		"context":   str("Context comun pentru toti agentii: stack, comenzi de build/test/rulare, conventii, structura"),
		"rationale": str("Pe scurt: de ce aceasta echipa"),
		"agents": map[string]any{
			"type":        "array",
			"description": "2-6 agenti (maxim 12). Cel putin unul cu reportsTo gol (raporteaza utilizatorului).",
			"items": schema([]string{"name", "role", "responsibilities", "reportsTo"}, map[string]any{
				"name":             str("Nume scurt, unic, ex. Rook"),
				"role":             str("Rolul, ex. Backend Developer"),
				"model":            map[string]any{"type": "string", "enum": []string{"opus", "sonnet", "haiku"}, "description": "Modelul Claude"},
				"responsibilities": str("Ce ia: tipurile de task-uri pe care le primeste"),
				"reportsTo":        str("Numele sefului din aceasta propunere; gol = raporteaza utilizatorului"),
				"reviewedBy":       str("Optional: numele celui care ii verifica munca"),
				"background":       str("Ce stie despre acest proiect"),
				"instructions":     str("Cum lucreaza si ce nu face niciodata"),
				"doneDef":          str("Ce preda cand termina"),
				"tools": map[string]any{
					"type":  "array",
					"items": map[string]any{"type": "string", "enum": []string{"write", "git"}},
				},
			}),
		},
	}),
}
```

La inceputul lui `toolsFor`:

```go
	if c.SetupID != "" {
		return []Tool{proposeSetupTool} // analistul din wizard nu are tool-uri de echipa
	}
```

In `internal/team/team.go`, stub-ul care satisface interfata:

```go
// ProposeSetup apartine wizard-ului (pachetul setup); echipa nu il serveste.
func (t *Team) ProposeSetup(mcp.Caller, json.RawMessage) (string, error) {
	return "", errors.New("propose_setup nu e disponibil aici")
}
```

Adauga importurile `encoding/json` si `errors` daca lipsesc.

- [ ] **Step 4: Run to pass**

Run: `go vet ./... && go test -race ./internal/mcp/ ./internal/team/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/mcp internal/team/team.go
git commit -m "mcp: tool-ul propose_setup pentru analistul din wizard"
```

---

### Task 4: setup.Manager

**Files:**
- Create: `internal/setup/manager.go`
- Test: `internal/setup/manager_test.go`

**Interfaces:**
- Consumes:
  - `runner.Spec`, `runner.Outcome`, `runner.NewID()`;
  - `prompt.MCPConfig(url)`;
  - `mcp.Caller{SetupID}`, `mcp.Backend`;
  - `models.Valid`.
- Produces:
  - `type Starter interface { Start(runner.Spec, func(runner.Outcome)) (*runner.Run, error); Stop(string) error }`;
  - `func New(st Starter, baseURL, tempRoot string) *Manager`;
  - `func (m *Manager) Start(workdir, description, model string) (Session, error)`: erorile de validare sunt `ValidationError`;
  - `func (m *Manager) Get(id string) (Session, bool)`;
  - `func (m *Manager) Stop(id string) error`: `ErrNotFound` daca nu exista;
  - `func (m *Manager) Wrap(inner mcp.Backend) mcp.Backend`;
  - `var ErrNotFound`;
  - `Session` cu tag-urile JSON `id`, `runId`, `workdir`, `description`, `status`, `proposal`, `error`, `createdAt`.

- [ ] **Step 1: Write the failing test** (`internal/setup/manager_test.go`)

```go
package setup

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"foreman/internal/mcp"
	"foreman/internal/runner"
)

type fakeStarter struct {
	mu      sync.Mutex
	specs   []runner.Spec
	done    map[string]func(runner.Outcome)
	stopped []string
}

func (f *fakeStarter) Start(s runner.Spec, d func(runner.Outcome)) (*runner.Run, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.specs = append(f.specs, s)
	f.done[s.ID] = d
	return nil, nil
}
func (f *fakeStarter) Stop(id string) error {
	f.mu.Lock()
	f.stopped = append(f.stopped, id)
	f.mu.Unlock()
	return nil
}

// innerBackend: backend-ul echipei; aici nu rezolva nimic.
type innerBackend struct{ mcp.Backend }

func (innerBackend) Resolve(string) (mcp.Caller, bool) { return mcp.Caller{}, false }

func newTest(t *testing.T) (*Manager, *fakeStarter) {
	f := &fakeStarter{done: map[string]func(runner.Outcome){}}
	return New(f, "http://127.0.0.1:1", t.TempDir()), f
}

// tokenOf scoate token-ul MCP din argumentul --mcp-config (ultima parte a URL-ului).
func tokenOf(t *testing.T, s runner.Spec) string {
	t.Helper()
	for i, a := range s.Args {
		if a == "--mcp-config" {
			var cfg struct {
				MCPServers map[string]struct {
					URL string `json:"url"`
				} `json:"mcpServers"`
			}
			json.Unmarshal([]byte(s.Args[i+1]), &cfg)
			url := cfg.MCPServers["team"].URL
			return url[strings.LastIndex(url, "/")+1:]
		}
	}
	t.Fatal("fara --mcp-config")
	return ""
}

const good = `{"name":"Shop","goal":"g","context":"c","agents":[{"name":"Ana","role":"Lead","responsibilities":"x","reportsTo":""}]}`

func TestManagerHappyPath(t *testing.T) {
	m, f := newTest(t)
	dir := t.TempDir()
	s, err := m.Start(dir, "un magazin", "haiku")
	if err != nil {
		t.Fatal(err)
	}
	spec := f.specs[0]
	if spec.Dir != dir || spec.ID != s.RunID || !strings.Contains(strings.Join(spec.Args, " "), "mcp__team__propose_setup") {
		t.Fatalf("spec: %+v", spec)
	}
	b := m.Wrap(innerBackend{})
	c, ok := b.Resolve(tokenOf(t, spec))
	if !ok || c.SetupID != s.ID {
		t.Fatalf("resolve: %+v %v", c, ok)
	}
	if _, err := b.ProposeSetup(c, json.RawMessage(`{"name":"","agents":[]}`)); err == nil {
		t.Fatal("propunerea invalida trebuia refuzata")
	}
	if _, err := b.ProposeSetup(c, json.RawMessage(good)); err != nil {
		t.Fatal(err)
	}
	f.done[spec.ID](runner.Outcome{RunID: spec.ID, Status: "done"})
	got, _ := m.Get(s.ID)
	if got.Status != StatusReady || got.Proposal == nil || got.Proposal.Name != "Shop" {
		t.Fatalf("sesiune: %+v", got)
	}
	if _, ok := b.Resolve(tokenOf(t, spec)); ok {
		t.Fatal("token-ul trebuia sters la final")
	}
}

func TestManagerNoProposalFails(t *testing.T) {
	m, f := newTest(t)
	s, _ := m.Start("", "o aplicatie de notite", "sonnet")
	spec := f.specs[0]
	if _, err := os.Stat(spec.Dir); err != nil || !strings.HasPrefix(spec.Dir, m.tempRoot) {
		t.Fatalf("fara folder trebuia un folder temporar: %s %v", spec.Dir, err)
	}
	f.done[spec.ID](runner.Outcome{RunID: spec.ID, Status: "done"})
	got, _ := m.Get(s.ID)
	if got.Status != StatusFailed || !strings.Contains(got.Error, "nu a propus nimic") {
		t.Fatalf("%+v", got)
	}
	if _, err := os.Stat(spec.Dir); !os.IsNotExist(err) {
		t.Fatal("folderul temporar trebuia sters")
	}
}

func TestManagerStopAndValidation(t *testing.T) {
	m, f := newTest(t)
	if _, err := m.Start("", "  ", "sonnet"); err == nil {
		t.Fatal("folder si descriere goale: trebuia eroare")
	}
	if _, err := m.Start(filepath.Join(t.TempDir(), "nu-exista"), "", "sonnet"); err == nil {
		t.Fatal("folder inexistent: trebuia eroare")
	}
	if _, err := m.Start("", "x", "gpt-5"); err == nil {
		t.Fatal("model invalid: trebuia eroare")
	}
	s, _ := m.Start("", "x", "sonnet")
	if err := m.Stop(s.ID); err != nil || len(f.stopped) != 1 {
		t.Fatalf("stop: %v %v", err, f.stopped)
	}
	f.done[s.RunID](runner.Outcome{RunID: s.RunID, Status: "stopped"})
	if got, _ := m.Get(s.ID); got.Status != StatusStopped {
		t.Fatalf("%+v", got)
	}
	if err := m.Stop("inexistent"); err != ErrNotFound {
		t.Fatalf("%v", err)
	}
}
```

- [ ] **Step 2: Run to fail**

Run: `go test ./internal/setup/`
Expected: FAIL, `undefined: New`.

- [ ] **Step 3: Implement** (`internal/setup/manager.go`)

```go
package setup

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"foreman/internal/mcp"
	"foreman/internal/models"
	"foreman/internal/prompt"
	"foreman/internal/runner"
)

const (
	StatusRunning = "running"
	StatusReady   = "ready"
	StatusFailed  = "failed"
	StatusStopped = "stopped"
)

var ErrNotFound = errors.New("sesiunea de setup nu exista (poate serverul a fost repornit)")

// Starter e partea din runner.Manager de care avem nevoie (inlocuita in teste).
type Starter interface {
	Start(spec runner.Spec, onDone func(runner.Outcome)) (*runner.Run, error)
	Stop(runID string) error
}

// Session e o analiza din wizard. Traieste doar in memorie.
type Session struct {
	ID          string    `json:"id"`
	RunID       string    `json:"runId"`
	Workdir     string    `json:"workdir"`
	Description string    `json:"description"`
	Status      string    `json:"status"`
	Proposal    *Proposal `json:"proposal"`
	Error       string    `json:"error"`
	CreatedAt   time.Time `json:"createdAt"`

	token   string
	tempDir string // folderul gol folosit cand nu exista folder de proiect
}

type Manager struct {
	starter  Starter
	baseURL  string
	tempRoot string

	mu       sync.Mutex
	sessions map[string]*Session
	tokens   map[string]string // token MCP -> id sesiune
}

func New(st Starter, baseURL, tempRoot string) *Manager {
	return &Manager{starter: st, baseURL: baseURL, tempRoot: tempRoot,
		sessions: map[string]*Session{}, tokens: map[string]string{}}
}

func randomHex(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// Start valideaza cererea si porneste analistul.
func (m *Manager) Start(workdir, description, model string) (Session, error) {
	workdir, description = strings.TrimSpace(workdir), strings.TrimSpace(description)
	if workdir == "" && description == "" {
		return Session{}, ValidationError("alege un folder sau descrie ce vrei sa faci")
	}
	if err := tooLong("descrierea", description, 4000); err != nil {
		return Session{}, err
	}
	if !models.Valid(model) {
		return Session{}, ValidationError("model necunoscut: " + model)
	}
	if workdir != "" {
		dir, err := expandHome(workdir)
		if err != nil {
			return Session{}, err
		}
		if !filepath.IsAbs(dir) {
			return Session{}, ValidationError("folderul trebuie sa fie o cale absoluta")
		}
		if info, err := os.Stat(dir); err != nil || !info.IsDir() {
			return Session{}, ValidationError("folderul nu exista: " + dir)
		}
		workdir = filepath.Clean(dir)
	}

	m.cleanupOld()
	s := &Session{ID: randomHex(8), RunID: runner.NewID(), Workdir: workdir, Description: description,
		Status: StatusRunning, CreatedAt: time.Now(), token: randomHex(16)}
	cwd := workdir
	if cwd == "" {
		s.tempDir = filepath.Join(m.tempRoot, s.ID)
		if err := os.MkdirAll(s.tempDir, 0o755); err != nil {
			return Session{}, err
		}
		cwd = s.tempDir
	}

	m.mu.Lock()
	m.sessions[s.ID] = s
	m.tokens[s.token] = s.ID
	m.mu.Unlock()

	spec := runner.Spec{
		ID: s.RunID, Key: "setup-" + s.ID, Dir: cwd,
		Args:   m.args(s, model),
		Prompt: turnPrompt(s),
	}
	if _, err := m.starter.Start(spec, func(o runner.Outcome) { m.finish(s.ID, o) }); err != nil {
		m.finish(s.ID, runner.Outcome{RunID: s.RunID, Status: "failed", Error: err.Error()})
		return Session{}, err
	}
	return m.snapshot(s), nil
}

func (m *Manager) args(s *Session, model string) []string {
	settings, _ := json.Marshal(map[string]any{"permissions": map[string]any{
		"allow": []string{"Read", "Glob", "Grep", "LS", "mcp__team__propose_setup"},
		"deny": []string{"Write", "Edit", "MultiEdit", "NotebookEdit", "Bash", "WebFetch", "WebSearch",
			"Agent", "TaskCreate", "TaskUpdate", "TaskList", "TaskGet", "TodoWrite"},
	}})
	return []string{
		"-p", turnPrompt(s),
		"--output-format", "stream-json", "--verbose",
		"--model", model,
		"--append-system-prompt", systemPrompt,
		"--setting-sources", "user",
		"--settings", string(settings),
		"--mcp-config", prompt.MCPConfig(m.baseURL + "/mcp/" + s.token),
		"--strict-mcp-config",
	}
}

// finish e apelata de runner cand analistul s-a oprit.
func (m *Manager) finish(id string, o runner.Outcome) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[id]
	if !ok || s.Status != StatusRunning {
		return
	}
	delete(m.tokens, s.token)
	if s.tempDir != "" {
		os.RemoveAll(s.tempDir)
	}
	switch {
	case o.Status == "stopped":
		s.Status = StatusStopped
	case s.Proposal != nil:
		s.Status = StatusReady // chiar daca run-ul a esuat dupa ce a propus
	case o.Status == "failed":
		s.Status, s.Error = StatusFailed, firstNonEmpty(o.Error, o.Result, "analiza a esuat")
	default:
		s.Status, s.Error = StatusFailed, "AI-ul nu a propus nimic. Reincearca."
	}
}

func firstNonEmpty(v ...string) string {
	for _, s := range v {
		if strings.TrimSpace(s) != "" {
			return s
		}
	}
	return ""
}

func (m *Manager) Get(id string) (Session, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[id]
	if !ok {
		return Session{}, false
	}
	return *s, true
}

func (m *Manager) snapshot(s *Session) Session {
	m.mu.Lock()
	defer m.mu.Unlock()
	return *s
}

func (m *Manager) Stop(id string) error {
	m.mu.Lock()
	s, ok := m.sessions[id]
	m.mu.Unlock()
	if !ok {
		return ErrNotFound
	}
	return m.starter.Stop(s.RunID)
}

// cleanupOld sterge sesiunile terminate mai vechi de 24h.
func (m *Manager) cleanupOld() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, s := range m.sessions {
		if s.Status != StatusRunning && time.Since(s.CreatedAt) > 24*time.Hour {
			delete(m.sessions, id)
		}
	}
}

// propose e apelat din MCP de analist.
func (m *Manager) propose(id string, raw json.RawMessage) (string, error) {
	var p Proposal
	if err := json.Unmarshal(raw, &p); err != nil {
		return "", ValidationError("propunerea nu e JSON valid: " + err.Error())
	}
	if err := ValidateProposal(&p, true); err != nil {
		return "", err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[id]
	if !ok || s.Status != StatusRunning {
		return "", ErrNotFound
	}
	s.Proposal = &p // o propunere noua o inlocuieste pe cea veche
	return "Propunere primita. Ai terminat: nu mai face nimic.", nil
}

// Wrap pune token-urile wizard-ului in fata backend-ului echipei.
func (m *Manager) Wrap(inner mcp.Backend) mcp.Backend { return &backend{Backend: inner, m: m} }

type backend struct {
	mcp.Backend
	m *Manager
}

func (b *backend) Resolve(token string) (mcp.Caller, bool) {
	b.m.mu.Lock()
	id, ok := b.m.tokens[token]
	b.m.mu.Unlock()
	if ok {
		return mcp.Caller{SetupID: id}, true
	}
	return b.Backend.Resolve(token)
}

func (b *backend) ProposeSetup(c mcp.Caller, args json.RawMessage) (string, error) {
	if c.SetupID == "" {
		return "", errors.New("propose_setup e doar pentru wizard")
	}
	return b.m.propose(c.SetupID, args)
}

// expandHome transforma ~/x in calea completa.
func expandHome(p string) (string, error) {
	if p != "~" && !strings.HasPrefix(p, "~/") {
		return p, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, strings.TrimPrefix(p, "~")), nil
}
```

`systemPrompt` si `turnPrompt` stau in `internal/setup/prompt.go`, cu textul din spec:

```go
package setup

const systemPrompt = `Esti arhitectul de echipa din Foreman. Citesti un proiect si propui o echipa mica de agenti Claude Code care va lucra pe el.

Reguli:
- Citeste doar cat iti trebuie: structura folderului, README, fisierele de dependinte si build (package.json, composer.json, go.mod, Makefile etc.) si cateva fisiere reprezentative. Nu citi tot repo-ul.
- Nu modifica nimic; nu ai voie.
- Echipa: 2-6 agenti, de obicei un singur lider radacina (reportsTo gol) care imparte munca; adauga un reviewer doar daca proiectul chiar are nevoie.
- Pentru fiecare agent scrie: rol, "Ce ia" (tipurile de task-uri), background (ce stie despre acest proiect), instructiuni (cum lucreaza, ce nu face), definition of done.
- Contextul comun: stack-ul, comenzile de build/test/rulare, conventiile si structura, pe scurt.
- Tool-uri: poti propune doar write (scrie fisiere) si git; utilizatorul adauga singur shell sau web daca vrea.
- Scrie in limba descrierii utilizatorului (implicit romana, fara diacritice).
- La final apeleaza o singura data propose_setup cu toata propunerea.`

func turnPrompt(s *Session) string {
	folder := "Nu exista inca un folder: proiectul e nou."
	if s.Workdir != "" {
		folder = "Folder: " + s.Workdir
	}
	want := "Utilizatorul nu a descris nimic; deduce din cod."
	if s.Description != "" {
		want = "Ce vrea utilizatorul: " + s.Description
	}
	return folder + "\n" + want
}
```

- [ ] **Step 4: Run to pass**

Run: `go vet ./... && go test -race ./internal/setup/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/setup
git commit -m "setup: Manager (analistul din wizard, sesiuni in memorie, token-uri MCP)"
```

---

### Task 5: Rutele web si legarea in main

**Files:**
- Create: `internal/web/api_setup.go`
- Modify: `internal/web/server.go` (camp, setter, rute), `cmd/foreman/main.go`, `internal/config` (folderul `setup`, daca `Paths` are campuri per folder)
- Test: `internal/web/setup_test.go`

**Interfaces:**
- Consumes:
  - `setup.New`, `(*Manager).Start/Get/Stop/Wrap`;
  - `setup.ValidateProposal`, `(*Proposal).Members()`, `setup.ValidationError`;
  - `store.CreateProjectWithTeam`, `resolveWorkdir`.
- Produces:
  - `func (s *Server) SetSetup(m *setup.Manager)`;
  - rutele `POST /api/setup`, `GET /api/setup/{id}`, `POST /api/setup/{id}/stop`, `POST /api/projects/setup`.

- [ ] **Step 1: Write the failing test** (`internal/web/setup_test.go`)

```go
package web

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	wizard "foreman/internal/setup" // alias: in pachetul web, "setup" e deja helper-ul de test
)

func withSetup(t *testing.T) *env {
	e := setup(t)
	e.srv.SetSetup(wizard.New(nopStarter{}, "http://127.0.0.1:0", t.TempDir()))
	return e
}

func TestSetupStartValidation(t *testing.T) {
	e := withSetup(t)
	cases := []map[string]any{
		{"workdir": "", "description": "", "model": "sonnet"},
		{"workdir": filepath.Join(t.TempDir(), "nu-exista"), "model": "sonnet"},
		{"description": "x", "model": "gpt-5"},
	}
	for _, c := range cases {
		if code, raw := e.do(t, "POST", "/api/setup", c); code != 400 {
			t.Errorf("%v: %d %s", c, code, raw)
		}
	}
	code, raw := e.do(t, "POST", "/api/setup", map[string]any{"description": "un blog", "model": "haiku"})
	var s struct{ ID, RunID, Status string }
	json.Unmarshal(raw, &s)
	if code != 201 || s.ID == "" || s.Status != "running" {
		t.Fatalf("%d %s", code, raw)
	}
	if code, _ := e.do(t, "GET", "/api/setup/"+s.ID, nil); code != 200 {
		t.Fatalf("get: %d", code)
	}
	if code, _ := e.do(t, "GET", "/api/setup/inexistent", nil); code != 404 {
		t.Fatalf("get inexistent: %d", code)
	}
	if code, _ := e.do(t, "POST", "/api/setup/"+s.ID+"/stop", nil); code != http.StatusAccepted {
		t.Fatalf("stop: %d", code)
	}
}

func TestCreateProjectFromSetup(t *testing.T) {
	e := withSetup(t)
	proposal := map[string]any{
		"name": "Shop", "goal": "g", "context": "c",
		"agents": []map[string]any{
			{"name": "Ana", "role": "Lead", "model": "sonnet", "reportsTo": ""},
			{"name": "Bob", "role": "Backend", "model": "haiku", "reportsTo": "Ana", "reviewedBy": "Cia", "tools": []string{"write", "shell"}},
			{"name": "Cia", "role": "QA", "model": "haiku", "reportsTo": "Ana"},
		},
	}
	code, raw := e.do(t, "POST", "/api/projects/setup", map[string]any{"workdir": t.TempDir(), "proposal": proposal})
	var p struct{ ID int64 }
	json.Unmarshal(raw, &p)
	if code != 201 || p.ID == 0 {
		t.Fatalf("%d %s", code, raw)
	}
	d := e.detail(t, p.ID)
	if len(d.Agents) != 3 {
		t.Fatalf("agenti: %d", len(d.Agents))
	}
	byName := map[string]int64{}
	for _, a := range d.Agents {
		byName[a.Name] = a.ID
	}
	for _, a := range d.Agents {
		if a.Name == "Bob" && (a.ParentID == nil || *a.ParentID != byName["Ana"] || a.ReviewerID == nil || *a.ReviewerID != byName["Cia"]) {
			t.Fatalf("legaturi Bob: %+v", a)
		}
	}

	// ciclu: 400 si nimic creat
	before := len(e.projects(t))
	proposal["agents"].([]map[string]any)[0]["reportsTo"] = "Bob"
	code, raw = e.do(t, "POST", "/api/projects/setup", map[string]any{"workdir": t.TempDir(), "proposal": proposal})
	if code != 400 || !strings.Contains(string(raw), "ciclu") {
		t.Fatalf("ciclu: %d %s", code, raw)
	}
	if len(e.projects(t)) != before {
		t.Fatal("nu trebuia creat nimic")
	}
}
```

Daca `env` nu are helper-ul `projects`, adauga-l in `setup_test.go`:

```go
func (e *env) projects(t *testing.T) []json.RawMessage {
	_, raw := e.do(t, "GET", "/api/projects", nil)
	var out []json.RawMessage
	json.Unmarshal(raw, &out)
	return out
}
```

- [ ] **Step 2: Run to fail**

Run: `go test ./internal/web/ -run Setup`
Expected: FAIL, `SetSetup undefined`.

- [ ] **Step 3: Implement**

In `server.go`:
- importa pachetul cu alias: `wizard "foreman/internal/setup"`. In pachetul `web`, numele `setup` e deja functia helper din teste, iar un import nealiasat nu compileaza in `go test`. Foloseste acelasi alias in `api_setup.go`;
- adauga campul `wizard *wizard.Manager // nil = wizard-ul nu e disponibil` in `Server`;
- adauga setter-ul:

```go
// SetSetup activeaza wizard-ul "Proiect nou cu AI".
func (s *Server) SetSetup(m *wizard.Manager) { s.wizard = m }
```

- in `Handler()`, langa rutele de proiect:

```go
	mux.HandleFunc("POST /api/setup", s.startSetup)
	mux.HandleFunc("GET /api/setup/{id}", s.getSetup)
	mux.HandleFunc("POST /api/setup/{id}/stop", s.stopSetup)
	mux.HandleFunc("POST /api/projects/setup", s.createProjectFromSetup)
```

`internal/web/api_wizard.go`:

```go
package web

import (
	"errors"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"

	wizard "foreman/internal/setup"
	"foreman/internal/store"
)

// setupFail traduce erorile de validare ale wizard-ului in 400.
func setupFail(w http.ResponseWriter, err error) {
	var ve wizard.ValidationError
	if errors.As(err, &ve) {
		writeErr(w, http.StatusBadRequest, ve.Error())
		return
	}
	if errors.Is(err, wizard.ErrNotFound) {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}
	fail(w, err)
}

func (s *Server) startSetup(w http.ResponseWriter, r *http.Request) {
	if s.wizard == nil {
		writeErr(w, http.StatusNotImplemented, "wizard-ul nu e disponibil")
		return
	}
	var in struct {
		Workdir     string `json:"workdir"`
		Description string `json:"description"`
		Model       string `json:"model"`
	}
	if err := decode(r, &in); err != nil {
		fail(w, err)
		return
	}
	sess, err := s.wizard.Start(in.Workdir, in.Description, in.Model)
	if err != nil {
		setupFail(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, sess)
}

func (s *Server) getSetup(w http.ResponseWriter, r *http.Request) {
	if s.wizard == nil {
		writeErr(w, http.StatusNotFound, wizard.ErrNotFound.Error())
		return
	}
	sess, ok := s.wizard.Get(r.PathValue("id"))
	if !ok {
		writeErr(w, http.StatusNotFound, wizard.ErrNotFound.Error())
		return
	}
	writeJSON(w, http.StatusOK, sess)
}

func (s *Server) stopSetup(w http.ResponseWriter, r *http.Request) {
	if s.wizard == nil {
		writeErr(w, http.StatusNotFound, wizard.ErrNotFound.Error())
		return
	}
	if err := s.wizard.Stop(r.PathValue("id")); err != nil {
		setupFail(w, err)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

// createProjectFromSetup creeaza proiectul si echipa din propunerea (editata)
// din wizard, intr-o singura tranzactie.
func (s *Server) createProjectFromSetup(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Workdir  string         `json:"workdir"`
		Proposal wizard.Proposal `json:"proposal"`
	}
	if err := decode(r, &in); err != nil {
		fail(w, err)
		return
	}
	// utilizatorul poate bifa si shell/web, deci fara strictTools
	if err := wizard.ValidateProposal(&in.Proposal, false); err != nil {
		setupFail(w, err)
		return
	}
	dir, err := resolveWorkdir(in.Workdir, in.Proposal.Name, s.dirs.Workspaces)
	if err != nil {
		fail(w, err)
		return
	}
	p := store.Project{Name: in.Proposal.Name, Goal: in.Proposal.Goal, Context: in.Proposal.Context, Workdir: dir}
	if _, err := s.st.CreateProjectWithTeam(&p, in.Proposal.Members()); errors.Is(err, store.ErrConflict) {
		writeErr(w, http.StatusBadRequest, "exista deja un agent cu acest nume in proiect")
		return
	} else if err != nil {
		fail(w, err)
		return
	}
	// ca la createProject: un folder ramas de la un proiect vechi cu acelasi id
	if err := os.RemoveAll(filepath.Join(s.dirs.Projects, strconv.FormatInt(p.ID, 10))); err != nil {
		log.Printf("curatare folder vechi proiect %d: %v", p.ID, err)
	}
	writeJSON(w, http.StatusCreated, p)
}
```

Verifica ce limite de lungime aplica `projectInput.toProject` pentru nume, obiectiv si context. Daca sunt mai stricte decat cele din `ValidateProposal` (80, 4000, 20000), aliniaza `ValidateProposal` la ele, ca proiectul creat din wizard sa poata fi editat si salvat din formularul obisnuit.

In `cmd/foreman/main.go`:

```go
	sm := setup.New(mgr, url, filepath.Join(paths.Root, "setup"))
	webSrv := web.New(st, tm, mgr, hub, mcp.NewServer(sm.Wrap(tm)),
		web.Dirs{Projects: paths.Projects, Workspaces: paths.Workspaces}, picker.Pick, picker.Available())
	webSrv.SetSetup(sm)
```

Importa `foreman/internal/setup` si `path/filepath`.

- [ ] **Step 4: Run to pass**

Run: `gofmt -l internal cmd; go vet ./... && go test -race ./...`
Expected: fara iesire de la gofmt si toate testele PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/web cmd/foreman
git commit -m "web: rutele wizard-ului (analiza, propunere, creare proiect cu echipa)"
```

---

### Task 6: UI, wizard-ul

**Files:**
- Create: `internal/web/static/js/setup.js`
- Modify: `internal/web/static/js/app.js` (ruta `#/setup`), `internal/web/static/js/projects.js` (card si buton), `internal/web/static/app.css`

**Interfaces:**
- Consumes:
  - `POST /api/setup` → `{id, runId, status}`;
  - `GET /api/setup/{id}` → `{status, proposal, error}`;
  - `POST /api/setup/{id}/stop`;
  - `POST /api/projects/setup` → `{id}`;
  - `GET /api/runs/{runId}/events` (SSE, cu `app_done` la final);
  - `/api/info` (`models`, `canPickFolder`), `POST /api/pick-folder`;
  - `liveRenderer` din `live.js`; `el`, `field`, `toast`, `TOOLS` din `ui.js`.
- Produces: `export async function setupView(root)`, care intoarce functia de curatare.

- [ ] **Step 1: Ruta si cardul**

In `app.js`, importa `setupView` din `./setup.js` si adauga in `routes`, inainte de ruta `#/new`:

```js
  { pattern: /^#\/setup$/, view: (root) => setupView(root) },
```

In `projects.js`, in `page-head`, pune langa „+ Proiect nou” butonul:

```js
el("a", { class: "btn", href: "#/setup" }, "Proiect nou cu AI")
```

In grid, inainte de cardul „Proiect nou”, adauga cardul:

```js
      el("a", { class: "card card-new", href: "#/setup" },
        el("span", { class: "plus" }, "✨"),
        el("strong", {}, "Proiect nou cu AI"),
        el("span", { class: "hint" }, "Alegi un folder, AI-ul propune proiectul si echipa")),
```

- [ ] **Step 2: `setup.js`**

```js
import { api } from "./api.js";
import { el, field, toast, TOOLS } from "./ui.js";
import { liveRenderer } from "./live.js";

// Wizard-ul "Proiect nou cu AI": 1) folder + descriere, 2) analiza live,
// 3) propunerea editabila, apoi "Creeaza proiectul".
export async function setupView(root) {
  const info = await api("/api/info");
  const form = { workdir: "", description: "", model: "sonnet" };
  let session = null;   // {id, runId}
  let stream = null;    // EventSource-ul analizei
  let closed = false;

  const body = el("div", { class: "wizard" });
  root.append(
    el("header", { class: "page-head" },
      el("div", {},
        el("div", { class: "crumbs" }, el("a", { href: "#/" }, "Proiecte"), " / Proiect nou cu AI"),
        el("h1", {}, "Proiect nou cu AI"),
        el("p", { class: "muted" }, "Alegi un folder (sau descrii proiectul), un AI il citeste si propune proiectul si echipa. Tu decizi ce pastrezi."))),
    body);

  // ---------- pasul 1 ----------
  function stepChoose() {
    const workdir = el("input", { type: "text", value: form.workdir, placeholder: "ex. ~/code/shop (optional)" });
    const pickBtn = info.canPickFolder && el("button", { type: "button", class: "btn", onclick: pick }, "Alege...");
    const description = el("textarea", { value: form.description, rows: 5, maxlength: 4000,
      placeholder: "Ce vrei sa faci? ex. refactor la checkout si teste pentru plati. Obligatoriu daca nu alegi folder." });
    const model = el("select", { value: form.model },
      (info.models || []).filter((m) => m.group === "latest").map((m) => el("option", { value: m.id }, m.label)));
    const go = el("button", { type: "submit", class: "btn primary" }, "Analizeaza");
    body.replaceChildren(el("form", { class: "section wizard-step", onsubmit: start },
      el("h2", {}, "1. Ce analizam"),
      field("Folder de proiect", el("div", { class: "input-row" }, workdir, pickBtn),
        "Folderul nu se modifica: AI-ul doar citeste. Gol = proiect nou, folderul se creeaza la final."),
      field("Ce vrei sa faci?", description),
      field("Model pentru analiza", model, "Sonnet e un echilibru bun; Haiku e mai rapid."),
      el("div", { class: "actions" }, go)));

    async function pick() {
      try {
        const r = await api("/api/pick-folder", { method: "POST" });
        if (r?.path) workdir.value = r.path;
      } catch (err) { toast(err.message); }
    }
    async function start(e) {
      e.preventDefault();
      Object.assign(form, { workdir: workdir.value.trim(), description: description.value.trim(), model: model.value });
      go.disabled = true;
      try {
        session = await api("/api/setup", { method: "POST", body: form });
        stepAnalyze();
      } catch (err) {
        toast(err.message);
        go.disabled = false;
      }
    }
  }

  // ---------- pasul 2 ----------
  function stepAnalyze() {
    const box = el("div", { class: "live wizard-live" }, el("div", { class: "live-head" }, "AI-ul citeste proiectul..."));
    const stopBtn = el("button", { type: "button", class: "btn", onclick: () => api(`/api/setup/${session.id}/stop`, { method: "POST" }).catch((e) => toast(e.message)) }, "Opreste");
    body.replaceChildren(el("section", { class: "section wizard-step" },
      el("h2", {}, "2. Analiza"), box, el("div", { class: "actions" }, stopBtn)));
    const render = liveRenderer(box);
    stream = new EventSource(`/api/runs/${session.runId}/events`);
    stream.onmessage = (msg) => {
      let ev;
      try { ev = JSON.parse(msg.data); } catch { return; }
      if (ev.type === "app_done") { closeStream(); checkResult(); return; }
      render(ev);
      box.scrollTop = box.scrollHeight;
    };
    stream.onerror = () => { if (stream?.readyState === EventSource.CLOSED) { closeStream(); checkResult(); } };
  }

  function closeStream() { stream?.close(); stream = null; }

  // checkResult: finish()-ul din server poate veni imediat dupa app_done
  async function checkResult(tries = 0) {
    if (closed) return;
    let s;
    try { s = await api(`/api/setup/${session.id}`); } catch (err) { toast(err.message); stepChoose(); return; }
    if (s.status === "running" && tries < 20) { setTimeout(() => checkResult(tries + 1), 250); return; }
    if (s.status === "ready") { stepReview(s.proposal); return; }
    body.replaceChildren(el("section", { class: "section wizard-step" },
      el("h2", {}, s.status === "stopped" ? "Analiza oprita" : "Analiza nu a mers"),
      s.error && el("p", { class: "err" }, s.error),
      el("div", { class: "actions" },
        el("button", { type: "button", class: "btn", onclick: stepChoose }, "Inapoi"),
        el("button", { type: "button", class: "btn primary", onclick: async () => {
          try { session = await api("/api/setup", { method: "POST", body: form }); stepAnalyze(); } catch (err) { toast(err.message); }
        } }, "Reincearca"))));
  }

  // ---------- pasul 3 ----------
  function stepReview(p) {
    const name = el("input", { type: "text", value: p.name, maxlength: 80 });
    const goal = el("textarea", { value: p.goal, rows: 3 });
    const context = el("textarea", { value: p.context, rows: 8 });
    const tree = el("pre", { class: "wizard-tree" });
    // un rand per agent propus, cu referinte la input-uri
    const rows = p.agents.map((a) => {
      const r = {
        include: el("input", { type: "checkbox", checked: true }),
        name: el("input", { type: "text", value: a.name, maxlength: 40 }),
        role: el("input", { type: "text", value: a.role, maxlength: 80 }),
        model: el("select", { value: a.model }, (info.models || []).map((m) => el("option", { value: m.id }, m.label))),
        resp: el("textarea", { value: a.responsibilities, rows: 2 }),
        parent: el("select", {}), reviewer: el("select", {}),
        parentName: a.reportsTo || "", reviewerName: a.reviewedBy || "",
        background: el("textarea", { value: a.background || "", rows: 4 }),
        instructions: el("textarea", { value: a.instructions || "", rows: 4 }),
        doneDef: el("textarea", { value: a.doneDef || "", rows: 3 }),
        tools: TOOLS.map((t) => ({ id: t.id, box: el("input", { type: "checkbox", checked: (a.tools || []).includes(t.id) }), label: t.label })),
      };
      r.el = el("div", { class: "wizard-agent" },
        el("label", { class: "check wizard-include" }, r.include, el("strong", {}, "Include")),
        el("div", { class: "row" }, field("Nume", r.name), field("Rol", r.role)),
        el("div", { class: "row" }, field("Model", r.model), field("Ce ia", r.resp)),
        el("div", { class: "row" }, field("Raporteaza lui", r.parent), field("Verificat de", r.reviewer)),
        el("div", { class: "field" }, el("span", { class: "label" }, "Tool-uri"),
          el("div", { class: "wizard-tools" }, r.tools.map((t) => el("label", { class: "check" }, t.box, t.label)))),
        el("details", {}, el("summary", {}, "Prompt"),
          field("Background", r.background), field("Instructiuni", r.instructions), field("Definition of done", r.doneDef)));
      return r;
    });

    // selectele de sef/reviewer contin doar agentii inclusi; un agent scos
    // trimite copiii la seful lui (ca la stergere), iar reviewer-ul devine nimeni
    function sync() {
      const included = rows.filter((r) => r.include.checked);
      const byName = new Map(rows.map((r) => [r.name.value.trim().toLowerCase(), r]));
      for (const r of rows) {
        r.el.classList.toggle("excluded", !r.include.checked);
        let boss = byName.get(r.parentName.toLowerCase());
        for (let i = 0; boss && !boss.include.checked && i < rows.length; i++) boss = byName.get(boss.parentName.toLowerCase());
        const rev = byName.get(r.reviewerName.toLowerCase());
        r.parentName = boss && boss !== r ? boss.name.value.trim() : "";
        r.reviewerName = rev && rev.include.checked && rev !== r ? rev.name.value.trim() : "";
        const opts = (first) => [el("option", { value: "" }, first),
          included.filter((o) => o !== r).map((o) => el("option", { value: o.name.value.trim() }, o.name.value.trim()))];
        r.parent.replaceChildren(...opts("Tu"));
        r.parent.value = r.parentName;
        r.reviewer.replaceChildren(...opts("nimeni"));
        r.reviewer.value = r.reviewerName;
      }
      drawTree(included);
    }
    function drawTree(included) {
      const lines = ["Tu"];
      // copiii lui parent: agentii inclusi cu acel sef ("" = radacinile, sub Tu)
      const walk = (parent, depth) => {
        for (const r of included.filter((x) => x.parentName.toLowerCase() === parent.toLowerCase())) {
          lines.push("  ".repeat(depth) + "└ " + r.name.value.trim() + (r.role.value ? " · " + r.role.value : "") +
            (r.reviewerName ? "  (verificat de " + r.reviewerName + ")" : ""));
          walk(r.name.value.trim(), depth + 1);
        }
      };
      walk("", 1);
      tree.textContent = lines.join("\n");
    }
    for (const r of rows) {
      r.include.addEventListener("change", sync);
      r.parent.addEventListener("change", () => { r.parentName = r.parent.value; sync(); });
      r.reviewer.addEventListener("change", () => { r.reviewerName = r.reviewer.value; sync(); });
      r.name.addEventListener("change", () => {
        // redenumirea pastreaza legaturile celorlalti spre acest agent
        const old = r.oldName ?? r.name.defaultValue;
        for (const o of rows) {
          if (o.parentName === old) o.parentName = r.name.value.trim();
          if (o.reviewerName === old) o.reviewerName = r.name.value.trim();
        }
        r.oldName = r.name.value.trim();
        sync();
      });
      r.role.addEventListener("input", () => drawTree(rows.filter((x) => x.include.checked)));
    }

    const createBtn = el("button", { type: "button", class: "btn primary", onclick: create }, "Creeaza proiectul");
    body.replaceChildren(el("div", { class: "wizard-review" },
      el("section", { class: "section" },
        el("h2", {}, "3. Propunerea"),
        field("Nume", name), field("Obiectiv", goal), field("Context comun", context),
        p.rationale && el("div", { class: "field" }, el("span", { class: "label" }, "De ce aceasta echipa"), el("p", { class: "muted" }, p.rationale))),
      el("section", { class: "section" },
        el("div", { class: "section-head" }, el("h2", {}, "Echipa"), el("span", { class: "hint" }, "Debifeaza ce nu vrei; ajustezi legaturile si pe canvas dupa creare.")),
        tree, rows.map((r) => r.el)),
      el("div", { class: "actions wizard-actions" },
        el("button", { type: "button", class: "btn", onclick: stepChoose }, "Inapoi"),
        el("span", { class: "grow" }), createBtn)));
    sync();

    async function create() {
      const agents = rows.filter((r) => r.include.checked).map((r) => ({
        name: r.name.value.trim(), role: r.role.value.trim(), model: r.model.value,
        responsibilities: r.resp.value, reportsTo: r.parentName, reviewedBy: r.reviewerName,
        background: r.background.value, instructions: r.instructions.value, doneDef: r.doneDef.value,
        tools: r.tools.filter((t) => t.box.checked).map((t) => t.id),
      }));
      createBtn.disabled = true;
      try {
        const proj = await api("/api/projects/setup", { method: "POST", body: {
          workdir: form.workdir,
          proposal: { name: name.value, goal: goal.value, context: context.value, rationale: p.rationale || "", agents },
        } });
        location.hash = `#/p/${proj.id}/echipa`;
      } catch (err) {
        toast(err.message);
        createBtn.disabled = false;
      }
    }
  }

  stepChoose();
  return () => { closed = true; closeStream(); };
}
```

- [ ] **Step 3: CSS** (la finalul `app.css`)

```css
/* ---------- wizard "Proiect nou cu AI" ---------- */
.wizard { display: grid; gap: 18px; max-width: 1100px; }
.wizard-step { max-width: 760px; }
.wizard-live { max-height: 60vh; overflow-y: auto; }
.wizard-review { display: grid; gap: 18px; }
.wizard-tree { font-family: var(--mono); font-size: 12px; color: var(--soft); background: var(--bg); border: 1px solid var(--line); border-radius: 9px; padding: 12px; margin: 0; white-space: pre-wrap; }
.wizard-agent { border: 1px solid var(--line); border-radius: 10px; padding: 14px; display: grid; gap: 12px; background: var(--card-2); }
.wizard-agent.excluded { opacity: .45; }
.wizard-agent.excluded .row, .wizard-agent.excluded details, .wizard-agent.excluded .field { display: none; }
.wizard-tools { display: flex; flex-wrap: wrap; gap: 14px; }
.wizard-actions { position: sticky; bottom: 0; background: var(--bg); padding: 12px 0; border-top: 1px solid var(--line); }
.wizard-actions .grow { flex: 1; }
```

- [ ] **Step 4: Verificare**

Run: `for f in internal/web/static/js/*.js; do node --check $f; done`

Apoi reporneste serverul si verifica prin CDP (Chrome headless, ca in memoria „verify-ui-with-screenshots”) la 1440×900 si 1280×720:
- pasul 1: `#/setup` se incarca cu campurile; un submit fara folder si fara descriere intoarce 400 si apare un toast;
- pasul 2: cu descrierea „un blog static simplu” si modelul haiku, output-ul live apare;
- pasul 3 (dupa run-ul real): propunerea apare, iar un agent debifat dispare din selecte si din arbore;
- „Creeaza proiectul” duce la `#/p/<id>/echipa`, cu agentii pe canvas.

Citeste screenshot-urile. La final sterge proiectul de test prin `DELETE /api/projects/<id>`.

- [ ] **Step 5: Commit**

```bash
git add internal/web/static
git commit -m "UI: wizard-ul Proiect nou cu AI"
```

---

### Task 7: e2e real si documentatie

**Files:**
- Modify: `CLAUDE.md` (sectiunea „Stare”)

- [ ] **Step 1: e2e real cu haiku pe folderul agentdash**

Cu serverul pornit:

```bash
curl -s -X POST localhost:7777/api/setup -H 'Content-Type: application/json' \
  -d "{\"workdir\":\"$PWD\",\"description\":\"vreau sa adaug template-uri de echipa\",\"model\":\"haiku\"}"
```

Apoi asteapta pana cand `GET /api/setup/<id>` intoarce `status` diferit de `running` (verifica la ~10s, maxim 5 minute).

Expected: `ready`, cu o propunere de 2-6 agenti si un context comun care mentioneaza Go si SQLite. Daca iese `failed`, citeste `error` si log-ul run-ului din `~/.foreman/logs/runs/<runId>.jsonl`, apoi repara.

- [ ] **Step 2: Creeaza proiectul din propunere, verifica, sterge**

Trimite `POST /api/projects/setup` cu `{workdir: <un folder temporar>, proposal: <propunerea primita>}`.
Expected: 201. `GET /api/projects/<id>` are agentii cu legaturile propuse. Apoi `DELETE /api/projects/<id>`.

- [ ] **Step 3: CLAUDE.md**

In „Stare”, dupa randul despre echipele ca graf, adauga:

```
Gata (wizard, spec in docs/superpowers/specs/2026-09-30-foreman-setup-wizard-design.md): "Proiect nou cu AI":
un analist Claude (doar citire) citeste folderul si propune proiectul si echipa prin tool-ul MCP propose_setup;
utilizatorul editeaza propunerea si creeaza totul intr-o tranzactie. Pachet: internal/setup.
```

- [ ] **Step 4: Commit**

```bash
git add CLAUDE.md
git commit -m "docs: wizard-ul Proiect nou cu AI in CLAUDE.md"
```
