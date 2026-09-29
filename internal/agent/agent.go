package agent

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Un agent este un folder din ~/.agentdash/agents.
// Claude Code ruleaza cu acel folder ca working directory si citeste singur
// CLAUDE.md. .claude/settings.json si .mcp.json le da runner-ul explicit
// (--settings / --mcp-config), altfel ar fi ignorate fara "trust".
type Agent struct {
	Name        string `json:"name"`
	Dir         string `json:"-"`
	HasClaudeMD bool   `json:"hasClaudeMd"`
}

var ErrNotFound = errors.New("agent inexistent")

func List(root string) ([]Agent, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	var out []Agent
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		out = append(out, load(root, e.Name()))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func Get(root, name string) (Agent, error) {
	// protectie contra path traversal (../, /, etc.)
	if name == "" || filepath.Base(name) != name || strings.HasPrefix(name, ".") {
		return Agent{}, ErrNotFound
	}
	info, err := os.Stat(filepath.Join(root, name))
	if err != nil || !info.IsDir() {
		return Agent{}, ErrNotFound
	}
	return load(root, name), nil
}

func load(root, name string) Agent {
	dir := filepath.Join(root, name)
	_, err := os.Stat(filepath.Join(dir, "CLAUDE.md"))
	return Agent{Name: name, Dir: dir, HasClaudeMD: err == nil}
}

// EnsureDemo creeaza un agent demo daca folderul de agenti e gol,
// ca prima pornire sa aiba ceva de rulat.
func EnsureDemo(root string) error {
	agents, err := List(root)
	if err != nil || len(agents) > 0 {
		return err
	}
	dir := filepath.Join(root, "demo")
	if err := os.MkdirAll(filepath.Join(dir, ".claude"), 0o755); err != nil {
		return err
	}
	claudeMD := "# Agent demo\n\nEsti un agent de test pentru agentdash. Raspunde scurt, in romana.\nPoti citi fisiere din acest folder, dar nu modifica nimic.\n"
	settings := "{\n  \"permissions\": {\n    \"allow\": [\"Read\", \"Glob\", \"Grep\"]\n  }\n}\n"
	if err := os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte(claudeMD), 0o644); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, ".claude", "settings.json"), []byte(settings), 0o644)
}
