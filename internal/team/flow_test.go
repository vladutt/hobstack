package team

import (
	"testing"

	"hobstack/internal/runner"
	"hobstack/internal/store"
)

func eventsOfKind(t *testing.T, f *fixture, taskID int64, kind string) []store.TaskEvent {
	t.Helper()
	evs, err := f.st.ListTaskEvents(taskID)
	if err != nil {
		t.Fatal(err)
	}
	var out []store.TaskEvent
	for _, e := range evs {
		if e.Kind == kind {
			out = append(out, e)
		}
	}
	return out
}

func TestRequestedFromChat(t *testing.T) {
	f := setup(t)
	m, err := f.tm.UserMessage(f.lead.ID, "add a share button", nil)
	if err != nil {
		t.Fatal(err)
	}
	mustReconcile(t, f)
	run := f.fs.last(t, f.lead)
	if _, err := f.tm.CreateTask(f.caller(t, run), "Share", "do it", "Rook", ""); err != nil {
		t.Fatal(err)
	}
	task := f.wantStatus(t, 1, store.StatusQueued)
	req := eventsOfKind(t, f, task.ID, "requested")
	if len(req) != 1 {
		t.Fatalf("vreau un requested, am %+v", req)
	}
	r := req[0]
	if r.Actor != "You" || r.To != f.lead.Name || r.Text != "Asked in chat" || r.Body != "add a share button" || !r.At.Equal(m.CreatedAt) {
		t.Fatalf("%+v", r)
	}
	created := eventsOfKind(t, f, task.ID, "created")
	if len(created) != 1 || r.At.After(created[0].At) {
		t.Fatalf("requested trebuie sa fie la ora mesajului, inainte de created: %+v %+v", r, created)
	}
	f.fs.finish(run.ID, runner.Outcome{Status: "done"})
}

func TestNoRequestedFromTaskRun(t *testing.T) {
	f := setup(t)
	wes := f.agent(t, "Wes", &f.rook, nil) // Rook devine sub-lider
	parent, err := f.tm.UserCreateTask(f.p.ID, "Big", "", f.rook.ID)
	if err != nil {
		t.Fatal(err)
	}
	mustReconcile(t, f)
	run := f.fs.last(t, f.rook) // run pe task
	if _, err := f.tm.CreateTask(f.caller(t, run), "Part", "", wes.Name, ""); err != nil {
		t.Fatal(err)
	}
	sub := f.wantStatus(t, 2, store.StatusQueued)
	if sub.ParentTaskID == nil || *sub.ParentTaskID != parent.ID {
		t.Fatalf("sub-task: %+v", sub)
	}
	if req := eventsOfKind(t, f, sub.ID, "requested"); len(req) != 0 {
		t.Fatalf("un run pe task nu are requested: %+v", req)
	}
	f.fs.finish(run.ID, runner.Outcome{Status: "done"})
}

func TestRequestedOnlyWithUserMessage(t *testing.T) {
	f := setup(t)
	// doar o nota (nu mesaj al utilizatorului) in coada liderului
	if err := f.tm.note(f.p.ID, f.lead.ID, "Rook finished T-9", false); err != nil {
		t.Fatal(err)
	}
	mustReconcile(t, f)
	run := f.fs.last(t, f.lead)
	if _, err := f.tm.CreateTask(f.caller(t, run), "Follow-up", "", "Rook", ""); err != nil {
		t.Fatal(err)
	}
	task := f.wantStatus(t, 1, store.StatusQueued)
	if req := eventsOfKind(t, f, task.ID, "requested"); len(req) != 0 {
		t.Fatalf("fara mesaj al utilizatorului nu exista requested: %+v", req)
	}
	f.fs.finish(run.ID, runner.Outcome{Status: "done"})
}

func TestNoRequestedFromQuestionReply(t *testing.T) {
	f := setup(t)
	run := replyRun(t, f, true)
	if _, err := f.tm.CreateTask(f.caller(t, run), "Share", "do it", "Rook", ""); err != nil {
		t.Fatal(err)
	}
	task := f.wantStatus(t, 1, store.StatusQueued)
	if req := eventsOfKind(t, f, task.ID, "requested"); len(req) != 0 {
		t.Fatalf("raspunsul la intrebare nu e cerere din chat: %+v", req)
	}
	f.fs.finish(run.ID, runner.Outcome{Status: "done"})
}
