package web

import (
	"net/http"
	"os"
	"path/filepath"
	"time"

	"foreman/internal/config"
	"foreman/internal/store"
)

// fileVersion: o predare mai veche a aceleiasi cai.
type fileVersion struct {
	ID        int64     `json:"id"`
	Size      int64     `json:"size"`
	AgentName string    `json:"agentName"`
	CreatedAt time.Time `json:"createdAt"`
}

// fileGroup: ultima versiune a unei cai, cu versiunile mai vechi.
type fileGroup struct {
	store.TaskFile
	Versions []fileVersion `json:"versions"`
}

// groupFiles: lista vine cea mai noua prima; prima aparitie a unei cai e versiunea curenta.
// Ordinea grupurilor = ordinea versiunilor curente (cel mai nou primul).
func groupFiles(list []store.TaskFile) []fileGroup {
	out := []fileGroup{}
	at := map[string]int{} // cale -> index in out
	for _, f := range list {
		if i, ok := at[f.Path]; ok {
			out[i].Versions = append(out[i].Versions, fileVersion{ID: f.ID, Size: f.Size, AgentName: f.AgentName, CreatedAt: f.CreatedAt})
			continue
		}
		at[f.Path] = len(out)
		out = append(out, fileGroup{TaskFile: f, Versions: []fileVersion{}})
	}
	return out
}

type subtaskFiles struct {
	TaskID int64       `json:"taskId"`
	Ref    string      `json:"ref"`
	Title  string      `json:"title"`
	Files  []fileGroup `json:"files"`
}

// getTaskFiles: tab-ul Files al modalei (fisierele task-ului si ale sub-task-urilor).
func (s *Server) getTaskFiles(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		fail(w, err)
		return
	}
	if _, err := s.st.GetTask(id); err != nil {
		fail(w, err)
		return
	}
	own, err := s.st.ListTaskFiles(id)
	if err != nil {
		fail(w, err)
		return
	}
	desc, err := s.st.ListDescendantFiles(id)
	if err != nil {
		fail(w, err)
		return
	}
	// desc vine ordonat dupa task: grupam pe task pastrand ordinea
	subs := []subtaskFiles{}
	var cur []store.TaskFile
	flush := func() {
		if len(cur) == 0 {
			return
		}
		sub := subtaskFiles{TaskID: cur[0].TaskID, Files: groupFiles(cur)}
		if t, err := s.st.GetTask(sub.TaskID); err == nil {
			sub.Ref, sub.Title = t.Ref(), t.Title
		}
		subs = append(subs, sub)
		cur = nil
	}
	for _, f := range desc {
		if len(cur) > 0 && cur[0].TaskID != f.TaskID {
			flush()
		}
		cur = append(cur, f)
	}
	flush()
	writeJSON(w, http.StatusOK, map[string]any{"files": groupFiles(own), "subtasks": subs})
}

// serveTaskFile: un fisier predat; ?download=1 forteaza descarcarea.
func (s *Server) serveTaskFile(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		fail(w, err)
		return
	}
	tf, err := s.st.GetTaskFile(id)
	if err != nil {
		fail(w, err)
		return
	}
	f, err := os.Open(filepath.Join(config.TaskFilesDir(s.dirs.Projects, tf.ProjectID, tf.TaskID), tf.Stored))
	if err != nil {
		if os.IsNotExist(err) {
			writeErr(w, http.StatusNotFound, "the file is missing from disk")
			return
		}
		fail(w, err)
		return
	}
	defer f.Close()
	serveStored(w, r, f, tf.Name, tf.Mime, tf.CreatedAt, r.URL.Query().Get("download") == "1")
}
