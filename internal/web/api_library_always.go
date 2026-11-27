package web

import (
	"net/http"

	"hobstack/internal/store"
)

// putProjectAlways: alegerea proiectului pentru "Always load" pe un skill.
//
//	{"on": true|false|null}
//
// null sterge alegerea (proiectul urmeaza flag-ul global al skill-ului). Raspuns 204; run-urile urmatoare o folosesc.
func (s *Server) putProjectAlways(w http.ResponseWriter, r *http.Request) {
	if !s.libraryOK(w) {
		return
	}
	pid, err := pathID(r)
	if err != nil {
		fail(w, err)
		return
	}
	itemID, err := idValue(r.PathValue("item"))
	if err != nil {
		fail(w, err)
		return
	}
	var in struct {
		On *bool `json:"on"`
	}
	if err := decode(r, &in); err != nil {
		fail(w, err)
		return
	}
	if _, err := s.st.GetProject(pid); err != nil {
		fail(w, err)
		return
	}
	it, err := s.st.GetLibraryItem(itemID)
	if err != nil {
		fail(w, err)
		return
	}
	if it.Kind != store.KindSkill {
		writeErr(w, http.StatusBadRequest, "only skills can be always loaded")
		return
	}
	if err := s.st.SetProjectAlwaysLoad(pid, itemID, in.On); err != nil {
		fail(w, err)
		return
	}
	s.hub.Publish(pid, "project", map[string]int64{"id": pid})
	w.WriteHeader(http.StatusNoContent)
}
