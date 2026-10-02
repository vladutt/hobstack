# Foreman: modificarile de fisiere pe task

Data: 2026-10-02
Status: design aprobat in chat.

## Scop

Pentru fiecare task, utilizatorul vede ce fisiere au fost modificate si cum (diff), de cine si cand, plus comenzile
shell rulate. Pe cardul task-ului: "N files"; in modala: tab-ul "Changes".

## Sursa: stream-json-ul run-ului (verificat cu claude real)

- Apelul: un eveniment `assistant` cu `message.content[]` de tip `tool_use`: `{id, name, input}`.
  - `Write`: `input = {file_path, content}`; `Edit`: `{file_path, old_string, new_string, replace_all}`;
    `MultiEdit`: `{file_path, edits: [{old_string, new_string}]}`; `NotebookEdit`: `{notebook_path, new_source, ...}`
    (calea din `notebook_path`; fara `structuredPatch` se trateaza ca fallback cu `new_source` ca linii `+`);
    `Bash`: `{command, description}`.
- Rezultatul: un eveniment `user` cu `message.content[]` de tip `tool_result`: `{tool_use_id, is_error}` si, la
  nivelul evenimentului, `tool_use_result`:
  - Write nou: `{type: "create", filePath, content, structuredPatch: [], originalFile: null}`;
  - Write peste un fisier existent: `{type: "update", ..., structuredPatch: [...]}`;
  - Edit / MultiEdit: `{filePath, structuredPatch: [...], originalFile, ...}`.
  - `structuredPatch` = `[{oldStart, oldLines, newStart, newLines, lines: ["-vechi", "+nou", " context", "\\ No newline at end of file"]}]`.

Regula: o modificare se pastreaza doar daca `tool_result.is_error` nu e `true`. Daca lipseste `tool_use_result` (alta
versiune de CLI), patch-ul se construieste din `input`: un singur bloc fara numere de linie, cu `-` pentru liniile din
`old_string` si `+` pentru cele din `new_string` (MultiEdit: un bloc per edit; Write: toate liniile `+`).

## Runner

`tracker` tine apelurile `tool_use` (Write, Edit, MultiEdit, NotebookEdit, Bash) dupa `id` pana la rezultat.
`runner.Outcome` primeste:

```go
type FileChange struct {
    Tool  string  // "Write", "Edit", "MultiEdit", "NotebookEdit"
    Path  string  // absolut, ca in input
    Kind  string  // "create", "update" (Write peste existent), "edit"
    Patch []Hunk  // blocurile; la "create" un bloc cu toate liniile "+"
    Truncated bool
}
type Hunk struct { OldStart, OldLines, NewStart, NewLines int; Lines []string }
Changes  []FileChange // in ordinea rezultatelor
Commands []string     // comenzile Bash (input.command), in ordine, si cele esuate
```

Limite: un patch peste 200000 de caractere (suma liniilor) se taie (blocurile de la final se scot, ultimul bloc pastrat
se scurteaza) si `Truncated = true` pe `FileChange`. Comenzile peste 2000 de caractere se taie cu "...".

## Store

Migrarea 013:

```sql
CREATE TABLE task_changes (
  id         INTEGER PRIMARY KEY,
  task_id    INTEGER NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
  run_id     TEXT NOT NULL,
  agent_id   INTEGER REFERENCES agents(id) ON DELETE SET NULL,
  agent_name TEXT NOT NULL DEFAULT '',   -- numele la momentul run-ului (ramane si daca agentul e sters)
  created_at TEXT NOT NULL,
  kind       TEXT NOT NULL,              -- create, update, edit, command
  tool       TEXT NOT NULL,
  path       TEXT NOT NULL DEFAULT '',   -- relativ la folderul agentului cand e in el, altfel absolut
  patch      TEXT NOT NULL DEFAULT '',   -- JSON []Hunk; '' pentru command
  command    TEXT NOT NULL DEFAULT '',
  truncated  INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX task_changes_task ON task_changes(task_id, id);
```

