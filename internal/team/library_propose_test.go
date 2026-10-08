package team

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

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

func TestProposeLibraryLocalMCP(t *testing.T) {
	f, c, dir := proposeSetup(t)
	// serverul de proba e binarul de test insusi; ${DIR} nu e folosit aici, dar trebuie inlocuit
	exe, _ := os.Executable()
	os.WriteFile(filepath.Join(dir, "server.py"), []byte("x"), 0o644)
	in := mcp.LibraryInput{Kind: "mcp", Name: "vehicles", Description: "d", Files: []string{"server.py"},
		Command: exe, Args: []string{"-test.run=TestHelperProposeMCP", "${DIR}"}}
	t.Setenv("FOREMAN_PROPOSE_MCP", "1")
	if _, err := f.tm.ProposeLibraryItem(c, in); err != nil {
		t.Fatal(err)
	}
	it, err := f.st.FindLibraryItem("mcp", "vehicles")
	if err != nil || strings.Join(it.ToolNames, ",") != "list_vehicles,get_vehicle" || it.Transport != "local" {
		t.Fatalf("%+v %v", it, err)
	}
	if it.Args[1] != "${DIR}" {
		t.Fatalf("args se salveaza cu ${DIR}: %v", it.Args)
	}

	// un server care nu raspunde: refuzat, fara resturi
	in2 := mcp.LibraryInput{Kind: "mcp", Name: "dead", Description: "d", Files: []string{"server.py"}, Command: "true"}
	if _, err := f.tm.ProposeLibraryItem(c, in2); err == nil {
		t.Fatal("serverul mort trebuia refuzat")
	}
	if _, err := f.st.FindLibraryItem("mcp", "dead"); err == nil {
		t.Fatal("rand orfan")
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
