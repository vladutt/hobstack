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
	addRunAt(t, s, "r5", lead.ID, 0, RunDone, h(-1), 5, 100, 10)    // inainte de perioada
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
	addRunAt(t, s, "at-from", lead.ID, 0, RunDone, from, 1, 1, 0)                        // "...:05Z": intra
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
