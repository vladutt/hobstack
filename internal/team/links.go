package team

import (
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"unicode/utf8"

	"foreman/internal/mcp"
	"foreman/internal/store"
)

// MaxOpenQuestions: cate intrebari fara raspuns poate avea un agent catre alte proiecte.
const MaxOpenQuestions = 3

// ProjectRequestError: cererea nu mai poate fi aprobata (de ex. legatura a fost stearsa).
type ProjectRequestError string

func (e ProjectRequestError) Error() string { return string(e) }

// linkedTarget gaseste, dupa nume (fara diferenta de litere), un proiect legat de proiectul from.
func (t *Team) linkedTarget(from int64, name string) (store.LinkedProject, error) {
	links, err := t.st.LinkedProjects(from)
	if err != nil {
		return store.LinkedProject{}, err
	}
	if len(links) == 0 {
		return store.LinkedProject{}, errors.New("this project has no linked projects")
	}
	name = strings.TrimSpace(name)
	names := make([]string, 0, len(links))
	for _, l := range links {
		if strings.EqualFold(l.Name, name) {
			return l, nil
		}
		names = append(names, l.Name)
	}
	return store.LinkedProject{}, fmt.Errorf("%q is not a linked project. Linked projects: %s", name, strings.Join(names, ", "))
}

// rootOf intoarce prima radacina a proiectului (ListAgents pune radacinile primele).
func (t *Team) rootOf(projectID int64) (store.Agent, bool, error) {
	agents, err := t.st.ListAgents(projectID)
	if err != nil {
		return store.Agent{}, false, err
	}
	for _, a := range agents {
		if store.RolesOf(a, agents).IsRoot {
			return a, true, nil
		}
	}
	return store.Agent{}, false, nil
}

// projectName: numele proiectului, pentru texte; un proiect sters devine "a deleted project".
func (t *Team) projectName(id int64) string {
	p, err := t.st.GetProject(id)
	if err != nil {
		return "a deleted project"
	}
	return p.Name
}

// agentNameOf: numele agentului (nil sau sters = "a deleted agent").
func (t *Team) agentNameOf(id *int64) string {
	if id == nil {
		return "a deleted agent"
	}
	a, err := t.st.GetAgent(*id)
	if err != nil {
		return "a deleted agent"
	}
	return a.Name
}

// parseRequestRef citeste "Q-7", "q-7" sau "7".
func parseRequestRef(ref, prefix string) (int64, error) {
	s := strings.ToUpper(strings.TrimSpace(ref))
	s = strings.TrimPrefix(s, prefix+"-")
	id, err := strconv.ParseInt(s, 10, 64)
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("invalid reference %q: use %s-<number>", ref, prefix)
	}
	return id, nil
}

// emitRequest anunta UI-ul ambelor proiecte ca o cerere s-a schimbat.
func (t *Team) emitRequest(r store.ProjectRequest) {
	t.emit(r.FromProject, "project_request", r)
	t.emit(r.ToProject, "project_request", r)
}

// ListLinkedProjects e tool-ul list_linked_projects.
func (t *Team) ListLinkedProjects(c mcp.Caller) (string, error) {
	links, err := t.st.LinkedProjects(c.ProjectID)
	if err != nil {
		return "", err
	}
	if len(links) == 0 {
		return "This project has no linked projects.", nil
	}
	var b strings.Builder
	b.WriteString("Linked projects:")
	for _, l := range links {
		root := "no root agent"
		if r, ok, err := t.rootOf(l.ID); err == nil && ok {
			root = "root: " + r.Name
		}
		fmt.Fprintf(&b, "\n- %s (%s): %s", l.Name, root, orDefault(strings.TrimSpace(l.Goal), "no goal set"))
	}
	return b.String(), nil
}

