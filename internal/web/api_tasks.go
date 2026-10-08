package web

import (
	"net/http"
	"sort"
	"strings"
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
	changesCount, err := s.st.CountTaskChanges(id)
	if err != nil {
		fail(w, err)
		return
	}
	runMsgs, err := s.st.ListTaskRunMessages(id)
	if err != nil {
		fail(w, err)
		return
	}
	usage, err := s.st.TaskUsage(id)
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
	// sarcina recurenta care l-a creat, cat timp mai exista (pentru linkul din modala)
	var recurring any
	if task.RecurringID != nil {
		if rec, err := s.st.GetRecurring(*task.RecurringID); err == nil {
			recurring = map[string]any{"id": rec.ID, "title": rec.Title}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"recurring": recurring,
		"task":      task, "events": slim, "assignee": assignee, "stage": stage,
		"conversation": conv, "working": working, "workingAgentId": workingID,
		"changesCount": changesCount, // fingerprint ieftin pentru tab-ul Changes
		"usage":        usage,        // run-uri, cost, tokeni si timp de lucru (coloana din dreapta a modalei)

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

// fileChangeView: un fisier modificat de un agent intr-un run (modificarile aceluiasi fisier unite).
type fileChangeView struct {
	Path      string       `json:"path"`
	Kind      string       `json:"kind"` // "create" daca prima modificare din run a creat fisierul, altfel "edit"
	Added     int          `json:"added"`
	Removed   int          `json:"removed"`
	Truncated bool         `json:"truncated"`
	Hunks     []store.Hunk `json:"hunks"`
}

// commandView: o comanda Bash a run-ului si daca a reusit.
type commandView struct {
	Text string `json:"text"`
	OK   bool   `json:"ok"`
}

// runChangesView: ce a facut un run pe un task.
type runChangesView struct {
	RunID    string           `json:"runId"`
	Agent    string           `json:"agent"`
	AgentID  *int64           `json:"agentId"`
	At       time.Time        `json:"at"`
	Files    []fileChangeView `json:"files"`
	Commands []commandView    `json:"commands"`
}

// getTaskChanges intoarce modificarile task-ului grupate pe run (in ordinea primei aparitii).
func (s *Server) getTaskChanges(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		fail(w, err)
		return
	}
	if _, err := s.st.GetTask(id); err != nil {
		fail(w, err)
		return
	}
	changes, err := s.st.ListTaskChanges(id)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"runs": groupChanges(changes)})
}

// getTaskFlow intoarce arborele de delegare al task-ului (tab-ul Flow).
func (s *Server) getTaskFlow(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		fail(w, err)
		return
	}
	f, err := s.st.TaskFlow(id)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, f)
}

// groupChanges grupeaza pe run_id si uneste modificarile aceluiasi fisier; mereu non-nil.
func groupChanges(changes []store.TaskChange) []runChangesView {
	runs := []runChangesView{}
	runIdx := map[string]int{}
	fileIdx := map[string]int{} // cheie: run + cale
	for _, c := range changes {
		i, ok := runIdx[c.RunID]
		if !ok {
			i = len(runs)
			runIdx[c.RunID] = i
			runs = append(runs, runChangesView{RunID: c.RunID, Agent: c.AgentName, AgentID: c.AgentID, At: c.At,
				Files: []fileChangeView{}, Commands: []commandView{}})
		}
		run := &runs[i]
		if c.Kind == "command" {
			run.Commands = append(run.Commands, commandView{Text: c.Command, OK: c.OK})
			continue
		}
		key := c.RunID + "\x00" + c.Path
		j, ok := fileIdx[key]
		if !ok {
			j = len(run.Files)
			fileIdx[key] = j
			kind := "edit"
			if c.Kind == "create" {
				kind = "create"
			}
			run.Files = append(run.Files, fileChangeView{Path: c.Path, Kind: kind, Hunks: []store.Hunk{}})
		}
		f := &run.Files[j]
		f.Truncated = f.Truncated || c.Truncated
		for _, h := range c.Patch {
			f.Hunks = append(f.Hunks, h)
			for _, l := range h.Lines {
				if strings.HasPrefix(l, "+") {
					f.Added++
				} else if strings.HasPrefix(l, "-") {
					f.Removed++
				}
			}
		}
	}
	return runs
}
