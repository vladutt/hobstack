package team

import (
	"context"
	"errors"
	"fmt"
	"log"
	"path/filepath"
	"slices"
	"strings"

	"foreman/internal/prompt"
	"foreman/internal/store"
)

var ErrNotBlocked = errors.New("the task is not blocked")

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
				log.Printf("project %d: %v", id, err)
			}
		}
	}
}

// Reconcile porneste run-urile care trebuie pornite in proiect:
//  0. agentii care au ceva de decis (review, accept, send_back) si mesaje
//     nelivrate primesc intai un run de notificari: o decizie deblocheaza
//     alti agenti, deci nu asteapta dupa toata coada;
//  1. orice agent inactiv cu un task in Queued primeste un run pe task,
//     in ordinea numarului;
//  2. altfel, un agent inactiv cu mesaje nelivrate primeste un run de notificari.
//
// max_parallel se aplica doar agentilor care au sef; radacinile vorbesc cu
// utilizatorul si pornesc mereu. E sigur de apelat oricand si de oricate ori.
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
	if err != nil || len(agents) == 0 {
		return err
	}
	tasks, err := t.st.ListTasks(projectID)
	if err != nil {
		return err
	}
	stages, err := t.st.ListStages(projectID)
	if err != nil {
		return err
	}
	byID := map[int64]store.Agent{}
	for _, a := range agents {
		byID[a.ID] = a
	}
	running := 0
	for _, a := range agents {
		if a.ParentID != nil && t.isActive(a.ID) {
			running++
		}
	}
	full := func(a store.Agent) bool {
		return a.ParentID != nil && p.MaxParallel > 0 && running >= p.MaxParallel
	}
	started := func(a store.Agent) {
		if a.ParentID != nil {
			running++
		}
	}
	notify := agents[0] // prima radacina: aici apar erorile de pornire
	// ctxFor: contextul turei; liderii si radacinile unui proiect cu etape vad si etapele
	ctxFor := func(a store.Agent, currentTaskID int64) prompt.TurnContext {
		c := t.turnContext(a, agents, tasks, currentTaskID, p.Folders)
		if r := store.RolesOf(a, agents); r.IsLead || r.IsRoot {
			c.Stages = stagesOverview(stages, agents)
		}
		return c
	}

	// failed devine true daca un run nu a pornit: proiectul e pe pauza si ne oprim
	failed := false
	notifyRun := func(a store.Agent) error {
		if t.isActive(a.ID) || full(a) {
			return nil
		}
		notes, err := t.st.Undelivered(a.ID)
		if err != nil || len(notes) == 0 {
			return err
		}
		turn := prompt.NotifyTurn(ctxFor(a, 0), t.texts(p.ID, notes))
		if err := t.startRun(p, a, agents, 0, turn); err != nil {
			failed = true
			return t.startFailed(p, notify, a, err)
		}
		if err := t.delivered(p.ID, notes); err != nil {
			return err
		}
		started(a)
		return nil
	}

	// 0. cine are de decis porneste primul
	decides := map[int64]bool{}
	for _, task := range tasks {
		if id := decisionOwner(task, agents); id != 0 {
			decides[id] = true
		}
	}
	for _, a := range agents {
		if !decides[a.ID] {
			continue
		}
		if err := notifyRun(a); err != nil || failed {
			return err
		}
	}

	// 1. task-urile din Queued
	hasQueued := map[int64]bool{}
	for i := range tasks {
		task := &tasks[i]
		if task.Status != store.StatusQueued {
			continue
		}
		var a store.Agent
		if task.StageID != nil {
			i := stageIndex(stages, task.StageID)
			if i < 0 {
				continue // etapa a disparut (nu se poate: stergerea e refuzata)
			}
			// primul agent liber din etapa; asignatul de acum are prioritate
			// (a fost ales la create_task sau il continua dupa o respingere)
			ids := append([]int64{task.AssigneeID}, stages[i].AgentIDs...)
			found := false
			for _, id := range ids {
				cand, ok := byID[id]
				// un agent nelegat nu lucreaza, chiar daca e bifat in etapa
				if ok && !cand.Detached && slices.Contains(stages[i].AgentIDs, id) && !t.isActive(id) && !full(cand) {
					a, found = cand, true
					break
				}
			}
			if !found {
				continue
			}
			task.AssigneeID = a.ID
		} else {
			hasQueued[task.AssigneeID] = true
			var ok bool
			a, ok = byID[task.AssigneeID]
			if !ok || t.isActive(a.ID) || full(a) {
				continue
			}
		}
		hasQueued[a.ID] = true
		msgs, err := t.st.Undelivered(a.ID)
		if err != nil {
			return err
		}
		task.Status = store.StatusInProgress
		if err := t.st.SaveTask(task); err != nil {
			return err
		}
		ctx := ctxFor(a, task.ID)
		ctx.Stage = stageLine(stages, *task)
		turn := prompt.TaskTurn(ctx, *task, subtaskLines(task.ID, tasks, byID), t.texts(p.ID, msgs))
		if err := t.startRun(p, a, agents, task.ID, turn); err != nil {
			task.Status = store.StatusQueued
			if err := t.st.SaveTask(task); err != nil {
				return err
			}
			return t.startFailed(p, notify, a, err)
		}
		if err := t.delivered(p.ID, msgs); err != nil {
			return err
		}
		t.emit(p.ID, "task", *task)
		started(a)
	}

	// 2. run-uri de notificari
	for _, a := range agents {
		if hasQueued[a.ID] {
			continue
		}
		if err := notifyRun(a); err != nil || failed {
			return err
		}
	}
	return nil
}

