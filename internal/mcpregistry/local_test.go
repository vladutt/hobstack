package mcpregistry

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestLocalServers(t *testing.T) {
	home := t.TempDir()
	t.Setenv("APPDATA", "") // pe windows: calea din home
	writeFile(t, filepath.Join(home, ".claude.json"), `{
  "numStartups": 3,
  "mcpServers": {
    "zeta": {"type": "http", "url": "https://mcp.example.com/mcp", "headers": {"Authorization": "Bearer HDR-SECRET-1"}},
    "Context7_Docs": {"command": "npx", "args": ["-y", "@upstash/context7-mcp"], "env": {"CTX_TOKEN": "TOP-SECRET-1", "A_FLAG": "TOP-SECRET-2"}}
  },
  "projects": {
    "/work/b": {"mcpServers": {"pg": {"type": "stdio", "command": "uvx", "args": ["postgres-mcp"], "env": {"DATABASE_URL": "postgres://u:PROJ-SECRET@x/db"}}}},
    "/work/a": {"mcpServers": {}},
    "/work/c": {"allowedTools": []}
  }
}`)
	writeFile(t, desktopConfigPath(home, runtime.GOOS), `{
  "mcpServers": {
    "filesystem": {"command": "npx", "args": ["-y", "@modelcontextprotocol/server-filesystem", "/tmp"], "env": {"FS_KEY": "DESK-SECRET"}}
  }
}`)

	got := LocalServers(home)
	want := []LocalServer{
		{Source: "Claude Code", Name: "Context7_Docs", Kind: "local", Command: "npx", Args: []string{"-y", "@upstash/context7-mcp"},
			Env: []EnvVar{{Name: "A_FLAG"}, {Name: "CTX_TOKEN"}}, SuggestedName: "context7-docs", Installable: true},
		{Source: "Claude Code", Name: "zeta", Kind: "remote", URL: "https://mcp.example.com/mcp", SuggestedName: "zeta",
			Reason: ReasonNeedsHeaders},
		{Source: "Claude Code", Name: "pg", Kind: "local", Command: "uvx", Args: []string{"postgres-mcp"},
			Env: []EnvVar{{Name: "DATABASE_URL"}}, SuggestedName: "pg", Installable: true},
		{Source: "Claude Desktop", Name: "filesystem", Kind: "local", Command: "npx",
			Args: []string{"-y", "@modelcontextprotocol/server-filesystem", "/tmp"}, Env: []EnvVar{{Name: "FS_KEY"}}, SuggestedName: "filesystem", Installable: true},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got  %+v\nwant %+v", got, want)
	}

	// valorile secrete nu apar nicaieri in raspuns
	b, _ := json.Marshal(got)
	for _, secret := range []string{"TOP-SECRET", "PROJ-SECRET", "DESK-SECRET", "HDR-SECRET", "Bearer"} {
		if strings.Contains(string(b), secret) {
			t.Fatalf("output contains %q: %s", secret, b)
		}
	}
}

func TestLocalServersDedupesProjects(t *testing.T) {
	home := t.TempDir()
	t.Setenv("APPDATA", "")
	same := `{"mcpServers": {"pg": {"command": "uvx", "args": ["postgres-mcp"]}}}`
	writeFile(t, filepath.Join(home, ".claude.json"), `{"projects": {"/a": `+same+`, "/b": `+same+`}}`)
	if got := LocalServers(home); len(got) != 1 {
		t.Fatalf("got %+v, want one entry", got)
	}
}

func TestLocalServersMissingOrInvalid(t *testing.T) {
	t.Setenv("APPDATA", "")
	home := t.TempDir()
	if got := LocalServers(home); len(got) != 0 {
		t.Fatalf("missing files: got %+v", got)
	}
	writeFile(t, filepath.Join(home, ".claude.json"), `{not json`)
	writeFile(t, desktopConfigPath(home, runtime.GOOS), `{"mcpServers": {"ok": {"command": "node", "args": ["s.js"]}}}`)
	got := LocalServers(home)
	if len(got) != 1 || got[0].Name != "ok" || got[0].Source != "Claude Desktop" {
		t.Fatalf("invalid .claude.json should be ignored, desktop kept: %+v", got)
	}
}

func TestDesktopConfigPath(t *testing.T) {
	t.Setenv("APPDATA", "")
	if p := desktopConfigPath("/h", "darwin"); p != filepath.Join("/h", "Library", "Application Support", "Claude", "claude_desktop_config.json") {
		t.Fatalf("darwin: %s", p)
	}
	if p := desktopConfigPath("/h", "windows"); p != filepath.Join("/h", "AppData", "Roaming", "Claude", "claude_desktop_config.json") {
		t.Fatalf("windows: %s", p)
	}
	t.Setenv("APPDATA", "/appdata")
	if p := desktopConfigPath("/h", "windows"); p != filepath.Join("/appdata", "Claude", "claude_desktop_config.json") {
		t.Fatalf("windows APPDATA: %s", p)
	}
}

