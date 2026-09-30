package web

import (
	"errors"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"slices"
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
		Workdir     string   `json:"workdir"` // vechi: un singur folder
		Workdirs    []string `json:"workdirs"`
		Description string   `json:"description"`
		Model       string   `json:"model"`
	}
	if err := decode(r, &in); err != nil {
		fail(w, err)
		return
	}
	if len(in.Workdirs) == 0 && in.Workdir != "" {
		in.Workdirs = []string{in.Workdir} // API-ul vechi: un singur folder
	}
	sess, err := s.wizard.Start(in.Workdirs, in.Description, in.Model)
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
	if err := wizard.ValidateProposal(&in.Proposal, false, nil); err != nil {
		setupFail(w, err)
		return
	}
	folders := slices.Clone(in.Proposal.Folders) // copie: nu modificam propunerea
	if len(folders) == 0 {
		folders = []wizard.ProposedFolder{{Path: in.Workdir}} // gol = folder nou in ~/Foreman
	} else if len(folders) == 1 && folders[0].Path == "" {
		folders[0].Path = in.Workdir // analistul a lasat calea goala: folderul ales de utilizator
	}
	p := store.Project{Name: in.Proposal.Name, Goal: in.Proposal.Goal, Context: in.Proposal.Context}
	seen := map[string]bool{}
	for i, f := range folders {
		hint := in.Proposal.Name
		if i > 0 {
			hint += " " + f.Name
		}
		dir, err := resolveWorkdir(f.Path, hint, s.dirs.Workspaces)
		if err != nil {
			fail(w, err)
			return
		}
		if seen[dir] {
			fail(w, badRequest("two folders with the path "+dir))
			return
		}
		seen[dir] = true
		if err := checkNotNested(p.Folders, dir); err != nil {
			fail(w, err)
			return
		}
		name := f.Name
		if name == "" {
			name = store.FolderName(dir)
		}
		p.Folders = append(p.Folders, store.Folder{Name: name, Path: dir, Context: f.Context})
	}
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
