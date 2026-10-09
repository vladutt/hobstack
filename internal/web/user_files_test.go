package web

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"foreman/internal/config"
	"foreman/internal/store"
)

type userFilesRes struct {
	User []struct {
		ID        int64  `json:"id"`
		Name      string `json:"name"`
		AgentName string `json:"agentName"`
		Size      int64  `json:"size"`
	} `json:"user"`
	Files []struct {
		Name string `json:"name"`
	} `json:"files"`
}

func getUserFiles(t *testing.T, e *env, taskID int64) userFilesRes {
	t.Helper()
	code, raw := e.do(t, "GET", "/api/tasks/"+itoa(taskID)+"/files", nil)
	if code != 200 {
		t.Fatalf("%d %s", code, raw)
	}
	var res userFilesRes
	json.Unmarshal(raw, &res)
	return res
}

func TestUploadTaskFiles(t *testing.T) {
	e := setup(t)
	p := e.createProject(t, t.TempDir())
	w := e.addWorker(t, p.ID, "Rook")
	task := store.Task{ProjectID: p.ID, Title: "Logo", AssigneeID: w.ID}
	if err := e.st.CreateTask(&task); err != nil {
		t.Fatal(err)
	}
	e.addTaskFile(t, task, "logo.png", "image/png", "png")
	path := "/api/tasks/" + itoa(task.ID) + "/files"

	// fara X-Foreman: refuzat de garda (un formular strain nu poate urca)
	if code, _ := upload(t, e, path, nil, map[string][]byte{"brief.txt": []byte("hi")}, false); code != http.StatusUnsupportedMediaType {
		t.Fatalf("fara header: %d", code)
	}
	code, raw := upload(t, e, path, nil, map[string][]byte{"brief.txt": []byte("hello")}, true)
	if code != http.StatusCreated {
		t.Fatalf("%d %s", code, raw)
	}
	var saved []store.TaskFile
	json.Unmarshal(raw, &saved)
	if len(saved) != 1 || saved[0].Name != "brief.txt" || saved[0].AgentName != "You" || saved[0].Size != 5 {
		t.Fatalf("%s", raw)
	}
	stored, _ := e.st.GetTaskFile(saved[0].ID)
	if _, err := os.Stat(filepath.Join(config.TaskFilesDir(e.dirs.Projects, p.ID, task.ID), stored.Stored)); err != nil {
		t.Fatalf("copia lipseste: %v", err)
	}
	if !stored.FromUser() || stored.Mime != "text/plain; charset=utf-8" {
		t.Fatalf("%+v", stored)
	}
	// staging-ul nu ramane pe disc
	entries, _ := os.ReadDir(filepath.Join(e.dirs.Projects, itoa(p.ID), "task-files"))
	if len(entries) != 1 || entries[0].Name() != itoa(task.ID) {
		t.Fatalf("task-files: %v", entries)
	}

	res := getUserFiles(t, e, task.ID)
	if len(res.User) != 1 || res.User[0].AgentName != "You" || res.User[0].Name != "brief.txt" {
		t.Fatalf("sectiunea utilizatorului: %+v", res)
	}
	if len(res.Files) != 1 || res.Files[0].Name != "logo.png" {
		t.Fatalf("livrabilele agentilor raman separate: %+v", res)
	}
	// fisierul se serveste ca orice fisier de task
	if code, body := e.do(t, "GET", "/api/task-files/"+itoa(saved[0].ID), nil); code != 200 || string(body) != "hello" {
		t.Fatalf("serve: %d %q", code, body)
	}

	// fara fisiere: 400
	if code, _ := upload(t, e, path, nil, nil, true); code != http.StatusBadRequest {
		t.Fatalf("gol: %d", code)
	}
	// task necunoscut: 404
	if code, _ := upload(t, e, "/api/tasks/9999/files", nil, map[string][]byte{"a.txt": []byte("a")}, true); code != http.StatusNotFound {
		t.Fatalf("lipsa: %d", code)
	}
	// task sters: 409
	if err := e.st.ArchiveTasks([]int64{task.ID}, task.ID); err != nil {
		t.Fatal(err)
	}
	if code, raw := upload(t, e, path, nil, map[string][]byte{"a.txt": []byte("a")}, true); code != http.StatusConflict {
		t.Fatalf("sters: %d %s", code, raw)
	}
}

func TestUploadTaskFilesTooLarge(t *testing.T) {
	e := setup(t)
	p := e.createProject(t, t.TempDir())
	w := e.addWorker(t, p.ID, "Rook")
	task := store.Task{ProjectID: p.ID, Title: "Logo", AssigneeID: w.ID}
	if err := e.st.CreateTask(&task); err != nil {
		t.Fatal(err)
	}
	big := bytes.Repeat([]byte("x"), maxFileSize+1)
	code, raw := upload(t, e, "/api/tasks/"+itoa(task.ID)+"/files", nil, map[string][]byte{"big.bin": big}, true)
	if code != http.StatusBadRequest {
		t.Fatalf("%d %s", code, raw)
	}
	if list, _ := e.st.ListTaskFiles(task.ID); len(list) != 0 {
		t.Fatalf("%+v", list)
	}
}

func TestCreateTaskWithFiles(t *testing.T) {
	e := setup(t)
	p := e.createProject(t, t.TempDir())
	w := e.addWorker(t, p.ID, "Rook")
	path := "/api/projects/" + itoa(p.ID) + "/tasks"

	code, raw := upload(t, e, path, map[string]string{"title": "Logo", "description": "Use the brief", "assigneeId": itoa(w.ID)},
		map[string][]byte{"brief.txt": []byte("brief"), "colors.txt": []byte("red")}, true)
	if code != http.StatusCreated {
		t.Fatalf("%d %s", code, raw)
	}
	var task store.Task
	json.Unmarshal(raw, &task)
	if task.Title != "Logo" || task.Description != "Use the brief" || task.AssigneeID != w.ID || !task.ByUser {
		t.Fatalf("%s", raw)
	}
	if res := getUserFiles(t, e, task.ID); len(res.User) != 2 {
		t.Fatalf("%+v", res)
	}

	// multipart fara titlu: 400, nimic creat, staging sters
	code, _ = upload(t, e, path, map[string]string{"assigneeId": itoa(w.ID)}, map[string][]byte{"a.txt": []byte("a")}, true)
	if code != http.StatusBadRequest {
		t.Fatalf("fara titlu: %d", code)
	}
	// asignat lipsa (fara etape): 400, staging sters
	code, _ = upload(t, e, path, map[string]string{"title": "X"}, map[string][]byte{"a.txt": []byte("a")}, true)
	if code != http.StatusBadRequest {
		t.Fatalf("fara asignat: %d", code)
	}
	if tasks, _ := e.st.ListTasks(p.ID); len(tasks) != 1 {
		t.Fatalf("%+v", tasks)
	}
	entries, _ := os.ReadDir(filepath.Join(e.dirs.Projects, itoa(p.ID), "task-files"))
	if len(entries) != 1 {
		t.Fatalf("staging ramas: %v", entries)
	}

	// JSON-ul merge ca inainte
	code, raw = e.do(t, "POST", path, map[string]any{"title": "Plain", "assigneeId": w.ID})
	if code != http.StatusCreated {
		t.Fatalf("json: %d %s", code, raw)
	}
	json.Unmarshal(raw, &task)
	if res := getUserFiles(t, e, task.ID); len(res.User) != 0 {
		t.Fatalf("%+v", res)
	}
}
