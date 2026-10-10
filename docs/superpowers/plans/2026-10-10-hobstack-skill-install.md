# Add skill din Claude si din skills.sh / Anthropic: plan de implementare

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Library -> Skills -> "+ Add skill" cauta in skills.sh (Anthropic "Official" primele) si arata skill-urile din Claude (Claude Code, claude.ai, plugin-uri); Install in doi pasi (pregatire cu lista fisierelor, apoi creare publicata); versiune fixa pe sursa, "Update available" si Update cu diferente.

**Architecture:** Limita 200 fisiere / 10 MB pentru `kind = skill` in `internal/library`; tabelul `library_sources` (migrarea 031) + store. Pachet nou `internal/skillsrc`: clienti skills.sh / GitHub (commit, arhiva tar.gz citita ca stream, gasirea folderului), surse locale, amprenta. `internal/web`: endpoint-uri de cautare, local, prepare (folder de lucru cu token, 30 min), install, sursa + update. UI: `static/js/skill-add.js` + Update pe pagina elementului. Verificarea zilnica a surselor intr-o goroutine din `cmd/hobstack`.

**Tech Stack:** Go (net/http, archive/tar, compress/gzip, crypto/sha256, os.OpenRoot), JS vanilla.

**Spec:** `docs/superpowers/specs/2026-10-10-hobstack-skill-install-design.md`

## Global Constraints

