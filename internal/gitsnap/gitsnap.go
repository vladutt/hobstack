// Package gitsnap gaseste fisierele schimbate de un run pe alte drumuri decat Edit / Write
// (cat > f, sed -i, scripturi, git checkout...). La pornirea run-ului Take retine HEAD-ul si
// fisierele deja modificate din folderul agentului (cu continutul lor); la final Changes compara
// starea de acum cu cea de la pornire si intoarce fisierele diferite, cu diff.
//
// Doar folderele dintr-un repo git; fisierele ignorate de git nu se vad. Daca alt agent lucreaza
// in acelasi folder in acelasi timp, schimbarile lui apar si aici (git nu stie cine a scris).
package gitsnap

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	MaxFiles      = 100              // fisiere intoarse de Changes (restul se ignora)
	maxFileBytes  = 1 << 20          // peste 1 MB: fara continut retinut / fara diff
	maxTotalBytes = 32 << 20         // continutul retinut la pornire, in total
	maxPatchChars = 200000           // ca runner: suma lungimilor liniilor dintr-un patch
	timeout       = 20 * time.Second // pentru toate comenzile git ale unui Take / Changes
)

// Hunk: un bloc de diff (aceleasi campuri ca runner.Hunk / store.Hunk).
type Hunk struct {
	OldStart, OldLines, NewStart, NewLines int
	Lines                                  []string // cu prefix "-", "+" sau " "
}

// Change: un fisier schimbat in timpul run-ului.
type Change struct {
	Path      string // relativ la folder, cu "/"
	Kind      string // create, edit, delete
	Patch     []Hunk
	Truncated bool // fara diff complet (binar, prea mare, prea lung)
	Committed bool // doar intrat intr-un commit facut in run (era deja modificat la pornire); Patch = diff-ul din commit
}

// state: un fisier la pornire. hash "" = nu exista.
type state struct {
	hash    string
	content []byte // nil = nu l-am retinut (prea mare); doar pentru fisierele deja modificate
	kept    bool
}

// Snapshot: starea folderului la pornirea run-ului.
type Snapshot struct {
	dir    string
	prefix string // folderul fata de radacina repo-ului ("" sau "sub/dir/")
	head   string // "" = repo fara commit-uri
	dirty  map[string]state
}

// Take retine starea folderului; (nil, nil) daca folderul nu e intr-un repo git sau git lipseste.
func Take(dir string) (*Snapshot, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	prefix, err := git(ctx, dir, nil, "rev-parse", "--show-prefix")
	if err != nil {
		return nil, nil // nu e repo (sau git lipseste): fara plasa de siguranta
	}
	s := &Snapshot{dir: dir, prefix: strings.TrimSpace(string(prefix)), dirty: map[string]state{}}
	if out, err := git(ctx, dir, nil, "rev-parse", "-q", "--verify", "HEAD"); err == nil {
		s.head = strings.TrimSpace(string(out))
	}
	paths, err := s.statusPaths(ctx)
	if err != nil {
		return nil, err
	}
	hashes, err := s.hashNow(ctx, paths)
	if err != nil {
		return nil, err
	}
	total := 0
	for _, p := range paths {
		st := state{hash: hashes[p]}
		if st.hash != "" && st.hash != other {
			if b, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(p))); err == nil && len(b) <= maxFileBytes && total+len(b) <= maxTotalBytes {
				st.content, st.kept = b, true
				total += len(b)
			}
		} else {
			st.kept = true // sters: continutul e gol
		}
		s.dirty[p] = st
	}
	return s, nil
}

// Dir: folderul pentru care s-a facut snapshot-ul.
func (s *Snapshot) Dir() string { return s.dir }

