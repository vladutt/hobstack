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
			Env: []EnvVar{{Name: "A_FLAG"}, {Name: "CTX_TOKEN"}}, SuggestedName: "context7-docs"},
		{Source: "Claude Code", Name: "zeta", Kind: "remote", URL: "https://mcp.example.com/mcp", SuggestedName: "zeta"},
		{Source: "Claude Code", Name: "pg", Kind: "local", Command: "uvx", Args: []string{"postgres-mcp"},
			Env: []EnvVar{{Name: "DATABASE_URL"}}, SuggestedName: "pg"},
		{Source: "Claude Desktop", Name: "filesystem", Kind: "local", Command: "npx",
			Args: []string{"-y", "@modelcontextprotocol/server-filesystem", "/tmp"}, Env: []EnvVar{{Name: "FS_KEY"}}, SuggestedName: "filesystem"},
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
