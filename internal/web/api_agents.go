package web

import (
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"foreman/internal/models"
	"foreman/internal/prompt"
	"foreman/internal/store"
)

type agentInput struct {
	Name         string   `json:"name"`
	Role         string   `json:"role"`
	Model        string   `json:"model"`
	Background   string   `json:"background"`
	Instructions string   `json:"instructions"`
	DoneDef      string   `json:"doneDef"`
	Tools        []string `json:"tools"`
}

// apply valideaza input-ul si il copiaza in agent (fara pozitie si proiect).
func (in agentInput) apply(a *store.Agent) error {
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return badRequest("numele agentului e obligatoriu")
	}
	if !models.Valid(in.Model) {
		return badRequest("model necunoscut: " + in.Model)
	}
	tools := []string{}
	for _, tool := range in.Tools {
		if !slices.Contains(prompt.ValidTools, tool) {
			return badRequest("tool necunoscut: " + tool)
		}
		if !slices.Contains(tools, tool) {
			tools = append(tools, tool)
		}
	}
	a.Name, a.Role, a.Model, a.Tools = name, strings.TrimSpace(in.Role), in.Model, tools
	a.Background = strings.TrimSpace(in.Background)
	a.Instructions = strings.TrimSpace(in.Instructions)
	a.DoneDef = strings.TrimSpace(in.DoneDef)
	for _, err := range []error{
		maxLen("numele agentului", a.Name, 40),
		maxLen("rolul", a.Role, 80),
		maxLen("background", a.Background, 10000),
		maxLen("instructiunile", a.Instructions, 10000),
		maxLen("definition of done", a.DoneDef, 4000),
	} {
		if err != nil {
			return err
		}
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
	a := store.Agent{ProjectID: pid, Position: store.PositionWorker}
	if err := in.apply(&a); err != nil {
		fail(w, err)
		return
	}
	if err := s.st.CreateAgent(&a); errors.Is(err, store.ErrConflict) {
		writeErr(w, http.StatusConflict, fmt.Sprintf("exista deja un agent cu numele %s in proiect", a.Name))
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
		writeErr(w, http.StatusConflict, fmt.Sprintf("exista deja un agent cu numele %s in proiect", a.Name))
		return
	} else if err != nil {
		fail(w, err)
		return
	}
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
		writeErr(w, http.StatusConflict, "agentul lucreaza acum; opreste echipa inainte sa il stergi")
		return
	}
	if err := s.st.DeleteAgent(id); errors.Is(err, store.ErrConflict) {
		writeErr(w, http.StatusConflict, "lead-ul si agentii care au task-uri nu pot fi stersi")
		return
	} else if err != nil {
		fail(w, err)
		return
	}
	s.hub.Publish(a.ProjectID, "agent", map[string]int64{"id": id})
	w.WriteHeader(http.StatusNoContent)
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
	msgs, err := s.st.ListMessages(id)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, msgs)
}

func (s *Server) postMessage(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		fail(w, err)
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
			fail(w, badRequest("mesajul e gol"))
			return
		}
		if saved, err = s.saveUploads(agent.ProjectID, store.KindMessageFile, files); err != nil {
			fail(w, err)
			return
		}
		if text == "" {
			text = "(vezi atasamentele)"
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
			fail(w, badRequest("mesajul e gol"))
			return
		}
	}
	if err := maxLen("mesajul", text, 10000); err != nil {
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
func (s *Server) promptPreview(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ProjectID    int64  `json:"projectId"`
		Name         string `json:"name"`
		Role         string `json:"role"`
		Position     string `json:"position"`
		ParentID     *int64 `json:"parentId"`
		Background   string `json:"background"`
		Instructions string `json:"instructions"`
		DoneDef      string `json:"doneDef"`
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
	a := store.Agent{
		Name: strings.TrimSpace(in.Name), Role: strings.TrimSpace(in.Role), Position: in.Position,
		Background: in.Background, Instructions: in.Instructions, DoneDef: in.DoneDef,
	}
	if a.Name == "" {
		a.Name = "Agent"
	}
	// echipa reala + agentul nou (ID 0): seful e parentId din cerere, altfel prima radacina
	team, err := s.st.ListAgents(p.ID)
	if err != nil {
		fail(w, err)
		return
	}
	a.ParentID = in.ParentID
	if a.ParentID == nil && len(team) > 0 {
		a.ParentID = &team[0].ID
	}
	writeJSON(w, http.StatusOK, map[string]string{"prompt": prompt.System(p, a, prompt.LinksFor(a, team))})
}
