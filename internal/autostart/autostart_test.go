//go:build !windows

package autostart

import (
	"os"
	"strings"
	"testing"
)

func TestEnableDisable(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", "")
	if Enabled(home, "") {
		t.Fatal("enabled in an empty home")
	}
	exe := "/Applications/Hob Stack/hobstack"
	if err := Enable(home, exe, home+"/.hobstack/hobstack.log"); err != nil {
		t.Fatal(err)
	}
	if !Enabled(home, "") {
		t.Fatal("not enabled after Enable")
	}
	var b []byte
	if p := plistOrDesktop(home); p != "" {
		b, _ = os.ReadFile(p)
	}
	if !strings.Contains(string(b), "Hob Stack") || !strings.Contains(string(b), "--no-open") {
		t.Fatalf("file: %s", b)
	}
	if err := Disable(home, ""); err != nil || Enabled(home, "") {
		t.Fatalf("Disable: %v", err)
	}
	if err := Disable(home, ""); err != nil {
		t.Fatalf("second Disable: %v", err)
	}
}

func TestDefaultOnlyOnce(t *testing.T) {
	home, root := t.TempDir(), t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", "")
	on, err := Default(root, home, "/x/hobstack", "/x/log")
	if err != nil || !on || !Enabled(home, "") {
		t.Fatalf("first start: %v %v", on, err)
	}
	// utilizatorul o opreste: urmatoarea pornire nu o mai porneste
	Disable(home, "")
	if on, err := Default(root, home, "/x/hobstack", "/x/log"); err != nil || on || Enabled(home, "") {
		t.Fatalf("after the user turned it off: %v %v", on, err)
	}
}

func TestDefaultFollowsTheExe(t *testing.T) {
	home, root := t.TempDir(), t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", "")
	if _, err := Default(root, home, "/Desktop/hobstack", "/x/log"); err != nil {
		t.Fatal(err)
	}
	// instalat apoi in alt folder: pornirea la login trece pe exe-ul nou
	if _, err := Default(root, home, "/Programs/HobStack/hobstack", "/x/log"); err != nil {
		t.Fatal(err)
	}
	if c := command(home); !strings.Contains(c, "/Programs/HobStack/hobstack") || strings.Contains(c, "/Desktop/") {
		t.Fatalf("still the old exe: %s", c)
	}
	// oprita de utilizator: ramane oprita
	Disable(home, "")
	Default(root, home, "/Other/hobstack", "/x/log")
	if Enabled(home, "") {
		t.Fatal("turned back on")
	}
}
