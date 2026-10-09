package mcpregistry

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeRegistry e un registru de test: handler-ul decide raspunsul dupa query, iar cererile se retin.
type fakeRegistry struct {
	mu      sync.Mutex
	reqs    []url.Values
	handler func(q url.Values) (int, any)
}

func (f *fakeRegistry) setHandler(h func(q url.Values) (int, any)) {
	f.mu.Lock()
	f.handler = h
	f.mu.Unlock()
}

func (f *fakeRegistry) requests() []url.Values {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]url.Values(nil), f.reqs...)
}

func (f *fakeRegistry) clearRequests() {
	f.mu.Lock()
	f.reqs = nil
	f.mu.Unlock()
}

func newFakeRegistry(t *testing.T) (*fakeRegistry, *httptest.Server) {
	t.Helper()
	f := &fakeRegistry{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v0/servers" {
			http.NotFound(w, r)
			return
		}
		f.mu.Lock()
		f.reqs = append(f.reqs, r.URL.Query())
		h := f.handler
		f.mu.Unlock()
		status, body := h(r.URL.Query())
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		if body != nil {
			json.NewEncoder(w).Encode(body)
		}
	}))
	t.Cleanup(srv.Close)
	return f, srv
}

// page construieste un raspuns al registrului.
func page(next string, entries ...map[string]any) map[string]any {
	if entries == nil {
		entries = []map[string]any{}
	}
	return map[string]any{"servers": entries, "metadata": map[string]any{"nextCursor": next, "count": len(entries)}}
}

// regEntry e o intrare cu un pachet npm construibil.
func regEntry(name, version, status string, latest bool, updatedAt string) map[string]any {
	return map[string]any{
		"server": map[string]any{
			"name":        name,
			"title":       "Title of " + name,
			"description": "Description of " + name,
			"version":     version,
			"packages": []any{map[string]any{
				"registryType": "npm",
				"identifier":   "@test/" + strings.ReplaceAll(name, "/", "-"),
				"version":      version,
				"transport":    map[string]any{"type": "stdio"},
			}},
		},
		"_meta": map[string]any{
			"io.modelcontextprotocol.registry/official": map[string]any{
				"status":    status,
				"isLatest":  latest,
				"updatedAt": updatedAt,
			},
		},
	}
}

// unbuildable e o intrare fara pachet sau remote pe care stim sa-l construim.
func unbuildable(name, version, updatedAt string) map[string]any {
	e := regEntry(name, version, "active", true, updatedAt)
	srv := e["server"].(map[string]any)
	delete(srv, "packages")
	srv["remotes"] = []any{map[string]any{"type": "websocket", "url": "wss://example.com"}}
	return e
}

func testCatalog(t *testing.T, path, baseURL string) *Catalog {
	t.Helper()
	c, err := OpenCatalog(path, baseURL)
	if err != nil {
		t.Fatal(err)
	}
	c.pause = 0
	c.retryDelays = []time.Duration{0, 0, 0}
	t.Cleanup(func() { c.Close() })
	return c
}

