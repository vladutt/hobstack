package web

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"hobstack/internal/skillsrc"
	"hobstack/internal/store"
)

// fakeGitHub tine starea serverului de test pentru skills.sh si GitHub (api, codeload, raw).
type fakeGitHub struct {
	mu        sync.Mutex
	head      map[string]string            // repo -> sha (commits/HEAD)
	pathSHA   map[string]string            // repo + ":" + path -> sha (commits?path=)
	archives  map[string]map[string]string // repo + "@" + sha -> fisiere (cale in repo -> continut)
	execs     map[string]bool              // cai in repo cu bitul executabil
	hits      []skillsrc.Hit
	official  []string          // folderele din anthropics/skills/skills
	officialD map[string]string // nume -> SKILL.md
	searchSt  int               // != 0: statusul raspunsului la cautare
	limited   bool              // API-ul raspunde 403 cu X-RateLimit-Remaining: 0
	pathRefs  []string          // parametrul sha din cererile commits?path=
	apiCalls  int
}

func (f *fakeGitHub) set(fn func(f *fakeGitHub)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fn(f)
}

func (f *fakeGitHub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	p := r.URL.Path
	switch {
	case p == "/sk/api/search":
		if f.searchSt != 0 {
			w.WriteHeader(f.searchSt)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"skills": f.hits})
	case strings.HasPrefix(p, "/api/"):
		f.apiCalls++
		if f.limited {
			w.Header().Set("X-RateLimit-Remaining", "0")
			w.WriteHeader(http.StatusForbidden)
			return
		}
		rest := strings.TrimPrefix(p, "/api/repos/")
		if rest == "anthropics/skills/contents/skills" {
			var out []map[string]string
			for _, n := range f.official {
				out = append(out, map[string]string{"name": n, "path": "skills/" + n, "type": "dir"})
			}
			json.NewEncoder(w).Encode(out)
			return
		}
		if repo, ok := strings.CutSuffix(rest, "/commits/HEAD"); ok {
			sha, ok := f.head[repo]
			if !ok {
				http.NotFound(w, r)
				return
			}
			json.NewEncoder(w).Encode(map[string]string{"sha": sha})
			return
		}
		if repo, ok := strings.CutSuffix(rest, "/commits"); ok {
			f.pathRefs = append(f.pathRefs, r.URL.Query().Get("sha"))
			sha, ok := f.pathSHA[repo+":"+r.URL.Query().Get("path")]
			if !ok {
				json.NewEncoder(w).Encode([]any{})
				return
			}
			json.NewEncoder(w).Encode([]map[string]string{{"sha": sha}})
			return
		}
		http.NotFound(w, r)
	case strings.HasPrefix(p, "/raw/anthropics/skills/HEAD/skills/"):
		name := strings.TrimSuffix(strings.TrimPrefix(p, "/raw/anthropics/skills/HEAD/skills/"), "/SKILL.md")
		body, ok := f.officialD[name]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte(body))
	case strings.HasPrefix(p, "/cl/"):
		// /cl/<owner>/<repo>/tar.gz/<sha>
		parts := strings.Split(strings.TrimPrefix(p, "/cl/"), "/")
		if len(parts) != 4 || parts[2] != "tar.gz" {
			http.NotFound(w, r)
			return
		}
		repo, sha := parts[0]+"/"+parts[1], parts[3]
		files, ok := f.archives[repo+"@"+sha]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Write(buildTarGz(parts[1]+"-"+sha, files, f.execs))
	default:
		http.NotFound(w, r)
	}
}

// buildTarGz face o arhiva ca la GitHub: totul sub un folder "<repo>-<sha>/".
func buildTarGz(prefix string, files map[string]string, execs map[string]bool) []byte {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		mode := int64(0o644)
		if execs[n] {
			mode = 0o755
		}
		tw.WriteHeader(&tar.Header{Name: prefix + "/" + n, Mode: mode, Size: int64(len(files[n])), Typeflag: tar.TypeReg})
		tw.Write([]byte(files[n]))
	}
	tw.Close()
	gz.Close()
	return buf.Bytes()
}

const (
	shaOne   = "1111111aaaa"
	shaTwo   = "2222222bbbb"
	shaPath1 = "3333333cccc" // ultimul commit pe skills/pdf la install
	shaPath2 = "4444444dddd" // un commit nou pe skills/pdf
)

func pdfSkill(desc string) string {
	return "---\nname: pdf\ndescription: " + desc + "\n---\n# PDF\n"
}

type skillEnv struct {
	*libEnv
	gh   *fakeGitHub
	home string
	now  time.Time
	nowM sync.Mutex
}

func (e *skillEnv) advance(d time.Duration) {
	e.nowM.Lock()
	e.now = e.now.Add(d)
	e.nowM.Unlock()
}

