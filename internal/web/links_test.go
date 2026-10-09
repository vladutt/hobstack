package web

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"hobstack/internal/mcp"
)

func TestProjectLinksAPI(t *testing.T) {
	e := setup(t)
	a := e.createProject(t, t.TempDir())
	code, raw := e.do(t, "POST", "/api/projects", map[string]any{"name": "Backend", "goal": "API", "workdir": t.TempDir()})
	if code != http.StatusCreated {
		t.Fatalf("%d %s", code, raw)
	}
	var b struct{ ID int64 }
	json.Unmarshal(raw, &b)

	if code, _ := e.do(t, "POST", "/api/projects/"+itoa(a.ID)+"/links", map[string]any{"projectId": a.ID}); code != http.StatusBadRequest {
		t.Fatalf("acelasi proiect: %d", code)
	}
	if code, _ := e.do(t, "POST", "/api/projects/"+itoa(a.ID)+"/links", map[string]any{"projectId": 9999}); code != http.StatusNotFound {
		t.Fatalf("inexistent: %d", code)
	}
	code, raw = e.do(t, "POST", "/api/projects/"+itoa(a.ID)+"/links", map[string]any{"projectId": b.ID})
	if code != http.StatusCreated || !strings.Contains(string(raw), `"name":"Backend"`) {
		t.Fatalf("%d %s", code, raw)
	}
	if code, _ := e.do(t, "POST", "/api/projects/"+itoa(b.ID)+"/links", map[string]any{"projectId": a.ID}); code != http.StatusConflict {
		t.Fatalf("dublura: %d", code)
	}
	_, raw = e.do(t, "GET", "/api/projects/"+itoa(b.ID)+"/links", nil)
	if !strings.Contains(string(raw), `"projectId":`+itoa(a.ID)) {
		t.Fatalf("din celalalt capat: %s", raw)
	}
	_, raw = e.do(t, "GET", "/api/projects/"+itoa(a.ID), nil)
	if !strings.Contains(string(raw), `"links":[{"projectId":`+itoa(b.ID)) {
		t.Fatalf("detaliu: %s", raw)
	}
	if code, _ := e.do(t, "DELETE", "/api/projects/"+itoa(a.ID)+"/links/"+itoa(b.ID), nil); code != http.StatusNoContent {
		t.Fatalf("stergere: %d", code)
	}
	if code, _ := e.do(t, "DELETE", "/api/projects/"+itoa(a.ID)+"/links/"+itoa(b.ID), nil); code != http.StatusNotFound {
		t.Fatalf("a doua stergere: %d", code)
	}
}

func TestWorkRequestsAPI(t *testing.T) {
	e := setup(t)
	a := e.createProject(t, t.TempDir())
	_, raw := e.do(t, "POST", "/api/projects", map[string]any{"name": "Backend", "goal": "API", "workdir": t.TempDir()})
	var b struct{ ID int64 }
	json.Unmarshal(raw, &b)
	e.do(t, "POST", "/api/projects/"+itoa(a.ID)+"/links", map[string]any{"projectId": b.ID})
	lead := e.root(t, a.ID)
	c := mcp.Caller{ProjectID: a.ID, AgentID: lead.ID, IsRoot: true, CanAddAgents: true, HasLinks: true}
	if _, err := e.tm.RequestProjectWork(c, "Backend", "Add /export", "CSV"); err != nil {
		t.Fatal(err)
	}
	var d struct {
		WorkRequests []struct {
			ID          int64  `json:"id"`
			Ref         string `json:"ref"`
			FromProject string `json:"fromProject"`
			FromAgent   string `json:"fromAgent"`
			Title       string `json:"title"`
		} `json:"workRequests"`
	}
	_, raw = e.do(t, "GET", "/api/projects/"+itoa(b.ID), nil)
	json.Unmarshal(raw, &d)
	if len(d.WorkRequests) != 1 || d.WorkRequests[0].FromProject != "ApiUnit" || d.WorkRequests[0].FromAgent != lead.Name ||
		d.WorkRequests[0].Title != "Add /export" || !strings.HasPrefix(d.WorkRequests[0].Ref, "W-") {
		t.Fatalf("%s", raw)
	}
	_, raw = e.do(t, "GET", "/api/projects", nil)
	if !strings.Contains(string(raw), `"workRequests":1`) {
		t.Fatalf("cardul: %s", raw)
	}
	id := itoa(d.WorkRequests[0].ID)
	if code, raw := e.do(t, "POST", "/api/project-requests/"+id+"/approve", nil); code != http.StatusNoContent {
		t.Fatalf("%d %s", code, raw)
	}
	if code, _ := e.do(t, "POST", "/api/project-requests/"+id+"/approve", nil); code != http.StatusConflict {
		t.Fatalf("a doua: %d", code)
	}
	if code, _ := e.do(t, "POST", "/api/project-requests/9999/dismiss", nil); code != http.StatusNotFound {
		t.Fatalf("inexistenta: %d", code)
	}
	_, raw = e.do(t, "GET", "/api/projects/"+itoa(b.ID), nil)
	if !strings.Contains(string(raw), `"fromProject":"ApiUnit"`) || !strings.Contains(string(raw), `"requestId":`) {
		t.Fatalf("task-ul cu fromProject: %s", raw)
	}
}

func TestApproveAfterUnlinkAPI(t *testing.T) {
	e := setup(t)
	a := e.createProject(t, t.TempDir())
	_, raw := e.do(t, "POST", "/api/projects", map[string]any{"name": "Backend", "goal": "API", "workdir": t.TempDir()})
	var b struct{ ID int64 }
	json.Unmarshal(raw, &b)
	e.do(t, "POST", "/api/projects/"+itoa(a.ID)+"/links", map[string]any{"projectId": b.ID})
	lead := e.root(t, a.ID)
	e.tm.RequestProjectWork(mcp.Caller{ProjectID: a.ID, AgentID: lead.ID, IsRoot: true, CanAddAgents: true, HasLinks: true}, "Backend", "x", "")
	reqs, _ := e.st.ListPendingWork(b.ID)
	e.st.UnlinkProjects(a.ID, b.ID)
	code, raw := e.do(t, "POST", "/api/project-requests/"+itoa(reqs[0].ID)+"/approve", nil)
	if code != http.StatusConflict || !strings.Contains(string(raw), "no longer linked") {
		t.Fatalf("%d %s", code, raw)
	}
}
