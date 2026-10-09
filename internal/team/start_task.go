package team

import (
	"errors"
	"fmt"
	"strings"

	"hobstack/internal/mcp"
	"hobstack/internal/store"
)

// MaxSelfStarted: cate task-uri deschise pornite cu start_task poate avea un agent (anti-bucla).
const MaxSelfStarted = 3

// StartTask e tool-ul start_task: intr-un run fara task, agentul isi porneste un task pentru
// modificarea ceruta. CreatorID ramane gol (altfel si-ar decide singur task-ul): decide seful,
// iar pentru o cerere din chat (run-ul are request) decide utilizatorul. Fara etape.
func (t *Team) StartTask(c mcp.Caller, title, description, continues string) (string, error) {
	t.work.Lock()
	defer t.work.Unlock()
	if c.TaskID != 0 {
		ref := "a task"
		if task, err := t.st.GetTask(c.TaskID); err == nil {
			ref = task.Ref()
		}
		return "", fmt.Errorf("you are already working on %s; do the work here", ref)
	}
	title = strings.TrimSpace(title)
	if title == "" {
		return "", errors.New("title is required")
	}
	a, err := t.st.GetAgent(c.AgentID)
	if err != nil {
		return "", errors.New("your agent no longer exists")
	}
	open, err := t.st.CountSelfStarted(a.ID)
	if err != nil {
		return "", err
	}
	if open >= MaxSelfStarted {
		return "", fmt.Errorf("you already have %d open tasks you started yourself; finish those first", open)
	}
	threadID, err := t.threadFor(c.ProjectID, continues)
	if err != nil {
		return "", err
	}
	_, fromChat := t.requestOf(c.AgentID, c.RunID)
	task := store.Task{ProjectID: c.ProjectID, Title: title, Description: strings.TrimSpace(description),
		AssigneeID: a.ID, ThreadID: threadID, ByUser: fromChat}
	if err := t.st.CreateTask(&task); err != nil {
		return "", err
	}
	t.recordRequest(c, task, a.Name)
	t.taskMsg(task, "created", a.Name, a.Name, "Started by "+a.Name, briefBody(task.Title, task.Description))
	t.taskEvent(task, "self_started", a.Name, a.Name+" works on it")
	t.note(c.ProjectID, a.ID, fmt.Sprintf("Started %s: %s", task.Ref(), title), true)
	t.emit(c.ProjectID, "task", task)
	t.Kick(c.ProjectID)
	// echipa oprita (de utilizator sau dupa o eroare): task-ul asteapta Start team; agentul trebuie sa-i spuna
	if p, err := t.st.GetProject(c.ProjectID); err == nil && p.Paused {
		return fmt.Sprintf("Created %s. The team is stopped: %s starts when the user clicks Start team. Tell the user, then stop.", task.Ref(), task.Ref()), nil
	}
	return fmt.Sprintf("Started %s. Stop now; the work continues in the task.", task.Ref()), nil
}
