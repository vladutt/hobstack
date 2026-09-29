# Foreman: folder de lucru usor si fisiere atasate — plan de implementare

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:**
- Folderul de lucru devine optional: se creeaza automat sau se alege cu dialogul nativ.
- Proiectele si mesajele pot avea fisiere atasate, pe care agentii le citesc.

**Architecture:**
- Fisierele stau in `~/.foreman/projects/<id>/files/` si sunt descrise in tabelul SQLite `attachments`.
- `team` da fiecarui run `--add-dir` catre acel folder si pune caile in mesajul turei.
- `web` primeste upload-urile multipart, protejate de header-ul `X-Foreman: 1`, si serveste fisierele cu header-e sigure.
- Dialogul nativ traieste in pachetul nou `internal/picker`.

**Tech Stack:** Go (net/http, mime/multipart), modernc.org/sqlite, JS vanilla.

**Spec:** `docs/superpowers/specs/2026-09-29-foreman-files-design.md`

## Global Constraints

- Texte pentru utilizator si agenti in romana, **fara diacritice**. Comentariile in cod, la fel.
- Nicio dependenta noua.
- Dupa fiecare task trec `go vet ./...`, `go build -race ./...`, `go test -race ./...` si `gofmt -l .` (nu trebuie sa afiseze nimic). Cross-compile-ul (`GOOS=windows` si `GOOS=darwin GOARCH=arm64`, `-o /dev/null`) trebuie sa treaca.
- Cererile non-GET sub `/api/` accepta `Content-Type: application/json`, sau `multipart/form-data` insotit de header-ul `X-Foreman: 1`. Orice altceva primeste 415.
- Limite: 50 MB per fisier si 200 MB per request. In mesajul turei apar maxim 50 de fisiere de proiect.
- Fisierele de proiect sunt in `~/.foreman/projects/<id>/files/`. Folderul implicit de lucru e `~/Foreman/<slug>`.
- Codul trebuie sa fie simplu si idiomatic, pentru ca autorul e incepator in Go.
- In stage intra doar fisierele atinse, niciodata binare.

## Review Focus

- **Nume de fisier periculoase:** `../../etc/passwd`, `a/b.png` sau un nume gol trebuie curatate, iar fisierul trebuie sa ajunga mereu in folderul proiectului. Test: Task 1, `TestCleanFileName`.
- **HTML sau SVG incarcat si deschis in browser** nu are voie sa ruleze script pe originea aplicatiei. Test: Task 4, `TestServeFileHeaders`.
- **Un fisier atasat unui mesaj trebuie sa fie legat de mesaj inainte ca agentul sa porneasca.** Altfel run-ul ar porni fara el. Test: Task 2, `TestMessageAttachmentsInTurn`.
- **Doua proiecte cu acelasi nume** primesc foldere implicite diferite. Test: Task 4, `TestDefaultWorkdirUnique`.
- **Upload multipart fara `X-Foreman`** e respins, ca protectie impotriva paginilor straine. Test: Task 4, `TestUploadNeedsHeader`.

## Fisiere

```
internal/config/config.go            Paths.Projects, Paths.Workspaces, ProjectFilesDir()
internal/store/migrations/002_attachments.sql
internal/store/attachments.go        Attachment + CRUD, CleanFileName, StoredName
internal/store/messages.go           Message.Attachments umplut la citire
internal/store/attachments_test.go
internal/prompt/files.go             FileRef, FilesBlock
internal/prompt/files_test.go
internal/team/team.go                SetFilesRoot, filesDir
internal/team/run.go                 --add-dir; blocul de fisiere in tura
internal/team/reconcile.go           texts() cu atasamente; UserMessage cu atasamente
internal/team/files_test.go
internal/picker/picker.go            ErrCanceled, ErrUnsupported, Available(), Pick()
internal/picker/picker_darwin.go, picker_windows.go, picker_other.go
internal/web/server.go               Dirs, guard multipart, rute noi, info.canPickFolder
internal/web/workdir.go              slugify, resolveWorkdir
internal/web/api_files.go            upload/list/serve/delete, mesaje multipart, pick-folder
internal/web/files_test.go
internal/web/api_projects.go         workdir optional; stergerea folderului de fisiere la delete
cmd/foreman/main.go                  config.Paths nou, web.New(..., Dirs, picker)
internal/web/static/js/api.js        upload()
internal/web/static/js/projects.js   workdir optional + "Alege..."
internal/web/static/js/workspace.js  sectiunea Fisiere; atasamente in chat
internal/web/static/app.css          stiluri noi
README.md
```

---

### Task 1: config + store (atasamente)

