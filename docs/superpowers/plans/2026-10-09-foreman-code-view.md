# Cod colorat, tab-ul Modified si modala de fisier — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Cod colorat (highlight.js) in markdown, diff-uri si previzualizari; tab-ul Modified pe task; o modala care arata fisierul de acum cu liniile schimbate de task marcate pe margine.

**Architecture:** Marcajele se calculeaza in Go din `task_changes` (`store.FileMarkers`), pe fragmentele de diff aplicate in ordine. Doua endpoint-uri noi (`/api/tasks/{id}/modified`, `/api/tasks/{id}/file`) citesc doar cai din `task_changes`, prin `os.OpenRoot`. In UI, un modul `highlight.js` peste highlight.js 11 (inclus in `static/vendor/`), un modul `file-view.js` pentru modala si un tab nou in `task-modal.js`.

**Tech Stack:** Go (stdlib), JS vanilla (module ES), highlight.js 11.9.0 (BSD-3, inclus in binar).

**Spec:** `docs/superpowers/specs/2026-10-09-foreman-code-view-design.md`

## Global Constraints

- `go vet ./...` si `go build -race ./...` trebuie sa treaca; `go test ./...` verde.
- JS: copiaza fisierul ca `.mjs` si ruleaza `node --check` (pe `.js` nu prinde erorile de sintaxa).
- Cross-compile: `GOOS=darwin GOARCH=arm64 go build ./cmd/foreman` si `GOOS=windows go build ./cmd/foreman`.
- Tot ce vede utilizatorul: engleza. Comentariile din cod: romana, fara diacritice.
- Fara CDN la runtime: highlight.js se descarca o singura data si se pune in `internal/web/static/vendor/highlight/`.
- Limite: colorare doar pana la 5000 de linii / 500 KB; fisier in modala pana la 1 MB; binar = octet NUL in primii 8 KB.
- Commit-uri: `zona: descriere scurta in romana`.

## Review Focus

1. O cale cu `..` sau un symlink care iese din folderul proiectului nu se citeste niciodata — test in Task 2 (`../`) si `os.OpenRoot` (symlink).
2. Un fisier modificat pe un sub-task se deschide din task-ul parinte (`&task=`), dar un `task=` care nu e sub-task al lui da 404 — test in Task 2.
3. Un fisier cu mai multe edit-uri succesive pe linii diferite are marcajele pe liniile corecte in versiunea finala — test in Task 1 (`TestFileMarkersShift`).
4. Un diff foarte mare nu blocheaza UI-ul: colorarea se opreste peste 5000 de linii — verificat in Task 3 (`highlightInto` cu limita) si la screenshot.
5. Textul scris de agent nu poate injecta HTML prin colorare (highlight.js scapa textul) — verificat in Task 3 cu un bloc ` ```html <script>` la screenshot.

---

### Task 1: Marcajele (`store.FileMarkers`)

**Files:**
- Create: `internal/store/markers.go`
- Test: `internal/store/markers_test.go`

**Interfaces:**
- Produces:

```go
// Markers: liniile (numerotate de la 1) din versiunea de acum a fisierului.
type Markers struct {
	Added    []int `json:"added"`
	Modified []int `json:"modified"`
	Deleted  []int `json:"deleted"` // linia inaintea careia s-a sters ceva (len+1 = la sfarsit)
}
func FileMarkers(changes []TaskChange) (Markers, bool) // bool = exact
```

- [ ] **Step 1: Testele (esueaza)**

`internal/store/markers_test.go`:

```go
package store

import (
	"slices"
	"testing"
)

func edit(hunks ...Hunk) TaskChange { return TaskChange{Kind: "edit", Patch: hunks} }

func TestFileMarkersCreate(t *testing.T) {
	m, ok := FileMarkers([]TaskChange{{Kind: "create", Patch: []Hunk{{0, 0, 1, 3, []string{"+a", "+b", "+c"}}}}})
	if !ok || !slices.Equal(m.Added, []int{1, 2, 3}) || len(m.Modified) != 0 || len(m.Deleted) != 0 {
		t.Fatalf("%+v %v", m, ok)
	}
}

func TestFileMarkersReplaceAndDelete(t *testing.T) {
	// linia 2 inlocuita, linia 5 stearsa (fisier de 6 linii)
	m, ok := FileMarkers([]TaskChange{edit(
		Hunk{1, 3, 1, 3, []string{" one", "-two", "+TWO", " three"}},
		Hunk{4, 3, 4, 2, []string{" four", "-five", " six"}},
	)})
	if !ok || !slices.Equal(m.Modified, []int{2}) || !slices.Equal(m.Deleted, []int{5}) || len(m.Added) != 0 {
		t.Fatalf("%+v", m)
	}
}

// al doilea edit adauga doua linii deasupra: marcajul primului coboara cu 2
func TestFileMarkersShift(t *testing.T) {
	m, _ := FileMarkers([]TaskChange{
		edit(Hunk{9, 3, 9, 3, []string{" a", "-b", "+B", " c"}}),          // linia 10 modificata
		edit(Hunk{1, 1, 1, 3, []string{" top", "+new1", "+new2"}}),         // doua linii noi dupa linia 1
	})
	if !slices.Equal(m.Added, []int{2, 3}) || !slices.Equal(m.Modified, []int{12}) {
		t.Fatalf("%+v", m)
	}
}

// o linie adaugata, apoi editata din nou: ramane "added"
func TestFileMarkersAddedThenEdited(t *testing.T) {
	m, _ := FileMarkers([]TaskChange{
		edit(Hunk{1, 1, 1, 2, []string{" a", "+b"}}),
		edit(Hunk{1, 2, 1, 2, []string{" a", "-b", "+b2"}}),
	})
	if !slices.Equal(m.Added, []int{2}) || len(m.Modified) != 0 {
		t.Fatalf("%+v", m)
	}
}

func TestFileMarkersNotExact(t *testing.T) {
	// fara numere de linie (fallback fara structuredPatch) sau trunchiat: fara marcaje
	for _, c := range []TaskChange{
		{Kind: "edit", Patch: []Hunk{{0, 0, 0, 0, []string{"-x", "+y"}}}},
		{Kind: "edit", Truncated: true, Patch: []Hunk{{1, 1, 1, 1, []string{"-x", "+y"}}}},
		{Kind: "update"}, // Write peste un fisier existent, fara fragmente
	} {
		if m, ok := FileMarkers([]TaskChange{c}); ok || len(m.Added)+len(m.Modified)+len(m.Deleted) != 0 {
			t.Fatalf("%+v: %+v %v", c, m, ok)
		}
	}
}
```

- [ ] **Step 2: Ruleaza, verifica ca esueaza**

Run: `go test ./internal/store/ -run FileMarkers`
Expected: FAIL la compilare (`FileMarkers`, `Markers` nedefinite).

- [ ] **Step 3: Implementarea**

`internal/store/markers.go`:

```go
package store

