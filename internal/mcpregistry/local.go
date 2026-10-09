package mcpregistry

import (
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
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
		s.Env = append(s.Env, EnvVar{Name: k})
	}

	// credentialele din argumente / URL se ascund aici, ca valoarea sa nu iasa din proces
	hidden := false
	for i, a := range s.Args {
		if i > 0 && secretFlag(s.Args[i-1]) && !strings.HasPrefix(a, "-") {
			s.Args[i], hidden = Hidden, true
			continue
		}
		if r, ok := redactArg(a); ok {
			s.Args[i], hidden = r, true
		}
	}
	if s.URL != "" {
		if r, ok := redactURL(s.URL); ok {
			s.URL, hidden = r, true
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

// Hidden inlocuieste valoarea unui credential gasit in argumente sau in URL.
const Hidden = "<hidden>"

// secretKey: nume de optiune / variabila / parametru care arata a credential.
var secretKey = regexp.MustCompile(`(?i)(key|token|secret|passw(or)?d|pwd|auth|credential|bearer|session|cookie)`)

// envAssign: NAME=value (de ex. dupa docker -e / --env).
var envAssign = regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_]*)=`)

// secretFlag: "--token" / "-api-key" fara "=": valoarea e argumentul urmator.
func secretFlag(a string) bool {
	return strings.HasPrefix(a, "-") && !strings.Contains(a, "=") && secretKey.MatchString(strings.TrimLeft(a, "-"))
}

// redactArg intoarce argumentul cu credentialele inlocuite si true daca a ascuns ceva.
func redactArg(a string) (string, bool) {
	low := strings.ToLower(a)
	if strings.Contains(low, "bearer ") || strings.Contains(low, "authorization:") {
		return Hidden, true
	}
	if strings.HasPrefix(a, "-") {
		if name, val, ok := strings.Cut(a, "="); ok {
			if secretKey.MatchString(strings.TrimLeft(name, "-")) {
				return name + "=" + Hidden, true
			}
			if r, ok := redactURL(val); ok {
				return name + "=" + r, true
			}
		}
		return a, false
	}
	if m := envAssign.FindStringSubmatch(a); m != nil {
		if secretKey.MatchString(m[1]) {
			return m[1] + "=" + Hidden, true
		}
		if r, ok := redactURL(a[len(m[0]):]); ok {
			return m[0] + r, true
		}
		return a, false
	}
	return redactURL(a)
}

// urlPrefix: "scheme://" la inceputul unui argument.
var urlPrefix = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9+.-]*://`)

// redactURL ascunde parola din user:parola@ si valorile parametrilor cu nume de credential.
// Lucreaza pe text (nu prin url.Parse, care refuza unele parole si ar lasa valoarea sa treaca),
// ca restul URL-ului sa ramana exact cum era.
func redactURL(raw string) (string, bool) {
	m := urlPrefix.FindString(raw)
	if m == "" {
		return raw, false
	}
	rest, hidden := raw[len(m):], false
	// autoritatea se termina la primul "/", "?" sau "#"; "@" din parola e luat cu LastIndex
	end := strings.IndexAny(rest, "/?#")
	if end < 0 {
		end = len(rest)
	}
	if at := strings.LastIndex(rest[:end], "@"); at >= 0 {
		if colon := strings.Index(rest[:at], ":"); colon >= 0 {
			rest = rest[:colon+1] + Hidden + rest[at:]
			hidden = true
		}
	}
	if q := strings.Index(rest, "?"); q >= 0 {
		query, frag, hasFrag := strings.Cut(rest[q+1:], "#")
		parts := strings.Split(query, "&")
		changed := false
		for i, p := range parts {
			k, _, _ := strings.Cut(p, "=")
			key, err := url.QueryUnescape(k)
			if err != nil {
				key = k
			}
			if secretKey.MatchString(key) {
				parts[i] = k + "=" + Hidden
				changed = true
			}
		}
		if changed {
			rest = rest[:q+1] + strings.Join(parts, "&")
			if hasFrag {
				rest += "#" + frag
			}
			hidden = true
		}
	}
	return m + rest, hidden
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
