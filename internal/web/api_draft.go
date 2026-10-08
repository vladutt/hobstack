package web

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"foreman/internal/draft"
	"foreman/internal/store"
)

func (s *Server) startDraft(w http.ResponseWriter, r *http.Request) {
	if s.drafts == nil {
		writeErr(w, http.StatusNotImplemented, "drafts are not available")
		return
	}
	id, err := pathID(r)
	if err != nil {
		fail(w, err)
		return
	}
	var in struct{ Ask, Model string }
	if err := decode(r, &in); err != nil {
		fail(w, err)
		return
	}
	p, err := s.st.GetProject(id)
	if err != nil {
		fail(w, err)
		return
	}
	agents, err := s.st.ListAgents(id)
	if err != nil {
		fail(w, err)
		return
	}
	d, err := s.drafts.Start(draft.Input{ProjectID: id, Dir: p.Workdir, Brief: draftBrief(p, agents),
		Names: agentNames(agents), Ask: in.Ask, Model: in.Model})
	switch {
	case errors.Is(err, draft.ErrBusy):
		writeErr(w, http.StatusConflict, err.Error())
	case err != nil:
		writeErr(w, http.StatusBadRequest, err.Error())
	default:
		writeJSON(w, http.StatusCreated, d)
	}
}

func (s *Server) getDraft(w http.ResponseWriter, r *http.Request) {
	if s.drafts == nil {
		writeErr(w, http.StatusNotFound, draft.ErrNotFound.Error())
		return
	}
	d, ok := s.drafts.Get(r.PathValue("id"))
	if !ok {
		writeErr(w, http.StatusNotFound, draft.ErrNotFound.Error())
		return
	}
	writeJSON(w, http.StatusOK, d)
}

func (s *Server) stopDraft(w http.ResponseWriter, r *http.Request) {
	if s.drafts == nil {
		writeErr(w, http.StatusNotFound, draft.ErrNotFound.Error())
		return
	}
	id := r.PathValue("id")
	if _, ok := s.drafts.Get(id); !ok {
		writeErr(w, http.StatusNotFound, draft.ErrNotFound.Error())
		return
	}
	// un draft deja oprit nu e eroare: Stop-ul runner-ului ar putea intoarce una, o ignoram
	_ = s.drafts.Stop(id)
	w.WriteHeader(http.StatusNoContent)
}

func agentNames(agents []store.Agent) []string {
	out := make([]string, len(agents))
	for i, a := range agents {
		out[i] = a.Name
	}
	return out
}

// draftBrief: proiectul si echipa, pe scurt, pentru mesajul draft-ului
func draftBrief(p store.Project, agents []store.Agent) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Project: %s\nGoal: %s\n", p.Name, p.Goal)
	if p.Context != "" {
		ctx := []rune(p.Context)
		if len(ctx) > 3000 {
			ctx = ctx[:3000]
		}
		fmt.Fprintf(&b, "Shared context:\n%s\n", string(ctx))
	}
	b.WriteString("\nTeam:\n")
	byID := map[int64]string{}
	for _, a := range agents {
		byID[a.ID] = a.Name
	}
	for _, a := range agents {
		boss := "the user"
		if a.ParentID != nil {
			boss = byID[*a.ParentID]
		}
		fmt.Fprintf(&b, "- %s (%s), reports to %s. Takes: %s\n", a.Name, a.Role, boss, a.Responsibilities)
	}
	return b.String()
}
