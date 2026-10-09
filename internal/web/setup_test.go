package web

import (
	"encoding/json"
	"foreman/internal/mcp"
	"foreman/internal/runner"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	wizard "foreman/internal/setup" // alias: in pachetul web, "setup" e deja helper-ul de test
	"foreman/internal/store"
)

func withSetup(t *testing.T) *env {
	e := setup(t)
	e.srv.SetSetup(wizard.New(nopStarter{}, "http://127.0.0.1:0", t.TempDir()))
	return e
}

func TestSetupStartValidation(t *testing.T) {
	e := withSetup(t)
	cases := []map[string]any{
		{"workdir": "", "description": "", "model": "sonnet"},
		{"workdir": filepath.Join(t.TempDir(), "nu-exista"), "model": "sonnet"},
		{"description": "x", "model": "gpt-5"},
	}
	for _, c := range cases {
		if code, raw := e.do(t, "POST", "/api/setup", c); code != 400 {
			t.Errorf("%v: %d %s", c, code, raw)
		}
	}
	code, raw := e.do(t, "POST", "/api/setup", map[string]any{"description": "un blog", "model": "haiku"})
	var s struct{ ID, RunID, Status string }
	json.Unmarshal(raw, &s)
	if code != 201 || s.ID == "" || s.Status != "running" {
		t.Fatalf("%d %s", code, raw)
	}
	if code, _ := e.do(t, "GET", "/api/setup/"+s.ID, nil); code != 200 {
		t.Fatalf("get: %d", code)
	}
	if code, _ := e.do(t, "GET", "/api/setup/inexistent", nil); code != 404 {
		t.Fatalf("get inexistent: %d", code)
	}
	if code, _ := e.do(t, "POST", "/api/setup/"+s.ID+"/stop", nil); code != http.StatusAccepted {
		t.Fatalf("stop: %d", code)
	}
}

func TestCreateProjectFromSetup(t *testing.T) {
	e := withSetup(t)
	proposal := map[string]any{
		"name": "Shop", "goal": "g", "context": "c",
		"agents": []map[string]any{
			{"name": "Ana", "role": "Lead", "model": "sonnet", "reportsTo": ""},
			{"name": "Bob", "role": "Backend", "model": "haiku", "reportsTo": "Ana", "reviewedBy": "Cia", "tools": []string{"write", "shell"}},
			{"name": "Cia", "role": "QA", "model": "haiku", "reportsTo": "Ana"},
		},
	}
	code, raw := e.do(t, "POST", "/api/projects/setup", map[string]any{"workdir": t.TempDir(), "proposal": proposal})
	var p struct{ ID int64 }
	json.Unmarshal(raw, &p)
	if code != 201 || p.ID == 0 {
		t.Fatalf("%d %s", code, raw)
	}
	d := e.detail(t, p.ID)
	if len(d.Agents) != 3 {
		t.Fatalf("agenti: %d", len(d.Agents))
	}
	byName := map[string]int64{}
	for _, a := range d.Agents {
		byName[a.Name] = a.ID
	}
	for _, a := range d.Agents {
		if a.Name == "Bob" && (a.ParentID == nil || *a.ParentID != byName["Ana"] || a.ReviewerID == nil || *a.ReviewerID != byName["Cia"]) {
			t.Fatalf("legaturi Bob: %+v", a)
		}
	}

	// ciclu: 400 si nimic creat
	before := len(e.projects(t))
	proposal["agents"].([]map[string]any)[0]["reportsTo"] = "Bob"
	code, raw = e.do(t, "POST", "/api/projects/setup", map[string]any{"workdir": t.TempDir(), "proposal": proposal})
	if code != 400 || !strings.Contains(string(raw), "cycle") {
		t.Fatalf("ciclu: %d %s", code, raw)
	}
	if len(e.projects(t)) != before {
		t.Fatal("nu trebuia creat nimic")
	}
}

func (e *env) projects(t *testing.T) []json.RawMessage {
	_, raw := e.do(t, "GET", "/api/projects", nil)
	var out []json.RawMessage
	json.Unmarshal(raw, &out)
	return out
}

