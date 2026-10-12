# HobStack: explorer de cod (Cmd/Ctrl+P)

Data: 2026-10-12
Status: design aprobat in chat (sectiunile 1 si 2).

## Scop

Cand verifici ce au scris agentii sau vrei sa le arati un loc anume din cod, acum trebuie sa iesi din HobStack
(editor, terminal) si sa copiezi de mana calea si linia. Fisierele se vad doar daca au fost modificate pe un task
(modala de fisier din task).

Adaugam un explorer de cod doar de citire, deschis cu o combinatie de taste pe orice pagina a unui proiect: arbore
cu fisierele folderelor proiectului, cautare dupa nume, cod colorat cu numere de linie. Din el copiezi calea sau o
referinta de linie (`src/App.vue:12`, `src/App.vue:12-20`) si pornesti chat-ul cu liderul cu referinta deja scrisa.

In afara scopului (v1): editare, marcajele "changed on task" in explorer, link-uri in URL spre o linie, fisierele
altor proiecte, cautare in continutul fisierelor.

## UI

### Deschidere

- **Cmd+P** (macOS) / **Ctrl+P** (in rest) pe orice pagina a unui proiect (`#/p/<pid>/...`: board, Team, Files,
  Stats, cu sau fara modala de task deschisa). Interceptam scurtatura (`preventDefault`), deci pe paginile de
  proiect nu se mai deschide dialogul de print al browserului. In afara proiectelor (Proiecte, Library, Settings)
  scurtatura nu face nimic (browserul ramane cu print).
- Butonul **Code** din bara de sus a proiectului (`title="Code explorer (⌘P)"`, Ctrl+P pe alte sisteme), langa
  tab-uri.
- Aceeasi scurtatura sau Esc inchide explorer-ul. Esc inchide intai lista "⋯" sau selectia de linii, daca sunt
  deschise, apoi explorer-ul.
- Explorer-ul sta peste orice (inclusiv peste modala de task si peste modala de fisier, z-index mai mare); la
  inchidere focusul revine unde era.

### Modala

Modala mare (ca modala de fisier, `max-width` ~1340px, inaltime `min(880px, 100%)`), trei zone:

- **Sus: cautarea** "Go to file…", focusata la deschidere. Cauta dupa nume in lista incarcata (aceeasi ordine ca
  `codefiles.SearchIdx`: numele incepe cu textul, numele contine textul, calea contine textul; la egalitate numele
  mai scurt), max 50 de rezultate intr-o lista sub camp. Sageti sus/jos aleg, Enter deschide fisierul (si il
  arata in arbore, cu folderele parinte desfasurate), Esc goleste cautarea. Fara text, lista nu apare.
- **Stanga: arborele**. O radacina pe folderul proiectului (numele folderului), restranse, cu exceptia celei care
  contine ultimul fisier deschis. Folderele se desfasoara la clic (si cu sageata dreapta / Enter pe tastatura);
  folderele inaintea fisierelor, alfabetic. Fisierul deschis e evidentiat. Un folder cu `truncated` are la final
  randul "Only the first 20000 files are listed; use search". Un folder cu `error` arata mesajul in locul
  arborelui ("Folder not found: <cale>").
- **Dreapta: codul**. Antet cu calea (folderul estompat, numele evidentiat) si butoanele Copy path, ⋯ (Copy absolute
  path). Codul colorat, cu numere de linie, doar de citire, ca in modala de fisier (`codeWithMarkers`, fara
  marcaje). Fara fisier deschis: "Pick a file on the left or search with ⌘P." Fisier binar: "Binary file: no
  preview."; peste 1 MB: "The file is larger than 1 MB: no preview."; disparut intre timp: "The file no longer
  exists." (si arborele se reincarca).

Pe ecran ingust (<= 720px) arborele trece deasupra codului (max 35% din inaltime), ca in modala de fisier.

### Selectia de linii si referinte

- Clic pe numarul unei linii o selecteaza (fundal evidentiat); Shift+clic pe alt numar selecteaza intervalul.
  Clic pe aceeasi linie selectata deselecteaza. Selectia se sterge la schimbarea fisierului.