import "strings"

// Markers: liniile (numerotate de la 1) din versiunea de acum a fisierului, schimbate de un task.
type Markers struct {
	Added    []int `json:"added"`
	Modified []int `json:"modified"`
	Deleted  []int `json:"deleted"` // linia inaintea careia s-a sters ceva (len+1 = la sfarsit)
}

// bitii unei linii in calcul
const (
	markAdded    = 1
	markModified = 2
	markDeleted  = 4 // s-a sters ceva chiar inaintea liniei
)

// FileMarkers aplica in ordine fragmentele de diff ale unui fisier (modificarile unui task, deja
// filtrate pe cale) si intoarce liniile marcate in versiunea finala. exact=false cand un fragment
// nu are numere de linie, e trunchiat sau un Write a rescris fisierul fara fragmente: atunci nu
// se poate spune unde sunt liniile, deci nu intoarcem marcaje.
func FileMarkers(changes []TaskChange) (Markers, bool) {
	var marks []uint8 // marks[i] = linia i+1 din versiunea curenta; lipsa = nemarcata
	for _, c := range changes {
		if c.Truncated || (c.Kind != "create" && len(c.Patch) == 0) {
			return Markers{Added: []int{}, Modified: []int{}, Deleted: []int{}}, false
		}
		// fragmentele unei modificari sunt in ordine; aplicate de jos in sus, pozitiile vechi raman valabile
		for i := len(c.Patch) - 1; i >= 0; i-- {
			h := c.Patch[i]
			if h.OldStart == 0 && h.NewStart == 0 {
				return Markers{Added: []int{}, Modified: []int{}, Deleted: []int{}}, false
			}
			marks = applyHunk(marks, h)
		}
	}
	out := Markers{Added: []int{}, Modified: []int{}, Deleted: []int{}}
	for i, m := range marks {
		if m&markAdded != 0 {
			out.Added = append(out.Added, i+1)
		} else if m&markModified != 0 {
			out.Modified = append(out.Modified, i+1)
		}
		if m&markDeleted != 0 {
			out.Deleted = append(out.Deleted, i+1)
		}
	}
	return out, true
}

// applyHunk inlocuieste in marks zona veche a fragmentului cu liniile noi, marcate.
func applyHunk(marks []uint8, h Hunk) []uint8 {
	pos := h.OldStart - 1 // indexul primei linii vechi din fragment
	if h.OldLines == 0 {
		pos = h.OldStart // fragment doar cu linii noi: ele vin dupa linia OldStart
	}
	if pos < 0 {
		pos = 0
	}
	for len(marks) < pos+h.OldLines+1 {
		marks = append(marks, 0) // fisierul e mai lung decat ce stim: liniile nemarcate
	}
	old := marks[pos : pos+h.OldLines]
	next := []uint8{}
	oi := 0        // linia veche curenta din fragment
	pending := 0   // linii "-" inca neinlocuite de un "+"
	carry := uint8(0)
	for _, l := range h.Lines {
		if l == "" || strings.HasPrefix(l, "\\") {
			continue
		}
		switch l[0] {
		case '-':
			if oi < len(old) {
				carry |= old[oi] & markDeleted // un marcaj de stergere de pe linia scoasa trece mai jos
			}
			oi++
			pending++
		case '+':
			m := uint8(markAdded)
			if pending > 0 {
				pending--
				m = markModified
			}
			// o linie adaugata de task si inlocuita tot de el ramane "added"
			if m == markModified && oi > 0 && oi-1 < len(old) && old[oi-1]&markAdded != 0 {
				m = markAdded
			}
			next = append(next, m|carry)
			carry = 0
		default: // context
			m := uint8(0)
			if oi < len(old) {
				m = old[oi]
			}
			oi++
			if pending > 0 {
				m |= markDeleted // s-au sters linii fara inlocuire, chiar inaintea acestei linii
				pending = 0
			}
			next = append(next, m|carry)
			carry = 0
		}
	}
	rest := marks[pos+h.OldLines:]
	if pending > 0 || carry != 0 {
		// stergere la capatul fragmentului: marcajul trece pe prima linie de dupa el
		if len(rest) == 0 {
			rest = []uint8{0}
		}
		rest = append([]uint8{rest[0] | markDeleted | carry}, rest[1:]...)
	}
	out := make([]uint8, 0, len(marks)-h.OldLines+len(next))
	out = append(out, marks[:pos]...)
	out = append(out, next...)
	return append(out, rest...)
}
```

- [ ] **Step 4: Ruleaza testele**

Run: `go test ./internal/store/ -run FileMarkers -v`
Expected: PASS pe toate cinci. Daca `TestFileMarkersReplaceAndDelete` da `Deleted: [5]` dar si o linie in plus din padding (`rest = []uint8{0}`), verifica: un marcaj pe o linie de dupa ultima linie reala apare ca `len+1` (stergere la sfarsit), asa e cerut de spec.

- [ ] **Step 5: Commit**

```bash
git add internal/store/markers.go internal/store/markers_test.go
git commit -m "store: marcajele unui fisier din diff-urile task-ului (FileMarkers)"
```

---

### Task 2: Endpoint-urile `/modified` si `/file`

**Files:**
- Create: `internal/web/api_code.go`
- Modify: `internal/web/server.go` (rutele)
- Test: `internal/web/code_test.go`

**Interfaces:**
- Consumes: `store.FileMarkers`, `store.Markers` (Task 1); `store.ListTaskChanges`, `store.Subtasks`, `store.HomeFolder`, `config.ProjectFilesDir`
- Produces:
  - `GET /api/tasks/{id}/modified` -> `{"files": [modifiedView]}`
  - `GET /api/tasks/{id}/file?path=&task=` -> `fileView`

```go
type modifiedView struct {
	TaskID  int64     `json:"taskId"`
	Ref     string    `json:"ref"`
	Path    string    `json:"path"`
	Added   int       `json:"added"`
	Removed int       `json:"removed"`
	Created bool      `json:"created"`
	Edits   int       `json:"edits"`
	Agent   string    `json:"agent"`
	At      time.Time `json:"at"`
}
type fileView struct {
	Path     string          `json:"path"`
	Content  string          `json:"content"`
	Exists   bool            `json:"exists"`
	Binary   bool            `json:"binary"`
	TooLarge bool            `json:"tooLarge"`
	Markers  store.Markers   `json:"markers"`
	Exact    bool            `json:"exact"`
	Runs     []runChangesView `json:"runs"` // diff-urile fisierului, pe run-uri (ca /changes)
}
```

- [ ] **Step 1: Testele (esueaza)**

`internal/web/code_test.go`:

```go
package web

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"foreman/internal/store"
)

