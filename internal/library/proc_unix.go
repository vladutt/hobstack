//go:build !windows

package library

import (
	"os/exec"
	"syscall"
	"time"
)

// Serverul MCP porneste in propriul process group, ca la timeout sa omoram si copiii lui.
func setProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return killTree(cmd) }
	cmd.WaitDelay = time.Second
}

// killTree omoara tot grupul de procese (SIGKILL).
func killTree(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
}

// hardLinked: un fisier cu mai multe nume poate fi un hardlink spre un fisier din afara folderului.
func hardLinked(fi interface{ Sys() any }) bool {
	st, ok := fi.Sys().(*syscall.Stat_t)
	return ok && st.Nlink > 1
}

// oNonblock: la open nu blocam pe un FIFO.
const oNonblock = syscall.O_NONBLOCK