**Files:** `internal/config/config.go`, `internal/config/config_test.go`, `internal/store/migrations/002_attachments.sql`, `internal/store/attachments.go`, `internal/store/messages.go`, `internal/store/attachments_test.go`

**Interfaces (produces):**
- `config.Paths` primeste campurile `Projects string` (`~/.foreman/projects`) si `Workspaces string` (`~/Foreman`). `Ensure()` creeaza `Projects`, dar nu si `Workspaces`.
- `config.ProjectFilesDir(projectsRoot string, id int64) string` intoarce `filepath.Join(projectsRoot, strconv.FormatInt(id,10), "files")`.
- `store.Attachment{ID, ProjectID, MessageID int64; Kind, Name, Stored, Mime string; Size int64; CreatedAt time.Time}`, cu JSON `id,projectId,messageId,kind,name,stored,mime,size,createdAt`. `MessageID` e 0 cand nu e legat de un mesaj.
- Constante: `store.KindProjectFile = "project"`, `store.KindMessageFile = "message"`.
- Functii pure:
  - `store.CleanFileName(name string) string` pastreaza doar baza numelui (fara foldere), inlocuieste caracterele de control si `/ \ : * ? " < > |` cu `_`, taie la 120 de caractere si intoarce `fisier` daca rezultatul e gol, `.` sau `..`;
  - `store.StoredName(clean string) string` intoarce `<16 caractere hex aleatoare>-<clean>`.
- Pe `*Store`:
  - `AddAttachment(a *Attachment) error`
  - `GetAttachment(id int64) (Attachment, error)` (ErrNotFound)
  - `ListProjectFiles(projectID int64) ([]Attachment, error)`: `kind='project'`, ordonate dupa `id`
  - `DeleteAttachment(id int64) error`
  - `LinkAttachments(ids []int64, messageID int64) error`: seteaza `message_id` doar pe randurile cu `kind='message'` si `message_id` NULL
- `store.Message` primeste campul `Attachments []Attachment` cu JSON `attachments`, mereu non-nil. Il umple fiecare citire de mesaje (`ListMessages`, `Undelivered`), printr-o singura interogare suplimentara per lista.

- [ ] **Step 1: Teste**

In `internal/config/config_test.go`, extinde `TestDefaultPaths`: `p.Projects == root/projects`, `p.Workspaces == home/Foreman`, iar `ProjectFilesDir(p.Projects, 7)` se termina cu `projects/7/files`.

`internal/store/attachments_test.go` (foloseste helper-ele `newStore`, `newProject`, `newWorker` din `store_test.go`):

```go
package store

import (
	"errors"
	"strings"
	"testing"
)

func TestCleanFileName(t *testing.T) {
	cases := map[string]string{
		"poza.png":           "poza.png",
		"../../etc/passwd":   "passwd",
		`C:\\tmp\\a.txt`:     "a.txt",
		"a/b.png":            "b.png",
		"":                   "fisier",
		"..":                 "fisier",
		"ra:u*?.txt":         "ra_u__.txt",
		"linie\nnoua.txt":    "linie_noua.txt",
	}
	for in, want := range cases {
		if got := CleanFileName(in); got != want {
			t.Errorf("CleanFileName(%q) = %q, vreau %q", in, got, want)
		}
	}
	if n := len([]rune(CleanFileName(strings.Repeat("x", 300) + ".png"))); n > 120 {
		t.Errorf("nume prea lung: %d", n)
	}
	a, b := StoredName("x.png"), StoredName("x.png")
	if a == b || !strings.HasSuffix(a, "-x.png") || len(a) != 16+1+len("x.png") {
		t.Errorf("StoredName: %q %q", a, b)
	}
}

func TestAttachmentsLifecycle(t *testing.T) {
	s := newStore(t)
	p, lead := newProject(t, s)
	doc := Attachment{ProjectID: p.ID, Kind: KindProjectFile, Name: "spec.pdf", Stored: "aa-spec.pdf", Size: 10, Mime: "application/pdf"}
	img := Attachment{ProjectID: p.ID, Kind: KindMessageFile, Name: "bug.png", Stored: "bb-bug.png", Size: 5, Mime: "image/png"}
	for _, a := range []*Attachment{&doc, &img} {
		if err := s.AddAttachment(a); err != nil {
			t.Fatal(err)
		}
	}
	files, err := s.ListProjectFiles(p.ID)
	if err != nil || len(files) != 1 || files[0].Name != "spec.pdf" {
		t.Fatalf("ListProjectFiles = %+v, %v", files, err)
	}

	m := Message{ProjectID: p.ID, AgentID: lead.ID, Kind: KindMsg, Author: AuthorUser, Text: "vezi poza"}
	s.AddMessage(&m)
	if err := s.LinkAttachments([]int64{img.ID, doc.ID}, m.ID); err != nil {
		t.Fatal(err)
	}
	msgs, _ := s.ListMessages(lead.ID)
	if len(msgs) != 1 || len(msgs[0].Attachments) != 1 || msgs[0].Attachments[0].Name != "bug.png" {
		t.Fatalf("mesajul trebuia sa aiba doar poza (fisierul de proiect nu se leaga): %+v", msgs)
	}
	pending, _ := s.Undelivered(lead.ID)
	if len(pending) != 1 || len(pending[0].Attachments) != 1 {
		t.Fatalf("Undelivered fara atasamente: %+v", pending)
	}
	plain := Message{ProjectID: p.ID, AgentID: lead.ID, Kind: KindEvent, Text: "x"}
	s.AddMessage(&plain)
	msgs, _ = s.ListMessages(lead.ID)
	if msgs[1].Attachments == nil {
		t.Fatal("Attachments trebuie sa fie [] nu nil")
	}

	if err := s.DeleteAttachment(doc.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetAttachment(doc.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("dupa stergere: %v", err)
	}
	if err := s.DeleteProject(p.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetAttachment(img.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("atasamentele trebuiau sterse in cascada cu proiectul")
	}
}
```

