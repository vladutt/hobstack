package store

import "testing"

func TestTaskFiles(t *testing.T) {
	s := newStore(t)
	p, lead := newProject(t, s)
	parent := Task{ProjectID: p.ID, Title: "Docs", AssigneeID: lead.ID}
	if err := s.CreateTask(&parent); err != nil {
		t.Fatal(err)
	}
	child := Task{ProjectID: p.ID, Title: "Logo", AssigneeID: lead.ID, ParentTaskID: &parent.ID}
	if err := s.CreateTask(&child); err != nil {
		t.Fatal(err)
	}
	grand := Task{ProjectID: p.ID, Title: "Icon", AssigneeID: lead.ID, ParentTaskID: &child.ID}
	if err := s.CreateTask(&grand); err != nil {
		t.Fatal(err)
	}
	add := func(task Task, path string) TaskFile {
		f := TaskFile{TaskID: task.ID, ProjectID: p.ID, AgentID: &lead.ID, AgentName: lead.Name,
			Path: path, Name: path, Stored: "x-" + path, Size: 3, Mime: "text/plain"}
		if err := s.AddTaskFile(&f); err != nil {
			t.Fatal(err)
		}
		return f
	}
	v1 := add(parent, "report.md")
	v2 := add(parent, "report.md") // a doua predare: versiune noua
	add(child, "logo.png")
	add(grand, "icon.svg")

	got, err := s.GetTaskFile(v2.ID)
	if err != nil || got.Path != "report.md" || got.AgentName != lead.Name || got.CreatedAt.IsZero() {
		t.Fatalf("%+v %v", got, err)
	}
	if _, err := s.GetTaskFile(9999); err != ErrNotFound {
		t.Fatalf("lipsa: %v", err)
	}
	list, _ := s.ListTaskFiles(parent.ID)
	if len(list) != 2 || list[0].ID != v2.ID || list[1].ID != v1.ID {
		t.Fatalf("ordinea: %+v", list)
	}
	desc, _ := s.ListDescendantFiles(parent.ID)
	if len(desc) != 2 || desc[0].TaskID != child.ID || desc[1].TaskID != grand.ID {
		t.Fatalf("descendenti: %+v", desc)
	}
	if empty, _ := s.ListDescendantFiles(grand.ID); empty == nil || len(empty) != 0 {
		t.Fatalf("fara descendenti: %+v", empty)
	}
	counts, _ := s.CountTaskFiles(p.ID)
	if counts[parent.ID] != 1 || counts[child.ID] != 1 {
		t.Fatalf("cai distincte: %v", counts)
	}
	if n, _ := s.CountTaskFilesTree(parent.ID); n != 4 {
		t.Fatalf("arbore: %d", n)
	}
}
