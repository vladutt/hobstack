# Liderul gestioneaza sarcini recurente — plan de implementare

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** liderii si radacinile pot lista, crea, schimba si sterge sarcini recurente prin tool-uri MCP, schimbarile doar
intr-un run pornit dintr-un mesaj al utilizatorului in chat.

**Architecture:** patru tool-uri MCP noi (`list_recurring`, `create_recurring`, `update_recurring`, `delete_recurring`)
in `internal/mcp`, implementate in `internal/team/recurring_tools.go` peste functiile existente din `team/recurring.go`
(refactorizate in variante fara lock). Regula "doar la cerere" = `t.requestOf(c.AgentID, c.RunID)`, ca la `add_agent`.
Coloana noua `recurring_tasks.created_by` (migrarea 032) si eticheta "Created by" in panoul Recurring.

**Tech Stack:** Go (stdlib, modernc.org/sqlite), JS vanilla.

**Spec:** `docs/superpowers/specs/2026-10-16-hobstack-lead-recurring-design.md`

## Global Constraints

- Tot ce vede utilizatorul sau agentul (UI, erori, descrieri de tool-uri) e in engleza; comentariile din cod sunt in romana.
- `go vet ./...` si `go build -race ./cmd/hobstack` trec; cross-compile `GOOS=darwin GOARCH=arm64` si `GOOS=windows`.
- JS: fiecare fisier schimbat se copiaza ca `.mjs` in scratchpad si trece `node --check`.
- Full autonomy **nu** ocoleste regula "doar la cerere".
- Aparitiile sarcinilor create de lider merg pe drumul de acum (decide utilizatorul sau auto-accept); nu se schimba nimic acolo.
- Titlu obligatoriu, max 200 caractere; descriere max 10000 (trim pe ambele) — o singura copie a regulii, in `team`.
- Fara restart HobStack cat timp `pgrep -f "claude -p"` gaseste agenti.

## Review Focus

1. Un sub-lider (are sef si echipa) nu trebuie sa poata schimba sarcina unei alte echipe, nici sa aleaga "first stage" — testat in Task 3 (`TestRecurringZone`).
2. Un run pornit din notificare / pe task (fara mesaj al utilizatorului) nu trebuie sa poata scrie, nici cu Full autonomy — Task 3 (`TestRecurringToolsNeedChat`).
3. `update_recurring` cu un singur camp nu trebuie sa stearga celelalte campuri (titlu, descriere, auto-accept) — Task 3 (`TestUpdateRecurringFromChat`).
4. `R-<id>` dintr-un alt proiect trebuie tratat ca inexistent (nu se schimba sarcina altui proiect) — Task 3 (`TestRecurringOtherProject`).
5. Numerele trimise ca text (`"every_hours": "3"`) si zilele scrise lung / cu majuscule (`"Wednesday"`, `"FRI"`) se accepta — Task 3 (zile) si Task 4 (`every_hours` text).

---

### Task 1: Store — `created_by` pe sarcinile recurente

**Files:**
- Create: `internal/store/migrations/032_recurring_created_by.sql`
- Modify: `internal/store/recurring.go` (struct, `recurringCols`, `scanRecurring`, `CreateRecurring`)
- Test: `internal/store/recurring_test.go`

**Interfaces:**
- Produces: `store.RecurringTask.CreatedBy *int64` (`json:"createdBy"`), scris de `CreateRecurring`, citit de `GetRecurring` / `ListRecurring`, neschimbat de `SaveRecurring`.

- [ ] **Step 1: Write the failing test** — la sfarsitul lui `internal/store/recurring_test.go`:

```go
func TestRecurringCreatedBy(t *testing.T) {
	s, p, lead := recurringFixture(t)
	next := time.Date(2026, 10, 5, 9, 0, 0, 0, time.UTC)
	daily := schedule.Spec{Kind: schedule.KindDaily, At: "09:00"}
	mine := RecurringTask{ProjectID: p.ID, Title: "Mine", AssigneeID: &lead.ID, Schedule: daily, Enabled: true, NextRunAt: next}
	byLead := RecurringTask{ProjectID: p.ID, Title: "Lead's", AssigneeID: &lead.ID, Schedule: daily, Enabled: true, NextRunAt: next, CreatedBy: &lead.ID}
	for _, r := range []*RecurringTask{&mine, &byLead} {
		if err := s.CreateRecurring(r); err != nil {
			t.Fatal(err)
		}
	}
	got, _ := s.GetRecurring(byLead.ID)
	if got.CreatedBy == nil || *got.CreatedBy != lead.ID {
		t.Fatalf("created_by: %+v", got.CreatedBy)
	}
	// SaveRecurring nu atinge created_by
	got.Title = "Renamed"
	if err := s.SaveRecurring(&got); err != nil {
		t.Fatal(err)
	}
	list, _ := s.ListRecurring(p.ID)
	if list[0].CreatedBy != nil || list[1].CreatedBy == nil || list[1].Title != "Renamed" {
		t.Fatalf("%+v", list)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/store/ -run TestRecurringCreatedBy`
Expected: FAIL la compilare: `unknown field CreatedBy in struct literal of type RecurringTask`

- [ ] **Step 3: Write minimal implementation**

`internal/store/migrations/032_recurring_created_by.sql`:

```sql
-- agentul care a creat sarcina la cererea utilizatorului (NULL = utilizatorul)
ALTER TABLE recurring_tasks ADD COLUMN created_by INTEGER REFERENCES agents(id) ON DELETE SET NULL;
```

In `internal/store/recurring.go`:
- in `RecurringTask`, dupa `LastNote`: `CreatedBy *int64 \`json:"createdBy"\` // agentul care a creat-o la cererea utilizatorului; nil = utilizatorul`
- `recurringCols` se termina cu `last_task_id, last_note, created_at, created_by`
- in `scanRecurring`, `Scan(..., &r.LastNote, &created, &r.CreatedBy)`
- `CreateRecurring`:

