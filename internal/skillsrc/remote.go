// Package skillsrc aduce skill-uri din afara Library: cautarea pe skills.sh, skill-urile oficiale
// Anthropic si arhivele de pe GitHub (remote.go, archive.go), skill-urile deja instalate in Claude
// (local.go) si amprenta unui folder (fingerprint.go).
package skillsrc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"
)

var (
	// ErrUnavailable: reteaua, un status != 200 sau un raspuns invalid.
	ErrUnavailable = errors.New("skills source unavailable")
	// ErrRateLimited: GitHub limiteaza cererile (403 cu X-RateLimit-Remaining: 0, sau 429).
	ErrRateLimited = errors.New("github rate limited")
	// ErrNotFound: skill-ul (sau repo-ul / commit-ul) nu exista.
	ErrNotFound = errors.New("skill not found")
	// ErrTooLarge: skill-ul trece de limitele Library sau arhiva de limita de citire.
	ErrTooLarge = errors.New("skill too large")
	// ErrBadArchive: arhiva are o cale interzisa sau un format stricat.
	ErrBadArchive = errors.New("bad skill archive")
)

// srcError e o eroare cu mesaj pentru utilizator care raspunde la errors.Is(err, kind).
type srcError struct {
	msg  string
	kind error
}

func (e *srcError) Error() string { return e.msg }
func (e *srcError) Unwrap() error { return e.kind }

func errorf(kind error, format string, args ...any) error {
	return &srcError{msg: fmt.Sprintf(format, args...), kind: kind}
}

const (
	OfficialRepo = "anthropics/skills"

	searchTTL   = 10 * time.Minute
	officialTTL = time.Hour
	maxJSON     = 4 << 20 // raspunsurile JSON sunt mici; limita doar ca protectie
	maxSkillMD  = 256 << 10
	maxCached   = 200 // cautari tinute in cache
)

// Remote vorbeste cu skills.sh si GitHub. Doar GET, fara credentiale.
type Remote struct {
	SkillsURL, APIURL, CodeloadURL, RawURL string
	HTTP                                   *http.Client // pentru cererile mici (timeout 30 s)
	fetchHTTP                              *http.Client // pentru arhive, vezi fetchClient

	mu       sync.Mutex
	searches map[string]searchEntry
	official []Official
	offAt    time.Time
}

type searchEntry struct {
	hits []Hit
	at   time.Time
}

// NewRemote ia URL-urile de baza din HOBSTACK_* (doar pentru teste); o valoare care nu e
// http/https cu host lasa implicitul.
func NewRemote() *Remote {
	r := &Remote{
		SkillsURL:   baseURL("HOBSTACK_SKILLS_URL", "https://skills.sh"),
		APIURL:      baseURL("HOBSTACK_GITHUB_API_URL", "https://api.github.com"),
		CodeloadURL: baseURL("HOBSTACK_GITHUB_CODELOAD_URL", "https://codeload.github.com"),
		RawURL:      baseURL("HOBSTACK_GITHUB_RAW_URL", "https://raw.githubusercontent.com"),
		HTTP:        &http.Client{Timeout: 30 * time.Second},
	}
	r.fetchClient()
	return r
}

func baseURL(env, def string) string {
	v := strings.TrimRight(strings.TrimSpace(os.Getenv(env)), "/")
	if v == "" {
		return def
	}
	u, err := url.Parse(v)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return def
	}
	return v
}

// partRe: o parte din owner/repo sau un skillId.
var partRe = regexp.MustCompile(`^[A-Za-z0-9._-]{1,100}$`)

func validPart(s string) bool {
	return partRe.MatchString(s) && !strings.Contains(s, "..") && s != "."
}

// ValidRepo: "owner/repo", fiecare parte cu [A-Za-z0-9._-], fara "..".
func ValidRepo(repo string) bool {
	owner, name, ok := strings.Cut(repo, "/")
	return ok && validPart(owner) && validPart(name)
}

// ValidSkillID: [A-Za-z0-9._-], fara "..".
func ValidSkillID(id string) bool { return validPart(id) }

var shaRe = regexp.MustCompile(`^[0-9a-fA-F]{7,64}$`)

// get face cererea GET cu clientul obisnuit si intoarce corpul (de inchis de apelant) doar pentru 200.
func (r *Remote) get(ctx context.Context, u string) (io.ReadCloser, error) {
	client := r.HTTP
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	return r.getWith(ctx, client, u)
}

