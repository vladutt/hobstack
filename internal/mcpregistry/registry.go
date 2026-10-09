// Pachetul mcpregistry cauta servere MCP in registrul oficial
// (registry.modelcontextprotocol.io) si le transforma in ceva ce Library poate instala:
// o comanda locala (npx / uvx / docker) sau un URL remote. Citeste si serverele
// din configurarile Claude ale utilizatorului (local.go), doar cu numele variabilelor.
package mcpregistry

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
	"strings"
	"sync"
	"time"
	"unicode"
)

// DefaultBaseURL e registrul oficial; in teste se schimba prin FOREMAN_MCP_REGISTRY_URL.
const DefaultBaseURL = "https://registry.modelcontextprotocol.io"

// Motivele afisate in UI cand un server nu se poate instala.
const (
	ReasonNeedsArgs    = "Needs arguments Foreman can't fill in yet"
	ReasonNeedsHeaders = "Needs headers; not supported yet"
	ReasonEnvNames     = "Uses environment variable names Foreman can't store"
	ReasonCredential   = "Has a credential in its arguments or URL; move it to env in your Claude config, then install"
)

// pageLimit e cate servere cerem pe pagina.
const pageLimit = 30

// maxBody limiteaza cat citim dintr-un raspuns (protectie la raspunsuri uriase).
const maxBody = 8 << 20

// ErrUnavailable: registrul nu raspunde (timeout, status != 200, corp care nu e JSON).
var ErrUnavailable = errors.New("registry unavailable")

// EnvVar e o variabila de mediu (sau un header) cerut de server; doar numele si metadatele, niciodata valoarea.
type EnvVar struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Required    bool   `json:"required"`
	Secret      bool   `json:"secret"`
}

