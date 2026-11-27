package store

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestNormalizeMemoryKey(t *testing.T) {
	if k, err := NormalizeMemoryKey(" API Auth "); err != nil || k != "api-auth" {
		t.Fatalf("got %q, %v", k, err)
	}
	for _, bad := range []string{"", "-x", strings.Repeat("a", 61), "a/b"} {
		if _, err := NormalizeMemoryKey(bad); err == nil {
			t.Errorf("%q: expected an error", bad)
		}
	}
}

func agentWrite(a Agent, key, summary, body string) MemoryWrite {
	return MemoryWrite{Key: key, Summary: summary, Body: body, AuthorAgentID: a.ID, AuthorName: a.Name}
}

func TestSetMemoryUpsertAndVersions(t *testing.T) {
	s := newStore(t)
	p, lead := newProject(t, s)
	m1, prev, changed, err := s.SetMemory(p.ID, agentWrite(lead, "ui.dates", "DD.MM.YYYY", ""), false)
	if err != nil || prev != nil || !changed {
		t.Fatalf("first: %v %v %v", prev, changed, err)
	}
	m2, prev, changed, err := s.SetMemory(p.ID, agentWrite(lead, "ui.dates", "DD.MM.YYYY everywhere", "body"), false)
	if err != nil || !changed || prev == nil || prev.Summary != "DD.MM.YYYY" || m2.ID != m1.ID {
		t.Fatalf("second: %+v %v %v", prev, changed, err)
	}
	_, _, changed, err = s.SetMemory(p.ID, agentWrite(lead, "ui.dates", "DD.MM.YYYY everywhere", "body"), false)
	if err != nil || changed {
		t.Fatalf("identical write: changed=%v err=%v", changed, err)
	}
	h, err := s.MemoryHistory(m1.ID)
	if err != nil || len(h) != 2 || h[0].Action != "set" || h[0].Summary != "DD.MM.YYYY everywhere" {
		t.Fatalf("history: %+v %v", h, err)
	}
}

func TestSetMemoryLocked(t *testing.T) {
	s := newStore(t)
	p, lead := newProject(t, s)
	m, _, _, _ := s.SetMemory(p.ID, agentWrite(lead, "ui.dates", "a", ""), false)
	if err := s.LockMemory(m.ID, true); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := s.SetMemory(p.ID, agentWrite(lead, "ui.dates", "b", ""), false); !errors.Is(err, ErrMemoryLocked) {
		t.Fatalf("set: %v", err)
	}
	if err := s.ForgetMemory(m.ID, agentWrite(lead, "", "", ""), "x", false); !errors.Is(err, ErrMemoryLocked) {
		t.Fatalf("forget: %v", err)
	}
	if _, _, _, err := s.SetMemory(p.ID, MemoryWrite{Key: "ui.dates", Summary: "b", AuthorName: UserFileAuthor}, true); err != nil {
		t.Fatalf("user set: %v", err)
	}
}

func TestForgetClearsLockAndRevive(t *testing.T) {
	s := newStore(t)
	p, lead := newProject(t, s)
	m, _, _, _ := s.SetMemory(p.ID, agentWrite(lead, "ui.dates", "a", ""), false)
	s.LockMemory(m.ID, true)
	user := MemoryWrite{AuthorName: UserFileAuthor}
	if err := s.ForgetMemory(m.ID, user, "old", true); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetMemory(m.ID)
	if got.DeletedAt == nil {
		t.Fatalf("after forget: %+v", got)
	}
	if active, _ := s.ListMemory(p.ID, false); len(active) != 0 {
		t.Fatalf("active: %+v", active)
	}
	again, _, _, err := s.SetMemory(p.ID, agentWrite(lead, "ui.dates", "b", ""), false)
	if err != nil || again.ID != m.ID || again.DeletedAt != nil || again.Locked {
		t.Fatalf("revive: %+v %v", again, err)
	}
	h, _ := s.MemoryHistory(m.ID)
	var acts []string
	for _, v := range h {
		acts = append(acts, v.Action)
	}
	if strings.Join(acts, ",") != "set,forget,lock,set" || h[1].Reason != "old" {
		t.Fatalf("history: %v %+v", acts, h)
	}
}

