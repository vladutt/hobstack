package web

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"hobstack/internal/library"
	"hobstack/internal/store"
)

type libEnv struct {
	*env
	lib *library.Lib
}

func setupLib(t *testing.T) *libEnv {
	t.Helper()
	e := setup(t)
	l := library.New(t.TempDir(), e.st)
	e.srv.SetLibrary(l)
	return &libEnv{e, l}
}

// propose scrie un element propus (cu fisiere) ca si cum l-ar fi propus un agent din proiectul pid.
func (e *libEnv) propose(t *testing.T, kind, name string, pid int64, files map[string]string, mod func(*store.LibraryItem)) store.LibraryItem {
	t.Helper()
	it := store.LibraryItem{Kind: kind, Name: name, Description: "d " + name, Status: store.ItemProposed,
		SourceProjectID: pid, MadeBy: "Rook", Entry: "run.sh"}
	if mod != nil {
		mod(&it)
	}
	if err := e.st.AddLibraryItem(&it); err != nil {
		t.Fatal(err)
	}
	e.writeProposal(t, it.ID, files)
	return it
}

func (e *libEnv) writeProposal(t *testing.T, id int64, files map[string]string) {
	t.Helper()
	dir := e.lib.ProposalDir(id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, content := range files {
		p := filepath.Join(dir, name)
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// get face un GET si intoarce si antetele.
func (e *libEnv) get(t *testing.T, path string) (int, http.Header, []byte) {
	t.Helper()
	res, err := http.Get(e.ts.URL + path)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	return res.StatusCode, res.Header, raw
}

func (e *libEnv) project(t *testing.T) store.Project {
	return e.createProject(t, t.TempDir())
}

func TestLibraryPublishTwiceMakesNewVersion(t *testing.T) {
	e := setupLib(t)
	p := e.project(t)
	it := e.propose(t, store.KindSkill, "pdf-offer", p.ID, map[string]string{"SKILL.md": "v1"}, nil)

	code, raw := e.do(t, "POST", "/api/library/"+itoa(it.ID)+"/publish", map[string]string{"note": "first"})
	if code != 200 {
		t.Fatalf("publish: %d %s", code, raw)
	}
	var v store.LibraryVersion
	json.Unmarshal(raw, &v)
	if v.Version != "1.0" || v.Note != "first" {
		t.Fatalf("version %+v", v)
	}
	// proiectul sursa il are instalat
	ins, _ := e.st.ListInstalls(p.ID)
	if len(ins) != 1 || ins[0].VersionID != v.ID {
		t.Fatalf("installs %+v", ins)
	}
	// publicat din nou, fara fisiere noi: nimic de publicat (422), nu 500
	if code, _ := e.do(t, "POST", "/api/library/"+itoa(it.ID)+"/publish", nil); code != 422 {
		t.Fatalf("publish without proposal: %d", code)
	}
	e.writeProposal(t, it.ID, map[string]string{"SKILL.md": "v2"})
	code, raw = e.do(t, "POST", "/api/library/"+itoa(it.ID)+"/publish", nil)
	json.Unmarshal(raw, &v)
	if code != 200 || v.Version != "1.1" {
		t.Fatalf("second publish: %d %s", code, raw)
	}
	_, _, body := e.get(t, "/api/library/"+itoa(it.ID))
	var d struct {
		Version  struct{ Version string } `json:"version"`
		Versions []store.LibraryVersion   `json:"versions"`
		Files    []library.FileInfo       `json:"files"`
		From     string                   `json:"filesFrom"`
	}
	json.Unmarshal(body, &d)
	if d.Version.Version != "1.1" || len(d.Versions) != 2 || d.From != "version" || len(d.Files) != 1 || d.Files[0].Name != "SKILL.md" {
		t.Fatalf("detail: %s", body)
	}
}

func TestLibraryListAndDetailNeverHaveSecretValues(t *testing.T) {
	e := setupLib(t)
	p := e.project(t)
	it := e.propose(t, store.KindMCP, "meta-ads", p.ID, map[string]string{"x": "y"}, func(i *store.LibraryItem) {
		i.Transport, i.URL, i.Secrets = "remote", "https://x.example/mcp", []string{"META_TOKEN"}
	})
	if code, raw := e.do(t, "POST", "/api/library/"+itoa(it.ID)+"/publish", nil); code != 200 {
		t.Fatalf("%d %s", code, raw)
	}
	path := "/api/projects/" + itoa(p.ID) + "/library/" + itoa(it.ID)
	if code, raw := e.do(t, "PUT", path, map[string]any{"installed": true, "secrets": map[string]string{"META_TOKEN": "s3cr3t-value"}}); code != 204 {
		t.Fatalf("put: %d %s", code, raw)
	}
	if m, _ := e.st.Secrets(p.ID, it.ID); m["META_TOKEN"] != "s3cr3t-value" {
		t.Fatalf("secret not stored: %v", m)
	}
	for _, url := range []string{"/api/library", "/api/library?kind=mcp", "/api/library/" + itoa(it.ID), "/api/projects/" + itoa(p.ID) + "/library", "/api/projects/" + itoa(p.ID)} {
		code, _, body := e.get(t, url)
		if code != 200 || strings.Contains(string(body), "s3cr3t") {
			t.Fatalf("%s: %d, leaks or fails: %s", url, code, body)
		}
	}
	_, _, body := e.get(t, "/api/projects/"+itoa(p.ID)+"/library")
	var list []struct {
		Installed bool
		Version   string
		Secrets   []secretView
		Item      struct{ Installs int }
	}
	json.Unmarshal(body, &list)
	if len(list) != 1 || !list[0].Installed || list[0].Version != "1.0" || len(list[0].Secrets) != 1 || !list[0].Secrets[0].Set || list[0].Item.Installs != 1 {
		t.Fatalf("project library: %s", body)
	}
	// kind invalid
	if code, _, _ := e.get(t, "/api/library?kind=nope"); code != 400 {
		t.Fatalf("bad kind: %d", code)
	}
}

func TestLibrarySecretsRules(t *testing.T) {
	e := setupLib(t)
	p := e.project(t)
	it := e.propose(t, store.KindMCP, "meta-ads", p.ID, nil, func(i *store.LibraryItem) {
		i.Transport, i.URL, i.Secrets = "remote", "https://x.example/mcp", []string{"META_TOKEN"}
	})
	e.do(t, "POST", "/api/library/"+itoa(it.ID)+"/publish", nil)
	path := "/api/projects/" + itoa(p.ID) + "/library/" + itoa(it.ID)
	put := func(body map[string]any) int { c, _ := e.do(t, "PUT", path, body); return c }

	if c := put(map[string]any{"secrets": map[string]string{"OTHER_ONE": "x"}}); c != 400 {
		t.Fatalf("undeclared secret: %d", c)
	}
	if c := put(map[string]any{"secrets": map[string]string{"bad name": "x"}}); c != 400 {
		t.Fatalf("bad secret name: %d", c)
	}
	put(map[string]any{"installed": true, "secrets": map[string]string{"META_TOKEN": "one"}})
	// valoare goala = pastreaza
	if c := put(map[string]any{"secrets": map[string]string{"META_TOKEN": ""}}); c != 204 {
		t.Fatalf("empty: %d", c)
	}
	if m, _ := e.st.Secrets(p.ID, it.ID); m["META_TOKEN"] != "one" {
		t.Fatalf("empty value wiped the secret: %v", m)
	}
	// sters doar explicit
	if c := put(map[string]any{"clear": []string{"META_TOKEN"}}); c != 204 {
		t.Fatalf("clear: %d", c)
	}
	if m, _ := e.st.Secrets(p.ID, it.ID); m["META_TOKEN"] != "" {
		t.Fatalf("clear did not clear: %v", m)
	}
	// scos din proiect: secretele nu se mai pot seta
	if c := put(map[string]any{"installed": false}); c != 204 {
		t.Fatalf("uninstall: %d", c)
	}
	if c := put(map[string]any{"installed": false}); c != 204 { // idempotent
		t.Fatalf("uninstall twice: %d", c)
	}
	if c := put(map[string]any{"secrets": map[string]string{"META_TOKEN": "x"}}); c != 400 {
		t.Fatalf("secrets while not installed: %d", c)
	}
}

func TestLibraryInstallRules(t *testing.T) {
	e := setupLib(t)
	src, other := e.project(t), e.project(t)
	it := e.propose(t, store.KindSkill, "pdf-offer", src.ID, map[string]string{"SKILL.md": "a"}, nil)
	put := func(pid int64, installed bool) int {
		c, _ := e.do(t, "PUT", "/api/projects/"+itoa(pid)+"/library/"+itoa(it.ID), map[string]any{"installed": installed})
		return c
	}
	if c := put(other.ID, true); c != 409 { // inca doar propus
		t.Fatalf("install proposal: %d", c)
	}
	e.do(t, "POST", "/api/library/"+itoa(it.ID)+"/publish", nil)
	if c := put(other.ID, true); c != 204 {
		t.Fatalf("install: %d", c)
	}
	// o versiune noua nu muta proiectul care o are deja
	e.writeProposal(t, it.ID, map[string]string{"SKILL.md": "b"})
	e.do(t, "POST", "/api/library/"+itoa(it.ID)+"/publish", nil)
	ins, _ := e.st.ListInstalls(other.ID)
	vs, _ := e.st.ListLibraryVersions(it.ID)
	if len(ins) != 1 || ins[0].VersionID != vs[1].ID {
		t.Fatalf("pin moved: %+v", ins)
	}
	if c, _ := e.do(t, "PUT", "/api/projects/9999/library/"+itoa(it.ID), map[string]any{"installed": true}); c != 404 {
		t.Fatalf("missing project: %d", c)
	}
	if c, _ := e.do(t, "PUT", "/api/projects/"+itoa(other.ID)+"/library/9999", map[string]any{"installed": true}); c != 404 {
		t.Fatalf("missing item: %d", c)
	}
}

func TestLibraryDeclineKeepsInSourceProject(t *testing.T) {
	e := setupLib(t)
	src, other := e.project(t), e.project(t)
	it := e.propose(t, store.KindSkill, "keep-me", src.ID, map[string]string{"SKILL.md": "a"}, nil)
	if code, raw := e.do(t, "POST", "/api/library/"+itoa(it.ID)+"/decline", nil); code != 204 {
		t.Fatalf("decline: %d %s", code, raw)
	}
	got, _ := e.st.GetLibraryItem(it.ID)
	if got.Status != store.ItemDeclined {
		t.Fatalf("status %s", got.Status)
	}
	if ins, _ := e.st.ListInstalls(src.ID); len(ins) != 1 {
		t.Fatalf("not kept in the source: %+v", ins)
	}
	if got.PublishedAt != nil {
		t.Fatalf("kept item was published: %v", got.PublishedAt)
	}
	// ascuns din Library, dar proiectul sursa il vede ca instalat; altele nu il pot instala
	_, _, body := e.get(t, "/api/library")
	if strings.Contains(string(body), "keep-me") {
		t.Fatalf("declined item listed: %s", body)
	}
	_, _, body = e.get(t, "/api/projects/"+itoa(src.ID)+"/library")
	if !strings.Contains(string(body), "keep-me") {
		t.Fatalf("source project lost it: %s", body)
	}
	if c, _ := e.do(t, "PUT", "/api/projects/"+itoa(other.ID)+"/library/"+itoa(it.ID), map[string]any{"installed": true}); c != 409 {
		t.Fatalf("other project installed a declined item: %d", c)
	}
	if c, _ := e.do(t, "POST", "/api/library/"+itoa(it.ID)+"/decline", nil); c != 409 {
		t.Fatalf("decline twice: %d", c)
	}

	// keep:false: respinsa de tot, fara install, fara fisiere
	it2 := e.propose(t, store.KindSkill, "drop-me", src.ID, map[string]string{"SKILL.md": "a"}, nil)
	if code, _ := e.do(t, "POST", "/api/library/"+itoa(it2.ID)+"/decline", map[string]any{"keep": false}); code != 204 {
		t.Fatalf("decline keep=false: %d", code)
	}
	if ins, _ := e.st.ListInstalls(src.ID); len(ins) != 1 {
		t.Fatalf("keep=false installed: %+v", ins)
	}
	if _, err := os.Stat(e.lib.ProposalDir(it2.ID)); err == nil {
		t.Fatal("proposal files remain")
	}
}

func TestLibraryUnpublish(t *testing.T) {
	e := setupLib(t)
	p := e.project(t)
	it := e.propose(t, store.KindSkill, "gone-soon", p.ID, map[string]string{"SKILL.md": "a"}, nil)
	if c, _ := e.do(t, "POST", "/api/library/"+itoa(it.ID)+"/unpublish", nil); c != 409 {
		t.Fatalf("unpublish proposal: %d", c)
	}
	e.do(t, "POST", "/api/library/"+itoa(it.ID)+"/publish", nil)
	if c, _ := e.do(t, "POST", "/api/library/"+itoa(it.ID)+"/unpublish", nil); c != 204 {
		t.Fatalf("unpublish: %d", c)
	}
	if c, _ := e.do(t, "POST", "/api/library/"+itoa(it.ID)+"/unpublish", nil); c != 409 {
		t.Fatalf("unpublish twice: %d", c)
	}
	// ramane instalat in sursa
	if ins, _ := e.st.ListInstalls(p.ID); len(ins) != 1 {
		t.Fatalf("installs %+v", ins)
	}
	if c, _ := e.do(t, "POST", "/api/library/9999/unpublish", nil); c != 404 {
		t.Fatalf("missing: %d", c)
	}
}

func TestLibraryFiles(t *testing.T) {
	e := setupLib(t)
	p := e.project(t)
	it := e.propose(t, store.KindSkill, "files-test", p.ID, map[string]string{
		"SKILL.md": "<script>alert(1)</script>", "sub/deep.txt": "deep"}, nil)
	base := "/api/library/" + itoa(it.ID) + "/files/"

	code, h, body := e.get(t, base+"SKILL.md")
	if code != 200 || string(body) != "<script>alert(1)</script>" {
		t.Fatalf("%d %s", code, body)
	}
	if ct := h.Get("Content-Type"); ct != "text/plain; charset=utf-8" {
		t.Fatalf("content-type %q", ct)
	}
	if h.Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal("no nosniff")
	}
	if code, _, body := e.get(t, base+"sub/deep.txt"); code != 200 || string(body) != "deep" {
		t.Fatalf("nested: %d %s", code, body)
	}
	for _, bad := range []string{"../1/SKILL.md", "sub/../../x", "..", "sub/..%2F..%2Fx", "%2e%2e/x", "a%00b", "a%5Cb"} {
		if code, _, _ := e.get(t, base+bad); code != 400 {
			t.Errorf("%q: got %d, want 400", bad, code)
		}
	}
	// absolut: nu poate scapa din folder, oricum ar fi rezolvat (400 sau 404, niciodata continutul)
	if code, _, body := e.get(t, base+"%2Fetc%2Fhosts"); code == 200 || strings.Contains(string(body), "localhost") {
		t.Errorf("absolute path served: %d", code)
	}
	if code, _, _ := e.get(t, base+"nope.txt"); code != 404 {
		t.Errorf("missing: %d", code)
	}
	if code, _, _ := e.get(t, base+"sub"); code != 404 { // folder
		t.Errorf("dir: %d", code)
	}
	// symlink spre exterior
	secret := filepath.Join(t.TempDir(), "secret.txt")
	os.WriteFile(secret, []byte("top secret"), 0o600)
	if err := os.Symlink(secret, filepath.Join(e.lib.ProposalDir(it.ID), "link.txt")); err == nil {
		if code, _, body := e.get(t, base+"link.txt"); code == 200 || strings.Contains(string(body), "top secret") {
			t.Errorf("symlink followed: %d", code)
		}
	}
	// prea mare
	os.WriteFile(filepath.Join(e.lib.ProposalDir(it.ID), "big.txt"), make([]byte, library.MaxViewBytes+1), 0o644)
	if code, _, _ := e.get(t, base+"big.txt"); code != 413 {
		t.Errorf("big: %d", code)
	}
	// dupa publicare, fisierele vin din versiune; ?version= alege una
	e.do(t, "POST", "/api/library/"+itoa(it.ID)+"/publish", nil)
	if code, _, body := e.get(t, base+"sub/deep.txt?version=1.0"); code != 200 || string(body) != "deep" {
		t.Errorf("version file: %d %s", code, body)
	}
	if code, _, _ := e.get(t, base+"SKILL.md?version=9.9"); code != 404 {
		t.Errorf("unknown version: %d", code)
	}
}

func TestLibraryCreateByUser(t *testing.T) {
	e := setupLib(t)
	post := func(body map[string]any) (int, []byte) { return e.do(t, "POST", "/api/library", body) }

	code, raw := post(map[string]any{"kind": "skill", "name": "my-skill", "description": "does a thing", "content": "# Skill"})
	var v struct {
		store.LibraryItem
		Version *store.LibraryVersion `json:"version"`
	}
	json.Unmarshal(raw, &v)
	if code != 201 || v.Status != store.ItemPublished || v.MadeBy != "You" || v.Version == nil || v.Version.Version != "1.0" {
		t.Fatalf("skill: %d %s", code, raw)
	}
	if data, err := library.ReadFileIn(v.Version.Dir, "SKILL.md"); err != nil || string(data) != "# Skill" {
		t.Fatalf("skill file: %q %v", data, err)
	}
	if ins, _ := e.st.ListInstalls(1); len(ins) != 0 {
		t.Fatalf("user items install nowhere: %+v", ins)
	}

	code, raw = post(map[string]any{"kind": "tool", "name": "my-tool", "description": "d", "entry": "run.sh", "content": "#!/bin/sh\necho hi\n"})
	if code != 201 {
		t.Fatalf("tool: %d %s", code, raw)
	}
	code, raw = post(map[string]any{"kind": "mcp", "name": "remote-srv", "description": "d", "url": "https://x.example/mcp", "secrets": []string{"AUTH_TOKEN"}})
	json.Unmarshal(raw, &v)
	if code != 201 || v.Transport != "remote" || v.Version == nil {
		t.Fatalf("remote mcp: %d %s", code, raw)
	}

	bad := []map[string]any{
		{"kind": "skill", "name": "Bad Name", "description": "d", "content": "x"},
		{"kind": "skill", "name": "ok-name", "description": "", "content": "x"},
		{"kind": "skill", "name": "ok-name", "description": "d", "content": ""},
		{"kind": "tool", "name": "ok-name", "description": "d", "content": "x"}, // fara entry
		{"kind": "tool", "name": "ok-name", "description": "d", "entry": "../x.sh", "content": "x"},
		{"kind": "tool", "name": "ok-name", "description": "d", "entry": "/etc/x.sh", "content": "x"},
		{"kind": "skill", "name": "ok-name", "description": "d", "content": "x", "files": []map[string]string{{"path": "../evil", "content": "x"}}},
		{"kind": "skill", "name": "ok-name", "description": "d", "content": "x", "files": []map[string]string{{"path": "SKILL.md", "content": "dup"}}},
		{"kind": "mcp", "name": "ok-name", "description": "d", "url": "ftp://x"},
		{"kind": "mcp", "name": "ok-name", "description": "d"},
		{"kind": "mcp", "name": "ok-name", "description": "d", "url": "https://x.example", "command": "node"},
		{"kind": "skill", "name": "ok-name", "description": "d", "content": "x", "secrets": []string{"lower"}},
		{"kind": "other", "name": "ok-name", "description": "d"},
	}
	for i, b := range bad {
		if code, raw := post(b); code != 400 {
			t.Errorf("bad[%d]: %d %s", i, code, raw)
		}
	}
	if code, _ := post(map[string]any{"kind": "skill", "name": "my-skill", "description": "d", "content": "x"}); code != 409 {
		t.Errorf("duplicate: %d", code)
	}
	// nimic ramas din cererile esuate
	items, _ := e.st.ListLibraryItems("")
	if len(items) != 3 {
		t.Errorf("leftover items: %d", len(items))
	}
}

// un element facut de utilizator nu se testeaza: comanda e a lui
func TestLibraryCreateLocalMCPSkipsProbe(t *testing.T) {
	e := setupLib(t)
	code, raw := e.do(t, "POST", "/api/library", map[string]any{"kind": "mcp", "name": "slow-srv", "description": "d",
		"command": "definitely-not-a-command-xyz"})
	if code != 201 {
		t.Fatalf("%d %s", code, raw)
	}
	it, err := e.st.FindLibraryItem("mcp", "slow-srv")
	if err != nil || it.Status != store.ItemPublished || len(it.ToolNames) != 0 {
		t.Fatalf("%+v %v", it, err)
	}
}

// proba esuata la Publish da 422; "Publish without test" ({skipTest:true}) publica fara nume de tool-uri
func TestLibraryPublishSkipTest(t *testing.T) {
	e := setupLib(t)
	p := e.project(t)
	it := e.propose(t, store.KindMCP, "dead-local", p.ID, map[string]string{"x": "y"}, func(i *store.LibraryItem) {
		i.Transport, i.Command = "local", "definitely-not-a-command-xyz"
	})
	path := "/api/library/" + itoa(it.ID) + "/publish"
	if code, raw := e.do(t, "POST", path, nil); code != 422 || !strings.Contains(string(raw), "did not answer tools/list") {
		t.Fatalf("%d %s", code, raw)
	}
	if code, raw := e.do(t, "POST", path, map[string]any{"skipTest": true}); code != 200 {
		t.Fatalf("%d %s", code, raw)
	}
	got, _ := e.st.GetLibraryItem(it.ID)
	if got.Status != store.ItemPublished || len(got.ToolNames) != 0 {
		t.Fatalf("%+v", got)
	}
}

func TestLibraryKeepSkipTest(t *testing.T) {
	e := setupLib(t)
	p := e.project(t)
	it := e.propose(t, store.KindMCP, "dead-keep", p.ID, map[string]string{"x": "y"}, func(i *store.LibraryItem) {
		i.Transport, i.Command = "local", "definitely-not-a-command-xyz"
	})
	path := "/api/library/" + itoa(it.ID) + "/decline"
	if code, _ := e.do(t, "POST", path, nil); code != 422 {
		t.Fatalf("keep without skipTest: %d", code)
	}
	if code, raw := e.do(t, "POST", path, map[string]any{"skipTest": true}); code != 204 {
		t.Fatalf("%d %s", code, raw)
	}
	if ins, _ := e.st.ListInstalls(p.ID); len(ins) != 1 {
		t.Fatalf("not installed: %+v", ins)
	}
}

// keep=false trece prin Lib (sub lock-ul item-ului); numele ramane ocupat si eroarea spune de cine
func TestLibraryDeclineDiscardAndNameTaken(t *testing.T) {
	e := setupLib(t)
	p := e.project(t)
	it := e.propose(t, store.KindSkill, "gone-skill", p.ID, map[string]string{"SKILL.md": "a"}, nil)
	if code, raw := e.do(t, "POST", "/api/library/"+itoa(it.ID)+"/decline", map[string]any{"keep": false}); code != 204 {
		t.Fatalf("%d %s", code, raw)
	}
	got, _ := e.st.GetLibraryItem(it.ID)
	if got.Status != store.ItemDeclined {
		t.Fatalf("status %s", got.Status)
	}
	if _, err := os.Stat(e.lib.ProposalDir(it.ID)); err == nil {
		t.Fatal("proposal files left")
	}
	if code, _ := e.do(t, "POST", "/api/library/"+itoa(it.ID)+"/decline", map[string]any{"keep": false}); code != 409 {
		t.Fatalf("second decline: %d", code)
	}
	code, raw := e.do(t, "POST", "/api/library", map[string]any{"kind": "skill", "name": "gone-skill", "description": "d", "content": "x"})
	if code != 409 || !strings.Contains(string(raw), "declined skill") || !strings.Contains(string(raw), p.Name) {
		t.Fatalf("%d %s", code, raw)
	}
}

// la stergerea proiectului dispar si build-urile lui (au wrapper-e cu secrete)
func TestLibraryDeleteProjectRemovesBuilds(t *testing.T) {
	e := setupLib(t)
	p := e.project(t)
	other := e.project(t)
	for _, id := range []int64{p.ID, other.ID} {
		os.MkdirAll(filepath.Join(e.lib.BuildRoot(), itoa(id)+"-abcd1234", "bin"), 0o755)
	}
	if code, raw := e.do(t, "DELETE", "/api/projects/"+itoa(p.ID), nil); code != 204 {
		t.Fatalf("%d %s", code, raw)
	}
	if _, err := os.Stat(filepath.Join(e.lib.BuildRoot(), itoa(p.ID)+"-abcd1234")); err == nil {
		t.Fatal("build of the deleted project left")
	}
	if _, err := os.Stat(filepath.Join(e.lib.BuildRoot(), itoa(other.ID)+"-abcd1234")); err != nil {
		t.Fatal("build of another project removed")
	}
}

func TestInfoHasOS(t *testing.T) {
	e := setup(t)
	if _, raw := e.do(t, "GET", "/api/info", nil); !strings.Contains(string(raw), `"os":"`) {
		t.Fatalf("%s", raw)
	}
}

func TestLibraryProposalsOnProject(t *testing.T) {
	e := setupLib(t)
	p := e.project(t)
	var d struct {
		LibraryProposals []store.LibraryItem `json:"libraryProposals"`
	}
	_, _, body := e.get(t, "/api/projects/"+itoa(p.ID))
	if !strings.Contains(string(body), `"libraryProposals":[]`) {
		t.Fatalf("not an empty array: %s", body)
	}
	e.propose(t, store.KindSkill, "waiting", p.ID, map[string]string{"SKILL.md": "a"}, nil)
	_, _, body = e.get(t, "/api/projects/"+itoa(p.ID))
	json.Unmarshal(body, &d)
	if len(d.LibraryProposals) != 1 || d.LibraryProposals[0].Name != "waiting" {
		t.Fatalf("detail: %s", body)
	}
	_, _, body = e.get(t, "/api/projects")
	var cards []struct {
		LibraryProposals int `json:"libraryProposals"`
	}
	json.Unmarshal(body, &cards)
	if len(cards) != 1 || cards[0].LibraryProposals != 1 {
		t.Fatalf("card: %s", body)
	}
}

func TestLibraryGuardAndUnavailable(t *testing.T) {
	e := setupLib(t)
	res, err := http.Post(e.ts.URL+"/api/library/1/unpublish", "text/plain", strings.NewReader(""))
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusUnsupportedMediaType {
		t.Fatalf("csrf guard: %d", res.StatusCode)
	}
	e2 := setup(t) // fara Library
	if code, _ := e2.do(t, "GET", "/api/library", nil); code != http.StatusServiceUnavailable {
		t.Fatalf("no library: %d", code)
	}
}

func TestLibraryKeepFailureLeavesProposal(t *testing.T) {
	e := setupLib(t)
	p := e.project(t)
	it := e.propose(t, store.KindMCP, "dead-local", p.ID, map[string]string{"x": "y"}, func(i *store.LibraryItem) {
		i.Transport, i.Command = "local", "definitely-not-a-command-xyz"
	})
	code, raw := e.do(t, "POST", "/api/library/"+itoa(it.ID)+"/decline", nil)
	if code != 422 {
		t.Fatalf("%d %s", code, raw)
	}
	got, _ := e.st.GetLibraryItem(it.ID)
	if got.Status != store.ItemProposed || got.PublishedAt != nil {
		t.Fatalf("not left as a proposal: %s %v", got.Status, got.PublishedAt)
	}
	if ins, _ := e.st.ListInstalls(p.ID); len(ins) != 0 {
		t.Fatalf("installed: %+v", ins)
	}
}

func TestLibraryUnpublishedHiddenAndUpgrade(t *testing.T) {
	e := setupLib(t)
	p := e.project(t)
	it := e.propose(t, store.KindSkill, "evolving", p.ID, map[string]string{"SKILL.md": "a"}, nil)
	e.do(t, "POST", "/api/library/"+itoa(it.ID)+"/publish", nil)
	e.writeProposal(t, it.ID, map[string]string{"SKILL.md": "b"})
	e.do(t, "POST", "/api/library/"+itoa(it.ID)+"/publish", nil)
	vs, _ := e.st.ListLibraryVersions(it.ID)
	// source project was moved to 1.1 by the second publish? it keeps 1.0 because Install upserts: check both ways
	path := "/api/projects/" + itoa(p.ID) + "/library/" + itoa(it.ID)
	e.st.Install(p.ID, it.ID, vs[1].ID) // pin pe 1.0
	if c, _ := e.do(t, "PUT", path, map[string]any{"installed": true}); c != 204 {
		t.Fatal(c)
	}
	if ins, _ := e.st.ListInstalls(p.ID); ins[0].VersionID != vs[1].ID {
		t.Fatalf("plain install moved the pin: %+v", ins)
	}
	if c, _ := e.do(t, "PUT", path, map[string]any{"upgrade": true}); c != 204 {
		t.Fatal(c)
	}
	if ins, _ := e.st.ListInstalls(p.ID); ins[0].VersionID != vs[0].ID {
		t.Fatalf("upgrade did not move the pin: %+v", ins)
	}
	e.do(t, "POST", "/api/library/"+itoa(it.ID)+"/unpublish", nil)
	_, _, body := e.get(t, "/api/library")
	if strings.Contains(string(body), "evolving") {
		t.Fatalf("unpublished item listed: %s", body)
	}
	_, _, body = e.get(t, "/api/projects/"+itoa(p.ID)+"/library")
	if !strings.Contains(string(body), "evolving") {
		t.Fatalf("installed unpublished item missing from the project: %s", body)
	}
	if c, _, _ := e.get(t, "/api/library/"+itoa(it.ID)); c != 200 {
		t.Fatalf("detail: %d", c)
	}
}

func TestLibraryCreateAcceptsOneMegabyteFile(t *testing.T) {
	e := setupLib(t)
	code, raw := e.do(t, "POST", "/api/library", map[string]any{"kind": "skill", "name": "big-skill", "description": "d",
		"content": strings.Repeat("x", 1000*1000)})
	if code != 201 {
		t.Fatalf("%d %.200s", code, raw)
	}
}
