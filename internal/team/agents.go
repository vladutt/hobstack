package team

import (
	"errors"
	"fmt"
	"log"
	"slices"
	"strings"
	"unicode/utf8"

	"foreman/internal/mcp"
	"foreman/internal/models"
	"foreman/internal/prompt"
	"foreman/internal/store"
)

// MaxAddedAgents: cati agenti (in echipa sau ceruti) poate adauga un lider.
const MaxAddedAgents = 6

// AgentRequestError: cererea nu mai poate fi aprobata asa cum e (nume luat,
// limita plina, liderul si-a pierdut un tool sau rolul). Cererea ramane deschisa.
type AgentRequestError string

func (e AgentRequestError) Error() string { return string(e) }

// CheckAgentFields valideaza campurile unui agent, la fel pentru UI si pentru
// add_agent: nume obligatoriu, model si tool-uri cunoscute (fara duplicate), lungimi.
func CheckAgentFields(a *store.Agent) error {
	if a.Name == "" {
		return errors.New("agent name is required")
	}
	if store.ReservedAgentName(a.Name) {
		return errors.New(store.ErrReservedName)
	}
	if !models.Valid(a.Model) {
		return errors.New("unknown model: " + a.Model)
	}
	tools := []string{}
	for _, tool := range a.Tools {
		if !slices.Contains(prompt.ValidTools, tool) {
			return errors.New("unknown tool: " + tool)
		}
		if !slices.Contains(tools, tool) {
			tools = append(tools, tool)
		}
	}
	a.Tools = tools
	for _, f := range []struct {
		label, value string
		max          int
	}{
		{"agent name", a.Name, 40},
		{"role", a.Role, 80},
		{"background", a.Background, 10000},
		{"instructions", a.Instructions, 10000},
		{"definition of done", a.DoneDef, 4000},
		{"responsibilities", a.Responsibilities, 2000},
	} {
		if utf8.RuneCountInString(f.value) > f.max {
			return fmt.Errorf("%s: at most %d characters", f.label, f.max)
		}
	}
	return nil
}

// canAddAgents: liderii si radacinile isi pot mari echipa (din DB, nu din flag-urile run-ului).
func canAddAgents(a store.Agent, agents []store.Agent) bool {
	r := store.RolesOf(a, agents)
	return r.IsLead || r.IsRoot
}

// newAgentSpec curata si valideaza argumentele lui add_agent pentru liderul dat.
func newAgentSpec(in mcp.AddAgentInput, lead store.Agent) (store.AgentSpec, string, error) {
	s := store.AgentSpec{
		Name: strings.TrimSpace(in.Name), Role: strings.TrimSpace(in.Role), Model: strings.TrimSpace(in.Model),
		Background: strings.TrimSpace(in.Background), Instructions: strings.TrimSpace(in.Instructions),
		DoneDef: strings.TrimSpace(in.DoneDef),
	}
	if s.Model == "" {
		s.Model = "sonnet"
	}
	switch {
	case in.ToolsSet:
		s.Tools = in.Tools
	case slices.Contains(lead.Tools, "write"):
		s.Tools = []string{"write"}
	default:
		s.Tools = []string{}
	}
	reason := strings.TrimSpace(in.Reason)
	switch {
	case s.Role == "":
		return s, "", errors.New("role is required")
	case s.Instructions == "":
		return s, "", errors.New("instructions are required")
	case reason == "":
		return s, "", errors.New("reason is required: say in one sentence why the team needs this agent")
	case utf8.RuneCountInString(reason) > 300:
		return s, "", errors.New("reason: at most 300 characters")
	}
	a := agentFrom(s, lead)
	if err := CheckAgentFields(&a); err != nil {
		return s, "", err
	}
	s.Tools = a.Tools
	var extra []string
	for _, tool := range s.Tools {
		if !slices.Contains(lead.Tools, tool) {
			extra = append(extra, tool)
		}
	}
	if len(extra) > 0 {
		return s, "", fmt.Errorf("you can only give tools you have yourself: %s", strings.Join(extra, ", "))
	}
	return s, reason, nil
}

