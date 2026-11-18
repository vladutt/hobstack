//go:build !(darwin && cgo)

package main

import "sync"

// Fara fereastra proprie (Linux, Windows deocamdata, macOS fara cgo): HobStack se deschide in browser.

var windowHooks struct {
	sync.Mutex
	root   string
	reopen func()
}

// motivul gol: fara fereastra pe platforma asta, Settings nu arata deloc alegerea (Reason e pentru o eroare reala)
func windowAvailable() (bool, string) { return false, "" }

func showWindow(url string) {}

func windowOpen() bool { return false }
