// Pachetul clipfiles citeste caile fisierelor copiate in clipboard-ul sistemului (Finder, Explorer).
// Firefox da paginii doar primul fisier la paste; serverul, local, le poate citi pe toate.
// Implementarile difera pe fiecare OS (vezi clipfiles_*.go).
package clipfiles

import (
	"net/url"
	"strings"
)

// Max: cate cai intoarce Paths (restul se ignora)
const Max = 50

// parseLines: o cale pe linie (iesirea osascript / PowerShell), fara linii goale, cel mult Max
func parseLines(out string) []string {
	var paths []string
	for _, l := range strings.Split(out, "\n") {
		l = strings.TrimRight(l, "\r")
		if strings.TrimSpace(l) == "" {
			continue
		}
		paths = append(paths, l)
		if len(paths) == Max {
			break
		}
	}
	return paths
}

// parseURIList: text/uri-list (Linux): doar URI-urile file://, decodate; comentariile (#) se sar
func parseURIList(out string) []string {
	var paths []string
	for _, l := range strings.Split(out, "\n") {
		l = strings.TrimSpace(l)
		if l == "" || strings.HasPrefix(l, "#") {
			continue
		}
		u, err := url.Parse(l)
		if err != nil || u.Scheme != "file" || u.Path == "" {
			continue
		}
		paths = append(paths, u.Path)
		if len(paths) == Max {
			break
		}
	}
	return paths
}
