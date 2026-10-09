package mcpregistry

import (
	"net/url"
	"regexp"
	"slices"
	"strings"
)

// Ascunderea credentialelor din comenzile, argumentele si URL-urile gasite in configurarile Claude.
// Valoarea gasita e inlocuita cu Hidden inainte sa iasa din LocalServers. Regulile sunt largi
// intentionat: o potrivire gresita doar face serverul neinstalabil, nu scapa nimic.

// Hidden inlocuieste valoarea unui credential.
const Hidden = "<hidden>"

// secretKey: nume de optiune / variabila / parametru / header care arata a credential.
var secretKey = regexp.MustCompile(`(?i)(key|token|secret|passw(or)?d|pwd|auth|credential|bearer|session|cookie|sig|signature|private)`)

// secretHeaders: header-e care poarta credentiale chiar daca numele nu se potriveste cu secretKey.
var secretHeaders = []string{"authorization", "cookie", "x-api-key", "proxy-authorization"}

// envAssign: NAME=value (dupa docker -e / --env, `env`, `set`); numele poate avea si "-".
var envAssign = regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_-]*)=`)

// headerShape: "Name: value" (si "Name:value"), dar nu "https://...".
var headerShape = regexp.MustCompile(`^(["']?)([A-Za-z][A-Za-z0-9_-]*):\s*(.+)$`)

// headerName: un cuvant de forma "Name:" (valoarea e cuvantul urmator).
var headerName = regexp.MustCompile(`^["']?([A-Za-z][A-Za-z0-9_-]*):$`)

// jsonKey: cheile dintr-o valoare JSON ("apiKey": ...).
var jsonKey = regexp.MustCompile(`"([^"\\]*)"\s*:`)

// urlPrefix: "scheme://" la inceputul unei valori.
var urlPrefix = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9+.-]*://`)

func isSecretHeader(name string) bool {
	return secretKey.MatchString(name) || slices.Contains(secretHeaders, strings.ToLower(name))
}

func isHeaderFlag(a string) bool {
	return a == "-H" || a == "--header" || a == "--headers" || a == "-header"
}

// isSecretFlag: "--token" / "-api-key" fara "=": valoarea e argumentul urmator.
func isSecretFlag(a string) bool {
	return strings.HasPrefix(a, "-") && !strings.Contains(a, "=") && secretKey.MatchString(strings.TrimLeft(a, "-"))
}

// ce urmeaza dupa un cuvant
type follow int

const (
	followNone   follow = iota
	followSecret        // urmatorul cuvant e o valoare secreta
	followHeader        // urmatorul cuvant e un header ("Name: value")
)

// followOf spune ce fel de valoare vine dupa cuvantul a.
func followOf(a string) follow {
	t := strings.Trim(a, `"'`)
	switch {
	case isHeaderFlag(t):
		return followHeader
	case isSecretFlag(t):
		return followSecret
	case strings.EqualFold(t, "bearer") || strings.EqualFold(t, "basic"):
		return followSecret
	}
	if m := headerName.FindStringSubmatch(a); m != nil && isSecretHeader(m[1]) {
		return followSecret
	}
	return followNone
}

// hideHeader: valoarea unui header dat dupa --header / -H; se pastreaza numele daca exista.
func hideHeader(v string) string {
	// doar "Name:" (cuvant separat): valoarea vine in cuvantul urmator (vezi followOf)
	if headerName.MatchString(v) {
		return v
	}
	if m := headerShape.FindStringSubmatch(v); m != nil && !strings.HasPrefix(m[3], "//") {
		return m[1] + m[2] + ": " + Hidden
	}
	return Hidden
}

// redactList ascunde credentialele dintr-o lista de argumente (sau de cuvinte); tine minte
// cuvantul precedent ("--token" -> urmatorul e secret, chiar daca incepe cu "-").
func redactList(items []string) ([]string, bool) {
	out := slices.Clone(items)
	hidden := false
	next := followNone
	for i, a := range out {
		switch next {
		case followSecret:
			out[i], hidden = Hidden, true
		case followHeader:
			out[i], hidden = hideHeader(a), true
		default:
			if r, h := redactItem(a); h {
				out[i], hidden = r, true
			}
		}
		next = followOf(a)
	}
	return out, hidden
}

// redactItem: un argument intreg; daca are spatii (sh -c "...", "--token S") il luam si cuvant cu cuvant.
func redactItem(a string) (string, bool) {
	if m := headerShape.FindStringSubmatch(a); m != nil && !strings.HasPrefix(m[3], "//") && isSecretHeader(m[2]) {
		return m[1] + m[2] + ": " + Hidden, true
	}
	r, hidden := redactOne(a)
	if strings.ContainsAny(r, " \t\r\n") {
		if words, h := redactList(strings.Fields(r)); h {
			r, hidden = strings.Join(words, " "), true
		}
	}
	return r, hidden
}

// redactOne: un singur cuvant: --flag=valoare, NAME=valoare, JSON sau URL.
func redactOne(a string) (string, bool) {
	if strings.HasPrefix(a, "-") {
		name, val, ok := strings.Cut(a, "=")
		if !ok {
			return a, false
		}
		if isHeaderFlag(name) {
			return name + "=" + hideHeader(val), true
		}
		if secretKey.MatchString(strings.TrimLeft(name, "-")) {
			return name + "=" + Hidden, true
		}
		r, h := redactValue(val)
		return name + "=" + r, h
	}
	return redactValue(a)
}

// redactValue: valoarea unui argument.
func redactValue(v string) (string, bool) {
	if t := strings.TrimLeft(v, `"' `); strings.HasPrefix(t, "{") || strings.HasPrefix(t, "[") {
		// JSON: ascundem tot daca are o cheie cu nume de credential
		for _, m := range jsonKey.FindAllStringSubmatch(v, -1) {
			if secretKey.MatchString(m[1]) {
				return Hidden, true
			}
		}
		return v, false
	}
	if m := envAssign.FindStringSubmatch(v); m != nil {
		if secretKey.MatchString(m[1]) {
			return m[1] + "=" + Hidden, true
		}
		r, h := redactValue(v[len(m[0]):])
		return m[0] + r, h
	}
	return redactURL(v)
}

// redactURL ascunde userinfo (parola, sau tot userinfo cand e doar un nume / token) si valorile
// parametrilor cu nume de credential din query si din fragment. Lucreaza pe text (url.Parse refuza
// unele parole si ar lasa valoarea sa treaca), ca restul URL-ului sa ramana cum era.
func redactURL(raw string) (string, bool) {
	scheme := urlPrefix.FindString(raw)
	if scheme == "" {
		return raw, false
	}
	rest, hidden := raw[len(scheme):], false

	// userinfo se termina la ultimul "@" care e inca in autoritate: inainte de el fie nu e niciun
	// "/", "?", "#", fie exista un ":" inaintea lor (parola care contine / ? #)
	at := -1
	for i := len(rest) - 1; i >= 0; i-- {
		if rest[i] != '@' {
			continue
		}
		cand := rest[:i]
		sep := strings.IndexAny(cand, "/?#")
		colon := strings.Index(cand, ":")
		if sep < 0 || (colon >= 0 && colon < sep) {
			at = i
			break
		}
	}
	head, tail := "", rest
	if at >= 0 {
		user := rest[:at]
		sep := strings.IndexAny(user, "/?#")
		if colon := strings.Index(user, ":"); colon >= 0 && (sep < 0 || colon < sep) {
			head = user[:colon+1] + Hidden + "@"
		} else {
			head = Hidden + "@"
		}
		tail = rest[at+1:]
		hidden = true
	}

	// query (?...) si fragment (#...)
	path, frag, hasFrag := strings.Cut(tail, "#")
	path, query, hasQuery := strings.Cut(path, "?")
	if hasQuery {
		if r, h := redactPairs(query); h {
			query, hidden = r, true
		}
	}
	if hasFrag {
		if r, h := redactPairs(frag); h {
			frag, hidden = r, true
		}
	}
	out := scheme + head + path
	if hasQuery {
		out += "?" + query
	}
	if hasFrag {
		out += "#" + frag
	}
	return out, hidden
}

// redactPairs: k=v&k2=v2; valorile cu nume de credential devin Hidden.
func redactPairs(s string) (string, bool) {
	parts := strings.Split(s, "&")
	hidden := false
	for i, p := range parts {
		k, _, ok := strings.Cut(p, "=")
		if !ok {
			continue
		}
		key, err := url.QueryUnescape(k)
		if err != nil {
			key = k
		}
		if secretKey.MatchString(key) {
			parts[i], hidden = k+"="+Hidden, true
		}
	}
	return strings.Join(parts, "&"), hidden
}
