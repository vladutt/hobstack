//go:build windows

package main

import (
	"log"
	"sync"
	"syscall"
	"unsafe"
)

// Notificarile pe Windows: balonul iconitei de langa ceas (Shell_NotifyIconW cu NIF_INFO). Windows 10 / 11 il
// arata ca notificare (toast). Clic pe el = NIN_BALLOONUSERCLICK pe mesajul iconitei (wndProc din tray_windows.go).

const (
	nimModify           = 1
	nifInfo             = 0x10
	niifInfo            = 0x1
	ninBalloonUserClick = 0x0405 // WM_USER + 5
)

// balloonClick: ce face clic-ul pe ultimul balon (Windows arata unul singur o data).
var balloonClick struct {
	sync.Mutex
	fn func()
}

func sendNotification(title, body string, onClick func()) {
	t := theTray
	if t == nil || t.hwnd == 0 {
		return // fara iconita (n-a pornit): nu avem pe ce pune balonul
	}
	balloonClick.Lock()
	balloonClick.fn = onClick
	balloonClick.Unlock()
	// o copie: t.nid e citit si de firul iconitei (add dupa repornirea Explorer-ului)
	nid := notifyIconData{CbSize: t.nid.CbSize, HWnd: t.nid.HWnd, UID: t.nid.UID, UFlags: nifInfo, DwInfoFlags: niifInfo}
	if s, err := syscall.UTF16FromString(title); err == nil {
		copy(nid.SzInfoTitle[:len(nid.SzInfoTitle)-1], s)
	}
	if s, err := syscall.UTF16FromString(body); err == nil {
		copy(nid.SzInfo[:len(nid.SzInfo)-1], s)
	}
	if r, _, err := pShellNotifyIcon.Call(nimModify, uintptr(unsafe.Pointer(&nid))); r == 0 {
		log.Printf("notification: %v", err)
	}
}

// balloonClicked: din wndProc (firul iconitei); munca merge in goroutine.
func balloonClicked() {
	balloonClick.Lock()
	fn := balloonClick.fn
	balloonClick.fn = nil
	balloonClick.Unlock()
	if fn != nil {
		go fn()
	}
}
