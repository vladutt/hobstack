# Liderul adauga agenti: plan de implementare

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** liderii si radacinile isi pot mari echipa prin tool-urile MCP `add_agent` / `remove_agent`, direct (Full
autonomy sau cerere din chat) sau cu aprobarea utilizatorului in "Needs you".

**Architecture:** store-ul primeste `agents.created_by` si tabelul `agent_requests` (migrarea 015). Pachetul `mcp`
expune doua tool-uri noi pentru `Caller.CanAddAgents`. Regulile (submultimea de tool-uri, limita de 6, cine aproba)
sunt in `internal/team/agents.go`, sub `t.work`. Web-ul expune approve/dismiss, UI-ul arata cererile in "Needs you".

**Tech Stack:** Go 1.22+ (net/http ServeMux cu pattern-uri), SQLite prin modernc.org/sqlite, JS vanilla (ES modules).

**Spec:** `docs/superpowers/specs/2026-10-06-foreman-lead-adds-agents-design.md`

## Global Constraints

- Tot ce vede utilizatorul sau agentul (UI, erori, raspunsuri de tool, prompt) e in engleza; comentariile din cod in romana.
- `go vet ./...` si `go build -race ./cmd/foreman` trec; cross-compile `GOOS=darwin GOARCH=arm64 go build ./cmd/foreman` si `GOOS=windows go build ./cmd/foreman`.
- Fara dependente noi.
- JS-ul se verifica copiind fisierul ca `.mjs` si rulind `node --check` (pe `.js` nu prinde erorile de sintaxa).
- Limita: `MaxAddedAgents = 6` (agenti activi adaugati de lider + cererile lui deschise).
- Lungimi: name 40, role 80, background 10000, instructions 10000, doneDef 4000, responsibilities 2000, reason 300.
- Model implicit `sonnet`; tool-uri implicite `["write"]` daca liderul are `write`, altfel `[]`.
- Agentul nou: `ParentID` = liderul, `FolderID` = folderul liderului, fara reviewer, `CreatedBy` = liderul, pozitie `nil`.
- Mesajele catre lider dupa decizie merg prin `t.note(..., false)`, niciodata prin `addUserMessage`.
- Comenzile se ruleaza din `agentdash/`.

## Review Focus

1. Un run pornit de raspunsul la o intrebare (`AnswerQuestion` foloseste `addUserMessage`) conteaza ca "din chat":
   liderul poate adauga direct. E acceptat (utilizatorul a vorbit cu el); nota de aprobare NU trebuie sa faca la fel
   (test in Task 3: dupa approve, nota e `KindEvent`, nu `AuthorUser`).
2. Approve dupa ce utilizatorul a creat din canvas un agent cu acelasi nume: trebuie 409 cu mesaj clar, cererea
   ramane deschisa (test in Task 3 si Task 5).
3. `remove_agent` pe un agent pe care utilizatorul l-a mutat sub alt sef: refuz `not on your team`, nu detasare
   (test in Task 3).
4. Dublu-click pe Approve (doua request-uri): al doilea primeste 409, nu se creeaza doi agenti (test in Task 3:
   approve de doua ori).
5. Proiect cu etape: agentul nou nu e in nicio etapa; raspunsul catre lider spune ca utilizatorul trebuie sa-l puna
   intr-o etapa (test in Task 3).

---

### Task 1: Store: `created_by` si `agent_requests`

**Files:**
- Create: `internal/store/migrations/015_agent_requests.sql`
- Create: `internal/store/agent_requests.go`
- Create: `internal/store/agent_requests_test.go`
- Modify: `internal/store/agents.go` (struct `Agent`, `agentCols`, `scanAgent`, `insertAgent`)
- Modify: `internal/store/store_test.go:63-64` (user_version 14 -> 15)

**Interfaces:**
- Produces:
  - `Agent.CreatedBy *int64` (`json:"createdBy"`), scris de `insertAgent`.
  - `type AgentSpec struct { Name, Role, Model, Background, Instructions, DoneDef string; Tools []string }` (tag-uri json `name, role, model, background, instructions, doneDef, tools`).
  - `type AgentRequest struct { ID, ProjectID, LeadID int64; TaskID *int64; RunID string; Spec AgentSpec; Reason, Status string; AgentID *int64; CreatedAt time.Time; ClosedAt *time.Time }`.
  - Constante `AgentRequestOpen = "open"`, `AgentRequestApproved = "approved"`, `AgentRequestDismissed = "dismissed"`.
  - `(*Store) AddAgentRequest(r *AgentRequest) error`
  - `(*Store) GetAgentRequest(id int64) (AgentRequest, error)` (ErrNotFound)
  - `(*Store) ListOpenAgentRequests(projectID int64) ([]AgentRequest, error)` (non-nil, dupa id)
  - `(*Store) CountOpenAgentRequests(projectID int64) (int, error)`
  - `(*Store) OpenAgentRequestNamed(projectID int64, name string) (bool, error)` (NOCASE)
  - `(*Store) CountAddedBy(leadID int64) (int, error)` (agenti `created_by = lead AND detached = 0` + cereri deschise ale liderului)
  - `(*Store) ApproveAgentRequest(id int64, a *Agent) error` (o tranzactie: cererea trebuie sa fie open, altfel ErrConflict; checkLinks + insertAgent, ErrConflict la nume luat; inchide cererea cu `agent_id`)
  - `(*Store) DismissAgentRequest(id int64) error` (doar din open; ErrConflict / ErrNotFound)

- [ ] **Step 1: Migrarea** `015_agent_requests.sql`:

```sql
-- agentii adaugati de un lider si cererile de aprobare pentru ei
ALTER TABLE agents ADD COLUMN created_by INTEGER REFERENCES agents(id) ON DELETE SET NULL;

CREATE TABLE agent_requests (
  id          INTEGER PRIMARY KEY,
  project_id  INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  lead_id     INTEGER NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
  task_id     INTEGER REFERENCES tasks(id) ON DELETE SET NULL,
  run_id      TEXT NOT NULL DEFAULT '',
  spec        TEXT NOT NULL,   -- JSON: name, role, model, background, instructions, doneDef, tools
  reason      TEXT NOT NULL,
  status      TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'approved', 'dismissed')),
  agent_id    INTEGER REFERENCES agents(id) ON DELETE SET NULL, -- agentul creat la aprobare
  created_at  TEXT NOT NULL,
  closed_at   TEXT
);
CREATE INDEX agent_requests_project ON agent_requests(project_id, status);
CREATE INDEX agent_requests_lead ON agent_requests(lead_id, status);
```

- [ ] **Step 2: Testele** (`agent_requests_test.go`, `package store`, cu `newStore`, `newProject`, `newAgent`, `ptr` din `store_test.go`):

```go
package store

import (
	"errors"
	"testing"
)

func newRequest(t *testing.T, s *Store, projectID, leadID int64, name string) AgentRequest {
	t.Helper()
	r := AgentRequest{ProjectID: projectID, LeadID: leadID, RunID: "run-1", Reason: "need QA",
		Spec: AgentSpec{Name: name, Role: "QA", Model: "sonnet", Instructions: "test it", Tools: []string{"write"}}}
	if err := s.AddAgentRequest(&r); err != nil {
		t.Fatal(err)
	}
	return r
}

func TestCreatedByRoundTrip(t *testing.T) {
	s := newStore(t)
	p, lead := newProject(t, s)
	a := Agent{ProjectID: p.ID, Name: "Quinn", Model: "sonnet", ParentID: &lead.ID, CreatedBy: &lead.ID}
	if err := s.CreateAgent(&a); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetAgent(a.ID)
	if err != nil || got.CreatedBy == nil || *got.CreatedBy != lead.ID {
		t.Fatalf("created_by: %v %v", got.CreatedBy, err)
	}
	if lead2, _ := s.GetAgent(lead.ID); lead2.CreatedBy != nil {
		t.Fatal("liderul creat cu proiectul nu are created_by")
	}
}

func TestAgentRequestLifecycle(t *testing.T) {
	s := newStore(t)
	p, lead := newProject(t, s)
	r := newRequest(t, s, p.ID, lead.ID, "Quinn")
	got, err := s.GetAgentRequest(r.ID)
	if err != nil || got.Status != AgentRequestOpen || got.Spec.Name != "Quinn" || got.Spec.Tools[0] != "write" || got.Reason != "need QA" {
		t.Fatalf("%+v %v", got, err)
	}
	if n, _ := s.CountOpenAgentRequests(p.ID); n != 1 {
		t.Fatalf("open = %d", n)
	}
	if open, _ := s.OpenAgentRequestNamed(p.ID, "quinn"); !open {
		t.Fatal("numele trebuie gasit fara diferenta de litere")
	}
	a := Agent{ProjectID: p.ID, Name: "Quinn", Model: "sonnet", ParentID: &lead.ID, CreatedBy: &lead.ID}
	if err := s.ApproveAgentRequest(r.ID, &a); err != nil {
		t.Fatal(err)
	}
	got, _ = s.GetAgentRequest(r.ID)
	if got.Status != AgentRequestApproved || got.AgentID == nil || *got.AgentID != a.ID || got.ClosedAt == nil {
		t.Fatalf("dupa approve: %+v", got)
	}
	// a doua aprobare (dublu-click) nu creeaza alt agent
	b := Agent{ProjectID: p.ID, Name: "Quinn2", Model: "sonnet", ParentID: &lead.ID}
	if err := s.ApproveAgentRequest(r.ID, &b); !errors.Is(err, ErrConflict) {
		t.Fatalf("a doua aprobare: %v", err)
	}
	if _, err := s.FindAgentByName(p.ID, "Quinn2"); !errors.Is(err, ErrNotFound) {
		t.Fatal("agentul celei de-a doua aprobari nu trebuie sa existe")
	}
	if list, _ := s.ListOpenAgentRequests(p.ID); len(list) != 0 {
		t.Fatalf("open: %+v", list)
	}
}

func TestApproveAgentRequestNameTaken(t *testing.T) {
	s := newStore(t)
	p, lead := newProject(t, s)
	r := newRequest(t, s, p.ID, lead.ID, "Quinn")
	newAgent(t, s, p.ID, "Quinn", &lead.ID)
	a := Agent{ProjectID: p.ID, Name: "Quinn", Model: "sonnet", ParentID: &lead.ID}
	if err := s.ApproveAgentRequest(r.ID, &a); !errors.Is(err, ErrConflict) {
		t.Fatalf("nume luat: %v", err)
	}
	if got, _ := s.GetAgentRequest(r.ID); got.Status != AgentRequestOpen {
		t.Fatal("cererea ramane deschisa")
	}
}

func TestDismissAgentRequest(t *testing.T) {
	s := newStore(t)
	p, lead := newProject(t, s)
	r := newRequest(t, s, p.ID, lead.ID, "Quinn")
	if err := s.DismissAgentRequest(r.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.DismissAgentRequest(r.ID); !errors.Is(err, ErrConflict) {
		t.Fatalf("a doua oara: %v", err)
	}
	if err := s.DismissAgentRequest(9999); !errors.Is(err, ErrNotFound) {
		t.Fatalf("inexistenta: %v", err)
	}
	if open, _ := s.OpenAgentRequestNamed(p.ID, "Quinn"); open {
		t.Fatal("o cerere inchisa nu mai ocupa numele")
	}
}

func TestCountAddedBy(t *testing.T) {
	s := newStore(t)
	p, lead := newProject(t, s)
	for _, name := range []string{"A1", "A2"} {
		a := Agent{ProjectID: p.ID, Name: name, Model: "sonnet", ParentID: &lead.ID, CreatedBy: &lead.ID}
		if err := s.CreateAgent(&a); err != nil {
			t.Fatal(err)
		}
	}
	newAgent(t, s, p.ID, "Manual", &lead.ID) // creat de utilizator: nu se numara
	newRequest(t, s, p.ID, lead.ID, "R1")
	closed := newRequest(t, s, p.ID, lead.ID, "R2")
	s.DismissAgentRequest(closed.ID)
	if n, err := s.CountAddedBy(lead.ID); err != nil || n != 3 {
		t.Fatalf("CountAddedBy = %d %v, vreau 3", n, err)
	}
	// un agent detasat nu mai conteaza
	a1, _ := s.FindAgentByName(p.ID, "A1")
	a1.ParentID, a1.Detached = nil, true
	if err := s.UpdateAgent(a1); err != nil {
		t.Fatal(err)
	}
	if n, _ := s.CountAddedBy(lead.ID); n != 2 {
		t.Fatalf("dupa detasare = %d, vreau 2", n)
	}
}
```

