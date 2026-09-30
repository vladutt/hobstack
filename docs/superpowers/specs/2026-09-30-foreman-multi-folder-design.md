# Foreman: proiecte cu mai multe foldere

Data: 2026-09-30
Status: design aprobat in chat.

## Scop

Un proiect Foreman poate avea mai multe foldere de lucru care tin de acelasi produs. Exemplu: Probe are
clientul (`~/code/probe-client`) si landing page-ul (`~/code/probe-landing`). Folderele pot fi repo-uri git
sau foldere simple.

- Fiecare folder are nume, cale si context propriu (stack, comenzi de build/test, conventii).
- Fiecare agent are un folder de baza: acolo ruleaza si doar acolo scrie. Celelalte foldere le poate citi.
- Echipele separate ies din ierarhia existenta: un lead de proiect, sub el cate un sub-lider per folder cu
  workerii lui. Comunicarea intre echipe merge prin ierarhie (task-uri si report_done), fara mecanism nou.
- Proiectele cu un singur folder merg si arata exact ca azi.

## Decizii (din brainstorming)

- **Varianta aleasa:** un proiect cu mai multe foldere (nu proiecte separate legate intre ele, nu folder parinte).
- **Scris:** doar in folderul propriu. Citire: toate folderele proiectului.
- **Wizard-ul** suporta mai multe foldere inca din aceasta versiune.
- **Folder, nu repo:** nimic nu presupune git.

## Model de date

Tabel nou:

```sql
CREATE TABLE project_folders (
  id         INTEGER PRIMARY KEY,
  project_id INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  name       TEXT NOT NULL,     -- "Client", unic in proiect (fara diferenta de litere)
  path       TEXT NOT NULL,     -- cale absoluta, unica in proiect
  context    TEXT NOT NULL DEFAULT '',
  position   INTEGER NOT NULL   -- ordinea; primul folder e cel implicit
);
```

- `agents.folder_id INTEGER NULL REFERENCES project_folders(id) ON DELETE SET NULL`.
  `NULL` = primul folder al proiectului.
- Migrare: pentru fiecare proiect, `projects.workdir` devine folderul cu `position = 0`, cu numele
  `filepath.Base(workdir)` si context gol. Coloana `projects.workdir` ramane (compatibilitate) si e tinuta
  egala cu calea primului folder; codul nou citeste folderele.
- Orice proiect are cel putin un folder.
- Task-urile nu au folder propriu: folderul unui task e folderul agentului asignat.

`store.Folder { ID, ProjectID, Name, Path, Context, Position }`. Proiectul se incarca cu `Folders []Folder`.
Functie ajutatoare: `HomeFolder(agent, folders) Folder` (folderul agentului sau primul).

## Run-ul unui agent (`internal/team/run.go`, `internal/prompt`)

- `cwd` = calea folderului de baza. Verificarea "folderul exista" se face pe el, iar mesajul numeste folderul.
- `--add-dir <cale>` pentru fiecare alt folder al proiectului (pe langa folderul de fisiere al proiectului).
- Permisiuni: `prompt.Settings(tools, home)`. Cu tool-ul `write`, in loc de `Edit`, `Write`, `NotebookEdit`
  fara restrictie, regulile devin `Edit(//<home>/**)`, `Write(//<home>/**)`, `NotebookEdit(//<home>/**)`.
  Limita cunoscuta ramane: cu `shell`, un agent poate ocoli regula (textul din UI spune deja asta).
- System prompt, sectiunea `Project`, dupa `Goal`:
  - daca proiectul are mai multe foldere, lista:
    `Folders:\n- Client: /Users/.../probe-client (your folder, you can write here)\n- Landing: /Users/.../probe-landing (read only)`;
  - `Shared context:` ca azi;
  - contextul fiecarui folder nevid: `Context for Client:\n...`.
  Cu un singur folder, sectiunea arata ca azi, plus contextul folderului daca exista.
- Mesajul turei pentru lideri: fiecare membru din "Your direct team" primeste `folder: <nume>` cand
  proiectul are mai multe foldere.
- Hash-ul sesiunii foloseste calea folderului de baza in locul lui `p.Workdir`: schimbarea folderului unui
  agent porneste o sesiune noua.

**Primul pas din implementare (spike):** verificam pe claude-ul real ca, in modul `-p` cu
`--setting-sources user`, `--add-dir` si regula `Edit(//<home>/**)`, un `Write` in celalalt folder e refuzat
si unul in folderul propriu trece. Daca nu merge, ne oprim si revenim la utilizator inainte de restul.

