# "Left for you": plan de implementare

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** la `report_done` agentul poate lasa o lista scurta de valori pe care doar utilizatorul le stie; utilizatorul
raspunde in comentariile task-ului, agentul completeaza si bifeaza, iar task-ul sta in "Needs you" cat timp lista are
puncte deschise.

**Architecture:** migrarea 020 adauga `task_todos`. Comentariile NU au tabel propriu (abatere de la spec, mai simplu):
un comentariu e un `task_event` de tip `comment` cu `body`, iar comentariul utilizatorului ajunge la agent ca mesaj
(`addUserMessage(..., reply=true)`), deci il trezeste exact ca raspunsul la o intrebare. Pachetul `mcp` primeste
argumentul `left_for_user` la `report_done` (validat inainte de raport) si doua tool-uri: `tick_left_for_user`,
`comment_task`. Web-ul expune lista pe task si pe proiect; UI-ul arata caseta din modala (mockup 15) si cardul
"TO FILL IN" din "Needs you" (mockup 14).

**Tech Stack:** Go 1.22+, SQLite (modernc.org/sqlite), JS vanilla.

**Spec:** `docs/superpowers/specs/2026-10-08-foreman-left-for-you-design.md`

## Global Constraints

- Tot ce vede utilizatorul sau agentul e in engleza; comentariile din cod in romana (fara diacritice).
- `go vet ./...`, `go build -race ./cmd/foreman`, cross-compile darwin/arm64 si windows trec. Fara dependente noi.
- Limite: max 10 puncte per task, fiecare 1..200 caractere; comentariu 1..4000 caractere.
- Testul `TestOpenAppliesMigrationsOnce` din `internal/store/store_test.go` numara migrarile: trece la 20.
- Schimbarea textului din prompt schimba hash-ul sesiunii: agentii pornesc o sesiune Claude noua la urmatorul run (normal, ca la orice schimbare de prompt).
- Comenzile se ruleaza din `agentdash/`. Nu atinge aplicatia de pe 7777 si nici `~/.foreman`.

## Review Focus

1. `left_for_user` invalid (11 puncte, un punct gol): `report_done` refuza tot, task-ul NU se misca (test in Task 2).
2. Agentul raporteaza de doua ori pe acelasi task (dupa un send back) cu alte puncte: lista veche ramane, punctele
   noi se adauga dupa ele, fara duplicate exacte (test in Task 1).
3. Utilizatorul comenteaza pe un task al carui asignat a fost sters: 409 cu mesaj, nu panica (test in Task 3).
4. Task Done cu puncte deschise: ramane Done si apare in "Needs you" pana se bifeaza tot (test in Task 3).
5. `tick_left_for_user` cu un numar inexistent (`[7]` dintr-o lista de 3): eroare clara, nimic bifat (test in Task 2).

---

### Task 1: store: `task_todos`

**Files:**
- Create: `internal/store/migrations/020_task_todos.sql`
- Create: `internal/store/todos.go`
- Create: `internal/store/todos_test.go`
- Modify: `internal/store/store_test.go` (20 migrari)

**Interfaces:**
- Produces:

```go
type Todo struct {
	TaskID   int64      `json:"taskId"`
	Position int        `json:"position"` // 1, 2, 3 (numarul vazut de agent si de utilizator)
	Text     string     `json:"text"`
	DoneAt   *time.Time `json:"doneAt"`
	DoneBy   string     `json:"doneBy"` // "You" sau numele agentului
}
type FillIn struct { // un task cu puncte deschise, pentru "Needs you"
	TaskID int64 `json:"taskId"`; Number int `json:"number"`; Title string `json:"title"`
	AgentID int64 `json:"agentId"`; Open int `json:"open"`; Total int `json:"total"`; First string `json:"first"`
}
func (s *Store) AddTodos(taskID int64, texts []string) (added int, err error) // sare peste textele deja existente pe task
func (s *Store) ListTodos(taskID int64) ([]Todo, error)                      // dupa position
func (s *Store) SetTodo(taskID int64, position int, done bool, by string) error // ErrNotFound daca nu exista
func (s *Store) ListFillIns(projectID int64) ([]FillIn, error)               // task-urile cu puncte deschise, dupa numar
```

- [ ] **Step 1: testul**

`internal/store/todos_test.go`:

