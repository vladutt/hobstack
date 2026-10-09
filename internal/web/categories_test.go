package web

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestProjectCategoryAPI(t *testing.T) {
	e := setup(t)
	code, raw := e.do(t, "POST", "/api/projects", map[string]any{"name": "Acme", "workdir": t.TempDir(), "category": "  Work  "})
	var p struct {
		ID       int64
		Category string
	}
	json.Unmarshal(raw, &p)
	if code != http.StatusCreated || p.Category != "Work" {
		t.Fatalf("POST: %d %s", code, raw)
	}
	// PUT fara categorie: ramane
	if code, raw := e.do(t, "PUT", "/api/projects/"+itoa(p.ID), map[string]any{"name": "Acme 2"}); code != 200 || !strings.Contains(string(raw), `"category":"Work"`) {
		t.Fatalf("PUT fara camp: %d %s", code, raw)
	}
	if code, raw := e.do(t, "PUT", "/api/projects/"+itoa(p.ID), map[string]any{"name": "Acme", "category": ""}); code != 200 || !strings.Contains(string(raw), `"category":""`) {
		t.Fatalf("PUT gol: %d %s", code, raw)
	}
	if code, _ := e.do(t, "PUT", "/api/projects/"+itoa(p.ID), map[string]any{"name": "Acme", "category": strings.Repeat("c", 41)}); code != http.StatusBadRequest {
		t.Fatalf("categorie prea lunga: %d", code)
	}
	e.do(t, "PUT", "/api/projects/"+itoa(p.ID), map[string]any{"name": "Acme", "category": "Clients"})
	if _, raw := e.do(t, "GET", "/api/projects", nil); !strings.Contains(string(raw), `"category":"Clients"`) {
		t.Fatalf("lista: %s", raw)
	}
}
