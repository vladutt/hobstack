package web

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"foreman/internal/store"
)

// codeSetup: un proiect cu un task al lui Beacon si doua fisiere modificate in folderul proiectului.
func codeSetup(t *testing.T) (*env, store.Project, store.Agent, store.Task, string) {
	t.Helper()
	e := setup(t)
	dir := t.TempDir()
	p := e.createProject(t, dir)
	w := e.addWorker(t, p.ID, "Beacon")
	task := store.Task{ProjectID: p.ID, Title: "A", AssigneeID: w.ID}
	if err := e.st.CreateTask(&task); err != nil {
		t.Fatal(err)
	}
	os.MkdirAll(filepath.Join(dir, "app"), 0o755)
	os.WriteFile(filepath.Join(dir, "app", "x.php"), []byte("<?php\n$a = 1;\n$b = 2;\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "secret.txt"), []byte("nope"), 0o644)
	e.st.AddTaskChanges([]store.TaskChange{
		{TaskID: task.ID, RunID: "r1", AgentID: &w.ID, AgentName: "Beacon", Kind: "create", Tool: "Write", Path: "app/x.php",
			Patch: []store.Hunk{{OldStart: 0, OldLines: 0, NewStart: 1, NewLines: 2, Lines: []string{"+<?php", "+$a = 1;"}}}},
		{TaskID: task.ID, RunID: "r2", AgentID: &w.ID, AgentName: "Beacon", Kind: "edit", Tool: "Edit", Path: "app/x.php",
			Patch: []store.Hunk{{OldStart: 2, OldLines: 1, NewStart: 2, NewLines: 2, Lines: []string{" $a = 1;", "+$b = 2;"}}}},
	})
	return e, p, w, task, dir
}

func TestModifiedFiles(t *testing.T) {
	e, p, w, task, _ := codeSetup(t)
	sub := store.Task{ProjectID: p.ID, Title: "B", AssigneeID: w.ID, ParentTaskID: &task.ID}
	e.st.CreateTask(&sub)
	e.st.AddTaskChanges([]store.TaskChange{{TaskID: sub.ID, RunID: "r3", AgentName: "Beacon", Kind: "edit", Tool: "Edit", Path: "README.md",
		Patch: []store.Hunk{{OldStart: 1, OldLines: 1, NewStart: 1, NewLines: 1, Lines: []string{"-a", "+b"}}}}})
	code, raw := e.do(t, "GET", "/api/tasks/"+itoa(task.ID)+"/modified", nil)
	var d struct{ Files []modifiedView }
	json.Unmarshal(raw, &d)
	if code != 200 || len(d.Files) != 2 {
		t.Fatalf("%d %s", code, raw)
	}
	f := d.Files[0]
	if f.Path != "app/x.php" || f.Added != 3 || f.Removed != 0 || !f.Created || f.Edits != 2 || f.Ref != "T-1" || f.Agent != "Beacon" {
		t.Fatalf("%+v", f)
	}
	if d.Files[1].Ref != "T-2" || d.Files[1].Path != "README.md" || d.Files[1].Added != 1 || d.Files[1].Removed != 1 {
		t.Fatalf("%+v", d.Files[1])
	}
}

func TestFileView(t *testing.T) {
	e, _, _, task, _ := codeSetup(t)
	code, raw := e.do(t, "GET", "/api/tasks/"+itoa(task.ID)+"/file?path=app/x.php", nil)
	var v fileView
	json.Unmarshal(raw, &v)
	if code != 200 || !v.Exists || !strings.Contains(v.Content, "$b = 2;") || !v.Exact || len(v.Markers.Added) != 3 || len(v.Runs) != 2 {
		t.Fatalf("%d %s", code, raw)
	}
	// cai care nu sunt in task_changes, iesiri din folder, sub-task strain: 404
	for _, q := range []string{"path=secret.txt", "path=../x.php", "path=app/../secret.txt", "path=app/x.php&task=99999"} {
		if code, raw := e.do(t, "GET", "/api/tasks/"+itoa(task.ID)+"/file?"+q, nil); code != http.StatusNotFound {
			t.Fatalf("%s: %d %s", q, code, raw)
		}
	}
}

func TestFileViewLimits(t *testing.T) {
	e, _, w, task, dir := codeSetup(t)
	os.WriteFile(filepath.Join(dir, "big.log"), []byte(strings.Repeat("x", 1<<20+1)), 0o644)
	os.WriteFile(filepath.Join(dir, "img.bin"), []byte("PNG\x00\x01"), 0o644)
	for _, p := range []string{"big.log", "img.bin", "gone.txt"} {
		e.st.AddTaskChanges([]store.TaskChange{{TaskID: task.ID, RunID: "r9", AgentID: &w.ID, AgentName: "Beacon", Kind: "edit", Tool: "Edit", Path: p,
			Patch: []store.Hunk{{OldStart: 1, OldLines: 1, NewStart: 1, NewLines: 1, Lines: []string{"-a", "+b"}}}}})
	}
	get := func(p string) fileView {
		_, raw := e.do(t, "GET", "/api/tasks/"+itoa(task.ID)+"/file?path="+p, nil)
		var v fileView
		json.Unmarshal(raw, &v)
		return v
	}
	if v := get("big.log"); !v.TooLarge || v.Content != "" {
		t.Fatalf("big: %+v", v.TooLarge)
	}
	if v := get("img.bin"); !v.Binary || v.Content != "" {
		t.Fatal("binar")
	}
	if v := get("gone.txt"); v.Exists || len(v.Runs) != 1 {
		t.Fatalf("lipsa: %+v", v)
	}
}

// un symlink din proiect care iese din folder nu se citeste (os.OpenRoot)
func TestFileViewSymlinkOutside(t *testing.T) {
	e, _, w, task, dir := codeSetup(t)
	outside := filepath.Join(t.TempDir(), "secret.txt")
	os.WriteFile(outside, []byte("top secret"), 0o644)
	if err := os.Symlink(outside, filepath.Join(dir, "link.txt")); err != nil {
		t.Skip("symlink:", err)
	}
	e.st.AddTaskChanges([]store.TaskChange{{TaskID: task.ID, RunID: "r5", AgentID: &w.ID, AgentName: "Beacon", Kind: "edit", Tool: "Edit", Path: "link.txt",
		Patch: []store.Hunk{{OldStart: 1, OldLines: 1, NewStart: 1, NewLines: 1, Lines: []string{"-a", "+b"}}}}})
	code, raw := e.do(t, "GET", "/api/tasks/"+itoa(task.ID)+"/file?path=link.txt", nil)
	if code != http.StatusNotFound || strings.Contains(string(raw), "top secret") {
		t.Fatalf("%d %s", code, raw)
	}
}

// proiect cu doua foldere: caile din task_changes au in fata numele folderului agentului
func TestFileViewMultiFolder(t *testing.T) {
	e := setup(t)
	back, front := t.TempDir(), t.TempDir()
	p := store.Project{Name: "Multi", Folders: []store.Folder{{Name: "backend", Path: back}, {Name: "frontend", Path: front}}}
	if _, err := e.st.CreateProject(&p); err != nil {
		t.Fatal(err)
	}
	w := e.addWorker(t, p.ID, "Beacon") // folderul implicit: backend
	task := store.Task{ProjectID: p.ID, Title: "A", AssigneeID: w.ID}
	e.st.CreateTask(&task)
	os.MkdirAll(filepath.Join(back, "app"), 0o755)
	os.WriteFile(filepath.Join(back, "app", "x.php"), []byte("<?php\necho 1;\n"), 0o644)
	e.st.AddTaskChanges([]store.TaskChange{{TaskID: task.ID, RunID: "r1", AgentID: &w.ID, AgentName: "Beacon", Kind: "edit", Tool: "Edit", Path: "backend/app/x.php",
		Patch: []store.Hunk{{OldStart: 1, OldLines: 2, NewStart: 1, NewLines: 2, Lines: []string{" <?php", "-echo 0;", "+echo 1;"}}}}})
	code, raw := e.do(t, "GET", "/api/tasks/"+itoa(task.ID)+"/file?path=backend/app/x.php", nil)
	var v fileView
	json.Unmarshal(raw, &v)
	if code != 200 || !v.Exists || !strings.Contains(v.Content, "echo 1;") {
		t.Fatalf("%d %s", code, raw)
	}
}
