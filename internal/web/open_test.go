package web

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"hobstack/internal/config"
	"hobstack/internal/opener"
	"hobstack/internal/store"
)

// fakeOpener inregistreaza ce s-ar fi deschis, fara sa lanseze aplicatii.
type fakeOpener struct {
	calls []string
}

func (f *fakeOpener) Editors() []opener.Editor {
	return []opener.Editor{{ID: "phpstorm", Name: "PhpStorm"}}
}
func (f *fakeOpener) OpenFolder(dir string) error {
	f.calls = append(f.calls, "folder:"+dir)
	return nil
}
func (f *fakeOpener) OpenFile(path string) error { f.calls = append(f.calls, "file:"+path); return nil }
func (f *fakeOpener) Reveal(path string) error   { f.calls = append(f.calls, "reveal:"+path); return nil }
func (f *fakeOpener) OpenInEditor(id, dir string) error {
	if id != "phpstorm" {
		return opener.ErrUnknownEditor
	}
	f.calls = append(f.calls, "editor:"+id+":"+dir)
	return nil
}

func TestOpenProject(t *testing.T) {
	e := setup(t)
	fo := &fakeOpener{}
	e.srv.SetOpener(fo)
	workdir := t.TempDir()
	p := e.createProject(t, workdir)
	path := "/api/projects/" + itoa(p.ID) + "/open"

	if code, _ := e.do(t, "POST", path, map[string]any{"target": "folder"}); code != http.StatusNoContent {
		t.Fatalf("folder: %d", code)
	}
	code, raw := e.do(t, "POST", path, map[string]any{"target": "browser"})
	if code != http.StatusBadRequest || !strings.Contains(string(raw), "no .html file") {
		t.Fatalf("browser fara html: %d %s", code, raw)
	}
	os.WriteFile(filepath.Join(workdir, "index.html"), []byte("<p>x</p>"), 0o644)
	if code, _ := e.do(t, "POST", path, map[string]any{"target": "browser"}); code != http.StatusNoContent {
		t.Fatalf("browser: %d", code)
	}
	if code, _ := e.do(t, "POST", path, map[string]any{"target": "editor", "editor": "phpstorm"}); code != http.StatusNoContent {
		t.Fatalf("editor: %d", code)
	}
	if code, _ := e.do(t, "POST", path, map[string]any{"target": "editor", "editor": "notepad"}); code != http.StatusBadRequest {
		t.Fatalf("editor necunoscut: %d", code)
	}
	if code, _ := e.do(t, "POST", path, map[string]any{"target": "altceva"}); code != http.StatusBadRequest {
		t.Fatalf("target necunoscut: %d", code)
	}
	want := []string{"folder:" + workdir, "file:" + filepath.Join(workdir, "index.html"), "editor:phpstorm:" + workdir}
	if strings.Join(fo.calls, "|") != strings.Join(want, "|") {
		t.Fatalf("apeluri: %v", fo.calls)
	}
	_, raw = e.do(t, "GET", "/api/info", nil)
	if !strings.Contains(string(raw), `"editors":[{"id":"phpstorm","name":"PhpStorm"}]`) {
		t.Fatalf("info fara editoare: %s", raw)
	}
}

func TestOpenWithoutOpener(t *testing.T) {
	e := setup(t)
	p := e.createProject(t, t.TempDir())
	if code, _ := e.do(t, "POST", "/api/projects/"+itoa(p.ID)+"/open", map[string]any{"target": "folder"}); code != http.StatusNotImplemented {
		t.Fatalf("fara opener: %d", code)
	}
	_, raw := e.do(t, "GET", "/api/info", nil)
	if !strings.Contains(string(raw), `"editors":[]`) {
		t.Fatalf("info: %s", raw)
	}
}

// Open / Show in Finder pe un fisier atasat: originalul cat exista, apoi copia; scripturile doar Reveal
func TestOpenAttachedTaskFile(t *testing.T) {
	e := setup(t)
	fo := &fakeOpener{}
	e.srv.SetOpener(fo)
	workdir := t.TempDir()
	p := e.createProject(t, workdir)
	agents, _ := e.st.ListAgents(p.ID)
	task := store.Task{ProjectID: p.ID, Title: "Logo", AssigneeID: agents[0].ID, Status: store.StatusInProgress}
	if err := e.st.CreateTask(&task); err != nil {
		t.Fatal(err)
	}
	orig := filepath.Join(workdir, "logo.svg")
	os.WriteFile(orig, []byte("<svg/>"), 0o644)
	copyDir := config.TaskFilesDir(e.dirs.Projects, p.ID, task.ID)
	os.MkdirAll(copyDir, 0o755)
	os.WriteFile(filepath.Join(copyDir, "x-logo.svg"), []byte("<svg/>"), 0o644)
	os.WriteFile(filepath.Join(copyDir, "x-run.sh"), []byte("echo"), 0o644)
	id := agents[0].ID
	svg := store.TaskFile{TaskID: task.ID, ProjectID: p.ID, AgentID: &id, AgentName: "Lead", Path: "logo.svg", Name: "logo.svg", Stored: "x-logo.svg", Mime: "image/svg+xml", Source: orig}
	sh := store.TaskFile{TaskID: task.ID, ProjectID: p.ID, AgentID: &id, AgentName: "Lead", Path: "run.sh", Name: "run.sh", Stored: "x-run.sh", Mime: "text/x-sh"}
	e.st.AddTaskFile(&svg)
	e.st.AddTaskFile(&sh)

	base := "/api/task-files/" + itoa(svg.ID)
	if code, raw := e.do(t, "POST", base+"/reveal", map[string]any{}); code != http.StatusOK || !strings.Contains(string(raw), `"original":true`) {
		t.Fatalf("reveal: %d %s", code, raw)
	}
	os.Remove(orig)
	if code, raw := e.do(t, "POST", base+"/open", map[string]any{}); code != http.StatusOK || !strings.Contains(string(raw), `"original":false`) {
		t.Fatalf("open copy: %d %s", code, raw)
	}
	if code, _ := e.do(t, "POST", "/api/task-files/"+itoa(sh.ID)+"/open", map[string]any{}); code != http.StatusBadRequest {
		t.Fatalf("a script must not open: %d", code)
	}
	want := []string{"reveal:" + orig, "file:" + filepath.Join(copyDir, "x-logo.svg")}
	if strings.Join(fo.calls, "|") != strings.Join(want, "|") {
		t.Fatalf("calls: %v", fo.calls)
	}
}
