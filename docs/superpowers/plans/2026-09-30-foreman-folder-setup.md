# Wizard AI pentru un folder dintr-un proiect existent: plan de implementare

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Din "Project instructions", pe un folder salvat, butonul "Set up agents with AI" porneste un analist care propune contextul folderului si o echipa noua pentru el, legata sub un agent existent; utilizatorul editeaza si apasa "Add agents".

**Architecture:** Extindem `internal/setup` cu un mod "folder" (Session.ProjectID/FolderID, `StartFolder`, prompturi separate, validare cu nume externe). Store-ul primeste `AddAgents` (agenti noi intr-un proiect existent, intr-o tranzactie). Web: o ruta care porneste analiza si una care adauga agentii. UI: acelasi `setup.js`, cu un mod folder.

**Tech Stack:** Go, SQLite (modernc.org/sqlite), vanilla JS (embed), Claude Code CLI headless.

**Spec:** `docs/superpowers/specs/2026-09-30-foreman-folder-setup-design.md`

## Global Constraints

- `go vet ./...`, `go build -race ./...`, `go test ./...` trec dupa fiecare task; cross-compile `GOOS=darwin GOARCH=arm64 go build ./cmd/foreman` si `GOOS=windows go build ./cmd/foreman`.
- JS: fiecare fisier din `internal/web/static/js/` se verifica ca modul: `cp f /tmp/x.mjs && node --check /tmp/x.mjs` (`node --check` pe `.js` nu prinde erorile de sintaxa).
- Tot ce vede utilizatorul si prompturile pentru agenti sunt in engleza; comentariile din cod in romana, fara diacritice.
- Agentii existenti nu se modifica niciodata de acest flux.
- `agents.folder_id` NULL = primul folder al proiectului.
- Wizard-ul de azi ("New project with AI") se comporta exact ca inainte.
- In JS, un camp optional se face `cond ? el(...) : null`, nu `cond && el(...)` cand valoarea e folosita apoi cu `?.` (`?.` nu protejeaza `false`).
- Cod simplu, comentat unde nu e evident: autorul e incepator in Go.

## Review Focus

1. **Sub-liderul raporteaza unui agent existent**: regula "cel putin un agent raporteaza utilizatorului" nu trebuie sa refuze propunerea. Test in Task 1 (`TestValidateProposalExternalBoss`).
2. **Nume nou identic cu un agent existent** (fara diferenta de litere): refuzat de analist si la "Add agents", fara nimic creat. Teste in Task 1 si Task 2.
3. **Un sef extern in lantul de cicluri**: bucla de verificare a ciclurilor nu trebuie sa urmeze un nume extern ca index 0. Test in Task 1 (`TestValidateProposalExternalBoss`, lant nou -> extern).
4. **Folderul primul vs. altul**: agentii noi in primul folder primesc `folder_id` NULL, in altul id-ul folderului. Test in Task 2.
5. **Wizard-ul de azi neschimbat**: apelurile existente trec `nil` ca nume externe; testele existente din `internal/setup` si `internal/web` trec neschimbate (in afara semnaturii).

---

### Task 1: Validarea propunerii cu agenti existenti

**Files:**
- Modify: `internal/setup/proposal.go` (ValidateProposal, CheckFolderProposal, MembersIn)
- Modify: `internal/setup/manager.go` (apelul ValidateProposal: `nil`)
- Modify: `internal/web/api_setup.go` (apelul ValidateProposal: `nil`)
- Modify: `internal/store/projects.go` (NewMember.ParentID, ReviewerID)
- Test: `internal/setup/proposal_test.go`

**Interfaces:**
- Produces:
  - `func ValidateProposal(p *Proposal, strictTools bool, external []string) error`: `external` = numele agentilor existenti (oricum scrise); `nil` = wizard-ul de azi
  - `func CheckFolderProposal(p *Proposal, folderPath string) error`
  - `func (p *Proposal) MembersIn(team []store.Agent) []store.NewMember`
  - `store.NewMember.ParentID, ReviewerID *int64`: legaturi la agenti existenti, folosite cand `Parent`/`Reviewer` = -1

- [ ] **Step 1: Testele**

In `proposal_test.go` actualizeaza apelurile existente `ValidateProposal(p, true)` -> `ValidateProposal(p, true, nil)` si `ValidateProposal(p, false)` -> `ValidateProposal(p, false, nil)`. Adauga (importa `foreman/internal/store`):

```go
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
	ok := []*Proposal{{}, {Folders: []ProposedFolder{{Path: "/c/landing/"}}}}
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
```

- [ ] **Step 2: Confirma RED**

Run: `go test ./internal/setup/`
Expected: eroare de compilare (ValidateProposal cu 3 argumente, CheckFolderProposal, MembersIn, NewMember.ParentID).

- [ ] **Step 3: `store.NewMember`**

In `internal/store/projects.go`, `NewMember` devine:

```go
type NewMember struct {
	Agent                Agent
	Parent, Reviewer     int    // index in aceeasi lista; -1 = niciunul (sau agent existent, vezi mai jos)
	Folder               int    // index in p.Folders; 0 = primul folder (salvat ca NULL)
	ParentID, ReviewerID *int64 // agent existent in proiect, cand Parent/Reviewer = -1 (doar AddAgents)
}
```

