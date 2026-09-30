package team

import (
	"errors"
	"os"
	"strings"
	"testing"

	"foreman/internal/runner"
	"foreman/internal/store"
)

func mustReconcile(t *testing.T, f *fixture) {
	t.Helper()
	if err := f.tm.Reconcile(f.p.ID); err != nil {
		t.Fatal(err)
	}
}

func turn(spec runner.Spec) string { return argValue(spec.Args, "-p") }

func TestLeadWakesOnUserMessage(t *testing.T) {
	f := setup(t)
	if _, err := f.tm.UserMessage(f.lead.ID, "Fa importul", nil); err != nil {
		t.Fatal(err)
	}
	mustReconcile(t, f)
	if f.fs.count() != 1 {
		t.Fatalf("run-uri pornite: %d", f.fs.count())
	}
	spec := f.fs.last(t, f.lead)
	if !strings.Contains(turn(spec), "User: Fa importul") || !strings.HasPrefix(turn(spec), "Your direct team:") {
		t.Fatalf("mesajul turei: %q", turn(spec))
	}
	if spec.Dir != f.p.Workdir || argValue(spec.Args, "--model") != "sonnet" || hasFlag(spec.Args, "--resume") {
		t.Fatalf("spec = %+v", spec)
	}
	mustReconcile(t, f) // mesajul e livrat: nu se mai porneste nimic
	if f.fs.count() != 1 {
		t.Fatalf("a doua reconciliere a pornit ceva: %d", f.fs.count())
	}
}

func TestRunArguments(t *testing.T) {
	f := setup(t)
	f.task(t, "A", f.rook, store.StatusQueued)
	mustReconcile(t, f)
	args := f.fs.last(t, f.rook).Args
	for _, flag := range []string{"--strict-mcp-config", "--verbose"} {
		if !hasFlag(args, flag) {
			t.Errorf("lipseste %s", flag)
		}
	}
	checks := map[string]string{
		"--output-format":   "stream-json",
		"--setting-sources": "user",
		"--model":           "sonnet",
	}
	for flag, want := range checks {
		if got := argValue(args, flag); got != want {
			t.Errorf("%s = %q, vreau %q", flag, got, want)
		}
	}
	if hasFlag(args, "--max-budget-usd") {
		t.Error("fara buget setat nu trebuie trimis --max-budget-usd")
	}
	if !strings.Contains(argValue(args, "--settings"), `"Edit"`) {
		t.Error("Rook are tool-ul write: --settings trebuia sa permita Edit")
	}
	if !strings.Contains(argValue(args, "--mcp-config"), `"url":"http://127.0.0.1:7777/mcp/`) {
		t.Errorf("--mcp-config = %s", argValue(args, "--mcp-config"))
	}
	if !strings.Contains(argValue(args, "--append-system-prompt"), "You are Rook, Backend Dev, in the project ApiUnit. You report to Lead.") {
		t.Error("system prompt gresit")
	}
}

