//go:build windows

package picker

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// Available: PowerShell vine cu Windows.
func Available() bool { return true }

// Pick deschide FolderBrowserDialog prin PowerShell. Iesire goala = anulat.
func Pick(ctx context.Context) (string, error) {
	script := `Add-Type -AssemblyName System.Windows.Forms; ` +
		`$d=New-Object System.Windows.Forms.FolderBrowserDialog; ` +
		`if($d.ShowDialog() -eq 'OK'){ $d.SelectedPath }`
	out, err := exec.CommandContext(ctx, "powershell", "-NoProfile", "-STA", "-Command", script).Output()
	if err != nil {
		return "", fmt.Errorf("dialog folder: %w", err)
	}
	path := strings.TrimSpace(string(out))
	if path == "" {
		return "", ErrCanceled
	}
	return path, nil
}
