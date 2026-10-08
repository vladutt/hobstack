package web

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"

	"foreman/internal/library"
	"foreman/internal/mcp"
	"foreman/internal/store"
)

// SetLibrary activeaza rutele /api/library (fara el raspund 503).
func (s *Server) SetLibrary(l *library.Lib) { s.lib = l }

// itemView: elementul plus versiunea curenta (cea mai noua) si in cate proiecte e instalat.
// Secretele apar doar ca nume (in item.Secrets); valorile nu ies niciodata din server.
type itemView struct {
	store.LibraryItem
	Version  *store.LibraryVersion `json:"version"` // null cat timp nu are nicio versiune publicata
	Installs int                   `json:"installs"`
}

type installedIn struct {
	ProjectID int64  `json:"projectId"`
	Project   string `json:"project"`
	Version   string `json:"version"`
	Enabled   bool   `json:"enabled"`
}

type libraryDetail struct {
	itemView
	Versions    []store.LibraryVersion `json:"versions"`
	RecentUses  []store.LibraryUse     `json:"recentUses"`
	Files       []library.FileInfo     `json:"files"`
	FilesFrom   string                 `json:"filesFrom"` // "proposal" | "version" | "" (nu are fisiere)
	InstalledIn []installedIn          `json:"installedIn"`
}

// libraryOK raspunde 503 daca Library nu e legata (testele vechi, build-uri fara ea).
func (s *Server) libraryOK(w http.ResponseWriter) bool {
	if s.lib == nil {
		writeErr(w, http.StatusServiceUnavailable, "the library is not available")
		return false
	}
	return true
}

func (s *Server) itemViewOf(it store.LibraryItem, installs int) (itemView, error) {
	vs, err := s.st.ListLibraryVersions(it.ID)
	if err != nil {
		return itemView{}, err
	}
	v := itemView{LibraryItem: it, Installs: installs}
	if len(vs) > 0 {
		v.Version = &vs[0]
	}
	return v, nil
}

func (s *Server) listLibrary(w http.ResponseWriter, r *http.Request) {
	if !s.libraryOK(w) {
		return
	}
	kind := r.URL.Query().Get("kind")
	if kind != "" && kind != store.KindTool && kind != store.KindSkill && kind != store.KindMCP {
		fail(w, badRequest("kind: tool, skill or mcp"))
		return
	}
	items, err := s.st.ListLibraryItems(kind)
	if err != nil {
		fail(w, err)
		return
	}
	counts, err := s.st.InstallCounts()
	if err != nil {
		fail(w, err)
		return
	}
	out := make([]itemView, 0, len(items))
	for _, it := range items {
		v, err := s.itemViewOf(it, counts[it.ID])
		if err != nil {
			fail(w, err)
			return
		}
		out = append(out, v)
	}
	writeJSON(w, http.StatusOK, out)
}

// filesDir alege de unde se citesc fisierele: ?version=1.0 cere acea versiune; altfel propunerea (daca exista),
// altfel cea mai noua versiune. "" = nu are fisiere.
func (s *Server) filesDir(it store.LibraryItem, version string) (dir, from string, err error) {
	vs, err := s.st.ListLibraryVersions(it.ID)
	if err != nil {
		return "", "", err
	}
	if version != "" {
		for _, v := range vs {
			if v.Version == version {
				return v.Dir, "version", nil
			}
		}
		return "", "", store.ErrNotFound
	}
	if p := s.lib.ProposalDir(it.ID); dirExists(p) {
		return p, "proposal", nil
	}
	if len(vs) > 0 {
		return vs[0].Dir, "version", nil
	}
	return "", "", nil
}

func dirExists(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.IsDir()
}

func (s *Server) getLibraryItem(w http.ResponseWriter, r *http.Request) {
	if !s.libraryOK(w) {
		return
	}
	id, err := pathID(r)
	if err != nil {
		fail(w, err)
		return
	}
	it, err := s.st.GetLibraryItem(id)
	if err != nil {
		fail(w, err)
		return
	}
	installs, err := s.st.ListItemInstalls(id)
	if err != nil {
		fail(w, err)
		return
	}
	view, err := s.itemViewOf(it, len(installs))
	if err != nil {
		fail(w, err)
		return
	}
	versions, err := s.st.ListLibraryVersions(id)
	if err != nil {
		fail(w, err)
		return
	}
	uses, err := s.st.RecentLibraryUses(id, 10)
	if err != nil {
		fail(w, err)
		return
	}
	d := libraryDetail{itemView: view, Versions: versions, RecentUses: uses, Files: []library.FileInfo{}, InstalledIn: []installedIn{}}
	dir, from, err := s.filesDir(it, "")
	if err != nil {
		fail(w, err)
		return
	}
	if dir != "" {
		d.Files, d.FilesFrom = library.ListFiles(dir), from
	}
	names := map[int64]string{}
	for _, v := range versions {
		names[v.ID] = v.Version
	}
	for _, in := range installs {
		name := ""
		if p, err := s.st.GetProject(in.ProjectID); err == nil {
			name = p.Name
		}
		d.InstalledIn = append(d.InstalledIn, installedIn{ProjectID: in.ProjectID, Project: name, Version: names[in.VersionID], Enabled: in.Enabled})
	}
	writeJSON(w, http.StatusOK, d)
}

