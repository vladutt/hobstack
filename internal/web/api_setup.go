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
	if errors.Is(err, wizard.ErrBusy) || errors.Is(err, wizard.ErrNotReady) || errors.Is(err, wizard.ErrNothingToUndo) {
		writeErr(w, http.StatusConflict, err.Error())
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
		AccountID   int64    `json:"accountId"` // 0 = Default
	}
	if err := decode(r, &in); err != nil {
		fail(w, err)
		return
	}
	if len(in.Workdirs) == 0 && in.Workdir != "" {
		in.Workdirs = []string{in.Workdir} // API-ul vechi: un singur folder
	}
	var acct wizard.Account
	if in.AccountID != 0 {
		a, err := s.st.Account(in.AccountID)
		if errors.Is(err, store.ErrNotFound) {
			fail(w, badRequest("unknown Claude account; reload the page"))
			return
		} else if err != nil {
			fail(w, err)
			return
		}
		acct = wizard.Account{ID: a.ID, Env: a.Env()}
	}
	sess, err := s.wizard.StartAs(acct, in.Workdirs, in.Description, in.Model)
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
	// contul ales la pasul 1 ramane pe sesiune (si cand pagina wizard-ului a fost redeschisa)
	if s.wizard != nil && in.SetupID != "" {
		if sess, ok := s.wizard.Get(in.SetupID); ok && sess.AccountID != 0 {
			ref, err := s.accountRef(sess.AccountID)
			if err != nil {
				fail(w, err)
				return
			}
			p.AccountID = ref
		}
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

// projectFolder incarca proiectul si folderul {fid} din el (404 daca lipsesc).
func (s *Server) projectFolder(w http.ResponseWriter, r *http.Request, folderID int64) (store.Project, store.Folder, bool) {
	pid, err := pathID(r)
	if err != nil {
		fail(w, err)
		return store.Project{}, store.Folder{}, false
	}
	p, err := s.st.GetProject(pid)
	if err != nil {
		fail(w, err)
		return store.Project{}, store.Folder{}, false
	}
	for _, f := range p.Folders {
		if f.ID == folderID {
			return p, f, true
		}
	}
	writeErr(w, http.StatusNotFound, "the folder no longer exists in this project")
	return store.Project{}, store.Folder{}, false
}

// startFolderSetup: analistul propune agenti pentru un folder al unui proiect existent.
func (s *Server) startFolderSetup(w http.ResponseWriter, r *http.Request) {
	if s.wizard == nil {
		writeErr(w, http.StatusNotImplemented, "the wizard is not available")
		return
	}
	fid, err := strconv.ParseInt(r.PathValue("fid"), 10, 64)
	if err != nil {
		fail(w, badRequest("invalid folder id"))
		return
	}
	p, folder, ok := s.projectFolder(w, r, fid)
	if !ok {
		return
	}
	var in struct {
		Description string `json:"description"`
		Model       string `json:"model"`
	}
	if err := decode(r, &in); err != nil {
		fail(w, err)
		return
	}
	team, err := s.st.ListAgents(p.ID)
	if err != nil {
		fail(w, err)
		return
	}
	env, err := s.projectEnv(p.ID)
	if err != nil {
		fail(w, err)
		return
	}
	sess, err := s.wizard.StartFolder(wizard.FolderInput{Project: p, Folder: folder, Team: team, Env: env}, in.Description, in.Model)
	if err != nil {
		setupFail(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, sess)
}

// addAgentsFromSetup adauga agentii propusi (editati) in proiectul existent.
func (s *Server) addAgentsFromSetup(w http.ResponseWriter, r *http.Request) {
	var in struct {
		SetupID       string          `json:"setupId"`
		FolderID      int64           `json:"folderId"`
		UpdateContext bool            `json:"updateContext"`
		Proposal      wizard.Proposal `json:"proposal"`
	}
	if err := decode(r, &in); err != nil {
		fail(w, err)
		return
	}
	p, folder, ok := s.projectFolder(w, r, in.FolderID)
	if !ok {
		return
	}
	team, err := s.st.ListAgents(p.ID)
	if err != nil {
		fail(w, err)
		return
	}
	names := make([]string, 0, len(team))
	for _, a := range team {
		names = append(names, a.Name)
	}
	// in modul folder "agents[].folder" se ignora
	for i := range in.Proposal.Agents {
		in.Proposal.Agents[i].Folder = ""
	}
	// utilizatorul poate bifa si shell/web, deci fara strictTools
	if err := wizard.ValidateProposal(&in.Proposal, false, names); err != nil {
		setupFail(w, err)
		return
	}
	var ctx *string
	if in.UpdateContext {
		c := ""
		if len(in.Proposal.Folders) > 0 {
			c = in.Proposal.Folders[0].Context
		}
		ctx = &c
	}
	agents, err := s.st.AddAgents(p.ID, folder.ID, in.Proposal.MembersIn(team), ctx)
	if errors.Is(err, store.ErrConflict) {
		writeErr(w, http.StatusConflict, "an agent with this name already exists in the project")
		return
	} else if err != nil {
		fail(w, err)
		return
	}
	if s.wizard != nil && in.SetupID != "" {
		s.wizard.Dismiss(in.SetupID) // sesiunea poate sa fi expirat; nu conteaza
	}
	s.hub.Publish(p.ID, "agent", map[string]int64{"id": agents[0].ID})
	s.hub.Publish(p.ID, "project", map[string]int64{"id": p.ID})
	writeJSON(w, http.StatusCreated, agents)
}

// chatSetup trimite un mesaj lui Foreman, impreuna cu propunerea curenta (cu editarile utilizatorului).
func (s *Server) chatSetup(w http.ResponseWriter, r *http.Request) {
	if s.wizard == nil {
		writeErr(w, http.StatusNotFound, wizard.ErrNotFound.Error())
		return
	}
	var in struct {
		Message  string          `json:"message"`
		Proposal wizard.Proposal `json:"proposal"`
	}
	if err := decode(r, &in); err != nil {
		fail(w, err)
		return
	}
	sess, err := s.wizard.Chat(r.PathValue("id"), in.Message, in.Proposal)
	if err != nil {
		setupFail(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, sess)
}

// undoSetup readuce propunerea de dinaintea ultimei schimbari a lui Foreman.
func (s *Server) undoSetup(w http.ResponseWriter, r *http.Request) {
	if s.wizard == nil {
		writeErr(w, http.StatusNotFound, wizard.ErrNotFound.Error())
		return
	}
	sess, err := s.wizard.Undo(r.PathValue("id"))
	if err != nil {
		setupFail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, sess)
}
