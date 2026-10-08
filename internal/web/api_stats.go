package web

import (
	"fmt"
	"net/http"
	"time"

	"foreman/internal/store"
)

// statsWeekAfter: peste atatea zile, range-ul all se grupeaza pe saptamani.
const statsWeekAfter = 90 * 24 * time.Hour

func midnight(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}

// statsRange: inceputul perioadei si bucket-ul pentru ?range= (sfarsitul e mereu now).
// first = prima activitate a proiectului, folosita doar de all (zero = nimic inca).
func statsRange(name string, now, first time.Time) (from time.Time, bucket string, ok bool) {
	today := midnight(now)
	switch name {
	case "today":
		return today, store.BucketHour, true
	case "7d":
		return today.AddDate(0, 0, -6), store.BucketDay, true
	case "30d":
		return today.AddDate(0, 0, -29), store.BucketDay, true
	case "all":
		from = today
		if !first.IsZero() {
			from = midnight(first.In(now.Location()))
		}
		if now.Sub(from) > statsWeekAfter {
			return from, store.BucketWeek, true
		}
		return from, store.BucketDay, true
	}
	return time.Time{}, "", false
}

// getProjectStats: tab-ul Stats; perioada precedenta are aceeasi lungime (lipseste la all).
func (s *Server) getProjectStats(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		fail(w, err)
		return
	}
	if _, err := s.st.GetProject(id); err != nil {
		fail(w, err)
		return
	}
	name := r.URL.Query().Get("range")
	if name == "" {
		name = "7d"
	}
	now := time.Now()
	var first time.Time
	if name == "all" {
		if first, err = s.st.ProjectFirstActivity(id); err != nil {
			fail(w, err)
			return
		}
	}
	from, bucket, ok := statsRange(name, now, first)
	if !ok {
		writeErr(w, http.StatusBadRequest, fmt.Sprintf("unknown range %q", name))
		return
	}
	stats, err := s.st.ProjectStats(id, from, now, bucket)
	if err != nil {
		fail(w, err)
		return
	}
	if name != "all" {
		prev, err := s.st.ProjectStatsTotals(id, from.Add(-now.Sub(from)), from)
		if err != nil {
			fail(w, err)
			return
		}
		stats.Prev = &prev
	}
	writeJSON(w, http.StatusOK, stats)
}
