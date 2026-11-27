package web

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestMemoryAPI(t *testing.T) {
	e := setup(t)
	p := e.createProject(t, t.TempDir())
	base := "/api/projects/" + itoa(p.ID) + "/memory"
	code, raw := e.do(t, "POST", base, map[string]any{"key": "UI Dates", "summary": "DD.MM.YYYY", "body": "everywhere", "locked": true})
	if code != 201 {
		t.Fatalf("create: %d %s", code, raw)
	}
	var m struct {
		ID         int64  `json:"id"`
		Key        string `json:"key"`
		Locked     bool   `json:"locked"`
		AuthorName string `json:"authorName"`
	}
	json.Unmarshal(raw, &m)
	if m.Key != "ui-dates" || !m.Locked || m.AuthorName != "You" {
		t.Fatalf("%s", raw)
	}
	if code, _ := e.do(t, "POST", base, map[string]any{"key": "ui-dates", "summary": "x"}); code != 409 {
		t.Fatalf("duplicate: %d", code)
	}
	if code, _ := e.do(t, "POST", base, map[string]any{"key": "a/b", "summary": "x"}); code != 400 {
		t.Fatalf("bad key: %d", code)
	}
	if code, _ := e.do(t, "POST", base, map[string]any{"key": "a.b", "summary": " "}); code != 400 {
		t.Fatalf("empty summary: %d", code)
	}
	one := "/api/memory/" + itoa(m.ID)
	if code, raw := e.do(t, "PUT", one, map[string]any{"summary": "DD.MM.YYYY always", "body": ""}); code != 200 || !strings.Contains(string(raw), "always") {
		t.Fatalf("edit locked: %d %s", code, raw)
	}
	if code, _ := e.do(t, "PUT", one+"/lock", map[string]any{"locked": false}); code != 204 {
		t.Fatalf("unlock: %d", code)
	}
	if code, _ := e.do(t, "DELETE", one, nil); code != 204 {
		t.Fatalf("delete: %d", code)
	}
	_, raw = e.do(t, "GET", base, nil)
	if strings.Contains(string(raw), "ui-dates") {
		t.Fatalf("deleted still listed: %s", raw)
	}
	_, raw = e.do(t, "GET", base+"?deleted=1", nil)
	if !strings.Contains(string(raw), "ui-dates") {
		t.Fatalf("deleted list: %s", raw)
	}
	if code, _ := e.do(t, "POST", one+"/restore", nil); code != 204 {
		t.Fatalf("restore: %d", code)
	}
	_, raw = e.do(t, "GET", one+"/history", nil)
	var h []struct{ Action string }
	json.Unmarshal(raw, &h)
	var acts []string
	for _, v := range h {
		acts = append(acts, v.Action)
	}
	if strings.Join(acts, ",") != "restore,forget,unlock,set,lock,set" {
		t.Fatalf("history: %v", acts)
	}
	if code, _ := e.do(t, "GET", "/api/memory/999/history", nil); code != 404 {
		t.Fatalf("missing: %d", code)
	}
}
