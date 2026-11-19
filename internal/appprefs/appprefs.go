// Package appprefs tine preferintele aplicatiei care nu stau in baza de date: unde se deschide HobStack
// (fereastra proprie sau browserul) si marimea / pozitia / zoom-ul ferestrei. Fisierul e <date>/app.json.
package appprefs

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

const (
	OpenWindow  = "window"  // fereastra proprie (implicit)
	OpenBrowser = "browser" // un tab in browserul implicit
)

// Frame: pozitia si marimea ferestrei, in punctele ecranului (macOS: originea jos-stanga).
type Frame struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
	W float64 `json:"w"`
	H float64 `json:"h"`
}

type Prefs struct {
	OpenIn string  `json:"openIn"`
	Frame  *Frame  `json:"window,omitempty"`
	Zoom   float64 `json:"zoom,omitempty"` // 0 = 1.0
	// notificarile de sistem (Settings > App); nil = pornite
	NotifyDone  *bool `json:"notifyDone,omitempty"`
	NotifyNeeds *bool `json:"notifyNeeds,omitempty"`
}

// Notify: "un proiect a terminat" si "ceva are nevoie de tine"; lipsa = pornite.
func (p Prefs) Notify() (done, needs bool) {
	return p.NotifyDone == nil || *p.NotifyDone, p.NotifyNeeds == nil || *p.NotifyNeeds
}

var mu sync.Mutex // Update: citire + scriere fara sa se calce doua schimbari

func file(root string) string { return filepath.Join(root, "app.json") }

// Load nu esueaza: fisier lipsa, stricat sau cu valori necunoscute = valorile implicite (fereastra).
// O preferinta stricata nu are voie sa opreasca pornirea.
func Load(root string) Prefs {
	var p Prefs
	if raw, err := os.ReadFile(file(root)); err == nil {
		if json.Unmarshal(raw, &p) != nil {
			p = Prefs{}
		}
	}
	if p.OpenIn != OpenBrowser {
		p.OpenIn = OpenWindow
	}
	return p
}

// Save scrie atomic: fisier temporar in acelasi folder + rename.
func Save(root string, p Prefs) error {
	raw, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(root, "app.json.tmp-*")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(raw); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	os.Chmod(tmp.Name(), 0o644)
	if err := os.Rename(tmp.Name(), file(root)); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return nil
}

// Update citeste, schimba prin fn si salveaza, sub un lock comun.
func Update(root string, fn func(*Prefs)) error {
	mu.Lock()
	defer mu.Unlock()
	p := Load(root)
	fn(&p)
	return Save(root, p)
}
