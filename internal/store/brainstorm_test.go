package store

import (
	"errors"
	"testing"
)

func newTopic(t *testing.T, s *Store, pid int64, name string, agents ...int64) BrainstormTopic {
	t.Helper()
	tp := BrainstormTopic{ProjectID: pid, Name: name, Participants: agents}
	if err := s.CreateTopic(&tp); err != nil {
		t.Fatal(err)
	}
	return tp
}

func TestBrainstormTopicNameUnique(t *testing.T) {
	s := newStore(t)
	p, lead := newProject(t, s)
	newTopic(t, s, p.ID, "Ideas", lead.ID)
	dup := BrainstormTopic{ProjectID: p.ID, Name: "Ideas"}
	if err := s.CreateTopic(&dup); !errors.Is(err, ErrConflict) {
		t.Fatalf("err = %v", err)
	}
	p2, _ := newProject(t, s)
	newTopic(t, s, p2.ID, "Ideas")
}

func TestBrainstormRoundCards(t *testing.T) {
	s := newStore(t)
	p, lead := newProject(t, s)
	w := newWorker(t, s, p.ID, "Rook")
	tp := newTopic(t, s, p.ID, "Ideas", lead.ID, w.ID)
	r := BrainstormRound{TopicID: tp.ID, Text: "first"}
	cards, err := s.AddRound(&r, tp.Participants)
	if err != nil || len(cards) != 2 {
		t.Fatalf("cards=%v err=%v", cards, err)
	}
	for _, c := range cards {
		if c.Status != CardWaiting || c.RoundID != r.ID {
			t.Fatalf("card: %+v", c)
		}
	}
	got, ok, err := s.NextCard(lead.ID, false)
	if err != nil || !ok || got.AgentID != lead.ID {
		t.Fatalf("next: %+v %v %v", got, ok, err)
	}
	r2 := BrainstormRound{TopicID: tp.ID, Text: "second"}
	cards2, _ := s.AddRound(&r2, []int64{lead.ID})
	if err := s.SetCardPriority(cards2[0].ID, true); err != nil {
		t.Fatal(err)
	}
	got, ok, _ = s.NextCard(lead.ID, true)
	if !ok || got.ID != cards2[0].ID {
		t.Fatalf("priority next: %+v", got)
	}
	got, _, _ = s.NextCard(lead.ID, false)
	if got.ID != cards2[0].ID {
		t.Fatalf("priority first: %+v", got)
	}
	if _, ok, _ := s.NextCard(w.ID, true); ok {
		t.Fatal("w n-are card prioritar")
	}
}

func TestBrainstormAnswerOnce(t *testing.T) {
	s := newStore(t)
	p, lead := newProject(t, s)
	tp := newTopic(t, s, p.ID, "Ideas", lead.ID)
	r := BrainstormRound{TopicID: tp.ID, Text: "x"}
	cards, _ := s.AddRound(&r, tp.Participants)
	q := cards[0]
	if err := s.FinishCard(q.ID, CardQuestion, "Which?", "A or B?", "", []string{"A", "B"}); err != nil {
		t.Fatal(err)
	}
	if err := s.FinishCard(q.ID, CardIdea, "t", "x", "", nil); !errors.Is(err, ErrConflict) {
		t.Fatalf("finish twice: %v", err)
	}
	open, _ := s.OpenQuestions(p.ID)
	if open[tp.ID] != 1 {
		t.Fatalf("open = %v", open)
	}
	if err := s.AnswerCard(q.ID, "Yes"); err != nil {
		t.Fatal(err)
	}
	if err := s.AnswerCard(q.ID, "No"); !errors.Is(err, ErrConflict) {
		t.Fatalf("second answer: %v", err)
	}
	open, _ = s.OpenQuestions(p.ID)
	if open[tp.ID] != 0 {
		t.Fatalf("open = %v", open)
	}
	got, _ := s.GetCard(q.ID)
	if got.Answer != "Yes" || len(got.Options) != 2 {
		t.Fatalf("card: %+v", got)
	}
	if n, _ := s.QuestionsAsked(r.ID, lead.ID); n != 1 {
		t.Fatalf("asked = %d", n)
	}
}

func TestBrainstormSkipOrphans(t *testing.T) {
	s := newStore(t)
	p, lead := newProject(t, s)
	w := newWorker(t, s, p.ID, "Rook")
	tp := newTopic(t, s, p.ID, "Ideas", lead.ID, w.ID)
	r := BrainstormRound{TopicID: tp.ID, Text: "x"}
	cards, _ := s.AddRound(&r, tp.Participants)
	tp.Participants = []int64{lead.ID}
	if err := s.UpdateTopic(tp); err != nil {
		t.Fatal(err)
	}
	n, err := s.SkipOrphanCards(p.ID)
	if err != nil || n != 1 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	for _, c := range cards {
		got, _ := s.GetCard(c.ID)
		want := CardWaiting
		if c.AgentID == w.ID {
			want = CardSkipped
		}
		if got.Status != want {
			t.Fatalf("agent %d: %s, vreau %s", c.AgentID, got.Status, want)
		}
	}
}

func TestBrainstormSessionsResetOnAccount(t *testing.T) {
	s := newStore(t)
	p, lead := newProject(t, s)
	tp := newTopic(t, s, p.ID, "Ideas", lead.ID)
	if err := s.SetBrainstormSession(tp.ID, lead.ID, "sess", "h"); err != nil {
		t.Fatal(err)
	}
	if sid, _, _ := s.BrainstormSession(tp.ID, lead.ID); sid != "sess" {
		t.Fatalf("sid = %q", sid)
	}
	a, _ := s.AddAccount("Work", "tok-1234567890")
	if _, err := s.SetProjectAccount(p.ID, &a.ID, "changed"); err != nil {
		t.Fatal(err)
	}
	if sid, _, _ := s.BrainstormSession(tp.ID, lead.ID); sid != "" {
		t.Fatalf("sid = %q", sid)
	}
}
