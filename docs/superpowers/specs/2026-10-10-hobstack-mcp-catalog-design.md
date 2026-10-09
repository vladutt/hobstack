# HobStack: catalog local al registrului MCP

Data: 2026-10-10
Status: spec scris, de aprobat.

## Scop

Cautarea din Library -> MCP -> + Add server merge azi direct la registrul oficial
(registry.modelcontextprotocol.io). Registrul raspunde greu la cautarile pe care nu le are in cache-ul lui: masurat pe
2026-10-09, intre 5 si 39 de secunde ("filesys" 29 s, "fileserver" 39 s); HobStack renunta dupa 10 s si arata "The MCP
registry is not reachable right now". Utilizatorul vrea: "sa preluam noi datele si sa cautam dupa aceea la noi; zilnic sa
preluam MCP-urile noi postate sau pe care nu le avem".

Decizii luate cu utilizatorul:
- Catalog local cu ultima versiune a fiecarui server din registru, intr-un fisier SQLite separat.
- Prima sincronizare completa in fundal, apoi zilnic doar ce s-a schimbat (`updated_since`).
- Cautarea se face doar local; pana e gata catalogul, cautarea merge la registru ca azi, cu timeout mai mare.
- Pe pagina Add MCP server: starea catalogului si "Update now".

In afara scopului: cautare full-text cu FTS5 / scoruri, versiuni vechi ale serverelor, alte registre, sincronizare
programata la o ora aleasa de utilizator.

## 1. Ce stim despre registru (masurat pe 2026-10-09)

- `GET /v0/servers?version=latest&limit=100&cursor=<c>`: 100 de servere pe pagina, `metadata.nextCursor` pentru urmatoarea.
  Primele 200 de pagini (20 000 de servere, ~18 MB JSON) in ~224 s; totalul e mai mare (cateva zeci de mii).
- `GET /v0/servers?updated_since=<RFC3339>&limit=100`: toate versiunile schimbate de atunci (nu doar ultima), in ~3 s.
  Fiecare intrare are `_meta["io.modelcontextprotocol.registry/official"]` cu `status` (`active` / `deprecated` /
  `deleted`), `isLatest`, `updatedAt`.
- Cererile pe pagini deja cerute de altcineva vin din cache-ul lor (sub 1 s); cele noi pot dura zeci de secunde.

## 2. Catalogul

Fisier: `~/.hobstack/mcp-registry.db` (langa baza principala, nu in ea: nu umfla backup-urile si se poate sterge oricand —
se reface singur). Deschis cu acelasi driver (modernc.org/sqlite), WAL, o conexiune. Schema proprie, versionata cu
`PRAGMA user_version` (versiune necunoscuta => fisierul se sterge si se reface).

```
servers
  name        TEXT PRIMARY KEY      -- io.github.user/foo
  version     TEXT NOT NULL
  title       TEXT NOT NULL DEFAULT ''
  description TEXT NOT NULL DEFAULT ''
  search      TEXT NOT NULL         -- lower(name + ' ' + title + ' ' + description), pentru cautare
  entry       TEXT NOT NULL         -- intrarea din registru (server + _meta), JSON compact
  updated_at  TEXT NOT NULL         -- _meta.updatedAt
meta
  key TEXT PRIMARY KEY, value TEXT NOT NULL
  -- full_done ("1"), full_cursor (cursorul paginii urmatoare cat timp prima sincronizare nu e gata),
  -- last_sync_at (inceputul ultimei sincronizari reusite, RFC3339), last_error, last_error_at
```

Se pastreaza intrarea bruta, nu `Server` normalizat: cand se schimba regulile de construire a comenzii (pachet nou
`registryType`, alte argumente), cautarea le aplica pe loc, fara descarcare noua. In catalog intra doar intrarile pe care
`normalize` le accepta azi (stdio npm/pypi/oci sau remote streamable-http/sse) — inclusiv cele afisate cu motiv
(`Installable=false`); restul (alt transport) nu se arata oricum.

## 3. Sincronizarea

Pachet: `internal/mcpregistry` (fisier nou `catalog.go`), tip `Catalog` cu `Open(path)`, `Sync(ctx)`, `Search(...)`,
`Status()`. Foloseste `BaseURL` din `NewClient()` (deci `HOBSTACK_MCP_REGISTRY_URL` in teste). Cererile catre registru:
doar GET, fara credentiale, timeout 60 s pe cerere, limita de 8 MiB pe raspuns (ca azi), 250 ms pauza intre pagini.

**Prima sincronizare** (`full_done` lipseste): `version=latest&limit=100`, pagina cu pagina, din `full_cursor`. Fiecare
pagina se scrie intr-o tranzactie impreuna cu noul `full_cursor`, deci un restart sau o eroare continua de unde a ramas.
Inainte de prima pagina se noteaza `sync_started` (in memorie); la ultima pagina: `full_done = 1`,
`last_sync_at = sync_started`, `full_cursor` sters.

