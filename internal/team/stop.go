package team

import (
	"fmt"

	"hobstack/internal/store"
)

// StopError: Stop / Resume refuzat, cu motivul pentru utilizator.
type StopError string

func (e StopError) Error() string { return string(e) }

// StopAgent opreste run-ul activ al agentului (chat, notificari sau task), fara sa opreasca echipa.
// Pe un task, task-ul ramane in Queued, oprit, pana il reia utilizatorul (ResumeTask).
// Procesul se opreste asincron; finish vine dupa.
func (t *Team) StopAgent(agentID int64) error {
	a, err := t.st.GetAgent(agentID)
	if err != nil {
		return err
	}
	if !t.stopRun(func(id int64, _ activeRun) bool { return id == agentID }) {
		return StopError(a.Name + " is not working right now.")
	}
	return nil
}

// StopTask opreste munca pe task: run-ul activ pe el (al oricui) se opreste si task-ul asteapta
// in Queued pana la Resume. Un task din coada care inca n-a pornit e doar oprit.
func (t *Team) StopTask(taskID int64) error {
	if t.stopRun(func(_ int64, ar activeRun) bool { return ar.taskID == taskID }) {
		return nil
	}
	t.work.Lock()
	defer t.work.Unlock()
	task, err := t.st.GetTask(taskID)
	if err != nil {
		return err
	}
	if task.Status != store.StatusQueued {
		return StopError(task.Ref() + " is not being worked on right now.")
	}
	if task.Stopped {
		return nil
	}
	task.Stopped = true
	if err := t.st.SaveTask(&task); err != nil {
		return err
	}
	t.taskEvent(task, "stopped", actorUser, "You stopped it; it waits in Queued until you resume it")
	t.emit(task.ProjectID, "task", task)
	return nil
}

// StopCard opreste brainstorming-ul pe card: run-ul lui se opreste (finishCard il lasa Stopped),
// iar un card in asteptare trece direct pe Stopped. Nu se reia singur, doar cu Retry.
func (t *Team) StopCard(cardID int64) error {
	if t.stopRun(func(_ int64, ar activeRun) bool { return ar.cardID == cardID }) {
		return nil
	}
	return t.cardAction(cardID, func(card store.BrainstormCard) error {
		if card.Status != store.CardWaiting {
			return CardStateError("This card is not waiting or running.")
		}
		return t.st.SetCardState(card.ID, store.CardStopped, "", "")
	})
}

// stopRun opreste primul run activ ales de match si il marcheaza ca oprit de utilizator.
func (t *Team) stopRun(match func(agentID int64, ar activeRun) bool) bool {
	t.mu.Lock()
	runID := ""
	for id, ar := range t.active {
		if match(id, ar) {
			ar.userStop = true
			t.active[id] = ar
			runID = ar.runID
			break
		}
	}
	t.mu.Unlock()
	if runID == "" {
		return false
	}
	// run-ul s-a putut termina intre timp: finish l-a scos deja, nu e o eroare pentru utilizator
	_ = t.runs.Stop(runID)
	return true
}

// ResumeTask: un task oprit de utilizator intra iar in lucru (primul agent liber il ia din coada).
func (t *Team) ResumeTask(taskID int64) error {
	t.work.Lock()
	defer t.work.Unlock()
	task, err := t.st.GetTask(taskID)
	if err != nil {
		return err
	}
	if task.Status != store.StatusQueued || !task.Stopped {
		return StopError(task.Ref() + " is not stopped.")
	}
	return t.resumeLocked(task, "")
}

// resumeLocked e corpul lui ResumeTask (si al mutarii pe Queued); se apeleaza cu t.work blocat.
// note: nota optionala a utilizatorului, devine feedback-ul task-ului.
func (t *Team) resumeLocked(task store.Task, note string) error {
	task.Stopped = false
	if note != "" {
		task.Feedback = note
	}
	if err := t.st.SaveTask(&task); err != nil {
		return err
	}
	if note != "" {
		t.taskEvent(task, "resumed", actorUser, "You resumed it: "+note)
	} else {
		t.taskEvent(task, "resumed", actorUser, "You resumed it")
	}
	t.emit(task.ProjectID, "task", task)
	t.Kick(task.ProjectID)
	return nil
}

// stoppedText: nota pentru agent cand utilizatorul i-a oprit run-ul (doar informativa, fara un run nou).
func stoppedText(task *store.Task) string {
	if task == nil {
		return "Stopped by the user."
	}
	return fmt.Sprintf("The user stopped your work on %s; it waits until the user resumes it.", task.Ref())
}