// codeSetup: un proiect cu un task al lui Beacon si doua fisiere modificate in folderul proiectului.
func codeSetup(t *testing.T) (*env, store.Project, store.Agent, store.Task, string) {
	t.Helper()
	e := setup(t)
	dir := t.TempDir()
	p := e.createProject(t, dir)
	w := e.addWorker(t, p.ID, "Beacon")
	task := store.Task{ProjectID: p.ID, Title: "A", AssigneeID: w.ID}
	if err := e.st.CreateTask(&task); err != nil {
		t.Fatal(err)
	}
	os.MkdirAll(filepath.Join(dir, "app"), 0o755)
	os.WriteFile(filepath.Join(dir, "app", "x.php"), []byte("<?php\n$a = 1;\n$b = 2;\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "secret.txt"), []byte("nope"), 0o644)
	e.st.AddTaskChanges([]store.TaskChange{
		{TaskID: task.ID, RunID: "r1", AgentID: &w.ID, AgentName: "Beacon", Kind: "create", Tool: "Write", Path: "app/x.php",
			Patch: []store.Hunk{{OldStart: 0, OldLines: 0, NewStart: 1, NewLines: 2, Lines: []string{"+<?php", "+$a = 1;"}}}},
		{TaskID: task.ID, RunID: "r2", AgentID: &w.ID, AgentName: "Beacon", Kind: "edit", Tool: "Edit", Path: "app/x.php",
			Patch: []store.Hunk{{OldStart: 2, OldLines: 1, NewStart: 2, NewLines: 2, Lines: []string{" $a = 1;", "+$b = 2;"}}}},
	})
	return e, p, w, task, dir
}

func TestModifiedFiles(t *testing.T) {
	e, p, w, task, _ := codeSetup(t)
	sub := store.Task{ProjectID: p.ID, Title: "B", AssigneeID: w.ID, ParentTaskID: &task.ID}
	e.st.CreateTask(&sub)
	e.st.AddTaskChanges([]store.TaskChange{{TaskID: sub.ID, RunID: "r3", AgentName: "Beacon", Kind: "edit", Tool: "Edit", Path: "README.md",
		Patch: []store.Hunk{{OldStart: 1, OldLines: 1, NewStart: 1, NewLines: 1, Lines: []string{"-a", "+b"}}}}})
	code, raw := e.do(t, "GET", "/api/tasks/"+itoa(task.ID)+"/modified", nil)
	var d struct{ Files []modifiedView }
	json.Unmarshal(raw, &d)
	if code != 200 || len(d.Files) != 2 {
		t.Fatalf("%d %s", code, raw)
	}
	f := d.Files[0]
	if f.Path != "app/x.php" || f.Added != 3 || f.Removed != 0 || !f.Created || f.Edits != 2 || f.Ref != "T-1" || f.Agent != "Beacon" {
		t.Fatalf("%+v", f)
	}
	if d.Files[1].Ref != "T-2" || d.Files[1].Path != "README.md" || d.Files[1].Added != 1 || d.Files[1].Removed != 1 {
		t.Fatalf("%+v", d.Files[1])
	}
}

func TestFileView(t *testing.T) {
	e, _, _, task, _ := codeSetup(t)
	code, raw := e.do(t, "GET", "/api/tasks/"+itoa(task.ID)+"/file?path=app/x.php", nil)
	var v fileView
	json.Unmarshal(raw, &v)
	if code != 200 || !v.Exists || !strings.Contains(v.Content, "$b = 2;") || !v.Exact || len(v.Markers.Added) != 3 || len(v.Runs) != 2 {
		t.Fatalf("%d %s", code, raw)
	}
	// cai care nu sunt in task_changes, iesiri din folder, sub-task strain: 404
	for _, q := range []string{"path=secret.txt", "path=../x.php", "path=app/../secret.txt", "path=app/x.php&task=99999"} {
		if code, raw := e.do(t, "GET", "/api/tasks/"+itoa(task.ID)+"/file?"+q, nil); code != http.StatusNotFound {
			t.Fatalf("%s: %d %s", q, code, raw)
		}
	}
}

func TestFileViewLimits(t *testing.T) {
	e, _, w, task, dir := codeSetup(t)
	os.WriteFile(filepath.Join(dir, "big.log"), []byte(strings.Repeat("x", 1<<20+1)), 0o644)
	os.WriteFile(filepath.Join(dir, "img.bin"), []byte("PNG\x00\x01"), 0o644)
	for _, p := range []string{"big.log", "img.bin", "gone.txt"} {
		e.st.AddTaskChanges([]store.TaskChange{{TaskID: task.ID, RunID: "r9", AgentID: &w.ID, AgentName: "Beacon", Kind: "edit", Tool: "Edit", Path: p,
			Patch: []store.Hunk{{OldStart: 1, OldLines: 1, NewStart: 1, NewLines: 1, Lines: []string{"-a", "+b"}}}}})
	}
	get := func(p string) fileView {
		_, raw := e.do(t, "GET", "/api/tasks/"+itoa(task.ID)+"/file?path="+p, nil)
		var v fileView
		json.Unmarshal(raw, &v)
		return v
	}
	if v := get("big.log"); !v.TooLarge || v.Content != "" {
		t.Fatalf("big: %+v", v.TooLarge)
	}
	if v := get("img.bin"); !v.Binary || v.Content != "" {
		t.Fatal("binar")
	}
	if v := get("gone.txt"); v.Exists || len(v.Runs) != 1 {
		t.Fatalf("lipsa: %+v", v)
	}
}
```

- [ ] **Step 2: Ruleaza, verifica ca esueaza**

Run: `go test ./internal/web/ -run 'ModifiedFiles|FileView'`
Expected: FAIL la compilare (`modifiedView`, `fileView` nedefinite).

- [ ] **Step 3: Implementarea**

`internal/web/api_code.go`:

```go
package web

import (
	"errors"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"foreman/internal/config"
	"foreman/internal/store"
)

const maxFileView = 1 << 20 // 1 MB: peste atat modala nu arata continutul

type modifiedView struct {
	TaskID  int64     `json:"taskId"`
	Ref     string    `json:"ref"`
	Path    string    `json:"path"`
	Added   int       `json:"added"`
	Removed int       `json:"removed"`
	Created bool      `json:"created"`
	Edits   int       `json:"edits"`
	Agent   string    `json:"agent"`
	At      time.Time `json:"at"`
}

type fileView struct {
	Path     string           `json:"path"`
	Content  string           `json:"content"`
	Exists   bool             `json:"exists"`
	Binary   bool             `json:"binary"`
	TooLarge bool             `json:"tooLarge"`
	Markers  store.Markers    `json:"markers"`
	Exact    bool             `json:"exact"`
	Runs     []runChangesView `json:"runs"`
}

// taskTree intoarce task-ul si toate sub-task-urile lui (recursiv), in ordinea: task-ul, apoi dupa numar.
func (s *Server) taskTree(task store.Task) ([]store.Task, error) {
	out := []store.Task{task}
	for i := 0; i < len(out); i++ {
		subs, err := s.st.Subtasks(out[i].ID)
		if err != nil {
			return nil, err
		}
		slices.SortFunc(subs, func(a, b store.Task) int { return a.Number - b.Number })
		out = append(out, subs...)
	}
	return out, nil
}

// fileChanges: modificarile de fisier (fara comenzi) ale task-ului, pe cale, in ordine.
func fileChanges(changes []store.TaskChange) (map[string][]store.TaskChange, []string) {
	by := map[string][]store.TaskChange{}
	order := []string{}
	for _, c := range changes {
		if c.Kind == "command" || c.Path == "" {
			continue
		}
		if _, ok := by[c.Path]; !ok {
			order = append(order, c.Path)
		}
		by[c.Path] = append(by[c.Path], c)
	}
	return by, order
}

// getTaskModified: fisierele modificate de task si de sub-task-urile lui (tab-ul Modified).
func (s *Server) getTaskModified(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		fail(w, err)
		return
	}
	task, err := s.st.GetTask(id)
	if err != nil {
		fail(w, err)
		return
	}
	tree, err := s.taskTree(task)
	if err != nil {
		fail(w, err)
		return
	}
	files := []modifiedView{}
	for _, tk := range tree {
		changes, err := s.st.ListTaskChanges(tk.ID)
		if err != nil {
			fail(w, err)
			return
		}
		by, order := fileChanges(changes)
		slices.Sort(order)
		for _, path := range order {
			cs := by[path]
			v := modifiedView{TaskID: tk.ID, Ref: tk.Ref(), Path: path, Created: cs[0].Kind == "create", Edits: len(cs)}
			for _, c := range cs {
				for _, h := range c.Patch {
					for _, l := range h.Lines {
						switch {
						case strings.HasPrefix(l, "+"):
							v.Added++
						case strings.HasPrefix(l, "-"):
							v.Removed++
						}
					}
				}
				v.Agent, v.At = c.AgentName, c.At
			}
			files = append(files, v)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"files": files})
}

// getTaskFile: fisierul de acum de pe disc, cu marcajele task-ului. Doar cai din task_changes.
func (s *Server) getTaskFile(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		fail(w, err)
		return
	}
	task, err := s.st.GetTask(id)
	if err != nil {
		fail(w, err)
		return
	}
	// task=: un sub-task al task-ului din URL (fisierele din tab-ul Modified pot fi ale sub-task-urilor)
	target := task
	if q := r.URL.Query().Get("task"); q != "" && q != strconv.FormatInt(id, 10) {
		tree, err := s.taskTree(task)
		if err != nil {
			fail(w, err)
			return
		}
		sub, _ := strconv.ParseInt(q, 10, 64)
		i := slices.IndexFunc(tree, func(t store.Task) bool { return t.ID == sub })
		if i < 0 {
			fail(w, store.ErrNotFound)
			return
		}
		target = tree[i]
	}
	path := r.URL.Query().Get("path")
	changes, err := s.st.ListTaskChanges(target.ID)
	if err != nil {
		fail(w, err)
		return
	}
	by, _ := fileChanges(changes)
	cs, ok := by[path]
	if !ok {
		fail(w, store.ErrNotFound)
		return
	}
	v := fileView{Path: path, Runs: groupChanges(cs)}
	v.Markers, v.Exact = store.FileMarkers(cs)
	content, err := s.readTaskFile(target.ProjectID, cs[len(cs)-1].AgentID, path)
	switch {
	case errors.Is(err, errOutside):
		fail(w, store.ErrNotFound)
		return
	case errors.Is(err, fs.ErrNotExist):
		// fisierul a fost sters: modala arata Diff
	case errors.Is(err, errTooLarge):
		v.Exists, v.TooLarge = true, true
	case err != nil:
		fail(w, err)
		return
	default:
		v.Exists = true
		head := content
		if len(head) > 8192 {
			head = head[:8192]
		}
		if strings.IndexByte(head, 0) >= 0 {
			v.Binary = true
		} else {
			v.Content = content
		}
	}
	writeJSON(w, http.StatusOK, v)
}

