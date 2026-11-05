package config

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

// legacyName e numele vechi al aplicatiei (foreman): datele erau in ~/.foreman/foreman.db.
const legacyName = "foreman"

// MoveLegacyData muta o singura data ~/.foreman in ~/.<AppName> si redenumeste baza de date
// (cu -wal / -shm). In locul folderului vechi ramane un symlink catre cel nou, ca textele vechi
// (task-uri, mesaje, instructiuni) care contin calea veche sa mearga in continuare.
// Intoarce folderul vechi daca a mutat ceva, altfel "". Nu atinge nimic daca folderul nou exista.
// Se cheama la pornire, inainte sa se deschida baza de date.
func MoveLegacyData(home string) (string, error) {
	oldRoot := filepath.Join(home, "."+legacyName)
	newRoot := filepath.Join(home, "."+AppName)
	if _, err := os.Lstat(newRoot); err == nil || !errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	fi, err := os.Lstat(oldRoot)
	if err != nil || !fi.IsDir() {
		return "", nil // fara date vechi (sau e deja symlink)
	}
	if err := os.Rename(oldRoot, newRoot); err != nil {
		return "", err
	}
	for _, suffix := range []string{".db", ".db-wal", ".db-shm"} {
		from := filepath.Join(newRoot, legacyName+suffix)
		if _, err := os.Stat(from); err != nil {
			continue
		}
		if err := os.Rename(from, filepath.Join(newRoot, AppName+suffix)); err != nil {
			return oldRoot, err
		}
	}
	// pe Windows symlink-ul cere drepturi speciale: fara el, calea veche pur si simplu nu mai exista
	_ = os.Symlink(newRoot, oldRoot)
	return oldRoot, nil
}

// MoveToInstall muta o singura data datele din ~/.hobstack in folderul installerului (root = <folder>/data),
// cand acolo nu e inca o baza de date. Intoarce folderul vechi daca a mutat ceva. La eroare (de ex. un HobStack
// vechi tine baza deschisa) nu schimba nimic: apelantul ramane pe folderul vechi.
func MoveToInstall(home, root string) (string, error) {
	oldRoot := filepath.Join(home, "."+AppName)
	if filepath.Clean(oldRoot) == filepath.Clean(root) {
		return "", nil
	}
	if _, err := os.Stat(filepath.Join(root, AppName+".db")); err == nil {
		return "", nil // instalarea are deja datele ei
	}
	fi, err := os.Lstat(oldRoot)
	if err != nil || !fi.IsDir() {
		return "", nil // nimic de mutat
	}
	if _, err := os.Stat(filepath.Join(oldRoot, AppName+".db")); err != nil {
		return "", nil
	}
	// un folder data gol (creat de o pornire anterioara) nu trebuie sa blocheze mutarea
	if entries, err := os.ReadDir(root); err == nil {
		if len(entries) > 0 {
			return "", errors.New(root + " already has files; leaving the data in " + oldRoot)
		}
		os.Remove(root)
	}
	if err := os.MkdirAll(filepath.Dir(root), 0o755); err != nil {
		return "", err
	}
	if err := os.Rename(oldRoot, root); err != nil {
		return "", err
	}
	return oldRoot, nil
}
