# Foreman: liderul adauga agenti in echipa

Data: 2026-10-06
Status: design aprobat in chat.

## Scop

Un lider (agent cu echipa directa) sau o radacina (chiar fara echipa) isi poate mari echipa singur:

- cand ii lipseste un specialist pentru o parte din task (agent permanent);
- cand vrea mai multe "maini" pentru sub-task-uri independente, pe care le scoate dupa (agenti temporari);
- cand utilizatorul ii cere in chat ("add a QA agent").

Tot ce e regula (unde intra agentul, ce permisiuni primeste, cati, cine aproba) o impune aplicatia, nu prompt-ul.

In afara scopului: liderul nu editeaza agenti existenti, nu creeaza sub-lideri (agentul nou intra doar sub el), nu
seteaza reviewer, nu adauga agenti in etape, nu sterge definitiv agenti.

## Tool-uri MCP

Pentru lideri si pentru radacini, chiar fara echipa. `mcp.Caller` primeste `CanAddAgents bool`, calculat in
`team/run.go` ca `isLead || roles.IsRoot` (radacina = fara sef si nelegata). `toolsFor` (`internal/mcp/tools.go`):

- `CanAddAgents` -> `add_agent`, `remove_agent`;
- `CanAddAgents && !IsLead` (radacina fara echipa) -> in plus `create_task` si `list_tasks`, ca sa-i poata da de lucru
  agentului nou in acelasi run (lista de tool-uri se fixeaza la pornirea run-ului). `create_task` verifica deja la
  apel ca asignatul e in echipa directa, din baza de date, deci agentul abia creat e acceptat.

`IsLead` ramane neschimbat (decizii, `accept_task`, `send_back`); un lider care si-a scos toata echipa ramane radacina
sau sub-lider: daca e radacina pastreaza `add_agent`, daca are sef il pierde (devine worker obisnuit).

Cand se spune mai jos "liderul", inseamna agentul care cheama tool-ul.

### `add_agent`

| camp              | obligatoriu | reguli                                                                 |
|-------------------|-------------|------------------------------------------------------------------------|
| `name`            | da          | trim, 1..40 caractere, unic in proiect (NOCASE)                        |
| `role`            | da          | max 80                                                                 |
| `instructions`    | da          | max 10000                                                              |
| `background`      | nu          | max 10000                                                              |
| `done_definition` | nu          | max 4000                                                               |
| `model`           | nu          | `models.Valid`; implicit sonnet                                        |
| `tools`           | nu          | din `prompt.ValidTools`, fara duplicate, submultime a tool-urilor liderului; implicit `["write"]` (daca liderul nu are `write`, implicit `[]`) |
| `reason`          | da          | max 300; o fraza: de ce e nevoie de agent (apare la aprobare)           |

Limitele de lungime sunt cele din `web/api_agents.go` (`apply`); validarea se muta intr-o functie comuna (vezi Team),
ca sa nu existe doua copii.

Agentul nou:

- `ParentID` = liderul care cheama; `FolderID` = folderul liderului; fara reviewer; `Detached = false`;
- `CreatedBy` = liderul;
- pozitia pe canvas: sub lider (`PosX` = al liderului + 220 * k, `PosY` = al liderului + 160, unde k = cati agenti
  are deja liderul); daca liderul nu are pozitie, `nil` (canvas-ul il aseaza ca azi).

**Creare directa sau cu aprobare.** Directa daca:

- proiectul are Full autonomy pornit (`t.autonomyOf`), sau
- run-ul liderului a pornit dintr-un mesaj al utilizatorului (`t.requestOf(c.AgentID, c.RunID)` gaseste mesajul).

Altfel se creeaza o cerere in "Needs you" (vezi Store).

Raspunsuri catre lider:

- direct: `Agent "X" added to your team. Give it work with create_task.`
- cerere: `Request sent to the user. You will get a message when it is approved or dismissed; continue with your current work meanwhile.`

**Limita:** agentii activi creati de lider (`created_by` = lider si `detached = 0`) + cererile lui deschise <= 6.
Peste: `you already have 6 agents you added (or requested); remove one with remove_agent first`.