```go
	res, err := s.db.Exec(`INSERT INTO recurring_tasks (project_id, title, description, assignee_id, kind, every_hours, at, weekdays,
		auto_accept, enabled, next_run_at, created_at, created_by) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.ProjectID, r.Title, r.Description, nullPtr(r.AssigneeID), r.Schedule.Kind, r.Schedule.EveryHours, r.Schedule.At,
		r.Schedule.Weekdays, r.AutoAccept, r.Enabled, runAt(r.NextRunAt), ts, nullPtr(r.CreatedBy))
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/store/`
Expected: PASS (tot pachetul, inclusiv `TestRecurringCRUD`)

- [ ] **Step 5: Commit**

```bash
git add internal/store/migrations/032_recurring_created_by.sql internal/store/recurring.go internal/store/recurring_test.go
git commit -m "store: recurring_tasks.created_by (migration 032)"
```

---

### Task 2: Team — validarea titlului in `team`, `CreatedBy`, variante fara lock

**Files:**
- Modify: `internal/team/recurring.go`
- Modify: `internal/web/api_recurring.go:60-84` (`recurringInput`)
- Test: `internal/team/recurring_test.go`, `internal/web/recurring_test.go` (existent, trebuie sa ramana verde)

**Interfaces:**
- Consumes: `store.RecurringTask.CreatedBy` (Task 1).
- Produces:
  - `team.RecurringInput.CreatedBy *int64` (folosit doar la creare)
  - `func (in *RecurringInput) clean() error` — trim + limite, `RecurringError`
  - `func (t *Team) createRecurring(projectID int64, in RecurringInput) (store.RecurringTask, error)` — cu `t.work` tinut
  - `func (t *Team) updateRecurring(id int64, in RecurringInput) (store.RecurringTask, error)` — cu `t.work` tinut
  - `func (t *Team) deleteRecurring(r store.RecurringTask) error` — cu `t.work` tinut
  - `CreateRecurring` / `UpdateRecurring` / `DeleteRecurring` raman cu aceeasi semnatura (iau lock-ul si cheama variantele de mai sus).

- [ ] **Step 1: Write the failing test** — in `internal/team/recurring_test.go`:

```go
func TestRecurringInputClean(t *testing.T) {
	f := setup(t)
	var re RecurringError
	for _, title := range []string{"   ", strings.Repeat("x", 201)} {
		if _, err := f.tm.CreateRecurring(f.p.ID, RecurringInput{Title: title, AssigneeID: f.rook.ID, Schedule: daily9, Enabled: true}); !errors.As(err, &re) {
			t.Fatalf("title %q: %v", title, err)
		}
	}
	if _, err := f.tm.CreateRecurring(f.p.ID, RecurringInput{Title: "x", Description: strings.Repeat("d", 10001), AssigneeID: f.rook.ID, Schedule: daily9, Enabled: true}); !errors.As(err, &re) {
		t.Fatalf("description: %v", err)
	}
	by := f.lead.ID
	r, err := f.tm.CreateRecurring(f.p.ID, RecurringInput{Title: "  Tests ", Description: " d ", AssigneeID: f.rook.ID, Schedule: daily9, Enabled: true, CreatedBy: &by})
	if err != nil || r.Title != "Tests" || r.Description != "d" || r.CreatedBy == nil || *r.CreatedBy != f.lead.ID {
		t.Fatalf("%+v %v", r, err)
	}
	if _, err := f.tm.UpdateRecurring(r.ID, RecurringInput{Title: " ", AssigneeID: f.rook.ID, Schedule: daily9, Enabled: true}); !errors.As(err, &re) {
		t.Fatalf("update title: %v", err)
	}
}
```

(adauga `"strings"` la importuri daca lipseste.)

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/team/ -run TestRecurringInputClean`
Expected: FAIL la compilare: `unknown field CreatedBy in struct literal of type RecurringInput`

- [ ] **Step 3: Write minimal implementation** — in `internal/team/recurring.go`:

```go
type RecurringInput struct {
	Title, Description string
	AssigneeID         int64
	Schedule           schedule.Spec
	AutoAccept         bool
	Enabled            bool
	CreatedBy          *int64 // doar la creare: agentul care a creat-o la cererea utilizatorului (nil = utilizatorul)
}

// clean curata titlul si descrierea si verifica limitele (aceleasi pentru UI si pentru tool-urile MCP)
func (in *RecurringInput) clean() error {
	in.Title, in.Description = strings.TrimSpace(in.Title), strings.TrimSpace(in.Description)
	switch {
	case in.Title == "":
		return RecurringError("title is required")
	case utf8.RuneCountInString(in.Title) > 200:
		return RecurringError("title: at most 200 characters")
	case utf8.RuneCountInString(in.Description) > 10000:
		return RecurringError("description: at most 10000 characters")
	}
	return nil
}

func (t *Team) CreateRecurring(projectID int64, in RecurringInput) (store.RecurringTask, error) {
	t.work.Lock()
	defer t.work.Unlock()
	return t.createRecurring(projectID, in)
}

// createRecurring: ca CreateRecurring, cu t.work tinut
func (t *Team) createRecurring(projectID int64, in RecurringInput) (store.RecurringTask, error) {
	if err := in.clean(); err != nil {
		return store.RecurringTask{}, err
	}
	who, err := t.checkRecurring(projectID, in, in.Enabled)
	if err != nil {
		return store.RecurringTask{}, err
	}
	r := store.RecurringTask{ProjectID: projectID, Title: in.Title, Description: in.Description, AssigneeID: who,
		Schedule: in.Schedule, AutoAccept: in.AutoAccept, Enabled: in.Enabled, NextRunAt: in.Schedule.Next(t.now()),
		CreatedBy: in.CreatedBy}
	if err := t.st.CreateRecurring(&r); err != nil {
		return store.RecurringTask{}, err
	}
	t.emit(projectID, "recurring", r)
	return r, nil
}

func (t *Team) UpdateRecurring(id int64, in RecurringInput) (store.RecurringTask, error) {
	t.work.Lock()
	defer t.work.Unlock()
	return t.updateRecurring(id, in)
}

// updateRecurring: ca UpdateRecurring, cu t.work tinut (CreatedBy din in se ignora)
func (t *Team) updateRecurring(id int64, in RecurringInput) (store.RecurringTask, error) {
	if err := in.clean(); err != nil {
		return store.RecurringTask{}, err
	}
	r, err := t.st.GetRecurring(id)
	// ... restul corpului de acum al lui UpdateRecurring, neschimbat, de la `if err != nil` pana la `return r, nil`
}

func (t *Team) DeleteRecurring(id int64) error {
	t.work.Lock()
	defer t.work.Unlock()
	r, err := t.st.GetRecurring(id)
	if err != nil {
		return err
	}
	return t.deleteRecurring(r)
}

// deleteRecurring: sterge sarcina (task-urile create raman), cu t.work tinut
func (t *Team) deleteRecurring(r store.RecurringTask) error {
	if err := t.st.DeleteRecurring(r.ID); err != nil {
		return err
	}
	t.emit(r.ProjectID, "recurring", r)
	return nil
}
```

Importuri noi in `recurring.go`: `"unicode/utf8"`.

In `internal/web/api_recurring.go`, `recurringInput` nu mai face trim / limite (le face `team`, iar `RecurringError` ajunge 400 prin `fail`):

```go
	if err := decode(r, &in); err != nil {
		return team.RecurringInput{}, err
	}
	return team.RecurringInput{Title: in.Title, Description: in.Description, AssigneeID: in.AssigneeID, Schedule: in.Schedule,
		AutoAccept: in.AutoAccept, Enabled: in.Enabled}, nil
```

Scoate importurile ramase nefolosite din `api_recurring.go` (probabil `strings`; `go build` le arata).

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/team/ ./internal/web/ -run Recurring`
Expected: PASS (inclusiv `TestRecurringAPI`, care trimite titlu gol / 201 caractere si asteapta 400)

- [ ] **Step 5: Commit**

```bash
git add internal/team/recurring.go internal/team/recurring_test.go internal/web/api_recurring.go
git commit -m "team: recurring title/description checks in one place, CreatedBy, lock-free helpers"
```

---

### Task 3: Team — tool-urile pentru sarcini recurente

**Files:**
- Modify: `internal/mcp/server.go` (doar tipurile de intrare, langa `AddAgentInput`)
- Create: `internal/team/recurring_tools.go`
- Test: `internal/team/recurring_tools_test.go`

**Interfaces:**
- Consumes: `createRecurring`, `updateRecurring`, `deleteRecurring`, `RecurringInput.CreatedBy` (Task 2); `t.requestOf`, `canAddAgents` (existente).
- Produces (folosite de Task 4):

```go
// in pachetul mcp
type RecurringSchedule struct {
	Kind       string
	EveryHours int
	At         string
	Weekdays   []string // "mon", "Wednesday"...
}
type RecurringArgs struct {
	Title, Description, Assignee string
	Schedule                     RecurringSchedule
	AutoAccept                   bool
}
type RecurringChange struct {
	ID                           string // "R-3" sau "3"
	Title, Description, Assignee *string
	Schedule                     *RecurringSchedule
	AutoAccept, Enabled          *bool
}

