# Fisiere atasate pe task: plan de implementare

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Agentii predau fisiere-livrabil prin `report_done(files: [...])`; foreman le copiaza ca snapshot, iar utilizatorul le vede (preview) si le descarca din modala de task.

**Architecture:** Tabel nou `task_files` (migrarea 024) + pachet mic `internal/taskfiles` care copiaza sigur un fisier din folderele proiectului (os.OpenRoot). `team.AttachFiles` (ca `LeaveForUser`) e apelat de MCP dupa un `report_done` reusit. Web: `GET /api/tasks/{id}/files`, `GET /api/task-files/{id}` (servire comuna cu `serveFile`), `attachments` pe cardul din board. UI: modul nou `task-files.js` si tab-ul Files in modala.

**Tech Stack:** Go 1.24+ (os.OpenRoot), SQLite (modernc), JS vanilla (module ES).

**Spec:** `docs/superpowers/specs/2026-10-08-foreman-task-files-design.md`

## Global Constraints

- Tot ce vede utilizatorul si textele pentru agenti sunt in engleza; comentariile din cod in romana.
- `go vet ./...` si `go build -race ./...` trec; `go test ./...` trece.
- JS: copiaza fisierul ca `.mjs` si ruleaza `node --check` (pe `.js` nu prinde erorile).
- Cross-compile: `GOOS=darwin GOARCH=arm64 go build ./cmd/foreman` si `GOOS=windows go build ./cmd/foreman`.
- Max 20 de fisiere pe `report_done`, fiecare cale 1-500 caractere; max 50 MB pe fisier.
- Doar fisiere din folderele proiectului (`project_folders`); HTML/SVG nu se servesc niciodata inline.
- Fara dependente noi.
- Repo-ul are modificari necommise straine (regulile de comenzi): fiecare commit adauga DOAR fisierele task-ului (`git add <cai>`, `git commit -- <cai>`).

## Review Focus

1. **Cale absoluta in alt folder al proiectului** (agentul scrie in folderul lui, dar preda `/abs/folder2/x.md`): trebuie acceptata si afisata absolut. Test in Task 2.
2. **Acelasi fisier predat de doua ori** (dupa send back): a doua predare e versiune noua, prima apare la "earlier versions", nu se suprascrie pe disc. Test in Task 1 (store) si Task 4 (API).
3. **Raport reusit, toate fisierele esueaza**: raportul ramane facut, agentul primeste `Not attached: ...` fara eroare de tool. Test in Task 3.
4. **Fisier `.html` sau `.svg` predat**: servit cu `Content-Security-Policy: sandbox` si `attachment`, nu inline. Test in Task 4.
5. **Fisier text mare (ex. 30 MB log)**: preview-ul cere doar primul MB (Range) si arata "Download to see the rest". Verificat in Task 5 (Range e acoperit de `http.ServeContent`; test API in Task 4).

---

### Task 1: Migrarea 024 + store `task_files`

**Files:**
- Create: `internal/store/migrations/024_task_files.sql`
- Create: `internal/store/task_files.go`
- Create: `internal/store/task_files_test.go`
- Modify: `internal/config/config.go` (dupa `ProjectFilesDir`, linia ~54)

**Interfaces:**
- Produces:
  - `type TaskFile struct { ID, TaskID, ProjectID int64; AgentID *int64; AgentName, Path, Name, Stored string; Size int64; Mime string; CreatedAt time.Time }` (tag-uri JSON: `id, taskId, projectId, agentId, agentName, path, name, size, mime, createdAt`; `Stored` cu `json:"-"`)
  - `func (s *Store) AddTaskFile(f *TaskFile) error` (seteaza ID si CreatedAt)
  - `func (s *Store) GetTaskFile(id int64) (TaskFile, error)` (`ErrNotFound` cand lipseste)
  - `func (s *Store) ListTaskFiles(taskID int64) ([]TaskFile, error)` (cel mai nou primul; non-nil)
  - `func (s *Store) ListDescendantFiles(taskID int64) ([]TaskFile, error)` (fisierele sub-task-urilor, recursiv; ordonate dupa `tasks.number`, apoi id desc; non-nil)
  - `func (s *Store) CountTaskFiles(projectID int64) (map[int64]int, error)` (cai distincte per task)
  - `func (s *Store) CountTaskFilesTree(taskID int64) (int, error)` (randuri pe task + descendenti; amprenta pentru UI)
  - `func config.TaskFilesDir(projectsRoot string, projectID, taskID int64) string` -> `<root>/<pid>/task-files/<taskID>`

- [ ] **Step 1: Migrarea**

`internal/store/migrations/024_task_files.sql`:
```sql
-- Fisiere predate de agenti pe un task (report_done files): copii (snapshot) in projects/<pid>/task-files/<task>/
CREATE TABLE task_files (
  id          INTEGER PRIMARY KEY,
  task_id     INTEGER NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
  project_id  INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  agent_id    INTEGER REFERENCES agents(id) ON DELETE SET NULL,
  agent_name  TEXT NOT NULL DEFAULT '',
  path        TEXT NOT NULL,
  name        TEXT NOT NULL,
  stored      TEXT NOT NULL,
  size        INTEGER NOT NULL,
  mime        TEXT NOT NULL,
  created_at  TEXT NOT NULL
);
CREATE INDEX task_files_task ON task_files(task_id, path, id);
CREATE INDEX task_files_project ON task_files(project_id);
```

- [ ] **Step 2: Testul care pica**

`internal/store/task_files_test.go`:
```go
package store

import "testing"

func TestTaskFiles(t *testing.T) {
	s := newStore(t)
	p, lead := newProject(t, s)
	parent := Task{ProjectID: p.ID, Title: "Docs", AssigneeID: lead.ID}
	if err := s.CreateTask(&parent); err != nil {
		t.Fatal(err)
	}
	child := Task{ProjectID: p.ID, Title: "Logo", AssigneeID: lead.ID, ParentTaskID: &parent.ID}
	if err := s.CreateTask(&child); err != nil {
		t.Fatal(err)
	}
	grand := Task{ProjectID: p.ID, Title: "Icon", AssigneeID: lead.ID, ParentTaskID: &child.ID}
	if err := s.CreateTask(&grand); err != nil {
		t.Fatal(err)
	}
	add := func(task Task, path string) TaskFile {
		f := TaskFile{TaskID: task.ID, ProjectID: p.ID, AgentID: &lead.ID, AgentName: lead.Name,
			Path: path, Name: path, Stored: "x-" + path, Size: 3, Mime: "text/plain"}
		if err := s.AddTaskFile(&f); err != nil {
			t.Fatal(err)
		}
		return f
	}
	v1 := add(parent, "report.md")
	v2 := add(parent, "report.md") // a doua predare: versiune noua
	add(child, "logo.png")
	add(grand, "icon.svg")

	got, err := s.GetTaskFile(v2.ID)
	if err != nil || got.Path != "report.md" || got.AgentName != lead.Name || got.CreatedAt.IsZero() {
		t.Fatalf("%+v %v", got, err)
	}
	if _, err := s.GetTaskFile(9999); err != ErrNotFound {
		t.Fatalf("lipsa: %v", err)
	}
	list, _ := s.ListTaskFiles(parent.ID)
	if len(list) != 2 || list[0].ID != v2.ID || list[1].ID != v1.ID {
		t.Fatalf("ordinea: %+v", list)
	}
	desc, _ := s.ListDescendantFiles(parent.ID)
	if len(desc) != 2 || desc[0].TaskID != child.ID || desc[1].TaskID != grand.ID {
		t.Fatalf("descendenti: %+v", desc)
	}
	if empty, _ := s.ListDescendantFiles(grand.ID); empty == nil || len(empty) != 0 {
		t.Fatalf("fara descendenti: %+v", empty)
	}
	counts, _ := s.CountTaskFiles(p.ID)
	if counts[parent.ID] != 1 || counts[child.ID] != 1 {
		t.Fatalf("cai distincte: %v", counts)
	}
	if n, _ := s.CountTaskFilesTree(parent.ID); n != 4 {
		t.Fatalf("arbore: %d", n)
	}
}
```

- [ ] **Step 3: Ruleaza, trebuie sa pice**

Run: `go test ./internal/store -run TestTaskFiles`
Expected: FAIL (`undefined: TaskFile`)

- [ ] **Step 4: Implementarea**