var (
	errOutside  = errors.New("the path is outside the project folders")
	errTooLarge = errors.New("the file is too large")
)

// readTaskFile citeste o cale din task_changes: relativa la folderul agentului (sau, daca agentul nu
// mai exista, la folderele proiectului in ordine), absoluta doar intr-un folder al proiectului.
// Citirea trece prin os.OpenRoot, deci ".." si symlink-urile in afara folderului sunt refuzate.
func (s *Server) readTaskFile(projectID int64, agentID *int64, path string) (string, error) {
	p, err := s.st.GetProject(projectID)
	if err != nil {
		return "", err
	}
	roots := []string{}
	if agentID != nil {
		if a, err := s.st.GetAgent(*agentID); err == nil {
			roots = append(roots, store.HomeFolder(a, p.Folders).Path)
		}
	}
	for _, f := range p.Folders {
		roots = append(roots, f.Path)
	}
	roots = append(roots, config.ProjectFilesDir(s.dirs.Projects, projectID))

	if filepath.IsAbs(path) {
		for _, root := range roots {
			rel, err := filepath.Rel(root, path)
			if err == nil && rel != "." && !strings.HasPrefix(rel, "..") {
				return readIn(root, rel)
			}
		}
		return "", errOutside
	}
	if !filepath.IsLocal(path) {
		return "", errOutside
	}
	var last error = fs.ErrNotExist
	for _, root := range roots {
		txt, err := readIn(root, path)
		if err == nil || !errors.Is(err, fs.ErrNotExist) {
			return txt, err
		}
		last = err
	}
	return "", last
}

// readIn citeste rel din root prin os.OpenRoot, cu limita maxFileView.
func readIn(root, rel string) (string, error) {
	rt, err := os.OpenRoot(root)
	if err != nil {
		return "", err
	}
	defer rt.Close()
	f, err := rt.Open(rel)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			return "", errOutside // iesire din root (symlink, ..) sau alta eroare de cale
		}
		return "", err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, maxFileView+1))
	if err != nil {
		return "", err
	}
	if len(b) > maxFileView {
		return "", errTooLarge
	}
	return string(b), nil
}
```

Atentie la `readIn`: `os.Root.Open` intoarce o eroare care nu e `fs.ErrNotExist` si pentru un director sau permisiuni; asta devine 404 (`errOutside`), ceea ce e acceptabil pentru o cale care nu e fisier.

`internal/web/server.go`, langa `GET /api/tasks/{id}/changes`:

```go
	mux.HandleFunc("GET /api/tasks/{id}/modified", s.getTaskModified)
	mux.HandleFunc("GET /api/tasks/{id}/file", s.getTaskFile)
