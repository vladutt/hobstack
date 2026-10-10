package store

import (
	"errors"
	"testing"
	"time"
)

func TestLibrarySources(t *testing.T) {
	s := newStore(t)
	it := LibraryItem{Kind: KindSkill, Name: "pdf", Description: "d", Status: ItemPublished}
	if err := s.AddLibraryItem(&it); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetLibrarySource(it.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
	src := LibrarySource{ItemID: it.ID, Kind: SourceGitHub, Repo: "anthropics/skills", Path: "skills/pdf", Ref: "aaa", Label: "anthropics/skills"}
	if err := s.SetLibrarySource(src); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetLibrarySource(it.ID)
	if err != nil || got.Repo != "anthropics/skills" || got.Ref != "aaa" || got.CheckedAt != nil {
		t.Fatalf("%+v %v", got, err)
	}
	// upsert
	src.Ref = "bbb"
	if err := s.SetLibrarySource(src); err != nil {
		t.Fatal(err)
	}
	if got, _ = s.GetLibrarySource(it.ID); got.Ref != "bbb" {
		t.Fatalf("%+v", got)
	}
	at := time.Now().UTC().Truncate(time.Second)
	if err := s.MarkSourceChecked(it.ID, "ccc", at); err != nil {
		t.Fatal(err)
	}
	got, _ = s.GetLibrarySource(it.ID)
	if got.Latest != "ccc" || got.CheckedAt == nil || !got.CheckedAt.Equal(at) {
		t.Fatalf("%+v", got)
	}
	if list, err := s.ListLibrarySources(); err != nil || len(list) != 1 {
		t.Fatalf("%v %v", list, err)
	}
	if f, err := s.FindLibrarySource(SourceGitHub, "anthropics/skills", "skills/pdf"); err != nil || f.ItemID != it.ID {
		t.Fatalf("%+v %v", f, err)
	}
	if _, err := s.FindLibrarySource(SourceGitHub, "x/y", "skills/pdf"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
	if err := s.MarkSourceChecked(999, "x", at); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
	// stergerea elementului sterge si sursa
	if _, err := s.db.Exec(`DELETE FROM library_items WHERE id = ?`, it.ID); err != nil {
		t.Fatal(err)
	}
	if list, _ := s.ListLibrarySources(); len(list) != 0 {
		t.Fatalf("sursa a ramas: %+v", list)
	}
}
