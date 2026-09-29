# Foreman: echipe ca graf (ierarhie + review) si editor canvas

Data: 2026-09-29
Status: design aprobat in chat.

## Problema

- Azi fiecare proiect are exact un lead si mai multi workeri.
- Lead-ul imparte munca ghicind din titlul rolurilor.
- Nu exista reguli despre cine ia ce, cine verifica pe cine, sau cum lucreaza mai multi lideri.

## Decizii (din brainstorming)

- **Structura:** o ierarhie (padure de arbori) plus legaturi de review.
- **Mai multi lideri:** se pot combina un coordonator cu sub-lideri si lideri independenti. Structura o alege utilizatorul.
- **Review:** munca trece intai pe la reviewer, apoi decide liderul care a creat task-ul.
- **Editor:** un canvas cu noduri mutabile si legaturi trase cu mouse-ul, facut in SVG si JS vanilla, fara biblioteci.

## Modelul de date (migrarea 003)

`agents` primeste coloane noi:
- `parent_id INTEGER NULL REFERENCES agents(id)`: seful. NULL inseamna ca agentul raporteaza **utilizatorului** si apare pe canvas legat de nodul „Tu”;
- `reviewer_id INTEGER NULL REFERENCES agents(id)`: cine ii verifica munca;
- `responsibilities TEXT NOT NULL DEFAULT ''`: „Ce ia”, adica tipurile de task-uri pe care le primeste;
- `pos_x REAL`, `pos_y REAL`: pozitia pe canvas (NULL inseamna asezare automata).

Alte schimbari in migrare:
- indexul `agents_one_lead` se sterge;
- coloana `position` ramane in tabel, dar nu mai e folosita; rolul reiese din legaturi;
- datele existente se convertesc: workerii fiecarui proiect primesc `parent_id` = id-ul fostului lead. Lead-ul ramane cu `parent_id` NULL.

`tasks` primeste coloane noi:
- `creator_id INTEGER NULL REFERENCES agents(id)`: liderul care a creat task-ul si care il accepta sau il trimite inapoi. NULL la task-urile vechi; migrarea pune lead-ul proiectului;
- `parent_task_id INTEGER NULL REFERENCES tasks(id)`: task-ul din care a fost spart;
- `review_stage TEXT NOT NULL DEFAULT ''`, cu valorile `''`, `'reviewer'` si `'lead'`. Arata cine are task-ul cand statusul e `review`.

**Roluri derivate** (calculate, nu stocate):
- **lider:** are cel putin un agent cu `parent_id` = el;
- **radacina:** are `parent_id` NULL. Primeste obiective direct de la utilizator;
- **reviewer:** apare ca `reviewer_id` la cel putin un agent.

**Invarianti** (verificati de server la editare):
- `parent_id` si `reviewer_id` apartin aceluiasi proiect;
- un agent nu poate fi propriul sef sau propriul reviewer;
- lantul de sefi nu are voie sa formeze un ciclu. O incercare intoarce 400 `legatura ar crea un ciclu`.

**Stergerea unui agent:**
- copiii lui trec la seful lui (sau devin radacini);
- `reviewer_id` care il indica devine NULL;
- task-urile raman refuzate ca acum (409 daca are task-uri).

**Proiect nou:** se creeaza ca acum un agent „Lead” radacina. Un agent nou adaugat din UI primeste implicit `parent_id` = prima radacina.

## Fluxul de lucru

**Crearea task-urilor:**
- `create_task` e permis unui lider doar pentru agentii **direct sub el**;
- `creator_id` e liderul care il creeaza;
- daca liderul e in mijlocul unui run pe un task T (primit de la seful lui), noul task are `parent_task_id` = T.

**Un task al unui sub-lider:**
- sub-liderul ruleaza pe task, il sparge in sub-task-uri si se opreste fara `report_done`;
- task-ul lui ramane `in_progress` (nu trece automat in review) cat timp exista sub-task-uri deschise, adica neajunse in `done`;
- cand toate sub-task-urile sunt `done`, sub-liderul primeste notificarea „toate sub-task-urile pentru T-x sunt gata” si raporteaza cu `report_done(task: "T-x", summary)`.

**Raportarea:**
- `report_done(summary, task?)`:
  - intr-un run pe task, parametrul `task` e implicit task-ul curent;
  - altfel `task` e obligatoriu si trebuie sa fie un task `in_progress` asignat apelantului;
- daca asignatul are reviewer: `status=review`, `review_stage=reviewer`, iar reviewer-ul primeste o notificare nelivrata;
- altfel: `review_stage=lead`, iar creatorul primeste notificarea.

**Review-ul:**
- `review_approve(task, note?)`, doar pentru reviewer-ul asignatului, doar cand `review_stage=reviewer`: task-ul trece la `review_stage=lead`, iar creatorul e notificat;
- `review_reject(task, feedback)`: task-ul revine in `queued` cu feedback si `send_backs++`. La 3 respingeri trece in `blocked`, cu aceeasi regula ca `send_back`.

**Decizia liderului:**
- `accept_task` si `send_back` sunt permise doar creatorului (sau oricarui stramos al lui) si doar cand `review_stage=lead` (sau `blocked`, la accept);
- la `accept` al unui sub-task: daca toate sub-task-urile task-ului parinte sunt `done`, asignatul task-ului parinte primeste notificarea de mai sus.

**Scheduler-ul (generalizat):**
- orice agent inactiv cu un task `queued` asignat primeste un run pe task, in ordinea numarului;
- altfel, daca are mesaje nelivrate (notificari, mesaje de la utilizator), primeste un run „de notificari”;
- limita `max_parallel` se aplica tuturor agentilor care nu sunt radacini.

