package web

import (
	"errors"
	"io/fs"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"hobstack/internal/store"
)

// maxRawFile: cat de mare poate fi un fisier aratat ca preview in explorer-ul de cod (imagine, pagina HTML).
const maxRawFile = 50 << 20

// getCodeRaw: GET /api/projects/{id}/raw/{folder}/{path...}: fisierul ca atare, pentru preview in explorer-ul
// de cod (imagini, SVG, pagini HTML). Calea e in URL, nu in query, ca o pagina HTML sa-si gaseasca CSS-ul, JS-ul
// si pozele cu cai relative. Doar fisierele din lista folderului (ca la code-file), prin os.OpenRoot.
// Pagina ruleaza sub CSP sandbox fara allow-same-origin: are o origine opaca, deci nu poate citi API-ul HobStack.
func (s *Server) getCodeRaw(w http.ResponseWriter, r *http.Request) {
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
	fid, _ := strconv.ParseInt(r.PathValue("folder"), 10, 64)
	i := slices.IndexFunc(p.Folders, func(f store.Folder) bool { return f.ID == fid })
	if i < 0 {
		fail(w, store.ErrNotFound)
		return
	}
	folder := p.Folders[i]
	rel := r.PathValue("path")
	if rel == "" || strings.Contains(rel, "\\") || !filepath.IsLocal(filepath.FromSlash(rel)) {
		fail(w, badRequest("bad path"))
		return
	}
	if !s.inCodeList(folder.Path, rel) {
		fail(w, store.ErrNotFound)
		return
	}
	rt, err := os.OpenRoot(folder.Path)
	if err != nil {
		fail(w, store.ErrNotFound)
		return
	}
	defer rt.Close()
	f, err := rt.Open(filepath.FromSlash(rel))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			s.codeCache.Invalidate(folder.Path)
		}
		fail(w, store.ErrNotFound) // lipsa sau iesire din folder (symlink, ..)
		return
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil || !fi.Mode().IsRegular() {
		fail(w, store.ErrNotFound)
		return
	}
	if fi.Size() > maxRawFile {
		writeErr(w, http.StatusRequestEntityTooLarge, "the file is larger than 50 MB")
		return
	}
	ctype := mime.TypeByExtension(filepath.Ext(rel))
	if ctype == "" {
		ctype = "application/octet-stream"
	}
	w.Header().Set("Content-Type", ctype)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "no-store") // fisierul se schimba cat lucreaza agentii
	w.Header().Set("Content-Security-Policy", "sandbox allow-scripts allow-forms allow-popups allow-modals")
	http.ServeContent(w, r, filepath.Base(rel), fi.ModTime(), f)
}
