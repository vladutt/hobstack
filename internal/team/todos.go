package team

import (
	"errors"
	"fmt"
	"strings"

	"hobstack/internal/mcp"
	"hobstack/internal/store"
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
	if task.Status == store.StatusArchived {
		return task, store.Agent{}, fmt.Errorf("%s was deleted by the user", task.Ref())
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

// TodosBy: agentul care a lasat punctele (actorul ultimului eveniment left_for_user), daca mai exista in proiect;
// altfel asignatul. In proiectele cu etape asignatul se muta la etapa urmatoare, dar punctele raman ale celui care le-a lasat.
func (t *Team) TodosBy(task store.Task) (store.Agent, error) {
	events, err := t.st.ListTaskEvents(task.ID)
	if err == nil {
		for i := len(events) - 1; i >= 0; i-- {
			if events[i].Kind != "left_for_user" {
				continue
			}
			if a, err := t.st.FindAgentByName(task.ProjectID, events[i].Actor); err == nil {
				return a, nil
			}
			break
		}
	}
	return t.st.GetAgent(task.AssigneeID)
}

// openItems: "Open items on T-5: 1. Company name; 3. VAT" (gol daca nu mai e nimic deschis)
func (t *Team) openItems(task store.Task) string {
	list, err := t.st.ListTodos(task.ID)
	if err != nil {
		return ""
	}
	var parts []string
	for _, x := range list {
		if x.DoneAt == nil {
			parts = append(parts, fmt.Sprintf("%d. %s", x.Position, x.Text))
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return fmt.Sprintf("Open items on %s: %s", task.Ref(), strings.Join(parts, "; "))
}

func (t *Team) LeaveForUser(c mcp.Caller, ref string, items []string) (string, error) {
	return t.leave(c, ref, items, "value")
}

// LeaveSteps: pasii pe care ii poate face doar utilizatorul (user_steps); aceeasi lista ca valorile, cu kind "step".
func (t *Team) LeaveSteps(c mcp.Caller, ref string, steps []string) (string, error) {
	return t.leave(c, ref, steps, "step")
}

func (t *Team) leave(c mcp.Caller, ref string, items []string, kind string) (string, error) {
	t.work.Lock()
	defer t.work.Unlock()
	task, a, err := t.todoTask(c, ref)
	if err != nil {
		return "", err
	}
	n, err := t.st.AddTodos(task.ID, items, kind)
	if err != nil {
		return "", err
	}
	if n > 0 {
		noun, nouns := "item", "items"
		if kind == "step" {
			noun, nouns = "step", "steps"
		}
		t.taskMsg(task, "left_for_user", a.Name, actorUser, fmt.Sprintf("%s left %d %s for you", a.Name, n, plural(n, noun, nouns)),
			"- "+strings.Join(items, "\n- "))
		t.emit(c.ProjectID, "task", task)
	}
	head := fmt.Sprintf("%d %s left for the user; they answer in the task's comments.", n, plural(n, "item", "items"))
	if kind == "step" {
		head = fmt.Sprintf("%d %s left for the user.", n, plural(n, "step", "steps"))
	}
	if n == 0 {
		head = "No new items; they were already listed."
	}
	out := head + " When you fill one in, call tick_left_for_user with its number."
	if kind == "step" {
		// pasii ii face utilizatorul si ii bifeaza tot el
		out = head + " Only the user can do them; the user does them and ticks them off."
	}
	if open := t.openItems(task); open != "" {
		out += " " + open + "."
	}
	return out, nil
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
	var texts []string
	for _, n := range items {
		if err := t.st.SetTodo(task.ID, n, true, a.Name); err != nil {
			return "", err
		}
		texts = append(texts, list[n-1].Text)
	}
	t.taskMsg(task, "todo_done", a.Name, actorUser, fmt.Sprintf("%s ticked %d %s", a.Name, len(texts), plural(len(texts), "item", "items")),
		"- "+strings.Join(texts, "\n- "))
	t.emit(c.ProjectID, "task", task)
	open := 0
	if list, err = t.st.ListTodos(task.ID); err == nil {
		for _, x := range list {
			if x.DoneAt == nil {
				open++
			}
		}
	}
	out := fmt.Sprintf("Ticked. %d still open on %s.", open, task.Ref())
	if o := t.openItems(task); o != "" {
		out += " " + o + "."
	}
	return out, nil
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
	if task.Status == store.StatusArchived {
		t.work.Unlock()
		return archivedError(task.Ref())
	}
	// primeste comentariul cel care a lasat punctele (nu neaparat asignatul curent)
	a, err := t.TodosBy(task)
	if err != nil {
		t.work.Unlock()
		return ErrNoAssignee
	}
	t.taskMsg(task, "comment", actorUser, a.Name, "You commented", text)
	// run-ul care citeste comentariul e doar de citire: modificarile merg intr-un task de continuare
	msg := fmt.Sprintf("Comment from the user on %s (%s): %s\nIf it fills in something you left for them, update the work in a "+
		"follow-up task (mcp__team__start_task with continues: %q), tick the item with mcp__team__tick_left_for_user "+
		"(item numbers below) and reply with mcp__team__comment_task.", task.Ref(), task.Title, text, task.Ref())
	if o := t.openItems(task); o != "" {
		msg += "\n" + o + "."
	}
	// seful nu primeste nota: nu poate bifa, iar un run in plus ar fi degeaba
	if _, err := t.addUserMessage(a, clip(msg), nil, true, true); err != nil {
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
	if task.Status == store.StatusArchived {
		return archivedError(task.Ref())
	}
	list, err := t.st.ListTodos(taskID)
	if err != nil {
		return err
	}
	if err := t.st.SetTodo(taskID, position, done, actorUser); err != nil {
		return err
	}
	for _, x := range list {
		if x.Position == position {
			if done {
				t.taskMsg(task, "todo_done", actorUser, "", "You ticked: "+x.Text, "")
			} else {
				t.taskMsg(task, "todo_reopened", actorUser, "", "You reopened: "+x.Text, "")
			}
		}
	}
	t.emit(task.ProjectID, "task", task)
	return nil
}
