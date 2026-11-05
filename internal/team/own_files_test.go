package team

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"hobstack/internal/runner"
	"hobstack/internal/store"
)

func TestRmTargets(t *testing.T) {
	cases := []struct {
		cmd  string
		want []string
	}{
		{"rm -rf .scratch", []string{".scratch"}},
		{"rm -r -f out/a.log 'tmp dir'", []string{"out/a.log", "tmp dir"}},
		{"/bin/rm --recursive --force -- -weird", []string{"-weird"}},
		{"rm -rf *.log", nil},        // glob
		{"rm -rf x && echo ok", nil}, // alta comanda
		{"rm -rf $HOME/x", nil},      // variabila
		{"rm -i x", nil},             // flag necunoscut
		{"rmdir x", nil},
		{"rm", nil},
	}
	for _, c := range cases {
		got, ok := rmTargets(c.cmd)
		if (c.want == nil) == ok || !slices.Equal(got, c.want) {
			t.Errorf("rmTargets(%q) = %q %v, want %q", c.cmd, got, ok, c.want)
		}
	}
}

// created inregistreaza fisierul ca facut de agent pe task si il scrie pe disc.
func created(t *testing.T, f *fixture, task store.Task, a store.Agent, rel string) {
	t.Helper()
	p := filepath.Join(f.p.Workdir, rel)
	os.MkdirAll(filepath.Dir(p), 0o755)
	os.WriteFile(p, []byte("x"), 0o644)
	id := a.ID
	if err := f.st.AddTaskChanges([]store.TaskChange{{TaskID: task.ID, RunID: "r1", AgentID: &id, AgentName: a.Name, Kind: "create", Tool: "Write", Path: rel}}); err != nil {
		t.Fatal(err)
	}
}

func TestDeleteOwnFiles(t *testing.T) {
	f := setup(t)
	task := f.task(t, "Scratch", f.rook, store.StatusInProgress)
	c := f.workerCaller(f.rook, task.ID)
	created(t, f, task, f.rook, ".scratch/a.txt")
	created(t, f, task, f.rook, ".scratch/sub/b.txt")
	created(t, f, task, f.rook, "notes.md")
	os.WriteFile(filepath.Join(f.p.Workdir, "user.txt"), []byte("mine"), 0o644)

	// un fisier al utilizatorului: refuzat, nimic sters
	if _, err := f.tm.DeleteFiles(c, []string{"notes.md", "user.txt"}); err == nil || !strings.Contains(err.Error(), "user.txt was not created by you") {
		t.Fatalf("foreign file: %v", err)
	}
	if _, err := os.Stat(filepath.Join(f.p.Workdir, "notes.md")); err != nil {
		t.Fatal("notes.md was deleted although the request was refused")
	}
	// in afara folderului
	if _, err := f.tm.DeleteFiles(c, []string{"../x"}); err == nil || !strings.Contains(err.Error(), "not inside your folder") {
		t.Fatalf("outside: %v", err)
	}
	// alt agent nu poate sterge fisierele lui Rook
	if _, err := f.tm.DeleteFiles(f.workerCaller(f.vera, task.ID), []string{"notes.md"}); err == nil {
		t.Fatal("Vera deleted Rook's file")
	}
	// folderul lui, cu tot ce e in el creat de el, si fisierul lui
	msg, err := f.tm.DeleteFiles(c, []string{".scratch", "notes.md"})
	if err != nil || !strings.Contains(msg, "Deleted .scratch, notes.md") {
		t.Fatalf("own: %q %v", msg, err)
	}
	if _, err := os.Stat(filepath.Join(f.p.Workdir, ".scratch")); !os.IsNotExist(err) {
		t.Fatal(".scratch is still there")
	}
	// un folder cu un fisier strain nu se sterge
	created(t, f, task, f.rook, "out/r.log")
	os.WriteFile(filepath.Join(f.p.Workdir, "out", "user.log"), []byte("u"), 0o644)
	if _, err := f.tm.DeleteFiles(c, []string{"out"}); err == nil || !strings.Contains(err.Error(), "out/user.log") {
		t.Fatalf("mixed folder: %v", err)
	}
	// istoricul task-ului
	evs, _ := f.st.ListTaskEvents(task.ID)
	if !slices.ContainsFunc(evs, func(e store.TaskEvent) bool { return e.Kind == "deleted_files" }) {
		t.Fatalf("no deleted_files event: %+v", evs)
	}
}

