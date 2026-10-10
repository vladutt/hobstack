# Run history pentru sarcini recurente: plan de implementare

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** O modala "Run history" per sarcina recurenta: fisierele tuturor rularilor (ultima versiune pe cale) si lista rularilor cu raportul fiecareia.

**Architecture:** Totul se citeste din datele existente (`tasks.recurring_id`, `tasks.result`, `task_files`), fara migrare. Store-ul primeste trei interogari (istoric paginat, totaluri, fisiere recursiv), web-ul doua endpointuri noi si campul `runs` pe sarcina recurenta. UI-ul e un modul nou `recurring-history.js` (aceleasi clase ca modala de task) care refoloseste `taskFilesView` pentru fisiere, deschis prin ruta `#/p/<pid>/tasks/recurring/<rid>`.

**Tech Stack:** Go 1.x (net/http, database/sql cu modernc.org/sqlite), JS vanilla (module ES), CSS in `internal/web/static/app.css`.

**Spec:** `docs/superpowers/specs/2026-10-10-hobstack-recurring-history-design.md`

## Global Constraints

- Fara migrare, fara AI, fara dependente noi.
- Tot ce vede utilizatorul e in engleza; comentariile din cod raman in romana (fara diacritice, ca in restul codului).
- `go vet ./...` si `go build -race ./...` trebuie sa treaca; cross-compile `GOOS=darwin GOARCH=arm64 go build ./cmd/hobstack` si `GOOS=windows go build ./cmd/hobstack` (sterge binarul `hobstack.exe` lasat in folder).
- JS-ul se verifica copiind fisierul ca `.mjs` si rulând `node --check` (pe `.js` nu prinde erorile de sintaxa).
- Paginare: `limit` 1..50, implicit 20; `before` = id-ul ultimei rulari primite.
- Excerpt-ul raportului: primul paragraf (pana la primul rand gol), max 300 de caractere.
- Files: primele 10 intrari, apoi "Show all N". Runs: primele 20, apoi "Load more".
- Descarcarea / preview-ul fisierelor merg doar prin `GET /api/task-files/{id}` existent.
- In repo pot fi modificari necomise ale altor sesiuni (ex. `workspace.js`): la commit adauga doar hunk-urile tale; daca un fisier are si modificari straine, intreaba inainte de commit.
- Nu reporni serverul daca `pgrep -f "claude -p"` gaseste agenti activi.

## Review Focus

1. Rulare arhivata (Delete pe board): ramane in istoric (estompata, "Deleted") si fisierele ei raman in Files - testat in Task 1 (`RecurringHistory` / `RecurringFiles` includ arhivatele) si Task 2.
2. Paginare cand apare o rulare noua intre doua "Load more": `before` pe id nu dubleaza si nu sare rulari - testat in Task 1 (`before`).
3. Sarcina stearsa cat timp modala e deschisa sau link vechi `#/p/<pid>/tasks/recurring/<rid>`: 404 -> mesajul "This recurring task no longer exists", nu o eroare goala - testat in Task 2 (404) si verificat manual in Task 4.
4. Fisier cu aceeasi cale livrat de rulari diferite: o singura intrare cu versiunile dedesubt, fiecare cu T-N-ul ei - testat in Task 2 (`/files`).
5. `before` / `limit` invalide (text, negativ, 0, 51): 400 cu mesaj, nu 500 - testat in Task 2.

---

### Task 1: Store - istoric, totaluri, fisiere

**Files:**
- Modify: `internal/store/recurring.go` (adauga la final)
- Test: `internal/store/recurring_test.go` (adauga la final)

**Interfaces:**
- Consumes: `Task`, `taskCols`, `scanTask`, `TaskFile`, `taskFileCols`, `queryTaskFiles`, `CountTaskFilesTree`, `ArchiveTasks` (existente).
- Produces:
  - `type RecurringCounts struct { Total, Done, Open, Archived int }` (json `total`, `done`, `open`, `archived`)
  - `type RecurringRun struct { Task; Files int }` (json: campurile task-ului + `files`)
  - `func (s *Store) RecurringCounts(id int64) (RecurringCounts, error)`
  - `func (s *Store) RecurringHistory(id, before int64, limit int) ([]RecurringRun, bool, error)` (bool = mai sunt rulari)
  - `func (s *Store) RecurringFiles(id int64) ([]TaskFile, error)` (cea mai noua prima; include arhivatele si sub-task-urile)

- [ ] **Step 1: Scrie testul care pica**

Adauga la finalul `internal/store/recurring_test.go`:

```go
func TestRecurringHistory(t *testing.T) {
	s, p, lead := recurringFixture(t)
	mk := func(title string) RecurringTask {
		r := RecurringTask{ProjectID: p.ID, Title: title, AssigneeID: &lead.ID,
			Schedule: schedule.Spec{Kind: schedule.KindDaily, At: "09:00"}, Enabled: true, NextRunAt: time.Now()}
		if err := s.CreateRecurring(&r); err != nil {
			t.Fatal(err)
		}
		return r
	}
	scout, other := mk("Scout"), mk("Other")
	run := func(r RecurringTask, parent *int64) Task {
		task := Task{ProjectID: p.ID, Title: r.Title, AssigneeID: lead.ID, ByUser: true, ParentTaskID: parent}
		if parent == nil {
			task.RecurringID = &r.ID
		}
		if err := s.CreateTask(&task); err != nil {
			t.Fatal(err)
		}
		return task
	}
	file := func(task Task, path string) TaskFile {
		f := TaskFile{TaskID: task.ID, ProjectID: p.ID, AgentID: &lead.ID, AgentName: lead.Name,
			Path: path, Name: path, Stored: "x-" + path, Size: 3, Mime: "text/plain"}
		if err := s.AddTaskFile(&f); err != nil {
			t.Fatal(err)
		}
		return f
	}
	r1, r2, r3 := run(scout, nil), run(scout, nil), run(scout, nil)
	sub := run(scout, &r3.ID) // sub-task al rularii 3 (fara recurring_id)
	o1 := run(other, nil)
	if _, err := s.db.Exec(`UPDATE tasks SET status = 'done', result = 'found 2' WHERE id IN (?, ?)`, r1.ID, r3.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.ArchiveTasks([]int64{r2.ID}, r2.ID); err != nil {
		t.Fatal(err)
	}
	f1 := file(r1, "leads.csv")
	f2 := file(r2, "leads.csv") // rularea arhivata: fisierul ramane
	f3 := file(sub, "notes.md")
	f4 := file(r3, "leads.csv")
	file(o1, "other.csv")

	// totaluri: 3 rulari (sub-task-ul nu e rulare), 2 done, 0 open, 1 arhivata
	c, err := s.RecurringCounts(scout.ID)
	if err != nil || c != (RecurringCounts{Total: 3, Done: 2, Open: 0, Archived: 1}) {
		t.Fatalf("counts: %+v %v", c, err)
	}

	// istoric: cea mai noua prima, arhivata inclusa, alta sarcina exclusa
	runs, more, err := s.RecurringHistory(scout.ID, 0, 20)
	if err != nil || more || len(runs) != 3 || runs[0].ID != r3.ID || runs[1].ID != r2.ID || runs[2].ID != r1.ID {
		t.Fatalf("history: %+v %v %v", runs, more, err)
	}
	if runs[0].Files != 2 || runs[1].Files != 1 || runs[0].Result != "found 2" || runs[1].Status != "archived" {
		t.Fatalf("campuri: %+v", runs)
	}

	// paginare: limit 2 -> more; before = ultima primita -> restul
	page, more, err := s.RecurringHistory(scout.ID, 0, 2)
	if err != nil || !more || len(page) != 2 {
		t.Fatalf("pagina 1: %+v %v %v", page, more, err)
	}
	r4 := run(scout, nil) // o rulare noua intre pagini nu strica pagina 2
	page, more, err = s.RecurringHistory(scout.ID, page[1].ID, 2)
	if err != nil || more || len(page) != 1 || page[0].ID != r1.ID {
		t.Fatalf("pagina 2: %+v %v %v", page, more, err)
	}
	if runs, _, _ := s.RecurringHistory(scout.ID, 0, 20); runs[0].ID != r4.ID {
		t.Fatalf("rularea noua nu e prima: %+v", runs)
	}

	// fisiere: ale rularilor (si arhivate) si ale sub-task-urilor, cel mai nou primul
	files, err := s.RecurringFiles(scout.ID)
	if err != nil || len(files) != 4 {
		t.Fatalf("files: %+v %v", files, err)
	}
	if files[0].ID != f4.ID || files[1].ID != f3.ID || files[2].ID != f2.ID || files[3].ID != f1.ID {
		t.Fatalf("ordine: %+v", files)
	}

	// sarcina fara rulari
	empty := mk("Empty")
	if runs, more, err := s.RecurringHistory(empty.ID, 0, 20); err != nil || more || len(runs) != 0 || runs == nil {
		t.Fatalf("goala: %+v %v %v", runs, more, err)
	}
	if files, err := s.RecurringFiles(empty.ID); err != nil || len(files) != 0 {
		t.Fatalf("goala files: %+v %v", files, err)
	}
}
```

- [ ] **Step 2: Ruleaza testul si verifica ca pica**

Run: `go test ./internal/store/ -run TestRecurringHistory`
Expected: FAIL la compilare (`s.RecurringCounts undefined`, `RecurringCounts`, `RecurringHistory`, `RecurringFiles`).

- [ ] **Step 3: Implementeaza**

Adauga la finalul `internal/store/recurring.go`:

```go
// RecurringCounts: rularile unei sarcini recurente, pe stari (open = inca nu e Done, fara arhivate).
type RecurringCounts struct {
	Total    int `json:"total"`
	Done     int `json:"done"`
	Open     int `json:"open"`
	Archived int `json:"archived"`
}

func (s *Store) RecurringCounts(id int64) (RecurringCounts, error) {
	var c RecurringCounts
	err := s.db.QueryRow(`SELECT COUNT(*), COALESCE(SUM(status = 'done'), 0),
		COALESCE(SUM(status NOT IN ('done', 'archived')), 0), COALESCE(SUM(status = 'archived'), 0)
		FROM tasks WHERE recurring_id = ?`, id).Scan(&c.Total, &c.Done, &c.Open, &c.Archived)
	return c, err
}

// RecurringRun: un task creat de sarcina, cu numarul fisierelor lui si ale sub-task-urilor (cu versiuni).
type RecurringRun struct {
	Task
	Files int `json:"files"`
}

// RecurringHistory: rularile sarcinii (si cele arhivate), cea mai noua prima. before > 0 intoarce
// doar rularile mai vechi decat el (paginare stabila cand apar rulari noi); bool = mai sunt.
func (s *Store) RecurringHistory(id, before int64, limit int) ([]RecurringRun, bool, error) {
	tasks, err := s.recurringTasks(id, before, limit+1)
	if err != nil {
		return nil, false, err
	}
	more := len(tasks) > limit
	if more {
		tasks = tasks[:limit]
	}
	out := make([]RecurringRun, 0, len(tasks))
	for _, t := range tasks {
		n, err := s.CountTaskFilesTree(t.ID)
		if err != nil {
			return nil, false, err
		}
		out = append(out, RecurringRun{Task: t, Files: n})
	}
	return out, more, nil
}

// recurringTasks citeste tot inainte de alte interogari (rows inchis la iesire).
func (s *Store) recurringTasks(id, before int64, n int) ([]Task, error) {
	rows, err := s.db.Query(`SELECT `+taskCols+` FROM tasks WHERE recurring_id = ? AND (? = 0 OR id < ?)
		ORDER BY id DESC LIMIT ?`, id, before, before, n)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Task
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// RecurringFiles: fisierele rularilor (si ale celor arhivate) si ale sub-task-urilor lor, cel mai nou primul.
func (s *Store) RecurringFiles(id int64) ([]TaskFile, error) {
	return s.queryTaskFiles(`WITH RECURSIVE sub(id) AS (
			SELECT id FROM tasks WHERE recurring_id = ?
			UNION ALL SELECT t.id FROM tasks t JOIN sub ON t.parent_task_id = sub.id WHERE t.status != 'archived')
		SELECT `+taskFileCols+` FROM task_files WHERE task_id IN (SELECT id FROM sub) ORDER BY id DESC`, id)
}
```

