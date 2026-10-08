package team

import (
	"errors"
	"fmt"
	"strings"

	"foreman/internal/mcp"
	"foreman/internal/store"
)

// todoTask: task-ul din argument sau cel curent; il poate atinge doar asignatul lui sau cel care a raportat
// pe el (report_done poate muta task-ul la alt asignat, de ex. in etapa urmatoare).
func (t *Team) todoTask(c mcp.Caller, ref string) (store.Task, store.Agent, error) {
	var task store.Task
	var err error
	if strings.TrimSpace(ref) == "" {
		if c.TaskID == 0 {
			return task, store.Agent{}, errors.New(`say which task: task: "T-3"`)
		}
		task, err = t.st.GetTask(c.TaskID)
	} else {
		task, err = t.findTask(c.ProjectID, ref)
	}
	if err != nil {
		return task, store.Agent{}, err
	}
	a, err := t.st.GetAgent(c.AgentID)
	if err != nil {
		return task, a, err
	}
	if task.AssigneeID != c.AgentID && !t.reportedBy(task, a) {
		return task, a, fmt.Errorf("%s is not assigned to you", task.Ref())
	}
	return task, a, nil
}

// reportedBy: agentul a raportat pe task (evenimentul "reported" ii poarta numele)
func (t *Team) reportedBy(task store.Task, a store.Agent) bool {
	events, err := t.st.ListTaskEvents(task.ID)
	if err != nil {
		return false
	}
	for _, e := range events {
		if e.Kind == "reported" && e.Actor == a.Name {
			return true
		}
	}
	return false
}

func (t *Team) LeaveForUser(c mcp.Caller, ref string, items []string) (string, error) {
	t.work.Lock()
	defer t.work.Unlock()
	task, a, err := t.todoTask(c, ref)
	if err != nil {
		return "", err
	}
	n, err := t.st.AddTodos(task.ID, items)
	if err != nil {
		return "", err
	}
	if n > 0 {
		t.taskMsg(task, "left_for_user", a.Name, actorUser, fmt.Sprintf("%s left %d %s for you", a.Name, n, plural(n, "item", "items")),
			"- "+strings.Join(items, "\n- "))
		t.emit(c.ProjectID, "task", task)
	}
	return fmt.Sprintf("%d %s left for the user; they answer in the task's comments. When you fill one in, call tick_left_for_user.", n, plural(n, "item", "items")), nil
}

func (t *Team) TickLeftForUser(c mcp.Caller, ref string, items []int) (string, error) {
	t.work.Lock()
	defer t.work.Unlock()
	task, a, err := t.todoTask(c, ref)
	if err != nil {
		return "", err
	}
	if len(items) == 0 {
		return "", errors.New("items: the numbers to tick, e.g. [1, 3]")
	}
	list, err := t.st.ListTodos(task.ID)
	if err != nil {
		return "", err
	}
	for _, n := range items { // intai verificam tot, apoi bifam: o eroare nu lasa jumatate bifat
		if n < 1 || n > len(list) {
			return "", fmt.Errorf("%s has items 1-%d; there is no item %d", task.Ref(), len(list), n)
		}
	}
	for _, n := range items {
		if err := t.st.SetTodo(task.ID, n, true, a.Name); err != nil {
			return "", err
		}
	}
	t.emit(c.ProjectID, "task", task)
	open := 0
	if list, err = t.st.ListTodos(task.ID); err == nil {
		for _, x := range list {
			if x.DoneAt == nil {
				open++
			}
		}
	}
	return fmt.Sprintf("Ticked. %d still open on %s.", open, task.Ref()), nil
}

func (t *Team) CommentTask(c mcp.Caller, ref, text string) (string, error) {
	text = strings.TrimSpace(text)
	if text == "" || len([]rune(text)) > 4000 {
		return "", errors.New("text: 1-4000 characters")
	}
	t.work.Lock()
	defer t.work.Unlock()
	task, a, err := t.todoTask(c, ref)
	if err != nil {
		return "", err
	}
	t.taskMsg(task, "comment", a.Name, actorUser, a.Name+" commented", text)
	t.emit(c.ProjectID, "task", task)
	return "Comment added to " + task.Ref() + ".", nil
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
