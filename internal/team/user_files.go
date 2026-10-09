package team

import (
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"foreman/internal/config"
	"foreman/internal/prompt"
	"foreman/internal/store"
)

// StagedFile e un fisier urcat de utilizator, deja scris pe disc in Dir/Stored (staging),
// dar inca fara rand in task_files. Mutarea in task-files/<task>/ se face sub t.work.
type StagedFile struct {
	Dir    string // folderul de staging (acelasi disc cu task-files, ca rename-ul sa mearga)
	Stored string // numele de pe disc (prefix aleator + nume), pastrat si dupa mutare
	Name   string // numele afisat (curatat cu store.CleanFileName)
	Mime   string
	Size   int64
}

var errNoFilesRoot = errors.New("files cannot be attached in this setup")

// UserCreateTaskWithFiles e UserCreateTask cu fisierele utilizatorului: randurile lor exista
// inainte de Kick, deci agentul nu poate porni pe task fara ele. La eroare nu ramane nimic
// creat; fisierele din staging le sterge apelantul.
func (t *Team) UserCreateTaskWithFiles(projectID int64, title, description string, agentID int64, files []StagedFile) (store.Task, error) {
	t.work.Lock()
	defer t.work.Unlock()
	return t.userCreateTask(projectID, title, description, agentID, taskOrigin{files: files})
}

// AddUserFiles: utilizatorul pune fisiere pe un task existent. Asignatul care lucreaza
// sau a lucrat deja pe task primeste o nota; un task in coada le gaseste in tura lui.
func (t *Team) AddUserFiles(taskID int64, files []StagedFile) ([]store.TaskFile, error) {
	t.work.Lock()
	defer t.work.Unlock()
	task, err := t.st.GetTask(taskID)
	if err != nil {
		return nil, err
	}
	if task.Status == store.StatusArchived {
		return nil, archivedError(task.Ref())
	}
	if len(files) == 0 {
		return []store.TaskFile{}, nil
	}
	saved, err := t.placeUserFiles(task, files)
	if err != nil {
		return nil, err
	}
	names := fileNames(saved)
	who := ""
	if a, err := t.st.GetAgent(task.AssigneeID); err == nil {
		who = a.Name
	}
	t.userFilesEvent(task, who, names)
	switch task.Status {
	case store.StatusInProgress, store.StatusReview, store.StatusBlocked, store.StatusDone:
		n := len(names)
		text := fmt.Sprintf("The user attached %d %s to %s: %s.", n, plural(n, "file", "files"), task.Ref(), strings.Join(names, ", "))
		if err := t.note(task.ProjectID, task.AssigneeID, clip(text), false); err != nil {
			log.Printf("task %d: user files note: %v", task.ID, err)
		}
	}
	t.emit(task.ProjectID, "task", task)
	return saved, nil
}

// placeUserFiles muta fisierele din staging in task-files/<task>/ si le adauga intr-o
// tranzactie. La eroare sterge copiile mutate. Se cheama cu t.work tinut.
func (t *Team) placeUserFiles(task store.Task, files []StagedFile) ([]store.TaskFile, error) {
	if t.filesRoot == "" {
		return nil, errNoFilesRoot
	}
	dest := config.TaskFilesDir(t.filesRoot, task.ProjectID, task.ID)
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return nil, err
	}
	var moved []string
	undo := func() {
		for _, name := range moved {
			removeQuiet(dest, name)
		}
	}
	rows := make([]*store.TaskFile, 0, len(files))
	for _, f := range files {
		if err := os.Rename(filepath.Join(f.Dir, f.Stored), filepath.Join(dest, f.Stored)); err != nil {
			undo()
			return nil, fmt.Errorf("saving %s: %w", f.Name, err)
		}
		moved = append(moved, f.Stored)
		rows = append(rows, &store.TaskFile{TaskID: task.ID, ProjectID: task.ProjectID, AgentName: store.UserFileAuthor,
			Path: f.Name, Name: f.Name, Stored: f.Stored, Size: f.Size, Mime: f.Mime})
	}
	if err := t.st.AddTaskFiles(rows); err != nil {
		undo()
		return nil, err
	}
	out := make([]store.TaskFile, len(rows))
	for i, r := range rows {
		out[i] = *r
	}
	return out, nil
}

// userFilesEvent: "You attached N files" in istoric, cu numele in corp.
func (t *Team) userFilesEvent(task store.Task, to string, names []string) {
	n := len(names)
	t.taskMsg(task, "user_files", actorUser, to, fmt.Sprintf("You attached %d %s", n, plural(n, "file", "files")),
		"- "+strings.Join(names, "\n- "))
}

func fileNames(list []store.TaskFile) []string {
	out := make([]string, len(list))
	for i, f := range list {
		out[i] = f.Name
	}
	return out
}

// userFileRefs: fisierele utilizatorului pe task, cu calea absoluta a copiei (pentru tura).
func (t *Team) userFileRefs(projectID, taskID int64) []prompt.FileRef {
	if t.filesRoot == "" || taskID == 0 {
		return nil
	}
	list, err := t.st.ListUserTaskFiles(taskID)
	if err != nil {
		log.Printf("task %d: user files: %v", taskID, err)
		return nil
	}
	dir := config.TaskFilesDir(t.filesRoot, projectID, taskID)
	refs := make([]prompt.FileRef, 0, len(list))
	for _, f := range list {
		refs = append(refs, prompt.FileRef{Name: f.Name, Path: filepath.Join(dir, f.Stored)})
	}
	return refs
}