- [ ] **Step 4: Ruleaza testele**

Run: `go test ./internal/store/`
Expected: PASS (si testele vechi).

- [ ] **Step 5: Commit**

```bash
git add internal/store/recurring.go internal/store/recurring_test.go
git commit -m "store: istoricul, totalurile si fisierele unei sarcini recurente"
```

---

### Task 2: API - runs, history, files

**Files:**
- Modify: `internal/web/api_recurring.go` (recurringView + doua handlere noi)
- Modify: `internal/web/api_task_files.go:19-47` (fileVersion / fileGroup / groupFiles + withRefs)
- Modify: `internal/web/server.go:140` (rutele noi, langa `POST /api/recurring/{id}/run`)
- Test: `internal/web/recurring_test.go` (adauga la final)

**Interfaces:**
- Consumes: `store.RecurringCounts`, `store.RecurringHistory`, `store.RecurringFiles`, `store.RecurringRun` (Task 1); `addTaskFile` (helper de test din `task_files_test.go`).
- Produces:
  - `recurringView.Runs int` (json `runs`) pe toate raspunsurile cu sarcini recurente (detaliul proiectului, POST, PUT).
  - `GET /api/recurring/{id}/history?before=&limit=` -> `{recurring: recurringView, counts: RecurringCounts, runs: [runView], more: bool}`; `runView` = campurile task-ului + `files`, `ref` ("T-70"), `assignee` (numele agentului).
  - `GET /api/recurring/{id}/files` -> `{user: [fileGroup], files: [fileGroup], subtasks: []}`, cu `ref` pe grup si `taskId` + `ref` pe fiecare versiune.
  - `fileGroup.Ref string` (json `ref,omitempty`), `fileVersion.TaskID int64` (json `taskId`), `fileVersion.Ref string` (json `ref,omitempty`).

- [ ] **Step 1: Scrie testul care pica**

Adauga la finalul `internal/web/recurring_test.go` (adauga `"hobstack/internal/schedule"`, `"hobstack/internal/store"` si `"time"` la importuri):

```go
func TestRecurringHistoryAPI(t *testing.T) {
	e := setup(t)
	p := e.createProject(t, t.TempDir())
	root := e.root(t, p.ID)
	rec := store.RecurringTask{ProjectID: p.ID, Title: "Scout", AssigneeID: &root.ID,
		Schedule: schedule.Spec{Kind: schedule.KindDaily, At: "09:00"}, Enabled: true, NextRunAt: time.Now()}
	if err := e.st.CreateRecurring(&rec); err != nil {
		t.Fatal(err)
	}
	run := func() store.Task {
		task := store.Task{ProjectID: p.ID, Title: "Scout", AssigneeID: root.ID, ByUser: true, RecurringID: &rec.ID}
		if err := e.st.CreateTask(&task); err != nil {
			t.Fatal(err)
		}
		return task
	}
	r1, r2 := run(), run()
	e.addTaskFile(t, r1, "leads.csv", "text/csv", "a")
	e.addTaskFile(t, r2, "leads.csv", "text/csv", "ab")
	e.addTaskFile(t, r2, "round2.md", "text/markdown", "# r2")
	base := "/api/recurring/" + itoa(rec.ID)

	// runs in detaliul proiectului
	_, raw := e.do(t, "GET", "/api/projects/"+itoa(p.ID), nil)
	if !strings.Contains(string(raw), `"runs":2`) {
		t.Fatalf("detaliu fara runs: %s", raw)
	}

	var h struct {
		Recurring struct {
			ID   int64 `json:"id"`
			Runs int   `json:"runs"`
		} `json:"recurring"`
		Counts struct {
			Total int `json:"total"`
			Open  int `json:"open"`
		} `json:"counts"`
		Runs []struct {
			ID       int64  `json:"id"`
			Ref      string `json:"ref"`
			Assignee string `json:"assignee"`
			Files    int    `json:"files"`
			Status   string `json:"status"`
		} `json:"runs"`
		More bool `json:"more"`
	}
	code, raw := e.do(t, "GET", base+"/history", nil)
	json.Unmarshal(raw, &h)
	if code != 200 || h.Recurring.ID != rec.ID || h.Recurring.Runs != 2 || h.Counts.Total != 2 || h.Counts.Open != 2 || h.More {
		t.Fatalf("history: %d %s", code, raw)
	}
	if len(h.Runs) != 2 || h.Runs[0].ID != r2.ID || h.Runs[0].Ref != r2.Ref() || h.Runs[0].Assignee != root.Name || h.Runs[0].Files != 2 {
		t.Fatalf("runs: %s", raw)
	}

	// paginare
	h.Runs = nil
	_, raw = e.do(t, "GET", base+"/history?limit=1", nil)
	json.Unmarshal(raw, &h)
	if len(h.Runs) != 1 || !h.More || h.Runs[0].ID != r2.ID {
		t.Fatalf("limit=1: %s", raw)
	}
	h.Runs = nil
	_, raw = e.do(t, "GET", base+"/history?limit=1&before="+itoa(r2.ID), nil)
	json.Unmarshal(raw, &h)
	if len(h.Runs) != 1 || h.More || h.Runs[0].ID != r1.ID {
		t.Fatalf("before: %s", raw)
	}

	// parametri invalizi -> 400; sarcina inexistenta -> 404
	for _, q := range []string{"?limit=0", "?limit=51", "?limit=x", "?before=-1", "?before=x"} {
		if code, raw := e.do(t, "GET", base+"/history"+q, nil); code != http.StatusBadRequest {
			t.Errorf("%s: %d %s", q, code, raw)
		}
	}
	for _, path := range []string{"/api/recurring/9999/history", "/api/recurring/9999/files"} {
		if code, _ := e.do(t, "GET", path, nil); code != http.StatusNotFound {
			t.Errorf("%s: %d", path, code)
		}
	}

	// fisiere: grupate pe cale, ultima versiune sus, versiunile cu ref
	var f struct {
		Files []struct {
			Path     string `json:"path"`
			Ref      string `json:"ref"`
			Size     int64  `json:"size"`
			Versions []struct {
				TaskID int64  `json:"taskId"`
				Ref    string `json:"ref"`
			} `json:"versions"`
		} `json:"files"`
		Subtasks []any `json:"subtasks"`
	}
	code, raw = e.do(t, "GET", base+"/files", nil)
	json.Unmarshal(raw, &f)
	if code != 200 || len(f.Files) != 2 || f.Subtasks == nil {
		t.Fatalf("files: %d %s", code, raw)
	}
	var leads = f.Files[0]
	if leads.Path != "leads.csv" {
		leads = f.Files[1]
	}
	if leads.Ref != r2.Ref() || leads.Size != 2 || len(leads.Versions) != 1 || leads.Versions[0].TaskID != r1.ID || leads.Versions[0].Ref != r1.Ref() {
		t.Fatalf("leads.csv: %s", raw)
	}
}
```

