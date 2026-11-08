package team

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"hobstack/internal/mcp"
	"hobstack/internal/runner"
	"hobstack/internal/store"
)

// la finalul unui run pe task, imaginile / SVG-urile facute in run ajung singure pe task, o singura data
func TestAttachVisualsAtRunEnd(t *testing.T) {
	f := setup(t)
	f.tm.SetFilesRoot(t.TempDir())
	p, _ := f.st.GetProject(f.p.ID)
	home := p.Folders[0].Path
	write := func(name, body string) string {
		path := filepath.Join(home, name)
		os.MkdirAll(filepath.Dir(path), 0o755)
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		return path
	}
	png := write("shots/home.png", "\x89PNG fake")
	svg := write("logo.svg", "<svg/>")
	txt := write("notes.txt", "hi")
	task := f.task(t, "Draw the logo", f.rook, store.StatusInProgress)
	o := runner.Outcome{RunID: "r1", Changes: []runner.FileChange{
		{Tool: "Write", Path: svg, Kind: "create"},
		{Tool: "Shell", Path: png, Kind: "create"},
		{Tool: "Write", Path: txt, Kind: "create"},
		{Tool: "Shell", Path: filepath.Join(home, "gone.png"), Kind: "delete"},
	}}
	run := func() {
		f.tm.mu.Lock()
		f.tm.active[f.rook.ID] = activeRun{runID: "r1", projectID: f.p.ID, taskID: task.ID}
		f.tm.mu.Unlock()
		f.tm.attachVisuals(f.rook.ID, o)
	}
	run()
	files, _ := f.st.ListTaskFiles(task.ID)
	if len(files) != 2 {
		t.Fatalf("%+v", files)
	}
	for _, tf := range files {
		if tf.Source != svg && tf.Source != png {
			t.Fatalf("source: %+v", tf)
		}
	}
	// acelasi run inca o data (sau attach_files pe aceleasi fisiere): fara dubluri
	run()
	if files, _ := f.st.ListTaskFiles(task.ID); len(files) != 2 {
		t.Fatalf("duplicates: %+v", files)
	}
	// fisierul schimbat intre timp intra ca versiune noua
	write("logo.svg", "<svg><circle/></svg>")
	run()
	if files, _ := f.st.ListTaskFiles(task.ID); len(files) != 3 {
		t.Fatalf("new version: %+v", files)
	}
}

// attach_files pe task: si cine nu e asignat (reviewer-ul) poate arata fisiere pe task-ul run-ului lui
func TestAttachFilesOnTheRunTask(t *testing.T) {
	f := setup(t)
	f.tm.SetFilesRoot(t.TempDir())
	p, _ := f.st.GetProject(f.p.ID)
	if err := os.WriteFile(filepath.Join(p.Folders[0].Path, "review.png"), []byte("img"), 0o644); err != nil {
		t.Fatal(err)
	}
	task := f.task(t, "Build page", f.rook, store.StatusReview)
	text, err := f.tm.AttachFiles(f.workerCaller(f.vera, task.ID), "", []string{"review.png"})
	if err != nil || !strings.Contains(text, "Attached 1 file.") {
		t.Fatalf("%q %v", text, err)
	}
}

// attach_files in chat: un mesaj al agentului cu fisierele ca atasamente
func TestAttachToChat(t *testing.T) {
	f := setup(t)
	f.tm.SetFilesRoot(t.TempDir())
	p, _ := f.st.GetProject(f.p.ID)
	shot := filepath.Join(p.Folders[0].Path, "mobile.png")
	if err := os.WriteFile(shot, []byte("img"), 0o644); err != nil {
		t.Fatal(err)
	}
	c := mcp.Caller{ProjectID: f.p.ID, AgentID: f.rook.ID}
	text, err := f.tm.AttachToChat(c, []string{"mobile.png", "/etc/hosts"}, "The page on mobile")
	if err != nil || !strings.Contains(text, "Attached 1 file") || !strings.Contains(text, "/etc/hosts") {
		t.Fatalf("%q %v", text, err)
	}
	msgs, _, err := f.st.ListChat(f.rook.ID)
	if err != nil {
		t.Fatal(err)
	}
	last := msgs[len(msgs)-1]
	if last.Author != "Rook" || last.Text != "The page on mobile" || len(last.Attachments) != 1 || last.Attachments[0].Source != shot {
		t.Fatalf("%+v", last)
	}
}
