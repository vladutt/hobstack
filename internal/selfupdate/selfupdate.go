// Package selfupdate actualizeaza HobStack din release-urile de pe GitHub: verifica ultimul release
// (tag semver, de ex. v1.1.0), iar la cerere descarca arhiva pentru platforma curenta, verifica sha256 din
// checksums.txt, porneste binarul nou o data ("version") ca proba, il pune in locul celui vechi (cel vechi
// ramane <exe>.prev), asteapta sa nu mai ruleze niciun agent, face o copie a bazei de date si reporneste.
package selfupdate

import (
	"archive/tar"
	"archive/zip"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Starile unui update.
const (
	StateIdle       = "idle"       // nimic in curs
	StateUpdating   = "updating"   // download, verificare, inlocuirea binarului (Step spune ce)
	StateWaiting    = "waiting"    // binarul nou e pus; asteapta sa termine agentii
	StateRestarting = "restarting" // aplicatia se opreste si porneste versiunea noua
	StateInstalled  = "installed"  // binarul nou e pus, dar restart-ul a fost anulat (sau copia bazei a esuat)
	StateFailed     = "failed"     // Error spune de ce; nimic nu s-a schimbat
)

// MaxDownload: arhiva unui release nu poate fi mai mare (binarul are ~20 MB).
const MaxDownload = 300 << 20

// Status e ce vede UI-ul.
type Status struct {
	Current  string `json:"current"`            // versiunea care ruleaza, de ex. "1.0.0"; "dev" = build local fara versiune
	Dev      bool   `json:"dev"`                // build fara versiune: nu propune singur update-uri
	Revision string `json:"revision,omitempty"` // commit-ul din care e facut binarul (scurt)
	Platform string `json:"platform"`           // de ex. darwin/arm64

	Latest     string    `json:"latest,omitempty"` // ultimul release, fara "v"; "" = niciun release (inca)
	LatestName string    `json:"latestName,omitempty"`
	Notes      string    `json:"notes,omitempty"` // notele release-ului (Markdown)
	URL        string    `json:"url,omitempty"`   // pagina release-ului pe GitHub
	Published  time.Time `json:"published,omitzero"`
	Available  bool      `json:"available"`        // Latest e mai nou decat Current
	CanUpdate  bool      `json:"canUpdate"`        // arhiva pentru platforma exista si binarul se poate inlocui
	Reason     string    `json:"reason,omitempty"` // de ce nu se poate (cand exista un release)

	CheckedAt  time.Time `json:"checkedAt,omitzero"`
	CheckError string    `json:"checkError,omitempty"`
	Checking   bool      `json:"checking"`

	State   string `json:"state"`
	Step    string `json:"step,omitempty"`
	Error   string `json:"error,omitempty"`
	Waiting int    `json:"waiting"`          // cati agenti ruleaza inca (StateWaiting)
	Target  string `json:"target,omitempty"` // versiunea care se instaleaza
}

// Deps leaga update-ul de restul aplicatiei.
type Deps struct {
	Active  func() int              // cate run-uri claude ruleaza acum
	Hold    func(on bool)           // opreste / reia pornirea run-urilor noi
	Backup  func(path string) error // copia bazei de date (VACUUM INTO)
	Restart func()                  // opreste aplicatia; main porneste binarul nou
	Backups string                  // folderul cu copiile bazei de date
}

// Options: de unde vin release-urile.
type Options struct {
	Repo    string // owner/name pe GitHub
	API     string // "" = https://api.github.com (sau HOBSTACK_UPDATE_URL)
	Version string // versiunea binarului curent (config.Version)
	Client  *http.Client
}

type asset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
	Size int64  `json:"size"`
}

type release struct {
	Tag       string    `json:"tag_name"`
	Name      string    `json:"name"`
	Body      string    `json:"body"`
	URL       string    `json:"html_url"`
	Published time.Time `json:"published_at"`
	Assets    []asset   `json:"assets"`
}

// Updater tine starea update-ului. Metodele sunt sigure din mai multe goroutine.
type Updater struct {
	exe     string
	deps    Deps
	opt     Options
	version string

	mu        sync.Mutex
	st        Status
	rel       *release      // ultimul release gasit
	cancel    chan struct{} // inchis de Cancel cat se asteapta agentii
	running   bool          // un singur update odata
	installed string        // versiunea pusa deja in locul binarului (restart anulat); "Restart now" nu o mai descarca
}