// agentFrom construieste agentul din propunere: sub lider, in folderul lui, fara reviewer.
func agentFrom(s store.AgentSpec, lead store.Agent) store.Agent {
	leadID := lead.ID
	return store.Agent{
		ProjectID: lead.ProjectID, Name: s.Name, Role: s.Role, Model: s.Model, Background: s.Background,
		Instructions: s.Instructions, DoneDef: s.DoneDef, Tools: s.Tools,
		ParentID: &leadID, FolderID: lead.FolderID, CreatedBy: &leadID,
	}
}

// checkAvailable verifica numele si limita liderului. La aprobare, cererea
// insasi e inca deschisa: ii ocupa numele si un loc din limita, deci nu se numara.
func (t *Team) checkAvailable(projectID, leadID int64, name string, approving bool) error {
	if _, err := t.st.FindAgentByName(projectID, name); err == nil {
		return fmt.Errorf("an agent named %q already exists", name)
	} else if !errors.Is(err, store.ErrNotFound) {
		return err
	}
	if !approving {
		open, err := t.st.OpenAgentRequestNamed(projectID, name)
		if err != nil {
			return err
		}
		if open {
			return fmt.Errorf("an agent named %q is already waiting for approval", name)
		}
	}
	n, err := t.st.CountAddedBy(leadID)
	if err != nil {
		return err
	}
	if (!approving && n >= MaxAddedAgents) || (approving && n > MaxAddedAgents) {
		return fmt.Errorf("you already have %d agents you added (or requested); remove one with remove_agent first", MaxAddedAgents)
	}
	return nil
}

// callerTask intoarce task-ul run-ului (pentru istoric), daca exista.
func (t *Team) callerTask(taskID int64) *store.Task {
	if taskID == 0 {
		return nil
	}
	task, err := t.st.GetTask(taskID)
	if err != nil {
		return nil
	}
	return &task
}

// addedText: raspunsul catre lider dupa ce agentul a intrat in echipa.
func (t *Team) addedText(projectID int64, name string) string {
	text := fmt.Sprintf("Agent %q added to your team. Give it work with create_task.", name)
	if stages, err := t.st.ListStages(projectID); err == nil && len(stages) > 0 {
		text = fmt.Sprintf("Agent %q added to your team. This project uses stages: it takes tasks only after the user adds it to a stage, so tell the user to add it to a stage (Columns).", name)
	}
	return text
}

// AddAgent e tool-ul add_agent: direct cu Full autonomy sau intr-un run pornit
// din chat-ul utilizatorului, altfel o cerere in "Needs you".
func (t *Team) AddAgent(c mcp.Caller, in mcp.AddAgentInput) (string, error) {
	text, added, err := t.addAgent(c, in)
	if err != nil {
		return "", err
	}
	if added {
		// rolurile s-au schimbat (liderul poate avea primul agent): recalculam si pornim ce e de pornit
		if err := t.LinksChanged(c.ProjectID); err != nil {
			log.Printf("add_agent: %v", err)
		}
	}
	return text, nil
}