- Cu o selectie, deasupra primei linii selectate (lipita de cod, sticky in zona vizibila) apare o bara mica:
  - **Copy reference** → `<cale>:42` sau `<cale>:42-50`;
  - **Ask <agent>** → vezi mai jos; sageata de langa el deschide lista agentilor proiectului (nedetasati) ca sa
    alegi altul.
- Scurtaturi in explorer:
  - **Cmd/Ctrl+C** fara text selectat in pagina: copiaza referinta (cu selectie de linii) sau calea (fara). Cu
    text selectat, copierea normala a browserului ramane neatinsa.
  - **Cmd/Ctrl+Shift+C**: copiaza calea fisierului deschis.
- Dupa copiere: toast "Copied <text>" (sau textul insusi daca clipboard-ul nu e disponibil, ca in modala de fisier).

### Cum se scriu caile

- **Copy path / Copy reference**: relativa in folder. Cu un singur folder: `src/App.vue`; cu mai multe, prefixata
  cu numele folderului (`web/src/App.vue`), la fel ca in `task_changes` (numele folderului, sau
  `store.FolderName(path)` cand e gol).
- **Copy absolute path** (din ⋯): `<folder.path>/<rel>`, cu separatorul sistemului.
- **Ask <agent>**: ca @ in chat, calea se scrie relativ la folderul agentului cand fisierul e acolo (agentul
  lucreaza in folderul lui: `agent.folderId`, gol = primul folder), absoluta altfel.

### Ask <agent>

- Agentul implicit: cel deschis in panoul lateral (drawer) daca exista, altfel prima radacina nedetasata
  (`firstRootId()`). Butonul arata numele lui ("Ask Foreman").
