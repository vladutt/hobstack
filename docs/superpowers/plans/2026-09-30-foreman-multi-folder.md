# Proiecte cu mai multe foldere: plan de implementare

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Un proiect Foreman are mai multe foldere de lucru (ex. Probe: Client + Landing); fiecare agent lucreaza si scrie in folderul lui si le poate citi pe celelalte; wizard-ul propune echipe per folder.

**Architecture:** Tabel nou `project_folders` + `agents.folder_id` (NULL = primul folder). `store.Project` se incarca mereu cu `Folders`, deci `prompt`, `team` si `web` au folderele fara interogari noi. Run-ul unui agent: `cwd` = folderul lui, `--add-dir` pentru celelalte, regula de scris `Edit(//<folder>/**)`. UI: editor de foldere in formularul proiectului, select de folder in editorul de agent, filtru pe board, select la "Open in".

**Tech Stack:** Go 1.x, SQLite (modernc.org/sqlite), HTML + JS vanilla (embed), Claude Code CLI headless.

**Spec:** `docs/superpowers/specs/2026-09-30-foreman-multi-folder-design.md`

## Global Constraints

- `go vet ./...` si `go build -race ./...` trebuie sa treaca; `go test ./...` trece dupa fiecare task.
- Cross-compile: `GOOS=darwin GOARCH=arm64 go build ./cmd/foreman` si `GOOS=windows go build ./cmd/foreman`.
- Tot ce vede utilizatorul (UI, mesaje, erori) si prompturile pentru agenti sunt in engleza. Comentariile din cod raman in romana, fara diacritice.
- Fara dependente noi.
- Proiectele cu un singur folder se comporta si arata exact ca azi (acelasi system prompt, aceeasi sesiune, acelasi UI).
- `NULL` in `agents.folder_id` = primul folder al proiectului.
- Limite: maxim 6 foldere per proiect; nume de folder maxim 40 de caractere; contextul unui folder maxim 20000.
- Cod simplu si comentat unde nu e evident: autorul e incepator in Go.

## Review Focus

1. **Salvarea unui agent din canvas sau din editor pierde folderul.** `agentBody` din `ui.js` trebuie sa trimita `folderId`, altfel orice legatura trasa pe canvas muta agentul in primul folder. Acoperit in Task 6, Step 1, si verificat in browser in Task 9.
2. **Un proiect vechi, cu un singur folder, isi pierde sesiunile dupa upgrade.** Hash-ul sesiunii = calea folderului + system prompt; cu un singur folder fara context, ambele trebuie sa ramana identice cu azi. Test in Task 3 (`TestSystemSingleFolderUnchanged`).
3. **Stergerea folderului in care lucreaza un agent.** Agentul trebuie sa treaca in primul folder, nu sa ramana cu un id inexistent. Test in Task 2 (`TestSaveFoldersRemovesAndMovesAgents`).
4. **Formularul vechi, sau un client API care trimite doar `workdir`, la editarea unui proiect cu mai multe foldere.** Folderele in plus trebuie sa ramana. Test in Task 5 (`TestUpdateProjectWorkdirOnlyKeepsFolders`).
5. **Folderul unui agent sters de pe disc.** Run-ul trebuie sa esueze cu mesaj care numeste folderul, nu cu o eroare generica. Test in Task 4 (`TestRunFailsWhenFolderMissing`).

---

### Task 1: Spike: regula de scris restransa la un folder

Verificam pe claude-ul real ca regula `Edit(//<cale>/**)` blocheaza scrisul in celalalt folder. Nu se scrie cod in repo.

**Files:** niciun fisier din repo (doar in scratchpad).

- [ ] **Step 1: Pregateste doua foldere**

```bash
S=$(mktemp -d); mkdir -p "$S/A" "$S/B"; echo "$S"
```

- [ ] **Step 2: Ruleaza claude cu regula restransa la A**

```bash
cd "$S/A" && claude -p "Use the Write tool to create $S/B/x.txt containing hi. Then use the Write tool to create $S/A/y.txt containing hi. Report exactly which writes succeeded." \
  --model haiku --output-format json \
  --setting-sources user --add-dir "$S/B" \
  --settings "{\"permissions\":{\"allow\":[\"Read\",\"Glob\",\"Grep\",\"Edit(/$S/A/**)\",\"Write(/$S/A/**)\"]}}"
ls "$S/A" "$S/B"
```

Observatie: `$S` incepe cu `/`, deci `"/$S/A/**"` da `//tmp/.../A/**`, forma cu cale absoluta.

Expected: exista `$S/A/y.txt`, dar nu si `$S/B/x.txt`.

- [ ] **Step 3: Decide**

- Daca `B/x.txt` **nu** exista si `A/y.txt` exista: regula merge. Continua cu Task 2.
- Daca `B/x.txt` exista, **sau** nici `A/y.txt` nu exista, incearca intai varianta cu `Edit` fara `Write` in allow. Daca nici asa nu merge, **opreste-te**: raporteaza utilizatorului ce ai vazut, inainte de orice alt task. Designul depinde de regula asta.
- Noteaza forma exacta care a mers. Task 3 o foloseste in `pathRule`.

---

### Task 2: Store: foldere de proiect si folderul agentului

**Files:**
- Create: `internal/store/migrations/006_folders.sql`
- Create: `internal/store/folders.go`
- Modify: `internal/store/projects.go` (Project.Folders, CreateProject, CreateProjectWithTeam, GetProject, ListProjects, UpdateProject)
- Modify: `internal/store/agents.go` (Agent.FolderID, agentCols, scanAgent, insertAgent, UpdateAgent, checkLinks)
- Test: `internal/store/folders_test.go`

**Interfaces:**
- Produces:
  - `type Folder struct { ID, ProjectID int64; Position int; Name, Path, Context string }` (JSON: `id, projectId, position, name, path, context`)
  - `Project.Folders []Folder` (JSON `folders`), incarcat de `GetProject` si `ListProjects`
  - `Agent.FolderID *int64` (JSON `folderId`)
  - `func (s *Store) ListFolders(projectID int64) ([]Folder, error)`
  - `func HomeFolder(a Agent, folders []Folder) Folder`
  - `func FolderName(path string) string`
  - `NewMember.Folder int`: index in `p.Folders`; 0 = primul (salvat ca NULL)
  - `CreateProject(p *Project)` si `CreateProjectWithTeam(p *Project, ...)`: daca `p.Folders` e gol, creeaza un folder din `p.Workdir`; seteaza ID-urile in `p.Folders`; `p.Workdir` = calea primului folder
  - `UpdateProject(p Project)`: daca `len(p.Folders) > 0`, inlocuieste folderele (ca `SaveStages`); agentii din folderele sterse trec pe NULL; `projects.workdir` = calea primului folder

- [ ] **Step 1: Scrie testele (esueaza la compilare)**

`internal/store/folders_test.go`:

```go
package store

import (
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
)

// o baza "veche" (migrarile 001-005) primeste la deschidere cate un folder per proiect
func TestMigration006AddsFolderFromWorkdir(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vechi.db")
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"001_init.sql", "002_attachments.sql", "003_team_graph.sql", "004_detached.sql", "005_stages.sql"} {
		body, _ := migrations.ReadFile("migrations/" + name)
		if _, err := db.Exec(string(body)); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
	for _, q := range []string{
		`PRAGMA user_version = 5`,
		`INSERT INTO projects (id, name, workdir, created_at) VALUES (1, 'Probe', '/Users/ana/code/probe-client/', '2026-01-01T00:00:00Z'), (2, 'Root', '/', '2026-01-01T00:00:00Z')`,
		`INSERT INTO agents (id, project_id, name, position, created_at) VALUES (1, 1, 'Lead', 'worker', '2026-01-01T00:00:00Z')`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	db.Close()

	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	p, err := s.GetProject(1)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Folders) != 1 || p.Folders[0].Name != "probe-client" || p.Folders[0].Path != "/Users/ana/code/probe-client/" || p.Folders[0].Position != 0 {
		t.Fatalf("foldere dupa migrare: %+v", p.Folders)
	}
	if root, _ := s.GetProject(2); len(root.Folders) != 1 || root.Folders[0].Name != "Main" {
		t.Fatalf("folderul '/' trebuia sa se numeasca Main: %+v", root.Folders)
	}
	if a, _ := s.GetAgent(1); a.FolderID != nil {
		t.Fatalf("agentul vechi trebuia sa ramana pe primul folder (NULL): %v", *a.FolderID)
	}
}

func TestCreateProjectMakesFirstFolder(t *testing.T) {
	s := newStore(t)
	p := Project{Name: "Shop", Workdir: "/tmp/shop"}
	if _, err := s.CreateProject(&p); err != nil {
		t.Fatal(err)
	}
	if len(p.Folders) != 1 || p.Folders[0].ID == 0 || p.Folders[0].Name != "shop" {
		t.Fatalf("p.Folders dupa creare: %+v", p.Folders)
	}
	got, _ := s.GetProject(p.ID)
	if len(got.Folders) != 1 || got.Folders[0].Path != "/tmp/shop" {
		t.Fatalf("GetProject: %+v", got.Folders)
	}
	list, _ := s.ListProjects()
	if len(list) != 1 || len(list[0].Folders) != 1 {
		t.Fatalf("ListProjects fara foldere: %+v", list)
	}
}

// doua foldere; agentul din al doilea; apoi il stergem pe al doilea
func TestSaveFoldersRemovesAndMovesAgents(t *testing.T) {
	s := newStore(t)
	p := Project{Name: "Probe", Folders: []Folder{
		{Name: "Client", Path: "/code/client", Context: "Go"},
		{Name: "Landing", Path: "/code/landing"},
	}}
	lead, err := s.CreateProject(&p)
	if err != nil {
		t.Fatal(err)
	}
	if p.Workdir != "/code/client" {
		t.Fatalf("workdir = %q", p.Workdir)
	}
	landing := p.Folders[1].ID
	lead.FolderID = &landing
	if err := s.UpdateAgent(lead); err != nil {
		t.Fatal(err)
	}

	// ordinea inversata: Landing devine primul, iar workdir urmeaza primul folder
	p.Folders = []Folder{p.Folders[1], p.Folders[0]}
	if err := s.UpdateProject(p); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetProject(p.ID)
	if got.Workdir != "/code/landing" || got.Folders[0].Name != "Landing" || got.Folders[1].Context != "Go" {
		t.Fatalf("dupa reordonare: %+v", got)
	}

	// Landing sters: agentul trece pe primul folder (NULL)
	got.Folders = got.Folders[1:]
	if err := s.UpdateProject(got); err != nil {
		t.Fatal(err)
	}
	a, _ := s.GetAgent(lead.ID)
	if a.FolderID != nil {
		t.Fatalf("agentul trebuia mutat pe primul folder, are %d", *a.FolderID)
	}
	if again, _ := s.GetProject(p.ID); len(again.Folders) != 1 || again.Workdir != "/code/client" {
		t.Fatalf("dupa stergere: %+v", again)
	}
}

func TestAgentFolderMustBeInProject(t *testing.T) {
	s := newStore(t)
	p1, lead := newProject(t, s)
	p2 := Project{Name: "Altul", Workdir: "/tmp/altul"}
	if _, err := s.CreateProject(&p2); err != nil {
		t.Fatal(err)
	}
	foreign := p2.Folders[0].ID
	lead.FolderID = &foreign
	var le LinkError
	if err := s.UpdateAgent(lead); !errors.As(err, &le) {
		t.Fatalf("folder din alt proiect: vreau LinkError, am %v", err)
	}
	own := p1.Folders[0].ID
	lead.FolderID = &own
	if err := s.UpdateAgent(lead); err != nil {
		t.Fatal(err)
	}
}

func TestHomeFolderAndName(t *testing.T) {
	folders := []Folder{{ID: 7, Name: "Client"}, {ID: 9, Name: "Landing"}}
	nine, missing := int64(9), int64(99)
	if HomeFolder(Agent{}, folders).ID != 7 || HomeFolder(Agent{FolderID: &nine}, folders).ID != 9 || HomeFolder(Agent{FolderID: &missing}, folders).ID != 7 {
		t.Fatal("HomeFolder: fara folder sau cu folder inexistent = primul")
	}
	if HomeFolder(Agent{}, nil).ID != 0 {
		t.Fatal("fara foldere: Folder gol")
	}
	for in, want := range map[string]string{"/a/b/probe": "probe", "/a/b/probe/": "probe", "/": "Main", "": "Main"} {
		if got := FolderName(in); got != want {
			t.Errorf("FolderName(%q) = %q, vreau %q", in, got, want)
		}
	}
}

func TestCreateProjectWithTeamFolders(t *testing.T) {
	s := newStore(t)
	p := Project{Name: "Probe", Folders: []Folder{{Name: "Client", Path: "/c"}, {Name: "Landing", Path: "/l"}}}
	agents, err := s.CreateProjectWithTeam(&p, []NewMember{
		{Agent: Agent{Name: "Ana", Model: "sonnet"}, Parent: -1, Reviewer: -1},
		{Agent: Agent{Name: "Lia", Model: "haiku"}, Parent: 0, Reviewer: -1, Folder: 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	if agents[0].FolderID != nil || agents[1].FolderID == nil || *agents[1].FolderID != p.Folders[1].ID {
		t.Fatalf("foldere agenti: %v %v", agents[0].FolderID, agents[1].FolderID)
	}
	if got, _ := s.GetAgent(agents[1].ID); got.FolderID == nil || *got.FolderID != p.Folders[1].ID {
		t.Fatalf("folder salvat: %+v", got.FolderID)
	}
}
```

