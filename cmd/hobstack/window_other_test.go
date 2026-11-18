//go:build !(darwin && cgo)

package main

import "testing"

// fara fereastra proprie, setarea "Open HobStack in" nu apare: UI-ul o arata doar cu un motiv sau cu suport
func TestWindowOtherHidesSetting(t *testing.T) {
	ok, reason := windowAvailable()
	if ok || reason != "" {
		t.Fatalf("ok=%v reason=%q", ok, reason)
	}
}
