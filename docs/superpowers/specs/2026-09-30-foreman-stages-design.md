# Foreman: coloane proprii pe board (etape de lucru)

Data: 2026-09-30
Status: design aprobat in chat.

## Scop

Fiecare proiect isi poate defini coloanele board-ului ca **etape de lucru**, de exemplu Analiza → Dev → QA. Fiecare etapa are agentii ei. Un task trece singur dintr-o etapa in urmatoarea cand agentul din etapa curenta termina. La final decide liderul care a creat task-ul.

## Decizii (din brainstorming)

- Coloanele sunt etape de lucru, nu doar o grupare vizuala.
- Liderul (sau utilizatorul) creeaza task-urile, iar etapele le duc mai departe. O etapa de tip **verificare** poate trimite task-ul inapoi. Dupa ultima etapa decide liderul.
- Intr-o etapa cu mai multi agenti, task-ul il ia **primul agent liber**.
- Etapele sunt **optionale per proiect**. Un proiect fara etape functioneaza exact ca azi: board fix, reviewer per agent.
- Implementarea: etapa se adauga peste statusurile existente (`stage_id` pe task). Scheduler-ul, MCP-ul si board-ul se extind, nu se rescriu.

## Modelul de date (migrarea 005)

```sql
CREATE TABLE stages (
  id         INTEGER PRIMARY KEY,
  project_id INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  position   INTEGER NOT NULL,           -- ordinea pe board, de la 0
  name       TEXT NOT NULL,
  kind       TEXT NOT NULL DEFAULT 'work' CHECK (kind IN ('work', 'check'))
);
CREATE INDEX stages_project ON stages(project_id, position);

CREATE TABLE stage_agents (
  stage_id INTEGER NOT NULL REFERENCES stages(id) ON DELETE CASCADE,
  agent_id INTEGER NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
  PRIMARY KEY (stage_id, agent_id)
);

ALTER TABLE tasks ADD COLUMN stage_id INTEGER REFERENCES stages(id);
```

- **Proiect cu etape:** un proiect care are cel putin o etapa.
- **`tasks.stage_id`:**
  - e NULL la proiectele fara etape;
  - e NULL si cand task-ul a trecut de ultima etapa (asteapta decizia liderului sau e Gata);
  - altfel e etapa curenta.
- **`assignee_id` ramane NOT NULL.** Cat timp un task asteapta intr-o etapa (`queued`), `assignee_id` e doar un loc tinut. Scheduler-ul il schimba pe primul agent liber din etapa chiar inainte sa porneasca run-ul. Board-ul nu arata acest nume pentru task-urile in asteptare din etape.
- Un agent poate fi in mai multe etape.

## Cum curge un task

**Statusurile raman cele de azi.** Intr-o etapa, ele inseamna:

| `status` | `stage_id` | Ce inseamna |
|---|---|---|
| `queued` | etapa E | asteapta un agent liber din E |
| `in_progress` | etapa E | un agent din E lucreaza la el |
| `blocked` | etapa E | blocat dupa 3 respingeri in E; decide utilizatorul |
| `review` (`review_stage='lead'`) | NULL | a trecut de toate etapele; decide creatorul |
| `review` (`review_stage='lead'`) | etapa E | run terminat fara raport in E; decide creatorul (`send_back` il intoarce in E) |
| `done` | NULL | Gata |

**Crearea task-ului:**
- `create_task` al unui lider, intr-un proiect cu etape:
  - task-ul intra in prima etapa (`position` minima), `queued`;
  - `assignee` e optional. Daca e dat, trebuie sa fie un agent din prima etapa (altfel eroare: „<nume> nu e in etapa <prima>”) si devine cel care il ia primul. Daca lipseste, locul tinut e primul agent al etapei (sau creatorul, daca etapa nu are agenti);
  - regula „doar echipei tale directe” nu se aplica in proiectele cu etape: orice lider poate crea task-uri, iar etapele decid cine lucreaza;
  - `creator_id` e liderul.
- `POST /api/projects/{id}/tasks` cu `{title, description}` e un endpoint nou, doar pentru proiectele cu etape. Task-ul intra in prima etapa cu `creator_id` NULL, deci decide utilizatorul.

