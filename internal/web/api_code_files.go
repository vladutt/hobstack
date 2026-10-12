package web

import (
	"log"
	"net/http"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"hobstack/internal/codefiles"
	"hobstack/internal/store"
)

// maxCodeHits: cate fisiere intoarce o cautare dupa @ in chat.
const maxCodeHits = 30

type codeFile struct {
	Path   string `json:"path"`   // ce se scrie dupa @: relativa in folderul agentului, altfel absoluta
	Name   string `json:"name"`   // numele fisierului
	Folder string `json:"folder"` // numele folderului proiectului
	// Changed: fisier modificat pe task (?task=): vine primul in lista
	Changed bool `json:"changed,omitempty"`
}

// codeFiles: fisierele de cod ale proiectului pentru @ in chat (?q=cautarea, ?agent=agentul cu care vorbesti).
// Cu ?task= (comentariu pe task), fisierele modificate de task si de sub-task-uri vin primele.
func (s *Server) codeFiles(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		fail(w, err)
		return
	}
	p, err := s.st.GetProject(id)
	if err != nil {
		fail(w, err)
		return
	}
	// folderul agentului: acolo e cwd-ul lui, deci caile relative merg direct
	home := store.Folder{}
	if len(p.Folders) > 0 {
		home = p.Folders[0]
	}
	if aid, err := strconv.ParseInt(r.URL.Query().Get("agent"), 10, 64); err == nil && aid > 0 {
		if a, err := s.st.GetAgent(aid); err == nil && a.ProjectID == p.ID {
			home = store.HomeFolder(a, p.Folders)
		}
	}
	q := r.URL.Query().Get("q")
	out := []codeFile{}
	seen := map[string]bool{} // cai absolute deja in lista
	if tid, err := strconv.ParseInt(r.URL.Query().Get("task"), 10, 64); err == nil && tid > 0 {
		changed, err := s.changedFiles(p, tid, home)
		if err != nil {
			fail(w, err)
			return
		}
		shows := make([]string, len(changed))
		for i, c := range changed {
			shows[i] = filepath.ToSlash(c.file.Path)
		}
		for _, i := range codefiles.SearchIdx(shows, q, maxCodeHits) {
			out = append(out, changed[i].file)
			seen[changed[i].abs] = true
		}
	}
	// toate folderele intr-o singura lista, ca ordinea sa fie una pentru tot proiectul
	var rels []string
	var owner []store.Folder
	for _, f := range p.Folders {
		files, err := s.codeCache.Files(f.Path)
		if err != nil {
			log.Printf("code files %s: %v", f.Path, err) // un folder sters de pe disc nu strica restul
			continue
		}
		for _, rel := range files {
			rels = append(rels, rel)
			owner = append(owner, f)
		}
	}
	for _, i := range codefiles.SearchIdx(rels, q, maxCodeHits) {
		if len(out) >= maxCodeHits {
			break
		}
		f, rel := owner[i], rels[i]
		if seen[filepath.Join(f.Path, filepath.FromSlash(rel))] {
			continue
		}
		show := rel
		if f.ID != home.ID {
			show = filepath.Join(f.Path, filepath.FromSlash(rel))
		}
		out = append(out, codeFile{Path: show, Name: path.Base(rel), Folder: f.Name})
	}
	writeJSON(w, http.StatusOK, map[string]any{"files": out})
}

// changedFile: un fisier modificat pe task, cu calea absoluta (pentru dubluri) si cum se scrie dupa @
type changedFile struct {
	abs  string
	file codeFile
}

// changedFiles: fisierele modificate de task si de sub-task-urile lui, cele mai recente primele.
// Caile din task_changes sunt relative la folderul agentului care a scris; aici devin relative
// la home (folderul agentului care primeste comentariul) sau absolute cand sunt in alt folder.
func (s *Server) changedFiles(p store.Project, taskID int64, home store.Folder) ([]changedFile, error) {
	task, err := s.st.GetTask(taskID)
	if err != nil {
		return nil, err
	}
	if task.ProjectID != p.ID {
		return nil, store.ErrNotFound
	}
	tree, err := s.taskTree(task)
	if err != nil {
		return nil, err
	}
	homes := map[int64]store.Folder{} // agent -> folderul lui
	homeOf := func(id *int64) store.Folder {
		if id == nil {
			return firstFolder(p)
		}
		if f, ok := homes[*id]; ok {
			return f
		}
		f := firstFolder(p)
		if a, err := s.st.GetAgent(*id); err == nil {
			f = store.HomeFolder(a, p.Folders)
		}
		homes[*id] = f
		return f
	}
	type entry struct {
		changedFile
		at time.Time
	}
	byAbs := map[string]int{}
	var list []entry
	for _, t := range tree {
		changes, err := s.st.ListTaskChanges(t.ID)
		if err != nil {
			return nil, err
		}
		for _, c := range changes {
			if c.Kind == "command" || c.Path == "" {
				continue
			}
			abs := filepath.Clean(c.Path)
			if !filepath.IsAbs(abs) {
				abs = filepath.Join(homeOf(c.AgentID).Path, filepath.FromSlash(c.Path))
			}
			if i, ok := byAbs[abs]; ok {
				if c.At.After(list[i].at) {
					list[i].at = c.At
				}
				continue
			}
			byAbs[abs] = len(list)
			list = append(list, entry{changedFile{abs, codeFile{Path: showPath(abs, home), Name: filepath.Base(abs), Folder: projectFolderName(p, abs), Changed: true}}, c.At})
		}
	}
	sort.SliceStable(list, func(i, j int) bool { return list[i].at.After(list[j].at) })
	out := make([]changedFile, len(list))
	for i, e := range list {
		out[i] = e.changedFile
	}
	return out, nil
}

func firstFolder(p store.Project) store.Folder {
	if len(p.Folders) > 0 {
		return p.Folders[0]
	}
	return store.Folder{}
}

// showPath: calea relativa in home cand fisierul e acolo, altfel absoluta
func showPath(abs string, home store.Folder) string {
	if home.Path != "" {
		if rel, err := filepath.Rel(home.Path, abs); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return rel
		}
	}
	return abs
}

// projectFolderName: numele folderului proiectului care contine fisierul ("" in afara lor)
func projectFolderName(p store.Project, abs string) string {
	for _, f := range p.Folders {
		if rel, err := filepath.Rel(f.Path, abs); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return f.Name
		}
	}
	return ""
}
