package team

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"hobstack/internal/prompt"
	"hobstack/internal/runner"
	"hobstack/internal/store"
)

// allowOf intoarce permissions.allow din --settings-ul unui run.
func allowOf(t *testing.T, spec runner.Spec) []string {
	t.Helper()
	var cfg struct {
		Permissions struct{ Allow []string } `json:"permissions"`
	}
	if err := json.Unmarshal([]byte(argAfter(spec.Args, "--settings")), &cfg); err != nil {
		t.Fatal(err)
	}
	return cfg.Permissions.Allow
}

// chatRun: utilizatorul scrie agentului in chat; intoarce run-ul pornit (activ).
func chatRun(t *testing.T, f *fixture, a store.Agent, text string) runner.Spec {
	t.Helper()
	f.tm.work.Lock()
	_, err := f.tm.addUserMessage(a, text, nil, false, false)
	f.tm.work.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	mustReconcile(t, f)
	return f.fs.last(t, a)
}

func TestChatRunIsReadOnly(t *testing.T) {
	f := setup(t)
	f.rook.Tools = []string{"write", "shell", "git"}
	f.st.UpdateAgent(f.rook)
	run := chatRun(t, f, f.rook, "change the discount rule")
	allow := allowOf(t, run)
	for _, a := range allow {
		if strings.HasPrefix(a, "Edit") || strings.HasPrefix(a, "Write") || a == "Bash(git commit:*)" {
			t.Fatalf("%s in allow la un run de chat: %v", a, allow)
		}
	}
	// shell-ul ramane in chat (verificari), prin regulile pentru comenzi
	if !slices.Contains(allow, "mcp__team__*") || !slices.Contains(allow, "Bash") || !slices.Contains(allow, "Bash(git log:*)") {
		t.Fatalf("%v", allow)
	}
	if !strings.Contains(argAfter(run.Args, "-p"), prompt.ReadOnlyTurn) {
		t.Fatal("lipseste linia read-only din tura")
	}
	f.fs.finish(run.ID, runner.Outcome{Status: store.RunDone})
	// run-ul pe task are tool-urile intregi
	f.task(t, "Change", f.rook, store.StatusQueued)
	mustReconcile(t, f)
	work := f.fs.last(t, f.rook)
	if !slices.Contains(allowOf(t, work), "Bash") || strings.Contains(argAfter(work.Args, "-p"), prompt.ReadOnlyTurn) {
		t.Fatalf("run pe task: %v", allowOf(t, work))
	}
}

// chat -> task: acelasi system prompt, deci aceeasi sesiune Claude (--resume)
func TestReadOnlyRunKeepsSession(t *testing.T) {
	f := setup(t)
	f.rook.Tools = []string{"write", "shell"}
	f.st.UpdateAgent(f.rook)
	run := chatRun(t, f, f.rook, "hi")
	f.fs.finish(run.ID, runner.Outcome{Status: store.RunDone, SessionID: "s-1"})
	f.task(t, "Change", f.rook, store.StatusQueued)
	mustReconcile(t, f)
	work := f.fs.last(t, f.rook)
	if argAfter(work.Args, "--append-system-prompt") != argAfter(run.Args, "--append-system-prompt") || argAfter(work.Args, "--resume") != "s-1" {
		t.Fatalf("sesiunea nu continua: resume=%q", argAfter(work.Args, "--resume"))
	}
}

// refuzurile de scriere dintr-un run fara task sunt intentionate (Bash doar fara shell); WebFetch tot face cerere
func TestChatRunWriteDenialsMakeNoRequest(t *testing.T) {
	f := setup(t)
	f.rook.Tools = []string{"write"}
	f.st.UpdateAgent(f.rook)
	f.tm.work.Lock()
	f.tm.recordDenials(activeRun{projectID: f.p.ID}, f.rook, []runner.Denial{
		{Tool: "Edit", Detail: "app/x.php"}, {Tool: "Write", Detail: "a"}, {Tool: "NotebookEdit"}, {Tool: "Bash", Detail: "php artisan test"},
		{Tool: "WebFetch", Detail: "https://x.dev"},
	})
	f.tm.work.Unlock()
	reqs, _ := f.st.ListOpenAccessRequests(f.p.ID)
	if len(reqs) != 1 || reqs[0].Tool != "WebFetch" {
		t.Fatalf("%+v", reqs)
	}
}

// cu shell, chat-ul ruleaza comenzi prin reguli: Ask face cerere de chat, iar aprobarea intra in run-ul de chat urmator
func TestChatRunShellFollowsRules(t *testing.T) {
	f := setup(t)
	f.rook.Tools = []string{"shell"}
	f.st.UpdateAgent(f.rook)
	f.tm.work.Lock()
	f.tm.recordDenials(activeRun{projectID: f.p.ID}, f.rook, []runner.Denial{
		{Tool: "Bash", Detail: "git push origin main"}, {Tool: "Bash", Detail: "sudo ls"},
	})
	f.tm.work.Unlock()
	reqs, _ := f.st.ListOpenAccessRequests(f.p.ID)
	if len(reqs) != 1 || reqs[0].Prefix != "git push" || reqs[0].TaskID != nil {
		t.Fatalf("doar git push (Ask) face cerere de chat: %+v", reqs)
	}
	if err := f.tm.AllowCommandForTask(reqs[0].ID); err != nil {
		t.Fatal(err)
	}
	mustReconcile(t, f)
	run := f.fs.last(t, f.rook)
	if allow := allowOf(t, run); !slices.Contains(allow, "Bash") || !slices.Contains(allow, "Bash(git push:*)") {
		t.Fatalf("run-ul de chat nu are shell-ul si grant-ul: %v", allow)
	}
}
