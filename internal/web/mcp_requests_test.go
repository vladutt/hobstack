package web

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"hobstack/internal/mcp"
	"hobstack/internal/store"
)

// install_mcp: agentul cere, utilizatorul instaleaza din Needs you (cu proba) sau refuza; agentul afla
func TestMCPRequestFlow(t *testing.T) {
	e := setupLib(t)
	p := e.project(t)
	agents, _ := e.st.ListAgents(p.ID)
	c := mcp.Caller{ProjectID: p.ID, AgentID: agents[0].ID}
	in := mcp.MCPRequestInput{Name: "vehicles", Description: "Vehicles", Command: "env", Args: helperArgs(),
		Secrets: []string{"VEH_TOKEN"}, Reason: "To read the vehicles"}
	text, err := e.tm.RequestMCP(c, in)
	if err != nil || !strings.Contains(text, "Needs you") {
		t.Fatalf("%q %v", text, err)
	}
	if text, _ := e.tm.RequestMCP(c, in); !strings.Contains(text, "already asked") {
		t.Fatalf("duplicate: %q", text)
	}
	if _, err := e.tm.RequestMCP(c, mcp.MCPRequestInput{Name: "x", Description: "X", Command: "npx", Reason: "r",
		Args: []string{"--token=<hidden>"}}); err == nil {
		t.Fatal("a hidden value in the arguments must be refused")
	}

	code, raw := e.do(t, "GET", "/api/projects/"+itoa(p.ID), nil)
	var d struct {
		MCPRequests []mcpRequestView `json:"mcpRequests"`
		Card        struct{}         `json:"-"`
	}
	json.Unmarshal(raw, &d)
	if code != 200 || len(d.MCPRequests) != 1 || d.MCPRequests[0].Command != "env" || d.MCPRequests[0].AgentName == "" {
		t.Fatalf("%d %s", code, raw)
	}
	id := itoa(d.MCPRequests[0].ID)

	code, raw = e.do(t, "POST", "/api/mcp-requests/"+id+"/install", map[string]any{"secretValues": map[string]string{"VEH_TOKEN": "s3"}})
	if code != 200 || !strings.Contains(string(raw), "list_vehicles") {
		t.Fatalf("install: %d %s", code, raw)
	}
	it, err := e.st.FindLibraryItem(store.KindMCP, "vehicles")
	if err != nil {
		t.Fatal(err)
	}
	if ins, _ := e.st.ListInstalls(p.ID); len(ins) != 1 || ins[0].ItemID != it.ID {
		t.Fatalf("installs: %+v", ins)
	}
	if code, _ := e.do(t, "POST", "/api/mcp-requests/"+id+"/install", map[string]any{}); code != http.StatusConflict {
		t.Fatalf("second install: %d", code)
	}
	notes, _ := e.st.Undelivered(agents[0].ID)
	if len(notes) == 0 || !strings.Contains(notes[len(notes)-1].Text, `installed the MCP server "vehicles"`) {
		t.Fatalf("notes: %+v", notes)
	}
	// deja instalat in proiect: fara cerere noua
	if text, _ := e.tm.RequestMCP(c, in); !strings.Contains(text, "already installed") {
		t.Fatalf("installed: %q", text)
	}

	// alt proiect: acelasi nume = elementul din Library, doar activat acolo
	p2 := e.project(t)
	a2, _ := e.st.ListAgents(p2.ID)
	c2 := mcp.Caller{ProjectID: p2.ID, AgentID: a2[0].ID}
	if text, err := e.tm.RequestMCP(c2, mcp.MCPRequestInput{Name: "vehicles", Description: "Other", Command: "rm", Reason: "need it"}); err != nil || !strings.Contains(text, "already exists") {
		t.Fatalf("%q %v", text, err)
	}
	reqs, _ := e.st.ListOpenMCPRequests(p2.ID)
	if len(reqs) != 1 || reqs[0].ItemID == nil || reqs[0].Command != "env" {
		t.Fatalf("existing item request: %+v", reqs)
	}
	if code, raw := e.do(t, "POST", "/api/mcp-requests/"+itoa(reqs[0].ID)+"/install", map[string]any{}); code != 200 {
		t.Fatalf("enable: %d %s", code, raw)
	}
	if ins, _ := e.st.ListInstalls(p2.ID); len(ins) != 1 || ins[0].ItemID != it.ID {
		t.Fatalf("p2 installs: %+v", ins)
	}

	// refuz
	e.tm.RequestMCP(c, mcp.MCPRequestInput{Name: "remote-one", Description: "R", URL: "https://example.com/mcp", Reason: "r"})
	reqs, _ = e.st.ListOpenMCPRequests(p.ID)
	if len(reqs) != 1 || reqs[0].Transport != "remote" {
		t.Fatalf("%+v", reqs)
	}
	if code, _ := e.do(t, "POST", "/api/mcp-requests/"+itoa(reqs[0].ID)+"/decline", map[string]any{}); code != http.StatusNoContent {
		t.Fatalf("decline: %d", code)
	}
	notes, _ = e.st.Undelivered(agents[0].ID)
	if !strings.Contains(notes[len(notes)-1].Text, "declined") {
		t.Fatalf("decline note: %+v", notes[len(notes)-1])
	}
	if _, err := e.st.FindLibraryItem(store.KindMCP, "remote-one"); err == nil {
		t.Fatal("declined server was created")
	}
}
