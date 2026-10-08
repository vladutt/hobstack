package library

import (
	"strings"
	"testing"

	"foreman/internal/mcp"
	"foreman/internal/store"
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
