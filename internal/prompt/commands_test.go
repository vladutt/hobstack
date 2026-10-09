package prompt

import (
	"slices"
	"strings"
	"testing"

	"foreman/internal/store"
)

func rules(pairs ...string) []store.CommandRule {
	var out []store.CommandRule
	for i := 0; i < len(pairs); i += 2 {
		out = append(out, store.CommandRule{Prefix: pairs[i], Mode: pairs[i+1]})
	}
	return out
}

func TestNormalizePrefix(t *testing.T) {
	if p, err := NormalizePrefix("  git   push "); err != nil || p != "git push" {
		t.Fatalf("%q %v", p, err)
	}
	for _, bad := range []string{"", "   ", "git push; rm", "a && b", "echo $HOME", "x|y", "`id`", "a\nb", "rm *", `a\b`, "x{y}", "a?", `say "hi"`, strings.Repeat("a", 81)} {
		if _, err := NormalizePrefix(bad); err == nil {
			t.Fatalf("acceptat: %q", bad)
		}
	}
}

func TestValidateRules(t *testing.T) {
	if _, err := ValidateRules(rules("git push", "ask", "git  push", "allow")); err == nil {
		t.Fatal("duplicat dupa normalizare acceptat")
	}
	if _, err := ValidateRules(rules("git push", "maybe")); err == nil {
		t.Fatal("stare necunoscuta acceptata")
	}
	got, err := ValidateRules(rules(" docker ", "block"))
	if err != nil || got[0].Prefix != "docker" {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestEffectiveAndMatch(t *testing.T) {
	global := rules("git push", "ask", "rm -rf", "ask", "sudo", "block", "git", "allow")
	project := rules("git push", "allow", "docker", "block")
	eff := EffectiveRules(global, project)
	cases := []struct{ cmd, mode, prefix string }{
		{"git push origin main", "allow", "git push"}, // proiectul a inlocuit regula globala
		{"git status", "allow", "git"},
		{"git pushx", "allow", "git"},             // cuvinte intregi: "git push" nu prinde "git pushx"
		{"echo ok && sudo rm x", "block", "sudo"}, // comanda legata: bucata a doua
		{"ls ; rm -rf build", "ask", "rm -rf"},
		{"docker ps | grep x", "block", "docker"},
		{"ls -la", "", ""},
	}
	for _, c := range cases {
		mode, prefix := MatchCommand(c.cmd, eff)
		if mode != c.mode || prefix != c.prefix {
			t.Errorf("%q -> %s %s, vreau %s %s", c.cmd, mode, prefix, c.mode, c.prefix)
		}
	}
	// Block castiga peste Ask intre bucati
	if mode, _ := MatchCommand("rm -rf x && sudo y", eff); mode != "block" {
		t.Fatalf("mode = %s", mode)
	}
}

func TestDenyBlockedWarnings(t *testing.T) {
	eff := rules("git push", "ask", "sudo", "block", "git", "block", "git status", "allow")
	deny := DenyPrefixes(eff, []string{"git push"})
	if strings.Join(deny, ",") != "git,sudo" {
		t.Fatalf("deny = %v", deny)
	}
	if strings.Join(BlockedPrefixes(eff), ",") != "git,sudo" {
		t.Fatalf("blocked = %v", BlockedPrefixes(eff))
	}
	w := RuleWarnings(eff)
	if len(w) != 1 || !strings.Contains(w[0], "git status") || !strings.Contains(w[0], `"git"`) {
		t.Fatalf("warnings = %v", w)
	}
}

func TestMatchCommandSplitting(t *testing.T) {
	eff := rules("sudo", "block", "git push", "ask", "rm -rf", "ask")
	cases := []struct{ cmd, mode, prefix string }{
		{"ls\nsudo rm x", "block", "sudo"},
		{"echo $(sudo x)", "block", "sudo"},
		{"(sudo x)", "block", "sudo"},
		{"FOO=1 BAR=2 git push origin", "ask", "git push"},
		{"echo `rm -rf /`", "ask", "rm -rf"},
		{"sudo\tls", "block", "sudo"},
	}
	for _, c := range cases {
		mode, prefix := MatchCommand(c.cmd, eff)
		if mode != c.mode || prefix != c.prefix {
			t.Errorf("%q -> %s %s, vreau %s %s", c.cmd, mode, prefix, c.mode, c.prefix)
		}
		if !AlwaysDeniedBash(c.cmd, nil, eff) || !bashMatches(c.cmd, []string{"Bash(" + c.prefix + ":*)"}) {
			t.Errorf("%q: AlwaysDeniedBash / bashMatches", c.cmd)
		}
	}
}

func TestDenyingRule(t *testing.T) {
	r := rules("git", store.CmdAsk, "git status", store.CmdAllow, "docker", store.CmdBlock, "git push", store.CmdAsk)
	if m, p := DenyingRule("git status", r); m != store.CmdAsk || p != "git" {
		t.Fatalf("%s %s", m, p)
	}
	if m, p := DenyingRule("git push && docker ps", r); m != store.CmdBlock || p != "docker" {
		t.Fatalf("%s %s", m, p)
	}
	if m, p := DenyingRule("git push origin", r); m != store.CmdAsk || p != "git push" {
		t.Fatalf("%s %s", m, p)
	}
	if m, _ := DenyingRule("ls", r); m != "" {
		t.Fatal(m)
	}
}

func TestGrantDoesNotLiftBlock(t *testing.T) {
	rules := []store.CommandRule{{Prefix: "git push", Mode: store.CmdAsk}, {Prefix: "sudo", Mode: store.CmdBlock}}
	got := DenyPrefixes(rules, []string{"git push", "sudo"})
	if len(got) != 1 || got[0] != "sudo" {
		t.Fatalf("%v", got)
	}
}

func TestAlwaysDeniedBashAnyDeny(t *testing.T) {
	rules := []store.CommandRule{{Prefix: "git", Mode: store.CmdAsk}, {Prefix: "git status", Mode: store.CmdAllow}}
	if !AlwaysDeniedBash("git status", []string{"shell"}, rules) {
		t.Fatal("git status e refuzat de git (Ask), chiar daca git status e Allow")
	}
}

func TestCommandPrefix(t *testing.T) {
	for in, want := range map[string]string{
		`git stash push -u -m "x" -- app/`: "git stash",
		"ls -la":                           "ls",
		"php artisan migrate --force":      "php artisan migrate",
		"php -v":                           "",
		"make":                             "make",
		"git -C dir status":                "",
		"bash -c ls":                       "",
		"env X=1 make":                     "",
		"python3 -c 'print(1)'":            "",
		"sudo ls":                          "",
		"npx prettier --write .":           "npx prettier",
		"./vendor/bin/phpunit --filter X":  "./vendor/bin/phpunit",
		"":                                 "",
	} {
		if got := CommandPrefix(in); got != want {
			t.Errorf("%q: %q, vrea %q", in, got, want)
		}
	}
}

func TestNoShellRequests(t *testing.T) {
	rules := []store.CommandRule{{Prefix: "git push", Mode: store.CmdAsk}, {Prefix: "sudo", Mode: store.CmdBlock}}
	git := []string{"git"}
	cases := []struct {
		cmd     string
		tools   []string
		granted []string
		want    []Request
		blocked bool
	}{
		// git stash nu e in tool-ul git: prefix calculat
		{"git stash push -u", git, nil, []Request{{"git stash", store.AccessCommand}}, false},
		// bucata permisa (git status) nu face cerere
		{"git status && git stash", git, nil, []Request{{"git stash", store.AccessCommand}}, false},
		// cu grant: nimic de cerut, ramane cererea de tool (Enable Shell)
		{"git stash", git, []string{"git stash"}, []Request{{"", store.AccessTool}}, false},
		// regula Ask: prefixul regulii
		{"git push origin main", git, nil, []Request{{"git push", store.AccessAsk}}, false},
		// Block: fara cerere
		{"sudo ls", git, nil, nil, true},
		// refuz fix (instalare fara packages): cerere de tool
		{"npm install x", git, nil, []Request{{"", store.AccessTool}}, false},
		// prefix prea larg (flag in locul subcomenzii, interpretor): doar Enable Shell
		{"git -C app stash", git, nil, []Request{{"", store.AccessTool}}, false},
		{"bash -c 'git stash'", git, nil, []Request{{"", store.AccessTool}}, false},
		// doua bucati necunoscute, fara duplicate
		{"cd app && make build && make test", nil, nil, []Request{{"cd", store.AccessCommand}, {"make build", store.AccessCommand}, {"make test", store.AccessCommand}}, false},
	}
	for _, c := range cases {
		got, blocked := NoShellRequests(c.cmd, c.tools, rules, c.granted)
		if !slices.Equal(got, c.want) || blocked != c.blocked {
			t.Errorf("%q: %+v %v, vrea %+v %v", c.cmd, got, blocked, c.want, c.blocked)
		}
	}
}
