package web

import (
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

type folderContext struct {
	Found     bool   `json:"found"`
	Content   string `json:"content"`
	Truncated bool   `json:"truncated"`
}

func (e *env) folderContext(t *testing.T, path string) (int, folderContext) {
	t.Helper()
	code, raw := e.do(t, "GET", "/api/folder-context?path="+url.QueryEscape(path), nil)
	var fc folderContext
	json.Unmarshal(raw, &fc)
	return code, fc
}

func TestFolderContextReadsClaudeMd(t *testing.T) {
	e := setup(t)
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte("  # Probe\nnpm test\n\n"), 0o644)
	code, fc := e.folderContext(t, dir)
	if code != 200 || !fc.Found || fc.Content != "# Probe\nnpm test" || fc.Truncated {
		t.Fatalf("%d %+v", code, fc)
	}

	// fara CLAUDE.md: 200, found false
	if code, fc := e.folderContext(t, t.TempDir()); code != 200 || fc.Found || fc.Content != "" {
		t.Fatalf("fara fisier: %d %+v", code, fc)
	}
	// folder inexistent: tot "not found", nu eroare (folderul se creeaza abia la salvare)
	if code, fc := e.folderContext(t, filepath.Join(dir, "nu-exista")); code != 200 || fc.Found {
		t.Fatalf("folder inexistent: %d %+v", code, fc)
	}
	// cale relativa sau goala: 400
	for _, bad := range []string{"relativ/x", ""} {
		if code, _ := e.folderContext(t, bad); code != 400 {
			t.Errorf("%q: vreau 400, am %d", bad, code)
		}
	}
}

func TestFolderContextTruncates(t *testing.T) {
	e := setup(t)
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte(strings.Repeat("ă", 25000)), 0o644)
	_, fc := e.folderContext(t, dir)
	if !fc.Found || !fc.Truncated || utf8.RuneCountInString(fc.Content) != 20000 {
		t.Fatalf("found=%v truncated=%v len=%d", fc.Found, fc.Truncated, utf8.RuneCountInString(fc.Content))
	}
}

func TestFolderContextExpandsHome(t *testing.T) {
	e := setup(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	os.MkdirAll(filepath.Join(home, "code"), 0o755)
	os.WriteFile(filepath.Join(home, "code", "CLAUDE.md"), []byte("go"), 0o644)
	if _, fc := e.folderContext(t, "~/code"); !fc.Found || fc.Content != "go" {
		t.Fatalf("%+v", fc)
	}
}
