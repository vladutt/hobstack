//go:build darwin

package picker

import (
	"bytes"
	"context"
	"os/exec"
)

// Available: osascript exista pe orice macOS.
func Available() bool { return true }

// Pick deschide dialogul "choose folder" prin osascript si intoarce calea aleasa.
func Pick(ctx context.Context) (string, error) {
	cmd := exec.CommandContext(ctx, "osascript",
		"-e", `tell application "System Events"`,
		"-e", `activate`,
		"-e", `set f to POSIX path of (choose folder with prompt "Choose the project folder")`,
		"-e", `end tell`,
		"-e", `return f`,
	)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	return parseResult(stdout.String(), stderr.String(), err)
}
