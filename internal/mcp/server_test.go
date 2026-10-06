package mcp

import (
	"encoding/json"
	"errors"
	"fmt"
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
func (f *fakeBackend) CreateTask(c Caller, title, desc, assignee, continues string) (string, error) {
	f.calls = append(f.calls, "create_task|"+title+"|"+desc+"|"+assignee+"|"+continues)
	return "Creat T-1 pentru " + assignee, f.err
}
func (f *fakeBackend) ListTasks(c Caller, status string) (string, error) {
	f.calls = append(f.calls, "list_tasks|"+status)
	return "No tasks.", f.err
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
	return "Proposal received.", f.err
}

func (f *fakeBackend) AskUser(c Caller, question string, options []string) (string, error) {
	f.calls = append(f.calls, "ask_user|"+question+"|"+strings.Join(options, ","))
	return "ok", f.err
}

func (f *fakeBackend) AddAgent(c Caller, in AddAgentInput) (string, error) {
	f.calls = append(f.calls, fmt.Sprintf("add_agent|%s|%s|%s|%v|%v|%s", in.Name, in.Role, in.Model, in.Tools, in.ToolsSet, in.Reason))
	return "ok", f.err
}

func (f *fakeBackend) RemoveAgent(c Caller, name string) (string, error) {
	f.calls = append(f.calls, "remove_agent|"+name)
	return "ok", f.err
}

func (f *fakeBackend) ListLinkedProjects(c Caller) (string, error) {
	f.calls = append(f.calls, "list_linked_projects")
	return "Linked projects: Backend", f.err
}

func (f *fakeBackend) AskProject(c Caller, project, question string) (string, error) {
	f.calls = append(f.calls, "ask_project|"+project+"|"+question)
	return "ok", f.err
}

func (f *fakeBackend) ReplyToProject(c Caller, request, answer string) (string, error) {
	f.calls = append(f.calls, "reply_to_project|"+request+"|"+answer)
	return "ok", f.err
}

func (f *fakeBackend) RequestProjectWork(c Caller, project, title, description string) (string, error) {
	f.calls = append(f.calls, "request_project_work|"+project+"|"+title+"|"+description)
	return "ok", f.err
}

func setup(t *testing.T) (*fakeBackend, *httptest.Server) {
	t.Helper()
	fb := &fakeBackend{callers: map[string]Caller{
		"lead":         {ProjectID: 1, AgentID: 1, IsLead: true},
		"worker":       {ProjectID: 1, AgentID: 2, TaskID: 5, HasParent: true},
		"sublead":      {ProjectID: 1, AgentID: 3, IsLead: true, HasParent: true},
		"reviewer":     {ProjectID: 1, AgentID: 4, HasParent: true, IsReviewer: true},
		"solo":         {ProjectID: 1, AgentID: 5},
		"setup":        {SetupID: "s1"},
		"stagework":    {ProjectID: 1, AgentID: 6, TaskID: 9, StageKind: "work"},
		"stagelead":    {ProjectID: 1, AgentID: 8, IsLead: true, HasStages: true},
		"stagecheck":   {ProjectID: 1, AgentID: 7, TaskID: 9, StageKind: "check"},
		"leadadd":      {ProjectID: 1, AgentID: 10, IsLead: true, CanAddAgents: true},
		"rootsolo":     {ProjectID: 1, AgentID: 11, CanAddAgents: true},
		"linkedworker": {ProjectID: 1, AgentID: 20, HasParent: true, HasLinks: true},
		"linkedlead":   {ProjectID: 1, AgentID: 21, IsLead: true, CanAddAgents: true, IsRoot: true, HasLinks: true},
		"rootfromreq":  {ProjectID: 1, AgentID: 22, IsRoot: true, CanAddAgents: true, TaskID: 9, FromRequest: true},
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
	if got := strings.Join(names("lead"), ","); got != "create_task,list_tasks,accept_task,send_back,ask_user" {
		t.Fatalf("lead: %s", got)
	}
	if got := strings.Join(names("worker"), ","); got != "report_done,ask_user" {
		t.Fatalf("worker: %s", got)
	}
	if got := strings.Join(names("sublead"), ","); got != "create_task,list_tasks,accept_task,send_back,report_done,ask_user" {
		t.Fatalf("sublead: %s", got)
	}
	if got := strings.Join(names("reviewer"), ","); got != "report_done,review_approve,review_reject,ask_user" {
		t.Fatalf("reviewer: %s", got)
	}
	if got := strings.Join(names("solo"), ","); got != "ask_user" {
		t.Fatalf("solo: %s", got)
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
	if text, isErr := toolText(t, resp); !isErr || !strings.Contains(text, "is not available") {
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
	if len(fb.calls) != 1 || fb.calls[0] != "create_task|Parser|d|Rook|" {
		t.Fatalf("apeluri: %v", fb.calls)
	}
	// continues ajunge la backend asa cum l-a scris agentul
	rpc(t, ts, "lead", `{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"create_task","arguments":{"title":"Fix","assignee":"Rook","continues":"T-85"}}}`)
	if len(fb.calls) != 2 || fb.calls[1] != "create_task|Fix||Rook|T-85" {
		t.Fatalf("apeluri cu continues: %v", fb.calls)
	}
}

func TestWorkerCannotCallLeadTool(t *testing.T) {
	fb, ts := setup(t)
	_, resp := rpc(t, ts, "worker", `{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"create_task","arguments":{"title":"x","assignee":"Rook"}}}`)
	if text, isErr := toolText(t, resp); !isErr || !strings.Contains(text, "is not available") {
		t.Fatalf("%q %v", text, isErr)
	}
	if len(fb.calls) != 0 {
		t.Fatalf("backend-ul nu trebuia apelat: %v", fb.calls)
	}
}

func TestBackendErrorBecomesToolError(t *testing.T) {
	fb, ts := setup(t)
	fb.err = errors.New("task T-9 does not exist")
	_, resp := rpc(t, ts, "worker", `{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"report_done","arguments":{"summary":"gata"}}}`)
	if text, isErr := toolText(t, resp); !isErr || text != "task T-9 does not exist" {
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

func TestStageTools(t *testing.T) {
	_, ts := setup(t)
	names := func(token string) string {
		_, resp := rpc(t, ts, token, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)
		var out []string
		for _, tool := range result(t, resp)["tools"].([]any) {
			out = append(out, tool.(map[string]any)["name"].(string))
		}
		return strings.Join(out, ",")
	}
	if got := names("stagework"); got != "report_done,ask_user" {
		t.Fatalf("work: %s", got)
	}
	if got := names("stagecheck"); got != "report_done,review_approve,review_reject,ask_user" {
		t.Fatalf("check: %s", got)
	}
}

// intr-un proiect cu etape, assignee e optional la create_task
func TestCreateTaskSchemaWithStages(t *testing.T) {
	_, ts := setup(t)
	required := func(token string) string {
		_, resp := rpc(t, ts, token, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)
		for _, tool := range result(t, resp)["tools"].([]any) {
			m := tool.(map[string]any)
			if m["name"] == "create_task" {
				b, _ := json.Marshal(m["inputSchema"].(map[string]any)["required"])
				return string(b)
			}
		}
		return ""
	}
	if got := required("lead"); got != `["title","assignee"]` {
		t.Fatalf("fara etape: %s", got)
	}
	if got := required("stagelead"); got != `["title"]` {
		t.Fatalf("cu etape: %s", got)
	}
	// ambele variante au argumentul optional continues
	for _, token := range []string{"lead", "stagelead"} {
		_, resp := rpc(t, ts, token, `{"jsonrpc":"2.0","id":3,"method":"tools/list"}`)
		found := false
		for _, tool := range result(t, resp)["tools"].([]any) {
			m := tool.(map[string]any)
			if m["name"] != "create_task" {
				continue
			}
			props := m["inputSchema"].(map[string]any)["properties"].(map[string]any)
			c, ok := props["continues"].(map[string]any)
			found = ok && c["description"] == "Optional: the task this continues (e.g. T-85), so it is grouped with it on the board"
		}
		if !found {
			t.Fatalf("%s: create_task fara continues", token)
		}
	}
}

func TestAskUserTool(t *testing.T) {
	fb, ts := setup(t)
	for _, token := range []string{"worker", "lead", "solo"} {
		_, resp := rpc(t, ts, token, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)
		found := false
		for _, tool := range result(t, resp)["tools"].([]any) {
			if tool.(map[string]any)["name"] == "ask_user" {
				found = true
			}
		}
		if !found {
			t.Errorf("%s: lipseste ask_user", token)
		}
	}
	_, resp := rpc(t, ts, "worker", `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"ask_user","arguments":{"question":"Q?","options":["a","b",3]}}}`)
	if _, isErr := toolText(t, resp); isErr || len(fb.calls) != 1 || fb.calls[0] != "ask_user|Q?|a,b" {
		t.Fatalf("apel: %v %v", resp, fb.calls)
	}
	_, resp = rpc(t, ts, "setup", `{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"ask_user","arguments":{"question":"Q?"}}}`)
	if _, isErr := toolText(t, resp); !isErr {
		t.Fatal("setup nu are voie la ask_user")
	}
}

func TestAddAgentTools(t *testing.T) {
	_, ts := setup(t)
	names := func(token string) string {
		_, resp := rpc(t, ts, token, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)
		var out []string
		for _, tool := range result(t, resp)["tools"].([]any) {
			out = append(out, tool.(map[string]any)["name"].(string))
		}
		return strings.Join(out, ",")
	}
	if got := names("leadadd"); got != "create_task,list_tasks,accept_task,send_back,add_agent,remove_agent,ask_user" {
		t.Fatalf("leadadd: %s", got)
	}
	// radacina fara echipa: create_task + list_tasks ca sa-i dea de lucru agentului nou, fara accept/send_back
	if got := names("rootsolo"); got != "create_task,list_tasks,add_agent,remove_agent,ask_user" {
		t.Fatalf("rootsolo: %s", got)
	}
	for _, token := range []string{"worker", "sublead", "setup"} {
		if got := names(token); strings.Contains(got, "add_agent") {
			t.Fatalf("%s nu are voie la add_agent: %s", token, got)
		}
	}
}

func TestCallAddAgent(t *testing.T) {
	fb, ts := setup(t)
	_, resp := rpc(t, ts, "leadadd", `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"add_agent","arguments":{"name":"Quinn","role":"QA","model":"haiku","tools":["write","git"],"reason":"tests"}}}`)
	if _, isErr := toolText(t, resp); isErr || fb.calls[0] != "add_agent|Quinn|QA|haiku|[write git]|true|tests" {
		t.Fatalf("%v %v", resp, fb.calls)
	}
	// fara tools: ToolsSet false (backend-ul alege implicitul)
	rpc(t, ts, "rootsolo", `{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"add_agent","arguments":{"name":"Ivo","role":"Dev","reason":"x"}}}`)
	if fb.calls[1] != "add_agent|Ivo|Dev||[]|false|x" {
		t.Fatalf("%v", fb.calls)
	}
	rpc(t, ts, "leadadd", `{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"remove_agent","arguments":{"name":"Quinn"}}}`)
	if fb.calls[2] != "remove_agent|Quinn" {
		t.Fatalf("%v", fb.calls)
	}
	_, resp = rpc(t, ts, "worker", `{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"add_agent","arguments":{"name":"X"}}}`)
	if _, isErr := toolText(t, resp); !isErr || len(fb.calls) != 3 {
		t.Fatal("un worker nu poate chema add_agent")
	}
}

func TestLinkedProjectTools(t *testing.T) {
	_, ts := setup(t)
	names := func(token string) string {
		_, resp := rpc(t, ts, token, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)
		var out []string
		for _, tool := range result(t, resp)["tools"].([]any) {
			out = append(out, tool.(map[string]any)["name"].(string))
		}
		return strings.Join(out, ",")
	}
	if got := names("linkedworker"); got != "report_done,list_linked_projects,ask_project,ask_user" {
		t.Fatalf("linkedworker: %s", got)
	}
	if got := names("linkedlead"); got != "create_task,list_tasks,accept_task,send_back,add_agent,remove_agent,list_linked_projects,ask_project,reply_to_project,request_project_work,ask_user" {
		t.Fatalf("linkedlead: %s", got)
	}
	// radacina intr-un run pe un task venit din alt proiect: report_done chiar fara sef
	if got := names("rootfromreq"); !strings.Contains(got, "report_done") || strings.Contains(got, "ask_project") {
		t.Fatalf("rootfromreq: %s", got)
	}
	for _, token := range []string{"lead", "worker", "setup"} {
		if got := names(token); strings.Contains(got, "ask_project") {
			t.Fatalf("%s fara legaturi nu are ask_project: %s", token, got)
		}
	}
}

func TestCallLinkedProjectTools(t *testing.T) {
	fb, ts := setup(t)
	calls := []string{
		`{"name":"list_linked_projects","arguments":{}}`,
		`{"name":"ask_project","arguments":{"project":"Backend","question":"Which format?"}}`,
		`{"name":"reply_to_project","arguments":{"request":"Q-7","answer":"JSON"}}`,
		`{"name":"request_project_work","arguments":{"project":"Backend","title":"Add /export","description":"CSV"}}`,
	}
	for i, c := range calls {
		_, resp := rpc(t, ts, "linkedlead", fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"method":"tools/call","params":%s}`, i+3, c))
		if _, isErr := toolText(t, resp); isErr {
			t.Fatalf("%s: %v", c, resp)
		}
	}
	want := "list_linked_projects,ask_project|Backend|Which format?,reply_to_project|Q-7|JSON,request_project_work|Backend|Add /export|CSV"
	if got := strings.Join(fb.calls, ","); got != want {
		t.Fatalf("%s", got)
	}
	// un worker nu poate cere munca si nu raspunde in numele proiectului
	for _, c := range calls[2:] {
		_, resp := rpc(t, ts, "linkedworker", `{"jsonrpc":"2.0","id":9,"method":"tools/call","params":`+c+`}`)
		if _, isErr := toolText(t, resp); !isErr {
			t.Fatalf("worker: %s", c)
		}
	}
}
