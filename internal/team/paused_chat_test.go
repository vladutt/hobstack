package team

import (
	"slices"
	"testing"

	"hobstack/internal/runner"
	"hobstack/internal/store"
)

func pause(t *testing.T, f *fixture) {
	t.Helper()
	if err := f.st.SetPaused(f.p.ID, true); err != nil {
		t.Fatal(err)
	}
}

// pe pauza, un mesaj al utilizatorului catre orice agent (si un worker) porneste un run
func TestPausedChatStartsRun(t *testing.T) {
	f := setup(t)
	pause(t, f)
	if _, err := f.tm.UserMessage(f.rook.ID, "how is the import going?", nil); err != nil {
		t.Fatal(err)
	}
	mustReconcile(t, f)
	if f.fs.count() != 1 {
		t.Fatalf("vreau un run de chat, am %d", f.fs.count())
	}
	run := f.fs.last(t, f.rook)
	if msgs, _ := f.st.Undelivered(f.rook.ID); len(msgs) != 0 {
		t.Fatalf("mesajul trebuia livrat: %+v", msgs)
	}
	f.fs.finish(run.ID, runner.Outcome{Status: "done"})
}

// pe pauza, notele de la agenti nu trezesc pe nimeni
func TestPausedNotesDoNothing(t *testing.T) {
	f := setup(t)
	pause(t, f)
	if err := f.tm.note(f.p.ID, f.lead.ID, "Rook finished T-9", false); err != nil {
		t.Fatal(err)
	}
	mustReconcile(t, f)
	if f.fs.count() != 0 {
		t.Fatalf("pe pauza notele nu pornesc run-uri: %d", f.fs.count())
	}
}

// pe pauza, task-urile din Queued asteapta Start team, chiar daca asignatul primeste un mesaj
func TestPausedQueuedTaskWaits(t *testing.T) {
	f := setup(t)
	pause(t, f)
	f.task(t, "A", f.rook, store.StatusQueued)
	if _, err := f.tm.UserMessage(f.rook.ID, "hi", nil); err != nil {
		t.Fatal(err)
	}
	mustReconcile(t, f)
	f.wantStatus(t, 1, store.StatusQueued)
	run := f.fs.last(t, f.rook)
	if c := f.caller(t, run); c.TaskID != 0 {
		t.Fatalf("pe pauza run-ul e de chat, nu pe task: %+v", c)
	}
	f.fs.finish(run.ID, runner.Outcome{Status: "done"})
}

// un run de chat pe pauza poate crea task-uri; ele raman in Queued, fara run pentru worker
func TestPausedChatCreatesQueuedTask(t *testing.T) {
	f := setup(t)
	pause(t, f)
	if _, err := f.tm.UserMessage(f.lead.ID, "plan the import", nil); err != nil {
		t.Fatal(err)
	}
	mustReconcile(t, f)
	run := f.fs.last(t, f.lead)
	if _, err := f.tm.CreateTask(f.caller(t, run), "Import", "", "Rook", "", ""); err != nil {
		t.Fatal(err)
	}
	mustReconcile(t, f)
	if f.fs.count() != 1 {
		t.Fatalf("worker-ul nu porneste pe pauza: %d run-uri", f.fs.count())
	}
	f.wantStatus(t, 1, store.StatusQueued)
	f.fs.finish(run.ID, runner.Outcome{Status: "done"})
}

// Stop opreste run-urile pe task-uri, dar lasa o conversatie cu utilizatorul sa se termine
func TestStopSparesChatRun(t *testing.T) {
	f := setup(t)
	f.task(t, "A", f.rook, store.StatusQueued)
	if _, err := f.tm.UserMessage(f.lead.ID, "status?", nil); err != nil {
		t.Fatal(err)
	}
	mustReconcile(t, f)
	work := f.fs.last(t, f.rook)
	chat := f.fs.last(t, f.lead)
	if err := f.tm.Stop(f.p.ID); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(f.fs.stopped, work.ID) {
		t.Fatalf("run-ul pe task trebuia oprit: %v", f.fs.stopped)
	}
	if slices.Contains(f.fs.stopped, chat.ID) {
		t.Fatalf("conversatia nu se opreste: %v", f.fs.stopped)
	}
	f.wantStatus(t, 1, store.StatusQueued)
	f.fs.finish(chat.ID, runner.Outcome{Status: "done"})
}
