// Package runner porneste procese `claude -p`, le citeste output-ul stream-json,
// il scrie in log si il transmite live abonatilor (SSE). Nu stie nimic despre
// proiecte sau agenti: primeste argumentele gata construite.
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
	"sync/atomic"
	"time"
)

var (
	ErrBusy     = errors.New("the agent already has an active run")
	ErrNotFound = errors.New("run not found")
)

// Spec descrie un run: ce argumente primeste claude si unde ruleaza.
type Spec struct {
	ID     string   // id-ul run-ului (NewID()); daca e gol se genereaza
	Key    string   // un singur run activ per cheie (ex. "agent-12")
	Dir    string   // working directory
	Args   []string // argumentele pentru claude, fara binar
	Prompt string   // mesajul turei, doar pentru evenimentul de start
	Env    []string // K=V adaugate in mediu; nevid = variabilele de cont mostenite sunt scoase (vezi procEnv)
}

// Command e o comanda Bash rulata de agent; OK e fals cand rezultatul a fost is_error (esuata sau blocata).
type Command struct {
	Text string
	OK   bool
}

// Outcome e rezultatul unui run terminat, extras din stream-json.
type Outcome struct {
	RunID     string
	Status    string // "done", "failed" sau "stopped"
	SessionID string // pentru --resume
	Result    string // textul final al agentului
	CostUSD   float64
	Tokens    int64 // intrare + cache + iesire, din "usage" al rezultatului
	Error     string
	Denials   []Denial     // actiuni refuzate de permisiuni
	Changes   []FileChange // fisiere modificate de agent, in ordinea rezultatelor
	Commands  []Command    // comenzile Bash rulate (si cele esuate)
	ToolUses  []string     // numele tuturor tool_use-urilor din run (ex. mcp__server__tool)
	RateLimit *RateLimit   // limitele contului din ultimul rate_limit_event; nil = run-ul nu le-a raportat
}

// RateLimit: cat din limitele contului Claude e folosit (0..1) si cand se reseteaza fiecare fereastra.
type RateLimit struct {
	Status         string // allowed, allowed_warning, rejected
	FiveHourUsed   float64
	FiveHourResets time.Time
	SevenDayUsed   float64
	SevenDayResets time.Time
}

// Run este o singura executie `claude -p`. Tine istoricul evenimentelor in
// memorie, ca un client SSE care se conecteaza tarziu sa primeasca tot.
type Run struct {
	ID        string    `json:"id"`
	Key       string    `json:"key"`
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

	activity atomic.Int64 // fisiere modificate + comenzi Bash pana acum (pentru report_done in timpul run-ului)
}

// publish adauga un eveniment in istoric si in log si il trimite abonatilor.
// Se apeleaza din doua goroutine (stdout si stderr), de aceea e sub mutex.
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
			// clientul nu tine pasul; un refresh reincarca tot istoricul
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

// Manager tine evidenta run-urilor din procesul curent.
type Manager struct {
	logDir string

	mu        sync.Mutex
	claudeBin string // "" = Claude Code nu e instalat inca (SetClaude il pune dupa instalare)
	runs   map[string]*Run
	active map[string]string // Key -> id-ul run-ului activ
}