```go
package store

import "testing"

func TestTodos(t *testing.T) {
	s := newStore(t)
	p, lead := newProject(t, s)
	task := Task{ProjectID: p.ID, Title: "Terms page", AssigneeID: lead.ID, CreatorID: &lead.ID}
	if err := s.CreateTask(&task); err != nil {
		t.Fatal(err)
	}
	if n, err := s.AddTodos(task.ID, []string{"Company name", "Registration number"}); err != nil || n != 2 {
		t.Fatalf("%d %v", n, err)
	}
	// al doilea raport: textul existent nu se dubleaza, cel nou vine la coada
	if n, _ := s.AddTodos(task.ID, []string{"Company name", "Contact e-mail"}); n != 1 {
		t.Fatalf("adaugate %d, vreau 1", n)
	}
	list, _ := s.ListTodos(task.ID)
	if len(list) != 3 || list[2].Position != 3 || list[2].Text != "Contact e-mail" {
		t.Fatalf("%+v", list)
	}
	if err := s.SetTodo(task.ID, 1, true, "You"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetTodo(task.ID, 9, true, "You"); err != ErrNotFound {
		t.Fatalf("punct inexistent: %v", err)
	}
	fills, _ := s.ListFillIns(p.ID)
	if len(fills) != 1 || fills[0].Open != 2 || fills[0].Total != 3 || fills[0].First != "Registration number" {
		t.Fatalf("%+v", fills)
	}
	s.SetTodo(task.ID, 2, true, "Lead")
	s.SetTodo(task.ID, 3, true, "Lead")
	if fills, _ := s.ListFillIns(p.ID); len(fills) != 0 {
		t.Fatalf("toate bifate, dar: %+v", fills)
	}
	// debifare
	s.SetTodo(task.ID, 2, false, "You")
	if l, _ := s.ListTodos(task.ID); l[1].DoneAt != nil {
		t.Fatal("debifarea nu s-a salvat")
	}
}
```

- [ ] **Step 2: ruleaza, trebuie sa pice**

Run: `go test ./internal/store/ -run TestTodos`
Expected: FAIL (`AddTodos` nedefinit).

- [ ] **Step 3: implementarea**

`020_task_todos.sql`:

```sql
-- "Left for you": ce i-a ramas utilizatorului de completat dupa un task (report_done left_for_user)
CREATE TABLE task_todos (
  id         INTEGER PRIMARY KEY,
  task_id    INTEGER NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
  position   INTEGER NOT NULL,
  text       TEXT NOT NULL,
  done_at    TEXT,
  done_by    TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  UNIQUE (task_id, position)
);
CREATE INDEX task_todos_open ON task_todos(task_id, done_at);
```

`todos.go`:

```go
package store

import (
	"database/sql"
	"strings"
	"time"
)

type Todo struct {
	TaskID   int64      `json:"taskId"`
	Position int        `json:"position"`
	Text     string     `json:"text"`
	DoneAt   *time.Time `json:"doneAt"`
	DoneBy   string     `json:"doneBy"`
}

type FillIn struct {
	TaskID  int64  `json:"taskId"`
	Number  int    `json:"number"`
	Title   string `json:"title"`
	AgentID int64  `json:"agentId"`
	Open    int    `json:"open"`
	Total   int    `json:"total"`
	First   string `json:"first"` // primul punct deschis
}

// AddTodos adauga punctele la coada listei task-ului; un text deja prezent (fara diferenta de litere) se sare.
func (s *Store) AddTodos(taskID int64, texts []string) (int, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	existing := map[string]bool{}
	last := 0
	rows, err := tx.Query(`SELECT position, text FROM task_todos WHERE task_id = ?`, taskID)
	if err != nil {
		return 0, err
	}
	for rows.Next() {
		var pos int
		var text string
		if err := rows.Scan(&pos, &text); err != nil {
			rows.Close()
			return 0, err
		}
		existing[strings.ToLower(text)] = true
		last = max(last, pos)
	}
	rows.Close()
	added := 0
	for _, text := range texts {
		key := strings.ToLower(strings.TrimSpace(text))
		if key == "" || existing[key] {
			continue
		}
		existing[key] = true
		last++
		if _, err := tx.Exec(`INSERT INTO task_todos (task_id, position, text, created_at) VALUES (?, ?, ?, ?)`,
			taskID, last, strings.TrimSpace(text), now()); err != nil {
			return 0, err
		}
		added++
	}
	return added, tx.Commit()
}

func (s *Store) ListTodos(taskID int64) ([]Todo, error) {
	rows, err := s.db.Query(`SELECT task_id, position, text, done_at, done_by FROM task_todos WHERE task_id = ? ORDER BY position`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Todo{}
	for rows.Next() {
		var t Todo
		var done sql.NullString
		if err := rows.Scan(&t.TaskID, &t.Position, &t.Text, &done, &t.DoneBy); err != nil {
			return nil, err
		}
		if done.Valid {
			at := parseTime(done.String)
			t.DoneAt = &at
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *Store) SetTodo(taskID int64, position int, done bool, by string) error {
	if done {
		return mustAffect(s.db.Exec(`UPDATE task_todos SET done_at = ?, done_by = ? WHERE task_id = ? AND position = ?`, now(), by, taskID, position))
	}
	return mustAffect(s.db.Exec(`UPDATE task_todos SET done_at = NULL, done_by = '' WHERE task_id = ? AND position = ?`, taskID, position))
}

func (s *Store) ListFillIns(projectID int64) ([]FillIn, error) {
	rows, err := s.db.Query(`SELECT t.id, t.number, t.title, t.assignee_id,
		SUM(CASE WHEN d.done_at IS NULL THEN 1 ELSE 0 END), COUNT(*),
		(SELECT text FROM task_todos x WHERE x.task_id = t.id AND x.done_at IS NULL ORDER BY position LIMIT 1)
		FROM tasks t JOIN task_todos d ON d.task_id = t.id
		WHERE t.project_id = ? GROUP BY t.id HAVING SUM(CASE WHEN d.done_at IS NULL THEN 1 ELSE 0 END) > 0
		ORDER BY t.number`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []FillIn{}
	for rows.Next() {
		var f FillIn
		var agent sql.NullInt64
		if err := rows.Scan(&f.TaskID, &f.Number, &f.Title, &agent, &f.Open, &f.Total, &f.First); err != nil {
			return nil, err
		}
		f.AgentID = agent.Int64
		out = append(out, f)
	}
	return out, rows.Err()
}
```

