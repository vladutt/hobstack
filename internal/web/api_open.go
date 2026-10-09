package web

import (
	"errors"
	"net/http"
	"os"
	"strings"

	"hobstack/internal/opener"
	"hobstack/internal/store"
)

// Opener deschide folderul proiectului in Finder, in editor sau pagina lui
// html in browser. In productie e *opener.Opener, in teste un fals.
type Opener interface {
	Editors() []opener.Editor
	OpenFolder(dir string) error
	OpenFile(path string) error
	OpenInEditor(id, dir string) error
}

// SetOpener activeaza butoanele "Deschide". Fara el, ruta intoarce 501.
func (s *Server) SetOpener(o Opener) { s.opener = o }

func (s *Server) editors() []opener.Editor {
	if s.opener == nil {
		return []opener.Editor{}
	}
	return s.opener.Editors()
}

// openProject: POST /api/projects/{id}/open cu {target: folder|browser|editor, editor, folderId?}.
func (s *Server) openProject(w http.ResponseWriter, r *http.Request) {
	if s.opener == nil {
		writeErr(w, http.StatusNotImplemented, "opening is not available on this system")
		return
	}
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
	var in struct {
		Target   string `json:"target"`
		Editor   string `json:"editor"`
		FolderID *int64 `json:"folderId"`
	}
	if err := decode(r, &in); err != nil {
		fail(w, err)
		return
	}
	folder := store.HomeFolder(store.Agent{FolderID: in.FolderID}, p.Folders)
	if in.FolderID != nil && folder.ID != *in.FolderID {
		fail(w, badRequest("unknown folder"))
		return
	}
	dir := folder.Path
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		fail(w, badRequest("the folder does not exist: "+dir))
		return
	}

	switch strings.TrimSpace(in.Target) {
	case "folder":
		err = s.opener.OpenFolder(dir)
	case "browser":
		var page string
		page, err = opener.FindHTML(dir)
		if errors.Is(err, opener.ErrNoHTML) {
			fail(w, badRequest(err.Error()))
			return
		}
		if err == nil {
			err = s.opener.OpenFile(page)
		}
	case "editor":
		err = s.opener.OpenInEditor(in.Editor, dir)
		if errors.Is(err, opener.ErrUnknownEditor) {
			fail(w, badRequest(err.Error()))
			return
		}
	default:
		fail(w, badRequest("unknown target; use folder, browser or editor"))
		return
	}
	if err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
