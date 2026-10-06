package team

import (
	"errors"
	"fmt"
	"log"
	"slices"
	"strings"

	"foreman/internal/mcp"
	"foreman/internal/store"
)

// stageIndex intoarce pozitia etapei id in lista (-1 daca nu exista).
func stageIndex(stages []store.Stage, id *int64) int {
	if id == nil {
		return -1
	}
	for i, s := range stages {
		if s.ID == *id {
			return i
		}
	}
	return -1
}

// nextStage intoarce etapa de dupa cea curenta; false daca era ultima.
func nextStage(stages []store.Stage, cur *int64) (store.Stage, bool) {
	i := stageIndex(stages, cur)
	if i < 0 || i+1 >= len(stages) {
		return store.Stage{}, false
	}
	return stages[i+1], true
}

// backStage: unde intoarce o respingere din etapa cur: cea mai apropiata
// etapa de lucru dinainte, altfel prima etapa.
func backStage(stages []store.Stage, cur *int64) store.Stage {
	for i := stageIndex(stages, cur) - 1; i >= 0; i-- {
		if stages[i].Kind == store.KindWork {
			return stages[i]
		}
	}
	return stages[0]
}

// placeholder alege asignatul "loc tinut" cand task-ul intra intr-o etapa:
// primul agent al etapei, altfel fallback. Scheduler-ul il inlocuieste cu
// primul agent liber inainte sa porneasca run-ul.
func placeholder(st store.Stage, fallback int64) int64 {
	if len(st.AgentIDs) > 0 {
		return st.AgentIDs[0]
	}
	return fallback
}

// stageLine e linia din tura agentului care lucreaza intr-o etapa.
func stageLine(stages []store.Stage, task store.Task) string {
	i := stageIndex(stages, task.StageID)
	if i < 0 {
		return ""
	}
	st := stages[i]
	next := "the lead, who decides"
	if n, ok := nextStage(stages, task.StageID); ok {
		next = n.Name
	}
	if st.Kind == store.KindCheck {
		return fmt.Sprintf("Stage: %s (check). Check the work (read the files, not just the summary) and call "+
			"mcp__team__review_approve if it is good (it goes to %s) or mcp__team__review_reject with concrete feedback.", st.Name, next)
	}
	return fmt.Sprintf("Stage: %s (work). When you finish, call mcp__team__report_done with the summary; the task goes to %s.", st.Name, next)
}

// AssignError: agentul ales nu poate lua task-ul (mesajul ajunge in UI, 400).
type AssignError string

func (e AssignError) Error() string { return string(e) }

// ErrTaskBusy: task-ul e in lucru; nu il mutam de sub agentul care lucreaza.
var ErrTaskBusy = errors.New("the task is in progress: wait for it to finish or stop the team")

// checkAssignee verifica un agent ales de utilizator pentru un task: din
// proiect, legat, si (in etape) din etapa data.
func checkAssignee(agents []store.Agent, agentID int64, stage *store.Stage) (store.Agent, error) {
	a, ok := store.FindAgent(agents, agentID)
	if !ok {
		return a, AssignError("the chosen agent is not in this project")
	}
	if a.Detached {
		return a, AssignError(a.Name + " is detached: link it on the canvas so it can work")
	}
	if stage != nil && !slices.Contains(stage.AgentIDs, a.ID) {
		return a, AssignError(fmt.Sprintf("%s is not in stage %s", a.Name, stage.Name))
	}
	return a, nil
}

// UserCreateTask: task creat de utilizator din board; la final decide tot el
// (ByUser). Cu etape intra in prima etapa (agentID optional, din acea etapa);
// fara etape agentID e obligatoriu.
func (t *Team) UserCreateTask(projectID int64, title, description string, agentID int64) (store.Task, error) {
	t.work.Lock()
	defer t.work.Unlock()
	return t.userCreateTask(projectID, title, description, agentID, taskOrigin{})
}

// taskOrigin: de unde vine un task creat pe drumul utilizatorului.
// Gol = din board; recurring = o sarcina recurenta; request = o cerere de lucru
// dintr-un proiect legat (from = proiectul sursa, by = agentul care a cerut).
type taskOrigin struct {
	recurring *store.RecurringTask
	request   *store.ProjectRequest
	from, by  string
}