Alte refuzuri (mesaje in engleza, intoarse ca eroare de tool):

- nume ocupat (agent existent sau cerere deschisa a oricui din proiect cu acelasi nume): `an agent named "X" already exists`;
- tool peste ale liderului: `you can only give tools you have yourself: shell` (lista celor in plus);
- model necunoscut: `unknown model "X"`.

### `remove_agent`

Camp: `name`. Conditii:

- agentul exista in proiect si are `created_by` = liderul care cheama; altfel `you can only remove agents you added`;
- nu e deja detasat; altfel `"X" is not on the team`;
- nu are task-uri asignate care nu sunt Done; altfel `"X" still has open tasks: T-12, T-15`;
- nu are echipa proprie si nu e reviewer pentru nimeni (utilizatorul l-ar fi putut lega asa din canvas); altfel
  `"X" has its own team or reviews someone; ask the user to change it`.

Efect, fara aprobare (nu se pierde nimic): `ParentID = nil`, `Detached = true`; ramane pe canvas, nelegat, cu
istoricul. Raspuns: `"X" removed from your team (kept on the canvas, detached).`

## Store

Migrarea 015:

```sql
-- agentii adaugati de un lider si cererile de aprobare pentru ei
ALTER TABLE agents ADD COLUMN created_by INTEGER REFERENCES agents(id) ON DELETE SET NULL;

CREATE TABLE agent_requests (
  id          INTEGER PRIMARY KEY,
  project_id  INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  lead_id     INTEGER NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
  task_id     INTEGER REFERENCES tasks(id) ON DELETE SET NULL,
  run_id      TEXT NOT NULL DEFAULT '',
  spec        TEXT NOT NULL,   -- JSON: name, role, instructions, background, doneDef, model, tools
  reason      TEXT NOT NULL,
  status      TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'approved', 'dismissed')),
  agent_id    INTEGER REFERENCES agents(id) ON DELETE SET NULL, -- agentul creat la aprobare
  created_at  TEXT NOT NULL,
  closed_at   TEXT
);
CREATE INDEX agent_requests_project ON agent_requests(project_id, status);
```

`created_by` NULL = creat de utilizator sau de wizard. `Agent` primeste `CreatedBy *int64` (si in coloanele citite).

`store.AgentRequest{ID, ProjectID, LeadID int64; TaskID *int64; RunID string; Spec NewAgentSpec; Reason, Status string;
AgentID *int64; CreatedAt time.Time; ClosedAt *time.Time}`.

Functii: `CreateAgentRequest(*AgentRequest) error`, `GetAgentRequest(id)`, `ListOpenAgentRequests(projectID)`
(dupa id, non-nil), `CountOpenAgentRequests(projectID)`, `CloseAgentRequest(id, status string, agentID *int64) error`
(doar din `open`; altfel `ErrConflict`), `CountAddedBy(leadID) (int, error)` (agenti activi + cereri deschise).

## Team

Fisier nou `internal/team/agents.go`:

- `ValidateNewAgent(spec, lead store.Agent) (NewAgentSpec, error)`: trim, lungimi, model, tools (si submultimea).
  `web/api_agents.go` foloseste aceleasi verificari de lungime/model/tools (fara regula submultimii).
- `AddAgent(c mcp.Caller, in mcp.AddAgentInput) (string, error)`: validare, limita, nume; apoi creare directa sau cerere.
- `RemoveAgent(c mcp.Caller, name string) (string, error)`.
- `ApproveAgentRequest(id)`, `DismissAgentRequest(id)`: din UI.

Toate sub `t.work`, ca `AskUser`. Dupa orice schimbare:

- `t.LinksChanged(projectID)` (re-calculeaza rolurile, Kick);
- evenimente SSE `agent` si `project`.

**Aprobare:** re-valideaza totul (intre timp numele s-a putut ocupa, limita s-a putut umple, liderul si-a putut pierde
un tool); la eroare cererea ramane deschisa si UI-ul arata eroarea (409). La succes: `CreateAgent` +
`CloseAgentRequest(approved, &id)` in aceeasi tranzactie.

