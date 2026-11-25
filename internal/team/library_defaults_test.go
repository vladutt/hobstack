package team

import (
	"strings"
	"testing"

	"hobstack/internal/mcp"
	"hobstack/internal/store"
)

// un MCP default e deja instalat; oprit in proiect, cererea agentului ajunge in Needs you
func TestInstallMCPDefaultTurnedOff(t *testing.T) {
	f := setup(t)
	it := store.LibraryItem{Kind: store.KindMCP, Name: "gh", Description: "GitHub", Status: store.ItemPublished,
		Transport: "local", Command: "npx", Args: []string{"-y", "gh"}}
	if err := f.st.AddLibraryItem(&it); err != nil {
		t.Fatal(err)
	}
	v := store.LibraryVersion{ItemID: it.ID, Version: "1.0", Dir: t.TempDir()}
	f.st.AddLibraryVersion(&v)
	f.st.SetLibraryDefault(it.ID, true)
	c := mcp.Caller{ProjectID: f.p.ID, AgentID: f.rook.ID, RunID: "notification"}
	in := mcp.MCPRequestInput{Name: "gh", Description: "D", Command: "npx", Reason: "need it"}

	text, err := f.tm.RequestMCP(c, in)
	if err != nil || !strings.Contains(text, "already installed") {
		t.Fatalf("default pornit: %q %v", text, err)
	}

	f.st.DisableInstall(f.p.ID, it.ID, v.ID)
	text, err = f.tm.RequestMCP(c, in)
	if err != nil || strings.Contains(text, "already installed") || !strings.Contains(text, "Needs you") {
		t.Fatalf("default oprit: %q %v", text, err)
	}
	reqs, _ := f.st.ListOpenMCPRequests(f.p.ID)
	if len(reqs) != 1 || reqs[0].ItemID == nil || *reqs[0].ItemID != it.ID {
		t.Fatalf("%+v", reqs)
	}
}
