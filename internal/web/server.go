// Package web serveste UI-ul, API-ul REST, stream-urile SSE si serverul MCP.
package web

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"hobstack/internal/clipfiles"
	"hobstack/internal/codefiles"
	"hobstack/internal/config"
	"hobstack/internal/draft"
	"hobstack/internal/events"
	"hobstack/internal/library"
	"hobstack/internal/mcpregistry"
	"hobstack/internal/models"
	"hobstack/internal/runner"
	wizard "hobstack/internal/setup" // alias: in pachetul web, "setup" e helper-ul din teste
	"hobstack/internal/skillsrc"
	"hobstack/internal/store"
	"hobstack/internal/team"
)

//go:embed static
var staticFS embed.FS

// Dirs sunt folderele gestionate de aplicatie.
type Dirs struct {
	Projects   string // fisierele proiectelor: <Projects>/<id>/files
	Workspaces string // folderele de lucru create implicit: <Workspaces>/<slug>
}

type Server struct {
	st      *store.Store
	team    *team.Team
	runs    *runner.Manager
	hub     *events.Hub
	mcp     http.Handler
	dirs    Dirs
	pick    func(context.Context) (string, error) // dialogul nativ de ales folder
	canPick bool
	opener  Opener          // nil = butoanele "Deschide" nu sunt disponibile
	wizard  *wizard.Manager // nil = wizard-ul "Proiect nou cu AI" nu e disponibil
	drafts  *draft.Manager  // nil = "Let AI draft this agent" nu e disponibil
	lib     *library.Lib    // nil = rutele Library raspund 503

	registry *mcpregistry.Client // registrul MCP (cautare din Library -> Add MCP server)
	// catalog: copia locala a registrului (nil = cautarea merge doar la registru); catalogCtx e contextul
	// aplicatiei, folosit de "Update now" (sincronizarea nu se opreste cand se termina cererea HTTP)
	catalog    *mcpregistry.Catalog
	catalogCtx context.Context
	home       func() (string, error) // folderul utilizatorului pentru configurarile Claude (testele dau unul temporar)

	codeCache *codefiles.Cache // fisierele de cod pentru @ in chat (cateva secunde in memorie)

	skills *skillsrc.Remote // skills.sh si GitHub (Library -> Add skill)
	preps  *preparations    // skill-urile pregatite pentru Install / Update (doar in memorie)
	now    func() time.Time // ceasul pentru expirarea pregatirilor (testele dau unul fals)

	clip func(context.Context) ([]string, error) // caile fisierelor din clipboard-ul sistemului (testele dau unul fals)
}

func New(st *store.Store, tm *team.Team, runs *runner.Manager, hub *events.Hub, mcpHandler http.Handler,
	dirs Dirs, pick func(context.Context) (string, error), canPick bool) *Server {
	return &Server{st: st, team: tm, runs: runs, hub: hub, mcp: mcpHandler, dirs: dirs, pick: pick, canPick: canPick,
		codeCache: codefiles.NewCache(30 * time.Second),
		registry:  mcpregistry.NewClient(), home: os.UserHomeDir,
		skills: skillsrc.NewRemote(), preps: newPreparations(), now: time.Now, clip: clipfiles.Paths}
}

// SetSetup activeaza wizard-ul "Proiect nou cu AI".
func (s *Server) SetSetup(m *wizard.Manager) { s.wizard = m }

// SetCatalog activeaza catalogul local al registrului MCP; ctx traieste cat aplicatia (sincronizarile pornite din UI).
func (s *Server) SetCatalog(cat *mcpregistry.Catalog, ctx context.Context) {
	s.catalog = cat
	s.catalogCtx = ctx
}

