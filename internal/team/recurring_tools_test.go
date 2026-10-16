package team

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"hobstack/internal/mcp"
	"hobstack/internal/store"
)

// chatCaller: identitatea MCP a unui run al agentului pornit dintr-un mesaj al utilizatorului (ce verifica requestOf)
func chatCaller(f *fixture, a store.Agent) mcp.Caller {
	runID := fmt.Sprintf("chat-%d", a.ID)
	f.tm.mu.Lock()
	f.tm.active[a.ID] = activeRun{runID: runID, projectID: a.ProjectID,
		request: &store.Message{ID: 1, ProjectID: a.ProjectID, AgentID: a.ID, Author: "user", Text: "please"}}
	f.tm.mu.Unlock()
	return mcp.Caller{ProjectID: a.ProjectID, AgentID: a.ID, RunID: runID, CanAddAgents: true}
}

func ptrOf[T any](v T) *T { return &v }

var (
	dailyAt9 = mcp.RecurringSchedule{Kind: "daily", At: "09:00"}
	mwf9     = mcp.RecurringSchedule{Kind: "weekly", At: "09:00", Weekdays: []string{"mon", "Wednesday", "FRI"}}
)

func onlyRecurring(t *testing.T, f *fixture) store.RecurringTask {
	t.Helper()
	list, err := f.st.ListRecurring(f.p.ID)
	if err != nil || len(list) != 1 {
		t.Fatalf("%+v %v", list, err)
	}
	return list[0]
}

func TestRecurringToolsNeedChat(t *testing.T) {
	f := setup(t)
	autonomous(t, f) // Full autonomy nu ajunge
	c := mcp.Caller{ProjectID: f.p.ID, AgentID: f.lead.ID, RunID: "notification", IsLead: true, CanAddAgents: true}
	if _, err := f.tm.CreateRecurringFor(c, mcp.RecurringArgs{Title: "Tests", Schedule: dailyAt9}); !errors.Is(err, errNotAsked) {
		t.Fatalf("create: %v", err)
	}
	if _, err := f.tm.UpdateRecurringFor(c, mcp.RecurringChange{ID: "R-1", Title: ptrOf("x")}); !errors.Is(err, errNotAsked) {
		t.Fatalf("update: %v", err)
	}
	if _, err := f.tm.DeleteRecurringFor(c, "R-1"); !errors.Is(err, errNotAsked) {
		t.Fatalf("delete: %v", err)
	}
	if text, err := f.tm.ListRecurringFor(c); err != nil || text != "No recurring tasks in this project." {
		t.Fatalf("list: %q %v", text, err)
	}
	if list, _ := f.st.ListRecurring(f.p.ID); len(list) != 0 {
		t.Fatalf("nothing may be created: %+v", list)
	}
}

func TestCreateRecurringFromChat(t *testing.T) {
	f := setup(t)
	c := chatCaller(f, f.lead)
	text, err := f.tm.CreateRecurringFor(c, mcp.RecurringArgs{Title: "  Tests ", Description: "run them", Assignee: "rook", Schedule: mwf9, AutoAccept: true})
	if err != nil {
		t.Fatal(err)
	}
	r := onlyRecurring(t, f)
	if r.Title != "Tests" || r.AssigneeID == nil || *r.AssigneeID != f.rook.ID || r.Schedule.Weekdays != 1<<1|1<<3|1<<5 ||
		!r.AutoAccept || !r.Enabled || r.CreatedBy == nil || *r.CreatedBy != f.lead.ID {
		t.Fatalf("%+v", r)
	}
	if !strings.HasPrefix(text, fmt.Sprintf(`Created R-%d "Tests" — Mon, Wed, Fri at 09:00 — Rook — on — next `, r.ID)) ||
		!strings.Contains(text, "auto-accept") || !strings.Contains(text, "created by "+f.lead.Name) {
		t.Fatalf("%q", text)
	}
	// fara assignee: liderul insusi
	if _, err := f.tm.CreateRecurringFor(c, mcp.RecurringArgs{Title: "Mine", Schedule: dailyAt9}); err != nil {
		t.Fatal(err)
	}
	list, _ := f.st.ListRecurring(f.p.ID)
	if *list[1].AssigneeID != f.lead.ID {
		t.Fatalf("default assignee: %+v", list[1])
	}
	var re RecurringError
	if _, err := f.tm.CreateRecurringFor(c, mcp.RecurringArgs{Title: "x", Schedule: mcp.RecurringSchedule{Kind: "weekly", At: "09:00", Weekdays: []string{"funday"}}}); !errors.As(err, &re) || !strings.Contains(err.Error(), "unknown day") {
		t.Fatalf("bad day: %v", err)
	}
	if _, err := f.tm.CreateRecurringFor(c, mcp.RecurringArgs{Title: "x", Schedule: mcp.RecurringSchedule{Kind: "daily", At: "25:00"}}); !errors.As(err, &re) {
		t.Fatalf("bad time: %v", err)
	}
	if _, err := f.tm.CreateRecurringFor(c, mcp.RecurringArgs{Title: "x", Assignee: "Nobody", Schedule: dailyAt9}); err == nil || !strings.Contains(err.Error(), `no agent named "Nobody"`) {
		t.Fatalf("unknown agent: %v", err)
	}
}

