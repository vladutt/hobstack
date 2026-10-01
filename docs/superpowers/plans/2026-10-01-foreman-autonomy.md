# Full autonomy si cererile de acces: plan de implementare

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Comutator per proiect "Full autonomy" (fara intrebari, radacina decide in locul utilizatorului) si cereri de acces din `permission_denials`, afisate in "Needs you" in toate modurile.

**Architecture:** Store: migrarea 010 (`projects.autonomy`, `tasks.escalated`, `access_requests`). Runner: tracker-ul citeste refuzurile. Team: prompt/ask_user, decizii la radacina, inregistrarea refuzurilor. Web: campuri noi in API, doua rute. UI: bifa, pastila, randuri in banda.

**Tech Stack:** Go, SQLite, vanilla JS.

**Spec:** `docs/superpowers/specs/2026-10-01-foreman-autonomy-design.md`

## Global Constraints

- vet / race build / test trec; cross-compile darwin/arm64 + windows.
- JS verificat ca modul (`cp f /tmp/x.mjs && node --check /tmp/x.mjs`); `cond ? x : null` pentru valori folosite cu `?.`; `replaceChildren`/`append` native nu aplatizeaza array-uri; regula `[hidden]` pentru containere cu `display`.
- Texte vizibile si prompturi in engleza, exact ca in spec; comentarii in romana fara diacritice.
- SQLite cu o singura conexiune: in tranzactie doar `tx`; nicio interogare cat timp un `*sql.Rows` e deschis.
- Team: functiile apelate cu `t.work` tinut nu iau lock-uri; `Kick` dupa unlock (ca in codul existent).
- Modul oprit (`Autonomy = false`) se comporta exact ca azi: testele existente trec neschimbate.

## Review Focus

1. **Modul oprit neschimbat:** niciun drum din team nu schimba comportamentul fara `Autonomy`. Testele existente + un test explicit (Task 3).
2. **Fara bucla:** un task autonom nu poate oscila la nesfarsit intre blocat si send_back (escalated -> Done automat). Test (Task 3).
3. **Comasarea refuzurilor:** acelasi (agent, tool) repetat in run-uri succesive = un rand cu `count` crescut; dupa Dismiss, rand nou. Test (Task 1, Task 2).
4. **Banda nu pierde ce scrie utilizatorul** intr-o intrebare cand vine un eveniment `access` (cheia de reconstruire). Verificat in browser (Task 5).
5. **Enable Shell** chiar adauga "shell" in tool-urile agentului fara sa piarda celelalte campuri ale agentului. Test web (Task 4).

---

### Task 1: Store

**Files:** Create `internal/store/migrations/010_autonomy.sql`, `internal/store/access.go`, `internal/store/access_test.go`; Modify `internal/store/projects.go`, `internal/store/tasks.go`, testul de `user_version`.

**Produces:**
- `store.Project.Autonomy bool` (JSON `autonomy`), citit/scris la create/update (ca `MaxParallel`).
- `store.Task.Escalated bool` (JSON `escalated`), salvat de `SaveTask`.
- `store.AccessRequest{ID, ProjectID, AgentID int64; TaskID *int64; Tool, Detail string; Count int; CreatedAt, UpdatedAt time.Time}` (JSON `id, projectId, agentId, taskId, tool, detail, count, createdAt, updatedAt`).
- `RecordAccessDenial(projectID, agentID int64, taskID *int64, tool, detail string) error`
- `ListOpenAccessRequests(projectID int64) ([]AccessRequest, error)` (non-nil, ordonat dupa id)
- `GetAccessRequest(id int64) (AccessRequest, error)` (`sql.ErrNoRows` daca lipseste)
- `DismissAccessRequest(id int64) error`
- `CountOpenAccessRequests(projectID int64) (int, error)`

- [ ] Migrarea: `ALTER TABLE projects ADD COLUMN autonomy INTEGER NOT NULL DEFAULT 0;`, `ALTER TABLE tasks ADD COLUMN escalated INTEGER NOT NULL DEFAULT 0;` si tabelul + indexul `access_requests` exact ca in spec; comentariu sus.
- [ ] `user_version` 9 -> 10 in testul existent.
- [ ] Project/Task: campurile noi in Scan/Insert/Update (toate locurile care listeaza coloanele).
- [ ] `RecordAccessDenial`: intr-o tranzactie, cauta randul deschis (agent, tool); daca exista: `count = count + 1, detail = ?, updated_at = ?` si `task_id = ?` doar daca taskID != nil; altfel INSERT cu `created_at = updated_at = now` (RFC3339Nano).
- [ ] Teste: autonomy salvat/citit; escalated salvat; insert; comasare (count 2, detail ultimul); rand nou dupa dismiss; list/count doar deschise; cascade la stergerea agentului; `task_id` devine NULL la stergerea task-ului.
- [ ] Commit: `store: autonomy, escalated si cererile de acces`

