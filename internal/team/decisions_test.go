package team

import (
	"errors"
	"strings"
	"testing"

	"foreman/internal/mcp"
	"foreman/internal/store"
)

func twoQuestions() []mcp.DecisionInput {
	return []mcp.DecisionInput{
		{Question: "Open a follow-up?", Options: []string{"Yes", "No"}},
		{Question: "Which?", Options: []string{"A", "B", "C"}, Multi: true},
	}
}

func TestAskDecisionsSavesWithAgentName(t *testing.T) {
	f := setup(t)
	task := f.task(t, "Build page", f.rook, store.StatusInProgress)
	c := f.workerCaller(f.rook, task.ID)
	if _, err := f.tm.ReportDone(c, "", "done"); err != nil {
		t.Fatal(err)
	}
	out, err := f.tm.AskDecisions(c, "", twoQuestions())
	if err != nil || !strings.Contains(out, "2 decisions waiting for the user.") {
		t.Fatalf("%q %v", out, err)
	}
	list, _ := f.st.ListDecisions(task.ID)
	if len(list) != 2 || list[0].AskedBy != f.rook.Name || list[1].Question != "Which?" || !list[1].Multi || len(list[1].Options) != 3 {
		t.Fatalf("%+v", list)
	}
	// un agent strain nu poate pune intrebari pe task
	if _, err := f.tm.AskDecisions(f.workerCaller(f.vera, 0), task.Ref(), twoQuestions()); err == nil {
		t.Fatal("un agent strain a pus intrebari")
	}
}

func TestAskDecisionsArchivedTask(t *testing.T) {
	f := setup(t)
	task := f.task(t, "Build page", f.rook, store.StatusInProgress)
	if _, err := f.tm.ArchiveTask(task.ID); err != nil {
		t.Fatal(err)
	}
	_, err := f.tm.AskDecisions(f.workerCaller(f.rook, task.ID), "", twoQuestions())
	if err == nil || !strings.Contains(err.Error(), "was deleted") {
		t.Fatalf("%v", err)
	}
}

func TestLeaveStepsSavesAsSteps(t *testing.T) {
	f := setup(t)
	task := f.task(t, "Build page", f.rook, store.StatusInProgress)
	c := f.workerCaller(f.rook, task.ID)
	f.tm.ReportDone(c, "", "done")
	out, err := f.tm.LeaveSteps(c, "", []string{"Set .env", "Restart"})
	if err != nil || !strings.Contains(out, "2 steps") {
		t.Fatalf("%q %v", out, err)
	}
	l, _ := f.st.ListTodos(task.ID)
	if len(l) != 2 || l[0].Kind != "step" {
		t.Fatalf("%+v", l)
	}
}

// task decis de un lider: intrebarile ajung la el in nota de raport
func TestAskDecisionsReachesLeader(t *testing.T) {
	f := setup(t)
	task := f.task(t, "Build page", f.rook, store.StatusInProgress)
	c := f.workerCaller(f.rook, task.ID)
	f.tm.ReportDone(c, "", "done")
	if _, err := f.tm.AskDecisions(c, "", twoQuestions()); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Decisions for the user:", "Open a follow-up?", "Yes / No", "A / B / C"} {
		if !f.hasMessage(f.lead.ID, want) {
			t.Fatalf("liderul nu a primit %q", want)
		}
	}
}

// task creat din board: decide utilizatorul, liderul nu primeste nimic
func TestAskDecisionsUserDecidesNoNote(t *testing.T) {
	f := setup(t)
	task := store.Task{ProjectID: f.p.ID, Title: "Build page", AssigneeID: f.rook.ID, ByUser: true, Status: store.StatusInProgress}
	if err := f.st.CreateTask(&task); err != nil {
		t.Fatal(err)
	}
	task.Status = store.StatusInProgress
	f.st.SaveTask(&task)
	c := f.workerCaller(f.rook, task.ID)
	f.tm.ReportDone(c, "", "done")
	if _, err := f.tm.AskDecisions(c, "", twoQuestions()); err != nil {
		t.Fatal(err)
	}
	if f.hasMessage(f.lead.ID, "Decisions for the user:") {
		t.Fatal("liderul a primit intrebari desi decide utilizatorul")
	}
}

