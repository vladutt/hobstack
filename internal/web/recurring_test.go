package web

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestRecurringAPI(t *testing.T) {
	e := setup(t)
	p := e.createProject(t, t.TempDir())
	root := e.root(t, p.ID)
	base := "/api/projects/" + itoa(p.ID) + "/recurring"
	daily := map[string]any{"kind": "daily", "at": "09:00"}
	body := func(over map[string]any) map[string]any {
		b := map[string]any{"title": "Tests", "description": "run them", "assigneeId": root.ID, "schedule": daily, "autoAccept": true, "enabled": true}
		for k, v := range over {
			b[k] = v
		}
		return b
	}
	for _, bad := range []map[string]any{
		body(map[string]any{"title": "  "}),
		body(map[string]any{"title": strings.Repeat("x", 201)}),
		body(map[string]any{"schedule": map[string]any{"kind": "daily", "at": "25:00"}}),
		body(map[string]any{"assigneeId": 0}), // fara etape: agent obligatoriu
	} {
		if code, raw := e.do(t, "POST", base, bad); code != http.StatusBadRequest {
			t.Errorf("%v: %d %s", bad, code, raw)
		}
	}
	code, raw := e.do(t, "POST", base, body(nil))
	var v struct {
		ID           int64  `json:"id"`
		ScheduleText string `json:"scheduleText"`
		Assignee     string `json:"assignee"`
		Enabled      bool   `json:"enabled"`
	}
	json.Unmarshal(raw, &v)
	if code != http.StatusCreated || v.ScheduleText != "Daily at 09:00" || v.Assignee != root.Name {
		t.Fatalf("%d %s", code, raw)
	}
	// lista vine in detaliul proiectului
	_, raw = e.do(t, "GET", "/api/projects/"+itoa(p.ID), nil)
	if !strings.Contains(string(raw), `"recurring":[{`) {
		t.Fatalf("detaliu: %s", raw)
	}
	// run now -> task cu recurringId; a doua oara -> 400
	code, raw = e.do(t, "POST", "/api/recurring/"+itoa(v.ID)+"/run", nil)
	var task struct {
		ID          int64  `json:"id"`
		RecurringID *int64 `json:"recurringId"`
	}
	json.Unmarshal(raw, &task)
	if code != http.StatusCreated || task.RecurringID == nil || *task.RecurringID != v.ID {
		t.Fatalf("run: %d %s", code, raw)
	}
	if code, raw := e.do(t, "POST", "/api/recurring/"+itoa(v.ID)+"/run", nil); code != http.StatusBadRequest || !strings.Contains(string(raw), "still open") {
		t.Fatalf("run dublu: %d %s", code, raw)
	}
	_, raw = e.do(t, "GET", "/api/tasks/"+itoa(task.ID), nil)
	if !strings.Contains(string(raw), `"recurring":{"id":`+itoa(v.ID)+`,"title":"Tests"}`) {
		t.Fatalf("getTask: %s", raw)
	}
	if code, raw := e.do(t, "PUT", "/api/recurring/"+itoa(v.ID), body(map[string]any{"enabled": false})); code != 200 || !strings.Contains(string(raw), `"enabled":false`) {
		t.Fatalf("put: %d %s", code, raw)
	}
	if code, _ := e.do(t, "DELETE", "/api/recurring/"+itoa(v.ID), nil); code != http.StatusNoContent {
		t.Fatalf("delete: %d", code)
	}
	if code, _ := e.do(t, "PUT", "/api/recurring/"+itoa(v.ID), body(nil)); code != http.StatusNotFound {
		t.Fatalf("put dupa delete: %d", code)
	}
	_, raw = e.do(t, "GET", "/api/tasks/"+itoa(task.ID), nil)
	if !strings.Contains(string(raw), `"recurring":null`) {
		t.Fatalf("getTask dupa delete: %s", raw)
	}
}
