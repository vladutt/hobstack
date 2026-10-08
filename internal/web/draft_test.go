package web

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"foreman/internal/draft"
	"foreman/internal/runner"
	"foreman/internal/store"
)

type draftStarter struct{ specs []runner.Spec }

func (d *draftStarter) Start(s runner.Spec, _ func(runner.Outcome)) (*runner.Run, error) {
	d.specs = append(d.specs, s)
	return nil, nil
}
func (d *draftStarter) Stop(string) error { return nil }

func TestDraftAPI(t *testing.T) {
	e := setup(t)
	st := &draftStarter{}
	e.srv.SetDraft(draft.New(st, "http://127.0.0.1:1", t.TempDir()))
	p := e.createProject(t, t.TempDir())
	path := "/api/projects/" + strconv.FormatInt(p.ID, 10) + "/agents/draft"
	code, raw := e.do(t, "POST", path, map[string]string{"ask": "someone for e-mails", "model": "sonnet"})
	if code != http.StatusCreated {
		t.Fatalf("%d %s", code, raw)
	}
	var d draft.Draft
	json.Unmarshal(raw, &d)
	if len(st.specs) != 1 || st.specs[0].Args[1] == "" {
		t.Fatal("run-ul nu a pornit")
	}
	if code, _ := e.do(t, "POST", path, map[string]string{"ask": "again", "model": "sonnet"}); code != http.StatusConflict {
		t.Fatalf("al doilea draft: %d", code)
	}
	if code, raw := e.do(t, "GET", "/api/drafts/"+d.ID, nil); code != http.StatusOK {
		t.Fatalf("%d %s", code, raw)
	}
	if code, _ := e.do(t, "GET", "/api/drafts/nope", nil); code != http.StatusNotFound {
		t.Fatalf("draft lipsa: %d", code)
	}
	if code, _ := e.do(t, "POST", "/api/drafts/nope/stop", nil); code != http.StatusNotFound {
		t.Fatalf("stop draft lipsa: %d", code)
	}
	if code, _ := e.do(t, "POST", "/api/drafts/"+d.ID+"/stop", nil); code != http.StatusNoContent {
		t.Fatalf("stop: %d", code)
	}
}

// draftBrief trece reviewer-ul fiecarui agent in prompt
func TestDraftBriefReviewer(t *testing.T) {
	rid := int64(2)
	agents := []store.Agent{
		{ID: 1, Name: "Dev", Role: "dev", ReviewerID: &rid},
		{ID: 2, Name: "Rev", Role: "reviewer"},
	}
	got := draftBrief(store.Project{Name: "P"}, agents)
	if !strings.Contains(got, "Dev (dev), reports to the user, reviewed by Rev.") {
		t.Fatalf("lipseste reviewer-ul: %s", got)
	}
	if strings.Count(got, "reviewed by") != 1 {
		t.Fatalf("reviewer pe agent fara reviewer: %s", got)
	}
}