func setupSkills(t *testing.T) *skillEnv {
	t.Helper()
	e := &skillEnv{libEnv: setupLib(t), home: t.TempDir(), now: time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)}
	e.gh = &fakeGitHub{
		head:    map[string]string{"o/skills": shaOne},
		pathSHA: map[string]string{"o/skills:skills/pdf": shaPath1},
		archives: map[string]map[string]string{
			"o/skills@" + shaOne: {
				"README.md":                 "repo",
				"skills/pdf/SKILL.md":       pdfSkill("Work with PDF files."),
				"skills/pdf/scripts/run.py": "print(1)\n",
				"skills/pdf/bin/tool":       "#!/bin/sh\necho hi\n",
				"skills/pdf/notes.md":       "old notes\n",
				"skills/pdf/gone.txt":       "will be removed\n",
				"skills/docx/SKILL.md":      "---\nname: docx\ndescription: d\n---\n",
			},
		},
		execs:     map[string]bool{"skills/pdf/bin/tool": true},
		official:  []string{"pdf", "xlsx"},
		officialD: map[string]string{"pdf": pdfSkill("Official PDF."), "xlsx": "---\nname: xlsx\ndescription: Sheets.\n---\n"},
	}
	// update-ul: un commit nou pe skills/pdf (notes modificat, gone sters, new adaugat)
	e.gh.archives["o/skills@"+shaPath2] = map[string]string{
		"skills/pdf/SKILL.md":       pdfSkill("Work with PDF files."),
		"skills/pdf/scripts/run.py": "print(1)\n",
		"skills/pdf/bin/tool":       "#!/bin/sh\necho hi\n",
		"skills/pdf/notes.md":       "new notes\n",
		"skills/pdf/new.txt":        "added\n",
	}
	// arhiva commit-ului de pe cale (ref-ul salvat) are acelasi folder ca HEAD
	e.gh.archives["o/skills@"+shaPath1] = e.gh.archives["o/skills@"+shaOne]
	ts := httptest.NewServer(e.gh)
	t.Cleanup(ts.Close)
	r := skillsrc.NewRemote()
	r.SkillsURL, r.APIURL, r.CodeloadURL, r.RawURL = ts.URL+"/sk", ts.URL+"/api", ts.URL+"/cl", ts.URL+"/raw"
	e.srv.skills = r
	e.srv.home = func() (string, error) { return e.home, nil }
	e.srv.now = func() time.Time {
		e.nowM.Lock()
		defer e.nowM.Unlock()
		return e.now
	}
	return e
}

func errMsg(raw []byte) string {
	var out map[string]any
	json.Unmarshal(raw, &out)
	s, _ := out["error"].(string)
	return s
}

type prepOut struct {
	Token       string              `json:"token"`
	Name        string              `json:"name"`
	Description string              `json:"description"`
	Files       []skillsrc.FileInfo `json:"files"`
	Source      store.LibrarySource `json:"source"`
	Diff        []map[string]any    `json:"diff"`
}

