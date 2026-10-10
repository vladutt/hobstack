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
	t.Setenv("HOBSTACK_MCP_REGISTRY_URL", "http://127.0.0.1:9/")
	if c := NewClient(); c.BaseURL != "http://127.0.0.1:9" {
		t.Fatalf("BaseURL = %q", c.BaseURL)
	}
	t.Setenv("HOBSTACK_MCP_REGISTRY_URL", "")
	c := NewClient()
	if c.BaseURL != "https://registry.modelcontextprotocol.io" {
		t.Fatalf("BaseURL = %q", c.BaseURL)
	}
	if c.HTTP == nil || c.HTTP.Timeout != 60*time.Second || c.TTL != 10*time.Minute {
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
	if len(py.Env) != 4 || py.Env[0] != (EnvVar{Name: "WHOOP_CLIENT_ID", Description: "Client ID of your WHOOP developer app (developer.whoop.com).", Required: true, Secret: true, Stored: true}) {
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
	if arg.Installable || arg.Reason != "Needs arguments HobStack can't fill in yet" || arg.Kind != "local" {
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
		"a/x":                                        "mcp-x",
		"a/team":                                     "team-mcp",
		"a/Team":                                     "team-mcp",
	}
	for in, want := range cases {
		if got := SuggestName(in); got != want {
			t.Errorf("SuggestName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNewClientRejectsNonHTTPURL(t *testing.T) {
	for _, v := range []string{"file:///etc/passwd", "ftp://x.example", "not a url", "http://"} {
		t.Setenv("HOBSTACK_MCP_REGISTRY_URL", v)
		if c := NewClient(); c.BaseURL != DefaultBaseURL {
			t.Errorf("%q: BaseURL = %q", v, c.BaseURL)
		}
	}
	t.Setenv("HOBSTACK_MCP_REGISTRY_URL", "https://reg.example/")
	if c := NewClient(); c.BaseURL != "https://reg.example" {
		t.Errorf("BaseURL = %q", c.BaseURL)
	}
}

// pageServer serveste un raspuns JSON scris in test.
func pageServer(t *testing.T, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestSearchRuntimeArgsIdentifiersEnv(t *testing.T) {
	pkg := func(name, extra string) string {
		return `{"server":{"name":"io.x/` + name + `","version":"1.0.0","packages":[{` + extra + `,"transport":{"type":"stdio"}}]}}`
	}
	body := `{"servers":[` + strings.Join([]string{
		pkg("rt-optional", `"registryType":"npm","identifier":"pkg-a","version":"1.0.0","runtimeArguments":[{"type":"named","name":"--registry","value":"https://evil.example"},{"type":"positional","value":"--yes"}]`),
		pkg("rt-required", `"registryType":"oci","identifier":"ghcr.io/x/y","version":"1.0.0","runtimeArguments":[{"type":"positional","value":"-v","isRequired":true}]`),
		pkg("id-dash", `"registryType":"npm","identifier":"-rf","version":"1.0.0"`),
		pkg("id-shell", `"registryType":"npm","identifier":"pkg;rm","version":"1.0.0"`),
		pkg("id-space", `"registryType":"pypi","identifier":"pkg x","version":"1.0.0"`),
		pkg("ver-dash", `"registryType":"npm","identifier":"pkg-b","version":"--foo"`),
		pkg("ver-space", `"registryType":"pypi","identifier":"pkg-c","version":"1.0 --x"`),
		pkg("env-lower", `"registryType":"npm","identifier":"pkg-d","version":"1.0.0","environmentVariables":[{"name":"api_key"}]`),
		pkg("env-path", `"registryType":"npm","identifier":"pkg-e","version":"1.0.0","environmentVariables":[{"name":"PATH"}]`),
		pkg("env-ok", `"registryType":"npm","identifier":"@scope/pkg-f","version":"1.0.0","environmentVariables":[{"name":"API_KEY"}]`),
	}, ",") + `],"metadata":{}}`
	list, _, err := testClient(pageServer(t, body).URL).Search(context.Background(), "", "")
	if err != nil {
		t.Fatal(err)
	}
	m := byName(list)
	for _, skip := range []string{"id-dash", "id-shell", "id-space", "ver-dash", "ver-space"} {
		if s, ok := m["io.x/"+skip]; ok {
			t.Errorf("%s should be skipped: %+v", skip, s)
		}
	}
	if s := m["io.x/rt-optional"]; !s.Installable || !reflect.DeepEqual(s.Args, []string{"-y", "pkg-a@1.0.0"}) {
		t.Errorf("optional runtime args must be dropped: %+v", s)
	}
	if s := m["io.x/rt-required"]; s.Installable || s.Reason != ReasonNeedsArgs {
		t.Errorf("required runtime arg: %+v", s)
	}
	for _, n := range []string{"env-lower", "env-path"} {
		if s := m["io.x/"+n]; s.Installable || s.Reason != ReasonEnvNames {
			t.Errorf("%s: %+v", n, s)
		}
	}
	if s := m["io.x/env-ok"]; !s.Installable {
		t.Errorf("env-ok: %+v", s)
	}
}

func TestSearchCacheReturnsCopies(t *testing.T) {
	srv, n := fixtureServer(t, "servers.json", 200, "application/json")
	c := testClient(srv.URL)
	first, _, err := c.Search(context.Background(), "docker", "")
	if err != nil {
		t.Fatal(err)
	}
	want := byName(first)["io.github.friendlygeorge/docker-mcp-server"]
	wantArgs := append([]string(nil), want.Args...)
	wantEnv := append([]EnvVar(nil), want.Env...)
	// apelantul modifica rezultatul
	for i := range first {
		first[i].Name = "changed"
		if len(first[i].Args) > 0 {
			first[i].Args[0] = "changed"
		}
		if len(first[i].Env) > 0 {
			first[i].Env[0].Name = "changed"
		}
	}
	// cautarea cu spatii in jur foloseste aceeasi intrare din cache
	second, _, err := c.Search(context.Background(), "  docker ", "")
	if err != nil {
		t.Fatal(err)
	}
	if got := atomic.LoadInt32(n); got != 1 {
		t.Fatalf("requests = %d, want 1", got)
	}
	got, ok := byName(second)["io.github.friendlygeorge/docker-mcp-server"]
	if !ok || !reflect.DeepEqual(got.Args, wantArgs) || !reflect.DeepEqual(got.Env, wantEnv) {
		t.Fatalf("cache was mutated: %+v", got)
	}
}

func TestSearchTrimsQuery(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.Query().Get("search")
		w.Write([]byte(`{"servers":[]}`))
	}))
	defer srv.Close()
	if _, _, err := testClient(srv.URL).Search(context.Background(), "  slack \n", ""); err != nil {
		t.Fatal(err)
	}
	if got != "slack" {
		t.Fatalf("search = %q", got)
	}
}