## API (`internal/web`)

- `GET /api/projects/{id}` intoarce `folders`. Agentii au `folderId`.
- `POST /api/projects` si `PUT /api/projects/{id}` primesc `folders: [{id?, name, path, context}]` in locul
  lui `workdir` (acceptat in continuare: un `workdir` fara `folders` inseamna un singur folder).
  - calea se rezolva ca azi (`resolveWorkdir`: gol = `~/Foreman/<slug>`, inexistenta = se creeaza);
  - numele gol devine `filepath.Base(cale)`;
  - nume sau cai duplicate: 400;
  - un folder care lipseste din lista e sters; agentii lui trec pe `NULL` (primul folder), intr-o tranzactie;
  - ordinea din lista e `position`.
- `PUT /api/agents/{id}` primeste `folderId` (null = primul folder); trebuie sa fie un folder al proiectului.
- `POST /api/projects/{id}/open` primeste optional `folderId` (implicit primul folder).

## UI

- **Formularul proiectului:** sectiunea "Working folder" devine "Folders": un rand per folder cu Name,
  Path (+ Browse...), Context; "+ Add folder"; stergere cat timp raman minim unul. Daca folderul sters are
  agenti, un `confirm()` spune ca trec in primul folder. "Shared context" ramane pentru ce e comun.
- **Editorul de agent:** select "Folder" cand proiectul are mai mult de un folder.
- **Canvas:** sub-eticheta nodului include folderul ("Lead · Client") cand sunt mai multe foldere.
- **Board:** chips "All / Client / Landing" deasupra coloanelor, dupa folderul agentului asignat; task-urile
  fara agent apar mereu. Alegerea se pastreaza in starea workspace-ului (nu in URL).
- **"Open in:":** cu mai multe foldere, un select cu folderul de deschis langa butoane.

## Wizard "New project with AI" (`internal/setup`, `setup.js`)

- **Pasul 1:** lista de foldere (rand cu cale + Browse..., "+ Add folder"). Regulile de azi raman: fara
  niciun folder e nevoie de descriere; un folder gol fara descriere e refuzat daca e singurul folder.
- **Sesiunea:** `Workdir string` devine `Workdirs []string`. Analistul ruleaza in primul folder, cu
  `--add-dir` pentru restul. Titlul cardului: `filepath.Base` al primului folder pana vine propunerea.
- **Turn prompt:** listeaza folderele cu calea lor.
- **System prompt:** cu mai multe foldere, propune cate o echipa per folder (sub-lider + workeri) sub un
  lead comun, un context comun pentru produs si un context per folder; fiecare agent are un folder.
- **`propose_setup`:** campuri noi `folders: [{path, name, context}]` si `agents[].folder` (numele
  folderului). Validare: cu foldere alese, `folders` trebuie sa contina exact acele cai; fiecare
  `agents[].folder` trebuie sa existe (gol = primul). Fara foldere alese, `folders` lipseste sau are un
  singur element fara cale.
- **Pasul 3:** un camp Context per folder (editabil), select "Folder" per agent, arborele arata folderul.
- **Creare:** `POST /api/projects/setup` creeaza proiectul, folderele si agentii intr-o tranzactie.

## Cazuri limita

- Folder inexistent la run: run-ul esueaza cu `the folder Client (/cale) does not exist`.
- Doua foldere cu acelasi nume sau aceeasi cale: 400 la salvare.
- Stergerea unui folder: agentii lui trec in primul folder (`folder_id = NULL`); se pornesc sesiuni noi
  (hash-ul se schimba).
- Stergerea primului folder: urmatorul devine primul; agentii cu `NULL` ajung acolo.
- Fisierele proiectului (tab-ul Files) nu se schimba.

## Teste

- store: migrarea (un proiect vechi primeste un folder din workdir), CRUD foldere, stergerea muta agentii.
- prompt: lista de foldere si contextele in system prompt; un singur folder = iesirea de azi;
  `Settings` cu reguli restranse la folder; "Your direct team" cu folderul.
- team: `cwd` = folderul agentului, `--add-dir` pentru celelalte.
- web: API-ul de foldere (creare, editare, stergere, duplicate), `folderId` pe agent, open cu `folderId`.
- setup: validarea `folders` si `agents[].folder`, crearea cu mai multe foldere.
- Screenshot-uri prin CDP: formularul proiectului, board-ul cu filtru, editorul de agent, wizard-ul.

## In afara scopului

- Scris in mai multe foldere pentru acelasi agent.
- Task-uri legate explicit de un folder (fara agent).
- Proiecte separate legate intre ele.