`internal/store/task_files.go`:
```go
package store

import (
	"database/sql"
	"strings"
	"time"
)

// TaskFile e un fisier predat de un agent pe un task (o copie facuta la report_done).
type TaskFile struct {
	ID        int64     `json:"id"`
	TaskID    int64     `json:"taskId"`
	ProjectID int64     `json:"projectId"`
	AgentID   *int64    `json:"agentId"`
	AgentName string    `json:"agentName"` // numele la momentul predarii
	Path      string    `json:"path"`      // relativa la folderul agentului cand e in el, altfel absoluta
	Name      string    `json:"name"`
	Stored    string    `json:"-"` // numele de pe disc, in task-files/<task>/
	Size      int64     `json:"size"`
	Mime      string    `json:"mime"`
	CreatedAt time.Time `json:"createdAt"`
}

const taskFileCols = `id, task_id, project_id, agent_id, agent_name, path, name, stored, size, mime, created_at`

func scanTaskFile(row interface{ Scan(...any) error }) (TaskFile, error) {
	var f TaskFile
	var agent sql.NullInt64
	var at string
	err := row.Scan(&f.ID, &f.TaskID, &f.ProjectID, &agent, &f.AgentName, &f.Path, &f.Name, &f.Stored, &f.Size, &f.Mime, &at)
	if agent.Valid {
		id := agent.Int64
		f.AgentID = &id
	}
	f.CreatedAt = parseTime(at)
	return f, err
}

func (s *Store) AddTaskFile(f *TaskFile) error {
	ts := now()
	var agent sql.NullInt64
	if f.AgentID != nil {
		agent = sql.NullInt64{Int64: *f.AgentID, Valid: true}
	}
	res, err := s.db.Exec(`INSERT INTO task_files (task_id, project_id, agent_id, agent_name, path, name, stored, size, mime, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		f.TaskID, f.ProjectID, agent, f.AgentName, f.Path, f.Name, f.Stored, f.Size, f.Mime, ts)
	if err != nil {
		return err
	}
	f.ID, err = res.LastInsertId()
	f.CreatedAt = parseTime(ts)
	return err
}

func (s *Store) GetTaskFile(id int64) (TaskFile, error) {
	f, err := scanTaskFile(s.db.QueryRow(`SELECT `+taskFileCols+` FROM task_files WHERE id = ?`, id))
	if err == sql.ErrNoRows {
		return TaskFile{}, ErrNotFound
	}
	return f, err
}

// ListTaskFiles: fisierele task-ului, cel mai nou primul; lista e mereu non-nil.
func (s *Store) ListTaskFiles(taskID int64) ([]TaskFile, error) {
	return s.queryTaskFiles(`SELECT `+taskFileCols+` FROM task_files WHERE task_id = ? ORDER BY id DESC`, taskID)
}

// ListDescendantFiles: fisierele sub-task-urilor (recursiv), in ordinea numerelor de task.
func (s *Store) ListDescendantFiles(taskID int64) ([]TaskFile, error) {
	return s.queryTaskFiles(`WITH RECURSIVE sub(id) AS (
			SELECT id FROM tasks WHERE parent_task_id = ?
			UNION ALL SELECT t.id FROM tasks t JOIN sub ON t.parent_task_id = sub.id)
		SELECT `+prefixCols("f.", taskFileCols)+` FROM task_files f JOIN tasks t ON t.id = f.task_id
		WHERE f.task_id IN (SELECT id FROM sub) ORDER BY t.number, f.id DESC`, taskID)
}

