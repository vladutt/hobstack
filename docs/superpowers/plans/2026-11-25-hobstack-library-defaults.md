# Library defaults Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Un element publicat din Library (tool, skill, MCP) poate fi marcat "Default": intra imediat in toate proiectele
pe ultima versiune; un proiect il poate opri; cheile se pun o data global, cu override per proiect.

**Architecture:** Flag `library_items.is_default` + tabel `library_default_secrets`. Rezolvarea se face la citire:
`store.EffectiveInstalls(pid)` = randurile proiectului + instalari virtuale pentru default-urile fara rand;
`store.EffectiveSecrets` = global suprascris de proiect. Toti cititorii "ce are proiectul" trec pe ele. Oprirea = rand
`enabled = 0`. Pornirea unui default intr-un proiect = stergerea randului.

**Tech Stack:** Go (stdlib, modernc.org/sqlite), JS vanilla in internal/web/static/js.

**Spec:** `docs/superpowers/specs/2026-11-25-hobstack-library-defaults-design.md`

## Global Constraints

- `go vet ./...` si `go build -race ./...` trec; `go test ./...` trece.
- JS: copiaza fisierul ca .mjs si `node --check` (pe .js nu prinde erorile).
- Cross-compile: `GOOS=darwin GOARCH=arm64 go build ./cmd/hobstack` si `GOOS=windows go build ./cmd/hobstack`.
- Textele din UI / erori in engleza; comentariile din cod in romana.
- Valorile secretelor (globale sau per proiect) nu ies niciodata din API; doar numele si `set`.
- Doar elementele `status = published` se aplica ca default.
- Un rand `enabled = 0` nu e niciodata repornit de Publish sau de update.

## Review Focus

1. Proiect care a oprit un default, apoi se publica o versiune noua (si din proiectul sursa) -> ramane oprit (test in Task 2).
2. Flag scos si pus la loc -> proiectele care il oprisera raman oprite (test in Task 2).
3. Cheie globala schimbata -> build-ul se reface (hash diferit) si cheia nu apare in calea build-ului (test in Task 3).
4. Default fara nicio versiune / element unpublished cu flag -> nu apare in Build si nu da eroare (test in Task 2).
5. GET pe item / lista proiectului nu contine valoarea cheii globale (test in Task 5).

---

### Task 1: Migrarea 041, flag-ul si cheile globale in store

**Files:**
- Create: `internal/store/migrations/041_library_defaults.sql`
- Modify: `internal/store/library.go` (LibraryItem, libraryItemCols, scanLibraryItem, functii noi)
- Test: `internal/store/library_defaults_test.go`

**Interfaces:**
- Produces:
  - `LibraryItem.IsDefault bool` (`json:"isDefault"`)
  - `func (s *Store) SetLibraryDefault(itemID int64, on bool) error` — la `on = true`, in aceeasi tranzactie, sterge
    randurile `library_installs` ale elementului cu `enabled = 1` (proiectele trec pe default); cele `enabled = 0` raman.
  - `func (s *Store) SetDefaultSecret(itemID int64, name, value string) error` — valoare goala = sterge randul.
  - `func (s *Store) DefaultSecrets(itemID int64) (map[string]string, error)` — harta goala daca nu are.
  - `func (s *Store) DefaultOffCount(itemID int64) (int, error)` — randuri `enabled = 0` ale elementului.

- [ ] **Step 1: Scrie migrarea**

```sql
-- Elemente din Library pornite in toate proiectele si cheile lor globale.
ALTER TABLE library_items ADD COLUMN is_default INTEGER NOT NULL DEFAULT 0;
CREATE TABLE library_default_secrets (
  item_id INTEGER NOT NULL REFERENCES library_items(id) ON DELETE CASCADE,
  name    TEXT NOT NULL,
  value   TEXT NOT NULL,
  PRIMARY KEY (item_id, name)
);
```

- [ ] **Step 2: Scrie testele care pica**

`TestSetLibraryDefault`: element publicat cu o versiune; proiectul A cu `Install` (enabled 1), B cu rand oprit
(`Install` apoi `DisableInstall` nu exista inca — foloseste `s.db.Exec("UPDATE library_installs SET enabled = 0 ...")`).
Dupa `SetLibraryDefault(id, true)`: `GetLibraryItem(id).IsDefault == true`; `ListInstalls(A)` nu mai are elementul;
`ListInstalls(B)` il are cu `Enabled == false`; `DefaultOffCount(id) == 1`. Dupa `SetLibraryDefault(id, false)`:
`IsDefault == false`, randul lui B ramane.

