package team

import (
	"errors"
	"strings"
	"testing"

	"hobstack/internal/mcp"
)

// install_mcp direct: un lider / o radacina, la cererea utilizatorului din chat, fara chei; in rest cerere in Needs you
func TestInstallMCPDirectFromChat(t *testing.T) {
	f := setup(t)
	var got []MCPInstall
	var fail error
	f.tm.SetMCPInstaller(func(in MCPInstall) ([]string, error) {
		if fail != nil {
			return nil, fail
		}
		got = append(got, in)
		return []string{"browser_open"}, nil
	})
	in := func(name string, secrets ...string) mcp.MCPRequestInput {
		return mcp.MCPRequestInput{Name: name, Description: "D", Command: "npx", Args: []string{"-y", name},
			Secrets: secrets, Reason: "the user asked"}
	}
	open := func() int {
		reqs, err := f.st.ListOpenMCPRequests(f.p.ID)
		if err != nil {
			t.Fatal(err)
		}
		return len(reqs)
	}

	// lider din chat: instalat direct
	c := chatCaller(f, f.lead)
	text, err := f.tm.RequestMCP(c, in("playwright"))
	if err != nil || !strings.Contains(text, "Installed the MCP server") || !strings.Contains(text, "browser_open") {
		t.Fatalf("%q %v", text, err)
	}
	if len(got) != 1 || got[0].Name != "playwright" || got[0].Command != "npx" || got[0].Transport != "local" || got[0].AgentName != f.lead.Name || got[0].AgentID != f.lead.ID || open() != 0 {
		t.Fatalf("%+v open=%d", got, open())
	}

	// cu chei: valorile le da doar utilizatorul
	if text, _ := f.tm.RequestMCP(c, in("github", "GITHUB_TOKEN")); !strings.Contains(text, "Needs you") || len(got) != 1 {
		t.Fatalf("secrets: %q", text)
	}
	// fara mesajul utilizatorului (notificare / task): cerere, si cu Full autonomy
	autonomous(t, f)
	notChat := mcp.Caller{ProjectID: f.p.ID, AgentID: f.lead.ID, RunID: "notification"}
	if text, _ := f.tm.RequestMCP(notChat, in("sqlite")); !strings.Contains(text, "Needs you") || len(got) != 1 {
		t.Fatalf("not chat: %q", text)
	}
	// un worker din chat: cerere
	if text, _ := f.tm.RequestMCP(chatCaller(f, f.rook), in("redis")); !strings.Contains(text, "Needs you") || len(got) != 1 {
		t.Fatalf("worker: %q", text)
	}
	// proba picata: cererea ajunge totusi in Needs you, cu motivul
	fail = &MCPTestError{Msg: "The server did not answer tools/list: boom"}
	text, err = f.tm.RequestMCP(c, in("broken"))
	if err != nil || !strings.Contains(text, "test failed") || !strings.Contains(text, "Needs you") {
		t.Fatalf("test failed: %q %v", text, err)
	}
	// alta eroare: ajunge la agent, fara cerere
	before := open()
	fail = errors.New("name taken")
	if _, err := f.tm.RequestMCP(c, in("other")); err == nil || open() != before {
		t.Fatalf("error: %v open=%d", err, open())
	}
	if before != 4 {
		t.Fatalf("open requests: %d", before)
	}
}
