// Package team coordoneaza echipele de agenti: implementeaza tool-urile MCP
// (create_task, report_done...), porneste run-urile cand e ceva de facut si
// actualizeaza starea cand se termina. E singurul pachet care leaga store,
// runner, mcp si prompt. Sursa de adevar e baza de date; in memorie tinem
// doar run-urile active si token-urile lor.
package team

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"foreman/internal/config"
	"foreman/internal/events"
	"foreman/internal/library"
	"foreman/internal/mcp"
	"foreman/internal/runner"
	"foreman/internal/store"
)

// Statusurile unui agent, afisate in UI si in lista echipei.
const (
	StatusWorking = "Working"
	StatusQueued  = "Queued"
	StatusIdle    = "Idle"
)

// Starter porneste si opreste procese claude. In productie e *runner.Manager,
// in teste un fals care doar inregistreaza apelurile.
type Starter interface {
	Start(spec runner.Spec, onDone func(runner.Outcome)) (*runner.Run, error)
	Stop(runID string) error
}

type Team struct {
	st      *store.Store
	runs    Starter
	hub     *events.Hub
	baseURL string // ex. http://127.0.0.1:7777, pentru URL-ul serverului MCP

	// filesRoot e radacina folderelor de proiect (config.Paths.Projects).
	// Goala = agentii nu primesc fisiere (cazul testelor).
	filesRoot string

	// lib construieste pluginul Library al proiectului; nil = fara Library (testele).
	lib *library.Lib

	// now da ora curenta (time.Now); ticker-ul detectorului de task-uri
	// blocate si web-ul o folosesc prin Now.
	now func() time.Time

	// work serializeaza toate schimbarile de stare: tool-uri MCP, final de run,
	// reconciliere, actiuni din UI. E mai simplu decat sa analizam fiecare
	// combinatie de goroutine, iar operatiile sunt oricum scurte.
	work sync.Mutex

	mu      sync.Mutex          // protejeaza campurile de mai jos
	active  map[int64]activeRun // agentID -> run-ul lui activ
	tokens  map[string]mcp.Caller
	pending map[int64]bool // proiecte care trebuie reconciliate
	wake    chan struct{}
}

type activeRun struct {
	runID     string
	projectID int64
	taskID    int64
	token     string
	request   *store.Message // ultimul mesaj al utilizatorului livrat la pornire (doar run-urile de chat)
	questions []int64        // intrebarile din alte proiecte livrate run-ului: nu poate intreba la randul lui (anti-bucla), iar la final se inchid
}

// setRequest retine mesajul utilizatorului care a pornit run-ul activ al agentului.
func (t *Team) setRequest(agentID int64, m store.Message) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if ar, ok := t.active[agentID]; ok {
		ar.request = &m
		t.active[agentID] = ar
	}
}

// addQuestions retine intrebarile (id-urile cererilor) livrate run-ului activ al agentului.
func (t *Team) addQuestions(agentID int64, ids []int64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if ar, ok := t.active[agentID]; ok {
		ar.questions = append(ar.questions, ids...)
		t.active[agentID] = ar
	}
}

// answeringOf: run-ul runID al agentului (inca activ) a primit o intrebare dintr-un proiect legat.
func (t *Team) answeringOf(agentID int64, runID string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	ar, ok := t.active[agentID]
	return ok && ar.runID == runID && len(ar.questions) > 0
}

// markAnswering: dupa ce run-ul a pornit cu notele date, retine intrebarile primite printre ele.
func (t *Team) markAnswering(agentID int64, notes []store.Message) {
	var ids []int64
	for _, n := range notes {
		if n.RequestID != nil {
			ids = append(ids, *n.RequestID)
		}
	}
	if len(ids) > 0 {
		t.addQuestions(agentID, ids)
	}
}

// requestOf intoarce mesajul retinut pentru run-ul runID al agentului, daca e inca activ.
func (t *Team) requestOf(agentID int64, runID string) (store.Message, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	ar, ok := t.active[agentID]
	if !ok || ar.runID != runID || ar.request == nil {
		return store.Message{}, false
	}
	return *ar.request, true
}

func New(st *store.Store, runs Starter, hub *events.Hub, baseURL string) *Team {
	return &Team{
		st:      st,
		runs:    runs,
		hub:     hub,
		baseURL: baseURL,
		now:     time.Now,
		active:  map[int64]activeRun{},
		tokens:  map[string]mcp.Caller{},
		pending: map[int64]bool{},
		wake:    make(chan struct{}, 1),
	}
}

// SetLibrary leaga Library de run-uri.
func (t *Team) SetLibrary(l *library.Lib) { t.lib = l }

// SetFilesRoot seteaza radacina cu fisierele proiectelor.
func (t *Team) SetFilesRoot(root string) { t.filesRoot = root }

// filesDir intoarce folderul cu fisierele unui proiect, sau "" fara radacina.
func (t *Team) filesDir(projectID int64) string {
	if t.filesRoot == "" {
		return ""
	}
	return config.ProjectFilesDir(t.filesRoot, projectID)
}

