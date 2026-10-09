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

// maxOtherLen: lungimea maxima (in caractere) a textului liber "other" dintr-un raspuns.
const maxOtherLen = 300

// decideWithAnswers salveaza raspunsurile utilizatorului la intrebarile task-ului, apoi
// aplica decizia (decideAsUser). Accept: agentul care a pus intrebarile afla raspunsurile
// (si nota) intr-o nota in coada. Send back: raspunsurile intra in feedback, dupa nota.
// Se apeleaza cu t.work blocat, dupa verificarea ca decizia e a utilizatorului.
func (t *Team) decideWithAnswers(task store.Task, agents []store.Agent, accept bool, feedback string, answers []store.DecisionAnswer) error {
	note := strings.TrimSpace(feedback)
	// raspunsurile goale (fara alegeri si fara other) nu conteaza: intrebarea ramane deschisa
	var given []store.DecisionAnswer
	for _, a := range answers {
		a.Other = strings.TrimSpace(a.Other)
		if len([]rune(a.Other)) > maxOtherLen {
			return fmt.Errorf("%w: the other text of question %d is longer than %d characters", store.ErrBadAnswer, a.ID, maxOtherLen)
		}
		if len(a.Choices) > 0 || a.Other != "" {
			given = append(given, a)
		}
	}
	if len(given) == 0 {
		// fara raspunsuri: exact drumul de dinainte, plus nota catre cel care a intrebat la accept
		if err := t.decideAsUser(task, agents, accept, feedback, 0, nil); err != nil {
			return err
		}
		// istoricul pastreaza nota doar cand a ajuns la cineva (task cu intrebari)
		if accept && note != "" && t.noteAskers(task, agents, "", note) {
			t.taskMsg(task, "decided", actorUser, agentName(agents, task.AssigneeID), "You left a note", noteLine(note))
		}
		return nil
	}
	if err := t.st.AnswerDecisions(task.ID, given); err != nil {
		return err
	}
	ds, err := t.st.ListDecisions(task.ID)
	if err != nil {
		return err
	}
	// textul: intrebarile raspunse acum + cele ramase fara raspuns (nu cele raspunse inainte)
	answeredNow := map[int64]bool{}
	for _, a := range given {
		answeredNow[a.ID] = true
	}
	var relevant []store.Decision
	for _, d := range ds {
		if answeredNow[d.ID] || d.AnsweredAt == nil {
			relevant = append(relevant, d)
		}
	}
	text := answersText(relevant)
	t.taskMsg(task, "decided", actorUser, agentName(agents, task.AssigneeID), "You answered the decisions", joinNonEmpty(text, noteLine(note)))
	if !accept {
		return t.decideAsUser(task, agents, false, joinNonEmpty(note, text), 0, nil)
	}
	if err := t.decideAsUser(task, agents, true, feedback, 0, nil); err != nil {
		return err
	}
	t.noteAskers(task, agents, text, note)
	return nil
}

// noteAskers trimite agentilor care au pus intrebari pe task (asked_by) o nota in coada:
// "The user accepted T-x." + raspunsuri + "Note: <nota>". Nu trimite nimic daca nu e nimic de spus.
// Intoarce true daca a trimis cel putin o nota.
func (t *Team) noteAskers(task store.Task, agents []store.Agent, text, note string) bool {
	if text == "" && note == "" {
		return false
	}
	ds, err := t.st.ListDecisions(task.ID)
	if err != nil {
		return false
	}
	msg := clip(strings.Join(nonEmpty("The user accepted "+task.Ref()+".", text, noteLine(note)), "\n"))
	seen, sent := map[string]bool{}, false
	for _, d := range ds {
		if seen[d.AskedBy] {
			continue
		}
		seen[d.AskedBy] = true
		for _, a := range agents {
			if a.Name == d.AskedBy {
				t.note(task.ProjectID, a.ID, msg, false)
				sent = true
				break
			}
		}
	}
	if sent {
		t.Kick(task.ProjectID)
	}
	return sent
}

// answersText: cate o linie pe intrebare, "Q<n> <intrebare>: <alegeri>[; other: <text>]";
// fara alegeri si fara other -> "no answer".
func answersText(ds []store.Decision) string {
	lines := make([]string, 0, len(ds))
	for _, d := range ds {
		parts := nonEmpty(strings.Join(d.Answer, ", "))
		if d.Other != "" {
			parts = append(parts, "other: "+d.Other)
		}
		ans := strings.Join(parts, "; ")
		if ans == "" {
			ans = "no answer"
		}
		lines = append(lines, fmt.Sprintf("Q%d %s: %s", d.Position, d.Question, ans))
	}
	return strings.Join(lines, "\n")
}

func noteLine(note string) string {
	if note == "" {
		return ""
	}
	return "Note: " + note
}

// joinNonEmpty uneste partile nevide cu o linie goala intre ele.
func joinNonEmpty(parts ...string) string {
	return strings.Join(nonEmpty(parts...), "\n\n")
}

func nonEmpty(parts ...string) []string {
	out := []string{}
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
