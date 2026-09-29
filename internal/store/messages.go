package store

import (
	"database/sql"
	"time"
)

const (
	KindMsg    = "msg"   // mesaj scris de user sau de agent
	KindEvent  = "event" // rand informativ: "a creat T-4 pentru Rook"
	AuthorUser = "user"
)

// Message e un rand din thread-ul unui agent. Delivered=false inseamna ca
// agentul inca nu l-a primit; il primeste la urmatorul lui run.
type Message struct {
	ID        int64     `json:"id"`
	ProjectID int64     `json:"projectId"`
	AgentID   int64     `json:"agentId"`
	Kind      string    `json:"kind"`
	Author    string    `json:"author"`
	Text      string    `json:"text"`
	RunID     string    `json:"runId"`
	Delivered bool      `json:"delivered"`
	CreatedAt time.Time `json:"createdAt"`

	Attachments []Attachment `json:"attachments"` // mereu non-nil
}

const messageCols = `id, project_id, agent_id, kind, author, text, run_id, delivered, created_at`

func scanMessage(sc scanner) (Message, error) {
	var m Message
	var run sql.NullString
	var created string
	if err := sc.Scan(&m.ID, &m.ProjectID, &m.AgentID, &m.Kind, &m.Author, &m.Text, &run, &m.Delivered, &created); err != nil {
		return Message{}, err
	}
	m.RunID = run.String
	m.CreatedAt = parseTime(created)
	m.Attachments = []Attachment{}
	return m, nil
}

func (s *Store) AddMessage(m *Message) error {
	ts := now()
	var run any
	if m.RunID != "" {
		run = m.RunID
	}
	res, err := s.db.Exec(`INSERT INTO messages (project_id, agent_id, kind, author, text, run_id, delivered, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, m.ProjectID, m.AgentID, m.Kind, m.Author, m.Text, run, m.Delivered, ts)
	if err != nil {
		return err
	}
	if m.ID, err = res.LastInsertId(); err != nil {
		return err
	}
	m.CreatedAt = parseTime(ts)
	m.Attachments = []Attachment{}
	return nil
}

func (s *Store) queryMessages(where string, args ...any) ([]Message, error) {
	rows, err := s.db.Query(`SELECT `+messageCols+` FROM messages WHERE `+where+` ORDER BY id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Message{}
	for rows.Next() {
		m, err := scanMessage(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close() // o singura conexiune: eliberam cursorul inainte de urmatoarea interogare

	// Atasamentele tuturor mesajelor, printr-o singura interogare.
	ids := make([]int64, len(out))
	for i, m := range out {
		ids[i] = m.ID
	}
	byMsg, err := s.attachmentsFor(ids)
	if err != nil {
		return nil, err
	}
	for i := range out {
		if att, ok := byMsg[out[i].ID]; ok {
			out[i].Attachments = att
		}
	}
	return out, nil
}

// ListMessages intoarce thread-ul agentului, cronologic.
func (s *Store) ListMessages(agentID int64) ([]Message, error) {
	return s.queryMessages(`agent_id = ?`, agentID)
}

// Undelivered intoarce ce trebuie trimis agentului la urmatorul run.
func (s *Store) Undelivered(agentID int64) ([]Message, error) {
	return s.queryMessages(`agent_id = ? AND delivered = 0`, agentID)
}

func (s *Store) HasUndelivered(agentID int64) (bool, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM messages WHERE agent_id = ? AND delivered = 0`, agentID).Scan(&n)
	return n > 0, err
}

func (s *Store) MarkDelivered(ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, id := range ids {
		if _, err := tx.Exec(`UPDATE messages SET delivered = 1 WHERE id = ?`, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}