- [ ] **Step 2: Ruleaza testele si confirma ca esueaza**

Run: `go test ./internal/store/`
Expected: eroare de compilare (`Folders`, `FolderID`, `HomeFolder`, `FolderName`, `NewMember.Folder` nu exista).

- [ ] **Step 3: Migrarea**

`internal/store/migrations/006_folders.sql`:

```sql
-- foldere de lucru per proiect (ex. Client + Landing); primul (position 0) e cel implicit
CREATE TABLE project_folders (
  id         INTEGER PRIMARY KEY,
  project_id INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  position   INTEGER NOT NULL,
  name       TEXT NOT NULL,
  path       TEXT NOT NULL,
  context    TEXT NOT NULL DEFAULT ''
);
CREATE INDEX project_folders_project ON project_folders(project_id, position);

-- fiecare proiect existent primeste folderul lui de lucru. Numele e ultimul
-- segment din cale: rtrim(w, <w fara '/'>) taie numele de la coada si lasa
-- "…/", iar replace scoate acel prefix. "/" sau gol devine "Main".
INSERT INTO project_folders (project_id, position, name, path)
  SELECT id, 0,
    COALESCE(NULLIF(replace(rtrim(workdir, '/'), rtrim(rtrim(workdir, '/'), replace(rtrim(workdir, '/'), '/', '')), ''), ''), 'Main'),
    workdir
  FROM projects;

-- folderul de baza al agentului; NULL = primul folder al proiectului
ALTER TABLE agents ADD COLUMN folder_id INTEGER REFERENCES project_folders(id) ON DELETE SET NULL;
```

- [ ] **Step 4: `internal/store/folders.go`**

```go
package store

import (
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
)

// Folder e un folder de lucru al proiectului (ex. "Client" -> ~/code/probe-client).
// Poate fi repo git sau folder simplu. Primul (Position 0) e cel implicit:
// acolo lucreaza agentii fara folder ales.
type Folder struct {
	ID        int64  `json:"id"`
	ProjectID int64  `json:"projectId"`
	Position  int    `json:"position"`
	Name      string `json:"name"`
	Path      string `json:"path"`
	Context   string `json:"context"`
}

// FolderName e numele implicit al unui folder: ultimul segment din cale.
func FolderName(path string) string {
	base := filepath.Base(strings.TrimRight(filepath.Clean(path), `/\`))
	if base == "." || base == "/" || base == `\` || base == "" {
		return "Main"
	}
	return base
}

// HomeFolder: folderul in care lucreaza agentul. Fara folder ales (sau cu unul
// care nu mai exista) e primul folder al proiectului.
func HomeFolder(a Agent, folders []Folder) Folder {
	if a.FolderID != nil {
		for _, f := range folders {
			if f.ID == *a.FolderID {
				return f
			}
		}
	}
	if len(folders) > 0 {
		return folders[0]
	}
	return Folder{}
}

// ListFolders intoarce folderele proiectului, in ordine.
func (s *Store) ListFolders(projectID int64) ([]Folder, error) {
	return listFolders(s.db, projectID)
}

func listFolders(q querier, projectID int64) ([]Folder, error) {
	rows, err := q.Query(`SELECT id, project_id, position, name, path, context FROM project_folders
		WHERE project_id = ? ORDER BY position, id`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Folder{}
	for rows.Next() {
		var f Folder
		if err := rows.Scan(&f.ID, &f.ProjectID, &f.Position, &f.Name, &f.Path, &f.Context); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// saveFolders inlocuieste folderele proiectului cu lista data, in ordinea ei
// (ca SaveStages). Cele cu ID se actualizeaza, cele fara se creeaza, iar cele
// care lipsesc se sterg; agentii lor trec pe primul folder (folder_id NULL).
// projects.workdir ramane egal cu calea primului folder. Seteaza ID-urile in lista.
func saveFolders(tx *sql.Tx, projectID int64, folders []Folder) error {
	if len(folders) == 0 {
		return errors.New("a project needs at least one folder")
	}
	old, err := listFolders(tx, projectID)
	if err != nil {
		return err
	}
	keep := map[int64]bool{}
	for _, f := range folders {
		if f.ID != 0 {
			keep[f.ID] = true
		}
	}
	for _, o := range old {
		if keep[o.ID] {
			continue
		}
		if _, err := tx.Exec(`UPDATE agents SET folder_id = NULL WHERE folder_id = ?`, o.ID); err != nil {
			return err
		}
		if _, err := tx.Exec(`DELETE FROM project_folders WHERE id = ?`, o.ID); err != nil {
			return err
		}
	}
	for i := range folders {
		f := &folders[i]
		f.ProjectID, f.Position = projectID, i
		if f.ID != 0 {
			if err := mustAffect(tx.Exec(`UPDATE project_folders SET position = ?, name = ?, path = ?, context = ?
				WHERE id = ? AND project_id = ?`, f.Position, f.Name, f.Path, f.Context, f.ID, projectID)); err != nil {
				return err
			}
			continue
		}
		res, err := tx.Exec(`INSERT INTO project_folders (project_id, position, name, path, context) VALUES (?, ?, ?, ?, ?)`,
			projectID, f.Position, f.Name, f.Path, f.Context)
		if err != nil {
			return err
		}
		if f.ID, err = res.LastInsertId(); err != nil {
			return err
		}
	}
	_, err = tx.Exec(`UPDATE projects SET workdir = ? WHERE id = ?`, folders[0].Path, projectID)
	return err
}

// defaultFolders: un proiect creat fara foldere primeste unul din Workdir.
func defaultFolders(p *Project) {
	if len(p.Folders) == 0 {
		p.Folders = []Folder{{Name: FolderName(p.Workdir), Path: p.Workdir}}
	}
	p.Workdir = p.Folders[0].Path
}
```

Observatie: `querier` exista deja in `stages.go` (`Query(...)`); `*sql.Tx` il implementeaza.

- [ ] **Step 5: `projects.go`**

1. In `Project` adauga campul (dupa `CreatedAt`):

```go
	Folders []Folder `json:"folders"` // cel putin unul; primul e cel implicit
```

2. In `CreateProject`, prima linie din functie devine `defaultFolders(p)`, inainte de `created := now()`. Dupa `p.CreatedAt = parseTime(created)` adauga:

```go
	if err := saveFolders(tx, p.ID, p.Folders); err != nil {
		return Agent{}, err
	}
```

3. In `CreateProjectWithTeam`, fa la fel: `defaultFolders(p)` la inceput, apoi `saveFolders(tx, p.ID, p.Folders)` dupa `p.CreatedAt = ...` (cu `return nil, err`). In bucla care insereaza agentii, dupa `a.ProjectID, a.ParentID, a.ReviewerID, a.Detached = p.ID, nil, nil, false`, adauga:

```go
		a.FolderID = nil // index 0 (sau invalid) = primul folder
		if m.Folder > 0 && m.Folder < len(p.Folders) {
			id := p.Folders[m.Folder].ID
			a.FolderID = &id
		}
```

In `NewMember` adauga campul:

```go
	Folder int // index in p.Folders; 0 = primul folder (salvat ca NULL)
```

4. `GetProject` si `ListProjects` incarca folderele:

```go
func (s *Store) GetProject(id int64) (Project, error) {
	p, err := scanProject(s.db.QueryRow(`SELECT `+projectCols+` FROM projects WHERE id = ?`, id))
	if err != nil {
		return p, err
	}
	p.Folders, err = s.ListFolders(p.ID)
	return p, err
}
```

In `ListProjects`, dupa ce bucla `rows.Next()` s-a terminat si `rows.Err()` e verificat, completeaza folderele pentru fiecare proiect. Nu interoga cat timp `rows` e deschis: exista o singura conexiune, deci ar aparea un deadlock.

```go
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	for i := range out {
		if out[i].Folders, err = s.ListFolders(out[i].ID); err != nil {
			return nil, err
		}
	}
	return out, nil
```

5. `UpdateProject` intr-o tranzactie:

```go
// UpdateProject salveaza campurile editabile din UI (nu si paused). Cu
// p.Folders nevid, inlocuieste si folderele (vezi saveFolders).
func (s *Store) UpdateProject(p Project) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if len(p.Folders) > 0 {
		p.Workdir = p.Folders[0].Path
	}
	if err := mustAffect(tx.Exec(`UPDATE projects SET name = ?, goal = ?, context = ?, workdir = ?, max_parallel = ?, budget_usd = ?
		WHERE id = ?`, p.Name, p.Goal, p.Context, p.Workdir, p.MaxParallel, p.BudgetUSD, p.ID)); err != nil {
		return err
	}
	if len(p.Folders) > 0 {
		if err := saveFolders(tx, p.ID, p.Folders); err != nil {
			return err
		}
	}
	return tx.Commit()
}
```

- [ ] **Step 6: `agents.go`**

1. In `Agent`, dupa `PosY`, adauga:

```go
	FolderID *int64 `json:"folderId"` // nil = primul folder al proiectului
```

