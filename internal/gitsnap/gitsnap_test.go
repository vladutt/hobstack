package gitsnap

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func run(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func write(t *testing.T, dir, rel, content string) {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// repo: un repo cu un commit; files: cale -> continut
func repo(t *testing.T, files map[string]string) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git lipseste")
	}
	dir := t.TempDir()
	run(t, dir, "init", "-q")
	for p, c := range files {
		write(t, dir, p, c)
	}
	write(t, dir, ".gitignore", "ignored/\n")
	run(t, dir, "add", "-A")
	run(t, dir, "commit", "-q", "-m", "init")
	return dir
}

func byPath(cs []Change) map[string]Change {
	m := map[string]Change{}
	for _, c := range cs {
		m[c.Path] = c
	}
	return m
}

func TestNotARepo(t *testing.T) {
	s, err := Take(t.TempDir())
	if s != nil || err != nil {
		t.Fatalf("got %v, %v; want nil, nil", s, err)
	}
	cs, err := s.Changes(nil) // nil snapshot: nimic
	if cs != nil || err != nil {
		t.Fatalf("nil snapshot: %v, %v", cs, err)
	}
}

func TestShellChanges(t *testing.T) {
	dir := repo(t, map[string]string{
		"a.txt":   "one\ntwo\nthree\n",
		"b.txt":   "keep\n",
		"del.txt": "bye\n",
		"pre.txt": "base\n",
	})
	write(t, dir, "pre.txt", "base\ndirty before\n") // modificat inainte de run, apoi neatins
	write(t, dir, "old-new.txt", "untracked before\n")
	s, err := Take(dir)
	if err != nil || s == nil {
		t.Fatalf("Take: %v, %v", s, err)
	}

	write(t, dir, "a.txt", "one\nTWO\nthree\n")         // sed -i
	write(t, dir, "src/new.php", "<?php\necho 1;\n")    // cat > ... <<EOF
	os.Remove(filepath.Join(dir, "del.txt"))            // rm
	write(t, dir, "old-new.txt", "untracked changed\n") // nou inainte, schimbat in run
	write(t, dir, "ignored/x.log", "noise\n")           // ignorat de git

	cs, err := s.Changes(nil)
	if err != nil {
		t.Fatal(err)
	}
	m := byPath(cs)
	if len(cs) != 4 {
		t.Fatalf("got %d changes: %+v", len(cs), cs)
	}
	if c := m["a.txt"]; c.Kind != "edit" || len(c.Patch) != 1 || !strings.Contains(strings.Join(c.Patch[0].Lines, "\n"), "-two\n+TWO") {
		t.Errorf("a.txt: %+v", c)
	}
	if c := m["src/new.php"]; c.Kind != "create" || len(c.Patch) != 1 || c.Patch[0].NewStart != 1 || c.Patch[0].NewLines != 2 {
		t.Errorf("src/new.php: %+v", c)
	}
	if c := m["del.txt"]; c.Kind != "delete" || len(c.Patch) != 1 || c.Patch[0].Lines[0] != "-bye" {
		t.Errorf("del.txt: %+v", c)
	}
	// starea de la pornire era cea modificata (nu HEAD): diff-ul e fata de ea
	if c := m["old-new.txt"]; c.Kind != "edit" || strings.Join(c.Patch[0].Lines, "|") != "-untracked before|+untracked changed" {
		t.Errorf("old-new.txt: %+v", c)
	}
	if _, ok := m["pre.txt"]; ok {
		t.Error("pre.txt was dirty before the run and untouched: must not appear")
	}
}

func TestCommitInRun(t *testing.T) {
	dir := repo(t, map[string]string{"a.txt": "a\n", "b.txt": "b\n"})
	write(t, dir, "a.txt", "a2\n") // munca altui task, necommit-uita
	s, _ := Take(dir)
	// run-ul face commit (ca T-2) si schimba b.txt prin shell, tot commit-uit
	write(t, dir, "b.txt", "b2\n")
	run(t, dir, "add", "-A")
	run(t, dir, "commit", "-q", "-m", "work")
	cs, err := s.Changes(nil)
	if err != nil {
		t.Fatal(err)
	}
	m := byPath(cs)
	// a.txt doar a intrat in commit (acelasi continut): apare cu diff-ul din commit
	if c, ok := m["a.txt"]; !ok || !c.Committed || c.Kind != "edit" || len(c.Patch) != 1 ||
		strings.Join(c.Patch[0].Lines, "|") != "-a|+a2" {
		t.Errorf("a.txt committed in the run: %+v", m["a.txt"])
	}
	if c, ok := m["b.txt"]; !ok || c.Kind != "edit" || c.Committed {
		t.Errorf("b.txt changed and committed in the run: %+v", cs)
	}
}

