package skillsrc

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"hobstack/internal/store"
)

// addSrc creeaza un skill publicat cu sursa data si intoarce id-ul.
func addSrc(t *testing.T, st *store.Store, name string, src store.LibrarySource) int64 {
	t.Helper()
	it := store.LibraryItem{Kind: store.KindSkill, Name: name, Description: "d", Status: store.ItemPublished, MadeBy: "You"}
	if err := st.AddLibraryItem(&it); err != nil {
		t.Fatal(err)
	}
	src.ItemID = it.ID
	if err := st.SetLibrarySource(src); err != nil {
		t.Fatal(err)
	}
	return it.ID
}

func TestCheckSources(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	calls := 0
	r := testRemote(t, func(w http.ResponseWriter, req *http.Request) {
		calls++
		w.Write([]byte(`[{"sha":"abcdef1"}]`))
	})
	now := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)

	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("---\nname: loc\n---\nx"), 0o644)
	gh := addSrc(t, st, "gh", store.LibrarySource{Kind: store.SourceGitHub, Repo: "o/s", Path: "skills/gh", Ref: "old"})
	loc := addSrc(t, st, "loc", store.LibrarySource{Kind: store.SourceClaudeCode, Path: dir, Ref: "old"})
	fresh := addSrc(t, st, "fresh", store.LibrarySource{Kind: store.SourceGitHub, Repo: "o/s", Path: "skills/fresh"})
	if err := st.MarkSourceChecked(fresh, "x", now.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}

	if err := CheckSources(context.Background(), st, r, now); err != nil {
		t.Fatal(err)
	}
	g, _ := st.GetLibrarySource(gh)
	if g.Latest != "abcdef1" || g.CheckedAt == nil {
		t.Errorf("github: %+v", g)
	}
	l, _ := st.GetLibrarySource(loc)
	if l.Latest == "" || l.Latest == "old" || l.CheckedAt == nil {
		t.Errorf("local: %+v", l)
	}
	f, _ := st.GetLibrarySource(fresh)
	if f.Latest != "x" || calls != 1 {
		t.Errorf("verificata acum o ora trebuia sarita: %+v, apeluri %d", f, calls)
	}

	// peste 24 h se verifica din nou
	if err := CheckSources(context.Background(), st, r, now.Add(25*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if f, _ = st.GetLibrarySource(fresh); f.Latest != "abcdef1" {
		t.Errorf("trebuia verificata: %+v", f)
	}
}

func TestCheckSourcesRateLimited(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	calls := 0
	r := testRemote(t, func(w http.ResponseWriter, req *http.Request) {
		calls++
		w.Header().Set("X-RateLimit-Remaining", "0")
		w.WriteHeader(http.StatusForbidden)
	})
	a := addSrc(t, st, "a", store.LibrarySource{Kind: store.SourceGitHub, Repo: "o/s", Path: "a"})
	b := addSrc(t, st, "b", store.LibrarySource{Kind: store.SourceGitHub, Repo: "o/s", Path: "b"})
	if err := CheckSources(context.Background(), st, r, time.Now()); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Errorf("la limita bucla se opreste: %d apeluri", calls)
	}
	for _, id := range []int64{a, b} {
		if s, _ := st.GetLibrarySource(id); s.CheckedAt != nil {
			t.Errorf("%d nu trebuia marcata: %+v", id, s)
		}
	}
}
