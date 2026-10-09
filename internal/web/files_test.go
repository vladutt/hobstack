package web

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"hobstack/internal/config"
	"hobstack/internal/events"
	"hobstack/internal/picker"
	"hobstack/internal/store"
)

// upload trimite un request multipart; campul fisierelor se numeste "files".
func upload(t *testing.T, e *env, path string, fields map[string]string, files map[string][]byte, withHeader bool) (int, []byte) {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	for k, v := range fields {
		mw.WriteField(k, v)
	}
	for name, data := range files {
		fw, err := mw.CreateFormFile("files", name)
		if err != nil {
			t.Fatal(err)
		}
		fw.Write(data)
	}
	mw.Close()
	req, _ := http.NewRequest("POST", e.ts.URL+path, &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	if withHeader {
		req.Header.Set("X-HobStack", "1")
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	return res.StatusCode, raw
}

func filesPath(id int64) string { return "/api/projects/" + itoa(id) + "/files" }

func uploadOK(t *testing.T, e *env, pid int64, files map[string][]byte) []store.Attachment {
	t.Helper()
	code, raw := upload(t, e, filesPath(pid), nil, files, true)
	if code != http.StatusCreated {
		t.Fatalf("upload: %d %s", code, raw)
	}
	var list []store.Attachment
	json.Unmarshal(raw, &list)
	return list
}

func TestUploadNeedsHeader(t *testing.T) {
	e := setup(t)
	p := e.createProject(t, t.TempDir())
	files := map[string][]byte{"a.txt": []byte("a")}
	if code, _ := upload(t, e, filesPath(p.ID), nil, files, false); code != http.StatusUnsupportedMediaType {
		t.Fatalf("fara header: %d", code)
	}
	if code, raw := upload(t, e, filesPath(p.ID), nil, files, true); code != http.StatusCreated {
		t.Fatalf("cu header: %d %s", code, raw)
	}
}

func TestUploadListServeDelete(t *testing.T) {
	e := setup(t)
	p := e.createProject(t, t.TempDir())
	uploadOK(t, e, p.ID, map[string][]byte{
		"spec.pdf":    []byte("%PDF-1.4 x"),
		"pagina.html": []byte("<script>alert(1)</script>"),
	})
	code, raw := e.do(t, "GET", filesPath(p.ID), nil)
	var list []store.Attachment
	json.Unmarshal(raw, &list)
	if code != 200 || len(list) != 2 {
		t.Fatalf("lista: %d %s", code, raw)
	}
	var pdf store.Attachment
	for _, a := range list {
		if a.Name == "spec.pdf" {
			pdf = a
		}
	}
	res, err := http.Get(e.ts.URL + "/api/files/" + itoa(pdf.ID))
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != 200 || string(body) != "%PDF-1.4 x" {
		t.Fatalf("serve: %d %q", res.StatusCode, body)
	}
	if !strings.HasPrefix(res.Header.Get("Content-Disposition"), "inline") {
		t.Errorf("disposition = %q", res.Header.Get("Content-Disposition"))
	}
	if csp := res.Header.Get("Content-Security-Policy"); csp != "" {
		t.Errorf("pdf-ul nu trebuie sa aiba CSP, are %q", csp)
	}
	if res.Header.Get("X-Content-Type-Options") != "nosniff" {
		t.Errorf("nosniff lipseste pe pdf")
	}
	if code, _ := e.do(t, "DELETE", "/api/files/"+itoa(pdf.ID), nil); code != http.StatusNoContent {
		t.Fatalf("delete: %d", code)
	}
	onDisk := filepath.Join(config.ProjectFilesDir(e.dirs.Projects, p.ID), pdf.Stored)
	if _, err := os.Stat(onDisk); !os.IsNotExist(err) {
		t.Errorf("fisierul exista inca: %v", err)
	}
	if code, _ := e.do(t, "GET", "/api/files/"+itoa(pdf.ID), nil); code != http.StatusNotFound {
		t.Errorf("dupa stergere: %d", code)
	}
}

func TestServeFileHeaders(t *testing.T) {
	e := setup(t)
	p := e.createProject(t, t.TempDir())
	list := uploadOK(t, e, p.ID, map[string][]byte{"pagina.html": []byte("<b>x</b>")})
	res, err := http.Get(e.ts.URL + "/api/files/" + itoa(list[0].ID))
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if !strings.HasPrefix(res.Header.Get("Content-Disposition"), "attachment") {
		t.Errorf("disposition = %q", res.Header.Get("Content-Disposition"))
	}
	if res.Header.Get("X-Content-Type-Options") != "nosniff" {
		t.Errorf("nosniff lipseste")
	}
	if res.Header.Get("Content-Security-Policy") != "sandbox" {
		t.Errorf("csp = %q", res.Header.Get("Content-Security-Policy"))
	}
	if res.Header.Get("Cross-Origin-Resource-Policy") != "same-origin" {
		t.Errorf("corp = %q", res.Header.Get("Cross-Origin-Resource-Policy"))
	}
}

func TestTextPlainGetsCharset(t *testing.T) {
	e := setup(t)
	p := e.createProject(t, t.TempDir())
	// fisier salvat cu mime fara charset (ca inainte de fix)
	dir := config.ProjectFilesDir(e.dirs.Projects, p.ID)
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "aa-n.txt"), []byte("salut"), 0o644)
	a := store.Attachment{ProjectID: p.ID, Kind: store.KindProjectFile, Name: "n.txt", Stored: "aa-n.txt", Size: 5, Mime: "text/plain"}
	if err := e.st.AddAttachment(&a); err != nil {
		t.Fatal(err)
	}
	res, err := http.Get(e.ts.URL + "/api/files/" + itoa(a.ID))
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if got := res.Header.Get("Content-Type"); got != "text/plain; charset=utf-8" {
		t.Errorf("content-type la servire = %q", got)
	}
	if normalizeMime("text/plain") != "text/plain; charset=utf-8" || normalizeMime("image/png") != "image/png" {
		t.Error("normalizeMime")
	}
}

