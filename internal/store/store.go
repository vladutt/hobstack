// Package store tine toata starea aplicatiei in SQLite: proiecte, agenti,
// task-uri, run-uri si mesaje.
package store

import (
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite" // driver SQLite in Go pur; se inregistreaza sub numele "sqlite"
)

var (
	ErrNotFound = errors.New("not found")
	ErrConflict = errors.New("conflict")
)

//go:embed migrations/*.sql
var migrations embed.FS

type Store struct {
	db *sql.DB
}

// Open deschide (sau creeaza) baza de date si aplica migrarile lipsa.
func Open(path string) (*Store, error) {
	dsn := "file:" + path + "?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	// O singura conexiune: SQLite are oricum un singur writer, iar asa nu
	// apar erori "database is locked" intre goroutine.
	// ATENTIE: in interiorul unei tranzactii folosim doar tx, niciodata s.db,
	// altfel asteptam dupa conexiunea pe care o tinem chiar noi (deadlock).
	db.SetMaxOpenConns(1)
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("database migration: %w", err)
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

// migrate aplica fisierele din migrations/ in ordinea numelui.
// Numarul ultimei migrari aplicate se tine in PRAGMA user_version.
func (s *Store) migrate() error {
	var version int
	if err := s.db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		return err
	}
	entries, err := migrations.ReadDir("migrations") // deja sortate dupa nume
	if err != nil {
		return err
	}
	for i, e := range entries {
		n := i + 1
		if n <= version {
			continue
		}
		body, err := migrations.ReadFile("migrations/" + e.Name())
		if err != nil {
			return err
		}
		tx, err := s.db.Begin()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(string(body)); err != nil {
			tx.Rollback()
			return fmt.Errorf("%s: %w", e.Name(), err)
		}
		if _, err := tx.Exec(fmt.Sprintf("PRAGMA user_version = %d", n)); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

// execer e implementat si de *sql.DB si de *sql.Tx.
type execer interface {
	Exec(query string, args ...any) (sql.Result, error)
}

// scanner e implementat si de *sql.Row si de *sql.Rows.
type scanner interface {
	Scan(dest ...any) error
}

// Timpii se salveaza ca text RFC3339 in UTC; asa se sorteaza corect si ca text.
func now() string { return time.Now().UTC().Format(time.RFC3339Nano) }

func parseTime(s string) time.Time {
	t, _ := time.Parse(time.RFC3339Nano, s)
	return t
}

func isUnique(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}

// mustAffect intoarce ErrNotFound daca un UPDATE/DELETE n-a atins niciun rand.
func mustAffect(res sql.Result, err error) error {
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// Backup scrie o copie consistenta a bazei de date in path (VACUUM INTO; path nu trebuie sa existe).
func (s *Store) Backup(path string) error {
	_, err := s.db.Exec("VACUUM INTO ?", path)
	return err
}
