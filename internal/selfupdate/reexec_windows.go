//go:build windows

package selfupdate

import (
	"os"
	"os/exec"
)

// Reexec: pe Windows nu exista exec, deci pornim binarul nou ca proces separat si iesim.
func Reexec(exe string, args []string) error {
	cmd := exec.Command(exe, args...)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Start(); err != nil {
		return err
	}
	os.Exit(0)
	return nil
}
