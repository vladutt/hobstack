package team

import (
	"errors"
	"fmt"
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
		stored := fmt.Sprintf("%02d%s-%s", i, filepath.Base(dir), n)
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
	// cel mai nou primul: brief.txt a intrat primul
	if err != nil || len(list) != 2 || list[1].Name != "brief.txt" || list[1].Path != "brief.txt" || !list[1].FromUser() {
		t.Fatalf("%+v %v", list, err)
	}
	brief := list[1]
	if _, err := os.Stat(filepath.Join(dest, brief.Stored)); err != nil {
		t.Fatalf("copia nu e in task-files: %v", err)
	}
	if _, err := os.Stat(filepath.Join(staging, files[0].Stored)); !os.IsNotExist(err) {
		t.Fatalf("staging-ul trebuia mutat: %v", err)
	}

	mustReconcile(t, f)
	spec := f.fs.last(t, f.rook)
	tr := turn(spec)
	if !strings.Contains(tr, "Files from the user (read them with Read):\n- "+filepath.Join(dest, list[0].Stored)+" (colors.txt)\n- "+
		filepath.Join(dest, brief.Stored)+" (brief.txt)") {
		t.Fatalf("tura fara fisierele utilizatorului:\n%s", tr)
	}
	if parent := config.ProjectTaskFilesDir(root, f.p.ID); !hasAddDir(spec.Args, parent) {
		t.Fatalf("lipseste --add-dir %s: %v", parent, spec.Args)
	}
}

// un task fara fisiere: fara bloc in tura si fara --add-dir pe folderul task-ului (doar parintele task-files)
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

// kicked: Kick a fost chemat pentru proiect (si nimeni nu a consumat inca semnalul)
func (f *fixture) kicked() bool {
	f.tm.mu.Lock()
	defer f.tm.mu.Unlock()
	return f.tm.pending[f.p.ID]
}

func (f *fixture) clearKick() {
	f.tm.mu.Lock()
	defer f.tm.mu.Unlock()
	delete(f.tm.pending, f.p.ID)
}

func undeliveredWith(t *testing.T, f *fixture, a store.Agent, text string) bool {
	t.Helper()
	und, _ := f.st.Undelivered(a.ID)
	for _, m := range und {
		if strings.Contains(m.Text, text) {
			return true
		}
	}
	return false
}