func (s *Store) queryTaskFiles(q string, args ...any) ([]TaskFile, error) {
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TaskFile{}
	for rows.Next() {
		f, err := scanTaskFile(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// CountTaskFiles: per task al proiectului, cate cai distincte au fost predate (fara versiuni).
func (s *Store) CountTaskFiles(projectID int64) (map[int64]int, error) {
	rows, err := s.db.Query(`SELECT task_id, COUNT(DISTINCT path) FROM task_files WHERE project_id = ? GROUP BY task_id`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]int{}
	for rows.Next() {
		var id int64
		var n int
		if err := rows.Scan(&id, &n); err != nil {
			return nil, err
		}
		out[id] = n
	}
	return out, rows.Err()
}

// CountTaskFilesTree: randurile task-ului si ale descendentilor (amprenta ieftina pentru tab-ul Files).
func (s *Store) CountTaskFilesTree(taskID int64) (int, error) {
	var n int
	err := s.db.QueryRow(`WITH RECURSIVE sub(id) AS (
			SELECT ? UNION ALL SELECT t.id FROM tasks t JOIN sub ON t.parent_task_id = sub.id)
		SELECT COUNT(*) FROM task_files WHERE task_id IN (SELECT id FROM sub)`, taskID).Scan(&n)
	return n, err
}
```

`prefixCols` nu exista in store; adaug-o tot in `task_files.go`:
```go
// prefixCols: "a, b" -> "f.a, f.b" (pentru JOIN-uri)
func prefixCols(prefix, cols string) string {
	parts := strings.Split(cols, ",")
	for i, c := range parts {
		parts[i] = prefix + strings.TrimSpace(c)
	}
	return strings.Join(parts, ", ")
}
```
(`"strings"` e deja in import-ul de mai sus).

`internal/config/config.go`, dupa `ProjectFilesDir`:
```go
// TaskFilesDir: copiile fisierelor predate pe un task (report_done files).
func TaskFilesDir(projectsRoot string, projectID, taskID int64) string {
	return filepath.Join(projectsRoot, strconv.FormatInt(projectID, 10), "task-files", strconv.FormatInt(taskID, 10))
}
```

- [ ] **Step 5: Ruleaza, trebuie sa treaca**

Run: `go test ./internal/store && go vet ./internal/store ./internal/config`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/store/migrations/024_task_files.sql internal/store/task_files.go internal/store/task_files_test.go internal/config/config.go
git commit -m "store: task_files (fisiere predate pe task)" -- internal/store/migrations/024_task_files.sql internal/store/task_files.go internal/store/task_files_test.go internal/config/config.go
```

---

### Task 2: Pachetul `internal/taskfiles` (copiere sigura)

**Files:**
- Create: `internal/taskfiles/copy.go`
- Create: `internal/taskfiles/open_unix.go`, `internal/taskfiles/open_windows.go`
- Create: `internal/taskfiles/copy_test.go`

**Interfaces:**
- Produces:
  - `const MaxSize = 50 << 20`
  - `type Copied struct { Path, Name, Stored, Mime string; Size int64 }`
  - `func Copy(roots []string, home, raw, destDir string) (Copied, error)`: `roots` = toate folderele proiectului (inclusiv `home`); `home` = folderul agentului; `raw` = calea predata; `destDir` = `config.TaskFilesDir(...)`. Erorile au texte scurte pentru agent: `outside the project folders; put deliverables in your folder`, `not found`, `not a regular file`, `larger than 50 MB`, `empty path`.

- [ ] **Step 1: Testul care pica**

`internal/taskfiles/copy_test.go`:
```go
package taskfiles

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func write(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestCopy(t *testing.T) {
	home, other, outside, dest := t.TempDir(), t.TempDir(), t.TempDir(), t.TempDir()
	roots := []string{home, other}
	write(t, filepath.Join(home, "docs", "report.md"), []byte("# Report\n"))
	write(t, filepath.Join(other, "logo.png"), []byte("\x89PNG\r\n\x1a\n0000"))
	write(t, filepath.Join(outside, "secret.txt"), []byte("key"))

	c, err := Copy(roots, home, "docs/report.md", dest)
	if err != nil {
		t.Fatal(err)
	}
	if c.Path != filepath.Join("docs", "report.md") || c.Name != "report.md" || c.Size != 9 {
		t.Fatalf("%+v", c)
	}
	if got, _ := os.ReadFile(filepath.Join(dest, c.Stored)); string(got) != "# Report\n" {
		t.Fatalf("copia: %q", got)
	}
	// acelasi fisier inca o data: alt nume pe disc (versiunea veche ramane)
	c2, err := Copy(roots, home, "./docs/report.md", dest)
	if err != nil || c2.Stored == c.Stored {
		t.Fatalf("%+v %v", c2, err)
	}
	// cale absoluta in alt folder al proiectului: acceptata, afisata absolut
	abs := filepath.Join(other, "logo.png")
	c3, err := Copy(roots, home, abs, dest)
	if err != nil || c3.Path != abs || c3.Mime != "image/png" {
		t.Fatalf("%+v %v", c3, err)
	}

	bad := map[string]string{
		filepath.Join(outside, "secret.txt"): "outside the project folders",
		"../" + filepath.Base(outside) + "/secret.txt": "outside the project folders",
		"missing.md": "not found",
		"docs":       "not a regular file",
		" ":          "empty path",
	}
	for raw, want := range bad {
		if _, err := Copy(roots, home, raw, dest); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: %v (vreau %q)", raw, err, want)
		}
	}
}

func TestCopySymlinkOut(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink-uri pe Windows cer drepturi")
	}
	home, outside, dest := t.TempDir(), t.TempDir(), t.TempDir()
	write(t, filepath.Join(outside, "id_rsa"), []byte("key"))
	if err := os.Symlink(filepath.Join(outside, "id_rsa"), filepath.Join(home, "key.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(home, "dir")); err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{"key.txt", "dir/id_rsa"} {
		if _, err := Copy([]string{home}, home, raw, dest); err == nil {
			t.Errorf("%s: symlink spre exterior acceptat", raw)
		}
	}
}

func TestCopyTooLarge(t *testing.T) {
	home, dest := t.TempDir(), t.TempDir()
	write(t, filepath.Join(home, "big.log"), bytes.Repeat([]byte("a"), MaxSize+1))
	if _, err := Copy([]string{home}, home, "big.log", dest); err == nil || !strings.Contains(err.Error(), "larger than 50 MB") {
		t.Fatalf("%v", err)
	}
	if entries, _ := os.ReadDir(dest); len(entries) != 0 {
		t.Fatalf("a ramas o copie partiala: %v", entries)
	}
}
```

- [ ] **Step 2: Ruleaza, trebuie sa pice**

Run: `go test ./internal/taskfiles`
Expected: FAIL (pachet fara cod: `undefined: Copy`)

- [ ] **Step 3: Implementarea**

`internal/taskfiles/open_unix.go`:
```go
//go:build !windows

package taskfiles

import "syscall"

// oNonblock: la open nu blocam pe un FIFO (verificam tipul abia dupa Lstat).
const oNonblock = syscall.O_NONBLOCK
```

`internal/taskfiles/open_windows.go`:
```go
//go:build windows

package taskfiles

const oNonblock = 0
```

`internal/taskfiles/copy.go`:
```go
// Package taskfiles copiaza fisierele predate de agenti pe un task (report_done files).
// Copierea o face foreman, deci verificam ca fisierul e intr-un folder al proiectului:
// altfel un agent ar putea "preda" orice fisier de pe masina, iar UI-ul l-ar servi.
package taskfiles

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"foreman/internal/store"
)

// MaxSize: cat poate avea un fisier predat (ca la upload).
const MaxSize = 50 << 20

// Copied descrie copia facuta.
type Copied struct {
	Path   string // calea de afisat: relativa la home cand e in el, altfel absoluta
	Name   string
	Stored string // numele copiei in destDir
	Mime   string
	Size   int64
}

var (
	errOutside    = errors.New("outside the project folders; put deliverables in your folder")
	errNotFound   = errors.New("not found")
	errNotRegular = errors.New("not a regular file")
	errTooLarge   = errors.New("larger than 50 MB")
	errEmpty      = errors.New("empty path")
)

// Copy copiaza fisierul raw (relativ la home sau absolut) in destDir.
// roots sunt folderele proiectului; fisierul trebuie sa fie sub unul din ele.
func Copy(roots []string, home, raw, destDir string) (Copied, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.ContainsRune(raw, 0) {
		return Copied{}, errEmpty
	}
	abs := raw
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(home, raw)
	}
	abs = filepath.Clean(abs)
	root, rel, ok := within(roots, abs)
	if !ok {
		return Copied{}, errOutside
	}
	display := abs
	if r, ok := relTo(home, abs); ok {
		display = r
	}

	// prin os.Root: ".." si symlink-urile care ies din radacina sunt refuzate de Go
	r, err := os.OpenRoot(root)
	if err != nil {
		return Copied{}, errNotFound
	}
	defer r.Close()
	lst, err := r.Lstat(rel)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Copied{}, errNotFound
		}
		return Copied{}, errOutside // de ex. o componenta e un symlink spre exterior
	}
	if !lst.Mode().IsRegular() {
		return Copied{}, errNotRegular // folder, symlink, FIFO, device
	}
	if lst.Size() > MaxSize {
		return Copied{}, errTooLarge
	}
	src, err := r.OpenFile(rel, os.O_RDONLY|oNonblock, 0)
	if err != nil {
		return Copied{}, errOutside
	}
	defer src.Close()

	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return Copied{}, err
	}
	name := store.CleanFileName(filepath.Base(abs))
	stored := randomPrefix() + "-" + name
	dst, err := os.OpenFile(filepath.Join(destDir, stored), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return Copied{}, err
	}
	// fisierul poate creste intre Lstat si citire: copiem cel mult MaxSize+1 octeti
	head := make([]byte, 512)
	n, _ := io.ReadFull(src, head)
	head = head[:n]
	size, err := io.Copy(dst, io.MultiReader(bytes.NewReader(head), io.LimitReader(src, MaxSize+1-int64(n))))
	cerr := dst.Close()
	if err == nil {
		err = cerr
	}
	if err == nil && size > MaxSize {
		err = errTooLarge
	}
	if err != nil {
		os.Remove(filepath.Join(destDir, stored))
		return Copied{}, err
	}
	return Copied{Path: display, Name: name, Stored: stored, Mime: detectMime(name, head), Size: size}, nil
}

// within cauta radacina care contine abs si intoarce calea relativa la ea.
func within(roots []string, abs string) (root, rel string, ok bool) {
	for _, r := range roots {
		if rel, ok := relTo(r, abs); ok {
			return filepath.Clean(r), rel, true
		}
	}
	return "", "", false
}

// relTo: abs relativ la dir, doar daca e strict sub dir.
func relTo(dir, abs string) (string, bool) {
	if dir == "" {
		return "", false
	}
	rel, err := filepath.Rel(filepath.Clean(dir), abs)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", false
	}
	return rel, true
}

func detectMime(name string, head []byte) string {
	if m := mime.TypeByExtension(strings.ToLower(filepath.Ext(name))); m != "" {
		return m
	}
	return http.DetectContentType(head)
}

func randomPrefix() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("crypto/rand: %v", err))
	}
	return hex.EncodeToString(b)
}
```

Nota: `mime.TypeByExtension(".md")` poate fi gol pe unele sisteme; atunci `DetectContentType` da `text/plain; charset=utf-8`. UI-ul decide preview-ul Markdown dupa extensie (Task 5), deci e in regula.

- [ ] **Step 4: Ruleaza, trebuie sa treaca**

Run: `go test ./internal/taskfiles && go vet ./internal/taskfiles && GOOS=windows go build ./internal/taskfiles`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/taskfiles
git commit -m "taskfiles: copiere sigura a fisierelor predate" -- internal/taskfiles
```

---

### Task 3: `team.AttachFiles` + parametrul `files` in MCP + prompt

**Files:**
- Create: `internal/team/task_files.go`
- Create: `internal/team/task_files_test.go`
- Modify: `internal/mcp/server.go` (interfata `Backend` ~linia 59; `case "report_done"` ~linia 183; validare langa `validLeftForUser` ~linia 286)
- Modify: `internal/mcp/tools.go:72-84` (`reportTool`)
- Modify: `internal/mcp/server_test.go` (fakeBackend ~linia 41; test nou dupa `TestReportDoneLeftForUser`)
- Modify: `internal/prompt/prompt.go` (const nou langa `howLeftForUser` ~linia 91; folosit la liniile ~180 si ~188)

**Interfaces:**
- Consumes: `store.AddTaskFile`, `store.TaskFile`, `config.TaskFilesDir`, `taskfiles.Copy`, `taskfiles.Copied`, `t.todoTask`, `t.taskMsg`, `t.emit`, `t.filesRoot`.
- Produces:
  - `func (t *Team) AttachFiles(c mcp.Caller, ref string, paths []string) (string, error)`; raspuns: `Attached N files.` + optional ` Not attached: <cale> (<motiv>); ...`
  - in `mcp.Backend`: `AttachFiles(c Caller, task string, paths []string) (string, error)`
  - `const mcp.MaxFiles = 20`
  - eveniment `task_events.kind = "files_attached"`

- [ ] **Step 1: Testul team care pica**

`internal/team/task_files_test.go`:
```go
package team

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"foreman/internal/config"
	"foreman/internal/store"
)

func TestAttachFiles(t *testing.T) {
	f := setup(t)
	root := t.TempDir()
	f.tm.SetFilesRoot(root)
	home := f.p.Folders[0].Path
	if err := os.WriteFile(filepath.Join(home, "report.md"), []byte("# R"), 0o644); err != nil {
		t.Fatal(err)
	}
	task := f.task(t, "Write report", f.rook, store.StatusInProgress)
	c := f.workerCaller(f.rook, task.ID)
	if _, err := f.tm.ReportDone(c, "", "report written"); err != nil {
		t.Fatal(err)
	}
	text, err := f.tm.AttachFiles(c, "", []string{"report.md", "report.md", "/etc/hosts", "nope.md"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "Attached 1 file.") || !strings.Contains(text, "/etc/hosts (outside the project folders") ||
		!strings.Contains(text, "nope.md (not found)") {
		t.Fatalf("%q", text)
	}
	files, _ := f.st.ListTaskFiles(task.ID)
	if len(files) != 1 || files[0].Path != "report.md" || files[0].AgentName != "Rook" {
		t.Fatalf("%+v", files)
	}
	copy := filepath.Join(config.TaskFilesDir(root, f.p.ID, task.ID), files[0].Stored)
	if got, err := os.ReadFile(copy); err != nil || string(got) != "# R" {
		t.Fatalf("copia: %q %v", got, err)
	}
	events, _ := f.st.ListTaskEvents(task.ID)
	last := events[len(events)-1]
	if last.Kind != "files_attached" || last.Actor != "Rook" || !strings.Contains(last.Body, "report.md") {
		t.Fatalf("%+v", last)
	}
	// totul esueaza: fara eroare de tool, fara eveniment nou
	text, err = f.tm.AttachFiles(c, "", []string{"nope.md"})
	if err != nil || !strings.Contains(text, "No files attached.") {
		t.Fatalf("%q %v", text, err)
	}
	if again, _ := f.st.ListTaskEvents(task.ID); len(again) != len(events) {
		t.Fatal("eveniment scris fara fisiere")
	}
	// un agent strain nu poate atasa
	if _, err := f.tm.AttachFiles(f.workerCaller(f.vera, 0), task.Ref(), []string{"report.md"}); err == nil {
		t.Fatal("agent strain a atasat")
	}
}
```

Daca `f.p.Folders` e gol in fixture (proiectul vine din `CreateProject` care apeleaza `defaultFolders`), citeste proiectul cu `f.st.GetProject(f.p.ID)` si ia `Folders[0].Path`.

- [ ] **Step 2: Ruleaza, trebuie sa pice**

Run: `go test ./internal/team -run TestAttachFiles`
Expected: FAIL (`f.tm.AttachFiles undefined`)

- [ ] **Step 3: Implementarea in team**

`internal/team/task_files.go`:
```go
package team

import (
	"errors"
	"fmt"
	"strings"

	"foreman/internal/config"
	"foreman/internal/mcp"
	"foreman/internal/store"
	"foreman/internal/taskfiles"
)

// AttachFiles copiaza fisierele predate la report_done si le leaga de task.
// Un fisier care nu poate fi copiat nu strica nimic: agentul afla care si de ce.
func (t *Team) AttachFiles(c mcp.Caller, ref string, paths []string) (string, error) {
	t.work.Lock()
	defer t.work.Unlock()
	task, a, err := t.todoTask(c, ref)
	if err != nil {
		return "", err
	}
	if t.filesRoot == "" {
		return "", errors.New("files cannot be attached in this setup")
	}
	p, err := t.st.GetProject(task.ProjectID)
	if err != nil {
		return "", err
	}
	roots := make([]string, 0, len(p.Folders))
	for _, f := range p.Folders {
		roots = append(roots, f.Path)
	}
	home := store.HomeFolder(a, p.Folders).Path
	dest := config.TaskFilesDir(t.filesRoot, p.ID, task.ID)

	var attached, failed []string
	seen := map[string]bool{}
	for _, raw := range paths {
		key := strings.TrimSpace(raw)
		if seen[key] {
			continue
		}
		seen[key] = true
		cp, err := taskfiles.Copy(roots, home, raw, dest)
		if err != nil {
			failed = append(failed, fmt.Sprintf("%s (%s)", key, err))
			continue
		}
		agentID := a.ID
		tf := store.TaskFile{TaskID: task.ID, ProjectID: p.ID, AgentID: &agentID, AgentName: a.Name,
			Path: cp.Path, Name: cp.Name, Stored: cp.Stored, Size: cp.Size, Mime: cp.Mime}
		if err := t.st.AddTaskFile(&tf); err != nil {
			removeQuiet(dest, cp.Stored)
			failed = append(failed, fmt.Sprintf("%s (%s)", key, err))
			continue
		}
		attached = append(attached, cp.Path)
	}

	out := "No files attached."
	if n := len(attached); n > 0 {
		out = fmt.Sprintf("Attached %d %s.", n, plural(n, "file", "files"))
		t.taskMsg(task, "files_attached", a.Name, actorUser,
			fmt.Sprintf("%s attached %d %s", a.Name, n, plural(n, "file", "files")),
			"- "+strings.Join(attached, "\n- "))
		t.emit(c.ProjectID, "task", task)
	}
	if len(failed) > 0 {
		out += " Not attached: " + strings.Join(failed, "; ") + "."
	}
	return out, nil
}
```

In acelasi fisier, helper-ul de curatenie:
```go
// removeQuiet sterge o copie ramasa fara rand in baza de date.
func removeQuiet(dir, name string) {
	if err := os.Remove(filepath.Join(dir, name)); err != nil && !os.IsNotExist(err) {
		log.Printf("task files: cannot remove %s: %v", name, err)
	}
}
```
(adauga `"log"`, `"os"`, `"path/filepath"` la import).

- [ ] **Step 4: Ruleaza testul team**

Run: `go test ./internal/team -run TestAttachFiles`
Expected: PASS

- [ ] **Step 5: Testul MCP care pica**

In `internal/mcp/server_test.go`, in fakeBackend (dupa `LeaveForUser`):
```go
func (f *fakeBackend) AttachFiles(c Caller, task string, paths []string) (string, error) {
	f.calls = append(f.calls, "attach|"+task+"|"+strings.Join(paths, ";"))
	return "Attached 2 files.", f.err
}
```
Test nou, dupa `TestReportDoneLeftForUser`:
```go
func TestReportDoneFiles(t *testing.T) {
	fb, ts := setup(t)
	body := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"report_done","arguments":{"summary":"done","files":["docs/a.md","b.png"]}}}`
	_, resp := rpc(t, ts, "worker", body)
	text, isErr := toolText(t, resp)
	if isErr || !strings.Contains(text, "Attached 2 files.") {
		t.Fatalf("%q %v", text, isErr)
	}
	if got := fb.calls[len(fb.calls)-1]; got != "attach||docs/a.md;b.png" {
		t.Fatalf("apel = %q", got)
	}
	many := make([]string, 21)
	for i := range many {
		many[i] = fmt.Sprintf("%q", fmt.Sprint("f", i, ".md"))
	}
	before := len(fb.calls)
	_, resp = rpc(t, ts, "worker", `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"report_done","arguments":{"summary":"done","files":[`+strings.Join(many, ",")+`]}}}`)
	if text, isErr := toolText(t, resp); !isErr || !strings.Contains(text, "at most 20") {
		t.Fatalf("%q %v", text, isErr)
	}
	if len(fb.calls) != before {
		t.Fatal("report_done a rulat desi files era invalid")
	}
	_, resp = rpc(t, ts, "worker", `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"report_done","arguments":{"summary":"done","files":[" "]}}}`)
	if _, isErr := toolText(t, resp); !isErr {
		t.Fatal("cale goala acceptata")
	}
}
```

