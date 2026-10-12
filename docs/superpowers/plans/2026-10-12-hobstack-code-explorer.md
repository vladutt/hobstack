# Code Explorer (Cmd/Ctrl+P) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Un explorer de cod doar de citire, deschis cu Cmd/Ctrl+P pe orice pagina a unui proiect: arbore + cautare + cod colorat, copiere cale / referinta de linie si "Ask <agent>" care pune `@cale:42-50 ` in chat.

**Architecture:** Doua endpointuri noi in `internal/web/api_code_tree.go` (lista fisierelor pe foldere, din `codeCache`, si citirea unui fisier doar daca e in lista, prin `os.OpenRoot`). Un modul JS nou `static/js/code-explorer.js` cu modala; `workspace.js` adauga scurtatura, butonul Code si `prefillChat`. Codul colorat refoloseste `codeWithMarkers` din `file-view.js`.

**Tech Stack:** Go 1.24+ (net/http, os.OpenRoot), JS vanilla ES modules, highlight.js deja inclus.

**Spec:** `docs/superpowers/specs/2026-10-12-hobstack-code-explorer-design.md`

## Global Constraints

- Tot ce vede utilizatorul (UI, mesaje, erori) e in engleza; comentariile din cod raman in romana.
- Doar citire: niciun endpoint nou nu scrie pe disc.
- Lista fisierelor = exact lista de la @ (`s.codeCache.Files(folder.Path)`), max `codefiles.MaxFiles` (20000) pe folder.
- Un fisier se citeste doar daca e in lista folderului; citirea prin `readIn` (os.OpenRoot, max `maxFileView` = 1 MB).
- Binar = NUL in primii 8 KB.
- `go vet ./...`, `go build -race ./...`, `GOOS=darwin GOARCH=arm64 go build ./cmd/hobstack`, `GOOS=windows go build ./cmd/hobstack` trebuie sa treaca.
- JS-ul se verifica ca modul: `cp f.js "$TMPDIR/chk.mjs" && node --check "$TMPDIR/chk.mjs"`.
- localStorage doar in try/catch; cheia `hobstack-code-<pid>`.
- Nu reporni HobStack daca `pgrep -f "claude -p"` gaseste agenti care ruleaza.
- Commit doar daca utilizatorul cere (repo-ul e pe `main`; nu se face commit automat in aceasta sesiune).

## Review Focus

1. Un proiect cu mai multe foldere: referinta copiata are prefixul folderului (`web/src/App.vue:12`), iar Ask scrie calea absoluta cand fisierul nu e in folderul agentului — testat in Task 5 (verificarea headless pe un proiect cu 2 foldere, Step 6).
2. Un fisier sters sau redenumit dupa ce lista a intrat in cache (30 s): clic pe el arata "The file no longer exists." si arborele se reincarca, fara eroare generica — testat in Task 3 (404 pe fisier sters) si Task 5 Step 6.
3. Cmd+P cand modala de task sau modala de fisier e deschisa: explorer-ul apare deasupra, Esc il inchide doar pe el (modala de task ramane) — Task 5 Step 6.
4. Cmd+C cu text selectat cu mouse-ul in cod: copierea normala nu e inlocuita de referinta — Task 4 (conditia `getSelection().toString()`), verificat in Task 5 Step 6.
5. Folder cu 20000+ fisiere: arborele nu deseneaza tot (doar folderele desfasurate) si arata nota `truncated`; cautarea ramane rapida — Task 2 (`truncated` pus din `MaxFiles`, testat cu `MaxFiles` micsorat) si Task 4 (desenare lenesa).

---

## File Structure

- Modify `internal/codefiles/codefiles.go` — `(*Cache).Invalidate(dir)`.
- Modify `internal/codefiles/codefiles_test.go` — test pentru Invalidate.
- Create `internal/web/api_code_tree.go` — `getProjectTree`, `getCodeFile`, `isBinary`, `treeFolderName`.
- Modify `internal/web/api_code.go` — `getTaskFile` foloseste `isBinary`.
- Modify `internal/web/server.go` — doua rute.
- Create `internal/web/api_code_tree_test.go` — testele endpointurilor.
- Modify `internal/web/static/js/file-view.js` — exporta `codeWithMarkers` si `copyText`.
- Create `internal/web/static/js/code-explorer.js` — modala explorer-ului.
- Modify `internal/web/static/js/workspace.js` — scurtatura, butonul Code, `prefillChat`.
- Modify `internal/web/static/app.css` — stilurile `.cx-*`.
- Modify `CLAUDE.md` — linia "Gata (explorer de cod...)".

---

### Task 1: `Cache.Invalidate`

**Files:**
- Modify: `internal/codefiles/codefiles.go` (dupa `func (c *Cache) Files`)
- Test: `internal/codefiles/codefiles_test.go`

**Interfaces:**
- Produces: `func (c *Cache) Invalidate(dir string)` — sterge intrarea folderului; urmatorul `Files(dir)` reciteste discul.

- [ ] **Step 1: Write the failing test** (la finalul `codefiles_test.go`; `touch` exista deja in fisier)

```go
// Invalidate: un fisier creat dupa ce lista a intrat in cache apare abia dupa invalidare
func TestCacheInvalidate(t *testing.T) {
	dir := t.TempDir()
	touch(t, dir, "a.go")
	c := NewCache(time.Hour)
	if got, _ := c.Files(dir); len(got) != 1 {
		t.Fatalf("got %v", got)
	}
	touch(t, dir, "b.go")
	if got, _ := c.Files(dir); len(got) != 1 {
		t.Fatalf("cache-ul trebuia sa raspunda tot cu lista veche: %v", got)
	}
	c.Invalidate(dir)
	if got, _ := c.Files(dir); len(got) != 2 {
		t.Fatalf("dupa Invalidate: %v", got)
	}
}
```

