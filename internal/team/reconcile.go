package team

import (
	"context"
	"errors"
	"fmt"
	"log"

	"foreman/internal/prompt"
	"foreman/internal/store"
)

var ErrNotBlocked = errors.New("task-ul nu e blocat")

// Run proceseaza cererile Kick pana cand ctx e anulat. Porneste din main.
func (t *Team) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.wake:
		}
		t.mu.Lock()
		ids := make([]int64, 0, len(t.pending))
		for id := range t.pending {
			ids = append(ids, id)
		}
		t.pending = map[int64]bool{}
		t.mu.Unlock()

		for _, id := range ids {
			if err := t.Reconcile(id); err != nil {
				log.Printf("proiectul %d: %v", id, err)
			}
		}
	}
}

// Reconcile porneste run-urile care trebuie pornite in proiect, in ordinea:
// workerii pentru task-urile din Queued, workerii cu mesaje directe, apoi
// lead-ul daca are notificari. E sigur de apelat oricand si de oricate ori.
func (t *Team) Reconcile(projectID int64) error {
	t.work.Lock()
	defer t.work.Unlock()

	p, err := t.st.GetProject(projectID)
	if errors.Is(err, store.ErrNotFound) {
		return nil // proiectul a fost sters intre timp
	}
	if err != nil {
		return err
	}
	if p.Paused {
		return nil
	}
	agents, err := t.st.ListAgents(projectID)
	if err != nil {
		return err
	}
	if len(agents) == 0 || agents[0].Position != store.PositionLead {
		return fmt.Errorf("proiectul %d nu are lead", projectID)
	}
	lead, workers := agents[0], agents[1:]
	tasks, err := t.st.ListTasks(projectID)
	if err != nil {
		return err
	}
	byID := map[int64]store.Agent{}
	for _, a := range agents {
		byID[a.ID] = a
	}
	running := 0
	for _, w := range workers {
		if t.isActive(w.ID) {
			running++
		}
	}

	// 1. task-urile din Queued, in ordinea numarului
	hasQueued := map[int64]bool{}
	for i := range tasks {
		task := &tasks[i]
		if task.Status != store.StatusQueued {
			continue
		}
		hasQueued[task.AssigneeID] = true
		if running >= p.MaxParallel || t.isActive(task.AssigneeID) {
			continue
		}
		w := byID[task.AssigneeID]
		msgs, err := t.st.Undelivered(w.ID)
		if err != nil {
			return err
		}
		task.Status = store.StatusInProgress
		if err := t.st.SaveTask(task); err != nil {
			return err
		}
		turn := prompt.WorkerTurn(t.members(agents, tasks), *task, lead.Name, texts(msgs, false))
		if err := t.startRun(p, w, lead, task.ID, turn); err != nil {
			task.Status = store.StatusQueued
			if err := t.st.SaveTask(task); err != nil {
				return err
			}
			return t.startFailed(p, lead, w, err)
		}
		if err := t.delivered(p.ID, msgs); err != nil {
			return err
		}
		t.emit(p.ID, "task", *task)
		running++
	}

	// 2. workeri fara task, carora utilizatorul le-a scris direct
	for _, w := range workers {
		if running >= p.MaxParallel {
			break
		}
		if t.isActive(w.ID) || hasQueued[w.ID] {
			continue
		}
		msgs, err := t.st.Undelivered(w.ID)
		if err != nil {
			return err
		}
		if len(msgs) == 0 {
			continue
		}
		if err := t.startRun(p, w, lead, 0, prompt.DirectTurn(texts(msgs, false))); err != nil {
			return t.startFailed(p, lead, w, err)
		}
		if err := t.delivered(p.ID, msgs); err != nil {
			return err
		}
		running++
	}

	// 3. lead-ul, daca are notificari (mesaje de la utilizator, rezultate)
	if t.isActive(lead.ID) {
		return nil
	}
	notes, err := t.st.Undelivered(lead.ID)
	if err != nil || len(notes) == 0 {
		return err
	}
	if err := t.startRun(p, lead, lead, 0, prompt.LeadTurn(t.members(agents, tasks), texts(notes, true))); err != nil {
		return t.startFailed(p, lead, lead, err)
	}
	return t.delivered(p.ID, notes)
}

// members construieste lista echipei cu statusul curent al fiecaruia.
func (t *Team) members(agents []store.Agent, tasks []store.Task) []prompt.Member {
	out := make([]prompt.Member, 0, len(agents))
	for _, a := range agents {
		out = append(out, prompt.Member{Name: a.Name, Role: a.Role, Position: a.Position, Status: t.statusOf(a, tasks, false)})
	}
	return out
}

// texts extrage textele mesajelor; pentru lead marcam ce a scris utilizatorul.
func texts(msgs []store.Message, labelUser bool) []string {
	out := make([]string, 0, len(msgs))
	for _, m := range msgs {
		if labelUser && m.Author == store.AuthorUser {
			out = append(out, "Utilizatorul: "+m.Text)
		} else {
			out = append(out, m.Text)
		}
	}
	return out
}

