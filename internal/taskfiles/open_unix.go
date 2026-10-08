//go:build !windows

package taskfiles

import "syscall"

// oNonblock: la open nu blocam pe un FIFO (verificam tipul abia dupa Lstat).
const oNonblock = syscall.O_NONBLOCK
