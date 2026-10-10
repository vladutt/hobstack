package skillsrc

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

const testSHA = "1ed29a0"

// tarEntry e o intrare din arhiva de test; Type 0 = fisier obisnuit.
type tarEntry struct {
	Name string
	Body string
	Size int64 // daca e > 0, continutul e Size octeti de zero (fisiere mari)
	Type byte
	Link string
	Mode int64
}

// writeTar scrie intrarile ca tar.gz in w, cu nivelul de compresie dat.
// Intoarce eroarea (nu t.Fatal), ca sa poata fi chemat si din handler-ul serverului de test.
func writeTar(w io.Writer, level int, entries []tarEntry) error {
	gz, err := gzip.NewWriterLevel(w, level)
	if err != nil {
		return err
	}
	tw := tar.NewWriter(gz)
	// ca la GitHub: un header global pax cu commit-ul
	if err := tw.WriteHeader(&tar.Header{Typeflag: tar.TypeXGlobalHeader, Name: "pax_global_header",
		PAXRecords: map[string]string{"comment": testSHA}}); err != nil {
		return err
	}
	for _, e := range entries {
		h := &tar.Header{Name: e.Name, Mode: e.Mode, Typeflag: e.Type, Linkname: e.Link}
		if h.Mode == 0 {
			h.Mode = 0o644
		}
		if h.Typeflag == 0 {
			h.Typeflag = tar.TypeReg
		}
		var body io.Reader = strings.NewReader(e.Body)
		if h.Typeflag == tar.TypeReg {
			h.Size = int64(len(e.Body))
			if e.Size > 0 {
				h.Size = e.Size
				body = io.LimitReader(zeros{}, e.Size)
			}
		}
		if err := tw.WriteHeader(h); err != nil {
			return err
		}
		if h.Typeflag == tar.TypeReg {
			if _, err := io.Copy(tw, body); err != nil {
				return err
			}
		}
	}
	if err := tw.Close(); err != nil {
		return err
	}
	return gz.Close()
}

type zeros struct{}

func (zeros) Read(p []byte) (int, error) {
	clear(p)
	return len(p), nil
}

// tarball construieste arhiva in memorie; numele primesc prefixul "<repo>-<sha>/".
func tarball(t *testing.T, entries ...tarEntry) []byte {
	t.Helper()
	for i := range entries {
		if !strings.HasPrefix(entries[i].Name, "/") && !strings.HasPrefix(entries[i].Name, "!") {
			entries[i].Name = "skills-" + testSHA + "/" + entries[i].Name
		}
		entries[i].Name = strings.TrimPrefix(entries[i].Name, "!")
	}
	var buf bytes.Buffer
	if err := writeTar(&buf, gzip.DefaultCompression, entries); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// fetchRemote serveste arhiva data la /cl/o/skills/tar.gz/<sha>.
func fetchRemote(t *testing.T, archive []byte) *Remote {
	return testRemote(t, func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path != "/cl/o/skills/tar.gz/"+testSHA {
			http.NotFound(w, req)
			return
		}
		w.Write(archive)
	})
}

func skill(name string) string {
	return "---\nname: " + name + "\ndescription: d\n---\n"
}

// listDir intoarce caile relative (cu /) ale fisierelor din dir.
func listDir(t *testing.T, dir string) []string {
	t.Helper()
	var out []string
	filepath.Walk(dir, func(p string, fi os.FileInfo, err error) error {
		if err == nil && !fi.IsDir() {
			rel, _ := filepath.Rel(dir, p)
			out = append(out, filepath.ToSlash(rel))
		}
		return nil
	})
	sort.Strings(out)
	return out
}

func fetch(t *testing.T, r *Remote, id string) (string, string, error) {
	t.Helper()
	dest := filepath.Join(t.TempDir(), "dest")
	p, err := r.Fetch(context.Background(), "o/skills", testSHA, id, dest)
	return p, dest, err
}