func metaValue(t *testing.T, c *Catalog, key string) (string, bool) {
	t.Helper()
	var v string
	err := c.db.QueryRow(`SELECT value FROM meta WHERE key = ?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false
	}
	if err != nil {
		t.Fatal(err)
	}
	return v, true
}

func rowVersion(t *testing.T, c *Catalog, name string) string {
	t.Helper()
	var v string
	err := c.db.QueryRow(`SELECT version FROM servers WHERE name = ?`, name).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return ""
	}
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// threePages serveste prima sincronizare pe 3 pagini (2 + 2 + 1 servere).
func threePages(q url.Values) (int, any) {
	switch q.Get("cursor") {
	case "":
		return 200, page("c2", regEntry("io.github.a/one", "1.0.0", "active", true, "2026-01-01T00:00:00Z"),
			regEntry("io.github.a/two", "1.0.0", "active", true, "2026-01-02T00:00:00Z"))
	case "c2":
		return 200, page("c3", regEntry("io.github.b/three", "1.0.0", "active", true, "2026-01-03T00:00:00Z"),
			regEntry("io.github.b/four", "1.0.0", "active", true, "2026-01-04T00:00:00Z"))
	case "c3":
		return 200, page("", regEntry("io.github.c/five", "1.0.0", "active", true, "2026-01-05T00:00:00Z"))
	}
	return 404, nil
}

func TestCatalogFullSync(t *testing.T) {
	f, srv := newFakeRegistry(t)
	f.setHandler(threePages)
	c := testCatalog(t, filepath.Join(t.TempDir(), "cat.db"), srv.URL)
	start := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	c.now = func() time.Time { return start }

	if c.Ready() {
		t.Fatal("ready before sync")
	}
	if st := c.Status(); st.State != "building" || st.UpdatedAt != nil {
		t.Fatalf("status before sync = %+v", st)
	}
	if err := c.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	st := c.Status()
	if st.Count != 5 || st.State != "ready" || st.UpdatedAt == nil || !st.UpdatedAt.Equal(start) || st.LastError != nil || st.Syncing {
		t.Fatalf("status = %+v", st)
	}
	if !c.Ready() {
		t.Fatal("not ready")
	}
	if _, ok := metaValue(t, c, "full_cursor"); ok {
		t.Fatal("full_cursor still set")
	}
	for _, q := range f.requests() {
		if q.Get("version") != "latest" || q.Get("limit") != "100" {
			t.Fatalf("query = %v", q)
		}
	}
	// intrarea se pastreaza bruta (server + _meta)
	var entry string
	c.db.QueryRow(`SELECT entry FROM servers WHERE name = 'io.github.a/one'`).Scan(&entry)
	if !strings.Contains(entry, `"_meta"`) || !strings.Contains(entry, `"packages"`) || strings.Contains(entry, "\n") {
		t.Fatalf("entry = %s", entry)
	}
}

func TestCatalogFullSyncResumesAfterError(t *testing.T) {
	f, srv := newFakeRegistry(t)
	var failures atomic.Int32
	f.setHandler(func(q url.Values) (int, any) {
		if q.Get("cursor") == "c2" {
			failures.Add(1)
			return 500, nil
		}
		return threePages(q)
	})
	path := filepath.Join(t.TempDir(), "cat.db")
	c := testCatalog(t, path, srv.URL)
	err := c.Sync(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	if n := failures.Load(); n != 4 {
		t.Fatalf("page 2 requested %d times, want 4", n)
	}
	st := c.Status()
	if st.LastError == nil || st.Count != 2 || st.State != "building" {
		t.Fatalf("status = %+v", st)
	}
	if v, _ := metaValue(t, c, "full_cursor"); v != "c2" {
		t.Fatalf("full_cursor = %q", v)
	}

	// "restart": catalogul se redeschide, serverul s-a reparat
	c.Close()
	c = testCatalog(t, path, srv.URL)
	f.setHandler(threePages)
	f.clearRequests()
	if err := c.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	reqs := f.requests()
	if len(reqs) != 2 || reqs[0].Get("cursor") != "c2" || reqs[1].Get("cursor") != "c3" {
		t.Fatalf("requests = %v", reqs)
	}
	st = c.Status()
	if st.Count != 5 || st.State != "ready" || st.LastError != nil {
		t.Fatalf("status = %+v", st)
	}
}

func TestCatalogUpdate(t *testing.T) {
	f, srv := newFakeRegistry(t)
	f.setHandler(func(q url.Values) (int, any) {
		return 200, page("",
			regEntry("io.github.a/keep", "1.0.0", "active", true, "2026-01-01T00:00:00Z"),
			regEntry("io.github.a/bump", "1.0.0", "active", true, "2026-01-01T00:00:00Z"),
			regEntry("io.github.a/old", "2.0.0", "active", true, "2026-01-01T00:00:00Z"),
			regEntry("io.github.a/gone", "1.0.0", "active", true, "2026-01-01T00:00:00Z"),
			regEntry("io.github.a/other", "1.0.0", "active", true, "2026-01-01T00:00:00Z"),
			regEntry("io.github.a/dep", "1.0.0", "active", true, "2026-01-01T00:00:00Z"),
			regEntry("io.github.a/broken", "1.0.0", "active", true, "2026-01-01T00:00:00Z"),
		)
	})
	c := testCatalog(t, filepath.Join(t.TempDir(), "cat.db"), srv.URL)
	first := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	c.now = func() time.Time { return first }
	if err := c.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}

	f.clearRequests()
	f.setHandler(func(q url.Values) (int, any) {
		switch q.Get("cursor") {
		case "":
			return 200, page("u2",
				regEntry("io.github.a/new", "1.0.0", "active", true, "2026-10-10T01:00:00Z"),
				regEntry("io.github.a/bump", "1.1.0", "active", true, "2026-10-10T02:00:00Z"),
				regEntry("io.github.a/old", "1.0.0", "active", false, "2026-10-10T03:00:00Z"),
				regEntry("io.github.a/gone", "1.0.0", "deleted", true, "2026-10-10T04:00:00Z"),
			)
		case "u2":
			return 200, page("",
				regEntry("io.github.a/other", "0.9.0", "deleted", false, "2026-10-10T05:00:00Z"),
				regEntry("io.github.a/dep", "1.0.0", "deprecated", true, "2026-10-10T06:00:00Z"),
				unbuildable("io.github.a/broken", "1.0.0", "2026-10-10T07:00:00Z"),
				unbuildable("io.github.a/weird", "1.0.0", "2026-10-10T08:00:00Z"),
			)
		}
		return 404, nil
	})
	second := first.Add(25 * time.Hour)
	c.now = func() time.Time { return second }
	if err := c.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	reqs := f.requests()
	if len(reqs) != 2 {
		t.Fatalf("requests = %v", reqs)
	}
	for _, q := range reqs {
		if q.Get("updated_since") != "2026-10-09T11:00:00Z" || q.Get("version") != "" || q.Get("limit") != "100" {
			t.Fatalf("query = %v", q)
		}
	}
	want := map[string]string{
		"io.github.a/keep":   "1.0.0",
		"io.github.a/new":    "1.0.0",
		"io.github.a/bump":   "1.1.0",
		"io.github.a/old":    "2.0.0", // isLatest=false ignorat
		"io.github.a/gone":   "",      // deleted pe versiunea curenta
		"io.github.a/other":  "1.0.0", // deleted pe alta versiune
		"io.github.a/dep":    "",      // deprecated = deleted
		"io.github.a/broken": "",      // normalize refuza acum aceeasi versiune
		"io.github.a/weird":  "",      // fara pachet / remote construibil
	}
	for name, v := range want {
		if got := rowVersion(t, c, name); got != v {
			t.Errorf("%s version = %q, want %q", name, got, v)
		}
	}
	if st := c.Status(); st.UpdatedAt == nil || !st.UpdatedAt.Equal(second) {
		t.Fatalf("status = %+v", st)
	}
}

func TestCatalogConcurrentSync(t *testing.T) {
	f, srv := newFakeRegistry(t)
	release := make(chan struct{})
	entered := make(chan struct{}, 1)
	f.setHandler(func(q url.Values) (int, any) {
		select {
		case entered <- struct{}{}:
		default:
		}
		<-release
		return 200, page("")
	})
	c := testCatalog(t, filepath.Join(t.TempDir(), "cat.db"), srv.URL)
	done := make(chan error)
	go func() { done <- c.Sync(context.Background()) }()
	<-entered
	if !c.Status().Syncing {
		t.Error("Syncing = false during sync")
	}
	if err := c.Sync(context.Background()); !errors.Is(err, ErrSyncRunning) {
		t.Errorf("second Sync = %v, want ErrSyncRunning", err)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestCatalogDue(t *testing.T) {
	f, srv := newFakeRegistry(t)
	f.setHandler(func(q url.Values) (int, any) { return 200, page("") })
	c := testCatalog(t, filepath.Join(t.TempDir(), "cat.db"), srv.URL)
	start := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	c.now = func() time.Time { return start }
	if !c.Due(start) {
		t.Fatal("not due before first sync")
	}
	if err := c.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	if c.Due(start.Add(time.Minute)) {
		t.Fatal("due right after sync")
	}
	if !c.Due(start.Add(25 * time.Hour)) {
		t.Fatal("not due after 25 h")
	}
}

// seed scrie direct randuri in catalog (fara retea).
func seed(t *testing.T, c *Catalog, entries ...map[string]any) {
	t.Helper()
	for _, e := range entries {
		raw, _ := json.Marshal(e)
		var ae apiEntry
		if err := json.Unmarshal(raw, &ae); err != nil {
			t.Fatal(err)
		}
		if err := upsertEntry(c.db, ae, raw); err != nil {
			t.Fatal(err)
		}
	}
}

func names(list []Server) []string {
	out := []string{}
	for _, s := range list {
		out = append(out, s.Name)
	}
	return out
}

func TestCatalogSearch(t *testing.T) {
	c := testCatalog(t, filepath.Join(t.TempDir(), "cat.db"), "http://127.0.0.1:9")
	seed(t, c,
		regEntry("io.github.x/postgres-tools", "1.0.0", "active", true, "2026-05-01T00:00:00Z"),
		regEntry("io.github.y/postgres", "1.0.0", "active", true, "2026-01-01T00:00:00Z"),
		regEntry("io.github.z/db-helper", "1.0.0", "active", true, "2026-06-01T00:00:00Z"),
	)
	// "postgres" e si in descrierea lui db-helper
	var e = regEntry("io.github.w/mysql", "1.0.0", "active", true, "2026-07-01T00:00:00Z")
	e["server"].(map[string]any)["description"] = "Works with Postgres too"
	seed(t, c, e)

	got, next, err := c.Search("Postgres", "")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"io.github.y/postgres", "io.github.x/postgres-tools", "io.github.w/mysql"}
	if strings.Join(names(got), ",") != strings.Join(want, ",") || next != "" {
		t.Fatalf("search = %v next %q", names(got), next)
	}
	if !got[0].Installable || got[0].Command != "npx" {
		t.Fatalf("server = %+v", got[0])
	}

	// doua cuvinte: ambele trebuie sa apara
	got, _, _ = c.Search("  postgres   TOOLS ", "")
	if strings.Join(names(got), ",") != "io.github.x/postgres-tools" {
		t.Fatalf("two words = %v", names(got))
	}

	// caractere speciale cautate literal
	for _, q := range []string{"%", "_", `"`, "post%", "'; DROP TABLE servers; --"} {
		got, _, err := c.Search(q, "")
		if err != nil || len(got) != 0 {
			t.Fatalf("search %q = %v, %v", q, names(got), err)
		}
	}

	// fara text: dupa updated_at descrescator
	got, _, _ = c.Search("   ", "")
	want = []string{"io.github.w/mysql", "io.github.z/db-helper", "io.github.x/postgres-tools", "io.github.y/postgres"}
	if strings.Join(names(got), ",") != strings.Join(want, ",") {
		t.Fatalf("empty = %v", names(got))
	}

	for _, bad := range []string{"abc", "-1", "1.5"} {
		if _, _, err := c.Search("", bad); !errors.Is(err, ErrBadCursor) {
			t.Fatalf("cursor %q err = %v", bad, err)
		}
	}
}

