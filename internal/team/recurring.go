package team

import (
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"foreman/internal/schedule"
	"foreman/internal/store"
)

// RecurringError e o eroare pe care o vede utilizatorul (web: 400).
type RecurringError string

func (e RecurringError) Error() string { return string(e) }

// RecurringInput e ce se poate seta din UI; AssigneeID 0 = primul agent liber din prima etapa.
type RecurringInput struct {
	Title, Description string
	AssigneeID         int64
	Schedule           schedule.Spec
	AutoAccept         bool
	Enabled            bool
}

// checkRecurring valideaza programul si asignatul (ca la un task nou din board).
// Intoarce asignatul ca pointer (nil = prima etapa). Se cheama cu t.work tinut.
// checkWho=false (sarcina oprita): programul se valideaza mereu, dar asignatul
// doar trebuie sa fie din acest proiect, ca sa se poata opri o sarcina stricata.
func (t *Team) checkRecurring(projectID int64, in RecurringInput, checkWho bool) (*int64, error) {
	if err := in.Schedule.Validate(); err != nil {
		return nil, RecurringError(err.Error())
	}
	stages, err := t.st.ListStages(projectID)
	if err != nil {
		return nil, err
	}
	if in.AssigneeID == 0 {
		if len(stages) == 0 && checkWho {
			return nil, RecurringError("choose the agent who does the task")
		}
		return nil, nil
	}
	agents, err := t.st.ListAgents(projectID)
	if err != nil {
		return nil, err
	}
	if !checkWho {
		if _, ok := store.FindAgent(agents, in.AssigneeID); !ok {
			return nil, RecurringError("the chosen agent is not in this project")
		}
		id := in.AssigneeID
		return &id, nil
	}
	var first *store.Stage
	if len(stages) > 0 {
		first = &stages[0]
	}
	if _, err := checkAssignee(agents, in.AssigneeID, first); err != nil {
		return nil, RecurringError(err.Error())
	}
	id := in.AssigneeID
	return &id, nil
}

func (t *Team) CreateRecurring(projectID int64, in RecurringInput) (store.RecurringTask, error) {
	t.work.Lock()
	defer t.work.Unlock()
	who, err := t.checkRecurring(projectID, in, in.Enabled)
	if err != nil {
		return store.RecurringTask{}, err
	}
	r := store.RecurringTask{ProjectID: projectID, Title: in.Title, Description: in.Description, AssigneeID: who,
		Schedule: in.Schedule, AutoAccept: in.AutoAccept, Enabled: in.Enabled, NextRunAt: in.Schedule.Next(t.now())}
	if err := t.st.CreateRecurring(&r); err != nil {
		return store.RecurringTask{}, err
	}
	t.emit(projectID, "recurring", r)
	return r, nil
}

func (t *Team) UpdateRecurring(id int64, in RecurringInput) (store.RecurringTask, error) {
	t.work.Lock()
	defer t.work.Unlock()
	r, err := t.st.GetRecurring(id)
	if err != nil {
		return store.RecurringTask{}, err
	}
	who, err := t.checkRecurring(r.ProjectID, in, in.Enabled)
	if err != nil {
		return store.RecurringTask{}, err
	}
	// programul se recalculeaza doar cand s-a schimbat sau sarcina e repornita
	if in.Schedule != r.Schedule || (in.Enabled && !r.Enabled) {
		r.NextRunAt = in.Schedule.Next(t.now())
		r.LastNote = "" // nota veche nu mai e valabila dupa reprogramare
	}
	r.Title, r.Description, r.AssigneeID, r.Schedule, r.AutoAccept, r.Enabled = in.Title, in.Description, who, in.Schedule, in.AutoAccept, in.Enabled
	if err := t.st.SaveRecurring(&r); err != nil {
		return store.RecurringTask{}, err
	}
	t.emit(r.ProjectID, "recurring", r)
	return r, nil
}

func (t *Team) DeleteRecurring(id int64) error {
	t.work.Lock()
	defer t.work.Unlock()
	r, err := t.st.GetRecurring(id)
	if err != nil {
		return err
	}
	if err := t.st.DeleteRecurring(id); err != nil {
		return err
	}
	t.emit(r.ProjectID, "recurring", r)
	return nil
}

