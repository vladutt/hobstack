package team

import (
	"fmt"
	"strings"

	"foreman/internal/mcp"
	"foreman/internal/store"
)

// AskDecisions salveaza intrebarile cu optiuni date la report_done. Limitele sunt validate in MCP.
// Cand task-ul nu il decide utilizatorul, ci un agent (reviewerul sau liderul), intrebarile ajung
// la el intr-o nota separata, "Decisions for the user: ...", trimisa imediat dupa nota de raport.
func (t *Team) AskDecisions(c mcp.Caller, ref string, qs []mcp.DecisionInput) (string, error) {
	t.work.Lock()
	defer t.work.Unlock()
	task, a, err := t.todoTask(c, ref)
	if err != nil {
		return "", err
	}
	rows := make([]store.Decision, len(qs))
	for i, q := range qs {
		rows[i] = store.Decision{Question: strings.TrimSpace(q.Question), Options: q.Options, Multi: q.Multi}
	}
	if err := t.st.SetDecisions(task.ID, a.Name, rows); err != nil {
		return "", err
	}
	t.emit(c.ProjectID, "task", task)
	t.noteDecisions(task, a, rows)
	return fmt.Sprintf("%d %s waiting for the user.", len(qs), plural(len(qs), "decision", "decisions")), nil
}

// noteDecisions: daca task-ul asteapta un agent (nu utilizatorul), il anunta cu intrebarile.
func (t *Team) noteDecisions(task store.Task, from store.Agent, qs []store.Decision) {
	agents, err := t.st.ListAgents(task.ProjectID)
	if err != nil {
		return
	}
	// task-ul proaspat din baza: report_done i-a schimbat statusul
	if fresh, err := t.st.GetTask(task.ID); err == nil {
		task = fresh
	}
	id := decisionOwnerIn(task, agents, t.autonomyOf(task.ProjectID))
	boss, ok := store.FindAgent(agents, id)
	if id == 0 || !ok || boss.ID == from.ID {
		return
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s: Decisions for the user:", task.Ref())
	for i, q := range qs {
		fmt.Fprintf(&b, "\n%d. %s (%s)", i+1, q.Question, strings.Join(q.Options, " / "))
	}
	t.note(boss.ProjectID, boss.ID, clip(b.String()), false)
	t.Kick(boss.ProjectID)
}