// delivered marcheaza mesajele ca livrate si anunta UI-ul (dispare "in asteptare").
func (t *Team) delivered(projectID int64, msgs []store.Message) error {
	ids := make([]int64, len(msgs))
	for i, m := range msgs {
		ids[i] = m.ID
	}
	if err := t.st.MarkDelivered(ids); err != nil {
		return err
	}
	for _, m := range msgs {
		m.Delivered = true
		t.emit(projectID, "message", m)
	}
	return nil
}

// startFailed: un run nu a putut porni (ex. folderul de lucru a disparut).
// Punem proiectul pe pauza ca sa nu reincercam in bucla si explicam in thread.
func (t *Team) startFailed(p store.Project, lead, a store.Agent, cause error) error {
	if err := t.st.SetPaused(p.ID, true); err != nil {
		return err
	}
	t.note(p.ID, lead.ID, fmt.Sprintf("Nu pot porni %s: %v. Proiectul e pe pauza.", a.Name, cause), true)
	t.emit(p.ID, "project", map[string]int64{"id": p.ID})
	return nil
}

// Stop opreste echipa: pauza, procesele active sunt oprite, task-urile in lucru
// revin in Queued. Procesele se opresc asincron; finish vine dupa.
func (t *Team) Stop(projectID int64) error {
	t.work.Lock()
	defer t.work.Unlock()

	if err := t.st.SetPaused(projectID, true); err != nil {
		return err
	}
	t.mu.Lock()
	var ids []string
	for _, ar := range t.active {
		if ar.projectID == projectID {
			ids = append(ids, ar.runID)
		}
	}
	t.mu.Unlock()
	for _, id := range ids {
		if err := t.runs.Stop(id); err != nil {
			log.Printf("oprire run %s: %v", id, err)
		}
	}
	if err := t.st.RequeueInProgress(projectID); err != nil {
		return err
	}
	t.emit(projectID, "project", map[string]int64{"id": projectID})
	return nil
}

func (t *Team) Resume(projectID int64) error {
	t.work.Lock()
	err := t.st.SetPaused(projectID, false)
	t.work.Unlock()
	if err != nil {
		return err
	}
	t.emit(projectID, "project", map[string]int64{"id": projectID})
	t.Kick(projectID)
	return nil
}

// Recover aduce baza de date intr-o stare curata la pornirea serverului:
// run-urile ramase "running" (procesele au murit odata cu serverul) devin
// "interrupted", task-urile in lucru revin in Queued si totul e pe pauza,
// ca nimic sa nu porneasca (si sa coste) fara stirea utilizatorului.
func (t *Team) Recover() error {
	t.work.Lock()
	defer t.work.Unlock()
	if err := t.st.InterruptRunning(); err != nil {
		return err
	}
	if err := t.st.RequeueAllInProgress(); err != nil {
		return err
	}
	return t.st.PauseAll()
}

// UserMessage salveaza un mesaj de la utilizator catre un agent. Agentul il
// primeste la urmatorul run; pentru un worker, lead-ul primeste o copie.
func (t *Team) UserMessage(agentID int64, text string) (store.Message, error) {
	t.work.Lock()
	defer t.work.Unlock()

	a, err := t.st.GetAgent(agentID)
	if err != nil {
		return store.Message{}, err
	}
	m := store.Message{ProjectID: a.ProjectID, AgentID: a.ID, Kind: store.KindMsg, Author: store.AuthorUser, Text: text, Delivered: false}
	if err := t.st.AddMessage(&m); err != nil {
		return store.Message{}, err
	}
	t.emit(a.ProjectID, "message", m)
	if a.Position == store.PositionWorker {
		lead, err := t.st.Lead(a.ProjectID)
		if err != nil {
			return store.Message{}, err
		}
		t.note(a.ProjectID, lead.ID, clip(fmt.Sprintf("Utilizatorul -> %s: %s", a.Name, text)), false)
	}
	t.Kick(a.ProjectID)
	return m, nil
}

func (t *Team) AcceptBlocked(taskID int64) error { return t.resolveBlocked(taskID, true) }
func (t *Team) Unblock(taskID int64) error       { return t.resolveBlocked(taskID, false) }

// resolveBlocked aplica decizia utilizatorului pe un task blocat si il anunta pe lead.
func (t *Team) resolveBlocked(taskID int64, accept bool) error {
	t.work.Lock()
	defer t.work.Unlock()

	task, err := t.st.GetTask(taskID)
	if err != nil {
		return err
	}
	if task.Status != store.StatusBlocked {
		return ErrNotBlocked
	}
	lead, err := t.st.Lead(task.ProjectID)
	if err != nil {
		return err
	}
	text := "Utilizatorul a acceptat " + task.Ref() + "."
	if accept {
		task.Status = store.StatusDone
	} else {
		task.Status = store.StatusQueued
		task.SendBacks = 0
		text = "Utilizatorul a deblocat " + task.Ref() + "; worker-ul il reia cu ultimul feedback."
	}
	if err := t.st.SaveTask(&task); err != nil {
		return err
	}
	t.note(task.ProjectID, lead.ID, text, false)
	t.emit(task.ProjectID, "task", task)
	t.Kick(task.ProjectID)
	return nil
}