func TestFullFlow(t *testing.T) {
	f := setup(t)
	f.tm.UserMessage(f.lead.ID, "Fa importul", nil)
	mustReconcile(t, f)
	lead1 := f.fs.last(t, f.lead)
	lc := f.caller(t, lead1)
	for _, x := range [][2]string{{"A", "Rook"}, {"B", "Vera"}, {"C", "rook"}} {
		if _, err := f.tm.CreateTask(lc, x[0], "", x[1]); err != nil {
			t.Fatal(err)
		}
	}
	f.fs.finish(lead1.ID, runner.Outcome{Status: "done", SessionID: "s-lead", Result: "Am delegat."})
	if !f.hasMessage(f.lead.ID, "Am delegat.") {
		t.Fatal("raspunsul final al lead-ului lipseste din thread")
	}

	// doi workeri in paralel; T-3 asteapta dupa Rook
	mustReconcile(t, f)
	rook1, vera1 := f.fs.last(t, f.rook), f.fs.last(t, f.vera)
	if !strings.Contains(turn(rook1), "Task T-1: A") || !strings.Contains(turn(vera1), "Task T-2: B") {
		t.Fatalf("ture: %q / %q", turn(rook1), turn(vera1))
	}
	f.wantStatus(t, 1, store.StatusInProgress)
	f.wantStatus(t, 3, store.StatusQueued)

	// Rook raporteaza T-1: lead-ul e trezit cu --resume si notificarea
	if _, err := f.tm.ReportDone(f.caller(t, rook1), "", "A e gata"); err != nil {
		t.Fatal(err)
	}
	mustReconcile(t, f)
	lead2 := f.fs.last(t, f.lead)
	if lead2.ID == lead1.ID || !strings.Contains(turn(lead2), "Rook finished T-1 (review): A e gata") {
		t.Fatalf("lead-ul nu a fost trezit corect: %q", turn(lead2))
	}
	if argValue(lead2.Args, "--resume") != "s-lead" {
		t.Fatalf("--resume = %q", argValue(lead2.Args, "--resume"))
	}

	// procesul lui Rook se termina: preia T-3, continuand sesiunea
	f.fs.finish(rook1.ID, runner.Outcome{Status: "done", SessionID: "s-rook", Result: "gata"})
	mustReconcile(t, f)
	rook2 := f.fs.last(t, f.rook)
	if !strings.Contains(turn(rook2), "Task T-3: C") || argValue(rook2.Args, "--resume") != "s-rook" {
		t.Fatalf("rook2: %q resume=%q", turn(rook2), argValue(rook2.Args, "--resume"))
	}

	// lead-ul trimite T-1 inapoi; Rook termina T-3 fara report_done
	if _, err := f.tm.SendBack(f.caller(t, lead2), "T-1", "Lipsesc testele"); err != nil {
		t.Fatal(err)
	}
	f.fs.finish(lead2.ID, runner.Outcome{Status: "done", SessionID: "s-lead"})
	f.fs.finish(rook2.ID, runner.Outcome{Status: "done", SessionID: "s-rook", Result: "C e gata"})
	if got := f.wantStatus(t, 3, store.StatusReview); got.Result != "C e gata" {
		t.Fatalf("T-3 rezultat = %q", got.Result)
	}

	mustReconcile(t, f)
	rook3 := f.fs.last(t, f.rook)
	if !strings.Contains(turn(rook3), "Task T-1: A") || !strings.Contains(turn(rook3), "Feedback on the previous attempt:\nLipsesc testele") {
		t.Fatalf("rook3: %q", turn(rook3))
	}
	lead3 := f.fs.last(t, f.lead)
	if !strings.Contains(turn(lead3), "Rook finished T-3 without report_done (review): C e gata") {
		t.Fatalf("lead3: %q", turn(lead3))
	}
	if _, err := f.tm.AcceptTask(f.caller(t, lead3), "T-3", ""); err != nil {
		t.Fatal(err)
	}
	f.wantStatus(t, 3, store.StatusDone)
}

func TestMaxParallel(t *testing.T) {
	f := setup(t)
	f.p.MaxParallel = 1
	f.st.UpdateProject(f.p)
	f.task(t, "A", f.rook, store.StatusQueued)
	f.task(t, "B", f.vera, store.StatusQueued)
	mustReconcile(t, f)
	if f.fs.count() != 1 {
		t.Fatalf("run-uri pornite: %d, vreau 1", f.fs.count())
	}
	f.wantStatus(t, 2, store.StatusQueued)
}

func TestFailedWorkerRunGoesToReview(t *testing.T) {
	f := setup(t)
	f.task(t, "A", f.rook, store.StatusQueued)
	mustReconcile(t, f)
	f.fs.finish(f.fs.last(t, f.rook).ID, runner.Outcome{Status: "failed", Error: "Reached maximum budget ($1)"})
	if got := f.wantStatus(t, 1, store.StatusReview); got.Result != "run failed: Reached maximum budget ($1)" {
		t.Fatalf("rezultat = %q", got.Result)
	}
	pending, _ := f.st.Undelivered(f.lead.ID)
	if len(pending) != 1 || !strings.Contains(pending[0].Text, "Rook's run on T-1 failed") {
		t.Fatalf("notificari lead: %+v", pending)
	}
	if !f.hasMessage(f.rook.ID, "Run failed: Reached maximum budget") {
		t.Error("lipseste evenimentul din thread-ul worker-ului")
	}
}

