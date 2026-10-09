package store

import "database/sql"

// Tipurile de etapa: de lucru (report_done) sau de verificare (review_approve/reject).
const (
	KindWork  = "work"
	KindCheck = "check"
)

// Stage e o coloana a board-ului: o etapa prin care trece un task.
type Stage struct {
	ID        int64   `json:"id"`
	ProjectID int64   `json:"projectId"`
	Position  int     `json:"position"`
	Name      string  `json:"name"`
	Kind      string  `json:"kind"`
	AgentIDs  []int64 `json:"agentIds"` // in ordinea in care iau task-uri
}

// StageInUseError: o etapa scoasa din lista inca are task-uri.
type StageInUseError string

func (e StageInUseError) Error() string {
	return "stage " + string(e) + " has tasks: finish or move them first"
}

// ListStages intoarce etapele proiectului in ordinea de pe board.
func (s *Store) ListStages(projectID int64) ([]Stage, error) {
	return listStages(s.db, projectID)
}

type querier interface {
	Query(query string, args ...any) (*sql.Rows, error)
}

func listStages(q querier, projectID int64) ([]Stage, error) {
	rows, err := q.Query(`SELECT id, project_id, position, name, kind FROM stages WHERE project_id = ? ORDER BY position, id`, projectID)
	if err != nil {
		return nil, err
	}
	out := []Stage{}
	for rows.Next() {
		var st Stage
		if err := rows.Scan(&st.ID, &st.ProjectID, &st.Position, &st.Name, &st.Kind); err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, st)
	}
	rows.Close()
	for i := range out {
		ar, err := q.Query(`SELECT agent_id FROM stage_agents WHERE stage_id = ? ORDER BY rowid`, out[i].ID)
		if err != nil {
			return nil, err
		}
		out[i].AgentIDs = []int64{}
		for ar.Next() {
			var id int64
			if err := ar.Scan(&id); err != nil {
				ar.Close()
				return nil, err
			}
			out[i].AgentIDs = append(out[i].AgentIDs, id)
		}
		ar.Close()
	}
	return out, nil
}

// CountStageTasks: cate task-uri sunt acum in etapa.
func (s *Store) CountStageTasks(stageID int64) (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM tasks WHERE stage_id = ? AND status != 'archived'`, stageID).Scan(&n)
	return n, err
}

// SaveStages inlocuieste etapele proiectului cu lista data, in ordinea ei.
// Etapele cu ID se actualizeaza, cele fara ID se creeaza, iar cele care lipsesc
// se sterg doar daca nu au task-uri (altfel StageInUseError si nu se schimba nimic).
// Validarea numelor si a agentilor o face apelantul.
func (s *Store) SaveStages(projectID int64, stages []Stage) ([]Stage, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	old, err := listStages(tx, projectID)
	if err != nil {
		return nil, err
	}
	keep := map[int64]bool{}
	for _, st := range stages {
		if st.ID != 0 {
			keep[st.ID] = true
		}
	}
	for _, o := range old {
		if keep[o.ID] {
			continue
		}
		var n int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM tasks WHERE stage_id = ? AND status != 'archived'`, o.ID).Scan(&n); err != nil {
			return nil, err
		}
		if n > 0 {
			return nil, StageInUseError(o.Name)
		}
		// task-urile arhivate din etapa raman fara etapa (la Restore apar in "No stage")
		if _, err := tx.Exec(`UPDATE tasks SET stage_id = NULL WHERE stage_id = ? AND status = 'archived'`, o.ID); err != nil {
			return nil, err
		}
		if _, err := tx.Exec(`DELETE FROM stages WHERE id = ?`, o.ID); err != nil {
			return nil, err
		}
	}
	for i := range stages {
		st := &stages[i]
		st.ProjectID, st.Position = projectID, i
		if st.ID != 0 {
			if err := mustAffect(tx.Exec(`UPDATE stages SET position = ?, name = ?, kind = ? WHERE id = ? AND project_id = ?`,
				st.Position, st.Name, st.Kind, st.ID, projectID)); err != nil {
				return nil, err
			}
		} else {
			res, err := tx.Exec(`INSERT INTO stages (project_id, position, name, kind) VALUES (?, ?, ?, ?)`,
				projectID, st.Position, st.Name, st.Kind)
			if err != nil {
				return nil, err
			}
			if st.ID, err = res.LastInsertId(); err != nil {
				return nil, err
			}
		}
		if _, err := tx.Exec(`DELETE FROM stage_agents WHERE stage_id = ?`, st.ID); err != nil {
			return nil, err
		}
		for _, aid := range st.AgentIDs {
			if _, err := tx.Exec(`INSERT OR IGNORE INTO stage_agents (stage_id, agent_id) VALUES (?, ?)`, st.ID, aid); err != nil {
				return nil, err
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.ListStages(projectID)
}
