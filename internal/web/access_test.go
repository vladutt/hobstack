package web

import (
	"encoding/json"
	"slices"
	"testing"

	"hobstack/internal/store"
)

func TestAccessInResponses(t *testing.T) {
	e := setup(t)
	p := e.createProject(t, t.TempDir())
	w := e.addWorker(t, p.ID, "Beacon")
	task := store.Task{ProjectID: p.ID, Title: "A", AssigneeID: w.ID}
	if err := e.st.CreateTask(&task); err != nil {
		t.Fatal(err)
	}
	e.st.RecordAccessDenial(p.ID, w.ID, &task.ID, "Bash", "npm test", "")
	e.st.RecordAccessDenial(p.ID, w.ID, &task.ID, "WebFetch", "https://x.dev", "")

	_, raw := e.do(t, "GET", "/api/projects/"+itoa(p.ID), nil)
	var d struct {
		AccessRequests int `json:"accessRequests"`
		Access         []struct {
			ID             int64  `json:"id"`
			AgentID        int64  `json:"agentId"`
			AgentName      string `json:"agentName"`
			TaskID         *int64 `json:"taskId"`
			Ref            string `json:"ref"`
			Tool           string `json:"tool"`
			Detail         string `json:"detail"`
			Count          int    `json:"count"`
			CanEnableShell bool   `json:"canEnableShell"`
		} `json:"access"`
	}
	json.Unmarshal(raw, &d)
	if d.AccessRequests != 2 || len(d.Access) != 2 {
		t.Fatalf("%s", raw)
	}
	a := d.Access[0]
	if a.AgentName != "Beacon" || a.Ref != task.Ref() || a.Tool != "Bash" || a.Detail != "npm test" || !a.CanEnableShell || a.TaskID == nil {
		t.Fatalf("%+v", a)
	}
	if d.Access[1].CanEnableShell {
		t.Fatalf("WebFetch nu are Enable Shell: %+v", d.Access[1])
	}
	_, raw = e.do(t, "GET", "/api/projects", nil)
	var cards []struct {
		AccessRequests int `json:"accessRequests"`
	}
	json.Unmarshal(raw, &cards)
	if len(cards) != 1 || cards[0].AccessRequests != 2 {
		t.Fatalf("%s", raw)
	}
	// fara cereri: lista e [] (nu null)
	p2 := e.createProject(t, t.TempDir())
	_, raw = e.do(t, "GET", "/api/projects/"+itoa(p2.ID), nil)
	var m map[string]json.RawMessage
	json.Unmarshal(raw, &m)
	if string(m["access"]) != "[]" {
		t.Fatalf("access: %s", m["access"])
	}
}

func TestAccessDismiss(t *testing.T) {
	e := setup(t)
	p := e.createProject(t, t.TempDir())
	w := e.addWorker(t, p.ID, "Beacon")
	e.st.RecordAccessDenial(p.ID, w.ID, nil, "Bash", "ls", "")
	reqs, _ := e.st.ListOpenAccessRequests(p.ID)
	if code, _ := e.do(t, "POST", "/api/access/"+itoa(reqs[0].ID)+"/dismiss", nil); code != 204 {
		t.Fatalf("dismiss: %d", code)
	}
	if n, _ := e.st.CountOpenAccessRequests(p.ID); n != 0 {
		t.Fatalf("a ramas deschisa: %d", n)
	}
	if code, _ := e.do(t, "POST", "/api/access/99999/dismiss", nil); code != 404 {
		t.Fatalf("inexistenta: %d", code)
	}
	if code, _ := e.do(t, "POST", "/api/access/99999/enable-shell", nil); code != 404 {
		t.Fatalf("enable-shell inexistenta: %d", code)
	}
}

func TestAccessEnableShell(t *testing.T) {
	e := setup(t)
	p := e.createProject(t, t.TempDir())
	w := e.addWorker(t, p.ID, "Beacon")
	w.Tools, w.Role = []string{"write"}, "Builder"
	if err := e.st.UpdateAgent(w); err != nil {
		t.Fatal(err)
	}
	e.st.RecordAccessDenial(p.ID, w.ID, nil, "Bash", "npm test", "")
	e.st.RecordAccessDenial(p.ID, w.ID, nil, "WebFetch", "https://x.dev", "")
	reqs, _ := e.st.ListOpenAccessRequests(p.ID)

	// alt tool decat Bash: 409, nimic schimbat
	if code, _ := e.do(t, "POST", "/api/access/"+itoa(reqs[1].ID)+"/enable-shell", nil); code != 409 {
		t.Fatalf("WebFetch: %d", code)
	}
	if code, raw := e.do(t, "POST", "/api/access/"+itoa(reqs[0].ID)+"/enable-shell", nil); code != 204 {
		t.Fatalf("enable-shell: %d %s", code, raw)
	}
	got, _ := e.st.GetAgent(w.ID)
	if !slices.Contains(got.Tools, "shell") || !slices.Contains(got.Tools, "write") || got.Role != "Builder" || got.Name != "Beacon" {
		t.Fatalf("agent: %+v", got)
	}
	open, _ := e.st.ListOpenAccessRequests(p.ID)
	if len(open) != 1 || open[0].Tool != "WebFetch" {
		t.Fatalf("deschise: %+v", open)
	}
	// a doua oara pe acelasi rand: tool-ul nu se dubleaza
	e.st.RecordAccessDenial(p.ID, w.ID, nil, "Bash", "make", "")
	open, _ = e.st.ListOpenAccessRequests(p.ID)
	if code, _ := e.do(t, "POST", "/api/access/"+itoa(open[1].ID)+"/enable-shell", nil); code != 204 {
		t.Fatalf("a doua oara: %d", code)
	}
	got, _ = e.st.GetAgent(w.ID)
	if n := len(got.Tools); n != 2 {
		t.Fatalf("tools duplicate: %v", got.Tools)
	}
}

