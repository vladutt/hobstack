package store

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	KindProjectFile = "project" // fisier al proiectului, vazut de toti agentii
	KindMessageFile = "message" // fisier atasat unui mesaj
)

// Attachment e un fisier urcat de user. MessageID e 0 cand nu e legat de un mesaj.
type Attachment struct {
	ID        int64     `json:"id"`
	ProjectID int64     `json:"projectId"`
	MessageID int64     `json:"messageId"`
	Kind      string    `json:"kind"`
	Name      string    `json:"name"`   // numele curatat, cum l-a dat userul
	Stored    string    `json:"stored"` // numele de pe disc (prefix aleator + Name)
	Mime      string    `json:"mime"`
	Size      int64     `json:"size"`
	CreatedAt time.Time `json:"createdAt"`
}

const attachmentCols = `id, project_id, message_id, kind, name, stored, mime, size, created_at`

// CleanFileName pastreaza doar numele fisierului (fara foldere) si inlocuieste
// caracterele periculoase cu "_". Merge si pe cai de Windows. Numele e taiat la
// cel mult 200 de bytes (limita pe disc e 255, iar prefixul aleator adauga 17),
// dar extensia scurta se pastreaza intotdeauna.
func CleanFileName(name string) string {
	name = filepath.Base(strings.ReplaceAll(name, `\`, "/"))
	name = strings.Map(func(r rune) rune {
		if r < 32 || r == 127 || strings.ContainsRune(`/\:*?"<>|`, r) {
			return '_'
		}
		return r
	}, name)
	if name == "" || name == "." || name == ".." {
		return "fisier"
	}
	const maxBytes = 200
	stem, ext := name, filepath.Ext(name)
	if len(ext) <= 16 {
		stem = strings.TrimSuffix(name, ext)
	} else {
		ext = "" // extensie prea lunga: face parte din stem
	}
	// taiem stem-ul in bytes, dar pe granita de runa, ca UTF-8 sa ramana valid
	for len(stem)+len(ext) > maxBytes {
		_, size := utf8.DecodeLastRuneInString(stem)
		stem = stem[:len(stem)-size]
	}
	if stem == "" {
		stem = "fisier"
	}
	return stem + ext
}

// StoredName intoarce numele de pe disc: 16 caractere hex aleatoare + "-" + nume.
// Prefixul evita coliziunile intre fisiere cu acelasi nume.
func StoredName(clean string) string {
	b := make([]byte, 8)
	rand.Read(b) // crypto/rand nu esueaza in practica
	return hex.EncodeToString(b) + "-" + clean
}

func scanAttachment(sc scanner) (Attachment, error) {
	var a Attachment
	var msg sql.NullInt64
	var created string
	if err := sc.Scan(&a.ID, &a.ProjectID, &msg, &a.Kind, &a.Name, &a.Stored, &a.Mime, &a.Size, &created); err != nil {
		return Attachment{}, err
	}
	a.MessageID = msg.Int64
	a.CreatedAt = parseTime(created)
	return a, nil
}

func (s *Store) AddAttachment(a *Attachment) error {
	ts := now()
	var msg any
	if a.MessageID != 0 {
		msg = a.MessageID
	}
	res, err := s.db.Exec(`INSERT INTO attachments (project_id, message_id, kind, name, stored, size, mime, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, a.ProjectID, msg, a.Kind, a.Name, a.Stored, a.Size, a.Mime, ts)
	if err != nil {
		return err
	}
	if a.ID, err = res.LastInsertId(); err != nil {
		return err
	}
	a.CreatedAt = parseTime(ts)
	return nil
}

func (s *Store) GetAttachment(id int64) (Attachment, error) {
	a, err := scanAttachment(s.db.QueryRow(`SELECT `+attachmentCols+` FROM attachments WHERE id = ?`, id))
	if err == sql.ErrNoRows {
		return Attachment{}, ErrNotFound
	}
	return a, err
}

// ListProjectFiles intoarce fisierele de proiect, in ordinea urcarii.
func (s *Store) ListProjectFiles(projectID int64) ([]Attachment, error) {
	rows, err := s.db.Query(`SELECT `+attachmentCols+` FROM attachments
		WHERE project_id = ? AND kind = ? ORDER BY id`, projectID, KindProjectFile)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Attachment{}
	for rows.Next() {
		a, err := scanAttachment(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Store) DeleteAttachment(id int64) error {
	return mustAffect(s.db.Exec(`DELETE FROM attachments WHERE id = ?`, id))
}

// LinkAttachments leaga de mesaj atasamentele de tip "message" care nu sunt
// legate inca. Fisierele de proiect si cele deja legate raman neatinse.
func (s *Store) LinkAttachments(ids []int64, messageID int64) error {
	if len(ids) == 0 {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, id := range ids {
		if _, err := tx.Exec(`UPDATE attachments SET message_id = ?
			WHERE id = ? AND kind = ? AND message_id IS NULL`, messageID, id, KindMessageFile); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// attachmentsFor intoarce atasamentele mesajelor date, grupate pe message_id,
// printr-o singura interogare.
func (s *Store) attachmentsFor(messageIDs []int64) (map[int64][]Attachment, error) {
	out := map[int64][]Attachment{}
	if len(messageIDs) == 0 {
		return out, nil
	}
	args := make([]any, len(messageIDs))
	for i, id := range messageIDs {
		args[i] = id
	}
	rows, err := s.db.Query(`SELECT `+attachmentCols+` FROM attachments WHERE message_id IN (?`+
		strings.Repeat(",?", len(messageIDs)-1)+`) ORDER BY id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		a, err := scanAttachment(rows)
		if err != nil {
			return nil, err
		}
		out[a.MessageID] = append(out[a.MessageID], a)
	}
	return out, rows.Err()
}