- [ ] **Step 2: Ruleaza testul si verifica ca pica**

Run: `go test ./internal/web/ -run TestRecurringHistoryAPI`
Expected: FAIL (`"runs":2` lipseste din detaliu, iar `/history` da 404/405).

- [ ] **Step 3: Implementeaza versiunile cu ref in `api_task_files.go`**

Inlocuieste `fileVersion`, `fileGroup` si `groupFiles` (liniile 19-47) cu:

```go
// fileVersion: o predare mai veche a aceleiasi cai.
type fileVersion struct {
	ID        int64     `json:"id"`
	TaskID    int64     `json:"taskId"`
	Ref       string    `json:"ref,omitempty"` // T-N, doar unde vin fisiere din mai multe task-uri (Run history)
	Size      int64     `json:"size"`
	AgentName string    `json:"agentName"`
	CreatedAt time.Time `json:"createdAt"`
}

// fileGroup: ultima versiune a unei cai, cu versiunile mai vechi.
type fileGroup struct {
	store.TaskFile
	Ref      string        `json:"ref,omitempty"`
	Versions []fileVersion `json:"versions"`
}

// groupFiles: lista vine cea mai noua prima; prima aparitie a unei cai e versiunea curenta.
// Ordinea grupurilor = ordinea versiunilor curente (cel mai nou primul).
func groupFiles(list []store.TaskFile) []fileGroup {
	out := []fileGroup{}
	at := map[string]int{} // cale -> index in out
	for _, f := range list {
		if i, ok := at[f.Path]; ok {
			out[i].Versions = append(out[i].Versions, fileVersion{ID: f.ID, TaskID: f.TaskID, Size: f.Size, AgentName: f.AgentName, CreatedAt: f.CreatedAt})
			continue
		}
		at[f.Path] = len(out)
		out = append(out, fileGroup{TaskFile: f, Versions: []fileVersion{}})
	}
	return out
}

// withRefs pune T-N-ul task-ului pe fiecare grup si versiune (refs: id task -> "T-N").
func withRefs(groups []fileGroup, refs map[int64]string) []fileGroup {
	for i := range groups {
		groups[i].Ref = refs[groups[i].TaskID]
		for j := range groups[i].Versions {
			groups[i].Versions[j].Ref = refs[groups[i].Versions[j].TaskID]
		}
	}
	return groups
}
```

- [ ] **Step 4: Implementeaza `runs` si handlerele in `api_recurring.go`**

Adauga `"strconv"` la importuri. In `recurringView` adauga campul si calculul:

```go
type recurringView struct {
	store.RecurringTask
	ScheduleText string        `json:"scheduleText"`
	Assignee     string        `json:"assignee"` // "" = prima etapa
	LastTask     *lastTaskView `json:"lastTask"`
	Runs         int           `json:"runs"` // task-urile create de sarcina (si arhivate)
}
```

si in `func (s *Server) recurringView(...)`, inainte de `return v`:

```go
	if c, err := s.st.RecurringCounts(r.ID); err == nil {
		v.Runs = c.Total
	}
```

La finalul fisierului adauga:

```go
// runView: o rulare din Run history.
type runView struct {
	store.RecurringRun
	Ref      string `json:"ref"`
	Assignee string `json:"assignee"`
}

// recurringHistory: modala Run history (rularile, cea mai noua prima, paginat cu before).
func (s *Server) recurringHistory(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		fail(w, err)
		return
	}
	rec, err := s.st.GetRecurring(id)
	if err != nil {
		fail(w, err)
		return
	}
	before, limit, err := historyPage(r)
	if err != nil {
		fail(w, err)
		return
	}
	runs, more, err := s.st.RecurringHistory(id, before, limit)
	if err != nil {
		fail(w, err)
		return
	}
	counts, err := s.st.RecurringCounts(id)
	if err != nil {
		fail(w, err)
		return
	}
	names := map[int64]string{} // agent -> nume, o singura citire per agent
	out := make([]runView, len(runs))
	for i, run := range runs {
		name, ok := names[run.AssigneeID]
		if !ok {
			if a, err := s.st.GetAgent(run.AssigneeID); err == nil {
				name = a.Name
			}
			names[run.AssigneeID] = name
		}
		out[i] = runView{RecurringRun: run, Ref: run.Ref(), Assignee: name}
	}
	writeJSON(w, http.StatusOK, map[string]any{"recurring": s.recurringView(rec), "counts": counts, "runs": out, "more": more})
}

// historyPage citeste ?before= (id, >= 0) si ?limit= (1..50, implicit 20).
func historyPage(r *http.Request) (int64, int, error) {
	q := r.URL.Query()
	before, limit := int64(0), 20
	if v := q.Get("before"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n < 0 {
			return 0, 0, badRequest("invalid before")
		}
		before = n
	}
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 50 {
			return 0, 0, badRequest("limit must be between 1 and 50")
		}
		limit = n
	}
	return before, limit, nil
}

// recurringFiles: fisierele tuturor rularilor, grupate pe cale (ca tab-ul Files), cu T-N pe versiuni.
func (s *Server) recurringFiles(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		fail(w, err)
		return
	}
	if _, err := s.st.GetRecurring(id); err != nil {
		fail(w, err)
		return
	}
	list, err := s.st.RecurringFiles(id)
	if err != nil {
		fail(w, err)
		return
	}
	refs := map[int64]string{}
	var mine, theirs []store.TaskFile
	for _, f := range list {
		if _, ok := refs[f.TaskID]; !ok {
			if t, err := s.st.GetTask(f.TaskID); err == nil {
				refs[f.TaskID] = t.Ref()
			} else {
				refs[f.TaskID] = ""
			}
		}
		if f.FromUser() {
			mine = append(mine, f)
		} else {
			theirs = append(theirs, f)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"user": withRefs(groupFiles(mine), refs), "files": withRefs(groupFiles(theirs), refs), "subtasks": []subtaskFiles{}})
}
```