Verifica in `internal/store/tasks.go` numele coloanelor (`number`, `assignee_id`, `project_id`); daca difera, adapteaza query-ul.
In `store_test.go`: `if v != 19` -> `if v != 20` si mesajul `vreau 20`.

- [ ] **Step 4: ruleaza**

Run: `go test ./internal/store/`
Expected: PASS

- [ ] **Step 5: commit**

```bash
git add internal/store/migrations/020_task_todos.sql internal/store/todos.go internal/store/todos_test.go internal/store/store_test.go
git commit -m "store: task_todos pentru Left for you (migrarea 020)"
```

### Task 2: MCP: `left_for_user`, `tick_left_for_user`, `comment_task`

**Files:**
- Modify: `internal/mcp/server.go` (Backend: LeaveForUser, TickLeftForUser, CommentTask; `call`; helper `argInts`)
- Modify: `internal/mcp/tools.go` (argumentul `left_for_user` in reportTool; tool-urile noi; `toolsFor`)
- Modify: `internal/mcp/server_test.go` (fakeBackend + teste)
- Create: `internal/team/todos.go`
- Create: `internal/team/todos_test.go`

**Interfaces:**
- Consumes: `store.AddTodos/ListTodos/SetTodo` (Task 1); `t.findTask`, `t.taskMsg`, `t.emit`, `t.work` din `internal/team`.
- Produces:

```go
// mcp.Backend
LeaveForUser(c Caller, task string, items []string) (string, error)
TickLeftForUser(c Caller, task string, items []int) (string, error)
CommentTask(c Caller, task, text string) (string, error)
// mcp
const MaxLeftForUser = 10
func validLeftForUser(items []string) error
```

- [ ] **Step 1: testele mcp**

In `server_test.go`, fakeBackend primeste (apelurile se tin in `f.calls`, ca la celelalte metode):

```go
func (f *fakeBackend) LeaveForUser(c Caller, task string, items []string) (string, error) {
	f.calls = append(f.calls, "leave|"+task+"|"+strings.Join(items, ";"))
	return "2 items left for the user", f.err
}
func (f *fakeBackend) TickLeftForUser(c Caller, task string, items []int) (string, error) {
	f.calls = append(f.calls, fmt.Sprint("tick|", task, "|", items))
	return "ok", f.err
}
func (f *fakeBackend) CommentTask(c Caller, task, text string) (string, error) {
	f.calls = append(f.calls, "comment|"+task+"|"+text)
	return "ok", f.err
}
```

si testul, cu helper-ele existente `setup(t)` (intoarce `fb, ts`), `rpc(t, ts, token, body)` si `toolText`:

```go
func reportBody(left string) string {
	return `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"report_done","arguments":{"summary":"done","left_for_user":` + left + `}}}`
}

func TestReportDoneLeftForUser(t *testing.T) {
	fb, ts := setup(t)
	_, resp := rpc(t, ts, "worker", reportBody(`["Company name","VAT number"]`))
	text, isErr := toolText(t, resp)
	if isErr || !strings.Contains(text, "items left for the user") {
		t.Fatalf("%q %v", text, isErr)
	}
	if got := fb.calls[len(fb.calls)-1]; got != "leave||Company name;VAT number" {
		t.Fatalf("apel = %q", got)
	}
	many := make([]string, 11)
	for i := range many {
		many[i] = fmt.Sprintf("%q", fmt.Sprint("item ", i))
	}
	before := len(fb.calls)
	_, resp = rpc(t, ts, "worker", reportBody("["+strings.Join(many, ",")+"]"))
	if text, isErr := toolText(t, resp); !isErr || !strings.Contains(text, "at most 10") {
		t.Fatalf("%q %v", text, isErr)
	}
	if len(fb.calls) != before {
		t.Fatal("report_done a rulat desi left_for_user era invalid")
	}
	_, resp = rpc(t, ts, "worker", reportBody(`[" "]`))
	if _, isErr := toolText(t, resp); !isErr {
		t.Fatal("punct gol acceptat")
	}
}
```

- [ ] **Step 2: ruleaza, trebuie sa pice**

Run: `go test ./internal/mcp/`
Expected: FAIL (metodele nu exista in interfata / argumentul e ignorat).

- [ ] **Step 3: implementarea mcp**

`tools.go`, `reportTool.InputSchema` primeste:

```go
		"left_for_user": map[string]any{
			"type":        "array",
			"items":       map[string]any{"type": "string"},
			"description": "Optional: up to 10 values only the user can give (e.g. Company registration number), which you left as placeholders. The user answers in the task's comments.",
		},
```

