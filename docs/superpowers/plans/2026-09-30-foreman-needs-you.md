# "Needs you": plan de implementare

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Agentii pun intrebari explicite utilizatorului (tool MCP `ask_user`); intrebarile deschise apar pe pagina proiectului, pe cardul din Projects si in chat; raspunsul ajunge la agent si il trezeste, iar un task in asteptare continua.

**Architecture:** Tabel `questions` in store; tool `ask_user` in `internal/mcp` servit de `internal/team`; task-ul care asteapta ramane In progress (si nu e re-pus in coada la Stop/restart); raspunsul inchide intrebarea, adauga un mesaj de la utilizator si pune task-ul inapoi in coada. UI: o componenta de intrebare (`questions.js`) folosita in banda "Needs you" si in chat.

**Tech Stack:** Go, SQLite (modernc.org/sqlite), vanilla JS (embed), Claude Code CLI headless.

**Spec:** `docs/superpowers/specs/2026-09-30-foreman-needs-you-design.md`

## Global Constraints

- `go vet ./...`, `go build -race ./...`, `go test ./...` trec dupa fiecare task; cross-compile `GOOS=darwin GOARCH=arm64 go build ./cmd/foreman` si `GOOS=windows go build ./cmd/foreman`.
- JS: fiecare fisier din `internal/web/static/js/` se verifica ca modul: `cp f /tmp/x.mjs && node --check /tmp/x.mjs` (`node --check` pe `.js` nu prinde erorile de sintaxa).
- In JS, o valoare optionala folosita apoi cu `?.` e `cond ? x : null`, nu `cond && x`.
- In app.css, o clasa care seteaza `display` bate atributul `hidden`: daca ascunzi cu `hidden`, adauga regula `[hidden]` potrivita.
- Tot ce vede utilizatorul si prompturile agentilor sunt in engleza; comentariile in romana, fara diacritice.
- Store: o singura conexiune SQLite; intr-o tranzactie doar `tx`; nicio interogare cat e deschis un `*sql.Rows`.
- Analistul din wizard nu primeste `ask_user`.
- Limite: intrebare maxim 2000 de caractere; maxim 4 optiuni, fiecare maxim 60; raspuns maxim 4000.

## Review Focus

1. **Stop team / restart server cu o intrebare deschisa pe un task:** task-ul NU trebuie re-pus in coada (altfel agentul reporneste fara raspuns si intreaba din nou). Test in Task 1 (`TestRequeueSkipsWaitingForAnswer`).
2. **Run terminat normal dupa `ask_user` pe un task:** task-ul ramane In progress, nu merge in review ("finished without report_done"). Test in Task 2.
3. **Raspuns pe o intrebare inchisa deja** (doua tab-uri, dublu click): 409, un singur mesaj trimis. Test in Task 3.
4. **Raspunsul tastat in banda nu se pierde** la reincarcarile dese ale proiectului (fiecare eveniment SSE face reload): banda se reconstruieste doar cand se schimba setul de intrebari. Verificat in Task 5 (browser).
5. **Ordinea la raspuns:** mesajul si re-punerea task-ului in coada se fac sub acelasi lock, inainte de `Kick`, ca urmatorul run sa fie pe task si sa aiba raspunsul in notificari. Test in Task 3.

---

### Task 1: Store: intrebarile

**Files:**
- Create: `internal/store/migrations/007_questions.sql`
- Create: `internal/store/questions.go`
- Modify: `internal/store/tasks.go` (RequeueInProgress, RequeueAllInProgress)
- Test: `internal/store/questions_test.go`

**Interfaces:**
- Produces:
  - `const QuestionOpen = "open"; QuestionAnswered = "answered"; QuestionDismissed = "dismissed"`
  - `type Question struct { ID, ProjectID, AgentID int64; TaskID *int64; RunID, Text string; Options []string; Answer, Status string; CreatedAt time.Time; ClosedAt *time.Time }` (JSON: `id, projectId, agentId, taskId, runId, text, options, answer, status, createdAt, closedAt`)
  - `func (s *Store) AddQuestion(q *Question) error`
  - `func (s *Store) GetQuestion(id int64) (Question, error)` (ErrNotFound)
  - `func (s *Store) ListOpenQuestions(projectID int64) ([]Question, error)` (cele mai vechi primele; mereu non-nil)
  - `func (s *Store) CountOpenQuestions(projectID int64) (int, error)`
  - `func (s *Store) CloseQuestion(id int64, status, answer string) error` (doar din open; altfel ErrConflict; inexistenta -> ErrNotFound)
  - `func (s *Store) HasOpenQuestionForRun(runID string) (bool, error)`

- [ ] **Step 1: Testele** — `internal/store/questions_test.go`:

```go
package store

import (
	"errors"
	"testing"
)

func TestQuestionsLifecycle(t *testing.T) {
	s := newStore(t)
	p, lead := newProject(t, s)
	task := Task{ProjectID: p.ID, Title: "A", AssigneeID: lead.ID}
	if err := s.CreateTask(&task); err != nil {
		t.Fatal(err)
	}
	q := Question{ProjectID: p.ID, AgentID: lead.ID, TaskID: &task.ID, RunID: "run-1", Text: "Allow Bash?", Options: []string{"Yes", "No"}}
	if err := s.AddQuestion(&q); err != nil || q.ID == 0 || q.Status != QuestionOpen {
		t.Fatalf("add: %v %+v", err, q)
	}
	open, _ := s.ListOpenQuestions(p.ID)
	if len(open) != 1 || open[0].Options[1] != "No" || *open[0].TaskID != task.ID {
		t.Fatalf("open: %+v", open)
	}
	if ok, _ := s.HasOpenQuestionForRun("run-1"); !ok {
		t.Fatal("run-1 are o intrebare deschisa")
	}
	if ok, _ := s.HasOpenQuestionForRun("run-2"); ok {
		t.Fatal("run-2 nu are")
	}
	if err := s.CloseQuestion(q.ID, QuestionAnswered, "Yes"); err != nil {
		t.Fatal(err)
	}
	if err := s.CloseQuestion(q.ID, QuestionDismissed, ""); !errors.Is(err, ErrConflict) {
		t.Fatalf("a doua inchidere: %v", err)
	}
	got, _ := s.GetQuestion(q.ID)
	if got.Status != QuestionAnswered || got.Answer != "Yes" || got.ClosedAt == nil {
		t.Fatalf("%+v", got)
	}
	if n, _ := s.CountOpenQuestions(p.ID); n != 0 {
		t.Fatalf("count: %d", n)
	}
	if _, err := s.GetQuestion(9999); !errors.Is(err, ErrNotFound) {
		t.Fatalf("inexistenta: %v", err)
	}
	if err := s.CloseQuestion(9999, QuestionAnswered, "x"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("inchidere inexistenta: %v", err)
	}
	// fara task si fara optiuni
	q2 := Question{ProjectID: p.ID, AgentID: lead.ID, Text: "Which DB?"}
	s.AddQuestion(&q2)
	if list, _ := s.ListOpenQuestions(p.ID); len(list) != 1 || list[0].TaskID != nil || list[0].Options == nil {
		t.Fatalf("%+v", list)
	}
}

// un task care asteapta raspunsul nu se re-pune in coada la Stop team / restart
func TestRequeueSkipsWaitingForAnswer(t *testing.T) {
	s := newStore(t)
	p, lead := newProject(t, s)
	waiting := Task{ProjectID: p.ID, Title: "W", AssigneeID: lead.ID}
	other := Task{ProjectID: p.ID, Title: "O", AssigneeID: lead.ID}
	for _, tk := range []*Task{&waiting, &other} {
		s.CreateTask(tk)
		tk.Status = StatusInProgress
		s.SaveTask(tk)
	}
	s.AddQuestion(&Question{ProjectID: p.ID, AgentID: lead.ID, TaskID: &waiting.ID, Text: "?"})
	if err := s.RequeueInProgress(p.ID); err != nil {
		t.Fatal(err)
	}
	w, _ := s.GetTask(waiting.ID)
	o, _ := s.GetTask(other.ID)
	if w.Status != StatusInProgress || o.Status != StatusQueued {
		t.Fatalf("waiting=%s other=%s", w.Status, o.Status)
	}
	if err := s.RequeueAllInProgress(); err != nil {
		t.Fatal(err)
	}
	if w, _ := s.GetTask(waiting.ID); w.Status != StatusInProgress {
		t.Fatalf("RequeueAll: %s", w.Status)
	}
}
```

(Daca `CreateTask`/`SaveTask` au alt nume sau cer alte campuri, foloseste functiile existente din `internal/store/tasks.go` si ajusteaza minim testul.)

- [ ] **Step 2: RED** — `go test ./internal/store/` → eroare de compilare.

- [ ] **Step 3: Migrarea** — `internal/store/migrations/007_questions.sql`:

```sql
-- intrebarile agentilor catre utilizator ("Needs you")
CREATE TABLE questions (
  id          INTEGER PRIMARY KEY,
  project_id  INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  agent_id    INTEGER NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
  task_id     INTEGER REFERENCES tasks(id) ON DELETE SET NULL,
  run_id      TEXT NOT NULL DEFAULT '',
  text        TEXT NOT NULL,
  options     TEXT NOT NULL DEFAULT '[]',
  answer      TEXT NOT NULL DEFAULT '',
  status      TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'answered', 'dismissed')),
  created_at  TEXT NOT NULL,
  closed_at   TEXT
);
CREATE INDEX questions_project ON questions(project_id, status);
CREATE INDEX questions_run ON questions(run_id);
```

Actualizeaza testul existent care verifica `user_version` (6 -> 7) daca exista.

- [ ] **Step 4: `internal/store/questions.go`**

