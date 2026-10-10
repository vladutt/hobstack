package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"hobstack/internal/mcpregistry"
	"hobstack/internal/store"
)

// onePage: servers.json din testdata, fara nextCursor (o singura pagina, deci sincronizarea se termina).
func onePage(t *testing.T) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "mcpregistry", "testdata", "servers.json"))
	if err != nil {
		t.Fatal(err)
	}
	var page map[string]any
	if err := json.Unmarshal(raw, &page); err != nil {
		t.Fatal(err)
	}
	page["metadata"] = map[string]any{"nextCursor": "", "count": 12}
	out, _ := json.Marshal(page)
	return out
}

// catalogRegistry e registrul folosit de catalog; block != nil => fiecare cerere asteapta pana se inchide block.
func catalogRegistry(t *testing.T, block chan struct{}, arrived chan<- struct{}) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	body := onePage(t)
	var n atomic.Int32
	var once sync.Once
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n.Add(1)
		if arrived != nil {
			once.Do(func() { close(arrived) })
		}
		if block != nil {
			<-block
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(body)
	}))
	t.Cleanup(ts.Close)
	return ts, &n
}

func openTestCatalog(t *testing.T, baseURL string) *mcpregistry.Catalog {
	t.Helper()
	cat, err := mcpregistry.OpenCatalog(filepath.Join(t.TempDir(), "mcp-registry.db"), baseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cat.Close() })
	return cat
}

type catalogOut struct {
	Catalog *mcpregistry.CatalogStatus `json:"catalog"`
}

type registryCatalogOut struct {
	registryOut
	catalogOut
}

func TestRegistrySearchFromCatalog(t *testing.T) {
	_, live := fakeRegistry(t, http.StatusOK) // registrul live nu trebuie folosit
	e := setupLib(t)
	reg, n := catalogRegistry(t, nil, nil)
	cat := openTestCatalog(t, reg.URL)
	if err := cat.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	reg.Close() // de aici orice cerere la registru ar esua
	before := n.Load()
	e.srv.SetCatalog(cat, context.Background())

	it := store.LibraryItem{Kind: store.KindMCP, Name: "waystation", Description: "d", Status: store.ItemPublished,
		MadeBy: "You", Transport: "remote", URL: "https://waystation.ai/postgres/mcp"}
	if err := e.st.AddLibraryItem(&it); err != nil {
		t.Fatal(err)
	}
	code, _, raw := e.get(t, "/api/library/registry?search=postgres")
	if code != http.StatusOK {
		t.Fatalf("%d %s", code, raw)
	}
	var out registryCatalogOut
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	if out.Catalog == nil || out.Catalog.State != "ready" {
		t.Fatalf("catalog: %s", raw)
	}
	found := map[string]bool{}
	for _, s := range out.Servers {
		found[s.Name] = s.InLibrary
	}
	if in, ok := found["ai.waystation/postgres"]; !ok || !in {
		t.Fatalf("waystation trebuia inLibrary: %s", raw)
	}
	if n.Load() != before || live.Load() != 0 {
		t.Fatalf("cereri noi la registru: catalog %d->%d, live %d", before, n.Load(), live.Load())
	}

	// cursor invalid pe catalog -> 400
	code, _, raw = e.get(t, "/api/library/registry?cursor=abc")
	if code != http.StatusBadRequest {
		t.Fatalf("cursor invalid: %d %s", code, raw)
	}
}

func TestRegistrySearchCatalogNotReady(t *testing.T) {
	_, live := fakeRegistry(t, http.StatusOK)
	e := setupLib(t)
	reg, _ := catalogRegistry(t, nil, nil)
	e.srv.SetCatalog(openTestCatalog(t, reg.URL), context.Background())

	code, _, raw := e.get(t, "/api/library/registry?search=postgres")
	if code != http.StatusOK {
		t.Fatalf("%d %s", code, raw)
	}
	var out registryCatalogOut
	json.Unmarshal(raw, &out)
	if live.Load() != 1 {
		t.Fatalf("cautarea trebuia sa ajunga la registru: %d", live.Load())
	}
	if out.Catalog == nil || out.Catalog.State != "building" || len(out.Servers) == 0 {
		t.Fatalf("%s", raw)
	}
}

