package mcp

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type fakeBackend struct {
	callers map[string]Caller
	calls   []string
	err     error
}

func (f *fakeBackend) Resolve(tok string) (Caller, bool) { c, ok := f.callers[tok]; return c, ok }
func (f *fakeBackend) CreateTask(c Caller, title, desc, assignee string) (string, error) {
	f.calls = append(f.calls, "create_task|"+title+"|"+desc+"|"+assignee)
	return "Creat T-1 pentru " + assignee, f.err
}
func (f *fakeBackend) ListTasks(c Caller, status string) (string, error) {
	f.calls = append(f.calls, "list_tasks|"+status)
	return "Nu exista task-uri.", f.err
}
func (f *fakeBackend) AcceptTask(c Caller, task, note string) (string, error) {
	f.calls = append(f.calls, "accept_task|"+task+"|"+note)
	return "ok", f.err
}
func (f *fakeBackend) SendBack(c Caller, task, feedback string) (string, error) {
	f.calls = append(f.calls, "send_back|"+task+"|"+feedback)
	return "ok", f.err
}
func (f *fakeBackend) ReportDone(c Caller, task, summary string) (string, error) {
	f.calls = append(f.calls, "report_done|"+task+"|"+summary)
	return "ok", f.err
}
func (f *fakeBackend) ReviewApprove(c Caller, task, note string) (string, error) {
	f.calls = append(f.calls, "review_approve|"+task+"|"+note)
	return "ok", f.err
}
func (f *fakeBackend) ReviewReject(c Caller, task, feedback string) (string, error) {
	f.calls = append(f.calls, "review_reject|"+task+"|"+feedback)
	return "ok", f.err
}

func (f *fakeBackend) ProposeSetup(c Caller, args json.RawMessage) (string, error) {
	f.calls = append(f.calls, "propose_setup|"+c.SetupID+"|"+string(args))
	return "Propunere primita.", f.err
}

func setup(t *testing.T) (*fakeBackend, *httptest.Server) {
	t.Helper()
	fb := &fakeBackend{callers: map[string]Caller{
		"lead":     {ProjectID: 1, AgentID: 1, IsLead: true},
		"worker":   {ProjectID: 1, AgentID: 2, TaskID: 5, HasParent: true},
		"sublead":  {ProjectID: 1, AgentID: 3, IsLead: true, HasParent: true},
		"reviewer": {ProjectID: 1, AgentID: 4, HasParent: true, IsReviewer: true},
		"solo":     {ProjectID: 1, AgentID: 5},
		"setup":    {SetupID: "s1"},
	}}
	mux := http.NewServeMux()
	srv := NewServer(fb)
	mux.Handle("POST /mcp/{token}", srv)
	mux.Handle("GET /mcp/{token}", srv)
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return fb, ts
}

// rpc trimite un request JSON-RPC si intoarce statusul HTTP si raspunsul decodat.
func rpc(t *testing.T, ts *httptest.Server, token, body string) (int, map[string]any) {
	t.Helper()
	res, err := http.Post(ts.URL+"/mcp/"+token, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	var out map[string]any
	if len(raw) > 0 && res.Header.Get("Content-Type") == "application/json" {
		if err := json.Unmarshal(raw, &out); err != nil {
			t.Fatalf("raspuns invalid %s: %v", raw, err)
		}
	}
	return res.StatusCode, out
}

func result(t *testing.T, resp map[string]any) map[string]any {
	t.Helper()
	r, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("fara result: %v", resp)
	}
	return r
}

// toolText intoarce textul si isError dintr-un raspuns tools/call.
func toolText(t *testing.T, resp map[string]any) (string, bool) {
	t.Helper()
	r := result(t, resp)
	content := r["content"].([]any)
	return content[0].(map[string]any)["text"].(string), r["isError"].(bool)
}

func TestInitializeEchoesProtocolVersion(t *testing.T) {
	_, ts := setup(t)
	code, resp := rpc(t, ts, "lead", `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25"}}`)
	r := result(t, resp)
	if code != 200 || r["protocolVersion"] != "2025-11-25" || resp["id"].(float64) != 1 {
		t.Fatalf("%d %v", code, resp)
	}
	if _, ok := r["capabilities"].(map[string]any)["tools"]; !ok {
		t.Fatalf("lipseste capabilities.tools: %v", r)
	}
}

