package web

import (
	"encoding/json"
	"strings"
	"testing"

	"hobstack/internal/store"
)

// publishedMCP: un server MCP publicat (cu cheia TOKEN), propus din proiectul pid.
func (e *libEnv) publishedMCP(t *testing.T, pid int64, name string) store.LibraryItem {
	t.Helper()
	it := e.propose(t, store.KindMCP, name, pid, nil, func(it *store.LibraryItem) {
		it.Transport, it.Command, it.Args, it.Secrets, it.MadeBy = "local", "npx", []string{"-y", name}, []string{"TOKEN"}, "You"
	})
	if code, raw := e.do(t, "POST", "/api/library/"+itoa(it.ID)+"/publish", map[string]any{"skipTest": true}); code != 200 {
		t.Fatalf("publish: %d %s", code, raw)
	}
	return it
}

func TestLibraryDefaultAPI(t *testing.T) {
	e := setupLib(t)
	p := e.project(t)
	it := e.publishedMCP(t, p.ID, "gh")
	path := "/api/library/" + itoa(it.ID) + "/default"

	code, raw := e.do(t, "PUT", path, map[string]any{"default": true, "secrets": map[string]string{"TOKEN": "global-secret-123"}})
	if code != 200 {
		t.Fatalf("%d %s", code, raw)
	}
	var v struct {
		IsDefault         bool     `json:"isDefault"`
		DefaultSecretsSet []string `json:"defaultSecretsSet"`
		OffIn             int      `json:"offIn"`
	}
	json.Unmarshal(raw, &v)
	if !v.IsDefault || len(v.DefaultSecretsSet) != 1 || v.DefaultSecretsSet[0] != "TOKEN" {
		t.Fatalf("%s", raw)
	}
	for _, url := range []string{"/api/library/" + itoa(it.ID), "/api/library", "/api/projects/" + itoa(p.ID) + "/library"} {
		if _, body := e.do(t, "GET", url, nil); strings.Contains(string(body), "global-secret-123") {
			t.Fatalf("cheia globala iese in %s", url)
		}
	}
	if code, raw = e.do(t, "PUT", path, map[string]any{"clear": []string{"TOKEN"}}); code != 200 {
		t.Fatalf("%d %s", code, raw)
	}
	json.Unmarshal(raw, &v)
	if !v.IsDefault || len(v.DefaultSecretsSet) != 0 {
		t.Fatalf("clear: %s", raw)
	}
	if code, _ := e.do(t, "PUT", path, map[string]any{"secrets": map[string]string{"NOPE": "x"}}); code != 400 {
		t.Fatalf("nume necunoscut: %d", code)
	}
	prop := e.propose(t, store.KindSkill, "draft", p.ID, map[string]string{"SKILL.md": "x"}, nil)
	if code, _ := e.do(t, "PUT", "/api/library/"+itoa(prop.ID)+"/default", map[string]any{"default": true}); code != 409 {
		t.Fatalf("nepublicat: %d", code)
	}
}

// pornirea flag-ului pe un skill inchide cardurile "wanted the skill" din proiectele care il primesc
func TestLibraryDefaultClosesSkillCards(t *testing.T) {
	e := setupLib(t)
	src := e.project(t)
	it := e.propose(t, store.KindSkill, "claude-api", src.ID, map[string]string{"SKILL.md": "x"}, nil)
	if code, raw := e.do(t, "POST", "/api/library/"+itoa(it.ID)+"/publish", nil); code != 200 {
		t.Fatalf("publish: %d %s", code, raw)
	}
	p := e.project(t)
	w := e.addWorker(t, p.ID, "Atlas")
	e.st.RecordAccessDenial(p.ID, w.ID, nil, "Skill", `{"skill":"claude-api"}`, "")
	if acc := e.skillAccess(t, p.ID); len(acc) != 1 {
		t.Fatalf("%+v", acc)
	}
	if code, raw := e.do(t, "PUT", "/api/library/"+itoa(it.ID)+"/default", map[string]any{"default": true}); code != 200 {
		t.Fatalf("%d %s", code, raw)
	}
	if acc := e.skillAccess(t, p.ID); len(acc) != 0 {
		t.Fatalf("cardul ramane: %+v", acc)
	}
}
