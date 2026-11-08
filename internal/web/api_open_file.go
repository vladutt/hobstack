package web

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"hobstack/internal/config"
)

// Fisierele atasate (pe task si in chat) se deschid pe masina utilizatorului: Open cu aplicatia implicita,
// Reveal in Finder / Explorer. Se deschide originalul din proiect cat timp exista (agentul il poate schimba),
// altfel copia pastrata de HobStack.

// openableExt: tipurile care se deschid cu Open. Restul (scripturi, executabile, .app) doar Reveal: un fisier
// facut de agent nu porneste nimic la un clic.
var openableExt = map[string]bool{
	".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".webp": true, ".avif": true, ".bmp": true, ".svg": true,
	".pdf": true, ".txt": true, ".md": true, ".csv": true, ".tsv": true, ".json": true, ".log": true,
	".html": true, ".htm": true, ".docx": true, ".xlsx": true, ".pptx": true, ".odt": true, ".ods": true,
	".mp4": true, ".webm": true, ".mov": true, ".mp3": true, ".wav": true,
}

// CanOpen: tipul fisierului se poate deschide cu Open (si UI-ul arata butonul).
func CanOpen(name string) bool { return openableExt[strings.ToLower(filepath.Ext(name))] }

// localFile: originalul daca e inca un fisier obisnuit, altfel copia (original = false).
func localFile(source, copyPath string) (path string, original bool) {
	if source != "" {
		if info, err := os.Stat(source); err == nil && info.Mode().IsRegular() {
			return source, true
		}
	}
	return copyPath, false
}

// openTaskFile: POST /api/task-files/{id}/open si /reveal.
func (s *Server) openTaskFile(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		fail(w, err)
		return
	}
	tf, err := s.st.GetTaskFile(id)
	if err != nil {
		fail(w, err)
		return
	}
	s.openLocal(w, r, tf.Name, tf.Source, filepath.Join(config.TaskFilesDir(s.dirs.Projects, tf.ProjectID, tf.TaskID), tf.Stored))
}

// openAttachment: POST /api/files/{id}/open si /reveal (fisierele din chat si ale proiectului).
func (s *Server) openAttachment(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		fail(w, err)
		return
	}
	a, err := s.st.GetAttachment(id)
	if err != nil {
		fail(w, err)
		return
	}
	s.openLocal(w, r, a.Name, a.Source, filepath.Join(config.ProjectFilesDir(s.dirs.Projects, a.ProjectID), a.Stored))
}

func (s *Server) openLocal(w http.ResponseWriter, r *http.Request, name, source, copyPath string) {
	if s.opener == nil {
		writeErr(w, http.StatusNotImplemented, "opening is not available on this system")
		return
	}
	path, original := localFile(source, copyPath)
	if _, err := os.Stat(path); err != nil {
		writeErr(w, http.StatusNotFound, "the file is missing from disk")
		return
	}
	reveal := strings.HasSuffix(r.URL.Path, "/reveal")
	if !reveal && !CanOpen(name) {
		writeErr(w, http.StatusBadRequest, "this kind of file only opens from its folder (Show in folder)")
		return
	}
	if reveal {
		err := s.opener.Reveal(path)
		if err != nil {
			fail(w, err)
			return
		}
	} else if err := s.opener.OpenFile(path); err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"original": original})
}
