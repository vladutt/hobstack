package library

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
)

// UserError e o eroare pe care utilizatorul o poate repara (nu o defectiune): API-ul o arata ca 422.
type UserError struct{ Msg string }

func (e UserError) Error() string { return e.Msg }

// Erorile de citire a unui fisier din Library.
var (
	ErrBadPath = errors.New("bad file path")
	ErrTooBig  = errors.New("file is too big to show")
)

// MaxViewBytes: cat se serveste dintr-un fisier al unui element.
const MaxViewBytes = 512 << 10

// TextFile e un fisier scris de utilizator in UI.
type TextFile struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

// ProposalDir e folderul propunerii unui element (exista doar cat e propus).
func (l *Lib) ProposalDir(itemID int64) string {
	return filepath.Join(l.Root, "proposals", strconv.FormatInt(itemID, 10))
}

// DiscardProposal sterge folderul propunerii (de ex. cand crearea unui element esueaza).
func (l *Lib) DiscardProposal(itemID int64) { os.RemoveAll(l.ProposalDir(itemID)) }

// WriteStaged scrie fisierele facute de utilizator intr-un folder de lucru (.staging-*), cu aceleasi limite ca
// CopyStaged. Caile trec prin checkRel si nu pot iesi din folder; sunt scrise doar in folderul proaspat creat.
// Chemati AdoptStaged sau stergeti folderul.
func (l *Lib) WriteStaged(files []TextFile) (staging string, nfiles, lines int, err error) {
	if len(files) > maxFiles {
		return "", 0, 0, UserError{fmt.Sprintf("too many files (max %d)", maxFiles)}
	}
	type item struct {
		rel  string
		data []byte
	}
	var list []item
	seen := map[string]bool{}
	total := 0
	for _, f := range files {
		rel, err := checkRel(f.Path)
		if err != nil {
			return "", 0, 0, UserError{err.Error()}
		}
		if seen[rel] {
			return "", 0, 0, UserError{fmt.Sprintf("duplicate file %q", f.Path)}
		}
		seen[rel] = true
		total += len(f.Content)
		if total > maxBytes {
			return "", 0, 0, UserError{"files are bigger than 1 MB in total"}
		}
		list = append(list, item{rel, []byte(f.Content)})
		lines += countLines([]byte(f.Content))
	}
	tmp, err := l.NewStaging()
	if err != nil {
		return "", 0, 0, err
	}
	for _, f := range list {
		dst := filepath.Join(tmp, f.rel)
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err == nil {
			err = os.WriteFile(dst, f.data, 0o644)
		}
		if err != nil {
			os.RemoveAll(tmp)
			return "", 0, 0, err
		}
	}
	return tmp, len(list), lines, nil
}

// FileInfo: un fisier dintr-un folder al unui element.
type FileInfo struct {
	Name string `json:"name"`
	Size int64  `json:"size"`
}

// ListFiles intoarce fisierele regulare din dir (relativ, cu '/'), sortate; symlink-urile se ignora.
func ListFiles(dir string) []FileInfo {
	out := []FileInfo{}
	_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || len(out) >= 200 {
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return nil
		}
		if fi, err := d.Info(); err == nil {
			out = append(out, FileInfo{Name: filepath.ToSlash(rel), Size: fi.Size()})
		}
		return nil
	})
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// ReadFileIn citeste un fisier regular din dir, cu calea rezolvata prin os.Root (nu iese din dir nici prin symlink).
// ErrBadPath: cale absoluta, cu "..", goala sau cu '\'. ErrTooBig: peste MaxViewBytes. Lipsa: os.ErrNotExist.
func ReadFileIn(dir, rel string) ([]byte, error) {
	if rel == "" {
		return nil, ErrBadPath
	}
	for _, c := range rel {
		if c == '\\' {
			return nil, ErrBadPath
		}
	}
	clean, err := checkRel(rel)
	if err != nil || clean != filepath.FromSlash(rel) {
		return nil, ErrBadPath // si cai ne-curate ("a//b", "./a"): aceeasi cale se cere intr-un singur fel
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	lst, err := root.Lstat(clean)
	if err != nil {
		return nil, err
	}
	if !lst.Mode().IsRegular() {
		return nil, os.ErrNotExist // folder, symlink sau fisier special: ca si cum nu ar exista
	}
	f, err := root.OpenFile(clean, os.O_RDONLY|oNonblock, 0)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !fi.Mode().IsRegular() || !os.SameFile(lst, fi) {
		return nil, os.ErrNotExist
	}
	data, err := io.ReadAll(io.LimitReader(f, MaxViewBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > MaxViewBytes {
		return nil, ErrTooBig
	}
	return data, nil
}
