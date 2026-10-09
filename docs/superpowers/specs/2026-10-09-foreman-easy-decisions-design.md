# Foreman: decizii usor de dat pe un task (intrebari cu optiuni, pasii tai, Accept / Send back rapid)

Data: 2026-10-09
Status: implementat (2026-10-09).

## Scop

Cand un task asteapta decizia utilizatorului (Review, decide utilizatorul), raportul agentului amesteca azi trei lucruri
in text liber: decizia Accept / Send back, intrebari ascunse ("daca vrei X, e un task separat") si pasi pe care doar
utilizatorul ii poate face (variabile in .env, restart de container, un seeder). Utilizatorul raspunde scriind de mana.
Exemplul care a pornit cererea: Luxury Motors - Dashboard, T-110.

Decizii luate cu utilizatorul:
- **Intrebari cu optiuni**: agentul le trimite structurat; in modala sunt butoane (radio / checkbox) plus "Other...".
- **Pasii tai ca checklist**: bifezi pe masura ce ii faci; agentul vede ce ai bifat.
- **Accept / Send back usor**: butoane mari, motive rapide pentru Send back, plus un camp "Anything to add?".

In afara scopului: intrebari structurate in timpul lucrului (ramane `ask_user`), decizii pe task-uri pe care le decide un
lider (aici doar cand decide utilizatorul), sabloane de intrebari salvate.

## 1. Agentul (MCP)

`report_done` primeste doua campuri optionale noi, langa `left_for_user` si `files`:

```
decisions: [ { question: string (1-300), options: [string (1-120)] (2-5), multi: bool (implicit false) } ]  max 5
user_steps: [ string (1-300) ]  max 10
```

- Validare in `internal/mcp/server.go`, ca `validLeftForUser` / `validFiles`; o eroare de validare refuza tot apelul
  (raportul nu ruleaza), cu mesaj clar pentru agent.
- Raspunsul tool-ului adauga `N decisions waiting for the user.` / `N steps left for the user.`.
- `user_steps` se salveaza in `task_todos` cu `kind = 'step'` (vezi Date); `left_for_user` ramane `kind = 'value'`.
  Ambele intra in caseta din modala si in "Needs you" ca azi.
- Prompt (`internal/prompt/prompt.go`, langa `howLeftForUser`): cand ai o intrebare pentru utilizator la final sau pasi pe
  care doar el ii poate face (configurare, restart, un seeder, o verificare manuala), pune-i in `decisions` /
  `user_steps`, nu in textul raportului. Optiunile sunt scurte si se exclud intre ele (sau `multi: true`).

Deciziile se pot trimite la orice `report_done`, dar UI-ul de raspuns apare doar cand task-ul asteapta decizia
utilizatorului (aceeasi conditie ca butoanele Accept / Send back de azi: `UserDecides`). Daca decide un lider, intrebarile
ajung la el in nota de raport (text: `Decisions for the user: ...`), fara UI.

## 2. Date (o migrare noua)

```
task_decisions
  id, task_id (FK tasks ON DELETE CASCADE), position, question, options (JSON []string), multi (0/1),
  asked_by TEXT (numele agentului), asked_at,
  answer (JSON []string, alegerile), other TEXT (raspuns liber), answered_at (NULL = fara raspuns)
INDEX (task_id, position)

task_todos: ADD COLUMN kind TEXT NOT NULL DEFAULT 'value'   -- 'value' (left_for_user) | 'step' (user_steps)
```

Un nou `report_done` pe acelasi task (dupa Send back) inlocuieste intrebarile fara raspuns ale aceluiasi agent; cele
cu raspuns raman in istoric. Numarul migrarii se alege la implementare (ultima de pe main + 1) si se verifica la merge.

## 3. Modala (cand decide utilizatorul)

Ordinea in coloana principala, deasupra raportului:
1. **Decide** (doar daca exista intrebari fara raspuns): fiecare intrebare cu optiunile ca butoane (radio pentru
   `multi: false`, checkbox pentru `multi: true`) si un camp "Other..." (text scurt). Alegerile se tin in pagina pana la
   Accept / Send back (nu se salveaza la fiecare click).
2. **Your steps**: caseta "Left for you" de azi, redenumita "Your steps" cand are si pasi; valorile si pasii in aceeasi
   lista, pasii cu eticheta "step". Bifarea functioneaza ca azi (se salveaza imediat).
3. **Zona de actiune** (inlocuieste textarea de feedback + butoanele de azi cand decide utilizatorul):
   - camp "Anything to add?" (optional);
   - motive rapide pentru Send back, ca butoane mici: "Missing tests", "Doesn't match the mockup", "Needs more work",
     "Wrong approach" (click = adauga textul in camp);
   - butoanele mari **Accept** si **Send back**. Send back cere text in camp sau cel putin un raspuns la o intrebare
     (altfel: "Write what needs to change").

Pe telefon totul pe o coloana; butoanele pe toata latimea.

## 4. Ce se intampla la trimitere

Endpoint-ul existent `POST /api/tasks/{id}/decide` primeste in plus:

```
{ action: "accept" | "send_back", feedback: string, answers: [ { id, choices: [string], other: string } ] }
```

- Raspunsurile se salveaza (`answer`, `other`, `answered_at`) in aceeasi tranzactie logica cu decizia, sub `t.work`.
  Optiunile alese trebuie sa fie din `options` (altfel 400); `multi: false` accepta cel mult una.
- Textul raspunsurilor (`answersText`): `Q1 <intrebare>: <alegeri>[; other: <text>]`, cate o linie; intrebarile lasate
  goale apar ca `no answer`.
- **Accept**: task-ul trece in Done ca azi (`UserDecide` / `decideAsUser`). Agentul care a pus intrebarile primeste o
  nota in coada (`delivered=false`) doar daca exista raspunsuri sau nota utilizatorului:
  `The user accepted T-x.` + raspunsurile + `Note: <feedback>`. Asa poate crea singur follow-up-ul ales.
- **Send back**: feedback-ul trimis agentului = nota utilizatorului + raspunsurile (acelasi text); restul ca azi.
- Istoric: eveniment `decided` (actor You) cu corpul = raspunsuri + nota, deci apare in Conversation.

## 5. Teste

- mcp: validarea `decisions` / `user_steps` (limite, opțiuni 2-5, gol) si apelul catre backend.
- store: salvare / listare / raspuns; inlocuirea intrebarilor fara raspuns la un raport nou; `kind` pe todos.
- team: Accept cu raspunsuri -> Done + nota cu raspunsurile la agentul care a intrebat; Accept fara raspunsuri si fara
  nota -> fara nota; Send back -> feedback-ul contine raspunsurile; alegere invalida -> eroare; lider decide -> intrebarile
  in nota de raport.
- web: `decide` cu `answers`; GET task-ul intoarce `decisions` si `todos` cu `kind`.
- UI: screenshot-uri (instanta goala, claude fals) la 1440 si 390: Decide cu radio + checkbox + Other, Your steps,
  motivele rapide, Accept / Send back.

## Etape

1. Migrare + store (task_decisions, kind pe todos).
2. MCP (`decisions`, `user_steps`) + prompt.
3. team (decide cu raspunsuri, note, evenimente) + web.
4. UI (Decide, Your steps, zona de actiune).
5. CLAUDE.md.
