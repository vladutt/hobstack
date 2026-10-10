package skillsrc

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"hobstack/internal/store"
)

// pluginHome pune plugin-ul superpowers@m la versiunea data, cu skill-ul brainstorming; versiunea "" = dezinstalat.
func pluginHome(t *testing.T, home, version, body string) {
	t.Helper()
	file := filepath.Join(home, ".claude", "plugins", "installed_plugins.json")
	if version == "" {
		writeFile(t, file, `{"version":2,"plugins":{}}`)
		return
	}
	pl := filepath.Join(home, ".claude", "plugins", "cache", "superpowers", version)
	writeFile(t, filepath.Join(pl, "skills", "brainstorming", "SKILL.md"), body)
	writeFile(t, file, fmt.Sprintf(`{"version":2,"plugins":{"superpowers@m":[
		{"scope":"user","installPath":%q,"version":%q},
		{"scope":"project","installPath":"/elsewhere/old","version":"0.1"}]}}`, pl, version))
}

func TestLocalSkillsPluginKey(t *testing.T) {
	home := t.TempDir()
	pluginHome(t, home, "6.4.1", "---\nname: brainstorming\n---\n")
	got := LocalSkills(home)
	if len(got) != 1 || got[0].PluginKey != "superpowers@m" || got[0].Name != "brainstorming" {
		t.Fatalf("%+v", got)
	}
}

func TestResolveLocalPlugin(t *testing.T) {
	home := t.TempDir()
	pluginHome(t, home, "6.4.1", "v1")
	src := store.LibrarySource{Kind: store.SourcePlugin, Repo: "superpowers@m", Path: "brainstorming"}
	dir, label, err := ResolveLocal(home, src)
	want := filepath.Join(home, ".claude", "plugins", "cache", "superpowers", "6.4.1", "skills", "brainstorming")
	if err != nil || dir != want || label != "Plugin: superpowers 6.4.1" {
		t.Fatalf("%q %q %v", dir, label, err)
	}
	a, err := CheckSource(context.Background(), nil, home, src)
	if err != nil {
		t.Fatal(err)
	}
	// versiune noua cu alt SKILL.md: alta amprenta
	os.RemoveAll(filepath.Join(home, ".claude", "plugins", "cache"))
	pluginHome(t, home, "6.5.0", "v2")
	b, err := CheckSource(context.Background(), nil, home, src)
	if err != nil || a == b {
		t.Fatalf("6.5.0: %q %q %v", a, b, err)
	}
	if _, label, _ := ResolveLocal(home, src); label != "Plugin: superpowers 6.5.0" {
		t.Fatalf("label %q", label)
	}
	// dezinstalat
	pluginHome(t, home, "", "")
	if _, err := CheckSource(context.Background(), nil, home, src); !errors.Is(err, ErrNotFound) ||
		err.Error() != "Source no longer on this machine." {
		t.Fatalf("dezinstalat: %v", err)
	}
	// numele skill-ului nu iese din folderul skills
	bad := src
	bad.Path = "../../x"
	pluginHome(t, home, "6.5.0", "v2")
	if _, _, err := ResolveLocal(home, bad); err == nil {
		t.Fatal("cale cu .. acceptata")
	}
}

