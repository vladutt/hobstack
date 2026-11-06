//go:build windows

package main

import (
	"log"
	"os"
	"os/exec"
	"syscall"
	"time"
	"unsafe"
)

var (
	kernel32 = syscall.NewLazyDLL("kernel32.dll")
	user32   = syscall.NewLazyDLL("user32.dll")
)

// Release-ul pe Windows e construit cu -H windowsgui: Windows nu-i mai deschide nicio consola (pe Windows 11
// consola implicita e Windows Terminal, pe care ShowWindow nu o poate ascunde, doar minimiza; inchisa, oprea HobStack).
// Un build simplu (go build) ramane aplicatie de consola, ca inainte.

// setupConsole pregateste consola la pornire; intoarce true cand HobStack ruleaza fara fereastra (log in fisier).
func setupConsole(start bool) bool {
	if consoleProcesses() > 0 {
		// build de consola: din terminal ramane in terminal; cu consola doar a lui (dublu-clic) o ascundem
		if start && consoleProcesses() == 1 {
			hideConsole()
			return true
		}
		return false
	}
	// exe-ul GUI, fara consola
	if !start {
		attachParent() // version / quit / install din cmd sau PowerShell: textul apare in terminal
		return false
	}
	hiddenConsole()
	return true
}

// consoleProcesses: cate procese folosesc consola noastra (0 = n-avem consola).
func consoleProcesses() int {
	proc := kernel32.NewProc("GetConsoleProcessList")
	if proc.Find() != nil {
		return 0
	}
	ids := make([]uint32, 4)
	n, _, _ := proc.Call(uintptr(unsafe.Pointer(&ids[0])), uintptr(len(ids)))
	return int(n)
}

// hideConsole ascunde fereastra consolei (build de consola pornit cu dublu-clic).
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

// hiddenConsole da procesului o consola fara fereastra. Fara ea, fiecare comanda pornita de agenti (git, claude,
// powershell) si-ar deschide fereastra ei. Pornim cmd.exe cu CREATE_NO_WINDOW (consola lui nu are fereastra),
// ne atasam la consola lui si il oprim; consola ramane cat timp o folosim noi, iar copiii o mostenesc.
func hiddenConsole() {
	comspec := os.Getenv("ComSpec")
	if comspec == "" {
		comspec = "cmd.exe"
	}
	cmd := exec.Command(comspec, "/q", "/k")
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x08000000} // CREATE_NO_WINDOW
	stdin, err := cmd.StdinPipe()                                     // tine cmd-ul deschis pana ne atasam
	if err != nil {
		log.Printf("hidden console: %v", err)
		return
	}
	if err := cmd.Start(); err != nil {
		log.Printf("hidden console: %v", err)
		return
	}
	attach := kernel32.NewProc("AttachConsole")
	ok := false
	for i := 0; i < 100 && !ok; i++ { // consola lui cmd apare dupa cateva milisecunde
		if r, _, _ := attach.Call(uintptr(cmd.Process.Pid)); r != 0 {
			ok = true
		} else {
			time.Sleep(20 * time.Millisecond)
		}
	}
	stdin.Close()
	cmd.Process.Kill()
	cmd.Wait()
	if !ok {
		log.Printf("hidden console: cannot attach; commands may open their own windows")
	}
}

// attachParent: un exe GUI pornit din terminal nu are stdout; ne legam la consola terminalului. Iesirile deja
// redirectate (pipe, de ex. proba `version` a update-ului sau installerul) raman neatinse.
func attachParent() {
	out, _ := syscall.GetStdHandle(syscall.STD_OUTPUT_HANDLE)
	if out != 0 && out != syscall.InvalidHandle {
		return
	}
	attach := kernel32.NewProc("AttachConsole")
	if r, _, _ := attach.Call(uintptr(^uint32(0))); r == 0 { // ATTACH_PARENT_PROCESS
		return
	}
	if f, err := os.OpenFile("CONOUT$", os.O_WRONLY, 0); err == nil {
		os.Stdout, os.Stderr = f, f
		log.SetOutput(f)
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
