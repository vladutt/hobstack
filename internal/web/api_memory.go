package web

import (
	"errors"
	"net/http"

	"hobstack/internal/store"
)

// API-ul memoriei proiectului (tab-ul Memory). Utilizatorul poate schimba si intrarile blocate.
// Spec: docs/superpowers/specs/2026-11-27-hobstack-project-memory-design.md

// userMemory: autorul scrierilor facute din UI.
func userMemory(key, summary, body string, taskID int64) store.MemoryWrite {
	return store.MemoryWrite{Key: key, Summary: summary, Body: body, AuthorName: store.UserFileAuthor, TaskID: taskID}
}

// memoryFail: erorile memoriei cu codul potrivit, restul prin fail.
func memoryFail(w http.ResponseWriter, err error) {
	var in store.MemoryInputError
	switch {
	case errors.As(err, &in):
		writeErr(w, http.StatusBadRequest, in.Error())
	case errors.Is(err, store.ErrMemoryExists):
		writeErr(w, http.StatusConflict, err.Error())
	case errors.Is(err, store.ErrMemoryFull):
		writeErr(w, http.StatusBadRequest, err.Error())
	default:
		fail(w, err)
	}
}

// memoryOf citeste intrarea din {id}; la eroare a raspuns deja.
func (s *Server) memoryOf(w http.ResponseWriter, r *http.Request) (store.Memory, bool) {
	id, err := pathID(r)
	if err != nil {
		fail(w, err)
		return store.Memory{}, false
	}
	m, err := s.st.GetMemory(id)
	if err != nil {
		fail(w, err)
		return m, false
	}
	return m, true
}

func (s *Server) memoryChanged(m store.Memory) {
	s.hub.Publish(m.ProjectID, "memory", map[string]int64{"id": m.ID})
}

// GET /api/projects/{id}/memory[?deleted=1]
func (s *Server) listMemory(w http.ResponseWriter, r *http.Request) {
	pid, err := pathID(r)
	if err != nil {
		fail(w, err)
		return
	}
	if _, err := s.st.GetProject(pid); err != nil {
		fail(w, err)
		return
	}
	list, err := s.st.ListMemory(pid, r.URL.Query().Get("deleted") == "1")
	if err != nil {
		fail(w, err)
		return
	}
	if list == nil {
		list = []store.Memory{}
	}
	writeJSON(w, http.StatusOK, list)
}

// POST /api/projects/{id}/memory {key, summary, body, locked}
func (s *Server) createMemory(w http.ResponseWriter, r *http.Request) {
	pid, err := pathID(r)
	if err != nil {
		fail(w, err)
		return
	}
	var in struct {
		Key, Summary, Body string
		Locked             bool
	}
	if err := decode(r, &in); err != nil {
		fail(w, err)
		return
	}
	if _, err := s.st.GetProject(pid); err != nil {
		fail(w, err)
		return
	}
	m, err := s.st.CreateMemory(pid, userMemory(in.Key, in.Summary, in.Body, 0), in.Locked)
	if err != nil {
		memoryFail(w, err)
		return
	}
	s.memoryChanged(m)
	writeJSON(w, http.StatusCreated, m)
}

// PUT /api/memory/{id} {summary, body}: cheia nu se schimba; task-ul intrarii ramane.
func (s *Server) updateMemory(w http.ResponseWriter, r *http.Request) {
	m, ok := s.memoryOf(w, r)
	if !ok {
		return
	}
	var in struct{ Summary, Body string }
	if err := decode(r, &in); err != nil {
		fail(w, err)
		return
	}
	if m.DeletedAt != nil {
		writeErr(w, http.StatusConflict, "restore the entry before editing it")
		return
	}
	m, _, _, err := s.st.SetMemory(m.ProjectID, userMemory(m.Key, in.Summary, in.Body, m.TaskID), true)
	if err != nil {
		memoryFail(w, err)
		return
	}
	s.memoryChanged(m)
	writeJSON(w, http.StatusOK, m)
}

// PUT /api/memory/{id}/lock {locked}
func (s *Server) lockMemory(w http.ResponseWriter, r *http.Request) {
	m, ok := s.memoryOf(w, r)
	if !ok {
		return
	}
	var in struct{ Locked bool }
	if err := decode(r, &in); err != nil {
		fail(w, err)
		return
	}
	if m.DeletedAt != nil {
		writeErr(w, http.StatusConflict, "the entry is deleted")
		return
	}
	if err := s.st.LockMemory(m.ID, in.Locked); err != nil {
		fail(w, err)
		return
	}
	s.memoryChanged(m)
	w.WriteHeader(http.StatusNoContent)
}

// DELETE /api/memory/{id}
func (s *Server) deleteMemory(w http.ResponseWriter, r *http.Request) {
	m, ok := s.memoryOf(w, r)
	if !ok {
		return
	}
	if err := s.st.ForgetMemory(m.ID, userMemory("", "", "", 0), "", true); err != nil {
		fail(w, err)
		return
	}
	s.memoryChanged(m)
	w.WriteHeader(http.StatusNoContent)
}

// POST /api/memory/{id}/restore
func (s *Server) restoreMemory(w http.ResponseWriter, r *http.Request) {
	m, ok := s.memoryOf(w, r)
	if !ok {
		return
	}
	if err := s.st.RestoreMemory(m.ID, userMemory("", "", "", 0)); err != nil {
		memoryFail(w, err)
		return
	}
	s.memoryChanged(m)
	w.WriteHeader(http.StatusNoContent)
}

// GET /api/memory/{id}/history
func (s *Server) memoryHistory(w http.ResponseWriter, r *http.Request) {
	m, ok := s.memoryOf(w, r)
	if !ok {
		return
	}
	h, err := s.st.MemoryHistory(m.ID)
	if err != nil {
		fail(w, err)
		return
	}
	if h == nil {
		h = []store.MemoryVersion{}
	}
	writeJSON(w, http.StatusOK, h)
}
