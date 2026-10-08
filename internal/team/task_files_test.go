package team

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"foreman/internal/config"
	"foreman/internal/store"
	"foreman/internal/taskfiles"
)

// fisierele proiectului (projects/<pid>/files, pagina Files) sunt si ele un loc pentru livrabile:
// agentii le primesc prin --add-dir, iar calea se arata relativ la folder
func TestAttachProjectFiles(t *testing.T) {
	f := setup(t)
	root := t.TempDir()
	f.tm.SetFilesRoot(root)
	files := config.ProjectFilesDir(root, f.p.ID)
	if err := os.MkdirAll(filepath.Join(files, "research"), 0o755); err != nil {
		t.Fatal(err)
	}
	csv := filepath.Join(files, "research", "leads.csv")
	if err := os.WriteFile(csv, []byte("name,email\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	task := f.task(t, "Find leads", f.rook, store.StatusInProgress)
	c := f.workerCaller(f.rook, task.ID)
	if _, err := f.tm.ReportDone(c, "", "leads found"); err != nil {
		t.Fatal(err)
	}
	text, err := f.tm.AttachFiles(c, "", []string{csv})
	if err != nil || !strings.Contains(text, "Attached 1 file.") {
		t.Fatalf("%q %v", text, err)
	}
	list, _ := f.st.ListTaskFiles(task.ID)
	if len(list) != 1 || list[0].Path != filepath.Join("research", "leads.csv") {
		t.Fatalf("%+v", list)
	}
}

func TestAttachFiles(t *testing.T) {
	f := setup(t)
	root := t.TempDir()
	f.tm.SetFilesRoot(root)
	p, err := f.st.GetProject(f.p.ID)
	if err != nil {
		t.Fatal(err)
	}
	home := p.Folders[0].Path
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
	// copierea (pana la 20 x 50 MB) nu tine lock-ul global: ceilalti agenti nu asteapta dupa disc
	lockFree := false
	copyTaskFile = func(roots []string, home, raw, dest string) (taskfiles.Copied, error) {
		if f.tm.work.TryLock() {
			lockFree = true
			f.tm.work.Unlock()
		}
		return taskfiles.Copy(roots, home, raw, dest)
	}
	t.Cleanup(func() { copyTaskFile = taskfiles.Copy })
	if _, err := f.tm.AttachFiles(c, "", []string{"report.md"}); err != nil {
		t.Fatal(err)
	}
	if !lockFree {
		t.Fatal("t.work era blocat in timpul copierii")
	}
	// un agent strain nu poate atasa
	if _, err := f.tm.AttachFiles(f.workerCaller(f.vera, 0), task.Ref(), []string{"report.md"}); err == nil {
		t.Fatal("agent strain a atasat")
	}
}
