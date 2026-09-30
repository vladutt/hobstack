package prompt

import (
	"encoding/json"
	"path/filepath"
	"slices"
	"strings"
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
// home e folderul agentului: cu tool-ul write, scrie doar acolo ("" = oriunde).
func Settings(tools []string, home string) string {
	allow := slices.Clone(alwaysAllow)
	for _, name := range ValidTools {
		if !slices.Contains(tools, name) {
			continue
		}
		if name == "write" && home != "" {
			for _, tool := range toolRules["write"] {
				allow = append(allow, tool+"("+pathRule(home)+")")
			}
			continue
		}
		allow = append(allow, toolRules[name]...)
	}
	b, _ := json.Marshal(map[string]any{
		"permissions": map[string]any{"allow": allow, "deny": denyAlways},
	})
	return string(b)
}

// pathRule: tot ce e sub dir, in sintaxa regulilor Claude Code. O cale absoluta
// incepe cu "//" (ex. //Users/ana/shop/**); pe Windows C:\x devine //c/x/**.
func pathRule(dir string) string {
	p := filepath.Clean(dir)
	if v := filepath.VolumeName(p); len(v) == 2 && v[1] == ':' {
		p = "/" + strings.ToLower(v[:1]) + p[2:]
	}
	return "/" + strings.TrimSuffix(filepath.ToSlash(p), "/") + "/**"
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
