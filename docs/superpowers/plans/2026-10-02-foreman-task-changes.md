# Modificarile de fisiere pe task: plan de implementare

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Pe fiecare task: ce fisiere au fost modificate (diff-ul din `structuredPatch`), de cine, cand, si comenzile shell rulate; eticheta "N files" pe card si tab-ul "Changes" in modala.

**Architecture:** Runner: tracker-ul leaga `tool_use` de `tool_result` / `tool_use_result` si produce `Outcome.Changes` / `Outcome.Commands`. Store: tabel `task_changes` (migrarea 013). Team: `recordOutcome` le salveaza pentru run-urile pe task. Web: `GET /api/tasks/{id}/changes` + `filesChanged` pe task-uri. UI: eticheta pe card, tab in `task-modal.js`.

**Tech Stack:** Go, SQLite, vanilla JS.

**Spec:** `docs/superpowers/specs/2026-10-02-foreman-task-changes-design.md`

## Global Constraints

- vet / race build / test trec; cross-compile darwin/arm64 + windows.
- JS verificat ca modul (`cp f /tmp/x.mjs && node --check /tmp/x.mjs`); `cond ? x : null` pentru valori date lui `el()` sau folosite cu `?.`; `replaceChildren`/`append` native nu aplatizeaza array-uri si scriu `null` ca text; regula `[hidden]` pentru containere cu `display`.
- SQLite cu o singura conexiune: in tranzactie doar `tx`; nicio interogare cat timp un `*sql.Rows` e deschis.
- Team: helper-ele apelate cu `t.work` tinut nu iau lock-uri.
- Texte vizibile in engleza, exact ca in spec; comentarii in romana fara diacritice.
- Limite: patch maxim 200000 de caractere per modificare (`Truncated`), comanda maxim 2000 ("...").

## Review Focus

1. **Doar modificarile reusite:** un `tool_result` cu `is_error: true` nu produce `FileChange`; un `tool_use` fara rezultat (run oprit la mijloc) nu produce nimic. Test (Task 1).
2. **Run esuat sau oprit** pastreaza modificarile deja facute. Test (Task 2).
3. **Linii lungi / fisiere mari** nu strica pagina: diff-ul are scroll orizontal propriu, pagina nu. Verificat in browser (Task 4).
4. **Starea deschis/inchis a diff-urilor si tab-ul ales** raman la refresh-ul live al modalei. Browser (Task 4).
5. **Cai in afara folderului agentului** (ex. `--add-dir`): raman absolute, nu "../..". Test (Task 2).

---

### Task 1: Runner: captarea modificarilor

**Files:** Modify `internal/runner/tracker.go`, `internal/runner/runner.go`; Test `internal/runner/tracker_test.go` (sau fisierul de test existent al tracker-ului).

**Produces:** `runner.Hunk{OldStart, OldLines, NewStart, NewLines int; Lines []string}`; `runner.FileChange{Tool, Path, Kind string; Patch []Hunk; Truncated bool}`; `runner.Outcome.Changes []FileChange`, `runner.Outcome.Commands []string`.

- [ ] Tracker: pe evenimentele `assistant`, retine `tool_use`-urile cu nume in {Write, Edit, MultiEdit, NotebookEdit, Bash} dupa `id` (input-ul decodat). Pe evenimentele `user`: pentru fiecare `tool_result` din `message.content` (cand `content` e lista) cu `tool_use_id` cunoscut: daca `is_error == true`, il ignori (dar Bash esuat intra totusi in `Commands`); altfel construiesti `FileChange` din `tool_use_result` al evenimentului (`type` "create"/"update" pentru Write, altfel "edit"; `structuredPatch`; la "create" un bloc `{0,0,1,N}` cu toate liniile din `content` prefixate cu "+"), iar daca `tool_use_result` lipseste sau nu are `structuredPatch`, fallback-ul din spec. Bash -> `Commands` (input.command, taiat la 2000 + "...").
- [ ] Atentie: un eveniment `user` poate avea mai multe `tool_result`; `tool_use_result` e unul singur per eveniment (asociaza-l doar cand evenimentul are exact un `tool_result`; altfel foloseste fallback-ul).
- [ ] Taierea la 200000 de caractere (suma lungimilor liniilor): scoate blocurile de la final, scurteaza ultimul bloc pastrat, `Truncated = true`.
- [ ] `outcome()` copiaza `Changes` / `Commands` indiferent de status.
- [ ] Teste (linii JSON reale, ca in spec): Write create, Write update (cu structuredPatch), Edit, MultiEdit, Edit esuat ignorat, tool_use fara rezultat ignorat, fallback fara tool_use_result (Edit si Write), NotebookEdit fallback, Bash reusit si esuat, taiere comanda, taiere patch.
- [ ] Commit: `runner: modificarile de fisiere si comenzile din stream`

### Task 2: Store + team

**Files:** Create `internal/store/migrations/013_task_changes.sql`, `internal/store/task_changes.go` (+ test); Modify `internal/store/store_test.go` (user_version 12 -> 13), `internal/team/run.go` (`recordOutcome`); Test `internal/team/changes_test.go`.

**Consumes:** `runner.Outcome.Changes/Commands` (Task 1).

