//go:build linux

package main

import (
	_ "embed"
	"fmt"
	"os"
	"sync"
	"time"

	"fyne.io/systray"
)

// Iconita langa ceas pe Linux: StatusNotifierItem prin DBus (fyne.io/systray, Go pur). Merge in KDE, XFCE,
// Cinnamon, MATE si in GNOME cu extensia AppIndicator; fara watcher HobStack merge mai departe, fara iconita.
// Clic = deschide HobStack; meniul are Open HobStack / Quit HobStack. Fara dialog nativ: cu agenti care
// lucreaza, primul Quit schimba textul ("click again to quit"), al doilea (in 10 s) opreste.

//go:embed trayicon.png
var trayIcon []byte

type tray struct {
	end  func()
	once sync.Once
}

func startTray(open, quit func(), active func() int) *tray {
	if os.Getenv("DISPLAY") == "" && os.Getenv("WAYLAND_DISPLAY") == "" {
		return nil // server fara desktop
	}
	onReady := func() {
		systray.SetIcon(trayIcon)
		systray.SetTitle("HobStack")
		systray.SetTooltip("HobStack")
		systray.SetOnTapped(func() { go open() })
		mOpen := systray.AddMenuItem("Open HobStack", "")
		systray.AddSeparator()
		mQuit := systray.AddMenuItem("Quit HobStack", "")
		go func() {
			var armed time.Time // cand s-a cerut confirmarea
			for {
				select {
				case <-mOpen.ClickedCh:
					go open()
				case <-mQuit.ClickedCh:
					if n := active(); n > 0 && time.Since(armed) > 10*time.Second {
						armed = time.Now()
						mQuit.SetTitle(fmt.Sprintf("%d agent(s) working: click again to quit", n))
						go func() {
							time.Sleep(10 * time.Second)
							mQuit.SetTitle("Quit HobStack")
						}()
						continue
					}
					go quit()
				}
			}
		}()
	}
	start, end := systray.RunWithExternalLoop(onReady, func() {})
	start()
	return &tray{end: end}
}

// Close scoate iconita (la oprire si inainte de restart-ul dupa update).
func (t *tray) Close() {
	if t == nil {
		return
	}
	t.once.Do(func() {
		defer func() { recover() }() // fara DBus, systray n-are conexiune de inchis
		t.end()
	})
}

func menuBarAvailable() bool { return false }

func runMenuBar() {}

func stopMenuBar() {}
