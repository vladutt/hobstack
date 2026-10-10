package store

import (
	"database/sql"
	"time"
)

// Tipurile de surse pentru un element din Library.
const (
	SourceGitHub     = "github"
	SourceClaudeCode = "claude-code"
	SourceClaudeAI   = "claude-ai"
	SourcePlugin     = "plugin"
)

// LibrarySource: de unde a fost instalat un element. Ref = commit-ul instalat (github) sau amprenta
// continutului (local); Latest = ultima referinta vazuta la sursa.
type LibrarySource struct {
	ItemID    int64      `json:"itemId"`
	Kind      string     `json:"kind"`
	Repo      string     `json:"repo"`
	Path      string     `json:"path"`
	Ref       string     `json:"ref"`
	Label     string     `json:"label"`
	Latest    string     `json:"latest"`
	CheckedAt *time.Time `json:"checkedAt"`
}

const sourceCols = `item_id, kind, repo, path, ref, label, latest, checked_at`

func scanSource(sc interface{ Scan(...any) error }) (LibrarySource, error) {
	var src LibrarySource
	var checked sql.NullString
	err := sc.Scan(&src.ItemID, &src.Kind, &src.Repo, &src.Path, &src.Ref, &src.Label, &src.Latest, &checked)
	if err == sql.ErrNoRows {
		return src, ErrNotFound
	}
	if err != nil {
		return src, err
	}
	if checked.Valid {
		t := parseTime(checked.String)
		src.CheckedAt = &t
	}
	return src, nil
}

// SetLibrarySource salveaza sursa elementului (inlocuieste una existenta; checked_at se pastreaza).
func (s *Store) SetLibrarySource(src LibrarySource) error {
	_, err := s.db.Exec(`INSERT INTO library_sources (item_id, kind, repo, path, ref, label, latest)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(item_id) DO UPDATE SET kind = excluded.kind, repo = excluded.repo, path = excluded.path,
		  ref = excluded.ref, label = excluded.label, latest = excluded.latest`,
		src.ItemID, src.Kind, src.Repo, src.Path, src.Ref, src.Label, src.Latest)
	return err
}

// GetLibrarySource intoarce sursa unui element (ErrNotFound daca nu are).
func (s *Store) GetLibrarySource(itemID int64) (LibrarySource, error) {
	return scanSource(s.db.QueryRow(`SELECT `+sourceCols+` FROM library_sources WHERE item_id = ?`, itemID))
}

// ListLibrarySources intoarce toate sursele, dupa id-ul elementului.
func (s *Store) ListLibrarySources() ([]LibrarySource, error) {
	rows, err := s.db.Query(`SELECT ` + sourceCols + ` FROM library_sources ORDER BY item_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []LibrarySource{}
	for rows.Next() {
		src, err := scanSource(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, src)
	}
	return out, rows.Err()
}

// MarkSourceChecked retine ultima referinta vazuta la sursa si momentul verificarii.
func (s *Store) MarkSourceChecked(itemID int64, latest string, at time.Time) error {
	return mustAffect(s.db.Exec(`UPDATE library_sources SET latest = ?, checked_at = ? WHERE item_id = ?`,
		latest, at.UTC().Format(time.RFC3339Nano), itemID))
}

// FindLibrarySource cauta o sursa dupa (kind, repo, path), pentru "In library". Kind e tipul sursei.
func (s *Store) FindLibrarySource(kind, repo, path string) (LibrarySource, error) {
	return scanSource(s.db.QueryRow(`SELECT `+sourceCols+` FROM library_sources
		WHERE kind = ? AND repo = ? AND path = ? ORDER BY item_id LIMIT 1`, kind, repo, path))
}
