package web

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"foreman/internal/mcp"
	"foreman/internal/store"
)

func TestAgentLinksValidation(t *testing.T) {
	e := setup(t)
	p := e.createProject(t, t.TempDir())
	root := e.root(t, p.ID)
	path := "/api/projects/" + itoa(p.ID) + "/agents"

	code, raw := e.do(t, "POST", path, map[string]any{"name": "Sub", "model": "haiku", "parentId": root.ID, "responsibilities": "API"})
	var sub store.Agent
	json.Unmarshal(raw, &sub)
	if code != http.StatusCreated || sub.ParentID == nil || *sub.ParentID != root.ID || sub.Responsibilities != "API" {
		t.Fatalf("creare Sub: %d %s", code, raw)
	}
	cycle := map[string]any{"name": "Lead", "model": "sonnet", "parentId": sub.ID}
	if code, raw := e.do(t, "PUT", "/api/agents/"+itoa(root.ID), cycle); code != 400 || !strings.Contains(string(raw), "legatura ar crea un ciclu") {
		t.Fatalf("ciclu: %d %s", code, raw)
	}
	other := e.createProject(t, t.TempDir())
	foreign := map[string]any{"name": "X", "model": "haiku", "parentId": e.root(t, other.ID).ID}
	if code, raw := e.do(t, "POST", path, foreign); code != 400 || !strings.Contains(string(raw), "seful ales nu e in acest proiect") {
		t.Fatalf("sef strain: %d %s", code, raw)
	}
	self := map[string]any{"name": "Sub", "model": "haiku", "parentId": root.ID, "reviewerId": sub.ID}
	if code, raw := e.do(t, "PUT", "/api/agents/"+itoa(sub.ID), self); code != 400 || !strings.Contains(string(raw), "propriul reviewer") {
		t.Fatalf("propriul reviewer: %d %s", code, raw)
	}
	long := map[string]any{"name": "Y", "model": "haiku", "responsibilities": strings.Repeat("x", 2001)}
	if code, _ := e.do(t, "POST", path, long); code != 400 {
		t.Fatalf("responsabilitati prea lungi: %d", code)
	}
	if code, _ := e.do(t, "POST", path, map[string]any{"name": "Solo", "model": "haiku"}); code != 201 {
		t.Fatalf("radacina noua: %d", code)
	}
	d := e.detail(t, p.ID)
	roles := map[string][3]bool{}
	for _, a := range d.Agents {
		roles[a.Name] = [3]bool{a.IsRoot, a.IsLead, a.IsReviewer}
	}
	if roles["Lead"] != [3]bool{true, true, false} || roles["Sub"] != [3]bool{false, false, false} || roles["Solo"] != [3]bool{true, false, false} {
		t.Fatalf("roluri: %+v", roles)
	}
}

// PUT nu accepta projectId sau pozitii din corp: agentul ramane in proiectul lui
// si verificarea "acelasi proiect" se face pe proiectul real.
func TestUpdateAgentIgnoresForeignProjectID(t *testing.T) {
	e := setup(t)
	p := e.createProject(t, t.TempDir())
	other := e.createProject(t, t.TempDir())
	root := e.root(t, p.ID)
	otherRoot := e.root(t, other.ID)
	w := e.addWorker(t, p.ID, "Rook")
	e.st.SetAgentPosition(w.ID, 5, 6)

	body := map[string]any{"name": "Rook", "model": "sonnet", "parentId": otherRoot.ID, "projectId": other.ID, "posX": 900, "posY": 900}
	if code, raw := e.do(t, "PUT", "/api/agents/"+itoa(w.ID), body); code != 400 || !strings.Contains(string(raw), "seful ales nu e in acest proiect") {
		t.Fatalf("sef strain cu projectId fals: %d %s", code, raw)
	}
	body["parentId"] = root.ID
	if code, raw := e.do(t, "PUT", "/api/agents/"+itoa(w.ID), body); code != 200 {
		t.Fatalf("update valid: %d %s", code, raw)
	}
	got, _ := e.st.GetAgent(w.ID)
	if got.ProjectID != p.ID || got.PosX == nil || *got.PosX != 5 || *got.PosY != 6 {
		t.Fatalf("projectId/pozitia s-au schimbat: %+v", got)
	}
}

