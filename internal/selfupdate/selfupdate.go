// Package selfupdate actualizeaza HobStack din folderul cu codul sursa (git): verifica daca ramura urmarita
// (de ex. hobstack/main) are commit-uri noi, apoi la cerere face git merge --ff-only, go build, inlocuieste
// binarul (cel vechi ramane <exe>.prev), asteapta sa nu mai ruleze niciun agent, face o copie a bazei de date
// si reporneste aplicatia.
package selfupdate

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Starile unui update.
const (
	StateIdle       = "idle"       // nimic in curs
	StateUpdating   = "updating"   // download, build, inlocuirea binarului (Step spune ce)
	StateWaiting    = "waiting"    // binarul nou e pus; asteapta sa termine agentii
	StateRestarting = "restarting" // aplicatia se opreste si porneste versiunea noua
	StateInstalled  = "installed"  // binarul nou e pus, dar restart-ul a fost anulat
	StateFailed     = "failed"     // Error spune de ce; nimic nu s-a schimbat (sau doar binarul, vezi Error)
)

// MaxCommits: cate commit-uri noi aratam in lista.
const MaxCommits = 30

// Commit e un commit nou de pe ramura urmarita.
type Commit struct {
	Hash    string    `json:"hash"`
	Subject string    `json:"subject"`
	Author  string    `json:"author"`
	When    time.Time `json:"when"`
}

// Status e ce vede UI-ul.
type Status struct {
	Current     string    `json:"current"`              // revizia binarului (scurta), "" = necunoscuta
	CurrentTime time.Time `json:"currentTime,omitzero"` // data commit-ului din care e facut binarul
	Modified    bool      `json:"modified"`             // binarul a fost facut cu modificari necommit-uite
	Source      string    `json:"source"`               // folderul cu codul sursa
	CanUpdate   bool      `json:"canUpdate"`            // update-ul din aplicatie e posibil
	Reason      string    `json:"reason,omitempty"`     // de ce nu e posibil
	Upstream    string    `json:"upstream,omitempty"`   // ramura urmarita, de ex. hobstack/main
	Behind      int       `json:"behind"`               // cate commit-uri noi are ramura urmarita
	Commits     []Commit  `json:"commits"`              // primele MaxCommits, cele mai noi primele
	CheckedAt   time.Time `json:"checkedAt,omitzero"`   // ultima verificare reusita
	CheckError  string    `json:"checkError,omitempty"` // ultima verificare a esuat
	Checking    bool      `json:"checking"`             // o verificare e in curs
	State       string    `json:"state"`                // vezi State*
	Step        string    `json:"step,omitempty"`       // pasul curent cat State e updating
	Error       string    `json:"error,omitempty"`      // cu StateFailed / StateInstalled
	Waiting     int       `json:"waiting"`              // cati agenti ruleaza inca (StateWaiting)
	Target      string    `json:"target,omitempty"`     // revizia la care se actualizeaza
}

// Deps leaga update-ul de restul aplicatiei.
type Deps struct {
	Active  func() int                                                 // cate run-uri claude ruleaza acum
	Hold    func(on bool)                                              // opreste / reia pornirea run-urilor noi
	Backup  func(path string) error                                    // copia bazei de date (VACUUM INTO)
	Restart func()                                                     // opreste aplicatia; main porneste binarul nou
	Backups string                                                     // folderul cu copiile bazei de date
	Build   func(ctx context.Context, src, out string) ([]byte, error) // nil = go build ./cmd/hobstack
}

// Updater tine starea update-ului. Metodele sunt sigure din mai multe goroutine.
type Updater struct {
	src  string // folderul sursa ("" = update-ul nu e posibil)
	exe  string // binarul care ruleaza
	deps Deps
	rev  string // revizia binarului (completa)

	mu      sync.Mutex
	st      Status
	cancel    chan struct{} // inchis de Cancel cat se asteapta agentii
	running   bool          // Update in curs (un singur update odata)
	installed string        // revizia pusa deja in locul binarului (restart anulat); "Restart now" nu o mai construieste
}

// BuildInfo intoarce revizia, data si "modified" din binarul curent (go build le pune singur intr-un repo git).
func BuildInfo() (rev string, when time.Time, modified bool) {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return "", time.Time{}, false
	}
	for _, s := range bi.Settings {
		switch s.Key {
		case "vcs.revision":
			rev = s.Value
		case "vcs.time":
			when, _ = time.Parse(time.RFC3339, s.Value)
		case "vcs.modified":
			modified = s.Value == "true"
		}
	}
	return rev, when, modified
}