- [ ] **Step 4: `ValidateProposal`**

Semnatura noua: `func ValidateProposal(p *Proposal, strictTools bool, external []string) error`. Comentariul functiei primeste: `external: numele agentilor care exista deja in proiect (modul folder); nil pentru un proiect nou.`

La inceputul functiei:

```go
	ext := map[string]bool{} // agenti existenti, litere mici
	for _, n := range external {
		ext[strings.ToLower(strings.TrimSpace(n))] = true
	}
```

In bucla peste agenti, dupa verificarea de duplicat `if _, dup := index[key]; dup {...}`:

```go
		if ext[key] {
			return ValidationError("there is already an agent named " + a.Name + " in the project; choose another name")
		}
```

Blocul de legaturi se inlocuieste cu:

```go
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
```

Actualizeaza apelurile: `internal/setup/manager.go` (`ValidateProposal(&p, true)` -> `ValidateProposal(&p, true, nil)`; Task 3 il schimba din nou) si `internal/web/api_setup.go` (`ValidateProposal(&in.Proposal, false)` -> `ValidateProposal(&in.Proposal, false, nil)`).

- [ ] **Step 5: `CheckFolderProposal` si `MembersIn`**

In `proposal.go`:

```go
// CheckFolderProposal: in modul folder, folders lipseste sau contine exact
// folderul analizat (folderPath, deja curatat).
func CheckFolderProposal(p *Proposal, folderPath string) error {
	if len(p.Folders) == 0 {
		return nil
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
```

- [ ] **Step 6: GREEN si tot restul**

Run: `go test ./internal/setup/ && go vet ./... && go test ./...`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/setup internal/store/projects.go internal/web/api_setup.go
git commit -m "setup: propunere cu agenti existenti (sef extern, nume luate, folder unic)"
```

---

### Task 2: Store: agenti noi intr-un proiect existent

**Files:**
- Modify: `internal/store/agents.go` (AddAgents)
- Test: `internal/store/folders_test.go`

**Interfaces:**
- Consumes: `NewMember.ParentID/ReviewerID` (Task 1)
- Produces: `func (s *Store) AddAgents(projectID, folderID int64, team []NewMember, folderContext *string) ([]Agent, error)`

- [ ] **Step 1: Testele**

In `internal/store/folders_test.go`:

```go
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
```

- [ ] **Step 2: Confirma RED**

Run: `go test ./internal/store/`
Expected: eroare de compilare (`AddAgents`).

- [ ] **Step 3: `AddAgents`**

In `internal/store/agents.go`:

```go
// AddAgents adauga o echipa noua intr-un proiect existent, toata in folderul
// folderID, intr-o singura tranzactie (la orice eroare nu ramane nimic).
// Legaturile: Parent/Reviewer = index in team; cu -1, ParentID/ReviewerID pot
// numi un agent existent al proiectului. Agentii existenti nu se modifica.
// folderContext != nil inlocuieste contextul folderului.
// Validarea (nume, cicluri) o face apelantul; aici: ErrConflict pentru un nume
// luat, LinkError pentru un folder sau un agent din alt proiect.
func (s *Store) AddAgents(projectID, folderID int64, team []NewMember, folderContext *string) ([]Agent, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var position int
	err = tx.QueryRow(`SELECT position FROM project_folders WHERE id = ? AND project_id = ?`, folderID, projectID).Scan(&position)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, LinkError("the chosen folder is not in this project")
	}
	if err != nil {
		return nil, err
	}
	var folder *int64 // primul folder = NULL, ca peste tot
	if position > 0 {
		folder = &folderID
	}

	agents := make([]Agent, len(team))
	for i, m := range team {
		a := m.Agent
		a.ID, a.ProjectID, a.ParentID, a.ReviewerID, a.Detached, a.FolderID = 0, projectID, nil, nil, false, folder
		if err := insertAgent(tx, &a); err != nil {
			return nil, err
		}
		agents[i] = a
	}
	// link: index in echipa noua, altfel agentul existent dat (verificat ca e din proiect)
	link := func(i int, existing *int64) (*int64, error) {
		if i >= 0 && i < len(agents) {
			id := agents[i].ID
			return &id, nil
		}
		if existing == nil {
			return nil, nil
		}
		if !inProject(tx, *existing, projectID) {
			return nil, LinkError("the chosen boss or reviewer is not in this project")
		}
		return existing, nil
	}
	for i, m := range team {
		parent, err := link(m.Parent, m.ParentID)
		if err != nil {
			return nil, err
		}
		reviewer, err := link(m.Reviewer, m.ReviewerID)
		if err != nil {
			return nil, err
		}
		agents[i].ParentID, agents[i].ReviewerID = parent, reviewer
		if _, err := tx.Exec(`UPDATE agents SET parent_id = ?, reviewer_id = ? WHERE id = ?`,
			nullPtr(parent), nullPtr(reviewer), agents[i].ID); err != nil {
			return nil, err
		}
	}
	if folderContext != nil {
		if _, err := tx.Exec(`UPDATE project_folders SET context = ? WHERE id = ?`, *folderContext, folderID); err != nil {
			return nil, err
		}
	}
	return agents, tx.Commit()
}
```

(`inProject(q queryer, ...)` exista deja si `*sql.Tx` implementeaza `QueryRow`.)

- [ ] **Step 4: GREEN**

Run: `go test ./internal/store/ && go vet ./...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/store
git commit -m "store: AddAgents adauga o echipa noua intr-un proiect existent"
```

---

### Task 3: Setup: sesiune in modul folder

**Files:**
- Modify: `internal/setup/manager.go` (Session, StartFolder, args, propose)
- Modify: `internal/setup/prompt.go` (folderSystemPrompt, folderTurnPrompt)
- Test: `internal/setup/manager_test.go`

**Interfaces:**
- Consumes: `ValidateProposal(p, strict, external)`, `CheckFolderProposal` (Task 1)
- Produces:
  - `Session.ProjectID, FolderID int64` (JSON `projectId`, `folderId`)
  - `type FolderInput struct { Project store.Project; Folder store.Folder; Team []store.Agent }`
  - `func (m *Manager) StartFolder(in FolderInput, description, model string) (Session, error)`

- [ ] **Step 1: Testele**

In `manager_test.go` (importa `foreman/internal/store`):

```go
func folderInput(t *testing.T) (FolderInput, string, string) {
	client, landing := t.TempDir(), t.TempDir()
	return FolderInput{
		Project: store.Project{ID: 3, Name: "Probe", Goal: "Ship v2", Context: "Shared API", Folders: []store.Folder{
			{ID: 1, Name: "Client", Path: client}, {ID: 2, Name: "Landing", Path: landing},
		}},
		Folder: store.Folder{ID: 2, Name: "Landing", Path: landing},
		Team:   []store.Agent{{ID: 7, Name: "Lead", Role: "Lead"}, {ID: 8, Name: "Rook", Role: "Backend", ParentID: ptr64(7)}},
	}, client, landing
}

