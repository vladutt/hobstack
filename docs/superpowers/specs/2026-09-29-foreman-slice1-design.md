# Foreman, felia 1: proiecte, echipe si orchestrare lead -> workeri

Data: 2026-09-29
Status: aprobat in discutie, asteapta review pe spec

## 1. Context si scop

agentdash este un tool local si personal: un binar Go si un UI in browser pe 127.0.0.1:7777, care controleaza agenti Claude Code rulati headless (`claude -p`). In forma actuala, un agent e un folder cu CLAUDE.md si settings, rulat standalone.

Tinta, pe baza design-ului din `../dashboard-images/`, este **Foreman**:
- mai multe proiecte;
- fiecare proiect are o echipa de agenti cu un lead;
- lead-ul primeste un obiectiv, il sparge in task-uri, le delegheaza workerilor si face review la rezultate;
- utilizatorul vorbeste cu lead-ul sau direct cu orice agent.

Intregul obiectiv a fost impartit in sub-proiecte. Acest spec acopera doar **felia 1**, o felie verticala care arata o echipa lucrand cap-coada:

1. **Felia 1 (acest spec):** SQLite, proiecte, agenti per proiect, orchestrare prin MCP, board de task-uri, chat cu fiecare agent.
2. Librarie de agenti refolositi intre proiecte, template-uri de echipa, reviewer separat.
3. Aprobari pentru actiuni riscante (prin `--permission-prompt-tool`), feed global „Live activity”.
4. Modurile de lucru collab si pipeline, Runs & logs, Settings.

### Decizii luate in brainstorming

- **Motorul ramane Claude Code CLI.** Ollama si modelele locale din design NU intra. Modelul per agent e unul din `opus`, `sonnet`, `haiku`.
- **Orchestrarea se face prin MCP.** Foreman expune un server MCP local cu tool-uri de echipa. Agentii le apeleaza, iar Go actualizeaza starea si porneste run-urile. Nu se foloseste JSON parsat din text si nici subagentii nativi.
- **Agentii traiesc in SQLite, nu in foldere.** Modelul vechi `~/.agentdash/agents/<nume>/` dispare. Toti agentii unui proiect ruleaza cu folderul proiectului ca cwd.
- **Numele e „foreman”**, definit intr-o singura constanta si usor de schimbat (vezi sectiunea 11).

## 2. Scope

### Intra
- CRUD proiecte: nume, obiectiv, context comun, folder de lucru, workeri in paralel, buget per run.
- CRUD agenti per proiect: exact un lead, 0..n workeri. Campuri: nume, rol, model, background, instructiuni, definition of done, tool-uri.
- Preview live al prompt-ului compilat.
- Orchestrare: lead -> `create_task` -> worker ruleaza -> `report_done` -> lead trezit -> `accept_task` / `send_back`.
- Board live: Queued / In progress / Review / Done. Task-urile blocate apar cu badge.
- Chat per agent. Mesajele trimise unui worker ajung in copie si la lead.
- Stop team / Resume. Pornire pe pauza dupa restart.
- Persistenta completa in SQLite. Logurile brute ale run-urilor raman `.jsonl` pe disc.

### Nu intra
Librarie de agenti, template-uri, reviewer separat, modurile collab si pipeline, aprobari interactive, feed global, pagina de modele, Runs & logs, Settings, editarea task-urilor din UI (in afara de Accept / Deblocheaza pe task-urile blocate).

## 3. Model de date

Fisier: `~/.foreman/foreman.db`, deschis cu `modernc.org/sqlite` (pure Go, fara CGO). WAL activ, `foreign_keys=ON`. Migrarile sunt un sir de fisiere SQL embedded, aplicate in ordine, iar versiunea se tine in `PRAGMA user_version`.