func TestFetchSkillsDirFirst(t *testing.T) {
	r := fetchRemote(t, tarball(t,
		tarEntry{Name: "README.md", Body: "readme"},
		tarEntry{Name: "pdf/SKILL.md", Body: skill("pdf")},
		tarEntry{Name: "skills/pdf/SKILL.md", Body: skill("pdf")},
		tarEntry{Name: "skills/pdf/scripts/run.py", Body: "print(1)", Mode: 0o755},
		tarEntry{Name: "skills/pdf/.git/config", Body: "x"},
		tarEntry{Name: "skills/pdf/lnk", Type: tar.TypeSymlink, Link: "/etc/passwd"},
		tarEntry{Name: "skills/pdf/hard", Type: tar.TypeLink, Link: "skills-" + testSHA + "/README.md"},
		tarEntry{Name: "skills/pdf/fifo", Type: tar.TypeFifo},
		tarEntry{Name: "skills/docx/SKILL.md", Body: skill("docx")},
	))
	p, dest, err := fetch(t, r, "pdf")
	if err != nil {
		t.Fatal(err)
	}
	if p != "skills/pdf" {
		t.Errorf("path = %q", p)
	}
	got := listDir(t, dest)
	if strings.Join(got, ",") != "SKILL.md,scripts/run.py" {
		t.Errorf("files = %v", got)
	}
	files, err := ListFiles(dest)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 || !files[1].Script || files[0].Script {
		t.Errorf("ListFiles = %+v", files)
	}
}

func TestFetchNamedFolderThenFrontmatter(t *testing.T) {
	r := fetchRemote(t, tarball(t,
		tarEntry{Name: "deep/a/tool/SKILL.md", Body: skill("tool")},
		tarEntry{Name: "x/tool/SKILL.md", Body: skill("tool")},
		tarEntry{Name: "x/tool/a.txt", Body: "a"},
		tarEntry{Name: "plugins/b/other/SKILL.md", Body: skill("helper")},
		tarEntry{Name: "plugins/b/other/h.md", Body: "h"},
		tarEntry{Name: "plugins/a/zz/SKILL.md", Body: skill("helper")},
	))
	// regula 2: folder numit "tool", cel mai scurt castiga
	p, dest, err := fetch(t, r, "tool")
	if err != nil || p != "x/tool" {
		t.Fatalf("tool: %q %v", p, err)
	}
	if got := listDir(t, dest); strings.Join(got, ",") != "SKILL.md,a.txt" {
		t.Errorf("files = %v", got)
	}
	// regula 3: frontmatter name; aceeasi lungime -> alfabetic
	p, _, err = fetch(t, r, "helper")
	if err != nil || p != "plugins/a/zz" {
		t.Fatalf("helper: %q %v", p, err)
	}
	// niciunul
	_, _, err = fetch(t, r, "missing")
	if !errors.Is(err, ErrNotFound) || !strings.Contains(err.Error(), "Couldn't find the skill missing in o/skills") {
		t.Errorf("missing: %v", err)
	}
}

func TestFetchRootSkill(t *testing.T) {
	r := fetchRemote(t, tarball(t,
		tarEntry{Name: "SKILL.md", Body: skill("skills")},
		tarEntry{Name: "ref.md", Body: "r"},
	))
	p, dest, err := fetch(t, r, "skills")
	if err != nil || p != "" {
		t.Fatalf("root: %q %v", p, err)
	}
	if got := listDir(t, dest); strings.Join(got, ",") != "SKILL.md,ref.md" {
		t.Errorf("files = %v", got)
	}
}

func TestFetchHostile(t *testing.T) {
	for name, e := range map[string]tarEntry{
		"dotdot":   {Name: "skills/pdf/../../evil", Body: "x"},
		"absolute": {Name: "/etc/evil", Body: "x"},
		"top-dots": {Name: "!../evil", Body: "x"},
	} {
		t.Run(name, func(t *testing.T) {
			r := fetchRemote(t, tarball(t, tarEntry{Name: "skills/pdf/SKILL.md", Body: skill("pdf")}, e))
			_, dest, err := fetch(t, r, "pdf")
			if err == nil {
				t.Fatal("hostile archive accepted")
			}
			if !strings.Contains(err.Error(), "path") {
				t.Errorf("unexpected error: %v", err)
			}
			if got := listDir(t, filepath.Dir(dest)); len(got) != 0 {
				t.Errorf("files written: %v", got)
			}
		})
	}
}