func ptr64(v int64) *int64 { return &v }

func TestStartFolder(t *testing.T) {
	m, f := newTest(t)
	in, client, landing := folderInput(t)
	s, err := m.StartFolder(in, "a landing page for pricing", "sonnet")
	if err != nil {
		t.Fatal(err)
	}
	spec := f.specs[0]
	if spec.Dir != landing || !hasArgPair(spec.Args, "--add-dir", client) || s.ProjectID != 3 || s.FolderID != 2 || s.Title != "Probe · Landing" {
		t.Fatalf("spec dir=%s session=%+v", spec.Dir, s)
	}
	for _, want := range []string{"Project: Probe", "Folder to set up: Landing (" + landing + ")", "- Rook: Backend", "reports to Lead", "a landing page for pricing"} {
		if !strings.Contains(spec.Prompt, want) {
			t.Errorf("turn prompt fara %q:\n%s", want, spec.Prompt)
		}
	}
	if sys := argAfter(spec.Args, "--append-system-prompt"); !strings.Contains(sys, "EXISTING project") {
		t.Fatalf("system prompt fara modul folder")
	}

	// propunerea: sef existent ok; nume luat sau alt folder refuzate; titlul ramane
	bk := m.Wrap(innerBackend{})
	c, _ := bk.Resolve(tokenOf(t, spec))
	taken := `{"name":"Probe","agents":[{"name":"rook","role":"x","reportsTo":"Lead"}]}`
	if _, err := bk.ProposeSetup(c, json.RawMessage(taken)); err == nil || !strings.Contains(err.Error(), "already an agent") {
		t.Fatalf("nume luat: %v", err)
	}
	wrongFolder := `{"name":"Probe","folders":[{"path":"` + client + `"}],"agents":[{"name":"Lia","role":"x","reportsTo":"Lead"}]}`
	if _, err := bk.ProposeSetup(c, json.RawMessage(wrongFolder)); err == nil {
		t.Fatal("alt folder trebuia refuzat")
	}
	okProp := `{"name":"Probe","folders":[{"path":"` + landing + `","context":"Hugo"}],"agents":[{"name":"Lia","role":"Landing lead","reportsTo":"Lead"}]}`
	if _, err := bk.ProposeSetup(c, json.RawMessage(okProp)); err != nil {
		t.Fatal(err)
	}
	if got, _ := m.Get(s.ID); got.Title != "Probe · Landing" || got.Proposal == nil {
		t.Fatalf("%+v", got)
	}
}

func TestStartFolderValidation(t *testing.T) {
	m, _ := newTest(t)
	in, _, _ := folderInput(t)
	if _, err := m.StartFolder(in, "", "gpt-5"); err == nil {
		t.Fatal("model invalid")
	}
	in.Folder.Path = filepath.Join(in.Folder.Path, "nu-exista")
	if _, err := m.StartFolder(in, "", "sonnet"); err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("folder lipsa: %v", err)
	}
}

// argAfter intoarce valoarea de dupa un flag
func argAfter(args []string, flag string) string {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == flag {
			return args[i+1]
		}
	}
	return ""
}
```

- [ ] **Step 2: Confirma RED**

Run: `go test ./internal/setup/`
Expected: eroare de compilare (`StartFolder`, `FolderInput`, `ProjectID`).

- [ ] **Step 3: Sesiunea si `StartFolder`**

In `Session` adauga dupa `Model`:

```go
	ProjectID int64 `json:"projectId"` // 0 = proiect nou; altfel modul folder (agenti pentru un folder existent)
	FolderID  int64 `json:"folderId"`
