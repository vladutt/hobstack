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