func (e *skillEnv) prepare(t *testing.T, body any) prepOut {
	t.Helper()
	code, raw := e.do(t, "POST", "/api/library/skills/prepare", body)
	if code != http.StatusOK {
		t.Fatalf("prepare: %d %s", code, raw)
	}
	var out prepOut
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func (e *skillEnv) prepareGitHub(t *testing.T) prepOut {
	return e.prepare(t, map[string]string{"kind": "github", "repo": "o/skills", "skillId": "pdf"})
}

type itemOut struct {
	store.LibraryItem
	Version         *store.LibraryVersion `json:"version"`
	Installs        int                   `json:"installs"`
	Source          *store.LibrarySource  `json:"source"`
	UpdateAvailable bool                  `json:"updateAvailable"`
}

func (e *skillEnv) install(t *testing.T, body any) itemOut {
	t.Helper()
	code, raw := e.do(t, "POST", "/api/library/skills/install", body)
	if code != http.StatusCreated {
		t.Fatalf("install: %d %s", code, raw)
	}
	var out itemOut
	json.Unmarshal(raw, &out)
	return out
}

func TestSkillSearchInLibrary(t *testing.T) {
	e := setupSkills(t)
	e.gh.set(func(f *fakeGitHub) {
		f.hits = []skillsrc.Hit{
			{ID: "o/skills/pdf", Name: "pdf", Source: "o/skills", SkillID: "pdf", Installs: 10},
			{ID: "o/skills/docx", Name: "docx", Source: "o/skills", SkillID: "docx", Installs: 5},
			{ID: "other/x/foo", Name: "Foo Tool", Source: "other/x", SkillID: "foo", Installs: 1},
		}
	})
	// "pdf" e in Library dupa nume; foo dupa sursa (alt nume)
	pdf := store.LibraryItem{Kind: store.KindSkill, Name: "pdf", Description: "d", Status: store.ItemPublished, MadeBy: "You"}
	other := store.LibraryItem{Kind: store.KindSkill, Name: "renamed", Description: "d", Status: store.ItemPublished, MadeBy: "You"}
	for _, it := range []*store.LibraryItem{&pdf, &other} {
		if err := e.st.AddLibraryItem(it); err != nil {
			t.Fatal(err)
		}
	}
	if err := e.st.SetLibrarySource(store.LibrarySource{ItemID: other.ID, Kind: store.SourceGitHub, Repo: "other/x", Path: "skills/foo"}); err != nil {
		t.Fatal(err)
	}
	code, _, raw := e.get(t, "/api/library/skills/search?q=pd")
	if code != http.StatusOK {
		t.Fatalf("%d %s", code, raw)
	}
	var out struct {
		Skills []struct {
			skillsrc.Hit
			Repo      string `json:"repo"`
			InLibrary bool   `json:"inLibrary"`
		} `json:"skills"`
	}
	json.Unmarshal(raw, &out)
	got := map[string]bool{}
	for _, s := range out.Skills {
		got[s.SkillID] = s.InLibrary
		if s.Repo != s.Source {
			t.Fatalf("repo %+v", s)
		}
	}
	if len(got) != 3 || !got["pdf"] || got["docx"] || !got["foo"] {
		t.Fatalf("inLibrary: %v (%s)", got, raw)
	}

	// q gol: Official, cu descrieri si inLibrary
	code, _, raw = e.get(t, "/api/library/skills/search?q=")
	if code != http.StatusOK {
		t.Fatalf("%d %s", code, raw)
	}
	var off struct {
		Official []struct {
			skillsrc.Official
			Repo      string `json:"repo"`
			SkillID   string `json:"skillId"`
			InLibrary bool   `json:"inLibrary"`
		} `json:"official"`
	}
	json.Unmarshal(raw, &off)
	if len(off.Official) != 2 || off.Official[0].Name != "pdf" || off.Official[0].Description != "Official PDF." ||
		!off.Official[0].InLibrary || off.Official[1].InLibrary || off.Official[0].Repo != "anthropics/skills" ||
		off.Official[1].SkillID != "xlsx" {
		t.Fatalf("official: %s", raw)
	}
}

func TestSkillSearchUnavailable(t *testing.T) {
	e := setupSkills(t)
	e.gh.set(func(f *fakeGitHub) { f.searchSt = http.StatusInternalServerError })
	code, _, raw := e.get(t, "/api/library/skills/search?q=pdf")
	if code != http.StatusBadGateway || errMsg(raw) != "Skills search is not reachable right now. Try again in a minute." {
		t.Fatalf("%d %s", code, raw)
	}
}

// localSkill scrie un skill Claude Code in HOME-ul de test.
func (e *skillEnv) localSkill(t *testing.T, name string, files map[string]string) string {
	t.Helper()
	dir := filepath.Join(e.home, ".claude", "skills", name)
	for p, body := range files {
		full := filepath.Join(dir, p)
		os.MkdirAll(filepath.Dir(full), 0o755)
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestSkillLocal(t *testing.T) {
	e := setupSkills(t)
	mine := e.localSkill(t, "mine", map[string]string{"SKILL.md": "---\nname: mine\ndescription: My skill.\n---\n"})
	e.localSkill(t, "other", map[string]string{"SKILL.md": "---\nname: other\ndescription: O.\n---\n"})
	it := store.LibraryItem{Kind: store.KindSkill, Name: "kept", Description: "d", Status: store.ItemPublished, MadeBy: "You"}
	if err := e.st.AddLibraryItem(&it); err != nil {
		t.Fatal(err)
	}
	e.st.SetLibrarySource(store.LibrarySource{ItemID: it.ID, Kind: store.SourceClaudeCode, Path: mine})
	code, _, raw := e.get(t, "/api/library/skills/local")
	if code != http.StatusOK {
		t.Fatalf("%d %s", code, raw)
	}
	var out struct {
		Skills []struct {
			skillsrc.LocalSkill
			InLibrary bool `json:"inLibrary"`
		} `json:"skills"`
	}
	json.Unmarshal(raw, &out)
	if len(out.Skills) != 2 || out.Skills[0].Name != "mine" || !out.Skills[0].InLibrary || out.Skills[1].InLibrary ||
		out.Skills[0].Source != "Claude Code" || out.Skills[0].Description != "My skill." {
		t.Fatalf("%s", raw)
	}
}

func TestSkillPrepareGitHub(t *testing.T) {
	e := setupSkills(t)
	p := e.prepareGitHub(t)
	if len(p.Token) != 32 || p.Name != "pdf" || p.Description != "Work with PDF files." {
		t.Fatalf("%+v", p)
	}
	files := map[string]skillsrc.FileInfo{}
	for _, f := range p.Files {
		files[f.Path] = f
	}
	if len(files) != 5 || !files["scripts/run.py"].Script || !files["bin/tool"].Script || files["SKILL.md"].Script ||
		files["notes.md"].Size != int64(len("old notes\n")) {
		t.Fatalf("files %+v", p.Files)
	}
	// ref = ultimul commit pe calea skill-ului (nu HEAD), ca verificarea sa nu vada un update fals
	s := p.Source
	if s.Kind != store.SourceGitHub || s.Repo != "o/skills" || s.Path != "skills/pdf" || s.Ref != shaPath1 || s.Label != "o/skills" {
		t.Fatalf("source %+v", s)
	}
	// previzualizare
	code, h, raw := e.get(t, "/api/library/skills/prepare/"+p.Token+"/file?path=scripts/run.py")
	if code != http.StatusOK || string(raw) != "print(1)\n" || !strings.HasPrefix(h.Get("Content-Type"), "text/plain") {
		t.Fatalf("file: %d %s", code, raw)
	}
	if code, _, _ := e.get(t, "/api/library/skills/prepare/"+p.Token+"/file?path=../../x"); code != http.StatusBadRequest {
		t.Fatalf("bad path: %d", code)
	}
	if code, _, _ := e.get(t, "/api/library/skills/prepare/"+p.Token+"/file?path=missing.md"); code != http.StatusNotFound {
		t.Fatalf("missing: %d", code)
	}
	if code, _, _ := e.get(t, "/api/library/skills/prepare/"+strings.Repeat("0", 32)+"/file?path=SKILL.md"); code != http.StatusGone {
		t.Fatalf("unknown token: %d", code)
	}
	// DELETE sterge folderul
	dir := e.srv.preps.dirOf(p.Token)
	if dir == "" || !dirExists(dir) {
		t.Fatalf("prepared dir %q", dir)
	}
	if code, raw := e.do(t, "DELETE", "/api/library/skills/prepare/"+p.Token, nil); code != http.StatusNoContent {
		t.Fatalf("delete: %d %s", code, raw)
	}
	if dirExists(dir) {
		t.Fatal("folderul pregatit a ramas")
	}
}

func TestSkillPrepareErrors(t *testing.T) {
	e := setupSkills(t)
	code, raw := e.do(t, "POST", "/api/library/skills/prepare", map[string]string{"kind": "github", "repo": "o/skills", "skillId": "zzz"})
	if code != http.StatusNotFound || errMsg(raw) != "Couldn't find the skill zzz in o/skills." {
		t.Fatalf("not found: %d %s", code, raw)
	}
	code, raw = e.do(t, "POST", "/api/library/skills/prepare", map[string]string{"kind": "github", "repo": "../x", "skillId": "pdf"})
	if code != http.StatusBadRequest {
		t.Fatalf("bad repo: %d %s", code, raw)
	}
	e.gh.set(func(f *fakeGitHub) { f.limited = true })
	code, raw = e.do(t, "POST", "/api/library/skills/prepare", map[string]string{"kind": "github", "repo": "o/skills", "skillId": "pdf"})
	if code != http.StatusServiceUnavailable || errMsg(raw) != "GitHub is rate limiting requests; try again in a few minutes." {
		t.Fatalf("rate limit: %d %s", code, raw)
	}
	// nimic ramas in folderul de lucru dupa erori
	entries, _ := os.ReadDir(filepath.Join(e.lib.Root, "proposals"))
	if len(entries) != 0 {
		t.Fatalf("ramas: %v", entries)
	}
}

func TestSkillPrepareLocalPaths(t *testing.T) {
	e := setupSkills(t)
	mine := e.localSkill(t, "mine", map[string]string{"SKILL.md": "---\nname: mine\ndescription: Mine.\n---\n", "run.sh": "echo\n"})
	// un folder cu SKILL.md care nu e in lista (~/.ssh)
	ssh := filepath.Join(e.home, ".ssh")
	os.MkdirAll(ssh, 0o700)
	os.WriteFile(filepath.Join(ssh, "SKILL.md"), []byte("---\nname: x\n---\n"), 0o600)
	os.WriteFile(filepath.Join(ssh, "id_rsa"), []byte("secret"), 0o600)
	for _, bad := range []string{"/etc", ssh, mine + "/../..", "../mine", "", filepath.Join(e.home, ".claude", "skills")} {
		code, raw := e.do(t, "POST", "/api/library/skills/prepare", map[string]string{"kind": "local", "path": bad})
		if code != http.StatusBadRequest || errMsg(raw) != "Unknown local skill." {
			t.Fatalf("%q: %d %s", bad, code, raw)
		}
	}
	// calea din lista, chiar cu un "/" la sfarsit (Clean)
	p := e.prepare(t, map[string]string{"kind": "local", "path": mine + "/"})
	fp, _ := skillsrc.Fingerprint(mine)
	if p.Name != "mine" || p.Description != "Mine." || len(p.Files) != 2 || p.Source.Kind != store.SourceClaudeCode ||
		p.Source.Path != mine || p.Source.Ref != fp || p.Source.Label != "Claude Code" || p.Source.Repo != "" {
		t.Fatalf("%+v", p)
	}
}

func TestSkillInstall(t *testing.T) {
	e := setupSkills(t)
	p := e.prepareGitHub(t)
	dir := e.srv.preps.dirOf(p.Token)
	it := e.install(t, map[string]any{"token": p.Token, "name": "pdf"})
	if it.Name != "pdf" || it.Kind != store.KindSkill || it.Status != store.ItemPublished || it.MadeBy != "You" ||
		it.Description != "Work with PDF files." || it.Version == nil || it.Version.Version != "1.0" || it.Version.Files != 5 {
		t.Fatalf("%+v", it)
	}
	if it.Source == nil || it.Source.Ref != shaPath1 || it.Source.Path != "skills/pdf" || it.UpdateAvailable {
		t.Fatalf("source %+v", it.Source)
	}
	if fi, err := os.Stat(filepath.Join(it.Version.Dir, "bin", "tool")); err != nil || fi.Mode()&0o111 == 0 {
		t.Fatalf("bitul executabil: %v %v", fi, err)
	}
	if dirExists(dir) {
		t.Fatal("folderul pregatit a ramas dupa install")
	}
	// GET-ul elementului are sursa
	code, _, raw := e.get(t, "/api/library/"+itoa(it.ID))
	var d itemOut
	json.Unmarshal(raw, &d)
	if code != 200 || d.Source == nil || d.Source.Repo != "o/skills" {
		t.Fatalf("get: %d %s", code, raw)
	}
	// al doilea Install pe acelasi token: 410
	code, raw = e.do(t, "POST", "/api/library/skills/install", map[string]any{"token": p.Token, "name": "pdf-two"})
	if code != http.StatusGone {
		t.Fatalf("token refolosit: %d %s", code, raw)
	}
	// nume luat: 409, iar tokenul ramane bun pentru alt nume
	p2 := e.prepareGitHub(t)
	code, raw = e.do(t, "POST", "/api/library/skills/install", map[string]any{"token": p2.Token, "name": "pdf"})
	if code != http.StatusConflict {
		t.Fatalf("409: %d %s", code, raw)
	}
	code, raw = e.do(t, "POST", "/api/library/skills/install", map[string]any{"token": p2.Token, "name": "Bad Name"})
	if code != http.StatusBadRequest {
		t.Fatalf("400: %d %s", code, raw)
	}
	e.install(t, map[string]any{"token": p2.Token, "name": "pdf-copy"})
}

func TestSkillInstallExpired(t *testing.T) {
	e := setupSkills(t)
	p := e.prepareGitHub(t)
	dir := e.srv.preps.dirOf(p.Token)
	e.advance(31 * time.Minute)
	code, raw := e.do(t, "POST", "/api/library/skills/install", map[string]any{"token": p.Token, "name": "pdf"})
	if code != http.StatusGone {
		t.Fatalf("expirat: %d %s", code, raw)
	}
	if dirExists(dir) {
		t.Fatal("folderul expirat a ramas")
	}
	// o pregatire noua curata si pregatirile expirate nefolosite
	p1 := e.prepareGitHub(t)
	dir1 := e.srv.preps.dirOf(p1.Token)
	e.advance(31 * time.Minute)
	e.prepareGitHub(t)
	if dirExists(dir1) {
		t.Fatal("pregatirea expirata nu a fost stearsa la o pregatire noua")
	}
}

func TestSkillInstallConcurrent(t *testing.T) {
	e := setupSkills(t)
	p := e.prepareGitHub(t)
	var wg sync.WaitGroup
	codes := make([]int, 4)
	for i := range codes {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			codes[i], _ = e.do(t, "POST", "/api/library/skills/install", map[string]any{"token": p.Token, "name": "pdf-" + string(rune('a'+i))})
		}(i)
	}
	wg.Wait()
	created := 0
	for _, c := range codes {
		if c == http.StatusCreated {
			created++
		} else if c != http.StatusGone {
			t.Fatalf("codes %v", codes)
		}
	}
	items, _ := e.st.ListLibraryItemsAnyStatus(store.KindSkill)
	if created != 1 || len(items) != 1 {
		t.Fatalf("created %d, items %d", created, len(items))
	}
}

func TestSkillInstallInProject(t *testing.T) {
	e := setupSkills(t)
	pr := e.project(t)
	p := e.prepareGitHub(t)
	it := e.install(t, map[string]any{"token": p.Token, "name": "pdf", "projectId": pr.ID})
	ins, _ := e.st.ListInstalls(pr.ID)
	if len(ins) != 1 || ins[0].ItemID != it.ID || !ins[0].Enabled || ins[0].VersionID != it.Version.ID || it.Installs != 1 {
		t.Fatalf("installs %+v", ins)
	}
	// proiect inexistent: 404 si nimic creat, tokenul ramane
	p2 := e.prepareGitHub(t)
	code, raw := e.do(t, "POST", "/api/library/skills/install", map[string]any{"token": p2.Token, "name": "pdf-x", "projectId": 9999})
	if code != http.StatusNotFound {
		t.Fatalf("%d %s", code, raw)
	}
}

func TestSkillSourceCheckAndUpdate(t *testing.T) {
	e := setupSkills(t)
	p := e.prepareGitHub(t)
	it := e.install(t, map[string]any{"token": p.Token, "name": "pdf"})
	check := func() (store.LibrarySource, bool) {
		t.Helper()
		code, raw := e.do(t, "POST", "/api/library/items/"+itoa(it.ID)+"/source/check", nil)
		if code != http.StatusOK {
			t.Fatalf("check: %d %s", code, raw)
		}
		var out struct {
			Source          store.LibrarySource `json:"source"`
			UpdateAvailable bool                `json:"updateAvailable"`
		}
		json.Unmarshal(raw, &out)
		return out.Source, out.UpdateAvailable
	}
	if src, up := check(); up || src.Latest != shaPath1 || src.CheckedAt == nil {
		t.Fatalf("fara commit nou: %+v %v", src, up)
	}
	// limita GitHub: 200, nimic schimbat
	e.gh.set(func(f *fakeGitHub) { f.limited = true })
	if src, up := check(); up || src.Latest != shaPath1 {
		t.Fatalf("rate limited: %+v", src)
	}
	e.gh.set(func(f *fakeGitHub) {
		f.limited = false
		f.pathSHA["o/skills:skills/pdf"] = shaPath2
		f.head["o/skills"] = shaTwo
		f.archives["o/skills@"+shaTwo] = f.archives["o/skills@"+shaPath2]
	})
	if src, up := check(); !up || src.Latest != shaPath2 {
		t.Fatalf("commit nou: %+v %v", src, up)
	}
	// GET-ul elementului vede update-ul
	code, _, raw := e.get(t, "/api/library/"+itoa(it.ID))
	var d itemOut
	json.Unmarshal(raw, &d)
	if code != 200 || !d.UpdateAvailable {
		t.Fatalf("get: %s", raw)
	}

	code, raw = e.do(t, "POST", "/api/library/items/"+itoa(it.ID)+"/source/prepare", nil)
	if code != http.StatusOK {
		t.Fatalf("source/prepare: %d %s", code, raw)
	}
	var up prepOut
	json.Unmarshal(raw, &up)
	if up.Source.Ref != shaPath2 || up.Token == "" {
		t.Fatalf("%+v", up)
	}
	changes := map[string]map[string]any{}
	for _, c := range up.Diff {
		changes[c["path"].(string)] = c
	}
	if len(changes) != 3 || changes["new.txt"]["change"] != "added" || changes["new.txt"]["after"] != "added\n" ||
		changes["gone.txt"]["change"] != "removed" || changes["gone.txt"]["before"] != "will be removed\n" ||
		changes["notes.md"]["change"] != "modified" || changes["notes.md"]["before"] != "old notes\n" ||
		changes["notes.md"]["after"] != "new notes\n" {
		t.Fatalf("diff %s", raw)
	}
	// un token de install nu merge pe update
	other := e.prepareGitHub(t)
	if code, raw := e.do(t, "POST", "/api/library/items/"+itoa(it.ID)+"/source/update", map[string]string{"token": other.Token}); code != http.StatusBadRequest {
		t.Fatalf("token strain: %d %s", code, raw)
	}

	code, raw = e.do(t, "POST", "/api/library/items/"+itoa(it.ID)+"/source/update", map[string]string{"token": up.Token})
	if code != http.StatusOK {
		t.Fatalf("update: %d %s", code, raw)
	}
	var after itemOut
	json.Unmarshal(raw, &after)
	if after.Version == nil || after.Version.Version != "1.1" || after.Version.Note != "Updated from o/skills" ||
		after.Source == nil || after.Source.Ref != shaPath2 || after.UpdateAvailable {
		t.Fatalf("after %s", raw)
	}
	// versiunea noua are exact fisierele sursei (gone.txt sters)
	if _, err := os.Stat(filepath.Join(after.Version.Dir, "gone.txt")); !os.IsNotExist(err) {
		t.Fatalf("gone.txt a ramas: %v", err)
	}
	if b, _ := os.ReadFile(filepath.Join(after.Version.Dir, "notes.md")); string(b) != "new notes\n" {
		t.Fatalf("notes %q", b)
	}
	// tokenul s-a consumat
	if code, _ := e.do(t, "POST", "/api/library/items/"+itoa(it.ID)+"/source/update", map[string]string{"token": up.Token}); code != http.StatusGone {
		t.Fatalf("al doilea update: %d", code)
	}
}

func TestSkillLocalInstallAndCheck(t *testing.T) {
	e := setupSkills(t)
	mine := e.localSkill(t, "mine", map[string]string{"SKILL.md": "---\nname: mine\ndescription: Mine.\n---\n"})
	p := e.prepare(t, map[string]string{"kind": "local", "path": mine})
	it := e.install(t, map[string]any{"token": p.Token, "name": "mine"})
	if it.Source == nil || it.Source.Kind != store.SourceClaudeCode || it.Source.Ref != p.Source.Ref {
		t.Fatalf("%+v", it.Source)
	}
	check := func() (int, bool, string) {
		code, raw := e.do(t, "POST", "/api/library/items/"+itoa(it.ID)+"/source/check", nil)
		var out struct {
			UpdateAvailable bool   `json:"updateAvailable"`
			Note            string `json:"note"`
		}
		json.Unmarshal(raw, &out)
		return code, out.UpdateAvailable, out.Note
	}
	if code, up, _ := check(); code != 200 || up {
		t.Fatalf("neschimbat: %d %v", code, up)
	}
	os.WriteFile(filepath.Join(mine, "extra.md"), []byte("x"), 0o644)
	if code, up, _ := check(); code != 200 || !up {
		t.Fatalf("schimbat: %d %v", code, up)
	}
	os.RemoveAll(mine)
	if code, _, note := check(); code != 200 || note != "Source no longer on this machine." {
		t.Fatalf("disparut: %d %q", code, note)
	}
	// fara sursa: 404
	plain := store.LibraryItem{Kind: store.KindSkill, Name: "plain", Description: "d", Status: store.ItemPublished, MadeBy: "You"}
	e.st.AddLibraryItem(&plain)
	if code, _ := e.do(t, "POST", "/api/library/items/"+itoa(plain.ID)+"/source/check", nil); code != http.StatusNotFound {
		t.Fatalf("fara sursa: %d", code)
	}
}

func TestSkillPrepareAsksPathCommitAtFetchedSHA(t *testing.T) {
	e := setupSkills(t)
	e.prepareGitHub(t)
	e.gh.set(func(f *fakeGitHub) {
		if len(f.pathRefs) != 1 || f.pathRefs[0] != shaOne {
			t.Fatalf("commits?path= fara sha-ul descarcat: %v", f.pathRefs)
		}
	})
}

// newCommit pune pe skills/pdf un commit nou cu fisierele date.
func (e *skillEnv) newCommit(sha string, files map[string]string) {
	e.gh.set(func(f *fakeGitHub) {
		f.pathSHA["o/skills:skills/pdf"] = sha
		arch := map[string]string{}
		for p, b := range files {
			arch["skills/pdf/"+p] = b
		}
		f.archives["o/skills@"+sha] = arch
	})
}

func (e *skillEnv) sourcePrepare(t *testing.T, id int64) prepOut {
	t.Helper()
	code, raw := e.do(t, "POST", "/api/library/items/"+itoa(id)+"/source/prepare", nil)
	if code != http.StatusOK {
		t.Fatalf("source/prepare: %d %s", code, raw)
	}
	var out prepOut
	json.Unmarshal(raw, &out)
	return out
}

func TestSkillUpdateConcurrentAndStale(t *testing.T) {
	e := setupSkills(t)
	it := e.install(t, map[string]any{"token": e.prepareGitHub(t).Token, "name": "pdf"})
	setA := map[string]string{"SKILL.md": pdfSkill("A."), "a.txt": "a"}
	setB := map[string]string{"SKILL.md": pdfSkill("B."), "b.txt": "b"}
	e.newCommit("5555555aaaa", setA)
	a := e.sourcePrepare(t, it.ID)
	e.newCommit("6666666bbbb", setB)
	b := e.sourcePrepare(t, it.ID)

	var wg sync.WaitGroup
	codes := make([]int, 2)
	for i, tok := range []string{a.Token, b.Token} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			codes[i], _ = e.do(t, "POST", "/api/library/items/"+itoa(it.ID)+"/source/update", map[string]string{"token": tok})
		}()
	}
	wg.Wait()
	ok := 0
	for _, c := range codes {
		if c == http.StatusOK {
			ok++
		} else if c != http.StatusConflict {
			t.Fatalf("codes %v", codes)
		}
	}
	if ok == 0 {
		t.Fatalf("niciun update: %v", codes)
	}
	vs, _ := e.st.ListLibraryVersions(it.ID)
	for _, v := range vs[:ok] {
		var got []string
		filepath.Walk(v.Dir, func(p string, fi os.FileInfo, err error) error {
			if err == nil && !fi.IsDir() {
				rel, _ := filepath.Rel(v.Dir, p)
				got = append(got, rel)
			}
			return nil
		})
		sort.Strings(got)
		if len(got) != 2 || got[0] != "SKILL.md" || (got[1] != "a.txt" && got[1] != "b.txt") {
			t.Fatalf("versiunea %s amesteca fisierele: %v", v.Version, got)
		}
	}
}

