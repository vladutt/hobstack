package web

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

func TestClipboardFiles(t *testing.T) {
	e := setup(t)
	dir := t.TempDir()
	a := filepath.Join(dir, "a.txt")
	b := filepath.Join(dir, "b.png")
	other := filepath.Join(dir, "secret.txt")
	os.WriteFile(a, []byte("alpha"), 0o644)
	os.WriteFile(b, []byte("\x89PNG"), 0o644)
	os.WriteFile(other, []byte("no"), 0o644)
	os.Mkdir(filepath.Join(dir, "sub"), 0o755)
	e.srv.clip = func(context.Context) ([]string, error) {
		return []string{a, filepath.Join(dir, "sub"), b, filepath.Join(dir, "gone.txt")}, nil
	}

	code, raw := e.do(t, "POST", "/api/clipboard/files", nil)
	var res struct{ Files []clipFile }
	json.Unmarshal(raw, &res)
	if code != 200 || len(res.Files) != 2 || res.Files[0].Name != "a.txt" || res.Files[1].Name != "b.png" || res.Files[0].Size != 5 {
		t.Fatalf("list: %d %s", code, raw)
	}

	code, raw = e.do(t, "POST", "/api/clipboard/file", map[string]string{"path": a})
	if code != 200 || string(raw) != "alpha" {
		t.Fatalf("file: %d %s", code, raw)
	}
	// doar ce e in clipboard acum: alt fisier de pe disc nu se poate citi
	if code, _ := e.do(t, "POST", "/api/clipboard/file", map[string]string{"path": other}); code != http.StatusNotFound {
		t.Fatalf("not on clipboard: %d", code)
	}
	if code, _ := e.do(t, "POST", "/api/clipboard/file", map[string]string{"path": filepath.Join(dir, "sub")}); code != http.StatusNotFound {
		t.Fatalf("folder: %d", code)
	}
	// GET nu exista (o pagina straina ar putea trimite GET fara preflight)
	if code, _ := e.do(t, "GET", "/api/clipboard/files", nil); code == 200 {
		t.Fatal("GET must not list the clipboard")
	}
}

func TestClipboardEmpty(t *testing.T) {
	e := setup(t)
	e.srv.clip = func(context.Context) ([]string, error) { return nil, os.ErrPermission }
	code, raw := e.do(t, "POST", "/api/clipboard/files", nil)
	if code != 200 || string(raw) != "{\"files\":[]}\n" {
		t.Fatalf("%d %s", code, raw)
	}
}
