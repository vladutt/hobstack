# Foreman: fisiere atasate pe task (livrabile)

Data: 2026-10-08
Status: spec aprobat in chat, de implementat.

## Scop

Cand un task cere un fisier (un document, o imagine, un export, un raport), rezultatul trebuie sa fie pe task: sa-l vezi
in modala si sa-l descarci, fara sa cauti in folderul agentului. Azi tab-ul Changes arata doar diff-urile Edit/Write
(`task_changes`), nu fisierul intreg, si nu spune care fisiere sunt "rezultatul".

Decizii luate cu utilizatorul:
- Doar **livrabile explicite**: agentul spune ce fisiere preda. Nu se ataseaza automat tot ce a scris.
- Foreman copiaza fisierul ca **snapshot** in momentul predarii (ramane vizibil si daca fisierul e modificat sau sters).
- **Preview + Download** in modala.

In afara scopului: upload de fisiere pe task de catre utilizator, tool separat `attach_file` apelabil oricand,
stergerea atasamentelor din UI, diff intre versiuni, fisiere din afara folderelor proiectului.

## Concept

- `report_done(..., files?: string[])`: caile fisierelor predate, relative la folderul agentului (sau absolute, dar
  doar in folderele proiectului). Max 20 de cai pe raport.
- Foreman (nu agentul) citeste fisierele si le copiaza in `~/.foreman/projects/<pid>/task-files/<taskID>/`.
- Raportul trece si daca unele fisiere nu pot fi atasate; raspunsul tool-ului spune care si de ce (ca la
  `left_for_user`): `Attached 2 files. Not attached: /tmp/x.mjs (outside the project folders; put deliverables in your folder).`
- Un task respins si raportat din nou poate preda acelasi fisier: devine o versiune noua. UI-ul arata ultima versiune
  per cale; cele vechi raman, pliate ("2 earlier versions").
- Pe task-ul parinte apar si fisierele sub-task-urilor (din arborele de delegare), ca rezultatul sa ajunga sus.

## Date

Migrarea 024:

```
task_files
  id          INTEGER PRIMARY KEY
  task_id     INTEGER NOT NULL REFERENCES tasks(id) ON DELETE CASCADE
  project_id  INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE
  agent_id    INTEGER REFERENCES agents(id) ON DELETE SET NULL
  agent_name  TEXT NOT NULL DEFAULT ''   -- numele la momentul predarii
  path        TEXT NOT NULL              -- calea predata: relativa la folderul agentului cand e in el, altfel absoluta
  name        TEXT NOT NULL              -- numele de afisat (baza caii)
  stored      TEXT NOT NULL              -- numele fisierului pe disc, in task-files/<taskID>/
  size        INTEGER NOT NULL
  mime        TEXT NOT NULL
  created_at  TEXT NOT NULL
INDEX task_files_task ON task_files(task_id, path, id)
```

Tabel separat de `attachments`: acolo `kind` are CHECK ('project','message') si extinderea ar cere reconstruirea
tabelului; in plus, aici avem agent, cale si versiuni.

Versiunea "curenta" a unei cai = randul cu id-ul cel mai mare pentru (task_id, path). La stergerea proiectului
directorul `task-files/` dispare odata cu `projects/<pid>/` (ca `files/`). Task-urile nu se sterg individual din
aplicatie; CASCADE pe `task_id` e doar pentru consistenta.

## Copierea (siguranta)

Agentul nu trebuie sa poata "atasa" orice fisier de pe masina (de ex. `~/.ssh/id_rsa`), pentru ca UI-ul l-ar servi.
De aceea:

1. Radacinile permise = folderele proiectului (`project_folders`): folderul agentului si cele primite prin `--add-dir`.
2. O cale relativa se rezolva fata de folderul agentului. O cale absoluta trebuie sa fie sub una din radacini;
   altfel: `outside the project folders; put deliverables in your folder`.
3. Fisierul se deschide prin `os.OpenRoot(radacina)` cu calea relativa, ca in `internal/library`: `..` si
   symlink-urile care ies din radacina sunt refuzate de Go.
4. Doar fisiere obisnuite (nu foldere, nu device-uri): `not a regular file`.
5. Max 50 MB pe fisier (`maxFileSize` din web): `larger than 50 MB`. Lipsa: `not found`.
6. Copierea in `task-files/<taskID>/<random>-<nume curat>` (nume curat cu `store.CleanFileName`), apoi randul in DB.
   Daca insertul esueaza, fisierul copiat se sterge.
7. Mime: `mime.TypeByExtension`, apoi `http.DetectContentType` pe primii 512 octeti; cai duplicate in acelasi apel se
   ataseaza o singura data.

Codul de copiere sta intr-un pachet mic, `internal/taskfiles` (Copy(roots, agentRoot, path, destDir) -> rezultat),
testabil fara server; `team` il apeleaza din `ReportDone` dupa ce raportul a trecut.

