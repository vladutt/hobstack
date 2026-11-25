package store

import "testing"

// publishedItem adauga un element publicat cu versiunile date (in ordinea data).
func publishedItem(t *testing.T, s *Store, kind, name string, versions ...string) (LibraryItem, []LibraryVersion) {
	t.Helper()
	it := LibraryItem{Kind: kind, Name: name, Description: "d", Status: ItemPublished, Secrets: []string{"TOKEN", "OTHER"}}
	if err := s.AddLibraryItem(&it); err != nil {
		t.Fatal(err)
	}
	var vs []LibraryVersion
	for _, v := range versions {
		lv := LibraryVersion{ItemID: it.ID, Version: v, Dir: "/x/" + v}
		if err := s.AddLibraryVersion(&lv); err != nil {
			t.Fatal(err)
		}
		vs = append(vs, lv)
	}
	return it, vs
}

func installOf(t *testing.T, list []LibraryInstall, itemID int64) (LibraryInstall, bool) {
	t.Helper()
	for _, in := range list {
		if in.ItemID == itemID {
			return in, true
		}
	}
	return LibraryInstall{}, false
}

func TestSetLibraryDefault(t *testing.T) {
	s := newStore(t)
	a, _ := newProject(t, s)
	b, _ := newProject(t, s)
	it, vs := publishedItem(t, s, KindSkill, "ro-copy", "1.0")
	s.Install(a.ID, it.ID, vs[0].ID)
	s.Install(b.ID, it.ID, vs[0].ID)
	if _, err := s.db.Exec(`UPDATE library_installs SET enabled = 0 WHERE project_id = ?`, b.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.SetLibraryDefault(it.ID, true); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.GetLibraryItem(it.ID); !got.IsDefault {
		t.Fatal("IsDefault ar trebui sa fie true")
	}
	la, _ := s.ListInstalls(a.ID)
	if _, ok := installOf(t, la, it.ID); ok {
		t.Fatal("randul pornit din A trebuia sters (A urmeaza default-ul)")
	}
	lb, _ := s.ListInstalls(b.ID)
	if in, ok := installOf(t, lb, it.ID); !ok || in.Enabled {
		t.Fatalf("randul oprit din B trebuia pastrat: %+v %v", in, ok)
	}
	if n, err := s.DefaultOffCount(it.ID); err != nil || n != 1 {
		t.Fatalf("off = %d %v", n, err)
	}
	if err := s.SetLibraryDefault(it.ID, false); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.GetLibraryItem(it.ID); got.IsDefault {
		t.Fatal("IsDefault ar trebui sa fie false")
	}
	lb, _ = s.ListInstalls(b.ID)
	if _, ok := installOf(t, lb, it.ID); !ok {
		t.Fatal("randul oprit din B dispare la scoaterea flag-ului")
	}
}

func TestDefaultSecrets(t *testing.T) {
	s := newStore(t)
	it, _ := publishedItem(t, s, KindMCP, "gh", "1.0")
	if err := s.SetDefaultSecret(it.ID, "TOKEN", "x"); err != nil {
		t.Fatal(err)
	}
	if m, _ := s.DefaultSecrets(it.ID); m["TOKEN"] != "x" {
		t.Fatalf("%v", m)
	}
	if err := s.SetDefaultSecret(it.ID, "TOKEN", ""); err != nil {
		t.Fatal(err)
	}
	if m, err := s.DefaultSecrets(it.ID); err != nil || m == nil || len(m) != 0 {
		t.Fatalf("%v %v", m, err)
	}
}