Ruleaza `go test ./internal/store/ ./internal/config/`. Trebuie sa pice la compilare.

- [ ] **Step 2: Migrarea `internal/store/migrations/002_attachments.sql`**

```sql
CREATE TABLE attachments (
  id         INTEGER PRIMARY KEY,
  project_id INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  message_id INTEGER REFERENCES messages(id) ON DELETE CASCADE,
  kind       TEXT NOT NULL CHECK (kind IN ('project','message')),
  name       TEXT NOT NULL,
  stored     TEXT NOT NULL,
  size       INTEGER NOT NULL,
  mime       TEXT NOT NULL,
  created_at TEXT NOT NULL
);
CREATE INDEX attachments_project ON attachments(project_id);
CREATE INDEX attachments_message ON attachments(message_id);
```

`migrate()` aplica fisierele in ordinea numelui, deci `002` ruleaza dupa `001` si pe bazele existente. Testul `TestOpenAppliesMigrationsOnce` astepta `user_version == 1`; actualizeaza-l la 2.

- [ ] **Step 3: Implementare**
- `internal/store/attachments.go`: struct-ul, constantele, `CleanFileName`, `StoredName` (cu `crypto/rand` si `hex`) si metodele. `CleanFileName` foloseste `filepath.Base` dupa ce a inlocuit `\` cu `/`, ca sa mearga si pe cai Windows.
- `internal/store/messages.go`:
  - adauga campul `Attachments` in struct-ul `Message`;
  - dupa ce `queryMessages` (si orice alt loc care intoarce mesaje) a strans lista, ruleaza un singur `SELECT ... FROM attachments WHERE message_id IN (SELECT id FROM messages WHERE <acelasi where>)`, sau `WHERE message_id IN (?,?,...)` cu id-urile adunate, si distribuie atasamentele pe mesaje. `AddMessage` pune `Attachments = []Attachment{}`.
- `internal/config/config.go`: campurile noi, `Ensure` si `ProjectFilesDir`.

- [ ] **Step 4: Verificare si commit**

Ruleaza verificarile din Global Constraints, apoi comite cu mesajul `store: atasamente pentru proiecte si mesaje`.

---

### Task 2: prompt + team (agentii vad fisierele)

**Files:** `internal/prompt/files.go`, `internal/prompt/files_test.go`, `internal/team/team.go`, `internal/team/run.go`, `internal/team/reconcile.go`, `internal/team/files_test.go`, plus call-site-urile lui `UserMessage` (`internal/web/api_agents.go`, testele din `internal/team` si `internal/web`)

**Interfaces:**
- `prompt.FileRef{Name, Path string}` si `prompt.FilesBlock(files []FileRef) string`. Pe o lista goala intoarce `""`. Altfel intoarce `"Fisiere proiect (le poti citi cu Read):\n- <Path> (<Name>)\n..."`. Peste 50 de fisiere, le pastreaza pe primele 50 si adauga linia `- ... si inca N fisiere`.
- `(*Team) SetFilesRoot(root string)`. Daca radacina e goala, run-urile nu primesc nici `--add-dir`, nici blocul de fisiere; asa raman valabile testele existente.
- `(*Team) filesDir(projectID int64) string` intoarce `config.ProjectFilesDir(t.filesRoot, projectID)`, sau `""` daca radacina e goala.
- `(*Team) UserMessage(agentID int64, text string, attachmentIDs []int64) (store.Message, error)`. **Semnatura se schimba**: actualizeaza toate apelurile, cu `nil` acolo unde nu exista atasamente. Sub `t.work`, dupa `AddMessage`, apeleaza `LinkAttachments(attachmentIDs, m.ID)` si reciteste atasamentele in `m.Attachments`, **inainte** de `emit` si `Kick`.
- In `startRun`, daca `filesDir != ""`:
  - `os.MkdirAll(dir, 0o755)`;
  - adauga la argumente `"--add-dir", dir`;
  - adauga la tura blocul `prompt.FilesBlock(...)` cu fisierele de proiect (`ListProjectFiles`), separat prin `"\n\n"`.
  Blocul se adauga in `startRun`, deci o singura data pentru toate cele trei tipuri de ture.
- `texts(...)` devine metoda `(t *Team) texts(projectID int64, msgs []store.Message, labelUser bool) []string`. Pentru fiecare atasament al unui mesaj, adauga la text linia `"\nAtasat: <filesDir>/<Stored> (<Name>)"`.

- [ ] **Step 1: Teste**

`internal/prompt/files_test.go`:

```go
package prompt

