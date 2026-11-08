// Package mcp implementeaza partea de server MCP de care au nevoie agentii:
// JSON-RPC 2.0 peste HTTP (request JSON, raspuns JSON, fara stream), cu
// metodele initialize, tools/list si tools/call. Logica tool-urilor e in Backend.
package mcp

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
)

// Caller identifica agentul care apeleaza, pe baza token-ului din URL.
type Caller struct {
	ProjectID    int64
	AgentID      int64
	RunID        string
	TaskID       int64  // 0 = run fara task
	IsLead       bool   // are agenti direct sub el
	HasParent    bool   // are sef, deci raporteaza cu report_done
	IsReviewer   bool   // verifica munca cuiva
	SetupID      string // != "" = analistul din wizard; vede doar propose_setup
	DraftID      string // != "" = draft-ul AI al unui agent nou; vede doar propose_agent
	StageKind    string // etapa task-ului curent: "work", "check" sau "" (fara etape)
	HasStages    bool   // proiectul are etape: la create_task assignee e optional
	CanAddAgents bool   // lider sau radacina: add_agent / remove_agent
	HasLinks     bool   // proiectul are legaturi cu alte proiecte
	CanWrite     bool   // agentul are tool-ul write: poate propune elemente pentru Library
	IsRoot       bool   // raporteaza utilizatorului: primeste intrebarile proiectelor legate
	FromRequest  bool   // run pe un task cerut de un proiect legat: are report_done chiar fara sef
}

// LibraryInput sunt argumentele lui propose_library_item.
type LibraryInput struct {
	Kind, Name, Description, Entry, Command, URL string
	Files, Args, Needs, Secrets                  []string
}

// MCPRequestInput sunt argumentele lui install_mcp: un server local (command + args) sau remote (url).
type MCPRequestInput struct {
	Name, Description, Command, URL, Reason string
	Args, Secrets                           []string
}

// AddAgentInput sunt argumentele lui add_agent. ToolsSet spune daca agentul a
// trimis "tools" (chiar gol); fara el, backend-ul alege tool-urile implicite.
type AddAgentInput struct {
	Name, Role, Model, Background, Instructions, DoneDef, Reason string
	Tools                                                        []string
	ToolsSet                                                     bool
}

// RecurringSchedule e programul din argumentele tool-urilor *_recurring (zilele ca nume, ex. "mon").
type RecurringSchedule struct {
	Kind       string
	EveryHours int
	At         string
	Weekdays   []string
}

// RecurringArgs sunt argumentele lui create_recurring.
type RecurringArgs struct {
	Title, Description, Assignee string
	Schedule                     RecurringSchedule
	AutoAccept                   bool
}

// RecurringChange sunt argumentele lui update_recurring: nil = campul nu se schimba.
type RecurringChange struct {
	ID                           string
	Title, Description, Assignee *string
	Schedule                     *RecurringSchedule
	AutoAccept, Enabled          *bool
}

