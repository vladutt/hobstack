package web

import (
	"errors"
	"net/http"
	"strings"

	"foreman/internal/store"
)

func (s *Server) answerQuestion(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		fail(w, err)
		return
	}
	var in struct {
		Text string `json:"text"`
	}
	if err := decode(r, &in); err != nil {
		fail(w, err)
		return
	}
	text := strings.TrimSpace(in.Text)
	if text == "" {
		fail(w, badRequest("the answer is empty"))
		return
	}
	if err := maxLen("answer", text, 4000); err != nil {
		fail(w, err)
		return
	}
	questionDone(w, s.team.AnswerQuestion(id, text))
}

func (s *Server) dismissQuestion(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		fail(w, err)
		return
	}
	questionDone(w, s.team.DismissQuestion(id))
}

// questionDone traduce rezultatul inchiderii unei intrebari in raspuns HTTP.
func questionDone(w http.ResponseWriter, err error) {
	switch {
	case err == nil:
		w.WriteHeader(http.StatusNoContent)
	case errors.Is(err, store.ErrConflict):
		writeErr(w, http.StatusConflict, "the question was already answered or dismissed")
	default:
		fail(w, err)
	}
}
