# Foreman: mutarea task-urilor intre coloane (drag & drop) si stergerea lor (arhivare)

Data: 2026-10-09
Status: implementat (2026-10-09).

## Scop

Pe board, utilizatorul vrea sa:
1. traga un card pe alta coloana, iar mutarea sa insemne actiunea reala a coloanei (nu doar un status schimbat);
2. stearga un task, cu posibilitatea de a-l readuce.

Decizii luate cu utilizatorul:
- Drag & drop = **actiuni reale**: Done = Accept, Queued = inapoi in coada, etapa proprie = mutare in etapa. Mutarile
  care nu au sens nu sunt permise, iar UI-ul spune de ce.
- Stergere = **arhivare** cu Restore (nimic nu dispare din baza de date sau de pe disc).

In afara scopului: stergerea definitiva (purge), reordonarea in coloana (prioritate), drag & drop in alte liste decat
board-ul, revert pe codul scris de agenti.

## 1. Mutari (drag & drop)

### Regula de baza

Serverul e autoritatea: `POST /api/tasks/{id}/move` cu `{ "to": <tinta>, "reason": "..." }`. Tinta e una din:
`"queued"`, `"done"` sau `"stage:<id>"`. Raspunsul e 204 la succes, 409 cu mesajul refuzului altfel (ex.
`An agent is working on T-12; wait for it or stop the team.`).

UI-ul calculeaza aceleasi reguli local (functia `moveAction(task, tinta)` in `board-move.js`) ca sa coloreze
coloanele cand tragi: permise (evidentiate), nepermise (estompate, cu motivul in `title` si intr-un mesaj scurt la drop).

### Board fara etape

| Starea task-ului | Tras pe Queued | Tras pe Done |
|---|---|---|
| Queued | – (deja acolo) | **Close**: Done fara lucru, raport "Closed by the user" (+ motivul, optional) |
| In progress | refuzat (un agent lucreaza) | refuzat |
| Review, decide utilizatorul | **Send back** (motiv obligatoriu) = `UserDecide(false, motiv)` | **Accept** = `UserDecide(true)` |
| Review, decide un lider/reviewer | **Send back** in numele utilizatorului (motiv obligatoriu) | **Accept** in numele utilizatorului |
| Blocked | **Unblock** = `Unblock` existent (motivul, daca e dat, ajunge ca nota) | **Accept** = `AcceptBlocked` existent |
| Done | **Reopen** (motiv obligatoriu): Queued, la acelasi asignat, motivul ca feedback | – |

- Coloanele In progress si Review nu primesc drop (acolo ajung task-urile doar prin munca agentilor).
- Un sub-task se muta la fel. Reopen pe un sub-task al carui parinte e Done nu redeschide parintele; nota ajunge la
  liderul care a creat sub-task-ul.
- "In numele utilizatorului" (Review decis de lider): utilizatorul poate trece peste lider. Evenimentul e
  `You accepted T-12 (instead of Atlas)`, iar liderul primeste nota ca la deciziile utilizatorului de azi.

### Board cu etape (coloane proprii)

- Un task Queued sau Blocked dintr-o etapa poate fi tras pe **alta etapa**: `MoveToStage(task, stage)` il pune in
  etapa tinta, Queued, fara asignat ales (primul agent liber din etapa il ia, ca la `create_task` in etape),
  `SendBacks`/`Escalated` resetate. Un task In progress sau Review nu se muta intre etape.
- "No stage" si "With lead" nu primesc drop. Pe "Done" se aplica regulile din tabel (Accept / Close).
- Coloana Queued nu exista pe board-ul cu etape; "inapoi in coada" = tras pe etapa lui (sau pe prima etapa).

### Ce se intampla dupa o mutare

- Istoricul task-ului: eveniment cu actorul `You` (`moved`, `reopened`, `closed` sau evenimentele existente ale
  actiunilor refolosite: `accepted`, `sent_back`, `unblocked`).
- Agentul afectat (asignatul) si decidentul (liderul / reviewerul) primesc o nota (`t.note`), ca la `AssignTask`.
- `t.emit(..., "task")` si `t.Kick(projectID)`, ca agentii sa reia munca.

## 2. Stergere (arhivare)

### Date

Migrarea 026 (025 e luata de conturile Claude, intrate in paralel):

```
ALTER TABLE tasks ADD COLUMN archived_from TEXT NOT NULL DEFAULT '';  -- statusul dinainte ('' = nearhivat)
ALTER TABLE tasks ADD COLUMN archived_at   TEXT;                      -- NULL = nearhivat
ALTER TABLE tasks ADD COLUMN archived_root INTEGER;                   -- task-ul pe care l-a sters utilizatorul (grupul)
```

`store.StatusArchived = "archived"`. Un task arhivat are `status = 'archived'`.

### Delete

`DELETE /api/tasks/{id}` (204) -> `team.ArchiveTask(id)`:

1. Grupul = task-ul + toti descendentii (`parent_task_id`, recursiv) care nu sunt deja arhivati.
2. Daca un agent lucreaza pe un task din grup (run activ pe task), run-ul e oprit (`t.runs.Stop(runID)`) si
   asignatul primeste nota `The user deleted T-12; stop working on it.`.
3. Fiecare task din grup: `archived_from = status`, `status = 'archived'`, `archived_at = acum`,
   `archived_root = id`. Eveniment `deleted` (actor You) pe task-ul sters.
