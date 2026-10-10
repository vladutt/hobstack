# HobStack: istoricul unei sarcini recurente

Data: 2026-10-10
Status: design aprobat in chat.

## Scop

Pe panoul "Recurring tasks", fiecare sarcina arata acum doar ultimul task ("T-70 · Done"). Dupa zeci de rulari
(ex. "Scount the internet": 25 de rulari orare, fiecare cu un raport si 3 fisiere, `leads-emails.csv` in 24 de
versiuni) nu ai unde vedea ce s-a cautat si ce a iesit la fiecare rulare, nici toate fisierele intr-un loc.

Adaugam o modala "Run history" per sarcina recurenta: fisierele tuturor rularilor (ultima versiune a fiecaruia) si
lista rularilor cu raportul fiecareia. Totul se construieste din datele existente (`tasks.recurring_id`,
`tasks.result`, `task_files`): fara migrare, fara AI, fara cost.

In afara scopului: rezumat scris de AI peste rulari, task-parinte real pentru rulari, cautare in rapoarte,
stergerea in masa a rularilor vechi.

## UI

### Rand pe panoul Recurring tasks (recurring.js)

Linkul "T-70 · Done" devine `25 runs · last T-70 Done`:
- `25 runs` (si titlul sarcinii) deschide modala Run history;
- `T-70 Done` ramane link spre modala task-ului, ca acum.
Fara rulari: nimic (ca acum). Numarul vine din `RecurringTask.Runs` (vezi API).

### Modala Run history (fisier nou static/js/recurring-history.js)

Ruta `#/p/<pid>/tasks/recurring/<rid>`: se deschide peste board ca modala de task (parseWorkspaceRoute intoarce
`recurringId`), Esc / X o inchide si revine la `#/p/<pid>/tasks`. Acelasi stil ca modala de task (doua coloane pe
ecran lat, una pe telefon).

Coloana din dreapta (detalii):
- programul ("Every hour"), agentul sau etapa, Next run, auto-accept;
- totaluri: `25 runs · 24 done · 1 open` (open = queued / in progress / review / blocked; arhivatele se numara
  separat: `· 2 deleted`, doar daca exista);
- `last_note` daca nu e gol (ex. "Skipped at 09:00: T-12 still open");
- butoanele Run now si Edit (aceleasi actiuni ca pe rand; Edit inchide modala si deschide formularul).

Coloana principala:

**Files** (sus). Toate fisierele livrate de rulari (si de sub-task-urile lor), grupate pe cale: o intrare per cale,
cu ultima versiune, cele mai noi primele. Randul: nume, `v24` (numarul de versiuni, doar peste 1), T-N-ul care a dat
ultima versiune (link), cand, marime, Preview / Download. Expand arata versiunile vechi, fiecare cu T-N-ul ei.
Preview-ul refoloseste ce face tab-ul Files din modala de task (imagine / PDF / Markdown / text / CSV ca text,
HTML/SVG doar Download). Primele 10 intrari, apoi "Show all N". Fisierele urcate de utilizator pe rulari apar si ele,
cu autorul "You". Fara fisiere: sectiunea lipseste.

**Runs**. Rularile, cea mai noua prima. Randul: data si ora crearii, T-N (link spre modala task-ului), status
(eticheta ca pe board), agentul asignat, eticheta `📎 N` daca are fisiere. Sub el, primul paragraf din raport
(`tasks.result` pana la primul rand gol, max 300 de caractere); click pe rand desface raportul intreg (Markdown, ca in
modala de task) si numele fisierelor rularii (click = preview in sectiunea Files). Fara raport: "No report yet"
(in lucru) sau "No report" (gata fara raport). Rularile arhivate apar estompate cu eticheta "Deleted".
Primele 20, apoi "Load more" (urmatoarele 20).

Live: modala se reincarca la evenimentul SSE `task` al proiectului (emis si la fisierele atasate) cand task-ul are
`recurringId` egal cu sarcina deschisa (debounce 1 s), ca o rulare noua sa apara fara refresh.

## API

