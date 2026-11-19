package web

import (
	"net/http"

	"hobstack/internal/appprefs"
	"hobstack/internal/notify"
)

// setProjectNotify porneste / opreste notificarile de sistem ale unui proiect (formularul proiectului).
func (s *Server) setProjectNotify(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		fail(w, err)
		return
	}
	var in struct {
		On bool `json:"on"`
	}
	if err := decode(r, &in); err != nil {
		fail(w, err)
		return
	}
	if err := s.st.SetNotify(id, in.On); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// setAppNotify schimba comutatoarele globale (Settings > App); un camp lipsa ramane cum era.
func (s *Server) setAppNotify(w http.ResponseWriter, r *http.Request) {
	if s.app == nil {
		writeErr(w, http.StatusServiceUnavailable, "not available")
		return
	}
	var in struct {
		Done  *bool `json:"done"`
		Needs *bool `json:"needs"`
	}
	if err := decode(r, &in); err != nil {
		fail(w, err)
		return
	}
	err := appprefs.Update(s.app.root, func(p *appprefs.Prefs) {
		if in.Done != nil {
			p.NotifyDone = in.Done
		}
		if in.Needs != nil {
			p.NotifyNeeds = in.Needs
		}
	})
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, s.appState())
}

// NotifySettings: comutatoarele globale; fara Settings > App (teste) ambele sunt pornite.
func (s *Server) NotifySettings() notify.Settings {
	if s.app == nil {
		return notify.Settings{Done: true, Needs: true}
	}
	d, n := appprefs.Load(s.app.root).Notify()
	return notify.Settings{Done: d, Needs: n}
}