func TestCatalogSearchPaging(t *testing.T) {
	c := testCatalog(t, filepath.Join(t.TempDir(), "cat.db"), "http://127.0.0.1:9")
	for i := 0; i < 65; i++ {
		seed(t, c, regEntry(fmt.Sprintf("io.github.p/srv-%02d", i), "1.0.0", "active", true,
			fmt.Sprintf("2026-01-01T00:%02d:00Z", i)))
	}
	// un rand pe care normalize il refuza e sarit
	seed(t, c, unbuildable("io.github.p/srv-broken", "1.0.0", "2026-01-01T00:30:30Z"))

	var all []string
	cursor := ""
	pages := 0
	for {
		got, next, err := c.Search("srv", cursor)
		if err != nil {
			t.Fatal(err)
		}
		pages++
		if len(got) > 30 {
			t.Fatalf("page size %d", len(got))
		}
		all = append(all, names(got)...)
		if next == "" {
			break
		}
		cursor = next
	}
	if pages != 3 || len(all) != 65 {
		t.Fatalf("pages %d, results %d", pages, len(all))
	}
	if all[0] != "io.github.p/srv-64" || all[64] != "io.github.p/srv-00" {
		t.Fatalf("order: first %s last %s", all[0], all[64])
	}
	got, next, _ := c.Search("srv", "")
	if len(got) != 30 || next != "30" {
		t.Fatalf("first page %d next %q", len(got), next)
	}
}

func TestOpenCatalogResetsUnknownSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cat.db")
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE junk (x INTEGER); INSERT INTO junk VALUES (1); PRAGMA user_version = 7`); err != nil {
		t.Fatal(err)
	}
	db.Close()

	c := testCatalog(t, path, "http://127.0.0.1:9")
	var v int
	c.db.QueryRow(`PRAGMA user_version`).Scan(&v)
	if v != 1 {
		t.Fatalf("user_version = %d", v)
	}
	var n int
	c.db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE name = 'junk'`).Scan(&n)
	if n != 0 {
		t.Fatal("old table kept")
	}
	if st := c.Status(); st.Count != 0 || st.State != "building" {
		t.Fatalf("status = %+v", st)
	}
	// redeschiderea pastreaza datele (versiunea e cunoscuta)
	seed(t, c, regEntry("io.github.a/one", "1.0.0", "active", true, "2026-01-01T00:00:00Z"))
	c.Close()
	c = testCatalog(t, path, "http://127.0.0.1:9")
	if st := c.Status(); st.Count != 1 {
		t.Fatalf("count after reopen = %d", st.Count)
	}
}