func (t *Team) addAgent(c mcp.Caller, in mcp.AddAgentInput) (string, bool, error) {
	t.work.Lock()
	defer t.work.Unlock()
	agents, err := t.st.ListAgents(c.ProjectID)
	if err != nil {
		return "", false, err
	}
	lead, ok := store.FindAgent(agents, c.AgentID)
	if !ok {
		return "", false, errors.New("your agent no longer exists")
	}
	if !canAddAgents(lead, agents) {
		return "", false, errors.New("only a lead or an agent that reports to the user can add agents")
	}
	spec, reason, err := newAgentSpec(in, lead)
	if err != nil {
		return "", false, err
	}
	if err := t.checkAvailable(c.ProjectID, lead.ID, spec.Name, false); err != nil {
		return "", false, err
	}
	task := t.callerTask(c.TaskID)
	_, fromChat := t.requestOf(c.AgentID, c.RunID)
	if t.autonomyOf(c.ProjectID) || fromChat {
		a := agentFrom(spec, lead)
		if err := t.st.CreateAgent(&a); errors.Is(err, store.ErrConflict) {
			return "", false, fmt.Errorf("an agent named %q already exists", a.Name)
		} else if err != nil {
			return "", false, err
		}
		if task != nil {
			t.taskEvent(*task, "agent_added", lead.Name, fmt.Sprintf("Added agent %q", a.Name))
		}
		t.emit(c.ProjectID, "agent", map[string]int64{"id": a.ID})
		return t.addedText(c.ProjectID, a.Name), true, nil
	}
	r := store.AgentRequest{ProjectID: c.ProjectID, LeadID: lead.ID, RunID: c.RunID, Spec: spec, Reason: reason}
	if task != nil {
		id := task.ID
		r.TaskID = &id
	}
	if err := t.st.AddAgentRequest(&r); err != nil {
		return "", false, err
	}
	if task != nil {
		t.taskEvent(*task, "agent_requested", lead.Name, fmt.Sprintf("Requested agent %q", spec.Name))
	}
	t.emit(c.ProjectID, "agent_request", r)
	return "Request sent to the user. You will get a message when it is approved or dismissed; continue with your current work meanwhile.", false, nil
}

// RemoveAgent e tool-ul remove_agent: detaseaza un agent adaugat de lider, fara task-uri deschise.
func (t *Team) RemoveAgent(c mcp.Caller, name string) (string, error) {
	text, err := t.removeAgent(c, name)
	if err != nil {
		return "", err
	}
	if err := t.LinksChanged(c.ProjectID); err != nil {
		log.Printf("remove_agent: %v", err)
	}
	return text, nil
}

func (t *Team) removeAgent(c mcp.Caller, name string) (string, error) {
	t.work.Lock()
	defer t.work.Unlock()
	name = strings.TrimSpace(name)
	if name == "" {
		return "", errors.New("name is required")
	}
	a, err := t.st.FindAgentByName(c.ProjectID, name)
	if errors.Is(err, store.ErrNotFound) {
		return "", fmt.Errorf("there is no agent named %q", name)
	}
	if err != nil {
		return "", err
	}
	if a.CreatedBy == nil || *a.CreatedBy != c.AgentID {
		return "", errors.New("you can only remove agents you added")
	}
	if a.ParentID == nil || *a.ParentID != c.AgentID {
		return "", fmt.Errorf("%q is not on your team", a.Name)
	}
	tasks, err := t.st.ListTasks(c.ProjectID)
	if err != nil {
		return "", err
	}
	var open []string
	for _, task := range tasks {
		if task.AssigneeID == a.ID && task.Status != store.StatusDone {
			open = append(open, task.Ref())
		}
	}
	if len(open) > 0 {
		return "", fmt.Errorf("%q still has open tasks: %s", a.Name, strings.Join(open, ", "))
	}
	agents, err := t.st.ListAgents(c.ProjectID)
	if err != nil {
		return "", err
	}
	if r := store.RolesOf(a, agents); r.IsLead || r.IsReviewer {
		return "", fmt.Errorf("%q has its own team or reviews someone; ask the user to change it", a.Name)
	}
	a.ParentID, a.Detached = nil, true
	if err := t.st.UpdateAgent(a); err != nil {
		return "", err
	}
	if task := t.callerTask(c.TaskID); task != nil {
		lead, _ := store.FindAgent(agents, c.AgentID)
		t.taskEvent(*task, "agent_removed", lead.Name, fmt.Sprintf("Removed agent %q", a.Name))
	}
	t.emit(c.ProjectID, "agent", map[string]int64{"id": a.ID})
	return fmt.Sprintf("%q removed from your team (kept on the canvas, detached).", a.Name), nil
}

