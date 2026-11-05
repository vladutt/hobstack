//go:build windows

package main

import (
	"fmt"
	"log"
	"runtime"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

// Iconita din zona de notificare (langa ceas): clic = deschide HobStack in browser, clic dreapta = meniul
// Open HobStack / Quit HobStack. Direct pe API-ul Windows (Shell_NotifyIconW), cu o fereastra ascunsa care
// primeste mesajele iconitei, pe un fir al ei (runtime.LockOSThread), cu bucla de mesaje proprie.

var (
	shell32 = syscall.NewLazyDLL("shell32.dll")

	pShellNotifyIcon     = shell32.NewProc("Shell_NotifyIconW")
	pRegisterClassEx     = user32.NewProc("RegisterClassExW")
	pCreateWindowEx      = user32.NewProc("CreateWindowExW")
	pDefWindowProc       = user32.NewProc("DefWindowProcW")
	pDestroyWindow       = user32.NewProc("DestroyWindow")
	pGetMessage          = user32.NewProc("GetMessageW")
	pTranslateMessage    = user32.NewProc("TranslateMessage")
	pDispatchMessage     = user32.NewProc("DispatchMessageW")
	pPostMessage         = user32.NewProc("PostMessageW")
	pPostQuitMessage     = user32.NewProc("PostQuitMessage")
	pCreatePopupMenu     = user32.NewProc("CreatePopupMenu")
	pAppendMenu          = user32.NewProc("AppendMenuW")
	pTrackPopupMenu      = user32.NewProc("TrackPopupMenu")
	pDestroyMenu         = user32.NewProc("DestroyMenu")
	pGetCursorPos        = user32.NewProc("GetCursorPos")
	pSetForegroundWindow = user32.NewProc("SetForegroundWindow")
	pLoadImage           = user32.NewProc("LoadImageW")
	pGetSystemMetrics    = user32.NewProc("GetSystemMetrics")
	pRegisterWindowMsg   = user32.NewProc("RegisterWindowMessageW")
	pMessageBox          = user32.NewProc("MessageBoxW")
	pGetModuleHandle     = kernel32.NewProc("GetModuleHandleW")
)

const (
	wmNull        = 0x0000
	wmDestroy     = 0x0002
	wmClose       = 0x0010
	wmCommand     = 0x0111
	wmLButtonUp   = 0x0202
	wmLButtonDbl  = 0x0203
	wmRButtonUp   = 0x0205
	wmContextMenu = 0x007B
	wmApp         = 0x8000
	wmTray        = wmApp + 1

	nimAdd    = 0
	nimDelete = 2
	nifFlags  = 0x1 | 0x2 | 0x4 // NIF_MESSAGE | NIF_ICON | NIF_TIP

	mfString    = 0x0
	mfSeparator = 0x800
	tpmFlags    = 0x0100 | 0x0080 | 0x0002 // TPM_RETURNCMD | TPM_NONOTIFY | TPM_RIGHTBUTTON

	cmdOpen = 1
	cmdQuit = 2
)

// notifyIconData e NOTIFYICONDATAW (976 de octeti pe 64 de biti; alinierea Go e cea din C).
type notifyIconData struct {
	CbSize           uint32
	HWnd             uintptr
	UID              uint32
	UFlags           uint32
	UCallbackMessage uint32
	HIcon            uintptr
	SzTip            [128]uint16
	DwState          uint32
	DwStateMask      uint32
	SzInfo           [256]uint16
	UVersion         uint32
	SzInfoTitle      [64]uint16
	DwInfoFlags      uint32
	GuidItem         [16]byte
	HBalloonIcon     uintptr
}

type wndClassEx struct {
	CbSize        uint32
	Style         uint32
	LpfnWndProc   uintptr
	CbClsExtra    int32
	CbWndExtra    int32
	HInstance     uintptr
	HIcon         uintptr
	HCursor       uintptr
	HbrBackground uintptr
	LpszMenuName  *uint16
	LpszClassName *uint16
	HIconSm       uintptr
}

type point struct{ X, Y int32 }

type msg struct {
	HWnd    uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      point
	Private uint32
}

// tray e iconita; o singura instanta pe proces (WndProc e o functie globala).
type tray struct {
	open   func()
	quit   func()
	active func() int

	hwnd           uintptr
	nid            notifyIconData
	taskbarCreated uintptr
	done           chan struct{}
	closeOnce      sync.Once
}

var theTray *tray

func utf16(s string) *uint16 {
	p, _ := syscall.UTF16PtrFromString(s)
	return p
}

// startTray pune iconita langa ceas. Close o scoate (la oprire si inainte de restart-ul dupa update).
func startTray(open, quit func(), active func() int) *tray {
	t := &tray{open: open, quit: quit, active: active, done: make(chan struct{})}
	ready := make(chan error, 1)
	go t.loop(ready)
	if err := <-ready; err != nil {
		log.Printf("tray icon: %v", err)
		return nil
	}
	return t
}

func (t *tray) loop(ready chan<- error) {
	runtime.LockOSThread() // fereastra si bucla de mesaje raman pe acelasi fir
	defer close(t.done)
	if err := pShellNotifyIcon.Find(); err != nil {
		ready <- err
		return
	}
	theTray = t
	inst, _, _ := pGetModuleHandle.Call(0)
	class := utf16("HobStackTray")
	wc := wndClassEx{LpfnWndProc: syscall.NewCallback(wndProc), HInstance: inst, LpszClassName: class}
	wc.CbSize = uint32(unsafe.Sizeof(wc))
	if r, _, err := pRegisterClassEx.Call(uintptr(unsafe.Pointer(&wc))); r == 0 {
		ready <- fmt.Errorf("RegisterClassEx: %v", err)
		return
	}
	hwnd, _, err := pCreateWindowEx.Call(0, uintptr(unsafe.Pointer(class)), uintptr(unsafe.Pointer(utf16("HobStack"))),
		0, 0, 0, 0, 0, 0, 0, inst, 0)
	if hwnd == 0 {
		ready <- fmt.Errorf("CreateWindowEx: %v", err)
		return
	}
	t.hwnd = hwnd
	t.taskbarCreated, _, _ = pRegisterWindowMsg.Call(uintptr(unsafe.Pointer(utf16("TaskbarCreated"))))

	// iconita din resursele exe-ului (grupul 1 din rsrc_windows_amd64.syso), la marimea mica a sistemului
	cx, _, _ := pGetSystemMetrics.Call(49) // SM_CXSMICON
	cy, _, _ := pGetSystemMetrics.Call(50) // SM_CYSMICON
	icon, _, _ := pLoadImage.Call(inst, 1, 1 /* IMAGE_ICON */, cx, cy, 0)
	t.nid = notifyIconData{HWnd: hwnd, UID: 1, UFlags: nifFlags, UCallbackMessage: wmTray, HIcon: icon}
	t.nid.CbSize = uint32(unsafe.Sizeof(t.nid))
	if tip, err := syscall.UTF16FromString("HobStack"); err == nil {
		copy(t.nid.SzTip[:], tip)
	}
	if !t.add() {
		pDestroyWindow.Call(hwnd)
		ready <- fmt.Errorf("Shell_NotifyIcon failed")
		return
	}
	ready <- nil

	var m msg
	for {
		r, _, _ := pGetMessage.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) <= 0 { // 0 = WM_QUIT, -1 = eroare
			return
		}
		pTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		pDispatchMessage.Call(uintptr(unsafe.Pointer(&m)))
	}
}

