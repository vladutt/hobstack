package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestMoveLegacyData(t *testing.T) {
	home := t.TempDir()
	old := filepath.Join(home, ".foreman")
	os.MkdirAll(filepath.Join(old, "projects", "5", "files"), 0o755)
	for _, f := range []string{"foreman.db", "foreman.db-wal", "foreman.db-shm", "projects/5/files/a.md"} {
		os.WriteFile(filepath.Join(old, f), []byte(f), 0o644)
	}

	got, err := MoveLegacyData(home)
	if err != nil {
		t.Fatal(err)
	}
	if got != old {
		t.Fatalf("got %q, vreau %q", got, old)
	}
	root := filepath.Join(home, ".hobstack")
	for _, f := range []string{"hobstack.db", "hobstack.db-wal", "hobstack.db-shm", "projects/5/files/a.md"} {
		if _, err := os.Stat(filepath.Join(root, f)); err != nil {
			t.Errorf("lipseste %s: %v", f, err)
		}
	}
	if b, _ := os.ReadFile(filepath.Join(root, "hobstack.db")); string(b) != "foreman.db" {
		t.Errorf("hobstack.db = %q", b)
	}
	if runtime.GOOS != "windows" {
		// calea veche ramane valabila (text vechi din task-uri si mesaje)
		if b, err := os.ReadFile(filepath.Join(old, "projects/5/files/a.md")); err != nil || string(b) != "projects/5/files/a.md" {
			t.Errorf("symlink-ul vechi: %v %q", err, b)
		}
	}

	// a doua pornire: nimic de mutat
	if got, err := MoveLegacyData(home); err != nil || got != "" {
		t.Fatalf("a doua oara: %q %v", got, err)
	}
}

func TestMoveLegacyDataNothing(t *testing.T) {
	home := t.TempDir()
	if got, err := MoveLegacyData(home); err != nil || got != "" {
		t.Fatalf("fara date vechi: %q %v", got, err)
	}
	// ambele exista: nu atingem nimic
	os.MkdirAll(filepath.Join(home, ".foreman"), 0o755)
	os.MkdirAll(filepath.Join(home, ".hobstack"), 0o755)
	os.WriteFile(filepath.Join(home, ".foreman", "foreman.db"), []byte("x"), 0o644)
	if got, err := MoveLegacyData(home); err != nil || got != "" {
		t.Fatalf("ambele: %q %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(home, ".foreman", "foreman.db")); err != nil {
		t.Fatal("datele vechi au fost atinse")
	}
}
