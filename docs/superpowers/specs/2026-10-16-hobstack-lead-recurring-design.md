# HobStack: liderul gestioneaza sarcini recurente

Data: 2026-10-16
Status: design aprobat in chat.

## Scop

Utilizatorul ii poate cere in chat unui lider (sau unei radacini) sa creeze, sa schimbe, sa puna pe pauza sau sa stearga o
sarcina recurenta ("run the tests every weekday at 09:00", "move the morning check to 10", "stop the daily report").
Liderul o face prin tool-uri MCP, in locul utilizatorului. Sarcina rezultata e una obisnuita (spec-ul
docs/superpowers/specs/2026-10-03-foreman-recurring-tasks-design.md): acelasi program, acelasi scheduler, aparitiile
merg pe drumul task-urilor create de utilizator (decide utilizatorul, sau auto-accept).

Regula de baza: **liderul schimba sarcini recurente doar cand i se cere**. Aplicatia o impune, nu prompt-ul.

In afara scopului: liderul nu propune sarcini recurente prin "Needs you" (fara aprobare; o idee se propune in chat),
nu ruleaza o sarcina acum ("Run now" ramane in UI), aparitiile nu raporteaza liderului, fara limita separata de numar.

## Cine primeste tool-urile

Agentii cu `mcp.Caller.CanAddAgents` (lideri si radacini, calculat in `team/run.go` ca `isLead || roles.IsRoot`), in
`toolsFor` (`internal/mcp/tools.go`), lista `recurringTools`:

- `list_recurring`, `create_recurring`, `update_recurring`, `delete_recurring`.

Tool-urile sunt date mereu acestor agenti (lista se fixeaza la pornirea run-ului); regula "doar la cerere" se verifica la
apel. La apel, rolul se verifica din nou din baza de date (`canAddAgents(caller, agents)` din `team/agents.go`), ca la
`add_agent`: un agent care nu mai e lider / radacina primeste eroare.

## Regula "doar la cerere"

`list_recurring` merge oricand (doar citeste).

`create_recurring`, `update_recurring`, `delete_recurring` merg doar daca run-ul apelantului a pornit dintr-un mesaj al
utilizatorului in chat: `t.requestOf(c.AgentID, c.RunID)` gaseste mesajul (aceeasi verificare ca `add_agent`). Full
autonomy **nu** ocoleste regula. Altfel, eroare pentru agent:

> Only when the user asks for it in chat. Suggest it to the user in your reply instead.

Asta exclude run-urile pe task, notificarile, comentariile, intrebarile de la proiecte legate si run-urile pornite de
scheduler.

## Ce poate atinge liderul

**Asignatul** unei sarcini create sau schimbate de lider ("zona" liderului):

- liderul insusi;
- un agent din echipa lui directa (`ParentID` = liderul);
- pentru o radacina, intr-un proiect cu etape: si "prima etapa" (`assignee` = `"first stage"`, adica `AssigneeID` 0).

Pe langa asta se aplica regulile de acum din `team.checkRecurring` / `checkAssignee` (agent din proiect, legat, nu
reviewer-only etc.), fara copie a lor.

**Ce sarcini poate schimba / sterge**: doar cele al caror asignat curent e in zona liderului (pentru radacina si cele fara
asignat). Altfel: `R-<id> belongs to another team; ask the user to change it in the Recurring panel.` Nu conteaza cine a
creat sarcina (si cele create de utilizator se pot schimba, daca utilizatorul o cere).

## Tool-uri

Sarcinile se numesc `R-<id>` in textele pentru agent (id-ul din `recurring_tasks`); argumentele accepta `"R-3"` sau `"3"`.

### `list_recurring`

Fara argumente. Text, cate o linie per sarcina a proiectului, in ordinea din `ListRecurring`:

```
R-3 "Run the tests" — Weekdays at 09:00 — Ana — on — next Mon 09:00 — auto-accept — created by Lead
R-5 "Weekly report" — Mon at 10:00 — first stage — paused — Skipped at 10:00: T-12 still open
```

Campuri: id, titlu, `schedule.Spec.String()`, asignatul (nume / "first stage" / "nobody"), on / paused, urmatoarea rulare
(doar cand e pornita; ora locala, ca in UI), "auto-accept" daca e bifat, `last_note` daca nu e gol, "created by <agent>"
daca `created_by` e setat. Fara sarcini: `No recurring tasks in this project.`

### `create_recurring`

