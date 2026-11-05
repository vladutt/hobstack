//go:build !windows

package main

import (
	"context"
	"log"
	"os"
	"os/exec"
	"strings"
	"time"
)

const pathMark = "__HOBSTACK_PATH__"

// loginShellPath: pornit din Finder, din Dock sau la login (launchd / desktop), HobStack primeste un PATH minim
// (/usr/bin:/bin...), iar agentii nu-si gasesc tool-urile (node, php, composer, go din Homebrew sau ~/.local/bin).
// Luam PATH-ul din shell-ul de login al utilizatorului, ca un terminal (cum face si VS Code). Din terminal (TERM
// setat) PATH-ul e deja cel bun.
func loginShellPath() {
	if os.Getenv("TERM") != "" {
		return
	}
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/zsh"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, shell, "-ilc", `printf '`+pathMark+`%s`+pathMark+`' "$PATH"`).Output()
	if err != nil {
		log.Printf("PATH from %s: %v", shell, err)
		return
	}
	s := string(out)
	i := strings.Index(s, pathMark)
	j := strings.LastIndex(s, pathMark)
	if i < 0 || j <= i {
		return
	}
	if p := s[i+len(pathMark) : j]; p != "" {
		os.Setenv("PATH", p)
	}
}
