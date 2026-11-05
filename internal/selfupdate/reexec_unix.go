//go:build !windows

package selfupdate

import (
	"os"
	"syscall"
)

// Reexec inlocuieste procesul curent cu binarul exe (acelasi PID, acelasi log).
// Se cheama dupa ce aplicatia a inchis portul si baza de date.
func Reexec(exe string, args []string) error {
	return syscall.Exec(exe, append([]string{exe}, args...), os.Environ())
}
