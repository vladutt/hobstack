package mcpregistry

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// fixtureServer serveste un fisier din testdata si numara cererile.
func fixtureServer(t *testing.T, file string, status int, contentType string) (*httptest.Server, *int32) {
	t.Helper()
	body, err := os.ReadFile("testdata/" + file)
	if err != nil {
		t.Fatal(err)
	}
	var n int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&n, 1)
		if r.URL.Path != "/v0/servers" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", contentType)
		w.WriteHeader(status)
		w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return srv, &n
}

func testClient(url string) *Client {
	c := NewClient()
	c.BaseURL = url
	return c
}

func byName(list []Server) map[string]Server {
	m := map[string]Server{}
	for _, s := range list {
		m[s.Name] = s
	}
	return m
}

func TestNewClientReadsEnv(t *testing.T) {
	t.Setenv("FOREMAN_MCP_REGISTRY_URL", "http://127.0.0.1:9/")
	if c := NewClient(); c.BaseURL != "http://127.0.0.1:9" {
		t.Fatalf("BaseURL = %q", c.BaseURL)
	}
	t.Setenv("FOREMAN_MCP_REGISTRY_URL", "")
	c := NewClient()
	if c.BaseURL != "https://registry.modelcontextprotocol.io" {
		t.Fatalf("BaseURL = %q", c.BaseURL)
	}
	if c.HTTP == nil || c.HTTP.Timeout != 10*time.Second || c.TTL != 10*time.Minute {
		t.Fatalf("defaults: %+v ttl=%v", c.HTTP, c.TTL)
	}
}

func TestSearchQuery(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.Query().Encode()
		w.Write([]byte(`{"servers":[],"metadata":{"count":0}}`))
	}))
	defer srv.Close()
	list, next, err := testClient(srv.URL).Search(context.Background(), "post gres", "a/b:1.0")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 0 || next != "" {
		t.Fatalf("list=%v next=%q", list, next)
	}
	want := "cursor=a%2Fb%3A1.0&limit=30&search=post+gres&version=latest"
	if got != want {
		t.Fatalf("query = %q, want %q", got, want)
	}
}

func TestSearchNormalizes(t *testing.T) {
	srv, _ := fixtureServer(t, "servers.json", 200, "application/json")
	list, next, err := testClient(srv.URL).Search(context.Background(), "", "")
	if err != nil {
		t.Fatal(err)
	}
	if next != "io.github.Yash-0620/postgres-mcp-secured:1.0.2" {
		t.Fatalf("next = %q", next)
	}
	m := byName(list)

	// doar ultima versiune; nuget si serverul fara pachete/remotes nu apar
	if len(list) != 9 {
		names := []string{}
		for _, s := range list {
			names = append(names, s.ID)
		}
		t.Fatalf("got %d servers: %v", len(list), names)
	}
	for _, s := range list {
		if s.Version == "0.3.4" {
			t.Fatalf("non-latest version listed: %s", s.ID)
		}
	}
	for _, skip := range []string{"com.devart/mcp-postgresql", "io.github.Yash-0620/postgres-mcp-secured"} {
		if _, ok := m[skip]; ok {
			t.Fatalf("%s should be skipped", skip)
		}
	}

	npm := m["io.github.friendlygeorge/docker-mcp-server"]
	wantNpm := Server{
		ID: "io.github.friendlygeorge/docker-mcp-server@0.3.5", Name: "io.github.friendlygeorge/docker-mcp-server",
		Title:       "Docker MCP Server",
		Description: "MCP server for Docker — containers, health checks, Compose, logs, monitoring for AI agents",
		Version:     "0.3.5", Kind: "local", Runtime: "npx", Command: "npx",
		Args:          []string{"-y", "@supernova123/docker-mcp-server@0.3.5"},
		Env:           []EnvVar{{Name: "DOCKER_HOST", Description: "Docker socket path (default: /var/run/docker.sock)"}},
		SuggestedName: "docker-mcp-server", Installable: true,
	}
	if !reflect.DeepEqual(npm, wantNpm) {
		t.Fatalf("npm:\n got %+v\nwant %+v", npm, wantNpm)
	}

	py := m["io.github.cunicopia-dev/whoop-postgres-mcp"]
	if py.Kind != "local" || py.Runtime != "uvx" || py.Command != "uvx" || !reflect.DeepEqual(py.Args, []string{"whoop-postgres-mcp==0.1.0"}) || !py.Installable {
		t.Fatalf("pypi: %+v", py)
	}
	if len(py.Env) != 4 || py.Env[0] != (EnvVar{Name: "WHOOP_CLIENT_ID", Description: "Client ID of your WHOOP developer app (developer.whoop.com).", Required: true, Secret: true}) {
		t.Fatalf("pypi env: %+v", py.Env)
	}
	if py.Env[2].Secret || !py.Env[2].Required || py.Env[2].Name != "WHOOP_REDIRECT_URI" {
		t.Fatalf("pypi env[2]: %+v", py.Env[2])
	}

	// identificatorul oci are deja tag-ul; nu se mai adauga versiunea
	oci := m["io.github.dockersamples/mcp-docker-release-information"]
	if oci.Runtime != "docker" || oci.Command != "docker" || !oci.Installable ||
		!reflect.DeepEqual(oci.Args, []string{"run", "-i", "--rm", "docker.io/dockersamples/mcp-docker-release-information:0.3.0"}) {
		t.Fatalf("oci: %+v", oci)
	}
	ociEnv := m["io.github.n24q02m/better-notion-mcp"]
	if !reflect.DeepEqual(ociEnv.Args, []string{"run", "-i", "--rm", "-e", "NOTION_TOKEN", "docker.io/n24q02m/better-notion-mcp:latest"}) ||
		len(ociEnv.Env) != 1 || !ociEnv.Env[0].Secret || ociEnv.SuggestedName != "better-notion-mcp" {
		t.Fatalf("oci env: %+v", ociEnv)
	}

	// argumente optionale fara valoare sunt sarite
	fs := m["io.github.j0hanz/filesystem-mcp"]
	if !fs.Installable || !reflect.DeepEqual(fs.Args, []string{"-y", "@j0hanz/filesystem-mcp@2.7.5"}) {
		t.Fatalf("optional args: %+v", fs)
	}
	// argumente cu valoare: named -> name=value, positional -> valoarea
	scout := m["io.github.bluwork/postgres-scout-mcp"]
	if !scout.Installable || !reflect.DeepEqual(scout.Args, []string{"-y", "postgres-scout-mcp@1.0.3", "--mode=read-only", "postgresql://localhost:5432/postgres"}) {
		t.Fatalf("args with values: %+v", scout)
	}

	remote := m["ai.waystation/postgres"]
	if remote.Kind != "remote" || remote.Runtime != "" || remote.Command != "" || remote.Args != nil ||
		remote.URL != "https://waystation.ai/postgres/mcp" || !remote.Installable || remote.ID != "ai.waystation/postgres@0.3.1" {
		t.Fatalf("remote: %+v", remote)
	}

	hdr := m["dev.reapx/docker-containers"]
	if hdr.Kind != "remote" || hdr.Installable || hdr.Reason != "Needs headers; not supported yet" || hdr.URL == "" {
		t.Fatalf("remote with headers: %+v", hdr)
	}

	arg := m["io.github.0design/aindf"]
	if arg.Installable || arg.Reason != "Needs arguments Foreman can't fill in yet" || arg.Kind != "local" {
		t.Fatalf("required arg: %+v", arg)
	}
}

