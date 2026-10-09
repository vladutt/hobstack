# Mutarea task-urilor (drag & drop) si stergerea lor (arhivare): plan de implementare

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Utilizatorul trage un card pe alta coloana (actiunea reala a coloanei) si poate sterge un task (arhivare cu Restore).

**Architecture:** Status nou `archived` (+ `archived_from`, `archived_at`, `archived_root`, migrarea 025); `ListTasks` / `Subtasks` nu mai intorc arhivate, deci scheduler-ul, board-ul si tool-urile MCP le ignora fara schimbari; la arhivare se inchid intrebarile / cererile legate. `team.MoveTask` alege actiunea dupa (status, tinta) si refoloseste deciziile existente (refactor in variante fara lock). UI: HTML5 drag & drop pe board, meniu "⋯" pe card (Move to… / Delete), panou "Deleted".

**Tech Stack:** Go, SQLite (modernc), JS vanilla.

**Spec:** `docs/superpowers/specs/2026-10-09-foreman-task-move-delete-design.md`

## Global Constraints

- UI, mesaje, erori si textele pentru agenti in engleza; comentariile din cod in romana.
- `go vet ./...`, `go build -race ./...`, `go test ./...` trec; JS verificat cu `node --check` pe o copie `.mjs`.
- Cross-compile: `GOOS=darwin GOARCH=arm64 go build ./cmd/foreman` si `GOOS=windows go build ./cmd/foreman`.
- Fara dependente noi (drag & drop cu API-ul HTML5).
- Nimic nu se sterge definitiv din baza de date sau de pe disc.
- Tintele de mutare: `"queued"`, `"done"`, `"stage:<id>"`. Motiv obligatoriu pentru Send back si Reopen.

## Review Focus

1. **Run activ pe un task sters**: run-ul e oprit, iar `finish` (task-ul nu mai e In progress) nu il readuce pe board. Test in Task 2.
2. **Parinte care asteapta sub-task-uri, iar un sub-task e sters**: parintele nu ramane blocat ("open subtasks", `notWaitingParent`). Test in Task 1 si Task 2.
3. **Sarcina recurenta a carei aparitie precedenta e stearsa**: urmatoarea aparitie se creeaza. Test in Task 2.
4. **Etapa stearsa cat timp un task arhivat o refera**: stergerea etapei nu e blocata de arhivate, iar Restore pune task-ul in "No stage". Test in Task 1.
5. **Drop pe In progress / Review, sau al unui card In progress**: refuz cu motiv, fara schimbare. Test in Task 3 (server) si Task 5 (UI).

---

### Task 1: Store: migrarea 025, arhivare, filtre

**Files:**
- Create: `internal/store/migrations/025_archived_tasks.sql`
- Create: `internal/store/archive.go`
- Create: `internal/store/archive_test.go`
- Modify: `internal/store/tasks.go` (Task, taskCols, scanTask, ListTasks, Subtasks, notWaitingParent, StatusArchived)
- Modify: `internal/store/todos.go:101` (ListFillIns), `internal/store/stats.go:244` (statsTasks), `internal/store/stages.go:74-110` (CountStageTasks, SaveStages), `internal/store/task_files.go` (ListDescendantFiles, CountTaskFilesTree), `internal/store/store_test.go` (user_version 25)

**Interfaces:**
- Produces:
  - `const StatusArchived = "archived"`
  - campuri noi in `Task`: `ArchivedFrom string \`json:"archivedFrom"\``, `ArchivedAt *time.Time \`json:"archivedAt"\``, `ArchivedRoot *int64 \`json:"archivedRoot"\``
  - `func (s *Store) TaskTree(id int64) ([]Task, error)` — task-ul + descendentii nearhivati (task-ul primul)
  - `func (s *Store) ArchiveTasks(ids []int64, root int64) error` — tranzactie: status archived + inchide `questions`, `access_requests`, `agent_requests` deschise ale task-urilor
  - `func (s *Store) RestoreGroup(root int64) ([]Task, error)` — readuce grupul; intoarce task-urile restaurate
  - `func (s *Store) ListArchivedRoots(projectID int64) ([]ArchivedGroup, error)`; `type ArchivedGroup struct { Task; Subtasks int \`json:"subtasks"\` }`
  - `func (s *Store) CountArchivedRoots(projectID int64) (int, error)`

- [ ] **Step 1: Migrarea**

`internal/store/migrations/025_archived_tasks.sql`:
```sql
-- Task-uri sterse de utilizator (arhivare cu Restore): status 'archived', statusul dinainte si grupul sters
ALTER TABLE tasks ADD COLUMN archived_from TEXT NOT NULL DEFAULT '';
ALTER TABLE tasks ADD COLUMN archived_at TEXT;
ALTER TABLE tasks ADD COLUMN archived_root INTEGER;
CREATE INDEX tasks_archived ON tasks(project_id, archived_root);
```
In `store_test.go` (`TestOpenAppliesMigrationsOnce`) schimba `24` in `25` (ambele aparitii).

- [ ] **Step 2: Testul care pica**

`internal/store/archive_test.go`:
```go
package store

import "testing"

func TestArchiveAndRestore(t *testing.T) {
	s := newStore(t)
	p, lead := newProject(t, s)
	mk := func(title, status string, parent *int64) Task {
		x := Task{ProjectID: p.ID, Title: title, AssigneeID: lead.ID, ParentTaskID: parent}
		if err := s.CreateTask(&x); err != nil {
			t.Fatal(err)
		}
		x.Status = status
		if err := s.SaveTask(&x); err != nil {
			t.Fatal(err)
		}
		return x
	}
	parent := mk("Parent", StatusInProgress, nil)
	child := mk("Child", StatusInProgress, &parent.ID)
	grand := mk("Grand", StatusQueued, &child.ID)
	other := mk("Other", StatusQueued, nil)

	// o intrebare deschisa pe copil: se inchide la arhivare
	q := Question{ProjectID: p.ID, AgentID: lead.ID, TaskID: &child.ID, Text: "Which DB?"}
	if err := s.AddQuestion(&q); err != nil {
		t.Fatal(err)
	}
	// parintele si copilul asteapta sub-task-uri deschise: niciunul nu se re-pune in coada
	if ids, _ := s.RequeueInProgress(p.ID); len(ids) != 0 {
		t.Fatalf("parinte/copil cu sub-task-uri deschise re-pusi: %v", ids)
	}

	tree, err := s.TaskTree(child.ID)
	if err != nil || len(tree) != 2 || tree[0].ID != child.ID || tree[1].ID != grand.ID {
		t.Fatalf("arbore: %+v %v", tree, err)
	}
	if err := s.ArchiveTasks([]int64{child.ID, grand.ID}, child.ID); err != nil {
		t.Fatal(err)
	}
	// parintele nu mai asteapta un sub-task sters
	if ids, _ := s.RequeueInProgress(p.ID); len(ids) != 1 || ids[0] != parent.ID {
		t.Fatalf("parintele inca asteapta un sub-task sters: %v", ids)
	}
	list, _ := s.ListTasks(p.ID)
	if len(list) != 2 {
		t.Fatalf("ListTasks fara arhivate: %d", len(list))
	}
	if subs, _ := s.Subtasks(parent.ID); len(subs) != 0 {
		t.Fatalf("Subtasks fara arhivate: %+v", subs)
	}
	if open, _ := s.ListOpenQuestions(p.ID); len(open) != 0 {
		t.Fatalf("intrebarea ramasa deschisa: %+v", open)
	}
	got, _ := s.GetTask(child.ID)
	if got.Status != StatusArchived || got.ArchivedFrom != StatusInProgress || got.ArchivedAt == nil || *got.ArchivedRoot != child.ID {
		t.Fatalf("%+v", got)
	}
	roots, _ := s.ListArchivedRoots(p.ID)
	if len(roots) != 1 || roots[0].ID != child.ID || roots[0].Subtasks != 1 {
		t.Fatalf("radacini: %+v", roots)
	}
	if n, _ := s.CountArchivedRoots(p.ID); n != 1 {
		t.Fatalf("numar: %d", n)
	}

	restored, err := s.RestoreGroup(child.ID)
	if err != nil || len(restored) != 2 {
		t.Fatalf("%+v %v", restored, err)
	}
	got, _ = s.GetTask(child.ID)
	// era In progress: revine Queued (run-ul fusese oprit)
	if got.Status != StatusQueued || got.ArchivedFrom != "" || got.ArchivedAt != nil || got.ArchivedRoot != nil {
		t.Fatalf("dupa restore: %+v", got)
	}
	if o, _ := s.GetTask(other.ID); o.Status != StatusQueued {
		t.Fatalf("un task din afara grupului atins: %s", o.Status)
	}
}

// un task arhivat in curs: la Restore revine Queued (run-ul a fost oprit)
func TestRestoreInProgressBecomesQueued(t *testing.T) {
	s := newStore(t)
	p, lead := newProject(t, s)
	x := Task{ProjectID: p.ID, Title: "A", AssigneeID: lead.ID}
	if err := s.CreateTask(&x); err != nil {
		t.Fatal(err)
	}
	x.Status = StatusInProgress
	if err := s.SaveTask(&x); err != nil {
		t.Fatal(err)
	}
	if err := s.ArchiveTasks([]int64{x.ID}, x.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RestoreGroup(x.ID); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.GetTask(x.ID); got.Status != StatusQueued {
		t.Fatalf("%s", got.Status)
	}
}

// o etapa se poate sterge chiar daca un task arhivat o refera; la Restore task-ul ramane fara etapa
func TestArchivedTaskDoesNotBlockStageDelete(t *testing.T) {
	s := newStore(t)
	p, lead := newProject(t, s)
	stages, err := s.SaveStages(p.ID, []Stage{{Name: "Build", Kind: KindWork, AgentIDs: []int64{lead.ID}}})
	if err != nil {
		t.Fatal(err)
	}
	x := Task{ProjectID: p.ID, Title: "A", AssigneeID: lead.ID, StageID: &stages[0].ID}
	if err := s.CreateTask(&x); err != nil {
		t.Fatal(err)
	}
	if err := s.ArchiveTasks([]int64{x.ID}, x.ID); err != nil {
		t.Fatal(err)
	}
	if n, _ := s.CountStageTasks(stages[0].ID); n != 0 {
		t.Fatalf("arhivatul numarat in etapa: %d", n)
	}
	if _, err := s.SaveStages(p.ID, nil); err != nil {
		t.Fatalf("stergerea etapei blocata: %v", err)
	}
	if _, err := s.RestoreGroup(x.ID); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.GetTask(x.ID); got.StageID != nil {
		t.Fatalf("etapa stearsa ramasa: %v", *got.StageID)
	}
}
```
Inainte de a rula: verifica numele reale `AddQuestion` / `Question` / `KindWork` (`grep -n "func (s \*Store) AddQuestion\|func (s \*Store) CreateQuestion\|KindWork" internal/store/*.go`) si adapteaza doar apelurile (nu asteptarile). Nota: `child` e In progress cu `grand` deschis sub el, deci la arhivare `ArchivedFrom` e `in_progress`.