`TestDefaultSecrets`: `SetDefaultSecret(id, "TOKEN", "x")` -> `DefaultSecrets(id)["TOKEN"] == "x"`;
`SetDefaultSecret(id, "TOKEN", "")` -> harta goala.

- [ ] **Step 3: Ruleaza** `go test ./internal/store -run 'TestSetLibraryDefault|TestDefaultSecrets'` — FAIL (nedefinite).

- [ ] **Step 4: Implementeaza** — adauga `is_default` la `libraryItemCols` si la scan; functiile de mai sus.

- [ ] **Step 5: Ruleaza** `go test ./internal/store` — PASS (si testele vechi de library).

- [ ] **Step 6: Commit** `feat(library): default flag and global secrets in store`

---

### Task 2: Rezolvarea efectiva si semantica instalarii

**Files:**
- Modify: `internal/store/library.go` (`LibraryInstall`, `Install`, `InstallWithSecrets`, functii noi)
- Test: `internal/store/library_defaults_test.go`

**Interfaces:**
- Consumes: Task 1.
- Produces:
  - `LibraryInstall.FromDefault bool` (`json:"fromDefault"`)
  - `func (s *Store) EffectiveInstalls(projectID int64) ([]LibraryInstall, error)` — randurile proiectului (ca
    `ListInstalls`) + pentru fiecare element `is_default = 1 AND status = 'published'` fara rand in proiect si cu
    macar o versiune: `{ProjectID, ItemID, VersionID: cea mai noua (ordinea din ListLibraryVersions, primul), Enabled: true, FromDefault: true}`. Ordonat dupa item_id.
  - `func (s *Store) EffectiveSecrets(projectID, itemID int64) (map[string]string, error)` — `DefaultSecrets`
    suprascris de valorile nevide din `Secrets(projectID, itemID)`.
  - `func (s *Store) DisableInstall(projectID, itemID, versionID int64) error` — upsert cu `enabled = 0`.
  - `func (s *Store) IsActiveDefault(itemID int64) (bool, error)` — `is_default = 1 AND status = 'published'`.
  - `Install` schimbat: daca `IsActiveDefault` -> nu face nimic; altfel upsert care la conflict schimba doar
    `version_id` (nu mai pune `enabled = 1`; un rand nou e tot `enabled = 1`).
  - `InstallWithSecrets` schimbat: daca `IsActiveDefault` -> in tranzactie sterge randul proiectului (daca exista) si
    salveaza secretele nevide; altfel ca azi (upsert cu `enabled = 1`). Asa toate cele 5 drumuri din web (prin
    `installWithSecrets`) repornesc un default oprit fara alte schimbari.

- [ ] **Step 1: Scrie testele care pica**

`TestEffectiveInstalls`: element default publicat cu versiunile 1.0 si 1.1; proiect fara rand -> un rand cu
`VersionID` = 1.1, `Enabled`, `FromDefault`. Proiect cu `DisableInstall` -> randul lui, `Enabled == false`,
`FromDefault == false`. Un al doilea element default fara versiuni si un al treilea default dar `unpublished` -> lipsesc.
Element ne-default instalat explicit -> apare ca azi.

`TestInstallKeepsDisabled` (Review Focus 1): element ne-default, rand oprit cu `DisableInstall`; `Install(p, it, v2)`
-> `Enabled == false`, `VersionID == v2`. Element default oprit in proiect; `Install` -> randul ramane neschimbat.

`TestDefaultToggleKeepsOptOut` (Review Focus 2): default on, proiect oprit, `SetLibraryDefault(false)`, apoi `true`
-> `EffectiveInstalls` al proiectului are elementul cu `Enabled == false`.

`TestInstallWithSecretsOnDefault`: default oprit in proiect; `InstallWithSecrets(p, it, v, {"TOKEN":"p"})` ->
`ListInstalls(p)` fara element, `EffectiveInstalls(p)` il are `FromDefault`, `Secrets(p, it)["TOKEN"] == "p"`.

