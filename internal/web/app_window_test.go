package web

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// appEnv: server cu Settings > App activ, home intr-un folder temporar (Downloads acolo)
func appEnv(t *testing.T) (*env, string) {
	t.Helper()
	e := setup(t)
	home := t.TempDir()
	root := t.TempDir()
	e.srv.SetApp(filepath.Join(home, "hobstack"), home, root, filepath.Join(root, "hobstack.log"), func() {})
	return e, home
}

func appState(t *testing.T, e *env) map[string]any {
	t.Helper()
	code, raw := e.do(t, "GET", "/api/app", nil)
	if code != http.StatusOK {
		t.Fatalf("GET /api/app: %d %s", code, raw)
	}
	var st map[string]any
	json.Unmarshal(raw, &st)
	return st
}

func TestAppStateOpenInDefault(t *testing.T) {
	e, _ := appEnv(t)
	st := appState(t, e)
	if st["openIn"] != "window" || st["windowSupported"] != false {
		t.Fatalf("%v", st)
	}
}

func TestSetOpenIn(t *testing.T) {
	e, _ := appEnv(t)
	e.srv.SetAppWindow(AppWindow{Supported: true})
	if code, raw := e.do(t, "PUT", "/api/app/open-in", map[string]string{"openIn": "browser"}); code != http.StatusOK {
		t.Fatalf("%d %s", code, raw)
	}
	if st := appState(t, e); st["openIn"] != "browser" || st["windowSupported"] != true {
		t.Fatalf("%v", st)
	}
	if code, _ := e.do(t, "PUT", "/api/app/open-in", map[string]string{"openIn": "tab"}); code != http.StatusBadRequest {
		t.Fatalf("tab: %d", code)
	}
}

func TestSetOpenInUnsupported(t *testing.T) {
	e, _ := appEnv(t)
	if code, _ := e.do(t, "PUT", "/api/app/open-in", map[string]string{"openIn": "browser"}); code != http.StatusConflict {
		t.Fatalf("%d", code)
	}
}

func TestShowCallsHook(t *testing.T) {
	e, _ := appEnv(t)
	if code, _ := e.do(t, "POST", "/api/app/show", nil); code != http.StatusServiceUnavailable {
		t.Fatalf("fara hook: %d", code)
	}
	shown := make(chan struct{}, 1)
	e.srv.SetAppWindow(AppWindow{Supported: true, Show: func() { shown <- struct{}{} }})
	if code, _ := e.do(t, "POST", "/api/app/show", nil); code != http.StatusNoContent {
		t.Fatalf("%d", code)
	}
	select {
	case <-shown:
	case <-time.After(time.Second):
		t.Fatal("Show nu a fost chemat")
	}
}

func TestDownloadSavesFile(t *testing.T) {
	e, home := appEnv(t)
	p := e.createProject(t, t.TempDir())
	list := uploadOK(t, e, p.ID, map[string][]byte{"a.txt": []byte("salut")})
	url := "/api/files/" + itoa(list[0].ID) + "?download=1"
	want := []string{filepath.Join(home, "Downloads", "a.txt"), filepath.Join(home, "Downloads", "a (2).txt")}
	for _, path := range want {
		code, raw := e.do(t, "POST", "/api/app/download", map[string]string{"url": url, "name": "a.txt"})
		if code != http.StatusOK {
			t.Fatalf("%d %s", code, raw)
		}
		var out struct{ Path string }
		json.Unmarshal(raw, &out)
		if out.Path != path {
			t.Fatalf("path = %q, vreau %q", out.Path, path)
		}
		if b, _ := os.ReadFile(path); string(b) != "salut" {
			t.Fatalf("continut %q", b)
		}
	}
}

func TestDownloadRejects(t *testing.T) {
	e, home := appEnv(t)
	cases := []struct {
		url, name string
		code      int
	}{
		{"http://evil/x", "x.txt", http.StatusBadRequest},
		{"/api/files/1", "../x", http.StatusBadRequest},
		{"/api/files/1", "", http.StatusBadRequest},
		{"/api/files/999999", "x.txt", http.StatusBadGateway},
	}
	for _, c := range cases {
		if code, raw := e.do(t, "POST", "/api/app/download", map[string]string{"url": c.url, "name": c.name}); code != c.code {
			t.Errorf("%s %q: %d %s", c.url, c.name, code, raw)
		}
	}
	entries, _ := os.ReadDir(filepath.Join(home, "Downloads"))
	if len(entries) != 0 {
		t.Fatalf("au ramas fisiere: %v", entries)
	}
}

func TestRevealOnlyDownloads(t *testing.T) {
	e, home := appEnv(t)
	fo := &fakeOpener{}
	e.srv.SetOpener(fo)
	outside := filepath.Join(t.TempDir(), "x.txt")
	os.WriteFile(outside, []byte("x"), 0o644)
	if code, _ := e.do(t, "POST", "/api/app/reveal", map[string]string{"path": outside}); code != http.StatusBadRequest {
		t.Fatalf("in afara: %d", code)
	}
	dl := filepath.Join(home, "Downloads")
	os.MkdirAll(dl, 0o755)
	inside := filepath.Join(dl, "y.txt")
	os.WriteFile(inside, []byte("y"), 0o644)
	if code, raw := e.do(t, "POST", "/api/app/reveal", map[string]string{"path": inside}); code != http.StatusNoContent {
		t.Fatalf("in Downloads: %d %s", code, raw)
	}
	if len(fo.calls) != 1 || fo.calls[0] != "reveal:"+inside {
		t.Fatalf("%v", fo.calls)
	}
}

func TestUniqueName(t *testing.T) {
	dir := t.TempDir()
	if got := uniqueName(dir, "report.pdf"); got != "report.pdf" {
		t.Fatal(got)
	}
	os.WriteFile(filepath.Join(dir, "report.pdf"), nil, 0o644)
	if got := uniqueName(dir, "report.pdf"); got != "report (2).pdf" {
		t.Fatal(got)
	}
	os.WriteFile(filepath.Join(dir, "report (2).pdf"), nil, 0o644)
	if got := uniqueName(dir, "report.pdf"); got != "report (3).pdf" {
		t.Fatal(got)
	}
	os.WriteFile(filepath.Join(dir, "notes"), nil, 0o644)
	if got := uniqueName(dir, "notes"); got != "notes (2)" {
		t.Fatal(got)
	}
}

// un nume lung: "nume (2).ext" trece de 255 de octeti; nu are voie sa blocheze serverul
func TestDownloadLongNameDoesNotHang(t *testing.T) {
	e, _ := appEnv(t)
	p := e.createProject(t, t.TempDir())
	list := uploadOK(t, e, p.ID, map[string][]byte{"a.txt": []byte("x")})
	url := "/api/files/" + itoa(list[0].ID) + "?download=1"
	long := strings.Repeat("a", 251) + ".txt" // 255 octeti
	done := make(chan int, 2)
	go func() {
		for i := 0; i < 2; i++ {
			code, _ := e.do(t, "POST", "/api/app/download", map[string]string{"url": url, "name": long})
			done <- code
		}
	}()
	for i := 0; i < 2; i++ {
		select {
		case code := <-done:
			if i == 0 && code != http.StatusOK {
				t.Fatalf("primul: %d", code)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("descarcarea s-a blocat")
		}
	}
	if code, _ := e.do(t, "POST", "/api/app/download", map[string]string{"url": url, "name": "a\x00b"}); code != http.StatusBadRequest {
		t.Fatalf("NUL: %d", code)
	}
}
