// Package mcp implementeaza partea de server MCP de care au nevoie agentii:
// JSON-RPC 2.0 peste HTTP (request JSON, raspuns JSON, fara stream), cu
// metodele initialize, tools/list si tools/call. Logica tool-urilor e in Backend.
package mcp

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
)

// Caller identifica agentul care apeleaza, pe baza token-ului din URL.
type Caller struct {
	ProjectID  int64
	AgentID    int64
	RunID      string
	TaskID     int64  // 0 = run fara task
	IsLead     bool   // are agenti direct sub el
	HasParent  bool   // are sef, deci raporteaza cu report_done
	IsReviewer bool   // verifica munca cuiva
	SetupID    string // != "" = analistul din wizard; vede doar propose_setup
	StageKind  string // etapa task-ului curent: "work", "check" sau "" (fara etape)
	HasStages  bool   // proiectul are etape: la create_task assignee e optional
}

// Backend face munca propriu-zisa. Erorile intoarse ajung la agent ca
// rezultat cu isError, deci trebuie sa fie mesaje clare pentru el.
type Backend interface {
	Resolve(token string) (Caller, bool)
	CreateTask(c Caller, title, description, assignee string) (string, error)
	ListTasks(c Caller, status string) (string, error)
	AcceptTask(c Caller, task, note string) (string, error)
	SendBack(c Caller, task, feedback string) (string, error)
	ReportDone(c Caller, task, summary string) (string, error)
	ReviewApprove(c Caller, task, note string) (string, error)
	ReviewReject(c Caller, task, feedback string) (string, error)
	ProposeSetup(c Caller, args json.RawMessage) (string, error)
	AskUser(c Caller, question string, options []string) (string, error)
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
		text, err = s.backend.CreateTask(c, arg("title"), arg("description"), arg("assignee"))
	case "list_tasks":
		text, err = s.backend.ListTasks(c, arg("status"))
	case "accept_task":
		text, err = s.backend.AcceptTask(c, arg("task"), arg("note"))
	case "send_back":
		text, err = s.backend.SendBack(c, arg("task"), arg("feedback"))
	case "report_done":
		text, err = s.backend.ReportDone(c, arg("task"), arg("summary"))
	case "review_approve":
		text, err = s.backend.ReviewApprove(c, arg("task"), arg("note"))
	case "review_reject":
		text, err = s.backend.ReviewReject(c, arg("task"), arg("feedback"))
	case "ask_user":
		text, err = s.backend.AskUser(c, arg("question"), argList(p.Arguments, "options"))
	case "propose_setup":
		raw, _ := json.Marshal(p.Arguments)
		text, err = s.backend.ProposeSetup(c, raw)
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
