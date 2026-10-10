package skillsrc

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLocalSkills(t *testing.T) {
	home := t.TempDir()
	sk := filepath.Join(home, ".claude", "skills")
	writeFile(t, filepath.Join(sk, "notes", "SKILL.md"), "---\nname: notes\ndescription: 'Daily notes'\n---\n")
	writeFile(t, filepath.Join(sk, "empty", "README.md"), "no skill here")
	// claude.ai: un skill cu descriere in manifest, unul fara intrare in manifest
	acct := filepath.Join(sk, "synced", "acc-1")
	writeFile(t, filepath.Join(acct, "brand", "SKILL.md"), "---\nname: brand\ndescription: from frontmatter\n---\n")
	writeFile(t, filepath.Join(acct, "loose", "SKILL.md"), "---\nname: loose\ndescription: loose one\n---\n")
	writeFile(t, filepath.Join(acct, "manifest.json"),
		`{"lastUpdated":1,"skills":[{"skillId":"s1","name":"brand","description":"Brand voice","source":"user","creatorType":"user"}]}`)
	// al doilea cont fara manifest
	writeFile(t, filepath.Join(sk, "synced", "acc-2", "solo", "SKILL.md"), "---\ndescription: \"Solo skill\"\n---\n")
	// plugin
	pl := filepath.Join(home, ".claude", "plugins", "cache", "superpowers", "6.4.1")
	writeFile(t, filepath.Join(pl, "skills", "brainstorming", "SKILL.md"), "---\nname: brainstorming\ndescription: Think first\n---\n")
	writeFile(t, filepath.Join(pl, "skills", "nope", "x.md"), "x")
	writeFile(t, filepath.Join(home, ".claude", "plugins", "installed_plugins.json"), fmt.Sprintf(
		`{"version":2,"plugins":{"superpowers@claude-plugins-official":[
			{"scope":"user","installPath":%q,"version":"6.4.1","gitCommitSha":"abc"},
			{"scope":"project","installPath":%q,"version":"6.4.1"}],
		  "rel@m":[{"installPath":"relative/path","version":"1"}]}}`, pl, pl))

	got := LocalSkills(home)
	var lines []string
	for _, s := range got {
		lines = append(lines, s.Kind+"|"+s.Source+"|"+s.Label+"|"+s.Name+"|"+s.Description)
		if !filepath.IsAbs(s.Path) || !strings.HasSuffix(s.Path, s.Name) {
			t.Errorf("path %q for %s", s.Path, s.Name)
		}
	}
	want := []string{
		"claude-code|Claude Code|Claude Code|notes|Daily notes",
		"claude-ai|claude.ai|claude.ai|brand|Brand voice",
		"claude-ai|claude.ai|claude.ai|loose|loose one",
		"claude-ai|claude.ai|claude.ai|solo|Solo skill",
		"plugin|Plugin: superpowers|Plugin: superpowers 6.4.1|brainstorming|Think first",
	}
	if strings.Join(lines, "\n") != strings.Join(want, "\n") {
		t.Errorf("got:\n%s\nwant:\n%s", strings.Join(lines, "\n"), strings.Join(want, "\n"))
	}
}

func TestLocalSkillsBadFiles(t *testing.T) {
	home := t.TempDir()
	writeFile(t, filepath.Join(home, ".claude", "skills", "a", "SKILL.md"), "---\nname: a\n---\n")
	writeFile(t, filepath.Join(home, ".claude", "skills", "synced", "acc", "manifest.json"), "{bad")
	writeFile(t, filepath.Join(home, ".claude", "skills", "synced", "acc", "b", "SKILL.md"), "---\ndescription: B\n---\n")
	writeFile(t, filepath.Join(home, ".claude", "plugins", "installed_plugins.json"), "not json")
	got := LocalSkills(home)
	if len(got) != 2 || got[0].Name != "a" || got[1].Name != "b" || got[1].Description != "B" {
		t.Errorf("got %+v", got)
	}
	if LocalSkills(t.TempDir()) != nil {
		t.Error("empty home should give nil")
	}
}

func TestCopyLocal(t *testing.T) {
	src := t.TempDir()
	writeFile(t, filepath.Join(src, "SKILL.md"), skill("x"))
	writeFile(t, filepath.Join(src, "scripts", "a.sh"), "echo")
	writeFile(t, filepath.Join(src, ".git", "HEAD"), "ref")
	if runtime.GOOS != "windows" {
		outside := filepath.Join(t.TempDir(), "secret")
		writeFile(t, outside, "secret")
		if err := os.Symlink(outside, filepath.Join(src, "link")); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(filepath.Dir(outside), filepath.Join(src, "dirlink")); err != nil {
			t.Fatal(err)
		}
	}
	dest := filepath.Join(t.TempDir(), "d")
	if err := CopyLocal(src, dest); err != nil {
		t.Fatal(err)
	}
	if got := listDir(t, dest); strings.Join(got, ",") != "SKILL.md,scripts/a.sh" {
		t.Errorf("copied %v", got)
	}

	big := t.TempDir()
	writeFile(t, filepath.Join(big, "SKILL.md"), "---\nname: big\n---\n")
	for i := 0; i < 201; i++ {
		writeFile(t, filepath.Join(big, fmt.Sprintf("f%d", i)), "x")
	}
	if err := CopyLocal(big, filepath.Join(t.TempDir(), "d")); !errors.Is(err, ErrTooLarge) {
		t.Errorf("201 files: %v", err)
	}
	huge := t.TempDir()
	writeFile(t, filepath.Join(huge, "SKILL.md"), "---\nname: huge\n---\n")
	if err := os.WriteFile(filepath.Join(huge, "blob"), make([]byte, 11<<20), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := CopyLocal(huge, filepath.Join(t.TempDir(), "d")); !errors.Is(err, ErrTooLarge) {
		t.Errorf("11 MB: %v", err)
	}
}
