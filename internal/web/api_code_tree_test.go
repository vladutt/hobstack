package web

import (
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
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
	// dupa 404, arborele nu mai are fisierul (cache-ul s-a invalidat)
	if files := getTree(t, e, p.ID).Folders[0].Files; slices.Contains(files, "app/New.php") {
		t.Fatalf("fisierul sters a ramas in arbore: %v", files)
	}
}

// repo git: un fisier urmarit si sters de pe disc (rm fara commit) nu mai apare in lista
func TestProjectTreeGitDeleted(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git lipseste")
	}
	e := setup(t)
	dir := t.TempDir()
	writeFiles(t, dir, "keep.go", "old.go")
	for _, args := range [][]string{{"init", "-q"}, {"add", "keep.go", "old.go"}} {
		if out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	os.Remove(filepath.Join(dir, "old.go"))
	p := e.createProject(t, dir)
	if files := getTree(t, e, p.ID).Folders[0].Files; slices.Contains(files, "old.go") || !slices.Contains(files, "keep.go") {
		t.Fatalf("%v", files)
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
	// link.txt e in lista (git il vede ca fisier nou), deci refuzul vine de la os.OpenRoot, nu de la lista
	if files := getTree(t, e, p.ID).Folders[0].Files; !slices.Contains(files, "link.txt") {
		t.Fatalf("link.txt lipseste din lista: %v", files)
	}
	if code, res := getCode(t, e, p.ID, p.Folders[0].ID, "link.txt"); code != http.StatusNotFound {
		t.Fatalf("symlink in afara: %d %+v", code, res)
	}
}
