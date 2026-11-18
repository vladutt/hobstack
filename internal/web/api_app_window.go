package web

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"hobstack/internal/appprefs"
)

// AppWindow: ce ii spune main stratului web despre fereastra proprie (macOS / Windows).
// Nesetat (teste, Linux) = fereastra nu exista, UI-ul nu arata alegerea.
type AppWindow struct {
	Supported bool
	Reason    string // de ce nu e disponibila ("" cand Supported)
	Show      func() // deschide UI-ul cum spune preferinta (fereastra sau browser)
}

// SetAppWindow activeaza alegerea "Open HobStack in" si POST /api/app/show.
func (s *Server) SetAppWindow(w AppWindow) { s.window = &w }

// windowState: campurile despre fereastra din GET /api/app.
func (s *Server) windowState() map[string]any {
	st := map[string]any{"openIn": appprefs.Load(s.app.root).OpenIn, "windowSupported": false, "windowReason": ""}
	if s.window != nil {
		st["windowSupported"] = s.window.Supported
		st["windowReason"] = s.window.Reason
	}
	return st
}

func (s *Server) setOpenIn(w http.ResponseWriter, r *http.Request) {
	if s.app == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "not available"})
		return
	}
	var in struct {
		OpenIn string `json:"openIn"`
	}
	if err := decode(r, &in); err != nil {
		fail(w, err)
		return
	}
	if in.OpenIn != appprefs.OpenWindow && in.OpenIn != appprefs.OpenBrowser {
		writeErr(w, http.StatusBadRequest, `openIn must be "window" or "browser"`)
		return
	}
	if s.window == nil || !s.window.Supported {
		writeErr(w, http.StatusConflict, "app window is not available")
		return
	}
	if err := appprefs.Update(s.app.root, func(p *appprefs.Prefs) { p.OpenIn = in.OpenIn }); err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, s.appState())
}

// showApp: a doua pornire a HobStack cere instantei care ruleaza sa-si arate fereastra (sau browserul).
func (s *Server) showApp(w http.ResponseWriter, r *http.Request) {
	if s.window == nil || s.window.Show == nil {
		writeErr(w, http.StatusServiceUnavailable, "not available")
		return
	}
	w.WriteHeader(http.StatusNoContent)
	go s.window.Show()
}

// downloadsDir: ~/Downloads al utilizatorului (creat daca lipseste).
func (s *Server) downloadsDir() (string, error) {
	dir := filepath.Join(s.app.home, "Downloads")
	return dir, os.MkdirAll(dir, 0o755)
}

// uniqueName: "report.pdf", apoi "report (2).pdf", "report (3).pdf"... primul nume liber din dir.
func uniqueName(dir, name string) string {
	ext := filepath.Ext(name)
	base := strings.TrimSuffix(name, ext)
	if ext == name { // ".env": fara extensie de separat
		base, ext = name, ""
	}
	candidate := name
	for i := 2; ; i++ {
		if _, err := os.Lstat(filepath.Join(dir, candidate)); os.IsNotExist(err) {
			return candidate
		}
		candidate = fmt.Sprintf("%s (%d)%s", base, i, ext)
	}
}

// fileResponse: ResponseWriter care scrie corpul raspunsului intr-un fisier (download-ul din fereastra).
type fileResponse struct {
	f      *os.File
	header http.Header
	status int
	err    error
}

func (fr *fileResponse) Header() http.Header { return fr.header }
func (fr *fileResponse) WriteHeader(code int) {
	if fr.status == 0 {
		fr.status = code
	}
}
func (fr *fileResponse) Write(b []byte) (int, error) {
	if fr.status == 0 {
		fr.status = http.StatusOK
	}
	n, err := fr.f.Write(b)
	if err != nil && fr.err == nil {
		fr.err = err
	}
	return n, err
}

// downloadFile: in fereastra proprie nu exista bara de descarcari a browserului, deci UI-ul cere serverului sa
// salveze fisierul in Downloads. Serverul isi cheama propriul handler (fara retea), doar pentru cai /api/.
func (s *Server) downloadFile(w http.ResponseWriter, r *http.Request) {
	if s.app == nil {
		writeErr(w, http.StatusServiceUnavailable, "not available")
		return
	}
	var in struct {
		URL  string `json:"url"`
		Name string `json:"name"`
	}
	if err := decode(r, &in); err != nil {
		fail(w, err)
		return
	}
	if !strings.HasPrefix(in.URL, "/api/") {
		writeErr(w, http.StatusBadRequest, "only HobStack files can be saved")
		return
	}
	name := strings.TrimSpace(in.Name)
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, `/\`) || filepath.Base(name) != name {
		writeErr(w, http.StatusBadRequest, "bad file name")
		return
	}
	dir, err := s.downloadsDir()
	if err != nil {
		fail(w, err)
		return
	}
	tmp, err := os.CreateTemp(dir, ".hobstack-download-*")
	if err != nil {
		fail(w, err)
		return
	}
	defer os.Remove(tmp.Name()) // dupa rename nu mai exista: Remove nu face nimic
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, in.URL, nil)
	if err != nil {
		tmp.Close()
		writeErr(w, http.StatusBadRequest, "bad url")
		return
	}
	req.Host = r.Host // garda de host a serverului vrea 127.0.0.1 / localhost
	fr := &fileResponse{f: tmp, header: http.Header{}}
	s.Handler().ServeHTTP(fr, req)
	closeErr := tmp.Close()
	if fr.status != http.StatusOK {
		writeErr(w, http.StatusBadGateway, fmt.Sprintf("cannot download the file (%d)", fr.status))
		return
	}
	if fr.err != nil || closeErr != nil {
		fail(w, fmt.Errorf("saving the file: %v %v", fr.err, closeErr))
		return
	}
	s.dlMu.Lock() // doua descarcari cu acelasi nume nu au voie sa ia acelasi nume liber
	defer s.dlMu.Unlock()
	dest := filepath.Join(dir, uniqueName(dir, name))
	if err := os.Rename(tmp.Name(), dest); err != nil {
		fail(w, err)
		return
	}
	os.Chmod(dest, 0o644)
	writeJSON(w, http.StatusOK, map[string]string{"path": dest})
}

// revealDownload: "Show in Finder" dupa o descarcare; doar fisiere direct in ~/Downloads.
func (s *Server) revealDownload(w http.ResponseWriter, r *http.Request) {
	if s.app == nil || s.opener == nil {
		writeErr(w, http.StatusServiceUnavailable, "not available")
		return
	}
	var in struct {
		Path string `json:"path"`
	}
	if err := decode(r, &in); err != nil {
		fail(w, err)
		return
	}
	dir := filepath.Join(s.app.home, "Downloads")
	path := filepath.Clean(in.Path)
	info, err := os.Lstat(path)
	if filepath.Dir(path) != dir || err != nil || !info.Mode().IsRegular() {
		writeErr(w, http.StatusBadRequest, "only files saved to Downloads")
		return
	}
	if err := s.opener.Reveal(path); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
