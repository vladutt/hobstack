# Detectorul de task-uri blocate: plan de implementare

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Task-urile care nu se mai misca de 5 minute (decizie neluata de un agent idle, sau In progress fara run) sunt impinse automat o data; daca tot stau, apar in "Needs you" cu Nudge again / Open <agent>, pe card si pe board.

**Architecture:** `tasks.nudged_at` in store (curatat la orice SaveTask). In `internal/team`, `stuck.go`: clasificarea, `CheckStuck(now)` (impinge o data), `StuckTasks(projectID, now)` (cele vizibile), `Nudge(taskID, now)` (manual); `Team.Run` primeste un ticker de 1 minut. Web: `stuck` pe proiect, `stuckTasks` pe card, `POST /api/tasks/{id}/nudge`. UI: randuri in banda "Needs you", badge, eticheta "stuck".

**Tech Stack:** Go, SQLite, vanilla JS.

**Spec:** `docs/superpowers/specs/2026-10-01-foreman-stuck-tasks-design.md`

## Global Constraints

- `go vet ./...`, `go build -race ./...`, `go test ./...` trec dupa fiecare task; cross-compile darwin/arm64 si windows.
- JS: verificare ca modul (`cp f /tmp/x.mjs && node --check /tmp/x.mjs`); `cond ? x : null` pentru valori folosite cu `?.`; `replaceChildren`/`append` native nu aplatizeaza array-uri; regula `[hidden]` pentru containere cu `display`.
- Engleza in UI si in mesajele catre agenti; comentarii in romana fara diacritice.
- Lock-uri in team: `t.work` serializeaza schimbarile de stare; `t.mu` doar pentru `active`/`tokens`/`pending`; `Kick` dupa eliberarea lui `t.work`; nicio functie care ia `t.work` apelata cu el tinut.
- Pragul: 5 minute (constanta `stuckAfter = 5 * time.Minute`).
- Proiectele pe pauza nu se verifica si nu se impinge nimic in ele (nici manual: 409).

## Review Focus

1. **O singura impingere automata:** dupa impingere, verificarile urmatoare nu mai trimit nimic (doar il fac vizibil dupa inca 5 min); `SaveTask` curata `nudged_at`, iar la orphan requeue-ul se face cu SaveTask si abia APOI se seteaza `nudged_at` (altfel bucla). Test in Task 2.
2. **Nu se impinge un agent care lucreaza sau are deja ceva de citit** (run activ, mesaje nelivrate, intrebare deschisa). Test in Task 2.
3. **Task-urile pe care le decide utilizatorul** (by_user, blocked) si **sub-liderii care asteapta sub-task-uri** nu sunt blocate. Test in Task 2.
4. **O singura nota per agent** cand are mai multe task-uri blocate in aceeasi verificare. Test in Task 2.
5. **Banda "Needs you" nu pierde raspunsul tastat** cand apar/dispar task-uri blocate: cheia de reconstruire include id-urile ambelor tipuri. Verificat in Task 5.

---

### Task 1: Store: `nudged_at`

**Files:** Create `internal/store/migrations/008_nudged.sql`; Modify `internal/store/tasks.go`; Test `internal/store/tasks_test.go` (sau `stuck_test.go` nou).

**Interfaces — Produces:** `Task.NudgedAt *time.Time` (JSON `nudgedAt`); `func (s *Store) SetTaskNudged(id int64, at time.Time) error`; `SaveTask` pune `nudged_at = NULL`.

- [ ] **Step 1: Test**

```go
func TestTaskNudgedAt(t *testing.T) {
	s := newStore(t)
	p, lead := newProject(t, s)
	task := Task{ProjectID: p.ID, Title: "A", AssigneeID: lead.ID}
	s.CreateTask(&task)
	at := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	if err := s.SetTaskNudged(task.ID, at); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetTask(task.ID)
	if got.NudgedAt == nil || !got.NudgedAt.Equal(at) {
		t.Fatalf("%v", got.NudgedAt)
	}
	got.Status = StatusReview
	s.SaveTask(&got)
	if again, _ := s.GetTask(task.ID); again.NudgedAt != nil {
		t.Fatal("SaveTask trebuia sa curete nudged_at")
	}
}
```

