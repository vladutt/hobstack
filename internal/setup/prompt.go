package setup

import (
	"encoding/json"
	"fmt"
	"strings"

	"hobstack/internal/store"
)

const systemPrompt = `You are the team architect in HobStack. You read a project and propose a small team of Claude Code agents that will work on it.

What you do: propose ONE team for THIS project. The user's description says what the team will work on after it is created; it is not a task for you. Do not implement anything, and do not propose alternatives, templates, or teams for other projects.

Rules:
- Read only what you need: the folder structure, the README, the dependency and build files (package.json, composer.json, go.mod, Makefile, etc.) and a few representative files. Do not read the whole repo.
- Do not modify anything; you are not allowed to.
- Team: 2-6 agents, usually a single root lead (empty reportsTo) who splits the work; add a reviewer only if the project really needs one.
- For each agent write: role, "Takes" (the kinds of tasks it takes), background (what it knows about this project), instructions (how it works, what it does not do), definition of done.
- Shared context: the stack, the build/test/run commands, the conventions and the structure, briefly.
- Several folders mean one product with several parts (e.g. a client app and its landing page). Then fill folders with exactly the given paths, each with a short name (e.g. Client, Landing) and its own context (stack, build/test/run commands, conventions of that folder). Propose one root lead for the whole product and, under it, one sub-lead per folder with its workers; set each agent's folder. The shared context describes the product and how the parts connect (e.g. the API between them).
- With one folder or none, you may leave folders out.
- If a folder has a CLAUDE.md, read it first and base that folder's context on it (and the shared context, if there is a single folder).
- Tools: you may propose only write (writes files) and git; the user adds shell or web themselves if they want.
- Write everything (names, context, prompts) in the language of the user's description; with no description, in English.
- The shared context describes the actual project in the folder (what you read), not assumed technologies.
- At the end, call propose_setup once with the whole proposal. If you get an error, fix it and call it again; otherwise stop.`

// folderSystemPrompt se adauga in modul folder (agenti pentru un folder al unui proiect existent).
const folderSystemPrompt = `Folder mode: you are adding ONE folder to an EXISTING project. The existing team is listed in the message; do not change, repeat or rename existing agents.
- Propose only new agents that work in this folder: usually one sub-lead and 1-3 workers.
- The sub-lead reports to an existing agent (usually the root lead): put its exact name in reportsTo. New names must not match existing ones.
- Put exactly this folder in folders (its path as given), with a short name and its own context (stack, build/test/run commands, conventions). If it has a CLAUDE.md, base the context on it.
- name: repeat the project name; goal and context of the project are ignored.`

// folderBrief: proiectul, folderele si echipa existenta, pentru mesajul analistului.
func folderBrief(in FolderInput) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Project: %s\n", in.Project.Name)
	if g := strings.TrimSpace(in.Project.Goal); g != "" {
		fmt.Fprintf(&b, "Goal: %s\n", g)
	}
	if c := strings.TrimSpace(in.Project.Context); c != "" {
		fmt.Fprintf(&b, "Shared context:\n%s\n", c)
	}
	fmt.Fprintf(&b, "Folder to set up: %s (%s)\n", in.Folder.Name, in.Folder.Path)
	for _, f := range in.Project.Folders {
		if f.ID != in.Folder.ID {
			fmt.Fprintf(&b, "Other folder (read only): %s (%s)\n", f.Name, f.Path)
		}
	}
	names := map[int64]string{}
	for _, a := range in.Team {
		names[a.ID] = a.Name
	}
	folderName := func(a store.Agent) string { return store.HomeFolder(a, in.Project.Folders).Name }
	b.WriteString("Existing team:")
	for _, a := range in.Team {
		line := "\n- " + a.Name
		if a.Role != "" {
			line += ": " + a.Role
		}
		line += ", folder: " + folderName(a)
		if a.ParentID != nil {
			line += ", reports to " + names[*a.ParentID]
		} else if !a.Detached {
			line += ", reports to the user"
		}
		if r := strings.TrimSpace(a.Responsibilities); r != "" {
			line += ", takes: " + r
		}
		b.WriteString(line)
	}
	return b.String()
}

func turnPrompt(s *Session) string {
	if s.ProjectID != 0 {
		want := "The user did not describe what these agents should do; infer it from the folder."
		if s.Description != "" {
			want = "What the new agents should do: " + s.Description
		}
		return s.brief + "\n" + want
	}
	folder := "There is no folder yet: the project is new."
	switch {
	case len(s.Workdirs) == 1:
		folder = "Folder: " + s.Workdirs[0]
	case len(s.Workdirs) > 1:
		folder = "Folders (parts of the same product; you are in the first one and can read all of them):\n- " + strings.Join(s.Workdirs, "\n- ")
	}
	want := "The user did not describe anything; infer it from the code."
	if s.Description != "" {
		want = "What the user wants: " + s.Description
	}
	return folder + "\n" + want
}

// chatTurn: mesajul pentru un run de chat (propunerea curenta + cererea utilizatorului).
func chatTurn(current Proposal, message string) (string, error) {
	raw, err := json.MarshalIndent(current, "", "  ")
	if err != nil {
		return "", err
	}
	return "The user is reviewing your proposal and asks for a change.\n" +
		"Current proposal (with the user's own edits; build on it, do not revert them):\n" + string(raw) + "\n\n" +
		"User: " + message + "\n\n" +
		"If this changes the proposal, call propose_setup once with the WHOLE updated proposal, then reply in one or two sentences with what you changed. " +
		"If it is only a question, answer it without calling propose_setup. " +
		"Do not add the shell or web tools yourself; keep the tools the user already chose.", nil
}
