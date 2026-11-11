package team

import (
	"database/sql"
	"errors"
	"fmt"
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

// Fix: un card cu contul picat devine Failed (cu Retry); pe pauza nu se reia la nesfarsit.
func TestBrainstormAuthFailedNoLoop(t *testing.T) {
	f := setup(t)
	tp := topicWith(t, f, f.rook)
	card := roundFor(t, f, tp, f.rook)
	run := startCard(t, f, f.rook, card)
	f.fs.finish(run.ID, runner.Outcome{Status: store.RunFailed, Error: "Invalid API key · Please run /login"})
	if got := cardOf(t, f, card.ID); got.Status != store.CardFailed || !strings.Contains(got.Error, "Invalid API key") {
		t.Fatalf("card: %+v", got)
	}
	if p, _ := f.st.GetProject(f.p.ID); !p.Paused {
		t.Fatal("proiectul nu e pe pauza")
	}
	n := f.fs.count()
	mustReconcile(t, f)
	if f.fs.count() != n {
		t.Fatal("cardul a pornit din nou dupa eroarea de cont")
	}
}

// Fix: Stop team nu opreste brainstorming-ul (merge pe pauza, ca chat-ul).
func TestBrainstormSurvivesStopTeam(t *testing.T) {
	f := setup(t)
	tp := topicWith(t, f, f.rook)
	card := roundFor(t, f, tp, f.rook)
	mustReconcile(t, f)
	if !cardRunOf(t, f, f.fs.last(t, f.rook), card.ID) {
		t.Fatal("cardul nu a pornit")
	}
	if err := f.tm.Stop(f.p.ID); err != nil {
		t.Fatal(err)
	}
	if len(f.fs.stopped) != 0 {
		t.Fatalf("run oprit: %v", f.fs.stopped)
	}
	if got := cardOf(t, f, card.ID); got.Status != store.CardRunning {
		t.Fatalf("card: %s", got.Status)
	}
}

// Fix: pe pauza, o nota nelivrata de la alt agent nu blocheaza cardurile.
func TestBrainstormPausedWithAgentNote(t *testing.T) {
	f := setup(t)
	if err := f.st.SetPaused(f.p.ID, true); err != nil {
		t.Fatal(err)
	}
	if err := f.tm.note(f.p.ID, f.rook.ID, "Vera finished T-1.", false); err != nil {
		t.Fatal(err)
	}
	tp := topicWith(t, f, f.rook)
	card := roundFor(t, f, tp, f.rook)
	mustReconcile(t, f)
	if got := cardOf(t, f, card.ID); got.Status != store.CardRunning {
		t.Fatalf("card: %s", got.Status)
	}
}

// Fix: pe un subiect arhivat cardurile nu pornesc si actiunile care pun carduri noi sunt refuzate.
func TestBrainstormArchivedTopic(t *testing.T) {
	f := setup(t)
	tp := topicWith(t, f, f.rook)
	card := roundFor(t, f, tp, f.rook)
	idea := pickedIdea(t, f, card, f.rook, store.CardIdea, "Rollback netcode")
	failed := store.BrainstormCard{TopicID: tp.ID, RoundID: card.RoundID, AgentID: f.rook.ID, Status: store.CardFailed}
	if err := f.st.AddCard(&failed); err != nil {
		t.Fatal(err)
	}
	tp.Archived = true
	if err := f.st.UpdateTopic(tp); err != nil {
		t.Fatal(err)
	}
	mustReconcile(t, f)
	if got := cardOf(t, f, card.ID); got.Status != store.CardWaiting {
		t.Fatalf("card: %s", got.Status)
	}
	if _, ok := f.fs.lastOK(f.rook); ok {
		t.Fatal("run pe un subiect arhivat")
	}
	for name, fn := range map[string]func() error{
		"Another idea": func() error { return f.tm.AnotherIdea(idea.ID) },
		"Run now":      func() error { return f.tm.RunNow(card.ID) },
		"Retry":        func() error { return f.tm.RetryCard(failed.ID) },
	} {
		var cse CardStateError
		if err := fn(); !errors.As(err, &cse) || !strings.Contains(err.Error(), "archived") {
			t.Fatalf("%s: %v", name, err)
		}
	}
}

// cardRunOf: true daca run-ul e cel de brainstorming al cardului.
func cardRunOf(t *testing.T, f *fixture, run runner.Spec, cardID int64) bool {
	t.Helper()
	return f.caller(t, run).BrainstormCardID == cardID
}

// Un card asteapta cat agentul are un task in coada; dupa task, Reconcile porneste cardul.
func TestBrainstormWaitsForQueuedTask(t *testing.T) {
	f := setup(t)
	task := f.task(t, "Build page", f.rook, store.StatusQueued)
	tp := topicWith(t, f, f.rook)
	card := roundFor(t, f, tp, f.rook)
	mustReconcile(t, f)
	run := f.fs.last(t, f.rook)
	if c := f.caller(t, run); c.BrainstormCardID != 0 || c.TaskID != task.ID {
		t.Fatalf("primul run: %+v", c)
	}
	if got := cardOf(t, f, card.ID); got.Status != store.CardWaiting {
		t.Fatalf("card: %s", got.Status)
	}
	f.fs.finish(run.ID, runner.Outcome{Status: store.RunDone, Result: "Done."})
	f.wantStatus(t, task.Number, store.StatusReview)
	mustReconcile(t, f)
	if next := f.fs.last(t, f.rook); !cardRunOf(t, f, next, card.ID) {
		t.Fatal("cardul nu a pornit dupa task")
	}
	if got := cardOf(t, f, card.ID); got.Status != store.CardRunning {
		t.Fatalf("card: %s", got.Status)
	}
}

// Run now: cardul porneste inaintea urmatorului task din coada, dupa run-ul in curs.
func TestBrainstormRunNowBeforeQueue(t *testing.T) {
	f := setup(t)
	f.task(t, "Build page", f.rook, store.StatusQueued)
	second := f.task(t, "Write docs", f.rook, store.StatusQueued)
	mustReconcile(t, f)
	run := f.fs.last(t, f.rook)
	tp := topicWith(t, f, f.rook)
	card := roundFor(t, f, tp, f.rook)
	if err := f.tm.RunNow(card.ID); err != nil {
		t.Fatal(err)
	}
	f.fs.finish(run.ID, runner.Outcome{Status: store.RunDone, Result: "Done."})
	mustReconcile(t, f)
	if next := f.fs.last(t, f.rook); !cardRunOf(t, f, next, card.ID) {
		t.Fatal("urmatorul run nu e al cardului")
	}
	f.wantStatus(t, second.Number, store.StatusQueued)
	if err := f.tm.RunNow(card.ID); err == nil {
		t.Fatal("Run now pe un card care ruleaza")
	}
}

// Pe pauza brainstorming-ul merge, ca si chat-ul.
func TestBrainstormOnPausedTeam(t *testing.T) {
	f := setup(t)
	if err := f.st.SetPaused(f.p.ID, true); err != nil {
		t.Fatal(err)
	}
	f.task(t, "Build page", f.rook, store.StatusQueued)
	tp := topicWith(t, f, f.rook)
	card := roundFor(t, f, tp, f.rook)
	mustReconcile(t, f)
	if got := cardOf(t, f, card.ID); got.Status != store.CardRunning {
		t.Fatalf("card: %s", got.Status)
	}
}

// Stop pe card: cardul ramane Stopped (nu se reia singur) pana la Retry.
func TestBrainstormStopStays(t *testing.T) {
	f := setup(t)
	tp := topicWith(t, f, f.rook)
	card := roundFor(t, f, tp, f.rook)
	mustReconcile(t, f)
	run := f.fs.last(t, f.rook)
	if err := f.tm.StopCard(card.ID); err != nil {
		t.Fatal(err)
	}
	if len(f.fs.stopped) != 1 || f.fs.stopped[0] != run.ID {
		t.Fatalf("run neoprit: %v", f.fs.stopped)
	}
	f.fs.finish(run.ID, runner.Outcome{Status: store.RunStopped})
	if got := cardOf(t, f, card.ID); got.Status != store.CardStopped {
		t.Fatalf("card: %s", got.Status)
	}
	n := f.fs.count()
	mustReconcile(t, f)
	if f.fs.count() != n {
		t.Fatal("un card oprit a pornit un run")
	}
	if err := f.tm.RetryCard(card.ID); err != nil {
		t.Fatal(err)
	}
	mustReconcile(t, f)
	if got := cardOf(t, f, card.ID); got.Status != store.CardRunning {
		t.Fatalf("dupa Retry: %s", got.Status)
	}
	if err := f.tm.RetryCard(card.ID); err == nil {
		t.Fatal("Retry pe un card care ruleaza")
	}

	// un card in asteptare se opreste direct
	waiting := roundFor(t, f, tp, f.rook)
	if err := f.tm.StopCard(waiting.ID); err != nil {
		t.Fatal(err)
	}
	if got := cardOf(t, f, waiting.ID); got.Status != store.CardStopped {
		t.Fatalf("card in asteptare: %s", got.Status)
	}
}

// Un agent scos din participanti: cardul lui in asteptare devine Skipped, fara run.
func TestBrainstormRemovedParticipantSkipped(t *testing.T) {
	f := setup(t)
	tp := topicWith(t, f, f.rook, f.vera)
	card := roundFor(t, f, tp, f.vera)
	tp.Participants = []int64{f.rook.ID}
	if err := f.st.UpdateTopic(tp); err != nil {
		t.Fatal(err)
	}
	mustReconcile(t, f)
	if got := cardOf(t, f, card.ID); got.Status != store.CardSkipped {
		t.Fatalf("card: %s", got.Status)
	}
	if _, ok := f.fs.lastOK(f.vera); ok {
		t.Fatal("run pentru Vera")
	}
}

func TestBrainstormRecoverRequeues(t *testing.T) {
	f := setup(t)
	tp := topicWith(t, f, f.rook)
	card := roundFor(t, f, tp, f.rook)
	startCard(t, f, f.rook, card)
	if err := f.tm.Recover(); err != nil {
		t.Fatal(err)
	}
	if got := cardOf(t, f, card.ID); got.Status != store.CardWaiting {
		t.Fatalf("card: %s", got.Status)
	}
}

func TestBrainstormAnswerFollowUp(t *testing.T) {
	f := setup(t)
	tp := topicWith(t, f, f.rook)
	card := roundFor(t, f, tp, f.rook)
	q := store.BrainstormCard{TopicID: tp.ID, RoundID: card.RoundID, AgentID: f.rook.ID, Status: store.CardDone,
		Kind: store.CardQuestion, Title: "PvP or co-op?", Options: []string{"PvP", "Co-op"}}
	if err := f.st.AddCard(&q); err != nil {
		t.Fatal(err)
	}
	if open, _ := f.st.OpenQuestions(f.p.ID); open[tp.ID] != 1 {
		t.Fatalf("intrebari deschise: %v", open)
	}
	if err := f.tm.AnswerCardQuestion(q.ID, "Co-op"); err != nil {
		t.Fatal(err)
	}
	cards, err := f.st.TopicCards(tp.ID)
	if err != nil {
		t.Fatal(err)
	}
	last := cards[len(cards)-1]
	if last.Status != store.CardWaiting || last.FollowsID == nil || *last.FollowsID != q.ID ||
		last.AgentID != f.rook.ID || last.RoundID != card.RoundID {
		t.Fatalf("cardul nou: %+v", last)
	}
	if open, _ := f.st.OpenQuestions(f.p.ID); open[tp.ID] != 0 {
		t.Fatalf("intrebari deschise: %v", open)
	}
	if err := f.tm.AnswerCardQuestion(q.ID, "PvP"); err == nil {
		t.Fatal("al doilea raspuns")
	}
}

// pickedIdea adauga o idee terminata si aleasa.
func pickedIdea(t *testing.T, f *fixture, card store.BrainstormCard, a store.Agent, kind, title string) store.BrainstormCard {
	t.Helper()
	c := store.BrainstormCard{TopicID: card.TopicID, RoundID: card.RoundID, AgentID: a.ID, Status: store.CardDone,
		Kind: kind, Title: title, Text: title + " in detail.", Basis: "net/sync.go"}
	if err := f.st.AddCard(&c); err != nil {
		t.Fatal(err)
	}
	if err := f.tm.PickCard(c.ID, true); err != nil {
		t.Fatal(err)
	}
	return c
}

func TestBrainstormSendToLead(t *testing.T) {
	f := setup(t)
	tp := topicWith(t, f, f.rook, f.vera)
	card := roundFor(t, f, tp, f.rook)
	a := pickedIdea(t, f, card, f.rook, store.CardIdea, "Rollback netcode")
	b := pickedIdea(t, f, card, f.vera, store.CardWild, "Lockstep replays")
	m, err := f.tm.SendToLead(tp.ID, f.lead.ID)
	if err != nil {
		t.Fatal(err)
	}
	if m.Author != store.AuthorUser || m.AgentID != f.lead.ID ||
		!strings.Contains(m.Text, "Rollback netcode") || !strings.Contains(m.Text, "Lockstep replays") {
		t.Fatalf("mesaj: %+v", m)
	}
	if link := fmt.Sprintf("\nTopic: #/p/%d/brainstorm/%d\n", f.p.ID, tp.ID); !strings.Contains(m.Text, link) {
		t.Fatalf("lipseste legatura spre subiect: %q", m.Text)
	}
	if !f.hasMessage(f.lead.ID, "Rollback netcode") {
		t.Fatal("mesajul nu e in chat-ul liderului")
	}
	if _, err := f.tm.SendToLead(tp.ID, f.rook.ID); err == nil {
		t.Fatal("un worker fara echipa a primit ideile")
	}
	for _, c := range []store.BrainstormCard{a, b} {
		if err := f.tm.PickCard(c.ID, false); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.tm.SendToLead(tp.ID, f.lead.ID); err == nil || !strings.Contains(err.Error(), "Pick at least one idea") {
		t.Fatalf("fara idei alese: %v", err)
	}
	if err := f.tm.PickCard(card.ID, true); err == nil {
		t.Fatal("Pick pe un card in asteptare")
	}
}

func TestBrainstormMarkdown(t *testing.T) {
	f := setup(t)
	tp := topicWith(t, f, f.rook)
	card := roundFor(t, f, tp, f.rook)
	pickedIdea(t, f, card, f.rook, store.CardWild, "Lockstep replays")
	q := store.BrainstormCard{TopicID: tp.ID, RoundID: card.RoundID, AgentID: f.rook.ID, Status: store.CardDone,
		Kind: store.CardQuestion, Title: "PvP or co-op?", Options: []string{"PvP", "Co-op"}}
	if err := f.st.AddCard(&q); err != nil {
		t.Fatal(err)
	}
	if err := f.st.AnswerCard(q.ID, "Co-op"); err != nil {
		t.Fatal(err)
	}
	name, body, err := f.tm.BrainstormMarkdown(tp.ID)
	if err != nil {
		t.Fatal(err)
	}
	if want := fmt.Sprintf("brainstorm-%d-netcode.md", tp.ID); name != want {
		t.Fatalf("nume: %q", name)
	}
	for _, want := range []string{"# Netcode", "How do we sync players?", "Lockstep replays", "Rook", "wild", "net/sync.go", "PvP or co-op?", "Co-op"} {
		if !strings.Contains(string(body), want) {
			t.Fatalf("lipseste %q:\n%s", want, body)
		}
	}
}

func TestBrainstormNewRound(t *testing.T) {
	f := setup(t)
	tp := topicWith(t, f, f.rook, f.vera)
	if _, _, err := f.tm.NewRound(tp.ID, "  "); err == nil {
		t.Fatal("runda fara text")
	}
	r, cards, err := f.tm.NewRound(tp.ID, "Ideas for multiplayer?")
	if err != nil {
		t.Fatal(err)
	}
	if r.ID == 0 || len(cards) != 2 {
		t.Fatalf("runda: %+v, carduri: %d", r, len(cards))
	}
	tp.Archived = true
	if err := f.st.UpdateTopic(tp); err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.tm.NewRound(tp.ID, "More?"); err == nil {
		t.Fatal("runda pe un subiect arhivat")
	}
}

func TestBrainstormAnotherIdea(t *testing.T) {
	f := setup(t)
	tp := topicWith(t, f, f.rook)
	card := roundFor(t, f, tp, f.rook)
	if err := f.tm.AnotherIdea(card.ID); err == nil {
		t.Fatal("Another idea pe un card in asteptare")
	}
	idea := pickedIdea(t, f, card, f.rook, store.CardIdea, "Rollback netcode")
	if err := f.tm.AnotherIdea(idea.ID); err != nil {
		t.Fatal(err)
	}
	cards, _ := f.st.TopicCards(tp.ID)
	last := cards[len(cards)-1]
	if last.Status != store.CardWaiting || last.AgentID != f.rook.ID || last.RoundID != card.RoundID {
		t.Fatalf("cardul nou: %+v", last)
	}
}

// Fix: o eroare la salvarea sesiunii nu lasa cardul running fara run.
func TestBrainstormSessionErrorStillFinishes(t *testing.T) {
	f := setup(t)
	tp := topicWith(t, f, f.rook)
	card := roundFor(t, f, tp, f.rook)
	run := startCard(t, f, f.rook, card)
	db, err := sql.Open("sqlite", "file:"+f.db)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TRIGGER no_sessions BEFORE INSERT ON brainstorm_sessions BEGIN SELECT RAISE(ABORT, 'disk full'); END`); err != nil {
		t.Fatal(err)
	}
	f.fs.finish(run.ID, runner.Outcome{Status: store.RunDone, Result: "Use rollback netcode", SessionID: "bs-1"})
	if got := cardOf(t, f, card.ID); got.Status != store.CardDone {
		t.Fatalf("card: %s", got.Status)
	}
}

// StopCard cu un id invalid nu opreste munca altcuiva (run-urile fara card au cardID 0).
func TestBrainstormStopCardInvalidID(t *testing.T) {
	f := setup(t)
	f.task(t, "Build page", f.rook, store.StatusQueued)
	mustReconcile(t, f)
	f.fs.last(t, f.rook)
	if err := f.tm.StopCard(0); err == nil {
		t.Fatal("StopCard(0) a mers")
	}
	if len(f.fs.stopped) != 0 {
		t.Fatalf("run oprit: %v", f.fs.stopped)
	}
}