import (
	"fmt"
	"strings"
	"testing"
)

func TestFilesBlock(t *testing.T) {
	if FilesBlock(nil) != "" {
		t.Fatal("lista goala trebuie sa dea text gol")
	}
	got := FilesBlock([]FileRef{{Name: "spec.pdf", Path: "/f/aa-spec.pdf"}})
	if got != "Fisiere proiect (le poti citi cu Read):\n- /f/aa-spec.pdf (spec.pdf)" {
		t.Fatalf("FilesBlock = %q", got)
	}
	var many []FileRef
	for i := 0; i < 53; i++ {
		many = append(many, FileRef{Name: fmt.Sprint(i), Path: fmt.Sprint("/f/", i)})
	}
	got = FilesBlock(many)
	if strings.Count(got, "\n- /f/") != 50 || !strings.HasSuffix(got, "- ... si inca 3 fisiere") {
		t.Fatalf("limita de 50: %q", got)
	}
}
```

`internal/team/files_test.go` (foloseste `setup`, `mustReconcile`, `turn`, `argValue` si `f.fs.last`):

```go
package team

import (
	"path/filepath"
	"strings"
	"testing"

	"foreman/internal/store"
)

func TestFilesDirAndBlockInTurn(t *testing.T) {
	f := setup(t)
	root := t.TempDir()
	f.tm.SetFilesRoot(root)
	doc := store.Attachment{ProjectID: f.p.ID, Kind: store.KindProjectFile, Name: "spec.pdf", Stored: "aa-spec.pdf", Size: 1, Mime: "application/pdf"}
	f.st.AddAttachment(&doc)
	f.task(t, "A", f.rook, store.StatusQueued)
	mustReconcile(t, f)
	spec := f.fs.last(t, f.rook)
	dir := filepath.Join(root, "1", "files")
	if argValue(spec.Args, "--add-dir") != dir {
		t.Fatalf("--add-dir = %q, vreau %q", argValue(spec.Args, "--add-dir"), dir)
	}
	want := "Fisiere proiect (le poti citi cu Read):\n- " + filepath.Join(dir, "aa-spec.pdf") + " (spec.pdf)"
	if !strings.Contains(turn(spec), want) {
		t.Fatalf("tura nu contine blocul de fisiere:\n%s", turn(spec))
	}
}

