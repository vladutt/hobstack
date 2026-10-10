package skillsrc

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// testRemote porneste un server de test si un Remote cu toate URL-urile catre el.
func testRemote(t *testing.T, h http.HandlerFunc) *Remote {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	r := NewRemote()
	r.SkillsURL = srv.URL + "/sk"
	r.APIURL = srv.URL + "/api"
	r.CodeloadURL = srv.URL + "/cl"
	r.RawURL = srv.URL + "/raw"
	return r
}

func TestNewRemoteEnv(t *testing.T) {
	t.Setenv("HOBSTACK_SKILLS_URL", "http://127.0.0.1:9/")
	t.Setenv("HOBSTACK_GITHUB_API_URL", "ftp://x")
	t.Setenv("HOBSTACK_GITHUB_CODELOAD_URL", "http://")
	t.Setenv("HOBSTACK_GITHUB_RAW_URL", "")
	r := NewRemote()
	if r.SkillsURL != "http://127.0.0.1:9" {
		t.Errorf("SkillsURL = %q", r.SkillsURL)
	}
	if r.APIURL != "https://api.github.com" || r.CodeloadURL != "https://codeload.github.com" ||
		r.RawURL != "https://raw.githubusercontent.com" {
		t.Errorf("defaults not used: %+v", r)
	}
	if r.HTTP == nil || r.HTTP.Timeout.Seconds() != 30 {
		t.Errorf("timeout: %+v", r.HTTP)
	}
	// arhiva: fara timeout pe tot corpul, doar pe header-ele raspunsului
	fc := r.fetchClient()
	tr, ok := fc.Transport.(*http.Transport)
	if fc.Timeout != 0 || !ok || tr.ResponseHeaderTimeout.Seconds() != 30 {
		t.Errorf("fetch client: timeout %v transport %+v", fc.Timeout, fc.Transport)
	}
}

func TestSearch(t *testing.T) {
	var calls atomic.Int32
	r := testRemote(t, func(w http.ResponseWriter, req *http.Request) {
		calls.Add(1)
		if req.Method != http.MethodGet || req.URL.Path != "/sk/api/search" || req.URL.Query().Get("q") != "pdf" {
			t.Errorf("bad request %s %s", req.Method, req.URL)
		}
		if req.Header.Get("User-Agent") != "HobStack" || req.Header.Get("Authorization") != "" {
			t.Errorf("headers: %v", req.Header)
		}
		w.Write([]byte(`{"query":"pdf","skills":[
			{"id":"someone/tools/pdf","source":"someone/tools","skillId":"pdf","name":"pdf","installs":10},
			{"id":"anthropics/skills/pdf","source":"anthropics/skills","skillId":"pdf","name":"pdf","installs":208236},
			{"id":"bad/../x","source":"bad/..","skillId":"x","name":"x","installs":1},
			{"id":"other/r/pdf-kit","source":"other/r","skillId":"pdf-kit","name":"pdf kit","installs":5}
		],"count":4}`))
	})
	hits, err := r.Search(context.Background(), "pdf")
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 3 {
		t.Fatalf("hits = %+v", hits)
	}
	if !hits[0].Official || hits[0].Source != "anthropics/skills" || hits[0].Installs != 208236 || hits[0].SkillID != "pdf" {
		t.Errorf("first = %+v", hits[0])
	}
	if hits[1].Source != "someone/tools" || hits[1].Official || hits[2].SkillID != "pdf-kit" {
		t.Errorf("order = %+v", hits)
	}
	// a doua cerere identica vine din cache, ca o copie
	hits[0].Name = "changed"
	again, err := r.Search(context.Background(), "pdf")
	if err != nil || calls.Load() != 1 {
		t.Errorf("cache: calls = %d err = %v", calls.Load(), err)
	}
	if again[0].Name != "pdf" {
		t.Errorf("cache shared with caller: %+v", again[0])
	}
	// un caracter: fara cerere
	if h, err := r.Search(context.Background(), " p "); h != nil || err != nil || calls.Load() != 1 {
		t.Errorf("short q: %v %v calls %d", h, err, calls.Load())
	}
}

func TestSearchErrors(t *testing.T) {
	r := testRemote(t, func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Query().Get("q") == "bad" {
			w.Write([]byte(`not json`))
			return
		}
		http.Error(w, "boom", 500)
	})
	if _, err := r.Search(context.Background(), "pdf"); !errors.Is(err, ErrUnavailable) {
		t.Errorf("500: %v", err)
	}
	if _, err := r.Search(context.Background(), "bad"); !errors.Is(err, ErrUnavailable) {
		t.Errorf("bad json: %v", err)
	}
	r.SkillsURL = "http://127.0.0.1:1"
	if _, err := r.Search(context.Background(), "xyz"); !errors.Is(err, ErrUnavailable) {
		t.Errorf("network: %v", err)
	}
}