- [ ] **Step 3: Ruleaza, trebuie sa pice**

Run: `go test ./internal/store -run 'Archive|Restore|ArchivedTask'`
Expected: FAIL (`undefined: TaskTree` / campuri lipsa)

- [ ] **Step 4: Task, coloane, filtre**

In `internal/store/tasks.go`:
- langa celelalte statusuri: `StatusArchived = "archived" // sters de utilizator (Restore il readuce)`
- in `Task`, dupa `ThreadID`:
```go
	// Arhivare (Delete din board): statusul dinainte, cand si grupul sters (task-ul pe care a apasat utilizatorul).
	ArchivedFrom string     `json:"archivedFrom"`
	ArchivedAt   *time.Time `json:"archivedAt"`
	ArchivedRoot *int64     `json:"archivedRoot"`
```
- `taskCols` primeste la final `, archived_from, archived_at, archived_root`; `scanTask` citeste cele trei (ca `nudged_at` pentru timp: `sql.NullString` -> `parseTime`; `archived_root` ca `sql.NullInt64`). Urmeaza exact tiparul existent din `scanTask` pentru `NudgedAt` si `ThreadID`.
- `SaveTask` NU scrie coloanele de arhivare (le scrie doar `ArchiveTasks` / `RestoreGroup`), ca o salvare obisnuita sa nu le strice.
- `ListTasks`: `... WHERE project_id = ? AND status != 'archived' ...`
- `Subtasks`: `... WHERE parent_task_id = ? AND status != 'archived' ...`
- `notWaitingParent`: `sub.status NOT IN ('done', 'archived')`

`ListFillIns` (todos.go): `WHERE t.project_id = ? AND t.status != 'archived'`.
`statsTasks` (stats.go): `WHERE t.project_id = ? AND t.status != 'archived'`.
`CountStageTasks` si interogarea din `SaveStages`: `WHERE stage_id = ? AND status != 'archived'`; in `SaveStages`, inainte de `DELETE FROM stages WHERE id = ?`:
```go
		// task-urile arhivate din etapa raman fara etapa (la Restore apar in "No stage")
		if _, err := tx.Exec(`UPDATE tasks SET stage_id = NULL WHERE stage_id = ? AND status = 'archived'`, o.ID); err != nil {
			return nil, err
		}
```
`task_files.go`: in CTE-urile din `ListDescendantFiles` si `CountTaskFilesTree`, pasul recursiv primeste `AND t.status != 'archived'` (si primul pas din `ListDescendantFiles`: `WHERE parent_task_id = ? AND status != 'archived'`).

- [ ] **Step 5: archive.go**

`internal/store/archive.go`:
```go
package store

import "strings"

// ArchivedGroup: un task sters de utilizator (radacina grupului) si cate sub-task-uri au plecat cu el.
type ArchivedGroup struct {
	Task
	Subtasks int `json:"subtasks"`
}

// TaskTree: task-ul si descendentii lui nearhivati, task-ul primul, apoi in ordinea numerelor.
func (s *Store) TaskTree(id int64) ([]Task, error) {
	rows, err := s.db.Query(`WITH RECURSIVE sub(id) AS (
			SELECT ? UNION ALL SELECT t.id FROM tasks t JOIN sub ON t.parent_task_id = sub.id WHERE t.status != 'archived')
		SELECT `+taskCols+` FROM tasks WHERE id IN (SELECT id FROM sub) ORDER BY id = ? DESC, number`, id, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Task{}
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// ArchiveTasks arhiveaza task-urile (statusul dinainte se pastreaza) si inchide ce astepta dupa ele
// in "Needs you": intrebari, cereri de acces, cereri de agenti. Totul intr-o tranzactie.
func (s *Store) ArchiveTasks(ids []int64, root int64) error {
	if len(ids) == 0 {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	ts := now()
	in, args := idList(ids)
	q := `UPDATE tasks SET archived_from = status, status = 'archived', archived_at = ?, archived_root = ?, updated_at = ?
		WHERE status != 'archived' AND id IN (` + in + `)`
	if _, err := tx.Exec(q, append([]any{ts, root, ts}, args...)...); err != nil {
		return err
	}
	for _, c := range []string{
		`UPDATE questions SET status = 'dismissed', closed_at = ? WHERE status = 'open' AND task_id IN (` + in + `)`,
		`UPDATE agent_requests SET status = 'dismissed', closed_at = ? WHERE status = 'open' AND task_id IN (` + in + `)`,
	} {
		if _, err := tx.Exec(c, append([]any{ts}, args...)...); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`UPDATE access_requests SET dismissed = 1 WHERE dismissed = 0 AND task_id IN (`+in+`)`, args...); err != nil {
		return err
	}
	return tx.Commit()
}

// RestoreGroup readuce task-urile sterse impreuna cu root la statusul dinainte; in lucru -> in coada
// (run-ul a fost oprit). Intoarce task-urile restaurate (root primul).
func (s *Store) RestoreGroup(root int64) ([]Task, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`UPDATE tasks SET
			status = CASE archived_from WHEN 'in_progress' THEN 'queued' WHEN '' THEN 'queued' ELSE archived_from END,
			archived_from = '', archived_at = NULL, archived_root = NULL, updated_at = ?, nudged_at = NULL
		WHERE archived_root = ? AND status = 'archived'`, now(), root); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.TaskTree(root)
}

// ListArchivedRoots: task-urile pe care utilizatorul le-a sters (radacinile grupurilor), cele mai noi primele.
func (s *Store) ListArchivedRoots(projectID int64) ([]ArchivedGroup, error) {
	rows, err := s.db.Query(`SELECT `+taskCols+`,
			(SELECT COUNT(*) FROM tasks x WHERE x.archived_root = tasks.id AND x.id != tasks.id)
		FROM tasks WHERE project_id = ? AND status = 'archived' AND archived_root = id
		ORDER BY archived_at DESC`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ArchivedGroup{}
	for rows.Next() {
		var g ArchivedGroup
		t, err := scanTaskWith(rows, &g.Subtasks)
		if err != nil {
			return nil, err
		}
		g.Task = t
		out = append(out, g)
	}
	return out, rows.Err()
}

func (s *Store) CountArchivedRoots(projectID int64) (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM tasks WHERE project_id = ? AND status = 'archived' AND archived_root = id`, projectID).Scan(&n)
	return n, err
}

