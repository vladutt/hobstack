//go:build !windows

package main

// ownConsole: doar pe Windows o fereastra pornita cu dublu-clic se inchide singura la iesire.
func ownConsole() bool { return false }