- [ ] **Step 5: Inregistreaza rutele in `server.go`**

Dupa linia `mux.HandleFunc("POST /api/recurring/{id}/run", s.runRecurring)`:

```go
	mux.HandleFunc("GET /api/recurring/{id}/history", s.recurringHistory)
	mux.HandleFunc("GET /api/recurring/{id}/files", s.recurringFiles)
```

- [ ] **Step 6: Ruleaza testele**

Run: `go test ./internal/web/ ./internal/store/ && go vet ./...`
Expected: PASS (inclusiv `TestTaskFilesAPI`: `taskId` in plus pe versiuni nu strica nimic).

- [ ] **Step 7: Commit**

```bash
git add internal/web/api_recurring.go internal/web/api_task_files.go internal/web/server.go internal/web/recurring_test.go
git commit -m "api: Run history pentru sarcini recurente (history, files, runs)"
```

---

### Task 3: Modala Run history (JS + CSS)

**Files:**
- Modify: `internal/web/static/js/task-files.js` (optiunea `limit`, T-N pe randuri)
- Create: `internal/web/static/js/recurring-history.js`
- Modify: `internal/web/static/app.css` (dupa blocul `.rec-*`, ~linia 766)
- Modify: `docs/superpowers/specs/2026-10-10-hobstack-recurring-history-design.md` (o propozitie, vezi Step 4)

**Interfaces:**
- Consumes: `GET /api/recurring/{id}/history`, `GET /api/recurring/{id}/files`, `POST /api/recurring/{id}/run` (Task 2); `api` (api.js), `el`, `toast`, `clock` (ui.js), `renderMarkdown` (md.js), `taskFilesView` (task-files.js).
- Produces:
  - `taskFilesView({ onUpload, canUpload, limit })`: `limit` > 0 arata primele `limit` grupuri din `files` si butonul "Show all N"; un grup / o versiune cu `ref` arata T-N-ul in meta.
  - `export function recurringHistory({ projectId, recurringId, onClose, onEdit })` -> `{ recurringId, refresh(), destroy() }`. `onEdit(rec)` primeste sarcina (recurringView) cand utilizatorul apasa Edit.

- [ ] **Step 1: `task-files.js` - limit si ref**

Semnatura si starea:

```js
export function taskFilesView({ onUpload = null, canUpload = () => true, limit = 0 } = {}) {
  const root = el("div", { class: "tfiles" });
  let last = null;       // ultimul raspuns aratat
  let previewId = null;  // fisierul deschis (ramane deschis la refresh)
  const open = new Set(); // cai cu "earlier versions" desfasurat
  let uploading = false;  // un upload in curs (butonul e dezactivat)
  let showAll = false;    // cu limit: toate grupurile, nu doar primele
```

In `renderList`, inlocuieste blocul `if (last.files.length) { ... }` cu:

```js
    if (last.files.length) {
      if (user.length) nodes.push(el("h4", { class: "tfiles-sub-head" }, "From agents"));
      const cut = limit > 0 && !showAll && last.files.length > limit;
      nodes.push(el("ul", { class: "tfiles-list" }, (cut ? last.files.slice(0, limit) : last.files).map(row)));
      if (cut) nodes.push(el("button", { type: "button", class: "link-btn tfiles-more", onclick: () => { showAll = true; renderList(); } },
        `Show all ${last.files.length}`));
    } else if (!user.length && !last.subtasks.length) {
```

In `row(g)`, meta-ul grupului si al versiunilor capata T-N-ul cand exista:

```js
      el("span", { class: "tfiles-meta" }, `${size(g.size)} · ${g.agentName} · ${ago(g.createdAt)}${g.ref ? " · " + g.ref : ""}`),
```

```js
        el("span", { class: "tfiles-meta" }, ` ${size(v.size)} · ${v.agentName}${v.ref ? " · " + v.ref : ""} · `),
```

- [ ] **Step 2: Creeaza `recurring-history.js`**