- [ ] **Step 6: Ruleaza, trebuie sa pice**

Run: `go test ./internal/mcp -run TestReportDoneFiles`
Expected: FAIL (fisierele nu sunt atasate: textul nu contine `Attached 2 files.`)

- [ ] **Step 7: Implementarea MCP**

`internal/mcp/server.go`, in `Backend` dupa `LeaveForUser`:
```go
	AttachFiles(c Caller, task string, paths []string) (string, error)
```
Langa `validLeftForUser`:
```go
// MaxFiles: cate fisiere poate preda un agent la un raport
const MaxFiles = 20

func validFiles(paths []string) error {
	if len(paths) > MaxFiles {
		return fmt.Errorf("files: at most %d files", MaxFiles)
	}
	for _, p := range paths {
		n := len([]rune(strings.TrimSpace(p)))
		if n == 0 || n > 500 {
			return errors.New("files: each path must be 1-500 characters")
		}
	}
	return nil
}
```
`case "report_done":` devine:
```go
	case "report_done":
		left := argList(p.Arguments, "left_for_user")
		if err := validLeftForUser(left); err != nil {
			return toolResult(err.Error(), true)
		}
		files := argList(p.Arguments, "files")
		if err := validFiles(files); err != nil {
			return toolResult(err.Error(), true)
		}
		text, err = s.backend.ReportDone(c, arg("task"), arg("summary"))
		if err == nil && len(left) > 0 {
			// raportul a trecut: punctele se lasa pe acelasi task (task gol = task-ul curent)
			extra, lerr := s.backend.LeaveForUser(c, arg("task"), left)
			if lerr != nil {
				text += " Note: the items for the user were NOT saved (" + lerr.Error() + "). Write them in your next comment_task so the user sees them: " + strings.Join(left, "; ") + "."
			} else {
				text += " " + extra
			}
		}
		if err == nil && len(files) > 0 {
			// fisierele se copiaza dupa raport; un esec nu anuleaza raportul
			extra, ferr := s.backend.AttachFiles(c, arg("task"), files)
			if ferr != nil {
				text += " Note: the files were NOT attached (" + ferr.Error() + ")."
			} else {
				text += " " + extra
			}
		}
```