```

si printre campurile private:

```go
	existing []string // modul folder: numele agentilor care exista deja
	brief    string   // modul folder: proiectul si echipa, pentru mesajul analistului
```

Adauga importul `foreman/internal/store`. Tipul si functia:

```go
// FolderInput: proiectul existent, folderul analizat si echipa de acum.
type FolderInput struct {
	Project store.Project
	Folder  store.Folder
	Team    []store.Agent
}

// StartFolder porneste analistul pentru un folder dintr-un proiect existent:
// propune contextul folderului si agenti noi pentru el (modul folder).
func (m *Manager) StartFolder(in FolderInput, description, model string) (Session, error) {
	description = strings.TrimSpace(description)
	if err := tooLong("description", description, 4000); err != nil {
		return Session{}, err
	}
	if !models.Valid(model) {
		return Session{}, ValidationError("unknown model: " + model)
	}
	dir := filepath.Clean(in.Folder.Path)
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return Session{}, ValidationError("the folder does not exist: " + dir)
	}
	existing := make([]string, 0, len(in.Team))
	for _, a := range in.Team {
		existing = append(existing, a.Name)
	}
	m.cleanupOld()
	s := &Session{ID: randomHex(8), RunID: runner.NewID(), Title: in.Project.Name + " · " + in.Folder.Name,
		Workdirs: []string{dir}, Description: description, Model: model,
		ProjectID: in.Project.ID, FolderID: in.Folder.ID,
		Status: StatusRunning, CreatedAt: time.Now(), token: randomHex(16),
		existing: existing, brief: folderBrief(in)}
	m.mu.Lock()
	m.sessions[s.ID] = s
	m.tokens[s.token] = s.ID
	m.mu.Unlock()

	args := m.args(s, model)
	for _, f := range in.Project.Folders {
		if filepath.Clean(f.Path) != dir {
			args = append(args, "--add-dir", f.Path) // celelalte foldere: doar citire
		}
	}
	spec := runner.Spec{ID: s.RunID, Key: "setup-" + s.ID, Dir: dir, Args: args, Prompt: turnPrompt(s)}
	if _, err := m.starter.Start(spec, func(o runner.Outcome) { m.finish(s.ID, o) }); err != nil {
		m.finish(s.ID, runner.Outcome{RunID: s.RunID, Status: "failed", Error: err.Error()})
		return Session{}, err
	}
	return m.snapshot(s), nil
}
```

In `args`, system prompt-ul depinde de mod:

```go
	system := systemPrompt
	if s.ProjectID != 0 {
		system += "\n\n" + folderSystemPrompt
	}
```

si `"--append-system-prompt", system,` in lista. (`s.Workdirs` are un singur element in modul folder, deci bucla existenta de `--add-dir` nu adauga nimic; celelalte foldere le adauga `StartFolder`.)

- [ ] **Step 4: Prompturile (`prompt.go`)**

```go
// folderSystemPrompt se adauga in modul folder (agenti pentru un folder al unui proiect existent).
const folderSystemPrompt = `Folder mode: you are adding ONE folder to an EXISTING project. The existing team is listed in the message; do not change, repeat or rename existing agents.
- Propose only new agents that work in this folder: usually one sub-lead and 1-3 workers.
- The sub-lead reports to an existing agent (usually the root lead): put its exact name in reportsTo. New names must not match existing ones.
- Put exactly this folder in folders (its path as given), with a short name and its own context (stack, build/test/run commands, conventions). If it has a CLAUDE.md, base the context on it.
- name: repeat the project name; goal and context of the project are ignored.`