// SetDraft activeaza "Let AI draft this agent".
func (s *Server) SetDraft(m *draft.Manager) { s.drafts = m }

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
	mux.HandleFunc("PUT /api/projects/{id}/favorite", s.setFavorite)
	mux.HandleFunc("POST /api/projects/{id}/stop", s.stopProject)
	mux.HandleFunc("POST /api/projects/{id}/resume", s.resumeProject)
	mux.HandleFunc("GET /api/projects/{id}/events", s.projectEvents)
	mux.HandleFunc("POST /api/projects/{id}/open", s.openProject)
	mux.HandleFunc("GET /api/projects/{id}/stats", s.getProjectStats)

	mux.HandleFunc("GET /api/setup", s.listSetups)
	mux.HandleFunc("POST /api/setup", s.startSetup)
	mux.HandleFunc("DELETE /api/setup/{id}", s.dismissSetup)
	mux.HandleFunc("GET /api/setup/{id}", s.getSetup)
	mux.HandleFunc("POST /api/setup/{id}/stop", s.stopSetup)
	mux.HandleFunc("POST /api/setup/{id}/chat", s.chatSetup)
	mux.HandleFunc("POST /api/setup/{id}/undo", s.undoSetup)
	mux.HandleFunc("POST /api/projects/{id}/agents/draft", s.startDraft)
	mux.HandleFunc("GET /api/drafts/{id}", s.getDraft)
	mux.HandleFunc("POST /api/drafts/{id}/stop", s.stopDraft)
	mux.HandleFunc("POST /api/projects/{id}/folders/{fid}/setup", s.startFolderSetup)
	mux.HandleFunc("POST /api/projects/{id}/agents/setup", s.addAgentsFromSetup)
	mux.HandleFunc("POST /api/projects/setup", s.createProjectFromSetup)

	mux.HandleFunc("POST /api/projects/{id}/agents", s.createAgent)
	mux.HandleFunc("PUT /api/agents/{id}", s.updateAgent)
	mux.HandleFunc("DELETE /api/agents/{id}", s.deleteAgent)
	mux.HandleFunc("PUT /api/agents/{id}/position", s.setAgentPosition)
	mux.HandleFunc("GET /api/agents/{id}/messages", s.listMessages)
	mux.HandleFunc("POST /api/agents/{id}/messages", s.postMessage)
	mux.HandleFunc("POST /api/agents/{id}/new-chat", s.newChat)
	mux.HandleFunc("POST /api/prompt-preview", s.promptPreview)

	mux.HandleFunc("GET /api/projects/{id}/files", s.listFiles)
	mux.HandleFunc("POST /api/projects/{id}/files", s.uploadFiles)
	mux.HandleFunc("GET /api/files/{id}", s.serveFile)
	mux.HandleFunc("DELETE /api/files/{id}", s.deleteFile)
	mux.HandleFunc("POST /api/pick-folder", s.pickFolder)
	mux.HandleFunc("POST /api/clipboard/files", s.clipboardFiles)
	mux.HandleFunc("POST /api/clipboard/file", s.clipboardFile)
	mux.HandleFunc("GET /api/folder-context", s.folderContext)

	mux.HandleFunc("PUT /api/projects/{id}/stages", s.saveStages)
	mux.HandleFunc("POST /api/projects/{id}/tasks", s.createUserTask)
	mux.HandleFunc("POST /api/projects/{id}/recurring", s.createRecurring)
	mux.HandleFunc("PUT /api/recurring/{id}", s.updateRecurring)
	mux.HandleFunc("DELETE /api/recurring/{id}", s.deleteRecurring)
	mux.HandleFunc("POST /api/recurring/{id}/run", s.runRecurring)
	mux.HandleFunc("GET /api/recurring/{id}/history", s.recurringHistory)
	mux.HandleFunc("GET /api/recurring/{id}/files", s.recurringFiles)
	mux.HandleFunc("GET /api/tasks/{id}", s.getTask)
	mux.HandleFunc("GET /api/tasks/{id}/changes", s.getTaskChanges)
	mux.HandleFunc("GET /api/tasks/{id}/modified", s.getTaskModified)
	mux.HandleFunc("GET /api/tasks/{id}/file", s.getTaskFile)
	mux.HandleFunc("GET /api/tasks/{id}/flow", s.getTaskFlow)
	mux.HandleFunc("GET /api/tasks/{id}/files", s.getTaskFiles)
	mux.HandleFunc("POST /api/tasks/{id}/files", s.uploadTaskFiles)
	mux.HandleFunc("GET /api/task-files/{id}", s.serveTaskFile)
	mux.HandleFunc("POST /api/tasks/{id}/decide", s.decideTask)
	mux.HandleFunc("POST /api/questions/{id}/answer", s.answerQuestion)
	mux.HandleFunc("POST /api/questions/{id}/dismiss", s.dismissQuestion)
	mux.HandleFunc("POST /api/access/{id}/dismiss", s.dismissAccess)
	mux.HandleFunc("POST /api/access/{id}/enable-shell", s.enableShell)
	mux.HandleFunc("POST /api/access/{id}/enable-packages", s.enablePackages)
	mux.HandleFunc("GET /api/settings/commands", s.getGlobalCommands)
	mux.HandleFunc("PUT /api/settings/commands", s.putGlobalCommands)
	mux.HandleFunc("POST /api/settings/commands/reset", s.resetGlobalCommands)
	mux.HandleFunc("GET /api/accounts", s.listAccounts)
	mux.HandleFunc("GET /api/accounts/default", s.defaultAccount)
	mux.HandleFunc("POST /api/accounts", s.createAccount)
	mux.HandleFunc("PUT /api/accounts/{id}", s.updateAccount)
	mux.HandleFunc("DELETE /api/accounts/{id}", s.deleteAccount)
	mux.HandleFunc("POST /api/accounts/{id}/check", s.checkAccountRoute)
	mux.HandleFunc("GET /api/projects/{id}/commands", s.getProjectCommands)
	mux.HandleFunc("PUT /api/projects/{id}/commands", s.putProjectCommands)
	mux.HandleFunc("POST /api/access/{id}/allow-task", s.allowCommandTask)
	mux.HandleFunc("POST /api/access/{id}/allow-project", s.allowCommandProject)
	mux.HandleFunc("POST /api/access/{id}/block-project", s.blockCommandProject)
	mux.HandleFunc("GET /api/projects/{id}/links", s.listLinks)
	mux.HandleFunc("POST /api/projects/{id}/links", s.createLink)
	mux.HandleFunc("DELETE /api/projects/{id}/links/{other}", s.deleteLink)
	mux.HandleFunc("POST /api/project-requests/{id}/approve", s.approveProjectRequest)
	mux.HandleFunc("POST /api/project-requests/{id}/dismiss", s.dismissProjectRequest)
	mux.HandleFunc("POST /api/agent-requests/{id}/approve", s.approveAgentRequest)
	mux.HandleFunc("POST /api/agent-requests/{id}/dismiss", s.dismissAgentRequest)
	mux.HandleFunc("POST /api/tasks/{id}/assign", s.assignTask)
	mux.HandleFunc("POST /api/tasks/{id}/accept", s.acceptTask)
	mux.HandleFunc("POST /api/tasks/{id}/move", s.moveTask)
	mux.HandleFunc("DELETE /api/tasks/{id}", s.deleteTask)
	mux.HandleFunc("POST /api/tasks/{id}/restore", s.restoreTask)
	mux.HandleFunc("GET /api/projects/{id}/archived", s.listArchived)
	mux.HandleFunc("GET /api/projects/{id}/code-files", s.codeFiles)
	mux.HandleFunc("GET /api/projects/{id}/tree", s.getProjectTree)
	mux.HandleFunc("GET /api/projects/{id}/code-file", s.getCodeFile)
	mux.HandleFunc("POST /api/tasks/{id}/unblock", s.unblockTask)
	mux.HandleFunc("POST /api/tasks/{id}/nudge", s.nudgeTask)
	mux.HandleFunc("POST /api/tasks/{id}/comments", s.commentTask)
	mux.HandleFunc("PUT /api/tasks/{id}/todos/{pos}", s.setTodo)

	mux.HandleFunc("GET /api/library", s.listLibrary)
	mux.HandleFunc("POST /api/library", s.createLibraryItem)
	mux.HandleFunc("GET /api/library/registry", s.searchRegistry)
	mux.HandleFunc("GET /api/library/registry/status", s.registryStatus)
	mux.HandleFunc("POST /api/library/registry/sync", s.syncRegistry)
	mux.HandleFunc("GET /api/library/local-mcp", s.localMCP)
	mux.HandleFunc("POST /api/library/install-mcp", s.installMCP)
	mux.HandleFunc("GET /api/library/skills/search", s.searchSkills)
	mux.HandleFunc("GET /api/library/skills/local", s.localSkills)
	mux.HandleFunc("POST /api/library/skills/prepare", s.prepareSkill)
	mux.HandleFunc("GET /api/library/skills/prepare/{token}/file", s.prepFile)
	mux.HandleFunc("DELETE /api/library/skills/prepare/{token}", s.dropPrep)
	mux.HandleFunc("POST /api/library/skills/install", s.installSkill)
	mux.HandleFunc("POST /api/library/items/{id}/source/check", s.checkSource)
	mux.HandleFunc("POST /api/library/items/{id}/source/prepare", s.prepareSourceUpdate)
	mux.HandleFunc("POST /api/library/items/{id}/source/update", s.updateFromSource)
	mux.HandleFunc("GET /api/library/{id}", s.getLibraryItem)
	mux.HandleFunc("GET /api/library/{id}/files/{path...}", s.libraryFile)
	mux.HandleFunc("POST /api/library/{id}/publish", s.publishLibraryItem)
	mux.HandleFunc("POST /api/library/{id}/decline", s.declineLibraryItem)
	mux.HandleFunc("POST /api/library/{id}/unpublish", s.unpublishLibraryItem)
	mux.HandleFunc("GET /api/projects/{id}/library", s.listProjectLibrary)
	mux.HandleFunc("PUT /api/projects/{id}/library/{item}", s.putProjectLibrary)

	mux.HandleFunc("GET /api/runs/{id}/events", s.streamRun)

	// MCP: POST pentru JSON-RPC; GET doar ca sa raspunda 405 (Claude incearca si GET).
	// Rutele sunt separate pe metoda ca sa nu intre in conflict cu "GET /".
	mux.Handle("POST /mcp/{token}", s.mcp)
	mux.Handle("GET /mcp/{token}", s.mcp)
	return guard(mux)
}

