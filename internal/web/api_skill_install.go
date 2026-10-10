package web

// Library -> Skills -> "+ Add skill": cautarea pe skills.sh (si lista Official), skill-urile din Claude,
// pregatirea unui skill intr-un folder de lucru (token, 30 min), Install, apoi sursa si Update.

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"hobstack/internal/library"
	"hobstack/internal/mcp"
	"hobstack/internal/skillsrc"
	"hobstack/internal/store"
)

const (
	skillsDownMsg  = "Skills search is not reachable right now. Try again in a minute."
	githubDownMsg  = "GitHub is not reachable right now. Try again in a minute."
	rateLimitedMsg = "GitHub is rate limiting requests; try again in a few minutes."
	noSkillMDMsg   = "This folder has no SKILL.md."
	unknownLocal   = "Unknown local skill."
	sourceGoneMsg  = "Source no longer on this machine."
	prepGoneMsg    = "This skill is no longer prepared. Open it again."
	copyFailedMsg  = "The skill files could not be copied."
	staleMsg       = "The skill changed since this update was prepared; open it again."
	notUpdatable   = "Only published skills can be updated."

	prepTTL = 30 * time.Minute // cat traieste un folder pregatit
)

// --- pregatirile (doar in memorie) ---

// prepared e un skill adus intr-un folder de lucru, asteptand Install (itemID 0) sau Update (itemID-ul elementului).
// Campurile nu se mai schimba dupa ce pregatirea intra in harta.
type prepared struct {
	dir         string
	name        string // numele sugerat in Library
	description string
	files       []skillsrc.FileInfo
	source      store.LibrarySource // Ref = referinta adusa acum
	itemID      int64
	baseRef     string // la Update: ref-ul elementului cand s-a pregatit (alt ref acum = pregatire veche)
	created     time.Time
}

type preparations struct {
	mu sync.Mutex
	m  map[string]*prepared
}

func newPreparations() *preparations { return &preparations{m: map[string]*prepared{}} }

func expired(pr *prepared, now time.Time) bool { return now.Sub(pr.created) >= prepTTL }

// add pune pregatirea in harta si intoarce tokenul (32 hex aleatori). Curata intai pregatirile expirate.
func (p *preparations) add(pr *prepared, now time.Time) (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	token := hex.EncodeToString(b[:])
	var old []string
	p.mu.Lock()
	for t, x := range p.m {
		if expired(x, now) {
			old = append(old, x.dir)
			delete(p.m, t)
		}
	}
	p.m[token] = pr
	p.mu.Unlock()
	for _, d := range old {
		os.RemoveAll(d)
	}
	return token, nil
}

// get intoarce pregatirea; take=true o si scoate din harta (o singura data, sub lock: doua Install pe acelasi
// token nu pot lua amandoua pregatirea). O pregatire expirata se sterge si nu se intoarce.
func (p *preparations) get(token string, now time.Time, take bool) (*prepared, bool) {
	p.mu.Lock()
	pr, ok := p.m[token]
	if ok && (take || expired(pr, now)) {
		delete(p.m, token)
	}
	p.mu.Unlock()
	if !ok {
		return nil, false
	}
	if expired(pr, now) {
		os.RemoveAll(pr.dir)
		return nil, false
	}
	return pr, true
}

// drop scoate pregatirea si ii sterge folderul (fereastra s-a inchis).
func (p *preparations) drop(token string) {
	p.mu.Lock()
	pr, ok := p.m[token]
	delete(p.m, token)
	p.mu.Unlock()
	if ok {
		os.RemoveAll(pr.dir)
	}
}

// dirOf: folderul unei pregatiri ("" daca nu exista); pentru teste.
func (p *preparations) dirOf(token string) string {
	p.mu.Lock()
	defer p.mu.Unlock()
	if pr, ok := p.m[token]; ok {
		return pr.dir
	}
	return ""
}

// --- "In library" ---

// skillIndex: ce skill-uri are deja Library, dupa nume si dupa sursa.
type skillIndex struct {
	names      map[string]bool
	github     map[string]bool // repo (litere mici) + "\x00" + cale
	githubBase map[string]bool // repo + "\x00" + numele folderului (rezultatele skills.sh nu au calea)
	local      map[string]bool // kind + "\x00" + cale; plugin dupa cheie: localKey
}

