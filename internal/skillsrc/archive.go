package skillsrc

// Arhiva GitHub: tar.gz/<sha> citit ca stream intr-un folder temporar (doar fisiere obisnuite),
// apoi se alege folderul skill-ului si doar fisierele lui ajung in destDir.

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"hobstack/internal/library"
	"hobstack/internal/store"
)

// variabile (nu constante) ca testele sa le poata micsora
var (
	maxArchive  int64 = 50 << 20  // arhiva comprimata
	maxUnpacked int64 = 256 << 20 // tot ce se dezarhiveaza (protectie la gzip bomb)
	maxEntries        = 100000    // intrari in arhiva
)

// archiveFile e un fisier obisnuit vazut in arhiva (cale relativa cu /, fara folderul de sus).
type archiveFile struct {
	size    int64
	exec    bool
	written bool // false pentru fisierele prea mari ca sa poata fi intr-un skill
}

// limitReader opreste citirea dupa n octeti si tine minte ca s-a depasit limita.
type limitReader struct {
	r        io.Reader
	n        int64
	exceeded bool
}

var errArchiveLimit = errors.New("archive limit reached")

func (l *limitReader) Read(p []byte) (int, error) {
	if l.n <= 0 {
		l.exceeded = true
		return 0, errArchiveLimit
	}
	if int64(len(p)) > l.n {
		p = p[:l.n]
	}
	n, err := l.r.Read(p)
	l.n -= int64(n)
	return n, err
}

// Fetch descarca tar.gz/<sha> din repo, gaseste folderul skill-ului (skills/<id>, apoi un folder
// numit <id>, apoi frontmatter name: <id>; la egalitate calea cea mai scurta, apoi alfabetic) si
// scrie doar fisierele lui in destDir. Intoarce calea folderului in repo ("" = radacina).
func (r *Remote) Fetch(ctx context.Context, repo, sha, skillID, destDir string) (string, error) {
	if !ValidRepo(repo) {
		return "", fmt.Errorf("invalid repository %q", repo)
	}
	if !ValidSkillID(skillID) {
		return "", fmt.Errorf("invalid skill id %q", skillID)
	}
	if !shaRe.MatchString(sha) {
		return "", fmt.Errorf("invalid commit %q", sha)
	}
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return "", err
	}
	// folderul temporar langa destDir (acelasi disc), sters la final
	tmp, err := os.MkdirTemp(filepath.Dir(destDir), ".skillsrc-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmp)

	body, err := r.get(ctx, r.CodeloadURL+"/"+repo+"/tar.gz/"+sha)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return "", errorf(ErrNotFound, "Couldn't find %s@%s on GitHub.", repo, sha)
		}
		return "", err
	}
	defer body.Close()

	files, err := extract(ctx, body, tmp)
	if err != nil {
		return "", err
	}
	dir, ok := pickFolder(files, tmp, repo, skillID)
	if !ok {
		return "", errorf(ErrNotFound, "Couldn't find the skill %s in %s.", skillID, repo)
	}

	// doar fisierele folderului ales, cu limitele unui skill din Library
	maxFiles, maxBytes := library.Limits(store.KindSkill)
	var chosen []string
	var total int64
	for p, f := range files {
		if dir != "" && !strings.HasPrefix(p, dir+"/") {
			continue
		}
		chosen = append(chosen, p)
		total += f.size
	}
	if len(chosen) > maxFiles {
		return "", errorf(ErrTooLarge, "The skill %s has more than %d files.", skillID, maxFiles)
	}
	if total > maxBytes {
		return "", errorf(ErrTooLarge, "The skill %s is larger than %d MB.", skillID, maxBytes>>20)
	}
	sort.Strings(chosen)

	src, err := os.OpenRoot(tmp)
	if err != nil {
		return "", err
	}
	defer src.Close()
	dst, err := os.OpenRoot(destDir)
	if err != nil {
		return "", err
	}
	defer dst.Close()
	for _, p := range chosen {
		rel := p
		if dir != "" {
			rel = strings.TrimPrefix(p, dir+"/")
		}
		if err := copyInRoot(src, filepath.FromSlash(p), dst, filepath.FromSlash(rel), files[p].exec); err != nil {
			return "", err
		}
	}
	return dir, nil
}