// ApproveAgentRequest creeaza agentul cerut, dupa o noua validare, si ii spune liderului.
func (t *Team) ApproveAgentRequest(id int64) error {
	projectID, err := t.approveAgentRequest(id)
	if err != nil {
		return err
	}
	// aprobarea e deja salvata: o eroare la recalculare doar se logheaza (ca la add_agent)
	if err := t.LinksChanged(projectID); err != nil {
		log.Printf("approve agent request: %v", err)
	}
	return nil
}

func (t *Team) approveAgentRequest(id int64) (int64, error) {
	t.work.Lock()
	defer t.work.Unlock()
	r, err := t.st.GetAgentRequest(id)
	if err != nil {
		return 0, err
	}
	if r.Status != store.AgentRequestOpen {
		return 0, store.ErrConflict
	}
	agents, err := t.st.ListAgents(r.ProjectID)
	if err != nil {
		return 0, err
	}
	lead, ok := store.FindAgent(agents, r.LeadID)
	if !ok {
		return 0, store.ErrNotFound
	}
	if !canAddAgents(lead, agents) {
		return 0, AgentRequestError(fmt.Sprintf("%s can no longer add agents (it is not a lead or a root anymore)", lead.Name))
	}
	in := mcp.AddAgentInput{Name: r.Spec.Name, Role: r.Spec.Role, Model: r.Spec.Model, Background: r.Spec.Background,
		Instructions: r.Spec.Instructions, DoneDef: r.Spec.DoneDef, Reason: r.Reason, Tools: r.Spec.Tools, ToolsSet: true}
	spec, _, err := newAgentSpec(in, lead)
	if err != nil {
		return 0, AgentRequestError(err.Error())
	}
	if err := t.checkAvailable(r.ProjectID, lead.ID, spec.Name, true); err != nil {
		return 0, AgentRequestError(err.Error())
	}
	a := agentFrom(spec, lead)
	if err := t.st.ApproveAgentRequest(r.ID, &a); errors.Is(err, store.ErrConflict) {
		// cererea e inca deschisa (verificat mai sus), deci conflictul e numele
		return 0, AgentRequestError(fmt.Sprintf("an agent named %q already exists", a.Name))
	} else if err != nil {
		return 0, err
	}
	// nota (eveniment), nu mesaj al utilizatorului: run-ul urmator nu trebuie sa para pornit din chat
	// acelasi text ca la add_agent direct (inclusiv indicatia pentru proiectele cu etape)
	t.note(r.ProjectID, lead.ID, "The user approved your request. "+t.addedText(r.ProjectID, a.Name), false)
	if r.TaskID != nil {
		if task := t.callerTask(*r.TaskID); task != nil {
			t.taskEvent(*task, "agent_approved", actorUser, fmt.Sprintf("Approved agent %q", a.Name))
		}
	}
	r.Status, r.AgentID = store.AgentRequestApproved, &a.ID
	t.emit(r.ProjectID, "agent", map[string]int64{"id": a.ID})
	t.emit(r.ProjectID, "agent_request", r)
	return r.ProjectID, nil
}

// DismissAgentRequest inchide cererea fara agent si ii spune liderului.
func (t *Team) DismissAgentRequest(id int64) error {
	t.work.Lock()
	r, err := t.st.GetAgentRequest(id)
	if err == nil {
		err = t.st.DismissAgentRequest(id)
	}
	if err != nil {
		t.work.Unlock()
		return err
	}
	t.note(r.ProjectID, r.LeadID, fmt.Sprintf("The user dismissed your request to add %q. Continue with your current team.", r.Spec.Name), false)
	if r.TaskID != nil {
		if task := t.callerTask(*r.TaskID); task != nil {
			t.taskEvent(*task, "agent_dismissed", actorUser, fmt.Sprintf("Dismissed agent %q", r.Spec.Name))
		}
	}
	r.Status = store.AgentRequestDismissed
	t.emit(r.ProjectID, "agent_request", r)
	t.work.Unlock()
	t.Kick(r.ProjectID)
	return nil
}
