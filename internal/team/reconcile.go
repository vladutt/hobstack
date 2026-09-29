package team

import (
	"context"
	"errors"
	"fmt"
	"log"
	"path/filepath"
	"strings"

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
	// full: s-a atins limita de workeri in paralel (0 = fara limita)
	full := func() bool { return p.MaxParallel > 0 && running >= p.MaxParallel }

	// 1. task-urile din Queued, in ordinea numarului
	hasQueued := map[int64]bool{}
	for i := range tasks {
		task := &tasks[i]
		if task.Status != store.StatusQueued {
			continue
		}
		hasQueued[task.AssigneeID] = true
		if full() || t.isActive(task.AssigneeID) {
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
		turn := prompt.TaskTurn(t.turnContext(w, agents, tasks, task.ID), *task, subtaskLines(task.ID, tasks, byID), t.texts(p.ID, msgs))
		if err := t.startRun(p, w, agents, task.ID, turn); err != nil {
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
		if full() {
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
		if err := t.startRun(p, w, agents, 0, prompt.NotifyTurn(t.turnContext(w, agents, tasks, 0), t.texts(p.ID, msgs))); err != nil {
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
	if err := t.startRun(p, lead, agents, 0, prompt.NotifyTurn(t.turnContext(lead, agents, tasks, 0), t.texts(p.ID, notes))); err != nil {
		return t.startFailed(p, lead, lead, err)
	}
	return t.delivered(p.ID, notes)
}

// turnContext aduna ce depinde de locul agentului in echipa: echipa lui
// directa, cine ii verifica munca si task-urile deschise (doar pentru lideri).
func (t *Team) turnContext(a store.Agent, agents []store.Agent, tasks []store.Task, currentTaskID int64) prompt.TurnContext {
	var c prompt.TurnContext
	for _, child := range store.Children(a.ID, agents) {
		c.Direct = append(c.Direct, prompt.Member{
			Name: child.Name, Role: child.Role, Responsibilities: child.Responsibilities,
			Status: t.statusOf(child, tasks, false),
		})
	}
	for _, o := range agents {
		if o.ReviewerID != nil && *o.ReviewerID == a.ID {
			c.Reviewees = append(c.Reviewees, o.Name)
		}
	}
	if len(c.Direct) == 0 {
		return c
	}
	for _, task := range tasks {
		if task.AssigneeID != a.ID || task.Status != store.StatusInProgress || task.ID == currentTaskID {
			continue
		}
		open := prompt.OpenTask{Ref: task.Ref(), Title: task.Title}
		for _, sub := range tasks {
			if sub.ParentTaskID != nil && *sub.ParentTaskID == task.ID {
				open.Total++
				if sub.Status == store.StatusDone {
					open.Done++
				}
			}
		}
		c.OpenTasks = append(c.OpenTasks, open)
	}
	return c
}

// subtaskLines descrie sub-task-urile unui task, cate un rand: "T-5 [done] Lexer -> Wes".
func subtaskLines(taskID int64, tasks []store.Task, byID map[int64]store.Agent) []string {
	var out []string
	for _, sub := range tasks {
		if sub.ParentTaskID != nil && *sub.ParentTaskID == taskID {
			out = append(out, fmt.Sprintf("%s [%s] %s -> %s", sub.Ref(), sub.Status, sub.Title, byID[sub.AssigneeID].Name))
		}
	}
	return out
}

// attachedLine e formatul unic al liniei de atasament din tura unui agent.
func (t *Team) attachedLine(dir string, a store.Attachment) string {
	return fmt.Sprintf("\nAtasat: %s (%s)", filepath.Join(dir, a.Stored), a.Name)
}

// texts extrage textele mesajelor; ce a scris utilizatorul e marcat mereu.
// Fiecare atasament apare ca linie separata cu calea fisierului.
func (t *Team) texts(projectID int64, msgs []store.Message) []string {
	dir := t.filesDir(projectID)
	out := make([]string, 0, len(msgs))
	for _, m := range msgs {
		text := m.Text
		if m.Author == store.AuthorUser {
			text = "Utilizatorul: " + text
		}
		for _, a := range m.Attachments {
			text += t.attachedLine(dir, a)
		}
		out = append(out, text)
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
	if err == nil {
		err = t.wakeLeadForDecisions(projectID)
	}
	t.work.Unlock()
	if err != nil {
		return err
	}
	t.emit(projectID, "project", map[string]int64{"id": projectID})
	t.Kick(projectID)
	return nil
}

// wakeLeadForDecisions: notificarile se marcheaza livrate cand porneste run-ul
// lead-ului. Daca run-ul a fost oprit (Stop/restart), dupa Resume nu mai e nimic
// nelivrat, iar task-urile din review/blocked ar ramane uitate. Aici adaugam o
// nota noua. Se apeleaza cu t.work blocat.
func (t *Team) wakeLeadForDecisions(projectID int64) error {
	lead, err := t.st.Lead(projectID)
	if err != nil {
		return err
	}
	if t.isActive(lead.ID) {
		return nil
	}
	pending, err := t.st.Undelivered(lead.ID)
	if err != nil || len(pending) > 0 {
		return err
	}
	tasks, err := t.st.ListTasks(projectID)
	if err != nil {
		return err
	}
	var refs []string
	for _, task := range tasks {
		if task.Status == store.StatusReview || task.Status == store.StatusBlocked {
			refs = append(refs, task.Ref())
		}
	}
	if len(refs) > 0 {
		t.note(projectID, lead.ID, "Echipa a fost repornita. Asteapta decizia ta: "+strings.Join(refs, ", ")+".", false)
	}
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
func (t *Team) UserMessage(agentID int64, text string, attachmentIDs []int64) (store.Message, error) {
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
	// atasamentele se leaga inainte de emit/Kick, ca UI-ul si tura sa le vada
	if err := t.st.LinkAttachments(attachmentIDs, m.ID); err != nil {
		return store.Message{}, err
	}
	for _, id := range attachmentIDs {
		if att, err := t.st.GetAttachment(id); err == nil && att.MessageID == m.ID {
			m.Attachments = append(m.Attachments, att)
		}
	}
	t.emit(a.ProjectID, "message", m)
	if a.Position == store.PositionWorker {
		lead, err := t.st.Lead(a.ProjectID)
		if err != nil {
			return store.Message{}, err
		}
		note := fmt.Sprintf("Utilizatorul -> %s: %s", a.Name, text)
		dir := t.filesDir(a.ProjectID)
		for _, att := range m.Attachments {
			note += t.attachedLine(dir, att)
		}
		t.note(a.ProjectID, lead.ID, clip(note), false)
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
