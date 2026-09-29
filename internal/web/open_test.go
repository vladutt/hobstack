package web

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"foreman/internal/opener"
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
	if code != http.StatusBadRequest || !strings.Contains(string(raw), "niciun fisier .html") {
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
