// Package schedule calculeaza programul sarcinilor recurente: validare,
// urmatoarea rulare si textul afisat. Functii pure, fara baza de date.
package schedule

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	KindEveryHours = "every_hours"
	KindDaily      = "daily"
	KindWeekly     = "weekly"
)

// Spec e programul unei sarcini recurente.
// Weekdays e o masca: bitul 1<<time.Weekday (duminica = bitul 0).
type Spec struct {
	Kind       string `json:"kind"`
	EveryHours int    `json:"everyHours"`
	At         string `json:"at"` // "HH:MM", 24h, ora locala
	Weekdays   int    `json:"weekdays"`
}

const (
	maskWeekdays = 0b0111110 // luni-vineri
	maskWeekends = 0b1000001 // sambata + duminica
	maskAll      = 0b1111111
)

func (s Spec) Validate() error {
	switch s.Kind {
	case KindEveryHours:
		if s.EveryHours < 1 || s.EveryHours > 24 {
			return errors.New("repeat every 1 to 24 hours")
		}
		return nil
	case KindDaily, KindWeekly:
		if _, _, ok := parseAt(s.At); !ok {
			return errors.New("time must be HH:MM")
		}
		if s.Kind == KindWeekly && (s.Weekdays < 1 || s.Weekdays > maskAll) {
			return errors.New("choose at least one day")
		}
		return nil
	}
	return fmt.Errorf("unknown schedule %q", s.Kind)
}

// parseAt citeste "HH:MM" strict (doua cifre fiecare).
func parseAt(at string) (h, m int, ok bool) {
	if len(at) != 5 {
		return 0, 0, false
	}
	v, err := time.Parse("15:04", at)
	if err != nil {
		return 0, 0, false
	}
	return v.Hour(), v.Minute(), true
}

// Next intoarce primul moment strict dupa `after`, in fusul orar al lui `after`
// (apelantii dau ora locala). Spec-ul trebuie sa fie valid.
func (s Spec) Next(after time.Time) time.Time {
	loc := after.Location()
	if s.Kind == KindEveryHours {
		n := after.Add(time.Duration(s.EveryHours) * time.Hour)
		return time.Date(n.Year(), n.Month(), n.Day(), n.Hour(), 0, 0, 0, loc) // aliniat la ora fixa
	}
	h, m, _ := parseAt(s.At)
	y, mo, d := after.Date()
	// cel mult 8 zile: azi + o saptamana intreaga
	for i := 0; i < 8; i++ {
		// time.Date normalizeaza o ora inexistenta (trecerea la ora de vara)
		c := time.Date(y, mo, d+i, h, m, 0, 0, loc)
		if c.After(after) && (s.Kind == KindDaily || s.Weekdays&(1<<c.Weekday()) != 0) {
			return c
		}
	}
	return after.Add(24 * time.Hour) // nu se ajunge aici cu un Spec valid
}

func (s Spec) String() string {
	switch s.Kind {
	case KindEveryHours:
		if s.EveryHours == 1 {
			return "Every hour"
		}
		return fmt.Sprintf("Every %d hours", s.EveryHours)
	case KindDaily:
		return "Daily at " + s.At
	case KindWeekly:
		return days(s.Weekdays) + " at " + s.At
	}
	return s.Kind
}

// days: "Weekdays", "Weekends", "Every day" sau zilele in ordinea Mon..Sun.
func days(mask int) string {
	switch mask {
	case maskAll:
		return "Every day"
	case maskWeekdays:
		return "Weekdays"
	case maskWeekends:
		return "Weekends"
	}
	var names []string
	for _, d := range []time.Weekday{time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday, time.Saturday, time.Sunday} {
		if mask&(1<<d) != 0 {
			names = append(names, d.String()[:3])
		}
	}
	return strings.Join(names, ", ")
}
