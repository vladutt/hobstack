package web

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"foreman/internal/events"
	"foreman/internal/mcp"
	"foreman/internal/runner"
	"foreman/internal/store"
	"foreman/internal/team"
)

// nopStarter: run-urile "pornesc" dar nu se termina niciodata.
type nopStarter struct{}

func (nopStarter) Start(runner.Spec, func(runner.Outcome)) (*runner.Run, error) { return nil, nil }
func (nopStarter) Stop(string) error                                            { return nil }

type env struct {
	st       *store.Store
	tm       *team.Team
	ts       *httptest.Server
	dirs     Dirs
	pickPath string // ce intoarce picker-ul fals
	pickErr  error
	srv      *Server
}

func setup(t *testing.T) *env {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	hub := events.NewHub()
	tm := team.New(st, nopStarter{}, hub, "http://127.0.0.1:0")
	dirs := Dirs{Projects: t.TempDir(), Workspaces: t.TempDir()}
	tm.SetFilesRoot(dirs.Projects)
	e := &env{st: st, tm: tm, dirs: dirs}
	pick := func(context.Context) (string, error) { return e.pickPath, e.pickErr }
	srv := New(st, tm, runner.NewManager("claude-inexistent", t.TempDir()), hub, mcp.NewServer(tm), dirs, pick, true)
	e.srv = srv
	e.ts = httptest.NewServer(srv.Handler())
	t.Cleanup(e.ts.Close)
	return e
}

func (e *env) do(t *testing.T, method, path string, body any) (int, []byte) {
	t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, e.ts.URL+path, rd)
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	return res.StatusCode, raw
}

func (e *env) createProject(t *testing.T, workdir string) store.Project {
	t.Helper()
	code, raw := e.do(t, "POST", "/api/projects", map[string]any{"name": "ApiUnit", "goal": "Import", "workdir": workdir})
	if code != http.StatusCreated {
		t.Fatalf("creare proiect: %d %s", code, raw)
	}
	var p store.Project
	json.Unmarshal(raw, &p)
	return p
}

func (e *env) addWorker(t *testing.T, projectID int64, name string) store.Agent {
	t.Helper()
	agents, err := e.st.ListAgents(projectID)
	if err != nil || len(agents) == 0 {
		t.Fatalf("proiect fara agenti: %v", err)
	}
	a := store.Agent{ProjectID: projectID, Name: name, ParentID: &agents[0].ID, Model: "sonnet"}
	if err := e.st.CreateAgent(&a); err != nil {
		t.Fatal(err)
	}
	return a
}

type detail struct {
	Status string `json:"status"`
	Paused bool   `json:"paused"`
	Done   int    `json:"done"`
	Total  int    `json:"total"`
	Team   []struct {
		Name   string
		IsLead bool `json:"isLead"`
		IsRoot bool `json:"isRoot"`
	} `json:"team"`
	Agents []struct {
		ID               int64  `json:"id"`
		Name             string `json:"name"`
		Status           string `json:"status"`
		ActiveRunID      string `json:"activeRunId"`
		ParentID         *int64 `json:"parentId"`
		ReviewerID       *int64 `json:"reviewerId"`
		Responsibilities string
		IsLead           bool `json:"isLead"`
		IsRoot           bool `json:"isRoot"`
		IsReviewer       bool `json:"isReviewer"`
	} `json:"agents"`
	Tasks []store.Task `json:"tasks"`
}

// root intoarce prima radacina a proiectului (Lead-ul creat odata cu el).
func (e *env) root(t *testing.T, projectID int64) store.Agent {
	t.Helper()
	agents, err := e.st.ListAgents(projectID)
	if err != nil || len(agents) == 0 {
		t.Fatalf("proiect fara agenti: %v", err)
	}
	return agents[0]
}