```

- [ ] **Step 4: Ruleaza testele**

Run: `go vet ./internal/web/ && go test ./internal/web/ -run 'ModifiedFiles|FileView' -v`
Expected: PASS. In `TestModifiedFiles`, `Added` pentru `app/x.php` e 3 (2 din create + 1 din edit).

- [ ] **Step 5: Commit**

```bash
git add internal/web/api_code.go internal/web/server.go internal/web/code_test.go
git commit -m "web: /modified si /file pe task (doar cai din task_changes, prin os.OpenRoot)"
```

---

### Task 3: Evidentierea de sintaxa

**Files:**
- Create: `internal/web/static/vendor/highlight/highlight.min.js`, `internal/web/static/vendor/highlight/LICENSE`
- Create: `internal/web/static/js/highlight.js`
- Modify: `internal/web/static/js/md.js`, `internal/web/static/js/changes.js`, `internal/web/static/js/task-files.js`, `internal/web/static/app.css`

**Interfaces:**
- Produces (`static/js/highlight.js`):
  - `languageFor(path: string): string|null`
  - `highlightInto(el: HTMLElement, text: string, language: string|null): void`
  - `highlightLine(text: string, language: string|null): DocumentFragment|string` — fragment colorat sau textul simplu

- [ ] **Step 1: Biblioteca**

```bash
V=internal/web/static/vendor/highlight && mkdir -p $V
curl -fsSL https://cdnjs.cloudflare.com/ajax/libs/highlight.js/11.9.0/es/highlight.min.js -o $V/highlight.min.js
curl -fsSL https://raw.githubusercontent.com/highlightjs/highlight.js/11.9.0/LICENSE -o $V/LICENSE
head -c 300 $V/highlight.min.js; grep -c "export" $V/highlight.min.js; wc -c $V/highlight.min.js
```

Expected: fisierul e un modul ES (contine `export`), ~120 KB; LICENSE incepe cu "BSD 3-Clause License". Verifica limbajele cu node:

```bash
cp $V/highlight.min.js /tmp/hl.mjs && node -e 'import("/tmp/hl.mjs").then(m => { const l = m.default.listLanguages(); console.log(l.length, ["php","javascript","typescript","go","python","sql","json","yaml","bash","css","xml","java","csharp","rust"].filter(x => !l.includes(x))) })'
```

Expected: `>= 30 []` (niciun limbaj din lista lipsa). Daca lipsesc, foloseste build-ul `highlight.min.js` (non-ES) de la aceeasi versiune si incarca-l cu `<script>` in `index.html`, iar `highlight.js` foloseste `window.hljs`; noteaza decizia in ledger.

- [ ] **Step 2: Modulul `highlight.js`**

`internal/web/static/js/highlight.js`:

```js
import hljs from "../vendor/highlight/highlight.min.js";

// Peste aceste limite nu coloram: un diff sau un fisier imens nu trebuie sa blocheze pagina.
const MAX_LINES = 5000;
const MAX_BYTES = 500 * 1024;

// extensie -> limbaj highlight.js (doar ce exista in build-ul "common")
const EXT = {
  php: "php", js: "javascript", mjs: "javascript", cjs: "javascript", jsx: "javascript",
  ts: "typescript", tsx: "typescript", go: "go", py: "python", rb: "ruby", java: "java",
  kt: "kotlin", swift: "swift", cs: "csharp", c: "c", h: "c", cpp: "cpp", hpp: "cpp", rs: "rust",
  sql: "sql", json: "json", yml: "yaml", yaml: "yaml", toml: "ini", ini: "ini", env: "ini",
  sh: "bash", bash: "bash", zsh: "bash", css: "css", scss: "scss", less: "less",
  html: "xml", htm: "xml", xml: "xml", svg: "xml", vue: "xml", svelte: "xml", twig: "xml",
  md: "markdown", diff: "diff", patch: "diff", lua: "lua", pl: "perl", r: "r",
};

// languageFor: limbajul dupa numele fisierului ("x.blade.php" -> php); null = necunoscut
export function languageFor(path) {
  const name = String(path || "").split(/[\\/]/).pop().toLowerCase();
  if (name === "dockerfile") return hljs.getLanguage("dockerfile") ? "dockerfile" : null;
  if (name === "makefile") return hljs.getLanguage("makefile") ? "makefile" : null;
  const ext = name.includes(".") ? name.split(".").pop() : "";
  const lang = EXT[ext] || null;
  return lang && hljs.getLanguage(lang) ? lang : null;
}

function tooBig(text) {
  if (text.length > MAX_BYTES) return true;
  let n = 0;
  for (let i = 0; i < text.length; i++) if (text.charCodeAt(i) === 10 && ++n > MAX_LINES) return true;
  return false;
}

// highlightInto: textul colorat in el (highlight.js scapa textul; nu intra HTML din continut).
// language null = ghicit dintre limbajele uzuale; text prea mare = text simplu.
export function highlightInto(el, text, language) {
  text = String(text ?? "");
  if (tooBig(text)) { el.textContent = text; return; }
  try {
    const res = language && hljs.getLanguage(language)
      ? hljs.highlight(text, { language, ignoreIllegals: true })
      : hljs.highlightAuto(text);
    el.innerHTML = res.value;
    el.classList.add("hljs");
  } catch {
    el.textContent = text;
  }
}

// highlightLine: o singura linie de cod (fara prefixul +/-), ca fragment; necunoscut = text simplu
export function highlightLine(text, language) {
  if (!language || !hljs.getLanguage(language) || text.length > 2000) return text;
  try {
    const t = document.createElement("template");
    t.innerHTML = hljs.highlight(text, { language, ignoreIllegals: true }).value;
    return t.content;
  } catch {
    return text;
  }
}
```

- [ ] **Step 3: Aplicarea**

`md.js`, in blocul ``` (`if (/^\s*```/.test(line))`): retine eticheta (`const tag = /^\s*```\s*([\w#+.-]*)/.exec(line)[1].toLowerCase();` inainte de `i++`), iar pentru blocul care nu e diff:

```js
      if (!diffy) {
        const pre = el("pre", { class: "md-pre" });
        const code = el("code", {});
        highlightInto(code, body.join("\n"), tag ? (languageFor("x." + tag) || tag) : null);
        pre.append(code);
        root.append(pre);
        continue;
      }
```

(`import { highlightInto, languageFor } from "./highlight.js";`; `languageFor("x.php")` transforma extensii ca `php`, `ts`; un tag ca `javascript` merge direct la `highlightInto`, care il verifica cu `hljs.getLanguage`.)

`changes.js`: `lineNode(text)` primeste limbajul:

```js
  function lineNode(text, lang) {
    const c = text[0];
    const cls = c === "+" ? "diff-add" : c === "-" ? "diff-del" : c === "\\" ? "diff-meta" : "diff-ctx";
    if (text === "") return el("div", { class: `diff-line ${cls}` }, " ");
    if (c === "\\") return el("div", { class: `diff-line ${cls}` }, text);
    // prefixul (+, -, spatiu) ramane separat; codul de dupa el se coloreaza
    return el("div", { class: `diff-line ${cls}` }, el("span", { class: "diff-sign" }, c), highlightLine(text.slice(1), lang));
  }
```

si `diffNode(f)` calculeaza `const lang = languageFor(f.path);` si cheama `lineNode(line, lang)`. `diffNode` se exporta pentru modala de fisier: `export function diffBlock(path, hunks)` care intoarce acelasi nod (`diffNode({ path, hunks })`), definita la nivel de modul (scoate `hunkHeader`, `lineNode`, `diffNode` din `changesView` la nivel de modul).

`task-files.js`: `el("pre", { class: "tfiles-text" }, text)` devine

```js
(() => { const pre = el("pre", { class: "tfiles-text" }); highlightInto(pre, text, languageFor(f.name)); return pre; })()
```

`app.css`: culorile token-urilor, din variabilele temei (adapteaza numele variabilelor la cele din `:root` al fisierului: cauta `--` in primele linii):

```css
/* evidentiere de sintaxa (highlight.js): culorile vin din tema, nu dintr-o tema highlight.js */
.hljs-keyword, .hljs-selector-tag, .hljs-built_in, .hljs-type { color: #c792ea; }
.hljs-string, .hljs-regexp, .hljs-addition, .hljs-attribute .hljs-string { color: #a5d6a7; }
.hljs-number, .hljs-literal, .hljs-symbol { color: #f9c784; }
.hljs-comment, .hljs-quote { color: #7d8799; font-style: italic; }
.hljs-title, .hljs-title.function_, .hljs-section { color: #82aaff; }
.hljs-variable, .hljs-template-variable, .hljs-params { color: #e0e6f0; }
.hljs-attr, .hljs-attribute, .hljs-property { color: #89ddff; }
.hljs-meta, .hljs-doctag { color: #ff9e80; }
.hljs-tag, .hljs-name { color: #f07178; }
.diff-sign { display: inline-block; width: 1.2em; opacity: .7; user-select: none; }
```

(Pastreaza contrastul cu fundalurile `.diff-add` / `.diff-del`; daca tema are deja variabile pentru accent/verde/albastru, foloseste-le in loc de culorile fixe.)

- [ ] **Step 4: Verificare**

```bash
for f in highlight md changes task-files; do cp internal/web/static/js/$f.js /tmp/$f.mjs && node --check /tmp/$f.mjs || echo FAIL $f; done
go build ./... && go test ./internal/web/
```

Expected: fara iesire de la node, testele trec (fisierele statice sunt incluse prin `embed`, nu se testeaza in Go).

- [ ] **Step 5: Commit**

```bash
git add internal/web/static
git commit -m "ui: evidentiere de sintaxa cu highlight.js (markdown, diff-uri, previzualizari)"
```

---

### Task 4: Tab-ul Modified si modala de fisier

**Files:**
- Create: `internal/web/static/js/file-view.js`, `internal/web/static/js/modified.js`
- Modify: `internal/web/static/js/task-modal.js`, `internal/web/static/js/changes.js`, `internal/web/static/js/md.js`, `internal/web/static/app.css`

**Interfaces:**
- Consumes: `highlightInto`, `languageFor` (Task 3); `diffBlock(path, hunks)` din `changes.js` (Task 3); endpoint-urile din Task 2.
- Produces:
  - `openFileView({ taskId, subTaskId, path }): void` (`file-view.js`) — deschide modala peste modala de task
  - `modifiedView({ onOpen }): { el, show(data), error(msg) }` (`modified.js`)
  - `changeStats(data, onOpen)` in `md.js`: valorile primesc `path` si `open()`.

- [ ] **Step 1: `file-view.js`**