// Changes: fisierele diferite fata de pornire (sortate, max MaxFiles). skip: cai (relative, cu "/")
// deja inregistrate altfel (Edit / Write), care nu se mai intorc.
func (s *Snapshot) Changes(skip map[string]bool) ([]Change, error) {
	if s == nil {
		return nil, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cand := map[string]bool{}
	for p := range s.dirty {
		cand[p] = true
	}
	now, err := s.statusPaths(ctx)
	if err != nil {
		return nil, err
	}
	for _, p := range now {
		cand[p] = true
	}
	// commit-uri facute in run (git commit, checkout, reset): fisierele dintre HEAD-ul vechi si cel nou
	newHead := ""
	committed := map[string]bool{} // fisierele din commit-urile noi (HEAD-ul nou descinde din cel vechi)
	if head, err := git(ctx, s.dir, nil, "rev-parse", "-q", "--verify", "HEAD"); err == nil && s.head != "" {
		if h := strings.TrimSpace(string(head)); h != s.head {
			out, err := git(ctx, s.dir, nil, "diff", "--name-only", "-z", "--no-renames", s.head, h, "--", ".")
			if err == nil {
				_, notAncestor := git(ctx, s.dir, nil, "merge-base", "--is-ancestor", s.head, h)
				for _, p := range splitZ(out) {
					cand[s.rel(p)] = true
					if notAncestor == nil {
						committed[s.rel(p)] = true
						newHead = h
					}
				}
			}
		}
	}
	var paths []string
	for p := range cand {
		if p != "" && !skip[p] {
			paths = append(paths, p)
		}
	}
	sort.Strings(paths)

	nowHash, err := s.hashNow(ctx, paths)
	if err != nil {
		return nil, err
	}
	var undirty []string // fisierele curate la pornire: starea lor de atunci e cea din HEAD
	for _, p := range paths {
		if _, ok := s.dirty[p]; !ok {
			undirty = append(undirty, p)
		}
	}
	headHash, err := s.headBlobs(ctx, undirty)
	if err != nil {
		return nil, err
	}

	tmp, err := os.MkdirTemp("", "hobstack-gitsnap-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)
	var out []Change
	for _, p := range paths {
		before, ok := s.dirty[p]
		if !ok {
			before = state{hash: headHash[p]}
		}
		after := nowHash[p]
		if before.hash == other || after == other {
			continue
		}
		if before.hash == after {
			// continutul nu s-a schimbat in run, dar fisierul a intrat intr-un commit facut in run
			// (ex. un task care doar face commit pe munca altuia): diff-ul din commit
			if !committed[p] || len(out) == MaxFiles {
				continue
			}
			if c, ok := s.commitChange(ctx, p, newHead); ok {
				out = append(out, c)
			}
			continue
		}
		if len(out) == MaxFiles {
			break
		}
		c := Change{Path: p, Kind: "edit"}
		switch {
		case before.hash == "":
			c.Kind = "create"
		case after == "":
			c.Kind = "delete"
		}
		c.Patch, c.Truncated = s.diff(ctx, tmp, p, before, after)
		out = append(out, c)
	}
	return out, nil
}

// rel: calea fata de radacina repo-ului -> calea fata de folder ("" daca e in afara folderului)
func (s *Snapshot) rel(p string) string {
	if !strings.HasPrefix(p, s.prefix) {
		return ""
	}
	return strings.TrimPrefix(p, s.prefix)
}

// statusPaths: fisierele modificate / noi / sterse din folder, fata de HEAD (fara cele ignorate).
func (s *Snapshot) statusPaths(ctx context.Context) ([]string, error) {
	out, err := git(ctx, s.dir, nil, "status", "--porcelain=v1", "-z", "--untracked-files=all", "--no-renames", "--", ".")
	if err != nil {
		return nil, err
	}
	var paths []string
	for _, e := range splitZ(out) {
		if len(e) < 4 {
			continue
		}
		if p := s.rel(e[3:]); p != "" {
			paths = append(paths, p)
		}
	}
	return paths, nil
}

// other: hash-ul pus pentru ce exista dar nu e un fisier obisnuit (folder, submodul, symlink): nu se compara.
const other = "-"

// hashNow: hash-ul git al fisierelor de pe disc (ca blob, cu filtrele repo-ului); "" = lipseste,
// other = nu e un fisier obisnuit.
func (s *Snapshot) hashNow(ctx context.Context, paths []string) (map[string]string, error) {
	res := map[string]string{}
	var exist []string
	for _, p := range paths {
		fi, err := os.Lstat(filepath.Join(s.dir, filepath.FromSlash(p)))
		switch {
		case err != nil:
		case fi.Mode().IsRegular():
			exist = append(exist, p)
		default:
			res[p] = other
		}
	}
	if len(exist) == 0 {
		return res, nil
	}
	// cai absolute: --stdin-paths le citeste fata de radacina repo-ului, nu fata de folder
	var in strings.Builder
	for _, p := range exist {
		in.WriteString(filepath.Join(s.dir, filepath.FromSlash(p)) + "\n")
	}
	out, err := git(ctx, s.dir, []byte(in.String()), "hash-object", "--stdin-paths")
	if err != nil {
		return nil, err
	}
	hashes := strings.Fields(string(out))
	if len(hashes) != len(exist) {
		return nil, fmt.Errorf("git hash-object: %d hashes for %d files", len(hashes), len(exist))
	}
	for i, p := range exist {
		res[p] = hashes[i]
	}
	return res, nil
}

// headBlobs: hash-ul fisierelor in HEAD-ul de la pornire; lipsa = "".
func (s *Snapshot) headBlobs(ctx context.Context, paths []string) (map[string]string, error) {
	res := map[string]string{}
	if s.head == "" || len(paths) == 0 {
		return res, nil
	}
	var in bytes.Buffer
	for _, p := range paths {
		in.WriteString(s.head + ":" + s.prefix + p + "\n")
	}
	out, err := git(ctx, s.dir, in.Bytes(), "cat-file", "--batch-check=%(objectname) %(objecttype)")
	if err != nil {
		return nil, err
	}
	sc := bufio.NewScanner(bytes.NewReader(out))
	sc.Buffer(make([]byte, 64*1024), 1<<20)
	for i := 0; sc.Scan() && i < len(paths); i++ {
		if f := strings.Fields(sc.Text()); len(f) == 2 && f[1] == "blob" {
			res[paths[i]] = f[0]
		}
	}
	return res, nil
}

// diff: patch-ul dintre starea de la pornire si cea de acum, prin git diff --no-index pe doua fisiere temporare.
func (s *Snapshot) diff(ctx context.Context, tmp, p string, before state, after string) ([]Hunk, bool) {
	var old []byte
	switch {
	case before.hash == "":
	case before.kept:
		old = before.content
	default:
		b, err := s.blob(ctx, before.hash)
		if err != nil {
			return nil, true
		}
		old = b
	}
	var cur []byte
	if after != "" {
		b, err := os.ReadFile(filepath.Join(s.dir, filepath.FromSlash(p)))
		if err != nil || len(b) > maxFileBytes {
			return nil, true
		}
		cur = b
	}
	if len(old) > maxFileBytes {
		return nil, true
	}
	a, b := filepath.Join(tmp, "a"), filepath.Join(tmp, "b")
	if os.WriteFile(a, old, 0o600) != nil || os.WriteFile(b, cur, 0o600) != nil {
		return nil, true
	}
	out, err := git(ctx, tmp, nil, "diff", "--no-index", "--no-color", "--no-ext-diff", "-U3", "a", "b")
	var exit *exec.ExitError
	if err != nil && !(errors.As(err, &exit) && exit.ExitCode() == 1) { // 1 = sunt diferente
		return nil, true
	}
	return parseHunks(out)
}

// commitChange: fisierul p intre HEAD-ul de la pornire si head (diff-ul pe care l-au adus commit-urile din run).
// ok = false cand commit-urile nu schimba fisierul.
func (s *Snapshot) commitChange(ctx context.Context, p, head string) (Change, bool) {
	olds, err := s.headBlobs(ctx, []string{p})
	if err != nil {
		return Change{}, false
	}
	news, err := (&Snapshot{dir: s.dir, prefix: s.prefix, head: head}).headBlobs(ctx, []string{p})
	if err != nil || olds[p] == news[p] {
		return Change{}, false
	}
	c := Change{Path: p, Kind: "edit", Committed: true}
	switch {
	case olds[p] == "":
		c.Kind = "create"
	case news[p] == "":
		c.Kind = "delete"
	}
	// pathspec-ul e relativ la folder (cwd-ul comenzii)
	out, err := git(ctx, s.dir, nil, "diff", "--no-color", "--no-ext-diff", "--no-renames", "-U3", s.head, head, "--", p)
	if err != nil {
		c.Truncated = true
		return c, true
	}
	c.Patch, c.Truncated = parseHunks(out)
	return c, true
}

// blob: continutul unui blob (pentru fisierele curate la pornire); peste 1 MB eroare.
func (s *Snapshot) blob(ctx context.Context, hash string) ([]byte, error) {
	size, err := git(ctx, s.dir, nil, "cat-file", "-s", hash)
	if err != nil {
		return nil, err
	}
	if n, _ := strconv.Atoi(strings.TrimSpace(string(size))); n > maxFileBytes {
		return nil, errors.New("too large")
	}
	return git(ctx, s.dir, nil, "cat-file", "blob", hash)
}

// parseHunks citeste blocurile "@@ -a,b +c,d @@" dintr-un diff unificat. truncated: binar sau peste limita.
func parseHunks(out []byte) ([]Hunk, bool) {
	var hunks []Hunk
	chars := 0
	inHunk := false
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, "Binary files ") {
			return nil, true
		}
		if strings.HasPrefix(line, "@@ ") {
			var h Hunk
			if !parseRange(line, &h) {
				return hunks, true
			}
			hunks = append(hunks, h)
			inHunk = true
			continue
		}
		if !inHunk || line == "" {
			continue
		}
		switch line[0] {
		case ' ', '-', '+':
			chars += len(line)
			if chars > maxPatchChars {
				return hunks, true
			}
			h := &hunks[len(hunks)-1]
			h.Lines = append(h.Lines, line)
		}
		// "\ No newline at end of file" se ignora
	}
	return hunks, false
}

