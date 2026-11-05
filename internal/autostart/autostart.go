// Package autostart porneste HobStack la login-ul utilizatorului: pe Windows o valoare in
// HKCU\Software\Microsoft\Windows\CurrentVersion\Run, pe macOS un LaunchAgent (~/Library/LaunchAgents), pe Linux
// un fisier .desktop in ~/.config/autostart. Comanda e mereu `<exe> start --no-open` (fara browser la login).
package autostart

// Args: argumentele cu care porneste HobStack la login.
var Args = []string{"start", "--no-open"}

// Label: numele intrarii (valoarea din registry, eticheta LaunchAgent-ului).
const Label = "HobStack"
