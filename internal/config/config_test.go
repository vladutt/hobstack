package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultPaths(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	p, err := Default()
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(home, ".foreman")
	if p.Root != root {
		t.Errorf("Root = %q, vreau %q", p.Root, root)
	}
	if p.DB != filepath.Join(root, "foreman.db") {
		t.Errorf("DB = %q", p.DB)
	}
	if p.Logs != filepath.Join(root, "logs", "runs") {
		t.Errorf("Logs = %q", p.Logs)
	}
	if p.Projects != filepath.Join(root, "projects") {
		t.Errorf("Projects = %q", p.Projects)
	}
	if p.Workspaces != filepath.Join(home, "Foreman") {
		t.Errorf("Workspaces = %q", p.Workspaces)
	}
	if d := ProjectFilesDir(p.Projects, 7); !strings.HasSuffix(filepath.ToSlash(d), "projects/7/files") {
		t.Errorf("ProjectFilesDir = %q", d)
	}
	if err := p.Ensure(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p.Logs); err != nil {
		t.Errorf("Ensure nu a creat %s: %v", p.Logs, err)
	}
}
