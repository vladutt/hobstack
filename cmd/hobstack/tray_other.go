//go:build !windows

package main

// Iconita langa ceas e doar pe Windows; pe macOS / Linux HobStack se deschide din browser si se opreste din Settings.
type tray struct{}

func startTray(open, quit func(), active func() int) *tray { return nil }

func (*tray) Close() {}
