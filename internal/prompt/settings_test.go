package prompt

import (
	"strings"
	"testing"
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
	shell := []string{"shell"}
	for _, cmd := range []string{"git push origin main", "rm -rf build", "sudo ls", "npm install", "  npm   install x", "cd web && npm install"} {
		if !AlwaysDeniedBash(cmd, shell) {
			t.Errorf("%q e refuzata fix", cmd)
		}
	}
	for _, cmd := range []string{"npm test", "git status", "rm file.txt", "go test ./...", "npm installer"} {
		if AlwaysDeniedBash(cmd, shell) {
			t.Errorf("%q nu e refuzata fix", cmd)
		}
	}
	// cu "packages", instalarile trec; restul refuzurilor fixe raman
	pkgs := []string{"shell", "packages"}
	if AlwaysDeniedBash("cd app && npm install", pkgs) || !AlwaysDeniedBash("npm install && git push", pkgs) || !AlwaysDeniedBash("brew install jq", pkgs) {
		t.Error("packages permite doar instalarile de pachete")
	}
	if !NeedsPackages("cd app && npm install 2>&1 | tail -8") || NeedsPackages("npm run build") {
		t.Error("NeedsPackages")
	}
}

func TestSettingsPackages(t *testing.T) {
	without, with := Settings([]string{"shell"}, ""), Settings([]string{"packages"}, "")
	if !strings.Contains(without, `"deny":[`) || !strings.Contains(without[strings.Index(without, `"deny"`):], "npm install") {
		t.Errorf("fara packages, npm install e in deny: %s", without)
	}
	allow, deny, _ := strings.Cut(with, `"deny"`)
	if !strings.Contains(allow, "Bash(npm install:*)") || strings.Contains(deny, "npm install") || !strings.Contains(deny, "brew install") {
		t.Errorf("cu packages, npm install e permis: %s", with)
	}
}
