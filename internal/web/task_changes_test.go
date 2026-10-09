package web

import (
	"encoding/json"
	"strings"
	"testing"

	"hobstack/internal/store"
)

type changesResp struct {
	Runs []struct {
		RunID   string `json:"runId"`
		Agent   string `json:"agent"`
		AgentID *int64 `json:"agentId"`
		Files   []struct {
			Path      string       `json:"path"`
			Kind      string       `json:"kind"`
			Added     int          `json:"added"`
			Removed   int          `json:"removed"`
			Truncated bool         `json:"truncated"`
			Hunks     []store.Hunk `json:"hunks"`
		} `json:"files"`
		Commands []struct {
			Text string `json:"text"`
			OK   bool   `json:"ok"`
		} `json:"commands"`
	} `json:"runs"`
}

func TestTaskChangesAPI(t *testing.T) {
	e := setup(t)
	p := e.createProject(t, t.TempDir())
	w := e.addWorker(t, p.ID, "Rook")
	task := store.Task{ProjectID: p.ID, Title: "Login", AssigneeID: w.ID}
	if err := e.st.CreateTask(&task); err != nil {
		t.Fatal(err)
	}
	path := "/api/tasks/" + itoa(task.ID) + "/changes"

	// fara modificari: runs e [] (nu null)
	code, raw := e.do(t, "GET", path, nil)
	if code != 200 || string(raw) != "{\"runs\":[]}\n" && string(raw) != `{"runs":[]}` {
		t.Fatalf("gol: %d %q", code, raw)
	}
	if _, raw := e.do(t, "GET", "/api/projects/"+itoa(p.ID), nil); !contains(raw, `"filesChanged":0`) {
		t.Fatalf("filesChanged implicit 0: %s", raw)
	}

	h := func(lines ...string) []store.Hunk {
		return []store.Hunk{{OldStart: 1, OldLines: 1, NewStart: 1, NewLines: 2, Lines: lines}}
	}
	aid := w.ID
	in := []store.TaskChange{
		{TaskID: task.ID, RunID: "r1", AgentID: &aid, AgentName: "Rook", Kind: "create", Tool: "Write", Path: "a.go", Patch: h("+x", "+y")},
		{TaskID: task.ID, RunID: "r1", AgentID: &aid, AgentName: "Rook", Kind: "edit", Tool: "Edit", Path: "b.go", Patch: h("-a", "+b", " c")},
		{TaskID: task.ID, RunID: "r1", AgentID: &aid, AgentName: "Rook", Kind: "edit", Tool: "Edit", Path: "a.go", Patch: h("-x", "+z"), Truncated: true},
		{TaskID: task.ID, RunID: "r1", AgentID: &aid, AgentName: "Rook", Kind: "command", Tool: "Bash", Command: "go test ./..."},
		{TaskID: task.ID, RunID: "r1", AgentID: &aid, AgentName: "Rook", Kind: "command", Tool: "Bash", Command: "ok-cmd", OK: true},
		{TaskID: task.ID, RunID: "r2", AgentName: "Gone", Kind: "update", Tool: "Write", Path: "a.go", Patch: h("-q", "+w")},
	}
	if err := e.st.AddTaskChanges(in); err != nil {
		t.Fatal(err)
	}
	code, raw = e.do(t, "GET", path, nil)
	var r changesResp
	if err := json.Unmarshal(raw, &r); err != nil || code != 200 || len(r.Runs) != 2 {
		t.Fatalf("runs: %d %s", code, raw)
	}
	r1 := r.Runs[0]
	if r1.RunID != "r1" || r1.Agent != "Rook" || r1.AgentID == nil || *r1.AgentID != w.ID || len(r1.Files) != 2 {
		t.Fatalf("r1: %s", raw)
	}
	a, b := r1.Files[0], r1.Files[1]
	if a.Path != "a.go" || a.Kind != "create" || a.Added != 3 || a.Removed != 1 || !a.Truncated || len(a.Hunks) != 2 {
		t.Fatalf("a.go unit: %+v", a)
	}
	if b.Path != "b.go" || b.Kind != "edit" || b.Added != 1 || b.Removed != 1 {
		t.Fatalf("b.go: %+v", b)
	}
	if len(r1.Commands) != 2 || r1.Commands[0].Text != "go test ./..." || r1.Commands[0].OK || !r1.Commands[1].OK {
		t.Fatalf("comenzi: %v", r1.Commands)
	}
	r2 := r.Runs[1]
	if r2.AgentID != nil || r2.Agent != "Gone" || len(r2.Files) != 1 || r2.Files[0].Kind != "edit" || len(r2.Commands) != 0 {
		t.Fatalf("r2: %s", raw)
	}
	if !contains(raw, `"commands":[]`) {
		t.Fatalf("commands non-nil: %s", raw)
	}

	// GET /api/tasks/{id} poarta changesCount (randuri task_changes: 3 fisiere + 2 comenzi + 1 din r2)
	_, raw = e.do(t, "GET", "/api/tasks/"+itoa(task.ID), nil)
	if !contains(raw, `"changesCount":6`) {
		t.Fatalf("changesCount: %s", raw)
	}

	// filesChanged: caile distincte (a.go, b.go)
	_, raw = e.do(t, "GET", "/api/projects/"+itoa(p.ID), nil)
	var pd struct {
		Tasks []struct {
			ID           int64 `json:"id"`
			FilesChanged int   `json:"filesChanged"`
		} `json:"tasks"`
	}
	json.Unmarshal(raw, &pd)
	if len(pd.Tasks) != 1 || pd.Tasks[0].FilesChanged != 2 {
		t.Fatalf("filesChanged: %s", raw)
	}

	if code, _ := e.do(t, "GET", "/api/tasks/99999/changes", nil); code != 404 {
		t.Fatalf("404: %d", code)
	}
}

func contains(b []byte, s string) bool { return strings.Contains(string(b), s) }