- [ ] **Step 3:** `go test ./internal/store/` -> FAIL (`AgentRequest`, `CreatedBy` lipsesc).

- [ ] **Step 4: `agents.go`.** In `Agent`, dupa `FolderID`:

```go
	CreatedBy        *int64   `json:"createdBy"` // liderul care l-a adaugat (add_agent); nil = utilizatorul sau wizard-ul
```

`agentCols` primeste la final `, created_by`; `scanAgent` scaneaza la final `&a.CreatedBy`. In `insertAgent`, INSERT-ul
primeste coloana `created_by` (dupa `folder_id`) si valoarea `nullPtr(a.CreatedBy)`:

```go
	res, err := ex.Exec(`INSERT INTO agents (project_id, name, role, position, model, background, instructions, done_def, tools, parent_id, reviewer_id, responsibilities, detached, folder_id, created_by, created_at)
		VALUES (?, ?, ?, 'worker', ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, // 'worker': coloana veche, pastrata doar pentru ca e NOT NULL
		a.ProjectID, a.Name, a.Role, a.Model, a.Background, a.Instructions, a.DoneDef, toolsJSON(a.Tools),
		nullPtr(a.ParentID), nullPtr(a.ReviewerID), a.Responsibilities, a.Detached, nullPtr(a.FolderID), nullPtr(a.CreatedBy), created)
```

`UpdateAgent` nu atinge `created_by` (ramane cine l-a creat).

- [ ] **Step 5: `agent_requests.go`:**

```go
package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

// Starile unei cereri de agent nou (add_agent cand liderul nu poate adauga direct).
const (
	AgentRequestOpen      = "open"
	AgentRequestApproved  = "approved"
	AgentRequestDismissed = "dismissed"
)

// AgentSpec e agentul propus de un lider; se salveaza ca JSON in agent_requests.spec.
type AgentSpec struct {
	Name         string   `json:"name"`
	Role         string   `json:"role"`
	Model        string   `json:"model"`
	Background   string   `json:"background"`
	Instructions string   `json:"instructions"`
	DoneDef      string   `json:"doneDef"`
	Tools        []string `json:"tools"` // mereu non-nil
}

// AgentRequest e o cerere a unui lider de a adauga un agent, in asteptarea utilizatorului.
type AgentRequest struct {
	ID        int64      `json:"id"`
	ProjectID int64      `json:"projectId"`
	LeadID    int64      `json:"leadId"`
	TaskID    *int64     `json:"taskId"` // task-ul run-ului care a cerut; nil = fara task
	RunID     string     `json:"runId"`
	Spec      AgentSpec  `json:"spec"`
	Reason    string     `json:"reason"`
	Status    string     `json:"status"`
	AgentID   *int64     `json:"agentId"` // agentul creat la aprobare
	CreatedAt time.Time  `json:"createdAt"`
	ClosedAt  *time.Time `json:"closedAt"`
}

const agentRequestCols = `id, project_id, lead_id, task_id, run_id, spec, reason, status, agent_id, created_at, closed_at`

func scanAgentRequest(sc scanner) (AgentRequest, error) {
	var r AgentRequest
	var spec, created string
	var closed sql.NullString
	err := sc.Scan(&r.ID, &r.ProjectID, &r.LeadID, &r.TaskID, &r.RunID, &spec, &r.Reason, &r.Status, &r.AgentID, &created, &closed)
	if errors.Is(err, sql.ErrNoRows) {
		return AgentRequest{}, ErrNotFound
	}
	if err != nil {
		return AgentRequest{}, err
	}
	if err := json.Unmarshal([]byte(spec), &r.Spec); err != nil {
		return AgentRequest{}, err
	}
	if r.Spec.Tools == nil {
		r.Spec.Tools = []string{}
	}
	r.CreatedAt = parseTime(created)
	if closed.Valid {
		t := parseTime(closed.String)
		r.ClosedAt = &t
	}
	return r, nil
}

// AddAgentRequest salveaza o cerere deschisa.
func (s *Store) AddAgentRequest(r *AgentRequest) error {
	if r.Spec.Tools == nil {
		r.Spec.Tools = []string{}
	}
	spec, err := json.Marshal(r.Spec)
	if err != nil {
		return err
	}
	created := now()
	res, err := s.db.Exec(`INSERT INTO agent_requests (project_id, lead_id, task_id, run_id, spec, reason, status, created_at)
		VALUES (?, ?, ?, ?, ?, ?, 'open', ?)`, r.ProjectID, r.LeadID, nullPtr(r.TaskID), r.RunID, string(spec), r.Reason, created)
	if err != nil {
		return err
	}
	if r.ID, err = res.LastInsertId(); err != nil {
		return err
	}
	r.Status, r.CreatedAt = AgentRequestOpen, parseTime(created)
	return nil
}

func (s *Store) GetAgentRequest(id int64) (AgentRequest, error) {
	return scanAgentRequest(s.db.QueryRow(`SELECT `+agentRequestCols+` FROM agent_requests WHERE id = ?`, id))
}

