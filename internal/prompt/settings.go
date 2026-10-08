package prompt

import (
	"encoding/json"
	"path/filepath"
	"slices"
	"strings"
)

// ValidTools sunt tool-urile pe care le poti bifa la un agent, in ordinea din UI.
var ValidTools = []string{"write", "shell", "packages", "git", "web"}

// toolRules traduce fiecare tool in reguli permissions.allow din Claude Code.
var toolRules = map[string][]string{
	"write": {"Edit", "Write", "NotebookEdit"},
	"shell": {"Bash"},
	"git": {"Bash(git status:*)", "Bash(git diff:*)", "Bash(git log:*)", "Bash(git add:*)",
		"Bash(git commit:*)", "Bash(git checkout -b:*)"},
	"web":      {"WebSearch", "WebFetch"},
	"packages": packageRules,
}

// packageRules sunt instalarile de pachete: refuzate implicit, permise doar
// agentilor cu tool-ul "packages" (merge si fara "shell").
var packageRules = []string{
	"Bash(npm install:*)", "Bash(composer require:*)", "Bash(go get:*)", "Bash(pip install:*)",
}

// alwaysAllow: citirea si tool-urile echipei sunt permise oricarui agent.
var alwaysAllow = []string{"Read", "Glob", "Grep", "mcp__team__*"}

// denyAlways blocheaza actiunile riscante (pana vin aprobarile) si tool-urile
// interne Claude Code care se suprapun cu echipa. Fara ele, lead-ul foloseste
// lista interna TaskCreate in loc de mcp__team__create_task (verificat 2026-09-29).
// Instalarile de pachete (packageRules) se adauga la deny doar fara tool-ul "packages".
var denyAlways = []string{
	"TaskCreate", "TaskUpdate", "TaskList", "TaskGet", "TodoWrite", "Agent",
	"Bash(git push:*)", "Bash(rm -rf:*)", "Bash(sudo:*)", "Bash(brew install:*)",
}

// denyFor: refuzurile fixe pentru un agent cu tool-urile date.
func denyFor(tools []string) []string {
	if slices.Contains(tools, "packages") {
		return denyAlways
	}
	return slices.Concat(denyAlways, packageRules)
}

// AlwaysDeniedTool spune daca tool-ul (fara argumente, ex. TaskUpdate) e in
// lista fixa de refuzuri: un refuz al lui e intentionat, nu o cerere de acces.
func AlwaysDeniedTool(tool string) bool {
	return !strings.HasPrefix(tool, "Bash(") && slices.Contains(denyAlways, tool)
}

// AlwaysDeniedBash spune daca o comanda Bash e refuzata de lista fixa (git push,
// rm -rf, sudo...), chiar si cu Shell pornit. Instalarile de pachete conteaza
// doar pentru un agent fara tool-ul "packages".
func AlwaysDeniedBash(command string, tools []string) bool {
	return bashMatches(command, denyFor(tools))
}

// NeedsPackages spune daca o comanda Bash instaleaza pachete (npm install...):
// un refuz al ei se rezolva cu tool-ul "packages".
func NeedsPackages(command string) bool {
	return bashMatches(command, packageRules)
}

// bashMatches spune daca vreo bucata din comanda se potriveste cu o regula
// Bash(prefix:*). Comenzile legate cu &&, ||, ; sau | se verifica pe bucati, ca in Claude Code.
func bashMatches(command string, rules []string) bool {
	parts := strings.FieldsFunc(command, func(r rune) bool { return r == '&' || r == '|' || r == ';' })
	for _, part := range parts {
		cmd := strings.Join(strings.Fields(part), " ") // spatii normalizate
		for _, rule := range rules {
			prefix, ok := strings.CutPrefix(rule, "Bash(")
			if !ok {
				continue
			}
			prefix = strings.TrimSuffix(prefix, ":*)")
			if cmd == prefix || strings.HasPrefix(cmd, prefix+" ") {
				return true
			}
		}
	}
	return false
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
		"permissions": map[string]any{"allow": allow, "deny": denyFor(tools)},
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