func TestRestoreKeepsLock(t *testing.T) {
	s := newStore(t)
	p, lead := newProject(t, s)
	m, _, _, _ := s.SetMemory(p.ID, agentWrite(lead, "ui.dates", "a", ""), false)
	s.LockMemory(m.ID, true)
	user := MemoryWrite{AuthorName: UserFileAuthor}
	s.ForgetMemory(m.ID, user, "", true)
	if err := s.RestoreMemory(m.ID, user); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.GetMemory(m.ID); got.DeletedAt != nil || !got.Locked {
		t.Fatalf("after restore: %+v", got)
	}
}

func TestMemoryLimit(t *testing.T) {
	s := newStore(t)
	p, lead := newProject(t, s)
	for i := 0; i < MaxMemoryEntries; i++ {
		if _, _, _, err := s.SetMemory(p.ID, agentWrite(lead, fmt.Sprintf("k%d", i), "s", ""), false); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, _, err := s.SetMemory(p.ID, agentWrite(lead, "extra", "s", ""), false); !errors.Is(err, ErrMemoryFull) {
		t.Fatalf("201st: %v", err)
	}
	if _, _, _, err := s.SetMemory(p.ID, agentWrite(lead, "k3", "changed", ""), false); err != nil {
		t.Fatalf("update at the limit: %v", err)
	}
}

func TestCreateMemoryExists(t *testing.T) {
	s := newStore(t)
	p, _ := newProject(t, s)
	user := MemoryWrite{Key: "ui.dates", Summary: "a", AuthorName: UserFileAuthor}
	m, err := s.CreateMemory(p.ID, user, true)
	if err != nil || !m.Locked {
		t.Fatalf("create: %+v %v", m, err)
	}
	if _, err := s.CreateMemory(p.ID, user, false); !errors.Is(err, ErrMemoryExists) {
		t.Fatalf("duplicate: %v", err)
	}
	s.ForgetMemory(m.ID, user, "", true)
	again, err := s.CreateMemory(p.ID, user, false)
	if err != nil || again.ID != m.ID || again.DeletedAt != nil {
		t.Fatalf("revive: %+v %v", again, err)
	}
}

func TestListMemoryTaskRef(t *testing.T) {
	s := newStore(t)
	p, lead := newProject(t, s)
	task := Task{ProjectID: p.ID, Title: "A", AssigneeID: lead.ID}
	if err := s.CreateTask(&task); err != nil {
		t.Fatal(err)
	}
	w := agentWrite(lead, "ui.dates", "a", "")
	w.TaskID = task.ID
	s.SetMemory(p.ID, w, false)
	list, err := s.ListMemory(p.ID, false)
	if err != nil || len(list) != 1 || list[0].TaskRef != task.Ref() {
		t.Fatalf("%+v %v", list, err)
	}
}

func TestSearchMemory(t *testing.T) {
	s := newStore(t)
	p, lead := newProject(t, s)
	s.SetMemory(p.ID, agentWrite(lead, "ui.dates", "Show the date", ""), false)
	s.SetMemory(p.ID, agentWrite(lead, "api.auth", "tokens", "never store the Dâte in the DB"), false)
	gone, _, _, _ := s.SetMemory(p.ID, agentWrite(lead, "old.date", "date", ""), false)
	s.ForgetMemory(gone.ID, agentWrite(lead, "", "", ""), "", false)
	res, err := s.SearchMemory(p.ID, "Daté", 10)
	if err != nil || len(res) != 2 {
		t.Fatalf("%+v %v", res, err)
	}
	if res, _ := s.SearchMemory(p.ID, "date", 1); len(res) != 1 {
		t.Fatalf("limit: %+v", res)
	}
}
