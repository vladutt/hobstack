package prompt

import (
	"encoding/json"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"foreman/internal/store"
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

// denyAlways blocheaza tool-urile interne Claude Code care se suprapun cu echipa.
// Fara ele, lead-ul foloseste lista interna TaskCreate in loc de mcp__team__create_task
// (verificat 2026-09-29). Comenzile Bash riscante (git push, sudo...) nu mai sunt aici:
// vin din regulile de comenzi (store.CommandRule), prin cmdDeny / AlwaysDeniedBash.
// Instalarile de pachete (packageRules) se adauga la deny doar fara tool-ul "packages".
var denyAlways = []string{
	"TaskCreate", "TaskUpdate", "TaskList", "TaskGet", "TodoWrite", "Agent",
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

// AlwaysDeniedBash spune daca o comanda Bash e refuzata chiar si cu Shell pornit:
// de regulile Ask / Block (git push, sudo...) sau de instalarile de pachete, care conteaza
// doar pentru un agent fara tool-ul "packages".
func AlwaysDeniedBash(command string, tools []string, rules []store.CommandRule) bool {
	if bashMatches(command, denyFor(tools)) {
		return true
	}
	mode, _ := MatchCommand(command, rules)
	return mode == store.CmdAsk || mode == store.CmdBlock
}

// NeedsPackages spune daca o comanda Bash instaleaza pachete (npm install...):
// un refuz al ei se rezolva cu tool-ul "packages".
func NeedsPackages(command string) bool {
	return bashMatches(command, packageRules)
}

// bashMatches spune daca vreo bucata din comanda se potriveste cu o regula
// Bash(prefix:*). Comenzile legate cu &&, ||, ; sau | se verifica pe bucati, ca in Claude Code.
func bashMatches(command string, rules []string) bool {
	for _, cmd := range splitCommand(command) {
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
// extraAllow: serverele MCP din Library; se accepta doar "mcp__<server>" (tot serverul),
// nimic cu wildcard, paranteze sau tool anume, ca Library sa nu poata largi permisiunile.
// cmdDeny: prefixele de comenzi refuzate (din DenyPrefixes), puse in deny ca Bash(prefix:*).
func Settings(tools []string, home string, extraAllow, cmdDeny []string) string {
	allow := slices.Clone(alwaysAllow)
	for _, rule := range extraAllow {
		if libraryServerRule.MatchString(rule) && rule != "mcp__team" {
			allow = append(allow, rule)
		}
	}
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
	deny := slices.Clone(denyFor(tools))
	for _, p := range cmdDeny {
		deny = append(deny, "Bash("+p+":*)")
	}
	b, _ := json.Marshal(map[string]any{
		"permissions": map[string]any{"allow": allow, "deny": deny},
	})
	return string(b)
}

// libraryServerRule: numele serverului are forma sigura din Library (fara "__", deci fara tool anume).
var libraryServerRule = regexp.MustCompile(`^mcp__[a-z][a-z0-9-]{1,39}$`)

// pathRule: tot ce e sub dir, in sintaxa regulilor Claude Code. O cale absoluta
// incepe cu "//" (ex. //Users/ana/shop/**); pe Windows C:\x devine //c/x/**.
func pathRule(dir string) string {
	p := filepath.Clean(dir)
	if v := filepath.VolumeName(p); len(v) == 2 && v[1] == ':' {
		p = "/" + strings.ToLower(v[:1]) + p[2:]
	}
	return "/" + strings.TrimSuffix(filepath.ToSlash(p), "/") + "/**"
}

// MCPConfig intoarce JSON-ul pentru --mcp-config: serverul "team" (chiar aplicatia
// noastra) plus serverele din Library instalate in proiect (extra poate fi nil).
// Un server din Library nu poate lua numele "team".
func MCPConfig(url string, extra map[string]any) string {
	servers := map[string]any{"team": map[string]any{"type": "http", "url": url}}
	for name, cfg := range extra {
		if name != "team" {
			servers[name] = cfg
		}
	}
	b, _ := json.Marshal(map[string]any{"mcpServers": servers})
	return string(b)
}
