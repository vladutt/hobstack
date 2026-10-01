package web

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"foreman/internal/store"
	"foreman/internal/team"
)

type folderInput struct {
	ID      int64  `json:"id"`
	Name    string `json:"name"`
	Path    string `json:"path"`
	Context string `json:"context"`
}

const maxFolders = 6

type projectInput struct {
	Name        string        `json:"name"`
	Goal        string        `json:"goal"`
	Context     string        `json:"context"`
	Workdir     string        `json:"workdir"` // API-ul vechi: un singur folder
	Folders     []folderInput `json:"folders"`
	MaxParallel int           `json:"maxParallel"`
	BudgetUSD   float64       `json:"budgetUsd"`
}

// folders valideaza folderele si le creeaza pe disc pe cele lipsa (ultimul
// pas). Fara lista, un singur folder din Workdir, ca inainte.
func (in projectInput) folders(projectName, workspaces string) ([]store.Folder, error) {
	list := in.Folders
	if len(list) == 0 {
		list = []folderInput{{Path: in.Workdir}}
	}
	if len(list) > maxFolders {
		return nil, badRequest(fmt.Sprintf("at most %d folders", maxFolders))
	}
	out := make([]store.Folder, 0, len(list))
	names := map[string]bool{}
	for _, f := range list {
		name, ctx := strings.TrimSpace(f.Name), strings.TrimSpace(f.Context)
		for _, err := range []error{maxLen("folder name", name, 40), maxLen("folder context", ctx, 20000)} {
			if err != nil {
				return nil, err
			}
		}
		// numele date se verifica inainte sa cream ceva pe disc
		if name != "" {
			if names[strings.ToLower(name)] {
				return nil, badRequest("two folders named " + name)
			}
			names[strings.ToLower(name)] = true
		}
		out = append(out, store.Folder{ID: f.ID, Name: name, Path: strings.TrimSpace(f.Path), Context: ctx})
	}
	paths := map[string]bool{}
	for i := range out {
		f := &out[i]
		hint := projectName // un folder nou in ~/Foreman: slug din proiect (+ numele folderului)
		if i > 0 {
			hint += " " + f.Name
		}
		dir, err := resolveWorkdir(f.Path, hint, workspaces)
		if err != nil {
			return nil, err
		}
		f.Path = dir
		if paths[dir] {
			return nil, badRequest("two folders with the path " + dir)
		}
		paths[dir] = true
		if err := checkNotNested(out[:i], dir); err != nil {
			return nil, err
		}
		if f.Name == "" {
			f.Name = store.FolderName(dir)
			if names[strings.ToLower(f.Name)] {
				return nil, badRequest("two folders named " + f.Name + "; give one of them another name")
			}
			names[strings.ToLower(f.Name)] = true
		}
	}
	return out, nil
}

// checkNotNested refuza un folder aflat in interiorul altuia (sau invers):
// un agent din folderul exterior ar putea scrie in cel interior. Caile sunt
// deja rezolvate; folderele din `prev` sunt cele verificate inainte.
func checkNotNested(prev []store.Folder, dir string) error {
	sep := string(filepath.Separator)
	for _, o := range prev {
		if strings.HasPrefix(dir, o.Path+sep) {
			return badRequest("folder " + dir + " is inside folder " + o.Path)
		}
		if strings.HasPrefix(o.Path, dir+sep) {
			return badRequest("folder " + o.Path + " is inside folder " + dir)
		}
	}
	return nil
}

