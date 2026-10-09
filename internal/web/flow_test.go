package web

import (
	"encoding/json"
	"net/http"
	"testing"

	"hobstack/internal/store"
)

func TestTaskFlowAPI(t *testing.T) {
	e := setup(t)
	p := e.createProject(t, t.TempDir())
	root := e.root(t, p.ID)
	parent := store.Task{ProjectID: p.ID, Title: "Parent", AssigneeID: root.ID}
	if err := e.st.CreateTask(&parent); err != nil {
		t.Fatal(err)
	}
	child := store.Task{ProjectID: p.ID, Title: "Child", AssigneeID: root.ID, ParentTaskID: &parent.ID}
	if err := e.st.CreateTask(&child); err != nil {
		t.Fatal(err)
	}
	code, raw := e.do(t, "GET", "/api/tasks/"+itoa(child.ID)+"/flow", nil)
	var f store.Flow
	if err := json.Unmarshal(raw, &f); err != nil || code != http.StatusOK {
		t.Fatalf("%d %s %v", code, raw, err)
	}
	if f.FocusID != child.ID || f.Root.ID != parent.ID || len(f.Root.Children) != 1 || f.Root.Children[0].ID != child.ID {
		t.Fatalf("%s", raw)
	}
	if code, _ := e.do(t, "GET", "/api/tasks/99999/flow", nil); code != http.StatusNotFound {
		t.Fatalf("inexistent: %d", code)
	}
}
