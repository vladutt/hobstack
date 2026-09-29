package config

import (
	"os"
	"path/filepath"
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
	}, nil
}

// Ensure creeaza folderele daca nu exista.
func (p Paths) Ensure() error {
	for _, d := range []string{p.Root, p.Logs} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return err
		}
	}
	return nil
}
