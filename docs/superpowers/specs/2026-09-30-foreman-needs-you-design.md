# Foreman: "Needs you" — intrebarile agentilor catre utilizator

Data: 2026-09-30
Status: design aprobat in chat.

## Problema

Un agent cu sef (ex. sub-liderul Beacon) care are nevoie de o decizie de la utilizator ("da-mi voie la Bash sau
trimite-mi output-ul testelor") se opreste cu intrebarea in mesajul lui. Mesajul ramane in thread-ul acelui agent,
pe care utilizatorul nu il urmareste: task-urile raman in review si nu se mai intampla nimic.

## Scop

- Orice agent poate pune explicit o intrebare utilizatorului (tool MCP `ask_user`).
- Intrebarile deschise se vad clar: banda "Needs you" pe pagina proiectului, badge pe cardul din Projects, caseta in
  chat-ul agentului si semn "?" pe agent (banda echipei si canvas).
- Raspunsul ajunge la agent ca mesaj si il trezeste; un task in asteptare continua cu raspunsul.

## Decizii (din brainstorming)

- **Detectie:** tool explicit `ask_user`, nu euristici pe text si nu escaladare prin sefi.
- **Unde apare:** banda pe pagina proiectului + badge pe cardul din Projects + chat-ul agentului (si semn pe agent).
- Fara notificari de browser (in afara scopului).

## Backend

### Date

Tabel nou:

```sql
CREATE TABLE questions (
  id          INTEGER PRIMARY KEY,
  project_id  INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  agent_id    INTEGER NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
  task_id     INTEGER REFERENCES tasks(id) ON DELETE SET NULL, -- task-ul run-ului care a intrebat (NULL = fara task)
  run_id      TEXT NOT NULL DEFAULT '',
  text        TEXT NOT NULL,
  options     TEXT NOT NULL DEFAULT '[]',                       -- JSON: raspunsuri scurte propuse
  answer      TEXT NOT NULL DEFAULT '',
  status      TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open','answered','dismissed')),
  created_at  TEXT NOT NULL,
  closed_at   TEXT
);
CREATE INDEX questions_project ON questions(project_id, status);
```

`store.Question { ID, ProjectID, AgentID int64; TaskID *int64; RunID, Text string; Options []string; Answer, Status
string; CreatedAt time.Time; ClosedAt *time.Time }`. Functii: `AddQuestion`, `ListOpenQuestions(projectID)`,
`CountOpenQuestions(projectID)`, `GetQuestion(id)`, `CloseQuestion(id, status, answer)` (doar din `open`, altfel
`ErrConflict`), `HasOpenQuestionForRun(runID)`.

### Tool-ul MCP `ask_user`

- Disponibil pentru orice agent al unei echipe (nu pentru analistul din wizard).
- Argumente: `question` (obligatoriu, maxim 2000 de caractere), `options` (optional, 0-4 siruri, fiecare maxim 60).
- Salveaza intrebarea (agent, task-ul run-ului, run id), publica evenimentul `question`, scrie in thread-ul agentului
  un eveniment "Asked you: <question>".
- Intoarce: `Question sent to the user. Stop now; the answer will arrive as a message.`

### Prompt

In sectiunea "How you work" a fiecarui agent se adauga:
"If you need a decision, a permission or information that only the user can give, call mcp__team__ask_user with a
clear question (and short options if it is a choice), then stop. Do not guess, and do not just end your message with
a question." Pentru agentii cu sef se adauga: "Do not call report_done while you wait for the answer."

Asta schimba system prompt-ul, deci sesiunile existente pornesc o data de la zero (acceptat).

### Task-ul cat asteapta

In `recordOutcome` (internal/team/run.go): daca run-ul s-a terminat fara report_done, pe un task inca In progress
al agentului, si run-ul a pus o intrebare (`HasOpenQuestionForRun`), task-ul ramane In progress (nu merge in review),
iar in thread apare nota "T-3 is waiting for your answer".

### Raspunsul

`POST /api/questions/{id}/answer {text}` (text obligatoriu) si `POST /api/questions/{id}/dismiss`:
- inchide intrebarea (409 daca nu mai e deschisa);
- trimite agentului un mesaj de la utilizator (acelasi drum ca `UserMessage`, deci il trezeste si isi informeaza
  seful ca pana acum):
  - raspuns: `Answer to your question "<intrebare scurtata>": <text>`
  - dismiss: `The user dismissed your question "<intrebare scurtata>" without answering. Decide yourself or ask differently.`
- daca intrebarea avea task si task-ul e inca In progress la acel agent, fara run activ: task-ul trece in Queued
  (acelasi agent), ca urmatorul run sa fie pe task, cu raspunsul in notificari.
- publica evenimentele `question` si `task`.

### API

- `GET /api/projects/{id}` adauga `questions` (deschise, cele mai vechi primele).
- `GET /api/projects` adauga `openQuestions` (numar) pe fiecare card.

## UI

- **Banda "Needs you"** (workspace.js), deasupra tab-urilor, doar cand exista intrebari deschise: pentru fiecare,
  avatar + "Agent · T-3", textul, optiunile ca butoane (click = raspuns trimis), camp de raspuns + **Send** +
  **Dismiss**. Se actualizeaza la evenimentele SSE `question` (reload ca la celelalte evenimente).
- **Cardul din Projects:** badge amber "1 needs you" / "N need you" si contur amber.
- **Chat:** daca agentul selectat are o intrebare deschisa, o caseta amber deasupra campului de scris, cu aceleasi
  controale.
- **Semn pe agent:** "?" amber pe butonul din banda echipei si pe nodul din canvas.
- Componenta de intrebare e una singura (folosita in banda si in chat).

## Cazuri limita

- Agentul sters: intrebarile lui dispar (CASCADE).
- Task sters sau mutat intre timp: raspunsul ajunge doar ca mesaj (task_id NULL sau task in alta stare).
- Doua raspunsuri simultane: al doilea primeste 409.
- Proiect pe pauza: raspunsul se salveaza ca mesaj; agentul il primeste cand pornesti echipa.
- Un agent pune mai multe intrebari: toate apar; fiecare raspuns e un mesaj separat.

## Teste

- store: CRUD intrebari, CloseQuestion o singura data, HasOpenQuestionForRun.
- mcp/team: `ask_user` in lista de tool-uri pentru agenti, nu pentru analist; validare lungimi; intrebarea salvata
  cu task si run.
- team: run fara report_done dupa ask_user -> task ramane In progress; raspuns -> task Queued + mesaj; dismiss.
- prompt: textul nou in "How you work".
- web: answer/dismiss (200, 409, 404), `questions` in proiect, `openQuestions` pe card.
- Browser (CDP + claude fals care apeleaza ask_user): banda, butoane de optiuni, raspuns, badge pe card, caseta in chat.

## In afara scopului

- Notificari de browser.
- Intrebari cu atasamente sau raspunsuri structurate (formulare).
- Istoric al intrebarilor inchise in UI (raman in DB si in thread-ul agentului).
