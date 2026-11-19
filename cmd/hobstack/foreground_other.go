//go:build !windows

package main

// allowForeground: doar Windows limiteaza cine aduce o fereastra in fata.
func allowForeground() {}
