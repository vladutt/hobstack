package team

import (
	"errors"
	"fmt"
	"strings"

	"foreman/internal/mcp"
	"foreman/internal/store"
)

var ErrNoStages = errors.New("task-uri din board se pot crea doar in proiectele cu etape")

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
	next := "liderul, care decide"
	if n, ok := nextStage(stages, task.StageID); ok {
		next = n.Name
	}
	if st.Kind == store.KindCheck {
		return fmt.Sprintf("Etapa: %s (verificare). Verifica munca (citeste fisierele, nu doar rezumatul) si apeleaza "+
			"mcp__team__review_approve daca e buna (merge la %s) sau mcp__team__review_reject cu feedback concret.", st.Name, next)
	}
	return fmt.Sprintf("Etapa: %s (lucru). Cand termini, apeleaza mcp__team__report_done cu rezumatul; task-ul merge la %s.", st.Name, next)
}

// UserCreateTask: task creat de utilizator din board, in prima etapa. La
// final decide tot utilizatorul (ByUser).
func (t *Team) UserCreateTask(projectID int64, title, description string) (store.Task, error) {
	t.work.Lock()
	defer t.work.Unlock()
	stages, err := t.st.ListStages(projectID)
	if err != nil {
		return store.Task{}, err
	}
	if len(stages) == 0 {
		return store.Task{}, ErrNoStages
	}
	agents, err := t.st.ListAgents(projectID)
	if err != nil || len(agents) == 0 {
		return store.Task{}, fmt.Errorf("proiectul nu are agenti: %v", err)
	}
	first := stages[0]
	task := store.Task{ProjectID: projectID, Title: title, Description: description, ByUser: true,
		StageID: &first.ID, AssigneeID: placeholder(first, agents[0].ID)}
	if err := t.st.CreateTask(&task); err != nil {
		return store.Task{}, err
	}
	t.emit(projectID, "task", task)
	t.Kick(projectID)
	return task, nil
}

var ErrNotUserDecision = errors.New("task-ul nu asteapta decizia ta")

// advance muta un task (cu Result pus) in etapa urmatoare, sau la lider dupa
// ultima etapa. Se apeleaza cu t.work blocat. Intoarce textul pentru apelant.
func (t *Team) advance(task *store.Task, stages []store.Stage, agents []store.Agent, byName string) (string, error) {
	task.Feedback, task.ReviewStage = "", store.StageNone
	if next, ok := nextStage(stages, task.StageID); ok {
		task.StageID = &next.ID
		task.Status = store.StatusQueued
		task.AssigneeID = placeholder(next, task.AssigneeID)
		if err := t.st.SaveTask(task); err != nil {
			return "", err
		}
		t.emit(task.ProjectID, "task", *task)
		t.Kick(task.ProjectID)
		return fmt.Sprintf("%s a trecut in etapa %s.", task.Ref(), next.Name), nil
	}
	task.StageID = nil
	task.Status, task.ReviewStage = store.StatusReview, store.StageLead
	if err := t.st.SaveTask(task); err != nil {
		return "", err
	}
	t.emit(task.ProjectID, "task", *task)
	if boss, ok := decider(*task, agents); ok {
		t.note(task.ProjectID, boss.ID, clip(fmt.Sprintf("%s a trecut prin toate etapele (ultimul: %s). Decide cu accept_task sau send_back: %s", task.Ref(), byName, task.Result)), false)
	}
	t.Kick(task.ProjectID)
	return fmt.Sprintf("%s a trecut prin toate etapele si asteapta decizia liderului.", task.Ref()), nil
}

// sendToStage pune un task inapoi, Queued, in etapa st, cu feedback (numara
// trimiterea; la MaxSendBacks se blocheaza in etapa curenta). Se apeleaza cu t.work blocat.
func (t *Team) sendToStage(task *store.Task, st store.Stage, feedback string, agents []store.Agent, by int64) (bool, error) {
	if task.SendBacks+1 < MaxSendBacks {
		task.StageID = &st.ID
		task.AssigneeID = placeholder(st, task.AssigneeID)
	} else if task.StageID == nil {
		// se blocheaza dupa decizia finala: il tinem in etapa tinta, ca sa ramana
		// pe board (cu Accepta / Deblocheaza) si sa reia drumul prin etape
		task.StageID = &st.ID
	}
	return t.rejectTask(task, feedback, agents, by)
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
	if task.Status != store.StatusReview || task.ReviewStage != store.StageLead || decisionOwner(task, agents) != 0 {
		return ErrNotUserDecision
	}
	if accept {
		task.Status, task.ReviewStage, task.StageID = store.StatusDone, store.StageNone, nil
		if err := t.st.SaveTask(&task); err != nil {
			return err
		}
		t.emit(task.ProjectID, "task", task)
		t.Kick(task.ProjectID)
		return nil
	}
	feedback = strings.TrimSpace(feedback)
	if feedback == "" {
		return errors.New("scrie ce trebuie schimbat")
	}
	stages, err := t.st.ListStages(task.ProjectID)
	if err != nil || len(stages) == 0 {
		return ErrNoStages
	}
	target := stages[0]
	if i := stageIndex(stages, task.StageID); i >= 0 {
		target = stages[i]
	}
	if _, err := t.sendToStage(&task, target, feedback, agents, 0); err != nil {
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
		return nil, nil, fmt.Errorf("%s nu e intr-o etapa de verificare", task.Ref())
	}
	if task.AssigneeID != c.AgentID || task.Status != store.StatusInProgress {
		return nil, nil, fmt.Errorf("%s nu e la tine in verificare acum", task.Ref())
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
	b.WriteString("Etapele board-ului (fiecare task trece prin ele in ordine):")
	for _, st := range stages {
		kind := "lucru"
		if st.Kind == store.KindCheck {
			kind = "verificare"
		}
		var names []string
		for _, id := range st.AgentIDs {
			if a, ok := store.FindAgent(agents, id); ok {
				names = append(names, a.Name)
			}
		}
		who := strings.Join(names, ", ")
		if who == "" {
			who = "fara agenti"
		}
		fmt.Fprintf(&b, "\n- %s (%s): %s", st.Name, kind, who)
	}
	fmt.Fprintf(&b, "\nTask-urile noi intra in %s; assignee e optional (il ia primul agent liber). Dupa ultima etapa decizi tu.", stages[0].Name)
	return b.String()
}

// UserDecides: task-ul asteapta decizia utilizatorului (e la "La lider" si nu
// are un agent care sa-l decida: creat din board, sau creatorul a disparut).
func (t *Team) UserDecides(task store.Task, agents []store.Agent) bool {
	return task.Status == store.StatusReview && task.ReviewStage == store.StageLead && decisionOwner(task, agents) == 0
}
