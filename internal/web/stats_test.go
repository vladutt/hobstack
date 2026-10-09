package web

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"hobstack/internal/store"
)

func TestStatsRange(t *testing.T) {
	zone := time.FixedZone("RO", 3*3600)
	now := time.Date(2026, 10, 8, 14, 30, 0, 0, zone)
	mid := time.Date(2026, 10, 8, 0, 0, 0, 0, zone)
	cases := []struct {
		name   string
		first  time.Time
		from   time.Time
		bucket string
	}{
		{"today", time.Time{}, mid, store.BucketHour},
		{"7d", time.Time{}, mid.AddDate(0, 0, -6), store.BucketDay},
		{"30d", time.Time{}, mid.AddDate(0, 0, -29), store.BucketDay},
		{"all", time.Time{}, mid, store.BucketDay},                                                                       // fara activitate: doar azi
		{"all", time.Date(2026, 9, 1, 21, 30, 0, 0, time.UTC), time.Date(2026, 9, 2, 0, 0, 0, 0, zone), store.BucketDay}, // 00:30 local pe 2
		{"all", mid.AddDate(0, 0, -120), mid.AddDate(0, 0, -120), store.BucketWeek},
	}
	for _, c := range cases {
		from, bucket, ok := statsRange(c.name, now, c.first)
		if !ok || !from.Equal(c.from) || bucket != c.bucket {
			t.Errorf("%s %v: %v %q %v, vrem %v %q", c.name, c.first, from, bucket, ok, c.from, c.bucket)
		}
	}
	if _, _, ok := statsRange("90d", now, time.Time{}); ok {
		t.Error("90d nu exista")
	}
}

func TestProjectStatsAPI(t *testing.T) {
	e := setup(t)
	p := e.createProject(t, t.TempDir())
	root := e.root(t, p.ID)
	task := store.Task{ProjectID: p.ID, Title: "Build", AssigneeID: root.ID}
	if err := e.st.CreateTask(&task); err != nil {
		t.Fatal(err)
	}

	code, raw := e.do(t, "GET", "/api/projects/"+itoa(p.ID)+"/stats", nil) // implicit 7d
	var st store.Stats
	if err := json.Unmarshal(raw, &st); err != nil || code != http.StatusOK {
		t.Fatalf("%d %s %v", code, raw, err)
	}
	if st.Bucket != store.BucketDay || len(st.Series) != 7 || st.Prev == nil || st.Totals.TasksCreated != 1 {
		t.Fatalf("%s", raw)
	}

	// all: fara comparatie; slice-urile goale vin ca [] nu null
	code, raw = e.do(t, "GET", "/api/projects/"+itoa(p.ID)+"/stats?range=all", nil)
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil || code != http.StatusOK {
		t.Fatalf("%d %s", code, raw)
	}
	if m["prev"] != nil || m["topFiles"] == nil || m["topTasks"] == nil {
		t.Fatalf("%s", raw)
	}

	if code, raw := e.do(t, "GET", "/api/projects/"+itoa(p.ID)+"/stats?range=90d", nil); code != http.StatusBadRequest {
		t.Fatalf("range necunoscut: %d %s", code, raw)
	}
	if code, _ := e.do(t, "GET", "/api/projects/99999/stats", nil); code != http.StatusNotFound {
		t.Fatalf("proiect inexistent: %d", code)
	}
}

// Today la 10:00 se compara cu ieri 00:00-10:00 (aceeasi parte a zilei), nu cu ieri 14:00-24:00.
func TestStatsPrev(t *testing.T) {
	zone := time.FixedZone("RO", 3*3600)
	now := time.Date(2026, 10, 8, 10, 0, 0, 0, zone)
	mid := time.Date(2026, 10, 8, 0, 0, 0, 0, zone)
	cases := []struct {
		name  string
		from  time.Time
		pfrom time.Time
		pto   time.Time
	}{
		{"today", mid, mid.AddDate(0, 0, -1), now.AddDate(0, 0, -1)},
		{"7d", mid.AddDate(0, 0, -6), mid.AddDate(0, 0, -13), now.AddDate(0, 0, -7)},
		{"30d", mid.AddDate(0, 0, -29), mid.AddDate(0, 0, -59), now.AddDate(0, 0, -30)},
	}
	for _, c := range cases {
		pfrom, pto, ok := statsPrev(c.name, c.from, now)
		if !ok || !pfrom.Equal(c.pfrom) || !pto.Equal(c.pto) {
			t.Errorf("%s: %v..%v %v, vrem %v..%v", c.name, pfrom, pto, ok, c.pfrom, c.pto)
		}
	}
	if _, _, ok := statsPrev("all", mid, now); ok {
		t.Error("all nu are perioada precedenta")
	}
}
