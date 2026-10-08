package setup

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
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
	"foreman/internal/store"
)

const (
	StatusRunning = "running"
	StatusReady   = "ready"
	StatusFailed  = "failed"
	StatusStopped = "stopped"
)

var ErrNotFound = errors.New("the setup session does not exist (maybe the server was restarted)")

var (
	ErrBusy     = errors.New("Foreman is still working; wait for the answer")
	ErrNotReady = errors.New("the proposal is not ready yet")
	// ErrNothingToUndo: istoricul de schimbari al sesiunii e gol
	ErrNothingToUndo = errors.New("there is nothing to undo")
)

// ChatMessage: un mesaj din conversatia cu Foreman dupa analiza.
// Role: "user", "foreman" sau "event" (stop, eroare, undo).
type ChatMessage struct {
	Role string    `json:"role"`
	Text string    `json:"text"`
	At   time.Time `json:"at"`
}

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
	Model       string    `json:"model"`     // modelul analistului, pentru Retry
	ProjectID   int64     `json:"projectId"` // 0 = proiect nou; altfel modul folder (agenti pentru un folder existent)
	FolderID    int64     `json:"folderId"`
	Status      string    `json:"status"`
	Proposal    *Proposal `json:"proposal"`
	Error       string    `json:"error"`
	CreatedAt   time.Time `json:"createdAt"`

	Chat      []ChatMessage `json:"chat"`      // conversatia cu Foreman dupa analiza; mereu non-nil
	Busy      bool          `json:"busy"`      // un run de chat e in curs
	ChatRunID string        `json:"chatRunId"` // run-ul de chat in curs
	Version   int           `json:"version"`   // creste la fiecare propunere noua sau Undo

	claudeSession string              // sesiunea Claude a analistului, pentru --resume
	history       []*Proposal         // versiunile anterioare (Undo), maxim 20
	chatTools     map[string][]string // tool-urile permise per agent (litere mici) in chat-ul curent
	chatChanged   bool                // run-ul de chat curent a propus ceva

	token   string
	tempDir string   // folderul gol folosit cand nu exista folder de proiect; traieste cat sesiunea
	cwd     string   // folderul in care ruleaza analistul (Claude tine sesiunile pe cwd, pentru --resume)
	addDirs []string // celelalte foldere pe care analistul le poate citi (--add-dir)
	hidden  bool     // ascunsa din lista (Dismiss sau proiect creat din ea)

	existing    []string // modul folder: numele agentilor care exista deja
	brief       string   // modul folder: proiectul si echipa, pentru mesajul analistului
	projectName string   // modul folder: completeaza numele lipsa din propunere
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
	// sesiunile sunt doar in memorie: dupa repornire folderele temporare raman orfane
	if tempRoot != "" {
		if entries, err := os.ReadDir(tempRoot); err == nil {
			for _, e := range entries {
				if err := os.RemoveAll(filepath.Join(tempRoot, e.Name())); err != nil {
					log.Printf("setup: nu pot sterge %s: %v", e.Name(), err)
				}
			}
		}
	}
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
		Status: StatusRunning, CreatedAt: time.Now(), token: randomHex(16), Chat: []ChatMessage{}}
	if len(dirs) > 0 {
		s.cwd, s.addDirs = dirs[0], dirs[1:]
	} else {
		s.tempDir = filepath.Join(m.tempRoot, s.ID)
		if err := os.MkdirAll(s.tempDir, 0o755); err != nil {
			return Session{}, err
		}
		s.cwd = s.tempDir
	}

	m.mu.Lock()
	m.sessions[s.ID] = s
	m.tokens[s.token] = s.ID
	m.mu.Unlock()

	spec := runner.Spec{
		ID: s.RunID, Key: "setup-" + s.ID, Dir: s.cwd,
		Args:   m.args(s, model, turnPrompt(s)),
		Prompt: turnPrompt(s),
	}
	if _, err := m.starter.Start(spec, func(o runner.Outcome) { m.finish(s.ID, o) }); err != nil {
		m.finish(s.ID, runner.Outcome{RunID: s.RunID, Status: "failed", Error: err.Error()})
		return Session{}, err
	}
	return m.snapshot(s), nil
}

// FolderInput: proiectul existent, folderul analizat si echipa de acum.
type FolderInput struct {
	Project store.Project
	Folder  store.Folder
	Team    []store.Agent
}

