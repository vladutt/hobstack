package team

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"foreman/internal/library"
	"foreman/internal/runner"
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
		Transport: "remote", URL: "http://example.invalid/mcp", Secrets: []string{"AUTH_TOKEN"}}
	if err := f.st.AddLibraryItem(&mcpIt); err != nil {
		t.Fatal(err)
	}
	mv := store.LibraryVersion{ItemID: mcpIt.ID, Version: "1.0", Dir: t.TempDir()}
	if err := f.st.AddLibraryVersion(&mv); err != nil {
		t.Fatal(err)
	}
	f.st.Install(f.p.ID, mcpIt.ID, mv.ID)
	if err := f.st.SetSecret(f.p.ID, mcpIt.ID, "AUTH_TOKEN", "s3cr3t-value"); err != nil {
		t.Fatal(err)
	}

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
	// secretele nu apar in linia de comanda: --mcp-config e o cale spre un fisier 0600
	path := argValue(spec.Args, "--mcp-config")
	if strings.Contains(strings.Join(spec.Args, "\n"), "s3cr3t-value") || strings.HasPrefix(path, "{") {
		t.Fatalf("secret sau JSON inline in Args: %v", spec.Args)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && fi.Mode().Perm() != 0o600 {
		t.Fatalf("mod %v", fi.Mode().Perm())
	}
	data, _ := os.ReadFile(path)
	var cfg struct {
		Servers map[string]any `json:"mcpServers"`
	}
	json.Unmarshal(data, &cfg)
	if cfg.Servers["team"] == nil || cfg.Servers["stock-db"] == nil || !strings.Contains(string(data), "s3cr3t-value") {
		t.Fatalf("mcp-config: %s", data)
	}
	// la sfarsitul run-ului fisierul dispare
	f.fs.finish(spec.ID, runner.Outcome{Status: store.RunDone})
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("fisierul a ramas: %v", err)
	}
}

// un server MCP fara secretul cerut nu intra in config; tura explica de ce.
func TestRunLibraryMissingSecret(t *testing.T) {
	f := setup(t)
	lib := library.New(filepath.Join(t.TempDir(), "library"), f.st)
	f.tm.SetLibrary(lib)
	it := store.LibraryItem{Kind: store.KindMCP, Name: "stock-db", Description: "d", Status: store.ItemPublished,
		Transport: "remote", URL: "http://example.invalid/mcp", Secrets: []string{"AUTH_TOKEN"}}
	if err := f.st.AddLibraryItem(&it); err != nil {
		t.Fatal(err)
	}
	v := store.LibraryVersion{ItemID: it.ID, Version: "1.0", Dir: t.TempDir()}
	if err := f.st.AddLibraryVersion(&v); err != nil {
		t.Fatal(err)
	}
	f.st.Install(f.p.ID, it.ID, v.ID)
	f.task(t, "A", f.rook, store.StatusQueued)
	mustReconcile(t, f)
	spec := f.fs.last(t, f.rook)
	if strings.Contains(argValue(spec.Args, "--mcp-config"), "stock-db") || strings.Contains(argValue(spec.Args, "--settings"), "stock-db") {
		t.Fatalf("serverul fara secret nu trebuie pornit: %v", spec.Args)
	}
	if !strings.HasPrefix(argValue(spec.Args, "--mcp-config"), "{") {
		t.Fatal("fara servere din Library config-ul ramane inline")
	}
	if !strings.Contains(turn(spec), "is off: missing secret AUTH_TOKEN") {
		t.Fatalf("tura: %s", turn(spec))
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