func TestProjectDetailShape(t *testing.T) {
	e := setup(t)
	p := e.createProject(t, t.TempDir())
	root := e.root(t, p.ID)
	w := e.addWorker(t, p.ID, "Rook")
	e.st.CreateTask(&store.Task{ProjectID: p.ID, Title: "A", AssigneeID: w.ID, CreatorID: &root.ID})
	_, raw := e.do(t, "GET", "/api/projects/"+itoa(p.ID), nil)
	body := string(raw)
	for _, want := range []string{`"parentId":null`, `"reviewerId":null`, `"responsibilities":""`, `"posX":null`,
		`"isRoot":true`, `"isLead":true`, `"isReviewer":false`, `"creatorId":` + itoa(root.ID), `"parentTaskId":null`, `"reviewStage":""`} {
		if !strings.Contains(body, want) {
			t.Errorf("lipseste %s", want)
		}
	}
	if strings.Contains(body, `"position"`) {
		t.Error("position nu mai trebuie sa apara")
	}
	_, raw = e.do(t, "GET", "/api/projects", nil)
	if strings.Contains(string(raw), `"position"`) || !strings.Contains(string(raw), `"isRoot":true`) {
		t.Errorf("lista proiecte: %s", raw)
	}
}

func TestAgentPositionEndpoint(t *testing.T) {
	e := setup(t)
	p := e.createProject(t, t.TempDir())
	root := e.root(t, p.ID)
	path := "/api/agents/" + itoa(root.ID) + "/position"
	if code, raw := e.do(t, "PUT", path, map[string]any{"x": 120.5, "y": 80}); code != http.StatusNoContent {
		t.Fatalf("%d %s", code, raw)
	}
	if a, _ := e.st.GetAgent(root.ID); a.PosX == nil || *a.PosX != 120.5 || *a.PosY != 80 {
		t.Fatalf("pozitie: %+v", a)
	}
	if code, raw := e.do(t, "PUT", path, map[string]any{"x": 1e7, "y": 0}); code != 400 || !strings.Contains(string(raw), "pozitie invalida") {
		t.Fatalf("pozitie mare: %d %s", code, raw)
	}
	if code, _ := e.do(t, "PUT", "/api/agents/9999/position", map[string]any{"x": 1, "y": 1}); code != 404 {
		t.Fatalf("agent inexistent: %d", code)
	}
	req, _ := http.NewRequest("PUT", e.ts.URL+path, strings.NewReader(`{"x":1,"y":1}`))
	req.Header.Set("Content-Type", "text/plain")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusUnsupportedMediaType {
		t.Fatalf("text/plain: %d, vreau 415", res.StatusCode)
	}
}

func TestPromptPreviewUsesLinks(t *testing.T) {
	e := setup(t)
	p := e.createProject(t, t.TempDir())
	root := e.root(t, p.ID)
	rook := e.addWorker(t, p.ID, "Rook") // Lead devine lider
	preview := func(body map[string]any) (int, string) {
		body["projectId"], body["name"] = p.ID, "Nova"
		code, raw := e.do(t, "POST", "/api/prompt-preview", body)
		return code, string(raw)
	}
	if code, got := preview(map[string]any{"parentId": root.ID}); code != 200 || !strings.Contains(got, "Raportezi lui Lead.") {
		t.Errorf("agent nou sub Lead: %d %s", code, got)
	}
	if code, got := preview(map[string]any{"parentId": rook.ID}); code != 200 || !strings.Contains(got, "Raportezi lui Rook.") {
		t.Errorf("agent nou sub Rook: %d %s", code, got)
	}
	if code, got := preview(map[string]any{"parentId": nil}); code != 200 || !strings.Contains(got, "Raportezi utilizatorului.") {
		t.Errorf("agent nou radacina: %d %s", code, got)
	}
	if code, got := preview(map[string]any{"agentId": root.ID, "parentId": nil}); code != 200 || !strings.Contains(got, "mcp__team__create_task") {
		t.Errorf("Lead are echipa: %d %s", code, got)
	}
	other := e.createProject(t, t.TempDir())
	if code, got := preview(map[string]any{"parentId": e.root(t, other.ID).ID}); code != 400 || !strings.Contains(got, "seful ales nu e in acest proiect") {
		t.Errorf("sef din alt proiect: %d %s", code, got)
	}
	if code, got := preview(map[string]any{"parentId": 99999}); code != 400 {
		t.Errorf("sef inexistent: %d %s", code, got)
	}
}

