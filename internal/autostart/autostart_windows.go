//go:build windows

package autostart

import (
	"errors"
	"os/exec"
	"strings"
	"syscall"
)

const runKey = `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`

// Supported: pe Windows merge mereu.
func Supported() bool { return true }

func reg(args ...string) (string, error) {
	cmd := exec.Command("reg", args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// Enabled spune daca HobStack porneste la login (exista valoarea in Run).
func Enabled(_, _ string) bool {
	_, err := reg("query", runKey, "/v", Label)
	return err == nil
}

// Enable scrie valoarea in Run: "<exe>" start --no-open.
func Enable(_, exe, _ string) error {
	value := `"` + exe + `" ` + strings.Join(Args, " ")
	if out, err := reg("add", runKey, "/v", Label, "/t", "REG_SZ", "/d", value, "/f"); err != nil {
		return errors.New("writing the Run key failed: " + strings.TrimSpace(out))
	}
	return nil
}

// command: comanda din Run ("" daca lipseste).
func command(string) string {
	out, err := reg("query", runKey, "/v", Label)
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(out, "\n") {
		if _, v, ok := strings.Cut(line, "REG_SZ"); ok {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// Disable sterge valoarea din Run (fara eroare daca nu exista).
func Disable(_, _ string) error {
	if !Enabled("", "") {
		return nil
	}
	if out, err := reg("delete", runKey, "/v", Label, "/f"); err != nil {
		return errors.New("removing the Run key failed: " + strings.TrimSpace(out))
	}
	return nil
}
