package web

import (
	"encoding/json"
	"testing"

	"hobstack/internal/store"
)

// Pagina proiectului primeste task-urile fara textele lungi (raport, descriere, feedback); modala le ia din /api/tasks/{id}.
func TestProjectTasksWithoutLongTexts(t *testing.T) {
	e := setup(t)
	p := e.createProject(t, t.TempDir())
	w := e.addWorker(t, p.ID, "Atlas")
	task := store.Task{ProjectID: p.ID, Title: "Fix login", Description: "Long brief", AssigneeID: w.ID}
	if err := e.st.CreateTask(&task); err != nil {
		t.Fatal(err)
	}
	task.Result, task.Feedback, task.Status = "Long report", "Try again", store.StatusDone
	if err := e.st.SaveTask(&task); err != nil {
		t.Fatal(err)
	}

	_, raw := e.do(t, "GET", "/api/projects/"+itoa(p.ID), nil)
	var pd struct {
		Tasks []map[string]any `json:"tasks"`
	}
	json.Unmarshal(raw, &pd)
	if len(pd.Tasks) != 1 || pd.Tasks[0]["title"] != "Fix login" || pd.Tasks[0]["status"] != "done" {
		t.Fatalf("%s", raw)
	}
	for _, k := range []string{"description", "result", "feedback"} {
		if v, ok := pd.Tasks[0][k]; ok && v != "" {
			t.Fatalf("%s in lista proiectului: %v", k, v)
		}
	}

	_, raw = e.do(t, "GET", "/api/tasks/"+itoa(task.ID), nil)
	var full struct {
		Task store.Task `json:"task"`
	}
	json.Unmarshal(raw, &full)
	if full.Task.Description != "Long brief" || full.Task.Result != "Long report" || full.Task.Feedback != "Try again" {
		t.Fatalf("modala: %s", raw)
	}
}