// guard apara API-ul de cereri cross-site: un site din browser poate trimite
// POST "simplu" (text/plain) catre 127.0.0.1, iar DNS rebinding ocoleste si
// politica de origine. De aceea verificam Host si Content-Type.
func guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Host: doar 127.0.0.1 sau localhost, pe toate rutele (inclusiv /mcp si static)
		host := r.Host
		if h, _, err := net.SplitHostPort(r.Host); err == nil {
			host = h
		}
		if host != "127.0.0.1" && host != "localhost" {
			writeErr(w, http.StatusForbidden, "host not allowed")
			return
		}
		// ".." intr-o cale /api: ServeMux ar raspunde cu redirect, noi refuzam direct
		if strings.HasPrefix(r.URL.Path, "/api/") {
			for _, seg := range strings.Split(r.URL.Path, "/") {
				if seg == ".." {
					writeErr(w, http.StatusBadRequest, "bad path")
					return
				}
			}
		}
		// Content-Type: cererile care schimba starea trebuie sa fie JSON, chiar
		// si fara body (stop/resume/accept), sau multipart cu header custom
		// (un formular din alt site nu poate trimite X-HobStack). /mcp e exceptat.
		if strings.HasPrefix(r.URL.Path, "/api/") && r.Method != http.MethodGet {
			ct := r.Header.Get("Content-Type")
			ok := strings.HasPrefix(ct, "application/json") ||
				(strings.HasPrefix(ct, "multipart/form-data") && r.Header.Get("X-HobStack") == "1")
			if !ok {
				writeErr(w, http.StatusUnsupportedMediaType, "Content-Type must be application/json (or multipart with X-HobStack: 1)")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) info(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"name": config.DisplayName, "canPickFolder": s.canPick, "models": models.All, "editors": s.editors(), "os": runtime.GOOS})
}

