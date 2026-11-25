package store

// Elementele default din Library: pornite in toate proiectele, pe ultima versiune, cat sunt published.
// Un proiect le opreste cu un rand library_installs cu enabled = 0. Cheile globale (library_default_secrets)
// sunt fallback-ul pentru orice proiect care are elementul si nu are valoarea lui.

// SetLibraryDefault porneste / opreste flag-ul. La pornire, randurile pornite ale elementului se sterg (proiectele
// lor trec pe default, deci pe ultima versiune); randurile oprite raman si devin opriri.
func (s *Store) SetLibraryDefault(itemID int64, on bool) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := mustAffect(tx.Exec(`UPDATE library_items SET is_default = ? WHERE id = ?`, on, itemID)); err != nil {
		return err
	}
	if on {
		if _, err := tx.Exec(`DELETE FROM library_installs WHERE item_id = ? AND enabled = 1`, itemID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// SetDefaultSecret salveaza cheia globala a elementului; o valoare goala o sterge.
func (s *Store) SetDefaultSecret(itemID int64, name, value string) error {
	if value == "" {
		_, err := s.db.Exec(`DELETE FROM library_default_secrets WHERE item_id = ? AND name = ?`, itemID, name)
		return err
	}
	_, err := s.db.Exec(`INSERT INTO library_default_secrets (item_id, name, value) VALUES (?, ?, ?)
		ON CONFLICT (item_id, name) DO UPDATE SET value = excluded.value`, itemID, name, value)
	return err
}

// DefaultSecrets intoarce cheile globale ale elementului (harta goala daca nu are).
func (s *Store) DefaultSecrets(itemID int64) (map[string]string, error) {
	rows, err := s.db.Query(`SELECT name, value FROM library_default_secrets WHERE item_id = ?`, itemID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		out[k] = v
	}
	return out, rows.Err()
}

// DefaultOffCount: in cate proiecte e oprit elementul (randuri cu enabled = 0).
func (s *Store) DefaultOffCount(itemID int64) (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM library_installs WHERE item_id = ? AND enabled = 0`, itemID).Scan(&n)
	return n, err
}

// IsActiveDefault: elementul e default si published (doar atunci se aplica in proiecte).
func (s *Store) IsActiveDefault(itemID int64) (bool, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM library_items WHERE id = ? AND is_default = 1 AND status = ?`,
		itemID, ItemPublished).Scan(&n)
	return n > 0, err
}

// DisableInstall opreste elementul in proiect (randul cu enabled = 0; versiunea doar umple coloana).
func (s *Store) DisableInstall(projectID, itemID, versionID int64) error {
	_, err := s.db.Exec(`INSERT INTO library_installs (project_id, item_id, version_id, enabled) VALUES (?, ?, ?, 0)
		ON CONFLICT (project_id, item_id) DO UPDATE SET enabled = 0`, projectID, itemID, versionID)
	return err
}

// EffectiveInstalls: ce are de fapt proiectul. Randurile lui (ca ListInstalls) plus, pentru fiecare element
// default si published fara rand in proiect, o instalare pe versiunea cea mai noua (FromDefault). Un default
// fara nicio versiune e sarit. Ordonat dupa item_id.
func (s *Store) EffectiveInstalls(projectID int64) ([]LibraryInstall, error) {
	rows, err := s.db.Query(`
		SELECT project_id, item_id, version_id, enabled, 0 FROM library_installs WHERE project_id = ?
		UNION ALL
		SELECT ?, i.id, (SELECT MAX(v.id) FROM library_versions v WHERE v.item_id = i.id), 1, 1
		FROM library_items i
		WHERE i.is_default = 1 AND i.status = ?
		  AND EXISTS (SELECT 1 FROM library_versions v WHERE v.item_id = i.id)
		  AND NOT EXISTS (SELECT 1 FROM library_installs l WHERE l.project_id = ? AND l.item_id = i.id)
		ORDER BY 2`, projectID, projectID, ItemPublished, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []LibraryInstall{}
	for rows.Next() {
		var in LibraryInstall
		if err := rows.Scan(&in.ProjectID, &in.ItemID, &in.VersionID, &in.Enabled, &in.FromDefault); err != nil {
			return nil, err
		}
		out = append(out, in)
	}
	return out, rows.Err()
}

// EffectiveSecrets: cheile globale ale elementului, suprascrise de valorile nevide ale proiectului.
func (s *Store) EffectiveSecrets(projectID, itemID int64) (map[string]string, error) {
	out, err := s.DefaultSecrets(itemID)
	if err != nil {
		return nil, err
	}
	own, err := s.Secrets(projectID, itemID)
	if err != nil {
		return nil, err
	}
	for k, v := range own {
		if v != "" {
			out[k] = v
		}
	}
	return out, nil
}
