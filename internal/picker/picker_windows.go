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
//
// PowerShell-ul e pornit de HobStack din fundal, iar Windows nu lasa un proces din fundal sa-si aduca
// fereastra in fata (foreground lock): fara owner, dialogul se deschidea in spatele browserului. De aceea
// dialogul primeste ca owner o forma invizibila TopMost (o fereastra detinuta sta mereu peste owner-ul ei),
// deci apare peste browser chiar daca nu primeste focusul.
func Pick(ctx context.Context) (string, error) {
	script := `[Console]::OutputEncoding=[System.Text.Encoding]::UTF8; ` +
		`Add-Type -AssemblyName System.Windows.Forms,System.Drawing; ` +
		`$o=New-Object System.Windows.Forms.Form; ` +
		`$o.TopMost=$true; $o.ShowInTaskbar=$false; $o.Opacity=0; ` +
		`$o.FormBorderStyle='None'; $o.StartPosition='CenterScreen'; $o.Size=New-Object System.Drawing.Size(1,1); ` +
		`$o.Show(); $o.Activate(); ` +
		`$d=New-Object System.Windows.Forms.FolderBrowserDialog; ` +
		`$d.Description='Choose the project folder'; ` +
		`$r=$d.ShowDialog($o); $o.Close(); ` +
		`if($r -eq 'OK'){ $d.SelectedPath }`
	out, err := exec.CommandContext(ctx, "powershell", "-NoProfile", "-STA", "-Command", script).Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) && len(ee.Stderr) > 0 { // Output() pune stderr in ExitError
			return "", fmt.Errorf("folder dialog: %w: %s", err, strings.TrimSpace(string(ee.Stderr)))
		}
		return "", fmt.Errorf("folder dialog: %w", err)
	}
	path := strings.TrimSpace(string(out))
	if path == "" {
		return "", ErrCanceled
	}
	return path, nil
}
