package web

import (
	"encoding/json"
	"testing"
	"time"

	"foreman/internal/store"
)

func TestStatsIncludesAccountUsage(t *testing.T) {
	e := setup(t)
	p := e.createProject(t, t.TempDir())
	var res struct {
		Account string             `json:"account"`
		Usage   *store.AccountUsage `json:"usage"`
		Totals  json.RawMessage    `json:"totals"`
	}
	code, raw := e.do(t, "GET", "/api/projects/"+itoa(p.ID)+"/stats", nil)
	json.Unmarshal(raw, &res)
	if code != 200 || res.Usage != nil || res.Account != "Default" || res.Totals == nil {
		t.Fatalf("fara date: %d %s", code, raw)
	}
	reset := time.Unix(1791556800, 0)
	if err := e.st.SaveAccountUsage(store.AccountUsage{AccountID: 0, Status: "allowed", FiveHourUsed: 0.05, FiveHourResets: reset, SevenDayUsed: 0.46}); err != nil {
		t.Fatal(err)
	}
	code, raw = e.do(t, "GET", "/api/projects/"+itoa(p.ID)+"/stats", nil)
	res.Usage = nil
	json.Unmarshal(raw, &res)
	if code != 200 || res.Usage == nil || res.Usage.FiveHourUsed != 0.05 || !res.Usage.FiveHourResets.Equal(reset) || res.Usage.SevenDayUsed != 0.46 {
		t.Fatalf("cu date: %d %s", code, raw)
	}
}