2. La `agentCols` adauga `, folder_id` la sfarsit. In `scanAgent`, adauga `&a.FolderID` la sfarsitul listei de `Scan`.

3. In `insertAgent`, adauga coloana `folder_id` si valoarea `nullPtr(a.FolderID)`: in lista de coloane dupa `detached`, un `?` in plus, si argumentul dupa `a.Detached`.

4. In `UpdateAgent`, in `SET` adauga `folder_id = ?`, iar printre argumente, inainte de `a.ID`, adauga `nullPtr(a.FolderID)`.

5. In `checkLinks`, inainte de `return nil`:

```go
	if a.FolderID != nil {
		var n int
		q.QueryRow(`SELECT COUNT(*) FROM project_folders WHERE id = ? AND project_id = ?`, *a.FolderID, a.ProjectID).Scan(&n)
		if n != 1 {
			return LinkError("the chosen folder is not in this project")
		}
	}
```

- [ ] **Step 7: Ruleaza testele**

Run: `go test ./internal/store/ && go vet ./... && go test ./...`
Expected: PASS. Testele celorlalte pachete trec neschimbate, pentru ca proiectele create cu `Workdir` primesc automat un folder.

- [ ] **Step 8: Commit**

```bash
git add internal/store
git commit -m "store: foldere de proiect si folderul agentului"
```

---

### Task 3: Prompt: foldere in system prompt, scris restrans, folderul membrilor

**Files:**
- Modify: `internal/prompt/prompt.go` (System, Member, Team)
- Modify: `internal/prompt/settings.go` (Settings, pathRule)
- Test: `internal/prompt/prompt_test.go` (TestSettings), `internal/prompt/folders_test.go` (nou)

**Interfaces:**
- Consumes: `store.Project.Folders`, `store.HomeFolder`, `store.Folder` (Task 2)
- Produces:
  - `func Settings(tools []string, home string) string`: `home` = calea folderului de baza; `""` = scris fara restrictie
  - `Member.Folder string`: numele folderului; gol = nu se afiseaza
  - `System(p, a, l)`: aceeasi semnatura, citeste `p.Folders` si `a.FolderID`

- [ ] **Step 1: Scrie testele**

`internal/prompt/folders_test.go`:

```go
package prompt

import (
	"encoding/json"
	"runtime"
	"slices"
	"strings"
	"testing"

	"foreman/internal/store"
)

func probe() store.Project {
	return store.Project{Name: "Probe", Goal: "Ship v2", Context: "One product, two parts.", Folders: []store.Folder{
		{ID: 1, Name: "Client", Path: "/code/probe-client", Context: "Go + SQLite"},
		{ID: 2, Name: "Landing", Path: "/code/probe-landing"},
	}}
}

func TestSystemListsFolders(t *testing.T) {
	two := int64(2)
	got := System(probe(), store.Agent{Name: "Lia", Role: "Writer", FolderID: &two}, Links{BossName: "Ana"})
	for _, want := range []string{
		"Folders:\n- Client: /code/probe-client (read only)\n- Landing: /code/probe-landing (your folder)",
		"Shared context:\nOne product, two parts.",
		"Context for Client:\nGo + SQLite",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("lipseste %q din:\n%s", want, got)
		}
	}
}

// un proiect cu un singur folder, fara context de folder, are exact prompt-ul de azi
func TestSystemSingleFolderUnchanged(t *testing.T) {
	p := store.Project{Name: "Shop", Goal: "g", Context: "c"}
	before := System(p, store.Agent{Name: "Ana"}, Links{})
	p.Folders = []store.Folder{{ID: 1, Name: "shop", Path: "/code/shop"}}
	if after := System(p, store.Agent{Name: "Ana"}, Links{}); after != before {
		t.Fatalf("prompt schimbat:\n%s\n---\n%s", before, after)
	}
	p.Folders[0].Context = "npm test"
	if got := System(p, store.Agent{Name: "Ana"}, Links{}); strings.Contains(got, "Folders:") || !strings.Contains(got, "Context for shop:\nnpm test") {
		t.Fatalf("un folder cu context:\n%s", got)
	}
}

func TestSettingsScopesWriteToHome(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("cai unix")
	}
	var cfg struct {
		Permissions struct {
			Allow []string `json:"allow"`
		} `json:"permissions"`
	}
	json.Unmarshal([]byte(Settings([]string{"write"}, "/code/probe-landing")), &cfg)
	for _, want := range []string{"Edit(//code/probe-landing/**)", "Write(//code/probe-landing/**)", "NotebookEdit(//code/probe-landing/**)"} {
		if !slices.Contains(cfg.Permissions.Allow, want) {
			t.Errorf("allow nu contine %s: %v", want, cfg.Permissions.Allow)
		}
	}
	if slices.Contains(cfg.Permissions.Allow, "Edit") || slices.Contains(cfg.Permissions.Allow, "Write") {
		t.Errorf("scrisul fara restrictie nu trebuia permis: %v", cfg.Permissions.Allow)
	}
}

func TestTeamShowsFolder(t *testing.T) {
	got := Team([]Member{{Name: "Rook", Role: "Backend Dev", Status: "idle", Folder: "Client"}})
	if !strings.Contains(got, "- Rook: Backend Dev, idle, folder: Client") {
		t.Fatalf("Team:\n%s", got)
	}
}
```

In `prompt_test.go`, in `TestSettings`, inlocuieste `Settings([]string{"git", "write"})` cu `Settings([]string{"git", "write"}, "")`, iar `Settings(nil)` cu `Settings(nil, "")`.

- [ ] **Step 2: Confirma ca esueaza**

Run: `go test ./internal/prompt/`
Expected: eroare de compilare (`Settings` cu 2 argumente, `Member.Folder`).

- [ ] **Step 3: `settings.go`**

Adauga importurile `path/filepath` si `strings`. Inlocuieste `Settings`:

```go
// Settings intoarce JSON-ul pentru --settings. In modul -p, ce lipseste din
// allow sau apare in deny e refuzat automat, fara sa intrebe pe nimeni.
// home e folderul agentului: cu tool-ul write, scrie doar acolo ("" = oriunde).
func Settings(tools []string, home string) string {
	allow := slices.Clone(alwaysAllow)
	for _, name := range ValidTools {
		if !slices.Contains(tools, name) {
			continue
		}
		if name == "write" && home != "" {
			for _, tool := range toolRules["write"] {
				allow = append(allow, tool+"("+pathRule(home)+")")
			}
			continue
		}
		allow = append(allow, toolRules[name]...)
	}
	b, _ := json.Marshal(map[string]any{
		"permissions": map[string]any{"allow": allow, "deny": denyAlways},
	})
	return string(b)
}

// pathRule: tot ce e sub dir, in sintaxa regulilor Claude Code. O cale absoluta
// incepe cu "//" (ex. //Users/ana/shop/**); pe Windows C:\x devine //c/x/**.
func pathRule(dir string) string {
	p := filepath.Clean(dir)
	if v := filepath.VolumeName(p); len(v) == 2 && v[1] == ':' {
		p = "/" + strings.ToLower(v[:1]) + p[2:]
	}
	return "/" + strings.TrimSuffix(filepath.ToSlash(p), "/") + "/**"
}
```

Daca spike-ul din Task 1 a aratat alta forma a regulii, foloseste-o pe aceea in `pathRule`, iar testul `TestSettingsScopesWriteToHome` il ajustezi la fel.

- [ ] **Step 4: `prompt.go`**

1. In `Member` adauga `Folder string` (dupa `Status`): `Name, Role, Responsibilities, Status, Folder string`.

2. In `Team`, lista `parts` include si folderul:

```go
		for _, p := range []string{m.Role, m.Status} {
			if p != "" {
				parts = append(parts, p)
			}
		}
		if m.Folder != "" {
			parts = append(parts, "folder: "+m.Folder)
		}
```

3. In `System`, inlocuieste blocul care construieste `proj`:

```go
	var proj []string
	if g := strings.TrimSpace(p.Goal); g != "" {
		proj = append(proj, "Goal: "+g)
	}
	// cu mai multe foldere, agentul afla unde lucreaza si ce poate doar citi
	if len(p.Folders) > 1 {
		home := store.HomeFolder(a, p.Folders)
		lines := []string{"Folders:"}
		for _, f := range p.Folders {
			access := "read only"
			if f.ID == home.ID {
				access = "your folder"
			}
			lines = append(lines, fmt.Sprintf("- %s: %s (%s)", f.Name, f.Path, access))
		}
		proj = append(proj, strings.Join(lines, "\n"))
	}
	if c := strings.TrimSpace(p.Context); c != "" {
		proj = append(proj, "Shared context:\n"+c)
	}
	for _, f := range p.Folders {
		if c := strings.TrimSpace(f.Context); c != "" {
			proj = append(proj, "Context for "+f.Name+":\n"+c)
		}
	}
	section(&b, "Project", strings.Join(proj, "\n\n"))
```

Pastreaza exact titlurile si textele existente. Daca in cod se numesc altfel decat `"Goal: "`, `"Shared context:\n"` sau `"Project"`, pastreaza-le pe cele din cod si adauga doar liniile noi.

- [ ] **Step 5: Ruleaza testele**

Run: `go test ./internal/prompt/`
Expected: PASS. `go build ./...` va esua in `internal/team/run.go` (`prompt.Settings` cu un argument); se repara in Task 4.

- [ ] **Step 6: Commit** (dupa Task 4, cand totul compileaza; vezi Task 4, Step 6)

---

### Task 4: Team: run-ul agentului in folderul lui

**Files:**
- Modify: `internal/team/run.go` (startRun, args)
- Modify: `internal/team/reconcile.go` (turnContext primeste folderele)
- Test: `internal/team/folders_test.go` (nou)

**Interfaces:**
- Consumes: `store.HomeFolder`, `Project.Folders`, `prompt.Settings(tools, home)`, `prompt.Member.Folder`
- Produces: `func (t *Team) turnContext(a store.Agent, agents []store.Agent, tasks []store.Task, currentTaskID int64, folders []store.Folder) prompt.TurnContext`

- [ ] **Step 1: Scrie testele**

`internal/team/folders_test.go`:

