# Foreman: Flow, drumul unui task de la cap la coada

Data: 2026-10-03
Status: design aprobat in chat.

## Scop

Pentru orice task, utilizatorul vede tot drumul lui: cine a cerut, cine a creat task-ul si cui l-a dat, cum l-a spart un
sub-lider in sub-task-uri, cine a verificat si cine a acceptat, cu orele. Vederea e un arbore cu cronologie: fiecare
task e un nod cu pasii lui in ordine, iar sub-task-urile apar indentate acolo unde au fost create. Din orice nod al
arborelui (si dintr-un sub-task) se vede tot arborele, de la radacina.

In afara scopului: o pagina de ansamblu cu toate fluxurile proiectului, diagrama pe culoare (sequence diagram), textul
spus de agenti in run-uri (ramane in tab-ul Conversation), completarea retroactiva a pasului "requested" pentru
task-urile vechi.

## Date

Pasii sunt evenimentele din `task_events` (aceleasi ca in History, cu `to_actor` si `body`). Tipurile existente:
`created, assigned, started, stage, waiting_subtasks, requeued, reported, approved, rejected, accepted, user_accepted,
sent_back, blocked, unblocked, failed, asked, answered, denied, nudged`. Nu se adauga tabele sau coloane.

### Pasul nou `requested` (de unde a pornit)

Azi, un task creat de un agent dintr-un mesaj al utilizatorului in chat nu pastreaza legatura cu mesajul. Solutia, fara
schimbare de schema:

- `activeRun` (internal/team/team.go) primeste `request string`: textul ultimului mesaj al utilizatorului
  (`Author == store.AuthorUser`) livrat la pornirea run-ului. Se completeaza acolo unde run-ul de notificari/chat ia
  mesajele nelivrate (`Undelivered` + `delivered` in internal/team/reconcile.go). Run-urile pe task nu il au.
- `CreateTask` (tool-ul MCP `create_task`, internal/team/tools.go): daca run-ul apelantului (`c.RunID`) e activ, nu are
  task (`c.TaskID == 0`) si are `request != ""`, dupa evenimentul "created" se adauga un eveniment
  `requested` cu `Actor = "You"`, `To` = numele agentului apelant, `Text = "Asked in chat"`, `Body` = mesajul (taiat ca
  orice body, `maxEventBody`). Ora evenimentului e ora mesajului (`Message.CreatedAt`), deci in cronologie apare
  inaintea lui "created".
- Task-urile create din board au deja "Created by you"; task-urile vechi raman fara `requested`.

## API

`GET /api/tasks/{id}/flow` -> 200:

```json
{
  "focusId": 8,
  "root": {
    "id": 7, "number": 7, "title": "Share button on cards", "status": "done",
    "assignee": "Beacon", "createdAt": "...", "doneAt": "..." ,
    "steps": [ { "at": "...", "kind": "created", "actor": "Foreman", "to": "Beacon", "text": "Created by Foreman", "body": "" } ],
    "children": [ { "...": "acelasi format, recursiv" } ]
  }
}
```

- Radacina: se urca pe `parent_task_id` de la task-ul cerut pana la un task fara parinte; garda contra ciclurilor (un
  task deja vazut opreste urcarea, iar ultimul task valid devine radacina). Un parinte sters opreste urcarea la fel.
- Copiii: `store.Subtasks(parentID)` (dupa numar), recursiv, cu aceeasi garda.
- `steps`: `ListTaskEvents` in ordinea `at` (iar la egalitate dupa id). Un task fara niciun eveniment primeste un pas
  sintetic `{kind: "created", text: "Created", at: createdAt}`, ca in `getTask`.
- `assignee`: numele agentului asignat ("" daca nu mai exista).
- `doneAt`: momentul ultimului pas `accepted` sau `user_accepted`, doar daca task-ul e Done; altfel `null`.
- Task inexistent -> 404 (ca `getTask`).
- Constructia arborelui e o functie testabila separat in internal/web (sau store), fara HTTP.