func TestLocalServersRedactsCredentials(t *testing.T) {
	home := t.TempDir()
	t.Setenv("APPDATA", "")
	writeFile(t, filepath.Join(home, ".claude.json"), `{"mcpServers": {
  "a-url":    {"command": "npx", "args": ["-y", "pg-mcp", "postgres://admin:PW-SECRET-1@db.local:5432/app"]},
  "b-query":  {"type": "http", "url": "https://api.example.com/mcp?api_key=QK-SECRET-2&region=eu"},
  "c-flag":   {"command": "npx", "args": ["-y", "x-mcp", "--api-key=FLAG-SECRET-3"]},
  "d-next":   {"command": "npx", "args": ["-y", "x-mcp", "--token", "NEXT-SECRET-4"]},
  "e-env":    {"command": "docker", "args": ["run", "-i", "--rm", "-e", "GITHUB_TOKEN=ENV-SECRET-5", "ghcr.io/x/y"]},
  "f-bearer": {"command": "npx", "args": ["mcp-remote", "https://h.example/mcp", "--header", "Authorization: Bearer BEARER-SECRET-6"]},
  "h-at":     {"command": "npx", "args": ["-y", "pg-mcp", "--db=postgres://u:p@ss-SECRET-7@h/db", "redis://:SECRET-8@r:6379"]},
  "g-clean":  {"command": "npx", "args": ["-y", "x-mcp", "--port", "8080", "https://h.example/mcp?page=2"]}
}}`)
	got := LocalServers(home)
	if len(got) != 8 {
		t.Fatalf("got %d servers: %+v", len(got), got)
	}
	b, _ := json.Marshal(got)
	if strings.Contains(string(b), "SECRET") {
		t.Fatalf("raw credential leaked: %s", b)
	}
	m := map[string]LocalServer{}
	for _, s := range got {
		m[s.Name] = s
	}
	wantArgs := map[string][]string{
		"a-url":    {"-y", "pg-mcp", "postgres://admin:<hidden>@db.local:5432/app"},
		"c-flag":   {"-y", "x-mcp", "--api-key=<hidden>"},
		"d-next":   {"-y", "x-mcp", "--token", "<hidden>"},
		"e-env":    {"run", "-i", "--rm", "-e", "GITHUB_TOKEN=<hidden>", "ghcr.io/x/y"},
		"f-bearer": {"mcp-remote", "https://h.example/mcp", "--header", "Authorization: <hidden>"},
		"h-at":     {"-y", "pg-mcp", "--db=postgres://u:<hidden>@h/db", "redis://:<hidden>@r:6379"},
	}
	for name, args := range wantArgs {
		s := m[name]
		if !reflect.DeepEqual(s.Args, args) || s.Installable || s.Reason != ReasonCredential {
			t.Errorf("%s: %+v", name, s)
		}
	}
	if q := m["b-query"]; q.URL != "https://api.example.com/mcp?api_key=<hidden>&region=eu" || q.Installable || q.Reason != ReasonCredential {
		t.Errorf("b-query: %+v", q)
	}
	if c := m["g-clean"]; !c.Installable || c.Reason != "" || !reflect.DeepEqual(c.Args, []string{"-y", "x-mcp", "--port", "8080", "https://h.example/mcp?page=2"}) {
		t.Errorf("g-clean: %+v", c)
	}
}

func TestLocalServersEnvAndRemoteRules(t *testing.T) {
	home := t.TempDir()
	t.Setenv("APPDATA", "")
	writeFile(t, filepath.Join(home, ".claude.json"), `{"mcpServers": {
  "lower":  {"command": "npx", "args": ["x"], "env": {"api_key": "v"}},
  "path":   {"command": "npx", "args": ["x"], "env": {"PATH": "/evil"}},
  "dyld":   {"command": "npx", "args": ["x"], "env": {"DYLD_INSERT_LIBRARIES": "v"}},
  "renv":   {"type": "sse", "url": "https://h.example/sse", "env": {"TOKEN": "v"}},
  "rplain": {"type": "http", "url": "https://h.example/mcp"}
}}`)
	m := map[string]LocalServer{}
	for _, s := range LocalServers(home) {
		m[s.Name] = s
	}
	for _, n := range []string{"lower", "path", "dyld"} {
		if s := m[n]; s.Installable || s.Reason != ReasonEnvNames {
			t.Errorf("%s: %+v", n, s)
		}
	}
	if s := m["renv"]; s.Installable || s.Reason != ReasonNeedsHeaders {
		t.Errorf("renv: %+v", s)
	}
	if s := m["rplain"]; !s.Installable || s.Reason != "" {
		t.Errorf("rplain: %+v", s)
	}
}

func TestLocalServersSkipsOnlyBadEntries(t *testing.T) {
	home := t.TempDir()
	t.Setenv("APPDATA", "")
	writeFile(t, filepath.Join(home, ".claude.json"), `{
  "mcpServers": {"bad": {"command": 5}, "good": {"command": "npx", "args": ["g"]}},
  "projects": {
    "/x": "not an object",
    "/y": {"mcpServers": {"bad2": {"command": "x", "args": "not a list"}, "ok2": {"command": "uvx", "args": ["o"]}}},
    "/z": {"mcpServers": []}
  }
}`)
	var names []string
	for _, s := range LocalServers(home) {
		names = append(names, s.Name)
	}
	if !reflect.DeepEqual(names, []string{"good", "ok2"}) {
		t.Fatalf("names = %v", names)
	}
}
