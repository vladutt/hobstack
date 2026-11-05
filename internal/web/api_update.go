package web

import (
	"context"
	"errors"
	"net/http"

	"hobstack/internal/selfupdate"
)

// SetUpdater activeaza butonul de update; ctx traieste cat aplicatia (update-ul nu se opreste cu cererea HTTP).
func (s *Server) SetUpdater(u *selfupdate.Updater, ctx context.Context) {
	s.updater = u
	s.updaterCtx = ctx
}

func (s *Server) getUpdate(w http.ResponseWriter, r *http.Request) {
	if s.updater == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "updates are not available"})
		return
	}
	writeJSON(w, http.StatusOK, s.updater.Status())
}

// checkUpdate face git fetch acum (poate dura cateva secunde) si intoarce starea noua.
func (s *Server) checkUpdate(w http.ResponseWriter, r *http.Request) {
	if s.updater == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "updates are not available"})
		return
	}
	s.updater.Check(r.Context()) // eroarea ramane in Status.CheckError
	writeJSON(w, http.StatusOK, s.updater.Status())
}

func (s *Server) startUpdate(w http.ResponseWriter, r *http.Request) {
	if s.updater == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "updates are not available"})
		return
	}
	if err := s.updater.Start(s.updaterCtx); err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, selfupdate.ErrBusy) {
			status = http.StatusConflict
		}
		writeJSON(w, status, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusAccepted, s.updater.Status())
}

func (s *Server) cancelUpdate(w http.ResponseWriter, r *http.Request) {
	if s.updater == nil || !s.updater.Cancel() {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "the update is not waiting for agents"})
		return
	}
	writeJSON(w, http.StatusOK, s.updater.Status())
}
