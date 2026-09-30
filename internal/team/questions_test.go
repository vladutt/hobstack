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
