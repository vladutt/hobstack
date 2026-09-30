// Package opener deschide folderul unui proiect in managerul de fisiere,
// intr-un editor instalat sau pagina lui html in browser. Comenzile ruleaza
// local, pe calculatorul utilizatorului, si doar pentru aplicatii cunoscute.
package opener

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

var (
	ErrNoHTML        = errors.New("no .html file found in the project folder")
	ErrUnknownEditor = errors.New("unknown or not installed editor")
)

// Editor e un editor detectat pe calculator.
type Editor struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// spec descrie cum gasim si pornim un editor pe fiecare sistem.
type spec struct {
	id, name string
	macApps  []string // numele aplicatiei din /Applications (pot fi mai multe variante)
	command  string   // comanda din PATH pe Windows/Linux
}

// known e lista editoarelor pe care le stim deschide, in ordinea din UI.
var known = []spec{
	{"phpstorm", "PhpStorm", []string{"PhpStorm"}, "phpstorm"},
	{"vscode", "VS Code", []string{"Visual Studio Code"}, "code"},
	{"cursor", "Cursor", []string{"Cursor"}, "cursor"},
	{"idea", "IntelliJ IDEA", []string{"IntelliJ IDEA", "IntelliJ IDEA Ultimate", "IntelliJ IDEA CE"}, "idea"},
	{"webstorm", "WebStorm", []string{"WebStorm"}, "webstorm"},
	{"goland", "GoLand", []string{"GoLand"}, "goland"},
	{"sublime", "Sublime Text", []string{"Sublime Text"}, "subl"},
	{"zed", "Zed", []string{"Zed"}, "zed"},
}

// detect intoarce editoarele instalate. Verificarile sunt primite ca functii,
// ca sa poata fi testat fara aplicatii reale.
func detect(mac bool, appExists func(app string) bool, cmdExists func(cmd string) bool) []Editor {
	out := []Editor{}
	for _, s := range known {
		found := false
		if mac {
			for _, app := range s.macApps {
				if appExists(app) {
					found = true
					break
				}
			}
		} else {
			found = cmdExists(s.command)
		}
		if found {
			out = append(out, Editor{ID: s.id, Name: s.name})
		}
	}
	return out
}

// macAppPath intoarce calea aplicatiei (in /Applications sau ~/Applications), sau "".
func macAppPath(app string) string {
	home, _ := os.UserHomeDir()
	for _, dir := range []string{"/Applications", filepath.Join(home, "Applications")} {
		p := filepath.Join(dir, app+".app")
		if info, err := os.Stat(p); err == nil && info.IsDir() {
			return p
		}
	}
	return ""
}

// FindHTML cauta pagina de deschis in browser, doar in radacina folderului:
// index.html are prioritate, altfel primul fisier .html in ordine alfabetica.
func FindHTML(dir string) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}
	var pages []string
	for _, e := range entries {
		if e.IsDir() || !strings.EqualFold(filepath.Ext(e.Name()), ".html") {
			continue
		}
		if strings.EqualFold(e.Name(), "index.html") {
			return filepath.Join(dir, e.Name()), nil
		}
		pages = append(pages, e.Name())
	}
	if len(pages) == 0 {
		return "", ErrNoHTML
	}
	sort.Strings(pages)
	return filepath.Join(dir, pages[0]), nil
}

// Opener deschide lucruri pe sistemul curent.
type Opener struct {
	editors []Editor
}

// New detecteaza editoarele instalate (o singura data, la pornire).
func New() *Opener {
	mac := runtime.GOOS == "darwin"
	appExists := func(app string) bool { return macAppPath(app) != "" }
	cmdExists := func(cmd string) bool { _, err := exec.LookPath(cmd); return err == nil }
	return &Opener{editors: detect(mac, appExists, cmdExists)}
}

func (o *Opener) Editors() []Editor { return o.editors }

// OpenFolder deschide folderul in managerul de fisiere.
func (o *Opener) OpenFolder(dir string) error {
	name, args := folderCommand(dir)
	return start(name, args...)
}

// OpenFile deschide fisierul cu aplicatia implicita (html -> browser).
func (o *Opener) OpenFile(path string) error {
	name, args := fileCommand(path)
	return start(name, args...)
}

// OpenInEditor deschide folderul in editorul cu id-ul dat, daca e instalat.
func (o *Opener) OpenInEditor(id, dir string) error {
	for _, e := range o.editors {
		if e.ID != id {
			continue
		}
		for _, s := range known {
			if s.id == id {
				name, args := editorCommand(s, dir)
				return start(name, args...)
			}
		}
	}
	return ErrUnknownEditor
}

// start porneste comanda fara sa astepte dupa ea (editorul ramane deschis).
func start(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	if err := cmd.Start(); err != nil {
		return err
	}
	go cmd.Wait() // colectam procesul cand se termina, ca sa nu ramana zombie
	return nil
}

func folderCommand(dir string) (string, []string) {
	switch runtime.GOOS {
	case "darwin":
		return "open", []string{dir}
	case "windows":
		return "explorer", []string{dir}
	default:
		return "xdg-open", []string{dir}
	}
}

func fileCommand(path string) (string, []string) {
	switch runtime.GOOS {
	case "darwin":
		return "open", []string{path}
	case "windows":
		return "rundll32", []string{"url.dll,FileProtocolHandler", path}
	default:
		return "xdg-open", []string{path}
	}
}

func editorCommand(s spec, dir string) (string, []string) {
	if runtime.GOOS == "darwin" {
		for _, app := range s.macApps {
			if p := macAppPath(app); p != "" {
				return "open", []string{"-a", p, dir}
			}
		}
	}
	return s.command, []string{dir}
}