// badRequest e o eroare de validare; mesajul ajunge ca atare in UI (400).
type badRequest string

func (e badRequest) Error() string { return string(e) }

func maxLen(label, value string, max int) error {
	if utf8.RuneCountInString(value) > max {
		return badRequest(fmt.Sprintf("%s: at most %d characters", label, max))
	}
	return nil
}

// fail traduce o eroare intr-un raspuns HTTP.
func fail(w http.ResponseWriter, err error) {
	var br badRequest
	var le store.LinkError
	var re team.RecurringError
	var ae team.AssignError
	var tmf team.TooManyFilesError
	var are team.AgentRequestError
	var pre team.ProjectRequestError
	switch {
	case errors.As(err, &br):
		writeErr(w, http.StatusBadRequest, br.Error())
	case errors.As(err, &re), errors.As(err, &ae), errors.As(err, &tmf):
		writeErr(w, http.StatusBadRequest, err.Error())
	case errors.As(err, &le):
		writeErr(w, http.StatusBadRequest, le.Error())
	case errors.As(err, &are):
		writeErr(w, http.StatusConflict, are.Error())
	case errors.As(err, &pre):
		writeErr(w, http.StatusConflict, pre.Error())
	case errors.Is(err, store.ErrSameProject):
		writeErr(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, store.ErrAccountInUse):
		writeErr(w, http.StatusConflict, err.Error())
	case errors.Is(err, store.ErrNotFound):
		writeErr(w, http.StatusNotFound, "not found")
	case errors.Is(err, team.ErrNotBlocked), errors.Is(err, team.ErrNotStuck), errors.Is(err, team.ErrAgentBusy):
		writeErr(w, http.StatusConflict, err.Error())
	case errors.Is(err, store.ErrConflict):
		writeErr(w, http.StatusConflict, "conflict with the current state")
	default:
		log.Printf("API error: %v", err)
		writeErr(w, http.StatusInternalServerError, "internal error, see the server log")
	}
}

func pathID(r *http.Request) (int64, error) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		return 0, badRequest("invalid id")
	}
	return id, nil
}

func decode(r *http.Request, v any) error { return decodeMax(r, v, 1<<20) }

// decodeMax: ca decode, cu alta limita de corp.
func decodeMax(r *http.Request, v any, max int64) error {
	if err := json.NewDecoder(io.LimitReader(r.Body, max)).Decode(v); err != nil {
		return badRequest("invalid JSON")
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
