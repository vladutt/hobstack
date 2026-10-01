package store

import "testing"

func TestStartNewChat(t *testing.T) {
	s := newStore(t)
	p, _ := newProject(t, s)
	w := newWorker(t, s, p.ID, "Rook")
	if err := s.SetSession(w.ID, "sess-1", "hash-1"); err != nil {
		t.Fatal(err)
	}
	add := func(text string, delivered bool) Message {
		m := Message{ProjectID: p.ID, AgentID: w.ID, Kind: KindMsg, Author: AuthorUser, Text: text, Delivered: delivered}
		if err := s.AddMessage(&m); err != nil {
			t.Fatal(err)
		}
		return m
	}
	add("vechi 1", true)
	add("vechi 2", true)
	pending := add("nelivrat", false)

	// fara New chat: tot thread-ul, nimic ascuns
	msgs, earlier, err := s.ListChat(w.ID)
	if err != nil || len(msgs) != 3 || earlier != 0 {
		t.Fatalf("inainte: %d mesaje, %d ascunse, %v", len(msgs), earlier, err)
	}

	note, err := s.StartNewChat(w.ID, "New chat started.")
	if err != nil {
		t.Fatal(err)
	}
	if note.Kind != KindEvent || note.Text != "New chat started." || !note.Delivered {
		t.Fatalf("nota: %+v", note)
	}
	a, _ := s.GetAgent(w.ID)
	if a.SessionID != "" || a.PromptHash != "" {
		t.Fatalf("sesiunea trebuia stearsa: %q %q", a.SessionID, a.PromptHash)
	}
	add("nou", true)

	// dupa: mesajul nelivrat ramane vizibil, cele vechi livrate sunt ascunse
	msgs, earlier, err = s.ListChat(w.ID)
	if err != nil || earlier != 2 || len(msgs) != 3 {
		t.Fatalf("dupa: %d mesaje, %d ascunse, %v", len(msgs), earlier, err)
	}
	if msgs[0].ID != pending.ID || msgs[1].ID != note.ID || msgs[2].Text != "nou" {
		t.Fatalf("ordinea: %+v", msgs)
	}
	// istoricul complet ramane salvat
	if all, _ := s.ListMessages(w.ID); len(all) != 5 {
		t.Fatalf("istoric complet: %d", len(all))
	}
	if _, err := s.StartNewChat(999999, "x"); err == nil {
		t.Fatal("agent inexistent: vreau eroare")
	}
}