`TestEffectiveSecrets`: global `TOKEN=g`, `OTHER=o`; proiect `TOKEN=p`, `OTHER=""` -> `{TOKEN: p, OTHER: o}`.

- [ ] **Step 2: Ruleaza** `go test ./internal/store -run 'Effective|InstallKeeps|DefaultToggle|InstallWithSecretsOnDefault'` — FAIL.

- [ ] **Step 3: Implementeaza** functiile de mai sus.

- [ ] **Step 4: Ruleaza** `go test ./internal/store` — PASS (verifica ca `TestLibraryLifecycle` / `TestInstallWithSecretsAtomic` trec; daca un test vechi se bazeaza pe `Install` care reporneste un rand oprit, schimba asertia: asta e comportamentul nou cerut de spec).

- [ ] **Step 5: Commit** `feat(library): effective installs and secrets for defaults`

---

### Task 3: Build-ul foloseste instalarile si cheile efective

**Files:**
- Modify: `internal/library/library.go` (`Build` ~l.637, `inputsHash` ~l.770, cheile la ~l.692)
- Test: `internal/library/library_test.go`

**Interfaces:**
- Consumes: `EffectiveInstalls`, `EffectiveSecrets`, `SetLibraryDefault`, `SetDefaultSecret`, `DisableInstall`.

- [ ] **Step 1: Scrie testele care pica**

`TestBuildIncludesDefault`: skill publicat (ca in `TestBuildSkillsToolsAndMCP`), fara `Install`, `SetLibraryDefault(on)`
-> `Build(p)` are `skills/<nume>/SKILL.md`. Dupa `DisableInstall(p, ...)` -> un nou `Build(p)` nu-l mai are.

`TestBuildGlobalSecret` (Review Focus 3): MCP cu `Secrets: ["DATABASE_URL"]`, default,
`SetDefaultSecret(id, "DATABASE_URL", "secret-value-one")` -> `b1 := Build(p)`; env-ul din `b1.MCPServers["stock-db"]`
are `DATABASE_URL == "secret-value-one"`. `SetDefaultSecret(..., "secret-value-two")` -> `b2 := Build(p)`:
`b2.PluginDir != b1.PluginDir`, env-ul are `"secret-value-two"`, `!strings.Contains(b2.PluginDir, "secret-value")`.
Apoi `SetSecret(p, id, "DATABASE_URL", "project-own")` -> env-ul are `"project-own"`.

- [ ] **Step 2: Ruleaza** `go test ./internal/library -run 'TestBuildIncludesDefault|TestBuildGlobalSecret'` — FAIL.

- [ ] **Step 3: Implementeaza** — `Build` si `inputsHash` cheama `EffectiveInstalls` in loc de `ListInstalls` si `EffectiveSecrets` in loc de `Secrets`.

- [ ] **Step 4: Ruleaza** `go test ./internal/library` — PASS.

- [ ] **Step 5: Commit** `feat(library): build includes default items`

---

### Task 4: team — folosirile si cererile MCP

**Files:**
- Modify: `internal/team/library.go:138` (`recordLibraryUses`), `internal/team/mcp_requests.go:132`
- Test: testele existente din `internal/team` pentru mcp_requests (cauta `install_mcp` / `prepareMCPRequest` in `*_test.go`)

**Interfaces:**
- Consumes: `EffectiveInstalls`.

- [ ] **Step 1: Scrie testul care pica**

`TestInstallMCPDefaultTurnedOff`: element MCP default, oprit in proiect (`DisableInstall`); agentul cere `install_mcp`
cu acelasi nume -> se creeaza o cerere (`mcp_requests` deschisa, `ItemID` = elementul), raspunsul NU contine
"already installed". Pe un default pornit (fara rand) -> raspunsul contine "already installed".

- [ ] **Step 2: Ruleaza** testul — FAIL (azi default-ul fara rand nu e vazut ca instalat).

- [ ] **Step 3: Implementeaza** — in `mcp_requests.go` foloseste `EffectiveInstalls` si considera instalat doar un rand
cu `Enabled`; in `recordLibraryUses` `EffectiveInstalls`.

- [ ] **Step 4: Ruleaza** `go test ./internal/team` — PASS.

