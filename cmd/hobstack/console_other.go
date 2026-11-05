//go:build !windows

package main

// ownConsole: doar pe Windows o fereastra de consola e creata pentru HobStack (dublu-clic, pornire la login).
func ownConsole() bool { return false }

func hideConsole() {}

func messageBox(string) {}
