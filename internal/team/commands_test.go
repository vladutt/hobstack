package team

import (
	"errors"
	"strings"
	"testing"

	"foreman/internal/runner"
	"foreman/internal/store"
)

func argAfter(args []string, flag string) string {
	for i, a := range args {
		if a == flag && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
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
	if err := f.tm.AllowCommandOnce(reqs[0].ID); err != nil {
		t.Fatal(err)
	}
	pending, _ := f.st.Undelivered(f.rook.ID)
	if len(pending) == 0 || !strings.Contains(pending[len(pending)-1].Text, "allowed once") {
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
	f.tm.recordDenials(activeRun{projectID: f.p.ID}, f.rook, []runner.Denial{{Tool: "Bash", Detail: "rm -rf build"}})
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
	if err := f.tm.AllowCommandOnce(reqs[len(reqs)-1].ID); err != ErrNotCommandRequest {
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
	if err := f.tm.AllowCommandOnce(reqs[0].ID); err != nil {
		t.Fatal(err)
	}
	// a doua apasare pe aceeasi cerere nu mai face nimic
	if err := f.tm.AllowCommandOnce(reqs[0].ID); !errors.Is(err, ErrRequestClosed) {
		t.Fatalf("err = %v", err)
	}
	if err := f.tm.AllowCommandOnce(99999); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
	f.wantStatus(t, task.Number, store.StatusQueued)
	if err := f.tm.Reconcile(f.p.ID); err != nil {
		t.Fatal(err)
	}
	run := f.fs.last(t, f.rook)
	if strings.Contains(argAfter(run.Args, "--settings"), "Bash(git push:*)") {
		t.Fatal("run-ul cu grant are git push in deny")
	}
	f.fs.finish(run.ID, runner.Outcome{Status: store.RunDone})
	f.tm.work.Lock()
	_, err := f.tm.addUserMessage(f.rook, "again", nil, false, false)
	f.tm.work.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if err := f.tm.Reconcile(f.p.ID); err != nil {
		t.Fatal(err)
	}
	next := f.fs.last(t, f.rook)
	if next.ID == run.ID {
		t.Fatal("nu a pornit al doilea run")
	}
	if !strings.Contains(argAfter(next.Args, "--settings"), "Bash(git push:*)") {
		t.Fatal("grant-ul a fost doar pentru un run")
	}
}

func TestGrantRestoredWhenStartFails(t *testing.T) {
	f := setup(t)
	f.rook.Tools = []string{"shell"}
	f.st.UpdateAgent(f.rook)
	f.task(t, "Ship", f.rook, store.StatusQueued)
	f.st.AddCommandGrant(f.p.ID, f.rook.ID, "git push")
	f.fs.failErr = errors.New("boom")
	f.tm.Reconcile(f.p.ID) // eroarea de pornire ajunge ca nota
	g, _ := f.st.TakeCommandGrants(f.rook.ID)
	if len(g) != 1 || g[0] != "git push" {
		t.Fatalf("grants = %v", g)
	}
}

func TestDenyingRuleFallback(t *testing.T) {
	f := setup(t)
	f.rook.Tools = []string{"shell"}
	f.st.UpdateAgent(f.rook)
	f.st.SetCommandRules(f.p.ID, []store.CommandRule{{Prefix: "git", Mode: store.CmdAsk}, {Prefix: "git status", Mode: store.CmdAllow}})
	// regula decisiva e Allow (git status), dar claude a refuzat din cauza lui "git" (Ask)
	f.tm.work.Lock()
	f.tm.recordDenials(activeRun{projectID: f.p.ID}, f.rook, []runner.Denial{{Tool: "Bash", Detail: "git status"}})
	f.tm.work.Unlock()
	reqs, _ := f.st.ListOpenAccessRequests(f.p.ID)
	if len(reqs) != 1 || reqs[0].Prefix != "git" {
		t.Fatalf("%+v", reqs)
	}
}
