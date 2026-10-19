package web

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"hobstack/internal/store"
)

func TestTodosAPI(t *testing.T) {
	e := setup(t)
	p := e.createProject(t, t.TempDir())
	w := e.addWorker(t, p.ID, "Canvas")
	task := store.Task{ProjectID: p.ID, Title: "Terms", AssigneeID: w.ID}
	if err := e.st.CreateTask(&task); err != nil {
		t.Fatal(err)
	}
	e.st.AddTodos(task.ID, []string{"Company name", "VAT"}, "value")
	id := strconv.FormatInt(task.ID, 10)
	if code, raw := e.do(t, "PUT", "/api/tasks/"+id+"/todos/1", map[string]bool{"done": true}); code != http.StatusNoContent {
		t.Fatalf("%d %s", code, raw)
	}
	if code, _ := e.do(t, "PUT", "/api/tasks/"+id+"/todos/9", map[string]bool{"done": true}); code != http.StatusNotFound {
		t.Fatalf("punct lipsa: %d", code)
	}
	_, raw := e.do(t, "GET", "/api/tasks/"+id, nil)
	var td struct {
		Todos []store.Todo `json:"todos"`
	}
	json.Unmarshal(raw, &td)
	if len(td.Todos) != 2 || td.Todos[0].DoneAt == nil {
		t.Fatalf("%s", raw)
	}
	_, raw = e.do(t, "GET", "/api/projects/"+strconv.FormatInt(p.ID, 10), nil)
	var d struct {
		FillIns []store.FillIn `json:"fillIns"`
	}
	json.Unmarshal(raw, &d)
	if len(d.FillIns) != 1 || d.FillIns[0].Open != 1 {
		t.Fatalf("%s", raw)
	}
	if code, raw := e.do(t, "POST", "/api/tasks/"+id+"/comments", map[string]string{"text": "Meridian Auto SRL"}); code != http.StatusNoContent {
		t.Fatalf("%d %s", code, raw)
	}
	if code, _ := e.do(t, "POST", "/api/tasks/"+id+"/comments", map[string]string{"text": " "}); code != http.StatusBadRequest {
		t.Fatalf("comentariu gol: %d", code)
	}
	if code, _ := e.do(t, "POST", "/api/tasks/99999/comments", map[string]string{"text": "x"}); code != http.StatusNotFound {
		t.Fatalf("task lipsa: %d", code)
	}
	// task sters: comentariul si bifa sunt refuzate cu 409
	if code, raw := e.do(t, "DELETE", "/api/tasks/"+id, nil); code != http.StatusOK {
		t.Fatalf("delete: %d %s", code, raw)
	}
	if code, raw := e.do(t, "POST", "/api/tasks/"+id+"/comments", map[string]string{"text": "x"}); code != http.StatusConflict || !strings.Contains(string(raw), "restore it first") {
		t.Fatalf("comentariu pe sters: %d %s", code, raw)
	}
	if code, raw := e.do(t, "PUT", "/api/tasks/"+id+"/todos/2", map[string]bool{"done": true}); code != http.StatusConflict {
		t.Fatalf("bifa pe sters: %d %s", code, raw)
	}
}

// Full autonomy: pasii lasati utilizatorului nu intra in "need you" (raman pe task)
func TestFillInsAutonomySkipsSteps(t *testing.T) {
	e := setup(t)
	p := e.createProject(t, t.TempDir())
	w := e.addWorker(t, p.ID, "Canvas")
	task := store.Task{ProjectID: p.ID, Title: "Map", AssigneeID: w.ID}
	if err := e.st.CreateTask(&task); err != nil {
		t.Fatal(err)
	}
	e.st.AddTodos(task.ID, []string{"Check the map"}, "step")
	count := func() (int, int) {
		_, raw := e.do(t, "GET", "/api/projects/"+strconv.FormatInt(p.ID, 10), nil)
		var d struct {
			FillIns []store.FillIn `json:"fillIns"`
		}
		json.Unmarshal(raw, &d)
		_, raw = e.do(t, "GET", "/api/projects", nil)
		var list []struct {
			ID      int64 `json:"id"`
			FillIns int   `json:"fillIns"`
		}
		json.Unmarshal(raw, &list)
		for _, c := range list {
			if c.ID == p.ID {
				return len(d.FillIns), c.FillIns
			}
		}
		return len(d.FillIns), -1
	}
	if a, b := count(); a != 1 || b != 1 {
		t.Fatalf("without autonomy: %d %d", a, b)
	}
	full, err := e.st.GetProject(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	full.Autonomy = true
	if err := e.st.UpdateProject(full); err != nil {
		t.Fatal(err)
	}
	if a, b := count(); a != 0 || b != 0 {
		t.Fatalf("with autonomy: %d %d", a, b)
	}
	// pe task pasul ramane
	_, raw := e.do(t, "GET", "/api/tasks/"+strconv.FormatInt(task.ID, 10), nil)
	if !strings.Contains(string(raw), "Check the map") {
		t.Fatalf("%s", raw)
	}
}
