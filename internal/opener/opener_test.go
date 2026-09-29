package opener

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func touch(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestFindHTML(t *testing.T) {
	dir := t.TempDir()
	if _, err := FindHTML(dir); !errors.Is(err, ErrNoHTML) {
		t.Fatalf("folder gol: %v", err)
	}
	touch(t, filepath.Join(dir, "sub", "index.html")) // subfolderele nu conteaza
	if _, err := FindHTML(dir); !errors.Is(err, ErrNoHTML) {
		t.Fatalf("doar in subfolder: %v", err)
	}
	touch(t, filepath.Join(dir, "zeta.html"))
	touch(t, filepath.Join(dir, "alfa.HTML"))
	if got, _ := FindHTML(dir); got != filepath.Join(dir, "alfa.HTML") {
		t.Fatalf("primul .html alfabetic: %q", got)
	}
	touch(t, filepath.Join(dir, "index.html"))
	if got, _ := FindHTML(dir); got != filepath.Join(dir, "index.html") {
		t.Fatalf("index.html are prioritate: %q", got)
	}
}

func TestDetect(t *testing.T) {
	apps := map[string]bool{"Visual Studio Code": true, "PhpStorm": true, "IntelliJ IDEA CE": true}
	got := detect(true, func(app string) bool { return apps[app] }, func(string) bool { return false })
	ids := ""
	for _, e := range got {
		ids += e.ID + ","
	}
	if ids != "phpstorm,vscode,idea," {
		t.Fatalf("mac: %s", ids)
	}
	cmds := map[string]bool{"code": true, "subl": true}
	got = detect(false, func(string) bool { return false }, func(c string) bool { return cmds[c] })
	if len(got) != 2 || got[0].ID != "vscode" || got[1].ID != "sublime" || got[0].Name != "VS Code" {
		t.Fatalf("altele: %+v", got)
	}
}
