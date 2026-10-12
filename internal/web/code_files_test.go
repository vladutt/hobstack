package web

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"hobstack/internal/store"
)

func TestCodeFilesAPI(t *testing.T) {
	e := setup(t)
	api, web := t.TempDir(), t.TempDir()
	for _, f := range []string{filepath.Join(api, "app", "UserController.php"), filepath.Join(api, "node_modules", "x", "user.js"),
		filepath.Join(web, "src", "UserCard.vue")} {
		if err := os.MkdirAll(filepath.Dir(f), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	code, raw := e.do(t, "POST", "/api/projects", map[string]any{"name": "Shop", "goal": "Sell",
		"folders": []map[string]any{{"name": "api", "path": api}, {"name": "web", "path": web}}})
	if code != http.StatusCreated {
		t.Fatalf("%d %s", code, raw)
	}
	var p store.Project
	json.Unmarshal(raw, &p)
	agents, _ := e.st.ListAgents(p.ID)
	lead := agents[0] // lucreaza in primul folder (api)

	code, raw = e.do(t, "GET", "/api/projects/"+itoa(p.ID)+"/code-files?q=user&agent="+itoa(lead.ID), nil)
	if code != 200 {
		t.Fatalf("%d %s", code, raw)
	}
	var res struct {
		Files []struct {
			Path   string `json:"path"`
			Name   string `json:"name"`
			Folder string `json:"folder"`
		} `json:"files"`
	}
	json.Unmarshal(raw, &res)
	got := map[string]string{}
	for _, f := range res.Files {
		got[f.Path] = f.Folder
	}
	// folderul agentului: cale relativa; celalalt folder: cale absoluta; node_modules sarit
	if len(res.Files) != 2 || got["app/UserController.php"] != "api" || got[filepath.Join(web, "src", "UserCard.vue")] != "web" {
		t.Fatalf("%s", raw)
	}
	if code, _ := e.do(t, "GET", "/api/projects/999999/code-files?q=x", nil); code != http.StatusNotFound {
		t.Fatalf("proiect lipsa: %d", code)
	}
}

// ?task=: fisierele modificate de task (si de sub-task-uri) vin primele, marcate changed, fara dubluri
func TestCodeFilesTaskFirst(t *testing.T) {
	e, p, w, task, dir := codeSetup(t)
	os.WriteFile(filepath.Join(dir, "app", "y.php"), []byte("x"), 0o644)
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("x"), 0o644)
	sub := store.Task{ProjectID: p.ID, Title: "B", AssigneeID: w.ID, ParentTaskID: &task.ID}
	e.st.CreateTask(&sub)
	e.st.AddTaskChanges([]store.TaskChange{{TaskID: sub.ID, RunID: "r3", AgentID: &w.ID, AgentName: "Beacon", Kind: "edit", Tool: "Edit", Path: "README.md"}})

	get := func(q string) []codeFile {
		t.Helper()
		code, raw := e.do(t, "GET", "/api/projects/"+itoa(p.ID)+"/code-files?"+q, nil)
		if code != 200 {
			t.Fatalf("%d %s", code, raw)
		}
		var res struct{ Files []codeFile }
		json.Unmarshal(raw, &res)
		return res.Files
	}
	files := get("task=" + itoa(task.ID) + "&agent=" + itoa(w.ID))
	if len(files) < 4 || !files[0].Changed || !files[1].Changed || files[2].Changed {
		t.Fatalf("%+v", files)
	}
	got := map[string]int{}
	for _, f := range files {
		got[f.Path]++
	}
	if got["app/x.php"] != 1 || got["README.md"] != 1 || got["app/y.php"] != 1 {
		t.Fatalf("dubluri sau lipsa: %+v", files)
	}
	// cautarea filtreaza si fisierele modificate
	files = get("q=x.php&task=" + itoa(task.ID) + "&agent=" + itoa(w.ID))
	if len(files) != 1 || files[0].Path != "app/x.php" || !files[0].Changed {
		t.Fatalf("%+v", files)
	}
	// task din alt proiect: 404
	other := e.createProject(t, t.TempDir())
	if code, _ := e.do(t, "GET", "/api/projects/"+itoa(other.ID)+"/code-files?task="+itoa(task.ID), nil); code != http.StatusNotFound {
		t.Fatalf("alt proiect: %d", code)
	}
}
