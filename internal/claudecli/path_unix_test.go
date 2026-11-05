//go:build !windows

package claudecli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAddToPathUnix(t *testing.T) {
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