tool-uri noi:

```go
var tickLeftTool = Tool{
	Name:        "tick_left_for_user",
	Description: "Ticks off items you left for the user (left_for_user) once you have filled them in.",
	InputSchema: schema([]string{"task", "items"}, map[string]any{
		"task":  str("The task, e.g. T-124"),
		"items": map[string]any{"type": "array", "items": map[string]any{"type": "integer"}, "description": "The item numbers, e.g. [1, 3]"},
	}),
}

var commentTaskTool = Tool{
	Name:        "comment_task",
	Description: "Answers the user in a task's comments (for example after they gave you a value you left for them).",
	InputSchema: schema([]string{"task", "text"}, map[string]any{
		"task": str("The task, e.g. T-124"),
		"text": str("Your reply, short"),
	}),
}
```

in `toolsFor`, chiar inainte de `out = append(out, askUserTool)`:

```go
	// cine poate raporta poate si lasa / bifa puncte pentru utilizator si raspunde in comentarii
	if has(reportTool.Name) {
		out = append(out, tickLeftTool, commentTaskTool)
	}
```

`server.go`: metodele noi in `Backend` (vezi Interfaces). In `call`, `case "report_done":` devine:

```go
	case "report_done":
		left := argList(p.Arguments, "left_for_user")
		if err := validLeftForUser(left); err != nil {
			return toolResult(err.Error(), true)
		}
		text, err = s.backend.ReportDone(c, arg("task"), arg("summary"))
		if err == nil && len(left) > 0 {
			// raportul a trecut: punctele se lasa pe acelasi task (task gol = task-ul curent)
			extra, lerr := s.backend.LeaveForUser(c, arg("task"), left)
			if lerr != nil {
				text += " Note: the items for the user were not saved: " + lerr.Error()
			} else {
				text += " " + extra
			}
		}
	case "tick_left_for_user":
		text, err = s.backend.TickLeftForUser(c, arg("task"), argInts(p.Arguments, "items"))
	case "comment_task":
		text, err = s.backend.CommentTask(c, arg("task"), arg("text"))
```

si la final:

```go
// MaxLeftForUser: cate puncte poate lasa un agent utilizatorului la un raport
const MaxLeftForUser = 10

func validLeftForUser(items []string) error {
	if len(items) > MaxLeftForUser {
		return fmt.Errorf("left_for_user: at most %d items", MaxLeftForUser)
	}
	for _, it := range items {
		n := len([]rune(strings.TrimSpace(it)))
		if n == 0 || n > 200 {
			return errors.New("left_for_user: each item must be 1-200 characters")
		}
	}
	return nil
}

// argInts citeste o lista de numere (JSON le aduce ca float64; "3" ca text e acceptat si el)
func argInts(args map[string]any, key string) []int {
	raw, _ := args[key].([]any)
	out := []int{}
	for _, v := range raw {
		switch x := v.(type) {
		case float64:
			out = append(out, int(x))
		case string:
			if n, err := strconv.Atoi(strings.TrimSpace(x)); err == nil {
				out = append(out, n)
			}
		}
	}
	return out
}
```

(importuri: `errors`, `strings`.)

- [ ] **Step 4: testele team**

`internal/team/todos_test.go`, cu fixture-ul existent din `fake_test.go` (`setup(t)`: lead + Rook + Vera;
`f.task(t, title, who, status)`; `f.workerCaller(agent, taskID)`):

```go
package team

import (
	"strings"
	"testing"

	"foreman/internal/store"
)

func TestLeaveTickAndComment(t *testing.T) {
	f := setup(t)
	task := f.task(t, "Terms page", f.rook, store.StatusInProgress)
	c := f.workerCaller(f.rook, task.ID)
	if _, err := f.tm.ReportDone(c, "", "page added"); err != nil {
		t.Fatal(err)
	}
	text, err := f.tm.LeaveForUser(c, "", []string{"Company name", "Registration number", "Contact e-mail"})
	if err != nil || !strings.Contains(text, "3 items") {
		t.Fatalf("%q %v", text, err)
	}
	if _, err := f.tm.TickLeftForUser(c, task.Ref(), []int{1, 7}); err == nil {
		t.Fatal("punct inexistent bifat")
	}
	if l, _ := f.st.ListTodos(task.ID); l[0].DoneAt != nil {
		t.Fatal("o eroare nu bifeaza nimic")
	}
	if _, err := f.tm.TickLeftForUser(c, task.Ref(), []int{1}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.tm.CommentTask(c, task.Ref(), "Company name is in."); err != nil {
		t.Fatal(err)
	}
	events, _ := f.st.ListTaskEvents(task.ID)
	last := events[len(events)-1]
	if last.Kind != "comment" || last.Body != "Company name is in." {
		t.Fatalf("%+v", last)
	}
	// Vera nu e asignata: nu poate bifa
	if _, err := f.tm.TickLeftForUser(f.workerCaller(f.vera, 0), task.Ref(), []int{2}); err == nil {
		t.Fatal("un agent strain a bifat")
	}
}
```

- [ ] **Step 5: implementarea team**

`internal/team/todos.go`:

