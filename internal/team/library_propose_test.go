package team

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"foreman/internal/library"
	"foreman/internal/mcp"
	"foreman/internal/store"
)

// TestHelperProposeMCP e un server MCP minimal: testul se re-porneste ca proces.
func TestHelperProposeMCP(t *testing.T) {
	if os.Getenv("FOREMAN_PROPOSE_MCP") != "1" {
		return
	}
	sc := bufio.NewScanner(os.Stdin)
	for sc.Scan() {
		var m struct {
			ID     any    `json:"id"`
			Method string `json:"method"`
		}
		json.Unmarshal(sc.Bytes(), &m)
		if m.ID == nil {
			continue
		}
		var r any = map[string]any{}
		switch m.Method {
		case "initialize":
			r = map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{"tools": map[string]any{}}, "serverInfo": map[string]any{"name": "t", "version": "1"}}
		case "tools/list":
			if os.Getenv("FOREMAN_PROPOSE_SLEEP") != "" {
				time.Sleep(time.Second)
			}
			r = map[string]any{"tools": []any{map[string]any{"name": "list_vehicles"}, map[string]any{"name": "get_vehicle"}}}
		}
		out, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": m.ID, "result": r})
		fmt.Println(string(out))
	}
	os.Exit(0)
}

func proposeSetup(t *testing.T) (*fixture, mcp.Caller, string) {
	f := setup(t)
	f.tm.SetLibrary(library.New(filepath.Join(t.TempDir(), "library"), f.st))
	// Rook lucreaza in folderul proiectului (Workdir)
	os.WriteFile(filepath.Join(f.p.Workdir, "SKILL.md"), []byte("---\nname: x\ndescription: d\n---\nbody\n"), 0o644)
	os.WriteFile(filepath.Join(f.p.Workdir, "run.sh"), []byte("#!/bin/sh\necho hi\n"), 0o755)
	c := f.workerCaller(f.rook, 0)
	c.CanWrite = true
	return f, c, f.p.Workdir
}

func TestProposeLibrarySkillLimitAndErrors(t *testing.T) {
	f, c, _ := proposeSetup(t)
	skill := func(name string) mcp.LibraryInput {
		return mcp.LibraryInput{Kind: "skill", Name: name, Description: "d", Files: []string{"SKILL.md"}}
	}
	for _, n := range []string{"tone-a", "tone-b", "tone-c"} {
		out, err := f.tm.ProposeLibraryItem(c, skill(n))
		if err != nil || !strings.HasPrefix(out, "Proposed "+n+" to the library.") {
			t.Fatalf("%s: %q %v", n, out, err)
		}
	}
	it, err := f.st.FindLibraryItem("skill", "tone-a")
	if err != nil || it.Status != store.ItemProposed || it.MadeBy != "Rook" || it.SourceProjectID != f.p.ID {
		t.Fatalf("%+v %v", it, err)
	}
	if _, err := f.tm.ProposeLibraryItem(c, skill("tone-d")); err == nil || !strings.Contains(err.Error(), "3 proposals") {
		t.Fatalf("a 4-a trebuia refuzata: %v", err)
	}
	if _, err := f.st.FindLibraryItem("skill", "tone-d"); err == nil {
		t.Fatal("a ramas un rand orfan")
	}
	// nota livrata in chat-ul agentului
	msgs, _ := f.st.Undelivered(f.rook.ID)
	if len(msgs) != 0 {
		t.Fatalf("nota trebuia sa fie livrata: %v", msgs)
	}
}

func TestProposeLibraryRejects(t *testing.T) {
	f, c, dir := proposeSetup(t)
	bad := []mcp.LibraryInput{
		{Kind: "skill", Name: "x", Description: "d", Files: []string{"SKILL.md"}},                    // nume prea scurt
		{Kind: "skill", Name: "ok-name", Description: "d", Files: []string{"../x"}},                  // fara SKILL.md
		{Kind: "skill", Name: "ok-name", Description: "d", Files: []string{"SKILL.md", "../x"}},      // iese din folder
		{Kind: "tool", Name: "ok-name", Description: "d", Files: []string{"run.sh"}},                 // fara entry
		{Kind: "tool", Name: "ok-name", Description: "d", Files: []string{"run.sh"}, Entry: "no.sh"}, // entry in afara listei
		{Kind: "skill", Name: "ok-name", Description: "d", Files: []string{"SKILL.md"}, Needs: []string{"root"}},
		{Kind: "skill", Name: "ok-name", Description: "d", Files: []string{"SKILL.md"}, Secrets: []string{"lower"}},
		{Kind: "skill", Name: "ok-name", Description: "", Files: []string{"SKILL.md"}},
		{Kind: "skill", Name: "ok-name", Description: "d", Files: []string{"SKILL.md", "missing.md"}}, // lipseste pe disc
		{Kind: "mcp", Name: "ok-name", Description: "d", Files: []string{"run.sh"}},                   // nici command, nici url
	}
	for i, in := range bad {
		if _, err := f.tm.ProposeLibraryItem(c, in); err == nil {
			t.Fatalf("cazul %d trebuia refuzat: %+v", i, in)
		}
	}
	if items, _ := f.st.ListLibraryItems(""); len(items) != 0 {
		t.Fatalf("randuri orfane: %+v", items)
	}
	if entries, _ := os.ReadDir(filepath.Join(f.tm.lib.Root, "proposals")); len(entries) != 0 {
		t.Fatalf("foldere orfane: %v", entries)
	}
	noStaging(t, f)
	// fara write sau din wizard/draft: refuzat
	c2 := c
	c2.CanWrite = false
	if _, err := f.tm.ProposeLibraryItem(c2, mcp.LibraryInput{Kind: "skill", Name: "ok-name", Description: "d", Files: []string{"SKILL.md"}}); err == nil {
		t.Fatal("fara write trebuia refuzat")
	}
	// nume duplicat
	good := mcp.LibraryInput{Kind: "tool", Name: "ok-name", Description: "d", Files: []string{"run.sh"}, Entry: "run.sh"}
	if _, err := f.tm.ProposeLibraryItem(c, good); err != nil {
		t.Fatal(err)
	}
	if _, err := f.tm.ProposeLibraryItem(c, good); err == nil || !strings.Contains(err.Error(), "already has") {
		t.Fatalf("duplicat: %v", err)
	}
	_ = dir
}

