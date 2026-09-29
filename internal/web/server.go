// Package web serveste UI-ul, API-ul REST, stream-urile SSE si serverul MCP.
package web

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"strconv"
	"unicode/utf8"

	"foreman/internal/config"
	"foreman/internal/events"
	"foreman/internal/runner"
	"foreman/internal/store"
	"foreman/internal/team"
)

//go:embed static
var staticFS embed.FS

type Server struct {
	st   *store.Store
	team *team.Team
	runs *runner.Manager
	hub  *events.Hub
	mcp  http.Handler
}

func New(st *store.Store, tm *team.Team, runs *runner.Manager, hub *events.Hub, mcpHandler http.Handler) *Server {
	return &Server{st: st, team: tm, runs: runs, hub: hub, mcp: mcpHandler}
}

func (s *Server) Handler() http.Handler {
	static, _ := fs.Sub(staticFS, "static")
	mux := http.NewServeMux()
	mux.Handle("GET /", http.FileServerFS(static))
	mux.HandleFunc("GET /api/info", s.info)

	mux.HandleFunc("GET /api/projects", s.listProjects)
	mux.HandleFunc("POST /api/projects", s.createProject)
	mux.HandleFunc("GET /api/projects/{id}", s.getProject)
	mux.HandleFunc("PUT /api/projects/{id}", s.updateProject)
	mux.HandleFunc("DELETE /api/projects/{id}", s.deleteProject)
	mux.HandleFunc("POST /api/projects/{id}/stop", s.stopProject)
	mux.HandleFunc("POST /api/projects/{id}/resume", s.resumeProject)
	mux.HandleFunc("GET /api/projects/{id}/events", s.projectEvents)

	mux.HandleFunc("POST /api/projects/{id}/agents", s.createAgent)
	mux.HandleFunc("PUT /api/agents/{id}", s.updateAgent)
	mux.HandleFunc("DELETE /api/agents/{id}", s.deleteAgent)
	mux.HandleFunc("GET /api/agents/{id}/messages", s.listMessages)
	mux.HandleFunc("POST /api/agents/{id}/messages", s.postMessage)
	mux.HandleFunc("POST /api/prompt-preview", s.promptPreview)

	mux.HandleFunc("POST /api/tasks/{id}/accept", s.acceptTask)
	mux.HandleFunc("POST /api/tasks/{id}/unblock", s.unblockTask)

	mux.HandleFunc("GET /api/runs/{id}/events", s.streamRun)

	// MCP: POST pentru JSON-RPC; GET doar ca sa raspunda 405 (Claude incearca si GET).
	// Rutele sunt separate pe metoda ca sa nu intre in conflict cu "GET /".
	mux.Handle("POST /mcp/{token}", s.mcp)
	mux.Handle("GET /mcp/{token}", s.mcp)
	return mux
}

func (s *Server) info(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"name": config.AppName})
}

// badRequest e o eroare de validare; mesajul ajunge ca atare in UI (400).
type badRequest string

func (e badRequest) Error() string { return string(e) }

func maxLen(label, value string, max int) error {
	if utf8.RuneCountInString(value) > max {
		return badRequest(fmt.Sprintf("%s: maxim %d de caractere", label, max))
	}
	return nil
}

// fail traduce o eroare intr-un raspuns HTTP.
func fail(w http.ResponseWriter, err error) {
	var br badRequest
	switch {
	case errors.As(err, &br):
		writeErr(w, http.StatusBadRequest, br.Error())
	case errors.Is(err, store.ErrNotFound):
		writeErr(w, http.StatusNotFound, "nu exista")
	case errors.Is(err, team.ErrNotBlocked):
		writeErr(w, http.StatusConflict, err.Error())
	case errors.Is(err, store.ErrConflict):
		writeErr(w, http.StatusConflict, "conflict cu starea curenta")
	default:
		log.Printf("eroare API: %v", err)
		writeErr(w, http.StatusInternalServerError, "eroare interna, vezi logul serverului")
	}
}

func pathID(r *http.Request) (int64, error) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		return 0, badRequest("id invalid")
	}
	return id, nil
}

func decode(r *http.Request, v any) error {
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(v); err != nil {
		return badRequest("JSON invalid")
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