func NewManager(claudeBin, logDir string) *Manager {
	return &Manager{
		claudeBin: claudeBin,
		logDir:    logDir,
		runs:      map[string]*Run{},
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

// Activity: cate fisiere a modificat si cate comenzi Bash a rulat pana acum run-ul. ok = false daca
// run-ul nu (mai) e cunoscut.
func (m *Manager) Activity(id string) (int, bool) {
	r, err := m.Get(id)
	if err != nil {
		return 0, false
	}
	return int(r.activity.Load()), true
}

// Start porneste claude cu argumentele din spec. onDone (poate fi nil) e
// apelat o singura data, dintr-o goroutine, dupa ce procesul s-a terminat.
func (m *Manager) Start(spec Spec, onDone func(Outcome)) (*Run, error) {
	if spec.ID == "" {
		spec.ID = NewID()
	}
	m.mu.Lock()
	if _, busy := m.active[spec.Key]; busy {
		m.mu.Unlock()
		return nil, ErrBusy
	}
	m.active[spec.Key] = spec.ID // rezervam locul inainte sa deblocam
	m.mu.Unlock()

	release := func() {
		m.mu.Lock()
		delete(m.active, spec.Key)
		m.mu.Unlock()
	}

	bin := m.Claude()
	if bin == "" {
		release()
		return nil, ErrNoClaude
	}
	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, bin, spec.Args...)
	cmd.Dir = spec.Dir
	cmd.Env = procEnv(spec.Env)
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
	logFile, err := os.Create(filepath.Join(m.logDir, spec.ID+".jsonl"))
	if err != nil {
		cancel()
		release()
		return nil, err
	}

	run := &Run{
		ID:        spec.ID,
		Key:       spec.Key,
		Prompt:    spec.Prompt,
		StartedAt: time.Now(),
		subs:      map[chan []byte]struct{}{},
		log:       logFile,
		cancel:    cancel,
	}

	if err := cmd.Start(); err != nil {
		logFile.Close()
		cancel()
		release()
		return nil, fmt.Errorf("cannot start claude: %w", err)
	}

	m.mu.Lock()
	m.runs[run.ID] = run
	m.mu.Unlock()

	run.publish(mustJSON(map[string]any{"type": "app_start", "prompt": spec.Prompt}))
	go m.pump(run, cmd, stdout, stderr, onDone)
	return run, nil
}

func (m *Manager) pump(run *Run, cmd *exec.Cmd, stdout, stderr io.Reader, onDone func(Outcome)) {
	var tr tracker // folosit doar de goroutine-ul de stdout, citit dupa wg.Wait
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		sc := bufio.NewScanner(stdout)
		// o linie stream-json poate depasi 64KB (ex. un fisier citit intreg)
		sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
		for sc.Scan() {
			line := append([]byte(nil), sc.Bytes()...)
			tr.observe(line)
			run.activity.Store(int64(len(tr.changes) + len(tr.commands)))
			run.publish(line)
		}
	}()

	go func() {
		defer wg.Done()
		sc := bufio.NewScanner(stderr)
		for sc.Scan() {
			run.publish(mustJSON(map[string]any{"type": "app_stderr", "text": sc.Text()}))
		}
	}()

	wg.Wait()
	waitErr := cmd.Wait()

	run.mu.Lock()
	stopped := run.stopped
	run.mu.Unlock()

	out := tr.outcome(run.ID, waitErr, stopped)
	done := map[string]any{"type": "app_done", "status": out.Status}
	if out.Error != "" {
		done["error"] = out.Error
	}
	run.publish(mustJSON(done))
	run.finish(out.Status)

	m.mu.Lock()
	if m.active[run.Key] == run.ID {
		delete(m.active, run.Key)
	}
	m.mu.Unlock()

	if onDone != nil {
		onDone(out)
	}
}

// Stop opreste run-ul (tot process group-ul). Rezultatul vine prin onDone.
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

// ActiveCount: cate run-uri ruleaza acum (agenti, wizard, draft).
func (m *Manager) ActiveCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.active)
}

// ErrNoClaude: Claude Code nu e instalat (sau HobStack nu l-a gasit).
var ErrNoClaude = errors.New("Claude Code (claude) is not installed; install it from HobStack's start page")

// SetClaude schimba binarul claude (dupa instalarea din aplicatie).
func (m *Manager) SetClaude(bin string) {
	m.mu.Lock()
	m.claudeBin = bin
	m.mu.Unlock()
}

// Claude intoarce binarul claude folosit ("" = lipseste).
func (m *Manager) Claude() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.claudeBin
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

// NewID genereaza un id de run sortabil dupa timp: 20260929-130056-af8b6e.
func NewID() string {
	b := make([]byte, 3)
	rand.Read(b)
	return time.Now().Format("20060102-150405") + "-" + hex.EncodeToString(b)
}

func mustJSON(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}
