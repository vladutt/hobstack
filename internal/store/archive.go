package store

import "strings"

// ArchivedGroup: un task sters de utilizator (radacina grupului) si cate sub-task-uri au plecat cu el.
type ArchivedGroup struct {
	Task
	Subtasks int `json:"subtasks"`
}

// TaskTree: task-ul si descendentii lui nearhivati, task-ul primul, apoi in ordinea numerelor.
func (s *Store) TaskTree(id int64) ([]Task, error) {
	rows, err := s.db.Query(`WITH RECURSIVE sub(id) AS (
			SELECT ? UNION ALL SELECT t.id FROM tasks t JOIN sub ON t.parent_task_id = sub.id WHERE t.status != 'archived')
		SELECT `+taskCols+` FROM tasks WHERE id IN (SELECT id FROM sub) ORDER BY id = ? DESC, number`, id, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Task{}
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// ArchiveTasks arhiveaza task-urile (statusul dinainte se pastreaza) si inchide ce astepta dupa ele
// in "Needs you": intrebari, cereri de acces, cereri de agenti. Totul intr-o tranzactie.
func (s *Store) ArchiveTasks(ids []int64, root int64) error {
	if len(ids) == 0 {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	ts := now()
	in, args := idList(ids)
	q := `UPDATE tasks SET archived_from = status, status = 'archived', archived_at = ?, archived_root = ?, updated_at = ?
		WHERE status != 'archived' AND id IN (` + in + `)`
	if _, err := tx.Exec(q, append([]any{ts, root, ts}, args...)...); err != nil {
		return err
	}
	for _, c := range []string{
		`UPDATE questions SET status = 'dismissed', closed_at = ? WHERE status = 'open' AND task_id IN (` + in + `)`,
		`UPDATE agent_requests SET status = 'dismissed', closed_at = ? WHERE status = 'open' AND task_id IN (` + in + `)`,
	} {
		if _, err := tx.Exec(c, append([]any{ts}, args...)...); err != nil {
			return err
		}
	}
	// access_requests nu are closed_at, doar dismissed
	if _, err := tx.Exec(`UPDATE access_requests SET dismissed = 1 WHERE dismissed = 0 AND task_id IN (`+in+`)`, args...); err != nil {
		return err
	}
	return tx.Commit()
}

// RestoreGroup readuce task-urile sterse impreuna cu root la statusul dinainte; in lucru -> in coada
// (run-ul a fost oprit). Intoarce task-urile restaurate (root primul).
func (s *Store) RestoreGroup(root int64) ([]Task, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`UPDATE tasks SET
			status = CASE archived_from WHEN 'in_progress' THEN 'queued' WHEN '' THEN 'queued' ELSE archived_from END,
			archived_from = '', archived_at = NULL, archived_root = NULL, updated_at = ?, nudged_at = NULL
		WHERE archived_root = ? AND status = 'archived'`, now(), root); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.TaskTree(root)
}

// ListArchivedRoots: task-urile pe care utilizatorul le-a sters (radacinile grupurilor), cele mai noi primele.
func (s *Store) ListArchivedRoots(projectID int64) ([]ArchivedGroup, error) {
	rows, err := s.db.Query(`SELECT `+taskCols+`,
			(SELECT COUNT(*) FROM tasks x WHERE x.archived_root = tasks.id AND x.id != tasks.id)
		FROM tasks WHERE project_id = ? AND status = 'archived' AND archived_root = id
		ORDER BY archived_at DESC`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ArchivedGroup{}
	for rows.Next() {
		var g ArchivedGroup
		t, err := scanTaskWith(rows, &g.Subtasks)
		if err != nil {
			return nil, err
		}
		g.Task = t
		out = append(out, g)
	}
	return out, rows.Err()
}

func (s *Store) CountArchivedRoots(projectID int64) (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM tasks WHERE project_id = ? AND status = 'archived' AND archived_root = id`, projectID).Scan(&n)
	return n, err
}

// idList: "?, ?, ?" si argumentele pentru un IN (...).
func idList(ids []int64) (string, []any) {
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	return strings.TrimSuffix(strings.Repeat("?, ", len(ids)), ", "), args
}
