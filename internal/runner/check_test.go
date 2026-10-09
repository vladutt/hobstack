//go:build !windows

package runner

import (
	"context"
	"strings"
	"testing"
)

func TestSpecEnvReplacesInheritedAuth(t *testing.T) {
	t.Setenv("FAKE_MODE", "env")
	t.Setenv("ANTHROPIC_API_KEY", "inherited")
	t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "") // shell-ul dezvoltatorului poate avea unul
	m := fakeManager(t)

	_, done := start(t, m, Spec{ID: "e1", Key: "agent-1", Dir: t.TempDir(), Env: []string{"CLAUDE_CODE_OAUTH_TOKEN=work-token"}})
	if o := wait(t, done); o.Result != "token=work-token key=" {
		t.Fatalf("cu cont: %q", o.Result)
	}
	_, done = start(t, m, Spec{ID: "e2", Key: "agent-1", Dir: t.TempDir()})
	if o := wait(t, done); o.Result != "token= key=inherited" {
		t.Fatalf("Default trebuie sa mosteneasca mediul: %q", o.Result)
	}
}

func TestCheck(t *testing.T) {
	m := fakeManager(t)
	t.Setenv("FAKE_MODE", "")
	if err := m.Check(context.Background(), []string{"CLAUDE_CODE_OAUTH_TOKEN=x"}); err != nil {
		t.Fatalf("verificare reusita: %v", err)
	}
	t.Setenv("FAKE_MODE", "notlogged")
	if err := m.Check(context.Background(), []string{"CLAUDE_CODE_OAUTH_TOKEN=x"}); err == nil || !strings.Contains(err.Error(), "Not logged in") {
		t.Fatalf("token respins: %v", err)
	}
	t.Setenv("FAKE_MODE", "crash")
	if err := m.Check(context.Background(), nil); err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("crash: %v", err)
	}
	missing := NewManager("/nu/exista/claude", t.TempDir())
	if err := missing.Check(context.Background(), nil); err == nil || !strings.Contains(err.Error(), "cannot run claude") {
		t.Fatalf("binar lipsa: %v", err)
	}
}

func TestAuthStatus(t *testing.T) {
	st, err := fakeManager(t).AuthStatus(context.Background())
	if err != nil || !st.LoggedIn || st.Email != "me@example.com" || st.SubscriptionType != "max" {
		t.Fatalf("AuthStatus = %+v, %v", st, err)
	}
}
