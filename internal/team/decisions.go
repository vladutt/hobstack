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
	t.note(boss.ProjectID, boss.ID, clip(task.Ref()+": "+decisionsList(qs)), false)
	t.Kick(boss.ProjectID)
}

// decisionsList: "Decisions for the user:" + cate o linie "<n>. <intrebare> (<optiuni>)".
func decisionsList(qs []store.Decision) string {
	var b strings.Builder
	b.WriteString("Decisions for the user:")
	for i, q := range qs {
		fmt.Fprintf(&b, "\n%d. %s (%s)", i+1, q.Question, strings.Join(q.Options, " / "))
	}
	return b.String()
}

// openDecisionsText: intrebarile fara raspuns ale task-ului, pentru nota liderului care decide
// dupa reviewer sau dupa ultima etapa (la raport nu decidea el). Gol daca nu sunt.
func (t *Team) openDecisionsText(taskID int64) string {
	ds, err := t.st.ListDecisions(taskID)
	if err != nil {
		return ""
	}
	var open []store.Decision
	for _, d := range ds {
		if d.AnsweredAt == nil {
			open = append(open, d)
		}
	}
	if len(open) == 0 {
		return ""
	}
	return decisionsList(open)
}

// withOpenDecisions: nota liderului (taiata cu clip) plus intrebarile deschise pe un rand nou.
// Intrebarile se adauga dupa clip, ca un raport lung sa nu le taie (sunt limitate in MCP).
func (t *Team) withOpenDecisions(text string, taskID int64) string {
	text = clip(text)
	if d := t.openDecisionsText(taskID); d != "" {
		return text + "\n" + d
	}
	return text
}

// maxOtherLen: lungimea maxima (in caractere) a textului liber "other" dintr-un raspuns.
const maxOtherLen = 300

// decideWithAnswers salveaza raspunsurile utilizatorului la intrebarile task-ului, apoi
// aplica decizia (decideAsUser). Accept: fiecare agent care a pus intrebari afla, intr-o
// nota in coada, raspunsurile la intrebarile lui (si nota). Send back: raspunsurile intra
// in feedback, dupa nota. Evenimentul "decided" se scrie doar dupa o decizie reusita, dupa
// user_accepted / sent_back. Se apeleaza cu t.work blocat, dupa verificarea ca decizia e a utilizatorului.
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
	to := agentName(agents, task.AssigneeID)
	if len(given) == 0 {
		// fara raspunsuri: exact drumul de dinainte, plus nota catre cei care au intrebat la accept
		if err := t.decideAsUser(task, agents, accept, feedback, 0, nil); err != nil {
			return err
		}
		if !accept || note == "" {
			return nil
		}
		ds, err := t.st.ListDecisions(task.ID)
		if err != nil {
			return err
		}
		// istoricul pastreaza nota doar cand a ajuns la cineva
		if who, ok := t.noteOnlyAccept(task, agents, ds, note); ok {
			t.taskMsg(task, "decided", actorUser, who, "You left a note", noteLine(note))
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
	// intrebarile de spus: cele raspunse acum + cele ramase fara raspuns (nu cele raspunse inainte)
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
	if !accept {
		if err := t.decideAsUser(task, agents, false, joinNonEmpty(note, text), 0, nil); err != nil {
			return err
		}
	} else {
		if err := t.decideAsUser(task, agents, true, feedback, 0, nil); err != nil {
			return err
		}
	}
	t.taskMsg(task, "decided", actorUser, to, "You answered the decisions", joinNonEmpty(text, noteLine(note)))
	if accept {
		t.noteAskers(task, agents, relevant, note)
	}
	return nil
}

// noteOnlyAccept: la un accept doar cu nota, nota ajunge la agentii cu intrebari ramase fara
// raspuns (cu liniile lor "no answer"); daca nu sunt, doar la ultimul agent care a intrebat.
// Fara intrebari (sau cel care a intrebat nu mai exista), nota merge la agentul care a lasat
// punctele pentru utilizator (TodosBy, altfel asignatul): acolo scrie utilizatorul valorile cerute.
// Intoarce destinatarul pentru istoric si daca nota a ajuns la cineva.
func (t *Team) noteOnlyAccept(task store.Task, agents []store.Agent, ds []store.Decision, note string) (string, bool) {
	var open []store.Decision
	for _, d := range ds {
		if d.AnsweredAt == nil {
			open = append(open, d)
		}
	}
	to := agentName(agents, task.AssigneeID)
	if len(open) > 0 {
		if t.noteAskers(task, agents, open, note) {
			return to, true
		}
	} else if len(ds) > 0 {
		last := ds[0]
		for _, d := range ds[1:] {
			if !d.AskedAt.Before(last.AskedAt) {
				last = d
			}
		}
		// fara linii de intrebari: raspunsurile le-a aflat deja
		if t.noteAsker(task, agents, last.AskedBy, "", note) {
			t.Kick(task.ProjectID)
			return to, true
		}
	}
	a, err := t.TodosBy(task)
	if err != nil || !t.noteAsker(task, agents, a.Name, "", note) {
		return "", false
	}
	t.Kick(task.ProjectID)
	return a.Name, true
}

// noteAskers trimite fiecarui agent din qs (asked_by) o nota in coada cu raspunsurile la
// intrebarile lui din qs si nota utilizatorului. Intoarce true daca a trimis ceva.
func (t *Team) noteAskers(task store.Task, agents []store.Agent, qs []store.Decision, note string) bool {
	var order []string
	byAsker := map[string][]store.Decision{}
	for _, d := range qs {
		if _, ok := byAsker[d.AskedBy]; !ok {
			order = append(order, d.AskedBy)
		}
		byAsker[d.AskedBy] = append(byAsker[d.AskedBy], d)
	}
	sent := false
	for _, name := range order {
		if t.noteAsker(task, agents, name, answersText(byAsker[name]), note) {
			sent = true
		}
	}
	if sent {
		t.Kick(task.ProjectID)
	}
	return sent
}

// noteAsker pune in coada agentului cu numele dat: "The user accepted T-x." + text + "Note: <nota>".
// Nu trimite nimic daca nu e nimic de spus sau agentul nu (mai) exista.
func (t *Team) noteAsker(task store.Task, agents []store.Agent, name, text, note string) bool {
	if text == "" && note == "" {
		return false
	}
	for _, a := range agents {
		if a.Name == name {
			t.note(task.ProjectID, a.ID, clip(strings.Join(nonEmpty("The user accepted "+task.Ref()+".", text, noteLine(note)), "\n")), false)
			return true
		}
	}
	return false
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
