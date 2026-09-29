package web

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"foreman/internal/store"
)

type projectInput struct {
	Name        string  `json:"name"`
	Goal        string  `json:"goal"`
	Context     string  `json:"context"`
	Workdir     string  `json:"workdir"`
	MaxParallel int     `json:"maxParallel"`
	BudgetUSD   float64 `json:"budgetUsd"`
}

// toProject valideaza input-ul. Limitele de lungime exista pentru ca aceste
// texte ajung in argumentele lui claude, iar sistemul le limiteaza lungimea.
func (in projectInput) toProject() (store.Project, error) {
	p := store.Project{
		Name:        strings.TrimSpace(in.Name),
		Goal:        strings.TrimSpace(in.Goal),
		Context:     strings.TrimSpace(in.Context),
		MaxParallel: in.MaxParallel,
		BudgetUSD:   in.BudgetUSD,
	}
	if p.Name == "" {
		return p, badRequest("numele proiectului e obligatoriu")
	}
	for _, err := range []error{
		maxLen("numele proiectului", p.Name, 80),
		maxLen("obiectivul", p.Goal, 2000),
		maxLen("contextul comun", p.Context, 20000),
	} {
		if err != nil {
			return p, err
		}
	}
	dir, err := expandHome(strings.TrimSpace(in.Workdir))
	if err != nil {
		return p, err
	}
	if !filepath.IsAbs(dir) {
		return p, badRequest("folderul de lucru trebuie sa fie o cale absoluta, ex. /Users/tu/proiect sau ~/proiect")
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return p, badRequest("folderul de lucru nu exista: " + dir)
	}
	p.Workdir = filepath.Clean(dir)
	// 0 (sau camp gol in UI) inseamna fara limita, la ambele.
	if p.MaxParallel < 0 || p.MaxParallel > 100 {
		return p, badRequest("workeri in paralel: gol pentru fara limita, sau intre 1 si 100")
	}
	if p.BudgetUSD < 0 || (p.BudgetUSD > 0 && p.BudgetUSD < 0.05) || p.BudgetUSD > 100 {
		return p, badRequest("bugetul per run: gol pentru fara limita, sau intre 0.05 si 100 USD")
	}
	return p, nil
}

// expandHome transforma "~/proiect" in cale absoluta.
func expandHome(path string) (string, error) {
	if path == "~" || strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, strings.TrimPrefix(path, "~")), nil
	}
	return path, nil
}

type memberBrief struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Position string `json:"position"`
}

type projectCard struct {
	store.Project
	Status string        `json:"status"` // Running | Idle | Paused
	Team   []memberBrief `json:"team"`
	Done   int           `json:"done"`
	Total  int           `json:"total"`
}

type agentView struct {
	store.Agent
	Status      string      `json:"status"`
	ActiveRunID string      `json:"activeRunId"`
	CurrentTask *store.Task `json:"currentTask"`
}

type projectDetail struct {
	projectCard
	Agents []agentView  `json:"agents"`
	Tasks  []store.Task `json:"tasks"`
}

// card calculeaza ce se afiseaza pe cardul proiectului.
func (s *Server) card(p store.Project) (projectCard, []store.Agent, []store.Task, error) {
	agents, err := s.st.ListAgents(p.ID)
	if err != nil {
		return projectCard{}, nil, nil, err
	}
	tasks, err := s.st.ListTasks(p.ID)
	if err != nil {
		return projectCard{}, nil, nil, err
	}
	c := projectCard{Project: p, Team: []memberBrief{}, Total: len(tasks), Status: "Idle"}
	for _, a := range agents {
		c.Team = append(c.Team, memberBrief{ID: a.ID, Name: a.Name, Position: a.Position})
	}
	busy := s.team.ProjectActive(p.ID)
	for _, t := range tasks {
		switch t.Status {
		case store.StatusDone:
			c.Done++
		case store.StatusQueued, store.StatusInProgress:
			busy = true
		}
	}
	switch {
	case p.Paused:
		c.Status = "Paused"
	case busy:
		c.Status = "Running"
	}
	return c, agents, tasks, nil
}

func (s *Server) listProjects(w http.ResponseWriter, r *http.Request) {
	projects, err := s.st.ListProjects()
	if err != nil {
		fail(w, err)
		return
	}
	out := []projectCard{}
	for _, p := range projects {
		c, _, _, err := s.card(p)
		if err != nil {
			fail(w, err)
			return
		}
		out = append(out, c)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) getProject(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		fail(w, err)
		return
	}
	p, err := s.st.GetProject(id)
	if err != nil {
		fail(w, err)
		return
	}
	c, agents, tasks, err := s.card(p)
	if err != nil {
		fail(w, err)
		return
	}
	d := projectDetail{projectCard: c, Agents: []agentView{}, Tasks: tasks}
	for _, a := range agents {
		status, current := s.team.AgentStatus(a, tasks)
		d.Agents = append(d.Agents, agentView{Agent: a, Status: status, ActiveRunID: s.team.ActiveRunID(a.ID), CurrentTask: current})
	}
	writeJSON(w, http.StatusOK, d)
}

func (s *Server) createProject(w http.ResponseWriter, r *http.Request) {
	var in projectInput
	if err := decode(r, &in); err != nil {
		fail(w, err)
		return
	}
	p, err := in.toProject()
	if err != nil {
		fail(w, err)
		return
	}
	if _, err := s.st.CreateProject(&p); err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, p)
}

func (s *Server) updateProject(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		fail(w, err)
		return
	}
	existing, err := s.st.GetProject(id)
	if err != nil {
		fail(w, err)
		return
	}
	var in projectInput
	if err := decode(r, &in); err != nil {
		fail(w, err)
		return
	}
	p, err := in.toProject()
	if err != nil {
		fail(w, err)
		return
	}
	p.ID, p.Paused, p.CreatedAt = existing.ID, existing.Paused, existing.CreatedAt
	if err := s.st.UpdateProject(p); err != nil {
		fail(w, err)
		return
	}
	s.hub.Publish(id, "project", map[string]int64{"id": id})
	s.team.Kick(id) // poate a crescut numarul de workeri in paralel
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) deleteProject(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		fail(w, err)
		return
	}
	if s.team.ProjectActive(id) {
		writeErr(w, http.StatusConflict, "echipa lucreaza acum; opreste echipa inainte sa stergi proiectul")
		return
	}
	if err := s.st.DeleteProject(id); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) stopProject(w http.ResponseWriter, r *http.Request) {
	s.projectAction(w, r, s.team.Stop)
}

func (s *Server) resumeProject(w http.ResponseWriter, r *http.Request) {
	s.projectAction(w, r, s.team.Resume)
}

func (s *Server) projectAction(w http.ResponseWriter, r *http.Request, action func(int64) error) {
	id, err := pathID(r)
	if err != nil {
		fail(w, err)
		return
	}
	if err := action(id); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
