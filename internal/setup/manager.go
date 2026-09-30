package setup

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"foreman/internal/mcp"
	"foreman/internal/models"
	"foreman/internal/prompt"
	"foreman/internal/runner"
)

const (
	StatusRunning = "running"
	StatusReady   = "ready"
	StatusFailed  = "failed"
	StatusStopped = "stopped"
)

var ErrNotFound = errors.New("the setup session does not exist (maybe the server was restarted)")

// Starter e partea din runner.Manager de care avem nevoie (inlocuita in teste).
type Starter interface {
	Start(spec runner.Spec, onDone func(runner.Outcome)) (*runner.Run, error)
	Stop(runID string) error
}

// Session e o analiza din wizard. Traieste doar in memorie. Ruleaza pe server
// si cand pagina wizard-ului e inchisa; pagina Proiecte o arata ca un card.
type Session struct {
	ID          string    `json:"id"`
	RunID       string    `json:"runId"`
	Title       string    `json:"title"` // pentru card: numele propus, altfel folderul sau descrierea
	Workdirs    []string  `json:"workdirs"`
	Description string    `json:"description"`
	Model       string    `json:"model"` // modelul analistului, pentru Retry
	Status      string    `json:"status"`
	Proposal    *Proposal `json:"proposal"`
	Error       string    `json:"error"`
	CreatedAt   time.Time `json:"createdAt"`

	token   string
	tempDir string // folderul gol folosit cand nu exista folder de proiect
	hidden  bool   // ascunsa din lista (Dismiss sau proiect creat din ea)
}

type Manager struct {
	starter  Starter
	baseURL  string
	tempRoot string

	mu       sync.Mutex
	sessions map[string]*Session
	tokens   map[string]string // token MCP -> id sesiune
}

func New(st Starter, baseURL, tempRoot string) *Manager {
	return &Manager{starter: st, baseURL: baseURL, tempRoot: tempRoot,
		sessions: map[string]*Session{}, tokens: map[string]string{}}
}

func randomHex(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// Start valideaza cererea si porneste analistul.
func (m *Manager) Start(workdirs []string, description, model string) (Session, error) {
	description = strings.TrimSpace(description)
	if err := tooLong("description", description, 4000); err != nil {
		return Session{}, err
	}
	if !models.Valid(model) {
		return Session{}, ValidationError("unknown model: " + model)
	}
	dirs := []string{}
	for _, w := range workdirs {
		if w = strings.TrimSpace(w); w == "" {
			continue
		}
		dir, err := expandHome(w)
		if err != nil {
			return Session{}, err
		}
		if !filepath.IsAbs(dir) {
			return Session{}, ValidationError("the folder must be an absolute path: " + w)
		}
		if info, err := os.Stat(dir); err != nil || !info.IsDir() {
			return Session{}, ValidationError("the folder does not exist: " + dir)
		}
		if dir = filepath.Clean(dir); !slices.Contains(dirs, dir) {
			dirs = append(dirs, dir)
		}
	}
	if len(dirs) > maxFolders {
		return Session{}, ValidationError(fmt.Sprintf("at most %d folders", maxFolders))
	}
	if len(dirs) == 0 && description == "" {
		return Session{}, ValidationError("choose a folder or describe what you want to do")
	}
	// un singur folder gol nu spune nimic despre proiect: fara descriere n-avem ce analiza
	if len(dirs) == 1 && description == "" {
		if entries, _ := os.ReadDir(dirs[0]); len(entries) == 0 {
			return Session{}, ValidationError("the folder is empty: describe what you want to do")
		}
	}

	m.cleanupOld()
	s := &Session{ID: randomHex(8), RunID: runner.NewID(), Title: titleFor(dirs, description),
		Workdirs: dirs, Description: description, Model: model,
		Status: StatusRunning, CreatedAt: time.Now(), token: randomHex(16)}
	cwd := ""
	if len(dirs) > 0 {
		cwd = dirs[0]
	} else {
		s.tempDir = filepath.Join(m.tempRoot, s.ID)
		if err := os.MkdirAll(s.tempDir, 0o755); err != nil {
			return Session{}, err
		}
		cwd = s.tempDir
	}

	m.mu.Lock()
	m.sessions[s.ID] = s
	m.tokens[s.token] = s.ID
	m.mu.Unlock()

	spec := runner.Spec{
		ID: s.RunID, Key: "setup-" + s.ID, Dir: cwd,
		Args:   m.args(s, model),
		Prompt: turnPrompt(s),
	}
	if _, err := m.starter.Start(spec, func(o runner.Outcome) { m.finish(s.ID, o) }); err != nil {
		m.finish(s.ID, runner.Outcome{RunID: s.RunID, Status: "failed", Error: err.Error()})
		return Session{}, err
	}
	return m.snapshot(s), nil
}

func (m *Manager) args(s *Session, model string) []string {
	settings, _ := json.Marshal(map[string]any{"permissions": map[string]any{
		"allow": []string{"Read", "Glob", "Grep", "LS", "mcp__team__propose_setup"},
		"deny": []string{"Write", "Edit", "MultiEdit", "NotebookEdit", "Bash", "WebFetch", "WebSearch",
			"Agent", "TaskCreate", "TaskUpdate", "TaskList", "TaskGet", "TodoWrite"},
	}})
	out := []string{
		"-p", turnPrompt(s),
		"--output-format", "stream-json", "--verbose",
		"--model", model,
		"--append-system-prompt", systemPrompt,
		"--setting-sources", "user",
		// fara el, un tool nepermis blocheaza run-ul in asteptarea unei aprobari
		"--permission-mode", "dontAsk",
		"--settings", string(settings),
		"--mcp-config", prompt.MCPConfig(m.baseURL + "/mcp/" + s.token),
		"--strict-mcp-config",
	}
	for _, dir := range s.Workdirs[min(1, len(s.Workdirs)):] {
		out = append(out, "--add-dir", dir) // analistul citeste si celelalte foldere
	}
	return out
}

// finish e apelata de runner cand analistul s-a oprit.
func (m *Manager) finish(id string, o runner.Outcome) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[id]
	if !ok || s.Status != StatusRunning {
		return
	}
	delete(m.tokens, s.token)
	if s.tempDir != "" {
		os.RemoveAll(s.tempDir)
	}
	switch {
	case o.Status == "stopped":
		s.Status = StatusStopped
	case s.Proposal != nil:
		s.Status = StatusReady // chiar daca run-ul a esuat dupa ce a propus
	case o.Status == "failed":
		s.Status, s.Error = StatusFailed, firstNonEmpty(o.Error, o.Result, "the analysis failed")
	default:
		s.Status, s.Error = StatusFailed, "The AI did not propose anything. Try again."
	}
}

