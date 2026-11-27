package store

import (
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// Memoria proiectului: fapte si decizii scurte cu cheie, scrise de agenti (tool-urile MCP din team) sau de utilizator
// (API). Indexul lor intra in fiecare tura; textul intreg doar la cerere. Spec:
// docs/superpowers/specs/2026-11-27-hobstack-project-memory-design.md

// Memory e o intrare din memoria proiectului.
type Memory struct {
	ID            int64      `json:"id"`
	ProjectID     int64      `json:"projectId"`
	Key           string     `json:"key"`
	Summary       string     `json:"summary"`
	Body          string     `json:"body"`
	Locked        bool       `json:"locked"`
	AuthorAgentID int64      `json:"authorAgentId"` // 0 = utilizatorul
	AuthorName    string     `json:"authorName"`
	TaskID        int64      `json:"taskId"`  // 0 = fara task
	TaskRef       string     `json:"taskRef"` // "T-212", gol fara task
	CreatedAt     time.Time  `json:"createdAt"`
	UpdatedAt     time.Time  `json:"updatedAt"`
	DeletedAt     *time.Time `json:"deletedAt"` // nil = activa
}

// MemoryVersion e un pas din istoricul unei intrari.
type MemoryVersion struct {
	ID            int64     `json:"id"`
	MemoryID      int64     `json:"memoryId"`
	Action        string    `json:"action"` // set, forget, restore, lock, unlock
	Summary       string    `json:"summary"`
	Body          string    `json:"body"`
	AuthorAgentID int64     `json:"authorAgentId"`
	AuthorName    string    `json:"authorName"`
	TaskID        int64     `json:"taskId"`
	Reason        string    `json:"reason"`
	At            time.Time `json:"at"`
}

// MemoryWrite e o scriere: continutul si cine o face (AuthorAgentID 0 = utilizatorul).
type MemoryWrite struct {
	Key, Summary, Body string
	AuthorAgentID      int64
	AuthorName         string
	TaskID             int64
}

// Limitele din spec.
const (
	MaxMemoryEntries = 200
	MaxMemorySummary = 140
	MaxMemoryBody    = 4000
)

var (
	ErrMemoryLocked = errors.New("memory entry is locked by the user")
	ErrMemoryFull   = fmt.Errorf("the project memory is full (%d entries)", MaxMemoryEntries)
	ErrMemoryExists = errors.New("a memory entry with this key already exists")
)

// MemoryInputError: cheie, rezumat sau text invalid (400 in API).
type MemoryInputError struct{ Msg string }

func (e MemoryInputError) Error() string { return e.Msg }

var memoryKeyRe = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,59}$`)

// NormalizeMemoryKey: lowercase, spatiile devin "-", apoi validarea formatului.
func NormalizeMemoryKey(s string) (string, error) {
	k := strings.Join(strings.Fields(strings.ToLower(s)), "-")
	if !memoryKeyRe.MatchString(k) {
		return "", MemoryInputError{fmt.Sprintf("invalid key %q: use 1-60 characters a-z, 0-9, '.', '_' or '-', starting with a letter or digit (for example ui.dates)", s)}
	}
	return k, nil
}

// checkMemoryText valideaza rezumatul (obligatoriu, un rand) si textul.
func checkMemoryText(w *MemoryWrite) error {
	w.Summary = strings.Join(strings.Fields(w.Summary), " ")
	w.Body = strings.TrimSpace(w.Body)
	if w.Summary == "" || len([]rune(w.Summary)) > MaxMemorySummary {
		return MemoryInputError{fmt.Sprintf("summary: 1-%d characters on one line", MaxMemorySummary)}
	}
	if len([]rune(w.Body)) > MaxMemoryBody {
		return MemoryInputError{fmt.Sprintf("text: at most %d characters", MaxMemoryBody)}
	}
	return nil
}

const memoryCols = `m.id, m.project_id, m.key, m.summary, m.body, m.locked, COALESCE(m.author_agent_id, 0), m.author_name,
	COALESCE(m.task_id, 0), COALESCE(t.number, 0), m.created_at, m.updated_at, m.deleted_at`

const memoryFrom = ` FROM project_memory m LEFT JOIN tasks t ON t.id = m.task_id`

func scanMemory(sc scanner) (Memory, error) {
	var m Memory
	var num int64
	var created, updated string
	var deleted sql.NullString
	if err := sc.Scan(&m.ID, &m.ProjectID, &m.Key, &m.Summary, &m.Body, &m.Locked, &m.AuthorAgentID, &m.AuthorName,
		&m.TaskID, &num, &created, &updated, &deleted); err != nil {
		return m, err
	}
	if num > 0 {
		m.TaskRef = fmt.Sprintf("T-%d", num)
	}
	m.CreatedAt, m.UpdatedAt = parseTime(created), parseTime(updated)
	if deleted.Valid {
		d := parseTime(deleted.String)
		m.DeletedAt = &d
	}
	return m, nil
}

type queryRower interface {
	QueryRow(query string, args ...any) *sql.Row
}

func getMemory(q queryRower, where string, args ...any) (Memory, error) {
	m, err := scanMemory(q.QueryRow(`SELECT `+memoryCols+memoryFrom+` WHERE `+where, args...))
	if errors.Is(err, sql.ErrNoRows) {
		return m, ErrNotFound
	}
	return m, err
}

// GetMemory intoarce intrarea (si una stearsa).
func (s *Store) GetMemory(id int64) (Memory, error) { return getMemory(s.db, `m.id = ?`, id) }

// GetMemoryByKey intoarce intrarea proiectului cu cheia data (si una stearsa).
func (s *Store) GetMemoryByKey(projectID int64, key string) (Memory, error) {
	return getMemory(s.db, `m.project_id = ? AND m.key = ?`, projectID, key)
}

func addMemoryVersion(tx *sql.Tx, memoryID int64, action string, w MemoryWrite, reason, at string) error {
	_, err := tx.Exec(`INSERT INTO project_memory_versions (memory_id, action, summary, body, author_agent_id, author_name, task_id, reason, at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, memoryID, action, w.Summary, w.Body, nullID(w.AuthorAgentID), w.AuthorName,
		nullID(w.TaskID), reason, at)
	return err
}