func TestAddUserFiles(t *testing.T) {
	f := setup(t)
	root := t.TempDir()
	f.tm.SetFilesRoot(root)
	working := f.task(t, "A", f.rook, store.StatusInProgress)
	queued := f.task(t, "B", f.vera, store.StatusQueued)

	f.clearKick()
	saved, err := f.tm.AddUserFiles(working.ID, stage(t, filepath.Join(root, "s1"), "a.txt", "b.txt"))
	if err != nil || len(saved) != 2 || !saved[0].FromUser() {
		t.Fatalf("%+v %v", saved, err)
	}
	dest := config.TaskFilesDir(root, f.p.ID, working.ID)
	pathA := "- " + filepath.Join(dest, saved[0].Stored) + " (a.txt)"
	// nota are caile absolute, ca agentul sa le poata citi si dintr-un run fara task
	if !undeliveredWith(t, f, f.rook, "The user attached 2 files to T-1:\n"+pathA) {
		und, _ := f.st.Undelivered(f.rook.ID)
		t.Fatalf("nota pentru Rook lipseste: %+v", und)
	}
	if !f.kicked() {
		t.Fatal("un task in lucru trebuia sa trezeasca echipa")
	}
	evs, _ := f.st.ListTaskEvents(working.ID)
	if last := evs[len(evs)-1]; last.Kind != "user_files" || last.Actor != "You" || !strings.Contains(last.Body, pathA) {
		t.Fatalf("eveniment: %+v", last)
	}

	// un task in coada nu are nevoie de nota: fisierele intra in tura lui
	f.clearKick()
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

// blocat: nota + trezire; review / done: doar evenimentul (fara nota, fara trezire)
func TestAddUserFilesByStatus(t *testing.T) {
	for _, tc := range []struct {
		status string
		note   bool
	}{
		{store.StatusBlocked, true},
		{store.StatusReview, false},
		{store.StatusDone, false},
		{store.StatusQueued, false},
	} {
		t.Run(tc.status, func(t *testing.T) {
			f := setup(t)
			root := t.TempDir()
			f.tm.SetFilesRoot(root)
			task := f.task(t, "A", f.rook, tc.status)
			f.clearKick()
			before, _ := f.st.Undelivered(f.rook.ID)
			saved, err := f.tm.AddUserFiles(task.ID, stage(t, filepath.Join(root, "s"), "spec.pdf"))
			if err != nil {
				t.Fatal(err)
			}
			after, _ := f.st.Undelivered(f.rook.ID)
			if got := len(after) > len(before); got != tc.note {
				t.Fatalf("nota: %v, vreau %v (%+v)", got, tc.note, after)
			}
			if f.kicked() != tc.note {
				t.Fatalf("kick: %v, vreau %v", f.kicked(), tc.note)
			}
			path := filepath.Join(config.TaskFilesDir(root, f.p.ID, task.ID), saved[0].Stored)
			evs, _ := f.st.ListTaskEvents(task.ID)
			if last := evs[len(evs)-1]; last.Kind != "user_files" || !strings.Contains(last.Body, path) {
				t.Fatalf("eveniment: %+v", last)
			}
		})
	}
}

// orice run al proiectului (si chat-ul / notificarile) poate citi task-files/
func TestTaskFilesDirOnEveryRun(t *testing.T) {
	f := setup(t)
	root := t.TempDir()
	f.tm.SetFilesRoot(root)
	if _, err := f.tm.UserMessage(f.lead.ID, "hello", nil); err != nil {
		t.Fatal(err)
	}
	mustReconcile(t, f)
	spec := f.fs.last(t, f.lead)
	dir := config.ProjectTaskFilesDir(root, f.p.ID)
	if !hasAddDir(spec.Args, dir) {
		t.Fatalf("lipseste --add-dir %s: %v", dir, spec.Args)
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		t.Fatalf("folderul trebuia creat: %v", err)
	}
}

// un follow-up vede si fisierele utilizatorului de pe task-ul continuat si de pe parinte
func TestFollowUpTurnListsContinuedFiles(t *testing.T) {
	f := setup(t)
	root := t.TempDir()
	f.tm.SetFilesRoot(root)
	origin := f.task(t, "Origin", f.lead, store.StatusDone)
	parent := f.task(t, "Parent", f.lead, store.StatusInProgress)
	fo, err := f.tm.AddUserFiles(origin.ID, stage(t, filepath.Join(root, "s1"), "origin.txt"))
	if err != nil {
		t.Fatal(err)
	}
	fp, err := f.tm.AddUserFiles(parent.ID, stage(t, filepath.Join(root, "s2"), "parent.txt"))
	if err != nil {
		t.Fatal(err)
	}
	follow := store.Task{ProjectID: f.p.ID, Title: "Follow", AssigneeID: f.rook.ID, CreatorID: &f.lead.ID,
		ThreadID: &origin.ID, ParentTaskID: &parent.ID}
	if err := f.st.CreateTask(&follow); err != nil {
		t.Fatal(err)
	}
	own, err := f.tm.AddUserFiles(follow.ID, stage(t, filepath.Join(root, "s3"), "own.txt"))
	if err != nil {
		t.Fatal(err)
	}
	mustReconcile(t, f)
	tr := turn(f.fs.last(t, f.rook))
	line := func(task store.Task, tf store.TaskFile) string {
		return "- " + filepath.Join(config.TaskFilesDir(root, f.p.ID, task.ID), tf.Stored) + " (" + tf.Name + ")"
	}
	want := "Files from the user (read them with Read):\n" + line(follow, own[0]) + "\n" + line(origin, fo[0]) + "\n" + line(parent, fp[0])
	if !strings.Contains(tr, want) {
		t.Fatalf("tura follow-up:\n%s\nvreau\n%s", tr, want)
	}

	// acelasi task ca fir si parinte: o singura data
	follow2 := store.Task{ProjectID: f.p.ID, Title: "Follow2", AssigneeID: f.vera.ID, CreatorID: &f.lead.ID,
		ThreadID: &origin.ID, ParentTaskID: &origin.ID}
	if err := f.st.CreateTask(&follow2); err != nil {
		t.Fatal(err)
	}
	mustReconcile(t, f)
	if tr := turn(f.fs.last(t, f.vera)); strings.Count(tr, "(origin.txt)") != 1 {
		t.Fatalf("duplicat:\n%s", tr)
	}
}

// cel mult 50 de fisiere ale utilizatorului pe task
func TestUserFilesCap(t *testing.T) {
	f := setup(t)
	root := t.TempDir()
	f.tm.SetFilesRoot(root)
	task := f.task(t, "A", f.rook, store.StatusQueued)
	names := make([]string, 49)
	for i := range names {
		names[i] = fmt.Sprintf("f%d.txt", i)
	}
	if _, err := f.tm.AddUserFiles(task.ID, stage(t, filepath.Join(root, "s1"), names...)); err != nil {
		t.Fatal(err)
	}
	var tm TooManyFilesError
	if _, err := f.tm.AddUserFiles(task.ID, stage(t, filepath.Join(root, "s2"), "x.txt", "y.txt")); !errors.As(err, &tm) ||
		!strings.Contains(err.Error(), "at most 50") {
		t.Fatalf("51 de fisiere: %v", err)
	}
	if _, err := f.tm.AddUserFiles(task.ID, stage(t, filepath.Join(root, "s3"), "z.txt")); err != nil {
		t.Fatalf("al 50-lea: %v", err)
	}
	many := make([]string, 51)
	for i := range many {
		many[i] = fmt.Sprintf("m%d.txt", i)
	}
	if _, err := f.tm.UserCreateTaskWithFiles(f.p.ID, "B", "", f.rook.ID, stage(t, filepath.Join(root, "s4"), many...)); !errors.As(err, &tm) {
		t.Fatalf("creare cu 51: %v", err)
	}
}

func TestReservedAgentName(t *testing.T) {
	for _, n := range []string{"You", "you", " YOU "} {
		a := store.Agent{Name: n, Model: "sonnet"}
		if err := CheckAgentFields(&a); err == nil || !strings.Contains(err.Error(), `"You" is reserved for the user`) {
			t.Fatalf("%q: %v", n, err)
		}
	}
	a := store.Agent{Name: "Yourself", Model: "sonnet"}
	if err := CheckAgentFields(&a); err != nil {
		t.Fatal(err)
	}
}