**Preluarea (scheduler):**
- pentru fiecare task `queued` cu `stage_id` E, in ordinea numarului;
- alege primul agent din E (in ordinea din `stage_agents`, adica ordinea bifarii) care e inactiv si nu depaseste `max_parallel`;
- seteaza `assignee_id` si porneste run-ul pe task;
- daca E nu are agenti, task-ul asteapta. Board-ul arata pe coloana: „Etapa fara agenti”.

**Etapa de lucru (`work`):**
- agentul termina cu `report_done(summary)`;
- `result` = rezumatul;
- task-ul trece in etapa urmatoare (`queued`, `feedback` golit). Daca era ultima etapa: `stage_id` NULL, `status=review`, `review_stage=lead`, iar creatorul primeste notificarea (sau decide utilizatorul, daca creatorul lipseste).

**Etapa de verificare (`check`):**
- agentul din etapa primeste task-ul pe run si foloseste:
  - `review_approve(task, note?)`: task-ul trece in etapa urmatoare, ca mai sus;
  - `review_reject(task, feedback)`: task-ul revine in cea mai apropiata etapa `work` dinaintea ei, `queued`, cu `feedback` si `send_backs++`. La 3, `status=blocked` in etapa curenta (regula de azi);
- daca inaintea ei nu exista nicio etapa `work`, respingerea intoarce task-ul in prima etapa;
- `report_done` intr-o etapa de verificare e tratat ca `review_approve`.

**Decizia finala:**
- creatorul (sau un stramos) foloseste `accept_task`, care duce task-ul in `done` (`stage_id` NULL), sau `send_back`, care il intoarce, `queued`, cu feedback si `send_backs++`: in etapa E daca `stage_id` e setat, altfel in prima etapa;
- utilizatorul are pe board butoanele **Accepta** si **Trimite inapoi** pe cardurile din „La lider” care nu au creator.

**Blocat:** butoanele de azi, **Accepta** (Gata) si **Deblocheaza**. Deblocheaza pune task-ul inapoi `queued` in aceeasi etapa, cu `send_backs=0`.

**Reviewer-ul per agent** (`reviewer_id`) se ignora pentru task-urile cu `stage_id`. Ierarhia si canvas-ul raman: cine vorbeste cu cine si cine creeaza task-uri.

**Run fara `report_done`:**
- un run pe un task dintr-o etapa care se termina fara `report_done` sau decizie de verificare pune task-ul in „La lider” (`review_stage=lead`, `stage_id` ramane E), cu `result` = „run terminat fara raport: <text>”, cum face azi;
- `accept_task` il duce in `done`, iar `send_back` il intoarce in etapa E.

**Stop / restart:** task-urile `in_progress` redevin `queued` in aceeasi etapa (`RequeueInProgress`, ca azi).

## Prompturi si MCP

- **Mesajul turei pe un task dintr-o etapa** contine: „Etapa: Dev (lucru). Cand termini, apeleaza report_done; task-ul merge la QA.” Sau, pentru verificare: „Etapa: QA (verificare). Verifica munca si foloseste review_approve sau review_reject; aprobat merge la <urmatoarea sau „liderul”>.”
- **Rezultatul etapei anterioare:** `result` si `feedback` apar in tura, ca azi.
- **Tool-urile pentru un run pe un task cu `stage_id`:**
  - etapa `work`: `report_done`;
  - etapa `check`: `review_approve` si `review_reject`, plus `report_done`, tratat ca aprobare;
  - acestea se adauga peste tool-urile rolului, calculate ca azi.
- **`create_task`**, intr-un proiect cu etape, primeste descrierea: „Task-ul intra in prima etapa (<nume>); assignee e optional, un agent din acea etapa.” Lista echipei din tura liderului contine si etapele cu agentii lor.
- **Revalidarea in backend:**
  - `review_approve` si `review_reject` pe un task cu `stage_id` cer ca apelantul sa fie asignatul curent si ca etapa sa fie `check`;
  - `report_done` cere asignatul curent.

## API

| Ruta | Ce face |
|---|---|
| `GET /api/projects/{id}` | contine si `stages: [{id, name, kind, position, agentIds}]`; task-urile au `stageId` |
| `PUT /api/projects/{id}/stages` | `{stages: [{id?, name, kind, agentIds}]}` in ordinea dorita; inlocuieste lista (vezi mai jos) |
| `POST /api/projects/{id}/tasks` | `{title, description}`; doar in proiectele cu etape (altfel 400) |
| `POST /api/tasks/{id}/decide` | `{action: "accept" \| "send_back", feedback?}`; doar pentru task-uri `review` + `lead` fara creator (decide utilizatorul) |

