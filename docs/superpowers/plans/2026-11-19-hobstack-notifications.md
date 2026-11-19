# System Notifications Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** HobStack trimite notificari de sistem cand un proiect care a lucrat s-a oprit si cand apare ceva nou in Needs you; se opresc global (Settings > App) sau per proiect.

**Architecture:** Pachetul nou `internal/notify` tine doar logica (un `Watcher` care primeste instantanee ale proiectelor si intoarce notificarile de trimis; fara sistem, testabil). `internal/web` construieste instantaneele (aceleasi surse ca "N need you"), tine cine se uita la ce proiect (raportat de pagina) si setarile. `cmd/hobstack` ruleaza bucla (la 5 s) si trimite notificarea nativa per sistem; clic = proiectul in fereastra / browser.

**Tech Stack:** Go (stdlib, cgo pe macOS cu UserNotifications), Win32 Shell_NotifyIconW (NIF_INFO), notify-send pe Linux, JS vanilla.

**Spec:** docs/superpowers/specs/2026-11-19-hobstack-notifications-design.md

## Global Constraints
- `go vet ./...` si `go build -race` trec; cross-compile `GOOS=darwin GOARCH=arm64` si `GOOS=windows` (release-ul Windows e CGO_ENABLED=0).
- JS verificat ca modul (.mjs + `node --check`).
- Textele vazute de utilizator in engleza; comentariile din cod in romana.
- Dependente minime: nimic nou in go.mod.
- Liniste: 60 s; adunare Needs you: 10 s; bucla: la 5 s.
- Setari implicit pornite (global si per proiect).

## Review Focus
1. Pornirea HobStack cu elemente deja in Needs you si proiecte deja oprite: nicio notificare la pornire (elementele existente sunt "vazute").
2. O pauza scurta intre doi agenti (< 60 s) nu da "finished"; un run nou in cele 60 s anuleaza numaratoarea.
3. Un card a carui numaratoare creste (acelasi id, count++) nu da o noua notificare; un card inchis si altul nou da notificare.
4. Proiect pe pauza (Stop team) cu task-uri in Queued: e "oprit" (nu "busy"), deci da "finished" daca a lucrat.
5. Pagina inchisa fara blur: "viewing" expira (45 s), notificarile revin.

---

### Task 1: Logica notificarilor (`internal/notify`)

**Files:**
- Create: `internal/notify/notify.go`, `internal/notify/notify_test.go`

**Interfaces:**
- Produces:
  ```go
  type Item struct{ Key, Line string }            // Key stabil ("q:12", "a:5", "s:41"...), Line = randul scurt
  type Snapshot struct {
      ProjectID int64; Name string
      Busy     bool  // run activ sau task pe care un agent il poate lua acum
      TaskWork bool  // un run pe task e activ acum
      Done     int   // task-uri Done acum
      Needs    []Item
      Enabled  bool  // comutatorul proiectului
  }
  type Note struct{ ProjectID int64; Title, Body string }
  type Settings struct{ Done, Needs bool }
  func NewWatcher() *Watcher
  func (w *Watcher) Observe(now time.Time, snaps []Snapshot, set Settings, viewing func(projectID int64) bool) []Note
  const Quiet = 60 * time.Second; const Gather = 10 * time.Second
  ```

- [ ] **Step 1: Teste** (in `notify_test.go`, cu `now` controlat; `obs(w, t, snaps...)` helper):
  - `TestNoNotesAtStartup`: prima observare cu Needs [q:1] si Busy=false -> nimic; si dupa 2 min tot nimic.
  - `TestFinishedAfterQuiet`: Busy+TaskWork (Done 0) la t0, Busy=false Done=3 la t0+5s; la t0+60s nimic; la t0+66s -> `{Title:"Meridian finished", Body:"3 tasks done"}`; mai tarziu nimic.
  - `TestShortPauseIsNotFinished`: busy, idle 30 s, busy, idle 61 s -> o singura notificare, dupa a doua pauza.
  - `TestChatOnlyIsNotFinished`: Busy fara TaskWork, Done neschimbat -> idle 2 min -> nimic.
  - `TestWaitingForYou`: dupa lucru, oprit cu Needs [q:1 vazut deja] -> `Title:"Meridian is waiting for you"`, `Body:"2 tasks done · 1 needs you"` (singular "needs", plural "need"); fara task-uri terminate -> Body `"1 needs you"`.
  - `TestNeedsGathered`: element nou q:2 la t, a:7 la t+4s -> la t+10s o notificare `{Title:"Meridian needs you", Body:"2 new items"}`; un singur element -> Body = Line.
  - `TestNeedsSameKeyOnce`: acelasi Key in observari succesive -> o singura notificare; Key disparut si reaparut -> notificare noua.
  - `TestWaitingSuppressedRightAfterNeeds`: notificare "needs you" la t, proiectul devine "finished" cu Needs la t+5s -> doar notificarea needs (cea de waiting nu se trimite daca ultima needs a proiectului e in ultimele `Gather`).
  - `TestDisabledAndViewing`: Enabled=false sau viewing(pid)=true -> nicio notificare, dar starea avanseaza (dupa ce se reactiveaza nu vin notificarile vechi); `Settings{Done:false}` opreste doar finished, `Needs:false` doar needs.
