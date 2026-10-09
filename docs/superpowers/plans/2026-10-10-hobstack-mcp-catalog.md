# Catalog local al registrului MCP: plan de implementare

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Cautarea din Library -> MCP -> + Add server merge pe un catalog local (`~/.hobstack/mcp-registry.db`) sincronizat cu registrul oficial (prima data complet, apoi zilnic doar schimbarile); pana e gata catalogul, cautarea merge la registru cu timeout 60 s.

**Architecture:** In `internal/mcpregistry` un tip nou `Catalog` (fisier `catalog.go`): baza SQLite proprie (schema versionata cu `user_version`), `Sync` (completa cu cursor salvat / actualizare cu `updated_since`), `Search` (local, aceeasi forma ca `Client.Search`), `Status`. `cmd/hobstack` deschide catalogul si porneste o goroutine (la pornire + la fiecare ora). `internal/web`: `GET /api/library/registry` alege catalogul sau registrul, plus `/status` si `/sync`. UI in `mcp-add.js`.

**Tech Stack:** Go (net/http, database/sql cu modernc.org/sqlite), JS vanilla.

**Spec:** `docs/superpowers/specs/2026-10-10-hobstack-mcp-catalog-design.md`

## Global Constraints

- UI, mesaje, erori in engleza; comentariile din cod in romana; tot textul din UI prin `el()`.
- `go vet ./...`, `go build -race ./...`, `go test ./...`, `GOOS=darwin GOARCH=arm64 go build ./cmd/hobstack`, `GOOS=windows go build ./cmd/hobstack`; JS cu `node --check` pe o copie `.mjs`.
- Fara dependente noi. Catre registru: doar GET, fara credentiale, timeout 60 s pe cerere, max 8 MiB pe raspuns (`maxBody`), 250 ms intre pagini, `limit=100`.
- Testele NU ies pe internet: registrul e un `httptest.Server`; URL-ul prin `HOBSTACK_MCP_REGISTRY_URL` sau `BaseURL`.
- Catalogul e un fisier separat; daca nu se poate deschide, aplicatia porneste oricum (cautarea ramane pe registru).
- Verificarea vizuala doar pe HOME gol cu `claude` fals (`/private/tmp/claude-501/fakebin`) si un registru local de test.

## Review Focus

1. **Restart / eroare in mijlocul primei sincronizari**: continua din `full_cursor`, fara dubluri si fara sa piarda pagini. Test in Task 1.
2. **Actualizare cu intrari amestecate** (versiune veche, `deleted` pe alta versiune decat cea din catalog, intrare pe care normalize o refuza): catalogul ramane corect. Test in Task 1.
3. **Cautare cu `%`, `_`, ghilimele, multe spatii**: text obisnuit, fara eroare SQL, fara rezultate "tot". Test in Task 1.
4. **Doua sincronizari in acelasi timp** (tick + Update now): a doua primeste "already running", nu corupe nimic. Test in Task 1 si Task 2.
5. **Catalog in lucru**: cautarea merge la registru, raspunsul are `catalog.state = "building"`; cand devine gata, nu mai iese pe retea. Test in Task 2.

---

### Task 1: `Catalog` (fisier, sincronizare, cautare)

**Files:** Create `internal/mcpregistry/catalog.go`, `internal/mcpregistry/catalog_test.go`; Modify `internal/mcpregistry/registry.go` (doar: `apiEntry.Meta.Official` primeste `UpdatedAt string \`json:"updatedAt"\``; `fetch` poate fi refolosit sau se adauga un helper `getPage(ctx, q url.Values) (apiPage, error)` comun, cu timeout-ul dat de `http.Client`).

**Interfaces (Produces):**
```go
type CatalogStatus struct {
    State     string     `json:"state"`     // "building" | "ready"
    Count     int        `json:"count"`
    UpdatedAt *time.Time `json:"updatedAt"` // last_sync_at; nil cat timp e "building"
    Syncing   bool       `json:"syncing"`
    LastError *string    `json:"lastError"`
}
var ErrSyncRunning = errors.New("catalog sync already running")

func OpenCatalog(path, baseURL string) (*Catalog, error) // creeaza/deschide; schema necunoscuta => sterge fisierul si reface
func (c *Catalog) Close() error
func (c *Catalog) Sync(ctx context.Context) error       // prima sincronizare (sau continuarea ei) daca full_done lipseste, altfel actualizarea; ErrSyncRunning daca ruleaza
func (c *Catalog) Due(now time.Time) bool               // full_done lipseste SAU last_sync_at mai vechi de 24 h
func (c *Catalog) Ready() bool                          // full_done = 1
func (c *Catalog) Search(query, cursor string) ([]Server, string, error) // ErrBadCursor pentru cursor invalid
var ErrBadCursor = errors.New("invalid cursor")
func (c *Catalog) Status() CatalogStatus
```
Campuri interne testabile: `pause time.Duration` (250 ms implicit, 0 in teste), `retryDelays []time.Duration` (2 s, 10 s, 30 s implicit; 0 in teste), `http *http.Client` (Timeout 60 s), `now func() time.Time`.

