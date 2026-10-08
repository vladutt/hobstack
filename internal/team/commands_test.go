package team

import (
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
	if got, _ := f.st.GetTask(task.ID); got.Status != store.StatusQueued && got.Status != store.StatusInProgress {
		t.Fatalf("status = %s", got.Status)
	}
	if r, _ := f.st.ListOpenAccessRequests(f.p.ID); len(r) != 0 {
		t.Fatal("cererea a ramas deschisa")
	}
	// grant-ul: Kick poate porni deja run-ul lui Rook (si consuma grant-ul); atunci --settings al acelui run
	// nu are git push in deny, altfel grant-ul e inca acolo
	if spec, ok := f.fs.lastOK(f.rook); ok {
		if strings.Contains(argAfter(spec.Args, "--settings"), "git push") {
			t.Fatal("run-ul de dupa Allow once are git push in deny")
		}
	} else if g, _ := f.st.TakeCommandGrants(f.rook.ID); len(g) != 1 || g[0] != "git push" {
		t.Fatalf("%v", g)
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