`internal/mcp/tools.go`, in `reportTool.InputSchema` dupa `left_for_user`:
```go
		"files": map[string]any{
			"type":        "array",
			"items":       map[string]any{"type": "string"},
			"description": "Optional: up to 20 files you produced as the deliverable of this task (documents, images, exports), relative to your folder. They are copied and attached to the task. Do not list code you changed; it is already shown as changes.",
		},
```

- [ ] **Step 8: Prompt**

`internal/prompt/prompt.go`, dupa `howLeftForUser`:
```go
const howFiles = `When the task asks for a file as its result (a document, an image, an export), save it in your folder ` +
	`and list its path in files when you call report_done, so the user can open it from the task.`
```
La liniile unde se adauga `howLeftForUser` (~180 si ~188), adauga si `howFiles`: `how = append(how, howSubLead, howLeftForUser, howFiles)` si `how = append(how, howLeftForUser, howFiles)`.

Daca exista teste de prompt care compara textul exact (`grep -rn "howLeftForUser\|left_for_user when you call" internal/prompt/*_test.go`), actualizeaza-le sa contina si `howFiles`.

- [ ] **Step 9: Ruleaza tot**

Run: `go test ./internal/mcp ./internal/team ./internal/prompt && go vet ./...`
Expected: PASS

- [ ] **Step 10: Commit**

```bash
git add internal/team/task_files.go internal/team/task_files_test.go internal/mcp/server.go internal/mcp/tools.go internal/mcp/server_test.go internal/prompt/prompt.go
git commit -m "mcp: report_done files ataseaza livrabile pe task" -- internal/team/task_files.go internal/team/task_files_test.go internal/mcp/server.go internal/mcp/tools.go internal/mcp/server_test.go internal/prompt/prompt.go
```
(adauga si fisierele de test din `internal/prompt` daca le-ai modificat. Inainte de commit, `git diff --stat` pe fisierele de mai sus: la scrierea planului niciunul nu avea modificari straine; daca acum are, opreste-te si intreaba.)

---

### Task 4: API (lista, servire) + numarul pe card

**Files:**
- Create: `internal/web/api_task_files.go`
- Create: `internal/web/task_files_test.go`
- Modify: `internal/web/api_files.go:176-211` (extrage `serveStored`)
- Modify: `internal/web/server.go` (rutele, langa `GET /api/tasks/{id}/flow`, linia ~122)
- Modify: `internal/web/api_tasks.go` (`getTask`: campul `filesCount`, langa `changesCount` ~linia 79 si 133)
- Modify: `internal/web/api_projects.go:209-213` (`taskView.Attachments`) si ~linia 380/401 (umplerea)

**Interfaces:**
- Consumes: `store.ListTaskFiles`, `store.ListDescendantFiles`, `store.GetTaskFile`, `store.CountTaskFiles`, `store.CountTaskFilesTree`, `config.TaskFilesDir`.
- Produces (JSON pentru Task 5):
  - `GET /api/tasks/{id}/files` -> `{"files":[fileGroup], "subtasks":[{"taskId","ref","title","files":[fileGroup]}]}` unde `fileGroup` = `{"id","path","name","size","mime","agentName","createdAt","versions":[{"id","size","agentName","createdAt"}]}`
  - `GET /api/task-files/{id}` (si `?download=1`)
  - `GET /api/tasks/{id}` primeste `"filesCount": <int>` (randuri pe task + descendenti)
  - task-urile din proiect primesc `"attachments": <int>` (cai distincte pe task)

- [ ] **Step 1: Testul care pica**

`internal/web/task_files_test.go`:
```go
package web

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"foreman/internal/config"
	"foreman/internal/store"
)

// addTaskFile scrie o copie pe disc si randul ei, ca dupa report_done files.
func (e *env) addTaskFile(t *testing.T, task store.Task, path, mime, content string) store.TaskFile {
	t.Helper()
	dir := config.TaskFilesDir(e.dirs.Projects, task.ProjectID, task.ID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	stored := fmt.Sprintf("%d-%s", time.Now().UnixNano(), filepath.Base(path))
	if err := os.WriteFile(filepath.Join(dir, stored), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	f := store.TaskFile{TaskID: task.ID, ProjectID: task.ProjectID, AgentName: "Rook", Path: path,
		Name: filepath.Base(path), Stored: stored, Size: int64(len(content)), Mime: mime}
	if err := e.st.AddTaskFile(&f); err != nil {
		t.Fatal(err)
	}
	return f
}

func TestTaskFilesAPI(t *testing.T) {
	e := setup(t)
	p := e.createProject(t, t.TempDir())
	w := e.addWorker(t, p.ID, "Rook")
	parent := store.Task{ProjectID: p.ID, Title: "Docs", AssigneeID: w.ID}
	if err := e.st.CreateTask(&parent); err != nil {
		t.Fatal(err)
	}
	child := store.Task{ProjectID: p.ID, Title: "Logo", AssigneeID: w.ID, ParentTaskID: &parent.ID}
	if err := e.st.CreateTask(&child); err != nil {
		t.Fatal(err)
	}
	old := e.addTaskFile(t, parent, "docs/report.md", "text/markdown", "# v1")
	cur := e.addTaskFile(t, parent, "docs/report.md", "text/markdown", "# v2!")
	page := e.addTaskFile(t, parent, "site/index.html", "text/html; charset=utf-8", "<script>alert(1)</script>")
	pdf := e.addTaskFile(t, parent, "out/a.pdf", "application/pdf", "%PDF-1.4")
	e.addTaskFile(t, child, "logo.png", "image/png", "png")

	code, raw := e.do(t, "GET", "/api/tasks/"+itoa(parent.ID)+"/files", nil)
	if code != 200 {
		t.Fatalf("%d %s", code, raw)
	}
	var res struct {
		Files []struct {
			ID       int64  `json:"id"`
			Path     string `json:"path"`
			Versions []struct {
				ID int64 `json:"id"`
			} `json:"versions"`
		} `json:"files"`
		Subtasks []struct {
			Ref   string `json:"ref"`
			Files []struct {
				Path string `json:"path"`
			} `json:"files"`
		} `json:"subtasks"`
	}
	json.Unmarshal(raw, &res)
	if len(res.Files) != 3 || res.Files[2].ID != cur.ID || len(res.Files[2].Versions) != 1 || res.Files[2].Versions[0].ID != old.ID {
		t.Fatalf("grupare: %s", raw)
	}
	if len(res.Subtasks) != 1 || res.Subtasks[0].Ref != child.Ref() || res.Subtasks[0].Files[0].Path != "logo.png" {
		t.Fatalf("sub-task-uri: %s", raw)
	}

	get := func(path string, hdr map[string]string) *http.Response {
		req, _ := http.NewRequest("GET", e.ts.URL+path, nil)
		for k, v := range hdr {
			req.Header.Set(k, v)
		}
		r, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { r.Body.Close() })
		return r
	}
	r := get("/api/task-files/"+itoa(page.ID), nil)
	if r.Header.Get("Content-Security-Policy") != "sandbox" || !strings.HasPrefix(r.Header.Get("Content-Disposition"), "attachment") {
		t.Fatalf("html: %v", r.Header)
	}
	r = get("/api/task-files/"+itoa(pdf.ID), nil)
	if r.Header.Get("Content-Security-Policy") != "" || !strings.HasPrefix(r.Header.Get("Content-Disposition"), "inline") {
		t.Fatalf("pdf: %v", r.Header)
	}
	r = get("/api/task-files/"+itoa(pdf.ID)+"?download=1", nil)
	if !strings.HasPrefix(r.Header.Get("Content-Disposition"), "attachment") {
		t.Fatalf("download: %v", r.Header)
	}
	r = get("/api/task-files/"+itoa(cur.ID), map[string]string{"Range": "bytes=0-1"})
	if body, _ := io.ReadAll(r.Body); r.StatusCode != http.StatusPartialContent || string(body) != "# " {
		t.Fatalf("range: %d %q", r.StatusCode, body)
	}
	if code, _ := e.do(t, "GET", "/api/task-files/99999", nil); code != http.StatusNotFound {
		t.Fatalf("lipsa: %d", code)
	}

	// amprenta din GET /api/tasks/{id} si numarul de pe card
	code, raw = e.do(t, "GET", "/api/tasks/"+itoa(parent.ID), nil)
	if code != 200 || !strings.Contains(string(raw), `"filesCount":5`) {
		t.Fatalf("filesCount: %s", raw)
	}
	code, raw = e.do(t, "GET", "/api/projects/"+itoa(p.ID), nil)
	if code != 200 || !strings.Contains(string(raw), `"attachments":3`) {
		t.Fatalf("attachments pe card: %s", raw)
	}
}
```
`itoa` exista deja in `internal/web/server_test.go:147`.