| camp          | obligatoriu | reguli                                                                          |
|---------------|-------------|---------------------------------------------------------------------------------|
| `title`       | da          | trim, 1..200 caractere (ca la UI)                                               |
| `description` | nu          | trim, max 10000 caractere (ca la UI)                                            |
| `assignee`    | nu          | numele agentului (NOCASE) sau `"first stage"`; implicit liderul insusi           |
| `schedule`    | da          | obiect: `kind` (`every_hours` / `daily` / `weekly`), `every_hours`, `at` "HH:MM", `weekdays` |
| `auto_accept` | nu          | implicit false                                                                  |

`weekdays`: lista de zile `["mon","tue",...]` (case-insensitive, si numele complete "monday"), convertita in masca
(`1<<time.Weekday`); dubluri ignorate; zi necunoscuta = eroare. Validarea programului: `schedule.Spec.Validate`.

Sarcina se creeaza pornita (`Enabled` true) prin `team.CreateRecurring`, cu `CreatedBy` = liderul. Raspuns:
`Created R-7 "Run the tests": Weekdays at 09:00, next run Mon 09:00, done by Ana. Each run is a task on the board that the
user accepts (or auto-accepted).`

Trim-ul si limitele de lungime (titlu obligatoriu, 200 / 10000) se muta din `web/api_recurring.go` (`recurringInput`) in
`team.checkRecurring`, ca sa nu existe doua copii; erorile raman `RecurringError` (400 in API, text clar pentru agent).

### `update_recurring`

`id` obligatoriu; restul optionale, doar ce se schimba: `title`, `description`, `assignee`, `schedule` (obiectul intreg,
inlocuieste programul), `auto_accept`, `enabled` (false = pauza, true = reluare). Se pleaca de la sarcina curenta, se
aplica campurile date si se cheama `team.UpdateRecurring` (care recalculeaza urmatoarea rulare cand programul se schimba
sau la reluare, ca acum). Fara niciun camp de schimbat: eroare `nothing to change`. Raspuns: starea noua, ca o linie
din `list_recurring`, precedata de `Updated`.

Zona se verifica si pentru asignatul vechi (ce poate schimba) si pentru cel nou (unde o poate muta).

### `delete_recurring`

`id` obligatoriu. `team.DeleteRecurring`; task-urile deja create raman (ca acum, `recurring_id` devine NULL). Raspuns:
`Deleted R-7 "Run the tests". Tasks it already created stay on the board.`

## Store

Migrarea 032:

```sql
-- agentul care a creat sarcina la cererea utilizatorului (NULL = utilizatorul)
ALTER TABLE recurring_tasks ADD COLUMN created_by INTEGER REFERENCES agents(id) ON DELETE SET NULL;
```

`store.RecurringTask.CreatedBy *int64` (`json:"createdBy"`), in `recurringCols` / `scanRecurring` / `CreateRecurring`.
`SaveRecurring` nu il schimba. `team.RecurringInput` primeste `CreatedBy *int64`, folosit doar de `CreateRecurring`.

## MCP

- `mcp.Backend` primeste `ListRecurring(c)`, `CreateRecurring(c, in RecurringInput)`, `UpdateRecurring(c, in
  RecurringUpdate)`, `DeleteRecurring(c, id string)`; tipurile de intrare sunt in pachetul `mcp` (ca `AddAgentInput`),
  cu campurile optionale ca pointeri in `RecurringUpdate`. Wrapper-ele din `draft` / `setup` incorporeaza `mcp.Backend`,
  deci nu se schimba.
- `server.go`: cazurile noi in `callTool`, decodarea argumentelor.
- Implementarea in `internal/team/recurring_tools.go` (fisier nou), langa `recurring.go`.

## UI

Panoul Recurring (randul sarcinii): "Created by <agent>" langa program, cand sarcina are un creator. `recurringView` din
`web/api_recurring.go` primeste `createdByName` (numele din `GetAgent`; agent sters = `created_by` NULL = nimic). Schimbarile
facute de lider ajung live prin evenimentul existent `recurring` (`t.emit`).

## Teste

- `team`: create / update / delete refuzate fara mesaj de la utilizator (si cu Full autonomy), acceptate cu mesaj;
  zona (sine, echipa directa, alt agent refuzat, "first stage" doar pentru radacina cu etape); update / delete refuzate
  pe sarcinile altei echipe; `weekdays` din nume; update partial pastreaza restul; `enabled` false / true; `CreatedBy`
  salvat; un agent care nu mai e lider primeste eroare.
- `mcp`: tool-urile apar pentru lider si radacina, nu pentru worker / reviewer / analist; decodarea argumentelor.
- `store`: migrarea, `CreatedBy` citit si scris.
- `web`: `createdByName` in `recurring` din `GET /api/projects/{id}`; limitele mutate in `team` raspund tot 400;
  eticheta verificata cu Chrome headless.