// localKey: cheia unei surse locale in skillIndex.local. Plugin-urile salvate dupa cheie (Repo = nume@marketplace,
// Path = numele skill-ului) nu au calea absoluta: o versiune noua a plugin-ului e tot "In library".
func localKey(kind, repo, p string) string {
	if kind == store.SourcePlugin && repo != "" {
		return kind + "\x00" + repo + "\x00" + p
	}
	return kind + "\x00" + p
}

func (s *Server) skillLibraryIndex() (skillIndex, error) {
	ix := skillIndex{names: map[string]bool{}, github: map[string]bool{}, githubBase: map[string]bool{}, local: map[string]bool{}}
	// in orice stare: si un element ascuns are numele luat (Install ar da 409)
	items, err := s.st.ListLibraryItemsAnyStatus(store.KindSkill)
	if err != nil {
		return ix, err
	}
	for _, it := range items {
		ix.names[it.Name] = true
	}
	sources, err := s.st.ListLibrarySources()
	if err != nil {
		return ix, err
	}
	for _, src := range sources {
		if src.Kind == store.SourceGitHub {
			repo := strings.ToLower(src.Repo)
			ix.github[repo+"\x00"+src.Path] = true
			ix.githubBase[repo+"\x00"+folderName(src.Repo, src.Path)] = true
		} else {
			ix.local[localKey(src.Kind, src.Repo, src.Path)] = true
		}
	}
	return ix, nil
}

// folderName: numele folderului skill-ului; la radacina repo-ului, numele repo-ului.
func folderName(repo, p string) string {
	if p == "" {
		_, name, _ := strings.Cut(repo, "/")
		return name
	}
	return path.Base(p)
}

// suggestName face un nume valid in Library ([a-z][a-z0-9-], 2-40) din numele unui skill; "" daca nu are litere/cifre.
func suggestName(raw string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(raw) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			if dash && b.Len() > 0 {
				b.WriteByte('-')
			}
			b.WriteRune(r)
			dash = false
		} else {
			dash = true
		}
	}
	name := b.String()
	if name == "" {
		return ""
	}
	if name[0] < 'a' || name[0] > 'z' || len(name) < 2 {
		name = "skill-" + name
	}
	if len(name) > 40 {
		name = strings.TrimRight(name[:40], "-")
	}
	return name
}

// clipDescription: descrierea din frontmatter, cel mult 300 de caractere (limita din Library).
func clipDescription(d string) string {
	d = strings.Join(strings.Fields(d), " ")
	if r := []rune(d); len(r) > 300 {
		return string(r[:299]) + "…"
	}
	return d
}

// --- erori ---

// remoteErr traduce erorile din skillsrc; notFound inlocuieste mesajul pentru ErrNotFound (cand mesajul
// erorii are un URL), "" = mesajul erorii.
func remoteErr(w http.ResponseWriter, err error, notFound string) {
	switch {
	case errors.Is(err, skillsrc.ErrRateLimited):
		writeErr(w, http.StatusServiceUnavailable, rateLimitedMsg)
	case errors.Is(err, skillsrc.ErrNotFound):
		if notFound == "" {
			notFound = err.Error()
		}
		writeErr(w, http.StatusNotFound, notFound)
	case errors.Is(err, skillsrc.ErrBadArchive), errors.Is(err, skillsrc.ErrTooLarge):
		writeErr(w, http.StatusUnprocessableEntity, err.Error())
	case errors.Is(err, skillsrc.ErrUnavailable):
		writeErr(w, http.StatusBadGateway, githubDownMsg)
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		writeErr(w, http.StatusBadGateway, githubDownMsg)
	default:
		fail(w, err)
	}
}

// --- cautare si skill-urile din Claude ---

type hitView struct {
	skillsrc.Hit
	Repo      string `json:"repo"`
	InLibrary bool   `json:"inLibrary"`
}

type officialView struct {
	skillsrc.Official
	Repo      string `json:"repo"`
	SkillID   string `json:"skillId"`
	InLibrary bool   `json:"inLibrary"`
}

