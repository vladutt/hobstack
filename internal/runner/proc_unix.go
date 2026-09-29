//go:build !windows

package runner

import (
	"os/exec"
	"syscall"
	"time"
)

// Pornim claude in propriul process group, ca la Stop sa omoram
// si procesele copil (comenzi bash lansate de agent), nu doar claude.
func setProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
	}
	// daca nu iese in 5s dupa SIGTERM, Go trimite SIGKILL
	cmd.WaitDelay = 5 * time.Second
}
