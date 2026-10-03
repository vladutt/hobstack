# Sarcini recurente: plan de implementare

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Utilizatorul defineste per proiect sarcini care creeaza singure task-uri pe un program (la N ore, zilnic, saptamanal), cu auto-accept optional.

**Architecture:** Pachet nou pur `internal/schedule` (validare, urmatoarea rulare, text). Store: tabel `recurring_tasks` + `tasks.recurring_id` / `tasks.auto_accept` (migrarea 014). Team: CRUD + `RunDue` pe ticker-ul existent din `Run` (si o data la pornire), task-urile create prin corpul lui `UserCreateTask`; auto-accept in locurile unde azi decide utilizatorul. Web: rute CRUD + `run`, lista in detaliul proiectului. UI: panou `Recurring` deasupra board-ului (fisier nou `recurring.js`), `↻` pe card, link in modala.

**Tech Stack:** Go (stdlib), SQLite (modernc.org/sqlite), vanilla JS.

**Spec:** `docs/superpowers/specs/2026-10-03-foreman-recurring-tasks-design.md`

## Global Constraints

- `go vet ./...`, `go build -race ./cmd/foreman`, `go test ./...` trec; cross-compile `GOOS=darwin GOARCH=arm64 go build ./cmd/foreman` si `GOOS=windows go build ./cmd/foreman`.
- JS verificat ca modul: `cp f.js $TMP/f.mjs && node --check $TMP/f.mjs`. `cond ? x : null` pentru copii dati lui `el()`; `replaceChildren`/`append` native nu aplatizeaza array-uri si scriu `null` ca text (filtreaza).
- SQLite cu o singura conexiune: in tranzactie doar `tx`; nicio alta interogare cat timp un `*sql.Rows` e deschis.
- Team: tot ce schimba starea ruleaza sub `t.work`; helper-ele chemate cu `t.work` tinut nu iau lock-ul din nou.
- Ora programului e `time.Local`; in baza de date timpii sunt RFC3339Nano UTC (helper-ele existente `now()` / `parseTime`).
- Texte vizibile in engleza, exact ca in spec; comentarii in romana fara diacritice; dependente noi: niciuna.
- Fara dependente noi; fara schimbari la apelantii existenti ai `UserCreateTask`.

## Review Focus

1. **Laptop inchis peste mai multe rulari:** la pornire apare un singur task, nu cate unul per rulare ratata, iar urmatoarea rulare e in viitor. Test in Task 3 (`TestRunDueMissedRunsCreateOne`).
2. **Agent blocat pe o sarcina orara:** nu se aduna task-uri; fiecare tur sarit lasa nota, iar dupa Done urmatorul tur creeaza din nou. Test in Task 3 (`TestRunDueSkipsWhileOpen`).
3. **Editarea unei sarcini in timp ce scheduler-ul ruleaza** nu pierde editarea (scheduler-ul reciteste sub `t.work`). Test in Task 3 (`TestRunDueRereadsUnderLock`: sarcina dezactivata intre `DueRecurring` si creare nu produce task).
4. **Ora de vara:** o sarcina zilnica la 03:30 in ziua trecerii (Europe/Bucharest, 29 martie 2026) ruleaza o data, in acea zi, dupa `after`. Test in Task 1.
5. **Agent sters / scos din prima etapa:** sarcina se opreste sau noteaza eroarea, fara panica si fara bucla; celelalte sarcini din acelasi tur ruleaza. Test in Task 3 (`TestRunDueAssigneeDeleted`, `TestRunDueErrorDoesNotStopOthers`).

---

### Task 1: Pachetul `internal/schedule`

**Files:**
- Create: `internal/schedule/schedule.go`
- Test: `internal/schedule/schedule_test.go`

**Interfaces:**
- Produces: `schedule.Spec{Kind string; EveryHours int; At string; Weekdays int}` (tag-uri JSON `kind, everyHours, at, weekdays`), constantele `KindEveryHours = "every_hours"`, `KindDaily = "daily"`, `KindWeekly = "weekly"`, metodele `Validate() error`, `Next(after time.Time) time.Time`, `String() string`. `Spec` e comparabil cu `==`.

- [ ] **Step 1: Testele** (`schedule_test.go`, `package schedule`):

```go
package schedule

import (
	"testing"
	"time"
)

func bucharest(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("Europe/Bucharest")
	if err != nil {
		t.Skip("fara tzdata:", err)
	}
	return loc
}

func TestNext(t *testing.T) {
	loc := bucharest(t)
	at := func(s string) time.Time {
		v, err := time.ParseInLocation("2006-01-02 15:04", s, loc)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	weekdays := 0b0111110 // L-V
	cases := []struct {
		name  string
		spec  Spec
		after string
		want  string
	}{
		{"la 3 ore, aliniat la ora fixa", Spec{Kind: KindEveryHours, EveryHours: 3}, "2026-10-05 10:20", "2026-10-05 13:00"},
		{"la 3 ore peste miezul noptii", Spec{Kind: KindEveryHours, EveryHours: 3}, "2026-10-05 22:40", "2026-10-06 01:00"},
		{"la 24 de ore", Spec{Kind: KindEveryHours, EveryHours: 24}, "2026-10-05 09:00", "2026-10-06 09:00"},
		{"zilnic, ora n-a trecut", Spec{Kind: KindDaily, At: "09:00"}, "2026-10-05 08:59", "2026-10-05 09:00"},
		{"zilnic, fix la ora: strict dupa", Spec{Kind: KindDaily, At: "09:00"}, "2026-10-05 09:00", "2026-10-06 09:00"},
		{"zilnic, ora a trecut", Spec{Kind: KindDaily, At: "09:00"}, "2026-10-05 17:00", "2026-10-06 09:00"},
		{"L-V vineri seara -> luni", Spec{Kind: KindWeekly, At: "09:00", Weekdays: weekdays}, "2026-10-09 18:00", "2026-10-12 09:00"},
		{"doar duminica", Spec{Kind: KindWeekly, At: "20:30", Weekdays: 1}, "2026-10-05 10:00", "2026-10-11 20:30"},
		{"azi e permis si ora n-a trecut", Spec{Kind: KindWeekly, At: "23:00", Weekdays: 1 << time.Monday}, "2026-10-05 22:00", "2026-10-05 23:00"},
	}
	for _, c := range cases {
		got := c.spec.Next(at(c.after))
		if !got.Equal(at(c.want)) {
			t.Errorf("%s: %v, vreau %s", c.name, got, c.want)
		}
	}
}

// 29 martie 2026: in Romania ora 03:00 sare la 04:00; 03:30 nu exista
func TestNextDaylightSaving(t *testing.T) {
	loc := bucharest(t)
	after := time.Date(2026, 3, 29, 1, 0, 0, 0, loc)
	got := Spec{Kind: KindDaily, At: "03:30"}.Next(after)
	if !got.After(after) || got.Day() != 29 {
		t.Fatalf("%v: trebuia sa ruleze o data, pe 29, dupa %v", got, after)
	}
	if again := (Spec{Kind: KindDaily, At: "03:30"}).Next(got); again.Day() != 30 {
		t.Fatalf("urmatoarea dupa %v e %v, vreau 30", got, again)
	}
}

func TestValidate(t *testing.T) {
	ok := []Spec{
		{Kind: KindEveryHours, EveryHours: 1}, {Kind: KindEveryHours, EveryHours: 24},
		{Kind: KindDaily, At: "00:00"}, {Kind: KindDaily, At: "23:59"},
		{Kind: KindWeekly, At: "09:00", Weekdays: 1}, {Kind: KindWeekly, At: "09:00", Weekdays: 127},
	}
	for _, s := range ok {
		if err := s.Validate(); err != nil {
			t.Errorf("%+v: %v", s, err)
		}
	}
	bad := []Spec{
		{}, {Kind: "monthly"}, {Kind: KindEveryHours}, {Kind: KindEveryHours, EveryHours: 25},
		{Kind: KindDaily}, {Kind: KindDaily, At: "9:00"}, {Kind: KindDaily, At: "24:00"}, {Kind: KindDaily, At: "09:60"},
		{Kind: KindWeekly, At: "09:00"}, {Kind: KindWeekly, At: "09:00", Weekdays: 128},
	}
	for _, s := range bad {
		if err := s.Validate(); err == nil {
			t.Errorf("%+v: trebuia sa fie invalid", s)
		}
	}
}

func TestString(t *testing.T) {
	cases := map[string]Spec{
		"Every hour":           {Kind: KindEveryHours, EveryHours: 1},
		"Every 3 hours":        {Kind: KindEveryHours, EveryHours: 3},
		"Daily at 09:00":       {Kind: KindDaily, At: "09:00"},
		"Weekdays at 09:00":    {Kind: KindWeekly, At: "09:00", Weekdays: 0b0111110},
		"Weekends at 10:00":    {Kind: KindWeekly, At: "10:00", Weekdays: 0b1000001},
		"Every day at 08:00":   {Kind: KindWeekly, At: "08:00", Weekdays: 127},
		"Mon, Wed, Sun at 07:15": {Kind: KindWeekly, At: "07:15", Weekdays: 1<<time.Monday | 1<<time.Wednesday | 1<<time.Sunday},
	}
	for want, s := range cases {
		if got := s.String(); got != want {
			t.Errorf("%+v: %q, vreau %q", s, got, want)
		}
	}
}
```

- [ ] **Step 2:** `go test ./internal/schedule/` -> FAIL (pachetul nu exista).

