# HobStack: stergerea agentilor cu task-uri terminate; board si Team mai simple

Data: 2026-11-05
Status: cerut de utilizator: "sa putem sterge agentii cu task-uri. Doar daca sunt in progress sau ceva sa nu putem.
Dar daca sunt done sa mearga. si sa ramana neasignate"; "sa scoatem alea 4" (filtrele All tasks / Mine / folderele de pe
board); "in team cand dau pe un agent sa-mi deschida sertarul cu chat-ul cu el".

## Stergerea agentilor (store.DeleteAgent, migrarea 034)

- tasks.assignee_id e NOT NULL cu cheie spre agents; reconstruirea tabelului tasks in SQLite (cu ON DELETE CASCADE in
  tabelele copil) e riscanta pe datele utilizatorului, deci un agent cu task-uri gata nu se sterge fizic: agents.deleted.
- Fara niciun task: randul se sterge ca inainte (sarcinile recurente raman fara asignat prin ON DELETE SET NULL).
- Doar task-uri done (si sterse dupa ce erau done): deleted = 1, detached, fara sef / reviewer / sesiune, numele devine
  "<nume> (deleted <id>)" (indexul unic pe nume se elibereaza); sarcinile recurente ale lui raman fara asignat.
  ListAgents si FindAgentByName nu-l mai intorc: pe board si in modala task-urile lui apar neasignate
  (GET /api/tasks/{id} da assignee "" pentru un agent sters).
- Orice alt task (queued, in_progress, review, blocked, sterse inainte sa fie gata): OpenTasksError cu T-N, 409.
- Un task gata al unui agent sters, redeschis pe Queued: Blocked, cu evenimentul "assign it to an agent" (Assign merge
  pe Blocked).

## Board si Team

- Board: au disparut butoanele All tasks, Mine si cele pe foldere; ramane Filter (asignatul) si avatarele.
- Team: clic pe un agent pe canvas deschide sertarul pe Chat (dublu-clic = Details).