func TestRecurringZone(t *testing.T) {
	f := setup(t)
	sub := f.agent(t, "Sub", &f.lead, nil)
	f.agent(t, "Kid", &sub, nil)
	cs := chatCaller(f, sub)
	if _, err := f.tm.CreateRecurringFor(cs, mcp.RecurringArgs{Title: "Kid's", Assignee: "kid", Schedule: dailyAt9}); err != nil {
		t.Fatalf("own team: %v", err)
	}
	for _, who := range []string{"Rook", f.lead.Name, "first stage"} {
		if _, err := f.tm.CreateRecurringFor(cs, mcp.RecurringArgs{Title: "x", Assignee: who, Schedule: dailyAt9}); err == nil || !strings.Contains(err.Error(), "direct team") {
			t.Fatalf("%s: %v", who, err)
		}
	}
	// sarcina echipei radacinii (Rook): sub-liderul nu o poate schimba sau sterge
	cl := chatCaller(f, f.lead)
	if _, err := f.tm.CreateRecurringFor(cl, mcp.RecurringArgs{Title: "Rook's", Assignee: "Rook", Schedule: dailyAt9}); err != nil {
		t.Fatal(err)
	}
	list, _ := f.st.ListRecurring(f.p.ID)
	ref := fmt.Sprintf("R-%d", list[1].ID)
	if _, err := f.tm.UpdateRecurringFor(cs, mcp.RecurringChange{ID: ref, Title: ptrOf("x")}); err == nil || !strings.Contains(err.Error(), "belongs to another team") {
		t.Fatalf("update: %v", err)
	}
	if _, err := f.tm.DeleteRecurringFor(cs, ref); err == nil || !strings.Contains(err.Error(), "belongs to another team") {
		t.Fatalf("delete: %v", err)
	}
	// nici mutata in echipa lui
	if _, err := f.tm.UpdateRecurringFor(cl, mcp.RecurringChange{ID: ref, Assignee: ptrOf("Kid")}); err == nil || !strings.Contains(err.Error(), "direct team") {
		t.Fatalf("root moves to a grandchild: %v", err)
	}
	// un worker (nici lider, nici radacina) e refuzat chiar din chat
	if _, err := f.tm.CreateRecurringFor(chatCaller(f, f.rook), mcp.RecurringArgs{Title: "x", Schedule: dailyAt9}); err == nil || !strings.Contains(err.Error(), "only a lead") {
		t.Fatalf("worker: %v", err)
	}
}

func TestRecurringFirstStage(t *testing.T) {
	f := setup(t)
	if _, err := f.st.SaveStages(f.p.ID, []store.Stage{{Name: "Dev", Kind: store.KindWork, AgentIDs: []int64{f.rook.ID}}}); err != nil {
		t.Fatal(err)
	}
	c := chatCaller(f, f.lead)
	if _, err := f.tm.CreateRecurringFor(c, mcp.RecurringArgs{Title: "Staged", Assignee: "First stage", Schedule: dailyAt9}); err != nil {
		t.Fatal(err)
	}
	if r := onlyRecurring(t, f); r.AssigneeID != nil {
		t.Fatalf("first stage = no assignee: %+v", r)
	}
	if text, _ := f.tm.ListRecurringFor(c); !strings.Contains(text, "— first stage — on — next ") {
		t.Fatalf("%q", text)
	}
}

