package mcpregistry

// Catalogul local al registrului MCP: o copie a registrului intr-un fisier SQLite separat
// (~/.hobstack/mcp-registry.db), sincronizata din registru, in care cautarea se face local.
// Se pastreaza intrarea bruta din registru; normalize se aplica la cautare, ca regulile noi
// de construire a comenzii sa se vada fara o descarcare noua.

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	_ "modernc.org/sqlite"
)

// catalogVersion e versiunea schemei (PRAGMA user_version); alta valoare in fisier => fisierul se reface.
const catalogVersion = 1

const catalogSchema = `
CREATE TABLE servers (name TEXT PRIMARY KEY, version TEXT NOT NULL, title TEXT NOT NULL DEFAULT '',
  description TEXT NOT NULL DEFAULT '', search TEXT NOT NULL, entry TEXT NOT NULL, updated_at TEXT NOT NULL);
CREATE INDEX servers_updated ON servers(updated_at DESC, name);
CREATE TABLE meta (key TEXT PRIMARY KEY, value TEXT NOT NULL);
`

const (
	syncPageLimit  = 100 // servere pe pagina la sincronizare
	searchPageSize = 30  // rezultate pe pagina la cautare
	maxSearchWords = 8
	// syncEvery: dupa cat timp o actualizare e din nou necesara
	syncEvery = 24 * time.Hour
	// overlap: actualizarea cere si ultima ora dinaintea sincronizarii precedente, ca sa nu pierdem nimic la limita
	overlap = time.Hour
)

// ErrSyncRunning: o sincronizare ruleaza deja.
var ErrSyncRunning = errors.New("catalog sync already running")

// ErrBadCursor: cursorul de cautare nu e un numar >= 0.
var ErrBadCursor = errors.New("invalid cursor")

// CatalogStatus e starea catalogului, afisata in UI.
type CatalogStatus struct {
	State     string     `json:"state"`     // "building" | "ready"
	Count     int        `json:"count"`     // cate servere sunt in catalog
	UpdatedAt *time.Time `json:"updatedAt"` // last_sync_at; nil cat timp e "building"
	Syncing   bool       `json:"syncing"`
	LastError *string    `json:"lastError"`
}

// Catalog e catalogul local; sigur pentru folosire din mai multe goroutine.
type Catalog struct {
	db      *sql.DB
	baseURL string

	// campuri schimbate in teste
	pause       time.Duration   // pauza intre pagini
	retryDelays []time.Duration // asteptarea inainte de fiecare reincercare (len = numarul de reincercari)
	http        *http.Client
	now         func() time.Time

	syncMu  sync.Mutex // o singura sincronizare la un moment dat (TryLock)
	syncing atomic.Bool
}

// OpenCatalog deschide (sau creeaza) catalogul din path. Daca fisierul are alta versiune de
// schema, il sterge si il reface gol (e doar o copie a registrului, se descarca din nou).
func OpenCatalog(path, baseURL string) (*Catalog, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	db, v, err := openCatalogDB(path)
	if err != nil {
		// fisier stricat (nu e SQLite): il stergem o data si o luam de la zero
		if err := removeCatalogFiles(path); err != nil {
			return nil, err
		}
		if db, v, err = openCatalogDB(path); err != nil {
			return nil, err
		}
	}
	if v != catalogVersion {
		// fisier nou (versiunea 0) sau schema necunoscuta: o luam de la zero
		db.Close()
		if err := removeCatalogFiles(path); err != nil {
			return nil, err
		}
		if db, _, err = openCatalogDB(path); err != nil {
			return nil, err
		}
		if _, err := db.Exec(catalogSchema + fmt.Sprintf("PRAGMA user_version = %d;", catalogVersion)); err != nil {
			db.Close()
			return nil, fmt.Errorf("create catalog schema: %w", err)
		}
	}
	return &Catalog{
		db:          db,
		baseURL:     baseURL,
		pause:       250 * time.Millisecond,
		retryDelays: []time.Duration{2 * time.Second, 10 * time.Second, 30 * time.Second},
		http:        &http.Client{Timeout: 60 * time.Second},
		now:         time.Now,
	}, nil
}

