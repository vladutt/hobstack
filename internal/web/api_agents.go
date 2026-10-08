package web

import (
	"errors"
	"fmt"
	"log"
	"math"
	"net/http"
	"slices"
	"strings"

	"foreman/internal/prompt"
	"foreman/internal/store"
	"foreman/internal/team"
)

type agentInput struct {
	Name             string   `json:"name"`
	Role             string   `json:"role"`
	Model            string   `json:"model"`
	Background       string   `json:"background"`
	Instructions     string   `json:"instructions"`
	DoneDef          string   `json:"doneDef"`
	Tools            []string `json:"tools"`
	Responsibilities string   `json:"responsibilities"`
	ParentID         *int64   `json:"parentId"`   // null sau lipsa = radacina
	ReviewerID       *int64   `json:"reviewerId"` // null sau lipsa = fara reviewer
	Detached         bool     `json:"detached"`   // cu parentId null: nelegat, nici de utilizator
	FolderID         *int64   `json:"folderId"`   // null sau lipsa = primul folder al proiectului
}

// apply valideaza input-ul si il copiaza in agent (fara proiect si pozitie; legaturile se valideaza in store).
func (in agentInput) apply(a *store.Agent) error {
	a.Name, a.Role, a.Model, a.Tools = strings.TrimSpace(in.Name), strings.TrimSpace(in.Role), in.Model, in.Tools
	a.Background = strings.TrimSpace(in.Background)
	a.Instructions = strings.TrimSpace(in.Instructions)
	a.DoneDef = strings.TrimSpace(in.DoneDef)
	a.Responsibilities = strings.TrimSpace(in.Responsibilities)
	a.ParentID, a.ReviewerID = in.ParentID, in.ReviewerID
	a.FolderID = in.FolderID // un folder din alt proiect: LinkError din store, 400
	a.Detached = in.Detached && in.ParentID == nil
	// validarea comuna cu add_agent (nume, model, tools fara duplicate, lungimi)
	if err := team.CheckAgentFields(a); err != nil {
		return badRequest(err.Error())
	}
	return nil
}

func (s *Server) createAgent(w http.ResponseWriter, r *http.Request) {
	pid, err := pathID(r)
	if err != nil {
		fail(w, err)
		return
	}
	if _, err := s.st.GetProject(pid); err != nil {
		fail(w, err)
		return
	}
	var in agentInput
	if err := decode(r, &in); err != nil {
		fail(w, err)
		return
	}
	a := store.Agent{ProjectID: pid}
	if err := in.apply(&a); err != nil {
		fail(w, err)
		return
	}
	if err := s.st.CreateAgent(&a); errors.Is(err, store.ErrConflict) {
		writeErr(w, http.StatusConflict, fmt.Sprintf("an agent named %s already exists in the project", a.Name))
		return
	} else if err != nil {
		fail(w, err)
		return
	}
	s.hub.Publish(pid, "agent", map[string]int64{"id": a.ID})
	writeJSON(w, http.StatusCreated, a)
}

func (s *Server) updateAgent(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		fail(w, err)
		return
	}
	// pornim de la agentul salvat: din corp se iau doar campurile editabile,
	// niciodata projectId sau pozitia de pe canvas
	a, err := s.st.GetAgent(id)
	if err != nil {
		fail(w, err)
		return
	}
	var in agentInput
	if err := decode(r, &in); err != nil {
		fail(w, err)
		return
	}
	if err := in.apply(&a); err != nil {
		fail(w, err)
		return
	}
	if err := s.st.UpdateAgent(a); errors.Is(err, store.ErrConflict) {
		writeErr(w, http.StatusConflict, fmt.Sprintf("an agent named %s already exists in the project", a.Name))
		return
	} else if err != nil {
		fail(w, err)
		return
	}
	s.linksChanged(a.ProjectID)
	s.hub.Publish(a.ProjectID, "agent", map[string]int64{"id": a.ID})
	writeJSON(w, http.StatusOK, a)
}

func (s *Server) deleteAgent(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		fail(w, err)
		return
	}
	a, err := s.st.GetAgent(id)
	if err != nil {
		fail(w, err)
		return
	}
	if s.team.ActiveRunID(id) != "" {
		writeErr(w, http.StatusConflict, "the agent is working now; stop the team before deleting it")
		return
	}
	if err := s.st.DeleteAgent(id); errors.Is(err, store.ErrConflict) {
		writeErr(w, http.StatusConflict, "agents with tasks and the last agent in the project cannot be deleted")
		return
	} else if err != nil {
		fail(w, err)
		return
	}
	s.linksChanged(a.ProjectID)
	s.hub.Publish(a.ProjectID, "agent", map[string]int64{"id": id})
	w.WriteHeader(http.StatusNoContent)
}

// linksChanged anunta echipa ca legaturile s-au putut schimba (ex. un reviewer
// disparut). Agentul e deja salvat, deci o eroare aici doar se logheaza.
func (s *Server) linksChanged(projectID int64) {
	if err := s.team.LinksChanged(projectID); err != nil {
		log.Printf("project %d, links changed: %v", projectID, err)
	}
}