`store.TaskChange{ID, TaskID int64; RunID string; AgentID *int64; AgentName string; At time.Time; Kind, Tool, Path string; Patch []Hunk; Command string; Truncated bool}`
(store are propriul tip `Hunk`, cu aceleasi campuri si tag-uri JSON `oldStart, oldLines, newStart, newLines, lines`).
`AddTaskChanges(changes []TaskChange) error` (o tranzactie), `ListTaskChanges(taskID) ([]TaskChange, error)` (ordonat
dupa id, non-nil), `CountChangedFiles(projectID int64) (map[int64]int, error)` (task -> numar de cai distincte cu
kind != 'command').

## Team

In `recordOutcome`, pentru orice status (done, failed, stopped), daca run-ul era pe un task si `o.Changes` sau
`o.Commands` nu sunt goale: salveaza-le ca `TaskChange` (agentul run-ului, `run_id`, `created_at` = acum; caile
relative la folderul agentului, `filepath.Rel`, daca nu incep cu ".."). Erorile se logheaza si nu opresc fluxul.
Daca s-a salvat ceva: `emit(project, "task", task)` (UI-ul reincarca). Inainte de logica de status existenta, ca la
refuzurile de acces.

## API

- `GET /api/tasks/{id}/changes` -> `{runs: [...]}`, grupate pe `run_id` in ordinea primei aparitii:
  ```json
  { "runId": "...", "agent": "Rook", "agentId": 3, "at": "...",
    "files": [{ "path": "internal/auth/login.go", "kind": "edit", "added": 12, "removed": 3, "truncated": false,
                "hunks": [{ "oldStart": 1, "oldLines": 1, "newStart": 1, "newLines": 2, "lines": ["-a", "+b"] }] }],
    "commands": ["go test ./..."] }
  ```
  In cadrul unui run, modificarile aceluiasi fisier se unesc intr-un singur element `files[]` (blocurile in ordine;
  `kind` = "create" daca prima a fost create, altfel "edit"); `added` / `removed` = numarul liniilor `+` / `-`.
  404 daca task-ul lipseste; `runs` mereu non-nil.
- `GET /api/projects/{id}`: fiecare task primeste `filesChanged int` (din `CountChangedFiles`, 0 implicit).

## UI

- **Cardul task-ului** (board): daca `filesChanged > 0`, eticheta discreta "N file(s)" (ex. "3 files", "1 file").
- **Modala**: al treilea tab "Changes" (dupa Conversation si History). Datele se incarca la deschiderea tab-ului si
  se reincarca la refresh-ul modalei doar daca tab-ul e deschis si `filesChanged` s-a schimbat.
  - Per run: antet "Rook · 13:05 · 3 files"; apoi un rand per fisier: calea (monospace), "new" daca `create`,
    `+12` verde, `−3` rosu; click deschide / inchide diff-ul (starea pastrata la refresh, cheie run + cale).
  - Diff: blocurile cu antet `@@ -oldStart,oldLines +newStart,newLines @@` (fara antet cand numerele lipsesc), liniile
    colorate (`+` verde, `-` rosu, context gri), monospace, scroll orizontal in interiorul diff-ului (pagina nu
    capata scroll orizontal); "\\ No newline at end of file" in gri.
  - "Truncated: the change was too large to keep in full." cand `truncated`.
  - "Commands run" sub fisierele run-ului: lista monospace.
  - Fara date: "No file changes recorded for this task." plus hint "Changes made through shell commands are not
    captured as diffs."

## Teste

- runner: tracker-ul cu Write create, Write update, Edit, MultiEdit, un Edit esuat (`is_error: true`) ignorat, fallback
  fara `tool_use_result`, Bash (si esuat), taierea patch-ului mare.
- store: migrarea (user_version 13), add/list, `CountChangedFiles` (cai distincte, fara comenzi), cascade la stergerea
  task-ului, agent sters -> agent_id NULL si agent_name pastrat.
- team: un run pe task cu modificari si comenzi -> randuri salvate cu cai relative; run fara task -> nimic; run esuat cu
  modificari -> salvate.
- web: gruparea pe run, unirea aceluiasi fisier, added/removed, `filesChanged` in proiect, 404.
- Browser: eticheta pe card, tab-ul Changes, diff colorat, deschis/inchis pastrat la SSE, comenzi, mobil fara overflow.

## In afara scopului

- Fisierele schimbate prin shell (sed, generatoare, git) ca diff.
- Modificarile live in timpul run-ului (apar la finalul run-ului).
- Istoric retroactiv pentru task-urile vechi.
- Revert / aplicare de patch din UI.
