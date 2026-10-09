package store

// coloanele cu text pe care agentii il folosesc in continuare (instructiuni, brief-uri, programe)
// si calea fisierelor modificate (Modified / View file); istoricul (mesaje, evenimente) ramane
// cum a fost scris, calea veche merge acolo prin symlink-ul pus de config.MoveLegacyData
var dataRootColumns = []struct{ table, column string }{
	{"agents", "background"}, {"agents", "instructions"}, {"agents", "responsibilities"}, {"agents", "done_def"},
	{"projects", "goal"}, {"projects", "context"}, {"project_folders", "context"},
	{"tasks", "description"}, {"tasks", "feedback"},
	{"recurring_tasks", "description"},
	{"task_changes", "path"},
}

// RewriteDataRoot inlocuieste folderul de date vechi cu cel nou (old + "/" -> new + "/", si cu "\"
// pe Windows) in coloanele de mai sus, intr-o tranzactie. Se cheama o data, dupa mutarea datelor.
func (s *Store) RewriteDataRoot(old, new string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, sep := range []string{"/", `\`} {
		from, to := old+sep, new+sep
		for _, c := range dataRootColumns {
			// numele de tabele si coloane sunt constante din cod, nu intrare de la utilizator
			q := `UPDATE ` + c.table + ` SET ` + c.column + ` = replace(` + c.column + `, ?, ?) WHERE instr(` + c.column + `, ?) > 0`
			if _, err := tx.Exec(q, from, to, from); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}
