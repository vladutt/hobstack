package prompt

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"foreman/internal/store"
)

// forbiddenPrefixChars: caracterele de shell care ar face din prefix altceva decat inceputul unei comenzi.
const forbiddenPrefixChars = "&|;$`()<>*\"'\n\r\\{}[]?!#~"

// NormalizePrefix curata spatiile si valideaza un prefix de comanda.
func NormalizePrefix(p string) (string, error) {
	if strings.ContainsAny(p, forbiddenPrefixChars) {
		return "", fmt.Errorf("%q: use only words separated by spaces (no & | ; $ ` ( ) < > * ? ! # ~ \\ { } [ ] or quotes)", strings.TrimSpace(p))
	}
	p = strings.Join(strings.Fields(p), " ")
	if p == "" {
		return "", errors.New("the command can't be empty")
	}
	if len([]rune(p)) > 80 {
		return "", fmt.Errorf("%q: at most 80 characters", p)
	}
	return p, nil
}

// ValidateRules normalizeaza prefixele si refuza duplicatele si starile necunoscute.
func ValidateRules(rules []store.CommandRule) ([]store.CommandRule, error) {
	out := make([]store.CommandRule, 0, len(rules))
	seen := map[string]bool{}
	for _, r := range rules {
		p, err := NormalizePrefix(r.Prefix)
		if err != nil {
			return nil, err
		}
		if r.Mode != store.CmdAllow && r.Mode != store.CmdAsk && r.Mode != store.CmdBlock {
			return nil, fmt.Errorf("%q: the rule must be allow, ask or block", p)
		}
		if seen[p] {
			return nil, fmt.Errorf("%q appears twice", p)
		}
		seen[p] = true
		out = append(out, store.CommandRule{Prefix: p, Mode: r.Mode})
	}
	return out, nil
}

// EffectiveRules: intrarile trebuie sa fie deja normalizate (toate caile de scriere trec prin ValidateRules).
// Regulile globale, cu cele ale proiectului peste ele (acelasi prefix = al proiectului).
func EffectiveRules(global, project []store.CommandRule) []store.CommandRule {
	by := map[string]string{}
	for _, r := range global {
		by[r.Prefix] = r.Mode
	}
	for _, r := range project {
		by[r.Prefix] = r.Mode
	}
	out := make([]store.CommandRule, 0, len(by))
	for p, m := range by {
		out = append(out, store.CommandRule{Prefix: p, Mode: m})
	}
	slices.SortFunc(out, func(a, b store.CommandRule) int { return strings.Compare(a.Prefix, b.Prefix) })
	return out
}

// matchPart: regula cu cel mai lung prefix pentru o singura comanda (cuvinte intregi).
func matchPart(cmd string, rules []store.CommandRule) (store.CommandRule, bool) {
	var best store.CommandRule
	found := false
	for _, r := range rules {
		if cmd == r.Prefix || strings.HasPrefix(cmd, r.Prefix+" ") {
			if !found || len(r.Prefix) > len(best.Prefix) {
				best, found = r, true
			}
		}
	}
	return best, found
}

// MatchCommand intoarce starea si prefixul regulii care decide comanda (Block > Ask > Allow intre bucati);
// "", "" daca nicio regula nu se potriveste.
func MatchCommand(command string, rules []store.CommandRule) (mode, prefix string) {
	rank := map[string]int{store.CmdAllow: 1, store.CmdAsk: 2, store.CmdBlock: 3}
	for _, cmd := range splitCommand(command) {
		if r, ok := matchPart(cmd, rules); ok && rank[r.Mode] > rank[mode] {
			mode, prefix = r.Mode, r.Prefix
		}
	}
	return mode, prefix
}

// DenyPrefixes: prefixele Ask si Block, fara cele cu grant (Allow once), sortate.
func DenyPrefixes(rules []store.CommandRule, granted []string) []string {
	out := []string{}
	for _, r := range rules {
		if (r.Mode == store.CmdAsk || r.Mode == store.CmdBlock) && !slices.Contains(granted, r.Prefix) {
			out = append(out, r.Prefix)
		}
	}
	slices.Sort(out)
	return out
}

// BlockedPrefixes: prefixele pe Block (pentru linia din prompt), sortate.
func BlockedPrefixes(rules []store.CommandRule) []string {
	out := []string{}
	for _, r := range rules {
		if r.Mode == store.CmdBlock {
			out = append(out, r.Prefix)
		}
	}
	slices.Sort(out)
	return out
}

// RuleWarnings: un Allow mai lung sub un Ask/Block mai scurt nu poate fi respectat (in claude deny castiga).
func RuleWarnings(rules []store.CommandRule) []string {
	out := []string{}
	for _, a := range rules {
		if a.Mode != store.CmdAllow {
			continue
		}
		for _, d := range rules {
			if d.Mode != store.CmdAllow && strings.HasPrefix(a.Prefix, d.Prefix+" ") {
				out = append(out, fmt.Sprintf("%q can't be allowed while %q is %s: the shorter rule wins.", a.Prefix, d.Prefix, d.Mode))
			}
		}
	}
	return out
}

var envAssign = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*=\S*$`)

// splitCommand taie o comanda Bash in comenzile simple din ea (separatori: & | ; newline ( ) ` si $( ),
// cu spatii normalizate si fara atribuirile de variabile de la inceput (FOO=1 sudo x -> "sudo x").
func splitCommand(command string) []string {
	command = strings.ReplaceAll(command, "$(", ";")
	parts := strings.FieldsFunc(command, func(r rune) bool { return strings.ContainsRune("&|;\n\r()`", r) })
	out := []string{}
	for _, part := range parts {
		words := strings.Fields(part)
		for len(words) > 0 && envAssign.MatchString(words[0]) {
			words = words[1:]
		}
		if len(words) > 0 {
			out = append(out, strings.Join(words, " "))
		}
	}
	return out
}
