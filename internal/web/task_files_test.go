package web

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"hobstack/internal/config"
	"hobstack/internal/store"
)

// addTaskFile scrie o copie pe disc si randul ei, ca dupa report_done files.
func (e *env) addTaskFile(t *testing.T, task store.Task, path, mime, content string) store.TaskFile {
	t.Helper()
	dir := config.TaskFilesDir(e.dirs.Projects, task.ProjectID, task.ID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	stored := fmt.Sprintf("%d-%s", time.Now().UnixNano(), filepath.Base(path))
	if err := os.WriteFile(filepath.Join(dir, stored), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	f := store.TaskFile{TaskID: task.ID, ProjectID: task.ProjectID, AgentName: "Rook", Path: path,
		Name: filepath.Base(path), Stored: stored, Size: int64(len(content)), Mime: mime}
	if err := e.st.AddTaskFile(&f); err != nil {
		t.Fatal(err)
	}
	return f
}

func TestTaskFilesAPI(t *testing.T) {
	e := setup(t)
	p := e.createProject(t, t.TempDir())
	w := e.addWorker(t, p.ID, "Rook")
	parent := store.Task{ProjectID: p.ID, Title: "Docs", AssigneeID: w.ID}
	if err := e.st.CreateTask(&parent); err != nil {
		t.Fatal(err)
	}
	child := store.Task{ProjectID: p.ID, Title: "Logo", AssigneeID: w.ID, ParentTaskID: &parent.ID}
	if err := e.st.CreateTask(&child); err != nil {
		t.Fatal(err)
	}
	old := e.addTaskFile(t, parent, "docs/report.md", "text/markdown", "# v1")
	cur := e.addTaskFile(t, parent, "docs/report.md", "text/markdown", "# v2!")
	page := e.addTaskFile(t, parent, "site/index.html", "text/html; charset=utf-8", "<script>alert(1)</script>")
	pdf := e.addTaskFile(t, parent, "out/a.pdf", "application/pdf", "%PDF-1.4")
	e.addTaskFile(t, child, "logo.png", "image/png", "png")

	code, raw := e.do(t, "GET", "/api/tasks/"+itoa(parent.ID)+"/files", nil)
	if code != 200 {
		t.Fatalf("%d %s", code, raw)
	}
	var res struct {
		Files []struct {
			ID       int64  `json:"id"`
			Path     string `json:"path"`
			Versions []struct {
				ID int64 `json:"id"`
			} `json:"versions"`
		} `json:"files"`
		Subtasks []struct {
			Ref   string `json:"ref"`
			Files []struct {
				Path string `json:"path"`
			} `json:"files"`
		} `json:"subtasks"`
	}
	json.Unmarshal(raw, &res)
	if len(res.Files) != 3 || res.Files[2].ID != cur.ID || len(res.Files[2].Versions) != 1 || res.Files[2].Versions[0].ID != old.ID {
		t.Fatalf("grupare: %s", raw)
	}
	if len(res.Subtasks) != 1 || res.Subtasks[0].Ref != child.Ref() || res.Subtasks[0].Files[0].Path != "logo.png" {
		t.Fatalf("sub-task-uri: %s", raw)
	}

	get := func(path string, hdr map[string]string) *http.Response {
		req, _ := http.NewRequest("GET", e.ts.URL+path, nil)
		for k, v := range hdr {
			req.Header.Set(k, v)
		}
		r, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { r.Body.Close() })
		return r
	}
	r := get("/api/task-files/"+itoa(page.ID), nil)
	if r.Header.Get("Content-Security-Policy") != "sandbox" || !strings.HasPrefix(r.Header.Get("Content-Disposition"), "attachment") {
		t.Fatalf("html: %v", r.Header)
	}
	r = get("/api/task-files/"+itoa(pdf.ID), nil)
	if r.Header.Get("Content-Security-Policy") != "" || !strings.HasPrefix(r.Header.Get("Content-Disposition"), "inline") {
		t.Fatalf("pdf: %v", r.Header)
	}
	r = get("/api/task-files/"+itoa(pdf.ID)+"?download=1", nil)
	if !strings.HasPrefix(r.Header.Get("Content-Disposition"), "attachment") {
		t.Fatalf("download: %v", r.Header)
	}
	r = get("/api/task-files/"+itoa(cur.ID), map[string]string{"Range": "bytes=0-1"})
	if body, _ := io.ReadAll(r.Body); r.StatusCode != http.StatusPartialContent || string(body) != "# " {
		t.Fatalf("range: %d %q", r.StatusCode, body)
	}
	if code, _ := e.do(t, "GET", "/api/task-files/99999", nil); code != http.StatusNotFound {
		t.Fatalf("lipsa: %d", code)
	}

	// amprenta din GET /api/tasks/{id} si numarul de pe card
	code, raw = e.do(t, "GET", "/api/tasks/"+itoa(parent.ID), nil)
	if code != 200 || !strings.Contains(string(raw), `"filesCount":5`) {
		t.Fatalf("filesCount: %s", raw)
	}
	code, raw = e.do(t, "GET", "/api/projects/"+itoa(p.ID), nil)
	if code != 200 || !strings.Contains(string(raw), `"attachments":3`) {
		t.Fatalf("attachments pe card: %s", raw)
	}
}
