//go:build darwin

package autostart

import (
	"encoding/xml"
	"os"
	"path/filepath"
	"strings"
)

// LaunchAgentID: numele fisierului din ~/Library/LaunchAgents.
const LaunchAgentID = "app.hobstack"

func Supported() bool { return true }

func plistPath(home string) string {
	return filepath.Join(home, "Library", "LaunchAgents", LaunchAgentID+".plist")
}

func Enabled(home, _ string) bool {
	_, err := os.Stat(plistPath(home))
	return err == nil
}

func esc(s string) string {
	var b strings.Builder
	xml.EscapeText(&b, []byte(s))
	return b.String()
}

// Enable scrie LaunchAgent-ul: porneste la login, log-ul in logFile. PATH-ul de acum intra in plist: launchd
// porneste cu un PATH minim, iar agentii au nevoie de tool-urile utilizatorului (npm, go, php...).
func Enable(home, exe, logFile string) error {
	var args strings.Builder
	for _, a := range append([]string{exe}, Args...) {
		args.WriteString("\t\t<string>" + esc(a) + "</string>\n")
	}
	path := os.Getenv("PATH")
	for _, extra := range []string{filepath.Join(home, ".local", "bin"), "/opt/homebrew/bin", "/usr/local/bin", "/usr/bin", "/bin", "/usr/sbin", "/sbin"} {
		if !strings.Contains(string(os.PathListSeparator)+path+string(os.PathListSeparator), string(os.PathListSeparator)+extra+string(os.PathListSeparator)) {
			path += string(os.PathListSeparator) + extra
		}
	}
	plist := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>` + LaunchAgentID + `</string>
	<key>ProgramArguments</key>
	<array>
` + args.String() + `	</array>
	<key>RunAtLoad</key>
	<true/>
	<key>EnvironmentVariables</key>
	<dict>
		<key>PATH</key>
		<string>` + esc(path) + `</string>
	</dict>
	<key>StandardOutPath</key>
	<string>` + esc(logFile) + `</string>
	<key>StandardErrorPath</key>
	<string>` + esc(logFile) + `</string>
</dict>
</plist>
`
	p := plistPath(home)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	return os.WriteFile(p, []byte(plist), 0o644)
}

func Disable(home, _ string) error {
	err := os.Remove(plistPath(home))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}
