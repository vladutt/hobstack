package web

import (
	"net/http"
	"time"

	"hobstack/internal/store"
)

// agentRequestView e o cerere add_agent deschisa, asa cum o vede UI-ul.
type agentRequestView struct {
	ID        int64           `json:"id"`
	LeadID    int64           `json:"leadId"`
	LeadName  string          `json:"leadName"`
	TaskID    *int64          `json:"taskId"`
	Ref       string          `json:"ref"` // T-3, gol daca cererea n-a venit de pe un task
	Spec      store.AgentSpec `json:"spec"`
	Reason    string          `json:"reason"`
	CreatedAt time.Time       `json:"createdAt"`
}

// agentRequestViews intoarce cererile deschise ale proiectului (lista e mereu non-nil).
func (s *Server) agentRequestViews(projectID int64, agents []store.Agent, tasks []store.Task) ([]agentRequestView, error) {
	reqs, err := s.st.ListOpenAgentRequests(projectID)
	if err != nil {
		return nil, err
	}
	out := make([]agentRequestView, 0, len(reqs))
	for _, r := range reqs {
		v := agentRequestView{ID: r.ID, LeadID: r.LeadID, TaskID: r.TaskID, Spec: r.Spec, Reason: r.Reason, CreatedAt: r.CreatedAt}
		if lead, ok := store.FindAgent(agents, r.LeadID); ok {
			v.LeadName = lead.Name
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

func (s *Server) approveAgentRequest(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		fail(w, err)
		return
	}
	if err := s.team.ApproveAgentRequest(id); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) dismissAgentRequest(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		fail(w, err)
		return
	}
	if err := s.team.DismissAgentRequest(id); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