// Server e un rezultat normalizat din registru.
type Server struct {
	ID          string   `json:"id"` // name@version
	Name        string   `json:"name"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Version     string   `json:"version"`
	Kind        string   `json:"kind"`    // "local" | "remote"
	Runtime     string   `json:"runtime"` // "npx" | "uvx" | "docker" | ""
	Command     string   `json:"command"`
	Args        []string `json:"args"`
	URL         string   `json:"url"`
	Env         []EnvVar `json:"env"`
	// SuggestedName e un nume valid in Library ([a-z0-9-]), din ultimul segment al numelui.
	SuggestedName string `json:"suggestedName"`
	Installable   bool   `json:"installable"`
	Reason        string `json:"reason,omitempty"` // de ce nu se poate instala
}

// Client vorbeste cu registrul; rezultatele stau in cache TTL pe (search, cursor).
type Client struct {
	BaseURL string
	HTTP    *http.Client
	TTL     time.Duration

	mu    sync.Mutex
	cache map[cacheKey]cacheEntry
}

type cacheKey struct{ search, cursor string }

type cacheEntry struct {
	servers []Server
	next    string
	at      time.Time
}

// NewClient foloseste FOREMAN_MCP_REGISTRY_URL daca e setat (teste), altfel registrul oficial;
// timeout 10 s si cache 10 min.
func NewClient() *Client {
	// doar URL-uri http/https cu host; orice altceva => registrul oficial
	base := strings.TrimRight(strings.TrimSpace(os.Getenv("FOREMAN_MCP_REGISTRY_URL")), "/")
	if u, err := url.Parse(base); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		base = DefaultBaseURL
	}
	return &Client{
		BaseURL: base,
		HTTP:    &http.Client{Timeout: 10 * time.Second},
		TTL:     10 * time.Minute,
	}
}

// Search intoarce serverele instalabile (sau afisabile cu motiv) si cursorul paginii urmatoare.
// Erorile de retea / raspunsurile ciudate sunt ErrUnavailable si nu intra in cache.
func (c *Client) Search(ctx context.Context, search, cursor string) ([]Server, string, error) {
	search = strings.TrimSpace(search)
	key := cacheKey{search, cursor}
	c.mu.Lock()
	if e, ok := c.cache[key]; ok && time.Since(e.at) < c.TTL {
		c.mu.Unlock()
		return copyServers(e.servers), e.next, nil
	}
	c.mu.Unlock()

	servers, next, err := c.fetch(ctx, search, cursor)
	if err != nil {
		return nil, "", err
	}

	c.mu.Lock()
	if c.cache == nil {
		c.cache = map[cacheKey]cacheEntry{}
	}
	// curatam intrarile expirate ca harta sa nu creasca la nesfarsit
	for k, e := range c.cache {
		if time.Since(e.at) >= c.TTL {
			delete(c.cache, k)
		}
	}
	c.cache[key] = cacheEntry{servers: copyServers(servers), next: next, at: time.Now()}
	c.mu.Unlock()
	return servers, next, nil
}

// copyServers: copie adanca, ca apelantul sa nu poata schimba ce sta in cache.
func copyServers(in []Server) []Server {
	out := make([]Server, len(in))
	for i, s := range in {
		s.Args = slices.Clone(s.Args)
		s.Env = slices.Clone(s.Env)
		out[i] = s
	}
	return out
}

func (c *Client) fetch(ctx context.Context, search, cursor string) ([]Server, string, error) {
	q := url.Values{}
	if search != "" {
		q.Set("search", search)
	}
	if cursor != "" {
		q.Set("cursor", cursor)
	}
	q.Set("version", "latest")
	q.Set("limit", fmt.Sprint(pageLimit))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(c.BaseURL, "/")+"/v0/servers?"+q.Encode(), nil)
	if err != nil {
		return nil, "", fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	req.Header.Set("Accept", "application/json")
	hc := c.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: 10 * time.Second}
	}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("%w: status %d", ErrUnavailable, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return nil, "", fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	var page apiPage
	if err := json.Unmarshal(body, &page); err != nil {
		return nil, "", fmt.Errorf("%w: invalid response: %v", ErrUnavailable, err)
	}
	out := []Server{}
	for _, e := range page.Servers {
		if s, ok := normalize(e); ok {
			out = append(out, s)
		}
	}
	return out, page.Metadata.NextCursor, nil
}

// --- forma raspunsului din registru (doar campurile folosite) ---

type apiPage struct {
	Servers  []apiEntry `json:"servers"`
	Metadata struct {
		NextCursor string `json:"nextCursor"`
	} `json:"metadata"`
}

type apiEntry struct {
	Server apiServer `json:"server"`
	Meta   struct {
		Official *struct {
			Status   string `json:"status"`
			IsLatest *bool  `json:"isLatest"`
		} `json:"io.modelcontextprotocol.registry/official"`
	} `json:"_meta"`
}

type apiServer struct {
	Name        string       `json:"name"`
	Title       string       `json:"title"`
	Description string       `json:"description"`
	Version     string       `json:"version"`
	Packages    []apiPackage `json:"packages"`
	Remotes     []apiRemote  `json:"remotes"`
}

type apiPackage struct {
	RegistryType     string     `json:"registryType"`
	Identifier       string     `json:"identifier"`
	Version          string     `json:"version"`
	Transport        apiRemote  `json:"transport"`
	RuntimeArguments []apiInput `json:"runtimeArguments"`
	PackageArguments []apiInput `json:"packageArguments"`
	EnvVars          []apiInput `json:"environmentVariables"`
}

type apiRemote struct {
	Type    string     `json:"type"`
	URL     string     `json:"url"`
	Headers []apiInput `json:"headers"`
}

// apiInput acopera argumentele (positional / named), variabilele de mediu si header-ele.
type apiInput struct {
	Type        string                     `json:"type"` // "positional" | "named" | "" (unele intrari nu il au)
	Name        string                     `json:"name"`
	Value       string                     `json:"value"`
	Default     string                     `json:"default"`
	Description string                     `json:"description"`
	IsRequired  bool                       `json:"isRequired"`
	IsSecret    bool                       `json:"isSecret"`
	Variables   map[string]json.RawMessage `json:"variables"`
}

// runtimes: registryType -> comanda care il porneste.
var runtimes = map[string]string{"npm": "npx", "pypi": "uvx", "oci": "docker"}

// normalize transforma o intrare din registru; ok=false daca nu are nimic ce stim construi.
// Preferam un pachet local instalabil, apoi un remote instalabil; daca niciunul nu e instalabil,
// aratam primul candidat cu motivul.
func normalize(e apiEntry) (Server, bool) {
	if o := e.Meta.Official; o != nil {
		if o.IsLatest != nil && !*o.IsLatest {
			return Server{}, false
		}
		if o.Status == "deleted" {
			return Server{}, false
		}
	}
	s := e.Server
	if s.Name == "" {
		return Server{}, false
	}
	base := Server{
		ID:            s.Name + "@" + s.Version,
		Name:          s.Name,
		Title:         s.Title,
		Description:   s.Description,
		Version:       s.Version,
		SuggestedName: SuggestName(s.Name),
	}

	var candidates []Server
	for _, p := range s.Packages {
		if c, ok := fromPackage(base, p); ok {
			candidates = append(candidates, c)
			break // primul pachet construibil
		}
	}
	for _, r := range s.Remotes {
		if c, ok := fromRemote(base, r); ok {
			candidates = append(candidates, c)
			break // primul remote streamable-http / sse
		}
	}
	if len(candidates) == 0 {
		return Server{}, false
	}
	for _, c := range candidates {
		if c.Installable {
			return c, true
		}
	}
	return candidates[0], true
}

func fromPackage(base Server, p apiPackage) (Server, bool) {
	runtime, ok := runtimes[p.RegistryType]
	if !ok || p.Transport.Type != "stdio" || !safeIdentifier.MatchString(p.Identifier) || !safeVersion(p.Version) {
		return Server{}, false
	}
	s := base
	s.Kind = "local"
	s.Runtime = runtime
	s.Command = runtime
	s.Installable = true
	for _, v := range p.EnvVars {
		if v.Name == "" {
			continue
		}
		s.Env = append(s.Env, EnvVar{Name: v.Name, Description: v.Description, Required: v.IsRequired, Secret: v.IsSecret})
	}

	// argumentele pentru runtime (npx / uvx / docker) pot schimba de unde vine pachetul
	// (de ex. --registry) sau ce vede containerul (-v): cele optionale le ignoram,
	// iar unul obligatoriu face serverul neinstalabil
	pkgArgs, ok := buildArgs(p.PackageArguments)
	for _, a := range p.RuntimeArguments {
		if a.IsRequired {
			ok = false
		}
	}
	if !ok {
		s.Installable = false
		s.Reason = ReasonNeedsArgs
	} else if !validEnvNames(s.Env) {
		s.Installable = false
		s.Reason = ReasonEnvNames
	}

	var args []string
	switch p.RegistryType {
	case "npm":
		id := p.Identifier
		if p.Version != "" {
			id += "@" + p.Version
		}
		args = []string{"-y", id}
	case "pypi":
		id := p.Identifier
		if p.Version != "" {
			id += "==" + p.Version
		}
		args = []string{id}
	case "oci":
		args = []string{"run", "-i", "--rm"}
		for _, v := range s.Env {
			args = append(args, "-e", v.Name)
		}
		args = append(args, ociImage(p.Identifier, p.Version))
	}
	s.Args = append(args, pkgArgs...)
	return s, true
}

// safeIdentifier: identificatorul unui pachet nu poate incepe cu "-" (ar deveni optiune) si nu are
// caractere ciudate (spatii, ; etc.).
var safeIdentifier = regexp.MustCompile(`^[A-Za-z0-9@][A-Za-z0-9._/@:+-]*$`)

// safeVersion: versiunea nu incepe cu "-" si nu are spatii.
func safeVersion(v string) bool {
	return !strings.HasPrefix(v, "-") && !strings.ContainsFunc(v, unicode.IsSpace)
}

// Copie a regulii din library.ValidSecretName (internal/library/validate.go): numele variabilelor devin
// Secrets ale elementului din Library, deci trebuie sa treaca aceeasi validare. Copiata ca sa nu legam
// pachetul de library (si de store); tine-le sincronizate.
var envNameRe = regexp.MustCompile(`^[A-Z][A-Z0-9_]{1,63}$`)

var reservedEnv = []string{"PATH", "HOME", "SHELL", "LD_PRELOAD", "LD_LIBRARY_PATH", "BASH_ENV", "ENV", "IFS", "PS4"}

func validEnvName(n string) bool {
	return envNameRe.MatchString(n) && !slices.Contains(reservedEnv, n) && !strings.HasPrefix(n, "DYLD_")
}

func validEnvNames(env []EnvVar) bool {
	for _, v := range env {
		if !validEnvName(v.Name) {
			return false
		}
	}
	return true
}

// ociImage adauga ":<version>" doar daca identificatorul nu are deja tag sau digest.
func ociImage(id, version string) string {
	last := id[strings.LastIndex(id, "/")+1:]
	if version == "" || strings.Contains(last, ":") || strings.Contains(last, "@") {
		return id
	}
	return id + ":" + version
}

// buildArgs construieste argumentele: valoarea fixa sau cea implicita; un argument optional fara
// valoare e sarit; unul obligatoriu fara valoare (sau cu {variabile} de completat) => ok=false.
func buildArgs(in []apiInput) ([]string, bool) {
	var out []string
	ok := true
	for _, a := range in {
		v := a.Value
		if v == "" {
			v = a.Default
		}
		if v != "" && hasPlaceholders(v, a.Variables) {
			v = ""
		}
		named := a.Type == "named" || (a.Type == "" && a.Name != "")
		if v == "" {
			if a.IsRequired {
				ok = false
			}
			continue
		}
		if named {
			out = append(out, a.Name+"="+v)
		} else {
			out = append(out, v)
		}
	}
	return out, ok
}

// hasPlaceholders: valoarea contine {nume} care trebuie completat de utilizator.
func hasPlaceholders(v string, vars map[string]json.RawMessage) bool {
	for name := range vars {
		if strings.Contains(v, "{"+name+"}") {
			return true
		}
	}
	return false
}

func fromRemote(base Server, r apiRemote) (Server, bool) {
	if (r.Type != "streamable-http" && r.Type != "sse") || r.URL == "" {
		return Server{}, false
	}
	s := base
	s.Kind = "remote"
	s.URL = r.URL
	s.Installable = true
	for _, h := range r.Headers {
		if h.IsRequired {
			s.Installable = false
			s.Reason = ReasonNeedsHeaders
			break
		}
	}
	// URL-uri sablon ({tenant} etc.) nu le putem completa inca
	if s.Installable && strings.Contains(r.URL, "{") {
		s.Installable = false
		s.Reason = ReasonNeedsArgs
	}
	return s, true
}

var notNameChar = regexp.MustCompile(`[^a-z0-9-]+`)
var dashes = regexp.MustCompile(`-{2,}`)

// SuggestName face un nume pentru Library din numele serverului: segmentul de dupa ultimul "/",
// litere mici, orice altceva decat [a-z0-9-] devine "-", fara "-" la capete, max 40.
// Numele din Library trebuie sa inceapa cu o litera, asa ca prefixam "mcp-" cand incepe cu o cifra.
func SuggestName(name string) string {
	n := strings.ToLower(name[strings.LastIndex(name, "/")+1:])
	n = notNameChar.ReplaceAllString(n, "-")
	n = dashes.ReplaceAllString(n, "-")
	n = strings.Trim(n, "-")
	if n == "" {
		return "mcp"
	}
	if n[0] >= '0' && n[0] <= '9' {
		n = "mcp-" + n
	}
	if len(n) > 40 {
		n = strings.TrimRight(n[:40], "-")
	}
	// numele din Library au minim 2 caractere, iar "team" e serverul MCP al aplicatiei
	if len(n) < 2 {
		n = "mcp-" + n
	}
	if n == "team" {
		n = "team-mcp"
	}
	return n
}
