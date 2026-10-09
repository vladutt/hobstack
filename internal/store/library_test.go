package store

import (
	"errors"
	"testing"
)

func TestLibraryLifecycle(t *testing.T) {
	s := newStore(t)
	p, lead := newProject(t, s)
	other, _ := newProject(t, s)
	it := LibraryItem{Kind: KindTool, Name: "pdf-offer", Description: "Builds a PDF offer", Status: ItemProposed,
		SourceProjectID: p.ID, MadeByAgentID: lead.ID, MadeBy: "Lead", Needs: []string{"write"}, Secrets: []string{"BRAND_KEY"}, Entry: "pdf-offer.sh"}
	if err := s.AddLibraryItem(&it); err != nil {
		t.Fatal(err)
	}
	dup := it
	dup.ID = 0
	if err := s.AddLibraryItem(&dup); err == nil {
		t.Fatal("acelasi nume si kind de doua ori")
	}
	if n, _ := s.CountOpenProposals(p.ID); n != 1 {
		t.Fatalf("propuneri deschise = %d", n)
	}
	v := LibraryVersion{ItemID: it.ID, Version: "1.0", Dir: "/x/1.0", Files: 2, Lines: 40}
	if err := s.AddLibraryVersion(&v); err != nil {
		t.Fatal(err)
	}
	if err := s.SetLibraryStatus(it.ID, ItemPublished); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetLibraryItem(it.ID)
	if got.Status != ItemPublished || got.PublishedAt == nil || got.Secrets[0] != "BRAND_KEY" {
		t.Fatalf("%+v", got)
	}
	if err := s.Install(other.ID, it.ID, v.ID); err != nil {
		t.Fatal(err)
	}
	s.SetSecret(other.ID, it.ID, "BRAND_KEY", "s3cret")
	if sec, _ := s.Secrets(other.ID, it.ID); sec["BRAND_KEY"] != "s3cret" {
		t.Fatalf("%v", sec)
	}
	// unpublish: instalarea ramane
	s.SetLibraryStatus(it.ID, ItemUnpublished)
	if ins, _ := s.ListInstalls(other.ID); len(ins) != 1 || ins[0].VersionID != v.ID {
		t.Fatalf("%+v", ins)
	}
	if list, _ := s.ListLibraryItems(KindTool); len(list) != 0 { // unpublished dispare din lista
		t.Fatalf("%+v", list)
	}
	s.Uninstall(other.ID, it.ID)
	if ins, _ := s.ListInstalls(other.ID); len(ins) != 0 {
		t.Fatal("dezinstalarea nu a mers")
	}
}