- [ ] **Step 2:** `go test ./internal/notify` -> FAIL (nedefinite).
- [ ] **Step 3:** Implementare: stare per proiect `{seeded, working, sawWork, doneStart, idleSince, seen map[string]bool, pending []Item, pendingAt, lastNeeds}`. Proiecte care lipsesc din snaps se sterg din stare. Body-ul finished: "N task(s) done", " · M need(s) you"; fara N si fara M -> "The team stopped".
- [ ] **Step 4:** `go test -race ./internal/notify` -> PASS.
- [ ] **Step 5:** Commit `notify: watcher for finished projects and new Needs you items`.

### Task 2: Setari globale + per proiect (store, appprefs, API)

**Files:**
- Create: `internal/store/migrations/040_project_notify.sql` (`ALTER TABLE projects ADD COLUMN notify INTEGER NOT NULL DEFAULT 1;`)
- Modify: `internal/store/projects.go` (Project.Notify `json:"notify"`, projectCols/scan, `SetNotify(id int64, on bool) error` ca SetFavorite; UpdateProject NU o schimba)
- Modify: `internal/appprefs/appprefs.go` (`NotifyDone, NotifyNeeds *bool` cu omitempty; `func (p Prefs) Notify() (done, needs bool)` nil = true)
- Modify: `internal/web/server.go`, `internal/web/api_app_window.go` (sau fisier nou `api_notify.go`): `PUT /api/projects/{id}/notify {"on":bool}` -> 204; `PUT /api/app/notify {"done"?:bool,"needs"?:bool}` -> appState; GET /api/app are `notifyDone`, `notifyNeeds`.
- Test: `internal/store/projects_test.go`, `internal/appprefs/appprefs_test.go`, `internal/web/notify_test.go`

**Interfaces:** Produces `store.Project.Notify`, `(*Store).SetNotify`, `appprefs.Prefs.Notify()`.

- [ ] **Step 1: Teste:** proiect nou are Notify=true; SetNotify(false) se pastreaza si dupa UpdateProject; Prefs fara campuri -> Notify() = (true,true); Save/Load cu NotifyNeeds=false -> (true,false); API: PUT notify pe proiect inexistent 404, `PUT /api/app/notify {"needs":false}` lasa done neatins.
- [ ] **Step 2:** ruleaza -> FAIL. **Step 3:** implementare. **Step 4:** `go test -race ./internal/store ./internal/appprefs ./internal/web` -> PASS.
- [ ] **Step 5:** Commit `Notifications: global and per-project switches`.

### Task 3: Instantaneele si "viewing" (`internal/web`)

**Files:**
- Create: `internal/web/notify_source.go`, extinde `internal/web/notify_test.go`

**Interfaces:**
- Consumes: `notify.Snapshot/Item` (Task 1), `Project.Notify` (Task 2).
- Produces:
  ```go
  func (s *Server) NotifySnapshots() ([]notify.Snapshot, error)
  func (s *Server) Viewing(projectID int64) bool            // un client cu focus pe proiect, raportat in ultimele 45 s
  func (s *Server) NotifySettings() notify.Settings          // din appprefs (s.app nil = ambele pornite)
  ```
  `POST /api/app/viewing {"client":"<id aleator>","project":<id|0>,"focused":bool}` -> 204 (JSON obligatoriu, ca restul POST-urilor).
- Busy = `!p.Paused && (team.ProjectActive(pid) || vreun agent cu status Queued)`; TaskWork = vreun agent cu `ActiveTaskID != 0`; Done = task-uri `StatusDone`.
- Needs = aceleasi surse ca `needsCount` din UI, cu Key si Line:
  intrebari `q:<id>` "<Agent> asks: <text, max 100>"; acces `a:<id>` "<Agent> was denied <Tool>" / skill "<Agent> wanted the skill "x"" / comanda "<Agent> wants to run <prefix>"; blocate `s:<taskId>` "<T-N> is stuck"; agenti `r:<id>` "<Lead> wants to add <name>"; lucru `w:<id>` "<Proiect> asks for work"; MCP `m:<id>` "<Agent> wants the MCP server <name>"; propuneri `l:<id>` "<Agent> proposed <name> for the Library"; fill-ins `f:<taskId>` "<T-N> has steps for you"; cont `c:<accountId>` "Claude account <name> failed".
