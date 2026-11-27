# Memoria proiectului (Project memory)

Data: 2026-11-27. Stare: design, neaprobat.

## De ce

Ce invata o echipa (de ce un task s-a intors de 3 ori, ce a decis utilizatorul la deblocare, o alegere de arhitectura)
traieste azi doar in sesiunea Claude a unui agent. O sesiune noua (instructiuni schimbate, New chat, cont schimbat) sau
un al doilea agent cu acelasi rol nu mai stiu nimic. Singurul loc comun e "Shared context", pe care il scrie doar
utilizatorul si care e in system prompt: orice schimbare acolo reseteaza sesiunile tuturor agentilor.

Vrem o memorie a proiectului: fapte si decizii scurte, cu cheie, autor si data, pe care orice agent al proiectului le
vede de la prima rulare si le poate scrie, fara sa reseteze sesiuni.

## Scop

- Doar agentii HobStack, doar in proiectul lor. Fara acces din afara (Claude Code din terminal, Desktop) si fara
  partajare intre proiecte legate (acolo ramane ask_project).
- Varianta aleasa: B, index in fiecare tura + tool-uri MCP (vezi Approaches).

## Non-goals / anti-goals

- Nu inlocuieste Shared context (instructiuni stabile, scrise de utilizator) si nici fisierele CLAUDE.md din foldere.
- Nu e istoricul unui task. Trimiterile inapoi repetate pe acelasi task sunt o schimbare separata (istoricul feedback-ului
  in TaskTurn), in afara acestui spec.
- Fara captura automata din evenimente (send_back, unblock, decide): ar umple memoria cu reclamatii punctuale.
- Fara cautare semantica / embeddings. Cautare simpla pe text.
- Anti-goal: o memorie care creste fara control si ajunge sa coste mai mult in tokeni decat aduce, sau care contine
  "fapte" vechi pe care nimeni nu le mai vede. De aici limitele, pagina din UI si blocarea.
- Anti-goal: sa reseteze sesiunile. Nimic din memorie nu intra in system prompt.

## Approaches

- **A. Doar la cerere** (tool-uri remember / recall / search, nimic in tura): cel mai simplu, dar agentul nu cauta ce nu
  stie ca exista (skill-urile default n-au fost chemate deloc in 6396 de run-uri). Respins.
- **B. Index in fiecare tura + tool-uri** (ales de utilizator): fiecare agent vede cheile si rezumatele la fiecare run,
  textul intreg la cerere. Cost: indexul in fiecare tura, limitat.
- **C. Scrisa de HobStack din evenimente**, fara tool pentru agenti: zgomotoasa, fara chei bune. Respins.

## Model de date (migrarea 043)

`project_memory`:
- `id`, `project_id` (FK, ON DELETE CASCADE)
- `key` TEXT: unic per proiect, `^[a-z0-9][a-z0-9._-]{0,59}$` (max 60). Agentul il da; serverul il normalizeaza
  (lowercase, spatiile -> `-`) inainte de validare.
- `summary` TEXT: un rand, max 140 caractere, obligatoriu (merge in index).
- `body` TEXT: max 4000 caractere, optional (doar prin recall si in UI).
- `locked` INTEGER 0/1: blocata de utilizator; agentii n-o pot schimba sau sterge.
- `author_agent_id` (NULL = utilizatorul), `author_name` (copiat la scriere: ramane si dupa stergerea agentului; "You"
  pentru utilizator), `task_id` (NULL sau task-ul pe care s-a scris).
- `created_at`, `updated_at`, `deleted_at` (NULL = activa).
- UNIQUE(project_id, key) — o cheie stearsa si scrisa din nou reinvie acelasi rand (deleted_at = NULL).
  (Decizia builder-ului, de verificat:) stergerea scoate si blocarea, deci randul reinviat porneste neblocat, cu autorul
  noii scrieri. Un agent poate reinvia si o cheie stearsa de utilizator; raspunsul lui remember spune atunci "was deleted
  by You on <data>: <motiv>" ca agentul sa vada ca reinvie ceva sters intentionat, iar istoricul arata reinvierea.
- `POST /api/projects/{id}/memory` pe o cheie activa = 409 ("key exists"); pe o cheie stearsa = reinviere.

`project_memory_versions`: fiecare scriere / stergere / restaurare / blocare: `memory_id`, `action`
(`set` / `forget` / `restore` / `lock` / `unlock`), `summary`, `body`, `author_agent_id`, `author_name`, `task_id`,
`reason` (la forget), `at`. Istoricul nu se sterge (doar cu proiectul).

Limite: max 200 de intrari active per proiect (a 201-a: eroare care spune sa stearga sau sa actualizeze una).

## Tool-uri MCP (internal/mcp/tools.go, implementarea in internal/team/memory.go)

Disponibile tuturor agentilor proiectului, in run-uri pe task si fara task (chat, notificari): memoria nu e un fisier,
deci nu intra sub regula "chat doar de citire". Nu exista in run-urile de draft, wizard si brainstorming (acolo tool-urile
sunt fixe).

