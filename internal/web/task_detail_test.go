package web

import (
	"encoding/json"
	"strings"
	"testing"

	"foreman/internal/store"
)

type taskDetail struct {
	Task     store.Task        `json:"task"`
	Events   []store.TaskEvent `json:"events"`
	Assignee string            `json:"assignee"`
	Stage    string            `json:"stage"`
}

func TestGetTaskDetail(t *testing.T) {
	e := setup(t)
	p := e.createProject(t, t.TempDir())
	w := e.addWorker(t, p.ID, "Rook")
	task := store.Task{ProjectID: p.ID, Title: "Login", AssigneeID: w.ID}
	if err := e.st.CreateTask(&task); err != nil {
		t.Fatal(err)
	}
	path := "/api/tasks/" + itoa(task.ID)

	// fara evenimente salvate: unul sintetic "created"
	code, raw := e.do(t, "GET", path, nil)
	var d taskDetail
	json.Unmarshal(raw, &d)
	if code != 200 || d.Assignee != "Rook" || d.Stage != "" || len(d.Events) != 1 ||
		d.Events[0].Kind != "created" || d.Events[0].Text != "Created" || !d.Events[0].At.Equal(task.CreatedAt) {
		t.Fatalf("sintetic: %d %s", code, raw)
	}
	if evs, _ := e.st.ListTaskEvents(task.ID); len(evs) != 0 {
		t.Fatalf("evenimentul sintetic nu se salveaza: %+v", evs)
	}

	// evenimente reale
	e.st.AddTaskEvent(&store.TaskEvent{TaskID: task.ID, Kind: "created", Actor: "You", Text: "Created by you"})
	e.st.AddTaskEvent(&store.TaskEvent{TaskID: task.ID, Kind: "assigned", Actor: "You", Text: "Assigned to Rook"})
	code, raw = e.do(t, "GET", path, nil)
	d = taskDetail{}
	json.Unmarshal(raw, &d)
	if code != 200 || len(d.Events) != 2 || d.Events[1].Text != "Assigned to Rook" || d.Task.ID != task.ID {
		t.Fatalf("reale: %d %s", code, raw)
	}
	if !strings.Contains(string(raw), `"taskId"`) || !strings.Contains(string(raw), `"actor"`) {
		t.Fatalf("json: %s", raw)
	}

	if code, _ := e.do(t, "GET", "/api/tasks/99999", nil); code != 404 {
		t.Fatalf("404: %d", code)
	}
	if code, _ := e.do(t, "GET", "/api/tasks/abc", nil); code != 400 {
		t.Fatalf("400: %d", code)
	}
}

func TestGetTaskDetailStageName(t *testing.T) {
	e := setup(t)
	p := e.createProject(t, t.TempDir())
	root := e.root(t, p.ID)
	path := "/api/projects/" + itoa(p.ID)
	e.do(t, "PUT", path+"/stages", map[string]any{"stages": []map[string]any{{"name": "Dev", "kind": "work", "agentIds": []int64{root.ID}}}})
	e.do(t, "POST", path+"/tasks", map[string]any{"title": "Login"})
	tasks, _ := e.st.ListTasks(p.ID)
	_, raw := e.do(t, "GET", "/api/tasks/"+itoa(tasks[0].ID), nil)
	var d taskDetail
	json.Unmarshal(raw, &d)
	if d.Stage != "Dev" {
		t.Fatalf("stage: %s", raw)
	}
}
