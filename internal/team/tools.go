package team

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"foreman/internal/mcp"
	"foreman/internal/store"
)

// MaxSendBacks: dupa atatea trimiteri inapoi, task-ul se blocheaza si decide utilizatorul.
const MaxSendBacks = 3

var _ mcp.Backend = (*Team)(nil) // compilatorul verifica ca Team implementeaza interfata

func (t *Team) CreateTask(c mcp.Caller, title, description, assignee string) (string, error) {
	t.work.Lock()
	defer t.work.Unlock()

	if !c.Lead {
		return "", errors.New("doar lead-ul poate crea task-uri")
	}
	title = strings.TrimSpace(title)
	if title == "" {
		return "", errors.New("title e obligatoriu")
	}
	w, err := t.st.FindAgentByName(c.ProjectID, strings.TrimSpace(assignee))
	if errors.Is(err, store.ErrNotFound) || (err == nil && w.Position != store.PositionWorker) {
		return "", fmt.Errorf("%q nu e un worker din echipa. Workeri: %s", assignee, t.workerNames(c.ProjectID))
	}
	if err != nil {
		return "", err
	}
	lead, err := t.st.GetAgent(c.AgentID)
	if err != nil {
		return "", err
	}

	task := store.Task{ProjectID: c.ProjectID, Title: title, Description: strings.TrimSpace(description), AssigneeID: w.ID}
	if err := t.st.CreateTask(&task); err != nil {
		return "", err
	}
	t.note(c.ProjectID, lead.ID, fmt.Sprintf("A creat %s pentru %s: %s", task.Ref(), w.Name, title), true)
	t.note(c.ProjectID, w.ID, fmt.Sprintf("%s asignat de %s: %s", task.Ref(), lead.Name, title), true)
	t.emit(c.ProjectID, "task", task)
	t.Kick(c.ProjectID)
	return fmt.Sprintf("Creat %s pentru %s. Il va primi automat.", task.Ref(), w.Name), nil
}

func (t *Team) ListTasks(c mcp.Caller, status string) (string, error) {
	t.work.Lock()
	defer t.work.Unlock()

	status = strings.TrimSpace(status)
	switch status {
	case "", store.StatusQueued, store.StatusInProgress, store.StatusReview, store.StatusDone, store.StatusBlocked:
	default:
		return "", fmt.Errorf("status necunoscut %q", status)
	}
	tasks, err := t.st.ListTasks(c.ProjectID)
	if err != nil {
		return "", err
	}
	agents, err := t.st.ListAgents(c.ProjectID)
	if err != nil {
		return "", err
	}
	names := map[int64]string{}
	for _, a := range agents {
		names[a.ID] = a.Name
	}

	var b strings.Builder
	for _, task := range tasks {
		if status != "" && task.Status != status {
			continue
		}
		fmt.Fprintf(&b, "%s [%s] %s -> %s\n", task.Ref(), task.Status, task.Title, names[task.AssigneeID])
		if (task.Status == store.StatusReview || task.Status == store.StatusBlocked) && task.Result != "" {
			fmt.Fprintf(&b, "  Rezultat: %s\n", clip(task.Result))
		}
	}
	if b.Len() == 0 {
		return "Nu exista task-uri.", nil
	}
	return strings.TrimRight(b.String(), "\n"), nil
}

func (t *Team) AcceptTask(c mcp.Caller, ref, note string) (string, error) {
	t.work.Lock()
	defer t.work.Unlock()

	if !c.Lead {
		return "", errors.New("doar lead-ul poate accepta task-uri")
	}
	task, err := t.findTask(c.ProjectID, ref)
	if err != nil {
		return "", err
	}
	if task.Status != store.StatusReview && task.Status != store.StatusBlocked {
		return "", fmt.Errorf("%s e %s; se accepta doar task-uri din review sau blocked", task.Ref(), task.Status)
	}
	task.Status = store.StatusDone
	if err := t.st.SaveTask(&task); err != nil {
		return "", err
	}
	text := "A acceptat " + task.Ref()
	if n := strings.TrimSpace(note); n != "" {
		text += ": " + n
	}
	t.note(c.ProjectID, c.AgentID, text, true)
	t.emit(c.ProjectID, "task", task)
	return task.Ref() + " e Done.", nil
}

