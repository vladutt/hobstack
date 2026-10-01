package store

import (
	"database/sql"
	"errors"
)

// StartNewChat porneste un chat nou cu agentul: sterge sesiunea Claude (urmatorul
// run incepe o sesiune noua), scrie nota text in thread si muta inceputul
// chat-ului la ea. Mesajele vechi raman salvate (ListMessages le intoarce pe toate).
func (s *Store) StartNewChat(agentID int64, text string) (Message, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return Message{}, err
	}
	defer tx.Rollback()

	var projectID int64
	if err := tx.QueryRow(`SELECT project_id FROM agents WHERE id = ?`, agentID).Scan(&projectID); err != nil {
		return Message{}, notFound(err)
	}
	ts := now()
	res, err := tx.Exec(`INSERT INTO messages (project_id, agent_id, kind, author, text, run_id, delivered, created_at)
		VALUES (?, ?, ?, '', ?, NULL, 1, ?)`, projectID, agentID, KindEvent, text, ts)
	if err != nil {
		return Message{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Message{}, err
	}
	if _, err := tx.Exec(`UPDATE agents SET session_id = '', prompt_hash = '', chat_from = ? WHERE id = ?`, id, agentID); err != nil {
		return Message{}, err
	}
	if err := tx.Commit(); err != nil {
		return Message{}, err
	}
	return Message{ID: id, ProjectID: projectID, AgentID: agentID, Kind: KindEvent, Text: text,
		Delivered: true, CreatedAt: parseTime(ts), Attachments: []Attachment{}}, nil
}

// ListChat intoarce chat-ul curent al agentului: mesajele de la ultimul "New chat"
// incolo, plus cele inca nelivrate (agentul le primeste oricum). earlier = cate
// mesaje mai vechi sunt ascunse.
func (s *Store) ListChat(agentID int64) (msgs []Message, earlier int, err error) {
	var from int64
	if err := s.db.QueryRow(`SELECT chat_from FROM agents WHERE id = ?`, agentID).Scan(&from); err != nil {
		return nil, 0, notFound(err)
	}
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM messages WHERE agent_id = ? AND id < ? AND delivered = 1`,
		agentID, from).Scan(&earlier); err != nil {
		return nil, 0, err
	}
	msgs, err = s.queryMessages(`agent_id = ? AND (id >= ? OR delivered = 0)`, agentID, from)
	return msgs, earlier, err
}

// notFound transforma "niciun rand" in ErrNotFound (API-ul raspunde 404).
func notFound(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	return err
}
