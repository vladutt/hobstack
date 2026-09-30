package team

import (
	"errors"
	"strings"
	"testing"

	"foreman/internal/mcp"
	"foreman/internal/runner"
	"foreman/internal/store"
)

func TestAskUserKeepsTaskWaiting(t *testing.T) {
	f := setup(t)
	task := f.task(t, "A", f.rook, store.StatusQueued)
	mustReconcile(t, f)
	spec := f.fs.last(t, f.rook)
	c := mcp.Caller{ProjectID: f.p.ID, AgentID: f.rook.ID, RunID: spec.ID, TaskID: task.ID, HasParent: true}

	if _, err := f.tm.AskUser(c, "  ", nil); err == nil {
		t.Fatal("intrebare goala")
	}
	if _, err := f.tm.AskUser(c, "Q", []string{"a", "b", "c", "d", "e"}); err == nil {
		t.Fatal("prea multe optiuni")
	}
	out, err := f.tm.AskUser(c, "Allow Bash for the tests?", []string{"Yes", "No"})
	if err != nil || !strings.Contains(out, "Stop now") {
		t.Fatalf("%q %v", out, err)
	}
	qs, _ := f.st.ListOpenQuestions(f.p.ID)
	if len(qs) != 1 || qs[0].TaskID == nil || *qs[0].TaskID != task.ID || qs[0].RunID != spec.ID {
		t.Fatalf("%+v", qs)
	}

	// run-ul se termina fara report_done: task-ul ramane In progress
	f.fs.finish(spec.ID, runner.Outcome{Status: store.RunDone, Result: "I asked the user."})
	got, _ := f.st.GetTask(task.ID)
	if got.Status != store.StatusInProgress {
		t.Fatalf("task: %s", got.Status)
	}
	msgs, _ := f.st.ListMessages(f.rook.ID)
	found := false
	for _, m := range msgs {
		if strings.Contains(m.Text, "waiting for your answer") {
			found = true
		}
	}
	if !found {
		t.Fatal("lipseste nota 'waiting for your answer'")
	}
}

func TestAnswerRequeuesTaskWithMessage(t *testing.T) {
	f := setup(t)
	task := f.task(t, "A", f.rook, store.StatusQueued)
	mustReconcile(t, f)
	spec := f.fs.last(t, f.rook)
	c := mcp.Caller{ProjectID: f.p.ID, AgentID: f.rook.ID, RunID: spec.ID, TaskID: task.ID, HasParent: true}
	f.tm.AskUser(c, "Allow Bash?", []string{"Yes", "No"})
	f.fs.finish(spec.ID, runner.Outcome{Status: store.RunDone})
	qs, _ := f.st.ListOpenQuestions(f.p.ID)

	if err := f.tm.AnswerQuestion(qs[0].ID, "Yes, Shell is enabled now"); err != nil {
		t.Fatal(err)
	}
	if err := f.tm.AnswerQuestion(qs[0].ID, "again"); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("a doua oara: %v", err)
	}
	mustReconcile(t, f)
	next := f.fs.last(t, f.rook)
	if next.ID == spec.ID || !strings.Contains(turn(next), "Task T-") || !strings.Contains(turn(next), `Answer to your question "Allow Bash?": Yes, Shell is enabled now`) {
		t.Fatalf("urmatorul run trebuia sa fie pe task, cu raspunsul:\n%s", turn(next))
	}
	msgs, _ := f.st.ListMessages(f.rook.ID)
	n := 0
	for _, m := range msgs {
		if strings.Contains(m.Text, "Answer to your question") {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("mesaje de raspuns: %d", n)
	}
}

func TestDismissQuestionWithoutTask(t *testing.T) {
	f := setup(t)
	c := mcp.Caller{ProjectID: f.p.ID, AgentID: f.lead.ID, RunID: "r"}
	f.tm.AskUser(c, "Which DB?", nil)
	qs, _ := f.st.ListOpenQuestions(f.p.ID)
	if err := f.tm.DismissQuestion(qs[0].ID); err != nil {
		t.Fatal(err)
	}
	got, _ := f.st.GetQuestion(qs[0].ID)
	if got.Status != store.QuestionDismissed {
		t.Fatalf("%+v", got)
	}
	msgs, _ := f.st.ListMessages(f.lead.ID)
	if !strings.Contains(msgs[len(msgs)-1].Text, "dismissed your question") {
		t.Fatalf("%+v", msgs[len(msgs)-1])
	}
}

func TestAskUserLimits(t *testing.T) {
	f := setup(t)
	c := mcp.Caller{ProjectID: f.p.ID, AgentID: f.lead.ID, RunID: "r"}
	if _, err := f.tm.AskUser(c, strings.Repeat("a", 2001), nil); err == nil {
		t.Fatal("intrebare de 2001 caractere")
	}
	if _, err := f.tm.AskUser(c, "Q", []string{strings.Repeat("o", 61)}); err == nil {
		t.Fatal("optiune de 61 caractere")
	}
	if _, err := f.tm.AskUser(c, strings.Repeat("a", 2000), []string{strings.Repeat("o", 60)}); err != nil {
		t.Fatalf("la limita: %v", err)
	}
}

