package web

import (
	"encoding/json"
	"strings"
	"testing"

	"hobstack/internal/store"
)

type taskDecisions struct {
	Decisions []store.Decision `json:"decisions"`
	Todos     []struct {
		Kind string `json:"kind"`
	} `json:"todos"`
}

// userReview: task creat din board, in Review la final (il decide utilizatorul).
func userReview(t *testing.T, e *env) (store.Task, store.Agent) {
	t.Helper()
	p := e.createProject(t, t.TempDir())
	rook := e.addWorker(t, p.ID, "Rook")
	task := store.Task{ProjectID: p.ID, Title: "Build page", AssigneeID: rook.ID, ByUser: true}
	if err := e.st.CreateTask(&task); err != nil {
		t.Fatal(err)
	}
	task.Status, task.ReviewStage, task.Result = store.StatusReview, store.StageLead, "done"
	if err := e.st.SaveTask(&task); err != nil {
		t.Fatal(err)
	}
	return task, rook
}

func (e *env) taskDecisions(t *testing.T, id int64) taskDecisions {
	t.Helper()
	code, raw := e.do(t, "GET", "/api/tasks/"+itoa(id), nil)
	if code != 200 {
		t.Fatalf("GET task: %d %s", code, raw)
	}
	var d taskDecisions
	if err := json.Unmarshal(raw, &d); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"decisions":[`) {
		t.Fatalf("decisions trebuie sa fie lista: %s", raw)
	}
	return d
}

func TestGetTaskDecisionsEmpty(t *testing.T) {
	e := setup(t)
	task, _ := userReview(t, e)
	if _, err := e.st.AddTodos(task.ID, []string{"Restart"}, "step"); err != nil {
		t.Fatal(err)
	}
	d := e.taskDecisions(t, task.ID)
	if len(d.Decisions) != 0 || len(d.Todos) != 1 || d.Todos[0].Kind != "step" {
		t.Fatalf("%+v", d)
	}
}

func TestDecideWithAnswers(t *testing.T) {
	e := setup(t)
	task, rook := userReview(t, e)
	if err := e.st.SetDecisions(task.ID, rook.Name, []store.Decision{{Question: "Follow-up?", Options: []string{"Yes", "No"}}}); err != nil {
		t.Fatal(err)
	}
	q := e.taskDecisions(t, task.ID).Decisions[0]
	path := "/api/tasks/" + itoa(task.ID) + "/decide"
	// alegere invalida -> 400, nimic salvat
	if code, raw := e.do(t, "POST", path, map[string]any{"action": "accept", "answers": []map[string]any{{"id": q.ID, "choices": []string{"Maybe"}}}}); code != 400 {
		t.Fatalf("alegere invalida: %d %s", code, raw)
	}
	// other prea lung -> 400
	if code, raw := e.do(t, "POST", path, map[string]any{"action": "accept", "answers": []map[string]any{{"id": q.ID, "other": strings.Repeat("x", 301)}}}); code != 400 {
		t.Fatalf("other lung: %d %s", code, raw)
	}
	if code, raw := e.do(t, "POST", path, map[string]any{"action": "accept", "answers": []map[string]any{{"id": q.ID, "choices": []string{"Yes"}}}}); code != 204 {
		t.Fatalf("decide: %d %s", code, raw)
	}
	d := e.taskDecisions(t, task.ID)
	if len(d.Decisions) != 1 || len(d.Decisions[0].Answer) != 1 || d.Decisions[0].Answer[0] != "Yes" {
		t.Fatalf("%+v", d)
	}
	if got, _ := e.st.GetTask(task.ID); got.Status != store.StatusDone {
		t.Fatalf("%+v", got)
	}
}