- [ ] **Step 2: RED**, **Step 3:** migrarea `ALTER TABLE tasks ADD COLUMN nudged_at TEXT;` (comentariu: momentul ultimei impingeri a detectorului; NULL = neimpins de la ultima schimbare). Adauga `nudged_at` la `taskCols` si la scan (NullString -> `*time.Time` cu `parseTime`); in `SaveTask` adauga `nudged_at = NULL` in `SET`; `SetTaskNudged`: `UPDATE tasks SET nudged_at = ? WHERE id = ?` cu `at.UTC().Format(time.RFC3339Nano)` + `mustAffect`. Actualizeaza asertia de `user_version` (7 -> 8) daca exista.

- [ ] **Step 4: GREEN**, **Step 5: Commit** — `git commit -m "store: nudged_at pe task"`

---

### Task 2: Team: detectarea si impingerea

**Files:** Create `internal/team/stuck.go`, `internal/team/stuck_test.go`; Modify `internal/team/team.go` (camp `now`), `internal/team/reconcile.go` (`Run` cu ticker).

**Interfaces:**
- Consumes: `Task.NudgedAt`, `SetTaskNudged`, `HasUndelivered`, `ListOpenQuestions`, `HasOpenQuestionForTask`, `decisionOwner`, `allSubtasksDone` (sau echivalentul existent), `isActive`, `note`, `emit`, `Kick`.
- Produces:
  - `const stuckAfter = 5 * time.Minute`
  - `type StuckTask struct { TaskID int64 \`json:"taskId"\`; Ref, Title, Kind string (json ref,title,kind); AgentID int64 \`json:"agentId"\`; AgentName string \`json:"agentName"\`; Since time.Time \`json:"since"\` }`
  - `func (t *Team) CheckStuck(now time.Time) error` (toate proiectele nepuse pe pauza)
  - `func (t *Team) StuckTasks(projectID int64, now time.Time) ([]StuckTask, error)` (doar cele vizibile; mereu non-nil)
  - `func (t *Team) Nudge(taskID int64, now time.Time) error` (ErrNotStuck = 409)
  - `var ErrNotStuck = errors.New("the task is not stuck: nothing to nudge")`
  - `Team.now func() time.Time` (implicit `time.Now`), folosit de ticker si de web.

- [ ] **Step 1: Teste** (`internal/team/stuck_test.go`). Folosesc fixture-ul `setup(t)` (lead, Rook, Vera) si fac task-urile "vechi" setand `updated_at` direct in DB nu e posibil prin API — in loc de asta, testele trimit un `now` in viitor fata de `UpdatedAt` (`task.UpdatedAt.Add(6*time.Minute)`). Pentru un proiect activ, testele apeleaza intai resume (sau seteaza `SetPaused(p.ID, false)`). Cazuri:

```go
// decizie: Rook a raportat T-1 (review, stage lead), Lead e idle -> o impingere, apoi vizibil
func TestStuckDecisionNudgedOnceThenVisible(t *testing.T) {
	f := setup(t)
	f.st.SetPaused(f.p.ID, false)
	task := f.task(t, "A", f.rook, store.StatusReview) // helper existent: pune statusul; verifica ReviewStage = lead
	// livreaza ce are Lead in coada, ca sa fie idle
	markDelivered(t, f, f.lead)
	t0 := task.UpdatedAt.Add(6 * time.Minute)
	if err := f.tm.CheckStuck(t0); err != nil {
		t.Fatal(err)
	}
	if ok, _ := f.st.HasUndelivered(f.lead.ID); !ok {
		t.Fatal("Lead trebuia trezit cu o nota")
	}
	got, _ := f.st.GetTask(task.ID)
	if got.NudgedAt == nil {
		t.Fatal("nudged_at nesetat")
	}
	// imediat dupa: nu e vizibil si nu se mai trimite nimic
	markDelivered(t, f, f.lead)
	f.tm.CheckStuck(t0.Add(time.Minute))
	if ok, _ := f.st.HasUndelivered(f.lead.ID); ok {
		t.Fatal("a doua impingere automata")
	}
	if st, _ := f.tm.StuckTasks(f.p.ID, t0.Add(time.Minute)); len(st) != 0 {
		t.Fatalf("prea devreme: %+v", st)
	}
	// dupa inca 5 min: vizibil, tot fara impingere
	st, _ := f.tm.StuckTasks(f.p.ID, t0.Add(6*time.Minute))
	if len(st) != 1 || st[0].Kind != "decision" || st[0].AgentID != f.lead.ID {
		t.Fatalf("%+v", st)
	}
}
```