// in pachetul team
func (t *Team) ListRecurringFor(c mcp.Caller) (string, error)
func (t *Team) CreateRecurringFor(c mcp.Caller, in mcp.RecurringArgs) (string, error)
func (t *Team) UpdateRecurringFor(c mcp.Caller, ch mcp.RecurringChange) (string, error)
func (t *Team) DeleteRecurringFor(c mcp.Caller, ref string) (string, error)
```

- [ ] **Step 1: Add the input types** — in `internal/mcp/server.go`, dupa `AddAgentInput`:

```go
// RecurringSchedule e programul din argumentele tool-urilor *_recurring (zilele ca nume, ex. "mon").
type RecurringSchedule struct {
	Kind       string
	EveryHours int
	At         string
	Weekdays   []string
}

// RecurringArgs sunt argumentele lui create_recurring.
type RecurringArgs struct {
	Title, Description, Assignee string
	Schedule                     RecurringSchedule
	AutoAccept                   bool
}

// RecurringChange sunt argumentele lui update_recurring: nil = campul nu se schimba.
type RecurringChange struct {
	ID                           string
	Title, Description, Assignee *string
	Schedule                     *RecurringSchedule
	AutoAccept, Enabled          *bool
}
```

- [ ] **Step 2: Write the failing tests** — `internal/team/recurring_tools_test.go`:

```go
package team

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"hobstack/internal/mcp"
	"hobstack/internal/store"
)

// chatCaller: identitatea MCP a unui run al agentului pornit dintr-un mesaj al utilizatorului (ce verifica requestOf)
func chatCaller(f *fixture, a store.Agent) mcp.Caller {
	runID := fmt.Sprintf("chat-%d", a.ID)
	f.tm.mu.Lock()
	f.tm.active[a.ID] = activeRun{runID: runID, projectID: a.ProjectID,
		request: &store.Message{ID: 1, ProjectID: a.ProjectID, AgentID: a.ID, Author: "user", Text: "please"}}
	f.tm.mu.Unlock()
	return mcp.Caller{ProjectID: a.ProjectID, AgentID: a.ID, RunID: runID, CanAddAgents: true}
}

func ptrOf[T any](v T) *T { return &v }

var (
	dailyAt9 = mcp.RecurringSchedule{Kind: "daily", At: "09:00"}
	mwf9     = mcp.RecurringSchedule{Kind: "weekly", At: "09:00", Weekdays: []string{"mon", "Wednesday", "FRI"}}
)

func onlyRecurring(t *testing.T, f *fixture) store.RecurringTask {
	t.Helper()
	list, err := f.st.ListRecurring(f.p.ID)
	if err != nil || len(list) != 1 {
		t.Fatalf("%+v %v", list, err)
	}
	return list[0]
}

func TestRecurringToolsNeedChat(t *testing.T) {
	f := setup(t)
	autonomous(t, f) // Full autonomy nu ajunge
	c := mcp.Caller{ProjectID: f.p.ID, AgentID: f.lead.ID, RunID: "notification", IsLead: true, CanAddAgents: true}
	if _, err := f.tm.CreateRecurringFor(c, mcp.RecurringArgs{Title: "Tests", Schedule: dailyAt9}); !errors.Is(err, errNotAsked) {
		t.Fatalf("create: %v", err)
	}
	if _, err := f.tm.UpdateRecurringFor(c, mcp.RecurringChange{ID: "R-1", Title: ptrOf("x")}); !errors.Is(err, errNotAsked) {
		t.Fatalf("update: %v", err)
	}
	if _, err := f.tm.DeleteRecurringFor(c, "R-1"); !errors.Is(err, errNotAsked) {
		t.Fatalf("delete: %v", err)
	}
	if text, err := f.tm.ListRecurringFor(c); err != nil || text != "No recurring tasks in this project." {
		t.Fatalf("list: %q %v", text, err)
	}
	if list, _ := f.st.ListRecurring(f.p.ID); len(list) != 0 {
		t.Fatalf("nothing may be created: %+v", list)
	}
}

func TestCreateRecurringFromChat(t *testing.T) {
	f := setup(t)
	c := chatCaller(f, f.lead)
	text, err := f.tm.CreateRecurringFor(c, mcp.RecurringArgs{Title: "  Tests ", Description: "run them", Assignee: "rook", Schedule: mwf9, AutoAccept: true})
	if err != nil {
		t.Fatal(err)
	}
	r := onlyRecurring(t, f)
	if r.Title != "Tests" || r.AssigneeID == nil || *r.AssigneeID != f.rook.ID || r.Schedule.Weekdays != 1<<1|1<<3|1<<5 ||
		!r.AutoAccept || !r.Enabled || r.CreatedBy == nil || *r.CreatedBy != f.lead.ID {
		t.Fatalf("%+v", r)
	}
	if !strings.HasPrefix(text, fmt.Sprintf(`Created R-%d "Tests" — Mon, Wed, Fri at 09:00 — Rook — on — next `, r.ID)) ||
		!strings.Contains(text, "auto-accept") || !strings.Contains(text, "created by "+f.lead.Name) {
		t.Fatalf("%q", text)
	}
	// fara assignee: liderul insusi
	if _, err := f.tm.CreateRecurringFor(c, mcp.RecurringArgs{Title: "Mine", Schedule: dailyAt9}); err != nil {
		t.Fatal(err)
	}
	list, _ := f.st.ListRecurring(f.p.ID)
	if *list[1].AssigneeID != f.lead.ID {
		t.Fatalf("default assignee: %+v", list[1])
	}
	var re RecurringError
	if _, err := f.tm.CreateRecurringFor(c, mcp.RecurringArgs{Title: "x", Schedule: mcp.RecurringSchedule{Kind: "weekly", At: "09:00", Weekdays: []string{"funday"}}}); !errors.As(err, &re) || !strings.Contains(err.Error(), "unknown day") {
		t.Fatalf("bad day: %v", err)
	}
	if _, err := f.tm.CreateRecurringFor(c, mcp.RecurringArgs{Title: "x", Schedule: mcp.RecurringSchedule{Kind: "daily", At: "25:00"}}); !errors.As(err, &re) {
		t.Fatalf("bad time: %v", err)
	}
	if _, err := f.tm.CreateRecurringFor(c, mcp.RecurringArgs{Title: "x", Assignee: "Nobody", Schedule: dailyAt9}); err == nil || !strings.Contains(err.Error(), `no agent named "Nobody"`) {
		t.Fatalf("unknown agent: %v", err)
	}
}