func TestUploadTooBig(t *testing.T) {
	e := setup(t)
	p := e.createProject(t, t.TempDir())
	big := bytes.Repeat([]byte("a"), 50<<20+1)
	code, raw := upload(t, e, filesPath(p.ID), nil, map[string][]byte{"mare.bin": big}, true)
	if code != http.StatusBadRequest || !strings.Contains(string(raw), "50 MB") {
		t.Fatalf("%d %s", code, raw)
	}
}

func TestMessageWithAttachment(t *testing.T) {
	e := setup(t)
	p := e.createProject(t, t.TempDir())
	rook := e.addWorker(t, p.ID, "Rook")
	path := "/api/agents/" + itoa(rook.ID) + "/messages"
	code, raw := upload(t, e, path, map[string]string{"text": "uite"}, map[string][]byte{"bug.png": []byte("\x89PNG")}, true)
	if code != http.StatusCreated {
		t.Fatalf("%d %s", code, raw)
	}
	var m store.Message
	json.Unmarshal(raw, &m)
	if len(m.Attachments) != 1 || m.Attachments[0].Name != "bug.png" {
		t.Fatalf("atasamente: %s", raw)
	}
	if _, raw := e.do(t, "GET", filesPath(p.ID), nil); strings.Contains(string(raw), "bug.png") {
		t.Errorf("atasamentul apare in fisierele proiectului: %s", raw)
	}
	if _, raw := e.do(t, "GET", path, nil); !strings.Contains(string(raw), "bug.png") {
		t.Errorf("atasamentul lipseste din mesaje: %s", raw)
	}
	// text gol fara fisiere: 400
	if code, _ := upload(t, e, path, map[string]string{"text": " "}, nil, true); code != http.StatusBadRequest {
		t.Errorf("gol: %d", code)
	}
}

func TestDefaultWorkdirUnique(t *testing.T) {
	e := setup(t)
	var dirs []string
	for i := 0; i < 2; i++ {
		code, raw := e.do(t, "POST", "/api/projects", map[string]any{"name": "HoReCa QR"})
		if code != http.StatusCreated {
			t.Fatalf("%d %s", code, raw)
		}
		var p store.Project
		json.Unmarshal(raw, &p)
		dirs = append(dirs, p.Workdir)
	}
	want := []string{filepath.Join(e.dirs.Workspaces, "horeca-qr"), filepath.Join(e.dirs.Workspaces, "horeca-qr-2")}
	for i, d := range dirs {
		if d != want[i] {
			t.Errorf("workdir %d = %q, vreau %q", i, d, want[i])
		}
		if info, err := os.Stat(d); err != nil || !info.IsDir() {
			t.Errorf("%s nu exista pe disc", d)
		}
	}
	nou := filepath.Join(t.TempDir(), "nou", "adanc")
	if p := e.createProject(t, nou); p.Workdir != nou {
		t.Errorf("workdir = %q", p.Workdir)
	}
	if info, err := os.Stat(nou); err != nil || !info.IsDir() {
		t.Errorf("%s nu a fost creat", nou)
	}
}

