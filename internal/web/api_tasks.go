package web

import (
	"net/http"
	"sort"
	"time"

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
	runMsgs, err := s.st.ListTaskRunMessages(id)
	if err != nil {
		fail(w, err)
		return
	}
	working, workingID := "", int64(0)
	if s.team.WorkingOn(id, task.AssigneeID) {
		working, workingID = assignee, task.AssigneeID
	}
	conv := buildConversation(events, runMsgs)
	// textul complet e doar in conversation; copiile din events (istoric) nu il mai poarta
	slim := make([]store.TaskEvent, len(events))
	for i, e := range events {
		e.Body, e.To = "", ""
		slim[i] = e
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"task": task, "events": slim, "assignee": assignee, "stage": stage,
		"conversation": conv, "working": working, "workingAgentId": workingID,
	})
}

// convItem e un element al conversatiei unui task: o comunicare (eveniment cu body)
// sau un mesaj al agentului dintr-un run.
type convItem struct {
	Type  string    `json:"type"` // "event" | "run"
	At    time.Time `json:"at"`
	Kind  string    `json:"kind,omitempty"`
	From  string    `json:"from"`
	To    string    `json:"to,omitempty"`
	Text  string    `json:"text"`
	RunID string    `json:"runId,omitempty"`
}

// maxConvText: lungimea maxima (in caractere) a unui mesaj de run in conversatie (ca la corpul evenimentelor).
const maxConvText = 20000

// buildConversation uneste evenimentele cu body si mesajele din run-uri, sortate dupa
// timp (la egalitate evenimentele inaintea mesajelor); mereu non-nil.
func buildConversation(events []store.TaskEvent, msgs []store.Message) []convItem {
	out := []convItem{}
	// (autor, text) al rapoartelor: un run terminat fara report_done salveaza acelasi text
	// si ca mesaj, si ca eveniment "reported"; il pastram doar pe cel din eveniment
	reported := map[[2]string]bool{}
	for _, e := range events {
		if e.Body != "" {
			out = append(out, convItem{Type: "event", At: e.At, Kind: e.Kind, From: e.Actor, To: e.To, Text: e.Body})
			if e.Kind == "reported" {
				reported[[2]string{e.Actor, e.Body}] = true
			}
		}
	}
	for _, m := range msgs {
		text := m.Text
		if r := []rune(text); len(r) > maxConvText {
			text = string(r[:maxConvText]) + " [...]"
		}
		if reported[[2]string{m.Author, text}] {
			continue
		}
		out = append(out, convItem{Type: "run", At: m.CreatedAt, From: m.Author, Text: text, RunID: m.RunID})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].At.Equal(out[j].At) {
			return out[i].At.Before(out[j].At)
		}
		return out[i].Type == "event" && out[j].Type == "run"
	})
	return out
}
