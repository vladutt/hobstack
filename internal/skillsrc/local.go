package skillsrc

// Skill-urile deja instalate in Claude pe masina asta: Claude Code (~/.claude/skills),
// claude.ai (~/.claude/skills/synced/<cont>) si plugin-urile Claude Code instalate.

import (
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"hobstack/internal/library"
	"hobstack/internal/store"
)

// Tipurile de sursa locala (aceleasi valori ca store.Source*).
const (
	KindClaudeCode = "claude-code"
	KindClaudeAI   = "claude-ai"
	KindPlugin     = "plugin"
)

// LocalSkill e un skill gasit pe disc.
type LocalSkill struct {
	Source      string `json:"source"` // "Claude Code" | "claude.ai" | "Plugin: <nume>" (afisat in lista)
	Kind        string `json:"kind"`   // claude-code | claude-ai | plugin
	Label       string `json:"label"`  // eticheta salvata la sursa; la plugin-uri cu versiunea ("Plugin: superpowers 6.4.1")
	Name        string `json:"name"`   // numele folderului
	Description string `json:"description"`
	Path        string `json:"path"`                // calea absoluta a folderului (la un symlink din ~/.claude/skills, calea link-ului)
	PluginKey   string `json:"pluginKey,omitempty"` // doar la plugin-uri: cheia "nume@marketplace" din installed_plugins.json
}

// LocalSkills citeste cele trei surse din home. Fisierele lipsa sau stricate se ignora.
func LocalSkills(home string) []LocalSkill {
	var out []LocalSkill
	skillsDir := filepath.Join(home, ".claude", "skills")

	// Claude Code: ~/.claude/skills/<nume>, fara folderul special "synced"; aici (si doar aici) un symlink spre
	// un folder cu SKILL.md conteaza (ex. find-skills -> ../../.agents/skills/find-skills)
	for _, d := range skillDirs(skillsDir, true) {
		if d.name == "synced" {
			continue
		}
		out = append(out, LocalSkill{Source: "Claude Code", Kind: KindClaudeCode, Label: "Claude Code",
			Name: d.name, Description: d.desc, Path: d.path})
	}

	// claude.ai: ~/.claude/skills/synced/<cont>/<nume> + manifest.json pentru descrieri
	synced := filepath.Join(skillsDir, "synced")
	for _, acct := range subdirs(synced, false) {
		acctDir := filepath.Join(synced, acct)
		descs := manifestDescriptions(filepath.Join(acctDir, "manifest.json"))
		for _, d := range skillDirs(acctDir, false) {
			desc := d.desc
			if m, ok := descs[d.name]; ok && m != "" {
				desc = m
			}
			out = append(out, LocalSkill{Source: "claude.ai", Kind: KindClaudeAI, Label: "claude.ai",
				Name: d.name, Description: desc, Path: d.path})
		}
	}

	// plugin-uri: installed_plugins.json -> <installPath>/skills/<nume> (prima instalare a fiecarei chei)
	seen := map[string]bool{}
	for _, p := range installedPlugins(pluginsFile(home)) {
		if seen[p.path] {
			continue
		}
		seen[p.path] = true
		for _, d := range skillDirs(filepath.Join(p.path, "skills"), false) {
			out = append(out, LocalSkill{Source: "Plugin: " + p.name, Kind: KindPlugin, Label: p.label(),
				Name: d.name, Description: d.desc, Path: d.path, PluginKey: p.key})
		}
	}
	return out
}

func pluginsFile(home string) string {
	return filepath.Join(home, ".claude", "plugins", "installed_plugins.json")
}

