# Modala de task si istoricul task-ului: plan de implementare

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Istoric structurat per task (`task_events`), inregistrat pe toate drumurile din `internal/team`; `GET /api/tasks/{id}`; pe board, card compact si modala cu descriere, istoric, raport ascuns si actiuni.

**Architecture:** Store: tabel + 2 functii. Team: helper `taskEvent` apelat din fiecare loc care schimba un task. Web: o ruta. UI: componenta `task-modal.js`, card compact in `workspace.js`, ruta `#/p/<id>/tasks/<taskId>`.

**Tech Stack:** Go, SQLite, vanilla JS.

**Spec:** `docs/superpowers/specs/2026-10-01-foreman-task-modal-design.md`

## Global Constraints

- vet / race build / test trec; cross-compile darwin/arm64 + windows.
- JS verificat ca modul (`cp f /tmp/x.mjs && node --check /tmp/x.mjs`); `cond ? x : null` pentru valori folosite cu `?.`; `replaceChildren`/`append` native nu aplatizeaza array-uri; regula `[hidden]` pentru containere cu `display`.
- Textele din istoric si UI in engleza; comentarii in romana fara diacritice.
- Team: helper-ul se cheama cu `t.work` tinut (unde e cazul), nu ia lock-uri; o eroare la scrierea evenimentului se logheaza si nu opreste fluxul.
- Textul unui eveniment: maxim 200 de caractere (taiat cu "...").

## Review Focus

1. **Niciun drum uitat:** fiecare loc din `internal/team` care schimba starea, asignatul sau etapa unui task scrie evenimentul potrivit. Teste per drum (Task 2).
2. **Modala nu pierde ce scrie utilizatorul** (feedback la Send back) la reincarcarile SSE. Verificat in browser (Task 4).
3. **Task vechi fara istoric:** API-ul intoarce un `created` sintetic, nesalvat. Test (Task 1).
4. **Ruta cu modala** (`#/p/<id>/tasks/<taskId>`) nu strica rutele existente (`#/p/<id>/tasks`, `#/p/<id>/team/<agent>`, adresele vechi in romana). Verificat in Task 3/4.

---

### Task 1: Store + helper + ruta

**Files:** Create `internal/store/migrations/009_task_events.sql`, `internal/store/task_events.go`; Modify `internal/team/` (helper in fisier nou `internal/team/events.go`), `internal/web/` (ruta noua, ex. `api_tasks.go`), `internal/web/server.go`; Tests in store si web.

**Produces:** `store.TaskEvent{ID, TaskID int64; At time.Time; Kind, Actor, Text string}` (JSON `id, taskId, at, kind, actor, text`); `AddTaskEvent(e *TaskEvent) error`; `ListTaskEvents(taskID int64) ([]TaskEvent, error)` (mereu non-nil); `func (t *Team) taskEvent(task store.Task, kind, actor, text string)`; `GET /api/tasks/{id}` -> `{task, events, assignee, stage}`.

- [ ] Migrarea (ca in spec), cu comentariu; `user_version` 8 -> 9 in testul existent.
- [ ] `AddTaskEvent` (seteaza `At` = now daca e zero, scrie RFC3339Nano), `ListTaskEvents` ordonat dupa id.
- [ ] Helper team `taskEvent`: taie textul la 200 de runes ("..."), `AddTaskEvent`, la eroare `log.Printf("task %d: event: %v", ...)`.
- [ ] Ruta: incarca task-ul (404), evenimentele; daca lista e goala, intoarce `[{kind:"created", text:"Created", at: task.CreatedAt}]` (nesalvat); `assignee` = numele asignatului (sau ""), `stage` = numele etapei curente (sau "").
- [ ] Teste: store (adaugare, listare, cascade la stergerea proiectului/task-ului); web (evenimente reale, sintetic pentru task fara evenimente, 404).
- [ ] Commit: `store+api: istoricul task-ului (task_events) si GET /api/tasks/{id}`

### Task 2: Inregistrarea pe toate drumurile

**Files:** Modify `internal/team/tools.go`, `reconcile.go`, `run.go`, `stages.go`, `stuck.go`; Test `internal/team/events_test.go` (nou).

**Consumes:** `taskEvent` (Task 1).

Pentru fiecare drum de mai jos, apeleaza `t.taskEvent(task, kind, actor, text)` dupa ce schimbarea a fost salvata cu succes, cu textele din spec (sectiunea "Unde se inregistreaza"):