```go
package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

// Starile unei intrebari puse de un agent utilizatorului.
const (
	QuestionOpen      = "open"
	QuestionAnswered  = "answered"
	QuestionDismissed = "dismissed"
)

// Question e o intrebare a unui agent catre utilizator (tool-ul ask_user).
type Question struct {
	ID        int64      `json:"id"`
	ProjectID int64      `json:"projectId"`
	AgentID   int64      `json:"agentId"`
	TaskID    *int64     `json:"taskId"` // task-ul run-ului care a intrebat; nil = fara task
	RunID     string     `json:"runId"`
	Text      string     `json:"text"`
	Options   []string   `json:"options"` // raspunsuri scurte propuse; mereu non-nil
	Answer    string     `json:"answer"`
	Status    string     `json:"status"`
	CreatedAt time.Time  `json:"createdAt"`
	ClosedAt  *time.Time `json:"closedAt"`
}

const questionCols = `id, project_id, agent_id, task_id, run_id, text, options, answer, status, created_at, closed_at`

func scanQuestion(sc scanner) (Question, error) {
	var q Question
	var opts, created string
	var closed sql.NullString
	err := sc.Scan(&q.ID, &q.ProjectID, &q.AgentID, &q.TaskID, &q.RunID, &q.Text, &opts, &q.Answer, &q.Status, &created, &closed)
	if errors.Is(err, sql.ErrNoRows) {
		return Question{}, ErrNotFound
	}
	if err != nil {
		return Question{}, err
	}
	if err := json.Unmarshal([]byte(opts), &q.Options); err != nil || q.Options == nil {
		q.Options = []string{}
	}
	q.CreatedAt = parseTime(created)
	if closed.Valid {
		t := parseTime(closed.String)
		q.ClosedAt = &t
	}
	return q, nil
}

// AddQuestion salveaza o intrebare deschisa.
func (s *Store) AddQuestion(q *Question) error {
	if q.Options == nil {
		q.Options = []string{}
	}
	opts, _ := json.Marshal(q.Options)
	created := now()
	res, err := s.db.Exec(`INSERT INTO questions (project_id, agent_id, task_id, run_id, text, options, status, created_at)
		VALUES (?, ?, ?, ?, ?, ?, 'open', ?)`, q.ProjectID, q.AgentID, nullPtr(q.TaskID), q.RunID, q.Text, string(opts), created)
	if err != nil {
		return err
	}
	if q.ID, err = res.LastInsertId(); err != nil {
		return err
	}
	q.Status, q.CreatedAt = QuestionOpen, parseTime(created)
	return nil
}

func (s *Store) GetQuestion(id int64) (Question, error) {
	return scanQuestion(s.db.QueryRow(`SELECT `+questionCols+` FROM questions WHERE id = ?`, id))
}

// ListOpenQuestions intoarce intrebarile deschise ale proiectului, cele mai vechi primele.
func (s *Store) ListOpenQuestions(projectID int64) ([]Question, error) {
	rows, err := s.db.Query(`SELECT `+questionCols+` FROM questions WHERE project_id = ? AND status = 'open' ORDER BY id`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Question{}
	for rows.Next() {
		q, err := scanQuestion(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, q)
	}
	return out, rows.Err()
}

func (s *Store) CountOpenQuestions(projectID int64) (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM questions WHERE project_id = ? AND status = 'open'`, projectID).Scan(&n)
	return n, err
}