- [ ] **Step 3: Implementarea** (`schedule.go`):

```go
// Package schedule calculeaza programul sarcinilor recurente: validare,
// urmatoarea rulare si textul afisat. Functii pure, fara baza de date.
package schedule

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	KindEveryHours = "every_hours"
	KindDaily      = "daily"
	KindWeekly     = "weekly"
)

// Spec e programul unei sarcini recurente.
// Weekdays e o masca: bitul 1<<time.Weekday (duminica = bitul 0).
type Spec struct {
	Kind       string `json:"kind"`
	EveryHours int    `json:"everyHours"`
	At         string `json:"at"` // "HH:MM", 24h, ora locala
	Weekdays   int    `json:"weekdays"`
}

const (
	maskWeekdays = 0b0111110 // luni-vineri
	maskWeekends = 0b1000001 // sambata + duminica
	maskAll      = 0b1111111
)

func (s Spec) Validate() error {
	switch s.Kind {
	case KindEveryHours:
		if s.EveryHours < 1 || s.EveryHours > 24 {
			return errors.New("repeat every 1 to 24 hours")
		}
		return nil
	case KindDaily, KindWeekly:
		if _, _, ok := parseAt(s.At); !ok {
			return errors.New("time must be HH:MM")
		}
		if s.Kind == KindWeekly && (s.Weekdays < 1 || s.Weekdays > maskAll) {
			return errors.New("choose at least one day")
		}
		return nil
	}
	return fmt.Errorf("unknown schedule %q", s.Kind)
}

// parseAt citeste "HH:MM" strict (doua cifre fiecare).
func parseAt(at string) (h, m int, ok bool) {
	if len(at) != 5 {
		return 0, 0, false
	}
	v, err := time.Parse("15:04", at)
	if err != nil {
		return 0, 0, false
	}
	return v.Hour(), v.Minute(), true
}

// Next intoarce primul moment strict dupa `after`, in fusul orar al lui `after`
// (apelantii dau ora locala). Spec-ul trebuie sa fie valid.
func (s Spec) Next(after time.Time) time.Time {
	loc := after.Location()
	if s.Kind == KindEveryHours {
		n := after.Add(time.Duration(s.EveryHours) * time.Hour)
		return time.Date(n.Year(), n.Month(), n.Day(), n.Hour(), 0, 0, 0, loc) // aliniat la ora fixa
	}
	h, m, _ := parseAt(s.At)
	y, mo, d := after.Date()
	// cel mult 8 zile: azi + o saptamana intreaga
	for i := 0; i < 8; i++ {
		// time.Date normalizeaza o ora inexistenta (trecerea la ora de vara)
		c := time.Date(y, mo, d+i, h, m, 0, 0, loc)
		if c.After(after) && (s.Kind == KindDaily || s.Weekdays&(1<<c.Weekday()) != 0) {
			return c
		}
	}
	return after.Add(24 * time.Hour) // nu se ajunge aici cu un Spec valid
}

func (s Spec) String() string {
	switch s.Kind {
	case KindEveryHours:
		if s.EveryHours == 1 {
			return "Every hour"
		}
		return fmt.Sprintf("Every %d hours", s.EveryHours)
	case KindDaily:
		return "Daily at " + s.At
	case KindWeekly:
		return days(s.Weekdays) + " at " + s.At
	}
	return s.Kind
}

// days: "Weekdays", "Weekends", "Every day" sau zilele in ordinea Mon..Sun.
func days(mask int) string {
	switch mask {
	case maskAll:
		return "Every day"
	case maskWeekdays:
		return "Weekdays"
	case maskWeekends:
		return "Weekends"
	}
	var names []string
	for _, d := range []time.Weekday{time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday, time.Saturday, time.Sunday} {
		if mask&(1<<d) != 0 {
			names = append(names, d.String()[:3])
		}
	}
	return strings.Join(names, ", ")
}
```

- [ ] **Step 4:** `go test ./internal/schedule/` -> PASS. Daca `TestNextDaylightSaving` pica pentru ca `time.Date` alege 02:30 (inainte de `after` nu se poate: `after` e 01:00), ajusteaza doar asertiunea daca rezultatul e tot pe 29 si dupa `after`; comportamentul cerut e "o data, in acea zi".
- [ ] **Step 5:** `gofmt -l internal/schedule` gol; commit: `schedule: programul sarcinilor recurente`

---

### Task 2: Store: `recurring_tasks` si coloanele noi din `tasks`

**Files:**
- Create: `internal/store/migrations/014_recurring_tasks.sql`, `internal/store/recurring.go`
- Modify: `internal/store/tasks.go` (`Task`, `taskCols`, `scanTask`, `CreateTask`), `internal/store/store_test.go:63-64` (user_version 13 -> 14)
- Test: `internal/store/recurring_test.go`

**Interfaces:**
- Consumes: `schedule.Spec` (Task 1).
- Produces:
  - `store.Task` primeste `RecurringID *int64` (`json:"recurringId"`) si `AutoAccept bool` (`json:"autoAccept"`); `CreateTask` le salveaza; `SaveTask` nu le schimba.
  - `store.RecurringTask{ID, ProjectID int64; Title, Description string; AssigneeID *int64; Schedule schedule.Spec; AutoAccept, Enabled bool; NextRunAt time.Time; LastRunAt *time.Time; LastTaskID *int64; LastNote string; CreatedAt time.Time}` (JSON: `id, projectId, title, description, assigneeId, schedule, autoAccept, enabled, nextRunAt, lastRunAt, lastTaskId, lastNote, createdAt`).
  - `CreateRecurring(r *RecurringTask) error`, `GetRecurring(id int64) (RecurringTask, error)` (`ErrNotFound`), `SaveRecurring(r *RecurringTask) error` (toate campurile in afara de id/project/created; `ErrNotFound` daca lipseste), `DeleteRecurring(id int64) error`, `ListRecurring(projectID int64) ([]RecurringTask, error)` (dupa id, non-nil), `DueRecurring(now time.Time) ([]RecurringTask, error)` (enabled si `next_run_at <= now`, dupa `next_run_at`, non-nil).

- [ ] **Step 1: Migrarea** `014_recurring_tasks.sql`, exact SQL-ul din spec (sectiunea Store), cu comentariul de antet `-- sarcini care creeaza singure task-uri pe un program`. Atentie: `next_run_at` se compara ca text, deci se scrie mereu in UTC cu acelasi format (`time.RFC3339Nano` taie zerourile de la final si strica ordinea lexicala: foloseste `"2006-01-02T15:04:05.000000000Z07:00"` pentru `next_run_at`, ca sirurile sa aiba lungime fixa).

- [ ] **Step 2: Testele** (`recurring_test.go`):

```go
package store

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"foreman/internal/schedule"
)

func recurringFixture(t *testing.T) (*Store, Project, Agent) {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	p := Project{Name: "P", Workdir: t.TempDir(), MaxParallel: 1}
	lead, err := s.CreateProject(&p)
	if err != nil {
		t.Fatal(err)
	}
	return s, p, lead
}

func TestRecurringCRUD(t *testing.T) {
	s, p, lead := recurringFixture(t)
	next := time.Date(2026, 10, 5, 9, 0, 0, 0, time.UTC)
	r := RecurringTask{ProjectID: p.ID, Title: "Tests", Description: "run them", AssigneeID: &lead.ID,
		Schedule: schedule.Spec{Kind: schedule.KindDaily, At: "09:00"}, AutoAccept: true, Enabled: true, NextRunAt: next}
	if err := s.CreateRecurring(&r); err != nil || r.ID == 0 {
		t.Fatalf("%+v %v", r, err)
	}
	got, err := s.GetRecurring(r.ID)
	if err != nil || got.Title != "Tests" || got.Schedule != r.Schedule || !got.AutoAccept || !got.NextRunAt.Equal(next) || got.LastRunAt != nil {
		t.Fatalf("%+v %v", got, err)
	}
	task := Task{ProjectID: p.ID, Title: "Tests", AssigneeID: lead.ID, ByUser: true, RecurringID: &r.ID, AutoAccept: true}
	if err := s.CreateTask(&task); err != nil {
		t.Fatal(err)
	}
	ran := next.Add(time.Minute)
	got.LastRunAt, got.LastTaskID, got.LastNote, got.Enabled = &ran, &task.ID, "Skipped", false
	if err := s.SaveRecurring(&got); err != nil {
		t.Fatal(err)
	}
	list, err := s.ListRecurring(p.ID)
	if err != nil || len(list) != 1 || *list[0].LastTaskID != task.ID || list[0].Enabled || list[0].LastNote != "Skipped" {
		t.Fatalf("%+v %v", list, err)
	}
	back, _ := s.GetTask(task.ID)
	if back.RecurringID == nil || *back.RecurringID != r.ID || !back.AutoAccept {
		t.Fatalf("coloanele noi din tasks: %+v", back)
	}
	if err := s.DeleteRecurring(r.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetRecurring(r.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("dupa stergere: %v", err)
	}
	back, _ = s.GetTask(task.ID)
	if back.RecurringID != nil || !back.AutoAccept {
		t.Fatalf("task-ul ramane, fara legatura, cu auto_accept: %+v", back)
	}
}

func TestDueRecurring(t *testing.T) {
	s, p, lead := recurringFixture(t)
	now := time.Date(2026, 10, 5, 9, 0, 0, 0, time.UTC)
	mk := func(title string, next time.Time, enabled bool) {
		r := RecurringTask{ProjectID: p.ID, Title: title, AssigneeID: &lead.ID,
			Schedule: schedule.Spec{Kind: schedule.KindEveryHours, EveryHours: 1}, Enabled: enabled, NextRunAt: next}
		if err := s.CreateRecurring(&r); err != nil {
			t.Fatal(err)
		}
	}
	mk("later", now.Add(time.Second), true)
	mk("b", now, true)
	mk("a", now.Add(-time.Hour), true)
	mk("off", now.Add(-time.Hour), false)
	mk("nano", now.Add(-500*time.Millisecond), true) // ordinea nu se strica la fractiuni de secunda
	due, err := s.DueRecurring(now)
	if err != nil {
		t.Fatal(err)
	}
	var titles []string
	for _, r := range due {
		titles = append(titles, r.Title)
	}
	if len(titles) != 3 || titles[0] != "a" || titles[1] != "nano" || titles[2] != "b" {
		t.Fatalf("%v", titles)
	}
}

func TestRecurringOnDelete(t *testing.T) {
	s, p, lead := recurringFixture(t)
	dev := Agent{ProjectID: p.ID, Name: "Dev", ParentID: &lead.ID, Model: "haiku"}
	if err := s.CreateAgent(&dev); err != nil {
		t.Fatal(err)
	}
	r := RecurringTask{ProjectID: p.ID, Title: "x", AssigneeID: &dev.ID,
		Schedule: schedule.Spec{Kind: schedule.KindDaily, At: "09:00"}, Enabled: true, NextRunAt: time.Now()}
	s.CreateRecurring(&r)
	if err := s.DeleteAgent(dev.ID); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.GetRecurring(r.ID); got.AssigneeID != nil {
		t.Fatalf("agent sters -> assignee NULL: %+v", got)
	}
	if err := s.DeleteProject(p.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetRecurring(r.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("proiect sters -> sarcina stearsa: %v", err)
	}
}
```

