# HobStack: Add skill din Claude si din skills.sh / Anthropic

Data: 2026-10-10
Status: implementat (2026-10-10).

## Scop

Azi un skill intra in Library doar propus de un agent (`propose_library_item`) sau scris de mana. Utilizatorul vrea, ca la
MCP: "la Add skill sa citim skill-urile deja existente in Claude + sa avem un provider ca la MCP; poate chiar Claude".

Decizii luate cu utilizatorul:
- Provider online: **skills.sh** (cautare in tot ecosistemul) cu skill-urile **Anthropic** (`anthropics/skills`) marcate
  "Official" si puse primele.
- Surse locale ("From your Claude"): `~/.claude/skills` (Claude Code), `~/.claude/skills/synced` (claude.ai) si skill-urile
  din plugin-urile Claude Code instalate. Nu: `.claude/skills` din folderele proiectelor.
- Limita pentru **toate** skill-urile devine 200 de fisiere / 10 MB (azi 20 / 1 MB pentru orice element).
- Versiune fixa la install + "Update available" cand sursa s-a schimbat; Update = versiune noua dupa ce vezi diferentele.

In afara scopului: plugin-uri intregi (doar skill-urile din ele), skill-urile din folderele proiectelor, publicarea pe
skills.sh, alte registre, update automat fara confirmare.

## 1. Ce stim despre surse (verificat pe 2026-10-10)

- skills.sh: `GET https://skills.sh/api/search?q=<text>` (minim 2 caractere, altfel 400) ->
  `{skills: [{id: "anthropics/skills/pdf", source: "anthropics/skills", skillId: "pdf", name: "pdf", installs: 208236}],
  count}` (max 100, ~1 s). Fara descriere.
- GitHub (fara cont, 60 cereri API / ora / IP):
  - `GET https://api.github.com/repos/<owner>/<repo>/commits?path=<cale>&per_page=1` -> ultimul commit care atinge calea;
  - `GET https://api.github.com/repos/<owner>/<repo>/commits/HEAD` -> commit-ul curent al ramurii implicite;
  - `GET https://api.github.com/repos/anthropics/skills/contents/skills` -> cele 19 skill-uri oficiale;
  - arhiva: `https://codeload.github.com/<owner>/<repo>/tar.gz/<sha>` (nu consuma din limita API; anthropics/skills ~4 MB);
  - fisiere brute: `https://raw.githubusercontent.com/<owner>/<repo>/<sha>/<cale>`.
- Local:
  - `~/.claude/skills/<nume>/SKILL.md` (+ alte fisiere); folderul `synced` e special (mai jos);
  - `~/.claude/skills/synced/<cont>/<nume>/` + `manifest.json` (`skills: [{skillId, name, description, source,
    creatorType, updatedAt}]`);
  - plugin-uri: `~/.claude/plugins/installed_plugins.json` -> `plugins["<nume>@<marketplace>"][]{installPath, version,
    gitCommitSha}`; skill-urile sunt in `<installPath>/skills/<nume>/SKILL.md`.
- Marimi: docx 61 fisiere / 1,2 MB, pptx 57 / 1,25 MB, xlsx 53 / 1,2 MB; restul sub 20 fisiere si 300 KB.

## 2. Ecranul

Library -> Skills -> "+ Add skill" (pagina `#/library/skill/add[?project=<id>]`; formularul de azi ramane prin link
"Add manually"):

- **Search skills.sh**: camp de cautare (debounce 300 ms, minim 2 caractere). Rezultate: nume, sursa (`owner/repo`, link
  catre GitHub), numarul de instalari ("208k installs"), eticheta **Official** pentru `anthropics/skills`, Install.
  Rezultatele Official primele, apoi ordinea skills.sh.
- **Official (Anthropic)**: fara text in cautare, lista celor din `anthropics/skills` (nume + descriere din SKILL.md).
- **From your Claude**: sectiune ca la MCP, cu sursa fiecaruia: "Claude Code", "claude.ai", "Plugin: <nume>" + descriere
  (frontmatter `description` din SKILL.md; pentru claude.ai din manifest).
- "In library" in loc de Install cand exista deja un skill cu acelasi nume sau din aceeasi sursa (vezi §5).
- Eroare la skills.sh / GitHub: mesaj + Retry; "From your Claude" ramane utilizabil.

## 3. Install

Doi pasi:

