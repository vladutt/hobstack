package notify

import (
	"testing"
	"time"
)

var t0 = time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)

func on() Settings { return Settings{Done: true, Needs: true} }

func snap(busy, work bool, done int, needs ...Item) Snapshot {
	return Snapshot{ProjectID: 1, Name: "Meridian", Busy: busy, TaskWork: work, Done: done, Needs: needs, Enabled: true}
}

func q(key string) Item { return Item{Key: key, Line: "Atlas asks: " + key} }

func noView(int64) bool { return false }

// at: observarea la t0+sec
func at(w *Watcher, sec int, s Snapshot) []Note {
	return w.Observe(t0.Add(time.Duration(sec)*time.Second), []Snapshot{s}, on(), noView)
}

func want(t *testing.T, got []Note, title, body string) {
	t.Helper()
	if len(got) != 1 || got[0].Title != title || got[0].Body != body || got[0].ProjectID != 1 {
		t.Fatalf("vrem %q / %q, avem %+v", title, body, got)
	}
}

func none(t *testing.T, got []Note) {
	t.Helper()
	if len(got) != 0 {
		t.Fatalf("nicio notificare, avem %+v", got)
	}
}

func TestNoNotesAtStartup(t *testing.T) {
	w := NewWatcher()
	none(t, at(w, 0, snap(false, false, 4, q("q:1"))))
	none(t, at(w, 120, snap(false, false, 4, q("q:1"))))
}

func TestFinishedAfterQuiet(t *testing.T) {
	w := NewWatcher()
	none(t, at(w, 0, snap(true, true, 0)))
	none(t, at(w, 5, snap(false, false, 3)))
	none(t, at(w, 60, snap(false, false, 3)))
	want(t, at(w, 66, snap(false, false, 3)), "Meridian finished", "3 tasks done")
	none(t, at(w, 200, snap(false, false, 3)))
}

func TestShortPauseIsNotFinished(t *testing.T) {
	w := NewWatcher()
	none(t, at(w, 0, snap(true, true, 0)))
	none(t, at(w, 5, snap(false, false, 1)))
	none(t, at(w, 35, snap(false, false, 1)))
	none(t, at(w, 40, snap(true, true, 1)))
	none(t, at(w, 45, snap(false, false, 2)))
	none(t, at(w, 100, snap(false, false, 2)))
	want(t, at(w, 106, snap(false, false, 2)), "Meridian finished", "2 tasks done")
}

func TestChatOnlyIsNotFinished(t *testing.T) {
	w := NewWatcher()
	none(t, at(w, 0, snap(true, false, 2)))
	none(t, at(w, 5, snap(false, false, 2)))
	none(t, at(w, 120, snap(false, false, 2)))
}

func TestWorkWithoutDoneTasks(t *testing.T) {
	w := NewWatcher()
	none(t, at(w, 0, snap(true, true, 2)))
	none(t, at(w, 5, snap(false, false, 2)))
	want(t, at(w, 70, snap(false, false, 2)), "Meridian finished", "The team stopped")
}

func TestWaitingForYou(t *testing.T) {
	w := NewWatcher()
	none(t, at(w, 0, snap(true, true, 0, q("q:1"))))
	none(t, at(w, 5, snap(false, false, 2, q("q:1"))))
	want(t, at(w, 70, snap(false, false, 2, q("q:1"))), "Meridian is waiting for you", "2 tasks done · 1 needs you")

	w = NewWatcher()
	none(t, at(w, 0, snap(true, true, 0, q("q:1"), q("q:2"))))
	none(t, at(w, 5, snap(false, false, 0, q("q:1"), q("q:2"))))
	want(t, at(w, 70, snap(false, false, 0, q("q:1"), q("q:2"))), "Meridian is waiting for you", "2 need you")
}

func TestOneTaskSingular(t *testing.T) {
	w := NewWatcher()
	none(t, at(w, 0, snap(true, true, 0)))
	none(t, at(w, 5, snap(false, false, 1)))
	want(t, at(w, 70, snap(false, false, 1)), "Meridian finished", "1 task done")
}

func TestNeedsGathered(t *testing.T) {
	w := NewWatcher()
	none(t, at(w, 0, snap(true, true, 0)))
	none(t, at(w, 5, snap(true, true, 0, q("q:2"))))
	none(t, at(w, 9, snap(true, true, 0, q("q:2"), q("a:7"))))
	none(t, at(w, 14, snap(true, true, 0, q("q:2"), q("a:7"))))
	want(t, at(w, 15, snap(true, true, 0, q("q:2"), q("a:7"))), "Meridian needs you", "2 new items")

	w = NewWatcher()
	none(t, at(w, 0, snap(true, true, 0)))
	none(t, at(w, 5, snap(true, true, 0, q("q:3"))))
	want(t, at(w, 15, snap(true, true, 0, q("q:3"))), "Meridian needs you", "Atlas asks: q:3")
}

