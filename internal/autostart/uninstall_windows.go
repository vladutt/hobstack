//go:build windows

package autostart

// uninstallKey: intrarea installerului din Add/Remove Programs (installer/hobstack.nsi).
const uninstallKey = `HKCU\Software\Microsoft\Windows\CurrentVersion\Uninstall\HobStack`

// SetInstalledVersion pune versiunea curenta in Add/Remove Programs dupa un update din aplicatie
// (doar daca HobStack a fost instalat cu installerul).
func SetInstalledVersion(version string) {
	if _, err := reg("query", uninstallKey, "/v", "DisplayVersion"); err != nil {
		return
	}
	reg("add", uninstallKey, "/v", "DisplayVersion", "/t", "REG_SZ", "/d", version, "/f")
}
