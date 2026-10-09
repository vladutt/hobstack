package store

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestAccountsCRUD(t *testing.T) {
	s := newStore(t)
	a, err := s.AddAccount("Work", "sk-ant-oat01-secret-ABCD")
	if err != nil {
		t.Fatal(err)
	}
	if a.ID == 0 || a.Name != "Work" || a.TokenHint != "ABCD" || a.CheckedAt != nil || a.LastError != "" {
		t.Fatalf("cont nou: %+v", a)
	}
	raw, _ := json.Marshal(a)
	if strings.Contains(string(raw), "secret") {
		t.Fatalf("token-ul a ajuns in JSON: %s", raw)
	}
	if env := a.Env(); len(env) != 1 || env[0] != "CLAUDE_CODE_OAUTH_TOKEN=sk-ant-oat01-secret-ABCD" {
		t.Fatalf("Env = %v", env)
	}
	if _, err := s.AddAccount("work", "alt-token-1234"); !errors.Is(err, ErrConflict) {
		t.Fatalf("nume dublat (alt caz): %v", err)
	}

	if err := s.SetAccountCheck(a.ID, "OAuth token has expired"); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Account(a.ID); got.LastError != "OAuth token has expired" || got.CheckedAt != nil {
		t.Fatalf("dupa eroare: %+v", got)
	}
	if err := s.SetAccountCheck(a.ID, ""); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Account(a.ID); got.LastError != "" || got.CheckedAt == nil {
		t.Fatalf("dupa verificare reusita: %+v", got)
	}

	// token nou: verificarea veche nu mai spune nimic despre el
	if err := s.SetAccountCheck(a.ID, "expired"); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateAccount(a.ID, "Acme", "sk-ant-oat01-new-WXYZ"); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Account(a.ID)
	if got.Name != "Acme" || got.TokenHint != "WXYZ" || got.LastError != "" || got.CheckedAt != nil {
		t.Fatalf("dupa UpdateAccount: %+v", got)
	}
	// doar numele: token-ul si verificarea raman
	s.SetAccountCheck(a.ID, "")
	if err := s.UpdateAccount(a.ID, "Acme Team", ""); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Account(a.ID); got.TokenHint != "WXYZ" || got.CheckedAt == nil {
		t.Fatalf("redenumirea a atins token-ul: %+v", got)
	}
	if err := s.UpdateAccount(999, "x", ""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cont inexistent: %v", err)
	}

	list, err := s.Accounts()
	if err != nil || len(list) != 1 || list[0].ID != a.ID {
		t.Fatalf("Accounts = %+v, %v", list, err)
	}
}

func TestDeleteAccountInUse(t *testing.T) {
	s := newStore(t)
	p, _ := newProject(t, s)
	a, _ := s.AddAccount("Work", "tok-1234567890")
	if _, err := s.SetProjectAccount(p.ID, &a.ID, "switched"); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Account(a.ID); got.Projects != 1 {
		t.Fatalf("Projects = %d", got.Projects)
	}
	if err := s.DeleteAccount(a.ID); !errors.Is(err, ErrAccountInUse) {
		t.Fatalf("stergere cont folosit: %v", err)
	}
	if _, err := s.SetProjectAccount(p.ID, nil, "back"); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteAccount(a.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Account(a.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("contul trebuia sters: %v", err)
	}
}

func TestSetProjectAccountResetsSessions(t *testing.T) {
	s := newStore(t)
	p, lead := newProject(t, s)
	worker := newWorker(t, s, p.ID, "Rook")
	for _, ag := range []Agent{lead, worker} {
		if err := s.SetSession(ag.ID, "sess-old", "hash"); err != nil {
			t.Fatal(err)
		}
	}
	a, _ := s.AddAccount("Work", "tok-1234567890")

	changed, err := s.SetProjectAccount(p.ID, &a.ID, "Claude account changed to Work; starting a new session.")
	if err != nil || !changed {
		t.Fatalf("changed=%v err=%v", changed, err)
	}
	for _, ag := range []Agent{lead, worker} {
		got, _ := s.GetAgent(ag.ID)
		if got.SessionID != "" || got.PromptHash != "" {
			t.Fatalf("%s pastreaza sesiunea: %+v", ag.Name, got)
		}
		msgs, earlier, _ := s.ListChat(ag.ID)
		if len(msgs) != 1 || !strings.Contains(msgs[0].Text, "changed to Work") || earlier != 0 {
			t.Fatalf("chat %s: %+v (earlier %d)", ag.Name, msgs, earlier)
		}
	}
	got, err := s.GetProject(p.ID)
	if err != nil || got.AccountID == nil || *got.AccountID != a.ID {
		t.Fatalf("AccountID = %v, %v", got.AccountID, err)
	}
	acct, err := s.ProjectAccount(p.ID)
	if err != nil || acct == nil || acct.ID != a.ID {
		t.Fatalf("ProjectAccount = %+v, %v", acct, err)
	}
	if ids, _ := s.AccountProjects(a.ID); len(ids) != 1 || ids[0] != p.ID {
		t.Fatalf("AccountProjects = %v", ids)
	}

	// acelasi cont: nimic nu se schimba, nici sesiunile
	s.SetSession(lead.ID, "sess-new", "hash")
	if changed, err := s.SetProjectAccount(p.ID, &a.ID, "x"); err != nil || changed {
		t.Fatalf("acelasi cont: changed=%v err=%v", changed, err)
	}
	if got, _ := s.GetAgent(lead.ID); got.SessionID != "sess-new" {
		t.Fatal("acelasi cont a sters sesiunea")
	}

	// inapoi pe Default
	if changed, err := s.SetProjectAccount(p.ID, nil, "Claude account changed to Default; starting a new session."); err != nil || !changed {
		t.Fatalf("Default: changed=%v err=%v", changed, err)
	}
	if acct, err := s.ProjectAccount(p.ID); err != nil || acct != nil {
		t.Fatalf("Default: %+v, %v", acct, err)
	}
	if _, err := s.SetProjectAccount(999, nil, "x"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("proiect inexistent: %v", err)
	}
}

func TestCreateProjectWithAccount(t *testing.T) {
	s := newStore(t)
	a, _ := s.AddAccount("Work", "tok-1234567890")
	p := Project{Name: "Acme", Workdir: t.TempDir(), AccountID: &a.ID}
	if _, err := s.CreateProject(&p); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetProject(p.ID)
	if got.AccountID == nil || *got.AccountID != a.ID {
		t.Fatalf("CreateProject: %v", got.AccountID)
	}
	q := Project{Name: "Team", Workdir: t.TempDir(), AccountID: &a.ID}
	if _, err := s.CreateProjectWithTeam(&q, []NewMember{{Agent: Agent{Name: "Ana", Role: "Lead", Model: "sonnet"}, Parent: -1, Reviewer: -1}}); err != nil {
		t.Fatal(err)
	}
	got, _ = s.GetProject(q.ID)
	if got.AccountID == nil || *got.AccountID != a.ID {
		t.Fatalf("CreateProjectWithTeam: %v", got.AccountID)
	}
	// UpdateProject nu atinge contul (se schimba doar prin SetProjectAccount)
	got.Name = "Team 2"
	got.AccountID = nil
	if err := s.UpdateProject(got); err != nil {
		t.Fatal(err)
	}
	if again, _ := s.GetProject(q.ID); again.AccountID == nil {
		t.Fatal("UpdateProject a sters contul")
	}
}
