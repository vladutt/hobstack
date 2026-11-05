package web

import (
	"errors"
	"fmt"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"

	"hobstack/internal/store"
	"hobstack/internal/team"
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
		fail(w, badRequest("at most 12 stages"))
		return
	}
	seen := map[string]bool{}
	for i := range in.Stages {
		st := &in.Stages[i]
		st.Name = strings.TrimSpace(st.Name)
		if st.Name == "" {
			fail(w, badRequest("every stage needs a name"))
			return
		}
		if err := maxLen("stage name", st.Name, 40); err != nil {
			fail(w, err)
			return
		}
		key := strings.ToLower(st.Name)
		if seen[key] {
			fail(w, badRequest("two stages named "+st.Name))
			return
		}
		seen[key] = true
		if st.Kind != store.KindWork && st.Kind != store.KindCheck {
			fail(w, badRequest("unknown stage type: "+st.Kind))
			return
		}
		for _, id := range st.AgentIDs {
			if !inProject[id] {
				fail(w, badRequest("a chosen agent is not in this project"))
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

// createUserTask: JSON (title, description, assigneeId) sau multipart cu aceleasi campuri
// plus fisierele utilizatorului ("files"). Fisierele se scriu intai in staging; team le pune
// pe task inainte ca agentul sa poata porni.
func (s *Server) createUserTask(w http.ResponseWriter, r *http.Request) {
	pid, err := pathID(r)
	if err != nil {
		fail(w, err)
		return
	}
	var in struct {
		Title       string `json:"title"`
		Description string `json:"description"`
		AssigneeID  int64  `json:"assigneeId"` // 0 = primul liber din etapa (doar cu stageId)
		StageID     int64  `json:"stageId"`    // etapa in care intra; 0 = drumul standard
	}
	var uploads []*multipart.FileHeader
	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
		if _, err := s.st.GetProject(pid); err != nil {
			fail(w, err)
			return
		}
		if uploads, err = s.readUploads(w, r); err != nil {
			fail(w, err)
			return
		}
		in.Title, in.Description = r.FormValue("title"), r.FormValue("description")
		if v := strings.TrimSpace(r.FormValue("assigneeId")); v != "" {
			if in.AssigneeID, err = strconv.ParseInt(v, 10, 64); err != nil {
				fail(w, badRequest("assigneeId must be a number"))
				return
			}
		}
		if v := strings.TrimSpace(r.FormValue("stageId")); v != "" {
			if in.StageID, err = strconv.ParseInt(v, 10, 64); err != nil {
				fail(w, badRequest("stageId must be a number"))
				return
			}
		}
	} else if err := decode(r, &in); err != nil {
		fail(w, err)
		return
	}
	title := strings.TrimSpace(in.Title)
	if title == "" {
		fail(w, badRequest("title is required"))
		return
	}
	for _, err := range []error{maxLen("title", title, 200), maxLen("description", in.Description, 10000)} {
		if err != nil {
			fail(w, err)
			return
		}
	}
	if len(uploads) > team.MaxUserFiles { // refuzat inainte de scrierea pe disc (team verifica din nou)
		fail(w, badRequest(fmt.Sprintf("a task can have at most %d of your files; you chose %d", team.MaxUserFiles, len(uploads))))
		return
	}
	var task store.Task
	if len(uploads) > 0 {
		dir, staged, serr := s.stageUploads(pid, uploads)
		defer removeStaging(dir)
		if serr != nil {
			fail(w, serr)
			return
		}
		task, err = s.team.UserCreateTaskWithFiles(pid, title, strings.TrimSpace(in.Description), in.AssigneeID, in.StageID, staged)
	} else {
		task, err = s.team.UserCreateTask(pid, title, strings.TrimSpace(in.Description), in.AssigneeID, in.StageID)
	}
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
		// raspunsurile la intrebarile cu optiuni ale task-ului (optionale)
		Answers []struct {
			ID      int64    `json:"id"`
			Choices []string `json:"choices"`
			Other   string   `json:"other"`
		} `json:"answers"`
	}
	if err := decode(r, &in); err != nil {
		fail(w, err)
		return
	}
	if in.Action != "accept" && in.Action != "send_back" {
		fail(w, badRequest("action must be accept or send_back"))
		return
	}
	answers := make([]store.DecisionAnswer, len(in.Answers))
	for i, a := range in.Answers {
		answers[i] = store.DecisionAnswer{ID: a.ID, Choices: a.Choices, Other: a.Other}
	}
	err = s.team.UserDecide(id, in.Action == "accept", in.Feedback, answers)
	switch {
	case errors.Is(err, team.ErrNotUserDecision):
		writeErr(w, http.StatusConflict, err.Error())
	case errors.Is(err, store.ErrBadAnswer): // alegere necunoscuta, prea multe alegeri, other prea lung
		fail(w, badRequest(err.Error()))
	case err != nil && in.Action == "send_back" && strings.TrimSpace(in.Feedback) == "": // lipseste feedback-ul
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
