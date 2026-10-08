package taskfiles

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func write(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestCopy(t *testing.T) {
	home, other, outside, dest := t.TempDir(), t.TempDir(), t.TempDir(), t.TempDir()
	roots := []string{home, other}
	write(t, filepath.Join(home, "docs", "report.md"), []byte("# Report\n"))
	write(t, filepath.Join(other, "logo.png"), []byte("\x89PNG\r\n\x1a\n0000"))
	write(t, filepath.Join(outside, "secret.txt"), []byte("key"))

	c, err := Copy(roots, home, "docs/report.md", dest)
	if err != nil {
		t.Fatal(err)
	}
	if c.Path != filepath.Join("docs", "report.md") || c.Name != "report.md" || c.Size != 9 {
		t.Fatalf("%+v", c)
	}
	if got, _ := os.ReadFile(filepath.Join(dest, c.Stored)); string(got) != "# Report\n" {
		t.Fatalf("copia: %q", got)
	}
	// acelasi fisier inca o data: alt nume pe disc (versiunea veche ramane)
	c2, err := Copy(roots, home, "./docs/report.md", dest)
	if err != nil || c2.Stored == c.Stored {
		t.Fatalf("%+v %v", c2, err)
	}
	// cale absoluta in alt folder al proiectului: acceptata, afisata absolut
	abs := filepath.Join(other, "logo.png")
	c3, err := Copy(roots, home, abs, dest)
	if err != nil || c3.Path != abs || c3.Mime != "image/png" {
		t.Fatalf("%+v %v", c3, err)
	}

	bad := map[string]string{
		filepath.Join(outside, "secret.txt"):           "outside the project folders",
		"../" + filepath.Base(outside) + "/secret.txt": "outside the project folders",
		"missing.md": "not found",
		"docs":       "not a regular file",
		" ":          "empty path",
	}
	for raw, want := range bad {
		if _, err := Copy(roots, home, raw, dest); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: %v (vreau %q)", raw, err, want)
		}
	}
}

func TestCopySymlinkOut(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink-uri pe Windows cer drepturi")
	}
	home, outside, dest := t.TempDir(), t.TempDir(), t.TempDir()
	write(t, filepath.Join(outside, "id_rsa"), []byte("key"))
	if err := os.Symlink(filepath.Join(outside, "id_rsa"), filepath.Join(home, "key.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(home, "dir")); err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{"key.txt", "dir/id_rsa"} {
		if _, err := Copy([]string{home}, home, raw, dest); err == nil {
			t.Errorf("%s: symlink spre exterior acceptat", raw)
		}
	}
}

// folderul proiectului salvat printr-un symlink (ex. /var -> /private/var pe macOS), iar agentul
// preda calea reala (getcwd o intoarce rezolvata): trebuie acceptata, si invers
func TestCopySymlinkedRoot(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink-uri pe Windows cer drepturi")
	}
	real, dest := t.TempDir(), t.TempDir()
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(real, "docs", "a.md"), []byte("# A"))

	c, err := Copy([]string{link}, link, filepath.Join(real, "docs", "a.md"), dest)
	if err != nil || c.Path != filepath.Join("docs", "a.md") {
		t.Fatalf("root prin symlink, cale reala: %+v %v", c, err)
	}
	c, err = Copy([]string{real}, real, filepath.Join(link, "docs", "a.md"), dest)
	if err != nil || c.Path != filepath.Join("docs", "a.md") {
		t.Fatalf("root real, cale prin symlink: %+v %v", c, err)
	}
	// un symlink din folder spre exterior ramane refuzat si dupa rezolvare
	outside := t.TempDir()
	write(t, filepath.Join(outside, "secret.txt"), []byte("key"))
	if err := os.Symlink(outside, filepath.Join(real, "out")); err != nil {
		t.Fatal(err)
	}
	if _, err := Copy([]string{link}, link, filepath.Join(link, "out", "secret.txt"), dest); err == nil {
		t.Fatal("symlink spre exterior acceptat dupa rezolvare")
	}
}

func TestCopyTooLarge(t *testing.T) {
	home, dest := t.TempDir(), t.TempDir()
	write(t, filepath.Join(home, "big.log"), bytes.Repeat([]byte("a"), MaxSize+1))
	if _, err := Copy([]string{home}, home, "big.log", dest); err == nil || !strings.Contains(err.Error(), "larger than 50 MB") {
		t.Fatalf("%v", err)
	}
	if entries, _ := os.ReadDir(dest); len(entries) != 0 {
		t.Fatalf("a ramas o copie partiala: %v", entries)
	}
}
