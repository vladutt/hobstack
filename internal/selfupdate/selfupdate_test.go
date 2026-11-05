package selfupdate

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestNewer(t *testing.T) {
	cases := []struct {
		latest, current string
		want            bool
	}{
		{"1.1.0", "1.0.0", true},
		{"v1.0.1", "1.0.0", true},
		{"2.0.0", "1.9.9", true},
		{"1.10.0", "1.9.0", true},
		{"1.0.0", "1.0.0", false},
		{"1.0.0", "1.1.0", false},
		{"1.0.0", "1.0.0-3-g657c1cf", false},       // build local dupa tag
		{"1.0.0", "1.0.0-3-g657c1cf-dirty", false}, // la fel, cu modificari
		{"1.0.1", "1.0.0-3-g657c1cf", true},
		{"1.0.0", "1.0.0-rc1", true},
		{"1.1.0-rc1", "1.0.0", false}, // pre-versiunile nu se propun
		{"1.1.0", "dev", false},
		{"junk", "1.0.0", false},
	}
	for _, c := range cases {
		if got := Newer(c.latest, c.current); got != c.want {
			t.Errorf("Newer(%q, %q) = %v, want %v", c.latest, c.current, got, c.want)
		}
	}
}

// fakeGitHub serveste /repos/o/r/releases/latest cu arhiva platformei curente; binarul din arhiva e un
// script care raspunde la "version" cu versiunea data (bin).
type fakeGitHub struct {
	srv       *httptest.Server
	version   string
	bin       []byte
	badSum    bool
	noRelease bool
	downloads atomic.Int32
}

func newFakeGitHub(t *testing.T, version string) *fakeGitHub {
	t.Helper()
	g := &fakeGitHub{version: version, bin: []byte("#!/bin/sh\necho " + version + "\n")}
	g.srv = httptest.NewServer(http.HandlerFunc(g.serve))
	t.Cleanup(g.srv.Close)
	return g
}

func (g *fakeGitHub) archive() []byte {
	var buf bytes.Buffer
	if runtime.GOOS == "windows" {
		zw := zip.NewWriter(&buf)
		w, _ := zw.Create("hobstack.exe")
		w.Write(g.bin)
		zw.Close()
		return buf.Bytes()
	}
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	tw.WriteHeader(&tar.Header{Name: "./README.md", Mode: 0o644, Size: 2, Typeflag: tar.TypeReg})
	tw.Write([]byte("hi"))
	tw.WriteHeader(&tar.Header{Name: "./hobstack", Mode: 0o755, Size: int64(len(g.bin)), Typeflag: tar.TypeReg})
	tw.Write(g.bin)
	tw.Close()
	gz.Close()
	return buf.Bytes()
}

func (g *fakeGitHub) serve(w http.ResponseWriter, r *http.Request) {
	name := archiveName(g.version)
	switch r.URL.Path {
	case "/repos/o/r/releases/latest":
		if g.noRelease {
			http.NotFound(w, r)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{
			"tag_name": "v" + g.version, "name": "HobStack v" + g.version, "body": "- faster board",
			"html_url": "https://github.com/o/r/releases/tag/v" + g.version, "published_at": time.Now().Format(time.RFC3339),
			"assets": []map[string]any{
				{"name": name, "browser_download_url": g.srv.URL + "/dl/" + name},
				{"name": "checksums.txt", "browser_download_url": g.srv.URL + "/dl/checksums.txt"},
			},
		})
	case "/dl/checksums.txt":
		sum := sha256.Sum256(g.archive())
		h := hex.EncodeToString(sum[:])
		if g.badSum {
			h = strings.Repeat("0", 64)
		}
		fmt.Fprintf(w, "%s  hobstack_other.tar.gz\n%s  %s\n", strings.Repeat("1", 64), h, name)
	case "/dl/" + name:
		g.downloads.Add(1)
		w.Write(g.archive())
	default:
		http.NotFound(w, r)
	}
}

type fake struct {
	active    atomic.Int32
	held      atomic.Bool
	restarted chan struct{}
	backups   atomic.Int32
}

func newUpdater(t *testing.T, g *fakeGitHub, current string, f *fake) (*Updater, string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the fake binary is a shell script")
	}
	dir := t.TempDir()
	exe := filepath.Join(dir, "hobstack")
	os.WriteFile(exe, []byte("old"), 0o755)
	f.restarted = make(chan struct{}, 1)
	u := New(exe, Options{Repo: "o/r", API: g.srv.URL, Version: current}, Deps{
		Active:  func() int { return int(f.active.Load()) },
		Hold:    func(on bool) { f.held.Store(on) },
		Backup:  func(path string) error { f.backups.Add(1); return os.WriteFile(path, []byte("db"), 0o644) },
		Restart: func() { f.restarted <- struct{}{} },
		Backups: filepath.Join(dir, "backups"),
	})
	return u, exe
}