// folderBrief: proiectul, folderele si echipa existenta, pentru mesajul analistului.
func folderBrief(in FolderInput) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Project: %s\n", in.Project.Name)
	if g := strings.TrimSpace(in.Project.Goal); g != "" {
		fmt.Fprintf(&b, "Goal: %s\n", g)
	}
	if c := strings.TrimSpace(in.Project.Context); c != "" {
		fmt.Fprintf(&b, "Shared context:\n%s\n", c)
	}
	fmt.Fprintf(&b, "Folder to set up: %s (%s)\n", in.Folder.Name, in.Folder.Path)
	for _, f := range in.Project.Folders {
		if f.ID != in.Folder.ID {
			fmt.Fprintf(&b, "Other folder (read only): %s (%s)\n", f.Name, f.Path)
		}
	}
	names := map[int64]string{}
	for _, a := range in.Team {
		names[a.ID] = a.Name
	}
	folderName := func(a store.Agent) string { return store.HomeFolder(a, in.Project.Folders).Name }
	b.WriteString("Existing team:")
	for _, a := range in.Team {
		line := "\n- " + a.Name
		if a.Role != "" {
			line += ": " + a.Role
		}
		line += ", folder: " + folderName(a)
		if a.ParentID != nil {
			line += ", reports to " + names[*a.ParentID]
		} else if !a.Detached {
			line += ", reports to the user"
		}
		if r := strings.TrimSpace(a.Responsibilities); r != "" {
			line += ", takes: " + r
		}
		b.WriteString(line)
	}
	return b.String()
}
```

`turnPrompt` incepe cu modul folder:

```go
func turnPrompt(s *Session) string {
	if s.ProjectID != 0 {
		want := "The user did not describe what these agents should do; infer it from the folder."
		if s.Description != "" {
			want = "What the new agents should do: " + s.Description
		}
		return s.brief + "\n" + want
	}
	// ... restul neschimbat
```

Adauga importul `fmt` in `prompt.go`.

- [ ] **Step 5: `propose` in modul folder**

In `propose`, citeste sesiunea inainte de validare (sub lock) si valideaza dupa mod:

```go
func (m *Manager) propose(id string, raw json.RawMessage) (string, error) {
	var p Proposal
	if err := json.Unmarshal(raw, &p); err != nil {
		return "", ValidationError("the proposal is not valid JSON: " + err.Error())
	}
	m.mu.Lock()
	s, ok := m.sessions[id]
	var running, folderMode bool
	var workdirs, existing []string
	if ok {
		running, workdirs, existing, folderMode = s.Status == StatusRunning, s.Workdirs, s.existing, s.ProjectID != 0
	}
	m.mu.Unlock()
	if !ok || !running {
		return "", ErrNotFound
	}
	// Workdirs si existing nu se schimba dupa pornire
	if err := ValidateProposal(&p, true, existing); err != nil {
		return "", err
	}
	check := CheckFolderPaths(&p, workdirs)
	if folderMode {
		check = CheckFolderProposal(&p, workdirs[0])
	}
	if check != nil {
		return "", check
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if s.Status != StatusRunning {
		return "", ErrNotFound
	}
	s.Proposal = &p // o propunere noua o inlocuieste pe cea veche
	if !folderMode {
		s.Title = p.Name // in modul folder titlul ramane "<proiect> · <folder>"
	}
	return "Proposal received. You are done: do nothing else.", nil
}
```

(`ValidateProposal` cu `existing` nil se comporta exact ca inainte.)

- [ ] **Step 6: GREEN**

Run: `go test -race ./internal/setup/ && go vet ./... && go test ./...`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/setup
git commit -m "setup: modul folder (StartFolder, prompturi cu echipa existenta, validare)"
```

---

### Task 4: Web: pornirea analizei si "Add agents"

**Files:**
- Modify: `internal/web/api_setup.go` (startFolderSetup, addAgentsFromSetup)
- Modify: `internal/web/server.go` (rute)
- Test: `internal/web/setup_test.go`

**Interfaces:**
- Consumes: `wizard.StartFolder`, `wizard.FolderInput`, `ValidateProposal(..., external)`, `Proposal.MembersIn`, `store.AddAgents`
- Produces:
  - `POST /api/projects/{id}/folders/{fid}/setup` `{description, model}` -> 201 sesiunea
  - `POST /api/projects/{id}/agents/setup` `{setupId, folderId, updateContext, proposal}` -> 201 agentii noi

- [ ] **Step 1: Testele**

In `internal/web/setup_test.go` (importa `strconv` daca lipseste; `itoa` exista):

```go
func TestFolderSetupFlow(t *testing.T) {
	e := withSetup(t)
	client, landing := t.TempDir(), t.TempDir()
	_, raw := e.do(t, "POST", "/api/projects", map[string]any{"name": "Probe", "folders": []map[string]any{
		{"name": "Client", "path": client}, {"name": "Landing", "path": landing},
	}})
	var p store.Project
	json.Unmarshal(raw, &p)
	d := e.folders(t, p.ID)
	landingID, leadID := d.Folders[1].ID, d.Agents[0].ID

	// pornire
	code, raw := e.do(t, "POST", "/api/projects/"+itoa(p.ID)+"/folders/"+itoa(landingID)+"/setup", map[string]any{"description": "pricing page", "model": "haiku"})
	var s struct {
		ID        string
		ProjectID int64 `json:"projectId"`
		FolderID  int64 `json:"folderId"`
		Title     string
	}
	json.Unmarshal(raw, &s)
	if code != 201 || s.ProjectID != p.ID || s.FolderID != landingID || s.Title != "Probe · Landing" {
		t.Fatalf("%d %s", code, raw)
	}
	if code, _ := e.do(t, "POST", "/api/projects/"+itoa(p.ID)+"/folders/99999/setup", map[string]any{"model": "haiku"}); code != 404 {
		t.Fatalf("folder necunoscut: %d", code)
	}

	// Add agents: Lia sub Lead-ul existent, Max sub Lia; contextul folderului actualizat
	proposal := map[string]any{"name": "Probe", "folders": []map[string]any{{"path": landing, "name": "Landing", "context": "Hugo"}},
		"agents": []map[string]any{
			{"name": "Lia", "role": "Landing lead", "model": "sonnet", "reportsTo": "Lead"},
			{"name": "Max", "role": "Web dev", "model": "haiku", "reportsTo": "Lia", "tools": []string{"write", "shell"}},
		}}
	path := "/api/projects/" + itoa(p.ID) + "/agents/setup"
	code, raw = e.do(t, "POST", path, map[string]any{"setupId": s.ID, "folderId": landingID, "updateContext": true, "proposal": proposal})
	if code != 201 {
		t.Fatalf("add: %d %s", code, raw)
	}
	d = e.folders(t, p.ID)
	if len(d.Agents) != 3 || d.Folders[1].Context != "Hugo" {
		t.Fatalf("dupa add: %+v", d)
	}
	for _, a := range d.Agents[1:] {
		if a.FolderID == nil || *a.FolderID != landingID {
			t.Fatalf("agent nou in alt folder: %+v", a)
		}
	}
	lead, _ := e.st.GetAgent(leadID)
	lia, _ := e.st.FindAgentByName(p.ID, "Lia")
	if lia.ParentID == nil || *lia.ParentID != lead.ID {
		t.Fatalf("Lia sub Lead: %+v", lia.ParentID)
	}
	// sesiunea dispare din lista
	_, raw = e.do(t, "GET", "/api/setup", nil)
	if strings.Contains(string(raw), s.ID) {
		t.Fatal("sesiunea trebuia ascunsa")
	}

	// a doua oara: numele sunt luate -> 400, nimic nou
	code, _ = e.do(t, "POST", path, map[string]any{"folderId": landingID, "proposal": proposal})
	if code != 400 {
		t.Fatalf("nume luate: %d", code)
	}
	if d := e.folders(t, p.ID); len(d.Agents) != 3 {
		t.Fatalf("s-au creat agenti: %d", len(d.Agents))
	}
	// fara updateContext: contextul ramane
	proposal["agents"] = []map[string]any{{"name": "Zed", "role": "x", "model": "haiku", "reportsTo": "Lead"}}
	proposal["folders"] = []map[string]any{{"path": landing, "context": "altceva"}}
	if code, raw := e.do(t, "POST", path, map[string]any{"folderId": landingID, "proposal": proposal}); code != 201 {
		t.Fatalf("%d %s", code, raw)
	}
	if d := e.folders(t, p.ID); d.Folders[1].Context != "Hugo" {
		t.Fatal("contextul nu trebuia schimbat")
	}
}
```

- [ ] **Step 2: Confirma RED**

Run: `go test ./internal/web/ -run FolderSetup`
Expected: FAIL (404 pe rutele noi).

- [ ] **Step 3: Handler-ele**

In `internal/web/api_setup.go`:

```go
// projectFolder incarca proiectul si folderul {fid} din el (404 daca lipsesc).
func (s *Server) projectFolder(w http.ResponseWriter, r *http.Request, folderID int64) (store.Project, store.Folder, bool) {
	pid, err := pathID(r)
	if err != nil {
		fail(w, err)
		return store.Project{}, store.Folder{}, false
	}
	p, err := s.st.GetProject(pid)
	if err != nil {
		fail(w, err)
		return store.Project{}, store.Folder{}, false
	}
	for _, f := range p.Folders {
		if f.ID == folderID {
			return p, f, true
		}
	}
	writeErr(w, http.StatusNotFound, "the folder is not in this project; reload the page")
	return store.Project{}, store.Folder{}, false
}

// startFolderSetup: analistul propune agenti pentru un folder al unui proiect existent.
func (s *Server) startFolderSetup(w http.ResponseWriter, r *http.Request) {
	if s.wizard == nil {
		writeErr(w, http.StatusNotImplemented, "the wizard is not available")
		return
	}
	fid, err := strconv.ParseInt(r.PathValue("fid"), 10, 64)
	if err != nil {
		fail(w, badRequest("invalid folder id"))
		return
	}
	p, folder, ok := s.projectFolder(w, r, fid)
	if !ok {
		return
	}
	var in struct {
		Description string `json:"description"`
		Model       string `json:"model"`
	}
	if err := decode(r, &in); err != nil {
		fail(w, err)
		return
	}
	team, err := s.st.ListAgents(p.ID)
	if err != nil {
		fail(w, err)
		return
	}
	sess, err := s.wizard.StartFolder(wizard.FolderInput{Project: p, Folder: folder, Team: team}, in.Description, in.Model)
	if err != nil {
		setupFail(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, sess)
}

// addAgentsFromSetup adauga agentii propusi (editati) in proiectul existent.
func (s *Server) addAgentsFromSetup(w http.ResponseWriter, r *http.Request) {
	var in struct {
		SetupID       string          `json:"setupId"`
		FolderID      int64           `json:"folderId"`
		UpdateContext bool            `json:"updateContext"`
		Proposal      wizard.Proposal `json:"proposal"`
	}
	if err := decode(r, &in); err != nil {
		fail(w, err)
		return
	}
	p, folder, ok := s.projectFolder(w, r, in.FolderID)
	if !ok {
		return
	}
	team, err := s.st.ListAgents(p.ID)
	if err != nil {
		fail(w, err)
		return
	}
	names := make([]string, 0, len(team))
	for _, a := range team {
		names = append(names, a.Name)
	}
	// utilizatorul poate bifa si shell/web, deci fara strictTools
	if err := wizard.ValidateProposal(&in.Proposal, false, names); err != nil {
		setupFail(w, err)
		return
	}
	var ctx *string
	if in.UpdateContext {
		c := ""
		if len(in.Proposal.Folders) > 0 {
			c = in.Proposal.Folders[0].Context
		}
		ctx = &c
	}
	agents, err := s.st.AddAgents(p.ID, folder.ID, in.Proposal.MembersIn(team), ctx)
	if errors.Is(err, store.ErrConflict) {
		writeErr(w, http.StatusConflict, "an agent with this name already exists in the project")
		return
	} else if err != nil {
		fail(w, err)
		return
	}
	if s.wizard != nil && in.SetupID != "" {
		s.wizard.Dismiss(in.SetupID) // sesiunea poate sa fi expirat; nu conteaza
	}
	s.hub.Publish(p.ID, "agent", map[string]int64{"id": agents[0].ID})
	s.hub.Publish(p.ID, "project", map[string]int64{"id": p.ID})
	writeJSON(w, http.StatusCreated, agents)
}
```

Importa `strconv` daca lipseste. In `server.go`, langa rutele `/api/setup`:

```go
	mux.HandleFunc("POST /api/projects/{id}/folders/{fid}/setup", s.startFolderSetup)
	mux.HandleFunc("POST /api/projects/{id}/agents/setup", s.addAgentsFromSetup)
```

Observatie: `ValidateProposal` refuza o propunere fara agenti ("at least one agent"), deci `agents[0]` exista mereu.

- [ ] **Step 4: GREEN**

Run: `go test ./internal/web/ && go vet ./... && go test ./...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/web
git commit -m "api: wizard pentru un folder existent si Add agents"
```

---

### Task 5: UI: butonul pe folder, wizard-ul in modul folder, cardul

**Files:**
- Modify: `internal/web/static/js/app.js` (ruta)
- Modify: `internal/web/static/js/projects.js` (buton pe folder, Retry pe card)
- Modify: `internal/web/static/js/setup.js` (modul folder)

**Interfaces:**
- Consumes: rutele din Task 4; `session.projectId`, `session.folderId`
- Produces: ruta `#/setup/folder/<projectId>/<folderId>`

- [ ] **Step 1: Ruta (`app.js`)**

Inaintea rutei `#/setup`:

```js
  { pattern: /^#\/setup\/folder\/(\d+)\/(\d+)$/, view: (root, m) => setupView(root, null, { projectId: Number(m[1]), folderId: Number(m[2]) }) },
```

Actualizeaza comentariul cu rutele: `#/setup/folder/<proiect>/<folder>`.

- [ ] **Step 2: Butonul pe folder (`projects.js`)**

`foldersEditor(initial, canPick, agents)` primeste al patrulea argument, `projectId` (apelul din `projectFormView` trimite `id`). In `add()`, pentru randurile salvate (`f.id` nevid si `projectId` nevid), in bara de actiuni a randului, langa "Remove folder":

```js
    const aiBtn = f.id && projectId ? el("a", { class: "btn small", href: `#/setup/folder/${projectId}/${f.id}`,
      title: "An AI reads this folder and proposes agents for it. Save your changes first." }, "Set up agents with AI") : null;
