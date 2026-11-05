package web

import (
	"context"
	"errors"
	"log"
	"net/http"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"hobstack/internal/claudecli"
)

// claudeSetup: Claude Code pe masina (pagina de start cand lipseste sau nu e logat). held = HobStack a pornit
// fara claude si a oprit run-urile noi (team.Hold) pana il gaseste.
type claudeSetup struct {
	home    string
	ctx     context.Context
	install *claudecli.Installer
	find    func(home string) string // testele dau alta cautare

	mu      sync.Mutex
	held    bool
	version map[string]string // calea -> claude --version
}

// SetClaude activeaza pagina Claude Code. missing = claude nu s-a gasit la pornire (run-urile sunt oprite cu
// team.Hold pana se instaleaza). ctx traieste cat aplicatia (installerul nu se opreste cu cererea HTTP).
func (s *Server) SetClaude(ctx context.Context, home string, missing bool) {
	s.claude = &claudeSetup{home: home, ctx: ctx, install: claudecli.NewInstaller(), find: claudecli.Find,
		held: missing, version: map[string]string{}}
}

// refind cauta iar claude; daca acum exista, runner-ul il primeste si run-urile pornesc.
func (s *Server) refindClaude() string {
	c := s.claude
	path := c.find(c.home)
	if path == "" {
		return ""
	}
	if s.runs.Claude() != path {
		s.runs.SetClaude(path)
		log.Printf("claude: %s", path)
	}
	c.mu.Lock()
	release := c.held
	c.held = false
	c.mu.Unlock()
	if release {
		s.team.Hold(false)
	}
	return path
}

type claudeStatus struct {
	Found          bool                   `json:"found"`
	Path           string                 `json:"path,omitempty"`
	Version        string                 `json:"version,omitempty"`
	LoggedIn       bool                   `json:"loggedIn"`
	Email          string                 `json:"email,omitempty"`
	InPath         bool                   `json:"inPath"`
	Dir            string                 `json:"dir"` // folderul care trebuie in PATH
	OS             string                 `json:"os"`
	InstallCommand []string               `json:"installCommand"`
	Install        claudecli.InstallState `json:"install"`
}

func (s *Server) claudeState(ctx context.Context) claudeStatus {
	c := s.claude
	st := claudeStatus{OS: runtime.GOOS, InstallCommand: claudecli.InstallCommand(), Install: c.install.State(),
		Dir: claudecli.LocalBin(c.home)}
	path := s.runs.Claude()
	if path == "" && !st.Install.Running {
		path = s.refindClaude()
	}
	if path == "" {
		return st
	}
	st.Found, st.Path, st.Dir = true, path, filepath.Dir(path)
	c.mu.Lock()
	v, ok := c.version[path]
	c.mu.Unlock()
	if !ok {
		if v, _ = claudecli.Version(ctx, path); v != "" {
			c.mu.Lock()
			c.version[path] = v
			c.mu.Unlock()
		}
	}
	st.Version = v
	actx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if a, err := s.runs.AuthStatus(actx); err == nil {
		st.LoggedIn, st.Email = a.LoggedIn, a.Email
	}
	st.InPath = claudecli.InPath(ctx, c.home, st.Dir)
	return st
}

func (s *Server) claudeAvailable(w http.ResponseWriter) bool {
	if s.claude == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "Claude Code setup is not available"})
		return false
	}
	return true
}

func (s *Server) getClaude(w http.ResponseWriter, r *http.Request) {
	if !s.claudeAvailable(w) {
		return
	}
	writeJSON(w, http.StatusOK, s.claudeState(r.Context()))
}

// installClaude ruleaza installerul oficial (doar la clic in UI); la final cauta iar claude.
func (s *Server) installClaude(w http.ResponseWriter, r *http.Request) {
	if !s.claudeAvailable(w) {
		return
	}
	err := s.claude.install.Start(s.claude.ctx, func(err error) {
		if path := s.refindClaude(); path == "" && err == nil {
			log.Printf("claude installer finished, but claude was not found")
		}
	})
	if errors.Is(err, claudecli.ErrRunning) {
		writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusAccepted, s.claudeState(r.Context()))
}

func (s *Server) checkClaude(w http.ResponseWriter, r *http.Request) {
	if !s.claudeAvailable(w) {
		return
	}
	s.claude.mu.Lock()
	s.claude.version = map[string]string{} // versiunea poate fi alta dupa o instalare din terminal
	s.claude.mu.Unlock()
	s.refindClaude()
	writeJSON(w, http.StatusOK, s.claudeState(r.Context()))
}

// claudePath pune folderul lui claude in PATH-ul utilizatorului (ca sa mearga si in terminal).
func (s *Server) claudePath(w http.ResponseWriter, r *http.Request) {
	if !s.claudeAvailable(w) {
		return
	}
	dir := claudecli.LocalBin(s.claude.home)
	if p := s.runs.Claude(); p != "" {
		dir = filepath.Dir(p)
	}
	msg, err := claudecli.AddToPath(r.Context(), s.claude.home, dir)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	st := s.claudeState(r.Context())
	writeJSON(w, http.StatusOK, map[string]any{"message": msg, "status": st})
}

// loginClaude deschide un terminal cu `claude auth login` (login-ul cere browserul si un terminal).
func (s *Server) loginClaude(w http.ResponseWriter, r *http.Request) {
	if !s.claudeAvailable(w) {
		return
	}
	path := s.runs.Claude()
	if path == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Install Claude Code first"})
		return
	}
	if err := claudecli.OpenLogin(path); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
