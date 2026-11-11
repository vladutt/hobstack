package team

import (
	"errors"
	"fmt"
	"strings"

	"hobstack/internal/library"
	"hobstack/internal/mcp"
	"hobstack/internal/store"
)

// install_mcp: un agent cere un server MCP pentru proiectul lui. De regula il instaleaza doar utilizatorul, din
// Needs you (si cu Full autonomy): un server local porneste un program pe masina lui. Exceptia: un lider sau o
// radacina, intr-un run pornit dintr-un mesaj al utilizatorului din chat (requestOf, ca la create_recurring),
// il instaleaza direct, daca serverul nu cere chei (valorile lor le da doar utilizatorul). Instalarea propriu-zisa
// (proba, elementul din Library, activarea in proiect) e in web (SetMCPInstaller); aici sunt cererea si anuntarea.

// maxOpenMCPRequests: cate cereri install_mcp pot astepta deodata intr-un proiect.
const maxOpenMCPRequests = 5

// hiddenMarker: valoarea unei chei ascunse (mcpregistry.Hidden); nu e o valoare reala.
const hiddenMarker = "<hidden>"

// MCPInstall: serverul pe care un lider il instaleaza direct in proiect. ItemID = un element deja in Library
// (doar se activeaza in proiect); altfel se creeaza, cu proba, ca la Library > MCP > Add server.
type MCPInstall struct {
	ProjectID   int64
	AgentID     int64 // liderul care il instaleaza ("made by" in Library)
	AgentName   string
	ItemID      *int64
	Name        string
	Description string
	Transport   string // "local" | "remote"
	Command     string
	Args        []string
	URL         string
}

// MCPTestError: proba serverului n-a raspuns la tools/list; utilizatorul il poate instala fara test din Needs you.
type MCPTestError struct{ Msg string }

func (e *MCPTestError) Error() string { return e.Msg }

// MCPInstaller instaleaza serverul in proiect si intoarce numele tool-urilor lui (pot lipsi: remote, fara proba).
type MCPInstaller func(in MCPInstall) ([]string, error)

// SetMCPInstaller: instalarea directa a liderilor (web); fara el install_mcp face mereu cerere.
func (t *Team) SetMCPInstaller(f MCPInstaller) { t.installMCP = f }

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
	r, done, direct, err := t.prepareMCPRequest(c, v, reason)
	if err != nil || done != "" || !direct {
		if err == nil && done == "" {
			done, err = t.addMCPRequest(c, r, "")
		}
		t.work.Unlock()
		return done, err
	}
	t.work.Unlock()

	// instalarea directa, fara lock: proba poate dura pana la 30 s
	agentName := ""
	if a, err := t.st.GetAgent(c.AgentID); err == nil {
		agentName = a.Name
	}
	tools, ierr := t.installMCP(MCPInstall{ProjectID: c.ProjectID, AgentID: c.AgentID, AgentName: agentName, ItemID: r.ItemID, Name: r.Name,
		Description: r.Description, Transport: r.Transport, Command: r.Command, Args: r.Args, URL: r.URL})
	var testErr *MCPTestError
	if errors.As(ierr, &testErr) {
		// proba picata: cererea ajunge totusi la utilizator, care il poate instala fara test
		t.work.Lock()
		defer t.work.Unlock()
		return t.addMCPRequest(c, r, testErr.Msg)
	}
	if ierr != nil {
		return "", fmt.Errorf("could not install %q: %w", r.Name, ierr)
	}
	t.work.Lock()
	if task := t.callerTask(c.TaskID); task != nil {
		if a, err := t.st.GetAgent(c.AgentID); err == nil {
			t.taskEvent(*task, "mcp_installed", a.Name, fmt.Sprintf("%s installed the MCP server %q (asked by the user)", a.Name, r.Name))
		}
	}
	t.work.Unlock()
	out := fmt.Sprintf("Installed the MCP server %q in this project, as the user asked. Its tools (mcp__%s__*) work "+
		"from your next run (and for the project's other agents from their next run).", r.Name, r.Name)
	if len(tools) > 0 {
		out += " Tools: " + strings.Join(tools, ", ") + "."
	}
	if r.ItemID != nil {
		out += " It was already in the library, so it was only turned on in this project."
	}
	return out, nil
}

// prepareMCPRequest construieste cererea; done != "" = raspunsul final (deja instalat / deja cerut), direct =
// apelantul il poate instala singur (lider sau radacina, la cererea utilizatorului din chat, fara chei).
// Se cheama cu t.work tinut.
func (t *Team) prepareMCPRequest(c mcp.Caller, v mcp.LibraryInput, reason string) (r store.MCPRequest, done string, direct bool, err error) {
	agents, err := t.st.ListAgents(c.ProjectID)
	if err != nil {
		return r, "", false, err
	}
	a, ok := store.FindAgent(agents, c.AgentID)
	if !ok {
		return r, "", false, errors.New("your agent no longer exists")
	}
	r = store.MCPRequest{ProjectID: c.ProjectID, AgentID: a.ID, Name: v.Name, Description: v.Description,
		Transport: "local", Command: v.Command, Args: v.Args, URL: v.URL, Secrets: v.Secrets, Reason: reason}
	if v.URL != "" {
		r.Transport = "remote"
	}
	// un server cu acelasi nume exista deja in Library: cererea il activeaza pe acela, cu comanda lui
	if it, err := t.st.FindLibraryItem(store.KindMCP, v.Name); err == nil {
		installs, err := t.st.ListInstalls(c.ProjectID)
		if err != nil {
			return r, "", false, err
		}
		for _, in := range installs {
			if in.ItemID == it.ID {
				return r, fmt.Sprintf("The MCP server %q is already installed in this project. If its tools are missing, "+
					"it may be turned off in Instructions > Library; ask the user.", v.Name), false, nil
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
		return r, "", false, err
	}
	if open, err := t.st.OpenMCPRequestNamed(c.ProjectID, v.Name); err != nil {
		return r, "", false, err
	} else if open {
		return r, fmt.Sprintf("You already asked for %q; the user has not decided yet. Continue without it meanwhile.", v.Name), false, nil
	}
	if t.installMCP != nil && len(r.Secrets) == 0 && canAddAgents(a, agents) {
		_, direct = t.requestOf(c.AgentID, c.RunID)
	}
	return r, "", direct, nil
}

// addMCPRequest salveaza cererea pentru Needs you si anunta; testFailed = de ce n-a mers instalarea directa.
// Se cheama cu t.work tinut.
func (t *Team) addMCPRequest(c mcp.Caller, r store.MCPRequest, testFailed string) (string, error) {
	if n, err := t.st.CountOpenMCPRequests(c.ProjectID); err != nil {
		return "", err
	} else if n >= maxOpenMCPRequests {
		return "", fmt.Errorf("%d MCP requests already wait for the user in this project; wait for their decision", n)
	}
	a, err := t.st.GetAgent(c.AgentID)
	if err != nil {
		return "", err
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
	if testFailed != "" {
		out = fmt.Sprintf("Could not install %q: its test failed (%s). ", r.Name, testFailed) + out +
			" The user can install it without the test from there; tell them what failed."
	}
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
