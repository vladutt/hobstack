//go:build !darwin && !windows

package clipfiles

import (
	"context"
	"os/exec"
)

// Paths: text/uri-list prin wl-paste (Wayland) sau xclip (X11); fara ele, nimic
func Paths(ctx context.Context) ([]string, error) {
	cmds := [][]string{
		{"wl-paste", "--no-newline", "--type", "text/uri-list"},
		{"xclip", "-selection", "clipboard", "-t", "text/uri-list", "-o"},
	}
	for _, c := range cmds {
		if _, err := exec.LookPath(c[0]); err != nil {
			continue
		}
		out, err := exec.CommandContext(ctx, c[0], c[1:]...).Output()
		if err != nil {
			return nil, nil // clipboard-ul nu are text/uri-list
		}
		return parseURIList(string(out)), nil
	}
	return nil, nil
}
