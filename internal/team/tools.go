package team

import (
	"errors"
	"fmt"
	"log"
	"slices"
	"strconv"
	"strings"

	"foreman/internal/mcp"
	"foreman/internal/store"
)

// MaxSendBacks: dupa atatea trimiteri inapoi, task-ul se blocheaza si decide utilizatorul.
const MaxSendBacks = 3

var _ mcp.Backend = (*Team)(nil) // compilatorul verifica ca Team implementeaza interfata

func (t *Team) CreateTask(c mcp.Caller, title, description, assignee string) (string, error) {
	t.work.Lock()
	defer t.work.Unlock()

	// flag-urile din Caller pot fi vechi (graful s-a editat in timpul run-ului), deci verificam din DB
	agents, err := t.st.ListAgents(c.ProjectID)
	if err != nil {
		return "", err
	}
	stages, err := t.st.ListStages(c.ProjectID)
	if err != nil {
		return "", err
	}
	if len(stages) > 0 {
		return t.createStageTask(c, agents, stages, title, description, assignee)
	}
	children := store.Children(c.AgentID, agents)
	if len(children) == 0 {
		return "", errors.New("only a lead (an agent with a direct team) can create tasks")
	}
	title = strings.TrimSpace(title)
	if title == "" {
		return "", errors.New("title is required")
	}
	w, err := t.st.FindAgentByName(c.ProjectID, strings.TrimSpace(assignee))
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return "", err
	}
	if err != nil || !isChild(children, w.ID) {
		names := make([]string, 0, len(children))
		for _, ch := range children {
			names = append(names, ch.Name)
		}
		return "", fmt.Errorf("%q is not on your direct team. Your direct team: %s", assignee, strings.Join(names, ", "))
	}
	lead, ok := store.FindAgent(agents, c.AgentID)
	if !ok {
		return "", errors.New("your agent no longer exists")
	}

	parentID, ambiguous, err := t.parentTaskFor(c)
	if err != nil {
		return "", err
	}
	task := store.Task{ProjectID: c.ProjectID, Title: title, Description: strings.TrimSpace(description),
		AssigneeID: w.ID, CreatorID: &lead.ID, ParentTaskID: parentID}
	if err := t.st.CreateTask(&task); err != nil {
		return "", err
	}
	t.note(c.ProjectID, lead.ID, fmt.Sprintf("Created %s for %s: %s", task.Ref(), w.Name, title), true)
	t.note(c.ProjectID, w.ID, fmt.Sprintf("%s assigned by %s: %s", task.Ref(), lead.Name, title), true)
	t.emit(c.ProjectID, "task", task)
	t.Kick(c.ProjectID)
	reply := fmt.Sprintf("Created %s for %s. They will receive it automatically.", task.Ref(), w.Name)
	if ambiguous {
		reply += " Created without a parent task: you have several tasks in progress."
	}
	return reply, nil
}

// createStageTask: intr-un proiect cu etape task-ul intra in prima etapa;
// assignee e optional si trebuie sa fie din acea etapa. Poate crea orice lider
// sau radacina (etapele decid cine lucreaza). Se apeleaza cu t.work blocat.
func (t *Team) createStageTask(c mcp.Caller, agents []store.Agent, stages []store.Stage, title, description, assignee string) (string, error) {
	lead, ok := store.FindAgent(agents, c.AgentID)
	if !ok {
		return "", errors.New("your agent no longer exists")
	}
	roles := store.RolesOf(lead, agents)
	if !roles.IsLead && !roles.IsRoot {
		return "", errors.New("only a lead or an agent that reports to the user can create tasks")
	}
	title = strings.TrimSpace(title)
	if title == "" {
		return "", errors.New("title is required")
	}
	first := stages[0]
	who := placeholder(first, lead.ID)
	if name := strings.TrimSpace(assignee); name != "" {
		w, err := t.st.FindAgentByName(c.ProjectID, name)
		if err != nil || !slices.Contains(first.AgentIDs, w.ID) {
			return "", fmt.Errorf("%q is not in stage %s (the first stage); leave assignee empty so the first free agent takes it", name, first.Name)
		}
		who = w.ID
	}
	parentID, _, err := t.parentTaskFor(c)
	if err != nil {
		return "", err
	}
	task := store.Task{ProjectID: c.ProjectID, Title: title, Description: strings.TrimSpace(description),
		AssigneeID: who, CreatorID: &lead.ID, ParentTaskID: parentID, StageID: &first.ID}
	if err := t.st.CreateTask(&task); err != nil {
		return "", err
	}
	t.note(c.ProjectID, lead.ID, fmt.Sprintf("Created %s in stage %s: %s", task.Ref(), first.Name, title), true)
	t.emit(c.ProjectID, "task", task)
	t.Kick(c.ProjectID)
	return fmt.Sprintf("Created %s in stage %s. The first free agent in the stage will take it.", task.Ref(), first.Name), nil
}

