package web

import (
	"errors"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"

	wizard "foreman/internal/setup"
	"foreman/internal/store"
)

// setupFail traduce erorile de validare ale wizard-ului in 400.
func setupFail(w http.ResponseWriter, err error) {
	var ve wizard.ValidationError
	if errors.As(err, &ve) {
		writeErr(w, http.StatusBadRequest, ve.Error())
		return
	}
	if errors.Is(err, wizard.ErrNotFound) {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}
	fail(w, err)
}

func (s *Server) startSetup(w http.ResponseWriter, r *http.Request) {
	if s.wizard == nil {
		writeErr(w, http.StatusNotImplemented, "the wizard is not available")
		return
	}
	var in struct {
		Workdir     string `json:"workdir"`
		Description string `json:"description"`
		Model       string `json:"model"`
	}
	if err := decode(r, &in); err != nil {
		fail(w, err)
		return
	}
	sess, err := s.wizard.Start(in.Workdir, in.Description, in.Model)
	if err != nil {
		setupFail(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, sess)
}

func (s *Server) getSetup(w http.ResponseWriter, r *http.Request) {
	if s.wizard == nil {
		writeErr(w, http.StatusNotFound, wizard.ErrNotFound.Error())
		return
	}
	sess, ok := s.wizard.Get(r.PathValue("id"))
	if !ok {
		writeErr(w, http.StatusNotFound, wizard.ErrNotFound.Error())
		return
	}
	writeJSON(w, http.StatusOK, sess)
}

func (s *Server) listSetups(w http.ResponseWriter, r *http.Request) {
	if s.wizard == nil {
		writeJSON(w, http.StatusOK, []wizard.Session{})
		return
	}
	writeJSON(w, http.StatusOK, s.wizard.List())
}

func (s *Server) dismissSetup(w http.ResponseWriter, r *http.Request) {
	if s.wizard == nil {
		writeErr(w, http.StatusNotFound, wizard.ErrNotFound.Error())
		return
	}
	if err := s.wizard.Dismiss(r.PathValue("id")); err != nil {
		setupFail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) stopSetup(w http.ResponseWriter, r *http.Request) {
	if s.wizard == nil {
		writeErr(w, http.StatusNotFound, wizard.ErrNotFound.Error())
		return
	}
	if err := s.wizard.Stop(r.PathValue("id")); err != nil {
		setupFail(w, err)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

// createProjectFromSetup creeaza proiectul si echipa din propunerea (editata)
// din wizard, intr-o singura tranzactie.
func (s *Server) createProjectFromSetup(w http.ResponseWriter, r *http.Request) {
	var in struct {
		SetupID  string          `json:"setupId"` // sesiunea din care vine; dispare din lista dupa creare
		Workdir  string          `json:"workdir"`
		Proposal wizard.Proposal `json:"proposal"`
	}
	if err := decode(r, &in); err != nil {
		fail(w, err)
		return
	}
	// utilizatorul poate bifa si shell/web, deci fara strictTools
	if err := wizard.ValidateProposal(&in.Proposal, false); err != nil {
		setupFail(w, err)
		return
	}
	dir, err := resolveWorkdir(in.Workdir, in.Proposal.Name, s.dirs.Workspaces)
	if err != nil {
		fail(w, err)
		return
	}
	p := store.Project{Name: in.Proposal.Name, Goal: in.Proposal.Goal, Context: in.Proposal.Context, Workdir: dir}
	if _, err := s.st.CreateProjectWithTeam(&p, in.Proposal.Members()); errors.Is(err, store.ErrConflict) {
		writeErr(w, http.StatusBadRequest, "an agent with this name already exists in the project")
		return
	} else if err != nil {
		fail(w, err)
		return
	}
	// ca la createProject: un folder ramas de la un proiect vechi cu acelasi id
	if err := os.RemoveAll(filepath.Join(s.dirs.Projects, strconv.FormatInt(p.ID, 10))); err != nil {
		log.Printf("cleaning up old project folder %d: %v", p.ID, err)
	}
	if s.wizard != nil && in.SetupID != "" {
		s.wizard.Dismiss(in.SetupID) // sesiunea poate sa fi expirat; nu conteaza
	}
	writeJSON(w, http.StatusCreated, p)
}
