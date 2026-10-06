# Foreman: proiecte legate (agentii vorbesc intre proiecte)

Data: 2026-10-06
Status: design aprobat in chat.

## Scop

Utilizatorul leaga doua proiecte (de ex. *Frontend* si *Backend*). Agentii lor pot apoi:

- **pune intrebari** proiectului legat ("ce format are raspunsul de la /users?") si primi raspunsul inapoi;
- **cere munca** proiectului legat ("adauga endpoint-ul /export"): devine un task pe board-ul celuilalt proiect, iar
  rezultatul se intoarce la cel care a cerut.

Proiectul celalalt e o cutie neagra: orice cerere ajunge la **radacina** lui, care decide singura (raspunde, sparge in
task-uri pentru echipa ei). Ierarhia, etapele, reviewer-ii si regulile proiectului tinta raman neschimbate.

In afara scopului: notificari automate intre proiecte, legaturi cu sens, conversatii directe cu alti agenti decat
radacina, vederea board-ului celuilalt proiect.

## Legaturi

- Fara sens: daca *Frontend* e legat cu *Backend*, fiecare ii poate trimite celuilalt intrebari si cereri.
- Le creeaza si le sterge doar utilizatorul, din UI. Agentii nu pot lega proiecte.
- Un proiect nu se leaga cu el insusi; o pereche exista o singura data (indiferent de ordine).

## Cine are voie

| tool MCP | cine il primeste | ce face |
|---|---|---|
| `list_linked_projects` | orice agent al unui proiect cu cel putin o legatura | proiectele legate: nume, scop, radacina |
| `ask_project(project, question)` | orice agent al unui proiect cu legaturi | intrebare catre radacina proiectului legat |
| `reply_to_project(request, answer)` | radacinile | raspunde la o intrebare primita (`Q-7`) |
| `request_project_work(project, title, description)` | liderii si radacinile (`CanAddAgents`) | cere munca proiectului legat |
| `report_done` | in plus: radacina, intr-un run pe un task venit din alt proiect | livreaza rezultatul inapoi |

Regulile se verifica in aplicatie, din baza de date (legatura exista, rolul, limitele), nu din flag-urile run-ului;
flag-urile din `mcp.Caller` decid doar ce tool-uri se listeaza.

"Radacina proiectului tinta" = prima radacina din `store.ListAgents` (radacinile vin primele, in ordinea adaugarii).
Fara radacina (toti agentii nelegati): `"Backend" has no root agent to receive it`.

Numele proiectului in tool-uri: numele exact, fara diferenta intre litere mari si mici. Un proiect nelegat sau
inexistent: `"X" is not a linked project. Linked projects: A, B` (sau `This project has no linked projects`).

## Intrebari (`kind = question`)

1. `ask_project` valideaza: legatura exista; intrebarea 1..2000 caractere; agentul are sub **3 intrebari deschise**
   trimise altor proiecte; regula anti-bucla (mai jos).
2. Se creeaza cererea (`status = open`) si o nota nelivrata la radacina tinta:
   `Question from Frontend (Rook) [Q-7]: <intrebarea>\nAnswer it with reply_to_project(request: "Q-7", answer).`
   Nota are `messages.request_id = 7`.
3. Raspuns catre cel care intreaba: `Question sent to Backend as Q-7. The answer arrives as a message; continue meanwhile.`
   Daca proiectul tinta e pe pauza, in plus: ` Backend is paused: it answers when the user starts it.`
4. `reply_to_project(request, answer)`: `request` accepta `Q-7` sau `7`. Doar radacina proiectului tinta, doar pe o
   intrebare `open` adresata proiectului ei; raspuns 1..10000 caractere. Cererea devine `answered`, iar cel care a
   intrebat primeste o nota nelivrata (il trezeste): `Answer from Backend to Q-7: <raspuns>`.
5. **Anti-bucla:** un run in care s-a livrat cel putin o nota cu o intrebare primita din alt proiect nu poate chema
   `ask_project`: `you are answering a question from another project; answer with what you know, or say you don't know`.
   Se tine in memorie pe run-ul activ (ca `request` din `activeRun`), din `messages.request_id` al notelor livrate.
6. Pe pauza, nota asteapta (pe pauza ruleaza doar agentii carora le-a scris utilizatorul), ca orice nota.