// SetMemory creeaza sau inlocuieste intrarea cu cheia w.Key. O cheie stearsa reinvie acelasi rand (neblocat).
// allowLocked = scrierea utilizatorului (poate schimba o intrare blocata). prev = intrarea de dinainte (nil la
// creare; stearsa la reinviere); changed = false pentru o scriere identica pe o intrare activa (fara versiune noua).
func (s *Store) SetMemory(projectID int64, w MemoryWrite, allowLocked bool) (m Memory, prev *Memory, changed bool, err error) {
	return s.setMemory(projectID, w, setOpts{allowLocked: allowLocked})
}

// setOpts: createOnly = ErrMemoryExists pe o cheie activa ("+ Add"); lock = blocheaza in aceeasi tranzactie.
type setOpts struct{ allowLocked, createOnly, lock bool }

func (s *Store) setMemory(projectID int64, w MemoryWrite, o setOpts) (m Memory, prev *Memory, changed bool, err error) {
	allowLocked := o.allowLocked
	if w.Key, err = NormalizeMemoryKey(w.Key); err != nil {
		return
	}
	if err = checkMemoryText(&w); err != nil {
		return
	}
	tx, err := s.db.Begin()
	if err != nil {
		return
	}
	defer tx.Rollback()
	old, gerr := getMemory(tx, `m.project_id = ? AND m.key = ?`, projectID, w.Key)
	if gerr != nil && !errors.Is(gerr, ErrNotFound) {
		return m, nil, false, gerr
	}
	exists := gerr == nil
	active := exists && old.DeletedAt == nil
	if active && o.createOnly {
		return m, nil, false, ErrMemoryExists
	}
	if active && old.Locked && !allowLocked {
		return m, nil, false, ErrMemoryLocked
	}
	if active && old.Summary == w.Summary && old.Body == w.Body {
		return old, &old, false, nil
	}
	if !active {
		var n int
		if err = tx.QueryRow(`SELECT COUNT(*) FROM project_memory WHERE project_id = ? AND deleted_at IS NULL`, projectID).Scan(&n); err != nil {
			return
		}
		if n >= MaxMemoryEntries {
			return m, nil, false, ErrMemoryFull
		}
	}
	ts := now()
	var id int64
	if exists {
		id = old.ID
		prev = &old
		_, err = tx.Exec(`UPDATE project_memory SET summary = ?, body = ?, author_agent_id = ?, author_name = ?, task_id = ?,
			updated_at = ?, deleted_at = NULL, locked = CASE WHEN deleted_at IS NULL THEN locked ELSE 0 END WHERE id = ?`,
			w.Summary, w.Body, nullID(w.AuthorAgentID), w.AuthorName, nullID(w.TaskID), ts, id)
	} else {
		var res sql.Result
		res, err = tx.Exec(`INSERT INTO project_memory (project_id, key, summary, body, author_agent_id, author_name, task_id, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, projectID, w.Key, w.Summary, w.Body, nullID(w.AuthorAgentID), w.AuthorName,
			nullID(w.TaskID), ts, ts)
		if err == nil {
			id, err = res.LastInsertId()
		}
	}
	if err != nil {
		return
	}
	if err = addMemoryVersion(tx, id, "set", w, "", ts); err != nil {
		return
	}
	if o.lock {
		if _, err = tx.Exec(`UPDATE project_memory SET locked = 1 WHERE id = ?`, id); err != nil {
			return
		}
		lockBy := MemoryWrite{Summary: w.Summary, Body: w.Body, AuthorName: UserFileAuthor}
		if err = addMemoryVersion(tx, id, "lock", lockBy, "", ts); err != nil {
			return
		}
	}
	if m, err = getMemory(tx, `m.id = ?`, id); err != nil {
		return
	}
	return m, prev, true, tx.Commit()
}

// CreateMemory e "+ Add" al utilizatorului: ErrMemoryExists pe o cheie activa, reinviere pe una stearsa.
// Verificarea si blocarea sunt in aceeasi tranzactie cu scrierea.
func (s *Store) CreateMemory(projectID int64, w MemoryWrite, locked bool) (Memory, error) {
	m, _, _, err := s.setMemory(projectID, w, setOpts{allowLocked: true, createOnly: true, lock: locked})
	return m, err
}

// ListMemory: intrarile active (deleted = false) sau doar cele sterse; blocate primele, apoi cele schimbate recent.
func (s *Store) ListMemory(projectID int64, deleted bool) ([]Memory, error) {
	cond := `m.deleted_at IS NULL`
	order := `m.locked DESC, m.updated_at DESC, m.id DESC`
	if deleted {
		cond, order = `m.deleted_at IS NOT NULL`, `m.deleted_at DESC, m.id DESC`
	}
	rows, err := s.db.Query(`SELECT `+memoryCols+memoryFrom+` WHERE m.project_id = ? AND `+cond+` ORDER BY `+order, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Memory
	for rows.Next() {
		m, err := scanMemory(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// SearchMemory cauta in cheie, rezumat si text (fara diacritice si litere mari), doar in intrarile active.
// Fiecare cuvant din query trebuie sa apara undeva.
func (s *Store) SearchMemory(projectID int64, query string, limit int) ([]Memory, error) {
	words := strings.Fields(FoldText(query))
	all, err := s.ListMemory(projectID, false)
	if err != nil {
		return nil, err
	}
	var out []Memory
	for _, m := range all {
		hay := FoldText(m.Key + " " + m.Summary + " " + m.Body)
		ok := true
		for _, w := range words {
			if !strings.Contains(hay, w) {
				ok = false
				break
			}
		}
		if ok {
			out = append(out, m)
			if limit > 0 && len(out) == limit {
				break
			}
		}
	}
	return out, nil
}

// ForgetMemory sterge intrarea (pastrata in istoric).
func (s *Store) ForgetMemory(id int64, by MemoryWrite, reason string, allowLocked bool) error {
	return s.memoryChange(id, func(tx *sql.Tx, m Memory, ts string) error {
		if m.DeletedAt != nil {
			return nil
		}
		if m.Locked && !allowLocked {
			return ErrMemoryLocked
		}
		// blocarea ramane pe rand: Restore (Undo) o pastreaza; o scriere noua pe cheie (SetMemory) o scoate
		if _, err := tx.Exec(`UPDATE project_memory SET deleted_at = ? WHERE id = ?`, ts, id); err != nil {
			return err
		}
		by.Summary, by.Body = m.Summary, m.Body
		return addMemoryVersion(tx, id, "forget", by, strings.TrimSpace(reason), ts)
	})
}

// RestoreMemory readuce o intrare stearsa (Undo / Restore din UI). ErrMemoryFull la limita.
func (s *Store) RestoreMemory(id int64, by MemoryWrite) error {
	return s.memoryChange(id, func(tx *sql.Tx, m Memory, ts string) error {
		if m.DeletedAt == nil {
			return nil
		}
		var n int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM project_memory WHERE project_id = ? AND deleted_at IS NULL`, m.ProjectID).Scan(&n); err != nil {
			return err
		}
		if n >= MaxMemoryEntries {
			return ErrMemoryFull
		}
		if _, err := tx.Exec(`UPDATE project_memory SET deleted_at = NULL, updated_at = ? WHERE id = ?`, ts, id); err != nil {
			return err
		}
		by.Summary, by.Body = m.Summary, m.Body
		return addMemoryVersion(tx, id, "restore", by, "", ts)
	})
}