func (t *tray) add() bool {
	r, _, _ := pShellNotifyIcon.Call(nimAdd, uintptr(unsafe.Pointer(&t.nid)))
	return r != 0
}

// Close scoate iconita si opreste bucla (asteapta cel mult 2 s).
func (t *tray) Close() {
	if t == nil {
		return
	}
	t.closeOnce.Do(func() {
		pPostMessage.Call(t.hwnd, wmClose, 0, 0)
		select {
		case <-t.done:
		case <-time.After(2 * time.Second):
		}
	})
}

func (t *tray) menu() {
	m, _, _ := pCreatePopupMenu.Call()
	if m == 0 {
		return
	}
	defer pDestroyMenu.Call(m)
	pAppendMenu.Call(m, mfString, cmdOpen, uintptr(unsafe.Pointer(utf16("Open HobStack"))))
	pAppendMenu.Call(m, mfSeparator, 0, 0)
	pAppendMenu.Call(m, mfString, cmdQuit, uintptr(unsafe.Pointer(utf16("Quit HobStack"))))
	var p point
	pGetCursorPos.Call(uintptr(unsafe.Pointer(&p)))
	// fara SetForegroundWindow, meniul nu se inchide cand dai clic in alta parte
	pSetForegroundWindow.Call(t.hwnd)
	cmd, _, _ := pTrackPopupMenu.Call(m, tpmFlags, uintptr(p.X), uintptr(p.Y), 0, t.hwnd, 0)
	pPostMessage.Call(t.hwnd, wmNull, 0, 0)
	switch cmd {
	case cmdOpen:
		go t.open()
	case cmdQuit:
		t.confirmQuit()
	}
}

// confirmQuit: cu agenti care lucreaza intreaba inainte (task-urile lor revin in coada la urmatoarea pornire).
func (t *tray) confirmQuit() {
	if n := t.active(); n > 0 {
		text := fmt.Sprintf("%d agent(s) are working right now. Quit HobStack anyway?\n\nTheir tasks go back to Queued and continue the next time HobStack starts.", n)
		r, _, _ := pMessageBox.Call(t.hwnd, uintptr(unsafe.Pointer(utf16(text))), uintptr(unsafe.Pointer(utf16("HobStack"))),
			0x4|0x20) // MB_YESNO | MB_ICONQUESTION
		if r != 6 { // IDYES
			return
		}
	}
	go t.quit()
}

func wndProc(hwnd, message, wparam, lparam uintptr) uintptr {
	t := theTray
	switch {
	case t == nil:
	case message == wmTray:
		switch lparam & 0xffff {
		case wmLButtonUp, wmLButtonDbl:
			go t.open()
		case wmRButtonUp, wmContextMenu:
			t.menu()
		}
		return 0
	case message == wmClose:
		pDestroyWindow.Call(hwnd)
		return 0
	case message == wmDestroy:
		pShellNotifyIcon.Call(nimDelete, uintptr(unsafe.Pointer(&t.nid)))
		pPostQuitMessage.Call(0)
		return 0
	case t.taskbarCreated != 0 && message == t.taskbarCreated:
		t.add() // Explorer a repornit: iconita se pune din nou
		return 0
	}
	r, _, _ := pDefWindowProc.Call(hwnd, message, wparam, lparam)
	return r
}

// Pe Windows iconita are firul ei (startTray); main nu ruleaza o bucla separata.
func menuBarAvailable() bool { return false }

func runMenuBar() {}

func stopMenuBar() {}