// searchSkills: GET /api/library/skills/search?q= — skills.sh; q gol = lista Official (anthropics/skills).
func (s *Server) searchSkills(w http.ResponseWriter, r *http.Request) {
	if !s.libraryOK(w) {
		return
	}
	ix, err := s.skillLibraryIndex()
	if err != nil {
		fail(w, err)
		return
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		list, err := s.skills.Official(r.Context())
		if err != nil {
			if errors.Is(err, skillsrc.ErrRateLimited) {
				writeErr(w, http.StatusServiceUnavailable, rateLimitedMsg)
			} else {
				writeErr(w, http.StatusBadGateway, skillsDownMsg)
			}
			return
		}
		out := make([]officialView, 0, len(list))
		for _, o := range list {
			in := ix.names[suggestName(o.Name)] || ix.github[strings.ToLower(skillsrc.OfficialRepo)+"\x00"+o.Path]
			out = append(out, officialView{Official: o, Repo: skillsrc.OfficialRepo, SkillID: o.Name, InLibrary: in})
		}
		writeJSON(w, http.StatusOK, map[string]any{"official": out})
		return
	}
	hits, err := s.skills.Search(r.Context(), q)
	if err != nil {
		writeErr(w, http.StatusBadGateway, skillsDownMsg)
		return
	}
	out := make([]hitView, 0, len(hits))
	for _, h := range hits {
		in := ix.names[suggestName(h.Name)] || ix.names[suggestName(h.SkillID)] ||
			ix.githubBase[strings.ToLower(h.Source)+"\x00"+h.SkillID]
		out = append(out, hitView{Hit: h, Repo: h.Source, InLibrary: in})
	}
	writeJSON(w, http.StatusOK, map[string]any{"skills": out})
}

type localSkillView struct {
	skillsrc.LocalSkill
	InLibrary bool `json:"inLibrary"`
}

// localSkills: GET /api/library/skills/local — skill-urile din Claude Code, claude.ai si plugin-uri.
func (s *Server) localSkills(w http.ResponseWriter, r *http.Request) {
	if !s.libraryOK(w) {
		return
	}
	out := []localSkillView{}
	home, err := s.home()
	if err != nil || home == "" {
		writeJSON(w, http.StatusOK, map[string]any{"skills": out})
		return
	}
	ix, err := s.skillLibraryIndex()
	if err != nil {
		fail(w, err)
		return
	}
	for _, ls := range skillsrc.LocalSkills(home) {
		in := ix.names[suggestName(ls.Name)] || ix.local[localKey(ls.Kind, "", ls.Path)] ||
			(ls.PluginKey != "" && ix.local[localKey(ls.Kind, ls.PluginKey, ls.Name)])
		out = append(out, localSkillView{LocalSkill: ls, InLibrary: in})
	}
	writeJSON(w, http.StatusOK, map[string]any{"skills": out})
}

// --- pregatire ---

// sourceInfo: sursa asa cum o vede fereastra de Install.
type sourceInfo struct {
	Kind  string `json:"kind"`
	Repo  string `json:"repo"`
	Path  string `json:"path"`
	Ref   string `json:"ref"`
	Label string `json:"label"`
}

type prepView struct {
	Token       string              `json:"token"`
	Name        string              `json:"name"`
	Description string              `json:"description"`
	Files       []skillsrc.FileInfo `json:"files"`
	Source      sourceInfo          `json:"source"`
	Diff        []fileChange        `json:"diff,omitempty"` // doar la source/prepare
}

func viewOf(token string, pr *prepared, diff []fileChange) prepView {
	src := pr.source
	return prepView{Token: token, Name: pr.name, Description: pr.description, Files: pr.files, Diff: diff,
		Source: sourceInfo{Kind: src.Kind, Repo: src.Repo, Path: src.Path, Ref: src.Ref, Label: src.Label}}
}

// fill completeaza fisierele, numele sugerat si descrierea din folderul adus. false = raspunsul de eroare
// e deja scris si folderul sters.
func fill(w http.ResponseWriter, pr *prepared, fallbackName string) bool {
	files, err := skillsrc.ListFiles(pr.dir)
	if err != nil {
		os.RemoveAll(pr.dir)
		fail(w, err)
		return false
	}
	hasMD := false
	for _, f := range files {
		hasMD = hasMD || f.Path == "SKILL.md"
	}
	if !hasMD {
		os.RemoveAll(pr.dir)
		writeErr(w, http.StatusUnprocessableEntity, noSkillMDMsg)
		return false
	}
	md, err := library.ReadFileIn(pr.dir, "SKILL.md")
	if err != nil && !errors.Is(err, library.ErrTooBig) {
		os.RemoveAll(pr.dir)
		fail(w, err)
		return false
	}
	fmName, desc := skillsrc.Frontmatter(md)
	pr.files, pr.description = files, clipDescription(desc)
	if pr.name == "" {
		pr.name = suggestName(fmName)
	}
	if pr.name == "" {
		pr.name = suggestName(fallbackName)
	}
	return true
}

