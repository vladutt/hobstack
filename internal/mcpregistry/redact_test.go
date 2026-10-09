package mcpregistry

import (
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// Fiecare forma de credential trebuie ascunsa: valoarea bruta (care contine "SECRET") nu are voie
// sa apara in json.Marshal(LocalServers(...)), iar serverul devine neinstalabil.
func TestLocalServersRedactsMoreShapes(t *testing.T) {
	type entry struct {
		Type    string   `json:"type,omitempty"`
		Command string   `json:"command,omitempty"`
		Args    []string `json:"args,omitempty"`
		URL     string   `json:"url,omitempty"`
	}
	cmd := func(args ...string) entry { return entry{Command: "npx", Args: args} }
	cases := map[string]entry{
		// 1. header dat ca valoare
		"hdr-after-flag": cmd("mcp-remote", "--header", "x-api-key: SECRET-A3"),
		"hdr-eq":         cmd("mcp-remote", "--header=X-Api-Key: SECRET-A4"),
		"hdr-cookie":     cmd("mcp-remote", "Cookie: session=SECRET-A18"),
		"hdr-H-bare":     cmd("mcp-remote", "-H", "SECRET-A19"),
		"hdr-nospace":    cmd("mcp-remote", "Authorization:SECRET-A30"),
		// 2. --env=NAME=val
		"env-eq":   cmd("x", "--env=GITHUB_TOKEN=SECRET-A8"),
		"env-e-eq": cmd("x", "-e=NAME_TOKEN=SECRET-A81"),
		// 3. token ca username
		"user-arg": cmd("x", "https://SECRET-A11@github.com/x"),
		"user-url": {Type: "http", URL: "https://SECRET-A110@mcp.example.com/mcp"},
		// 4. argumente cu spatii
		"ws-flag":  cmd("--token SECRET-A5"),
		"ws-cmd":   cmd("npx x --api-key SECRET-A13"),
		"ws-sh":    {Command: "sh", Args: []string{"-c", "curl -H 'Authorization: Bearer SECRET-A20' https://x.example"}},
		"ws-sh2":   {Command: "bash", Args: []string{"-c", "curl -H x-api-key: SECRET-A201 https://x.example"}},
		"ws-sh3":   {Command: "sh", Args: []string{"-c", "curl -H \"Authorization: Bearer SECRET-A202\""}},
		"ws-cmd-c": {Command: "cmd", Args: []string{"/c", "set API_TOKEN=SECRET-A21 && run"}},
		// 5. fragment
		"frag": {Type: "http", URL: "https://h.example/mcp#access_token=SECRET-A22&state=1"},
		// 6. parola cu / ? #
		"pw-slash": cmd("x", "postgres://u:pa/SECRET-A12@h/db"),
		"pw-q":     cmd("x", "postgres://u:pa?SECRET-A23@h/db"),
		"pw-hash":  cmd("x", "postgres://u:pa#SECRET-A24@h/db"),
		// 7. valoare care incepe cu -
		"dash-val": cmd("x", "--token", "-SECRET-A14"),
		// 8. nume pe care regex-ul le rata
		"dash-name": cmd("x", "api-key=SECRET-A9"),
		"sig":       cmd("x", "--sig=SECRET-A25"),
		"signature": cmd("x", "--signature", "SECRET-A251"),
		"private":   cmd("x", "--private-key-pem=SECRET-A26"),
		"json-flag": cmd("x", `--config={"apiKey":"SECRET-A16"}`),
		"json-sp":   cmd("x", `--config={"apiKey": "SECRET-A161"}`),
		"json-bare": cmd("x", `{"nested":{"client_secret":"SECRET-A27"}}`),
		// 9. comanda
		"command": {Command: "env TOKEN=SECRET-A28 npx", Args: []string{"x"}},
	}
	servers := map[string]entry{}
	for k, v := range cases {
		servers[k] = v
	}
	b, _ := json.Marshal(map[string]any{"mcpServers": servers})
	home := t.TempDir()
	t.Setenv("APPDATA", "")
	writeFile(t, filepath.Join(home, ".claude.json"), string(b))

	got := LocalServers(home)
	if len(got) != len(cases) {
		t.Fatalf("got %d servers, want %d: %+v", len(got), len(cases), got)
	}
	for _, s := range got {
		out, _ := json.Marshal(s)
		if strings.Contains(string(out), "SECRET") {
			t.Errorf("%s leaks: %s", s.Name, out)
		}
		if s.Installable || s.Reason != ReasonCredential {
			t.Errorf("%s: installable=%v reason=%q", s.Name, s.Installable, s.Reason)
		}
	}
}

// Forma exacta a unor rezultate: se ascunde doar valoarea, restul ramane.
func TestRedactKeepsTheRest(t *testing.T) {
	cases := []struct {
		in   []string
		want []string
	}{
		{[]string{"--header", "x-api-key: S"}, []string{"--header", "x-api-key: <hidden>"}},
		{[]string{"--header=X-Api-Key: S"}, []string{"--header=X-Api-Key: <hidden>"}},
		{[]string{"--env=GITHUB_TOKEN=S"}, []string{"--env=GITHUB_TOKEN=<hidden>"}},
		{[]string{"https://S@github.com/x"}, []string{"https://<hidden>@github.com/x"}},
		{[]string{"--token S"}, []string{"--token <hidden>"}},
		{[]string{"https://h/mcp#access_token=S&state=1"}, []string{"https://h/mcp#access_token=<hidden>&state=1"}},
		{[]string{"https://h/mcp?page=2#section"}, []string{"https://h/mcp?page=2#section"}},
		{[]string{"postgres://u:pa/S@h/db?sslmode=require"}, []string{"postgres://u:<hidden>@h/db?sslmode=require"}},
		{[]string{"--token", "-S"}, []string{"--token", "<hidden>"}},
		{[]string{"-y", "pkg", "--port", "8080", "Content-Type: json"}, []string{"-y", "pkg", "--port", "8080", "Content-Type: json"}},
	}
	for _, c := range cases {
		got, _ := redactList(c.in)
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("redactList(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
