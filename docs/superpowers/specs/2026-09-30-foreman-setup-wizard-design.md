# Foreman: wizard „Proiect nou cu AI”

Data: 2026-09-30
Status: design aprobat in chat.

## Scop

Un proiect nou poate fi facut cu ajutorul unui AI:
- alegi un folder local (optional) si scrii ce vrei sa faci (optional, dar cerut fara folder sau cu folder gol);
- un analist Claude citeste folderul si propune numele, obiectivul si contextul comun al proiectului, plus echipa: agentii, legaturile dintre ei si prompturile lor;
- vezi propunerea, o editezi si abia apoi creezi proiectul.

## Decizii (din brainstorming)

- **Dupa analiza:** previzualizare editabila, nu creare directa si nici conversatie.
- **Folder gol sau lipsa:** wizard-ul merge si doar din descriere. Folderul se rezolva ca acum; gol inseamna `~/Foreman/<slug>`.
- **Detaliile agentilor:** AI-ul completeaza tot (nume, rol, model, „Ce ia”, sef, reviewer, background, instructiuni, definition of done). Tool-urile propuse pot fi doar `write` si `git`; `shell` si `web` le bifeaza utilizatorul in previzualizare.
- **Rezultatul structurat:** vine printr-un tool MCP `propose_setup` pe serverul MCP existent, validat la apel. Nu parsam JSON din text.

## Fluxul in UI

Pagina Proiecte primeste cardul „Proiect nou cu AI” (ruta `#/setup`). Cardul „Proiect nou” ramane formularul clasic.

**Pas 1, alegerea:**
- `Folder` (optional), cu butonul „Alege...” (acelasi dialog nativ ca in formularul de proiect);
- `Ce vrei sa faci?` (textarea, maxim 4000 de caractere);
- `Model analist`: select cu modelele din `/api/info`, implicit `sonnet`;
- butonul **Analizeaza**.

**Pas 2, analiza:**
- output-ul live al run-ului, afisat cu `liveRenderer` din `live.js`: texte si tool-uri, ca in chat;
- butonul **Opreste**.

**Pas 3, propunerea:**
- campuri editabile: Nume, Obiectiv, Context comun;
- textul „De ce aceasta echipa”, scris de AI, doar de citit;
- un card per agent propus:
  - casuta „Include” (bifata implicit);
  - Nume, Rol, Model, „Ce ia”, „Raporteaza lui” (Tu + agentii inclusi), „Verificat de” (nimeni + agentii inclusi);
  - tool-urile ca bife;
  - background, instructiuni si definition of done intr-un `<details>` „Prompt”;
- o previzualizare a grafului, doar de citit: arborele indentat „Tu → Lead → ...”, redesenat la fiecare schimbare;
- butoanele **Inapoi** (la pasul 1, pastrand campurile) si **Creeaza proiectul**.

Un agent debifat dispare din selectele celorlalti. Cei care il aveau ca sef trec la seful lui (sau la Tu), iar reviewer-ul lui devine „nimeni”, aceeasi regula ca la stergere.

Dupa creare, navigarea merge la `#/p/<id>/echipa`.

## Backend

### Pachetul `internal/setup`

`Manager` tine sesiunile de setup in memorie (`map[id]*Session`, cu mutex). Nu exista tabel in DB: o sesiune se pierde la restart, ceea ce e acceptabil.

```go
type Session struct {
    ID          string    // aleator, 16 hex
    RunID       string
    Workdir     string    // cum a fost dat (poate fi gol)
    Description string
    Status      string    // "running", "ready", "failed", "stopped"
    Proposal    *Proposal // setat de propose_setup
    Error       string
    CreatedAt   time.Time
}

type Proposal struct {
    Name      string          `json:"name"`
    Goal      string          `json:"goal"`
    Context   string          `json:"context"`
    Rationale string          `json:"rationale"`
    Agents    []ProposedAgent `json:"agents"`
}

type ProposedAgent struct {
    Name             string   `json:"name"`
    Role             string   `json:"role"`
    Model            string   `json:"model"`
    Responsibilities string   `json:"responsibilities"`
    ReportsTo        string   `json:"reportsTo"`  // numele sefului; "" = utilizatorul
    ReviewedBy       string   `json:"reviewedBy"` // numele reviewer-ului; "" = nimeni
    Background       string   `json:"background"`
    Instructions     string   `json:"instructions"`
    DoneDef          string   `json:"doneDef"`
    Tools            []string `json:"tools"`
}
```

**`Start(workdir, description, model)`:**
- validari:
  - descrierea si folderul nu pot fi amandoua goale;
  - modelul trebuie sa fie `models.Valid`;
  - daca folderul e dat, trebuie sa existe si sa fie director (altfel 400 „folderul nu exista”). Wizard-ul nu creeaza folderul la analiza, abia la creare;
