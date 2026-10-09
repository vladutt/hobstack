# Foreman: cod colorat, tab-ul Modified si modala de fisier cu marcaje

Data: 2026-10-09
Status: spec, de implementat.

## Scop

Codul apare azi ca text simplu in trei locuri: diff-urile din tab-ul Changes (`changes.js`), blocurile de cod din
markdown (`md.js`: chat, rapoarte, Conversation) si previzualizarea text din tab-ul Files (`task-files.js`). Utilizatorul
vrea:
1. evidentiere de sintaxa pentru multe limbaje, nu doar PHP;
2. pe task, un tab separat cu fisierele modificate;
3. un clic pe un fisier modificat deschide o modala cu codul lui;
4. "highlight de git": in modala, liniile schimbate de task marcate pe margine, ca in IDE.

Decizii luate cu utilizatorul:
- Doar evidentiere si vizualizare, fara editor (editorul ar fi o felie separata).
- Marcajele vin din diff-urile pe care le avem deja (`task_changes`), fara git: merg si in proiectele care nu sunt repo git.
  Un buton comuta pe vederea de diff.

In afara scopului: editare, `git diff` fata de HEAD, colorare in chat-ul cu agentul pentru numele de fisiere (acolo nu
exista context de task), modificarile facute prin comenzi Bash (nu au diff).

## 1. Evidentierea de sintaxa

Biblioteca: highlight.js 11 (build-ul "common", ~35 de limbaje: PHP, JavaScript/TypeScript, Go, Python, SQL, JSON,
YAML, Bash, CSS/SCSS, HTML/XML, Java, C#, C/C++, Rust, Ruby, Kotlin, Swift, Markdown, Diff, INI...). Pusa o singura data
in `internal/web/static/vendor/highlight/` (`highlight.min.js` + `LICENSE`, BSD-3), inclusa in binar ca restul UI-ului,
deci merge fara internet. Culorile: clasele `hljs-*` primesc culori din tokenii temei (CSS-ul nostru, nu o tema
highlight.js), ca sa se potriveasca cu tema ardezie.

Un modul nou `static/js/highlight.js`:
- `languageFor(path)`: limbajul dupa extensie (`.php` -> php, `.blade.php` -> php, `.ts/.tsx` -> typescript,
  `.vue/.svelte/.twig` -> xml, `.yml` -> yaml, fara extensie cunoscuta -> null);
- `highlightInto(el, text, language)`: pune HTML-ul colorat in `el` (highlight.js scapa textul); `language` null =
  `highlightAuto` limitat la limbajele uzuale; text peste 5000 de linii sau 500 KB = text simplu, fara colorare;
- `highlightLine(text, language)`: o singura linie (pentru diff), cu `ignoreIllegals`.

Se aplica:
- `md.js`: blocurile ``` primesc limbajul din eticheta blocului (```php), altfel auto; blocurile de diff (+/-) raman
  cum sunt, cu liniile colorate dupa limbajul eticheta-ului daca exista;
- `changes.js`: fiecare linie de diff colorata dupa extensia fisierului; fundalul verde/rosu si prefixul +/- raman;
  colorarea se face abia la deschiderea diff-ului (ca acum, `ensureDiff`);
- `task-files.js`: previzualizarea text, dupa extensie;
- modala de fisier (sectiunea 3).

Limita stiuta: un comentariu sau un string pe mai multe linii, taiat de marginea unui fragment de diff, poate iesi
colorat gresit pe acele linii (diff-ul se coloreaza linie cu linie).

## 2. Tab-ul Modified

In modala de task, un tab nou **Modified**, langa Changes (Changes ramane cum e, cu istoria pe run-uri).

`GET /api/tasks/{id}/modified` intoarce fisierele modificate de task si de sub-task-urile lui (recursiv, prin
`ParentTaskID`), cate un rand pe (task, cale):

```json
{ "files": [ { "taskId": 12, "ref": "T-12", "path": "app/Domains/Awb/X.php", "added": 14, "removed": 3,
               "created": false, "edits": 4, "agent": "Atlas", "at": "2026-10-09T12:10:28Z" } ] }
```

- `added` / `removed`: suma liniilor +/- din toate diff-urile fisierului pe acel task;
- `created`: prima modificare a fost `create` (Write pe un fisier nou);
- `agent` / `at`: ultima modificare;
- ordinea: intai task-ul curent, apoi sub-task-urile (dupa numar), in fiecare dupa cale.

UI: un rand pe fisier (folderul estompat, numele evidentiat, `+N −M`, "new", ultimul agent si ora; la sub-task-uri
eticheta "T-12" langa cale). Clic pe rand = modala de fisier. Eticheta tab-ului arata numarul de fisiere. Gol:
"No file changes recorded for this task." plus nota existenta despre comenzile Bash.