// Backend face munca propriu-zisa. Erorile intoarse ajung la agent ca
// rezultat cu isError, deci trebuie sa fie mesaje clare pentru el.
type Backend interface {
	Resolve(token string) (Caller, bool)
	CreateTask(c Caller, title, description, assignee, continues, stage string) (string, error)
	StartTask(c Caller, title, description, continues string) (string, error)
	ListTasks(c Caller, status string) (string, error)
	AcceptTask(c Caller, task, note string) (string, error)
	SendBack(c Caller, task, feedback string) (string, error)
	ReportDone(c Caller, task, summary string) (string, error)
	LeaveForUser(c Caller, task string, items []string) (string, error)
	LeaveSteps(c Caller, task string, steps []string) (string, error)
	AskDecisions(c Caller, task string, qs []DecisionInput) (string, error)
	AttachFiles(c Caller, task string, paths []string) (string, error)
	TickLeftForUser(c Caller, task string, items []int) (string, error)
	DeleteFiles(c Caller, paths []string) (string, error)
	AttachToChat(c Caller, paths []string, note string) (string, error)
	RequestMCP(c Caller, in MCPRequestInput) (string, error)
	CommentTask(c Caller, task, text string) (string, error)
	ReviewApprove(c Caller, task, note string) (string, error)
	ReviewReject(c Caller, task, feedback string) (string, error)
	ProposeSetup(c Caller, args json.RawMessage) (string, error)
	ProposeAgent(c Caller, args json.RawMessage) (string, error)
	ProposeLibraryItem(c Caller, in LibraryInput) (string, error)
	AskUser(c Caller, question string, options []string) (string, error)
	AddAgent(c Caller, in AddAgentInput) (string, error)
	RemoveAgent(c Caller, name string) (string, error)
	ListRecurringFor(c Caller) (string, error)
	CreateRecurringFor(c Caller, in RecurringArgs) (string, error)
	UpdateRecurringFor(c Caller, ch RecurringChange) (string, error)
	DeleteRecurringFor(c Caller, ref string) (string, error)
	ListLinkedProjects(c Caller) (string, error)
	AskProject(c Caller, project, question string) (string, error)
	ReplyToProject(c Caller, request, answer string) (string, error)
	RequestProjectWork(c Caller, project, title, description string) (string, error)
}

type Server struct {
	backend Backend
}

func NewServer(b Backend) *Server { return &Server{backend: b} }

// fallbackProtocol se foloseste doar daca clientul nu cere o versiune.
const fallbackProtocol = "2025-06-18"

type request struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	caller, ok := s.backend.Resolve(r.PathValue("token"))
	if !ok {
		http.Error(w, "unknown token", http.StatusNotFound)
		return
	}
	// Claude deschide si un GET pentru mesaje server -> client; nu avem asa ceva.
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}

	var req request
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		writeJSON(w, response{JSONRPC: "2.0", ID: json.RawMessage("null"), Error: &rpcError{-32700, "invalid JSON"}})
		return
	}
	// notificare (fara id): nu se asteapta raspuns
	if len(req.ID) == 0 || string(req.ID) == "null" {
		w.WriteHeader(http.StatusAccepted)
		return
	}

	resp := response{JSONRPC: "2.0", ID: req.ID}
	switch req.Method {
	case "initialize":
		resp.Result = initialize(req.Params)
	case "ping":
		resp.Result = map[string]any{}
	case "tools/list":
		resp.Result = map[string]any{"tools": toolsFor(caller)}
	case "tools/call":
		resp.Result = s.call(caller, req.Params)
	default:
		resp.Error = &rpcError{-32601, "unknown method: " + req.Method}
	}
	writeJSON(w, resp)
}

func initialize(params json.RawMessage) any {
	var p struct {
		ProtocolVersion string `json:"protocolVersion"`
	}
	json.Unmarshal(params, &p)
	version := p.ProtocolVersion
	if version == "" {
		version = fallbackProtocol
	}
	return map[string]any{
		"protocolVersion": version,
		"capabilities":    map[string]any{"tools": map[string]any{}},
		"serverInfo":      map[string]any{"name": "team", "version": "1"},
	}
}