**Stop, resume si recuperarea la restart** raman ca acum.

## Prompturi

System prompt-ul (partea stabila) are:
- linia de rol: „Raportezi lui X” sau „Raportezi utilizatorului”;
- blocuri „Cum lucrezi” compuse dupa rolurile agentului:
  - **lider:** imparte munca doar echipei tale directe, dupa campul „Ce ia” al fiecaruia; verifica rezultatele; cand ai primit tu un task, sparge-l si raporteaza-l in sus cu `report_done` dupa ce toate sub-task-urile sunt gata;
  - **worker:** blocul de acum;
  - **reviewer:** verifica munca oamenilor pe care ii revizuiesti si foloseste `review_approve` sau `review_reject` cu feedback concret.

Mesajul turei (partea dinamica) are:
- **echipa directa:** nume, rol, „Ce ia”, status. Pentru un lider, doar agentii direct sub el;
- **„Revizuiesti munca lui: …”** pentru reviewer;
- **„Task-urile tale deschise”** pentru un lider cu task-uri primite in lucru;
- blocul de fisiere si notificarile, ca acum.

O schimbare de legaturi schimba rolurile, deci hash-ul, deci porneste o sesiune noua. E acceptabil.

## MCP

`Caller` are campurile `IsLead`, `HasParent` si `IsReviewer`, calculate la pornirea run-ului. Lista de tool-uri e reuniunea tool-urilor pentru rolurile agentului. Backend-ul revalideaza din DB la fiecare apel.

| Tool | Cine il primeste |
|---|---|
| `create_task`, `list_tasks`, `accept_task`, `send_back` | liderii |
| `report_done` | agentii cu sef |
| `review_approve`, `review_reject` | reviewerii |

Un agent radacina fara copii nu are niciun tool de echipa si doar vorbeste cu utilizatorul.

## API

- **`GET /api/projects/{id}`:**
  - fiecare agent contine `parentId`, `reviewerId`, `responsibilities`, `posX`, `posY` si rolurile derivate `isLead`, `isRoot`, `isReviewer`;
  - fiecare task contine `creatorId`, `parentTaskId` si `reviewStage`;
  - `position` dispare din raspuns.
- **`POST /api/projects/{id}/agents` si `PUT /api/agents/{id}`** accepta `responsibilities`, `parentId` (null inseamna radacina) si `reviewerId`, cu validarea invariantilor.
- **`PUT /api/agents/{id}/position`** cu `{x, y}` e un endpoint separat, ieftin, apelat la mutarea unui nod.
- **`prompt-preview`** foloseste legaturile agentului, sau pe cele trimise in body la un agent nou.

## UI

**Canvas-ul din sectiunea „Echipa”** a workspace-ului (inaltime ~420px, buton „Ecran mare” pentru overlay pe tot ecranul):
- nodul **„Tu”** sus, fix; agentii sunt noduri-card cu avatar, nume, rol, „Ce ia” scurtat si punctul de status live;
- **mutare:** drag pe nod, iar pozitia se salveaza la `mouseup`. Nodurile fara pozitie se aseaza automat pe niveluri, dupa adancimea in arbore;
- **legaturi:** ● jos pe fiecare nod (si pe „Tu”). Tragi pana pe alt nod, apoi alegi dintr-un meniu mic „Delegheaza catre” sau „Verifica munca lui”:
  - „Delegheaza catre” face nodul tinta copilul sursei. De pe „Tu”, tinta devine radacina;
  - „Verifica munca lui” face sursa reviewer-ul tintei;
- **desenare:** delegarea e o linie plina cu sageata, review-ul o linie punctata amber;
- **editare:** click pe o linie o selecteaza si „Sterge legatura” (sau tasta Delete) o sterge. Stergerea unei delegari face tinta radacina;
- click pe nod selecteaza agentul in consola, ca acum; dublu-click deschide editorul agentului.

**Editorul agentului:**
- campurile noi „Ce ia (responsabilitati)”, „Raporteaza lui” (select: Tu + agentii proiectului) si „Verificat de” (select: nimeni + agentii);
- textul fix „Pozitie in echipa” dispare.

**Board-ul:** in coloana Review, cardul arata „la <reviewer>” sau „la <lider>”.

**Consola:** agentul selectat implicit e prima radacina.

## Testare

- **store:** migrarea (inclusiv conversia unui proiect vechi), invariantii (ciclu, alt proiect), re-parentarea la stergere.
- **team:** fluxul complet cu coordonator, sub-lider, worker si reviewer:
  - utilizator → coordonator → task pentru sub-lider → sub-task pentru worker;
  - report → reviewer → approve → sub-lider accepta → notificare „toate gata” → sub-liderul raporteaza in sus → coordonatorul accepta;
  - plus respingerea la review, limita de 3 si `create_task` refuzat pentru un agent care nu e direct sub lider.
- **mcp:** listele de tool-uri dupa roluri.
- **web:** endpoint-urile noi si validarile.
- **UI:** `node --check`, plus screenshot-uri si masuratori prin Chrome headless (CDP) la 1440×900 si 1280×720: canvas vizibil, legaturi desenate, chat-ul tot vizibil.
- **e2e cu Claude real (haiku):** coordonator, doi sub-lideri, fiecare cu cate un worker, plus un reviewer; se urmareste un obiectiv mic pana la Done.

## In afara acestei felii

- modurile pipeline sau predari laterale intre workeri;
- template-uri de echipa;
- biblioteca de agenti refolositi intre proiecte;
- undo pe canvas.
