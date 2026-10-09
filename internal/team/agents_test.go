package team

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"hobstack/internal/mcp"
	"hobstack/internal/runner"
	"hobstack/internal/store"
)

// withTools seteaza tool-urile liderului fixture-ului.
func withTools(t *testing.T, f *fixture, tools ...string) {
	t.Helper()
	f.lead.Tools = tools
	if err := f.st.UpdateAgent(f.lead); err != nil {
		t.Fatal(err)
	}
}

func quinn() mcp.AddAgentInput {
	return mcp.AddAgentInput{Name: "Quinn", Role: "QA", Instructions: "Write tests", Reason: "no one writes tests"}
}

func TestAddAgentRequestWithoutAutonomy(t *testing.T) {
	f := setup(t)
	withTools(t, f, "write")
	text, err := f.tm.AddAgent(f.leadCaller(), quinn())
	if err != nil || !strings.Contains(text, "Request sent to the user") {
		t.Fatalf("%q %v", text, err)
	}
	if _, err := f.st.FindAgentByName(f.p.ID, "Quinn"); !errors.Is(err, store.ErrNotFound) {
		t.Fatal("fara aprobare agentul nu exista inca")
	}
	reqs, _ := f.st.ListOpenAgentRequests(f.p.ID)
	if len(reqs) != 1 || reqs[0].LeadID != f.lead.ID || reqs[0].Spec.Model != "sonnet" || strings.Join(reqs[0].Spec.Tools, ",") != "write" {
		t.Fatalf("%+v", reqs)
	}
}

func TestAddAgentDirectWithAutonomy(t *testing.T) {
	f := setup(t)
	withTools(t, f, "write", "git")
	autonomous(t, f)
	in := quinn()
	in.Tools, in.ToolsSet, in.Model = []string{"git"}, true, "haiku"
	text, err := f.tm.AddAgent(f.leadCaller(), in)
	if err != nil || !strings.Contains(text, `"Quinn" added to your team`) {
		t.Fatalf("%q %v", text, err)
	}
	a, err := f.st.FindAgentByName(f.p.ID, "Quinn")
	if err != nil {
		t.Fatal(err)
	}
	if a.ParentID == nil || *a.ParentID != f.lead.ID || a.CreatedBy == nil || *a.CreatedBy != f.lead.ID ||
		a.Model != "haiku" || strings.Join(a.Tools, ",") != "git" || a.ReviewerID != nil || a.PosX != nil {
		t.Fatalf("%+v", a)
	}
}

func TestAddAgentDirectFromChat(t *testing.T) {
	f := setup(t)
	withTools(t, f, "write")
	if _, err := f.tm.UserMessage(f.lead.ID, "add a QA agent", nil); err != nil {
		t.Fatal(err)
	}
	mustReconcile(t, f)
	run := f.fs.last(t, f.lead)
	c := f.caller(t, run)
	if !c.CanAddAgents {
		t.Fatal("liderul trebuie sa aiba CanAddAgents")
	}
	if _, err := f.tm.AddAgent(c, quinn()); err != nil {
		t.Fatal(err)
	}
	if _, err := f.st.FindAgentByName(f.p.ID, "Quinn"); err != nil {
		t.Fatal("din chat agentul se creeaza direct")
	}
	f.fs.finish(run.ID, runner.Outcome{Status: "done"})
}

func TestAddAgentRefusals(t *testing.T) {
	f := setup(t)
	withTools(t, f, "write")
	autonomous(t, f)
	cases := []struct {
		name string
		edit func(*mcp.AddAgentInput)
		want string
	}{
		{"tool peste ale liderului", func(in *mcp.AddAgentInput) { in.Tools, in.ToolsSet = []string{"write", "shell"}, true }, "only give tools you have yourself: shell"},
		{"nume luat", func(in *mcp.AddAgentInput) { in.Name = "rook" }, `an agent named "rook" already exists`},
		{"model necunoscut", func(in *mcp.AddAgentInput) { in.Model = "gpt" }, "unknown model: gpt"},
		{"fara motiv", func(in *mcp.AddAgentInput) { in.Reason = " " }, "reason is required"},
		{"fara rol", func(in *mcp.AddAgentInput) { in.Role = "" }, "role is required"},
		{"fara instructiuni", func(in *mcp.AddAgentInput) { in.Instructions = "" }, "instructions are required"},
		{"tool necunoscut", func(in *mcp.AddAgentInput) { in.Tools, in.ToolsSet = []string{"root"}, true }, "unknown tool: root"},
	}
	for _, tc := range cases {
		in := quinn()
		tc.edit(&in)
		if _, err := f.tm.AddAgent(f.leadCaller(), in); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: %v", tc.name, err)
		}
	}
	// un worker cu sef nu poate adauga agenti
	if _, err := f.tm.AddAgent(f.workerCaller(f.rook, 0), quinn()); err == nil {
		t.Error("un worker nu poate adauga agenti")
	}
}

