package web

import (
	"net/http"
	"strconv"

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
	Runs         int           `json:"runs"` // task-urile create de sarcina (si arhivate)
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
	if c, err := s.st.RecurringCounts(r.ID); err == nil {
		v.Runs = c.Total
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
	// trim-ul si limitele titlului / descrierii le face team (RecurringInput.clean), ca pentru tool-urile MCP
	return team.RecurringInput{Title: in.Title, Description: in.Description, AssigneeID: in.AssigneeID, Schedule: in.Schedule,
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

// runView: o rulare din Run history.
type runView struct {
	store.RecurringRun
	Ref      string `json:"ref"`
	Assignee string `json:"assignee"`
}

// recurringHistory: modala Run history (rularile, cea mai noua prima, paginat cu before).
func (s *Server) recurringHistory(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		fail(w, err)
		return
	}
	rec, err := s.st.GetRecurring(id)
	if err != nil {
		fail(w, err)
		return
	}
	before, limit, err := historyPage(r)
	if err != nil {
		fail(w, err)
		return
	}
	runs, more, err := s.st.RecurringHistory(id, before, limit)
	if err != nil {
		fail(w, err)
		return
	}
	counts, err := s.st.RecurringCounts(id)
	if err != nil {
		fail(w, err)
		return
	}
	names := map[int64]string{} // agent -> nume, o singura citire per agent
	out := make([]runView, len(runs))
	for i, run := range runs {
		name, ok := names[run.AssigneeID]
		if !ok {
			if a, err := s.st.GetAgent(run.AssigneeID); err == nil {
				name = a.Name
			}
			names[run.AssigneeID] = name
		}
		out[i] = runView{RecurringRun: run, Ref: run.Ref(), Assignee: name}
	}
	writeJSON(w, http.StatusOK, map[string]any{"recurring": s.recurringView(rec), "counts": counts, "runs": out, "more": more})
}

// historyPage citeste ?before= (id, >= 0) si ?limit= (1..50, implicit 20).
func historyPage(r *http.Request) (int64, int, error) {
	q := r.URL.Query()
	before, limit := int64(0), 20
	if v := q.Get("before"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n < 0 {
			return 0, 0, badRequest("invalid before")
		}
		before = n
	}
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 50 {
			return 0, 0, badRequest("limit must be between 1 and 50")
		}
		limit = n
	}
	return before, limit, nil
}

// recurringFiles: fisierele tuturor rularilor, grupate pe cale (ca tab-ul Files), cu T-N pe versiuni.
func (s *Server) recurringFiles(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		fail(w, err)
		return
	}
	if _, err := s.st.GetRecurring(id); err != nil {
		fail(w, err)
		return
	}
	list, err := s.st.RecurringFiles(id)
	if err != nil {
		fail(w, err)
		return
	}
	refs, err := s.st.RecurringRefs(id)
	if err != nil {
		fail(w, err)
		return
	}
	var mine, theirs []store.TaskFile
	for _, f := range list {
		if f.FromUser() {
			mine = append(mine, f)
		} else {
			theirs = append(theirs, f)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"user": withRefs(groupFiles(mine), refs), "files": withRefs(groupFiles(theirs), refs), "subtasks": []subtaskFiles{}})
}
