package web

import (
	"database/sql"
	"errors"
	"net/http"

	"foreman/internal/prompt"
	"foreman/internal/store"
	"foreman/internal/team"
)

func (s *Server) getGlobalCommands(w http.ResponseWriter, r *http.Request) {
	rules, err := s.st.CommandRules(0)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, rules)
}

func (s *Server) putGlobalCommands(w http.ResponseWriter, r *http.Request) {
	s.putCommands(w, r, 0)
}

func (s *Server) resetGlobalCommands(w http.ResponseWriter, r *http.Request) {
	if err := s.st.ResetGlobalCommandRules(); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// putCommands valideaza si inlocuieste lista unui nivel (0 = global).
// O lista goala (sau null) e permisa: goleste nivelul.
func (s *Server) putCommands(w http.ResponseWriter, r *http.Request, projectID int64) {
	var in []store.CommandRule
	if err := decode(r, &in); err != nil {
		fail(w, err)
		return
	}
	rules, err := prompt.ValidateRules(in)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.st.SetCommandRules(projectID, rules); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// projectExists intoarce ErrNotFound pentru un proiect necunoscut.
func (s *Server) projectExists(id int64) error {
	if _, err := s.st.GetProject(id); errors.Is(err, sql.ErrNoRows) {
		return store.ErrNotFound
	} else {
		return err
	}
}

func (s *Server) getProjectCommands(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		fail(w, err)
		return
	}
	if err := s.projectExists(id); err != nil {
		fail(w, err)
		return
	}
	global, err := s.st.CommandRules(0)
	if err != nil {
		fail(w, err)
		return
	}
	project, err := s.st.CommandRules(id)
	if err != nil {
		fail(w, err)
		return
	}
	eff := prompt.EffectiveRules(global, project)
	writeJSON(w, http.StatusOK, map[string]any{"global": global, "project": project, "effective": eff, "warnings": prompt.RuleWarnings(eff)})
}

func (s *Server) putProjectCommands(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		fail(w, err)
		return
	}
	if err := s.projectExists(id); err != nil {
		fail(w, err)
		return
	}
	s.putCommands(w, r, id)
}

// accessCommand aplica o decizie pe o cerere de comanda; team cauta cererea
// (necunoscuta = 404, inchisa sau de alt fel = 409).
func (s *Server) accessCommand(w http.ResponseWriter, r *http.Request, act func(int64) error) {
	id, err := pathID(r)
	if err != nil {
		fail(w, err)
		return
	}
	if err := act(id); err != nil {
		if errors.Is(err, team.ErrNotCommandRequest) || errors.Is(err, team.ErrRequestClosed) || errors.Is(err, team.ErrRuleChanged) {
			writeErr(w, http.StatusConflict, err.Error())
			return
		}
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) allowCommandTask(w http.ResponseWriter, r *http.Request) {
	s.accessCommand(w, r, s.team.AllowCommandForTask)
}

func (s *Server) allowCommandProject(w http.ResponseWriter, r *http.Request) {
	s.accessCommand(w, r, s.team.AllowCommandInProject)
}

func (s *Server) blockCommandProject(w http.ResponseWriter, r *http.Request) {
	s.accessCommand(w, r, s.team.BlockCommandInProject)
}
