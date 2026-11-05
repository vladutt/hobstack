package team

import (
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"hobstack/internal/config"
	"hobstack/internal/prompt"
	"hobstack/internal/store"
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

// MaxUserFiles: cate fisiere poate pune utilizatorul pe un task.
const MaxUserFiles = 50

// TooManyFilesError: s-ar depasi MaxUserFiles (web: 400).
type TooManyFilesError string

func (e TooManyFilesError) Error() string { return string(e) }

var errNoFilesRoot = errors.New("files cannot be attached in this setup")

// UserCreateTaskWithFiles e UserCreateTask cu fisierele utilizatorului: randurile lor exista
// inainte de Kick, deci agentul nu poate porni pe task fara ele. La eroare nu ramane nimic
// creat; fisierele din staging le sterge apelantul.
func (t *Team) UserCreateTaskWithFiles(projectID int64, title, description string, agentID, stageID int64, files []StagedFile) (store.Task, error) {
	t.work.Lock()
	defer t.work.Unlock()
	return t.userCreateTask(projectID, title, description, agentID, taskOrigin{files: files, stageID: stageID})
}

// AddUserFiles: utilizatorul pune fisiere pe un task existent. Dupa status:
// in lucru / blocat: asignatul primeste o nota cu caile si echipa e trezita;
// review / done: doar evenimentul (fara nota si fara run in plus);
// in coada: nimic, fisierele intra in tura lui.
func (t *Team) AddUserFiles(taskID int64, files []StagedFile) ([]store.TaskFile, error) {
	saved, kick, err := t.addUserFiles(taskID, files)
	if kick != 0 {
		t.Kick(kick) // dupa Unlock, ca la UserComment
	}
	return saved, err
}

// addUserFiles e corpul lui AddUserFiles, sub t.work; intoarce proiectul de trezit (0 = niciunul).
func (t *Team) addUserFiles(taskID int64, files []StagedFile) ([]store.TaskFile, int64, error) {
	t.work.Lock()
	defer t.work.Unlock()
	task, err := t.st.GetTask(taskID)
	if err != nil {
		return nil, 0, err
	}
	if task.Status == store.StatusArchived {
		return nil, 0, archivedError(task.Ref())
	}
	if len(files) == 0 {
		return []store.TaskFile{}, 0, nil
	}
	existing, err := t.st.ListUserTaskFiles(task.ID)
	if err != nil {
		return nil, 0, err
	}
	if err := checkUserFilesCap(task, len(existing), len(files)); err != nil {
		return nil, 0, err
	}
	saved, err := t.placeUserFiles(task, files)
	if err != nil {
		return nil, 0, err
	}
	who := ""
	if a, err := t.st.GetAgent(task.AssigneeID); err == nil {
		who = a.Name
	}
	lines := t.fileLines(task, saved)
	t.userFilesEvent(task, who, lines)
	t.emit(task.ProjectID, "task", task)
	if task.Status != store.StatusInProgress && task.Status != store.StatusBlocked {
		return saved, 0, nil
	}
	n := len(saved)
	text := fmt.Sprintf("The user attached %d %s to %s:\n%s", n, plural(n, "file", "files"), task.Ref(), strings.Join(lines, "\n"))
	if err := t.note(task.ProjectID, task.AssigneeID, clip(text), false); err != nil {
		log.Printf("task %d: user files note: %v", task.ID, err)
		return saved, 0, nil
	}
	return saved, task.ProjectID, nil
}

// checkUserFilesCap: cel mult MaxUserFiles fisiere ale utilizatorului pe un task.
func checkUserFilesCap(task store.Task, have, adding int) error {
	if have+adding <= MaxUserFiles {
		return nil
	}
	if have == 0 {
		return TooManyFilesError(fmt.Sprintf("a task can have at most %d of your files; you chose %d", MaxUserFiles, adding))
	}
	return TooManyFilesError(fmt.Sprintf("a task can have at most %d of your files; %s already has %d and you are adding %d",
		MaxUserFiles, task.Ref(), have, adding))
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

// userFilesEvent: "You attached N files" in istoric; corpul are randurile "- <cale> (<nume>)".
func (t *Team) userFilesEvent(task store.Task, to string, lines []string) {
	n := len(lines)
	t.taskMsg(task, "user_files", actorUser, to, fmt.Sprintf("You attached %d %s", n, plural(n, "file", "files")),
		strings.Join(lines, "\n"))
}

// fileLines: "- <cale absoluta> (<nume>)", ca in tura (prompt.TaskTurn).
func (t *Team) fileLines(task store.Task, list []store.TaskFile) []string {
	dir := config.TaskFilesDir(t.filesRoot, task.ProjectID, task.ID)
	out := make([]string, len(list))
	for i, f := range list {
		out[i] = fmt.Sprintf("- %s (%s)", filepath.Join(dir, f.Stored), f.Name)
	}
	return out
}

// userFileRefs: fisierele utilizatorului pentru tura unui task, cu calea absoluta a copiei:
// ale task-ului, apoi ale firului pe care il continua (ThreadID) si ale parintelui, fara dubluri;
// in fiecare grup cel mai nou primul.
func (t *Team) userFileRefs(task store.Task) []prompt.FileRef {
	if t.filesRoot == "" || task.ID == 0 {
		return nil
	}
	ids := []int64{task.ID}
	for _, id := range []*int64{task.ThreadID, task.ParentTaskID} {
		if id != nil && !slices.Contains(ids, *id) {
			ids = append(ids, *id)
		}
	}
	var refs []prompt.FileRef
	for _, id := range ids {
		list, err := t.st.ListUserTaskFiles(id)
		if err != nil {
			log.Printf("task %d: user files: %v", id, err)
			continue
		}
		dir := config.TaskFilesDir(t.filesRoot, task.ProjectID, id)
		for _, f := range list {
			refs = append(refs, prompt.FileRef{Name: f.Name, Path: filepath.Join(dir, f.Stored)})
		}
	}
	return refs
}