func TestStopAndResume(t *testing.T) {
	f := setup(t)
	f.task(t, "A", f.rook, store.StatusQueued)
	mustReconcile(t, f)
	run := f.fs.last(t, f.rook)

	if err := f.tm.Stop(f.p.ID); err != nil {
		t.Fatal(err)
	}
	if len(f.fs.stopped) != 1 || f.fs.stopped[0] != run.ID {
		t.Fatalf("oprite: %v", f.fs.stopped)
	}
	f.wantStatus(t, 1, store.StatusQueued)
	if p, _ := f.st.GetProject(f.p.ID); !p.Paused {
		t.Fatal("proiectul nu e pe pauza")
	}
	f.fs.finish(run.ID, runner.Outcome{Status: "stopped", Error: "oprit"})
	mustReconcile(t, f)
	if f.fs.count() != 1 {
		t.Fatal("pe pauza nu trebuie sa porneasca nimic")
	}

	if err := f.tm.Resume(f.p.ID); err != nil {
		t.Fatal(err)
	}
	mustReconcile(t, f)
	if f.fs.count() != 2 {
		t.Fatalf("dupa Resume: %d run-uri", f.fs.count())
	}
}

func TestResumeWakesLeadForReview(t *testing.T) {
	f := setup(t)
	f.task(t, "A", f.rook, store.StatusReview)
	if err := f.tm.Stop(f.p.ID); err != nil {
		t.Fatal(err)
	}
	if err := f.tm.Resume(f.p.ID); err != nil {
		t.Fatal(err)
	}
	mustReconcile(t, f)
	if f.fs.count() != 1 {
		t.Fatalf("run-uri pornite: %d", f.fs.count())
	}
	got := turn(f.fs.last(t, f.lead))
	if !strings.Contains(got, "The team was restarted") || !strings.Contains(got, "T-1") {
		t.Fatalf("tura lead-ului: %q", got)
	}
}

func TestWorkdirChangeStartsNewSession(t *testing.T) {
	f := setup(t)
	f.tm.UserMessage(f.rook.ID, "salut", nil)
	mustReconcile(t, f)
	f.fs.finish(f.fs.last(t, f.rook).ID, runner.Outcome{Status: "done", SessionID: "s-rook", Result: "ok"})

	f.p.Workdir = t.TempDir()
	if err := f.st.UpdateProject(f.p); err != nil {
		t.Fatal(err)
	}
	f.tm.UserMessage(f.rook.ID, "din nou", nil)
	mustReconcile(t, f)
	if hasFlag(f.fs.last(t, f.rook).Args, "--resume") {
		t.Fatal("folder schimbat: nu trebuia --resume")
	}
}

func TestRecover(t *testing.T) {
	f := setup(t)
	task := f.task(t, "A", f.rook, store.StatusInProgress)
	f.st.CreateRun(&store.Run{ID: "r-vechi", AgentID: f.rook.ID, TaskID: task.ID})
	if err := f.tm.Recover(); err != nil {
		t.Fatal(err)
	}
	if r, _ := f.st.GetRun("r-vechi"); r.Status != store.RunInterrupted {
		t.Fatalf("run vechi: %q", r.Status)
	}
	f.wantStatus(t, 1, store.StatusQueued)
	if p, _ := f.st.GetProject(f.p.ID); !p.Paused {
		t.Fatal("dupa Recover proiectul trebuie sa fie pe pauza")
	}
}

func TestMissingWorkdirPausesProject(t *testing.T) {
	f := setup(t)
	f.tm.UserMessage(f.lead.ID, "salut", nil)
	os.RemoveAll(f.p.Workdir)
	mustReconcile(t, f)
	if f.fs.count() != 0 {
		t.Fatal("nu trebuia pornit nimic")
	}
	if p, _ := f.st.GetProject(f.p.ID); !p.Paused {
		t.Fatal("proiectul trebuia pus pe pauza")
	}
	if !f.hasMessage(f.lead.ID, "Cannot start Lead") {
		t.Fatal("lipseste explicatia din thread-ul lead-ului")
	}
}

func TestPromptChangeStartsNewSession(t *testing.T) {
	f := setup(t)
	f.st.SetSession(f.rook.ID, "s-vechi", "hash-vechi")
	f.tm.UserMessage(f.rook.ID, "salut", nil)
	mustReconcile(t, f)
	if hasFlag(f.fs.last(t, f.rook).Args, "--resume") {
		t.Fatal("prompt schimbat: nu trebuia --resume")
	}
	if !f.hasMessage(f.rook.ID, "new session") {
		t.Fatal("lipseste evenimentul de sesiune noua")
	}
}

