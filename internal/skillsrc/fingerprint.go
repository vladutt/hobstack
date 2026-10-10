package skillsrc

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
)

// Fingerprint e amprenta continutului unui folder: sha256 peste (cale relativa + "\x00" +
// continut) pentru fiecare fisier obisnuit, in ordinea cailor. Folosita ca "ref" la sursele locale.
func Fingerprint(dir string) (string, error) {
	files, err := walkFiles(dir)
	if err != nil {
		return "", err
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return "", err
	}
	defer root.Close()
	h := sha256.New()
	for _, f := range files {
		in, err := root.Open(filepath.FromSlash(f.rel))
		if err != nil {
			return "", err
		}
		io.WriteString(h, f.rel+"\x00")
		_, err = io.Copy(h, in)
		in.Close()
		if err != nil {
			return "", err
		}
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// FileInfo descrie un fisier pregatit pentru install.
type FileInfo struct {
	Path   string `json:"path"` // relativa, cu /
	Size   int64  `json:"size"`
	Script bool   `json:"script"`
}

// scriptExt: extensiile considerate scripturi (pe langa bitul executabil).
var scriptExt = map[string]bool{
	".py": true, ".sh": true, ".js": true, ".mjs": true, ".ts": true, ".rb": true,
	".pl": true, ".ps1": true, ".bat": true, ".cmd": true,
}

// ListFiles: fisierele obisnuite din dir (fara symlink-uri si .git), sortate, cu marcajul Script.
func ListFiles(dir string) ([]FileInfo, error) {
	files, err := walkFiles(dir)
	if err != nil {
		return nil, err
	}
	out := make([]FileInfo, 0, len(files))
	for _, f := range files {
		script := scriptExt[strings.ToLower(path.Ext(f.rel))] || f.mode&0o111 != 0
		out = append(out, FileInfo{Path: f.rel, Size: f.size, Script: script})
	}
	return out, nil
}

// Frontmatter citeste name si description din frontmatter-ul YAML al unui SKILL.md
// (doar cheile de pe primul nivel; ghilimele simple/duble si blocuri > / | pe mai multe randuri).
// Fara frontmatter inchis cu "---": "", "".
func Frontmatter(skillMD []byte) (name, description string) {
	skillMD = bytes.TrimPrefix(skillMD, []byte("\ufeff"))
	sc := bufio.NewScanner(bytes.NewReader(skillMD))
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	if !sc.Scan() || strings.TrimSpace(sc.Text()) != "---" {
		return "", ""
	}
	values := map[string]string{}
	var block string // cheia unui bloc > sau | in curs
	var blockLines []string
	var folded bool
	endBlock := func() {
		if block == "" {
			return
		}
		sep := "\n"
		if folded {
			sep = " "
		}
		values[block] = strings.TrimSpace(strings.Join(blockLines, sep))
		block, blockLines = "", nil
	}
	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), "\r")
		if strings.TrimSpace(line) == "---" {
			endBlock()
			return values["name"], values["description"]
		}
		if block != "" {
			if line == "" || line[0] == ' ' || line[0] == '\t' {
				blockLines = append(blockLines, strings.TrimSpace(line))
				continue
			}
			endBlock()
		}
		if line == "" || line[0] == ' ' || line[0] == '\t' || line[0] == '#' {
			continue // randuri goale, chei imbricate, comentarii
		}
		key, val, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		val = strings.TrimSpace(val)
		if key != "name" && key != "description" {
			continue
		}
		if strings.HasPrefix(val, ">") || strings.HasPrefix(val, "|") {
			block, folded = key, val[0] == '>'
			continue
		}
		values[key] = unquote(val)
	}
	return "", ""
}

// unquote scoate ghilimelele YAML: "..." (cu escape-uri) sau '...' (” = ').
func unquote(v string) string {
	if len(v) >= 2 && v[0] == '"' && v[len(v)-1] == '"' {
		if s, err := strconv.Unquote(v); err == nil {
			return s
		}
		return v[1 : len(v)-1]
	}
	if len(v) >= 2 && v[0] == '\'' && v[len(v)-1] == '\'' {
		return strings.ReplaceAll(v[1:len(v)-1], "''", "'")
	}
	return v
}
