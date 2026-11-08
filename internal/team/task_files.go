package team

import (
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"hobstack/internal/config"
	"hobstack/internal/mcp"
	"hobstack/internal/runner"
	"hobstack/internal/store"
	"hobstack/internal/taskfiles"
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
	var task store.Task
	var a store.Agent
	var err error
	if strings.TrimSpace(ref) == "" && c.TaskID != 0 {
		// attach_files si atasarea automata: task-ul run-ului, pentru oricine lucreaza pe el (si reviewer-ul)
		if task, err = t.st.GetTask(c.TaskID); err != nil {
			return attachTarget{}, err
		}
		if task.Status == store.StatusArchived {
			return attachTarget{}, fmt.Errorf("%s was deleted by the user", task.Ref())
		}
		if a, err = t.st.GetAgent(c.AgentID); err != nil {
			return attachTarget{}, err
		}
	} else if task, a, err = t.todoTask(c, ref); err != nil {
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

	var attached, failed, same []string
	seen := map[string]bool{}
	existing, _ := t.st.ListTaskFiles(task.ID)
	for _, raw := range paths {
		key := strings.TrimSpace(raw)
		if seen[key] {
			continue
		}
		seen[key] = true
		if alreadyAttached(existing, home, key) {
			same = append(same, key)
			continue
		}
		cp, err := copyTaskFile(roots, home, raw, dest)
		if err != nil {
			failed = append(failed, fmt.Sprintf("%s (%s)", key, err))
			continue
		}
		// un fisier din fisierele proiectului: "research/leads.csv", nu calea lunga din ~/.hobstack
		if rel, ok := taskfiles.RelIn(tg.files, cp.Path); ok {
			cp.Path = rel
		}
		agentID := a.ID
		tf := store.TaskFile{TaskID: task.ID, ProjectID: task.ProjectID, AgentID: &agentID, AgentName: a.Name,
			Path: cp.Path, Name: cp.Name, Stored: cp.Stored, Size: cp.Size, Mime: cp.Mime, Source: cp.Source}
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
	if len(same) > 0 {
		out += " Already attached, unchanged: " + strings.Join(same, ", ") + "."
	}
	if len(failed) > 0 {
		out += " Not attached: " + strings.Join(failed, "; ") + "."
	}
	return out, nil
}

// alreadyAttached: fisierul (aceeasi cale originala) e deja pe task si nu s-a schimbat de atunci. Asa atasarea
// automata de la finalul run-ului nu dubleaza ce a atasat agentul cu attach_files.
func alreadyAttached(existing []store.TaskFile, home, raw string) bool {
	abs := raw
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(home, raw)
	}
	abs = filepath.Clean(abs)
	info, err := os.Stat(abs)
	if err != nil {
		return false
	}
	for _, f := range existing { // cel mai nou primul
		if f.Source != abs {
			continue
		}
		return f.Size == info.Size() && !info.ModTime().After(f.CreatedAt.Add(time.Second))
	}
	return false
}

// AttachToChat: attach_files intr-un run fara task (chat). Fisierele se copiaza la fisierele proiectului si apar
// intr-un mesaj al agentului, ca atasamente.
func (t *Team) AttachToChat(c mcp.Caller, paths []string, note string) (string, error) {
	if t.filesRoot == "" {
		return "", errors.New("files cannot be attached in this setup")
	}
	t.work.Lock()
	a, err := t.st.GetAgent(c.AgentID)
	var p store.Project
	if err == nil {
		p, err = t.st.GetProject(c.ProjectID)
	}
	t.work.Unlock()
	if err != nil {
		return "", err
	}
	roots := make([]string, 0, len(p.Folders)+1)
	for _, f := range p.Folders {
		roots = append(roots, f.Path)
	}
	dest := t.filesDir(p.ID)
	roots = append(roots, dest)
	home := store.HomeFolder(a, p.Folders).Path

	var atts []store.Attachment
	var ids []int64
	var names, failed []string
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
		att := store.Attachment{ProjectID: p.ID, Kind: store.KindMessageFile, Name: cp.Name, Stored: cp.Stored,
			Mime: cp.Mime, Size: cp.Size, Source: cp.Source}
		if err := t.st.AddAttachment(&att); err != nil {
			removeQuiet(dest, cp.Stored)
			failed = append(failed, fmt.Sprintf("%s (%s)", key, err))
			continue
		}
		atts = append(atts, att)
		ids = append(ids, att.ID)
		names = append(names, cp.Name)
	}
	out := "No files attached."
	if len(atts) > 0 {
		text := strings.TrimSpace(note)
		if text == "" {
			text = "Attached " + strings.Join(names, ", ")
		}
		t.mu.Lock()
		runID := t.active[c.AgentID].runID
		t.mu.Unlock()
		t.work.Lock()
		m := store.Message{ProjectID: p.ID, AgentID: a.ID, Kind: store.KindMsg, Author: a.Name, Text: text, RunID: runID, Delivered: true}
		err := t.st.AddMessage(&m)
		if err == nil {
			err = t.st.LinkAttachments(ids, m.ID)
		}
		if err == nil {
			m.Attachments = atts
			t.emit(p.ID, "message", m)
		}
		t.work.Unlock()
		if err != nil {
			return "", err
		}
		out = fmt.Sprintf("Attached %d %s to your chat with the user.", len(atts), plural(len(atts), "file", "files"))
	}
	if len(failed) > 0 {
		out += " Not attached: " + strings.Join(failed, "; ") + "."
	}
	return out, nil
}

