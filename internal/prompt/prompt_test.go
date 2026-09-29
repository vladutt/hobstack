package prompt

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"foreman/internal/store"
)

var proj = store.Project{Name: "ApiUnit", Goal: "Import din OpenAPI 3.1", Context: "Stack: PHP 8.3"}

func TestSystemWorker(t *testing.T) {
	a := store.Agent{Name: "Rook", Role: "Backend Dev", Position: store.PositionWorker,
		Background: "Stie Laravel.", DoneDef: "rezumat si fisiere schimbate"}
	got := System(proj, a, "Atlas")
	for _, want := range []string{
		"# Rol\nEsti Rook, Backend Dev, worker in proiectul ApiUnit. Raportezi lui Atlas (lead).",
		"# Background\nStie Laravel.",
		"# Cand termini un task\nApeleaza mcp__team__report_done cu: rezumat si fisiere schimbate",
		"# Proiect\nObiectiv: Import din OpenAPI 3.1\n\nContext comun:\nStack: PHP 8.3",
		"# Cum lucrezi\nLucreaza doar pe task-ul primit",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("lipseste %q din:\n%s", want, got)
		}
	}
	if strings.Contains(got, "# Instructiuni") {
		t.Error("sectiunea goala Instructiuni nu trebuia sa apara")
	}
}

func TestSystemLead(t *testing.T) {
	a := store.Agent{Name: "Atlas", Position: store.PositionLead}
	got := System(store.Project{Name: "X"}, a, "Atlas")
	for _, want := range []string{
		"Esti Atlas, lead, lead in proiectul X. Raportezi utilizatorului.",
		"# Cand termini\nCand obiectivul e atins, scrie utilizatorului un rezumat.",
		"mcp__team__create_task",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("lipseste %q din:\n%s", want, got)
		}
	}
	if strings.Contains(got, "# Proiect") {
		t.Error("proiect fara obiectiv si context: sectiunea nu trebuia sa apara")
	}
}

func TestHashStable(t *testing.T) {
	if Hash("a") != Hash("a") || Hash("a") == Hash("b") || len(Hash("a")) != 64 {
		t.Fatal("Hash nu e sha256 hex stabil")
	}
}

var team = []Member{
	{Name: "Atlas", Role: "Tech Lead", Position: "lead", Status: "Working"},
	{Name: "Rook", Role: "Backend Dev", Position: "worker", Status: "Idle"},
	{Name: "Vera", Position: "worker"},
}

func TestTeam(t *testing.T) {
	want := "Echipa:\n- Atlas: lead, Tech Lead, Working\n- Rook: worker, Backend Dev, Idle\n- Vera: worker"
	if got := Team(team); got != want {
		t.Fatalf("Team =\n%s\nvreau\n%s", got, want)
	}
}

func TestWorkerTurn(t *testing.T) {
	task := store.Task{Number: 3, Title: "Parser $ref", Description: "Doar referinte locale.", Feedback: "Lipsesc testele."}
	got := WorkerTurn(team, task, "Atlas", []string{"Grabeste-te"})
	want := Team(team) +
		"\n\nTask T-3: Parser $ref\nDoar referinte locale." +
		"\n\nFeedback de la Atlas la incercarea anterioara:\nLipsesc testele." +
		"\n\nMesaje noi de la utilizator:\n- Grabeste-te"
	if got != want {
		t.Fatalf("WorkerTurn =\n%s\nvreau\n%s", got, want)
	}
	plain := WorkerTurn(team, store.Task{Number: 1, Title: "A"}, "Atlas", nil)
	if plain != Team(team)+"\n\nTask T-1: A" {
		t.Fatalf("fara extra: %q", plain)
	}
}

func TestDirectAndLeadTurn(t *testing.T) {
	if got := DirectTurn([]string{"salut"}); got != "Mesaje de la utilizator (nu ai un task activ, raspunde direct):\n- salut" {
		t.Fatalf("DirectTurn = %q", got)
	}
	got := LeadTurn(team, []string{"Utilizatorul: fa X", "Rook a terminat T-1"})
	if got != Team(team)+"\n\nNotificari:\n- Utilizatorul: fa X\n- Rook a terminat T-1" {
		t.Fatalf("LeadTurn = %q", got)
	}
	// mesajele turei nu au voie sa inceapa cu "-": claude le-ar lua drept flag
	for _, s := range []string{got, DirectTurn([]string{"-x"}), WorkerTurn(team, store.Task{Title: "-x"}, "A", nil)} {
		if strings.HasPrefix(s, "-") {
			t.Fatalf("mesaj care incepe cu '-': %q", s)
		}
	}
}

func TestSettings(t *testing.T) {
	var cfg struct {
		Permissions struct {
			Allow []string `json:"allow"`
			Deny  []string `json:"deny"`
		} `json:"permissions"`
	}
	if err := json.Unmarshal([]byte(Settings([]string{"git", "write"})), &cfg); err != nil {
		t.Fatal(err)
	}
	allow := cfg.Permissions.Allow
	for _, want := range []string{"Read", "Glob", "Grep", "mcp__team__*", "Edit", "Write", "Bash(git commit:*)"} {
		if !slices.Contains(allow, want) {
			t.Errorf("allow nu contine %s: %v", want, allow)
		}
	}
	for _, not := range []string{"Bash", "WebSearch"} {
		if slices.Contains(allow, not) {
			t.Errorf("allow nu trebuia sa contina %s", not)
		}
	}
	for _, want := range []string{"TaskCreate", "TodoWrite", "Agent", "Bash(git push:*)", "Bash(rm -rf:*)", "Bash(npm install:*)"} {
		if !slices.Contains(cfg.Permissions.Deny, want) {
			t.Errorf("deny nu contine %s", want)
		}
	}
	// fara tool-uri: doar citire + echipa
	json.Unmarshal([]byte(Settings(nil)), &cfg)
	if len(cfg.Permissions.Allow) != 4 {
		t.Errorf("allow fara tool-uri = %v", cfg.Permissions.Allow)
	}
}

func TestMCPConfig(t *testing.T) {
	want := `{"mcpServers":{"team":{"type":"http","url":"http://127.0.0.1:7777/mcp/abc"}}}`
	if got := MCPConfig("http://127.0.0.1:7777/mcp/abc"); got != want {
		t.Fatalf("MCPConfig = %s", got)
	}
}
