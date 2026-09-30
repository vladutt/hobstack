package store

import (
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
)

// Folder e un folder de lucru al proiectului (ex. "Client" -> ~/code/probe-client).
// Poate fi repo git sau folder simplu. Primul (Position 0) e cel implicit:
// acolo lucreaza agentii fara folder ales.
type Folder struct {
	ID        int64  `json:"id"`
	ProjectID int64  `json:"projectId"`
	Position  int    `json:"position"`
	Name      string `json:"name"`
	Path      string `json:"path"`
	Context   string `json:"context"`
}

// FolderName e numele implicit al unui folder: ultimul segment din cale.
func FolderName(path string) string {
	base := filepath.Base(strings.TrimRight(filepath.Clean(path), `/\`))
	if base == "." || base == "/" || base == `\` || base == "" {
		return "Main"
	}
	return base
}

// HomeFolder: folderul in care lucreaza agentul. Fara folder ales (sau cu unul
// care nu mai exista) e primul folder al proiectului.
func HomeFolder(a Agent, folders []Folder) Folder {
	if a.FolderID != nil {
		for _, f := range folders {
			if f.ID == *a.FolderID {
				return f
			}
		}
	}
	if len(folders) > 0 {
		return folders[0]
	}
	return Folder{}
}

// ListFolders intoarce folderele proiectului, in ordine.
func (s *Store) ListFolders(projectID int64) ([]Folder, error) {
	return listFolders(s.db, projectID)
}

func listFolders(q querier, projectID int64) ([]Folder, error) {
	rows, err := q.Query(`SELECT id, project_id, position, name, path, context FROM project_folders
		WHERE project_id = ? ORDER BY position, id`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Folder{}
	for rows.Next() {
		var f Folder
		if err := rows.Scan(&f.ID, &f.ProjectID, &f.Position, &f.Name, &f.Path, &f.Context); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// saveFolders inlocuieste folderele proiectului cu lista data, in ordinea ei
// (ca SaveStages). Cele cu ID se actualizeaza, cele fara se creeaza, iar cele
// care lipsesc se sterg; agentii lor trec pe primul folder (folder_id NULL).
// projects.workdir ramane egal cu calea primului folder. Seteaza ID-urile in lista.
func saveFolders(tx *sql.Tx, projectID int64, folders []Folder) error {
	if len(folders) == 0 {
		return errors.New("a project needs at least one folder")
	}
	old, err := listFolders(tx, projectID)
	if err != nil {
		return err
	}
	keep := map[int64]bool{}
	for _, f := range folders {
		if f.ID != 0 {
			keep[f.ID] = true
		}
	}
	for _, o := range old {
		if keep[o.ID] {
			continue
		}
		if _, err := tx.Exec(`UPDATE agents SET folder_id = NULL WHERE folder_id = ?`, o.ID); err != nil {
			return err
		}
		if _, err := tx.Exec(`DELETE FROM project_folders WHERE id = ?`, o.ID); err != nil {
			return err
		}
	}
	for i := range folders {
		f := &folders[i]
		f.ProjectID, f.Position = projectID, i
		if f.ID != 0 {
			if err := mustAffect(tx.Exec(`UPDATE project_folders SET position = ?, name = ?, path = ?, context = ?
				WHERE id = ? AND project_id = ?`, f.Position, f.Name, f.Path, f.Context, f.ID, projectID)); err != nil {
				return err
			}
			continue
		}
		res, err := tx.Exec(`INSERT INTO project_folders (project_id, position, name, path, context) VALUES (?, ?, ?, ?, ?)`,
			projectID, f.Position, f.Name, f.Path, f.Context)
		if err != nil {
			return err
		}
		if f.ID, err = res.LastInsertId(); err != nil {
			return err
		}
	}
	_, err = tx.Exec(`UPDATE projects SET workdir = ? WHERE id = ?`, folders[0].Path, projectID)
	return err
}

// defaultFolders: un proiect creat fara foldere primeste unul din Workdir.
func defaultFolders(p *Project) {
	if len(p.Folders) == 0 {
		p.Folders = []Folder{{Name: FolderName(p.Workdir), Path: p.Workdir}}
	}
	p.Workdir = p.Folders[0].Path
}