// ListOpenAgentRequests intoarce cererile deschise ale proiectului, cele mai vechi primele.
func (s *Store) ListOpenAgentRequests(projectID int64) ([]AgentRequest, error) {
	rows, err := s.db.Query(`SELECT `+agentRequestCols+` FROM agent_requests WHERE project_id = ? AND status = 'open' ORDER BY id`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AgentRequest{}
	for rows.Next() {
		r, err := scanAgentRequest(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) CountOpenAgentRequests(projectID int64) (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM agent_requests WHERE project_id = ? AND status = 'open'`, projectID).Scan(&n)
	return n, err
}

// OpenAgentRequestNamed: exista o cerere deschisa in proiect pentru un agent cu numele dat
// (fara diferenta intre litere mari si mici). Un nume cerut e ocupat pana la decizie.
func (s *Store) OpenAgentRequestNamed(projectID int64, name string) (bool, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM agent_requests WHERE project_id = ? AND status = 'open'
		AND json_extract(spec, '$.name') = ? COLLATE NOCASE`, projectID, name).Scan(&n)
	return n > 0, err
}

// CountAddedBy numara ce ocupa din limita unui lider: agentii adaugati de el
// care sunt inca in echipa (nedetasati) plus cererile lui deschise.
func (s *Store) CountAddedBy(leadID int64) (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT
		(SELECT COUNT(*) FROM agents WHERE created_by = ? AND detached = 0) +
		(SELECT COUNT(*) FROM agent_requests WHERE lead_id = ? AND status = 'open')`, leadID, leadID).Scan(&n)
	return n, err
}

// ApproveAgentRequest creeaza agentul a si inchide cererea, intr-o tranzactie.
// O cerere deja inchisa sau un nume luat intorc ErrConflict (nu se schimba nimic).
func (s *Store) ApproveAgentRequest(id int64, a *Agent) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var status string
	err = tx.QueryRow(`SELECT status FROM agent_requests WHERE id = ?`, id).Scan(&status)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if status != AgentRequestOpen {
		return ErrConflict
	}
	if err := checkLinks(tx, *a); err != nil {
		return err
	}
	if err := insertAgent(tx, a); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE agent_requests SET status = 'approved', agent_id = ?, closed_at = ? WHERE id = ?`, a.ID, now(), id); err != nil {
		return err
	}
	return tx.Commit()
}

// DismissAgentRequest inchide o cerere deschisa fara agent. Una deja inchisa
// intoarce ErrConflict, una inexistenta ErrNotFound.
func (s *Store) DismissAgentRequest(id int64) error {
	res, err := s.db.Exec(`UPDATE agent_requests SET status = 'dismissed', closed_at = ? WHERE id = ? AND status = 'open'`, now(), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 1 {
		return nil
	}
	if _, err := s.GetAgentRequest(id); err != nil {
		return err
	}
	return ErrConflict
}
```

- [ ] **Step 6:** `store_test.go` liniile 63-64: `14` -> `15` (ambele locuri). `go test ./internal/store/` -> PASS. Daca `json_extract` lipseste (nu ar trebui, modernc include JSON1), opreste-te si raporteaza.
- [ ] **Step 7:** `gofmt -l internal/store` gol; commit: `store: agenti adaugati de lider si cererile lor (migrarea 015)`

---

### Task 2: MCP: tool-urile `add_agent` / `remove_agent`

**Files:**
- Modify: `internal/mcp/server.go` (`Caller`, `Backend`, `call`)
- Modify: `internal/mcp/tools.go` (tool-uri noi, `toolsFor`)
- Modify: `internal/mcp/server_test.go` (fake + teste)
- Create: `internal/team/agents.go` (doar stub-urile, ca `Team` sa implementeze inca `mcp.Backend`; Task 3 le inlocuieste)
- Modify: `internal/team/run.go:86-89` (`CanAddAgents`)

**Interfaces:**
- Consumes: nimic din Task 1.
- Produces:
  - `Caller.CanAddAgents bool` (lider sau radacina; calculat in `team/run.go`).
  - `type AddAgentInput struct { Name, Role, Model, Background, Instructions, DoneDef, Reason string; Tools []string; ToolsSet bool }` in `mcp`.
  - `Backend.AddAgent(c Caller, in AddAgentInput) (string, error)` si `Backend.RemoveAgent(c Caller, name string) (string, error)`.

- [ ] **Step 1: Testele** (`server_test.go`). In `fakeBackend` adauga:

```go
func (f *fakeBackend) AddAgent(c Caller, in AddAgentInput) (string, error) {
	f.calls = append(f.calls, fmt.Sprintf("add_agent|%s|%s|%s|%v|%v|%s", in.Name, in.Role, in.Model, in.Tools, in.ToolsSet, in.Reason))
	return "ok", f.err
}
func (f *fakeBackend) RemoveAgent(c Caller, name string) (string, error) {
	f.calls = append(f.calls, "remove_agent|"+name)
	return "ok", f.err
}
```

(import `fmt`). In `setup`, la `callers` adauga:

```go
		"leadadd":  {ProjectID: 1, AgentID: 10, IsLead: true, CanAddAgents: true},
		"rootsolo": {ProjectID: 1, AgentID: 11, CanAddAgents: true},
```

Teste noi:

```go
func TestAddAgentTools(t *testing.T) {
	_, ts := setup(t)
	names := func(token string) string {
		_, resp := rpc(t, ts, token, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)
		var out []string
		for _, tool := range result(t, resp)["tools"].([]any) {
			out = append(out, tool.(map[string]any)["name"].(string))
		}
		return strings.Join(out, ",")
	}
	if got := names("leadadd"); got != "create_task,list_tasks,accept_task,send_back,add_agent,remove_agent,ask_user" {
		t.Fatalf("leadadd: %s", got)
	}
	// radacina fara echipa: create_task + list_tasks ca sa-i dea de lucru agentului nou, fara accept/send_back
	if got := names("rootsolo"); got != "create_task,list_tasks,add_agent,remove_agent,ask_user" {
		t.Fatalf("rootsolo: %s", got)
	}
	for _, token := range []string{"worker", "sublead", "setup"} {
		if got := names(token); strings.Contains(got, "add_agent") {
			t.Fatalf("%s nu are voie la add_agent: %s", token, got)
		}
	}
}

func TestCallAddAgent(t *testing.T) {
	fb, ts := setup(t)
	_, resp := rpc(t, ts, "leadadd", `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"add_agent","arguments":{"name":"Quinn","role":"QA","model":"haiku","tools":["write","git"],"reason":"tests"}}}`)
	if _, isErr := toolText(t, resp); isErr || fb.calls[0] != "add_agent|Quinn|QA|haiku|[write git]|true|tests" {
		t.Fatalf("%v %v", resp, fb.calls)
	}
	// fara tools: ToolsSet false (backend-ul alege implicitul)
	rpc(t, ts, "rootsolo", `{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"add_agent","arguments":{"name":"Ivo","role":"Dev","reason":"x"}}}`)
	if fb.calls[1] != "add_agent|Ivo|Dev||[]|false|x" {
		t.Fatalf("%v", fb.calls)
	}
	rpc(t, ts, "leadadd", `{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"remove_agent","arguments":{"name":"Quinn"}}}`)
	if fb.calls[2] != "remove_agent|Quinn" {
		t.Fatalf("%v", fb.calls)
	}
	_, resp = rpc(t, ts, "worker", `{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"add_agent","arguments":{"name":"X"}}}`)
	if _, isErr := toolText(t, resp); !isErr || len(fb.calls) != 3 {
		t.Fatal("un worker nu poate chema add_agent")
	}
}
```

- [ ] **Step 2:** `go test ./internal/mcp/` -> FAIL (nu compileaza).

- [ ] **Step 3: `server.go`.** In `Caller`, dupa `HasStages`:

```go
	CanAddAgents bool // lider sau radacina: add_agent / remove_agent
```

Langa `Backend`:

```go
// AddAgentInput sunt argumentele lui add_agent. ToolsSet spune daca agentul a
// trimis "tools" (chiar gol); fara el, backend-ul alege tool-urile implicite.
type AddAgentInput struct {
	Name, Role, Model, Background, Instructions, DoneDef, Reason string
	Tools                                                       []string
	ToolsSet                                                    bool
}
```

(lasa `gofmt` sa alinieze). In `Backend`, dupa `AskUser`:

```go
	AddAgent(c Caller, in AddAgentInput) (string, error)
	RemoveAgent(c Caller, name string) (string, error)
```

In `call`, dupa `case "ask_user"`:

```go
	case "add_agent":
		_, toolsSet := p.Arguments["tools"]
		text, err = s.backend.AddAgent(c, AddAgentInput{
			Name: arg("name"), Role: arg("role"), Model: arg("model"), Background: arg("background"),
			Instructions: arg("instructions"), DoneDef: arg("done_definition"), Reason: arg("reason"),
			Tools: argList(p.Arguments, "tools"), ToolsSet: toolsSet,
		})
	case "remove_agent":
		text, err = s.backend.RemoveAgent(c, arg("name"))
```

- [ ] **Step 4: `tools.go`.** Dupa `askUserTool`:

```go
var addAgentTools = []Tool{
	{
		Name: "add_agent",
		Description: "Adds a new agent to your direct team. Use it only when no current member fits part of the work, or to run " +
			"independent sub-tasks in parallel. Depending on the project, the agent is added at once or the user approves it first; " +
			"the result tells you which. A new agent starts working when you give it a task with create_task.",
		InputSchema: schema([]string{"name", "role", "instructions", "reason"}, map[string]any{
			"name":            str("Short, unique name, e.g. Quinn"),
			"role":            str("The role, e.g. QA Engineer"),
			"instructions":    str("How it works and what it never does"),
			"background":      str("Optional: what it knows about this project"),
			"done_definition": str("Optional: what it hands over when it finishes"),
			"model":           map[string]any{"type": "string", "enum": []string{"opus", "sonnet", "haiku"}, "description": "Optional: the Claude model (default sonnet)"},
			"tools": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string", "enum": []string{"write", "shell", "git", "web"}},
				"description": "Optional: only tools you have yourself (default write)",
			},
			"reason": str("One sentence: why the team needs this agent (the user sees it)"),
		}),
	},
	{
		Name:        "remove_agent",
		Description: "Removes from your team an agent you added, once it has no open tasks. It stays on the canvas, detached, with its history.",
		InputSchema: schema([]string{"name"}, map[string]any{
			"name": str("The agent's name"),
		}),
	},
}
```

In `toolsFor`, dupa blocul `if c.IsLead { ... }`:

```go
	if c.CanAddAgents {
		// o radacina fara echipa primeste create_task / list_tasks ca sa-i dea de lucru agentului nou in acelasi run
		if !c.IsLead {
			out = append(out, leadTools[0], leadTools[1])
		}
		out = append(out, addAgentTools...)
	}
```

(`leadTools[0]` e `create_task`, `leadTools[1]` e `list_tasks`; o radacina fara echipa dintr-un proiect cu etape are deja `IsLead`, deci aici nu e nevoie de `createTaskInStages`.)

- [ ] **Step 5: Stub-urile in team** (`internal/team/agents.go`), ca pachetul sa compileze:

```go
package team

import (
	"errors"

	"foreman/internal/mcp"
)

// AddAgent: tool-ul add_agent (implementarea completa vine in pasul urmator al planului).
func (t *Team) AddAgent(c mcp.Caller, in mcp.AddAgentInput) (string, error) {
	return "", errors.New("add_agent is not available yet")
}

// RemoveAgent: tool-ul remove_agent.
func (t *Team) RemoveAgent(c mcp.Caller, name string) (string, error) {
	return "", errors.New("remove_agent is not available yet")
}
```

- [ ] **Step 6: `run.go`.** In `startRun`, la construirea `mcp.Caller` adauga `CanAddAgents: isLead || roles.IsRoot` (comentariu: `// liderii si radacinile (chiar fara echipa) isi pot mari echipa`).

- [ ] **Step 7:** `go test ./internal/mcp/ ./internal/team/ ./internal/setup/` -> PASS; `go vet ./...`. (`setup.backend` imbrica `mcp.Backend`, deci primeste metodele noi automat.)
- [ ] **Step 8:** commit: `mcp: tool-urile add_agent si remove_agent`

---

### Task 3: Team: regulile pentru agentii adaugati

**Files:**
- Modify: `internal/team/agents.go` (inlocuieste stub-urile)
- Create: `internal/team/agents_test.go`

**Interfaces:**
- Consumes: Task 1 (`AgentSpec`, `AgentRequest`, `CreatedBy`, functiile store); Task 2 (`mcp.AddAgentInput`, `Caller.CanAddAgents`).
- Produces:
  - `const MaxAddedAgents = 6`
  - `func CheckAgentFields(a *store.Agent) error` (folosit si de web in Task 5)
  - `type AgentRequestError string` (cu `Error()`), intors de `ApproveAgentRequest` cand re-validarea pica
  - `(*Team) AddAgent(c mcp.Caller, in mcp.AddAgentInput) (string, error)`
  - `(*Team) RemoveAgent(c mcp.Caller, name string) (string, error)`
  - `(*Team) ApproveAgentRequest(id int64) error` (`store.ErrNotFound`, `store.ErrConflict` daca nu e deschisa, `AgentRequestError`)
  - `(*Team) DismissAgentRequest(id int64) error`
  - Evenimente SSE: `agent` (`{"id": ...}`) si `agent_request` (cererea).
  - Kind-uri in `task_events`: `agent_requested`, `agent_added`, `agent_removed`, `agent_approved`, `agent_dismissed`.

- [ ] **Step 1: Testele** (`agents_test.go`, `package team`, fixture-ul `setup(t)` din `fake_test.go`; `autonomous` din `autonomy_test.go`; `mustReconcile` din `reconcile_test.go`; `hasEvent` din `autonomy_test.go`):

```go
package team

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"foreman/internal/mcp"
	"foreman/internal/runner"
	"foreman/internal/store"
)

// withTools seteaza tool-urile liderului fixture-ului.
func withTools(t *testing.T, f *fixture, tools ...string) {
	t.Helper()
	f.lead.Tools = tools
	if err := f.st.UpdateAgent(f.lead); err != nil {
		t.Fatal(err)
	}
}

func quinn() mcp.AddAgentInput {
	return mcp.AddAgentInput{Name: "Quinn", Role: "QA", Instructions: "Write tests", Reason: "no one writes tests"}
}

func TestAddAgentRequestWithoutAutonomy(t *testing.T) {
	f := setup(t)
	withTools(t, f, "write")
	text, err := f.tm.AddAgent(f.leadCaller(), quinn())
	if err != nil || !strings.Contains(text, "Request sent to the user") {
		t.Fatalf("%q %v", text, err)
	}
	if _, err := f.st.FindAgentByName(f.p.ID, "Quinn"); !errors.Is(err, store.ErrNotFound) {
		t.Fatal("fara aprobare agentul nu exista inca")
	}
	reqs, _ := f.st.ListOpenAgentRequests(f.p.ID)
	if len(reqs) != 1 || reqs[0].LeadID != f.lead.ID || reqs[0].Spec.Model != "sonnet" || strings.Join(reqs[0].Spec.Tools, ",") != "write" {
		t.Fatalf("%+v", reqs)
	}
}

func TestAddAgentDirectWithAutonomy(t *testing.T) {
	f := setup(t)
	withTools(t, f, "write", "git")
	autonomous(t, f)
	in := quinn()
	in.Tools, in.ToolsSet, in.Model = []string{"git"}, true, "haiku"
	text, err := f.tm.AddAgent(f.leadCaller(), in)
	if err != nil || !strings.Contains(text, `"Quinn" added to your team`) {
		t.Fatalf("%q %v", text, err)
	}
	a, err := f.st.FindAgentByName(f.p.ID, "Quinn")
	if err != nil {
		t.Fatal(err)
	}
	if a.ParentID == nil || *a.ParentID != f.lead.ID || a.CreatedBy == nil || *a.CreatedBy != f.lead.ID ||
		a.Model != "haiku" || strings.Join(a.Tools, ",") != "git" || a.ReviewerID != nil || a.PosX != nil {
		t.Fatalf("%+v", a)
	}
}

func TestAddAgentDirectFromChat(t *testing.T) {
	f := setup(t)
	withTools(t, f, "write")
	if _, err := f.tm.UserMessage(f.lead.ID, "add a QA agent", nil); err != nil {
		t.Fatal(err)
	}
	mustReconcile(t, f)
	run := f.fs.last(t, f.lead)
	c := f.caller(t, run)
	if !c.CanAddAgents {
		t.Fatal("liderul trebuie sa aiba CanAddAgents")
	}
	if _, err := f.tm.AddAgent(c, quinn()); err != nil {
		t.Fatal(err)
	}
	if _, err := f.st.FindAgentByName(f.p.ID, "Quinn"); err != nil {
		t.Fatal("din chat agentul se creeaza direct")
	}
	f.fs.finish(run.ID, runner.Outcome{Status: "done"})
}

func TestAddAgentRefusals(t *testing.T) {
	f := setup(t)
	withTools(t, f, "write")
	autonomous(t, f)
	cases := []struct {
		name string
		edit func(*mcp.AddAgentInput)
		want string
	}{
		{"tool peste ale liderului", func(in *mcp.AddAgentInput) { in.Tools, in.ToolsSet = []string{"write", "shell"}, true }, "only give tools you have yourself: shell"},
		{"nume luat", func(in *mcp.AddAgentInput) { in.Name = "rook" }, `an agent named "rook" already exists`},
		{"model necunoscut", func(in *mcp.AddAgentInput) { in.Model = "gpt" }, "unknown model: gpt"},
		{"fara motiv", func(in *mcp.AddAgentInput) { in.Reason = " " }, "reason is required"},
		{"fara rol", func(in *mcp.AddAgentInput) { in.Role = "" }, "role is required"},
		{"fara instructiuni", func(in *mcp.AddAgentInput) { in.Instructions = "" }, "instructions are required"},
		{"tool necunoscut", func(in *mcp.AddAgentInput) { in.Tools, in.ToolsSet = []string{"root"}, true }, "unknown tool: root"},
	}
	for _, tc := range cases {
		in := quinn()
		tc.edit(&in)
		if _, err := f.tm.AddAgent(f.leadCaller(), in); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: %v", tc.name, err)
		}
	}
	// un worker cu sef nu poate adauga agenti
	if _, err := f.tm.AddAgent(f.workerCaller(f.rook, 0), quinn()); err == nil {
		t.Error("un worker nu poate adauga agenti")
	}
}

func TestAddAgentLimit(t *testing.T) {
	f := setup(t)
	withTools(t, f, "write")
	// 4 agenti directi (autonomy) + 2 cereri = 6
	autonomous(t, f)
	for i := 0; i < 4; i++ {
		in := quinn()
		in.Name = fmt.Sprintf("A%d", i)
		if _, err := f.tm.AddAgent(f.leadCaller(), in); err != nil {
			t.Fatal(err)
		}
	}
	f.p.Autonomy = false
	f.st.UpdateProject(f.p)
	for i := 0; i < 2; i++ {
		in := quinn()
		in.Name = fmt.Sprintf("R%d", i)
		if _, err := f.tm.AddAgent(f.leadCaller(), in); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.tm.AddAgent(f.leadCaller(), quinn()); err == nil || !strings.Contains(err.Error(), "already have 6 agents") {
		t.Fatalf("a 7-a: %v", err)
	}
	// numele unei cereri deschise e ocupat
	in := quinn()
	in.Name = "r0"
	if _, err := f.tm.AddAgent(f.leadCaller(), in); err == nil || !strings.Contains(err.Error(), "waiting for approval") {
		t.Fatalf("nume cerut: %v", err)
	}
}

func TestApproveAgentRequest(t *testing.T) {
	f := setup(t)
	withTools(t, f, "write")
	task := f.task(t, "Ship it", f.lead, store.StatusInProgress)
	c := f.leadCaller()
	c.TaskID = task.ID
	if _, err := f.tm.AddAgent(c, quinn()); err != nil {
		t.Fatal(err)
	}
	reqs, _ := f.st.ListOpenAgentRequests(f.p.ID)
	if err := f.tm.ApproveAgentRequest(reqs[0].ID); err != nil {
		t.Fatal(err)
	}
	a, err := f.st.FindAgentByName(f.p.ID, "Quinn")
	if err != nil || *a.ParentID != f.lead.ID || *a.CreatedBy != f.lead.ID {
		t.Fatalf("%+v %v", a, err)
	}
	// nota catre lider e eveniment, nu mesaj al utilizatorului (altfel run-ul urmator ar parea pornit din chat)
	msgs, _ := f.st.ListMessages(f.lead.ID)
	last := msgs[len(msgs)-1]
	if !strings.Contains(last.Text, `approved your request: "Quinn"`) || last.Kind != store.KindEvent || last.Author == store.AuthorUser || last.Delivered {
		t.Fatalf("nota: %+v", last)
	}
	if !hasEvent(t, f, task.ID, "agent_requested", f.lead.Name, `Requested agent "Quinn"`) ||
		!hasEvent(t, f, task.ID, "agent_approved", "You", `Approved agent "Quinn"`) {
		t.Fatal("lipsesc evenimentele din istoric")
	}
	// a doua aprobare (dublu-click)
	if err := f.tm.ApproveAgentRequest(reqs[0].ID); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("a doua aprobare: %v", err)
	}
}

func TestApproveRevalidates(t *testing.T) {
	f := setup(t)
	withTools(t, f, "write")
	if _, err := f.tm.AddAgent(f.leadCaller(), quinn()); err != nil {
		t.Fatal(err)
	}
	reqs, _ := f.st.ListOpenAgentRequests(f.p.ID)
	// intre timp utilizatorul creeaza din canvas un agent cu acelasi nume
	f.agent(t, "Quinn", &f.lead, nil)
	err := f.tm.ApproveAgentRequest(reqs[0].ID)
	var are AgentRequestError
	if !errors.As(err, &are) || !strings.Contains(err.Error(), `"Quinn" already exists`) {
		t.Fatalf("nume luat: %v", err)
	}
	if r, _ := f.st.GetAgentRequest(reqs[0].ID); r.Status != store.AgentRequestOpen {
		t.Fatal("cererea ramane deschisa")
	}
	// liderul si-a pierdut tool-ul cerut (cererea Ivo cere "write", apoi liderul ramane fara tool-uri)
	in := quinn()
	in.Name = "Ivo"
	if _, err := f.tm.AddAgent(f.leadCaller(), in); err != nil {
		t.Fatal(err)
	}
	withTools(t, f)
	reqs, _ = f.st.ListOpenAgentRequests(f.p.ID)
	if err := f.tm.ApproveAgentRequest(reqs[len(reqs)-1].ID); !errors.As(err, &are) || !strings.Contains(err.Error(), "write") {
		t.Fatalf("tool pierdut: %v", err)
	}
}

func TestDismissAgentRequest(t *testing.T) {
	f := setup(t)
	withTools(t, f, "write")
	f.tm.AddAgent(f.leadCaller(), quinn())
	reqs, _ := f.st.ListOpenAgentRequests(f.p.ID)
	if err := f.tm.DismissAgentRequest(reqs[0].ID); err != nil {
		t.Fatal(err)
	}
	if !f.hasMessage(f.lead.ID, `dismissed your request to add "Quinn"`) {
		t.Fatal("liderul afla de respingere")
	}
	if _, err := f.st.FindAgentByName(f.p.ID, "Quinn"); !errors.Is(err, store.ErrNotFound) {
		t.Fatal("respins = fara agent")
	}
}

func TestRemoveAgent(t *testing.T) {
	f := setup(t)
	withTools(t, f, "write")
	autonomous(t, f)
	f.tm.AddAgent(f.leadCaller(), quinn())
	q, _ := f.st.FindAgentByName(f.p.ID, "Quinn")

	if _, err := f.tm.RemoveAgent(f.leadCaller(), "Rook"); err == nil || !strings.Contains(err.Error(), "only remove agents you added") {
		t.Fatalf("Rook: %v", err)
	}
	open := f.task(t, "Test", q, store.StatusInProgress)
	if _, err := f.tm.RemoveAgent(f.leadCaller(), "quinn"); err == nil || !strings.Contains(err.Error(), "still has open tasks: "+open.Ref()) {
		t.Fatalf("cu task deschis: %v", err)
	}
	open.Status = store.StatusDone
	f.st.SaveTask(&open)
	text, err := f.tm.RemoveAgent(f.leadCaller(), "Quinn")
	if err != nil || !strings.Contains(text, "removed from your team") {
		t.Fatalf("%q %v", text, err)
	}
	q, _ = f.st.GetAgent(q.ID)
	if q.ParentID != nil || !q.Detached {
		t.Fatalf("trebuia detasat: %+v", q)
	}
	if n, _ := f.st.CountAddedBy(f.lead.ID); n != 0 {
		t.Fatalf("limita se elibereaza: %d", n)
	}
}

func TestRemoveAgentMovedOrLeading(t *testing.T) {
	f := setup(t)
	withTools(t, f, "write")
	autonomous(t, f)
	for _, n := range []string{"Quinn", "Ivo"} {
		in := quinn()
		in.Name = n
		f.tm.AddAgent(f.leadCaller(), in)
	}
	// utilizatorul muta Quinn sub Rook
	q, _ := f.st.FindAgentByName(f.p.ID, "Quinn")
	q.ParentID = &f.rook.ID
	f.st.UpdateAgent(q)
	if _, err := f.tm.RemoveAgent(f.leadCaller(), "Quinn"); err == nil || !strings.Contains(err.Error(), "not on your team") {
		t.Fatalf("mutat: %v", err)
	}
	// utilizatorul pune un agent sub Ivo: Ivo are echipa proprie
	ivo, _ := f.st.FindAgentByName(f.p.ID, "Ivo")
	f.agent(t, "Kid", &ivo, nil)
	if _, err := f.tm.RemoveAgent(f.leadCaller(), "Ivo"); err == nil || !strings.Contains(err.Error(), "has its own team") {
		t.Fatalf("cu echipa: %v", err)
	}
}

func TestSoloRootAddsAndDelegates(t *testing.T) {
	f := setup(t)
	solo := f.agent(t, "Solo", nil, nil) // a doua radacina, fara echipa
	solo.Tools = []string{"write"}
	f.st.UpdateAgent(solo)
	autonomous(t, f)
	c := mcp.Caller{ProjectID: f.p.ID, AgentID: solo.ID, CanAddAgents: true}
	if _, err := f.tm.AddAgent(c, quinn()); err != nil {
		t.Fatal(err)
	}
	// acelasi Caller (flag-urile de la pornirea run-ului): create_task citeste echipa din DB
	if _, err := f.tm.CreateTask(c, "Write tests", "", "Quinn"); err != nil {
		t.Fatalf("create_task pentru agentul nou: %v", err)
	}
}

func TestAddAgentWithStagesSaysSo(t *testing.T) {
	f := setup(t)
	withTools(t, f, "write")
	autonomous(t, f)
	if _, err := f.st.SaveStages(f.p.ID, []store.Stage{{Name: "Build", Kind: "work", AgentIDs: []int64{f.rook.ID}}}); err != nil {
		t.Fatal(err)
	}
	text, err := f.tm.AddAgent(f.leadCaller(), quinn())
	if err != nil || !strings.Contains(text, "add it to a stage") {
		t.Fatalf("%q %v", text, err)
	}
}
```

Inainte de Step 2, verifica numele exacte din store pentru etape (`grep -n "func (s \*Store) .*Stage" internal/store/stages.go`) si campurile `store.Stage`; adapteaza doar apelul `SaveStages` din ultimul test la semnatura reala (comportamentul testat ramane acelasi).

- [ ] **Step 2:** `go test ./internal/team/ -run 'AddAgent|Approve|Dismiss|RemoveAgent|SoloRoot'` -> FAIL.

- [ ] **Step 3: Implementarea** (`internal/team/agents.go`, inlocuieste tot fisierul):

```go
package team

import (
	"errors"
	"fmt"
	"log"
	"slices"
	"strings"
	"unicode/utf8"

	"foreman/internal/mcp"
	"foreman/internal/models"
	"foreman/internal/prompt"
	"foreman/internal/store"
)

// MaxAddedAgents: cati agenti (in echipa sau ceruti) poate adauga un lider.
const MaxAddedAgents = 6

// AgentRequestError: cererea nu mai poate fi aprobata asa cum e (nume luat,
// limita plina, liderul si-a pierdut un tool sau rolul). Cererea ramane deschisa.
type AgentRequestError string

func (e AgentRequestError) Error() string { return string(e) }

// CheckAgentFields valideaza campurile unui agent, la fel pentru UI si pentru
// add_agent: nume obligatoriu, model si tool-uri cunoscute (fara duplicate), lungimi.
func CheckAgentFields(a *store.Agent) error {
	if a.Name == "" {
		return errors.New("agent name is required")
	}
	if !models.Valid(a.Model) {
		return errors.New("unknown model: " + a.Model)
	}
	tools := []string{}
	for _, tool := range a.Tools {
		if !slices.Contains(prompt.ValidTools, tool) {
			return errors.New("unknown tool: " + tool)
		}
		if !slices.Contains(tools, tool) {
			tools = append(tools, tool)
		}
	}
	a.Tools = tools
	for _, f := range []struct {
		label, value string
		max          int
	}{
		{"agent name", a.Name, 40},
		{"role", a.Role, 80},
		{"background", a.Background, 10000},
		{"instructions", a.Instructions, 10000},
		{"definition of done", a.DoneDef, 4000},
		{"responsibilities", a.Responsibilities, 2000},
	} {
		if utf8.RuneCountInString(f.value) > f.max {
			return fmt.Errorf("%s: at most %d characters", f.label, f.max)
		}
	}
	return nil
}

// canAddAgents: liderii si radacinile isi pot mari echipa (din DB, nu din flag-urile run-ului).
func canAddAgents(a store.Agent, agents []store.Agent) bool {
	r := store.RolesOf(a, agents)
	return r.IsLead || r.IsRoot
}

// newAgentSpec curata si valideaza argumentele lui add_agent pentru liderul dat.
func newAgentSpec(in mcp.AddAgentInput, lead store.Agent) (store.AgentSpec, string, error) {
	s := store.AgentSpec{
		Name: strings.TrimSpace(in.Name), Role: strings.TrimSpace(in.Role), Model: strings.TrimSpace(in.Model),
		Background: strings.TrimSpace(in.Background), Instructions: strings.TrimSpace(in.Instructions),
		DoneDef: strings.TrimSpace(in.DoneDef),
	}
	if s.Model == "" {
		s.Model = "sonnet"
	}
	switch {
	case in.ToolsSet:
		s.Tools = in.Tools
	case slices.Contains(lead.Tools, "write"):
		s.Tools = []string{"write"}
	default:
		s.Tools = []string{}
	}
	reason := strings.TrimSpace(in.Reason)
	switch {
	case s.Role == "":
		return s, "", errors.New("role is required")
	case s.Instructions == "":
		return s, "", errors.New("instructions are required")
	case reason == "":
		return s, "", errors.New("reason is required: say in one sentence why the team needs this agent")
	case utf8.RuneCountInString(reason) > 300:
		return s, "", errors.New("reason: at most 300 characters")
	}
	a := agentFrom(s, lead)
	if err := CheckAgentFields(&a); err != nil {
		return s, "", err
	}
	s.Tools = a.Tools
	var extra []string
	for _, tool := range s.Tools {
		if !slices.Contains(lead.Tools, tool) {
			extra = append(extra, tool)
		}
	}
	if len(extra) > 0 {
		return s, "", fmt.Errorf("you can only give tools you have yourself: %s", strings.Join(extra, ", "))
	}
	return s, reason, nil
}

// agentFrom construieste agentul din propunere: sub lider, in folderul lui, fara reviewer.
func agentFrom(s store.AgentSpec, lead store.Agent) store.Agent {
	leadID := lead.ID
	return store.Agent{
		ProjectID: lead.ProjectID, Name: s.Name, Role: s.Role, Model: s.Model, Background: s.Background,
		Instructions: s.Instructions, DoneDef: s.DoneDef, Tools: s.Tools,
		ParentID: &leadID, FolderID: lead.FolderID, CreatedBy: &leadID,
	}
}

// checkAvailable verifica numele si limita liderului. La aprobare, cererea
// insasi e inca deschisa: ii ocupa numele si un loc din limita, deci nu se numara.
func (t *Team) checkAvailable(projectID, leadID int64, name string, approving bool) error {
	if _, err := t.st.FindAgentByName(projectID, name); err == nil {
		return fmt.Errorf("an agent named %q already exists", name)
	} else if !errors.Is(err, store.ErrNotFound) {
		return err
	}
	if !approving {
		open, err := t.st.OpenAgentRequestNamed(projectID, name)
		if err != nil {
			return err
		}
		if open {
			return fmt.Errorf("an agent named %q is already waiting for approval", name)
		}
	}
	n, err := t.st.CountAddedBy(leadID)
	if err != nil {
		return err
	}
	if (!approving && n >= MaxAddedAgents) || (approving && n > MaxAddedAgents) {
		return fmt.Errorf("you already have %d agents you added (or requested); remove one with remove_agent first", MaxAddedAgents)
	}
	return nil
}

// callerTask intoarce task-ul run-ului (pentru istoric), daca exista.
func (t *Team) callerTask(taskID int64) *store.Task {
	if taskID == 0 {
		return nil
	}
	task, err := t.st.GetTask(taskID)
	if err != nil {
		return nil
	}
	return &task
}

// addedText: raspunsul catre lider dupa ce agentul a intrat in echipa.
func (t *Team) addedText(projectID int64, name string) string {
	text := fmt.Sprintf("Agent %q added to your team. Give it work with create_task.", name)
	if stages, err := t.st.ListStages(projectID); err == nil && len(stages) > 0 {
		text = fmt.Sprintf("Agent %q added to your team. This project uses stages: it takes tasks only after the user adds it to a stage (Columns).", name)
	}
	return text
}

// AddAgent e tool-ul add_agent: direct cu Full autonomy sau intr-un run pornit
// din chat-ul utilizatorului, altfel o cerere in "Needs you".
func (t *Team) AddAgent(c mcp.Caller, in mcp.AddAgentInput) (string, error) {
	text, added, err := t.addAgent(c, in)
	if err != nil {
		return "", err
	}
	if added {
		// rolurile s-au schimbat (liderul poate avea primul agent): recalculam si pornim ce e de pornit
		if err := t.LinksChanged(c.ProjectID); err != nil {
			log.Printf("add_agent: %v", err)
		}
	}
	return text, nil
}

func (t *Team) addAgent(c mcp.Caller, in mcp.AddAgentInput) (string, bool, error) {
	t.work.Lock()
	defer t.work.Unlock()
	agents, err := t.st.ListAgents(c.ProjectID)
	if err != nil {
		return "", false, err
	}
	lead, ok := store.FindAgent(agents, c.AgentID)
	if !ok {
		return "", false, errors.New("your agent no longer exists")
	}
	if !canAddAgents(lead, agents) {
		return "", false, errors.New("only a lead or an agent that reports to the user can add agents")
	}
	spec, reason, err := newAgentSpec(in, lead)
	if err != nil {
		return "", false, err
	}
	if err := t.checkAvailable(c.ProjectID, lead.ID, spec.Name, false); err != nil {
		return "", false, err
	}
	task := t.callerTask(c.TaskID)
	_, fromChat := t.requestOf(c.AgentID, c.RunID)
	if t.autonomyOf(c.ProjectID) || fromChat {
		a := agentFrom(spec, lead)
		if err := t.st.CreateAgent(&a); errors.Is(err, store.ErrConflict) {
			return "", false, fmt.Errorf("an agent named %q already exists", a.Name)
		} else if err != nil {
			return "", false, err
		}
		if task != nil {
			t.taskEvent(*task, "agent_added", lead.Name, fmt.Sprintf("Added agent %q", a.Name))
		}
		t.emit(c.ProjectID, "agent", map[string]int64{"id": a.ID})
		return t.addedText(c.ProjectID, a.Name), true, nil
	}
	r := store.AgentRequest{ProjectID: c.ProjectID, LeadID: lead.ID, RunID: c.RunID, Spec: spec, Reason: reason}
	if task != nil {
		id := task.ID
		r.TaskID = &id
	}
	if err := t.st.AddAgentRequest(&r); err != nil {
		return "", false, err
	}
	if task != nil {
		t.taskEvent(*task, "agent_requested", lead.Name, fmt.Sprintf("Requested agent %q", spec.Name))
	}
	t.emit(c.ProjectID, "agent_request", r)
	return "Request sent to the user. You will get a message when it is approved or dismissed; continue with your current work meanwhile.", false, nil
}

// RemoveAgent e tool-ul remove_agent: detaseaza un agent adaugat de lider, fara task-uri deschise.
func (t *Team) RemoveAgent(c mcp.Caller, name string) (string, error) {
	text, err := t.removeAgent(c, name)
	if err != nil {
		return "", err
	}
	if err := t.LinksChanged(c.ProjectID); err != nil {
		log.Printf("remove_agent: %v", err)
	}
	return text, nil
}

func (t *Team) removeAgent(c mcp.Caller, name string) (string, error) {
	t.work.Lock()
	defer t.work.Unlock()
	name = strings.TrimSpace(name)
	if name == "" {
		return "", errors.New("name is required")
	}
	a, err := t.st.FindAgentByName(c.ProjectID, name)
	if errors.Is(err, store.ErrNotFound) {
		return "", fmt.Errorf("there is no agent named %q", name)
	}
	if err != nil {
		return "", err
	}
	if a.CreatedBy == nil || *a.CreatedBy != c.AgentID {
		return "", errors.New("you can only remove agents you added")
	}
	if a.ParentID == nil || *a.ParentID != c.AgentID {
		return "", fmt.Errorf("%q is not on your team", a.Name)
	}
	tasks, err := t.st.ListTasks(c.ProjectID)
	if err != nil {
		return "", err
	}
	var open []string
	for _, task := range tasks {
		if task.AssigneeID == a.ID && task.Status != store.StatusDone {
			open = append(open, task.Ref())
		}
	}
	if len(open) > 0 {
		return "", fmt.Errorf("%q still has open tasks: %s", a.Name, strings.Join(open, ", "))
	}
	agents, err := t.st.ListAgents(c.ProjectID)
	if err != nil {
		return "", err
	}
	if r := store.RolesOf(a, agents); r.IsLead || r.IsReviewer {
		return "", fmt.Errorf("%q has its own team or reviews someone; ask the user to change it", a.Name)
	}
	a.ParentID, a.Detached = nil, true
	if err := t.st.UpdateAgent(a); err != nil {
		return "", err
	}
	if task := t.callerTask(c.TaskID); task != nil {
		lead, _ := store.FindAgent(agents, c.AgentID)
		t.taskEvent(*task, "agent_removed", lead.Name, fmt.Sprintf("Removed agent %q", a.Name))
	}
	t.emit(c.ProjectID, "agent", map[string]int64{"id": a.ID})
	return fmt.Sprintf("%q removed from your team (kept on the canvas, detached).", a.Name), nil
}

// ApproveAgentRequest creeaza agentul cerut, dupa o noua validare, si ii spune liderului.
func (t *Team) ApproveAgentRequest(id int64) error {
	projectID, err := t.approveAgentRequest(id)
	if err != nil {
		return err
	}
	return t.LinksChanged(projectID)
}

func (t *Team) approveAgentRequest(id int64) (int64, error) {
	t.work.Lock()
	defer t.work.Unlock()
	r, err := t.st.GetAgentRequest(id)
	if err != nil {
		return 0, err
	}
	if r.Status != store.AgentRequestOpen {
		return 0, store.ErrConflict
	}
	agents, err := t.st.ListAgents(r.ProjectID)
	if err != nil {
		return 0, err
	}
	lead, ok := store.FindAgent(agents, r.LeadID)
	if !ok {
		return 0, store.ErrNotFound
	}
	if !canAddAgents(lead, agents) {
		return 0, AgentRequestError(fmt.Sprintf("%s can no longer add agents (it is not a lead or a root anymore)", lead.Name))
	}
	in := mcp.AddAgentInput{Name: r.Spec.Name, Role: r.Spec.Role, Model: r.Spec.Model, Background: r.Spec.Background,
		Instructions: r.Spec.Instructions, DoneDef: r.Spec.DoneDef, Reason: r.Reason, Tools: r.Spec.Tools, ToolsSet: true}
	spec, _, err := newAgentSpec(in, lead)
	if err != nil {
		return 0, AgentRequestError(err.Error())
	}
	if err := t.checkAvailable(r.ProjectID, lead.ID, spec.Name, true); err != nil {
		return 0, AgentRequestError(err.Error())
	}
	a := agentFrom(spec, lead)
	if err := t.st.ApproveAgentRequest(r.ID, &a); errors.Is(err, store.ErrConflict) {
		// cererea e inca deschisa (verificat mai sus), deci conflictul e numele
		return 0, AgentRequestError(fmt.Sprintf("an agent named %q already exists", a.Name))
	} else if err != nil {
		return 0, err
	}
	// nota (eveniment), nu mesaj al utilizatorului: run-ul urmator nu trebuie sa para pornit din chat
	t.note(r.ProjectID, lead.ID, fmt.Sprintf("The user approved your request: %q is now on your team. Give it work with create_task.", a.Name), false)
	if r.TaskID != nil {
		if task := t.callerTask(*r.TaskID); task != nil {
			t.taskEvent(*task, "agent_approved", actorUser, fmt.Sprintf("Approved agent %q", a.Name))
		}
	}
	r.Status, r.AgentID = store.AgentRequestApproved, &a.ID
	t.emit(r.ProjectID, "agent", map[string]int64{"id": a.ID})
	t.emit(r.ProjectID, "agent_request", r)
	return r.ProjectID, nil
}

// DismissAgentRequest inchide cererea fara agent si ii spune liderului.
func (t *Team) DismissAgentRequest(id int64) error {
	t.work.Lock()
	r, err := t.st.GetAgentRequest(id)
	if err == nil {
		err = t.st.DismissAgentRequest(id)
	}
	if err != nil {
		t.work.Unlock()
		return err
	}
	t.note(r.ProjectID, r.LeadID, fmt.Sprintf("The user dismissed your request to add %q. Continue with your current team.", r.Spec.Name), false)
	if r.TaskID != nil {
		if task := t.callerTask(*r.TaskID); task != nil {
			t.taskEvent(*task, "agent_dismissed", actorUser, fmt.Sprintf("Dismissed agent %q", r.Spec.Name))
		}
	}
	r.Status = store.AgentRequestDismissed
	t.emit(r.ProjectID, "agent_request", r)
	t.work.Unlock()
	t.Kick(r.ProjectID)
	return nil
}
```

Note:
- `LinksChanged` ia singur `t.work`, de aceea `addAgent` / `removeAgent` / `approveAgentRequest` sunt separate si elibereaza lock-ul (prin `defer`) inainte.
- Verifica in `internal/team/events.go` ca `taskEvent` nu cere `t.work` (e apelat deja sub lock din alte tool-uri) si ca `actorUser` e `"You"`.
- Daca `models` importa ceva din `team` (ciclu), opreste-te si raporteaza; nu ar trebui (`internal/models` e o lista de modele).

- [ ] **Step 4:** `go test ./internal/team/` si `go test -race ./internal/team/` -> PASS (inclusiv testele existente).
- [ ] **Step 5:** `gofmt -l internal/team` gol; commit: `team: add_agent / remove_agent, aprobare si respingere`

---

### Task 4: Prompt: paragraful despre `add_agent`

**Files:**
- Modify: `internal/prompt/prompt.go` (`Links`, `LinksFor`, `System`)
- Modify: `internal/prompt/prompt_test.go`

**Interfaces:**
- Produces: `Links.CanAddAgents bool` (lider sau radacina), calculat in `LinksFor`.

- [ ] **Step 1: Testul** (in `prompt_test.go`; urmareste cum construiesc `TestSystemRoles` / `TestLinksFor` proiectul si agentii si foloseste aceleasi helper-e):

```go
func TestSystemAddAgents(t *testing.T) {
	p := store.Project{Name: "P", Goal: "G"}
	lead := store.Agent{ID: 1, Name: "Lead"}
	worker := store.Agent{ID: 2, Name: "Rook", ParentID: ptrID(1)}
	team := []store.Agent{lead, worker}
	if l := LinksFor(lead, team); !l.CanAddAgents {
		t.Fatal("radacina/liderul poate adauga agenti")
	}
	if l := LinksFor(worker, team); l.CanAddAgents {
		t.Fatal("un worker cu sef nu poate")
	}
	solo := store.Agent{ID: 3, Name: "Solo"}
	if l := LinksFor(solo, []store.Agent{solo}); !l.CanAddAgents {
		t.Fatal("radacina fara echipa poate")
	}
	if s := System(p, lead, LinksFor(lead, team)); !strings.Contains(s, "mcp__team__add_agent") || !strings.Contains(s, "mcp__team__remove_agent") {
		t.Fatalf("liderul trebuie sa afle de add_agent:\n%s", s)
	}
	if s := System(p, worker, LinksFor(worker, team)); strings.Contains(s, "add_agent") {
		t.Fatal("workerul nu afla de add_agent")
	}
}
```

Daca in `prompt_test.go` nu exista un helper pentru `*int64`, adauga `func ptrID(v int64) *int64 { return &v }` (sau foloseste-l pe cel existent, cu alt nume).

- [ ] **Step 2:** `go test ./internal/prompt/` -> FAIL.

- [ ] **Step 3: Implementarea.** In `Links`, dupa `IsReviewer`: `CanAddAgents bool // lider sau radacina: are add_agent / remove_agent`. In `LinksFor`: `l := Links{IsLead: r.IsLead, IsReviewer: r.IsReviewer, CanAddAgents: r.IsLead || r.IsRoot}`. Langa `howLead`:

```go
const howAddAgents = `You can grow your team with mcp__team__add_agent: only when no current member fits part of the work, ` +
	`or when you have independent sub-tasks to run in parallel. Check your team first. A new agent starts working when you ` +
	`give it a task with mcp__team__create_task. When an agent you added has no more work, remove it with mcp__team__remove_agent.`
```

In `System`, inainte de `if l.IsReviewer {`: `if l.CanAddAgents { how = append(how, howAddAgents) }`.

- [ ] **Step 4:** `go test ./internal/prompt/ ./internal/team/` -> PASS (testele din team care compara prompt-uri intregi, daca exista, se actualizeaza doar cu paragraful nou).
- [ ] **Step 5:** commit: `prompt: liderii si radacinile afla de add_agent`

---

### Task 5: Web: API pentru cereri si validarea comuna

**Files:**
- Create: `internal/web/api_agent_requests.go`
- Create: `internal/web/agent_requests_test.go`
- Modify: `internal/web/server.go` (rute, `fail`)
- Modify: `internal/web/api_projects.go` (`projectCard.AgentRequests`, `projectDetail.AgentRequests`)
- Modify: `internal/web/api_agents.go` (`apply` foloseste `team.CheckAgentFields`)

**Interfaces:**
- Consumes: Task 1 (store), Task 3 (`team.ApproveAgentRequest`, `team.DismissAgentRequest`, `team.AgentRequestError`, `team.CheckAgentFields`).
- Produces:
  - `POST /api/agent-requests/{id}/approve` si `POST /api/agent-requests/{id}/dismiss`: 204; 404 inexistenta; 409 inchisa sau re-validare picata (`{"error": "<mesaj>"}`).
  - `GET /api/projects/{id}`: `agentRequests: [{id, leadId, leadName, taskId, ref, spec, reason, createdAt}]` (mereu non-nil); cardul: `agentRequests` (numar).
  - JSON-ul agentilor primeste `createdBy` (din `store.Agent`).

- [ ] **Step 1: Testele** (`agent_requests_test.go`, cu `setup`, `e.do`, `e.createProject`, `e.root`, `itoa` din `server_test.go`):

```go
package web

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"foreman/internal/mcp"
	"foreman/internal/store"
)

// requestFromLead face o cerere add_agent de la radacina proiectului (fara autonomy, deci cerere deschisa).
func requestFromLead(t *testing.T, e *env, p store.Project, name string) store.AgentRequest {
	t.Helper()
	lead := e.root(t, p.ID)
	lead.Tools = []string{"write"}
	if err := e.st.UpdateAgent(lead); err != nil {
		t.Fatal(err)
	}
	c := mcp.Caller{ProjectID: p.ID, AgentID: lead.ID, CanAddAgents: true}
	if _, err := e.tm.AddAgent(c, mcp.AddAgentInput{Name: name, Role: "QA", Instructions: "test", Reason: "tests"}); err != nil {
		t.Fatal(err)
	}
	reqs, _ := e.st.ListOpenAgentRequests(p.ID)
	return reqs[len(reqs)-1]
}

func TestAgentRequestsInProject(t *testing.T) {
	e := setup(t)
	p := e.createProject(t, t.TempDir())
	r := requestFromLead(t, e, p, "Quinn")
	code, raw := e.do(t, "GET", "/api/projects/"+itoa(p.ID), nil)
	var d struct {
		AgentRequests []struct {
			ID       int64           `json:"id"`
			LeadName string          `json:"leadName"`
			Spec     store.AgentSpec `json:"spec"`
			Reason   string          `json:"reason"`
		} `json:"agentRequests"`
	}
	json.Unmarshal(raw, &d)
	if code != 200 || len(d.AgentRequests) != 1 || d.AgentRequests[0].ID != r.ID || d.AgentRequests[0].LeadName == "" ||
		d.AgentRequests[0].Spec.Name != "Quinn" || d.AgentRequests[0].Reason != "tests" {
		t.Fatalf("%d %s", code, raw)
	}
	_, raw = e.do(t, "GET", "/api/projects", nil)
	if !strings.Contains(string(raw), `"agentRequests":1`) {
		t.Fatalf("cardul: %s", raw)
	}
}

func TestApproveAgentRequestAPI(t *testing.T) {
	e := setup(t)
	p := e.createProject(t, t.TempDir())
	r := requestFromLead(t, e, p, "Quinn")
	if code, raw := e.do(t, "POST", "/api/agent-requests/"+itoa(r.ID)+"/approve", nil); code != http.StatusNoContent {
		t.Fatalf("%d %s", code, raw)
	}
	a, err := e.st.FindAgentByName(p.ID, "Quinn")
	if err != nil || a.CreatedBy == nil {
		t.Fatalf("%+v %v", a, err)
	}
	if code, _ := e.do(t, "POST", "/api/agent-requests/"+itoa(r.ID)+"/approve", nil); code != http.StatusConflict {
		t.Fatalf("a doua aprobare: %d", code)
	}
	if code, _ := e.do(t, "POST", "/api/agent-requests/9999/approve", nil); code != http.StatusNotFound {
		t.Fatalf("inexistenta: %d", code)
	}
	_, raw := e.do(t, "GET", "/api/projects/"+itoa(p.ID), nil)
	if !strings.Contains(string(raw), `"createdBy":`+itoa(*a.CreatedBy)) {
		t.Fatalf("createdBy lipseste din agenti: %s", raw)
	}
}

func TestApproveAgentRequestNameTakenAPI(t *testing.T) {
	e := setup(t)
	p := e.createProject(t, t.TempDir())
	r := requestFromLead(t, e, p, "Quinn")
	e.addWorker(t, p.ID, "Quinn")
	code, raw := e.do(t, "POST", "/api/agent-requests/"+itoa(r.ID)+"/approve", nil)
	if code != http.StatusConflict || !strings.Contains(string(raw), `already exists`) {
		t.Fatalf("%d %s", code, raw)
	}
}

func TestDismissAgentRequestAPI(t *testing.T) {
	e := setup(t)
	p := e.createProject(t, t.TempDir())
	r := requestFromLead(t, e, p, "Quinn")
	if code, raw := e.do(t, "POST", "/api/agent-requests/"+itoa(r.ID)+"/dismiss", nil); code != http.StatusNoContent {
		t.Fatalf("%d %s", code, raw)
	}
	if code, _ := e.do(t, "POST", "/api/agent-requests/"+itoa(r.ID)+"/dismiss", nil); code != http.StatusConflict {
		t.Fatalf("a doua oara: %d", code)
	}
}
```

- [ ] **Step 2:** `go test ./internal/web/ -run AgentRequest` -> FAIL.

- [ ] **Step 3: `api_agent_requests.go`:**

```go
package web

import (
	"net/http"
	"time"

	"foreman/internal/store"
)

// agentRequestView e o cerere add_agent deschisa, asa cum o vede UI-ul.
type agentRequestView struct {
	ID        int64           `json:"id"`
	LeadID    int64           `json:"leadId"`
	LeadName  string          `json:"leadName"`
	TaskID    *int64          `json:"taskId"`
	Ref       string          `json:"ref"` // T-3, gol daca cererea n-a venit de pe un task
	Spec      store.AgentSpec `json:"spec"`
	Reason    string          `json:"reason"`
	CreatedAt time.Time       `json:"createdAt"`
}

// agentRequestViews intoarce cererile deschise ale proiectului (lista e mereu non-nil).
func (s *Server) agentRequestViews(projectID int64, agents []store.Agent, tasks []store.Task) ([]agentRequestView, error) {
	reqs, err := s.st.ListOpenAgentRequests(projectID)
	if err != nil {
		return nil, err
	}
	out := make([]agentRequestView, 0, len(reqs))
	for _, r := range reqs {
		v := agentRequestView{ID: r.ID, LeadID: r.LeadID, TaskID: r.TaskID, Spec: r.Spec, Reason: r.Reason, CreatedAt: r.CreatedAt}
		if lead, ok := store.FindAgent(agents, r.LeadID); ok {
			v.LeadName = lead.Name
		}
		if r.TaskID != nil {
			for _, t := range tasks {
				if t.ID == *r.TaskID {
					v.Ref = t.Ref()
				}
			}
		}
		out = append(out, v)
	}
	return out, nil
}

func (s *Server) approveAgentRequest(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		fail(w, err)
		return
	}
	if err := s.team.ApproveAgentRequest(id); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) dismissAgentRequest(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		fail(w, err)
		return
	}
	if err := s.team.DismissAgentRequest(id); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
```

(Evenimentele SSE le emite team; verifica in `server.go` ca `s.team` e numele campului — `getProject` foloseste `s.team.StuckTasks`, deci da.)

- [ ] **Step 4: Legaturile.**
  - `server.go`, langa rutele `/api/access/...`:
    ```go
    	mux.HandleFunc("POST /api/agent-requests/{id}/approve", s.approveAgentRequest)
    	mux.HandleFunc("POST /api/agent-requests/{id}/dismiss", s.dismissAgentRequest)
    ```
  - `fail`: `var are team.AgentRequestError` si un caz nou inainte de `errors.Is(err, store.ErrNotFound)`:
    ```go
    	case errors.As(err, &are):
    		writeErr(w, http.StatusConflict, are.Error())
    ```
    (`store.ErrConflict` -> 409 exista deja.)
  - `api_projects.go`: in `projectCard`, dupa `AccessRequests`:
    ```go
    	// AgentRequests: cereri deschise ale liderilor de a adauga agenti
    	AgentRequests int `json:"agentRequests"`
    ```
    in `card`, dupa `CountOpenAccessRequests`: `if c.AgentRequests, err = s.st.CountOpenAgentRequests(p.ID); err != nil { return projectCard{}, nil, nil, nil, err }`.
    In `projectDetail`, dupa `Access`:
    ```go
    	// AgentRequests: cererile add_agent deschise (mereu non-nil)
    	AgentRequests []agentRequestView `json:"agentRequests"`
    ```
    Atentie: `projectDetail` imbrica `projectCard`, care are deja campul JSON `agentRequests` (numarul). Campul din `projectDetail` e mai putin adanc, deci `encoding/json` il alege pe el si numarul dispare din detaliu (in lista de proiecte ramane). E ce vrem: in detaliu UI-ul numara lista. In `getProject`, dupa `accessViews`: `agentReqs, err := s.agentRequestViews(p.ID, agents, tasks)` (cu `fail` la eroare) si `AgentRequests: agentReqs` in literalul `projectDetail`.
  - `api_agents.go`, in `apply`: inlocuieste verificarile de nume, model, tools si blocul `maxLen` cu o singura verificare dupa ce campurile sunt copiate:
    ```go
    	if err := team.CheckAgentFields(a); err != nil {
    		return badRequest(err.Error())
    	}
    ```
    Pastreaza `strings.TrimSpace` pe nume si restul copierii exact ca acum. Scoate importurile ramase nefolosite (`models`, `prompt`, `slices` daca nu mai sunt folosite in fisier).

- [ ] **Step 5:** `go test ./internal/web/` -> PASS (inclusiv testele existente de agenti: mesajele de eroare sunt aceleasi); `go vet ./...`.
- [ ] **Step 6:** commit: `web: API pentru cererile add_agent`

---

### Task 6: UI: cererile in "Needs you", "Added by", CLAUDE.md, verificarea finala

**Files:**
- Create: `internal/web/static/js/agent-requests.js`
- Modify: `internal/web/static/js/workspace.js` (~84-96 banda "Needs you", `renderNeeds` ~243-265)
- Modify: `internal/web/static/js/projects.js:89` (numarul "needs you")
- Modify: `internal/web/static/js/agent.js` (linia "Added by")
- Modify: `internal/web/static/js/canvas.js:216` (tooltip pe nod)
- Modify: `internal/web/static/css/app.css` (doar daca e nevoie, vezi Step 4)
- Modify: `CLAUDE.md` (sectiunea Stare)

**Interfaces:**
- Consumes: `project.agentRequests` (lista din Task 5), `agent.createdBy`, rutele approve/dismiss.

- [ ] **Step 1: `agent-requests.js`** (in stilul lui `access.js`):

```js
import { api } from "./api.js";
import { el, avatar, kindOf, toast } from "./ui.js";

// agentRequestBox: un lider cere un agent nou ("Lead wants to add Quinn"), cu
// rolul, modelul, tool-urile, motivul si instructiunile (restranse). Butoane:
// Approve, Dismiss. onDone se cheama dupa o actiune reusita; la eroare
// (ex. numele s-a ocupat intre timp) apare un toast si cardul ramane.
export function agentRequestBox(r, { lead, onDone }) {
  const s = r.spec || {};
  const buttons = [];
  const act = (label, cls, path) => {
    const b = el("button", { type: "button", class: cls, onclick: async () => {
      for (const x of buttons) x.disabled = true;
      try { await api(path, { method: "POST" }); await onDone?.(); }
      catch (err) { toast(err.message); for (const x of buttons) x.disabled = false; }
    } }, label);
    buttons.push(b);
    return b;
  };
  const tools = (s.tools || []).length ? s.tools.join(", ") : "read only";
  const more = el("details", { class: "agent-request-more" },
    el("summary", {}, "Show instructions"),
    el("p", { class: "question-text" }, s.instructions || ""));
  return el("div", { class: "question agent-request" },
    el("div", { class: "question-head" }, lead ? avatar(lead.name, kindOf(lead), 20) : null,
      el("strong", {}, `${r.leadName || "A lead"} wants to add ${s.name}`),
      r.ref ? ` · ${r.ref}` : null),
    el("p", { class: "question-text" }, `${s.role} · ${s.model} · tools: ${tools}`),
    el("p", { class: "question-text" }, r.reason),
    more,
    el("div", { class: "actions" },
      act("Approve", "btn small primary", `/api/agent-requests/${r.id}/approve`),
      act("Dismiss", "btn small", `/api/agent-requests/${r.id}/dismiss`)));
}
```

- [ ] **Step 2: `workspace.js`.**
  - Import: `import { agentRequestBox } from "./agent-requests.js";`
  - Langa `accessList`: `const agentReqList = el("div", { class: "needs-agent-requests" });` si adauga-l in `needsBox` dupa `accessList`.
  - Langa `let accessKey = null;`: `let agentReqKey = null; // id-urile cererilor de agent desenate ultima data`.
  - Langa `accessNode`:
    ```js
      // agentReqNode: caseta unei cereri add_agent (un lider vrea un agent nou)
      function agentReqNode(r) {
        return agentRequestBox(r, {
          lead: state.project.agents.find((a) => a.id === r.leadId),
          onDone: reload,
        });
      }
    ```
  - In `renderNeeds`: `const agentReqs = state.project.agentRequests || [];`, adauga `agentReqs.length` la `total`, iar la final:
    ```js
        const rKey = agentReqs.map((r) => r.id).join(",");
        if (rKey !== agentReqKey) {
          agentReqKey = rKey;
          agentReqList.replaceChildren(...agentReqs.map(agentReqNode));
        }
    ```
  - SSE: `scheduleReload()` se cheama la orice eveniment (inclusiv `agent_request`); verifica in handler-ul de evenimente (cauta `scheduleReload` in `workspace.js`) ca nu exista o lista de tipuri permise. Daca exista, adauga `"agent_request"`.
- [ ] **Step 3: `projects.js:89`:** adauga `+ (p.agentRequests || 0)` la `asks` si actualizeaza comentariul (`..., cereri de agenti noi`).
- [ ] **Step 4: "Added by".**
  - `agent.js`, in `agentEditor`: `const addedBy = a.createdBy ? project.agents.find((x) => x.id === a.createdBy) : null;` si, primul element din formular (cauta unde se construieste continutul returnat), `addedBy ? el("p", { class: "hint" }, \`Added by ${addedBy.name}\`) : null`.
  - `canvas.js`, in `agentNode`, primul copil al lui `g` (inainte de `svg("g", { "clip-path": ... })`): `a.createdBy ? svg("title", {}, "Added by " + (project.agents.find((x) => x.id === a.createdBy)?.name || "a lead")) : null`. Verifica in `canvas.js` ca helper-ul `svg` ignora copiii `null` (cum face `el`); `(project.questions || []).some(...) ? ... : null` din acelasi nod arata ca da.
  - `app.css`: `.needs-agent-requests` primeste aceeasi regula ca `.needs-access` (`display: contents;`, linia 355: adauga selectorul in lista). Pentru `details.agent-request-more summary` adauga `cursor: pointer; color: var(--soft);` cu comentariul `/* cererile add_agent */` doar daca arata nealiniat in screenshot.
- [ ] **Step 5: Verificare JS:** copiaza `agent-requests.js`, `workspace.js`, `projects.js`, `agent.js`, `canvas.js` ca `.mjs` intr-un folder temporar si ruleaza `node --check` pe fiecare.
- [ ] **Step 6: Verificare in browser** (memoria: screenshot-uri cu Chrome headless prin CDP, cu un HOME separat si o copie a bazei de date, port 7790, `--user-data-dir` nou ca sa nu prinda JS vechi din cache; opreste serverul de test dupa PID, nu dupa cale). Creeaza in copia bazei o cerere deschisa (de exemplu cu `sqlite3` un INSERT in `agent_requests` pentru radacina unui proiect) si un agent cu `created_by`. Screenshot-uri: banda "Needs you" cu cardul cererii (si cu "Show instructions" deschis), canvas-ul (tooltip-ul nu apare in screenshot: verifica prin CDP `document.querySelector('[data-agent="<id>"] title').textContent`), panoul agentului cu "Added by". Apasa Approve prin CDP si verifica ca agentul apare pe canvas si cardul dispare.
- [ ] **Step 7: `CLAUDE.md`** (`agentdash/CLAUDE.md`), sectiunea Stare, dupa paragraful Flow:

```
Gata (liderul adauga agenti, spec in docs/superpowers/specs/2026-10-06-foreman-lead-adds-agents-design.md):
tool-urile MCP add_agent / remove_agent pentru lideri si radacini (si fara echipa); agentul intra sub lider, cu
tool-uri din ale liderului, max 6 adaugati; direct cu Full autonomy sau la cererea utilizatorului din chat, altfel
aprobare in "Needs you" (agent_requests); remove = detasare.
```

- [ ] **Step 8: Verificarea completa:** `go vet ./...`, `go build -race ./cmd/foreman`, `go test ./...`, `GOOS=darwin GOARCH=arm64 go build ./cmd/foreman`, `GOOS=windows go build ./cmd/foreman` (sterge binarele produse de cross-compile daca nu erau acolo inainte: `foreman` si `foreman.exe` exista deja in repo si sunt ignorate sau nu, verifica `git status`).
- [ ] **Step 9:** commit: `ui: cererile add_agent in Needs you, "Added by" pe agent`
