# Foreman: statistici pe proiect (tab-ul Stats)

Data: 2026-10-08
Status: design aprobat in chat.

## Scop

Un tab nou **Stats** pe proiect (`#/p/3/stats`, langa Tasks / Team / Files) care raspunde la trei intrebari pe o
perioada aleasa:

- **cheltuiala**: unde se duc banii si tokenii (pe agent, pe zi, task-urile cele mai scumpe);
- **performanta echipei**: cate task-uri se termina, in cat timp, cate sunt trimise inapoi, cate run-uri esueaza;
- **activitate**: fisiere modificate, comenzi Bash, intrebari catre utilizator.

In afara scopului: pagina globala (toate proiectele), date custom (date picker), export CSV, refresh live prin SSE,
coloane noi in schema.

## Perioada

Butoane in dreapta-sus: **Today / 7d / 30d / All**, implicit **7d**. Alegerea se tine in localStorage
(`foreman-stats-range`, in try/catch, ca `foreman-drawer`).

Toate limitele sunt in ora locala (`time.Local`), ca la sarcinile recurente si `ProjectActivity`:

| range | from | to | bucket |
|---|---|---|---|
| `today` | azi 00:00 | acum | ora |
| `7d` | 00:00 acum 6 zile | acum | zi |
| `30d` | 00:00 acum 29 zile | acum | zi |
| `all` | 00:00 in ziua primului task sau run al proiectului | acum | zi (saptamana daca sunt peste 90 de zile) |

Perioada precedenta = `[from - (to - from), from)`; pentru `all` nu exista (fara comparatie pe tile-uri).

## Ce arata pagina (de sus in jos)

1. **Tile-uri**: Spend ($), Tokens, Tasks done, Median time to done, Run success rate, Questions to you. Fiecare cu
   diferenta fata de perioada precedenta ("+12% vs prev 7d"); fara diferenta cand perioada precedenta e 0 sau range-ul e `all`.
2. **Spend over time**: bare pe bucket, stivuite pe agent, culorile din `ui.toneOf` (aceleasi ca avatarele).
3. **Throughput**: doua linii pe bucket, task-uri create vs terminate.
4. **Agents**: tabel cu agent, runs, failed, cost, tokens, work time, tasks done, send-backs, files changed. Sortabil
   pe click pe antet (implicit dupa cost). Click pe un agent deschide agentul (`#/p/3/team/<id>`).
5. **Top tasks by cost**: primele 5 task-uri dupa costul run-urilor din perioada (T-nr, titlu, cost, runs, status).
   Click deschide modala task-ului (`#/p/3/tasks/<id>`).
6. **Activity**: fisiere modificate (distincte), comenzi Bash (si cate au esuat), intrebari puse / raspunse si
   primele 5 fisiere dupa numarul de modificari.

Gol (nimic in perioada): un singur mesaj "No activity in this range." in locul graficelor; tile-urile arata 0.

## Definitii

- **Run in perioada**: `runs.started_at` in `[from, to)`; proiectul prin `agents.project_id`.
- **Spend**: `SUM(cost_usd)`. **Tokens**: `SUM(MAX(tokens, 0))` (`-1` = necunoscut, nu intra; ca `ProjectUsage`).
- **Work time**: suma `ended_at - started_at` pentru run-urile terminate.
- **Run success rate**: `done / (done + failed)`; `stopped` si `interrupted` nu intra (le-a oprit utilizatorul sau
  restartul). Fara run-uri terminate: "—".
- **Task creat**: `tasks.created_at` in perioada.
- **Task terminat**: primul eveniment `accepted` sau `user_accepted` din `task_events` in perioada. Task-urile vechi
  (de dinainte de `task_events`) cu `status = done` si fara astfel de eveniment folosesc `tasks.updated_at`.
  Un task terminat apartine agentului `assignee_id`.
- **Time to done**: momentul terminarii minus `tasks.created_at`; mediana pe task-urile terminate in perioada.
- **Send-backs**: evenimente `sent_back` si `rejected` in perioada, pe agentul `assignee_id` al task-ului.
- **Files changed**: `task_changes` cu `kind` in (`create`, `update`, `edit`), `path` distinct, `created_at` in perioada.
  **Commands**: `kind = command`; esuate = `ok = 0`. Pe agent dupa `task_changes.agent_id`.
- **Questions**: `questions.created_at` in perioada (puse); `status = answered` si `closed_at` in perioada (raspunse).

Agentii stersi: run-urile lor pleaca odata cu ei (`ON DELETE CASCADE`), deci nu intra nici in total. Modificarile lor
(`task_changes`, `agent_id` NULL) intra in totaluri, dar nu au rand in tabelul Agents. Acceptat, fara schimbari de schema.

## Backend

### `internal/store/stats.go`

