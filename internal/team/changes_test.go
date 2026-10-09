package team

import (
	"path/filepath"
	"testing"

	"hobstack/internal/runner"
	"hobstack/internal/store"
)

func hunk(l ...string) []runner.Hunk {
	return []runner.Hunk{{OldStart: 1, OldLines: 1, NewStart: 1, NewLines: 1, Lines: l}}
}

func TestRunChangesSavedRelative(t *testing.T) {
	f := setup(t)
	task := f.task(t, "A", f.rook, store.StatusQueued)
	mustReconcile(t, f)
	dir := f.p.Workdir
	outside := filepath.Join(filepath.Dir(dir), "elsewhere", "o.txt")
	f.fs.finish(f.fs.last(t, f.rook).ID, runner.Outcome{
		Status: store.RunDone,
		Changes: []runner.FileChange{
			{Tool: "Edit", Path: filepath.Join(dir, "internal", "a.go"), Kind: "edit", Patch: hunk("-a", "+b")},
			{Tool: "Edit", Path: filepath.Join(dir, "internal", "a.go"), Kind: "edit", Patch: hunk("-c", "+d"), Truncated: true},
			{Tool: "Write", Path: outside, Kind: "create"},
		},
		Commands: []runner.Command{{Text: "go test ./...", OK: false}},
	})
	cs, err := f.st.ListTaskChanges(task.ID)
	if err != nil || len(cs) != 4 {
		t.Fatalf("%+v %v", cs, err)
	}
	if cs[0].Path != filepath.Join("internal", "a.go") || cs[0].Kind != "edit" || cs[0].Tool != "Edit" ||
		cs[0].AgentName != "Rook" || cs[0].AgentID == nil || *cs[0].AgentID != f.rook.ID || cs[0].RunID == "" ||
		len(cs[0].Patch) != 1 || cs[0].Patch[0].Lines[1] != "+b" {
		t.Fatalf("%+v", cs[0])
	}
	if cs[1].Path != filepath.Join("internal", "a.go") || !cs[1].Truncated {
		t.Fatalf("%+v", cs[1])
	}
	if cs[2].Path != outside || cs[2].Kind != "create" {
		t.Fatalf("calea din afara folderului ramane absoluta: %+v", cs[2])
	}
	if cs[3].Kind != "command" || cs[3].Tool != "Bash" || cs[3].Path != "" || cs[3].Command != "go test ./..." || cs[3].OK {
		t.Fatalf("%+v", cs[3])
	}
}

func TestRunChangesFailedRunKept(t *testing.T) {
	f := setup(t)
	task := f.task(t, "A", f.rook, store.StatusQueued)
	mustReconcile(t, f)
	f.fs.finish(f.fs.last(t, f.rook).ID, runner.Outcome{Status: store.RunFailed, Error: "x",
		Changes: []runner.FileChange{{Tool: "Write", Path: filepath.Join(f.p.Workdir, "n.txt"), Kind: "create"}}})
	cs, _ := f.st.ListTaskChanges(task.ID)
	if len(cs) != 1 || cs[0].Path != "n.txt" {
		t.Fatalf("%+v", cs)
	}
}

func TestRunWithoutTaskSavesNothing(t *testing.T) {
	f := setup(t)
	if _, err := f.tm.UserMessage(f.lead.ID, "hi", nil); err != nil {
		t.Fatal(err)
	}
	mustReconcile(t, f)
	f.fs.finish(f.fs.last(t, f.lead).ID, runner.Outcome{Status: store.RunDone,
		Changes: []runner.FileChange{{Tool: "Write", Path: filepath.Join(f.p.Workdir, "n.txt"), Kind: "create"}}})
	counts, _ := f.st.CountChangedFiles(f.p.ID)
	if len(counts) != 0 {
		t.Fatalf("%v", counts)
	}
}

func TestRunChangesPrefixedWithFolderNameWhenMultiFolder(t *testing.T) {
	f := setup(t)
	_, landing := withFolders(t, f)
	task := f.task(t, "A", f.rook, store.StatusQueued)
	mustReconcile(t, f)
	f.fs.finish(f.fs.last(t, f.rook).ID, runner.Outcome{Status: store.RunDone,
		Changes: []runner.FileChange{{Tool: "Write", Path: filepath.Join(landing.Path, "src", "package.json"), Kind: "create"}}})
	cs, _ := f.st.ListTaskChanges(task.ID)
	if len(cs) != 1 || cs[0].Path != "Landing/src/package.json" {
		t.Fatalf("%+v", cs)
	}
}