func TestDeleteAgentReparentsViaAPI(t *testing.T) {
	e := setup(t)
	p := e.createProject(t, t.TempDir())
	root := e.root(t, p.ID)
	mid := e.addWorker(t, p.ID, "Mid")
	leaf := store.Agent{ProjectID: p.ID, Name: "Leaf", Model: "haiku", ParentID: &mid.ID}
	e.st.CreateAgent(&leaf)
	if code, _ := e.do(t, "DELETE", "/api/agents/"+itoa(mid.ID), nil); code != http.StatusNoContent {
		t.Fatalf("stergere: %d", code)
	}
	if a, _ := e.st.GetAgent(leaf.ID); a.ParentID == nil || *a.ParentID != root.ID {
		t.Fatalf("Leaf nu a trecut la Lead: %+v", a.ParentID)
	}
}

// Cand asignatul isi pierde reviewer-ul (legatura stearsa sau reviewer sters),
// task-ul din etapa reviewer trece la lider, care e anuntat si poate decide.
func TestReviewerLossMovesTaskToLead(t *testing.T) {
	for _, how := range []string{"unlink", "delete"} {
		t.Run(how, func(t *testing.T) {
			e := setup(t)
			p := e.createProject(t, t.TempDir())
			root := e.root(t, p.ID)
			rook := e.addWorker(t, p.ID, "Rook")
			vera := e.addWorker(t, p.ID, "Vera")
			rook.ReviewerID = &vera.ID
			e.st.UpdateAgent(rook)
			task := store.Task{ProjectID: p.ID, Title: "A", AssigneeID: rook.ID, CreatorID: &root.ID}
			e.st.CreateTask(&task)
			task.Status, task.ReviewStage, task.Result = store.StatusReview, store.StageReviewer, "A e gata"
			e.st.SaveTask(&task)

			if how == "unlink" {
				body := map[string]any{"name": "Rook", "model": "sonnet", "parentId": root.ID, "reviewerId": nil}
				if code, raw := e.do(t, "PUT", "/api/agents/"+itoa(rook.ID), body); code != 200 {
					t.Fatalf("PUT: %d %s", code, raw)
				}
			} else if code, raw := e.do(t, "DELETE", "/api/agents/"+itoa(vera.ID), nil); code != http.StatusNoContent {
				t.Fatalf("DELETE: %d %s", code, raw)
			}
			got, _ := e.st.GetTask(task.ID)
			if got.Status != store.StatusReview || got.ReviewStage != store.StageLead {
				t.Fatalf("task: %s/%s", got.Status, got.ReviewStage)
			}
			pending, _ := e.st.Undelivered(root.ID)
			if len(pending) != 1 || !strings.Contains(pending[0].Text, "Rook a terminat T-1") {
				t.Fatalf("Lead: %+v", pending)
			}
			if _, err := e.tm.AcceptTask(mcp.Caller{ProjectID: p.ID, AgentID: root.ID, IsLead: true}, "T-1", ""); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// Reviewer schimbat pe canvas: reviewer-ul nou e anuntat si poate aproba.
func TestNewReviewerGetsPendingReview(t *testing.T) {
	e := setup(t)
	p := e.createProject(t, t.TempDir())
	root := e.root(t, p.ID)
	rook := e.addWorker(t, p.ID, "Rook")
	vera := e.addWorker(t, p.ID, "Vera")
	bea := e.addWorker(t, p.ID, "Bea")
	rook.ReviewerID = &vera.ID
	e.st.UpdateAgent(rook)
	task := store.Task{ProjectID: p.ID, Title: "A", AssigneeID: rook.ID, CreatorID: &root.ID}
	e.st.CreateTask(&task)
	task.Status, task.ReviewStage = store.StatusReview, store.StageReviewer
	e.st.SaveTask(&task)

	body := map[string]any{"name": "Rook", "model": "sonnet", "parentId": root.ID, "reviewerId": bea.ID}
	if code, raw := e.do(t, "PUT", "/api/agents/"+itoa(rook.ID), body); code != 200 {
		t.Fatalf("PUT: %d %s", code, raw)
	}
	pending, _ := e.st.Undelivered(bea.ID)
	if len(pending) != 1 || !strings.Contains(pending[0].Text, "asteapta review-ul tau") {
		t.Fatalf("Bea: %+v", pending)
	}
	if _, err := e.tm.ReviewApprove(mcp.Caller{ProjectID: p.ID, AgentID: bea.ID, HasParent: true, IsReviewer: true}, "T-1", ""); err != nil {
		t.Fatal(err)
	}
}

// Un agent nelegat nu e radacina, nu primeste mesaje si devine legat cand
// primeste un sef sau cand e legat de utilizator. Copiii unui agent fara sef
// raman nelegati cand acesta e sters.
func TestDetachedAgent(t *testing.T) {
	e := setup(t)
	p := e.createProject(t, t.TempDir())
	root := e.root(t, p.ID)
	code, raw := e.do(t, "POST", "/api/projects/"+itoa(p.ID)+"/agents", map[string]any{"name": "Banca", "model": "haiku", "detached": true})
	var banca store.Agent
	json.Unmarshal(raw, &banca)
	if code != http.StatusCreated || !banca.Detached {
		t.Fatalf("creare nelegat: %d %s", code, raw)
	}
	for _, a := range e.detail(t, p.ID).Agents {
		if a.Name == "Banca" && a.IsRoot {
			t.Fatal("un agent nelegat nu e radacina")
		}
	}
	if code, raw := e.do(t, "POST", "/api/agents/"+itoa(banca.ID)+"/messages", map[string]any{"text": "salut"}); code != 400 || !strings.Contains(string(raw), "nu e legat") {
		t.Fatalf("mesaj catre nelegat: %d %s", code, raw)
	}

	// cu sef, "detached" din corp se ignora
	e.do(t, "PUT", "/api/agents/"+itoa(banca.ID), map[string]any{"name": "Banca", "model": "haiku", "parentId": root.ID, "detached": true})
	if a, _ := e.st.GetAgent(banca.ID); a.Detached || a.ParentID == nil {
		t.Fatalf("cu sef trebuie sa fie legat: %+v", a)
	}

	// fara sef si fara detached: radacina
	e.do(t, "PUT", "/api/agents/"+itoa(banca.ID), map[string]any{"name": "Banca", "model": "haiku"})
	if a, _ := e.st.GetAgent(banca.ID); a.Detached || a.ParentID != nil {
		t.Fatalf("trebuia sa fie radacina: %+v", a)
	}

	// stergerea unei radacini lasa copiii nelegati
	copil := store.Agent{ProjectID: p.ID, Name: "Copil", Model: "haiku", ParentID: &banca.ID}
	if err := e.st.CreateAgent(&copil); err != nil {
		t.Fatal(err)
	}
	if code, _ := e.do(t, "DELETE", "/api/agents/"+itoa(banca.ID), nil); code != http.StatusNoContent {
		t.Fatalf("stergere: %d", code)
	}
	if a, _ := e.st.GetAgent(copil.ID); !a.Detached || a.ParentID != nil {
		t.Fatalf("Copil trebuia sa ramana nelegat: %+v", a)
	}
}