func (e *env) detail(t *testing.T, id int64) detail {
	t.Helper()
	code, raw := e.do(t, "GET", "/api/projects/"+itoa(id), nil)
	if code != 200 {
		t.Fatalf("GET proiect: %d %s", code, raw)
	}
	var d detail
	json.Unmarshal(raw, &d)
	return d
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

func TestInfoAndStatic(t *testing.T) {
	e := setup(t)
	if code, raw := e.do(t, "GET", "/api/info", nil); code != 200 || !strings.Contains(string(raw), `"name":"foreman"`) {
		t.Fatalf("%d %s", code, raw)
	}
	if code, raw := e.do(t, "GET", "/", nil); code != 200 || !strings.Contains(string(raw), "<title>") {
		t.Fatalf("index: %d", code)
	}
}

func TestCreateProjectValidation(t *testing.T) {
	e := setup(t)
	cases := []map[string]any{
		{"name": "", "workdir": t.TempDir()},
		{"name": "X", "workdir": "relativ/folder"},
		{"name": "X", "workdir": existingFile(t)},
		{"name": "X", "workdir": t.TempDir(), "maxParallel": -1},
		{"name": "X", "workdir": t.TempDir(), "budgetUsd": 0.01},
		{"name": "X", "workdir": t.TempDir(), "budgetUsd": -1},
	}
	for _, body := range cases {
		if code, raw := e.do(t, "POST", "/api/projects", body); code != http.StatusBadRequest || !strings.Contains(string(raw), `"error"`) {
			t.Errorf("%v: %d %s", body, code, raw)
		}
	}
}

func TestCreateProjectWorkdirWithSpaces(t *testing.T) {
	e := setup(t)
	dir := filepath.Join(t.TempDir(), "AI Dashboard")
	os.Mkdir(dir, 0o755)
	if p := e.createProject(t, dir); p.Workdir != dir {
		t.Fatalf("workdir = %q", p.Workdir)
	}
}

func TestProjectFieldLimits(t *testing.T) {
	e := setup(t)
	body := map[string]any{"name": "X", "workdir": t.TempDir(), "context": strings.Repeat("a", 20001)}
	if code, _ := e.do(t, "POST", "/api/projects", body); code != http.StatusBadRequest {
		t.Fatalf("context prea lung: %d", code)
	}
}

func TestCreateProjectCreatesLead(t *testing.T) {
	e := setup(t)
	p := e.createProject(t, t.TempDir())
	d := e.detail(t, p.ID)
	if d.Status != "Idle" || len(d.Agents) != 1 || !d.Agents[0].IsRoot || d.Agents[0].IsLead || d.Agents[0].ParentID != nil || d.Agents[0].Status != "Idle" || len(d.Tasks) != 0 {
		t.Fatalf("detaliu = %+v", d)
	}
}

func TestProjectsListProgress(t *testing.T) {
	e := setup(t)
	p := e.createProject(t, t.TempDir())
	w := e.addWorker(t, p.ID, "Rook")
	a := store.Task{ProjectID: p.ID, Title: "A", AssigneeID: w.ID}
	b := store.Task{ProjectID: p.ID, Title: "B", AssigneeID: w.ID}
	e.st.CreateTask(&a)
	e.st.CreateTask(&b)
	a.Status = store.StatusDone
	e.st.SaveTask(&a)

	code, raw := e.do(t, "GET", "/api/projects", nil)
	var list []detail
	json.Unmarshal(raw, &list)
	if code != 200 || len(list) != 1 || list[0].Done != 1 || list[0].Total != 2 || list[0].Status != "Running" || len(list[0].Team) != 2 {
		t.Fatalf("%d %s", code, raw)
	}
}

func TestAgentCRUD(t *testing.T) {
	e := setup(t)
	p := e.createProject(t, t.TempDir())
	root := e.root(t, p.ID)
	path := "/api/projects/" + itoa(p.ID) + "/agents"
	good := map[string]any{"name": "Rook", "role": "Backend", "model": "sonnet", "parentId": root.ID, "tools": []string{"write", "write", "git"}}

	code, raw := e.do(t, "POST", path, good)
	var a store.Agent
	json.Unmarshal(raw, &a)
	if code != 201 || a.ParentID == nil || *a.ParentID != root.ID || len(a.Tools) != 2 {
		t.Fatalf("creare: %d %s", code, raw)
	}
	if code, _ := e.do(t, "POST", path, map[string]any{"name": "rook", "model": "sonnet"}); code != http.StatusConflict {
		t.Errorf("nume duplicat: %d", code)
	}
	if code, _ := e.do(t, "POST", path, map[string]any{"name": "X", "model": "gpt"}); code != http.StatusBadRequest {
		t.Errorf("model gresit: %d", code)
	}
	if code, _ := e.do(t, "POST", path, map[string]any{"name": "X", "model": "haiku", "tools": []string{"root"}}); code != http.StatusBadRequest {
		t.Errorf("tool gresit: %d", code)
	}
	good["role"] = "QA"
	if code, raw := e.do(t, "PUT", "/api/agents/"+itoa(a.ID), good); code != 200 || !strings.Contains(string(raw), `"role":"QA"`) {
		t.Errorf("update: %d %s", code, raw)
	}
	if code, _ := e.do(t, "DELETE", "/api/agents/"+itoa(a.ID), nil); code != http.StatusNoContent {
		t.Errorf("stergere: %d", code)
	}
}

func TestDeleteAgentConflicts(t *testing.T) {
	e := setup(t)
	p := e.createProject(t, t.TempDir())
	w := e.addWorker(t, p.ID, "Rook")
	e.st.CreateTask(&store.Task{ProjectID: p.ID, Title: "A", AssigneeID: w.ID})
	if code, _ := e.do(t, "DELETE", "/api/agents/"+itoa(w.ID), nil); code != http.StatusConflict {
		t.Errorf("worker cu task-uri: %d", code)
	}
	// ultimul agent din proiect nu se poate sterge
	solo := e.createProject(t, t.TempDir())
	lead := e.root(t, solo.ID)
	if code, _ := e.do(t, "DELETE", "/api/agents/"+itoa(lead.ID), nil); code != http.StatusConflict {
		t.Errorf("ultimul agent: %d", code)
	}
}

func TestPromptPreview(t *testing.T) {
	e := setup(t)
	p := e.createProject(t, t.TempDir())
	root := e.root(t, p.ID)
	code, raw := e.do(t, "POST", "/api/prompt-preview", map[string]any{
		"projectId": p.ID, "name": "Nova", "role": "Frontend Developer", "parentId": root.ID, "background": "Vue 3",
	})
	if code != 200 || !strings.Contains(string(raw), "Esti Nova, Frontend Developer, in proiectul ApiUnit. Raportezi lui Lead.") {
		t.Fatalf("%d %s", code, raw)
	}
}

func TestPostMessageToWorker(t *testing.T) {
	e := setup(t)
	p := e.createProject(t, t.TempDir())
	w := e.addWorker(t, p.ID, "Rook")
	if code, _ := e.do(t, "POST", "/api/agents/"+itoa(w.ID)+"/messages", map[string]any{"text": "  "}); code != http.StatusBadRequest {
		t.Errorf("mesaj gol: %d", code)
	}
	code, raw := e.do(t, "POST", "/api/agents/"+itoa(w.ID)+"/messages", map[string]any{"text": "salut"})
	if code != 201 || !strings.Contains(string(raw), `"delivered":false`) {
		t.Fatalf("%d %s", code, raw)
	}
	lead := e.root(t, p.ID)
	_, raw = e.do(t, "GET", "/api/agents/"+itoa(lead.ID)+"/messages", nil)
	var msgs []store.Message
	json.Unmarshal(raw, &msgs)
	if len(msgs) != 1 || msgs[0].Text != "Utilizatorul -> Rook: salut" || msgs[0].Delivered {
		t.Fatalf("copia pentru lead lipseste: %s", raw)
	}
}

func TestStopResumeAndBlocked(t *testing.T) {
	e := setup(t)
	p := e.createProject(t, t.TempDir())
	id := itoa(p.ID)
	if code, _ := e.do(t, "POST", "/api/projects/"+id+"/stop", nil); code != http.StatusNoContent {
		t.Fatalf("stop: %d", code)
	}
	if d := e.detail(t, p.ID); !d.Paused || d.Status != "Paused" {
		t.Fatalf("dupa stop: %+v", d)
	}
	e.do(t, "POST", "/api/projects/"+id+"/resume", nil)
	if d := e.detail(t, p.ID); d.Paused {
		t.Fatal("dupa resume inca e pe pauza")
	}
	w := e.addWorker(t, p.ID, "Rook")
	task := store.Task{ProjectID: p.ID, Title: "A", AssigneeID: w.ID}
	e.st.CreateTask(&task)
	if code, _ := e.do(t, "POST", "/api/tasks/"+itoa(task.ID)+"/accept", nil); code != http.StatusConflict {
		t.Errorf("accept pe task neblocat: %d", code)
	}
}

func TestDeleteActiveProject409(t *testing.T) {
	e := setup(t)
	p := e.createProject(t, t.TempDir())
	lead := e.root(t, p.ID)
	e.tm.UserMessage(lead.ID, "porneste", nil)
	if err := e.tm.Reconcile(p.ID); err != nil {
		t.Fatal(err)
	}
	if code, _ := e.do(t, "DELETE", "/api/projects/"+itoa(p.ID), nil); code != http.StatusConflict {
		t.Fatalf("stergere cu echipa activa: %d", code)
	}
	if d := e.detail(t, p.ID); d.Agents[0].ActiveRunID == "" || d.Agents[0].Status != "Working" {
		t.Fatalf("lead-ul trebuia sa apara activ: %+v", d.Agents[0])
	}
}

func TestMCPMounted(t *testing.T) {
	e := setup(t)
	if code, _ := e.do(t, "POST", "/mcp/token-necunoscut", map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/list"}); code != http.StatusNotFound {
		t.Fatalf("mcp: %d", code)
	}
}

func TestBlocksCrossSite(t *testing.T) {
	e := setup(t)
	p := e.createProject(t, t.TempDir())

	req, _ := http.NewRequest("POST", e.ts.URL+"/api/projects", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "text/plain")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusUnsupportedMediaType {
		t.Errorf("text/plain: %d", res.StatusCode)
	}

	req, _ = http.NewRequest("GET", e.ts.URL+"/api/info", nil)
	req.Host = "evil.example"
	res, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusForbidden {
		t.Errorf("host strain: %d", res.StatusCode)
	}

	if code, _ := e.do(t, "POST", "/api/projects/"+itoa(p.ID)+"/stop", nil); code != http.StatusNoContent {
		t.Errorf("stop fara body: %d", code)
	}
}

func TestProjectLimitsOptional(t *testing.T) {
	e := setup(t)
	p := e.createProject(t, t.TempDir()) // fara maxParallel si budgetUsd
	if p.MaxParallel != 0 || p.BudgetUSD != 0 {
		t.Fatalf("limitele trebuiau sa ramana 0 (fara limita): %+v", p)
	}
	body := map[string]any{"name": "Y", "workdir": t.TempDir(), "maxParallel": 20, "budgetUsd": 2.5}
	code, raw := e.do(t, "POST", "/api/projects", body)
	if code != http.StatusCreated || !strings.Contains(string(raw), `"maxParallel":20`) || !strings.Contains(string(raw), `"budgetUsd":2.5`) {
		t.Fatalf("limite setate explicit: %d %s", code, raw)
	}
}

// existingFile intoarce o cale care exista, dar e fisier (nu folder).
func existingFile(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "fisier.txt")
	if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestModelsInInfoAndPinnedVersion(t *testing.T) {
	e := setup(t)
	_, raw := e.do(t, "GET", "/api/info", nil)
	if !strings.Contains(string(raw), `"claude-opus-4-8"`) || !strings.Contains(string(raw), `"fable"`) {
		t.Fatalf("info fara lista de modele: %s", raw)
	}
	p := e.createProject(t, t.TempDir())
	path := "/api/projects/" + itoa(p.ID) + "/agents"
	if code, raw := e.do(t, "POST", path, map[string]any{"name": "Fix", "model": "claude-opus-4-8"}); code != http.StatusCreated {
		t.Fatalf("versiune fixa: %d %s", code, raw)
	}
	if code, _ := e.do(t, "POST", path, map[string]any{"name": "Fab", "model": "fable"}); code != http.StatusCreated {
		t.Fatalf("alias fable: %d", code)
	}
}
