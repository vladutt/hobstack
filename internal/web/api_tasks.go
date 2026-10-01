package web

import "net/http"

// Butoanele de pe task-urile blocate: utilizatorul decide.
func (s *Server) acceptTask(w http.ResponseWriter, r *http.Request) {
	s.taskAction(w, r, s.team.AcceptBlocked)
}

func (s *Server) unblockTask(w http.ResponseWriter, r *http.Request) {
	s.taskAction(w, r, s.team.Unblock)
}

// nudgeTask impinge manual agentul de care asteapta un task blocat.
func (s *Server) nudgeTask(w http.ResponseWriter, r *http.Request) {
	s.taskAction(w, r, func(id int64) error { return s.team.Nudge(id, s.team.Now()) })
}

func (s *Server) taskAction(w http.ResponseWriter, r *http.Request, action func(int64) error) {
	id, err := pathID(r)
	if err != nil {
		fail(w, err)
		return
	}
	if err := action(id); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