```js
import { api } from "./api.js";
import { el, toast, clock } from "./ui.js";
import { renderMarkdown } from "./md.js";
import { taskFilesView } from "./task-files.js";

// Etichetele starilor (ca pe board); o rulare arhivata apare ca "Deleted".
const STATUS = {
  queued: "Queued", in_progress: "In progress", review: "Review", blocked: "Blocked", done: "Done",
};
const PAGE = 20;
const EXCERPT = 300;

// day: "Oct 9 14:00" (ora locala)
function day(iso) {
  return new Date(iso).toLocaleDateString("en-US", { month: "short", day: "numeric" }) + " " + clock(iso);
}

// excerpt: primul paragraf din raport, max 300 de caractere
function excerpt(text) {
  const first = text.trim().split(/\n\s*\n/)[0].trim();
  return first.length > EXCERPT ? first.slice(0, EXCERPT).trimEnd() + "…" : first;
}

// recurringHistory: modala Run history a unei sarcini recurente, peste board.
// - onClose(): inchide (prin adresa); onEdit(rec): deschide formularul sarcinii
// Intoarce { recurringId, refresh, destroy }; refresh reincarca fara sa piarda ce e desfasurat.
export function recurringHistory({ projectId, recurringId, onClose, onEdit }) {
  let destroyed = false;
  let rec = null;          // sarcina (recurringView)
  let counts = null;
  let runs = [];           // rularile incarcate
  let more = false;
  let loadingMore = false;
  const expanded = new Set(); // id-urile rularilor cu raportul intreg

  const titleId = "rh-title-" + recurringId;
  const title = el("h2", { class: "modal-title", id: titleId }, "Run history");
  const closeBtn = el("button", { type: "button", class: "modal-close", "aria-label": "Close", onclick: () => onClose() }, "×");
  const files = taskFilesView({ limit: 10 });
  const filesSection = el("section", { class: "modal-section rh-files", hidden: true },
    el("h3", { class: "rh-head" }, "Files"), files.el);
  const runsList = el("ul", { class: "rh-runs" });
  const moreBtn = el("button", { type: "button", class: "btn ghost small", hidden: true, onclick: loadMore }, "Load more");
  const runsSection = el("section", { class: "modal-section" }, el("h3", { class: "rh-head" }, "Runs"), runsList, moreBtn);
  const body = el("div", { class: "modal-body" }, el("p", { class: "hint" }, "Loading..."));
  const side = el("aside", { class: "modal-side", "aria-label": "Recurring task details" });
  const dialog = el("div", { class: "modal", role: "dialog", "aria-modal": "true", "aria-labelledby": titleId, tabindex: "-1" },
    el("div", { class: "modal-main" },
      // header ca la modala de task: crumbs cu X, apoi titlul
      el("div", { class: "modal-head" }, el("div", { class: "modal-head-main" },
        el("div", { class: "modal-crumbs" }, el("span", {}, "Recurring task"), el("span", { class: "grow" }), closeBtn), title)),
      body),
    side);
  const overlay = el("div", { class: "modal-overlay" }, dialog);
  let downOnOverlay = false;
  overlay.addEventListener("mousedown", (e) => { downOnOverlay = e.target === overlay; });
  overlay.addEventListener("click", (e) => { if (downOnOverlay && e.target === overlay) onClose(); });
  const onKey = (e) => { if (e.key === "Escape") { e.preventDefault(); onClose(); } };
  document.addEventListener("keydown", onKey);
  const prevOverflow = document.body.style.overflow;
  document.body.style.overflow = "hidden";
  document.body.append(overlay);
  dialog.focus();
  load();

  // load: prima pagina (sau cate rulari erau incarcate, max 50), totalurile si fisierele
  async function load() {
    const limit = Math.min(Math.max(runs.length, PAGE), 50);
    try {
      const [h, f] = await Promise.all([
        api(`/api/recurring/${recurringId}/history?limit=${limit}`),
        api(`/api/recurring/${recurringId}/files`),
      ]);
      if (destroyed) return;
      rec = h.recurring; counts = h.counts; runs = h.runs; more = h.more;
      files.show(f);
      filesSection.hidden = !f.files.length && !f.user.length;
      render();
    } catch (err) {
      if (destroyed) return;
      const msg = err.status === 404 ? "This recurring task no longer exists." : `Could not load the history: ${err.message}`;
      body.replaceChildren(el("p", { class: "hint" }, msg));
      side.replaceChildren();
    }
  }

  async function loadMore() {
    if (loadingMore || !runs.length) return;
    loadingMore = true;
    moreBtn.disabled = true;
    try {
      const h = await api(`/api/recurring/${recurringId}/history?limit=${PAGE}&before=${runs[runs.length - 1].id}`);
      if (destroyed) return;
      runs = runs.concat(h.runs);
      more = h.more;
      renderRuns();
    } catch (err) {
      toast(err.message);
    } finally {
      loadingMore = false;
      moreBtn.disabled = false;
    }
  }

  async function runNow() {
    try {
      const task = await api(`/api/recurring/${recurringId}/run`, { method: "POST" });
      toast("Created T-" + task.number, "ok");
      await load();
    } catch (err) {
      toast(err.message);
    }
  }

  function render() {
    title.textContent = rec.title;
    body.replaceChildren(filesSection, runsSection);
    renderRuns();
    renderSide();
  }

  function renderSide() {
    const parts = [`${counts.total} ${counts.total === 1 ? "run" : "runs"}`, `${counts.done} done`];
    if (counts.open) parts.push(`${counts.open} open`);
    if (counts.archived) parts.push(`${counts.archived} deleted`);
    const row = (label, value) => el("div", { class: "rh-side-row" }, el("span", { class: "hint" }, label), el("span", {}, value));
    side.replaceChildren(
      row("Schedule", rec.scheduleText),
      row("Assignee", rec.assignee || "First stage"),
      row("Next run", rec.enabled ? day(rec.nextRunAt) : "Off"),
      rec.autoAccept ? row("Auto-accept", "On") : null,
      row("Runs", parts.join(" · ")),
      rec.lastNote ? el("p", { class: rec.lastNote.startsWith("Skipped") ? "hint" : "rec-error" }, rec.lastNote) : null,
      el("div", { class: "rh-side-actions" },
        el("button", { type: "button", class: "btn small", onclick: runNow }, "Run now"),
        el("button", { type: "button", class: "btn small", onclick: () => onEdit(rec) }, "Edit")));
  }

  function renderRuns() {
    if (!runs.length) {
      runsList.replaceChildren(el("li", { class: "hint" }, "No runs yet."));
    } else {
      runsList.replaceChildren(...runs.map(runRow));
    }
    moreBtn.hidden = !more;
  }

  function runRow(r) {
    const archived = r.status === "archived";
    const isOpen = expanded.has(r.id);
    const none = r.status === "done" || archived ? "No report." : "No report yet.";
    const report = !r.result ? el("p", { class: "hint rh-excerpt" }, none)
      : isOpen ? renderMarkdown(r.result) : el("p", { class: "rh-excerpt" }, excerpt(r.result));
    return el("li", { class: "rh-run" + (archived ? " archived" : "") },
      el("div", { class: "rh-run-head" },
        el("button", { type: "button", class: "rh-toggle", "aria-expanded": String(isOpen), disabled: !r.result,
          onclick: () => { if (isOpen) expanded.delete(r.id); else expanded.add(r.id); renderRuns(); } },
          el("span", { class: "rh-when" }, day(r.createdAt))),
        el("a", { class: "task-id", href: `#/p/${projectId}/tasks/${r.id}` }, r.ref),
        archived ? el("span", { class: "pill" }, "Deleted")
          : el("span", { class: `pill pill-task-${r.status}` }, STATUS[r.status] || r.status),
        el("span", { class: "hint" }, r.assignee),
        r.files ? el("span", { class: "task-meta", title: `${r.files} ${r.files === 1 ? "file" : "files"}` }, `📎 ${r.files}`) : null),
      report);
  }

  return {
    recurringId,
    refresh() { if (!destroyed && !loadingMore) load(); },
    destroy() {
      destroyed = true;
      document.removeEventListener("keydown", onKey);
      document.body.style.overflow = prevOverflow;
      overlay.remove();
    },
  };
}
```

- [ ] **Step 3: CSS**

Adauga in `internal/web/static/app.css`, dupa linia `.rec-opt input[type=number] { width: 76px; }`:

```css
.rec-info .rec-runs { color: var(--amber); }
.rh-head { margin: 0 0 10px; font-size: 15px; font-weight: 650; }
.rh-files .tfiles-more { margin-top: 8px; }
.rh-runs { list-style: none; margin: 0; padding: 0; display: grid; gap: 8px; }
.rh-run { background: var(--card); border-radius: 11px; padding: 10px 14px; display: grid; gap: 6px; min-width: 0; }
.rh-run.archived { opacity: .55; }
.rh-run-head { display: flex; flex-wrap: wrap; align-items: center; gap: 6px 12px; font-size: 13px; }
.rh-toggle { background: none; border: 0; padding: 0; color: var(--text); font: inherit; font-weight: 600; cursor: pointer; }
.rh-toggle:disabled { cursor: default; }
.rh-toggle[aria-expanded="true"] .rh-when::after { content: " ▾"; color: var(--soft); }
.rh-toggle:not(:disabled)[aria-expanded="false"] .rh-when::after { content: " ▸"; color: var(--soft); }
.rh-excerpt { margin: 0; font-size: 13px; color: var(--soft); overflow-wrap: anywhere; }
.rh-run .md { font-size: 13px; overflow-wrap: anywhere; }
.rh-side-row { display: flex; justify-content: space-between; gap: 12px; font-size: 13px; padding: 4px 0; }
.rh-side-row > span:last-child { text-align: right; overflow-wrap: anywhere; }
.rh-side-actions { display: flex; flex-wrap: wrap; gap: 6px; margin-top: 10px; }
```

- [ ] **Step 4: Aliniaza spec-ul**

In spec, sectiunea Runs, inlocuieste "click pe rand desface raportul intreg (Markdown, ca in modala de task) si numele fisierelor rularii (click = preview in sectiunea Files)" cu "click pe data desface raportul intreg (Markdown, ca in modala de task); fisierele rularii se vad in Files (cu T-N-ul pe fiecare versiune)", iar la Store sterge `RecurringRuns` (numarul vine din `RecurringCounts`, apelat in `recurringView`) si la Live scrie "modala se reincarca la fiecare reincarcare a proiectului (scheduleReload, deja cu debounce)".

- [ ] **Step 5: Verifica sintaxa**

Run:
```bash
cd internal/web/static/js && for f in recurring-history task-files; do cp $f.js /tmp/$f.mjs && node --check /tmp/$f.mjs || exit 1; done; echo OK
```
Expected: `OK`.

- [ ] **Step 6: Commit**

```bash
git add internal/web/static/js/recurring-history.js internal/web/static/js/task-files.js internal/web/static/app.css docs/superpowers/specs/2026-10-10-hobstack-recurring-history-design.md
git commit -m "ui: modala Run history (fisierele si rularile unei sarcini recurente)"
```

---

### Task 4: Legaturi - ruta, randul din panou, Edit, refresh; verificare in browser

**Files:**
- Modify: `internal/web/static/js/workspace.js` (`parseWorkspaceRoute` ~linia 59, `update` ~linia 586, `toggleRecurring` ~linia 997, `syncModal` ~linia 1440, `reload` ~linia 1873, `cleanup`, apelul initial `syncModal(route.taskId)` ~linia 1883)
- Modify: `internal/web/static/js/recurring.js` (randul din `renderList` ~linia 84, `return` ~linia 243)

**Interfaces:**
- Consumes: `recurringHistory({ projectId, recurringId, onClose, onEdit })` -> `{ recurringId, refresh, destroy }` (Task 3); `recurringView.runs` (Task 2).
- Produces: `parseWorkspaceRoute` intoarce si `recurringId` (number | null); `recurringPanel(...)` intoarce in plus `edit(rec)`.

- [ ] **Step 1: Ruta in `parseWorkspaceRoute`**

Inlocuieste finalul functiei:

```js
  const tab = TABS.some((t) => t.id === parts[0]) ? parts[0] : TABS[0].id;
  const num = /^\d+$/.test(parts[1] || "") ? Number(parts[1]) : null;
  const agentId = tab === "team" ? num : null;
  const taskId = tab === "tasks" ? num : null;
  // #/p/3/tasks/recurring/5 deschide Run history al sarcinii recurente 5 peste board
  const recurringId = tab === "tasks" && parts[1] === "recurring" && /^\d+$/.test(parts[2] || "") ? Number(parts[2]) : null;
  return { tab, agentId, taskId, recurringId };
