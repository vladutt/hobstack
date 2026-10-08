package team

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"slices"
	"strings"
	"time"

	"foreman/internal/library"
	"foreman/internal/mcp"
	"foreman/internal/store"
)

// maxOpenProposals: cate propuneri deschise poate avea un proiect.
const maxOpenProposals = 3

var (
	itemNameRe   = regexp.MustCompile(`^[a-z][a-z0-9-]{1,39}$`)
	secretNameRe = regexp.MustCompile(`^[A-Z][A-Z0-9_]{1,63}$`)
	needsAllowed = []string{"network", "write", "browser", "shell"}
)

// validateLibraryInput verifica tot ce nu cere disc. Intoarce campurile curatate.
func validateLibraryInput(in mcp.LibraryInput) (mcp.LibraryInput, error) {
	in.Name = strings.TrimSpace(in.Name)
	in.Description = strings.TrimSpace(in.Description)
	in.Entry = strings.TrimSpace(in.Entry)
	in.Command = strings.TrimSpace(in.Command)
	in.URL = strings.TrimSpace(in.URL)
	if in.Kind != store.KindTool && in.Kind != store.KindSkill && in.Kind != store.KindMCP {
		return in, errors.New("kind: tool, skill or mcp")
	}
	if !itemNameRe.MatchString(in.Name) {
		return in, errors.New("name: lowercase letters, digits and dashes, 2-40 characters, starting with a letter (e.g. pdf-offer)")
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
	if !remote && len(in.Files) == 0 {
		return in, errors.New("files: at least one file")
	}
	return in, nil
}

// checkProposalRoom verifica limita de propuneri deschise si numele liber. Se cheama cu t.work blocat.
func (t *Team) checkProposalRoom(projectID int64, in mcp.LibraryInput) error {
	n, err := t.st.CountOpenProposals(projectID)
	if err != nil {
		return err
	}
	if n >= maxOpenProposals {
		return fmt.Errorf("this project already has %d proposals waiting for the user; wait for a decision before proposing more", maxOpenProposals)
	}
	if _, err := t.st.FindLibraryItem(in.Kind, in.Name); err == nil {
		return fmt.Errorf("the library already has a %s named %q; pick another name", in.Kind, in.Name)
	} else if !errors.Is(err, store.ErrNotFound) {
		return err
	}
	return nil
}

// clipText scurteaza un text (eroarea probe-ului poate contine tot ce scrie serverul).
func clipText(s string, n int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) <= n {
		return string(r)
	}
	return string(r[:n]) + "..."
}

// ProposeLibraryItem: un agent cu tool-ul write propune un tool / skill / server MCP pentru Library.
// Randul din baza apare abia la sfarsit, dupa ce fisierele sunt copiate si serverul a trecut probe-ul:
// pana atunci propunerea exista doar intr-un folder de lucru (.staging-*), invizibil pentru utilizator.
func (t *Team) ProposeLibraryItem(c mcp.Caller, in mcp.LibraryInput) (string, error) {
	if t.lib == nil {
		return "", errors.New("the library is not available")
	}
	if c.SetupID != "" || c.DraftID != "" || !c.CanWrite {
		return "", errors.New("propose_library_item is only for agents that can write files")
	}
	in, err := validateLibraryInput(in)
	if err != nil {
		return "", err
	}
	// folderul agentului: de aici se copiaza fisierele
	p, err := t.st.GetProject(c.ProjectID)
	if err != nil {
		return "", err
	}
	a, err := t.st.GetAgent(c.AgentID)
	if err != nil {
		return "", err
	}
	local := in.Kind == store.KindMCP && in.URL == ""
	// un server local porneste un proces pe masina: asta cere Shell, nu doar write
	if local && !slices.Contains(a.Tools, "shell") {
		return "", errors.New("Proposing a local MCP server needs the Shell tool; ask the user to add it, or propose a remote server.")
	}
	home := store.HomeFolder(a, p.Folders)
	if home.Path == "" {
		return "", errors.New("your project has no folder")
	}

	// verificare rapida inainte de munca pe disc (se repeta la sfarsit, sub lock)
	t.work.Lock()
	err = t.checkProposalRoom(c.ProjectID, in)
	t.work.Unlock()
	if err != nil {
		return "", err
	}

	// copiere si probe fara t.work: probe-ul poate dura 10 s si ar bloca tot scheduler-ul
	var staging string
	if len(in.Files) > 0 {
		if staging, _, _, err = t.lib.CopyStaged(home.Path, in.Files); err != nil {
			return "", err
		}
	} else {
		// mcp remote fara fisiere: nu e nimic de copiat, dar Publish are nevoie de un folder al propunerii (gol)
		if staging, err = t.lib.NewStaging(); err != nil {
			return "", err
		}
	}
	defer os.RemoveAll(staging) // dupa AdoptStaged nu mai exista

	var toolNames []string
	if local {
		args := make([]string, len(in.Args))
		for i, s := range in.Args {
			args[i] = strings.ReplaceAll(s, "${DIR}", staging)
		}
		// fara secrete la propunere: serverul trebuie sa raspunda la tools/list si fara ele
		toolNames, err = library.ProbeMCPIn(staging, in.Command, args, nil, 10*time.Second)
		if err != nil {
			return "", fmt.Errorf("the MCP server did not answer tools/list: %s", clipText(err.Error(), 300))
		}
	}

	t.work.Lock()
	defer t.work.Unlock()
	if err := t.checkProposalRoom(c.ProjectID, in); err != nil { // cat a durat probe-ul, altcineva putea ocupa locul
		return "", err
	}
	it := store.LibraryItem{Kind: in.Kind, Name: in.Name, Description: in.Description, Status: store.ItemProposed,
		SourceProjectID: c.ProjectID, SourceTaskID: c.TaskID, MadeByAgentID: a.ID, MadeBy: a.Name,
		Needs: in.Needs, Secrets: in.Secrets, Entry: in.Entry, Command: in.Command, Args: in.Args, URL: in.URL,
		ToolNames: toolNames}
	if in.Kind == store.KindMCP {
		it.Transport = "local"
		if !local {
			it.Transport = "remote"
		}
	}
	if err := t.st.AddLibraryItem(&it); errors.Is(err, store.ErrConflict) {
		return "", fmt.Errorf("the library already has a %s named %q; pick another name", in.Kind, in.Name)
	} else if err != nil {
		return "", err
	}
	if _, err := t.lib.AdoptStaged(staging, it.ID); err != nil {
		_ = t.st.DeleteLibraryItem(it.ID)
		return "", err
	}
	t.note(c.ProjectID, a.ID, fmt.Sprintf("Proposed %s %q to the library. The user decides in Needs you; keep using it in this project meanwhile.", it.Kind, it.Name), true)
	t.emit(c.ProjectID, "library", it)
	return fmt.Sprintf("Proposed %s to the library. The user decides in Needs you; keep using it in this project meanwhile.", it.Name), nil
}
