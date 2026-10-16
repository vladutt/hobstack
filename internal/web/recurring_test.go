package web

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"hobstack/internal/schedule"
	"hobstack/internal/store"
	"hobstack/internal/team"
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

func TestRecurringHistoryAPI(t *testing.T) {
	e := setup(t)
	p := e.createProject(t, t.TempDir())
	root := e.root(t, p.ID)
	rec := store.RecurringTask{ProjectID: p.ID, Title: "Scout", AssigneeID: &root.ID,
		Schedule: schedule.Spec{Kind: schedule.KindDaily, At: "09:00"}, Enabled: true, NextRunAt: time.Now()}
	if err := e.st.CreateRecurring(&rec); err != nil {
		t.Fatal(err)
	}
	run := func() store.Task {
		task := store.Task{ProjectID: p.ID, Title: "Scout", AssigneeID: root.ID, ByUser: true, RecurringID: &rec.ID}
		if err := e.st.CreateTask(&task); err != nil {
			t.Fatal(err)
		}
		return task
	}
	r1, r2 := run(), run()
	e.addTaskFile(t, r1, "leads.csv", "text/csv", "a")
	e.addTaskFile(t, r2, "leads.csv", "text/csv", "ab")
	e.addTaskFile(t, r2, "round2.md", "text/markdown", "# r2")
	base := "/api/recurring/" + itoa(rec.ID)

	// runs in detaliul proiectului
	_, raw := e.do(t, "GET", "/api/projects/"+itoa(p.ID), nil)
	if !strings.Contains(string(raw), `"runs":2`) {
		t.Fatalf("detaliu fara runs: %s", raw)
	}

	var h struct {
		Recurring struct {
			ID   int64 `json:"id"`
			Runs int   `json:"runs"`
		} `json:"recurring"`
		Counts struct {
			Total int `json:"total"`
			Open  int `json:"open"`
		} `json:"counts"`
		Runs []struct {
			ID       int64  `json:"id"`
			Ref      string `json:"ref"`
			Assignee string `json:"assignee"`
			Files    int    `json:"files"`
			Status   string `json:"status"`
		} `json:"runs"`
		More bool `json:"more"`
	}
	code, raw := e.do(t, "GET", base+"/history", nil)
	json.Unmarshal(raw, &h)
	if code != 200 || h.Recurring.ID != rec.ID || h.Recurring.Runs != 2 || h.Counts.Total != 2 || h.Counts.Open != 2 || h.More {
		t.Fatalf("history: %d %s", code, raw)
	}
	if len(h.Runs) != 2 || h.Runs[0].ID != r2.ID || h.Runs[0].Ref != r2.Ref() || h.Runs[0].Assignee != root.Name || h.Runs[0].Files != 2 {
		t.Fatalf("runs: %s", raw)
	}

	// paginare
	h.Runs = nil
	_, raw = e.do(t, "GET", base+"/history?limit=1", nil)
	json.Unmarshal(raw, &h)
	if len(h.Runs) != 1 || !h.More || h.Runs[0].ID != r2.ID {
		t.Fatalf("limit=1: %s", raw)
	}
	h.Runs = nil
	_, raw = e.do(t, "GET", base+"/history?limit=1&before="+itoa(r2.ID), nil)
	json.Unmarshal(raw, &h)
	if len(h.Runs) != 1 || h.More || h.Runs[0].ID != r1.ID {
		t.Fatalf("before: %s", raw)
	}

	// parametri invalizi -> 400; sarcina inexistenta -> 404
	for _, q := range []string{"?limit=0", "?limit=51", "?limit=x", "?before=-1", "?before=x"} {
		if code, raw := e.do(t, "GET", base+"/history"+q, nil); code != http.StatusBadRequest {
			t.Errorf("%s: %d %s", q, code, raw)
		}
	}
	for _, path := range []string{"/api/recurring/9999/history", "/api/recurring/9999/files"} {
		if code, _ := e.do(t, "GET", path, nil); code != http.StatusNotFound {
			t.Errorf("%s: %d", path, code)
		}
	}

	// fisiere: grupate pe cale, ultima versiune sus, versiunile cu ref
	var f struct {
		Files []struct {
			Path     string `json:"path"`
			Ref      string `json:"ref"`
			Size     int64  `json:"size"`
			Versions []struct {
				TaskID int64  `json:"taskId"`
				Ref    string `json:"ref"`
			} `json:"versions"`
		} `json:"files"`
		Subtasks []any `json:"subtasks"`
	}
	code, raw = e.do(t, "GET", base+"/files", nil)
	json.Unmarshal(raw, &f)
	if code != 200 || len(f.Files) != 2 || f.Subtasks == nil {
		t.Fatalf("files: %d %s", code, raw)
	}
	var leads = f.Files[0]
	if leads.Path != "leads.csv" {
		leads = f.Files[1]
	}
	if leads.Ref != r2.Ref() || leads.Size != 2 || len(leads.Versions) != 1 || leads.Versions[0].TaskID != r1.ID || leads.Versions[0].Ref != r1.Ref() {
		t.Fatalf("leads.csv: %s", raw)
	}
}

func TestRecurringCreatedByName(t *testing.T) {
	e := setup(t)
	p := e.createProject(t, t.TempDir())
	root := e.root(t, p.ID)
	by := root.ID
	if _, err := e.tm.CreateRecurring(p.ID, team.RecurringInput{Title: "By lead", AssigneeID: root.ID,
		Schedule: schedule.Spec{Kind: schedule.KindDaily, At: "09:00"}, Enabled: true, CreatedBy: &by}); err != nil {
		t.Fatal(err)
	}
	_, raw := e.do(t, "GET", "/api/projects/"+itoa(p.ID), nil)
	if !strings.Contains(string(raw), `"createdByName":"`+root.Name+`"`) {
		t.Fatalf("%s", raw)
	}
}
