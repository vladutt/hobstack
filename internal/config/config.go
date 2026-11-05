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
const AppName = "hobstack"

// DisplayName e numele afisat in UI (titlul paginii, panoul din wizard).
const DisplayName = "HobStack"

// Version e versiunea aplicatiei (semver fara "v"), pusa la build:
//
//	go build -ldflags "-X hobstack/internal/config.Version=1.0.0"
//
// (scripts/build-release.sh pentru release-uri, scripts/build.sh local). Un go build simplu ramane "dev".
var Version = "dev"

// UpdateRepo e repo-ul GitHub cu release-urile (update din aplicatie).
const UpdateRepo = "vladutt/hobstack"

// Paths tine toate locatiile de pe disc folosite de aplicatie.
type Paths struct {
	Root string // ~/.hobstack
	DB   string // ~/.hobstack/hobstack.db
	Logs string // ~/.hobstack/logs/runs

	Projects   string // ~/.hobstack/projects (fisierele proiectelor)
	Workspaces string // ~/HobStack (foldere implicite de lucru; nu se creeaza aici)
}

func Default() (Paths, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Paths{}, err
	}
	root := filepath.Join(home, "."+AppName)
	if dir := InstallDir(); dir != "" {
		root = filepath.Join(dir, "data")
	}
	return PathsAt(home, root), nil
}

// PathsAt intoarce locatiile pentru datele din root.
func PathsAt(home, root string) Paths {
	return Paths{
		Root: root,
		DB:   filepath.Join(root, AppName+".db"),
		Logs: filepath.Join(root, "logs", "runs"),

		Projects:   filepath.Join(root, "projects"),
		Workspaces: filepath.Join(home, "HobStack"),
	}
}

// InstallMarker: fisierul pus de installer langa exe; cu el, datele stau in <folder>/data, langa binar.
const InstallMarker = AppName + ".portable"

// InstallDir: folderul binarului daca HobStack e instalat cu installerul (are InstallMarker langa el), altfel "".
func InstallDir() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	if real, err := filepath.EvalSymlinks(exe); err == nil {
		exe = real
	}
	dir := filepath.Dir(exe)
	if _, err := os.Stat(filepath.Join(dir, InstallMarker)); err != nil {
		return ""
	}
	return dir
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