## UI

- **Tab-ul "Flow"** in modala de task, al patrulea (Conversation / History / Changes / Flow), acelasi tipar de tab-uri
  (`role="tab"`, `aria-selected`, `aria-controls`). Datele se cer doar cat tab-ul e deschis si se recer la fiecare
  `refresh` al modalei (evenimentele SSE ale proiectului), cu acelasi mecanism anti-raspuns-vechi (`seq`) ca la Changes.
- Fisier nou `internal/web/static/js/flow.js`: `flowView({ projectId })` -> `{ el, show(data), error(msg) }`
  (`focusId` vine din raspuns), in stilul lui `changes.js`.
- **Nodul unui task**:
  - antet: `T-7 · titlu`, pastila de stare (aceleasi etichete ca in modala), asignatul, si la final durata
    `Done · 42m` (de la `createdAt` la `doneAt`; sub o ora in minute, apoi `2h 05m`, peste o zi `3d 4h`);
  - antetul altui task e un link `#/p/<proiect>/tasks/<id>` (modala se muta pe el); nodul task-ului
    curent (`focusId`) nu e clicabil si are bordura `--amber`;
  - pasii, in ordine: ora (`HH:MM`; alta zi: `Sep 30 14:02`), `Actor → Destinatar` (sau doar actorul cand `to` e gol),
    textul;
  - un pas cu `body` are butonul `Show` / `Hide`; textul se randeaza cu `renderMarkdown` din `md.js`; starea
    deschis/inchis (cheie: id task + at + kind) ramane intre refresh-uri;
  - sub-task-urile sunt noduri indentate, cu o linie verticala in stanga, intercalate in pasii parintelui dupa ora
    evenimentului lor "created" (ordinea: pas sau copil, crescator dupa ora).
- **Culori** (doar tokenuri existente): `accepted`, `user_accepted`, `approved` cu `--green`; `sent_back`, `rejected`,
  `blocked`, `failed` cu `--red`; `asked` cu `--amber`; `requested` cu textul in `--soft` si eticheta "asked in chat".
- **Fara delegare** (radacina fara copii): pasii, plus hint-ul "No delegation: this task was done by one agent."
- **Latime ingusta**: indentarea scade la 12px pe nivel, textul se imparte pe randuri; pagina nu face scroll orizontal.
- Tot textul venit de la agenti trece prin `el()` / `renderMarkdown` (niciodata HTML).

## Erori si cazuri de margine

- Task sters in arbore: nu apare (arborele se construieste din ce exista).
- Ciclu in `parent_task_id`: garda opreste urcarea / coborarea; nu se intra in bucla.
- Eroare la cerere: mesajul apare in tab (ca la Changes); la refresh se incearca din nou.
- Run-ul s-a terminat inainte de `create_task` (nu se intampla: tool-ul ruleaza in timpul run-ului): fara `requested`.
- Mai multe mesaje ale utilizatorului livrate in acelasi run: se pastreaza ultimul.

## Teste

- Web/store: arbore pe 3 niveluri cerut dintr-un sub-task (radacina corecta, `focusId`, copiii in ordine, pasii cu
  actor/to/text/body, `doneAt` doar pe Done), task vechi fara evenimente (pas sintetic), ciclu in `parent_task_id`
  (fara bucla), 404.
- Team: run de chat al radacinii cu un mesaj al utilizatorului care face `create_task` -> eveniment `requested` cu
  textul mesajului si ora lui; run pe task (sub-lider) care face `create_task` -> fara `requested`; run de chat fara
  mesaj al utilizatorului (doar note) -> fara `requested`.
- UI: screenshot-uri in Chrome headless pe o copie a bazei de date (arborele real T-7/T-8 din Luxury Motors), deschis
  din radacina si din sub-task, un pas desfasurat, latime 390px fara scroll orizontal.
