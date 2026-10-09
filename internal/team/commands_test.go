package team

import (
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	"hobstack/internal/runner"
	"hobstack/internal/store"
)

func argAfter(args []string, flag string) string {
	for i, a := range args {
		if a == flag && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}

// denyOf intoarce permissions.deny din --settings-ul unui run.
func denyOf(t *testing.T, spec runner.Spec) []string {
	t.Helper()
	var cfg struct {
		Permissions struct{ Deny []string } `json:"permissions"`
	}
	if err := json.Unmarshal([]byte(argAfter(spec.Args, "--settings")), &cfg); err != nil {
		t.Fatal(err)
	}
	return cfg.Permissions.Deny
}

func TestCommandRulesInRun(t *testing.T) {
	f := setup(t)
	f.rook.Tools = []string{"shell"}
	f.st.UpdateAgent(f.rook)
	f.st.SetCommandRules(f.p.ID, []store.CommandRule{{Prefix: "git push", Mode: store.CmdAllow}, {Prefix: "docker", Mode: store.CmdBlock}})
	f.task(t, "Ship", f.rook, store.StatusQueued)
	if err := f.tm.Reconcile(f.p.ID); err != nil { // sincron, porneste run-ul lui Rook
		t.Fatal(err)
	}
	spec := f.fs.last(t, f.rook)
	settings := argAfter(spec.Args, "--settings")
	if strings.Contains(settings, "git push") || !strings.Contains(settings, "Bash(docker:*)") || !strings.Contains(settings, "Bash(rm -rf:*)") {
		t.Fatalf("settings = %s", settings)
	}
	if !strings.Contains(argAfter(spec.Args, "--append-system-prompt"), "do not try them: docker, sudo.") {
		t.Fatal("linia cu comenzile blocate lipseste")
	}
}

func TestAskDenialAllowOnceAndProject(t *testing.T) {
	f := setup(t)
	f.rook.Tools = []string{"shell"}
	f.st.UpdateAgent(f.rook)
	task := f.task(t, "Ship", f.rook, store.StatusInProgress)
	ar := activeRun{projectID: f.p.ID, taskID: task.ID}
	f.tm.work.Lock()
	f.tm.recordDenials(ar, f.rook, []runner.Denial{
		{Tool: "Bash", Detail: "echo ok && git push origin main"},
		{Tool: "Bash", Detail: "sudo rm x"},
	})
	f.tm.work.Unlock()
	reqs, _ := f.st.ListOpenAccessRequests(f.p.ID)
	if len(reqs) != 1 || reqs[0].Prefix != "git push" {
		t.Fatalf("doar git push (Ask) face cerere, sudo (Block) nu: %+v", reqs)
	}
	if err := f.tm.AllowCommandForTask(reqs[0].ID); err != nil {
		t.Fatal(err)
	}
	pending, _ := f.st.Undelivered(f.rook.ID)
	if len(pending) == 0 || !strings.Contains(pending[len(pending)-1].Text, "allowed on T-1") {
		t.Fatalf("Rook nu a fost trezit: %+v", pending)
	}
	if got, _ := f.st.GetTask(task.ID); got.Status != store.StatusQueued {
		t.Fatalf("status = %s", got.Status)
	}
	if r, _ := f.st.ListOpenAccessRequests(f.p.ID); len(r) != 0 {
		t.Fatal("cererea a ramas deschisa")
	}
	// Allow in this project scrie regula proiectului
	f.tm.work.Lock()
	f.tm.recordDenials(ar, f.rook, []runner.Denial{{Tool: "Bash", Detail: "git push"}})
	f.tm.work.Unlock()
	reqs, _ = f.st.ListOpenAccessRequests(f.p.ID)
	if err := f.tm.AllowCommandInProject(reqs[0].ID); err != nil {
		t.Fatal(err)
	}
	eff, _ := f.tm.EffectiveCommandRules(f.p.ID)
	for _, r := range eff {
		if r.Prefix == "git push" && r.Mode != store.CmdAllow {
			t.Fatalf("%+v", eff)
		}
	}
	// alt proiect ramane pe regula globala (Ask)
	other, _ := f.tm.EffectiveCommandRules(f.p.ID + 999)
	for _, r := range other {
		if r.Prefix == "git push" && r.Mode != store.CmdAsk {
			t.Fatalf("%+v", other)
		}
	}
}

func TestBlockCommandAndNonCommandRequest(t *testing.T) {
	f := setup(t)
	f.rook.Tools = []string{"shell"}
	f.st.UpdateAgent(f.rook)
	f.tm.work.Lock()
	f.tm.recordDenials(activeRun{projectID: f.p.ID, taskID: f.task(t, "X", f.rook, store.StatusInProgress).ID}, f.rook, []runner.Denial{{Tool: "Bash", Detail: "rm -rf build"}})
	f.tm.work.Unlock()
	reqs, _ := f.st.ListOpenAccessRequests(f.p.ID)
	if err := f.tm.BlockCommandInProject(reqs[0].ID); err != nil {
		t.Fatal(err)
	}
	proj, _ := f.st.CommandRules(f.p.ID)
	if len(proj) != 1 || proj[0].Prefix != "rm -rf" || proj[0].Mode != store.CmdBlock {
		t.Fatalf("%+v", proj)
	}
	// cerere de tool (agent fara Shell): actiunile pe comanda o refuza
	f.vera.Tools = nil
	f.st.UpdateAgent(f.vera)
	f.st.RecordAccessDenial(f.p.ID, f.vera.ID, nil, "Bash", "ls", "")
	reqs, _ = f.st.ListOpenAccessRequests(f.p.ID)
	if err := f.tm.AllowCommandForTask(reqs[len(reqs)-1].ID); err != ErrNotCommandRequest {
		t.Fatalf("err = %v", err)
	}
}

func TestAllowOnceGrantLifecycle(t *testing.T) {
	f := setup(t)
	f.rook.Tools = []string{"shell"}
	f.st.UpdateAgent(f.rook)
	task := f.task(t, "Ship", f.rook, store.StatusInProgress)
	f.tm.work.Lock()
	f.tm.recordDenials(activeRun{projectID: f.p.ID, taskID: task.ID}, f.rook, []runner.Denial{{Tool: "Bash", Detail: "git push"}})
	f.tm.work.Unlock()
	reqs, _ := f.st.ListOpenAccessRequests(f.p.ID)
	if err := f.tm.AllowCommandForTask(reqs[0].ID); err != nil {
		t.Fatal(err)
	}
	// a doua apasare pe aceeasi cerere nu mai face nimic
	if err := f.tm.AllowCommandForTask(reqs[0].ID); !errors.Is(err, ErrRequestClosed) {
		t.Fatalf("err = %v", err)
	}
	if err := f.tm.AllowCommandForTask(99999); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
	f.wantStatus(t, task.Number, store.StatusQueued)
	if err := f.tm.Reconcile(f.p.ID); err != nil {
		t.Fatal(err)
	}
	run := f.fs.last(t, f.rook)
	if slices.Contains(denyOf(t, run), "Bash(git push:*)") {
		t.Fatal("run-ul cu grant are git push in deny")
	}
	f.fs.finish(run.ID, runner.Outcome{Status: store.RunDone})
	// un run in chat (fara task) nu primeste grant-ul task-ului
	f.tm.work.Lock()
	_, err := f.tm.addUserMessage(f.rook, "again", nil, false, false)
	f.tm.work.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if err := f.tm.Reconcile(f.p.ID); err != nil {
		t.Fatal(err)
	}
	chat := f.fs.last(t, f.rook)
	if chat.ID == run.ID {
		t.Fatal("nu a pornit run-ul de chat")
	}
	if !slices.Contains(denyOf(t, chat), "Bash(git push:*)") {
		t.Fatal("grant-ul task-ului a ajuns in chat")
	}
	f.fs.finish(chat.ID, runner.Outcome{Status: store.RunDone})
	// al doilea run pe acelasi task: grant-ul tine
	got, _ := f.st.GetTask(task.ID)
	got.Status = store.StatusQueued
	f.st.SaveTask(&got)
	if err := f.tm.Reconcile(f.p.ID); err != nil {
		t.Fatal(err)
	}
	again := f.fs.last(t, f.rook)
	if again.ID == chat.ID || slices.Contains(denyOf(t, again), "Bash(git push:*)") {
		t.Fatal("grant-ul nu a tinut pe al doilea run al task-ului")
	}
}

func TestDenyingRuleFallback(t *testing.T) {
	f := setup(t)
	f.rook.Tools = []string{"shell"}
	f.st.UpdateAgent(f.rook)
	f.st.SetCommandRules(f.p.ID, []store.CommandRule{{Prefix: "git", Mode: store.CmdAsk}, {Prefix: "git status", Mode: store.CmdAllow}})
	// regula decisiva e Allow (git status), dar claude a refuzat din cauza lui "git" (Ask)
	f.tm.work.Lock()
	f.tm.recordDenials(activeRun{projectID: f.p.ID, taskID: f.task(t, "X", f.rook, store.StatusInProgress).ID}, f.rook, []runner.Denial{{Tool: "Bash", Detail: "git status"}})
	f.tm.work.Unlock()
	reqs, _ := f.st.ListOpenAccessRequests(f.p.ID)
	if len(reqs) != 1 || reqs[0].Prefix != "git" {
		t.Fatalf("%+v", reqs)
	}
}

func TestDenialClassifiedOnFullCommand(t *testing.T) {
	f := setup(t)
	f.rook.Tools = []string{"shell"}
	f.st.UpdateAgent(f.rook)
	long := strings.Repeat("echo x && ", 30) + "git push"
	f.tm.work.Lock()
	f.tm.recordDenials(activeRun{projectID: f.p.ID, taskID: f.task(t, "X", f.rook, store.StatusInProgress).ID}, f.rook, []runner.Denial{{Tool: "Bash", Detail: long[:120] + "...", Command: long}})
	f.tm.work.Unlock()
	reqs, _ := f.st.ListOpenAccessRequests(f.p.ID)
	if len(reqs) != 1 || reqs[0].Prefix != "git push" {
		t.Fatalf("%+v", reqs)
	}
}

func TestAllowRefusedWhenRuleNoLongerAsk(t *testing.T) {
	f := setup(t)
	f.rook.Tools = []string{"shell"}
	f.st.UpdateAgent(f.rook)
	task := f.task(t, "Ship", f.rook, store.StatusInProgress)
	f.tm.work.Lock()
	f.tm.recordDenials(activeRun{projectID: f.p.ID, taskID: task.ID}, f.rook, []runner.Denial{{Tool: "Bash", Detail: "git push"}})
	f.tm.work.Unlock()
	reqs, _ := f.st.ListOpenAccessRequests(f.p.ID)
	f.st.SetCommandRules(f.p.ID, []store.CommandRule{{Prefix: "git push", Mode: store.CmdBlock}})
	for _, act := range []func(int64) error{f.tm.AllowCommandForTask, f.tm.AllowCommandInProject} {
		err := act(reqs[0].ID)
		if !errors.Is(err, ErrRuleChanged) || err.Error() != "The rule for git push changed; this request is out of date." {
			t.Fatalf("err = %v", err)
		}
	}
	if g, _ := f.st.CommandGrants(f.rook.ID, task.ID); len(g) != 0 {
		t.Fatalf("grants = %v", g)
	}
	if open, _ := f.st.ListOpenAccessRequests(f.p.ID); len(open) != 1 {
		t.Fatal("cererea trebuia sa ramana deschisa")
	}
	// grant-ul nu ridica Block-ul nici daca exista deja
	task2 := f.task(t, "Again", f.rook, store.StatusQueued)
	f.st.AddCommandGrant(f.p.ID, f.rook.ID, &task2.ID, "git push")
	f.tm.Reconcile(f.p.ID)
	if !slices.Contains(denyOf(t, f.fs.last(t, f.rook)), "Bash(git push:*)") {
		t.Fatal("Block ridicat de grant")
	}
}

func TestAllowInProjectSweepsSamePrefix(t *testing.T) {
	f := setup(t)
	f.rook.Tools = []string{"shell"}
	f.vera.Tools = []string{"shell"}
	f.st.UpdateAgent(f.rook)
	f.st.UpdateAgent(f.vera)
	tr := f.task(t, "A", f.rook, store.StatusInProgress)
	tv := f.task(t, "B", f.vera, store.StatusInProgress)
	for _, c := range []struct {
		a store.Agent
		t store.Task
	}{{f.rook, tr}, {f.vera, tv}} {
		f.tm.work.Lock()
		f.tm.recordDenials(activeRun{projectID: f.p.ID, taskID: c.t.ID}, c.a, []runner.Denial{{Tool: "Bash", Detail: "git push"}})
		f.tm.work.Unlock()
	}
	reqs, _ := f.st.ListOpenAccessRequests(f.p.ID)
	if len(reqs) != 2 {
		t.Fatalf("%+v", reqs)
	}
	if err := f.tm.AllowCommandInProject(reqs[0].ID); err != nil {
		t.Fatal(err)
	}
	if open, _ := f.st.ListOpenAccessRequests(f.p.ID); len(open) != 0 {
		t.Fatalf("%+v", open)
	}
	pending, _ := f.st.Undelivered(f.vera.ID)
	want := `You may now run commands starting with "git push" (allowed in this project by the user). Run your command again if you still need it.`
	if len(pending) == 0 || pending[len(pending)-1].Text != want {
		t.Fatalf("Vera nu a fost trezita: %+v", pending)
	}
	if got, _ := f.st.GetTask(tv.ID); got.Status != store.StatusQueued {
		t.Fatalf("status = %s", got.Status)
	}
}

func TestBlockInProjectDismissesSamePrefix(t *testing.T) {
	f := setup(t)
	f.rook.Tools = []string{"shell"}
	f.vera.Tools = []string{"shell"}
	f.st.UpdateAgent(f.rook)
	f.st.UpdateAgent(f.vera)
	for _, a := range []store.Agent{f.rook, f.vera} {
		f.tm.work.Lock()
		f.tm.recordDenials(activeRun{projectID: f.p.ID, taskID: f.task(t, "X", a, store.StatusInProgress).ID}, a, []runner.Denial{{Tool: "Bash", Detail: "git push"}})
		f.tm.work.Unlock()
	}
	reqs, _ := f.st.ListOpenAccessRequests(f.p.ID)
	if err := f.tm.BlockCommandInProject(reqs[0].ID); err != nil {
		t.Fatal(err)
	}
	if open, _ := f.st.ListOpenAccessRequests(f.p.ID); len(open) != 0 {
		t.Fatalf("%+v", open)
	}
	if p, _ := f.st.Undelivered(f.vera.ID); len(p) != 0 {
		t.Fatalf("Block nu trezeste: %+v", p)
	}
}

func TestTaskGrantAllowsCommandWithoutShell(t *testing.T) {
	f := setup(t)
	f.rook.Tools = []string{"git"}
	f.st.UpdateAgent(f.rook)
	task := f.task(t, "Stash", f.rook, store.StatusQueued)
	f.st.AddCommandGrant(f.p.ID, f.rook.ID, &task.ID, "git stash")
	if err := f.tm.Reconcile(f.p.ID); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(argAfter(f.fs.last(t, f.rook).Args, "--settings"), `"Bash(git stash:*)"`) {
		t.Fatal("grant-ul nu e in allow")
	}
}

func TestNoShellDenialAllowOnTask(t *testing.T) {
	f := setup(t)
	f.rook.Tools = []string{"git"}
	f.st.UpdateAgent(f.rook)
	task := f.task(t, "Stash", f.rook, store.StatusInProgress)
	f.tm.work.Lock()
	f.tm.recordDenials(activeRun{projectID: f.p.ID, taskID: task.ID}, f.rook, []runner.Denial{
		{Tool: "Bash", Detail: `git stash push -u -m "wip" -- app/`},
	})
	f.tm.work.Unlock()
	reqs, _ := f.st.ListOpenAccessRequests(f.p.ID)
	if len(reqs) != 1 || reqs[0].Prefix != "git stash" || reqs[0].Kind != store.AccessCommand || reqs[0].TaskID == nil || *reqs[0].TaskID != task.ID {
		t.Fatalf("%+v", reqs)
	}
	// Allow in this project / Block nu au sens fara Shell
	for _, act := range []func(int64) error{f.tm.AllowCommandInProject, f.tm.BlockCommandInProject} {
		if err := act(reqs[0].ID); !errors.Is(err, ErrNoProjectRule) {
			t.Fatalf("err = %v", err)
		}
	}
	if err := f.tm.AllowCommandForTask(reqs[0].ID); err != nil {
		t.Fatal(err)
	}
	if g, _ := f.st.CommandGrants(f.rook.ID, task.ID); len(g) != 1 || g[0] != "git stash" {
		t.Fatalf("grants = %v", g)
	}
	pending, _ := f.st.Undelivered(f.rook.ID)
	want := `You may now run commands starting with "git stash" (allowed on T-1 by the user). Run your command again if you still need it.`
	if len(pending) == 0 || pending[len(pending)-1].Text != want {
		t.Fatalf("%+v", pending)
	}
	f.wantStatus(t, task.Number, store.StatusQueued)
}

func TestNoShellAllowRefusedWhenBlockedNow(t *testing.T) {
	f := setup(t)
	f.rook.Tools = []string{"git"}
	f.st.UpdateAgent(f.rook)
	task := f.task(t, "Stash", f.rook, store.StatusInProgress)
	f.tm.work.Lock()
	f.tm.recordDenials(activeRun{projectID: f.p.ID, taskID: task.ID}, f.rook, []runner.Denial{{Tool: "Bash", Detail: "git stash"}})
	f.tm.work.Unlock()
	reqs, _ := f.st.ListOpenAccessRequests(f.p.ID)
	f.st.SetCommandRules(f.p.ID, []store.CommandRule{{Prefix: "git stash", Mode: store.CmdBlock}})
	if err := f.tm.AllowCommandForTask(reqs[0].ID); !errors.Is(err, ErrRuleChanged) {
		t.Fatalf("err = %v", err)
	}
	if g, _ := f.st.CommandGrants(f.rook.ID, task.ID); len(g) != 0 {
		t.Fatalf("grants = %v", g)
	}
}

func TestNoShellDenialSkipsGrantedAndBlocked(t *testing.T) {
	f := setup(t)
	f.rook.Tools = []string{"git"}
	f.st.UpdateAgent(f.rook)
	task := f.task(t, "Stash", f.rook, store.StatusInProgress)
	f.st.AddCommandGrant(f.p.ID, f.rook.ID, &task.ID, "git stash")
	f.tm.work.Lock()
	f.tm.recordDenials(activeRun{projectID: f.p.ID, taskID: task.ID}, f.rook, []runner.Denial{
		{Tool: "Bash", Detail: "sudo ls"},
		{Tool: "Bash", Detail: "git stash && make build"},
	})
	f.tm.work.Unlock()
	reqs, _ := f.st.ListOpenAccessRequests(f.p.ID)
	if len(reqs) != 1 || reqs[0].Prefix != "make build" {
		t.Fatalf("doar make build: %+v", reqs)
	}
}

func TestEnableShellWakesAgent(t *testing.T) {
	f := setup(t)
	f.rook.Tools = []string{"git"}
	f.st.UpdateAgent(f.rook)
	task := f.task(t, "Stash", f.rook, store.StatusInProgress)
	f.st.RecordCommandRequest(f.p.ID, f.rook.ID, &task.ID, "git stash", "git stash")
	f.st.RecordAccessDenial(f.p.ID, f.rook.ID, nil, "WebFetch", "https://x.dev", "")
	reqs, _ := f.st.ListOpenAccessRequests(f.p.ID)
	if err := f.tm.EnableTool(reqs[1].ID, "shell"); !errors.Is(err, ErrNotToolRequest) {
		t.Fatalf("WebFetch: %v", err)
	}
	if err := f.tm.EnableTool(reqs[0].ID, "packages"); !errors.Is(err, ErrNotToolRequest) {
		t.Fatalf("packages pe o comanda: %v", err)
	}
	if err := f.tm.EnableTool(reqs[0].ID, "shell"); err != nil {
		t.Fatal(err)
	}
	got, _ := f.st.GetAgent(f.rook.ID)
	if !slices.Contains(got.Tools, "shell") || !slices.Contains(got.Tools, "git") {
		t.Fatalf("%v", got.Tools)
	}
	if !f.hasMessage(f.rook.ID, "You now have the Shell tool (enabled by the user).") {
		t.Fatal("Rook nu a fost trezit")
	}
	f.wantStatus(t, task.Number, store.StatusQueued)
	if err := f.tm.EnableTool(reqs[0].ID, "shell"); !errors.Is(err, ErrRequestClosed) {
		t.Fatalf("a doua oara: %v", err)
	}
}

// o comanda compusa face mai multe carduri: agentul e trezit abia dupa ultima aprobare pe task
func TestAllowWakesAfterLastRequestOnTask(t *testing.T) {
	f := setup(t)
	f.rook.Tools = []string{"git"}
	f.st.UpdateAgent(f.rook)
	task := f.task(t, "Build", f.rook, store.StatusInProgress)
	f.tm.work.Lock()
	f.tm.recordDenials(activeRun{projectID: f.p.ID, taskID: task.ID}, f.rook, []runner.Denial{{Tool: "Bash", Detail: "make build && make test"}})
	f.tm.work.Unlock()
	reqs, _ := f.st.ListOpenAccessRequests(f.p.ID)
	if len(reqs) != 2 {
		t.Fatalf("%+v", reqs)
	}
	if err := f.tm.AllowCommandForTask(reqs[0].ID); err != nil {
		t.Fatal(err)
	}
	f.wantStatus(t, task.Number, store.StatusInProgress)
	if p, _ := f.st.Undelivered(f.rook.ID); len(p) != 0 {
		t.Fatalf("trezit prea devreme: %+v", p)
	}
	if err := f.tm.AllowCommandForTask(reqs[1].ID); err != nil {
		t.Fatal(err)
	}
	f.wantStatus(t, task.Number, store.StatusQueued)
	if p, _ := f.st.Undelivered(f.rook.ID); len(p) != 1 {
		t.Fatalf("un singur mesaj: %+v", p)
	}
}

// Enable Shell inchide toate cererile Bash de tool/comanda ale agentului (au devenit inutile)
func TestEnableShellClosesOtherBashRequests(t *testing.T) {
	f := setup(t)
	f.rook.Tools = []string{"git"}
	f.st.UpdateAgent(f.rook)
	task := f.task(t, "Build", f.rook, store.StatusInProgress)
	f.st.RecordCommandRequest(f.p.ID, f.rook.ID, &task.ID, "make build", "make build")
	f.st.RecordCommandRequest(f.p.ID, f.rook.ID, &task.ID, "make test", "make test")
	f.st.RecordAccessDenial(f.p.ID, f.rook.ID, nil, "WebFetch", "https://x.dev", "")
	reqs, _ := f.st.ListOpenAccessRequests(f.p.ID)
	if err := f.tm.EnableTool(reqs[0].ID, "shell"); err != nil {
		t.Fatal(err)
	}
	open, _ := f.st.ListOpenAccessRequests(f.p.ID)
	if len(open) != 1 || open[0].Tool != "WebFetch" {
		t.Fatalf("%+v", open)
	}
}

// Allow in this project nu ajuta un agent fara Shell: nici direct, nici prin maturarea cererilor
func TestAllowInProjectSkipsAgentsWithoutShell(t *testing.T) {
	f := setup(t)
	f.rook.Tools = []string{"shell"}
	f.vera.Tools = []string{"git"}
	f.st.UpdateAgent(f.rook)
	f.st.UpdateAgent(f.vera)
	for _, a := range []store.Agent{f.rook, f.vera} {
		f.tm.work.Lock()
		f.tm.recordDenials(activeRun{projectID: f.p.ID, taskID: f.task(t, "X", a, store.StatusInProgress).ID}, a, []runner.Denial{{Tool: "Bash", Detail: "git push"}})
		f.tm.work.Unlock()
	}
	reqs, _ := f.st.ListOpenAccessRequests(f.p.ID)
	if len(reqs) != 2 || reqs[1].AgentID != f.vera.ID {
		t.Fatalf("%+v", reqs)
	}
	if err := f.tm.AllowCommandInProject(reqs[1].ID); !errors.Is(err, ErrNoProjectRule) {
		t.Fatalf("err = %v", err)
	}
	if err := f.tm.AllowCommandInProject(reqs[0].ID); err != nil {
		t.Fatal(err)
	}
	open, _ := f.st.ListOpenAccessRequests(f.p.ID)
	if len(open) != 1 || open[0].AgentID != f.vera.ID {
		t.Fatalf("cererea Verei trebuia sa ramana: %+v", open)
	}
	if p, _ := f.st.Undelivered(f.vera.ID); len(p) != 0 {
		t.Fatalf("Vera trezita degeaba: %+v", p)
	}
}

// un prefix calculat aprobat pe task trece si daca intre timp a devenit regula Ask (grant-ul ridica Ask)
func TestNoShellAllowWhenRuleIsAskNow(t *testing.T) {
	f := setup(t)
	f.rook.Tools = []string{"git"}
	f.st.UpdateAgent(f.rook)
	task := f.task(t, "Stash", f.rook, store.StatusInProgress)
	f.tm.work.Lock()
	f.tm.recordDenials(activeRun{projectID: f.p.ID, taskID: task.ID}, f.rook, []runner.Denial{{Tool: "Bash", Detail: "git stash"}})
	f.tm.work.Unlock()
	reqs, _ := f.st.ListOpenAccessRequests(f.p.ID)
	f.st.SetCommandRules(f.p.ID, []store.CommandRule{{Prefix: "git stash", Mode: store.CmdAsk}})
	if err := f.tm.AllowCommandForTask(reqs[0].ID); err != nil {
		t.Fatal(err)
	}
	if g, _ := f.st.CommandGrants(f.rook.ID, task.ID); len(g) != 1 {
		t.Fatalf("grants = %v", g)
	}
}
