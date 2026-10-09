package library

import (
	"strings"
	"testing"

	"hobstack/internal/mcp"
	"hobstack/internal/store"
)

// "team" e serverul MCP al aplicatiei: un server din Library nu poate purta numele lui.
func TestValidateReservedMCPName(t *testing.T) {
	in := mcp.LibraryInput{Kind: store.KindMCP, Name: "team", Description: "d", URL: "http://example.invalid/mcp"}
	if _, err := ValidateInput(in, true); err == nil || !strings.Contains(err.Error(), "reserved") {
		t.Fatalf("err = %v", err)
	}
	in.Name = "team-db"
	if _, err := ValidateInput(in, true); err != nil {
		t.Fatal(err)
	}
}

func TestValidateSecretsRules(t *testing.T) {
	for _, n := range []string{"PATH", "HOME", "SHELL", "LD_PRELOAD", "LD_LIBRARY_PATH", "DYLD_INSERT_LIBRARIES", "BASH_ENV", "ENV", "IFS", "PS4"} {
		if ValidSecretName(n) {
			t.Errorf("%s accepted", n)
		}
		in := mcp.LibraryInput{Kind: store.KindSkill, Name: "sk", Description: "d", Files: []string{"SKILL.md"}, Secrets: []string{n}}
		if _, err := ValidateInput(in, false); err == nil || !strings.Contains(err.Error(), "reserved") {
			t.Errorf("%s: %v", n, err)
		}
	}
	if !ValidSecretName("META_TOKEN") {
		t.Fatal("META_TOKEN refused")
	}
	// mcp remote: doar AUTH_TOKEN sau nimic
	remote := mcp.LibraryInput{Kind: store.KindMCP, Name: "rem", Description: "d", URL: "https://x.example/mcp"}
	for _, secrets := range [][]string{nil, {"AUTH_TOKEN"}} {
		remote.Secrets = secrets
		if _, err := ValidateInput(remote, false); err != nil {
			t.Fatalf("%v: %v", secrets, err)
		}
	}
	for _, secrets := range [][]string{{"META_TOKEN"}, {"AUTH_TOKEN", "OTHER_ONE"}} {
		remote.Secrets = secrets
		if _, err := ValidateInput(remote, false); err == nil || !strings.Contains(err.Error(), "AUTH_TOKEN") {
			t.Fatalf("%v: %v", secrets, err)
		}
	}
	// tool cu secrete nu poate avea entry "run"
	tool := mcp.LibraryInput{Kind: store.KindTool, Name: "tl", Description: "d", Files: []string{"run"}, Entry: "run", Secrets: []string{"META_TOKEN"}}
	if _, err := ValidateInput(tool, false); err == nil || !strings.Contains(err.Error(), `"run"`) {
		t.Fatalf("%v", err)
	}
	tool.Secrets = nil
	if _, err := ValidateInput(tool, false); err != nil {
		t.Fatal(err)
	}
}