func TestFindLibraryItemNotFound(t *testing.T) {
	s := newStore(t)
	if _, err := s.FindLibraryItem(KindTool, "nu-exista"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
	it := LibraryItem{Kind: KindSkill, Name: "x", Status: ItemProposed}
	if err := s.AddLibraryItem(&it); err != nil {
		t.Fatal(err)
	}
	if got, err := s.FindLibraryItem(KindSkill, "x"); err != nil || got.ID != it.ID {
		t.Fatalf("%+v %v", got, err)
	}
	// acelasi nume, alt kind: nu se gaseste
	if _, err := s.FindLibraryItem(KindTool, "x"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
	if _, err := s.GetLibraryItem(9999); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
}

func TestLibraryEmptyListsAreNonNil(t *testing.T) {
	s := newStore(t)
	it := LibraryItem{Kind: KindMCP, Name: "m", Status: ItemProposed}
	if err := s.AddLibraryItem(&it); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetLibraryItem(it.ID)
	if got.Needs == nil || got.Secrets == nil || got.Args == nil || got.ToolNames == nil {
		t.Fatalf("liste nil: %+v", got)
	}
}

func TestRecentLibraryUsesOrderAndLimit(t *testing.T) {
	s := newStore(t)
	p, lead := newProject(t, s)
	it := LibraryItem{Kind: KindTool, Name: "t", Status: ItemPublished}
	if err := s.AddLibraryItem(&it); err != nil {
		t.Fatal(err)
	}
	var ids []int64
	for _, title := range []string{"a", "b", "c"} {
		task := Task{ProjectID: p.ID, Title: title, AssigneeID: lead.ID}
		if err := s.CreateTask(&task); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, task.ID)
		if err := s.AddLibraryUse(it.ID, task.ID, lead.ID); err != nil {
			t.Fatal(err)
		}
	}
	// timpuri distincte, ca ordinea sa nu depinda de rezolutia ceasului
	for i, id := range ids {
		if _, err := s.db.Exec(`UPDATE library_uses SET at = ? WHERE task_id = ?`,
			"2026-10-0"+string(rune('1'+i))+"T10:00:00Z", id); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.RecentLibraryUses(it.ID, 2)
	if err != nil || len(got) != 2 {
		t.Fatalf("%+v %v", got, err)
	}
	if got[0].Title != "c" || got[1].Title != "b" || got[0].Agent != lead.Name || got[0].Number != 3 || got[0].TaskID != ids[2] {
		t.Fatalf("%+v", got)
	}
	if none, err := s.RecentLibraryUses(9999, 5); err != nil || len(none) != 0 {
		t.Fatalf("%+v %v", none, err)
	}
}

func TestSecretsEmptyAndPublishedAtOnce(t *testing.T) {
	s := newStore(t)
	p, _ := newProject(t, s)
	it := LibraryItem{Kind: KindTool, Name: "t", Status: ItemProposed}
	if err := s.AddLibraryItem(&it); err != nil {
		t.Fatal(err)
	}
	sec, err := s.Secrets(p.ID, it.ID)
	if err != nil || sec == nil || len(sec) != 0 {
		t.Fatalf("%v %v", sec, err)
	}
	if err := s.SetLibraryStatus(it.ID, ItemPublished); err != nil {
		t.Fatal(err)
	}
	first, _ := s.GetLibraryItem(it.ID)
	s.SetLibraryStatus(it.ID, ItemUnpublished)
	s.SetLibraryStatus(it.ID, ItemPublished)
	again, _ := s.GetLibraryItem(it.ID)
	if first.PublishedAt == nil || again.PublishedAt == nil || !first.PublishedAt.Equal(*again.PublishedAt) {
		t.Fatalf("published_at s-a schimbat: %v -> %v", first.PublishedAt, again.PublishedAt)
	}
	if err := s.SetLibraryStatus(9999, ItemPublished); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
}

func TestListLibraryItemsAnyStatus(t *testing.T) {
	s := newStore(t)
	for _, st := range []string{ItemPublished, ItemDeclined, ItemUnpublished, ItemProposed} {
		it := LibraryItem{Kind: KindMCP, Name: "m-" + st, Status: st, Transport: "local", Command: "npx"}
		if err := s.AddLibraryItem(&it); err != nil {
			t.Fatal(err)
		}
	}
	other := LibraryItem{Kind: KindSkill, Name: "s", Status: ItemDeclined}
	if err := s.AddLibraryItem(&other); err != nil {
		t.Fatal(err)
	}
	all, err := s.ListLibraryItemsAnyStatus(KindMCP)
	if err != nil || len(all) != 4 {
		t.Fatalf("%+v %v", all, err)
	}
}

func TestInstallWithSecretsAtomic(t *testing.T) {
	s := newStore(t)
	p, _ := newProject(t, s)
	it := LibraryItem{Kind: KindMCP, Name: "m", Status: ItemPublished, Secrets: []string{"A_KEY", "B_KEY"}}
	if err := s.AddLibraryItem(&it); err != nil {
		t.Fatal(err)
	}
	// versiune inexistenta: cheia straina pica, nimic salvat (nici secretele)
	if err := s.InstallWithSecrets(p.ID, it.ID, 9999, map[string]string{"A_KEY": "a"}); err == nil {
		t.Fatal("versiune inexistenta acceptata")
	}
	if ins, _ := s.ListInstalls(p.ID); len(ins) != 0 {
		t.Fatalf("instalat: %+v", ins)
	}
	if sec, _ := s.Secrets(p.ID, it.ID); len(sec) != 0 {
		t.Fatalf("secrete ramase: %v", sec)
	}
	v := LibraryVersion{ItemID: it.ID, Version: "1.0", Dir: "/x"}
	if err := s.AddLibraryVersion(&v); err != nil {
		t.Fatal(err)
	}
	if err := s.InstallWithSecrets(p.ID, it.ID, v.ID, map[string]string{"A_KEY": "a", "B_KEY": "b"}); err != nil {
		t.Fatal(err)
	}
	ins, _ := s.ListInstalls(p.ID)
	sec, _ := s.Secrets(p.ID, it.ID)
	if len(ins) != 1 || !ins[0].Enabled || ins[0].VersionID != v.ID || sec["A_KEY"] != "a" || sec["B_KEY"] != "b" {
		t.Fatalf("%+v %v", ins, sec)
	}
}