// StartFolder porneste analistul pentru un folder dintr-un proiect existent:
// propune contextul folderului si agenti noi pentru el (modul folder).
func (m *Manager) StartFolder(in FolderInput, description, model string) (Session, error) {
	description = strings.TrimSpace(description)
	if err := tooLong("description", description, 4000); err != nil {
		return Session{}, err
	}
	if !models.Valid(model) {
		return Session{}, ValidationError("unknown model: " + model)
	}
	dir := filepath.Clean(in.Folder.Path)
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return Session{}, ValidationError("the folder does not exist: " + dir)
	}
	existing := make([]string, 0, len(in.Team))
	for _, a := range in.Team {
		existing = append(existing, a.Name)
	}
	m.cleanupOld()
	s := &Session{ID: randomHex(8), RunID: runner.NewID(), Title: in.Project.Name + " · " + in.Folder.Name,
		Workdirs: []string{dir}, Description: description, Model: model,
		ProjectID: in.Project.ID, FolderID: in.Folder.ID,
		Status: StatusRunning, CreatedAt: time.Now(), token: randomHex(16), Chat: []ChatMessage{},
		existing: existing, brief: folderBrief(in), projectName: in.Project.Name, cwd: dir}
	for _, f := range in.Project.Folders {
		if filepath.Clean(f.Path) != dir {
			s.addDirs = append(s.addDirs, f.Path) // celelalte foldere: doar citire
		}
	}
	m.mu.Lock()
	m.sessions[s.ID] = s
	m.tokens[s.token] = s.ID
	m.mu.Unlock()

	spec := runner.Spec{ID: s.RunID, Key: "setup-" + s.ID, Dir: s.cwd, Args: m.args(s, model, turnPrompt(s)), Prompt: turnPrompt(s)}
	if _, err := m.starter.Start(spec, func(o runner.Outcome) { m.finish(s.ID, o) }); err != nil {
		m.finish(s.ID, runner.Outcome{RunID: s.RunID, Status: "failed", Error: err.Error()})
		return Session{}, err
	}
	return m.snapshot(s), nil
}

// args: argumentele analistului pentru o tura (analiza sau chat). turn e mesajul turei.
func (m *Manager) args(s *Session, model, turn string) []string {
	settings, _ := json.Marshal(map[string]any{"permissions": map[string]any{
		"allow": []string{"Read", "Glob", "Grep", "LS", "mcp__team__propose_setup"},
		"deny": []string{"Write", "Edit", "MultiEdit", "NotebookEdit", "Bash", "WebFetch", "WebSearch",
			"Agent", "TaskCreate", "TaskUpdate", "TaskList", "TaskGet", "TodoWrite"},
	}})
	system := systemPrompt
	if s.ProjectID != 0 {
		system += "\n\n" + folderSystemPrompt
	}
	out := []string{
		"-p", turn,
		"--output-format", "stream-json", "--verbose",
		"--model", model,
		"--append-system-prompt", system,
		"--setting-sources", "user",
		// fara el, un tool nepermis blocheaza run-ul in asteptarea unei aprobari
		"--permission-mode", "dontAsk",
		"--settings", string(settings),
		"--mcp-config", prompt.MCPConfig(m.baseURL+"/mcp/"+s.token, nil),
		"--strict-mcp-config",
	}
	for _, dir := range s.addDirs {
		out = append(out, "--add-dir", dir) // analistul citeste si celelalte foldere
	}
	return out
}

