# Foreman: modificarile se fac doar pe task (chat doar de citire)

Data: 2026-10-09
Status: implementat (2026-10-09).

## Problema

In proiectul "Mobile-API", utilizatorul i-a cerut in chat radacinii (Atlas, cu `write` + `shell`) o modificare. Atlas a
scris codul direct in run-ul de chat si abia apoi a creat T-1 (teste) si T-2 (review). Munca lui nu are task: nu apare pe
board, n-are istoric, Changes sau Flow, iar Done-ul nu trece pe la nimeni.

Cauze:
- `howSolo` spune "Reply to their messages and do the requested work."; nimic nu cere un task pentru modificari.
- Un agent nu poate crea un task pentru el insusi: `create_task` accepta doar echipa directa (`internal/team/tools.go`),
  iar un agent fara echipa nu poate crea deloc.
- Run-urile fara task au aceleasi permisiuni ca cele pe task.

## Decizii luate cu utilizatorul

- Orice modificare in proiect se face pe un task. Analiza sau altceva: pe task doar la cerere, altfel raspuns in chat.
- Regula e **impusa de aplicatie**, nu doar ceruta in prompt: un run fara task e doar de citire. Costul acceptat: in chat
  agentul nu poate rula nici teste (pentru asta porneste un task sau i-l cere utilizatorul).

In afara scopului: wizard-ul si draft-ul (au deja run-uri doar de citire), serverele MCP din Library (raman cum sunt),
schimbari in modul in care liderii decid task-urile.

## 1. Run-urile fara task sunt doar de citire

Un run fara task = `startRun(..., taskID 0, ...)`: chat, notificari (decizii Accept / Send back, rapoarte), comentarii
pe task-uri, raspunsuri. Toate trec prin `notesRun` in `internal/team/reconcile.go`.

Permisiunile lor (`prompt.Settings` primeste tool-urile filtrate de `prompt.ReadOnlyTools(tools)`):
- dispar `write`, `shell`, `packages`;
- `git` devine `git-read`: `Bash(git status:*)`, `Bash(git diff:*)`, `Bash(git log:*)`, `Bash(git show:*)`,
  `Bash(git blame:*)` (fara add / commit / checkout -b); `git-read` e un nume intern, nu apare in UI;
- `web` ramane;
- comenzile de citire pe care claude le permite singur (`ls`, `cat`, `grep`, `find`... vezi
  `prompt.readOnlyCommands`) merg in continuare.

Run-urile pe task nu se schimba. Grant-urile de chat (`command_grants.task_id IS NULL`) nu se mai aplica (run-ul fara task
nu primeste grant-uri) si nu se mai creeaza.

## 2. Refuzurile dintr-un run fara task

In `recordDenials`, pentru un run fara task (`ar.taskID == 0`):
- refuzurile pe `Edit`, `Write`, `NotebookEdit` si `Bash` sunt intentionate: nu fac cereri de acces (nici carduri in
  "Needs you"). Agentul afla din turn ce are de facut (sectiunea 4).
- restul (ex. `WebFetch` fara tool-ul `web`) fac cereri ca pana acum.

UI: butonul "Allow … in this chat" nu mai apare in practica (nu mai exista cereri de comanda fara task); codul din
`access.js` ramane pentru cererile vechi deschise.

## 3. Tool-ul MCP `start_task`

`mcp__team__start_task(title, description, continues?)`:
- disponibil oricarui agent (cu sau fara echipa, cu sau fara sef) **doar in run-urile fara task** (`Caller.TaskID == 0`);
  intr-un run pe task nu apare in lista de tool-uri, iar un apel direct e refuzat ("You are already working on T-N").
- creeaza un task asignat agentului care cheama, in Queued, fara etape (chiar daca proiectul are etape: e munca
  agentului, nu fluxul pe coloane); `continues` ca la `create_task` (fir de lucru);
- cine decide Done:
  - run-ul a livrat un mesaj al utilizatorului in chat (`activeRun.request != nil`; raspunsurile la intrebari si
    comentariile pe task-uri au `Reply` si nu conteaza) -> `ByUser = true`: decide utilizatorul;
  - altfel (comentariu, notificare de la sef sau de la echipa) -> regula obisnuita (`decider`: seful; radacina ->
    utilizatorul). `CreatorID` ramane gol: altfel agentul si-ar decide singur task-ul.
- pasul "requested" din Flow (`recordRequest`) il leaga de mesajul din chat, ca la `create_task`;
- in chat-ul agentului apare nota `Started T-5: <title>`, iar in istoricul task-ului `Started by <agent>`;
- task-ul porneste dupa ce se termina run-ul curent (reconcilierea il ia ca orice task Queued); sesiunea Claude continua
  (`--resume`), deci agentul are contextul conversatiei.

Raspunsul tool-ului: `Started T-5. Stop now; the work continues in the task.`

Liderii folosesc in continuare `create_task` pentru echipa si `start_task` pentru partea lor. Limita: max 3 task-uri
deschise pornite cu `start_task` de acelasi agent (anti-bucla); al patrulea e refuzat cu mesaj clar.

## 4. Prompt

In turn-ul fiecarui run fara task (`prompt.NotifyTurn`), o linie fixa:

> This conversation is read-only: you can read and analyze, but not change files or run commands that change anything.
> When the user asks for a change, start a task (mcp__team__start_task for yourself, mcp__team__create_task for your
> team) and stop: the work continues in the task. For analysis or questions, answer here; create a task only if the user
> asks for one. If a comment on a finished task needs changes, start a follow-up task with continues set to that task.

`howSolo` devine: "You talk directly with the user and have no team. Answer their messages; any change to the project
is done in a task you start with mcp__team__start_task."

Linia e in turn (nu in system prompt), fiindca system prompt-ul e acelasi pentru run-urile cu si fara task.

## Teste

- prompt: `ReadOnlyTools` (write/shell/packages dispar, git -> git-read, web ramane); `Settings` pentru `git-read` nu
  are `git add` / `git commit`; turn-ul de notificare contine linia read-only.
- team:
  - un run de chat al unui agent cu `write` + `shell` are settings fara `Edit`, `Write`, `Bash` intreg; un run pe task le are;
  - grant-urile de chat nu intra intr-un run fara task;
  - refuz `Edit` / `Bash` intr-un run fara task nu face cerere; `WebFetch` face;
  - `start_task` dintr-un run de chat: task Queued pentru agent, `ByUser`, pasul "requested", nota in chat;
    dintr-un run pe task: refuzat; al patrulea task deschis: refuzat; intr-un proiect cu etape: fara etapa.
- mcp: `start_task` in lista doar pentru `TaskID == 0`.
