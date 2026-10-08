package prompt

import (
	"strings"
	"testing"

	"foreman/internal/store"
)

func TestAlwaysDenied(t *testing.T) {
	for _, tool := range []string{"TaskCreate", "TaskUpdate", "TaskList", "TaskGet", "TodoWrite", "Agent"} {
		if !AlwaysDeniedTool(tool) {
			t.Errorf("%s e in lista fixa", tool)
		}
	}
	for _, tool := range []string{"Bash", "Write", "WebFetch", "Bash(sudo:*)"} {
		if AlwaysDeniedTool(tool) {
			t.Errorf("%s nu e un tool refuzat fix", tool)
		}
	}
	if !NeedsPackages("cd app && npm install 2>&1 | tail -8") || NeedsPackages("npm run build") {
		t.Error("NeedsPackages")
	}
}

func TestSettingsPackages(t *testing.T) {
	without, with := Settings([]string{"shell"}, "", nil, nil), Settings([]string{"packages"}, "", nil, []string{"brew install"})
	if !strings.Contains(without, `"deny":[`) || !strings.Contains(without[strings.Index(without, `"deny"`):], "npm install") {
		t.Errorf("fara packages, npm install e in deny: %s", without)
	}
	allow, deny, _ := strings.Cut(with, `"deny"`)
	if !strings.Contains(allow, "Bash(npm install:*)") || strings.Contains(deny, "npm install") || !strings.Contains(deny, "brew install") {
		t.Errorf("cu packages, npm install e permis: %s", with)
	}
}

func TestMCPConfigExtra(t *testing.T) {
	got := MCPConfig("http://x/mcp/t", map[string]any{"stock-db": map[string]any{"command": "python3"}, "team": map[string]any{"command": "evil"}})
	if !strings.Contains(got, `"team"`) || !strings.Contains(got, `"stock-db"`) || strings.Contains(got, "evil") {
		t.Fatal(got)
	}
	if !strings.Contains(Settings([]string{"write"}, "/w", []string{"mcp__stock-db"}, nil), `"mcp__stock-db"`) {
		t.Fatal("allow-ul extra lipseste")
	}
}

func TestSettingsExtraAllowOnlyWholeServers(t *testing.T) {
	got := Settings(nil, "", []string{"mcp__ok", "Bash", "mcp__team__*", "mcp__x__tool", "mcp__a(b)", "mcp__*"}, nil)
	for _, bad := range []string{`"Bash"`, `mcp__x__tool`, `mcp__a(b)`, `"mcp__*"`} {
		if strings.Contains(got, bad) {
			t.Fatalf("allow nepermis %s: %s", bad, got)
		}
	}
	if strings.Count(got, `"mcp__team__*"`) != 1 { // doar cel fix
		t.Fatal(got)
	}
	if !strings.Contains(got, `"mcp__ok"`) {
		t.Fatal(got)
	}
}

func TestAlwaysDeniedBashWithRules(t *testing.T) {
	eff := []store.CommandRule{{Prefix: "git push", Mode: store.CmdAsk}, {Prefix: "sudo", Mode: store.CmdBlock}}
	if !AlwaysDeniedBash("sudo ls", []string{"shell"}, eff) || !AlwaysDeniedBash("git push", nil, eff) {
		t.Fatal("regulile Ask/Block trebuie sa conteze")
	}
	if AlwaysDeniedBash("ls", nil, eff) || !AlwaysDeniedBash("npm install x", nil, eff) || AlwaysDeniedBash("npm install x", []string{"packages"}, eff) {
		t.Fatal("pachetele raman legate de tool-ul packages")
	}
	if AlwaysDeniedBash("git push", nil, nil) {
		t.Fatal("fara reguli, git push nu mai e refuzat fix")
	}
}

func TestSettingsCommandDeny(t *testing.T) {
	got := Settings([]string{"shell"}, "/w", nil, []string{"git push", "sudo"})
	for _, want := range []string{`"Bash(git push:*)"`, `"Bash(sudo:*)"`, `"TaskCreate"`, `"Bash"`} {
		if !strings.Contains(got, want) {
			t.Fatalf("lipseste %s din %s", want, got)
		}
	}
	if strings.Contains(got, "rm -rf") {
		t.Fatal("rm -rf nu mai e fix: vine doar din reguli")
	}
}
