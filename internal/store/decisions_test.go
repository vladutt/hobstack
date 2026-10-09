package store

import (
	"errors"
	"testing"
)

func TestDecisions(t *testing.T) {
	s := newStore(t)
	p, lead := newProject(t, s)
	task := Task{ProjectID: p.ID, Title: "Pick stack", AssigneeID: lead.ID, CreatorID: &lead.ID}
	if err := s.CreateTask(&task); err != nil {
		t.Fatal(err)
	}
	err := s.SetDecisions(task.ID, "Lead", []Decision{
		{Question: "Which DB?", Options: []string{"sqlite", "postgres"}},
		{Question: "Which features?", Options: []string{"a", "b", "c"}, Multi: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	list, err := s.ListDecisions(task.ID)
	if err != nil || len(list) != 2 || list[0].Position >= list[1].Position || len(list[1].Options) != 3 || !list[1].Multi || list[0].AskedBy != "Lead" {
		t.Fatalf("%+v %v", list, err)
	}
	// alegere invalida: nimic salvat (nici prima, valida)
	err = s.AnswerDecisions(task.ID, []DecisionAnswer{
		{ID: list[0].ID, Choices: []string{"sqlite"}},
		{ID: list[1].ID, Choices: []string{"zzz"}},
	})
	if !errors.Is(err, ErrBadAnswer) {
		t.Fatalf("vreau ErrBadAnswer, am %v", err)
	}
	after, _ := s.ListDecisions(task.ID)
	if after[0].AnsweredAt != nil || after[1].AnsweredAt != nil {
		t.Fatal("s-a salvat ceva desi eroare")
	}
	// doua alegeri la multi=false
	err = s.AnswerDecisions(task.ID, []DecisionAnswer{{ID: list[0].ID, Choices: []string{"sqlite", "postgres"}}})
	if !errors.Is(err, ErrBadAnswer) {
		t.Fatalf("vreau ErrBadAnswer, am %v", err)
	}
	// raspuns valid
	err = s.AnswerDecisions(task.ID, []DecisionAnswer{
		{ID: list[0].ID, Choices: []string{"sqlite"}, Other: "keep it simple"},
	})
	if err != nil {
		t.Fatal(err)
	}
	after, _ = s.ListDecisions(task.ID)
	if after[0].AnsweredAt == nil || len(after[0].Answer) != 1 || after[0].Answer[0] != "sqlite" || after[0].Other != "keep it simple" {
		t.Fatalf("%+v", after[0])
	}
	// a doua oara aceeasi intrebare: deja raspunsa
	if err := s.AnswerDecisions(task.ID, []DecisionAnswer{{ID: list[0].ID, Choices: []string{"sqlite"}}}); !errors.Is(err, ErrBadAnswer) {
		t.Fatalf("deja raspunsa: %v", err)
	}
	// acelasi raspuns din nou (reincercare): nimic de facut, fara eroare
	if err := s.AnswerDecisions(task.ID, []DecisionAnswer{{ID: list[0].ID, Choices: []string{"sqlite"}, Other: " keep it simple "}}); err != nil {
		t.Fatalf("raspuns identic: %v", err)
	}
	// alt raspuns la o intrebare raspunsa: tot eroare
	if err := s.AnswerDecisions(task.ID, []DecisionAnswer{{ID: list[0].ID, Choices: []string{"postgres"}, Other: "keep it simple"}}); !errors.Is(err, ErrBadAnswer) {
		t.Fatalf("raspuns diferit: %v", err)
	}
	// raspuns gol se salveaza
	if err := s.AnswerDecisions(task.ID, []DecisionAnswer{{ID: list[1].ID}}); err != nil {
		t.Fatal(err)
	}
	after, _ = s.ListDecisions(task.ID)
	if after[1].AnsweredAt == nil || after[1].Answer == nil || len(after[1].Answer) != 0 {
		t.Fatalf("%+v", after[1])
	}
	// SetDecisions din nou: cele cu raspuns raman, cele fara se inlocuiesc
	s.SetDecisions(task.ID, "Lead", []Decision{{Question: "Third?", Options: []string{"x"}}})
	s.SetDecisions(task.ID, "Lead", []Decision{{Question: "Fourth?", Options: []string{"y"}}})
	after, _ = s.ListDecisions(task.ID)
	if len(after) != 3 || after[2].Question != "Fourth?" || after[2].Position <= after[1].Position {
		t.Fatalf("%+v", after)
	}
	// alt agent nu sterge intrebarile lui Lead
	s.SetDecisions(task.ID, "Other", []Decision{{Question: "Fifth?", Options: []string{"z"}}})
	if after, _ = s.ListDecisions(task.ID); len(after) != 4 {
		t.Fatalf("%+v", after)
	}
	// intrebare straina de task
	if err := s.AnswerDecisions(task.ID+99, []DecisionAnswer{{ID: after[3].ID, Choices: []string{"z"}}}); !errors.Is(err, ErrBadAnswer) {
		t.Fatalf("alt task: %v", err)
	}
}

func TestTodoKind(t *testing.T) {
	s := newStore(t)
	p, lead := newProject(t, s)
	task := Task{ProjectID: p.ID, Title: "Steps", AssigneeID: lead.ID, CreatorID: &lead.ID}
	if err := s.CreateTask(&task); err != nil {
		t.Fatal(err)
	}
	s.AddTodos(task.ID, []string{"VAT"}, "value")
	s.AddTodos(task.ID, []string{"Run the migration"}, "step")
	list, _ := s.ListTodos(task.ID)
	if len(list) != 2 || list[0].Kind != "value" || list[1].Kind != "step" {
		t.Fatalf("%+v", list)
	}
}

func TestAnswerDecisionsIdenticalMulti(t *testing.T) {
	s := newStore(t)
	p, lead := newProject(t, s)
	task := Task{ProjectID: p.ID, Title: "x", AssigneeID: lead.ID, CreatorID: &lead.ID}
	s.CreateTask(&task)
	s.SetDecisions(task.ID, "Lead", []Decision{{Question: "Which?", Options: []string{"a", "b", "c"}, Multi: true}})
	list, _ := s.ListDecisions(task.ID)
	if err := s.AnswerDecisions(task.ID, []DecisionAnswer{{ID: list[0].ID, Choices: []string{"a", "c"}}}); err != nil {
		t.Fatal(err)
	}
	first, _ := s.ListDecisions(task.ID)
	// aceeasi multime, alta ordine: identic
	if err := s.AnswerDecisions(task.ID, []DecisionAnswer{{ID: list[0].ID, Choices: []string{"c", "a"}}}); err != nil {
		t.Fatalf("identic: %v", err)
	}
	again, _ := s.ListDecisions(task.ID)
	if !again[0].AnsweredAt.Equal(*first[0].AnsweredAt) {
		t.Fatal("raspunsul identic a rescris answered_at")
	}
	if err := s.AnswerDecisions(task.ID, []DecisionAnswer{{ID: list[0].ID, Choices: []string{"a"}}}); !errors.Is(err, ErrBadAnswer) {
		t.Fatalf("diferit: %v", err)
	}
}
