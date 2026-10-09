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

// DenyingRule: regula Ask/Block care a refuzat de fapt comanda (in claude orice deny potrivit castiga,
// chiar daca un Allow mai lung se potriveste). Block > Ask, apoi cel mai lung prefix; "", "" daca nu e niciuna.
func DenyingRule(command string, rules []store.CommandRule) (mode, prefix string) {
	var deny []store.CommandRule
	for _, r := range rules {
		if r.Mode == store.CmdAsk || r.Mode == store.CmdBlock {
			deny = append(deny, r)
		}
	}
	rank := map[string]int{store.CmdAsk: 1, store.CmdBlock: 2}
	for _, cmd := range splitCommand(command) {
		r, ok := matchPart(cmd, deny)
		if !ok {
			continue
		}
		if rank[r.Mode] > rank[mode] || (r.Mode == mode && len(r.Prefix) > len(prefix)) {
			mode, prefix = r.Mode, r.Prefix
		}
	}
	return mode, prefix
}

// DenyPrefixes: prefixele Ask si Block, sortate. Un grant (Allow once) ridica doar un Ask, niciodata un Block.
func DenyPrefixes(rules []store.CommandRule, granted []string) []string {
	out := []string{}
	for _, r := range rules {
		if r.Mode == store.CmdBlock || (r.Mode == store.CmdAsk && !slices.Contains(granted, r.Prefix)) {
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

// subcommandTools: unelte la care al doilea cuvant e subcomanda (git stash, npm run...).
var subcommandTools = map[string]bool{
	"git": true, "npm": true, "npx": true, "pnpm": true, "yarn": true, "composer": true,
	"go": true, "docker": true, "cargo": true, "make": true, "kubectl": true,
}

var subcommandWord = regexp.MustCompile(`^[a-z][a-z0-9:-]*$`)

// wrapperCommands: comenzi care ruleaza alte comenzi (interpretoare, sudo, env...); aprobarea lor
// ar fi cat Shell-ul intreg (si ar ocoli regulile Block), deci nu primesc prefix.
var wrapperCommands = map[string]bool{
	"bash": true, "sh": true, "zsh": true, "fish": true, "dash": true, "env": true, "xargs": true,
	"eval": true, "exec": true, "sudo": true, "su": true, "doas": true, "nohup": true, "timeout": true,
	"nice": true, "time": true, "command": true, "builtin": true, "watch": true, "php": true,
	"python": true, "python3": true, "python2": true, "node": true, "perl": true, "ruby": true,
	"deno": true, "bun": true, "osascript": true,
}

// CommandPrefix: prefixul pe care il aprobi pentru o comanda (o bucata, fara && / |):
// primul cuvant, plus subcomanda la uneltele cu subcomenzi ("git stash", "php artisan migrate").
// "" = nu se poate aproba ca prefix (prea larg): un interpretor / wrapper, sau o unealta cu
// subcomenzi la care pe locul subcomenzii e un flag ("git -C dir ..." ar insemna tot git-ul).
func CommandPrefix(cmd string) string {
	words := strings.Fields(cmd)
	if len(words) == 0 {
		return ""
	}
	n := 1
	switch {
	case words[0] == "php" && len(words) > 1 && words[1] == "artisan":
		n = 2
	case wrapperCommands[words[0]]:
		return ""
	case !subcommandTools[words[0]]:
		return words[0]
	}
	if len(words) > n {
		if !subcommandWord.MatchString(words[n]) {
			return ""
		}
		n++
	}
	return strings.Join(words[:n], " ")
}

// BashAllowed spune daca o bucata de comanda e permisa agentului: cu Shell orice,
// altfel ce permit tool-urile lui (git -> git status...) si grant-urile.
func BashAllowed(cmd string, tools, granted []string) bool {
	if slices.Contains(tools, "shell") {
		return true
	}
	var rules []string
	for _, name := range tools {
		rules = append(rules, toolRules[name]...)
	}
	for _, g := range granted {
		rules = append(rules, "Bash("+g+":*)")
	}
	return bashMatches(cmd, rules)
}

// Request: o cerere pentru "Needs you" dintr-o comanda refuzata (Kind = store.Access*).
type Request struct {
	Prefix string
	Kind   string
}

// NoShellRequests imparte comanda refuzata unui agent fara Shell in cereri, pe bucati:
// regula Block = fara cerere (blocked); regula Ask = prefixul regulii (daca nu are deja grant);
// refuz fix (npm install fara packages) = cerere de tool; bucata permisa = nimic;
// altfel prefixul calculat. Fara nicio cerere si fara Block ramane cererea de tool (Enable Shell),
// ca utilizatorul sa vada refuzul.
func NoShellRequests(cmd string, tools []string, rules []store.CommandRule, granted []string) (reqs []Request, blocked bool) {
	add := func(r Request) {
		if !slices.Contains(reqs, r) {
			reqs = append(reqs, r)
		}
	}
	for _, part := range splitCommand(cmd) {
		mode, pfx := MatchCommand(part, rules)
		if mode != store.CmdBlock && mode != store.CmdAsk {
			mode, pfx = DenyingRule(part, rules)
		}
		switch {
		case mode == store.CmdBlock:
			blocked = true
		case mode == store.CmdAsk:
			if !slices.Contains(granted, pfx) {
				add(Request{pfx, store.AccessAsk})
			}
		case bashMatches(part, denyFor(tools)):
			add(Request{"", store.AccessTool})
		case BashAllowed(part, tools, granted):
			// deja permisa
		default:
			if pfx := CommandPrefix(part); pfx != "" {
				add(Request{pfx, store.AccessCommand})
			} else {
				add(Request{"", store.AccessTool}) // prea larg pentru un prefix: doar Enable Shell
			}
		}
	}
	if len(reqs) == 0 && !blocked {
		reqs = []Request{{"", store.AccessTool}}
	}
	return reqs, blocked
}
