//go:build darwin

package selfupdate

import (
	"log"
	"os/exec"
	"path/filepath"
	"strings"
)

// afterSwap: in HobStack.app binarul nou schimba continutul bundle-ului, deci versiunea din Info.plist se
// actualizeaza si bundle-ul se semneaza din nou ad-hoc (altfel macOS il poate vedea ca modificat).
// Release-urile de pe GitHub vin semnate Developer ID si notarizate (docs/macos-signing.md); dupa un update din
// aplicatie semnatura devine ad-hoc. E acceptabil: fisierele descarcate de aplicatie insasi nu primesc atributul de
// carantina (com.apple.quarantine), deci Gatekeeper nu le mai verifica si aplicatia porneste in continuare.
func afterSwap(exe, version string) {
	macos := filepath.Dir(exe)
	if filepath.Base(macos) != "MacOS" || !strings.HasSuffix(filepath.Dir(filepath.Dir(macos)), ".app") {
		return // binar simplu, nu intr-un .app
	}
	app := filepath.Dir(filepath.Dir(macos))
	plist := filepath.Join(app, "Contents", "Info.plist")
	for _, key := range []string{"CFBundleShortVersionString", "CFBundleVersion"} {
		if out, err := exec.Command("plutil", "-replace", key, "-string", version, plist).CombinedOutput(); err != nil {
			log.Printf("update: Info.plist %s: %v %s", key, err, out)
		}
	}
	if out, err := exec.Command("codesign", "--force", "--deep", "--sign", "-", app).CombinedOutput(); err != nil {
		log.Printf("update: signing %s: %v %s", app, err, out)
	}
}
