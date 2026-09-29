package runner

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"foreman/internal/agent"
)

var (
	ErrBusy     = errors.New("agentul are deja un run activ")
	ErrNotFound = errors.New("run inexistent")
)

// Run este o singura executie `claude -p`.
// Tine istoricul evenimentelor in memorie, ca un client SSE care
// se conecteaza tarziu (sau face refresh) sa primeasca tot de la inceput.
type Run struct {
	ID        string    `json:"id"`
	Agent     string    `json:"agent"`
	Prompt    string    `json:"prompt"`
	StartedAt time.Time `json:"startedAt"`

	mu      sync.Mutex
	history [][]byte
	subs    map[chan []byte]struct{}
	done    bool
	status  string
	stopped bool
	log     *os.File
	cancel  context.CancelFunc
}

// publish adauga un eveniment in istoric, in fisierul de log si il trimite
// tuturor abonatilor. Se apeleaza din doua goroutine (stdout si stderr),
// de aceea totul e sub mutex.
func (r *Run) publish(line []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.history = append(r.history, line)
	if r.log != nil {
		r.log.Write(line)
		r.log.Write([]byte("\n"))
	}
	for ch := range r.subs {
		select {
		case ch <- line:
		default:
			// clientul nu tine pasul (buffer plin). Pentru MVP sarim evenimentul;
			// un refresh in browser reincarca tot istoricul.
		}
	}
}

// Subscribe intoarce istoricul de pana acum si un channel pentru ce urmeaza.
// Daca run-ul s-a terminat deja, channel-ul e nil si done e true.
func (r *Run) Subscribe() (replay [][]byte, ch chan []byte, done bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	replay = append([][]byte(nil), r.history...)
	if r.done {
		return replay, nil, true
	}
	ch = make(chan []byte, 1024)
	r.subs[ch] = struct{}{}
	return replay, ch, false
}

func (r *Run) Unsubscribe(ch chan []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.subs[ch]; ok {
		delete(r.subs, ch)
		close(ch)
	}
}

func (r *Run) finish(status string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.done = true
	r.status = status
	for ch := range r.subs {
		delete(r.subs, ch)
		close(ch)
	}
	if r.log != nil {
		r.log.Close()
		r.log = nil
	}
}

func (r *Run) Status() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.done {
		return "running"
	}
	return r.status
}

// Manager tine evidenta tuturor run-urilor si a sesiunilor per agent.
// Pentru MVP totul e in memorie; in pasul urmator se muta in SQLite.
type Manager struct {
	claudeBin string
	logDir    string

	mu       sync.Mutex
	runs     map[string]*Run
	sessions map[string]string // agent -> session_id pentru --resume
	active   map[string]string // agent -> run id activ
}

func NewManager(claudeBin, logDir string) *Manager {
	return &Manager{
		claudeBin: claudeBin,
		logDir:    logDir,
		runs:      map[string]*Run{},
		sessions:  map[string]string{},
		active:    map[string]string{},
	}
}

func (m *Manager) Get(id string) (*Run, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.runs[id]
	if !ok {
		return nil, ErrNotFound
	}
	return r, nil
}

// AgentState intoarce sesiunea curenta si run-ul activ (daca exista).
func (m *Manager) AgentState(name string) (sessionID, activeRunID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.sessions[name], m.active[name]
}

func (m *Manager) ResetSession(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sessions, name)
}

// Start porneste `claude -p` in folderul agentului.
// Un singur run activ per agent, altfel doua procese ar continua
// aceeasi sesiune in paralel.
func (m *Manager) Start(a agent.Agent, prompt string) (*Run, error) {
	m.mu.Lock()
	if _, busy := m.active[a.Name]; busy {
		m.mu.Unlock()
		return nil, ErrBusy
	}
	sessionID := m.sessions[a.Name]
	id := newID()
	m.active[a.Name] = id // rezervam locul inainte sa deblocam
	m.mu.Unlock()

	release := func() {
		m.mu.Lock()
		delete(m.active, a.Name)
		m.mu.Unlock()
	}

	args := []string{"-p", prompt, "--output-format", "stream-json", "--verbose"}
	args = append(args, agentConfigArgs(a.Dir)...)
	if sessionID != "" {
		args = append(args, "--resume", sessionID)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, m.claudeBin, args...)
	cmd.Dir = a.Dir
	setProcessGroup(cmd)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		release()
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		cancel()
		release()
		return nil, err
	}

	logFile, err := os.Create(filepath.Join(m.logDir, id+".jsonl"))
	if err != nil {
		cancel()
		release()
		return nil, err
	}

	run := &Run{
		ID:        id,
		Agent:     a.Name,
		Prompt:    prompt,
		StartedAt: time.Now(),
		subs:      map[chan []byte]struct{}{},
		log:       logFile,
		cancel:    cancel,
	}

	if err := cmd.Start(); err != nil {
		logFile.Close()
		cancel()
		release()
		return nil, fmt.Errorf("nu pot porni claude: %w", err)
	}

	m.mu.Lock()
	m.runs[id] = run
	m.mu.Unlock()

	run.publish(mustJSON(map[string]any{
		"type":    "agentdash_start",
		"prompt":  prompt,
		"resumed": sessionID != "",
	}))

	go m.pump(run, cmd, stdout, stderr)
	return run, nil
}

