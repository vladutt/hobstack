package team

import (
	"errors"
	"fmt"

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