// toProject valideaza input-ul. Limitele de lungime exista pentru ca aceste
// texte ajung in argumentele lui claude, iar sistemul le limiteaza lungimea.
func (in projectInput) toProject(workspaces string) (store.Project, error) {
	p := store.Project{
		Name:        strings.TrimSpace(in.Name),
		Goal:        strings.TrimSpace(in.Goal),
		Context:     strings.TrimSpace(in.Context),
		MaxParallel: in.MaxParallel,
		BudgetUSD:   in.BudgetUSD,
	}
	if p.Name == "" {
		return p, badRequest("project name is required")
	}
	for _, err := range []error{
		maxLen("project name", p.Name, 80),
		maxLen("goal", p.Goal, 2000),
		maxLen("shared context", p.Context, 20000),
	} {
		if err != nil {
			return p, err
		}
	}
	// 0 (sau camp gol in UI) inseamna fara limita, la ambele.
	if p.MaxParallel < 0 || p.MaxParallel > 100 {
		return p, badRequest("parallel workers: empty for no limit, or between 1 and 100")
	}
	if p.BudgetUSD < 0 || (p.BudgetUSD > 0 && p.BudgetUSD < 0.05) || p.BudgetUSD > 100 {
		return p, badRequest("budget per run: empty for no limit, or between 0.05 and 100 USD")
	}
	// ultimul pas: creeaza folderele doar dupa ce restul e valid
	folders, err := in.folders(p.Name, workspaces)
	if err != nil {
		return p, err
	}
	p.Folders, p.Workdir = folders, folders[0].Path
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
	ID     int64  `json:"id"`
	Name   string `json:"name"`
	IsLead bool   `json:"isLead"`
	IsRoot bool   `json:"isRoot"`
}

type projectCard struct {
	store.Project
	Status string        `json:"status"` // Running | Idle | Paused
	Team   []memberBrief `json:"team"`
	Done   int           `json:"done"`
	Total  int           `json:"total"`
	// OpenQuestions: intrebari de la agenti la care utilizatorul nu a raspuns
	OpenQuestions int `json:"openQuestions"`
	// StuckTasks: cate task-uri impinse deja stau tot blocate ("Needs you")
	StuckTasks int `json:"stuckTasks"`
}

type agentView struct {
	store.Agent
	store.Roles
	Status      string      `json:"status"`
	ActiveRunID string      `json:"activeRunId"`
	CurrentTask *store.Task `json:"currentTask"`
}

type projectDetail struct {
	projectCard
	Agents []agentView   `json:"agents"`
	Tasks  []store.Task  `json:"tasks"`
	Stages []store.Stage `json:"stages"` // coloanele proprii (etape); gol = board-ul implicit
	// UserDecides: task-urile din "La lider" pe care le decide utilizatorul (butoane in board)
	UserDecides []int64 `json:"userDecides"`
	// Questions: intrebarile deschise ale agentilor catre utilizator
	Questions []store.Question `json:"questions"`
	// Stuck: task-urile blocate pe care utilizatorul le poate impinge (mereu non-nil)
	Stuck []team.StuckTask `json:"stuck"`
}

// card calculeaza ce se afiseaza pe cardul proiectului.
// card intoarce si task-urile blocate (calculate o singura data), ca getProject
// sa nu le mai calculeze inca o data pentru lista din detaliu.
func (s *Server) card(p store.Project) (projectCard, []store.Agent, []store.Task, []team.StuckTask, error) {
	agents, err := s.st.ListAgents(p.ID)
	if err != nil {
		return projectCard{}, nil, nil, nil, err
	}
	tasks, err := s.st.ListTasks(p.ID)
	if err != nil {
		return projectCard{}, nil, nil, nil, err
	}
	c := projectCard{Project: p, Team: []memberBrief{}, Total: len(tasks), Status: "Idle"}
	if c.OpenQuestions, err = s.st.CountOpenQuestions(p.ID); err != nil {
		return projectCard{}, nil, nil, nil, err
	}
	for _, a := range agents {
		r := store.RolesOf(a, agents)
		c.Team = append(c.Team, memberBrief{ID: a.ID, Name: a.Name, IsLead: r.IsLead, IsRoot: r.IsRoot})
	}
	stuck, err := s.team.StuckTasks(p.ID, s.team.Now())
	if err != nil {
		return projectCard{}, nil, nil, nil, err
	}
	c.StuckTasks = len(stuck)
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
	return c, agents, tasks, stuck, nil
}