func TestSetupListDismissAndCreate(t *testing.T) {
	e := withSetup(t)
	_, raw := e.do(t, "POST", "/api/setup", map[string]any{"description": "un blog", "model": "haiku"})
	var s struct{ ID string }
	json.Unmarshal(raw, &s)

	list := func() []struct{ ID, Title, Status string } {
		code, raw := e.do(t, "GET", "/api/setup", nil)
		var out []struct{ ID, Title, Status string }
		if code != 200 || json.Unmarshal(raw, &out) != nil {
			t.Fatalf("list: %d %s", code, raw)
		}
		return out
	}
	if l := list(); len(l) != 1 || l[0].ID != s.ID || l[0].Title != "un blog" || l[0].Status != "running" {
		t.Fatalf("%+v", l)
	}

	// crearea proiectului din sesiune o scoate din lista
	proposal := map[string]any{"name": "Blog", "goal": "g", "context": "c",
		"agents": []map[string]any{{"name": "Ana", "role": "Lead", "model": "sonnet", "reportsTo": ""}}}
	if code, raw := e.do(t, "POST", "/api/projects/setup", map[string]any{"setupId": s.ID, "workdir": t.TempDir(), "proposal": proposal}); code != 201 {
		t.Fatalf("%d %s", code, raw)
	}
	if l := list(); len(l) != 0 {
		t.Fatalf("dupa creare: %+v", l)
	}

	_, raw = e.do(t, "POST", "/api/setup", map[string]any{"description": "altul", "model": "haiku"})
	json.Unmarshal(raw, &s)
	if code, _ := e.do(t, "DELETE", "/api/setup/"+s.ID, nil); code != http.StatusNoContent {
		t.Fatalf("dismiss: %d", code)
	}
	if l := list(); len(l) != 0 {
		t.Fatalf("dupa dismiss: %+v", l)
	}
	if code, _ := e.do(t, "DELETE", "/api/setup/inexistent", nil); code != 404 {
		t.Fatalf("dismiss inexistent: %d", code)
	}
}

func TestCreateProjectFromSetupWithFolders(t *testing.T) {
	e := withSetup(t)
	client, landing := t.TempDir(), t.TempDir()
	proposal := map[string]any{
		"name": "Probe", "goal": "g", "context": "c",
		"folders": []map[string]any{{"path": client, "name": "Client", "context": "Go"}, {"path": landing, "name": "Landing"}},
		"agents": []map[string]any{
			{"name": "Ana", "role": "Lead", "model": "sonnet", "reportsTo": ""},
			{"name": "Lia", "role": "Web", "model": "haiku", "reportsTo": "Ana", "folder": "Landing"},
		},
	}
	code, raw := e.do(t, "POST", "/api/projects/setup", map[string]any{"proposal": proposal})
	var p struct{ ID int64 }
	json.Unmarshal(raw, &p)
	if code != 201 {
		t.Fatalf("%d %s", code, raw)
	}
	d := e.folders(t, p.ID)
	if len(d.Folders) != 2 || d.Folders[1].Path != landing || d.Folders[0].Context != "Go" {
		t.Fatalf("foldere: %+v", d.Folders)
	}
	var lia *int64
	for _, a := range d.Agents {
		if a.FolderID != nil {
			lia = a.FolderID
		}
	}
	if lia == nil || *lia != d.Folders[1].ID {
		t.Fatalf("Lia trebuia in Landing: %+v", d.Agents)
	}
}

func TestCreateProjectFromSetupSingleFolderNoPath(t *testing.T) {
	e := withSetup(t)
	dir := t.TempDir()
	proposal := map[string]any{
		"name": "Solo", "goal": "g", "context": "c",
		"folders": []map[string]any{{"name": "Main"}},
		"agents":  []map[string]any{{"name": "Ana", "role": "Lead", "model": "sonnet", "reportsTo": ""}},
	}
	code, raw := e.do(t, "POST", "/api/projects/setup", map[string]any{"proposal": proposal, "workdir": dir})
	var p struct{ ID int64 }
	json.Unmarshal(raw, &p)
	if code != 201 {
		t.Fatalf("%d %s", code, raw)
	}
	if d := e.folders(t, p.ID); len(d.Folders) != 1 || d.Folders[0].Path != dir {
		t.Fatalf("foldere: %+v", d.Folders)
	}
	// fara workdir: folder nou in workspaces (comportamentul vechi)
	code, raw = e.do(t, "POST", "/api/projects/setup", map[string]any{"proposal": proposal})
	json.Unmarshal(raw, &p)
	if code != 201 {
		t.Fatalf("%d %s", code, raw)
	}
	if d := e.folders(t, p.ID); len(d.Folders) != 1 || d.Folders[0].Path == "" || d.Folders[0].Path == dir {
		t.Fatalf("folder nou: %+v", d.Folders)
	}
}

