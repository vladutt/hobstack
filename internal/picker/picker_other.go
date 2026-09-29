//go:build !darwin && !windows

package picker

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
)

// Available: pe Linux si altele avem nevoie de zenity instalat.
func Available() bool {
	_, err := exec.LookPath("zenity")
	return err == nil
}

// Pick deschide dialogul zenity. Cod de iesire 1 = anulat.
func Pick(ctx context.Context) (string, error) {
	if !Available() {
		return "", ErrUnsupported
	}
	cmd := exec.CommandContext(ctx, "zenity", "--file-selection", "--directory",
		"--title=Alege folderul proiectului")
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
		return "", ErrCanceled
	}
	return parseResult(stdout.String(), stderr.String(), err)
}
