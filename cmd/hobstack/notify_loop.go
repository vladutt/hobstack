package main

import (
	"context"
	"log"
	"strconv"
	"time"

	"hobstack/internal/notify"
	"hobstack/internal/web"
)

// notifyEvery: cat de des se uita bucla la proiecte (liniste 60 s si adunare 10 s: 5 s e destul de fin).
const notifyEvery = 5 * time.Second

// notifyURL: adresa proiectului in UI (clic pe notificare).
func notifyURL(base string, projectID int64) string {
	return base + "#/p/" + strconv.FormatInt(projectID, 10)
}

// notifyLoop urmareste proiectele si trimite notificarile de sistem (notify.Watcher decide ce si cand).
// open deschide UI-ul la adresa data (fereastra proprie sau browserul, dupa preferinta).
func notifyLoop(ctx context.Context, src *web.Server, base string, open func(url string)) {
	w := notify.NewWatcher()
	tick := time.NewTicker(notifyEvery)
	defer tick.Stop()
	failed := false
	for {
		snaps, err := src.NotifySnapshots()
		switch {
		case err != nil && !failed:
			log.Printf("notifications: %v", err) // o data, pana merge din nou
			failed = true
		case err == nil:
			failed = false
			for _, n := range w.Observe(time.Now(), snaps, src.NotifySettings(), src.Viewing) {
				u := notifyURL(base, n.ProjectID)
				log.Printf("notification: %s · %s", n.Title, n.Body)
				sendNotification(n.Title, n.Body, func() { open(u) })
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}
