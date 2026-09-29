//go:build windows

package picker

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// Available: PowerShell vine cu Windows.
func Available() bool { return true }

// Pick deschide FolderBrowserDialog prin PowerShell. Iesire goala = anulat.
func Pick(ctx context.Context) (string, error) {
	script := `[Console]::OutputEncoding=[System.Text.Encoding]::UTF8; ` +
		`Add-Type -AssemblyName System.Windows.Forms; ` +
		`$d=New-Object System.Windows.Forms.FolderBrowserDialog; ` +
		`if($d.ShowDialog() -eq 'OK'){ $d.SelectedPath }`
	out, err := exec.CommandContext(ctx, "powershell", "-NoProfile", "-STA", "-Command", script).Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) && len(ee.Stderr) > 0 { // Output() pune stderr in ExitError
			return "", fmt.Errorf("dialog folder: %w: %s", err, strings.TrimSpace(string(ee.Stderr)))
		}
		return "", fmt.Errorf("dialog folder: %w", err)
	}
	path := strings.TrimSpace(string(out))
	if path == "" {
		return "", ErrCanceled
	}
	return path, nil
}