// userCreateTask e corpul lui UserCreateTask; se cheama cu t.work tinut.
func (t *Team) userCreateTask(projectID int64, title, description string, agentID int64, origin taskOrigin) (store.Task, error) {
	stages, err := t.st.ListStages(projectID)
	if err != nil {
		return store.Task{}, err
	}
	agents, err := t.st.ListAgents(projectID)
	if err != nil {
		return store.Task{}, err
	}
	if len(agents) == 0 {
		return store.Task{}, AssignError("the project has no agents")
	}
	task := store.Task{ProjectID: projectID, Title: title, Description: description, ByUser: true}
	created, actor := "Created by you", actorUser
	if origin.recurring != nil {
		task.RecurringID, task.AutoAccept = &origin.recurring.ID, origin.recurring.AutoAccept
		created = fmt.Sprintf("Created by recurring task %q", origin.recurring.Title)
	}
	if origin.request != nil {
		task.RequestID = &origin.request.ID
		created = fmt.Sprintf("Requested by %s (%s)", origin.from, origin.by)
		actor = origin.from
	}
	var assigned string // textul evenimentului "assigned" din istoric
	var to string       // destinatarul brief-ului ("" = coada etapei)
	if len(stages) > 0 {
		first := stages[0]
		task.StageID = &first.ID
		task.AssigneeID = placeholder(first, agents[0].ID)
		assigned = "Queued for stage " + first.Name
		if agentID != 0 {
			a, err := checkAssignee(agents, agentID, &first)
			if err != nil {
				return store.Task{}, err
			}
			task.AssigneeID = agentID
			assigned = "Assigned to " + a.Name
			to = a.Name
		}
	} else {
		if agentID == 0 {
			return store.Task{}, AssignError("choose the agent who does the task")
		}
		a, err := checkAssignee(agents, agentID, nil)
		if err != nil {
			return store.Task{}, err
		}
		task.AssigneeID = agentID
		assigned = "Assigned to " + a.Name
		to = a.Name
	}
	if err := t.st.CreateTask(&task); err != nil {
		return store.Task{}, err
	}
	t.taskMsg(task, "created", actor, to, created, briefBody(task.Title, task.Description))
	t.taskEvent(task, "assigned", actorUser, assigned)
	if a, ok := store.FindAgent(agents, task.AssigneeID); ok && task.StageID == nil {
		t.note(projectID, a.ID, fmt.Sprintf("%s assigned by the user: %s", task.Ref(), title), true)
	}
	t.emit(projectID, "task", task)
	t.Kick(projectID)
	return task, nil
}

// AssignTask: utilizatorul muta un task (in asteptare sau blocat) la alt agent.
// Un task blocat se si deblocheaza. Un task in lucru nu se muta (ErrTaskBusy).
func (t *Team) AssignTask(taskID, agentID int64) error {
	t.work.Lock()
	defer t.work.Unlock()
	task, err := t.st.GetTask(taskID)
	if err != nil {
		return err
	}
	if task.Status == store.StatusInProgress {
		return ErrTaskBusy
	}
	if task.Status != store.StatusQueued && task.Status != store.StatusBlocked {
		return AssignError(fmt.Sprintf("%s is %s; only queued or blocked tasks can be moved", task.Ref(), task.Status))
	}
	agents, err := t.st.ListAgents(task.ProjectID)
	if err != nil {
		return err
	}
	var stage *store.Stage
	if task.StageID != nil {
		stages, err := t.st.ListStages(task.ProjectID)
		if err != nil {
			return err
		}
		if i := stageIndex(stages, task.StageID); i >= 0 {
			stage = &stages[i]
		}
	}
	a, err := checkAssignee(agents, agentID, stage)
	if err != nil {
		return err
	}
	task.AssigneeID = a.ID
	if task.Status == store.StatusBlocked {
		// deblocat de utilizator: incercari noi, ca la Unblock
		task.Status, task.SendBacks, task.Escalated = store.StatusQueued, 0, false
	}
	if err := t.st.SaveTask(&task); err != nil {
		return err
	}
	t.taskEvent(task, "assigned", actorUser, "Assigned to "+a.Name)
	if boss, ok := decider(task, agents, t.autonomyOf(task.ProjectID)); ok {
		t.note(task.ProjectID, boss.ID, fmt.Sprintf("The user moved %s to %s.", task.Ref(), a.Name), true)
	}
	t.note(task.ProjectID, a.ID, fmt.Sprintf("%s assigned by the user: %s", task.Ref(), task.Title), true)
	t.emit(task.ProjectID, "task", task)
	t.Kick(task.ProjectID)
	return nil
}

var ErrNotUserDecision = errors.New("the task is not waiting for your decision")