// LockMemory blocheaza / deblocheaza intrarea (doar utilizatorul).
func (s *Store) LockMemory(id int64, locked bool) error {
	return s.memoryChange(id, func(tx *sql.Tx, m Memory, ts string) error {
		if m.Locked == locked {
			return nil
		}
		if _, err := tx.Exec(`UPDATE project_memory SET locked = ? WHERE id = ?`, locked, id); err != nil {
			return err
		}
		action := "unlock"
		if locked {
			action = "lock"
		}
		return addMemoryVersion(tx, id, action, MemoryWrite{Summary: m.Summary, Body: m.Body, AuthorName: UserFileAuthor}, "", ts)
	})
}

// memoryChange ruleaza fn intr-o tranzactie, cu intrarea citita in ea.
func (s *Store) memoryChange(id int64, fn func(tx *sql.Tx, m Memory, ts string) error) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	m, err := getMemory(tx, `m.id = ?`, id)
	if err != nil {
		return err
	}
	if err := fn(tx, m, now()); err != nil {
		return err
	}
	return tx.Commit()
}

// MemoryHistory: pasii intrarii, cei noi primele.
func (s *Store) MemoryHistory(id int64) ([]MemoryVersion, error) {
	rows, err := s.db.Query(`SELECT id, memory_id, action, summary, body, COALESCE(author_agent_id, 0), author_name,
		COALESCE(task_id, 0), reason, at FROM project_memory_versions WHERE memory_id = ? ORDER BY id DESC`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []MemoryVersion
	for rows.Next() {
		var v MemoryVersion
		var at string
		if err := rows.Scan(&v.ID, &v.MemoryID, &v.Action, &v.Summary, &v.Body, &v.AuthorAgentID, &v.AuthorName,
			&v.TaskID, &v.Reason, &at); err != nil {
			return nil, err
		}
		v.At = parseTime(at)
		out = append(out, v)
	}
	return out, rows.Err()
}

// foldMap: diacriticele uzuale (romana si latine) -> litera de baza.
var foldMap = strings.NewReplacer(
	"ă", "a", "â", "a", "á", "a", "à", "a", "ä", "a", "ã", "a", "å", "a",
	"î", "i", "í", "i", "ì", "i", "ï", "i",
	"ș", "s", "ş", "s", "ț", "t", "ţ", "t",
	"é", "e", "è", "e", "ê", "e", "ë", "e",
	"ó", "o", "ò", "o", "ô", "o", "ö", "o", "õ", "o",
	"ú", "u", "ù", "u", "û", "u", "ü", "u",
	"ñ", "n", "ç", "c",
)

// FoldText: lowercase si fara diacritice, pentru cautare.
func FoldText(s string) string { return foldMap.Replace(strings.ToLower(s)) }
