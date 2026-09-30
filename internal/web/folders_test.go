package web

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"foreman/internal/store"
)

type folderDetail struct {
	Workdir string         `json:"workdir"`
	Folders []store.Folder `json:"folders"`
	Agents  []struct {
		ID       int64  `json:"id"`
		FolderID *int64 `json:"folderId"`
	} `json:"agents"`
}

func (e *env) folders(t *testing.T, id int64) folderDetail {
	t.Helper()
	_, raw := e.do(t, "GET", "/api/projects/"+itoa(id), nil)
	var d folderDetail
	json.Unmarshal(raw, &d)
	return d
}

func TestProjectFolders(t *testing.T) {
	e := setup(t)
	client, landing := t.TempDir(), filepath.Join(t.TempDir(), "probe-landing")
	code, raw := e.do(t, "POST", "/api/projects", map[string]any{"name": "Probe", "folders": []map[string]any{
		{"name": "Client", "path": client, "context": "Go"},
		{"name": "", "path": landing}, // fara nume: numele folderului; inexistent: se creeaza
	}})
	if code != http.StatusCreated {
		t.Fatalf("%d %s", code, raw)
	}
	var p store.Project
	json.Unmarshal(raw, &p)
	d := e.folders(t, p.ID)
	if len(d.Folders) != 2 || d.Folders[1].Name != "probe-landing" || d.Workdir != client || d.Folders[0].Context != "Go" {
		t.Fatalf("foldere: %+v", d)
	}

	for _, bad := range [][]map[string]any{
		{{"name": "A", "path": client}, {"name": "a", "path": landing}},
		{{"name": "A", "path": client}, {"name": "B", "path": client}},
	} {
		if code, raw := e.do(t, "POST", "/api/projects", map[string]any{"name": "X", "folders": bad}); code != 400 {
			t.Errorf("duplicate: %d %s", code, raw)
		}
	}

	// agentul in Landing, apoi Landing sters: agentul revine pe primul folder
	lead := d.Agents[0].ID
	landingID := d.Folders[1].ID
	body := map[string]any{"name": "Lead", "model": "sonnet", "tools": []string{}, "folderId": landingID}
	if code, raw := e.do(t, "PUT", "/api/agents/"+itoa(lead), body); code != 200 {
		t.Fatalf("folderId: %d %s", code, raw)
	}
	if d := e.folders(t, p.ID); d.Agents[0].FolderID == nil || *d.Agents[0].FolderID != landingID {
		t.Fatalf("folderId salvat: %+v", d.Agents)
	}
	code, raw = e.do(t, "PUT", "/api/projects/"+itoa(p.ID), map[string]any{"name": "Probe", "folders": []map[string]any{
		{"id": d.Folders[0].ID, "name": "Client", "path": client},
	}})
	if code != 200 {
		t.Fatalf("update: %d %s", code, raw)
	}
	if d := e.folders(t, p.ID); len(d.Folders) != 1 || d.Agents[0].FolderID != nil {
		t.Fatalf("dupa stergerea folderului: %+v", d)
	}

	// un id de folder care nu e al proiectului: 400
	if code, _ := e.do(t, "PUT", "/api/projects/"+itoa(p.ID), map[string]any{"name": "Probe", "folders": []map[string]any{
		{"id": 99999, "name": "X", "path": client},
	}}); code != 400 {
		t.Fatalf("folder necunoscut: %d", code)
	}
}

// clientii vechi trimit doar workdir: folderele in plus raman
func TestUpdateProjectWorkdirOnlyKeepsFolders(t *testing.T) {
	e := setup(t)
	a, b := t.TempDir(), t.TempDir()
	_, raw := e.do(t, "POST", "/api/projects", map[string]any{"name": "Probe", "folders": []map[string]any{
		{"name": "A", "path": a}, {"name": "B", "path": b},
	}})
	var p store.Project
	json.Unmarshal(raw, &p)
	if code, raw := e.do(t, "PUT", "/api/projects/"+itoa(p.ID), map[string]any{"name": "Probe 2", "workdir": ""}); code != 200 {
		t.Fatalf("%d %s", code, raw)
	}
	if d := e.folders(t, p.ID); len(d.Folders) != 2 || d.Folders[1].Path != b {
		t.Fatalf("foldere pierdute: %+v", d.Folders)
	}
}

func TestAgentFolderFromOtherProject(t *testing.T) {
	e := setup(t)
	p1 := e.createProject(t, t.TempDir())
	p2 := e.createProject(t, t.TempDir())
	foreign := e.folders(t, p2.ID).Folders[0].ID
	lead := e.root(t, p1.ID)
	code, raw := e.do(t, "PUT", "/api/agents/"+itoa(lead.ID), map[string]any{"name": "Lead", "model": "sonnet", "tools": []string{}, "folderId": foreign})
	if code != 400 || !strings.Contains(string(raw), "folder") {
		t.Fatalf("%d %s", code, raw)
	}
}

