// Package draft: "Let AI draft this agent" de pe pagina Agent nou. Un run scurt,
// doar citire, cu un singur tool MCP (propose_agent). Sesiunile traiesc doar in memorie.
package draft

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"hobstack/internal/mcp"
	"hobstack/internal/models"
	"hobstack/internal/prompt"
	"hobstack/internal/runner"
	"hobstack/internal/store"
)

const (
	StatusRunning = "running"
	StatusReady   = "ready"
	StatusFailed  = "failed"
	StatusStopped = "stopped"
)

// Timeout: cat are voie sa dureze un draft
const Timeout = 90 * time.Second

var (
	ErrBusy     = errors.New("a draft is already running for this project")
	ErrNotFound = errors.New("the draft does not exist (maybe the server was restarted)")
)

type Starter interface {
	Start(spec runner.Spec, onDone func(runner.Outcome)) (*runner.Run, error)
	Stop(runID string) error
}

type Input struct {
	ProjectID  int64
	Dir        string
	Brief      string
	Names      []string
	Ask, Model string
	Env        []string // contul Claude al proiectului (nil = Default)
}

type Proposal struct {
	Name             string   `json:"name"`
	Role             string   `json:"role"`
	Responsibilities string   `json:"responsibilities"`
	Model            string   `json:"model"`
	ReportsTo        string   `json:"reportsTo"`
	ReviewedBy       string   `json:"reviewedBy"`
	Tools            []string `json:"tools"`
	Instructions     string   `json:"instructions"`
	Background       string   `json:"background"`
	DoneDef          string   `json:"doneDef"`
}

type Draft struct {
	ID        string    `json:"id"`
	ProjectID int64     `json:"projectId"`
	Status    string    `json:"status"`
	Proposal  *Proposal `json:"proposal"`
	Error     string    `json:"error"`

	runID, token, tempDir string
	names                 []string
	timedOut              bool
}

type Manager struct {
	starter  Starter
	baseURL  string
	tempRoot string
	// after: ceasul (inlocuit in teste); porneste fn dupa d
	after func(d time.Duration, fn func())

	mu     sync.Mutex
	drafts map[string]*Draft
	tokens map[string]string
}

func New(st Starter, baseURL, tempRoot string) *Manager {
	return &Manager{starter: st, baseURL: baseURL, tempRoot: tempRoot,
		after:  func(d time.Duration, fn func()) { time.AfterFunc(d, fn) },
		drafts: map[string]*Draft{}, tokens: map[string]string{}}
}

func randomHex(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func (m *Manager) Start(in Input) (Draft, error) {
	ask := strings.TrimSpace(in.Ask)
	if ask == "" || len([]rune(ask)) > 1000 {
		return Draft{}, errors.New("say in 1-1000 characters what the team is missing")
	}
	if !models.Valid(in.Model) {
		return Draft{}, errors.New("unknown model: " + in.Model)
	}
	m.mu.Lock()
	for _, d := range m.drafts {
		if d.ProjectID == in.ProjectID && d.Status == StatusRunning {
			m.mu.Unlock()
			return Draft{}, ErrBusy
		}
	}
	d := &Draft{ID: randomHex(8), ProjectID: in.ProjectID, Status: StatusRunning, runID: runner.NewID(), token: randomHex(16), names: in.Names}
	m.drafts[d.ID] = d
	m.tokens[d.token] = d.ID
	m.mu.Unlock()

	dir := in.Dir
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		// proiect fara folder pe disc (inca): ruleaza intr-un folder gol
		dir = filepath.Join(m.tempRoot, "draft-"+d.ID)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			m.fail(d.ID, err.Error())
			return Draft{}, err
		}
		m.mu.Lock()
		d.tempDir = dir // sub lock: finish() il citeste din alt thread
		m.mu.Unlock()
	}
	turn := turnPrompt(in.Brief, ask)
	spec := runner.Spec{ID: d.runID, Key: "draft-" + d.ID, Dir: dir, Args: m.args(d, in.Model, turn), Prompt: turn, Env: in.Env}
	if _, err := m.starter.Start(spec, func(o runner.Outcome) { m.finish(d.ID, o) }); err != nil {
		m.fail(d.ID, err.Error())
		return Draft{}, err
	}
	m.after(Timeout, func() {
		m.mu.Lock()
		running := d.Status == StatusRunning
		if running {
			d.timedOut = true
		}
		m.mu.Unlock()
		if running {
			m.starter.Stop(d.runID)
		}
	})
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.snapshot(d), nil
}

func (m *Manager) args(d *Draft, model, turn string) []string {
	settings, _ := json.Marshal(map[string]any{"permissions": map[string]any{
		"allow": []string{"Read", "Glob", "Grep", "LS", "mcp__team__propose_agent"},
		"deny": []string{"Write", "Edit", "MultiEdit", "NotebookEdit", "Bash", "WebFetch", "WebSearch",
			"Agent", "TaskCreate", "TaskUpdate", "TaskList", "TaskGet", "TodoWrite"},
	}})
	return []string{
		"-p", turn,
		"--output-format", "stream-json", "--verbose",
		"--model", model,
		"--append-system-prompt", systemPrompt,
		"--setting-sources", "user",
		"--permission-mode", "dontAsk",
		"--settings", string(settings),
		"--mcp-config", prompt.MCPConfig(m.baseURL+"/mcp/"+d.token, nil),
		"--strict-mcp-config",
	}
}

