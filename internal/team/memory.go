package team

import (
	"errors"
	"fmt"
	"log"
	"sort"
	"strings"

	"hobstack/internal/mcp"
	"hobstack/internal/prompt"
	"hobstack/internal/store"
)

// Memoria proiectului prin MCP: remember / recall / search_memory / forget. Indexul intra in fiecare tura
// (memoryTurn), nu in system prompt, ca sa nu reseteze sesiunile.

const memoryDate = "2006-01-02"

// memoryErr traduce erorile din store in mesaje pentru agent.
func memoryErr(key string, err error) error {
	switch {
	case errors.Is(err, store.ErrMemoryLocked):
		return fmt.Errorf("%s is locked by the user. If it is wrong, tell the user (ask_user or your report).", key)
	case errors.Is(err, store.ErrMemoryFull):
		return fmt.Errorf("the project memory is full (%d entries): update or forget an entry that is no longer needed", store.MaxMemoryEntries)
	}
	return err
}

// lastForget: ultima stergere a intrarii (autor, data, motiv), daca exista.
func (t *Team) lastForget(id int64) (store.MemoryVersion, bool) {
	h, err := t.st.MemoryHistory(id)
	if err != nil {
		return store.MemoryVersion{}, false
	}
	for _, v := range h {
		if v.Action == "forget" {
			return v, true
		}
	}
	return store.MemoryVersion{}, false
}

func forgotText(v store.MemoryVersion) string {
	s := v.AuthorName + " on " + v.At.Local().Format(memoryDate)
	if v.Reason != "" {
		s += ": " + v.Reason
	}
	return s
}

// Remember creeaza sau inlocuieste o intrare. task = "T-N" optional; implicit task-ul run-ului.
func (t *Team) Remember(c mcp.Caller, key, summary, text, task string) (string, error) {
	t.work.Lock()
	defer t.work.Unlock()
	a, err := t.st.GetAgent(c.AgentID)
	if err != nil {
		return "", err
	}
	w := store.MemoryWrite{Key: key, Summary: summary, Body: text, AuthorAgentID: a.ID, AuthorName: a.Name, TaskID: c.TaskID}
	var onTask *store.Task
	if strings.TrimSpace(task) != "" {
		tk, err := t.findTask(c.ProjectID, task)
		if err != nil {
			return "", err
		}
		w.TaskID, onTask = tk.ID, &tk
	} else if c.TaskID != 0 {
		if tk, err := t.st.GetTask(c.TaskID); err == nil {
			onTask = &tk
		}
	}
	m, prev, changed, err := t.st.SetMemory(c.ProjectID, w, false)
	if err != nil {
		k, kerr := store.NormalizeMemoryKey(key)
		if kerr != nil {
			k = key
		}
		return "", memoryErr(k, err)
	}
	if !changed {
		return m.Key + " is already saved with this text.", nil
	}
	if onTask != nil {
		text := fmt.Sprintf("%s saved memory %s: %s", a.Name, m.Key, m.Summary)
		body := "Saved to project memory, " + m.Key + ": " + m.Summary
		if m.Body != "" {
			body += "\n\n" + m.Body
		}
		t.memoryEvent(*onTask, a.Name, text, body)
	}
	t.emit(c.ProjectID, "memory", map[string]int64{"id": m.ID})
	switch {
	case prev == nil:
		return "Saved " + m.Key + ".", nil
	case prev.DeletedAt != nil:
		reply := "Saved " + m.Key + "."
		if v, ok := t.lastForget(m.ID); ok {
			reply += " Note: it was deleted by " + forgotText(v) + "."
		}
		return reply, nil
	}
	return fmt.Sprintf("Updated %s (was: %s).", m.Key, prev.Summary), nil
}