// libraryFile serveste un fisier al elementului ca text simplu. Niciodata ca HTML: continutul e scris de agenti.
func (s *Server) libraryFile(w http.ResponseWriter, r *http.Request) {
	if !s.libraryOK(w) {
		return
	}
	id, err := pathID(r)
	if err != nil {
		fail(w, err)
		return
	}
	it, err := s.st.GetLibraryItem(id)
	if err != nil {
		fail(w, err)
		return
	}
	dir, _, err := s.filesDir(it, r.URL.Query().Get("version"))
	if err != nil {
		fail(w, err)
		return
	}
	if dir == "" {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	data, err := library.ReadFileIn(dir, r.PathValue("path"))
	switch {
	case errors.Is(err, library.ErrBadPath):
		writeErr(w, http.StatusBadRequest, "bad file path")
		return
	case errors.Is(err, library.ErrTooBig):
		writeErr(w, http.StatusRequestEntityTooLarge, "file is bigger than 512 KB")
		return
	case err != nil:
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	h := w.Header()
	h.Set("Content-Type", "text/plain; charset=utf-8")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Security-Policy", "sandbox")
	w.Write(data)
}

// decodeOptional: corp JSON optional (un POST fara corp e valid).
func decodeOptional(r *http.Request, v any) error {
	raw, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		return badRequest("invalid body")
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, v); err != nil {
		return badRequest("invalid JSON")
	}
	return nil
}

func idValue(s string) (int64, error) {
	id, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, badRequest("invalid id")
	}
	return id, nil
}

// libraryErr: erorile pe care utilizatorul le poate repara (probe esuat, nimic de publicat) sunt 422.
func libraryErr(w http.ResponseWriter, err error) {
	var ue library.UserError
	if errors.As(err, &ue) {
		writeErr(w, http.StatusUnprocessableEntity, ue.Msg)
		return
	}
	fail(w, err)
}

func (s *Server) announceLibrary(it store.LibraryItem) {
	if it.SourceProjectID != 0 {
		s.hub.Publish(it.SourceProjectID, "library", it)
	}
}

func (s *Server) publishLibraryItem(w http.ResponseWriter, r *http.Request) {
	if !s.libraryOK(w) {
		return
	}
	id, err := pathID(r)
	if err != nil {
		fail(w, err)
		return
	}
	var in struct {
		Note string `json:"note"`
	}
	if err := decodeOptional(r, &in); err != nil {
		fail(w, err)
		return
	}
	in.Note = strings.TrimSpace(in.Note)
	if err := maxLen("note", in.Note, 300); err != nil {
		fail(w, err)
		return
	}
	it, err := s.st.GetLibraryItem(id)
	if err != nil {
		fail(w, err)
		return
	}
	if it.Status != store.ItemProposed && it.Status != store.ItemPublished {
		writeErr(w, http.StatusConflict, "only a proposal or a published item can be published")
		return
	}
	// fara lock-uri tinute aici: Publish poate dura (probe-ul unui server MCP local)
	v, err := s.lib.Publish(id, in.Note)
	if err != nil {
		libraryErr(w, err)
		return
	}
	if cur, err := s.st.GetLibraryItem(id); err == nil {
		s.announceLibrary(cur)
	}
	writeJSON(w, http.StatusOK, v)
}