Schema (`user_version = 1`):
```sql
CREATE TABLE servers (name TEXT PRIMARY KEY, version TEXT NOT NULL, title TEXT NOT NULL DEFAULT '',
  description TEXT NOT NULL DEFAULT '', search TEXT NOT NULL, entry TEXT NOT NULL, updated_at TEXT NOT NULL);
CREATE INDEX servers_updated ON servers(updated_at DESC, name);
CREATE TABLE meta (key TEXT PRIMARY KEY, value TEXT NOT NULL);
```
DSN ca in store (`_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)`), `SetMaxOpenConns(1)`.

Reguli (din spec §3, §4):
- Prima sincronizare: `GET /v0/servers?version=latest&limit=100[&cursor=full_cursor]`; fiecare pagina: upsert al intrarilor pe care `normalize` le accepta + `full_cursor = nextCursor` intr-o tranzactie; ultima pagina (nextCursor gol): `full_done=1`, `last_sync_at=<inceputul sincronizarii>`, sterge `full_cursor`.
- Actualizare: `GET /v0/servers?updated_since=<last_sync_at - 1h, RFC3339>&limit=100[&cursor=]`, toate paginile; per intrare: `active` + `isLatest` + normalize ok -> upsert; `deleted`/`deprecated` cu (name, version) egale cu randul -> delete; normalize refuza si randul are aceeasi versiune -> delete; altfel nimic. La final `last_sync_at`.
- `search` = lower(name + " " + title + " " + description). `entry` = JSON compact al intrarii (server + _meta).
- Erori: 3 reincercari pe cerere (retryDelays), apoi `last_error`/`last_error_at` salvate si eroarea intoarsa; un succes complet sterge `last_error`.
- Search: cuvinte = `strings.Fields(lower(query))`, max 8; fiecare `instr(search, ?) > 0`; ORDER BY (segmentul de nume dupa ultimul `/` = query intreg) DESC, (instr(lower(name), primul cuvant) > 0) DESC, updated_at DESC, name; LIMIT 31 OFFSET cursor (al 31-lea arata ca exista pagina urmatoare -> nextCursor = offset+30). Randurile pe care normalize le refuza sunt sarite. Cursor: "" sau numar >= 0, altfel `ErrBadCursor`.

- [ ] Teste care pica (`httptest.Server` care serveste pagini construite in test din intrari JSON mici; `pause=0`, `retryDelays` zero):
  - prima sincronizare pe 3 pagini -> Count corect, Ready, `full_cursor` sters, UpdatedAt setat;
  - pagina 2 intoarce 500 de 4 ori -> Sync intoarce eroare, `last_error` setat, randurile paginii 1 raman; serverul repara -> Sync continua cu cursorul paginii 2 (verificat in cererile primite), fara dubluri;
  - actualizare: `updated_since` din cerere = last_sync_at - 1 h; server nou adaugat; versiune noua inlocuieste; `isLatest=false` ignorat; `deleted` pe versiunea curenta sterge; `deleted` pe alta versiune nu sterge; intrare fara pachet/remote construibil ignorata;
  - doua `Sync` concurente: una intoarce `ErrSyncRunning`;
  - `Due`: false imediat dupa sincronizare, true dupa 25 h (prin `now`), true daca nu e gata;
  - Search: doua cuvinte (ambele trebuie sa apara), nume exact primul, `%` / `_` / `"` cautate literal (zero rezultate cand nu exista), paginare 30 + nextCursor, cursor "abc" si "-1" -> ErrBadCursor, fara text -> dupa updated_at;
  - `user_version` diferit in fisier -> OpenCatalog il reface gol.
- [ ] RED, implementare, GREEN; `go vet`.
- [ ] Commit: `mcpregistry: catalog local cu sincronizare din registru si cautare`.

### Task 2: Pornire + web

