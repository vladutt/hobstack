package team

import (
	"strings"
	"testing"

	"foreman/internal/store"
)

func TestTaskEventTruncatesText(t *testing.T) {
	f := setup(t)
	task := store.Task{ProjectID: f.p.ID, Title: "t", AssigneeID: f.rook.ID}
	if err := f.st.CreateTask(&task); err != nil {
		t.Fatal(err)
	}
	f.tm.taskEvent(task, "rejected", "Vera", strings.Repeat("ă", 300))
	f.tm.taskEvent(task, "created", "You", "short")
	evs, err := f.st.ListTaskEvents(task.ID)
	if err != nil || len(evs) != 2 {
		t.Fatalf("%+v %v", evs, err)
	}
	if r := []rune(evs[0].Text); len(r) != 203 || !strings.HasSuffix(evs[0].Text, "...") {
		t.Fatalf("lungime %d", len(r))
	}
	if evs[1].Text != "short" || evs[1].Actor != "You" || evs[1].Kind != "created" {
		t.Fatalf("%+v", evs[1])
	}
}