```js
import { api } from "./api.js";
import { el, toast } from "./ui.js";
import { highlightInto, languageFor } from "./highlight.js";
import { diffBlock } from "./changes.js";
import { statBadge } from "./md.js";

// openFileView: modala cu fisierul de acum, liniile schimbate de task marcate pe margine,
// si comutare pe Diff. Se inchide cu Esc, cu x sau cu clic in afara.
export async function openFileView({ taskId, subTaskId, path }) {
  const q = new URLSearchParams({ path });
  if (subTaskId && subTaskId !== taskId) q.set("task", String(subTaskId));
  let v;
  try {
    v = await api(`/api/tasks/${taskId}/file?${q}`);
  } catch (err) {
    toast(err.message);
    return;
  }
  const lang = languageFor(path);
  let added = 0, removed = 0;
  for (const run of v.runs || []) for (const f of run.files || []) { added += f.added; removed += f.removed; }

  const fileBody = el("div", { class: "fv-file" });
  const diffBody = el("div", { class: "fv-diff", hidden: true });
  let mode = "file";
  const toggle = el("button", { type: "button", class: "btn small" }, "Diff");
  toggle.addEventListener("click", () => {
    mode = mode === "file" ? "diff" : "file";
    fileBody.hidden = mode !== "file";
    diffBody.hidden = mode !== "diff";
    toggle.textContent = mode === "file" ? "Diff" : "File";
  });
  const copy = el("button", { type: "button", class: "btn small", onclick: async () => {
    try { await navigator.clipboard.writeText(path); toast("Path copied"); } catch { toast(path); }
  } }, "Copy path");

  // vederea Diff: diff-urile task-ului pe fisier, pe run-uri
  for (const run of v.runs || []) {
    diffBody.append(el("h4", { class: "change-run" }, `${run.agent || "?"} · ${new Date(run.at).toLocaleString()}`));
    for (const f of run.files || []) diffBody.append(diffBlock(f.path, f.hunks));
  }

  // vederea File
  const note = (t) => el("p", { class: "hint" }, t);
  if (!v.exists) {
    fileBody.append(note("The file no longer exists."));
    toggle.click();
  } else if (v.binary) {
    fileBody.append(note("Binary file: no preview. See Diff."));
  } else if (v.tooLarge) {
    fileBody.append(note("The file is larger than 1 MB: no preview. See Diff."));
  } else {
    fileBody.append(note(v.exact
      ? "Markers show this task's changes; the file may have changed since."
      : "This task's changes can't be placed on lines (a change was too large or had no line numbers); see Diff."));
    fileBody.append(codeWithMarkers(v.content, lang, v.exact ? v.markers : null));
  }

  const cut = Math.max(path.lastIndexOf("/"), path.lastIndexOf("\\")) + 1;
  const close = () => { overlay.remove(); document.removeEventListener("keydown", onKey, true); };
  const onKey = (e) => { if (e.key === "Escape") { e.stopPropagation(); close(); } };
  const box = el("div", { class: "modal fv", role: "dialog", "aria-modal": "true", "aria-label": path },
    el("div", { class: "fv-head" },
      el("span", { class: "change-path" }, cut ? el("span", { class: "change-dir" }, path.slice(0, cut)) : null,
        el("span", { class: "change-name" }, path.slice(cut))),
      statBadge({ added, removed }),
      el("span", { class: "fv-actions" }, toggle, copy,
        el("button", { type: "button", class: "modal-close", "aria-label": "Close", onclick: close }, "×"))),
    fileBody, diffBody);
  const overlay = el("div", { class: "modal-overlay fv-overlay", onclick: (e) => { if (e.target === overlay) close(); } }, box);
  document.addEventListener("keydown", onKey, true); // capture: Esc inchide doar modala de fisier, nu si pe cea de task
  document.body.append(overlay);
  fileBody.querySelector(".fv-mark")?.scrollIntoView({ block: "center" });
}

// codeWithMarkers: codul colorat cu numere de linie si marcaje pe margine. Coloram tot textul o data
// (contextul pe mai multe linii ramane corect), apoi taiem HTML-ul pe linii.
function codeWithMarkers(text, lang, markers) {
  const tmp = el("code", {});
  highlightInto(tmp, text, lang);
  const lines = splitHighlighted(tmp);
  const kind = new Map();
  for (const n of markers?.added || []) kind.set(n, "added");
  for (const n of markers?.modified || []) kind.set(n, "modified");
  const deleted = new Set(markers?.deleted || []);
  const rows = el("div", { class: "fv-code" });
  const frag = document.createDocumentFragment();
  lines.forEach((html, i) => {
    const n = i + 1;
    const k = kind.get(n);
    const row = el("div", { class: "fv-line" + (k ? ` fv-mark fv-${k}` : "") + (deleted.has(n) ? " fv-del-before" : "") },
      el("span", { class: "fv-num" }, String(n)),
      el("span", { class: "fv-gutter" }));
    const code = el("span", { class: "fv-text hljs" });
    code.innerHTML = html || " "; // html vine din highlight.js (deja scapat), sau linia goala
    row.append(code);
    frag.append(row);
  });
  if (deleted.has(lines.length + 1)) frag.append(el("div", { class: "fv-line fv-mark fv-del-before fv-end" }, el("span", { class: "fv-num" }, ""), el("span", { class: "fv-gutter" })));
  rows.append(frag);
  return rows;
}

// splitHighlighted: imparte HTML-ul colorat pe linii, inchizand si redeschizand <span>-urile
// deschise la capatul fiecarei linii (un comentariu pe mai multe linii ramane colorat).
function splitHighlighted(code) {
  const out = [];
  let cur = "";
  const open = [];
  const walk = (node) => {
    for (const ch of node.childNodes) {
      if (ch.nodeType === Node.TEXT_NODE) {
        const parts = ch.textContent.split("\n");
        parts.forEach((p, i) => {
          if (i > 0) {
            out.push(cur + "</span>".repeat(open.length));
            cur = open.map((c) => `<span class="${c}">`).join("");
          }
          cur += p.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;");
        });
      } else if (ch.nodeType === Node.ELEMENT_NODE) {
        const cls = ch.getAttribute("class") || "";
        open.push(cls);
        cur += `<span class="${cls}">`;
        walk(ch);
        open.pop();
        cur += "</span>";
      }
    }
  };
  walk(code);
  out.push(cur);
  if (out.length > 1 && out[out.length - 1] === "") out.pop(); // ultimul "\n" din fisier
  return out;
}
```

(Verifica numele clasei overlay-ului folosit de modala de task in `task-modal.js` — `grep -n "overlay" internal/web/static/js/task-modal.js` — si foloseste aceleasi clase CSS de baza, ca stilul sa fie la fel; `fv-overlay` doar adauga `z-index` mai mare.)

- [ ] **Step 2: `modified.js` si tab-ul**

`internal/web/static/js/modified.js`:

```js
import { el, clock } from "./ui.js";
import { statBadge } from "./md.js";

// modifiedView: tab-ul "Modified" al unui task: un rand pe fisier (si pe sub-task); clic = onOpen(file).
export function modifiedView({ onOpen }) {
  const root = el("div", { class: "modified" }, el("p", { class: "hint" }, "Loading..."));
  function row(f, ownTaskId) {
    const cut = Math.max(f.path.lastIndexOf("/"), f.path.lastIndexOf("\\")) + 1;
    return el("button", { type: "button", class: "change-file modified-row", onclick: () => onOpen(f) },
      el("span", { class: "change-path" },
        cut ? el("span", { class: "change-dir" }, f.path.slice(0, cut)) : null,
        el("span", { class: "change-name" }, f.path.slice(cut))),
      f.taskId !== ownTaskId ? el("span", { class: "modified-ref" }, f.ref) : null,
      statBadge({ added: f.added, removed: f.removed, kind: f.created ? "create" : "edit" }),
      el("span", { class: "modified-meta" }, `${f.agent || "?"} · ${clock(f.at)}`));
  }
  return {
    el: root,
    show(data, ownTaskId) {
      const files = data.files || [];
      if (!files.length) {
        root.replaceChildren(el("p", { class: "hint" }, "No file changes recorded for this task."),
          el("p", { class: "hint" }, "Changes made through shell commands are not captured as diffs."));
        return;
      }
      root.replaceChildren(...files.map((f) => row(f, ownTaskId)));
    },
    error(msg) { root.replaceChildren(el("p", { class: "hint" }, msg)); },
  };
}
```

`task-modal.js`:
- import `modifiedView` si `openFileView`;
- ca la Changes: `const modified = modifiedView({ onOpen: (f) => openFileView({ taskId, subTaskId: f.taskId, path: f.path }) });`, `const modPane = el("div", { id: \`mod-${taskId}\`, role: "tabpanel", hidden: true }, modified.el);`, `const tabMod = el("button", { type: "button", class: "tab", role: "tab", "aria-controls": modPane.id, onclick: () => setTab("modified") }, "Modified");`
- tab-ul intra in tablist dupa `tabChg` si pane-ul dupa `chgPane`; `applyTab` primeste `"modified"` in `names`, `tabMod` in `tabs`, `modPane` in `panes`, si cheama `syncModified()`;
- `syncModified` (ca `syncChanges`): cand tab-ul e deschis si `data.changesCount` difera de cel incarcat (`modCount`), cere `/api/tasks/${taskId}/modified` cu un contor `modSeq` (ignora raspunsurile vechi), apoi `modified.show(res, taskId)`; la eroare `modified.error(err.message)` si `modCount = null`;
- eticheta tab-ului: `tabMod.textContent = n ? \`Modified (${n})\` : "Modified"` dupa raspuns (n = numarul de fisiere);
- `changeStats(res)` din `syncChanges` devine `changeStats(res, (path) => openFileView({ taskId, path }))`.
- `initialTab` / tipul din comentariu (`"conversation" | ... | "flow"`) primeste si `"modified"`.

