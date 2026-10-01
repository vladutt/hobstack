package prompt

import "testing"

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
	for _, cmd := range []string{"git push origin main", "rm -rf build", "sudo ls", "npm install", "  npm   install x", "cd web && npm install"} {
		if !AlwaysDeniedBash(cmd) {
			t.Errorf("%q e refuzata fix", cmd)
		}
	}
	for _, cmd := range []string{"npm test", "git status", "rm file.txt", "go test ./...", "npm installer"} {
		if AlwaysDeniedBash(cmd) {
			t.Errorf("%q nu e refuzata fix", cmd)
		}
	}
}
