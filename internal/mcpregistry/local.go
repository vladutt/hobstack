package mcpregistry

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strings"
)

// Sursele afisate in UI.
const (
	SourceClaudeCode    = "Claude Code"
	SourceClaudeDesktop = "Claude Desktop"
)

// LocalServer e un server MCP gasit in configurarile Claude ale utilizatorului.
// Env are DOAR numele variabilelor; valorile (si header-ele remote) nu se citesc mai departe.
type LocalServer struct {
	Source        string   `json:"source"`
	Name          string   `json:"name"`
	Kind          string   `json:"kind"` // "local" | "remote"
	Command       string   `json:"command"`
	Args          []string `json:"args"`
	URL           string   `json:"url"`
	Env           []EnvVar `json:"env"`
	SuggestedName string   `json:"suggestedName"`
	Installable   bool     `json:"installable"`
	Reason        string   `json:"reason,omitempty"` // de ce nu se poate instala
}

// configServer e o intrare din mcpServers. Din env si headers decodam doar cheile (valorile raman
// RawMessage si nu ies din functie).
type configServer struct {
	Type    string                     `json:"type"`
	Command string                     `json:"command"`
	Args    []string                   `json:"args"`
	URL     string                     `json:"url"`
	Env     map[string]json.RawMessage `json:"env"`
	Headers map[string]json.RawMessage `json:"headers"`
}

// claudeConfig e decodat pe bucati: o intrare stricata sare doar ea, nu tot fisierul.
type claudeConfig struct {
	MCPServers json.RawMessage            `json:"mcpServers"`
	Projects   map[string]json.RawMessage `json:"projects"`
}

// decodeServers decodeaza un obiect mcpServers intrare cu intrare; intrarile invalide sunt sarite.
func decodeServers(raw json.RawMessage) map[string]configServer {
	var entries map[string]json.RawMessage
	if json.Unmarshal(raw, &entries) != nil {
		return nil
	}
	out := map[string]configServer{}
	for name, e := range entries {
		var c configServer
		if json.Unmarshal(e, &c) == nil {
			out[name] = c
		}
	}
	return out
}

// projectServers scoate mcpServers dintr-o intrare din projects (sau nimic, daca nu e obiect).
func projectServers(raw json.RawMessage) map[string]configServer {
	var p struct {
		MCPServers json.RawMessage `json:"mcpServers"`
	}
	if json.Unmarshal(raw, &p) != nil {
		return nil
	}
	return decodeServers(p.MCPServers)
}

// LocalServers citeste ~/.claude.json (mcpServers de sus + projects.*.mcpServers) si configurarea
// Claude Desktop. Fisierele lipsa sau cu JSON invalid sunt ignorate. Ordinea: Claude Code (sus, apoi
// proiectele sortate dupa cale), apoi Claude Desktop; in fiecare grup sortate dupa nume.
func LocalServers(home string) []LocalServer {
	out := []LocalServer{}
	seen := map[string]bool{}
	add := func(source string, servers map[string]configServer) {
		names := make([]string, 0, len(servers))
		for n := range servers {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, n := range names {
			s, ok := toLocal(source, n, servers[n])
			if !ok {
				continue
			}
			// acelasi server (sursa + nume + comanda/URL) din mai multe proiecte apare o data
			key := source + "\x00" + n + "\x00" + s.Command + "\x00" + strings.Join(s.Args, "\x00") + "\x00" + s.URL
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, s)
		}
	}

	var code claudeConfig
	if readJSON(filepath.Join(home, ".claude.json"), &code) {
		add(SourceClaudeCode, decodeServers(code.MCPServers))
		paths := make([]string, 0, len(code.Projects))
		for p := range code.Projects {
			paths = append(paths, p)
		}
		sort.Strings(paths)
		for _, p := range paths {
			add(SourceClaudeCode, projectServers(code.Projects[p]))
		}
	}

	var desktop claudeConfig
	if readJSON(desktopConfigPath(home, runtime.GOOS), &desktop) {
		add(SourceClaudeDesktop, decodeServers(desktop.MCPServers))
	}
	return out
}

func toLocal(source, name string, c configServer) (LocalServer, bool) {
	s := LocalServer{Source: source, Name: name, SuggestedName: SuggestName(name)}
	switch {
	case c.Type == "http" || c.Type == "sse" || c.Type == "streamable-http" || (c.Command == "" && c.URL != ""):
		if c.URL == "" {
			return LocalServer{}, false
		}
		s.Kind = "remote"
		s.URL = c.URL
	case c.Command != "":
		s.Kind = "local"
		s.Command = c.Command
		s.Args = slices.Clone(c.Args)
	default:
		return LocalServer{}, false
	}
	keys := make([]string, 0, len(c.Env))
	for k := range c.Env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		s.Env = append(s.Env, EnvVar{Name: k, Stored: true})
	}

	// credentialele din argumente / URL se ascund aici, ca valoarea sa nu iasa din proces
	// Limita acceptata: un secret aflat doar in calea unui URL sau o valoare goala fara nume dupa care
	// sa-l recunoastem (de ex. calea unui webhook Slack) nu poate fi gasit; utilizatorul confirma comanda la Install.
	var hidden, h bool
	s.Args, hidden = redactList(s.Args)
	if r, hc := redactList([]string{s.Command}); hc {
		s.Command, hidden = r[0], true
	}
	if s.URL != "" {
		if s.URL, h = redactURL(s.URL); h {
			hidden = true
		}
	}

	s.Installable = true
	switch {
	case hidden:
		s.Installable, s.Reason = false, ReasonCredential
	case s.Kind == "remote" && (len(c.Headers) > 0 || len(c.Env) > 0):
		// header-ele / variabilele unui server remote s-ar pierde la instalare
		s.Installable, s.Reason = false, ReasonNeedsHeaders
	case !validEnvNames(s.Env):
		s.Installable, s.Reason = false, ReasonEnvNames
	}
	return s, true
}

// readJSON intoarce false daca fisierul lipseste sau nu e JSON valid.
func readJSON(path string, v any) bool {
	b, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	return json.Unmarshal(b, v) == nil
}

// desktopConfigPath e calea configurarii Claude Desktop pe fiecare sistem.
func desktopConfigPath(home, goos string) string {
	const file = "claude_desktop_config.json"
	switch goos {
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", "Claude", file)
	case "windows":
		if appData := os.Getenv("APPDATA"); appData != "" {
			return filepath.Join(appData, "Claude", file)
		}
		return filepath.Join(home, "AppData", "Roaming", "Claude", file)
	default:
		return filepath.Join(home, ".config", "Claude", file)
	}
}