func TestAddAgentLimit(t *testing.T) {
	f := setup(t)
	withTools(t, f, "write")
	// 4 agenti directi (autonomy) + 2 cereri = 6
	autonomous(t, f)
	for i := 0; i < 4; i++ {
		in := quinn()
		in.Name = fmt.Sprintf("A%d", i)
		if _, err := f.tm.AddAgent(f.leadCaller(), in); err != nil {
			t.Fatal(err)
		}
	}
	f.p.Autonomy = false
	f.st.UpdateProject(f.p)
	for i := 0; i < 2; i++ {
		in := quinn()
		in.Name = fmt.Sprintf("R%d", i)
		if _, err := f.tm.AddAgent(f.leadCaller(), in); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.tm.AddAgent(f.leadCaller(), quinn()); err == nil || !strings.Contains(err.Error(), "already have 6 agents") {
		t.Fatalf("a 7-a: %v", err)
	}
	// numele unei cereri deschise e ocupat
	in := quinn()
	in.Name = "r0"
	if _, err := f.tm.AddAgent(f.leadCaller(), in); err == nil || !strings.Contains(err.Error(), "waiting for approval") {
		t.Fatalf("nume cerut: %v", err)
	}
}

func TestApproveAgentRequest(t *testing.T) {
	f := setup(t)
	withTools(t, f, "write")
	task := f.task(t, "Ship it", f.lead, store.StatusInProgress)
	c := f.leadCaller()
	c.TaskID = task.ID
	if _, err := f.tm.AddAgent(c, quinn()); err != nil {
		t.Fatal(err)
	}
	reqs, _ := f.st.ListOpenAgentRequests(f.p.ID)
	if err := f.tm.ApproveAgentRequest(reqs[0].ID); err != nil {
		t.Fatal(err)
	}
	a, err := f.st.FindAgentByName(f.p.ID, "Quinn")
	if err != nil || *a.ParentID != f.lead.ID || *a.CreatedBy != f.lead.ID {
		t.Fatalf("%+v %v", a, err)
	}
	// nota catre lider e eveniment, nu mesaj al utilizatorului (altfel run-ul urmator ar parea pornit din chat)
	msgs, _ := f.st.ListMessages(f.lead.ID)
	last := msgs[len(msgs)-1]
	if !strings.Contains(last.Text, `The user approved your request. Agent "Quinn" added to your team. Give it work with create_task.`) || last.Kind != store.KindEvent || last.Author == store.AuthorUser || last.Delivered {
		t.Fatalf("nota: %+v", last)
	}
	if !hasEvent(t, f, task.ID, "agent_requested", f.lead.Name, `Requested agent "Quinn"`) ||
		!hasEvent(t, f, task.ID, "agent_approved", "You", `Approved agent "Quinn"`) {
		t.Fatal("lipsesc evenimentele din istoric")
	}
	// a doua aprobare (dublu-click)
	if err := f.tm.ApproveAgentRequest(reqs[0].ID); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("a doua aprobare: %v", err)
	}
}