// store pune pregatirea in harta si scrie raspunsul.
func (s *Server) storePrep(w http.ResponseWriter, pr *prepared, diff []fileChange) {
	pr.created = s.now()
	token, err := s.preps.add(pr, pr.created)
	if err != nil {
		os.RemoveAll(pr.dir)
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, viewOf(token, pr, diff))
}

// fetchGitHub aduce skillID din repo la commit-ul sha in dir. Ref-ul intors e ultimul commit pe folderul
// skill-ului pana la sha (acelasi continut ca la sha), ca verificarea pe cale sa nu vada un update fals; daca GitHub nu
// raspunde la a doua cerere (ex. limita) ramane sha, iar skillsrc.CheckSource recunoaste ca acelasi continut.
func (s *Server) fetchGitHub(ctx context.Context, repo, sha, skillID, dir string) (store.LibrarySource, error) {
	p, err := s.skills.Fetch(ctx, repo, sha, skillID, dir)
	if err != nil {
		return store.LibrarySource{}, err
	}
	ref := sha
	if p != "" {
		if c, err := s.skills.PathCommitAt(ctx, repo, p, sha); err == nil {
			ref = c
		}
	}
	return store.LibrarySource{Kind: store.SourceGitHub, Repo: repo, Path: p, Ref: ref, Label: repo}, nil
}

// prepareSkill: POST /api/library/skills/prepare {kind: "github", repo, skillId} | {kind: "local", path}.
func (s *Server) prepareSkill(w http.ResponseWriter, r *http.Request) {
	if !s.libraryOK(w) {
		return
	}
	var in struct {
		Kind    string `json:"kind"`
		Repo    string `json:"repo"`
		SkillID string `json:"skillId"`
		Path    string `json:"path"`
	}
	if err := decode(r, &in); err != nil {
		fail(w, err)
		return
	}
	switch in.Kind {
	case "github":
		if !skillsrc.ValidRepo(in.Repo) || !skillsrc.ValidSkillID(in.SkillID) {
			fail(w, badRequest("Invalid repository or skill id."))
			return
		}
		sha, err := s.skills.HeadCommit(r.Context(), in.Repo)
		if err != nil {
			remoteErr(w, err, "Couldn't find "+in.Repo+" on GitHub.")
			return
		}
		dir, err := s.lib.NewStaging()
		if err != nil {
			fail(w, err)
			return
		}
		src, err := s.fetchGitHub(r.Context(), in.Repo, sha, in.SkillID, dir)
		if err != nil {
			os.RemoveAll(dir)
			remoteErr(w, err, "")
			return
		}
		pr := &prepared{dir: dir, source: src}
		if fill(w, pr, in.SkillID) {
			s.storePrep(w, pr, nil)
		}
	case "local":
		// doar o cale din lista intoarsa de /local: nimic altceva de pe disc nu se citeste
		home, err := s.home()
		var found *skillsrc.LocalSkill
		if err == nil && home != "" && in.Path != "" {
			want := filepath.Clean(in.Path)
			for _, ls := range skillsrc.LocalSkills(home) {
				if ls.Path == want {
					found = &ls
					break
				}
			}
		}
		if found == nil {
			fail(w, badRequest(unknownLocal))
			return
		}
		src := store.LibrarySource{Kind: found.Kind, Path: found.Path, Label: found.Label}
		if found.PluginKey != "" {
			// plugin: dupa cheie si numele skill-ului, ca update-ul sa urmeze versiunile plugin-ului
			src.Repo, src.Path = found.PluginKey, found.Name
		}
		pr, ok := s.copyLocal(w, home, src)
		if ok && fill(w, pr, found.Name) {
			s.storePrep(w, pr, nil)
		}
	default:
		fail(w, badRequest("kind: github or local"))
	}
}

// copyLocal copiaza folderul de acum al sursei locale (skillsrc.ResolveLocal) intr-un folder de lucru;
// Ref = amprenta copiei, Label = eticheta de acum (la plugin-uri cu versiunea instalata acum).
func (s *Server) copyLocal(w http.ResponseWriter, home string, src store.LibrarySource) (*prepared, bool) {
	from, label, err := skillsrc.ResolveLocal(home, src)
	if errors.Is(err, skillsrc.ErrNotFound) {
		writeErr(w, http.StatusNotFound, sourceGoneMsg)
		return nil, false
	} else if err != nil {
		fail(w, err)
		return nil, false
	}
	src.Label = label
	dir, err := s.lib.NewStaging()
	if err != nil {
		fail(w, err)
		return nil, false
	}
	err = skillsrc.CopyLocal(from, dir)
	if err == nil {
		src.Ref, err = skillsrc.Fingerprint(dir)
	}
	if err != nil {
		os.RemoveAll(dir)
		if errors.Is(err, fs.ErrNotExist) {
			writeErr(w, http.StatusNotFound, sourceGoneMsg)
		} else {
			remoteErr(w, err, "")
		}
		return nil, false
	}
	return &prepared{dir: dir, source: src}, true
}

