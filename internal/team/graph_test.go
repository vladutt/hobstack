package team

import (
	"strings"
	"testing"

	"foreman/internal/store"
)

func TestSubLeadTurnShowsDirectTeamAndSubtasks(t *testing.T) {
	f := setup(t)
	sub := f.agent(t, "Sub", &f.lead, nil)
	wes := f.agent(t, "Wes", &sub, nil)
	parent := f.task(t, "Import", sub, store.StatusQueued)
	child := store.Task{ProjectID: f.p.ID, Title: "Lexer", AssigneeID: wes.ID, CreatorID: &sub.ID, ParentTaskID: &parent.ID}
	f.st.CreateTask(&child)
	child.Status = store.StatusDone
	f.st.SaveTask(&child)

	mustReconcile(t, f)
	spec := f.fs.last(t, sub)
	got := turn(spec)
	if !strings.HasPrefix(got, "Echipa ta directa:\n- Wes") || strings.Contains(got, "Rook") {
		t.Fatalf("echipa directa gresita:\n%s", got)
	}
	if !strings.Contains(got, "Sub-task-uri deja create pentru acest task (nu le crea din nou):\n- T-2 [done] Lexer -> Wes") {
		t.Fatalf("lipsesc sub-task-urile:\n%s", got)
	}
	if !strings.Contains(argValue(spec.Args, "--append-system-prompt"), "Raportezi lui Lead.") {
		t.Fatal("sub-liderul trebuie sa raporteze lui Lead")
	}
}