func TestDeleteOwnFileTrackedByGit(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git")
	}
	f := setup(t)
	task := f.task(t, "Git", f.rook, store.StatusInProgress)
	created(t, f, task, f.rook, "gen.go")
	for _, args := range [][]string{{"init", "-q"}, {"add", "gen.go"}, {"-c", "user.name=T", "-c", "user.email=t@t", "commit", "-qm", "x"}} {
		if out, err := exec.Command("git", append([]string{"-C", f.p.Workdir}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	if _, err := f.tm.DeleteFiles(f.workerCaller(f.rook, task.ID), []string{"gen.go"}); err == nil || !strings.Contains(err.Error(), "tracked by git") {
		t.Fatalf("tracked: %v", err)
	}
}

// rm -rf pe fisierele lui, oprit de regula Ask: HobStack le sterge, fara cerere in Needs you
func TestDeniedRmOfOwnFilesDeletesWithoutAsking(t *testing.T) {
	f := setup(t)
	f.rook.Tools = []string{"shell", "write"}
	f.st.UpdateAgent(f.rook)
	task := f.task(t, "Scratch", f.rook, store.StatusInProgress)
	created(t, f, task, f.rook, ".scratch/a.txt")
	os.WriteFile(filepath.Join(f.p.Workdir, "keep.txt"), []byte("u"), 0o644)
	ar := activeRun{projectID: f.p.ID, taskID: task.ID}
	f.tm.work.Lock()
	f.tm.recordDenials(ar, f.rook, []runner.Denial{{Tool: "Bash", Detail: "rm -rf .scratch"}})
	f.tm.work.Unlock()
	if reqs, _ := f.st.ListOpenAccessRequests(f.p.ID); len(reqs) != 0 {
		t.Fatalf("own files: no request expected, got %+v", reqs)
	}
	if _, err := os.Stat(filepath.Join(f.p.Workdir, ".scratch")); !os.IsNotExist(err) {
		t.Fatal(".scratch was not deleted")
	}
	// un fisier al utilizatorului: cererea ramane la utilizator
	f.tm.work.Lock()
	f.tm.recordDenials(ar, f.rook, []runner.Denial{{Tool: "Bash", Detail: "rm -rf keep.txt"}})
	f.tm.work.Unlock()
	if reqs, _ := f.st.ListOpenAccessRequests(f.p.ID); len(reqs) != 1 || reqs[0].Prefix != "rm -rf" {
		t.Fatalf("foreign file: %+v", reqs)
	}
	if _, err := os.Stat(filepath.Join(f.p.Workdir, "keep.txt")); err != nil {
		t.Fatal("keep.txt was deleted")
	}
}

// rm dintr-un subfolder (dupa cd): ".scratch" se gaseste printre caile create de agent
func TestDeleteOwnFromSubfolder(t *testing.T) {
	f := setup(t)
	task := f.task(t, "Landing", f.rook, store.StatusInProgress)
	created(t, f, task, f.rook, "site/app/.scratch/t.mjs")
	msg, err := f.tm.DeleteFiles(f.workerCaller(f.rook, task.ID), []string{".scratch"})
	if err != nil || !strings.Contains(msg, "site/app/.scratch") {
		t.Fatalf("%q %v", msg, err)
	}
	if _, err := os.Stat(filepath.Join(f.p.Workdir, "site", "app", ".scratch")); !os.IsNotExist(err) {
		t.Fatal("not deleted")
	}
	// doua foldere .scratch create de el: ambiguu, nu sterge nimic
	created(t, f, task, f.rook, "a/.scratch/1")
	created(t, f, task, f.rook, "b/.scratch/2")
	if msg, err := f.tm.DeleteFiles(f.workerCaller(f.rook, task.ID), []string{".scratch"}); err != nil || !strings.Contains(msg, "Nothing to delete") {
		t.Fatalf("ambiguous: %q %v", msg, err)
	}
}