func TestAccessEnablePackages(t *testing.T) {
	e := setup(t)
	p := e.createProject(t, t.TempDir())
	w := e.addWorker(t, p.ID, "Beacon")
	w.Tools = []string{"write", "shell"}
	if err := e.st.UpdateAgent(w); err != nil {
		t.Fatal(err)
	}
	e.st.RecordAccessDenial(p.ID, w.ID, nil, "Bash", "cd app && npm install 2>&1 | tail -8", "")
	_, raw := e.do(t, "GET", "/api/projects/"+itoa(p.ID), nil)
	var d struct {
		Access []struct {
			ID                int64 `json:"id"`
			CanEnableShell    bool  `json:"canEnableShell"`
			CanEnablePackages bool  `json:"canEnablePackages"`
		} `json:"access"`
	}
	json.Unmarshal(raw, &d)
	if len(d.Access) != 1 || d.Access[0].CanEnableShell || !d.Access[0].CanEnablePackages {
		t.Fatalf("%s", raw)
	}
	if code, raw := e.do(t, "POST", "/api/access/"+itoa(d.Access[0].ID)+"/enable-packages", nil); code != 204 {
		t.Fatalf("enable-packages: %d %s", code, raw)
	}
	got, _ := e.st.GetAgent(w.ID)
	if !slices.Equal(got.Tools, []string{"write", "shell", "packages"}) {
		t.Fatalf("tools: %v", got.Tools)
	}
	if n, _ := e.st.CountOpenAccessRequests(p.ID); n != 0 {
		t.Fatalf("a ramas deschisa: %d", n)
	}
}

func TestUpdateProjectKeepsAutonomy(t *testing.T) {
	e := setup(t)
	p := e.createProject(t, t.TempDir())
	body := func(autonomy any) map[string]any {
		m := map[string]any{"name": "ApiUnit", "goal": "Import", "folders": []map[string]any{{"id": p.Folders[0].ID, "path": p.Folders[0].Path}}}
		if autonomy != nil {
			m["autonomy"] = autonomy
		}
		return m
	}
	put := func(b map[string]any) store.Project {
		code, raw := e.do(t, "PUT", "/api/projects/"+itoa(p.ID), b)
		if code != 200 {
			t.Fatalf("PUT: %d %s", code, raw)
		}
		var out store.Project
		json.Unmarshal(raw, &out)
		return out
	}
	if !put(body(true)).Autonomy {
		t.Fatal("autonomy nu s-a salvat")
	}
	// un PUT fara camp (alt client) nu il reseteaza
	if !put(body(nil)).Autonomy {
		t.Fatal("PUT fara autonomy a resetat-o")
	}
	if put(body(false)).Autonomy {
		t.Fatal("autonomy false nu s-a salvat")
	}
}

// o comanda din refuzurile fixe (git push...) nu primeste Enable Shell: n-ar ajuta
func TestAccessNoEnableShellForAlwaysDenied(t *testing.T) {
	e := setup(t)
	p := e.createProject(t, t.TempDir())
	w := e.addWorker(t, p.ID, "Beacon")
	e.st.RecordAccessDenial(p.ID, w.ID, nil, "Bash", "git push origin main", "")
	_, raw := e.do(t, "GET", "/api/projects/"+itoa(p.ID), nil)
	var d struct {
		Access []struct {
			CanEnableShell bool `json:"canEnableShell"`
		} `json:"access"`
	}
	json.Unmarshal(raw, &d)
	if len(d.Access) != 1 || d.Access[0].CanEnableShell {
		t.Fatalf("%s", raw)
	}
}

// userCanDecide: ce poate decide utilizatorul din board, si in modul autonom
// (unde userDecides nu mai include task-urile decise de radacina)
func TestDetailUserCanDecide(t *testing.T) {
	for _, auto := range []bool{false, true} {
		e := setup(t)
		p := e.createProject(t, t.TempDir())
		w := e.addWorker(t, p.ID, "Beacon")
		if auto {
			got, _ := e.st.GetProject(p.ID)
			got.Autonomy = true
			if err := e.st.UpdateProject(got); err != nil {
				t.Fatal(err)
			}
		}
		path := "/api/projects/" + itoa(p.ID)
		_, raw := e.do(t, "POST", path+"/tasks", map[string]any{"title": "A", "assigneeId": w.ID})
		var task store.Task
		json.Unmarshal(raw, &task)
		task.Status, task.ReviewStage = store.StatusReview, store.StageLead
		if err := e.st.SaveTask(&task); err != nil {
			t.Fatal(err)
		}
		var d struct {
			UserDecides   []int64 `json:"userDecides"`
			UserCanDecide []int64 `json:"userCanDecide"`
		}
		_, raw = e.do(t, "GET", path, nil)
		json.Unmarshal(raw, &d)
		if !slices.Equal(d.UserCanDecide, []int64{task.ID}) {
			t.Fatalf("autonomy=%v userCanDecide: %s", auto, raw)
		}
		want := []int64{task.ID}
		if auto {
			want = []int64{} // decide radacina
		}
		if !slices.Equal(d.UserDecides, want) {
			t.Fatalf("autonomy=%v userDecides: %s", auto, raw)
		}
	}
}