| Drum (functie) | kind | actor | text |
|---|---|---|---|
| `CreateTask` (MCP, lider) | created + assigned | lider | "Created by <Lead>" ; "Assigned to <Agent>" (sau "Queued for stage <S>" in proiect cu etape fara asignat) |
| `UserCreateTask` (board) | created + assigned | You | "Created by you" ; "Assigned to <Agent>" |
| `AssignTask` (board) | assigned | You | "Assigned to <Agent>" |
| scheduler: queued -> in_progress (`reconcile.go`) | started | agent | "<Agent> started working" |
| `ReportDone` | reported | agent | "<Agent> reported it done" |
| mutare in etapa urmatoare (stages.go) | stage | agent/Foreman | "Moved to stage <S>" |
| `ReviewApprove` / `ReviewReject` | approved / rejected | reviewer | "<R> approved" / "<R> rejected: <feedback>" |
| `AcceptTask` / `SendBack` | accepted / sent_back | lider | "<L> accepted" / "<L> sent it back: <feedback>" |
| trecerea in blocked (dupa 3 trimiteri sau fara decident) | blocked | Foreman | motivul real (ex. "Blocked after 3 send-backs") |
| `UserDecide` (accept / send_back) | user_accepted / sent_back | You | "You accepted" / "You sent it back: <feedback>" |
| `resolveBlocked` (AcceptBlocked / Unblock) | user_accepted / unblocked | You | "You accepted" / "You unblocked it" |
| `AskUser` (cu task) | asked | agent | "<Agent> asked you: <intrebare>" |
| `closeQuestion` (cu task) | answered | You | "You answered" / "You dismissed the question" |
| sub-lider oprit cu sub-task-uri deschise (`recordOutcome`) | waiting_subtasks | agent | "Waiting for subtasks T-4, T-5" |
| run esuat pe task (`recordOutcome`, RunFailed) | failed | agent | "<Agent>'s run failed: <eroare>" |
| detector: decizie / orfan | nudged / requeued | Foreman | "Foreman nudged <Agent>" / "Foreman requeued it" |
| finished without report_done (`toReview` din recordOutcome) | reported | agent | "<Agent> finished without report_done" |

- [ ] Citeste fiecare functie inainte de editare; daca un drum lipseste sau e altfel decat in tabel, adapteaza minim si noteaza in raport. Nu duplica: daca mai multe drumuri trec printr-un helper comun (ex. `toReview`, `afterDone`, trecerea in blocked), pune evenimentul in helper.
- [ ] Teste in `events_test.go`: un test per rand din tabel (sau grupate logic), folosind fixture-ul existent (`setup`, `f.task`, `f.tm.*`, `mustReconcile`, `f.fs.finish`), care verifica ca `ListTaskEvents` contine `kind`-ul si textul asteptat (substring).
- [ ] Verificare completa (vet, race, test, `go test -race ./internal/team/`, cross-compile).
- [ ] Commit: `team: istoricul task-ului pe toate drumurile`

### Task 3: UI: card compact, modala, ruta

**Files:** Create `internal/web/static/js/task-modal.js`; Modify `internal/web/static/js/workspace.js`, `internal/web/static/app.css`.

- [ ] **Ruta:** `parseWorkspaceRoute` intoarce si `taskId` pentru `tasks/<n>` (numar); rutele existente neschimbate (inclusiv adresele vechi in romana). `update()` deschide/inchide modala dupa `taskId`.
- [ ] **Card compact:** `taskCard` nu mai e `<details>`: un `button`/element focusabil cu ref + titlu, agent, etichetele existente, si `t.description` pe maxim 2 randuri (`.task-desc` cu line-clamp). Click/Enter -> `location.hash = #/p/<id>/tasks/<taskId>`. Se scot `state.open` si zona `.task-detail` (mutate in modala).
- [ ] **Modala** (`taskModal({ projectId, taskId, project, info, onClose, actions })` -> `{ el, refresh(), destroy() }`): overlay + fereastra; Esc / click pe overlay / × inchid (navigheaza la `#/p/<id>/tasks`); incarca `GET /api/tasks/{id}`; sectiuni ca in spec (Description, Feedback, History cu ora `clock()`, Show report). Actiunile folosesc functiile existente din workspace (`taskAction`, `decide`, `assign`, nudge) primite prin `actions`, ca sa nu se dubleze logica.
- [ ] **Live:** la `reload()` din workspace (SSE), daca modala e deschisa, `modal.refresh()` reincarca datele si redeseneaza doar antetul, descrierea, feedback-ul, istoricul si raportul; zona de actiuni se reconstruieste doar daca s-a schimbat starea task-ului (ca un textarea de feedback sa nu se piarda).
- [ ] **CSS:** `.modal-overlay` (fixed, fundal semi-transparent, z-index peste tot), `.modal` (max-width ~720px, max-height 85vh, scroll intern, `--card`), `.task-desc` (line-clamp 2), `.history` (lista cu ora in `--muted`); regula `[hidden]` daca e nevoie; mobil: modala aproape full-screen.
- [ ] Verificare: bucla `.mjs`, `go test ./internal/web/`, `go vet ./...`.
- [ ] Commit: `ui: card compact si modala de task cu istoric`

### Task 4: Verificare in browser si CLAUDE.md

- [ ] Instanta de test (7789, HOME temporar, claude fals); un proiect cu un task care trece prin cateva stari (creat din board -> pornit -> raportat), plus un task vechi.
- [ ] CDP: card compact cu descrierea; click -> modala cu Description, History (evenimentele), Show report; Esc inchide; refresh pe `#/p/<id>/tasks/<taskId>` redeschide; Send back cu feedback tastat ramane in camp la un eveniment SSE; task vechi arata "Created"; rutele `#/p/<id>/team/<agent>` si `#/p/<id>/task-uri` merg in continuare. Screenshot-uri.
- [ ] CLAUDE.md, "Stare": `Gata (modala de task, spec in docs/superpowers/specs/2026-10-01-foreman-task-modal-design.md): istoric per task (task_events) inregistrat pe toate drumurile; card compact pe board si modala cu descriere, istoric, raport ascuns si actiuni.` + commit.
