package team

import (
	"strings"
	"testing"
	"unicode/utf8"

	"foreman/internal/store"
)

func TestCreateTaskAssignsWorker(t *testing.T) {
	f := setup(t)
	text, err := f.tm.CreateTask(f.leadCaller(), "Parser $ref", "Doar local", "Rook")
	if err != nil || text != "Creat T-1 pentru Rook. Il va primi automat." {
		t.Fatalf("%q %v", text, err)
	}
	task := f.wantStatus(t, 1, store.StatusQueued)
	if task.AssigneeID != f.rook.ID || task.Description != "Doar local" {
		t.Fatalf("task = %+v", task)
	}
	if !f.hasMessage(f.lead.ID, "A creat T-1 pentru Rook: Parser $ref") {
		t.Error("lipseste evenimentul din thread-ul lead-ului")
	}
	if !f.hasMessage(f.rook.ID, "T-1 asignat de Lead: Parser $ref") {
		t.Error("lipseste evenimentul din thread-ul lui Rook")
	}
}

func TestCreateTaskAssigneeLoose(t *testing.T) {
	f := setup(t)
	if _, err := f.tm.CreateTask(f.leadCaller(), "A", "", "  rook "); err != nil {
		t.Fatal(err)
	}
	if task := f.wantStatus(t, 1, store.StatusQueued); task.AssigneeID != f.rook.ID {
		t.Fatalf("asignat lui %d", task.AssigneeID)
	}
}

func TestCreateTaskErrors(t *testing.T) {
	f := setup(t)
	if _, err := f.tm.CreateTask(f.leadCaller(), "A", "", "Nimeni"); err == nil || !strings.Contains(err.Error(), "Rook, Vera") {
		t.Errorf("worker necunoscut: %v", err)
	}
	if _, err := f.tm.CreateTask(f.leadCaller(), "A", "", "Lead"); err == nil {
		t.Error("task asignat lead-ului trebuia refuzat")
	}
	if _, err := f.tm.CreateTask(f.leadCaller(), "  ", "", "Rook"); err == nil {
		t.Error("titlu gol trebuia refuzat")
	}
	if _, err := f.tm.CreateTask(f.workerCaller(f.rook, 0), "A", "", "Vera"); err == nil {
		t.Error("un worker nu poate crea task-uri")
	}
	if tasks, _ := f.st.ListTasks(f.p.ID); len(tasks) != 0 {
		t.Fatalf("s-au creat task-uri: %+v", tasks)
	}
}

func TestListTasks(t *testing.T) {
	f := setup(t)
	if text, _ := f.tm.ListTasks(f.leadCaller(), ""); text != "Nu exista task-uri." {
		t.Fatalf("gol: %q", text)
	}
	f.task(t, "A", f.rook, store.StatusQueued)
	b := f.task(t, "B", f.vera, store.StatusReview)
	b.Result = "gata"
	f.st.SaveTask(&b)

	all, err := f.tm.ListTasks(f.leadCaller(), "")
	if err != nil || all != "T-1 [queued] A -> Rook\nT-2 [review] B -> Vera\n  Rezultat: gata" {
		t.Fatalf("toate: %q %v", all, err)
	}
	review, _ := f.tm.ListTasks(f.leadCaller(), "review")
	if review != "T-2 [review] B -> Vera\n  Rezultat: gata" {
		t.Fatalf("review: %q", review)
	}
	if _, err := f.tm.ListTasks(f.leadCaller(), "gresit"); err == nil {
		t.Fatal("status necunoscut trebuia refuzat")
	}
}

func TestReportDone(t *testing.T) {
	f := setup(t)
	task := f.task(t, "A", f.rook, store.StatusInProgress)
	text, err := f.tm.ReportDone(f.workerCaller(f.rook, task.ID), "A e gata")
	if err != nil || text != "T-1 e in Review. Lead va verifica." {
		t.Fatalf("%q %v", text, err)
	}
	if got := f.wantStatus(t, 1, store.StatusReview); got.Result != "A e gata" {
		t.Fatalf("rezultat = %q", got.Result)
	}
	pending, _ := f.st.Undelivered(f.lead.ID)
	if len(pending) != 1 || pending[0].Text != "Rook a terminat T-1 (review): A e gata" {
		t.Fatalf("notificari lead: %+v", pending)
	}
}