```go
package team

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"foreman/internal/store"
)

// withFolders adauga proiectului fixture-ului un al doilea folder si il muta pe Rook acolo.
func withFolders(t *testing.T, f *fixture) (client, landing store.Folder) {
	t.Helper()
	p, err := f.st.GetProject(f.p.ID)
	if err != nil {
		t.Fatal(err)
	}
	p.Folders[0].Name = "Client"
	p.Folders = append(p.Folders, store.Folder{Name: "Landing", Path: t.TempDir()})
	if err := f.st.UpdateProject(p); err != nil {
		t.Fatal(err)
	}
	p, _ = f.st.GetProject(f.p.ID)
	f.rook.FolderID = &p.Folders[1].ID
	if err := f.st.UpdateAgent(f.rook); err != nil {
		t.Fatal(err)
	}
	return p.Folders[0], p.Folders[1]
}

func TestRunInAgentFolder(t *testing.T) {
	f := setup(t)
	client, landing := withFolders(t, f)
	f.task(t, "Hero section", f.rook, store.StatusQueued)
	mustReconcile(t, f)
	spec := f.fs.last(t, f.rook)
	if spec.Dir != landing.Path {
		t.Fatalf("cwd = %q, vreau %q", spec.Dir, landing.Path)
	}
	if argValue(spec.Args, "--add-dir") != client.Path {
		t.Fatalf("--add-dir = %q, vreau %q", argValue(spec.Args, "--add-dir"), client.Path)
	}
	if s := argValue(spec.Args, "--settings"); !strings.Contains(s, "Write(/"+filepath.ToSlash(landing.Path)+"/**)") {
		t.Fatalf("scrisul trebuia restrans la Landing: %s", s)
	}
	if sys := argValue(spec.Args, "--append-system-prompt"); !strings.Contains(sys, "- Landing: "+landing.Path+" (your folder)") {
		t.Fatalf("system prompt:\n%s", sys)
	}
}

func TestLeadSeesMemberFolders(t *testing.T) {
	f := setup(t)
	withFolders(t, f)
	if _, err := f.tm.UserMessage(f.lead.ID, "Plan v2", nil); err != nil {
		t.Fatal(err)
	}
	mustReconcile(t, f)
	got := turn(f.fs.last(t, f.lead))
	if !strings.Contains(got, "- Rook: Backend Dev") || !strings.Contains(got, "folder: Landing") || !strings.Contains(got, "folder: Client") {
		t.Fatalf("tura lead-ului:\n%s", got)
	}
}

func TestRunFailsWhenFolderMissing(t *testing.T) {
	f := setup(t)
	_, landing := withFolders(t, f)
	os.RemoveAll(landing.Path)
	task := f.task(t, "Hero", f.rook, store.StatusQueued)
	f.tm.Reconcile(f.p.ID) // eroarea de pornire ajunge ca nota, nu ca panica
	msgs, _ := f.st.ListMessages(f.lead.ID)
	found := false
	for _, m := range msgs {
		if strings.Contains(m.Text, "Landing") && strings.Contains(m.Text, "does not exist") {
			found = true
		}
	}
	if !found {
		t.Fatalf("nicio nota cu folderul lipsa; task %s, mesaje: %+v", task.Ref(), msgs)
	}
}
```

Erorile de pornire ajung ca nota in thread-ul primei radacini (`startFailed` din `reconcile.go`, care pune si proiectul pe pauza), deci testul le cauta cu `ListMessages(f.lead.ID)`.

- [ ] **Step 2: Confirma ca esueaza**

Run: `go test ./internal/team/`
Expected: eroare de compilare in `run.go` (`prompt.Settings`) sau teste esuate.

- [ ] **Step 3: `run.go`**

In `startRun`, inlocuieste verificarea folderului si inceputul lui `extraArgs`:

```go
	home := store.HomeFolder(a, p.Folders)
	if info, err := os.Stat(home.Path); err != nil || !info.IsDir() {
		return fmt.Errorf("the folder %s (%s) does not exist", home.Name, home.Path)
	}

	// celelalte foldere ale proiectului: agentul le poate citi (scrie doar in al lui)
	var extraArgs []string
	for _, f := range p.Folders {
		if f.ID != home.ID {
			extraArgs = append(extraArgs, "--add-dir", f.Path)
		}
	}
	// fisierele proiectului: agentul are voie sa citeasca folderul si le vede in tura
	if dir := t.filesDir(p.ID); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		extraArgs = append(extraArgs, "--add-dir", dir)
```

Restul blocului de fisiere ramane neschimbat. Apoi:
- `hash := prompt.Hash(p.Workdir + "\n" + system)` devine `hash := prompt.Hash(home.Path + "\n" + system)`;
- in `runner.Spec`, `Dir: p.Workdir` devine `Dir: home.Path`;
- in `args`, `prompt.Settings(a.Tools)` devine `prompt.Settings(a.Tools, store.HomeFolder(a, p.Folders).Path)`.

Mesajul din `t.note(..., "The instructions or the working folder changed; starting a new session.", true)` ramane.

- [ ] **Step 4: `reconcile.go`**

Semnatura noua: `func (t *Team) turnContext(a store.Agent, agents []store.Agent, tasks []store.Task, currentTaskID int64, folders []store.Folder) prompt.TurnContext`. In bucla peste copii:

```go
	for _, child := range store.Children(a.ID, agents) {
		m := prompt.Member{
			Name: child.Name, Role: child.Role, Responsibilities: child.Responsibilities,
			Status: t.statusOf(child, tasks, false),
		}
		if len(folders) > 1 { // cu un singur folder nu adauga nimic
			m.Folder = store.HomeFolder(child, folders).Name
		}
		c.Direct = append(c.Direct, m)
	}
```

La apelul din `ctxFor`: `c := t.turnContext(a, agents, tasks, currentTaskID, p.Folders)`. Asigura-te ca `p` e proiectul incarcat cu `GetProject` in `Reconcile`, deci cu folderele. Daca `p` vine din alta sursa, incarca-l cu `t.st.GetProject`.

- [ ] **Step 5: Ruleaza tot**

Run: `go vet ./... && go test ./...`
Expected: PASS.

- [ ] **Step 6: Commit** (include si Task 3)

```bash
git add internal/prompt internal/team
git commit -m "agentii lucreaza in folderul lor: cwd, --add-dir, scris restrans, folderele in prompt"
```

---

### Task 5: Web API: foldere in proiect, folderul agentului, "Open in" pe folder

**Files:**
- Modify: `internal/web/api_projects.go` (projectInput, toProject, updateProject, createProject)
- Modify: `internal/web/api_agents.go` (agentInput.FolderID, promptPreview)
- Modify: `internal/web/api_open.go` (folderId)
- Test: `internal/web/folders_test.go` (nou)

**Interfaces:**
- Consumes: `store.Folder`, `store.FolderName`, `Project.Folders`, `Agent.FolderID`
- Produces (JSON):
  - `POST/PUT /api/projects[/{id}]` primesc `folders: [{id?, name, path, context}]`; fara `folders`, se foloseste `workdir`, ca inainte
  - `PUT /api/agents/{id}` si `POST /api/projects/{id}/agents` primesc `folderId` (null = primul folder)
  - `POST /api/prompt-preview` primeste `folderId`
  - `POST /api/projects/{id}/open` primeste `folderId` (optional)
  - `GET /api/projects/{id}` intoarce `folders`, iar agentii au `folderId`

- [ ] **Step 1: Scrie testele**

`internal/web/folders_test.go`:

```go
package web

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"foreman/internal/store"
)

type folderDetail struct {
	Workdir string         `json:"workdir"`
	Folders []store.Folder `json:"folders"`
	Agents  []struct {
		ID       int64  `json:"id"`
		FolderID *int64 `json:"folderId"`
	} `json:"agents"`
}

func (e *env) folders(t *testing.T, id int64) folderDetail {
	t.Helper()
	_, raw := e.do(t, "GET", "/api/projects/"+itoa(id), nil)
	var d folderDetail
	json.Unmarshal(raw, &d)
	return d
}

func TestProjectFolders(t *testing.T) {
	e := setup(t)
	client, landing := t.TempDir(), filepath.Join(t.TempDir(), "probe-landing")
	code, raw := e.do(t, "POST", "/api/projects", map[string]any{"name": "Probe", "folders": []map[string]any{
		{"name": "Client", "path": client, "context": "Go"},
		{"name": "", "path": landing}, // fara nume: numele folderului; inexistent: se creeaza
	}})
	if code != http.StatusCreated {
		t.Fatalf("%d %s", code, raw)
	}
	var p store.Project
	json.Unmarshal(raw, &p)
	d := e.folders(t, p.ID)
	if len(d.Folders) != 2 || d.Folders[1].Name != "probe-landing" || d.Workdir != client || d.Folders[0].Context != "Go" {
		t.Fatalf("foldere: %+v", d)
	}

	for _, bad := range [][]map[string]any{
		{{"name": "A", "path": client}, {"name": "a", "path": landing}},
		{{"name": "A", "path": client}, {"name": "B", "path": client}},
	} {
		if code, raw := e.do(t, "POST", "/api/projects", map[string]any{"name": "X", "folders": bad}); code != 400 {
			t.Errorf("duplicate: %d %s", code, raw)
		}
	}

	// agentul in Landing, apoi Landing sters: agentul revine pe primul folder
	lead := d.Agents[0].ID
	landingID := d.Folders[1].ID
	body := map[string]any{"name": "Lead", "model": "sonnet", "tools": []string{}, "folderId": landingID}
	if code, raw := e.do(t, "PUT", "/api/agents/"+itoa(lead), body); code != 200 {
		t.Fatalf("folderId: %d %s", code, raw)
	}
	if d := e.folders(t, p.ID); d.Agents[0].FolderID == nil || *d.Agents[0].FolderID != landingID {
		t.Fatalf("folderId salvat: %+v", d.Agents)
	}
	code, raw = e.do(t, "PUT", "/api/projects/"+itoa(p.ID), map[string]any{"name": "Probe", "folders": []map[string]any{
		{"id": d.Folders[0].ID, "name": "Client", "path": client},
	}})
	if code != 200 {
		t.Fatalf("update: %d %s", code, raw)
	}
	if d := e.folders(t, p.ID); len(d.Folders) != 1 || d.Agents[0].FolderID != nil {
		t.Fatalf("dupa stergerea folderului: %+v", d)
	}

	// un id de folder care nu e al proiectului: 400
	if code, _ := e.do(t, "PUT", "/api/projects/"+itoa(p.ID), map[string]any{"name": "Probe", "folders": []map[string]any{
		{"id": 99999, "name": "X", "path": client},
	}}); code != 400 {
		t.Fatalf("folder necunoscut: %d", code)
	}
}

// clientii vechi trimit doar workdir: folderele in plus raman
func TestUpdateProjectWorkdirOnlyKeepsFolders(t *testing.T) {
	e := setup(t)
	a, b := t.TempDir(), t.TempDir()
	_, raw := e.do(t, "POST", "/api/projects", map[string]any{"name": "Probe", "folders": []map[string]any{
		{"name": "A", "path": a}, {"name": "B", "path": b},
	}})
	var p store.Project
	json.Unmarshal(raw, &p)
	if code, raw := e.do(t, "PUT", "/api/projects/"+itoa(p.ID), map[string]any{"name": "Probe 2", "workdir": ""}); code != 200 {
		t.Fatalf("%d %s", code, raw)
	}
	if d := e.folders(t, p.ID); len(d.Folders) != 2 || d.Folders[1].Path != b {
		t.Fatalf("foldere pierdute: %+v", d.Folders)
	}
}

func TestAgentFolderFromOtherProject(t *testing.T) {
	e := setup(t)
	p1 := e.createProject(t, t.TempDir())
	p2 := e.createProject(t, t.TempDir())
	foreign := e.folders(t, p2.ID).Folders[0].ID
	lead := e.root(t, p1.ID)
	code, raw := e.do(t, "PUT", "/api/agents/"+itoa(lead.ID), map[string]any{"name": "Lead", "model": "sonnet", "tools": []string{}, "folderId": foreign})
	if code != 400 || !strings.Contains(string(raw), "folder") {
		t.Fatalf("%d %s", code, raw)
	}
}

func TestOpenChosenFolder(t *testing.T) {
	e := setup(t)
	fo := &fakeOpener{}
	e.srv.SetOpener(fo)
	a, b := t.TempDir(), t.TempDir()
	_, raw := e.do(t, "POST", "/api/projects", map[string]any{"name": "Probe", "folders": []map[string]any{
		{"name": "A", "path": a}, {"name": "B", "path": b},
	}})
	var p store.Project
	json.Unmarshal(raw, &p)
	second := e.folders(t, p.ID).Folders[1].ID
	path := "/api/projects/" + itoa(p.ID) + "/open"
	if code, _ := e.do(t, "POST", path, map[string]any{"target": "folder", "folderId": second}); code != http.StatusNoContent {
		t.Fatalf("open: %d", code)
	}
	if code, _ := e.do(t, "POST", path, map[string]any{"target": "folder"}); code != http.StatusNoContent {
		t.Fatalf("open implicit: %d", code)
	}
	if len(fo.calls) != 2 || fo.calls[0] != "folder:"+b || fo.calls[1] != "folder:"+a {
		t.Fatalf("calls: %v", fo.calls)
	}
	if code, _ := e.do(t, "POST", path, map[string]any{"target": "folder", "folderId": 99999}); code != 400 {
		t.Fatalf("folder necunoscut: %d", code)
	}
}
```

