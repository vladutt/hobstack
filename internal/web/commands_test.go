package web

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"foreman/internal/store"
)

func TestCommandSettingsAPI(t *testing.T) {
	e := setup(t)
	code, raw := e.do(t, "GET", "/api/settings/commands", nil)
	var global []store.CommandRule
	json.Unmarshal(raw, &global)
	if code != http.StatusOK || len(global) != 4 {
		t.Fatalf("%d %s", code, raw)
	}
	for _, bad := range []any{
		[]map[string]string{{"prefix": "git push; rm", "mode": "ask"}},
		[]map[string]string{{"prefix": "", "mode": "ask"}},
		[]map[string]string{{"prefix": strings.Repeat("a", 81), "mode": "ask"}},
		[]map[string]string{{"prefix": "docker", "mode": "maybe"}},
	} {
		if code, _ := e.do(t, "PUT", "/api/settings/commands", bad); code != http.StatusBadRequest {
			t.Fatalf("acceptat %v: %d", bad, code)
		}
	}
	if _, raw := e.do(t, "GET", "/api/settings/commands", nil); !strings.Contains(string(raw), "git push") {
		t.Fatal("o cerere invalida a schimbat lista")
	}
	if code, _ := e.do(t, "PUT", "/api/settings/commands", []map[string]string{{"prefix": " docker ", "mode": "block"}}); code != http.StatusNoContent {
		t.Fatalf("%d", code)
	}
	e.do(t, "POST", "/api/settings/commands/reset", nil)

	p := e.createProject(t, t.TempDir())
	path := "/api/projects/" + strconv.FormatInt(p.ID, 10) + "/commands"
	e.do(t, "PUT", path, []map[string]string{{"prefix": "git", "mode": "block"}, {"prefix": "git status", "mode": "allow"}})
	_, raw = e.do(t, "GET", path, nil)
	var d struct {
		Effective []store.CommandRule `json:"effective"`
		Warnings  []string            `json:"warnings"`
	}
	json.Unmarshal(raw, &d)
	if len(d.Effective) != 6 || len(d.Warnings) != 1 {
		t.Fatalf("%s", raw)
	}
	// lista goala si null golesc nivelul; proiect necunoscut = 404
	if code, _ := e.do(t, "PUT", path, []map[string]string{}); code != http.StatusNoContent {
		t.Fatalf("lista goala: %d", code)
	}
	if code, _ := e.do(t, "GET", "/api/projects/99999/commands", nil); code != http.StatusNotFound {
		t.Fatalf("proiect necunoscut: %d", code)
	}
	if code, _ := e.do(t, "POST", "/api/access/99999/allow-task", nil); code != http.StatusNotFound {
		t.Fatalf("cerere necunoscuta: %d", code)
	}
}

func TestAccessCommandActionsAPI(t *testing.T) {
	e := setup(t)
	p := e.createProject(t, t.TempDir())
	w := e.addWorker(t, p.ID, "Rook")
	w.Tools = []string{"shell"}
	e.st.UpdateAgent(w)
	e.st.RecordAccessDenial(p.ID, w.ID, nil, "Bash", "git push origin main", "git push")
	e.st.RecordAccessDenial(p.ID, w.ID, nil, "Bash", "ls", "")
	reqs, _ := e.st.ListOpenAccessRequests(p.ID)
	cmdID, toolID := strconv.FormatInt(reqs[0].ID, 10), strconv.FormatInt(reqs[1].ID, 10)
	if code, _ := e.do(t, "POST", "/api/access/"+toolID+"/allow-task", nil); code != http.StatusConflict {
		t.Fatalf("cerere de tool: %d", code)
	}
	if code, raw := e.do(t, "POST", "/api/access/"+cmdID+"/allow-project", nil); code != http.StatusNoContent {
		t.Fatalf("%d %s", code, raw)
	}
	if code, _ := e.do(t, "POST", "/api/access/"+cmdID+"/allow-project", nil); code != http.StatusConflict {
		t.Fatalf("cerere inchisa: %d", code)
	}
	_, raw := e.do(t, "GET", "/api/projects/"+strconv.FormatInt(p.ID, 10), nil)
	if strings.Contains(string(raw), `"prefix":"git push"`) {
		t.Fatal("cererea a ramas in Needs you")
	}
	// agent fara Shell refuzat la sudo (Block): "Enable Shell" nu mai e oferit
	v := e.addWorker(t, p.ID, "Vera")
	e.st.RecordAccessDenial(p.ID, v.ID, nil, "Bash", "sudo ls", "")
	_, raw = e.do(t, "GET", "/api/projects/"+strconv.FormatInt(p.ID, 10), nil)
	if strings.Contains(string(raw), `"canEnableShell":true`) {
		t.Fatalf("Enable Shell oferit pentru o comanda blocata: %s", raw)
	}
}

func TestAccessCommandKindAPI(t *testing.T) {
	e := setup(t)
	p := e.createProject(t, t.TempDir())
	w := e.addWorker(t, p.ID, "Beacon")
	w.Tools = []string{"git"}
	e.st.UpdateAgent(w)
	e.st.RecordCommandRequest(p.ID, w.ID, nil, "git stash push", "git stash")
	reqs, _ := e.st.ListOpenAccessRequests(p.ID)
	id := itoa(reqs[0].ID)
	code, raw := e.do(t, "GET", "/api/projects/"+itoa(p.ID), nil)
	if code != 200 || !strings.Contains(string(raw), `"kind":"command"`) || !strings.Contains(string(raw), `"hasShell":false`) || !strings.Contains(string(raw), `"canEnableShell":true`) {
		t.Fatalf("%d %s", code, raw)
	}
	if code, raw := e.do(t, "POST", "/api/access/"+id+"/allow-project", nil); code != http.StatusConflict || !strings.Contains(string(raw), "no Shell") {
		t.Fatalf("allow-project: %d %s", code, raw)
	}
	if code, raw := e.do(t, "POST", "/api/access/"+id+"/allow-task", nil); code != http.StatusNoContent {
		t.Fatalf("allow-task: %d %s", code, raw)
	}
	if g, _ := e.st.CommandGrants(w.ID, 0); len(g) != 1 || g[0] != "git stash" {
		t.Fatalf("grants = %v", g)
	}
}
