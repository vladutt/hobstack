package team

import (
	"errors"
	"strings"
	"testing"
	"time"

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

// notesFor intoarce textele notelor "The user accepted" primite de agent.
func acceptNotes(f *fixture, agentID int64) []string {
	msgs, _ := f.st.ListMessages(agentID)
	var out []string
	for _, m := range msgs {
		if strings.Contains(m.Text, "The user accepted") {
			out = append(out, m.Text)
		}
	}
	return out
}

// Vera are o intrebare raspunsa intr-o runda veche: nu mai primeste nimic.
func TestUserDecideNoNoteToOldAsker(t *testing.T) {
	f := setup(t)
	task, ds := userReviewTask(t, f)
	f.st.SetDecisions(task.ID, f.vera.Name, []store.Decision{{Question: "Old?", Options: []string{"Yes", "No"}}})
	all, _ := f.st.ListDecisions(task.ID)
	if err := f.st.AnswerDecisions(task.ID, []store.DecisionAnswer{{ID: all[2].ID, Choices: []string{"No"}}}); err != nil {
		t.Fatal(err)
	}
	if err := f.tm.UserDecide(task.ID, true, "", []store.DecisionAnswer{{ID: ds[0].ID, Choices: []string{"Yes"}}}); err != nil {
		t.Fatal(err)
	}
	if n := acceptNotes(f, f.vera.ID); len(n) != 0 {
		t.Fatalf("Vera: %q", n)
	}
	if n := acceptNotes(f, f.rook.ID); len(n) != 1 || strings.Contains(n[0], "Old?") {
		t.Fatalf("Rook: %q", n)
	}
}

// doi agenti cu intrebari deschise: fiecare primeste doar intrebarile lui
func TestUserDecideEachAskerOwnQuestions(t *testing.T) {
	f := setup(t)
	task, ds := userReviewTask(t, f)
	f.st.SetDecisions(task.ID, f.vera.Name, []store.Decision{{Question: "Vera asks?", Options: []string{"Yes", "No"}}})
	all, _ := f.st.ListDecisions(task.ID)
	if err := f.tm.UserDecide(task.ID, true, "thanks", []store.DecisionAnswer{
		{ID: ds[0].ID, Choices: []string{"Yes"}}, {ID: all[2].ID, Choices: []string{"No"}},
	}); err != nil {
		t.Fatal(err)
	}
	r, v := acceptNotes(f, f.rook.ID), acceptNotes(f, f.vera.ID)
	if len(r) != 1 || !strings.Contains(r[0], "Q1 Open a follow-up?: Yes") || !strings.Contains(r[0], "Q2 Which?: no answer") ||
		strings.Contains(r[0], "Vera asks?") || !strings.Contains(r[0], "Note: thanks") {
		t.Fatalf("Rook: %q", r)
	}
	if len(v) != 1 || !strings.Contains(v[0], "Q3 Vera asks?: No") || strings.Contains(v[0], "Open a follow-up?") || !strings.Contains(v[0], "Note: thanks") {
		t.Fatalf("Vera: %q", v)
	}
}

// doar nota: la cei cu intrebari deschise; daca nu sunt, doar la ultimul care a intrebat
func TestUserDecideNoteOnlyRecipients(t *testing.T) {
	f := setup(t)
	task, _ := userReviewTask(t, f)
	f.st.SetDecisions(task.ID, f.vera.Name, []store.Decision{{Question: "Vera asks?", Options: []string{"Yes"}}})
	all, _ := f.st.ListDecisions(task.ID)
	f.st.AnswerDecisions(task.ID, []store.DecisionAnswer{{ID: all[2].ID, Choices: []string{"Yes"}}})
	if err := f.tm.UserDecide(task.ID, true, "ok", nil); err != nil {
		t.Fatal(err)
	}
	if len(acceptNotes(f, f.vera.ID)) != 0 || len(acceptNotes(f, f.rook.ID)) != 1 {
		t.Fatal("nota trebuia doar la Rook (are intrebari deschise)")
	}

	// toate raspunse: doar ultimul care a intrebat (Vera)
	task2, ds2 := userReviewTask(t, f)
	f.st.AnswerDecisions(task2.ID, []store.DecisionAnswer{{ID: ds2[0].ID, Choices: []string{"No"}}, {ID: ds2[1].ID, Choices: []string{"A"}}})
	time.Sleep(5 * time.Millisecond)
	f.st.SetDecisions(task2.ID, f.vera.Name, []store.Decision{{Question: "Late?", Options: []string{"Yes"}}})
	all2, _ := f.st.ListDecisions(task2.ID)
	f.st.AnswerDecisions(task2.ID, []store.DecisionAnswer{{ID: all2[2].ID, Choices: []string{"Yes"}}})
	if err := f.tm.UserDecide(task2.ID, true, "ok2", nil); err != nil {
		t.Fatal(err)
	}
	if v := acceptNotes(f, f.vera.ID); len(v) != 1 || !strings.Contains(v[0], "Note: ok2") {
		t.Fatalf("Vera: %q", v)
	}
	if len(acceptNotes(f, f.rook.ID)) != 1 {
		t.Fatal("Rook nu trebuia sa mai primeasca o nota")
	}
}

// decided vine dupa decizie (user_accepted / sent_back), pe toate drumurile
func TestUserDecideDecidedAfterDecision(t *testing.T) {
	cases := []struct {
		accept   bool
		feedback string
		answer   bool
		after    string
	}{
		{true, "", true, "user_accepted"},
		{true, "note", false, "user_accepted"},
		{false, "", true, "sent_back"},
	}
	for _, c := range cases {
		f := setup(t)
		task, ds := userReviewTask(t, f)
		var ans []store.DecisionAnswer
		if c.answer {
			ans = []store.DecisionAnswer{{ID: ds[0].ID, Choices: []string{"Yes"}}}
		}
		if err := f.tm.UserDecide(task.ID, c.accept, c.feedback, ans); err != nil {
			t.Fatal(err)
		}
		evs, _ := f.st.ListTaskEvents(task.ID)
		di, ai := -1, -1
		for i, e := range evs {
			switch e.Kind {
			case "decided":
				di = i
			case c.after:
				ai = i
			}
		}
		if di < 0 || ai < 0 || di < ai {
			t.Fatalf("%+v: decided %d, %s %d", c, di, c.after, ai)
		}
	}
}

// reincercare: raspunsurile deja salvate (identice) nu dau eroare; altele da
func TestUserDecideRetrySameAnswers(t *testing.T) {
	f := setup(t)
	task, ds := userReviewTask(t, f)
	ans := []store.DecisionAnswer{{ID: ds[0].ID, Choices: []string{"Yes"}, Other: "soon"}}
	if err := f.tm.UserDecide(task.ID, true, "", ans); err != nil {
		t.Fatal(err)
	}
	// a doua oara, task-ul deja Done: doar ErrNotUserDecision
	if err := f.tm.UserDecide(task.ID, true, "", ans); !errors.Is(err, ErrNotUserDecision) {
		t.Fatalf("%v", err)
	}
	// ca dupa o decizie care a picat: raspunsurile salvate, task-ul inca in Review
	got, _ := f.st.GetTask(task.ID)
	got.Status, got.ReviewStage = store.StatusReview, store.StageLead
	f.st.SaveTask(&got)
	if err := f.tm.UserDecide(task.ID, false, "", []store.DecisionAnswer{{ID: ds[0].ID, Choices: []string{"No"}}}); !errors.Is(err, store.ErrBadAnswer) {
		t.Fatalf("raspuns diferit: %v", err)
	}
	if err := f.tm.UserDecide(task.ID, true, "", []store.DecisionAnswer{{ID: ds[0].ID, Choices: []string{"Yes"}, Other: " soon "}}); err != nil {
		t.Fatalf("raspuns identic: %v", err)
	}
	if got, _ := f.st.GetTask(task.ID); got.Status != store.StatusDone {
		t.Fatalf("%+v", got)
	}
}

// I1: accept doar cu nota pe un task fara intrebari, cu valori lasate pentru utilizator:
// nota ajunge la cel care a lasat punctele (nu la asignatul de acum) si istoricul are "decided"
func TestUserDecideNoteOnlyNoDecisionsGoesToTodosBy(t *testing.T) {
	f := setup(t)
	task := store.Task{ProjectID: f.p.ID, Title: "Invoice", AssigneeID: f.rook.ID, ByUser: true}
	if err := f.st.CreateTask(&task); err != nil {
		t.Fatal(err)
	}
	task.Status = store.StatusInProgress
	f.st.SaveTask(&task)
	c := f.workerCaller(f.rook, task.ID)
	if _, err := f.tm.ReportDone(c, "", "done"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.tm.LeaveForUser(c, "", []string{"VAT number"}); err != nil {
		t.Fatal(err)
	}
	// asignatul s-a schimbat (ca in etape): nota tot la Rook, care a lasat punctele
	got, _ := f.st.GetTask(task.ID)
	got.AssigneeID = f.vera.ID
	got.Status, got.ReviewStage = store.StatusReview, store.StageLead
	f.st.SaveTask(&got)
	if err := f.tm.UserDecide(task.ID, true, "VAT is RO123", nil); err != nil {
		t.Fatal(err)
	}
	notes := acceptNotes(f, f.rook.ID)
	if len(notes) != 1 || !strings.Contains(notes[0], "Note: VAT is RO123") {
		t.Fatalf("Rook: %q", notes)
	}
	msgs, _ := f.st.Undelivered(f.rook.ID)
	queued := false
	for _, m := range msgs {
		queued = queued || strings.Contains(m.Text, "VAT is RO123")
	}
	if !queued {
		t.Fatal("nota nu e in coada lui Rook")
	}
	if len(acceptNotes(f, f.vera.ID)) != 0 {
		t.Fatal("Vera nu a lasat punctele")
	}
	evs, _ := f.st.ListTaskEvents(task.ID)
	di, ai := -1, -1
	for i, e := range evs {
		switch e.Kind {
		case "decided":
			di = i
			if e.Body != "Note: VAT is RO123" || e.To != f.rook.Name {
				t.Fatalf("decided: %+v", e)
			}
		case "user_accepted":
			ai = i
		}
	}
	if di < 0 || ai < 0 || di < ai {
		t.Fatalf("decided %d, user_accepted %d", di, ai)
	}
}

// I2: worker cu reviewer: intrebarile ajung la lider odata cu aprobarea reviewer-ului
func TestDecisionsReachLeadAfterReviewApprove(t *testing.T) {
	f := setup(t)
	f.rook.ReviewerID = &f.vera.ID
	if err := f.st.UpdateAgent(f.rook); err != nil {
		t.Fatal(err)
	}
	task := f.task(t, "A", f.rook, store.StatusInProgress)
	c := f.workerCaller(f.rook, task.ID)
	if _, err := f.tm.ReportDone(c, "", "done"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.tm.AskDecisions(c, "", twoQuestions()); err != nil {
		t.Fatal(err)
	}
	vc := mcp.Caller{ProjectID: f.p.ID, AgentID: f.vera.ID, HasParent: true, IsReviewer: true}
	if _, err := f.tm.ReviewApprove(vc, task.Ref(), "ok"); err != nil {
		t.Fatal(err)
	}
	msgs, _ := f.st.ListMessages(f.lead.ID)
	for _, m := range msgs {
		if strings.Contains(m.Text, "Vera approved "+task.Ref()) {
			if !strings.Contains(m.Text, "Decisions for the user:") || !strings.Contains(m.Text, "Open a follow-up? (Yes / No)") ||
				!strings.Contains(m.Text, "Which? (A / B / C)") {
				t.Fatalf("nota liderului fara intrebari: %q", m.Text)
			}
			return
		}
	}
	t.Fatalf("liderul nu a primit aprobarea: %+v", msgs)
}

// I2: proiect cu etape: dupa ultima etapa, nota liderului are intrebarile deschise
func TestDecisionsReachLeadAfterLastStage(t *testing.T) {
	f, _, stages := stagesFixture(t)
	task := store.Task{ProjectID: f.p.ID, Title: "A", AssigneeID: f.rook.ID, CreatorID: &f.lead.ID, StageID: &stages[0].ID}
	if err := f.st.CreateTask(&task); err != nil {
		t.Fatal(err)
	}
	task.Status = store.StatusInProgress
	f.st.SaveTask(&task)
	c := f.workerCaller(f.rook, task.ID)
	if _, err := f.tm.ReportDone(c, "", "done"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.tm.AskDecisions(c, "", twoQuestions()); err != nil {
		t.Fatal(err)
	}
	if f.hasMessage(f.lead.ID, "Decisions for the user:") {
		t.Fatal("liderul nu decide inca (task-ul e in QA)")
	}
	got, _ := f.st.GetTask(task.ID)
	got.Status = store.StatusInProgress // Vera il ia in QA
	f.st.SaveTask(&got)
	vc := mcp.Caller{ProjectID: f.p.ID, AgentID: f.vera.ID, TaskID: task.ID, HasParent: true, StageKind: store.KindCheck}
	if _, err := f.tm.ReviewApprove(vc, task.Ref(), ""); err != nil {
		t.Fatal(err)
	}
	msgs, _ := f.st.ListMessages(f.lead.ID)
	for _, m := range msgs {
		if strings.Contains(m.Text, "went through all stages") {
			if !strings.Contains(m.Text, "Decisions for the user:") || !strings.Contains(m.Text, "Open a follow-up? (Yes / No)") {
				t.Fatalf("nota liderului fara intrebari: %q", m.Text)
			}
			return
		}
	}
	t.Fatalf("liderul nu a primit nota de final: %+v", msgs)
}

// I2: decide utilizatorul (task din board, cu reviewer): nimeni nu primeste intrebarile ca nota
func TestDecisionsNotSentWhenUserDecidesAfterReview(t *testing.T) {
	f := setup(t)
	f.rook.ReviewerID = &f.vera.ID
	f.st.UpdateAgent(f.rook)
	task := store.Task{ProjectID: f.p.ID, Title: "A", AssigneeID: f.rook.ID, ByUser: true}
	f.st.CreateTask(&task)
	task.Status = store.StatusInProgress
	f.st.SaveTask(&task)
	c := f.workerCaller(f.rook, task.ID)
	f.tm.ReportDone(c, "", "done")
	f.tm.AskDecisions(c, "", twoQuestions())
	vc := mcp.Caller{ProjectID: f.p.ID, AgentID: f.vera.ID, HasParent: true, IsReviewer: true}
	if _, err := f.tm.ReviewApprove(vc, task.Ref(), ""); err != nil {
		t.Fatal(err)
	}
	if f.hasMessage(f.lead.ID, "Decisions for the user:") {
		t.Fatal("liderul a primit intrebari desi decide utilizatorul")
	}
}

// M1: un report_done nou fara intrebari sterge intrebarile vechi fara raspuns ale agentului
func TestReportDoneClearsOldOpenDecisions(t *testing.T) {
	f := setup(t)
	task := f.task(t, "A", f.rook, store.StatusInProgress)
	c := f.workerCaller(f.rook, task.ID)
	f.tm.ReportDone(c, "", "done")
	if _, err := f.tm.AskDecisions(c, "", twoQuestions()); err != nil {
		t.Fatal(err)
	}
	ds, _ := f.st.ListDecisions(task.ID)
	if err := f.st.AnswerDecisions(task.ID, []store.DecisionAnswer{{ID: ds[0].ID, Choices: []string{"Yes"}}}); err != nil {
		t.Fatal(err)
	}
	got, _ := f.st.GetTask(task.ID)
	got.Status, got.ReviewStage = store.StatusInProgress, store.StageNone
	f.st.SaveTask(&got)
	if _, err := f.tm.ReportDone(c, "", "done again"); err != nil {
		t.Fatal(err)
	}
	list, _ := f.st.ListDecisions(task.ID)
	if len(list) != 1 || list[0].ID != ds[0].ID {
		t.Fatalf("raman doar intrebarile raspunse: %+v", list)
	}
}

// pasii ii face utilizatorul: agentul nu e rugat sa-i bifeze
func TestLeaveStepsNoTickInstruction(t *testing.T) {
	f := setup(t)
	task := f.task(t, "Build page", f.rook, store.StatusInProgress)
	c := f.workerCaller(f.rook, task.ID)
	f.tm.ReportDone(c, "", "done")
	out, err := f.tm.LeaveSteps(c, "", []string{"Set .env"})
	if err != nil || strings.Contains(out, "tick_left_for_user") || !strings.Contains(out, "the user does them") {
		t.Fatalf("%q %v", out, err)
	}
	out, _ = f.tm.LeaveForUser(c, "", []string{"VAT"})
	if !strings.Contains(out, "call tick_left_for_user with its number") {
		t.Fatalf("valori: %q", out)
	}
}
