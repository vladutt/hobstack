//go:build darwin && cgo

package main

// extern void hsReplyTerminate(void);
import "C"

import (
	"log"
	"sync"
	"time"

	"hobstack/internal/appprefs"
)

// Chemate din fereastra (Objective-C, pe firul principal); munca merge in goroutine.

// starea ferestrei de scris in app.json; un singur timer: o mutare cu mouse-ul trimite multe evenimente
var windowSave struct {
	sync.Mutex
	frame appprefs.Frame
	zoom  float64
	timer *time.Timer
}

// goWindowState: fereastra s-a mutat / redimensionat / ascuns sau zoom-ul s-a schimbat. Scrie dupa 500 ms de liniste.
//
//export goWindowState
func goWindowState(x, y, w, h, zoom C.double) {
	windowHooks.Lock()
	root := windowHooks.root
	windowHooks.Unlock()
	if root == "" {
		return
	}
	windowSave.Lock()
	defer windowSave.Unlock()
	windowSave.frame = appprefs.Frame{X: float64(x), Y: float64(y), W: float64(w), H: float64(h)}
	windowSave.zoom = float64(zoom)
	if windowSave.timer != nil {
		windowSave.timer.Stop()
	}
	windowSave.timer = time.AfterFunc(500*time.Millisecond, func() {
		windowSave.Lock()
		frame, z := windowSave.frame, windowSave.zoom
		windowSave.Unlock()
		err := appprefs.Update(root, func(p *appprefs.Prefs) {
			p.Frame = &frame
			p.Zoom = z
		})
		if err != nil {
			log.Printf("app window: saving size: %v", err)
		}
	})
}

// goWindowReopen: clic pe iconita din Dock / HobStack.app deschis din nou cat ruleaza.
//
//export goWindowReopen
func goWindowReopen() {
	windowHooks.Lock()
	reopen := windowHooks.reopen
	windowHooks.Unlock()
	if reopen != nil {
		go reopen()
	}
}

// goAppTerminate: Quit din Dock / logout. Oprire curata, fara intrebare (macOS asteapta raspunsul,
// pe care il da hsStopApp cand start s-a terminat).
//
//export goAppTerminate
func goAppTerminate() {
	menuHooks.Lock()
	quit := menuHooks.quit
	menuHooks.Unlock()
	if quit == nil { // HobStack inca porneste: nimic de oprit curat
		C.hsReplyTerminate()
		return
	}
	go quit()
}
