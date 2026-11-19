//go:build windows

package main

// allowForeground: pornit a doua oara, procesul nou (pornit de utilizator, deci cu voie sa aduca ferestre in fata) da
// voie instantei care ruleaza sa-si aduca fereastra in fata; altfel Windows doar clipeste butonul din bara de activitati.
func allowForeground() {
	user32.NewProc("AllowSetForegroundWindow").Call(0xFFFFFFFF) // ASFW_ANY
}
