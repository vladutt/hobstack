package web

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"foreman/internal/mcpregistry"
	"foreman/internal/store"
)

// TestHelperMCPWeb e un server MCP minimal (acelasi ca in testele internal/library): testul se re-porneste ca proces.
func TestHelperMCPWeb(t *testing.T) {
	if os.Getenv("FOREMAN_HELPER_MCP") != "1" {
		return
	}
	sc := bufio.NewScanner(os.Stdin)
	for sc.Scan() {
		var m struct {
			ID     any    `json:"id"`
			Method string `json:"method"`
		}
		json.Unmarshal(sc.Bytes(), &m)
		if m.ID == nil {
			continue
		}
		var r any = map[string]any{}
		switch m.Method {
		case "initialize":
			r = map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{"tools": map[string]any{}}, "serverInfo": map[string]any{"name": "t", "version": "1"}}
		case "tools/list":
			r = map[string]any{"tools": []any{map[string]any{"name": "list_vehicles"}, map[string]any{"name": "get_vehicle"}}}
		}
		out, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": m.ID, "result": r})
		fmt.Println(string(out))
	}
	os.Exit(0)
}

// helperArgs: `env FOREMAN_HELPER_MCP=1 <test binary> -test.run=...` porneste serverul de test (proba are mediul curatat).
func helperArgs() []string {
	return []string{"FOREMAN_HELPER_MCP=1", os.Args[0], "-test.run=^TestHelperMCPWeb$"}
}

// fakeRegistry serveste testdata din mcpregistry; numara cererile.
func fakeRegistry(t *testing.T, status int) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "mcpregistry", "testdata", "servers.json"))
	if err != nil {
		t.Fatal(err)
	}
	var n atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n.Add(1)
		if status != http.StatusOK {
			w.WriteHeader(status)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(raw)
	}))
	t.Cleanup(ts.Close)
	t.Setenv("FOREMAN_MCP_REGISTRY_URL", ts.URL)
	return ts, &n
}

type registryOut struct {
	Servers []struct {
		mcpregistry.Server
		InLibrary bool `json:"inLibrary"`
	} `json:"servers"`
	NextCursor string `json:"nextCursor"`
}

func TestRegistryProxy(t *testing.T) {
	_, n := fakeRegistry(t, http.StatusOK)
	e := setupLib(t)
	// un element cu acelasi URL ca un server remote din registru
	it := store.LibraryItem{Kind: store.KindMCP, Name: "waystation", Description: "d", Status: store.ItemPublished,
		MadeBy: "You", Transport: "remote", URL: "https://waystation.ai/postgres/mcp"}
	if err := e.st.AddLibraryItem(&it); err != nil {
		t.Fatal(err)
	}
	code, _, raw := e.get(t, "/api/library/registry?search=postgres")
	if code != http.StatusOK {
		t.Fatalf("%d %s", code, raw)
	}
	var out registryOut
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	if out.NextCursor == "" || len(out.Servers) == 0 {
		t.Fatalf("raspuns gol: %s", raw)
	}
	found := map[string]bool{}
	for _, s := range out.Servers {
		found[s.Name] = s.InLibrary
	}
	if in, ok := found["ai.waystation/postgres"]; !ok || !in {
		t.Fatalf("waystation trebuia inLibrary: %v", found)
	}
	if found["io.github.bluwork/postgres-scout-mcp"] {
		t.Fatalf("postgres-scout nu e in library: %v", found)
	}
	if n.Load() != 1 {
		t.Fatalf("cereri catre registru: %d", n.Load())
	}
}

func TestRegistryProxyUnavailable(t *testing.T) {
	fakeRegistry(t, http.StatusTooManyRequests)
	e := setupLib(t)
	code, _, raw := e.get(t, "/api/library/registry")
	if code != http.StatusBadGateway {
		t.Fatalf("%d %s", code, raw)
	}
	var out map[string]string
	json.Unmarshal(raw, &out)
	if out["error"] != "The MCP registry is not reachable right now. Try again in a minute." {
		t.Fatalf("%s", raw)
	}
}