func TestApproveRevalidates(t *testing.T) {
	f := setup(t)
	withTools(t, f, "write")
	if _, err := f.tm.AddAgent(f.leadCaller(), quinn()); err != nil {
		t.Fatal(err)
	}
	reqs, _ := f.st.ListOpenAgentRequests(f.p.ID)
	// intre timp utilizatorul creeaza din canvas un agent cu acelasi nume
	f.agent(t, "Quinn", &f.lead, nil)
	err := f.tm.ApproveAgentRequest(reqs[0].ID)
	var are AgentRequestError
	if !errors.As(err, &are) || !strings.Contains(err.Error(), `"Quinn" already exists`) {
		t.Fatalf("nume luat: %v", err)
	}
	if r, _ := f.st.GetAgentRequest(reqs[0].ID); r.Status != store.AgentRequestOpen {
		t.Fatal("cererea ramane deschisa")
	}
	// liderul si-a pierdut tool-ul cerut (cererea Ivo cere "write", apoi liderul ramane fara tool-uri)
	in := quinn()
	in.Name = "Ivo"
	if _, err := f.tm.AddAgent(f.leadCaller(), in); err != nil {
		t.Fatal(err)
	}
	withTools(t, f)
	reqs, _ = f.st.ListOpenAgentRequests(f.p.ID)
	if err := f.tm.ApproveAgentRequest(reqs[len(reqs)-1].ID); !errors.As(err, &are) || !strings.Contains(err.Error(), "write") {
		t.Fatalf("tool pierdut: %v", err)
	}
}

func TestDismissAgentRequest(t *testing.T) {
	f := setup(t)
	withTools(t, f, "write")
	f.tm.AddAgent(f.leadCaller(), quinn())
	reqs, _ := f.st.ListOpenAgentRequests(f.p.ID)
	if err := f.tm.DismissAgentRequest(reqs[0].ID); err != nil {
		t.Fatal(err)
	}
	if !f.hasMessage(f.lead.ID, `dismissed your request to add "Quinn"`) {
		t.Fatal("liderul afla de respingere")
	}
	if _, err := f.st.FindAgentByName(f.p.ID, "Quinn"); !errors.Is(err, store.ErrNotFound) {
		t.Fatal("respins = fara agent")
	}
}

func TestRemoveAgent(t *testing.T) {
	f := setup(t)
	withTools(t, f, "write")
	autonomous(t, f)
	f.tm.AddAgent(f.leadCaller(), quinn())
	q, _ := f.st.FindAgentByName(f.p.ID, "Quinn")

	if _, err := f.tm.RemoveAgent(f.leadCaller(), "Rook"); err == nil || !strings.Contains(err.Error(), "only remove agents you added") {
		t.Fatalf("Rook: %v", err)
	}
	open := f.task(t, "Test", q, store.StatusInProgress)
	if _, err := f.tm.RemoveAgent(f.leadCaller(), "quinn"); err == nil || !strings.Contains(err.Error(), "still has open tasks: "+open.Ref()) {
		t.Fatalf("cu task deschis: %v", err)
	}
	open.Status = store.StatusDone
	f.st.SaveTask(&open)
	text, err := f.tm.RemoveAgent(f.leadCaller(), "Quinn")
	if err != nil || !strings.Contains(text, "removed from your team") {
		t.Fatalf("%q %v", text, err)
	}
	q, _ = f.st.GetAgent(q.ID)
	if q.ParentID != nil || !q.Detached {
		t.Fatalf("trebuia detasat: %+v", q)
	}
	if n, _ := f.st.CountAddedBy(f.lead.ID); n != 0 {
		t.Fatalf("limita se elibereaza: %d", n)
	}
}

func TestRemoveAgentMovedOrLeading(t *testing.T) {
	f := setup(t)
	withTools(t, f, "write")
	autonomous(t, f)
	for _, n := range []string{"Quinn", "Ivo"} {
		in := quinn()
		in.Name = n
		f.tm.AddAgent(f.leadCaller(), in)
	}
	// utilizatorul muta Quinn sub Rook
	q, _ := f.st.FindAgentByName(f.p.ID, "Quinn")
	q.ParentID = &f.rook.ID
	f.st.UpdateAgent(q)
	if _, err := f.tm.RemoveAgent(f.leadCaller(), "Quinn"); err == nil || !strings.Contains(err.Error(), "not on your team") {
		t.Fatalf("mutat: %v", err)
	}
	// utilizatorul pune un agent sub Ivo: Ivo are echipa proprie
	ivo, _ := f.st.FindAgentByName(f.p.ID, "Ivo")
	f.agent(t, "Kid", &ivo, nil)
	if _, err := f.tm.RemoveAgent(f.leadCaller(), "Ivo"); err == nil || !strings.Contains(err.Error(), "has its own team") {
		t.Fatalf("cu echipa: %v", err)
	}
}