- UI, mesaje, erori in engleza; comentariile din cod in romana; tot textul din UI prin `el()`.
- `go vet ./...`, `go build -race ./...`, `go test ./...`, `GOOS=darwin GOARCH=arm64 go build ./cmd/hobstack`, `GOOS=windows go build ./cmd/hobstack`; JS cu `node --check` pe o copie `.mjs`.
- Fara dependente noi. Catre skills.sh si GitHub: doar GET, fara credentiale, timeout 30 s, `User-Agent: HobStack`, din server (niciodata din browser).
- URL-uri de baza configurabile doar pentru teste: `HOBSTACK_SKILLS_URL` (implicit https://skills.sh), `HOBSTACK_GITHUB_API_URL` (https://api.github.com), `HOBSTACK_GITHUB_CODELOAD_URL` (https://codeload.github.com), `HOBSTACK_GITHUB_RAW_URL` (https://raw.githubusercontent.com); doar http/https cu host, altfel implicitul.
- Testele NU ies pe internet (httptest.Server, arhive tar.gz construite in test) si nu ating ~/.hobstack sau ~/.claude (HOME de test).
- Fisiere: doar fisiere obisnuite, fara symlink-uri si fara `.git`, caile prin `checkRel`; skill max 200 fisiere / 10 MB; arhiva max 50 MB comprimat; local prin `os.OpenRoot` pe folderul skill-ului.
- `owner/repo` si `skillId`: doar `[A-Za-z0-9._-]` pe fiecare parte, fara `..`, nevide.
- Migrarea: numarul se verifica la merge (ultima de pe main + 1; acum 031).
- Verificarea vizuala doar pe HOME gol cu `claude` fals (`/private/tmp/claude-501/fakebin`) si servere de test locale.

## Review Focus

1. **Arhiva GitHub ostila** (`../`, cale absoluta, symlink, hardlink, fisier imens, 201 fisiere, gzip bomb peste 50 MB): nimic scris in afara folderului de lucru, eroare clara. Test in Task 2.
2. **Skill fara folder clar in repo** (niciun SKILL.md cu numele, mai multe candidate): mesaj "Couldn't find the skill ...", alegere determinista. Test in Task 2.
3. **Cale locala falsificata de browser** (`/etc`, `~/.ssh`, `..`): 400, nimic citit. Test in Task 3.
4. **Pregatire expirata / token refolosit / doua Install pe acelasi token**: 410 / un singur element creat. Test in Task 3.
5. **Limita GitHub (403/429) la verificarea zilnica sau la Install**: verificarea se amana fara eroare in UI; la Install mesaj clar "GitHub is rate limiting requests; try again in a few minutes." Test in Task 2 si Task 5.

---

### Task 1: Limite pe tip + `library_sources`

**Files:** Modify `internal/library/library.go`, `internal/library/files.go` (limitele dupa tip), callers (`internal/web/api_library.go`, `internal/web/api_mcp_install.go`, `internal/team/library.go` daca foloseste CopyStaged/WriteStaged); Create `internal/store/migrations/031_library_sources.sql`, `internal/store/library_sources.go` + teste.

**Interfaces (Produces):**
```go
// library
func Limits(kind string) (files int, bytes int64)  // skill: 200, 10<<20; altfel 20, 1<<20
// WriteStaged si CopyStaged primesc kind ca prim argument:
func (l *Lib) WriteStaged(kind string, files []TextFile) (staging string, nfiles, lines int, err error)
func (l *Lib) CopyStaged(kind, srcDir string, files []string) (staging string, nfiles, lines int, err error)

// store
type LibrarySource struct {
    ItemID int64; Kind, Repo, Path, Ref, Label, Latest string; CheckedAt *time.Time
}
const (SourceGitHub = "github"; SourceClaudeCode = "claude-code"; SourceClaudeAI = "claude-ai"; SourcePlugin = "plugin")
func (s *Store) SetLibrarySource(src LibrarySource) error                 // upsert pe item_id
func (s *Store) GetLibrarySource(itemID int64) (LibrarySource, error)     // ErrNotFound
func (s *Store) ListLibrarySources() ([]LibrarySource, error)
func (s *Store) MarkSourceChecked(itemID int64, latest string, at time.Time) error
func (s *Store) FindLibrarySource(kind, repo, path string) (LibrarySource, error) // pentru inLibrary; ErrNotFound
```
Migrarea (schema din spec §5; `checked_at TEXT` NULL; FK cu ON DELETE CASCADE pe `library_items(id)`).

- [ ] Teste care pica: skill cu 150 de fisiere (si 5 MB) prin WriteStaged acceptat; tool cu 21 de fisiere respins; skill cu 201 respins; 11 MB respins; store: set/get/upsert/list/mark/find, cascada la stergerea elementului.
- [ ] RED, implementare, GREEN; `go vet`; testele existente din library/web/team trec dupa schimbarea semnaturilor.
- [ ] Commit: `library: limite pe tip (skill 200 fisiere / 10 MB) si library_sources`.

### Task 2: `internal/skillsrc`

**Files:** Create `internal/skillsrc/remote.go` (skills.sh + GitHub), `internal/skillsrc/archive.go` (extragere + gasirea folderului), `internal/skillsrc/local.go` (surse locale), `internal/skillsrc/fingerprint.go`, teste pentru fiecare.

**Interfaces (Produces):**
```go
var ErrUnavailable = errors.New("skills source unavailable")   // retea, status != 200, JSON invalid
var ErrRateLimited = errors.New("github rate limited")         // 403 cu X-RateLimit-Remaining: 0, sau 429
var ErrNotFound    = errors.New("skill not found")             // folderul nu exista in repo
type Remote struct { SkillsURL, APIURL, CodeloadURL, RawURL string; HTTP *http.Client }
func NewRemote() *Remote                                        // din variabilele HOBSTACK_* (Global Constraints)
type Hit struct { ID, Name, Source, SkillID string; Installs int; Official bool }
func (r *Remote) Search(ctx context.Context, q string) ([]Hit, error)          // q < 2 caractere -> nil, nil fara cerere; Official primele; cache 10 min
type Official struct { Name, Description, Path string }
func (r *Remote) Official(ctx context.Context) ([]Official, error)             // anthropics/skills/skills, descrieri din SKILL.md (raw); cache 1 h
func (r *Remote) HeadCommit(ctx context.Context, repo string) (string, error)  // commits/HEAD -> sha
func (r *Remote) PathCommit(ctx context.Context, repo, path string) (string, error) // commits?path=&per_page=1
func (r *Remote) Fetch(ctx context.Context, repo, sha, skillID, destDir string) (path string, err error)
    // descarca tar.gz/<sha>, gaseste folderul skill-ului (spec §3), scrie DOAR fisierele lui in destDir; intoarce calea folderului in repo
func ValidRepo(repo string) bool; func ValidSkillID(id string) bool

type LocalSkill struct { Source, Kind, Label, Name, Description, Path string } // Kind: claude-code | claude-ai | plugin
func LocalSkills(home string) []LocalSkill    // ~/.claude/skills/* (fara "synced"), synced/<cont>/* + manifest, plugin-uri din installed_plugins.json
func CopyLocal(src, destDir string) error     // OpenRoot pe src; doar fisiere obisnuite; limite
func Fingerprint(dir string) (string, error)  // sha256 peste (cale relativa + "\x00" + continut), cai sortate
type FileInfo struct { Path string; Size int64; Script bool }
func ListFiles(dir string) ([]FileInfo, error)
func Frontmatter(skillMD []byte) (name, description string)
```
Reguli: extensii script `.py .sh .js .mjs .ts .rb .pl .ps1 .bat .cmd` sau bit executabil; arhiva: primul nivel e `<repo>-<sha>/` si se scoate; se ignora symlink, hardlink, device, `.git/`; cale cu `..` sau absoluta -> eroare pentru toata arhiva; 200 fisiere / 10 MB pentru folderul ales (nu pentru tot repo-ul); citirea arhivei se opreste la 50 MB comprimat (`io.LimitReader` + eroare clara). Gasirea folderului: `skills/<id>` > folder numit `<id>` > frontmatter `name: <id>`; la egalitate calea cea mai scurta, apoi alfabetic.

- [ ] Teste care pica (httptest pentru skills.sh / api / codeload / raw; arhive construite cu archive/tar + gzip in test):
  - Search: Official primele, q de 1 caracter fara cerere, 500 -> ErrUnavailable, cache (a doua cerere identica fara retea);
  - Official: lista + descrieri; HeadCommit / PathCommit; 403 cu `X-RateLimit-Remaining: 0` si 429 -> ErrRateLimited;
  - Fetch: cele trei reguli de gasire, niciunul -> ErrNotFound, doua candidate egale -> cea mai scurta; `../evil`, cale absoluta, symlink, hardlink ignorate/respinse cum scrie mai sus; 201 fisiere; 11 MB; arhiva peste 50 MB;
  - LocalSkills: HOME de test cu cele trei surse, folder fara SKILL.md ignorat, symlink ca folder de skill ignorat, manifest lipsa -> descrierea din frontmatter, `installed_plugins.json` invalid ignorat;
  - CopyLocal (symlink in folder ignorat), Fingerprint stabil si schimbat cand se schimba un fisier, Frontmatter cu si fara ghilimele, ListFiles cu Script.
- [ ] RED, implementare, GREEN; `go vet`.
- [ ] Commit: `skillsrc: skills.sh, GitHub, skill-urile din Claude si amprenta`.

### Task 3: Web

**Files:** Create `internal/web/api_skill_install.go` + `internal/web/skill_install_test.go`; Modify `internal/web/server.go` (rute, `skills *skillsrc.Remote`, `home func() (string, error)` deja existent, pregatirile in memorie).

**Interfaces:** (spec §6)
- `GET /api/library/skills/search?q=` -> `{skills: [Hit + inLibrary]}`; q gol -> `{official: [Official + inLibrary]}`; ErrUnavailable -> 502 `"Skills search is not reachable right now. Try again in a minute."`.
- `GET /api/library/skills/local` -> `{skills: [LocalSkill + inLibrary]}`.
- `POST /api/library/skills/prepare` `{kind: "github", repo, skillId}` | `{kind: "local", path}` -> `{token, name, description, files: [FileInfo], source: {kind, repo, path, ref, label}}`. Local: `path` acceptat doar daca e in `LocalSkills(home)` (altfel 400). GitHub: `HeadCommit` + `Fetch` intr-un folder de lucru (`lib.NewStaging()`), `ref = sha`, `label = repo`; local: `CopyLocal`, `ref = Fingerprint`. ErrRateLimited -> 503 `"GitHub is rate limiting requests; try again in a few minutes."`; ErrNotFound -> 404 `"Couldn't find the skill <id> in <repo>."`; fara SKILL.md -> 422.
- Pregatirile: harta in memorie token (32 hex aleator) -> {dir, source, created}; expira dupa 30 min (folderul sters); `DELETE /api/library/skills/prepare/{token}` la inchiderea ferestrei.
- `GET /api/library/skills/prepare/{token}/file?path=` -> continut (MaxViewBytes, prin OpenRoot).
- `POST /api/library/skills/install` `{token, name, projectId?}` -> 201 element; validare `library.ValidateInput` (kind skill, files din folder); 409 nume luat; 410 token necunoscut/expirat; tokenul se consuma o singura data (sub lock); creare ca in `createLibraryItem` (declined -> `CopyStaged("skill", dir, files)` -> AdoptStaged -> PublishSkipTest), apoi `SetLibrarySource`; `projectId` -> instalat + activat (ca la MCP install). La eroare dupa creare: elementul si folderele sterse.
- `POST /api/library/items/{id}/source/check` -> `{source, updateAvailable}` (github: PathCommit; local: Fingerprint; ErrRateLimited -> raspuns 200 fara schimbare).
- `POST /api/library/items/{id}/source/prepare` -> ca prepare (din sursa salvata, ref nou) + `diff: [{path, change: "added"|"removed"|"modified", before, after}]` (text, max MaxViewBytes per fisier; binar -> fara before/after).
- `POST /api/library/items/{id}/source/update` `{token}` -> versiune noua: `CopyStaged` -> AdoptStaged pe item -> PublishSkipTest(note "Updated from <label>"), `ref = latest`.
- GET-ul elementului (`itemView`) primeste `source` (sau null) si `updateAvailable`.

- [ ] Teste care pica (servere de test + HOME de test): search cu inLibrary (dupa nume si dupa sursa); 502; local; prepare github -> fisiere + Script; prepare local cu cale din lista OK, cu `/etc`, `../`, `~/.ssh` -> 400; 503 la limita; 404 skill lipsa; file preview; install 201 cu versiunea 1 + sursa; 409; 410 dupa expirare (ceas injectabil) si la al doilea Install pe acelasi token; doua Install concurente pe acelasi token -> un singur element; projectId -> instalat si activat; check -> updateAvailable dupa un commit nou; source/prepare + update -> versiune noua, ref schimbat, diff corect.
- [ ] RED, implementare, GREEN; `go vet`, `go build -race ./...`.
- [ ] Commit: `web: Add skill din skills.sh, Anthropic si Claude, cu sursa si Update`.

### Task 4: UI

**Files:** Create `internal/web/static/js/skill-add.js`; Modify `internal/web/static/js/library.js` ("+ Add skill" pentru skills -> `#/library/skill/add`, formularul manual prin "Add manually"), `app.js` (ruta `#/library/skill/add[?project=]`), `library-cards.js` (link "Add skill" in Instructions > Library cu `?project=`), pagina elementului (Update available + fereastra cu diff, refoloseste `diffBlock` din changes.js si previzualizarea cu `highlight.js`), `app.css`.

- Pagina (spec §2): cautare (debounce 300 ms, AbortController ca la mcp-add), rezultate cu Official / sursa cu link GitHub / "208k installs" / Install sau "In library"; fara text: lista Official cu descriere; sectiunea "From your Claude" cu sursa; eroare 502 + Retry.
- Fereastra de install (spec §3): nume (validat `/^[a-z][a-z0-9-]{1,39}$/`), descriere, lista fisierelor cu marime si eticheta "script", click = previzualizare, sursa, "Enable in <proiect>", Install (confirmare pentru scripturi: `This skill has N scripts that agents can run. Install it?`); 409/410/503 cu mesajul serverului; la inchidere `DELETE` pe token; 201 -> pagina elementului.
- Pagina elementului: daca `source`: randul "From anthropics/skills @ 1ed29a0" / "From claude.ai" / "From Plugin: superpowers"; `updateAvailable` -> "Update available" + buton Update -> fereastra cu diff si "Update to <ref scurt>"; la deschidere `source/check` daca `checkedAt` e mai vechi de 1 h. Punct pe cardul din lista Library cand `updateAvailable`.
- [ ] `node --check` pe copii `.mjs`; verificare vizuala (HOME gol + claude fals + servere de test pentru skills.sh/GitHub + un ~/.claude de test cu cele trei surse) la 1440 si 390: cautare, Official, From your Claude, fereastra cu scripturi, Update available + diff; fara depasire orizontala.
- [ ] Commit: `ui: Add skill cu cautare, Official, From your Claude si Update`.

### Task 5: Verificarea zilnica + CLAUDE.md

**Files:** Modify `cmd/hobstack/main.go` (goroutine: la pornire si la fiecare ora, pentru sursele cu `checked_at` mai vechi de 24 h: check ca in Task 3; ErrRateLimited -> se opreste pana la ora urmatoare), eventual un helper comun in `internal/web` sau `internal/skillsrc` (`CheckSources(ctx, st, remote, home, now)`) cu test; `CLAUDE.md`; spec `Status: implementat (2026-10-10).`

- [ ] Test care pica: CheckSources marcheaza `latest` pentru o sursa github cu commit nou si pentru una locala schimbata, sare peste cele verificate in ultimele 24 h, se opreste la ErrRateLimited fara sa marcheze restul.
- [ ] vet, build -race, test, cross-compile.
- [ ] CLAUDE.md: paragraf "Gata (Add skill din Claude si skills.sh, spec in docs/superpowers/specs/2026-10-10-hobstack-skill-install-design.md): ..." inainte de "Urmeaza".
- [ ] Commit: `skills: verificarea zilnica a surselor; docs`.