// parseRange: "@@ -1,3 +1,4 @@ ..." -> OldStart, OldLines, NewStart, NewLines (fara numar = 1)
func parseRange(line string, h *Hunk) bool {
	f := strings.Fields(line)
	if len(f) < 3 || !strings.HasPrefix(f[1], "-") || !strings.HasPrefix(f[2], "+") {
		return false
	}
	var ok1, ok2 bool
	h.OldStart, h.OldLines, ok1 = rangeOf(f[1][1:])
	h.NewStart, h.NewLines, ok2 = rangeOf(f[2][1:])
	return ok1 && ok2
}

func rangeOf(s string) (int, int, bool) {
	start, count, found := strings.Cut(s, ",")
	a, err := strconv.Atoi(start)
	if err != nil {
		return 0, 0, false
	}
	if !found {
		return a, 1, true
	}
	b, err := strconv.Atoi(count)
	return a, b, err == nil
}

func splitZ(b []byte) []string {
	var out []string
	for _, e := range strings.Split(string(b), "\x00") {
		if e != "" {
			out = append(out, e)
		}
	}
	return out
}

// git ruleaza o comanda git in dir. GIT_OPTIONAL_LOCKS=0: status nu ia index.lock, ca sa nu se
// bata cu git-ul agentului care ruleaza in acelasi timp.
func git(ctx context.Context, dir string, stdin []byte, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-c", "core.quotepath=off"}, args...)...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0", "LC_ALL=C")
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			// apelantul decide (git diff --no-index iese cu 1 la diferente); ExitError pastreaza codul
			exit.Stderr = stderr.Bytes()
			return out, fmt.Errorf("git %s: %w: %s", args[0], exit, strings.TrimSpace(stderr.String()))
		}
		return nil, fmt.Errorf("git %s: %w", args[0], err)
	}
	return out, nil
}