```

si randul de actiuni devine `el("div", { class: "actions" }, aiBtn, el("span", { class: "grow" }), r.removeBtn)`.

- [ ] **Step 3: Retry pe card (`projects.js`, setupCard)**

In `retry`, porneste analiza dupa mod:

```js
  const retry = act(async () => {
    const next = s.projectId
      ? await api(`/api/projects/${s.projectId}/folders/${s.folderId}/setup`, { method: "POST", body: { description: s.description, model: s.model } })
      : await api("/api/setup", { method: "POST", body: { workdirs: s.workdirs, description: s.description, model: s.model } });
    await api(`/api/setup/${s.id}`, { method: "DELETE" });
    location.hash = `#/setup/${next.id}`;
  });
```

Textul "New project with AI · started ..." devine `(s.projectId ? "Agents with AI" : "New project with AI") + " · started " + ...`.

- [ ] **Step 4: Modul folder in `setup.js`**

1. Semnatura: `export async function setupView(root, id, target)`, unde `target = { projectId, folderId }` sau `null`. Dupa ce sesiunea e incarcata (`if (id)`), daca `session.projectId`, atunci `target = { projectId: session.projectId, folderId: session.folderId }`. Incarcarea sesiunii se muta inaintea desenarii header-ului, ca modul sa fie cunoscut. Pastreaza comportamentul existent de eroare: toast si `#/setup`.