// un fisier nou, deja creat la pornire, intra in commit; in subfolder caile raman relative
func TestCommitInRunSubfolder(t *testing.T) {
	dir := repo(t, map[string]string{"api/x.go": "x\n", "web/y.js": "y\n"})
	write(t, dir, "api/n.go", "n\n")
	write(t, dir, "web/y.js", "y2\n")
	s, _ := Take(filepath.Join(dir, "api"))
	run(t, dir, "add", "-A")
	run(t, dir, "commit", "-q", "-m", "work")
	cs, err := s.Changes(nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(cs) != 1 || cs[0].Path != "n.go" || cs[0].Kind != "create" || !cs[0].Committed {
		t.Fatalf("want only n.go, created by the commit: %+v", cs)
	}
}

// HEAD mutat inapoi (reset --soft) nu e un commit facut in run: fisierele cu acelasi continut nu apar
func TestResetIsNotCommit(t *testing.T) {
	dir := repo(t, map[string]string{"a.txt": "a\n"})
	write(t, dir, "a.txt", "a2\n")
	run(t, dir, "commit", "-q", "-am", "second")
	s, _ := Take(dir)
	run(t, dir, "reset", "-q", "--soft", "HEAD~1")
	cs, err := s.Changes(nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(cs) != 0 {
		t.Fatalf("reset: nothing changed on disk: %+v", cs)
	}
}

func TestSkipAndSubfolder(t *testing.T) {
	dir := repo(t, map[string]string{"api/x.go": "x\n", "web/y.js": "y\n"})
	api := filepath.Join(dir, "api")
	s, err := Take(api)
	if err != nil || s == nil {
		t.Fatalf("Take: %v, %v", s, err)
	}
	write(t, dir, "api/x.go", "x2\n")
	write(t, dir, "api/z.go", "z\n")
	write(t, dir, "web/y.js", "y2\n") // in afara folderului agentului
	cs, err := s.Changes(map[string]bool{"z.go": true})
	if err != nil {
		t.Fatal(err)
	}
	if len(cs) != 1 || cs[0].Path != "x.go" {
		t.Fatalf("want only x.go (relative to api/, z.go skipped): %+v", cs)
	}
}

func TestBinaryAndUnchanged(t *testing.T) {
	dir := repo(t, map[string]string{"img.bin": "a\x00b"})
	s, _ := Take(dir)
	cs, _ := s.Changes(nil)
	if len(cs) != 0 {
		t.Fatalf("nothing changed: %+v", cs)
	}
	write(t, dir, "img.bin", "c\x00d")
	cs, _ = s.Changes(nil)
	if len(cs) != 1 || !cs[0].Truncated || cs[0].Patch != nil {
		t.Fatalf("binary: want one truncated change without patch: %+v", cs)
	}
}

func TestParseHunks(t *testing.T) {
	out := "diff --git a/a b/b\n--- a/a\n+++ b/b\n@@ -1 +1,2 @@\n-x\n+y\n+z\n\\ No newline at end of file\n@@ -10,0 +12 @@ f()\n+w\n"
	hs, tr := parseHunks([]byte(out))
	if tr || len(hs) != 2 {
		t.Fatalf("%+v %v", hs, tr)
	}
	if h := hs[0]; h.OldStart != 1 || h.OldLines != 1 || h.NewStart != 1 || h.NewLines != 2 || len(h.Lines) != 3 {
		t.Errorf("first: %+v", h)
	}
	if h := hs[1]; h.OldStart != 10 || h.OldLines != 0 || h.NewStart != 12 || h.NewLines != 1 {
		t.Errorf("second: %+v", h)
	}
}