// idList: "?, ?, ?" si argumentele pentru un IN (...).
func idList(ids []int64) (string, []any) {
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	return strings.TrimSuffix(strings.Repeat("?, ", len(ids)), ", "), args
}
```
`scanTaskWith(sc, extra ...any)`: o varianta a `scanTask` care scaneaza coloanele task-ului plus `extra`. Implementeaz-o in `tasks.go` refactorizand `scanTask` sa fie `func scanTask(sc scanner) (Task, error) { return scanTaskWith(sc) }`, iar `scanTaskWith` sa adauge `extra...` la finalul listei de destinatii din `Scan`.

Verifica in schema ca `access_requests` nu are `closed_at` (are doar `dismissed`) — de aceea are update separat.

- [ ] **Step 6: Ruleaza, trebuie sa treaca**

Run: `go test ./internal/store && go vet ./internal/store`
Expected: PASS

- [ ] **Step 7: Auditul interogarilor**

Run: `grep -rn "FROM tasks\|JOIN tasks\|ListTasks(\|Subtasks(\|GetTaskByNumber(" internal --include='*.go' | grep -v _test`
Pentru fiecare rezultat scrie in ledger o linie `Audit: <fisier:linie> — filtrat | nu conteaza (<motiv>)`. Asteptarea (verificata la scrierea planului):
- filtrate in acest task: ListTasks, Subtasks, ListFillIns, statsTasks, CountStageTasks, SaveStages, ListDescendantFiles, CountTaskFilesTree, notWaitingParent;
- nu conteaza: GetTask (garzile din team decid), statsRuns / statsSendBacks / statsChanges (munca a fost facuta, costul ramane), CountChangedFiles (pe task), ProjectFirstActivity, RecentLibraryUses, DeleteAgent;
- GetTaskByNumber: garda in Task 2 (`findTask`).
Orice alt rezultat: decide si noteaza.

- [ ] **Step 8: Commit**

```bash
git add internal/store
git commit -m "store: task-uri arhivate (migrarea 025), grup, restaurare, filtre"
```

---

### Task 2: team: ArchiveTask, RestoreTask, garzi

**Files:**
- Create: `internal/team/archive.go`
- Create: `internal/team/archive_test.go`
- Modify: `internal/team/tools.go:1098` (findTask), `internal/team/recurring.go:214` (openOccurrence)

**Interfaces:**
- Consumes: `store.TaskTree`, `store.ArchiveTasks`, `store.RestoreGroup`, `store.StatusArchived`.
- Produces:
  - `func (t *Team) ArchiveTask(taskID int64) (int, error)` — intoarce cate task-uri au fost arhivate (cu sub-task-urile)
  - `func (t *Team) RestoreTask(taskID int64) error`
  - `var ErrArchived = errors.New("the task was deleted")`; `type RestoreError string` (`Error()`), ex. `Restore T-3 first.`

- [ ] **Step 1: Testul care pica**

`internal/team/archive_test.go`:
```go
package team

import (
	"strings"
	"testing"
	"time"

	"foreman/internal/runner"
	"foreman/internal/store"
)

func TestArchiveStopsRunAndRestores(t *testing.T) {
	f := setup(t)
	task := f.task(t, "Build page", f.rook, store.StatusQueued)
	mustReconcile(t, f)
	run := f.fs.last(t, f.rook)

	n, err := f.tm.ArchiveTask(task.ID)
	if err != nil || n != 1 {
		t.Fatalf("%d %v", n, err)
	}
	if len(f.fs.stopped) != 1 || f.fs.stopped[0] != run.ID {
		t.Fatalf("run neoprit: %v", f.fs.stopped)
	}
	// procesul se termina dupa Stop: finish nu readuce task-ul
	f.fs.finish(run.ID, runner.Outcome{Status: store.RunStopped})
	if got, _ := f.st.GetTask(task.ID); got.Status != store.StatusArchived {
		t.Fatalf("finish a scos task-ul din arhiva: %s", got.Status)
	}
	if !f.hasMessage(f.rook.ID, "The user deleted "+task.Ref()) {
		t.Fatal("asignatul nu a fost anuntat")
	}
	// tool-urile MCP vad task-ul ca sters
	if _, err := f.tm.ReportDone(f.workerCaller(f.rook, 0), task.Ref(), "done"); err == nil || !strings.Contains(err.Error(), "deleted") {
		t.Fatalf("report_done pe sters: %v", err)
	}

	if err := f.tm.RestoreTask(task.ID); err != nil {
		t.Fatal(err)
	}
	if got, _ := f.st.GetTask(task.ID); got.Status != store.StatusQueued {
		t.Fatalf("restore: %s", got.Status)
	}
	events, _ := f.st.ListTaskEvents(task.ID)
	kinds := []string{}
	for _, e := range events {
		kinds = append(kinds, e.Kind)
	}
	if s := strings.Join(kinds, ","); !strings.Contains(s, "deleted") || !strings.Contains(s, "restored") {
		t.Fatalf("istoric: %s", s)
	}
}

func TestArchiveSubtaskUnblocksParent(t *testing.T) {
	f := setup(t)
	parent := f.task(t, "Parent", f.lead, store.StatusInProgress)
	sub := store.Task{ProjectID: f.p.ID, Title: "Sub", AssigneeID: f.rook.ID, CreatorID: &f.lead.ID, ParentTaskID: &parent.ID}
	if err := f.st.CreateTask(&sub); err != nil {
		t.Fatal(err)
	}
	if _, err := f.tm.ArchiveTask(sub.ID); err != nil {
		t.Fatal(err)
	}
	if subs, _ := f.st.Subtasks(parent.ID); len(subs) != 0 {
		t.Fatalf("sub-task-ul sters inca numarat: %+v", subs)
	}
	if !f.hasMessage(f.lead.ID, "The user deleted "+sub.Ref()) {
		t.Fatal("liderul nu a fost anuntat")
	}
	// parintele se arhiveaza separat; restaurarea sub-task-ului cere intai parintele
	if _, err := f.tm.ArchiveTask(parent.ID); err != nil {
		t.Fatal(err)
	}
	if err := f.tm.RestoreTask(sub.ID); err == nil || !strings.Contains(err.Error(), "Restore "+parent.Ref()+" first") {
		t.Fatalf("%v", err)
	}
}

