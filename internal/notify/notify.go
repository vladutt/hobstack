// Package notify decide cand HobStack trimite o notificare de sistem: cand un proiect care a lucrat s-a oprit
// si cand apare ceva nou in "Needs you". Pachetul tine doar logica (fara sistem): primeste instantanee ale
// proiectelor si intoarce notificarile de trimis; trimiterea nativa e in cmd/hobstack.
package notify

import (
	"fmt"
	"time"
)

// Quiet: cat trebuie sa stea un proiect oprit ca sa fie "terminat" (o pauza intre doi agenti nu conteaza).
const Quiet = 60 * time.Second

// Gather: elementele noi din Needs you aparute in acest interval se aduna intr-o singura notificare.
const Gather = 10 * time.Second

// Item e un element din Needs you: Key stabil ("q:12", "a:5"...), Line = randul scurt al cardului.
type Item struct{ Key, Line string }

// Snapshot e starea unui proiect la o observare.
type Snapshot struct {
	ProjectID int64
	Name      string
	Busy      bool // run activ sau task pe care un agent il poate lua acum
	TaskWork  bool // un run pe task e activ acum
	Done      int  // task-uri Done acum
	Needs     []Item
	Enabled   bool // comutatorul proiectului
}

// Note e o notificare de trimis.
type Note struct {
	ProjectID   int64
	Title, Body string
}

// Settings sunt comutatoarele globale (Settings > App).
type Settings struct{ Done, Needs bool }

// project e starea urmarita pentru un proiect.
type project struct {
	sawWork   bool      // a rulat macar un run pe task de la ultima notificare "finished"
	doneStart int       // task-uri Done la ultima notificare "finished" (sau la prima observare)
	idleSince time.Time // de cand e oprit; zero = lucreaza
	seen      map[string]bool
	pending   []Item    // elemente noi care asteapta sa fie adunate
	pendingAt time.Time // cand a aparut primul element din pending
	lastNeeds time.Time // ultima notificare "needs you" trimisa
}

// Watcher tine starea intre observari. Nu e sigur pentru folosire din mai multe goroutine.
type Watcher struct {
	state map[int64]*project
}

func NewWatcher() *Watcher {
	return &Watcher{state: map[int64]*project{}}
}

// Observe primeste instantaneele tuturor proiectelor si intoarce notificarile de trimis acum.
// viewing spune daca utilizatorul se uita chiar acum la proiect (atunci nu se trimite nimic,
// dar starea merge mai departe, ca notificarile vechi sa nu vina mai tarziu).
func (w *Watcher) Observe(now time.Time, snaps []Snapshot, set Settings, viewing func(projectID int64) bool) []Note {
	var notes []Note
	present := map[int64]bool{}
	for _, s := range snaps {
		present[s.ProjectID] = true
		st := w.state[s.ProjectID]
		if st == nil {
			// prima observare: ce exista deja e "vazut", nimic nu se notifica
			st = &project{seen: map[string]bool{}, doneStart: s.Done, sawWork: s.TaskWork}
			for _, it := range s.Needs {
				st.seen[it.Key] = true
			}
			if !s.Busy {
				st.idleSince = now
			}
			w.state[s.ProjectID] = st
			continue
		}
		quiet := !s.Enabled || (viewing != nil && viewing(s.ProjectID))
		if n, ok := st.needs(now, s, set.Needs && !quiet); ok {
			notes = append(notes, n)
		}
		if n, ok := st.finished(now, s, set.Done && !quiet); ok {
			notes = append(notes, n)
		}
	}
	// proiectele sterse ies din stare
	for pid := range w.state {
		if !present[pid] {
			delete(w.state, pid)
		}
	}
	return notes
}

// needs urmareste elementele noi din Needs you si le aduna Gather secunde.
func (st *project) needs(now time.Time, s Snapshot, send bool) (Note, bool) {
	cur := map[string]bool{}
	for _, it := range s.Needs {
		cur[it.Key] = true
		if !st.seen[it.Key] {
			st.seen[it.Key] = true
			if len(st.pending) == 0 {
				st.pendingAt = now
			}
			st.pending = append(st.pending, it)
		}
	}
	// un element inchis nu mai e "vazut" (daca revine e nou) si iese din pending
	for k := range st.seen {
		if !cur[k] {
			delete(st.seen, k)
		}
	}
	kept := st.pending[:0]
	for _, it := range st.pending {
		if cur[it.Key] {
			kept = append(kept, it)
		}
	}
	st.pending = kept
	if len(st.pending) == 0 || now.Sub(st.pendingAt) < Gather {
		return Note{}, false
	}
	items := st.pending
	st.pending = nil
	if !send {
		return Note{}, false
	}
	st.lastNeeds = now
	body := items[0].Line
	if len(items) > 1 {
		body = fmt.Sprintf("%d new items", len(items))
	}
	return Note{ProjectID: s.ProjectID, Title: s.Name + " needs you", Body: body}, true
}

// finished urmareste trecerea "lucreaza -> oprit" care tine Quiet.
func (st *project) finished(now time.Time, s Snapshot, send bool) (Note, bool) {
	if s.Busy {
		st.idleSince = time.Time{}
		if s.TaskWork {
			st.sawWork = true
		}
		return Note{}, false
	}
	if st.idleSince.IsZero() {
		st.idleSince = now
	}
	if !st.sawWork || now.Sub(st.idleSince) < Quiet {
		return Note{}, false
	}
	done := s.Done - st.doneStart
	st.sawWork = false
	st.doneStart = s.Done
	if !send {
		return Note{}, false
	}
	// "needs you" chiar acum pentru aceleasi elemente: nu mai trimitem si "waiting"
	if len(s.Needs) > 0 && !st.lastNeeds.IsZero() && now.Sub(st.lastNeeds) < Gather {
		return Note{}, false
	}
	title := s.Name + " finished"
	var body string
	if done > 0 {
		body = plural(done, "task done", "tasks done")
	}
	if len(s.Needs) > 0 {
		title = s.Name + " is waiting for you"
		if body != "" {
			body += " · "
		}
		body += plural(len(s.Needs), "needs you", "need you")
		// elementele inca neadunate sunt numarate aici; nu mai vine si "needs you" pentru ele
		st.pending = nil
	}
	if body == "" {
		body = "The team stopped"
	}
	return Note{ProjectID: s.ProjectID, Title: title, Body: body}, true
}

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}