// prepFile: GET /api/library/skills/prepare/{token}/file?path= — un fisier pregatit, ca text (previzualizare).
func (s *Server) prepFile(w http.ResponseWriter, r *http.Request) {
	if !s.libraryOK(w) {
		return
	}
	pr, ok := s.preps.get(r.PathValue("token"), s.now(), false)
	if !ok {
		writeErr(w, http.StatusGone, prepGoneMsg)
		return
	}
	data, err := library.ReadFileIn(pr.dir, r.URL.Query().Get("path"))
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

// dropPrep: DELETE /api/library/skills/prepare/{token} — fereastra s-a inchis.
func (s *Server) dropPrep(w http.ResponseWriter, r *http.Request) {
	s.preps.drop(r.PathValue("token"))
	w.WriteHeader(http.StatusNoContent)
}

func filePaths(files []skillsrc.FileInfo) []string {
	out := make([]string, len(files))
	for i, f := range files {
		out[i] = f.Path
	}
	return out
}

// --- install ---

// installSkill: POST /api/library/skills/install {token, name, description?, projectId?} -> 201 itemView.
// Validarea si 409 se fac inainte sa se consume tokenul (utilizatorul poate alege alt nume).
func (s *Server) installSkill(w http.ResponseWriter, r *http.Request) {
	if !s.libraryOK(w) {
		return
	}
	var in struct {
		Token       string `json:"token"`
		Name        string `json:"name"`
		Description string `json:"description"` // optional; implicit descrierea din SKILL.md
		ProjectID   int64  `json:"projectId"`
	}
	if err := decode(r, &in); err != nil {
		fail(w, err)
		return
	}
	pr, ok := s.preps.get(in.Token, s.now(), false)
	if !ok {
		writeErr(w, http.StatusGone, prepGoneMsg)
		return
	}
	if pr.itemID != 0 {
		fail(w, badRequest("This skill was prepared for an update, not an install."))
		return
	}
	desc := strings.TrimSpace(in.Description)
	if desc == "" {
		desc = pr.description
	}
	if desc == "" {
		desc = strings.TrimSpace(in.Name)
	}
	paths := filePaths(pr.files)
	v, err := library.ValidateInput(mcp.LibraryInput{Kind: store.KindSkill, Name: in.Name, Description: desc, Files: paths}, true)
	if err != nil {
		fail(w, badRequest(err.Error()))
		return
	}
	if in.ProjectID != 0 {
		if _, err := s.st.GetProject(in.ProjectID); err != nil {
			fail(w, err)
			return
		}
	}
	conflict := func() { writeErr(w, http.StatusConflict, s.lib.NameTakenMsg(v.Kind, v.Name)) }
	if _, err := s.st.FindLibraryItem(v.Kind, v.Name); err == nil {
		conflict()
		return
	} else if !errors.Is(err, store.ErrNotFound) {
		fail(w, err)
		return
	}
	// de aici tokenul e al nostru: un al doilea Install pe el primeste 410
	if pr, ok = s.preps.get(in.Token, s.now(), true); !ok {
		writeErr(w, http.StatusGone, prepGoneMsg)
		return
	}
	defer os.RemoveAll(pr.dir)

	// aceleasi verificari ca la orice element (cai, symlink-uri, limite), intr-un folder de lucru nou
	staging, _, _, err := s.lib.CopyStaged(store.KindSkill, pr.dir, paths)
	if err != nil {
		// eroarea are cai de pe disc: doar in log
		log.Printf("skill install: copy %s: %v", pr.dir, err)
		writeErr(w, http.StatusUnprocessableEntity, copyFailedMsg)
		return
	}
	defer os.RemoveAll(staging) // dupa AdoptStaged nu mai exista

	it := store.LibraryItem{Kind: v.Kind, Name: v.Name, Description: v.Description, MadeBy: "You",
		Status: store.ItemDeclined} // ascuns cat se pregateste; PublishSkipTest il face published
	if err := s.st.AddLibraryItem(&it); errors.Is(err, store.ErrConflict) {
		// alt Install a luat numele intre verificarea de sus si acum: tokenul e deja consumat (folderul se
		// sterge), deci utilizatorul redeschide skill-ul. Cazul e rar si nu merita o rezervare a numelui.
		conflict()
		return
	} else if err != nil {
		fail(w, err)
		return
	}
	// ca la install-mcp: randurile (versiuni, instalari, sursa) cad prin cascade, plus folderele
	undo := func() {
		_ = s.st.DeleteLibraryItem(it.ID)
		s.lib.DiscardProposal(it.ID)
		_ = os.RemoveAll(filepath.Join(s.lib.Root, "items", strconv.FormatInt(it.ID, 10)))
	}
	if _, err := s.lib.AdoptStaged(staging, it.ID); err != nil {
		undo()
		fail(w, err)
		return
	}
	ver, err := s.lib.PublishSkipTest(it.ID, "")
	if err != nil {
		undo()
		libraryErr(w, err)
		return
	}
	src := pr.source
	src.ItemID, src.Latest = it.ID, src.Ref
	if err := s.st.SetLibrarySource(src); err != nil {
		undo()
		fail(w, err)
		return
	}
	// tocmai am adus sursa: pagina elementului nu are de ce sa verifice iar in urmatoarea ora
	if err := s.st.MarkSourceChecked(it.ID, src.Ref, s.now()); err != nil {
		undo()
		fail(w, err)
		return
	}
	installs := 0
	if in.ProjectID != 0 {
		if err := s.installInProject(in.ProjectID, it.ID, ver.ID, nil); err != nil {
			undo()
			fail(w, err)
			return
		}
		installs = 1
		s.hub.Publish(in.ProjectID, "project", map[string]int64{"id": in.ProjectID})
	}
	cur, err := s.st.GetLibraryItem(it.ID)
	if err != nil {
		fail(w, err)
		return
	}
	view, err := s.itemViewOf(cur, installs)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, view)
}

