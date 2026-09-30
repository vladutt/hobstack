package web

import (
	"errors"
	"net/http"
	"strings"

	"foreman/internal/store"
	"foreman/internal/team"
)

func (s *Server) saveStages(w http.ResponseWriter, r *http.Request) {
	pid, err := pathID(r)
	if err != nil {
		fail(w, err)
		return
	}
	if _, err := s.st.GetProject(pid); err != nil {
		fail(w, err)
		return
	}
	var in struct {
		Stages []store.Stage `json:"stages"`
	}
	if err := decode(r, &in); err != nil {
		fail(w, err)
		return
	}
	agents, err := s.st.ListAgents(pid)
	if err != nil {
		fail(w, err)
		return
	}
	inProject := map[int64]bool{}
	for _, a := range agents {
		inProject[a.ID] = true
	}
	if len(in.Stages) > 12 {
		fail(w, badRequest("maxim 12 etape"))
		return
	}
	seen := map[string]bool{}
	for i := range in.Stages {
		st := &in.Stages[i]
		st.Name = strings.TrimSpace(st.Name)
		if st.Name == "" {
			fail(w, badRequest("fiecare etapa are nevoie de nume"))
			return
		}
		if err := maxLen("numele etapei", st.Name, 40); err != nil {
			fail(w, err)
			return
		}
		key := strings.ToLower(st.Name)
		if seen[key] {
			fail(w, badRequest("doua etape cu numele "+st.Name))
			return
		}
		seen[key] = true
		if st.Kind != store.KindWork && st.Kind != store.KindCheck {
			fail(w, badRequest("tip de etapa necunoscut: "+st.Kind))
			return
		}
		for _, id := range st.AgentIDs {
			if !inProject[id] {
				fail(w, badRequest("un agent ales nu e in acest proiect"))
				return
			}
		}
	}
	saved, err := s.st.SaveStages(pid, in.Stages)
	var inUse store.StageInUseError
	if errors.As(err, &inUse) {
		writeErr(w, http.StatusConflict, inUse.Error())
		return
	}
	if err != nil {
		fail(w, err)
		return
	}
	s.hub.Publish(pid, "stages", map[string]int64{"id": pid})
	s.team.Kick(pid)
	writeJSON(w, http.StatusOK, saved)
}

func (s *Server) createUserTask(w http.ResponseWriter, r *http.Request) {
	pid, err := pathID(r)
	if err != nil {
		fail(w, err)
		return
	}
	var in struct {
		Title       string `json:"title"`
		Description string `json:"description"`
		AssigneeID  int64  `json:"assigneeId"` // 0 = primul liber (doar in proiectele cu etape)
	}
	if err := decode(r, &in); err != nil {
		fail(w, err)
		return
	}
	title := strings.TrimSpace(in.Title)
	if title == "" {
		fail(w, badRequest("titlul e obligatoriu"))
		return
	}
	for _, err := range []error{maxLen("titlul", title, 200), maxLen("descrierea", in.Description, 10000)} {
		if err != nil {
			fail(w, err)
			return
		}
	}
	task, err := s.team.UserCreateTask(pid, title, strings.TrimSpace(in.Description), in.AssigneeID)
	var ae team.AssignError
	if errors.As(err, &ae) {
		fail(w, badRequest(ae.Error()))
		return
	}
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, task)
}

func (s *Server) decideTask(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		fail(w, err)
		return
	}
	var in struct {
		Action   string `json:"action"`
		Feedback string `json:"feedback"`
	}
	if err := decode(r, &in); err != nil {
		fail(w, err)
		return
	}
	if in.Action != "accept" && in.Action != "send_back" {
		fail(w, badRequest("action trebuie sa fie accept sau send_back"))
		return
	}
	err = s.team.UserDecide(id, in.Action == "accept", in.Feedback)
	switch {
	case errors.Is(err, team.ErrNotUserDecision):
		writeErr(w, http.StatusConflict, err.Error())
	case err != nil && strings.Contains(err.Error(), "scrie ce trebuie"):
		fail(w, badRequest(err.Error()))
	case err != nil:
		fail(w, err)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

// assignTask: utilizatorul muta un task (in asteptare sau blocat) la alt agent.
func (s *Server) assignTask(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		fail(w, err)
		return
	}
	var in struct {
		AgentID int64 `json:"agentId"`
	}
	if err := decode(r, &in); err != nil {
		fail(w, err)
		return
	}
	err = s.team.AssignTask(id, in.AgentID)
	var ae team.AssignError
	switch {
	case errors.As(err, &ae):
		fail(w, badRequest(ae.Error()))
	case errors.Is(err, team.ErrTaskBusy):
		writeErr(w, http.StatusConflict, err.Error())
	case err != nil:
		fail(w, err)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}