// finish e apelata de runner cand analistul s-a oprit.
func (m *Manager) finish(id string, o runner.Outcome) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[id]
	if !ok {
		return
	}
	if s.Busy && o.RunID == s.ChatRunID {
		m.finishChat(s, o)
		return
	}
	if s.Status != StatusRunning {
		return
	}
	delete(m.tokens, s.token)
	// folderul temporar ramane: un --resume ulterior trebuie rulat in acelasi cwd
	if o.SessionID != "" {
		s.claudeSession = o.SessionID
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

// finishChat inchide un run de chat; statusul sesiunii ramane ready. Se cheama cu m.mu tinut.
func (m *Manager) finishChat(s *Session, o runner.Outcome) {
	delete(m.tokens, s.token)
	s.Busy, s.ChatRunID, s.chatTools = false, "", nil
	if o.SessionID != "" {
		s.claudeSession = o.SessionID
	}
	// un run care n-a schimbat propunerea nu lasa o intrare pentru Undo;
	// s.Proposal ramane versiunea trimisa (cu editarile utilizatorului)
	if !s.chatChanged && len(s.history) > 0 {
		s.history = s.history[:len(s.history)-1]
	}
	now := time.Now()
	switch {
	case o.Status == "stopped":
		s.Chat = append(s.Chat, ChatMessage{Role: "event", Text: "Stopped.", At: now})
	case o.Status == "failed":
		s.Chat = append(s.Chat, ChatMessage{Role: "event", Text: "Foreman failed: " + firstNonEmpty(o.Error, o.Result, "unknown error"), At: now})
	default:
		s.Chat = append(s.Chat, ChatMessage{Role: "foreman", Text: firstNonEmpty(o.Result, "Done."), At: now})
	}
}

// Chat trimite un mesaj lui Foreman (analistul, reluat cu --resume) despre
// propunere. current e propunerea din pagina, cu editarile utilizatorului.
func (m *Manager) Chat(id, message string, current Proposal) (Session, error) {
	message = strings.TrimSpace(message)
	if message == "" {
		return Session{}, ValidationError("the message is empty")
	}
	if err := tooLong("message", message, 4000); err != nil {
		return Session{}, err
	}
	m.mu.Lock()
	s, ok := m.sessions[id]
	if !ok {
		m.mu.Unlock()
		return Session{}, ErrNotFound
	}
	if s.Busy {
		m.mu.Unlock()
		return Session{}, ErrBusy
	}
	// o sesiune ascunsa poate sa-si fi pierdut folderul temporar
	if s.hidden || s.Status != StatusReady || s.claudeSession == "" {
		m.mu.Unlock()
		return Session{}, ErrNotReady
	}
	existing := s.existing
	m.mu.Unlock()

	// copie proprie: propunerea apelantului nu se imparte cu sesiunea
	current, err := cloneProposal(current)
	if err != nil {
		return Session{}, err
	}
	if err := ValidateProposal(&current, false, existing); err != nil {
		return Session{}, err
	}
	tools := map[string][]string{}
	for _, a := range current.Agents {
		tools[strings.ToLower(a.Name)] = a.Tools
	}
	turn, err := chatTurn(current, message)
	if err != nil {
		return Session{}, err
	}

	m.mu.Lock()
	if s.Busy { // alt mesaj a castigat intre timp
		m.mu.Unlock()
		return Session{}, ErrBusy
	}
	if s.hidden || s.Status != StatusReady { // un Dismiss intre cele doua lock-uri sterge folderul temporar
		m.mu.Unlock()
		return Session{}, ErrNotReady
	}
	// versiunea trimisa (cu editarile utilizatorului) devine propunerea curenta si
	// intra in istoric: Undo dupa schimbarea lui Foreman o readuce exact asa
	p := current
	s.Proposal = &p
	s.history = append(s.history, &p)
	if len(s.history) > 20 {
		s.history = s.history[len(s.history)-20:]
	}
	s.Chat = append(s.Chat, ChatMessage{Role: "user", Text: message, At: time.Now()})
	s.Busy, s.ChatRunID, s.chatTools, s.chatChanged = true, runner.NewID(), tools, false
	delete(m.tokens, s.token)
	s.token = randomHex(16)
	m.tokens[s.token] = s.ID
	args := append(m.args(s, s.Model, turn), "--resume", s.claudeSession)
	spec := runner.Spec{ID: s.ChatRunID, Key: "setup-" + s.ID, Dir: s.cwd, Args: args, Prompt: turn}
	m.mu.Unlock()

	if _, err := m.starter.Start(spec, func(o runner.Outcome) { m.finish(s.ID, o) }); err != nil {
		m.finish(s.ID, runner.Outcome{RunID: spec.ID, Status: "failed", Error: err.Error()})
		return Session{}, err
	}
	return m.snapshot(s), nil
}

// Undo readuce propunerea dinaintea ultimei schimbari.
func (m *Manager) Undo(id string) (Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[id]
	if !ok {
		return Session{}, ErrNotFound
	}
	if s.Busy {
		return Session{}, ErrBusy
	}
	if len(s.history) == 0 {
		return Session{}, ErrNothingToUndo
	}
	s.Proposal = s.history[len(s.history)-1]
	s.history = s.history[:len(s.history)-1]
	s.Version++
	s.Chat = append(s.Chat, ChatMessage{Role: "event", Text: "Undid the last change.", At: time.Now()})
	return *s, nil
}

// cloneProposal face o copie adanca (slice-urile nu se impart).
func cloneProposal(p Proposal) (Proposal, error) {
	raw, err := json.Marshal(p)
	if err != nil {
		return Proposal{}, err
	}
	var out Proposal
	err = json.Unmarshal(raw, &out)
	return out, err
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
	running, runID, tempDir := s.Status == StatusRunning, s.RunID, s.tempDir
	if s.Busy {
		running, runID = true, s.ChatRunID
	}
	m.mu.Unlock()
	if running {
		// folderul temporar se sterge la curatenie, dupa ce run-ul s-a oprit
		return m.starter.Stop(runID)
	}
	if tempDir != "" {
		os.RemoveAll(tempDir)
	}
	return nil
}

func (m *Manager) Stop(id string) error {
	m.mu.Lock()
	s, ok := m.sessions[id]
	runID := ""
	if ok {
		runID = s.RunID
		if s.Busy {
			runID = s.ChatRunID // opreste run-ul de chat in curs
		}
	}
	m.mu.Unlock()
	if !ok {
		return ErrNotFound
	}
	return m.starter.Stop(runID)
}

// cleanupOld sterge sesiunile terminate mai vechi de 24h.
func (m *Manager) cleanupOld() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, s := range m.sessions {
		if s.Status != StatusRunning && !s.Busy && time.Since(s.CreatedAt) > 24*time.Hour {
			delete(m.sessions, id)
			if s.tempDir != "" {
				os.RemoveAll(s.tempDir)
			}
		}
	}
}

