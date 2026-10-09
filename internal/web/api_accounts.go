package web

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"hobstack/internal/runner"
	"hobstack/internal/store"
)

// checkTimeout: cat asteptam raspunsul la verificarea unui cont
const checkTimeout = 60 * time.Second

type accountInput struct {
	Name  *string `json:"name"`
	Token *string `json:"token"`
}

func validAccountName(name string) error {
	if name == "" {
		return badRequest("the account needs a name")
	}
	return maxLen("account name", name, 40)
}

// validToken: token-ul din `claude setup-token`, lipit intreg (fara spatii in el).
func validToken(token string) error {
	if len(token) < 20 || len(token) > 2000 || strings.ContainsAny(token, " \t\r\n") {
		return badRequest("paste the whole token printed by `claude setup-token`")
	}
	return nil
}

func (s *Server) listAccounts(w http.ResponseWriter, r *http.Request) {
	list, err := s.st.Accounts()
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// defaultAccount: ce cont foloseste "Default" (CLI-ul logat), doar informativ.
func (s *Server) defaultAccount(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	st, err := s.runs.AuthStatus(ctx)
	if err != nil {
		log.Printf("claude auth status: %v", err)
		st = runner.AuthStatus{} // loggedIn false: UI-ul spune ca nu stim
	}
	writeJSON(w, http.StatusOK, st)
}

func (s *Server) createAccount(w http.ResponseWriter, r *http.Request) {
	var in accountInput
	if err := decode(r, &in); err != nil {
		fail(w, err)
		return
	}
	var name, token string
	if in.Name != nil {
		name = strings.TrimSpace(*in.Name)
	}
	if in.Token != nil {
		token = strings.TrimSpace(*in.Token)
	}
	for _, err := range []error{validAccountName(name), validToken(token)} {
		if err != nil {
			fail(w, err)
			return
		}
	}
	a, err := s.st.AddAccount(name, token)
	if errors.Is(err, store.ErrConflict) {
		writeErr(w, http.StatusConflict, "an account named "+name+" already exists")
		return
	}
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, s.checkAccount(a))
}

func (s *Server) updateAccount(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		fail(w, err)
		return
	}
	var in accountInput
	if err := decode(r, &in); err != nil {
		fail(w, err)
		return
	}
	var name, token string
	if in.Name != nil {
		name = strings.TrimSpace(*in.Name)
		if err := validAccountName(name); err != nil {
			fail(w, err)
			return
		}
	}
	if in.Token != nil {
		token = strings.TrimSpace(*in.Token)
		if err := validToken(token); err != nil {
			fail(w, err)
			return
		}
	}
	err = s.st.UpdateAccount(id, name, token)
	if errors.Is(err, store.ErrConflict) {
		writeErr(w, http.StatusConflict, "an account named "+name+" already exists")
		return
	}
	if err != nil {
		fail(w, err)
		return
	}
	a, err := s.st.Account(id)
	if err != nil {
		fail(w, err)
		return
	}
	if token != "" {
		a = s.checkAccount(a) // token nou: il verificam imediat
	}
	writeJSON(w, http.StatusOK, a)
}

func (s *Server) deleteAccount(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		fail(w, err)
		return
	}
	if err := s.st.DeleteAccount(id); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) checkAccountRoute(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		fail(w, err)
		return
	}
	a, err := s.st.Account(id)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, s.checkAccount(a))
}

// checkAccount verifica token-ul cu un mesaj scurt si salveaza rezultatul pe cont. Proiectele
// contului primesc un eveniment: cardul din "Needs you" apare sau dispare. Contextul nu vine
// din cerere: o pagina inchisa nu trebuie sa lase o eroare falsa ("did not answer in time").
func (s *Server) checkAccount(a store.Account) store.Account {
	ctx, cancel := context.WithTimeout(context.Background(), checkTimeout)
	defer cancel()
	msg := ""
	if err := s.runs.Check(ctx, a.Env()); err != nil {
		msg = err.Error()
	}
	if err := s.st.SetAccountCheck(a.ID, msg); err != nil {
		log.Printf("account %d: saving the check: %v", a.ID, err)
	}
	if ids, err := s.st.AccountProjects(a.ID); err == nil {
		for _, pid := range ids {
			s.hub.Publish(pid, "project", map[string]int64{"id": pid})
		}
	}
	if fresh, err := s.st.Account(a.ID); err == nil {
		return fresh
	}
	return a
}

// accountRef: 0 = Default (nil); altfel contul trebuie sa existe.
func (s *Server) accountRef(id int64) (*int64, error) {
	if id == 0 {
		return nil, nil
	}
	if _, err := s.st.Account(id); errors.Is(err, store.ErrNotFound) {
		return nil, badRequest("unknown Claude account; reload the page")
	} else if err != nil {
		return nil, err
	}
	return &id, nil
}

// projectEnv: mediul run-urilor pornite din web pentru proiect (draft, wizard pe folder).
func (s *Server) projectEnv(projectID int64) ([]string, error) {
	a, err := s.st.ProjectAccount(projectID)
	if err != nil {
		return nil, fmt.Errorf("Claude account: %w", err)
	}
	if a == nil {
		return nil, nil
	}
	return a.Env(), nil
}
