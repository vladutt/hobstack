package setup

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
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

// Session e o analiza din wizard. Traieste doar in memorie.
type Session struct {
	ID          string    `json:"id"`
	RunID       string    `json:"runId"`
	Workdir     string    `json:"workdir"`
	Description string    `json:"description"`
	Status      string    `json:"status"`
	Proposal    *Proposal `json:"proposal"`
	Error       string    `json:"error"`
	CreatedAt   time.Time `json:"createdAt"`

	token   string
	tempDir string // folderul gol folosit cand nu exista folder de proiect
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
func (m *Manager) Start(workdir, description, model string) (Session, error) {
	workdir, description = strings.TrimSpace(workdir), strings.TrimSpace(description)
	if workdir == "" && description == "" {
		return Session{}, ValidationError("choose a folder or describe what you want to do")
	}
	if err := tooLong("description", description, 4000); err != nil {
		return Session{}, err
	}
	if !models.Valid(model) {
		return Session{}, ValidationError("unknown model: " + model)
	}
	if workdir != "" {
		dir, err := expandHome(workdir)
		if err != nil {
			return Session{}, err
		}
		if !filepath.IsAbs(dir) {
			return Session{}, ValidationError("the folder must be an absolute path")
		}
		if info, err := os.Stat(dir); err != nil || !info.IsDir() {
			return Session{}, ValidationError("the folder does not exist: " + dir)
		}
		// un folder gol nu spune nimic despre proiect: fara descriere n-avem ce analiza
		if entries, _ := os.ReadDir(dir); len(entries) == 0 && description == "" {
			return Session{}, ValidationError("the folder is empty: describe what you want to do")
		}
		workdir = filepath.Clean(dir)
	}

	m.cleanupOld()
	s := &Session{ID: randomHex(8), RunID: runner.NewID(), Workdir: workdir, Description: description,
		Status: StatusRunning, CreatedAt: time.Now(), token: randomHex(16)}
	cwd := workdir
	if cwd == "" {
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
	return []string{
		"-p", turnPrompt(s),
		"--output-format", "stream-json", "--verbose",
		"--model", model,
		"--append-system-prompt", systemPrompt,
		"--setting-sources", "user",
		"--settings", string(settings),
		"--mcp-config", prompt.MCPConfig(m.baseURL + "/mcp/" + s.token),
		"--strict-mcp-config",
	}
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
	defer m.mu.Unlock()
	s, ok := m.sessions[id]
	if !ok || s.Status != StatusRunning {
		return "", ErrNotFound
	}
	s.Proposal = &p // o propunere noua o inlocuieste pe cea veche
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
