package library

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"testing"

	"hobstack/internal/store"
)

// stagedSkill scrie fisierele intr-un folder de lucru (CopyStaged), ca la Update.
func stagedSkill(t *testing.T, lib *Lib, files map[string]string) string {
	t.Helper()
	src := t.TempDir()
	var names []string
	for n, body := range files {
		p := filepath.Join(src, n)
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte(body), 0o644)
		names = append(names, n)
	}
	staging, _, _, err := lib.CopyStaged(store.KindSkill, src, names)
	if err != nil {
		t.Fatal(err)
	}
	return staging
}

func filesIn(t *testing.T, dir string) []string {
	t.Helper()
	var out []string
	filepath.Walk(dir, func(p string, fi os.FileInfo, err error) error {
		if err == nil && !fi.IsDir() {
			rel, _ := filepath.Rel(dir, p)
			out = append(out, filepath.ToSlash(rel))
		}
		return nil
	})
	sort.Strings(out)
	return out
}

// publishedSkill: un skill publicat 1.0 cu SKILL.md + old.txt.
func publishedSkill(t *testing.T, st *store.Store, lib *Lib, name string) store.LibraryItem {
	t.Helper()
	it := addItem(t, st, store.KindSkill, name)
	if _, err := lib.AdoptStaged(stagedSkill(t, lib, map[string]string{"SKILL.md": "v1", "old.txt": "x"}), it.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := lib.Publish(it.ID, ""); err != nil {
		t.Fatal(err)
	}
	return it
}

func TestPublishReplace(t *testing.T) {
	st, lib, _ := newEnv(t)
	it := publishedSkill(t, st, lib, "rep")
	lib.AdoptStaged(stagedSkill(t, lib, map[string]string{"SKILL.md": "v2"}), it.ID)
	v, err := lib.PublishReplace(it.ID, "n")
	if err != nil {
		t.Fatal(err)
	}
	if got := filesIn(t, v.Dir); len(got) != 1 || got[0] != "SKILL.md" || v.Version != "1.1" {
		t.Fatalf("replace: %v %s", got, v.Version)
	}
	// Publish-ul obisnuit pastreaza fisierele versiunii precedente (neschimbat)
	other := publishedSkill(t, st, lib, "ovl")
	lib.AdoptStaged(stagedSkill(t, lib, map[string]string{"SKILL.md": "v2"}), other.ID)
	v2, err := lib.PublishSkipTest(other.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if got := filesIn(t, v2.Dir); len(got) != 2 {
		t.Fatalf("overlay: %v", got)
	}
}

func TestPublishReplaceStaged(t *testing.T) {
	st, lib, _ := newEnv(t)
	it := publishedSkill(t, st, lib, "stg")
	// doua update-uri concurente: fiecare versiune are exact fisierele unuia
	sets := []map[string]string{{"SKILL.md": "a", "a.txt": "a"}, {"SKILL.md": "b", "b.txt": "b"}}
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := range sets {
		staging := stagedSkill(t, lib, sets[i])
		wg.Add(1)
		go func() { defer wg.Done(); _, errs[i] = lib.PublishReplaceStaged(it.ID, staging, "u") }()
	}
	wg.Wait()
	if errs[0] != nil || errs[1] != nil {
		t.Fatalf("errs %v", errs)
	}
	vs, _ := st.ListLibraryVersions(it.ID)
	if len(vs) != 3 {
		t.Fatalf("versiuni %d", len(vs))
	}
	for _, v := range vs[:2] {
		got := filesIn(t, v.Dir)
		if len(got) != 2 || (got[1] != "a.txt" && got[1] != "b.txt") || got[0] != "SKILL.md" {
			t.Fatalf("%s: %v", v.Version, got)
		}
	}
	// un element care nu e publicat nu primeste update
	st.SetLibraryStatus(it.ID, store.ItemUnpublished)
	staging := stagedSkill(t, lib, map[string]string{"SKILL.md": "c"})
	if _, err := lib.PublishReplaceStaged(it.ID, staging, "u"); !errors.Is(err, ErrStatus) {
		t.Fatalf("unpublished: %v", err)
	}
	if _, err := os.Stat(lib.ProposalDir(it.ID)); !os.IsNotExist(err) {
		t.Fatal("propunerea a ramas")
	}
}