Alte teste (scrie-le complet, in acelasi stil):
- `TestStuckSkips`: niciun efect cand (a) proiectul e pe pauza, (b) Lead are mesaje nelivrate, (c) Lead are o intrebare deschisa (`AskUser` cu Caller Lead), (d) task-ul e `ByUser` (creat cu `UserCreateTask`), (e) task-ul e `blocked`, (f) `now - UpdatedAt < 5 min`, (g) Lead are run activ (porneste un run cu `mustReconcile` dupa un `UserMessage` si nu-l termina).
- `TestStuckOrphanRequeued`: task In progress la Rook fara run activ (dupa `f.fs.finish` cu `RunStopped` + setare manuala `in_progress`, sau creeaza-l si seteaza statusul) -> `CheckStuck` il pune `queued` si `NudgedAt != nil` (requeue cu SaveTask INAINTE de SetTaskNudged); nota "Foreman requeued T-…".
- `TestStuckSubLeadWaitingForSubtasks`: task In progress al unui sub-lider cu un sub-task nefinalizat -> nu e orfan.
- `TestStuckOneNotePerAgent`: doua task-uri in review la Lead -> o singura nota nelivrata care contine ambele referinte.
- `TestNudgeManual`: `Nudge` pe un task in review la Lead idle (chiar daca < 5 min) -> nota + `nudged_at`; `Nudge` pe un task `done` -> `ErrNotStuck`; pe proiect pe pauza -> `ErrNotStuck`.

(Daca helper-ul `f.task` nu poate pune `review` cu `ReviewStage = lead`, seteaza campurile pe task si apeleaza `f.st.SaveTask`. `markDelivered(t, f, agent)`: listeaza `Undelivered(agent.ID)` si cheama `MarkDelivered`.)

- [ ] **Step 2: RED**

- [ ] **Step 3: `stuck.go`**

```go
package team

// Detectorul de task-uri blocate: un task care nu s-a mai schimbat de
// stuckAfter si pe care nu-l misca nimeni primeste o impingere automata (o data);
// daca tot sta, apare in "Needs you" (StuckTasks) cu Nudge again.

const stuckAfter = 5 * time.Minute

var ErrNotStuck = errors.New("the task is not stuck: nothing to nudge")

type StuckTask struct { ... } // ca in Interfaces

// stuckKind spune daca task-ul e blocat (fara prag de timp): "decision" cu agentul
// care trebuie sa decida, sau "orphan" cu asignatul. Se cheama cu t.work tinut.
func (t *Team) stuckKind(task store.Task, agents []store.Agent, tasks []store.Task, open []store.Question) (string, store.Agent, bool) {
	hasOpenQuestion := func(agentID int64) bool {
		for _, q := range open {
			if q.AgentID == agentID {
				return true
			}
		}
		return false
	}
	switch task.Status {
	case store.StatusReview:
		id := decisionOwner(task, agents)
		if id == 0 {
			return "", store.Agent{}, false // decide utilizatorul: are butoane pe board
		}
		a, ok := store.FindAgent(agents, id)
		if !ok || t.isActive(a.ID) || hasOpenQuestion(a.ID) {
			return "", store.Agent{}, false
		}
		if waiting, err := t.st.HasUndelivered(a.ID); err != nil || waiting {
			return "", store.Agent{}, false
		}
		return "decision", a, true
	case store.StatusInProgress:
		a, ok := store.FindAgent(agents, task.AssigneeID)
		if !ok || t.isActive(a.ID) || !allSubtasksDone(task.ID, tasks) {
			return "", store.Agent{}, false
		}
		for _, q := range open {
			if q.TaskID != nil && *q.TaskID == task.ID {
				return "", store.Agent{}, false
			}
		}
		return "orphan", a, true
	}
	return "", store.Agent{}, false
}
```

(Daca `allSubtasksDone` are alta semnatura, adapteaza; un task fara sub-task-uri trebuie sa conteze ca "done".)

