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
