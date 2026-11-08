package team

import (
	"errors"
	"fmt"
	"strings"

	"hobstack/internal/library"
	"hobstack/internal/mcp"
	"hobstack/internal/store"
)

// install_mcp: un agent cere un server MCP pentru proiectul lui. Il instaleaza doar utilizatorul, din Needs you
// (si cu Full autonomy): un server local porneste un program pe masina lui. Instalarea propriu-zisa (proba,
// elementul din Library, activarea in proiect) e in web; aici sunt cererea si anuntarea agentului.

// maxOpenMCPRequests: cate cereri install_mcp pot astepta deodata intr-un proiect.
const maxOpenMCPRequests = 5

// hiddenMarker: valoarea unei chei ascunse (mcpregistry.Hidden); nu e o valoare reala.
const hiddenMarker = "<hidden>"

// RequestMCP e tool-ul install_mcp.
func (t *Team) RequestMCP(c mcp.Caller, in mcp.MCPRequestInput) (string, error) {
	reason := strings.TrimSpace(in.Reason)
	if reason == "" || len([]rune(reason)) > 300 {
		return "", errors.New("reason: why you need it, 1-300 characters")
	}
	v, err := library.ValidateInput(mcp.LibraryInput{Kind: store.KindMCP, Name: in.Name, Description: in.Description,
		Command: in.Command, URL: in.URL, Args: in.Args, Secrets: in.Secrets}, true)
	if err != nil {
		return "", err
	}
	for _, part := range append([]string{v.Command, v.URL}, v.Args...) {
		if strings.Contains(part, hiddenMarker) || strings.Contains(part, "${DIR}") {
			return "", errors.New("put keys in secrets, not in the command, arguments or url")
		}
	}
	t.work.Lock()
	defer t.work.Unlock()
	a, err := t.st.GetAgent(c.AgentID)
	if err != nil {
		return "", err
	}
	r := store.MCPRequest{ProjectID: c.ProjectID, AgentID: a.ID, Name: v.Name, Description: v.Description,
		Transport: "local", Command: v.Command, Args: v.Args, URL: v.URL, Secrets: v.Secrets, Reason: reason}
	if v.URL != "" {
		r.Transport = "remote"
	}
	// un server cu acelasi nume exista deja in Library: cererea il activeaza pe acela, cu comanda lui
	if it, err := t.st.FindLibraryItem(store.KindMCP, v.Name); err == nil {
		installs, err := t.st.ListInstalls(c.ProjectID)
		if err != nil {
			return "", err
		}
		for _, in := range installs {
			if in.ItemID == it.ID {
				return fmt.Sprintf("The MCP server %q is already installed in this project. If its tools are missing, "+
					"it may be turned off in Instructions > Library; ask the user.", v.Name), nil
			}
		}
		id := it.ID
		r.ItemID = &id
		r.Description, r.Command, r.Args, r.URL, r.Secrets = it.Description, it.Command, it.Args, it.URL, it.Secrets
		r.Transport = it.Transport
		if r.Transport == "" {
			r.Transport = "local"
			if it.URL != "" {
				r.Transport = "remote"
			}
		}
	} else if !errors.Is(err, store.ErrNotFound) {
		return "", err
	}
	if open, err := t.st.OpenMCPRequestNamed(c.ProjectID, v.Name); err != nil {
		return "", err
	} else if open {
		return fmt.Sprintf("You already asked for %q; the user has not decided yet. Continue without it meanwhile.", v.Name), nil
	}
	if n, err := t.st.CountOpenMCPRequests(c.ProjectID); err != nil {
		return "", err
	} else if n >= maxOpenMCPRequests {
		return "", fmt.Errorf("%d MCP requests already wait for the user in this project; wait for their decision", n)
	}
	task := t.callerTask(c.TaskID)
	if task != nil {
		id := task.ID
		r.TaskID = &id
	}
	if err := t.st.AddMCPRequest(&r); err != nil {
		return "", err
	}
	if task != nil {
		t.taskEvent(*task, "mcp_requested", a.Name, fmt.Sprintf("%s asked to install the MCP server %q", a.Name, r.Name))
	}
	t.emit(c.ProjectID, "mcp_request", r)
	out := fmt.Sprintf("Asked the user to install %q; it appears in their Needs you list. You will be told when it is "+
		"installed (its tools work from your next run) or declined. Continue with what you can do without it; do not wait idle.", r.Name)
	if r.ItemID != nil {
		out += " Note: a library server with this name already exists, so the request is to turn that one on in this project."
	}
	return out, nil
}

// MCPRequestInstalled: utilizatorul a instalat serverul; agentul afla la urmatorul run (nota in coada + Kick).
func (t *Team) MCPRequestInstalled(id int64, tools []string) error {
	t.work.Lock()
	r, err := t.st.GetMCPRequest(id)
	if err == nil {
		err = t.st.CloseMCPRequest(id, store.MCPRequestInstalled)
	}
	if err != nil {
		t.work.Unlock()
		return err
	}
	text := fmt.Sprintf("The user installed the MCP server %q in this project. Its tools (mcp__%s__*) work from your next run.", r.Name, r.Name)
	if len(tools) > 0 {
		text += " Tools: " + strings.Join(tools, ", ") + "."
	}
	t.note(r.ProjectID, r.AgentID, text, false)
	if r.TaskID != nil {
		if task := t.callerTask(*r.TaskID); task != nil {
			t.taskEvent(*task, "mcp_installed", actorUser, fmt.Sprintf("Installed the MCP server %q", r.Name))
		}
	}
	r.Status = store.MCPRequestInstalled
	t.emit(r.ProjectID, "mcp_request", r)
	t.work.Unlock()
	t.Kick(r.ProjectID)
	return nil
}

// DeclineMCPRequest: utilizatorul a refuzat; agentul afla si continua fara server.
func (t *Team) DeclineMCPRequest(id int64) error {
	t.work.Lock()
	r, err := t.st.GetMCPRequest(id)
	if err == nil {
		err = t.st.CloseMCPRequest(id, store.MCPRequestDeclined)
	}
	if err != nil {
		t.work.Unlock()
		return err
	}
	t.note(r.ProjectID, r.AgentID, fmt.Sprintf("The user declined installing the MCP server %q. Continue without it.", r.Name), false)
	if r.TaskID != nil {
		if task := t.callerTask(*r.TaskID); task != nil {
			t.taskEvent(*task, "mcp_declined", actorUser, fmt.Sprintf("Declined the MCP server %q", r.Name))
		}
	}
	r.Status = store.MCPRequestDeclined
	t.emit(r.ProjectID, "mcp_request", r)
	t.work.Unlock()
	t.Kick(r.ProjectID)
	return nil
}