`CheckStuck(now)`: lock `t.work`; pentru fiecare proiect din `ListProjects` care nu e `Paused`: incarca agentii, task-urile, `ListOpenQuestions`; pentru fiecare task cu `now.Sub(task.UpdatedAt) >= stuckAfter` si `NudgedAt == nil` si `stuckKind` ok -> grupeaza: `decision` pe agent (lista de task-uri), `orphan` direct. Pentru fiecare grup decision: `t.note(..., agent.ID, nudgeText(...), false)` + `t.note(..., agent.ID, "Foreman nudged <Name>: <refs> waiting for a decision.", true)` + `SetTaskNudged` pentru fiecare task. Pentru orphan: `task.Status = queued; SaveTask; SetTaskNudged(task.ID, now); emit task; note livrata`. Tine minte proiectele atinse; dupa `t.work.Unlock()`, `Kick` pentru fiecare.

`nudgeText(agent, tasks, agents)`: daca toate task-urile sunt la stage `reviewer` pentru acest agent: `"<refs> <is|are> waiting for your review: call review_approve or review_reject. If you cannot decide without the user, call ask_user."`; altfel `"<refs> <is|are> waiting for your decision: call accept_task or send_back. If you cannot decide without the user, call ask_user."` (refs = "T-2, T-3").

`StuckTasks(projectID, now)`: lock `t.work`; daca proiectul e pe pauza -> lista goala; altfel task-urile cu `NudgedAt != nil && now.Sub(*NudgedAt) >= stuckAfter` si `stuckKind` ok -> `StuckTask{..., Since: task.UpdatedAt}`.

`Nudge(taskID, now)`: lock `t.work`; task, proiect (pe pauza -> ErrNotStuck), `stuckKind` (fara prag) -> !ok -> ErrNotStuck; altfel aceeasi impingere ca in CheckStuck pentru acest singur task (decision: nota nelivrata + nota livrata; orphan: requeue) si `SetTaskNudged(now)`; unlock; Kick. Extrage impingerea intr-o functie comuna `nudgeLocked(...)` folosita de ambele (fara duplicare).

- [ ] **Step 4: Ticker** — in `Team` adauga `now func() time.Time` (in `New`: `now: time.Now`). In `Run`:

```go
	tick := time.NewTicker(time.Minute)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			if err := t.CheckStuck(t.now()); err != nil {
				log.Printf("stuck tasks: %v", err)
			}
			continue
		case <-t.wake:
		}
		// ... restul ca acum
```

Adauga si metoda `func (t *Team) Now() time.Time { return t.now() }` pentru web.

- [ ] **Step 5: GREEN** — `go test -race ./internal/team/ && go vet ./... && go test ./...` + cross-compile.

- [ ] **Step 6: Commit** — `git commit -m "team: detectorul de task-uri blocate (impingere automata o data, nudge manual)"`

---

### Task 3: Web: `stuck`, `stuckTasks`, ruta nudge

**Files:** Modify `internal/web/api_projects.go`, `internal/web/server.go`; Create `internal/web/api_nudge.go` (sau in `api_tasks.go`); Test `internal/web/stuck_test.go`.

**Interfaces — Produces:** `projectDetail.Stuck []team.StuckTask \`json:"stuck"\`` (mereu non-nil); `projectCard.StuckTasks int \`json:"stuckTasks"\``; `POST /api/tasks/{id}/nudge` -> 204; 409 (`ErrNotStuck`); 404.

- [ ] **Step 1: Test** — creeaza proiect + worker, task in review la lead (ca in Task 2), proiect activ; `GET /api/projects/{id}` are `stuck: []`; `POST /api/tasks/{id}/nudge` -> 204 si Lead are mesaje nelivrate; pe un task `done` -> 409; inexistent -> 404. Pentru `stuck` vizibil foloseste direct `e.tm.CheckStuck` / `StuckTasks` cu un `now` in viitor si verifica doar ca JSON-ul are campul (vizibilitatea e testata in team).

- [ ] **Step 2: RED**, **Step 3:** in `getProject`: `d.Stuck, err = s.team.StuckTasks(p.ID, s.team.Now())`; in `card()`: `c.StuckTasks = len(stuck)` (aceeasi functie). Handler nudge: `pathID`, `err := s.team.Nudge(id, s.team.Now())`; `errors.Is(err, team.ErrNotStuck)` -> 409 cu mesajul erorii; altfel `fail`/204. Ruta `POST /api/tasks/{id}/nudge`.

- [ ] **Step 4: GREEN**, **Step 5: Commit** — `git commit -m "api: task-uri blocate si nudge"`

