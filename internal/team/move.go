package team

import (
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"

	"hobstack/internal/store"
)

// MoveError: mutare refuzata, cu motivul pentru utilizator.
type MoveError string

func (e MoveError) Error() string { return string(e) }

// ErrReasonRequired: mutarea cere un motiv (Send back, Reopen), pe care il citeste agentul.
var ErrReasonRequired = errors.New("write why (the agent reads it)")

// MoveTask aplica actiunea coloanei pe care utilizatorul a tras task-ul: "queued", "done" sau "stage:<id>".
func (t *Team) MoveTask(taskID int64, to, reason string) error {
	t.work.Lock()
	defer t.work.Unlock()
	task, err := t.st.GetTask(taskID)
	if err != nil {
		return err
	}
	if task.Status == store.StatusArchived {
		return MoveError(task.Ref() + " was deleted.")
	}
	if task.Status == store.StatusInProgress {
		return MoveError(fmt.Sprintf("An agent is working on %s; wait for it to finish.", task.Ref()))
	}
	agents, err := t.st.ListAgents(task.ProjectID)
	if err != nil {
		return err
	}
	reason = strings.TrimSpace(reason)
	switch {
	case to == "done":
		return t.moveToDone(task, agents, reason)
	case to == "queued":
		return t.moveToQueue(task, agents, reason)
	case strings.HasPrefix(to, "stage:"):
		id, err := strconv.ParseInt(strings.TrimPrefix(to, "stage:"), 10, 64)
		if err != nil {
			return MoveError("Unknown column.")
		}
		return t.moveToStage(task, agents, id, reason)
	}
	return MoveError("Tasks can't be moved there.")
}

func (t *Team) moveToDone(task store.Task, agents []store.Agent, reason string) error {
	switch task.Status {
	case store.StatusReview:
		return t.decideAsUser(task, agents, true, "", decisionOwner(task, agents), nil)
	case store.StatusBlocked:
		return t.resolveBlockedLocked(task, agents, true, "")
	case store.StatusQueued:
		return t.closeTask(task, agents, reason)
	}
	return MoveError(task.Ref() + " is already done.")
}

func (t *Team) moveToQueue(task store.Task, agents []store.Agent, reason string) error {
	switch task.Status {
	case store.StatusReview:
		if reason == "" {
			return ErrReasonRequired
		}
		return t.decideAsUser(task, agents, false, reason, decisionOwner(task, agents), nil)
	case store.StatusBlocked:
		return t.resolveBlockedLocked(task, agents, false, reason)
	case store.StatusDone:
		if reason == "" {
			return ErrReasonRequired
		}
		return t.reopenTask(task, agents, nil, reason)
	case store.StatusQueued:
		if task.StageID != nil {
			return t.leaveStages(task, agents, reason)
		}
	}
	return MoveError(task.Ref() + " is already queued.")
}

// leaveStages: un task care asteapta intr-o etapa, tras pe Queued, iese din etape si ramane
// la asignatul de acum, pe drumul standard (send back il intoarce la el, nu intr-o etapa).
func (t *Team) leaveStages(task store.Task, agents []store.Agent, reason string) error {
	task.StageID, task.ViaStages = nil, false
	task.SendBacks, task.Escalated = 0, false
	if reason != "" {
		task.Feedback = reason
	}
	if err := t.st.SaveTask(&task); err != nil {
		return err
	}
	name := agentName(agents, task.AssigneeID)
	t.taskEvent(task, "assigned", actorUser, "You moved it to Queued, for "+name)
	t.note(task.ProjectID, task.AssigneeID, fmt.Sprintf("%s assigned by the user: %s", task.Ref(), task.Title), true)
	t.emit(task.ProjectID, "task", task)
	t.Kick(task.ProjectID)
	return nil
}

