//go:build windows

package clipfiles

import (
	"context"
	"os/exec"
)

// Paths: caile fisierelor din clipboard (FileDropList), prin PowerShell (nil daca nu sunt fisiere)
func Paths(ctx context.Context) ([]string, error) {
	script := `[Console]::OutputEncoding=[System.Text.Encoding]::UTF8; ` +
		`Get-Clipboard -Format FileDropList | ForEach-Object { $_.FullName }`
	out, err := exec.CommandContext(ctx, "powershell", "-NoProfile", "-STA", "-Command", script).Output()
	if err != nil {
		return nil, err
	}
	return parseLines(string(out)), nil
}