func (m *Manager) pump(run *Run, cmd *exec.Cmd, stdout, stderr io.Reader) {
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		sc := bufio.NewScanner(stdout)
		// o linie stream-json poate depasi 64KB (ex. un fisier citit intreg)
		sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
		for sc.Scan() {
			line := append([]byte(nil), sc.Bytes()...)
			m.captureSession(run.Agent, line)
			run.publish(line)
		}
	}()

	go func() {
		defer wg.Done()
		sc := bufio.NewScanner(stderr)
		for sc.Scan() {
			run.publish(mustJSON(map[string]any{"type": "agentdash_stderr", "text": sc.Text()}))
		}
	}()

	wg.Wait()
	err := cmd.Wait()

	run.mu.Lock()
	stopped := run.stopped
	run.mu.Unlock()

	status := "success"
	switch {
	case stopped:
		status = "stopped"
	case err != nil:
		status = "error"
	}

	done := map[string]any{"type": "agentdash_done", "status": status}
	if err != nil && !stopped {
		done["error"] = err.Error()
	}
	run.publish(mustJSON(done))
	run.finish(status)

	m.mu.Lock()
	if m.active[run.Agent] == run.ID {
		delete(m.active, run.Agent)
	}
	m.mu.Unlock()
}

// captureSession extrage session_id din evenimentele claude
// (apare in "system/init" si in "result"), pentru --resume la urmatorul prompt.
func (m *Manager) captureSession(agentName string, line []byte) {
	var head struct {
		SessionID string `json:"session_id"`
	}
	if json.Unmarshal(line, &head) != nil || head.SessionID == "" {
		return
	}
	m.mu.Lock()
	m.sessions[agentName] = head.SessionID
	m.mu.Unlock()
}

func (m *Manager) Stop(id string) error {
	run, err := m.Get(id)
	if err != nil {
		return err
	}
	run.mu.Lock()
	run.stopped = true
	run.mu.Unlock()
	run.cancel()
	return nil
}

// Shutdown opreste toate run-urile active (la Ctrl+C).
func (m *Manager) Shutdown() {
	m.mu.Lock()
	ids := make([]string, 0, len(m.active))
	for _, id := range m.active {
		ids = append(ids, id)
	}
	m.mu.Unlock()
	for _, id := range ids {
		m.Stop(id)
	}
}

func newID() string {
	b := make([]byte, 3)
	rand.Read(b)
	return time.Now().Format("20060102-150405") + "-" + hex.EncodeToString(b)
}

func mustJSON(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}

// agentConfigArgs da explicit fisierele de config ale agentului.
// Claude Code ignora .claude/settings.json si .mcp.json dintr-un folder
// in care nu s-a acceptat "trust" interactiv. Fisierele date prin
// --settings / --mcp-config se aplica oricum. Cu --setting-sources user
// nu mai citeste settings-ul de proiect, deci dispare si avertismentul.
func agentConfigArgs(dir string) []string {
	args := []string{"--setting-sources", "user"}
	if p := filepath.Join(dir, ".claude", "settings.json"); fileExists(p) {
		args = append(args, "--settings", p)
	}
	if p := filepath.Join(dir, ".mcp.json"); fileExists(p) {
		args = append(args, "--mcp-config", p)
	}
	return args
}

func fileExists(p string) bool {
	info, err := os.Stat(p)
	return err == nil && !info.IsDir()
}
