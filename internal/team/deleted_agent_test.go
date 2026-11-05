package team

import (
	"testing"

	"hobstack/internal/store"
)

// un task gata al unui agent sters, redeschis: Blocked, pana il dai cuiva
func TestReopenTaskOfDeletedAgent(t *testing.T) {
	f := setup(t)
	task := f.task(t, "Old work", f.vera, store.StatusDone)
	if err := f.st.DeleteAgent(f.vera.ID); err != nil {
		t.Fatal(err)
	}
	if err := f.tm.MoveTask(task.ID, "queued", "do it again"); err != nil {
		t.Fatal(err)
	}
	got, _ := f.st.GetTask(task.ID)
	if got.Status != store.StatusBlocked {
		t.Fatalf("status = %s, want blocked", got.Status)
	}
	if err := f.tm.AssignTask(task.ID, f.rook.ID); err != nil {
		t.Fatal(err)
	}
	if got, _ := f.st.GetTask(task.ID); got.Status != store.StatusQueued || got.AssigneeID != f.rook.ID {
		t.Fatalf("after assign: %+v", got)
	}
}