- `remember(key, summary, text?, task?)`: creeaza sau inlocuieste intrarea. `task` = "T-N" optional; daca run-ul e pe
  un task si lipseste, se pune task-ul run-ului. Raspuns: "Saved <key>" / "Updated <key> (was: <summary vechi>)". Pe o
  intrare blocata: eroare "<key> is locked by the user. If it is wrong, tell the user (ask_user or your report)."
  Scrierea identica (acelasi summary si body) = no-op, fara versiune.
- `recall(key)`: summary, text, autor, data, task-ul; pentru o cheie stearsa spune ca a fost stearsa, de cine si de ce.
  Cheie necunoscuta: lista cu cele mai apropiate 5 chei (prefix / contine).
- `search_memory(query)`: cauta in cheie, summary si body (fara diacritice, fara litere mari), max 10 rezultate
  (cheie + summary), cele blocate si cele recente primele.
- `forget(key, reason)`: sterge (deleted_at), cu motivul in istoric. Pe o intrare blocata: aceeasi eroare ca la remember.

Fiecare scriere emite evenimentul SSE `memory` pe proiect si, daca are task, un task_event `memory` ("<agent> saved
memory <key>: <summary>") vizibil in Conversation.

## Indexul din tura (internal/team/run.go)

Langa liniile Library (aceeasi regula: in tura, nu in system prompt), in toate run-urile unui agent al proiectului
(task, chat, notificari, comentarii, brainstorming; fara draft si wizard):

```
Project memory (decisions and facts saved by the team; read the full text with mcp__team__recall):
- api.auth-tokens: tokens live in Redis, never in the DB (locked, You)
- ui.dates: show dates as DD.MM.YYYY everywhere (Backend Bob, T-212)
- ... 12 more: search with mcp__team__search_memory
```

- Ordinea: blocate, apoi dupa updated_at descrescator.
- Limita: max 40 de randuri si max 3000 de caractere; restul ca "N more".
- Fara bloc cand memoria e goala. In brainstorming indexul intra, dar fara tool-uri (run-ul are doar brainstorm_reply);
  acolo linia de titlu nu pomeneste recall.

## Prompt (internal/prompt)

O regula noua `howMemory` in "How you work", pentru toti agentii (inclusiv radacinile):

> Project memory holds decisions and facts that every agent of this project must follow; it is listed in your turn.
> Follow it. Save to it with mcp__team__remember only what outlives this task: a general rule behind a send-back or a
> rejection, a decision the user made (in chat, when unblocking, in Decide), an architecture or convention choice. Use a
> short stable key (area.topic) and update the existing key instead of adding a near duplicate. Do not save task
> progress, file lists or anything already in the project's shared context. If an entry is wrong or outdated, update or
> forget it and say why.

Schimbarea system prompt-ului reseteaza o singura data sesiunile tuturor agentilor la prima rulare dupa update (ca la
orice schimbare de prompt). Acceptat.

## API (internal/web/api_memory.go)

- `GET /api/projects/{id}/memory?deleted=1` — lista (active; cu deleted=1 doar cele sterse).
- `POST /api/projects/{id}/memory` — {key, summary, body, locked} de la utilizator (autor "You").
- `PUT /api/memory/{id}` — {summary, body} (utilizatorul poate edita si intrarile blocate; cheia nu se schimba).
- `PUT /api/memory/{id}/lock` — {locked: bool}.
- `DELETE /api/memory/{id}` — stergere (si a celor blocate); `POST /api/memory/{id}/restore`.
- `GET /api/memory/{id}/history` — versiunile, cele noi primele.
Toate emit `memory` pe proiect.

## UI (static/js/memory.js)

Tab nou "Memory" pe proiect, intre Files si Brainstorm (`#/p/<pid>/memory`, `#/p/<pid>/memory/<id>` deschide intrarea).
- Lista: cautare (ca pe board: fara diacritice), rand = cheie (mono), summary, autor (avatar colorat / "You"), "2h ago",
  T-N clicabil, lacat. "+ Add" deschide formularul.
- Intrarea (panou sau modala, ca la task): summary, text (markdown), Edit, Lock / Unlock, Delete (toast cu Undo),
  History (versiunile cu autor, data, task, motivul la forget).
- Filtrul "Deleted (N)" cu Restore.
- Gol: un rand care explica la ce foloseste si ca agentii o completeaza singuri.
- Reactualizare la evenimentul SSE `memory`.

## Ce ramane la builder

- Structura exacta a codului (store/memory.go, team/memory.go, web/api_memory.go) si numele functiilor.
- Panou lateral sau modala pentru intrare; stilul listei, in tema existenta.
- Normalizarea pentru cautare (se poate refolosi ce face cautarea de pe board).

## Teste

- store: unicitatea cheii, reinvierea unei chei sterse, versiunile, limita de 200.
- team: remember / recall / search / forget, blocarea, no-op la scriere identica, task-ul implicit al run-ului,
  task_event-ul.
- prompt: indexul (ordinea, limita de 40 randuri / 3000 caractere, "N more", gol = fara bloc, varianta de brainstorming).
- web: API-ul (inclusiv utilizatorul pe intrari blocate, restore).
- Un run real cu claude: un agent salveaza o regula dupa un send_back, iar un agent nou o vede in tura si o respecta.
