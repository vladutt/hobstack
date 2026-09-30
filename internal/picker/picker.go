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
	ErrCanceled = errors.New("canceled")
	// ErrUnsupported: pe acest sistem nu exista un dialog disponibil.
	ErrUnsupported = errors.New("the folder picker is not available on this system")
)

// errAutomationDenied: userul a refuzat permisiunea de Automatizare pe macOS.
var errAutomationDenied = errors.New("macOS denied access to the dialog; allow Terminal/foreman in System Settings > Privacy & Security > Automation")

// parseResult transforma iesirea unei comenzi de dialog intr-o cale sau o eroare.
// Pur (nu ruleaza nimic), deci se poate testa pe orice OS.
func parseResult(stdout, stderr string, err error) (string, error) {
	if err != nil {
		// osascript raporteaza anularea ca eroare cu "User canceled" / (-128).
		if strings.Contains(stderr, "User canceled") || strings.Contains(stderr, "(-128)") {
			return "", ErrCanceled
		}
		// -1743: macOS nu a dat voie accesul la System Events (Automatizare)
		if strings.Contains(stderr, "-1743") {
			return "", errAutomationDenied
		}
		return "", fmt.Errorf("folder dialog: %w: %s", err, strings.TrimSpace(stderr))
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