func (s *Server) call(c Caller, params json.RawMessage) any {
	var p struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return toolResult("invalid parameters", true)
	}
	if !allowed(c, p.Name) {
		return toolResult("the tool "+p.Name+" is not available for your role", true)
	}
	arg := func(key string) string { return argString(p.Arguments, key) }

	var text string
	var err error
	switch p.Name {
	case "create_task":
		text, err = s.backend.CreateTask(c, arg("title"), arg("description"), arg("assignee"), arg("continues"), arg("stage"))
	case "start_task":
		text, err = s.backend.StartTask(c, arg("title"), arg("description"), arg("continues"))
	case "list_tasks":
		text, err = s.backend.ListTasks(c, arg("status"))
	case "accept_task":
		text, err = s.backend.AcceptTask(c, arg("task"), arg("note"))
	case "send_back":
		text, err = s.backend.SendBack(c, arg("task"), arg("feedback"))
	case "report_done":
		left := argList(p.Arguments, "left_for_user")
		if err := validLeftForUser(left); err != nil {
			return toolResult(err.Error(), true)
		}
		steps := argList(p.Arguments, "user_steps")
		if err := validSteps(steps); err != nil {
			return toolResult(err.Error(), true)
		}
		decisions := argDecisions(p.Arguments, "decisions")
		if err := validDecisions(decisions); err != nil {
			return toolResult(err.Error(), true)
		}
		files := argList(p.Arguments, "files")
		if err := validFiles(files); err != nil {
			return toolResult(err.Error(), true)
		}
		text, err = s.backend.ReportDone(c, arg("task"), arg("summary"))
		if err == nil && len(left) > 0 {
			// raportul a trecut: punctele se lasa pe acelasi task (task gol = task-ul curent)
			extra, lerr := s.backend.LeaveForUser(c, arg("task"), left)
			if lerr != nil {
				text += " Note: the items for the user were NOT saved (" + lerr.Error() + "). Write them in your next comment_task so the user sees them: " + strings.Join(left, "; ") + "."
			} else {
				text += " " + extra
			}
		}
		if err == nil && len(steps) > 0 {
			extra, serr := s.backend.LeaveSteps(c, arg("task"), steps)
			if serr != nil {
				text += " Note: the steps for the user were NOT saved (" + serr.Error() + "). Write them in your next comment_task: " + strings.Join(steps, "; ") + "."
			} else {
				text += " " + extra
			}
		}
		if err == nil && len(decisions) > 0 {
			extra, derr := s.backend.AskDecisions(c, arg("task"), decisions)
			if derr != nil {
				text += " Note: the questions for the user were NOT saved (" + derr.Error() + ")."
			} else {
				text += " " + extra
			}
		}
		if err == nil && len(files) > 0 {
			// fisierele se copiaza dupa raport; un esec nu anuleaza raportul
			extra, ferr := s.backend.AttachFiles(c, arg("task"), files)
			if ferr != nil {
				text += " Note: the files were NOT attached (" + ferr.Error() + ")."
			} else {
				text += " " + extra
			}
		}
	case "install_mcp":
		text, err = s.backend.RequestMCP(c, MCPRequestInput{Name: arg("name"), Description: arg("description"),
			Command: arg("command"), URL: arg("url"), Reason: arg("reason"),
			Args: argList(p.Arguments, "args"), Secrets: argList(p.Arguments, "secrets")})
	case "attach_files":
		paths := argList(p.Arguments, "paths")
		switch {
		case len(paths) == 0:
			err = errors.New("paths: the files to attach, e.g. [\"screenshots/home.png\"]")
		case len(paths) > 20:
			err = errors.New("at most 20 files at a time")
		case c.TaskID != 0:
			text, err = s.backend.AttachFiles(c, "", paths)
		default:
			text, err = s.backend.AttachToChat(c, paths, arg("note"))
		}
	case "delete_files":
		text, err = s.backend.DeleteFiles(c, argList(p.Arguments, "paths"))
	case "tick_left_for_user":
		text, err = s.backend.TickLeftForUser(c, arg("task"), argInts(p.Arguments, "items"))
	case "comment_task":
		text, err = s.backend.CommentTask(c, arg("task"), arg("text"))
	case "review_approve":
		text, err = s.backend.ReviewApprove(c, arg("task"), arg("note"))
	case "review_reject":
		text, err = s.backend.ReviewReject(c, arg("task"), arg("feedback"))
	case "ask_user":
		text, err = s.backend.AskUser(c, arg("question"), argList(p.Arguments, "options"))
	case "add_agent":
		_, toolsSet := p.Arguments["tools"]
		text, err = s.backend.AddAgent(c, AddAgentInput{
			Name: arg("name"), Role: arg("role"), Model: arg("model"), Background: arg("background"),
			Instructions: arg("instructions"), DoneDef: arg("done_definition"), Reason: arg("reason"),
			Tools: argList(p.Arguments, "tools"), ToolsSet: toolsSet,
		})
	case "remove_agent":
		text, err = s.backend.RemoveAgent(c, arg("name"))
	case "list_recurring":
		text, err = s.backend.ListRecurringFor(c)
	case "create_recurring":
		in := RecurringArgs{Title: arg("title"), Description: arg("description"), Assignee: arg("assignee")}
		if sc := argSchedule(p.Arguments, "schedule"); sc != nil {
			in.Schedule = *sc
		}
		if b := argBoolPtr(p.Arguments, "auto_accept"); b != nil {
			in.AutoAccept = *b
		}
		text, err = s.backend.CreateRecurringFor(c, in)
	case "update_recurring":
		text, err = s.backend.UpdateRecurringFor(c, RecurringChange{
			ID: arg("id"), Title: argStrPtr(p.Arguments, "title"), Description: argStrPtr(p.Arguments, "description"),
			Assignee: argStrPtr(p.Arguments, "assignee"), Schedule: argSchedule(p.Arguments, "schedule"),
			AutoAccept: argBoolPtr(p.Arguments, "auto_accept"), Enabled: argBoolPtr(p.Arguments, "enabled"),
		})
	case "delete_recurring":
		text, err = s.backend.DeleteRecurringFor(c, arg("id"))
	case "list_linked_projects":
		text, err = s.backend.ListLinkedProjects(c)
	case "ask_project":
		text, err = s.backend.AskProject(c, arg("project"), arg("question"))
	case "reply_to_project":
		text, err = s.backend.ReplyToProject(c, arg("request"), arg("answer"))
	case "request_project_work":
		text, err = s.backend.RequestProjectWork(c, arg("project"), arg("title"), arg("description"))
	case "propose_library_item":
		text, err = s.backend.ProposeLibraryItem(c, LibraryInput{
			Kind: arg("kind"), Name: arg("name"), Description: arg("description"), Entry: arg("entry"),
			Command: arg("command"), URL: arg("url"), Files: argList(p.Arguments, "files"), Args: argList(p.Arguments, "args"),
			Needs: argList(p.Arguments, "needs"), Secrets: argList(p.Arguments, "secrets"),
		})
	case "propose_setup":
		raw, _ := json.Marshal(p.Arguments)
		text, err = s.backend.ProposeSetup(c, raw)
	case "propose_agent":
		raw, _ := json.Marshal(p.Arguments)
		text, err = s.backend.ProposeAgent(c, raw)
	}
	if err != nil {
		return toolResult(err.Error(), true)
	}
	return toolResult(text, false)
}