1. **Pregatire** (click pe Install): serverul aduce skill-ul intr-un folder de lucru (`library/.staging-*`):
   - GitHub (din skills.sh sau Official): commit-ul curent (`commits/HEAD`), arhiva `tar.gz/<sha>` citita ca stream,
     pastrat doar folderul skill-ului (vezi mai jos); calea folderului in repo se tine minte.
   - local: copia folderului.
   Raspunsul: nume sugerat, descriere (frontmatter), lista fisierelor cu marimea, care sunt scripturi (extensiile `.py
   .sh .js .mjs .ts .rb .pl .ps1 .bat .cmd` sau bitul executabil), sursa, commit-ul. Folderul de lucru se sterge dupa
   30 min sau cand fereastra se inchide.
2. **Fereastra**: nume in Library (sugerat din `name` din frontmatter / numele folderului, forma `[a-z0-9-]`, editabil),
   descrierea, lista fisierelor (scripturile cu eticheta "script"; click = previzualizare cu cod colorat, ca in Library),
   sursa ("anthropics/skills @ 1ed29a0", "claude.ai", "Plugin: superpowers 6.4.1"), bifa "Enable in <proiect>" cand pagina
   a primit `?project=`, buton **Install**. Pentru skill-urile cu scripturi butonul cere o confirmare: "This skill has N
   scripts that agents can run. Install it?".
3. Install creeaza elementul `kind = skill`, `status = published`, `made_by = You`, cu versiunea 1 din folderul pregatit
   (acelasi drum ca un element facut de utilizator: `WriteStaged` / `AdoptStaged` sau echivalentul pentru un folder),
   salveaza sursa (§5) si, daca e bifat, il instaleaza si il activeaza in proiect (ca la MCP install).

**Gasirea folderului in arhiva GitHub**: toate folderele care contin `SKILL.md`; se alege, in ordine: `skills/<skillId>`,
apoi un folder numit `<skillId>`, apoi unul al carui frontmatter are `name: <skillId>`. Niciunul -> "Couldn't find the
skill <skillId> in <owner/repo>." Mai multe la acelasi nivel de preferinta -> cel cu calea cea mai scurta.

**Reguli pentru fisiere** (GitHub si local): doar fisiere obisnuite (fara symlink-uri, fara fisiere speciale), caile trec
prin `checkRel` (nu ies din folder, fara `..`), fara foldere ascunse `.git`; max 200 fisiere / 10 MB in total; arhiva se
citeste cu limita de 50 MB comprimat si se opreste la depasire. Local: citit prin `os.OpenRoot` pe folderul skill-ului.

## 4. Limite

`maxFiles = 200`, `maxBytes = 10 MB` pentru `kind = skill` (propuse de agenti, scrise de mana sau importate). Tool-urile
si MCP raman la 20 / 1 MB. Previzualizarea unui fisier ramane la `MaxViewBytes` (512 KB).

## 5. Sursa si Update

Tabel nou (migrarea urmatoare de pe main):
```
library_sources
  item_id   INTEGER PRIMARY KEY REFERENCES library_items(id) ON DELETE CASCADE
  kind      TEXT NOT NULL      -- 'github' | 'claude-code' | 'claude-ai' | 'plugin'
  repo      TEXT NOT NULL DEFAULT ''   -- owner/repo (github)
  path      TEXT NOT NULL DEFAULT ''   -- calea folderului in repo / pe disc
  ref       TEXT NOT NULL DEFAULT ''   -- commit-ul instalat (github) sau amprenta continutului (local)
  label     TEXT NOT NULL DEFAULT ''   -- "anthropics/skills", "Plugin: superpowers"
  latest    TEXT NOT NULL DEFAULT ''   -- ultima referinta vazuta la sursa
  checked_at TEXT                       -- ultima verificare
```
Amprenta locala: sha256 peste (cale relativa + continut) al fisierelor, in ordine.

**Verificare**: o data pe zi (ticker-ul existent din `team.Run` sau o goroutine ca la catalogul MCP) si la deschiderea
paginii elementului daca `checked_at` e mai vechi de 1 h:
- github: `commits?path=<path>&per_page=1` -> sha; diferit de `ref` -> `latest = sha`. Raspuns 403/429 (limita) ->
  verificarea se amana, fara eroare in UI.
- local: amprenta folderului de acum (folder disparut -> fara update, nota "Source no longer on this machine").
"Update available" pe pagina elementului (si un punct pe card) cand `latest != '' && latest != ref`.

