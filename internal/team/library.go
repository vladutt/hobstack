package team

import (
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"foreman/internal/library"
	"foreman/internal/mcp"
	"foreman/internal/runner"
	"foreman/internal/store"
)

// maxOpenProposals: cate propuneri deschise poate avea un proiect.
const maxOpenProposals = 3

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
	in, err := library.ValidateInput(in, false)
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

	// copiere fara t.work. Serverul MCP local NU se porneste aici (comanda e aleasa de agent): il testeaza
	// Publish, la actiunea utilizatorului.
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

	t.work.Lock()
	defer t.work.Unlock()
	if err := t.checkProposalRoom(c.ProjectID, in); err != nil { // cat a durat probe-ul, altcineva putea ocupa locul
		return "", err
	}
	it := store.LibraryItem{Kind: in.Kind, Name: in.Name, Description: in.Description, Status: store.ItemProposed,
		SourceProjectID: c.ProjectID, SourceTaskID: c.TaskID, MadeByAgentID: a.ID, MadeBy: a.Name,
		Needs: in.Needs, Secrets: in.Secrets, Entry: in.Entry, Command: in.Command, Args: in.Args, URL: in.URL,
	}
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
	if local {
		return fmt.Sprintf("Proposed %s to the library. The user tests and publishes it; keep using it in this project meanwhile.", it.Name), nil
	}
	return fmt.Sprintf("Proposed %s to the library. The user decides in Needs you; keep using it in this project meanwhile.", it.Name), nil
}

// recordLibraryUses noteaza ce elemente din Library a folosit run-ul. Un tool conteaza cand o comanda
// Bash contine <build>/<proiect>-<hash>/bin/<nume>/ (build = radacina build-urilor Library, deci nu
// potrivim copii din repo sau build-urile altor proiecte); un server MCP, cand run-ul apeleaza
// mcp__<nume>__*. Cel mult un rand per element si task: daca ultima folosire a elementului e deja pe
// acelasi task, nu mai adaugam (un task are mai multe run-uri). Run-urile fara task (chat) nu se noteaza:
// "Recent use" arata task-uri. Skill-urile nu se urmaresc. Erorile se logheaza.
func (t *Team) recordLibraryUses(ar activeRun, a store.Agent, o runner.Outcome) {
	if t.lib == nil || ar.taskID == 0 || (len(o.Commands) == 0 && len(o.ToolUses) == 0) {
		return
	}
	installs, err := t.st.ListInstalls(ar.projectID)
	if err != nil {
		log.Printf("agent %d: library uses: %v", a.ID, err)
		return
	}
	root := t.lib.BuildRoot() + string(filepath.Separator) + strconv.FormatInt(ar.projectID, 10) + "-"
	for _, in := range installs {
		if !in.Enabled {
			continue
		}
		it, err := t.st.GetLibraryItem(in.ItemID)
		if err != nil {
			continue
		}
		used := false
		switch it.Kind {
		case store.KindTool:
			re, err := regexp.Compile(regexp.QuoteMeta(root) + `[0-9A-Za-z]+` + regexp.QuoteMeta(string(filepath.Separator)+"bin"+string(filepath.Separator)+it.Name+string(filepath.Separator)))
			if err != nil {
				continue
			}
			for _, c := range o.Commands {
				if re.MatchString(c.Text) {
					used = true
					break
				}
			}
		case store.KindMCP:
			for _, n := range o.ToolUses {
				if strings.HasPrefix(n, "mcp__"+it.Name+"__") {
					used = true
					break
				}
			}
		}
		if !used {
			continue
		}
		if last, err := t.st.RecentLibraryUses(it.ID, 1); err == nil && len(last) == 1 && last[0].TaskID == ar.taskID {
			continue
		}
		if err := t.st.AddLibraryUse(it.ID, ar.taskID, a.ID); err != nil {
			log.Printf("agent %d: library use: %v", a.ID, err)
		}
	}
}