`changes.js`, in `fileNode`, langa buton (in afara lui, ca un clic pe nume sa deschida tot diff-ul):

```js
    const view = el("button", { type: "button", class: "btn small change-view", onclick: () => openFile?.(f.path) }, "View file");
    return el("div", { class: "change" }, el("div", { class: "change-row" }, btn, view), body);
```

cu `changesView({ openFile } = {})` ca parametru, iar in `task-modal.js` `changesView({ openFile: (path) => openFileView({ taskId, path }) })`.

`md.js`:
- `changeStats(data, onOpen)`: fiecare valoare primeste `path: f.path` si, daca `onOpen` e dat, `open: () => onOpen(f.path)`;
- in `inline`, eticheta de fisier devine clicabila cand `s.open` exista:

```js
      out.push(s ? el("span", { class: "md-file" + (s.open ? " md-file-link" : ""), role: s.open ? "button" : null, tabindex: s.open ? "0" : null,
        title: s.open ? "View file" : null, onclick: s.open ? () => s.open() : null,
        onkeydown: s.open ? (e) => { if (e.key === "Enter") s.open(); } : null }, el("code", {}, m[1]), statBadge(s)) : el("code", {}, m[1]));
```

(Verifica in `ui.js` ca `el()` ignora atributele `null`; daca nu, construieste obiectul de atribute conditionat.)

- [ ] **Step 3: CSS**

In `app.css`, langa stilurile `.changes` / `.change-file`:

```css
/* modala de fisier: cod cu numere de linie si marcaje pe margine */
.fv-overlay { z-index: 60; }
.modal.fv { width: min(1100px, 96vw); max-height: 90vh; display: flex; flex-direction: column; }
.fv-head { display: flex; align-items: center; gap: 10px; padding: 12px 16px; border-bottom: 1px solid var(--line, #2a3240); }
.fv-actions { margin-left: auto; display: flex; gap: 6px; align-items: center; }
.fv-file, .fv-diff { overflow: auto; padding: 8px 0; }
.fv-code { font-family: ui-monospace, SFMono-Regular, Menlo, monospace; font-size: 12.5px; line-height: 1.55; }
.fv-line { display: flex; position: relative; }
.fv-num { width: 4em; text-align: right; padding-right: 10px; color: #6b7587; user-select: none; flex: none; }
.fv-gutter { width: 4px; flex: none; margin-right: 10px; }
.fv-added .fv-gutter { background: #4caf7a; }
.fv-modified .fv-gutter { background: #5b8def; }
.fv-added { background: rgba(76, 175, 122, .08); }
.fv-modified { background: rgba(91, 141, 239, .08); }
.fv-del-before::before { content: ""; position: absolute; left: calc(4em + 2px); top: -4px; border: 4px solid transparent; border-left-color: #e5534b; }
.fv-text { white-space: pre; padding-right: 16px; }
.change-row { display: flex; align-items: center; gap: 6px; }
.change-row .change-file { flex: 1; }
.modified-row { display: flex; gap: 10px; align-items: center; width: 100%; }
.modified-ref, .modified-meta { color: #8a94a6; font-size: 12px; }
.modified-meta { margin-left: auto; }
.md-file-link { cursor: pointer; text-decoration: underline dotted; }
```

(Inlocuieste culorile fixe cu variabilele temei acolo unde exista: cauta in `:root` din `app.css`.)

- [ ] **Step 4: Verificare sintaxa**

```bash
for f in file-view modified task-modal changes md; do cp internal/web/static/js/$f.js /tmp/$f.mjs && node --check /tmp/$f.mjs || echo FAIL $f; done
go build ./...
```

Expected: fara iesire de la node.

- [ ] **Step 5: Verificare cu Chrome headless (memoria: verifica UI-ul cu screenshot-uri)**

Pe o copie de test (HOME temporar, port 7791, proiect pe pauza), cu un task care are in `task_changes` un `create` si un `edit` pe un fisier `.php` real din folderul proiectului, un sub-task cu un `.ts`, si un raport cu ` ```php ` si un `app/x.php` in backticks:
- tab-ul Modified: randurile, eticheta T-N la sub-task, `Modified (N)`;
- clic pe rand: modala cu numere de linie, marcaje verzi/albastre, cod colorat, nota; Diff comuta; Esc inchide doar modala de fisier;
- Changes: diff-ul colorat, butonul View file;
- Conversation: blocul ```php colorat, numele de fisier clicabil;
- un bloc ```html cu `<script>alert(1)</script>` in raport: apare ca text colorat, nu se executa.
Masoara cu `getBoundingClientRect` ca modala incape la 1280x720 si 1000x700 si fa screenshot-uri.

- [ ] **Step 6: Commit**

```bash
git add internal/web/static
git commit -m "ui: tab-ul Modified si modala de fisier cu marcaje pe margine"
```

---

### Task 5: Verificare finala si documentatie

- [ ] **Step 1: Verificarile din conventii**

```bash
go vet ./... && go build -race ./... && go test ./... \
 && GOOS=darwin GOARCH=arm64 go build -o /dev/null ./cmd/foreman && GOOS=windows go build -o /dev/null ./cmd/foreman
```

- [ ] **Step 2: Documentatie**

`CLAUDE.md`, dupa intrarea "chat doar de citire":

```
Gata (cod colorat si fisierele modificate, spec in docs/superpowers/specs/2026-10-09-foreman-code-view-design.md):
highlight.js 11.9.0 (build common, static/vendor/highlight, BSD-3) prin static/js/highlight.js (languageFor,
highlightInto, highlightLine; fara colorare peste 5000 linii / 500 KB) in markdown, diff-uri si previzualizari. Tab-ul
Modified pe task (GET /api/tasks/{id}/modified, cu sub-task-urile) si modala de fisier (file-view.js, GET
/api/tasks/{id}/file?path=&task=): fisierul de acum de pe disc, doar cai din task_changes, prin os.OpenRoot, max 1 MB,
binar = NUL in primii 8 KB; marcajele added/modified/deleted din store.FileMarkers (fragmentele aplicate in ordine;
exact=false fara numere de linie / trunchiat / Write fara fragmente). Deschisa din Modified, "View file" din Changes si
numele de fisiere +/- din rapoarte.
```

Spec: `Status: implementat (2026-10-09).`

- [ ] **Step 3: Commit**

```bash
git add CLAUDE.md docs/superpowers/specs/2026-10-09-foreman-code-view-design.md
git commit -m "docs: cod colorat, tab-ul Modified si modala de fisier"
```