// advance muta un task (cu Result pus) in etapa urmatoare, sau la lider dupa
// ultima etapa. ev (raportul sau aprobarea) intra in istoric dupa salvare,
// urmat de mutarea in etapa. Se apeleaza cu t.work blocat. Intoarce textul pentru apelant.
// In modul autonom, dupa ultima etapa decide radacina; daca nu exista (asignatul
// e radacina), task-ul trece direct in Done.
func (t *Team) advance(task *store.Task, stages []store.Stage, agents []store.Agent, byName string, ev taskNote, autonomous bool) (string, error) {
	task.Feedback, task.ReviewStage = "", store.StageNone
	if next, ok := nextStage(stages, task.StageID); ok {
		task.StageID = &next.ID
		task.Status = store.StatusQueued
		task.AssigneeID = placeholder(next, task.AssigneeID)
		if err := t.st.SaveTask(task); err != nil {
			return "", err
		}
		ev.to = "" // in coada etapei urmatoare: fara destinatar anume
		t.record(*task, ev)
		t.taskEvent(*task, "stage", byName, "Moved to stage "+next.Name)
		t.emit(task.ProjectID, "task", *task)
		t.Kick(task.ProjectID)
		return fmt.Sprintf("%s moved to stage %s.", task.Ref(), next.Name), nil
	}
	boss, ok := decider(*task, agents, autonomous)
	ev.to = whoName(boss, ok) // dupa ultima etapa: decidentul, altfel utilizatorul
	if !ok && (autonomous || task.AutoAccept) {
		markDone(task)
		if err := t.st.SaveTask(task); err != nil {
			return "", err
		}
		t.record(*task, ev)
		t.autoAccepted(*task, acceptText(*task))
		if task.AutoAccept {
			return fmt.Sprintf("%s went through all stages and was accepted automatically (recurring task).", task.Ref()), nil
		}
		return fmt.Sprintf("%s went through all stages and was accepted automatically (full autonomy).", task.Ref()), nil
	}
	task.StageID = nil
	task.Status, task.ReviewStage = store.StatusReview, store.StageLead
	if err := t.st.SaveTask(task); err != nil {
		return "", err
	}
	t.record(*task, ev)
	t.emit(task.ProjectID, "task", *task)
	if ok {
		t.note(task.ProjectID, boss.ID, clip(fmt.Sprintf("%s went through all stages (last: %s). Decide with accept_task or send_back: %s", task.Ref(), byName, task.Result)), false)
	}
	t.Kick(task.ProjectID)
	return fmt.Sprintf("%s went through all stages and is waiting for the lead's decision.", task.Ref()), nil
}

// sendToStage pune un task inapoi, Queued, in etapa st, cu feedback (numara
// trimiterea; la MaxSendBacks se blocheaza in etapa curenta). ev e respingerea
// pentru istoric; daca task-ul schimba etapa, urmeaza "Moved to stage". Se apeleaza cu t.work blocat.
func (t *Team) sendToStage(task *store.Task, st store.Stage, feedback string, agents []store.Agent, by int64, ev taskNote, autonomous bool) (bool, error) {
	moved := task.StageID == nil || *task.StageID != st.ID
	// destinatarul feedback-ului: asignatul daca task-ul ramane la el (blocat),
	// altfel nimeni anume (inapoi in coada etapei, il ia primul liber)
	ev.to = agentName(agents, task.AssigneeID)
	if task.SendBacks+1 < MaxSendBacks {
		ev.to = ""
		task.StageID = &st.ID
		task.AssigneeID = placeholder(st, task.AssigneeID)
	} else if task.StageID == nil {
		// se blocheaza dupa decizia finala: il tinem in etapa tinta, ca sa ramana
		// pe board (cu Accepta / Deblocheaza) si sa reia drumul prin etape
		task.StageID = &st.ID
	}
	blocked, err := t.rejectTask(task, feedback, agents, by, ev, autonomous)
	if err == nil && !blocked && moved {
		t.taskEvent(*task, "stage", ev.actor, "Moved to stage "+st.Name)
	}
	return blocked, err
}