// Recall intoarce intrarea intreaga; pentru o cheie necunoscuta, cele mai apropiate chei.
func (t *Team) Recall(c mcp.Caller, key string) (string, error) {
	k, err := store.NormalizeMemoryKey(key)
	if err != nil {
		return "", err
	}
	m, err := t.st.GetMemoryByKey(c.ProjectID, k)
	if errors.Is(err, store.ErrNotFound) {
		return t.closestKeys(c.ProjectID, k)
	}
	if err != nil {
		return "", err
	}
	if m.DeletedAt != nil {
		reply := k + " was forgotten"
		if v, ok := t.lastForget(m.ID); ok {
			reply += " by " + forgotText(v)
		}
		return reply + ". Its last summary: " + m.Summary, nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s: %s", m.Key, m.Summary)
	if m.Body != "" {
		b.WriteString("\n" + m.Body)
	}
	fmt.Fprintf(&b, "\nSaved by %s on %s", m.AuthorName, m.UpdatedAt.Local().Format(memoryDate))
	if m.TaskRef != "" {
		b.WriteString(" (" + m.TaskRef + ")")
	}
	b.WriteString(".")
	if m.Locked {
		b.WriteString(" Locked by the user.")
	}
	return b.String(), nil
}

// closestKeys: cheile care contin cautarea, apoi cele cu prefixul comun cel mai lung (max 5).
func (t *Team) closestKeys(projectID int64, k string) (string, error) {
	all, err := t.st.ListMemory(projectID, false)
	if err != nil {
		return "", err
	}
	type cand struct {
		key      string
		contains bool
		prefix   int
	}
	var cs []cand
	for _, m := range all {
		p := 0
		for p < len(k) && p < len(m.Key) && k[p] == m.Key[p] {
			p++
		}
		c := cand{m.Key, strings.Contains(m.Key, k) || strings.Contains(k, m.Key), p}
		if c.contains || c.prefix > 0 {
			cs = append(cs, c)
		}
	}
	sort.SliceStable(cs, func(i, j int) bool {
		if cs[i].contains != cs[j].contains {
			return cs[i].contains
		}
		if cs[i].prefix != cs[j].prefix {
			return cs[i].prefix > cs[j].prefix
		}
		return cs[i].key < cs[j].key
	})
	if len(cs) == 0 {
		return "No memory " + k + ".", nil
	}
	var keys []string
	for i, c := range cs {
		if i == 5 {
			break
		}
		keys = append(keys, c.key)
	}
	return "No memory " + k + ". Closest keys: " + strings.Join(keys, ", "), nil
}

// SearchMemory cauta in intrarile active (max 10).
func (t *Team) SearchMemory(c mcp.Caller, query string) (string, error) {
	q := strings.TrimSpace(query)
	if q == "" {
		return "", errors.New("query is empty")
	}
	res, err := t.st.SearchMemory(c.ProjectID, q, 10)
	if err != nil {
		return "", err
	}
	if len(res) == 0 {
		return fmt.Sprintf("No memory matches %q.", q), nil
	}
	lines := make([]string, len(res))
	for i, m := range res {
		lines[i] = "- " + m.Key + ": " + m.Summary
	}
	return strings.Join(lines, "\n"), nil
}

// Forget sterge o intrare (ramane in istoric, cu motivul).
func (t *Team) Forget(c mcp.Caller, key, reason string) (string, error) {
	t.work.Lock()
	defer t.work.Unlock()
	k, err := store.NormalizeMemoryKey(key)
	if err != nil {
		return "", err
	}
	m, err := t.st.GetMemoryByKey(c.ProjectID, k)
	if errors.Is(err, store.ErrNotFound) || (err == nil && m.DeletedAt != nil) {
		return "", fmt.Errorf("%s is not in the project memory", k)
	}
	if err != nil {
		return "", err
	}
	a, err := t.st.GetAgent(c.AgentID)
	if err != nil {
		return "", err
	}
	by := store.MemoryWrite{AuthorAgentID: a.ID, AuthorName: a.Name, TaskID: c.TaskID}
	if err := t.st.ForgetMemory(m.ID, by, reason, false); err != nil {
		return "", memoryErr(k, err)
	}
	if c.TaskID != 0 {
		if tk, err := t.st.GetTask(c.TaskID); err == nil {
			text := fmt.Sprintf("%s forgot memory %s", a.Name, k)
			if r := strings.TrimSpace(reason); r != "" {
				text += ": " + r
			}
			t.memoryEvent(tk, a.Name, text, text)
		}
	}
	t.emit(c.ProjectID, "memory", map[string]int64{"id": m.ID})
	return "Forgot " + k + ".", nil
}

// memoryEvent: pasul "memory" pe task (cu text, deci si in Conversation) si evenimentul "task" pentru modala deschisa.
func (t *Team) memoryEvent(task store.Task, actor, text, body string) {
	t.taskMsg(task, "memory", actor, "", text, body)
	t.emit(task.ProjectID, "task", task)
}

// memoryTurn: blocul "Project memory" pentru tura; gol fara memorie sau la eroare (run-ul merge oricum).
func (t *Team) memoryTurn(projectID int64, withTools bool) string {
	list, err := t.st.ListMemory(projectID, false)
	if err != nil {
		log.Printf("project %d: memory index: %v", projectID, err)
		return ""
	}
	return prompt.MemoryIndex(list, withTools)
}
