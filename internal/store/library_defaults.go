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
