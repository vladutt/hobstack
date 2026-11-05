package selfupdate

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// repo pregateste un "remote" (bare) si o clona cu go.mod si binarul "hobstack" (continut "old").
// Intoarce clona si o functie care adauga un commit pe remote.
func repo(t *testing.T) (src string, push func(msg string)) {
	t.Helper()
	root := t.TempDir()
	bare := filepath.Join(root, "remote.git")
	src = filepath.Join(root, "src")
	work := filepath.Join(root, "work")
	run := func(dir string, args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=T", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=T", "GIT_COMMITTER_EMAIL=t@t")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	run(root, "init", "--quiet", "--bare", "-b", "main", bare)
	run(root, "clone", "--quiet", bare, work)
	os.WriteFile(filepath.Join(work, "go.mod"), []byte("module hobstack\n\ngo 1.24\n"), 0o644)
	os.WriteFile(filepath.Join(work, ".gitignore"), []byte("/hobstack\n/hobstack.*\n"), 0o644)
	run(work, "add", ".")
	run(work, "commit", "--quiet", "-m", "first")
	run(work, "push", "--quiet", "origin", "HEAD:main")
	run(root, "clone", "--quiet", bare, src)
	os.WriteFile(filepath.Join(src, "hobstack"), []byte("old"), 0o755)
	n := 0
	push = func(msg string) {
		n++
		os.WriteFile(filepath.Join(work, "f.txt"), []byte(msg), 0o644)
		run(work, "add", ".")
		run(work, "commit", "--quiet", "-m", msg)
		run(work, "push", "--quiet", "origin", "HEAD:main")
	}
	return src, push
}

func head(t *testing.T, dir, ref string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", dir, "rev-parse", ref).Output()
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(out))
}

type fake struct {
	active    atomic.Int32
	held      atomic.Bool
	restarted chan struct{}
	backups   atomic.Int32
	builds    atomic.Int32
	buildErr  error
}

func newUpdater(t *testing.T, src string, f *fake) *Updater {
	t.Helper()
	f.restarted = make(chan struct{}, 1)
	t.Setenv("HOBSTACK_SOURCE", src)
	u := New(filepath.Join(src, "hobstack"), Deps{
		Active:  func() int { return int(f.active.Load()) },
		Hold:    func(on bool) { f.held.Store(on) },
		Backup:  func(path string) error { f.backups.Add(1); return os.WriteFile(path, []byte("db"), 0o644) },
		Restart: func() { f.restarted <- struct{}{} },
		Backups: filepath.Join(t.TempDir(), "backups"),
		Build: func(ctx context.Context, dir, out string) ([]byte, error) {
			f.builds.Add(1)
			if f.buildErr != nil {
				return []byte("main.go:1: syntax error"), f.buildErr
			}
			return nil, os.WriteFile(out, []byte("new"), 0o755)
		},
	})
	if !u.Status().CanUpdate {
		t.Fatalf("cannot update: %s", u.Status().Reason)
	}
	u.rev = head(t, src, "HEAD") // binarul "e facut" din commit-ul curent
	u.st.Current = short(u.rev)
	return u
}

// waitState asteapta pana cand starea update-ului devine una din cele date.
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

func TestNotFromSource(t *testing.T) {
	t.Setenv("HOBSTACK_SOURCE", t.TempDir())
	u := New("/usr/local/bin/hobstack", Deps{})
	if s := u.Status(); s.CanUpdate || s.Reason == "" {
		t.Fatalf("a folder without go.mod must not be updatable: %+v", s)
	}
	if err := u.Start(context.Background()); err == nil {
		t.Fatal("Start without a source folder must fail")
	}
}

func TestCheckCountsNewCommits(t *testing.T) {
	src, push := repo(t)
	u := newUpdater(t, src, &fake{})
	if err := u.Check(context.Background()); err != nil {
		t.Fatal(err)
	}
	if s := u.Status(); s.Behind != 0 || s.Upstream != "origin/main" {
		t.Fatalf("fresh clone: %+v", s)
	}
	push("second")
	push("third")
	if err := u.Check(context.Background()); err != nil {
		t.Fatal(err)
	}
	s := u.Status()
	if s.Behind != 2 || len(s.Commits) != 2 || s.Commits[0].Subject != "third" || s.CheckedAt.IsZero() {
		t.Fatalf("after 2 pushes: %+v", s)
	}
}