// visualExt: fisierele pe care utilizatorul le verifica privindu-le (capturi, imagini, SVG, PDF).
var visualExt = map[string]bool{".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".webp": true, ".avif": true, ".svg": true, ".pdf": true}

const (
	maxAutoVisuals    = 10
	maxAutoVisualSize = 10 << 20
)

// visualFiles: fisierele vizuale create sau schimbate in run (Write / Edit si prin shell), cel mult maxAutoVisuals.
func visualFiles(changes []runner.FileChange) []string {
	var out []string
	seen := map[string]bool{}
	for _, c := range changes {
		if c.Kind == "delete" || seen[c.Path] || !visualExt[strings.ToLower(filepath.Ext(c.Path))] {
			continue
		}
		seen[c.Path] = true
		info, err := os.Stat(c.Path)
		if err != nil || !info.Mode().IsRegular() || info.Size() > maxAutoVisualSize {
			continue
		}
		out = append(out, c.Path)
		if len(out) == maxAutoVisuals {
			break
		}
	}
	return out
}

// attachVisuals: la finalul unui run pe task, imaginile / SVG-urile / PDF-urile facute in run ajung singure pe task
// (fara dubluri cu ce a atasat agentul). Se cheama din finish, inainte de t.work (copierea poate dura).
func (t *Team) attachVisuals(agentID int64, o runner.Outcome) {
	t.mu.Lock()
	ar, ok := t.active[agentID]
	t.mu.Unlock()
	if !ok || ar.runID != o.RunID || ar.taskID == 0 {
		return
	}
	paths := visualFiles(o.Changes)
	if len(paths) == 0 {
		return
	}
	c := mcp.Caller{ProjectID: ar.projectID, AgentID: agentID, TaskID: ar.taskID}
	if _, err := t.AttachFiles(c, "", paths); err != nil {
		log.Printf("run %s: attaching visuals: %v", o.RunID, err)
	}
}

// removeQuiet sterge o copie ramasa fara rand in baza de date.
func removeQuiet(dir, name string) {
	if err := os.Remove(filepath.Join(dir, name)); err != nil && !os.IsNotExist(err) {
		log.Printf("task files: cannot remove %s: %v", name, err)
	}
}
