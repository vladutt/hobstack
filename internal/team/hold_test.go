package team

import (
	"context"
	"testing"
	"time"

	"hobstack/internal/store"
)

// Hold (update-ul asteapta agentii): Reconcile nu porneste run-uri noi; la reluare proiectul se reconciliaza.
func TestHoldDefersNewRuns(t *testing.T) {
	f := setup(t)
	f.task(t, "Write the importer", f.rook, store.StatusQueued)
	f.tm.Hold(true)
	if err := f.tm.Reconcile(f.p.ID); err != nil {
		t.Fatal(err)
	}
	if n := f.fs.count(); n != 0 {
		t.Fatalf("held: %d runs started", n)
	}
	if f.tm.ActiveCount() != 0 {
		t.Fatal("no run should be active")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go f.tm.Run(ctx)
	f.tm.Hold(false)
	deadline := time.Now().Add(5 * time.Second)
	for f.fs.count() == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if f.fs.count() != 1 {
		t.Fatalf("after Hold(false): %d runs, want 1", f.fs.count())
	}
	if f.tm.ActiveCount() != 1 {
		t.Fatalf("ActiveCount = %d", f.tm.ActiveCount())
	}
	f.tm.Drain()
}