func firstNonEmpty(v ...string) string {
	for _, s := range v {
		if strings.TrimSpace(s) != "" {
			return s
		}
	}
	return ""
}

func (m *Manager) Get(id string) (Session, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[id]
	if !ok {
		return Session{}, false
	}
	return *s, true
}

func (m *Manager) snapshot(s *Session) Session {
	m.mu.Lock()
	defer m.mu.Unlock()
	return *s
}

// List intoarce sesiunile vizibile, cele mai noi primele (pentru pagina Proiecte).
func (m *Manager) List() []Session {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []Session{}
	for _, s := range m.sessions {
		if !s.hidden {
			out = append(out, *s)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out
}

// Dismiss scoate sesiunea din lista; daca analiza inca ruleaza, o opreste.
// Sesiunea ramane accesibila dupa id pana la curatenia de 24h.
func (m *Manager) Dismiss(id string) error {
	m.mu.Lock()
	s, ok := m.sessions[id]
	if !ok {
		m.mu.Unlock()
		return ErrNotFound
	}
	s.hidden = true
	running, runID := s.Status == StatusRunning, s.RunID
	m.mu.Unlock()
	if running {
		return m.starter.Stop(runID)
	}
	return nil
}

func (m *Manager) Stop(id string) error {
	m.mu.Lock()
	s, ok := m.sessions[id]
	m.mu.Unlock()
	if !ok {
		return ErrNotFound
	}
	return m.starter.Stop(s.RunID)
}

// cleanupOld sterge sesiunile terminate mai vechi de 24h.
func (m *Manager) cleanupOld() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, s := range m.sessions {
		if s.Status != StatusRunning && time.Since(s.CreatedAt) > 24*time.Hour {
			delete(m.sessions, id)
		}
	}
}

// propose e apelat din MCP de analist.
func (m *Manager) propose(id string, raw json.RawMessage) (string, error) {
	var p Proposal
	if err := json.Unmarshal(raw, &p); err != nil {
		return "", ValidationError("the proposal is not valid JSON: " + err.Error())
	}
	if err := ValidateProposal(&p, true); err != nil {
		return "", err
	}
	m.mu.Lock()
	s, ok := m.sessions[id]
	m.mu.Unlock()
	if !ok || s.Status != StatusRunning {
		return "", ErrNotFound
	}
	if err := CheckFolderPaths(&p, s.Workdirs); err != nil { // Workdirs nu se schimba dupa Start
		return "", err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if s.Status != StatusRunning {
		return "", ErrNotFound
	}
	s.Proposal = &p // o propunere noua o inlocuieste pe cea veche
	s.Title = p.Name
	return "Proposal received. You are done: do nothing else.", nil
}

// Wrap pune token-urile wizard-ului in fata backend-ului echipei.
func (m *Manager) Wrap(inner mcp.Backend) mcp.Backend { return &backend{Backend: inner, m: m} }

type backend struct {
	mcp.Backend
	m *Manager
}

func (b *backend) Resolve(token string) (mcp.Caller, bool) {
	b.m.mu.Lock()
	id, ok := b.m.tokens[token]
	b.m.mu.Unlock()
	if ok {
		return mcp.Caller{SetupID: id}, true
	}
	return b.Backend.Resolve(token)
}

func (b *backend) ProposeSetup(c mcp.Caller, args json.RawMessage) (string, error) {
	if c.SetupID == "" {
		return "", errors.New("propose_setup is only for the setup wizard")
	}
	return b.m.propose(c.SetupID, args)
}

// titleFor da un titlu pentru card pana vine propunerea: numele folderului,
// altfel prima linie din descriere, scurtata.
func titleFor(workdirs []string, description string) string {
	if len(workdirs) > 0 {
		return filepath.Base(workdirs[0])
	}
	line, _, _ := strings.Cut(description, "\n")
	line = strings.TrimSpace(line)
	if r := []rune(line); len(r) > 60 {
		line = strings.TrimSpace(string(r[:60])) + "…"
	}
	return line
}

// expandHome transforma ~/x in calea completa.
func expandHome(p string) (string, error) {
	if p != "~" && !strings.HasPrefix(p, "~/") {
		return p, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, strings.TrimPrefix(p, "~")), nil
}
