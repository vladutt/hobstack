# Foreman: sarcini recurente

Data: 2026-10-03
Status: design aprobat in chat.

## Scop

Utilizatorul defineste, per proiect, sarcini care se creeaza singure pe un program (ex. "Weekdays at 09:00: run the
tests and report what is broken"). Fiecare aparitie e un task obisnuit pe board, creat pe acelasi drum ca butonul de
task nou (`team.UserCreateTask`): acelasi agent sau prima etapa, aceeasi conversatie, istoric si tab Changes.
Mecanismul e generic (verificari, intretinere cu cod, rapoarte), nu optimizat pentru un caz anume.

In afara scopului: expresii cron, rulare fara Foreman pornit (cron/launchd, "service install" e alt sub-proiect),
mai multe task-uri pentru rularile ratate, istoric separat al rularilor (ultimul task si ultimul mesaj ajung).

## Program

Trei tipuri, ora locala a masinii (`time.Local`):

| kind          | campuri                 | text afisat               |
|---------------|-------------------------|---------------------------|
| `every_hours` | `every_hours` 1..24     | "Every 3 hours"           |
| `daily`       | `at` "HH:MM"            | "Daily at 09:00"          |
| `weekly`      | `at`, `weekdays` (masca)| "Weekdays at 09:00", "Mon, Wed, Fri at 09:00" |

`weekdays`: bitul `1<<time.Weekday` (duminica = bitul 0). Masca 0 e invalida. Textul: L-V = "Weekdays", S+D =
"Weekends", toate 7 = "Every day", altfel zilele in ordinea Mon..Sun.

Functie pura, in pachetul nou `internal/schedule`:

```go
type Spec struct {
    Kind       string // "every_hours", "daily", "weekly"
    EveryHours int
    At         string // "HH:MM", 24h
    Weekdays   int    // masca
}
func (s Spec) Validate() error
func (s Spec) Next(after time.Time) time.Time // primul moment strict dupa `after`
func (s Spec) String() string                 // textul afisat
```

- `every_hours`: `after` + N ore, aliniat la ora fixa (minutul 0). Ex. N=3, after=10:20 -> 13:00.
- `daily` / `weekly`: urmatoarea zi (inclusiv azi, daca ora n-a trecut) permisa, la `At`, construita cu `time.Date`
  in `time.Local`. La trecerea la ora de vara, o ora inexistenta (02:30) devine ce normalizeaza `time.Date` (03:30);
  la trecerea inapoi, o ora dubla ruleaza o singura data (prima).
- `Validate`: kind cunoscut; `EveryHours` 1..24 pentru `every_hours`; `At` "HH:MM" valid pentru `daily`/`weekly`;
  `Weekdays` 1..127 pentru `weekly`. Erori in engleza ("time must be HH:MM").

## Store

Migrarea 014:

```sql
-- sarcini care creeaza singure task-uri pe un program
CREATE TABLE recurring_tasks (
  id          INTEGER PRIMARY KEY,
  project_id  INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  title       TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT '',
  assignee_id INTEGER REFERENCES agents(id) ON DELETE SET NULL, -- NULL = prima etapa (doar cu etape)
  kind        TEXT NOT NULL,              -- every_hours, daily, weekly
  every_hours INTEGER NOT NULL DEFAULT 0,
  at          TEXT NOT NULL DEFAULT '',   -- HH:MM, ora locala
  weekdays    INTEGER NOT NULL DEFAULT 0, -- masca, bitul 1<<time.Weekday
  auto_accept INTEGER NOT NULL DEFAULT 0,
  enabled     INTEGER NOT NULL DEFAULT 1,
  next_run_at TEXT NOT NULL,              -- RFC3339, UTC
  last_run_at TEXT,                       -- ultima incercare (creat, sarit sau eroare)
  last_task_id INTEGER REFERENCES tasks(id) ON DELETE SET NULL,
  last_note   TEXT NOT NULL DEFAULT '',   -- "Skipped at 09:00: T-12 still open", eroarea, sau ''
  created_at  TEXT NOT NULL
);
CREATE INDEX recurring_tasks_project ON recurring_tasks(project_id, id);

ALTER TABLE tasks ADD COLUMN recurring_id INTEGER REFERENCES recurring_tasks(id) ON DELETE SET NULL;
ALTER TABLE tasks ADD COLUMN auto_accept INTEGER NOT NULL DEFAULT 0;
```

`assignee_id` cu `ON DELETE SET NULL`: un agent sters lasa sarcina fara asignat; scheduler-ul o dezactiveaza (vezi
Erori). Fara etape, `NULL` inseamna mereu "asignat sters".

`store.RecurringTask{ID, ProjectID int64; Title, Description string; AssigneeID *int64; Schedule schedule.Spec;
AutoAccept, Enabled bool; NextRunAt time.Time; LastRunAt *time.Time; LastTaskID *int64; LastNote string;
CreatedAt time.Time}`; `Task` primeste `RecurringID *int64` si `AutoAccept bool` (si in `taskCols`).

Functii: `CreateRecurring(*RecurringTask) error`, `GetRecurring(id)`, `SaveRecurring(*RecurringTask) error`,
`DeleteRecurring(id) error`, `ListRecurring(projectID) ([]RecurringTask, error)` (dupa id, non-nil),
`DueRecurring(now time.Time) ([]RecurringTask, error)` (enabled si `next_run_at <= now`, toate proiectele, dupa
`next_run_at`).

## Team

`UserCreateTask` primeste optiuni pentru recurente fara sa-si schimbe apelantii existenti:
`userCreateTask(projectID, title, description, agentID, recurringID *int64, autoAccept bool)`, iar
`UserCreateTask` il cheama cu `nil, false`. Evenimentul "created" ramane "Created by you" pentru task-urile din board;
pentru un task recurent textul e `Created by recurring task "<titlu>"` (actor tot "You").

### Scheduler

`Run` (internal/team/reconcile.go) are deja un ticker de 1 minut pentru `CheckStuck`; pe acelasi tick se cheama si
`RunDue(t.now())`. In plus, `RunDue` se cheama o data la pornirea lui `Run`, inaintea buclei (rularile ratate cat a fost
inchis Foreman apar imediat, nu dupa un minut).

`RunDue(now)`, sub `t.work` doar cat creeaza un task (ca `UserCreateTask`): pentru fiecare sarcina din `DueRecurring(now)`:

1. **Suprapunere**: daca `last_task_id` exista si task-ul nu e Done, nu se creeaza nimic;
   `last_note = "Skipped at HH:MM: T-12 still open"`.
2. **Asignat sters** (`assignee_id` NULL si proiectul nu are etape): `enabled = false`,
   `last_note = "Turned off: the assignee was deleted"`.
3. Altfel `userCreateTask(..., &r.ID, r.AutoAccept)`; la succes `last_task_id` = task-ul nou, `last_note = ""`;
   la eroare `last_note` = mesajul erorii (fara reincercare pana la urmatoarea ora).
4. Mereu: `last_run_at = now`, `next_run_at = Spec.Next(now)`, `SaveRecurring`, eveniment SSE `recurring`.

Pentru ca `next_run_at` se calculeaza de la `now`, oricate rulari s-au ratat cat a fost inchis Foreman, la pornire se
creeaza un singur task. Daca echipa e pe pauza (cum e la pornire), task-ul sta in Queued pana la Start team, ca orice
task din board.

`RunNow(id)`: aceiasi pasi 1-3 pentru o singura sarcina, dar fara sa mute `next_run_at`; la suprapunere intoarce
eroarea `"T-12 is still open"` (400) in loc sa scrie nota.

La create si la edit (program schimbat, sau reactivare) `next_run_at = Spec.Next(now)`.

### Auto-accept

Un task cu `AutoAccept` merge pe fluxul normal (reviewer-ul asignatului, daca are, verifica intai). Se schimba doar
locurile unde azi un task `ByUser` ar ajunge la decizia utilizatorului; acolo, cu `AutoAccept`, task-ul trece direct in
Done cu `markDone` + `autoAccepted(task, "Accepted automatically (recurring task)")`, ca in modul autonom:

- `toReview`, cazul `!hasWho && task.ByUser` (agent fara reviewer raporteaza);
- `review_approve`, cazul `task.ByUser && !autonomous` (reviewer-ul a aprobat);
- dupa ultima etapa, cand nu exista decident (`!ok`);
- `toReview`, ramura cu etape (task dintr-o etapa terminat fara raport), cand nu exista decident.

Intrebarile (`ask_user`), cererile de acces, task-urile blocate si detectorul de task-uri blocate merg ca azi.
Sub-task-urile create de un lider dintr-un task recurent nu mostenesc `auto_accept`: le decide liderul.

## API

- Lista vine in detaliul proiectului (`GET /api/projects/{id}`), cheia `recurring: [view]`, ca etapele; SSE-ul
  existent o reincarca.
- `POST /api/projects/{id}/recurring` (corp: title, description, assigneeId (0 = prima etapa), kind, everyHours, at,
  weekdays, autoAccept, enabled) -> 201 view
- `PUT /api/recurring/{id}` (acelasi corp) -> view
- `DELETE /api/recurring/{id}` -> 204
- `POST /api/recurring/{id}/run` -> 201 task creat

`view` = campurile sarcinii (programul in `schedule: {kind, everyHours, at, weekdays}`) plus `scheduleText` (din `Spec.String()`), `lastTask` (`{id, number, status}` sau null)
si `assignee` (numele, sau "" pentru prima etapa).

Validare ca la `createUserTask`: titlu obligatoriu, `maxLen` title 200 / description 10000, `Spec.Validate()`,
asignatul trebuie sa fie agent al proiectului (fara etape: obligatoriu). Erorile sunt 400 cu mesaj in engleza.

`GET /api/tasks/{id}` si task-urile din proiect au deja `recurringId` si `autoAccept` prin `Task`; `getTask` adauga
`recurring: {id, title}` cand sarcina inca exista.

## UI

- **Tab-ul Tasks**: buton `Recurring` langa `+ Task` si `Columns`; deschide un panou deasupra board-ului, ca ele.
- **Lista**: per sarcina titlul, programul ("Weekdays at 09:00"), urmatoarea rulare ("Today 14:00", "Tomorrow 09:00",
  "Oct 6 09:00"; "Off" cand e oprita), asignatul, link spre ultimul task (`T-12 · Done`) si `last_note` (estompat; rosu
  pentru erori). Actiuni: comutator on/off, `Run now`, `Edit`, `Delete` (cu confirm). Lista goala: un hint si butonul
  `New recurring task`.
- **Formular** (in acelasi panou): Title, Description, Agent (select ca la task nou; cu etape, prima optiune
  "First free agent in <etapa>"), Schedule (radio: "Every N hours" cu numar 1-24, "Daily at" cu `<input type=time>`,
  "Weekly on" cu 7 butoane-comutator Mon..Sun si ora), bifa "Accept automatically when done" cu hint-ul
  "Skips your Accept / Send back. Questions and blocked tasks still reach you."
- **Board**: cardul unui task cu `recurringId` are un `↻` mic langa numar (`title="From a recurring task"`).
- **Modala de task**: sub meta, "From recurring: <titlu>" (inchide modala si deschide panoul Recurring), cand `recurring` exista.
- Evenimentul SSE `recurring` reincarca proiectul (ca orice eveniment); lista din panou se redeseneaza, formularul deschis nu.

## Erori si cazuri de margine

- Asignat sters: sarcina se opreste la urmatoarea rulare, cu nota; la edit se poate alege alt agent si reporni.
- Proiect sters: sarcinile dispar (`ON DELETE CASCADE`).
- Sarcina stearsa: task-urile create raman; `recurring_id` devine NULL (dispare `↻`), `auto_accept` ramane.
- Ceasul sistemului mutat inapoi: `next_run_at` ramane in viitor si se asteapta; mutat inainte: o singura rulare.
- O eroare la o sarcina nu opreste celelalte din acelasi tur; se logheaza si ajunge in `last_note`.

## Teste

- `internal/schedule`: tabel pentru `Next` (every_hours peste miezul noptii si aliniere, daily inainte/dupa ora,
  weekly cu masti diferite si sfarsit de saptamana, ora de vara in Europe/Bucharest cu `time.LoadLocation`) si
  `String`, `Validate`.
- Store: CRUD, `DueRecurring` (enabled, ordinea), `ON DELETE` pe agent / task / proiect, coloanele noi din `tasks`.
- Team (ceas injectat prin `t.now`, fake-ul existent din `fake_test.go`): creare la timp, rulare ratata -> un singur
  task, suprapunere -> nota, asignat sters -> oprit, eroare -> nota si `next_run_at` mutat, `RunNow` cu suprapunere,
  auto-accept pe cele trei drumuri (fara reviewer, cu reviewer, cu etape) si fara auto-accept -> asteapta utilizatorul.
- Web: validare (titlu, program, asignat), CRUD, `run`, `getTask` cu `recurring`.
- UI: verificare cu screenshot-uri in Chrome headless (lista, formular, `↻` pe card).