**Files:** Modify `cmd/hobstack/main.go` (deschide `filepath.Join(paths.Root, "mcp-registry.db")` cu `OpenCatalog(path, mcpregistry.NewClient().BaseURL)`; la eroare doar log; goroutine: `if cat.Due(now) { cat.Sync(ctx) }` la pornire si la `time.NewTicker(time.Hour)`; erorile doar in log), `internal/web/server.go` (`SetCatalog(*mcpregistry.Catalog)`, rute noi), `internal/web/api_mcp_install.go`, `internal/mcpregistry/registry.go` (`NewClient`: `HTTP.Timeout` 60 s); teste in `internal/web/mcp_install_test.go` (sau `catalog_test.go` in web).

**Interfaces:**
- `GET /api/library/registry?search=&cursor=`: catalog setat si `Ready()` -> `Catalog.Search`; altfel `Client.Search` (ca azi). Raspuns: `{servers, nextCursor, catalog: CatalogStatus | null}` (`null` cand nu exista catalog). `ErrBadCursor` -> 400. Eroare de registru -> 502 cu mesajul nou `"The MCP registry is slow or not reachable right now. Try again in a minute."` si `catalog`.
- `GET /api/library/registry/status` -> `{catalog}`.
- `POST /api/library/registry/sync` -> 202 `{catalog}` si porneste `go cat.Sync(context.Background())` (contextul serverului, nu al cererii); daca `Status().Syncing` -> 409 `{error: "The catalog is already updating."}`; fara catalog -> 404.
- `inLibrary` se calculeaza la fel pentru ambele surse.

- [ ] Teste care pica: catalog gata (populat prin Sync pe un registru de test, apoi registrul de test inchis) -> cautarea raspunde 200 fara cereri noi la registru, `catalog.state = "ready"`; catalog nesincronizat -> proxy la registru (cererea ajunge la registrul de test), `catalog.state = "building"`; cursor invalid pe catalog -> 400; `/status`; `/sync` 202, apoi 409 cat timp ruleaza (registru de test care blocheaza pana deblocheaza testul); fara catalog (`SetCatalog` neapelat) -> comportamentul de azi, `catalog: null`.
- [ ] RED, implementare, GREEN; `go vet`, `go build -race ./...`.
- [ ] Commit: `web: cautarea MCP din catalogul local, stare si Update now`.

### Task 3: UI

**Files:** Modify `internal/web/static/js/mcp-add.js`, `internal/web/static/app.css`.

- Randul de stare sub cautare (din `catalog` al fiecarui raspuns si din `/status`):
  - ready: `Catalog: 31,204 servers · updated 3 h ago · ` + link-buton `Update now` (`Updating…` cat timp `syncing`);
  - building: `Building local catalog… 12,400 servers so far. Search uses the live registry until it's done.`;
  - `lastError`: `Last update failed: <text>` + `Update now`;
  - `catalog: null`: randul lipseste.
  Numere cu `toLocaleString("en-US")`; "3 h ago" cu helper-ul existent de timp relativ daca exista in `ui.js`, altfel unul mic local.
- Cat timp `syncing`: `/status` la 3 s (oprit la iesirea din pagina — functia de curatare a view-ului); cand `state` trece din building in ready, cautarea curenta se reface.
- `Update now`: `POST /api/library/registry/sync`; 409 -> doar starea; eroare -> toast.
- Cautare: `AbortController` — cererea precedenta se anuleaza la fiecare cautare noua (abort-ul nu arata eroare); in modul building, cat asteapta: `Searching the registry… this can take up to a minute.`.
- [ ] `node --check` pe copie `.mjs`; verificare vizuala (HOME gol + claude fals + registru local de test cu `HOBSTACK_MCP_REGISTRY_URL`, un server mic Go/Node in /private/tmp/claude-501 care serveste cateva pagini si poate intarzia / pica la cerere) la 1440 si 390: building cu numar, ready cu Update now, Updating…, eroare; fara depasire pe orizontala.
- [ ] Commit: `ui: starea catalogului MCP, Update now si cautare anulabila`.

### Task 4: CLAUDE.md + verificare finala

- [ ] vet, build -race, test, cross-compile.
- [ ] CLAUDE.md: paragraf "Gata (catalog MCP local, spec in docs/superpowers/specs/2026-10-10-hobstack-mcp-catalog-design.md): ..." inainte de "Urmeaza"; spec: `Status: implementat (2026-10-10).`
- [ ] Commit: `docs: catalog MCP local gata`.
