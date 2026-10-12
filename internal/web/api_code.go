package web

import (
	"errors"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"hobstack/internal/config"
	"hobstack/internal/store"
)

const maxFileView = 1 << 20 // 1 MB: peste atat modala nu arata continutul

type modifiedView struct {
	TaskID  int64     `json:"taskId"`
	Ref     string    `json:"ref"`
	Path    string    `json:"path"`
	Added   int       `json:"added"`
	Removed int       `json:"removed"`
	Created bool      `json:"created"`
	Edits   int       `json:"edits"`
	Agent   string    `json:"agent"`
	At      time.Time `json:"at"`
}

type fileView struct {
	Path     string           `json:"path"`
	Content  string           `json:"content"`
	Exists   bool             `json:"exists"`
	Binary   bool             `json:"binary"`
	TooLarge bool             `json:"tooLarge"`
	Markers  store.Markers    `json:"markers"`
	Exact    bool             `json:"exact"`
	Runs     []runChangesView `json:"runs"`
}

// taskTree intoarce task-ul si toate sub-task-urile lui (recursiv), in ordinea: task-ul, apoi dupa numar.
func (s *Server) taskTree(task store.Task) ([]store.Task, error) {
	out := []store.Task{task}
	for i := 0; i < len(out); i++ {
		subs, err := s.st.Subtasks(out[i].ID)
		if err != nil {
			return nil, err
		}
		slices.SortFunc(subs, func(a, b store.Task) int { return a.Number - b.Number })
		out = append(out, subs...)
	}
	return out, nil
}

// fileChanges: modificarile de fisier (fara comenzi) ale task-ului, pe cale, in ordine.
func fileChanges(changes []store.TaskChange) (map[string][]store.TaskChange, []string) {
	by := map[string][]store.TaskChange{}
	order := []string{}
	for _, c := range changes {
		if c.Kind == "command" || c.Path == "" {
			continue
		}
		if _, ok := by[c.Path]; !ok {
			order = append(order, c.Path)
		}
		by[c.Path] = append(by[c.Path], c)
	}
	return by, order
}

// getTaskModified: fisierele modificate de task si de sub-task-urile lui (tab-ul Modified).
func (s *Server) getTaskModified(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		fail(w, err)
		return
	}
	task, err := s.st.GetTask(id)
	if err != nil {
		fail(w, err)
		return
	}
	tree, err := s.taskTree(task)
	if err != nil {
		fail(w, err)
		return
	}
	files := []modifiedView{}
	for _, tk := range tree {
		changes, err := s.st.ListTaskChanges(tk.ID)
		if err != nil {
			fail(w, err)
			return
		}
		by, order := fileChanges(changes)
		slices.Sort(order)
		for _, path := range order {
			cs := by[path]
			v := modifiedView{TaskID: tk.ID, Ref: tk.Ref(), Path: path, Created: cs[0].Kind == "create", Edits: len(cs)}
			for _, c := range cs {
				for _, h := range c.Patch {
					for _, l := range h.Lines {
						switch {
						case strings.HasPrefix(l, "+"):
							v.Added++
						case strings.HasPrefix(l, "-"):
							v.Removed++
						}
					}
				}
				v.Agent, v.At = c.AgentName, c.At
			}
			files = append(files, v)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"files": files})
}