`DeleteAgent` (`internal/store/agents.go:234`) si `DeleteProject` (`projects.go:202`) exista; agentul `Dev` nu are task-uri sau subalterni, deci stergerea lui e permisa.

- [ ] **Step 3:** `go test ./internal/store/` -> FAIL (tipuri lipsa).
- [ ] **Step 4: `tasks.go`**: adauga campurile in `Task` (dupa `Escalated`):

```go
	RecurringID *int64 `json:"recurringId"` // sarcina recurenta care l-a creat; nil = creat altfel sau sarcina stearsa
	AutoAccept  bool   `json:"autoAccept"`  // la final trece singur in Done, in loc sa astepte utilizatorul
```

`taskCols` primeste la final `, recurring_id, auto_accept`; `scanTask` scaneaza `&t.RecurringID, &t.AutoAccept` dupa `&t.Escalated`; `CreateTask` adauga coloanele in INSERT (`..., by_user, recurring_id, auto_accept, created_at, updated_at`) cu `nullPtr(t.RecurringID), t.AutoAccept`.

- [ ] **Step 5: `recurring.go`**:

```go
package store

import (
	"database/sql"
	"errors"
	"time"

	"foreman/internal/schedule"
)

// RecurringTask e o sarcina care creeaza singura task-uri pe un program.
type RecurringTask struct {
	ID          int64         `json:"id"`
	ProjectID   int64         `json:"projectId"`
	Title       string        `json:"title"`
	Description string        `json:"description"`
	AssigneeID  *int64        `json:"assigneeId"` // nil = prima etapa (sau agentul a fost sters)
	Schedule    schedule.Spec `json:"schedule"`
	AutoAccept  bool          `json:"autoAccept"`
	Enabled     bool          `json:"enabled"`
	NextRunAt   time.Time     `json:"nextRunAt"`
	LastRunAt   *time.Time    `json:"lastRunAt"`
	LastTaskID  *int64        `json:"lastTaskId"`
	LastNote    string        `json:"lastNote"`
	CreatedAt   time.Time     `json:"createdAt"`
}

// runAtLayout are lungime fixa: next_run_at se compara ca text in DueRecurring.
const runAtLayout = "2006-01-02T15:04:05.000000000Z07:00"

func runAt(t time.Time) string { return t.UTC().Format(runAtLayout) }

const recurringCols = `id, project_id, title, description, assignee_id, kind, every_hours, at, weekdays,
	auto_accept, enabled, next_run_at, last_run_at, last_task_id, last_note, created_at`

func scanRecurring(sc scanner) (RecurringTask, error) {
	var r RecurringTask
	var next, created string
	var last sql.NullString
	err := sc.Scan(&r.ID, &r.ProjectID, &r.Title, &r.Description, &r.AssigneeID,
		&r.Schedule.Kind, &r.Schedule.EveryHours, &r.Schedule.At, &r.Schedule.Weekdays,
		&r.AutoAccept, &r.Enabled, &next, &last, &r.LastTaskID, &r.LastNote, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return RecurringTask{}, ErrNotFound
	}
	if err != nil {
		return RecurringTask{}, err
	}
	r.NextRunAt, r.CreatedAt = parseTime(next), parseTime(created)
	if last.Valid {
		at := parseTime(last.String)
		r.LastRunAt = &at
	}
	return r, nil
}

func (s *Store) CreateRecurring(r *RecurringTask) error {
	ts := now()
	res, err := s.db.Exec(`INSERT INTO recurring_tasks (project_id, title, description, assignee_id, kind, every_hours, at, weekdays,
		auto_accept, enabled, next_run_at, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.ProjectID, r.Title, r.Description, nullPtr(r.AssigneeID), r.Schedule.Kind, r.Schedule.EveryHours, r.Schedule.At,
		r.Schedule.Weekdays, r.AutoAccept, r.Enabled, runAt(r.NextRunAt), ts)
	if err != nil {
		return err
	}
	r.CreatedAt = parseTime(ts)
	r.ID, err = res.LastInsertId()
	return err
}

func (s *Store) GetRecurring(id int64) (RecurringTask, error) {
	return scanRecurring(s.db.QueryRow(`SELECT `+recurringCols+` FROM recurring_tasks WHERE id = ?`, id))
}

// SaveRecurring scrie tot ce se poate schimba (editare din UI sau o rulare).
func (s *Store) SaveRecurring(r *RecurringTask) error {
	var last any
	if r.LastRunAt != nil {
		last = runAt(*r.LastRunAt)
	}
	return mustAffect(s.db.Exec(`UPDATE recurring_tasks SET title = ?, description = ?, assignee_id = ?, kind = ?, every_hours = ?,
		at = ?, weekdays = ?, auto_accept = ?, enabled = ?, next_run_at = ?, last_run_at = ?, last_task_id = ?, last_note = ?
		WHERE id = ?`, r.Title, r.Description, nullPtr(r.AssigneeID), r.Schedule.Kind, r.Schedule.EveryHours, r.Schedule.At,
		r.Schedule.Weekdays, r.AutoAccept, r.Enabled, runAt(r.NextRunAt), last, nullPtr(r.LastTaskID), r.LastNote, r.ID))
}

func (s *Store) DeleteRecurring(id int64) error {
	return mustAffect(s.db.Exec(`DELETE FROM recurring_tasks WHERE id = ?`, id))
}

func (s *Store) ListRecurring(projectID int64) ([]RecurringTask, error) {
	return s.listRecurring(`SELECT `+recurringCols+` FROM recurring_tasks WHERE project_id = ? ORDER BY id`, projectID)
}

// DueRecurring: sarcinile pornite al caror moment a venit, din toate proiectele.
func (s *Store) DueRecurring(now time.Time) ([]RecurringTask, error) {
	return s.listRecurring(`SELECT `+recurringCols+` FROM recurring_tasks WHERE enabled = 1 AND next_run_at <= ? ORDER BY next_run_at, id`, runAt(now))
}

