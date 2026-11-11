package web

import (
	"errors"
	"net/http"

	"hobstack/internal/team"
)

// stopAgent: Stop in chat-ul agentului, fara Stop team (opreste run-ul lui activ).
func (s *Server) stopAgent(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		fail(w, err)
		return
	}
	stopReply(w, s.team.StopAgent(id))
}

// stopTask: Stop pe card / in modala; task-ul asteapta in Queued pana la Resume.
func (s *Server) stopTask(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		fail(w, err)
		return
	}
	stopReply(w, s.team.StopTask(id))
}

func (s *Server) resumeTask(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		fail(w, err)
		return
	}
	stopReply(w, s.team.ResumeTask(id))
}

func stopReply(w http.ResponseWriter, err error) {
	var se team.StopError
	switch {
	case errors.As(err, &se):
		writeErr(w, http.StatusConflict, se.Error())
	case err != nil:
		fail(w, err)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}