// ResolveLocal intoarce folderul de acum al unei surse locale si eticheta ei. La plugin-uri sursa e salvata
// dupa cheie (Repo = "nume@marketplace", Path = numele folderului skill-ului): folderul vine din installPath-ul
// de acum din installed_plugins.json (prima instalare a cheii), deci o versiune noua a plugin-ului se vede, iar
// eticheta are versiunea noua. Plugin dezinstalat = ErrNotFound. Sursele de plugin mai vechi (Repo gol) si
// celelalte tipuri au calea absoluta in Path.
func ResolveLocal(home string, src store.LibrarySource) (dir, label string, err error) {
	if src.Kind != store.SourcePlugin || src.Repo == "" {
		return src.Path, src.Label, nil
	}
	if !ValidSkillID(src.Path) {
		return "", "", fmt.Errorf("invalid plugin skill %q", src.Path)
	}
	if home != "" {
		for _, p := range installedPlugins(pluginsFile(home)) {
			if p.key == src.Repo {
				return filepath.Join(p.path, "skills", src.Path), p.label(), nil
			}
		}
	}
	return "", "", errorf(ErrNotFound, "Source no longer on this machine.")
}

type skillDir struct{ name, path, desc string }

// skillDirs intoarce, sortate, folderele din dir care au SKILL.md obisnuit. links=true: si symlink-urile spre
// un folder (calea ramane cea a link-ului); symlink-urile din interiorul skill-ului se sar oricum la copiere.
func skillDirs(dir string, links bool) []skillDir {
	var out []skillDir
	for _, name := range subdirs(dir, links) {
		p := filepath.Join(dir, name)
		fi, err := os.Lstat(filepath.Join(p, "SKILL.md"))
		if err != nil || !fi.Mode().IsRegular() {
			continue
		}
		data, err := readCapped(filepath.Join(p, "SKILL.md"), maxSkillMD)
		if err != nil {
			continue
		}
		_, desc := Frontmatter(data)
		out = append(out, skillDir{name: name, path: p, desc: desc})
	}
	return out
}

// subdirs: numele folderelor din dir, sortate; symlink-urile conteaza ca foldere doar cu links=true si doar
// cand duc la un folder (un link rupt sau spre un fisier se ignora).
func subdirs(dir string, links bool) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".") {
			continue
		}
		isDir := e.Type().IsDir()
		if !isDir && links && e.Type()&fs.ModeSymlink != 0 {
			fi, err := os.Stat(filepath.Join(dir, e.Name()))
			isDir = err == nil && fi.IsDir()
		}
		if isDir {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out
}

// manifestDescriptions: nume (si skillId) -> descriere din manifest.json; manifest lipsa/stricat = gol.
func manifestDescriptions(file string) map[string]string {
	data, err := readCapped(file, maxJSON)
	if err != nil {
		return nil
	}
	var m struct {
		Skills []struct {
			SkillID     string `json:"skillId"`
			Name        string `json:"name"`
			Description string `json:"description"`
		} `json:"skills"`
	}
	if json.Unmarshal(data, &m) != nil {
		return nil
	}
	out := map[string]string{}
	for _, s := range m.Skills {
		if s.SkillID != "" {
			out[s.SkillID] = s.Description
		}
		if s.Name != "" {
			out[s.Name] = s.Description
		}
	}
	return out
}

type plugin struct{ key, name, version, path string }

// label: eticheta salvata la sursa, cu versiunea ("Plugin: superpowers 6.4.1").
func (p plugin) label() string {
	if p.version == "" {
		return "Plugin: " + p.name
	}
	return "Plugin: " + p.name + " " + p.version
}