func TestCreateProjectFromSetupNestedFolders(t *testing.T) {
	e := withSetup(t)
	outer := t.TempDir()
	proposal := map[string]any{
		"name": "Probe", "goal": "g", "context": "c",
		"folders": []map[string]any{{"path": outer, "name": "A"}, {"path": filepath.Join(outer, "web"), "name": "B"}},
		"agents":  []map[string]any{{"name": "Ana", "role": "Lead", "model": "sonnet", "reportsTo": ""}},
	}
	code, raw := e.do(t, "POST", "/api/projects/setup", map[string]any{"proposal": proposal})
	if code != 400 || !strings.Contains(string(raw), "is inside") {
		t.Fatalf("%d %s", code, raw)
	}
}

func TestFolderSetupFlow(t *testing.T) {
	e := withSetup(t)
	client, landing := t.TempDir(), t.TempDir()
	_, raw := e.do(t, "POST", "/api/projects", map[string]any{"name": "Probe", "folders": []map[string]any{
		{"name": "Client", "path": client}, {"name": "Landing", "path": landing},
	}})
	var p store.Project
	json.Unmarshal(raw, &p)
	d := e.folders(t, p.ID)
	landingID, leadID := d.Folders[1].ID, d.Agents[0].ID

	// pornire
	code, raw := e.do(t, "POST", "/api/projects/"+itoa(p.ID)+"/folders/"+itoa(landingID)+"/setup", map[string]any{"description": "pricing page", "model": "haiku"})
	var s struct {
		ID        string
		ProjectID int64 `json:"projectId"`
		FolderID  int64 `json:"folderId"`
		Title     string
	}
	json.Unmarshal(raw, &s)
	if code != 201 || s.ProjectID != p.ID || s.FolderID != landingID || s.Title != "Probe · Landing" {
		t.Fatalf("%d %s", code, raw)
	}
	if code, _ := e.do(t, "POST", "/api/projects/"+itoa(p.ID)+"/folders/99999/setup", map[string]any{"model": "haiku"}); code != 404 {
		t.Fatalf("folder necunoscut: %d", code)
	}

	// Add agents: Lia sub Lead-ul existent, Max sub Lia; contextul folderului actualizat
	proposal := map[string]any{"name": "Probe", "folders": []map[string]any{{"path": landing, "name": "Landing", "context": "Hugo"}},
		"agents": []map[string]any{
			{"name": "Lia", "role": "Landing lead", "model": "sonnet", "reportsTo": "Lead"},
			{"name": "Max", "role": "Web dev", "model": "haiku", "reportsTo": "Lia", "tools": []string{"write", "shell"}},
		}}
	path := "/api/projects/" + itoa(p.ID) + "/agents/setup"
	code, raw = e.do(t, "POST", path, map[string]any{"setupId": s.ID, "folderId": landingID, "updateContext": true, "proposal": proposal})
	if code != 201 {
		t.Fatalf("add: %d %s", code, raw)
	}
	d = e.folders(t, p.ID)
	if len(d.Agents) != 3 || d.Folders[1].Context != "Hugo" {
		t.Fatalf("dupa add: %+v", d)
	}
	for _, a := range d.Agents[1:] {
		if a.FolderID == nil || *a.FolderID != landingID {
			t.Fatalf("agent nou in alt folder: %+v", a)
		}
	}
	lead, _ := e.st.GetAgent(leadID)
	lia, _ := e.st.FindAgentByName(p.ID, "Lia")
	if lia.ParentID == nil || *lia.ParentID != lead.ID {
		t.Fatalf("Lia sub Lead: %+v", lia.ParentID)
	}
	// sesiunea dispare din lista
	_, raw = e.do(t, "GET", "/api/setup", nil)
	if strings.Contains(string(raw), s.ID) {
		t.Fatal("sesiunea trebuia ascunsa")
	}

	// a doua oara: numele sunt luate -> 400, nimic nou
	code, _ = e.do(t, "POST", path, map[string]any{"folderId": landingID, "proposal": proposal})
	if code != 400 {
		t.Fatalf("nume luate: %d", code)
	}
	if d := e.folders(t, p.ID); len(d.Agents) != 3 {
		t.Fatalf("s-au creat agenti: %d", len(d.Agents))
	}
	// fara updateContext: contextul ramane
	proposal["agents"] = []map[string]any{{"name": "Zed", "role": "x", "model": "haiku", "reportsTo": "Lead"}}
	proposal["folders"] = []map[string]any{{"path": landing, "context": "altceva"}}
	if code, raw := e.do(t, "POST", path, map[string]any{"folderId": landingID, "proposal": proposal}); code != 201 {
		t.Fatalf("%d %s", code, raw)
	}
	if d := e.folders(t, p.ID); d.Folders[1].Context != "Hugo" {
		t.Fatal("contextul nu trebuia schimbat")
	}
}

