package team

// Detectorul de task-uri blocate: un task care nu s-a mai schimbat de
// stuckAfter si pe care nu-l misca nimeni primeste o impingere automata (o data);
// daca tot sta, apare in "Needs you" (StuckTasks) cu Nudge again.

import (
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"foreman/internal/store"
)

// stuckAfter: cat sta un task neschimbat pana il consideram blocat; tot atat
// asteptam dupa impingere pana il aratam utilizatorului.
const stuckAfter = 5 * time.Minute

// Felurile de blocaj.
const (
	stuckDecision = "decision" // in review, decidentul (agent) e idle
	stuckOrphan   = "orphan"   // in progress, fara run si fara nimic de asteptat
)

var ErrNotStuck = errors.New("the task is not stuck: nothing to nudge")

// StuckTask e un rand din "Needs you": un task impins deja si tot blocat.
type StuckTask struct {
	TaskID    int64     `json:"taskId"`
	Ref       string    `json:"ref"`
	Title     string    `json:"title"`
	Kind      string    `json:"kind"` // "decision" sau "orphan"
	AgentID   int64     `json:"agentId"`
	AgentName string    `json:"agentName"`
	Since     time.Time `json:"since"` // updated_at: de cand nu s-a mai miscat
}

// Now intoarce ora curenta a echipei (time.Now; in teste poate fi alta).
func (t *Team) Now() time.Time { return t.now() }

// stuckKind spune daca task-ul e blocat (fara prag de timp): "decision" cu agentul
// care trebuie sa decida, sau "orphan" cu asignatul. Se cheama cu t.work tinut.
func (t *Team) stuckKind(task store.Task, agents []store.Agent, tasks []store.Task, open []store.Question) (string, store.Agent, bool) {
	switch task.Status {
	case store.StatusReview:
		id := decisionOwner(task, agents)
		if id == 0 {
			return "", store.Agent{}, false // decide utilizatorul: are butoane pe board
		}
		a, ok := store.FindAgent(agents, id)
		if !ok || t.isActive(a.ID) {
			return "", store.Agent{}, false
		}
		// o intrebare deschisa = agentul asteapta utilizatorul (e deja in "Needs you")
		for _, q := range open {
			if q.AgentID == a.ID {
				return "", store.Agent{}, false
			}
		}
		// mesajele din coada il vor trezi oricum
		if waiting, err := t.st.HasUndelivered(a.ID); err != nil || waiting {
			return "", store.Agent{}, false
		}
		return stuckDecision, a, true
	case store.StatusInProgress:
		a, ok := store.FindAgent(agents, task.AssigneeID)
		// un sub-lider cu sub-task-uri nefinalizate asteapta legitim
		if !ok || t.isActive(a.ID) || hasUnfinishedSubtasks(task.ID, tasks) {
			return "", store.Agent{}, false
		}
		for _, q := range open {
			if q.TaskID != nil && *q.TaskID == task.ID {
				return "", store.Agent{}, false // asteapta raspunsul utilizatorului
			}
		}
		return stuckOrphan, a, true
	}
	return "", store.Agent{}, false
}

// stuckInputs incarca ce trebuie pentru stuckKind. Se cheama cu t.work tinut.
func (t *Team) stuckInputs(projectID int64) ([]store.Agent, []store.Task, []store.Question, error) {
	agents, err := t.st.ListAgents(projectID)
	if err != nil {
		return nil, nil, nil, err
	}
	tasks, err := t.st.ListTasks(projectID)
	if err != nil {
		return nil, nil, nil, err
	}
	open, err := t.st.ListOpenQuestions(projectID)
	if err != nil {
		return nil, nil, nil, err
	}
	return agents, tasks, open, nil
}

// CheckStuck verifica toate proiectele care nu sunt pe pauza si impinge o data
// task-urile blocate de cel putin stuckAfter. Ruleaza la fiecare minut din Run.
func (t *Team) CheckStuck(now time.Time) error {
	t.work.Lock()
	touched, err := t.checkStuckLocked(now)
	t.work.Unlock()
	// Kick dupa ce am eliberat t.work: reconcilierea porneste run-urile
	for _, id := range touched {
		t.Kick(id)
	}
	return err
}

