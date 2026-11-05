//go:build !windows && !linux && !(darwin && cgo)

package main

// Fara iconita langa ceas (macOS construit fara cgo, BSD): HobStack se deschide din browser si se
// opreste din Settings.
type tray struct{}

func startTray(open, quit func(), active func() int) *tray { return nil }

func (*tray) Close() {}

func menuBarAvailable() bool { return false }

func runMenuBar() {}

func stopMenuBar() {}