// New pregateste update-ul pentru binarul exe. Folderul sursa e HOBSTACK_SOURCE sau folderul binarului
// (HobStack se construieste cu go build -o hobstack ./cmd/hobstack in radacina repo-ului).
func New(exe string, deps Deps) *Updater {
	rev, when, modified := BuildInfo()
	u := &Updater{exe: exe, deps: deps, rev: rev}
	u.st = Status{Current: short(rev), CurrentTime: when, Modified: modified, State: StateIdle, Commits: []Commit{}}
	src := os.Getenv("HOBSTACK_SOURCE")
	if src == "" {
		src = filepath.Dir(exe)
	}
	u.st.Source = src
	if reason := checkSource(src); reason != "" {
		u.st.Reason = reason
	} else {
		u.src = src
		u.st.CanUpdate = true
	}
	return u
}

// checkSource: "" daca src e un checkout git al HobStack si avem git si go.
func checkSource(src string) string {
	mod, err := os.ReadFile(filepath.Join(src, "go.mod"))
	if err != nil || !bytes.HasPrefix(bytes.TrimSpace(mod), []byte("module hobstack")) {
		return "HobStack is not running from its source folder, so it cannot update itself. Set HOBSTACK_SOURCE to the folder with the code."
	}
	if _, err := os.Stat(filepath.Join(src, ".git")); err != nil {
		return "The source folder " + src + " is not a git repository."
	}
	if _, err := exec.LookPath("git"); err != nil {
		return "git is not installed (or not in PATH)."
	}
	if _, err := exec.LookPath("go"); err != nil {
		return "Go is not installed (or not in PATH), so the new version cannot be built."
	}
	return ""
}

// Status intoarce o copie a starii.
func (u *Updater) Status() Status {
	u.mu.Lock()
	defer u.mu.Unlock()
	s := u.st
	s.Commits = append([]Commit{}, u.st.Commits...)
	return s
}

func (u *Updater) set(f func(*Status)) {
	u.mu.Lock()
	f(&u.st)
	u.mu.Unlock()
}

// git ruleaza o comanda git in folderul sursa, fara prompt-uri (ssh in BatchMode).
func (u *Updater) git(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = u.src
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_OPTIONAL_LOCKS=0")
	if os.Getenv("GIT_SSH_COMMAND") == "" {
		cmd.Env = append(cmd.Env, "GIT_SSH_COMMAND=ssh -o BatchMode=yes")
	}
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(errb.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("git %s: %s", args[0], msg)
	}
	return strings.TrimSpace(out.String()), nil
}

// Check aduce ramura urmarita (git fetch) si numara commit-urile noi fata de binarul care ruleaza.
func (u *Updater) Check(ctx context.Context) error {
	if u.src == "" {
		return nil
	}
	u.mu.Lock()
	if u.st.Checking || u.running {
		u.mu.Unlock()
		return nil // o verificare sau un update e deja in curs
	}
	u.st.Checking = true
	u.mu.Unlock()

	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	upstream, behind, commits, err := u.compare(ctx)
	u.set(func(s *Status) {
		s.Checking = false
		if err != nil {
			s.CheckError = err.Error()
			return
		}
		s.CheckError = ""
		s.CheckedAt = time.Now()
		s.Upstream, s.Behind, s.Commits = upstream, behind, commits
	})
	return err
}

