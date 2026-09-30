package web

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	wizard "foreman/internal/setup" // alias: in pachetul web, "setup" e deja helper-ul de test
)

func withSetup(t *testing.T) *env {
	e := setup(t)
	e.srv.SetSetup(wizard.New(nopStarter{}, "http://127.0.0.1:0", t.TempDir()))
	return e
}

func TestSetupStartValidation(t *testing.T) {
	e := withSetup(t)
	cases := []map[string]any{
		{"workdir": "", "description": "", "model": "sonnet"},
		{"workdir": filepath.Join(t.TempDir(), "nu-exista"), "model": "sonnet"},
		{"description": "x", "model": "gpt-5"},
	}
	for _, c := range cases {
		if code, raw := e.do(t, "POST", "/api/setup", c); code != 400 {
			t.Errorf("%v: %d %s", c, code, raw)
		}
	}
	code, raw := e.do(t, "POST", "/api/setup", map[string]any{"description": "un blog", "model": "haiku"})
	var s struct{ ID, RunID, Status string }
	json.Unmarshal(raw, &s)
	if code != 201 || s.ID == "" || s.Status != "running" {
		t.Fatalf("%d %s", code, raw)
	}
	if code, _ := e.do(t, "GET", "/api/setup/"+s.ID, nil); code != 200 {
		t.Fatalf("get: %d", code)
	}
	if code, _ := e.do(t, "GET", "/api/setup/inexistent", nil); code != 404 {
		t.Fatalf("get inexistent: %d", code)
	}
	if code, _ := e.do(t, "POST", "/api/setup/"+s.ID+"/stop", nil); code != http.StatusAccepted {
		t.Fatalf("stop: %d", code)
	}
}

func TestCreateProjectFromSetup(t *testing.T) {
	e := withSetup(t)
	proposal := map[string]any{
		"name": "Shop", "goal": "g", "context": "c",
		"agents": []map[string]any{
			{"name": "Ana", "role": "Lead", "model": "sonnet", "reportsTo": ""},
			{"name": "Bob", "role": "Backend", "model": "haiku", "reportsTo": "Ana", "reviewedBy": "Cia", "tools": []string{"write", "shell"}},
			{"name": "Cia", "role": "QA", "model": "haiku", "reportsTo": "Ana"},
		},
	}
	code, raw := e.do(t, "POST", "/api/projects/setup", map[string]any{"workdir": t.TempDir(), "proposal": proposal})
	var p struct{ ID int64 }
	json.Unmarshal(raw, &p)
	if code != 201 || p.ID == 0 {
		t.Fatalf("%d %s", code, raw)
	}
	d := e.detail(t, p.ID)
	if len(d.Agents) != 3 {
		t.Fatalf("agenti: %d", len(d.Agents))
	}
	byName := map[string]int64{}
	for _, a := range d.Agents {
		byName[a.Name] = a.ID
	}
	for _, a := range d.Agents {
		if a.Name == "Bob" && (a.ParentID == nil || *a.ParentID != byName["Ana"] || a.ReviewerID == nil || *a.ReviewerID != byName["Cia"]) {
			t.Fatalf("legaturi Bob: %+v", a)
		}
	}

	// ciclu: 400 si nimic creat
	before := len(e.projects(t))
	proposal["agents"].([]map[string]any)[0]["reportsTo"] = "Bob"
	code, raw = e.do(t, "POST", "/api/projects/setup", map[string]any{"workdir": t.TempDir(), "proposal": proposal})
	if code != 400 || !strings.Contains(string(raw), "cycle") {
		t.Fatalf("ciclu: %d %s", code, raw)
	}
	if len(e.projects(t)) != before {
		t.Fatal("nu trebuia creat nimic")
	}
}

func (e *env) projects(t *testing.T) []json.RawMessage {
	_, raw := e.do(t, "GET", "/api/projects", nil)
	var out []json.RawMessage
	json.Unmarshal(raw, &out)
	return out
}
