package team

import (
	"strings"
	"testing"

	"foreman/internal/mcp"
	"foreman/internal/store"
)

func TestStartTaskFromChat(t *testing.T) {
	f := setup(t)
	run := chatRun(t, f, f.lead, "add the discount id to AWBDiscounts")
	c := f.caller(t, run)
	out, err := f.tm.StartTask(c, "Save DISCOUNTID", "on AWB generation", "")
	if err != nil || !strings.Contains(out, "Started T-1") {
		t.Fatalf("%q %v", out, err)
	}
	task := f.wantStatus(t, 1, store.StatusQueued)
	if task.AssigneeID != f.lead.ID || task.CreatorID != nil || !task.ByUser || task.StageID != nil {
		t.Fatalf("%+v", task)
	}
	if !f.hasMessage(f.lead.ID, "Started T-1: Save DISCOUNTID") {
		t.Fatal("lipseste nota din chat")
	}
	evs, _ := f.st.ListTaskEvents(task.ID)
	kinds := []string{}
	for _, e := range evs {
		kinds = append(kinds, e.Kind)
	}
	if strings.Join(kinds, ",") != "requested,created,self_started" {
		t.Fatalf("%v", kinds)
	}
}

func TestStartTaskRules(t *testing.T) {
	f := setup(t)
	// intr-un run pe task: refuzat
	task := f.task(t, "A", f.rook, store.StatusInProgress)
	if _, err := f.tm.StartTask(mcp.Caller{ProjectID: f.p.ID, AgentID: f.rook.ID, TaskID: task.ID}, "x", "", ""); err == nil || !strings.Contains(err.Error(), "already working on T-1") {
		t.Fatalf("%v", err)
	}
	// fara mesaj al utilizatorului (notificare): ByUser fals, decide seful
	c := mcp.Caller{ProjectID: f.p.ID, AgentID: f.rook.ID}
	if _, err := f.tm.StartTask(c, "Fix", "", ""); err != nil {
		t.Fatal(err)
	}
	got := f.wantStatus(t, 2, store.StatusQueued)
	if got.ByUser || got.CreatorID != nil {
		t.Fatalf("%+v", got)
	}
	// titlu gol
	if _, err := f.tm.StartTask(c, "  ", "", ""); err == nil {
		t.Fatal("titlu gol acceptat")
	}
	// limita: max 3 deschise pornite de agent
	f.tm.StartTask(c, "Two", "", "")
	f.tm.StartTask(c, "Three", "", "")
	if _, err := f.tm.StartTask(c, "Four", "", ""); err == nil || !strings.Contains(err.Error(), "3 open tasks") {
		t.Fatalf("%v", err)
	}
}

func TestStartTaskIgnoresStages(t *testing.T) {
	f := setup(t)
	if _, err := f.st.SaveStages(f.p.ID, []store.Stage{{Name: "Build", Kind: "work", AgentIDs: []int64{f.rook.ID}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.tm.StartTask(mcp.Caller{ProjectID: f.p.ID, AgentID: f.lead.ID}, "Mine", "", ""); err != nil {
		t.Fatal(err)
	}
	if got := f.wantStatus(t, 1, store.StatusQueued); got.StageID != nil || got.AssigneeID != f.lead.ID {
		t.Fatalf("%+v", got)
	}
}
