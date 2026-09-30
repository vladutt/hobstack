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
