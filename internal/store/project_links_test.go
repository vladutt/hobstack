package store

import (
	"errors"
	"strconv"
	"testing"
)

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

// twoProjects: proiectul fixture-ului si inca unul, fiecare cu liderul lui.
func twoProjects(t *testing.T, s *Store) (Project, Agent, Project, Agent) {
	t.Helper()
	p1, l1 := newProject(t, s)
	p2 := Project{Name: "Backend", Goal: "Serve the API", Workdir: t.TempDir()}
	l2, err := s.CreateProject(&p2)
	if err != nil {
		t.Fatal(err)
	}
	return p1, l1, p2, l2
}

func TestProjectLinks(t *testing.T) {
	s := newStore(t)
	p1, _, p2, _ := twoProjects(t, s)
	if err := s.LinkProjects(p1.ID, p1.ID); !errors.Is(err, ErrSameProject) {
		t.Fatalf("acelasi proiect: %v", err)
	}
	// ordinea nu conteaza: (p2, p1) e aceeasi pereche
	if err := s.LinkProjects(p2.ID, p1.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.LinkProjects(p1.ID, p2.ID); !errors.Is(err, ErrConflict) {
		t.Fatalf("dublura: %v", err)
	}
	if ok, _ := s.AreLinked(p1.ID, p2.ID); !ok {
		t.Fatal("trebuie legate")
	}
	l, err := s.LinkedProjects(p1.ID)
	if err != nil || len(l) != 1 || l[0].ID != p2.ID || l[0].Name != "Backend" || l[0].Goal != "Serve the API" {
		t.Fatalf("%+v %v", l, err)
	}
	if l, _ := s.LinkedProjects(p2.ID); len(l) != 1 || l[0].ID != p1.ID {
		t.Fatalf("din celalalt capat: %+v", l)
	}
	if err := s.UnlinkProjects(p1.ID, p2.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.UnlinkProjects(p1.ID, p2.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a doua stergere: %v", err)
	}
	if l, _ := s.LinkedProjects(p1.ID); l == nil || len(l) != 0 {
		t.Fatalf("lista goala non-nil: %#v", l)
	}
}

func TestLinksCascadeOnProjectDelete(t *testing.T) {
	s := newStore(t)
	p1, l1, p2, _ := twoProjects(t, s)
	if err := s.LinkProjects(p1.ID, p2.ID); err != nil {
		t.Fatal(err)
	}
	r := ProjectRequest{Kind: RequestQuestion, FromProject: p1.ID, FromAgent: &l1.ID, ToProject: p2.ID, Text: "?", Status: ReqOpen}
	if err := s.AddProjectRequest(&r); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteProject(p2.ID); err != nil {
		t.Fatal(err)
	}
	if l, _ := s.LinkedProjects(p1.ID); len(l) != 0 {
		t.Fatalf("legatura trebuia stearsa: %+v", l)
	}
	if _, err := s.GetProjectRequest(r.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cererea trebuia stearsa: %v", err)
	}
}

func TestProjectRequestLifecycle(t *testing.T) {
	s := newStore(t)
	p1, l1, p2, _ := twoProjects(t, s)
	q := ProjectRequest{Kind: RequestQuestion, FromProject: p1.ID, FromAgent: &l1.ID, ToProject: p2.ID, Text: "Which format?", Status: ReqOpen}
	if err := s.AddProjectRequest(&q); err != nil {
		t.Fatal(err)
	}
	if q.Ref() != "Q-"+itoa(q.ID) {
		t.Fatalf("ref: %s", q.Ref())
	}
	if open, _ := s.OpenQuestionsFrom(l1.ID); len(open) != 1 || open[0].ID != q.ID {
		t.Fatalf("open = %+v", open)
	}
	if err := s.TransitionProjectRequest(q.ID, ReqOpen, ReqAnswered, "JSON", nil); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetProjectRequest(q.ID)
	if got.Status != ReqAnswered || got.Answer != "JSON" || got.ClosedAt == nil {
		t.Fatalf("%+v", got)
	}
	if err := s.TransitionProjectRequest(q.ID, ReqOpen, ReqClosed, "", nil); !errors.Is(err, ErrConflict) {
		t.Fatalf("din alta stare: %v", err)
	}
	if err := s.TransitionProjectRequest(9999, ReqOpen, ReqClosed, "", nil); !errors.Is(err, ErrNotFound) {
		t.Fatalf("inexistenta: %v", err)
	}
	if open, _ := s.OpenQuestionsFrom(l1.ID); len(open) != 0 {
		t.Fatalf("dupa raspuns = %+v", open)
	}

	w := ProjectRequest{Kind: RequestWork, FromProject: p1.ID, FromAgent: &l1.ID, ToProject: p2.ID, Title: "Add /export", Text: "CSV", Status: ReqPending}
	if err := s.AddProjectRequest(&w); err != nil {
		t.Fatal(err)
	}
	if w.Ref() != "W-"+itoa(w.ID) {
		t.Fatalf("ref: %s", w.Ref())
	}
	if list, _ := s.ListPendingWork(p2.ID); len(list) != 1 || list[0].Title != "Add /export" {
		t.Fatalf("%+v", list)
	}
	if n, _ := s.CountPendingWork(p2.ID); n != 1 {
		t.Fatalf("pending = %d", n)
	}
	task := Task{ProjectID: p2.ID, Title: "Add /export", AssigneeID: l1.ID, RequestID: &w.ID}
	// asignatul trebuie sa fie din proiectul 2; folosim radacina lui
	l2s, _ := s.ListAgents(p2.ID)
	task.AssigneeID = l2s[0].ID
	if err := s.CreateTask(&task); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.GetTask(task.ID); got.RequestID == nil || *got.RequestID != w.ID {
		t.Fatalf("request_id pe task: %+v", got.RequestID)
	}
	if err := s.TransitionProjectRequest(w.ID, ReqPending, ReqApproved, "", &task.ID); err != nil {
		t.Fatal(err)
	}
	got, _ = s.GetProjectRequest(w.ID)
	if got.ToTask == nil || *got.ToTask != task.ID || got.ClosedAt != nil {
		t.Fatalf("aprobata: %+v", got)
	}
	if err := s.TransitionProjectRequest(w.ID, ReqApproved, ReqDone, "Done: /export", nil); err != nil {
		t.Fatal(err)
	}
	got, _ = s.GetProjectRequest(w.ID)
	if got.Status != ReqDone || got.Answer != "Done: /export" || got.ToTask == nil || got.ClosedAt == nil {
		t.Fatalf("livrata: %+v", got)
	}
}

func TestOpenRequestsBetweenAndTo(t *testing.T) {
	s := newStore(t)
	p1, l1, p2, l2 := twoProjects(t, s)
	add := func(kind string, from, to int64, agent int64, status string) ProjectRequest {
		r := ProjectRequest{Kind: kind, FromProject: from, FromAgent: &agent, ToProject: to, Text: "x", Status: status}
		if err := s.AddProjectRequest(&r); err != nil {
			t.Fatal(err)
		}
		return r
	}
	add(RequestQuestion, p1.ID, p2.ID, l1.ID, ReqOpen)
	add(RequestWork, p2.ID, p1.ID, l2.ID, ReqPending)
	add(RequestWork, p1.ID, p2.ID, l1.ID, ReqApproved)
	add(RequestQuestion, p1.ID, p2.ID, l1.ID, ReqAnswered)
	if list, _ := s.OpenRequestsBetween(p2.ID, p1.ID); len(list) != 2 {
		t.Fatalf("intre: %+v", list)
	}
	if list, _ := s.OpenRequestsTo(p2.ID); len(list) != 2 { // open + approved catre p2
		t.Fatalf("catre p2: %+v", list)
	}
}

func TestMessageRequestID(t *testing.T) {
	s := newStore(t)
	p1, l1, p2, _ := twoProjects(t, s)
	r := ProjectRequest{Kind: RequestQuestion, FromProject: p2.ID, ToProject: p1.ID, Text: "?", Status: ReqOpen}
	if err := s.AddProjectRequest(&r); err != nil {
		t.Fatal(err)
	}
	m := Message{ProjectID: p1.ID, AgentID: l1.ID, Kind: KindEvent, Text: "Question", RequestID: &r.ID}
	if err := s.AddMessage(&m); err != nil {
		t.Fatal(err)
	}
	msgs, _ := s.Undelivered(l1.ID)
	if len(msgs) == 0 || msgs[len(msgs)-1].RequestID == nil || *msgs[len(msgs)-1].RequestID != r.ID {
		t.Fatalf("request_id pe mesaj: %+v", msgs)
	}
}