- [ ] **Step 5: Commit** `feat(team): defaults count as installed for uses and MCP requests`

---

### Task 5: API — flag-ul si cheile globale pe element

**Files:**
- Modify: `internal/web/api_library.go` (`itemView`, `itemViewOf`, `getLibraryItem` / `libraryDetail`), `internal/web/server.go` (ruta)
- Create: `internal/web/api_library_default.go`
- Test: `internal/web/library_defaults_test.go`

**Interfaces:**
- Consumes: Task 1, `s.skillInstalled(pid, it)` (api_access.go:192), `s.hub.Publish`.
- Produces:
  - `itemView` primeste `DefaultSecretsSet []string` (`json:"defaultSecretsSet"`, numele cu valoare) si
    `OffIn int` (`json:"offIn"`); `IsDefault` vine din `LibraryItem`.
  - `PUT /api/library/{id}/default` -> `func (s *Server) putLibraryDefault(w, r)`; corp
    `{"default": bool?, "secrets": {name: value}?, "clear": [name]?}`. Validare nume ca `checkSecretValues(it.Secrets, ...)`.
    `default: true` pe un element ne-published -> 409 `"only published items can be default"`. Valorile goale din
    `secrets` nu schimba nimic; `clear` sterge. Raspuns 200 cu `itemView`.
  - La pornire (era off, devine on): pentru fiecare proiect (`ListProjects`) in care `EffectiveInstalls` are elementul
    `Enabled`, `s.skillInstalled(pid, it)` si `s.hub.Publish(pid, "project", map[string]int64{"id": pid})`. La oprire:
    doar `hub.Publish` pe toate proiectele.

- [ ] **Step 1: Scrie testele care pica** (folosind `setupLib`, `e.propose`, `e.do`, `e.get`)

`TestLibraryDefaultAPI`: element publicat; `PUT .../default {"default": true, "secrets": {"TOKEN": "global-secret-123"}}`
-> 200, `isDefault == true`, `defaultSecretsSet == ["TOKEN"]`. GET `/api/library/{id}`, `/api/library`,
`/api/projects/{pid}/library` -> corpul nu contine `global-secret-123` (Review Focus 5). Element propus (nepublicat)
-> 409. `{"clear": ["TOKEN"]}` -> `defaultSecretsSet` gol. Nume necunoscut in secrets -> 400.

`TestLibraryDefaultClosesSkillCards`: skill cu un card "wanted the skill" deschis in proiect (ca in
`access_skill_test.go`); `PUT default true` -> cardul e inchis.

- [ ] **Step 2: Ruleaza** `go test ./internal/web -run 'TestLibraryDefault'` — FAIL.

- [ ] **Step 3: Implementeaza** handler-ul si campurile din view; inregistreaza ruta langa `PUT /api/projects/{id}/library/{item}`.

- [ ] **Step 4: Ruleaza** `go test ./internal/web` — PASS.

- [ ] **Step 5: Commit** `feat(web): mark library items as default with global keys`

---

### Task 6: API — lista si comutatorul pe proiect

**Files:**
- Modify: `internal/web/api_library.go` (`projectLibraryView`, `secretView`, `secretViews`, `listProjectLibrary` ~l.565, `putProjectLibrary` ~l.700)
- Test: `internal/web/library_defaults_test.go`

