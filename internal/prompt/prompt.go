// Package prompt construieste tot ce primeste claude la un run: system prompt-ul
// agentului, mesajul turei, permisiunile si config-ul MCP. Sunt doar functii
// pure, fara acces la baza de date, ca sa fie usor de testat.
package prompt

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"foreman/internal/store"
)

// Links descrie locul agentului in echipa; intra in system prompt.
type Links struct {
	BossName     string // "" = raporteaza utilizatorului
	IsLead       bool
	IsReviewer   bool
	CanAddAgents bool // lider sau radacina: are add_agent / remove_agent
}

// LinksFor calculeaza Links din lista echipei. Merge si pentru un agent nou
// (ID 0): atunci doar BossName poate fi setat.
func LinksFor(a store.Agent, team []store.Agent) Links {
	r := store.RolesOf(a, team)
	l := Links{IsLead: r.IsLead, IsReviewer: r.IsReviewer, CanAddAgents: r.IsLead || r.IsRoot}
	if a.ParentID != nil {
		if boss, ok := store.FindAgent(team, *a.ParentID); ok {
			l.BossName = boss.Name
		}
	}
	return l
}

// Member e un rand din "Echipa ta directa".
type Member struct {
	Name, Role, Responsibilities, Status, Folder string
}

// OpenTask e un task primit de un lider si inca in lucru.
type OpenTask struct {
	Ref, Title  string
	Done, Total int // sub-task-uri gata / total
}

// TurnContext e starea care se schimba de la o tura la alta.
type TurnContext struct {
	Direct    []Member   // doar pentru lideri: agentii direct sub el
	Reviewees []string   // numele agentilor pe care ii verifica
	OpenTasks []OpenTask // task-urile primite de lider, in lucru (fara cel curent)
	Stage     string     // linia etapei pentru un run pe un task dintr-o etapa (coloane proprii)
	Stages    string     // pentru lideri, intr-un proiect cu etape: etapele si agentii lor
}

const howAsk = `If you need a decision, a permission or information that only the user can give, call mcp__team__ask_user with a clear question (and short options if it is a choice), then stop. Do not guess, and do not just end your message with a question.`

const howAskWait = `Do not call report_done while you wait for the answer.`

// howAuto inlocuieste howAsk / howAskWait cand proiectul e in modul full autonomy.
const howAuto = `Full autonomy is on: the user will not answer questions. Make reasonable decisions yourself and state your assumptions in your report.`

const howLead = `You are a lead: you have a direct team, listed in the turn message. Break the work into small, clear, verifiable tasks ` +
	`and create them with mcp__team__create_task, only for agents on your direct team, choosing by each one's "Takes" field. ` +
	`Do not do their work yourself. When you receive results, check them (you can read the changed files) and call ` +
	`mcp__team__accept_task or mcp__team__send_back with concrete feedback. Use mcp__team__list_tasks to see the status. ` +
	`Do not use any task lists other than the mcp__team__ tools.`

const howAddAgents = `You can grow your team with mcp__team__add_agent: only when no current member fits part of the work, ` +
	`or when you have independent sub-tasks to run in parallel. Check your team first. A new agent starts working when you ` +
	`give it a task with mcp__team__create_task. When an agent you added has no more work, remove it with mcp__team__remove_agent.`

const howSubLead = `When you receive a task from your boss, break it into subtasks for your team and stop without calling report_done. ` +
	`When the task comes back to you with all subtasks done, check the result and call mcp__team__report_done ` +
	`with the summary.`

const howWorker = `Work only on the task you received and do not change anything outside it. ` +
	`When you finish, call mcp__team__report_done with the requested summary. ` +
	`If something is unclear or blocked and only the user can resolve it, use mcp__team__ask_user; ` +
	`if it is a problem for your lead, call mcp__team__report_done explaining it. Do not guess.`

// howWorkerAuto e howWorker fara ask_user (modul full autonomy).
const howWorkerAuto = `Work only on the task you received and do not change anything outside it. ` +
	`When you finish, call mcp__team__report_done with the requested summary. ` +
	`If something is a problem for your lead, call mcp__team__report_done explaining it.`

const howReviewer = `You check the work of the agents listed under "You review the work of" (in the turn message). When you receive a task to check, ` +
	`read the result and the changed files, then call mcp__team__review_approve if it is good, or ` +
	`mcp__team__review_reject with concrete feedback: what is wrong and what needs to change. ` +
	`Read the changed files to check the work; do not rely only on the summary.`

const howSolo = `You talk directly with the user and have no team. Reply to their messages and do the requested work.`

