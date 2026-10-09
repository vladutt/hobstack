package store

import (
	"testing"
	"time"
)

func TestAccountUsage(t *testing.T) {
	s := newStore(t)
	if u, err := s.GetAccountUsage(0); err != nil || u != nil {
		t.Fatalf("fara date: %+v %v", u, err)
	}
	reset5 := time.Unix(1791556800, 0)
	reset7 := time.Unix(1791788400, 0)
	in := AccountUsage{AccountID: 0, Status: "allowed", FiveHourUsed: 0.05, FiveHourResets: reset5, SevenDayUsed: 0.46, SevenDayResets: reset7}
	if err := s.SaveAccountUsage(in); err != nil {
		t.Fatal(err)
	}
	// acelasi cont: randul se suprascrie
	in.FiveHourUsed, in.Status = 0.81, "allowed_warning"
	if err := s.SaveAccountUsage(in); err != nil {
		t.Fatal(err)
	}
	u, err := s.GetAccountUsage(0)
	if err != nil || u == nil || u.Status != "allowed_warning" || u.FiveHourUsed != 0.81 || u.SevenDayUsed != 0.46 ||
		!u.FiveHourResets.Equal(reset5) || !u.SevenDayResets.Equal(reset7) || u.UpdatedAt.IsZero() {
		t.Fatalf("%+v %v", u, err)
	}
	// alt cont: rand separat
	if err := s.SaveAccountUsage(AccountUsage{AccountID: 7, Status: "rejected", FiveHourUsed: 1}); err != nil {
		t.Fatal(err)
	}
	if u, _ := s.GetAccountUsage(7); u == nil || u.Status != "rejected" || !u.FiveHourResets.IsZero() {
		t.Fatalf("contul 7: %+v", u)
	}
	if u, _ := s.GetAccountUsage(0); u.Status != "allowed_warning" {
		t.Fatalf("Default atins: %+v", u)
	}
}