```sql
CREATE TABLE projects (
  id           INTEGER PRIMARY KEY,
  name         TEXT NOT NULL,
  goal         TEXT NOT NULL DEFAULT '',
  context      TEXT NOT NULL DEFAULT '',
  workdir      TEXT NOT NULL,              -- cale absoluta, trebuie sa existe
  paused       INTEGER NOT NULL DEFAULT 0,
  max_parallel INTEGER NOT NULL DEFAULT 2, -- workeri care ruleaza simultan
  budget_usd   REAL    NOT NULL DEFAULT 1, -- --max-budget-usd per run
  created_at   TEXT NOT NULL
);

CREATE TABLE agents (
  id           INTEGER PRIMARY KEY,
  project_id   INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  name         TEXT NOT NULL,              -- unic in proiect (case-insensitive)
  role         TEXT NOT NULL DEFAULT '',
  position     TEXT NOT NULL CHECK (position IN ('lead','worker')),
  model        TEXT NOT NULL DEFAULT 'sonnet',
  background   TEXT NOT NULL DEFAULT '',
  instructions TEXT NOT NULL DEFAULT '',
  done_def     TEXT NOT NULL DEFAULT '',
  tools        TEXT NOT NULL DEFAULT '[]', -- JSON: ["write","shell","git","web"]
  session_id   TEXT NOT NULL DEFAULT '',   -- pentru --resume
  prompt_hash  TEXT NOT NULL DEFAULT '',   -- hash-ul system prompt-ului cu care s-a creat sesiunea
  created_at   TEXT NOT NULL
);
-- exact un lead per proiect: index unic partial
CREATE UNIQUE INDEX one_lead ON agents(project_id) WHERE position = 'lead';

CREATE TABLE tasks (
  id          INTEGER PRIMARY KEY,
  project_id  INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  number      INTEGER NOT NULL,            -- afisat T-<number>, incrementat per proiect
  title       TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT '',
  assignee_id INTEGER NOT NULL REFERENCES agents(id),
  status      TEXT NOT NULL CHECK (status IN ('queued','in_progress','review','done','blocked')),
  result      TEXT NOT NULL DEFAULT '',    -- ultimul report_done / textul final al worker-ului
  feedback    TEXT NOT NULL DEFAULT '',    -- ultimul send_back
  send_backs  INTEGER NOT NULL DEFAULT 0,
  created_at  TEXT NOT NULL,
  updated_at  TEXT NOT NULL,
  UNIQUE (project_id, number)
);

CREATE TABLE runs (
  id         TEXT PRIMARY KEY,             -- acelasi format ca acum: 20260929-130056-af8b6e
  agent_id   INTEGER NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
  task_id    INTEGER REFERENCES tasks(id), -- NULL pentru run-urile lead-ului sau mesajele directe
  status     TEXT NOT NULL CHECK (status IN ('running','done','failed','stopped','interrupted')),
  started_at TEXT NOT NULL,
  ended_at   TEXT,
  cost_usd   REAL,                         -- din evenimentul `result`
  error      TEXT NOT NULL DEFAULT ''
);
-- log: ~/.foreman/logs/runs/<id>.jsonl

CREATE TABLE messages (
  id         INTEGER PRIMARY KEY,
  project_id INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  agent_id   INTEGER NOT NULL REFERENCES agents(id) ON DELETE CASCADE, -- thread-ul
  kind       TEXT NOT NULL CHECK (kind IN ('msg','event')),
  author     TEXT NOT NULL,                -- 'user' sau numele agentului; '' la event
  text       TEXT NOT NULL,
  run_id     TEXT,                         -- run-ul care a produs mesajul (link spre log)
  delivered  INTEGER NOT NULL DEFAULT 1,   -- 0 = mesaj de la user care inca n-a ajuns la agent
  created_at TEXT NOT NULL
);
```

Statusul afisat al proiectului **se calculeaza, nu se stocheaza**:
- `Paused` daca `paused = 1`;
- altfel `Running` daca exista run-uri active sau task-uri `queued`;
- altfel `Idle`.

Statusul unui agent:
- `Working` daca are un run activ;
- `Queued` daca are task-uri `queued` sau mesaje nelivrate;
- altfel `Idle`.

Stergeri:
- stergerea unui agent cu task-uri e refuzata (409);
- stergerea proiectului sterge totul in cascada, dar nu atinge folderul de lucru.

## 4. Serverul MCP (`internal/mcp`)