func (s *Server) listMessages(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		fail(w, err)
		return
	}
	if _, err := s.st.GetAgent(id); err != nil {
		fail(w, err)
		return
	}
	// ?chat=1: doar chat-ul curent (de la ultimul "New chat") si cate mesaje sunt ascunse
	if r.URL.Query().Get("chat") == "1" {
		msgs, earlier, err := s.st.ListChat(id)
		if err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"messages": msgs, "earlier": earlier})
		return
	}
	msgs, err := s.st.ListMessages(id)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, msgs)
}

// newChat porneste un chat nou cu agentul (sesiune Claude noua, istoric ascuns).
func (s *Server) newChat(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		fail(w, err)
		return
	}
	m, err := s.team.NewChat(id)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, m)
}

func (s *Server) postMessage(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		fail(w, err)
		return
	}
	// un agent nelegat nu lucreaza: nu primeste mesaje pana il legi pe canvas
	if a, err := s.st.GetAgent(id); err != nil {
		fail(w, err)
		return
	} else if a.Detached {
		fail(w, badRequest("the agent is not linked. Link it to You or to a lead so it can work"))
		return
	}
	var text string
	var saved []store.Attachment
	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
		agent, err := s.st.GetAgent(id)
		if err != nil {
			fail(w, err)
			return
		}
		files, err := s.readUploads(w, r)
		if err != nil {
			fail(w, err)
			return
		}
		text = strings.TrimSpace(r.FormValue("text"))
		if text == "" && len(files) == 0 {
			fail(w, badRequest("the message is empty"))
			return
		}
		if saved, err = s.saveUploads(agent.ProjectID, store.KindMessageFile, files); err != nil {
			fail(w, err)
			return
		}
		if text == "" {
			text = "(see attachments)"
		}
	} else {
		var in struct {
			Text string `json:"text"`
		}
		if err := decode(r, &in); err != nil {
			fail(w, err)
			return
		}
		text = strings.TrimSpace(in.Text)
		if text == "" {
			fail(w, badRequest("the message is empty"))
			return
		}
	}
	if err := maxLen("message", text, 10000); err != nil {
		s.discardUploads(saved)
		fail(w, err)
		return
	}
	ids := []int64{}
	for _, a := range saved {
		ids = append(ids, a.ID)
	}
	m, err := s.team.UserMessage(id, text, ids)
	if err != nil {
		s.discardUploads(saved)
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, m)
}

// promptPreview compileaza system prompt-ul din campurile nesalvate ale editorului.
// Seful vine din parentId (null = radacina); lider/reviewer reies din legaturile salvate.
func (s *Server) promptPreview(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ProjectID    int64    `json:"projectId"`
		AgentID      int64    `json:"agentId"` // 0 = agent nou
		Name         string   `json:"name"`
		Role         string   `json:"role"`
		ParentID     *int64   `json:"parentId"`
		FolderID     *int64   `json:"folderId"`
		Tools        []string `json:"tools"` // alege daca promptul are linia cu comenzile blocate
		Background   string   `json:"background"`
		Instructions string   `json:"instructions"`
		DoneDef      string   `json:"doneDef"`
	}
	if err := decode(r, &in); err != nil {
		fail(w, err)
		return
	}
	p, err := s.st.GetProject(in.ProjectID)
	if err != nil {
		fail(w, err)
		return
	}
	agents, err := s.st.ListAgents(p.ID)
	if err != nil {
		fail(w, err)
		return
	}
	if in.ParentID != nil {
		if _, ok := store.FindAgent(agents, *in.ParentID); !ok {
			fail(w, badRequest("the chosen boss is not in this project"))
			return
		}
		if *in.ParentID == in.AgentID {
			fail(w, badRequest("an agent cannot be its own boss"))
			return
		}
	}
	a := store.Agent{
		ID: in.AgentID, ProjectID: p.ID, Name: strings.TrimSpace(in.Name), Role: strings.TrimSpace(in.Role),
		Background: in.Background, Instructions: in.Instructions, DoneDef: in.DoneDef, ParentID: in.ParentID, FolderID: in.FolderID, Tools: in.Tools,
	}
	if a.Name == "" {
		a.Name = "Agent"
	}
	pl := prompt.LinksFor(a, agents)
	if slices.Contains(a.Tools, "shell") { // ca in team/run.go
		rules, err := s.team.EffectiveCommandRules(p.ID)
		if err != nil {
			fail(w, err)
			return
		}
		pl.BlockedCommands = prompt.BlockedPrefixes(rules)
	}
	writeJSON(w, http.StatusOK, map[string]string{"prompt": prompt.System(p, a, pl)})
}

// setAgentPosition retine pozitia agentului pe canvas (fara eveniment SSE).
func (s *Server) setAgentPosition(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		fail(w, err)
		return
	}
	var in struct {
		X float64 `json:"x"`
		Y float64 `json:"y"`
	}
	if err := decode(r, &in); err != nil {
		fail(w, err)
		return
	}
	if math.Abs(in.X) > 100000 || math.Abs(in.Y) > 100000 {
		fail(w, badRequest("invalid position"))
		return
	}
	if err := s.st.SetAgentPosition(id, in.X, in.Y); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