func (m *Manager) fail(id, msg string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if d, ok := m.drafts[id]; ok {
		d.Status, d.Error = StatusFailed, msg
		delete(m.tokens, d.token)
		if d.tempDir != "" {
			os.RemoveAll(d.tempDir)
		}
	}
}

func (m *Manager) finish(id string, o runner.Outcome) {
	m.mu.Lock()
	defer m.mu.Unlock()
	d, ok := m.drafts[id]
	if !ok || d.Status != StatusRunning {
		return
	}
	delete(m.tokens, d.token)
	if d.tempDir != "" {
		os.RemoveAll(d.tempDir)
	}
	switch {
	case d.Proposal != nil:
		d.Status = StatusReady
	case d.timedOut:
		d.Status, d.Error = StatusFailed, "The draft took too long. Try again with a shorter request."
	case o.Status == "stopped":
		d.Status = StatusStopped
	case o.Status == "failed":
		d.Status, d.Error = StatusFailed, firstNonEmpty(o.Error, o.Result, "the draft failed")
	default:
		d.Status, d.Error = StatusFailed, "The AI did not propose an agent. Try again."
	}
}

func (m *Manager) Get(id string) (Draft, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	d, ok := m.drafts[id]
	if !ok {
		return Draft{}, false
	}
	return m.snapshot(d), true
}

func (m *Manager) Stop(id string) error {
	m.mu.Lock()
	d, ok := m.drafts[id]
	m.mu.Unlock()
	if !ok {
		return ErrNotFound
	}
	return m.starter.Stop(d.runID) // runID nu se schimba dupa creare
}

// snapshot: o copie fara campurile interne; se cheama cu m.mu tinut
func (m *Manager) snapshot(d *Draft) Draft {
	out := Draft{ID: d.ID, ProjectID: d.ProjectID, Status: d.Status, Error: d.Error}
	if d.Proposal != nil {
		p := *d.Proposal
		out.Proposal = &p
	}
	return out
}

var allowedTools = []string{"write", "shell", "packages", "git", "web"}

func (m *Manager) propose(id string, raw json.RawMessage) (string, error) {
	var p Proposal
	if err := json.Unmarshal(raw, &p); err != nil {
		return "", errors.New("invalid proposal: " + err.Error())
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	d, ok := m.drafts[id]
	if !ok || d.Status != StatusRunning {
		return "", errors.New("this draft is no longer running")
	}
	p.Name = strings.TrimSpace(p.Name)
	has := func(name string) bool {
		return slices.ContainsFunc(d.names, func(n string) bool { return strings.EqualFold(n, strings.TrimSpace(name)) })
	}
	switch {
	case p.Name == "" || len([]rune(p.Name)) > 40:
		return "", errors.New("name: 1-40 characters")
	case store.ReservedAgentName(p.Name):
		return "", errors.New(store.ErrReservedName)
	case has(p.Name):
		return "", fmt.Errorf("an agent named %s already exists; pick another name", p.Name)
	case strings.TrimSpace(p.Role) == "" || len([]rune(p.Role)) > 80:
		return "", errors.New("role: 1-80 characters")
	case len([]rune(p.Responsibilities)) > 2000:
		return "", errors.New("responsibilities: at most 2000 characters")
	case len([]rune(p.Instructions)) > 4000 || len([]rune(p.Background)) > 4000 || len([]rune(p.DoneDef)) > 4000:
		return "", errors.New("instructions, background and doneDef: at most 4000 characters each")
	case p.ReportsTo != "" && !has(p.ReportsTo):
		return "", fmt.Errorf("reportsTo: %s is not in the team; use one of %s or leave it empty", p.ReportsTo, strings.Join(d.names, ", "))
	case p.ReviewedBy != "" && !has(p.ReviewedBy):
		return "", fmt.Errorf("reviewedBy: %s is not in the team", p.ReviewedBy)
	case p.Model != "" && !models.Valid(p.Model):
		return "", errors.New("model: opus, sonnet or haiku")
	}
	for _, t := range p.Tools {
		if !slices.Contains(allowedTools, t) {
			return "", fmt.Errorf("unknown tool %q; use %s", t, strings.Join(allowedTools, ", "))
		}
	}
	d.Proposal = &p
	return "Proposal received. You are done: do nothing else.", nil
}

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
		return mcp.Caller{DraftID: id}, true
	}
	return b.Backend.Resolve(token)
}

func (b *backend) ProposeAgent(c mcp.Caller, args json.RawMessage) (string, error) {
	if c.DraftID == "" {
		return "", errors.New("propose_agent is only for agent drafts")
	}
	return b.m.propose(c.DraftID, args)
}

func firstNonEmpty(s ...string) string {
	for _, x := range s {
		if strings.TrimSpace(x) != "" {
			return x
		}
	}
	return ""
}

const systemPrompt = `You design one new agent for an existing team of Claude Code agents. Read the project briefly ` +
	`(README, CLAUDE.md, the main folders) only if it helps; do not change anything. Then call ` +
	`mcp__team__propose_agent once. Rules: the name is short and not used by anyone in the team; the ` +
	`responsibilities do not overlap with other agents and say what the agent does not take; reportsTo is the lead ` +
	`whose area it fits (empty only if it should report to the user); tools: only what the work needs ` +
	`(write for code, shell for commands and tests, git for commits, web for research).`

func turnPrompt(brief, ask string) string {
	return brief + "\n\nWhat the user says the team is missing:\n" + ask
}