- [ ] **Step 2: Ruleaza, trebuie sa pice**

Run: `go test ./internal/web -run TestTaskFilesAPI`
Expected: FAIL (404 pe `/api/tasks/{id}/files`)

- [ ] **Step 3: `serveStored` comun**

In `internal/web/api_files.go`, inlocuieste corpul de dupa `defer f.Close()` din `serveFile` cu un apel si adauga helper-ul:
```go
	serveStored(w, r, f, a.Name, a.Mime, a.CreatedAt, false)
}

// serveStored trimite un fisier salvat de noi (urcat de utilizator sau predat de un agent).
// Inline doar tipurile din inlineTypes; restul (HTML, SVG...) se descarca, in sandbox,
// ca un fisier scris de un agent sa nu ruleze pe originea aplicatiei.
func serveStored(w http.ResponseWriter, r *http.Request, f io.ReadSeeker, name, mimeType string, mod time.Time, download bool) {
	base, _, _ := strings.Cut(mimeType, ";")
	base = strings.TrimSpace(base)
	disposition := "attachment"
	if inlineTypes[base] && !download {
		disposition = "inline"
	}
	w.Header().Set("Content-Type", normalizeMime(mimeType)) // si fisierele salvate mai demult
	w.Header().Set("Cross-Origin-Resource-Policy", "same-origin")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	// viewerul de PDF din Chrome nu merge sub sandbox; restul tipurilor raman in sandbox
	if base != "application/pdf" {
		w.Header().Set("Content-Security-Policy", "sandbox")
	}
	w.Header().Set("Content-Disposition", mime.FormatMediaType(disposition, map[string]string{"filename": name}))
	http.ServeContent(w, r, name, mod, f)
}
```
(adauga `"time"` la import daca lipseste). Ruleaza `go test ./internal/web -run 'File'` ca testele existente pe `serveFile` sa treaca neschimbate.

- [ ] **Step 4: Handler-ele**

`internal/web/api_task_files.go`:
```go
package web

import (
	"net/http"
	"os"
	"path/filepath"
	"time"

	"foreman/internal/config"
	"foreman/internal/store"
)

// fileVersion: o predare mai veche a aceleiasi cai.
type fileVersion struct {
	ID        int64     `json:"id"`
	Size      int64     `json:"size"`
	AgentName string    `json:"agentName"`
	CreatedAt time.Time `json:"createdAt"`
}

// fileGroup: ultima versiune a unei cai, cu versiunile mai vechi.
type fileGroup struct {
	store.TaskFile
	Versions []fileVersion `json:"versions"`
}

// groupFiles: lista vine cea mai noua prima; prima aparitie a unei cai e versiunea curenta.
// Ordinea grupurilor = ordinea versiunilor curente (cel mai nou primul).
func groupFiles(list []store.TaskFile) []fileGroup {
	out := []fileGroup{}
	at := map[string]int{} // cale -> index in out
	for _, f := range list {
		if i, ok := at[f.Path]; ok {
			out[i].Versions = append(out[i].Versions, fileVersion{ID: f.ID, Size: f.Size, AgentName: f.AgentName, CreatedAt: f.CreatedAt})
			continue
		}
		at[f.Path] = len(out)
		out = append(out, fileGroup{TaskFile: f, Versions: []fileVersion{}})
	}
	return out
}

type subtaskFiles struct {
	TaskID int64       `json:"taskId"`
	Ref    string      `json:"ref"`
	Title  string      `json:"title"`
	Files  []fileGroup `json:"files"`
}

// getTaskFiles: tab-ul Files al modalei (fisierele task-ului si ale sub-task-urilor).
func (s *Server) getTaskFiles(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		fail(w, err)
		return
	}
	if _, err := s.st.GetTask(id); err != nil {
		fail(w, err)
		return
	}
	own, err := s.st.ListTaskFiles(id)
	if err != nil {
		fail(w, err)
		return
	}
	desc, err := s.st.ListDescendantFiles(id)
	if err != nil {
		fail(w, err)
		return
	}
	// desc vine ordonat dupa task: grupam pe task pastrand ordinea
	subs := []subtaskFiles{}
	var cur []store.TaskFile
	flush := func() {
		if len(cur) == 0 {
			return
		}
		sub := subtaskFiles{TaskID: cur[0].TaskID, Files: groupFiles(cur)}
		if t, err := s.st.GetTask(sub.TaskID); err == nil {
			sub.Ref, sub.Title = t.Ref(), t.Title
		}
		subs = append(subs, sub)
		cur = nil
	}
	for _, f := range desc {
		if len(cur) > 0 && cur[0].TaskID != f.TaskID {
			flush()
		}
		cur = append(cur, f)
	}
	flush()
	writeJSON(w, http.StatusOK, map[string]any{"files": groupFiles(own), "subtasks": subs})
}

// serveTaskFile: un fisier predat; ?download=1 forteaza descarcarea.
func (s *Server) serveTaskFile(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		fail(w, err)
		return
	}
	tf, err := s.st.GetTaskFile(id)
	if err != nil {
		fail(w, err)
		return
	}
	f, err := os.Open(filepath.Join(config.TaskFilesDir(s.dirs.Projects, tf.ProjectID, tf.TaskID), tf.Stored))
	if err != nil {
		if os.IsNotExist(err) {
			writeErr(w, http.StatusNotFound, "the file is missing from disk")
			return
		}
		fail(w, err)
		return
	}
	defer f.Close()
	serveStored(w, r, f, tf.Name, tf.Mime, tf.CreatedAt, r.URL.Query().Get("download") == "1")
}
```

`internal/web/server.go`, dupa ruta `/flow`:
```go
	mux.HandleFunc("GET /api/tasks/{id}/files", s.getTaskFiles)
	mux.HandleFunc("GET /api/task-files/{id}", s.serveTaskFile)
```

`internal/web/api_tasks.go`, in `getTask` dupa `changesCount`:
```go
	filesCount, err := s.st.CountTaskFilesTree(id)
	if err != nil {
		fail(w, err)
		return
	}
```
si in `writeJSON`, dupa `"changesCount": changesCount,`:
```go
		"filesCount":   filesCount, // fingerprint ieftin pentru tab-ul Files (task + sub-task-uri)
```

`internal/web/api_projects.go`: in `taskView` adauga
```go
	Attachments  int    `json:"attachments"` // cai distincte predate pe task (report_done files)
```
dupa `changed, err := s.st.CountChangedFiles(p.ID)` (+ verificarea erorii):
```go
	attached, err := s.st.CountTaskFiles(p.ID)
	if err != nil {
		fail(w, err)
		return
	}
```
si `views[i] = taskView{Task: t, FilesChanged: changed[t.ID], Attachments: attached[t.ID]}`.

- [ ] **Step 5: Ruleaza**

