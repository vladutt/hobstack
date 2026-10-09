package web

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"hobstack/internal/mcp"
	"hobstack/internal/store"
)

// requestFromLead face o cerere add_agent de la radacina proiectului (fara autonomy, deci cerere deschisa).
func requestFromLead(t *testing.T, e *env, p store.Project, name string) store.AgentRequest {
	t.Helper()
	lead := e.root(t, p.ID)
	lead.Tools = []string{"write"}
	if err := e.st.UpdateAgent(lead); err != nil {
		t.Fatal(err)
	}
	c := mcp.Caller{ProjectID: p.ID, AgentID: lead.ID, CanAddAgents: true}
	if _, err := e.tm.AddAgent(c, mcp.AddAgentInput{Name: name, Role: "QA", Instructions: "test", Reason: "tests"}); err != nil {
		t.Fatal(err)
	}
	reqs, _ := e.st.ListOpenAgentRequests(p.ID)
	return reqs[len(reqs)-1]
}

func TestAgentRequestsInProject(t *testing.T) {
	e := setup(t)
	p := e.createProject(t, t.TempDir())
	r := requestFromLead(t, e, p, "Quinn")
	code, raw := e.do(t, "GET", "/api/projects/"+itoa(p.ID), nil)
	var d struct {
		AgentRequests []struct {
			ID       int64           `json:"id"`
			LeadName string          `json:"leadName"`
			Spec     store.AgentSpec `json:"spec"`
			Reason   string          `json:"reason"`
		} `json:"agentRequests"`
	}
	json.Unmarshal(raw, &d)
	if code != 200 || len(d.AgentRequests) != 1 || d.AgentRequests[0].ID != r.ID || d.AgentRequests[0].LeadName == "" ||
		d.AgentRequests[0].Spec.Name != "Quinn" || d.AgentRequests[0].Reason != "tests" {
		t.Fatalf("%d %s", code, raw)
	}
	_, raw = e.do(t, "GET", "/api/projects", nil)
	if !strings.Contains(string(raw), `"agentRequests":1`) {
		t.Fatalf("cardul: %s", raw)
	}
}

func TestApproveAgentRequestAPI(t *testing.T) {
	e := setup(t)
	p := e.createProject(t, t.TempDir())
	r := requestFromLead(t, e, p, "Quinn")
	if code, raw := e.do(t, "POST", "/api/agent-requests/"+itoa(r.ID)+"/approve", nil); code != http.StatusNoContent {
		t.Fatalf("%d %s", code, raw)
	}
	a, err := e.st.FindAgentByName(p.ID, "Quinn")
	if err != nil || a.CreatedBy == nil {
		t.Fatalf("%+v %v", a, err)
	}
	if code, _ := e.do(t, "POST", "/api/agent-requests/"+itoa(r.ID)+"/approve", nil); code != http.StatusConflict {
		t.Fatalf("a doua aprobare: %d", code)
	}
	if code, _ := e.do(t, "POST", "/api/agent-requests/9999/approve", nil); code != http.StatusNotFound {
		t.Fatalf("inexistenta: %d", code)
	}
	_, raw := e.do(t, "GET", "/api/projects/"+itoa(p.ID), nil)
	if !strings.Contains(string(raw), `"createdBy":`+itoa(*a.CreatedBy)) {
		t.Fatalf("createdBy lipseste din agenti: %s", raw)
	}
}

func TestApproveAgentRequestNameTakenAPI(t *testing.T) {
	e := setup(t)
	p := e.createProject(t, t.TempDir())
	r := requestFromLead(t, e, p, "Quinn")
	e.addWorker(t, p.ID, "Quinn")
	code, raw := e.do(t, "POST", "/api/agent-requests/"+itoa(r.ID)+"/approve", nil)
	if code != http.StatusConflict || !strings.Contains(string(raw), `already exists`) {
		t.Fatalf("%d %s", code, raw)
	}
}

func TestDismissAgentRequestAPI(t *testing.T) {
	e := setup(t)
	p := e.createProject(t, t.TempDir())
	r := requestFromLead(t, e, p, "Quinn")
	if code, raw := e.do(t, "POST", "/api/agent-requests/"+itoa(r.ID)+"/dismiss", nil); code != http.StatusNoContent {
		t.Fatalf("%d %s", code, raw)
	}
	if code, _ := e.do(t, "POST", "/api/agent-requests/"+itoa(r.ID)+"/dismiss", nil); code != http.StatusConflict {
		t.Fatalf("a doua oara: %d", code)
	}
}
