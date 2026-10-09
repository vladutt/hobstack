package runner

import (
	"os"
	"slices"
	"strings"
)

// authVars aleg contul (sau furnizorul) cu care ruleaza claude. Un run cu cont propriu
// (Spec.Env) nu le mosteneste din shell-ul care a pornit foreman: o cheie API sau un
// gateway mostenit ar castiga in fata token-ului contului, iar munca ar ajunge pe alt cont.
var authVars = []string{"ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN", "ANTHROPIC_BASE_URL",
	"CLAUDE_CODE_OAUTH_TOKEN", "CLAUDE_CODE_USE_BEDROCK", "CLAUDE_CODE_USE_VERTEX"}

// cleanEnv scoate authVars din mediu (pe Windows numele nu tin cont de litere mari/mici).
func cleanEnv(env []string) []string {
	out := make([]string, 0, len(env))
	for _, kv := range env {
		k, _, _ := strings.Cut(kv, "=")
		if slices.ContainsFunc(authVars, func(v string) bool { return strings.EqualFold(v, k) }) {
			continue
		}
		out = append(out, kv)
	}
	return out
}

// procEnv: mediul procesului claude. Fara env propriu (Default) mosteneste tot (nil);
// cu env propriu scoate variabilele de cont mostenite si le adauga pe cele date.
func procEnv(extra []string) []string {
	if len(extra) == 0 {
		return nil
	}
	return append(cleanEnv(os.Environ()), extra...)
}

// authMarkers: fragmente din erorile de autentificare ale lui claude (token expirat sau
// revocat, delogat, cheie invalida). Comparate fara diferenta intre litere mari/mici.
var authMarkers = []string{"authentication_error", "failed to authenticate", "invalid api key", "oauth token",
	"not logged in", "/login", "api error: 401"}

// IsAuthError spune daca eroarea unui run vine de la autentificare.
func IsAuthError(msg string) bool {
	m := strings.ToLower(msg)
	for _, s := range authMarkers {
		if strings.Contains(m, s) {
			return true
		}
	}
	return false
}