Run: `go test ./internal/web && go vet ./internal/web`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/web/api_task_files.go internal/web/task_files_test.go internal/web/api_files.go internal/web/server.go internal/web/api_tasks.go internal/web/api_projects.go
git commit -m "web: API pentru fisierele predate pe task" -- internal/web/api_task_files.go internal/web/task_files_test.go internal/web/api_files.go internal/web/server.go internal/web/api_tasks.go internal/web/api_projects.go
```

---

### Task 5: UI: eticheta pe card + tab-ul Files cu preview

**Files:**
- Create: `internal/web/static/js/task-files.js`
- Modify: `internal/web/static/js/task-modal.js` (tab-uri ~linia 60-69; `applyTab` ~linia 351; sync nou langa `syncChanges` ~linia 388)
- Modify: `internal/web/static/js/workspace.js:1053-1068` (card)
- Modify: `internal/web/static/app.css` (stiluri noi la final)

**Interfaces:**
- Consumes: `GET /api/tasks/{id}/files`, `GET /api/task-files/{id}`, `data.filesCount`, `t.attachments`; `renderMarkdown(text)` din `md.js`; `el`, `toast` din `ui.js`; `api` din `api.js`.
- Produces: `export function taskFilesView()` -> `{ el, show(data), error(msg) }`.

- [ ] **Step 1: Modulul `task-files.js`**

`internal/web/static/js/task-files.js`:
```js
import { el } from "./ui.js";
import { renderMarkdown } from "./md.js";

// Cat text se arata in preview (restul se descarca).
const PREVIEW_BYTES = 1 << 20;

// Extensiile afisate ca text (pe langa mime-urile text/*).
const TEXT_EXT = new Set(["txt", "md", "markdown", "json", "yaml", "yml", "csv", "tsv", "log", "xml", "ini", "toml",
  "go", "php", "js", "mjs", "ts", "py", "rb", "java", "kt", "sql", "sh", "css", "scss", "env", "conf"]);
const IMAGE_MIME = new Set(["image/png", "image/jpeg", "image/gif", "image/webp"]);

function ext(name) {
  const i = name.lastIndexOf(".");
  return i < 0 ? "" : name.slice(i + 1).toLowerCase();
}

// kindOfFile: cum se arata fisierul in preview ("image" | "pdf" | "markdown" | "text" | "none").
// HTML si SVG nu se arata niciodata (scrise de agenti, ar rula pe originea aplicatiei).
export function kindOfFile(f) {
  const base = (f.mime || "").split(";")[0].trim();
  const e = ext(f.name);
  if (e === "html" || e === "htm" || e === "svg" || base === "text/html" || base === "image/svg+xml") return "none";
  if (IMAGE_MIME.has(base)) return "image";
  if (base === "application/pdf") return "pdf";
  if (e === "md" || e === "markdown") return "markdown";
  if (base.startsWith("text/") || TEXT_EXT.has(e) || base === "application/json") return "text";
  return "none";
}

function size(n) {
  if (n < 1024) return `${n} B`;
  if (n < 1 << 20) return `${(n / 1024).toFixed(1)} KB`;
  return `${(n / (1 << 20)).toFixed(1)} MB`;
}

function ago(iso) {
  const s = Math.max(0, (Date.now() - new Date(iso).getTime()) / 1000);
  if (s < 60) return "just now";
  if (s < 3600) return `${Math.floor(s / 60)} min ago`;
  if (s < 86400) return `${Math.floor(s / 3600)} h ago`;
  return new Date(iso).toLocaleDateString("en-US", { month: "short", day: "numeric" });
}

const url = (id, download) => `/api/task-files/${id}${download ? "?download=1" : ""}`;

// taskFilesView: tab-ul "Files" al unui task. Click pe un rand deschide preview-ul in locul listei (cu Back).
export function taskFilesView() {
  const root = el("div", { class: "tfiles" });
  let last = null;       // ultimul raspuns aratat
  let previewId = null;  // fisierul deschis (ramane deschis la refresh)
  const open = new Set(); // cai cu "earlier versions" desfasurat

  function show(data) {
    last = data;
    if (previewId != null) {
      const f = findFile(previewId);
      if (f) return; // preview-ul ramane; lista se redeseneaza la Back
      previewId = null;
    }
    renderList();
  }

  function error(msg) {
    root.replaceChildren(el("p", { class: "hint" }, `Could not load the files: ${msg}`));
  }

  function findFile(id) {
    const groups = [...last.files, ...last.subtasks.flatMap((s) => s.files)];
    for (const g of groups) {
      if (g.id === id) return g;
      const v = g.versions.find((x) => x.id === id);
      if (v) return { ...g, ...v, versions: [] };
    }
    return null;
  }

  function renderList() {
    const nodes = [];
    if (last.files.length) nodes.push(el("ul", { class: "tfiles-list" }, last.files.map(row)));
    else nodes.push(el("p", { class: "hint" }, "No files were attached to this task."));
    if (last.subtasks.length) {
      nodes.push(el("h4", { class: "tfiles-sub-head" }, "From subtasks"));
      for (const s of last.subtasks) {
        nodes.push(el("div", { class: "tfiles-sub" },
          el("div", { class: "tfiles-sub-title" }, el("span", { class: "task-id" }, s.ref), " ", s.title),
          el("ul", { class: "tfiles-list" }, s.files.map(row))));
      }
    }
    root.replaceChildren(...nodes);
  }

  function row(g) {
    const dir = g.path.includes("/") ? g.path.slice(0, g.path.lastIndexOf("/") + 1) : "";
    const versions = g.versions.length
      ? el("button", { type: "button", class: "link small", onclick: () => { open.has(g.path) ? open.delete(g.path) : open.add(g.path); renderList(); } },
        `${g.versions.length} earlier ${g.versions.length === 1 ? "version" : "versions"}`)
      : null;
    return el("li", { class: "tfiles-row" },
      el("button", { type: "button", class: "tfiles-main", title: g.path, onclick: () => preview(g.id) },
        el("span", { class: "tfiles-name" }, g.name),
        dir ? el("span", { class: "tfiles-dir" }, dir) : null),
      el("span", { class: "tfiles-meta" }, `${size(g.size)} · ${g.agentName} · ${ago(g.createdAt)}`),
      versions,
      el("a", { class: "btn ghost small", href: url(g.id, true), download: g.name }, "Download"),
      open.has(g.path) ? el("ul", { class: "tfiles-versions" }, g.versions.map((v) => el("li", {},
        el("button", { type: "button", class: "link small", onclick: () => preview(v.id) }, ago(v.createdAt)),
        el("span", { class: "tfiles-meta" }, ` ${size(v.size)} · ${v.agentName} `),
        el("a", { class: "link small", href: url(v.id, true), download: g.name }, "Download")))) : null);
  }

  async function preview(id) {
    const f = findFile(id);
    if (!f) return;
    previewId = id;
    const back = el("button", { type: "button", class: "btn ghost small", onclick: () => { previewId = null; renderList(); } }, "← Back");
    const head = el("div", { class: "tfiles-preview-head" }, back,
      el("span", { class: "tfiles-name" }, f.name), el("span", { class: "tfiles-dir" }, f.path),
      el("span", { class: "grow" }),
      el("a", { class: "btn ghost small", href: url(id, true), download: f.name }, "Download"));
    const body = el("div", { class: "tfiles-preview-body" }, el("p", { class: "hint" }, "Loading..."));
    root.replaceChildren(head, body);
    const kind = kindOfFile(f);
    if (kind === "image") { body.replaceChildren(el("img", { src: url(id), alt: f.name })); return; }
    if (kind === "pdf") { body.replaceChildren(el("iframe", { src: url(id), title: f.name })); return; }
    if (kind === "none") { body.replaceChildren(el("p", { class: "hint" }, "No preview for this file type.")); return; }
    try {
      // doar primul MB: un log de 30 MB nu se incarca in modala
      const res = await fetch(url(id), { headers: { Range: `bytes=0-${PREVIEW_BYTES - 1}` } });
      if (!res.ok) throw new Error(`HTTP ${res.status}`);
      const text = await res.text();
      if (previewId !== id) return;
      const more = f.size > PREVIEW_BYTES ? el("p", { class: "hint" }, "Showing the first 1 MB. Download to see the rest.") : null;
      body.replaceChildren(kind === "markdown" ? el("div", { class: "md" }, renderMarkdown(text)) : el("pre", { class: "tfiles-text" }, text), more);
    } catch (err) {
      if (previewId === id) body.replaceChildren(el("p", { class: "hint" }, `Could not load the file: ${err.message}`));
    }
  }

  return { el: root, show, error };
}
```

Verifica in `md.js` ce intoarce `renderMarkdown` (un nod sau o lista de noduri): `sed -n 85,95p internal/web/static/js/md.js`. Daca intoarce un array, `el("div", { class: "md" }, renderMarkdown(text))` e corect (el() accepta array-uri, vezi `top.map(...)` in task-modal.js); daca intoarce un element, la fel. Verifica si ca o clasa `link`/`small` exista in app.css (`grep -n "\.link\b" internal/web/static/app.css`); daca nu, foloseste `btn ghost small` peste tot.

- [ ] **Step 2: Tab-ul in modala**

In `task-modal.js`:
- import: `import { taskFilesView } from "./task-files.js";`
- langa `chgCount`/`chgSeq`:
```js
  let filesCount = null;  // filesCount-ul pentru care s-a incarcat tab-ul Files (null = neincarcat)
  let filesSeq = 0;       // ca seq, pentru cererea Files
  const filesView = taskFilesView();