func isChild(children []store.Agent, id int64) bool {
	for _, ch := range children {
		if ch.ID == id {
			return true
		}
	}
	return false
}

// parentTaskFor alege task-ul din care se sparge un sub-task nou: task-ul
// run-ului curent, sau singurul task in lucru al apelantului (run de notificari).
// ambiguous e true cand apelantul are mai multe task-uri in lucru: atunci
// sub-task-ul se creeaza fara parinte, ca sa nu il legam de unul gresit.
func (t *Team) parentTaskFor(c mcp.Caller) (parent *int64, ambiguous bool, err error) {
	if c.TaskID != 0 {
		return &c.TaskID, false, nil
	}
	tasks, err := t.st.ListTasks(c.ProjectID)
	if err != nil {
		return nil, false, err
	}
	for _, task := range tasks {
		if task.AssigneeID == c.AgentID && task.Status == store.StatusInProgress {
			if parent != nil {
				return nil, true, nil
			}
			id := task.ID
			parent = &id
		}
	}
	return parent, false, nil
}

func (t *Team) ListTasks(c mcp.Caller, status string) (string, error) {
	t.work.Lock()
	defer t.work.Unlock()

	status = strings.TrimSpace(status)
	switch status {
	case "", store.StatusQueued, store.StatusInProgress, store.StatusReview, store.StatusDone, store.StatusBlocked:
	default:
		return "", fmt.Errorf("unknown status %q", status)
	}
	tasks, err := t.st.ListTasks(c.ProjectID)
	if err != nil {
		return "", err
	}
	agents, err := t.st.ListAgents(c.ProjectID)
	if err != nil {
		return "", err
	}
	names := map[int64]string{}
	for _, a := range agents {
		names[a.ID] = a.Name
	}

	var b strings.Builder
	for _, task := range tasks {
		if status != "" && task.Status != status {
			continue
		}
		fmt.Fprintf(&b, "%s [%s] %s -> %s\n", task.Ref(), task.Status, task.Title, names[task.AssigneeID])
		if (task.Status == store.StatusReview || task.Status == store.StatusBlocked) && task.Result != "" {
			fmt.Fprintf(&b, "  Result: %s\n", clip(task.Result))
		}
	}
	if b.Len() == 0 {
		return "No tasks.", nil
	}
	return strings.TrimRight(b.String(), "\n"), nil
}

// decider intoarce liderul care decide un task din Review: creatorul, iar
// daca a fost sters, seful asignatului.
func decider(task store.Task, agents []store.Agent) (store.Agent, bool) {
	if task.ByUser {
		return store.Agent{}, false // creat din board: decide utilizatorul
	}
	if task.CreatorID != nil {
		if a, ok := store.FindAgent(agents, *task.CreatorID); ok {
			return a, true
		}
	}
	if w, ok := store.FindAgent(agents, task.AssigneeID); ok && w.ParentID != nil {
		return store.FindAgent(agents, *w.ParentID)
	}
	return store.Agent{}, false
}

// reviewerOf intoarce reviewer-ul asignatului task-ului, daca are unul care exista.
func reviewerOf(task store.Task, agents []store.Agent) (store.Agent, bool) {
	w, ok := store.FindAgent(agents, task.AssigneeID)
	if !ok || w.ReviewerID == nil {
		return store.Agent{}, false
	}
	return store.FindAgent(agents, *w.ReviewerID)
}

