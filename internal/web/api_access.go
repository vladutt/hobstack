package web

import (
	"database/sql"
	"errors"
	"net/http"
	"slices"
	"time"

	"foreman/internal/prompt"
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
	Prefix    string    `json:"prefix"` // gol = cerere de tool; altfel prefixul de comanda pe care il poti permite/bloca
	Count     int       `json:"count"`
	UpdatedAt time.Time `json:"updatedAt"`
	// CanEnableShell: tool-ul refuzat e Bash, agentul nu are inca "shell" si
	// comanda nu e in refuzurile fixe (acolo Shell nu ar ajuta)
	CanEnableShell bool `json:"canEnableShell"`
	// CanEnablePackages: comanda refuzata instaleaza pachete (npm install...) si
	// agentul nu are inca "packages"
	CanEnablePackages bool `json:"canEnablePackages"`
	Kind              string `json:"kind"`     // "" (tool) / "ask" / "command"
	HasShell          bool   `json:"hasShell"` // fara Shell, "Allow in this project" nu are efect
}

// accessViews intoarce cererile deschise ale proiectului (lista e mereu non-nil).
func (s *Server) accessViews(projectID int64, agents []store.Agent, tasks []store.Task) ([]accessView, error) {
	reqs, err := s.st.ListOpenAccessRequests(projectID)
	if err != nil {
		return nil, err
	}
	rules, err := s.team.EffectiveCommandRules(projectID)
	if err != nil {
		return nil, err
	}
	out := make([]accessView, 0, len(reqs))
	for _, r := range reqs {
		v := accessView{ID: r.ID, AgentID: r.AgentID, TaskID: r.TaskID, Tool: r.Tool, Detail: r.Detail, Prefix: r.Prefix, Kind: r.Kind, Count: r.Count, UpdatedAt: r.UpdatedAt}
		for _, a := range agents {
			if a.ID == r.AgentID {
				hasShell := slices.Contains(a.Tools, "shell")
				v.AgentName, v.HasShell = a.Name, hasShell
				// Enable Shell ajuta la o cerere de tool Bash (daca nu e refuz fix) si la o comanda calculata
				v.CanEnableShell = r.Tool == "Bash" && !hasShell &&
					(r.Kind == store.AccessCommand || (r.Kind == store.AccessTool && !prompt.AlwaysDeniedBash(r.Detail, a.Tools, rules)))
				v.CanEnablePackages = r.Kind == store.AccessTool && r.Tool == "Bash" && !slices.Contains(a.Tools, "packages") && prompt.NeedsPackages(r.Detail)
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

func (s *Server) enableShell(w http.ResponseWriter, r *http.Request) {
	s.enableTool(w, r, "shell")
}

func (s *Server) enablePackages(w http.ResponseWriter, r *http.Request) {
	s.enableTool(w, r, "packages")
}

// enableTool adauga tool-ul ("shell" sau "packages") la tool-urile agentului
// (restul campurilor raman) si inchide cererea.
func (s *Server) enableTool(w http.ResponseWriter, r *http.Request, tool string) {
	req, err := s.getAccess(r)
	if err != nil {
		fail(w, err)
		return
	}
	if req.Tool != "Bash" || req.Prefix != "" {
		writeErr(w, http.StatusConflict, "only shell access can be enabled from here")
		return
	}
	a, err := s.st.GetAgent(req.AgentID)
	if err != nil {
		fail(w, err)
		return
	}
	if !slices.Contains(a.Tools, tool) {
		a.Tools = append(a.Tools, tool)
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
