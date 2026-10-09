# Search si Install pentru servere MCP: plan de implementare

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** In Library -> MCP, "+ Add server" deschide o cautare in registrul oficial MCP si lista serverelor din configurarile Claude ale utilizatorului; Install creeaza elementul publicat (cu proba) si optional il activeaza in proiect.

**Architecture:** Pachet nou `internal/mcpregistry`: client pentru `/v0/servers` (cache 10 min, timeout 10 s, URL de baza configurabil prin `FOREMAN_MCP_REGISTRY_URL` pentru teste), normalizare in `Server` (comanda construita din pachet sau URL remote, variabile de mediu, installable + reason) si citirea configurarilor Claude (doar nume, fara valori). Web: trei endpoint-uri sub `/api/library`. UI: pagina `#/library/mcp/add` in `library.js` (sau modul nou `mcp-add.js`).

**Tech Stack:** Go (net/http client, encoding/json), JS vanilla.

**Spec:** `docs/superpowers/specs/2026-10-09-foreman-mcp-install-design.md`

## Global Constraints

- UI, mesaje, erori in engleza; comentariile din cod in romana; tot textul prin `el()`.
- `go vet ./...`, `go build -race ./...`, `go test ./...`, cross-compile darwin/arm64 si windows; JS cu `node --check` pe copie `.mjs`.
- Fara dependente noi. Cererile catre registru: doar GET, fara credentiale, timeout 10 s, cache 10 min pe (search, cursor), din server (niciodata din browser).
- Testele NU ies pe internet: registrul e inlocuit de un `httptest.Server` cu raspunsuri salvate (fisiere in `internal/mcpregistry/testdata/`).
- Valorile variabilelor de mediu din configurarile Claude nu se citesc in raspunsuri si nu se salveaza; doar numele.
- Comanda exacta se confirma inainte de proba (UI). Proba: `library.ProbeMCP(command, args, env, 30*time.Second)`.
- Verificarea vizuala doar pe HOME gol cu `claude` fals (`/private/tmp/claude-501/fakebin`), registrul de test local prin `FOREMAN_MCP_REGISTRY_URL`.

## Review Focus

1. **Registrul nu raspunde / raspunde cu HTML sau 429**: eroare clara, UI cu Retry, "From your Claude" merge. Test in Task 1 si Task 3.
2. **Server cu argument obligatoriu fara valoare / remote cu header obligatoriu**: afisat, Install dezactivat cu motiv. Test in Task 1.
3. **Valoarea unei variabile secrete din ~/.claude.json ajunge in raspuns sau in DB**: nu are voie. Test in Task 1 si Task 2.
4. **Nume deja in Library**: 409 la install, "In library" in lista. Test in Task 2.
5. **Proba pica**: nimic creat fara skipTest; cu skipTest creat cu tool_names gol. Test in Task 2.

---

### Task 1: `internal/mcpregistry` (registru + configurari Claude)

**Files:** Create `internal/mcpregistry/registry.go`, `internal/mcpregistry/local.go`, teste + `testdata/*.json` (raspunsuri reale: un npm stdio, un pypi, un oci, un remote streamable-http, un remote cu header obligatoriu, un npm cu argument obligatoriu fara valoare, o versiune ne-latest care trebuie ignorata).

**Interfaces (Produces):**
```go
type EnvVar struct { Name, Description string; Required, Secret bool }
type Server struct {
    ID, Name, Title, Description, Version string // ID = name@version
    Kind    string   // "local" | "remote"
    Runtime string   // "npx" | "uvx" | "docker" | ""
    Command string; Args []string; URL string
    Env     []EnvVar
    SuggestedName string // [a-z0-9-], din ultimul segment al numelui
    Installable bool; Reason string // motivul cand nu se poate instala
}
type Client struct { BaseURL string; HTTP *http.Client; TTL time.Duration } // NewClient() citeste FOREMAN_MCP_REGISTRY_URL, altfel https://registry.modelcontextprotocol.io
func (c *Client) Search(ctx context.Context, search, cursor string) ([]Server, string, error) // GET /v0/servers?search=&cursor=&version=latest&limit=30
var ErrUnavailable = errors.New("registry unavailable") // timeout, status != 200, corp care nu e JSON

type LocalServer struct { Source, Name, Kind, Command string; Args []string; URL string; Env []EnvVar; SuggestedName string }
func LocalServers(home string) []LocalServer // ~/.claude.json (mcpServers + projects.*.mcpServers) si Claude Desktop (darwin: Library/Application Support/Claude/claude_desktop_config.json; windows: %APPDATA%\Claude\...); fisier lipsa / JSON invalid => ignorat; Env doar nume
```
Reguli de construire: npm -> `npx` `-y <id>@<ver>`; pypi -> `uvx` `<id>==<ver>`; oci -> `docker` `run -i --rm [-e NAME ...] <id>:<ver>`; argumentele cu valoare implicita adaugate; argument obligatoriu fara valoare -> `Installable=false, Reason="Needs arguments Foreman can't fill in yet"`; remote: primul `streamable-http`/`sse`; header obligatoriu -> `Reason="Needs headers; not supported yet"`; alt tip -> sarit. SuggestedName: segmentul de dupa ultimul `/`, litere mici, orice altceva decat `[a-z0-9-]` -> `-`, fara `-` la capete, max 40.