func TestSearchCaches(t *testing.T) {
	srv, n := fixtureServer(t, "servers.json", 200, "application/json")
	c := testClient(srv.URL)
	for i := 0; i < 2; i++ {
		if _, _, err := c.Search(context.Background(), "docker", ""); err != nil {
			t.Fatal(err)
		}
	}
	if got := atomic.LoadInt32(n); got != 1 {
		t.Fatalf("requests = %d, want 1 (cached)", got)
	}
	// alta cautare -> alta cerere
	if _, _, err := c.Search(context.Background(), "docker", "next"); err != nil {
		t.Fatal(err)
	}
	if got := atomic.LoadInt32(n); got != 2 {
		t.Fatalf("requests = %d, want 2", got)
	}
	// dupa expirare -> cerere noua
	c.TTL = time.Nanosecond
	time.Sleep(time.Millisecond)
	if _, _, err := c.Search(context.Background(), "docker", ""); err != nil {
		t.Fatal(err)
	}
	if got := atomic.LoadInt32(n); got != 3 {
		t.Fatalf("requests = %d, want 3 (expired)", got)
	}
}

func TestSearchTimeout(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()
	defer close(release)
	c := testClient(srv.URL)
	c.HTTP = &http.Client{Timeout: 50 * time.Millisecond}
	_, _, err := c.Search(context.Background(), "x", "")
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("err = %v, want ErrUnavailable", err)
	}
}

func TestSearchHTMLIsUnavailable(t *testing.T) {
	for _, status := range []int{200, 429} {
		srv, n := fixtureServer(t, "rate-limit.html", status, "text/html")
		c := testClient(srv.URL)
		_, _, err := c.Search(context.Background(), "x", "")
		if !errors.Is(err, ErrUnavailable) {
			t.Fatalf("status %d: err = %v, want ErrUnavailable", status, err)
		}
		// erorile nu se tin in cache
		c.Search(context.Background(), "x", "")
		if got := atomic.LoadInt32(n); got != 2 {
			t.Fatalf("status %d: requests = %d, want 2", status, got)
		}
	}
}

func TestSuggestName(t *testing.T) {
	cases := map[string]string{
		"io.github.friendlygeorge/docker-mcp-server": "docker-mcp-server",
		"io.github.x/My_Server.v2!":                  "my-server-v2",
		"io.github.0nork/0nMCP":                      "mcp-0nmcp",
		"plain":                                      "plain",
		"a/--Weird--":                                "weird",
		"a/" + strings.Repeat("abcde", 10):           strings.Repeat("abcde", 8),
		"a/" + strings.Repeat("x", 39) + "_y":        strings.Repeat("x", 39),
		"a/!!!":                                      "mcp",
	}
	for in, want := range cases {
		if got := SuggestName(in); got != want {
			t.Errorf("SuggestName(%q) = %q, want %q", in, got, want)
		}
	}
}
