package web

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// slugify face din numele proiectului un nume de folder: litere mici, cifre si
// "-". Diacriticele romanesti devin litere simple.
func slugify(name string) string {
	repl := strings.NewReplacer("ă", "a", "â", "a", "î", "i", "ș", "s", "ț", "t", "ş", "s", "ţ", "t")
	var b strings.Builder
	dash := true // true la inceput: nu incepem cu "-"
	for _, r := range repl.Replace(strings.ToLower(name)) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			dash = false
		} else if !dash {
			b.WriteByte('-')
			dash = true
		}
	}
	slug := strings.Trim(b.String(), "-")
	if len(slug) > 60 {
		slug = strings.Trim(slug[:60], "-")
	}
	if slug == "" {
		return "proiect"
	}
	return slug
}

// resolveWorkdir intoarce folderul de lucru al proiectului si il creeaza daca
// lipseste. Fara input, alege un folder nou in workspaces (slug, slug-2, ...).
func resolveWorkdir(input, projectName, workspaces string) (string, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		if err := os.MkdirAll(workspaces, 0o755); err != nil {
			return "", fmt.Errorf("nu pot crea %s: %w", workspaces, err)
		}
		base := filepath.Join(workspaces, slugify(projectName))
		// Mkdir esueaza atomic daca exista deja, deci nu avem cursa check-then-create.
		for n := 1; n <= 1000; n++ {
			dir := base
			if n > 1 {
				dir = fmt.Sprintf("%s-%d", base, n)
			}
			err := os.Mkdir(dir, 0o755)
			if err == nil {
				return dir, nil
			}
			if !errors.Is(err, fs.ErrExist) {
				return "", fmt.Errorf("nu pot crea folderul de lucru: %w", err)
			}
		}
		return "", fmt.Errorf("nu gasesc un nume liber pentru folderul de lucru in %s", workspaces)
	}
	dir, err := expandHome(input)
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(dir) {
		return "", badRequest("folderul de lucru trebuie sa fie o cale absoluta, ex. /Users/tu/proiect sau ~/proiect")
	}
	dir = filepath.Clean(dir)
	if info, err := os.Stat(dir); err == nil {
		if !info.IsDir() {
			return "", badRequest("exista deja un fisier cu acest nume: " + dir)
		}
		return dir, nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", badRequest("nu pot crea folderul de lucru " + dir + ": " + err.Error())
	}
	return dir, nil
}
