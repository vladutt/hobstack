# Foreman: conversatia pe task (cum s-au coordonat agentii)

Data: 2026-10-01
Status: design aprobat in chat (directia); detaliul de stocare ales la scrierea spec-ului.

## Scop

In modala unui task, utilizatorul vede cum s-au coordonat agentii pe acel task: brief-ul liderului, ce a spus
agentul in fiecare run, raportul, feedback-ul reviewer-ului / liderului, intrebarile catre utilizator si raspunsurile,
impingerile lui Foreman. Ca un chat intre agenti, cu textul complet, live.

## De ce nu din notele din thread-uri

Notele (`messages`, kind `event`) sunt scrise per destinatar (aceeasi comunicare apare de doua ori, cu texte diferite)
si sunt scurte ("Reported T-1"). Textul complet exista in punctele unde se scrie deja istoricul task-ului
(`task_events`, modala de task). Deci extindem `task_events`.

## Backend

### Stocare

Migrarea 012:

```sql
ALTER TABLE task_events ADD COLUMN to_actor TEXT NOT NULL DEFAULT ''; -- destinatarul: agent, "You", "" = nimeni anume
ALTER TABLE task_events ADD COLUMN body     TEXT NOT NULL DEFAULT ''; -- textul complet al comunicarii ('' = doar istoric)
```

`store.TaskEvent` primeste `To string` (JSON `to`) si `Body string` (JSON `body`). `AddTaskEvent` le scrie. `body` se
limiteaza la 20000 de runes (taiat cu " [...]").

Helper team nou: `t.taskMsg(task, kind, from, to, text, body string)`: ca `taskEvent` (text scurt pentru History,
taiat la 200), plus `to` si `body` complet. `taskEvent` ramane pentru evenimentele fara continut.

### Ce devine comunicare (body completat)

| Moment | kind | from -> to | body |
|---|---|---|---|
| create_task (lider) | created | lider -> asignat (sau "" daca e in coada unei etape) | titlul + "\n\n" + descrierea |
| task creat din board | created | You -> asignat | titlul + descrierea |
| report_done / terminat fara raport | reported | asignat -> reviewer sau decident (sau "You") | raportul (`task.Result` la acel moment) |
| review_approve | approved | reviewer -> decident | nota reviewer-ului daca exista, altfel "Approved" |
| review_reject | rejected | reviewer -> asignat | feedback-ul |
| accept_task | accepted | lider -> asignat | nota liderului daca exista, altfel "Accepted" |
| send_back (lider, inclusiv pe Blocked in autonomie) | sent_back | lider -> asignat | feedback-ul |
| decizia utilizatorului din board | user_accepted / sent_back | You -> asignat | "Accepted" / feedback-ul |
| ask_user cu task | asked | asignat -> You | intrebarea (+ optiunile pe randuri "- ...") |
| raspuns la intrebare | answered | You -> asignat | raspunsul (sau "Dismissed") |
| nudge pe task (decizie) | nudged | Foreman -> agentul impins | textul notei de impingere |

Celelalte evenimente (assigned, started, stage, blocked, requeued, waiting_subtasks, failed, denied) raman doar in
History (body gol).

### Raspunsurile agentilor din run-uri

Mesajele agentilor (`messages` cu `kind = msg`, `author` = numele agentului, `run_id` setat) dintr-un run cu
`runs.task_id = task` sunt si ele parte din conversatie: "ce a spus agentul" la finalul run-ului.
`store.ListTaskRunMessages(taskID) ([]Message, error)`: join `messages` x `runs` pe `run_id`, filtrat pe `task_id`,
doar `kind = 'msg'` si `author != 'user'`, ordonat dupa id. Functioneaza si pentru task-urile vechi (datele exista deja).

### API

`GET /api/tasks/{id}` primeste in plus `conversation`: lista unita si sortata dupa timp (apoi dupa tip, evenimentele
inaintea mesajelor la egalitate) cu elemente:

```json
{ "type": "event", "at": "...", "kind": "reported", "from": "Rook", "to": "Lead", "text": "<body>" }
{ "type": "run",   "at": "...", "from": "Rook", "text": "<mesajul agentului>", "runId": "..." }
```

Doar evenimentele cu `body` nevid intra. Lista e mereu non-nil. Plus `working: "<Agent>"` daca asignatul are acum un
run activ pe acest task (altfel "").

## UI (modala de task)

- Doua tab-uri sub descriere: **Conversation** si **History**. Implicit Conversation cand are elemente, altfel History.
  Tab-ul ales se pastreaza la reincarcarile live (SSE) cat timp modala e deschisa.
- **Conversation**: bule ca in chat; antet "Lead → Rook · 12:04" (avatarul expeditorului; "You" si "Foreman" cu
  avatarul lor); textul complet, cu `white-space: pre-wrap`. Mesajele din run-uri: antet "Rook · 12:05" fara sageata
  (e ce a spus agentul). Bulele utilizatorului ("You") aliniate la dreapta, restul la stanga. Texte lungi (> 600 de
  caractere) se arata scurtate cu "Show more".
- Daca `working` e setat: un rand la final "Rook is working on it…" cu link "Watch live" catre
  `#/p/<id>/team/<agentId>` (chat-ul agentului, unde se vede output-ul live).
- Task vechi fara conversatie salvata: Conversation arata doar raspunsurile din run-uri (daca exista) sau
  "No conversation recorded for this task."
- Live: la evenimentele SSE ale proiectului, modala reincarca datele (ca azi); conversatia se redeseneaza doar daca s-a
  schimbat numarul de elemente sau `working`, ca scroll-ul si "Show more" deschise sa nu se piarda.

## Teste

- store: migrarea (user_version 12), `AddTaskEvent` cu `To`/`Body` (si taierea la 20000), `ListTaskRunMessages`
  (doar mesajele agentului din run-urile task-ului, nu din alte task-uri, nu ale utilizatorului).
- team: cate un test pe randurile din tabel (create din MCP si din board, report, approve, reject, accept, send_back,
  decizia utilizatorului, ask/answer, nudge) care verifica `kind`, `from`, `to`, `body`.
- web: `conversation` (evenimente cu body + mesaje din run-uri, ordonate; evenimentele fara body lipsesc), `working`,
  task vechi.
- Browser: tab-urile, bulele cu sageti, You la dreapta, Show more, Working + Watch live, live update fara pierderea
  tab-ului.

## In afara scopului

- Mesaje directe intre agenti in timpul lucrului (tool MCP nou); se poate adauga ulterior, iar mesajele ar intra in
  aceeasi conversatie.
- Output-ul complet al run-ului (tool calls) in modala; "Watch live" duce la chat-ul agentului.
- Conversatie retroactiva pentru evenimentele vechi (doar raspunsurile din run-uri apar).