func TestSlugify(t *testing.T) {
	for in, want := range map[string]string{
		"HoReCa QR": "horeca-qr",
		"Țara mea!": "tara-mea",
		"   ":       "project",
		"a--b__c":   "a-b-c",
	} {
		if got := slugify(in); got != want {
			t.Errorf("slugify(%q) = %q, vreau %q", in, got, want)
		}
	}
}

func TestPickFolder(t *testing.T) {
	e := setup(t)
	e.pickPath = "/x/y"
	if code, raw := e.do(t, "POST", "/api/pick-folder", nil); code != 200 || !strings.Contains(string(raw), `"path":"/x/y"`) {
		t.Fatalf("%d %s", code, raw)
	}
	e.pickErr = picker.ErrCanceled
	if code, _ := e.do(t, "POST", "/api/pick-folder", nil); code != http.StatusNoContent {
		t.Fatalf("anulat: %d", code)
	}
	if _, raw := e.do(t, "GET", "/api/info", nil); !strings.Contains(string(raw), `"canPickFolder":true`) {
		t.Fatalf("info: %s", raw)
	}
}

func TestDeleteProjectRemovesFiles(t *testing.T) {
	e := setup(t)
	work := t.TempDir()
	p := e.createProject(t, work)
	uploadOK(t, e, p.ID, map[string][]byte{"a.txt": []byte("a")})
	projDir := filepath.Join(e.dirs.Projects, itoa(p.ID))
	if _, err := os.Stat(projDir); err != nil {
		t.Fatal(err)
	}
	if code, _ := e.do(t, "DELETE", "/api/projects/"+itoa(p.ID), nil); code != http.StatusNoContent {
		t.Fatalf("delete: %d", code)
	}
	if _, err := os.Stat(projDir); !os.IsNotExist(err) {
		t.Errorf("folderul proiectului exista: %v", err)
	}
	if _, err := os.Stat(work); err != nil {
		t.Errorf("folderul de lucru a disparut: %v", err)
	}
}

func TestWorkspacesIsFile(t *testing.T) {
	e := setup(t)
	f := filepath.Join(t.TempDir(), "fisier")
	os.WriteFile(f, []byte("x"), 0o644)
	e.dirs.Workspaces = f
	srv := New(e.st, e.tm, nil, events.NewHub(), http.NotFoundHandler(), e.dirs, nil, false)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	res, err := http.Post(ts.URL+"/api/projects", "application/json", strings.NewReader(`{"name":"X"}`))
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode == http.StatusCreated {
		t.Fatalf("a creat proiect: %d", res.StatusCode)
	}
}

func TestInvalidInputCreatesNoFolder(t *testing.T) {
	e := setup(t)
	code, _ := e.do(t, "POST", "/api/projects", map[string]any{"name": "X", "budgetUsd": 0.01})
	if code != http.StatusBadRequest {
		t.Fatalf("cod %d", code)
	}
	if _, err := os.Stat(filepath.Join(e.dirs.Workspaces, "x")); !os.IsNotExist(err) {
		t.Errorf("folderul orfan exista: %v", err)
	}
}

func TestCreateProjectClearsStaleFilesDir(t *testing.T) {
	e := setup(t)
	old := filepath.Join(e.dirs.Projects, "1", "files")
	os.MkdirAll(old, 0o755)
	os.WriteFile(filepath.Join(old, "vechi.txt"), []byte("x"), 0o644)
	p := e.createProject(t, t.TempDir())
	if p.ID != 1 {
		t.Fatalf("astept id 1, am %d", p.ID)
	}
	if _, err := os.Stat(filepath.Join(old, "vechi.txt")); !os.IsNotExist(err) {
		t.Fatalf("vechi.txt trebuia sters: %v", err)
	}
}