// removeCatalogFiles sterge fisierul catalogului si fisierele WAL ale lui.
func removeCatalogFiles(path string) error {
	for _, p := range []string{path, path + "-wal", path + "-shm"} {
		if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

// openCatalogDB: acelasi DSN ca in store (WAL, busy_timeout), o singura conexiune.
// Intoarce si user_version; orice eroare (de ex. fisierul nu e SQLite) inchide baza.
func openCatalogDB(path string) (*sql.DB, int, error) {
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, 0, err
	}
	db.SetMaxOpenConns(1)
	var v int
	if err := db.QueryRow(`PRAGMA user_version`).Scan(&v); err != nil {
		db.Close()
		return nil, 0, err
	}
	return db, v, nil
}

// Close inchide fisierul.
func (c *Catalog) Close() error {
	return c.db.Close()
}

// --- meta ---

// execer e ce au in comun *sql.DB si *sql.Tx pentru scrieri.
type execer interface {
	Exec(query string, args ...any) (sql.Result, error)
}

func (c *Catalog) getMeta(key string) (string, bool, error) {
	var v string
	err := c.db.QueryRow(`SELECT value FROM meta WHERE key = ?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return v, true, nil
}

func setMeta(ex execer, key, value string) error {
	_, err := ex.Exec(`INSERT INTO meta (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value)
	return err
}

func deleteMeta(ex execer, keys ...string) error {
	for _, k := range keys {
		if _, err := ex.Exec(`DELETE FROM meta WHERE key = ?`, k); err != nil {
			return err
		}
	}
	return nil
}

// Ready: prima sincronizare s-a terminat (full_done = 1).
func (c *Catalog) Ready() bool {
	v, _, err := c.getMeta("full_done")
	return err == nil && v == "1"
}

// lastSync intoarce last_sync_at; ok=false daca lipseste sau nu se poate citi.
func (c *Catalog) lastSync() (time.Time, bool) {
	v, ok, err := c.getMeta("last_sync_at")
	if err != nil || !ok {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339, v)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

// Due: trebuie sincronizat (prima sincronizare neterminata sau ultima mai veche de 24 h).
func (c *Catalog) Due(now time.Time) bool {
	if !c.Ready() {
		return true
	}
	last, ok := c.lastSync()
	return !ok || now.Sub(last) > syncEvery
}

// Status citeste starea din fisier; erorile de citire dau valori goale (e doar pentru afisare).
func (c *Catalog) Status() CatalogStatus {
	st := CatalogStatus{State: "building", Syncing: c.syncing.Load()}
	c.db.QueryRow(`SELECT count(*) FROM servers`).Scan(&st.Count)
	if c.Ready() {
		st.State = "ready"
		if t, ok := c.lastSync(); ok {
			st.UpdatedAt = &t
		}
	}
	if v, ok, err := c.getMeta("last_error"); err == nil && ok {
		st.LastError = &v
	}
	return st
}

// --- sincronizarea ---

// Sync face prima sincronizare (sau o continua din full_cursor) daca nu e gata, altfel actualizarea.
// La eroare salveaza last_error si o intoarce; datele deja scrise raman.
func (c *Catalog) Sync(ctx context.Context) error {
	if !c.syncMu.TryLock() {
		return ErrSyncRunning
	}
	defer c.syncMu.Unlock()
	c.syncing.Store(true)
	defer c.syncing.Store(false)

	// secunde intregi, ca valoarea salvata (RFC3339) sa fie exact inceputul sincronizarii
	start := c.now().UTC().Truncate(time.Second)
	var err error
	if c.Ready() {
		err = c.update(ctx, start)
	} else {
		err = c.fullSync(ctx, start)
	}
	if err != nil {
		// la oprirea aplicatiei (context anulat) nu notam o eroare: nu e o problema a registrului
		if ctx.Err() == nil {
			c.saveError(err)
		}
		return err
	}
	return nil
}

func (c *Catalog) saveError(err error) {
	tx, txErr := c.db.Begin()
	if txErr != nil {
		return
	}
	defer tx.Rollback()
	if setMeta(tx, "last_error", err.Error()) != nil ||
		setMeta(tx, "last_error_at", c.now().UTC().Format(time.RFC3339)) != nil {
		return
	}
	tx.Commit()
}

// fullSync: version=latest, pagina cu pagina din full_cursor. Fiecare pagina se scrie intr-o
// tranzactie impreuna cu cursorul urmator, deci o eroare / un restart continua de unde a ramas.
// full_started (inceputul primei incercari) se salveaza o data si devine last_sync_at la final,
// ca actualizarea sa nu piarda ce s-a schimbat intre incercari.
func (c *Catalog) fullSync(ctx context.Context, start time.Time) error {
	if _, ok, err := c.getMeta("full_started"); err != nil {
		return err
	} else if !ok {
		if err := setMeta(c.db, "full_started", start.Format(time.RFC3339)); err != nil {
			return err
		}
	}
	cursor, _, err := c.getMeta("full_cursor")
	if err != nil {
		return err
	}
	restarted := false
	for first := true; ; first = false {
		if !first {
			if err := sleepCtx(ctx, c.pause); err != nil {
				return err
			}
		}
		q := url.Values{}
		q.Set("version", "latest")
		q.Set("limit", strconv.Itoa(syncPageLimit))
		if cursor != "" {
			q.Set("cursor", cursor)
		}
		entries, next, err := c.fetchPage(ctx, q)
		var se *statusError
		if err != nil && cursor != "" && !restarted && errors.As(err, &se) && isClientError(se.Code) {
			// cursorul salvat nu mai e valid (de ex. registrul s-a schimbat): reluam de la prima
			// pagina, o singura data; randurile deja scrise raman (upsert)
			restarted = true
			if err := deleteMeta(c.db, "full_cursor"); err != nil {
				return err
			}
			cursor = ""
			continue
		}
		if err != nil {
			return err
		}
		if next != "" && next == cursor {
			return fmt.Errorf("%w: registry returned the same cursor again", ErrUnavailable)
		}

		tx, err := c.db.Begin()
		if err != nil {
			return err
		}
		if err := writeFullPage(tx, entries, next); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
		if next == "" {
			return nil
		}
		cursor = next
	}
}

// isClientError: 4xx, in afara de 429 (prea multe cereri, se reincearca).
func isClientError(code int) bool {
	return code >= 400 && code < 500 && code != http.StatusTooManyRequests
}

// writeFullPage scrie o pagina din prima sincronizare si cursorul urmator (sau finalul).
func writeFullPage(tx *sql.Tx, entries []rawEntry, next string) error {
	for _, e := range entries {
		// deprecated se trateaza ca deleted in catalog (normalize il accepta pentru clientul live)
		if isGone(e.entry) {
			continue
		}
		if _, ok := normalize(e.entry); !ok {
			continue
		}
		if err := upsertEntry(tx, e.entry, e.raw); err != nil {
			return err
		}
	}
	if next != "" {
		return setMeta(tx, "full_cursor", next)
	}
	// ultima pagina: catalogul e gata; last_sync_at = inceputul primei incercari
	if _, err := tx.Exec(`INSERT INTO meta (key, value) SELECT 'last_sync_at', value FROM meta WHERE key = 'full_started'
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`); err != nil {
		return err
	}
	if err := setMeta(tx, "full_done", "1"); err != nil {
		return err
	}
	return deleteMeta(tx, "full_cursor", "full_started", "last_error", "last_error_at")
}

// update cere ce s-a schimbat de la ultima sincronizare (cu o ora suprapunere), toate paginile.
func (c *Catalog) update(ctx context.Context, start time.Time) error {
	last, ok := c.lastSync()
	if !ok {
		return errors.New("catalog has no valid last_sync_at")
	}
	since := last.Add(-overlap).UTC().Format(time.RFC3339)
	cursor := ""
	for first := true; ; first = false {
		if !first {
			if err := sleepCtx(ctx, c.pause); err != nil {
				return err
			}
		}
		q := url.Values{}
		q.Set("updated_since", since)
		q.Set("limit", strconv.Itoa(syncPageLimit))
		if cursor != "" {
			q.Set("cursor", cursor)
		}
		entries, next, err := c.fetchPage(ctx, q)
		if err != nil {
			return err
		}
		if next != "" && next == cursor {
			return fmt.Errorf("%w: registry returned the same cursor again", ErrUnavailable)
		}

		tx, err := c.db.Begin()
		if err != nil {
			return err
		}
		for _, e := range entries {
			if err := applyUpdate(tx, e); err != nil {
				tx.Rollback()
				return err
			}
		}
		if next == "" {
			err = setMeta(tx, "last_sync_at", start.Format(time.RFC3339))
			if err == nil {
				err = deleteMeta(tx, "last_error", "last_error_at")
			}
			if err != nil {
				tx.Rollback()
				return err
			}
		}
		if err := tx.Commit(); err != nil {
			return err
		}
		if next == "" {
			return nil
		}
		cursor = next
	}
}

// applyUpdate aplica o intrare din actualizare:
//   - deleted / deprecated pe versiunea din catalog -> randul se sterge;
//   - active + latest + normalize o accepta -> upsert (versiunea noua inlocuieste randul);
//   - active + latest dar normalize o refuza -> randul se sterge dupa nume (orice versiune),
//     pentru ca versiunea din catalog nu mai e ultima;
//   - altfel (isLatest=false, o versiune veche) -> nimic.
//
// Fara _meta oficial intrarea conteaza ca active + latest; isLatest lipsa = latest (ca in normalize).
func applyUpdate(tx *sql.Tx, e rawEntry) error {
	status, latest := "active", true
	if o := e.entry.Meta.Official; o != nil {
		status = o.Status
		latest = o.IsLatest == nil || *o.IsLatest
	}
	switch {
	case isGone(e.entry):
		return deleteVersion(tx, e.entry.Server.Name, e.entry.Server.Version)
	case status == "active" && latest:
		if _, ok := normalize(e.entry); ok {
			return upsertEntry(tx, e.entry, e.raw)
		}
		_, err := tx.Exec(`DELETE FROM servers WHERE name = ?`, e.entry.Server.Name)
		return err
	}
	return nil
}

// isGone: intrarea e deleted sau deprecated (pentru catalog, la fel).
func isGone(e apiEntry) bool {
	o := e.Meta.Official
	return o != nil && (o.Status == "deleted" || o.Status == "deprecated")
}

func deleteVersion(ex execer, name, version string) error {
	_, err := ex.Exec(`DELETE FROM servers WHERE name = ? AND version = ?`, name, version)
	return err
}

// upsertEntry scrie (sau inlocuieste) randul serverului; raw e JSON-ul intrarii (server + _meta).
func upsertEntry(ex execer, e apiEntry, raw []byte) error {
	var compact bytes.Buffer
	if err := json.Compact(&compact, raw); err != nil {
		return err
	}
	s := e.Server
	search := strings.ToLower(s.Name + " " + s.Title + " " + s.Description)
	updated := ""
	if e.Meta.Official != nil {
		updated = e.Meta.Official.UpdatedAt
	}
	_, err := ex.Exec(`INSERT INTO servers (name, version, title, description, search, entry, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(name) DO UPDATE SET version = excluded.version, title = excluded.title,
		  description = excluded.description, search = excluded.search, entry = excluded.entry,
		  updated_at = excluded.updated_at`,
		s.Name, s.Version, s.Title, s.Description, search, compact.String(), updated)
	return err
}

// rawEntry e o intrare din registru: decodata (pentru reguli) si bruta (pentru coloana entry).
type rawEntry struct {
	entry apiEntry
	raw   []byte
}

// fetchPage cere o pagina, cu reincercari (retryDelays). Intrarile care nu se pot decoda sunt sarite.
func (c *Catalog) fetchPage(ctx context.Context, q url.Values) ([]rawEntry, string, error) {
	for attempt := 0; ; attempt++ {
		entries, next, err := c.fetchPageOnce(ctx, q)
		if err == nil {
			return entries, next, nil
		}
		var se *statusError
		if errors.As(err, &se) && isClientError(se.Code) {
			return nil, "", err // un 4xx nu se rezolva reincercand
		}
		if attempt >= len(c.retryDelays) || ctx.Err() != nil {
			return nil, "", err
		}
		if err := sleepCtx(ctx, c.retryDelays[attempt]); err != nil {
			return nil, "", err
		}
	}
}

func (c *Catalog) fetchPageOnce(ctx context.Context, q url.Values) ([]rawEntry, string, error) {
	body, err := getPage(ctx, c.http, c.baseURL, q)
	if err != nil {
		return nil, "", err
	}
	var page struct {
		Servers  []json.RawMessage `json:"servers"`
		Metadata struct {
			NextCursor string `json:"nextCursor"`
		} `json:"metadata"`
	}
	if err := json.Unmarshal(body, &page); err != nil {
		return nil, "", fmt.Errorf("%w: invalid response: %v", ErrUnavailable, err)
	}
	out := make([]rawEntry, 0, len(page.Servers))
	for _, raw := range page.Servers {
		var e apiEntry
		if json.Unmarshal(raw, &e) != nil || e.Server.Name == "" {
			continue
		}
		out = append(out, rawEntry{entry: e, raw: raw})
	}
	return out, page.Metadata.NextCursor, nil
}

// sleepCtx asteapta d sau pana se anuleaza contextul.
func sleepCtx(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// --- cautarea ---

// Search cauta in catalog: fiecare cuvant (max 8) trebuie sa apara in nume / titlu / descriere.
// Ordinea: numele (segmentul dupa ultimul "/") egal cu textul, apoi numele care contin primul
// cuvant, apoi restul; in fiecare grup cele mai noi primele. Cursorul e offset-ul ("" = 0).
//
// Limitare: se citesc 31 de randuri (al 31-lea arata ca exista pagina urmatoare), iar randurile pe
// care normalize le refuza acum sunt sarite, deci o pagina poate avea mai putin de 30 de rezultate.
// Nu se pierde nimic: pagina urmatoare incepe mereu la offset+30.
func (c *Catalog) Search(query, cursor string) ([]Server, string, error) {
	offset := 0
	if cursor != "" {
		n, err := strconv.Atoi(cursor)
		if err != nil || n < 0 {
			return nil, "", ErrBadCursor
		}
		offset = n
	}
	words := strings.Fields(strings.ToLower(query))
	if len(words) > maxSearchWords {
		words = words[:maxSearchWords]
	}

	// instr in loc de LIKE: % si _ nu sunt speciale; valorile merg doar ca parametri
	var where []string
	var args []any
	for _, w := range words {
		where = append(where, "instr(search, ?) > 0")
		args = append(args, w)
	}
	sqlText := `SELECT entry FROM servers`
	if len(where) > 0 {
		sqlText += " WHERE " + strings.Join(where, " AND ")
	}
	order := "updated_at DESC, name"
	if len(words) > 0 {
		whole := strings.Join(words, " ")
		// segmentul de dupa ultimul "/" nu are "/", deci un text cu "/" nu poate fi egal cu el.
		// Limitare: lower() din SQLite schimba doar literele ASCII, iar textul e trecut prin
		// strings.ToLower; pentru nume cu litere non-ASCII potrivirea exacta poate lipsi.
		exact := "0"
		if !strings.Contains(whole, "/") {
			suffix := "/" + whole
			exact = "(lower(name) = ? OR substr(lower(name), -?) = ?)"
			args = append(args, whole, utf8.RuneCountInString(suffix), suffix)
		}
		order = exact + " DESC, (instr(lower(name), ?) > 0) DESC, " + order
		args = append(args, words[0])
	}
	sqlText += " ORDER BY " + order + " LIMIT ? OFFSET ?"
	args = append(args, searchPageSize+1, offset)

	rows, err := c.db.Query(sqlText, args...)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()
	out := []Server{}
	n := 0
	for rows.Next() {
		n++
		var entry string
		if err := rows.Scan(&entry); err != nil {
			return nil, "", err
		}
		if n > searchPageSize {
			continue // randul 31 doar arata ca mai exista o pagina
		}
		var e apiEntry
		if json.Unmarshal([]byte(entry), &e) != nil {
			continue
		}
		if s, ok := normalize(e); ok {
			out = append(out, s)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, "", err
	}
	next := ""
	if n > searchPageSize {
		next = strconv.Itoa(offset + searchPageSize)
	}
	return out, next, nil
}
