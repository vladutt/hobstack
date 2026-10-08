package team

import (
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"foreman/internal/config"
	"foreman/internal/mcp"
	"foreman/internal/store"
	"foreman/internal/taskfiles"
)

// copyTaskFile e taskfiles.Copy; testele il inlocuiesc ca sa vada daca t.work e liber in timpul copierii.
var copyTaskFile = taskfiles.Copy

// attachTarget: ce trebuie stiut ca sa copiem, citit sub t.work.
type attachTarget struct {
	task  store.Task
	agent store.Agent
	roots []string
	home  string
	files string // fisierele proiectului (pagina Files); caile de acolo se arata relativ
	dest  string
}

func (t *Team) attachTarget(c mcp.Caller, ref string) (attachTarget, error) {
	t.work.Lock()
	defer t.work.Unlock()
	task, a, err := t.todoTask(c, ref)
	if err != nil {
		return attachTarget{}, err
	}
	if t.filesRoot == "" {
		return attachTarget{}, errors.New("files cannot be attached in this setup")
	}
	p, err := t.st.GetProject(task.ProjectID)
	if err != nil {
		return attachTarget{}, err
	}
	roots := make([]string, 0, len(p.Folders)+1)
	for _, f := range p.Folders {
		roots = append(roots, f.Path)
	}
	// fisierele proiectului: agentii le primesc prin --add-dir si scriu des livrabilele acolo
	files := t.filesDir(p.ID)
	roots = append(roots, files)
	return attachTarget{task: task, agent: a, roots: roots, home: store.HomeFolder(a, p.Folders).Path,
		files: files, dest: config.TaskFilesDir(t.filesRoot, p.ID, task.ID)}, nil
}

// AttachFiles copiaza fisierele predate la report_done si le leaga de task.
// Un fisier care nu poate fi copiat nu strica nimic: agentul afla care si de ce.
// Copierea (pana la 20 x 50 MB) se face fara t.work: un disc lent nu opreste ceilalti agenti.
func (t *Team) AttachFiles(c mcp.Caller, ref string, paths []string) (string, error) {
	tg, err := t.attachTarget(c, ref)
	if err != nil {
		return "", err
	}
	task, a, roots, home, dest := tg.task, tg.agent, tg.roots, tg.home, tg.dest

	var attached, failed []string
	seen := map[string]bool{}
	for _, raw := range paths {
		key := strings.TrimSpace(raw)
		if seen[key] {
			continue
		}
		seen[key] = true
		cp, err := copyTaskFile(roots, home, raw, dest)
		if err != nil {
			failed = append(failed, fmt.Sprintf("%s (%s)", key, err))
			continue
		}
		// un fisier din fisierele proiectului: "research/leads.csv", nu calea lunga din ~/.foreman
		if rel, ok := taskfiles.RelIn(tg.files, cp.Path); ok {
			cp.Path = rel
		}
		agentID := a.ID
		tf := store.TaskFile{TaskID: task.ID, ProjectID: task.ProjectID, AgentID: &agentID, AgentName: a.Name,
			Path: cp.Path, Name: cp.Name, Stored: cp.Stored, Size: cp.Size, Mime: cp.Mime}
		if err := t.st.AddTaskFile(&tf); err != nil {
			removeQuiet(dest, cp.Stored)
			failed = append(failed, fmt.Sprintf("%s (%s)", key, err))
			continue
		}
		attached = append(attached, cp.Path)
	}

	out := "No files attached."
	if n := len(attached); n > 0 {
		out = fmt.Sprintf("Attached %d %s.", n, plural(n, "file", "files"))
		t.work.Lock() // evenimentul intra in istoric ca orice schimbare de stare
		defer t.work.Unlock()
		t.taskMsg(task, "files_attached", a.Name, actorUser,
			fmt.Sprintf("%s attached %d %s", a.Name, n, plural(n, "file", "files")),
			"- "+strings.Join(attached, "\n- "))
		t.emit(c.ProjectID, "task", task)
	}
	if len(failed) > 0 {
		out += " Not attached: " + strings.Join(failed, "; ") + "."
	}
	return out, nil
}

// removeQuiet sterge o copie ramasa fara rand in baza de date.
func removeQuiet(dir, name string) {
	if err := os.Remove(filepath.Join(dir, name)); err != nil && !os.IsNotExist(err) {
		log.Printf("task files: cannot remove %s: %v", name, err)
	}
}
