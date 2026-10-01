# Foreman: detectorul de task-uri blocate

Data: 2026-10-01
Status: design aprobat in chat.

## Problema

Un task poate sta la nesfarsit fara ca nimeni sa-l miste: un sef care a primit rezultatul, l-a citit si s-a oprit
fara accept_task/send_back (cazul real T-2/T-3 cu Beacon), sau un task ramas In progress fara run (dupa un esec).
"Needs you" prinde doar intrebarile explicite (ask_user).

## Decizii (din brainstorming)

- **Reactie:** prima data Foreman impinge singur agentul responsabil (o singura data); daca tot e blocat, task-ul
  apare in "Needs you" cu butoanele Nudge again / Open <agent>.
- **Prag:** 5 minute fara schimbare.
- Proiectele pe pauza nu se verifica (pauza e alegerea utilizatorului).

## Ce e "blocat"

Un task e blocat cand `now - updated_at >= 5 min`, proiectul nu e pe pauza si:

1. **Asteapta o decizie** (`kind = "decision"`): status `review`, decidentul `decisionOwner(task, agents)` e un agent
   (nu utilizatorul: task-urile create din board si cele blocate au deja butoane) si acel agent e **idle**:
   - nu are run activ;
   - nu are mesaje nelivrate (`HasUndelivered`);
   - nu are intrebari deschise (o intrebare deschisa = asteapta utilizatorul, apare deja in "Needs you").
2. **Orfan** (`kind = "orphan"`): status `in_progress`, asignatul nu are run activ, task-ul nu are intrebari
   deschise (`HasOpenQuestionForTask`) si nu are sub-task-uri nefinalizate.

Task-urile cu status `queued` nu sunt blocate (le porneste scheduler-ul cand e loc).

## Impingerea

Coloana noua `tasks.nudged_at` (TEXT, NULL). `SaveTask` o pune pe NULL (orice schimbare de stare = activitate).

Verificarea ruleaza la fiecare minut (goroutine-ul `Team.Run` primeste un ticker de 1 minut; testele cheama direct
`CheckStuck(now)`):
- task blocat cu `nudged_at` NULL -> **impingere automata**, apoi `nudged_at = now`;
- task blocat cu `nudged_at` setat si `now - nudged_at >= 5 min` -> **blocat vizibil** (apare in `stuck`), fara alta
  impingere automata.

Impingerea:
- **decision:** o nota nelivrata (o singura nota per agent, pentru toate task-urile lui blocate in aceeasi verificare)
  care il trezeste:
  - sef/lider: `T-2, T-3 are waiting for your decision: call accept_task or send_back. If you cannot decide without the user, call ask_user.`
  - reviewer (stage reviewer): `T-2 is waiting for your review: call review_approve or review_reject. If you cannot decide without the user, call ask_user.`
  + o nota livrata in thread-ul agentului: `Foreman nudged <Name>: T-2, T-3 are waiting for a decision.`
- **orphan:** task-ul trece in `queued` (acelasi asignat; scheduler-ul il reporneste) + nota livrata
  `Foreman requeued T-4: it was in progress with nothing running.`
- dupa impingere: `Kick(project)` si evenimentul `task` pentru task-urile atinse.

Impingerea manuala `POST /api/tasks/{id}/nudge`: aceeasi impingere pentru acel task (chiar daca nu e inca vizibil
blocat), `nudged_at = now`; 409 daca task-ul nu e blocat dupa regulile de mai sus fara prag (adica nu e in review la un
agent idle si nici orfan).

## API

- `GET /api/projects/{id}` -> `stuck: [{taskId, ref, title, kind ("decision"|"orphan"), agentId, agentName, since}]`
  (`since` = updated_at; doar cele vizibile, adica impinse deja si tot blocate dupa inca 5 min).
- `GET /api/projects` -> `stuckTasks: int` pe card.

## UI

- **Banda "Needs you":** pe langa intrebari, un rand per task blocat: avatarul agentului, "T-2 · <titlu>" si
  "waiting for Beacon to decide · 12 min" / "in progress but nothing is running · 12 min"; butoane **Nudge again**
  (`POST nudge`, randul dispare dupa reload) si **Open <Name>** (`#/p/<id>/team/<agentId>`, panoul Chat). Titlul:
  "Needs you (N)" cu N = intrebari + task-uri blocate. Cheia de reconstruire include si id-urile task-urilor blocate.
- **Cardul din Projects:** badge-ul "N need(s) you" numara intrebari + task-uri blocate.
- **Board:** cardul task-ului blocat primeste o eticheta amber "stuck" (cu `title` = motivul).

## Teste

- store: `nudged_at` setat/curatat de SaveTask; `SetTaskNudged`.
- team (cu `now` injectat): decision -> o impingere (nota nelivrata la decident, nudged_at setat), apoi vizibil dupa
  inca 5 min; agent activ / mesaje nelivrate / intrebare deschisa / proiect pe pauza / decide utilizatorul -> nimic;
  orphan -> requeue; o singura nota per agent pentru mai multe task-uri; nudge manual; 409 pe task neblocat.
- web: `stuck` si `stuckTasks`; ruta nudge.
- Browser: task blocat in banda, Nudge again, Open, badge, eticheta pe board.

## In afara scopului

- Prag configurabil per proiect.
- Detectarea agentilor care ruleaza la nesfarsit (run lung).
- Task-uri in `queued` care nu pornesc (de ex. limita de paralelism).
