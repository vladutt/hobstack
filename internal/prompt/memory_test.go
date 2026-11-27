package prompt

import (
	"fmt"
	"strings"
	"testing"

	"hobstack/internal/store"
)

func TestMemoryIndexEmpty(t *testing.T) {
	if got := MemoryIndex(nil, true); got != "" {
		t.Fatalf("got %q", got)
	}
}

func TestMemoryIndexLines(t *testing.T) {
	list := []store.Memory{
		{Key: "api.auth", Summary: "tokens live in Redis", Locked: true, AuthorName: "You"},
		{Key: "ui.dates", Summary: "show dates as DD.MM.YYYY", AuthorName: "Backend Bob", TaskRef: "T-212"},
	}
	got := MemoryIndex(list, true)
	want := "Project memory (decisions and facts saved by the team; read the full text with mcp__team__recall):\n" +
		"- api.auth: tokens live in Redis (locked, You)\n" +
		"- ui.dates: show dates as DD.MM.YYYY (Backend Bob, T-212)"
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
	if got := MemoryIndex(list, false); !strings.HasPrefix(got, "Project memory (decisions and facts saved by the team):\n") ||
		strings.Contains(got, "recall") {
		t.Fatalf("no tools: %s", got)
	}
}

func TestMemoryIndexLimits(t *testing.T) {
	var list []store.Memory
	for i := 0; i < 50; i++ {
		list = append(list, store.Memory{Key: fmt.Sprintf("k%d", i), Summary: "short", AuthorName: "A"})
	}
	got := MemoryIndex(list, true)
	lines := strings.Split(got, "\n")
	if len(lines) != 1+40+1 || lines[41] != "- ... 10 more: search with mcp__team__search_memory" {
		t.Fatalf("%d lines, last %q", len(lines), lines[len(lines)-1])
	}
	// rezumate lungi, multi-byte: limita de 3000 de caractere
	list = nil
	for i := 0; i < 40; i++ {
		list = append(list, store.Memory{Key: fmt.Sprintf("k%d", i), Summary: strings.Repeat("ș", 140), AuthorName: "A"})
	}
	got = MemoryIndex(list, false)
	if n := len([]rune(got)); n > 3000 {
		t.Fatalf("%d characters", n)
	}
	lines = strings.Split(got, "\n")
	shown := len(lines) - 2
	if want := fmt.Sprintf("- ... %d more", 40-shown); lines[len(lines)-1] != want {
		t.Fatalf("last %q, want %q", lines[len(lines)-1], want)
	}
}

func TestSystemHasMemoryRule(t *testing.T) {
	for _, l := range []Links{{BossName: "Atlas"}, {BossName: "Atlas", IsLead: true}, {IsLead: true}, {}} {
		if got := System(proj, store.Agent{Name: "A"}, l); !strings.Contains(got, "Project memory holds decisions") {
			t.Fatalf("%+v: no memory rule", l)
		}
	}
}
