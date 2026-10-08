// Package library tine elementele refolosibile (tool-uri, skill-uri, servere MCP):
// copiaza propunerile agentilor, publica versiuni read-only si construieste
// pluginul Claude Code al fiecarui proiect.
package library

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"

	"foreman/internal/store"
)

const (
	maxFiles = 20
	maxBytes = 1 << 20 // 1 MB in total
)

// Lib lucreaza pe disc in Root (~/.foreman/library) si in store.
type Lib struct {
	Root string
	St   *store.Store

	mu       sync.Mutex // protejeaza harta de mai jos
	buildMus map[int64]*sync.Mutex
}

func New(root string, st *store.Store) *Lib {
	if abs, err := filepath.Abs(root); err == nil {
		root = abs
	}
	return &Lib{Root: root, St: st, buildMus: map[int64]*sync.Mutex{}}
}

// safeName: numele elementelor ajung in cai de pe disc, deci fara separatori sau "..".
var safeName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// envName: numele unui secret devine variabila de mediu.
var envName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// CopyProposal copiaza fisierele din folderul agentului in Root/proposals/<item-id>/.
// Fiecare cale trebuie sa fie relativa si sa ramana in srcDir: fara "..", fara cai absolute,
// fara symlink-uri (nici pe parcurs), fara foldere, cel mult 20 de fisiere si 1 MB.
func (l *Lib) CopyProposal(itemID int64, srcDir string, files []string) (dir string, nfiles, lines int, err error) {
	if len(files) == 0 {
		return "", 0, 0, errors.New("no files to copy")
	}
	if len(files) > maxFiles {
		return "", 0, 0, fmt.Errorf("too many files (max %d)", maxFiles)
	}
	// srcDir se rezolva o singura data; tot ce citim trebuie sa ramana sub el
	base, err := filepath.EvalSymlinks(srcDir)
	if err != nil {
		return "", 0, 0, fmt.Errorf("source folder: %w", err)
	}
	type file struct {
		rel  string
		data []byte
		mode fs.FileMode
	}
	var list []file
	seen := map[string]bool{}
	total := 0
	for _, raw := range files {
		rel, err := checkRel(raw)
		if err != nil {
			return "", 0, 0, err
		}
		if seen[rel] {
			return "", 0, 0, fmt.Errorf("duplicate file %q", raw)
		}
		seen[rel] = true
		fi, err := lstatPath(base, rel)
		if err != nil {
			return "", 0, 0, fmt.Errorf("%q: %w", raw, err)
		}
		total += int(fi.Size())
		if total > maxBytes {
			return "", 0, 0, errors.New("files are bigger than 1 MB in total")
		}
		data, err := readCapped(filepath.Join(base, rel), maxBytes)
		if err != nil {
			return "", 0, 0, fmt.Errorf("%q: %w", raw, err)
		}
		mode := fs.FileMode(0o644)
		if fi.Mode()&0o111 != 0 {
			mode = 0o755
		}
		list = append(list, file{rel, data, mode})
		lines += countLines(data)
	}

	// scriem intr-un folder temporar si abia apoi inlocuim propunerea veche
	dir = filepath.Join(l.Root, "proposals", strconv.FormatInt(itemID, 10))
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		return "", 0, 0, err
	}
	tmp, err := os.MkdirTemp(filepath.Dir(dir), strconv.FormatInt(itemID, 10)+".tmp-")
	if err != nil {
		return "", 0, 0, err
	}
	defer os.RemoveAll(tmp) // dupa rename nu mai exista
	for _, f := range list {
		dst := filepath.Join(tmp, f.rel)
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return "", 0, 0, err
		}
		if err := os.WriteFile(dst, f.data, f.mode); err != nil {
			return "", 0, 0, err
		}
		_ = os.Chmod(dst, f.mode) // umask-ul nu trebuie sa ne scoata bitul de executie
	}
	if err := os.RemoveAll(dir); err != nil {
		return "", 0, 0, err
	}
	if err := os.Rename(tmp, dir); err != nil {
		return "", 0, 0, err
	}
	return dir, len(list), lines, nil
}