---

### Task 4: UI

**Files:** Modify `internal/web/static/js/workspace.js`, `internal/web/static/js/projects.js`, `internal/web/static/js/questions.js` (sau un fisier nou `stuck.js`), `internal/web/static/app.css`.

- [ ] **Step 1: Banda** (`workspace.js`, `renderNeeds`): `const stuck = state.project.stuck || []`; cheia devine `questions.map(q=>"q"+q.id).concat(stuck.map(s=>"s"+s.taskId)).join(",")`; `needsBox.hidden = !(questions.length + stuck.length)`; titlul `Needs you (${questions.length + stuck.length})`; randurile: intai intrebarile, apoi `...stuck.map(stuckNode)`.

`stuckNode(s)` (componenta noua, `stuckBox(s, { agent, projectId, onDone })` exportata dintr-un `stuck.js` nou):

```js
// stuckBox: un task care nu se mai misca, cu agentul responsabil.
export function stuckBox(s, { agent, projectId, onDone }) {
  const mins = Math.max(1, Math.round((Date.now() - new Date(s.since)) / 60000));
  const why = s.kind === "orphan"
    ? `in progress but nothing is running · ${mins} min`
    : `waiting for ${s.agentName} to decide · ${mins} min`;
  const nudge = el("button", { type: "button", class: "btn small primary", onclick: async () => {
    nudge.disabled = true;
    try { await api(`/api/tasks/${s.taskId}/nudge`, { method: "POST" }); await onDone?.(); }
    catch (err) { toast(err.message); nudge.disabled = false; }
  } }, "Nudge again");
  return el("div", { class: "question stuck" },
    el("div", { class: "question-head" }, agent ? avatar(agent.name, kindOf(agent), 20) : null,
      el("strong", {}, `${s.ref} · ${s.title}`)),
    el("p", { class: "question-text" }, why),
    el("div", { class: "actions" }, nudge,
      el("a", { class: "btn small", href: `#/p/${projectId}/team/${s.agentId}` }, `Open ${s.agentName}`)));
}
```

(La click pe "Open", panoul din dreapta trebuie sa fie Chat: daca `update()` deschide Details pentru `team/<id>`, seteaza `state.panel = "chat"` cand ruta vine din acest link — de ex. `href` cu un sufix `#/p/<id>/team/<agentId>` si un handler `onclick` care seteaza `state.panel = "chat"` inainte de navigare; alege varianta minima care merge.)

- [ ] **Step 2: Card** (`projects.js`): numarul din badge = `(p.openQuestions || 0) + (p.stuckTasks || 0)`; clasa `needs-you-card` dupa acelasi total.

- [ ] **Step 3: Board** (`workspace.js`, `taskCard`): daca `(state.project.stuck || []).some((s) => s.taskId === t.id)`, adauga `el("span", { class: "badge-stuck", title: "No one is moving this task" }, "stuck")` (`cond ? x : null`).

- [ ] **Step 4: CSS**: `.badge-stuck { font-size: 11px; font-weight: 600; color: var(--amber); margin-left: auto; }` (si orice ajustare pentru `.question.stuck`).

- [ ] **Step 5: Verificare** — `.mjs` loop, `go test ./internal/web/`, `go vet ./...`. **Commit** — `git commit -m "ui: task-uri blocate in Needs you, pe card si pe board"`

---

### Task 5: Verificare in browser si CLAUDE.md

- [ ] Instanta de test (7789, HOME temporar, claude fals). Pentru a nu astepta 5 minute: pregateste in DB-ul de test un task in review cu `updated_at` si `nudged_at` vechi (UPDATE direct in sqlite pe copia de test), astfel incat sa fie vizibil imediat; verifica banda, Nudge again (mesajul nelivrat la decident / run pornit), Open <agent> (panoul Chat), badge-ul de pe card, eticheta "stuck" pe board; si ca raspunsul tastat intr-o intrebare deschisa ramane la reload-uri.
- [ ] CLAUDE.md, "Stare": `Gata (task-uri blocate, spec in docs/superpowers/specs/2026-10-01-foreman-stuck-tasks-design.md): verificare la 1 min; task-ul nemiscat 5 min (decizie la un agent idle sau In progress fara run) e impins automat o data, apoi apare in "Needs you" cu Nudge again.` + commit.
