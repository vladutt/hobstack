# Foreman: Stats tab Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Un tab nou **Stats** pe proiect (`#/p/3/stats`) cu cheltuiala, performanta echipei si activitate pe o perioada (Today / 7d / 30d / All).

**Architecture:** `store.ProjectStats` citeste randurile din perioada (runs, tasks, task_events, task_changes, questions) si le aduna in Go intr-o singura trecere, cu bucket-uri in ora locala. `GET /api/projects/{id}/stats?range=` alege perioada, adauga totalurile perioadei precedente si intoarce JSON. `static/js/stats.js` deseneaza tile-uri, doua grafice SVG facute de mana, tabelul pe agenti si topuri.

**Tech Stack:** Go 1.26, SQLite (modernc.org/sqlite), JS vanilla (module ES), SVG inline, fara dependente noi.

**Spec:** `docs/superpowers/specs/2026-10-08-foreman-project-stats-design.md`

## Global Constraints

- Fara dependente noi (Go sau JS).
- Tot ce vede utilizatorul e in engleza; comentariile din cod in romana (fara diacritice, ca restul codului).
- `go vet ./...` si `go build -race ./...` trebuie sa treaca.
- JS: copiaza fisierul ca `.mjs` si ruleaza `node --check` (pe `.js` nu prinde erorile de sintaxa).
- Cross-compile: `GOOS=darwin GOARCH=arm64 go build ./cmd/foreman` si `GOOS=windows go build ./cmd/foreman`.
- Toate limitele perioadelor in ora locala (`time.Local`); timestamp-urile din DB sunt RFC3339Nano UTC.
- localStorage doar in try/catch; cheia `foreman-stats-range`.
- JSON in camelCase.

## Review Focus

1. **Miezul noptii local vs UTC**: un eveniment la 01:00 ora locala (UTC+3) trebuie sa cada in ziua locala, nu in ziua UTC precedenta. Testat in Task 1 (`TestProjectStats` cu `time.FixedZone`).
2. **Limite de perioada exacte** (`[from, to)`): un rand fix la `to` nu intra, unul fix la `from` intra, chiar daca RFC3339Nano taie zerourile (`...:05Z` vs `...:05.5Z`). Testat in Task 1 (`TestStatsBoundaries`).
3. **Task-uri vechi fara evenimente** marcate done: intra ca terminate pe `updated_at`. Testat in Task 1 (t2).
4. **Proiect gol / perioada goala**: serii cu toate bucket-urile la 0, slice-uri goale (nu `null`) in JSON, UI cu "No activity in this range.". Testat in Task 1 (`TestProjectStatsEmpty`) si Task 2 (JSON).
5. **`range=all` pe un proiect fara activitate si peste 90 de zile** (bucket `week`). Testat in Task 2 (`TestStatsRange`).

---

## File Structure

- Create: `internal/store/stats.go`: tipurile `Stats*`, `ProjectStats`, `ProjectStatsTotals`, `ProjectFirstActivity`, helperii de bucket.
- Create: `internal/store/stats_test.go`
- Create: `internal/web/api_stats.go`: `statsRange` si handler-ul `getProjectStats`.
- Create: `internal/web/stats_test.go`
- Modify: `internal/web/server.go` (o ruta noua)
- Create: `internal/web/static/js/stats.js`: view-ul tab-ului si graficele SVG.
- Modify: `internal/web/static/js/workspace.js` (tab, corp, incarcare la intrare)
- Modify: `internal/web/static/app.css` (clase `stats-*`)
- Modify: `CLAUDE.md` (linia "Gata" pentru Stats)

---

### Task 1: store: statisticile unui proiect pe o perioada

**Files:**
- Create: `internal/store/stats.go`
- Test: `internal/store/stats_test.go`

**Interfaces:**
- Consumes: `Store.db`, `parseTime`, `nullID` (din `runs.go`), helperii de test `newStore`, `newProject`, `newWorker`, `ptr`.
- Produces:
  - `const BucketHour, BucketDay, BucketWeek = "hour", "day", "week"`
  - `type StatsTotals`, `StatsBucket`, `StatsAgent`, `StatsTask`, `StatsFile`, `Stats` (campurile de mai jos)
  - `func (s *Store) ProjectStats(projectID int64, from, to time.Time, bucket string) (Stats, error)`: `Prev` ramane nil
  - `func (s *Store) ProjectStatsTotals(projectID int64, from, to time.Time) (StatsTotals, error)`
  - `func (s *Store) ProjectFirstActivity(projectID int64) (time.Time, error)`: zero = fara activitate

- [ ] **Step 1: Write the failing tests**

`internal/store/stats_test.go`:

```go
package store

import (
	"fmt"
	"testing"
	"time"
)

// addRunAt pune un run terminat cu momente exacte (CreateRun foloseste ora curenta).
func addRunAt(t *testing.T, s *Store, id string, agent, task int64, status string, start time.Time, mins int, cost float64, tokens int64) {
	t.Helper()
	_, err := s.db.Exec(`INSERT INTO runs (id, agent_id, task_id, status, started_at, ended_at, cost_usd, tokens) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		id, agent, nullID(task), status, start.UTC().Format(time.RFC3339Nano),
		start.Add(time.Duration(mins)*time.Minute).UTC().Format(time.RFC3339Nano), cost, tokens)
	if err != nil {
		t.Fatal(err)
	}
}

// setAt muta un timestamp la un moment dat: query are doi parametri (momentul, id-ul).
func setAt(t *testing.T, s *Store, query string, at time.Time, id any) {
	t.Helper()
	if _, err := s.db.Exec(query, at.UTC().Format(time.RFC3339Nano), id); err != nil {
		t.Fatal(err)
	}
}

func newTaskAt(t *testing.T, s *Store, projectID, assignee int64, title string, at time.Time) Task {
	t.Helper()
	task := Task{ProjectID: projectID, Title: title, AssigneeID: assignee}
	if err := s.CreateTask(&task); err != nil {
		t.Fatal(err)
	}
	setAt(t, s, `UPDATE tasks SET created_at = ? WHERE id = ?`, at, task.ID)
	return task
}

func eventAt(t *testing.T, s *Store, taskID int64, kind string, at time.Time) {
	t.Helper()
	if err := s.AddTaskEvent(&TaskEvent{TaskID: taskID, Kind: kind, Text: kind, At: at}); err != nil {
		t.Fatal(err)
	}
}

// fixture comun: trei zile locale in UTC+3, ca miezul noptii local sa nu fie si in UTC.
type statsFixture struct {
	s          *Store
	p          Project
	lead, rook Agent
	t1         Task
	from, to   time.Time
}

