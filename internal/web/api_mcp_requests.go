package web

import (
	"errors"
	"net/http"
	"time"

	"hobstack/internal/store"
)

// Cererile install_mcp ale agentilor, in "Needs you": utilizatorul vede comanda exacta, completeaza cheile si
// instaleaza (aceeasi proba si acelasi drum ca Library > MCP > Add server) sau refuza.

type mcpRequestView struct {
	ID          int64     `json:"id"`
	AgentID     int64     `json:"agentId"`
	AgentName   string    `json:"agentName"`
	TaskID      *int64    `json:"taskId"`
	Ref         string    `json:"ref"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Transport   string    `json:"transport"`
	Command     string    `json:"command"`
	Args        []string  `json:"args"`
	URL         string    `json:"url"`
	Secrets     []string  `json:"secrets"`
	Existing    bool      `json:"existing"` // serverul e deja in Library: se activeaza in proiect
	Reason      string    `json:"reason"`
	CreatedAt   time.Time `json:"createdAt"`
}

func (s *Server) mcpRequestViews(projectID int64, agents []store.Agent, tasks []store.Task) ([]mcpRequestView, error) {
	reqs, err := s.st.ListOpenMCPRequests(projectID)
	if err != nil {
		return nil, err
	}
	out := make([]mcpRequestView, 0, len(reqs))
	for _, r := range reqs {
		v := mcpRequestView{ID: r.ID, AgentID: r.AgentID, TaskID: r.TaskID, Name: r.Name, Description: r.Description,
			Transport: r.Transport, Command: r.Command, Args: r.Args, URL: r.URL, Secrets: r.Secrets,
			Existing: r.ItemID != nil, Reason: r.Reason, CreatedAt: r.CreatedAt}
		if a, ok := store.FindAgent(agents, r.AgentID); ok {
			v.AgentName = a.Name
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

// installMCPRequest: POST /api/mcp-requests/{id}/install {secretValues, skipTest}.
func (s *Server) installMCPRequest(w http.ResponseWriter, r *http.Request) {
	if !s.libraryOK(w) {
		return
	}
	id, err := pathID(r)
	if err != nil {
		fail(w, err)
		return
	}
	var in struct {
		SecretValues map[string]string `json:"secretValues"`
		SkipTest     bool              `json:"skipTest"`
	}
	if err := decode(r, &in); err != nil {
		fail(w, err)
		return
	}
	req, err := s.st.GetMCPRequest(id)
	if err != nil {
		fail(w, err)
		return
	}
	if req.Status != store.MCPRequestOpen {
		writeErr(w, http.StatusConflict, "this request was already decided")
		return
	}
	var tools []string
	if req.ItemID != nil {
		// un server deja in Library: doar se activeaza in proiect, cu cheile date acum
		it, err := s.st.GetLibraryItem(*req.ItemID)
		if err != nil {
			fail(w, err)
			return
		}
		if err := checkSecretValues(it.Secrets, in.SecretValues, nil); err != nil {
			fail(w, err)
			return
		}
		vs, err := s.st.ListLibraryVersions(it.ID)
		if err != nil {
			fail(w, err)
			return
		}
		if len(vs) == 0 || it.Status != store.ItemPublished {
			writeErr(w, http.StatusConflict, "this library server has no published version")
			return
		}
		if err := s.installInProject(req.ProjectID, it.ID, vs[0].ID, in.SecretValues); err != nil {
			fail(w, err)
			return
		}
		s.hub.Publish(req.ProjectID, "project", map[string]int64{"id": req.ProjectID})
		tools = it.ToolNames
	} else {
		it, ferr := s.createMCP(mcpInstall{Kind: req.Transport, Name: req.Name, Description: req.Description,
			Command: req.Command, Args: req.Args, URL: req.URL, Secrets: req.Secrets, SkipTest: in.SkipTest,
			ProjectID: req.ProjectID, SecretValues: in.SecretValues})
		if ferr != nil {
			ferr.write(w)
			return
		}
		tools = it.ToolNames
	}
	if err := s.team.MCPRequestInstalled(id, tools); err != nil && !errors.Is(err, store.ErrConflict) {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"name": req.Name, "tools": tools})
}

// declineMCPRequest: POST /api/mcp-requests/{id}/decline.
func (s *Server) declineMCPRequest(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		fail(w, err)
		return
	}
	if err := s.team.DeclineMCPRequest(id); errors.Is(err, store.ErrConflict) {
		writeErr(w, http.StatusConflict, "this request was already decided")
		return
	} else if err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
