# Conversatia pe task: plan de implementare

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** In modala task-ului, tab-ul "Conversation" arata comunicarea completa dintre agenti pe acel task (brief, raport, feedback, intrebari, raspunsuri, impingeri) plus ce a spus fiecare agent in run-urile pe task, live.

**Architecture:** `task_events` primeste `to_actor` si `body` (migrarea 012); team scrie body-ul in punctele unde inregistreaza deja istoricul; store citeste mesajele agentilor din run-urile task-ului; `GET /api/tasks/{id}` uneste totul in `conversation`; `task-modal.js` are tab-urile Conversation / History.

**Tech Stack:** Go, SQLite, vanilla JS.

**Spec:** `docs/superpowers/specs/2026-10-01-foreman-task-conversation-design.md`

## Global Constraints

- vet / race build / test trec; cross-compile darwin/arm64 + windows.
- JS verificat ca modul (`cp f /tmp/x.mjs && node --check /tmp/x.mjs`); `cond ? x : null` pentru valori date lui `el()` sau folosite cu `?.`; `replaceChildren`/`append` native nu aplatizeaza array-uri si scriu `null` ca text.
- SQLite cu o singura conexiune: in tranzactie doar `tx`; nicio interogare cat timp un `*sql.Rows` e deschis.
- Team: helper-ele apelate cu `t.work` tinut nu iau lock-uri.
- Texte vizibile in engleza; comentarii in romana fara diacritice.
- `body` maxim 20000 de runes (taiat cu " [...]"); `text` (History) ramane taiat la 200 cu "...".

## Review Focus

1. **Destinatarul corect la raport:** reviewer-ul daca asignatul are reviewer, altfel decidentul (inclusiv radacina in modul autonom), altfel "You". Test per caz (Task 2).
2. **Fara dubluri:** o comunicare = un singur rand in conversatie (nu cate unul per destinatar). Test (Task 2).
3. **Mesajele din run-uri ale altor task-uri sau ale utilizatorului nu intra in conversatie.** Test (Task 1).
4. **Modala nu pierde tab-ul ales, scroll-ul sau "Show more"** la evenimentele SSE. Verificat in browser (Task 4).
5. **Task vechi** (fara body-uri): Conversation arata mesajele din run-uri sau "No conversation recorded for this task.", fara erori. Test web (Task 3) + browser.

---

### Task 1: Store + helper

**Files:** Create `internal/store/migrations/012_task_conversation.sql`; Modify `internal/store/task_events.go` (+ test), `internal/store/messages.go` (sau fisier nou `internal/store/task_conversation.go`) (+ test), `internal/store/store_test.go` (user_version 11 -> 12), `internal/team/events.go`.

**Produces:**
- `store.TaskEvent` cu `To string` (JSON `to`) si `Body string` (JSON `body`), scrise de `AddTaskEvent` si citite de `ListTaskEvents`.
- `store.ListTaskRunMessages(taskID int64) ([]Message, error)`: mesajele `kind = 'msg'`, `author != 'user'`, din run-urile cu `runs.task_id = taskID` (join pe `messages.run_id = runs.id`), ordonate dupa id, mereu non-nil, cu `Attachments` non-nil (poti reutiliza `queryMessages` cu un `where` pe `run_id IN (SELECT id FROM runs WHERE task_id = ?)`).
- team: `func (t *Team) taskMsg(task store.Task, kind, from, to, text, body string)` (text taiat la 200 ca `taskEvent`, body la 20000); `taskNote` primeste campurile `to, body string` (constructorii existenti se actualizeaza; `record` foloseste `taskMsg` cand body e nevid).

- [ ] Migrarea (ca in spec), cu comentariu.
- [ ] Teste store: To/Body salvate si citite; body > 20000 runes taiat (taierea poate sta in team, atunci testul e in Task 2; decide si noteaza); `ListTaskRunMessages`: doua task-uri, run-uri pe fiecare, un mesaj al utilizatorului cu run_id, un mesaj `event` -> doar mesajele agentului din run-urile task-ului cerut.
- [ ] Commit: `store: conversatia pe task (to/body, mesajele din run-uri)`

### Task 2: Team: body pe toate comunicarile

**Files:** Modify `internal/team/tools.go`, `stages.go`, `reconcile.go`, `run.go`, `stuck.go`, `events.go`; Test `internal/team/conversation_test.go`.

Pentru fiecare rand din tabelul din spec ("Ce devine comunicare"), evenimentul existent primeste `to` si `body` (folosind `taskMsg` sau `taskNote` cu to/body). Nu schimba `kind`, `actor` sau `text` existente (History ramane identic). Unde un helper comun (`toReview`, `rejectTask`, `advance`, `sendToStage`) inregistreaza evenimentul, destinatarul se calculeaza acolo (reviewer / decident / "You").