func (t *Team) SendBack(c mcp.Caller, ref, feedback string) (string, error) {
	t.work.Lock()
	defer t.work.Unlock()

	if !c.Lead {
		return "", errors.New("doar lead-ul poate trimite task-uri inapoi")
	}
	feedback = strings.TrimSpace(feedback)
	if feedback == "" {
		return "", errors.New("feedback e obligatoriu: spune concret ce trebuie schimbat")
	}
	task, err := t.findTask(c.ProjectID, ref)
	if err != nil {
		return "", err
	}
	if task.Status != store.StatusReview {
		return "", fmt.Errorf("%s e %s; se trimit inapoi doar task-uri din review", task.Ref(), task.Status)
	}
	worker, err := t.st.GetAgent(task.AssigneeID)
	if err != nil {
		return "", err
	}
	lead, err := t.st.GetAgent(c.AgentID)
	if err != nil {
		return "", err
	}

	task.SendBacks++
	task.Feedback = feedback
	if task.SendBacks >= MaxSendBacks {
		task.Status = store.StatusBlocked
		if err := t.st.SaveTask(&task); err != nil {
			return "", err
		}
		t.note(c.ProjectID, lead.ID, fmt.Sprintf("%s e blocat dupa %d trimiteri inapoi. Decide utilizatorul.", task.Ref(), MaxSendBacks), true)
		t.emit(c.ProjectID, "task", task)
		return fmt.Sprintf("%s a fost trimis inapoi de %d ori si e acum blocat. Utilizatorul decide ce se intampla cu el.", task.Ref(), MaxSendBacks), nil
	}

	task.Status = store.StatusQueued
	if err := t.st.SaveTask(&task); err != nil {
		return "", err
	}
	t.note(c.ProjectID, lead.ID, fmt.Sprintf("A trimis inapoi %s lui %s: %s", task.Ref(), worker.Name, feedback), true)
	t.note(c.ProjectID, worker.ID, fmt.Sprintf("%s trimis inapoi de %s: %s", task.Ref(), lead.Name, feedback), true)
	t.emit(c.ProjectID, "task", task)
	t.Kick(c.ProjectID)
	return fmt.Sprintf("%s trimis inapoi lui %s (%d/%d).", task.Ref(), worker.Name, task.SendBacks, MaxSendBacks), nil
}

func (t *Team) ReportDone(c mcp.Caller, summary string) (string, error) {
	t.work.Lock()
	defer t.work.Unlock()

	if c.Lead {
		return "", errors.New("report_done e doar pentru workeri")
	}
	if c.TaskID == 0 {
		return "", errors.New("nu lucrezi pe niciun task acum; raspunde direct in mesaj")
	}
	summary = strings.TrimSpace(summary)
	if summary == "" {
		return "", errors.New("summary e obligatoriu")
	}
	task, err := t.st.GetTask(c.TaskID)
	if err != nil {
		return "", err
	}
	if task.Status != store.StatusInProgress {
		return "", fmt.Errorf("%s nu mai e in lucru (status %s)", task.Ref(), task.Status)
	}
	worker, err := t.st.GetAgent(c.AgentID)
	if err != nil {
		return "", err
	}
	lead, err := t.st.Lead(c.ProjectID)
	if err != nil {
		return "", err
	}

	task.Status = store.StatusReview
	task.Result = summary
	if err := t.st.SaveTask(&task); err != nil {
		return "", err
	}
	t.note(c.ProjectID, worker.ID, fmt.Sprintf("A raportat %s", task.Ref()), true)
	t.note(c.ProjectID, lead.ID, clip(fmt.Sprintf("%s a terminat %s (review): %s", worker.Name, task.Ref(), summary)), false)
	t.emit(c.ProjectID, "task", task)
	t.Kick(c.ProjectID)
	return fmt.Sprintf("%s e in Review. %s va verifica.", task.Ref(), lead.Name), nil
}

// findTask accepta "T-3", "t-3" sau "3".
func (t *Team) findTask(projectID int64, ref string) (store.Task, error) {
	clean := strings.TrimSpace(ref)
	clean = strings.TrimPrefix(strings.TrimPrefix(clean, "T-"), "t-")
	n, err := strconv.Atoi(clean)
	if err != nil {
		return store.Task{}, fmt.Errorf("task invalid %q; foloseste formatul T-3", ref)
	}
	task, err := t.st.GetTaskByNumber(projectID, n)
	if errors.Is(err, store.ErrNotFound) {
		return store.Task{}, fmt.Errorf("nu exista task-ul T-%d", n)
	}
	return task, err
}

func (t *Team) workerNames(projectID int64) string {
	agents, _ := t.st.ListAgents(projectID)
	var names []string
	for _, a := range agents {
		if a.Position == store.PositionWorker {
			names = append(names, a.Name)
		}
	}
	if len(names) == 0 {
		return "(niciunul)"
	}
	return strings.Join(names, ", ")
}
