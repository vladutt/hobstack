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
	f.st.AddTodos(task.ID, []string{"VAT"}, "value")
	if err := f.tm.UserSetTodo(task.ID, 1, true); err != nil {
		t.Fatal(err)
	}
	f.wantStatus(t, task.Number, store.StatusDone)
	if err := f.tm.UserComment(task.ID, "   "); err == nil {
		t.Fatal("comentariu gol acceptat")
	}
}

// intr-o etapa asignatul se muta, dar comentariul ajunge tot la cel care a lasat punctele; seful nu primeste nota
func TestUserCommentGoesToWhoLeftItems(t *testing.T) {
	f := setup(t)
	task := f.task(t, "Terms page", f.rook, store.StatusInProgress)
	if _, err := f.tm.LeaveForUser(f.workerCaller(f.rook, task.ID), "", []string{"Company name", "VAT"}); err != nil {
		t.Fatal(err)
	}
	task, _ = f.st.GetTask(task.ID)
	task.AssigneeID = f.vera.ID // simuleaza etapa urmatoare
	if err := f.st.SaveTask(&task); err != nil {
		t.Fatal(err)
	}
	if by, err := f.tm.TodosBy(task); err != nil || by.ID != f.rook.ID {
		t.Fatalf("TodosBy = %+v %v", by, err)
	}
	veraBefore, _ := f.st.Undelivered(f.vera.ID)
	leadBefore, _ := f.st.Undelivered(f.lead.ID)
	if err := f.tm.UserComment(task.ID, "Company is Meridian."); err != nil {
		t.Fatal(err)
	}
	pending, _ := f.st.Undelivered(f.rook.ID)
	if len(pending) == 0 || !strings.Contains(pending[len(pending)-1].Text, "1. Company name; 2. VAT") {
		t.Fatalf("mesajul nu a ajuns la Rook cu numerele: %+v", pending)
	}
	if after, _ := f.st.Undelivered(f.vera.ID); len(after) != len(veraBefore) {
		t.Fatal("asignatul nou a primit comentariul")
	}
	if rook, _ := f.st.GetAgent(f.rook.ID); rook.ParentID == nil {
		t.Fatal("Rook trebuie sa aiba sef ca testul sa conteze")
	}
	if after, _ := f.st.Undelivered(f.lead.ID); len(after) != len(leadBefore) {
		t.Fatal("seful a primit nota pentru un comentariu")
	}
}

func TestLeaveForUserListsNumbersAndTicksRecordEvents(t *testing.T) {
	f := setup(t)
	task := f.task(t, "Terms page", f.rook, store.StatusInProgress)
	c := f.workerCaller(f.rook, task.ID)
	f.tm.LeaveForUser(c, "", []string{"Company name", "VAT", "Address"})
	out, _ := f.tm.LeaveForUser(c, "", []string{"vat"})
	if !strings.HasPrefix(out, "No new items; they were already listed.") || !strings.Contains(out, "1. Company name; 2. VAT; 3. Address") {
		t.Fatalf("raspuns: %q", out)
	}
	out, err := f.tm.TickLeftForUser(c, "", []int{2})
	if err != nil || !strings.Contains(out, "1. Company name; 3. Address") {
		t.Fatalf("tick: %q %v", out, err)
	}
	if err := f.tm.UserSetTodo(task.ID, 1, true); err != nil {
		t.Fatal(err)
	}
	if err := f.tm.UserSetTodo(task.ID, 1, false); err != nil {
		t.Fatal(err)
	}
	kinds := ""
	events, _ := f.st.ListTaskEvents(task.ID)
	for _, e := range events {
		kinds += e.Kind + ","
	}
	if !strings.Contains(kinds, "left_for_user,todo_done,todo_done,todo_reopened,") {
		t.Fatalf("evenimente: %s", kinds)
	}
}
