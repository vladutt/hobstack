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

	// fisierele din lista de cod au si folderul + calea relativa (clic = exploratorul de cod la linie)
	var refs struct {
		Found map[string]struct {
			Folder int64  `json:"folder"`
			File   string `json:"file"`
		} `json:"found"`
	}
	json.Unmarshal(raw, &refs)
	if r := refs.Found["src/server.go:42"]; r.Folder != p.Folders[0].ID || r.File != "src/server.go" {
		t.Errorf("server.go ref: %+v", r)
	}
	if r := refs.Found["Checkout.tsx"]; r.Folder != p.Folders[1].ID || r.File != "src/pages/Checkout.tsx" {
		t.Errorf("Checkout ref: %+v", r)
	}
	if r := refs.Found["src/"]; r.Folder != 0 || r.File != "" {
		t.Errorf("dir has a ref: %+v", r)
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

// intervalele de linii (:64-68) si fisierele din proiectele legate (nu si din cele nelegate)
func TestChatPathsLinkedAndRanges(t *testing.T) {
	e := setup(t)
	p, api, _ := twoFolders(t, e)
	writeFiles(t, api, "src/server.go")
	mk := func(name string) (int64, int64, string) {
		dir := t.TempDir()
		code, raw := e.do(t, "POST", "/api/projects", map[string]any{"name": name, "goal": "x",
			"folders": []map[string]any{{"name": name, "path": dir}}})
		if code != http.StatusCreated {
			t.Fatalf("%d %s", code, raw)
		}
		var q struct {
			ID      int64 `json:"id"`
			Folders []struct {
				ID int64 `json:"id"`
			} `json:"folders"`
		}
		json.Unmarshal(raw, &q)
		return q.ID, q.Folders[0].ID, dir
	}
	linked, linkedFolder, ldir := mk("Mobile")
	_, _, odir := mk("Other")
	writeFiles(t, ldir, "app/Rules/HasKnownAmountRule.php")
	writeFiles(t, odir, "app/Secret.php")
	if code, raw := e.do(t, "POST", "/api/projects/"+itoa(p.ID)+"/links", map[string]any{"projectId": linked}); code != http.StatusCreated && code != http.StatusOK {
		t.Fatalf("link: %d %s", code, raw)
	}

	code, raw := e.do(t, "POST", "/api/projects/"+itoa(p.ID)+"/paths",
		map[string]any{"paths": []string{"server.go:42-50", "HasKnownAmountRule.php:33-37", "Secret.php"}})
	if code != http.StatusOK {
		t.Fatalf("%d %s", code, raw)
	}
	var res struct {
		Found map[string]struct {
			Project int64  `json:"project"`
			Folder  int64  `json:"folder"`
			File    string `json:"file"`
		} `json:"found"`
	}
	json.Unmarshal(raw, &res)
	if r, ok := res.Found["server.go:42-50"]; !ok || r.File != "src/server.go" || r.Project != 0 {
		t.Errorf("range: %s", raw)
	}
	if r, ok := res.Found["HasKnownAmountRule.php:33-37"]; !ok || r.Project != linked || r.Folder != linkedFolder || r.File != "app/Rules/HasKnownAmountRule.php" {
		t.Errorf("linked: %s", raw)
	}
	if _, ok := res.Found["Secret.php"]; ok {
		t.Errorf("unlinked project file found: %s", raw)
	}
}