func TestNeedsSameKeyOnce(t *testing.T) {
	w := NewWatcher()
	none(t, at(w, 0, snap(true, true, 0)))
	none(t, at(w, 5, snap(true, true, 0, q("q:3"))))
	want(t, at(w, 15, snap(true, true, 0, q("q:3"))), "Meridian needs you", "Atlas asks: q:3")
	none(t, at(w, 20, snap(true, true, 0, q("q:3"))))
	none(t, at(w, 40, snap(true, true, 0, q("q:3"))))
	// inchis, apoi deschis din nou cu aceeasi cheie: e un element nou
	none(t, at(w, 45, snap(true, true, 0)))
	none(t, at(w, 50, snap(true, true, 0, q("q:3"))))
	want(t, at(w, 60, snap(true, true, 0, q("q:3"))), "Meridian needs you", "Atlas asks: q:3")
}

func TestWaitingSuppressedRightAfterNeeds(t *testing.T) {
	w := NewWatcher()
	none(t, at(w, 0, snap(true, true, 0)))
	none(t, at(w, 2, snap(false, false, 1, q("q:9"))))
	none(t, at(w, 10, snap(false, false, 1, q("q:9"))))
	// needs la 12 s; "waiting" ar veni la 62 s: e la 50 s dupa needs, deci vine
	want(t, at(w, 12, snap(false, false, 1, q("q:9"))), "Meridian needs you", "Atlas asks: q:9")
	want(t, at(w, 62, snap(false, false, 1, q("q:9"))), "Meridian is waiting for you", "1 task done · 1 needs you")

	// needs si waiting in acelasi moment: ramane doar needs
	w = NewWatcher()
	none(t, at(w, 0, snap(true, true, 0)))
	none(t, at(w, 5, snap(false, false, 1)))
	none(t, at(w, 58, snap(false, false, 1, q("q:9"))))
	got := at(w, 68, snap(false, false, 1, q("q:9")))
	want(t, got, "Meridian needs you", "Atlas asks: q:9")
	none(t, at(w, 70, snap(false, false, 1, q("q:9"))))
}

func TestDisabledAndViewing(t *testing.T) {
	// proiect oprit: starea merge mai departe, notificarile nu vin nici dupa ce se reporneste
	w := NewWatcher()
	off := snap(true, true, 0)
	off.Enabled = false
	none(t, at(w, 0, off))
	off = snap(false, false, 1, q("q:1"))
	off.Enabled = false
	none(t, at(w, 5, off))
	none(t, at(w, 80, off))
	none(t, at(w, 90, snap(false, false, 1, q("q:1"))))

	// cineva se uita la proiect
	w = NewWatcher()
	view := func(pid int64) bool { return pid == 1 }
	none(t, w.Observe(t0, []Snapshot{snap(true, true, 0)}, on(), view))
	none(t, w.Observe(t0.Add(5*time.Second), []Snapshot{snap(false, false, 1, q("q:1"))}, on(), view))
	none(t, w.Observe(t0.Add(80*time.Second), []Snapshot{snap(false, false, 1, q("q:1"))}, on(), view))

	// setarile globale: Done oprit = doar needs; Needs oprit = doar finished
	w = NewWatcher()
	s := Settings{Done: false, Needs: true}
	w.Observe(t0, []Snapshot{snap(true, true, 0)}, s, noView)
	w.Observe(t0.Add(5*time.Second), []Snapshot{snap(false, false, 1, q("q:1"))}, s, noView)
	got := w.Observe(t0.Add(80*time.Second), []Snapshot{snap(false, false, 1, q("q:1"))}, s, noView)
	want(t, got, "Meridian needs you", "Atlas asks: q:1")

	w = NewWatcher()
	s = Settings{Done: true, Needs: false}
	w.Observe(t0, []Snapshot{snap(true, true, 0)}, s, noView)
	w.Observe(t0.Add(5*time.Second), []Snapshot{snap(false, false, 1, q("q:1"))}, s, noView)
	got = w.Observe(t0.Add(80*time.Second), []Snapshot{snap(false, false, 1, q("q:1"))}, s, noView)
	want(t, got, "Meridian is waiting for you", "1 task done · 1 needs you")
}

// un proiect care dispare (sters) iese din stare; altul nu e atins
func TestMultipleProjects(t *testing.T) {
	w := NewWatcher()
	a := snap(true, true, 0)
	b := snap(false, false, 0)
	b.ProjectID, b.Name = 2, "Probe"
	w.Observe(t0, []Snapshot{a, b}, on(), noView)
	a = snap(false, false, 1)
	w.Observe(t0.Add(5*time.Second), []Snapshot{a, b}, on(), noView)
	got := w.Observe(t0.Add(70*time.Second), []Snapshot{a, b}, on(), noView)
	want(t, got, "Meridian finished", "1 task done")
	if len(w.state) != 2 {
		t.Fatalf("stare: %d", len(w.state))
	}
	w.Observe(t0.Add(80*time.Second), []Snapshot{a}, on(), noView)
	if len(w.state) != 1 {
		t.Fatalf("proiectul sters ramane in stare")
	}
}
