package web

import (
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"hobstack/internal/config"
	"hobstack/internal/store"
)

// Caile din chat (web/src/App.tsx, /Users/.../site/index.html, Checkout.tsx:42) devin clicabile: clic = Show in
// Finder / Explorer (un folder se deschide). Doar cai din folderele proiectului si din fisierele lui de pe disc;
// restul discului nu se poate cere de aici.

const (
	maxPathChecks = 200
	maxPathLen    = 1024
)

// lineSuffix: ":42" sau ":42:7" de la finalul unei cai (linia si coloana)
var lineSuffix = regexp.MustCompile(`:\d+(:\d+)?$`)

// pathRoots: folderele proiectului (primul = folderul agentului), apoi fisierele proiectului si de pe task-uri
func (s *Server) pathRoots(p store.Project, home store.Folder) (folders []store.Folder, extra []string) {
	folders = append(folders, home)
	for _, f := range p.Folders {
		if f.ID != home.ID {
			folders = append(folders, f)
		}
	}
	if s.dirs.Projects != "" {
		extra = []string{config.ProjectFilesDir(s.dirs.Projects, p.ID), config.ProjectTaskFilesDir(s.dirs.Projects, p.ID)}
	}
	return folders, extra
}

// resolvePath gaseste calea pe disc: absoluta (sau ~/) in folderele proiectului, relativa la folderul agentului
// (apoi la celelalte foldere), sau un nume / o coada de cale care se potriveste unui singur fisier din proiect.
func (s *Server) resolvePath(p store.Project, home store.Folder, raw string) (abs string, dir bool, ok bool) {
	raw = strings.TrimPrefix(strings.TrimSpace(raw), "@")
	raw = lineSuffix.ReplaceAllString(raw, "")
	if raw == "" || len(raw) > maxPathLen || strings.ContainsRune(raw, 0) {
		return "", false, false
	}
	if rest, found := strings.CutPrefix(raw, "~/"); found {
		if h, err := os.UserHomeDir(); err == nil {
			raw = filepath.Join(h, rest)
		}
	}
	folders, extra := s.pathRoots(p, home)
	roots := extra
	for _, f := range folders {
		roots = append(roots, f.Path)
	}
	stat := func(path string) (string, bool, bool) {
		info, err := os.Stat(path)
		if err != nil {
			return "", false, false
		}
		return path, info.IsDir(), true
	}
	if filepath.IsAbs(raw) {
		raw = filepath.Clean(raw)
		for _, r := range roots {
			if r == "" {
				continue
			}
			if rel, err := filepath.Rel(r, raw); err == nil && (rel == "." || filepath.IsLocal(rel)) {
				return stat(raw)
			}
		}
		return "", false, false
	}
	rel := filepath.Clean(filepath.FromSlash(strings.TrimPrefix(raw, "./")))
	if !filepath.IsLocal(rel) {
		return "", false, false
	}
	for _, f := range folders {
		if f.Path == "" {
			continue
		}
		if path, d, ok := stat(filepath.Join(f.Path, rel)); ok {
			return path, d, true
		}
	}
	// "Checkout.tsx" sau "pages/Checkout.tsx": un singur fisier din lista proiectului se termina asa
	want := filepath.ToSlash(rel)
	match := ""
	for _, f := range folders {
		files, err := s.codeCache.Files(f.Path)
		if err != nil {
			continue
		}
		for _, file := range files {
			if file == want || strings.HasSuffix(file, "/"+want) {
				if match != "" {
					return "", false, false // ambiguu
				}
				match = filepath.Join(f.Path, filepath.FromSlash(file))
			}
		}
	}
	if match == "" {
		return "", false, false
	}
	return stat(match)
}

// pathContext: proiectul si folderul agentului din chat (?agent / "agent" in body)
func (s *Server) pathContext(r *http.Request, agentID int64) (store.Project, store.Folder, error) {
	id, err := pathID(r)
	if err != nil {
		return store.Project{}, store.Folder{}, err
	}
	p, err := s.st.GetProject(id)
	if err != nil {
		return p, store.Folder{}, err
	}
	home := store.Folder{}
	if len(p.Folders) > 0 {
		home = p.Folders[0]
	}
	if agentID > 0 {
		if a, err := s.st.GetAgent(agentID); err == nil && a.ProjectID == p.ID {
			home = store.HomeFolder(a, p.Folders)
		}
	}
	return p, home, nil
}

// checkPaths: POST /api/projects/{id}/paths {paths, agent}: care din caile din chat exista (doar acelea devin link).
func (s *Server) checkPaths(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Paths []string `json:"paths"`
		Agent int64    `json:"agent"`
	}
	if err := decode(r, &in); err != nil {
		fail(w, err)
		return
	}
	if len(in.Paths) > maxPathChecks {
		in.Paths = in.Paths[:maxPathChecks]
	}
	p, home, err := s.pathContext(r, in.Agent)
	if err != nil {
		fail(w, err)
		return
	}
	found := map[string]any{}
	for _, raw := range in.Paths {
		if _, dir, ok := s.resolvePath(p, home, raw); ok {
			found[raw] = map[string]bool{"dir": dir}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"found": found})
}

// revealPath: POST /api/projects/{id}/reveal {path, agent}: fisierul selectat in Finder / Explorer, folderul deschis.
func (s *Server) revealPath(w http.ResponseWriter, r *http.Request) {
	if s.opener == nil {
		writeErr(w, http.StatusNotImplemented, "opening is not available on this system")
		return
	}
	var in struct {
		Path  string `json:"path"`
		Agent int64  `json:"agent"`
	}
	if err := decode(r, &in); err != nil {
		fail(w, err)
		return
	}
	p, home, err := s.pathContext(r, in.Agent)
	if err != nil {
		fail(w, err)
		return
	}
	abs, dir, ok := s.resolvePath(p, home, in.Path)
	if !ok {
		writeErr(w, http.StatusNotFound, "not found in the project folders")
		return
	}
	if dir {
		err = s.opener.OpenFolder(abs)
	} else {
		err = s.opener.Reveal(abs)
	}
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"path": abs, "dir": dir})
}
