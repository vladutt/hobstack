package prompt

import (
	"encoding/json"
	"slices"
)

// ValidTools sunt tool-urile pe care le poti bifa la un agent, in ordinea din UI.
var ValidTools = []string{"write", "shell", "git", "web"}

// toolRules traduce fiecare tool in reguli permissions.allow din Claude Code.
var toolRules = map[string][]string{
	"write": {"Edit", "Write", "NotebookEdit"},
	"shell": {"Bash"},
	"git": {"Bash(git status:*)", "Bash(git diff:*)", "Bash(git log:*)", "Bash(git add:*)",
		"Bash(git commit:*)", "Bash(git checkout -b:*)"},
	"web": {"WebSearch", "WebFetch"},
}

// alwaysAllow: citirea si tool-urile echipei sunt permise oricarui agent.
var alwaysAllow = []string{"Read", "Glob", "Grep", "mcp__team__*"}

// denyAlways blocheaza actiunile riscante (pana vin aprobarile) si tool-urile
// interne Claude Code care se suprapun cu echipa. Fara ele, lead-ul foloseste
// lista interna TaskCreate in loc de mcp__team__create_task (verificat 2026-09-29).
var denyAlways = []string{
	"TaskCreate", "TaskUpdate", "TaskList", "TaskGet", "TodoWrite", "Agent",
	"Bash(git push:*)", "Bash(rm -rf:*)", "Bash(sudo:*)",
	"Bash(npm install:*)", "Bash(composer require:*)", "Bash(go get:*)",
	"Bash(pip install:*)", "Bash(brew install:*)",
}

// Settings intoarce JSON-ul pentru --settings. In modul -p, ce lipseste din
// allow sau apare in deny e refuzat automat, fara sa intrebe pe nimeni.
func Settings(tools []string) string {
	allow := slices.Clone(alwaysAllow)
	for _, name := range ValidTools {
		if slices.Contains(tools, name) {
			allow = append(allow, toolRules[name]...)
		}
	}
	b, _ := json.Marshal(map[string]any{
		"permissions": map[string]any{"allow": allow, "deny": denyAlways},
	})
	return string(b)
}

// MCPConfig intoarce JSON-ul pentru --mcp-config: un singur server, "team",
// care e chiar aplicatia noastra.
func MCPConfig(url string) string {
	b, _ := json.Marshal(map[string]any{
		"mcpServers": map[string]any{
			"team": map[string]any{"type": "http", "url": url},
		},
	})
	return string(b)
}
