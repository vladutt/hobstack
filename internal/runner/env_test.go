package runner

import (
	"slices"
	"testing"
)

func TestCleanEnv(t *testing.T) {
	in := []string{"PATH=/bin", "ANTHROPIC_API_KEY=k", "CLAUDE_CODE_OAUTH_TOKEN=t", "ANTHROPIC_BASE_URL=http://gw",
		"ANTHROPIC_AUTH_TOKEN=a", "CLAUDE_CODE_USE_BEDROCK=1", "CLAUDE_CODE_USE_VERTEX=1", "HOME=/h", "ANTHROPIC_MODEL=x"}
	got := cleanEnv(in)
	want := []string{"PATH=/bin", "HOME=/h", "ANTHROPIC_MODEL=x"}
	if !slices.Equal(got, want) {
		t.Fatalf("cleanEnv = %v", got)
	}
}

func TestProcEnv(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "inherited")
	if procEnv(nil) != nil {
		t.Fatal("fara env propriu procesul mosteneste tot (nil)")
	}
	env := procEnv([]string{"CLAUDE_CODE_OAUTH_TOKEN=work"})
	if slices.Contains(env, "ANTHROPIC_API_KEY=inherited") || !slices.Contains(env, "CLAUDE_CODE_OAUTH_TOKEN=work") {
		t.Fatalf("procEnv = %v", env)
	}
}

func TestIsAuthError(t *testing.T) {
	yes := []string{
		`API Error: 401 {"type":"error","error":{"type":"authentication_error","message":"OAuth token has expired."}}`,
		"Not logged in · Please run /login",
		"Invalid API key · Fix external API key",
		"OAuth token revoked",
		"Failed to authenticate. API Error: 401 OAuth access token is invalid.", // claude 2.1.295, token gresit
		"Failed to authenticate.",
	}
	no := []string{"", "Reached maximum budget ($1)", "the folder Client does not exist", "fix T-401 first", "claude exited without a result"}
	for _, m := range yes {
		if !IsAuthError(m) {
			t.Errorf("trebuia recunoscut: %q", m)
		}
	}
	for _, m := range no {
		if IsAuthError(m) {
			t.Errorf("nu e eroare de autentificare: %q", m)
		}
	}
}
