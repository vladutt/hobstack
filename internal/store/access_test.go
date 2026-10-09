package store

import (
	"database/sql"
	"errors"
	"testing"
)

func TestAutonomyAndEscalatedPersist(t *testing.T) {
	s := newStore(t)
	p, _ := newProject(t, s)
	if p.Autonomy {
		t.Fatal("autonomy trebuie sa fie false implicit")
	}
	p.Autonomy = true
	if err := s.UpdateProject(p); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetProject(p.ID)
	if err != nil || !got.Autonomy {
		t.Fatalf("autonomy dupa update: %+v %v", got, err)
	}
	// si la creare
	np := Project{Name: "A", Workdir: t.TempDir(), Autonomy: true}
	if _, err := s.CreateProject(&np); err != nil {
		t.Fatal(err)
	}
	if g, _ := s.GetProject(np.ID); !g.Autonomy {
		t.Fatal("autonomy pierdut la create")
	}

	w := newWorker(t, s, p.ID, "Rook")
	task := Task{ProjectID: p.ID, Title: "t", AssigneeID: w.ID}
	if err := s.CreateTask(&task); err != nil {
		t.Fatal(err)
	}
	task.Escalated = true
	if err := s.SaveTask(&task); err != nil {
		t.Fatal(err)
	}
	if g, _ := s.GetTask(task.ID); !g.Escalated {
		t.Fatal("escalated nesalvat")
	}
}

func TestAccessDenialInsertMergeDismiss(t *testing.T) {
	s := newStore(t)
	p, _ := newProject(t, s)
	w := newWorker(t, s, p.ID, "Rook")
	t1 := Task{ProjectID: p.ID, Title: "t1", AssigneeID: w.ID}
	s.CreateTask(&t1)

	if l, err := s.ListOpenAccessRequests(p.ID); err != nil || l == nil || len(l) != 0 {
		t.Fatalf("lista goala trebuie non-nil: %v %v", l, err)
	}
	if err := s.RecordAccessDenial(p.ID, w.ID, &t1.ID, "Bash", "npm test", ""); err != nil {
		t.Fatal(err)
	}
	// refuzurile pe acelasi task se comaseaza, detail = ultimul (alt task / chat = alt rand, vezi TestAccessRequestsKeptPerTask)
	if err := s.RecordAccessDenial(p.ID, w.ID, &t1.ID, "Bash", "npm build", ""); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordAccessDenial(p.ID, w.ID, &t1.ID, "Bash", "npm lint", ""); err != nil {
		t.Fatal(err)
	}
	// alt tool = rand nou
	if err := s.RecordAccessDenial(p.ID, w.ID, nil, "WebFetch", "http://x", ""); err != nil {
		t.Fatal(err)
	}
	l, err := s.ListOpenAccessRequests(p.ID)
	if err != nil || len(l) != 2 {
		t.Fatalf("lista: %+v %v", l, err)
	}
	r := l[0]
	if r.Tool != "Bash" || r.Count != 3 || r.Detail != "npm lint" || r.TaskID == nil || *r.TaskID != t1.ID ||
		r.ProjectID != p.ID || r.AgentID != w.ID || r.CreatedAt.IsZero() || r.UpdatedAt.Before(r.CreatedAt) {
		t.Fatalf("rand comasat: %+v", r)
	}
	if l[1].Tool != "WebFetch" || l[1].Count != 1 || l[1].TaskID != nil {
		t.Fatalf("al doilea rand: %+v", l[1])
	}
	if n, _ := s.CountOpenAccessRequests(p.ID); n != 2 {
		t.Fatalf("count = %d", n)
	}
	got, err := s.GetAccessRequest(r.ID)
	if err != nil || got.ID != r.ID || got.Count != 3 {
		t.Fatalf("get: %+v %v", got, err)
	}
	if _, err := s.GetAccessRequest(9999); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("lipsa trebuie sa dea sql.ErrNoRows: %v", err)
	}

	// dupa dismiss: iese din lista/count, iar un refuz nou face rand nou
	if err := s.DismissAccessRequest(r.ID); err != nil {
		t.Fatal(err)
	}
	if n, _ := s.CountOpenAccessRequests(p.ID); n != 1 {
		t.Fatalf("count dupa dismiss = %d", n)
	}
	if err := s.RecordAccessDenial(p.ID, w.ID, nil, "Bash", "ls", ""); err != nil {
		t.Fatal(err)
	}
	l, _ = s.ListOpenAccessRequests(p.ID)
	if len(l) != 2 || l[1].Tool != "Bash" || l[1].Count != 1 || l[1].ID == r.ID {
		t.Fatalf("rand nou dupa dismiss: %+v", l)
	}
}