**Mesajul catre lider** (ca la `closeQuestion`, prin `addUserMessage`, apoi Kick):

- `The user approved your request: "X" is now on your team. Give it work with create_task.`
- `The user dismissed your request to add "X". Continue with your current team.`

**Istoricul task-ului:** daca `c.TaskID != 0`, in `task_events`: `Requested agent "X"`, `Added agent "X"`,
`Removed agent "X"` (actor = liderul); la aprobare/respingere `Approved agent "X"` / `Dismissed agent "X"` (actor "You").

## Prompt

`internal/prompt`: pentru agentii cu `add_agent` (lideri si radacini), un paragraf nou in prompt-ul de sistem:

> You can grow your team with add_agent: only when no current member fits part of the work, or when you have
> independent sub-tasks to run in parallel. Check your team first. A new agent starts working when you give it a task
> with create_task. When an agent you added has no more work, remove it with remove_agent.

Prompt-ul liderilor si al radacinilor se schimba, deci hash-ul lor: la urmatorul run fiecare porneste o sesiune Claude noua
(comportamentul existent la orice schimbare de prompt).

## Web

- `GET /api/projects/{id}`: `agentRequests []AgentRequestView` (cerere + numele liderului); cardul de proiect primeste
  `agentRequests int` (numarat in "Needs you", ca `accessRequests`).
- `POST /api/agent-requests/{id}/approve`, `POST /api/agent-requests/{id}/dismiss`: 204; 404 daca nu exista; 409 daca
  nu mai e deschisa sau re-validarea pica (mesajul erorii in corp).
- `agentView` primeste `createdBy *int64`.

## UI

- **"Needs you"** (proiect si agentul lider, ca intrebarile): card `<Lead> wants to add <Name>` cu rolul, modelul,
  tool-urile, motivul, instructiunile restranse (Show more) si **Approve** / **Dismiss**. Eroarea de la approve apare
  ca toast, cardul ramane.
- **Canvas / panoul agentului:** eticheta mica `Added by <Lead>` pe agentii cu `createdBy`.
- Fara alte schimbari: agentii adaugati se editeaza, muta, leaga si sterg ca oricare altii.

## Teste

- store: migrarea 015 (user_version 15), `created_by`, CRUD `agent_requests`, `CloseAgentRequest` doar din open,
  `CountAddedBy`.
- team:
  - creare directa cu autonomy; creare directa din run de chat; cerere fara niciuna;
  - approve (agent creat sub lider, in folderul lui, `created_by`, mesaj catre lider) si re-validare care pica;
  - dismiss (mesaj catre lider);
  - refuzuri: tool peste ale liderului, limita de 6 (inclusiv cereri deschise), nume duplicat (agent si cerere);
  - remove_agent: doar agentii proprii, refuz cu task-uri deschise, refuz cu echipa/reviewer, detasare;
  - evenimentele din `task_events`.
- mcp: `add_agent` / `remove_agent` la lideri si radacini, nu la workeri cu sef; radacina fara echipa primeste si
  `create_task` / `list_tasks`, dar nu `accept_task` / `send_back`.
- team: o radacina fara echipa adauga un agent (direct) si ii creeaza un task in acelasi run (acelasi `Caller`).
- web: approve / dismiss (204, 404, 409).
- UI: `node --check` pe copii `.mjs`; screenshot-uri cu Chrome headless prin CDP pentru cardul din "Needs you" si
  eticheta de pe canvas.
- `go vet ./...`, `go build -race ./cmd/foreman`, `go test ./...`, cross-compile darwin/arm64 si windows.

## CLAUDE.md

In sectiunea Stare: `Gata (liderul adauga agenti, spec in docs/superpowers/specs/2026-10-06-foreman-lead-adds-agents-design.md):
tool-urile MCP add_agent / remove_agent pentru lideri si radacini (si fara echipa); agentul intra sub lider, cu tool-uri din ale liderului, max 6
adaugati; direct cu Full autonomy sau la cererea utilizatorului din chat, altfel aprobare in "Needs you"; remove = detasare.`