func TestSkillUpdateStaleToken(t *testing.T) {
	e := setupSkills(t)
	it := e.install(t, map[string]any{"token": e.prepareGitHub(t).Token, "name": "pdf"})
	e.newCommit("5555555aaaa", map[string]string{"SKILL.md": pdfSkill("A.")})
	a := e.sourcePrepare(t, it.ID)
	b := e.sourcePrepare(t, it.ID)
	if code, raw := e.do(t, "POST", "/api/library/items/"+itoa(it.ID)+"/source/update", map[string]string{"token": a.Token}); code != 200 {
		t.Fatalf("%d %s", code, raw)
	}
	code, raw := e.do(t, "POST", "/api/library/items/"+itoa(it.ID)+"/source/update", map[string]string{"token": b.Token})
	if code != http.StatusConflict || errMsg(raw) != "The skill changed since this update was prepared; open it again." {
		t.Fatalf("token vechi: %d %s", code, raw)
	}
	if e.srv.preps.dirOf(b.Token) == "" {
		t.Fatal("tokenul vechi s-a consumat")
	}
}

func TestSkillUpdateNotPublished(t *testing.T) {
	e := setupSkills(t)
	it := e.install(t, map[string]any{"token": e.prepareGitHub(t).Token, "name": "pdf"})
	e.newCommit("5555555aaaa", map[string]string{"SKILL.md": pdfSkill("A.")})
	tok := e.sourcePrepare(t, it.ID).Token
	e.st.SetLibraryStatus(it.ID, store.ItemUnpublished)
	const msg = "Only published skills can be updated."
	code, raw := e.do(t, "POST", "/api/library/items/"+itoa(it.ID)+"/source/prepare", nil)
	if code != http.StatusConflict || errMsg(raw) != msg {
		t.Fatalf("prepare: %d %s", code, raw)
	}
	code, raw = e.do(t, "POST", "/api/library/items/"+itoa(it.ID)+"/source/update", map[string]string{"token": tok})
	if code != http.StatusConflict || errMsg(raw) != msg {
		t.Fatalf("update: %d %s", code, raw)
	}
	if e.srv.preps.dirOf(tok) == "" {
		t.Fatal("tokenul s-a consumat")
	}
}

