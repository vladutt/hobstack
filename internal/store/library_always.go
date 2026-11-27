package store

// Skill-uri incarcate mereu: agentii primesc in tura cererea sa incarce skill-ul (tool-ul Skill) inainte de orice.
// Flag-ul global (library_items.always_load) se aplica in toate proiectele care au skill-ul; un proiect il poate
// suprascrie (library_always). Fara rand in library_always = proiectul urmeaza flag-ul global.

// SetItemAlwaysLoad schimba flag-ul global al elementului.
func (s *Store) SetItemAlwaysLoad(itemID int64, on bool) error {
	return mustAffect(s.db.Exec(`UPDATE library_items SET always_load = ? WHERE id = ?`, on, itemID))
}

// SetProjectAlwaysLoad salveaza alegerea proiectului; nil o sterge (proiectul urmeaza flag-ul global).
func (s *Store) SetProjectAlwaysLoad(projectID, itemID int64, on *bool) error {
	if on == nil {
		_, err := s.db.Exec(`DELETE FROM library_always WHERE project_id = ? AND item_id = ?`, projectID, itemID)
		return err
	}
	_, err := s.db.Exec(`INSERT INTO library_always (project_id, item_id, always) VALUES (?, ?, ?)
		ON CONFLICT (project_id, item_id) DO UPDATE SET always = excluded.always`, projectID, itemID, *on)
	return err
}

// AlwaysLoadOverrides: alegerile proiectului, item -> pornit / oprit.
func (s *Store) AlwaysLoadOverrides(projectID int64) (map[int64]bool, error) {
	rows, err := s.db.Query(`SELECT item_id, always FROM library_always WHERE project_id = ?`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]bool{}
	for rows.Next() {
		var id int64
		var on bool
		if err := rows.Scan(&id, &on); err != nil {
			return nil, err
		}
		out[id] = on
	}
	return out, rows.Err()
}

// AlwaysLoadIn: skill-ul se incarca mereu in proiect (alegerea proiectului, altfel flag-ul global).
func AlwaysLoadIn(it LibraryItem, overrides map[int64]bool) bool {
	if on, ok := overrides[it.ID]; ok {
		return on
	}
	return it.AlwaysLoad
}
