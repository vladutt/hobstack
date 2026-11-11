package team

import (
	"strings"
	"testing"

	"hobstack/internal/mcp"
	"hobstack/internal/runner"
	"hobstack/internal/store"
)

// topicWith creeaza un subiect de brainstorming cu agentii dati ca participanti.
func topicWith(t *testing.T, f *fixture, agents ...store.Agent) store.BrainstormTopic {
	t.Helper()
	tp := store.BrainstormTopic{ProjectID: f.p.ID, Name: "Netcode", Description: "How do we sync players?"}
	for _, a := range agents {
		tp.Participants = append(tp.Participants, a.ID)
	}
	if err := f.st.CreateTopic(&tp); err != nil {
		t.Fatal(err)
	}
	return tp
}

// roundFor adauga o runda cu un card waiting pentru agent si il intoarce.
func roundFor(t *testing.T, f *fixture, tp store.BrainstormTopic, a store.Agent) store.BrainstormCard {
	t.Helper()
	r := store.BrainstormRound{TopicID: tp.ID, Text: "Ideas for multiplayer?"}
	cards, err := f.st.AddRound(&r, []int64{a.ID})
	if err != nil {
		t.Fatal(err)
	}
	return cards[0]
}

// startCard porneste run-ul de brainstorming pentru card (ca Reconcile, cu t.work tinut).
func startCard(t *testing.T, f *fixture, a store.Agent, card store.BrainstormCard) runner.Spec {
	t.Helper()
	agents, err := f.st.ListAgents(f.p.ID)
	if err != nil {
		t.Fatal(err)
	}
	a, err = f.st.GetAgent(a.ID)
	if err != nil {
		t.Fatal(err)
	}
	f.tm.work.Lock()
	err = f.tm.brainstormRun(f.p, a, agents, card)
	f.tm.work.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	return f.fs.last(t, a)
}

