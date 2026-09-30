package web

import (
	"encoding/json"
	"net/http"
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