// UserDecide: utilizatorul decide un task al lui (creat din board) ajuns la final.
func (t *Team) UserDecide(taskID int64, accept bool, feedback string) error {
	t.work.Lock()
	defer t.work.Unlock()
	task, err := t.st.GetTask(taskID)
	if err != nil {
		return err
	}
	agents, err := t.st.ListAgents(task.ProjectID)
	if err != nil {
		return err
	}
	// regula de azi (fara autonomie): utilizatorul poate decide din board si
	// task-urile pe care in modul autonom le decide radacina
	if task.Status != store.StatusReview || task.ReviewStage != store.StageLead || decisionOwner(task, agents) != 0 {
		return ErrNotUserDecision
	}
	if accept {
		task.Status, task.ReviewStage, task.StageID = store.StatusDone, store.StageNone, nil
		if err := t.st.SaveTask(&task); err != nil {
			return err
		}
		t.taskMsg(task, "user_accepted", actorUser, agentName(agents, task.AssigneeID), "You accepted", "Accepted")
		t.emit(task.ProjectID, "task", task)
		// task-ul e deja Done: livreaza raportul unei cereri dintr-un proiect legat
		if err := t.afterDone(task); err != nil {
			log.Printf("after accept %s: %v", task.Ref(), err)
		}
		t.Kick(task.ProjectID)
		return nil
	}
	feedback = strings.TrimSpace(feedback)
	if feedback == "" {
		return errors.New("write what needs to change")
	}
	stages, err := t.st.ListStages(task.ProjectID)
	if err != nil {
		return err
	}
	// destinatarul: asignatul (cu etape il schimba sendToStage)
	sent := taskNote{kind: "sent_back", actor: actorUser, text: "You sent it back: " + feedback,
		to: agentName(agents, task.AssigneeID), body: feedback}
	// decizia utilizatorului nu declanseaza niciodata acceptul automat: regulile de azi
	if len(stages) == 0 {
		// fara etape: inapoi la acelasi agent, cu feedback
		if _, err := t.rejectTask(&task, feedback, agents, 0, sent, false); err != nil {
			return err
		}
		t.Kick(task.ProjectID)
		return nil
	}
	target := stages[0]
	if i := stageIndex(stages, task.StageID); i >= 0 {
		target = stages[i]
	}
	if _, err := t.sendToStage(&task, target, feedback, agents, 0, sent, false); err != nil {
		return err
	}
	t.Kick(task.ProjectID)
	return nil
}

// stageCheckTarget valideaza un review_approve/review_reject pe un task dintr-o
// etapa de verificare: apelantul lucreaza chiar acum la el. Se apeleaza cu t.work blocat.
func (t *Team) stageCheckTarget(c mcp.Caller, task store.Task) ([]store.Stage, []store.Agent, error) {
	stages, err := t.st.ListStages(c.ProjectID)
	if err != nil {
		return nil, nil, err
	}
	i := stageIndex(stages, task.StageID)
	if i < 0 || stages[i].Kind != store.KindCheck {
		return nil, nil, fmt.Errorf("%s is not in a check stage", task.Ref())
	}
	if task.AssigneeID != c.AgentID || task.Status != store.StatusInProgress {
		return nil, nil, fmt.Errorf("%s is not with you for checking right now", task.Ref())
	}
	agents, err := t.st.ListAgents(c.ProjectID)
	return stages, agents, err
}

// stagesOverview descrie etapele pentru tura unui lider: ordinea, tipul si
// agentii fiecareia, plus regula de creare a task-urilor.
func stagesOverview(stages []store.Stage, agents []store.Agent) string {
	if len(stages) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("Board stages (every task goes through them in order):")
	for _, st := range stages {
		kind := "work"
		if st.Kind == store.KindCheck {
			kind = "check"
		}
		var names []string
		for _, id := range st.AgentIDs {
			if a, ok := store.FindAgent(agents, id); ok {
				names = append(names, a.Name)
			}
		}
		who := strings.Join(names, ", ")
		if who == "" {
			who = "no agents"
		}
		fmt.Fprintf(&b, "\n- %s (%s): %s", st.Name, kind, who)
	}
	fmt.Fprintf(&b, "\nNew tasks enter %s; assignee is optional (the first free agent takes it). After the last stage you decide.", stages[0].Name)
	return b.String()
}

// UserDecides: task-ul asteapta decizia utilizatorului (e la "La lider" si nu
// are un agent care sa-l decida: creat din board, sau creatorul a disparut).
// In modul autonom, task-urile pe care le decide radacina nu intra aici
// (utilizatorul le poate decide totusi din board, vezi UserDecide).
// autonomous e comutatorul proiectului (apelantul il are deja); cu false
// raspunde exact ca verificarea din UserDecide (ce poate decide din board).
func (t *Team) UserDecides(task store.Task, agents []store.Agent, autonomous bool) bool {
	if task.Status != store.StatusReview || task.ReviewStage != store.StageLead {
		return false
	}
	return decisionOwnerIn(task, agents, autonomous) == 0
}
