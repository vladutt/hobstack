package prompt

import (
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"

	"foreman/internal/store"
)

var proj = store.Project{Name: "ApiUnit", Goal: "Import din OpenAPI 3.1", Context: "Stack: PHP 8.3"}

func ptrID(v int64) *int64 { return &v }

// propozitia pentru lideri despre firele de lucru (continues la create_task)
const continuesSentence = `When a new task continues earlier work (a fix after an audit, a follow-up of T-85), pass continues: "T-85" to mcp__team__create_task so it is grouped with it.`

func TestSystemRoles(t *testing.T) {
	a := store.Agent{Name: "Sub", Role: "Tech Lead", Background: "Stie Laravel.", DoneDef: "rezumat si fisiere schimbate"}
	cases := []struct {
		name      string
		links     Links
		want, not []string
	}{
		{"worker", Links{BossName: "Atlas"},
			[]string{"# Role\nYou are Sub, Tech Lead, in the project ApiUnit. You report to Atlas.",
				"# When you finish a task\nCall mcp__team__report_done with: rezumat si fisiere schimbate",
				"# How you work\nWork only on the task you received", "# Background\nStie Laravel."},
			[]string{"create_task", "review_approve"}},
		{"sub-lider", Links{BossName: "Atlas", IsLead: true},
			[]string{"mcp__team__create_task", "stop without calling report_done", `"Takes"`, continuesSentence},
			[]string{"Work only on the task you received", "review_approve"}},
		{"radacina lider", Links{IsLead: true},
			[]string{"You report to the user.", "mcp__team__create_task", continuesSentence,
				"# When you finish\nWhen the goal is reached, write the user a summary. The summary includes: rezumat si fisiere schimbate"},
			[]string{"report_done"}},
		{"reviewer", Links{BossName: "Atlas", IsReviewer: true},
			[]string{"mcp__team__review_approve", "mcp__team__review_reject", "Work only on the task you received",
				"Read the changed files to check the work; do not rely only on the summary."},
			[]string{"create_task", "continues:"}},
		{"singur", Links{},
			[]string{"You report to the user.", "You talk directly with the user"},
			[]string{"mcp__team__create_task", "report_done"}},
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
	if got := System(store.Project{Name: "X"}, store.Agent{Name: "A"}, Links{}); !strings.Contains(got, "You are A, agent, in the project X.") {
		t.Errorf("rol gol: %s", got)
	}
	// radacina lider: propozitia despre rezumat apare o singura data
	if got := System(proj, a, Links{IsLead: true}); strings.Count(got, "write the user a summary") != 1 {
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
		"coordonator": {lead, Links{IsLead: true, IsReviewer: true, CanAddAgents: true}},
		"sub-lider":   {sub, Links{BossName: "Atlas", IsLead: true, CanAddAgents: true}},
		"worker":      {wes, Links{BossName: "Sub"}},
		"agent nou":   {store.Agent{ParentID: &two}, Links{BossName: "Sub"}},
	}
	for name, c := range cases {
		if got := LinksFor(c.a, team); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: %+v, vreau %+v", name, got, c.want)
		}
	}
}

