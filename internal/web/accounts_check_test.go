//go:build !windows

package web

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"foreman/internal/runner"
)

// withFakeClaude: runner-ul serverului foloseste claude-ul fals din runner/testdata.
func withFakeClaude(t *testing.T, e *env) {
	t.Helper()
	bin, err := filepath.Abs("../runner/testdata/fakeclaude.sh")
	if err != nil {
		t.Fatal(err)
	}
	e.srv.runs = runner.NewManager(bin, t.TempDir())
}

func TestAccountCheckAndDefault(t *testing.T) {
	e := setup(t)
	withFakeClaude(t, e)
	t.Setenv("FAKE_MODE", "")
	id := e.addAccount(t, "Work", "sk-ant-oat01-token-1234")
	var a struct {
		LastError string
		CheckedAt *string
	}
	_, raw := e.do(t, "GET", "/api/accounts", nil)
	var list []json.RawMessage
	json.Unmarshal(raw, &list)
	json.Unmarshal(list[0], &a)
	if a.LastError != "" || a.CheckedAt == nil {
		t.Fatalf("verificare reusita: %s", list[0])
	}

	t.Setenv("FAKE_MODE", "notlogged")
	_, raw = e.do(t, "PUT", "/api/accounts/"+itoa(id), map[string]any{"token": "sk-ant-oat01-other-5678"})
	json.Unmarshal(raw, &a)
	if a.LastError != "Not logged in · Please run /login" {
		t.Fatalf("token nou respins: %s", raw)
	}

	_, raw = e.do(t, "GET", "/api/accounts/default", nil)
	var def runner.AuthStatus
	json.Unmarshal(raw, &def)
	if !def.LoggedIn || def.Email != "me@example.com" {
		t.Fatalf("default: %s", raw)
	}
}