- La clic: explorer-ul se inchide, se deschide chat-ul agentului (`openAgent(id, "chat")`, ca butonul "Chat with
  ..."), iar campul de mesaj primeste `@<cale>:42-50 ` la inceput (textul deja scris ramane dupa referinta), cu
  cursorul dupa referinta si focus in camp. Nu se trimite nimic automat; agentul citeste singur fisierul, ca la @.
- Daca modala de task e deschisa, chat-ul se deschide langa ea (ca "Watch live"), fara sa o inchida.

### Ce se tine minte

Per proiect in localStorage, cheia `hobstack-code-<pid>` (citire/scriere in try/catch, ca restul cheilor):
`{ open: [<folderId>/<cale folder>...], last: { folder, path } }`. La redeschidere arborele are aceleasi foldere
desfasurate si ultimul fisier e deschis (daca mai exista in lista; altfel nimic deschis).

## API

### `GET /api/projects/{id}/tree`

Raspuns:

```json
{ "folders": [ { "id": 3, "name": "api", "path": "/Users/x/shop/api",
                 "files": ["app/Http/Kernel.php", "composer.json"], "truncated": false, "error": "" } ] }
```

- Lista e exact cea de la @ (`s.codeCache.Files(folder.Path)`): `git ls-files` cu `.gitignore` (inclusiv
  fisierele netrackuite neignorate), altfel parcurgere fara node_modules / vendor / dist / build / foldere ascunse;
  max 20000 pe folder (`truncated` = s-a atins limita), cache 30 s. Caile cu `/`.
- Un folder care nu se poate citi (sters de pe disc) vine cu `error` (mesaj pentru utilizator) si `files: []`;
  celelalte merg normal; raspunsul ramane 200.
- Proiect inexistent: 404.

`truncated` = lungimea listei a atins `codefiles.MaxFiles` (variabila exista deja).

### `GET /api/projects/{id}/code-file?folder=<folderId>&path=<rel>`

Raspuns:

```json
{ "folder": 3, "path": "app/Http/Kernel.php", "content": "...", "binary": false, "tooLarge": false }
```

Reguli (in ordine):
1. Proiectul exista (altfel 404); `folder` e un folder al proiectului (altfel 404).
2. `path` e nevid, relativ si local (`filepath.IsLocal` pe forma din sistem); altfel 400 "bad path".
3. `path` apare in lista folderului (`codeCache.Files`). Daca nu apare, cache-ul folderului se invalideaza si
   lista se citeste o data din nou (un fisier tocmai creat); daca tot nu apare: 404. Astfel un fisier ignorat de
   git (ex. `.env`) sau din node_modules nu se poate citi scriind calea in URL, chiar daca e in folder.
4. Citirea prin `os.OpenRoot(folder.Path)` (refuza `..` si symlink-urile care ies din folder → 404). Fisier
   disparut → 404. Peste 1 MB: `tooLarge: true`, fara `content`. NUL in primii 8 KB: `binary: true`, fara
   `content`.

Reutilizare: citirea e `readIn(root, rel)` din `api_code.go` (os.OpenRoot, limita `maxFileView` = 1 MB,
`errTooLarge`, `errOutside`); testul de binar (NUL in primii 8 KB) din `getTaskFile` se scoate intr-o functie mica
`isBinary(content)` folosita de ambele endpointuri.

`Cache` primeste `Invalidate(dir)` pentru pasul 3.

## Front-end

- **`static/js/code-explorer.js`** (fisier nou): `openCodeExplorer({ projectId, project, agentId, onAsk })` →
  `{ close() }`. Contine modala, arborele, cautarea, selectia de linii si bara de referinte. `onAsk(agentId, text)`
  e functia din workspace care deschide chat-ul si pune textul.
- **`static/js/file-view.js`**: exporta `codeWithMarkers` (folosit fara marcaje) si, daca e nevoie, functia de
  copiere cu toast.
- **`static/js/workspace.js`**:
  - ascultator `keydown` pe document pentru Cmd/Ctrl+P cat timp workspace-ul exista (scos in `destroy`);
    deschide / inchide explorer-ul;
  - butonul Code in bara de sus (`renderHeader`);
  - `prefillChat(agentId, text)`: `openAgent(agentId, "chat")` (fara toggle: daca chat-ul e deja deschis pe acel
    agent ramane deschis), apoi pune textul la inceputul campului, muta cursorul dupa el si focuseaza.
- **`static/app.css`**: stilurile explorer-ului (`.cx-*`), refolosind variabilele si stilurile din `.fv-*`
  (linii, numere, arbore).

## Erori

- `tree` esuat (retea / server): mesaj in locul arborelui cu butonul Retry; cautarea dezactivata.
- `code-file` esuat: mesajul in zona de cod; arborele ramane. 404 pe un fisier din lista: "The file no longer
  exists." si arborele se reincarca (lista poate fi veche de pana la 30 s).
- Clipboard indisponibil (http fara permisiune): toast cu textul, ca sa-l poti copia de mana.

## Teste

Go (`internal/web/api_code_tree_test.go`, cu `setup` si un proiect pe un folder temporar):
- `tree`: fisierele apar; `node_modules/x.js` si un fisier din `.gitignore` (folder cu `git init`) nu apar; un al
  doilea folder sters de pe disc vine cu `error` si raspunsul e 200; proiect inexistent 404.
- `code-file`: citire OK (continut); cale care nu e in lista (fisier ignorat existent) 404; `../x` si cale
  absoluta 400; symlink din folder catre un fisier din afara (in lista prin parcurgere) refuzat (404); fisier cu
  NUL → `binary`; fisier > 1 MB → `tooLarge` fara continut; folder al altui proiect 404; fisier creat dupa ce
  lista a intrat in cache se citeste (invalidarea).
- `codefiles`: `Invalidate` forteaza recitirea.

JS: `node --check` pe fisierele schimbate (copiate ca .mjs). Verificare in Chrome headless prin CDP, la 1440x900
si 800x800: Cmd+P deschide (si inchide) explorer-ul de pe board si de peste modala de task; cautarea gaseste si
deschide un fisier; arborele il arata; clic pe linia 42 + Shift+clic pe 50 arata bara; Copy reference pune
`<cale>:42-50` in clipboard (citit prin `Runtime.evaluate` cu permisiunea de clipboard data prin
`Browser.grantPermissions`); Ask deschide chat-ul radacinii cu `@<cale>:42-50 ` in camp; screenshot-uri citite.

`go vet ./...`, `go build -race`, cross-compile darwin/arm64 si windows, ca de obicei.