// withShell da agentului si tool-ul shell (serverele MCP locale il cer).
func withShell(t *testing.T, f *fixture, a store.Agent) {
	t.Helper()
	a.Tools = []string{"write", "shell"}
	if err := f.st.UpdateAgent(a); err != nil {
		t.Fatal(err)
	}
}

func noStaging(t *testing.T, f *fixture) {
	t.Helper()
	entries, _ := os.ReadDir(filepath.Join(f.tm.lib.Root, "proposals"))
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".staging-") {
			t.Fatalf("a ramas folder de lucru: %s", e.Name())
		}
	}
}

func TestProposeLocalMCPNeedsShell(t *testing.T) {
	f, c, dir := proposeSetup(t)
	os.WriteFile(filepath.Join(dir, "x.sh"), []byte("#!/bin/sh\ntouch "+filepath.Join(dir, "pwned")+"\n"), 0o755)
	in := mcp.LibraryInput{Kind: "mcp", Name: "sneaky", Description: "d", Files: []string{"x.sh"}, Command: "sh", Args: []string{"${DIR}/x.sh"}}
	_, err := f.tm.ProposeLibraryItem(c, in)
	if err == nil || !strings.Contains(err.Error(), "needs the Shell tool") {
		t.Fatalf("trebuia refuzat: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "pwned")); err == nil {
		t.Fatal("scriptul a rulat")
	}
	if items, _ := f.st.ListLibraryItems(""); len(items) != 0 {
		t.Fatalf("%+v", items)
	}
	noStaging(t, f)
}

// propunerea unui mcp local nu porneste comanda: se testeaza la Publish
func TestProposeLocalMCPDoesNotRun(t *testing.T) {
	f, c, dir := proposeSetup(t)
	withShell(t, f, f.rook)
	marker := filepath.Join(dir, "ran")
	os.WriteFile(filepath.Join(dir, "x.sh"), []byte("#!/bin/sh\ntouch "+marker+"\n"), 0o755)
	in := mcp.LibraryInput{Kind: "mcp", Name: "marker-srv", Description: "d", Files: []string{"x.sh"}, Command: "sh", Args: []string{"${DIR}/x.sh"}}
	out, err := f.tm.ProposeLibraryItem(c, in)
	if err != nil || !strings.Contains(out, "tests and publishes") {
		t.Fatalf("%q %v", out, err)
	}
	time.Sleep(300 * time.Millisecond)
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("comanda a rulat la propunere")
	}
	it, err := f.st.FindLibraryItem("mcp", "marker-srv")
	if err != nil || len(it.ToolNames) != 0 || it.Status != store.ItemProposed {
		t.Fatalf("%+v %v", it, err)
	}
	if _, err := os.Stat(filepath.Join(f.tm.lib.Root, "proposals", strconv.FormatInt(it.ID, 10), "x.sh")); err != nil {
		t.Fatal(err)
	}
	noStaging(t, f)
}

func TestProposeLibraryLocalMCP(t *testing.T) {
	f, c, dir := proposeSetup(t)
	withShell(t, f, f.rook)
	os.WriteFile(filepath.Join(dir, "server.py"), []byte("x"), 0o644)
	in := mcp.LibraryInput{Kind: "mcp", Name: "vehicles", Description: "d", Files: []string{"server.py"},
		Command: "python3", Args: []string{"${DIR}/server.py"}}
	if _, err := f.tm.ProposeLibraryItem(c, in); err != nil {
		t.Fatal(err)
	}
	it, err := f.st.FindLibraryItem("mcp", "vehicles")
	if err != nil || it.Transport != "local" || it.Args[0] != "${DIR}/server.py" {
		t.Fatalf("%+v %v", it, err)
	}

	// remote: fara fisiere, fara probe
	in3 := mcp.LibraryInput{Kind: "mcp", Name: "remote-one", Description: "d", URL: "https://example.com/mcp", Secrets: []string{"API_TOKEN"}}
	if _, err := f.tm.ProposeLibraryItem(c, in3); err != nil {
		t.Fatal(err)
	}
	r, _ := f.st.FindLibraryItem("mcp", "remote-one")
	if r.Transport != "remote" || r.URL != "https://example.com/mcp" {
		t.Fatalf("%+v", r)
	}
}