// chatStarter retine onDone, ca sa putem termina manual run-urile wizard-ului.
type chatStarter struct {
	mu    sync.Mutex
	specs []runner.Spec
	done  map[string]func(runner.Outcome)
}

func (c *chatStarter) Start(s runner.Spec, d func(runner.Outcome)) (*runner.Run, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.specs = append(c.specs, s)
	c.done[s.ID] = d
	return nil, nil
}
func (c *chatStarter) Stop(string) error { return nil }

type noInner struct{ mcp.Backend }

func (noInner) Resolve(string) (mcp.Caller, bool) { return mcp.Caller{}, false }

func mcpToken(t *testing.T, s runner.Spec) string {
	t.Helper()
	for i, a := range s.Args {
		if a == "--mcp-config" {
			var cfg struct {
				MCPServers map[string]struct {
					URL string `json:"url"`
				} `json:"mcpServers"`
			}
			json.Unmarshal([]byte(s.Args[i+1]), &cfg)
			u := cfg.MCPServers["team"].URL
			return u[strings.LastIndex(u, "/")+1:]
		}
	}
	t.Fatal("fara --mcp-config")
	return ""
}

const chatProposal = `{"name":"Shop","goal":"g","context":"c","agents":[{"name":"Ana","role":"Lead","reportsTo":""},{"name":"Bob","role":"Dev","reportsTo":"Ana"}]}`

func TestSetupChatAndUndo(t *testing.T) {
	e := setup(t)
	cs := &chatStarter{done: map[string]func(runner.Outcome){}}
	m := wizard.New(cs, "http://127.0.0.1:0", t.TempDir())
	e.srv.SetSetup(m)

	dir := t.TempDir()
	code, raw := e.do(t, "POST", "/api/setup", map[string]any{"workdirs": []string{dir}, "description": "a shop", "model": "sonnet"})
	if code != http.StatusAccepted && code != http.StatusCreated && code != http.StatusOK {
		t.Fatalf("start: %d %s", code, raw)
	}
	var s wizard.Session
	json.Unmarshal(raw, &s)
	var prop wizard.Proposal
	json.Unmarshal([]byte(chatProposal), &prop)
	chat := "/api/setup/" + s.ID + "/chat"

	// inca ruleaza analiza: 409
	if code, _ := e.do(t, "POST", chat, map[string]any{"message": "hi", "proposal": prop}); code != 409 {
		t.Fatalf("not ready: %d", code)
	}
	// nimic de anulat: 409
	if code, _ := e.do(t, "POST", "/api/setup/"+s.ID+"/undo", nil); code != 409 {
		t.Fatalf("undo gol: %d", code)
	}

	// aducem sesiunea in ready
	b := m.Wrap(noInner{})
	c, _ := b.Resolve(mcpToken(t, cs.specs[0]))
	if _, err := b.ProposeSetup(c, json.RawMessage(chatProposal)); err != nil {
		t.Fatal(err)
	}
	cs.done[s.RunID](runner.Outcome{RunID: s.RunID, Status: "done", SessionID: "sid-1"})

	if code, _ := e.do(t, "POST", chat, map[string]any{"message": "  ", "proposal": prop}); code != 400 {
		t.Fatalf("mesaj gol: %d", code)
	}
	if code, _ := e.do(t, "POST", "/api/setup/nu-exista/chat", map[string]any{"message": "hi", "proposal": prop}); code != 404 {
		t.Fatalf("404: %d", code)
	}
	code, raw = e.do(t, "POST", chat, map[string]any{"message": "rename", "proposal": prop})
	if code != http.StatusAccepted {
		t.Fatalf("chat: %d %s", code, raw)
	}
	// busy: 409 si la chat, si la undo
	if code, _ := e.do(t, "POST", chat, map[string]any{"message": "again", "proposal": prop}); code != 409 {
		t.Fatalf("busy chat: %d", code)
	}
	if code, _ := e.do(t, "POST", "/api/setup/"+s.ID+"/undo", nil); code != 409 {
		t.Fatalf("busy undo: %d", code)
	}

	// Foreman propune o schimbare, apoi run-ul se termina
	spec := cs.specs[len(cs.specs)-1]
	c2, _ := b.Resolve(mcpToken(t, spec))
	changed := strings.Replace(chatProposal, `"Shop"`, `"Shop 2"`, 1)
	if _, err := b.ProposeSetup(c2, json.RawMessage(changed)); err != nil {
		t.Fatal(err)
	}
	cs.done[spec.ID](runner.Outcome{RunID: spec.ID, Status: "done", SessionID: "sid-2", Result: "Renamed."})

	code, raw = e.do(t, "POST", "/api/setup/"+s.ID+"/undo", nil)
	if code != 200 {
		t.Fatalf("undo: %d %s", code, raw)
	}
	var got wizard.Session
	json.Unmarshal(raw, &got)
	if got.Proposal == nil || got.Proposal.Name != "Shop" {
		t.Fatalf("undo: %+v", got.Proposal)
	}
	if code, _ := e.do(t, "POST", "/api/setup/"+s.ID+"/undo", nil); code != 409 {
		t.Fatalf("undo a doua oara: %d", code)
	}
}

