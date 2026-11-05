//go:build !windows && !darwin

package autostart

func plistOrDesktop(home string) string { return desktopPath(home) }