// checkStuckLocked face munca din CheckStuck si intoarce proiectele atinse.
// O eroare pe un proiect nu le sare pe celelalte: se logheaza si se strang.
func (t *Team) checkStuckLocked(now time.Time) ([]int64, error) {
	projects, err := t.st.ListProjects()
	if err != nil {
		return nil, err
	}
	var touched []int64
	var errs []error
	for _, p := range projects {
		if p.Paused {
			continue // pauza e alegerea utilizatorului
		}
		nudged, err := t.checkProjectStuck(p.ID, now)
		if err != nil {
			log.Printf("project %d: stuck check: %v", p.ID, err)
			errs = append(errs, err)
		}
		if nudged {
			touched = append(touched, p.ID)
		}
	}
	return touched, errors.Join(errs...)
}

// checkProjectStuck impinge task-urile blocate ale unui proiect; spune daca a impins ceva.
func (t *Team) checkProjectStuck(projectID int64, now time.Time) (bool, error) {
	agents, tasks, open, err := t.stuckInputs(projectID)
	if err != nil {
		return false, err
	}
	// deciziile se grupeaza pe agent: o singura nota pentru toate task-urile lui
	var order []store.Agent
	decisions := map[int64][]store.Task{}
	nudged := false
	for _, task := range tasks {
		if task.NudgedAt != nil {
			// impins deja: in minutul (un tick) in care trece pragul de
			// vizibilitate trimitem un eveniment, ca pagina deschisa sa se
			// reincarce si sa-l arate in "Needs you"
			since := now.Sub(*task.NudgedAt)
			if since >= stuckAfter && since < stuckAfter+time.Minute {
				if _, _, ok := t.stuckKind(task, agents, tasks, open); ok {
					t.emit(projectID, "task", task)
				}
			}
			continue
		}
		if now.Sub(task.UpdatedAt) < stuckAfter {
			continue
		}
		kind, a, ok := t.stuckKind(task, agents, tasks, open)
		if !ok {
			continue
		}
		if kind == stuckOrphan {
			if err := t.nudgeLocked(projectID, kind, a, []store.Task{task}, agents, now); err != nil {
				return nudged, err
			}
			nudged = true
			continue
		}
		if _, seen := decisions[a.ID]; !seen {
			order = append(order, a)
		}
		decisions[a.ID] = append(decisions[a.ID], task)
	}
	for _, a := range order {
		if err := t.nudgeLocked(projectID, stuckDecision, a, decisions[a.ID], agents, now); err != nil {
			return nudged, err
		}
		nudged = true
	}
	return nudged, nil
}

// nudgeLocked impinge agentul pentru task-urile date (toate de acelasi fel) si
// noteaza nudged_at = now. Folosit de CheckStuck si de Nudge. Se cheama cu
// t.work tinut; apelantul face Kick dupa ce il elibereaza.
func (t *Team) nudgeLocked(projectID int64, kind string, a store.Agent, tasks []store.Task, agents []store.Agent, now time.Time) error {
	switch kind {
	case stuckDecision:
		if err := t.note(projectID, a.ID, nudgeText(a, tasks, agents), false); err != nil {
			return err
		}
		refs, verb := refsOf(tasks)
		if err := t.note(projectID, a.ID, fmt.Sprintf("Foreman nudged %s: %s %s waiting for a decision.", a.Name, refs, verb), true); err != nil {
			return err
		}
		for _, task := range tasks {
			if err := t.st.SetTaskNudged(task.ID, now); err != nil {
				return err
			}
			task.NudgedAt = &now
			t.emit(projectID, "task", task)
		}
	case stuckOrphan:
		for _, task := range tasks {
			// intai SaveTask (curata nudged_at), apoi SetTaskNudged: altfel
			// detectorul l-ar vedea neimpins si l-ar impinge din nou
			task.Status = store.StatusQueued
			if err := t.st.SaveTask(&task); err != nil {
				return err
			}
			if err := t.st.SetTaskNudged(task.ID, now); err != nil {
				return err
			}
			task.NudgedAt = &now
			t.emit(projectID, "task", task)
			text := fmt.Sprintf("Foreman requeued %s: it was in progress with nothing running.", task.Ref())
			if err := t.note(projectID, a.ID, text, true); err != nil {
				return err
			}
		}
	}
	return nil
}