func TestRegistrySearchCatalogNotReadyUnavailable(t *testing.T) {
	fakeRegistry(t, http.StatusTooManyRequests)
	e := setupLib(t)
	reg, _ := catalogRegistry(t, nil, nil)
	e.srv.SetCatalog(openTestCatalog(t, reg.URL), context.Background())
	code, _, raw := e.get(t, "/api/library/registry")
	if code != http.StatusBadGateway {
		t.Fatalf("%d %s", code, raw)
	}
	var out struct {
		Error   string                     `json:"error"`
		Catalog *mcpregistry.CatalogStatus `json:"catalog"`
	}
	json.Unmarshal(raw, &out)
	if out.Error != registryDownMsg || out.Catalog == nil || out.Catalog.State != "building" {
		t.Fatalf("%s", raw)
	}
}

func TestRegistryNoCatalog(t *testing.T) {
	fakeRegistry(t, http.StatusOK)
	e := setupLib(t)
	code, _, raw := e.get(t, "/api/library/registry?search=postgres")
	if code != http.StatusOK {
		t.Fatalf("%d %s", code, raw)
	}
	var out map[string]json.RawMessage
	json.Unmarshal(raw, &out)
	if c, ok := out["catalog"]; !ok || string(c) != "null" {
		t.Fatalf("catalog trebuia null: %s", raw)
	}
	if code, _, raw := e.get(t, "/api/library/registry/status"); code != http.StatusOK || string(mustField(t, raw, "catalog")) != "null" {
		t.Fatalf("status fara catalog: %d %s", code, raw)
	}
	if code, raw := e.do(t, "POST", "/api/library/registry/sync", nil); code != http.StatusNotFound {
		t.Fatalf("sync fara catalog: %d %s", code, raw)
	}
}

func mustField(t *testing.T, raw []byte, key string) json.RawMessage {
	t.Helper()
	var out map[string]json.RawMessage
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("%v: %s", err, raw)
	}
	return out[key]
}

func TestRegistryStatusAndSync(t *testing.T) {
	e := setupLib(t)
	block := make(chan struct{})
	arrived := make(chan struct{})
	reg, _ := catalogRegistry(t, block, arrived)
	cat := openTestCatalog(t, reg.URL)
	unblock := sync.OnceFunc(func() { close(block) })
	t.Cleanup(unblock) // inaintea inchiderii registrului (Cleanup ruleaza invers)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	e.srv.SetCatalog(cat, ctx)

	code, _, raw := e.get(t, "/api/library/registry/status")
	var st catalogOut
	json.Unmarshal(raw, &st)
	if code != http.StatusOK || st.Catalog == nil || st.Catalog.State != "building" || st.Catalog.Syncing {
		t.Fatalf("status: %d %s", code, raw)
	}

	code, raw2 := e.do(t, "POST", "/api/library/registry/sync", nil)
	json.Unmarshal(raw2, &st)
	if code != http.StatusAccepted || st.Catalog == nil || !st.Catalog.Syncing {
		t.Fatalf("sync: %d %s", code, raw2)
	}
	select {
	case <-arrived:
	case <-time.After(5 * time.Second):
		t.Fatal("sincronizarea nu a ajuns la registru")
	}
	// cererea a trecut de contextul cererii HTTP: sincronizarea ruleaza inca
	code, raw2 = e.do(t, "POST", "/api/library/registry/sync", nil)
	if code != http.StatusConflict || string(mustField(t, raw2, "error")) != `"The catalog is already updating."` {
		t.Fatalf("al doilea sync: %d %s", code, raw2)
	}

	unblock()
	deadline := time.Now().Add(5 * time.Second)
	for {
		_, _, raw = e.get(t, "/api/library/registry/status")
		json.Unmarshal(raw, &st)
		if st.Catalog != nil && st.Catalog.State == "ready" && !st.Catalog.Syncing {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("catalogul nu s-a terminat: %s", raw)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if st.Catalog.Count == 0 || st.Catalog.UpdatedAt == nil {
		t.Fatalf("%s", raw)
	}
}