func (r *Remote) getWith(ctx context.Context, client *http.Client, u string) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	req.Header.Set("User-Agent", "HobStack")
	resp, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	if resp.StatusCode == http.StatusOK {
		return resp.Body, nil
	}
	resp.Body.Close()
	switch {
	// limita primara (403 + X-RateLimit-Remaining: 0), cea secundara (403 + Retry-After) si 429
	case resp.StatusCode == http.StatusTooManyRequests,
		resp.StatusCode == http.StatusForbidden && (resp.Header.Get("X-RateLimit-Remaining") == "0" ||
			resp.Header.Get("Retry-After") != ""):
		return nil, errorf(ErrRateLimited, "GitHub is rate limiting requests; try again in a few minutes.")
	case resp.StatusCode == http.StatusNotFound:
		return nil, fmt.Errorf("%w: %s returned 404", ErrNotFound, redactURL(u))
	}
	return nil, fmt.Errorf("%w: %s returned %d", ErrUnavailable, redactURL(u), resp.StatusCode)
}

// redactURL scoate query-ul din mesajele de eroare (doar calea conteaza).
func redactURL(u string) string {
	if i := strings.IndexByte(u, '?'); i >= 0 {
		return u[:i]
	}
	return u
}

// getJSON decodeaza raspunsul in v; JSON invalid = ErrUnavailable.
func (r *Remote) getJSON(ctx context.Context, u string, v any) error {
	body, err := r.get(ctx, u)
	if err != nil {
		return err
	}
	defer body.Close()
	if err := json.NewDecoder(io.LimitReader(body, maxJSON)).Decode(v); err != nil {
		return fmt.Errorf("%w: bad response from %s: %v", ErrUnavailable, redactURL(u), err)
	}
	return nil
}

// Hit e un rezultat de la skills.sh.
type Hit struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Source   string `json:"source"` // owner/repo
	SkillID  string `json:"skillId"`
	Installs int    `json:"installs"`
	Official bool   `json:"official"`
}

// Search cauta pe skills.sh. Sub 2 caractere: nil, nil fara cerere. Rezultatele Official
// (anthropics/skills) primele, apoi ordinea skills.sh; cache 10 min pe text.
func (r *Remote) Search(ctx context.Context, q string) ([]Hit, error) {
	q = strings.TrimSpace(q)
	if utf8.RuneCountInString(q) < 2 {
		return nil, nil
	}
	r.mu.Lock()
	if e, ok := r.searches[q]; ok && time.Since(e.at) < searchTTL {
		r.mu.Unlock()
		return slices.Clone(e.hits), nil
	}
	r.mu.Unlock()

	var resp struct {
		Skills []Hit `json:"skills"`
	}
	if err := r.getJSON(ctx, r.SkillsURL+"/api/search?q="+url.QueryEscape(q), &resp); err != nil {
		if errors.Is(err, ErrNotFound) || errors.Is(err, ErrRateLimited) {
			// pentru skills.sh orice raspuns neasteptat inseamna "indisponibil"
			return nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
		}
		return nil, err
	}
	hits := make([]Hit, 0, len(resp.Skills))
	for _, h := range resp.Skills {
		// datele sunt externe: pastram doar ce se poate instala
		if !ValidRepo(h.Source) || !ValidSkillID(h.SkillID) {
			continue
		}
		h.Official = strings.EqualFold(h.Source, OfficialRepo)
		if h.Name == "" {
			h.Name = h.SkillID
		}
		hits = append(hits, h)
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].Official && !hits[j].Official })

	r.mu.Lock()
	if r.searches == nil || len(r.searches) >= maxCached {
		r.searches = map[string]searchEntry{}
	}
	r.searches[q] = searchEntry{hits: slices.Clone(hits), at: time.Now()}
	r.mu.Unlock()
	return hits, nil
}

// Official e un skill din anthropics/skills/skills.
type Official struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Path        string `json:"path"` // calea in repo, ex. "skills/pdf"
}