**Produces:** `store.Hunk` (JSON `oldStart, oldLines, newStart, newLines, lines`); `store.TaskChange{ID, TaskID int64; RunID string; AgentID *int64; AgentName string; At time.Time; Kind, Tool, Path string; Patch []Hunk; Command string; Truncated bool}`; `AddTaskChanges([]TaskChange) error`; `ListTaskChanges(taskID) ([]TaskChange, error)`; `CountChangedFiles(projectID) (map[int64]int, error)`.

- [ ] Migrarea exact ca in spec.
- [ ] Store: `patch` salvat ca JSON; `AddTaskChanges` intr-o tranzactie (doar `tx`); `CountChangedFiles` cu `COUNT(DISTINCT path)` pe task-urile proiectului, `kind != 'command'`.
- [ ] Team (`recordOutcome`, pentru orice status, inaintea logicii de status, ca `recordDenials`): daca run-ul are task si exista modificari/comenzi, construieste `TaskChange`-urile (kind `command` pentru comenzi, cu `tool` "Bash"); calea relativa la folderul agentului (`filepath.Rel`) doar daca rezultatul nu incepe cu ".."; altfel absoluta. Folderul agentului: cum il gaseste deja run-ul (folderul agentului sau primul folder al proiectului). Erorile logate. Daca s-a salvat ceva: `emit(project, "task", task)`.
- [ ] Teste store: migrarea, add/list (patch round-trip), count (cai distincte, fara comenzi, doar proiectul cerut), cascade la stergerea task-ului, agent sters -> `AgentID` nil si `AgentName` pastrat.
- [ ] Teste team: run pe task cu 2 modificari (un fisier de doua ori) + o comanda -> randuri cu cai relative; cale in afara folderului -> absoluta; run fara task -> nimic; run esuat cu modificari -> salvate.
- [ ] Verificare completa (+ `go test -race ./internal/team/`); commit: `store+team: modificarile de fisiere pe task`

### Task 3: API + UI

**Files:** Modify `internal/web/api_tasks.go` (+ test), `internal/web/server.go` (ruta), `internal/web/api_projects.go` (`filesChanged`), `internal/web/static/js/task-modal.js`, `internal/web/static/js/workspace.js` (eticheta pe card), `internal/web/static/app.css`. Optional fisier nou `internal/web/static/js/changes.js` pentru randarea diff-urilor (recomandat, ca `task-modal.js` sa nu creasca mult).

**Consumes:** `ListTaskChanges`, `CountChangedFiles` (Task 2).

- [ ] `GET /api/tasks/{id}/changes`: gruparea pe run si unirea aceluiasi fisier exact ca in spec; `added`/`removed` = numarul liniilor care incep cu "+" / "-"; `runs` non-nil; 404.
- [ ] `filesChanged` pe fiecare task din `GET /api/projects/{id}` (0 implicit). Daca task-urile sunt `store.Task` serializate direct, adauga un camp in tipul de raspuns (nu in `store.Task`), urmand modelul existent din `api_projects.go`.
- [ ] Teste web: grupare, unire, numaratori, comenzi, `filesChanged`, 404, task fara modificari (`runs: []`).
- [ ] Card: eticheta "N files" / "1 file" (`class="task-files"`), discreta, langa celelalte etichete.
- [ ] Modala: tab-ul "Changes" (al treilea); datele se incarca la prima deschidere a tab-ului si se reincarca la refresh doar cand tab-ul e deschis si `filesChanged` al task-ului s-a schimbat; antet de run, randuri de fisier (cale monospace, "new", `+N` verde, `−N` rosu), click -> diff (stare pastrata, cheie run + cale); blocuri cu antet `@@ ... @@` (omis cand numerele sunt 0), linii colorate, "\\ No newline at end of file" gri; "Truncated: the change was too large to keep in full."; "Commands run"; starea goala cu hint-ul din spec.
- [ ] CSS: diff monospace, `overflow-x: auto` pe containerul diff-ului, `white-space: pre`, culori din variabilele existente (sau noi pe `:root` cu varianta dark/light ca restul aplicatiei); mobil fara overflow orizontal al paginii.
- [ ] Verificare: bucla `.mjs`, `go test ./internal/web/`, `go vet ./...`; commit: `ui: tab-ul Changes si eticheta de fisiere pe task`

### Task 4: Verificare in browser si CLAUDE.md

- [ ] Claude fals care, pentru un task, emite in stream evenimente reale `assistant`/`user` cu Write create, Edit (structuredPatch), un Edit esuat si un Bash, apoi `report_done`.
- [ ] CDP: eticheta "2 files" pe card; tab-ul Changes: antetul run-ului, fisierele cu +/−, diff-ul deschis colorat, Edit-ul esuat absent, Commands run; deschis/inchis si tab-ul pastrate la un eveniment SSE; o linie foarte lunga nu da scroll paginii; mobil; screenshot-uri.
- [ ] CLAUDE.md, "Stare": `Gata (modificarile pe task, spec in docs/superpowers/specs/2026-10-02-foreman-task-changes-design.md): din stream-json (tool_use + tool_use_result.structuredPatch) se salveaza diff-urile Edit/Write si comenzile Bash per task (task_changes); eticheta "N files" pe card si tab-ul Changes in modala.` + commit.