// installedPlugins citeste installed_plugins.json, sortat dupa cheie, cu prima instalare valida a fiecarei
// chei (aceeasi pe care o foloseste ResolveLocal); fisier invalid = nimic.
func installedPlugins(file string) []plugin {
	data, err := readCapped(file, maxJSON)
	if err != nil {
		return nil
	}
	var f struct {
		Plugins map[string][]struct {
			InstallPath string `json:"installPath"`
			Version     string `json:"version"`
		} `json:"plugins"`
	}
	if json.Unmarshal(data, &f) != nil {
		return nil
	}
	keys := make([]string, 0, len(f.Plugins))
	for k := range f.Plugins {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var out []plugin
	for _, k := range keys {
		name, _, _ := strings.Cut(k, "@")
		for _, inst := range f.Plugins[k] {
			if inst.InstallPath == "" || !filepath.IsAbs(inst.InstallPath) {
				continue
			}
			out = append(out, plugin{key: k, name: name, version: inst.Version, path: filepath.Clean(inst.InstallPath)})
			break
		}
	}
	return out
}

// resolveDir rezolva o data symlink-urile din calea folderului (ex. ~/.claude/skills/<nume> -> alt folder);
// de aici totul se citeste prin os.OpenRoot pe rezultat, iar symlink-urile din interior se sar.
func resolveDir(dir string) (string, error) {
	return filepath.EvalSymlinks(dir)
}

// CopyLocal copiaza folderul unui skill local in destDir: citit prin os.OpenRoot pe src (rezolvat o data daca
// e symlink), doar fisiere obisnuite (fara symlink-uri si .git), cu limitele unui skill din Library.
func CopyLocal(src, destDir string) error {
	src, err := resolveDir(src)
	if err != nil {
		return err
	}
	// folderul de acum (si tinta unui symlink) trebuie sa fie tot un skill: un link schimbat intre timp
	// spre alt folder nu se copiaza
	if fi, err := os.Lstat(filepath.Join(src, "SKILL.md")); err != nil || !fi.Mode().IsRegular() {
		return errorf(ErrNotFound, "This folder has no SKILL.md.")
	}
	files, err := walkFiles(src)
	if err != nil {
		return err
	}
	maxFiles, maxBytes := library.Limits(store.KindSkill)
	var total int64
	for _, f := range files {
		total += f.size
	}
	if len(files) > maxFiles {
		return errorf(ErrTooLarge, "The skill has more than %d files.", maxFiles)
	}
	if total > maxBytes {
		return errorf(ErrTooLarge, "The skill is larger than %d MB.", maxBytes>>20)
	}
	root, err := os.OpenRoot(src)
	if err != nil {
		return err
	}
	defer root.Close()
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return err
	}
	dst, err := os.OpenRoot(destDir)
	if err != nil {
		return err
	}
	defer dst.Close()
	// fisierele se pot schimba intre listare si copiere: copiem cel mult cat a mai ramas din limita
	left := maxBytes
	for _, f := range files {
		name := filepath.FromSlash(f.rel)
		n, err := copyLimited(root, name, dst, name, f.mode&0o111 != 0, left)
		if err != nil {
			return err
		}
		left -= n
	}
	return nil
}

func copyLimited(src *os.Root, from string, dst *os.Root, to string, exec bool, left int64) (int64, error) {
	in, err := src.Open(from)
	if err != nil {
		return 0, err
	}
	defer in.Close()
	if fi, err := in.Stat(); err != nil || !fi.Mode().IsRegular() {
		return 0, fmt.Errorf("%s is not a regular file", filepath.ToSlash(from))
	}
	if d := filepath.Dir(to); d != "." {
		if err := dst.MkdirAll(d, 0o755); err != nil {
			return 0, err
		}
	}
	mode := os.FileMode(0o644)
	if exec {
		mode = 0o755
	}
	out, err := dst.OpenFile(to, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return 0, err
	}
	n, err := io.Copy(out, io.LimitReader(in, left+1))
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err == nil && n > left {
		_, maxBytes := library.Limits(store.KindSkill)
		err = errorf(ErrTooLarge, "The skill is larger than %d MB.", maxBytes>>20)
	}
	return n, err
}

// localFile e un fisier obisnuit dintr-un folder (cale relativa cu /).
type localFile struct {
	rel  string
	size int64
	mode fs.FileMode
}

// walkFiles parcurge dir prin os.OpenRoot: doar fisiere obisnuite, fara symlink-uri si .git, sortate.
func walkFiles(dir string) ([]localFile, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	var out []localFile
	err = fs.WalkDir(root.FS(), ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".git" {
				return fs.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil // symlink, device, socket...
		}
		fi, err := d.Info()
		if err != nil {
			return err
		}
		out = append(out, localFile{rel: p, size: fi.Size(), mode: fi.Mode()})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool { return out[i].rel < out[j].rel })
	return out, nil
}