- [ ] **Step 2: Confirma ca esueaza**

Run: `go test ./internal/web/ -run 'Folder|Workdir'`
Expected: FAIL (folderele nu se salveaza, `folderId` e ignorat).

- [ ] **Step 3: `api_projects.go`**

Adauga importul `fmt`. Tipuri si validare:

```go
type folderInput struct {
	ID      int64  `json:"id"`
	Name    string `json:"name"`
	Path    string `json:"path"`
	Context string `json:"context"`
}

const maxFolders = 6

type projectInput struct {
	Name        string        `json:"name"`
	Goal        string        `json:"goal"`
	Context     string        `json:"context"`
	Workdir     string        `json:"workdir"` // API-ul vechi: un singur folder
	Folders     []folderInput `json:"folders"`
	MaxParallel int           `json:"maxParallel"`
	BudgetUSD   float64       `json:"budgetUsd"`
}

// folders valideaza folderele si le creeaza pe disc pe cele lipsa (ultimul
// pas). Fara lista, un singur folder din Workdir, ca inainte.
func (in projectInput) folders(projectName, workspaces string) ([]store.Folder, error) {
	list := in.Folders
	if len(list) == 0 {
		list = []folderInput{{Path: in.Workdir}}
	}
	if len(list) > maxFolders {
		return nil, badRequest(fmt.Sprintf("at most %d folders", maxFolders))
	}
	out := make([]store.Folder, 0, len(list))
	names := map[string]bool{}
	for _, f := range list {
		name, ctx := strings.TrimSpace(f.Name), strings.TrimSpace(f.Context)
		for _, err := range []error{maxLen("folder name", name, 40), maxLen("folder context", ctx, 20000)} {
			if err != nil {
				return nil, err
			}
		}
		// numele date se verifica inainte sa cream ceva pe disc
		if name != "" {
			if names[strings.ToLower(name)] {
				return nil, badRequest("two folders named " + name)
			}
			names[strings.ToLower(name)] = true
		}
		out = append(out, store.Folder{ID: f.ID, Name: name, Path: strings.TrimSpace(f.Path), Context: ctx})
	}
	paths := map[string]bool{}
	for i := range out {
		f := &out[i]
		hint := projectName // un folder nou in ~/Foreman: slug din proiect (+ numele folderului)
		if i > 0 {
			hint += " " + f.Name
		}
		dir, err := resolveWorkdir(f.Path, hint, workspaces)
		if err != nil {
			return nil, err
		}
		f.Path = dir
		if paths[dir] {
			return nil, badRequest("two folders with the path " + dir)
		}
		paths[dir] = true
		if f.Name == "" {
			f.Name = store.FolderName(dir)
			if names[strings.ToLower(f.Name)] {
				return nil, badRequest("two folders named " + f.Name + "; give one of them another name")
			}
			names[strings.ToLower(f.Name)] = true
		}
	}
	return out, nil
}
```

In `toProject`, inlocuieste ultimul pas (`resolveWorkdir(in.Workdir, ...)`) cu:

```go
	// ultimul pas: creeaza folderele doar dupa ce restul e valid
	folders, err := in.folders(p.Name, workspaces)
	if err != nil {
		return p, err
	}
	p.Folders, p.Workdir = folders, folders[0].Path
	return p, nil
```

In `createProject`, orice `id` venit din client e ignorat. Inainte de `in.toProject`:

```go
	for i := range in.Folders {
		in.Folders[i].ID = 0 // proiect nou: toate folderele sunt noi
	}
```

In `updateProject`, inlocuieste blocul `if strings.TrimSpace(in.Workdir) == "" { ... }` cu:

```go
	if len(in.Folders) == 0 {
		// API-ul vechi (doar workdir): folderele raman; un workdir nevid schimba primul folder
		for _, f := range existing.Folders {
			in.Folders = append(in.Folders, folderInput{ID: f.ID, Name: f.Name, Path: f.Path, Context: f.Context})
		}
		if w := strings.TrimSpace(in.Workdir); w != "" && len(in.Folders) > 0 {
			in.Folders[0].Path = w
		}
	}
	known := map[int64]bool{}
	for _, f := range existing.Folders {
		known[f.ID] = true
	}
	for _, f := range in.Folders {
		if f.ID != 0 && !known[f.ID] {
			fail(w, badRequest("unknown folder; reload the page"))
			return
		}
	}
```

Dupa `s.st.UpdateProject(p)`, raspunsul contine proiectul reincarcat, cu ID-urile folderelor noi:

```go
	saved, err := s.st.GetProject(id)
	if err != nil {
		fail(w, err)
		return
	}
	s.hub.Publish(id, "project", map[string]int64{"id": id})
	s.team.Kick(id)
	writeJSON(w, http.StatusOK, saved)
```

- [ ] **Step 4: `api_agents.go`**

In `agentInput` adauga:

```go
	FolderID *int64 `json:"folderId"` // null sau lipsa = primul folder al proiectului
```

In `apply`, dupa `a.ParentID, a.ReviewerID = ...`, adauga `a.FolderID = in.FolderID`. Store-ul intoarce `LinkError` pentru un folder din alt proiect, iar `fail` il transforma deja in 400.

In `promptPreview`, adauga `FolderID *int64 \`json:"folderId"\`` in struct-ul de input si `FolderID: in.FolderID` in `store.Agent{...}`.

- [ ] **Step 5: `api_open.go`**

In struct-ul de input adauga `FolderID *int64 \`json:"folderId"\``. Dupa decode, inlocuieste folosirea lui `p.Workdir` cu `dir`:

```go
	folder := store.HomeFolder(store.Agent{FolderID: in.FolderID}, p.Folders)
	if in.FolderID != nil && folder.ID != *in.FolderID {
		fail(w, badRequest("unknown folder"))
		return
	}
	dir := folder.Path
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		fail(w, badRequest("the folder does not exist: "+dir))
		return
	}
```

Toate aparitiile `p.Workdir` din `switch` devin `dir`. Adauga importul `foreman/internal/store`.

- [ ] **Step 6: Ruleaza tot**

Run: `go vet ./... && go test ./...`
Expected: PASS. Daca un test vechi verifica textul „the working folder does not exist”, actualizeaza-l la „the folder does not exist”.

- [ ] **Step 7: Commit**

```bash
git add internal/web
git commit -m "api: foldere per proiect, folderId pe agent si la Open in"
```

---

### Task 6: UI: formularul proiectului, editorul de agent, canvas, board, "Open in"

**Files:**
- Modify: `internal/web/static/js/ui.js` (agentBody)
- Modify: `internal/web/static/js/projects.js` (projectFormView: editorul de foldere)
- Modify: `internal/web/static/js/agent.js` (select Folder, preview)
- Modify: `internal/web/static/js/canvas.js` (folderul pe nod)
- Modify: `internal/web/static/js/workspace.js` (filtru pe board, folder la "Open in")
- Modify: `internal/web/static/app.css`

**Interfaces:**
- Consumes: `project.folders`, `agent.folderId` si API-ul din Task 5.
- Produces: `folderOf(project, agent)` in `ui.js`, care intoarce folderul agentului (sau primul).

- [ ] **Step 1: `ui.js`**

In `agentBody`, adauga `folderId: a.folderId ?? null,` dupa `detached: !!a.detached,`. Fara el, orice salvare (din editor sau dintr-o legatura trasa pe canvas) muta agentul in primul folder.

Adauga si helper-ul:

```js
// folderOf: folderul in care lucreaza agentul (fara folder ales: primul).
export function folderOf(project, a) {
  const folders = project.folders || [];
  return folders.find((f) => f.id === a?.folderId) || folders[0] || null;
}
```

- [ ] **Step 2: `projects.js`: editorul de foldere**

In `projectFormView`, sterge `workdir`, `pickBtn`, `folderField` si `pickFolder`. Proiectul nou primeste `folders: [{ id: 0, name: "", path: "", context: "" }]`. Adauga:

```js
  // foldere: un proiect poate avea mai multe parti (ex. Client + Landing); primul e implicit
  const folders = foldersEditor(p.folders?.length ? p.folders : [{ id: 0, name: "", path: "", context: "" }],
    canPick, p.agents || []);
```

(`p` e rezultatul lui `GET /api/projects/{id}`, deci contine `agents` cu `folderId`.)

Sectiunea noua, dupa `el("div", { class: "form-cols" }, ...)`, in acelasi `form`:

```js
    el("section", { class: "section" },
      el("div", { class: "section-head" },
        el("h2", {}, "Folders"),
        el("button", { type: "button", class: "btn small", onclick: () => folders.add() }, "+ Add folder")),
      el("p", { class: "hint" }, "A folder can be a repo or a plain folder. Each agent works and writes in its own folder and can read the others. The first folder is the default."),
      folders.el)
```

In `save`, `body` primeste `folders: folders.values()` in loc de `workdir: workdir.value`.

Functia, la nivel de modul:

```js
// foldersEditor: randurile Name / Path / Context din formularul proiectului.
// agents: pentru avertismentul de la stergerea unui folder care are agenti.
function foldersEditor(initial, canPick, agents) {
  const box = el("div", { class: "folder-rows" });
  const rows = [];

  function add(f = { id: 0, name: "", path: "", context: "" }) {
    const name = el("input", { type: "text", value: f.name, maxlength: 40, placeholder: "e.g. Client" });
    const path = el("input", { type: "text", value: f.path,
      placeholder: rows.length ? "e.g. ~/code/probe-landing" : "empty = ~/Foreman/<project name>" });
    const context = el("textarea", { value: f.context, rows: 3, maxlength: 20000,
      placeholder: "Stack, build/test commands, conventions for this folder." });
    const pickBtn = canPick && el("button", { type: "button", class: "btn", onclick: () => pickInto(path, pickBtn) }, "Browse...");
    const r = { id: f.id || 0, name, path, context };
    r.removeBtn = el("button", { type: "button", class: "btn small danger", onclick: () => remove(r) }, "Remove folder");
    r.el = el("div", { class: "folder-row" },
      el("div", { class: "row" }, field("Name", name), field("Path", el("div", { class: "input-row" }, path, pickBtn))),
      field("Context for this folder", context),
      el("div", { class: "actions" }, el("span", { class: "grow" }), r.removeBtn));
    rows.push(r);
    draw();
    if (rows.length > 1) name.focus();
  }

  function remove(r) {
    if (rows.length === 1) return;
    const n = r.id ? agents.filter((a) => a.folderId === r.id).length : 0;
    const label = r.name.value.trim() || r.path.value.trim() || "this folder";
    if (n && !confirm(`${n === 1 ? "1 agent works" : n + " agents work"} in ${label}. They will move to the first folder. Remove it?`)) return;
    rows.splice(rows.indexOf(r), 1);
    draw();
  }

  function draw() {
    box.replaceChildren(...rows.map((r) => r.el));
    for (const r of rows) r.removeBtn.hidden = rows.length === 1;
  }

  initial.forEach((f) => add(f));
  return {
    el: box,
    add: () => add(),
    values: () => rows.map((r) => ({ id: r.id, name: r.name.value.trim(), path: r.path.value.trim(), context: r.context.value })),
  };
}

// pickInto deschide dialogul nativ si pune calea aleasa in input.
async function pickInto(input, btn) {
  btn.disabled = true;
  btn.textContent = "Opening...";
  try {
    const r = await api("/api/pick-folder", { method: "POST" });
    if (r && r.path) input.value = r.path; // 204 = a renuntat
  } catch (err) {
    toast(err.message);
  } finally {
    btn.disabled = false;
    btn.textContent = "Browse...";
  }
}
```

