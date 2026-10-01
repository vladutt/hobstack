# Foreman: modala de task si istoricul task-ului

Data: 2026-10-01
Status: design aprobat in chat.

## Scop

Pe board, un task arata ce e de facut si ce s-a intamplat cu el, nu proza AI-ului. Cardul devine compact; click
deschide o modala cu descrierea, istoricul (timeline), raportul AI ascuns implicit si actiunile.

## Backend: istoricul

Tabel nou:

```sql
CREATE TABLE task_events (
  id         INTEGER PRIMARY KEY,
  task_id    INTEGER NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
  created_at TEXT NOT NULL,
  kind       TEXT NOT NULL,   -- created, assigned, started, reported, approved, rejected, accepted, sent_back,
                              -- blocked, user_accepted, unblocked, stage, asked, answered, waiting_subtasks,
                              -- nudged, requeued, failed
  actor      TEXT NOT NULL DEFAULT '', -- numele agentului, "You" sau "Foreman"
  text       TEXT NOT NULL             -- propozitie scurta, maxim ~200 de caractere
);
CREATE INDEX task_events_task ON task_events(task_id, id);
```

`store.TaskEvent{ID, TaskID int64; At time.Time; Kind, Actor, Text string}`; `AddTaskEvent`, `ListTaskEvents(taskID)`.

Un helper in team, `t.taskEvent(task store.Task, kind, actor, text string)`: taie textul la 200 de caractere (cu "..."),
salveaza, nu emite separat (evenimentul `task` existent reincarca UI-ul). Erorile se logheaza, nu opresc fluxul.

### Unde se inregistreaza (textele, in engleza)

- **created:** "Created by <Lead>" (create_task) / "Created by you" (din board).
- **assigned:** "Assigned to <Agent>" (la creare si la reasignare din board sau prin etape).
- **started:** "<Agent> started working" (scheduler-ul trece task-ul in In progress).
- **reported:** "<Agent> reported it done" (report_done; raportul ramane in `result`).
- **approved / rejected:** "<Reviewer> approved" / "<Reviewer> rejected: <feedback>".
- **accepted / sent_back:** "<Lead> accepted" / "<Lead> sent it back: <feedback>".
- **blocked:** "Blocked after 3 send-backs" (sau motivul real din cod).
- **user_accepted / unblocked:** "You accepted" / "You unblocked it" / "You sent it back: <feedback>" (deciziile din board).
- **stage:** "Moved to stage <Name>".
- **asked / answered:** "<Agent> asked you: <intrebare>" / "You answered" / "You dismissed the question".
- **waiting_subtasks:** "Waiting for subtasks T-4, T-5" (sub-liderul s-a oprit cu sub-task-uri deschise).
- **nudged / requeued:** "Foreman nudged <Agent>" / "Foreman requeued it" (detectorul).
- **failed:** "<Agent>'s run failed: <eroare scurta>".

Task-urile vechi (fara evenimente): API-ul intoarce un eveniment sintetic `created` cu `created_at` al task-ului si
textul "Created" (nesalvat).

### API

`GET /api/tasks/{id}` -> `{task, events, assignee, stage}`: task-ul (cu `result`), evenimentele (cele mai vechi primele),
numele asignatului si al etapei. 404 daca lipseste.

## UI

- **Cardul pe board:** compact (fara `<details>`): ref + titlu, agentul, etichetele existente (Blocked, stuck,
  "with <sef>"), plus o linie de descriere (maxim 2 randuri, CSS line-clamp) daca exista. Click/Enter -> modala.
- **Modala** (componenta noua `task-modal.js`): overlay intunecat, fereastra centrata, scroll intern; se inchide cu Esc,
  click pe overlay, ×. Continut:
  - antet: "T-2 · <titlu>", pill cu starea, agentul (avatar + nume), etapa / "with <sef>";
  - **Description** (sau "No description.");
  - **Feedback** doar daca exista;
  - **History**: lista evenimentelor (ora, text), cele mai noi jos;
  - **Show report**: buton care desfasoara `result` (ascuns implicit; lipseste daca `result` e gol);
  - actiunile de azi mutate aici: Accept / Send back (cand decide utilizatorul; Send back cere feedback intr-un
    textarea), Accept / Unblock (blocat), select Assignee (queued/blocked), Nudge (daca task-ul e in `stuck`).
- **Adresa:** `#/p/<id>/tasks/<taskId>` deschide modala; inchiderea revine la `#/p/<id>/tasks`; refresh-ul pastreaza
  modala; Back o inchide.
- **Live:** la evenimentele SSE ale proiectului, modala deschisa isi reincarca datele (GET task); un textarea in care
  utilizatorul scrie (feedback) nu se pierde (se actualizeaza doar zonele de date, nu formularul de actiune, cand
  starea nu s-a schimbat).

## Teste

- store: AddTaskEvent / ListTaskEvents, cascade la stergerea task-ului.
- team: cate un test pe drum (create, assign, start, report, approve, reject, accept, send_back, blocked, user decide,
  unblock, stage, ask/answer/dismiss, waiting_subtasks, nudge/requeue, failed) care verifica evenimentul.
- web: `GET /api/tasks/{id}` (evenimente, sintetic pentru task vechi, 404).
- Browser: card compact, modala, istoric, Show report, actiuni din modala, Esc, refresh pe link, actualizare live.

## In afara scopului

- Editarea titlului/descrierii din modala.
- Istoric retroactiv pentru task-urile vechi (doar evenimentul sintetic).
