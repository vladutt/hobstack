//go:build darwin && cgo

package main

// extern void hsReplyTerminate(void);
import "C"

import (
	"log"

	"hobstack/internal/appprefs"
)

// Chemate din fereastra (Objective-C, pe firul principal); munca merge in goroutine.

// goWindowClosed: fereastra s-a ascuns; marimea, pozitia si zoom-ul raman pentru data viitoare.
//
//export goWindowClosed
func goWindowClosed(x, y, w, h, zoom C.double) {
	windowHooks.Lock()
	root := windowHooks.root
	windowHooks.Unlock()
	if root == "" {
		return
	}
	go func() {
		err := appprefs.Update(root, func(p *appprefs.Prefs) {
			p.Frame = &appprefs.Frame{X: float64(x), Y: float64(y), W: float64(w), H: float64(h)}
			p.Zoom = float64(zoom)
		})
		if err != nil {
			log.Printf("app window: saving size: %v", err)
		}
	}()
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