// --- sursa si Update ---

// homeDir: folderul utilizatorului pentru sursele locale ("" daca nu se stie: plugin-urile dupa cheie = disparute).
func (s *Server) homeDir() string {
	home, err := s.home()
	if err != nil {
		return ""
	}
	return home
}

func updateAvailable(src store.LibrarySource) bool { return src.Latest != "" && src.Latest != src.Ref }

// itemSource citeste elementul si sursa lui; false = raspunsul de eroare e scris.
func (s *Server) itemSource(w http.ResponseWriter, r *http.Request) (store.LibraryItem, store.LibrarySource, bool) {
	id, err := pathID(r)
	if err != nil {
		fail(w, err)
		return store.LibraryItem{}, store.LibrarySource{}, false
	}
	it, err := s.st.GetLibraryItem(id)
	if err != nil {
		fail(w, err)
		return it, store.LibrarySource{}, false
	}
	src, err := s.st.GetLibrarySource(id)
	if errors.Is(err, store.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "This item has no source.")
		return it, src, false
	} else if err != nil {
		fail(w, err)
		return it, src, false
	}
	return it, src, true
}

// checkSource: POST /api/library/items/{id}/source/check -> {source, updateAvailable, note}.
// Limita GitHub: raspuns 200 fara schimbare (verificarea se amana).
func (s *Server) checkSource(w http.ResponseWriter, r *http.Request) {
	if !s.libraryOK(w) {
		return
	}
	it, src, ok := s.itemSource(w, r)
	if !ok {
		return
	}
	note := ""
	latest, err := skillsrc.CheckSource(r.Context(), s.skills, s.homeDir(), src)
	switch {
	case err == nil:
		if err := s.st.MarkSourceChecked(it.ID, latest, s.now()); err != nil {
			fail(w, err)
			return
		}
	case errors.Is(err, skillsrc.ErrRateLimited):
		// nimic nou; se incearca la urmatoarea verificare
	case errors.Is(err, skillsrc.ErrNotFound):
		note = sourceGoneMsg
		if src.Kind == store.SourceGitHub {
			note = "Couldn't find the source on GitHub."
		}
		if err := s.st.MarkSourceChecked(it.ID, src.Latest, s.now()); err != nil {
			fail(w, err)
			return
		}
	default:
		remoteErr(w, err, "")
		return
	}
	cur, err := s.st.GetLibrarySource(it.ID)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"source": cur, "updateAvailable": updateAvailable(cur), "note": note})
}

