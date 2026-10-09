package web

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"foreman/internal/store"
)

func TestCodeFilesAPI(t *testing.T) {
	e := setup(t)
	api, web := t.TempDir(), t.TempDir()
	for _, f := range []string{filepath.Join(api, "app", "UserController.php"), filepath.Join(api, "node_modules", "x", "user.js"),
		filepath.Join(web, "src", "UserCard.vue")} {
		if err := os.MkdirAll(filepath.Dir(f), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	code, raw := e.do(t, "POST", "/api/projects", map[string]any{"name": "Shop", "goal": "Sell",
		"folders": []map[string]any{{"name": "api", "path": api}, {"name": "web", "path": web}}})
	if code != http.StatusCreated {
		t.Fatalf("%d %s", code, raw)
	}
	var p store.Project
	json.Unmarshal(raw, &p)
	agents, _ := e.st.ListAgents(p.ID)
	lead := agents[0] // lucreaza in primul folder (api)

	code, raw = e.do(t, "GET", "/api/projects/"+itoa(p.ID)+"/code-files?q=user&agent="+itoa(lead.ID), nil)
	if code != 200 {
		t.Fatalf("%d %s", code, raw)
	}
	var res struct {
		Files []struct {
			Path   string `json:"path"`
			Name   string `json:"name"`
			Folder string `json:"folder"`
		} `json:"files"`
	}
	json.Unmarshal(raw, &res)
	got := map[string]string{}
	for _, f := range res.Files {
		got[f.Path] = f.Folder
	}
	// folderul agentului: cale relativa; celalalt folder: cale absoluta; node_modules sarit
	if len(res.Files) != 2 || got["app/UserController.php"] != "api" || got[filepath.Join(web, "src", "UserCard.vue")] != "web" {
		t.Fatalf("%s", raw)
	}
	if code, _ := e.do(t, "GET", "/api/projects/999999/code-files?q=x", nil); code != http.StatusNotFound {
		t.Fatalf("proiect lipsa: %d", code)
	}
}