```go
package team

import (
	"errors"
	"fmt"
	"strings"

	"foreman/internal/mcp"
	"foreman/internal/store"
)

// todoTask: task-ul din argument sau cel curent; doar asignatul lui (cel care a raportat) il poate atinge
func (t *Team) todoTask(c mcp.Caller, ref string) (store.Task, store.Agent, error) {
	var task store.Task
	var err error
	if strings.TrimSpace(ref) == "" {
		if c.TaskID == 0 {
			return task, store.Agent{}, errors.New(`say which task: task: "T-3"`)
		}
		task, err = t.st.GetTask(c.TaskID)
	} else {
		task, err = t.findTask(c.ProjectID, ref)
	}
	if err != nil {
		return task, store.Agent{}, err
	}
	a, err := t.st.GetAgent(c.AgentID)
	if err != nil {
		return task, a, err
	}
	if task.AssigneeID != c.AgentID {
		return task, a, fmt.Errorf("%s is not assigned to you", task.Ref())
	}
	return task, a, nil
}

func (t *Team) LeaveForUser(c mcp.Caller, ref string, items []string) (string, error) {
	t.work.Lock()
	defer t.work.Unlock()
	task, a, err := t.todoTask(c, ref)
	if err != nil {
		return "", err
	}
	n, err := t.st.AddTodos(task.ID, items)
	if err != nil {
		return "", err
	}
	if n > 0 {
		t.taskMsg(task, "left_for_user", a.Name, actorUser, fmt.Sprintf("%s left %d %s for you", a.Name, n, plural(n, "item", "items")),
			"- "+strings.Join(items, "\n- "))
		t.emit(c.ProjectID, "task", task)
	}
	return fmt.Sprintf("%d %s left for the user; they answer in the task's comments. When you fill one in, call tick_left_for_user.", n, plural(n, "item", "items")), nil
}

func (t *Team) TickLeftForUser(c mcp.Caller, ref string, items []int) (string, error) {
	t.work.Lock()
	defer t.work.Unlock()
	task, a, err := t.todoTask(c, ref)
	if err != nil {
		return "", err
	}
	if len(items) == 0 {
		return "", errors.New("items: the numbers to tick, e.g. [1, 3]")
	}
	list, err := t.st.ListTodos(task.ID)
	if err != nil {
		return "", err
	}
	for _, n := range items { // intai verificam tot, apoi bifam: o eroare nu lasa jumatate bifat
		if n < 1 || n > len(list) {
			return "", fmt.Errorf("%s has items 1-%d; there is no item %d", task.Ref(), len(list), n)
		}
	}
	for _, n := range items {
		if err := t.st.SetTodo(task.ID, n, true, a.Name); err != nil {
			return "", err
		}
	}
	t.emit(c.ProjectID, "task", task)
	open := 0
	if list, err = t.st.ListTodos(task.ID); err == nil {
		for _, x := range list {
			if x.DoneAt == nil {
				open++
			}
		}
	}
	return fmt.Sprintf("Ticked. %d still open on %s.", open, task.Ref()), nil
}

func (t *Team) CommentTask(c mcp.Caller, ref, text string) (string, error) {
	text = strings.TrimSpace(text)
	if text == "" || len([]rune(text)) > 4000 {
		return "", errors.New("text: 1-4000 characters")
	}
	t.work.Lock()
	defer t.work.Unlock()
	task, a, err := t.todoTask(c, ref)
	if err != nil {
		return "", err
	}
	t.taskMsg(task, "comment", a.Name, actorUser, a.Name+" commented", text)
	t.emit(c.ProjectID, "task", task)
	return "Comment added to " + task.Ref() + ".", nil
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
```

Verifica: `t.emit(projectID, "task", ...)` e forma folosita in alte locuri (cauta `t.emit(` in `tools.go`); foloseste
acelasi tip de eveniment pe care UI-ul il trateaza ca reload (orice eveniment reincarca proiectul). Daca exista deja
un helper `plural`, sterge-l pe acesta.

- [ ] **Step 6: ruleaza**

Run: `go test ./internal/mcp/ ./internal/team/ ./internal/setup/ ./internal/draft/ 2>/dev/null; go vet ./...`
Expected: PASS (pachetul draft exista doar daca planul de draft e facut; altfel scoate-l din comanda)

- [ ] **Step 7: commit**

```bash
git add internal/mcp internal/team/todos.go internal/team/todos_test.go
git commit -m "mcp+team: left_for_user la report_done, tick_left_for_user si comment_task"
```

### Task 3: utilizatorul: comentarii, bifat, "Needs you" si promptul

**Files:**
- Modify: `internal/team/todos.go` (UserComment, UserSetTodo)
- Modify: `internal/web/api_tasks.go` (rute + `todos` in GET task)
- Modify: `internal/web/api_projects.go` (`fillIns` in proiect, `FillIns` in card)
- Modify: `internal/web/server.go` (rutele)
- Modify: `internal/prompt/prompt.go` (howLeftForUser)
- Test: `internal/web/todos_test.go`, `internal/team/todos_test.go`

**Interfaces:**
- Consumes: Task 1 si 2.
- Produces:

```go
func (t *Team) UserComment(taskID int64, text string) error  // eveniment + mesaj catre asignat (il trezeste)
func (t *Team) UserSetTodo(taskID int64, position int, done bool) error
// HTTP
POST /api/tasks/{id}/comments {text}            -> 204; 409 daca asignatul nu mai exista
PUT  /api/tasks/{id}/todos/{pos} {done}         -> 204; 404 punct inexistent
GET  /api/tasks/{id}                             -> + "todos": []store.Todo
GET  /api/projects/{id}                          -> + "fillIns": []store.FillIn
projectCard.FillIns int `json:"fillIns"`        -> intra in "N need you" pe lista de proiecte
```

- [ ] **Step 1: testele**

In `internal/team/todos_test.go`:

```go
func TestUserCommentWakesAssignee(t *testing.T) {
	f := setup(t)
	task := f.task(t, "Terms page", f.rook, store.StatusDone)
	if err := f.tm.UserComment(task.ID, "Company is Meridian Auto SRL."); err != nil {
		t.Fatal(err)
	}
	pending, _ := f.st.Undelivered(f.rook.ID)
	if len(pending) == 0 || !strings.Contains(pending[len(pending)-1].Text, task.Ref()) {
		t.Fatalf("mesajul nu a ajuns la Rook: %+v", pending)
	}
	// task Done cu puncte: bifatul nu ii schimba statusul
	f.st.AddTodos(task.ID, []string{"VAT"})
	if err := f.tm.UserSetTodo(task.ID, 1, true); err != nil {
		t.Fatal(err)
	}
	f.wantStatus(t, task.Number, store.StatusDone)
}
```

`internal/web/todos_test.go`:

```go
func TestTodosAPI(t *testing.T) {
	e := setup(t)
	p := e.createProject(t, t.TempDir())
	w := e.addWorker(t, p.ID, "Canvas")
	task := store.Task{ProjectID: p.ID, Title: "Terms", AssigneeID: w.ID}
	e.st.CreateTask(&task)
	e.st.AddTodos(task.ID, []string{"Company name", "VAT"})
	id := strconv.FormatInt(task.ID, 10)
	if code, raw := e.do(t, "PUT", "/api/tasks/"+id+"/todos/1", map[string]bool{"done": true}); code != http.StatusNoContent {
		t.Fatalf("%d %s", code, raw)
	}
	if code, _ := e.do(t, "PUT", "/api/tasks/"+id+"/todos/9", map[string]bool{"done": true}); code != http.StatusNotFound {
		t.Fatalf("punct lipsa: %d", code)
	}
	_, raw := e.do(t, "GET", "/api/projects/"+strconv.FormatInt(p.ID, 10), nil)
	var d struct {
		FillIns []store.FillIn `json:"fillIns"`
	}
	json.Unmarshal(raw, &d)
	if len(d.FillIns) != 1 || d.FillIns[0].Open != 1 {
		t.Fatalf("%s", raw)
	}
	if code, raw := e.do(t, "POST", "/api/tasks/"+id+"/comments", map[string]string{"text": "Meridian Auto SRL"}); code != http.StatusNoContent {
		t.Fatalf("%d %s", code, raw)
	}
	// asignatul sters: 409
	e.st.DeleteAgent(w.ID)
	if code, _ := e.do(t, "POST", "/api/tasks/"+id+"/comments", map[string]string{"text": "x"}); code != http.StatusConflict {
		t.Fatalf("asignat sters: %d", code)
	}
}
```

(Verifica numele: `store.Task.AssigneeID` e `int64`; daca `CreateTask` cere `CreatorID`, pune-l la lider; `DeleteAgent` poate avea alt nume, cauta-l in `internal/store/agents.go`. Un task al unui agent sters poate disparea prin cascada; in cazul asta testeaza 404 in loc de 409 si noteaza in raport.)

- [ ] **Step 2: ruleaza, trebuie sa pice**

Run: `go test ./internal/team/ ./internal/web/ -run 'TestUserComment|TestTodosAPI'`
Expected: FAIL.

- [ ] **Step 3: implementarea**

`internal/team/todos.go`:

```go
// UserComment: comentariul utilizatorului pe task; ajunge la asignat ca mesaj si il trezeste
func (t *Team) UserComment(taskID int64, text string) error {
	text = strings.TrimSpace(text)
	if text == "" || len([]rune(text)) > 4000 {
		return errors.New("comment: 1-4000 characters")
	}
	t.work.Lock()
	task, err := t.st.GetTask(taskID)
	if err != nil {
		t.work.Unlock()
		return err
	}
	a, err := t.st.GetAgent(task.AssigneeID)
	if err != nil {
		t.work.Unlock()
		return ErrNoAssignee
	}
	t.taskMsg(task, "comment", actorUser, a.Name, "You commented", text)
	msg := fmt.Sprintf("Comment from the user on %s (%s): %s\nIf it fills in something you left for them, update the work, "+
		"tick it with mcp__team__tick_left_for_user and reply with mcp__team__comment_task.", task.Ref(), task.Title, text)
	if _, err := t.addUserMessage(a, clip(msg), nil, true); err != nil {
		t.work.Unlock()
		return err
	}
	t.work.Unlock()
	t.Kick(task.ProjectID)
	return nil
}

var ErrNoAssignee = errors.New("the task's agent no longer exists")

func (t *Team) UserSetTodo(taskID int64, position int, done bool) error {
	t.work.Lock()
	defer t.work.Unlock()
	task, err := t.st.GetTask(taskID)
	if err != nil {
		return err
	}
	if err := t.st.SetTodo(taskID, position, done, actorUser); err != nil {
		return err
	}
	t.emit(task.ProjectID, "task", task)
	return nil
}
```

