package web

import (
	"net/http"

	"foreman/internal/store"
)

// Butoanele de pe task-urile blocate: utilizatorul decide.
func (s *Server) acceptTask(w http.ResponseWriter, r *http.Request) {
	s.taskAction(w, r, s.team.AcceptBlocked)
}

func (s *Server) unblockTask(w http.ResponseWriter, r *http.Request) {
	s.taskAction(w, r, s.team.Unblock)
}

// nudgeTask impinge manual agentul de care asteapta un task blocat.
func (s *Server) nudgeTask(w http.ResponseWriter, r *http.Request) {
	s.taskAction(w, r, func(id int64) error { return s.team.Nudge(id, s.team.Now()) })
}

func (s *Server) taskAction(w http.ResponseWriter, r *http.Request, action func(int64) error) {
	id, err := pathID(r)
	if err != nil {
		fail(w, err)
		return
	}
	if err := action(id); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// getTask intoarce task-ul cu istoricul lui, numele asignatului si al etapei curente.
func (s *Server) getTask(w http.ResponseWriter, r *http.Request) {
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
	events, err := s.st.ListTaskEvents(id)
	if err != nil {
		fail(w, err)
		return
	}
	if len(events) == 0 { // task vechi, fara istoric: un eveniment sintetic (nesalvat)
		events = []store.TaskEvent{{TaskID: id, At: task.CreatedAt, Kind: "created", Text: "Created"}}
	}
	assignee := ""
	if a, err := s.st.GetAgent(task.AssigneeID); err == nil {
		assignee = a.Name
	}
	stage := ""
	if task.StageID != nil {
		stages, err := s.st.ListStages(task.ProjectID)
		if err != nil {
			fail(w, err)
			return
		}
		for _, st := range stages {
			if st.ID == *task.StageID {
				stage = st.Name
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"task": task, "events": events, "assignee": assignee, "stage": stage})
}