Pastreaza `name.focus()` de la sfarsitul lui `projectFormView`. Apelul `initial.forEach(add)` nu trebuie sa fure focusul: `add` da focus doar randurilor adaugate dupa primul. Asta muta totusi focusul pe ultimul rand la incarcarea unui proiect cu 2 foldere; ca sa eviti, apeleaza `name.focus()` din `projectFormView` dupa crearea editorului, cum e deja la final.

- [ ] **Step 3: `agent.js`: select Folder**

Dupa `reviewerSelect`:

```js
  // folderul in care lucreaza agentul; apare doar cand proiectul are mai multe
  const folders = project.folders || [];
  const folderSelect = folders.length > 1 && el("select", { value: String(a.folderId ?? folders[0].id) },
    folders.map((f) => el("option", { value: f.id }, f.name)));
  // primul folder se salveaza ca null (= implicit), ca in store
  const folderValue = () => {
    if (!folderSelect) return a.folderId ?? null;
    const id = Number(folderSelect.value);
    return id === folders[0].id ? null : id;
  };
```

In formular, dupa randul `Reports to` / `Reviewed by`:

```js
    folderSelect && field("Folder", folderSelect, "The agent works and writes here; it can read the other folders."),
```

In `save`, adauga `folderId: folderValue(),` in patch-ul lui `agentBody`. In `updatePreview`, adauga `folderId: folderValue(),` in body.

- [ ] **Step 4: `canvas.js`: folderul pe nod**

Importa `folderOf` din `./ui.js`. In `agentNode`, linia cu rolul devine:

```js
        svg("text", { class: "sub", x: 46, y: 40 }, shorten([a.role, (project.folders || []).length > 1 ? folderOf(project, a)?.name : ""].filter(Boolean).join(" \u00b7 "))),
```

- [ ] **Step 5: `workspace.js`: filtru pe board si "Open in"**

1. Importa `folderOf` din `./ui.js`. In `state`, adauga `folderFilter: null, // id-ul folderului filtrat pe board; null = toate`.

2. Dupa `const boardTools = ...`, adauga `const folderChips = el("div", { class: "folder-chips", role: "group", "aria-label": "Filter by folder" });` si pune `folderChips` in `bodies.tasks`, intre `editorSlot` si `board`.

3. La inceputul lui `renderBoard`, dupa `const stages = p.stages || [];`:

```js
    const folders = p.folders || [];
    if (!folders.some((f) => f.id === state.folderFilter)) state.folderFilter = null;
    folderChips.hidden = folders.length < 2;
    const chip = (id, label) => el("button", {
      type: "button", class: "btn small" + (state.folderFilter === id ? " active" : ""),
      "aria-pressed": String(state.folderFilter === id),
      onclick: () => { state.folderFilter = id; renderBoard(); },
    }, label);
    folderChips.replaceChildren(chip(null, "All"), ...folders.map((f) => chip(f.id, f.name)));
    // task-urile fara agent apar mereu; celelalte dupa folderul agentului
    const tasks = p.tasks.filter((t) => !state.folderFilter || !t.assigneeId ||
      folderOf(p, byId.get(t.assigneeId))?.id === state.folderFilter);
```

Apoi, in restul lui `renderBoard`, **toate** aparitiile lui `p.tasks` devin `tasks`: coloanele implicite, `inStage`, `loose`, „With lead” si „Done”. `count.textContent` ramane pe `p.done` si `p.total`.

4. „Open in”: in `buildOpenBar`, creeaza un select de folder, tinut in afara functiei ca sa-l poata actualiza `renderHeader`:

```js
  const openFolder = el("select", { class: "select-small", "aria-label": "Folder to open" });
```

Pune-l inainte de `buildOpenBar(...)`, in `workspaceView`. In `buildOpenBar`, adauga `openFolder` imediat dupa `el("span", { class: "hint" }, "Open in:")`. In `renderHeader`, la inceput:

```js
    const folders = p.folders || [];
    const keep = openFolder.value;
    openFolder.replaceChildren(...folders.map((f) => el("option", { value: f.id }, f.name)));
    if (folders.some((f) => String(f.id) === keep)) openFolder.value = keep;
    openFolder.hidden = folders.length < 2;
```

In `openIn`, body-ul devine `{ target, editor, folderId: openFolder.hidden ? null : Number(openFolder.value) }`.

- [ ] **Step 6: CSS**

La sfarsitul lui `app.css`:

```css
/* foldere de proiect */
.folder-rows { display: grid; gap: 12px; }
.folder-row { border: 1px solid var(--line); border-radius: 10px; padding: 12px; display: grid; gap: 10px; background: var(--card-2); }
.folder-chips { display: flex; gap: 6px; flex-wrap: wrap; }
```

Stilul pentru chip-ul activ exista deja: `.btn.active`, folosit de butoanele `Columns` si `+ Task`.

- [ ] **Step 7: Verificare**

Run: `for f in internal/web/static/js/*.js; do node --check $f; done && go test ./internal/web/`
Expected: fara erori. Verificarea in browser se face in Task 9.

- [ ] **Step 8: Commit**

```bash
git add internal/web/static
git commit -m "ui: foldere in formularul proiectului, folder per agent, filtru pe board, Open in pe folder"
```

---

### Task 7: Wizard backend: mai multe foldere

**Files:**
- Modify: `internal/setup/manager.go` (Session.Workdirs, Start, args, propose, titleFor)
- Modify: `internal/setup/prompt.go` (systemPrompt, turnPrompt)
- Modify: `internal/setup/proposal.go` (ProposedFolder, Folders, Folder, ValidateProposal, CheckFolderPaths, Members)
- Modify: `internal/mcp/tools.go` (schema propose_setup)
- Modify: `internal/web/api_setup.go` (startSetup, createProjectFromSetup)
- Test: `internal/setup/manager_test.go`, `internal/setup/proposal_test.go`, `internal/web/setup_test.go`

**Interfaces:**
- Consumes: `store.Folder`, `store.FolderName`, `NewMember.Folder`, `resolveWorkdir`
- Produces:
  - `func (m *Manager) Start(workdirs []string, description, model string) (Session, error)`
  - `Session.Workdirs []string` (JSON `workdirs`), care inlocuieste `Workdir`
  - `type ProposedFolder struct { Path, Name, Context string }`, `Proposal.Folders`, `ProposedAgent.Folder`
  - `func CheckFolderPaths(p *Proposal, workdirs []string) error`
  - `POST /api/setup` primeste `workdirs` (si `workdir`, pentru compatibilitate)

- [ ] **Step 1: Scrie testele**

In `proposal_test.go` adauga:

```go
func TestProposalFolders(t *testing.T) {
	p := valid()
	p.Folders = []ProposedFolder{{Path: "/c/client", Name: "Client", Context: "Go"}, {Path: "/c/landing/"}}
	p.Agents[1].Folder = "landing" // numele implicit, fara diferenta de litere
	if err := ValidateProposal(p, true); err != nil {
		t.Fatal(err)
	}
	if p.Folders[1].Name != "landing" {
		t.Fatalf("nume implicit: %+v", p.Folders[1])
	}
	if m := p.Members(); m[0].Folder != 0 || m[1].Folder != 1 {
		t.Fatalf("members: %+v", m)
	}
	for want, mutate := range map[string]func(p *Proposal){
		"two folders named":             func(p *Proposal) { p.Folders[1].Name = "CLIENT" },
		"does not exist in the proposal": func(p *Proposal) { p.Agents[1].Folder = "Zed" },
	} {
		q := valid()
		q.Folders = []ProposedFolder{{Path: "/c/client", Name: "Client"}, {Path: "/c/landing", Name: "Landing"}}
		mutate(q)
		if err := ValidateProposal(q, true); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: am %v", want, err)
		}
	}
}

func TestCheckFolderPaths(t *testing.T) {
	two := []string{"/c/client", "/c/landing"}
	ok := &Proposal{Folders: []ProposedFolder{{Path: "/c/landing/"}, {Path: "/c/client"}}}
	if err := CheckFolderPaths(ok, two); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []*Proposal{
		{},
		{Folders: []ProposedFolder{{Path: "/c/client"}}},
		{Folders: []ProposedFolder{{Path: "/c/client"}, {Path: "/c/other"}}},
		{Folders: []ProposedFolder{{Path: "/c/client"}, {Path: "/c/client"}}},
	} {
		if err := CheckFolderPaths(bad, two); err == nil {
			t.Errorf("trebuia refuzat: %+v", bad.Folders)
		}
	}
	// un singur folder: folders poate lipsi sau avea calea lui
	if err := CheckFolderPaths(&Proposal{}, []string{"/c/a"}); err != nil {
		t.Fatal(err)
	}
	if err := CheckFolderPaths(&Proposal{Folders: []ProposedFolder{{Path: "/c/b"}}}, []string{"/c/a"}); err == nil {
		t.Fatal("calea gresita trebuia refuzata")
	}
	if err := CheckFolderPaths(&Proposal{Folders: []ProposedFolder{{Name: "Main"}}}, nil); err != nil {
		t.Fatal(err)
	}
}
```

In `manager_test.go`, actualizeaza apelurile existente: `m.Start(dir, ...)` devine `m.Start([]string{dir}, ...)`, `m.Start("", ...)` devine `m.Start(nil, ...)`, iar `m.Start(filepath.Join(...), ...)` devine `m.Start([]string{filepath.Join(...)}, ...)`. Apoi adauga:

```go
func TestManagerSeveralFolders(t *testing.T) {
	m, f := newTest(t)
	a, b := t.TempDir(), t.TempDir()
	os.WriteFile(filepath.Join(a, "go.mod"), []byte("module a"), 0o644)
	s, err := m.Start([]string{a, " ", b, a}, "", "sonnet") // gol si dublura ignorate
	if err != nil {
		t.Fatal(err)
	}
	spec := f.specs[0]
	if spec.Dir != a || !hasArgPair(spec.Args, "--add-dir", b) || len(s.Workdirs) != 2 || s.Title != filepath.Base(a) {
		t.Fatalf("spec: dir=%s args=%v session=%+v", spec.Dir, spec.Args, s)
	}
	if !strings.Contains(spec.Prompt, "- "+a) || !strings.Contains(spec.Prompt, "- "+b) {
		t.Fatalf("turn prompt:\n%s", spec.Prompt)
	}
	// analistul trebuie sa propuna exact folderele alese
	bk := m.Wrap(innerBackend{})
	c, _ := bk.Resolve(tokenOf(t, spec))
	if _, err := bk.ProposeSetup(c, json.RawMessage(good)); err == nil || !strings.Contains(err.Error(), "folders") {
		t.Fatalf("fara folders trebuia refuzat: %v", err)
	}
	withFolders := strings.Replace(good, `"agents"`, `"folders":[{"path":"`+a+`","name":"Client"},{"path":"`+b+`","name":"Landing"}],"agents"`, 1)
	if _, err := bk.ProposeSetup(c, json.RawMessage(withFolders)); err != nil {
		t.Fatal(err)
	}
}

func hasArgPair(args []string, flag, value string) bool {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == flag && args[i+1] == value {
			return true
		}
	}
	return false
}
```

In `internal/web/setup_test.go` adauga:

```go
func TestCreateProjectFromSetupWithFolders(t *testing.T) {
	e := withSetup(t)
	client, landing := t.TempDir(), t.TempDir()
	proposal := map[string]any{
		"name": "Probe", "goal": "g", "context": "c",
		"folders": []map[string]any{{"path": client, "name": "Client", "context": "Go"}, {"path": landing, "name": "Landing"}},
		"agents": []map[string]any{
			{"name": "Ana", "role": "Lead", "model": "sonnet", "reportsTo": ""},
			{"name": "Lia", "role": "Web", "model": "haiku", "reportsTo": "Ana", "folder": "Landing"},
		},
	}
	code, raw := e.do(t, "POST", "/api/projects/setup", map[string]any{"proposal": proposal})
	var p struct{ ID int64 }
	json.Unmarshal(raw, &p)
	if code != 201 {
		t.Fatalf("%d %s", code, raw)
	}
	d := e.folders(t, p.ID)
	if len(d.Folders) != 2 || d.Folders[1].Path != landing || d.Folders[0].Context != "Go" {
		t.Fatalf("foldere: %+v", d.Folders)
	}
	var lia *int64
	for _, a := range d.Agents {
		if a.FolderID != nil {
			lia = a.FolderID
		}
	}
	if lia == nil || *lia != d.Folders[1].ID {
		t.Fatalf("Lia trebuia in Landing: %+v", d.Agents)
	}
}
```

(`e.folders` vine din `folders_test.go`, Task 5.)

- [ ] **Step 2: Confirma ca esueaza**

Run: `go test ./internal/setup/ ./internal/web/`
Expected: eroare de compilare (`Start([]string...)`, `ProposedFolder`, `CheckFolderPaths`).

- [ ] **Step 3: `proposal.go`**

Adauga importul `path/filepath`. Tipuri:

```go
// ProposedFolder: un folder al proiectului, cu numele si contextul propuse.
type ProposedFolder struct {
	Path    string `json:"path"`
	Name    string `json:"name"`
	Context string `json:"context"`
}
```

In `Proposal`, adauga `Folders []ProposedFolder \`json:"folders"\``, dupa `Rationale`. In `ProposedAgent`, adauga `Folder string \`json:"folder"\` // numele folderului; "" = primul`.

In `ValidateProposal`, dupa verificarile de lungime pentru proiect (inainte de `if len(p.Agents) == 0`):

```go
	if len(p.Folders) > maxFolders {
		return ValidationError(fmt.Sprintf("at most %d folders", maxFolders))
	}
	folderNames := map[string]bool{}
	for i := range p.Folders {
		f := &p.Folders[i]
		f.Path, f.Name, f.Context = strings.TrimSpace(f.Path), strings.TrimSpace(f.Name), strings.TrimSpace(f.Context)
		if f.Path != "" {
			f.Path = filepath.Clean(f.Path)
		}
		if f.Name == "" {
			f.Name = store.FolderName(f.Path) // fara cale: "Main"
		}
		for _, err := range []error{tooLong("folder name", f.Name, 40), tooLong("context of folder "+f.Name, f.Context, 20000)} {
			if err != nil {
				return err
			}
		}
		if folderNames[strings.ToLower(f.Name)] {
			return ValidationError("two folders named " + f.Name)
		}
		folderNames[strings.ToLower(f.Name)] = true
	}
```

`const maxFolders = 6` langa `maxAgents`.

In bucla peste agenti, dupa `a.DoneDef = ...`:

```go
		a.Folder = strings.TrimSpace(a.Folder)
		if a.Folder != "" && !folderNames[strings.ToLower(a.Folder)] {
			return ValidationError(fmt.Sprintf("the folder of %s (%s) does not exist in the proposal", a.Name, a.Folder))
		}
```

Functia noua:

```go
// CheckFolderPaths: folderele propuse de analist sunt exact cele alese de
// utilizator (workdirs, deja curatate). Cu un folder sau niciunul, folders
// poate lipsi sau avea un singur element, cu calea aleasa sau fara cale.
func CheckFolderPaths(p *Proposal, workdirs []string) error {
	if len(workdirs) <= 1 {
		if len(p.Folders) > 1 {
			return ValidationError("there is only one folder: put at most one entry in folders")
		}
		if len(p.Folders) == 1 && p.Folders[0].Path != "" && (len(workdirs) == 0 || filepath.Clean(p.Folders[0].Path) != workdirs[0]) {
			return ValidationError("the folder path must be " + strings.Join(workdirs, "") + " (or empty)")
		}
		return nil
	}
	list := strings.Join(workdirs, ", ")
	if len(p.Folders) != len(workdirs) {
		return ValidationError(fmt.Sprintf("put exactly these %d folders in folders: %s", len(workdirs), list))
	}
	want := map[string]bool{}
	for _, w := range workdirs {
		want[w] = true
	}
	for _, f := range p.Folders {
		path := filepath.Clean(f.Path)
		if !want[path] {
			return ValidationError("unknown or repeated folder path " + f.Path + "; use exactly: " + list)
		}
		delete(want, path) // a doua aparitie a aceleiasi cai e refuzata
	}
	return nil
}
```

In `Members()`, calculeaza indexul folderelor si seteaza `Folder`:

```go
	folderAt := map[string]int{}
	for i, f := range p.Folders {
		folderAt[strings.ToLower(f.Name)] = i
	}
```

Apoi, in `store.NewMember{...}`, adauga `Folder: folderAt[strings.ToLower(a.Folder)],`. Un nume lipsa sau gol da 0, adica primul folder.

- [ ] **Step 4: `manager.go`**

1. `Session`: `Workdir string \`json:"workdir"\`` devine `Workdirs []string \`json:"workdirs"\``.

2. `Start(workdirs []string, description, model string)`:

```go
func (m *Manager) Start(workdirs []string, description, model string) (Session, error) {
	description = strings.TrimSpace(description)
	if err := tooLong("description", description, 4000); err != nil {
		return Session{}, err
	}
	if !models.Valid(model) {
		return Session{}, ValidationError("unknown model: " + model)
	}
	dirs := []string{}
	for _, w := range workdirs {
		if w = strings.TrimSpace(w); w == "" {
			continue
		}
		dir, err := expandHome(w)
		if err != nil {
			return Session{}, err
		}
		if !filepath.IsAbs(dir) {
			return Session{}, ValidationError("the folder must be an absolute path: " + w)
		}
		if info, err := os.Stat(dir); err != nil || !info.IsDir() {
			return Session{}, ValidationError("the folder does not exist: " + dir)
		}
		if dir = filepath.Clean(dir); !slices.Contains(dirs, dir) {
			dirs = append(dirs, dir)
		}
	}
	if len(dirs) > maxFolders {
		return Session{}, ValidationError(fmt.Sprintf("at most %d folders", maxFolders))
	}
	if len(dirs) == 0 && description == "" {
		return Session{}, ValidationError("choose a folder or describe what you want to do")
	}
	// un singur folder gol nu spune nimic despre proiect: fara descriere n-avem ce analiza
	if len(dirs) == 1 && description == "" {
		if entries, _ := os.ReadDir(dirs[0]); len(entries) == 0 {
			return Session{}, ValidationError("the folder is empty: describe what you want to do")
		}
	}
```

Restul ramane ca inainte, cu `Workdirs: dirs` si `Title: titleFor(dirs, description)` in `Session`. Pentru `cwd`: `if len(dirs) > 0 { cwd = dirs[0] } else { ... tempDir ... }`. Adauga importurile `fmt` si `slices`.

3. `titleFor(workdirs []string, description string)`: `if len(workdirs) > 0 { return filepath.Base(workdirs[0]) }`. Restul ramane.

4. `args`: la sfarsit, inainte de `return`, adauga celelalte foldere:

```go
	for _, dir := range s.Workdirs[min(1, len(s.Workdirs)):] {
		out = append(out, "--add-dir", dir) // analistul citeste si celelalte foldere
	}
```

(Transforma lista literala intoarsa intr-o variabila `out`, apoi `return out`.)

5. `propose`: dupa `ValidateProposal(&p, true)`:

```go
	m.mu.Lock()
	s, ok := m.sessions[id]
	m.mu.Unlock()
	if !ok || s.Status != StatusRunning {
		return "", ErrNotFound
	}
	if err := CheckFolderPaths(&p, s.Workdirs); err != nil { // Workdirs nu se schimba dupa Start
		return "", err
	}
```

Pastreaza apoi blocul existent, care blocheaza din nou si seteaza `s.Proposal` si `s.Title`.

- [ ] **Step 5: `prompt.go` (setup)**

In `systemPrompt`, adauga dupa regula despre shared context:

```
- Several folders mean one product with several parts (e.g. a client app and its landing page). Then fill folders with exactly the given paths, each with a short name (e.g. Client, Landing) and its own context (stack, build/test/run commands, conventions of that folder). Propose one root lead for the whole product and, under it, one sub-lead per folder with its workers; set each agent's folder. The shared context describes the product and how the parts connect (e.g. the API between them).
- With one folder or none, you may leave folders out.
```

`turnPrompt`:

```go
func turnPrompt(s *Session) string {
	folder := "There is no folder yet: the project is new."
	switch {
	case len(s.Workdirs) == 1:
		folder = "Folder: " + s.Workdirs[0]
	case len(s.Workdirs) > 1:
		folder = "Folders (parts of the same product; you are in the first one and can read all of them):\n- " + strings.Join(s.Workdirs, "\n- ")
	}
	want := "The user did not describe anything; infer it from the code."
	if s.Description != "" {
		want = "What the user wants: " + s.Description
	}
	return folder + "\n" + want
}
```

Adauga importul `strings`.

- [ ] **Step 6: `mcp/tools.go`**

In `proposeSetupTool.InputSchema`, langa `"rationale"`:

```go
		"folders": map[string]any{
			"type":        "array",
			"description": "Only with several folders: exactly the given folders, each with a short name and its own context. With one folder you may omit it.",
			"items": schema([]string{"path", "name"}, map[string]any{
				"path":    str("The folder path, exactly as given"),
				"name":    str("Short name, e.g. Client or Landing"),
				"context": str("Context for this folder: stack, build/test/run commands, conventions"),
			}),
		},
```