- **Endpoint:** `POST /mcp/{token}` pe serverul HTTP existent. E transportul „streamable HTTP” din MCP, dar doar varianta simpla: request JSON si raspuns JSON, fara stream SSE pe acest endpoint.
- **Metode JSON-RPC 2.0 suportate:**
  - `initialize`: raspunde cu `protocolVersion`, `capabilities: {tools: {}}` si `serverInfo`;
  - `notifications/initialized`: raspunde cu 202, fara body;
  - `tools/list`;
  - `tools/call`;
  - orice alta metoda intoarce eroarea `-32601`. Claude trimite intai `server/discover`, iar `-32601` e raspunsul corect;
  - `GET` pe endpoint intoarce 405, pentru ca nu oferim stream server -> client. Verificat pe 2026-09-29 cu Claude Code 2.1.284: conexiunea merge.
- **Token:** 32 de bytes aleatori, in hex. Se genereaza la pornirea fiecarui run si e tinut in memorie ca `token -> {projectID, agentID, runID, taskID}`. Se sterge cand run-ul se termina. Un token necunoscut primeste 404.
- **Lista de tool-uri** depinde de pozitia agentului, astfel incat un worker sa nu vada tool-urile lead-ului.
- **Numele serverului** in config e `team`, deci in Claude tool-urile apar ca `mcp__team__<nume>`.

### Tool-uri pentru lead

| Tool | Input | Efect |
|---|---|---|
| `create_task` | `title` (string), `description` (string), `assignee` (nume worker) | Task nou in `queued`. Eroare daca worker-ul nu exista sau e chiar lead-ul. Intoarce `T-<n>`. |
| `list_tasks` | optional `status` | Lista text: `T-n [status] titlu -> worker` plus rezultatul pentru task-urile din `review` sau `blocked`. |
| `accept_task` | `task` („T-3” sau 3), `note` (optional) | `review` sau `blocked` -> `done`. |
| `send_back` | `task`, `feedback` (obligatoriu) | `review` -> `queued`, cu `feedback` salvat si `send_backs++`. Daca `send_backs` ajunge la 3, task-ul trece in `blocked`, iar lead-ul primeste mesajul „task blocat, decide utilizatorul”. |

### Tool-uri pentru worker

| Tool | Input | Efect |
|---|---|---|
| `report_done` | `summary` (string) | Task-ul curent al run-ului trece in `review`, cu `result = summary`. Eroare daca run-ul nu are task (mesaj direct de la user). |

Fiecare tool reusit:
- scrie un mesaj `event` in thread-ul potrivit, de exemplu „a creat T-4 pentru Rook” in thread-ul lead-ului;
- publica un eveniment pe stream-ul SSE al proiectului;
- trimite un semnal scheduler-ului.

Erorile de validare intorc `isError: true` cu un mesaj in romana, fara diacritice, clar pentru agent.

## 5. Scheduler-ul (`internal/team`)

Exista o singura goroutine per proces. Ea citeste un canal de semnale (`kick(projectID)`) si, la fiecare semnal, reconciliaza proiectul. Nu pastreaza stare proprie: sursa de adevar e DB-ul, plus lista de run-uri active din runner.

**Reconcilierea unui proiect** (se sare daca proiectul e `paused`):
1. **Workeri.** Se iau task-urile `queued` in ordinea `number`. Pentru fiecare task al carui worker nu are run activ, cat timp numarul de workeri activi e sub `max_parallel`, task-ul trece in `in_progress` si se porneste un run de worker.
2. **Mesaje directe catre workeri.** Pentru fiecare worker fara run activ, cu mesaje nelivrate si fara task-uri `queued`, se porneste un run de mesaj direct (fara task).
3. **Lead.** Daca lead-ul nu are run activ si exista notificari nelivrate (mesaje de la user sau evenimente de worker marcate pentru lead), se porneste un run de lead care le livreaza pe toate odata.

**Notificarile pentru lead** sunt randuri `messages` in thread-ul lead-ului, cu `delivered = 0`:
- mesajele userului;
- copiile mesajelor catre workeri („Tu -> Rook: ...”);
- evenimentele „Rook a terminat T-3 (review): <rezumat>” si „run-ul lui Rook pe T-3 a esuat: <motiv>”.

Un eveniment `event` normal e `delivered = 1`. Doar cele care trebuie citite de lead au `delivered = 0`.

