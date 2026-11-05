//go:build !windows && !darwin

package autostart

import (
	"os"
	"path/filepath"
	"strings"
)

func Supported() bool { return true }

func desktopPath(home string) string {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "autostart", "hobstack.desktop")
}

func Enabled(home, _ string) bool {
	_, err := os.Stat(desktopPath(home))
	return err == nil
}

// Enable scrie ~/.config/autostart/hobstack.desktop (XDG autostart, pornit de mediul grafic la login).
func Enable(home, exe, _ string) error {
	quote := func(s string) string { return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`, "`", "\\`", "$", `\$`).Replace(s) + `"` }
	desktop := "[Desktop Entry]\nType=Application\nName=" + Label + "\nExec=" + quote(exe) + " " + strings.Join(Args, " ") +
		"\nX-GNOME-Autostart-enabled=true\nNoDisplay=true\n"
	p := desktopPath(home)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	return os.WriteFile(p, []byte(desktop), 0o644)
}

func Disable(home, _ string) error {
	err := os.Remove(desktopPath(home))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}
