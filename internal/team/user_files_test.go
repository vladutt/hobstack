package team

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"foreman/internal/config"
	"foreman/internal/store"
)

// stage scrie fisierele in folderul de staging, ca upload-ul din web.
func stage(t *testing.T, dir string, names ...string) []StagedFile {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	var out []StagedFile
	for i, n := range names {
		stored := string(rune('a'+i)) + "0-" + n
		if err := os.WriteFile(filepath.Join(dir, stored), []byte("data "+n), 0o644); err != nil {
			t.Fatal(err)
		}
		out = append(out, StagedFile{Dir: dir, Stored: stored, Name: n, Mime: "text/plain", Size: int64(len("data " + n))})
	}
	return out
}

func hasAddDir(args []string, dir string) bool {
	for i, a := range args {
		if a == "--add-dir" && i+1 < len(args) && args[i+1] == dir {
			return true
		}
	}
	return false
}

// fisierele exista (rand + copie in task-files/<task>) inainte ca agentul sa porneasca pe task
func TestUserCreateTaskWithFiles(t *testing.T) {
	f := setup(t)
	root := t.TempDir()
	f.tm.SetFilesRoot(root)
	staging := filepath.Join(root, "staging")
	files := stage(t, staging, "brief.txt", "colors.txt")

	task, err := f.tm.UserCreateTaskWithFiles(f.p.ID, "Logo", "Use the brief.", f.rook.ID, files)
	if err != nil {
		t.Fatal(err)
	}
	dest := config.TaskFilesDir(root, f.p.ID, task.ID)
	list, err := f.st.ListUserTaskFiles(task.ID)
	if err != nil || len(list) != 2 || list[0].Name != "brief.txt" || list[0].Path != "brief.txt" || !list[0].FromUser() {
		t.Fatalf("%+v %v", list, err)
	}
	if _, err := os.Stat(filepath.Join(dest, list[0].Stored)); err != nil {
		t.Fatalf("copia nu e in task-files: %v", err)
	}
	if _, err := os.Stat(filepath.Join(staging, files[0].Stored)); !os.IsNotExist(err) {
		t.Fatalf("staging-ul trebuia mutat: %v", err)
	}

	mustReconcile(t, f)
	spec := f.fs.last(t, f.rook)
	tr := turn(spec)
	if !strings.Contains(tr, "Files from the user (read them with Read):\n- "+filepath.Join(dest, list[0].Stored)+" (brief.txt)") {
		t.Fatalf("tura fara fisierele utilizatorului:\n%s", tr)
	}
	if !hasAddDir(spec.Args, dest) {
		t.Fatalf("lipseste --add-dir %s: %v", dest, spec.Args)
	}
}

// un task fara fisiere nu primeste --add-dir pe task-files
func TestUserTaskWithoutFilesNoAddDir(t *testing.T) {
	f := setup(t)
	root := t.TempDir()
	f.tm.SetFilesRoot(root)
	task, err := f.tm.UserCreateTaskWithFiles(f.p.ID, "Logo", "", f.rook.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	mustReconcile(t, f)
	spec := f.fs.last(t, f.rook)
	if hasAddDir(spec.Args, config.TaskFilesDir(root, f.p.ID, task.ID)) || strings.Contains(turn(spec), "Files from the user") {
		t.Fatalf("fara fisiere: %v", spec.Args)
	}
}

// un asignat gresit: nu se creeaza nimic, staging-ul ramane (il sterge apelantul)
func TestUserCreateTaskWithFilesFails(t *testing.T) {
	f := setup(t)
	root := t.TempDir()
	f.tm.SetFilesRoot(root)
	files := stage(t, filepath.Join(root, "staging"), "brief.txt")
	var ae AssignError
	if _, err := f.tm.UserCreateTaskWithFiles(f.p.ID, "Logo", "", 0, files); !errors.As(err, &ae) {
		t.Fatalf("%v", err)
	}
	if tasks, _ := f.st.ListTasks(f.p.ID); len(tasks) != 0 {
		t.Fatalf("task creat: %+v", tasks)
	}
	// un rename imposibil anuleaza tot (task-ul abia creat se sterge)
	bad := []StagedFile{{Dir: filepath.Join(root, "nope"), Stored: "x-a.txt", Name: "a.txt"}}
	if _, err := f.tm.UserCreateTaskWithFiles(f.p.ID, "Logo", "", f.rook.ID, bad); err == nil {
		t.Fatal("rename-ul trebuia sa esueze")
	}
	if tasks, _ := f.st.ListTasks(f.p.ID); len(tasks) != 0 {
		t.Fatalf("task ramas dupa esec: %+v", tasks)
	}
}

func TestAddUserFiles(t *testing.T) {
	f := setup(t)
	root := t.TempDir()
	f.tm.SetFilesRoot(root)
	working := f.task(t, "A", f.rook, store.StatusInProgress)
	queued := f.task(t, "B", f.vera, store.StatusQueued)

	saved, err := f.tm.AddUserFiles(working.ID, stage(t, filepath.Join(root, "s1"), "a.txt", "b.txt"))
	if err != nil || len(saved) != 2 || !saved[0].FromUser() {
		t.Fatalf("%+v %v", saved, err)
	}
	und, _ := f.st.Undelivered(f.rook.ID)
	found := false
	for _, m := range und {
		if strings.Contains(m.Text, "The user attached 2 files to T-1: a.txt, b.txt.") {
			found = true
		}
	}
	if !found {
		t.Fatalf("nota pentru Rook lipseste: %+v", und)
	}
	evs, _ := f.st.ListTaskEvents(working.ID)
	if last := evs[len(evs)-1]; last.Kind != "user_files" || last.Actor != "You" || !strings.Contains(last.Body, "a.txt") {
		t.Fatalf("eveniment: %+v", last)
	}

	// un task in coada nu are nevoie de nota: fisierele intra in tura lui
	before, _ := f.st.Undelivered(f.vera.ID)
	if _, err := f.tm.AddUserFiles(queued.ID, stage(t, filepath.Join(root, "s2"), "c.txt")); err != nil {
		t.Fatal(err)
	}
	if after, _ := f.st.Undelivered(f.vera.ID); len(after) != len(before) {
		t.Fatalf("nota in plus pentru un task in coada: %+v", after)
	}
	if evs, _ := f.st.ListTaskEvents(queued.ID); evs[len(evs)-1].Kind != "user_files" {
		t.Fatalf("evenimentul lipseste: %+v", evs)
	}

	// un task sters nu primeste fisiere
	if err := f.st.ArchiveTasks([]int64{queued.ID}, queued.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.tm.AddUserFiles(queued.ID, stage(t, filepath.Join(root, "s3"), "d.txt")); !errors.Is(err, ErrArchived) {
		t.Fatalf("pe sters: %v", err)
	}
	if _, err := f.tm.AddUserFiles(9999, nil); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("task lipsa: %v", err)
	}
}
