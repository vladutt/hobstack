//go:build windows

package claudecli

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// userPath citeste PATH-ul de utilizator din registry (HKCU\Environment), prin PowerShell.
func userPath(ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "powershell", "-NoProfile", "-Command",
		"[Environment]::GetEnvironmentVariable('Path','User')").Output()
	return strings.TrimSpace(string(out)), err
}

func samePath(a, b string) bool {
	clean := func(p string) string {
		return strings.ToLower(strings.TrimRight(filepath.Clean(os.ExpandEnv(windowsVars(p))), `\`))
	}
	return clean(a) == clean(b)
}

// windowsVars: %USERPROFILE% -> ${USERPROFILE}, ca sa le extinda os.ExpandEnv.
func windowsVars(p string) string {
	for strings.Count(p, "%") >= 2 {
		i := strings.Index(p, "%")
		j := strings.Index(p[i+1:], "%") + i + 1
		p = p[:i] + "${" + p[i+1:j] + "}" + p[j+1:]
	}
	return p
}

// InPath spune daca dir e in PATH-ul procesului sau in PATH-ul de utilizator salvat.
func InPath(ctx context.Context, home, dir string) bool {
	for _, p := range filepath.SplitList(os.Getenv("PATH")) {
		if samePath(p, dir) {
			return true
		}
	}
	up, err := userPath(ctx)
	if err != nil {
		return false
	}
	for _, p := range strings.Split(up, ";") {
		if p != "" && samePath(p, dir) {
			return true
		}
	}
	return false
}

// AddToPath pune dir la finalul PATH-ului de utilizator (restul ramane neatins). SetEnvironmentVariable anunta
// Windows-ul, deci ferestrele noi de terminal il vad. Folderul vine prin variabila de mediu, nu in comanda.
func AddToPath(ctx context.Context, home, dir string) (string, error) {
	if InPath(ctx, home, dir) {
		return "It is already in your PATH.", nil
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	script := `$d = $env:HOBSTACK_ADD_DIR
$p = [Environment]::GetEnvironmentVariable('Path','User')
if ([string]::IsNullOrEmpty($p)) { $n = $d } else { $n = $p.TrimEnd(';') + ';' + $d }
[Environment]::SetEnvironmentVariable('Path', $n, 'User')`
	cmd := exec.CommandContext(ctx, "powershell", "-NoProfile", "-Command", script)
	cmd.Env = append(os.Environ(), "HOBSTACK_ADD_DIR="+dir)
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", errors.New("updating the user PATH failed: " + strings.TrimSpace(string(out)))
	}
	os.Setenv("PATH", os.Getenv("PATH")+string(os.PathListSeparator)+dir)
	return "Added " + dir + " to your user PATH. Open a new terminal window to use claude there.", nil
}

// OpenLogin deschide o fereastra noua de consola cu `claude auth login`.
func OpenLogin(bin string) error {
	cmd := exec.Command("powershell", "-NoProfile", "-Command",
		"Start-Process -FilePath $env:HOBSTACK_CLAUDE -ArgumentList 'auth','login'")
	cmd.Env = append(os.Environ(), "HOBSTACK_CLAUDE="+bin)
	return cmd.Run()
}
