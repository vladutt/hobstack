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

// endAfterResult opreste claude dupa rezultat cu SIGKILL pe grupul lui: comenzile pornite in fundal de agent au
// grupul lor (nu le atingem, un server de dev ramane pornit), iar claude nu mai apuca sa le opreasca.
func endAfterResult(cmd *exec.Cmd) {
	if cmd.Process != nil {
		syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}
