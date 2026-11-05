package team

import (
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"unicode/utf8"

	"hobstack/internal/mcp"
	"hobstack/internal/runner"
	"hobstack/internal/store"
)

// MaxOpenQuestions: cate intrebari fara raspuns poate avea un agent catre alte proiecte.
const MaxOpenQuestions = 3

// MaxAnswer: lungimea maxima a unui raspuns (incape intreg in nota catre cel care a intrebat).
const MaxAnswer = 3800

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
	open, err := t.st.OpenQuestionsFrom(asker.ID)
	if err != nil {
		return "", err
	}
	if len(open) >= MaxOpenQuestions {
		refs := make([]string, len(open))
		for i, q := range open {
			refs[i] = q.Ref()
		}
		return "", fmt.Errorf("you already have %d open questions to other projects (%s); wait for the answers", len(open), strings.Join(refs, ", "))
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
	if utf8.RuneCountInString(answer) > MaxAnswer {
		return "", fmt.Errorf("the answer is too long: at most %d characters", MaxAnswer)
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
	if err := t.answerQuestion(r, answer); err != nil {
		return "", err
	}
	return fmt.Sprintf("Answer sent for %s.", r.Ref()), nil
}

// answerQuestion marcheaza intrebarea (open) raspunsa si ii trimite raspunsul
// celui care a intrebat. Se cheama cu t.work tinut.
func (t *Team) answerQuestion(r store.ProjectRequest, answer string) error {
	if err := t.st.TransitionProjectRequest(r.ID, store.ReqOpen, store.ReqAnswered, answer, nil); err != nil {
		return err
	}
	r.Status, r.Answer = store.ReqAnswered, answer
	if r.FromAgent != nil {
		// cererea e deja raspunsa: eroarea nu se intoarce (o reincercare ar primi "already closed"), doar se logheaza
		if err := t.note(r.FromProject, *r.FromAgent, clip(fmt.Sprintf("Answer from %s to %s: %s", t.projectName(r.ToProject), r.Ref(), answer)), false); err != nil {
			log.Printf("reply %s: note to asker: %v", r.Ref(), err)
		}
		t.Kick(r.FromProject)
	}
	t.emitRequest(r)
	return nil
}

// settleQuestions: la finalul unui run, intrebarile primite in el si ramase
// deschise (radacina nu a chemat reply_to_project) primesc ca raspuns textul
// final al run-ului; fara text, se inchid. Altfel ar ramane deschise pentru
// totdeauna si ar bloca limita celui care a intrebat. Se cheama cu t.work tinut.
func (t *Team) settleQuestions(ar activeRun, o runner.Outcome) {
	answer := strings.TrimSpace(o.Result)
	if r := []rune(answer); len(r) > MaxAnswer {
		answer = string(r[:MaxAnswer])
	}
	for _, id := range ar.questions {
		r, err := t.st.GetProjectRequest(id)
		if err != nil {
			if !errors.Is(err, store.ErrNotFound) {
				log.Printf("run %s: question %d: %v", o.RunID, id, err)
			}
			continue
		}
		if r.Status != store.ReqOpen {
			continue // deja raspunsa cu reply_to_project, sau inchisa
		}
		if answer == "" {
			t.closeOpenRequest(r, "its root did not answer")
			continue
		}
		if err := t.answerQuestion(r, answer); err != nil {
			log.Printf("run %s: answer %s: %v", o.RunID, r.Ref(), err)
		}
	}
}

// RequestProjectWork e tool-ul request_project_work: direct intr-un run pornit
// din chat-ul utilizatorului, altfel o cerere in "Needs you" la proiectul tinta.
func (t *Team) RequestProjectWork(c mcp.Caller, project, title, description string) (string, error) {
	t.work.Lock()
	defer t.work.Unlock()
	agents, err := t.st.ListAgents(c.ProjectID)
	if err != nil {
		return "", err
	}
	lead, ok := store.FindAgent(agents, c.AgentID)
	if !ok {
		return "", errors.New("your agent no longer exists")
	}
	if !canAddAgents(lead, agents) {
		return "", errors.New("only a lead or an agent that reports to the user can request work from another project")
	}
	title, description = strings.TrimSpace(title), strings.TrimSpace(description)
	switch {
	case title == "":
		return "", errors.New("title is required")
	case utf8.RuneCountInString(title) > 200:
		return "", errors.New("title: at most 200 characters")
	case utf8.RuneCountInString(description) > 10000:
		return "", errors.New("description: at most 10000 characters")
	}
	target, err := t.linkedTarget(c.ProjectID, project)
	if err != nil {
		return "", err
	}
	if _, ok, err := t.rootOf(target.ID); err != nil {
		return "", err
	} else if !ok {
		return "", fmt.Errorf("%q has no root agent to receive it", target.Name)
	}
	leadID := lead.ID
	r := store.ProjectRequest{Kind: store.RequestWork, FromProject: c.ProjectID, FromAgent: &leadID,
		ToProject: target.ID, Title: title, Text: description, Status: store.ReqPending}
	if c.TaskID != 0 {
		id := c.TaskID
		r.FromTask = &id
	}
	if err := t.st.AddProjectRequest(&r); err != nil {
		return "", err
	}
	// doar o cerere scrisa de utilizator in chat merge direct (Full autonomy nu)
	if _, fromChat := t.requestOf(c.AgentID, c.RunID); fromChat {
		task, err := t.startRequestedWork(&r)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("%s got your request as %s (task %s). You will get its report as a message.", target.Name, r.Ref(), task.Ref()), nil
	}
	t.emitRequest(r)
	return fmt.Sprintf("Request %s sent to %s; the user approves it first. You will get a message either way.", r.Ref(), target.Name), nil
}

// startRequestedWork creeaza task-ul cererii in proiectul tinta (pentru radacina,
// sau in prima etapa) si marcheaza cererea aprobata. Se cheama cu t.work tinut.
func (t *Team) startRequestedWork(r *store.ProjectRequest) (store.Task, error) {
	root, ok, err := t.rootOf(r.ToProject)
	if err != nil {
		return store.Task{}, err
	}
	if !ok {
		return store.Task{}, ProjectRequestError(fmt.Sprintf("%s has no root agent to receive it", t.projectName(r.ToProject)))
	}
	agentID := root.ID // la radacina, pe drumul standard (si intr-un proiect cu etape)
	from, by := t.projectName(r.FromProject), t.agentNameOf(r.FromAgent)
	desc := strings.TrimSpace(r.Text + fmt.Sprintf("\n\nRequested by %s (%s) as %s.", from, by, r.Ref()))
	task, err := t.userCreateTask(r.ToProject, r.Title, desc, agentID, taskOrigin{request: r, from: from, by: by})
	if err != nil {
		return store.Task{}, err
	}
	if err := t.st.TransitionProjectRequest(r.ID, store.ReqPending, store.ReqApproved, "", &task.ID); err != nil {
		return store.Task{}, err
	}
	r.Status, r.ToTask = store.ReqApproved, &task.ID
	t.emitRequest(*r)
	return task, nil
}

// ApproveProjectRequest: utilizatorul aproba o cerere de lucru din "Needs you".
func (t *Team) ApproveProjectRequest(id int64) error {
	t.work.Lock()
	defer t.work.Unlock()
	r, err := t.st.GetProjectRequest(id)
	if err != nil {
		return err
	}
	if r.Kind != store.RequestWork || r.Status != store.ReqPending {
		return store.ErrConflict
	}
	linked, err := t.st.AreLinked(r.FromProject, r.ToProject)
	if err != nil {
		return err
	}
	if !linked {
		return ProjectRequestError("the projects are no longer linked")
	}
	task, err := t.startRequestedWork(&r)
	if err != nil {
		return err
	}
	if r.FromAgent != nil {
		if err := t.note(r.FromProject, *r.FromAgent, fmt.Sprintf("%s accepted %s (task %s there).", t.projectName(r.ToProject), r.Ref(), task.Ref()), false); err != nil {
			log.Printf("approve %s: note to requester: %v", r.Ref(), err)
		}
		t.Kick(r.FromProject)
	}
	return nil
}

// DismissProjectRequest: utilizatorul respinge o cerere de lucru.
func (t *Team) DismissProjectRequest(id int64) error {
	t.work.Lock()
	defer t.work.Unlock()
	r, err := t.st.GetProjectRequest(id)
	if err != nil {
		return err
	}
	if r.Kind != store.RequestWork {
		return store.ErrConflict
	}
	if err := t.st.TransitionProjectRequest(r.ID, store.ReqPending, store.ReqDismissed, "", nil); err != nil {
		return err
	}
	r.Status = store.ReqDismissed
	if r.FromAgent != nil {
		if err := t.note(r.FromProject, *r.FromAgent, fmt.Sprintf("The user dismissed your request %s to %s.", r.Ref(), t.projectName(r.ToProject)), false); err != nil {
			log.Printf("dismiss %s: note to requester: %v", r.Ref(), err)
		}
		t.Kick(r.FromProject)
	}
	t.emitRequest(r)
	return nil
}

// deliverRequest: un task cerut de un proiect legat a ajuns in Done; raportul
// pleaca inapoi la cel care a cerut. Se cheama din afterDone, cu t.work tinut.
func (t *Team) deliverRequest(task store.Task) error {
	r, err := t.st.GetProjectRequest(*task.RequestID)
	if errors.Is(err, store.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if r.Status != store.ReqApproved {
		return nil
	}
	report := orDefault(strings.TrimSpace(task.Result), "(no report)")
	if err := t.st.TransitionProjectRequest(r.ID, store.ReqApproved, store.ReqDone, report, nil); err != nil {
		return err
	}
	r.Status, r.Answer = store.ReqDone, report
	if r.FromAgent != nil {
		text := fmt.Sprintf("%s finished %s: %s", t.projectName(task.ProjectID), r.Ref(), report)
		if r.FromTask != nil {
			if ft, err := t.st.GetTask(*r.FromTask); err == nil {
				text += fmt.Sprintf(" (your task %s)", ft.Ref())
			}
		}
		if err := t.note(r.FromProject, *r.FromAgent, clip(text), false); err != nil {
			log.Printf("deliver %s: note to requester: %v", r.Ref(), err)
		}
		t.Kick(r.FromProject)
	}
	t.emitRequest(r)
	return nil
}

// finishRequestedTask: radacina livreaza cu report_done un task cerut de un
// proiect legat. Nu are sef: cu reviewer, task-ul trece pe la el (livrarea se
// face cand ajunge in Done, din afterDone); fara, direct in Done. Cu t.work tinut.
func (t *Team) finishRequestedTask(task *store.Task, root store.Agent, agents []store.Agent, summary string) (string, error) {
	subs, err := t.st.Subtasks(task.ID)
	if err != nil {
		return "", err
	}
	var open []string
	for _, s := range subs {
		if s.Status != store.StatusDone {
			open = append(open, s.Ref())
		}
	}
	if len(open) > 0 {
		return "", fmt.Errorf("%s still has open subtasks: %s. Report after they are done.", task.Ref(), strings.Join(open, ", "))
	}
	r, err := t.st.GetProjectRequest(*task.RequestID)
	if err != nil {
		return "", err
	}
	from := t.projectName(r.FromProject)
	task.Result = summary
	if root.ReviewerID != nil {
		if who, ok := store.FindAgent(agents, *root.ReviewerID); ok {
			if _, err := t.toReview(task, root, agents, root.Name+" finished "+task.Ref(), reportedNote(root, summary), true); err != nil {
				return "", err
			}
			t.note(task.ProjectID, root.ID, "Reported "+task.Ref(), true)
			t.Kick(task.ProjectID)
			return fmt.Sprintf("%s is in Review. %s will check it; the report goes back to %s when it is accepted.", task.Ref(), who.Name, from), nil
		}
	}
	markDone(task)
	if err := t.st.SaveTask(task); err != nil {
		return "", err
	}
	t.taskMsg(*task, "reported", root.Name, from, root.Name+" reported it done", summary)
	if err := t.note(task.ProjectID, root.ID, "Reported "+task.Ref()+" to "+from, true); err != nil {
		log.Printf("finish %s: note: %v", task.Ref(), err)
	}
	t.emit(task.ProjectID, "task", *task)
	if err := t.afterDone(*task); err != nil {
		log.Printf("after report %s: %v", task.Ref(), err)
	}
	t.Kick(task.ProjectID)
	return fmt.Sprintf("%s is Done; the report went back to %s.", task.Ref(), from), nil
}

// UnlinkProjects sterge legatura; intrebarile deschise se inchid, cererile
// neaprobate se resping, iar cei care au cerut afla. Cererile aprobate continua.
func (t *Team) UnlinkProjects(a, b int64) error {
	t.work.Lock()
	defer t.work.Unlock()
	reqs, err := t.st.OpenRequestsBetween(a, b)
	if err != nil {
		return err
	}
	if err := t.st.UnlinkProjects(a, b); err != nil {
		return err
	}
	for _, r := range reqs {
		t.closeOpenRequest(r, "the projects are no longer linked")
	}
	return nil
}

// BeforeDeleteProject anunta pe cei care asteptau ceva de la proiect (intrebari,
// cereri neaprobate sau in lucru); se cheama inainte de stergerea proiectului.
func (t *Team) BeforeDeleteProject(id int64) error {
	t.work.Lock()
	defer t.work.Unlock()
	reqs, err := t.st.OpenRequestsTo(id)
	if err != nil {
		return err
	}
	for _, r := range reqs {
		t.closeOpenRequest(r, "the project was deleted")
	}
	return nil
}

// closeOpenRequest inchide o cerere care inca astepta (open/approved -> closed,
// pending -> dismissed) si ii spune celui care a cerut de ce. Cu t.work tinut.
func (t *Team) closeOpenRequest(r store.ProjectRequest, why string) {
	to := store.ReqClosed
	if r.Status == store.ReqPending {
		to = store.ReqDismissed
	}
	if err := t.st.TransitionProjectRequest(r.ID, r.Status, to, "", nil); err != nil {
		log.Printf("close %s: %v", r.Ref(), err)
		return
	}
	r.Status = to
	if r.FromAgent != nil {
		if err := t.note(r.FromProject, *r.FromAgent, fmt.Sprintf("%s to %s was closed: %s.", r.Ref(), t.projectName(r.ToProject), why), false); err != nil {
			log.Printf("close %s: note to requester: %v", r.Ref(), err)
		}
		t.Kick(r.FromProject)
	}
	t.emitRequest(r)
}