func (s *Store) listRecurring(q string, args ...any) ([]RecurringTask, error) {
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []RecurringTask{}
	for rows.Next() {
		r, err := scanRecurring(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
```

Verifica ca `mustAffect` intoarce `ErrNotFound` cand nu s-a atins niciun rand (asa il folosesc celelalte functii); `nullPtr` e in `runs.go`.

- [ ] **Step 6:** `store_test.go`: user_version 13 -> 14 (ambele locuri de pe liniile 63-64). `go test ./internal/store/` -> PASS.
- [ ] **Step 7:** commit: `store: sarcini recurente (migrarea 014)`

---

### Task 3: Team: CRUD si scheduler

**Files:**
- Create: `internal/team/recurring.go`
- Modify: `internal/team/stages.go:99-155` (`UserCreateTask` -> lock + corp separat), `internal/team/reconcile.go:21-33` (`Run`)
- Test: `internal/team/recurring_test.go`

**Interfaces:**
- Consumes: `schedule.Spec` (Task 1); `store.RecurringTask`, `Task.RecurringID/AutoAccept`, CRUD-ul din Task 2.
- Produces:
  - `type RecurringError string` (`Error() string`): erori de validare / stare pentru utilizator (web -> 400).
  - `type RecurringInput struct { Title, Description string; AssigneeID int64; Schedule schedule.Spec; AutoAccept, Enabled bool }` (`AssigneeID` 0 = prima etapa).
  - `func (t *Team) CreateRecurring(projectID int64, in RecurringInput) (store.RecurringTask, error)`
  - `func (t *Team) UpdateRecurring(id int64, in RecurringInput) (store.RecurringTask, error)`
  - `func (t *Team) DeleteRecurring(id int64) error`
  - `func (t *Team) RunRecurringNow(id int64) (store.Task, error)`
  - `func (t *Team) RunDue(now time.Time) error`
  - eveniment SSE `"recurring"` cu sarcina, pe proiectul ei.

- [ ] **Step 1: Refactor `UserCreateTask`** (fara schimbare de comportament): corpul de azi devine

```go
// userCreateTask e corpul lui UserCreateTask; se cheama cu t.work tinut.
// recurring (optional) e sarcina recurenta care creeaza task-ul.
func (t *Team) userCreateTask(projectID int64, title, description string, agentID int64, recurring *store.RecurringTask) (store.Task, error) {
```

`UserCreateTask` ramane cu aceeasi semnatura: `t.work.Lock(); defer t.work.Unlock(); return t.userCreateTask(projectID, title, description, agentID, nil)`. In corp, la construirea task-ului:

```go
	task := store.Task{ProjectID: projectID, Title: title, Description: description, ByUser: true}
	created := "Created by you"
	if recurring != nil {
		task.RecurringID, task.AutoAccept = &recurring.ID, recurring.AutoAccept
		created = fmt.Sprintf("Created by recurring task %q", recurring.Title)
	}
```

si `t.taskMsg(task, "created", actorUser, to, created, briefBody(...))`. Ruleaza `go test ./internal/team/` -> PASS (refactor neutru).

- [ ] **Step 2: Testele** (`recurring_test.go`). Fixture-ul `setup(t)` din `fake_test.go` (lead + Rook + Vera, fara etape). Ceasul: `f.tm.now = func() time.Time { return clock }` cu `clock` variabila locala (campul `now` e in acelasi pachet).

```go
package team

import (
	"errors"
	"strings"
	"testing"
	"time"

	"foreman/internal/schedule"
	"foreman/internal/store"
)

var daily9 = schedule.Spec{Kind: schedule.KindDaily, At: "09:00"}

func at(s string) time.Time {
	v, err := time.ParseInLocation("2006-01-02 15:04", s, time.Local)
	if err != nil {
		panic(err)
	}
	return v
}

func (f *fixture) recurring(t *testing.T, in RecurringInput, now time.Time) store.RecurringTask {
	t.Helper()
	f.tm.now = func() time.Time { return now }
	r, err := f.tm.CreateRecurring(f.p.ID, in)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestCreateRecurringValidates(t *testing.T) {
	f := setup(t)
	var re RecurringError
	if _, err := f.tm.CreateRecurring(f.p.ID, RecurringInput{Title: "x", Schedule: daily9, Enabled: true}); !errors.As(err, &re) {
		t.Fatalf("fara etape agentul e obligatoriu: %v", err)
	}
	if _, err := f.tm.CreateRecurring(f.p.ID, RecurringInput{Title: "x", AssigneeID: f.rook.ID, Schedule: schedule.Spec{Kind: "monthly"}}); !errors.As(err, &re) {
		t.Fatalf("program invalid: %v", err)
	}
	if _, err := f.tm.CreateRecurring(f.p.ID, RecurringInput{Title: "x", AssigneeID: 99999, Schedule: daily9}); err == nil {
		t.Fatal("agent din alt proiect / inexistent")
	}
	r := f.recurring(t, RecurringInput{Title: "Tests", AssigneeID: f.rook.ID, Schedule: daily9, Enabled: true}, at("2026-10-05 10:00"))
	if !r.NextRunAt.Equal(at("2026-10-06 09:00")) {
		t.Fatalf("next = %v", r.NextRunAt)
	}
}

func TestRunDueCreatesTask(t *testing.T) {
	f := setup(t)
	r := f.recurring(t, RecurringInput{Title: "Tests", Description: "run", AssigneeID: f.rook.ID, Schedule: daily9, Enabled: true, AutoAccept: true}, at("2026-10-05 08:00"))
	if err := f.tm.RunDue(at("2026-10-05 08:59")); err != nil {
		t.Fatal(err)
	}
	if tasks, _ := f.st.ListTasks(f.p.ID); len(tasks) != 0 {
		t.Fatalf("prea devreme: %+v", tasks)
	}
	if err := f.tm.RunDue(at("2026-10-05 09:00")); err != nil {
		t.Fatal(err)
	}
	task := f.wantStatus(t, 1, store.StatusQueued)
	if task.RecurringID == nil || *task.RecurringID != r.ID || !task.AutoAccept || !task.ByUser || task.AssigneeID != f.rook.ID {
		t.Fatalf("%+v", task)
	}
	got, _ := f.st.GetRecurring(r.ID)
	if got.LastTaskID == nil || *got.LastTaskID != task.ID || got.LastNote != "" || !got.NextRunAt.Equal(at("2026-10-06 09:00")) {
		t.Fatalf("%+v", got)
	}
	evs, _ := f.st.ListTaskEvents(task.ID)
	if len(evs) == 0 || evs[0].Text != `Created by recurring task "Tests"` {
		t.Fatalf("%+v", evs)
	}
}

func TestRunDueMissedRunsCreateOne(t *testing.T) {
	f := setup(t)
	r := f.recurring(t, RecurringInput{Title: "Hourly", AssigneeID: f.rook.ID, Schedule: schedule.Spec{Kind: schedule.KindEveryHours, EveryHours: 1}, Enabled: true}, at("2026-10-05 08:30"))
	// Foreman a fost inchis doua zile
	now := at("2026-10-07 10:15")
	if err := f.tm.RunDue(now); err != nil {
		t.Fatal(err)
	}
	if err := f.tm.RunDue(now); err != nil { // al doilea tur in acelasi minut: nimic nou
		t.Fatal(err)
	}
	if tasks, _ := f.st.ListTasks(f.p.ID); len(tasks) != 1 {
		t.Fatalf("vreau un singur task, am %d", len(tasks))
	}
	if got, _ := f.st.GetRecurring(r.ID); !got.NextRunAt.Equal(at("2026-10-07 11:00")) {
		t.Fatalf("next = %v", got.NextRunAt)
	}
}

func TestRunDueSkipsWhileOpen(t *testing.T) {
	f := setup(t)
	r := f.recurring(t, RecurringInput{Title: "Hourly", AssigneeID: f.rook.ID, Schedule: schedule.Spec{Kind: schedule.KindEveryHours, EveryHours: 1}, Enabled: true}, at("2026-10-05 08:30"))
	f.tm.RunDue(at("2026-10-05 10:00"))
	f.tm.RunDue(at("2026-10-05 11:00"))
	if tasks, _ := f.st.ListTasks(f.p.ID); len(tasks) != 1 {
		t.Fatalf("task-ul precedent e deschis: vreau 1, am %d", len(tasks))
	}
	got, _ := f.st.GetRecurring(r.ID)
	if got.LastNote != "Skipped at 11:00: T-1 still open" || !got.NextRunAt.Equal(at("2026-10-05 12:00")) {
		t.Fatalf("%+v", got)
	}
	task := f.wantStatus(t, 1, store.StatusQueued)
	task.Status = store.StatusDone
	f.st.SaveTask(&task)
	f.tm.RunDue(at("2026-10-05 12:00"))
	if tasks, _ := f.st.ListTasks(f.p.ID); len(tasks) != 2 {
		t.Fatalf("dupa Done urmeaza alt task: %d", len(tasks))
	}
}

func TestRunDueAssigneeDeleted(t *testing.T) {
	f := setup(t)
	wes := f.agent(t, "Wes", &f.lead, nil)
	r := f.recurring(t, RecurringInput{Title: "x", AssigneeID: wes.ID, Schedule: daily9, Enabled: true}, at("2026-10-05 08:00"))
	if err := f.st.DeleteAgent(wes.ID); err != nil {
		t.Fatal(err)
	}
	if err := f.tm.RunDue(at("2026-10-05 09:00")); err != nil {
		t.Fatal(err)
	}
	got, _ := f.st.GetRecurring(r.ID)
	if got.Enabled || got.LastNote != "Turned off: the assignee was deleted" {
		t.Fatalf("%+v", got)
	}
}

func TestRunDueErrorDoesNotStopOthers(t *testing.T) {
	f := setup(t)
	// cu etape, un agent care nu e in prima etapa da AssignError la creare
	stages, err := f.st.SaveStages(f.p.ID, []store.Stage{{Name: "Dev", Kind: store.KindWork, AgentIDs: []int64{f.rook.ID}}})
	if err != nil {
		t.Fatal(err)
	}
	bad := f.recurring(t, RecurringInput{Title: "bad", AssigneeID: f.rook.ID, Schedule: daily9, Enabled: true}, at("2026-10-05 08:00"))
	stages[0].AgentIDs = []int64{f.vera.ID} // Rook scos din etapa dupa creare (ID-ul etapei pastrat)
	if _, err := f.st.SaveStages(f.p.ID, stages); err != nil {
		t.Fatal(err)
	}
	good := f.recurring(t, RecurringInput{Title: "good", Schedule: daily9, Enabled: true}, at("2026-10-05 08:00"))
	if err := f.tm.RunDue(at("2026-10-05 09:00")); err != nil {
		t.Fatal(err)
	}
	b, _ := f.st.GetRecurring(bad.ID)
	if b.LastNote == "" || !b.Enabled || !b.NextRunAt.Equal(at("2026-10-06 09:00")) {
		t.Fatalf("eroarea in nota, programul mutat: %+v", b)
	}
	if g, _ := f.st.GetRecurring(good.ID); g.LastTaskID == nil {
		t.Fatalf("cealalta sarcina trebuia sa ruleze: %+v", g)
	}
}

func TestRunDueRereadsUnderLock(t *testing.T) {
	f := setup(t)
	r := f.recurring(t, RecurringInput{Title: "x", AssigneeID: f.rook.ID, Schedule: daily9, Enabled: true}, at("2026-10-05 08:00"))
	due, _ := f.st.DueRecurring(at("2026-10-05 09:00"))
	// utilizatorul o opreste intre citire si rulare
	off := RecurringInput{Title: "x", AssigneeID: f.rook.ID, Schedule: daily9, Enabled: false}
	if _, err := f.tm.UpdateRecurring(r.ID, off); err != nil {
		t.Fatal(err)
	}
	if err := f.tm.runRecurring(due[0].ID, at("2026-10-05 09:00"), true); err != nil {
		t.Fatal(err)
	}
	if tasks, _ := f.st.ListTasks(f.p.ID); len(tasks) != 0 {
		t.Fatalf("sarcina oprita nu creeaza task: %+v", tasks)
	}
}

func TestRunRecurringNow(t *testing.T) {
	f := setup(t)
	r := f.recurring(t, RecurringInput{Title: "x", AssigneeID: f.rook.ID, Schedule: daily9, Enabled: false}, at("2026-10-05 08:00"))
	task, err := f.tm.RunRecurringNow(r.ID)
	if err != nil || task.Number != 1 {
		t.Fatalf("%+v %v", task, err)
	}
	var re RecurringError
	if _, err := f.tm.RunRecurringNow(r.ID); !errors.As(err, &re) || !strings.Contains(err.Error(), "T-1 is still open") {
		t.Fatalf("suprapunere: %v", err)
	}
	if got, _ := f.st.GetRecurring(r.ID); !got.NextRunAt.Equal(r.NextRunAt) {
		t.Fatalf("Run now nu muta programul: %v -> %v", r.NextRunAt, got.NextRunAt)
	}
}

func TestUpdateRecurringRecomputesNext(t *testing.T) {
	f := setup(t)
	r := f.recurring(t, RecurringInput{Title: "x", AssigneeID: f.rook.ID, Schedule: daily9, Enabled: true}, at("2026-10-05 10:00"))
	f.tm.now = func() time.Time { return at("2026-10-05 11:00") }
	in := RecurringInput{Title: "y", AssigneeID: f.rook.ID, Schedule: schedule.Spec{Kind: schedule.KindDaily, At: "12:00"}, Enabled: true}
	got, err := f.tm.UpdateRecurring(r.ID, in)
	if err != nil || got.Title != "y" || !got.NextRunAt.Equal(at("2026-10-05 12:00")) {
		t.Fatalf("%+v %v", got, err)
	}
	in.Title = "z" // doar titlul: programul ramane
	got, _ = f.tm.UpdateRecurring(r.ID, in)
	if !got.NextRunAt.Equal(at("2026-10-05 12:00")) {
		t.Fatalf("%v", got.NextRunAt)
	}
	if err := f.tm.DeleteRecurring(r.ID); err != nil {
		t.Fatal(err)
	}
}
```

`TestRunDueErrorDoesNotStopOthers` se bazeaza pe `checkAssignee` (`internal/team/stages.go:84`), care refuza un agent ce nu e in etapa; conditia ceruta e doar ca `userCreateTask` sa intoarca eroare pentru prima sarcina.

- [ ] **Step 3:** `go test ./internal/team/ -run Recurring\|RunDue` -> FAIL.
- [ ] **Step 4: `recurring.go`**:

```go
package team

import (
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"foreman/internal/schedule"
	"foreman/internal/store"
)

// RecurringError e o eroare pe care o vede utilizatorul (web: 400).
type RecurringError string

func (e RecurringError) Error() string { return string(e) }

// RecurringInput e ce se poate seta din UI; AssigneeID 0 = primul agent liber din prima etapa.
type RecurringInput struct {
	Title, Description string
	AssigneeID         int64
	Schedule           schedule.Spec
	AutoAccept         bool
	Enabled            bool
}

// checkRecurring valideaza programul si asignatul (ca la un task nou din board).
// Intoarce asignatul ca pointer (nil = prima etapa). Se cheama cu t.work tinut.
func (t *Team) checkRecurring(projectID int64, in RecurringInput) (*int64, error) {
	if err := in.Schedule.Validate(); err != nil {
		return nil, RecurringError(err.Error())
	}
	stages, err := t.st.ListStages(projectID)
	if err != nil {
		return nil, err
	}
	if in.AssigneeID == 0 {
		if len(stages) == 0 {
			return nil, RecurringError("choose the agent who does the task")
		}
		return nil, nil
	}
	agents, err := t.st.ListAgents(projectID)
	if err != nil {
		return nil, err
	}
	var first *store.Stage
	if len(stages) > 0 {
		first = &stages[0]
	}
	if _, err := checkAssignee(agents, in.AssigneeID, first); err != nil {
		return nil, RecurringError(err.Error())
	}
	id := in.AssigneeID
	return &id, nil
}

func (t *Team) CreateRecurring(projectID int64, in RecurringInput) (store.RecurringTask, error) {
	t.work.Lock()
	defer t.work.Unlock()
	who, err := t.checkRecurring(projectID, in)
	if err != nil {
		return store.RecurringTask{}, err
	}
	r := store.RecurringTask{ProjectID: projectID, Title: in.Title, Description: in.Description, AssigneeID: who,
		Schedule: in.Schedule, AutoAccept: in.AutoAccept, Enabled: in.Enabled, NextRunAt: in.Schedule.Next(t.now())}
	if err := t.st.CreateRecurring(&r); err != nil {
		return store.RecurringTask{}, err
	}
	t.emit(projectID, "recurring", r)
	return r, nil
}

func (t *Team) UpdateRecurring(id int64, in RecurringInput) (store.RecurringTask, error) {
	t.work.Lock()
	defer t.work.Unlock()
	r, err := t.st.GetRecurring(id)
	if err != nil {
		return store.RecurringTask{}, err
	}
	who, err := t.checkRecurring(r.ProjectID, in)
	if err != nil {
		return store.RecurringTask{}, err
	}
	// programul se recalculeaza doar cand s-a schimbat sau sarcina e repornita
	if in.Schedule != r.Schedule || (in.Enabled && !r.Enabled) {
		r.NextRunAt = in.Schedule.Next(t.now())
	}
	r.Title, r.Description, r.AssigneeID, r.Schedule, r.AutoAccept, r.Enabled = in.Title, in.Description, who, in.Schedule, in.AutoAccept, in.Enabled
	if err := t.st.SaveRecurring(&r); err != nil {
		return store.RecurringTask{}, err
	}
	t.emit(r.ProjectID, "recurring", r)
	return r, nil
}

func (t *Team) DeleteRecurring(id int64) error {
	t.work.Lock()
	defer t.work.Unlock()
	r, err := t.st.GetRecurring(id)
	if err != nil {
		return err
	}
	if err := t.st.DeleteRecurring(id); err != nil {
		return err
	}
	t.emit(r.ProjectID, "recurring", r)
	return nil
}

// RunDue creeaza task-urile sarcinilor al caror moment a venit. O eroare la o
// sarcina se logheaza si nu le opreste pe celelalte.
func (t *Team) RunDue(now time.Time) error {
	due, err := t.st.DueRecurring(now)
	if err != nil {
		return err
	}
	for _, r := range due {
		if err := t.runRecurring(r.ID, now, true); err != nil {
			log.Printf("recurring %d: %v", r.ID, err)
		}
	}
	return nil
}

// RunRecurringNow creeaza imediat un task (butonul Run now), fara sa mute programul.
func (t *Team) RunRecurringNow(id int64) (store.Task, error) {
	t.work.Lock()
	defer t.work.Unlock()
	r, err := t.st.GetRecurring(id)
	if err != nil {
		return store.Task{}, err
	}
	if open, ok := t.openOccurrence(r); ok {
		return store.Task{}, RecurringError(open.Ref() + " is still open")
	}
	task, err := t.userCreateTask(r.ProjectID, r.Title, r.Description, derefID(r.AssigneeID), &r)
	if err != nil {
		return store.Task{}, err
	}
	r.LastTaskID, r.LastNote = &task.ID, ""
	if err := t.st.SaveRecurring(&r); err != nil {
		return store.Task{}, err
	}
	t.emit(r.ProjectID, "recurring", r)
	return task, nil
}

// runRecurring face o rulare programata. Reciteste sarcina sub t.work: o editare
// facuta intre DueRecurring si aici castiga. Erorile de creare ajung in nota.
func (t *Team) runRecurring(id int64, now time.Time, scheduled bool) error {
	t.work.Lock()
	defer t.work.Unlock()
	r, err := t.st.GetRecurring(id)
	if errors.Is(err, store.ErrNotFound) {
		return nil // stearsa intre timp
	}
	if err != nil {
		return err
	}
	if scheduled && (!r.Enabled || r.NextRunAt.After(now)) {
		return nil // oprita sau reprogramata intre timp
	}
	hhmm := now.In(time.Local).Format("15:04")
	switch open, busy := t.openOccurrence(r); {
	case busy:
		r.LastNote = fmt.Sprintf("Skipped at %s: %s still open", hhmm, open.Ref())
	default:
		stages, err := t.st.ListStages(r.ProjectID)
		if err != nil {
			return err
		}
		if r.AssigneeID == nil && len(stages) == 0 {
			r.Enabled, r.LastNote = false, "Turned off: the assignee was deleted"
			break
		}
		task, err := t.userCreateTask(r.ProjectID, r.Title, r.Description, derefID(r.AssigneeID), &r)
		if err != nil {
			r.LastNote = "Could not create the task: " + strings.TrimSuffix(err.Error(), ".")
		} else {
			r.LastTaskID, r.LastNote = &task.ID, ""
		}
	}
	r.LastRunAt = &now
	r.NextRunAt = r.Schedule.Next(now)
	if err := t.st.SaveRecurring(&r); err != nil {
		return err
	}
	t.emit(r.ProjectID, "recurring", r)
	return nil
}

// openOccurrence: ultimul task creat de sarcina, daca exista si nu e Done.
func (t *Team) openOccurrence(r store.RecurringTask) (store.Task, bool) {
	if r.LastTaskID == nil {
		return store.Task{}, false
	}
	task, err := t.st.GetTask(*r.LastTaskID)
	if err != nil {
		return store.Task{}, false // sters: nu blocheaza
	}
	return task, task.Status != store.StatusDone
}

func derefID(p *int64) int64 {
	if p == nil {
		return 0
	}
	return *p
}
```

Nota: `now` vine de la ticker in ora locala (`t.now()` = `time.Now`); `Schedule.Next(now)` lucreaza in fusul lui `now`. Daca `derefID` sau un helper echivalent exista deja in pachet, foloseste-l pe acela.

- [ ] **Step 5: `Run`** (`reconcile.go`): inainte de `for`, `if err := t.RunDue(t.now()); err != nil { log.Printf("recurring: %v", err) }` (comentariu: rularile ratate cat a fost inchis Foreman apar imediat). In `case <-tick.C:` dupa `CheckStuck`, acelasi apel. Actualizeaza comentariul functiei: "O data pe minut cauta si task-urile blocate (CheckStuck) si ruleaza sarcinile recurente (RunDue)".
- [ ] **Step 6:** `go test ./internal/team/` si `go test -race ./internal/team/` -> PASS.
- [ ] **Step 7:** commit: `team: sarcini recurente si scheduler`

---

### Task 4: Team: auto-accept

**Files:**
- Modify: `internal/team/tools.go` (constante langa `autoAcceptText` ~316; `toReview` ~372-440; `ReviewApprove` ~848-880), `internal/team/stages.go:~232` (dupa ultima etapa)
- Test: `internal/team/recurring_test.go` (adauga)

**Interfaces:**
- Consumes: `Task.AutoAccept` (Task 2); `userCreateTask(..., recurring)` (Task 3).
- Produces: `recurringAcceptText = "Accepted automatically (recurring task)"`; `acceptText(task store.Task) string`.

- [ ] **Step 1: Testele** (in `recurring_test.go`):

```go
// recurringTask creeaza prin scheduler un task pentru agent, cu auto-accept dat
func (f *fixture) recurringTask(t *testing.T, who store.Agent, auto bool) store.Task {
	t.Helper()
	r := f.recurring(t, RecurringInput{Title: "Daily", AssigneeID: who.ID, Schedule: daily9, Enabled: true, AutoAccept: auto}, at("2026-10-05 08:00"))
	if _, err := f.tm.RunRecurringNow(r.ID); err != nil {
		t.Fatal(err)
	}
	tasks, _ := f.st.ListTasks(f.p.ID)
	task := tasks[len(tasks)-1]
	task.Status = store.StatusInProgress
	f.st.SaveTask(&task)
	return task
}

func lastEventText(t *testing.T, f *fixture, taskID int64) string {
	t.Helper()
	evs, _ := f.st.ListTaskEvents(taskID)
	return evs[len(evs)-1].Text
}

func TestAutoAcceptWithoutReviewer(t *testing.T) {
	f := setup(t)
	task := f.recurringTask(t, f.rook, true)
	if _, err := f.tm.ReportDone(f.workerCaller(f.rook, task.ID), "", "all green"); err != nil {
		t.Fatal(err)
	}
	f.wantStatus(t, task.Number, store.StatusDone)
	if got := lastEventText(t, f, task.ID); got != "Accepted automatically (recurring task)" {
		t.Fatalf("%q", got)
	}
}

func TestAutoAcceptAfterReviewer(t *testing.T) {
	f := setup(t)
	wes := f.agent(t, "Wes", &f.lead, &f.vera)
	task := f.recurringTask(t, wes, true)
	f.tm.ReportDone(f.workerCaller(wes, task.ID), "", "gata")
	f.wantStatus(t, task.Number, store.StatusReview) // intai reviewer-ul
	vc := f.workerCaller(f.vera, 0)
	vc.IsReviewer = true
	if _, err := f.tm.ReviewApprove(vc, task.Ref(), ""); err != nil {
		t.Fatal(err)
	}
	f.wantStatus(t, task.Number, store.StatusDone)
}

func TestAutoAcceptAfterLastStage(t *testing.T) {
	f := setup(t)
	if _, err := f.st.SaveStages(f.p.ID, []store.Stage{{Name: "Dev", Kind: store.KindWork, AgentIDs: []int64{f.rook.ID}}}); err != nil {
		t.Fatal(err)
	}
	task := f.recurringTask(t, f.rook, true)
	if _, err := f.tm.ReportDone(f.workerCaller(f.rook, task.ID), "", "gata"); err != nil {
		t.Fatal(err)
	}
	f.wantStatus(t, task.Number, store.StatusDone)
}

func TestNoAutoAcceptWaitsForUser(t *testing.T) {
	f := setup(t)
	task := f.recurringTask(t, f.rook, false)
	f.tm.ReportDone(f.workerCaller(f.rook, task.ID), "", "gata")
	got := f.wantStatus(t, task.Number, store.StatusReview)
	if !f.tm.UserDecides(got, mustAgents(t, f), false) {
		t.Fatalf("fara auto-accept decide utilizatorul: %+v", got)
	}
}
```

Verifica in `stages_test.go` cum se termina un task din ultima etapa (prin `ReportDone` sau alt tool, ex. `stage_done`) si foloseste acelasi drum in `TestAutoAcceptAfterLastStage`.

- [ ] **Step 2:** `go test ./internal/team/ -run AutoAccept` -> FAIL (task-urile raman in Review).
- [ ] **Step 3: Implementarea.** Langa `autoAcceptText` (tools.go):

```go
	recurringAcceptText = "Accepted automatically (recurring task)"
```

si

```go
// acceptText: textul din istoric pentru un task trecut singur in Done.
func acceptText(task store.Task) string {
	if task.AutoAccept {
		return recurringAcceptText
	}
	return autoAcceptText
}
```

Apoi, in cele patru locuri (fiecare e un caz "nu exista decident"; in modul autonom ramane comportamentul de azi):

1. `toReview`, ramura cu etape: `if !ok && autonomous {` -> `if !ok && (autonomous || task.AutoAccept) {`; `t.autoAccepted(*task, autoAcceptText)` -> `acceptText(*task)`.
2. `toReview`, ramura fara etape: in `switch`, inainte de `case !hasWho && !task.ByUser:` adauga

```go
	case !hasWho && task.AutoAccept:
		// sarcina recurenta cu auto-accept: nu mai asteapta utilizatorul
		markDone(task)
```

   iar mai jos `t.autoAccepted(*task, autoAcceptText)` -> `acceptText(*task)`.
3. `ReviewApprove`: inainte de `if task.ByUser && !autonomous {` adauga

```go
	if task.ByUser && task.AutoAccept && !autonomous {
		// sarcina recurenta cu auto-accept: dupa reviewer trece direct in Done
		markDone(&task)
		if err := t.st.SaveTask(&task); err != nil {
			return "", err
		}
		t.record(task, approved)
		t.note(c.ProjectID, reviewer.ID, "Approved "+task.Ref(), true)
		t.autoAccepted(task, recurringAcceptText)
		return fmt.Sprintf("%s was accepted automatically (recurring task).", task.Ref()), nil
	}
```

4. `stages.go` dupa ultima etapa: `if !ok && autonomous {` -> `if !ok && (autonomous || task.AutoAccept) {`; textul -> `acceptText(*task)`; mesajul intors: daca `task.AutoAccept` "... and was accepted automatically (recurring task).", altfel cel de azi.

- [ ] **Step 4:** `go test ./internal/team/` si `go test -race ./internal/team/` -> PASS (inclusiv testele existente de autonomie si etape).
- [ ] **Step 5:** commit: `team: auto-accept pentru sarcinile recurente`

---

### Task 5: Web API

**Files:**
- Create: `internal/web/api_recurring.go`
- Modify: `internal/web/server.go` (rute langa `POST /api/projects/{id}/tasks`; `fail`: `team.RecurringError` -> 400), `internal/web/api_projects.go` (`projectDetail.Recurring`), `internal/web/api_tasks.go` (`getTask`: cheia `recurring`)
- Test: `internal/web/recurring_test.go`

**Interfaces:**
- Consumes: `team.RecurringInput`, `team.RecurringError`, metodele team din Task 3; `store.ListRecurring`, `GetRecurring`.
- Produces (JSON):
  - `recurringView` = `store.RecurringTask` (embedded) + `scheduleText string`, `assignee string`, `lastTask *{id, number, status}`.
  - `POST /api/projects/{id}/recurring` -> 201 view; `PUT /api/recurring/{id}` -> 200 view; `DELETE /api/recurring/{id}` -> 204; `POST /api/recurring/{id}/run` -> 201 task.
  - `GET /api/projects/{id}` primeste `recurring: [view]` (mereu array).
  - `GET /api/tasks/{id}` primeste `recurring: {id, title}` sau `null`.

- [ ] **Step 1: Testele** (`recurring_test.go`), cu helper-ele din `server_test.go` (`setup`, `e.do`, `e.createProject`, `e.root`, `itoa`):

```go
package web

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestRecurringAPI(t *testing.T) {
	e := setup(t)
	p := e.createProject(t, t.TempDir())
	root := e.root(t, p.ID)
	base := "/api/projects/" + itoa(p.ID) + "/recurring"
	daily := map[string]any{"kind": "daily", "at": "09:00"}
	body := func(over map[string]any) map[string]any {
		b := map[string]any{"title": "Tests", "description": "run them", "assigneeId": root.ID, "schedule": daily, "autoAccept": true, "enabled": true}
		for k, v := range over {
			b[k] = v
		}
		return b
	}
	for _, bad := range []map[string]any{
		body(map[string]any{"title": "  "}),
		body(map[string]any{"title": strings.Repeat("x", 201)}),
		body(map[string]any{"schedule": map[string]any{"kind": "daily", "at": "25:00"}}),
		body(map[string]any{"assigneeId": 0}), // fara etape: agent obligatoriu
	} {
		if code, raw := e.do(t, "POST", base, bad); code != http.StatusBadRequest {
			t.Errorf("%v: %d %s", bad, code, raw)
		}
	}
	code, raw := e.do(t, "POST", base, body(nil))
	var v struct {
		ID           int64  `json:"id"`
		ScheduleText string `json:"scheduleText"`
		Assignee     string `json:"assignee"`
		Enabled      bool   `json:"enabled"`
	}
	json.Unmarshal(raw, &v)
	if code != http.StatusCreated || v.ScheduleText != "Daily at 09:00" || v.Assignee != root.Name {
		t.Fatalf("%d %s", code, raw)
	}
	// lista vine in detaliul proiectului
	_, raw = e.do(t, "GET", "/api/projects/"+itoa(p.ID), nil)
	if !strings.Contains(string(raw), `"recurring":[{`) {
		t.Fatalf("detaliu: %s", raw)
	}
	// run now -> task cu recurringId; a doua oara -> 400
	code, raw = e.do(t, "POST", "/api/recurring/"+itoa(v.ID)+"/run", nil)
	var task struct {
		ID          int64  `json:"id"`
		RecurringID *int64 `json:"recurringId"`
	}
	json.Unmarshal(raw, &task)
	if code != http.StatusCreated || task.RecurringID == nil || *task.RecurringID != v.ID {
		t.Fatalf("run: %d %s", code, raw)
	}
	if code, raw := e.do(t, "POST", "/api/recurring/"+itoa(v.ID)+"/run", nil); code != http.StatusBadRequest || !strings.Contains(string(raw), "still open") {
		t.Fatalf("run dublu: %d %s", code, raw)
	}
	_, raw = e.do(t, "GET", "/api/tasks/"+itoa(task.ID), nil)
	if !strings.Contains(string(raw), `"recurring":{"id":`+itoa(v.ID)+`,"title":"Tests"}`) {
		t.Fatalf("getTask: %s", raw)
	}
	if code, raw := e.do(t, "PUT", "/api/recurring/"+itoa(v.ID), body(map[string]any{"enabled": false})); code != 200 || !strings.Contains(string(raw), `"enabled":false`) {
		t.Fatalf("put: %d %s", code, raw)
	}
	if code, _ := e.do(t, "DELETE", "/api/recurring/"+itoa(v.ID), nil); code != http.StatusNoContent {
		t.Fatalf("delete: %d", code)
	}
	if code, _ := e.do(t, "PUT", "/api/recurring/"+itoa(v.ID), body(nil)); code != http.StatusNotFound {
		t.Fatalf("put dupa delete: %d", code)
	}
	_, raw = e.do(t, "GET", "/api/tasks/"+itoa(task.ID), nil)
	if !strings.Contains(string(raw), `"recurring":null`) {
		t.Fatalf("getTask dupa delete: %s", raw)
	}
}
```

`itoa(int64)` e in `server_test.go:147`; `e.do` pune mereu `Content-Type: application/json`, deci POST cu body `nil` trece de guard.

- [ ] **Step 2:** `go test ./internal/web/ -run Recurring` -> FAIL.
- [ ] **Step 3: `api_recurring.go`**:

```go
package web

import (
	"net/http"
	"strings"

	"foreman/internal/schedule"
	"foreman/internal/store"
	"foreman/internal/team"
)

// recurringView: sarcina recurenta cu ce mai trebuie aratat in UI.
type recurringView struct {
	store.RecurringTask
	ScheduleText string        `json:"scheduleText"`
	Assignee     string        `json:"assignee"` // "" = prima etapa
	LastTask     *lastTaskView `json:"lastTask"`
}

type lastTaskView struct {
	ID     int64  `json:"id"`
	Number int    `json:"number"`
	Status string `json:"status"`
}

func (s *Server) recurringView(r store.RecurringTask) recurringView {
	v := recurringView{RecurringTask: r, ScheduleText: r.Schedule.String()}
	if r.AssigneeID != nil {
		if a, err := s.st.GetAgent(*r.AssigneeID); err == nil {
			v.Assignee = a.Name
		}
	}
	if r.LastTaskID != nil {
		if t, err := s.st.GetTask(*r.LastTaskID); err == nil {
			v.LastTask = &lastTaskView{ID: t.ID, Number: t.Number, Status: t.Status}
		}
	}
	return v
}

// recurringViews: lista proiectului pentru detaliu; mereu non-nil.
func (s *Server) recurringViews(projectID int64) ([]recurringView, error) {
	list, err := s.st.ListRecurring(projectID)
	if err != nil {
		return nil, err
	}
	out := make([]recurringView, len(list))
	for i, r := range list {
		out[i] = s.recurringView(r)
	}
	return out, nil
}

// recurringInput citeste si valideaza corpul comun POST/PUT.
func recurringInput(r *http.Request) (team.RecurringInput, error) {
	var in struct {
		Title       string        `json:"title"`
		Description string        `json:"description"`
		AssigneeID  int64         `json:"assigneeId"`
		Schedule    schedule.Spec `json:"schedule"`
		AutoAccept  bool          `json:"autoAccept"`
		Enabled     bool          `json:"enabled"`
	}
	if err := decode(r, &in); err != nil {
		return team.RecurringInput{}, err
	}
	title := strings.TrimSpace(in.Title)
	if title == "" {
		return team.RecurringInput{}, badRequest("title is required")
	}
	desc := strings.TrimSpace(in.Description)
	for _, err := range []error{maxLen("title", title, 200), maxLen("description", desc, 10000)} {
		if err != nil {
			return team.RecurringInput{}, err
		}
	}
	return team.RecurringInput{Title: title, Description: desc, AssigneeID: in.AssigneeID, Schedule: in.Schedule,
		AutoAccept: in.AutoAccept, Enabled: in.Enabled}, nil
}

func (s *Server) createRecurring(w http.ResponseWriter, r *http.Request) {
	pid, err := pathID(r)
	if err != nil {
		fail(w, err)
		return
	}
	if _, err := s.st.GetProject(pid); err != nil {
		fail(w, err)
		return
	}
	in, err := recurringInput(r)
	if err != nil {
		fail(w, err)
		return
	}
	rec, err := s.team.CreateRecurring(pid, in)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, s.recurringView(rec))
}

func (s *Server) updateRecurring(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		fail(w, err)
		return
	}
	in, err := recurringInput(r)
	if err != nil {
		fail(w, err)
		return
	}
	rec, err := s.team.UpdateRecurring(id, in)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, s.recurringView(rec))
}

func (s *Server) deleteRecurring(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		fail(w, err)
		return
	}
	if err := s.team.DeleteRecurring(id); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) runRecurring(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		fail(w, err)
		return
	}
	task, err := s.team.RunRecurringNow(id)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, task)
}
```

- [ ] **Step 4: Legaturile.**
  - `server.go`, dupa `POST /api/projects/{id}/tasks`:
    ```go
    mux.HandleFunc("POST /api/projects/{id}/recurring", s.createRecurring)
    mux.HandleFunc("PUT /api/recurring/{id}", s.updateRecurring)
    mux.HandleFunc("DELETE /api/recurring/{id}", s.deleteRecurring)
    mux.HandleFunc("POST /api/recurring/{id}/run", s.runRecurring)
    ```
  - `fail`: declara `var re team.RecurringError` si `var ae team.AssignError` si adauga `case errors.As(err, &re), errors.As(err, &ae): writeErr(w, http.StatusBadRequest, err.Error())` langa `badRequest`. `fail` nu trateaza azi `AssignError` (`createUserTask` il converteste local); la `run`, `userCreateTask` il poate intoarce, deci trebuie 400, nu 500.
  - `api_projects.go`: `projectDetail` primeste `Recurring []recurringView \`json:"recurring"\``; in `getProject`, dupa `stages`, `recurring, err := s.recurringViews(p.ID)` (cu `fail`) si `Recurring: recurring` in literalul `projectDetail`.
  - `api_tasks.go` `getTask`: inainte de `writeJSON`,
    ```go
    // sarcina recurenta care l-a creat, cat timp mai exista (pentru linkul din modala)
    var recurring any
    if task.RecurringID != nil {
        if rec, err := s.st.GetRecurring(*task.RecurringID); err == nil {
            recurring = map[string]any{"id": rec.ID, "title": rec.Title}
        }
    }
    ```
    si `"recurring": recurring` in map. Atentie: `map[string]any` se serializeaza cu cheile sortate, deci `{"id":..,"title":..}` ca in test.
- [ ] **Step 5:** `go test ./internal/web/` -> PASS; `go vet ./...`.
- [ ] **Step 6:** commit: `web: API pentru sarcinile recurente`

---

### Task 6: UI: panoul Recurring, `↻` pe card, link in modala

**Files:**
- Create: `internal/web/static/js/recurring.js`
- Modify: `internal/web/static/js/workspace.js` (`boardPanel`, `boardTools` ~497, `toggleRecurring`, `taskCard` ~628, `syncModal` ~654 + reload), `internal/web/static/js/task-modal.js` (meta in `render`), `internal/web/static/app.css`, `CLAUDE.md` (sectiunea Stare)

**Interfaces:**
- Consumes: `state.project.recurring` (`recurringView[]`), `task.recurringId`, raspunsul `getTask` cu `recurring`; rutele din Task 5; din `ui.js`: `el`, `field`, `toast`, `clock`; din `api.js`: `api`.
- Produces: `export function recurringPanel({ projectId, project, assignable, onClose, onChanged })` -> `{ el, update(project) }`. `taskModal` primeste in `actions` functia `openRecurring()`.

- [ ] **Step 1: `recurring.js`.** Structura (scrie-l complet, in stilul lui `stages.js`/`task-modal.js`):
  - `el("section", { class: "section recurring-panel" })` cu antet `h2` "Recurring tasks", butonul `New recurring task` si `Close`.
  - **Lista** (`renderList(project)`): pentru fiecare `r` din `project.recurring`, un `div.rec-row` cu:
    - titlu (`strong`), `r.scheduleText`, asignatul (`r.assignee || "First free agent in " + primaEtapa`), urmatoarea rulare: `r.enabled ? "Next: " + when(r.nextRunAt) : "Off"`, unde `when` da `"Today 14:00"`, `"Tomorrow 09:00"` sau `"Oct 6 09:00"` (`toLocaleDateString("en-US", { month: "short", day: "numeric" })` + `clock`);
    - `r.lastTask ? el("a", { href: \`#/p/${projectId}/tasks/${r.lastTask.id}\` }, \`T-${r.lastTask.number} · ${STATUS[r.lastTask.status]}\`) : null`;
    - `r.lastNote ? el("span", { class: r.lastNote.startsWith("Skipped") ? "hint" : "rec-error" }, r.lastNote) : null`;
    - `r.autoAccept ? el("span", { class: "hint" }, "auto-accept") : null`;
    - actiuni: comutator `input type=checkbox` cu `aria-label="Enabled"` (PUT cu tot corpul si `enabled` schimbat), `Run now` (POST `/run`, `toast("Created T-" + task.number, "ok")`), `Edit` (deschide formularul cu valorile), `Delete` (`confirm(\`Delete "${r.title}"? Tasks it already created stay.\`)`, apoi DELETE).
    - Dupa fiecare actiune reusita: `await onChanged()` (reincarca proiectul). Erorile: `toast(err.message)`.
    - Lista goala: `el("p", { class: "hint" }, "No recurring tasks yet. They create a task on the board on a schedule, for example every weekday at 09:00.")`.
  - **Formularul** (`openForm(r | null)`), in locul listei (un `editing` local; cat e deschis, `update(project)` nu redeseneaza nimic):
    - `field("Title", input maxlength=200 required)`, `field("Description", textarea rows=3)`;
    - `field("Assignee", select)`: optiunile ca in `toggleTaskForm` din `workspace.js` (prima etapa -> `value=""` "First free agent in <etapa>", apoi `assignable(first?.id)`); la editare, daca asignatul nu mai e in lista, optiunea lui (sau "?") ramane selectata, ca in `task-modal.js` `renderActions`;
    - **Schedule**: `fieldset` cu `legend` "Schedule" si trei `label`-uri radio (`name` unic per formular): "Every" + `input type=number min=1 max=24` + "hours"; "Daily at" + `input type=time`; "Weekly on" + 7 butoane `aria-pressed` (Mon..Sun, biti `1<<1 .. 1<<6, 1<<0`) + `input type=time`. Doar controalele variantei alese sunt active (`disabled` pe celelalte). Valori implicite: daily, `09:00`, L-V.
    - `label` cu `checkbox` "Accept automatically when done" + `hint` "Skips your Accept / Send back. Questions and blocked tasks still reach you." (bifa pe `enabled` nu apare in formular: un formular nou creeaza sarcina pornita; la editare se pastreaza `r.enabled`).
    - `Save` (POST nou / PUT editare) si `Cancel` (inapoi la lista). Corpul:
      ```js
      { title, description, assigneeId: Number(who.value) || 0, autoAccept: auto.checked, enabled: r ? r.enabled : true,
        schedule: { kind, everyHours: Number(hours.value) || 0, at: (kind === "every_hours" ? "" : time.value), weekdays: kind === "weekly" ? mask : 0 } }
      ```
      Fara zi aleasa la weekly: `toast("Choose at least one day.")` si nu trimite.
  - `update(project)`: tine proiectul nou; daca formularul nu e deschis, `renderList(project)`.
- [ ] **Step 2: `workspace.js`.**
  - `boardPanel` poate fi si `"recurring"` (actualizeaza comentariile de la `let boardPanel` si `showPanel`); `let recurring = null;` langa el.
  - `boardTools.replaceChildren(toggle("task", "+ Task", toggleTaskForm), toggle("recurring", "Recurring", toggleRecurring), toggle("stages", "Columns", toggleStages));`
  - ```js
    function toggleRecurring() {
      if (boardPanel === "recurring") return closePanel();
      recurring = recurringPanel({ projectId, project: state.project, assignable, onClose: closePanel, onChanged: reload });
      showPanel("recurring", recurring.el);
    }
    ```
    `showPanel` pune `recurring = null` cand `id !== "recurring"`. Dupa fiecare `reload()` reusit (acolo unde se cheama `renderBoard()` cu proiectul nou), `if (boardPanel === "recurring") recurring?.update(state.project);`.
  - `taskCard`: in `task-title`, dupa `el("span", { class: "task-id" }, ref(t))`, adauga `t.recurringId ? el("span", { class: "task-recurring", title: "From a recurring task", "aria-label": "From a recurring task" }, "↻") : null`.
  - `syncModal`: in `actions` adauga `openRecurring: () => { location.hash = \`#/p/${projectId}/tasks\`; if (boardPanel !== "recurring") toggleRecurring(); }`.
- [ ] **Step 3: `task-modal.js`.** In `render`, in `modal-meta`, dupa pill-ul de status: `data.recurring ? el("button", { type: "button", class: "link-btn", onclick: () => actions.openRecurring() }, \`From recurring: ${data.recurring.title}\`) : null`. Actualizeaza comentariul `actions` din antetul functiei (`openRecurring()`).
- [ ] **Step 4: `app.css`** (la final, cu comentariu `/* sarcini recurente */`): `.recurring-panel` (grid, gap 12px), `.rec-row` (border `var(--line)`, radius 9px, `var(--card-2)`, padding 10px 12px, flex wrap, gap 8px 12px, `min-width: 0`), `.rec-error { color: var(--red); }`, `.rec-days` (flex gap 4px; butoanele `aria-pressed="true"` cu `border-color: var(--amber)`), `.task-recurring { color: var(--muted); margin-right: 6px; font-size: 12px; }`, `.link-btn` (fara fundal/border, `color: var(--soft)`, underline la hover, `focus-visible` cu outline amber) daca nu exista deja o clasa echivalenta (cauta in `app.css`). Containerele cu `display` primesc regula `[hidden] { display: none; }`.
- [ ] **Step 5: Verificare JS:** `node --check` pe copiile `.mjs` ale lui `recurring.js`, `workspace.js`, `task-modal.js`.
- [ ] **Step 6: Verificare in browser** (memoria: screenshot-uri cu Chrome headless prin CDP, cu un HOME separat si copia bazei de date, port 7790, `--user-data-dir` nou ca sa nu prinda JS vechi din cache; opreste serverul de test dupa PID, nu dupa cale):
  1. panoul gol (hint), apoi formularul cu fiecare din cele 3 variante de program;
  2. lista cu o sarcina pornita, una oprita, una cu `Skipped…` si una cu eroare;
  3. `Run now` -> task nou pe board cu `↻`; modala lui arata "From recurring: …" si butonul deschide panoul;
  4. latime 390px: fara scroll orizontal pe pagina (`document.documentElement.scrollWidth <= innerWidth`).
- [ ] **Step 7: `CLAUDE.md`**, sectiunea Stare, dupa "coloane proprii": `Gata (sarcini recurente, spec in docs/superpowers/specs/2026-10-03-foreman-recurring-tasks-design.md): sarcini per proiect care creeaza task-uri pe un program (la N ore, zilnic, saptamanal; ora locala), pe ticker-ul din team.Run si o data la pornire (o singura rulare pentru cele ratate); sarite cat timp aparitia precedenta e deschisa; auto-accept optional. Pachet: internal/schedule.` Din "Dupa:" scoate "cron-uri".
- [ ] **Step 8: Verificarea completa:** `go vet ./...`, `go build -race ./cmd/foreman`, `go test ./...`, cele doua cross-compile.
- [ ] **Step 9:** commit: `ui: sarcini recurente (panou, ↻ pe card, link in modala)`