// compare face fetch si intoarce ramura urmarita, cate commit-uri are in plus si primele MaxCommits.
func (u *Updater) compare(ctx context.Context) (string, int, []Commit, error) {
	upstream, err := u.git(ctx, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{u}")
	if err != nil {
		return "", 0, nil, errors.New("the current branch of " + u.src + " does not track a remote branch (git branch --set-upstream-to)")
	}
	if _, err := u.git(ctx, "fetch", "--quiet"); err != nil {
		return upstream, 0, nil, err
	}
	base := u.base(ctx)
	n, err := u.git(ctx, "rev-list", "--count", base+"..@{u}")
	if err != nil {
		return upstream, 0, nil, err
	}
	behind, _ := strconv.Atoi(n)
	out, err := u.git(ctx, "log", "--format=%H%x1f%s%x1f%an%x1f%cI", "-n", strconv.Itoa(MaxCommits), base+"..@{u}")
	if err != nil {
		return upstream, 0, nil, err
	}
	commits := []Commit{}
	for line := range strings.SplitSeq(out, "\n") {
		f := strings.Split(line, "\x1f")
		if len(f) != 4 {
			continue
		}
		when, _ := time.Parse(time.RFC3339, f[3])
		commits = append(commits, Commit{Hash: short(f[0]), Subject: f[1], Author: f[2], When: when})
	}
	return upstream, behind, commits, nil
}

// base: commit-ul fata de care comparam: revizia binarului daca o are repo-ul, altfel HEAD.
func (u *Updater) base(ctx context.Context) string {
	if u.rev != "" {
		if _, err := u.git(ctx, "cat-file", "-e", u.rev+"^{commit}"); err == nil {
			return u.rev
		}
	}
	return "HEAD"
}

// ErrBusy: un update e deja in curs.
var ErrBusy = errors.New("an update is already in progress")

// Start porneste update-ul in fundal (ctx traieste cat aplicatia). Erorile ajung in Status.
func (u *Updater) Start(ctx context.Context) error {
	if u.src == "" {
		return errors.New(u.st.Reason)
	}
	u.mu.Lock()
	if u.running {
		u.mu.Unlock()
		return ErrBusy
	}
	u.running = true
	u.cancel = make(chan struct{})
	u.st.State, u.st.Step, u.st.Error, u.st.Waiting = StateUpdating, "Downloading", "", 0
	cancel := u.cancel
	u.mu.Unlock()
	go u.run(ctx, cancel)
	return nil
}

// Cancel opreste asteptarea agentilor: binarul nou ramane pus si porneste la urmatorul restart.
func (u *Updater) Cancel() bool {
	u.mu.Lock()
	defer u.mu.Unlock()
	if !u.running || u.st.State != StateWaiting || u.cancel == nil {
		return false
	}
	close(u.cancel)
	u.cancel = nil
	return true
}

func (u *Updater) run(ctx context.Context, cancel chan struct{}) {
	restarting := false
	defer func() {
		if !restarting {
			u.mu.Lock()
			u.running = false
			u.mu.Unlock()
		}
	}()
	fail := func(msg string) {
		log.Printf("update: %s", msg)
		u.set(func(s *Status) { s.State, s.Step, s.Error = StateFailed, "", msg })
	}

	// 1. sursa curata si ramura urmarita in fata (fara commit-uri locale care ar cere merge)
	if dirty, err := u.git(ctx, "status", "--porcelain", "--untracked-files=no"); err != nil {
		fail(err.Error())
		return
	} else if dirty != "" {
		fail("The source folder has uncommitted changes. Commit or stash them, then update again.")
		return
	}
	upstream, _, _, err := u.compare(ctx)
	if err != nil {
		fail(err.Error())
		return
	}
	if _, err := u.git(ctx, "merge-base", "--is-ancestor", "HEAD", "@{u}"); err != nil {
		fail("The local branch has commits that are not on " + upstream + ". Push or merge them by hand, then update again.")
		return
	}
	old, err := u.git(ctx, "rev-parse", "HEAD")
	if err != nil {
		fail(err.Error())
		return
	}
	target, err := u.git(ctx, "rev-parse", "@{u}")
	if err != nil {
		fail(err.Error())
		return
	}
	if target == u.rev && !u.st.Modified {
		u.set(func(s *Status) { s.State, s.Step, s.Behind, s.Commits = StateIdle, "", 0, []Commit{} })
		return // deja la zi
	}
	u.mu.Lock()
	ready := u.installed == target && old == target
	u.mu.Unlock()
	if !ready {
		if !u.install(ctx, old, target, fail) {
			return
		}
	}
	u.restartWhenIdle(ctx, cancel)
	restarting = u.Status().State == StateRestarting
}

// install aduce sursa la target, construieste si pune binarul nou; false (cu fail chemat) la eroare.
func (u *Updater) install(ctx context.Context, old, target string, fail func(string)) bool {
	u.set(func(s *Status) { s.Step, s.Target = "Updating the source", short(target) })
	if _, err := u.git(ctx, "merge", "--ff-only", "--quiet", target); err != nil {
		fail(err.Error())
		return false
	}

	// 2. build intr-un fisier alaturi; la eroare sursa revine unde era
	u.set(func(s *Status) { s.Step = "Building" })
	next := u.exe + ".new"
	os.Remove(next)
	bctx, bcancel := context.WithTimeout(ctx, 10*time.Minute)
	out, err := u.build(bctx, next)
	bcancel()
	if err != nil {
		os.Remove(next)
		if _, rerr := u.git(ctx, "reset", "--keep", "--quiet", old); rerr != nil {
			log.Printf("update: putting the source back at %s: %v", short(old), rerr)
		}
		fail("The build failed; nothing changed.\n" + lastLines(string(out), 20))
		return false
	}

	// 3. binarul nou in locul celui vechi (cel vechi ramane .prev)
	u.set(func(s *Status) { s.Step = "Installing" })
	if err := swap(u.exe, next); err != nil {
		os.Remove(next)
		fail("Replacing the binary failed: " + err.Error())
		return false
	}
	u.mu.Lock()
	u.installed = target
	u.mu.Unlock()
	return true
}

// restartWhenIdle asteapta sa nu mai ruleze niciun agent, face copia bazei de date si reporneste.
// Cancel (sau o copie esuata) lasa binarul nou pus, pentru urmatoarea pornire.
func (u *Updater) restartWhenIdle(ctx context.Context, cancel chan struct{}) {
	// 4. asteptam sa nu mai ruleze niciun agent; intre timp nu pornesc run-uri noi
	u.deps.Hold(true)
	held := true
	defer func() {
		if held {
			u.deps.Hold(false)
		}
	}()
	tick := time.NewTicker(2 * time.Second)
	defer tick.Stop()
	for {
		n := u.deps.Active()
		if n == 0 {
			break
		}
		u.set(func(s *Status) { s.State, s.Step, s.Waiting = StateWaiting, "", n })
		select {
		case <-ctx.Done():
			return
		case <-cancel:
			u.set(func(s *Status) {
				s.State, s.Waiting = StateInstalled, 0
				s.Error = "Restart cancelled. The new version is installed and starts the next time HobStack starts."
			})
			return
		case <-tick.C:
		}
	}

	// 5. copia bazei de date, apoi restart
	u.set(func(s *Status) { s.State, s.Step, s.Waiting = StateUpdating, "Backing up the database", 0 })
	if u.deps.Backup != nil && u.deps.Backups != "" {
		if err := u.backup(short(u.rev)); err != nil {
			u.set(func(s *Status) {
				s.State, s.Step = StateInstalled, ""
				s.Error = "The database backup failed, so HobStack did not restart: " + err.Error() +
					". The new version is installed and starts the next time HobStack starts."
			})
			return
		}
	}
	u.set(func(s *Status) { s.State, s.Step = StateRestarting, "" })
	held = false // run-urile nu mai pornesc oricum; procesul se opreste
	log.Printf("update: restarting with %s", u.Status().Target)
	u.deps.Restart()
}

func (u *Updater) build(ctx context.Context, out string) ([]byte, error) {
	if u.deps.Build != nil {
		return u.deps.Build(ctx, u.src, out)
	}
	cmd := exec.CommandContext(ctx, "go", "build", "-o", out, "./cmd/hobstack")
	cmd.Dir = u.src
	return cmd.CombinedOutput()
}

// backup scrie o copie a bazei de date si pastreaza doar ultimele 3.
func (u *Updater) backup(rev string) error {
	if err := os.MkdirAll(u.deps.Backups, 0o755); err != nil {
		return err
	}
	if rev == "" {
		rev = "unknown"
	}
	name := "hobstack-" + time.Now().Format("20060102-150405") + "-" + rev + ".db"
	if err := u.deps.Backup(filepath.Join(u.deps.Backups, name)); err != nil {
		return err
	}
	old, _ := filepath.Glob(filepath.Join(u.deps.Backups, "hobstack-*.db"))
	// numele incep cu data, deci ordinea alfabetica e cea cronologica
	for len(old) > 3 {
		os.Remove(old[0])
		old = old[1:]
	}
	return nil
}

// swap pune next in locul lui exe; exe vechi devine exe.prev. Pe Windows un binar care ruleaza
// poate fi redenumit, dar nu suprascris, deci mereu prin rename.
func swap(exe, next string) error {
	prev := exe + ".prev"
	os.Remove(prev)
	if err := os.Rename(exe, prev); err != nil {
		return err
	}
	if err := os.Rename(next, exe); err != nil {
		os.Rename(prev, exe) // inapoi
		return err
	}
	return nil
}

func short(rev string) string {
	if len(rev) > 7 {
		return rev[:7]
	}
	return rev
}

func lastLines(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}
