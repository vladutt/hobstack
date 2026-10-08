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
	// un agent strain nu poate atasa
	if _, err := f.tm.AttachFiles(f.workerCaller(f.vera, 0), task.Ref(), []string{"report.md"}); err == nil {
		t.Fatal("agent strain a atasat")
	}
}