func TestRecurringZone(t *testing.T) {
	f := setup(t)
	sub := f.agent(t, "Sub", &f.lead, nil)
	f.agent(t, "Kid", &sub, nil)
	cs := chatCaller(f, sub)
	if _, err := f.tm.CreateRecurringFor(cs, mcp.RecurringArgs{Title: "Kid's", Assignee: "kid", Schedule: dailyAt9}); err != nil {
		t.Fatalf("own team: %v", err)
	}
	for _, who := range []string{"Rook", f.lead.Name, "first stage"} {
		if _, err := f.tm.CreateRecurringFor(cs, mcp.RecurringArgs{Title: "x", Assignee: who, Schedule: dailyAt9}); err == nil || !strings.Contains(err.Error(), "direct team") {
			t.Fatalf("%s: %v", who, err)
		}
	}
	// sarcina echipei radacinii (Rook): sub-liderul nu o poate schimba sau sterge
	cl := chatCaller(f, f.lead)
	if _, err := f.tm.CreateRecurringFor(cl, mcp.RecurringArgs{Title: "Rook's", Assignee: "Rook", Schedule: dailyAt9}); err != nil {
		t.Fatal(err)
	}
	list, _ := f.st.ListRecurring(f.p.ID)
	ref := fmt.Sprintf("R-%d", list[1].ID)
	if _, err := f.tm.UpdateRecurringFor(cs, mcp.RecurringChange{ID: ref, Title: ptrOf("x")}); err == nil || !strings.Contains(err.Error(), "belongs to another team") {
		t.Fatalf("update: %v", err)
	}
	if _, err := f.tm.DeleteRecurringFor(cs, ref); err == nil || !strings.Contains(err.Error(), "belongs to another team") {
		t.Fatalf("delete: %v", err)
	}
	// nici mutata in echipa lui
	if _, err := f.tm.UpdateRecurringFor(cl, mcp.RecurringChange{ID: ref, Assignee: ptrOf("Kid")}); err == nil || !strings.Contains(err.Error(), "direct team") {
		t.Fatalf("root moves to a grandchild: %v", err)
	}
	// un worker (nici lider, nici radacina) e refuzat chiar din chat
	if _, err := f.tm.CreateRecurringFor(chatCaller(f, f.rook), mcp.RecurringArgs{Title: "x", Schedule: dailyAt9}); err == nil || !strings.Contains(err.Error(), "only a lead") {
		t.Fatalf("worker: %v", err)
	}
}

func TestRecurringFirstStage(t *testing.T) {
	f := setup(t)
	if _, err := f.st.SaveStages(f.p.ID, []store.Stage{{Name: "Dev", Kind: store.KindWork, AgentIDs: []int64{f.rook.ID}}}); err != nil {
		t.Fatal(err)
	}
	c := chatCaller(f, f.lead)
	if _, err := f.tm.CreateRecurringFor(c, mcp.RecurringArgs{Title: "Staged", Assignee: "First stage", Schedule: dailyAt9}); err != nil {
		t.Fatal(err)
	}
	if r := onlyRecurring(t, f); r.AssigneeID != nil {
		t.Fatalf("first stage = no assignee: %+v", r)
	}
	if text, _ := f.tm.ListRecurringFor(c); !strings.Contains(text, "— first stage — on — next ") {
		t.Fatalf("%q", text)
	}
}

func TestUpdateRecurringFromChat(t *testing.T) {
	f := setup(t)
	f.tm.now = func() time.Time { return at("2026-10-05 10:00") }
	c := chatCaller(f, f.lead)
	if _, err := f.tm.CreateRecurringFor(c, mcp.RecurringArgs{Title: "Tests", Description: "d", Assignee: "Rook", Schedule: dailyAt9, AutoAccept: true}); err != nil {
		t.Fatal(err)
	}
	id := onlyRecurring(t, f).ID
	ref := fmt.Sprintf("R-%d", id)
	if _, err := f.tm.UpdateRecurringFor(c, mcp.RecurringChange{ID: ref}); err == nil || !strings.Contains(err.Error(), "nothing to change") {
		t.Fatalf("empty update: %v", err)
	}
	text, err := f.tm.UpdateRecurringFor(c, mcp.RecurringChange{ID: ref, Schedule: &mcp.RecurringSchedule{Kind: "daily", At: "10:00"}})
	if err != nil || !strings.HasPrefix(text, fmt.Sprintf(`Updated R-%d "Tests" — Daily at 10:00`, id)) {
		t.Fatalf("%q %v", text, err)
	}
	r, _ := f.st.GetRecurring(id)
	if r.Title != "Tests" || r.Description != "d" || !r.AutoAccept || *r.AssigneeID != f.rook.ID || !r.NextRunAt.Equal(at("2026-10-06 10:00")) {
		t.Fatalf("partial update kept the rest: %+v", r)
	}
	// id fara prefix, pauza si reluare
	if _, err := f.tm.UpdateRecurringFor(c, mcp.RecurringChange{ID: fmt.Sprint(id), Enabled: ptrOf(false)}); err != nil {
		t.Fatal(err)
	}
	if text, _ := f.tm.ListRecurringFor(c); !strings.Contains(text, "— paused") {
		t.Fatalf("%q", text)
	}
	if _, err := f.tm.UpdateRecurringFor(c, mcp.RecurringChange{ID: ref, Enabled: ptrOf(true)}); err != nil {
		t.Fatal(err)
	}
	if r, _ := f.st.GetRecurring(id); !r.Enabled {
		t.Fatal("resumed")
	}
	text, err = f.tm.DeleteRecurringFor(c, ref)
	if err != nil || text != fmt.Sprintf(`Deleted R-%d "Tests". Tasks it already created stay on the board.`, id) {
		t.Fatalf("%q %v", text, err)
	}
	if _, err := f.st.GetRecurring(id); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("after delete: %v", err)
	}
}