func newStatsFixture(t *testing.T) statsFixture {
	t.Helper()
	s := newStore(t)
	p, lead := newProject(t, s)
	rook := newWorker(t, s, p.ID, "Rook")
	_, otherLead := newProject(t, s)
	zone := time.FixedZone("RO", 3*3600)
	from := time.Date(2026, 10, 6, 0, 0, 0, 0, zone)
	to := from.AddDate(0, 0, 3)
	h := func(n int) time.Time { return from.Add(time.Duration(n) * time.Hour) }

	// t1: creat la 01:00 local (22:00 UTC in ziua precedenta), trimis inapoi, acceptat la 05:00
	t1 := newTaskAt(t, s, p.ID, rook.ID, "Build API", h(1))
	eventAt(t, s, t1.ID, "sent_back", h(3))
	eventAt(t, s, t1.ID, "accepted", h(5))
	// t2: creat inainte de perioada, done fara eveniment (task vechi): conteaza updated_at
	t2 := newTaskAt(t, s, p.ID, rook.ID, "Old", h(-48))
	if _, err := s.db.Exec(`UPDATE tasks SET status = 'done' WHERE id = ?`, t2.ID); err != nil {
		t.Fatal(err)
	}
	setAt(t, s, `UPDATE tasks SET updated_at = ? WHERE id = ?`, h(26), t2.ID)
	// t3: creat in a treia zi, inca deschis; t4: dupa perioada
	newTaskAt(t, s, p.ID, lead.ID, "Docs", h(50))
	newTaskAt(t, s, p.ID, lead.ID, "Later", to.Add(time.Hour))

	addRunAt(t, s, "r1", rook.ID, t1.ID, RunDone, h(2), 30, 1.0, 1000)
	addRunAt(t, s, "r2", rook.ID, t1.ID, RunFailed, h(3), 10, 0.5, -1)
	addRunAt(t, s, "r3", lead.ID, 0, RunDone, h(25), 60, 2.0, 500)
	addRunAt(t, s, "r4", lead.ID, 0, RunStopped, h(49), 5, 0.25, 0)
	addRunAt(t, s, "r5", lead.ID, 0, RunDone, h(-1), 5, 100, 10) // inainte de perioada
	addRunAt(t, s, "r6", otherLead.ID, 0, RunDone, h(2), 5, 50, 10) // alt proiect

	if err := s.AddTaskChanges([]TaskChange{
		{TaskID: t1.ID, RunID: "r1", AgentID: &rook.ID, AgentName: "Rook", At: h(2), Kind: "edit", Tool: "Edit", Path: "a.go"},
		{TaskID: t1.ID, RunID: "r1", AgentID: &rook.ID, AgentName: "Rook", At: h(2).Add(10 * time.Minute), Kind: "edit", Tool: "Edit", Path: "a.go"},
		{TaskID: t1.ID, RunID: "r1", AgentID: &rook.ID, AgentName: "Rook", At: h(2), Kind: "create", Tool: "Write", Path: "b.go"},
		{TaskID: t1.ID, RunID: "r1", AgentID: &rook.ID, AgentName: "Rook", At: h(2), Kind: "command", Tool: "Bash", Command: "go test", OK: true},
		{TaskID: t1.ID, RunID: "r1", AgentID: &rook.ID, AgentName: "Rook", At: h(2), Kind: "command", Tool: "Bash", Command: "false"},
		{TaskID: t1.ID, RunID: "r0", AgentID: &rook.ID, AgentName: "Rook", At: h(-2), Kind: "edit", Tool: "Edit", Path: "old.go"},
	}); err != nil {
		t.Fatal(err)
	}

	// q1: pusa si raspunsa in perioada; q2: pusa inainte, raspunsa in perioada
	q1 := Question{ProjectID: p.ID, AgentID: rook.ID, Text: "Which DB?"}
	q2 := Question{ProjectID: p.ID, AgentID: rook.ID, Text: "Port?"}
	for _, q := range []*Question{&q1, &q2} {
		if err := s.AddQuestion(q); err != nil {
			t.Fatal(err)
		}
	}
	setAt(t, s, `UPDATE questions SET created_at = ? WHERE id = ?`, h(4), q1.ID)
	setAt(t, s, `UPDATE questions SET created_at = ? WHERE id = ?`, h(-5), q2.ID)
	for _, q := range []struct {
		id int64
		at time.Time
	}{{q1.ID, h(4).Add(30 * time.Minute)}, {q2.ID, h(1)}} {
		setAt(t, s, `UPDATE questions SET status = 'answered', closed_at = ? WHERE id = ?`, q.at, q.id)
	}
	return statsFixture{s: s, p: p, lead: lead, rook: rook, t1: t1, from: from, to: to}
}

func TestProjectStats(t *testing.T) {
	f := newStatsFixture(t)
	st, err := f.s.ProjectStats(f.p.ID, f.from, f.to, BucketDay)
	if err != nil {
		t.Fatal(err)
	}
	want := StatsTotals{
		CostUSD: 3.75, Tokens: 1500, TasksCreated: 2, TasksDone: 2,
		MedianDoneSec: (4*3600 + 74*3600) / 2, RunsDone: 2, RunsFailed: 1,
		Questions: 1, Answered: 2, FilesChanged: 2, Commands: 2, CommandsFailed: 1,
	}
	if st.Totals != want {
		t.Fatalf("totals:\n got %+v\nwant %+v", st.Totals, want)
	}
	if st.Prev != nil || st.Bucket != BucketDay {
		t.Fatalf("prev %v bucket %q", st.Prev, st.Bucket)
	}

	// trei zile locale; t1 (01:00 local) e in prima zi, nu in ziua UTC precedenta
	if len(st.Series) != 3 {
		t.Fatalf("series: %d", len(st.Series))
	}
	type day struct {
		rook, lead float64
		created    int
		done       int
	}
	for i, w := range []day{{1.5, 0, 1, 1}, {0, 2, 0, 1}, {0, 0.25, 1, 0}} {
		b := st.Series[i]
		if !b.Start.Equal(f.from.AddDate(0, 0, i)) || b.CostBy[f.rook.ID] != w.rook || b.CostBy[f.lead.ID] != w.lead ||
			b.Created != w.created || b.Done != w.done {
			t.Fatalf("ziua %d: %+v, vrem %+v", i, b, w)
		}
	}

	// agentii: dupa cost, descrescator
	if len(st.Agents) != 2 {
		t.Fatalf("agents: %+v", st.Agents)
	}
	lead, rook := st.Agents[0], st.Agents[1]
	if lead.AgentID != f.lead.ID || lead.Runs != 2 || lead.Failed != 0 || lead.CostUSD != 2.25 || lead.Tokens != 500 ||
		lead.Seconds != 3900 || lead.TasksDone != 0 {
		t.Fatalf("lead: %+v", lead)
	}
	if rook.AgentID != f.rook.ID || rook.Name != "Rook" || rook.Runs != 2 || rook.Failed != 1 || rook.CostUSD != 1.5 ||
		rook.Tokens != 1000 || rook.Seconds != 2400 || rook.TasksDone != 2 || rook.SendBacks != 1 || rook.FilesChanged != 2 {
		t.Fatalf("rook: %+v", rook)
	}

	if len(st.TopTasks) != 1 || st.TopTasks[0].TaskID != f.t1.ID || st.TopTasks[0].Number != f.t1.Number ||
		st.TopTasks[0].Title != "Build API" || st.TopTasks[0].CostUSD != 1.5 || st.TopTasks[0].Runs != 2 {
		t.Fatalf("top tasks: %+v", st.TopTasks)
	}
	if fmt.Sprint(st.TopFiles) != "[{a.go 2} {b.go 1}]" {
		t.Fatalf("top files: %v", st.TopFiles)
	}
}

func TestProjectStatsTotalsPrevious(t *testing.T) {
	f := newStatsFixture(t)
	prev, err := f.s.ProjectStatsTotals(f.p.ID, f.from.AddDate(0, 0, -3), f.from)
	if err != nil {
		t.Fatal(err)
	}
	// r5 (100$), t2 creat, schimbarea pe old.go, q2 pusa
	if prev.CostUSD != 100 || prev.RunsDone != 1 || prev.TasksCreated != 1 || prev.FilesChanged != 1 || prev.Questions != 1 {
		t.Fatalf("%+v", prev)
	}
}

func TestStatsBoundaries(t *testing.T) {
	s := newStore(t)
	p, lead := newProject(t, s)
	from := time.Date(2026, 10, 6, 0, 0, 5, 0, time.UTC)
	to := from.Add(time.Hour)
	addRunAt(t, s, "at-from", lead.ID, 0, RunDone, from, 1, 1, 0)                         // "...:05Z": intra
	addRunAt(t, s, "before", lead.ID, 0, RunDone, from.Add(-time.Millisecond), 1, 10, 0) // "...:04.999Z": nu
	addRunAt(t, s, "at-to", lead.ID, 0, RunDone, to, 1, 100, 0)                          // nu
	addRunAt(t, s, "half", lead.ID, 0, RunDone, from.Add(500*time.Millisecond), 1, 1000, 0)
	tot, err := s.ProjectStatsTotals(p.ID, from, to)
	if err != nil || tot.CostUSD != 1001 {
		t.Fatalf("%+v %v", tot, err)
	}
}