// declineLibraryItem: propunerea nu intra in Library. Cu {"keep": true} (implicit, cand propunerea are proiect sursa)
// ramane instalata in proiectul sursa ("Keep in <project>"): se ia un instantaneu al fisierelor ca versiune,
// pe care proiectul o foloseste mai departe, iar elementul ramane ascuns din Library (declined).
// {"keep": false} o respinge de tot si sterge fisierele propunerii.
func (s *Server) declineLibraryItem(w http.ResponseWriter, r *http.Request) {
	if !s.libraryOK(w) {
		return
	}
	id, err := pathID(r)
	if err != nil {
		fail(w, err)
		return
	}
	var in struct {
		Keep *bool `json:"keep"`
	}
	if err := decodeOptional(r, &in); err != nil {
		fail(w, err)
		return
	}
	it, err := s.st.GetLibraryItem(id)
	if err != nil {
		fail(w, err)
		return
	}
	if it.Status != store.ItemProposed {
		writeErr(w, http.StatusConflict, "only a proposal can be declined")
		return
	}
	keep := it.SourceProjectID != 0
	if in.Keep != nil {
		keep = *in.Keep && it.SourceProjectID != 0
	}
	if keep {
		// Publish face versiunea si o instaleaza in proiectul sursa; apoi o ascundem din Library
		if _, err := s.lib.Publish(id, "Kept in the project"); err != nil {
			libraryErr(w, err)
			return
		}
	} else {
		s.lib.DiscardProposal(id)
	}
	if err := s.st.SetLibraryStatus(id, store.ItemDeclined); err != nil {
		fail(w, err)
		return
	}
	it.Status = store.ItemDeclined
	s.announceLibrary(it)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) unpublishLibraryItem(w http.ResponseWriter, r *http.Request) {
	if !s.libraryOK(w) {
		return
	}
	id, err := pathID(r)
	if err != nil {
		fail(w, err)
		return
	}
	it, err := s.st.GetLibraryItem(id)
	if err != nil {
		fail(w, err)
		return
	}
	if it.Status != store.ItemPublished {
		writeErr(w, http.StatusConflict, "only a published item can be unpublished")
		return
	}
	if err := s.st.SetLibraryStatus(id, store.ItemUnpublished); err != nil {
		fail(w, err)
		return
	}
	it.Status = store.ItemUnpublished
	s.announceLibrary(it)
	w.WriteHeader(http.StatusNoContent)
}

// libraryCreate: un element facut de utilizator din text. Skill: content = SKILL.md. Tool: content = fisierul
// din entry. MCP: url (remote) sau command + args (local), fisiere optionale in files.
type libraryCreate struct {
	Kind        string             `json:"kind"`
	Name        string             `json:"name"`
	Description string             `json:"description"`
	Needs       []string           `json:"needs"`
	Secrets     []string           `json:"secrets"`
	Entry       string             `json:"entry"`
	Content     string             `json:"content"`
	Command     string             `json:"command"`
	Args        []string           `json:"args"`
	URL         string             `json:"url"`
	Files       []library.TextFile `json:"files"`
}

// createLibraryItem face elementul, il publica ca 1.0 si intoarce itemView (201). Nu se instaleaza nicaieri
// (nu are proiect sursa): utilizatorul il instaleaza pe fiecare proiect.
func (s *Server) createLibraryItem(w http.ResponseWriter, r *http.Request) {
	if !s.libraryOK(w) {
		return
	}
	var in libraryCreate
	if err := decode(r, &in); err != nil {
		fail(w, err)
		return
	}
	files := append([]library.TextFile(nil), in.Files...)
	switch in.Kind {
	case store.KindSkill:
		if strings.TrimSpace(in.Content) == "" {
			fail(w, badRequest("content: the text of SKILL.md"))
			return
		}
		files = append(files, library.TextFile{Path: "SKILL.md", Content: in.Content})
	case store.KindTool:
		if strings.TrimSpace(in.Content) == "" {
			fail(w, badRequest("content: the code of the entry file"))
			return
		}
		files = append(files, library.TextFile{Path: strings.TrimSpace(in.Entry), Content: in.Content})
	}
	names := make([]string, len(files))
	for i, f := range files {
		names[i] = f.Path
	}
	v, err := library.ValidateInput(mcp.LibraryInput{Kind: in.Kind, Name: in.Name, Description: in.Description, Entry: in.Entry,
		Command: in.Command, URL: in.URL, Files: names, Args: in.Args, Needs: in.Needs, Secrets: in.Secrets}, true)
	if err != nil {
		fail(w, badRequest(err.Error()))
		return
	}
	conflict := func() { writeErr(w, http.StatusConflict, "the library already has a "+v.Kind+" named \""+v.Name+"\"") }
	if _, err := s.st.FindLibraryItem(v.Kind, v.Name); err == nil {
		conflict()
		return
	} else if !errors.Is(err, store.ErrNotFound) {
		fail(w, err)
		return
	}
	// fisierele se scriu intr-un folder de lucru, prin ajutoarele din library (caile din input nu ajung direct pe disc)
	var staging string
	if len(files) > 0 {
		var ue library.UserError
		if staging, _, _, err = s.lib.WriteStaged(files); errors.As(err, &ue) {
			fail(w, badRequest(ue.Msg))
			return
		} else if err != nil {
			fail(w, err)
			return
		}
	} else if staging, err = s.lib.NewStaging(); err != nil {
		fail(w, err)
		return
	}
	defer os.RemoveAll(staging) // dupa AdoptStaged nu mai exista

	it := store.LibraryItem{Kind: v.Kind, Name: v.Name, Description: v.Description, MadeBy: "You",
		Needs: v.Needs, Secrets: v.Secrets, Entry: v.Entry, Command: v.Command, Args: v.Args, URL: v.URL,
		// ascuns (declined) cat se pregateste; Publish il face published sau se sterge daca esueaza
		Status: store.ItemDeclined}
	if v.Kind == store.KindMCP {
		it.Transport = "local"
		if v.URL != "" {
			it.Transport = "remote"
		}
	}
	if err := s.st.AddLibraryItem(&it); errors.Is(err, store.ErrConflict) {
		conflict()
		return
	} else if err != nil {
		fail(w, err)
		return
	}
	if _, err := s.lib.AdoptStaged(staging, it.ID); err != nil {
		_ = s.st.DeleteLibraryItem(it.ID)
		fail(w, err)
		return
	}
	if _, err := s.lib.Publish(it.ID, ""); err != nil {
		_ = s.st.DeleteLibraryItem(it.ID)
		s.lib.DiscardProposal(it.ID)
		libraryErr(w, err)
		return
	}
	cur, err := s.st.GetLibraryItem(it.ID)
	if err != nil {
		fail(w, err)
		return
	}
	view, err := s.itemViewOf(cur, 0)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, view)
}