// userReviewTask: task creat din board, in Review la final (il decide utilizatorul), cu 2 intrebari puse de Rook.
func userReviewTask(t *testing.T, f *fixture) (store.Task, []store.Decision) {
	t.Helper()
	task := store.Task{ProjectID: f.p.ID, Title: "Build page", AssigneeID: f.rook.ID, ByUser: true}
	if err := f.st.CreateTask(&task); err != nil {
		t.Fatal(err)
	}
	task.Status, task.ReviewStage, task.Result = store.StatusReview, store.StageLead, "done"
	if err := f.st.SaveTask(&task); err != nil {
		t.Fatal(err)
	}
	if err := f.st.SetDecisions(task.ID, f.rook.Name, []store.Decision{
		{Question: "Open a follow-up?", Options: []string{"Yes", "No"}},
		{Question: "Which?", Options: []string{"A", "B", "C"}, Multi: true},
	}); err != nil {
		t.Fatal(err)
	}
	ds, _ := f.st.ListDecisions(task.ID)
	return task, ds
}

func TestUserDecideAcceptWithAnswers(t *testing.T) {
	f := setup(t)
	task, ds := userReviewTask(t, f)
	if err := f.tm.UserDecide(task.ID, true, "", []store.DecisionAnswer{{ID: ds[0].ID, Choices: []string{"Yes"}}}); err != nil {
		t.Fatal(err)
	}
	if got, _ := f.st.GetTask(task.ID); got.Status != store.StatusDone {
		t.Fatalf("%+v", got)
	}
	msgs, _ := f.st.ListMessages(f.rook.ID)
	var note *store.Message
	for i := range msgs {
		if strings.Contains(msgs[i].Text, "The user accepted "+task.Ref()+".") {
			note = &msgs[i]
		}
	}
	if note == nil || note.Delivered || !strings.Contains(note.Text, "Q1 Open a follow-up?: Yes") || !strings.Contains(note.Text, "Q2 Which?: no answer") {
		t.Fatalf("nota catre Rook: %+v", note)
	}
	evs, _ := f.st.ListTaskEvents(task.ID)
	found := false
	for _, e := range evs {
		if e.Kind == "decided" && e.Actor == "You" && strings.Contains(e.Body, "Q1 Open a follow-up?: Yes") {
			found = true
		}
	}
	if !found {
		t.Fatalf("lipseste evenimentul decided: %+v", evs)
	}
	list, _ := f.st.ListDecisions(task.ID)
	if list[0].AnsweredAt == nil || list[1].AnsweredAt != nil {
		t.Fatalf("%+v", list)
	}
}

func TestUserDecideAcceptWithoutAnswersNoNote(t *testing.T) {
	f := setup(t)
	task, _ := userReviewTask(t, f)
	before := countMessages(t, f, f.rook, "")
	if err := f.tm.UserDecide(task.ID, true, "", nil); err != nil {
		t.Fatal(err)
	}
	if got, _ := f.st.GetTask(task.ID); got.Status != store.StatusDone {
		t.Fatalf("%+v", got)
	}
	if n := countMessages(t, f, f.rook, ""); n != before {
		t.Fatalf("nota noua fara raspunsuri: %d -> %d", before, n)
	}
	evs, _ := f.st.ListTaskEvents(task.ID)
	for _, e := range evs {
		if e.Kind == "decided" {
			t.Fatalf("decided fara nimic de spus: %+v", e)
		}
	}
}

