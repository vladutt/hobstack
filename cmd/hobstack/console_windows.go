//go:build windows

package main

import (
	"syscall"
	"unsafe"
)

// ownConsole: consola a fost creata doar pentru procesul asta (dublu-clic in Explorer), nu e un terminal deschis
// de utilizator. Atunci fereastra se inchide odata cu procesul.
func ownConsole() bool {
	proc := syscall.NewLazyDLL("kernel32.dll").NewProc("GetConsoleProcessList")
	if proc.Find() != nil {
		return false
	}
	ids := make([]uint32, 4)
	n, _, _ := proc.Call(uintptr(unsafe.Pointer(&ids[0])), uintptr(len(ids)))
	return n == 1
}