2. In modul folder, incarca proiectul: `const project = target ? await api(`/api/projects/${target.projectId}`) : null;` si `const folder = project?.folders.find((f) => f.id === target.folderId);`. Daca `folder` lipseste: `toast("The folder no longer exists.")` si `location.hash = "#/"`.

3. Header-ul:

```js
  const title = target ? `Set up agents for ${folder.name}` : "New project with AI";
  root.append(
    el("header", { class: "page-head" },
      el("div", {},
        el("div", { class: "crumbs" }, el("a", { href: "#/" }, "Projects"), " / ",
          target && [el("a", { href: `#/p/${project.id}/edit` }, project.name), " / "], title),
        el("h1", {}, title),
        el("p", { class: "muted" }, target
          ? "An AI reads this folder and proposes agents for it. Your existing team is not changed."
          : "Pick a folder (or describe the project); an AI reads it and proposes the project and the team. You decide what to keep."))),
    body);
```

4. `stepChoose`: in modul folder, formularul are doar:
   - `field("What should these agents do?", description)`, cu placeholder "e.g. build and maintain the landing page. Optional.";
   - modelul;
   - Analyze.
   
   Fara lista de foldere. Ramura existenta ramane neschimbata pentru proiect nou. Implementeaza cu `if (target) { ...; return; }` la inceputul lui `stepChoose`, cu propriul `start` care face `Object.assign(form, { description, model })` si `await begin()`.

5. `begin()`:

```js
  async function begin() {
    session = target
      ? await api(`/api/projects/${target.projectId}/folders/${target.folderId}/setup`, { method: "POST", body: { description: form.description, model: form.model } })
      : await api("/api/setup", { method: "POST", body: form });
    history.replaceState(null, "", `#/setup/${session.id}`);
    stepAnalyze();
  }
