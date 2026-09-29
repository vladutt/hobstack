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

// Member e un rand din lista echipei trimisa in mesajul turei.
type Member struct {
	Name     string
	Role     string
	Position string // store.PositionLead sau store.PositionWorker
	Status   string // "Working", "Idle", "Queued"; gol = nu se afiseaza
}

const howLead = `Esti coordonatorul echipei. Analizeaza obiectivul si citeste codul daca ai nevoie de context. ` +
	`Sparge munca in task-uri mici, clare si verificabile si creeaza-le cu mcp__team__create_task, ` +
	`fiecare asignat unui worker potrivit rolului. Nu face tu munca workerilor. ` +
	`Cand primesti rezultate, verifica-le (poti citi fisierele modificate) si apeleaza ` +
	`mcp__team__accept_task sau mcp__team__send_back cu feedback concret. ` +
	`Foloseste mcp__team__list_tasks ca sa vezi starea. Nu folosi alte liste de task-uri ` +
	`in afara de tool-urile mcp__team__. Cand obiectivul e atins, scrie utilizatorului un rezumat.`

const howWorker = `Lucreaza doar pe task-ul primit si nu modifica lucruri din afara lui. ` +
	`Cand termini, apeleaza mcp__team__report_done cu rezumatul cerut. ` +
	`Daca ceva e neclar sau blocat, apeleaza mcp__team__report_done explicand problema, in loc sa ghicesti.`

// System compileaza system prompt-ul agentului. Contine doar date stabile:
// Claude Code il inregistreaza la prima cerere a sesiunii si il retrimite
// neschimbat la --resume. Starea care se schimba (echipa, task-uri) merge in
// mesajul turei.
func System(p store.Project, a store.Agent, leadName string) string {
	var b strings.Builder
	isLead := a.Position == store.PositionLead

	role := a.Role
	if role == "" {
		role = a.Position
	}
	reports := fmt.Sprintf("Raportezi lui %s (lead).", leadName)
	if isLead {
		reports = "Raportezi utilizatorului."
	}
	section(&b, "Rol", fmt.Sprintf("Esti %s, %s, %s in proiectul %s. %s", a.Name, role, a.Position, p.Name, reports))
	section(&b, "Background", a.Background)
	section(&b, "Instructiuni", a.Instructions)

	done := strings.TrimSpace(a.DoneDef)
	if isLead {
		text := "Cand obiectivul e atins, scrie utilizatorului un rezumat."
		if done != "" {
			text += " Rezumatul contine: " + done
		}
		section(&b, "Cand termini", text)
	} else {
		text := "Apeleaza mcp__team__report_done cu un rezumat al muncii."
		if done != "" {
			text = "Apeleaza mcp__team__report_done cu: " + done
		}
		section(&b, "Cand termini un task", text)
	}

	var proj []string
	if g := strings.TrimSpace(p.Goal); g != "" {
		proj = append(proj, "Obiectiv: "+g)
	}
	if c := strings.TrimSpace(p.Context); c != "" {
		proj = append(proj, "Context comun:\n"+c)
	}
	section(&b, "Proiect", strings.Join(proj, "\n\n"))

	if isLead {
		section(&b, "Cum lucrezi", howLead)
	} else {
		section(&b, "Cum lucrezi", howWorker)
	}
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

// Team formateaza lista echipei pentru mesajul turei.
func Team(members []Member) string {
	var b strings.Builder
	b.WriteString("Echipa:")
	for _, m := range members {
		fmt.Fprintf(&b, "\n- %s: %s", m.Name, m.Position)
		if m.Role != "" {
			b.WriteString(", " + m.Role)
		}
		if m.Status != "" {
			b.WriteString(", " + m.Status)
		}
	}
	return b.String()
}

// WorkerTurn e mesajul cu care porneste un worker pe un task.
func WorkerTurn(members []Member, t store.Task, leadName string, userMsgs []string) string {
	task := fmt.Sprintf("Task %s: %s", t.Ref(), t.Title)
	if d := strings.TrimSpace(t.Description); d != "" {
		task += "\n" + d
	}
	parts := []string{Team(members), task}
	if f := strings.TrimSpace(t.Feedback); f != "" {
		parts = append(parts, fmt.Sprintf("Feedback de la %s la incercarea anterioara:\n%s", leadName, f))
	}
	if len(userMsgs) > 0 {
		parts = append(parts, bullets("Mesaje noi de la utilizator:", userMsgs))
	}
	return strings.Join(parts, "\n\n")
}

// DirectTurn e mesajul pentru un worker fara task, cand utilizatorul ii scrie direct.
func DirectTurn(userMsgs []string) string {
	return bullets("Mesaje de la utilizator (nu ai un task activ, raspunde direct):", userMsgs)
}

// LeadTurn trezeste lead-ul cu tot ce s-a strans de la ultimul lui run.
func LeadTurn(members []Member, notes []string) string {
	return Team(members) + "\n\n" + bullets("Notificari:", notes)
}

func bullets(title string, items []string) string {
	var b strings.Builder
	b.WriteString(title)
	for _, it := range items {
		b.WriteString("\n- " + strings.TrimSpace(it))
	}
	return b.String()
}