// decisionOwner intoarce agentul care trebuie sa decida acum un task din
// Review sau Blocked: reviewer-ul in etapa reviewer (daca inca exista),
// altfel decidentul. Intoarce 0 daca nu decide nimeni.
func decisionOwner(task store.Task, agents []store.Agent) int64 {
	if task.Status != store.StatusReview && task.Status != store.StatusBlocked {
		return 0
	}
	if task.Status == store.StatusReview && task.ReviewStage == store.StageReviewer {
		if r, ok := reviewerOf(task, agents); ok {
			return r.ID
		}
	}
	if d, ok := decider(task, agents); ok {
		return d.ID
	}
	return 0
}

// noDeciderText e nota pentru asignatul unui task pe care nu il poate decide nimeni.
func noDeciderText(task store.Task, assignee store.Agent) string {
	if assignee.ParentID == nil {
		return fmt.Sprintf("No one can check %s. The user decides from the board (Accept / Unblock).", task.Ref())
	}
	return fmt.Sprintf("No one can check %s: the agent has no boss. Link it to a lead on the canvas.", task.Ref())
}

// canDecide: accept_task si send_back sunt permise creatorului si oricarui
// sef de-al lui. Fara creator (sters), oricarui sef al asignatului.
func canDecide(callerID int64, task store.Task, agents []store.Agent) bool {
	if task.ByUser {
		return false // creat din board: decide doar utilizatorul
	}
	if task.CreatorID != nil {
		return *task.CreatorID == callerID || store.IsAncestor(callerID, *task.CreatorID, agents)
	}
	return store.IsAncestor(callerID, task.AssigneeID, agents)
}

// toReview muta un task (cu Result deja pus) in Review: la reviewer-ul
// asignatului daca are, altfel la liderul care decide. Intoarce agentul
// notificat (zero daca nu exista). Se apeleaza cu t.work blocat.
func (t *Team) toReview(task *store.Task, assignee store.Agent, agents []store.Agent, headline string) (store.Agent, error) {
	// task dintr-o etapa terminat fara raport: la lider (sau la utilizator), in aceeasi etapa
	if task.StageID != nil {
		task.Status, task.ReviewStage = store.StatusReview, store.StageLead
		if err := t.st.SaveTask(task); err != nil {
			return store.Agent{}, err
		}
		t.emit(task.ProjectID, "task", *task)
		boss, ok := decider(*task, agents)
		if ok {
			t.note(boss.ProjectID, boss.ID, clip(fmt.Sprintf("%s (review): %s", headline, task.Result)), false)
		}
		return boss, nil
	}
	var who store.Agent
	var hasWho bool
	task.Status = store.StatusReview
	if assignee.ReviewerID != nil {
		who, hasWho = store.FindAgent(agents, *assignee.ReviewerID)
	}
	if hasWho {
		task.ReviewStage = store.StageReviewer
	} else {
		task.ReviewStage = store.StageLead
		who, hasWho = decider(*task, agents)
	}
	if !hasWho && !task.ByUser {
		// nimeni nu poate decide: nu lasam task-ul blocat in Review, decide utilizatorul
		task.Status = store.StatusBlocked
		task.ReviewStage = store.StageNone
	}
	if err := t.st.SaveTask(task); err != nil {
		return store.Agent{}, err
	}
	switch {
	case !hasWho && task.ByUser:
		// task-ul utilizatorului: decide el din board (Accepta / Trimite inapoi)
	case !hasWho:
		t.note(task.ProjectID, assignee.ID, noDeciderText(*task, assignee), true)
	case task.ReviewStage == store.StageReviewer:
		t.note(who.ProjectID, who.ID, clip(fmt.Sprintf("%s. Check it with review_approve or review_reject: %s", headline, task.Result)), false)
	default:
		t.note(who.ProjectID, who.ID, clip(fmt.Sprintf("%s (review): %s", headline, task.Result)), false)
	}
	t.emit(task.ProjectID, "task", *task)
	return who, nil
}