// RunDue creeaza task-urile sarcinilor al caror moment a venit. O eroare la o
// sarcina se logheaza si nu le opreste pe celelalte.
func (t *Team) RunDue(now time.Time) error {
	due, err := t.st.DueRecurring(now)
	if err != nil {
		return err
	}
	for _, r := range due {
		if err := t.runRecurring(r.ID, now, true); err != nil {
			log.Printf("recurring %d: %v", r.ID, err)
		}
	}
	return nil
}

// RunRecurringNow creeaza imediat un task (butonul Run now), fara sa mute programul.
func (t *Team) RunRecurringNow(id int64) (store.Task, error) {
	t.work.Lock()
	defer t.work.Unlock()
	r, err := t.st.GetRecurring(id)
	if err != nil {
		return store.Task{}, err
	}
	if open, ok := t.openOccurrence(r); ok {
		return store.Task{}, RecurringError(open.Ref() + " is still open")
	}
	if r.AssigneeID == nil {
		stages, err := t.st.ListStages(r.ProjectID)
		if err != nil {
			return store.Task{}, err
		}
		if len(stages) == 0 {
			return store.Task{}, RecurringError("choose an agent first: the recurring task has no assignee")
		}
	}
	task, err := t.userCreateTask(r.ProjectID, r.Title, r.Description, derefID(r.AssigneeID), &r)
	if err != nil {
		return store.Task{}, err
	}
	r.LastTaskID, r.LastNote = &task.ID, ""
	if err := t.st.SaveRecurring(&r); err != nil {
		return store.Task{}, err
	}
	t.emit(r.ProjectID, "recurring", r)
	return task, nil
}

// runRecurring face o rulare programata. Reciteste sarcina sub t.work: o editare
// facuta intre DueRecurring si aici castiga. Erorile de creare ajung in nota.
func (t *Team) runRecurring(id int64, now time.Time, scheduled bool) error {
	t.work.Lock()
	defer t.work.Unlock()
	r, err := t.st.GetRecurring(id)
	if errors.Is(err, store.ErrNotFound) {
		return nil // stearsa intre timp
	}
	if err != nil {
		return err
	}
	if scheduled && (!r.Enabled || r.NextRunAt.After(now)) {
		return nil // oprita sau reprogramata intre timp
	}
	hhmm := now.In(time.Local).Format("15:04")
	switch open, busy := t.openOccurrence(r); {
	case busy:
		r.LastNote = fmt.Sprintf("Skipped at %s: %s still open", hhmm, open.Ref())
	default:
		stages, err := t.st.ListStages(r.ProjectID)
		if err != nil {
			return err
		}
		if r.AssigneeID == nil && len(stages) == 0 {
			r.Enabled, r.LastNote = false, "Turned off: choose an agent (no assignee and the project has no stages)"
			break
		}
		task, err := t.userCreateTask(r.ProjectID, r.Title, r.Description, derefID(r.AssigneeID), &r)
		if err != nil {
			r.LastNote = "Could not create the task: " + strings.TrimSuffix(err.Error(), ".")
		} else {
			r.LastTaskID, r.LastNote = &task.ID, ""
		}
	}
	r.LastRunAt = &now
	r.NextRunAt = r.Schedule.Next(now)
	if err := t.st.SaveRecurring(&r); err != nil {
		return err
	}
	t.emit(r.ProjectID, "recurring", r)
	return nil
}

// openOccurrence: ultimul task creat de sarcina, daca exista si nu e Done.
func (t *Team) openOccurrence(r store.RecurringTask) (store.Task, bool) {
	if r.LastTaskID == nil {
		return store.Task{}, false
	}
	task, err := t.st.GetTask(*r.LastTaskID)
	if err != nil {
		return store.Task{}, false // sters: nu blocheaza
	}
	return task, task.Status != store.StatusDone
}

func derefID(p *int64) int64 {
	if p == nil {
		return 0
	}
	return *p
}
