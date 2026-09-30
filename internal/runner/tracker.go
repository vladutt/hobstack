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
}

func (t *tracker) observe(line []byte) {
	var ev struct {
		Type      string  `json:"type"`
		Subtype   string  `json:"subtype"`
		SessionID string  `json:"session_id"`
		Result    string  `json:"result"`
		IsError   bool    `json:"is_error"`
		Cost      float64 `json:"total_cost_usd"`
		Errors    []any   `json:"errors"`
	}
	if json.Unmarshal(line, &ev) != nil {
		return
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
	o := Outcome{RunID: runID, Status: "done", SessionID: t.sessionID, Result: t.result, CostUSD: t.cost}
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