// afterDone: cand ultimul sub-task al unui task ajunge in Done, task-ul
// parinte revine in Queued la acelasi asignat (sub-liderul). Asa primeste un
// run pe task, cu lista sub-task-urilor, si il raporteaza in sus; finalul
// run-ului (recordOutcome) trateaza orice iesire. Se apeleaza cu t.work blocat.
func (t *Team) afterDone(task store.Task) error {
	if task.ParentTaskID == nil {
		return nil
	}
	parent, err := t.st.GetTask(*task.ParentTaskID)
	if err != nil || parent.Status != store.StatusInProgress {
		return err
	}
	subs, err := t.st.Subtasks(parent.ID)
	if err != nil {
		return err
	}
	for _, s := range subs {
		if s.Status != store.StatusDone {
			return nil
		}
	}
	// daca sub-liderul lucreaza chiar acum pe parinte, finalul run-ului lui
	// muta task-ul in Review; nu il punem inapoi in coada
	t.mu.Lock()
	ar, busy := t.active[parent.AssigneeID]
	t.mu.Unlock()
	if busy && ar.taskID == parent.ID {
		return nil
	}
	parent.Status = store.StatusQueued
	if err := t.st.SaveTask(&parent); err != nil {
		return err
	}
	t.emit(parent.ProjectID, "task", parent)
	text := fmt.Sprintf("All subtasks for %s are done; %s is back in the queue so you can check it and report it.", parent.Ref(), parent.Ref())
	return t.note(parent.ProjectID, parent.AssigneeID, text, true)
}

// rejectTask e partea comuna a send_back si review_reject: numara respingerea
// si, la MaxSendBacks, blocheaza task-ul si anunta reviewer-ul si decidentul.
// Intoarce true daca task-ul s-a blocat; altfel a ramas Queued, iar apelantul
// scrie notele si trezeste asignatul. Se apeleaza cu t.work blocat.
func (t *Team) rejectTask(task *store.Task, feedback string, agents []store.Agent, extra int64) (bool, error) {
	task.SendBacks++
	task.Feedback = feedback
	task.ReviewStage = store.StageNone
	blocked := task.SendBacks >= MaxSendBacks
	if blocked {
		task.Status = store.StatusBlocked
	} else {
		task.Status = store.StatusQueued
	}
	if err := t.st.SaveTask(task); err != nil {
		return false, err
	}
	if blocked {
		ids := map[int64]bool{extra: true}
		if w, ok := store.FindAgent(agents, task.AssigneeID); ok && w.ReviewerID != nil {
			ids[*w.ReviewerID] = true
		}
		if d, ok := decider(*task, agents); ok {
			ids[d.ID] = true
		}
		text := fmt.Sprintf("%s is blocked after %d rejections or send-backs. The user decides.", task.Ref(), MaxSendBacks)
		for id := range ids {
			t.note(task.ProjectID, id, text, true)
		}
	}
	t.emit(task.ProjectID, "task", *task)
	return blocked, nil
}

func blockedReply(task store.Task) string {
	return fmt.Sprintf("%s was sent back %d times and is now blocked. The user decides what happens to it.", task.Ref(), MaxSendBacks)
}

func (t *Team) AcceptTask(c mcp.Caller, ref, note string) (string, error) {
	t.work.Lock()
	defer t.work.Unlock()

	agents, err := t.st.ListAgents(c.ProjectID)
	if err != nil {
		return "", err
	}
	task, err := t.findTask(c.ProjectID, ref)
	if err != nil {
		return "", err
	}
	if !canDecide(c.AgentID, task, agents) {
		return "", fmt.Errorf("only the lead who created %s (or one of their bosses) decides", task.Ref())
	}
	if err := t.checkNotAtReviewer(task, agents); err != nil {
		return "", err
	}
	// checkNotAtReviewer a trecut: task-ul e la lider (sau reviewer-ul a disparut)
	if task.Status != store.StatusReview && task.Status != store.StatusBlocked {
		return "", fmt.Errorf("%s is %s; only tasks in review or blocked can be accepted", task.Ref(), task.Status)
	}
	task.Status = store.StatusDone
	task.ReviewStage = store.StageNone
	task.StageID = nil
	if err := t.st.SaveTask(&task); err != nil {
		return "", err
	}
	text := "Accepted " + task.Ref()
	if n := strings.TrimSpace(note); n != "" {
		text += ": " + n
	}
	t.note(c.ProjectID, c.AgentID, text, true)
	t.emit(c.ProjectID, "task", task)
	// task-ul e deja Done: o eroare la trezirea parintelui nu face accept-ul esuat
	if err := t.afterDone(task); err != nil {
		log.Printf("after accept %s: %v", task.Ref(), err)
	}
	t.Kick(c.ProjectID)
	return task.Ref() + " is Done.", nil
}

