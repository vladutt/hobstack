package web

import (
	"errors"
	"io/fs"
	"log"
	"net/http"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"hobstack/internal/codefiles"
	"hobstack/internal/store"
)

// treeFolder: un folder al proiectului in explorer-ul de cod (Cmd/Ctrl+P)
type treeFolder struct {
	ID        int64    `json:"id"`
	Name      string   `json:"name"`
	Path      string   `json:"path"`
	Files     []string `json:"files"`     // relative, cu "/", ca la @ in chat
	Truncated bool     `json:"truncated"` // s-a atins codefiles.MaxFiles
	Error     string   `json:"error,omitempty"`
}

// treeFolderName: numele afisat al folderului (ca in task_changes cand proiectul are mai multe foldere)
func treeFolderName(f store.Folder) string {
	if f.Name != "" {
		return f.Name
	}
	return store.FolderName(f.Path)
}

// getProjectTree: fisierele fiecarui folder al proiectului, aceeasi lista ca la @ in chat.
// Un folder care nu se poate citi vine cu Error; celelalte merg normal.
func (s *Server) getProjectTree(w http.ResponseWriter, r *http.Request) {
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
	out := []treeFolder{}
	for _, f := range p.Folders {
		tf := treeFolder{ID: f.ID, Name: treeFolderName(f), Path: f.Path, Files: []string{}}
		files, err := s.codeCache.Files(f.Path)
		switch {
		case errors.Is(err, fs.ErrNotExist):
			tf.Error = "Folder not found: " + f.Path
		case err != nil:
			log.Printf("code tree %s: %v", f.Path, err)
			tf.Error = "Can't read the folder: " + f.Path
		default:
			tf.Files = files
			tf.Truncated = len(files) >= codefiles.MaxFiles
		}
		out = append(out, tf)
	}
	writeJSON(w, http.StatusOK, map[string]any{"folders": out})
}

// codeFileView: un fisier deschis in explorer-ul de cod
type codeFileView struct {
	Folder   int64  `json:"folder"`
	Path     string `json:"path"`
	Content  string `json:"content,omitempty"`
	Binary   bool   `json:"binary"`
	TooLarge bool   `json:"tooLarge"`
	// NotRegular: un folder (ex. submodul git) sau un fisier special, listat de git
	NotRegular bool `json:"notRegular"`
}

// isBinary: NUL in primii 8 KB (ca in modala de fisier a task-ului)
func isBinary(content string) bool {
	head := content
	if len(head) > 8192 {
		head = head[:8192]
	}
	return strings.IndexByte(head, 0) >= 0
}

// getCodeFile: un fisier din explorer-ul de cod (?folder=<id>&path=<rel>). Se citeste doar daca e in
// lista folderului (aceeasi ca la @): un fisier ignorat de git sau din node_modules nu se poate cere
// scriind calea. Citirea prin os.OpenRoot (readIn), deci ".." si symlink-urile in afara sunt refuzate.
func (s *Server) getCodeFile(w http.ResponseWriter, r *http.Request) {
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
	fid, _ := strconv.ParseInt(r.URL.Query().Get("folder"), 10, 64)
	i := slices.IndexFunc(p.Folders, func(f store.Folder) bool { return f.ID == fid })
	if i < 0 {
		fail(w, store.ErrNotFound)
		return
	}
	folder := p.Folders[i]
	rel := r.URL.Query().Get("path")
	if rel == "" || strings.Contains(rel, "\\") || !filepath.IsLocal(filepath.FromSlash(rel)) {
		fail(w, badRequest("bad path"))
		return
	}
	if !s.inCodeList(folder.Path, rel) {
		fail(w, store.ErrNotFound)
		return
	}
	v := codeFileView{Folder: folder.ID, Path: rel}
	content, err := readIn(folder.Path, filepath.FromSlash(rel))
	switch {
	case errors.Is(err, fs.ErrNotExist):
		// sters intre timp: lista din cache e veche, arborele reincarcat nu trebuie sa-l mai arate
		s.codeCache.Invalidate(folder.Path)
		fail(w, store.ErrNotFound)
		return
	case errors.Is(err, errOutside):
		fail(w, store.ErrNotFound)
		return
	case errors.Is(err, errTooLarge):
		v.TooLarge = true
	case errors.Is(err, errNotRegular):
		v.NotRegular = true
	case err != nil:
		fail(w, err)
		return
	case isBinary(content):
		v.Binary = true
	default:
		v.Content = content
	}
	writeJSON(w, http.StatusOK, v)
}

// inCodeList: rel e in lista folderului; daca nu, lista se reciteste o data (un fisier tocmai creat)
func (s *Server) inCodeList(dir, rel string) bool {
	files, err := s.codeCache.Files(dir)
	if err == nil && slices.Contains(files, rel) {
		return true
	}
	s.codeCache.Invalidate(dir)
	files, err = s.codeCache.Files(dir)
	return err == nil && slices.Contains(files, rel)
}
