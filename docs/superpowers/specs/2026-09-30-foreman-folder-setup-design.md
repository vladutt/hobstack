# Foreman: wizard AI pentru un folder dintr-un proiect existent

Data: 2026-09-30
Status: design aprobat in chat.

## Scop

Cand adaugi un folder intr-un proiect existent (ex. Probe primeste Landing), un analist AI citeste folderul,
vede proiectul si echipa existenta si propune:
- contextul folderului;
- o echipa mica pentru acel folder (de obicei un sub-lider + 1-3 workeri), legata sub un agent existent.

Utilizatorul editeaza propunerea si adauga agentii in proiect. Agentii existenti nu se modifica niciodata.

## Decizii (din brainstorming)

- **Ce propune:** doar agenti noi pentru folder + contextul folderului. Fara modificari la agentii existenti.
- **Pornire:** buton **"Set up agents with AI"** pe fiecare folder deja salvat, in formularul "Project instructions".
- **Abordare:** extindem wizard-ul existent (`internal/setup`) cu un mod "folder", nu un wizard separat.
  Aceeasi sesiune in fundal, acelasi tool MCP `propose_setup`, aceeasi pagina de review.
- **Rulare in fundal:** ca wizard-ul de azi (card pe Projects: Analyzing / Ready / Failed; Dismiss; Retry).

## Backend

### Sesiunea (`internal/setup`)

`Session` primeste campurile:

```go
ProjectID int64 `json:"projectId"` // 0 = proiect nou (wizard-ul de azi); altfel modul folder
FolderID  int64 `json:"folderId"`
```

Nume nou de pornire, separat de `Start` (care ramane neschimbat):

```go
// StartFolder porneste analistul pentru un folder dintr-un proiect existent.
func (m *Manager) StartFolder(in FolderInput, description, model string) (Session, error)

type FolderInput struct {
    Project store.Project  // cu Folders
    Folder  store.Folder   // folderul analizat (din Project.Folders)
    Team    []store.Agent  // echipa existenta
}
```

- `cwd` = calea folderului; `--add-dir` pentru celelalte foldere ale proiectului (doar citire, ca acum).
- `Workdirs` = `[folder.Path]` (pentru card si pentru CheckFolderPaths).
- Titlul cardului: `"<Project.Name> · <Folder.Name>"` (ramane asa si dupa propunere).
- Sesiunea retine numele agentilor existenti (litere mici) si un rezumat al echipei, pentru validare.
- Aceleasi argumente de siguranta: `--permission-mode dontAsk`, doar citire, `--setting-sources user`.

### Prompturi

- System prompt: cel existent + un paragraf pentru modul folder, activ doar in modul folder (se adauga cu
  `--append-system-prompt`, textul e o constanta separata):
  "You are adding ONE folder to an EXISTING project. Propose only new agents that work in this folder (usually one
  sub-lead and 1-3 workers) and the folder's context. Do not change or repeat existing agents. The sub-lead reports
  to an existing agent (usually the root lead); use its exact name in reportsTo. New names must not collide with
  existing ones. If the folder has a CLAUDE.md, base the folder context on it. Put exactly this folder in folders;
  name/goal/context of the project are ignored (repeat the project name)."
- Turn prompt: proiectul (nume, obiectiv, context comun), folderul (nume, cale), celelalte foldere (nume, cale),
  echipa existenta (cate o linie: `- Name: Role, folder: X, reports to: Y, takes: ...`), dorinta utilizatorului
  (sau "infer it from the folder").

### Validarea in modul folder (la `propose_setup`)

Dupa `ValidateProposal` (care ramane pentru regulile comune), `CheckFolderProposal(p, sess)`:
- `folders`: lipseste sau contine exact folderul analizat (aceeasi cale); numele si contextul lui se pastreaza.
- Numele agentilor noi nu exista deja in proiect (fara diferenta de litere).
- `reportsTo` / `reviewedBy`: un agent nou din propunere SAU un agent existent din proiect.
- Cel putin un agent nou are `reportsTo` = un agent existent sau gol (utilizatorul), ca echipa sa nu ramana nelegata.
- `agents[].folder` e ignorat: toti agentii noi lucreaza in folderul analizat.

