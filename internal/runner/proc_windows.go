//go:build windows

package runner

import "os/exec"

func setProcessGroup(cmd *exec.Cmd) {}

// endAfterResult opreste claude dupa rezultat (doar procesul lui; comenzile din fundal raman).
func endAfterResult(cmd *exec.Cmd) {
	if cmd.Process != nil {
		cmd.Process.Kill()
	}
}