## Cereri de lucru (`kind = work`)

1. `request_project_work` valideaza: legatura exista; agentul e lider sau radacina (din DB); titlu 1..200,
   descriere pana la 10000 caractere.
2. **Direct** daca run-ul celui care cere a pornit dintr-un mesaj real al utilizatorului din chat
   (`t.requestOf(c.AgentID, c.RunID)`; raspunsurile la `ask_user` nu conteaza, vezi `messages.reply`). Full autonomy
   NU ocoleste aprobarea. Altfel cererea e `pending` si apare in "Needs you" **la proiectul tinta**.
3. Raspuns catre cel care cere:
   - direct: `Backend got your request as W-3 (task T-12). You will get its report as a message.`
   - cu aprobare: `Request W-3 sent to Backend; the user approves it first. You will get a message either way.`
4. **Approve** (sau creare directa): in proiectul tinta se creeaza un task obisnuit asignat radacinii, cu
   `tasks.request_id`, titlul si descrierea cererii (descrierea primeste la final
   `\n\nRequested by Frontend (Lead) as W-3.`). Istoricul task-ului: pasul `created` cu textul
   `Requested by Frontend (Lead)` (actor = numele proiectului sursa). Cererea devine `approved`, `to_task` = task-ul.
   Cel care a cerut primeste o nota: `Backend accepted W-3 (task T-12 there).` (doar la aprobare; la directa raspunsul
   tool-ului ajunge).
5. **Dismiss:** cererea devine `dismissed`; nota la cel care a cerut: `The user dismissed your request W-3 to Backend.`
6. **Livrare:** radacina tinta, intr-un run pe acel task, cheama `report_done(summary)` (are tool-ul chiar fara sef,
   `Caller.FromRequest`). Task-ul trece direct in Done (nu exista lider deasupra), cererea devine `done` cu
   `answer = summary`, iar cel care a cerut primeste o nota nelivrata:
   `Backend finished W-3: <summary>` (plus ` (your task T-4)` daca cererea a venit dintr-un run pe un task).
   Sub-task-urile radacinii merg ca de obicei; radacina raporteaza cand le are gata.
7. Task-ul tinta urmeaza regulile proiectului tinta (etape, pauza, max parallel). Pe pauza sta in Queued.

## Ciclul de viata la stergeri

- **Stergi legatura:** intrebarile `open` dintre cele doua proiecte devin `closed` (nota la cel care a intrebat:
  `Q-7 to Backend was closed: the projects are no longer linked.`); cererile `pending` devin `dismissed` (nota
  similara). Cererile `approved` isi continua drumul: rezultatul tot ajunge inapoi.
- **Stergi un proiect:** inainte de stergere, aceleasi note pentru cererile deschise in care proiectul e tinta;
  legaturile si cererile lui dispar (cascade).
- **Agentul care a cerut a fost sters:** `from_agent` devine NULL; rezultatul ramane doar in istoricul cererii.

## Store

Migrarea 017:

```sql
-- legaturi intre proiecte si cererile dintre ele
CREATE TABLE project_links (
  id         INTEGER PRIMARY KEY,
  project_a  INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE, -- mereu id-ul mai mic
  project_b  INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  created_at TEXT NOT NULL,
  UNIQUE (project_a, project_b),
  CHECK (project_a < project_b)
);

CREATE TABLE project_requests (
  id           INTEGER PRIMARY KEY,
  kind         TEXT NOT NULL CHECK (kind IN ('question', 'work')),
  from_project INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  from_agent   INTEGER REFERENCES agents(id) ON DELETE SET NULL,
  from_task    INTEGER REFERENCES tasks(id) ON DELETE SET NULL,
  to_project   INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  title        TEXT NOT NULL DEFAULT '',  -- doar work
  text         TEXT NOT NULL,             -- intrebarea sau descrierea
  status       TEXT NOT NULL CHECK (status IN ('open', 'answered', 'closed', 'pending', 'approved', 'done', 'dismissed')),
  answer       TEXT NOT NULL DEFAULT '',  -- raspunsul sau raportul
  to_task      INTEGER REFERENCES tasks(id) ON DELETE SET NULL,
  created_at   TEXT NOT NULL,
  closed_at    TEXT
);
CREATE INDEX project_requests_to ON project_requests(to_project, status);
CREATE INDEX project_requests_from ON project_requests(from_agent, kind, status);

ALTER TABLE tasks ADD COLUMN request_id INTEGER REFERENCES project_requests(id) ON DELETE SET NULL;
ALTER TABLE messages ADD COLUMN request_id INTEGER REFERENCES project_requests(id) ON DELETE SET NULL;
```