func TestRecurringOtherProject(t *testing.T) {
	f := setup(t)
	other := store.Project{Name: "Other", Workdir: t.TempDir(), MaxParallel: 1}
	otherLead, err := f.st.CreateProject(&other)
	if err != nil {
		t.Fatal(err)
	}
	r, err := f.tm.CreateRecurring(other.ID, RecurringInput{Title: "Theirs", AssigneeID: otherLead.ID, Schedule: daily9, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	c := chatCaller(f, f.lead)
	for _, ref := range []string{fmt.Sprintf("R-%d", r.ID), "R-9999", "abc"} {
		if _, err := f.tm.DeleteRecurringFor(c, ref); err == nil || !strings.Contains(err.Error(), "list_recurring") {
			t.Fatalf("%s: %v", ref, err)
		}
	}
	if _, err := f.st.GetRecurring(r.ID); err != nil {
		t.Fatal("the other project's task must stay")
	}
}
```

Adauga `"time"` la importuri (pentru `TestUpdateRecurringFromChat`). `daily9`, `at` si `autonomous` exista deja in testele pachetului.

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test ./internal/team/ -run 'Recurring(ToolsNeedChat|Zone|FirstStage|OtherProject)|RecurringFromChat'`
Expected: FAIL la compilare: `f.tm.CreateRecurringFor undefined`, `undefined: errNotAsked`

- [ ] **Step 4: Write the implementation** — `internal/team/recurring_tools.go`:

```go
package team

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"hobstack/internal/mcp"
	"hobstack/internal/schedule"
	"hobstack/internal/store"
)

// Tool-urile MCP pentru sarcinile recurente (list/create/update/delete_recurring), pentru lideri si radacini.
// Schimbarile merg doar intr-un run pornit dintr-un mesaj al utilizatorului in chat, si cu Full autonomy
// (spec: docs/superpowers/specs/2026-10-16-hobstack-lead-recurring-design.md).

// errNotAsked: create / update / delete fara cererea utilizatorului din chat
var errNotAsked = errors.New("only when the user asks for it in chat. Suggest it to the user in your reply instead")

// firstStageName: assignee-ul care inseamna prima etapa (AssigneeID 0)
const firstStageName = "first stage"

// dayNames: zilele acceptate in weekdays (scurt si lung, fara majuscule)
var dayNames = map[string]time.Weekday{
	"sun": time.Sunday, "mon": time.Monday, "tue": time.Tuesday, "wed": time.Wednesday,
	"thu": time.Thursday, "fri": time.Friday, "sat": time.Saturday,
	"sunday": time.Sunday, "monday": time.Monday, "tuesday": time.Tuesday, "wednesday": time.Wednesday,
	"thursday": time.Thursday, "friday": time.Friday, "saturday": time.Saturday,
}

// recurringLead: apelantul, daca e (inca) lider sau radacina (din DB); write cere si mesajul utilizatorului.
// Se cheama cu t.work tinut.
func (t *Team) recurringLead(c mcp.Caller, write bool) (store.Agent, []store.Agent, error) {
	agents, err := t.st.ListAgents(c.ProjectID)
	if err != nil {
		return store.Agent{}, nil, err
	}
	lead, ok := store.FindAgent(agents, c.AgentID)
	if !ok {
		return store.Agent{}, nil, errors.New("your agent no longer exists")
	}
	if !canAddAgents(lead, agents) {
		return store.Agent{}, nil, errors.New("only a lead or an agent that reports to the user can manage recurring tasks")
	}
	if write {
		if _, fromChat := t.requestOf(c.AgentID, c.RunID); !fromChat {
			return store.Agent{}, nil, errNotAsked
		}
	}
	return lead, agents, nil
}

// inZone: asignatul (0 = prima etapa) e liderul, un agent din echipa lui directa sau, pentru o radacina, prima etapa
func inZone(lead store.Agent, agents []store.Agent, assigneeID int64) bool {
	if assigneeID == 0 {
		return store.RolesOf(lead, agents).IsRoot
	}
	if assigneeID == lead.ID {
		return true
	}
	a, ok := store.FindAgent(agents, assigneeID)
	return ok && a.ParentID != nil && *a.ParentID == lead.ID
}

func zoneError(lead store.Agent, agents []store.Agent) error {
	if store.RolesOf(lead, agents).IsRoot {
		return errors.New("the work can be done only by you, an agent of your direct team or the first stage")
	}
	return errors.New("the work can be done only by you or an agent of your direct team")
}

// assigneeOf: numele dat de agent -> id (gol = liderul, "first stage" = 0)
func assigneeOf(lead store.Agent, agents []store.Agent, name string) (int64, error) {
	name = strings.TrimSpace(name)
	switch {
	case name == "":
		return lead.ID, nil
	case strings.EqualFold(name, firstStageName):
		return 0, nil
	}
	for _, a := range agents {
		if strings.EqualFold(a.Name, name) {
			return a.ID, nil
		}
	}
	return 0, fmt.Errorf("no agent named %q in this project", name)
}

// scheduleOf: programul din argumentele MCP; zilele devin masca, campurile care nu tin de kind se golesc
func scheduleOf(in mcp.RecurringSchedule) (schedule.Spec, error) {
	s := schedule.Spec{Kind: strings.TrimSpace(in.Kind)}
	switch s.Kind {
	case schedule.KindEveryHours:
		s.EveryHours = in.EveryHours
	case schedule.KindDaily:
		s.At = strings.TrimSpace(in.At)
	case schedule.KindWeekly:
		s.At = strings.TrimSpace(in.At)
		for _, d := range in.Weekdays {
			wd, ok := dayNames[strings.ToLower(strings.TrimSpace(d))]
			if !ok {
				return s, RecurringError(fmt.Sprintf("unknown day %q (use mon, tue, wed, thu, fri, sat, sun)", d))
			}
			s.Weekdays |= 1 << wd
		}
	}
	if err := s.Validate(); err != nil {
		return s, RecurringError(err.Error())
	}
	return s, nil
}

// recurringOf: sarcina R-<id> (sau doar id-ul) din proiectul apelantului; alt proiect = inexistenta
func (t *Team) recurringOf(projectID int64, ref string) (store.RecurringTask, error) {
	s := strings.TrimSpace(ref)
	if len(s) > 2 && strings.EqualFold(s[:2], "R-") {
		s = s[2:]
	}
	notFound := fmt.Errorf("%q is not a recurring task of this project; use an id from list_recurring", ref)
	id, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return store.RecurringTask{}, notFound
	}
	r, err := t.st.GetRecurring(id)
	if errors.Is(err, store.ErrNotFound) || (err == nil && r.ProjectID != projectID) {
		return store.RecurringTask{}, notFound
	}
	return r, err
}

// recurringLine: o sarcina pe un rand, ca in list_recurring
func recurringLine(r store.RecurringTask, agents []store.Agent, hasStages bool) string {
	who := "no one"
	if r.AssigneeID != nil {
		if a, ok := store.FindAgent(agents, *r.AssigneeID); ok {
			who = a.Name
		}
	} else if hasStages {
		who = firstStageName
	}
	parts := []string{fmt.Sprintf("R-%d %q", r.ID, r.Title), r.Schedule.String(), who}
	if r.Enabled {
		parts = append(parts, "on", "next "+r.NextRunAt.In(time.Local).Format("Mon Jan 2 15:04"))
	} else {
		parts = append(parts, "paused")
	}
	if r.AutoAccept {
		parts = append(parts, "auto-accept")
	}
	if r.LastNote != "" {
		parts = append(parts, r.LastNote)
	}
	if r.CreatedBy != nil {
		if a, ok := store.FindAgent(agents, *r.CreatedBy); ok {
			parts = append(parts, "created by "+a.Name)
		}
	}
	return strings.Join(parts, " — ")
}

func (t *Team) hasStages(projectID int64) (bool, error) {
	stages, err := t.st.ListStages(projectID)
	return len(stages) > 0, err
}

func (t *Team) ListRecurringFor(c mcp.Caller) (string, error) {
	t.work.Lock()
	defer t.work.Unlock()
	_, agents, err := t.recurringLead(c, false)
	if err != nil {
		return "", err
	}
	list, err := t.st.ListRecurring(c.ProjectID)
	if err != nil {
		return "", err
	}
	if len(list) == 0 {
		return "No recurring tasks in this project.", nil
	}
	staged, err := t.hasStages(c.ProjectID)
	if err != nil {
		return "", err
	}
	lines := make([]string, len(list))
	for i, r := range list {
		lines[i] = recurringLine(r, agents, staged)
	}
	return strings.Join(lines, "\n"), nil
}

func (t *Team) CreateRecurringFor(c mcp.Caller, in mcp.RecurringArgs) (string, error) {
	t.work.Lock()
	defer t.work.Unlock()
	lead, agents, err := t.recurringLead(c, true)
	if err != nil {
		return "", err
	}
	who, err := assigneeOf(lead, agents, in.Assignee)
	if err != nil {
		return "", err
	}
	if !inZone(lead, agents, who) {
		return "", zoneError(lead, agents)
	}
	spec, err := scheduleOf(in.Schedule)
	if err != nil {
		return "", err
	}
	by := lead.ID
	r, err := t.createRecurring(c.ProjectID, RecurringInput{Title: in.Title, Description: in.Description, AssigneeID: who,
		Schedule: spec, AutoAccept: in.AutoAccept, Enabled: true, CreatedBy: &by})
	if err != nil {
		return "", err
	}
	staged, err := t.hasStages(c.ProjectID)
	if err != nil {
		return "", err
	}
	return "Created " + recurringLine(r, agents, staged) +
		". Each run becomes a task on the board; the user decides it (or it is accepted automatically with auto-accept).", nil
}

func (t *Team) UpdateRecurringFor(c mcp.Caller, ch mcp.RecurringChange) (string, error) {
	t.work.Lock()
	defer t.work.Unlock()
	lead, agents, err := t.recurringLead(c, true)
	if err != nil {
		return "", err
	}
	r, err := t.recurringOf(c.ProjectID, ch.ID)
	if err != nil {
		return "", err
	}
	if !inZone(lead, agents, idOrZero(r.AssigneeID)) {
		return "", fmt.Errorf("R-%d belongs to another team; ask the user to change it in the Recurring panel", r.ID)
	}
	in := RecurringInput{Title: r.Title, Description: r.Description, AssigneeID: idOrZero(r.AssigneeID),
		Schedule: r.Schedule, AutoAccept: r.AutoAccept, Enabled: r.Enabled}
	changed := false
	if ch.Title != nil {
		in.Title, changed = *ch.Title, true
	}
	if ch.Description != nil {
		in.Description, changed = *ch.Description, true
	}
	if ch.Assignee != nil {
		who, err := assigneeOf(lead, agents, *ch.Assignee)
		if err != nil {
			return "", err
		}
		if !inZone(lead, agents, who) {
			return "", zoneError(lead, agents)
		}
		in.AssigneeID, changed = who, true
	}
	if ch.Schedule != nil {
		spec, err := scheduleOf(*ch.Schedule)
		if err != nil {
			return "", err
		}
		in.Schedule, changed = spec, true
	}
	if ch.AutoAccept != nil {
		in.AutoAccept, changed = *ch.AutoAccept, true
	}
	if ch.Enabled != nil {
		in.Enabled, changed = *ch.Enabled, true
	}
	if !changed {
		return "", errors.New("nothing to change: pass at least one field besides id")
	}
	r, err = t.updateRecurring(r.ID, in)
	if err != nil {
		return "", err
	}
	staged, err := t.hasStages(c.ProjectID)
	if err != nil {
		return "", err
	}
	return "Updated " + recurringLine(r, agents, staged), nil
}

func (t *Team) DeleteRecurringFor(c mcp.Caller, ref string) (string, error) {
	t.work.Lock()
	defer t.work.Unlock()
	lead, agents, err := t.recurringLead(c, true)
	if err != nil {
		return "", err
	}
	r, err := t.recurringOf(c.ProjectID, ref)
	if err != nil {
		return "", err
	}
	if !inZone(lead, agents, idOrZero(r.AssigneeID)) {
		return "", fmt.Errorf("R-%d belongs to another team; ask the user to change it in the Recurring panel", r.ID)
	}
	if err := t.deleteRecurring(r); err != nil {
		return "", err
	}
	return fmt.Sprintf("Deleted R-%d %q. Tasks it already created stay on the board.", r.ID, r.Title), nil
}

// idOrZero: asignatul ca id (nil = 0, prima etapa sau agent sters)
func idOrZero(p *int64) int64 {
	if p == nil {
		return 0
	}
	return *p
}
```

Inainte sa scrii `idOrZero`, cauta un helper existent: `grep -rn "func idOrZero\|func deref" internal/team/`; daca exista unul cu acelasi rol, foloseste-l si nu-l mai defini. In `TestRecurringZone` cazul "Lead" ca assignee pentru sub-lider da `zoneError` (radacina nu e in echipa lui directa) — mesajul contine "direct team" in ambele variante.

Nota de ordine in `UpdateRecurringFor`: `recurringOf` se cheama dupa `recurringLead`, deci in `TestRecurringToolsNeedChat` update / delete pe `R-1` inexistent intorc `errNotAsked` (regula se verifica prima).

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/team/`
Expected: PASS (tot pachetul)

- [ ] **Step 6: Commit**

```bash
git add internal/mcp/server.go internal/team/recurring_tools.go internal/team/recurring_tools_test.go
git commit -m "team: recurring task tools for leads, only when the user asks in chat"
```

---

### Task 4: MCP — tool-urile, decodarea argumentelor, `Backend`

**Files:**
- Modify: `internal/mcp/tools.go` (lista `recurringTools`, `toolsFor`)
- Modify: `internal/mcp/server.go` (`Backend`, cazurile din dispatch, `argSchedule` / `argStrPtr` / `argBoolPtr`)
- Test: `internal/mcp/server_test.go`

**Interfaces:**
- Consumes: tipurile `mcp.RecurringSchedule` / `RecurringArgs` / `RecurringChange` si metodele `ListRecurringFor` / `CreateRecurringFor` / `UpdateRecurringFor` / `DeleteRecurringFor` ale `*team.Team` (Task 3).
- Produces: `mcp.Backend` cu cele 4 metode (wrapper-ele din `draft` si `setup` incorporeaza `mcp.Backend`, deci compileaza fara schimbari).

- [ ] **Step 1: Write the failing tests** — in `internal/mcp/server_test.go`:

Metodele lui `fakeBackend` (langa `RemoveAgent`):

```go
func (f *fakeBackend) ListRecurringFor(c Caller) (string, error) {
	f.calls = append(f.calls, "list_recurring")
	return "No recurring tasks in this project.", f.err
}

func (f *fakeBackend) CreateRecurringFor(c Caller, in RecurringArgs) (string, error) {
	s := in.Schedule
	f.calls = append(f.calls, fmt.Sprintf("create_recurring|%s|%s|%s|%s|%d|%s|%s|%v",
		in.Title, in.Description, in.Assignee, s.Kind, s.EveryHours, s.At, strings.Join(s.Weekdays, ","), in.AutoAccept))
	return "ok", f.err
}

func (f *fakeBackend) UpdateRecurringFor(c Caller, ch RecurringChange) (string, error) {
	show := func(p *string) string {
		if p == nil {
			return "-"
		}
		return *p
	}
	showB := func(p *bool) string {
		if p == nil {
			return "-"
		}
		return fmt.Sprint(*p)
	}
	sched := "-"
	if ch.Schedule != nil {
		sched = fmt.Sprintf("%s/%d", ch.Schedule.Kind, ch.Schedule.EveryHours)
	}
	f.calls = append(f.calls, fmt.Sprintf("update_recurring|%s|%s|%s|%s|%s|%s|%s",
		ch.ID, show(ch.Title), show(ch.Description), show(ch.Assignee), sched, showB(ch.AutoAccept), showB(ch.Enabled)))
	return "ok", f.err
}

func (f *fakeBackend) DeleteRecurringFor(c Caller, ref string) (string, error) {
	f.calls = append(f.calls, "delete_recurring|"+ref)
	return "ok", f.err
}
```

Asteptarile exacte de liste se schimba (tool-urile noi vin dupa `remove_agent`):
- linia 480: `"create_task,list_tasks,accept_task,send_back,add_agent,remove_agent,list_recurring,create_recurring,update_recurring,delete_recurring,start_task,ask_user"`
- linia 484: `"create_task,list_tasks,add_agent,remove_agent,list_recurring,create_recurring,update_recurring,delete_recurring,start_task,ask_user"`
- linia 528: `"create_task,list_tasks,accept_task,send_back,add_agent,remove_agent,list_recurring,create_recurring,update_recurring,delete_recurring,list_linked_projects,ask_project,reply_to_project,request_project_work,start_task,ask_user"`

Test nou:

```go
func TestRecurringTools(t *testing.T) {
	fb, ts := setup(t)
	for _, token := range []string{"worker", "sublead", "reviewer", "setup", "stagework"} {
		_, resp := rpc(t, ts, token, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
		raw, _ := json.Marshal(resp)
		if strings.Contains(string(raw), "_recurring") {
			t.Fatalf("%s must not get recurring tools", token)
		}
	}
	calls := []string{
		`{"name":"list_recurring","arguments":{}}`,
		`{"name":"create_recurring","arguments":{"title":"Tests","description":"run","assignee":"Rook","schedule":{"kind":"weekly","at":"09:00","weekdays":["mon","fri"]},"auto_accept":true}}`,
		`{"name":"create_recurring","arguments":{"title":"Ping","schedule":{"kind":"every_hours","every_hours":"3"}}}`,
		`{"name":"update_recurring","arguments":{"id":"R-3","enabled":false}}`,
		`{"name":"update_recurring","arguments":{"id":3,"title":"New","schedule":{"kind":"every_hours","every_hours":2},"auto_accept":false}}`,
		`{"name":"delete_recurring","arguments":{"id":"R-3"}}`,
	}
	for i, call := range calls {
		_, resp := rpc(t, ts, "leadadd", fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"method":"tools/call","params":%s}`, i+2, call))
		if result(t, resp)["isError"] == true {
			t.Fatalf("%s: %v", call, resp)
		}
	}
	want := []string{
		"list_recurring",
		"create_recurring|Tests|run|Rook|weekly|0|09:00|mon,fri|true",
		"create_recurring|Ping|||every_hours|3|||false",
		"update_recurring|R-3|-|-|-|-|-|false",
		"update_recurring|3|New|-|-|every_hours/2|false|-",
		"delete_recurring|R-3",
	}
	if strings.Join(fb.calls, "\n") != strings.Join(want, "\n") {
		t.Fatalf("calls:\n%s", strings.Join(fb.calls, "\n"))
	}
}
```

Verifica inainte cum arata `result(t, resp)` si cheia `isError` in testele existente (ex. `TestCallAddAgent`) si foloseste aceeasi forma; daca importurile `encoding/json` / `fmt` lipsesc din fisierul de test, adauga-le.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/mcp/ -run 'TestRecurringTools|TestAddAgentTools|TestLinkedProjectTools'`
Expected: FAIL — listele nu contin inca tool-urile noi, iar `create_recurring` intoarce `unknown tool` (isError)

- [ ] **Step 3: Write the implementation**

`internal/mcp/tools.go`, dupa `addAgentTools`:

```go
// scheduleArg: programul unei sarcini recurente (ora locala a masinii)
var scheduleArg = map[string]any{
	"type":        "object",
	"description": "When it runs (local time). every_hours: every N hours; daily: every day at `at`; weekly: on `weekdays` at `at`",
	"properties": map[string]any{
		"kind":        map[string]any{"type": "string", "enum": []string{"every_hours", "daily", "weekly"}},
		"every_hours": map[string]any{"type": "integer", "description": "every_hours: 1-24"},
		"at":          str("daily / weekly: HH:MM, 24h"),
		"weekdays":    map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": `weekly: days, e.g. ["mon", "wed", "fri"]`},
	},
	"required": []string{"kind"},
}

var recurringIDArg = str("The recurring task, e.g. R-3 (from list_recurring)")

// recurringTools: liderii si radacinile; schimbarile doar la cererea utilizatorului din chat (verificat la apel)
var recurringTools = []Tool{
	{
		Name:        "list_recurring",
		Description: "Lists this project's recurring tasks (R-N): schedule, who does them, on or paused, next run.",
		InputSchema: schema([]string{}, map[string]any{}),
	},
	{
		Name: "create_recurring",
		Description: "Creates a recurring task: it puts a task on the board on a schedule, done by you or an agent of your direct team. " +
			"Use it ONLY when the user asks for it in this chat; otherwise suggest it in your reply. The user decides each run " +
			"(or it is accepted automatically with auto_accept). Check list_recurring first so you do not create a duplicate.",
		InputSchema: schema([]string{"title", "schedule"}, map[string]any{
			"title":       str("Short, clear title"),
			"description": str("What to do on each run and what to report"),
			"assignee":    str(`Who does it: you, an agent of your direct team, or "first stage" (root agent, projects with stages). Default: you`),
			"schedule":    scheduleArg,
			"auto_accept": map[string]any{"type": "boolean", "description": "Accept each run automatically when it finishes (default false)"},
		}),
	},
	{
		Name: "update_recurring",
		Description: "Changes a recurring task when the user asks for it in this chat. Pass only what changes; " +
			"enabled false pauses it, true resumes it.",
		InputSchema: schema([]string{"id"}, map[string]any{
			"id":          recurringIDArg,
			"title":       str("New title"),
			"description": str("New description"),
			"assignee":    str(`Who does it: you, an agent of your direct team, or "first stage"`),
			"schedule":    scheduleArg,
			"auto_accept": map[string]any{"type": "boolean"},
			"enabled":     map[string]any{"type": "boolean", "description": "false = pause, true = resume"},
		}),
	},
	{
		Name:        "delete_recurring",
		Description: "Deletes a recurring task when the user asks for it in this chat. Tasks it already created stay on the board.",
		InputSchema: schema([]string{"id"}, map[string]any{"id": recurringIDArg}),
	},
}
```

In `toolsFor`, in blocul `if c.CanAddAgents {`, dupa `out = append(out, addAgentTools...)`:

```go
		out = append(out, recurringTools...)
```

`internal/mcp/server.go` — in `Backend`, dupa `RemoveAgent`:

```go
	ListRecurringFor(c Caller) (string, error)
	CreateRecurringFor(c Caller, in RecurringArgs) (string, error)
	UpdateRecurringFor(c Caller, ch RecurringChange) (string, error)
	DeleteRecurringFor(c Caller, ref string) (string, error)
```

In dispatch, dupa `case "remove_agent":`:

```go
	case "list_recurring":
		text, err = s.backend.ListRecurringFor(c)
	case "create_recurring":
		in := RecurringArgs{Title: arg("title"), Description: arg("description"), Assignee: arg("assignee")}
		if sc := argSchedule(p.Arguments, "schedule"); sc != nil {
			in.Schedule = *sc
		}
		if b := argBoolPtr(p.Arguments, "auto_accept"); b != nil {
			in.AutoAccept = *b
		}
		text, err = s.backend.CreateRecurringFor(c, in)
	case "update_recurring":
		text, err = s.backend.UpdateRecurringFor(c, RecurringChange{
			ID: arg("id"), Title: argStrPtr(p.Arguments, "title"), Description: argStrPtr(p.Arguments, "description"),
			Assignee: argStrPtr(p.Arguments, "assignee"), Schedule: argSchedule(p.Arguments, "schedule"),
			AutoAccept: argBoolPtr(p.Arguments, "auto_accept"), Enabled: argBoolPtr(p.Arguments, "enabled"),
		})
	case "delete_recurring":
		text, err = s.backend.DeleteRecurringFor(c, arg("id"))
```

Helper-e, langa `argList`:

```go
// argSchedule citeste obiectul schedule al tool-urilor *_recurring (nil daca lipseste)
func argSchedule(args map[string]any, key string) *RecurringSchedule {
	m, ok := args[key].(map[string]any)
	if !ok {
		return nil
	}
	s := &RecurringSchedule{Kind: argString(m, "kind"), At: argString(m, "at"), Weekdays: argList(m, "weekdays")}
	if n, err := strconv.Atoi(strings.TrimSpace(argString(m, "every_hours"))); err == nil {
		s.EveryHours = n // numar sau text ("3")
	}
	return s
}

// argStrPtr: un text optional (nil = lipseste, deci nu se schimba)
func argStrPtr(args map[string]any, key string) *string {
	if _, ok := args[key]; !ok {
		return nil
	}
	v := argString(args, key)
	return &v
}

// argBoolPtr: un bool optional (nil = lipseste sau nu e bool)
func argBoolPtr(args map[string]any, key string) *bool {
	v, ok := args[key].(bool)
	if !ok {
		return nil
	}
	return &v
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/mcp/ && go build ./... && go test ./internal/team/`
Expected: PASS; `go build ./...` confirma ca `*team.Team` (si wrapper-ele din `draft` / `setup`) implementeaza `mcp.Backend`

- [ ] **Step 5: Commit**

```bash
git add internal/mcp/tools.go internal/mcp/server.go internal/mcp/server_test.go
git commit -m "mcp: list/create/update/delete_recurring for leads and roots"
```

---

### Task 5: UI — "Created by" in panoul Recurring

**Files:**
- Modify: `internal/web/api_recurring.go` (`recurringView`)
- Modify: `internal/web/static/js/recurring.js:80-90` (randul sarcinii)
- Test: `internal/web/recurring_test.go`

**Interfaces:**
- Consumes: `store.RecurringTask.CreatedBy` (Task 1), `team.RecurringInput.CreatedBy` (Task 2).
- Produces: `recurringView.CreatedByName string \`json:"createdByName"\`` ("" = utilizatorul sau agent sters).

- [ ] **Step 1: Write the failing test** — in `internal/web/recurring_test.go`:

```go
func TestRecurringCreatedByName(t *testing.T) {
	e := setup(t)
	p := e.createProject(t, t.TempDir())
	root := e.root(t, p.ID)
	by := root.ID
	if _, err := e.tm.CreateRecurring(p.ID, team.RecurringInput{Title: "By lead", AssigneeID: root.ID,
		Schedule: schedule.Spec{Kind: schedule.KindDaily, At: "09:00"}, Enabled: true, CreatedBy: &by}); err != nil {
		t.Fatal(err)
	}
	_, raw := e.do(t, "GET", "/api/projects/"+itoa(p.ID), nil)
	if !strings.Contains(string(raw), `"createdByName":"`+root.Name+`"`) {
		t.Fatalf("%s", raw)
	}
}
```

(importuri: `hobstack/internal/team`, `hobstack/internal/schedule`, daca lipsesc.)

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/web/ -run TestRecurringCreatedByName`
Expected: FAIL — raspunsul nu contine `createdByName`

- [ ] **Step 3: Write the implementation**

`internal/web/api_recurring.go`:

```go
type recurringView struct {
	store.RecurringTask
	ScheduleText  string        `json:"scheduleText"`
	Assignee      string        `json:"assignee"` // "" = prima etapa
	CreatedByName string        `json:"createdByName"` // agentul care a creat-o la cererea utilizatorului; "" = utilizatorul
	LastTask      *lastTaskView `json:"lastTask"`
	Runs          int           `json:"runs"` // task-urile create de sarcina (si arhivate)
}
```

In `recurringView(...)`, dupa blocul pentru `Assignee`:

```go
	if r.CreatedBy != nil {
		if a, err := s.st.GetAgent(*r.CreatedBy); err == nil {
			v.CreatedByName = a.Name
		}
	}
```

`internal/web/static/js/recurring.js`, in `rec-info`, dupa `r.autoAccept ? ... : null`:

```js
            r.autoAccept ? el("span", { class: "hint" }, "auto-accept") : null,
            r.createdByName ? el("span", { class: "hint" }, "Created by " + r.createdByName) : null),
```

- [ ] **Step 4: Run tests and checks**

Run:
```bash
go test ./internal/web/
SP=/private/tmp/claude-501/-Users-yzy-YzY-AI-Dashboard/8d95c559-0068-4d9b-8ac4-b3fe2d04fd5b/scratchpad
cp internal/web/static/js/recurring.js $SP/recurring.mjs && node --check $SP/recurring.mjs
```
Expected: PASS si fara iesire de la `node --check`

- [ ] **Step 5: Verify in the browser** — instanta izolata (`HOME=$SP/h2 ./hobstack start --no-open --port 7788`, proiectul 1), echipa oprita (`POST /api/projects/1/stop`), apoi o sarcina cu `created_by` = radacina inserata cu sqlite3 in `$SP/h2/.hobstack/hobstack.db` (`next_run_at` in viitor, `enabled` 1). Cu Chrome headless prin CDP (port 9333, `Page.reload {ignoreCache:true}`) deschide `#/p/1/tasks`, panoul Recurring, si verifica textul `Created by <radacina>` pe rand, la 1440 si 800 px, cu screenshot. Opreste instanta cu `pkill -f -- "--port 7788"`.

Expected: eticheta apare pe randul sarcinii, pe acelasi rand cu `auto-accept` / program, fara scroll orizontal la 800 px.

- [ ] **Step 6: Commit**

```bash
git add internal/web/api_recurring.go internal/web/static/js/recurring.js internal/web/recurring_test.go
git commit -m "ui: Created by <agent> on recurring tasks made by a lead"
```

---

### Task 6: Documentatie, verificare completa, restart

**Files:**
- Modify: `CLAUDE.md` (intrare "Gata", inainte de `Urmeaza (sub-proiecte separate`)

- [ ] **Step 1: Add the CLAUDE.md entry** — inainte de linia `Urmeaza (sub-proiecte separate, fiecare cu spec + plan):`:

```
Gata (liderul si sarcinile recurente, spec in docs/superpowers/specs/2026-10-16-hobstack-lead-recurring-design.md):
liderii si radacinile au list_recurring / create_recurring / update_recurring / delete_recurring (team/recurring_tools.go);
schimbarile doar intr-un run pornit dintr-un mesaj al utilizatorului (requestOf; Full autonomy nu ajunge), doar pentru ei
insisi, echipa directa sau prima etapa (radacina), si doar pe sarcinile din zona lor. Aparitiile merg ca la sarcinile
utilizatorului. recurring_tasks.created_by (migrarea 032), "Created by <agent>" in panoul Recurring; trim-ul si limitele
titlului / descrierii sunt in team (RecurringInput.clean), comune cu UI-ul.
```

- [ ] **Step 2: Run the full verification**

Run:
```bash
go vet ./... && go build -race -o /dev/null ./cmd/hobstack && \
GOOS=darwin GOARCH=arm64 go build -o /dev/null ./cmd/hobstack && GOOS=windows go build -o /dev/null ./cmd/hobstack && \
go test ./... 2>&1 | grep -v "^ok\|no test files"; echo done
```
Expected: doar `done` (nicio linie FAIL)

- [ ] **Step 3: Commit and push**

```bash
git add CLAUDE.md
git commit -m "docs: CLAUDE.md entry for lead-managed recurring tasks"
git push origin main
```

- [ ] **Step 4: Restart HobStack** — doar daca `pgrep -f "claude -p"` nu gaseste nimic:

```bash
go build -o hobstack ./cmd/hobstack && pkill -f "hobstack start"; (nohup ./hobstack start --no-open > ~/.hobstack/hobstack.log 2>&1 &)
```

Daca ruleaza agenti, porneste in fundal asteptarea: `until ! pgrep -f "claude -p" >/dev/null; do sleep 15; done; pkill -f "hobstack start"; (nohup ./hobstack start --no-open > ~/.hobstack/hobstack.log 2>&1 &)`.