func TestProjectStatsEmpty(t *testing.T) {
	s := newStore(t)
	p, _ := newProject(t, s)
	from := time.Date(2026, 10, 8, 0, 0, 0, 0, time.Local)
	st, err := s.ProjectStats(p.ID, from, from.Add(24*time.Hour), BucketHour)
	if err != nil {
		t.Fatal(err)
	}
	if st.Totals != (StatsTotals{}) || len(st.Series) != 24 || st.Agents == nil || st.TopTasks == nil || st.TopFiles == nil {
		t.Fatalf("%+v", st)
	}
	if st.Series[0].CostBy == nil {
		t.Fatal("costBy trebuie sa fie {} in JSON, nu null")
	}
	first, err := s.ProjectFirstActivity(p.ID)
	if err != nil || !first.IsZero() {
		t.Fatalf("%v %v", first, err)
	}
}

func TestProjectFirstActivity(t *testing.T) {
	f := newStatsFixture(t)
	first, err := f.s.ProjectFirstActivity(f.p.ID)
	if err != nil || !first.Equal(f.from.Add(-48*time.Hour)) { // t2
		t.Fatalf("%v %v", first, err)
	}
}

func TestBucketStartsWeek(t *testing.T) {
	from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	starts := bucketStarts(from, from.AddDate(0, 0, 15), BucketWeek)
	if len(starts) != 3 || bucketIndex(starts, from.AddDate(0, 0, 13)) != 1 || bucketIndex(starts, from.Add(-time.Second)) != -1 {
		t.Fatalf("%v", starts)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/store -run 'Stats|FirstActivity|BucketStarts' -v`
Expected: FAIL la compilare (`undefined: BucketDay`, `ProjectStats`, ...).

- [ ] **Step 3: Write the implementation**

`internal/store/stats.go`:

```go
package store

import (
	"database/sql"
	"sort"
	"time"
)

// Bucket-urile seriilor din tab-ul Stats.
const (
	BucketHour = "hour"
	BucketDay  = "day"
	BucketWeek = "week"
)

// StatsTotals: cifrele mari ale unei perioade (si ale perioadei precedente).
type StatsTotals struct {
	CostUSD        float64 `json:"costUsd"`
	Tokens         int64   `json:"tokens"`
	TasksCreated   int     `json:"tasksCreated"`
	TasksDone      int     `json:"tasksDone"`
	MedianDoneSec  int64   `json:"medianDoneSec"` // 0 = niciun task terminat
	RunsDone       int     `json:"runsDone"`
	RunsFailed     int     `json:"runsFailed"`
	Questions      int     `json:"questions"` // puse in perioada
	Answered       int     `json:"answered"`  // raspunse in perioada
	FilesChanged   int     `json:"filesChanged"`
	Commands       int     `json:"commands"`
	CommandsFailed int     `json:"commandsFailed"`
}

// StatsBucket: o ora, o zi sau o saptamana din serie.
type StatsBucket struct {
	Start   time.Time         `json:"start"`
	CostBy  map[int64]float64 `json:"costBy"` // agent -> cost
	Created int               `json:"created"`
	Done    int               `json:"done"`
}

// StatsAgent: un rand din tabelul Agents (doar agentii cu activitate in perioada).
type StatsAgent struct {
	AgentID      int64   `json:"agentId"`
	Name         string  `json:"name"`
	Runs         int     `json:"runs"`
	Failed       int     `json:"failed"`
	CostUSD      float64 `json:"costUsd"`
	Tokens       int64   `json:"tokens"`
	Seconds      int64   `json:"seconds"`
	TasksDone    int     `json:"tasksDone"`
	SendBacks    int     `json:"sendBacks"`
	FilesChanged int     `json:"filesChanged"`
}

type StatsTask struct {
	TaskID  int64   `json:"taskId"`
	Number  int     `json:"number"`
	Title   string  `json:"title"`
	Status  string  `json:"status"`
	CostUSD float64 `json:"costUsd"`
	Runs    int     `json:"runs"`
}

type StatsFile struct {
	Path    string `json:"path"`
	Changes int    `json:"changes"`
}

type Stats struct {
	From     time.Time     `json:"from"`
	To       time.Time     `json:"to"`
	Bucket   string        `json:"bucket"`
	Totals   StatsTotals   `json:"totals"`
	Prev     *StatsTotals  `json:"prev"` // nil = fara comparatie (range-ul all)
	Series   []StatsBucket `json:"series"`
	Agents   []StatsAgent  `json:"agents"`
	TopTasks []StatsTask   `json:"topTasks"`
	TopFiles []StatsFile   `json:"topFiles"`
}

// statsTop: cate task-uri si fisiere arata topurile.
const statsTop = 5

// sqlBound formateaza o limita de perioada pentru comparat ca text cu coloanele RFC3339Nano.
// Cu 9 zecimale fixe comparatia e corecta si cand valoarea salvata nu are zecimale
// ("...:05Z" > "...:05.000000000Z", pentru ca 'Z' > '0'), deci [from, to) ramane exact.
func sqlBound(t time.Time) string { return t.UTC().Format("2006-01-02T15:04:05.000000000Z07:00") }

// bucketStarts: inceputurile bucket-urilor din [from, to), in ora lui from.
// AddDate pastreaza miezul noptii local si peste schimbarea orei.
func bucketStarts(from, to time.Time, bucket string) []time.Time {
	var out []time.Time
	for t := from; t.Before(to); {
		out = append(out, t)
		switch bucket {
		case BucketHour:
			t = t.Add(time.Hour)
		case BucketWeek:
			t = t.AddDate(0, 0, 7)
		default:
			t = t.AddDate(0, 0, 1)
		}
	}
	return out
}

// bucketIndex: bucket-ul in care cade t; -1 inainte de primul.
func bucketIndex(starts []time.Time, t time.Time) int {
	return sort.Search(len(starts), func(i int) bool { return starts[i].After(t) }) - 1
}

// statsAcc aduna randurile unei perioade. Fara starts (perioada precedenta)
// se calculeaza doar totalurile.
type statsAcc struct {
	from, to   time.Time
	starts     []time.Time
	totals     StatsTotals
	series     []StatsBucket
	agents     map[int64]*StatsAgent
	agentFiles map[int64]map[string]bool
	tasks      map[int64]*StatsTask
	files      map[string]int
	doneSecs   []int64
}

func (a *statsAcc) in(t time.Time) bool { return !t.Before(a.from) && t.Before(a.to) }

// bucket: bucket-ul lui t sau nil (fara serie).
func (a *statsAcc) bucket(t time.Time) *StatsBucket {
	if i := bucketIndex(a.starts, t); i >= 0 && i < len(a.series) {
		return &a.series[i]
	}
	return nil
}

func (a *statsAcc) agent(id int64, name string) *StatsAgent {
	r := a.agents[id]
	if r == nil {
		r = &StatsAgent{AgentID: id, Name: name}
		a.agents[id] = r
	}
	return r
}

func (s *Store) ProjectStats(projectID int64, from, to time.Time, bucket string) (Stats, error) {
	a := newStatsAcc(from, to, bucketStarts(from, to, bucket))
	if err := s.collectStats(projectID, a); err != nil {
		return Stats{}, err
	}
	return a.result(bucket), nil
}

func (s *Store) ProjectStatsTotals(projectID int64, from, to time.Time) (StatsTotals, error) {
	a := newStatsAcc(from, to, nil)
	if err := s.collectStats(projectID, a); err != nil {
		return StatsTotals{}, err
	}
	return a.result("").Totals, nil
}

func newStatsAcc(from, to time.Time, starts []time.Time) *statsAcc {
	a := &statsAcc{
		from: from, to: to, starts: starts,
		agents: map[int64]*StatsAgent{}, agentFiles: map[int64]map[string]bool{},
		tasks: map[int64]*StatsTask{}, files: map[string]int{},
	}
	for _, st := range starts {
		a.series = append(a.series, StatsBucket{Start: st, CostBy: map[int64]float64{}})
	}
	return a
}

func (s *Store) collectStats(projectID int64, a *statsAcc) error {
	for _, step := range []func(int64, *statsAcc) error{s.statsRuns, s.statsTasks, s.statsSendBacks, s.statsChanges, s.statsQuestions} {
		if err := step(projectID, a); err != nil {
			return err
		}
	}
	return nil
}

// statsRuns: cost, tokeni, run-uri reusite/esuate, timp de lucru; pe agent, pe bucket si pe task.
func (s *Store) statsRuns(projectID int64, a *statsAcc) error {
	rows, err := s.db.Query(`SELECT r.agent_id, ag.name, r.task_id, COALESCE(t.number, 0), COALESCE(t.title, ''), COALESCE(t.status, ''),
			r.status, r.started_at, r.ended_at, COALESCE(r.cost_usd, 0), r.tokens
		FROM runs r JOIN agents ag ON ag.id = r.agent_id LEFT JOIN tasks t ON t.id = r.task_id
		WHERE ag.project_id = ? AND r.started_at >= ? AND r.started_at < ?`,
		projectID, sqlBound(a.from), sqlBound(a.to))
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var agentID int64
		var name, title, taskStatus, status, started string
		var taskID sql.NullInt64
		var number int
		var ended sql.NullString
		var cost float64
		var tokens int64
		if err := rows.Scan(&agentID, &name, &taskID, &number, &title, &taskStatus, &status, &started, &ended, &cost, &tokens); err != nil {
			return err
		}
		ag := a.agent(agentID, name)
		ag.Runs++
		ag.CostUSD += cost
		a.totals.CostUSD += cost
		if tokens > 0 { // -1 = necunoscut
			ag.Tokens += tokens
			a.totals.Tokens += tokens
		}
		switch status {
		case RunDone:
			a.totals.RunsDone++
		case RunFailed:
			a.totals.RunsFailed++
			ag.Failed++
		}
		start := parseTime(started)
		if ended.Valid {
			ag.Seconds += int64(parseTime(ended.String).Sub(start).Seconds())
		}
		if b := a.bucket(start); b != nil {
			b.CostBy[agentID] += cost
		}
		if taskID.Valid {
			tk := a.tasks[taskID.Int64]
			if tk == nil {
				tk = &StatsTask{TaskID: taskID.Int64, Number: number, Title: title, Status: taskStatus}
				a.tasks[taskID.Int64] = tk
			}
			tk.Runs++
			tk.CostUSD += cost
		}
	}
	return rows.Err()
}

// statsTasks: task-uri create si terminate. Terminat = primul accepted / user_accepted;
// task-urile vechi (fara evenimente) cu status done folosesc updated_at.
func (s *Store) statsTasks(projectID int64, a *statsAcc) error {
	rows, err := s.db.Query(`SELECT t.assignee_id, COALESCE(ag.name, ''), t.created_at,
			COALESCE((SELECT MIN(e.created_at) FROM task_events e WHERE e.task_id = t.id AND e.kind IN ('accepted', 'user_accepted')),
				CASE WHEN t.status = 'done' THEN t.updated_at END)
		FROM tasks t LEFT JOIN agents ag ON ag.id = t.assignee_id
		WHERE t.project_id = ?`, projectID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var assignee int64
		var name, created string
		var doneAt sql.NullString
		if err := rows.Scan(&assignee, &name, &created, &doneAt); err != nil {
			return err
		}
		c := parseTime(created)
		if a.in(c) {
			a.totals.TasksCreated++
			if b := a.bucket(c); b != nil {
				b.Created++
			}
		}
		if !doneAt.Valid {
			continue
		}
		d := parseTime(doneAt.String)
		if !a.in(d) {
			continue
		}
		a.totals.TasksDone++
		a.doneSecs = append(a.doneSecs, int64(d.Sub(c).Seconds()))
		if b := a.bucket(d); b != nil {
			b.Done++
		}
		if name != "" { // agent sters: fara rand in tabel
			a.agent(assignee, name).TasksDone++
		}
	}
	return rows.Err()
}

// statsSendBacks: trimiterile inapoi (sent_back, rejected) ale task-urilor fiecarui agent.
func (s *Store) statsSendBacks(projectID int64, a *statsAcc) error {
	rows, err := s.db.Query(`SELECT t.assignee_id, ag.name
		FROM task_events e JOIN tasks t ON t.id = e.task_id JOIN agents ag ON ag.id = t.assignee_id
		WHERE t.project_id = ? AND e.kind IN ('sent_back', 'rejected') AND e.created_at >= ? AND e.created_at < ?`,
		projectID, sqlBound(a.from), sqlBound(a.to))
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var name string
		if err := rows.Scan(&id, &name); err != nil {
			return err
		}
		a.agent(id, name).SendBacks++
	}
	return rows.Err()
}

