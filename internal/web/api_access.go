package web

import (
	"database/sql"
	"errors"
	"net/http"
	"slices"
	"time"

	"foreman/internal/store"
)

// accessView e o cerere de acces deschisa, asa cum o vede UI-ul.
type accessView struct {
	ID        int64     `json:"id"`
	AgentID   int64     `json:"agentId"`
	AgentName string    `json:"agentName"`
	TaskID    *int64    `json:"taskId"`
	Ref       string    `json:"ref"` // T-3, gol daca refuzul n-a venit de pe un task
	Tool      string    `json:"tool"`
	Detail    string    `json:"detail"`
	Count     int       `json:"count"`
	UpdatedAt time.Time `json:"updatedAt"`
	// CanEnableShell: tool-ul refuzat e Bash si agentul nu are inca "shell"
	CanEnableShell bool `json:"canEnableShell"`
}

// accessViews intoarce cererile deschise ale proiectului (lista e mereu non-nil).
func (s *Server) accessViews(projectID int64, agents []store.Agent, tasks []store.Task) ([]accessView, error) {
	reqs, err := s.st.ListOpenAccessRequests(projectID)
	if err != nil {
		return nil, err
	}
	out := make([]accessView, 0, len(reqs))
	for _, r := range reqs {
		v := accessView{ID: r.ID, AgentID: r.AgentID, TaskID: r.TaskID, Tool: r.Tool, Detail: r.Detail, Count: r.Count, UpdatedAt: r.UpdatedAt}
		for _, a := range agents {
			if a.ID == r.AgentID {
				v.AgentName = a.Name
				v.CanEnableShell = r.Tool == "Bash" && !slices.Contains(a.Tools, "shell")
			}
		}
		if r.TaskID != nil {
			for _, t := range tasks {
				if t.ID == *r.TaskID {
					v.Ref = t.Ref()
				}
			}
		}
		out = append(out, v)
	}
	return out, nil
}

// getAccess cauta cererea; lipsa se raporteaza ca 404.
func (s *Server) getAccess(r *http.Request) (store.AccessRequest, error) {
	id, err := pathID(r)
	if err != nil {
		return store.AccessRequest{}, err
	}
	req, err := s.st.GetAccessRequest(id)
	if errors.Is(err, sql.ErrNoRows) {
		return req, store.ErrNotFound
	}
	return req, err
}

func (s *Server) dismissAccess(w http.ResponseWriter, r *http.Request) {
	req, err := s.getAccess(r)
	if err != nil {
		fail(w, err)
		return
	}
	if err := s.st.DismissAccessRequest(req.ID); err != nil {
		fail(w, err)
		return
	}
	s.hub.Publish(req.ProjectID, "access", map[string]int64{"agentId": req.AgentID})
	w.WriteHeader(http.StatusNoContent)
}

// enableShell adauga "shell" la tool-urile agentului (restul campurilor raman)
// si inchide cererea.
func (s *Server) enableShell(w http.ResponseWriter, r *http.Request) {
	req, err := s.getAccess(r)
	if err != nil {
		fail(w, err)
		return
	}
	if req.Tool != "Bash" {
		writeErr(w, http.StatusConflict, "only shell access can be enabled from here")
		return
	}
	a, err := s.st.GetAgent(req.AgentID)
	if err != nil {
		fail(w, err)
		return
	}
	if !slices.Contains(a.Tools, "shell") {
		a.Tools = append(a.Tools, "shell")
		if err := s.st.UpdateAgent(a); err != nil {
			fail(w, err)
			return
		}
		s.hub.Publish(a.ProjectID, "agent", map[string]int64{"id": a.ID})
	}
	if err := s.st.DismissAccessRequest(req.ID); err != nil {
		fail(w, err)
		return
	}
	s.hub.Publish(req.ProjectID, "access", map[string]int64{"agentId": req.AgentID})
	w.WriteHeader(http.StatusNoContent)
}
