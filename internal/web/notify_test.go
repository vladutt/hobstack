package web

import (
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"hobstack/internal/store"
	"hobstack/internal/team"
)

func TestProjectNotifySwitch(t *testing.T) {
	e := setup(t)
	p := e.createProject(t, t.TempDir())
	url := "/api/projects/" + strconv.FormatInt(p.ID, 10) + "/notify"
	if code, raw := e.do(t, "PUT", url, map[string]bool{"on": false}); code != http.StatusNoContent {
		t.Fatalf("%d %s", code, raw)
	}
	if got, _ := e.st.GetProject(p.ID); got.Notify {
		t.Fatal("notify a ramas pornit")
	}
	if code, _ := e.do(t, "PUT", "/api/projects/999/notify", map[string]bool{"on": true}); code != http.StatusNotFound {
		t.Fatalf("inexistent: %d", code)
	}
}

func TestAppNotifySettings(t *testing.T) {
	e, _ := appEnv(t)
	if st := appState(t, e); st["notifyDone"] != true || st["notifyNeeds"] != true {
		t.Fatalf("implicit: %v", st)
	}
	if code, raw := e.do(t, "PUT", "/api/app/notify", map[string]bool{"needs": false}); code != http.StatusOK {
		t.Fatalf("%d %s", code, raw)
	}
	if st := appState(t, e); st["notifyDone"] != true || st["notifyNeeds"] != false {
		t.Fatalf("dupa needs=false: %v", st)
	}
	if set := e.srv.NotifySettings(); !set.Done || set.Needs {
		t.Fatalf("NotifySettings: %+v", set)
	}
}

func TestNotifySnapshots(t *testing.T) {
	e := setup(t)
	p := e.createProject(t, t.TempDir())
	w := e.addWorker(t, p.ID, "Atlas")
	q := store.Question{ProjectID: p.ID, AgentID: w.ID, Text: "Which payment provider?"}
	if err := e.st.AddQuestion(&q); err != nil {
		t.Fatal(err)
	}
	if err := e.st.RecordAccessDenial(p.ID, w.ID, nil, "Skill", team.SkillDetail("claude-api"), ""); err != nil {
		t.Fatal(err)
	}
	snaps, err := e.srv.NotifySnapshots()
	if err != nil || len(snaps) != 1 {
		t.Fatalf("%v %+v", err, snaps)
	}
	s := snaps[0]
	if !s.Enabled || s.Name != "ApiUnit" || len(s.Needs) != 2 {
		t.Fatalf("%+v", s)
	}
	if s.Needs[0].Key != "q:"+itoa(q.ID) || s.Needs[0].Line != "Atlas asks: Which payment provider?" {
		t.Fatalf("intrebare: %+v", s.Needs[0])
	}
	if !strings.HasPrefix(s.Needs[1].Key, "a:") || s.Needs[1].Line != `Atlas wanted the skill "claude-api"` {
		t.Fatalf("acces: %+v", s.Needs[1])
	}

	// un task in coada tine proiectul "busy"; pe pauza nu
	task := store.Task{ProjectID: p.ID, Title: "Fix", AssigneeID: w.ID}
	if err := e.st.CreateTask(&task); err != nil {
		t.Fatal(err)
	}
	if snaps, _ = e.srv.NotifySnapshots(); !snaps[0].Busy {
		t.Fatal("task in coada: busy")
	}
	e.st.SetPaused(p.ID, true)
	e.st.SetNotify(p.ID, false)
	if snaps, _ = e.srv.NotifySnapshots(); snaps[0].Busy || snaps[0].Enabled {
		t.Fatalf("pauza / notify oprit: %+v", snaps[0])
	}
}

func TestViewing(t *testing.T) {
	e := setup(t)
	now := time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)
	e.srv.viewing.now = func() time.Time { return now }
	report := func(client string, project int64, focused bool) {
		t.Helper()
		if code, raw := e.do(t, "POST", "/api/app/viewing", map[string]any{"client": client, "project": project, "focused": focused}); code != http.StatusNoContent {
			t.Fatalf("%d %s", code, raw)
		}
	}
	report("a", 3, true)
	if !e.srv.Viewing(3) || e.srv.Viewing(4) {
		t.Fatal("focus pe 3")
	}
	report("a", 3, false)
	if e.srv.Viewing(3) {
		t.Fatal("blur")
	}
	report("b", 3, true)
	now = now.Add(46 * time.Second)
	if e.srv.Viewing(3) {
		t.Fatal("raport expirat")
	}
	if code, _ := e.do(t, "POST", "/api/app/viewing", map[string]any{"project": 3}); code != http.StatusBadRequest {
		t.Fatalf("fara client: %d", code)
	}
}