// prepareSourceUpdate: POST /api/library/items/{id}/source/prepare — aduce sursa de acum (ca prepare) si
// intoarce in plus diferentele fata de versiunea curenta a elementului.
func (s *Server) prepareSourceUpdate(w http.ResponseWriter, r *http.Request) {
	if !s.libraryOK(w) {
		return
	}
	it, src, ok := s.itemSource(w, r)
	if !ok {
		return
	}
	if it.Status != store.ItemPublished {
		writeErr(w, http.StatusConflict, notUpdatable)
		return
	}
	latest, err := skillsrc.CheckSource(r.Context(), s.skills, s.homeDir(), src)
	switch {
	case err == nil:
		if err := s.st.MarkSourceChecked(it.ID, latest, s.now()); err != nil {
			fail(w, err)
			return
		}
	case errors.Is(err, skillsrc.ErrRateLimited) && src.Latest != "":
		latest = src.Latest // ultima referinta vazuta; arhiva (codeload) nu intra in limita API
	case errors.Is(err, skillsrc.ErrNotFound) && src.Kind != store.SourceGitHub:
		writeErr(w, http.StatusNotFound, sourceGoneMsg)
		return
	default:
		remoteErr(w, err, "Couldn't find the source on GitHub.")
		return
	}

	var pr *prepared
	if src.Kind == store.SourceGitHub {
		dir, err := s.lib.NewStaging()
		if err != nil {
			fail(w, err)
			return
		}
		// calea exacta salvata la install: un alt folder cu acelasi nume nu il inlocuieste
		if err := s.skills.FetchPath(r.Context(), src.Repo, latest, src.Path, dir); err != nil {
			os.RemoveAll(dir)
			remoteErr(w, err, "")
			return
		}
		next := src
		next.Ref = latest
		pr = &prepared{dir: dir, source: next}
	} else {
		if pr, ok = s.copyLocal(w, s.homeDir(), src); !ok {
			return
		}
	}
	pr.itemID, pr.name, pr.baseRef = it.ID, it.Name, src.Ref
	if !fill(w, pr, it.Name) {
		return
	}
	oldDir := ""
	if vs, err := s.st.ListLibraryVersions(it.ID); err != nil {
		os.RemoveAll(pr.dir)
		fail(w, err)
		return
	} else if len(vs) > 0 {
		oldDir = vs[0].Dir
	}
	diff, err := skillDiff(oldDir, pr.dir)
	if err != nil {
		os.RemoveAll(pr.dir)
		fail(w, err)
		return
	}
	s.storePrep(w, pr, diff)
}

