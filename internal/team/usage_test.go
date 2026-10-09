package team

import (
	"testing"
	"time"

	"foreman/internal/runner"
	"foreman/internal/store"
)

// limitele raportate de un run se salveaza pe contul proiectului (aici Default = 0)
func TestRunSavesAccountUsage(t *testing.T) {
	f := setup(t)
	f.task(t, "A", f.rook, store.StatusQueued)
	mustReconcile(t, f)
	reset := time.Unix(1791556800, 0)
	f.fs.finish(f.fs.last(t, f.rook).ID, runner.Outcome{Status: store.RunDone, RateLimit: &runner.RateLimit{
		Status: "allowed_warning", FiveHourUsed: 0.82, FiveHourResets: reset, SevenDayUsed: 0.4,
	}})
	u, err := f.st.GetAccountUsage(0)
	if err != nil || u == nil || u.Status != "allowed_warning" || u.FiveHourUsed != 0.82 || !u.FiveHourResets.Equal(reset) {
		t.Fatalf("%+v %v", u, err)
	}
}

// un run fara rate_limit_event nu sterge limitele stiute
func TestRunWithoutRateLimitKeepsUsage(t *testing.T) {
	f := setup(t)
	if err := f.st.SaveAccountUsage(store.AccountUsage{AccountID: 0, Status: "allowed", FiveHourUsed: 0.3}); err != nil {
		t.Fatal(err)
	}
	f.task(t, "A", f.rook, store.StatusQueued)
	mustReconcile(t, f)
	f.fs.finish(f.fs.last(t, f.rook).ID, runner.Outcome{Status: store.RunDone})
	if u, _ := f.st.GetAccountUsage(0); u == nil || u.FiveHourUsed != 0.3 {
		t.Fatalf("%+v", u)
	}
}