### Task 2: Refuzurile (runner + team)

**Files:** Modify `internal/runner/tracker.go`, `internal/runner/runner.go`, `internal/team/run.go` (in `recordOutcome`), eventual `internal/team/events.go`; Tests `internal/runner/tracker_test.go` (sau existent), `internal/team/access_test.go`.

**Consumes:** `RecordAccessDenial` (Task 1), `t.taskEvent`.

**Produces:** `runner.Denial{Tool, Detail string}`; `runner.Outcome.Denials []Denial`; evenimentul SSE `access` (payload `{agentId}`).

- [ ] Tracker: campul `PermissionDenials []struct{ ToolName string `json:"tool_name"`; ToolInput map[string]any `json:"tool_input"` } `json:"permission_denials"`` in evenimentul `result`; `Detail` = `command` (Bash), `file_path` (Edit/Write/MultiEdit/Read/NotebookEdit), `url` (WebFetch), `query` (WebSearch), altfel JSON compact al `tool_input`; taiat la 120 de runes cu "...". `outcome()` copiaza refuzurile in `Outcome.Denials` indiferent de status.
- [ ] Fake-ul de claude din testele team (daca exista un helper `finish`) poate primi refuzuri: adauga minim ce trebuie ca testul sa trimita un `Outcome` cu `Denials`.
- [ ] `recordOutcome`: pentru fiecare tool distinct din `o.Denials` (primul detail), sarind `TaskCreate`, `TodoWrite`, `Agent`, `Task`: `RecordAccessDenial(project, agent, taskID-ul run-ului sau nil, tool, detail)`; daca run-ul era pe un task: `t.taskEvent(task, "denied", agent.Name, fmt.Sprintf("%s was denied %s: %s", agent.Name, tool, detail))` (fara ": detail" daca e gol). Erorile se logheaza. Daca s-a inregistrat ceva: `t.emit(projectID, "access", map[string]any{"agentId": agentID})`. Ruleaza inaintea restului logicii de outcome si pentru orice status (done/failed/stopped).
- [ ] Teste: tracker (Bash -> command, Edit -> file_path, alt tool -> JSON, lipsa campului -> nil); team (refuz Bash pe un run de task -> cerere + eveniment `denied`; doua refuzuri Bash in acelasi run -> count 1; TodoWrite ignorat; run urmator cu acelasi refuz -> count 2).
- [ ] Verificare completa; commit: `team: cererile de acces din permission_denials`

### Task 3: Modul autonom (prompt, ask_user, decizii)

**Files:** Modify `internal/prompt/prompt.go` (+ test), `internal/team/tools.go`, `internal/team/run.go`, `internal/team/reconcile.go`, `internal/team/stages.go`, `internal/team/stuck.go` (daca foloseste `decider`/`decisionOwner`); Test `internal/team/autonomy_test.go`.

**Consumes:** `Project.Autonomy`, `Task.Escalated` (Task 1).

**Produces:** `rootOf(agentID int64, agents []store.Agent) (store.Agent, bool)`; `Team.UserDecides(task, agents)` intoarce false pentru task-urile pe care le decide radacina in modul autonom (semnatura poate primi proiectul; actualizeaza apelantul din `internal/web/api_projects.go`).

- [ ] **Prompt:** cu `p.Autonomy`, `howAsk`/`howAskWait` si linia cu "use mcp__team__ask_user" se inlocuiesc cu fraza din spec; promptul autonom nu contine "ask_user". Teste: autonom vs normal (normal neschimbat).
- [ ] **ask_user:** in `AskUser`, daca proiectul apelantului are `Autonomy`, intoarce textul din spec, fara intrebare salvata si fara alta schimbare.
- [ ] **Decident:** `decider`, `canDecide`, `decisionOwner` primesc un flag `autonomous bool` (citit o data per apel din proiect). Cu flag: task `ByUser` (sau fara decident) -> radacina asignatului (`rootOf`), daca radacina != asignat. Toate apelurile existente trec flag-ul proiectului; cu `false` comportamentul e identic.
- [ ] **Done automat:** in `toReview` (si drumul din `recordOutcome` care blocheaza pentru lipsa decidentului), cu autonomie, daca nu exista decident nici dupa regula radacinii (asignatul e radacina): `Status = Done`, `ReviewStage = None`, `StageID = nil`, eveniment `accepted`/`Foreman`/`Accepted automatically (full autonomy)`, apoi `afterDone` si `Kick` dupa unlock (ca in `AcceptTask`).
- [ ] **Blocat:** in `rejectTask` (si `sendToStage` daca blocheaza separat), cu autonomie: daca `task.Escalated` -> Done automat cu textul `Accepted automatically after repeated send-backs (full autonomy)` si nota livrata radacinii; altfel ramane Blocked, iar nota catre radacina e cea din spec (in loc de "The user decides"). Celelalte locuri care pun Blocked pentru lipsa decidentului (`run.go`, `reconcile.go`): cu autonomie aplica Done automat.
- [ ] **send_back pe Blocked:** in `SendBack`, permis pe `StatusBlocked` doar cu autonomie si doar daca apelantul e radacina asignatului: `SendBacks = MaxSendBacks - 1`, `Escalated = true`, apoi drumul normal (`rejectTask` / `sendToStage`) cu feedback-ul. Fara autonomie, eroarea de azi.
- [ ] **Teste** (`autonomy_test.go`): ask_user autonom -> raspuns, nicio intrebare; task ByUser raportat -> radacina primeste nota si poate accept_task / send_back, iar `UserDecides` e false; utilizatorul poate tot accepta din board; asignatul radacina -> Done automat cu eveniment; blocat dupa 3 send-back-uri -> nota la radacina; radacina send_back pe Blocked -> Queued, escalated; al doilea blocaj -> Done automat; acelasi scenariu fara autonomie -> comportamentul de azi; un nerad (sef intermediar) nu poate send_back pe Blocked.
- [ ] Verificare completa (+ `go test -race ./internal/team/`); commit: `team: modul full autonomy`

