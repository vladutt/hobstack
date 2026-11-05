//go:build !windows

package claudecli

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

const rcLine = `export PATH="$HOME/.local/bin:$PATH"`

// rcFile: fisierul de pornire al shell-ului utilizatorului ("" = shell necunoscut, de ex. fish).
func rcFile(home string) string {
	switch filepath.Base(os.Getenv("SHELL")) {
	case "zsh", "":
		return filepath.Join(home, ".zshrc")
	case "bash":
		if runtime.GOOS == "darwin" {
			return filepath.Join(home, ".bash_profile")
		}
		return filepath.Join(home, ".bashrc")
	}
	return ""
}

// InPath spune daca dir e in PATH-ul procesului sau e adaugat in fisierul de pornire al shell-ului.
func InPath(_ context.Context, home, dir string) bool {
	for _, p := range filepath.SplitList(os.Getenv("PATH")) {
		if filepath.Clean(p) == filepath.Clean(dir) {
			return true
		}
	}
	if rc := rcFile(home); rc != "" {
		if b, err := os.ReadFile(rc); err == nil && strings.Contains(string(b), ".local/bin") {
			return true
		}
	}
	return false
}

// AddToPath adauga ~/.local/bin in fisierul de pornire al shell-ului (o linie la final).
func AddToPath(ctx context.Context, home, dir string) (string, error) {
	if InPath(ctx, home, dir) {
		return "It is already in your PATH.", nil
	}
	if dir != LocalBin(home) {
		return "", errors.New("only ~/.local/bin can be added automatically; add " + dir + " to your PATH by hand")
	}
	rc := rcFile(home)
	if rc == "" {
		return "", errors.New("your shell is not zsh or bash; add ~/.local/bin to your PATH by hand")
	}
	f, err := os.OpenFile(rc, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return "", err
	}
	defer f.Close()
	if _, err := f.WriteString("\n# Added by HobStack: Claude Code\n" + rcLine + "\n"); err != nil {
		return "", err
	}
	return "Added ~/.local/bin to " + rc + ". Open a new terminal window to use claude there.", nil
}

// shellQuote pune s intre apostrofuri pentru sh.
func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// OpenLogin deschide un terminal cu `claude auth login`.
func OpenLogin(bin string) error {
	line := shellQuote(bin) + " auth login"
	if runtime.GOOS == "darwin" {
		// textul comenzii intra intr-un sir AppleScript: \ si " se scapa
		as := strings.ReplaceAll(strings.ReplaceAll(line, `\`, `\\`), `"`, `\"`)
		return exec.Command("osascript", "-e", `tell application "Terminal" to do script "`+as+`"`,
			"-e", `tell application "Terminal" to activate`).Run()
	}
	for _, term := range [][]string{{"x-terminal-emulator", "-e"}, {"gnome-terminal", "--"}, {"konsole", "-e"}, {"xterm", "-e"}} {
		if _, err := exec.LookPath(term[0]); err == nil {
			return exec.Command(term[0], append(term[1:], "sh", "-c", line+"; exec $SHELL")...).Start()
		}
	}
	return errors.New("no terminal found; run `claude auth login` in a terminal")
}