// Resolve gaseste agentul dupa token-ul din URL-ul MCP (implementeaza mcp.Backend).
func (t *Team) Resolve(token string) (mcp.Caller, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	c, ok := t.tokens[token]
	return c, ok
}

// Kick cere reconcilierea proiectului. Nu blocheaza: bucla din Run (Task 9)
// preia cererea; mai multe Kick-uri la rand se aduna intr-o singura reconciliere.
func (t *Team) Kick(projectID int64) {
	t.mu.Lock()
	t.pending[projectID] = true
	t.mu.Unlock()
	select {
	case t.wake <- struct{}{}:
	default: // exista deja un semnal in asteptare
	}
}

// ActiveRunID intoarce run-ul activ al agentului sau "".
func (t *Team) ActiveRunID(agentID int64) string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.active[agentID].runID
}

// ProjectActive spune daca vreun agent al proiectului ruleaza acum.
func (t *Team) ProjectActive(projectID int64) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, ar := range t.active {
		if ar.projectID == projectID {
			return true
		}
	}
	return false
}

func (t *Team) isActive(agentID int64) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	_, ok := t.active[agentID]
	return ok
}

// activeTaskID intoarce task-ul run-ului activ al agentului; 0 = fara run
// activ sau run fara task.
func (t *Team) activeTaskID(agentID int64) int64 {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.active[agentID].taskID
}

// ActiveTaskID: task-ul la care lucreaza acum agentul (0 = niciunul); pentru board.
func (t *Team) ActiveTaskID(agentID int64) int64 { return t.activeTaskID(agentID) }

// WorkingOn spune daca agentul asignat are acum un run activ pe task-ul dat.
func (t *Team) WorkingOn(taskID, assigneeID int64) bool {
	if taskID == 0 {
		return false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	ar, ok := t.active[assigneeID]
	return ok && ar.taskID == taskID
}

// AgentStatus intoarce statusul afisat si task-ul curent (in lucru sau urmatorul).
func (t *Team) AgentStatus(a store.Agent, tasks []store.Task) (string, *store.Task) {
	pending, _ := t.st.HasUndelivered(a.ID)
	var current *store.Task
	for _, want := range []string{store.StatusInProgress, store.StatusQueued} {
		for i := range tasks {
			if current == nil && tasks[i].AssigneeID == a.ID && tasks[i].Status == want {
				current = &tasks[i]
			}
		}
	}
	return t.statusOf(a, tasks, pending), current
}

func (t *Team) statusOf(a store.Agent, tasks []store.Task, pending bool) string {
	if t.isActive(a.ID) {
		return StatusWorking
	}
	if pending {
		return StatusQueued
	}
	for _, task := range tasks {
		if task.AssigneeID == a.ID && task.Status == store.StatusQueued {
			return StatusQueued
		}
	}
	return StatusIdle
}

func (t *Team) emit(projectID int64, typ string, data any) {
	if t.hub != nil {
		t.hub.Publish(projectID, typ, data)
	}
}

// note adauga un eveniment in thread-ul unui agent. delivered=false il pune in
// coada agentului: il primeste la urmatorul run (asa afla lead-ul de rezultate).
func (t *Team) note(projectID, agentID int64, text string, delivered bool) error {
	m := store.Message{ProjectID: projectID, AgentID: agentID, Kind: store.KindEvent, Text: text, Delivered: delivered}
	if err := t.st.AddMessage(&m); err != nil {
		return err
	}
	t.emit(projectID, "message", m)
	return nil
}

func newToken() string {
	b := make([]byte, 32)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// noteMax limiteaza textele puse in notificari: mesajul turei ajunge ca
// argument de linie de comanda, iar sistemul de operare limiteaza lungimea lor.
const noteMax = 4000

func clip(s string) string {
	r := []rune(s)
	if len(r) <= noteMax {
		return s
	}
	return string(r[:noteMax]) + " [...]"
}

// ProposeSetup apartine wizard-ului (pachetul setup); echipa nu il serveste.
func (t *Team) ProposeSetup(mcp.Caller, json.RawMessage) (string, error) {
	return "", errors.New("propose_setup is not available here")
}

// ProposeAgent: doar draft-ul AI de agent are tool-ul (vezi internal/draft).
func (t *Team) ProposeAgent(mcp.Caller, json.RawMessage) (string, error) {
	return "", errors.New("propose_agent is only for agent drafts")
}

// ErrAgentBusy: agentul are un run activ (New chat cere oprirea lui intai).
var ErrAgentBusy = errors.New("the agent is working; stop it first")

// NewChat porneste un chat nou cu agentul: sesiune Claude noua la urmatorul
// mesaj, iar thread-ul arata doar ce urmeaza (istoricul ramane salvat).
func (t *Team) NewChat(agentID int64) (store.Message, error) {
	t.work.Lock()
	defer t.work.Unlock()
	if t.ActiveRunID(agentID) != "" {
		return store.Message{}, ErrAgentBusy
	}
	m, err := t.st.StartNewChat(agentID, "New chat started.")
	if err != nil {
		return store.Message{}, err
	}
	t.emit(m.ProjectID, "message", m)
	return m, nil
}