```
- dupa `tabChg`:
```js
  const filesPane = el("div", { id: `files-${taskId}`, role: "tabpanel", hidden: true }, filesView.el);
  const tabFiles = el("button", { type: "button", class: "tab", role: "tab", "aria-controls": filesPane.id, hidden: true, onclick: () => setTab("files") }, "Files");
```
- in `tabsSection`: lista de tab-uri devine `tabHist, tabConv, tabChg, tabFiles, tabFlow`, iar panourile `convPane, histPane, chgPane, filesPane, flowPane`.
- comentariul de la `let tab`: adauga `"files"`.
- `applyTab`:
```js
    const names = ["conversation", "history", "changes", "files", "flow"];
    const tabs = [tabConv, tabHist, tabChg, tabFiles, tabFlow];
    const panes = [convPane, histPane, chgPane, filesPane, flowPane];
```
si dupa `syncChanges();` adauga `syncFiles();`.
- functie noua langa `syncChanges`:
```js
  // syncFiles: tab-ul Files apare doar cand exista fisiere (pe task sau pe sub-task-uri);
  // /files se cere cand tab-ul e deschis si filesCount s-a schimbat
  async function syncFiles() {
    if (!data) return;
    const count = data.filesCount || 0;
    tabFiles.hidden = count === 0;
    if (count === 0 && tab === "files") { setTab("conversation"); return; }
    if (tab !== "files" || count === filesCount) return;
    const my = ++filesSeq;
    try {
      const res = await api(`/api/tasks/${taskId}/files`);
      if (destroyed || my !== filesSeq) return;
      filesCount = count;
      filesView.show(res);
    } catch (err) {
      if (destroyed || my !== filesSeq) return;
      filesCount = null;
      filesView.error(err.message);
    }
  }
```
- gaseste locul unde dupa fiecare `load()` se cheama `syncChanges()` (linia ~363, in `render`) si adauga `syncFiles();` imediat dupa, ca butonul sa apara si fara schimbare de tab.
- daca exista o alegere automata a tab-ului initial (`tab === null` la primul raspuns, `grep -n "tab === null\|tab = \"" internal/web/static/js/task-modal.js`), nu o schimba; Files se deschide doar la click sau cu `initialTab: "files"`.

- [ ] **Step 3: Eticheta pe card**

In `workspace.js`, in `task-foot`, imediat dupa linia cu `kids.length ? ... subtasks ...`:
```js
        t.attachments > 0 ? el("span", { class: "task-meta", title: `${t.attachments} attached ${t.attachments === 1 ? "file" : "files"}` }, `📎 ${t.attachments}`) : null,
```

- [ ] **Step 4: CSS**

La finalul `internal/web/static/app.css` (foloseste variabilele de culoare deja definite; verifica numele cu `grep -n "^  --" internal/web/static/app.css | head -30` si inlocuieste `--muted`, `--line`, `--panel` cu cele existente):
```css
/* Files: fisierele predate pe task */
.tfiles-list { list-style: none; margin: 0; padding: 0; }
.tfiles-row { display: flex; flex-wrap: wrap; align-items: center; gap: 8px; padding: 8px 0; border-bottom: 1px solid var(--line); }
.tfiles-main { display: flex; align-items: baseline; gap: 6px; min-width: 0; flex: 1; background: none; border: 0; padding: 0; color: inherit; font: inherit; text-align: left; cursor: pointer; }
.tfiles-main:hover .tfiles-name { text-decoration: underline; }
.tfiles-name { font-weight: 600; overflow-wrap: anywhere; }
.tfiles-dir, .tfiles-meta { color: var(--muted); font-size: 12px; overflow-wrap: anywhere; }
.tfiles-versions { list-style: none; flex-basis: 100%; margin: 4px 0 0 12px; padding: 0; display: grid; gap: 4px; }
.tfiles-sub-head { margin: 16px 0 4px; font-size: 13px; color: var(--muted); }
.tfiles-sub { margin-bottom: 8px; }
.tfiles-sub-title { font-size: 13px; margin: 6px 0 2px; }
.tfiles-preview-head { display: flex; flex-wrap: wrap; align-items: center; gap: 8px; margin-bottom: 8px; }
.tfiles-preview-body img { max-width: 100%; height: auto; display: block; border-radius: 6px; }
.tfiles-preview-body iframe { width: 100%; height: 70vh; border: 1px solid var(--line); border-radius: 6px; background: #fff; }
.tfiles-text { max-height: 60vh; overflow: auto; white-space: pre-wrap; overflow-wrap: anywhere; font-size: 12px; padding: 12px; border-radius: 6px; background: var(--panel); }
```

- [ ] **Step 5: Verificare sintaxa JS**

Run:
```bash
cd internal/web/static/js && for f in task-files task-modal workspace; do cp $f.js /tmp/check-$f.mjs && node --check /tmp/check-$f.mjs || echo "FAIL $f"; done
```
Expected: fara iesire `FAIL`.

- [ ] **Step 6: Verificare vizuala**

Construieste si porneste o instanta de test (port liber, alta baza de date, ca sa nu atingi `~/.foreman`):
```bash
go build -o /tmp/foreman-test ./cmd/foreman && /tmp/foreman-test --help
```
Foloseste flag-urile reale din `--help` pentru port si folderul de date. Creeaza un proiect pe un folder temporar, un task, apoi insereaza 4 fisiere (un `.png`, un `.md`, un `.log` de 2 MB, un `.html`) si un sub-task cu un fisier, prin `sqlite3` + copii in `task-files/<task>/` (aceeasi forma ca in `addTaskFile` din Task 4). Deschide modala cu Chrome headless prin CDP (vezi memoria "Verifica UI-ul cu screenshot-uri") si fa screenshot-uri pentru: card cu `📎 N`; tab-ul Files (lista + From subtasks); preview imagine; preview Markdown; preview log cu "Showing the first 1 MB"; HTML cu "No preview". Verifica si latimea de telefon (390px): randurile se rup, fara scroll orizontal.

- [ ] **Step 7: Commit**

```bash
git add internal/web/static/js/task-files.js internal/web/static/js/task-modal.js internal/web/static/js/workspace.js internal/web/static/app.css
git commit -m "ui: tab-ul Files cu preview si eticheta pe card" -- internal/web/static/js/task-files.js internal/web/static/js/task-modal.js internal/web/static/js/workspace.js internal/web/static/app.css
```
(inainte, `git diff --stat` pe aceste fisiere: daca au modificari straine, opreste-te si intreaba.)

---

### Task 6: Verificare finala + CLAUDE.md

**Files:**
- Modify: `agentdash/CLAUDE.md` (sectiunea "Stare", dupa linia "Gata (reguli pentru comenzi, ...)")
- Modify: `docs/superpowers/specs/2026-10-08-foreman-task-files-design.md` (Status)

- [ ] **Step 1: Verificarile din conventii**

Run:
```bash
go vet ./... && go build -race ./... && go test ./... && GOOS=darwin GOARCH=arm64 go build -o /dev/null ./cmd/foreman && GOOS=windows go build -o /dev/null ./cmd/foreman
```
Expected: toate trec.

- [ ] **Step 2: CLAUDE.md si spec**

In `CLAUDE.md`, dupa paragraful "Gata (reguli pentru comenzi, ...)":
```
Gata (fisiere pe task, spec in docs/superpowers/specs/2026-10-08-foreman-task-files-design.md): report_done files
(max 20) copiaza livrabilele ca snapshot in projects/<pid>/task-files/<task>/ (task_files, migrarea 024; pachet
internal/taskfiles, doar din folderele proiectului, prin os.OpenRoot); tab-ul Files din modala (preview imagine/PDF/
Markdown/text, Download, versiuni, "From subtasks"), eticheta 📎 N pe card; HTML/SVG nu se servesc inline.
```
In spec: `Status: implementat (2026-10-08).`

- [ ] **Step 3: Commit**

```bash
git add CLAUDE.md docs/superpowers/specs/2026-10-08-foreman-task-files-design.md
git commit -m "docs: fisiere pe task gata" -- CLAUDE.md docs/superpowers/specs/2026-10-08-foreman-task-files-design.md
```
