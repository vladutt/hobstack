package team

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"foreman/internal/library"
	"foreman/internal/store"
)

// instalam in proiect un skill publicat si un server MCP, apoi pornim un run.
func TestRunGetsLibrary(t *testing.T) {
	f := setup(t)
	lib := library.New(filepath.Join(t.TempDir(), "library"), f.st)
	f.tm.SetLibrary(lib)

	src := t.TempDir()
	os.WriteFile(filepath.Join(src, "SKILL.md"), []byte("---\nname: tone\ndescription: d\n---\nbody\n"), 0o644)
	it := store.LibraryItem{Kind: store.KindSkill, Name: "tone", Description: "d", Status: store.ItemProposed}
	if err := f.st.AddLibraryItem(&it); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := lib.CopyProposal(it.ID, src, []string{"SKILL.md"}); err != nil {
		t.Fatal(err)
	}
	v, err := lib.Publish(it.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	f.st.Install(f.p.ID, it.ID, v.ID)

	// un script: apare in tura ca linie "Library tools"
	tsrc := t.TempDir()
	os.WriteFile(filepath.Join(tsrc, "hi.sh"), []byte("#!/bin/sh\necho hi\n"), 0o755)
	tool := store.LibraryItem{Kind: store.KindTool, Name: "hi", Description: "says hi", Status: store.ItemProposed, Entry: "hi.sh"}
	if err := f.st.AddLibraryItem(&tool); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := lib.CopyProposal(tool.ID, tsrc, []string{"hi.sh"}); err != nil {
		t.Fatal(err)
	}
	tv, err := lib.Publish(tool.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	f.st.Install(f.p.ID, tool.ID, tv.ID)

	mcpIt := store.LibraryItem{Kind: store.KindMCP, Name: "stock-db", Description: "d", Status: store.ItemPublished,
		Transport: "remote", URL: "http://example.invalid/mcp"}
	if err := f.st.AddLibraryItem(&mcpIt); err != nil {
		t.Fatal(err)
	}
	mv := store.LibraryVersion{ItemID: mcpIt.ID, Version: "1.0", Dir: t.TempDir()}
	if err := f.st.AddLibraryVersion(&mv); err != nil {
		t.Fatal(err)
	}
	f.st.Install(f.p.ID, mcpIt.ID, mv.ID)

	f.task(t, "A", f.rook, store.StatusQueued)
	mustReconcile(t, f)
	spec := f.fs.last(t, f.rook)
	if !hasFlag(spec.Args, "--plugin-dir") {
		t.Fatal("fara --plugin-dir")
	}
	if !strings.Contains(turn(spec), "Library") {
		t.Fatalf("tura nu mentioneaza Library: %s", turn(spec))
	}
	if !strings.Contains(argValue(spec.Args, "--settings"), `"mcp__stock-db"`) {
		t.Fatal("allow fara serverul din Library")
	}
	var cfg struct {
		Servers map[string]any `json:"mcpServers"`
	}
	json.Unmarshal([]byte(argValue(spec.Args, "--mcp-config")), &cfg)
	if cfg.Servers["team"] == nil || cfg.Servers["stock-db"] == nil {
		t.Fatalf("mcp-config: %v", cfg.Servers)
	}
}

// fara Library sau fara item-uri instalate, linia de comanda ramane ca inainte.
func TestRunWithoutLibraryUnchanged(t *testing.T) {
	f := setup(t)
	f.task(t, "A", f.rook, store.StatusQueued)
	mustReconcile(t, f)
	if hasFlag(f.fs.last(t, f.rook).Args, "--plugin-dir") {
		t.Fatal("--plugin-dir fara Library")
	}
	f2 := setup(t)
	f2.tm.SetLibrary(library.New(filepath.Join(t.TempDir(), "library"), f2.st))
	f2.task(t, "A", f2.rook, store.StatusQueued)
	mustReconcile(t, f2)
	spec := f2.fs.last(t, f2.rook)
	if hasFlag(spec.Args, "--plugin-dir") || strings.Contains(turn(spec), "Library") {
		t.Fatal("proiect fara item-uri: fara plugin si fara sectiune")
	}
}
