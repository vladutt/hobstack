package library

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"foreman/internal/store"
)

func newEnv(t *testing.T) (*store.Store, *Lib, string) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st, New(filepath.Join(t.TempDir(), "library"), st), t.TempDir()
}

func project(t *testing.T, st *store.Store) store.Project {
	t.Helper()
	p := store.Project{Name: "P", Goal: "g", Workdir: t.TempDir()}
	if _, err := st.CreateProject(&p); err != nil {
		t.Fatal(err)
	}
	return p
}

func addItem(t *testing.T, st *store.Store, kind, name string) store.LibraryItem {
	t.Helper()
	it := store.LibraryItem{Kind: kind, Name: name, Description: "d " + name, Status: store.ItemProposed}
	if err := st.AddLibraryItem(&it); err != nil {
		t.Fatal(err)
	}
	return it
}

func TestCopyProposalRejectsEscapes(t *testing.T) {
	st, lib, src := newEnv(t)
	os.WriteFile(filepath.Join(src, "run.sh"), []byte("#!/bin/sh\necho hi\n"), 0o755)
	os.Symlink("/etc/hosts", filepath.Join(src, "link"))
	os.MkdirAll(filepath.Join(src, "sub"), 0o755)
	// director cu symlink in cale: "evil" arata spre un folder din afara
	outside := t.TempDir()
	os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("x"), 0o644)
	os.Symlink(outside, filepath.Join(src, "evil"))
	it := addItem(t, st, store.KindTool, "hi")
	for _, bad := range [][]string{
		{"../outside"}, {"/etc/hosts"}, {"link"}, {}, {"sub"}, {"evil/secret.txt"}, {"sub/../../x"},
		{"run.sh", "run.sh"}, {"run.sh", "./run.sh"}, {"."}, {""}, {"missing.txt"},
	} {
		if _, _, _, err := lib.CopyProposal(it.ID, src, bad); err == nil {
			t.Fatalf("acceptat: %v", bad)
		}
	}
	dir, n, lines, err := lib.CopyProposal(it.ID, src, []string{"run.sh"})
	if err != nil || n != 1 || lines != 2 {
		t.Fatalf("%s %d %d %v", dir, n, lines, err)
	}
	if fi, err := os.Stat(filepath.Join(dir, "run.sh")); err != nil || fi.Mode()&0o100 == 0 {
		t.Fatalf("scriptul nu e executabil: %v %v", fi, err)
	}
}

func TestCopyProposalSymlinkedSrcDirAndLimits(t *testing.T) {
	st, lib, src := newEnv(t)
	it := addItem(t, st, store.KindTool, "lim")
	// srcDir in sine poate fi symlink (se rezolva o data)
	link := filepath.Join(t.TempDir(), "srclink")
	os.Symlink(src, link)
	os.WriteFile(filepath.Join(src, "a.txt"), []byte("a"), 0o644)
	if _, n, _, err := lib.CopyProposal(it.ID, link, []string{"a.txt"}); err != nil || n != 1 {
		t.Fatal(n, err)
	}
	var many []string
	for i := 0; i < 21; i++ {
		name := string(rune('a'+i)) + ".txt"
		os.WriteFile(filepath.Join(src, name), []byte("x"), 0o644)
		many = append(many, name)
	}
	if _, _, _, err := lib.CopyProposal(it.ID, src, many); err == nil {
		t.Fatal("peste 20 de fisiere acceptate")
	}
	os.WriteFile(filepath.Join(src, "big.bin"), make([]byte, 1<<20+1), 0o644)
	if _, _, _, err := lib.CopyProposal(it.ID, src, []string{"big.bin"}); err == nil {
		t.Fatal("peste 1 MB acceptat")
	}
	// o copiere esuata nu strica propunerea buna de dinainte
	if _, err := os.Stat(filepath.Join(lib.Root, "proposals", "1", "a.txt")); err != nil && it.ID == 1 {
		t.Fatal("propunerea buna a disparut")
	}
}

