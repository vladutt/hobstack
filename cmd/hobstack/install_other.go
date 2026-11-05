//go:build !linux

package main

import (
	"fmt"
	"os"
)

// install / uninstall: pe Windows e installerul (HobStack-Setup), pe macOS HobStack.app din .dmg.
func install([]string) int {
	fmt.Fprintln(os.Stderr, "On this system use the installer from https://github.com/vladutt/hobstack/releases/latest")
	return 1
}

func uninstall([]string) int { return install(nil) }
