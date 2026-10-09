package web

import (
	"net/http"
	"strings"
	"testing"

	"foreman/internal/store"
)

func TestMoveDeleteRestoreAPI(t *testing.T) {
	e := setup(t)
	p := e.createProject(t, t.TempDir())
	w := e.addWorker(t, p.ID, "Rook")
	task := store.Task{ProjectID: p.ID, Title: "Page", AssigneeID: w.ID}
	if err := e.st.CreateTask(&task); err != nil {
		t.Fatal(err)
	}
	id := itoa(task.ID)

	if code, raw := e.do(t, "POST", "/api/tasks/"+id+"/move", map[string]any{"to": "done"}); code != http.StatusNoContent {
		t.Fatalf("close: %d %s", code, raw)
	}
	if code, _ := e.do(t, "POST", "/api/tasks/"+id+"/move", map[string]any{"to": "queued"}); code != http.StatusBadRequest {
		t.Fatalf("reopen fara motiv: %d", code)
	}
	if code, _ := e.do(t, "POST", "/api/tasks/"+id+"/move", map[string]any{"to": "review"}); code != http.StatusConflict {
		t.Fatalf("tinta nepermisa: %d", code)
	}

	code, raw := e.do(t, "DELETE", "/api/tasks/"+id, nil)
	if code != http.StatusOK || !strings.Contains(string(raw), `"deleted":1`) {
		t.Fatalf("delete: %d %s", code, raw)
	}
	if code, _ := e.do(t, "DELETE", "/api/tasks/"+id, nil); code != http.StatusConflict {
		t.Fatalf("delete de doua ori: %d", code)
	}
	code, raw = e.do(t, "GET", "/api/projects/"+itoa(p.ID), nil)
	if code != 200 || !strings.Contains(string(raw), `"archived":1`) || strings.Contains(string(raw), `"title":"Page"`) {
		t.Fatalf("vederea proiectului: %s", raw)
	}
	code, raw = e.do(t, "GET", "/api/projects/"+itoa(p.ID)+"/archived", nil)
	if code != 200 || !strings.Contains(string(raw), `"title":"Page"`) {
		t.Fatalf("lista sterse: %d %s", code, raw)
	}
	if code, raw := e.do(t, "POST", "/api/tasks/"+id+"/restore", nil); code != http.StatusNoContent {
		t.Fatalf("restore: %d %s", code, raw)
	}
	if code, _ := e.do(t, "POST", "/api/tasks/"+id+"/restore", nil); code != http.StatusConflict {
		t.Fatalf("restore de doua ori: %d", code)
	}
}