// turnContext aduna ce depinde de locul agentului in echipa: echipa lui
// directa, cine ii verifica munca si task-urile deschise (doar pentru lideri).
func (t *Team) turnContext(a store.Agent, agents []store.Agent, tasks []store.Task, currentTaskID int64, folders []store.Folder) prompt.TurnContext {
	var c prompt.TurnContext
	for _, child := range store.Children(a.ID, agents) {
		m := prompt.Member{
			Name: child.Name, Role: child.Role, Responsibilities: child.Responsibilities,
			Status: t.statusOf(child, tasks, false),
		}
		if len(folders) > 1 { // cu un singur folder nu adauga nimic
			m.Folder = store.HomeFolder(child, folders).Name
		}
		c.Direct = append(c.Direct, m)
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
	return fmt.Sprintf("\nAttached: %s (%s)", filepath.Join(dir, a.Stored), a.Name)
}

// texts extrage textele mesajelor; ce a scris utilizatorul e marcat mereu.
// Fiecare atasament apare ca linie separata cu calea fisierului.
func (t *Team) texts(projectID int64, msgs []store.Message) []string {
	dir := t.filesDir(projectID)
	out := make([]string, 0, len(msgs))
	for _, m := range msgs {
		text := m.Text
		if m.Author == store.AuthorUser {
			text = "User: " + text
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
func (t *Team) startFailed(p store.Project, notify, a store.Agent, cause error) error {
	if err := t.st.SetPaused(p.ID, true); err != nil {
		return err
	}
	t.note(p.ID, notify.ID, fmt.Sprintf("Cannot start %s: %v. The project is paused.", a.Name, cause), true)
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
			log.Printf("stopping run %s: %v", id, err)
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
		err = t.wakeForDecisions(projectID)
	}
	t.work.Unlock()
	if err != nil {
		return err
	}
	t.emit(projectID, "project", map[string]int64{"id": projectID})
	t.Kick(projectID)
	return nil
}

// wakeForDecisions: la Resume, notificarile livrate unui run oprit s-au
// pierdut. Fiecare agent care are ceva de decis primeste o nota noua:
// reviewer-ul pentru task-urile la review, decidentul pentru cele la lider
// sau blocate, sub-liderul pentru un task ale carui sub-task-uri sunt gata.
func (t *Team) wakeForDecisions(projectID int64) error {
	agents, err := t.st.ListAgents(projectID)
	if err != nil {
		return err
	}
	tasks, err := t.st.ListTasks(projectID)
	if err != nil {
		return err
	}
	waiting := map[int64][]string{}
	for _, task := range tasks {
		// reviewer-ul, sau decidentul daca reviewer-ul a disparut
		who := decisionOwner(task, agents)
		if task.Status == store.StatusInProgress && allSubtasksDone(task.ID, tasks) {
			who = task.AssigneeID
		}
		if who != 0 {
			waiting[who] = append(waiting[who], task.Ref())
		}
	}
	for _, a := range agents {
		refs := waiting[a.ID]
		if len(refs) == 0 || t.isActive(a.ID) {
			continue
		}
		pending, err := t.st.Undelivered(a.ID)
		if err != nil {
			return err
		}
		if len(pending) == 0 {
			t.note(projectID, a.ID, "The team was restarted. Waiting for your decision: "+strings.Join(refs, ", ")+".", false)
		}
	}
	return nil
}

// LinksChanged se apeleaza din web dupa ce un agent a fost editat sau sters.
// Pentru fiecare task care asteapta la reviewer:
//   - daca asignatul nu mai are reviewer, task-ul trece la decident (sau,
//     fara decident, in Blocked, si decide utilizatorul);
//   - altfel reviewer-ul curent (poate unul nou) e anuntat, daca nu are deja
//     mesaje in asteptare. Nu stim cui s-a trimis nota initiala, deci o nota
//     in plus e pretul acceptat pentru a nu lasa task-ul agatat.
func (t *Team) LinksChanged(projectID int64) error {
	t.work.Lock()
	err := t.relinkReviews(projectID)
	t.work.Unlock()
	if err != nil {
		return err
	}
	t.Kick(projectID)
	return nil
}

// relinkReviews face munca din LinksChanged. Se apeleaza cu t.work blocat.
func (t *Team) relinkReviews(projectID int64) error {
	agents, err := t.st.ListAgents(projectID)
	if err != nil {
		return err
	}
	tasks, err := t.st.ListTasks(projectID)
	if err != nil {
		return err
	}
	// reviewerii care au deja mesaje in asteptare nu mai primesc nota
	hasPending := map[int64]bool{}
	for _, a := range agents {
		if hasPending[a.ID], err = t.st.HasUndelivered(a.ID); err != nil {
			return err
		}
	}
	for i := range tasks {
		task := &tasks[i]
		if task.Status != store.StatusReview || task.ReviewStage != store.StageReviewer {
			continue
		}
		assignee, _ := store.FindAgent(agents, task.AssigneeID)
		if r, ok := reviewerOf(*task, agents); ok {
			if !hasPending[r.ID] {
				text := fmt.Sprintf("%s finished %s and is waiting for your review. Check it with review_approve or review_reject: %s", assignee.Name, task.Ref(), task.Result)
				if err := t.note(projectID, r.ID, clip(text), false); err != nil {
					return err
				}
			}
			continue
		}
		// reviewer-ul a disparut: decide liderul
		boss, hasBoss := decider(*task, agents)
		task.ReviewStage = store.StageLead
		if !hasBoss {
			task.Status, task.ReviewStage = store.StatusBlocked, store.StageNone
		}
		if err := t.st.SaveTask(task); err != nil {
			return err
		}
		t.emit(projectID, "task", *task)
		if !hasBoss {
			if err := t.note(projectID, assignee.ID, noDeciderText(*task, assignee), true); err != nil {
				return err
			}
			continue
		}
		text := fmt.Sprintf("%s finished %s; there is no reviewer anymore, so you decide (review): %s", assignee.Name, task.Ref(), task.Result)
		if err := t.note(projectID, boss.ID, clip(text), false); err != nil {
			return err
		}
	}
	return nil
}

// allSubtasksDone: true doar daca task-ul are cel putin un sub-task si toate sunt Done.
func allSubtasksDone(taskID int64, tasks []store.Task) bool {
	found := false
	for _, sub := range tasks {
		if sub.ParentTaskID != nil && *sub.ParentTaskID == taskID {
			if sub.Status != store.StatusDone {
				return false
			}
			found = true
		}
	}
	return found
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
// primeste la urmatorul run; pentru un agent cu sef, seful primeste o copie.
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
	if a.ParentID != nil {
		note := fmt.Sprintf("User -> %s: %s", a.Name, text)
		dir := t.filesDir(a.ProjectID)
		for _, att := range m.Attachments {
			note += t.attachedLine(dir, att)
		}
		t.note(a.ProjectID, *a.ParentID, clip(note), false)
	}
	t.Kick(a.ProjectID)
	return m, nil
}

func (t *Team) AcceptBlocked(taskID int64) error { return t.resolveBlocked(taskID, true) }
func (t *Team) Unblock(taskID int64) error       { return t.resolveBlocked(taskID, false) }

// resolveBlocked aplica decizia utilizatorului pe un task blocat si anunta decidentul lui.
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
	agents, err := t.st.ListAgents(task.ProjectID)
	if err != nil {
		return err
	}
	text := "The user accepted " + task.Ref() + "."
	if accept {
		task.Status = store.StatusDone
		task.ReviewStage = store.StageNone
		task.StageID = nil
	} else {
		task.Status = store.StatusQueued
		task.SendBacks = 0
		text = "The user unblocked " + task.Ref() + "; the assignee resumes it with the latest feedback."
	}
	if err := t.st.SaveTask(&task); err != nil {
		return err
	}
	if d, ok := decider(task, agents); ok {
		t.note(task.ProjectID, d.ID, text, false)
	}
	t.emit(task.ProjectID, "task", task)
	if accept {
		if err := t.afterDone(task); err != nil {
			log.Printf("after accept %s: %v", task.Ref(), err)
		}
	}
	t.Kick(task.ProjectID)
	return nil
}
