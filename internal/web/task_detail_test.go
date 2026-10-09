package web

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"hobstack/internal/store"
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

type convDetail struct {
	Conversation   []map[string]any `json:"conversation"`
	Working        string           `json:"working"`
	WorkingAgentID int64            `json:"workingAgentId"`
}

func TestGetTaskConversation(t *testing.T) {
	e := setup(t)
	p := e.createProject(t, t.TempDir())
	w := e.addWorker(t, p.ID, "Rook")
	task := store.Task{ProjectID: p.ID, Title: "Login", AssigneeID: w.ID}
	if err := e.st.CreateTask(&task); err != nil {
		t.Fatal(err)
	}
	path := "/api/tasks/" + itoa(task.ID)

	// task fara nimic: lista goala non-nil, nimeni nu lucreaza
	code, raw := e.do(t, "GET", path, nil)
	var d convDetail
	json.Unmarshal(raw, &d)
	if code != 200 || d.Conversation == nil || len(d.Conversation) != 0 || d.Working != "" || d.WorkingAgentID != 0 ||
		!strings.Contains(string(raw), `"conversation":[]`) {
		t.Fatalf("gol: %d %s", code, raw)
	}

	// doar run-uri (task vechi)
	if err := e.st.CreateRun(&store.Run{ID: "r1", AgentID: w.ID, TaskID: task.ID}); err != nil {
		t.Fatal(err)
	}
	e.st.AddMessage(&store.Message{ProjectID: p.ID, AgentID: w.ID, Kind: store.KindMsg, Author: "Rook", Text: "done it", RunID: "r1"})
	_, raw = e.do(t, "GET", path, nil)
	d = convDetail{}
	json.Unmarshal(raw, &d)
	if len(d.Conversation) != 1 || d.Conversation[0]["type"] != "run" || d.Conversation[0]["runId"] != "r1" {
		t.Fatalf("doar run: %s", raw)
	}

	// evenimente cu body inaintea mesajului din run; cele fara body lipsesc
	base := time.Now().UTC().Add(-time.Hour)
	e.st.AddTaskEvent(&store.TaskEvent{TaskID: task.ID, At: base, Kind: "created", Actor: "You", Text: "Created"})
	e.st.AddTaskEvent(&store.TaskEvent{TaskID: task.ID, At: base, Kind: "assigned", Actor: "Lead", To: "Rook", Text: "Assigned", Body: "please do it"})
	e.st.AddTaskEvent(&store.TaskEvent{TaskID: task.ID, At: base.Add(time.Minute), Kind: "reported", Actor: "Rook", Text: "Reported", Body: "all done"})
	_, raw = e.do(t, "GET", path, nil)
	d = convDetail{}
	json.Unmarshal(raw, &d)
	if len(d.Conversation) != 3 {
		t.Fatalf("3 elemente: %s", raw)
	}
	c := d.Conversation
	if c[0]["type"] != "event" || c[0]["text"] != "please do it" || c[0]["from"] != "Lead" || c[0]["to"] != "Rook" || c[0]["kind"] != "assigned" ||
		c[1]["text"] != "all done" || c[2]["type"] != "run" || c[2]["text"] != "done it" {
		t.Fatalf("ordine: %s", raw)
	}
}

func TestConversationDedupAndSlimEvents(t *testing.T) {
	e := setup(t)
	p := e.createProject(t, t.TempDir())
	w := e.addWorker(t, p.ID, "Rook")
	task := store.Task{ProjectID: p.ID, Title: "Login", AssigneeID: w.ID}
	if err := e.st.CreateTask(&task); err != nil {
		t.Fatal(err)
	}
	e.st.CreateRun(&store.Run{ID: "r1", AgentID: w.ID, TaskID: task.ID})
	e.st.AddMessage(&store.Message{ProjectID: p.ID, AgentID: w.ID, Kind: store.KindMsg, Author: "Rook", Text: "all done", RunID: "r1"})
	e.st.AddMessage(&store.Message{ProjectID: p.ID, AgentID: w.ID, Kind: store.KindMsg, Author: "Rook", Text: "other note", RunID: "r1"})
	e.st.AddTaskEvent(&store.TaskEvent{TaskID: task.ID, At: time.Now().UTC().Add(-time.Hour), Kind: "reported", Actor: "Rook", To: "Lead", Text: "Reported", Body: "all done"})

	_, raw := e.do(t, "GET", "/api/tasks/"+itoa(task.ID), nil)
	var d struct {
		Conversation []map[string]any `json:"conversation"`
		Events       []map[string]any `json:"events"`
	}
	json.Unmarshal(raw, &d)
	if len(d.Conversation) != 2 || d.Conversation[0]["type"] != "event" || d.Conversation[0]["text"] != "all done" ||
		d.Conversation[1]["type"] != "run" || d.Conversation[1]["text"] != "other note" {
		t.Fatalf("duplicat: %s", raw)
	}
	if len(d.Events) != 1 || d.Events[0]["body"] != "" || d.Events[0]["to"] != "" || d.Events[0]["text"] != "Reported" {
		t.Fatalf("events fara body/to: %s", raw)
	}
}

func TestConversationRunTextCap(t *testing.T) {
	long := strings.Repeat("x", 20010)
	got := buildConversation(nil, []store.Message{{Author: "Rook", Text: long, RunID: "r1"}})
	if len(got) != 1 || len([]rune(got[0].Text)) != 20006 || !strings.HasSuffix(got[0].Text, " [...]") {
		t.Fatalf("cap: %d", len([]rune(got[0].Text)))
	}
}