func TestUpdateRecurringFromChat(t *testing.T) {
	f := setup(t)
	f.tm.now = func() time.Time { return at("2026-10-05 10:00") }
	c := chatCaller(f, f.lead)
	if _, err := f.tm.CreateRecurringFor(c, mcp.RecurringArgs{Title: "Tests", Description: "d", Assignee: "Rook", Schedule: dailyAt9, AutoAccept: true}); err != nil {
		t.Fatal(err)
	}
	id := onlyRecurring(t, f).ID
	ref := fmt.Sprintf("R-%d", id)
	if _, err := f.tm.UpdateRecurringFor(c, mcp.RecurringChange{ID: ref}); err == nil || !strings.Contains(err.Error(), "nothing to change") {
		t.Fatalf("empty update: %v", err)
	}
	text, err := f.tm.UpdateRecurringFor(c, mcp.RecurringChange{ID: ref, Schedule: &mcp.RecurringSchedule{Kind: "daily", At: "10:00"}})
	if err != nil || !strings.HasPrefix(text, fmt.Sprintf(`Updated R-%d "Tests" — Daily at 10:00`, id)) {
		t.Fatalf("%q %v", text, err)
	}
	r, _ := f.st.GetRecurring(id)
	if r.Title != "Tests" || r.Description != "d" || !r.AutoAccept || *r.AssigneeID != f.rook.ID || !r.NextRunAt.Equal(at("2026-10-06 10:00")) {
		t.Fatalf("partial update kept the rest: %+v", r)
	}
	// id fara prefix, pauza si reluare
	if _, err := f.tm.UpdateRecurringFor(c, mcp.RecurringChange{ID: fmt.Sprint(id), Enabled: ptrOf(false)}); err != nil {
		t.Fatal(err)
	}
	if text, _ := f.tm.ListRecurringFor(c); !strings.Contains(text, "— paused") {
		t.Fatalf("%q", text)
	}
	if _, err := f.tm.UpdateRecurringFor(c, mcp.RecurringChange{ID: ref, Enabled: ptrOf(true)}); err != nil {
		t.Fatal(err)
	}
	if r, _ := f.st.GetRecurring(id); !r.Enabled {
		t.Fatal("resumed")
	}
	text, err = f.tm.DeleteRecurringFor(c, ref)
	if err != nil || text != fmt.Sprintf(`Deleted R-%d "Tests". Tasks it already created stay on the board.`, id) {
		t.Fatalf("%q %v", text, err)
	}
	if _, err := f.st.GetRecurring(id); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("after delete: %v", err)
	}
}

func TestRecurringOtherProject(t *testing.T) {
	f := setup(t)
	other := store.Project{Name: "Other", Workdir: t.TempDir(), MaxParallel: 1}
	otherLead, err := f.st.CreateProject(&other)
	if err != nil {
		t.Fatal(err)
	}
	r, err := f.tm.CreateRecurring(other.ID, RecurringInput{Title: "Theirs", AssigneeID: otherLead.ID, Schedule: daily9, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	c := chatCaller(f, f.lead)
	for _, ref := range []string{fmt.Sprintf("R-%d", r.ID), "R-9999", "abc"} {
		if _, err := f.tm.DeleteRecurringFor(c, ref); err == nil || !strings.Contains(err.Error(), "list_recurring") {
			t.Fatalf("%s: %v", ref, err)
		}
	}
	if _, err := f.st.GetRecurring(r.ID); err != nil {
		t.Fatal("the other project's task must stay")
	}
}

func TestUpdateRecurringBlankAssigneeKeepsIt(t *testing.T) {
	f := setup(t)
	c := chatCaller(f, f.lead)
	if _, err := f.tm.CreateRecurringFor(c, mcp.RecurringArgs{Title: "Tests", Assignee: "Rook", Schedule: dailyAt9}); err != nil {
		t.Fatal(err)
	}
	id := onlyRecurring(t, f).ID
	// assignee gol la update = neschimbat (gol inseamna "eu" doar la creare)
	if _, err := f.tm.UpdateRecurringFor(c, mcp.RecurringChange{ID: fmt.Sprint(id), Assignee: ptrOf("  "), Enabled: ptrOf(false)}); err != nil {
		t.Fatal(err)
	}
	if r, _ := f.st.GetRecurring(id); *r.AssigneeID != f.rook.ID || r.Enabled {
		t.Fatalf("%+v", r)
	}
}