4. Decidentul primeste nota `The user deleted T-12: <titlu>.` (nu mai asteapta raportul).
5. `t.emit` + `t.Kick`.

Finalul run-ului oprit (`finish`) nu are voie sa scoata task-ul din `archived` (nu il pune inapoi in Queued si nu
inregistreaza raport ca Review): un task arhivat e sarit in `finish`, in reconciliere, in detectorul de task-uri
blocate si in `report_done` / `accept_task` / `send_back` / `ask_user` (eroare pentru agent:
`T-12 was deleted by the user.`).

### Unde nu mai apare un task arhivat

- Board: coloanele merg dupa status, deci un task `archived` nu are coloana (filtrat explicit si in JS).
- Scheduler, reconciliere, Recover la pornire, detectorul de task-uri blocate: lucreaza pe statusuri active.
- "Needs you": intrebarile (`questions`), cererile de acces (`access_requests`), cererile de agenti si "Left for you"
  (`task_todos`) legate de un task arhivat nu se afiseaza si nu intra in "N need you".
- Sarcini recurente: aparitia precedenta arhivata conteaza ca inchisa (nu blocheaza urmatoarea).
- Stats: task-urile arhivate nu intra in "created / done / median"; costul run-urilor lor ramane in totaluri
  (banii au fost cheltuiti).
- `list_tasks` (MCP): nu intorc task-uri arhivate.
- Totalul proiectului ("N of M done", lista de proiecte): fara arhivate.

Implementarea face un audit al tuturor interogarilor pe `tasks` (`grep -n "FROM tasks\|JOIN tasks"`) si al apelurilor
`ListTasks`; fiecare loc e trecut in plan cu decizia lui (filtreaza / nu conteaza).

### Restore

`POST /api/tasks/{id}/restore` (204) -> `team.RestoreTask(id)`: tot grupul cu `archived_root = id` revine la
`archived_from`; `in_progress` devine `queued` (run-ul a fost oprit). Eveniment `restored` (You). `t.Kick`.
Un grup a carui radacina are parintele arhivat (sub-task sters separat, apoi parintele sters) nu se restaureaza singur:
409 `Restore T-3 first.` (altfel ar aparea un sub-task activ sub un parinte sters).

`GET /api/projects/{id}/archived` -> task-urile cu `archived_root = id` (radacinile grupurilor sterse), cele mai
recente primele, cu numarul de sub-task-uri din grup.

## 3. UI

### Drag & drop (desktop)

- Cardurile din board sunt `draggable` (HTML5 drag & drop, fara biblioteca). In timpul tragerii, coloanele primesc
  clasa `drop-ok` sau `drop-no` (cu motivul din `moveAction` in `title`).
- La drop pe o tinta permisa:
  - Accept / Unblock / Close / mutare de etapa: imediat, fara Undo (actiunile raman in istoric; un Done gresit se
    corecteaza cu Reopen).
  - Send back / Reopen: o fereastra mica cere motivul (obligatoriu), cu Cancel.
- La drop pe o tinta nepermisa: toast cu motivul.
- Accesibilitate si telefon: meniul cardului (buton `⋯`) are "Move to…" cu tintele permise si "Delete". Pe ecrane
  inguste (fara drag) se foloseste doar meniul.

### Delete si Restore

- Modala de task: buton "Delete" (zona de actiuni, stil discret, rosu la hover). Confirmarea:
  `Delete T-12 and its 3 subtasks? You can restore them from Deleted.` (fara sub-task-uri: fara partea a doua;
  cu agent la lucru: `Atlas is working on it; the run will be stopped.`).
- Dupa Delete: modala se inchide, toast `Deleted T-12 · Undo` (Undo = Restore).
- Board: in bara de unelte, "Deleted (N)" (doar cand N > 0) deschide un panou (ca Recurring / Columns) cu lista:
  `T-12 · titlu · 3 subtasks · deleted 5 min ago · Restore`.

## 4. Teste

- team: fiecare celula din tabelul de mutari (permis -> efectul; refuzat -> eroarea), mutare intre etape, mutare
  refuzata pentru In progress; arhivarea unui task cu run activ (run oprit, `finish` nu il readuce) si cu
  sub-task-uri; tool-urile MCP pe un task arhivat; Restore (inclusiv in_progress -> queued).
- store: migrarea; interogarile din audit nu intorc arhivate; grupul la arhivare/restaurare.
- web: `move` (204 / 409 / 400 fara motiv), `DELETE`, `restore`, `archived`; "Needs you" fara elementele
  task-urilor arhivate.
- UI: `node --check`; screenshot-uri (Chrome headless) la 1440 si 390: drag simulat (evenimente HTML5 prin CDP),
  coloanele evidentiate, fereastra de motiv, meniul "Move to…", panoul "Deleted".

## Etape

1. Migrare 026 + store (status archived, grup, restaurare, listare) + auditul interogarilor.
2. team: `ArchiveTask`, `RestoreTask`, garzile din finish / reconciliere / tool-uri MCP.
3. team: `MoveTask` (tabelul de mutari, `MoveToStage`, Reopen, Close).
4. web: endpoint-uri + filtrele din "Needs you" si din vederea proiectului.
5. UI: drag & drop, meniul cardului, Delete / Undo / panoul Deleted.
6. CLAUDE.md.
