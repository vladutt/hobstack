package store

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"hobstack/internal/schedule"
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

func TestRecurringHistory(t *testing.T) {
	s, p, lead := recurringFixture(t)
	mk := func(title string) RecurringTask {
		r := RecurringTask{ProjectID: p.ID, Title: title, AssigneeID: &lead.ID,
			Schedule: schedule.Spec{Kind: schedule.KindDaily, At: "09:00"}, Enabled: true, NextRunAt: time.Now()}
		if err := s.CreateRecurring(&r); err != nil {
			t.Fatal(err)
		}
		return r
	}
	scout, other := mk("Scout"), mk("Other")
	run := func(r RecurringTask, parent *int64) Task {
		task := Task{ProjectID: p.ID, Title: r.Title, AssigneeID: lead.ID, ByUser: true, ParentTaskID: parent}
		if parent == nil {
			task.RecurringID = &r.ID
		}
		if err := s.CreateTask(&task); err != nil {
			t.Fatal(err)
		}
		return task
	}
	file := func(task Task, path string) TaskFile {
		f := TaskFile{TaskID: task.ID, ProjectID: p.ID, AgentID: &lead.ID, AgentName: lead.Name,
			Path: path, Name: path, Stored: "x-" + path, Size: 3, Mime: "text/plain"}
		if err := s.AddTaskFile(&f); err != nil {
			t.Fatal(err)
		}
		return f
	}
	r1, r2, r3 := run(scout, nil), run(scout, nil), run(scout, nil)
	sub := run(scout, &r3.ID) // sub-task al rularii 3 (fara recurring_id)
	o1 := run(other, nil)
	if _, err := s.db.Exec(`UPDATE tasks SET status = 'done', result = 'found 2' WHERE id IN (?, ?)`, r1.ID, r3.ID); err != nil {
		t.Fatal(err)
	}
	sub2 := run(scout, &r2.ID) // sub-task al rularii arhivate: se arhiveaza odata cu ea (ca team.ArchiveTask)
	f1 := file(r1, "leads.csv")
	f2 := file(r2, "leads.csv")  // rularea arhivata: fisierul ramane
	f5 := file(sub2, "deep.csv") // si fisierul sub-task-ului ei
	f3 := file(sub, "notes.md")
	f4 := file(r3, "leads.csv")
	file(o1, "other.csv")
	if err := s.ArchiveTasks([]int64{r2.ID, sub2.ID}, r2.ID); err != nil {
		t.Fatal(err)
	}

	// totaluri: 3 rulari (sub-task-ul nu e rulare), 2 done, 0 open, 1 arhivata
	c, err := s.RecurringCounts(scout.ID)
	if err != nil || c != (RecurringCounts{Total: 3, Done: 2, Open: 0, Archived: 1}) {
		t.Fatalf("counts: %+v %v", c, err)
	}

	// istoric: cea mai noua prima, arhivata inclusa, alta sarcina exclusa
	runs, more, err := s.RecurringHistory(scout.ID, 0, 20)
	if err != nil || more || len(runs) != 3 || runs[0].ID != r3.ID || runs[1].ID != r2.ID || runs[2].ID != r1.ID {
		t.Fatalf("history: %+v %v %v", runs, more, err)
	}
	if runs[0].Files != 2 || runs[1].Files != 2 || runs[0].Result != "found 2" || runs[1].Status != "archived" {
		t.Fatalf("campuri: %+v", runs)
	}

	// paginare: limit 2 -> more; before = ultima primita -> restul
	page, more, err := s.RecurringHistory(scout.ID, 0, 2)
	if err != nil || !more || len(page) != 2 {
		t.Fatalf("pagina 1: %+v %v %v", page, more, err)
	}
	r4 := run(scout, nil) // o rulare noua intre pagini nu strica pagina 2
	page, more, err = s.RecurringHistory(scout.ID, page[1].ID, 2)
	if err != nil || more || len(page) != 1 || page[0].ID != r1.ID {
		t.Fatalf("pagina 2: %+v %v %v", page, more, err)
	}
	if runs, _, _ := s.RecurringHistory(scout.ID, 0, 20); runs[0].ID != r4.ID {
		t.Fatalf("rularea noua nu e prima: %+v", runs)
	}

	// fisiere: ale rularilor (si arhivate) si ale sub-task-urilor, cel mai nou primul
	files, err := s.RecurringFiles(scout.ID)
	if err != nil || len(files) != 5 {
		t.Fatalf("files: %+v %v", files, err)
	}
	if files[0].ID != f4.ID || files[1].ID != f3.ID || files[2].ID != f5.ID || files[3].ID != f2.ID || files[4].ID != f1.ID {
		t.Fatalf("ordine: %+v", files)
	}

	// T-N-urile tuturor task-urilor din arborele rularilor (pentru versiunile din Files), o singura interogare
	refs, err := s.RecurringRefs(scout.ID)
	if err != nil || refs[r2.ID] != r2.Ref() || refs[sub2.ID] != sub2.Ref() || refs[sub.ID] != sub.Ref() || refs[o1.ID] != "" {
		t.Fatalf("refs: %v %v", refs, err)
	}

	// sarcina fara rulari
	empty := mk("Empty")
	if runs, more, err := s.RecurringHistory(empty.ID, 0, 20); err != nil || more || len(runs) != 0 || runs == nil {
		t.Fatalf("goala: %+v %v %v", runs, more, err)
	}
	if files, err := s.RecurringFiles(empty.ID); err != nil || len(files) != 0 {
		t.Fatalf("goala files: %+v %v", files, err)
	}
}

func TestRecurringCreatedBy(t *testing.T) {
	s, p, lead := recurringFixture(t)
	next := time.Date(2026, 10, 5, 9, 0, 0, 0, time.UTC)
	daily := schedule.Spec{Kind: schedule.KindDaily, At: "09:00"}
	mine := RecurringTask{ProjectID: p.ID, Title: "Mine", AssigneeID: &lead.ID, Schedule: daily, Enabled: true, NextRunAt: next}
	byLead := RecurringTask{ProjectID: p.ID, Title: "Lead's", AssigneeID: &lead.ID, Schedule: daily, Enabled: true, NextRunAt: next, CreatedBy: &lead.ID}
	for _, r := range []*RecurringTask{&mine, &byLead} {
		if err := s.CreateRecurring(r); err != nil {
			t.Fatal(err)
		}
	}
	got, _ := s.GetRecurring(byLead.ID)
	if got.CreatedBy == nil || *got.CreatedBy != lead.ID {
		t.Fatalf("created_by: %+v", got.CreatedBy)
	}
	// SaveRecurring nu atinge created_by
	got.Title = "Renamed"
	if err := s.SaveRecurring(&got); err != nil {
		t.Fatal(err)
	}
	list, _ := s.ListRecurring(p.ID)
	if list[0].CreatedBy != nil || list[1].CreatedBy == nil || list[1].Title != "Renamed" {
		t.Fatalf("%+v", list)
	}
}