// statsChanges: fisiere modificate (distincte) si comenzi Bash; pe agent doar daca agentul mai exista.
func (s *Store) statsChanges(projectID int64, a *statsAcc) error {
	rows, err := s.db.Query(`SELECT c.agent_id, COALESCE(ag.name, ''), c.kind, c.path, c.ok
		FROM task_changes c JOIN tasks t ON t.id = c.task_id LEFT JOIN agents ag ON ag.id = c.agent_id
		WHERE t.project_id = ? AND c.created_at >= ? AND c.created_at < ?`,
		projectID, sqlBound(a.from), sqlBound(a.to))
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var agentID sql.NullInt64
		var name, kind, path string
		var ok bool
		if err := rows.Scan(&agentID, &name, &kind, &path, &ok); err != nil {
			return err
		}
		if kind == "command" {
			a.totals.Commands++
			if !ok {
				a.totals.CommandsFailed++
			}
			continue
		}
		a.files[path]++
		if agentID.Valid && name != "" {
			seen := a.agentFiles[agentID.Int64]
			if seen == nil {
				seen = map[string]bool{}
				a.agentFiles[agentID.Int64] = seen
			}
			if !seen[path] {
				seen[path] = true
				a.agent(agentID.Int64, name).FilesChanged++
			}
		}
	}
	return rows.Err()
}

// statsQuestions: intrebari puse in perioada si raspunse in perioada (pot fi intrebari diferite).
func (s *Store) statsQuestions(projectID int64, a *statsAcc) error {
	f, t := sqlBound(a.from), sqlBound(a.to)
	rows, err := s.db.Query(`SELECT created_at, status, closed_at FROM questions
		WHERE project_id = ? AND ((created_at >= ? AND created_at < ?) OR (closed_at >= ? AND closed_at < ?))`,
		projectID, f, t, f, t)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var created, status string
		var closed sql.NullString
		if err := rows.Scan(&created, &status, &closed); err != nil {
			return err
		}
		if a.in(parseTime(created)) {
			a.totals.Questions++
		}
		if status == "answered" && closed.Valid && a.in(parseTime(closed.String)) {
			a.totals.Answered++
		}
	}
	return rows.Err()
}