## 3. Modala de fisier

Se deschide din:
- tab-ul Modified (clic pe rand);
- tab-ul Changes: un buton mic "View file" langa fiecare fisier (clicul pe nume ramane cel care deschide diff-ul);
- rapoartele din modala de task: numele de fisiere care au eticheta +/- (`md-file`) devin clicabile.

`GET /api/tasks/{id}/file?path=<cale>&task=<id sub-task optional>`:

```json
{ "path": "app/X.php", "content": "...", "exists": true, "binary": false, "tooLarge": false,
  "markers": { "added": [12, 13, 14], "modified": [20], "deleted": [31] }, "exact": true,
  "hunks": [ { "agent": "Atlas", "at": "...", "hunks": [ ... ] } ] }
```

Siguranta: calea trebuie sa apara in `task_changes` pentru task-ul cerut (`task`, care trebuie sa fie task-ul din URL
sau un sub-task al lui); altfel 404. Rezolvarea caii:
- relativa: fata de folderul agentului care a facut modificarea (`HomeFolder(agent, folders)`); daca agentul nu mai
  exista, se incearca folderele proiectului in ordine;
- absoluta: trebuie sa fie intr-un folder al proiectului (sau in `projects/<pid>/files`).
Citirea se face prin `os.OpenRoot(folder)` + calea relativa la el (ca in `internal/taskfiles`), deci `..` si
symlink-urile in afara folderului sunt refuzate.

Limite: peste 1 MB -> `tooLarge: true`, fara continut; un octet NUL in primii 8 KB -> `binary: true`, fara continut;
fisierul lipsa -> `exists: false` (vederea Diff ramane disponibila).

Marcajele (Go, `store.FileMarkers(changes []TaskChange) (Markers, bool)`), calculate pe toate diff-urile fisierului din
task, in ordinea lor:
- se pleaca de la un fisier fara marcaje; fiecare diff se aplica pe rand: liniile `+` din fragment devin "added", sau
  "modified" cand in acelasi fragment, imediat inainte, sunt linii `-` (inlocuire); liniile `-` fara `+` dupa ele lasa
  un marcaj "deleted" pe linia de dupa locul stergerii;
- un diff nou deplaseaza marcajele de sub el cu `newLines - oldLines`; marcajele din zona inlocuita de un fragment nou
  se recalculeaza din acel fragment;
- `Write` pe un fisier existent (kind `update`) fara fragmente: tot fisierul "modified"; `create`: tot fisierul "added";
- un diff fara numere de linie (`NewStart == 0`, fallback fara `structuredPatch`) sau trunchiat -> `exact: false`,
  fara marcaje.

UI-ul modalei (`static/js/file-view.js`):
- titlu = calea (folderul estompat), `+N −M`, buton **Copy path**, buton **Diff** / **File** (comutare), inchidere cu
  Esc si clic in afara (ca modala de task);
- vederea File: numerele de linie, marcajul pe margine (verde = added, albastru = modified, triunghi rosu intre linii =
  deleted), codul colorat (`highlightInto`), scroll la primul marcaj;
- nota cand `exact` e fals: "This task's changes can't be placed on lines (a change was too large or had no line
  numbers); see Diff."; altfel o nota mica: "Markers show this task's changes; the file may have changed since.";
- `exists: false`: "The file no longer exists." si vederea Diff deschisa; `binary` / `tooLarge`: mesajul potrivit si
  Diff.
- vederea Diff: diff-urile task-ului pentru fisier, pe run-uri (agent, ora), colorate ca in Changes.

## Teste

- store: `FileMarkers` — un `create`; doua `edit` succesive unde al doilea deplaseaza marcajele primului; inlocuire
  (-/+ -> modified); stergere pura (deleted); `update` fara fragmente; fragment fara numere de linie -> `exact` fals.
- web:
  - `/modified`: totaluri pe fisier, `created`, sub-task-urile incluse cu `ref`, ordinea;
  - `/file`: continut + marcaje pentru o cale relativa la folderul agentului; 404 pentru o cale care nu e in
    `task_changes`, pentru `../` si pentru un sub-task care nu e al task-ului; `tooLarge`, `binary`, `exists: false`;
    cale absoluta in afara folderelor proiectului -> 404.
- UI: `node --check` pe modulele noi/schimbate; screenshot cu Chrome headless (tab-ul Modified, modala cu marcaje si
  cod colorat, Changes cu diff colorat, un bloc de cod colorat in Conversation).
