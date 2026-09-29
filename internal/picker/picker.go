// Pachetul picker deschide dialogul nativ al sistemului pentru a alege un folder.
// Implementarile difera pe fiecare OS (vezi picker_*.go).
package picker

import (
	"errors"
	"fmt"
	"strings"
)

var (
	// ErrCanceled: utilizatorul a inchis dialogul fara sa aleaga nimic.
	ErrCanceled = errors.New("anulat")
	// ErrUnsupported: pe acest sistem nu exista un dialog disponibil.
	ErrUnsupported = errors.New("dialogul de ales folder nu e disponibil pe acest sistem")
)

// parseResult transforma iesirea unei comenzi de dialog intr-o cale sau o eroare.
// Pur (nu ruleaza nimic), deci se poate testa pe orice OS.
func parseResult(stdout, stderr string, err error) (string, error) {
	if err != nil {
		// osascript raporteaza anularea ca eroare cu "User canceled" / (-128).
		if strings.Contains(stderr, "User canceled") || strings.Contains(stderr, "(-128)") {
			return "", ErrCanceled
		}
		return "", fmt.Errorf("dialog folder: %w: %s", err, strings.TrimSpace(stderr))
	}
	path := strings.TrimSpace(stdout)
	if path == "" {
		return "", ErrCanceled
	}
	// Taiem slash-ul final, dar lasam "/" singur (radacina).
	if path != "/" {
		path = strings.TrimSuffix(path, "/")
		if path == "" {
			path = "/"
		}
	}
	return path, nil
}
