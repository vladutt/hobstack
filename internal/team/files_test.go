package team

import (
	"path/filepath"
	"strings"
	"testing"

	"foreman/internal/store"
)

func TestFilesDirAndBlockInTurn(t *testing.T) {
	f := setup(t)
	root := t.TempDir()
	f.tm.SetFilesRoot(root)
	doc := store.Attachment{ProjectID: f.p.ID, Kind: store.KindProjectFile, Name: "spec.pdf", Stored: "aa-spec.pdf", Size: 1, Mime: "application/pdf"}
	f.st.AddAttachment(&doc)
	f.task(t, "A", f.rook, store.StatusQueued)
	mustReconcile(t, f)
	spec := f.fs.last(t, f.rook)
	dir := filepath.Join(root, "1", "files")
	if argValue(spec.Args, "--add-dir") != dir {
		t.Fatalf("--add-dir = %q, vreau %q", argValue(spec.Args, "--add-dir"), dir)
	}
	want := "Project files (you can read them with Read):\n- " + filepath.Join(dir, "aa-spec.pdf") + " (spec.pdf)"
	if !strings.Contains(turn(spec), want) {
		t.Fatalf("tura nu contine blocul de fisiere:\n%s", turn(spec))
	}
}

func TestMessageAttachmentsInTurn(t *testing.T) {
	f := setup(t)
	root := t.TempDir()
	f.tm.SetFilesRoot(root)
	img := store.Attachment{ProjectID: f.p.ID, Kind: store.KindMessageFile, Name: "bug.png", Stored: "bb-bug.png", Size: 1, Mime: "image/png"}
	f.st.AddAttachment(&img)
	m, err := f.tm.UserMessage(f.lead.ID, "Vezi bug-ul", []int64{img.ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Attachments) != 1 {
		t.Fatalf("mesajul intors trebuie sa aiba atasamentul: %+v", m)
	}
	mustReconcile(t, f)
	want := "User: Vezi bug-ul\nAttached: " + filepath.Join(root, "1", "files", "bb-bug.png") + " (bug.png)"
	if got := turn(f.fs.last(t, f.lead)); !strings.Contains(got, want) {
		t.Fatalf("tura lead-ului:\n%s", got)
	}
}

func TestNoFilesRootNoAddDir(t *testing.T) {
	f := setup(t)
	f.task(t, "A", f.rook, store.StatusQueued)
	mustReconcile(t, f)
	if hasFlag(f.fs.last(t, f.rook).Args, "--add-dir") {
		t.Fatal("fara SetFilesRoot nu trebuie --add-dir")
	}
}

func TestWorkerMessageAttachmentsReachLead(t *testing.T) {
	f := setup(t)
	root := t.TempDir()
	f.tm.SetFilesRoot(root)
	img := store.Attachment{ProjectID: f.p.ID, Kind: store.KindMessageFile, Name: "bug.png", Stored: "bb-bug.png", Size: 1, Mime: "image/png"}
	f.st.AddAttachment(&img)
	if _, err := f.tm.UserMessage(f.rook.ID, "vezi", []int64{img.ID}); err != nil {
		t.Fatal(err)
	}
	mustReconcile(t, f)
	got := turn(f.fs.last(t, f.lead))
	want := "User -> Rook: vezi\nAttached: " + filepath.Join(root, "1", "files", "bb-bug.png") + " (bug.png)"
	if !strings.Contains(got, want) {
		t.Fatalf("tura lead-ului:\n%s", got)
	}
}
