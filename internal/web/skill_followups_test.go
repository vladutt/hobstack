package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"hobstack/internal/store"
)

// setPlugin pune plugin-ul superpowers@m la versiunea data, cu brainstorming/SKILL.md = body;
// version "" = plugin dezinstalat.
func (e *skillEnv) setPlugin(t *testing.T, version, body string) string {
	t.Helper()
	file := filepath.Join(e.home, ".claude", "plugins", "installed_plugins.json")
	os.MkdirAll(filepath.Dir(file), 0o755)
	if version == "" {
		os.WriteFile(file, []byte(`{"version":2,"plugins":{}}`), 0o644)
		return ""
	}
	pl := filepath.Join(e.home, ".claude", "plugins", "cache", "superpowers", version)
	skill := filepath.Join(pl, "skills", "brainstorming")
	os.MkdirAll(skill, 0o755)
	if err := os.WriteFile(filepath.Join(skill, "SKILL.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(file, []byte(fmt.Sprintf(`{"version":2,"plugins":{"superpowers@m":[{"scope":"user","installPath":%q,"version":%q}]}}`,
		pl, version)), 0o644)
	return skill
}

func (e *skillEnv) checkItem(t *testing.T, id int64) (store.LibrarySource, bool, string) {
	t.Helper()
	code, raw := e.do(t, "POST", "/api/library/items/"+itoa(id)+"/source/check", nil)
	if code != http.StatusOK {
		t.Fatalf("check: %d %s", code, raw)
	}
	var out struct {
		Source          store.LibrarySource `json:"source"`
		UpdateAvailable bool                `json:"updateAvailable"`
		Note            string              `json:"note"`
	}
	json.Unmarshal(raw, &out)
	return out.Source, out.UpdateAvailable, out.Note
}

func (e *skillEnv) localInLibrary(t *testing.T, name string) bool {
	t.Helper()
	_, _, raw := e.get(t, "/api/library/skills/local")
	var out struct {
		Skills []struct {
			Name      string `json:"name"`
			InLibrary bool   `json:"inLibrary"`
		} `json:"skills"`
	}
	json.Unmarshal(raw, &out)
	for _, s := range out.Skills {
		if s.Name == name {
			return s.InLibrary
		}
	}
	t.Fatalf("%s lipseste din /local: %s", name, raw)
	return false
}

func TestSkillPluginFollowsVersion(t *testing.T) {
	e := setupSkills(t)
	old := e.setPlugin(t, "6.4.1", "---\nname: brainstorming\ndescription: Think.\n---\nv1\n")
	p := e.prepare(t, map[string]string{"kind": "local", "path": old})
	it := e.install(t, map[string]any{"token": p.Token, "name": "brainstorming"})
	if s := it.Source; s == nil || s.Kind != store.SourcePlugin || s.Repo != "superpowers@m" || s.Path != "brainstorming" ||
		s.Label != "Plugin: superpowers 6.4.1" {
		t.Fatalf("sursa %+v", it.Source)
	}
	if _, up, _ := e.checkItem(t, it.ID); up {
		t.Fatal("update fals")
	}
	// plugin-ul trece pe 6.5.0 cu alt SKILL.md
	os.RemoveAll(filepath.Join(e.home, ".claude", "plugins", "cache"))
	e.setPlugin(t, "6.5.0", "---\nname: brainstorming\ndescription: Think.\n---\nv2\n")
	if _, up, note := e.checkItem(t, it.ID); !up || note != "" {
		t.Fatalf("6.5.0: update %v %q", up, note)
	}
	if !e.localInLibrary(t, "brainstorming") {
		t.Fatal("skill-ul din 6.5.0 trebuie sa fie In library")
	}
	up := e.sourcePrepare(t, it.ID)
	code, raw := e.do(t, "POST", "/api/library/items/"+itoa(it.ID)+"/source/update", map[string]string{"token": up.Token})
	if code != http.StatusOK {
		t.Fatalf("update: %d %s", code, raw)
	}
	var after itemOut
	json.Unmarshal(raw, &after)
	if after.Source == nil || after.Source.Label != "Plugin: superpowers 6.5.0" || after.UpdateAvailable {
		t.Fatalf("after %s", raw)
	}
	if b, _ := os.ReadFile(filepath.Join(after.Version.Dir, "SKILL.md")); !strings.Contains(string(b), "v2") {
		t.Fatalf("SKILL.md %q", b)
	}
	// dezinstalat
	e.setPlugin(t, "", "")
	if _, _, note := e.checkItem(t, it.ID); note != "Source no longer on this machine." {
		t.Fatalf("dezinstalat: %q", note)
	}
	code, raw = e.do(t, "POST", "/api/library/items/"+itoa(it.ID)+"/source/prepare", nil)
	if code != http.StatusNotFound || errMsg(raw) != "Source no longer on this machine." {
		t.Fatalf("prepare dezinstalat: %d %s", code, raw)
	}
}