`api_tasks.go` (handler-e noi; `writeErr(w, http.StatusConflict, ...)` cand `errors.Is(err, team.ErrNoAssignee)`):

```go
func (s *Server) commentTask(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		fail(w, err)
		return
	}
	var in struct{ Text string }
	if err := decode(r, &in); err != nil {
		fail(w, err)
		return
	}
	if err := s.team.UserComment(id, in.Text); err != nil {
		if errors.Is(err, team.ErrNoAssignee) {
			writeErr(w, http.StatusConflict, err.Error())
			return
		}
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) setTodo(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		fail(w, err)
		return
	}
	pos, err := strconv.Atoi(r.PathValue("pos"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad item number")
		return
	}
	var in struct{ Done bool }
	if err := decode(r, &in); err != nil {
		fail(w, err)
		return
	}
	if err := s.team.UserSetTodo(id, pos, in.Done); err != nil {
		fail(w, err) // fail traduce store.ErrNotFound in 404
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
```

In `getTask`: `todos, err := s.st.ListTodos(id)` si `"todos": todos` in raspuns.
Rute in `server.go`:

```go
	mux.HandleFunc("POST /api/tasks/{id}/comments", s.commentTask)
	mux.HandleFunc("PUT /api/tasks/{id}/todos/{pos}", s.setTodo)
```

`api_projects.go`: `projectDetail` primeste `FillIns []store.FillIn `json:"fillIns"``, umplut in `getProject` din
`s.st.ListFillIns(p.ID)`; `projectCard` primeste `FillIns int `json:"fillIns"``, in `card()` = `len(ListFillIns)`.

`internal/prompt/prompt.go`:

```go
const howLeftForUser = `If the work needs values only the user can give (a company name, a registration number, a contact ` +
	`e-mail), do not stop: use a clear placeholder, list each value in left_for_user when you call report_done, and finish. ` +
	`The user answers in the task's comments; then fill them in, tick them with mcp__team__tick_left_for_user and reply ` +
	`with mcp__team__comment_task.`