// checkRel valideaza o cale relativa si o intoarce curatata.
func checkRel(raw string) (string, error) {
	if raw == "" || strings.ContainsRune(raw, 0) {
		return "", fmt.Errorf("bad path %q", raw)
	}
	if filepath.IsAbs(raw) || strings.HasPrefix(raw, "/") || strings.HasPrefix(raw, `\`) || filepath.VolumeName(raw) != "" {
		return "", fmt.Errorf("absolute path %q is not allowed", raw)
	}
	rel := filepath.Clean(raw)
	if rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path %q leaves the folder", raw)
	}
	return rel, nil
}

// lstatPath verifica fiecare componenta a caii (nu doar ultima): niciun symlink,
// parintii sunt foldere, iar fisierul e regular.
func lstatPath(base, rel string) (fs.FileInfo, error) {
	cur := base
	parts := strings.Split(rel, string(filepath.Separator))
	for i, p := range parts {
		cur = filepath.Join(cur, p)
		fi, err := os.Lstat(cur)
		if err != nil {
			return nil, err
		}
		if fi.Mode()&fs.ModeSymlink != 0 {
			return nil, errors.New("symlinks are not allowed")
		}
		if i < len(parts)-1 {
			if !fi.IsDir() {
				return nil, errors.New("parent is not a folder")
			}
			continue
		}
		if !fi.Mode().IsRegular() {
			return nil, errors.New("not a regular file")
		}
		return fi, nil
	}
	return nil, errors.New("empty path")
}

// readCapped citeste cel mult max+1 octeti, ca un fisier crescut intre timp sa nu umple memoria.
func readCapped(path string, max int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > max {
		return nil, errors.New("file is bigger than 1 MB")
	}
	return data, nil
}

func countLines(data []byte) int {
	n := bytes.Count(data, []byte("\n"))
	if len(data) > 0 && data[len(data)-1] != '\n' {
		n++
	}
	return n
}

// Publish face din propunere (peste versiunea curenta, daca exista) o versiune read-only,
// o marcheaza publicata si o instaleaza in proiectul sursa.
func (l *Lib) Publish(itemID int64, note string) (store.LibraryVersion, error) {
	it, err := l.St.GetLibraryItem(itemID)
	if err != nil {
		return store.LibraryVersion{}, err
	}
	if !safeName.MatchString(it.Name) {
		return store.LibraryVersion{}, fmt.Errorf("bad item name %q", it.Name)
	}
	prop := filepath.Join(l.Root, "proposals", strconv.FormatInt(itemID, 10))
	if fi, err := os.Stat(prop); err != nil || !fi.IsDir() {
		return store.LibraryVersion{}, errors.New("nothing to publish: no proposal files")
	}
	versions, err := l.St.ListLibraryVersions(itemID)
	if err != nil {
		return store.LibraryVersion{}, err
	}
	next := "1.0"
	var prev string
	if len(versions) > 0 {
		next = nextVersion(versions[0].Version)
		prev = versions[0].Dir
	}
	dest := filepath.Join(l.Root, "items", strconv.FormatInt(itemID, 10), next)
	if err := os.RemoveAll(dest); err != nil { // ramasita dintr-o publicare esuata
		return store.LibraryVersion{}, err
	}
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return store.LibraryVersion{}, err
	}
	if prev != "" {
		if _, err := os.Stat(prev); err == nil {
			if err := copyTree(prev, dest); err != nil {
				return store.LibraryVersion{}, err
			}
		}
	}
	if err := copyTree(prop, dest); err != nil {
		return store.LibraryVersion{}, err
	}
	nfiles, lines, err := freeze(dest)
	if err != nil {
		return store.LibraryVersion{}, err
	}
	v := store.LibraryVersion{ItemID: itemID, Version: next, Dir: dest, Files: nfiles, Lines: lines, Note: note}
	if err := l.St.AddLibraryVersion(&v); err != nil {
		return store.LibraryVersion{}, err
	}
	if err := l.St.SetLibraryStatus(itemID, store.ItemPublished); err != nil {
		return store.LibraryVersion{}, err
	}
	if it.SourceProjectID != 0 {
		if err := l.St.Install(it.SourceProjectID, itemID, v.ID); err != nil {
			return store.LibraryVersion{}, err
		}
	}
	_ = os.RemoveAll(prop)
	return v, nil
}

// nextVersion: "1.0" -> "1.1".
func nextVersion(v string) string {
	major, minor, ok := strings.Cut(v, ".")
	m, err := strconv.Atoi(minor)
	if !ok || err != nil {
		return v + ".1"
	}
	return major + "." + strconv.Itoa(m+1)
}

// copyTree copiaza fisierele regulare din src in dst (foldere create la nevoie), pastrand bitul de executie.
// Symlink-urile si fisierele ciudate sunt ignorate.
func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		fi, err := d.Info()
		if err != nil {
			return err
		}
		if !fi.Mode().IsRegular() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		mode := fs.FileMode(0o644)
		if fi.Mode()&0o111 != 0 {
			mode = 0o755
		}
		if err := os.WriteFile(target, data, mode); err != nil {
			return err
		}
		return os.Chmod(target, mode)
	})
}

// freeze pune fisierele read-only (0o444, scripturile 0o555) si numara fisierele si liniile.
func freeze(dir string) (nfiles, lines int, err error) {
	err = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		fi, err := d.Info()
		if err != nil {
			return err
		}
		mode := fs.FileMode(0o444)
		if fi.Mode()&0o111 != 0 {
			mode = 0o555
		}
		if err := os.Chmod(path, mode); err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		nfiles++
		lines += countLines(data)
		return nil
	})
	return
}

// Build e ce primeste un run: pluginul proiectului si ce se adauga pe linia de comanda si in tura.
type Build struct {
	PluginDir  string           // "" daca proiectul nu are skill-uri / tool-uri instalate
	MCPServers map[string]any   // intra in mcpServers din --mcp-config
	Allow      []string         // "mcp__<server>" pentru serverele active
	TurnLines  []string         // text pentru agent (fara valori de secrete)
	ToolPaths  map[string]int64 // calea comenzii -> item (pentru "Recent use")
}

func (l *Lib) projectMu(id int64) *sync.Mutex {
	l.mu.Lock()
	defer l.mu.Unlock()
	m := l.buildMus[id]
	if m == nil {
		m = &sync.Mutex{}
		l.buildMus[id] = m
	}
	return m
}

// Build reface pluginul proiectului in Root/build/<id>: scrie intr-un folder temporar si apoi il muta
// la locul lui, cu un mutex per proiect ca doua build-uri sa nu se calce.
func (l *Lib) Build(projectID int64) (Build, error) {
	mu := l.projectMu(projectID)
	mu.Lock()
	defer mu.Unlock()

	out := Build{MCPServers: map[string]any{}, ToolPaths: map[string]int64{}}
	installs, err := l.St.ListInstalls(projectID)
	if err != nil {
		return out, err
	}
	buildRoot := filepath.Join(l.Root, "build")
	if err := os.MkdirAll(buildRoot, 0o755); err != nil {
		return out, err
	}
	pid := strconv.FormatInt(projectID, 10)
	final := filepath.Join(buildRoot, pid)
	tmp, err := os.MkdirTemp(buildRoot, pid+".tmp-")
	if err != nil {
		return out, err
	}
	defer os.RemoveAll(tmp) // dupa rename nu mai exista

	var toolLines []string
	pluginItems := 0
	for _, in := range installs {
		if !in.Enabled {
			continue
		}
		it, err := l.St.GetLibraryItem(in.ItemID)
		if err != nil {
			log.Printf("library: item %d: %v", in.ItemID, err)
			continue
		}
		ver, ok, err := l.versionOf(it.ID, in.VersionID)
		if err != nil {
			return out, err
		}
		if !ok {
			log.Printf("library: install of item %d points to version %d, which is not one of its versions", it.ID, in.VersionID)
			continue
		}
		if !safeName.MatchString(it.Name) {
			log.Printf("library: skipping item with unsafe name %q", it.Name)
			continue
		}
		secrets, err := l.St.Secrets(projectID, it.ID)
		if err != nil {
			return out, err
		}
		switch it.Kind {
		case store.KindSkill:
			if !dirExists(ver.Dir) {
				log.Printf("library: version dir %s is missing, skipping %s", ver.Dir, it.Name)
				continue
			}
			if err := copyTree(ver.Dir, filepath.Join(tmp, "skills", it.Name)); err != nil {
				return out, err
			}
			pluginItems++
		case store.KindTool:
			if !dirExists(ver.Dir) {
				log.Printf("library: version dir %s is missing, skipping %s", ver.Dir, it.Name)
				continue
			}
			line, path, err := l.buildTool(tmp, final, it, ver.Dir, secrets)
			if err != nil {
				return out, err
			}
			if path == "" {
				out.TurnLines = append(out.TurnLines, line)
				continue
			}
			out.ToolPaths[path] = it.ID
			toolLines = append(toolLines, line)
			pluginItems++
		case store.KindMCP:
			if miss := missingSecret(it.Secrets, secrets); miss != "" {
				out.TurnLines = append(out.TurnLines, fmt.Sprintf("Library server %s is off: missing secret %s. Ask the user to add it in the project's Library settings.", it.Name, miss))
				continue
			}
			out.MCPServers[it.Name] = mcpServer(it, ver.Dir, secrets)
			out.Allow = append(out.Allow, "mcp__"+it.Name)
		}
	}
	if len(toolLines) > 0 {
		out.TurnLines = append(append([]string{"Library tools (run them with Bash):"}, toolLines...), out.TurnLines...)
	}

	if err := os.RemoveAll(final); err != nil {
		return out, err
	}
	if pluginItems == 0 {
		return out, nil
	}
	manifest, _ := json.Marshal(map[string]string{"name": "foreman-library", "version": "1",
		"description": "Foreman library for project " + pid})
	if err := os.MkdirAll(filepath.Join(tmp, ".claude-plugin"), 0o755); err != nil {
		return out, err
	}
	if err := os.WriteFile(filepath.Join(tmp, ".claude-plugin", "plugin.json"), manifest, 0o644); err != nil {
		return out, err
	}
	if err := os.Rename(tmp, final); err != nil {
		return out, err
	}
	out.PluginDir = final
	return out, nil
}

// versionOf cauta versiunea printre cele ale item-ului (instalarea nu e validata de store).
func (l *Lib) versionOf(itemID, versionID int64) (store.LibraryVersion, bool, error) {
	vs, err := l.St.ListLibraryVersions(itemID)
	if err != nil {
		return store.LibraryVersion{}, false, err
	}
	for _, v := range vs {
		if v.ID == versionID {
			return v, true, nil
		}
	}
	return store.LibraryVersion{}, false, nil
}

func dirExists(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.IsDir()
}

// missingSecret intoarce primul secret declarat care nu are valoare ("" daca sunt toate).
func missingSecret(declared []string, have map[string]string) string {
	for _, s := range declared {
		if have[s] == "" {
			return s
		}
	}
	return ""
}

// buildTool copiaza tool-ul in bin/<name>/ si intoarce linia pentru agent si calea comenzii.
// Daca tool-ul nu poate rula, calea e "" si linia explica de ce.
func (l *Lib) buildTool(tmp, final string, it store.LibraryItem, verDir string, secrets map[string]string) (line, path string, err error) {
	entry, err := checkRel(it.Entry)
	if err != nil {
		log.Printf("library: tool %s has a bad entry %q: %v", it.Name, it.Entry, err)
		return fmt.Sprintf("Library tool %s is off: it has no valid entry file.", it.Name), "", nil
	}
	if miss := missingSecret(it.Secrets, secrets); miss != "" {
		return fmt.Sprintf("Library tool %s is off: missing secret %s. Ask the user to add it in the project's Library settings.", it.Name, miss), "", nil
	}
	if len(it.Secrets) > 0 && runtime.GOOS == "windows" {
		return fmt.Sprintf("Library tool %s is off: secrets are not supported on Windows.", it.Name), "", nil
	}
	dst := filepath.Join(tmp, "bin", it.Name)
	if err := copyTree(verDir, dst); err != nil {
		return "", "", err
	}
	_ = os.Chmod(filepath.Join(dst, entry), 0o755)
	cmdPath := filepath.Join(final, "bin", it.Name, entry)
	if len(it.Secrets) > 0 {
		var sb strings.Builder
		sb.WriteString("#!/bin/sh\n")
		for _, name := range it.Secrets {
			if !envName.MatchString(name) {
				return fmt.Sprintf("Library tool %s is off: bad secret name.", it.Name), "", nil
			}
			sb.WriteString("export " + name + "=" + shQuote(secrets[name]) + "\n")
		}
		sb.WriteString("exec \"$(dirname \"$0\")/\"" + shQuote(entry) + " \"$@\"\n")
		run := filepath.Join(dst, "run")
		if err := os.WriteFile(run, []byte(sb.String()), 0o700); err != nil {
			return "", "", err
		}
		if err := os.Chmod(run, 0o700); err != nil {
			return "", "", err
		}
		cmdPath = filepath.Join(final, "bin", it.Name, "run")
	}
	return fmt.Sprintf("- %s: %s — run: %s", it.Name, it.Description, cmdPath), cmdPath, nil
}

// shQuote pune valoarea intre '...', cu ' scapat ca '\”.
func shQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func mcpServer(it store.LibraryItem, verDir string, secrets map[string]string) map[string]any {
	if it.Transport == "remote" {
		srv := map[string]any{"type": "http", "url": it.URL}
		if tok := secrets["AUTH_TOKEN"]; tok != "" {
			srv["headers"] = map[string]string{"Authorization": "Bearer " + tok}
		}
		return srv
	}
	args := make([]string, len(it.Args))
	for i, a := range it.Args {
		args[i] = strings.ReplaceAll(a, "${DIR}", verDir)
	}
	env := map[string]string{}
	for _, name := range it.Secrets {
		env[name] = secrets[name]
	}
	return map[string]any{"command": it.Command, "args": args, "env": env}
}