// moveToStage: un task in asteptare / blocat trece in etapa (il ia primul liber de acolo), si
// unul de pe drumul standard; unul Done se redeschide in etapa aceea (cu motiv). Un task blocat
// tras pe etapa lui e deblocat, iar unul din Review e trimis inapoi in etapa pe care a fost tras (cu motiv).
func (t *Team) moveToStage(task store.Task, agents []store.Agent, stageID int64, reason string) error {
	stages, err := t.st.ListStages(task.ProjectID)
	if err != nil {
		return err
	}
	i := stageIndex(stages, &stageID)
	if i < 0 {
		return MoveError("That column no longer exists.")
	}
	st := stages[i]
	switch task.Status {
	case store.StatusDone:
		if reason == "" {
			return ErrReasonRequired
		}
		return t.reopenTask(task, agents, &st, reason)
	case store.StatusReview:
		if reason == "" {
			return ErrReasonRequired
		}
		return t.decideAsUser(task, agents, false, reason, decisionOwner(task, agents), &st)
	case store.StatusBlocked:
		if task.StageID != nil && *task.StageID == st.ID {
			return t.resolveBlockedLocked(task, agents, false, reason)
		}
	case store.StatusQueued:
	default:
		return MoveError(fmt.Sprintf("%s can't be moved there.", task.Ref()))
	}
	if task.StageID != nil && *task.StageID == st.ID {
		return MoveError(fmt.Sprintf("%s is already in %s.", task.Ref(), st.Name))
	}
	task.StageID = &st.ID
	task.AssigneeID = placeholder(st, task.AssigneeID)
	task.Status, task.SendBacks, task.Escalated = store.StatusQueued, 0, false
	if reason != "" {
		task.Feedback = reason
	}
	if err := t.st.SaveTask(&task); err != nil {
		return err
	}
	t.taskEvent(task, "stage", actorUser, "You moved it to "+st.Name)
	if d, ok := decider(task, agents, t.autonomyOf(task.ProjectID)); ok {
		t.note(task.ProjectID, d.ID, fmt.Sprintf("The user moved %s to stage %s.", task.Ref(), st.Name), true)
	}
	t.emit(task.ProjectID, "task", task)
	t.Kick(task.ProjectID)
	return nil
}

// closeTask: utilizatorul inchide un task din coada fara lucru.
func (t *Team) closeTask(task store.Task, agents []store.Agent, reason string) error {
	task.Status, task.ReviewStage, task.StageID = store.StatusDone, store.StageNone, nil
	task.Result = "Closed by the user."
	if reason != "" {
		task.Result += " " + reason
	}
	if err := t.st.SaveTask(&task); err != nil {
		return err
	}
	t.taskMsg(task, "closed", actorUser, agentName(agents, task.AssigneeID), "You closed it", task.Result)
	t.note(task.ProjectID, task.AssigneeID, fmt.Sprintf("The user closed %s; do not work on it.", task.Ref()), true)
	if d, ok := decider(task, agents, t.autonomyOf(task.ProjectID)); ok && d.ID != task.AssigneeID {
		t.note(task.ProjectID, d.ID, fmt.Sprintf("The user closed %s without work.", task.Ref()), false)
	}
	t.emit(task.ProjectID, "task", task)
	if err := t.afterDone(task); err != nil {
		log.Printf("after close %s: %v", task.Ref(), err)
	}
	t.Kick(task.ProjectID)
	return nil
}

// reopenTask: un task Done revine in coada, la acelasi asignat (sau in etapa st), cu motivul ca feedback.
func (t *Team) reopenTask(task store.Task, agents []store.Agent, st *store.Stage, reason string) error {
	task.Status, task.ReviewStage, task.SendBacks, task.Escalated = store.StatusQueued, store.StageNone, 0, false
	task.Feedback = reason
	if st != nil {
		task.StageID = &st.ID
		task.AssigneeID = placeholder(*st, task.AssigneeID)
	} else {
		task.StageID, task.ViaStages = nil, false // redeschis pe Queued: drumul standard, la asignat
	}
	if err := t.st.SaveTask(&task); err != nil {
		return err
	}
	t.taskMsg(task, "reopened", actorUser, agentName(agents, task.AssigneeID), "You reopened it: "+reason, reason)
	t.note(task.ProjectID, task.AssigneeID, fmt.Sprintf("The user reopened %s: %s", task.Ref(), reason), false)
	if d, ok := decider(task, agents, t.autonomyOf(task.ProjectID)); ok && d.ID != task.AssigneeID {
		t.note(task.ProjectID, d.ID, fmt.Sprintf("The user reopened %s: %s", task.Ref(), reason), false)
	}
	t.emit(task.ProjectID, "task", task)
	t.Kick(task.ProjectID)
	return nil
}