type secretView struct {
	Name string `json:"name"`
	Set  bool   `json:"set"`
}

type projectLibraryView struct {
	Item      itemView     `json:"item"`
	Installed bool         `json:"installed"`
	Enabled   bool         `json:"enabled"`
	Version   string       `json:"version"` // versiunea fixata in proiect ("" daca nu e instalat)
	Update    bool         `json:"updateAvailable"`
	Secrets   []secretView `json:"secrets"`
}

func (s *Server) secretViews(projectID int64, it store.LibraryItem) ([]secretView, error) {
	have, err := s.st.Secrets(projectID, it.ID)
	if err != nil {
		return nil, err
	}
	out := make([]secretView, 0, len(it.Secrets))
	for _, n := range it.Secrets {
		out = append(out, secretView{Name: n, Set: have[n] != ""})
	}
	return out, nil
}

// listProjectLibrary: ce poate instala proiectul (elementele publicate) si ce are deja instalat
// (inclusiv elemente retrase sau pastrate din propuneri).
func (s *Server) listProjectLibrary(w http.ResponseWriter, r *http.Request) {
	if !s.libraryOK(w) {
		return
	}
	pid, err := pathID(r)
	if err != nil {
		fail(w, err)
		return
	}
	if _, err := s.st.GetProject(pid); err != nil {
		fail(w, err)
		return
	}
	installs, err := s.st.ListInstalls(pid)
	if err != nil {
		fail(w, err)
		return
	}
	byItem := map[int64]store.LibraryInstall{}
	for _, in := range installs {
		byItem[in.ItemID] = in
	}
	items, err := s.st.ListLibraryItems("")
	if err != nil {
		fail(w, err)
		return
	}
	counts, err := s.st.InstallCounts()
	if err != nil {
		fail(w, err)
		return
	}
	var list []store.LibraryItem
	seen := map[int64]bool{}
	for _, it := range items {
		seen[it.ID] = true
		if _, has := byItem[it.ID]; has || it.Status == store.ItemPublished {
			list = append(list, it)
		}
	}
	for _, in := range installs { // instalate dar ascunse din lista (declined)
		if seen[in.ItemID] {
			continue
		}
		if it, err := s.st.GetLibraryItem(in.ItemID); err == nil {
			list = append(list, it)
		}
	}
	out := []projectLibraryView{}
	for _, it := range list {
		view, err := s.itemViewOf(it, counts[it.ID])
		if err != nil {
			fail(w, err)
			return
		}
		secrets, err := s.secretViews(pid, it)
		if err != nil {
			fail(w, err)
			return
		}
		pv := projectLibraryView{Item: view, Secrets: secrets}
		if in, has := byItem[it.ID]; has {
			pv.Installed, pv.Enabled = true, in.Enabled
			vs, err := s.st.ListLibraryVersions(it.ID)
			if err != nil {
				fail(w, err)
				return
			}
			for _, v := range vs {
				if v.ID == in.VersionID {
					pv.Version = v.Version
				}
			}
			pv.Update = len(vs) > 0 && vs[0].ID != in.VersionID
		}
		out = append(out, pv)
	}
	writeJSON(w, http.StatusOK, out)
}