// checkNotAtReviewer refuza decizia liderului cat timp task-ul e la un
// reviewer care exista. Daca asignatul nu mai are reviewer (legatura stearsa
// sau reviewer sters), decide liderul, altfel task-ul ar ramane agatat.
func (t *Team) checkNotAtReviewer(task store.Task, agents []store.Agent) error {
	if task.Status != store.StatusReview || task.ReviewStage != store.StageReviewer {
		return nil
	}
	r, ok := reviewerOf(task, agents)
	if !ok {
		return nil
	}
	return fmt.Errorf("%s is still in review with %s", task.Ref(), r.Name)
}

func (t *Team) SendBack(c mcp.Caller, ref, feedback string) (string, error) {
	t.work.Lock()
	defer t.work.Unlock()

	feedback = strings.TrimSpace(feedback)
	if feedback == "" {
		return "", errors.New("feedback is required: say concretely what needs to change")
	}
	agents, err := t.st.ListAgents(c.ProjectID)
	if err != nil {
		return "", err
	}
	task, err := t.findTask(c.ProjectID, ref)
	if err != nil {
		return "", err
	}
	if !canDecide(c.AgentID, task, agents) {
		return "", fmt.Errorf("only the lead who created %s (or one of their bosses) decides", task.Ref())
	}
	if err := t.checkNotAtReviewer(task, agents); err != nil {
		return "", err
	}
	if task.Status != store.StatusReview {
		return "", fmt.Errorf("%s is %s; only tasks in review can be sent back", task.Ref(), task.Status)
	}
	worker, ok := store.FindAgent(agents, task.AssigneeID)
	lead, ok2 := store.FindAgent(agents, c.AgentID)
	if !ok || !ok2 {
		return "", errors.New("the agent no longer exists")
	}
	// proiect cu etape: inapoi in etapa in care era (sau in prima etapa)
	stages, err := t.st.ListStages(c.ProjectID)
	if err != nil {
		return "", err
	}
	if len(stages) > 0 {
		target := stages[0]
		if i := stageIndex(stages, task.StageID); i >= 0 {
			target = stages[i]
		}
		blocked, err := t.sendToStage(&task, target, feedback, agents, lead.ID)
		if err != nil {
			return "", err
		}
		if blocked {
			return blockedReply(task), nil
		}
		t.note(c.ProjectID, lead.ID, fmt.Sprintf("Sent %s back to stage %s: %s", task.Ref(), target.Name, feedback), true)
		t.Kick(c.ProjectID)
		return fmt.Sprintf("%s sent back to stage %s (%d/%d).", task.Ref(), target.Name, task.SendBacks, MaxSendBacks), nil
	}

	blocked, err := t.rejectTask(&task, feedback, agents, lead.ID)
	if err != nil {
		return "", err
	}
	if blocked {
		return blockedReply(task), nil
	}
	t.note(c.ProjectID, lead.ID, fmt.Sprintf("Sent %s back to %s: %s", task.Ref(), worker.Name, feedback), true)
	t.note(c.ProjectID, worker.ID, fmt.Sprintf("%s sent back by %s: %s", task.Ref(), lead.Name, feedback), true)
	t.Kick(c.ProjectID)
	return fmt.Sprintf("%s sent back to %s (%d/%d).", task.Ref(), worker.Name, task.SendBacks, MaxSendBacks), nil
}