func TestAccessRequestCascadeAndSetNull(t *testing.T) {
	s := newStore(t)
	p, _ := newProject(t, s)
	w := newWorker(t, s, p.ID, "Rook")
	task := Task{ProjectID: p.ID, Title: "t", AssigneeID: w.ID}
	s.CreateTask(&task)
	s.RecordAccessDenial(p.ID, w.ID, &task.ID, "Bash", "x", "")

	// stergerea task-ului pastreaza cererea, cu task_id NULL
	if _, err := s.db.Exec(`DELETE FROM tasks WHERE id = ?`, task.ID); err != nil {
		t.Fatal(err)
	}
	l, _ := s.ListOpenAccessRequests(p.ID)
	if len(l) != 1 || l[0].TaskID != nil {
		t.Fatalf("dupa stergerea task-ului: %+v", l)
	}

	// stergerea agentului sterge cererea
	if _, err := s.db.Exec(`DELETE FROM agents WHERE id = ?`, w.ID); err != nil {
		t.Fatal(err)
	}
	if n, _ := s.CountOpenAccessRequests(p.ID); n != 0 {
		t.Fatalf("dupa stergerea agentului: %d", n)
	}
}

// cererile se comaseaza doar pe acelasi task: altfel aprobarea ar ajunge pe alt task (sau in chat)
func TestAccessRequestsKeptPerTask(t *testing.T) {
	s := newStore(t)
	p, lead := newProject(t, s)
	t1 := Task{ProjectID: p.ID, Title: "a", AssigneeID: lead.ID}
	t2 := Task{ProjectID: p.ID, Title: "b", AssigneeID: lead.ID}
	s.CreateTask(&t1)
	s.CreateTask(&t2)
	s.RecordCommandRequest(p.ID, lead.ID, &t1.ID, "git stash", "git stash")
	s.RecordCommandRequest(p.ID, lead.ID, &t2.ID, "git stash", "git stash")
	s.RecordCommandRequest(p.ID, lead.ID, nil, "git stash", "git stash")
	s.RecordCommandRequest(p.ID, lead.ID, &t1.ID, "git stash pop", "git stash")
	reqs, _ := s.ListOpenAccessRequests(p.ID)
	if len(reqs) != 3 || *reqs[0].TaskID != t1.ID || reqs[0].Count != 2 || *reqs[1].TaskID != t2.ID || reqs[2].TaskID != nil {
		t.Fatalf("%+v", reqs)
	}
	if ok, _ := s.HasOpenAccessRequest(lead.ID, t1.ID); !ok {
		t.Fatal("T-1 si-a pierdut cererea")
	}
}

// Stop team / restart nu repun in coada un task care asteapta o aprobare
func TestRequeueSkipsTasksWaitingForAccess(t *testing.T) {
	s := newStore(t)
	p, lead := newProject(t, s)
	waiting := Task{ProjectID: p.ID, Title: "a", AssigneeID: lead.ID}
	free := Task{ProjectID: p.ID, Title: "b", AssigneeID: lead.ID}
	for _, tk := range []*Task{&waiting, &free} {
		s.CreateTask(tk) // CreateTask pune mereu Queued
		tk.Status = StatusInProgress
		s.SaveTask(tk)
	}
	s.RecordCommandRequest(p.ID, lead.ID, &waiting.ID, "git stash", "git stash")
	ids, err := s.RequeueInProgress(p.ID)
	if err != nil || len(ids) != 1 || ids[0] != free.ID {
		t.Fatalf("%v %v", ids, err)
	}
	if ids, _ := s.RequeueAllInProgress(); len(ids) != 0 {
		t.Fatalf("restart: %v", ids)
	}
}