`GET /api/projects/{id}/recurring` (existent): fiecare sarcina primeste in plus `runs` (numarul de task-uri cu
`recurring_id`, inclusiv arhivate).

`GET /api/recurring/{id}/history?before=<taskId>&limit=20` (nou):

```json
{
  "recurring": { ...RecurringTask },
  "counts": { "total": 25, "done": 24, "open": 1, "archived": 0 },
  "runs": [
    { "id": 812, "number": 70, "status": "done", "archived": false, "assignee": "Scout",
      "createdAt": "...", "updatedAt": "...", "result": "Round 26 found ...", "files": 3 }
  ],
  "more": true
}
```

`before` = id-ul ultimei rulari primite (paginare stabila cand apar rulari noi); `limit` 1..50, implicit 20.
`result` e intreg (rapoartele au ~1-2 KB); excerpt-ul se taie in UI. `files` = fisierele rularii plus ale
sub-task-urilor ei.

`GET /api/recurring/{id}/files` (nou): aceeasi forma ca grupurile din `GET /api/tasks/{id}/files`
(`fileGroup`: ultima versiune + `versions`), plus `taskId` / `ref` pe fiecare versiune. Descarcarea si preview-ul
merg prin `GET /api/task-files/{id}` (existent), deci aceleasi reguli (serveStored, HTML/SVG nu inline).

404 pentru o sarcina inexistenta.

## Store

- `RecurringRuns(projectID) (map[int64]int, error)`: numarul de rulari per sarcina, pentru lista.
- `RecurringHistory(id, before int64, limit int) ([]RecurringRun, bool, error)`: task-urile cu `recurring_id = id`
  (si arhivate), `id < before` cand `before > 0`, ordonate `id DESC`, `limit+1` pentru `more`; numarul de fisiere
  per rulare din `CountTaskFilesTree` (sau un singur query agregat, daca e simplu).
- `RecurringCounts(id) (RecurringCounts, error)`: total / done / open / archived intr-un query.
- `RecurringFiles(id) ([]TaskFile, error)`: fisierele task-urilor sarcinii si ale descendentilor lor (CTE recursiv
  pe `parent_task_id`, ca `ListDescendantFiles`), cea mai noua prima. Gruparea pe cale o face `groupFiles` din web
  (aceeasi functie ca la task), extinsa cu `taskId` / `ref` pe versiuni.

Grupare: dupa `path` (calea relativa salvata in `task_files`), la fel ca la task. Doua fisiere cu aceeasi cale din
aceeasi rulare (sub-task-uri diferite) sunt versiuni separate.

## Cazuri limita

- Sarcina stearsa: `recurring_id` devine NULL (FK existent), istoricul ei dispare odata cu sarcina; task-urile raman pe
  board. Modala deschisa pe o sarcina stearsa arata "This recurring task no longer exists" si se inchide la Esc.
- Sarcina fara rulari: modala arata "No runs yet" si butonul Run now.
- Rulare arhivata (Delete pe board): ramane in istoric, estompata; fisierele ei raman in Files (snapshot-urile nu se
  sterg la arhivare).
- Rulare cu sub-task-uri: rularea e un singur rand; fisierele sub-task-urilor intra in `files` si in Files.
- Multe fisiere cu nume diferite per rulare (`leads-round26.csv`): fiecare e o intrare; "Show all N" tine lista scurta.

## Teste

- store: `RecurringHistory` (ordine, paginare cu `before`, `more`, arhivate incluse, alta sarcina exclusa),
  `RecurringCounts`, `RecurringFiles` (include sub-task-urile, exclude alte sarcini), `RecurringRuns`.
- web: `GET /api/recurring/{id}/history` (forma, 404, limita) si `/files` (grupare pe cale, versiuni cu ref).
- JS: `node --check` pe recurring-history.js / recurring.js / workspace.js copiate ca .mjs.
- UI: screenshot cu Chrome headless prin CDP al modalei pe "Scount the internet" (lat si ~400 px).
- `go vet ./...`, `go build -race`, cross-compile darwin/arm64 si windows.
