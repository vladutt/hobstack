package web

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func (e *env) addAccount(t *testing.T, name, token string) int64 {
	t.Helper()
	code, raw := e.do(t, "POST", "/api/accounts", map[string]any{"name": name, "token": token})
	if code != http.StatusCreated {
		t.Fatalf("POST /api/accounts: %d %s", code, raw)
	}
	var a struct{ ID int64 }
	json.Unmarshal(raw, &a)
	return a.ID
}

func TestAccountsAPIHidesToken(t *testing.T) {
	e := setup(t) // runner cu binar inexistent: verificarea pica, contul se salveaza oricum
	const token = "sk-ant-oat01-SECRETPART-ABCD"
	code, raw := e.do(t, "POST", "/api/accounts", map[string]any{"name": "Work", "token": token})
	if code != http.StatusCreated || strings.Contains(string(raw), "SECRETPART") {
		t.Fatalf("POST: %d %s", code, raw)
	}
	var a struct {
		ID        int64
		TokenHint string
		LastError string
	}
	json.Unmarshal(raw, &a)
	if a.TokenHint != "ABCD" || a.LastError == "" {
		t.Fatalf("cont: %+v", a)
	}
	for _, path := range []string{"/api/accounts", "/api/accounts/" + itoa(a.ID) + "/check"} {
		method := "GET"
		if strings.HasSuffix(path, "/check") {
			method = "POST"
		}
		if code, raw := e.do(t, method, path, nil); code != 200 || strings.Contains(string(raw), "SECRETPART") {
			t.Fatalf("%s %s: %d %s", method, path, code, raw)
		}
	}
	if code, raw := e.do(t, "PUT", "/api/accounts/"+itoa(a.ID), map[string]any{"name": "Acme"}); code != 200 || !strings.Contains(string(raw), `"Acme"`) {
		t.Fatalf("PUT nume: %d %s", code, raw)
	}
	if code, _ := e.do(t, "POST", "/api/accounts", map[string]any{"name": "acme", "token": token}); code != http.StatusConflict {
		t.Fatalf("nume dublat: %d", code)
	}
	for _, bad := range []map[string]any{
		{"name": "", "token": token},
		{"name": "X", "token": "short"},
		{"name": "X", "token": "sk-ant oat01 with spaces"},
		{"name": strings.Repeat("n", 41), "token": token},
	} {
		if code, _ := e.do(t, "POST", "/api/accounts", bad); code != http.StatusBadRequest {
			t.Fatalf("%v: %d", bad, code)
		}
	}
	if code, _ := e.do(t, "PUT", "/api/accounts/999", map[string]any{"name": "Y"}); code != http.StatusNotFound {
		t.Fatalf("PUT inexistent: %d", code)
	}
}

func TestProjectAccountAPI(t *testing.T) {
	e := setup(t)
	acct := e.addAccount(t, "Work", "sk-ant-oat01-token-1234")
	code, raw := e.do(t, "POST", "/api/projects", map[string]any{"name": "Acme", "workdir": t.TempDir(), "accountId": acct})
	var p struct {
		ID        int64
		AccountID *int64
	}
	json.Unmarshal(raw, &p)
	if code != http.StatusCreated || p.AccountID == nil || *p.AccountID != acct {
		t.Fatalf("POST proiect: %d %s", code, raw)
	}
	var card struct{ AccountName, AccountError string }
	_, raw = e.do(t, "GET", "/api/projects/"+itoa(p.ID), nil)
	json.Unmarshal(raw, &card)
	if card.AccountName != "Work" || card.AccountError == "" { // verificarea a picat (binar inexistent)
		t.Fatalf("detaliu: %+v", card)
	}
	if code, _ := e.do(t, "DELETE", "/api/accounts/"+itoa(acct), nil); code != http.StatusConflict {
		t.Fatalf("stergere cont folosit: %d", code)
	}

	// schimbarea contului sterge sesiunile agentilor
	lead := e.root(t, p.ID)
	e.st.SetSession(lead.ID, "sess-old", "h")
	if code, raw := e.do(t, "PUT", "/api/projects/"+itoa(p.ID), map[string]any{"name": "Acme", "accountId": 0}); code != 200 {
		t.Fatalf("PUT Default: %d %s", code, raw)
	}
	if got, _ := e.st.GetAgent(lead.ID); got.SessionID != "" {
		t.Fatal("sesiunea trebuia stearsa")
	}
	// fara accountId in corp: contul ramane
	e.do(t, "PUT", "/api/projects/"+itoa(p.ID), map[string]any{"name": "Acme", "accountId": acct})
	e.st.SetSession(lead.ID, "sess-2", "h")
	if code, _ := e.do(t, "PUT", "/api/projects/"+itoa(p.ID), map[string]any{"name": "Acme 2"}); code != 200 {
		t.Fatal("PUT fara cont")
	}
	if got, _ := e.st.GetProject(p.ID); got.AccountID == nil || *got.AccountID != acct {
		t.Fatalf("PUT fara accountId a schimbat contul: %v", got.AccountID)
	}
	if got, _ := e.st.GetAgent(lead.ID); got.SessionID != "sess-2" {
		t.Fatal("PUT fara schimbare de cont a sters sesiunea")
	}
	if code, _ := e.do(t, "PUT", "/api/projects/"+itoa(p.ID), map[string]any{"name": "Acme", "accountId": 999}); code != http.StatusBadRequest {
		t.Fatalf("cont necunoscut: %d", code)
	}
}

func TestProjectAccountChange409WhenActive(t *testing.T) {
	e := setup(t)
	acct := e.addAccount(t, "Work", "sk-ant-oat01-token-1234")
	p := e.createProject(t, t.TempDir())
	lead := e.root(t, p.ID)
	e.tm.UserMessage(lead.ID, "porneste", nil)
	if err := e.tm.Reconcile(p.ID); err != nil {
		t.Fatal(err)
	}
	code, raw := e.do(t, "PUT", "/api/projects/"+itoa(p.ID), map[string]any{"name": "Renamed", "accountId": acct})
	if code != http.StatusConflict || !strings.Contains(string(raw), "Stop the team first") {
		t.Fatalf("schimbare cu echipa activa: %d %s", code, raw)
	}
	if got, _ := e.st.GetProject(p.ID); got.AccountID != nil || got.Name == "Renamed" {
		t.Fatalf("nu trebuia salvat nimic: %+v", got)
	}
}
