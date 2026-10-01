package team

import "testing"

func TestWorkingOn(t *testing.T) {
	f := setup(t)
	if f.tm.WorkingOn(5, f.rook.ID) {
		t.Fatal("nimeni nu lucreaza")
	}
	f.tm.mu.Lock()
	f.tm.active[f.rook.ID] = activeRun{runID: "r1", taskID: 5}
	f.tm.active[f.vera.ID] = activeRun{runID: "r2"} // run fara task
	f.tm.mu.Unlock()
	if !f.tm.WorkingOn(5, f.rook.ID) {
		t.Fatal("rook lucreaza pe 5")
	}
	if f.tm.WorkingOn(5, f.vera.ID) {
		t.Fatal("vera nu e asignata / nu lucreaza pe 5")
	}
	if f.tm.WorkingOn(0, f.rook.ID) {
		t.Fatal("task 0 nu exista")
	}
	if f.tm.WorkingOn(6, f.rook.ID) {
		t.Fatal("alt task")
	}
}
