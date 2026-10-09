package web

import (
	"errors"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"foreman/internal/config"
	"foreman/internal/store"
	"foreman/internal/taskfiles"
	"foreman/internal/team"
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
	// fisierele utilizatorului ("From you") separat de livrabilele agentilor
	var mine, theirs []store.TaskFile
	for _, f := range own {
		if f.FromUser() {
			mine = append(mine, f)
		} else {
			theirs = append(theirs, f)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"user": groupFiles(mine), "files": groupFiles(theirs), "subtasks": subs})
}

// stageUploads scrie fisierele urcate intr-un folder de staging din task-files (acelasi disc,
// ca team sa le poata muta cu rename). Apelantul sterge folderul cu os.RemoveAll dupa.
func (s *Server) stageUploads(projectID int64, files []*multipart.FileHeader) (string, []team.StagedFile, error) {
	parent := filepath.Join(s.dirs.Projects, strconv.FormatInt(projectID, 10), "task-files")
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return "", nil, err
	}
	dir, err := os.MkdirTemp(parent, ".staging-")
	if err != nil {
		return "", nil, err
	}
	out := make([]team.StagedFile, 0, len(files))
	for _, fh := range files {
		name := store.CleanFileName(fh.Filename)
		src, err := fh.Open()
		if err != nil {
			return dir, nil, err
		}
		c, err := taskfiles.Save(src, name, dir)
		src.Close()
		if err != nil {
			return dir, nil, err
		}
		out = append(out, team.StagedFile{Dir: dir, Stored: c.Stored, Name: c.Name, Mime: normalizeMime(c.Mime), Size: c.Size})
	}
	return dir, out, nil
}

// removeStaging sterge folderul de staging (ce n-a fost mutat in task).
func removeStaging(dir string) {
	if dir == "" {
		return
	}
	if err := os.RemoveAll(dir); err != nil {
		log.Printf("task files: cannot remove staging %s: %v", dir, err)
	}
}

// uploadTaskFiles: utilizatorul pune fisiere pe un task existent (tab-ul Files din modala).
func (s *Server) uploadTaskFiles(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		fail(w, err)
		return
	}
	task, err := s.st.GetTask(id)
	if err != nil {
		fail(w, err)
		return
	}
	// verificat si aici, ca un task sters sa nu primeasca 200 MB degeaba (team verifica din nou sub lock)
	if task.Status == store.StatusArchived {
		writeErr(w, http.StatusConflict, task.Ref()+" was deleted; restore it first.")
		return
	}
	files, err := s.readUploads(w, r)
	if err != nil {
		fail(w, err)
		return
	}
	if len(files) == 0 {
		fail(w, badRequest("choose at least one file"))
		return
	}
	dir, staged, err := s.stageUploads(task.ProjectID, files)
	defer removeStaging(dir)
	if err != nil {
		fail(w, err)
		return
	}
	saved, err := s.team.AddUserFiles(id, staged)
	if errors.Is(err, team.ErrArchived) {
		writeErr(w, http.StatusConflict, err.Error())
		return
	}
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, saved)
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