func (t *Team) ReportDone(c mcp.Caller, ref, summary string) (string, error) {
	t.work.Lock()
	defer t.work.Unlock()

	agents, err := t.st.ListAgents(c.ProjectID)
	if err != nil {
		return "", err
	}
	worker, ok := store.FindAgent(agents, c.AgentID)
	if !ok {
		return "", errors.New("the agent no longer exists")
	}
	summary = strings.TrimSpace(summary)
	if summary == "" {
		return "", errors.New("summary is required")
	}
	var task store.Task
	switch {
	case strings.TrimSpace(ref) == "" && c.TaskID == 0 && worker.ParentID == nil:
		return "", errors.New("you report directly to the user: write the summary in your reply, without report_done")
	case strings.TrimSpace(ref) == "" && c.TaskID == 0:
		return "", errors.New(`say which task you are reporting on: report_done(task: "T-3", summary)`)
	case strings.TrimSpace(ref) == "":
		task, err = t.st.GetTask(c.TaskID)
	default:
		task, err = t.findTask(c.ProjectID, ref)
	}
	if err != nil {
		return "", err
	}
	if task.AssigneeID != worker.ID {
		return "", fmt.Errorf("%s is not assigned to you", task.Ref())
	}
	if task.Status != store.StatusInProgress {
		return "", fmt.Errorf("%s is no longer in progress (status %s)", task.Ref(), task.Status)
	}
	// task dintr-o etapa: trece in etapa urmatoare (intr-o etapa de verificare, ca o aprobare)
	if task.StageID != nil {
		stages, err := t.st.ListStages(c.ProjectID)
		if err != nil {
			return "", err
		}
		task.Result = summary
		t.note(c.ProjectID, worker.ID, "Finished "+task.Ref()+" in its stage", true)
		return t.advance(&task, stages, agents, worker.Name)
	}
	if worker.ParentID == nil {
		return "", errors.New("you report directly to the user: write the summary in your reply, without report_done")
	}
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

	task.Result = summary
	who, err := t.toReview(&task, worker, agents, worker.Name+" finished "+task.Ref())
	if err != nil {
		return "", err
	}
	t.note(c.ProjectID, worker.ID, "Reported "+task.Ref(), true)
	t.Kick(c.ProjectID)
	if task.Status == store.StatusBlocked {
		return fmt.Sprintf("%s is blocked: no one can check it.", task.Ref()), nil
	}
	if who.ID == 0 {
		return task.Ref() + " is in Review.", nil
	}
	return fmt.Sprintf("%s is in Review. %s will check it.", task.Ref(), who.Name), nil
}

// reviewTarget valideaza apelul unui reviewer si intoarce task-ul, asignatul si lista de agenti.
func (t *Team) reviewTarget(c mcp.Caller, ref string) (store.Task, store.Agent, []store.Agent, error) {
	agents, err := t.st.ListAgents(c.ProjectID)
	if err != nil {
		return store.Task{}, store.Agent{}, nil, err
	}
	task, err := t.findTask(c.ProjectID, ref)
	if err != nil {
		return task, store.Agent{}, nil, err
	}
	worker, ok := store.FindAgent(agents, task.AssigneeID)
	if !ok {
		return task, worker, nil, errors.New("the assignee no longer exists")
	}
	if worker.ReviewerID == nil || *worker.ReviewerID != c.AgentID {
		return task, worker, nil, fmt.Errorf("you do not review the work of %s", worker.Name)
	}
	if task.Status != store.StatusReview || task.ReviewStage != store.StageReviewer {
		return task, worker, nil, fmt.Errorf("%s is not waiting for review (status %s)", task.Ref(), task.Status)
	}
	return task, worker, agents, nil
}