- cwd-ul run-ului e folderul dat, iar fara folder e `~/.foreman/setup/<id>` (creat gol si sters cand se termina sesiunea);
- porneste run-ul prin `runner.Manager` cu cheia `setup-<id>`.

**Argumentele lui `claude`:**
- `-p <prompt>`, cu textul din sectiunea Prompt;
- `--output-format stream-json --verbose --model <m>`;
- `--append-system-prompt <sistem>`;
- `--setting-sources user`;
- `--settings`:
  - allow `Read`, `Glob`, `Grep`, `LS` si `mcp__foreman__propose_setup`;
  - deny `Write`, `Edit`, `MultiEdit`, `NotebookEdit`, `Bash`, `WebFetch`, `WebSearch`, `Agent`, `TaskCreate`, `TaskUpdate`, `TaskList`, `TaskGet`, `TodoWrite`;
- `--mcp-config` spre `/mcp/<token>` si `--strict-mcp-config`.

La final (`onDone`):
- run-ul termina cu propunere: `ready`;
- run-ul termina fara propunere: `failed`, cu „AI-ul nu a propus nimic. Reincearca.”;
- run-ul esueaza: `failed`, cu eroarea din `Outcome` (ex. nu esti logat);
- run-ul e oprit: `stopped`.

Token-ul e sters la final. Sesiunile terminate mai vechi de 24h sunt sterse la urmatorul `Start`.

**`Stop(id)`** opreste run-ul, iar **`Get(id)`** intoarce o copie a sesiunii.

**`Propose(id, raw json.RawMessage)`** e apelat din MCP. Parseaza si valideaza cu `ValidateProposal`. Pe eroare intoarce mesajul (ajunge la Claude ca `isError`, iar el corecteaza). Pe succes retine propunerea si intoarce „Propunere primita. Ai terminat.”

### `ValidateProposal(p *Proposal, strictTools bool) error`

Se foloseste si la MCP (`strictTools=true`), si la creare (`false`).

- **Limite:**
  - `name` 1-80 de caractere, dupa `TrimSpace`;
  - `goal` maxim 4000;
  - `context` maxim 20000;
  - `rationale` maxim 4000;
  - agenti: intre 1 si 12.
- **Fiecare agent:**
  - nume 1-40 de caractere, unic (fara diferenta intre litere mari si mici);
  - rol maxim 80, `responsibilities` maxim 2000, background si instructiuni maxim 10000 fiecare, definition of done maxim 4000.
- **Model:** unul necunoscut devine `sonnet` (nu e eroare).
- **Tool-uri:**
  - duplicatele se elimina;
  - cu `strictTools`, doar `write` si `git` sunt permise, iar orice altceva e eroare: „poti propune doar tool-urile write si git”;
  - fara `strictTools`, se accepta orice din `prompt.ValidTools`.
- **Legaturi:**
  - `reportsTo` si `reviewedBy` sunt nume din propunere sau goale;
  - nimeni nu e propriul sef sau propriul reviewer;
  - lantul de sefi nu formeaza un ciclu („legatura ar crea un ciclu”);
  - exista cel putin o radacina (un agent cu `reportsTo` gol).

### MCP

**`Caller`:**
- primeste campul `SetupID string`;
- un caller cu `SetupID` vede in `tools/list` doar `propose_setup`, fara tool-urile de echipa.

**`Backend`:**
- primeste metoda `ProposeSetup(c Caller, args json.RawMessage) (string, error)`;
- in `web`, `mcpBackend` compune doua surse:
  - `Resolve` cauta intai in token-urile de setup, apoi in cele ale echipei;
  - `ProposeSetup` merge la `setup.Manager`;
  - restul metodelor merg la `team`, ca acum;
- `team` implementeaza `ProposeSetup` intorcand o eroare, ca sa satisfaca interfata.

**Tool-ul `propose_setup`:**
- descrierea: „Trimite propunerea de proiect si echipa. Apeleaza-l o singura data, la final.”;
- `inputSchema` e schema JSON a `Proposal`:
  - campuri obligatorii: `name`, `goal`, `context`, `agents`;
  - campuri obligatorii la agenti: `name`, `role`, `responsibilities`, `reportsTo`;
  - `tools` e un array cu `enum ["write","git"]`.

### Prompt-ul analistului

System prompt (fix):

