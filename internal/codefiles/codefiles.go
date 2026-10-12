// Package codefiles listeaza fisierele de cod ale unui folder de proiect, pentru @ in chat.
// Intr-un repo git decide .gitignore (git ls-files); altfel se parcurge folderul si se sar
// dependentele, build-urile si folderele ascunse.
package codefiles

import (
	"bufio"
	"bytes"
	"io/fs"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// MaxFiles: cate fisiere se retin dintr-un folder (un monorepo urias nu blocheaza chat-ul).
var MaxFiles = 20000

// skipDirs: folderele sarite cand folderul nu e repo git.
var skipDirs = map[string]bool{
	"node_modules": true, "vendor": true, "dist": true, "build": true, "target": true,
	"__pycache__": true, "bower_components": true, "coverage": true,
}

// List intoarce caile fisierelor din dir (relative, cu "/"), cel mult MaxFiles.
func List(dir string) ([]string, error) {
	if files, ok := gitFiles(dir); ok {
		return files, nil
	}
	return walk(dir)
}

// gitFiles: fisierele urmarite si cele noi neignorate; ok=false daca dir nu e intr-un repo git.
func gitFiles(dir string) ([]string, bool) {
	if _, err := exec.LookPath("git"); err != nil {
		return nil, false
	}
	cmd := exec.Command("git", "ls-files", "--cached", "--others", "--exclude-standard", "-z")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return nil, false
	}
	// fisierele urmarite dar sterse de pe disc (rm / mv fara commit) raman in --cached: le scoatem
	seen := map[string]bool{}
	del := exec.Command("git", "ls-files", "--deleted", "-z")
	del.Dir = dir
	if out, err := del.Output(); err == nil {
		for _, f := range bytes.Split(out, []byte{0}) {
			if len(f) > 0 {
				seen[string(f)] = true
			}
		}
	}
	var files []string
	sc := bufio.NewScanner(bytes.NewReader(out))
	sc.Buffer(make([]byte, 64*1024), 1<<20)
	sc.Split(splitNUL)
	for sc.Scan() && len(files) < MaxFiles {
		f := sc.Text()
		// dependentele scapate din .gitignore (node_modules necommis, vendor) nu intra nici aici
		if f != "" && !seen[f] && !inSkippedDir(f) {
			seen[f] = true
			files = append(files, f)
		}
	}
	return files, true
}

// inSkippedDir: calea trece printr-un folder din skipDirs.
func inSkippedDir(rel string) bool {
	parts := strings.Split(rel, "/")
	for _, p := range parts[:len(parts)-1] {
		if skipDirs[p] {
			return true
		}
	}
	return false
}

func splitNUL(data []byte, atEOF bool) (int, []byte, error) {
	if i := bytes.IndexByte(data, 0); i >= 0 {
		return i + 1, data[:i], nil
	}
	if atEOF && len(data) > 0 {
		return len(data), data, nil
	}
	return 0, nil, nil
}

// errFull opreste parcurgerea cand s-a atins MaxFiles.
var errFull = fs.ErrClosed

func walk(dir string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if p == dir {
				return err
			}
			return nil // un subfolder fara drept de citire nu strica lista
		}
		if d.IsDir() {
			if p != dir && (skipDirs[d.Name()] || strings.HasPrefix(d.Name(), ".")) {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return nil
		}
		files = append(files, filepath.ToSlash(rel))
		if len(files) >= MaxFiles {
			return errFull
		}
		return nil
	})
	if err == errFull {
		err = nil
	}
	return files, err
}

// Search intoarce cel mult limit cai care contin q (fara majuscule). Intai cele al caror nume
// incepe cu q, apoi cele al caror nume il contine, apoi cele potrivite doar pe cale; la egalitate
// numele mai scurt, apoi calea. q gol: primele limit cai, in ordine alfabetica.
func Search(files []string, q string, limit int) []string {
	idx := SearchIdx(files, q, limit)
	out := make([]string, len(idx))
	for i, j := range idx {
		out[i] = files[j]
	}
	return out
}

// SearchIdx e Search, dar intoarce pozitiile din files (pentru liste combinate din mai multe foldere).
func SearchIdx(files []string, q string, limit int) []int {
	q = strings.ToLower(strings.TrimSpace(q))
	type hit struct {
		i, tier, name int
	}
	var hits []hit
	for i, f := range files {
		lf := strings.ToLower(f)
		name := path.Base(lf)
		tier := -1
		switch {
		case q == "":
			tier = 0
		case strings.HasPrefix(name, q):
			tier = 0
		case strings.Contains(name, q):
			tier = 1
		case strings.Contains(lf, q):
			tier = 2
		}
		if tier >= 0 {
			hits = append(hits, hit{i, tier, len(name)})
		}
	}
	sort.Slice(hits, func(x, y int) bool {
		a, b := hits[x], hits[y]
		if q != "" {
			if a.tier != b.tier {
				return a.tier < b.tier
			}
			if a.name != b.name {
				return a.name < b.name
			}
		}
		return files[a.i] < files[b.i]
	})
	if len(hits) > limit {
		hits = hits[:limit]
	}
	out := make([]int, len(hits))
	for k, h := range hits {
		out[k] = h.i
	}
	return out
}

// Cache tine listele pe folder cateva secunde: tastarea dupa @ nu reciteste discul la fiecare litera.
type Cache struct {
	TTL time.Duration
	mu  sync.Mutex
	m   map[string]cached
}

type cached struct {
	at    time.Time
	files []string
}

func NewCache(ttl time.Duration) *Cache { return &Cache{TTL: ttl, m: map[string]cached{}} }

// Files: lista folderului dir, din cache daca e proaspata.
func (c *Cache) Files(dir string) ([]string, error) {
	c.mu.Lock()
	e, ok := c.m[dir]
	c.mu.Unlock()
	if ok && time.Since(e.at) < c.TTL {
		return e.files, nil
	}
	files, err := List(dir)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	c.m[dir] = cached{time.Now(), files}
	c.mu.Unlock()
	return files, nil
}

// Invalidate scoate folderul din cache: urmatorul Files il reciteste de pe disc.
func (c *Cache) Invalidate(dir string) {
	c.mu.Lock()
	delete(c.m, dir)
	c.mu.Unlock()
}
