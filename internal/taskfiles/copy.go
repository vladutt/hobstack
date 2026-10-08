// Package taskfiles copiaza fisierele predate de agenti pe un task (report_done files).
// Copierea o face foreman, deci verificam ca fisierul e intr-un folder al proiectului:
// altfel un agent ar putea "preda" orice fisier de pe masina, iar UI-ul l-ar servi.
package taskfiles

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"foreman/internal/store"
)

// MaxSize: cat poate avea un fisier predat (ca la upload).
const MaxSize = 50 << 20

// Copied descrie copia facuta.
type Copied struct {
	Path   string // calea de afisat: relativa la home cand e in el, altfel absoluta
	Name   string
	Stored string // numele copiei in destDir
	Mime   string
	Size   int64
}

var (
	errOutside    = errors.New("outside the project folders; put deliverables in your folder")
	errNotFound   = errors.New("not found")
	errNotRegular = errors.New("not a regular file")
	errTooLarge   = errors.New("larger than 50 MB")
	errEmpty      = errors.New("empty path")
)

// Copy copiaza fisierul raw (relativ la home sau absolut) in destDir.
// roots sunt folderele proiectului; fisierul trebuie sa fie sub unul din ele.
func Copy(roots []string, home, raw, destDir string) (Copied, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.ContainsRune(raw, 0) {
		return Copied{}, errEmpty
	}
	abs := raw
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(home, raw)
	}
	abs = filepath.Clean(abs)
	root, rel, ok := within(roots, abs)
	if !ok {
		// folderul poate fi salvat printr-un symlink (/var -> /private/var pe macOS), iar agentul
		// preda calea reala (sau invers): comparam caile rezolvate. Deschiderea ramane prin os.Root
		// pe radacina rezolvata, deci un symlink spre exterior e tot refuzat.
		root, rel, ok = within(resolveAll(roots), resolveParent(abs))
		if !ok {
			return Copied{}, errOutside
		}
	}
	display := abs
	if r, ok := relTo(home, abs); ok {
		display = r
	} else if r, ok := relTo(resolve(home), resolveParent(abs)); ok {
		display = r
	}

	// prin os.Root: ".." si symlink-urile care ies din radacina sunt refuzate de Go
	r, err := os.OpenRoot(root)
	if err != nil {
		return Copied{}, errNotFound
	}
	defer r.Close()
	lst, err := r.Lstat(rel)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Copied{}, errNotFound
		}
		return Copied{}, errOutside // de ex. o componenta e un symlink spre exterior
	}
	if !lst.Mode().IsRegular() {
		return Copied{}, errNotRegular // folder, symlink, FIFO, device
	}
	if lst.Size() > MaxSize {
		return Copied{}, errTooLarge
	}
	src, err := r.OpenFile(rel, os.O_RDONLY|oNonblock, 0)
	if err != nil {
		return Copied{}, errOutside
	}
	defer src.Close()

	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return Copied{}, err
	}
	name := store.CleanFileName(filepath.Base(abs))
	stored := randomPrefix() + "-" + name
	dst, err := os.OpenFile(filepath.Join(destDir, stored), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return Copied{}, err
	}
	// fisierul poate creste intre Lstat si citire: copiem cel mult MaxSize+1 octeti
	head := make([]byte, 512)
	n, _ := io.ReadFull(src, head)
	head = head[:n]
	size, err := io.Copy(dst, io.MultiReader(bytes.NewReader(head), io.LimitReader(src, MaxSize+1-int64(n))))
	cerr := dst.Close()
	if err == nil {
		err = cerr
	}
	if err == nil && size > MaxSize {
		err = errTooLarge
	}
	if err != nil {
		os.Remove(filepath.Join(destDir, stored))
		return Copied{}, err
	}
	return Copied{Path: display, Name: name, Stored: stored, Mime: detectMime(name, head), Size: size}, nil
}

// within cauta radacina care contine abs si intoarce calea relativa la ea.
func within(roots []string, abs string) (root, rel string, ok bool) {
	for _, r := range roots {
		if rel, ok := relTo(r, abs); ok {
			return filepath.Clean(r), rel, true
		}
	}
	return "", "", false
}

// resolve: calea fara symlink-uri (aceeasi cale daca nu se poate rezolva, de ex. folder sters).
func resolve(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return filepath.Clean(p)
}

// resolveParent rezolva doar folderul: un symlink in locul fisierului ramane symlink
// (si e refuzat mai departe ca "not a regular file").
func resolveParent(p string) string {
	return filepath.Join(resolve(filepath.Dir(p)), filepath.Base(p))
}

func resolveAll(roots []string) []string {
	out := make([]string, len(roots))
	for i, r := range roots {
		out[i] = resolve(r)
	}
	return out
}

// relTo: abs relativ la dir, doar daca e strict sub dir.
func relTo(dir, abs string) (string, bool) {
	if dir == "" {
		return "", false
	}
	rel, err := filepath.Rel(filepath.Clean(dir), abs)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", false
	}
	return rel, true
}

func detectMime(name string, head []byte) string {
	if m := mime.TypeByExtension(strings.ToLower(filepath.Ext(name))); m != "" {
		return m
	}
	return http.DetectContentType(head)
}

// randomPrefix: 16 caractere hex, ca doua predari ale aceluiasi fisier sa nu se suprascrie.
func randomPrefix() string {
	b := make([]byte, 8)
	rand.Read(b) // crypto/rand.Read nu esueaza (Go 1.24+)
	return hex.EncodeToString(b)
}
