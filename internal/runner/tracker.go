package runner

import (
	"encoding/json"
	"fmt"
	"strings"
)

// tracker aduna din stream-json ce ne trebuie la finalul run-ului:
// sesiunea (pentru --resume), textul final, costul si eventualele erori.
type tracker struct {
	sessionID string
	result    string
	cost      float64
	hasResult bool
	isError   bool
	errText   string
	denials   []Denial

	pending  map[string]pendingCall // tool_use-uri fara rezultat inca, dupa id
	changes  []FileChange
	commands []Command
}

// Denial e o actiune refuzata de permisiuni in run (din permission_denials).
type Denial struct {
	Tool   string
	Detail string
}

// denialDetail alege din tool_input campul care spune ce a vrut agentul sa faca.
func denialDetail(tool string, in map[string]any) string {
	key := ""
	switch tool {
	case "Bash":
		key = "command"
	case "Edit", "Write", "MultiEdit", "Read", "NotebookEdit":
		key = "file_path"
	case "WebFetch":
		key = "url"
	case "WebSearch":
		key = "query"
	}
	d := ""
	if s, ok := in[key].(string); ok && key != "" {
		d = s
	} else if len(in) > 0 {
		b, _ := json.Marshal(in)
		d = string(b)
	}
	if r := []rune(d); len(r) > 120 {
		d = string(r[:120]) + "..."
	}
	return d
}

func (t *tracker) observe(line []byte) {
	var ev struct {
		Type              string  `json:"type"`
		Subtype           string  `json:"subtype"`
		SessionID         string  `json:"session_id"`
		Result            string  `json:"result"`
		IsError           bool    `json:"is_error"`
		Cost              float64 `json:"total_cost_usd"`
		Errors            []any   `json:"errors"`
		PermissionDenials []struct {
			ToolName  string         `json:"tool_name"`
			ToolInput map[string]any `json:"tool_input"`
		} `json:"permission_denials"`
	}
	if json.Unmarshal(line, &ev) != nil {
		return
	}
	switch ev.Type {
	case "assistant":
		t.observeAssistant(line)
	case "user":
		t.observeUser(line)
	}
	if ev.SessionID != "" {
		t.sessionID = ev.SessionID
	}
	if ev.Type != "result" {
		return
	}
	t.hasResult = true
	t.result = ev.Result
	t.cost = ev.Cost
	t.isError = ev.IsError
	t.denials = nil
	for _, d := range ev.PermissionDenials {
		t.denials = append(t.denials, Denial{Tool: d.ToolName, Detail: denialDetail(d.ToolName, d.ToolInput)})
	}
	var errs []string
	for _, e := range ev.Errors {
		errs = append(errs, fmt.Sprint(e))
	}
	t.errText = strings.Join(errs, "; ")
	if t.errText == "" && ev.IsError {
		// ex. "Not logged in": claude pune textul in result, iar subtype ramane "success"
		t.errText = strings.TrimSpace(ev.Result)
		if t.errText == "" {
			t.errText = ev.Subtype
		}
	}
}

// outcome decide starea finala. Ordinea conteaza: oprirea manuala castiga,
// apoi eroarea raportata de claude (ex. buget depasit), apoi codul de iesire.
func (t *tracker) outcome(runID string, waitErr error, stopped bool) Outcome {
	o := Outcome{RunID: runID, Status: "done", SessionID: t.sessionID, Result: t.result, CostUSD: t.cost, Denials: t.denials, Changes: t.changes, Commands: t.commands}
	switch {
	case stopped:
		o.Status, o.Error = "stopped", "stopped"
	case t.isError:
		o.Status, o.Error = "failed", t.errText
	case waitErr != nil:
		o.Status, o.Error = "failed", waitErr.Error()
	case !t.hasResult:
		o.Status, o.Error = "failed", "claude exited without a result"
	}
	return o
}