### Task 4: Web: API + UI

**Files:** Modify `internal/web/api_projects.go`, `internal/web/server.go` (rute), fisier nou `internal/web/api_access.go` (+ test), `internal/web/static/js/projects.js`, `internal/web/static/js/workspace.js`, fisier nou `internal/web/static/js/access.js`, `internal/web/static/app.css`.

**Consumes:** functiile store din Task 1; `Team.UserDecides` actualizat (Task 3).

- [ ] **API proiect:** `autonomy` acceptat la create/update (`updateProject`) si intors (vine din `store.Project`). `projectDetail.Access []accessView` (`id, agentId, agentName, taskId, ref, tool, detail, count, updatedAt, canEnableShell`); `projectCard.AccessRequests int` (`accessRequests`).
- [ ] **Rute:** `POST /api/access/{id}/dismiss` -> 204 (404 lipsa); `POST /api/access/{id}/enable-shell` -> 409 daca `tool != "Bash"`, altfel adauga "shell" in `agent.Tools` (daca lipseste) si salveaza agentul pe acelasi drum ca `updateAgent` (pastrand restul campurilor), apoi dismiss, emit `agent` + `access`, 204.
- [ ] **Teste web:** `access`/`accessRequests` in raspunsuri; dismiss; enable-shell (tool-urile agentului contin "shell" si pe cele vechi; randul nu mai e deschis); 409 pe alt tool; 404.
- [ ] **Project instructions** (`projects.js`): bifa "Full autonomy" cu textul din spec, trimisa ca `autonomy`.
- [ ] **Pastila** "Autonomous" langa numele proiectului (workspace) si pe cardul din Projects; badge-ul "N need(s) you" aduna `accessRequests`.
- [ ] **Banda "Needs you":** `access.js` exporta `accessBox(r, { agent, projectId, onDone })` cu textul si butoanele din spec (Enable Shell for <Name> doar cu `canEnableShell`, Open <Name>, Dismiss). Workspace-ul adauga randurile in banda; titlul numara intrebari + blocate + acces; cheia de reconstruire include `id:count` pentru acces. Evenimentul SSE `access` declanseaza reload-ul (ca `task`).
- [ ] Verificare: bucla `.mjs`, `go test ./internal/web/`, `go vet ./...`.
- [ ] Commit: `ui: full autonomy si cererile de acces in Needs you`

### Task 5: Verificare in browser si CLAUDE.md

- [ ] Instanta de test (7789, HOME temporar, claude fals care pentru un prompt anume intoarce in `result` un `permission_denials` cu Bash).
- [ ] CDP: bifa Full autonomy salvata si pastila vizibila; run cu refuz -> rand in Needs you "X was denied Bash" + comanda; al doilea run -> (2×), un singur rand; Enable Shell -> randul dispare, agentul are Shell in editor; Dismiss; badge pe card; textul tastat intr-o intrebare ramane la un eveniment `access`; evenimentul `denied` in modala task-ului. Screenshot-uri.
- [ ] CLAUDE.md, "Stare": `Gata (full autonomy, spec in docs/superpowers/specs/2026-10-01-foreman-autonomy-design.md): comutator per proiect (agentii nu intreaba, radacina decide in locul utilizatorului, fara bucla la blocaj); cererile de acces din permission_denials apar in "Needs you" in toate modurile, cu Enable Shell / Dismiss.` + commit.