func TestOfficial(t *testing.T) {
	var calls atomic.Int32
	r := testRemote(t, func(w http.ResponseWriter, req *http.Request) {
		calls.Add(1)
		switch req.URL.Path {
		case "/api/repos/anthropics/skills/contents/skills":
			w.Write([]byte(`[{"name":"pdf","path":"skills/pdf","type":"dir"},
				{"name":"README.md","path":"skills/README.md","type":"file"},
				{"name":"docx","path":"skills/docx","type":"dir"}]`))
		case "/raw/anthropics/skills/HEAD/skills/pdf/SKILL.md":
			w.Write([]byte("---\nname: pdf\ndescription: \"Work with PDF files\"\n---\n# PDF\n"))
		case "/raw/anthropics/skills/HEAD/skills/docx/SKILL.md":
			w.Write([]byte("---\nname: docx\ndescription: Word documents\n---\n"))
		default:
			http.NotFound(w, req)
		}
	})
	list, err := r.Official(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || list[0].Name != "docx" || list[0].Description != "Word documents" ||
		list[1].Name != "pdf" || list[1].Description != "Work with PDF files" || list[1].Path != "skills/pdf" {
		t.Fatalf("list = %+v", list)
	}
	n := calls.Load()
	list[0].Description = "changed"
	again, err := r.Official(context.Background())
	if err != nil || calls.Load() != n {
		t.Errorf("cache: %d -> %d", n, calls.Load())
	}
	if again[0].Description != "Word documents" {
		t.Errorf("cache shared with caller: %+v", again[0])
	}
}

func TestOfficialNotCachedOnFailure(t *testing.T) {
	var rawCalls atomic.Int32
	r := testRemote(t, func(w http.ResponseWriter, req *http.Request) {
		switch req.URL.Path {
		case "/api/repos/anthropics/skills/contents/skills":
			w.Write([]byte(`[{"name":"pdf","path":"skills/pdf","type":"dir"}]`))
		case "/raw/anthropics/skills/HEAD/skills/pdf/SKILL.md":
			if rawCalls.Add(1) == 1 {
				http.Error(w, "boom", 500)
				return
			}
			w.Write([]byte("---\ndescription: PDFs\n---\n"))
		}
	})
	list, err := r.Official(context.Background())
	if err != nil || len(list) != 1 || list[0].Description != "" {
		t.Fatalf("first: %+v %v", list, err)
	}
	list, err = r.Official(context.Background())
	if err != nil || list[0].Description != "PDFs" || rawCalls.Load() != 2 {
		t.Errorf("second: %+v %v raw calls %d", list, err, rawCalls.Load())
	}
}

func TestCommits(t *testing.T) {
	sha := strings.Repeat("ab", 20)
	r := testRemote(t, func(w http.ResponseWriter, req *http.Request) {
		switch {
		case req.URL.Path == "/api/repos/o/r/commits/HEAD":
			w.Write([]byte(`{"sha":"` + sha + `"}`))
		case req.URL.Path == "/api/repos/o/r/commits" && req.URL.Query().Get("path") == "skills/pdf" &&
			req.URL.Query().Get("per_page") == "1":
			w.Write([]byte(`[{"sha":"1ed29a0"}]`))
		case req.URL.Path == "/api/repos/o/r/commits":
			w.Write([]byte(`[]`))
		case req.URL.Path == "/api/repos/limit/r/commits/HEAD":
			w.Header().Set("X-RateLimit-Remaining", "0")
			http.Error(w, "rate", 403)
		case req.URL.Path == "/api/repos/many/r/commits/HEAD":
			http.Error(w, "slow down", 429)
		case req.URL.Path == "/api/repos/retry/r/commits/HEAD":
			w.Header().Set("Retry-After", "60")
			http.Error(w, "secondary limit", 403)
		case req.URL.Path == "/api/repos/forbid/r/commits/HEAD":
			w.Header().Set("X-RateLimit-Remaining", "12")
			http.Error(w, "no", 403)
		default:
			http.NotFound(w, req)
		}
	})
	ctx := context.Background()
	if got, err := r.HeadCommit(ctx, "o/r"); err != nil || got != sha {
		t.Errorf("HeadCommit = %q %v", got, err)
	}
	if got, err := r.PathCommit(ctx, "o/r", "skills/pdf"); err != nil || got != "1ed29a0" {
		t.Errorf("PathCommit = %q %v", got, err)
	}
	if _, err := r.PathCommit(ctx, "o/r", "nothing"); !errors.Is(err, ErrNotFound) {
		t.Errorf("empty commits: %v", err)
	}
	if _, err := r.HeadCommit(ctx, "limit/r"); !errors.Is(err, ErrRateLimited) {
		t.Errorf("403 limit: %v", err)
	}
	if _, err := r.HeadCommit(ctx, "many/r"); !errors.Is(err, ErrRateLimited) {
		t.Errorf("429: %v", err)
	}
	if _, err := r.HeadCommit(ctx, "retry/r"); !errors.Is(err, ErrRateLimited) {
		t.Errorf("403 Retry-After: %v", err)
	}
	if _, err := r.HeadCommit(ctx, "forbid/r"); errors.Is(err, ErrRateLimited) || err == nil {
		t.Errorf("403 without limit: %v", err)
	}
	if _, err := r.HeadCommit(ctx, "../x"); err == nil {
		t.Error("invalid repo accepted")
	}
}

func TestValid(t *testing.T) {
	for _, s := range []string{"anthropics/skills", "a.b/c_d-e", "A9/b"} {
		if !ValidRepo(s) {
			t.Errorf("ValidRepo(%q) = false", s)
		}
	}
	for _, s := range []string{"", "a", "a/b/c", "../b", "a/..", "a/b c", "a/", "/b", "a/b%2f", "a..b/c"} {
		if ValidRepo(s) {
			t.Errorf("ValidRepo(%q) = true", s)
		}
	}
	for _, s := range []string{"pdf", "web-artifacts.builder_2"} {
		if !ValidSkillID(s) {
			t.Errorf("ValidSkillID(%q) = false", s)
		}
	}
	for _, s := range []string{"", ".", "..", "a/b", "a b", "a..b"} {
		if ValidSkillID(s) {
			t.Errorf("ValidSkillID(%q) = true", s)
		}
	}
}