func TestUpdateWaitsForAgentsThenRestarts(t *testing.T) {
	src, push := repo(t)
	push("second")
	f := &fake{}
	f.active.Store(1)
	u := newUpdater(t, src, f)
	if err := u.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	s := waitState(t, u, StateWaiting, StateFailed)
	if s.State != StateWaiting || s.Waiting != 1 || !f.held.Load() {
		t.Fatalf("with an agent running the update must wait and hold new runs: %+v held=%v", s, f.held.Load())
	}
	if err := u.Start(context.Background()); !errors.Is(err, ErrBusy) {
		t.Fatalf("second Start: %v", err)
	}
	if got, _ := os.ReadFile(filepath.Join(src, "hobstack")); string(got) != "new" {
		t.Fatalf("binary: %q", got)
	}
	if got, _ := os.ReadFile(filepath.Join(src, "hobstack.prev")); string(got) != "old" {
		t.Fatalf("previous binary: %q", got)
	}
	f.active.Store(0)
	select {
	case <-f.restarted:
	case <-time.After(10 * time.Second):
		t.Fatalf("no restart: %+v", u.Status())
	}
	if u.Status().State != StateRestarting || f.backups.Load() != 1 {
		t.Fatalf("after restart: %+v backups=%d", u.Status(), f.backups.Load())
	}
	if head(t, src, "HEAD") != head(t, src, "origin/main") {
		t.Fatal("the source was not fast-forwarded")
	}
}

func TestUpdateRefusesDirtySource(t *testing.T) {
	src, push := repo(t)
	push("second")
	u := newUpdater(t, src, &fake{})
	before := head(t, src, "HEAD")
	os.WriteFile(filepath.Join(src, "go.mod"), []byte("module hobstack\n// local\n"), 0o644)
	u.Start(context.Background())
	s := waitState(t, u, StateFailed, StateRestarting)
	if s.State != StateFailed || !strings.Contains(s.Error, "uncommitted") {
		t.Fatalf("dirty source: %+v", s)
	}
	if head(t, src, "HEAD") != before {
		t.Fatal("the source moved")
	}
}

func TestBuildFailureLeavesEverything(t *testing.T) {
	src, push := repo(t)
	push("second")
	f := &fake{buildErr: errors.New("exit status 1")}
	u := newUpdater(t, src, f)
	before := head(t, src, "HEAD")
	u.Start(context.Background())
	s := waitState(t, u, StateFailed, StateRestarting)
	if s.State != StateFailed || !strings.Contains(s.Error, "syntax error") {
		t.Fatalf("build failure: %+v", s)
	}
	if head(t, src, "HEAD") != before {
		t.Fatal("the source was not put back")
	}
	if got, _ := os.ReadFile(filepath.Join(src, "hobstack")); string(got) != "old" {
		t.Fatalf("binary changed: %q", got)
	}
	if _, err := os.Stat(filepath.Join(src, "hobstack.new")); err == nil {
		t.Fatal("hobstack.new left behind")
	}
}

func TestCancelWhileWaiting(t *testing.T) {
	src, push := repo(t)
	push("second")
	f := &fake{}
	f.active.Store(2)
	u := newUpdater(t, src, f)
	u.Start(context.Background())
	waitState(t, u, StateWaiting)
	if !u.Cancel() {
		t.Fatal("Cancel while waiting")
	}
	s := waitState(t, u, StateInstalled)
	if f.held.Load() {
		t.Fatal("runs still held after cancel")
	}
	if !strings.Contains(s.Error, "next time") {
		t.Fatalf("cancel message: %q", s.Error)
	}
	select {
	case <-f.restarted:
		t.Fatal("restarted after cancel")
	default:
	}
	// "Restart now" dupa anulare: binarul e deja pus, deci doar asteapta si reporneste (fara alt build)
	f.active.Store(0)
	if err := u.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-f.restarted:
	case <-time.After(10 * time.Second):
		t.Fatalf("no restart: %+v", u.Status())
	}
	if f.builds.Load() != 1 {
		t.Fatalf("builds = %d, want 1", f.builds.Load())
	}
	if got, _ := os.ReadFile(filepath.Join(src, "hobstack.prev")); string(got) != "old" {
		t.Fatalf("the previous binary was replaced: %q", got)
	}
}