func TestOpenChosenFolder(t *testing.T) {
	e := setup(t)
	fo := &fakeOpener{}
	e.srv.SetOpener(fo)
	a, b := t.TempDir(), t.TempDir()
	_, raw := e.do(t, "POST", "/api/projects", map[string]any{"name": "Probe", "folders": []map[string]any{
		{"name": "A", "path": a}, {"name": "B", "path": b},
	}})
	var p store.Project
	json.Unmarshal(raw, &p)
	second := e.folders(t, p.ID).Folders[1].ID
	path := "/api/projects/" + itoa(p.ID) + "/open"
	if code, _ := e.do(t, "POST", path, map[string]any{"target": "folder", "folderId": second}); code != http.StatusNoContent {
		t.Fatalf("open: %d", code)
	}
	if code, _ := e.do(t, "POST", path, map[string]any{"target": "folder"}); code != http.StatusNoContent {
		t.Fatalf("open implicit: %d", code)
	}
	if len(fo.calls) != 2 || fo.calls[0] != "folder:"+b || fo.calls[1] != "folder:"+a {
		t.Fatalf("calls: %v", fo.calls)
	}
	if code, _ := e.do(t, "POST", path, map[string]any{"target": "folder", "folderId": 99999}); code != 400 {
		t.Fatalf("folder necunoscut: %d", code)
	}
}

// o cale goala pe un folder existent il pastreaza; nu se creeaza un folder nou
func TestUpdateProjectEmptyPathKeepsFolder(t *testing.T) {
	e := setup(t)
	a, b := t.TempDir(), t.TempDir()
	_, raw := e.do(t, "POST", "/api/projects", map[string]any{"name": "Probe", "folders": []map[string]any{
		{"name": "A", "path": a}, {"name": "B", "path": b},
	}})
	var p store.Project
	json.Unmarshal(raw, &p)
	d := e.folders(t, p.ID)
	code, raw := e.do(t, "PUT", "/api/projects/"+itoa(p.ID), map[string]any{"name": "Probe", "folders": []map[string]any{
		{"id": d.Folders[0].ID, "name": "A", "path": a},
		{"id": d.Folders[1].ID, "name": "B", "path": ""},
	}})
	if code != 200 {
		t.Fatalf("%d %s", code, raw)
	}
	if d := e.folders(t, p.ID); len(d.Folders) != 2 || d.Folders[1].Path != b {
		t.Fatalf("calea s-a schimbat: %+v", d.Folders)
	}
}

// doar workdir pe un proiect cu 2 foldere: se schimba primul folder, al doilea ramane
func TestUpdateProjectWorkdirOnlyChangesFirstFolder(t *testing.T) {
	e := setup(t)
	a, b, c := t.TempDir(), t.TempDir(), t.TempDir()
	_, raw := e.do(t, "POST", "/api/projects", map[string]any{"name": "Probe", "folders": []map[string]any{
		{"name": "A", "path": a}, {"name": "B", "path": b},
	}})
	var p store.Project
	json.Unmarshal(raw, &p)
	before := e.folders(t, p.ID)
	if code, raw := e.do(t, "PUT", "/api/projects/"+itoa(p.ID), map[string]any{"name": "Probe", "workdir": c}); code != 200 {
		t.Fatalf("%d %s", code, raw)
	}
	d := e.folders(t, p.ID)
	if len(d.Folders) != 2 || d.Workdir != c || d.Folders[0].Path != c || d.Folders[0].ID != before.Folders[0].ID || d.Folders[1].Path != b {
		t.Fatalf("foldere: %+v workdir %q", d.Folders, d.Workdir)
	}
}

// foldere imbricate: 400, ca un agent sa nu scrie in folderul altuia
func TestNestedFoldersRejected(t *testing.T) {
	e := setup(t)
	outer := t.TempDir()
	inner := filepath.Join(outer, "web")
	code, raw := e.do(t, "POST", "/api/projects", map[string]any{"name": "X", "folders": []map[string]any{
		{"name": "A", "path": outer}, {"name": "B", "path": inner},
	}})
	if code != 400 || !strings.Contains(string(raw), "is inside") {
		t.Fatalf("createProject: %d %s", code, raw)
	}
	// prefix de nume, nu de director: acceptat
	sibling := outer + "-web"
	code, raw = e.do(t, "POST", "/api/projects", map[string]any{"name": "Y", "folders": []map[string]any{
		{"name": "A", "path": outer}, {"name": "B", "path": sibling},
	}})
	if code != http.StatusCreated {
		t.Fatalf("frati: %d %s", code, raw)
	}
	os.RemoveAll(sibling)
}
