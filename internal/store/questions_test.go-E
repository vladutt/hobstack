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
	if ok, _ := s.HasOpenQuestionForTask(task.ID); !ok {
		t.Fatal("task-ul are o intrebare deschisa")
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

// regula: un task asteapta cat timp are ORICE intrebare deschisa pe el
func TestQuestionHelpers(t *testing.T) {
	s := newStore(t)
	p, lead := newProject(t, s)
	task := Task{ProjectID: p.ID, Title: "A", AssigneeID: lead.ID}
	s.CreateTask(&task)
	if ok, err := s.HasOpenQuestionForTask(task.ID); err != nil || ok {
		t.Fatalf("fara intrebari: %v %v", ok, err)
	}
	if ok, _ := s.HasQuestionForRun("run-1"); ok {
		t.Fatal("run-1 nu a intrebat inca")
	}
	q1 := Question{ProjectID: p.ID, AgentID: lead.ID, TaskID: &task.ID, RunID: "run-1", Text: "1?"}
	q2 := Question{ProjectID: p.ID, AgentID: lead.ID, TaskID: &task.ID, RunID: "run-1", Text: "2?"}
	s.AddQuestion(&q1)
	s.AddQuestion(&q2)
	if ok, _ := s.HasOpenQuestionForTask(task.ID); !ok {
		t.Fatal("task-ul are intrebari deschise")
	}
	s.CloseQuestion(q1.ID, QuestionAnswered, "x")
	if ok, _ := s.HasOpenQuestionForTask(task.ID); !ok {
		t.Fatal("a doua intrebare e inca deschisa")
	}
	s.CloseQuestion(q2.ID, QuestionDismissed, "")
	if ok, _ := s.HasOpenQuestionForTask(task.ID); ok {
		t.Fatal("toate intrebarile sunt inchise")
	}
	// HasQuestionForRun conteaza orice status
	if ok, _ := s.HasQuestionForRun("run-1"); !ok {
		t.Fatal("run-1 a intrebat")
	}
	if ok, _ := s.HasQuestionForRun("run-2"); ok {
		t.Fatal("run-2 nu a intrebat")
	}
	// run_id gol nu se potriveste niciodata
	s.AddQuestion(&Question{ProjectID: p.ID, AgentID: lead.ID, Text: "no run"})
	if ok, _ := s.HasQuestionForRun(""); ok {
		t.Fatal("run_id gol")
	}
}
