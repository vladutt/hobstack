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

// AttachFiles copiaza fisierele predate la report_done si le leaga de task.
// Un fisier care nu poate fi copiat nu strica nimic: agentul afla care si de ce.
func (t *Team) AttachFiles(c mcp.Caller, ref string, paths []string) (string, error) {
	t.work.Lock()
	defer t.work.Unlock()
	task, a, err := t.todoTask(c, ref)
	if err != nil {
		return "", err
	}
	if t.filesRoot == "" {
		return "", errors.New("files cannot be attached in this setup")
	}
	p, err := t.st.GetProject(task.ProjectID)
	if err != nil {
		return "", err
	}
	roots := make([]string, 0, len(p.Folders))
	for _, f := range p.Folders {
		roots = append(roots, f.Path)
	}
	home := store.HomeFolder(a, p.Folders).Path
	dest := config.TaskFilesDir(t.filesRoot, p.ID, task.ID)

	var attached, failed []string
	seen := map[string]bool{}
	for _, raw := range paths {
		key := strings.TrimSpace(raw)
		if seen[key] {
			continue
		}
		seen[key] = true
		cp, err := taskfiles.Copy(roots, home, raw, dest)
		if err != nil {
			failed = append(failed, fmt.Sprintf("%s (%s)", key, err))
			continue
		}
		agentID := a.ID
		tf := store.TaskFile{TaskID: task.ID, ProjectID: p.ID, AgentID: &agentID, AgentName: a.Name,
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
