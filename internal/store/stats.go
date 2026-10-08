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
