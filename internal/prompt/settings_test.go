package prompt

import (
	"encoding/json"
	"slices"
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
	without, with := Settings([]string{"shell"}, "", nil, nil, nil), Settings([]string{"packages"}, "", nil, []string{"brew install"}, nil)
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
	if !strings.Contains(Settings([]string{"write"}, "/w", []string{"mcp__stock-db"}, nil, nil), `"mcp__stock-db"`) {
		t.Fatal("allow-ul extra lipseste")
	}
}

func TestSettingsExtraAllowOnlyWholeServers(t *testing.T) {
	got := Settings(nil, "", []string{"mcp__ok", "Bash", "mcp__team__*", "mcp__x__tool", "mcp__a(b)", "mcp__*"}, nil, nil)
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
	got := Settings([]string{"shell"}, "/w", nil, []string{"git push", "sudo"}, nil)
	for _, want := range []string{`"Bash(git push:*)"`, `"Bash(sudo:*)"`, `"TaskCreate"`, `"Bash"`} {
		if !strings.Contains(got, want) {
			t.Fatalf("lipseste %s din %s", want, got)
		}
	}
	if strings.Contains(got, "rm -rf") {
		t.Fatal("rm -rf nu mai e fix: vine doar din reguli")
	}
}

func TestSettingsGranted(t *testing.T) {
	raw := Settings([]string{"git"}, "", nil, []string{"sudo"}, []string{"git stash", "sudo", "bad)*"})
	var cfg struct {
		Permissions struct{ Allow, Deny []string } `json:"permissions"`
	}
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(cfg.Permissions.Allow, "Bash(git stash:*)") {
		t.Fatalf("grant-ul lipseste din allow: %s", raw)
	}
	// Block ramane in deny chiar daca apare si in grant-uri (deny castiga in claude)
	if !slices.Contains(cfg.Permissions.Deny, "Bash(sudo:*)") {
		t.Fatalf("%s", raw)
	}
	if strings.Contains(raw, "bad)") {
		t.Fatalf("prefix invalid acceptat: %s", raw)
	}
}

func TestReadOnlyTools(t *testing.T) {
	got := ReadOnlyTools([]string{"write", "shell", "packages", "git", "web"})
	if !slices.Equal(got, []string{"git-read", "web"}) {
		t.Fatalf("%v", got)
	}
	if got := ReadOnlyTools(nil); len(got) != 0 {
		t.Fatalf("%v", got)
	}
	raw := Settings(ReadOnlyTools([]string{"write", "shell", "git"}), "/w", nil, nil, nil)
	var cfg struct {
		Permissions struct{ Allow, Deny []string } `json:"permissions"`
	}
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		t.Fatal(err)
	}
	for _, a := range cfg.Permissions.Allow {
		if a == "Bash" || a == "Bash(git add:*)" || a == "Bash(git commit:*)" || strings.HasPrefix(a, "Edit") || strings.HasPrefix(a, "Write") {
			t.Fatalf("%s in allow: %s", a, raw)
		}
	}
	for _, want := range []string{"Read", "mcp__team__*", "Bash(git status:*)", "Bash(git diff:*)", "Bash(git log:*)", "Bash(git show:*)", "Bash(git blame:*)"} {
		if !slices.Contains(cfg.Permissions.Allow, want) {
			t.Fatalf("lipseste %s: %s", want, raw)
		}
	}
}
