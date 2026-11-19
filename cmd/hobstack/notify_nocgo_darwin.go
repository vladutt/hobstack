//go:build darwin && !cgo

package main

// Fara cgo (cross-compile) nu exista UNUserNotificationCenter: doar AppleScript, fara clic.
func sendNotification(title, body string, onClick func()) { osascriptNotify(title, body) }
