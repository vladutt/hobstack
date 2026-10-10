package skillsrc

// Arhiva GitHub: tar.gz/<sha> salvat (comprimat, max 50 MB) intr-un fisier .staging-src-* langa
// destinatie, apoi citit de doua ori: trecerea 1 citeste doar header-ele si SKILL.md-urile si alege
// folderul, trecerea 2 scrie doar fisierele acelui folder direct in destDir (prin os.OpenRoot).

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
	maxUnpacked int64 = 256 << 20 // suma marimilor tuturor intrarilor (protectie la gzip bomb)
	maxEntries        = 100000    // intrari in arhiva
)

// archiveFile e un fisier obisnuit vazut in arhiva (cale relativa cu /, fara folderul de sus).
type archiveFile struct {
	size int64
	exec bool
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
		// mai citim un octet ca sa stim daca arhiva chiar trece de limita
		var one [1]byte
		if n, _ := l.r.Read(one[:]); n > 0 {
			l.exceeded = true
			return 0, errArchiveLimit
		}
		return 0, io.EOF
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
	if !ValidSkillID(skillID) {
		return "", fmt.Errorf("invalid skill id %q", skillID)
	}
	return r.fetch(ctx, repo, sha, skillID, destDir, func(skillMDs map[string][]byte) (string, error) {
		dir, ok := pickFolder(skillMDs, repo, skillID)
		if !ok {
			return "", errorf(ErrNotFound, "Couldn't find the skill %s in %s.", skillID, repo)
		}
		return dir, nil
	})
}

// FetchPath e Fetch pe calea exacta a folderului in repo (dir, "" = radacina), folosit la Update: fara cautare
// dupa nume, deci un alt folder cu acelasi nume aparut intre timp nu il inlocuieste. Fara SKILL.md la calea
// aceea: ErrNotFound "The skill moved or was removed in <repo>."
func (r *Remote) FetchPath(ctx context.Context, repo, sha, dir, destDir string) error {
	if !validRepoPath(dir) {
		return fmt.Errorf("invalid path %q", dir)
	}
	name := path.Base(dir)
	if dir == "" {
		_, name, _ = strings.Cut(repo, "/")
	}
	_, err := r.fetch(ctx, repo, sha, name, destDir, func(skillMDs map[string][]byte) (string, error) {
		if _, ok := skillMDs[dir]; !ok {
			return "", errorf(ErrNotFound, "The skill moved or was removed in %s.", repo)
		}
		return dir, nil
	})
	return err
}

// validRepoPath: o cale relativa curata in repo, cu /, fara "..", "." sau .git ("" = radacina).
func validRepoPath(p string) bool {
	if p == "" {
		return true
	}
	if strings.HasPrefix(p, "/") || strings.ContainsAny(p, "\\\x00:") || path.Clean(p) != p {
		return false
	}
	for _, part := range strings.Split(p, "/") {
		if part == "." || part == ".." || part == ".git" {
			return false
		}
	}
	return true
}