func TestFetchLimits(t *testing.T) {
	many := []tarEntry{{Name: "skills/big/SKILL.md", Body: skill("big")}}
	for i := 0; i < 200; i++ {
		many = append(many, tarEntry{Name: fmt.Sprintf("skills/big/f%03d.txt", i), Body: "x"})
	}
	// 200 de fisiere in alt folder nu conteaza
	other := append([]tarEntry{}, many...)
	for i := range other {
		other[i].Name = strings.Replace(other[i].Name, "skills/big/", "skills/other/", 1)
	}
	r := fetchRemote(t, tarball(t, append(append(many, other...), tarEntry{Name: "skills/ok/SKILL.md", Body: skill("ok")})...))
	if _, _, err := fetch(t, r, "big"); !errors.Is(err, ErrTooLarge) || !strings.Contains(err.Error(), "200 files") {
		t.Errorf("201 files: %v", err)
	}
	if _, _, err := fetch(t, r, "ok"); err != nil {
		t.Errorf("small skill next to big ones: %v", err)
	}

	r = fetchRemote(t, tarball(t,
		tarEntry{Name: "skills/huge/SKILL.md", Body: skill("huge")},
		tarEntry{Name: "skills/huge/blob.bin", Size: 11 << 20},
	))
	if _, _, err := fetch(t, r, "huge"); !errors.Is(err, ErrTooLarge) || !strings.Contains(err.Error(), "10 MB") {
		t.Errorf("11 MB: %v", err)
	}
}

func TestFetchArchiveOver50MB(t *testing.T) {
	r := testRemote(t, func(w http.ResponseWriter, req *http.Request) {
		// fara compresie, ca arhiva sa treaca de 50 MB
		_ = writeTar(w, gzip.NoCompression, []tarEntry{
			{Name: "skills-" + testSHA + "/skills/pdf/SKILL.md", Body: skill("pdf")},
			{Name: "skills-" + testSHA + "/big.bin", Size: 51 << 20},
		})
	})
	_, _, err := fetch(t, r, "pdf")
	if !errors.Is(err, ErrTooLarge) || !strings.Contains(err.Error(), "50 MB") {
		t.Errorf("err = %v", err)
	}
}

func TestFetchUnpackedLimit(t *testing.T) {
	old := maxUnpacked
	maxUnpacked = 1 << 20
	t.Cleanup(func() { maxUnpacked = old })
	r := fetchRemote(t, tarball(t,
		tarEntry{Name: "skills/pdf/SKILL.md", Body: skill("pdf")},
		tarEntry{Name: "zeros.bin", Size: 2 << 20}, // se comprima la cativa KB
	))
	if _, _, err := fetch(t, r, "pdf"); !errors.Is(err, ErrTooLarge) {
		t.Errorf("err = %v", err)
	}
}

func TestFetchErrors(t *testing.T) {
	r := testRemote(t, func(w http.ResponseWriter, req *http.Request) {
		switch req.URL.Path {
		case "/cl/o/limit/tar.gz/" + testSHA:
			http.Error(w, "slow", 429)
		case "/cl/o/broken/tar.gz/" + testSHA:
			w.Write([]byte("not a gzip"))
		default:
			http.NotFound(w, req)
		}
	})
	ctx := context.Background()
	dest := t.TempDir()
	if _, err := r.Fetch(ctx, "o/limit", testSHA, "pdf", dest); !errors.Is(err, ErrRateLimited) {
		t.Errorf("429: %v", err)
	}
	if _, err := r.Fetch(ctx, "o/broken", testSHA, "pdf", dest); !errors.Is(err, ErrUnavailable) {
		t.Errorf("broken: %v", err)
	}
	if _, err := r.Fetch(ctx, "o/missing", testSHA, "pdf", dest); !errors.Is(err, ErrNotFound) {
		t.Errorf("404: %v", err)
	}
	if _, err := r.Fetch(ctx, "o/x", "../x", "pdf", dest); err == nil {
		t.Error("bad sha accepted")
	}
	if _, err := r.Fetch(ctx, "o/x", testSHA, "../pdf", dest); err == nil {
		t.Error("bad skill id accepted")
	}
}