```go
type StatsTotals struct {
    CostUSD       float64 `json:"costUsd"`
    Tokens        int64   `json:"tokens"`
    TasksCreated  int     `json:"tasksCreated"`
    TasksDone     int     `json:"tasksDone"`
    MedianDoneSec int64   `json:"medianDoneSec"` // 0 = niciun task terminat
    RunsDone      int     `json:"runsDone"`
    RunsFailed    int     `json:"runsFailed"`
    Questions     int     `json:"questions"`
    Answered      int     `json:"answered"`
    FilesChanged  int     `json:"filesChanged"`
    Commands      int     `json:"commands"`
    CommandsFailed int    `json:"commandsFailed"`
}

type StatsBucket struct {
    Start   time.Time          `json:"start"`
    CostBy  map[int64]float64  `json:"costBy"`  // agent id -> cost
    Created int                `json:"created"`
    Done    int                `json:"done"`
}

type StatsAgent struct { // un rand din tabelul Agents
    AgentID int64; Name string; Runs, Failed int; CostUSD float64; Tokens, Seconds int64
    TasksDone, SendBacks, FilesChanged int
}

type StatsTask struct { TaskID int64; Number int; Title, Status string; CostUSD float64; Runs int }
type StatsFile struct { Path string; Changes int }

type Stats struct {
    From, To time.Time
    Bucket   string        // "hour", "day", "week"
    Totals   StatsTotals
    Prev     *StatsTotals  // nil pentru all
    Series   []StatsBucket // toate bucket-urile, si cele goale
    Agents   []StatsAgent  // agentii cu activitate in perioada
    TopTasks []StatsTask   // max 5
    TopFiles []StatsFile   // max 5
}

func (s *Store) ProjectStats(projectID int64, from, to time.Time, bucket string) (Stats, error)
func (s *Store) ProjectStatsTotals(projectID int64, from, to time.Time) (StatsTotals, error) // perioada precedenta
func (s *Store) ProjectFirstActivity(projectID int64) (time.Time, error) // pentru range-ul all
```

(campurile JSON in camelCase, ca restul API-ului.)

- Filtrarea pe proiect si perioada in SQL (timestamp-urile sunt RFC3339 UTC, comparabile ca text cand limita e
  formatata cu 9 zecimale fixe). Randurile se aduna in Go, intr-o singura trecere: totaluri, serii, agenti si topuri.
- Gruparea pe ore/zile/saptamani in Go, in `from.Location()`, nu in SQLite (ar grupa in UTC). Un helper
  `bucketIndex(t, from, bucket)` tine cont de schimbarea orei (zilele se numara pe data calendaristica, nu pe 24h).
- Perioada precedenta: aceleasi interogari de totaluri, fara serii si topuri.
- Un singur fisier, functii mici per sectiune (`statsRuns`, `statsTasks`, `statsChanges`, `statsQuestions`).

### `internal/web/api_stats.go`

`GET /api/projects/{id}/stats?range=today|7d|30d|all`

- range lipsa = `7d`; altceva = 400 `unknown range "x"`.
- proiect inexistent = 404.
- calculeaza `from`/`to`/`bucket` dupa tabelul de mai sus (`time.Now()` in ora locala) si intoarce `Stats` ca JSON.

## UI

- `static/js/stats.js`: `statsView(root, projectId)` intoarce `{ cleanup }`; helperi SVG mici `bars(series, ...)`
  si `lines(series, ...)` (fara librarii), cu axa X etichetata pe bucket si tooltip prin `<title>`.
- `workspace.js`: tab nou `{ id: "stats", label: "Stats" }` in `TABS`; corpul tab-ului e creat la intrarea pe tab si
  reincarcat la fiecare intrare (fara auto-refresh).
- `app.css`: clase `stats-*` folosind token-urile existente (tema ardezie; aplicatia are o singura tema, inchisa); grila de tile-uri se
  impacheteaza pe ecrane inguste.
- Numerele: `$1.23`, tokenii compacti (`1.2M`), durate `3h 12m`.

## Erori

- Fetch esuat: mesaj in pagina "Couldn't load stats: <err>" cu buton Retry.
- Server: erorile SQL ajung ca 500, ca in restul API-ului.

## Teste

- `internal/store/stats_test.go`: run-uri, task-uri, evenimente, modificari si intrebari in si in afara perioadei;
  verifica totalurile, perioada precedenta, randurile pe agent, topurile, fallback-ul pe `updated_at` pentru task-urile
  fara eveniment, mediana, `tokens = -1`, si gruparea pe zile peste miezul noptii local (cu `time.FixedZone`).
- `internal/web/stats_test.go`: range implicit, range necunoscut = 400, proiect inexistent = 404, forma JSON.
- `node --check` pe `stats.js` copiat ca `.mjs`; screenshot cu Chrome headless al tab-ului (latime normala si
  ingusta) inainte de "gata".
- `go vet ./...`, `go build -race`, cross-compile darwin/arm64 si windows.