// Official intoarce skill-urile oficiale (folderele din anthropics/skills/skills) cu descrierea
// din SKILL.md (raw.githubusercontent.com, nu consuma din limita API); cache 1 h.
func (r *Remote) Official(ctx context.Context) ([]Official, error) {
	r.mu.Lock()
	if r.official != nil && time.Since(r.offAt) < officialTTL {
		list := slices.Clone(r.official)
		r.mu.Unlock()
		return list, nil
	}
	r.mu.Unlock()

	var entries []struct {
		Name string `json:"name"`
		Path string `json:"path"`
		Type string `json:"type"`
	}
	if err := r.getJSON(ctx, r.APIURL+"/repos/"+OfficialRepo+"/contents/skills", &entries); err != nil {
		return nil, err
	}
	list := []Official{}
	for _, e := range entries {
		if e.Type == "dir" && ValidSkillID(e.Name) {
			list = append(list, Official{Name: e.Name, Path: "skills/" + e.Name})
		}
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Name < list[j].Name })

	// descrierile in paralel, cate 4; un SKILL.md care lipseste lasa descrierea goala,
	// iar lista nu se tine in cache (la urmatoarea cerere se incearca din nou)
	var failed atomic.Bool
	var wg sync.WaitGroup
	sem := make(chan struct{}, 4)
	for i := range list {
		wg.Add(1)
		go func(o *Official) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			body, err := r.get(ctx, r.RawURL+"/"+OfficialRepo+"/HEAD/"+o.Path+"/SKILL.md")
			if err != nil {
				failed.Store(true)
				return
			}
			defer body.Close()
			data, err := io.ReadAll(io.LimitReader(body, maxSkillMD))
			if err != nil {
				failed.Store(true)
			}
			_, o.Description = Frontmatter(data)
		}(&list[i])
	}
	wg.Wait()
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	if !failed.Load() {
		r.mu.Lock()
		r.official, r.offAt = slices.Clone(list), time.Now()
		r.mu.Unlock()
	}
	return list, nil
}

// HeadCommit: commit-ul curent al ramurii implicite (commits/HEAD).
func (r *Remote) HeadCommit(ctx context.Context, repo string) (string, error) {
	if !ValidRepo(repo) {
		return "", fmt.Errorf("invalid repository %q", repo)
	}
	var c struct {
		SHA string `json:"sha"`
	}
	if err := r.getJSON(ctx, r.APIURL+"/repos/"+repo+"/commits/HEAD", &c); err != nil {
		return "", err
	}
	if !shaRe.MatchString(c.SHA) {
		return "", fmt.Errorf("%w: bad commit from GitHub", ErrUnavailable)
	}
	return c.SHA, nil
}

// PathCommit: ultimul commit de pe ramura implicita care atinge calea (commits?path=&per_page=1);
// niciunul = ErrNotFound.
func (r *Remote) PathCommit(ctx context.Context, repo, path string) (string, error) {
	return r.PathCommitAt(ctx, repo, path, "")
}

// PathCommitAt: ca PathCommit, dar istoria pleaca de la commit-ul at (&sha=at); at "" = ramura implicita.
// La install: commit-ul care a schimbat ultima data folderul in arhiva descarcata (nu unul venit intre timp).
func (r *Remote) PathCommitAt(ctx context.Context, repo, path, at string) (string, error) {
	if !ValidRepo(repo) {
		return "", fmt.Errorf("invalid repository %q", repo)
	}
	if at != "" && !shaRe.MatchString(at) {
		return "", fmt.Errorf("invalid commit %q", at)
	}
	var list []struct {
		SHA string `json:"sha"`
	}
	u := r.APIURL + "/repos/" + repo + "/commits?path=" + url.QueryEscape(path) + "&per_page=1"
	if at != "" {
		u += "&sha=" + at
	}
	if err := r.getJSON(ctx, u, &list); err != nil {
		return "", err
	}
	if len(list) == 0 {
		return "", errorf(ErrNotFound, "No commits for %s in %s.", path, repo)
	}
	if !shaRe.MatchString(list[0].SHA) {
		return "", fmt.Errorf("%w: bad commit from GitHub", ErrUnavailable)
	}
	return list[0].SHA, nil
}

// fetchClient e clientul pentru arhive: fara timeout pe tot corpul (o arhiva mare pe o retea lenta
// poate dura), doar 30 s pana la header-ele raspunsului; restul il opreste ctx-ul cererii.
func (r *Remote) fetchClient() *http.Client {
	if r.fetchHTTP == nil {
		tr := http.DefaultTransport.(*http.Transport).Clone()
		tr.ResponseHeaderTimeout = 30 * time.Second
		r.fetchHTTP = &http.Client{Transport: tr}
	}
	return r.fetchHTTP
}
