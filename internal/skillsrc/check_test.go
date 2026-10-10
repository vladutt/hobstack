package skillsrc

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"hobstack/internal/store"
)

func TestCheckSourceGitHub(t *testing.T) {
	sha, limited := "aaaaaaa1", false
	var gotPath string
	r := testRemote(t, func(w http.ResponseWriter, req *http.Request) {
		if limited {
			w.Header().Set("X-RateLimit-Remaining", "0")
			w.WriteHeader(http.StatusForbidden)
			return
		}
		if req.URL.Path == "/api/repos/o/skills/commits" {
			gotPath = req.URL.Query().Get("path")
			if req.URL.Query().Get("sha") != "" {
				t.Errorf("verificarea trebuie sa ceara ramura implicita, nu %q", req.URL.Query().Get("sha"))
			}
			w.Write([]byte(`[{"sha":"` + sha + `"}]`))
			return
		}
		http.NotFound(w, req)
	})
	src := store.LibrarySource{Kind: store.SourceGitHub, Repo: "o/skills", Path: "skills/pdf", Ref: "aaaaaaa1"}
	if got, err := CheckSource(context.Background(), r, src); err != nil || got != "aaaaaaa1" || gotPath != "skills/pdf" {
		t.Fatalf("%q %v %q", got, err, gotPath)
	}
	sha = "bbbbbbb2" // commit nou
	if got, err := CheckSource(context.Background(), r, src); err != nil || got != "bbbbbbb2" {
		t.Fatalf("%q %v", got, err)
	}
	limited = true
	if _, err := CheckSource(context.Background(), r, src); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("limita: %v", err)
	}
}

func TestCheckSourceLocal(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "mine")
	writeFile(t, filepath.Join(dir, "SKILL.md"), "v1")
	src := store.LibrarySource{Kind: store.SourceClaudeCode, Path: dir}
	a, err := CheckSource(context.Background(), nil, src)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "SKILL.md"), "v2")
	b, err := CheckSource(context.Background(), nil, src)
	if err != nil || a == b {
		t.Fatalf("schimbat: %q %q %v", a, b, err)
	}
	os.RemoveAll(dir)
	_, err = CheckSource(context.Background(), nil, src)
	if !errors.Is(err, ErrNotFound) || err.Error() != "Source no longer on this machine." {
		t.Fatalf("disparut: %v", err)
	}
}

func TestPathCommitAt(t *testing.T) {
	var gotSHA string
	r := testRemote(t, func(w http.ResponseWriter, req *http.Request) {
		gotSHA = req.URL.Query().Get("sha")
		w.Write([]byte(`[{"sha":"ccccccc3"}]`))
	})
	if got, err := r.PathCommitAt(context.Background(), "o/skills", "skills/pdf", "1ed29a0"); err != nil || got != "ccccccc3" || gotSHA != "1ed29a0" {
		t.Fatalf("%q %v %q", got, err, gotSHA)
	}
	if _, err := r.PathCommitAt(context.Background(), "o/skills", "skills/pdf", "bad sha!"); err == nil {
		t.Fatal("sha invalid acceptat")
	}
}