// extract citeste arhiva in tmp; intoarce fisierele obisnuite (cale cu / -> info).
// O cale cu ".." sau absoluta strica toata arhiva; symlink, hardlink, device, fifo si .git/ se ignora.
func extract(ctx context.Context, body io.Reader, tmp string) (map[string]archiveFile, error) {
	lim := &limitReader{r: body, n: maxArchive}
	readErr := func(err error) error {
		if lim.exceeded {
			return errorf(ErrTooLarge, "The archive is larger than %d MB.", maxArchive>>20)
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("%w: reading the archive: %v", ErrUnavailable, err)
	}
	gz, err := gzip.NewReader(lim)
	if err != nil {
		return nil, readErr(err)
	}
	defer gz.Close()

	root, err := os.OpenRoot(tmp)
	if err != nil {
		return nil, err
	}
	defer root.Close()

	_, perFile := library.Limits(store.KindSkill)
	files := map[string]archiveFile{}
	var unpacked int64
	entries := 0
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, readErr(err)
		}
		if entries++; entries > maxEntries {
			return nil, errorf(ErrTooLarge, "The archive has more than %d entries.", maxEntries)
		}
		if h.Typeflag == tar.TypeXGlobalHeader {
			continue
		}
		rel, err := archivePath(h.Name)
		if err != nil {
			return nil, err
		}
		if rel == "" || (h.Typeflag != tar.TypeReg && h.Typeflag != tar.TypeRegA) {
			continue // folderul de sus, foldere, symlink-uri, hardlink-uri, device-uri, fifo
		}
		if strings.Contains(rel, `\`) || hasGitDir(rel) {
			continue
		}
		if unpacked += h.Size; unpacked > maxUnpacked {
			return nil, errorf(ErrTooLarge, "The archive unpacks to more than %d MB.", maxUnpacked>>20)
		}
		f := archiveFile{size: h.Size, exec: h.Mode&0o111 != 0}
		// un fisier mai mare decat un skill intreg nu se scrie; marimea ramane pentru limita
		if h.Size <= perFile {
			if err := writeEntry(root, rel, tr, h.Size); err != nil {
				if lim.exceeded || ctx.Err() != nil {
					return nil, readErr(err)
				}
				return nil, err
			}
			f.written = true
		}
		files[rel] = f
	}
	return files, nil
}

// archivePath scoate primul nivel (<repo>-<sha>/) si verifica restul caii.
func archivePath(name string) (string, error) {
	if strings.HasPrefix(name, "/") || strings.ContainsRune(name, 0) {
		return "", fmt.Errorf("the archive has an absolute path %q", name)
	}
	parts := strings.Split(name, "/")
	for _, p := range parts {
		if p == ".." {
			return "", fmt.Errorf("the archive has a path that leaves the folder: %q", name)
		}
	}
	if len(parts) > 0 && strings.Contains(parts[0], ":") {
		return "", fmt.Errorf("the archive has an absolute path %q", name)
	}
	_, rest, _ := strings.Cut(name, "/")
	rest = path.Clean("/" + rest)[1:] // fara "./", "//" si "/" la final
	return rest, nil
}

func hasGitDir(rel string) bool {
	for _, p := range strings.Split(rel, "/") {
		if p == ".git" {
			return true
		}
	}
	return false
}

func writeEntry(root *os.Root, rel string, r io.Reader, size int64) error {
	name := filepath.FromSlash(rel)
	if d := filepath.Dir(name); d != "." {
		if err := root.MkdirAll(d, 0o755); err != nil {
			return fmt.Errorf("bad archive entry %q: %v", rel, err)
		}
	}
	out, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("bad archive entry %q: %v", rel, err)
	}
	_, err = io.CopyN(out, r, size)
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	return err
}

// pickFolder alege folderul skill-ului dintre folderele cu SKILL.md.
// Radacina repo-ului ("") are numele repo-ului.
func pickFolder(files map[string]archiveFile, tmp, repo, id string) (string, bool) {
	var dirs []string
	for p, f := range files {
		if path.Base(p) == "SKILL.md" && f.written {
			d := path.Dir(p)
			if d == "." {
				d = ""
			}
			dirs = append(dirs, d)
		}
	}
	_, repoName, _ := strings.Cut(repo, "/")
	name := func(d string) string {
		if d == "" {
			return repoName
		}
		return path.Base(d)
	}
	rules := []func(d string) bool{
		func(d string) bool { return d == "skills/"+id },
		func(d string) bool { return name(d) == id },
		func(d string) bool {
			data, err := readCapped(filepath.Join(tmp, filepath.FromSlash(path.Join(d, "SKILL.md"))), maxSkillMD)
			if err != nil {
				return false
			}
			n, _ := Frontmatter(data)
			return n == id
		},
	}
	for _, rule := range rules {
		var match []string
		for _, d := range dirs {
			if rule(d) {
				match = append(match, d)
			}
		}
		if len(match) > 0 {
			sort.Slice(match, func(i, j int) bool {
				if len(match[i]) != len(match[j]) {
					return len(match[i]) < len(match[j])
				}
				return match[i] < match[j]
			})
			return match[0], true
		}
	}
	return "", false
}

func readCapped(name string, n int64) ([]byte, error) {
	f, err := os.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(io.LimitReader(f, n))
}

// copyInRoot copiaza un fisier dintre doua radacini OpenRoot, cu bitul executabil pastrat.
func copyInRoot(src *os.Root, from string, dst *os.Root, to string, exec bool) error {
	in, err := src.Open(from)
	if err != nil {
		return err
	}
	defer in.Close()
	if d := filepath.Dir(to); d != "." {
		if err := dst.MkdirAll(d, 0o755); err != nil {
			return err
		}
	}
	mode := os.FileMode(0o644)
	if exec {
		mode = 0o755
	}
	out, err := dst.OpenFile(to, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, in)
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	return err
}