// System compileaza system prompt-ul agentului. Contine doar date stabile:
// Claude Code il inregistreaza la prima cerere a sesiunii si il retrimite
// neschimbat la --resume. Starea care se schimba (echipa, task-uri) merge in
// mesajul turei.
func System(p store.Project, a store.Agent, l Links) string {
	var b strings.Builder
	isRoot := l.BossName == ""

	role := a.Role
	if role == "" {
		role = "agent"
	}
	reports := "You report to the user."
	if !isRoot {
		reports = fmt.Sprintf("You report to %s.", l.BossName)
	}
	section(&b, "Role", fmt.Sprintf("You are %s, %s, in the project %s. %s", a.Name, role, p.Name, reports))
	section(&b, "Background", a.Background)
	section(&b, "Instructions", a.Instructions)

	done := strings.TrimSpace(a.DoneDef)
	if isRoot {
		text := "When the goal is reached, write the user a summary."
		if done != "" {
			text += " The summary includes: " + done
		}
		section(&b, "When you finish", text)
	} else {
		text := "Call mcp__team__report_done with a summary of the work."
		if done != "" {
			text = "Call mcp__team__report_done with: " + done
		}
		section(&b, "When you finish a task", text)
	}

	var proj []string
	if g := strings.TrimSpace(p.Goal); g != "" {
		proj = append(proj, "Goal: "+g)
	}
	// cu mai multe foldere, agentul afla unde lucreaza si ce poate doar citi
	if len(p.Folders) > 1 {
		home := store.HomeFolder(a, p.Folders)
		lines := []string{"Folders:"}
		for _, f := range p.Folders {
			access := "read only"
			if f.ID == home.ID {
				access = "your folder"
			}
			lines = append(lines, fmt.Sprintf("- %s: %s (%s)", f.Name, f.Path, access))
		}
		proj = append(proj, strings.Join(lines, "\n"))
	}
	if c := strings.TrimSpace(p.Context); c != "" {
		proj = append(proj, "Shared context:\n"+c)
	}
	for _, f := range p.Folders {
		if c := strings.TrimSpace(f.Context); c != "" {
			proj = append(proj, "Context for "+f.Name+":\n"+c)
		}
	}
	section(&b, "Project", strings.Join(proj, "\n\n"))

	// "Cum lucrezi" se compune din blocurile care se aplica agentului
	var how []string
	if l.IsLead {
		how = append(how, howLead)
		if !isRoot {
			how = append(how, howSubLead)
		}
	} else if !isRoot {
		if p.Autonomy {
			how = append(how, howWorkerAuto)
		} else {
			how = append(how, howWorker)
		}
	}
	if l.CanAddAgents {
		how = append(how, howAddAgents)
	}
	if l.IsReviewer {
		how = append(how, howReviewer)
	}
	if isRoot && !l.IsLead && !l.IsReviewer {
		how = append(how, howSolo)
	}
	switch {
	case p.Autonomy:
		// agentii nu pun intrebari: decid singuri si isi spun presupunerile
		how = append(how, howAuto)
	case isRoot:
		how = append(how, howAsk)
	default:
		how = append(how, howAsk, howAskWait)
	}
	section(&b, "How you work", strings.Join(how, "\n\n"))
	return strings.TrimSpace(b.String())
}

// section scrie "# Titlu\ncontinut"; sectiunile goale se omit.
func section(b *strings.Builder, title, body string) {
	body = strings.TrimSpace(body)
	if body == "" {
		return
	}
	fmt.Fprintf(b, "# %s\n%s\n\n", title, body)
}

// Hash identifica un system prompt; daca se schimba, pornim o sesiune noua.
func Hash(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// Team formateaza echipa directa a unui lider pentru mesajul turei.
func Team(members []Member) string {
	if len(members) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("Your direct team:")
	for _, m := range members {
		b.WriteString("\n- " + m.Name)
		var parts []string
		for _, p := range []string{m.Role, m.Status} {
			if p != "" {
				parts = append(parts, p)
			}
		}
		if m.Folder != "" {
			parts = append(parts, "folder: "+m.Folder)
		}
		if len(parts) > 0 {
			b.WriteString(": " + strings.Join(parts, ", "))
		}
		if m.Responsibilities != "" {
			b.WriteString("\n  Takes: " + m.Responsibilities)
		}
	}
	return b.String()
}

// Context aduna partea din mesajul turei care depinde de locul agentului in echipa.
func Context(c TurnContext) string {
	var parts []string
	if c.Stage != "" {
		parts = append(parts, c.Stage)
	}
	if t := Team(c.Direct); t != "" {
		parts = append(parts, t)
	}
	if c.Stages != "" {
		parts = append(parts, c.Stages)
	}
	if len(c.Reviewees) > 0 {
		parts = append(parts, "You review the work of: "+strings.Join(c.Reviewees, ", "))
	}
	if len(c.OpenTasks) > 0 {
		var b strings.Builder
		b.WriteString("Your open tasks:")
		for _, o := range c.OpenTasks {
			progress := "no subtasks yet"
			if o.Total > 0 {
				progress = fmt.Sprintf("subtasks done: %d of %d", o.Done, o.Total)
			}
			fmt.Fprintf(&b, "\n- %s: %s (%s)", o.Ref, o.Title, progress)
		}
		parts = append(parts, b.String())
	}
	return strings.Join(parts, "\n\n")
}

// TaskTurn e mesajul cu care porneste un agent pe un task.
func TaskTurn(c TurnContext, t store.Task, subtasks, notes []string) string {
	var parts []string
	if ctx := Context(c); ctx != "" {
		parts = append(parts, ctx)
	}
	task := fmt.Sprintf("Task %s: %s", t.Ref(), t.Title)
	if d := strings.TrimSpace(t.Description); d != "" {
		task += "\n" + d
	}
	parts = append(parts, task)
	if len(subtasks) > 0 {
		parts = append(parts, bullets("Subtasks already created for this task (do not create them again):", subtasks))
	}
	if f := strings.TrimSpace(t.Feedback); f != "" {
		parts = append(parts, "Feedback on the previous attempt:\n"+f)
	}
	if len(notes) > 0 {
		parts = append(parts, bullets("Notifications:", notes))
	}
	return strings.Join(parts, "\n\n")
}

// NotifyTurn trezeste un agent fara task, cu tot ce s-a strans de la ultimul lui run.
func NotifyTurn(c TurnContext, notes []string) string {
	parts := []string{}
	if ctx := Context(c); ctx != "" {
		parts = append(parts, ctx)
	}
	parts = append(parts, bullets("Notifications:", notes))
	return strings.Join(parts, "\n\n")
}

func bullets(title string, items []string) string {
	var b strings.Builder
	b.WriteString(title)
	for _, it := range items {
		b.WriteString("\n- " + strings.TrimSpace(it))
	}
	return b.String()
}
