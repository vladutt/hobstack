package team

import (
	"errors"
	"testing"

	"hobstack/internal/runner"
	"hobstack/internal/store"
)

// Stop pe un task in lucru: run-ul se opreste, task-ul asteapta in Queued (oprit) si agentul trece la
// urmatorul task; Resume il pune iar in lucru.
func TestStopTaskWaitsForResume(t *testing.T) {
	f := setup(t)
	first := f.task(t, "Build page", f.rook, store.StatusQueued)
	second := f.task(t, "Write docs", f.rook, store.StatusQueued)
	mustReconcile(t, f)
	run := f.fs.last(t, f.rook)

	if err := f.tm.StopTask(first.ID); err != nil {
		t.Fatal(err)
	}
	if len(f.fs.stopped) != 1 || f.fs.stopped[0] != run.ID {
		t.Fatalf("run neoprit: %v", f.fs.stopped)
	}
	f.fs.finish(run.ID, runner.Outcome{Status: store.RunStopped})
	got, _ := f.st.GetTask(first.ID)
	if got.Status != store.StatusQueued || !got.Stopped {
		t.Fatalf("dupa stop: %s stopped=%v", got.Status, got.Stopped)
	}
	if !f.hasMessage(f.rook.ID, "The user stopped your work on "+first.Ref()) {
		t.Fatal("agentul nu a aflat")
	}

	// agentul ia urmatorul task, nu pe cel oprit
	mustReconcile(t, f)
	if got, _ := f.st.GetTask(second.ID); got.Status != store.StatusInProgress {
		t.Fatalf("al doilea task: %s", got.Status)
	}
	if got, _ := f.st.GetTask(first.ID); got.Status != store.StatusQueued {
		t.Fatalf("task-ul oprit a pornit: %s", got.Status)
	}
	f.fs.finish(f.fs.last(t, f.rook).ID, runner.Outcome{Status: store.RunStopped})
	if err := f.tm.StopTask(second.ID); err != nil { // in coada, nepornit: doar oprit
		t.Fatal(err)
	}
	n := f.fs.count()
	mustReconcile(t, f)
	if f.fs.count() != n {
		t.Fatal("un task oprit a pornit un run")
	}

	if err := f.tm.ResumeTask(first.ID); err != nil {
		t.Fatal(err)
	}
	mustReconcile(t, f)
	if got, _ := f.st.GetTask(first.ID); got.Status != store.StatusInProgress || got.Stopped {
		t.Fatalf("dupa resume: %s stopped=%v", got.Status, got.Stopped)
	}
	// tras pe Queued, un task oprit se reia
	if err := f.tm.MoveTask(second.ID, "queued", ""); err != nil {
		t.Fatal(err)
	}
	if got, _ := f.st.GetTask(second.ID); got.Stopped {
		t.Fatal("mutarea pe Queued nu l-a reluat")
	}
	if err := f.tm.ResumeTask(second.ID); !errors.As(err, new(StopError)) {
		t.Fatalf("resume pe un task neoprit: %v", err)
	}
}

// Stop pe agent: opreste run-ul de chat fara sa puna echipa pe pauza; pe un agent liber e refuzat.
func TestStopAgentChat(t *testing.T) {
	f := setup(t)
	if err := f.tm.StopAgent(f.rook.ID); !errors.As(err, new(StopError)) {
		t.Fatalf("agent liber: %v", err)
	}
	if _, err := f.tm.UserMessage(f.rook.ID, "how is it going?", nil); err != nil {
		t.Fatal(err)
	}
	mustReconcile(t, f)
	run := f.fs.last(t, f.rook)
	if err := f.tm.StopAgent(f.rook.ID); err != nil {
		t.Fatal(err)
	}
	if len(f.fs.stopped) != 1 || f.fs.stopped[0] != run.ID {
		t.Fatalf("run neoprit: %v", f.fs.stopped)
	}
	f.fs.finish(run.ID, runner.Outcome{Status: store.RunStopped})
	if !f.hasMessage(f.rook.ID, "Stopped by the user.") {
		t.Fatal("lipseste randul din chat")
	}
	if p, _ := f.st.GetProject(f.p.ID); p.Paused {
		t.Fatal("echipa a fost pusa pe pauza")
	}
}
