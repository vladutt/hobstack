package web

import (
	"log"
	"net/http"
	"path"
	"path/filepath"
	"strconv"

	"hobstack/internal/codefiles"
	"hobstack/internal/store"
)

// maxCodeHits: cate fisiere intoarce o cautare dupa @ in chat.
const maxCodeHits = 30

type codeFile struct {
	Path   string `json:"path"`   // ce se scrie dupa @: relativa in folderul agentului, altfel absoluta
	Name   string `json:"name"`   // numele fisierului
	Folder string `json:"folder"` // numele folderului proiectului
}

// codeFiles: fisierele de cod ale proiectului pentru @ in chat (?q=cautarea, ?agent=agentul cu care vorbesti).
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
	out := []codeFile{}
	for _, i := range codefiles.SearchIdx(rels, r.URL.Query().Get("q"), maxCodeHits) {
		f, rel := owner[i], rels[i]
		show := rel
		if f.ID != home.ID {
			show = filepath.Join(f.Path, filepath.FromSlash(rel))
		}
		out = append(out, codeFile{Path: show, Name: path.Base(rel), Folder: f.Name})
	}
	writeJSON(w, http.StatusOK, map[string]any{"files": out})
}