// propose e apelat din MCP de analist.
func (m *Manager) propose(id string, raw json.RawMessage) (string, error) {
	var p Proposal
	if err := json.Unmarshal(raw, &p); err != nil {
		return "", ValidationError("the proposal is not valid JSON: " + err.Error())
	}
	m.mu.Lock()
	s, ok := m.sessions[id]
	var running, busy, folderMode bool
	var workdirs, existing []string
	var projectName string
	var chatTools map[string][]string
	if ok {
		running, workdirs, existing, folderMode = s.Status == StatusRunning, s.Workdirs, s.existing, s.ProjectID != 0
		projectName, busy, chatTools = s.projectName, s.Busy, s.chatTools
	}
	m.mu.Unlock()
	// se accepta in analiza (running) sau intr-un run de chat (busy)
	if !ok || !(running || busy) {
		return "", ErrNotFound
	}
	// Workdirs si existing nu se schimba dupa pornire
	if folderMode && strings.TrimSpace(p.Name) == "" {
		p.Name = projectName // analistul poate omite numele; folosim cel al proiectului
	}
	if folderMode {
		// in modul folder "agents[].folder" se ignora (toti agentii merg in folderul analizat)
		for i := range p.Agents {
			p.Agents[i].Folder = ""
		}
	}
	if busy {
		// in chat: Foreman pastreaza tool-urile alese de utilizator, dar nu adauga shell sau web
		if err := ValidateProposal(&p, false, existing); err != nil {
			return "", err
		}
		// redenumire: un singur agent trimis a disparut si unul singur e nou => e acelasi agent,
		// deci pastreaza tool-urile lui
		var newNames []string
		seen := map[string]bool{}
		for _, a := range p.Agents {
			n := strings.ToLower(a.Name)
			seen[n] = true
			if _, known := chatTools[n]; !known {
				newNames = append(newNames, n)
			}
		}
		var missing []string
		for n := range chatTools {
			if !seen[n] {
				missing = append(missing, n)
			}
		}
		renamedFrom := map[string]string{}
		if len(missing) == 1 && len(newNames) == 1 {
			renamedFrom[newNames[0]] = missing[0]
		}
		for _, a := range p.Agents {
			n := strings.ToLower(a.Name)
			allowed := chatTools[n]
			if old, ok := renamedFrom[n]; ok {
				allowed = chatTools[old]
			}
			for _, tool := range a.Tools {
				if tool == "write" || tool == "git" || slices.Contains(allowed, tool) {
					continue
				}
				return "", ValidationError(fmt.Sprintf("you may not add the shell or web tools (%s: %s); the user adds them", a.Name, tool))
			}
		}
	} else if err := ValidateProposal(&p, true, existing); err != nil {
		return "", err
	}
	check := CheckFolderPaths(&p, workdirs)
	if folderMode {
		check = CheckFolderProposal(&p, workdirs[0])
	}
	if check != nil {
		return "", check
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if s.Status != StatusRunning && !s.Busy {
		return "", ErrNotFound
	}
	s.Proposal = &p // o propunere noua o inlocuieste pe cea veche
	s.Version++
	if s.Busy {
		s.chatChanged = true
		// raspunsul lui din chat ajunge la utilizator
		return "Proposal received. Now reply to the user in one or two sentences with what you changed.", nil
	}
	if !folderMode {
		s.Title = p.Name // in modul folder titlul ramane "<proiect> · <folder>"
	}
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

// ProposeAgent: doar draft-ul AI de agent are tool-ul (vezi internal/draft).
func (b *backend) ProposeAgent(mcp.Caller, json.RawMessage) (string, error) {
	return "", errors.New("propose_agent is only for agent drafts")
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