func TestSetupWithAccount(t *testing.T) {
	e := withSetup(t)
	acct := e.addAccount(t, "Work", "sk-ant-oat01-token-1234")
	code, raw := e.do(t, "POST", "/api/setup", map[string]any{"description": "notes app", "model": "sonnet", "accountId": acct})
	var sess struct {
		ID        string
		AccountID int64
	}
	json.Unmarshal(raw, &sess)
	if code != http.StatusCreated || sess.AccountID != acct {
		t.Fatalf("POST /api/setup: %d %s", code, raw)
	}
	if code, _ := e.do(t, "POST", "/api/setup", map[string]any{"description": "x", "model": "sonnet", "accountId": 999}); code != http.StatusBadRequest {
		t.Fatalf("cont necunoscut: %d", code)
	}
	proposal := map[string]any{"name": "Notes", "goal": "g", "context": "c",
		"agents": []map[string]any{{"name": "Ana", "role": "Lead", "model": "sonnet", "reportsTo": ""}}}
	code, raw = e.do(t, "POST", "/api/projects/setup", map[string]any{"setupId": sess.ID, "workdir": t.TempDir(), "proposal": proposal})
	var p struct {
		ID        int64
		AccountID *int64
	}
	json.Unmarshal(raw, &p)
	if code != http.StatusCreated || p.AccountID == nil || *p.AccountID != acct {
		t.Fatalf("proiect din wizard: %d %s", code, raw)
	}
}

// sesiunea wizard-ului a disparut (restart / expirata): contul vine din corp, nu cade pe Default
func TestCreateFromSetupWithoutSessionKeepsAccount(t *testing.T) {
	e := withSetup(t)
	acct := e.addAccount(t, "Work", "sk-ant-oat01-token-1234")
	proposal := map[string]any{"name": "Notes", "goal": "g", "context": "c",
		"agents": []map[string]any{{"name": "Ana", "role": "Lead", "model": "sonnet", "reportsTo": ""}}}
	code, raw := e.do(t, "POST", "/api/projects/setup", map[string]any{"setupId": "gone", "accountId": acct, "workdir": t.TempDir(), "proposal": proposal})
	var p struct {
		AccountID *int64
	}
	json.Unmarshal(raw, &p)
	if code != http.StatusCreated || p.AccountID == nil || *p.AccountID != acct {
		t.Fatalf("fara sesiune: %d %s", code, raw)
	}
}