func TestTeamBlock(t *testing.T) {
	got := Team([]Member{{Name: "Rook", Role: "Backend Dev", Responsibilities: "API si baze de date", Status: "Idle"}, {Name: "Vera"}})
	if got != "Your direct team:\n- Rook: Backend Dev, Idle\n  Takes: API si baze de date\n- Vera" {
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
	ctx := "Your direct team:\n- Wes: Dev, Idle" +
		"\n\nYou review the work of: Ana, Ion" +
		"\n\nYour open tasks:\n- T-1: Import (subtasks done: 1 of 2)\n- T-4: Export (no subtasks yet)"
	if got := Context(c); got != ctx {
		t.Fatalf("Context =\n%s", got)
	}
	if Context(TurnContext{}) != "" {
		t.Fatal("context gol")
	}
	task := store.Task{Number: 3, Title: "Parser", Description: "Doar local.", Feedback: "Lipsesc testele."}
	got := TaskTurn(c, task, []string{"T-5 [done] Lexer -> Wes"}, []string{"User: hurry up"})
	want := ctx + "\n\nTask T-3: Parser\nDoar local." +
		"\n\nSubtasks already created for this task (do not create them again):\n- T-5 [done] Lexer -> Wes" +
		"\n\nFeedback on the previous attempt:\nLipsesc testele." +
		"\n\nNotifications:\n- User: hurry up"
	if got != want {
		t.Fatalf("TaskTurn =\n%s\nvreau\n%s", got, want)
	}
	if got := TaskTurn(TurnContext{}, store.Task{Number: 1, Title: "A"}, nil, nil); got != "Task T-1: A" {
		t.Fatalf("simplu: %q", got)
	}
	if got := NotifyTurn(TurnContext{}, []string{"salut"}); got != "Notifications:\n- salut" {
		t.Fatalf("NotifyTurn gol: %q", got)
	}
	if got := NotifyTurn(c, []string{"x"}); got != ctx+"\n\nNotifications:\n- x" {
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
	if err := json.Unmarshal([]byte(Settings([]string{"git", "write"}, "", nil, nil)), &cfg); err != nil {
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
	for _, want := range []string{"TaskCreate", "TodoWrite", "Agent", "Bash(npm install:*)"} {
		if !slices.Contains(cfg.Permissions.Deny, want) {
			t.Errorf("deny nu contine %s", want)
		}
	}
	// fara tool-uri: doar citire + echipa
	json.Unmarshal([]byte(Settings(nil, "", nil, nil)), &cfg)
	if len(cfg.Permissions.Allow) != 4 {
		t.Errorf("allow fara tool-uri = %v", cfg.Permissions.Allow)
	}
}

func TestMCPConfig(t *testing.T) {
	want := `{"mcpServers":{"team":{"type":"http","url":"http://127.0.0.1:7777/mcp/abc"}}}`
	if got := MCPConfig("http://127.0.0.1:7777/mcp/abc", nil); got != want {
		t.Fatalf("MCPConfig = %s", got)
	}
}

func TestSystemAskUser(t *testing.T) {
	for _, l := range []Links{{}, {IsLead: true}, {BossName: "Atlas"}, {BossName: "Atlas", IsReviewer: true}} {
		got := System(proj, store.Agent{Name: "A"}, l)
		if !strings.Contains(got, "mcp__team__ask_user") {
			t.Errorf("%+v: lipseste ask_user", l)
		}
		has := strings.Contains(got, "Do not call report_done while you wait for the answer")
		if has != (l.BossName != "") {
			t.Errorf("%+v: howAskWait=%v", l, has)
		}
	}
}

// workerul nu mai e trimis la report_done pentru orice nelamurire:
// ce poate rezolva doar utilizatorul merge pe ask_user
func TestWorkerUnclearUsesAskUser(t *testing.T) {
	got := System(proj, store.Agent{Name: "A"}, Links{BossName: "Atlas"})
	want := "If something is unclear or blocked and only the user can resolve it, use mcp__team__ask_user; " +
		"if it is a problem for your lead, call mcp__team__report_done explaining it. Do not guess."
	if !strings.Contains(got, want) {
		t.Fatalf("lipseste regula pentru nelamuriri:\n%s", got)
	}
	if strings.Contains(got, "call mcp__team__report_done explaining the problem instead of guessing") {
		t.Fatal("textul vechi a ramas")
	}
}

// modul autonom: nicio mentiune a lui ask_user, in schimb fraza de autonomie
func TestSystemAutonomy(t *testing.T) {
	auto := proj
	auto.Autonomy = true
	a := store.Agent{Name: "A", Role: "Dev"}
	const want = "Full autonomy is on: the user will not answer questions. Make reasonable decisions yourself and state your assumptions in your report."
	for _, l := range []Links{{}, {IsLead: true}, {BossName: "Boss"}, {BossName: "Boss", IsLead: true}, {BossName: "Boss", IsReviewer: true}} {
		got := System(auto, a, l)
		if strings.Contains(got, "ask_user") || strings.Contains(got, "wait for the answer") {
			t.Errorf("%+v: ask_user in modul autonom:\n%s", l, got)
		}
		if strings.Count(got, want) != 1 {
			t.Errorf("%+v: lipseste fraza de autonomie:\n%s", l, got)
		}
		// modul normal ramane neschimbat
		if normal := System(proj, a, l); strings.Contains(normal, want) || !strings.Contains(normal, "mcp__team__ask_user") {
			t.Errorf("%+v: modul normal s-a schimbat:\n%s", l, normal)
		}
	}
	if got := System(auto, a, Links{BossName: "Boss"}); !strings.Contains(got, "call mcp__team__report_done explaining it") {
		t.Errorf("worker-ul autonom tot raporteaza problemele liderului:\n%s", got)
	}
}

func TestSystemAddAgents(t *testing.T) {
	p := store.Project{Name: "P", Goal: "G"}
	lead := store.Agent{ID: 1, Name: "Lead"}
	worker := store.Agent{ID: 2, Name: "Rook", ParentID: ptrID(1)}
	team := []store.Agent{lead, worker}
	if l := LinksFor(lead, team); !l.CanAddAgents {
		t.Fatal("radacina/liderul poate adauga agenti")
	}
	if l := LinksFor(worker, team); l.CanAddAgents {
		t.Fatal("un worker cu sef nu poate")
	}
	solo := store.Agent{ID: 3, Name: "Solo"}
	if l := LinksFor(solo, []store.Agent{solo}); !l.CanAddAgents {
		t.Fatal("radacina fara echipa poate")
	}
	if s := System(p, lead, LinksFor(lead, team)); !strings.Contains(s, "mcp__team__add_agent") || !strings.Contains(s, "mcp__team__remove_agent") {
		t.Fatalf("liderul trebuie sa afle de add_agent:\n%s", s)
	}
	if s := System(p, worker, LinksFor(worker, team)); strings.Contains(s, "add_agent") {
		t.Fatal("workerul nu afla de add_agent")
	}
}

func TestSystemLinked(t *testing.T) {
	p := store.Project{Name: "P", Goal: "G"}
	lead := store.Agent{ID: 1, Name: "Lead"}
	worker := store.Agent{ID: 2, Name: "Rook", ParentID: ptrID(1)}
	team := []store.Agent{lead, worker}
	ll := LinksFor(lead, team)
	if s := System(p, lead, ll); strings.Contains(s, "list_linked_projects") {
		t.Fatal("fara legaturi nu apare nimic")
	}
	ll.Linked = true
	s := System(p, lead, ll)
	for _, want := range []string{"mcp__team__list_linked_projects", "mcp__team__ask_project", "mcp__team__request_project_work", "mcp__team__reply_to_project"} {
		if !strings.Contains(s, want) {
			t.Fatalf("radacina/liderul trebuie sa afle de %s:\n%s", want, s)
		}
	}
	wl := LinksFor(worker, team)
	wl.Linked = true
	s = System(p, worker, wl)
	if !strings.Contains(s, "mcp__team__ask_project") || strings.Contains(s, "request_project_work") || strings.Contains(s, "reply_to_project") {
		t.Fatalf("workerul doar intreaba:\n%s", s)
	}
}

func TestLeftForUserInPrompt(t *testing.T) {
	worker := System(proj, store.Agent{Name: "A"}, Links{BossName: "Atlas"})
	sub := System(proj, store.Agent{Name: "S"}, Links{BossName: "Atlas", IsLead: true})
	root := System(proj, store.Agent{Name: "R"}, Links{IsLead: true})
	for name, got := range map[string]string{"worker": worker, "sub-lead": sub} {
		if !strings.Contains(got, "left_for_user") || !strings.Contains(got, "mcp__team__tick_left_for_user") {
			t.Fatalf("%s: lipseste regula left_for_user:\n%s", name, got)
		}
	}
	if strings.Contains(root, "left_for_user") {
		t.Fatal("radacina nu lasa puncte utilizatorului")
	}
}

func TestFilesInPrompt(t *testing.T) {
	worker := System(proj, store.Agent{Name: "A"}, Links{BossName: "Atlas"})
	sub := System(proj, store.Agent{Name: "S"}, Links{BossName: "Atlas", IsLead: true})
	root := System(proj, store.Agent{Name: "R"}, Links{IsLead: true})
	for name, got := range map[string]string{"worker": worker, "sub-lead": sub} {
		if !strings.Contains(got, "list its path in files when you call report_done") || !strings.Contains(got, "or in the project files folder") {
			t.Fatalf("%s: lipseste regula files:\n%s", name, got)
		}
	}
	if strings.Contains(root, "in files when you call report_done") {
		t.Fatal("radacina nu foloseste report_done")
	}
}

func TestSystemBlockedCommands(t *testing.T) {
	p := store.Project{Name: "Shop"}
	a := store.Agent{Name: "Rook", Tools: []string{"shell"}}
	got := System(p, a, Links{BossName: "Lead", BlockedCommands: []string{"sudo", "docker"}})
	if !strings.Contains(got, "These commands are blocked in this project; do not try them: sudo, docker.") {
		t.Fatal(got)
	}
	if strings.Contains(System(p, a, Links{BossName: "Lead"}), "blocked in this project") {
		t.Fatal("fara comenzi blocate nu apare linia")
	}
}