**Update**: pregatire ca la install (din `latest`), apoi fereastra cu diferentele pe fisiere (adaugate / sterse /
modificate, diff text cu `diffBlock`), confirmare -> versiune noua a elementului (drumul de azi pentru o versiune noua),
`ref = latest`. Proiectele trec pe versiunea noua ca la orice versiune noua din Library.

"In library": exista un skill cu acelasi nume, sau o sursa cu acelasi (kind, repo, path) / (kind, path).

## 6. Server

- `GET /api/library/skills/search?q=` -> `{skills: [{id, name, source, repo, skillId, installs, official, inLibrary}]}`;
  q gol -> lista Official (cache 1 h; descrierile din SKILL.md prin raw.githubusercontent.com). skills.sh / GitHub
  indisponibil -> 502 `{error: "Skills search is not reachable right now. Try again in a minute."}`. Cache 10 min pe q.
- `GET /api/library/skills/local` -> `{skills: [{source: "Claude Code"|"claude.ai"|"Plugin: <nume>", name, description,
  path, inLibrary}]}`.
- `POST /api/library/skills/prepare` `{kind: "github", repo, skillId}` | `{kind: "local", path}` -> `{token, name,
  description, files: [{path, size, script}], source}`; `path` local trebuie sa fie unul din cele intoarse de `/local`
  (altfel 400).
- `GET /api/library/skills/prepare/{token}/file?path=` -> continutul unui fisier pregatit (pentru previzualizare).
- `POST /api/library/skills/install` `{token, name, projectId?}` -> 201 element | 400 | 409 nume luat | 410 pregatirea a
  expirat.
- `POST /api/library/items/{id}/source/check` -> starea sursei; `POST /api/library/items/{id}/source/prepare` -> ca
  prepare, plus `diff`; `POST /api/library/items/{id}/source/update {token}` -> versiunea noua.
- Cererile catre skills.sh si GitHub le face serverul (GET, fara credentiale, timeout 30 s, `User-Agent: HobStack`),
  browserul ramane pe localhost. URL-urile de baza configurabile pentru teste (`HOBSTACK_SKILLS_URL`,
  `HOBSTACK_GITHUB_API_URL`, `HOBSTACK_GITHUB_CODELOAD_URL`, `HOBSTACK_GITHUB_RAW_URL`, doar http/https).

## 7. Siguranta

- Un skill poate contine scripturi pe care agentii le ruleaza: fisierele si scripturile se vad inainte de install,
  scripturile cer confirmare; versiunea e fixata pe commit.
- Datele de la skills.sh / GitHub sunt externe: afisate prin `el()`, numele trec prin validarea Library.
- Repo-ul si skillId din cerere: `owner/repo` cu `[A-Za-z0-9._-]`, skillId cu `[A-Za-z0-9._-]`, fara `..`.
- Din folderele locale se citeste doar folderul skill-ului (OpenRoot), nimic din afara; caile primite de la browser sunt
  acceptate doar daca sunt in lista intoarsa de `/local`.

## 8. Teste

Fara internet: skills.sh si GitHub inlocuite de `httptest.Server` (arhive tar.gz construite in test).
- cautare: rezultate Official primele, `inLibrary`, 502 la eroare, q de 1 caracter -> lista goala fara cerere, cache.
- local: cele trei surse din fisiere temporare (HOME de test), manifest claude.ai, plugin din `installed_plugins.json`,
  folder fara SKILL.md ignorat, symlink ignorat.
- gasirea folderului in arhiva (cele trei reguli, niciunul, mai multe), arhiva cu `../`, symlink, 201 fisiere, 11 MB,
  arhiva peste 50 MB.
- install: element publicat cu versiunea 1 si sursa; 409; 410 dupa expirare; enable in proiect; scripturi marcate.
- limite: skill cu 150 de fisiere acceptat, tool cu 21 respins.
- update: commit nou -> Update available; local schimbat -> Update available; 403 de la GitHub -> fara eroare; update ->
  versiune noua, `ref` schimbat, diff corect.
- UI: screenshot-uri (instanta goala, claude fals, surse de test) la 1440 si 390: cautare, Official, From your Claude,
  fereastra de install cu scripturi, Update available + diff.

## Etape

1. Limita 200 / 10 MB pentru skill-uri + tabelul `library_sources` (migrare) + store.
2. Pachet `internal/skillsrc`: skills.sh, GitHub (commit, arhiva, gasirea folderului), surse locale, amprenta.
3. Web: search, local, prepare / file / install, sursa si update.
4. UI: pagina Add skill, fereastra de install, Update available + diff.
5. Verificarea zilnica a surselor + CLAUDE.md.
