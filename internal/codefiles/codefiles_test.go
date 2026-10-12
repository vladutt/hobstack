package codefiles

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
	"time"
)

func touch(t *testing.T, root string, rels ...string) {
	t.Helper()
	for _, rel := range rels {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func sorted(s []string) []string {
	out := append([]string(nil), s...)
	sort.Strings(out)
	return out
}

// folder fara git: se sar folderele de dependente, build-urile si cele ascunse
func TestListWalkSkipsDependencies(t *testing.T) {
	dir := t.TempDir()
	touch(t, dir, "app/Http/UserController.php", "resources/js/app.js", "README.md",
		"node_modules/lodash/index.js", "vendor/laravel/x.php", ".git/config", ".idea/w.xml",
		"dist/app.js", "build/out.js", "sub/node_modules/y.js", ".env")
	got, err := List(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{".env", "README.md", "app/Http/UserController.php", "resources/js/app.js"}
	if !reflect.DeepEqual(sorted(got), want) {
		t.Fatalf("got %v, want %v", sorted(got), want)
	}
}

// repo git: .gitignore decide, fisierele noi neignorate apar si ele
func TestListGitRespectsGitignore(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git lipseste")
	}
	dir := t.TempDir()
	// node_modules lipseste din .gitignore: tot se sare
	touch(t, dir, "src/main.go", "src/new.go", "storage/logs/app.log", ".gitignore", "node_modules/x/index.js")
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("storage/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"init", "-q"}, {"add", "src/main.go", ".gitignore"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	got, err := List(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{".gitignore", "src/main.go", "src/new.go"}
	if !reflect.DeepEqual(sorted(got), want) {
		t.Fatalf("got %v, want %v", sorted(got), want)
	}
}

func TestListCapsFiles(t *testing.T) {
	dir := t.TempDir()
	old := MaxFiles
	MaxFiles = 3
	t.Cleanup(func() { MaxFiles = old })
	touch(t, dir, "a.go", "b.go", "c.go", "d.go", "e.go")
	got, err := List(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("%d fisiere, vreau 3", len(got))
	}
}

func TestSearchRanksNameMatchesFirst(t *testing.T) {
	files := []string{"app/Models/User.php", "tests/UserTest.php", "app/Http/Controllers/UserController.php",
		"docs/user-guide.md", "app/Http/Kernel.php", "src/users/list.js"}
	got := Search(files, "user", 4)
	// numele care incepe cu cautarea, apoi numele care o contine, apoi doar calea; la egalitate numele mai scurt
	want := []string{"app/Models/User.php", "tests/UserTest.php", "docs/user-guide.md", "app/Http/Controllers/UserController.php"}
	if got := Search(files, "users/", 5); !reflect.DeepEqual(got, []string{"src/users/list.js"}) {
		t.Fatalf("doar pe cale: %v", got)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	if got := Search(files, "kernel", 10); !reflect.DeepEqual(got, []string{"app/Http/Kernel.php"}) {
		t.Fatalf("kernel: %v", got)
	}
	if got := Search(files, "", 2); len(got) != 2 {
		t.Fatalf("cautare goala: %v", got)
	}
	if got := Search(files, "nothing-here", 5); len(got) != 0 {
		t.Fatalf("fara potrivire: %v", got)
	}
}

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
