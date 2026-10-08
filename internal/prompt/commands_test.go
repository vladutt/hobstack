package prompt

import (
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
