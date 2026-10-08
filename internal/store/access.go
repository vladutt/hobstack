package store

import (
	"database/sql"
	"time"
)

// AccessRequest: un agent a fost refuzat la un tool; refuzurile repetate pentru
// acelasi (agent, tool) se aduna intr-un singur rand deschis, cu count.
type AccessRequest struct {
	ID        int64     `json:"id"`
	ProjectID int64     `json:"projectId"`
	AgentID   int64     `json:"agentId"`
	TaskID    *int64    `json:"taskId"` // ultimul task in care a fost refuzat; nil = fara task
	Tool      string    `json:"tool"`
	Detail    string    `json:"detail"` // rezumat scurt al ultimei incercari
	Prefix    string    `json:"prefix"` // prefixul regulii Ask care a refuzat comanda; "" = cerere de tool
	Count     int       `json:"count"`
	Dismissed bool      `json:"dismissed"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

const accessCols = `id, project_id, agent_id, task_id, tool, detail, prefix, count, dismissed, created_at, updated_at`

func scanAccessRequest(sc scanner) (AccessRequest, error) {
	var r AccessRequest
	var created, updated string
	err := sc.Scan(&r.ID, &r.ProjectID, &r.AgentID, &r.TaskID, &r.Tool, &r.Detail, &r.Prefix, &r.Count, &r.Dismissed, &created, &updated)
	if err != nil {
		return AccessRequest{}, err
	}
	r.CreatedAt, r.UpdatedAt = parseTime(created), parseTime(updated)
	return r, nil
}

// RecordAccessDenial noteaza un refuz. Daca agentul are deja un rand deschis
// pentru acelasi tool si prefix, il comasam (count++, detail si task_id = ultimele).
func (s *Store) RecordAccessDenial(projectID, agentID int64, taskID *int64, tool, detail, prefix string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	ts := now()
	var id int64
	err = tx.QueryRow(`SELECT id FROM access_requests WHERE agent_id = ? AND tool = ? AND prefix = ? AND dismissed = 0`, agentID, tool, prefix).Scan(&id)
	switch {
	case err == nil:
		// task_id se schimba doar cand avem unul nou
		if _, err := tx.Exec(`UPDATE access_requests SET count = count + 1, detail = ?, updated_at = ?,
			task_id = COALESCE(?, task_id) WHERE id = ?`, detail, ts, nullPtr(taskID), id); err != nil {
			return err
		}
	case err == sql.ErrNoRows:
		if _, err := tx.Exec(`INSERT INTO access_requests (project_id, agent_id, task_id, tool, detail, prefix, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, projectID, agentID, nullPtr(taskID), tool, detail, prefix, ts, ts); err != nil {
			return err
		}
	default:
		return err
	}
	return tx.Commit()
}

// ListOpenAccessRequests intoarce cererile nerezolvate ale proiectului, in ordinea id-ului.
func (s *Store) ListOpenAccessRequests(projectID int64) ([]AccessRequest, error) {
	rows, err := s.db.Query(`SELECT `+accessCols+` FROM access_requests WHERE project_id = ? AND dismissed = 0 ORDER BY id`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AccessRequest{}
	for rows.Next() {
		r, err := scanAccessRequest(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// GetAccessRequest intoarce sql.ErrNoRows daca cererea nu exista.
func (s *Store) GetAccessRequest(id int64) (AccessRequest, error) {
	return scanAccessRequest(s.db.QueryRow(`SELECT `+accessCols+` FROM access_requests WHERE id = ?`, id))
}

// DismissAccessRequest inchide cererea; un refuz nou va face un rand nou.
func (s *Store) DismissAccessRequest(id int64) error {
	_, err := s.db.Exec(`UPDATE access_requests SET dismissed = 1 WHERE id = ?`, id)
	return err
}

func (s *Store) CountOpenAccessRequests(projectID int64) (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM access_requests WHERE project_id = ? AND dismissed = 0`, projectID).Scan(&n)
	return n, err
}