func TestDirectMessageToWorker(t *testing.T) {
	f := setup(t)
	f.tm.UserMessage(f.rook.ID, "ce faci?", nil)
	mustReconcile(t, f)
	if got := turn(f.fs.last(t, f.rook)); !strings.Contains(got, "Notifications:\n- User: ce faci?") {
		t.Fatalf("worker: %q", got)
	}
	if got := turn(f.fs.last(t, f.lead)); !strings.Contains(got, "User -> Rook: ce faci?") {
		t.Fatalf("lead: %q", got)
	}
	msgs, _ := f.st.ListMessages(f.rook.ID)
	if !msgs[0].Delivered {
		t.Fatal("mesajul trebuia marcat livrat")
	}
}

func TestBlockedTaskActions(t *testing.T) {
	f := setup(t)
	a := f.task(t, "A", f.rook, store.StatusBlocked)
	b := f.task(t, "B", f.rook, store.StatusBlocked)
	c := f.task(t, "C", f.rook, store.StatusQueued)
	if err := f.tm.Unblock(a.ID); err != nil {
		t.Fatal(err)
	}
	if got := f.wantStatus(t, 1, store.StatusQueued); got.SendBacks != 0 {
		t.Fatalf("send_backs = %d", got.SendBacks)
	}
	if err := f.tm.AcceptBlocked(b.ID); err != nil {
		t.Fatal(err)
	}
	f.wantStatus(t, 2, store.StatusDone)
	if err := f.tm.AcceptBlocked(c.ID); !errors.Is(err, ErrNotBlocked) {
		t.Fatalf("task neblocat: %v", err)
	}
	if !f.hasMessage(f.lead.ID, "The user unblocked T-1") {
		t.Fatal("lead-ul trebuie anuntat")
	}
}

func TestBudgetFlagWhenSet(t *testing.T) {
	f := setup(t)
	f.p.BudgetUSD = 1.5
	f.st.UpdateProject(f.p)
	f.task(t, "A", f.rook, store.StatusQueued)
	mustReconcile(t, f)
	if got := argValue(f.fs.last(t, f.rook).Args, "--max-budget-usd"); got != "1.50" {
		t.Fatalf("--max-budget-usd = %q, vreau 1.50", got)
	}
}

func TestUnlimitedParallel(t *testing.T) {
	f := setup(t)
	f.p.MaxParallel = 0 // fara limita
	f.st.UpdateProject(f.p)
	quill := store.Agent{ProjectID: f.p.ID, Name: "Quill", ParentID: &f.lead.ID, Model: "haiku"}
	if err := f.st.CreateAgent(&quill); err != nil {
		t.Fatal(err)
	}
	f.task(t, "A", f.rook, store.StatusQueued)
	f.task(t, "B", f.vera, store.StatusQueued)
	f.task(t, "C", quill, store.StatusQueued)
	mustReconcile(t, f)
	if f.fs.count() != 3 {
		t.Fatalf("run-uri pornite: %d, vreau 3 (fara limita)", f.fs.count())
	}
}

// Cu max_parallel mic, cine are de decis (aici reviewer-ul) porneste inaintea
// task-urilor din coada; altfel review-ul ar astepta dupa toata coada.
func TestDecisionsBeforeQueue(t *testing.T) {
	f := setup(t)
	f.p.MaxParallel = 1
	f.st.UpdateProject(f.p)
	f.rook.ReviewerID = &f.vera.ID
	f.st.UpdateAgent(f.rook)
	done := f.task(t, "A", f.rook, store.StatusInProgress)
	if _, err := f.tm.ReportDone(f.workerCaller(f.rook, done.ID), "", "A e gata"); err != nil {
		t.Fatal(err)
	}
	f.task(t, "B", f.rook, store.StatusQueued)
	f.task(t, "C", f.rook, store.StatusQueued)
	mustReconcile(t, f)
	if f.fs.count() != 1 || f.fs.last(t, f.vera).ID == "" {
		t.Fatalf("run-uri: %d, vreau doar Vera", f.fs.count())
	}
	f.wantStatus(t, 2, store.StatusQueued)
}