func toolResult(text string, isError bool) map[string]any {
	return map[string]any{
		"content": []map[string]any{{"type": "text", "text": text}},
		"isError": isError,
	}
}

// argString citeste un argument ca text. Agentii trimit uneori numere
// (ex. "task": 3 in loc de "T-3").
func argString(args map[string]any, key string) string {
	switch v := args[key].(type) {
	case nil:
		return ""
	case string:
		return v
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	default:
		return fmt.Sprint(v)
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

// argSchedule citeste obiectul schedule al tool-urilor *_recurring (nil daca lipseste)
func argSchedule(args map[string]any, key string) *RecurringSchedule {
	m, ok := args[key].(map[string]any)
	if !ok {
		return nil
	}
	s := &RecurringSchedule{Kind: argString(m, "kind"), At: argString(m, "at"), Weekdays: argList(m, "weekdays")}
	if n, err := strconv.Atoi(strings.TrimSpace(argString(m, "every_hours"))); err == nil {
		s.EveryHours = n // numar sau text ("3")
	}
	return s
}

// argStrPtr: un text optional (nil = lipseste sau null, deci nu se schimba)
func argStrPtr(args map[string]any, key string) *string {
	if args[key] == nil {
		return nil
	}
	v := argString(args, key)
	return &v
}

// argBoolPtr: un bool optional (nil = lipseste sau nu e bool)
func argBoolPtr(args map[string]any, key string) *bool {
	v, ok := args[key].(bool)
	if !ok {
		return nil
	}
	return &v
}

// argList citeste un argument lista de texte (ignora elementele care nu sunt text).
func argList(args map[string]any, key string) []string {
	raw, _ := args[key].([]any)
	out := []string{}
	for _, v := range raw {
		if s, ok := v.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// MaxLeftForUser: cate puncte poate lasa un agent utilizatorului la un raport
const MaxLeftForUser = 10

func validLeftForUser(items []string) error {
	if len(items) > MaxLeftForUser {
		return fmt.Errorf("left_for_user: at most %d items", MaxLeftForUser)
	}
	for _, it := range items {
		n := len([]rune(strings.TrimSpace(it)))
		if n == 0 || n > 200 {
			return errors.New("left_for_user: each item must be 1-200 characters")
		}
	}
	return nil
}

// DecisionInput: o intrebare cu optiuni data la report_done
type DecisionInput struct {
	Question string
	Options  []string
	Multi    bool
}

// Limitele pentru decisions / user_steps (store-ul nu valideaza, doar MCP).
const (
	MaxDecisions = 5
	MaxSteps     = 10
)

// argDecisions citeste lista de obiecte {question, options, multi}; multi lipsa = false.
func argDecisions(args map[string]any, key string) []DecisionInput {
	raw, _ := args[key].([]any)
	out := []DecisionInput{}
	for _, v := range raw {
		m, _ := v.(map[string]any)
		q, _ := m["question"].(string)
		multi, _ := m["multi"].(bool)
		d := DecisionInput{Question: q, Multi: multi, Options: []string{}}
		opts, _ := m["options"].([]any)
		for _, o := range opts {
			if s, ok := o.(string); ok {
				d.Options = append(d.Options, s)
			} else {
				d.Options = append(d.Options, "") // invalid: prins la validare
			}
		}
		out = append(out, d)
	}
	return out
}

func validDecisions(qs []DecisionInput) error {
	if len(qs) > MaxDecisions {
		return fmt.Errorf("decisions: at most %d questions", MaxDecisions)
	}
	for _, q := range qs {
		if n := len([]rune(strings.TrimSpace(q.Question))); n == 0 || n > 300 {
			return errors.New("decisions: each question must be 1-300 characters")
		}
		if len(q.Options) < 2 || len(q.Options) > 5 {
			return errors.New("decisions: each question needs 2-5 options")
		}
		for _, o := range q.Options {
			if n := len([]rune(strings.TrimSpace(o))); n == 0 || n > 120 {
				return errors.New("decisions: each option must be 1-120 characters")
			}
		}
	}
	return nil
}

func validSteps(items []string) error {
	if len(items) > MaxSteps {
		return fmt.Errorf("user_steps: at most %d steps", MaxSteps)
	}
	for _, it := range items {
		if n := len([]rune(strings.TrimSpace(it))); n == 0 || n > 300 {
			return errors.New("user_steps: each step must be 1-300 characters")
		}
	}
	return nil
}

// MaxFiles: cate fisiere poate preda un agent la un raport
const MaxFiles = 20

func validFiles(paths []string) error {
	if len(paths) > MaxFiles {
		return fmt.Errorf("files: at most %d files", MaxFiles)
	}
	for _, p := range paths {
		n := len([]rune(strings.TrimSpace(p)))
		if n == 0 || n > 500 {
			return errors.New("files: each path must be 1-500 characters")
		}
	}
	return nil
}

// argInts citeste o lista de numere (JSON le aduce ca float64; "3" ca text e acceptat si el)
func argInts(args map[string]any, key string) []int {
	raw, _ := args[key].([]any)
	out := []int{}
	for _, v := range raw {
		switch x := v.(type) {
		case float64:
			out = append(out, int(x))
		case string:
			if n, err := strconv.Atoi(strings.TrimSpace(x)); err == nil {
				out = append(out, n)
			}
		}
	}
	return out
}
