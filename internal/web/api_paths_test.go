package web

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
)

// caile din chat: care exista in proiect (doar acelea devin link) si Show in Finder pe ele
func TestChatPaths(t *testing.T) {
	e := setup(t)
	fo := &fakeOpener{}
	e.srv.SetOpener(fo)
	p, api, web := twoFolders(t, e)
	writeFiles(t, api, "src/server.go", "README.md")
	writeFiles(t, web, "src/pages/Checkout.tsx", "README.md")
	base := "/api/projects/" + itoa(p.ID)

	ask := []string{"src/server.go:42", "Checkout.tsx", "pages/Checkout.tsx:3:1", "README.md", "src/",
		filepath.Join(web, "src", "pages"), "/etc/hosts", "../secret", "claude.ai/install.sh", "nope.txt"}
	code, raw := e.do(t, "POST", base+"/paths", map[string]any{"paths": ask})
	if code != http.StatusOK {
		t.Fatalf("%d %s", code, raw)
	}
	var res struct {
		Found map[string]struct{ Dir bool } `json:"found"`
	}
	json.Unmarshal(raw, &res)
	for _, want := range []string{"src/server.go:42", "Checkout.tsx", "pages/Checkout.tsx:3:1", "README.md", "src/", filepath.Join(web, "src", "pages")} {
		if _, ok := res.Found[want]; !ok {
			t.Errorf("%s: not found (%s)", want, raw)
		}
	}
	for _, bad := range []string{"/etc/hosts", "../secret", "claude.ai/install.sh", "nope.txt"} {
		if _, ok := res.Found[bad]; ok {
			t.Errorf("%s: found", bad)
		}
	}
	if !res.Found["src/"].Dir || res.Found["README.md"].Dir {
		t.Fatalf("dir flags: %s", raw)
	}

	// README.md e in ambele foldere: primul e cel al agentului (aici primul folder)
	if code, _ := e.do(t, "POST", base+"/reveal", map[string]any{"path": "README.md"}); code != http.StatusOK {
		t.Fatalf("reveal: %d", code)
	}
	if code, _ := e.do(t, "POST", base+"/reveal", map[string]any{"path": "src/pages"}); code != http.StatusOK {
		t.Fatalf("reveal dir: %d", code)
	}
	if code, _ := e.do(t, "POST", base+"/reveal", map[string]any{"path": "/etc/hosts"}); code != http.StatusNotFound {
		t.Fatalf("outside: %d", code)
	}
	want := "reveal:" + filepath.Join(api, "README.md") + "|folder:" + filepath.Join(web, "src", "pages")
	if got := strings.Join(fo.calls, "|"); got != want {
		t.Fatalf("calls: %s", got)
	}
}
