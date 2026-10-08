# Foreman: "Left for you" (ce ramane de completat de utilizator dupa un task)

Data: 2026-10-08
Status: implementat (2026-10-08).

## Scop

Un agent termina un task, dar unele valori nu le are de unde sti: numele firmei, numarul de inregistrare, un e-mail
de contact. Azi fie intreaba (`ask_user`, o intrebare pe rand, task-ul sta), fie pune `[TODO]` in cod si o
mentioneaza in raport, unde se pierde. "Left for you" e o lista scurta, legata de task, pe care utilizatorul o
completeaza cand are informatia, fara sa blocheze munca facuta.

In afara scopului: formulare cu tipuri (numar, data), atasamente pe fiecare punct, liste care se completeaza singure
din alte task-uri.

## Concept

- La `report_done` agentul poate trimite `left_for_user: ["Company name", "Registration number", ...]` (max 10
  puncte, fiecare max 200 de caractere).
- Task-ul trece mai departe normal (review, decizie), dar primeste o lista de puncte deschise. Cat timp lista are
  puncte deschise, task-ul apare in "Needs you" ca "TO FILL IN · N" (mockup 14) si pe card scrie "Waiting on you".
- Utilizatorul raspunde in comentariile task-ului (mockup 15: "Answer in the comments below"). Raspunsul ajunge la
  agentul care a lucrat pe task (nota nelivrata, il trezeste); agentul actualizeaza codul si bifeaza punctele cu un
  tool nou `tick_left_for_user(task, items)`. Utilizatorul poate bifa si singur (de ex. a completat el in cod).
- Cand toate punctele sunt bifate, task-ul iese din "Needs you". Daca task-ul era deja Done, ramane Done.

## Date

```
task_todos
  id, task_id, position, text, done_at (NULL = deschis), done_by ("You" sau numele agentului), created_at
```

Comentariile sunt `task_events` cu `kind = comment` (decis la plan: fara tabel separat).
`task_events` primeste `left_for_user`, `todo_done`, `todo_reopened`, `comment` (istoric si tab-ul Activity). `todo_done` se scrie
cand agentul (tick_left_for_user) sau utilizatorul bifeaza; `todo_reopened` cand utilizatorul debifeaza. Comentariul
utilizatorului ajunge la agentul care a lasat punctele (actorul ultimului `left_for_user`, daca mai exista; altfel asignatul),
nu neaparat la asignatul curent (in etape asignatul se muta). Mesajul lui si raspunsurile tool-urilor listeaza punctele
deschise cu numere; seful nu primeste nota pentru comentarii.

## MCP

- `report_done(..., left_for_user?: string[])`: punctele se salveaza; raspunsul spune
  `3 items left for the user; they answer in the task's comments.`
- `tick_left_for_user(task: "T-124", items: [1, 3])`: doar asignatul task-ului (sau cel care a raportat); bifeaza.
- `comment_task(task, text)`: agentul raspunde in comentarii (ca in mockup 15, "Canvas · 10:16").
- Prompt: agentii folosesc `left_for_user` doar pentru valori pe care nu le pot afla din repo sau din brief, nu ca
  inlocuitor pentru `ask_user` cand munca nu poate continua.

## UI

- Board, coloana "Needs you": cardul "TO FILL IN · 3", titlul, primul rand din raport, T-<nr>, agentul, "Fill in".
- Modala de task: caseta "Left for you" sus (checklist, "1 of 3"), "Report from <agent>", apoi "Comments" cu
  campul "Reply to <agent>...". In coloana din dreapta: Status "Waiting on you · 1 of 3".
- Lista de proiecte: punctele deschise intra in "N need you".

## Etape

1. Migrare + store (`task_todos`, `task_comments`) + API (GET/POST comments, POST todo tick).
2. MCP: `left_for_user` la `report_done`, `tick_left_for_user`, `comment_task`; texte in prompt.
3. UI: caseta din modala, comentariile, cardul din "Needs you".
