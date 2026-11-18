//go:build !(darwin && cgo)

package main

import "sync"

// Fara fereastra proprie (Linux, Windows deocamdata, macOS fara cgo): HobStack se deschide in browser.

var windowHooks struct {
	sync.Mutex
	root   string
	reopen func()
}

func windowAvailable() (bool, string) { return false, "not available on this system" }

func showWindow(url string) {}

func windowOpen() bool { return false }
