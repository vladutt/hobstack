package schedule

import (
	"testing"
	"time"
)

func bucharest(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("Europe/Bucharest")
	if err != nil {
		t.Skip("fara tzdata:", err)
	}
	return loc
}

func TestNext(t *testing.T) {
	loc := bucharest(t)
	at := func(s string) time.Time {
		v, err := time.ParseInLocation("2006-01-02 15:04", s, loc)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	weekdays := 0b0111110 // L-V
	cases := []struct {
		name  string
		spec  Spec
		after string
		want  string
	}{
		{"la 3 ore, aliniat la ora fixa", Spec{Kind: KindEveryHours, EveryHours: 3}, "2026-10-05 10:20", "2026-10-05 13:00"},
		{"la 3 ore peste miezul noptii", Spec{Kind: KindEveryHours, EveryHours: 3}, "2026-10-05 22:40", "2026-10-06 01:00"},
		{"la 24 de ore", Spec{Kind: KindEveryHours, EveryHours: 24}, "2026-10-05 09:00", "2026-10-06 09:00"},
		{"zilnic, ora n-a trecut", Spec{Kind: KindDaily, At: "09:00"}, "2026-10-05 08:59", "2026-10-05 09:00"},
		{"zilnic, fix la ora: strict dupa", Spec{Kind: KindDaily, At: "09:00"}, "2026-10-05 09:00", "2026-10-06 09:00"},
		{"zilnic, ora a trecut", Spec{Kind: KindDaily, At: "09:00"}, "2026-10-05 17:00", "2026-10-06 09:00"},
		{"L-V vineri seara -> luni", Spec{Kind: KindWeekly, At: "09:00", Weekdays: weekdays}, "2026-10-09 18:00", "2026-10-12 09:00"},
		{"doar duminica", Spec{Kind: KindWeekly, At: "20:30", Weekdays: 1}, "2026-10-05 10:00", "2026-10-11 20:30"},
		{"azi e permis si ora n-a trecut", Spec{Kind: KindWeekly, At: "23:00", Weekdays: 1 << time.Monday}, "2026-10-05 22:00", "2026-10-05 23:00"},
	}
	for _, c := range cases {
		got := c.spec.Next(at(c.after))
		if !got.Equal(at(c.want)) {
			t.Errorf("%s: %v, vreau %s", c.name, got, c.want)
		}
	}
}

// 29 martie 2026: in Romania ora 03:00 sare la 04:00; 03:30 nu exista
func TestNextDaylightSaving(t *testing.T) {
	loc := bucharest(t)
	after := time.Date(2026, 3, 29, 1, 0, 0, 0, loc)
	got := Spec{Kind: KindDaily, At: "03:30"}.Next(after)
	if !got.After(after) || got.Day() != 29 {
		t.Fatalf("%v: trebuia sa ruleze o data, pe 29, dupa %v", got, after)
	}
	if again := (Spec{Kind: KindDaily, At: "03:30"}).Next(got); again.Day() != 30 {
		t.Fatalf("urmatoarea dupa %v e %v, vreau 30", got, again)
	}
}

func TestValidate(t *testing.T) {
	ok := []Spec{
		{Kind: KindEveryHours, EveryHours: 1}, {Kind: KindEveryHours, EveryHours: 24},
		{Kind: KindDaily, At: "00:00"}, {Kind: KindDaily, At: "23:59"},
		{Kind: KindWeekly, At: "09:00", Weekdays: 1}, {Kind: KindWeekly, At: "09:00", Weekdays: 127},
	}
	for _, s := range ok {
		if err := s.Validate(); err != nil {
			t.Errorf("%+v: %v", s, err)
		}
	}
	bad := []Spec{
		{}, {Kind: "monthly"}, {Kind: KindEveryHours}, {Kind: KindEveryHours, EveryHours: 25},
		{Kind: KindDaily}, {Kind: KindDaily, At: "9:00"}, {Kind: KindDaily, At: "24:00"}, {Kind: KindDaily, At: "09:60"},
		{Kind: KindWeekly, At: "09:00"}, {Kind: KindWeekly, At: "09:00", Weekdays: 128},
	}
	for _, s := range bad {
		if err := s.Validate(); err == nil {
			t.Errorf("%+v: trebuia sa fie invalid", s)
		}
	}
}

func TestString(t *testing.T) {
	cases := map[string]Spec{
		"Every hour":             {Kind: KindEveryHours, EveryHours: 1},
		"Every 3 hours":          {Kind: KindEveryHours, EveryHours: 3},
		"Daily at 09:00":         {Kind: KindDaily, At: "09:00"},
		"Weekdays at 09:00":      {Kind: KindWeekly, At: "09:00", Weekdays: 0b0111110},
		"Weekends at 10:00":      {Kind: KindWeekly, At: "10:00", Weekdays: 0b1000001},
		"Every day at 08:00":     {Kind: KindWeekly, At: "08:00", Weekdays: 127},
		"Mon, Wed, Sun at 07:15": {Kind: KindWeekly, At: "07:15", Weekdays: 1<<time.Monday | 1<<time.Wednesday | 1<<time.Sunday},
	}
	for want, s := range cases {
		if got := s.String(); got != want {
			t.Errorf("%+v: %q, vreau %q", s, got, want)
		}
	}
}