**Cand se termina un run** (callback din runner):
- `runs` primeste status, `ended_at`, `cost_usd` si `error`;
- textul final al agentului (campul `result` din evenimentul `result`) devine un mesaj `msg` in thread-ul lui;
- daca e run de worker si task-ul e inca `in_progress` (worker-ul n-a apelat `report_done`), task-ul trece in `review` cu `result` = textul final, sau „run esuat: <motiv>” daca run-ul a esuat;
- daca task-ul a ajuns in `review` pe oricare cale, se adauga notificarea pentru lead;
- la final, `kick(projectID)`.

**Stop team:** `paused = 1`, se opresc toate run-urile active ale proiectului (kill pe process group) si task-urile `in_progress` revin in `queued`. **Resume:** `paused = 0`, apoi `kick`.

**La pornirea serverului:** run-urile `running` devin `interrupted`, task-urile `in_progress` devin `queued`, iar toate proiectele primesc `paused = 1`. Nimic nu porneste singur.

**Actiuni ale userului pe task-uri `blocked`** (butoane pe card):
- `Accept` -> `done`;
- `Deblocheaza` -> `queued`, cu `send_backs = 0`.

## 6. Pornirea unui run

Runner-ul existent (`internal/runner`) se pastreaza: porneste procesul, parseaza stream-json, scrie log-ul, face broadcast SSE si opreste tot process group-ul. Se schimba doar construirea comenzii, care vine acum din `internal/prompt`:

```
claude -p <mesajul turei>
  --output-format stream-json --verbose
  --model <agent.model>
  --append-system-prompt <system prompt compilat>
  --setting-sources user
  --settings <JSON inline: permissions.allow + permissions.deny>
  --mcp-config <JSON inline: {"mcpServers":{"team":{"type":"http","url":"http://127.0.0.1:<port>/mcp/<token>"}}}>
  --strict-mcp-config
  --max-budget-usd <project.budget_usd>
  [--resume <agent.session_id>]
cwd = project.workdir
```

**Sesiuni si system prompt.** Claude Code inregistreaza system prompt-ul la prima cerere a unei sesiuni si il retrimite neschimbat la fiecare `--resume`. De aceea:
- **system prompt-ul contine doar date stabile:** rolul agentului, campurile lui, obiectivul si contextul proiectului, blocul „Cum lucrezi”;
- **starea dinamica merge in mesajul turei:** echipa curenta, task-ul si feedback-ul, notificarile;
- la fiecare run se calculeaza `sha256` pe system prompt. Daca e diferit de `agent.prompt_hash`, se incepe o sesiune noua (fara `--resume`) si se scrie un event in thread: „instructiunile s-au schimbat, sesiune noua”;
- `session_id` se salveaza din evenimentul `system/init`, ca acum.

**Mesajul turei, pe tip de run:**
- **Worker cu task:**
  ```
  Echipa: <lista>
  Task T-3: <titlu>
  <descriere>
  [Feedback de la <lead> la incercarea anterioara: <feedback>]
  [Mesaje noi de la utilizator: ...]
  ```
- **Worker, mesaj direct:** `Mesaj de la utilizator: <text>` (se livreaza toate mesajele nelivrate). Lead-ul e deja in copie prin notificare.
- **Lead:**
  ```
  Echipa: <lista cu status>
  Notificari:
  - Utilizatorul: <text>
  - Rook a terminat T-3: <rezumat>
  ...
  ```

### Prompt-ul compilat (`internal/prompt`)

```
# Rol
Esti <nume>, <rol>, <lead|worker> in proiectul <proiect>. <Raportezi utilizatorului | Raportezi lui <lead> (lead).>
# Background
<background>
# Instructiuni
<instructions>
# Cand termini un task
<worker: Apeleaza report_done cu: <done_def> | lead: Cand obiectivul e atins, scrie utilizatorului un rezumat: <done_def>>
# Proiect
Obiectiv: <goal>
Context comun:
<context>
# Cum lucrezi
<text fix pentru lead sau worker, vezi mai jos>
```

Sectiunile cu continut gol se omit. `internal/prompt` expune si un `Preview(agent, project)`, folosit de editor pentru preview-ul live.

**„Cum lucrezi”, lead:** Esti coordonatorul echipei. Analizeaza obiectivul si citeste codul daca ai nevoie de context. Sparge munca in task-uri mici, clare si verificabile si creeaza-le cu `create_task`, fiecare asignat unui worker potrivit rolului. Nu face tu munca workerilor. Cand primesti rezultate, verifica-le (poti citi fisierele modificate) si apeleaza `accept_task` sau `send_back` cu feedback concret. Foloseste `list_tasks` ca sa vezi starea. Cand obiectivul e atins, scrie utilizatorului un rezumat.

