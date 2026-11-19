//go:build darwin

package main

import (
	"log"
	"os/exec"
)

// osascriptNotify: notificarea prin AppleScript, pentru binarul pornit in afara HobStack.app (build local,
// tar.gz), unde UNUserNotificationCenter nu merge. Fara clic (o deschide Script Editor). Titlul si textul
// intra ca argumente (on run argv), deci nu au nevoie de escape.
func osascriptNotify(title, body string) {
	script := `on run argv
display notification (item 2 of argv) with title (item 1 of argv)
end run`
	if out, err := exec.Command("osascript", "-e", script, title, body).CombinedOutput(); err != nil {
		log.Printf("notification: %v %s", err, out)
	}
}