func waitState(t *testing.T, u *Updater, states ...string) Status {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if s := u.Status(); slices.Contains(states, s.State) {
			return s
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("state stayed %q (%s), want %v", u.Status().State, u.Status().Error, states)
	return Status{}
}

func waitRestart(t *testing.T, u *Updater, f *fake) {
	t.Helper()
	select {
	case <-f.restarted:
	case <-time.After(10 * time.Second):
		t.Fatalf("no restart: %+v", u.Status())
	}
}

func TestCheckFindsNewRelease(t *testing.T) {
	g := newFakeGitHub(t, "1.1.0")
	u, _ := newUpdater(t, g, "1.0.0", &fake{})
	if err := u.Check(context.Background()); err != nil {
		t.Fatal(err)
	}
	s := u.Status()
	if !s.Available || !s.CanUpdate || s.Latest != "1.1.0" || s.Notes != "- faster board" || s.CheckedAt.IsZero() {
		t.Fatalf("status: %+v", s)
	}

	// la zi: nimic de propus, Start refuza
	u2, _ := newUpdater(t, g, "1.1.0", &fake{})
	u2.Check(context.Background())
	if s := u2.Status(); s.Available {
		t.Fatalf("same version: %+v", s)
	}
	if err := u2.Start(context.Background()); err == nil {
		t.Fatal("Start without a newer version must fail")
	}

	// build local fara versiune: nu propune, dar poate instala la cerere
	u3, _ := newUpdater(t, g, "", &fake{})
	u3.Check(context.Background())
	if s := u3.Status(); !s.Dev || s.Available || !s.CanUpdate {
		t.Fatalf("dev build: %+v", s)
	}
}

func TestCheckWithoutReleases(t *testing.T) {
	g := newFakeGitHub(t, "1.0.0")
	g.noRelease = true
	u, _ := newUpdater(t, g, "1.0.0", &fake{})
	if err := u.Check(context.Background()); err != nil {
		t.Fatal(err)
	}
	if s := u.Status(); s.Latest != "" || s.Available || s.CheckError != "" {
		t.Fatalf("no releases: %+v", s)
	}
}

func TestUpdateWaitsForAgentsThenRestarts(t *testing.T) {
	g := newFakeGitHub(t, "1.1.0")
	f := &fake{}
	f.active.Store(1)
	u, exe := newUpdater(t, g, "1.0.0", f)
	u.Check(context.Background())
	if err := u.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	s := waitState(t, u, StateWaiting, StateFailed)
	if s.State != StateWaiting || s.Waiting != 1 || !f.held.Load() {
		t.Fatalf("with an agent running the update must wait and hold new runs: %+v held=%v", s, f.held.Load())
	}
	if err := u.Start(context.Background()); err != ErrBusy {
		t.Fatalf("second Start: %v", err)
	}
	if got, _ := os.ReadFile(exe); !bytes.Equal(got, g.bin) {
		t.Fatalf("binary: %q", got)
	}
	if got, _ := os.ReadFile(exe + ".prev"); string(got) != "old" {
		t.Fatalf("previous binary: %q", got)
	}
	f.active.Store(0)
	waitRestart(t, u, f)
	if u.Status().State != StateRestarting || f.backups.Load() != 1 {
		t.Fatalf("after restart: %+v backups=%d", u.Status(), f.backups.Load())
	}
}

func TestDamagedDownloadChangesNothing(t *testing.T) {
	g := newFakeGitHub(t, "1.1.0")
	g.badSum = true
	u, exe := newUpdater(t, g, "1.0.0", &fake{})
	u.Check(context.Background())
	u.Start(context.Background())
	s := waitState(t, u, StateFailed, StateRestarting)
	if s.State != StateFailed || !strings.Contains(s.Error, "damaged") {
		t.Fatalf("bad checksum: %+v", s)
	}
	if got, _ := os.ReadFile(exe); string(got) != "old" {
		t.Fatalf("binary changed: %q", got)
	}
}

func TestBrokenBinaryChangesNothing(t *testing.T) {
	g := newFakeGitHub(t, "1.1.0")
	g.bin = []byte("#!/bin/sh\necho 0.9.0\n") // raspunde cu alta versiune
	u, exe := newUpdater(t, g, "1.0.0", &fake{})
	u.Check(context.Background())
	u.Start(context.Background())
	s := waitState(t, u, StateFailed, StateRestarting)
	if s.State != StateFailed || !strings.Contains(s.Error, "0.9.0") {
		t.Fatalf("wrong version inside: %+v", s)
	}
	if got, _ := os.ReadFile(exe); string(got) != "old" {
		t.Fatalf("binary changed: %q", got)
	}
	if _, err := os.Stat(exe + ".new"); err == nil {
		t.Fatal("hobstack.new left behind")
	}
}

func TestCancelWhileWaiting(t *testing.T) {
	g := newFakeGitHub(t, "1.1.0")
	f := &fake{}
	f.active.Store(2)
	u, exe := newUpdater(t, g, "1.0.0", f)
	u.Check(context.Background())
	u.Start(context.Background())
	waitState(t, u, StateWaiting)
	if !u.Cancel() {
		t.Fatal("Cancel while waiting")
	}
	s := waitState(t, u, StateInstalled)
	if f.held.Load() || !strings.Contains(s.Error, "next time") {
		t.Fatalf("after cancel: %+v held=%v", s, f.held.Load())
	}
	// "Restart now": binarul e deja pus, deci doar asteapta si reporneste (fara alt download)
	f.active.Store(0)
	if err := u.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	waitRestart(t, u, f)
	if g.downloads.Load() != 1 {
		t.Fatalf("downloads = %d, want 1", g.downloads.Load())
	}
	if got, _ := os.ReadFile(exe + ".prev"); string(got) != "old" {
		t.Fatalf("the previous binary was replaced: %q", got)
	}
}

func TestCleanupPrevious(t *testing.T) {
	exe := filepath.Join(t.TempDir(), "hobstack")
	os.WriteFile(exe, []byte("now"), 0o755)
	os.WriteFile(exe+".prev", []byte("before"), 0o755)
	CleanupPrevious(exe)
	if _, err := os.Stat(exe + ".prev"); !os.IsNotExist(err) {
		t.Fatal(".prev is still there")
	}
	if b, _ := os.ReadFile(exe); string(b) != "now" {
		t.Fatal("the current binary changed")
	}
}
