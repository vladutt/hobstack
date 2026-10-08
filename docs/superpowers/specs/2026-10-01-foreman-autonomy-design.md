# Foreman: modul "Full autonomy" si notificarile "Access needed"

Data: 2026-10-01
Status: design aprobat in chat.

## Scop

1. Un proiect poate merge fara utilizator: agentii nu pun intrebari, iar deciziile care ar fi ajuns la utilizator le ia
   liderul radacina.
2. In toate modurile, cand unui agent i s-a refuzat un tool (permisiune lipsa), utilizatorul afla din "Needs you" si
   poate da acces dintr-un click (Shell).

## 1. Comutatorul

- Coloana noua `projects.autonomy INTEGER NOT NULL DEFAULT 0` (migrarea 010, impreuna cu `tasks.escalated INTEGER NOT NULL DEFAULT 0` si tabelul de mai jos). `store.Project.Autonomy bool`
  (JSON `autonomy`), citita si scrisa ca restul campurilor (create/update proiect, `GET /api/projects/{id}`).
- UI, Project instructions: bifa "Full autonomy" cu explicatia "Agents don't ask you questions; the root lead decides
  what you would decide. You still get access requests."
- Board: langa numele proiectului, o pastila "Autonomous" cand e pornit; cardul din Projects la fel.
- Schimbarea comutatorului e luata in seama de la urmatorul run al fiecarui agent (promptul se reconstruieste; hash-ul
  promptului se schimba, asa cum se intampla deja la editarea instructiunilor).

## 2. Agentii in modul autonom

