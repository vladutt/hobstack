// Package autostart porneste HobStack la login-ul utilizatorului: pe Windows o valoare in
// HKCU\Software\Microsoft\Windows\CurrentVersion\Run, pe macOS un LaunchAgent (~/Library/LaunchAgents), pe Linux
// un fisier .desktop in ~/.config/autostart. Comanda e mereu `<exe> start --no-open` (fara browser la login).
package autostart

import (
	"os"
	"path/filepath"
	"strings"
)

// Args: argumentele cu care porneste HobStack la login.
var Args = []string{"start", "--no-open"}

// Label: numele intrarii (valoarea din registry, eticheta LaunchAgent-ului).
const Label = "HobStack"

// choiceFile: marcajul ca s-a decis pornirea la login (implicit la prima pornire sau de utilizator in Settings).
// Fara el, o versiune oficiala o activeaza singura; dupa el, alegerea utilizatorului ramane.
const choiceFile = "autostart-chosen"

// Default activeaza pornirea la login o singura data, daca nimeni n-a ales inca. enabled spune daca a activat-o acum.
func Default(root, home, exe, logFile string) (enabled bool, err error) {
	if Chosen(root) {
		// pornirea ramane cum a ales-o utilizatorul, dar trebuie sa duca la exe-ul de acum (de ex. pornit intai de pe
		// Desktop, apoi instalat cu installerul: intrarea arata inca spre exe-ul vechi)
		if Enabled(home, exe) && !strings.Contains(command(home), exe) {
			return false, Enable(home, exe, logFile)
		}
		return false, nil
	}
	if !Enabled(home, exe) {
		if err := Enable(home, exe, logFile); err != nil {
			return false, err
		}
		enabled = true
	}
	return enabled, MarkChosen(root)
}

// Chosen: pornirea la login a fost deja decisa (implicit sau de utilizator).
func Chosen(root string) bool {
	_, err := os.Stat(filepath.Join(root, choiceFile))
	return err == nil
}

// MarkChosen retine ca pornirea la login e decisa; Default nu o mai schimba.
func MarkChosen(root string) error {
	return os.WriteFile(filepath.Join(root, choiceFile), []byte("1\n"), 0o644)
}