// New pregateste update-ul pentru binarul exe.
func New(exe string, opt Options, deps Deps) *Updater {
	if opt.API == "" {
		opt.API = os.Getenv("HOBSTACK_UPDATE_URL")
	}
	if opt.API == "" {
		opt.API = "https://api.github.com"
	}
	opt.API = strings.TrimRight(opt.API, "/")
	if opt.Client == nil {
		opt.Client = &http.Client{Timeout: 5 * time.Minute}
	}
	v := strings.TrimPrefix(strings.TrimSpace(opt.Version), "v")
	if v == "" {
		v = "dev"
	}
	u := &Updater{exe: exe, deps: deps, opt: opt, version: v}
	u.st = Status{Current: v, Dev: v == "dev", Revision: revision(), Platform: runtime.GOOS + "/" + runtime.GOARCH, State: StateIdle}
	return u
}

// revision: commit-ul scurt din care e facut binarul (go build il pune singur intr-un repo git).
func revision() string {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	for _, s := range bi.Settings {
		if s.Key == "vcs.revision" && len(s.Value) >= 7 {
			return s.Value[:7]
		}
	}
	return ""
}

// Status intoarce o copie a starii.
func (u *Updater) Status() Status {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.st
}

func (u *Updater) set(f func(*Status)) {
	u.mu.Lock()
	f(&u.st)
	u.mu.Unlock()
}

// end pune starea finala a unui update si il incheie in acelasi pas, ca un Start imediat dupa sa mearga.
func (u *Updater) end(f func(*Status)) {
	u.mu.Lock()
	f(&u.st)
	u.running = false
	u.mu.Unlock()
}

// Check cere ultimul release de pe GitHub.
func (u *Updater) Check(ctx context.Context) error {
	u.mu.Lock()
	if u.st.Checking || u.running {
		u.mu.Unlock()
		return nil
	}
	u.st.Checking = true
	u.mu.Unlock()

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	rel, err := u.latest(ctx)
	u.mu.Lock()
	defer u.mu.Unlock()
	u.st.Checking = false
	if err != nil {
		u.st.CheckError = err.Error()
		return err
	}
	u.st.CheckError = ""
	u.st.CheckedAt = time.Now()
	u.rel = rel
	u.describe(rel)
	return nil
}

// describe completeaza Status din release (rel nil = niciun release publicat). Se cheama cu mu blocat.
func (u *Updater) describe(rel *release) {
	s := &u.st
	s.Latest, s.LatestName, s.Notes, s.URL, s.Published = "", "", "", "", time.Time{}
	s.Available, s.CanUpdate, s.Reason = false, false, ""
	if rel == nil {
		return
	}
	s.Latest = strings.TrimPrefix(rel.Tag, "v")
	s.LatestName, s.Notes, s.URL, s.Published = rel.Name, rel.Body, rel.URL, rel.Published
	s.Available = !s.Dev && Newer(s.Latest, u.version)
	if _, ok := findAsset(rel, archiveName(s.Latest)); !ok {
		s.Reason = "This release has no download for " + s.Platform + "."
		return
	}
	if _, ok := findAsset(rel, "checksums.txt"); !ok {
		s.Reason = "This release has no checksums.txt, so the download cannot be verified."
		return
	}
	if err := writable(filepath.Dir(u.exe)); err != nil {
		s.Reason = "HobStack cannot write to " + filepath.Dir(u.exe) + ", so it cannot replace itself. Download the new version by hand."
		return
	}
	s.CanUpdate = true
}

func (u *Updater) latest(ctx context.Context) (*release, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", u.opt.API+"/repos/"+u.opt.Repo+"/releases/latest", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "HobStack/"+u.version)
	resp, err := u.opt.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("cannot reach GitHub: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, nil // niciun release publicat inca
	}
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 300))
		return nil, fmt.Errorf("GitHub answered %s: %s", resp.Status, strings.TrimSpace(string(msg)))
	}
	var rel release
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&rel); err != nil {
		return nil, fmt.Errorf("reading the release: %w", err)
	}
	return &rel, nil
}

// archiveName: numele arhivei pentru platforma curenta (scripts/build-release.sh).
func archiveName(version string) string {
	ext := ".tar.gz"
	if runtime.GOOS == "windows" {
		ext = ".zip"
	}
	return fmt.Sprintf("hobstack_%s_%s_%s%s", version, runtime.GOOS, runtime.GOARCH, ext)
}

func binaryName() string {
	if runtime.GOOS == "windows" {
		return "hobstack.exe"
	}
	return "hobstack"
}

func findAsset(rel *release, name string) (asset, bool) {
	for _, a := range rel.Assets {
		if a.Name == name {
			return a, true
		}
	}
	return asset{}, false
}

func writable(dir string) error {
	f, err := os.CreateTemp(dir, ".hobstack-write-*")
	if err != nil {
		return err
	}
	f.Close()
	return os.Remove(f.Name())
}

// ErrBusy: un update e deja in curs.
var ErrBusy = errors.New("an update is already in progress")