// AskProject e tool-ul ask_project: intrebarea ajunge ca nota la radacina proiectului legat.
func (t *Team) AskProject(c mcp.Caller, project, question string) (string, error) {
	t.work.Lock()
	defer t.work.Unlock()
	if t.answeringOf(c.AgentID, c.RunID) {
		return "", errors.New("you are answering a question from another project; answer with what you know, or say you don't know")
	}
	question = strings.TrimSpace(question)
	if question == "" {
		return "", errors.New("question is required")
	}
	if utf8.RuneCountInString(question) > 2000 {
		return "", errors.New("the question is too long: at most 2000 characters")
	}
	target, err := t.linkedTarget(c.ProjectID, project)
	if err != nil {
		return "", err
	}
	asker, err := t.st.GetAgent(c.AgentID)
	if err != nil {
		return "", errors.New("your agent no longer exists")
	}
	n, err := t.st.CountOpenQuestionsFrom(asker.ID)
	if err != nil {
		return "", err
	}
	if n >= MaxOpenQuestions {
		return "", fmt.Errorf("you already have %d open questions to other projects; wait for the answers", MaxOpenQuestions)
	}
	root, ok, err := t.rootOf(target.ID)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", fmt.Errorf("%q has no root agent to receive it", target.Name)
	}
	askerID := asker.ID
	r := store.ProjectRequest{Kind: store.RequestQuestion, FromProject: c.ProjectID, FromAgent: &askerID,
		ToProject: target.ID, Text: question, Status: store.ReqOpen}
	if c.TaskID != 0 {
		id := c.TaskID
		r.FromTask = &id
	}
	if err := t.st.AddProjectRequest(&r); err != nil {
		return "", err
	}
	text := fmt.Sprintf("Question from %s (%s) [%s]: %s\nAnswer it with reply_to_project(request: %q, answer).",
		t.projectName(c.ProjectID), asker.Name, r.Ref(), question, r.Ref())
	m := store.Message{ProjectID: target.ID, AgentID: root.ID, Kind: store.KindEvent, Text: clip(text), RequestID: &r.ID}
	if err := t.st.AddMessage(&m); err != nil {
		return "", err
	}
	t.emit(target.ID, "message", m)
	t.emitRequest(r)
	t.Kick(target.ID)
	out := fmt.Sprintf("Question sent to %s as %s. The answer arrives as a message; continue meanwhile.", target.Name, r.Ref())
	if p, err := t.st.GetProject(target.ID); err == nil && p.Paused {
		out += fmt.Sprintf(" %s is paused: it answers when the user starts it.", target.Name)
	}
	return out, nil
}

// ReplyToProject e tool-ul reply_to_project: raspunsul ajunge ca nota la cel care a intrebat.
func (t *Team) ReplyToProject(c mcp.Caller, request, answer string) (string, error) {
	t.work.Lock()
	defer t.work.Unlock()
	id, err := parseRequestRef(request, "Q")
	if err != nil {
		return "", err
	}
	answer = strings.TrimSpace(answer)
	if answer == "" {
		return "", errors.New("answer is required")
	}
	if utf8.RuneCountInString(answer) > 10000 {
		return "", errors.New("the answer is too long: at most 10000 characters")
	}
	agents, err := t.st.ListAgents(c.ProjectID)
	if err != nil {
		return "", err
	}
	me, ok := store.FindAgent(agents, c.AgentID)
	if !ok || !store.RolesOf(me, agents).IsRoot {
		return "", errors.New("only the root agent of this project answers questions from linked projects")
	}
	r, err := t.st.GetProjectRequest(id)
	if errors.Is(err, store.ErrNotFound) {
		return "", fmt.Errorf("there is no question Q-%d", id)
	}
	if err != nil {
		return "", err
	}
	if r.Kind != store.RequestQuestion || r.ToProject != c.ProjectID {
		return "", fmt.Errorf("%s is not a question to this project", r.Ref())
	}
	if r.Status != store.ReqOpen {
		return "", fmt.Errorf("%s is already closed (%s)", r.Ref(), r.Status)
	}
	if err := t.st.TransitionProjectRequest(r.ID, store.ReqOpen, store.ReqAnswered, answer, nil); err != nil {
		return "", err
	}
	r.Status, r.Answer = store.ReqAnswered, answer
	if r.FromAgent != nil {
		// cererea e deja raspunsa: eroarea nu se intoarce (o reincercare ar primi "already closed"), doar se logheaza
		if err := t.note(r.FromProject, *r.FromAgent, clip(fmt.Sprintf("Answer from %s to %s: %s", t.projectName(c.ProjectID), r.Ref(), answer)), false); err != nil {
			log.Printf("reply %s: note to asker: %v", r.Ref(), err)
		}
		t.Kick(r.FromProject)
	}
	t.emitRequest(r)
	return fmt.Sprintf("Answer sent for %s.", r.Ref()), nil
}

// RequestProjectWork: stub pana la Task 4.
func (t *Team) RequestProjectWork(c mcp.Caller, project, title, description string) (string, error) {
	return "", errors.New("not available yet")
}
