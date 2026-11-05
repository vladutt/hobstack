package claudecli

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestFindInLocalBin(t *testing.T) {
	t.Setenv("PATH", t.TempDir()) // fara claude in PATH
	home := t.TempDir()
	if got := Find(home); got != "" && !strings.HasPrefix(got, "/opt/homebrew") && !strings.HasPrefix(got, "/usr/local") {
		t.Fatalf("empty home: %q", got)
	}
	bin := filepath.Join(LocalBin(home), exeName())
	os.MkdirAll(filepath.Dir(bin), 0o755)
	os.WriteFile(bin, []byte("x"), 0o755)
	if got := Find(home); got != bin {
		t.Fatalf("Find = %q, want %q", got, bin)
	}
}

func TestStripANSI(t *testing.T) {
	if got := stripANSI("\x1b[32m✔\x1b[0m Claude Code installed"); got != "✔ Claude Code installed" {
		t.Fatalf("%q", got)
	}
}

func TestInstallerKeepsOutput(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sh")
	}
	in := NewInstaller()
	in.cmd = func(ctx context.Context) *exec.Cmd {
		return exec.CommandContext(ctx, "sh", "-c", "echo downloading; echo '\x1b[32mdone\x1b[0m' >&2; exit 0")
	}
	done := make(chan error, 1)
	if err := in.Start(context.Background(), func(err error) { done <- err }); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("installer did not finish")
	}
	st := in.State()
	if st.Running || !st.Done || st.Error != "" || strings.Join(st.Log, "|") != "downloading|done" {
		t.Fatalf("state: %+v", st)
	}

	in.cmd = func(ctx context.Context) *exec.Cmd { return exec.CommandContext(ctx, "sh", "-c", "echo nope; exit 3") }
	in.Start(context.Background(), func(err error) { done <- err })
	if err := <-done; err == nil || !strings.Contains(in.State().Error, "failed") {
		t.Fatalf("failing installer: %v %+v", err, in.State())
	}
}

func TestAddToPathUnix(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix shells")
	}
	home := t.TempDir()
	t.Setenv("SHELL", "/bin/zsh")
	t.Setenv("PATH", "/usr/bin:/bin")
	dir := LocalBin(home)
	if InPath(context.Background(), home, dir) {
		t.Fatal("not in PATH yet")
	}
	if _, err := AddToPath(context.Background(), home, dir); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(home, ".zshrc"))
	if !strings.Contains(string(b), rcLine) || !InPath(context.Background(), home, dir) {
		t.Fatalf(".zshrc: %q", b)
	}
	// a doua oara nu mai scrie nimic
	AddToPath(context.Background(), home, dir)
	if b2, _ := os.ReadFile(filepath.Join(home, ".zshrc")); string(b2) != string(b) {
		t.Fatalf("added twice: %q", b2)
	}
}