func TestSkillUpdateModeOnly(t *testing.T) {
	e := setupSkills(t)
	mine := e.localSkill(t, "mine", map[string]string{"SKILL.md": "---\nname: mine\ndescription: M.\n---\n", "run": "echo\n"})
	it := e.install(t, map[string]any{"token": e.prepare(t, map[string]string{"kind": "local", "path": mine}).Token, "name": "mine"})
	os.Chmod(filepath.Join(mine, "run"), 0o755)
	p := e.sourcePrepare(t, it.ID)
	if len(p.Diff) != 1 || p.Diff[0]["path"] != "run" || p.Diff[0]["change"] != "mode" ||
		p.Diff[0]["before"] != nil || p.Diff[0]["after"] != nil {
		t.Fatalf("diff %+v", p.Diff)
	}
}

func TestSkillCopyErrorHidesPaths(t *testing.T) {
	e := setupSkills(t)
	p := e.prepareGitHub(t)
	os.RemoveAll(e.srv.preps.dirOf(p.Token))
	code, raw := e.do(t, "POST", "/api/library/skills/install", map[string]any{"token": p.Token, "name": "pdf"})
	if code != http.StatusUnprocessableEntity || errMsg(raw) != "The skill files could not be copied." || strings.Contains(string(raw), "/") {
		t.Fatalf("%d %s", code, raw)
	}
	it := e.install(t, map[string]any{"token": e.prepareGitHub(t).Token, "name": "pdf"})
	e.newCommit("5555555aaaa", map[string]string{"SKILL.md": pdfSkill("A.")})
	up := e.sourcePrepare(t, it.ID)
	os.RemoveAll(e.srv.preps.dirOf(up.Token))
	code, raw = e.do(t, "POST", "/api/library/items/"+itoa(it.ID)+"/source/update", map[string]string{"token": up.Token})
	if code != http.StatusUnprocessableEntity || errMsg(raw) != "The skill files could not be copied." || strings.Contains(string(raw), "/") {
		t.Fatalf("update: %d %s", code, raw)
	}
}