func TestBuildSkillsToolsAndMCP(t *testing.T) {
	st, lib, src := newEnv(t)
	p := project(t, st)
	os.MkdirAll(filepath.Join(src, "ro"), 0o755)
	os.WriteFile(filepath.Join(src, "ro", "SKILL.md"), []byte("---\nname: ro-copy\ndescription: Romanian copy\n---\nUse ș.\n"), 0o644)
	skill := addItem(t, st, store.KindSkill, "ro-copy")
	if _, _, _, err := lib.CopyProposal(skill.ID, filepath.Join(src, "ro"), []string{"SKILL.md"}); err != nil {
		t.Fatal(err)
	}
	v, err := lib.Publish(skill.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	st.Install(p.ID, skill.ID, v.ID)
	m := store.LibraryItem{Kind: store.KindMCP, Name: "stock-db", Description: "x", Status: store.ItemPublished,
		Transport: "local", Command: "python3", Args: []string{"server.py"}, Secrets: []string{"DATABASE_URL"}}
	st.AddLibraryItem(&m)
	mv := store.LibraryVersion{ItemID: m.ID, Version: "1.0", Dir: t.TempDir()}
	st.AddLibraryVersion(&mv)
	st.Install(p.ID, m.ID, mv.ID)

	b, err := lib.Build(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(b.PluginDir, "skills", "ro-copy", "SKILL.md")); err != nil {
		t.Fatal("skill-ul lipseste din plugin")
	}
	if _, err := os.Stat(filepath.Join(b.PluginDir, ".claude-plugin", "plugin.json")); err != nil {
		t.Fatal("plugin.json lipseste")
	}
	if len(b.MCPServers) != 0 || !strings.Contains(strings.Join(b.TurnLines, "\n"), "missing secret DATABASE_URL") {
		t.Fatalf("server fara secret pornit: %+v %v", b.MCPServers, b.TurnLines)
	}
	st.SetSecret(p.ID, m.ID, "DATABASE_URL", "postgres://x")
	b, _ = lib.Build(p.ID)
	srv := b.MCPServers["stock-db"].(map[string]any)
	if srv["env"].(map[string]string)["DATABASE_URL"] != "postgres://x" || b.Allow[0] != "mcp__stock-db" {
		t.Fatalf("%+v %v", srv, b.Allow)
	}
	if strings.Contains(strings.Join(b.TurnLines, "\n"), "postgres://x") {
		t.Fatal("secret in TurnLines")
	}
	os.RemoveAll(v.Dir)
	b, err = lib.Build(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if b.PluginDir != "" {
		t.Fatalf("skill sarit, dar PluginDir=%q", b.PluginDir)
	}
}

func TestBuildToolWrapperAndSecrets(t *testing.T) {
	st, lib, src := newEnv(t)
	p := project(t, st)
	os.WriteFile(filepath.Join(src, "go.sh"), []byte("#!/bin/sh\necho \"$KEY\"\n"), 0o755)
	it := store.LibraryItem{Kind: store.KindTool, Name: "go", Description: "d", Status: store.ItemProposed,
		Entry: "go.sh", Secrets: []string{"KEY"}, SourceProjectID: p.ID}
	st.AddLibraryItem(&it)
	if _, _, _, err := lib.CopyProposal(it.ID, src, []string{"go.sh"}); err != nil {
		t.Fatal(err)
	}
	if _, err := lib.Publish(it.ID, ""); err != nil {
		t.Fatal(err)
	}
	// publicarea a instalat tool-ul in proiectul sursa
	if ins, _ := st.ListInstalls(p.ID); len(ins) != 1 {
		t.Fatalf("installs: %v", ins)
	}
	if runtime.GOOS == "windows" {
		t.Skip("fara secrete pe windows")
	}
	b, _ := lib.Build(p.ID)
	if !strings.Contains(strings.Join(b.TurnLines, "\n"), "missing secret KEY") || len(b.ToolPaths) != 0 {
		t.Fatalf("tool fara secret: %v %v", b.TurnLines, b.ToolPaths)
	}
	st.SetSecret(p.ID, it.ID, "KEY", "a'b $(x)")
	b, _ = lib.Build(p.ID)
	if len(b.ToolPaths) != 1 {
		t.Fatalf("%v %v", b.ToolPaths, b.TurnLines)
	}
	var path string
	for k := range b.ToolPaths {
		path = k
	}
	fi, err := os.Stat(path)
	if err != nil || fi.Mode().Perm() != 0o700 {
		t.Fatalf("wrapper: %v %v", fi, err)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), `export KEY='a'\''b $(x)'`) {
		t.Fatalf("quoting gresit:\n%s", data)
	}
	if strings.Contains(strings.Join(b.TurnLines, "\n"), "a'b") || !strings.Contains(strings.Join(b.TurnLines, "\n"), path) {
		t.Fatalf("TurnLines: %v", b.TurnLines)
	}
}

func TestBuildIgnoresForeignVersion(t *testing.T) {
	st, lib, src := newEnv(t)
	p := project(t, st)
	a := addItem(t, st, store.KindSkill, "aa")
	b2 := addItem(t, st, store.KindSkill, "bb")
	os.WriteFile(filepath.Join(src, "SKILL.md"), []byte("x\n"), 0o644)
	lib.CopyProposal(b2.ID, src, []string{"SKILL.md"})
	vb, _ := lib.Publish(b2.ID, "")
	st.Install(p.ID, a.ID, vb.ID) // versiunea apartine altui item
	b, err := lib.Build(p.ID)
	if err != nil || b.PluginDir != "" {
		t.Fatalf("%+v %v", b, err)
	}
}

func TestPublishVersionsReadOnly(t *testing.T) {
	st, lib, src := newEnv(t)
	it := addItem(t, st, store.KindTool, "tt")
	os.WriteFile(filepath.Join(src, "run.sh"), []byte("echo 1\n"), 0o755)
	os.WriteFile(filepath.Join(src, "README"), []byte("r\n"), 0o644)
	lib.CopyProposal(it.ID, src, []string{"run.sh", "README"})
	v1, err := lib.Publish(it.ID, "first")
	if err != nil || v1.Version != "1.0" || v1.Files != 2 {
		t.Fatalf("%+v %v", v1, err)
	}
	if fi, _ := os.Stat(filepath.Join(v1.Dir, "README")); fi.Mode().Perm() != 0o444 {
		t.Fatalf("README %v", fi.Mode())
	}
	if fi, _ := os.Stat(filepath.Join(v1.Dir, "run.sh")); fi.Mode().Perm() != 0o555 {
		t.Fatalf("run.sh %v", fi.Mode())
	}
	if _, err := os.Stat(filepath.Join(lib.Root, "proposals", "1")); err == nil {
		t.Fatal("propunerea trebuia stearsa")
	}
	if got, _ := st.GetLibraryItem(it.ID); got.Status != store.ItemPublished {
		t.Fatal(got.Status)
	}
	// versiunea noua: fisier nou peste cea curenta
	os.WriteFile(filepath.Join(src, "extra.txt"), []byte("e\n"), 0o644)
	lib.CopyProposal(it.ID, src, []string{"extra.txt"})
	v2, err := lib.Publish(it.ID, "")
	if err != nil || v2.Version != "1.1" || v2.Files != 3 {
		t.Fatalf("%+v %v", v2, err)
	}
	if _, err := lib.Publish(it.ID, ""); err == nil {
		t.Fatal("fara propunere nu e nimic de publicat")
	}
}

func TestBuildConcurrent(t *testing.T) {
	st, lib, src := newEnv(t)
	p := project(t, st)
	it := addItem(t, st, store.KindSkill, "ss")
	os.WriteFile(filepath.Join(src, "SKILL.md"), []byte("x\n"), 0o644)
	lib.CopyProposal(it.ID, src, []string{"SKILL.md"})
	v, _ := lib.Publish(it.ID, "")
	st.Install(p.ID, it.ID, v.ID)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			b, err := lib.Build(p.ID)
			if err != nil {
				t.Error(err)
				return
			}
			if _, err := os.Stat(filepath.Join(b.PluginDir, "skills", "ss", "SKILL.md")); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
}

// publishSkill publica un skill cu un singur fisier si il instaleaza in proiect.
func publishSkill(t *testing.T, st *store.Store, lib *Lib, p store.Project, name, body string) store.LibraryItem {
	t.Helper()
	src := t.TempDir()
	os.WriteFile(filepath.Join(src, "SKILL.md"), []byte(body), 0o644)
	it := addItem(t, st, store.KindSkill, name)
	if _, _, _, err := lib.CopyProposal(it.ID, src, []string{"SKILL.md"}); err != nil {
		t.Fatal(err)
	}
	v, err := lib.Publish(it.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	st.Install(p.ID, it.ID, v.ID)
	return it
}

func TestBuildKeepsOldPluginDirAndReusesSame(t *testing.T) {
	st, lib, _ := newEnv(t)
	p := project(t, st)
	publishSkill(t, st, lib, p, "one", "one\n")
	b1, err := lib.Build(p.ID)
	if err != nil || b1.PluginDir == "" {
		t.Fatal(b1, err)
	}
	b1b, _ := lib.Build(p.ID)
	if b1b.PluginDir != b1.PluginDir {
		t.Fatalf("aceleasi intrari, alt folder: %s %s", b1.PluginDir, b1b.PluginDir)
	}
	publishSkill(t, st, lib, p, "two", "two\n")
	b2, err := lib.Build(p.ID)
	if err != nil || b2.PluginDir == b1.PluginDir {
		t.Fatalf("%s %s %v", b1.PluginDir, b2.PluginDir, err)
	}
	// folderul vechi (posibil folosit de un run) a ramas intact
	data, err := os.ReadFile(filepath.Join(b1.PluginDir, "skills", "one", "SKILL.md"))
	if err != nil || string(data) != "one\n" {
		t.Fatalf("%q %v", data, err)
	}
	if _, err := os.Stat(filepath.Join(b1.PluginDir, "skills", "two")); err == nil {
		t.Fatal("folderul vechi a fost modificat")
	}
	// folderele vechi de peste 24h se curata la urmatorul build, cel curent ramane
	old := time.Now().Add(-48 * time.Hour)
	os.Chtimes(b1.PluginDir, old, old)
	b3, _ := lib.Build(p.ID)
	if b3.PluginDir != b2.PluginDir {
		t.Fatal("build diferit fara schimbari")
	}
	if _, err := os.Stat(b1.PluginDir); err == nil {
		t.Fatal("folderul vechi trebuia sters")
	}
	if _, err := os.Stat(b2.PluginDir); err != nil {
		t.Fatal("folderul curent a fost sters")
	}
}

func TestCopyProposalRejectsHardlinkAndEnforcesBudget(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fara hardlink-uri")
	}
	st, lib, src := newEnv(t)
	it := addItem(t, st, store.KindTool, "hl")
	outside := filepath.Join(t.TempDir(), "id_rsa")
	os.WriteFile(outside, []byte("secret\n"), 0o600)
	if err := os.Link(outside, filepath.Join(src, "x")); err != nil {
		t.Skip("hardlink indisponibil:", err)
	}
	if _, _, _, err := lib.CopyProposal(it.ID, src, []string{"x"}); err == nil {
		t.Fatal("hardlink acceptat")
	}
	// bugetul de 1 MB se aplica pe octetii cititi, impartit intre fisiere
	os.WriteFile(filepath.Join(src, "f1"), make([]byte, 700<<10), 0o644)
	os.WriteFile(filepath.Join(src, "f2"), make([]byte, 400<<10), 0o644)
	if _, _, _, err := lib.CopyProposal(it.ID, src, []string{"f1", "f2"}); err == nil {
		t.Fatal("peste 1 MB in total acceptat")
	}
}

func TestPublishConcurrentSerialized(t *testing.T) {
	st, lib, src := newEnv(t)
	it := addItem(t, st, store.KindTool, "cc")
	os.WriteFile(filepath.Join(src, "run.sh"), []byte("echo 1\n"), 0o755)
	lib.CopyProposal(it.ID, src, []string{"run.sh"})
	var wg sync.WaitGroup
	errs := make([]error, 4)
	for i := range errs {
		wg.Add(1)
		go func() { defer wg.Done(); _, errs[i] = lib.Publish(it.ID, "") }()
	}
	wg.Wait()
	ok := 0
	for _, e := range errs {
		if e == nil {
			ok++
		}
	}
	// propunerea se consuma o data: o singura publicare reuseste, restul primesc eroare
	vs, _ := st.ListLibraryVersions(it.ID)
	if ok != 1 || len(vs) != 1 {
		t.Fatalf("ok=%d versiuni=%d %v", ok, len(vs), errs)
	}
	if _, err := os.Stat(filepath.Join(vs[0].Dir, "run.sh")); err != nil {
		t.Fatal("fisierele versiunii publicate lipsesc")
	}
	// un folder de versiune cu rand in baza nu se sterge
	lib.CopyProposal(it.ID, src, []string{"run.sh"})
	// rand "9.1" cu folder, apoi unul mai nou "9": urmatoarea versiune calculata e "9.1", deci cu rand in baza
	d := filepath.Join(lib.Root, "items", "1", "9.1")
	os.MkdirAll(d, 0o755)
	st.AddLibraryVersion(&store.LibraryVersion{ItemID: it.ID, Version: "9.1", Dir: d})
	st.AddLibraryVersion(&store.LibraryVersion{ItemID: it.ID, Version: "9", Dir: d + "-x"})
	if _, err := lib.Publish(it.ID, ""); err == nil {
		t.Fatal("destinatie cu rand in baza acceptata")
	}
}

func TestPublishRemovesOrphanDest(t *testing.T) {
	st, lib, src := newEnv(t)
	it := addItem(t, st, store.KindTool, "or")
	os.WriteFile(filepath.Join(src, "run.sh"), []byte("echo 1\n"), 0o755)
	lib.CopyProposal(it.ID, src, []string{"run.sh"})
	v1, _ := lib.Publish(it.ID, "")
	orphan := filepath.Join(lib.Root, "items", "1", "1.1")
	os.MkdirAll(orphan, 0o755)
	os.WriteFile(filepath.Join(orphan, "junk"), []byte("x"), 0o644)
	lib.CopyProposal(it.ID, src, []string{"run.sh"})
	v2, err := lib.Publish(it.ID, "")
	if err != nil || v2.Version != "1.1" || v1.Version != "1.0" {
		t.Fatalf("%+v %v", v2, err)
	}
	if _, err := os.Stat(filepath.Join(orphan, "run.sh")); err != nil {
		t.Fatal("fisierul propunerii lipseste")
	}
	if _, err := os.Stat(filepath.Join(orphan, "junk")); err == nil {
		t.Fatal("ramasita orfana")
	}
}

func TestBuildToolMissingEntryAndBadName(t *testing.T) {
	st, lib, src := newEnv(t)
	p := project(t, st)
	os.WriteFile(filepath.Join(src, "a.sh"), []byte("echo\n"), 0o755)
	it := store.LibraryItem{Kind: store.KindTool, Name: "nt", Description: "d", Status: store.ItemProposed, Entry: "gone.sh", SourceProjectID: p.ID}
	st.AddLibraryItem(&it)
	lib.CopyProposal(it.ID, src, []string{"a.sh"})
	if _, err := lib.Publish(it.ID, ""); err != nil {
		t.Fatal(err)
	}
	b, err := lib.Build(p.ID)
	if err != nil || len(b.ToolPaths) != 0 || !strings.Contains(strings.Join(b.TurnLines, "\n"), "entry file gone.sh is missing") {
		t.Fatalf("%+v %v", b, err)
	}
	bad := addItem(t, st, store.KindSkill, "Bad_Name")
	if _, err := lib.Publish(bad.ID, ""); err == nil {
		t.Fatal("nume nepermis acceptat")
	}
}

// la hash identic Build nu mai copiaza nimic: nu creeaza folder temporar, iar caile tool-urilor arata in folderul existent
func TestBuildHitCreatesNoTmp(t *testing.T) {
	st, lib, src := newEnv(t)
	p := project(t, st)
	publishSkill(t, st, lib, p, "tone", "---\nname: tone\ndescription: d\n---\nx\n")
	os.WriteFile(filepath.Join(src, "go.sh"), []byte("#!/bin/sh\necho hi\n"), 0o755)
	tool := store.LibraryItem{Kind: store.KindTool, Name: "go", Description: "d", Status: store.ItemProposed, Entry: "go.sh"}
	st.AddLibraryItem(&tool)
	if _, _, _, err := lib.CopyProposal(tool.ID, src, []string{"go.sh"}); err != nil {
		t.Fatal(err)
	}
	v, err := lib.Publish(tool.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	st.Install(p.ID, tool.ID, v.ID)
	b1, err := lib.Build(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	buildRoot := filepath.Join(lib.Root, "build")
	before, _ := os.ReadDir(buildRoot)
	// build-ul nu are voie sa scrie in build/: un MkdirTemp ar pica (nu merge pe windows / ca root)
	if runtime.GOOS != "windows" && os.Geteuid() != 0 {
		os.Chmod(buildRoot, 0o555)
		defer os.Chmod(buildRoot, 0o755)
	}
	b2, err := lib.Build(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadDir(buildRoot)
	if b2.PluginDir != b1.PluginDir || len(before) != len(after) {
		t.Fatalf("%v %v", before, after)
	}
	for path := range b2.ToolPaths {
		if !strings.HasPrefix(path, b2.PluginDir) {
			t.Fatalf("calea %s nu e in %s", path, b2.PluginDir)
		}
		if _, err := os.Stat(path); err != nil {
			t.Fatal(err)
		}
	}
	if len(b2.ToolPaths) != 1 || strings.Join(b1.TurnLines, "|") != strings.Join(b2.TurnLines, "|") {
		t.Fatalf("%v %v", b1.TurnLines, b2.TurnLines)
	}
}