**„Cum lucrezi”, worker:** Lucreaza doar pe task-ul primit. Nu modifica lucruri din afara task-ului. Cand termini, apeleaza `report_done` cu rezumatul cerut. Daca ceva e neclar sau blocat, apeleaza `report_done` explicand problema, in loc sa ghicesti.

### Permisiuni (`--settings` inline)

| Tool bifat in UI | `permissions.allow` |
|---|---|
| mereu | `Read`, `Glob`, `Grep`, `mcp__team__*` |
| `write` | `Edit`, `Write`, `NotebookEdit` |
| `shell` | `Bash` |
| `git` (fara shell complet) | `Bash(git status:*)`, `Bash(git diff:*)`, `Bash(git log:*)`, `Bash(git add:*)`, `Bash(git commit:*)`, `Bash(git checkout -b:*)` |
| `web` | `WebSearch`, `WebFetch` |

`permissions.deny` este fix pentru toti agentii:
- actiuni riscante: `Bash(git push:*)`, `Bash(rm -rf:*)`, `Bash(sudo:*)`, `Bash(npm install:*)`, `Bash(composer require:*)`, `Bash(go get:*)`, `Bash(pip install:*)`, `Bash(brew install:*)`;
- tool-uri interne Claude Code care se suprapun cu echipa: `TaskCreate`, `TaskUpdate`, `TaskList`, `TaskGet`, `TodoWrite`, `Agent`.

Proba din 2026-09-29 a aratat ca, fara acest deny, lead-ul foloseste `TaskCreate` (lista interna de task-uri) in loc de `mcp__team__create_task`. Cu deny, apeleaza tool-ul nostru. Aceste tool-uri interne nu cer permisiune in `-p`, deci trebuie blocate explicit.

In `-p` tot ce lipseste din `allow` sau apare in `deny` e refuzat automat. Acest lucru a fost verificat pe 2026-09-29 pentru `allow`. `--setting-sources user` pastreaza setarile globale ale utilizatorului si ignora `.claude/settings.json` din repo. `CLAUDE.md` din folderul proiectului se incarca in continuare.

## 7. API HTTP (`internal/web`)

Toate raspunsurile sunt JSON. Erorile au forma `{"error": "..."}` (in romana, fara diacritice), cu 400 / 404 / 409.

| Metoda si ruta | Ce face |
|---|---|
| `GET /api/info` | `{name}`, adica AppName, pentru titlul din UI |
| `GET /api/projects` | lista, cu status calculat, echipa (nume, pozitie) si progres (done/total) |
| `POST /api/projects` | creeaza proiectul si lead-ul implicit (nume „Lead”, model `sonnet`, fara tool-uri de scriere). Valideaza: nume nevid, `workdir` absolut si existent. |
| `GET /api/projects/{id}` | proiectul, agentii cu status si task-ul curent, task-urile |
| `PUT /api/projects/{id}` | editeaza campurile proiectului |
| `DELETE /api/projects/{id}` | 409 daca are run-uri active |
| `POST /api/projects/{id}/stop` · `/resume` | Stop team / Resume |
| `POST /api/projects/{id}/agents` | agent nou (worker; lead-ul exista deja) |
| `PUT /api/agents/{id}` · `DELETE /api/agents/{id}` | editare / stergere (409 daca are task-uri sau run activ) |
| `POST /api/prompt-preview` | body = campurile agentului + `project_id`, intoarce prompt-ul compilat |
| `GET /api/agents/{id}/messages` | thread-ul, cronologic |
| `POST /api/agents/{id}/messages` | mesaj de la user: `delivered = 0`, iar daca e worker, si o copie in thread-ul lead-ului; apoi `kick` |
| `POST /api/tasks/{id}/accept` · `/unblock` | doar pentru task-uri `blocked` |
| `GET /api/projects/{id}/events` | SSE: `task`, `message`, `agent`, `project` (payload = obiectul actualizat) |
| `GET /api/runs/{id}/events` | SSE existent, output live al unui run |
| `POST /mcp/{token}` | serverul MCP |