// CloseQuestion inchide o intrebare deschisa (raspuns sau dismiss). O intrebare
// deja inchisa intoarce ErrConflict, una inexistenta ErrNotFound.
func (s *Store) CloseQuestion(id int64, status, answer string) error {
	res, err := s.db.Exec(`UPDATE questions SET status = ?, answer = ?, closed_at = ? WHERE id = ? AND status = 'open'`,
		status, answer, now(), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 1 {
		return nil
	}
	if _, err := s.GetQuestion(id); err != nil {
		return err
	}
	return ErrConflict
}

// HasOpenQuestionForRun: run-ul a pus o intrebare care inca asteapta raspuns.
func (s *Store) HasOpenQuestionForRun(runID string) (bool, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM questions WHERE run_id = ? AND run_id != '' AND status = 'open'`, runID).Scan(&n)
	return n > 0, err
}
```

- [ ] **Step 5: Re-punerea in coada** — in `internal/store/tasks.go`, langa `notWaitingParent`, adauga un fragment si foloseste-l in ambele functii:

```go
// notWaitingAnswer: un task al carui agent asteapta raspunsul utilizatorului
// ramane In progress; raspunsul il pune inapoi in coada.
const notWaitingAnswer = ` AND id NOT IN (SELECT task_id FROM questions WHERE status = 'open' AND task_id IS NOT NULL)`
```

`WHERE project_id = ? AND status = 'in_progress'`+notWaitingParent+notWaitingAnswer (la fel in `RequeueAllInProgress`). Actualizeaza comentariile functiilor.

- [ ] **Step 6: GREEN** — `go test ./internal/store/ && go vet ./... && go test ./...`

- [ ] **Step 7: Commit** — `git commit -m "store: intrebarile agentilor (questions) si task-uri care asteapta raspuns"`

---

### Task 2: Tool-ul `ask_user`, promptul si task-ul care asteapta

**Files:**
- Modify: `internal/mcp/server.go` (Backend.AskUser, case "ask_user", argument `options` ca lista)
- Modify: `internal/mcp/tools.go` (askUserTool, toolsFor)
- Modify: `internal/team/tools.go` (AskUser)
- Modify: `internal/team/run.go` (recordOutcome)
- Modify: `internal/prompt/prompt.go` (howAsk)
- Modify: `internal/setup/manager.go` doar daca `backend` nu compileaza (embed-ul `mcp.Backend` ar trebui sa acopere metoda noua)
- Test: `internal/mcp/server_test.go`, `internal/team/questions_test.go` (nou), `internal/prompt/prompt_test.go`

**Interfaces:**
- Consumes: store din Task 1.
- Produces: `mcp.Backend.AskUser(c Caller, question string, options []string) (string, error)`; evenimentul SSE `question` (data: `store.Question`).

- [ ] **Step 1: Testele**

In `internal/team/questions_test.go` (foloseste `setup`, `f.task`, `mustReconcile`, `f.fs`):

```go
package team

import (
	"strings"
	"testing"

	"foreman/internal/mcp"
	"foreman/internal/runner"
	"foreman/internal/store"
)

func TestAskUserKeepsTaskWaiting(t *testing.T) {
	f := setup(t)
	task := f.task(t, "A", f.rook, store.StatusQueued)
	mustReconcile(t, f)
	spec := f.fs.last(t, f.rook)
	c := mcp.Caller{ProjectID: f.p.ID, AgentID: f.rook.ID, RunID: spec.ID, TaskID: task.ID, HasParent: true}

	if _, err := f.tm.AskUser(c, "  ", nil); err == nil {
		t.Fatal("intrebare goala")
	}
	if _, err := f.tm.AskUser(c, "Q", []string{"a", "b", "c", "d", "e"}); err == nil {
		t.Fatal("prea multe optiuni")
	}
	out, err := f.tm.AskUser(c, "Allow Bash for the tests?", []string{"Yes", "No"})
	if err != nil || !strings.Contains(out, "Stop now") {
		t.Fatalf("%q %v", out, err)
	}
	qs, _ := f.st.ListOpenQuestions(f.p.ID)
	if len(qs) != 1 || qs[0].TaskID == nil || *qs[0].TaskID != task.ID || qs[0].RunID != spec.ID {
		t.Fatalf("%+v", qs)
	}

	// run-ul se termina fara report_done: task-ul ramane In progress
	f.fs.finish(spec.ID, runner.Outcome{Status: store.RunDone, Result: "I asked the user."})
	got, _ := f.st.GetTask(task.ID)
	if got.Status != store.StatusInProgress {
		t.Fatalf("task: %s", got.Status)
	}
	msgs, _ := f.st.ListMessages(f.rook.ID)
	found := false
	for _, m := range msgs {
		if strings.Contains(m.Text, "waiting for your answer") {
			found = true
		}
	}
	if !found {
		t.Fatal("lipseste nota 'waiting for your answer'")
	}
}
```

(Foloseste constanta existenta pentru un run terminat normal — `store.RunDone` sau cum se numeste in `internal/store/runs.go`.)

In `internal/mcp/server_test.go`: `ask_user` apare in `tools/list` pentru un worker, un lead si un agent radacina fara copii; NU apare pentru un Caller cu `SetupID`. Ajusteaza testele existente care compara liste exacte de tool-uri.

In `internal/prompt/prompt_test.go`: `System` contine `mcp__team__ask_user` pentru orice agent, iar pentru un agent cu sef contine si "Do not call report_done while you wait for the answer".

- [ ] **Step 2: RED** — `go test ./internal/mcp/ ./internal/team/ ./internal/prompt/`

- [ ] **Step 3: MCP**

`tools.go`:

```go
var askUserTool = Tool{
	Name:        "ask_user",
	Description: "Ask the user a question when you need a decision, a permission or information only they can give. After calling it, stop: the answer arrives later as a message. Do not call report_done while you wait.",
	InputSchema: schema([]string{"question"}, map[string]any{
		"question": str("The question, clear and self-contained (what you need and why)"),
		"options": map[string]any{
			"type":        "array",
			"items":       map[string]any{"type": "string"},
			"description": "Optional: 2-4 short answers the user can pick with one click",
		},
	}),
}
```

In `toolsFor`, pentru orice caller al echipei (nu `SetupID`), la final, inainte de `return out`: `out = append(out, askUserTool)`. Comentariul functiei nu mai spune "un agent radacina fara copii nu primeste niciun tool" (acum primeste `ask_user`).

`server.go`: `AskUser(c Caller, question string, options []string) (string, error)` in `Backend`; in `call`:

```go
	case "ask_user":
		text, err = s.backend.AskUser(c, arg("question"), argList(p.Arguments, "options"))
```

cu:

```go
// argList citeste un argument lista de texte (ignora elementele care nu sunt text).
func argList(args map[string]any, key string) []string {
	raw, _ := args[key].([]any)
	out := []string{}
	for _, v := range raw {
		if s, ok := v.(string); ok {
			out = append(out, s)
		}
	}
	return out
}
```

Orice implementare de test a `Backend` (fake-uri in `internal/mcp`) primeste metoda `AskUser`.

- [ ] **Step 4: `Team.AskUser`** (`internal/team/tools.go`):

```go
// AskUser salveaza o intrebare catre utilizator ("Needs you"). Agentul se opreste
// dupa ea; raspunsul vine ca mesaj (AnswerQuestion).
func (t *Team) AskUser(c mcp.Caller, question string, options []string) (string, error) {
	question = strings.TrimSpace(question)
	if question == "" {
		return "", errors.New("question is required")
	}
	if utf8.RuneCountInString(question) > 2000 {
		return "", errors.New("the question is too long: at most 2000 characters")
	}
	opts := []string{}
	for _, o := range options {
		if o = strings.TrimSpace(o); o != "" {
			opts = append(opts, o)
		}
	}
	if len(opts) > 4 {
		return "", errors.New("at most 4 options")
	}
	for _, o := range opts {
		if utf8.RuneCountInString(o) > 60 {
			return "", errors.New("each option must be at most 60 characters")
		}
	}
	t.work.Lock()
	defer t.work.Unlock()
	q := store.Question{ProjectID: c.ProjectID, AgentID: c.AgentID, RunID: c.RunID, Text: question, Options: opts}
	if c.TaskID != 0 {
		id := c.TaskID
		q.TaskID = &id
	}
	if err := t.st.AddQuestion(&q); err != nil {
		return "", err
	}
	t.note(c.ProjectID, c.AgentID, clip("Asked you: "+question), true)
	t.emit(c.ProjectID, "question", q)
	return "Question sent to the user. Stop now; the answer will arrive as a message.", nil
}
```

(Importa `unicode/utf8` daca lipseste.)

- [ ] **Step 5: Task-ul care asteapta** (`internal/team/run.go`, `recordOutcome`): in ramura `default:` (run terminat normal, fara report_done, task inca In progress la agent), inainte de `toReview`:

```go
	default:
		// a intrebat utilizatorul (ask_user): task-ul asteapta raspunsul, nu merge in review
		if waiting, err := t.st.HasOpenQuestionForRun(o.RunID); err != nil {
			return err
		} else if waiting {
			return t.note(ar.projectID, a.ID, task.Ref()+" is waiting for your answer.", true)
		}
		task.Result = strings.TrimSpace(o.Result)
		...
```

- [ ] **Step 6: Promptul** (`internal/prompt/prompt.go`):

```go
const howAsk = `If you need a decision, a permission or information that only the user can give, call mcp__team__ask_user with a clear question (and short options if it is a choice), then stop. Do not guess, and do not just end your message with a question.`

const howAskWait = `Do not call report_done while you wait for the answer.`
```

In `System`, dupa construirea listei `how` (inainte de `section(&b, "How you work", ...)`): `how = append(how, howAsk)`; daca agentul are sef (`!isRoot`), adauga si `howAskWait`. Actualizeaza testele de prompt care compara textul exact.

- [ ] **Step 7: GREEN** — `go test ./... && go vet ./... && go build -race ./...` + cross-compile.

- [ ] **Step 8: Commit** — `git commit -m "ask_user: agentii intreaba utilizatorul; task-ul asteapta raspunsul"`

---

### Task 3: Raspunsul (team + web)

**Files:**
- Modify: `internal/team/reconcile.go` (UserMessage -> addUserMessage; AnswerQuestion, DismissQuestion)
- Modify: `internal/web/api_projects.go` (Questions pe proiect, OpenQuestions pe card)
- Create: `internal/web/api_questions.go`
- Modify: `internal/web/server.go` (rute)
- Test: `internal/team/questions_test.go`, `internal/web/questions_test.go` (nou)

**Interfaces:**
- Produces:
  - `func (t *Team) AnswerQuestion(id int64, text string) error`, `func (t *Team) DismissQuestion(id int64) error` (ErrNotFound, ErrConflict din store)
  - `POST /api/questions/{id}/answer {text}` -> 204; `POST /api/questions/{id}/dismiss` -> 204; 404 / 409 / 400 (text gol sau > 4000)
  - `GET /api/projects/{id}` -> `questions: []store.Question`; `GET /api/projects` -> `openQuestions: int` pe fiecare card

- [ ] **Step 1: Testele (team)** — in `internal/team/questions_test.go`:

```go
func TestAnswerRequeuesTaskWithMessage(t *testing.T) {
	f := setup(t)
	task := f.task(t, "A", f.rook, store.StatusQueued)
	mustReconcile(t, f)
	spec := f.fs.last(t, f.rook)
	c := mcp.Caller{ProjectID: f.p.ID, AgentID: f.rook.ID, RunID: spec.ID, TaskID: task.ID, HasParent: true}
	f.tm.AskUser(c, "Allow Bash?", []string{"Yes", "No"})
	f.fs.finish(spec.ID, runner.Outcome{Status: store.RunDone})
	qs, _ := f.st.ListOpenQuestions(f.p.ID)

	if err := f.tm.AnswerQuestion(qs[0].ID, "Yes, Shell is enabled now"); err != nil {
		t.Fatal(err)
	}
	if err := f.tm.AnswerQuestion(qs[0].ID, "again"); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("a doua oara: %v", err)
	}
	mustReconcile(t, f)
	next := f.fs.last(t, f.rook)
	if next.ID == spec.ID || !strings.Contains(turn(next), "Task T-") || !strings.Contains(turn(next), `Answer to your question "Allow Bash?": Yes, Shell is enabled now`) {
		t.Fatalf("urmatorul run trebuia sa fie pe task, cu raspunsul:\n%s", turn(next))
	}
	msgs, _ := f.st.ListMessages(f.rook.ID)
	n := 0
	for _, m := range msgs {
		if strings.Contains(m.Text, "Answer to your question") {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("mesaje de raspuns: %d", n)
	}
}

func TestDismissQuestionWithoutTask(t *testing.T) {
	f := setup(t)
	c := mcp.Caller{ProjectID: f.p.ID, AgentID: f.lead.ID, RunID: "r"}
	f.tm.AskUser(c, "Which DB?", nil)
	qs, _ := f.st.ListOpenQuestions(f.p.ID)
	if err := f.tm.DismissQuestion(qs[0].ID); err != nil {
		t.Fatal(err)
	}
	got, _ := f.st.GetQuestion(qs[0].ID)
	if got.Status != store.QuestionDismissed {
		t.Fatalf("%+v", got)
	}
	msgs, _ := f.st.ListMessages(f.lead.ID)
	if !strings.Contains(msgs[len(msgs)-1].Text, "dismissed your question") {
		t.Fatalf("%+v", msgs[len(msgs)-1])
	}
}
```

(Importa `errors`. `turn(spec)` exista in reconcile_test.go. Daca referinta task-ului din tura are alt format, ajusteaza "Task T-".)

- [ ] **Step 2: Testele (web)** — `internal/web/questions_test.go`:

```go
package web

import (
	"encoding/json"
	"testing"

	"foreman/internal/mcp"
)

func TestQuestionsAPI(t *testing.T) {
	e := setup(t)
	p := e.createProject(t, t.TempDir())
	lead := e.root(t, p.ID)
	e.tm.AskUser(mcp.Caller{ProjectID: p.ID, AgentID: lead.ID, RunID: "r"}, "Which DB?", []string{"Postgres", "MySQL"})

	_, raw := e.do(t, "GET", "/api/projects/"+itoa(p.ID), nil)
	var d struct {
		Questions []struct {
			ID      int64
			Text    string
			Options []string
		} `json:"questions"`
	}
	json.Unmarshal(raw, &d)
	if len(d.Questions) != 1 || d.Questions[0].Options[0] != "Postgres" {
		t.Fatalf("%s", raw)
	}
	_, raw = e.do(t, "GET", "/api/projects", nil)
	var cards []struct {
		OpenQuestions int `json:"openQuestions"`
	}
	json.Unmarshal(raw, &cards)
	if len(cards) != 1 || cards[0].OpenQuestions != 1 {
		t.Fatalf("%s", raw)
	}

	id := itoa(d.Questions[0].ID)
	if code, _ := e.do(t, "POST", "/api/questions/"+id+"/answer", map[string]any{"text": "  "}); code != 400 {
		t.Fatalf("raspuns gol: %d", code)
	}
	if code, _ := e.do(t, "POST", "/api/questions/"+id+"/answer", map[string]any{"text": "Postgres"}); code != 204 {
		t.Fatalf("answer: %d", code)
	}
	if code, _ := e.do(t, "POST", "/api/questions/"+id+"/answer", map[string]any{"text": "MySQL"}); code != 409 {
		t.Fatalf("a doua oara: %d", code)
	}
	if code, _ := e.do(t, "POST", "/api/questions/"+id+"/dismiss", nil); code != 409 {
		t.Fatalf("dismiss dupa raspuns: %d", code)
	}
	if code, _ := e.do(t, "POST", "/api/questions/99999/dismiss", nil); code != 404 {
		t.Fatalf("inexistenta: %d", code)
	}
}
```

(`e.tm` e `*team.Team` in env-ul de test.)

- [ ] **Step 3: RED** — `go test ./internal/team/ ./internal/web/`

- [ ] **Step 4: Team** (`internal/team/reconcile.go`):

`UserMessage` se imparte: corpul (fara lock si fara `Kick`) devine `addUserMessage(a store.Agent, text string, attachmentIDs []int64) (store.Message, error)`; `UserMessage` = lock + `GetAgent` + `addUserMessage` + unlock + `Kick`. Comportamentul ramane identic.

```go
// AnswerQuestion inchide intrebarea cu raspunsul utilizatorului, i-l trimite
// agentului ca mesaj si, daca intrebarea era pe un task care asteapta, pune
// task-ul inapoi in coada (acelasi agent): urmatorul run e pe task, cu raspunsul.
func (t *Team) AnswerQuestion(id int64, text string) error {
	return t.closeQuestion(id, store.QuestionAnswered, strings.TrimSpace(text))
}

// DismissQuestion inchide intrebarea fara raspuns; agentul afla asta.
func (t *Team) DismissQuestion(id int64) error {
	return t.closeQuestion(id, store.QuestionDismissed, "")
}

func (t *Team) closeQuestion(id int64, status, answer string) error {
	t.work.Lock()
	q, err := t.st.GetQuestion(id)
	if err != nil {
		t.work.Unlock()
		return err
	}
	if err := t.st.CloseQuestion(id, status, answer); err != nil {
		t.work.Unlock()
		return err
	}
	a, err := t.st.GetAgent(q.AgentID)
	if err != nil {
		t.work.Unlock()
		return err
	}
	short := q.Text
	if r := []rune(short); len(r) > 200 {
		short = string(r[:200]) + "..."
	}
	msg := fmt.Sprintf("Answer to your question %q: %s", short, answer)
	if status == store.QuestionDismissed {
		msg = fmt.Sprintf("The user dismissed your question %q without answering. Decide yourself or ask differently.", short)
	}
	// intai task-ul in coada, apoi mesajul: amandoua sub lock, inainte de Kick
	if q.TaskID != nil && !t.isActive(a.ID) {
		if task, err := t.st.GetTask(*q.TaskID); err == nil && task.Status == store.StatusInProgress && task.AssigneeID == a.ID {
			task.Status = store.StatusQueued
			if err := t.st.SaveTask(&task); err != nil {
				t.work.Unlock()
				return err
			}
			t.emit(q.ProjectID, "task", task)
		}
	}
	if _, err := t.addUserMessage(a, clip(msg), nil); err != nil {
		t.work.Unlock()
		return err
	}
	q.Status, q.Answer = status, answer
	t.emit(q.ProjectID, "question", q)
	t.work.Unlock()
	t.Kick(q.ProjectID)
	return nil
}
```

(Daca `isActive` sau `SaveTask` au alte nume, foloseste-le pe cele existente.)

- [ ] **Step 5: Web**

`internal/web/api_questions.go`:

```go
package web

import (
	"errors"
	"net/http"
	"strings"

	"foreman/internal/store"
)

func (s *Server) answerQuestion(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		fail(w, err)
		return
	}
	var in struct {
		Text string `json:"text"`
	}
	if err := decode(r, &in); err != nil {
		fail(w, err)
		return
	}
	text := strings.TrimSpace(in.Text)
	if text == "" {
		fail(w, badRequest("the answer is empty"))
		return
	}
	if err := maxLen("answer", text, 4000); err != nil {
		fail(w, err)
		return
	}
	questionDone(w, s.team.AnswerQuestion(id, text))
}

func (s *Server) dismissQuestion(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		fail(w, err)
		return
	}
	questionDone(w, s.team.DismissQuestion(id))
}

// questionDone traduce rezultatul inchiderii unei intrebari in raspuns HTTP.
func questionDone(w http.ResponseWriter, err error) {
	switch {
	case err == nil:
		w.WriteHeader(http.StatusNoContent)
	case errors.Is(err, store.ErrConflict):
		writeErr(w, http.StatusConflict, "the question was already answered or dismissed")
	default:
		fail(w, err)
	}
}
```

Rute in `server.go`: `POST /api/questions/{id}/answer`, `POST /api/questions/{id}/dismiss`.

In `api_projects.go`: `projectCard` primeste `OpenQuestions int \`json:"openQuestions"\``, calculat in `card()` cu `s.st.CountOpenQuestions(p.ID)`; `projectDetail` primeste `Questions []store.Question \`json:"questions"\``, completat in `getProject` cu `s.st.ListOpenQuestions(p.ID)`.

- [ ] **Step 6: GREEN** — `go test ./... && go vet ./... && go build -race ./...` + cross-compile.

- [ ] **Step 7: Commit** — `git commit -m "raspuns la intrebari: mesaj catre agent, task inapoi in coada, API"`

---

### Task 4: UI: banda "Needs you", chat, semne, card

**Files:**
- Create: `internal/web/static/js/questions.js`
- Modify: `internal/web/static/js/workspace.js`, `internal/web/static/js/canvas.js`, `internal/web/static/js/projects.js`, `internal/web/static/app.css`

**Interfaces:**
- Consumes: `project.questions` (`{id, agentId, taskId, text, options}`), `card.openQuestions`, rutele answer/dismiss.
- Produces: `questionBox(q, { agentName, taskRef, onDone })` in `questions.js`.

- [ ] **Step 1: Componenta** — `internal/web/static/js/questions.js`:

```js
import { api } from "./api.js";
import { el, toast } from "./ui.js";

// questionBox: o intrebare a unui agent catre utilizator, cu optiunile ca
// butoane si un raspuns liber (Send / Dismiss). onDone se cheama dupa trimitere.
export function questionBox(q, { agentName, taskRef, onDone }) {
  const answer = el("textarea", { rows: 2, maxlength: 4000, placeholder: "Your answer" });
  const send = async (text) => {
    if (!text.trim()) { answer.focus(); return; }
    busy(true);
    try {
      await api(`/api/questions/${q.id}/answer`, { method: "POST", body: { text } });
      await onDone?.();
    } catch (err) {
      toast(err.message);
      busy(false);
    }
  };
  const dismiss = async () => {
    busy(true);
    try {
      await api(`/api/questions/${q.id}/dismiss`, { method: "POST" });
      await onDone?.();
    } catch (err) {
      toast(err.message);
      busy(false);
    }
  };
  const buttons = [];
  const button = (label, cls, onclick) => {
    const b = el("button", { type: "button", class: cls, onclick }, label);
    buttons.push(b);
    return b;
  };
  function busy(on) { for (const b of buttons) b.disabled = on; answer.disabled = on; }
  answer.addEventListener("keydown", (e) => {
    if (e.key === "Enter" && !e.shiftKey) { e.preventDefault(); send(answer.value); }
  });
  return el("div", { class: "question" },
    el("div", { class: "question-head" }, el("strong", {}, agentName), taskRef ? " · " + taskRef : null),
    el("p", { class: "question-text" }, q.text),
    q.options.length ? el("div", { class: "actions" }, q.options.map((o) => button(o, "btn small", () => send(o)))) : null,
    el("div", { class: "question-answer" }, answer,
      button("Send", "btn primary small", () => send(answer.value)),
      button("Dismiss", "btn small", dismiss)));
}
```

- [ ] **Step 2: Banda pe pagina proiectului** (`workspace.js`)

- Importa `questionBox`. Creeaza o singura data `const needsBox = el("section", { class: "needs-you", "aria-live": "polite" });` si pune-l in `header` intre page-head si `tabsNav` (in `renderHeader`: `header.replaceChildren(pageHead, needsBox, tabsNav)`).
- `renderNeeds()`, chemata din `renderAll()`: construieste cheia `const key = (p.questions || []).map((q) => q.id).join(",")`; daca e egala cu cheia de la ultima desenare, **nu face nimic** (ca raspunsul tastat sa nu se piarda la reincarcari). Altfel:
  - `needsBox.hidden = !questions.length`;
  - `needsBox.replaceChildren(el("h2", {}, \`Needs you (${questions.length})\`), questions.map((q) => questionBox(q, { agentName, taskRef, onDone: reload })))`;
  - `agentName` e numele agentului din `p.agents`;
  - `taskRef` e `T-<number>` din `p.tasks`, dupa `q.taskId`, sau `null`.
- Helper `openQuestionOf(agentId)`, care intoarce prima intrebare deschisa a agentului.

- [ ] **Step 3: Chat** — deasupra lui `composer`, un container `chatQuestion` creat o singura data si pus in `chatBox`, intre `thread` si `composer`. In `renderConsoleHead()` sau in `renderAll()`, cu aceeasi tehnica de cheie (`id`-ul intrebarii agentului selectat), il umpli cu `questionBox(q, {...})` sau il golesti (`hidden`).

- [ ] **Step 4: Semne pe agent**
  - **Banda echipei:** in `renderStrip`, pe butonul agentului cu intrebare deschisa, adauga `el("span", { class: "ask-mark", title: "Waiting for your answer" }, "?")`.
  - **Canvas:** in `agentNode` (`canvas.js`), daca `(project.questions || []).some((q) => q.agentId === a.id)`, adauga in grupul nodului `svg("text", { class: "ask-mark", x: NODE_W - 40, y: 18 }, "?")`.

- [ ] **Step 5: Cardul din Projects** — in `projectCard(p)` (`projects.js`):
  - in `card-top`, dupa `pill`, adauga `p.openQuestions ? el("span", { class: "pill pill-ask" }, \`${p.openQuestions} ${p.openQuestions === 1 ? "needs" : "need"} you\`) : null`;
  - pe `a.card` adauga clasa `needs-you-card` cand `p.openQuestions > 0`.

- [ ] **Step 6: CSS** (`app.css`):

```css
/* intrebari catre utilizator ("Needs you") */
.needs-you { display: grid; gap: 10px; margin: 8px 0 12px; }
.needs-you[hidden] { display: none; }
.needs-you h2 { font-size: 14px; color: var(--amber); margin: 0; }
.question { border: 1px solid var(--amber); background: var(--amber-bg); border-radius: 10px; padding: 12px; display: grid; gap: 8px; }
.question-head { font-size: 13px; }
.question-text { margin: 0; white-space: pre-wrap; overflow-wrap: anywhere; }
.question-answer { display: flex; gap: 8px; align-items: flex-start; }
.question-answer textarea { flex: 1; min-width: 0; }
.ask-mark { color: var(--amber); font-weight: 700; }
svg .ask-mark { fill: var(--amber); font-size: 14px; }
.pill-ask { background: rgba(229, 165, 75, .14); color: var(--amber); }
.card.needs-you-card { border-color: var(--amber); }
```

(`.chat-question` / containerul din chat: `display: grid` numai daca il faci grid; atunci adauga si `[hidden]`.)

- [ ] **Step 7: Verificare** — bucla `node --check` pe `.mjs`, `go test ./internal/web/`, `go vet ./...`.

- [ ] **Step 8: Commit** — `git commit -m "ui: Needs you pe proiect, in chat, pe agent si pe card"`

---

### Task 5: Verificare in browser si CLAUDE.md

- [ ] **Step 1:** Verificarea completa: vet, race, test, cross-compile, `.mjs`.
- [ ] **Step 2:** Pregateste o instanta de test (port 7789, `HOME` temporar) cu un `claude` fals. Cand prompt-ul turei contine „Task T-”, falsul:
  - apeleaza `ask_user` prin MCP, cu `{question:"Allow Bash for the tests?", options:["Yes","No"]}`;
  - se termina fara `report_done`.
  
  La un run in care tura contine „Answer to your question”, falsul apeleaza `report_done`.
- [ ] **Step 3:** Parcurge fluxul prin CDP:
  1. Un task pentru un worker. Porneste echipa.
  2. Verifica:
     - banda „Needs you (1)” cu butoanele Yes/No;
     - „?” pe agent, in banda echipei si pe canvas;
     - task-ul In progress;
     - badge-ul „1 needs you” pe Projects.
  3. Scrie in campul de raspuns, asteapta cateva evenimente SSE si verifica ca textul a ramas.
  4. Click pe „Yes”. Banda dispare, iar agentul reporneste pe task si raporteaza. Task-ul trece in Review.
  5. Din chat-ul agentului, cu o a doua intrebare: raspunde din caseta din chat.
  6. Dismiss pe o a treia.
  
  Fa screenshot-uri si citeste-le.
- [ ] **Step 4:** In CLAUDE.md, la „Stare”, adauga o linie:

  ```
  Gata ("Needs you", spec in docs/superpowers/specs/2026-09-30-foreman-needs-you-design.md): tool-ul MCP ask_user; intrebarile deschise apar pe proiect, pe card, in chat si pe agent; raspunsul trezeste agentul si pune task-ul inapoi in coada.
  ```

  Apoi commit.
