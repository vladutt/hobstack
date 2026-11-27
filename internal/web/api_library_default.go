package web

import (
	"log"
	"net/http"

	"hobstack/internal/store"
)

// putLibraryDefault porneste / opreste "Default for all projects" si schimba cheile globale ale elementului.
//
//	{"default": true|false, "secrets": {"NAME": "value"}, "clear": ["NAME"], "alwaysLoad": true|false}
//
// "alwaysLoad" (doar skill-uri): agentii il incarca la fiecare run, in toate proiectele care il au si nu au ales altfel.
//
// Fiecare camp e optional: cheile globale se pot schimba si fara flag (sunt fallback-ul oricarui proiect care are
// elementul). O valoare goala in "secrets" nu schimba nimic; se sterge doar cu "clear". Raspunsul e itemView.
func (s *Server) putLibraryDefault(w http.ResponseWriter, r *http.Request) {
	if !s.libraryOK(w) {
		return
	}
	id, err := pathID(r)
	if err != nil {
		fail(w, err)
		return
	}
	var in struct {
		Default *bool             `json:"default"`
		Secrets map[string]string `json:"secrets"`
		Clear   []string          `json:"clear"`
		Always  *bool             `json:"alwaysLoad"`
	}
	if err := decode(r, &in); err != nil {
		fail(w, err)
		return
	}
	it, err := s.st.GetLibraryItem(id)
	if err != nil {
		fail(w, err)
		return
	}
	if err := checkSecretValues(it.Secrets, in.Secrets, in.Clear); err != nil {
		fail(w, err)
		return
	}
	if in.Always != nil && it.Kind != store.KindSkill {
		writeErr(w, http.StatusBadRequest, "only skills can be always loaded")
		return
	}
	turnOn := in.Default != nil && *in.Default && !it.IsDefault
	if turnOn && it.Status != store.ItemPublished {
		writeErr(w, http.StatusConflict, "only published items can be default")
		return
	}
	for name, val := range in.Secrets {
		if val == "" {
			continue
		}
		if err := s.st.SetDefaultSecret(id, name, val); err != nil {
			fail(w, err)
			return
		}
	}
	for _, name := range in.Clear {
		if err := s.st.SetDefaultSecret(id, name, ""); err != nil {
			fail(w, err)
			return
		}
	}
	if in.Default != nil && *in.Default != it.IsDefault {
		if err := s.st.SetLibraryDefault(id, *in.Default); err != nil {
			fail(w, err)
			return
		}
	}
	if in.Always != nil && *in.Always != it.AlwaysLoad {
		if err := s.st.SetItemAlwaysLoad(id, *in.Always); err != nil {
			fail(w, err)
			return
		}
	}
	s.defaultChanged(it, turnOn)
	cur, err := s.st.GetLibraryItem(id)
	if err != nil {
		fail(w, err)
		return
	}
	installs, err := s.st.ListItemInstalls(id)
	if err != nil {
		fail(w, err)
		return
	}
	view, err := s.itemViewOf(cur, len(installs))
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

// defaultChanged anunta toate proiectele (pagina Library din proiect se reface). Cand flag-ul tocmai a pornit,
// proiectele in care elementul e acum activ il primesc ca la o instalare (un skill inchide cardurile lui).
func (s *Server) defaultChanged(it store.LibraryItem, turnedOn bool) {
	projects, err := s.st.ListProjects()
	if err != nil {
		log.Printf("library default %s: %v", it.Name, err)
		return
	}
	for _, p := range projects {
		if turnedOn {
			installs, err := s.st.EffectiveInstalls(p.ID)
			if err != nil {
				log.Printf("library default %s, project %d: %v", it.Name, p.ID, err)
				continue
			}
			for _, in := range installs {
				if in.ItemID == it.ID && in.Enabled {
					s.skillInstalled(p.ID, it)
				}
			}
		}
		s.hub.Publish(p.ID, "project", map[string]int64{"id": p.ID})
	}
}
