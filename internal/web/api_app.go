package web

import (
	"net/http"
	"runtime"

	"hobstack/internal/autostart"
)

// appControl: pornirea la login si oprirea aplicatiei din Settings (HobStack ruleaza fara terminal).
type appControl struct {
	exe, home, logFile string
	root               string // ~/.hobstack: marcajul alegerii pentru pornirea la login
	quit               func() // opreste aplicatia (contextul din main)
}

// SetApp activeaza Settings > App.
func (s *Server) SetApp(exe, home, root, logFile string, quit func()) {
	s.app = &appControl{exe: exe, home: home, root: root, logFile: logFile, quit: quit}
}

func (s *Server) appState() map[string]any {
	a := s.app
	return map[string]any{
		"os":        runtime.GOOS,
		"autostart": autostart.Supported() && autostart.Enabled(a.home, a.exe),
		"supported": autostart.Supported(),
		"exe":       a.exe,
		"logFile":   a.logFile,
		"running":   max(s.runs.ActiveCount(), s.team.ActiveCount()),
	}
}

func (s *Server) getApp(w http.ResponseWriter, r *http.Request) {
	if s.app == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "not available"})
		return
	}
	writeJSON(w, http.StatusOK, s.appState())
}

func (s *Server) setAutostart(w http.ResponseWriter, r *http.Request) {
	if s.app == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "not available"})
		return
	}
	var in struct {
		Enabled bool `json:"enabled"`
	}
	if err := decode(r, &in); err != nil {
		fail(w, err)
		return
	}
	a := s.app
	var err error
	if in.Enabled {
		err = autostart.Enable(a.home, a.exe, a.logFile)
	} else {
		err = autostart.Disable(a.home, a.exe)
	}
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	autostart.MarkChosen(a.root) // alegerea utilizatorului ramane (pornirea implicita nu o mai schimba)
	writeJSON(w, http.StatusOK, s.appState())
}

// quitApp opreste HobStack. Cu agenti care ruleaza cere force (run-urile lor se opresc si task-urile revin
// in coada la urmatoarea pornire).
func (s *Server) quitApp(w http.ResponseWriter, r *http.Request) {
	if s.app == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "not available"})
		return
	}
	var in struct {
		Force bool `json:"force"`
	}
	if err := decodeOptional(r, &in); err != nil {
		fail(w, err)
		return
	}
	if n := max(s.runs.ActiveCount(), s.team.ActiveCount()); n > 0 && !in.Force {
		writeJSON(w, http.StatusConflict, map[string]any{"error": "agents are running", "running": n})
		return
	}
	w.WriteHeader(http.StatusNoContent)
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
	go s.app.quit()
}