func TestReportDoneErrors(t *testing.T) {
	f := setup(t)
	queued := f.task(t, "A", f.rook, store.StatusQueued)
	running := f.task(t, "B", f.rook, store.StatusInProgress)
	cases := map[string]error{}
	_, cases["fara task"] = f.tm.ReportDone(f.workerCaller(f.rook, 0), "x")
	_, cases["lead"] = f.tm.ReportDone(f.leadCaller(), "x")
	_, cases["task care nu e in lucru"] = f.tm.ReportDone(f.workerCaller(f.rook, queued.ID), "x")
	_, cases["rezumat gol"] = f.tm.ReportDone(f.workerCaller(f.rook, running.ID), "  ")
	for name, err := range cases {
		if err == nil {
			t.Errorf("%s: trebuia eroare", name)
		}
	}
}

func TestLongSummaryIsClipped(t *testing.T) {
	f := setup(t)
	task := f.task(t, "A", f.rook, store.StatusInProgress)
	long := strings.Repeat("x", 10000)
	if _, err := f.tm.ReportDone(f.workerCaller(f.rook, task.ID), long); err != nil {
		t.Fatal(err)
	}
	if got := f.wantStatus(t, 1, store.StatusReview); got.Result != long {
		t.Fatal("rezultatul complet trebuie pastrat pe task")
	}
	pending, _ := f.st.Undelivered(f.lead.ID)
	if n := utf8.RuneCountInString(pending[0].Text); n > noteMax+100 {
		t.Fatalf("notificarea are %d caractere", n)
	}
}

func TestSendBackThenBlock(t *testing.T) {
	f := setup(t)
	f.task(t, "A", f.rook, store.StatusReview)
	for i := 1; i <= 2; i++ {
		text, err := f.tm.SendBack(f.leadCaller(), "T-1", "mai bine")
		want := "T-1 trimis inapoi lui Rook (" + string(rune('0'+i)) + "/3)."
		if err != nil || text != want {
			t.Fatalf("trimiterea %d: %q %v", i, text, err)
		}
		got := f.wantStatus(t, 1, store.StatusQueued)
		if got.Feedback != "mai bine" || got.SendBacks != i {
			t.Fatalf("dupa trimiterea %d: %+v", i, got)
		}
		got.Status = store.StatusReview // worker-ul a raportat din nou
		f.st.SaveTask(&got)
	}
	text, err := f.tm.SendBack(f.leadCaller(), "1", "tot nu")
	if err != nil || !strings.Contains(text, "blocat") {
		t.Fatalf("a treia trimitere: %q %v", text, err)
	}
	f.wantStatus(t, 1, store.StatusBlocked)
}

func TestSendBackErrors(t *testing.T) {
	f := setup(t)
	f.task(t, "A", f.rook, store.StatusQueued)
	if _, err := f.tm.SendBack(f.leadCaller(), "T-1", "x"); err == nil {
		t.Error("task din queued nu se poate trimite inapoi")
	}
	if _, err := f.tm.SendBack(f.leadCaller(), "T-1", " "); err == nil {
		t.Error("feedback gol trebuia refuzat")
	}
	if _, err := f.tm.SendBack(f.leadCaller(), "T-99", "x"); err == nil || !strings.Contains(err.Error(), "T-99") {
		t.Errorf("task inexistent: %v", err)
	}
	if _, err := f.tm.SendBack(f.leadCaller(), "abc", "x"); err == nil {
		t.Error("referinta invalida trebuia refuzata")
	}
}

func TestAcceptTask(t *testing.T) {
	f := setup(t)
	f.task(t, "A", f.rook, store.StatusReview)
	f.task(t, "B", f.rook, store.StatusQueued)
	if text, err := f.tm.AcceptTask(f.leadCaller(), "t-1", "bun"); err != nil || text != "T-1 e Done." {
		t.Fatalf("%q %v", text, err)
	}
	f.wantStatus(t, 1, store.StatusDone)
	if !f.hasMessage(f.lead.ID, "A acceptat T-1: bun") {
		t.Error("lipseste evenimentul de accept")
	}
	if _, err := f.tm.AcceptTask(f.leadCaller(), "T-2", ""); err == nil {
		t.Error("task din queued nu se poate accepta")
	}
}

func TestResolveUnknownToken(t *testing.T) {
	f := setup(t)
	if _, ok := f.tm.Resolve("nimic"); ok {
		t.Fatal("token necunoscut rezolvat")
	}
}