func TestLocalSkillsSymlinks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink-uri")
	}
	home := t.TempDir()
	sk := filepath.Join(home, ".claude", "skills")
	os.MkdirAll(sk, 0o755)
	// relativ, ca find-skills -> ../../.agents/skills/find-skills
	writeFile(t, filepath.Join(home, ".agents", "skills", "find-skills", "SKILL.md"), "---\nname: find-skills\ndescription: Find.\n---\n")
	writeFile(t, filepath.Join(home, ".agents", "skills", "find-skills", "ref.md"), "r")
	if err := os.Symlink(filepath.Join("..", "..", ".agents", "skills", "find-skills"), filepath.Join(sk, "find-skills")); err != nil {
		t.Fatal(err)
	}
	// absolut
	abs := filepath.Join(t.TempDir(), "absolute")
	writeFile(t, filepath.Join(abs, "SKILL.md"), "---\nname: abs\n---\n")
	os.Symlink(abs, filepath.Join(sk, "abs"))
	// fara SKILL.md, rupt, spre un fisier, SKILL.md symlink: ignorate
	writeFile(t, filepath.Join(home, "plain", "README.md"), "x")
	os.Symlink(filepath.Join(home, "plain"), filepath.Join(sk, "noskill"))
	os.Symlink(filepath.Join(home, "missing"), filepath.Join(sk, "dangling"))
	os.Symlink(filepath.Join(home, "plain", "README.md"), filepath.Join(sk, "file"))
	writeFile(t, filepath.Join(home, "md", "real.md"), "---\nname: x\n---\n")
	os.MkdirAll(filepath.Join(home, "mdlink"), 0o755)
	os.Symlink(filepath.Join(home, "md", "real.md"), filepath.Join(home, "mdlink", "SKILL.md"))
	os.Symlink(filepath.Join(home, "mdlink"), filepath.Join(sk, "mdlink"))

	got := LocalSkills(home)
	var names []string
	for _, s := range got {
		names = append(names, s.Name)
		if s.Path != filepath.Join(sk, s.Name) {
			t.Errorf("path %q pentru %s", s.Path, s.Name)
		}
	}
	if strings.Join(names, ",") != "abs,find-skills" {
		t.Fatalf("got %v", names)
	}
	link := filepath.Join(sk, "find-skills")
	dest := filepath.Join(t.TempDir(), "d")
	if err := CopyLocal(link, dest); err != nil {
		t.Fatal(err)
	}
	if got := listDir(t, dest); strings.Join(got, ",") != "SKILL.md,ref.md" {
		t.Fatalf("copied %v", got)
	}
	a, err := Fingerprint(link)
	b, _ := Fingerprint(filepath.Join(home, ".agents", "skills", "find-skills"))
	if err != nil || a != b {
		t.Fatalf("fingerprint %q %q %v", a, b, err)
	}
	if _, err := Fingerprint(filepath.Join(sk, "dangling")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("dangling: %v", err)
	}
}

// Ref-ul salvat e HEAD-ul repo-ului (PathCommitAt limitat la install): verificarea nu vede un update fals.
func TestCheckSourceHeadRef(t *testing.T) {
	const head, onPath, newer = "aaaaaaa1", "bbbbbbb2", "ccccccc3"
	latest := onPath
	r := testRemote(t, func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path != "/api/repos/o/skills/commits" {
			http.NotFound(w, req)
			return
		}
		sha := latest
		if req.URL.Query().Get("sha") == head {
			sha = onPath // ultimul commit pe cale pana la HEAD-ul de la install
		}
		w.Write([]byte(`[{"sha":"` + sha + `"}]`))
	})
	src := store.LibrarySource{Kind: store.SourceGitHub, Repo: "o/skills", Path: "skills/pdf", Ref: head}
	if got, err := CheckSource(context.Background(), r, "", src); err != nil || got != head {
		t.Fatalf("fara commit nou: %q %v", got, err)
	}
	latest = newer
	if got, err := CheckSource(context.Background(), r, "", src); err != nil || got != newer {
		t.Fatalf("commit nou: %q %v", got, err)
	}
}

func TestFetchPath(t *testing.T) {
	r := fetchRemote(t, tarball(t,
		tarEntry{Name: "pdf/SKILL.md", Body: skill("pdf")},
		tarEntry{Name: "tools/extra/pdf/SKILL.md", Body: skill("pdf")},
		tarEntry{Name: "tools/extra/pdf/a.txt", Body: "a"},
	))
	dest := filepath.Join(t.TempDir(), "d")
	if err := r.FetchPath(context.Background(), "o/skills", testSHA, "tools/extra/pdf", dest); err != nil {
		t.Fatal(err)
	}
	if got := listDir(t, dest); strings.Join(got, ",") != "SKILL.md,a.txt" {
		t.Fatalf("files %v", got)
	}
	err := r.FetchPath(context.Background(), "o/skills", testSHA, "tools/pdf", filepath.Join(t.TempDir(), "d"))
	if !errors.Is(err, ErrNotFound) || err.Error() != "The skill moved or was removed in o/skills." {
		t.Fatalf("lipsa: %v", err)
	}
	if err := r.FetchPath(context.Background(), "o/skills", testSHA, "../pdf", filepath.Join(t.TempDir(), "d")); err == nil || errors.Is(err, ErrNotFound) {
		t.Fatalf("cale invalida: %v", err)
	}
}