Pentru asta, validarea legaturilor din `ValidateProposal` primeste numele externe permise:
`ValidateProposal(p, strictTools, external []string)` — un `reportsTo`/`reviewedBy` care nu e in propunere e acceptat
daca e in `external`. Wizard-ul de azi trimite `nil`. Ciclurile se verifica doar intre agentii noi (cei existenti
nu pot avea ca sef un agent nou). Regula de azi "cel putin un agent raporteaza utilizatorului" devine, cand
`external` e nevid, "cel putin un agent nou raporteaza utilizatorului sau unui agent din `external`".

### Adaugarea agentilor (store + web)

`store.AddAgents(projectID, folderID int64, team []NewMember, folderContext *string) ([]Agent, error)`, intr-o
tranzactie:
- `NewMember` primeste `ParentID, ReviewerID *int64` pentru legaturi la agenti existenti (folosite cand
  `Parent`/`Reviewer` = -1);
- toti agentii noi primesc `folder_id` = folderID (NULL daca e primul folder, ca peste tot);
- verifica: folderul e al proiectului, sefii/reviewerii existenti sunt ai proiectului, nume unice (ErrConflict);
- daca `folderContext != nil`, actualizeaza contextul folderului.

Rute noi:
- `POST /api/projects/{id}/folders/{fid}/setup` `{description, model}` -> porneste `StartFolder`, intoarce sesiunea (201).
- `POST /api/projects/{id}/agents/setup` `{setupId, folderId, updateContext: bool, proposal}` -> valideaza din nou
  (ca la creare), rezolva numele in id-uri (agenti noi = index, existenti = id), apeleaza `AddAgents`, apoi
  `Dismiss(setupId)`, publica evenimentul `agent` si intoarce agentii noi (201).

## UI

- **Project instructions:** fiecare rand de folder cu `id` (salvat) primeste butonul **"Set up agents with AI"** ->
  `#/setup/folder/<projectId>/<folderId>`. Randurile nesalvate nu il au (hint: "Save first").
- **Pasul 1 (mod folder):** titlu "Set up agents for <Folder>", "What should these agents do?" (optional), modelul,
  **Analyze**. Fara lista de foldere.
- **Pasul 3 (mod folder):**
  - sectiunea proiectului (nume/obiectiv/context comun) nu apare;
  - "Folder context" editabil + casuta **"Update folder context"** (bifata daca folderul n-are context);
  - cardurile agentilor noi ca acum, fara select Folder; "Reports to" contine: You, agentii noi, apoi agentii
    existenti (grup "Existing team");
  - arborele arata agentii existenti relevanti (seful) si echipa noua sub el;
  - butonul final **"Add agents"** -> `#/p/<id>/team`.
- **Cardul de pe Projects:** titlul `"<Proiect> · <Folder>"`; acelasi comportament (Watch/Stop, Review, Retry, Dismiss).
  Retry in modul folder reporneste `StartFolder` cu aceleasi date.
- Sesiunea expune `projectId`/`folderId`, deci `#/setup/<id>` stie modul la redeschidere.

## Cazuri limita

- Folderul sau proiectul sters intre timp: "Add agents" -> 404/400 cu mesaj clar.
- Un agent existent redenumit intre timp: numele din reportsTo nu mai exista -> 400 "the boss X does not exist";
  utilizatorul alege din select.
- Nume duplicat cu un agent existent (adaugat intre timp): 409 cu mesajul existent "an agent named X already exists".
- Proiect pe pauza: agentii se adauga normal; nu pornesc pana dai Start team.

## Teste

- setup: `ValidateProposal` cu nume externe; `CheckFolderProposal` (folder gresit, nume luat, sef existent, echipa
  nelegata); `StartFolder` (cwd, --add-dir, turn prompt cu echipa, titlu).
- store: `AddAgents` (folder_id, legaturi la existenti, context actualizat sau nu, nume duplicat -> nimic creat).
- web: cele doua rute (happy path, folder din alt proiect, nume duplicat).
- Browser (CDP + claude fals): buton -> analiza -> review -> Add agents -> agentii apar pe canvas in folder.

## In afara scopului

- Modificari la agentii existenti.
- Pornire automata la salvarea unui folder nou.
- Wizard pentru mai multe foldere deodata intr-un proiect existent.
