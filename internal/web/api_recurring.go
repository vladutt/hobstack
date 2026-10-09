package web

import (
	"net/http"
	"strings"

	"hobstack/internal/schedule"
	"hobstack/internal/store"
	"hobstack/internal/team"
)

// recurringView: sarcina recurenta cu ce mai trebuie aratat in UI.
type recurringView struct {
	store.RecurringTask
	ScheduleText string        `json:"scheduleText"`
	Assignee     string        `json:"assignee"` // "" = prima etapa
	LastTask     *lastTaskView `json:"lastTask"`
}

type lastTaskView struct {
	ID     int64  `json:"id"`
	Number int    `json:"number"`
	Status string `json:"status"`
}

func (s *Server) recurringView(r store.RecurringTask) recurringView {
	v := recurringView{RecurringTask: r, ScheduleText: r.Schedule.String()}
	if r.AssigneeID != nil {
		if a, err := s.st.GetAgent(*r.AssigneeID); err == nil {
			v.Assignee = a.Name
		}
	}
	if r.LastTaskID != nil {
		if t, err := s.st.GetTask(*r.LastTaskID); err == nil {
			v.LastTask = &lastTaskView{ID: t.ID, Number: t.Number, Status: t.Status}
		}
	}
	return v
}

// recurringViews: lista proiectului pentru detaliu; mereu non-nil.
func (s *Server) recurringViews(projectID int64) ([]recurringView, error) {
	list, err := s.st.ListRecurring(projectID)
	if err != nil {
		return nil, err
	}
	out := make([]recurringView, len(list))
	for i, r := range list {
		out[i] = s.recurringView(r)
	}
	return out, nil
}

// recurringInput citeste si valideaza corpul comun POST/PUT.
func recurringInput(r *http.Request) (team.RecurringInput, error) {
	var in struct {
		Title       string        `json:"title"`
		Description string        `json:"description"`
		AssigneeID  int64         `json:"assigneeId"`
		Schedule    schedule.Spec `json:"schedule"`
		AutoAccept  bool          `json:"autoAccept"`
		Enabled     bool          `json:"enabled"`
	}
	if err := decode(r, &in); err != nil {
		return team.RecurringInput{}, err
	}
	title := strings.TrimSpace(in.Title)
	if title == "" {
		return team.RecurringInput{}, badRequest("title is required")
	}
	desc := strings.TrimSpace(in.Description)
	for _, err := range []error{maxLen("title", title, 200), maxLen("description", desc, 10000)} {
		if err != nil {
			return team.RecurringInput{}, err
		}
	}
	return team.RecurringInput{Title: title, Description: desc, AssigneeID: in.AssigneeID, Schedule: in.Schedule,
		AutoAccept: in.AutoAccept, Enabled: in.Enabled}, nil
}

func (s *Server) createRecurring(w http.ResponseWriter, r *http.Request) {
	pid, err := pathID(r)
	if err != nil {
		fail(w, err)
		return
	}
	if _, err := s.st.GetProject(pid); err != nil {
		fail(w, err)
		return
	}
	in, err := recurringInput(r)
	if err != nil {
		fail(w, err)
		return
	}
	rec, err := s.team.CreateRecurring(pid, in)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, s.recurringView(rec))
}

func (s *Server) updateRecurring(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		fail(w, err)
		return
	}
	in, err := recurringInput(r)
	if err != nil {
		fail(w, err)
		return
	}
	rec, err := s.team.UpdateRecurring(id, in)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, s.recurringView(rec))
}

func (s *Server) deleteRecurring(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		fail(w, err)
		return
	}
	if err := s.team.DeleteRecurring(id); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) runRecurring(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		fail(w, err)
		return
	}
	task, err := s.team.RunRecurringNow(id)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, task)
}
