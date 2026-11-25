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

func TestEffectiveInstalls(t *testing.T) {
	s := newStore(t)
	p, _ := newProject(t, s)
	off, _ := newProject(t, s)
	def, vs := publishedItem(t, s, KindSkill, "def", "1.0", "1.1")
	s.SetLibraryDefault(def.ID, true)
	noVer, _ := publishedItem(t, s, KindSkill, "nover")
	s.SetLibraryDefault(noVer.ID, true)
	gone, _ := publishedItem(t, s, KindSkill, "gone", "1.0")
	s.SetLibraryDefault(gone.ID, true)
	s.SetLibraryStatus(gone.ID, ItemUnpublished)
	plain, pv := publishedItem(t, s, KindTool, "plain", "1.0")
	s.Install(p.ID, plain.ID, pv[0].ID)

	list, err := s.EffectiveInstalls(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	in, ok := installOf(t, list, def.ID)
	if !ok || in.VersionID != vs[1].ID || !in.Enabled || !in.FromDefault || in.ProjectID != p.ID {
		t.Fatalf("default: %+v %v", in, ok)
	}
	if _, ok := installOf(t, list, noVer.ID); ok {
		t.Fatal("default fara versiuni nu trebuie sa apara")
	}
	if _, ok := installOf(t, list, gone.ID); ok {
		t.Fatal("default unpublished nu trebuie sa apara")
	}
	if in, ok := installOf(t, list, plain.ID); !ok || in.FromDefault || !in.Enabled {
		t.Fatalf("instalare explicita: %+v %v", in, ok)
	}

	if err := s.DisableInstall(off.ID, def.ID, vs[1].ID); err != nil {
		t.Fatal(err)
	}
	list, _ = s.EffectiveInstalls(off.ID)
	if in, ok := installOf(t, list, def.ID); !ok || in.Enabled || in.FromDefault {
		t.Fatalf("oprit: %+v %v", in, ok)
	}
}

func TestInstallKeepsDisabled(t *testing.T) {
	s := newStore(t)
	p, _ := newProject(t, s)
	it, vs := publishedItem(t, s, KindSkill, "plain", "1.0", "1.1")
	s.DisableInstall(p.ID, it.ID, vs[0].ID)
	if err := s.Install(p.ID, it.ID, vs[1].ID); err != nil {
		t.Fatal(err)
	}
	l, _ := s.ListInstalls(p.ID)
	if in, _ := installOf(t, l, it.ID); in.Enabled || in.VersionID != vs[1].ID {
		t.Fatalf("ne-default: %+v", in)
	}

	def, dv := publishedItem(t, s, KindSkill, "def", "1.0", "1.1")
	s.SetLibraryDefault(def.ID, true)
	s.DisableInstall(p.ID, def.ID, dv[0].ID)
	if err := s.Install(p.ID, def.ID, dv[1].ID); err != nil {
		t.Fatal(err)
	}
	l, _ = s.ListInstalls(p.ID)
	if in, _ := installOf(t, l, def.ID); in.Enabled || in.VersionID != dv[0].ID {
		t.Fatalf("default oprit atins de Install: %+v", in)
	}
}

func TestDefaultToggleKeepsOptOut(t *testing.T) {
	s := newStore(t)
	p, _ := newProject(t, s)
	it, vs := publishedItem(t, s, KindSkill, "def", "1.0")
	s.SetLibraryDefault(it.ID, true)
	s.DisableInstall(p.ID, it.ID, vs[0].ID)
	s.SetLibraryDefault(it.ID, false)
	s.SetLibraryDefault(it.ID, true)
	l, _ := s.EffectiveInstalls(p.ID)
	if in, ok := installOf(t, l, it.ID); !ok || in.Enabled {
		t.Fatalf("%+v %v", in, ok)
	}
}

func TestInstallWithSecretsOnDefault(t *testing.T) {
	s := newStore(t)
	p, _ := newProject(t, s)
	it, vs := publishedItem(t, s, KindMCP, "gh", "1.0")
	s.SetLibraryDefault(it.ID, true)
	s.DisableInstall(p.ID, it.ID, vs[0].ID)
	if err := s.InstallWithSecrets(p.ID, it.ID, vs[0].ID, map[string]string{"TOKEN": "p"}); err != nil {
		t.Fatal(err)
	}
	if l, _ := s.ListInstalls(p.ID); len(l) != 0 {
		t.Fatalf("randul de oprire trebuia sters: %+v", l)
	}
	l, _ := s.EffectiveInstalls(p.ID)
	if in, ok := installOf(t, l, it.ID); !ok || !in.FromDefault {
		t.Fatalf("%+v %v", in, ok)
	}
	if sec, _ := s.Secrets(p.ID, it.ID); sec["TOKEN"] != "p" {
		t.Fatalf("%v", sec)
	}
}

func TestEffectiveSecrets(t *testing.T) {
	s := newStore(t)
	p, _ := newProject(t, s)
	it, _ := publishedItem(t, s, KindMCP, "gh", "1.0")
	s.SetDefaultSecret(it.ID, "TOKEN", "g")
	s.SetDefaultSecret(it.ID, "OTHER", "o")
	s.SetSecret(p.ID, it.ID, "TOKEN", "p")
	s.SetSecret(p.ID, it.ID, "OTHER", "")
	got, err := s.EffectiveSecrets(p.ID, it.ID)
	if err != nil || got["TOKEN"] != "p" || got["OTHER"] != "o" || len(got) != 2 {
		t.Fatalf("%v %v", got, err)
	}
}