**`PUT /stages`:**
- intr-o tranzactie;
- etapele cu `id` se actualizeaza (nume, tip, pozitie, agenti), cele fara `id` se creeaza;
- etapele care lipsesc din lista se sterg doar daca nu au task-uri (`stage_id`). Altfel 409: „Etapa X are task-uri: termina-le sau muta-le intai”;
- validari: 0-12 etape, nume 1-40 de caractere si unice (fara diferenta intre litere mari si mici), `kind` valid, agentii din proiect;
- o lista goala e permisa doar daca nicio etapa nu are task-uri. Proiectul redevine unul fara etape.

**Evenimente:** se publica evenimentul SSE `stages`, iar UI-ul reincarca proiectul.

## UI (tab-ul Task-uri)

**Header-ul board-ului:** butonul **Coloane**, plus **+ Task** doar in proiectele cu etape.

**Editorul de coloane** e un panou care se deschide peste board:
- o lista de etape, fiecare cu: nume, tip (Lucru sau Verificare), bife cu agentii proiectului, butoane ↑ ↓ si ×;
- butoanele „+ Etapa”, **Salveaza** si **Renunta**;
- un × pe o etapa cu task-uri e dezactivat, cu tooltip-ul „are task-uri”.

**Board-ul unui proiect cu etape:**
- o coloana per etapa, apoi „La lider”, apoi „Gata”;
- capul coloanei are numele, tipul (o eticheta „verificare”), avatarele agentilor si numarul de task-uri. O etapa fara agenti afiseaza avertismentul „fara agenti”;
- cardurile arata „in asteptare” sau avatarul si numele celui care lucreaza, plus „Blocat”, cu butoanele de azi.

**Board-ul unui proiect fara etape** ramane cel de azi, cu un rand discret: „Fiecare proiect e diferit: fa-ti propriile coloane (etape) din butonul Coloane.”

**Scroll:** board-ul trece pe scroll orizontal cand sunt mai mult de 4-5 coloane (`grid-auto-flow: column`, coloane de minim 220px).

**+ Task** deschide un mic formular (titlu, descriere) si trimite `POST /api/projects/{id}/tasks`.

## Erori si cazuri limita

- Stergerea unui agent il scoate din `stage_agents` (cascada). Task-urile `queued` care il aveau loc tinut raman in etapa si le ia alt agent. Daca etapa ramane goala, se aplica „fara agenti”.
- Reordonarea etapelor nu muta task-urile. Un task isi continua drumul dupa noua ordine.
- Schimbarea tipului unei etape cu un task `in_progress` se aplica de la urmatorul run.
- Un proiect fara etape nu are regulile noi.

## Testare

- **store:** migrarea; salvarea etapelor (creare, actualizare, reordonare, stergere refuzata cand etapa are task-uri); `stage_agents` la stergerea unui agent.
- **team (runner-ul fals):**
  - flux Dev(Bob, Rook) → QA(Vera): doua task-uri le iau Bob si Rook (primul liber);
  - `report_done` duce in QA si Vera il preia;
  - `review_reject` il intoarce in Dev cu feedback, `review_approve` il duce la La lider, iar liderul accepta si task-ul ajunge in Gata;
  - respingerea de 3 ori → blocat;
  - etapa fara agenti: task-ul asteapta;
  - `create_task` cu un assignee din afara primei etape → eroare;
  - `send_back` la final → prima etapa.
- **mcp:** tool-urile pentru un run intr-o etapa `check`.
- **web:** `PUT /stages` (validari, 409), `POST /tasks` (400 fara etape), `POST /decide`.
- **UI:** `node --check`, plus capturi prin CDP la 1440×900 si 1280×720: editorul de coloane si board-ul cu etape.
- **e2e real (haiku):** proiect cu Lead → Dev(worker) → QA(verificare); un obiectiv mic pana la Gata.

## In afara acestei felii

- Mutarea manuala a task-urilor cu drag intre coloane.
- Template-uri de etape; limite WIP pe etapa.
- Etape pentru sub-task-urile sub-liderilor (un sub-lider intr-un proiect cu etape lucreaza pe etape ca oricare agent).