// result: ordoneaza si taie topurile; slice-urile sunt mereu non-nil (in JSON [] nu null).
func (a *statsAcc) result(bucket string) Stats {
	t := a.totals
	t.FilesChanged = len(a.files)
	if n := len(a.doneSecs); n > 0 {
		sort.Slice(a.doneSecs, func(i, j int) bool { return a.doneSecs[i] < a.doneSecs[j] })
		if n%2 == 1 {
			t.MedianDoneSec = a.doneSecs[n/2]
		} else {
			t.MedianDoneSec = (a.doneSecs[n/2-1] + a.doneSecs[n/2]) / 2
		}
	}
	out := Stats{From: a.from, To: a.to, Bucket: bucket, Totals: t, Series: a.series,
		Agents: []StatsAgent{}, TopTasks: []StatsTask{}, TopFiles: []StatsFile{}}
	if out.Series == nil {
		out.Series = []StatsBucket{}
	}
	for _, ag := range a.agents {
		out.Agents = append(out.Agents, *ag)
	}
	sort.Slice(out.Agents, func(i, j int) bool {
		if out.Agents[i].CostUSD != out.Agents[j].CostUSD {
			return out.Agents[i].CostUSD > out.Agents[j].CostUSD
		}
		return out.Agents[i].Name < out.Agents[j].Name
	})
	for _, tk := range a.tasks {
		out.TopTasks = append(out.TopTasks, *tk)
	}
	sort.Slice(out.TopTasks, func(i, j int) bool {
		if out.TopTasks[i].CostUSD != out.TopTasks[j].CostUSD {
			return out.TopTasks[i].CostUSD > out.TopTasks[j].CostUSD
		}
		return out.TopTasks[i].Number < out.TopTasks[j].Number
	})
	if len(out.TopTasks) > statsTop {
		out.TopTasks = out.TopTasks[:statsTop]
	}
	for path, n := range a.files {
		out.TopFiles = append(out.TopFiles, StatsFile{Path: path, Changes: n})
	}
	sort.Slice(out.TopFiles, func(i, j int) bool {
		if out.TopFiles[i].Changes != out.TopFiles[j].Changes {
			return out.TopFiles[i].Changes > out.TopFiles[j].Changes
		}
		return out.TopFiles[i].Path < out.TopFiles[j].Path
	})
	if len(out.TopFiles) > statsTop {
		out.TopFiles = out.TopFiles[:statsTop]
	}
	return out
}

// ProjectFirstActivity: primul task sau run al proiectului (zero = nimic inca).
// Pentru range-ul all.
func (s *Store) ProjectFirstActivity(projectID int64) (time.Time, error) {
	var first sql.NullString
	err := s.db.QueryRow(`SELECT MIN(x) FROM (
			SELECT MIN(created_at) AS x FROM tasks WHERE project_id = ?
			UNION ALL
			SELECT MIN(r.started_at) FROM runs r JOIN agents ag ON ag.id = r.agent_id WHERE ag.project_id = ?)`,
		projectID, projectID).Scan(&first)
	if err != nil || !first.Valid {
		return time.Time{}, err
	}
	return parseTime(first.String), nil
}
```

Nota pentru `TestProjectFirstActivity`: `MIN` pe text compara si zecimalele; in fixture momentele sunt la ora fixa (fara zecimale), deci rezultatul e exact.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/store -run 'Stats|FirstActivity|BucketStarts' -v`
Expected: PASS (5 teste + `TestBucketStartsWeek`). Apoi `go test ./internal/store` (tot pachetul) trebuie sa treaca.

- [ ] **Step 5: Commit**

```bash
git add internal/store/stats.go internal/store/stats_test.go
git commit -m "store: statisticile unui proiect pe o perioada (ProjectStats)"
```

---

### Task 2: web: `GET /api/projects/{id}/stats`

**Files:**
- Create: `internal/web/api_stats.go`
- Modify: `internal/web/server.go:73` (dupa `POST /api/projects/{id}/open`)
- Test: `internal/web/stats_test.go`

**Interfaces:**
- Consumes: `store.ProjectStats`, `store.ProjectStatsTotals`, `store.ProjectFirstActivity`, `store.Bucket*` (Task 1); `pathID`, `fail`, `writeJSON`, `writeErr`; in teste `setup`, `e.do`, `e.createProject`, `e.root`, `itoa`.
- Produces: ruta `GET /api/projects/{id}/stats?range=today|7d|30d|all` care intoarce `store.Stats` ca JSON (cu `prev` pentru today/7d/30d); `func statsRange(name string, now, first time.Time) (from time.Time, bucket string, ok bool)`.

- [ ] **Step 1: Write the failing tests**

`internal/web/stats_test.go`:

```go
package web

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"foreman/internal/store"
)

func TestStatsRange(t *testing.T) {
	zone := time.FixedZone("RO", 3*3600)
	now := time.Date(2026, 10, 8, 14, 30, 0, 0, zone)
	mid := time.Date(2026, 10, 8, 0, 0, 0, 0, zone)
	cases := []struct {
		name   string
		first  time.Time
		from   time.Time
		bucket string
	}{
		{"today", time.Time{}, mid, store.BucketHour},
		{"7d", time.Time{}, mid.AddDate(0, 0, -6), store.BucketDay},
		{"30d", time.Time{}, mid.AddDate(0, 0, -29), store.BucketDay},
		{"all", time.Time{}, mid, store.BucketDay}, // fara activitate: doar azi
		{"all", time.Date(2026, 9, 1, 21, 30, 0, 0, time.UTC), time.Date(2026, 9, 2, 0, 0, 0, 0, zone), store.BucketDay}, // 00:30 local pe 2
		{"all", mid.AddDate(0, 0, -120), mid.AddDate(0, 0, -120), store.BucketWeek},
	}
	for _, c := range cases {
		from, bucket, ok := statsRange(c.name, now, c.first)
		if !ok || !from.Equal(c.from) || bucket != c.bucket {
			t.Errorf("%s %v: %v %q %v, vrem %v %q", c.name, c.first, from, bucket, ok, c.from, c.bucket)
		}
	}
	if _, _, ok := statsRange("90d", now, time.Time{}); ok {
		t.Error("90d nu exista")
	}
}

func TestProjectStatsAPI(t *testing.T) {
	e := setup(t)
	p := e.createProject(t, t.TempDir())
	root := e.root(t, p.ID)
	task := store.Task{ProjectID: p.ID, Title: "Build", AssigneeID: root.ID}
	if err := e.st.CreateTask(&task); err != nil {
		t.Fatal(err)
	}

	code, raw := e.do(t, "GET", "/api/projects/"+itoa(p.ID)+"/stats", nil) // implicit 7d
	var st store.Stats
	if err := json.Unmarshal(raw, &st); err != nil || code != http.StatusOK {
		t.Fatalf("%d %s %v", code, raw, err)
	}
	if st.Bucket != store.BucketDay || len(st.Series) != 7 || st.Prev == nil || st.Totals.TasksCreated != 1 {
		t.Fatalf("%s", raw)
	}

	// all: fara comparatie; slice-urile goale vin ca [] nu null
	code, raw = e.do(t, "GET", "/api/projects/"+itoa(p.ID)+"/stats?range=all", nil)
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil || code != http.StatusOK {
		t.Fatalf("%d %s", code, raw)
	}
	if m["prev"] != nil || m["topFiles"] == nil || m["topTasks"] == nil {
		t.Fatalf("%s", raw)
	}

	if code, raw := e.do(t, "GET", "/api/projects/"+itoa(p.ID)+"/stats?range=90d", nil); code != http.StatusBadRequest {
		t.Fatalf("range necunoscut: %d %s", code, raw)
	}
	if code, _ := e.do(t, "GET", "/api/projects/99999/stats", nil); code != http.StatusNotFound {
		t.Fatalf("proiect inexistent: %d", code)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/web -run 'StatsRange|ProjectStatsAPI' -v`
Expected: FAIL la compilare (`undefined: statsRange`).

- [ ] **Step 3: Write the implementation**

`internal/web/api_stats.go`:

```go
package web

import (
	"fmt"
	"net/http"
	"time"

	"foreman/internal/store"
)

// statsWeekAfter: peste atatea zile, range-ul all se grupeaza pe saptamani.
const statsWeekAfter = 90 * 24 * time.Hour

func midnight(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}

// statsRange: inceputul perioadei si bucket-ul pentru ?range= (sfarsitul e mereu now).
// first = prima activitate a proiectului, folosita doar de all (zero = nimic inca).
func statsRange(name string, now, first time.Time) (from time.Time, bucket string, ok bool) {
	today := midnight(now)
	switch name {
	case "today":
		return today, store.BucketHour, true
	case "7d":
		return today.AddDate(0, 0, -6), store.BucketDay, true
	case "30d":
		return today.AddDate(0, 0, -29), store.BucketDay, true
	case "all":
		from = today
		if !first.IsZero() {
			from = midnight(first.In(now.Location()))
		}
		if now.Sub(from) > statsWeekAfter {
			return from, store.BucketWeek, true
		}
		return from, store.BucketDay, true
	}
	return time.Time{}, "", false
}

// getProjectStats: tab-ul Stats; perioada precedenta are aceeasi lungime (lipseste la all).
func (s *Server) getProjectStats(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		fail(w, err)
		return
	}
	if _, err := s.st.GetProject(id); err != nil {
		fail(w, err)
		return
	}
	name := r.URL.Query().Get("range")
	if name == "" {
		name = "7d"
	}
	now := time.Now()
	var first time.Time
	if name == "all" {
		if first, err = s.st.ProjectFirstActivity(id); err != nil {
			fail(w, err)
			return
		}
	}
	from, bucket, ok := statsRange(name, now, first)
	if !ok {
		writeErr(w, http.StatusBadRequest, fmt.Sprintf("unknown range %q", name))
		return
	}
	stats, err := s.st.ProjectStats(id, from, now, bucket)
	if err != nil {
		fail(w, err)
		return
	}
	if name != "all" {
		prev, err := s.st.ProjectStatsTotals(id, from.Add(-now.Sub(from)), from)
		if err != nil {
			fail(w, err)
			return
		}
		stats.Prev = &prev
	}
	writeJSON(w, http.StatusOK, stats)
}
```

In `internal/web/server.go`, dupa linia `mux.HandleFunc("POST /api/projects/{id}/open", s.openProject)`:

```go
	mux.HandleFunc("GET /api/projects/{id}/stats", s.getProjectStats)
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/web -run 'StatsRange|ProjectStatsAPI' -v`
Expected: PASS. Apoi `go test ./...` si `go vet ./...`: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/web/api_stats.go internal/web/stats_test.go internal/web/server.go
git commit -m "web: GET /api/projects/{id}/stats cu perioada si perioada precedenta"
```

---

### Task 3: UI: tab-ul Stats

**Files:**
- Create: `internal/web/static/js/stats.js`
- Modify: `internal/web/static/js/workspace.js` (import, `TABS` la linia ~42, `bodies` la linia ~209, `update` la linia ~493, finalul `workspaceView` inainte de `return { cleanup, update };`)
- Modify: `internal/web/static/app.css` (dupa blocul `.files-body`, linia ~398)
- Modify: `CLAUDE.md` (sectiunea "Stare")

**Interfaces:**
- Consumes: JSON-ul din Task 2 (`from, to, bucket, totals, prev, series[{start, costBy, created, done}], agents[{agentId, name, runs, failed, costUsd, tokens, seconds, tasksDone, sendBacks, filesChanged}], topTasks[{taskId, number, title, status, costUsd, runs}], topFiles[{path, changes}]`); `api` din `api.js`; `el`, `svg`, `toneOf`, `kindOf` din `ui.js`.
- Produces: `export function statsView(projectId, { agentKind }) -> { el, load }`; `el` e corpul tab-ului (`section.tab-body.stats-body`), `load()` reincarca datele.

- [ ] **Step 1: Write `stats.js`**

`internal/web/static/js/stats.js`:

```js
// Tab-ul Stats al unui proiect: cheltuiala, performanta echipei si activitate pe o perioada.
// Graficele sunt SVG desenat de mana, fara librarii.
import { api } from "./api.js";
import { el, svg, toneOf } from "./ui.js";

const RANGES = [["today", "Today"], ["7d", "7 days"], ["30d", "30 days"], ["all", "All time"]];
const PREV_LABEL = { today: "vs yesterday", "7d": "vs prev 7 days", "30d": "vs prev 30 days" };
const RANGE_KEY = "foreman-stats-range";

function savedRange() {
  try {
    const v = localStorage.getItem(RANGE_KEY);
    return RANGES.some(([id]) => id === v) ? v : "7d";
  } catch { return "7d"; }
}
function saveRange(v) {
  try { localStorage.setItem(RANGE_KEY, v); } catch {}
}

// ---------- formatare ----------
const money = (v) => "$" + (v || 0).toFixed(2);
function compact(n) {
  n = n || 0;
  if (n >= 1e6) return (n / 1e6).toFixed(1) + "M";
  if (n >= 1e3) return (n / 1e3).toFixed(1) + "k";
  return String(n);
}
// duration: 2d 4h, 3h 12m, 45m, 30s
function duration(sec) {
  sec = Math.round(sec || 0);
  const d = Math.floor(sec / 86400), h = Math.floor(sec % 86400 / 3600), m = Math.floor(sec % 3600 / 60);
  if (d) return `${d}d ${h}h`;
  if (h) return `${h}h ${m}m`;
  if (m) return `${m}m`;
  return `${sec}s`;
}
const successRate = (t) => (t.runsDone + t.runsFailed ? t.runsDone / (t.runsDone + t.runsFailed) : null);

function bucketLabel(iso, bucket) {
  const d = new Date(iso);
  if (bucket === "hour") return d.toLocaleTimeString("en", { hour: "2-digit", minute: "2-digit", hour12: false });
  return d.toLocaleDateString("en", { month: "short", day: "numeric" });
}

// ---------- grafice ----------
const W = 720, H = 200, PAD = { l: 48, r: 8, t: 10, b: 26 };

// niceMax: o valoare rotunda >= v pentru axa Y
function niceMax(v) {
  if (!(v > 0)) return 1;
  const p = 10 ** Math.floor(Math.log10(v));
  return [1, 2, 2.5, 5, 10].map((m) => m * p).find((x) => x >= v);
}

// axes: liniile orizontale cu etichete pe Y si etichetele bucket-urilor pe X (max ~8)
function axes(series, bucket, max, fmt) {
  const iw = W - PAD.l - PAD.r, ih = H - PAD.t - PAD.b, slot = iw / series.length;
  const out = [];
  for (let k = 0; k <= 4; k++) {
    const y = PAD.t + ih - (ih * k) / 4;
    out.push(svg("line", { x1: PAD.l, x2: W - PAD.r, y1: y, y2: y, class: "stats-grid" }));
    out.push(svg("text", { x: PAD.l - 6, y: y + 4, "text-anchor": "end", class: "stats-axis" }, fmt((max * k) / 4)));
  }
  const step = Math.ceil(series.length / 8);
  series.forEach((b, i) => {
    if (i % step) return;
    out.push(svg("text", { x: PAD.l + slot * (i + 0.5), y: H - 8, "text-anchor": "middle", class: "stats-axis" }, bucketLabel(b.start, bucket)));
  });
  return out;
}

// spendChart: bare stivuite pe agent, culorile avatarelor
function spendChart(d, agentKind) {
  const s = d.series;
  const totals = s.map((b) => Object.values(b.costBy || {}).reduce((a, v) => a + v, 0));
  const max = niceMax(Math.max(0, ...totals));
  const iw = W - PAD.l - PAD.r, ih = H - PAD.t - PAD.b, slot = iw / s.length;
  const bw = Math.max(2, Math.min(28, slot * 0.7));
  const y = (v) => PAD.t + ih - (v / max) * ih;
  const bars = [];
  s.forEach((b, i) => {
    const x = PAD.l + i * slot + (slot - bw) / 2;
    let acc = 0;
    for (const a of d.agents) {
      const v = (b.costBy || {})[a.agentId] || 0;
      if (!v) continue;
      bars.push(svg("rect", { x, y: y(acc + v), width: bw, height: Math.max(1, y(acc) - y(acc + v)), fill: toneOf(a.name, agentKind(a.agentId))[0] },
        svg("title", {}, `${a.name}: ${money(v)} · ${bucketLabel(b.start, d.bucket)}`)));
      acc += v;
    }
  });
  return svg("svg", { viewBox: `0 0 ${W} ${H}`, class: "stats-chart", role: "img", "aria-label": "Spend over time" },
    axes(s, d.bucket, max, money), bars);
}