In `items` pentru agenti, adauga `"folder": str("The agent's folder (a name from folders); empty = the first folder"),`.

- [ ] **Step 7: `api_setup.go`**

`startSetup`: input-ul primeste `Workdirs []string \`json:"workdirs"\``, pe langa `Workdir`. Inainte de `s.wizard.Start`:

```go
	if len(in.Workdirs) == 0 && in.Workdir != "" {
		in.Workdirs = []string{in.Workdir} // API-ul vechi: un singur folder
	}
	sess, err := s.wizard.Start(in.Workdirs, in.Description, in.Model)
```

`createProjectFromSetup`: inlocuieste blocul `dir, err := resolveWorkdir(...)` si `p := store.Project{...}` cu:

```go
	folders := in.Proposal.Folders
	if len(folders) == 0 {
		folders = []wizard.ProposedFolder{{Path: in.Workdir}} // gol = folder nou in ~/Foreman
	}
	p := store.Project{Name: in.Proposal.Name, Goal: in.Proposal.Goal, Context: in.Proposal.Context}
	seen := map[string]bool{}
	for i, f := range folders {
		hint := in.Proposal.Name
		if i > 0 {
			hint += " " + f.Name
		}
		dir, err := resolveWorkdir(f.Path, hint, s.dirs.Workspaces)
		if err != nil {
			fail(w, err)
			return
		}
		if seen[dir] {
			fail(w, badRequest("two folders with the path "+dir))
			return
		}
		seen[dir] = true
		name := f.Name
		if name == "" {
			name = store.FolderName(dir)
		}
		p.Folders = append(p.Folders, store.Folder{Name: name, Path: dir, Context: f.Context})
	}
```

Restul (`CreateProjectWithTeam(&p, in.Proposal.Members())`, curatenia, `Dismiss`) ramane neschimbat.

- [ ] **Step 8: Ruleaza tot**

Run: `go vet ./... && go test ./...`
Expected: PASS.

- [ ] **Step 9: Commit**

```bash
git add internal/setup internal/mcp internal/web
git commit -m "wizard: mai multe foldere, echipe si context per folder"
```

---

### Task 8: Wizard UI: foldere la pasul 1 si 3, cardurile din Projects

**Files:**
- Modify: `internal/web/static/js/setup.js`
- Modify: `internal/web/static/js/projects.js` (setupCard)

**Interfaces:**
- Consumes: `POST /api/setup {workdirs, description, model}`, `session.workdirs`, `proposal.folders`, `agent.folder`

- [ ] **Step 1: Pasul 1: lista de foldere**

`form` devine `{ workdirs: [""], description: "", model: "sonnet" }`. In `stepChoose`, inlocuieste `workdir` si `pickBtn` cu o lista:

```js
    const folderBox = el("div", { class: "folder-rows" });
    const inputs = [];
    const addFolder = (value = "") => {
      const input = el("input", { type: "text", value, placeholder: inputs.length ? "e.g. ~/code/probe-landing" : "e.g. ~/code/shop (optional)" });
      const pick = info.canPickFolder && el("button", { type: "button", class: "btn", onclick: async () => {
        try { const r = await api("/api/pick-folder", { method: "POST" }); if (r?.path) input.value = r.path; } catch (err) { toast(err.message); }
      } }, "Browse...");
      const row = el("div", { class: "input-row" }, input, pick);
      const del = el("button", { type: "button", class: "btn", "aria-label": "Remove folder", onclick: () => {
        inputs.splice(inputs.indexOf(input), 1); row.remove(); syncDel();
      } }, "\u00d7");
      row.append(del);
      input.del = del;
      inputs.push(input);
      folderBox.append(row);
      syncDel();
    };
    const syncDel = () => inputs.forEach((i) => { i.del.hidden = inputs.length === 1; });
    (form.workdirs.length ? form.workdirs : [""]).forEach(addFolder);
```

Campul din formular:

```js
      el("div", { class: "field" },
        el("span", { class: "label" }, "Project folders"),
        folderBox,
        el("div", { class: "actions" }, el("button", { type: "button", class: "btn small", onclick: () => addFolder() }, "+ Add folder")),
        el("span", { class: "hint" }, "The folders are not modified: the AI only reads. Several folders = one project with several parts (e.g. client + landing page). Empty = new project; the folder is created at the end.")),
```

In `start`: `Object.assign(form, { workdirs: inputs.map((i) => i.value.trim()).filter(Boolean), description: ..., model: ... })`.

Sterge functia `pick` din `stepChoose`: e inlocuita de butoanele per rand.

- [ ] **Step 2: Redeschiderea sesiunii**

In blocul `if (id)`, `Object.assign(form, { workdir: session.workdir, ... })` devine `Object.assign(form, { workdirs: session.workdirs || [], description: session.description, model: session.model || form.model })`.

- [ ] **Step 3: Pasul 3: folderele si folderul fiecarui agent**

La inceputul lui `stepReview(p)`:

```js
    const folders = (p.folders || []).map((f) => ({
      path: f.path,
      name: el("input", { type: "text", value: f.name, maxlength: 40 }),
      context: el("textarea", { value: f.context || "", rows: 3 }),
    }));
    const many = folders.length > 1;
    const folderIndex = (name) => Math.max(0, (p.folders || []).findIndex((f) => f.name.toLowerCase() === (name || "").toLowerCase()));
```

In obiectul `r` al fiecarui agent adauga:

```js
        folder: many && el("select", { value: String(folderIndex(a.folder)) },
          folders.map((f, i) => el("option", { value: i }, f.name.value))),
```

In `r.el`, randul `Model / Takes` ramane. Dupa randul `Reports to / Reviewed by`, adauga:

```js
        r.folder && el("div", { class: "row" }, field("Folder", r.folder)),
```

(`r.folder` e creat inainte de `r.el`: muta crearea lui `r.el` dupa ce `r` are toate campurile, cum e deja.)

Cand se schimba numele unui folder, etichetele din selectele agentilor se actualizeaza:

```js
    folders.forEach((f, i) => f.name.addEventListener("input", () => {
      for (const r of rows) if (r.folder) r.folder.options[i].textContent = f.name.value;
      drawTree(rows.filter((x) => x.include.checked));
    }));
```

(pune-l dupa definirea lui `rows`)

In `drawTree`, linia unui agent primeste folderul cand `many`:

```js
            (many ? "  [" + folders[Number(r.folder.value)].name.value.trim() + "]" : "") +
```

(adaugat inainte de partea cu `(reviewed by ...)`). Adauga si `r.folder?.addEventListener("change", () => drawTree(rows.filter((x) => x.include.checked)));` in bucla de evenimente.

Sectiunea de foldere, in `body.replaceChildren`, intre sectiunea proiectului si sectiunea `Team`:

```js
      folders.length > 0 && el("section", { class: "section" },
        el("h2", {}, many ? "Folders" : "Folder"),
        folders.map((f) => el("div", { class: "folder-row" },
          el("div", { class: "row" }, field("Name", f.name), el("div", { class: "field" }, el("span", { class: "label" }, "Path"), el("code", {}, f.path || "new folder"))),
          field("Context for this folder", f.context)))),
```

In `create()`:

```js
      const agents = rows.filter((r) => r.include.checked).map((r) => ({
        ...
        folder: r.folder ? folders[Number(r.folder.value)].name.value.trim() : "",
      }));
      ...
        body: {
          setupId: session?.id, workdir: form.workdirs[0] || "",
          proposal: { ..., folders: folders.map((f) => ({ path: f.path, name: f.name.value.trim(), context: f.context.value })), agents },
        }
```

- [ ] **Step 4: `projects.js`: cardurile de analiza**

In `setupCard`, `s.workdir` devine `s.workdirs?.[0]`: in `text` (`s.proposal?.goal || s.description || (s.workdirs || []).join(", ")`) si in `retry` (`body: { workdirs: s.workdirs, description: s.description, model: s.model }`).

- [ ] **Step 5: Verificare**

Run: `for f in internal/web/static/js/*.js; do node --check $f; done && go test ./...`
Expected: fara erori.

- [ ] **Step 6: Commit**

```bash
git add internal/web/static
git commit -m "wizard ui: mai multe foldere, context si folder per agent in propunere"
```

---

### Task 9: Verificare cap-coada in browser, CLAUDE.md

**Files:**
- Modify: `CLAUDE.md` (Stare)

- [ ] **Step 1: Verificarea completa**

Run:

```bash
go vet ./... && go build -race ./... && go test ./... && \
GOOS=darwin GOARCH=arm64 go build -o /dev/null ./cmd/foreman && GOOS=windows go build -o /dev/null ./cmd/foreman
```

Expected: totul trece.

- [ ] **Step 2: Instanta de test cu claude fals**

Construieste binarul in scratchpad si porneste-l cu `HOME` temporar si portul 7789. Foloseste un `claude` fals din scratchpad, primul in `PATH`, ca in sesiunea in care s-a construit wizard-ul in fundal:
- scriptul raspunde cu `propose_setup` prin MCP (`curl` la URL-ul din `--mcp-config`);
- propunerea contine doua foldere si agenti cu `folder`.

**Nu** opri instanta de pe portul 7777.

- [ ] **Step 3: Screenshot-uri prin CDP**

Porneste Chrome headless cu `--remote-debugging-port=9333` si pilotarea printr-un script Node 22 (`WebSocket` global): `Page.navigate`, `Runtime.evaluate`, `Page.captureScreenshot`. Screenshot-ul simplu cu `--screenshot` se blocheaza din cauza SSE. Verifica si citeste PNG-urile pentru:

1. `#/new`: sectiunea Folders cu 2 randuri; creeaza proiectul si verifica `GET /api/projects/{id}` (2 foldere).
2. Tab-ul Team: nodurile arata „Rol · Client”; editorul agentului are select-ul Folder. Schimba folderul, salveaza, apoi trage o legatura noua pe canvas si verifica prin API ca `folderId` a ramas. Acesta e punctul 1 din Review Focus.
3. Tab-ul Tasks: chips „All / Client / Landing”; filtrul ascunde task-urile agentilor din celalalt folder.
4. „Open in”: select-ul de folder e vizibil.
5. `#/setup`: doua foldere, analiza (claude fals), pasul 3 cu Folders + select Folder per agent, apoi „Create project”; proiectul are 2 foldere si agentii in folderele lor.
6. Un proiect vechi, cu un singur folder: formularul si board-ul arata ca inainte, fara chips si fara select la „Open in”.

La 1440x900 si 1000x700.

- [ ] **Step 4: CLAUDE.md**

In sectiunea „Stare”, adauga dupa linia despre wizard:

```
Gata (foldere, spec in docs/superpowers/specs/2026-09-30-foreman-multi-folder-design.md): un proiect are unul sau mai multe
foldere (tabel project_folders; primul e implicit). Fiecare agent lucreaza si scrie in folderul lui (agents.folder_id,
NULL = primul), citeste restul prin --add-dir. Wizard-ul propune echipe si context per folder.
```

- [ ] **Step 5: Oprire si commit**

Opreste instanta de test si Chrome-ul headless.

```bash
git add CLAUDE.md
git commit -m "docs: stare dupa proiectele cu mai multe foldere"
```
