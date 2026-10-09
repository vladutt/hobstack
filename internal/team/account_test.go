package team

import (
	"slices"
	"strings"
	"testing"

	"foreman/internal/runner"
	"foreman/internal/store"
)

// useAccount muta proiectul fixture-ului pe un cont nou.
func useAccount(t *testing.T, f *fixture) store.Account {
	t.Helper()
	a, err := f.st.AddAccount("Work", "sk-ant-oat01-work-1234")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.st.SetProjectAccount(f.p.ID, &a.ID, "switched"); err != nil {
		t.Fatal(err)
	}
	return a
}

func TestRunUsesProjectAccount(t *testing.T) {
	f := setup(t)
	f.task(t, "A", f.rook, store.StatusQueued)
	mustReconcile(t, f)
	if env := f.fs.last(t, f.rook).Env; env != nil {
		t.Fatalf("Default: Env = %v", env)
	}

	g := setup(t)
	useAccount(t, g)
	g.task(t, "A", g.rook, store.StatusQueued)
	mustReconcile(t, g)
	env := g.fs.last(t, g.rook).Env
	if !slices.Equal(env, []string{"CLAUDE_CODE_OAUTH_TOKEN=sk-ant-oat01-work-1234"}) {
		t.Fatalf("cu cont: Env = %v", env)
	}
	if strings.Contains(strings.Join(g.fs.last(t, g.rook).Args, " "), "sk-ant") {
		t.Fatal("token-ul a ajuns in linia de comanda")
	}
}

func TestAuthErrorPausesProject(t *testing.T) {
	f := setup(t)
	acct := useAccount(t, f)
	f.task(t, "A", f.rook, store.StatusQueued)
	mustReconcile(t, f)
	spec := f.fs.last(t, f.rook)
	msg := `API Error: 401 {"type":"error","error":{"type":"authentication_error","message":"OAuth token has expired."}}`
	f.fs.finish(spec.ID, runner.Outcome{Status: store.RunFailed, Error: msg})

	if p, _ := f.st.GetProject(f.p.ID); !p.Paused {
		t.Fatal("proiectul trebuia pus pe pauza")
	}
	if got, _ := f.st.Account(acct.ID); !strings.Contains(got.LastError, "OAuth token has expired") {
		t.Fatalf("LastError = %q", got.LastError)
	}
	f.wantStatus(t, 1, store.StatusQueued) // nu Review cu "run failed": munca nu e vina agentului
	if !f.hasMessage(f.rook.ID, "Claude account Work failed to sign in") {
		t.Fatal("lipseste nota din thread")
	}
	before := f.fs.count()
	mustReconcile(t, f) // pe pauza: nu se reincearca in bucla
	if f.fs.count() != before {
		t.Fatal("un proiect pe pauza a pornit run-uri")
	}
}

func TestAuthErrorOnDefaultPauses(t *testing.T) {
	f := setup(t)
	f.task(t, "A", f.rook, store.StatusQueued)
	mustReconcile(t, f)
	f.fs.finish(f.fs.last(t, f.rook).ID, runner.Outcome{Status: store.RunFailed, Error: "Not logged in · Please run /login"})
	if p, _ := f.st.GetProject(f.p.ID); !p.Paused {
		t.Fatal("proiectul trebuia pus pe pauza")
	}
	f.wantStatus(t, 1, store.StatusQueued)
	if !f.hasMessage(f.rook.ID, "The logged-in Claude CLI failed to sign in") {
		t.Fatal("lipseste nota din thread")
	}
}
