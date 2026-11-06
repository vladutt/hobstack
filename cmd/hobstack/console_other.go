//go:build !windows

package main

// setupConsole: doar pe Windows HobStack porneste fara fereastra de consola (dublu-clic, pornire la login).
func setupConsole(bool) bool { return false }

func messageBox(string) {}