- [ ] Citeste fiecare drum inainte de editare; daca un rand din spec nu are inca eveniment (ex. accept_task cu nota), adauga body pe evenimentul existent cel mai apropiat si noteaza in raport.
- [ ] `nudged`: doar impingerea pe decizie (o nota per agent pentru mai multe task-uri): fiecare task impins primeste body = textul notei, to = agentul impins.
- [ ] Teste: create (MCP: to = asignatul, body = titlu + descriere; board: from You), report (to = reviewer; to = lider; to = "You" pentru task din board fara decident; to = radacina in modul autonom), approve, reject (body = feedback), accept, send_back (lider si utilizator), ask (body cu optiunile) / answer / dismiss, nudge; un singur eveniment cu body per comunicare.
- [ ] Verificare completa (+ `go test -race ./internal/team/`); commit: `team: conversatia pe task`

### Task 3: API + UI

**Files:** Modify `internal/web/api_tasks.go` (+ test), `internal/team/team.go` (helper "cine lucreaza acum pe task"), `internal/web/static/js/task-modal.js`, `internal/web/static/app.css`.

**Produces:** `GET /api/tasks/{id}` -> in plus `conversation: [...]` (forma din spec, mereu non-nil) si `working: string`; `func (t *Team) WorkingOn(taskID int64) (agentID int64, ok bool)` (citeste `t.active` sub `t.mu`).

- [ ] API: uneste evenimentele cu body nevid (`type: "event"`, `at`, `kind`, `from` = actor, `to`, `text` = body) cu `ListTaskRunMessages` (`type: "run"`, `at`, `from` = author, `text`, `runId`); sortare dupa `at`, la egalitate evenimentele inainte. `working` = numele agentului daca `WorkingOn` ok, altfel "". Include si `workingAgentId` (0 daca nu) pentru link.
- [ ] Teste web: conversatie cu evenimente + run (ordine), evenimentele fara body lipsesc, task vechi (doar run-uri sau lista goala non-nil), `working` gol cand nu ruleaza nimic.
- [ ] Modala: tab-uri "Conversation" / "History" (butoane cu `aria-selected`); implicit Conversation daca are elemente; tab-ul ales tinut in starea modalei (nu se reseteaza la `refresh`).
- [ ] Bule: antet `from → to · ora` (fara sageata pentru `run` sau `to` gol), avatar (agentii din `project.agents` cu `avatar(name, kindOf(a), 20)`; "You"/"Foreman" cu avatarul simplu folosit deja in UI pentru ei sau initiala), text cu `white-space: pre-wrap`; "You" la dreapta (`.conv-msg.me`); text > 600 caractere scurtat cu buton "Show more" / "Show less" (starea pastrata per element intre refresh-uri, cheie = tip + at + from).
- [ ] `working`: rand final "<Agent> is working on it…" cu link "Watch live" la `#/p/<id>/team/<agentId>`.
- [ ] Gol: "No conversation recorded for this task."
- [ ] Live: conversatia se redeseneaza doar cand se schimba (numarul de elemente, `working`); scroll-ul nu sare.
- [ ] CSS: `.conv`, `.conv-msg`, `.conv-msg.me`, `.conv-head`, `.conv-text`, `.conv-working`; regula `[hidden]` daca un container cu `display` se ascunde; mobil fara overflow orizontal.
- [ ] Verificare: bucla `.mjs`, `go test ./internal/web/`, `go vet ./...`; commit: `ui: tab-ul Conversation in modala task-ului`

### Task 4: Verificare in browser si CLAUDE.md

- [ ] Instanta de test (7789, HOME temporar, claude fals): un lider care creeaza un task cu descriere pentru un worker cu reviewer, worker-ul raporteaza, reviewer-ul respinge cu feedback, worker-ul raporteaza din nou, reviewer-ul aproba, liderul accepta; plus o intrebare ask_user si raspunsul.
- [ ] CDP: tab-ul Conversation implicit, bulele in ordine cu sagetile corecte, You la dreapta, Show more pe un raport lung, Working + Watch live in timpul unui run, tab-ul ramane la SSE, task vechi; screenshot-uri desktop + mobil.
- [ ] CLAUDE.md, "Stare": `Gata (conversatia pe task, spec in docs/superpowers/specs/2026-10-01-foreman-task-conversation-design.md): task_events cu destinatar si text complet; tab-ul Conversation din modala arata brief-ul, rapoartele, feedback-ul, intrebarile si ce a spus fiecare agent in run-urile pe task.` + commit.
