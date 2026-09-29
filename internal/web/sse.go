package web

import (
	"fmt"
	"net/http"
	"time"
)

func sseHeaders(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
}

// streamRun trimite evenimentele unui run: tot istoricul, apoi live pana la final.
func (s *Server) streamRun(w http.ResponseWriter, r *http.Request) {
	run, err := s.runs.Get(r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeErr(w, http.StatusInternalServerError, "streaming nesuportat")
		return
	}
	sseHeaders(w)

	replay, ch, done := run.Subscribe()
	for _, line := range replay {
		fmt.Fprintf(w, "data: %s\n\n", line)
	}
	flusher.Flush()
	if done {
		return
	}
	defer run.Unsubscribe(ch)

	ping := time.NewTicker(15 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ping.C:
			fmt.Fprint(w, ": ping\n\n")
			flusher.Flush()
		case line, ok := <-ch:
			if !ok {
				return
			}
			fmt.Fprintf(w, "data: %s\n\n", line)
			flusher.Flush()
		}
	}
}

// projectEvents trimite schimbarile unui proiect (task, mesaj, agent, proiect).
// UI-ul reincarca starea completa la reconectare, deci nu trimitem istoric.
func (s *Server) projectEvents(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		fail(w, err)
		return
	}
	if _, err := s.st.GetProject(id); err != nil {
		fail(w, err)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeErr(w, http.StatusInternalServerError, "streaming nesuportat")
		return
	}
	sseHeaders(w)
	ch, cancel := s.hub.Subscribe(id)
	defer cancel()
	fmt.Fprint(w, ": conectat\n\n")
	flusher.Flush()

	ping := time.NewTicker(15 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ping.C:
			fmt.Fprint(w, ": ping\n\n")
			flusher.Flush()
		case payload, ok := <-ch:
			if !ok {
				return
			}
			fmt.Fprintf(w, "data: %s\n\n", payload)
			flusher.Flush()
		}
	}
}
