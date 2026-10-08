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

// ErrNoAssignee: asignatul task-ului nu mai exista (defensiv: baza nu lasa de obicei un agent cu task-uri sa dispara)
var ErrNoAssignee = errors.New("the task's agent no longer exists")

// ErrBadComment: comentariu gol sau prea lung (400 in API)
var ErrBadComment = errors.New("comment: 1-4000 characters")

// UserComment: comentariul utilizatorului pe task; ajunge la asignat ca mesaj si il trezeste
func (t *Team) UserComment(taskID int64, text string) error {
	text = strings.TrimSpace(text)
	if text == "" || len([]rune(text)) > 4000 {
		return ErrBadComment
	}
	t.work.Lock()
	task, err := t.st.GetTask(taskID)
	if err != nil {
		t.work.Unlock()
		return err
	}
	a, err := t.st.GetAgent(task.AssigneeID)
	if err != nil {
		t.work.Unlock()
		return ErrNoAssignee
	}
	t.taskMsg(task, "comment", actorUser, a.Name, "You commented", text)
	msg := fmt.Sprintf("Comment from the user on %s (%s): %s\nIf it fills in something you left for them, update the work, "+
		"tick it with mcp__team__tick_left_for_user and reply with mcp__team__comment_task.", task.Ref(), task.Title, text)
	if _, err := t.addUserMessage(a, clip(msg), nil, true); err != nil {
		t.work.Unlock()
		return err
	}
	t.work.Unlock()
	t.Kick(task.ProjectID)
	return nil
}

// UserSetTodo: utilizatorul bifeaza (sau debifeaza) un punct; statusul task-ului nu se schimba
func (t *Team) UserSetTodo(taskID int64, position int, done bool) error {
	t.work.Lock()
	defer t.work.Unlock()
	task, err := t.st.GetTask(taskID)
	if err != nil {
		return err
	}
	if err := t.st.SetTodo(taskID, position, done, actorUser); err != nil {
		return err
	}
	t.emit(task.ProjectID, "task", task)
	return nil
}