func TestMessageAttachmentsInTurn(t *testing.T) {
	f := setup(t)
	root := t.TempDir()
	f.tm.SetFilesRoot(root)
	img := store.Attachment{ProjectID: f.p.ID, Kind: store.KindMessageFile, Name: "bug.png", Stored: "bb-bug.png", Size: 1, Mime: "image/png"}
	f.st.AddAttachment(&img)
	m, err := f.tm.UserMessage(f.lead.ID, "Vezi bug-ul", []int64{img.ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Attachments) != 1 {
		t.Fatalf("mesajul intors trebuie sa aiba atasamentul: %+v", m)
	}
	mustReconcile(t, f)
	want := "Utilizatorul: Vezi bug-ul\nAtasat: " + filepath.Join(root, "1", "files", "bb-bug.png") + " (bug.png)"
	if got := turn(f.fs.last(t, f.lead)); !strings.Contains(got, want) {
		t.Fatalf("tura lead-ului:\n%s", got)
	}
}

func TestNoFilesRootNoAddDir(t *testing.T) {
	f := setup(t)
	f.task(t, "A", f.rook, store.StatusQueued)
	mustReconcile(t, f)
	if hasFlag(f.fs.last(t, f.rook).Args, "--add-dir") {
		t.Fatal("fara SetFilesRoot nu trebuie --add-dir")
	}
}
```

Numarul `"1"` din cale e id-ul proiectului creat de `setup` (primul proiect din baza noua).

- [ ] **Step 2: Implementare** conform Interfaces. `team` poate importa `foreman/internal/config`.
- [ ] **Step 3: Verificare si commit.** Mesajul: `team: agentii primesc fisierele proiectului si atasamentele`.

---

### Task 3: pachetul `picker` (dialogul nativ de ales folder)

**Files:** `internal/picker/picker.go`, `picker_darwin.go`, `picker_windows.go`, `picker_other.go` (build tag `!darwin && !windows`), `internal/picker/picker_test.go`

**Interfaces:**
- `var ErrCanceled = errors.New("anulat")` si `var ErrUnsupported = errors.New("dialogul de ales folder nu e disponibil pe acest sistem")`
- `func Available() bool`, `func Pick(ctx context.Context) (string, error)`
- **macOS:** `Available()` intoarce true. `Pick` ruleaza `osascript`, cu argumentele:
  - `-e 'tell application "System Events"'`
  - `-e 'activate'`
  - `-e 'set f to POSIX path of (choose folder with prompt "Alege folderul proiectului")'`
  - `-e 'end tell'`
  - `-e 'return f'`

  Cancel iese cu cod nenul si scrie in stderr „User canceled” sau `(-128)`; asta se traduce in `ErrCanceled`. Rezultatul e taiat cu `TrimSpace`, apoi cu `TrimSuffix "/"` (fara sa atinga `/` singur).
- **Windows:** `Available()` intoarce true. `Pick` ruleaza `powershell -NoProfile -STA -Command "Add-Type -AssemblyName System.Windows.Forms; $d=New-Object System.Windows.Forms.FolderBrowserDialog; if($d.ShowDialog() -eq 'OK'){ $d.SelectedPath }"`. Un output gol inseamna `ErrCanceled`.
- **Altele:** `Available()` intoarce `exec.LookPath("zenity") == nil`. `Pick` ruleaza `zenity --file-selection --directory --title="Alege folderul proiectului"`; codul de iesire 1 inseamna `ErrCanceled`, iar zenity lipsa inseamna `ErrUnsupported`.
- Un helper pur, testabil pe orice OS, in `picker.go`: `func parseResult(stdout, stderr string, err error) (string, error)`. E folosit de implementarile macOS si Linux.

- [ ] **Step 1: Test `picker_test.go`**, doar pentru `parseResult`. Nu deschide niciun dialog.

```go
package picker

import (
	"errors"
	"testing"
)

func TestParseResult(t *testing.T) {
	if p, err := parseResult("/Users/x/Proiect/\n", "", nil); err != nil || p != "/Users/x/Proiect" {
		t.Fatalf("%q %v", p, err)
	}
	if p, _ := parseResult("/\n", "", nil); p != "/" {
		t.Fatalf("radacina: %q", p)
	}
	if _, err := parseResult("", "execution error: User canceled. (-128)", errors.New("exit 1")); !errors.Is(err, ErrCanceled) {
		t.Fatalf("cancel: %v", err)
	}
	if _, err := parseResult("", "", nil); !errors.Is(err, ErrCanceled) {
		t.Fatal("rezultat gol = cancel")
	}
	if _, err := parseResult("", "altceva", errors.New("exit 2")); err == nil || errors.Is(err, ErrCanceled) {
		t.Fatalf("eroare reala: %v", err)
	}
}
```

- [ ] **Step 2: Implementare.** In testele automate nu rula niciodata `Pick()`: ar deschide o fereastra pe ecranul utilizatorului.
- [ ] **Step 3: Verificare**, inclusiv cross-compile pentru windows, darwin/arm64 si `GOOS=linux go build -o /dev/null ./...`. Commit: `picker: dialog nativ de ales folder`.

---

### Task 4: web (folder optional, fisiere, mesaje multipart, pick-folder) + main

**Files:** `internal/web/server.go`, `internal/web/workdir.go`, `internal/web/api_files.go`, `internal/web/api_projects.go`, `internal/web/api_agents.go`, `internal/web/files_test.go`, `internal/web/server_test.go` (setup), `cmd/foreman/main.go`

**Interfaces:**
- `type Dirs struct { Projects, Workspaces string }`
- `web.New(st, tm, runs, hub, mcpHandler, dirs Dirs, pick func(context.Context) (string, error), canPick bool) *Server`. In `main`, se paseaza `web.Dirs{Projects: paths.Projects, Workspaces: paths.Workspaces}`, `picker.Pick` si `picker.Available()`. Tot in `main`: `tm.SetFilesRoot(paths.Projects)`.
- **guard:** un request non-GET sub `/api/` trece daca are `Content-Type` care incepe cu `application/json`, sau `multipart/form-data` impreuna cu `r.Header.Get("X-Foreman") == "1"`. Altfel primeste 415, cu mesajul `Content-Type trebuie sa fie application/json (sau multipart cu X-Foreman: 1)`.
- `GET /api/info` intoarce `{"name": AppName, "canPickFolder": canPick}`.
- **`workdir.go`:**
  - `slugify(name string) string`: litere mici; `ăâîșțş ţ` devin `a a i s t s t`; orice altceva decat `[a-z0-9]` devine `-`; `-` repetate se comprima si se taie la capete; maxim 60 de caractere; un rezultat gol devine `proiect`.
  - `resolveWorkdir(input, projectName, workspaces string) (string, error)`:
    - input gol: alege `workspaces/<slug>`; daca exista, `-2`, `-3` si asa mai departe, primul care nu exista; il creeaza cu `MkdirAll`;
    - input dat: `expandHome`; cale relativa: badRequest (mesajul existent); daca exista si nu e folder, badRequest `exista deja un fisier cu acest nume: <cale>`; daca nu exista, `MkdirAll`.
  - `toProject` foloseste `resolveWorkdir`.
  - La `PUT`, un workdir gol pastreaza folderul existent al proiectului: input gol devine `existing.Workdir` inainte de `resolveWorkdir`.
- **`api_files.go`:**
  - `POST /api/projects/{id}/files`:
    - `r.Body = http.MaxBytesReader(w, r.Body, 200<<20)`, apoi `r.ParseMultipartForm(32<<20)`;
    - pentru fiecare `r.MultipartForm.File["files"]`: marime peste 50 MB inseamna 400 `fisierul <nume> depaseste 50 MB`;
    - numele se curata cu `store.CleanFileName`, iar numele pe disc vine din `store.StoredName`;
    - folderul `config.ProjectFilesDir(dirs.Projects, id)` se creeaza, fisierul se copiaza cu `io.Copy`;
    - mime-ul se ia din header-ul partii. Daca e gol sau `application/octet-stream`, se ghiceste din extensie cu `mime.TypeByExtension`, altfel ramane `application/octet-stream`;
    - se apeleaza `st.AddAttachment` cu kind `project`;
    - la final: `hub.Publish(id, "files", {id})` si 201 cu lista;
    - fara fisiere in request: 400 `niciun fisier`.
  - `GET /api/projects/{id}/files`: lista, cu 404 daca proiectul nu exista.
  - `GET /api/files/{id}`:
    - header-e: `Content-Type` = mime-ul salvat, `X-Content-Type-Options: nosniff`, `Content-Security-Policy: sandbox`;
    - `Content-Disposition`: `inline` pentru `image/png`, `image/jpeg`, `image/gif`, `image/webp`, `application/pdf` si `text/plain` (cu charset), `attachment` pentru restul. Numele se da prin `mime.FormatMediaType("inline"|"attachment", map[string]string{"filename": name})`;
    - continutul se serveste cu `http.ServeContent`.
  - `DELETE /api/files/{id}`: sterge fisierul de pe disc (un fisier deja lipsa se ignora), apoi randul, publica `files`, intoarce 204.
  - `POST /api/pick-folder`:
    - fara dialog disponibil: 501 `dialogul de ales folder nu e disponibil pe acest sistem`;
    - apeleaza `pick(r.Context())`: `ErrCanceled` da 204, o alta eroare da 500 cu mesajul ei logat, succesul da 200 `{"path": p}`.
- **`postMessage`:**
  - daca `Content-Type` e multipart: se aplica aceleasi limite, textul vine din `r.FormValue("text")`, fisierele din `files` se salveaza ca `kind='message'` (in proiectul agentului), apoi `team.UserMessage(id, text, ids)`;
  - textul gol e permis doar daca exista cel putin un fisier. In acel caz, textul trimis agentului devine `(vezi atasamentele)`;
  - pentru JSON ramane comportamentul de acum, cu `team.UserMessage(id, text, nil)`.
- **`deleteProject`:** dupa `DeleteProject` reusit, `os.RemoveAll(filepath.Join(dirs.Projects, id))`. Folderul de lucru nu se atinge niciodata.

- [ ] **Step 1: Teste**

In `internal/web/files_test.go`, adauga un helper `upload(t, e, path, fields map[string]string, files map[string][]byte, withHeader bool) (int, []byte)`. Helper-ul construieste body-ul cu `mime/multipart.Writer`, cu campul `files` pentru fiecare fisier, si seteaza `X-Foreman: 1` doar cand `withHeader` e true.

`setup` din `server_test.go` se schimba asa:
- foloseste `Dirs{Projects: t.TempDir(), Workspaces: t.TempDir()}`;
- paseaza un picker fals prin campuri pe `env`: `e.pickPath string` si `e.pickErr error`, care pot fi schimbate din test;
- `canPick` e true;
- apeleaza `tm.SetFilesRoot(dirs.Projects)`;
- pastreaza `dirs` in `env`.

Teste obligatorii (fiecare cu asertii concrete):
- `TestUploadNeedsHeader`: upload fara header da 415; cu header da 201.
- `TestUploadListServeDelete`:
  - incarci `spec.pdf` (continut `%PDF-1.4 x`) si `pagina.html` (`<script>alert(1)</script>`);
  - lista are 2 elemente;
  - `GET` pe pdf intoarce 200, continutul corect, `Content-Disposition` care incepe cu `inline` si `Content-Security-Policy: sandbox`;
  - `DELETE` pe pdf intoarce 204, iar fisierul lipseste de pe disc (`os.Stat` pe `ProjectFilesDir/stored` da `IsNotExist`).
- `TestServeFileHeaders`: html-ul se serveste cu `attachment` si `nosniff`.
- `TestUploadTooBig`: un fisier de 50 MB + 1 byte da 400. Se genereaza cu `bytes.Repeat`, iar testul trebuie sa ramana sub cateva secunde.
- `TestMessageWithAttachment`:
  - multipart catre `/api/agents/{rookId}/messages` cu `text=uite` si `bug.png`: 201;
  - raspunsul are `attachments` cu un element;
  - `GET /api/projects/{id}/files` NU contine `bug.png`;
  - `GET` pe mesajele lui Rook arata atasamentul.
- `TestDefaultWorkdirUnique`:
  - doua `POST /api/projects` cu `name: "HoReCa QR"` si fara `workdir` primesc `Workspaces/horeca-qr` si `Workspaces/horeca-qr-2`, ambele create pe disc;
  - un workdir dat, dar inexistent (`t.TempDir()+"/nou/adanc"`), se creeaza.
  - Actualizeaza `TestCreateProjectValidation`: cazul „folder inexistent” nu mai e 400. Inlocuieste-l cu o cale care exista ca **fisier**, deci 400.
- `TestSlugify`: `HoReCa QR` → `horeca-qr`, `Țara mea!` → `tara-mea`, `   ` → `proiect`, `a--b__c` → `a-b-c`.
- `TestPickFolder`:
  - picker-ul fals intoarce `/x/y`, deci 200 cu `{"path":"/x/y"}`;
  - intoarce `picker.ErrCanceled`, deci 204;
  - `GET /api/info` contine `"canPickFolder":true`.
- `TestDeleteProjectRemovesFiles`: incarci un fisier, stergi proiectul (204), iar `filepath.Join(dirs.Projects, id)` nu mai exista. Folderul de lucru exista in continuare.

- [ ] **Step 2: Implementare** conform Interfaces, plus actualizarea `main.go`. In `main`, dupa `tm := team.New(...)`, apeleaza `tm.SetFilesRoot(paths.Projects)`.
- [ ] **Step 3: Verificare** (toate testele, cross-compile), apoi verificare manuala prin HTTP:
  - serverul pornit cu HOME temporar, pe portul 7788, construit in `/tmp`, oprit la final dupa PID;
  - `curl -F` cu header-ul `-H 'X-Foreman: 1'` pentru upload;
  - `curl` pe `/api/info`.

  **Nu** apela `/api/pick-folder` pe binarul real: ar deschide un dialog pe ecranul utilizatorului.

  Commit: `web: folder de lucru optional, fisiere de proiect si atasamente in chat`.

---

### Task 5: UI

**Files:** `internal/web/static/js/api.js`, `projects.js`, `workspace.js`, `internal/web/static/app.css`

**Cerinte:**
- **`api.js`:** noul export `upload(path, formData)` face `fetch(path, {method: "POST", headers: {"X-Foreman": "1"}, body: formData})`. Nu seteaza `Content-Type`; browserul pune singur boundary-ul. Erorile se trateaza ca in `api()`.
- **`projects.js`, formularul:**
  - campul de folder nu mai are `required`;
  - placeholder-ul e `gol = ~/Foreman/<nume proiect>`;
  - hint-ul devine: „Optional. Daca il lasi gol, se creeaza automat. O cale care nu exista se creeaza.”
  - daca `(await api("/api/info")).canPickFolder`, langa camp apare butonul `Alege...`, de tip `button`. Butonul face `POST /api/pick-folder`: pe raspuns cu `path`, completeaza campul; pe raspuns gol (204), nu face nimic; pe eroare, arata un toast. Cat asteapta, textul butonului e `Se deschide...` si butonul e dezactivat.
- **`workspace.js`:**
  - **Sectiunea noua „Fisiere”**, sub board:
    - antet cu `h2` „Fisiere” si butonul `Incarca` (un input file ascuns, `multiple`);
    - lista: pentru fiecare fisier, un link spre `/api/files/{id}` (`target=_blank`, `rel=noopener`), marimea formatata (B/KB/MB) si butonul `x` pentru stergere, cu `confirm`;
    - drag & drop peste sectiune: `dragover` cu `preventDefault` si o clasa vizuala, `drop` care face upload;
    - lista se reincarca la evenimentul SSE `files` si dupa fiecare actiune;
    - gol: „Niciun fisier. Trage fisiere aici sau apasa Incarca.”
  - **Atasamente in chat:**
    - in composer, inainte de textarea, un buton agrafa (`📎`, cu `aria-label` „Ataseaza fisiere”) cu un input file ascuns `multiple`;
    - paste pe textarea: daca `e.clipboardData.files` are fisiere, le adauga la lista in asteptare. Imaginile lipite fara nume primesc numele `screenshot-<ora>.png`;
    - drag & drop peste consola adauga la lista in asteptare;
    - lista in asteptare apare deasupra formularului ca etichete `nume ×`; `×` scoate fisierul din lista;
    - la trimitere, daca exista fisiere: `FormData` cu `text` si fiecare fisier ca `files`, trimis cu `upload(`/api/agents/${id}/messages`, fd)`. Altfel, JSON ca pana acum. Dupa succes, lista in asteptare se goleste.
    - trimiterea e permisa si cu text gol, daca exista fisiere.
  - **`messageNode`:** daca `m.attachments` nu e gol, sub bubble apare un rand cu atasamente:
    - imaginile (`mime` care incepe cu `image/`) apar ca `img` (max 180×180, `loading=lazy`, `alt` = nume), in interiorul unui link spre fisier;
    - restul apar ca link-uri cu numele.
    - Totul se construieste cu `el()`, fara innerHTML.
- **CSS:** stiluri pentru `.files-list`, `.file-row`, `.dropping` (contur amber), `.chips`/`.chip`, `.attachments img` si butonul agrafa, in stilul existent (variabilele `--line`, `--amber` etc.).
- [ ] **Verificare:**
  - bucla `node --check` pe copii `.mjs` ale fiecarui fisier js (nicio linie `EROARE`);
  - `go test ./internal/web/`;
  - pe binarul pornit cu HOME temporar si port 7788, `GET /js/workspace.js` si `/js/projects.js` intorc 200 si contin `X-Foreman` si `Alege...`.

  Commit: `UI: fisiere de proiect, atasamente in chat, folder optional cu Alege...`.

---

### Task 6: documentatie + test cap-coada cu Claude real

- [ ] **README.md:**
  - sectiunea „Rulare” mentioneaza ca folderul de lucru e optional (`~/Foreman/<nume>`);
  - un bullet nou in „Cum functioneaza”: fisierele atasate stau in `~/.foreman/projects/<id>/files/`, agentii le primesc cu `--add-dir` si le citesc cu Read (inclusiv imagini si PDF);
  - in „Date” se adauga linia pentru `~/.foreman/projects/<id>/files/`.
- [ ] **Test cap-coada prin API**, cu HOME-ul real, portul 7788 si `--no-open`, oprit dupa PID:
  1. creezi un proiect fara workdir, deci in `~/Foreman/<slug>`;
  2. setezi lead-ul pe haiku, fara tool-uri;
  3. generezi un PNG rosu de 40×20 cu python (zlib, fara PIL) si il incarci ca fisier de proiect;
  4. ii trimiti lead-ului, ca multipart cu un fisier text atasat (`nota.txt` cu `cod: PAPAGAL-42`), mesajul „Ce culoare are imaginea din fisierele proiectului si ce cod scrie in nota atasata? Raspunde scurt, fara sa creezi task-uri.”;
  5. astepti raspunsul lead-ului (maxim ~2 minute).

  **Asteptat:** raspunsul contine „rosu”/„rosie” si „PAPAGAL-42”. In raport pune tura trimisa (din `app_start` in log) si raspunsul. La final, sterge proiectul prin API; folderul `~/Foreman/<slug>` ramane pe disc, iar asta e corect.
- [ ] Commit: `docs: fisiere atasate si folder optional`.

**Dialogul „Alege...” nu se testeaza automat.** Il verifica utilizatorul la final.