func TestLocalMCPNoValues(t *testing.T) {
	e := setupLib(t)
	home := t.TempDir()
	e.srv.home = func() (string, error) { return home, nil }
	cfg := `{"mcpServers": {
		"vehicles": {"command": "npx", "args": ["-y", "vehicles-mcp"], "env": {"VEH_TOKEN": "sup3r-s3cret-value"}},
		"remote-one": {"type": "http", "url": "https://example.com/mcp"}
	}}`
	if err := os.WriteFile(filepath.Join(home, ".claude.json"), []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	it := store.LibraryItem{Kind: store.KindMCP, Name: "vehicles-x", Description: "d", Status: store.ItemPublished,
		MadeBy: "You", Transport: "local", Command: "npx", Args: []string{"-y", "vehicles-mcp"}}
	if err := e.st.AddLibraryItem(&it); err != nil {
		t.Fatal(err)
	}
	code, _, raw := e.get(t, "/api/library/local-mcp")
	if code != http.StatusOK {
		t.Fatalf("%d %s", code, raw)
	}
	if strings.Contains(string(raw), "sup3r-s3cret-value") {
		t.Fatalf("valoarea secreta a iesit: %s", raw)
	}
	var out struct {
		Servers []struct {
			mcpregistry.LocalServer
			InLibrary bool `json:"inLibrary"`
		} `json:"servers"`
	}
	json.Unmarshal(raw, &out)
	if len(out.Servers) != 2 {
		t.Fatalf("%s", raw)
	}
	for _, s := range out.Servers {
		switch s.Name {
		case "vehicles":
			if !s.InLibrary || len(s.Env) != 1 || s.Env[0].Name != "VEH_TOKEN" {
				t.Fatalf("vehicles: %+v", s)
			}
		case "remote-one":
			if s.InLibrary {
				t.Fatalf("remote-one nu e in library")
			}
		}
	}
}

func installBody(name string, args []string) map[string]any {
	return map[string]any{"name": name, "description": "Vehicles", "kind": "local", "command": "env", "args": args,
		"secrets": []string{"VEH_TOKEN"}}
}

func TestInstallMCPProbed(t *testing.T) {
	e := setupLib(t)
	code, raw := e.do(t, "POST", "/api/library/install-mcp", installBody("vehicles", helperArgs()))
	if code != http.StatusCreated {
		t.Fatalf("%d %s", code, raw)
	}
	it, err := e.st.FindLibraryItem(store.KindMCP, "vehicles")
	if err != nil {
		t.Fatal(err)
	}
	if it.Status != store.ItemPublished || it.MadeBy != "You" || it.Transport != "local" ||
		strings.Join(it.ToolNames, ",") != "list_vehicles,get_vehicle" || strings.Join(it.Secrets, ",") != "VEH_TOKEN" {
		t.Fatalf("%+v", it)
	}
	if vs, _ := e.st.ListLibraryVersions(it.ID); len(vs) != 1 {
		t.Fatalf("versiuni: %v", vs)
	}
	// acelasi nume: 409
	code, raw = e.do(t, "POST", "/api/library/install-mcp", installBody("vehicles", helperArgs()))
	if code != http.StatusConflict {
		t.Fatalf("%d %s", code, raw)
	}
}

func TestInstallMCPProbeFails(t *testing.T) {
	e := setupLib(t)
	body := installBody("broken", []string{"false"})
	code, raw := e.do(t, "POST", "/api/library/install-mcp", body)
	if code != http.StatusUnprocessableEntity {
		t.Fatalf("%d %s", code, raw)
	}
	var out struct {
		Error      string `json:"error"`
		TestFailed bool   `json:"testFailed"`
	}
	json.Unmarshal(raw, &out)
	if !out.TestFailed || out.Error == "" {
		t.Fatalf("%s", raw)
	}
	if _, err := e.st.FindLibraryItem(store.KindMCP, "broken"); err == nil {
		t.Fatal("elementul nu trebuia creat")
	}
	// Install without test
	body["skipTest"] = true
	code, raw = e.do(t, "POST", "/api/library/install-mcp", body)
	if code != http.StatusCreated {
		t.Fatalf("%d %s", code, raw)
	}
	it, err := e.st.FindLibraryItem(store.KindMCP, "broken")
	if err != nil || it.Status != store.ItemPublished || len(it.ToolNames) != 0 {
		t.Fatalf("%+v %v", it, err)
	}
}

func TestInstallMCPInProject(t *testing.T) {
	e := setupLib(t)
	p := e.project(t)
	body := installBody("vehicles", helperArgs())
	body["projectId"] = p.ID
	code, raw := e.do(t, "POST", "/api/library/install-mcp", body)
	if code != http.StatusCreated {
		t.Fatalf("%d %s", code, raw)
	}
	it, _ := e.st.FindLibraryItem(store.KindMCP, "vehicles")
	ins, err := e.st.ListInstalls(p.ID)
	if err != nil || len(ins) != 1 || ins[0].ItemID != it.ID || !ins[0].Enabled {
		t.Fatalf("%+v %v", ins, err)
	}
	// proiect inexistent: nimic creat
	body = installBody("other", helperArgs())
	body["projectId"] = 99999
	code, _ = e.do(t, "POST", "/api/library/install-mcp", body)
	if code != http.StatusNotFound {
		t.Fatalf("proiect inexistent: %d", code)
	}
	if _, err := e.st.FindLibraryItem(store.KindMCP, "other"); err == nil {
		t.Fatal("creat pe un proiect inexistent")
	}
}

func TestInstallMCPRemoteNoProbe(t *testing.T) {
	e := setupLib(t)
	code, raw := e.do(t, "POST", "/api/library/install-mcp", map[string]any{"name": "remote-one", "description": "R",
		"kind": "remote", "url": "https://example.com/mcp"})
	if code != http.StatusCreated {
		t.Fatalf("%d %s", code, raw)
	}
	it, err := e.st.FindLibraryItem(store.KindMCP, "remote-one")
	if err != nil || it.Transport != "remote" || it.URL != "https://example.com/mcp" || it.Status != store.ItemPublished {
		t.Fatalf("%+v %v", it, err)
	}
}

func TestInstallMCPRejects(t *testing.T) {
	e := setupLib(t)
	cases := []map[string]any{
		// placeholder-ul de redactare nu e un argument real
		{"name": "hid-a", "description": "d", "kind": "local", "command": "npx", "args": []string{"--token", mcpregistry.Hidden}, "skipTest": true},
		{"name": "hid-b", "description": "d", "kind": "remote", "url": "https://x.com/mcp?key=" + mcpregistry.Hidden, "skipTest": true},
		{"name": "hid-c", "description": "d", "kind": "local", "command": mcpregistry.Hidden, "skipTest": true},
		// validarea din library
		{"name": "Bad Name", "description": "d", "kind": "local", "command": "npx", "skipTest": true},
		{"name": "no-cmd", "description": "d", "kind": "local", "skipTest": true},
		{"name": "bad-secret", "description": "d", "kind": "local", "command": "npx", "secrets": []string{"PATH"}, "skipTest": true},
		// kind nepotrivit cu campurile
		{"name": "mixed", "description": "d", "kind": "local", "url": "https://x.com/mcp", "skipTest": true},
		{"name": "mixed-b", "description": "d", "kind": "remote", "command": "npx", "skipTest": true},
	}
	for _, c := range cases {
		code, raw := e.do(t, "POST", "/api/library/install-mcp", c)
		if code != http.StatusBadRequest {
			t.Fatalf("%v: %d %s", c["name"], code, raw)
		}
	}
	items, _ := e.st.ListLibraryItems("")
	if len(items) != 0 {
		t.Fatalf("create: %+v", items)
	}
}