> Esti arhitectul de echipa din Foreman. Citesti un proiect si propui o echipa mica de agenti Claude Code care va lucra pe el.
>
> Reguli:
> - Citeste doar cat iti trebuie: structura folderului, README, fisierele de dependinte si build (package.json, composer.json, go.mod, Makefile etc.) si cateva fisiere reprezentative. Nu citi tot repo-ul.
> - Nu modifica nimic; nu ai voie.
> - Echipa: 2-6 agenti, de obicei un singur lider radacina (reportsTo gol) care imparte munca; adauga un reviewer doar daca proiectul chiar are nevoie.
> - Pentru fiecare agent scrie: rol, „Ce ia” (tipurile de task-uri), background (ce stie despre acest proiect), instructiuni (cum lucreaza, ce nu face), definition of done.
> - Contextul comun: stack-ul, comenzile de build/test/rulare, conventiile si structura, pe scurt.
> - Scrie in limba descrierii utilizatorului (implicit romana, fara diacritice).
> - La final apeleaza o singura data propose_setup cu toata propunerea.

Mesajul turei:
- `Folder: <cale>`, sau `Nu exista inca un folder: proiectul e nou.`;
- `Ce vrea utilizatorul: <descriere>`, sau `Utilizatorul nu a descris nimic; deduce din cod.`

### API

| Ruta | Ce face |
|---|---|
| `POST /api/setup` | `{workdir, description, model}` → 201 cu `{id, runId}`; 400 pe validari |
| `GET /api/setup/{id}` | sesiunea (`status`, `runId`, `proposal`, `error`); 404 daca nu exista |
| `POST /api/setup/{id}/stop` | 202 |
| `POST /api/projects/setup` | `{workdir, proposal}` → 201 cu proiectul creat |

Cand sesiunea se schimba (`ready`, `failed`, `stopped`), UI-ul afla din evenimentul `app_done` al SSE-ului de run, apoi face `GET /api/setup/{id}`.

**`POST /api/projects/setup`:**
- `ValidateProposal(p, false)` doar pe agentii trimisi (UI-ul trimite doar agentii inclusi);
- folderul se rezolva cu `resolveWorkdir`, ca la `POST /api/projects`;
- `store.CreateProjectWithTeam(p *Project, agents []Agent, links)` face intr-o singura tranzactie:
  - proiectul, fara „Lead”-ul implicit;
  - agentii in ordinea din propunere;
  - apoi `parent_id` si `reviewer_id` dupa nume;
- pe orice eroare nu se creeaza nimic;
- se publica evenimentul `project` ca la crearea normala.

## Erori

- Analist fara `propose_setup`: `failed`, cu „AI-ul nu a propus nimic. Reincearca.”; UI-ul arata butonul **Reincearca**, care reporneste cu aceleasi campuri.
- CLI-ul lipsa sau nelogat, ori reteaua cazuta: mesajul din `Outcome.Error` sau `Result`, ca la agenti.
- Folderul dat nu exista: 400 la `POST /api/setup`.
- Un nume de proiect care exista deja nu e eroare (numele proiectelor nu sunt unice).
- Un nume de agent duplicat dupa editare: 400 „exista deja un agent cu numele X in proiect”.

## Securitate

- Analistul nu are tool-uri de scriere, shell sau web; permisiunile vin doar din `--settings` (ca la agenti).
- Codul din folder e trimis la Claude, la fel ca la orice agent; alegerea folderului e consimtamantul.
- Rutele noi trec prin garda existenta (Host si Content-Type).

## Testare

- **setup:**
  - `ValidateProposal`: ciclu, fara radacina, nume duplicat, sef inexistent, tool-uri interzise cu `strictTools`, model necunoscut → `sonnet`, limite;
  - `Manager` cu runner-ul fals din teste: propunere → `ready`; fara propunere → `failed`; `Stop` → `stopped`.
- **mcp:** pentru un caller cu `SetupID`, `tools/list` contine doar `propose_setup`, iar `tools/call` ajunge la `ProposeSetup`.
- **store:** `CreateProjectWithTeam` creeaza legaturile corect si, la un ciclu, nu creeaza nimic.
- **web:**
  - `POST /api/setup` cu validari (ambele goale, folder inexistent, model invalid);
  - `POST /api/projects/setup` (201 si detaliile proiectului cu legaturi; 400 fara nimic creat).
- **UI:** `node --check`, plus screenshot-uri si masuratori prin Chrome headless la 1440×900 si 1280×720 pentru cei trei pasi. Pasul 3 se testeaza cu o sesiune `ready` injectata prin runner-ul fals, sau cu e2e-ul real.
- **e2e real (haiku):** wizard pe folderul `agentdash` insusi, apoi „Creeaza proiectul” si verificarea echipei pe canvas.

## In afara acestei felii

- „Propune agenti noi” pentru un proiect existent.
- Template-uri de echipa si biblioteca de agenti.
- Sesiuni de setup persistente dupa restart.