**Interfaces:**
- Consumes: Task 2 (`EffectiveInstalls`, `DisableInstall`, `InstallWithSecrets` default-aware), Task 5.
- Produces:
  - `projectLibraryView.FromDefault bool` (`json:"fromDefault"`), `Installed`/`Enabled`/`Version` calculate din
    `EffectiveInstalls` (default fara rand: `installed: true, enabled: true, version` = ultima, `updateAvailable: false`).
  - `secretView.Global bool` (`json:"global"`): cheia nu e setata in proiect dar are valoare globala.
    `Set` ramane "proiectul are valoare proprie".
  - `putProjectLibrary`: `cur` vine din `EffectiveInstalls`. Pe un element activ default:
    `installed: false` -> `DisableInstall(pid, item, ultima versiune)`; `installed: true` -> `installInProject` (care
    acum sterge randul de oprire); secretele per proiect se pot salva fara rand (nu mai intoarce "install the item
    before setting its secrets" cand elementul e default activ). Pe elemente ne-default comportamentul ramane.

- [ ] **Step 1: Scrie testele care pica**

`TestProjectLibraryDefaultToggle`: element default; GET `/api/projects/{pid}/library` -> randul are
`installed && enabled && fromDefault`. `PUT {"installed": false}` -> 204; GET -> `enabled == false`, `fromDefault == false`;
`EffectiveInstalls` are `Enabled == false`. `PUT {"installed": true}` -> GET -> `fromDefault == true`, `ListInstalls` fara rand.

`TestProjectLibraryGlobalSecretView`: default cu cheie globala `TOKEN`; GET pe proiect -> secretul `TOKEN` are
`global == true, set == false`. `PUT {"secrets": {"TOKEN": "own"}}` pe proiect fara rand -> 204, apoi `set == true`.

- [ ] **Step 2: Ruleaza** `go test ./internal/web -run 'TestProjectLibrary(DefaultToggle|GlobalSecretView)'` — FAIL.

- [ ] **Step 3: Implementeaza**.

- [ ] **Step 4: Ruleaza** `go test ./internal/web` — PASS (testele vechi din `library_test.go` raman verzi).

- [ ] **Step 5: Commit** `feat(web): project library shows and toggles defaults`

---

### Task 7: UI

**Files:**
- Modify: `internal/web/static/js/library.js` (pagina item-ului, ~l.380-410 si ~l.496; lista Library), `internal/web/static/js/library-cards.js` (`libraryInstalls`, `row` ~l.99), CSS-ul Library existent daca e nevoie

**Interfaces:**
- Consumes: `PUT /api/library/{id}/default`, campurile `isDefault`, `defaultSecretsSet`, `offIn` (Task 5); `fromDefault`, `secrets[].global` (Task 6).

- [ ] **Step 1: Pagina item-ului** — pentru elementele publicate: comutator "Default for all projects", hint
"On in every project unless a project turns it off." Sub el, cand `offIn > 0`: "Off in N project(s)". Pentru elementele cu
`secrets`: cate un camp parola per nume, hint "Used by every project that has no own value.", placeholder "Saved" cand
numele e in `defaultSecretsSet`, buton Clear; salvare prin PUT, apoi redesenare cu raspunsul.

- [ ] **Step 2: Lista Library** — eticheta "Default" langa nume pentru `isDefault`.

- [ ] **Step 3: Instructions > Library pe proiect** — eticheta "Default" pe randurile cu `fromDefault` sau
`item.isDefault`; comutatorul existent trimite `installed` ca azi; la cheile cu `global && !set` hint "Using the global value".

- [ ] **Step 4: Verifica sintaxa**

Run: `for f in library library-cards; do cp internal/web/static/js/$f.js /tmp/$f.mjs && node --check /tmp/$f.mjs; done`
Expected: fara output.

- [ ] **Step 5: Verifica in aplicatie** — porneste un build local pe alt port / alt folder de date (NU reporni instanta care
ruleaza daca `pgrep -f "claude -p"` gaseste ceva), marcheaza un skill default, verifica cu screenshot Chrome headless pagina
item-ului si Instructions > Library dintr-un proiect (eticheta Default, comutator, "Using the global value").

- [ ] **Step 6: Commit** `feat(ui): library defaults`

---

### Task 8: Verificari finale si CLAUDE.md

**Files:**
- Modify: `CLAUDE.md` (sectiunea Stare, un paragraf "Gata (Library defaults, spec in docs/superpowers/specs/2026-11-25-hobstack-library-defaults-design.md): ...")

- [ ] **Step 1: Ruleaza** `go vet ./... && go build -race ./... && go test ./...` — totul trece.
- [ ] **Step 2: Ruleaza** `GOOS=darwin GOARCH=arm64 go build ./cmd/hobstack && GOOS=windows go build ./cmd/hobstack` — trec (sterge binarele rezultate).
- [ ] **Step 3: Adauga paragraful in CLAUDE.md** (migrarea 041, EffectiveInstalls / EffectiveSecrets, oprire = rand enabled 0, pornirea flag-ului muta randurile pornite pe default, Install nu mai reporneste, PUT /api/library/{id}/default).
- [ ] **Step 4: Commit** `docs: library defaults in CLAUDE.md`
