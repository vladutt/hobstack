package team

import (
	"strings"
	"testing"

	"foreman/internal/store"
)

func TestLeaveTickAndComment(t *testing.T) {
	f := setup(t)
	task := f.task(t, "Terms page", f.rook, store.StatusInProgress)
	c := f.workerCaller(f.rook, task.ID)
	if _, err := f.tm.ReportDone(c, "", "page added"); err != nil {
		t.Fatal(err)
	}
	text, err := f.tm.LeaveForUser(c, "", []string{"Company name", "Registration number", "Contact e-mail"})
	if err != nil || !strings.Contains(text, "3 items") {
		t.Fatalf("%q %v", text, err)
	}
	if _, err := f.tm.TickLeftForUser(c, task.Ref(), []int{1, 7}); err == nil {
		t.Fatal("punct inexistent bifat")
	}
	if l, _ := f.st.ListTodos(task.ID); l[0].DoneAt != nil {
		t.Fatal("o eroare nu bifeaza nimic")
	}
	if _, err := f.tm.TickLeftForUser(c, task.Ref(), []int{1}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.tm.CommentTask(c, task.Ref(), "Company name is in."); err != nil {
		t.Fatal(err)
	}
	events, _ := f.st.ListTaskEvents(task.ID)
	last := events[len(events)-1]
	if last.Kind != "comment" || last.Body != "Company name is in." {
		t.Fatalf("%+v", last)
	}
	// Vera nu e asignata: nu poate bifa
	if _, err := f.tm.TickLeftForUser(f.workerCaller(f.vera, 0), task.Ref(), []int{2}); err == nil {
		t.Fatal("un agent strain a bifat")
	}
}

// dupa report_done intr-o etapa, task-ul trece la alt asignat; cel care a raportat poate lasa puncte in continuare
func TestLeaveForUserAfterStageHandoff(t *testing.T) {
	f := setup(t)
	task := f.task(t, "Terms page", f.rook, store.StatusInProgress)
	task.AssigneeID = f.vera.ID // simuleaza mutarea in etapa urmatoare
	if err := f.st.SaveTask(&task); err != nil {
		t.Fatal(err)
	}
	f.tm.taskMsg(task, "reported", f.rook.Name, "", "Rook reported it done", "x")
	if _, err := f.tm.LeaveForUser(f.workerCaller(f.rook, task.ID), "", []string{"VAT"}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.tm.LeaveForUser(f.workerCaller(f.lead, 0), task.Ref(), []string{"VAT2"}); err == nil {
		t.Fatal("un strain a lasat puncte")
	}
}

func TestUserCommentWakesAssignee(t *testing.T) {
	f := setup(t)
	task := f.task(t, "Terms page", f.rook, store.StatusDone)
	if err := f.tm.UserComment(task.ID, "Company is Meridian Auto SRL."); err != nil {
		t.Fatal(err)
	}
	pending, _ := f.st.Undelivered(f.rook.ID)
	if len(pending) == 0 || !strings.Contains(pending[len(pending)-1].Text, task.Ref()) {
		t.Fatalf("mesajul nu a ajuns la Rook: %+v", pending)
	}
	// task Done cu puncte: bifatul nu ii schimba statusul
	f.st.AddTodos(task.ID, []string{"VAT"})
	if err := f.tm.UserSetTodo(task.ID, 1, true); err != nil {
		t.Fatal(err)
	}
	f.wantStatus(t, task.Number, store.StatusDone)
	if err := f.tm.UserComment(task.ID, "   "); err == nil {
		t.Fatal("comentariu gol acceptat")
	}
}
