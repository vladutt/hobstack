package library

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"hobstack/internal/store"
)

// publishSkill publica un skill simplu si il intoarce cu versiunea lui.
func publishDefSkill(t *testing.T, st *store.Store, lib *Lib, src, name string) (store.LibraryItem, store.LibraryVersion) {
	t.Helper()
	os.MkdirAll(filepath.Join(src, name), 0o755)
	os.WriteFile(filepath.Join(src, name, "SKILL.md"), []byte("---\nname: "+name+"\ndescription: d\n---\nx\n"), 0o644)
	it := addItem(t, st, store.KindSkill, name)
	if _, _, _, err := lib.CopyProposal(it.ID, filepath.Join(src, name), []string{"SKILL.md"}); err != nil {
		t.Fatal(err)
	}
	v, err := lib.Publish(it.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	return it, v
}

func TestBuildIncludesDefault(t *testing.T) {
	st, lib, src := newEnv(t)
	p := project(t, st)
	it, v := publishDefSkill(t, st, lib, src, "ro-copy")
	if err := st.SetLibraryDefault(it.ID, true); err != nil {
		t.Fatal(err)
	}
	b, err := lib.Build(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(b.PluginDir, "skills", "ro-copy", "SKILL.md")); err != nil {
		t.Fatalf("skill-ul default lipseste din plugin (%q)", b.PluginDir)
	}
	st.DisableInstall(p.ID, it.ID, v.ID)
	b, _ = lib.Build(p.ID)
	if b.PluginDir != "" {
		t.Fatalf("skill-ul oprit in proiect a intrat in build: %q", b.PluginDir)
	}
}

func TestBuildGlobalSecret(t *testing.T) {
	st, lib, src := newEnv(t)
	p := project(t, st)
	skill, _ := publishDefSkill(t, st, lib, src, "ro-copy") // ca build-ul sa aiba un folder (hash-ul in nume)
	st.SetLibraryDefault(skill.ID, true)
	m := store.LibraryItem{Kind: store.KindMCP, Name: "stock-db", Description: "x", Status: store.ItemPublished,
		Transport: "local", Command: "python3", Args: []string{"server.py"}, Secrets: []string{"DATABASE_URL"}}
	st.AddLibraryItem(&m)
	mv := store.LibraryVersion{ItemID: m.ID, Version: "1.0", Dir: t.TempDir()}
	st.AddLibraryVersion(&mv)
	st.SetLibraryDefault(m.ID, true)
	envOf := func(b Build) string {
		t.Helper()
		srv, ok := b.MCPServers["stock-db"].(map[string]any)
		if !ok {
			t.Fatalf("serverul default lipseste: %v", b.TurnLines)
		}
		return srv["env"].(map[string]string)["DATABASE_URL"]
	}

	st.SetDefaultSecret(m.ID, "DATABASE_URL", "secret-value-one")
	b1, err := lib.Build(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if envOf(b1) != "secret-value-one" {
		t.Fatal("cheia globala nu a ajuns in env")
	}
	st.SetDefaultSecret(m.ID, "DATABASE_URL", "secret-value-two")
	b2, _ := lib.Build(p.ID)
	if b2.PluginDir == b1.PluginDir || envOf(b2) != "secret-value-two" {
		t.Fatalf("build-ul nu s-a refacut la cheia noua: %q %q", b1.PluginDir, b2.PluginDir)
	}
	if strings.Contains(b2.PluginDir, "secret-value") {
		t.Fatal("valoarea cheii apare in cale")
	}
	st.SetSecret(p.ID, m.ID, "DATABASE_URL", "project-own")
	b3, _ := lib.Build(p.ID)
	if envOf(b3) != "project-own" {
		t.Fatal("valoarea proiectului nu suprascrie cheia globala")
	}
}
