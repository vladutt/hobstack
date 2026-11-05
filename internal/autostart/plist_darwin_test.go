package autostart

import (
	"os"
	"os/exec"
	"testing"
)

func TestPlistIsValid(t *testing.T) {
	home := t.TempDir()
	if err := Enable(home, "/tmp/a & b/hobstack", "/tmp/x.log"); err != nil {
		t.Fatal(err)
	}
	if _, err := exec.LookPath("plutil"); err != nil {
		t.Skip("no plutil")
	}
	if out, err := exec.Command("plutil", "-lint", plistPath(home)).CombinedOutput(); err != nil {
		b, _ := os.ReadFile(plistPath(home))
		t.Fatalf("%s\n%s", out, b)
	}
}
