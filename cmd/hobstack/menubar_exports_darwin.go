//go:build darwin && cgo

package main

// #include <stdlib.h>
import "C"

// Chemate din meniul barii (Objective-C, pe firul principal); munca grea merge in goroutine.

//export goMenuOpen
func goMenuOpen() {
	menuHooks.Lock()
	open := menuHooks.open
	menuHooks.Unlock()
	if open != nil {
		go open()
	}
}

//export goMenuQuit
func goMenuQuit() {
	menuHooks.Lock()
	quit, active := menuHooks.quit, menuHooks.active
	menuHooks.Unlock()
	if quit == nil {
		return
	}
	if active != nil {
		if n := active(); n > 0 && !confirmQuit(n) {
			return
		}
	}
	go quit()
}