// refsOf intoarce "T-2, T-3" si verbul potrivit ("is"/"are").
func refsOf(tasks []store.Task) (string, string) {
	refs := make([]string, len(tasks))
	for i, task := range tasks {
		refs[i] = task.Ref()
	}
	if len(refs) == 1 {
		return refs[0], "is"
	}
	return strings.Join(refs, ", "), "are"
}

// nudgeText e nota care trezeste agentul. Task-urile la care e reviewer cer
// review_approve/review_reject; celelalte accept_task/send_back.
func nudgeText(a store.Agent, tasks []store.Task, agents []store.Agent) string {
	var review, decide []store.Task
	for _, task := range tasks {
		if r, ok := reviewerOf(task, agents); ok && task.ReviewStage == store.StageReviewer && r.ID == a.ID {
			review = append(review, task)
		} else {
			decide = append(decide, task)
		}
	}
	var parts []string
	if len(review) > 0 {
		refs, verb := refsOf(review)
		parts = append(parts, fmt.Sprintf("%s %s waiting for your review: call review_approve or review_reject.", refs, verb))
	}
	if len(decide) > 0 {
		refs, verb := refsOf(decide)
		parts = append(parts, fmt.Sprintf("%s %s waiting for your decision: call accept_task or send_back.", refs, verb))
	}
	parts = append(parts, "If you cannot decide without the user, call ask_user.")
	return strings.Join(parts, " ")
}

// StuckTasks intoarce task-urile vizibil blocate ale proiectului: impinse deja
// de cel putin stuckAfter si tot blocate. Mereu non-nil.
func (t *Team) StuckTasks(projectID int64, now time.Time) ([]StuckTask, error) {
	t.work.Lock()
	defer t.work.Unlock()
	out := []StuckTask{}
	p, err := t.st.GetProject(projectID)
	if err != nil {
		return out, err
	}
	if p.Paused {
		return out, nil
	}
	agents, tasks, open, err := t.stuckInputs(projectID)
	if err != nil {
		return out, err
	}
	for _, task := range tasks {
		if task.NudgedAt == nil || now.Sub(*task.NudgedAt) < stuckAfter {
			continue
		}
		kind, a, ok := t.stuckKind(task, agents, tasks, open)
		if !ok {
			continue
		}
		out = append(out, StuckTask{
			TaskID: task.ID, Ref: task.Ref(), Title: task.Title, Kind: kind,
			AgentID: a.ID, AgentName: a.Name, Since: task.UpdatedAt,
		})
	}
	return out, nil
}

// Nudge e impingerea manuala (Nudge again din "Needs you"): aceeasi impingere
// ca cea automata, chiar daca task-ul nu e inca vizibil blocat. ErrNotStuck
// daca task-ul nu e blocat dupa reguli (fara prag de timp) sau proiectul e pe pauza.
func (t *Team) Nudge(taskID int64, now time.Time) error {
	t.work.Lock()
	projectID, err := t.nudgeOne(taskID, now)
	t.work.Unlock()
	if err != nil {
		return err
	}
	t.Kick(projectID)
	return nil
}

// nudgeOne face munca din Nudge si intoarce proiectul task-ului.
func (t *Team) nudgeOne(taskID int64, now time.Time) (int64, error) {
	task, err := t.st.GetTask(taskID)
	if err != nil {
		return 0, err
	}
	p, err := t.st.GetProject(task.ProjectID)
	if err != nil {
		return 0, err
	}
	if p.Paused {
		return 0, ErrNotStuck
	}
	agents, tasks, open, err := t.stuckInputs(p.ID)
	if err != nil {
		return 0, err
	}
	kind, a, ok := t.stuckKind(task, agents, tasks, open)
	if !ok {
		return 0, ErrNotStuck
	}
	return p.ID, t.nudgeLocked(p.ID, kind, a, []store.Task{task}, agents, now)
}
