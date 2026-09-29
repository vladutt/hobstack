package web

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"strings"
	"time"

	"agentdash/internal/agent"
	"agentdash/internal/config"
	"agentdash/internal/runner"
)

//go:embed static
var staticFS embed.FS

type Server struct {
	paths config.Paths
	runs  *runner.Manager
}

func New(paths config.Paths, runs *runner.Manager) *Server {
	return &Server{paths: paths, runs: runs}
}

func (s *Server) Handler() http.Handler {
	static, _ := fs.Sub(staticFS, "static")

	mux := http.NewServeMux()
	mux.Handle("GET /", http.FileServerFS(static))
	mux.HandleFunc("GET /api/agents", s.listAgents)
	mux.HandleFunc("POST /api/agents/{name}/runs", s.startRun)
	mux.HandleFunc("POST /api/agents/{name}/reset", s.resetSession)
	mux.HandleFunc("GET /api/runs/{id}/events", s.streamRun)
	mux.HandleFunc("POST /api/runs/{id}/stop", s.stopRun)
	return mux
}

type agentView struct {
	agent.Agent
	SessionID   string `json:"sessionId"`
	ActiveRunID string `json:"activeRunId"`
}

func (s *Server) listAgents(w http.ResponseWriter, r *http.Request) {
	agents, err := agent.List(s.paths.Agents)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	out := make([]agentView, 0, len(agents))
	for _, a := range agents {
		sid, active := s.runs.AgentState(a.Name)
		out = append(out, agentView{Agent: a, SessionID: sid, ActiveRunID: active})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) startRun(w http.ResponseWriter, r *http.Request) {
	a, err := agent.Get(s.paths.Agents, r.PathValue("name"))
	if err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}

	var body struct {
		Prompt string `json:"prompt"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.Prompt) == "" {
		writeErr(w, http.StatusBadRequest, errors.New("promptul lipseste"))
		return
	}

	run, err := s.runs.Start(a, body.Prompt)
	if errors.Is(err, runner.ErrBusy) {
		writeErr(w, http.StatusConflict, err)
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"runId": run.ID})
}

func (s *Server) resetSession(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if _, active := s.runs.AgentState(name); active != "" {
		writeErr(w, http.StatusConflict, runner.ErrBusy)
		return
	}
	s.runs.ResetSession(name)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) stopRun(w http.ResponseWriter, r *http.Request) {
	if err := s.runs.Stop(r.PathValue("id")); err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

// streamRun trimite evenimentele unui run prin Server-Sent Events.
// Mai intai tot istoricul, apoi live pana se termina run-ul.
func (s *Server) streamRun(w http.ResponseWriter, r *http.Request) {
	run, err := s.runs.Get(r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeErr(w, http.StatusInternalServerError, errors.New("streaming nesuportat"))
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	replay, ch, done := run.Subscribe()
	for _, line := range replay {
		fmt.Fprintf(w, "data: %s\n\n", line)
	}
	flusher.Flush()
	if done {
		return
	}
	defer run.Unsubscribe(ch)

	// ping periodic ca proxy-urile sau browserul sa nu inchida conexiunea
	ping := time.NewTicker(15 * time.Second)
	defer ping.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-ping.C:
			fmt.Fprint(w, ": ping\n\n")
			flusher.Flush()
		case line, ok := <-ch:
			if !ok {
				return
			}
			fmt.Fprintf(w, "data: %s\n\n", line)
			flusher.Flush()
		}
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]string{"error": err.Error()})
}