Rutele vechi `/api/agents` (lista din foldere), `/api/agents/{name}/runs` si `/reset` dispar.

## 8. UI (`internal/web/static`)

- **Tehnologie:** HTML si JS vanilla, fara build step, embedded cu `embed`. Fisiere: `index.html`, `app.js`, `app.css`, `fonts/`. Rutele sunt in hash: `#/`, `#/new`, `#/p/{id}`, `#/p/{id}/edit`, `#/p/{id}/agent/new`, `#/p/{id}/agent/{aid}`.
- **Stil:** urmeaza mockup-urile Foreman. Tema dark, lead-ul cu amber `#e5a54b`, workerii cu teal `#6cc3b4`. Fonturile Space Grotesk (titluri), IBM Plex Sans (text) si JetBrains Mono (modele, ID-uri) sunt embedded ca woff2, deci merge offline. Textele din UI sunt in romana, fara diacritice.
- **Sidebar:** logo, AppName, link „Proiecte”.

**Ecrane:**
1. **Proiecte (`#/`):** grid de carduri cu nume, pill de status, obiectiv, avatare echipa (initiale; lead amber, workeri teal), „X din Y task-uri” cu bara de progres. Ultimul card e „Proiect nou”.
2. **Proiect nou / editare (`#/new`, `#/p/{id}/edit`):** brief (nume, obiectiv, context comun, folder de lucru), apoi „Workeri in paralel” si „Buget per run (USD)”. Dupa creare se deschide workspace-ul, unde echipa se construieste cu „Adauga agent”.
3. **Workspace (`#/p/{id}`):**
   - header: breadcrumb, nume, pill de status, obiectiv, butoanele „Instructiuni proiect” (editare) si „Stop team” / „Resume”;
   - Echipa: cardul lead-ului, apoi cardurile workerilor, fiecare cu punct de status (Working / Idle / Queued), task-ul curent, model si buton de editare. Click pe card selecteaza thread-ul din consola;
   - Board: 4 coloane (Queued / In progress / Review / Done). Cardul arata `T-n`, titlul si avatarul asignatului. Task-urile `blocked` apar in coloana Review cu badge „Blocat” si butoanele Accept / Deblocheaza. Click pe card deschide un panou cu descriere, feedback si rezultat;
   - Consola (dreapta): thread-ul agentului selectat. Mesajele userului sunt aliniate la dreapta, ale agentului la stanga, evenimentele sunt randuri gri. Daca agentul are run activ, apare un bloc „live” cu output-ul run-ului, tool call-urile fiind restranse pe un rand. Dedesubt e input-ul, iar pentru un worker apare nota „Lead-ul e in copie”.
4. **Editor agent:** Identitate (nume, rol, lead/worker read-only), Model (select opus / sonnet / haiku), Background / Instructiuni / Definition of done, checkbox-uri de tool-uri (Scrie fisiere, Comenzi shell, Git, Web) si preview-ul live al prompt-ului (debounce 300 ms, prin `POST /api/prompt-preview`).

**Live:** workspace-ul deschide stream-ul SSE al proiectului si actualizeaza in loc board-ul, cardurile si thread-ul. Pentru agentul selectat care are run activ se deschide si stream-ul run-ului. Reconectarea e automata (EventSource). La reconectare se reincarca starea completa cu `GET /api/projects/{id}`.

## 9. Structura codului

```
cmd/foreman/          CLI: start [--port] [--no-open], oprire curata
internal/config/      AppName, cai (~/.foreman, db, logs)
internal/store/       SQLite: open, migrari, query-uri tipizate
internal/prompt/      system prompt, mesajul turei, settings JSON, mcp-config JSON (functii pure)
internal/runner/      proces claude, stream-json, log, broadcast, stop (existent, adaptat)
internal/mcp/         handler HTTP JSON-RPC, registry token -> run, tool-uri
internal/team/        scheduler: kick, reconciliere, callback la final de run, stop/resume, recuperare la pornire
internal/web/         API, SSE proiect, UI embedded
```

- `internal/agent` se sterge.
- Dependente: doar `team` le cunoaste pe `store`, `runner`, `mcp` si `prompt`. `mcp` nu face logica de business: apeleaza o interfata implementata de `team` (`CreateTask`, `ReportDone`...), ca sa ramana testabil.
- `runner` e folosit de `team` prin interfata `Starter`, ca testele sa-l poata inlocui cu un fals.
- Singura dependenta noua: `modernc.org/sqlite`.

