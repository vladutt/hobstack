package web

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"foreman/internal/store"
)

// linkView e un proiect legat, asa cum il vede UI-ul.
type linkView struct {
	ProjectID int64  `json:"projectId"`
	Name      string `json:"name"`
}

// workRequestView e o cerere de lucru care asteapta aprobarea, la proiectul tinta.
type workRequestView struct {
	ID          int64     `json:"id"`
	Ref         string    `json:"ref"`
	FromProject string    `json:"fromProject"`
	FromAgent   string    `json:"fromAgent"`
	Title       string    `json:"title"`
	Text        string    `json:"text"`
	CreatedAt   time.Time `json:"createdAt"`
}

// linkViews intoarce legaturile proiectului (lista e mereu non-nil).
func (s *Server) linkViews(projectID int64) ([]linkView, error) {
	links, err := s.st.LinkedProjects(projectID)
	if err != nil {
		return nil, err
	}
	out := make([]linkView, 0, len(links))
	for _, l := range links {
		out = append(out, linkView{ProjectID: l.ID, Name: l.Name})
	}
	return out, nil
}

// workRequestViews intoarce cererile de lucru in asteptare catre proiect (mereu non-nil).
func (s *Server) workRequestViews(projectID int64) ([]workRequestView, error) {
	reqs, err := s.st.ListPendingWork(projectID)
	if err != nil {
		return nil, err
	}
	out := make([]workRequestView, 0, len(reqs))
	for _, r := range reqs {
		v := workRequestView{ID: r.ID, Ref: r.Ref(), Title: r.Title, Text: r.Text, CreatedAt: r.CreatedAt,
			FromProject: "a deleted project", FromAgent: "a deleted agent"}
		if p, err := s.st.GetProject(r.FromProject); err == nil {
			v.FromProject = p.Name
		}
		if r.FromAgent != nil {
			if a, err := s.st.GetAgent(*r.FromAgent); err == nil {
				v.FromAgent = a.Name
			}
		}
		out = append(out, v)
	}
	return out, nil
}

func (s *Server) listLinks(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		fail(w, err)
		return
	}
	if _, err := s.st.GetProject(id); err != nil {
		fail(w, err)
		return
	}
	views, err := s.linkViews(id)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, views)
}

func (s *Server) createLink(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		fail(w, err)
		return
	}
	var in struct {
		ProjectID int64 `json:"projectId"`
	}
	if err := decode(r, &in); err != nil {
		fail(w, err)
		return
	}
	if in.ProjectID == id {
		writeErr(w, http.StatusBadRequest, store.ErrSameProject.Error())
		return
	}
	if _, err := s.st.GetProject(id); err != nil {
		fail(w, err)
		return
	}
	other, err := s.st.GetProject(in.ProjectID)
	if err != nil {
		fail(w, err)
		return
	}
	if err := s.st.LinkProjects(id, other.ID); errors.Is(err, store.ErrConflict) {
		writeErr(w, http.StatusConflict, "the projects are already linked")
		return
	} else if err != nil {
		fail(w, err)
		return
	}
	s.hub.Publish(id, "project", map[string]int64{"id": id})
	s.hub.Publish(other.ID, "project", map[string]int64{"id": other.ID})
	writeJSON(w, http.StatusCreated, linkView{ProjectID: other.ID, Name: other.Name})
}

func (s *Server) deleteLink(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		fail(w, err)
		return
	}
	other, err := strconv.ParseInt(r.PathValue("other"), 10, 64)
	if err != nil {
		fail(w, badRequest("invalid id"))
		return
	}
	if err := s.team.UnlinkProjects(id, other); err != nil {
		fail(w, err)
		return
	}
	s.hub.Publish(id, "project", map[string]int64{"id": id})
	s.hub.Publish(other, "project", map[string]int64{"id": other})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) approveProjectRequest(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		fail(w, err)
		return
	}
	if err := s.team.ApproveProjectRequest(id); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) dismissProjectRequest(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		fail(w, err)
		return
	}
	if err := s.team.DismissProjectRequest(id); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