func TestUserDecideAcceptWithNoteOnly(t *testing.T) {
	f := setup(t)
	task, _ := userReviewTask(t, f)
	if err := f.tm.UserDecide(task.ID, true, "  good job ", nil); err != nil {
		t.Fatal(err)
	}
	if !f.hasMessage(f.rook.ID, "Note: good job") {
		t.Fatal("nota utilizatorului nu a ajuns la Rook")
	}
	evs, _ := f.st.ListTaskEvents(task.ID)
	if last := evs[len(evs)-1]; last.Kind != "decided" || last.Body != "Note: good job" {
		t.Fatalf("%+v", last)
	}
}

func TestUserDecideSendBackAnswersOnly(t *testing.T) {
	f := setup(t)
	task, ds := userReviewTask(t, f)
	if err := f.tm.UserDecide(task.ID, false, "", []store.DecisionAnswer{{ID: ds[1].ID, Choices: []string{"A", "C"}, Other: "and D"}}); err != nil {
		t.Fatal(err)
	}
	got, _ := f.st.GetTask(task.ID)
	if got.Status != store.StatusQueued || !strings.Contains(got.Feedback, "Q2 Which?: A, C; other: and D") || !strings.Contains(got.Feedback, "Q1 Open a follow-up?: no answer") {
		t.Fatalf("%+v", got)
	}
}

func TestUserDecideSendBackNoteAndAnswers(t *testing.T) {
	f := setup(t)
	task, ds := userReviewTask(t, f)
	if err := f.tm.UserDecide(task.ID, false, "fix the header", []store.DecisionAnswer{{ID: ds[0].ID, Choices: []string{"No"}}}); err != nil {
		t.Fatal(err)
	}
	got, _ := f.st.GetTask(task.ID)
	if !strings.HasPrefix(got.Feedback, "fix the header\n\nQ1 Open a follow-up?: No") {
		t.Fatalf("%q", got.Feedback)
	}
}

func TestUserDecideSendBackEmptyFails(t *testing.T) {
	f := setup(t)
	task, _ := userReviewTask(t, f)
	err := f.tm.UserDecide(task.ID, false, " ", nil)
	if err == nil || err.Error() != "write what needs to change" {
		t.Fatalf("%v", err)
	}
	// un raspuns gol (fara alegeri si fara other) nu conteaza
	if err := f.tm.UserDecide(task.ID, false, "", []store.DecisionAnswer{{ID: 1}}); err == nil {
		t.Fatal("raspuns gol acceptat ca feedback")
	}
}

func TestUserDecideBadAnswerSavesNothing(t *testing.T) {
	f := setup(t)
	task, ds := userReviewTask(t, f)
	err := f.tm.UserDecide(task.ID, true, "", []store.DecisionAnswer{
		{ID: ds[0].ID, Choices: []string{"Yes"}},
		{ID: ds[1].ID, Choices: []string{"Z"}},
	})
	if !errors.Is(err, store.ErrBadAnswer) {
		t.Fatalf("%v", err)
	}
	got, _ := f.st.GetTask(task.ID)
	list, _ := f.st.ListDecisions(task.ID)
	if got.Status != store.StatusReview || list[0].AnsweredAt != nil {
		t.Fatalf("%+v %+v", got, list)
	}
	// other prea lung -> tot ErrBadAnswer
	long := strings.Repeat("x", 301)
	if err := f.tm.UserDecide(task.ID, true, "", []store.DecisionAnswer{{ID: ds[0].ID, Other: long}}); !errors.Is(err, store.ErrBadAnswer) {
		t.Fatalf("%v", err)
	}
	if got, _ := f.st.GetTask(task.ID); got.Status != store.StatusReview {
		t.Fatalf("%+v", got)
	}
}

func TestAnswersText(t *testing.T) {
	ds := []store.Decision{
		{Position: 1, Question: "Q?", Answer: []string{"A", "B"}, Other: "c"},
		{Position: 2, Question: "R?", Answer: []string{}},
		{Position: 3, Question: "S?", Answer: []string{}, Other: "only"},
	}
	want := "Q1 Q?: A, B; other: c\nQ2 R?: no answer\nQ3 S?: other: only"
	if got := answersText(ds); got != want {
		t.Fatalf("%q", got)
	}
}