```

si ramura `agent` sa intoarca si `recurringId: null`. Actualizeaza comentariul de deasupra: `-> { tab, agentId, taskId, recurringId }`.

- [ ] **Step 2: Deschiderea si inchiderea modalei in `workspace.js`**

Langa `let modal` (declaratia modalei de task) adauga `let history = null; // modala Run history deschisa`. Dupa functia `syncModal` adauga:

```js
  // syncHistory: deschide / inchide Run history dupa recurringId din adresa
  function syncHistory(recurringId) {
    if (history && history.recurringId === recurringId) return;
    history?.destroy();
    history = null;
    if (!recurringId || gone()) return;
    history = recurringHistory({
      projectId, recurringId,
      onClose: () => { location.hash = `#/p/${projectId}/tasks`; },
      // Edit: inchide modala (prin adresa), apoi deschide panoul Recurring cu formularul sarcinii
      onEdit: (rec) => {
        window.addEventListener("hashchange", () => {
          if (boardPanel !== "recurring") toggleRecurring();
          recurring?.edit(rec);
        }, { once: true });
        location.hash = `#/p/${projectId}/tasks`;
      },
    });
  }
```

Importa: `import { recurringHistory } from "./recurring-history.js";` langa `import { recurringPanel } from "./recurring.js";`.

Apeleaza `syncHistory` peste tot unde se apeleaza `syncModal` cu ruta:
- in `update(r)`: dupa `syncModal(r.taskId);` adauga `syncHistory(r.recurringId);`
- la final: dupa `syncModal(route.taskId);` adauga `syncHistory(route.recurringId);`

In `reload` (unde e `modal?.refresh(state.project);`) adauga `history?.refresh(); // o rulare noua sau un fisier nou apar fara refresh`.

