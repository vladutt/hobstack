package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMoveToInstall(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(t.TempDir(), "HobStack", "data")
	old := filepath.Join(home, ".hobstack")

	// fara date vechi: nimic
	if moved, err := MoveToInstall(home, root); moved != "" || err != nil {
		t.Fatalf("nothing to move: %q %v", moved, err)
	}

	os.MkdirAll(filepath.Join(old, "projects"), 0o755)
	os.WriteFile(filepath.Join(old, "hobstack.db"), []byte("db"), 0o644)
	os.MkdirAll(root, 0o755) // un folder data gol
	moved, err := MoveToInstall(home, root)
	if err != nil || moved != old {
		t.Fatalf("move: %q %v", moved, err)
	}
	if b, _ := os.ReadFile(filepath.Join(root, "hobstack.db")); string(b) != "db" {
		t.Fatal("the database did not move")
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatal("the old folder is still there")
	}

	// a doua oara: instalarea are deja baza ei
	os.MkdirAll(old, 0o755)
	os.WriteFile(filepath.Join(old, "hobstack.db"), []byte("other"), 0o644)
	if moved, err := MoveToInstall(home, root); moved != "" || err != nil {
		t.Fatalf("second time: %q %v", moved, err)
	}
	if b, _ := os.ReadFile(filepath.Join(root, "hobstack.db")); string(b) != "db" {
		t.Fatal("the install database was replaced")
	}
}
