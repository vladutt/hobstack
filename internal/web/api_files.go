package web

import (
	"errors"
	"fmt"
	"io"
	"log"
	"mime"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"hobstack/internal/config"
	"hobstack/internal/picker"
	"hobstack/internal/store"
)

const (
	maxFileSize    = 50 << 20  // per fisier
	maxRequestSize = 200 << 20 // per request
)

// readUploads parseaza request-ul multipart si intoarce fisierele din campul
// "files", dupa ce verifica marimea fiecaruia.
func (s *Server) readUploads(w http.ResponseWriter, r *http.Request) ([]*multipart.FileHeader, error) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestSize)
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		return nil, badRequest("cannot read the request (max 200 MB): " + err.Error())
	}
	files := r.MultipartForm.File["files"]
	for _, fh := range files {
		if fh.Size > maxFileSize {
			return nil, badRequest(fmt.Sprintf("file %s exceeds 50 MB", store.CleanFileName(fh.Filename)))
		}
	}
	return files, nil
}

// saveUploads copie fisierele pe disc si le inregistreaza in baza de date.
// Daca ceva esueaza, ce s-a salvat pana atunci se sterge.
func (s *Server) saveUploads(projectID int64, kind string, files []*multipart.FileHeader) ([]store.Attachment, error) {
	dir := config.ProjectFilesDir(s.dirs.Projects, projectID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	saved := []store.Attachment{}
	for _, fh := range files {
		a, err := s.saveOne(dir, projectID, kind, fh)
		if err != nil {
			s.discardUploads(saved)
			return nil, err
		}
		saved = append(saved, a)
	}
	return saved, nil
}

func (s *Server) saveOne(dir string, projectID int64, kind string, fh *multipart.FileHeader) (store.Attachment, error) {
	a := store.Attachment{ProjectID: projectID, Kind: kind, Name: store.CleanFileName(fh.Filename)}
	a.Stored = store.StoredName(a.Name)
	a.Mime = fh.Header.Get("Content-Type")
	if a.Mime == "" || a.Mime == "application/octet-stream" {
		a.Mime = mime.TypeByExtension(filepath.Ext(a.Name))
		if a.Mime == "" {
			a.Mime = "application/octet-stream"
		}
	}
	a.Mime = normalizeMime(a.Mime)
	src, err := fh.Open()
	if err != nil {
		return a, err
	}
	defer src.Close()
	path := filepath.Join(dir, a.Stored)
	dst, err := os.Create(path)
	if err != nil {
		return a, err
	}
	n, err := io.Copy(dst, src)
	if cerr := dst.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(path)
		return a, err
	}
	a.Size = n
	if err := s.st.AddAttachment(&a); err != nil {
		os.Remove(path)
		return a, err
	}
	return a, nil
}

// normalizeMime adauga charset la text/plain fara parametri, ca browserul
// sa nu ghiceasca gresit codarea (romana cu diacritice).
func normalizeMime(m string) string {
	if strings.EqualFold(strings.TrimSpace(m), "text/plain") {
		return "text/plain; charset=utf-8"
	}
	return m
}

// discardUploads sterge fisierele si randurile create de un request esuat.
func (s *Server) discardUploads(list []store.Attachment) {
	for _, a := range list {
		if err := s.removeFile(a); err != nil {
			log.Printf("cleaning up attachment %d: %v", a.ID, err)
		}
	}
}

// removeFile sterge fisierul de pe disc (un fisier deja lipsa se ignora), apoi randul.
func (s *Server) removeFile(a store.Attachment) error {
	path := filepath.Join(config.ProjectFilesDir(s.dirs.Projects, a.ProjectID), a.Stored)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		log.Printf("cannot delete %s: %v", path, err)
	}
	return s.st.DeleteAttachment(a.ID)
}

func (s *Server) listFiles(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		fail(w, err)
		return
	}
	if _, err := s.st.GetProject(id); err != nil {
		fail(w, err)
		return
	}
	list, err := s.st.ListProjectFiles(id)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) uploadFiles(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		fail(w, err)
		return
	}
	if _, err := s.st.GetProject(id); err != nil {
		fail(w, err)
		return
	}
	files, err := s.readUploads(w, r)
	if err != nil {
		fail(w, err)
		return
	}
	if len(files) == 0 {
		fail(w, badRequest("no file"))
		return
	}
	saved, err := s.saveUploads(id, store.KindProjectFile, files)
	if err != nil {
		fail(w, err)
		return
	}
	s.hub.Publish(id, "files", map[string]int64{"id": id})
	writeJSON(w, http.StatusCreated, saved)
}

// inlineTypes se pot afisa direct in browser; restul se descarca.
var inlineTypes = map[string]bool{
	"image/png": true, "image/jpeg": true, "image/gif": true, "image/webp": true,
	"application/pdf": true, "text/plain": true,
}

func (s *Server) serveFile(w http.ResponseWriter, r *http.Request) {
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
	f, err := os.Open(filepath.Join(config.ProjectFilesDir(s.dirs.Projects, a.ProjectID), a.Stored))
	if err != nil {
		if os.IsNotExist(err) {
			writeErr(w, http.StatusNotFound, "the file is missing from disk")
			return
		}
		fail(w, err)
		return
	}
	defer f.Close()
	serveStored(w, r, f, a.Name, a.Mime, a.CreatedAt, r.URL.Query().Get("download") == "1")
}

// serveStored trimite un fisier salvat de noi (urcat de utilizator sau predat de un agent).
// Inline doar tipurile din inlineTypes; restul (HTML, SVG...) se descarca, in sandbox,
// ca un fisier scris de un agent sa nu ruleze pe originea aplicatiei.
func serveStored(w http.ResponseWriter, r *http.Request, f io.ReadSeeker, name, mimeType string, mod time.Time, download bool) {
	base, _, _ := strings.Cut(mimeType, ";")
	base = strings.TrimSpace(base)
	disposition := "attachment"
	if inlineTypes[base] && !download {
		disposition = "inline"
	}
	w.Header().Set("Content-Type", normalizeMime(mimeType)) // si fisierele salvate mai demult
	w.Header().Set("Cross-Origin-Resource-Policy", "same-origin")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	// viewerul de PDF din Chrome nu merge sub sandbox; restul tipurilor raman in sandbox
	if base != "application/pdf" {
		w.Header().Set("Content-Security-Policy", "sandbox")
	}
	w.Header().Set("Content-Disposition", mime.FormatMediaType(disposition, map[string]string{"filename": name}))
	http.ServeContent(w, r, name, mod, f)
}

func (s *Server) deleteFile(w http.ResponseWriter, r *http.Request) {
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
	if err := s.removeFile(a); err != nil && !errors.Is(err, store.ErrNotFound) {
		fail(w, err)
		return
	}
	s.hub.Publish(a.ProjectID, "files", map[string]int64{"id": a.ProjectID})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) pickFolder(w http.ResponseWriter, r *http.Request) {
	if !s.canPick || s.pick == nil {
		writeErr(w, http.StatusNotImplemented, "the folder picker is not available on this system")
		return
	}
	path, err := s.pick(r.Context())
	switch {
	case errors.Is(err, picker.ErrCanceled):
		w.WriteHeader(http.StatusNoContent)
	case err != nil:
		fail(w, err) // logat si intors ca 500
	default:
		writeJSON(w, http.StatusOK, map[string]string{"path": path})
	}
}