// Start instaleaza ultimul release in fundal (ctx traieste cat aplicatia). Erorile ajung in Status.
func (u *Updater) Start(ctx context.Context) error {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.running {
		return ErrBusy
	}
	if u.rel == nil || u.st.Latest == "" {
		return errors.New("no release to install; check for updates first")
	}
	if !u.st.CanUpdate {
		return errors.New(u.st.Reason)
	}
	if !u.st.Available && !u.st.Dev && u.installed != u.st.Latest {
		return errors.New("you already have the latest version")
	}
	u.running = true
	u.cancel = make(chan struct{})
	u.st.State, u.st.Step, u.st.Error, u.st.Waiting, u.st.Target = StateUpdating, "Downloading", "", 0, u.st.Latest
	go u.run(ctx, *u.rel, u.st.Latest, u.cancel)
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

func (u *Updater) run(ctx context.Context, rel release, target string, cancel chan struct{}) {
	restarting := false
	defer func() {
		if !restarting {
			u.mu.Lock()
			u.running = false
			u.mu.Unlock()
		}
	}()
	u.mu.Lock()
	ready := u.installed == target
	u.mu.Unlock()
	if !ready {
		if err := u.install(ctx, rel, target); err != nil {
			log.Printf("update: %v", err)
			u.end(func(s *Status) { s.State, s.Step, s.Error = StateFailed, "", err.Error() })
			return
		}
	}
	u.restartWhenIdle(ctx, target, cancel)
	restarting = u.Status().State == StateRestarting
}

// install descarca, verifica si pune binarul versiunii target in locul celui curent.
func (u *Updater) install(ctx context.Context, rel release, target string) error {
	arch, _ := findAsset(&rel, archiveName(target))
	sums, _ := findAsset(&rel, "checksums.txt")

	// 1. sha256 asteptat, din checksums.txt
	var want string
	{
		body, err := u.download(ctx, sums.URL, 1<<20)
		if err != nil {
			return fmt.Errorf("downloading checksums.txt: %w", err)
		}
		want = checksumFor(body, arch.Name)
		if want == "" {
			return fmt.Errorf("checksums.txt has no line for %s", arch.Name)
		}
	}

	// 2. arhiva, verificata
	data, err := u.download(ctx, arch.URL, MaxDownload)
	if err != nil {
		return fmt.Errorf("downloading %s: %w", arch.Name, err)
	}
	sum := sha256.Sum256(data)
	if got := hex.EncodeToString(sum[:]); got != want {
		return fmt.Errorf("the download of %s is damaged (sha256 %s, expected %s); nothing changed", arch.Name, got[:12], want[:12])
	}

	// 3. binarul din arhiva, langa cel curent
	u.set(func(s *Status) { s.Step = "Unpacking" })
	bin, err := extract(arch.Name, data, binaryName())
	if err != nil {
		return err
	}
	next := u.exe + ".new"
	os.Remove(next)
	if err := os.WriteFile(next, bin, 0o755); err != nil {
		return fmt.Errorf("writing the new binary: %w", err)
	}

	// 4. proba: binarul nou porneste pe masina asta si are versiunea asteptata
	u.set(func(s *Status) { s.Step = "Checking the new version" })
	vctx, vcancel := context.WithTimeout(ctx, 20*time.Second)
	out, err := exec.CommandContext(vctx, next, "version").Output()
	vcancel()
	if got := strings.TrimSpace(string(out)); err != nil || got != target {
		os.Remove(next)
		if err != nil {
			return fmt.Errorf("the new version does not start on this machine (%v); nothing changed", err)
		}
		return fmt.Errorf("the new binary says it is %q, expected %s; nothing changed", got, target)
	}

	// 5. in locul celui vechi (cel vechi ramane .prev)
	u.set(func(s *Status) { s.Step = "Installing" })
	if err := swap(u.exe, next); err != nil {
		os.Remove(next)
		return fmt.Errorf("replacing the binary failed: %w", err)
	}
	afterSwap(u.exe, target) // macOS: Info.plist si semnatura HobStack.app
	u.mu.Lock()
	u.installed = target
	u.mu.Unlock()
	return nil
}

func (u *Updater) download(ctx context.Context, url string, max int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "HobStack/"+u.version)
	resp, err := u.opt.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, errors.New(resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > max {
		return nil, fmt.Errorf("larger than %d MB", max>>20)
	}
	return data, nil
}

// checksumFor gaseste sha256 pentru name in formatul sha256sum ("<hash>  <nume>").
func checksumFor(body []byte, name string) string {
	sc := bufio.NewScanner(bytes.NewReader(body))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) == 2 && strings.TrimPrefix(f[1], "*") == name && len(f[0]) == 64 {
			return strings.ToLower(f[0])
		}
	}
	return ""
}