func TestSoloRootAddsAndDelegates(t *testing.T) {
	f := setup(t)
	solo := f.agent(t, "Solo", nil, nil) // a doua radacina, fara echipa
	solo.Tools = []string{"write"}
	f.st.UpdateAgent(solo)
	autonomous(t, f)
	c := mcp.Caller{ProjectID: f.p.ID, AgentID: solo.ID, CanAddAgents: true}
	if _, err := f.tm.AddAgent(c, quinn()); err != nil {
		t.Fatal(err)
	}
	// acelasi Caller (flag-urile de la pornirea run-ului): create_task citeste echipa din DB
	if _, err := f.tm.CreateTask(c, "Write tests", "", "Quinn", ""); err != nil {
		t.Fatalf("create_task pentru agentul nou: %v", err)
	}
}

func TestAddAgentWithStagesSaysSo(t *testing.T) {
	f := setup(t)
	withTools(t, f, "write")
	autonomous(t, f)
	if _, err := f.st.SaveStages(f.p.ID, []store.Stage{{Name: "Build", Kind: "work", AgentIDs: []int64{f.rook.ID}}}); err != nil {
		t.Fatal(err)
	}
	text, err := f.tm.AddAgent(f.leadCaller(), quinn())
	if err != nil || !strings.Contains(text, "add it to a stage") {
		t.Fatalf("%q %v", text, err)
	}
}

func TestApproveWithStagesSaysSo(t *testing.T) {
	f := setup(t)
	withTools(t, f, "write")
	if _, err := f.st.SaveStages(f.p.ID, []store.Stage{{Name: "Build", Kind: "work", AgentIDs: []int64{f.rook.ID}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.tm.AddAgent(f.leadCaller(), quinn()); err != nil {
		t.Fatal(err)
	}
	reqs, _ := f.st.ListOpenAgentRequests(f.p.ID)
	if err := f.tm.ApproveAgentRequest(reqs[0].ID); err != nil {
		t.Fatal(err)
	}
	msgs, _ := f.st.ListMessages(f.lead.ID)
	last := msgs[len(msgs)-1]
	if !strings.Contains(last.Text, "The user approved your request.") || !strings.Contains(last.Text, "add it to a stage") || last.Kind != store.KindEvent {
		t.Fatalf("nota: %+v", last)
	}
}

// replyRun: liderul intreaba (fara task), utilizatorul raspunde sau inchide intrebarea,
// iar run-ul care livreaza raspunsul nu conteaza ca cerere din chat.
func replyRun(t *testing.T, f *fixture, answer bool) runner.Spec {
	t.Helper()
	if _, err := f.tm.AskUser(mcp.Caller{ProjectID: f.p.ID, AgentID: f.lead.ID, RunID: "r"}, "Which DB?", nil); err != nil {
		t.Fatal(err)
	}
	id := openQuestionIDs(t, f)[0]
	var err error
	if answer {
		err = f.tm.AnswerQuestion(id, "Postgres")
	} else {
		err = f.tm.DismissQuestion(id)
	}
	if err != nil {
		t.Fatal(err)
	}
	mustReconcile(t, f)
	return f.fs.last(t, f.lead)
}

func TestAddAgentAfterQuestionReplyNeedsApproval(t *testing.T) {
	for _, answer := range []bool{false, true} {
		f := setup(t)
		withTools(t, f, "write")
		run := replyRun(t, f, answer)
		text, err := f.tm.AddAgent(f.caller(t, run), quinn())
		if err != nil || !strings.Contains(text, "Request sent to the user") {
			t.Fatalf("answer=%v: %q %v", answer, text, err)
		}
		if _, err := f.st.FindAgentByName(f.p.ID, "Quinn"); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("answer=%v: raspunsul la intrebare nu e cerere din chat", answer)
		}
		if reqs, _ := f.st.ListOpenAgentRequests(f.p.ID); len(reqs) != 1 {
			t.Fatalf("answer=%v: %+v", answer, reqs)
		}
		// raspunsul ramane in thread ca mesaj al utilizatorului, livrat
		msgs, _ := f.st.ListMessages(f.lead.ID)
		last := msgs[len(msgs)-1]
		if last.Kind != store.KindMsg || last.Author != store.AuthorUser || !last.Delivered || !last.Reply {
			t.Fatalf("answer=%v: %+v", answer, last)
		}
		f.fs.finish(run.ID, runner.Outcome{Status: "done"})
	}
}