func TestSkillSymlinkedLocal(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink-uri")
	}
	e := setupSkills(t)
	real := filepath.Join(e.home, ".agents", "skills", "find-skills")
	os.MkdirAll(real, 0o755)
	os.WriteFile(filepath.Join(real, "SKILL.md"), []byte("---\nname: find-skills\ndescription: Find.\n---\n"), 0o644)
	sk := filepath.Join(e.home, ".claude", "skills")
	os.MkdirAll(sk, 0o755)
	if err := os.Symlink(filepath.Join("..", "..", ".agents", "skills", "find-skills"), filepath.Join(sk, "find-skills")); err != nil {
		t.Fatal(err)
	}
	os.Symlink(filepath.Join(e.home, "nowhere"), filepath.Join(sk, "dangling"))
	link := filepath.Join(sk, "find-skills")
	if e.localInLibrary(t, "find-skills") {
		t.Fatal("inca nu e in Library")
	}
	p := e.prepare(t, map[string]string{"kind": "local", "path": link})
	if p.Source.Path != link || len(p.Files) != 1 {
		t.Fatalf("prepare %+v", p)
	}
	it := e.install(t, map[string]any{"token": p.Token, "name": "find-skills"})
	if it.Source == nil || it.Source.Path != link {
		t.Fatalf("sursa %+v", it.Source)
	}
	if !e.localInLibrary(t, "find-skills") {
		t.Fatal("trebuia In library")
	}
	if _, up, note := e.checkItem(t, it.ID); up || note != "" {
		t.Fatalf("neschimbat: %v %q", up, note)
	}
	os.WriteFile(filepath.Join(real, "more.md"), []byte("x"), 0o644)
	if _, up, _ := e.checkItem(t, it.ID); !up {
		t.Fatal("schimbarea din folderul tinta nu se vede")
	}
}

func TestSkillInstallPathCommitLimited(t *testing.T) {
	e := setupSkills(t)
	e.gh.set(func(f *fakeGitHub) { f.pathLimit = true })
	it := e.install(t, map[string]any{"token": e.prepareGitHub(t).Token, "name": "pdf"})
	if it.Source == nil || it.Source.Ref != shaOne {
		t.Fatalf("ref %+v", it.Source)
	}
	e.gh.set(func(f *fakeGitHub) { f.pathLimit = false })
	if src, up, _ := e.checkItem(t, it.ID); up || src.Latest != shaOne {
		t.Fatalf("fara commit nou: %+v %v", src, up)
	}
	// un commit nou pe cale se vede
	e.newCommit(shaPath2, map[string]string{"SKILL.md": pdfSkill("New.")})
	e.gh.set(func(f *fakeGitHub) { f.head["o/skills"] = shaTwo })
	if src, up, _ := e.checkItem(t, it.ID); !up || src.Latest != shaPath2 {
		t.Fatalf("commit nou: %+v %v", src, up)
	}
}

func TestSkillUpdateExactPath(t *testing.T) {
	e := setupSkills(t)
	const p1, p2, p3 = "7777777aaaa", "8888888bbbb", "9999999cccc"
	e.gh.set(func(f *fakeGitHub) {
		f.head["o/other"] = p1
		f.pathSHA["o/other:tools/extra/pdf"] = p1
		f.archives["o/other@"+p1] = map[string]string{"tools/extra/pdf/SKILL.md": pdfSkill("Deep."), "tools/extra/pdf/a.txt": "a1"}
	})
	p := e.prepare(t, map[string]string{"kind": "github", "repo": "o/other", "skillId": "pdf"})
	if p.Source.Path != "tools/extra/pdf" {
		t.Fatalf("path %q", p.Source.Path)
	}
	it := e.install(t, map[string]any{"token": p.Token, "name": "pdf"})
	// apare un folder pdf/ mai scurt, iar cel instalat se schimba
	e.gh.set(func(f *fakeGitHub) {
		f.head["o/other"] = p2
		f.pathSHA["o/other:tools/extra/pdf"] = p2
		f.archives["o/other@"+p2] = map[string]string{"pdf/SKILL.md": pdfSkill("Other."),
			"tools/extra/pdf/SKILL.md": pdfSkill("Deep."), "tools/extra/pdf/a.txt": "a2"}
	})
	up := e.sourcePrepare(t, it.ID)
	if len(up.Diff) != 1 || up.Diff[0]["path"] != "a.txt" || up.Diff[0]["after"] != "a2" {
		t.Fatalf("diff %+v", up.Diff)
	}
	// folderul instalat dispare
	e.gh.set(func(f *fakeGitHub) {
		f.head["o/other"] = p3
		f.pathSHA["o/other:tools/extra/pdf"] = p3
		f.archives["o/other@"+p3] = map[string]string{"pdf/SKILL.md": pdfSkill("Other.")}
	})
	code, raw := e.do(t, "POST", "/api/library/items/"+itoa(it.ID)+"/source/prepare", nil)
	if code != http.StatusNotFound || errMsg(raw) != "The skill moved or was removed in o/other." {
		t.Fatalf("lipsa: %d %s", code, raw)
	}
}

func TestLibraryCreateSkillBodyLimit(t *testing.T) {
	e := setupLib(t)
	// 9 fisiere de ~1 MB: sub limita de 10 MB a unui skill
	var files []map[string]string
	chunk := strings.Repeat("abcdefghi\n", 100<<10)
	for i := 0; i < 9; i++ {
		files = append(files, map[string]string{"path": fmt.Sprintf("ref/f%d.md", i), "content": chunk})
	}
	code, raw := e.do(t, "POST", "/api/library", map[string]any{"kind": "skill", "name": "big-skill", "description": "d",
		"content": "# Big", "files": files})
	if code != http.StatusCreated {
		t.Fatalf("9 MB: %d %.200s", code, raw)
	}
	files = append(files, map[string]string{"path": "huge.md", "content": strings.Repeat("x", 8<<20)})
	code, raw = e.do(t, "POST", "/api/library", map[string]any{"kind": "skill", "name": "huge-skill", "description": "d",
		"content": "# Huge", "files": files})
	if code != http.StatusRequestEntityTooLarge || errMsg(raw) != "The files are too big (max 10 MB for a skill)." {
		t.Fatalf("17 MB: %d %.200s", code, raw)
	}
}
