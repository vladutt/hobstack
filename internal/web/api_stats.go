package web

import (
	"fmt"
	"net/http"
	"time"

	"hobstack/internal/store"
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

// statsPrevDays: cu cate zile calendaristice se muta fereastra pentru comparatie.
var statsPrevDays = map[string]int{"today": 1, "7d": 7, "30d": 30}

// statsPrev: perioada precedenta = aceeasi fereastra mutata inapoi cu N zile
// calendaristice, ca Today la 10:00 sa se compare cu ieri 00:00-10:00 (nu cu ieri seara).
// all nu are perioada precedenta.
func statsPrev(name string, from, now time.Time) (pfrom, pto time.Time, ok bool) {
	days, ok := statsPrevDays[name]
	if !ok {
		return time.Time{}, time.Time{}, false
	}
	return from.AddDate(0, 0, -days), now.AddDate(0, 0, -days), true
}

// getProjectStats: tab-ul Stats, cu totalurile perioadei precedente (lipsesc la all).
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
	if pfrom, pto, ok := statsPrev(name, from, now); ok {
		prev, err := s.st.ProjectStatsTotals(id, pfrom, pto)
		if err != nil {
			fail(w, err)
			return
		}
		stats.Prev = &prev
	}
	// limitele contului Claude al proiectului (5 ore / saptamanal), din ultimul run care le-a raportat
	account, accountID := "Default", int64(0)
	if a, err := s.st.ProjectAccount(id); err == nil && a != nil {
		account, accountID = a.Name, a.ID
	}
	usage, err := s.st.GetAccountUsage(accountID)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, statsView{Stats: stats, Account: account, Usage: usage})
}

// statsView: statisticile proiectului plus limitele contului lui Claude (usage nil = fara date inca).
type statsView struct {
	store.Stats
	Account string              `json:"account"`
	Usage   *store.AccountUsage `json:"usage"`
}