// updateFromSource: POST /api/library/items/{id}/source/update {token} -> 200 itemView cu versiunea noua.
func (s *Server) updateFromSource(w http.ResponseWriter, r *http.Request) {
	if !s.libraryOK(w) {
		return
	}
	it, src, ok := s.itemSource(w, r)
	if !ok {
		return
	}
	var in struct {
		Token string `json:"token"`
	}
	if err := decode(r, &in); err != nil {
		fail(w, err)
		return
	}
	pr, ok := s.preps.get(in.Token, s.now(), false)
	if !ok {
		writeErr(w, http.StatusGone, prepGoneMsg)
		return
	}
	if pr.itemID != it.ID {
		fail(w, badRequest("This update was prepared for another item."))
		return
	}
	// verificarile inainte sa se consume tokenul: un refuz lasa fereastra deschisa
	if it.Status != store.ItemPublished {
		writeErr(w, http.StatusConflict, notUpdatable)
		return
	}
	if src.Ref != pr.baseRef {
		writeErr(w, http.StatusConflict, staleMsg)
		return
	}
	if pr, ok = s.preps.get(in.Token, s.now(), true); !ok {
		writeErr(w, http.StatusGone, prepGoneMsg)
		return
	}
	defer os.RemoveAll(pr.dir)
	staging, _, _, err := s.lib.CopyStaged(store.KindSkill, pr.dir, filePaths(pr.files))
	if err != nil {
		log.Printf("skill update: copy %s: %v", pr.dir, err)
		writeErr(w, http.StatusUnprocessableEntity, copyFailedMsg)
		return
	}
	defer os.RemoveAll(staging) // dupa adoptare nu mai exista
	// mutarea in propunere si publicarea sub acelasi lock al elementului; versiunea noua are exact fisierele
	// sursei (fara cele sterse intre timp la sursa)
	if _, err := s.lib.PublishReplaceStaged(it.ID, staging, "Updated from "+pr.source.Label); errors.Is(err, library.ErrStatus) {
		writeErr(w, http.StatusConflict, notUpdatable)
		return
	} else if err != nil {
		libraryErr(w, err)
		return
	}
	next := pr.source
	next.ItemID, next.Latest = it.ID, next.Ref
	if err := s.st.SetLibrarySource(next); err != nil {
		fail(w, err)
		return
	}
	if err := s.st.MarkSourceChecked(it.ID, next.Ref, s.now()); err != nil {
		fail(w, err)
		return
	}
	cur, err := s.st.GetLibraryItem(it.ID)
	if err != nil {
		fail(w, err)
		return
	}
	installs, err := s.st.ListItemInstalls(it.ID)
	if err != nil {
		fail(w, err)
		return
	}
	view, err := s.itemViewOf(cur, len(installs))
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

// --- diferente ---

// fileChange: un fisier diferit intre versiunea curenta si sursa adusa. Before/After lipsesc pentru fisierele
// binare (Binary), mai mari de MaxViewBytes (TooLarge) si cand s-a schimbat doar bitul executabil ("mode").
type fileChange struct {
	Path     string  `json:"path"`
	Change   string  `json:"change"` // added | removed | modified | mode
	Before   *string `json:"before,omitempty"`
	After    *string `json:"after,omitempty"`
	Binary   bool    `json:"binary,omitempty"`
	TooLarge bool    `json:"tooLarge,omitempty"`
}

// skillDiff compara fisierele din oldDir ("" = niciunul) cu cele din newDir, sortat dupa cale.
func skillDiff(oldDir, newDir string) ([]fileChange, error) {
	sizes := func(dir string) (map[string]int64, error) {
		out := map[string]int64{}
		if dir == "" {
			return out, nil
		}
		files, err := skillsrc.ListFiles(dir)
		for _, f := range files {
			out[f.Path] = f.Size
		}
		return out, err
	}
	before, err := sizes(oldDir)
	if err != nil {
		return nil, err
	}
	after, err := sizes(newDir)
	if err != nil {
		return nil, err
	}
	all := map[string]bool{}
	for p := range before {
		all[p] = true
	}
	for p := range after {
		all[p] = true
	}
	paths := make([]string, 0, len(all))
	for p := range all {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	out := []fileChange{}
	for _, p := range paths {
		bs, inOld := before[p]
		as, inNew := after[p]
		c := fileChange{Path: p}
		switch {
		case inOld && inNew:
			if bs == as {
				same, err := sameFile(oldDir, newDir, p)
				if err != nil {
					return nil, err
				}
				if same {
					if isExec(oldDir, p) == isExec(newDir, p) {
						continue
					}
					c.Change = "mode" // acelasi continut, alt bit executabil: fara text
					out = append(out, c)
					continue
				}
			}
			c.Change = "modified"
		case inNew:
			c.Change = "added"
		default:
			c.Change = "removed"
		}
		if inOld {
			if err := c.attach(oldDir, p, &c.Before); err != nil {
				return nil, err
			}
		}
		if inNew {
			if err := c.attach(newDir, p, &c.After); err != nil {
				return nil, err
			}
		}
		if c.Binary || c.TooLarge {
			c.Before, c.After = nil, nil
		}
		out = append(out, c)
	}
	return out, nil
}

// attach pune textul fisierului in dst, sau marcheaza Binary / TooLarge.
func (c *fileChange) attach(dir, rel string, dst **string) error {
	data, err := library.ReadFileIn(dir, rel)
	if errors.Is(err, library.ErrTooBig) {
		c.TooLarge = true
		return nil
	}
	if err != nil {
		return err
	}
	head := data
	if len(head) > 8<<10 {
		head = head[:8<<10]
	}
	if bytes.IndexByte(head, 0) >= 0 || !utf8.Valid(data) {
		c.Binary = true
		return nil
	}
	s := string(data)
	*dst = &s
	return nil
}

// isExec: fisierul are bitul executabil (folderele sunt ale noastre: pregatirea sau o versiune).
func isExec(dir, rel string) bool {
	fi, err := os.Lstat(filepath.Join(dir, filepath.FromSlash(rel)))
	return err == nil && fi.Mode()&0o111 != 0
}

// sameFile compara continutul (sha256) aceluiasi fisier din doua foldere, citit prin os.OpenRoot.
func sameFile(a, b, rel string) (bool, error) {
	ha, err := hashIn(a, rel)
	if err != nil {
		return false, err
	}
	hb, err := hashIn(b, rel)
	if err != nil {
		return false, err
	}
	return ha == hb, nil
}

func hashIn(dir, rel string) (string, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return "", err
	}
	defer root.Close()
	f, err := root.Open(filepath.FromSlash(rel))
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
