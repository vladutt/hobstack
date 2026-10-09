package web

import (
	"errors"
	"net/http"

	"foreman/internal/team"
)

// moveTask: drag & drop pe board (sau "Move to…"): actiunea coloanei tinta.
func (s *Server) moveTask(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		fail(w, err)
		return
	}
	var in struct {
		To     string `json:"to"`
		Reason string `json:"reason"`
	}
	if err := decode(r, &in); err != nil {
		fail(w, err)
		return
	}
	err = s.team.MoveTask(id, in.To, in.Reason)
	var me team.MoveError
	switch {
	case errors.As(err, &me):
		writeErr(w, http.StatusConflict, me.Error())
	case errors.Is(err, team.ErrReasonRequired):
		fail(w, badRequest(err.Error()))
	case err != nil:
		fail(w, err)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

// deleteTask: stergerea din board = arhivare (Restore o anuleaza).
func (s *Server) deleteTask(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		fail(w, err)
		return
	}
	n, err := s.team.ArchiveTask(id)
	switch {
	case errors.Is(err, team.ErrArchived):
		writeErr(w, http.StatusConflict, "the task is already deleted")
	case err != nil:
		fail(w, err)
	default:
		writeJSON(w, http.StatusOK, map[string]int{"deleted": n})
	}
}

func (s *Server) restoreTask(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		fail(w, err)
		return
	}
	err = s.team.RestoreTask(id)
	var re team.RestoreError
	switch {
	case errors.As(err, &re):
		writeErr(w, http.StatusConflict, re.Error())
	case err != nil:
		fail(w, err)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

// listArchived: panoul "Deleted" de pe board.
func (s *Server) listArchived(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		fail(w, err)
		return
	}
	list, err := s.st.ListArchivedRoots(id)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"tasks": list})
}