```

adaugat in `System()` in ramura de worker (`} else if !isRoot {`) si pentru sub-lideri (`if !isRoot` din ramura de lider),
dupa textele existente. Adauga in `prompt_test.go` o aserțiune ca un worker are `left_for_user` in prompt, iar radacina nu.

- [ ] **Step 4: ruleaza**

Run: `go test ./internal/... && go vet ./...`
Expected: PASS

- [ ] **Step 5: commit**

```bash
git add internal/team/todos.go internal/team/todos_test.go internal/web internal/prompt
git commit -m "web+prompt: comentariile utilizatorului, bifat si Left for you in Needs you"
```

### Task 4: UI: caseta din modala si cardul "TO FILL IN"

**Files:**
- Modify: `internal/web/static/js/task-modal.js`
- Modify: `internal/web/static/js/workspace.js` (needs: fillIns; card: "Waiting on you")
- Modify: `internal/web/static/js/projects.js` (fillIns in "N need you")
- Modify: `internal/web/static/app.css`

**Interfaces:**
- Consumes: `data.todos`, `project.fillIns`, `POST /api/tasks/{id}/comments`, `PUT /api/tasks/{id}/todos/{pos}`.

- [ ] **Step 1: modala**

In `task-modal.js`, un nod stabil `todoBox` pus primul in `body` (inaintea `topBox`) si o cheie `todoKey`:

```js
  const todoBox = el("div", { class: "modal-top" });
  let todoKey = null;
  // renderTodos: "Left for you" cu checklist; un click bifeaza / debifeaza pe server
  function renderTodos() {
    const list = data.todos || [];
    const key = JSON.stringify(list.map((x) => [x.position, x.text, !!x.doneAt]));
    if (key === todoKey) return;
    todoKey = key;
    if (!list.length) { todoBox.replaceChildren(); return; }
    const done = list.filter((x) => x.doneAt).length;
    const who = data.assignee || "The agent";
    todoBox.replaceChildren(el("section", { class: "left-for-you" },
      el("div", { class: "lfy-head" }, el("span", { class: "dot", style: "background:var(--amber)" }),
        el("strong", {}, "Left for you"), el("span", { class: "hint" }, `${who} finished the work but needs these from you.`),
        el("span", { class: "grow" }), el("span", { class: "lfy-count" }, `${done} of ${list.length}`)),
      el("div", { class: "lfy-list" }, list.map((x) => {
        const box = el("input", { type: "checkbox", checked: !!x.doneAt, onchange: async () => {
          box.disabled = true;
          try { await api(`/api/tasks/${taskId}/todos/${x.position}`, { method: "PUT", body: { done: box.checked } }); load(); }
          catch (err) { box.checked = !box.checked; toast(err.message); }
          box.disabled = false;
        } });
        return el("label", { class: "check lfy-item" + (x.doneAt ? " done" : "") }, box, el("span", {}, x.text));
      })),
      el("p", { class: "hint" }, `Answer in the comments below. ${who} updates the work and ticks them off.`)));
  }
```

(importa `toast` din `./ui.js`). In `render()`: `renderTodos();` si `body.replaceChildren(todoBox, topBox, reportBox, filesBox, tabsSection)`.

Comentariile: un tab nou "Comments" e inutil; campul sta sub tab-uri, iar comentariile (evenimentele `comment`) apar deja
in tab-ul Conversation (au `body`). Adauga dupa `tabsSection` un formular stabil:

```js
  const commentInput = el("textarea", { rows: 2, placeholder: "Reply to the agent...", "aria-label": "Comment" });
  const commentForm = el("form", { class: "comment-form", onsubmit: async (e) => {
    e.preventDefault();
    const text = commentInput.value.trim();
    if (!text) return;
    try {
      await api(`/api/tasks/${taskId}/comments`, { method: "POST", body: { text } });
      commentInput.value = "";
      setTab("conversation");
      load();
    } catch (err) { toast(err.message); }
  } }, commentInput, el("button", { type: "submit", class: "btn primary small" }, "Send"));
```

pus in `body` dupa `tabsSection`; placeholder-ul se actualizeaza in `render()` cu `Reply to ${who}...`.
`requestClose` intreaba si cand `commentInput.value.trim()` nu e gol (ca la feedback).

In `renderSide`, randul Status: daca `(data.todos || []).some((x) => !x.doneAt)`, pune langa pastila un
`el("span", { class: "side-waiting" }, `Waiting on you · ${done} of ${total}`)`.

- [ ] **Step 2: board si proiecte**

`workspace.js`, in `renderNeeds()`: `const fills = state.project.fillIns || [];` intra in `total`; o lista stabila
`fillList` (ca `stuckList`) cu cheie `fills.map((f) => f.taskId + ":" + f.open).join(",")`, fiecare card:

```js
  function fillNode(f) {
    const agent = state.project.agents.find((a) => a.id === f.agentId);
    return el("div", { class: "question fill-in" },
      el("div", { class: "question-head" }, el("strong", {}, `To fill in · ${f.open}`)),
      el("p", { class: "question-text fill-title" }, f.title),
      el("p", { class: "question-text hint" }, f.first),
      el("div", { class: "actions" },
        el("span", { class: "task-id" }, `T-${f.number}`), agent ? el("span", { class: "hint" }, agent.name) : null,
        el("span", { class: "grow" }),
        el("a", { class: "btn small primary", href: `#/p/${projectId}/tasks/${f.taskId}` }, "Fill in")));
  }
```

In `taskCard`: daca `(state.project.fillIns || []).some((f) => f.taskId === t.id)` si nu e alta linie, `line = el("span", { class: "task-line warn" }, "Waiting on you")`.

`projects.js`: `asks` include `(p.fillIns || 0)`.

`app.css`:

```css
.left-for-you { border: 1px solid var(--amber-line); border-radius: 14px; padding: 20px 22px; display: grid; gap: 14px; background: rgba(242, 181, 68, .03); }
.lfy-head { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; }
.lfy-count { font: 600 14px var(--mono); color: var(--amber); }
.lfy-list { display: grid; gap: 10px; }
.lfy-item { font-size: 15px; }
.lfy-item.done span { text-decoration: line-through; color: var(--muted); }
.comment-form { display: flex; gap: 8px; align-items: flex-end; }
.comment-form textarea { min-height: 48px; }
.side-waiting { color: var(--amber); font-size: 13px; }
.fill-in .fill-title { font-weight: 600; }
```

- [ ] **Step 3: verificare**

`node --check` pe copii `.mjs` ale celor 3 fisiere JS. Pe o copie a bazei de date (port 7799): pune puncte cu
`sqlite3 <copie> "INSERT INTO task_todos ..."` pe un task, verifica cardul din "Needs you", bifeaza din modala,
trimite un comentariu (agentul primeste mesajul; NU porni echipa, ca sa nu ruleze Claude). Screenshot-uri.

- [ ] **Step 4: commit**

```bash
git add internal/web/static
git commit -m "ui: Left for you in modala, comentarii si cardul To fill in din Needs you"
```

### Task 5: spec si CLAUDE.md

- [ ] **Step 1:** in spec, sectiunea Date: inlocuieste `task_comments` cu nota "comentariile sunt task_events kind=comment (decis la plan: fara tabel separat)".
- [ ] **Step 2:** linie in `CLAUDE.md` / Stare: `Gata (Left for you, spec ...): report_done left_for_user, tick_left_for_user, comment_task; task_todos (migrarea 020); comentariile utilizatorului trezesc asignatul; cardul To fill in din Needs you.`
- [ ] **Step 3:** commit `docs: Left for you`.