In `cleanup` adauga `history?.destroy();` dupa `modal?.destroy();`.

- [ ] **Step 3: Randul din panou si `edit` in `recurring.js`**

In `renderList`, inlocuieste linia cu `r.lastTask ? el("a", ...) : null,` cu:

```js
            r.runs ? el("a", { class: "rec-runs", href: `#/p/${projectId}/tasks/recurring/${r.id}`, title: "Run history" },
              `${r.runs} ${r.runs === 1 ? "run" : "runs"}`) : null,
            r.lastTask ? el("a", { href: `#/p/${projectId}/tasks/${r.lastTask.id}` },
              `last T-${r.lastTask.number} · ${STATUS[r.lastTask.status] || r.lastTask.status}`) : null,
```

si titlul devine link cand are rulari:

```js
          r.runs ? el("a", { class: "rec-title", href: `#/p/${projectId}/tasks/recurring/${r.id}` }, el("strong", {}, r.title))
            : el("strong", { class: "rec-title" }, r.title),
```

In obiectul intors la final adauga:

```js
    // edit: formularul sarcinii (din Run history)
    edit(r) { openForm(r); },
```

- [ ] **Step 4: Verifica sintaxa si build-ul**

Run:
```bash
cd internal/web/static/js && for f in workspace recurring recurring-history; do cp $f.js /tmp/$f.mjs && node --check /tmp/$f.mjs || exit 1; done; cd - && go vet ./... && go build -race ./... && GOOS=darwin GOARCH=arm64 go build ./cmd/hobstack && GOOS=windows go build ./cmd/hobstack && rm -f hobstack.exe && go test ./... 2>&1 | tail -20
```
Expected: fara erori, toate pachetele `ok`.

- [ ] **Step 5: Verificare in browser (Chrome headless prin CDP)**

1. `pgrep -f "claude -p"` - daca ruleaza agenti, NU reporni serverul principal; porneste o copie pe alt port cu alt folder de date, daca binarul are optiuni pentru asta, sau cere utilizatorului momentul restartului.
2. Pe serverul cu codul nou, deschide `#/p/<pid>/tasks` pe un proiect cu o sarcina recurenta cu rulari (ex. "Scount the internet"), deschide panoul Recurring si verifica: randul arata `N runs · last T-70 Done`; click pe `N runs` deschide modala; Files are intrari unice cu `v`/versiuni si T-N; Runs arata data, T-N, status, excerpt; click pe data desface raportul; "Load more" apare la peste 20 de rulari; Esc inchide si revine la `#/p/<pid>/tasks`; Edit deschide formularul; un link `#/p/<pid>/tasks/recurring/9999` arata "This recurring task no longer exists."
3. Screenshot la latime 1440 px si la ~400 px (fara scroll orizontal pe body). Masoara cu `document.documentElement.scrollWidth <= innerWidth`.

- [ ] **Step 6: Commit**

```bash
git status --short   # workspace.js poate avea si modificari ale altor sesiuni: adauga doar ce e al tau
git add internal/web/static/js/workspace.js internal/web/static/js/recurring.js
git commit -m "ui: Run history din panoul Recurring si ruta #/p/<id>/tasks/recurring/<id>"
```

- [ ] **Step 7: CLAUDE.md**

Adauga in sectiunea Stare din `CLAUDE.md` (dupa intrarea "sarcini recurente"):

```
Gata (Run history, spec in docs/superpowers/specs/2026-10-10-hobstack-recurring-history-design.md): modala per sarcina
recurenta (#/p/<pid>/tasks/recurring/<rid>, din "N runs" pe panoul Recurring): Files (fisierele tuturor rularilor si ale
sub-task-urilor, grupate pe cale, T-N pe versiuni) si Runs (paginat cate 20, excerpt din raport, arhivatele estompate);
store.RecurringHistory / RecurringCounts / RecurringFiles, GET /api/recurring/{id}/history si /files, recurring-history.js.
```

```bash
git add CLAUDE.md
git commit -m "docs: Run history in CLAUDE.md"
```
