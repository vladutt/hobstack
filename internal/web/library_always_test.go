package web

import (
	"encoding/json"
	"testing"

	"hobstack/internal/store"
)

// alwaysOf: alwaysLoad / alwaysOwn ale elementului in GET /api/projects/{id}/library
func (e *libEnv) alwaysOf(t *testing.T, pid, itemID int64) (on, own bool) {
	t.Helper()
	_, raw := e.do(t, "GET", "/api/projects/"+itoa(pid)+"/library", nil)
	var rows []struct {
		Item       struct{ ID int64 } `json:"item"`
		AlwaysLoad bool               `json:"alwaysLoad"`
		AlwaysOwn  bool               `json:"alwaysOwn"`
	}
	json.Unmarshal(raw, &rows)
	for _, r := range rows {
		if r.Item.ID == itemID {
			return r.AlwaysLoad, r.AlwaysOwn
		}
	}
	t.Fatalf("elementul %d lipseste: %s", itemID, raw)
	return
}

func TestLibraryAlwaysLoadAPI(t *testing.T) {
	e := setupLib(t)
	p := e.project(t)
	it := e.propose(t, store.KindSkill, "short", p.ID, map[string]string{"SKILL.md": "x"}, nil)
	if code, raw := e.do(t, "POST", "/api/library/"+itoa(it.ID)+"/publish", nil); code != 200 {
		t.Fatalf("publish: %d %s", code, raw)
	}
	code, raw := e.do(t, "PUT", "/api/library/"+itoa(it.ID)+"/default", map[string]any{"default": true, "alwaysLoad": true})
	if code != 200 {
		t.Fatalf("%d %s", code, raw)
	}
	var v struct {
		AlwaysLoad bool `json:"alwaysLoad"`
	}
	json.Unmarshal(raw, &v)
	if !v.AlwaysLoad {
		t.Fatalf("flag-ul global nu s-a salvat: %s", raw)
	}
	if on, own := e.alwaysOf(t, p.ID, it.ID); !on || own {
		t.Fatalf("proiectul trebuia sa urmeze flag-ul global: on=%v own=%v", on, own)
	}
	path := "/api/projects/" + itoa(p.ID) + "/library/" + itoa(it.ID) + "/always"
	if code, raw := e.do(t, "PUT", path, map[string]any{"on": false}); code != 204 {
		t.Fatalf("%d %s", code, raw)
	}
	if on, own := e.alwaysOf(t, p.ID, it.ID); on || !own {
		t.Fatalf("oprirea in proiect: on=%v own=%v", on, own)
	}
	if code, _ := e.do(t, "PUT", path, map[string]any{"on": nil}); code != 204 {
		t.Fatalf("reset: %d", code)
	}
	if on, own := e.alwaysOf(t, p.ID, it.ID); !on || own {
		t.Fatalf("dupa reset: on=%v own=%v", on, own)
	}
	mcp := e.publishedMCP(t, p.ID, "gh")
	if code, _ := e.do(t, "PUT", "/api/library/"+itoa(mcp.ID)+"/default", map[string]any{"alwaysLoad": true}); code != 400 {
		t.Fatalf("MCP cu alwaysLoad: %d", code)
	}
	if code, _ := e.do(t, "PUT", "/api/projects/"+itoa(p.ID)+"/library/"+itoa(mcp.ID)+"/always", map[string]any{"on": true}); code != 400 {
		t.Fatalf("MCP in proiect: %d", code)
	}
}