// askOnTask porneste un run al lui Rook pe un task nou si pune o intrebare din el.
func askOnTask(t *testing.T, f *fixture, title string, questions ...string) (store.Task, runner.Spec) {
	t.Helper()
	task := f.task(t, title, f.rook, store.StatusQueued)
	mustReconcile(t, f)
	spec := f.fs.last(t, f.rook)
	c := f.caller(t, spec)
	if c.TaskID != task.ID {
		t.Fatalf("run-ul lui Rook e pe task %d, vreau %d", c.TaskID, task.ID)
	}
	for _, q := range questions {
		if _, err := f.tm.AskUser(c, q, nil); err != nil {
			t.Fatal(err)
		}
	}
	return task, spec
}

func wantTask(t *testing.T, f *fixture, id int64, status string) {
	t.Helper()
	got, err := f.st.GetTask(id)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != status {
		t.Fatalf("%s are status %q, vreau %q", got.Ref(), got.Status, status)
	}
}

func openQuestionIDs(t *testing.T, f *fixture) []int64 {
	t.Helper()
	qs, err := f.st.ListOpenQuestions(f.p.ID)
	if err != nil {
		t.Fatal(err)
	}
	var ids []int64
	for _, q := range qs {
		ids = append(ids, q.ID)
	}
	return ids
}

// raspunsul vine cat timp agentul lucreaza la ALT task: task-ul care astepta revine in coada
func TestAnswerWhileBusyOnOtherTask(t *testing.T) {
	f := setup(t)
	a, spec := askOnTask(t, f, "A", "Allow Bash?")
	f.fs.finish(spec.ID, runner.Outcome{Status: store.RunDone})
	wantTask(t, f, a.ID, store.StatusInProgress)

	b := f.task(t, "B", f.rook, store.StatusQueued)
	mustReconcile(t, f)
	if c := f.caller(t, f.fs.last(t, f.rook)); c.TaskID != b.ID {
		t.Fatalf("Rook trebuia sa lucreze la B, e pe %d", c.TaskID)
	}
	if err := f.tm.AnswerQuestion(openQuestionIDs(t, f)[0], "Yes"); err != nil {
		t.Fatal(err)
	}
	wantTask(t, f, a.ID, store.StatusQueued)
}

// raspunsul vine in timpul run-ului care a intrebat: la final task-ul revine
// in coada (nu la review), iar urmatorul run e pe task, cu raspunsul
func TestAnswerDuringAskingRun(t *testing.T) {
	f := setup(t)
	a, spec := askOnTask(t, f, "A", "Allow Bash?")
	if err := f.tm.AnswerQuestion(openQuestionIDs(t, f)[0], "Yes, go"); err != nil {
		t.Fatal(err)
	}
	wantTask(t, f, a.ID, store.StatusInProgress) // run-ul inca lucreaza la el
	f.fs.finish(spec.ID, runner.Outcome{Status: store.RunDone, Result: "I asked."})
	wantTask(t, f, a.ID, store.StatusQueued)

	mustReconcile(t, f)
	next := f.fs.last(t, f.rook)
	if next.ID == spec.ID || !strings.Contains(turn(next), a.Ref()) || !strings.Contains(turn(next), `Answer to your question "Allow Bash?": Yes, go`) {
		t.Fatalf("urmatorul run trebuia sa fie pe %s, cu raspunsul:\n%s", a.Ref(), turn(next))
	}
}

// Stop team cat timp run-ul care a intrebat e activ: task-ul ramane In progress
// (asteapta raspunsul); raspunsul il pune in coada
func TestStopWhileQuestionOpen(t *testing.T) {
	f := setup(t)
	a, spec := askOnTask(t, f, "A", "Allow Bash?")
	if err := f.tm.Stop(f.p.ID); err != nil {
		t.Fatal(err)
	}
	f.fs.finish(spec.ID, runner.Outcome{Status: store.RunStopped})
	wantTask(t, f, a.ID, store.StatusInProgress)

	if err := f.tm.AnswerQuestion(openQuestionIDs(t, f)[0], "Yes"); err != nil {
		t.Fatal(err)
	}
	wantTask(t, f, a.ID, store.StatusQueued)
}

// doua intrebari deschise pe acelasi task: doar ultimul raspuns il pune in coada
func TestTwoQuestionsOnOneTask(t *testing.T) {
	f := setup(t)
	a, spec := askOnTask(t, f, "A", "First?", "Second?")
	f.fs.finish(spec.ID, runner.Outcome{Status: store.RunDone})
	wantTask(t, f, a.ID, store.StatusInProgress)

	ids := openQuestionIDs(t, f)
	if len(ids) != 2 {
		t.Fatalf("intrebari deschise: %v", ids)
	}
	if err := f.tm.AnswerQuestion(ids[0], "one"); err != nil {
		t.Fatal(err)
	}
	wantTask(t, f, a.ID, store.StatusInProgress)
	if err := f.tm.DismissQuestion(ids[1]); err != nil {
		t.Fatal(err)
	}
	wantTask(t, f, a.ID, store.StatusQueued)
}

// doua intrebari, una raspunsa in timpul run-ului: la final task-ul inca asteapta
func TestAnswerDuringRunWithAnotherOpen(t *testing.T) {
	f := setup(t)
	a, spec := askOnTask(t, f, "A", "First?", "Second?")
	if err := f.tm.AnswerQuestion(openQuestionIDs(t, f)[0], "one"); err != nil {
		t.Fatal(err)
	}
	f.fs.finish(spec.ID, runner.Outcome{Status: store.RunDone})
	wantTask(t, f, a.ID, store.StatusInProgress)
	if err := f.tm.AnswerQuestion(openQuestionIDs(t, f)[0], "two"); err != nil {
		t.Fatal(err)
	}
	wantTask(t, f, a.ID, store.StatusQueued)
}
