package config

import (
	"os"
	"path/filepath"
	"strconv"
)

// AppName e numele aplicatiei. Din el se deriva folderul de date, fisierul
// bazei de date si textele afisate. Ca sa redenumesti aplicatia: schimbi
// constanta, redenumesti cmd/<nume> si rulezi `go mod edit -module <nume>`
// plus un replace pe importuri.
const AppName = "foreman"

// Paths tine toate locatiile de pe disc folosite de aplicatie.
type Paths struct {
	Root string // ~/.foreman
	DB   string // ~/.foreman/foreman.db
	Logs string // ~/.foreman/logs/runs

	Projects   string // ~/.foreman/projects (fisierele proiectelor)
	Workspaces string // ~/Foreman (foldere implicite de lucru; nu se creeaza aici)
}

func Default() (Paths, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Paths{}, err
	}
	root := filepath.Join(home, "."+AppName)
	return Paths{
		Root: root,
		DB:   filepath.Join(root, AppName+".db"),
		Logs: filepath.Join(root, "logs", "runs"),

		Projects:   filepath.Join(root, "projects"),
		Workspaces: filepath.Join(home, "Foreman"),
	}, nil
}

// Ensure creeaza folderele daca nu exista.
func (p Paths) Ensure() error {
	for _, d := range []string{p.Root, p.Logs, p.Projects} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return err
		}
	}
	return nil
}

// ProjectFilesDir intoarce folderul cu fisierele unui proiect.
func ProjectFilesDir(projectsRoot string, id int64) string {
	return filepath.Join(projectsRoot, strconv.FormatInt(id, 10), "files")
}

// ProjectTaskFilesDir: folderul cu fisierele de pe task-urile unui proiect (task-files/<task>/ sub el).
func ProjectTaskFilesDir(projectsRoot string, projectID int64) string {
	return filepath.Join(projectsRoot, strconv.FormatInt(projectID, 10), "task-files")
}

// TaskFilesDir: copiile fisierelor predate pe un task (report_done files).
func TaskFilesDir(projectsRoot string, projectID, taskID int64) string {
	return filepath.Join(ProjectTaskFilesDir(projectsRoot, projectID), strconv.FormatInt(taskID, 10))
}