// throughputChart: doua linii, task-uri create si terminate
function throughputChart(d) {
  const s = d.series;
  const max = niceMax(Math.max(0, ...s.map((b) => Math.max(b.created, b.done))));
  const iw = W - PAD.l - PAD.r, ih = H - PAD.t - PAD.b, slot = iw / s.length;
  const pt = (i, v) => [PAD.l + slot * (i + 0.5), PAD.t + ih - (v / max) * ih];
  const line = (key) => [
    svg("polyline", { class: `stats-line ${key}`, points: s.map((b, i) => pt(i, b[key]).join(",")).join(" ") }),
    ...s.map((b, i) => {
      const [cx, cy] = pt(i, b[key]);
      return svg("circle", { cx, cy, r: 3, class: `stats-dot ${key}` },
        svg("title", {}, `${key === "created" ? "Created" : "Done"}: ${b[key]} · ${bucketLabel(b.start, d.bucket)}`));
    }),
  ];
  return svg("svg", { viewBox: `0 0 ${W} ${H}`, class: "stats-chart", role: "img", "aria-label": "Tasks created and done" },
    axes(s, d.bucket, max, (v) => (Number.isInteger(v) ? String(v) : "")), line("created"), line("done"));
}

// ---------- sectiuni ----------
const TILES = [
  ["Spend", (t) => t.costUsd, money],
  ["Tokens", (t) => t.tokens, compact],
  ["Tasks done", (t) => t.tasksDone, String],
  ["Median time to done", (t) => t.medianDoneSec || null, duration],
  ["Run success", successRate, (v) => Math.round(v * 100) + "%"],
  ["Questions to you", (t) => t.questions, String],
];

function tiles(d, range) {
  return el("div", { class: "stats-tiles" }, TILES.map(([label, get, fmt]) => {
    const cur = get(d.totals);
    const prev = d.prev ? get(d.prev) : null;
    let delta = null;
    if (prev && cur != null) {
      const pct = Math.round(((cur - prev) / prev) * 100);
      delta = el("div", { class: "stats-delta" }, `${pct > 0 ? "+" : ""}${pct}% ${PREV_LABEL[range]}`);
    }
    return el("div", { class: "stats-tile" },
      el("div", { class: "stats-tile-label" }, label),
      el("div", { class: "stats-tile-value" }, cur == null ? "—" : fmt(cur)),
      delta);
  }));
}

function legend(d, agentKind) {
  return el("div", { class: "stats-legend" }, d.agents.filter((a) => a.costUsd > 0).map((a) =>
    el("span", {}, el("i", { style: `background:${toneOf(a.name, agentKind(a.agentId))[0]}` }), a.name)));
}

const COLS = [
  ["name", "Agent", null], ["runs", "Runs", String], ["failed", "Failed", String], ["costUsd", "Cost", money],
  ["tokens", "Tokens", compact], ["seconds", "Work time", duration], ["tasksDone", "Done", String],
  ["sendBacks", "Sent back", String], ["filesChanged", "Files", String],
];

function card(title, ...children) {
  return el("section", { class: "stats-card" }, el("h3", {}, title), children);
}

// statsView: corpul tab-ului. load() aduce datele pentru perioada aleasa.
// agentKind(id) -> "lead" / "worker", pentru culoarea agentului.
export function statsView(projectId, { agentKind }) {
  const state = { range: savedRange(), sort: "costUsd", data: null };
  const rangeBar = el("div", { class: "stats-ranges", role: "group", "aria-label": "Time range" });
  const content = el("div", { class: "stats-content" });
  const root = el("section", { class: "tab-body stats-body" },
    el("div", { class: "stats-toolbar" }, el("h2", {}, "Stats"), el("span", { class: "grow" }), rangeBar),
    content);
  let seq = 0; // un raspuns vechi (alt range) nu suprascrie unul nou

  function renderRanges() {
    rangeBar.replaceChildren(...RANGES.map(([id, label]) => el("button", {
      type: "button", class: "btn small ghost", "aria-pressed": String(id === state.range),
      onclick: () => { if (id === state.range) return; state.range = id; saveRange(id); load(); },
    }, label)));
  }

  async function load() {
    renderRanges();
    const my = ++seq;
    if (!state.data) content.replaceChildren(el("p", { class: "muted" }, "Loading…"));
    content.classList.add("loading");
    try {
      const data = await api(`/api/projects/${projectId}/stats?range=${state.range}`);
      if (my !== seq) return;
      state.data = data;
      render();
    } catch (err) {
      if (my !== seq) return;
      content.replaceChildren(el("div", { class: "stats-error" },
        el("p", {}, `Couldn't load stats: ${err.message}`),
        el("button", { type: "button", class: "btn small", onclick: load }, "Retry")));
    } finally {
      if (my === seq) content.classList.remove("loading");
    }
  }

  function agentsTable(d) {
    const key = state.sort;
    const rows = [...d.agents].sort((a, b) => (key === "name" ? a.name.localeCompare(b.name) : b[key] - a[key]));
    return el("div", { class: "stats-table-wrap" }, el("table", { class: "stats-table" },
      el("thead", {}, el("tr", {}, COLS.map(([k, label]) => el("th", { "aria-sort": k === key ? (k === "name" ? "ascending" : "descending") : null },
        el("button", { type: "button", onclick: () => { state.sort = k; render(); } }, label))))),
      el("tbody", {}, rows.map((a) => el("tr", {}, COLS.map(([k, , fmt]) => el("td", {},
        fmt ? fmt(a[k]) : el("a", { href: `#/p/${projectId}/team/${a.agentId}` }, a.name))))))));
  }

  function topTasks(d) {
    if (!d.topTasks.length) return el("p", { class: "muted" }, "No task runs in this range.");
    return el("ol", { class: "stats-list" }, d.topTasks.map((t) => el("li", {},
      el("a", { href: `#/p/${projectId}/tasks/${t.taskId}` }, el("span", { class: "muted" }, `T-${t.number} `), t.title),
      el("span", { class: "grow" }),
      el("span", { class: "muted" }, `${t.runs} ${t.runs === 1 ? "run" : "runs"}`),
      el("strong", {}, money(t.costUsd)))));
  }

  function activity(d) {
    const t = d.totals;
    return [
      el("div", { class: "stats-facts" },
        el("div", {}, el("strong", {}, String(t.filesChanged)), " files changed"),
        el("div", {}, el("strong", {}, String(t.commands)), " commands", t.commandsFailed ? el("span", { class: "muted" }, ` (${t.commandsFailed} failed)`) : null),
        el("div", {}, el("strong", {}, String(t.questions)), " questions asked · ", el("strong", {}, String(t.answered)), " answered")),
      d.topFiles.length
        ? el("ol", { class: "stats-list" }, d.topFiles.map((f) => el("li", {},
          el("code", { title: f.path }, f.path), el("span", { class: "grow" }), el("span", { class: "muted" }, `${f.changes}×`))))
        : null,
    ];
  }

  function render() {
    const d = state.data;
    const t = d.totals;
    const empty = !d.agents.length && !t.tasksCreated && !t.tasksDone && !t.filesChanged && !t.commands && !t.questions && !t.answered;
    if (empty) {
      content.replaceChildren(tiles(d, state.range), el("p", { class: "stats-empty muted" }, "No activity in this range."));
      return;
    }
    content.replaceChildren(
      tiles(d, state.range),
      el("div", { class: "stats-grid-2" },
        card("Spend over time", spendChart(d, agentKind), legend(d, agentKind)),
        card("Throughput", throughputChart(d),
          el("div", { class: "stats-legend" },
            el("span", {}, el("i", { class: "created" }), "Created"),
            el("span", {}, el("i", { class: "done" }), "Done")))),
      card("Agents", agentsTable(d)),
      el("div", { class: "stats-grid-2" },
        card("Top tasks by cost", topTasks(d)),
        card("Activity", activity(d))));
  }

  return { el: root, load };
}
```

- [ ] **Step 2: Verifica sintaxa**

Run: `cp internal/web/static/js/stats.js "$TMPDIR/stats.mjs" && node --check "$TMPDIR/stats.mjs"`
Expected: fara output, exit 0.

- [ ] **Step 3: Leaga tab-ul in `workspace.js`**

1. Importul, langa celelalte (dupa `import { taskModal } from "./task-modal.js";`):

```js
import { statsView } from "./stats.js";
```

2. In `TABS`, dupa `{ id: "files", label: "Files" },`:

```js
  { id: "stats", label: "Stats" },
