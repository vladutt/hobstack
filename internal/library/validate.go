package library

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strings"

	"foreman/internal/mcp"
	"foreman/internal/store"
)

var (
	secretNameRe = regexp.MustCompile(`^[A-Z][A-Z0-9_]{1,63}$`)
	needsAllowed = []string{"network", "write", "browser", "shell"}
)

// ValidSecretName: numele unui secret arata ca META_TOKEN (devine variabila de mediu).
func ValidSecretName(s string) bool { return secretNameRe.MatchString(s) }

// ValidateInput verifica tot ce nu cere disc, pentru propunerile agentilor si pentru elementele facute de utilizator.
// Intoarce campurile curatate. byUser: un server MCP local poate lipsi de fisiere (de ex. `npx -y pachet`).
func ValidateInput(in mcp.LibraryInput, byUser bool) (mcp.LibraryInput, error) {
	in.Name = strings.TrimSpace(in.Name)
	in.Description = strings.TrimSpace(in.Description)
	in.Entry = strings.TrimSpace(in.Entry)
	in.Command = strings.TrimSpace(in.Command)
	in.URL = strings.TrimSpace(in.URL)
	if in.Kind != store.KindTool && in.Kind != store.KindSkill && in.Kind != store.KindMCP {
		return in, errors.New("kind: tool, skill or mcp")
	}
	if !safeName.MatchString(in.Name) {
		return in, errors.New("name: lowercase letters, digits and dashes, 2-40 characters, starting with a letter (e.g. pdf-offer)")
	}
	// "team" e serverul MCP al aplicatiei; un server din Library cu acelasi nume l-ar suprascrie
	if in.Kind == store.KindMCP && in.Name == "team" {
		return in, errors.New(`name: "team" is reserved for the app's own MCP server`)
	}
	if n := len([]rune(in.Description)); n < 1 || n > 300 {
		return in, errors.New("description: 1-300 characters")
	}
	for _, n := range in.Needs {
		if !slices.Contains(needsAllowed, n) {
			return in, fmt.Errorf("needs: %q is not one of %s", n, strings.Join(needsAllowed, ", "))
		}
	}
	for _, s := range in.Secrets {
		if !secretNameRe.MatchString(s) {
			return in, fmt.Errorf("secrets: %q must look like META_TOKEN (capital letters, digits, underscores)", s)
		}
	}
	remote := in.Kind == store.KindMCP && in.URL != ""
	switch in.Kind {
	case store.KindSkill:
		if !slices.Contains(in.Files, "SKILL.md") {
			return in, errors.New("a skill needs SKILL.md in files")
		}
	case store.KindTool:
		if in.Entry == "" || !slices.Contains(in.Files, in.Entry) {
			return in, errors.New("a tool needs entry: one of the files, the one to run")
		}
	case store.KindMCP:
		if remote {
			u, err := url.Parse(in.URL)
			if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
				return in, errors.New("url: a full http(s) address")
			}
			if in.Command != "" {
				return in, errors.New("an mcp is either remote (url) or local (command), not both")
			}
		} else if in.Command == "" {
			return in, errors.New("an mcp needs command (local) or url (remote)")
		}
	}
	// mcp remote: fisierele sunt optionale (nu e nimic de pornit local); in rest cel putin unul
	if !remote && len(in.Files) == 0 && !(byUser && in.Kind == store.KindMCP) {
		return in, errors.New("files: at least one file")
	}
	return in, nil
}
