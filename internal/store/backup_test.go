package store

import (
	"path/filepath"
	"testing"
)

func TestBackup(t *testing.T) {
	s := newStore(t)
	p := Project{Name: "Copy me", Workdir: t.TempDir()}
	if _, err := s.CreateProject(&p); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "b.db")
	if err := s.Backup(path); err != nil {
		t.Fatal(err)
	}
	c, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	ps, err := c.ListProjects()
	if err != nil || len(ps) != 1 || ps[0].Name != "Copy me" {
		t.Fatalf("backup: %v %+v", err, ps)
	}
}