const maxSecretLen = 8192

// putProjectLibrary instaleaza / scoate un element din proiect si salveaza secretele lui.
//
//	{"installed": true|false, "secrets": {"NAME": "value"}, "clear": ["NAME"]}
//
// "installed" lipsa = nu schimba instalarea (doar secretele). O valoare goala in "secrets" pastreaza secretul
// existent (un formular lasat gol nu sterge nimic); se sterge doar cu "clear". Instalarea fixeaza versiunea
// curenta; un element deja instalat isi pastreaza versiunea (doar se reactiveaza). Secretele raman salvate
// cand elementul e scos (nu avem cum sa le stergem separat, si reinstalarea le gaseste).
func (s *Server) putProjectLibrary(w http.ResponseWriter, r *http.Request) {
	if !s.libraryOK(w) {
		return
	}
	pid, err := pathID(r)
	if err != nil {
		fail(w, err)
		return
	}
	itemID, err := idValue(r.PathValue("item"))
	if err != nil {
		fail(w, err)
		return
	}
	var in struct {
		Installed *bool             `json:"installed"`
		Secrets   map[string]string `json:"secrets"`
		Clear     []string          `json:"clear"`
	}
	if err := decode(r, &in); err != nil {
		fail(w, err)
		return
	}
	if _, err := s.st.GetProject(pid); err != nil {
		fail(w, err)
		return
	}
	it, err := s.st.GetLibraryItem(itemID)
	if err != nil {
		fail(w, err)
		return
	}
	declared := map[string]bool{}
	for _, n := range it.Secrets {
		declared[n] = true
	}
	check := func(name string) error {
		if !library.ValidSecretName(name) || !declared[name] {
			return badRequest("secret " + name + " is not one of this item's secrets")
		}
		return nil
	}
	for name, val := range in.Secrets {
		if err := check(name); err != nil {
			fail(w, err)
			return
		}
		if len(val) > maxSecretLen || strings.ContainsRune(val, 0) {
			fail(w, badRequest("secret "+name+": too long or has a NUL character"))
			return
		}
	}
	for _, name := range in.Clear {
		if err := check(name); err != nil {
			fail(w, err)
			return
		}
	}
	installs, err := s.st.ListInstalls(pid)
	if err != nil {
		fail(w, err)
		return
	}
	var cur *store.LibraryInstall
	for i := range installs {
		if installs[i].ItemID == itemID {
			cur = &installs[i]
		}
	}
	wantInstalled := cur != nil
	if in.Installed != nil {
		wantInstalled = *in.Installed
	}
	if len(in.Secrets) > 0 || len(in.Clear) > 0 {
		if !wantInstalled {
			fail(w, badRequest("install the item before setting its secrets"))
			return
		}
	}
	switch {
	case wantInstalled:
		// doar elementele publicate se instaleaza; "Keep in <proiect>" ramane doar in proiectul sursa
		allowed := it.Status == store.ItemPublished || (it.Status == store.ItemDeclined && it.SourceProjectID == pid) || cur != nil
		if !allowed {
			writeErr(w, http.StatusConflict, "only published items can be installed")
			return
		}
		versionID := int64(0)
		if cur != nil {
			versionID = cur.VersionID
		} else {
			vs, err := s.st.ListLibraryVersions(itemID)
			if err != nil {
				fail(w, err)
				return
			}
			if len(vs) == 0 {
				writeErr(w, http.StatusConflict, "this item has no published version")
				return
			}
			versionID = vs[0].ID
		}
		if err := s.st.Install(pid, itemID, versionID); err != nil {
			fail(w, err)
			return
		}
	case cur != nil:
		if err := s.st.Uninstall(pid, itemID); err != nil && !errors.Is(err, store.ErrNotFound) {
			fail(w, err)
			return
		}
	}
	for name, val := range in.Secrets {
		if val == "" {
			continue // gol = pastreaza
		}
		if err := s.st.SetSecret(pid, itemID, name, val); err != nil {
			fail(w, err)
			return
		}
	}
	for _, name := range in.Clear {
		if err := s.st.SetSecret(pid, itemID, name, ""); err != nil {
			fail(w, err)
			return
		}
	}
	s.hub.Publish(pid, "project", map[string]int64{"id": pid})
	w.WriteHeader(http.StatusNoContent)
}

// stats ale rutei: nefolosit, pastreaza importul atomic pentru numaratori viitoare