func TestNotificationAccepted(t *testing.T) {
	_, ts := setup(t)
	if code, _ := rpc(t, ts, "lead", `{"jsonrpc":"2.0","method":"notifications/initialized"}`); code != http.StatusAccepted {
		t.Fatalf("notificare: %d", code)
	}
}

func TestUnknownTokenAndGet(t *testing.T) {
	_, ts := setup(t)
	if code, _ := rpc(t, ts, "nimeni", `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`); code != http.StatusNotFound {
		t.Fatalf("token necunoscut: %d", code)
	}
	res, err := http.Get(ts.URL + "/mcp/lead")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("GET: %d", res.StatusCode)
	}
}

func TestUnknownMethod(t *testing.T) {
	_, ts := setup(t)
	_, resp := rpc(t, ts, "lead", `{"jsonrpc":"2.0","id":"server-discover-probe-1","method":"server/discover"}`)
	e, ok := resp["error"].(map[string]any)
	if !ok || e["code"].(float64) != -32601 || resp["id"] != "server-discover-probe-1" {
		t.Fatalf("server/discover: %v", resp)
	}
}

func TestToolsListByRole(t *testing.T) {
	_, ts := setup(t)
	names := func(token string) []string {
		_, resp := rpc(t, ts, token, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)
		var out []string
		for _, tool := range result(t, resp)["tools"].([]any) {
			m := tool.(map[string]any)
			if _, ok := m["inputSchema"].(map[string]any); !ok {
				t.Fatalf("tool fara inputSchema: %v", m)
			}
			out = append(out, m["name"].(string))
		}
		return out
	}
	if got := strings.Join(names("lead"), ","); got != "create_task,list_tasks,accept_task,send_back" {
		t.Fatalf("lead: %s", got)
	}
	if got := strings.Join(names("worker"), ","); got != "report_done" {
		t.Fatalf("worker: %s", got)
	}
	if got := strings.Join(names("sublead"), ","); got != "create_task,list_tasks,accept_task,send_back,report_done" {
		t.Fatalf("sublead: %s", got)
	}
	if got := strings.Join(names("reviewer"), ","); got != "report_done,review_approve,review_reject" {
		t.Fatalf("reviewer: %s", got)
	}
	if got := names("solo"); len(got) != 0 {
		t.Fatalf("solo: %v", got)
	}
	// lista goala trebuie sa fie [], nu null
	_, resp := rpc(t, ts, "solo", `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)
	if tools, ok := result(t, resp)["tools"].([]any); !ok || len(tools) != 0 {
		t.Fatalf("solo tools: %v", result(t, resp)["tools"])
	}
}

func TestCallReportDoneWithTask(t *testing.T) {
	fb, ts := setup(t)
	rpc(t, ts, "sublead", `{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"report_done","arguments":{"task":"T-4","summary":"gata"}}}`)
	if len(fb.calls) != 1 || fb.calls[0] != "report_done|T-4|gata" {
		t.Fatalf("apeluri: %v", fb.calls)
	}
}

func TestCallReviewTools(t *testing.T) {
	fb, ts := setup(t)
	rpc(t, ts, "reviewer", `{"jsonrpc":"2.0","id":8,"method":"tools/call","params":{"name":"review_approve","arguments":{"task":3,"note":"ok"}}}`)
	rpc(t, ts, "reviewer", `{"jsonrpc":"2.0","id":9,"method":"tools/call","params":{"name":"review_reject","arguments":{"task":"T-3","feedback":"lipsesc teste"}}}`)
	if len(fb.calls) != 2 || fb.calls[0] != "review_approve|3|ok" || fb.calls[1] != "review_reject|T-3|lipsesc teste" {
		t.Fatalf("apeluri: %v", fb.calls)
	}
}

func TestWorkerCannotReview(t *testing.T) {
	fb, ts := setup(t)
	_, resp := rpc(t, ts, "worker", `{"jsonrpc":"2.0","id":10,"method":"tools/call","params":{"name":"review_approve","arguments":{"task":"T-3"}}}`)
	if text, isErr := toolText(t, resp); !isErr || !strings.Contains(text, "nu e disponibil") {
		t.Fatalf("%q %v", text, isErr)
	}
	if len(fb.calls) != 0 {
		t.Fatalf("backend-ul nu trebuia apelat: %v", fb.calls)
	}
}

func TestCallCreateTask(t *testing.T) {
	fb, ts := setup(t)
	_, resp := rpc(t, ts, "lead", `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"create_task","arguments":{"title":"Parser","description":"d","assignee":"Rook"}}}`)
	text, isErr := toolText(t, resp)
	if isErr || text != "Creat T-1 pentru Rook" {
		t.Fatalf("%q %v", text, isErr)
	}
	if len(fb.calls) != 1 || fb.calls[0] != "create_task|Parser|d|Rook" {
		t.Fatalf("apeluri: %v", fb.calls)
	}
}

func TestWorkerCannotCallLeadTool(t *testing.T) {
	fb, ts := setup(t)
	_, resp := rpc(t, ts, "worker", `{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"create_task","arguments":{"title":"x","assignee":"Rook"}}}`)
	if text, isErr := toolText(t, resp); !isErr || !strings.Contains(text, "nu e disponibil") {
		t.Fatalf("%q %v", text, isErr)
	}
	if len(fb.calls) != 0 {
		t.Fatalf("backend-ul nu trebuia apelat: %v", fb.calls)
	}
}

func TestBackendErrorBecomesToolError(t *testing.T) {
	fb, ts := setup(t)
	fb.err = errors.New("nu exista task-ul T-9")
	_, resp := rpc(t, ts, "worker", `{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"report_done","arguments":{"summary":"gata"}}}`)
	if text, isErr := toolText(t, resp); !isErr || text != "nu exista task-ul T-9" {
		t.Fatalf("%q %v", text, isErr)
	}
}

func TestNumericTaskArgument(t *testing.T) {
	fb, ts := setup(t)
	rpc(t, ts, "lead", `{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"accept_task","arguments":{"task":3}}}`)
	if len(fb.calls) != 1 || fb.calls[0] != "accept_task|3|" {
		t.Fatalf("apeluri: %v", fb.calls)
	}
}

func TestInvalidJSON(t *testing.T) {
	_, ts := setup(t)
	_, resp := rpc(t, ts, "lead", `{nu e json`)
	if e, ok := resp["error"].(map[string]any); !ok || e["code"].(float64) != -32700 {
		t.Fatalf("JSON invalid: %v", resp)
	}
}

func TestSetupCallerSeesOnlyProposeSetup(t *testing.T) {
	fb, ts := setup(t)
	names := func(token string) []string {
		_, resp := rpc(t, ts, token, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)
		var out []string
		for _, tool := range result(t, resp)["tools"].([]any) {
			out = append(out, tool.(map[string]any)["name"].(string))
		}
		return out
	}
	if got := names("setup"); len(got) != 1 || got[0] != "propose_setup" {
		t.Fatalf("tools pentru setup: %v", got)
	}
	for _, n := range names("lead") {
		if n == "propose_setup" {
			t.Fatal("lead-ul nu trebuie sa vada propose_setup")
		}
	}
	_, resp := rpc(t, ts, "setup", `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"propose_setup","arguments":{"name":"Shop","agents":[]}}}`)
	if _, isErr := toolText(t, resp); isErr || len(fb.calls) != 1 || !strings.HasPrefix(fb.calls[0], `propose_setup|s1|{`) {
		t.Fatalf("apel: %v %v", resp, fb.calls)
	}
	// un lead nu poate apela propose_setup
	_, resp = rpc(t, ts, "lead", `{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"propose_setup","arguments":{}}}`)
	if _, isErr := toolText(t, resp); !isErr {
		t.Fatal("lead-ul nu trebuia sa poata apela propose_setup")
	}
}
