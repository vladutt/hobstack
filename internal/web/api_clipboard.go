package web

import (
	"context"
	"io"
	"log"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"time"
)

// Fisierele din clipboard-ul sistemului: Firefox da paginii doar primul fisier la paste,
// asa ca pagina le cere serverului (local) pe toate. Ambele rute sunt POST cu JSON: o pagina
// straina nu le poate chema fara preflight (vezi guard), deci nu poate citi fisierele.

type clipFile struct {
	Path   string `json:"path"`
	Name   string `json:"name"`
	Size   int64  `json:"size"`
	TooBig bool   `json:"tooBig"` // peste 50 MB, ca la upload: pagina nu-l mai cere
}

// clipPaths: caile din clipboard, cu timeout (osascript / PowerShell pot sta)
func (s *Server) clipPaths(ctx context.Context) []string {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	paths, err := s.clip(ctx)
	if err != nil {
		log.Printf("clipboard files: %v", err)
		return nil
	}
	return paths
}

// clipboardFiles: fisierele obisnuite din clipboard (folderele si ce nu exista se sar)
func (s *Server) clipboardFiles(w http.ResponseWriter, r *http.Request) {
	files := []clipFile{}
	for _, p := range s.clipPaths(r.Context()) {
		fi, err := os.Stat(p)
		if err != nil || !fi.Mode().IsRegular() {
			continue
		}
		files = append(files, clipFile{Path: p, Name: filepath.Base(p), Size: fi.Size(), TooBig: fi.Size() > maxFileSize})
	}
	writeJSON(w, http.StatusOK, map[string]any{"files": files})
}

// clipboardFile: continutul unui fisier, doar daca e in clipboard chiar acum
func (s *Server) clipboardFile(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Path string `json:"path"`
	}
	if err := decode(r, &in); err != nil {
		fail(w, err)
		return
	}
	if in.Path == "" || !slices.Contains(s.clipPaths(r.Context()), in.Path) {
		writeErr(w, http.StatusNotFound, "this file is no longer on the clipboard")
		return
	}
	f, err := os.Open(in.Path)
	if err != nil {
		writeErr(w, http.StatusNotFound, "cannot read the file")
		return
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil || !fi.Mode().IsRegular() {
		writeErr(w, http.StatusNotFound, "not a regular file")
		return
	}
	if fi.Size() > maxFileSize {
		writeErr(w, http.StatusBadRequest, "file exceeds 50 MB")
		return
	}
	ct := mime.TypeByExtension(filepath.Ext(in.Path))
	if ct == "" {
		ct = "application/octet-stream"
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Content-Length", strconv.FormatInt(fi.Size(), 10))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	io.Copy(w, io.LimitReader(f, maxFileSize))
}
