package skillsrc

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestFingerprint(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "SKILL.md"), skill("x"))
	writeFile(t, filepath.Join(dir, "ref", "a.md"), "a")
	a, err := Fingerprint(dir)
	if err != nil || len(a) != 64 {
		t.Fatalf("%q %v", a, err)
	}
	// acelasi continut in alt folder -> aceeasi amprenta
	other := t.TempDir()
	writeFile(t, filepath.Join(other, "ref", "a.md"), "a")
	writeFile(t, filepath.Join(other, "SKILL.md"), skill("x"))
	if b, _ := Fingerprint(other); b != a {
		t.Errorf("not stable: %s vs %s", a, b)
	}
	writeFile(t, filepath.Join(dir, "ref", "a.md"), "b")
	if c, _ := Fingerprint(dir); c == a {
		t.Error("content change not detected")
	}
	writeFile(t, filepath.Join(other, "ref", "b.md"), "a")
	if d, _ := Fingerprint(other); d == a {
		t.Error("new file not detected")
	}
	if _, err := Fingerprint(filepath.Join(dir, "missing")); err == nil {
		t.Error("missing folder gave no error")
	}
}

func TestListFiles(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "SKILL.md"), "hello")
	writeFile(t, filepath.Join(dir, "scripts", "a.PY"), "x")
	writeFile(t, filepath.Join(dir, "scripts", "b.mjs"), "x")
	writeFile(t, filepath.Join(dir, "bin", "tool"), "x")
	if runtime.GOOS != "windows" {
		if err := os.Chmod(filepath.Join(dir, "bin", "tool"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	files, err := ListFiles(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"SKILL.md": false, "bin/tool": runtime.GOOS != "windows", "scripts/a.PY": true, "scripts/b.mjs": true}
	if len(files) != len(want) {
		t.Fatalf("files = %+v", files)
	}
	for _, f := range files {
		if s, ok := want[f.Path]; !ok || s != f.Script {
			t.Errorf("%+v", f)
		}
	}
	if files[0].Path != "SKILL.md" || files[0].Size != 5 {
		t.Errorf("first = %+v", files[0])
	}
}

func TestFrontmatter(t *testing.T) {
	cases := []struct{ in, name, desc string }{
		{"---\nname: pdf\ndescription: Work with PDFs\n---\n# x", "pdf", "Work with PDFs"},
		{"---\r\nname: \"pdf\"\r\ndescription: 'It''s quoted'\r\n---\r\n", "pdf", "It's quoted"},
		{"\ufeff---\nname: a\ndescription: \"say \\\"hi\\\"\"\nlicense: MIT\n---\n", "a", `say "hi"`},
		{"---\nname: b\ndescription: >\n  folded\n  text\nother: 1\n---\n", "b", "folded text"},
		{"# no frontmatter\nname: x\n", "", ""},
		{"---\nname: open\n", "", ""},
		{"---\n  name: nested\ndescription: top\n---\n", "", "top"},
	}
	for _, c := range cases {
		n, d := Frontmatter([]byte(c.in))
		if n != c.name || d != c.desc {
			t.Errorf("Frontmatter(%q) = %q, %q; want %q, %q", c.in, n, d, c.name, c.desc)
		}
	}
}