**Actualizarea** (`full_done = 1`): `updated_since = last_sync_at - 1 h` (suprapunere, ca sa nu pierdem nimic la limita),
fara `version=latest`, toate paginile. Pentru fiecare intrare, dupa `_meta`:
- `status = active` si `isLatest = true` si `normalize` o accepta -> upsert (versiunea noua inlocuieste randul);
- `status = deleted` sau `deprecated`, pentru versiunea din catalog (`name` + `version` egale) -> randul se sterge;
- `isLatest = false` (versiune veche) sau `normalize` o refuza -> ignorata (daca era in catalog cu aceeasi versiune si
  normalize o refuza acum: se sterge).
La final `last_sync_at = inceputul acestei sincronizari`.

**Erori**: o cerere care pica se reincearca de 3 ori (2 s, 10 s, 30 s); apoi sincronizarea se opreste, `last_error` se
salveaza, iar urmatoarea incercare e la urmatorul tick. Datele deja scrise raman.

**Cand ruleaza**: o goroutine pornita din `cmd/hobstack` cu contextul aplicatiei (se opreste la Ctrl+C):
- la pornire: prima sincronizare (sau continuarea ei) daca `full_done` lipseste, altfel actualizarea daca
  `last_sync_at` e mai vechi de 24 h;
- apoi verificare la fiecare ora, cu aceeasi regula (prima sincronizare neterminata se reia si ea la ora);
- "Update now" din UI: porneste actualizarea (sau continua prima sincronizare) imediat.
O singura sincronizare la un moment dat (mutex; a doua cerere primeste "already running").

## 4. Cautarea

`Catalog.Search(query, cursor string) ([]Server, string, error)`, aceeasi forma ca `Client.Search`:
- textul se taie in cuvinte (spatii), litere mici, max 8 cuvinte; fiecare cuvant trebuie sa apara in `search`
  (`instr`, fara LIKE, ca `%` si `_` sa nu fie speciale);
- ordinea: numele (segmentul dupa ultimul `/`) egal cu textul cautat, apoi numele care contine primul cuvant, apoi restul;
  in fiecare grup dupa `updated_at` descrescator, apoi `name`;
- fara text: toate, dupa `updated_at` descrescator;
- 30 pe pagina; cursorul e offset-ul (text opac pentru UI, validat: numar >= 0, altfel 400);
- fiecare rand: `normalize(entry)` -> `Server` (randurile pe care normalize le refuza sunt sarite).

## 5. Server web

`GET /api/library/registry?search=&cursor=` (acelasi URL):
- catalog gata (`full_done`): cauta local; raspunsul are in plus `catalog` (vezi mai jos). Nu mai e 502 decat daca
  fisierul catalogului nu se poate citi.
- catalog in lucru: cauta la registru ca azi (`Client.Search`, cache 10 min), dar cu timeout 60 s; eroarea are mesajul
  nou "The MCP registry is slow or not reachable right now. Try again in a minute." (502), plus `catalog`.

`catalog`: `{ state: "building" | "ready", count, updatedAt (last_sync_at sau null), syncing: bool, lastError (text sau
null) }`.

`GET /api/library/registry/status` -> doar `catalog` (pentru reimprospatare in UI).
`POST /api/library/registry/sync` -> 202 si porneste sincronizarea; 409 `{error: "The catalog is already updating."}` daca
ruleaza deja.

## 6. UI (pagina Add MCP server)

- Sub campul de cautare, un rand mic:
  - gata: `Catalog: 31,204 servers · updated 3 h ago · Update now` (in timpul actualizarii: `Updating…` in loc de link);
  - in lucru: `Building local catalog… 12,400 servers so far. Search uses the live registry until it's done.`;
  - eroare (`lastError`): `Last update failed: <eroare scurta>` + `Update now`.
  Cat timp `syncing` e true, randul se reimprospateaza la 3 s prin `/status`; cand catalogul devine gata, cautarea
  curenta se reface (local).
- Cautarea: cererea precedenta se anuleaza (AbortController) cand se scrie din nou; cat asteapta registrul (doar in
  modul "building") apare `Searching the registry… this can take up to a minute.`.
- Restul paginii (rezultate, Install, From your Claude) ramane la fel.

## 7. Teste

Fara internet: registrul e un `httptest.Server` cu pagini salvate / construite in test.
- catalog: prima sincronizare pe 3 pagini; oprita dupa pagina 2 (eroare) si reluata din `full_cursor`; actualizare cu
  server nou, versiune noua (inlocuieste), versiune veche (ignorata), `deleted` pe versiunea curenta (sters), `deleted` pe
  alta versiune (nimic), intrare pe care normalize o refuza; `updated_since` trimis = `last_sync_at - 1 h`; reincercari
  si `last_error`; schema necunoscuta => fisier refacut.
- cautare: mai multe cuvinte, ordinea (nume exact primul), `%`/`_` ca text obisnuit, paginare cu cursor, cursor invalid,
  fara text.
- web: catalog gata => fara cerere la registru; in lucru => proxy la registru + `catalog.state = building`; `/status`;
  `/sync` 202 si 409 cand ruleaza.
- UI: `node --check`; screenshot-uri (instanta goala, claude fals, registru local de test) la 1440 si 390: catalog gata,
  in lucru, eroare.

## Etape

1. `Catalog` (fisier, schema, sincronizare completa + actualizare, cautare) cu teste.
2. Goroutine-ul de sincronizare in `cmd/hobstack` + endpoint-urile web.
3. UI (randul de stare, Update now, anulare cautare, mesajul de asteptare).
4. CLAUDE.md.
