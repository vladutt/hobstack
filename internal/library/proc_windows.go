//go:build windows

package library

import "os/exec"

func setProcessGroup(cmd *exec.Cmd) {}

func killTree(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	return cmd.Process.Kill()
}

// Pe Windows nu verificam hardlink-urile.
func hardLinked(fi interface{ Sys() any }) bool { return false }
