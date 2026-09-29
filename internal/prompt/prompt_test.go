package prompt

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"foreman/internal/store"
)

var proj = store.Project{Name: "ApiUnit", Goal: "Import din OpenAPI 3.1", Context: "Stack: PHP 8.3"}

func TestSystemRoles(t *testing.T) {
	a := store.Agent{Name: "Sub", Role: "Tech Lead", Background: "Stie Laravel.", DoneDef: "rezumat si fisiere schimbate"}
	cases := []struct {
		name      string
		links     Links
		want, not []string
	}{
		{"worker", Links{BossName: "Atlas"},
			[]string{"# Rol\nEsti Sub, Tech Lead, in proiectul ApiUnit. Raportezi lui Atlas.",
				"# Cand termini un task\nApeleaza mcp__team__report_done cu: rezumat si fisiere schimbate",
				"# Cum lucrezi\nLucreaza doar pe task-ul primit", "# Background\nStie Laravel."},
			[]string{"create_task", "review_approve"}},
		{"sub-lider", Links{BossName: "Atlas", IsLead: true},
			[]string{"mcp__team__create_task", "opreste-te fara report_done", `"Ce ia"`},
			[]string{"Lucreaza doar pe task-ul primit", "review_approve"}},
		{"radacina lider", Links{IsLead: true},
			[]string{"Raportezi utilizatorului.", "mcp__team__create_task",
				"# Cand termini\nCand obiectivul e atins, scrie utilizatorului un rezumat. Rezumatul contine: rezumat si fisiere schimbate"},
			[]string{"report_done"}},
		{"reviewer", Links{BossName: "Atlas", IsReviewer: true},
			[]string{"mcp__team__review_approve", "mcp__team__review_reject", "Lucreaza doar pe task-ul primit"},
			[]string{"create_task"}},
		{"singur", Links{},
			[]string{"Raportezi utilizatorului.", "Vorbesti direct cu utilizatorul"},
			[]string{"mcp__team__"}},
	}
	for _, c := range cases {
		got := System(proj, a, c.links)
		for _, w := range c.want {
			if !strings.Contains(got, w) {
				t.Errorf("%s: lipseste %q din:\n%s", c.name, w, got)
			}
		}
		for _, n := range c.not {
			if strings.Contains(got, n) {
				t.Errorf("%s: nu trebuia %q in:\n%s", c.name, n, got)
			}
		}
	}
	if got := System(store.Project{Name: "X"}, store.Agent{Name: "A"}, Links{}); !strings.Contains(got, "Esti A, agent, in proiectul X.") {
		t.Errorf("rol gol: %s", got)
	}
	// radacina lider: propozitia despre rezumat apare o singura data
	if got := System(proj, a, Links{IsLead: true}); strings.Count(got, "scrie utilizatorului un rezumat") != 1 {
		t.Errorf("propozitia se repeta:\n%s", got)
	}
}

func TestLinksFor(t *testing.T) {
	one, two := int64(1), int64(2)
	lead := store.Agent{ID: 1, Name: "Atlas"}
	sub := store.Agent{ID: 2, Name: "Sub", ParentID: &one}
	wes := store.Agent{ID: 3, Name: "Wes", ParentID: &two, ReviewerID: &one}
	team := []store.Agent{lead, sub, wes}
	cases := map[string]struct {
		a    store.Agent
		want Links
	}{
		"coordonator": {lead, Links{IsLead: true, IsReviewer: true}},
		"sub-lider":   {sub, Links{BossName: "Atlas", IsLead: true}},
		"worker":      {wes, Links{BossName: "Sub"}},
		"agent nou":   {store.Agent{ParentID: &two}, Links{BossName: "Sub"}},
	}
	for name, c := range cases {
		if got := LinksFor(c.a, team); got != c.want {
			t.Errorf("%s: %+v, vreau %+v", name, got, c.want)
		}
	}
}

func TestTeamBlock(t *testing.T) {
	got := Team([]Member{{Name: "Rook", Role: "Backend Dev", Responsibilities: "API si baze de date", Status: "Idle"}, {Name: "Vera"}})
	if got != "Echipa ta directa:\n- Rook: Backend Dev, Idle\n  Ce ia: API si baze de date\n- Vera" {
		t.Fatalf("Team = %q", got)
	}
	if Team(nil) != "" {
		t.Fatal("echipa goala trebuie sa dea text gol")
	}
}

func TestContextAndTurns(t *testing.T) {
	c := TurnContext{
		Direct:    []Member{{Name: "Wes", Role: "Dev", Status: "Idle"}},
		Reviewees: []string{"Ana", "Ion"},
		OpenTasks: []OpenTask{{Ref: "T-1", Title: "Import", Done: 1, Total: 2}, {Ref: "T-4", Title: "Export"}},
	}
	ctx := "Echipa ta directa:\n- Wes: Dev, Idle" +
		"\n\nRevizuiesti munca lui: Ana, Ion" +
		"\n\nTask-urile tale deschise:\n- T-1: Import (sub-task-uri gata: 1 din 2)\n- T-4: Export (fara sub-task-uri inca)"
	if got := Context(c); got != ctx {
		t.Fatalf("Context =\n%s", got)
	}
	if Context(TurnContext{}) != "" {
		t.Fatal("context gol")
	}
	task := store.Task{Number: 3, Title: "Parser", Description: "Doar local.", Feedback: "Lipsesc testele."}
	got := TaskTurn(c, task, []string{"T-5 [done] Lexer -> Wes"}, []string{"Utilizatorul: grabeste-te"})
	want := ctx + "\n\nTask T-3: Parser\nDoar local." +
		"\n\nSub-task-uri deja create pentru acest task (nu le crea din nou):\n- T-5 [done] Lexer -> Wes" +
		"\n\nFeedback la incercarea anterioara:\nLipsesc testele." +
		"\n\nNotificari:\n- Utilizatorul: grabeste-te"
	if got != want {
		t.Fatalf("TaskTurn =\n%s\nvreau\n%s", got, want)
	}
	if got := TaskTurn(TurnContext{}, store.Task{Number: 1, Title: "A"}, nil, nil); got != "Task T-1: A" {
		t.Fatalf("simplu: %q", got)
	}
	if got := NotifyTurn(TurnContext{}, []string{"salut"}); got != "Notificari:\n- salut" {
		t.Fatalf("NotifyTurn gol: %q", got)
	}
	if got := NotifyTurn(c, []string{"x"}); got != ctx+"\n\nNotificari:\n- x" {
		t.Fatalf("NotifyTurn: %q", got)
	}
}

func TestHashStable(t *testing.T) {
	if Hash("a") != Hash("a") || Hash("a") == Hash("b") || len(Hash("a")) != 64 {
		t.Fatal("Hash nu e sha256 hex stabil")
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