func cardOf(t *testing.T, f *fixture, id int64) store.BrainstormCard {
	t.Helper()
	c, err := f.st.GetCard(id)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestBrainstormRunIsReadOnlyAndSeparate(t *testing.T) {
	f := setup(t)
	if err := f.st.SetSession(f.rook.ID, "chat-session", "h"); err != nil {
		t.Fatal(err)
	}
	tp := topicWith(t, f, f.rook)
	card := roundFor(t, f, tp, f.rook)
	run := startCard(t, f, f.rook, card)
	if hasFlag(run.Args, "--resume") {
		t.Fatalf("run-ul de brainstorming reia sesiunea de chat: %v", run.Args)
	}
	for _, a := range allowOf(t, run) {
		if strings.HasPrefix(a, "Write") || strings.HasPrefix(a, "Edit") {
			t.Fatalf("%s in allow la un run de brainstorming", a)
		}
	}
	c := f.caller(t, run)
	if c.BrainstormCardID != card.ID || c.TaskID != 0 || c.IsLead {
		t.Fatalf("caller: %+v", c)
	}
	if got := cardOf(t, f, card.ID); got.Status != store.CardRunning || got.RunID != run.ID {
		t.Fatalf("card: %+v", got)
	}
	if !strings.Contains(argValue(run.Args, "-p"), "Netcode") {
		t.Fatal("tura nu are subiectul")
	}
}

func TestBrainstormReplyOnce(t *testing.T) {
	f := setup(t)
	tp := topicWith(t, f, f.rook)
	card := roundFor(t, f, tp, f.rook)
	c := f.caller(t, startCard(t, f, f.rook, card))
	in := mcp.BrainstormReplyInput{Kind: store.CardIdea, Title: "Rollback netcode", Text: "Predict and roll back.", Basis: "net/sync.go"}
	if _, err := f.tm.BrainstormReply(c, in); err != nil {
		t.Fatal(err)
	}
	got := cardOf(t, f, card.ID)
	if got.Status != store.CardDone || got.Kind != store.CardIdea || got.Title != "Rollback netcode" {
		t.Fatalf("card: %+v", got)
	}
	if _, err := f.tm.BrainstormReply(c, in); err == nil || !strings.Contains(err.Error(), "already answered") {
		t.Fatalf("al doilea raspuns: %v", err)
	}
}

func TestBrainstormQuestionLimit(t *testing.T) {
	f := setup(t)
	tp := topicWith(t, f, f.rook)
	card := roundFor(t, f, tp, f.rook)
	for i := 0; i < 2; i++ {
		q := store.BrainstormCard{TopicID: tp.ID, RoundID: card.RoundID, AgentID: f.rook.ID, Status: store.CardDone,
			Kind: store.CardQuestion, Title: "Q?", Options: []string{"a", "b"}}
		if err := f.st.AddCard(&q); err != nil {
			t.Fatal(err)
		}
	}
	c := f.caller(t, startCard(t, f, f.rook, card))
	_, err := f.tm.BrainstormReply(c, mcp.BrainstormReplyInput{Kind: store.CardQuestion, Title: "PvP or co-op?", Text: "Which mode?",
		Options: []string{"PvP", "Co-op"}})
	if err == nil || !strings.Contains(err.Error(), "2 questions") {
		t.Fatalf("a treia intrebare: %v", err)
	}
	if got := cardOf(t, f, card.ID); got.Status != store.CardRunning {
		t.Fatalf("cardul s-a schimbat: %+v", got)
	}
}

func TestBrainstormFallbackToResult(t *testing.T) {
	f := setup(t)
	tp := topicWith(t, f, f.rook)
	card := roundFor(t, f, tp, f.rook)
	before, _ := f.st.ListMessages(f.rook.ID)
	run := startCard(t, f, f.rook, card)
	f.fs.finish(run.ID, runner.Outcome{Status: store.RunDone, Result: "Use rollback netcode\nbecause...", SessionID: "bs-1"})
	got := cardOf(t, f, card.ID)
	if got.Status != store.CardDone || got.Kind != store.CardIdea || got.Title != "Use rollback netcode" {
		t.Fatalf("card: %+v", got)
	}
	if sid, _, _ := f.st.BrainstormSession(tp.ID, f.rook.ID); sid != "bs-1" {
		t.Fatalf("sesiunea de brainstorming: %q", sid)
	}
	if a, _ := f.st.GetAgent(f.rook.ID); a.SessionID != "" {
		t.Fatalf("sesiunea de chat s-a schimbat: %q", a.SessionID)
	}
	if after, _ := f.st.ListMessages(f.rook.ID); len(after) != len(before) {
		t.Fatalf("mesaje noi in chat: %d -> %d", len(before), len(after))
	}

	// urmatorul run pe acelasi subiect reia sesiunea de brainstorming
	r := store.BrainstormRound{TopicID: tp.ID, Text: "More?"}
	cards, err := f.st.AddRound(&r, []int64{f.rook.ID})
	if err != nil {
		t.Fatal(err)
	}
	next := startCard(t, f, f.rook, cards[0])
	if argValue(next.Args, "--resume") != "bs-1" {
		t.Fatalf("resume: %q", argValue(next.Args, "--resume"))
	}
	if !strings.Contains(argValue(next.Args, "-p"), "Use rollback netcode") {
		t.Fatal("tura nu are ideea data deja")
	}
}

func TestBrainstormStoppedRun(t *testing.T) {
	f := setup(t)
	tp := topicWith(t, f, f.rook)
	card := roundFor(t, f, tp, f.rook)
	run := startCard(t, f, f.rook, card)
	f.fs.finish(run.ID, runner.Outcome{Status: store.RunStopped})
	if got := cardOf(t, f, card.ID); got.Status != store.CardStopped {
		t.Fatalf("card: %+v", got)
	}
}

func TestBrainstormFailedRun(t *testing.T) {
	f := setup(t)
	tp := topicWith(t, f, f.rook)
	card := roundFor(t, f, tp, f.rook)
	run := startCard(t, f, f.rook, card)
	f.fs.finish(run.ID, runner.Outcome{Status: store.RunFailed, Error: "boom"})
	if got := cardOf(t, f, card.ID); got.Status != store.CardFailed || got.Error != "boom" {
		t.Fatalf("card: %+v", got)
	}
	if f.hasMessage(f.rook.ID, "Run failed") {
		t.Fatal("nota Run failed in chat")
	}
}

func TestBrainstormAuthFailedWaits(t *testing.T) {
	f := setup(t)
	tp := topicWith(t, f, f.rook)
	card := roundFor(t, f, tp, f.rook)
	run := startCard(t, f, f.rook, card)
	f.fs.finish(run.ID, runner.Outcome{Status: store.RunFailed, Error: "Invalid API key · Please run /login"})
	if got := cardOf(t, f, card.ID); got.Status != store.CardWaiting {
		t.Fatalf("card: %+v", got)
	}
	if p, _ := f.st.GetProject(f.p.ID); !p.Paused {
		t.Fatal("proiectul nu e pe pauza")
	}
}