- [ ] Teste care pica (normalizare pe testdata; cache: a doua cautare identica nu mai face cerere — numara cererile in httptest; timeout -> ErrUnavailable; raspuns HTML -> ErrUnavailable; local: fisiere temporare cu valori secrete -> raspunsul nu contine valorile, doar numele; fisier lipsa ignorat).
- [ ] RED, implementare, GREEN; `go vet`.
- [ ] Commit: `mcpregistry: cautare in registrul MCP si serverele din configurarile Claude`.

### Task 2: Install (web + proba)

**Files:** Create `internal/web/api_mcp_install.go` + teste; Modify `internal/web/server.go` (rute, un `*mcpregistry.Client` in Server).

**Interfaces:**
- `GET /api/library/registry?search=&cursor=` -> `{servers: [Server + inLibrary bool], nextCursor}`; registru indisponibil -> 502 `{error: "The MCP registry is not reachable right now. Try again in a minute."}`.
- `GET /api/library/local-mcp` -> `{servers: [LocalServer + inLibrary]}`.
- `POST /api/library/install-mcp` `{name, description, kind, command, args, url, secrets, skipTest, projectId}` -> 201 element | 400 validare (`library.ValidateInput`) | 409 nume luat | 422 `{error, testFailed: true}`.
- `inLibrary`: exista element `kind=mcp` cu acelasi nume, sau aceeasi comanda+argumente, sau acelasi URL.
- Install: validare -> nume liber -> proba (local; env = valorile secretelor date deja in `projectId`, daca exista) -> creare `status=published`, `made_by` "You", `tool_names` din proba -> daca `projectId`, instalat si activat in proiect (acelasi drum ca `PUT /api/projects/{id}/library/{item}`). Proba picata fara skipTest -> 422, nimic creat. Remote: fara proba (ca azi la Publish; verifica si foloseste comportamentul existent).

- [ ] Teste care pica (registry proxy cu httptest + `FOREMAN_MCP_REGISTRY_URL`; 502 la registru indisponibil; local-mcp fara valori; install: creare publicata cu proba reusita — foloseste serverul MCP de test din testele `internal/library`; nume luat 409; proba picata 422 si nimic in DB; skipTest -> tool_names gol; projectId -> instalat si activat).
- [ ] RED, implementare, GREEN; `go vet`.
- [ ] Commit: `web: cautare in registrul MCP, servere locale si install cu proba`.

### Task 3: UI

**Files:** Create `internal/web/static/js/mcp-add.js`; Modify `internal/web/static/js/library.js` ("+ Add server" pentru mcp deschide `#/library/mcp/add`; formularul manual ramane prin link "Add manually"), ruterul (`app.js`) pentru `#/library/mcp/add`, `library-cards.js` (link "Add MCP server" in Instructions > Library cu `?project=<id>`), `app.css`.

- Pagina: cautare (debounce 300 ms, min 2 caractere; fara text: primele rezultate), rezultate cu titlu, descriere pe o linie, tip/runtime, versiune, Install (dezactivat cu `reason` in `title` + text mic); "Load more" cu `nextCursor`; eroare 502 -> mesaj + Retry; sectiunea "From your Claude" (sursa, comanda/URL, Install); "In library" in loc de Install.
- Fereastra Install: Name (sugerat, editabil, validat `[a-z0-9-]`), comanda exacta / URL (monospace), variabilele (nume, descriere, required/secret), bifa "Enable in <proiect>" cand pagina a primit `?project=`, buton Install cu confirmare "Run this command to test it?"; 422 -> eroarea + "Install without test" (confirmare) -> retrimite cu `skipTest`; 201 -> du-te pe pagina elementului.
- [ ] `node --check`; verificare vizuala pe HOME gol + claude fals + registru de test local (un mic server Go/Node in /private/tmp/claude-501 care serveste testdata prin `FOREMAN_MCP_REGISTRY_URL`), 1440 si 390: cautare cu rezultate, Install dezactivat cu motiv, fereastra, eroare de registru + Retry, "From your Claude" (pe HOME gol: un `.claude.json` de test cu un server si o variabila secreta — valoarea nu trebuie sa apara).
- [ ] Commit: `ui: Add MCP server cu cautare in registru si install`.

### Task 4: CLAUDE.md + verificare finala

- [ ] vet, build -race, test, cross-compile.
- [ ] CLAUDE.md: paragraf "Gata (install MCP din registru, spec in docs/superpowers/specs/2026-10-09-foreman-mcp-install-design.md): ..."; spec: `Status: implementat (2026-10-09).`
- [ ] Commit: `docs: install MCP din registru gata`.
