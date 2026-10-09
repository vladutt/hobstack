package store

import "testing"

func TestRewriteDataRoot(t *testing.T) {
	s := newStore(t)
	p, _ := newProject(t, s)
	w := newWorker(t, s, p.ID, "Rook")
	old, neu := "/home/u/.foreman", "/home/u/.hobstack"
	if _, err := s.db.Exec(`UPDATE agents SET instructions = ?, responsibilities = ? WHERE id = ?`,
		"Write to /home/u/.foreman/projects/5/files/x.md", "keep /home/u/.foremanx/y", w.ID); err != nil {
		t.Fatal(err)
	}
	task := Task{ProjectID: p.ID, Title: "t", AssigneeID: w.ID, Description: "see /home/u/.foreman/projects/5/files/a.md"}
	if err := s.CreateTask(&task); err != nil {
		t.Fatal(err)
	}
	if err := s.AddTaskChanges([]TaskChange{{TaskID: task.ID, RunID: "r", Kind: "create", Tool: "Write",
		Path: "/home/u/.foreman/projects/5/files/a.md"}}); err != nil {
		t.Fatal(err)
	}

	if err := s.RewriteDataRoot(old, neu); err != nil {
		t.Fatal(err)
	}
	var ins, resp, desc, path string
	s.db.QueryRow(`SELECT instructions, responsibilities FROM agents WHERE id = ?`, w.ID).Scan(&ins, &resp)
	s.db.QueryRow(`SELECT description FROM tasks WHERE id = ?`, task.ID).Scan(&desc)
	s.db.QueryRow(`SELECT path FROM task_changes WHERE task_id = ?`, task.ID).Scan(&path)
	if ins != "Write to /home/u/.hobstack/projects/5/files/x.md" {
		t.Errorf("instructions = %q", ins)
	}
	if resp != "keep /home/u/.foremanx/y" { // doar calea exacta + "/"
		t.Errorf("responsibilities = %q", resp)
	}
	if desc != "see /home/u/.hobstack/projects/5/files/a.md" || path != "/home/u/.hobstack/projects/5/files/a.md" {
		t.Errorf("description = %q, path = %q", desc, path)
	}
}
