package team

import (
	"fmt"

	"hobstack/internal/store"
)

// authFailed: run-ul a cazut la autentificare (token expirat sau revocat, CLI delogat).
// Fail closed: proiectul trece pe pauza (anti-bucla, ca la startFailed), eroarea ramane pe
// cont (cardul din "Needs you"), iar task-ul revine in coada: nu e vina agentului.
// Repornirea o face utilizatorul (Start team), dupa ce rezolva contul in Settings.
func (t *Team) authFailed(ar activeRun, a store.Agent, msg string) error {
	label := "The logged-in Claude CLI"
	if ar.accountID != 0 {
		if err := t.st.SetAccountCheck(ar.accountID, msg); err != nil {
			return err
		}
		if acct, err := t.st.Account(ar.accountID); err == nil {
			label = "Claude account " + acct.Name
		}
	}
	if err := t.st.SetPaused(ar.projectID, true); err != nil {
		return err
	}
	if ar.taskID != 0 {
		task, err := t.st.GetTask(ar.taskID)
		if err != nil {
			return err
		}
		if task.Status == store.StatusInProgress && task.AssigneeID == a.ID {
			task.Status = store.StatusQueued
			if err := t.st.SaveTask(&task); err != nil {
				return err
			}
			t.taskEvent(task, "requeued", actorHobStack, fmt.Sprintf("%s failed to sign in; back in the queue until you fix it", label))
			t.emit(ar.projectID, "task", task)
		}
	}
	t.emit(ar.projectID, "project", map[string]int64{"id": ar.projectID})
	return t.note(ar.projectID, a.ID, fmt.Sprintf("%s failed to sign in: %s. The project is paused; fix the account in Settings, then Start team.",
		label, clip(msg)), true)
}