- [ ] **Step 1: Teste:** proiect cu o intrebare si o cerere de acces -> Needs cu cheile `q:<id>`, `a:<id>` si Line-urile de mai sus; proiect pe pauza cu task Queued -> Busy=false; Notify=false -> Enabled=false; Viewing: raport focused pe 3 -> true; acelasi client focused=false -> false; raport mai vechi de 45 s (ceas injectat prin `s.team.Now` / camp `now`) -> false.
- [ ] **Step 2:** FAIL. **Step 3:** implementare (refoloseste `card()` si functiile de view existente). **Step 4:** `go test -race ./internal/web` -> PASS.
- [ ] **Step 5:** Commit `Notifications: project snapshots and who is looking`.

### Task 4: UI (setari, comutator pe proiect, raportul "viewing")

**Files:**
- Modify: `internal/web/static/js/app-settings.js` (doua randuri toggle sub "Open HobStack in"/Autostart: "Notify me when a project finishes" — hint "When a project that was working stops for a minute."; "Notify me when something needs me" — hint "New questions, approvals and blocked tasks in Needs you.")
- Modify: formularul proiectului (fisierul cu Edit project; comutator "Notifications", hint "System notifications for this project.", salvat imediat prin PUT /notify ca steluta)
- Modify: `internal/web/static/js/app.js` (raportul viewing: client id aleator per pagina; trimite la focus / blur / visibilitychange / hashchange si la 20 s cat are focus; proiectul din `#/p/<id>`; erorile ignorate)
- [ ] **Step 1:** implementare; **Step 2:** `node --check` pe fisierele schimbate (.mjs).
- [ ] **Step 3:** Verificare cu Chrome headless pe instanta de test (port 7791): screenshot Settings > App si formularul proiectului; comutatoarele schimba GET /api/app si `notify` pe proiect.
- [ ] **Step 4:** Commit `Notifications: settings and project switch in the UI`.

### Task 5: Trimiterea pe sisteme + bucla (`cmd/hobstack`)

**Files:**
- Create: `cmd/hobstack/notify_darwin.go` (cgo; UNUserNotificationCenter cand `bundlePath` se termina in ".app" si exista bundleIdentifier: cere autorizarea o data, delegate cu willPresent = banner, didReceive = goNotifyClick(url); altfel `osascript` cu `on run argv` (titlul si textul ca argumente, fara escape))
- Create: `cmd/hobstack/notify_windows.go` (balon pe iconita din tray: NIM_MODIFY cu NIF_INFO, szInfoTitle/szInfo, NIIF_USER/NIIF_INFO; clic NIN_BALLOONUSERCLICK 0x405 pe wmTray = deschide ultimul URL; trimis prin PostMessage pe firul tray-ului)
- Create: `cmd/hobstack/notify_linux.go` (`notify-send --app-name=HobStack -i <appicon> --action=open=Open --wait`; daca esueaza ca optiune necunoscuta, fara --action; lipsa = log o data)
- Create: `cmd/hobstack/notify_other.go` (no-op pentru restul)
- Modify: `cmd/hobstack/main.go` (dupa startTray: `go notifyLoop(ctx, webSrv, func(pid) { openProject(root, url, pid) })` la 5 s cu `notify.NewWatcher()`); `openProject` = openUI pe `url + "#/p/<id>"` (browser) sau fereastra + navigarea la hash.
- Modify: `cmd/hobstack/window_darwin.go`, `window_windows.go`, `window_other.go`: `windowGo(hash string)` (macOS evaluateJavaScript `location.hash=`; Windows ExecuteScript pe firul ferestrei; altfel nimic)

**Interfaces:**
- Consumes: `web.Server.NotifySnapshots/NotifySettings/Viewing`, `notify.Watcher`.
- Produces: `func sendNotification(title, body string, onClick func())` per platforma.

- [ ] **Step 1:** test pentru partea pura din main: `notifyURL(base string, pid int64) string` -> `base+"#/p/<pid>"` (in `main_test.go`).
- [ ] **Step 2:** implementare; `go vet ./...`, `go build -race ./cmd/hobstack`, `GOOS=windows go build`, `GOOS=linux go build`, `GOOS=darwin GOARCH=arm64 go build`.
- [ ] **Step 3:** Verificare pe macOS cu instanta de test: un `hs notify-test` ascuns? NU — se verifica prin instanta de test cu un proiect si o intrebare inserata in DB: apare notificarea (osascript, binar in afara bundle-ului); captura de ecran.
- [ ] **Step 4:** Commit `Notifications: native senders and the loop`.

### Task 6: Documentatie + verificare finala
- [ ] CLAUDE.md: intrarea "Gata (notificari de sistem, spec ...)".
- [ ] `go test -race ./...`, vet, cross-build.
- [ ] Commit `CLAUDE.md: notifications`.
