package runner

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"
)

// tracker aduna din stream-json ce ne trebuie la finalul run-ului:
// sesiunea (pentru --resume), textul final, costul si eventualele erori.
type tracker struct {
	rateLimit *RateLimit // ultimul rate_limit_event
	sessionID string
	result    string
	cost      float64
	tokens    int64
	hasResult bool
	isError   bool
	errText   string
	denials   []Denial

	pending  map[string]pendingCall // tool_use-uri fara rezultat inca, dupa id
	changes  []FileChange
	commands []Command
	toolUses []string // numele tool_use-urilor, in ordine (cu repetari)
}

// usage: tokenii din evenimentul "result" (cache-ul citit intra si el, ca in consum)
type usage struct {
	Input         int64 `json:"input_tokens"`
	CacheCreation int64 `json:"cache_creation_input_tokens"`
	CacheRead     int64 `json:"cache_read_input_tokens"`
	Output        int64 `json:"output_tokens"`
}

func (u usage) total() int64 { return u.Input + u.CacheCreation + u.CacheRead + u.Output }

// LogTokens citeste tokenii din ultimul "result" al unui log de run (logs/runs/<id>.jsonl).
// ok = false daca logul lipseste sau nu are rezultat.
func LogTokens(path string) (int64, bool) {
	f, err := os.Open(path)
	if err != nil {
		return 0, false
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 64*1024*1024) // liniile cu rezultate de tool pot fi mari
	var n int64
	found := false
	for sc.Scan() {
		line := sc.Bytes()
		if !bytes.Contains(line, []byte(`"type":"result"`)) {
			continue
		}
		var ev struct {
			Usage usage `json:"usage"`
		}
		if json.Unmarshal(line, &ev) == nil {
			n, found = ev.Usage.total(), true
		}
	}
	return n, found
}

// Denial e o actiune refuzata de permisiuni in run (din permission_denials).
type Denial struct {
	Tool    string
	Detail  string // scurtat, pentru afisare si salvare
	Command string // comanda Bash intreaga (limitata), pentru clasificare; "" la alte tool-uri
}

// maxDenialCommand limiteaza comanda pastrata intreaga pentru clasificare.
const maxDenialCommand = 4000

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
		Usage             usage   `json:"usage"`
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
	case "rate_limit_event":
		t.observeRateLimit(line)
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
	t.tokens = ev.Usage.total()
	t.isError = ev.IsError
	t.denials = nil
	for _, d := range ev.PermissionDenials {
		den := Denial{Tool: d.ToolName, Detail: denialDetail(d.ToolName, d.ToolInput)}
		if cmd, ok := d.ToolInput["command"].(string); ok && d.ToolName == "Bash" {
			if r := []rune(cmd); len(r) > maxDenialCommand {
				cmd = string(r[:maxDenialCommand])
			}
			den.Command = cmd
		}
		t.denials = append(t.denials, den)
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

// observeRateLimit retine limitele contului (fereastra de 5 ore si cea saptamanala) din rate_limit_event.
func (t *tracker) observeRateLimit(line []byte) {
	type window struct {
		Utilization float64 `json:"utilization"`
		ResetsAt    int64   `json:"resetsAt"`
	}
	var ev struct {
		Info struct {
			Status  string `json:"status"`
			Windows struct {
				FiveHour *window `json:"five_hour"`
				SevenDay *window `json:"seven_day"`
			} `json:"unifiedWindows"`
		} `json:"rate_limit_info"`
	}
	if json.Unmarshal(line, &ev) != nil || ev.Info.Status == "" {
		return
	}
	rl := &RateLimit{Status: ev.Info.Status}
	if w := ev.Info.Windows.FiveHour; w != nil {
		rl.FiveHourUsed, rl.FiveHourResets = w.Utilization, unixTime(w.ResetsAt)
	}
	if w := ev.Info.Windows.SevenDay; w != nil {
		rl.SevenDayUsed, rl.SevenDayResets = w.Utilization, unixTime(w.ResetsAt)
	}
	t.rateLimit = rl
}

// unixTime: secunde Unix -> time.Time; 0 = necunoscut (time.Time zero)
func unixTime(sec int64) time.Time {
	if sec <= 0 {
		return time.Time{}
	}
	return time.Unix(sec, 0)
}

// outcome decide starea finala. Ordinea conteaza: oprirea manuala castiga,
// apoi eroarea raportata de claude (ex. buget depasit), apoi codul de iesire.
func (t *tracker) outcome(runID string, waitErr error, stopped bool) Outcome {
	o := Outcome{RunID: runID, Status: "done", SessionID: t.sessionID, Result: t.result, CostUSD: t.cost, Tokens: t.tokens, Denials: t.denials, Changes: t.changes, Commands: t.commands, ToolUses: t.toolUses, RateLimit: t.rateLimit}
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