```

6. `stepReview(p)` in modul folder:
   - **Sectiunea proiectului (Name/Goal/Shared context) nu apare.** Pastreaza `p.rationale` intr-o sectiune "3. Proposal" cu "Why these agents", daca exista.
   - **Sectiunea de folder:** un singur rand, cu numele folderului (text, needitabil), `el("code", {}, folder.path)`, textarea "Context for this folder" (valoarea `p.folders?.[0]?.context || ""`) si casuta:

     ```js
     const updateCtx = el("input", { type: "checkbox", checked: !folder.context.trim() });
     // ...
     el("label", { class: "check" }, updateCtx, "Update folder context")
     ```

     plus hint-ul "Current context is kept unless you tick this." cand folderul are deja context.
   - **Agentii:** fara select Folder (`many` e fals, `folders` in modul folder nu se construieste din `p.folders` pentru selecte). **"Reports to" / "Reviewed by"** contin, dupa agentii noi, un `optgroup` "Existing team" cu `project.agents` (value = numele).
   - **Numele existente** (`const existingNames = new Map(project.agents.map((a) => [a.name.toLowerCase(), a.name]))`). In `sync()`, cand `r.parentName` nu e un agent nou, dar e in `existingNames`, `r.effParent` = numele existent. Inseamna: in bucla de urcare, daca `boss` nu se gaseste in `byName`, verifica `existingNames`. La fel `r.effReviewer` pentru `reviewerName`. Optiunile se construiesc cu `opts(first)` + optgroup; valoarea selectului ramane `r.effParent`.
   - **`drawTree`:** dupa `walk("", 1)`, pentru fiecare sef existent folosit de agentii inclusi (valori `effParent` distincte care sunt in `existingNames`), adauga linia `"  \u2514 " + name + "  (existing)"` si apeleaza `walk(name, 2)`.
   - **Butonul final:** `"Add agents"`, cu `create()` in modul folder:

     ```js
     const res = await api(`/api/projects/${project.id}/agents/setup`, { method: "POST", body: {
       setupId: session?.id, folderId: folder.id, updateContext: updateCtx.checked,
       proposal: { name: project.name, goal: "", context: "", rationale: p.rationale || "",
         folders: [{ path: folder.path, name: folder.name, context: ctxArea.value }], agents },
     } });
     location.hash = `#/p/${project.id}/team`;
     ```

     `agents` se construiesc ca acum, cu `folder: ""`.
   - **"Back":** duce la `stepChoose` (modul folder).

   Pune ramificatiile in functii mici, `reviewFolderSection()` si `createInFolder()`, ca `stepReview` sa ramana lizibil. Respecta regula: campurile optionale sunt `cond ? el(...) : null`.

- [ ] **Step 5: Verificare**

Run: `for f in internal/web/static/js/*.js; do cp "$f" /tmp/fchk.mjs && node --check /tmp/fchk.mjs || echo "FAIL $f"; done && go test ./internal/web/`
Expected: fara FAIL, teste PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/web/static
git commit -m "ui: Set up agents with AI pe folder, wizard in modul folder, Add agents"
```

---

### Task 6: Verificare in browser si CLAUDE.md

- [ ] **Step 1:** Verificarea completa (vet, race, test, cross-compile, `node --check` ca `.mjs`).
- [ ] **Step 2:** Pregateste o instanta de test pe portul 7789, cu `HOME` temporar si un `claude` fals primul in `PATH`. Cand turn prompt-ul contine "Folder to set up:", `claude`-ul fals apeleaza `propose_setup` cu:
  - `folders:[{path:<cwd>, context:"Hugo"}]`;
  - un sub-lider cu `reportsTo:"Lead"` si un worker sub el.
- [ ] **Step 3:** Parcurge fluxul prin CDP:
  1. Creeaza un proiect cu 2 foldere. In Project instructions, butonul "Set up agents with AI" apare pe folderele salvate si lipseste pe un rand nou.
  2. Apasa butonul, apoi Analyze. Verifica pasul 3: sectiunea de folder, "Reports to" cu grupul "Existing team", arborele cu `(existing)`.
  3. Apasa "Add agents". Pe canvas, agentii noi sunt sub Lead si afiseaza „· Landing”; contextul folderului e actualizat.
  4. Pe Projects, cardul arata "Probe · Landing" si dispare dupa Add agents.
  5. Wizard-ul "New project with AI" merge ca inainte.
  
  Fa screenshot-uri la 1440x900 si citeste-le.
- [ ] **Step 4:** In CLAUDE.md, sectiunea "Stare", adauga o linie:

  ```
  Gata (wizard pe folder, spec in docs/superpowers/specs/2026-09-30-foreman-folder-setup-design.md): "Set up agents with AI" pe un folder al unui proiect existent; analistul propune contextul folderului si agenti noi sub un agent existent, adaugati cu store.AddAgents.
  ```

  Apoi commit.