func TestRecurringNotBlockedByDeletedOccurrence(t *testing.T) {
	f := setup(t)
	r := store.RecurringTask{ProjectID: f.p.ID, Title: "Scout", AssigneeID: &f.rook.ID, Kind: "every_hours", EveryHours: 1}
	if err := f.st.CreateRecurring(&r, time.Now()); err != nil {
		t.Fatal(err)
	}
	first, err := f.tm.RunRecurringNow(r.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.tm.ArchiveTask(first.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.tm.RunRecurringNow(r.ID); err != nil {
		t.Fatalf("aparitia stearsa blocheaza: %v", err)
	}
}
```
Verifica numele reale inainte de rulare: `RunStopped` (`grep -n "RunStopped" internal/store/*.go`), `CreateRecurring` si campurile `RecurringTask` (`grep -n "func (s \*Store) CreateRecurring\|type RecurringTask" -A12 internal/store/recurring.go`). Adapteaza doar apelurile, nu asteptarile.

- [ ] **Step 2: Ruleaza, trebuie sa pice**

Run: `go test ./internal/team -run 'Archive|Recurring.*Deleted'`
Expected: FAIL (`ArchiveTask undefined`)

- [ ] **Step 3: archive.go**

`internal/team/archive.go`:
```go
package team

import (
	"errors"
	"fmt"
	"log"

	"foreman/internal/store"
)

// ErrArchived: task-ul a fost sters de utilizator (tool-urile MCP primesc mesajul lui).
var ErrArchived = errors.New("the task was deleted")

// RestoreError: Restore refuzat (de ex. parintele e inca sters).
type RestoreError string

func (e RestoreError) Error() string { return string(e) }

// ArchiveTask sterge (arhiveaza) task-ul si sub-task-urile lui: run-urile active pe ele se opresc,
// asignatii si decidentul afla, iar intrebarile / cererile legate se inchid. Intoarce cate au plecat.
func (t *Team) ArchiveTask(taskID int64) (int, error) {
	t.work.Lock()
	defer t.work.Unlock()
	task, err := t.st.GetTask(taskID)
	if err != nil {
		return 0, err
	}
	if task.Status == store.StatusArchived {
		return 0, ErrArchived
	}
	tree, err := t.st.TaskTree(taskID)
	if err != nil {
		return 0, err
	}
	agents, err := t.st.ListAgents(task.ProjectID)
	if err != nil {
		return 0, err
	}
	ids := make([]int64, len(tree))
	inTree := map[int64]bool{}
	for i, x := range tree {
		ids[i] = x.ID
		inTree[x.ID] = true
	}
	// run-urile active pe task-urile din grup: le oprim (finish vede apoi un task arhivat si nu-l atinge)
	t.mu.Lock()
	var stop []string
	working := map[int64]bool{} // agentii care lucrau pe grup
	for agentID, ar := range t.active {
		if inTree[ar.taskID] {
			stop = append(stop, ar.runID)
			working[agentID] = true
		}
	}
	t.mu.Unlock()
	for _, id := range stop {
		if err := t.runs.Stop(id); err != nil {
			log.Printf("stopping run %s: %v", id, err)
		}
	}
	if err := t.st.ArchiveTasks(ids, taskID); err != nil {
		return 0, err
	}
	t.taskEvent(task, "deleted", actorUser, fmt.Sprintf("You deleted it (%d %s)", len(ids), plural(len(ids), "task", "tasks")))
	text := fmt.Sprintf("The user deleted %s: %s. Stop working on it.", task.Ref(), task.Title)
	for agentID := range working {
		t.note(task.ProjectID, agentID, text, true)
	}
	if d, ok := decider(task, agents, t.autonomyOf(task.ProjectID)); ok && !working[d.ID] {
		t.note(task.ProjectID, d.ID, fmt.Sprintf("The user deleted %s: %s. Do not wait for it.", task.Ref(), task.Title), true)
	}
	t.emit(task.ProjectID, "task", task)
	t.Kick(task.ProjectID)
	return len(ids), nil
}

// RestoreTask readuce grupul sters impreuna cu taskID (doar radacina grupului se restaureaza).
func (t *Team) RestoreTask(taskID int64) error {
	t.work.Lock()
	defer t.work.Unlock()
	task, err := t.st.GetTask(taskID)
	if err != nil {
		return err
	}
	if task.Status != store.StatusArchived || task.ArchivedRoot == nil || *task.ArchivedRoot != task.ID {
		return RestoreError(task.Ref() + " is not a deleted task")
	}
	if task.ParentTaskID != nil {
		if p, err := t.st.GetTask(*task.ParentTaskID); err == nil && p.Status == store.StatusArchived {
			return RestoreError("Restore " + p.Ref() + " first.")
		}
	}
	restored, err := t.st.RestoreGroup(taskID)
	if err != nil {
		return err
	}
	if len(restored) > 0 {
		t.taskEvent(restored[0], "restored", actorUser, "You restored it")
		t.emit(task.ProjectID, "task", restored[0])
	}
	t.Kick(task.ProjectID)
	return nil
}
```
Garzile:
- `findTask` (tools.go): dupa `GetTaskByNumber`, inainte de `return task, err`:
```go
	if err == nil && task.Status == store.StatusArchived {
		return store.Task{}, fmt.Errorf("%s was deleted by the user", task.Ref())
	}
```
- `ReportDone` foloseste `findTask` pentru `ref` nevid (deci testul trece prin garda). Pentru `ref` gol (task-ul curent) verificarea existenta `task.Status != StatusInProgress` refuza deja; inlocuieste mesajul ei, doar pentru arhivat:
```go
	if task.Status == store.StatusArchived {
		return "", fmt.Errorf("%s was deleted by the user", task.Ref())
	}
```
  pus inainte de `if task.Status != store.StatusInProgress` din `ReportDone` (tools.go ~linia 853).
- `openOccurrence` (recurring.go): `return task, task.Status != store.StatusDone && task.Status != store.StatusArchived`.

- [ ] **Step 4: Ruleaza**

Run: `go test ./internal/team && go vet ./internal/team`
Expected: PASS (toata suita team: filtrele din Task 1 nu trebuie sa strice nimic)

- [ ] **Step 5: Commit**

```bash
git add internal/team/archive.go internal/team/archive_test.go internal/team/tools.go internal/team/recurring.go
git commit -m "team: stergere (arhivare) cu oprirea run-ului, Restore, garzi pentru task-urile sterse"
```

---

### Task 3: team: MoveTask

**Files:**
- Create: `internal/team/move.go`
- Create: `internal/team/move_test.go`
- Modify: `internal/team/stages.go:313-370` (UserDecide -> wrapper peste `decideAsUser`), `internal/team/reconcile.go:741-785` (resolveBlocked -> wrapper peste `resolveBlockedLocked`)

**Interfaces:**
- Consumes: `rejectTask`, `sendToStage`, `afterDone`, `decider`, `decisionOwner`, `placeholder`, `stageIndex`, `agentName`, `t.note`, `t.taskMsg`.
- Produces:
  - `func (t *Team) MoveTask(taskID int64, to, reason string) error`
  - `type MoveError string` (`Error()`), refuzurile cu mesaj pentru utilizator
  - `var ErrReasonRequired = errors.New("write why (the agent reads it)")`

- [ ] **Step 1: Testul care pica**

`internal/team/move_test.go`:
```go
package team

import (
	"errors"
	"strings"
	"testing"

	"foreman/internal/store"
)

func TestMoveTable(t *testing.T) {
	f := setup(t)

	// Queued -> Done = Close
	q := f.task(t, "Close me", f.rook, store.StatusQueued)
	if err := f.tm.MoveTask(q.ID, "done", ""); err != nil {
		t.Fatal(err)
	}
	if got, _ := f.st.GetTask(q.ID); got.Status != store.StatusDone || !strings.Contains(got.Result, "Closed by the user") {
		t.Fatalf("close: %+v", got)
	}

	// Done -> Queued = Reopen (motiv obligatoriu)
	if err := f.tm.MoveTask(q.ID, "queued", " "); !errors.Is(err, ErrReasonRequired) {
		t.Fatalf("reopen fara motiv: %v", err)
	}
	if err := f.tm.MoveTask(q.ID, "queued", "Missing the footer"); err != nil {
		t.Fatal(err)
	}
	got, _ := f.st.GetTask(q.ID)
	if got.Status != store.StatusQueued || got.Feedback != "Missing the footer" || got.AssigneeID != f.rook.ID {
		t.Fatalf("reopen: %+v", got)
	}

	// In progress: refuzat in ambele directii
	ip := f.task(t, "Busy", f.rook, store.StatusInProgress)
	for _, to := range []string{"done", "queued"} {
		var me MoveError
		if err := f.tm.MoveTask(ip.ID, to, "x"); !errors.As(err, &me) {
			t.Fatalf("%s: %v", to, err)
		}
	}

	// Review decis de lider: utilizatorul trece peste el
	rv := f.task(t, "Review me", f.rook, store.StatusReview)
	if err := f.tm.MoveTask(rv.ID, "done", ""); err != nil {
		t.Fatal(err)
	}
	if got, _ := f.st.GetTask(rv.ID); got.Status != store.StatusDone {
		t.Fatalf("accept peste lider: %s", got.Status)
	}
	events, _ := f.st.ListTaskEvents(rv.ID)
	if last := events[len(events)-1]; !strings.Contains(last.Text, "instead of "+f.lead.Name) {
		t.Fatalf("eveniment: %+v", last)
	}

	// Review -> Queued = Send back, motiv obligatoriu
	rv2 := f.task(t, "Send me back", f.rook, store.StatusReview)
	if err := f.tm.MoveTask(rv2.ID, "queued", ""); !errors.Is(err, ErrReasonRequired) {
		t.Fatalf("send back fara motiv: %v", err)
	}
	if err := f.tm.MoveTask(rv2.ID, "queued", "Use the brand colors"); err != nil {
		t.Fatal(err)
	}
	if got, _ := f.st.GetTask(rv2.ID); got.Status != store.StatusQueued || got.Feedback != "Use the brand colors" {
		t.Fatalf("send back: %+v", got)
	}

	// Blocked -> Queued = Unblock; Blocked -> Done = Accept
	b1 := f.task(t, "Blocked 1", f.rook, store.StatusBlocked)
	if err := f.tm.MoveTask(b1.ID, "queued", ""); err != nil {
		t.Fatal(err)
	}
	if got, _ := f.st.GetTask(b1.ID); got.Status != store.StatusQueued {
		t.Fatalf("unblock: %s", got.Status)
	}
	b2 := f.task(t, "Blocked 2", f.rook, store.StatusBlocked)
	if err := f.tm.MoveTask(b2.ID, "done", ""); err != nil {
		t.Fatal(err)
	}
	if got, _ := f.st.GetTask(b2.ID); got.Status != store.StatusDone {
		t.Fatalf("accept blocat: %s", got.Status)
	}

	// tinta necunoscuta, task sters
	var me MoveError
	if err := f.tm.MoveTask(b2.ID, "review", ""); !errors.As(err, &me) {
		t.Fatalf("tinta review: %v", err)
	}
	if _, err := f.tm.ArchiveTask(q.ID); err != nil {
		t.Fatal(err)
	}
	if err := f.tm.MoveTask(q.ID, "done", ""); !errors.As(err, &me) {
		t.Fatalf("mutare pe sters: %v", err)
	}
}

func TestMoveBetweenStages(t *testing.T) {
	f := setup(t)
	stages, err := f.st.SaveStages(f.p.ID, []store.Stage{
		{Name: "Build", Kind: store.KindWork, AgentIDs: []int64{f.rook.ID}},
		{Name: "QA", Kind: store.KindCheck, AgentIDs: []int64{f.vera.ID}},
	})
	if err != nil {
		t.Fatal(err)
	}
	task := store.Task{ProjectID: f.p.ID, Title: "Page", AssigneeID: f.rook.ID, CreatorID: &f.lead.ID, StageID: &stages[0].ID}
	if err := f.st.CreateTask(&task); err != nil {
		t.Fatal(err)
	}
	if err := f.tm.MoveTask(task.ID, "stage:"+itoa(stages[1].ID), ""); err != nil {
		t.Fatal(err)
	}
	got, _ := f.st.GetTask(task.ID)
	if got.StageID == nil || *got.StageID != stages[1].ID || got.Status != store.StatusQueued {
		t.Fatalf("%+v", got)
	}
	var me MoveError
	if err := f.tm.MoveTask(task.ID, "stage:"+itoa(stages[1].ID), ""); !errors.As(err, &me) {
		t.Fatalf("aceeasi etapa: %v", err)
	}
	if err := f.tm.MoveTask(task.ID, "stage:999999", ""); !errors.As(err, &me) {
		t.Fatalf("etapa inexistenta: %v", err)
	}
}
```
Daca `itoa` nu exista in testele team (`grep -n "func itoa" internal/team/*_test.go`), adauga in `move_test.go`: `func itoa(n int64) string { return strconv.FormatInt(n, 10) }` (+ import `strconv`). Verifica si `store.KindWork` / `store.KindCheck`.

- [ ] **Step 2: Ruleaza, trebuie sa pice**

Run: `go test ./internal/team -run 'TestMove'`
Expected: FAIL (`MoveTask undefined`)

- [ ] **Step 3: Refactor fara lock**

`stages.go`: `UserDecide` ramane semnatura publica, devine:
```go
func (t *Team) UserDecide(taskID int64, accept bool, feedback string) error {
	t.work.Lock()
	defer t.work.Unlock()
	task, err := t.st.GetTask(taskID)
	if err != nil {
		return err
	}
	agents, err := t.st.ListAgents(task.ProjectID)
	if err != nil {
		return err
	}
	if task.Status != store.StatusReview || task.ReviewStage != store.StageLead || decisionOwner(task, agents) != 0 {
		return ErrNotUserDecision
	}
	return t.decideAsUser(task, agents, accept, feedback, "")
}
```
iar corpul de dupa verificare (accept / send back) se muta neschimbat in `func (t *Team) decideAsUser(task store.Task, agents []store.Agent, accept bool, feedback, instead string) error` (se apeleaza cu `t.work` blocat), cu doua schimbari:
- acceptul seteaza si `task.ReviewStage = store.StageNone` (deja) — functioneaza si pentru Review la reviewer;
- textele evenimentelor: daca `instead != ""`, `"You accepted (instead of " + instead + ")"` si `"You sent it back (instead of " + instead + "): " + feedback`; altfel textele de azi.
Dupa accept sau send back, cand `instead != ""`, nota catre acel decident: `t.note(task.ProjectID, ownerID, "The user decided "+task.Ref()+" for you: accepted."/"sent back: "+feedback, false)` — pentru asta `decideAsUser` primeste numele si cauta agentul cu `store.FindAgent` dupa ID; simplifica: semnatura `decideAsUser(task, agents, accept, feedback string, owner int64)` si numele se ia din `agentName(agents, owner)` cand `owner != 0`.

`reconcile.go`: `resolveBlocked(taskID, accept)` devine wrapper: lock + `GetTask` + `ListAgents` + `t.resolveBlockedLocked(task, agents, accept)`; corpul de azi (de la `if task.Status != store.StatusBlocked`) se muta in `resolveBlockedLocked`.

Run: `go test ./internal/team` — Expected: PASS (refactorul nu schimba comportamentul; noile teste pica inca pe `MoveTask`).

- [ ] **Step 4: move.go**

`internal/team/move.go`:
```go
package team

import (
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"

	"foreman/internal/store"
)

// MoveError: mutare refuzata, cu motivul pentru utilizator.
type MoveError string

func (e MoveError) Error() string { return string(e) }

var ErrReasonRequired = errors.New("write why (the agent reads it)")

// MoveTask aplica actiunea coloanei pe care utilizatorul a tras task-ul: "queued", "done" sau "stage:<id>".
func (t *Team) MoveTask(taskID int64, to, reason string) error {
	t.work.Lock()
	defer t.work.Unlock()
	task, err := t.st.GetTask(taskID)
	if err != nil {
		return err
	}
	if task.Status == store.StatusArchived {
		return MoveError(task.Ref() + " was deleted.")
	}
	if task.Status == store.StatusInProgress {
		return MoveError(fmt.Sprintf("An agent is working on %s; wait for it to finish.", task.Ref()))
	}
	agents, err := t.st.ListAgents(task.ProjectID)
	if err != nil {
		return err
	}
	reason = strings.TrimSpace(reason)
	switch {
	case to == "done":
		return t.moveToDone(task, agents, reason)
	case to == "queued":
		return t.moveToQueue(task, agents, reason)
	case strings.HasPrefix(to, "stage:"):
		id, err := strconv.ParseInt(strings.TrimPrefix(to, "stage:"), 10, 64)
		if err != nil {
			return MoveError("Unknown column.")
		}
		return t.moveToStage(task, agents, id, reason)
	}
	return MoveError("Tasks can't be moved there.")
}

func (t *Team) moveToDone(task store.Task, agents []store.Agent, reason string) error {
	switch task.Status {
	case store.StatusReview:
		return t.decideAsUser(task, agents, true, "", decisionOwner(task, agents))
	case store.StatusBlocked:
		return t.resolveBlockedLocked(task, agents, true)
	case store.StatusQueued:
		return t.closeTask(task, agents, reason)
	}
	return MoveError(task.Ref() + " is already done.")
}

func (t *Team) moveToQueue(task store.Task, agents []store.Agent, reason string) error {
	switch task.Status {
	case store.StatusReview:
		if reason == "" {
			return ErrReasonRequired
		}
		return t.decideAsUser(task, agents, false, reason, decisionOwner(task, agents))
	case store.StatusBlocked:
		return t.resolveBlockedLocked(task, agents, false)
	case store.StatusDone:
		if reason == "" {
			return ErrReasonRequired
		}
		return t.reopenTask(task, agents, nil, reason)
	}
	return MoveError(task.Ref() + " is already queued.")
}

// moveToStage: un task in asteptare / blocat trece in alta etapa (il ia primul liber de acolo);
// unul Done se redeschide in etapa aceea (cu motiv).
func (t *Team) moveToStage(task store.Task, agents []store.Agent, stageID int64, reason string) error {
	stages, err := t.st.ListStages(task.ProjectID)
	if err != nil {
		return err
	}
	i := stageIndex(stages, &stageID)
	if i < 0 {
		return MoveError("That column no longer exists.")
	}
	st := stages[i]
	switch task.Status {
	case store.StatusDone:
		if reason == "" {
			return ErrReasonRequired
		}
		return t.reopenTask(task, agents, &st, reason)
	case store.StatusQueued, store.StatusBlocked:
	default:
		return MoveError(fmt.Sprintf("%s is waiting for a decision; accept it or send it back.", task.Ref()))
	}
	if task.StageID != nil && *task.StageID == st.ID {
		return MoveError(fmt.Sprintf("%s is already in %s.", task.Ref(), st.Name))
	}
	task.StageID = &st.ID
	task.AssigneeID = placeholder(st, task.AssigneeID)
	task.Status, task.SendBacks, task.Escalated = store.StatusQueued, 0, false
	if reason != "" {
		task.Feedback = reason
	}
	if err := t.st.SaveTask(&task); err != nil {
		return err
	}
	t.taskEvent(task, "stage", actorUser, "You moved it to "+st.Name)
	if d, ok := decider(task, agents, t.autonomyOf(task.ProjectID)); ok {
		t.note(task.ProjectID, d.ID, fmt.Sprintf("The user moved %s to stage %s.", task.Ref(), st.Name), true)
	}
	t.emit(task.ProjectID, "task", task)
	t.Kick(task.ProjectID)
	return nil
}

// closeTask: utilizatorul inchide un task din coada fara lucru.
func (t *Team) closeTask(task store.Task, agents []store.Agent, reason string) error {
	task.Status, task.ReviewStage, task.StageID = store.StatusDone, store.StageNone, nil
	task.Result = "Closed by the user."
	if reason != "" {
		task.Result += " " + reason
	}
	if err := t.st.SaveTask(&task); err != nil {
		return err
	}
	t.taskMsg(task, "closed", actorUser, agentName(agents, task.AssigneeID), "You closed it", task.Result)
	t.note(task.ProjectID, task.AssigneeID, fmt.Sprintf("The user closed %s; do not work on it.", task.Ref()), true)
	if d, ok := decider(task, agents, t.autonomyOf(task.ProjectID)); ok && d.ID != task.AssigneeID {
		t.note(task.ProjectID, d.ID, fmt.Sprintf("The user closed %s without work.", task.Ref()), true)
	}
	t.emit(task.ProjectID, "task", task)
	if err := t.afterDone(task); err != nil {
		log.Printf("after close %s: %v", task.Ref(), err)
	}
	t.Kick(task.ProjectID)
	return nil
}

// reopenTask: un task Done revine in coada, la acelasi asignat (sau in etapa st), cu motivul ca feedback.
func (t *Team) reopenTask(task store.Task, agents []store.Agent, st *store.Stage, reason string) error {
	task.Status, task.ReviewStage, task.SendBacks, task.Escalated = store.StatusQueued, store.StageNone, 0, false
	task.Feedback = reason
	if st != nil {
		task.StageID = &st.ID
		task.AssigneeID = placeholder(*st, task.AssigneeID)
	}
	if err := t.st.SaveTask(&task); err != nil {
		return err
	}
	t.taskMsg(task, "reopened", actorUser, agentName(agents, task.AssigneeID), "You reopened it: "+reason, reason)
	t.note(task.ProjectID, task.AssigneeID, fmt.Sprintf("The user reopened %s: %s", task.Ref(), reason), true)
	if d, ok := decider(task, agents, t.autonomyOf(task.ProjectID)); ok && d.ID != task.AssigneeID {
		t.note(task.ProjectID, d.ID, fmt.Sprintf("The user reopened %s: %s", task.Ref(), reason), true)
	}
	t.emit(task.ProjectID, "task", task)
	t.Kick(task.ProjectID)
	return nil
}
```
Verifica semnatura `placeholder` (`grep -n "func placeholder" internal/team/*.go`) si `t.note` (intoarce error: ignor-o ca in restul codului sau logheaz-o, ca apelurile existente).

- [ ] **Step 5: Ruleaza**

Run: `go test ./internal/team && go vet ./internal/team`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/team/move.go internal/team/move_test.go internal/team/stages.go internal/team/reconcile.go
git commit -m "team: MoveTask (Accept, Send back, Unblock, Reopen, Close, mutare intre etape)"
```

---

### Task 4: web: endpoint-uri

**Files:**
- Create: `internal/web/api_task_move.go`
- Create: `internal/web/task_move_test.go`
- Modify: `internal/web/server.go` (rute), `internal/web/api_projects.go` (`projectDetail.Archived`)

**Interfaces:**
- Consumes: `team.MoveTask`, `team.ArchiveTask`, `team.RestoreTask`, `team.MoveError`, `team.ErrReasonRequired`, `team.ErrArchived`, `team.RestoreError`, `store.ListArchivedRoots`, `store.CountArchivedRoots`.
- Produces (pentru Task 5):
  - `POST /api/tasks/{id}/move` `{to, reason}` -> 204 | 409 `{error}` (MoveError) | 400 (ErrReasonRequired)
  - `DELETE /api/tasks/{id}` -> 200 `{"deleted": N}` | 409 (deja sters)
  - `POST /api/tasks/{id}/restore` -> 204 | 409 (RestoreError)
  - `GET /api/projects/{id}/archived` -> `{"tasks": [ArchivedGroup...]}`
  - vederea proiectului primeste `"archived": N`

- [ ] **Step 1: Testul care pica**

`internal/web/task_move_test.go`:
```go
package web

import (
	"net/http"
	"strings"
	"testing"

	"foreman/internal/store"
)

func TestMoveDeleteRestoreAPI(t *testing.T) {
	e := setup(t)
	p := e.createProject(t, t.TempDir())
	w := e.addWorker(t, p.ID, "Rook")
	task := store.Task{ProjectID: p.ID, Title: "Page", AssigneeID: w.ID}
	if err := e.st.CreateTask(&task); err != nil {
		t.Fatal(err)
	}
	id := itoa(task.ID)

	if code, raw := e.do(t, "POST", "/api/tasks/"+id+"/move", map[string]any{"to": "done"}); code != http.StatusNoContent {
		t.Fatalf("close: %d %s", code, raw)
	}
	if code, _ := e.do(t, "POST", "/api/tasks/"+id+"/move", map[string]any{"to": "queued"}); code != http.StatusBadRequest {
		t.Fatalf("reopen fara motiv: %d", code)
	}
	if code, _ := e.do(t, "POST", "/api/tasks/"+id+"/move", map[string]any{"to": "review"}); code != http.StatusConflict {
		t.Fatalf("tinta nepermisa: %d", code)
	}

	code, raw := e.do(t, "DELETE", "/api/tasks/"+id, nil)
	if code != http.StatusOK || !strings.Contains(string(raw), `"deleted":1`) {
		t.Fatalf("delete: %d %s", code, raw)
	}
	if code, _ := e.do(t, "DELETE", "/api/tasks/"+id, nil); code != http.StatusConflict {
		t.Fatalf("delete de doua ori: %d", code)
	}
	code, raw = e.do(t, "GET", "/api/projects/"+itoa(p.ID), nil)
	if code != 200 || !strings.Contains(string(raw), `"archived":1`) || strings.Contains(string(raw), `"title":"Page"`) {
		t.Fatalf("vederea proiectului: %s", raw)
	}
	code, raw = e.do(t, "GET", "/api/projects/"+itoa(p.ID)+"/archived", nil)
	if code != 200 || !strings.Contains(string(raw), `"title":"Page"`) {
		t.Fatalf("lista sterse: %d %s", code, raw)
	}
	if code, raw := e.do(t, "POST", "/api/tasks/"+id+"/restore", nil); code != http.StatusNoContent {
		t.Fatalf("restore: %d %s", code, raw)
	}
	if code, _ := e.do(t, "POST", "/api/tasks/"+id+"/restore", nil); code != http.StatusConflict {
		t.Fatalf("restore de doua ori: %d", code)
	}
}
```

- [ ] **Step 2: Ruleaza, trebuie sa pice**

Run: `go test ./internal/web -run TestMoveDeleteRestoreAPI`
Expected: FAIL (404 / 405 pe `/move`)

- [ ] **Step 3: Implementarea**

`internal/web/api_task_move.go`:
```go
package web

import (
	"errors"
	"net/http"

	"foreman/internal/team"
)

// moveTask: drag & drop pe board (sau "Move to…"): actiunea coloanei tinta.
func (s *Server) moveTask(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		fail(w, err)
		return
	}
	var in struct {
		To     string `json:"to"`
		Reason string `json:"reason"`
	}
	if err := decode(r, &in); err != nil {
		fail(w, err)
		return
	}
	err = s.team.MoveTask(id, in.To, in.Reason)
	var me team.MoveError
	switch {
	case errors.As(err, &me):
		writeErr(w, http.StatusConflict, me.Error())
	case errors.Is(err, team.ErrReasonRequired):
		fail(w, badRequest(err.Error()))
	case err != nil:
		fail(w, err)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

// deleteTask: stergerea din board = arhivare (Restore o anuleaza).
func (s *Server) deleteTask(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		fail(w, err)
		return
	}
	n, err := s.team.ArchiveTask(id)
	switch {
	case errors.Is(err, team.ErrArchived):
		writeErr(w, http.StatusConflict, "the task is already deleted")
	case err != nil:
		fail(w, err)
	default:
		writeJSON(w, http.StatusOK, map[string]int{"deleted": n})
	}
}

func (s *Server) restoreTask(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		fail(w, err)
		return
	}
	err = s.team.RestoreTask(id)
	var re team.RestoreError
	switch {
	case errors.As(err, &re):
		writeErr(w, http.StatusConflict, re.Error())
	case err != nil:
		fail(w, err)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

// listArchived: panoul "Deleted" de pe board.
func (s *Server) listArchived(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		fail(w, err)
		return
	}
	list, err := s.st.ListArchivedRoots(id)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"tasks": list})
}
```
`server.go`, langa rutele de task:
```go
	mux.HandleFunc("POST /api/tasks/{id}/move", s.moveTask)
	mux.HandleFunc("DELETE /api/tasks/{id}", s.deleteTask)
	mux.HandleFunc("POST /api/tasks/{id}/restore", s.restoreTask)
	mux.HandleFunc("GET /api/projects/{id}/archived", s.listArchived)
```
Verifica ca nu exista deja o ruta `DELETE /api/tasks/{id}` (`grep -n '"DELETE /api/tasks' internal/web/server.go`) si ca middleware-ul de CSRF / content-type din `server.go` (~linia 196: "si fara body (stop/resume/accept)") accepta `DELETE` fara body; daca nu, adauga-l in aceeasi lista.

`api_projects.go`: in `projectDetail` campul `Archived int \`json:"archived"\` // task-uri sterse (radacini), pentru "Deleted (N)"`, umplut cu `s.st.CountArchivedRoots(p.ID)` langa celelalte numaratori (cu verificarea erorii ca restul).

- [ ] **Step 4: Ruleaza**

Run: `go test ./internal/web && go vet ./internal/web`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/web/api_task_move.go internal/web/task_move_test.go internal/web/server.go internal/web/api_projects.go
git commit -m "web: move, delete, restore si lista task-urilor sterse"
```

---

### Task 5: UI: drag & drop, meniul cardului, Delete, panoul Deleted

**Files:**
- Create: `internal/web/static/js/board-move.js`
- Create: `internal/web/static/js/deleted.js`
- Modify: `internal/web/static/js/workspace.js` (column ~764, taskCard ~1050, toolbar ~720, panouri ~910-930, SUB_STATUS ~18)
- Modify: `internal/web/static/js/task-modal.js` (STATUS ~8, zona de actiuni: buton Delete)
- Modify: `internal/web/static/js/ui.js:133` (toast cu actiune)
- Modify: `internal/web/static/app.css`

**Interfaces:**
- Consumes: API-urile din Task 4; `t.status`, `t.stageId`, `project.stages`, `project.archived`.
- Produces:
  - `export function moveAction(task, target)` -> `{ ok: true, label, needsReason }` | `{ ok: false, why }`; `target` = `"queued" | "done" | "stage:<id>" | null`
  - `export async function moveTask(task, target, askReason)` -> true / false (cere motivul cand trebuie, apeleaza API-ul, toast la eroare)
  - `export function deletedPanel({ projectId, onClose, onChanged })` -> `{ el }`
  - `toast(message, kind = "error", action = null)`; `action = { label, onClick }`

- [ ] **Step 1: moveAction (logica, testata in node)**

`internal/web/static/js/board-move.js`:
```js
import { api } from "./api.js";
import { toast } from "./ui.js";

// moveAction: ce inseamna sa lasi task-ul pe coloana target (aceleasi reguli ca team.MoveTask).
// target: "queued" | "done" | "stage:<id>" | null (coloana nu primeste carduri).
export function moveAction(t, target) {
  const ref = `T-${t.number}`;
  if (!target) return { ok: false, why: "Tasks get here only through the agents' work." };
  if (t.status === "in_progress") return { ok: false, why: `An agent is working on ${ref}; wait for it to finish.` };
  if (target === "done") {
    if (t.status === "review") return { ok: true, label: "Accept" };
    if (t.status === "blocked") return { ok: true, label: "Accept" };
    if (t.status === "queued") return { ok: true, label: "Close" };
    return { ok: false, why: `${ref} is already done.` };
  }
  if (target === "queued") {
    if (t.status === "review") return { ok: true, label: "Send back", needsReason: true };
    if (t.status === "blocked") return { ok: true, label: "Unblock" };
    if (t.status === "done") return { ok: true, label: "Reopen", needsReason: true };
    return { ok: false, why: `${ref} is already queued.` };
  }
  if (target.startsWith("stage:")) {
    const id = Number(target.slice(6));
    if (t.status === "done") return { ok: true, label: "Reopen", needsReason: true };
    if (t.status !== "queued" && t.status !== "blocked") return { ok: false, why: `${ref} is waiting for a decision; accept it or send it back.` };
    if (t.stageId === id) return { ok: false, why: `${ref} is already here.` };
    return { ok: true, label: "Move" };
  }
  return { ok: false, why: "Tasks can't be moved there." };
}

// moveTask: cere motivul daca trebuie (askReason(label) -> text sau null la Cancel), apoi muta.
export async function moveTask(t, target, askReason) {
  const a = moveAction(t, target);
  if (!a.ok) { toast(a.why); return false; }
  let reason = "";
  if (a.needsReason) {
    reason = await askReason(a.label, t);
    if (reason == null) return false;
  }
  try {
    await api(`/api/tasks/${t.id}/move`, { method: "POST", body: { to: target, reason } });
    return true;
  } catch (err) {
    toast(err.message);
    return false;
  }
}
```
Test rapid al logicii, fara browser (fisier temporar, nu in repo):
```bash
mkdir -p /tmp/bm && cp internal/web/static/js/board-move.js /tmp/bm/board-move.mjs
printf 'export const api=()=>{};\n' > /tmp/bm/api.js
printf 'export const toast=()=>{};\n' > /tmp/bm/ui.js
sed -i '' 's#"./api.js"#"./api.js"#' /tmp/bm/board-move.mjs
cat > /tmp/bm/t.mjs <<'EOF'
import { moveAction as m } from "./board-move.mjs";
const eq = (a, b) => { if (JSON.stringify(a) !== JSON.stringify(b)) { console.error("FAIL", a, b); process.exit(1); } };
eq(m({ number: 1, status: "queued" }, "done").label, "Close");
eq(m({ number: 1, status: "done" }, "queued").needsReason, true);
eq(m({ number: 1, status: "in_progress" }, "done").ok, false);
eq(m({ number: 1, status: "review" }, "queued").label, "Send back");
eq(m({ number: 1, status: "blocked" }, "queued").label, "Unblock");
eq(m({ number: 1, status: "queued", stageId: 4 }, "stage:4").ok, false);
eq(m({ number: 1, status: "queued", stageId: 3 }, "stage:4").label, "Move");
eq(m({ number: 1, status: "queued" }, null).ok, false);
console.log("ok");
EOF
node /tmp/bm/t.mjs
```
Expected: `ok`. Scrie-l intai cu `moveAction` lipsa (fisierul doar cu importurile) ca sa-l vezi picand, apoi cu codul de mai sus.

- [ ] **Step 2: toast cu actiune**

`ui.js`, `toast` devine:
```js
export function toast(message, kind = "error", action = null) {
  const box = document.getElementById("toast");
  box.replaceChildren(document.createTextNode(message));
  if (action) {
    const b = document.createElement("button");
    b.type = "button";
    b.className = "toast-action";
    b.textContent = action.label;
    b.addEventListener("click", () => { box.hidden = true; action.onClick(); });
    box.append(" ", b);
  }
  box.classList.toggle("ok", kind === "ok");
  box.hidden = false;
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => { box.hidden = true; }, action ? 8000 : 5000);
}
```

- [ ] **Step 3: Board: tinta coloanelor, drag & drop, meniu**

In `workspace.js`:
- import: `import { moveAction, moveTask } from "./board-move.js";` si `import { deletedPanel } from "./deleted.js";`
- `SUB_STATUS` primeste `archived: "Deleted"`.
- `column(title, items, byId, stage, opts)` primeste `opts.target` (string sau null). Pe `div.col` adauga:
```js
      "data-target": opts.target || "",
      ondragover: (e) => { if (dragging && moveAction(dragging, opts.target || null).ok) e.preventDefault(); },
      ondrop: async (e) => {
        e.preventDefault();
        const t = dragging;
        endDrag();
        if (t && await moveTask(t, opts.target || null, askReason)) reload();
      },
```
- apelurile `column(...)`:
  - fara etape: `COLUMNS.map((c, i) => column(c.title, inCol("c" + i), byId, null, { add: i === 0, page: i === COLUMNS.length - 1, target: i === 0 ? "queued" : i === COLUMNS.length - 1 ? "done" : null }))`
  - cu etape: "No stage" `{ target: null }`; etapele `{ add: i === 0, target: "stage:" + s.id }`; "With lead" `{ target: null }`; "Done" `{ page: true, target: "done" }`.
- starea drag-ului, langa celelalte variabile ale board-ului:
```js
  let dragging = null; // task-ul tras acum (null = niciunul)
  // startDrag: coloanele arata unde se poate lasa cardul (drop-ok) si unde nu (drop-no, cu motivul)
  function startDrag(t) {
    dragging = t;
    for (const col of board.querySelectorAll(".col")) {
      const a = moveAction(t, col.dataset.target || null);
      col.classList.add(a.ok ? "drop-ok" : "drop-no");
      col.title = a.ok ? a.label : a.why;
    }
  }
  function endDrag() {
    dragging = null;
    for (const col of board.querySelectorAll(".col")) { col.classList.remove("drop-ok", "drop-no"); col.removeAttribute("title"); }
  }
```
- `taskCard(t, ...)`: butonul cardului primeste `draggable: "true"`, `ondragstart: (e) => { e.dataTransfer.effectAllowed = "move"; e.dataTransfer.setData("text/plain", String(t.id)); startDrag(t); }`, `ondragend: endDrag`. Cardul e un `<button>`; un buton "⋯" nu poate sta in alt buton, deci in `groupCard` (unde se construieste cardul, ~linia 875) cardul se pune intr-un `div.card-wrap` cu `position: relative`, iar butonul `⋯` (clasa `card-menu-btn`, `aria-label: "Task actions"`) sta absolut in coltul din dreapta-sus, langa card. Click pe `⋯` deschide un meniu (`div.card-menu` cu `role="menu"`) cu:
  - "Move to…" -> sub-lista cu coloanele board-ului pentru care `moveAction(t, target).ok` (eticheta: `${label} → ${titlu coloana}`); click = `moveTask(...)` apoi `reload()`;
  - "Delete" -> `deleteTask(t)` (mai jos).
  Meniul se inchide la Esc, la click in afara si dupa o alegere (un singur meniu deschis odata).
- `askReason(label, t)`: o fereastra mica (`dialog` simplu cu clasa `modal-overlay` existenta) cu titlul `${label} T-${t.number}`, `textarea` (placeholder `What should change? The agent reads this.`), butoanele Cancel / `${label}`; intoarce un Promise cu textul (trimis doar nevid) sau `null`.
- `deleteTask(t)` (folosit de meniu si de modala):
```js
  async function deleteTask(t) {
    const subs = (state.project.tasks || []).filter((x) => x.parentTaskId === t.id).length;
    const busy = (state.project.agents || []).some((a) => a.workingTaskId === t.id);
    let msg = `Delete T-${t.number}` + (subs ? ` and its ${subs} ${subs === 1 ? "subtask" : "subtasks"}` : "") + "? You can restore it from Deleted.";
    if (busy) msg += "\nAn agent is working on it; the run will be stopped.";
    if (!confirm(msg)) return false;
    try {
      await api(`/api/tasks/${t.id}`, { method: "DELETE" });
    } catch (err) { toast(err.message); return false; }
    toast(`Deleted T-${t.number}`, "ok", { label: "Undo", onClick: async () => {
      try { await api(`/api/tasks/${t.id}/restore`, { method: "POST" }); reload(); } catch (err) { toast(err.message); }
    } });
    reload();
    return true;
  }
```
  Verifica numele reale ale campurilor (`state.project.agents`, `workingTaskId` — vezi `agentView` in api_projects.go) si ale functiei de reincarcare (`reload` / `renderBoard`) inainte; foloseste ce exista. Subtask-urile se numara recursiv daca exista deja un helper (`taskGroups` are `subs`); altfel numarul copiilor directi e suficient pentru mesaj.
- Toolbar (`boardTools.replaceChildren(...)`): adauga primul `p.archived > 0 ? toggle("deleted", `Deleted (${p.archived})`, toggleDeleted) : null` (filtrat de null, cum spune comentariul existent).
- `toggleDeleted` dupa tiparul `toggleRecurring` (~linia 926): `setPanel("deleted", deletedPanel({ projectId, onClose: closePanel, onChanged: reload }).el)`; foloseste exact functiile de panou existente.
- Expune `deleteTask` catre modala prin obiectul `actions` dat lui `taskModal` (vezi comentariul de la `taskModal`: `actions` contine functiile din workspace): `actions.deleteTask = deleteTask`.

`internal/web/static/js/deleted.js`:
```js
import { api } from "./api.js";
import { el, toast } from "./ui.js";

function ago(iso) {
  const s = Math.max(0, (Date.now() - new Date(iso).getTime()) / 1000);
  if (s < 60) return "just now";
  if (s < 3600) return `${Math.floor(s / 60)} min ago`;
  if (s < 86400) return `${Math.floor(s / 3600)} h ago`;
  return new Date(iso).toLocaleDateString("en-US", { month: "short", day: "numeric" });
}

// deletedPanel: task-urile sterse din proiect, cu Restore (panou pe board, ca Recurring).
export function deletedPanel({ projectId, onClose, onChanged }) {
  const list = el("ul", { class: "deleted-list" }, el("li", { class: "hint" }, "Loading..."));
  const root = el("section", { class: "board-panel deleted-panel", "aria-label": "Deleted tasks" },
    el("div", { class: "panel-head" }, el("h3", {}, "Deleted tasks"),
      el("button", { type: "button", class: "btn ghost small", onclick: onClose }, "Close")),
    el("p", { class: "hint" }, "Deleted tasks are kept. Restore brings a task back to where it was, with its subtasks."),
    list);
  load();
  async function load() {
    try {
      const res = await api(`/api/projects/${projectId}/archived`);
      if (!res.tasks.length) { list.replaceChildren(el("li", { class: "hint" }, "Nothing deleted.")); return; }
      list.replaceChildren(...res.tasks.map((t) => el("li", { class: "deleted-row" },
        el("span", { class: "task-id" }, `T-${t.number}`),
        el("span", { class: "deleted-title" }, t.title),
        el("span", { class: "hint" }, [t.subtasks ? `${t.subtasks} ${t.subtasks === 1 ? "subtask" : "subtasks"} · ` : "", `deleted ${ago(t.archivedAt)}`].join("")),
        el("button", { type: "button", class: "btn small", onclick: async () => {
          try {
            await api(`/api/tasks/${t.id}/restore`, { method: "POST" });
            toast(`Restored T-${t.number}`, "ok");
            onChanged();
            load();
          } catch (err) { toast(err.message); }
        } }, "Restore"))));
    } catch (err) {
      list.replaceChildren(el("li", { class: "hint" }, `Could not load: ${err.message}`));
    }
  }
  return { el: root };
}
```
Clasele de panou (`board-panel`, `panel-head`): foloseste exact ce foloseste `recurring.js` (`grep -n "class:" internal/web/static/js/recurring.js | head`), nu inventa altele.

`task-modal.js`: `STATUS` primeste `archived: "Deleted"`; in zona de actiuni (`renderActions`), ultimul element: `el("button", { type: "button", class: "btn ghost small danger-hover", onclick: async () => { if (await actions.deleteTask(data.task)) onClose(); } }, "Delete")`, doar cand `data.task.status !== "archived"`.

`app.css`, la final:
```css
/* Drag & drop pe board: unde se poate lasa cardul */
.col.drop-ok { outline: 2px dashed var(--teal-line); outline-offset: -2px; background: var(--teal-bg); }
.col.drop-no { opacity: .55; }
.task[draggable="true"] { cursor: grab; }
.card-wrap { position: relative; }
.card-menu-btn { position: absolute; top: 6px; right: 6px; width: 26px; height: 26px; border-radius: 6px; border: 0; background: transparent; color: var(--muted); cursor: pointer; opacity: 0; }
.card-wrap:hover .card-menu-btn, .card-menu-btn:focus-visible, .card-menu-btn[aria-expanded="true"] { opacity: 1; }
.card-menu-btn:hover { background: var(--card-hi); color: var(--text); }
.card-menu { position: absolute; top: 34px; right: 6px; z-index: 20; min-width: 200px; padding: 4px; border-radius: 8px; background: var(--deep); border: 1px solid var(--line-2); display: grid; }
.card-menu button { text-align: left; background: none; border: 0; color: var(--soft); padding: 6px 10px; border-radius: 6px; font: inherit; cursor: pointer; }
.card-menu button:hover, .card-menu button:focus-visible { background: var(--card-hi); color: var(--text); }
.card-menu .danger { color: var(--red); }
.toast-action { margin-left: 8px; background: none; border: 1px solid currentColor; color: inherit; border-radius: 6px; padding: 2px 8px; font: inherit; cursor: pointer; }
.btn.danger-hover:hover { color: var(--red); }
.deleted-list { list-style: none; margin: 0; padding: 0; display: grid; gap: 6px; }
.deleted-row { display: flex; flex-wrap: wrap; align-items: center; gap: 8px; padding: 8px 0; border-bottom: 1px solid var(--line); }
.deleted-title { flex: 1 1 200px; min-width: 0; overflow-wrap: anywhere; }
@media (hover: none) { .card-menu-btn { opacity: 1; } }
```

- [ ] **Step 4: Sintaxa**

Run: copiaza `board-move.js`, `deleted.js`, `workspace.js`, `task-modal.js`, `ui.js` ca `.mjs` intr-un folder temporar (fisier cu fisier, cai literale) si `node --check` pe fiecare.
Expected: fara erori.

- [ ] **Step 5: Verificare vizuala**

Instanta de test izolata (alt `HOME`, port 7790, echipa oprita cu `POST /api/projects/{id}/stop` inainte de a crea task-uri), un proiect cu un task in fiecare stare (Queued, Review creat de utilizator, Blocked, Done) — starile se pun cu `sqlite3` dupa creare, ca in verificarea pentru fisierele pe task. Chrome headless prin CDP:
- drag simulat: `Input.dispatchDragEvent` nu e necesar; in `Runtime.evaluate` creeaza un `DataTransfer` si trimite `dragstart` pe card, `dragover` pe coloana Done, screenshot (coloanele `drop-ok` / `drop-no`), apoi `drop` si verifica prin API ca task-ul a ajuns Done;
- Send back prin drop pe Queued: fereastra de motiv (screenshot), completeaza, verifica statusul;
- meniul `⋯` deschis (screenshot) si "Move to…";
- Delete din modala: toast-ul cu Undo (screenshot), Undo readuce task-ul;
- panoul "Deleted (N)" (screenshot), Restore;
- latimea de telefon (390px): butonul `⋯` vizibil, fara scroll orizontal.

- [ ] **Step 6: Commit**

```bash
git add internal/web/static/js/board-move.js internal/web/static/js/deleted.js internal/web/static/js/workspace.js internal/web/static/js/task-modal.js internal/web/static/js/ui.js internal/web/static/app.css
git commit -m "ui: drag & drop intre coloane, meniul cardului, Delete cu Undo si panoul Deleted"
```

---

### Task 6: Verificare finala + CLAUDE.md

**Files:**
- Modify: `CLAUDE.md` (sectiunea Stare), `docs/superpowers/specs/2026-10-09-foreman-task-move-delete-design.md` (Status)

- [ ] **Step 1: Conventiile**

Run:
```bash
go vet ./... && go build -race ./... && go test ./... && GOOS=darwin GOARCH=arm64 go build -o /dev/null ./cmd/foreman && GOOS=windows go build -o /dev/null ./cmd/foreman
```
Expected: toate trec.

- [ ] **Step 2: Docs**

In `CLAUDE.md`, dupa paragraful "Gata (fisiere pe task, ...)":
```
Gata (mutare si stergere, spec in docs/superpowers/specs/2026-10-09-foreman-task-move-delete-design.md): drag & drop pe
board = actiunea coloanei (team.MoveTask: Accept, Send back, Unblock, Reopen, Close, mutare intre etape; In progress si
Review nu primesc drop), meniul "⋯" de pe card (Move to… / Delete). Delete = arhivare (status archived, migrarea 025,
archived_from/archived_root): run-ul se opreste, intrebarile/cererile legate se inchid, ListTasks/Subtasks nu le mai
intorc; toast cu Undo si panoul "Deleted (N)" cu Restore.
```
In spec: `Status: implementat (2026-10-09).`

- [ ] **Step 3: Commit**

```bash
git add CLAUDE.md docs/superpowers/specs/2026-10-09-foreman-task-move-delete-design.md
git commit -m "docs: mutare si stergere de task-uri gata"
```