## MCP si prompt

- `report_done` primeste `files`: array de string-uri, descrierea: `Optional: up to 20 files you produced as the
  deliverable of this task (documents, images, exports), relative to your folder. They are copied and attached to the
  task. Do not list code you changed; it is already shown as changes.`
- Validare la intrare (ca `validLeftForUser`): max 20, fiecare nevid, max 500 de caractere.
- Raspunsul tool-ului adauga `Attached N files.` si, daca e cazul, `Not attached: <cale> (<motiv>); ...`.
- Prompt-ul agentilor (sectiunea despre raportare): cand task-ul cere un fisier ca rezultat, il pui in folderul tau si
  il predai prin `files` la `report_done`.

## Istoric

`task_events`: evenimentul raportului primeste in text `· attached 3 files` (fara kind nou), deci apare in
Conversation si Flow. Numele fisierelor apar in textul complet al evenimentului.

## API

- `GET /api/tasks/{id}/files` ->
  ```
  { "files": [ { "id", "path", "name", "size", "mime", "agentName", "createdAt", "versions": [ {id,size,agentName,createdAt}, ... ] } ],
    "subtasks": [ { "taskId", "ref", "title", "files": [ ...acelasi format... ] } ] }
  ```
  `files` = ultima versiune per cale, cea mai noua prima; `versions` = versiunile mai vechi, cea mai noua prima.
  `subtasks` = descendentii task-ului (acelasi arbore ca `store.TaskFlow`) care au fisiere, in ordinea din arbore.
- `GET /api/task-files/{id}` serveste fisierul; `?download=1` forteaza `Content-Disposition: attachment`.
- Servirea refoloseste logica din `serveFile`, extrasa intr-un helper comun (`serveStored`): `nosniff`,
  `Cross-Origin-Resource-Policy: same-origin`, `Content-Security-Policy: sandbox` pentru tot in afara de PDF,
  inline doar pentru tipurile din `inlineTypes` (deci HTML/SVG scrise de agenti nu ruleaza pe originea aplicatiei).
- View-ul task-ului din board primeste `fileCount` (numarul de cai distincte pe task, fara sub-task-uri).

## UI

- **Card pe board:** eticheta `📎 N` cand `fileCount > 0`, langa "N files" (Changes).
- **Modala de task:** tab nou **Files** (dupa Changes), ascuns cand nu exista fisiere nici pe task, nici pe sub-task-uri.
  - Lista: icon dupa tip, nume, cale (gri, mic), marime, agent, timp relativ, buton Download; "N earlier versions" pliat,
    cu acelasi rand pentru fiecare versiune.
  - Click pe un rand deschide preview-ul in modala (sub lista sau in locul ei, cu Back):
    - imagini (`image/png|jpeg|gif|webp`): `<img src="/api/task-files/{id}">`;
    - PDF: `<iframe>` pe acelasi URL;
    - Markdown (`.md`): textul randat cu acelasi renderer ca rapoartele;
    - text si cod (`text/*`, json, yaml, csv, sursa; max 1 MB afisat, apoi "Download to see the rest"): `<pre>`;
    - restul (inclusiv HTML si SVG): "No preview for this file type" + Download.
  - Sectiunea **From sub-tasks**: un grup per sub-task (`T-12 · titlu`), cu aceleasi randuri.
- SSE: dupa atasare se emite evenimentul task-ului care reimprospateaza board-ul (ca la report).

## Teste

- `internal/taskfiles`: cale relativa ok; absoluta in alt folder al proiectului ok; `..` refuzat; symlink spre exterior
  refuzat; absoluta in afara (`/tmp/x`) refuzata; folder refuzat; lipsa; peste 50 MB; duplicat in acelasi apel.
- store: insert + listare (ultima versiune per cale, versiunile vechi), agregare pe sub-task-uri, `fileCount`.
- MCP: `report_done` cu `files` -> raspunsul contine Attached / Not attached; raportul trece si cand toate esueaza;
  validarea (21 de cai, cale goala).
- web: header-ele pe HTML si SVG (sandbox, nu inline), PDF inline fara sandbox, `?download=1` -> attachment.
- UI: verificare cu screenshot (Chrome headless) pe modala cu imagine, Markdown, text si sub-task-uri.

## Etape

1. Migrare 024 + store (`task_files`: insert, list per task, list per arbore, count).
2. `internal/taskfiles` (copiere sigura) + teste.
3. MCP: `files` la `report_done`, apel din team, text in event si in prompt.
4. API + helper `serveStored` comun cu `serveFile`.
5. UI: eticheta pe card, tab-ul Files cu preview, sectiunea From sub-tasks.
6. CLAUDE.md: linia "Gata (fisiere pe task, ...)".
