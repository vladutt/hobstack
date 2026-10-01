package team

import "testing"

func TestWorkingOn(t *testing.T) {
	f := setup(t)
	if _, ok := f.tm.WorkingOn(5); ok {
		t.Fatal("nimeni nu lucreaza")
	}
	f.tm.mu.Lock()
	f.tm.active[f.rook.ID] = activeRun{runID: "r1", taskID: 5}
	f.tm.active[f.vera.ID] = activeRun{runID: "r2"} // run fara task
	f.tm.mu.Unlock()
	if id, ok := f.tm.WorkingOn(5); !ok || id != f.rook.ID {
		t.Fatalf("%d %v", id, ok)
	}
	if _, ok := f.tm.WorkingOn(0); ok {
		t.Fatal("task 0 nu exista")
	}
	if _, ok := f.tm.WorkingOn(6); ok {
		t.Fatal("alt task")
	}
}
