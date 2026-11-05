//go:build windows

package main

import (
	"syscall"
	"unsafe"
)

var (
	kernel32 = syscall.NewLazyDLL("kernel32.dll")
	user32   = syscall.NewLazyDLL("user32.dll")
)

// ownConsole: consola a fost creata doar pentru procesul asta (dublu-clic in Explorer, pornire la login), nu e un
// terminal deschis de utilizator.
func ownConsole() bool {
	proc := kernel32.NewProc("GetConsoleProcessList")
	if proc.Find() != nil {
		return false
	}
	ids := make([]uint32, 4)
	n, _, _ := proc.Call(uintptr(unsafe.Pointer(&ids[0])), uintptr(len(ids)))
	return n == 1
}

// hideConsole ascunde fereastra consolei. Consola ramane (doar invizibila), deci comenzile pornite de agenti
// (git, claude, powershell) o mostenesc si nu-si deschid ferestre proprii.
func hideConsole() {
	get := kernel32.NewProc("GetConsoleWindow")
	show := user32.NewProc("ShowWindow")
	if get.Find() != nil || show.Find() != nil {
		return
	}
	if hwnd, _, _ := get.Call(); hwnd != 0 {
		show.Call(hwnd, 0) // SW_HIDE
	}
}

// messageBox arata o eroare cand HobStack ruleaza fara fereastra.
func messageBox(text string) {
	proc := user32.NewProc("MessageBoxW")
	if proc.Find() != nil {
		return
	}
	t, _ := syscall.UTF16PtrFromString(text)
	c, _ := syscall.UTF16PtrFromString("HobStack")
	proc.Call(0, uintptr(unsafe.Pointer(t)), uintptr(unsafe.Pointer(c)), 0x10) // MB_ICONERROR
}
