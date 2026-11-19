//go:build linux

package main

import (
	"context"
	"log"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// Notificarile pe Linux prin notify-send (libnotify). Cu --action si --wait (libnotify 0.7.9+) clic-ul pe
// notificare deschide proiectul; versiunile vechi nu stiu optiunile, atunci trimitem fara clic. Fara notify-send
// sau fara desktop nu se intampla nimic (un singur rand in log).

var notifySend struct {
	sync.Mutex
	path     string
	checked  bool
	noAction bool // notify-send vechi: fara --action
}

func sendNotification(title, body string, onClick func()) {
	if os.Getenv("DISPLAY") == "" && os.Getenv("WAYLAND_DISPLAY") == "" {
		return
	}
	notifySend.Lock()
	if !notifySend.checked {
		notifySend.checked = true
		if p, err := exec.LookPath("notify-send"); err == nil {
			notifySend.path = p
		} else {
			log.Printf("notifications: notify-send not found (install libnotify-bin / libnotify)")
		}
	}
	path, noAction := notifySend.path, notifySend.noAction
	notifySend.Unlock()
	if path == "" {
		return
	}
	go func() {
		if !noAction {
			// --wait tine procesul pana se inchide notificarea; o limita, ca sa nu ramana agatat
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
			defer cancel()
			out, err := exec.CommandContext(ctx, path, "--app-name=HobStack", "--action=open=Open", "--wait", "--", title, body).CombinedOutput()
			if err == nil {
				if strings.TrimSpace(string(out)) == "open" && onClick != nil {
					onClick()
				}
				return
			}
			if !strings.Contains(strings.ToLower(string(out)), "unknown option") {
				return // inchisa / expirata / anulata: nimic de facut
			}
			notifySend.Lock()
			notifySend.noAction = true
			notifySend.Unlock()
		}
		if out, err := exec.Command(path, "--app-name=HobStack", "--", title, body).CombinedOutput(); err != nil {
			log.Printf("notification: %v %s", err, out)
		}
	}()
}