// extract scoate fisierul bin din radacina arhivei (.tar.gz sau .zip).
func extract(archive string, data []byte, bin string) ([]byte, error) {
	if strings.HasSuffix(archive, ".zip") {
		zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", archive, err)
		}
		for _, f := range zr.File {
			if strings.TrimPrefix(f.Name, "./") == bin {
				rc, err := f.Open()
				if err != nil {
					return nil, err
				}
				defer rc.Close()
				return io.ReadAll(io.LimitReader(rc, MaxDownload))
			}
		}
		return nil, fmt.Errorf("%s has no %s", archive, bin)
	}
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", archive, err)
	}
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return nil, fmt.Errorf("%s has no %s", archive, bin)
		}
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", archive, err)
		}
		if h.Typeflag == tar.TypeReg && strings.TrimPrefix(h.Name, "./") == bin {
			return io.ReadAll(io.LimitReader(tr, MaxDownload))
		}
	}
}

// restartWhenIdle asteapta sa nu mai ruleze niciun agent, face copia bazei de date si reporneste.
// Cancel (sau o copie esuata) lasa binarul nou pus, pentru urmatoarea pornire.
func (u *Updater) restartWhenIdle(ctx context.Context, target string, cancel chan struct{}) {
	// asteptam sa nu mai ruleze niciun agent; intre timp nu pornesc run-uri noi
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
			u.end(func(s *Status) {
				s.State, s.Waiting = StateInstalled, 0
				s.Error = "Restart cancelled. Version " + target + " is installed and starts the next time HobStack starts."
			})
			return
		case <-tick.C:
		}
	}

	// copia bazei de date, apoi restart
	u.set(func(s *Status) { s.State, s.Step, s.Waiting = StateUpdating, "Backing up the database", 0 })
	if u.deps.Backup != nil && u.deps.Backups != "" {
		if err := u.backup(); err != nil {
			u.end(func(s *Status) {
				s.State, s.Step = StateInstalled, ""
				s.Error = "The database backup failed, so HobStack did not restart: " + err.Error() +
					". Version " + target + " is installed and starts the next time HobStack starts."
			})
			return
		}
	}
	u.set(func(s *Status) { s.State, s.Step = StateRestarting, "" })
	held = false // procesul se opreste oricum
	log.Printf("update: restarting with %s", target)
	u.deps.Restart()
}

// backup scrie o copie a bazei de date si pastreaza doar ultimele 3.
func (u *Updater) backup() error {
	if err := os.MkdirAll(u.deps.Backups, 0o755); err != nil {
		return err
	}
	name := "hobstack-" + time.Now().Format("20060102-150405") + "-" + u.version + ".db"
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

// describeSuffix: ce adauga git describe dupa un tag ("-3-g657c1cf", "-dirty"): build facut dupa versiune.
var describeSuffix = regexp.MustCompile(`^-\d+-g[0-9a-f]+(-dirty)?$|^-dirty$`)

// Newer spune daca versiunea latest e mai noua decat current ("1.2.0", "v1.2.0", "1.1.0-3-gabc", "1.2.0-rc1").
// O versiune care nu se poate citi nu e niciodata mai noua.
func Newer(latest, current string) bool {
	lc, lpre, ok1 := parse(latest)
	cc, cpre, ok2 := parse(current)
	if !ok1 || !ok2 || lpre != "" {
		return false // release-urile au versiuni simple; o pre-versiune nu se propune
	}
	for i := range 3 {
		if lc[i] != cc[i] {
			return lc[i] > cc[i]
		}
	}
	// acelasi numar: e mai nou doar fata de o pre-versiune (1.2.0-rc1), nu fata de un build de dupa tag
	return cpre != "" && !describeSuffix.MatchString(cpre)
}

func parse(v string) (core [3]int, pre string, ok bool) {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	if i := strings.IndexAny(v, "-+"); i >= 0 {
		v, pre = v[:i], v[i:]
	}
	parts := strings.Split(v, ".")
	if len(parts) != 3 {
		return core, "", false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return core, "", false
		}
		core[i] = n
	}
	return core, pre, true
}

// CleanupPrevious sterge binarul de dinaintea ultimului update (<exe>.prev) si un .new ramas, dupa ce versiunea
// noua a pornit: ramane doar versiunea curenta. Pe Windows procesul vechi poate tine inca fisierul cateva
// secunde dupa restart, deci se mai incearca o data.
func CleanupPrevious(exe string) {
	for _, p := range []string{exe + ".prev", exe + ".new"} {
		if _, err := os.Stat(p); err != nil {
			continue
		}
		if err := os.Remove(p); err != nil {
			time.Sleep(10 * time.Second)
			if err := os.Remove(p); err != nil {
				log.Printf("update: cannot remove %s: %v", p, err)
			}
		}
	}
}
