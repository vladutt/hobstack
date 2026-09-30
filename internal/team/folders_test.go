package team

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"foreman/internal/store"
)

// withFolders adauga proiectului fixture-ului un al doilea folder si il muta pe Rook acolo.
func withFolders(t *testing.T, f *fixture) (client, landing store.Folder) {
	t.Helper()
	p, err := f.st.GetProject(f.p.ID)
	if err != nil {
		t.Fatal(err)
	}
	p.Folders[0].Name = "Client"
	p.Folders = append(p.Folders, store.Folder{Name: "Landing", Path: t.TempDir()})
	if err := f.st.UpdateProject(p); err != nil {
		t.Fatal(err)
	}
	p, _ = f.st.GetProject(f.p.ID)
	f.rook.FolderID = &p.Folders[1].ID
	if err := f.st.UpdateAgent(f.rook); err != nil {
		t.Fatal(err)
	}
	return p.Folders[0], p.Folders[1]
}

func TestRunInAgentFolder(t *testing.T) {
	f := setup(t)
	client, landing := withFolders(t, f)
	f.task(t, "Hero section", f.rook, store.StatusQueued)
	mustReconcile(t, f)
	spec := f.fs.last(t, f.rook)
	if spec.Dir != landing.Path {
		t.Fatalf("cwd = %q, vreau %q", spec.Dir, landing.Path)
	}
	if argValue(spec.Args, "--add-dir") != client.Path {
		t.Fatalf("--add-dir = %q, vreau %q", argValue(spec.Args, "--add-dir"), client.Path)
	}
	if s := argValue(spec.Args, "--settings"); !strings.Contains(s, "Write(/"+filepath.ToSlash(landing.Path)+"/**)") {
		t.Fatalf("scrisul trebuia restrans la Landing: %s", s)
	}
	if sys := argValue(spec.Args, "--append-system-prompt"); !strings.Contains(sys, "- Landing: "+landing.Path+" (your folder)") {
		t.Fatalf("system prompt:\n%s", sys)
	}
	if got := argValue(spec.Args, "--permission-mode"); got != "dontAsk" {
		t.Fatalf("--permission-mode = %q, vreau dontAsk", got)
	}
}

func TestLeadSeesMemberFolders(t *testing.T) {
	f := setup(t)
	withFolders(t, f)
	if _, err := f.tm.UserMessage(f.lead.ID, "Plan v2", nil); err != nil {
		t.Fatal(err)
	}
	mustReconcile(t, f)
	got := turn(f.fs.last(t, f.lead))
	if !strings.Contains(got, "- Rook: Backend Dev") || !strings.Contains(got, "folder: Landing") || !strings.Contains(got, "folder: Client") {
		t.Fatalf("tura lead-ului:\n%s", got)
	}
}

func TestRunFailsWhenFolderMissing(t *testing.T) {
	f := setup(t)
	_, landing := withFolders(t, f)
	os.RemoveAll(landing.Path)
	task := f.task(t, "Hero", f.rook, store.StatusQueued)
	f.tm.Reconcile(f.p.ID) // eroarea de pornire ajunge ca nota, nu ca panica
	msgs, _ := f.st.ListMessages(f.lead.ID)
	found := false
	for _, m := range msgs {
		if strings.Contains(m.Text, "Landing") && strings.Contains(m.Text, "does not exist") {
			found = true
		}
	}
	if !found {
		t.Fatalf("nicio nota cu folderul lipsa; task %s, mesaje: %+v", task.Ref(), msgs)
	}
}

// un alt folder sters nu strica run-ul; doar nu primeste --add-dir
func TestRunSkipsMissingOtherFolder(t *testing.T) {
	f := setup(t)
	client, _ := withFolders(t, f)
	os.RemoveAll(client.Path)
	f.task(t, "Hero section", f.rook, store.StatusQueued)
	mustReconcile(t, f)
	spec := f.fs.last(t, f.rook)
	for i, a := range spec.Args {
		if a == "--add-dir" && spec.Args[i+1] == client.Path {
			t.Fatalf("--add-dir pentru un folder sters: %v", spec.Args)
		}
	}
}
