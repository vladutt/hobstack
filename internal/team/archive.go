package team

import (
	"errors"
	"fmt"
	"log"

	"hobstack/internal/store"
)

// ErrArchived: task-ul a fost sters de utilizator (tool-urile MCP primesc mesajul lui).
var ErrArchived = errors.New("the task was deleted")

// archivedError: actiune a utilizatorului pe un task sters (comentariu, bifa); errors.Is(err, ErrArchived) e adevarat.
type archivedError string

func (e archivedError) Error() string        { return string(e) + " was deleted; restore it first." }
func (e archivedError) Is(target error) bool { return target == ErrArchived }

// RestoreError: Restore refuzat (de ex. parintele e inca sters).
type RestoreError string

func (e RestoreError) Error() string { return string(e) }

// ArchiveTask sterge (arhiveaza) task-ul si sub-task-urile lui: run-urile active pe ele se opresc,
// asignatii si decidentul afla, iar intrebarile / cererile legate se inchid. Intoarce cate au plecat.
func (t *Team) ArchiveTask(taskID int64) (int, error) {
	t.work.Lock()
	defer t.work.Unlock()
	task, err := t.st.GetTask(taskID)
	if err != nil {
		return 0, err
	}
	if task.Status == store.StatusArchived {
		return 0, ErrArchived
	}
	tree, err := t.st.TaskTree(taskID)
	if err != nil {
		return 0, err
	}
	agents, err := t.st.ListAgents(task.ProjectID)
	if err != nil {
		return 0, err
	}
	ids := make([]int64, len(tree))
	inTree := map[int64]bool{}
	for i, x := range tree {
		ids[i] = x.ID
		inTree[x.ID] = true
	}
	// run-urile active pe task-urile din grup: le oprim (finish vede apoi un task arhivat si nu-l atinge)
	t.mu.Lock()
	var stop []string
	working := map[int64]bool{} // agentii care lucrau pe grup
	for agentID, ar := range t.active {
		if inTree[ar.taskID] {
			stop = append(stop, ar.runID)
			working[agentID] = true
		}
	}
	t.mu.Unlock()
	for _, id := range stop {
		if err := t.runs.Stop(id); err != nil {
			log.Printf("stopping run %s: %v", id, err)
		}
	}
	if err := t.st.ArchiveTasks(ids, taskID); err != nil {
		return 0, err
	}
	t.taskEvent(task, "deleted", actorUser, fmt.Sprintf("You deleted it (%d %s)", len(ids), plural(len(ids), "task", "tasks")))
	text := fmt.Sprintf("The user deleted %s: %s. Stop working on it.", task.Ref(), task.Title)
	for agentID := range working {
		t.note(task.ProjectID, agentID, text, true)
	}
	if d, ok := decider(task, agents, t.autonomyOf(task.ProjectID)); ok && !working[d.ID] {
		// un task Done sters e doar curatenie: nota ramane informativa (livrata), fara un run platit
		done := task.Status == store.StatusDone
		t.note(task.ProjectID, d.ID, fmt.Sprintf("The user deleted %s: %s. Do not wait for it.", task.Ref(), task.Title), done)
	}
	t.emit(task.ProjectID, "task", task)
	t.Kick(task.ProjectID)
	return len(ids), nil
}

// RestoreTask readuce grupul sters impreuna cu taskID (doar radacina grupului se restaureaza).
func (t *Team) RestoreTask(taskID int64) error {
	t.work.Lock()
	defer t.work.Unlock()
	task, err := t.st.GetTask(taskID)
	if err != nil {
		return err
	}
	if task.Status != store.StatusArchived || task.ArchivedRoot == nil || *task.ArchivedRoot != task.ID {
		return RestoreError(task.Ref() + " is not a deleted task")
	}
	if task.ParentTaskID != nil {
		if p, err := t.st.GetTask(*task.ParentTaskID); err == nil && p.Status == store.StatusArchived {
			return RestoreError("Restore " + p.Ref() + " first.")
		}
	}
	restored, err := t.st.RestoreGroup(taskID)
	if err != nil {
		return err
	}
	if len(restored) > 0 {
		t.taskEvent(restored[0], "restored", actorUser, "You restored it")
		t.emit(task.ProjectID, "task", restored[0])
	}
	t.Kick(task.ProjectID)
	return nil
}
