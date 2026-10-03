package store

import (
	"errors"
	"sort"
	"time"
)

// FlowStep e un pas din drumul unui task (un eveniment din istoric).
type FlowStep struct {
	At    time.Time `json:"at"`
	Kind  string    `json:"kind"`
	Actor string    `json:"actor"`
	To    string    `json:"to"`
	Text  string    `json:"text"`
	Body  string    `json:"body"`
}

// FlowNode e un task din arborele de delegare, cu pasii si sub-task-urile lui.
type FlowNode struct {
	ID        int64      `json:"id"`
	Number    int        `json:"number"`
	Title     string     `json:"title"`
	Status    string     `json:"status"`
	Assignee  string     `json:"assignee"`
	CreatedAt time.Time  `json:"createdAt"`
	DoneAt    *time.Time `json:"doneAt"` // ultimul "accepted"/"user_accepted", doar pentru Done
	Steps     []FlowStep `json:"steps"`
	Children  []FlowNode `json:"children"`
}

// Flow e arborele task-ului cerut, pornit de la radacina lui.
type Flow struct {
	FocusID int64    `json:"focusId"`
	Root    FlowNode `json:"root"`
}

// TaskFlow construieste arborele din care face parte task-ul: urca pe
// parent_task_id pana la radacina, apoi coboara prin sub-task-uri. Un ciclu
// (date stricate) sau un parinte sters opreste urcarea.
func (s *Store) TaskFlow(taskID int64) (Flow, error) {
	task, err := s.GetTask(taskID)
	if err != nil {
		return Flow{}, err
	}
	root := task
	seen := map[int64]bool{task.ID: true}
	for root.ParentTaskID != nil {
		p, err := s.GetTask(*root.ParentTaskID)
		if errors.Is(err, ErrNotFound) {
			break
		}
		if err != nil {
			return Flow{}, err
		}
		if seen[p.ID] {
			break
		}
		seen[p.ID] = true
		root = p
	}
	agents, err := s.ListAgents(task.ProjectID)
	if err != nil {
		return Flow{}, err
	}
	names := make(map[int64]string, len(agents))
	for _, a := range agents {
		names[a.ID] = a.Name
	}
	node, err := s.flowNode(root, names, map[int64]bool{})
	if err != nil {
		return Flow{}, err
	}
	return Flow{FocusID: taskID, Root: node}, nil
}

// flowNode: nodul unui task si, recursiv, al sub-task-urilor; visited opreste ciclurile.
func (s *Store) flowNode(t Task, names map[int64]string, visited map[int64]bool) (FlowNode, error) {
	visited[t.ID] = true
	n := FlowNode{ID: t.ID, Number: t.Number, Title: t.Title, Status: t.Status, Assignee: names[t.AssigneeID],
		CreatedAt: t.CreatedAt, Steps: []FlowStep{}, Children: []FlowNode{}}
	events, err := s.ListTaskEvents(t.ID)
	if err != nil {
		return FlowNode{}, err
	}
	for _, e := range events {
		n.Steps = append(n.Steps, FlowStep{At: e.At, Kind: e.Kind, Actor: e.Actor, To: e.To, Text: e.Text, Body: e.Body})
	}
	if len(n.Steps) == 0 { // task vechi, fara istoric: un pas sintetic (ca in getTask)
		n.Steps = append(n.Steps, FlowStep{At: t.CreatedAt, Kind: "created", Text: "Created"})
	}
	// dupa ora; la egalitate ramane ordinea inregistrarii ("requested" are ora mesajului)
	sort.SliceStable(n.Steps, func(i, j int) bool { return n.Steps[i].At.Before(n.Steps[j].At) })
	if t.Status == StatusDone {
		for i := len(n.Steps) - 1; i >= 0; i-- {
			if k := n.Steps[i].Kind; k == "accepted" || k == "user_accepted" {
				at := n.Steps[i].At
				n.DoneAt = &at
				break
			}
		}
	}
	subs, err := s.Subtasks(t.ID)
	if err != nil {
		return FlowNode{}, err
	}
	for _, sub := range subs {
		if visited[sub.ID] {
			continue
		}
		child, err := s.flowNode(sub, names, visited)
		if err != nil {
			return FlowNode{}, err
		}
		n.Children = append(n.Children, child)
	}
	return n, nil
}