// getTaskFile: fisierul de acum de pe disc, cu marcajele task-ului. Doar cai din task_changes.
func (s *Server) getTaskFile(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		fail(w, err)
		return
	}
	task, err := s.st.GetTask(id)
	if err != nil {
		fail(w, err)
		return
	}
	// task=: un sub-task al task-ului din URL (fisierele din tab-ul Modified pot fi ale sub-task-urilor)
	target := task
	if q := r.URL.Query().Get("task"); q != "" && q != strconv.FormatInt(id, 10) {
		tree, err := s.taskTree(task)
		if err != nil {
			fail(w, err)
			return
		}
		sub, _ := strconv.ParseInt(q, 10, 64)
		i := slices.IndexFunc(tree, func(t store.Task) bool { return t.ID == sub })
		if i < 0 {
			fail(w, store.ErrNotFound)
			return
		}
		target = tree[i]
	}
	path := r.URL.Query().Get("path")
	changes, err := s.st.ListTaskChanges(target.ID)
	if err != nil {
		fail(w, err)
		return
	}
	by, _ := fileChanges(changes)
	cs, ok := by[path]
	if !ok {
		fail(w, store.ErrNotFound)
		return
	}
	v := fileView{Path: path, Runs: groupChanges(cs)}
	v.Markers, v.Exact = store.FileMarkers(cs)
	content, err := s.readTaskFile(target.ProjectID, cs[len(cs)-1].AgentID, path)
	switch {
	case errors.Is(err, errOutside), errors.Is(err, errNotRegular):
		fail(w, store.ErrNotFound)
		return
	case errors.Is(err, fs.ErrNotExist):
		// fisierul a fost sters: modala arata Diff
	case errors.Is(err, errTooLarge):
		v.Exists, v.TooLarge = true, true
	case err != nil:
		fail(w, err)
		return
	default:
		v.Exists = true
		if isBinary(content) {
			v.Binary = true
		} else {
			v.Content = content
		}
	}
	writeJSON(w, http.StatusOK, v)
}

var (
	errOutside  = errors.New("the path is outside the project folders")
	errTooLarge = errors.New("the file is too large")
	// errNotRegular: calea e un folder (ex. submodul git) sau un fisier special
	errNotRegular = errors.New("not a regular file")
)

// readTaskFile citeste o cale din task_changes: relativa la folderul agentului (sau, daca agentul nu
// mai exista, la folderele proiectului in ordine), absoluta doar intr-un folder al proiectului.
// Citirea trece prin os.OpenRoot, deci ".." si symlink-urile in afara folderului sunt refuzate.
func (s *Server) readTaskFile(projectID int64, agentID *int64, path string) (string, error) {
	p, err := s.st.GetProject(projectID)
	if err != nil {
		return "", err
	}
	roots := []string{}
	if agentID != nil {
		if a, err := s.st.GetAgent(*agentID); err == nil {
			roots = append(roots, store.HomeFolder(a, p.Folders).Path)
		}
	}
	for _, f := range p.Folders {
		roots = append(roots, f.Path)
	}
	roots = append(roots, config.ProjectFilesDir(s.dirs.Projects, projectID))

	if filepath.IsAbs(path) {
		for _, root := range roots {
			rel, err := filepath.Rel(root, path)
			if err == nil && rel != "." && !strings.HasPrefix(rel, "..") {
				return readIn(root, rel)
			}
		}
		return "", errOutside
	}
	if !filepath.IsLocal(path) {
		return "", errOutside
	}
	// cu mai multe foldere, calea e "<numele folderului>/<cale in folder>" (vezi recordChanges in team/run.go)
	if len(p.Folders) > 1 {
		if name, rest, ok := strings.Cut(filepath.ToSlash(path), "/"); ok {
			for _, f := range p.Folders {
				fn := f.Name
				if fn == "" {
					fn = store.FolderName(f.Path)
				}
				if fn == name {
					return readIn(f.Path, filepath.FromSlash(rest))
				}
			}
		}
	}
	var last error = fs.ErrNotExist
	for _, root := range roots {
		txt, err := readIn(root, path)
		if err == nil || !errors.Is(err, fs.ErrNotExist) {
			return txt, err
		}
		last = err
	}
	return "", last
}

// readIn citeste rel din root prin os.OpenRoot, cu limita maxFileView.
func readIn(root, rel string) (string, error) {
	rt, err := os.OpenRoot(root)
	if err != nil {
		return "", err
	}
	defer rt.Close()
	f, err := rt.Open(rel)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			return "", errOutside // iesire din root (symlink, ..) sau alta eroare de cale
		}
		return "", err
	}
	defer f.Close()
	if fi, err := f.Stat(); err != nil {
		return "", err
	} else if !fi.Mode().IsRegular() {
		return "", errNotRegular
	}
	b, err := io.ReadAll(io.LimitReader(f, maxFileView+1))
	if err != nil {
		return "", err
	}
	if len(b) > maxFileView {
		return "", errTooLarge
	}
	return string(b), nil
}