func (s *Server) listProjects(w http.ResponseWriter, r *http.Request) {
	projects, err := s.st.ListProjects()
	if err != nil {
		fail(w, err)
		return
	}
	out := []projectCard{}
	for _, p := range projects {
		c, _, _, _, err := s.card(p)
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
	c, agents, tasks, stuck, err := s.card(p)
	if err != nil {
		fail(w, err)
		return
	}
	stages, err := s.st.ListStages(p.ID)
	if err != nil {
		fail(w, err)
		return
	}
	questions, err := s.st.ListOpenQuestions(p.ID)
	if err != nil {
		fail(w, err)
		return
	}
	if questions == nil {
		questions = []store.Question{}
	}
	d := projectDetail{projectCard: c, Agents: []agentView{}, Tasks: tasks, Stages: stages, UserDecides: []int64{}, Questions: questions, Stuck: stuck}
	for _, t := range tasks {
		if s.team.UserDecides(t, agents) {
			d.UserDecides = append(d.UserDecides, t.ID)
		}
	}
	for _, a := range agents {
		status, current := s.team.AgentStatus(a, tasks)
		d.Agents = append(d.Agents, agentView{Agent: a, Roles: store.RolesOf(a, agents), Status: status, ActiveRunID: s.team.ActiveRunID(a.ID), CurrentTask: current})
	}
	writeJSON(w, http.StatusOK, d)
}

func (s *Server) createProject(w http.ResponseWriter, r *http.Request) {
	var in projectInput
	if err := decode(r, &in); err != nil {
		fail(w, err)
		return
	}
	for i := range in.Folders {
		in.Folders[i].ID = 0 // proiect nou: toate folderele sunt noi
	}
	p, err := in.toProject(s.dirs.Workspaces)
	if err != nil {
		fail(w, err)
		return
	}
	if _, err := s.st.CreateProject(&p); err != nil {
		fail(w, err)
		return
	}
	// id-urile pot fi refolosite: un folder ramas de la un proiect vechi nu are ce cauta aici
	if err := os.RemoveAll(filepath.Join(s.dirs.Projects, strconv.FormatInt(p.ID, 10))); err != nil {
		log.Printf("cleaning up old project folder %d: %v", p.ID, err)
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
	if len(in.Folders) == 0 {
		// API-ul vechi (doar workdir): folderele raman; un workdir nevid schimba primul folder
		for _, f := range existing.Folders {
			in.Folders = append(in.Folders, folderInput{ID: f.ID, Name: f.Name, Path: f.Path, Context: f.Context})
		}
		if w := strings.TrimSpace(in.Workdir); w != "" && len(in.Folders) > 0 {
			in.Folders[0].Path = w
		}
	}
	known := map[int64]bool{}
	for _, f := range existing.Folders {
		known[f.ID] = true
	}
	for _, f := range in.Folders {
		if f.ID != 0 && !known[f.ID] {
			fail(w, badRequest("unknown folder; reload the page"))
			return
		}
	}
	// o cale goala pe un folder existent = pastreaza calea (nu se creeaza un folder nou)
	oldPath := map[int64]string{}
	for _, f := range existing.Folders {
		oldPath[f.ID] = f.Path
	}
	for i, f := range in.Folders {
		if f.ID != 0 && strings.TrimSpace(f.Path) == "" {
			in.Folders[i].Path = oldPath[f.ID]
		}
	}
	p, err := in.toProject(s.dirs.Workspaces)
	if err != nil {
		fail(w, err)
		return
	}
	p.ID, p.Paused, p.CreatedAt = existing.ID, existing.Paused, existing.CreatedAt
	if err := s.st.UpdateProject(p); err != nil {
		fail(w, err)
		return
	}
	saved, err := s.st.GetProject(id)
	if err != nil {
		fail(w, err)
		return
	}
	s.hub.Publish(id, "project", map[string]int64{"id": id})
	s.team.Kick(id)
	writeJSON(w, http.StatusOK, saved)
}

func (s *Server) deleteProject(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		fail(w, err)
		return
	}
	if s.team.ProjectActive(id) {
		writeErr(w, http.StatusConflict, "the team is working now; stop the team before deleting the project")
		return
	}
	if err := s.st.DeleteProject(id); err != nil {
		fail(w, err)
		return
	}
	// fisierele proiectului; folderul de lucru nu se atinge niciodata
	if err := os.RemoveAll(filepath.Join(s.dirs.Projects, strconv.FormatInt(id, 10))); err != nil {
		log.Printf("cannot delete files of project %d: %v", id, err)
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
