package web

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"foreman/internal/store"
)

func TestStagesAPI(t *testing.T) {
	e := setup(t)
	p := e.createProject(t, t.TempDir())
	root := e.root(t, p.ID)
	path := "/api/projects/" + itoa(p.ID)

	// fara etape: POST tasks -> 400
	if code, _ := e.do(t, "POST", path+"/tasks", map[string]any{"title": "x"}); code != 400 {
		t.Fatalf("task fara etape: %d", code)
	}
	bad := []map[string]any{
		{"stages": []map[string]any{{"name": "", "kind": "work"}}},
		{"stages": []map[string]any{{"name": "Dev", "kind": "work"}, {"name": "dev", "kind": "work"}}},
		{"stages": []map[string]any{{"name": "Dev", "kind": "altceva"}}},
		{"stages": []map[string]any{{"name": "Dev", "kind": "work", "agentIds": []int64{99999}}}},
	}
	for _, b := range bad {
		if code, raw := e.do(t, "PUT", path+"/stages", b); code != 400 {
			t.Errorf("%v: %d %s", b, code, raw)
		}
	}
	code, raw := e.do(t, "PUT", path+"/stages", map[string]any{"stages": []map[string]any{
		{"name": "Dev", "kind": "work", "agentIds": []int64{root.ID}},
		{"name": "QA", "kind": "check"},
	}})
	var saved []store.Stage
	json.Unmarshal(raw, &saved)
	if code != 200 || len(saved) != 2 {
		t.Fatalf("salvare: %d %s", code, raw)
	}
	code, raw = e.do(t, "POST", path+"/tasks", map[string]any{"title": "Login", "description": "d"})
	if code != http.StatusCreated {
		t.Fatalf("task: %d %s", code, raw)
	}
	// detaliul contine etapele si stageId
	_, raw = e.do(t, "GET", path, nil)
	if !strings.Contains(string(raw), `"stages":[{`) || !strings.Contains(string(raw), `"stageId":`) {
		t.Fatalf("detaliu: %s", raw)
	}
	// stergerea etapei cu task -> 409
	if code, raw := e.do(t, "PUT", path+"/stages", map[string]any{"stages": []map[string]any{{"id": saved[1].ID, "name": "QA", "kind": "check"}}}); code != 409 || !strings.Contains(string(raw), "Dev") {
		t.Fatalf("409: %d %s", code, raw)
	}
	// decide pe un task care nu asteapta decizia utilizatorului -> 409
	var task store.Task
	tasks, _ := e.st.ListTasks(p.ID)
	task = tasks[0]
	if code, _ := e.do(t, "POST", "/api/tasks/"+itoa(task.ID)+"/decide", map[string]any{"action": "accept"}); code != 409 {
		t.Fatalf("decide: %d", code)
	}
}

// detaliul proiectului spune ce task-uri asteapta decizia utilizatorului
func TestDetailListsUserDecisions(t *testing.T) {
	e := setup(t)
	p := e.createProject(t, t.TempDir())
	root := e.root(t, p.ID)
	path := "/api/projects/" + itoa(p.ID)
	e.do(t, "PUT", path+"/stages", map[string]any{"stages": []map[string]any{{"name": "Dev", "kind": "work", "agentIds": []int64{root.ID}}}})
	_, raw := e.do(t, "POST", path+"/tasks", map[string]any{"title": "A"})
	var task store.Task
	json.Unmarshal(raw, &task)
	task.StageID, task.Status, task.ReviewStage = nil, store.StatusReview, store.StageLead
	if err := e.st.SaveTask(&task); err != nil {
		t.Fatal(err)
	}
	var d struct {
		UserDecides []int64 `json:"userDecides"`
	}
	_, raw = e.do(t, "GET", path, nil)
	json.Unmarshal(raw, &d)
	if len(d.UserDecides) != 1 || d.UserDecides[0] != task.ID {
		t.Fatalf("userDecides: %s", raw)
	}
}

// task-uri ale utilizatorului intr-un proiect fara etape si mutarea lor
func TestUserTaskAndAssignAPI(t *testing.T) {
	e := setup(t)
	p := e.createProject(t, t.TempDir())
	root := e.root(t, p.ID)
	path := "/api/projects/" + itoa(p.ID)
	code, raw := e.do(t, "POST", path+"/agents", map[string]any{"name": "Vera", "model": "haiku", "parentId": root.ID})
	var vera store.Agent
	json.Unmarshal(raw, &vera)
	if code != 201 {
		t.Fatalf("agent: %d %s", code, raw)
	}
	if code, raw := e.do(t, "POST", path+"/tasks", map[string]any{"title": "A"}); code != 400 || !strings.Contains(string(raw), "agent") {
		t.Fatalf("fara agent: %d %s", code, raw)
	}
	code, raw = e.do(t, "POST", path+"/tasks", map[string]any{"title": "A", "assigneeId": root.ID})
	var task store.Task
	json.Unmarshal(raw, &task)
	if code != 201 || task.AssigneeID != root.ID || !task.ByUser {
		t.Fatalf("creare: %d %s", code, raw)
	}
	if code, raw := e.do(t, "POST", "/api/tasks/"+itoa(task.ID)+"/assign", map[string]any{"agentId": vera.ID}); code != 204 {
		t.Fatalf("assign: %d %s", code, raw)
	}
	if got, _ := e.st.GetTask(task.ID); got.AssigneeID != vera.ID {
		t.Fatalf("asignat: %+v", got)
	}
	if code, _ := e.do(t, "POST", "/api/tasks/"+itoa(task.ID)+"/assign", map[string]any{"agentId": 99999}); code != 400 {
		t.Fatalf("agent strain: %d", code)
	}
	task.Status = store.StatusInProgress
	e.st.SaveTask(&task)
	if code, _ := e.do(t, "POST", "/api/tasks/"+itoa(task.ID)+"/assign", map[string]any{"agentId": root.ID}); code != 409 {
		t.Fatalf("in lucru: %d", code)
	}
}