```

3. Inainte de `const bodies = {`:

```js
  // tab-ul Stats: datele se incarca doar la intrarea pe tab (fara auto-refresh)
  const stats = statsView(projectId, {
    agentKind: (id) => kindOf(state.project.agents.find((a) => a.id === id)),
  });
```

si in `bodies`, dupa linia `"files": ...`:

```js
    "stats": stats.el,
```

4. In `update(r)`, chiar inainte de `state.tab = r.tab;`:

```js
    const enteringStats = r.tab === "stats" && state.tab !== "stats";
```

iar dupa apelul `renderTabs();` care urmeaza lui `if (keep) { ... }` (cel din finalul lui `update`, langa `syncHash();`):

```js
    if (enteringStats) stats.load();
```

5. In finalul `workspaceView`, chiar inainte de `return { cleanup, update };`:

```js
  if (state.tab === "stats") stats.load(); // pagina deschisa direct pe #/p/3/stats
```

Apoi: `cp internal/web/static/js/workspace.js "$TMPDIR/workspace.mjs" && node --check "$TMPDIR/workspace.mjs"`. Expected: exit 0.

- [ ] **Step 4: CSS**

In `internal/web/static/app.css`, dupa linia `.files-body { max-width: 900px; }`:

```css
/* tab-ul Stats */
.stats-body { max-width: 1200px; display: flex; flex-direction: column; gap: 16px; }
.stats-toolbar { display: flex; align-items: center; gap: 12px; flex-wrap: wrap; }
.stats-toolbar .grow { flex: 1; }
.stats-ranges { display: flex; gap: 4px; flex-wrap: wrap; }
.stats-content { display: flex; flex-direction: column; gap: 16px; transition: opacity .15s; }
.stats-content.loading { opacity: .55; }
.stats-tiles { display: grid; grid-template-columns: repeat(auto-fit, minmax(160px, 1fr)); gap: 12px; }
.stats-tile { background: var(--col); border: 1px solid var(--line); border-radius: var(--radius); padding: 14px 16px; min-width: 0; }
.stats-tile-label { font-size: 12px; color: var(--muted); }
.stats-tile-value { font: 650 24px/1.2 var(--display); margin-top: 4px; font-variant-numeric: tabular-nums; }
.stats-delta { font-size: 12px; color: var(--dim); margin-top: 2px; }
.stats-grid-2 { display: grid; grid-template-columns: repeat(auto-fit, minmax(min(100%, 420px), 1fr)); gap: 16px; }
.stats-card { background: var(--col); border: 1px solid var(--line); border-radius: var(--radius); padding: 16px; min-width: 0; }
.stats-card h3 { margin: 0 0 10px; font-size: 14px; font-weight: 600; }
.stats-chart { width: 100%; height: auto; display: block; }
.stats-grid { stroke: var(--line); stroke-width: 1; }
.stats-axis { fill: var(--dim); font: 11px var(--sans); }
.stats-line { fill: none; stroke-width: 2; }
.stats-line.created, .stats-dot.created { stroke: var(--blue); }
.stats-line.done, .stats-dot.done { stroke: var(--teal); }
.stats-dot { fill: var(--col); stroke-width: 2; }
.stats-legend { display: flex; flex-wrap: wrap; gap: 6px 14px; margin-top: 8px; font-size: 12px; color: var(--soft); }
.stats-legend span { display: inline-flex; align-items: center; gap: 6px; }
.stats-legend i { width: 10px; height: 10px; border-radius: 3px; display: inline-block; }
.stats-legend i.created { background: var(--blue); }
.stats-legend i.done { background: var(--teal); }
.stats-table-wrap { overflow-x: auto; }
.stats-table { width: 100%; border-collapse: collapse; font-size: 13px; font-variant-numeric: tabular-nums; }
.stats-table th, .stats-table td { padding: 7px 10px; text-align: right; border-bottom: 1px solid var(--line); white-space: nowrap; }
.stats-table th:first-child, .stats-table td:first-child { text-align: left; }
.stats-table th button { all: unset; cursor: pointer; color: var(--muted); font-weight: 600; font-size: 12px; }
.stats-table th[aria-sort] button { color: var(--text); }
.stats-table th button:focus-visible { outline: 2px solid var(--amber); outline-offset: 2px; }
.stats-table a { color: var(--text); text-decoration: none; }
.stats-table a:hover { color: var(--amber); }
.stats-list { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: 2px; }
.stats-list li { display: flex; align-items: center; gap: 10px; padding: 6px 0; border-bottom: 1px solid var(--line); min-width: 0; }
.stats-list li:last-child { border-bottom: 0; }
.stats-list .grow { flex: 1; }
.stats-list a { color: var(--text); text-decoration: none; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; min-width: 0; }
.stats-list a:hover { color: var(--amber); }
.stats-list code { font: 12px var(--mono); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; min-width: 0; direction: rtl; text-align: left; }
.stats-facts { display: flex; flex-direction: column; gap: 6px; margin-bottom: 12px; color: var(--soft); }
.stats-facts strong { color: var(--text); font-variant-numeric: tabular-nums; }
.stats-empty, .stats-error { padding: 40px 0; text-align: center; }
.stats-error { display: flex; flex-direction: column; align-items: center; gap: 10px; color: var(--red); }
```

Nota: `direction: rtl` pe caile de fisier taie inceputul caii, nu numele fisierului.

- [ ] **Step 5: Verificare in browser (obligatoriu, vezi memoria "verifica UI-ul cu screenshot-uri")**

1. `go build -o /tmp/foreman-stats ./cmd/foreman` si porneste-l cu o baza de date care are activitate (copie a `~/.foreman/foreman.db` intr-un HOME temporar, ca sa nu atingi datele reale; vezi cum alege `cmd/foreman` folderul de date).
2. Cu Chrome headless prin CDP, deschide `http://127.0.0.1:7777/#/p/<id>/stats`, fa screenshot la 1440×900 si la 390×844.
3. Verifica: tile-urile, cele doua grafice cu bare/linii vizibile, tabelul sortabil (click pe "Runs" schimba ordinea), click pe un agent duce la `#/p/<id>/team/<agentId>`, click pe un task deschide modala, schimbarea perioadei se pastreaza dupa refresh, si la 390px nu exista scroll orizontal pe pagina (`document.documentElement.scrollWidth <= innerWidth`).
4. Un proiect fara activitate arata "No activity in this range.".

- [ ] **Step 6: Verificari finale si CLAUDE.md**

In `CLAUDE.md`, sub ultima linie "Gata (...)" din "Stare", adauga:

```
Gata (Stats, spec in docs/superpowers/specs/2026-10-08-foreman-project-stats-design.md): tab-ul Stats pe proiect
(Today / 7d / 30d / All): cheltuiala si tokeni pe agent si pe zi, task-uri create/terminate, mediana pana la Done,
rata de succes a run-urilor, fisiere si comenzi, intrebari; store.ProjectStats + GET /api/projects/{id}/stats, grafice SVG in stats.js.
```

Run:

```bash
go vet ./... && go build -race ./... && go test ./... \
  && GOOS=darwin GOARCH=arm64 go build -o /dev/null ./cmd/foreman \
  && GOOS=windows go build -o /dev/null ./cmd/foreman
```

Expected: totul trece.

- [ ] **Step 7: Commit**

```bash
git add internal/web/static/js/stats.js internal/web/static/js/workspace.js internal/web/static/app.css CLAUDE.md
git commit -m "ui: tab-ul Stats pe proiect (tile-uri, grafice SVG, agenti, topuri)"
```