func (t *Team) ReviewApprove(c mcp.Caller, ref, note string) (string, error) {
	t.work.Lock()
	defer t.work.Unlock()

	if task, err := t.findTask(c.ProjectID, ref); err == nil && task.StageID != nil {
		stages, agents, err := t.stageCheckTarget(c, task)
		if err != nil {
			return "", err
		}
		checker, _ := store.FindAgent(agents, c.AgentID)
		if n := strings.TrimSpace(note); n != "" {
			task.Result = strings.TrimSpace(task.Result + "\nReview note: " + n)
		}
		t.note(c.ProjectID, c.AgentID, "Approved "+task.Ref(), true)
		return t.advance(&task, stages, agents, checker.Name)
	}

	task, worker, agents, err := t.reviewTarget(c, ref)
	if err != nil {
		return "", err
	}
	reviewer, _ := store.FindAgent(agents, c.AgentID)
	if task.ByUser {
		// creat de utilizator: dupa verificare decide el
		task.ReviewStage = store.StageLead
		if err := t.st.SaveTask(&task); err != nil {
			return "", err
		}
		t.note(c.ProjectID, reviewer.ID, "Approved "+task.Ref(), true)
		t.emit(c.ProjectID, "task", task)
		return fmt.Sprintf("%s went to the user for a decision.", task.Ref()), nil
	}
	boss, ok := decider(task, agents)
	if !ok {
		task.Status, task.ReviewStage = store.StatusBlocked, store.StageNone
		if err := t.st.SaveTask(&task); err != nil {
			return "", err
		}
		t.note(c.ProjectID, worker.ID, noDeciderText(task, worker), true)
		t.emit(c.ProjectID, "task", task)
		return fmt.Sprintf("%s is blocked: no one can decide. The user decides.", task.Ref()), nil
	}
	task.ReviewStage = store.StageLead
	if err := t.st.SaveTask(&task); err != nil {
		return "", err
	}
	text := fmt.Sprintf("%s approved %s, done by %s (review): %s", reviewer.Name, task.Ref(), worker.Name, task.Result)
	if n := strings.TrimSpace(note); n != "" {
		text += " Note: " + n
	}
	t.note(c.ProjectID, boss.ID, clip(text), false)
	t.note(c.ProjectID, reviewer.ID, "Approved "+task.Ref(), true)
	t.emit(c.ProjectID, "task", task)
	t.Kick(c.ProjectID)
	return fmt.Sprintf("%s went to %s for a decision.", task.Ref(), boss.Name), nil
}

func (t *Team) ReviewReject(c mcp.Caller, ref, feedback string) (string, error) {
	t.work.Lock()
	defer t.work.Unlock()

	if task, err := t.findTask(c.ProjectID, ref); err == nil && task.StageID != nil {
		stages, agents, err := t.stageCheckTarget(c, task)
		if err != nil {
			return "", err
		}
		feedback = strings.TrimSpace(feedback)
		if feedback == "" {
			return "", errors.New("feedback is required: say concretely what needs to change")
		}
		back := backStage(stages, task.StageID)
		blocked, err := t.sendToStage(&task, back, feedback, agents, c.AgentID)
		if err != nil {
			return "", err
		}
		if blocked {
			return blockedReply(task), nil
		}
		t.note(c.ProjectID, c.AgentID, fmt.Sprintf("Rejected %s: %s", task.Ref(), feedback), true)
		t.Kick(c.ProjectID)
		return fmt.Sprintf("%s sent back to stage %s (%d/%d).", task.Ref(), back.Name, task.SendBacks, MaxSendBacks), nil
	}

	task, worker, agents, err := t.reviewTarget(c, ref)
	if err != nil {
		return "", err
	}
	feedback = strings.TrimSpace(feedback)
	if feedback == "" {
		return "", errors.New("feedback is required: say concretely what needs to change")
	}
	reviewer, _ := store.FindAgent(agents, c.AgentID)
	blocked, err := t.rejectTask(&task, feedback, agents, reviewer.ID)
	if err != nil {
		return "", err
	}
	if blocked {
		return blockedReply(task), nil
	}
	t.note(c.ProjectID, worker.ID, fmt.Sprintf("%s rejected in review by %s: %s", task.Ref(), reviewer.Name, feedback), true)
	t.note(c.ProjectID, reviewer.ID, fmt.Sprintf("Rejected %s: %s", task.Ref(), feedback), true)
	t.Kick(c.ProjectID)
	return fmt.Sprintf("%s sent back to %s (%d/%d).", task.Ref(), worker.Name, task.SendBacks, MaxSendBacks), nil
}

// findTask accepta "T-3", "t-3" sau "3".
func (t *Team) findTask(projectID int64, ref string) (store.Task, error) {
	clean := strings.TrimSpace(ref)
	clean = strings.TrimPrefix(strings.TrimPrefix(clean, "T-"), "t-")
	n, err := strconv.Atoi(clean)
	if err != nil {
		return store.Task{}, fmt.Errorf("invalid task %q; use the format T-3", ref)
	}
	task, err := t.st.GetTaskByNumber(projectID, n)
	if errors.Is(err, store.ErrNotFound) {
		return store.Task{}, fmt.Errorf("task T-%d does not exist", n)
	}
	return task, err
}