Referinta afisata: `Q-<id>` pentru intrebari, `W-<id>` pentru cereri de lucru (acelasi sir de id-uri).

## MCP

`mcp.Caller` primeste `HasLinks bool` (proiectul are legaturi), `IsRoot bool` si `FromRequest bool` (run pe un task cu
`request_id`). `toolsFor`: `HasLinks` -> `list_linked_projects`, `ask_project`; `HasLinks && IsRoot` ->
`reply_to_project`; `HasLinks && CanAddAgents` -> `request_project_work`; `FromRequest` -> `report_done` (fara dubluri).

## Prompt

Pentru agentii unui proiect cu legaturi, un paragraf in "How you work":

> This project is linked with other projects. Use mcp__team__list_linked_projects to see them. To ask one of them
> something, use mcp__team__ask_project; the answer arrives later as a message. Only ask what your own team cannot
> answer.

In plus, pentru lideri si radacini: `To have a linked project do work for you, use mcp__team__request_project_work.`
Pentru radacini: `Questions from linked projects arrive as messages with a reference like Q-7: answer each with
mcp__team__reply_to_project. Work requested by a linked project arrives as a task: when it is done, call
mcp__team__report_done on it.`

## Web

- `GET /api/projects/{id}/links` -> `[{projectId, name}]`; `POST /api/projects/{id}/links` `{projectId}` -> 201
  (400 pentru acelasi proiect, 404 proiect inexistent, 409 legatura exista); `DELETE /api/projects/{id}/links/{other}`
  -> 204 (aplica ciclul de viata de mai sus).
- `GET /api/projects/{id}`: `links` (lista de mai sus) si `workRequests` (cererile `pending` catre proiect:
  `{id, ref, fromProject, fromAgent, title, text, createdAt}`); cardul de proiect: `workRequests` (numar, in "Needs you").
- `POST /api/project-requests/{id}/approve`, `/dismiss` -> 204; 404; 409 daca nu mai e `pending` sau legatura nu mai
  exista.
- Task-urile primesc `requestId` si `fromProject` (numele proiectului sursa) in JSON.

## UI

- **Proiect -> "Linked projects"** (langa Project instructions): lista legaturilor (nume, link spre proiect, Remove cu
  confirmare) si un select "Link a project" cu proiectele nelegate.
- **"Needs you"** (proiectul tinta): card `<Frontend> (<Lead>) asks for work: <titlu>` cu descrierea (restransa) si
  **Approve** / **Dismiss**; eroarea ca toast, cardul ramane.
- **Board:** pe cardul task-ului eticheta `From <Frontend>`; in modala, istoricul arata pasul `created`.
- Intrebarile si raspunsurile apar ca note in chat-ul celor doi agenti (fara ecran separat).

## Teste

- store: legaturi (ordinea, unicitatea, cascade), cererile si tranzitiile de stare, `request_id` pe task si mesaj.
- team: intrebare -> raspuns (nota la cel care intreaba); limita de 3; anti-bucla; proiect tinta pe pauza (textul);
  cerere de lucru cu aprobare si cu Dismiss; cerere directa dintr-un run de chat; un raspuns la `ask_user` nu face
  cererea directa; Full autonomy nu o face directa; `report_done` al radacinii pe task-ul tinta (Done, nota la cel
  care a cerut); stergerea legaturii si a proiectului (notele); refuzuri (nelegat, fara radacina, worker la
  `request_project_work`, ne-radacina la `reply_to_project`).
- mcp: cine primeste ce tool.
- web: legaturi (201/400/404/409/204), approve/dismiss (204/404/409), campurile noi din detaliu.
- UI: `node --check`; screenshot-uri prin Chrome headless (CDP) pentru "Linked projects", cardul din "Needs you" si
  eticheta de pe board.