// fetch descarca arhiva si scrie in destDir fisierele folderului ales de pick (din folderele cu SKILL.md);
// name apare in mesajele despre limite.
func (r *Remote) fetch(ctx context.Context, repo, sha, name, destDir string, pick func(map[string][]byte) (string, error)) (string, error) {
	if !ValidRepo(repo) {
		return "", fmt.Errorf("invalid repository %q", repo)
	}
	if !shaRe.MatchString(sha) {
		return "", fmt.Errorf("invalid commit %q", sha)
	}
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return "", err
	}
	spool, err := r.download(ctx, r.CodeloadURL+"/"+repo+"/tar.gz/"+sha, filepath.Dir(destDir))
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return "", errorf(ErrNotFound, "Couldn't find %s@%s on GitHub.", repo, sha)
		}
		return "", err
	}
	defer func() {
		spool.Close()
		os.Remove(spool.Name())
	}()

	// trecerea 1: fisierele (doar marimi) si continutul SKILL.md-urilor
	files := map[string]archiveFile{}
	skillMDs := map[string][]byte{} // folder -> SKILL.md
	err = walkArchive(ctx, spool, func(rel string, h *tar.Header, body io.Reader) error {
		files[rel] = archiveFile{size: h.Size, exec: h.Mode&0o111 != 0}
		if path.Base(rel) == "SKILL.md" {
			// pentru frontmatter ajunge inceputul fisierului
			data, err := io.ReadAll(io.LimitReader(body, maxSkillMD))
			if err != nil {
				return err
			}
			d := path.Dir(rel)
			if d == "." {
				d = ""
			}
			skillMDs[d] = data
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	dir, err := pick(skillMDs)
	if err != nil {
		return "", err
	}
	inSkill := func(rel string) bool { return dir == "" || strings.HasPrefix(rel, dir+"/") }

	// limitele unui skill din Library, doar pentru folderul ales
	maxFiles, maxBytes := library.Limits(store.KindSkill)
	n, total := 0, int64(0)
	for p, f := range files {
		if inSkill(p) {
			n++
			total += f.size
		}
	}
	if n > maxFiles {
		return "", errorf(ErrTooLarge, "The skill %s has more than %d files.", name, maxFiles)
	}
	if total > maxBytes {
		return "", errorf(ErrTooLarge, "The skill %s is larger than %d MB.", name, maxBytes>>20)
	}

	// trecerea 2: doar fisierele folderului ales, direct in destDir
	dst, err := os.OpenRoot(destDir)
	if err != nil {
		return "", err
	}
	defer dst.Close()
	err = walkArchive(ctx, spool, func(rel string, h *tar.Header, body io.Reader) error {
		if !inSkill(rel) {
			return nil
		}
		to := rel
		if dir != "" {
			to = strings.TrimPrefix(rel, dir+"/")
		}
		return writeEntry(dst, filepath.FromSlash(to), body, h.Size, h.Mode&0o111 != 0)
	})
	if err != nil {
		return "", err
	}
	return dir, nil
}

// download salveaza corpul raspunsului (max maxArchive) intr-un fisier .staging-src-* din dir
// (sters de library.sweepStaging daca ramane dupa un crash). La eroare fisierul se sterge.
func (r *Remote) download(ctx context.Context, u, dir string) (*os.File, error) {
	body, err := r.getWith(ctx, r.fetchClient(), u)
	if err != nil {
		return nil, err
	}
	defer body.Close()
	f, err := os.CreateTemp(dir, ".staging-src-*")
	if err != nil {
		return nil, err
	}
	lim := &limitReader{r: body, n: maxArchive}
	_, err = io.Copy(f, lim)
	switch {
	case lim.exceeded:
		err = errorf(ErrTooLarge, "The archive is larger than %d MB.", maxArchive>>20)
	case err != nil && ctx.Err() != nil:
		err = ctx.Err()
	case err != nil:
		err = fmt.Errorf("%w: downloading the archive: %v", ErrUnavailable, err)
	}
	if err != nil {
		f.Close()
		os.Remove(f.Name())
		return nil, err
	}
	return f, nil
}

// walkArchive citeste arhiva de la inceput si cheama fn pentru fiecare fisier obisnuit (cale fara
// folderul de sus, cu /). O cale cu "..", absoluta, cu NUL sau cu litera de disc strica toata arhiva
// (ErrBadArchive); symlink, hardlink, device, fifo, .git/ si numele cu \ se sar. Limita de
// dezarhivare numara marimea fiecarei intrari, si a celor sarite.
func walkArchive(ctx context.Context, f *os.File, fn func(rel string, h *tar.Header, body io.Reader) error) error {
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return err
	}
	gz, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrBadArchive, err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	var unpacked int64
	entries := 0
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		h, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("%w: %v", ErrBadArchive, err)
		}
		if entries++; entries > maxEntries {
			return errorf(ErrTooLarge, "The archive has more than %d entries.", maxEntries)
		}
		if h.Size > 0 {
			if unpacked += h.Size; unpacked > maxUnpacked {
				return errorf(ErrTooLarge, "The archive unpacks to more than %d MB.", maxUnpacked>>20)
			}
		}
		if h.Typeflag == tar.TypeXGlobalHeader {
			continue
		}
		rel, err := archivePath(h.Name)
		if err != nil {
			return err
		}
		if rel == "" || (h.Typeflag != tar.TypeReg && h.Typeflag != tar.TypeRegA) {
			continue // folderul de sus, foldere, symlink-uri, hardlink-uri, device-uri, fifo
		}
		if strings.Contains(rel, `\`) || hasGitDir(rel) {
			continue
		}
		if err := fn(rel, h, tr); err != nil {
			if errors.Is(err, io.ErrUnexpectedEOF) {
				return fmt.Errorf("%w: %v", ErrBadArchive, err)
			}
			return err
		}
	}
}

// archivePath scoate primul nivel (<repo>-<sha>/) si verifica restul caii.
func archivePath(name string) (string, error) {
	if strings.HasPrefix(name, "/") || strings.ContainsRune(name, 0) {
		return "", errorf(ErrBadArchive, "The archive has an absolute path %q.", name)
	}
	parts := strings.Split(name, "/")
	for _, p := range parts {
		if p == ".." {
			return "", errorf(ErrBadArchive, "The archive has a path that leaves the folder: %q.", name)
		}
	}
	if strings.Contains(parts[0], ":") {
		return "", errorf(ErrBadArchive, "The archive has an absolute path %q.", name)
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

// writeEntry scrie size octeti din r in root/name, cu bitul executabil pastrat.
func writeEntry(root *os.Root, name string, r io.Reader, size int64, exec bool) error {
	if d := filepath.Dir(name); d != "." {
		if err := root.MkdirAll(d, 0o755); err != nil {
			return fmt.Errorf("%w: entry %q: %v", ErrBadArchive, filepath.ToSlash(name), err)
		}
	}
	mode := os.FileMode(0o644)
	if exec {
		mode = 0o755
	}
	out, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return fmt.Errorf("%w: entry %q: %v", ErrBadArchive, filepath.ToSlash(name), err)
	}
	_, err = io.CopyN(out, r, size)
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	return err
}

// pickFolder alege folderul skill-ului dintre folderele cu SKILL.md (folder -> continut).
// Radacina repo-ului ("") are numele repo-ului.
func pickFolder(skillMDs map[string][]byte, repo, id string) (string, bool) {
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
			n, _ := Frontmatter(skillMDs[d])
			return n == id
		},
	}
	for _, rule := range rules {
		var match []string
		for d := range skillMDs {
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
