package web

import (
	"encoding/json"
	"net/http"
	"testing"

	"foreman/internal/store"
)

// reviewTask: un task al lui Rook, in review la lead, proiect activ.
func reviewTask(t *testing.T, e *env) (store.Project, store.Agent, store.Task) {
	t.Helper()
	p := e.createProject(t, t.TempDir())
	e.st.SetPaused(p.ID, false)
	lead := e.root(t, p.ID)
	w := e.addWorker(t, p.ID, "Rook")
	task := store.Task{ProjectID: p.ID, Title: "A", AssigneeID: w.ID, CreatorID: &lead.ID}
	if err := e.st.CreateTask(&task); err != nil {
		t.Fatal(err)
	}
	task.Status, task.ReviewStage = store.StatusReview, store.StageLead
	if err := e.st.SaveTask(&task); err != nil {
		t.Fatal(err)
	}
	return p, lead, task
}

func TestStuckFieldsInJSON(t *testing.T) {
	e := setup(t)
	p, _, _ := reviewTask(t, e)
	code, raw := e.do(t, "GET", "/api/projects/"+itoa(p.ID), nil)
	if code != 200 {
		t.Fatalf("GET: %d %s", code, raw)
	}
	var d map[string]json.RawMessage
	json.Unmarshal(raw, &d)
	if string(d["stuck"]) != "[]" {
		t.Fatalf("stuck: %s", d["stuck"])
	}
	if string(d["stuckTasks"]) != "0" {
		t.Fatalf("stuckTasks: %s", d["stuckTasks"])
	}
}

func TestNudgeRoute(t *testing.T) {
	e := setup(t)
	_, lead, task := reviewTask(t, e)
	// Lead e idle si fara mesaje nelivrate
	msgs, _ := e.st.Undelivered(lead.ID)
	ids := make([]int64, len(msgs))
	for i, m := range msgs {
		ids[i] = m.ID
	}
	e.st.MarkDelivered(ids)

	if code, raw := e.do(t, "POST", "/api/tasks/"+itoa(task.ID)+"/nudge", nil); code != http.StatusNoContent {
		t.Fatalf("nudge: %d %s", code, raw)
	}
	if has, _ := e.st.HasUndelivered(lead.ID); !has {
		t.Fatal("Lead trebuia trezit cu o nota")
	}

	task.Status, task.ReviewStage = store.StatusDone, ""
	e.st.SaveTask(&task)
	if code, _ := e.do(t, "POST", "/api/tasks/"+itoa(task.ID)+"/nudge", nil); code != http.StatusConflict {
		t.Fatalf("task done: %d", code)
	}
	if code, _ := e.do(t, "POST", "/api/tasks/99999/nudge", nil); code != http.StatusNotFound {
		t.Fatalf("inexistent: %d", code)
	}
}