## 10. Erori si cazuri limita

- **Tool MCP invalid** (task inexistent, worker necunoscut, status gresit, `report_done` fara task): se intoarce `isError: true` cu un mesaj clar, fara sa se schimbe starea.
- **`claude` iese cu eroare, isi depaseste bugetul sau e oprit:** run-ul devine `failed` / `stopped`, cu `error` completat. Task-ul unui worker trece in `review` cu „run esuat: <motiv>”, iar lead-ul e notificat. Lead-ul decide: `send_back` sau `accept_task`.
- **Lead-ul esueaza:** eventul ramane in thread-ul lui si nu se reincearca automat. Urmatorul mesaj al userului il trezeste din nou.
- **Folderul de lucru dispare:** pornirea run-ului esueaza, proiectul trece pe pauza si apare un mesaj in thread-ul lead-ului.
- **Erori de DB in scheduler:** se logheaza si se sare peste proiect la acel kick. Serverul nu se opreste.
- **Portul:** URL-ul MCP foloseste portul real pe care asculta serverul.

## 11. Numele aplicatiei

- `internal/config`: `const AppName = "foreman"`. Din el se deriva: folderul `~/.<AppName>`, fisierul `<AppName>.db`, textele din CLI si `/api/info`.
- Binarul se construieste din `cmd/foreman`. Modulul Go se redenumeste din `agentdash` in `foreman`.
- **Redenumirea ulterioara:** schimbi constanta, redenumesti `cmd/<nume>`, rulezi `go mod edit -module <nume>` si faci replace pe importuri.
- `~/.agentdash` nu se migreaza (continea doar agentul demo si loguri de test).
- README si CLAUDE.md se actualizeaza cu noul model.

## 12. Testare

- **`store`:** teste pe o baza temporara (`t.TempDir()`), pentru migrari, CRUD, unicitatea lead-ului, numerotarea `T-n` per proiect si cascade.
- **`prompt`:** teste pe system prompt (sectiuni goale omise, lead vs worker), pe mesajul turei (cu si fara feedback/notificari), pe settings JSON (mapare tool-uri si lista `deny`) si pe mcp-config.
- **`mcp`:** teste cu `httptest` pentru `initialize`, `tools/list` (lead si worker vad seturi diferite), `tools/call` cu succes si cu fiecare validare, plus token necunoscut -> 404.
- **`team`:** runner fals si store real. Flux complet: mesaj user -> run lead -> `create_task` x2 -> 2 run-uri worker, cu respectarea `max_parallel` -> `report_done` -> lead notificat intr-un singur run -> `send_back` -> worker cu `--resume` si feedback -> `accept_task` -> done. Plus: al treilea `send_back` -> `blocked`, worker care nu apeleaza `report_done`, Stop / Resume, recuperarea la pornire.
- **Manual cap-coada cu Claude real:** proiect temporar (un folder cu un README), lead plus 2 workeri, obiectiv mic de tipul „adauga 2 fisiere text cu X si Y”. Se verifica board-ul live, costurile si chat-ul direct cu un worker.
- **Conventii:** `go vet ./...`, `go build -race ./...`, cross-compile darwin/arm64 si windows.

## 13. Riscuri verificate (proba din 2026-09-29, Claude Code 2.1.284)

1. **MCP HTTP inline:** merge. Serverul raspunde JSON simplu, iar tool-urile apar ca `mcp__team__*`, sunt descoperite prin ToolSearch si se apeleaza fara refuz cand sunt in `allow`. E nevoie de deny pe tool-urile interne de task-uri (sectiunea 6).
2. **`--append-system-prompt` cu `--resume`:** help-ul confirma ca prompt-ul se inregistreaza la prima cerere. Regula cu `prompt_hash` din sectiunea 6 acopera cazul.
3. **`--max-budget-usd`:** run-ul se opreste, iar evenimentul `result` vine cu `subtype: "error_max_budget_usd"`, `is_error: true`, `errors: ["Reached maximum budget ($X)"]` si `result` null. Limita se verifica intre ture, deci costul real poate depasi putin bugetul.