Adauga `"time"` la importurile testului daca lipseste.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/codefiles/ -run TestCacheInvalidate -count=1`
Expected: FAIL la compilare: `c.Invalidate undefined`.

- [ ] **Step 3: Write minimal implementation** (dupa `Files`)

```go
// Invalidate scoate folderul din cache: urmatorul Files il reciteste de pe disc.
func (c *Cache) Invalidate(dir string) {
	c.mu.Lock()
	delete(c.m, dir)
	c.mu.Unlock()
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/codefiles/ -count=1`
Expected: `ok  hobstack/internal/codefiles`

---

### Task 2: `GET /api/projects/{id}/tree`

**Files:**
- Create: `internal/web/api_code_tree.go`
- Modify: `internal/web/server.go` (langa `mux.HandleFunc("GET /api/projects/{id}/code-files", s.codeFiles)`)
- Test: `internal/web/api_code_tree_test.go`

**Interfaces:**
- Consumes: `s.codeCache.Files(dir) ([]string, error)`, `codefiles.MaxFiles`, `store.FolderName(path)`.
- Produces: JSON `{"folders":[{"id":int,"name":string,"path":string,"files":[string],"truncated":bool,"error":string?}]}`; tipul Go `treeFolder`; `treeFolderName(f store.Folder) string` (numele afisat: `f.Name`, altfel `store.FolderName(f.Path)`).

- [ ] **Step 1: Write the failing tests** (`internal/web/api_code_tree_test.go`)

```go
package web

import (
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"hobstack/internal/codefiles"
	"hobstack/internal/store"
)

type treeRes struct {
	Folders []treeFolder `json:"folders"`
}

// writeFiles: creeaza fisierele (cu continut "x") sub dir
func writeFiles(t *testing.T, dir string, names ...string) {
	t.Helper()
	for _, n := range names {
		p := filepath.Join(dir, filepath.FromSlash(n))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// twoFolders: proiect cu folderele api (primul) si web
func twoFolders(t *testing.T, e *env) (store.Project, string, string) {
	t.Helper()
	api, web := t.TempDir(), t.TempDir()
	code, raw := e.do(t, "POST", "/api/projects", map[string]any{"name": "Shop", "goal": "Sell",
		"folders": []map[string]any{{"name": "api", "path": api}, {"name": "web", "path": web}}})
	if code != http.StatusCreated {
		t.Fatalf("%d %s", code, raw)
	}
	var p store.Project
	json.Unmarshal(raw, &p)
	return p, api, web
}

func getTree(t *testing.T, e *env, pid int64) treeRes {
	t.Helper()
	code, raw := e.do(t, "GET", "/api/projects/"+itoa(pid)+"/tree", nil)
	if code != 200 {
		t.Fatalf("%d %s", code, raw)
	}
	var res treeRes
	json.Unmarshal(raw, &res)
	return res
}

func TestProjectTree(t *testing.T) {
	e := setup(t)
	p, api, web := twoFolders(t, e)
	writeFiles(t, api, "app/User.php", "node_modules/x/index.js")
	writeFiles(t, web, "src/App.vue")
	res := getTree(t, e, p.ID)
	if len(res.Folders) != 2 || res.Folders[0].Name != "api" || res.Folders[1].Name != "web" {
		t.Fatalf("%+v", res)
	}
	if got := res.Folders[0].Files; len(got) != 1 || got[0] != "app/User.php" {
		t.Fatalf("node_modules trebuia sarit: %v", got)
	}
	if res.Folders[0].Truncated || res.Folders[0].Error != "" || res.Folders[0].Path != api {
		t.Fatalf("%+v", res.Folders[0])
	}
	if code, _ := e.do(t, "GET", "/api/projects/999999/tree", nil); code != http.StatusNotFound {
		t.Fatalf("proiect lipsa: %d", code)
	}
}

// repo git: fisierele din .gitignore nu apar
func TestProjectTreeGitignore(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git lipseste")
	}
	e := setup(t)
	dir := t.TempDir()
	writeFiles(t, dir, "main.go", ".env")
	os.WriteFile(filepath.Join(dir, ".gitignore"), []byte(".env\n"), 0o644)
	if out, err := exec.Command("git", "-C", dir, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	p := e.createProject(t, dir)
	files := getTree(t, e, p.ID).Folders[0].Files
	for _, f := range files {
		if f == ".env" {
			t.Fatalf(".env e ignorat de git: %v", files)
		}
	}
}

// un folder sters de pe disc: eroare doar pe el, raspunsul ramane 200
func TestProjectTreeMissingFolder(t *testing.T) {
	e := setup(t)
	p, api, web := twoFolders(t, e)
	writeFiles(t, api, "a.php")
	os.RemoveAll(web)
	res := getTree(t, e, p.ID)
	if res.Folders[0].Error != "" || len(res.Folders[0].Files) != 1 {
		t.Fatalf("%+v", res.Folders[0])
	}
	if res.Folders[1].Error != "Folder not found: "+web || res.Folders[1].Files == nil || len(res.Folders[1].Files) != 0 {
		t.Fatalf("%+v", res.Folders[1])
	}
}

// truncated: lista a atins MaxFiles
func TestProjectTreeTruncated(t *testing.T) {
	old := codefiles.MaxFiles
	codefiles.MaxFiles = 2
	t.Cleanup(func() { codefiles.MaxFiles = old })
	e := setup(t)
	dir := t.TempDir()
	writeFiles(t, dir, "a.go", "b.go", "c.go")
	p := e.createProject(t, dir)
	f := getTree(t, e, p.ID).Folders[0]
	if !f.Truncated || len(f.Files) != 2 {
		t.Fatalf("%+v", f)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/web/ -run 'TestProjectTree' -count=1`
Expected: FAIL la compilare: `undefined: treeFolder`.

- [ ] **Step 3: Write minimal implementation**

`internal/web/api_code_tree.go`:

```go
package web

import (
	"errors"
	"io/fs"
	"log"
	"net/http"

	"hobstack/internal/codefiles"
	"hobstack/internal/store"
)

// treeFolder: un folder al proiectului in explorer-ul de cod (Cmd/Ctrl+P)
type treeFolder struct {
	ID        int64    `json:"id"`
	Name      string   `json:"name"`
	Path      string   `json:"path"`
	Files     []string `json:"files"`     // relative, cu "/", ca la @ in chat
	Truncated bool     `json:"truncated"` // s-a atins codefiles.MaxFiles
	Error     string   `json:"error,omitempty"`
}

// treeFolderName: numele afisat al folderului (ca in task_changes cand proiectul are mai multe foldere)
func treeFolderName(f store.Folder) string {
	if f.Name != "" {
		return f.Name
	}
	return store.FolderName(f.Path)
}

// getProjectTree: fisierele fiecarui folder al proiectului, aceeasi lista ca la @ in chat.
// Un folder care nu se poate citi vine cu Error; celelalte merg normal.
func (s *Server) getProjectTree(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		fail(w, err)
		return
	}
	p, err := s.st.GetProject(id)
	if err != nil {
		fail(w, err)
		return
	}
	out := []treeFolder{}
	for _, f := range p.Folders {
		tf := treeFolder{ID: f.ID, Name: treeFolderName(f), Path: f.Path, Files: []string{}}
		files, err := s.codeCache.Files(f.Path)
		switch {
		case errors.Is(err, fs.ErrNotExist):
			tf.Error = "Folder not found: " + f.Path
		case err != nil:
			log.Printf("code tree %s: %v", f.Path, err)
			tf.Error = "Can't read the folder: " + f.Path
		default:
			tf.Files = files
			tf.Truncated = len(files) >= codefiles.MaxFiles
		}
		out = append(out, tf)
	}
	writeJSON(w, http.StatusOK, map[string]any{"folders": out})
}
```

In `internal/web/server.go`, dupa linia cu `code-files`:

```go
	mux.HandleFunc("GET /api/projects/{id}/tree", s.getProjectTree)
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/web/ -run 'TestProjectTree' -count=1 -v 2>&1 | tail -12`
Expected: toate `--- PASS` (`TestProjectTreeGitignore` poate fi SKIP fara git).

---

### Task 3: `GET /api/projects/{id}/code-file`

**Files:**
- Modify: `internal/web/api_code_tree.go`
- Modify: `internal/web/api_code.go` (`getTaskFile`, blocul cu `head := content` / `strings.IndexByte(head, 0)`)
- Modify: `internal/web/server.go`
- Test: `internal/web/api_code_tree_test.go`

**Interfaces:**
- Consumes: `readIn(root, rel string) (string, error)` cu `errOutside`, `errTooLarge`, `fs.ErrNotExist` (din `api_code.go`); `s.codeCache.Files`, `s.codeCache.Invalidate` (Task 1); `badRequest` (string → 400 prin `fail`).
- Produces: JSON `{"folder":int,"path":string,"content":string?,"binary":bool,"tooLarge":bool}`; `func isBinary(content string) bool`.

- [ ] **Step 1: Write the failing tests** (adauga in `api_code_tree_test.go`)

```go
type codeFileRes struct {
	Folder   int64  `json:"folder"`
	Path     string `json:"path"`
	Content  string `json:"content"`
	Binary   bool   `json:"binary"`
	TooLarge bool   `json:"tooLarge"`
}

func getCode(t *testing.T, e *env, pid, folder int64, path string) (int, codeFileRes) {
	t.Helper()
	q := url.Values{"folder": {itoa(folder)}, "path": {path}}
	code, raw := e.do(t, "GET", "/api/projects/"+itoa(pid)+"/code-file?"+q.Encode(), nil)
	var res codeFileRes
	json.Unmarshal(raw, &res)
	return code, res
}

func TestCodeFile(t *testing.T) {
	e := setup(t)
	p, api, web := twoFolders(t, e)
	writeFiles(t, api, "app/User.php", "node_modules/x/index.js")
	os.WriteFile(filepath.Join(api, "app", "User.php"), []byte("<?php\necho 1;\n"), 0o644)
	os.WriteFile(filepath.Join(api, "logo.png"), append([]byte("PNG"), 0, 1, 2), 0o644)
	os.WriteFile(filepath.Join(api, "big.txt"), make([]byte, maxFileView+1), 0o644)
	writeFiles(t, web, "src/App.vue")
	apiID, webID := p.Folders[0].ID, p.Folders[1].ID

	code, res := getCode(t, e, p.ID, apiID, "app/User.php")
	if code != 200 || res.Content != "<?php\necho 1;\n" || res.Binary || res.TooLarge || res.Path != "app/User.php" || res.Folder != apiID {
		t.Fatalf("%d %+v", code, res)
	}
	// al doilea folder
	if code, res := getCode(t, e, p.ID, webID, "src/App.vue"); code != 200 || res.Content != "x" {
		t.Fatalf("web: %d %+v", code, res)
	}
	// binar: fara continut
	if code, res := getCode(t, e, p.ID, apiID, "logo.png"); code != 200 || !res.Binary || res.Content != "" {
		t.Fatalf("binar: %d %+v", code, res)
	}
	// > 1 MB: fara continut
	if code, res := getCode(t, e, p.ID, apiID, "big.txt"); code != 200 || !res.TooLarge || res.Content != "" {
		t.Fatalf("mare: %d %+v", code, res)
	}
	// in folder, dar nu in lista (node_modules): 404
	if code, _ := getCode(t, e, p.ID, apiID, "node_modules/x/index.js"); code != http.StatusNotFound {
		t.Fatalf("node_modules: %d", code)
	}
	// cai rele: 400
	for _, bad := range []string{"", "../x", "/etc/passwd", "app/../../x"} {
		if code, _ := getCode(t, e, p.ID, apiID, bad); code != http.StatusBadRequest {
			t.Fatalf("%q: %d", bad, code)
		}
	}
	// folderul altui proiect: 404
	other := e.createProject(t, t.TempDir())
	if code, _ := getCode(t, e, other.ID, apiID, "app/User.php"); code != http.StatusNotFound {
		t.Fatalf("alt proiect: %d", code)
	}
	// fisier creat dupa ce lista a intrat in cache: se citeste (invalidare)
	writeFiles(t, api, "app/New.php")
	if code, _ := getCode(t, e, p.ID, apiID, "app/New.php"); code != 200 {
		t.Fatalf("fisier nou: %d", code)
	}
	// fisier sters dupa ce lista a intrat in cache: 404
	os.Remove(filepath.Join(api, "app", "New.php"))
	if code, _ := getCode(t, e, p.ID, apiID, "app/New.php"); code != http.StatusNotFound {
		t.Fatalf("fisier sters: %d", code)
	}
}

// symlink din folder catre un fisier din afara: listat de git (fisier nou neignorat), refuzat la citire
func TestCodeFileSymlinkOutside(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git lipseste")
	}
	e := setup(t)
	dir, outside := t.TempDir(), t.TempDir()
	writeFiles(t, outside, "secret.txt")
	if err := os.Symlink(filepath.Join(outside, "secret.txt"), filepath.Join(dir, "link.txt")); err != nil {
		t.Skip("fara symlink: ", err)
	}
	if out, err := exec.Command("git", "-C", dir, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	p := e.createProject(t, dir)
	if code, res := getCode(t, e, p.ID, p.Folders[0].ID, "link.txt"); code != http.StatusNotFound {
		t.Fatalf("symlink in afara: %d %+v", code, res)
	}
}
```

Adauga `"net/url"` la importurile fisierului de test.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/web/ -run 'TestCodeFile' -count=1`
Expected: FAIL: `TestCodeFile` primeste 404 (ruta nu exista) la prima citire: `404 {...}`.

- [ ] **Step 3: Write minimal implementation**

In `api_code_tree.go`, adauga importurile `"path/filepath"`, `"slices"`, `"strconv"`, `"strings"` si:

```go
// codeFileView: un fisier deschis in explorer-ul de cod
type codeFileView struct {
	Folder   int64  `json:"folder"`
	Path     string `json:"path"`
	Content  string `json:"content,omitempty"`
	Binary   bool   `json:"binary"`
	TooLarge bool   `json:"tooLarge"`
}

// isBinary: NUL in primii 8 KB (ca in modala de fisier a task-ului)
func isBinary(content string) bool {
	head := content
	if len(head) > 8192 {
		head = head[:8192]
	}
	return strings.IndexByte(head, 0) >= 0
}

// getCodeFile: un fisier din explorer-ul de cod (?folder=<id>&path=<rel>). Se citeste doar daca e in
// lista folderului (aceeasi ca la @): un fisier ignorat de git sau din node_modules nu se poate cere
// scriind calea. Citirea prin os.OpenRoot (readIn), deci ".." si symlink-urile in afara sunt refuzate.
func (s *Server) getCodeFile(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		fail(w, err)
		return
	}
	p, err := s.st.GetProject(id)
	if err != nil {
		fail(w, err)
		return
	}
	fid, _ := strconv.ParseInt(r.URL.Query().Get("folder"), 10, 64)
	i := slices.IndexFunc(p.Folders, func(f store.Folder) bool { return f.ID == fid })
	if i < 0 {
		fail(w, store.ErrNotFound)
		return
	}
	folder := p.Folders[i]
	rel := r.URL.Query().Get("path")
	if rel == "" || strings.Contains(rel, "\\") || !filepath.IsLocal(filepath.FromSlash(rel)) {
		fail(w, badRequest("bad path"))
		return
	}
	if !s.inCodeList(folder.Path, rel) {
		fail(w, store.ErrNotFound)
		return
	}
	v := codeFileView{Folder: folder.ID, Path: rel}
	content, err := readIn(folder.Path, filepath.FromSlash(rel))
	switch {
	case errors.Is(err, errOutside), errors.Is(err, fs.ErrNotExist):
		fail(w, store.ErrNotFound)
		return
	case errors.Is(err, errTooLarge):
		v.TooLarge = true
	case err != nil:
		fail(w, err)
		return
	case isBinary(content):
		v.Binary = true
	default:
		v.Content = content
	}
	writeJSON(w, http.StatusOK, v)
}

// inCodeList: rel e in lista folderului; daca nu, lista se reciteste o data (un fisier tocmai creat)
func (s *Server) inCodeList(dir, rel string) bool {
	files, err := s.codeCache.Files(dir)
	if err == nil && slices.Contains(files, rel) {
		return true
	}
	s.codeCache.Invalidate(dir)
	files, err = s.codeCache.Files(dir)
	return err == nil && slices.Contains(files, rel)
}
```

Nota: `"app/../../x"` nu e local → 400; `"/etc/passwd"` nu e local → 400. `strings.Contains(rel, "\\")` refuza separatorii Windows (caile din lista au doar `/`).

In `internal/web/server.go`:

```go
	mux.HandleFunc("GET /api/projects/{id}/code-file", s.getCodeFile)
```

In `internal/web/api_code.go`, in `getTaskFile`, inlocuieste:

```go
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
```

cu:

```go
		v.Exists = true
		if isBinary(content) {
			v.Binary = true
		} else {
			v.Content = content
		}
```

(daca `strings` ramane nefolosit in `api_code.go`, compilatorul spune; in prezent e folosit si in `readTaskFile`, deci ramane.)

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/web/ -count=1 2>&1 | tail -3 && go vet ./...`
Expected: `ok  hobstack/internal/web`, fara iesire de la vet.

---

### Task 4: Modala explorer-ului (`code-explorer.js` + CSS)

**Files:**
- Modify: `internal/web/static/js/file-view.js`
- Create: `internal/web/static/js/code-explorer.js`
- Modify: `internal/web/static/app.css` (dupa blocul `/* modala de fisier ... */`, inainte de `.change-row`)

**Interfaces:**
- Consumes: `GET /api/projects/{id}/tree` (Task 2), `GET /api/projects/{id}/code-file` (Task 3); `api(path, opts)` din `api.js` (erorile au `.status`); `el`, `toast` din `ui.js`; `languageFor` din `highlight.js`.
- Nota: alegerea altui agent din sageata de langa "Ask" trimite direct catre el (si il tine minte pentru restul
  sesiunii explorer-ului).
- Produces:
  - din `file-view.js`: `export function codeWithMarkers(text, lang, markers)` (exista, doar devine export) si `export async function copyText(text)`.
  - din `code-explorer.js`: `export function openCodeExplorer({ projectId, project, defaultAgentId, onAsk, onClose })` → `{ close() }`. `project` = `state.project` (are `agents[]` cu `id, name, folderId, detached`); `onAsk(agentId, text)` (Task 5); `onClose()` chemat o data la inchidere.

- [ ] **Step 1: Export helpers in `file-view.js`**

Schimba `function codeWithMarkers(` in `export function codeWithMarkers(`. Adauga dupa importuri:

```js
// copyText: textul in clipboard, cu toast; fara clipboard (http fara permisiune) toast-ul arata textul
export async function copyText(text) {
  try { await navigator.clipboard.writeText(text); toast(`Copied ${text}`, "ok"); } catch { toast(text); }
}
```

Si butonul Copy path din `openFileView` devine:

```js
  const copy = el("button", { type: "button", class: "btn small", onclick: () => copyText(current.path) }, "Copy path");
```

- [ ] **Step 2: Create `code-explorer.js`**

```js
import { api } from "./api.js";
import { el } from "./ui.js";
import { languageFor } from "./highlight.js";
import { codeWithMarkers, copyText } from "./file-view.js";

const isMac = /Mac|iPhone|iPad/.test(navigator.platform);
export const shortcutLabel = isMac ? "⌘P" : "Ctrl+P";

// searchFiles: cautarea dupa nume, in ordinea din codefiles.SearchIdx: numele incepe cu q, numele
// contine q, calea contine q; la egalitate numele mai scurt, apoi calea. items: [{ folder, path }].
export function searchFiles(items, q, limit) {
  q = q.trim().toLowerCase();
  if (!q) return [];
  const hits = [];
  for (const it of items) {
    const lp = it.path.toLowerCase();
    const name = lp.slice(lp.lastIndexOf("/") + 1);
    const tier = name.startsWith(q) ? 0 : name.includes(q) ? 1 : lp.includes(q) ? 2 : -1;
    if (tier >= 0) hits.push({ it, tier, n: name.length, lp });
  }
  hits.sort((a, b) => a.tier - b.tier || a.n - b.n || (a.lp < b.lp ? -1 : a.lp > b.lp ? 1 : 0));
  return hits.slice(0, limit).map((h) => h.it);
}

// absPath: calea absoluta, cu separatorul folderului proiectului
function absPath(folder, rel) {
  const sep = folder.path.includes("\\") && !folder.path.includes("/") ? "\\" : "/";
  return folder.path.replace(/[\\/]+$/, "") + sep + rel.split("/").join(sep);
}

// openCodeExplorer: explorer-ul de cod al proiectului, doar citire (Cmd/Ctrl+P).
// - defaultAgentId: agentul pentru "Ask" (cel din panou sau radacina)
// - onAsk(agentId, text): deschide chat-ul agentului cu textul la inceputul campului
// - onClose(): dupa inchidere
// Intoarce { close() }.
export function openCodeExplorer({ projectId, project, defaultAgentId, onAsk, onClose }) {
  const storeKey = `hobstack-code-${projectId}`;
  let saved = {};
  try { saved = JSON.parse(localStorage.getItem(storeKey) || "{}") || {}; } catch {}
  const openDirs = new Set(Array.isArray(saved.open) ? saved.open : []); // "<folderId>/<cale>" ("<id>/" = radacina)
  let folders = [];        // raspunsul /tree
  let all = [];            // [{ folder, path }] pentru cautare
  let current = null;      // { folder, path } deschis
  let sel = null;          // { from, to } liniile selectate
  let anchorLine = null;   // linia de la care pleaca Shift+clic
  let agentId = defaultAgentId;
  let seq = 0;             // ultima cerere de fisier
  let results = [];        // rezultatele cautarii
  let active = 0;          // rezultatul ales
  let closed = false;

  const agents = (project.agents || []).filter((a) => !a.detached);
  const agentName = (id) => agents.find((a) => a.id === id)?.name || "agent";

  function persist() {
    try { localStorage.setItem(storeKey, JSON.stringify({ open: [...openDirs], last: current })); } catch {}
  }

  // ---------- caile pentru copiere si chat ----------
  const folderOf = (id) => folders.find((f) => f.id === id);
  // copyPath: relativa in folder; cu mai multe foldere, prefixata cu numele folderului
  const copyPath = (c) => (folders.length > 1 ? `${folderOf(c.folder).name}/${c.path}` : c.path);
  // chatPath: ca @ in chat: relativa in folderul agentului, absoluta altfel
  function chatPath(c, aid) {
    const a = agents.find((x) => x.id === aid);
    const home = a?.folderId ?? folders[0]?.id;
    return c.folder === home ? c.path : absPath(folderOf(c.folder), c.path);
  }
  const lines = () => (sel ? (sel.from === sel.to ? `:${sel.from}` : `:${sel.from}-${sel.to}`) : "");

  // ---------- DOM ----------
  const search = el("input", { type: "search", class: "cx-search", placeholder: "Go to file…", "aria-label": "Go to file",
    autocomplete: "off", spellcheck: "false" });
  const resultsBox = el("ul", { class: "cx-results", role: "listbox", hidden: true });
  const tree = el("nav", { class: "cx-tree", "aria-label": "Project files" }, el("p", { class: "hint" }, "Loading..."));
  const pathBox = el("span", { class: "change-path" });
  const menuBox = el("div", { class: "cx-menu", hidden: true });
  const moreBtn = el("button", { type: "button", class: "btn small", "aria-label": "More", hidden: true,
    onclick: (e) => { e.stopPropagation(); toggleMenu([["Copy absolute path", () => copyText(absPath(folderOf(current.folder), current.path))]]); } }, "⋯");
  const copyBtn = el("button", { type: "button", class: "btn small", hidden: true, onclick: () => copyText(copyPath(current)) }, "Copy path");
  const codeBox = el("div", { class: "cx-code" }, el("p", { class: "hint cx-empty" }, `Pick a file on the left or search with ${shortcutLabel}.`));
  const bar = el("div", { class: "cx-bar", hidden: true });
  const closeBtn = el("button", { type: "button", class: "modal-close", "aria-label": "Close", onclick: () => close() }, "×");
  const box = el("div", { class: "modal cx", role: "dialog", "aria-modal": "true", "aria-label": "Code explorer" },
    el("div", { class: "cx-top" }, el("div", { class: "cx-search-wrap" }, search, resultsBox), closeBtn),
    el("div", { class: "cx-main" }, tree,
      el("div", { class: "cx-view" },
        el("div", { class: "fv-head cx-head" }, pathBox, el("span", { class: "fv-actions" }, copyBtn, moreBtn), menuBox),
        codeBox)));
  const overlay = el("div", { class: "modal-overlay cx-overlay", onclick: (e) => { if (e.target === overlay) close(); } }, box);
  const before = document.activeElement;

  // ---------- meniul mic (⋯ si alegerea agentului) ----------
  function toggleMenu(items, anchor) {
    if (!menuBox.hidden) { menuBox.hidden = true; return; }
    menuBox.replaceChildren(...items.map(([label, fn]) => el("button", { type: "button", class: "filter-item",
      onclick: () => { menuBox.hidden = true; fn(); } }, label)));
    menuBox.classList.toggle("cx-menu-bar", !!anchor);
    if (anchor) { bar.append(menuBox); } else { box.querySelector(".cx-head").append(menuBox); }
    menuBox.hidden = false;
  }

  // ---------- arborele ----------
  // buildNode: { dirs: Map(nume -> nod), files: [nume] } din lista de cai
  function buildNode(files) {
    const root = { dirs: new Map(), files: [] };
    for (const p of files) {
      const parts = p.split("/");
      let n = root;
      for (const d of parts.slice(0, -1)) {
        if (!n.dirs.has(d)) n.dirs.set(d, { dirs: new Map(), files: [] });
        n = n.dirs.get(d);
      }
      n.files.push(parts[parts.length - 1]);
    }
    return root;
  }

  // renderDir: randurile unui folder desfasurat; subfolderele se deseneaza doar daca sunt deschise
  function renderDir(f, node, dir, depth, out) {
    const pad = `${10 + depth * 14}px`;
    for (const name of [...node.dirs.keys()].sort((a, b) => a.localeCompare(b))) {
      const path = dir + name + "/";
      const key = `${f.id}/${path}`;
      const open = openDirs.has(key);
      out.push(el("button", { type: "button", class: "cx-dir", style: `padding-left:${pad}`, "aria-expanded": String(open),
        onclick: () => { if (open) openDirs.delete(key); else openDirs.add(key); persist(); renderTree(); } },
        el("span", { class: "cx-caret" }, open ? "▾" : "▸"), name));
      if (open) renderDir(f, node.dirs.get(name), path, depth + 1, out);
    }
    for (const name of [...node.files].sort((a, b) => a.localeCompare(b))) {
      const path = dir + name;
      const on = current && current.folder === f.id && current.path === path;
      out.push(el("button", { type: "button", class: "cx-file" + (on ? " on" : ""), style: `padding-left:${pad}`, title: path,
        "aria-current": on ? "true" : null, onclick: () => openFile({ folder: f.id, path }, false) }, name));
    }
  }

  function renderTree() {
    const out = [];
    for (const f of folders) {
      const key = `${f.id}/`;
      const open = openDirs.has(key);
      out.push(el("button", { type: "button", class: "cx-dir cx-root", "aria-expanded": String(open),
        onclick: () => { if (open) openDirs.delete(key); else openDirs.add(key); persist(); renderTree(); } },
        el("span", { class: "cx-caret" }, open ? "▾" : "▸"), f.name));
      if (!open) continue;
      if (f.error) { out.push(el("p", { class: "hint cx-note" }, f.error)); continue; }
      f.node ||= buildNode(f.files);
      renderDir(f, f.node, "", 1, out);
      if (f.truncated) out.push(el("p", { class: "hint cx-note" }, `Only the first ${f.files.length} files are listed; use search.`));
    }
    tree.replaceChildren(...out);
  }

  // reveal: desfasoara folderele parinte ale fisierului si il aduce in zona vizibila
  function reveal(c) {
    openDirs.add(`${c.folder}/`);
    const parts = c.path.split("/");
    for (let i = 1; i < parts.length; i++) openDirs.add(`${c.folder}/${parts.slice(0, i).join("/")}/`);
    persist();
    renderTree();
    tree.querySelector(".cx-file.on")?.scrollIntoView({ block: "nearest" });
  }

  async function loadTree() {
    try {
      const res = await api(`/api/projects/${projectId}/tree`);
      if (closed) return;
      folders = res.folders || [];
      all = folders.flatMap((f) => f.files.map((path) => ({ folder: f.id, path })));
      search.disabled = false;
      // la prima deschidere: radacina primului folder desfasurata
      if (!openDirs.size && folders[0]) openDirs.add(`${folders[0].id}/`);
      renderTree();
    } catch (err) {
      if (closed) return;
      search.disabled = true;
      tree.replaceChildren(el("p", { class: "hint cx-note" }, err.message),
        el("button", { type: "button", class: "btn small", onclick: () => { tree.replaceChildren(el("p", { class: "hint" }, "Loading...")); loadTree(); } }, "Retry"));
    }
  }

  // ---------- fisierul ----------
  async function openFile(c, doReveal = true) {
    current = { folder: c.folder, path: c.path };
    sel = null;
    anchorLine = null;
    bar.hidden = true;
    persist();
    if (doReveal) reveal(current); else renderTree();
    const f = folderOf(c.folder);
    const cut = c.path.lastIndexOf("/") + 1;
    pathBox.replaceChildren(el("span", { class: "change-dir" }, (folders.length > 1 ? f.name + "/" : "") + c.path.slice(0, cut)),
      el("span", { class: "change-name" }, c.path.slice(cut)));
    copyBtn.hidden = moreBtn.hidden = false;
    codeBox.replaceChildren(el("p", { class: "hint cx-empty" }, "Loading..."));
    const my = ++seq;
    try {
      const q = new URLSearchParams({ folder: String(c.folder), path: c.path });
      const v = await api(`/api/projects/${projectId}/code-file?${q}`);
      if (my !== seq || closed) return;
      if (v.binary) codeBox.replaceChildren(el("p", { class: "hint cx-empty" }, "Binary file: no preview."));
      else if (v.tooLarge) codeBox.replaceChildren(el("p", { class: "hint cx-empty" }, "The file is larger than 1 MB: no preview."));
      else codeBox.replaceChildren(codeWithMarkers(v.content, languageFor(c.path), null), bar);
      codeBox.scrollTop = 0;
      codeBox.scrollLeft = 0;
    } catch (err) {
      if (my !== seq || closed) return;
      if (err.status === 404) {
        codeBox.replaceChildren(el("p", { class: "hint cx-empty" }, "The file no longer exists."));
        loadTree(); // lista poate fi veche de pana la 30 s
      } else {
        codeBox.replaceChildren(el("p", { class: "hint cx-empty" }, err.message));
      }
    }
  }

  // ---------- selectia de linii ----------
  function paintSelection() {
    const rows = codeBox.querySelectorAll(".fv-line");
    rows.forEach((row, i) => row.classList.toggle("cx-sel", !!sel && i + 1 >= sel.from && i + 1 <= sel.to));
    if (!sel) { bar.hidden = true; return; }
    const first = rows[sel.from - 1];
    const ask = el("button", { type: "button", class: "btn small primary", onclick: () => ask(agentId) }, `Ask ${agentName(agentId)}`);
    const pick = agents.length > 1 ? el("button", { type: "button", class: "btn small primary cx-pick", "aria-label": "Ask another agent",
      onclick: (e) => { e.stopPropagation(); toggleMenu(agents.filter((a) => a.id !== agentId).map((a) => [`Ask ${a.name}`, () => { agentId = a.id; ask(a.id); }]), true); } }, "▾") : null;
    bar.replaceChildren(el("button", { type: "button", class: "btn small", onclick: () => copyText(copyPath(current) + lines()) }, "Copy reference"),
      el("span", { class: "cx-ask" }, ask, pick));
    bar.hidden = false;
    bar.style.top = `${Math.max(0, first.offsetTop - bar.offsetHeight - 4)}px`;
  }

  codeBox.addEventListener("click", (e) => {
    const num = e.target.closest(".fv-num");
    if (!num || !current) return;
    const n = Number(num.textContent);
    if (!n) return;
    if (e.shiftKey && anchorLine) sel = { from: Math.min(anchorLine, n), to: Math.max(anchorLine, n) };
    else if (sel && sel.from === n && sel.to === n) { sel = null; anchorLine = null; }
    else { sel = { from: n, to: n }; anchorLine = n; }
    window.getSelection()?.removeAllRanges(); // Shift+clic nu lasa text selectat
    paintSelection();
  });

  function ask(aid) {
    const text = `@${chatPath(current, aid)}${lines()} `;
    close();
    onAsk(aid, text);
  }

  // ---------- cautarea ----------
  function renderResults() {
    if (!results.length) {
      resultsBox.hidden = !search.value.trim();
      resultsBox.replaceChildren(el("li", { class: "mention-empty" }, "No matching file"));
      return;
    }
    resultsBox.replaceChildren(...results.map((r, i) => {
      const cut = r.path.lastIndexOf("/") + 1;
      const f = folderOf(r.folder);
      return el("li", { class: "mention-item" + (i === active ? " active" : ""), role: "option", "aria-selected": String(i === active),
        onmousedown: (e) => { e.preventDefault(); choose(i); } },
        el("span", { class: "mention-name" }, r.path.slice(cut)),
        el("span", { class: "mention-dir" }, (folders.length > 1 ? f.name + "/" : "") + r.path.slice(0, cut)));
    }));
    resultsBox.hidden = false;
    resultsBox.children[active]?.scrollIntoView({ block: "nearest" });
  }
  function choose(i) {
    const r = results[i];
    if (!r) return;
    search.value = "";
    results = [];
    resultsBox.hidden = true;
    openFile(r);
  }
  search.addEventListener("input", () => { results = searchFiles(all, search.value, 50); active = 0; renderResults(); });
  search.addEventListener("keydown", (e) => {
    if (e.key === "ArrowDown" || e.key === "ArrowUp") {
      if (!results.length) return;
      e.preventDefault();
      active = (active + (e.key === "ArrowDown" ? 1 : results.length - 1)) % results.length;
      renderResults();
    } else if (e.key === "Enter") {
      e.preventDefault();
      choose(active);
    }
  });
  search.addEventListener("blur", () => setTimeout(() => { resultsBox.hidden = true; }, 150));
  search.addEventListener("focus", () => { if (search.value.trim()) renderResults(); });

  // in arbore: sageata dreapta desfasoara un folder, stanga il restrange (Enter merge ca un clic pe buton)
  tree.addEventListener("keydown", (e) => {
    const b = e.target.closest(".cx-dir");
    if (!b) return;
    const open = b.getAttribute("aria-expanded") === "true";
    if ((e.key === "ArrowRight" && !open) || (e.key === "ArrowLeft" && open)) {
      e.preventDefault();
      const label = b.textContent;
      b.click();
      // butonul a fost redesenat: focusul pe noul buton cu acelasi text
      [...tree.querySelectorAll(".cx-dir")].find((x) => x.textContent.slice(1) === label.slice(1))?.focus();
    }
  });

  // ---------- tastatura ----------
  // capture: Esc / Tab nu ajung la modala de task de dedesubt
  function onKey(e) {
    const mod = e.metaKey || e.ctrlKey;
    if (e.key === "Escape") {
      e.preventDefault();
      e.stopPropagation();
      if (!menuBox.hidden) menuBox.hidden = true;
      else if (document.activeElement === search && search.value) { search.value = ""; results = []; resultsBox.hidden = true; }
      else if (sel) { sel = null; anchorLine = null; paintSelection(); }
      else close();
    } else if (e.key === "Tab") {
      e.stopPropagation();
    } else if (mod && e.shiftKey && e.key.toLowerCase() === "c" && current) {
      e.preventDefault();
      copyText(copyPath(current));
    } else if (mod && !e.shiftKey && e.key.toLowerCase() === "c" && current && document.activeElement !== search
      && !(window.getSelection()?.toString())) {
      e.preventDefault();
      copyText(copyPath(current) + lines());
    }
  }
  const onDocClick = (e) => { if (!menuBox.hidden && !menuBox.contains(e.target)) menuBox.hidden = true; };

  function close() {
    if (closed) return;
    closed = true;
    overlay.remove();
    document.removeEventListener("keydown", onKey, true);
    document.removeEventListener("click", onDocClick);
    before?.focus?.();
    onClose?.();
  }

  document.addEventListener("keydown", onKey, true);
  document.addEventListener("click", onDocClick);
  document.body.append(overlay);
  search.disabled = true;
  search.focus();
  loadTree().then(() => {
    // ultimul fisier deschis, daca mai e in lista
    const last = saved.last;
    if (!closed && last && all.some((x) => x.folder === last.folder && x.path === last.path)) openFile(last);
    if (!closed) search.focus();
  });
  return { close };
}
```

- [ ] **Step 3: Add CSS** (in `app.css`, dupa regula `.fv-diff .diff { ... }`)

```css
/* explorer de cod (Cmd/Ctrl+P): cautare sus, arbore in stanga, cod in dreapta; peste modala de fisier */
.cx-overlay { z-index: 70; }
.modal.cx { display: flex; flex-direction: column; max-width: 1340px; height: min(880px, 100%); }
.cx-top { display: flex; align-items: center; gap: 10px; padding: 12px 14px 12px 18px; border-bottom: 1px solid var(--line); }
.cx-search-wrap { position: relative; flex: 1; min-width: 0; }
.cx-search { width: 100%; font: 14px var(--sans); }
.cx-results { position: absolute; left: 0; right: 0; top: calc(100% + 6px); z-index: 5; max-height: 360px; overflow-y: auto;
  margin: 0; padding: 4px; list-style: none; background: var(--deep); border: 1px solid var(--line-2); border-radius: 10px; box-shadow: 0 8px 24px rgba(0, 0, 0, .35); }
.cx-results[hidden] { display: none; }
.cx-main { display: flex; flex: 1; min-height: 0; }
.cx-tree { width: 300px; flex: none; overflow: auto; border-right: 1px solid var(--line); background: var(--deep); padding: 8px 0 14px; }
.cx-dir, .cx-file { display: flex; align-items: center; gap: 6px; width: 100%; background: none; border: 0; border-left: 2px solid transparent;
  color: var(--soft); cursor: pointer; text-align: left; padding: 4px 12px; font: 12.5px var(--mono); font-variant-ligatures: none; white-space: nowrap; }
.cx-dir:hover, .cx-file:hover, .cx-dir:focus-visible, .cx-file:focus-visible { background: #191d23; color: var(--text); outline: none; }
.cx-file { padding-left: 30px; }
.cx-file.on { background: var(--card); color: var(--text); border-left-color: var(--blue); }
.cx-root { font: 600 13px var(--sans); color: var(--text); padding-top: 8px; }
.cx-caret { width: 12px; flex: none; color: var(--muted); font-size: 10px; }
.cx-note { padding: 4px 14px; margin: 0; font-size: 12.5px; }
.cx-view { display: flex; flex-direction: column; flex: 1; min-width: 0; min-height: 0; }
.cx-head { position: relative; min-height: 52px; }
.cx-code { position: relative; overflow: auto; flex: 1; min-height: 0; padding: 8px 0 16px; }
.cx-empty { padding: 8px 18px; }
.cx-code .fv-num { cursor: pointer; }
.cx-code .fv-num:hover { color: var(--text); }
.cx-code .fv-line.cx-sel { background: rgba(240, 180, 75, .1); }
.cx-code .fv-line.cx-sel .fv-gutter { background: var(--amber); }
.cx-bar { position: absolute; left: calc(4.2em + 20px); z-index: 2; display: flex; gap: 6px; align-items: center; padding: 4px;
  background: var(--deep); border: 1px solid var(--line-2); border-radius: 9px; box-shadow: 0 6px 18px rgba(0, 0, 0, .35); font-family: var(--sans); }
.cx-bar[hidden] { display: none; }
.cx-ask { display: inline-flex; }
.cx-ask .btn:first-child:not(:last-child) { border-top-right-radius: 0; border-bottom-right-radius: 0; }
.cx-pick { border-top-left-radius: 0; border-bottom-left-radius: 0; margin-left: 1px; padding-left: 7px; padding-right: 7px; }
.cx-menu { position: absolute; right: 14px; top: calc(100% - 4px); z-index: 6; min-width: 200px; display: grid; gap: 2px; padding: 6px;
  background: var(--col); border: 1px solid var(--line-2); border-radius: 10px; box-shadow: 0 10px 30px rgba(0, 0, 0, .35); }
.cx-menu[hidden] { display: none; }
.cx-menu.cx-menu-bar { right: auto; left: 0; top: calc(100% + 4px); }
@media (max-width: 720px) {
  .cx-main { flex-direction: column; }
  .cx-tree { width: auto; max-height: 35%; border-right: 0; border-bottom: 1px solid var(--line); }
}
```

- [ ] **Step 4: Syntax check**

Run:
```bash
cd internal/web/static/js && for f in file-view.js code-explorer.js; do cp $f "$TMPDIR/chk.mjs" && node --check "$TMPDIR/chk.mjs" && echo "$f ok"; done
```
Expected: `file-view.js ok`, `code-explorer.js ok`.

---

### Task 5: Integrarea in workspace (scurtatura, butonul Code, Ask) + verificare

**Files:**
- Modify: `internal/web/static/js/workspace.js` (importuri; `renderHeader`; langa `openAgent`; `cleanup` si ramura `if (gone())`)
- Modify: `CLAUDE.md`

**Interfaces:**
- Consumes: `openCodeExplorer`, `shortcutLabel` din `code-explorer.js` (Task 4); `openAgent(id, panel)`, `firstRootId()`, `input` (textarea-ul chat-ului), `state` din `workspaceView`.
- Produces: `toggleExplorer()`, `prefillChat(agentId, text)` (interne in `workspaceView`).

- [ ] **Step 1: Import si starea**

La importuri:

```js
import { openCodeExplorer, shortcutLabel } from "./code-explorer.js";
```

Langa `let dragging = null;` (dupa `folderChips`):

```js
  let explorer = null; // explorer-ul de cod deschis (Cmd/Ctrl+P), null = inchis
```

- [ ] **Step 2: `toggleExplorer`, `prefillChat` si scurtatura** (dupa functia `openAgent`)

```js
  // toggleExplorer: deschide / inchide explorer-ul de cod (Cmd/Ctrl+P, butonul Code)
  function toggleExplorer() {
    if (explorer) { explorer.close(); return; }
    const rootId = state.project.agents.find((a) => a.isRoot && !a.detached)?.id ?? firstRootId();
    explorer = openCodeExplorer({
      projectId, project: state.project,
      defaultAgentId: state.drawer && state.selected ? state.selected : rootId,
      onAsk: prefillChat,
      onClose: () => { explorer = null; },
    });
  }

  // prefillChat: chat-ul agentului deschis, cu text la inceputul campului (textul scris ramane dupa el)
  async function prefillChat(agentId, text) {
    if (!(state.drawer && state.selected === agentId && state.panel === "chat")) await openAgent(agentId, "chat");
    input.value = text + input.value.replace(/^\s+/, "");
    input.setSelectionRange(text.length, text.length);
    input.dispatchEvent(new Event("input", { bubbles: true })); // inaltimea campului, ciorna
    input.focus();
  }

  // Cmd/Ctrl+P pe paginile proiectului: explorer-ul de cod in locul dialogului de print
  const onShortcut = (e) => {
    if ((e.metaKey || e.ctrlKey) && !e.shiftKey && !e.altKey && e.key.toLowerCase() === "p") {
      e.preventDefault();
      toggleExplorer();
    }
  };
  document.addEventListener("keydown", onShortcut);
```

- [ ] **Step 3: Butonul Code in bara de sus** (`renderHeader`, inainte de `tabsNav,`)

```js
      el("button", { type: "button", class: "btn ghost", title: `Code explorer (${shortcutLabel})`, onclick: toggleExplorer }, "Code"),
```

- [ ] **Step 4: Curatenie la iesirea din proiect**

In ramura `if (gone()) {` si in `const cleanup = () => {`, langa `document.removeEventListener("pointerdown", closeMenus);`, adauga:

```js
    document.removeEventListener("keydown", onShortcut);
    explorer?.close();
```

- [ ] **Step 5: Verificari statice**

Run:
```bash
cd internal/web/static/js && cp workspace.js "$TMPDIR/chk.mjs" && node --check "$TMPDIR/chk.mjs" && echo ok
cd ../../../.. && go vet ./... && go build -race -o /dev/null ./cmd/hobstack && GOOS=darwin GOARCH=arm64 go build -o /dev/null ./cmd/hobstack && GOOS=windows go build -o /dev/null ./cmd/hobstack && go test ./internal/... -count=1 2>&1 | grep -v '^ok' ; echo done
```
Expected: `ok`, apoi doar `done` (niciun pachet FAIL).

- [ ] **Step 6: Verificare in browser (Chrome headless prin CDP)**

Daca `pgrep -fl "claude -p"` nu gaseste nimic: `go build -o hobstack ./cmd/hobstack`, opreste `hobstack start`, porneste `./hobstack start --no-open` in fundal. Apoi un script Node 22 (in scratchpad) care, la 1440x900 si 800x800, pe un proiect cu fisiere (si pe unul cu 2 foldere, daca exista; altfel creeaza unul de test prin API si sterge-l la final):

1. deschide `#/p/<pid>/tasks`, trimite `Input.dispatchKeyEvent` cu `key: "p"`, `modifiers: 4` (Meta; 2 = Ctrl pe Linux) → `.cx-overlay` exista; din nou → dispare;
2. Cmd+P, tasteaza un nume in `.cx-search` (`Input.insertText`), Enter → `.cx-head .change-name` are numele, `.cx-file.on` exista;
3. clic pe `.fv-num` 3, Shift+clic pe 6 (`Input.dispatchMouseEvent` cu `modifiers: 8`) → 4 randuri `.cx-sel`, `.cx-bar` vizibil;
4. `Browser.grantPermissions` cu `clipboardReadWrite`, clic pe "Copy reference" → `navigator.clipboard.readText()` = `<cale>:3-6` (cu prefixul folderului pe proiectul cu 2 foldere);
5. clic pe "Ask …" → `.cx-overlay` a disparut, textarea chat-ului incepe cu `@<cale>:3-6 ` (absolut daca fisierul e in alt folder decat al agentului);
6. deschide o modala de task (`#/p/<pid>/tasks/<id>`), Cmd+P, Esc → explorer-ul inchis, `.task-modal`/modala de task tot deschisa;
7. selecteaza text in cod cu `Runtime.evaluate` (Range pe un `.fv-text`), Cmd+C → clipboard-ul are textul selectat, nu referinta;
8. sterge un fisier de test dupa ce arborele s-a incarcat, clic pe el → "The file no longer exists.";
9. screenshot-uri (explorer cu selectie, 1440 si 800), citite cu Read.

Expected: toate verificarile `true`; layout-ul fara suprapuneri (bara de referinta deasupra primei linii selectate, arborele vizibil la 800).

- [ ] **Step 7: CLAUDE.md**

Dupa linia "Gata (fisierele doar sus): ..." (blocul ei de 4 randuri), adauga:

```
Gata (explorer de cod, spec in docs/superpowers/specs/2026-10-12-hobstack-code-explorer-design.md): Cmd/Ctrl+P sau butonul
Code pe paginile proiectului deschide code-explorer.js (doar citire): arbore pe foldere (desenare lenesa, starea in
localStorage "hobstack-code-<pid>"), cautare dupa nume, cod colorat. GET /api/projects/{id}/tree (lista de la @, cu
truncated/error) si /code-file?folder=&path= (doar cai din lista, cache invalidat o data, readIn + isBinary). Clic / Shift+clic
pe numere selecteaza linii: Copy reference (cale:42-50, prefixul folderului cu mai multe foldere), Ask <agent> pune
"@cale:42-50 " in chat (prefillChat; calea ca la @). Cmd+C fara text selectat = referinta, Cmd+Shift+C = calea.
```