- `prompt.System`: daca `p.Autonomy`, in loc de `howAsk` / `howAskWait` intra:
  `Full autonomy is on: the user will not answer questions. Make reasonable decisions yourself and state your assumptions in your report.`
  Orice alta mentiune a lui `ask_user` din prompt (ex. linia "If something is unclear or blocked and only the user can
  resolve it, use mcp__team__ask_user") se scoate sau se inlocuieste cu acelasi principiu.
- Tool-ul `ask_user` ramane in lista (lista de tool-uri nu depinde de proiect), dar in modul autonom nu creeaza nicio
  intrebare si raspunde: `Full autonomy is on: the user will not answer. Decide yourself, state your assumption, and continue.`
  Fara alt efect (task-ul nu trece in asteptare).
- Intrebarile deja deschise raman; utilizatorul le poate raspunde sau inchide ca azi.

## 3. Cine decide ce ar fi decis utilizatorul

Definitie: **radacina** unui task = stramosul fara sef al asignatului (urcand pe `parent_id`); daca asignatul nu are
sef, radacina e chiar el. Helper `rootOf(agentID, agents) (store.Agent, bool)`.

In modul autonom (proiectul are `Autonomy`), peste regulile de azi:

- **Task creat din board (`ByUser`)**, ajuns la decizie (Review, etapa lider): decide radacina asignatului, cu
  `accept_task` / `send_back`. `decider`, `canDecide`, `decisionOwner` primesc proiectul (sau flag-ul) si intorc
  radacina in loc de "utilizatorul". Radacina primeste nota obisnuita "(review)". Butoanele din board raman si ele
  active (utilizatorul poate decide oricand); `userDecides` din API nu mai include aceste task-uri.
- **Asignatul e chiar radacina** (nimeni deasupra): la raportare, task-ul trece direct in Done; istoric
  kind `accepted`, actor `Foreman`, text `Accepted automatically (full autonomy)`; apoi `afterDone`.
  **Exceptie (2026-10-08, T-45 din Hobstack Landing):** un task creat din board (`ByUser`) nu trece singur in Done;
  ramane in Review si decide utilizatorul (l-a cerut el, deci e de fata). Radacina raspunde de multe ori cu variante
  sau intrebari, nu cu munca terminata. Raman cu Done automat doar task-urile fara decident care nu sunt din board
  (ex. creatorul a fost sters) si sarcinile recurente cu auto-accept (helper `autoDone`).
- **Run terminat fara `report_done`** de un agent care il putea chema (are sef, sau lucreaza intr-o etapa): nu se
  accepta niciodata automat, ca un run esuat; fara decident ramane la utilizator. La o radacina in afara etapelor
  raspunsul run-ului e raportul (radacina nu are voie la `report_done`), deci regula de mai sus se aplica normal.
- **Task blocat** (dupa `MaxSendBacks` sau fara decident): in loc de "The user decides", radacina primeste nota
  `T-N is blocked after 3 rejections or send-backs. As the root lead in full autonomy, decide: accept_task to take it as it is, or send_back with clear feedback for one more try.`
  si poate chema `accept_task` (deja permis pe Blocked) sau `send_back` pe un task Blocked (nou, doar in modul autonom
  si doar radacina). `send_back` pe Blocked pune `SendBacks = MaxSendBacks - 1` (inca o incercare) si seteaza coloana noua
  `tasks.escalated = 1`.
- **Al doilea blocaj** (task cu `escalated = 1` care se blocheaza iar, sau radacina e asignatul si nu are cine decide):
  Done automat, istoric `accepted` / `Foreman` / `Accepted automatically after repeated send-backs (full autonomy)`,
  nota livrata radacinii. Fara bucla.
- Detectorul de task-uri blocate merge ca azi: `decisionOwner` intoarce radacina, deci o radacina idle cu decizii
  nefacute e impinsa.
- **Modul oprit**: totul ca azi. Task-urile ramase Blocked/Review la oprire raman la utilizator (butoanele din board).
- **Pornirea modului** cu task-uri deja Blocked sau in Review la utilizator: nu se muta retroactiv; le decizi din board
  (YAGNI). Detectorul le prinde daca radacina devine decident.

## 4. Notificarile "Access needed" (in toate modurile)

### Citirea

`runner.tracker` citeste din evenimentul `result` campul `permission_denials`
(`[{tool_name, tool_use_id, tool_input}]`, verificat cu claude real). `runner.Outcome.Denials []Denial`,
`Denial{Tool string; Detail string}`; `Detail` = rezumat scurt din `tool_input`: `command` pentru Bash, `file_path`
pentru Edit/Write/Read, `url` pentru WebFetch, `query` pentru WebSearch, altfel JSON-ul compact; taiat la 120 de runes.

### Salvarea

Tabel nou (migrarea 010):

```sql
CREATE TABLE access_requests (
  id          INTEGER PRIMARY KEY,
  project_id  INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  agent_id    INTEGER NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
  task_id     INTEGER REFERENCES tasks(id) ON DELETE SET NULL,
  tool        TEXT NOT NULL,
  detail      TEXT NOT NULL DEFAULT '',
  count       INTEGER NOT NULL DEFAULT 1,
  created_at  TEXT NOT NULL,
  updated_at  TEXT NOT NULL,
  dismissed   INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX access_requests_open ON access_requests(project_id, dismissed);
```

`store.AccessRequest{ID, ProjectID, AgentID int64; TaskID *int64; Tool, Detail string; Count int; CreatedAt, UpdatedAt time.Time}`.
`RecordAccessDenial(projectID, agentID int64, taskID *int64, tool, detail string) error`: daca exista un rand deschis
(`dismissed = 0`) pentru (agent, tool), `count++`, `detail` = ultimul, `task_id` = ultimul (daca nu e nil),
`updated_at = now`; altfel insereaza. `ListOpenAccessRequests(projectID)`, `DismissAccessRequest(id)`,
`CountOpenAccessRequests(projectID)`.

In `recordOutcome` (team), pentru fiecare refuz al run-ului (oricare ar fi statusul run-ului): se ignora tool-urile
refuzate intentionat si fara rost pentru utilizator: `TaskCreate`, `TodoWrite`, `Agent`, `Task`. Restul se
inregistreaza; refuzurile cu acelasi tool din acelasi run se numara o singura data. Daca run-ul era pe un task:
eveniment in istoric kind `denied`, actor agentul, text `<Agent> was denied <Tool>: <detail>` (o data per tool per run).
Apoi `emit(project, "access", ...)` ca UI-ul sa se reincarce.

### API

- `GET /api/projects/{id}` -> `access: [{id, agentId, agentName, taskId, ref, tool, detail, count, updatedAt, canEnableShell}]`
  (`canEnableShell` = tool-ul e `Bash` si agentul nu are "shell" in `tools`).
- `GET /api/projects` -> `accessRequests: int` pe card.
- `POST /api/access/{id}/dismiss` -> 204.
- `POST /api/access/{id}/enable-shell` -> adauga "shell" in tool-urile agentului (salvare ca la editarea agentului),
  inchide randul, 204; 409 daca tool-ul randului nu e Bash.

### UI

- Banda "Needs you": un rand per cerere deschisa, cu avatarul agentului, `Beacon was denied Bash` + `npm test` (in
  `code`), `(3×)` daca `count > 1`, `T-2` daca are task, timpul relativ; butoane **Enable Shell for Beacon** (doar cu
  `canEnableShell`), **Open Beacon** (`#/p/<id>/team/<agentId>`), **Dismiss**. Titlul "Needs you (N)" numara intrebari +
  task-uri blocate + cereri de acces; cheia de reconstruire include id-urile si `count`.
- Cardul din Projects: badge-ul "N need(s) you" le numara si pe acestea.
- Modala de task: evenimentul `denied` apare in History ca oricare altul.

## Teste

- store: migrarea (user_version 10), `Autonomy` salvat/citit, `RecordAccessDenial` (insert, comasare, rand nou dupa
  dismiss), list/count/dismiss, cascade.
- runner: tracker-ul citeste `permission_denials` si rezuma `Detail` pentru Bash/Edit/alt tool.
- prompt: modul autonom nu mentioneaza `ask_user`, contine fraza de autonomie; modul normal neschimbat.
- team: `ask_user` in modul autonom nu creeaza intrebare; task ByUser decis de radacina (accept si send_back, iar
  utilizatorul tot poate decide); asignatul radacina -> Done automat; blocat -> nota la radacina, send_back pe Blocked,
  al doilea blocaj -> Done automat; modul oprit neschimbat; refuzuri -> cereri + evenimente `denied`, tool-uri ignorate,
  o singura numarare per tool per run.
- web: `access` / `accessRequests`, dismiss, enable-shell (si 409).
- Browser: bifa in Project instructions, pastila, rand de acces in Needs you cu Enable Shell / Dismiss, badge.

## In afara scopului

- Aprobari interactive in timpul run-ului (`--permission-prompt-tool`), sub-proiectul 3.
- Butoane "Enable" pentru alte tool-uri decat Shell (Write, Web): randul are doar Open / Dismiss.
- Mutarea retroactiva a deciziilor la pornirea modului.
