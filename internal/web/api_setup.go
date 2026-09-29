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
		writeErr(w, http.StatusNotImplemented, "wizard-ul nu e disponibil")
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
		writeErr(w, http.StatusBadRequest, "exista deja un agent cu acest nume in proiect")
		return
	} else if err != nil {
		fail(w, err)
		return
	}
	// ca la createProject: un folder ramas de la un proiect vechi cu acelasi id
	if err := os.RemoveAll(filepath.Join(s.dirs.Projects, strconv.FormatInt(p.ID, 10))); err != nil {
		log.Printf("curatare folder vechi proiect %d: %v", p.ID, err)
	}
	writeJSON(w, http.StatusCreated, p)
}
