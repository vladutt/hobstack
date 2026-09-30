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
	BossName   string // "" = raporteaza utilizatorului
	IsLead     bool
	IsReviewer bool
}

// LinksFor calculeaza Links din lista echipei. Merge si pentru un agent nou
// (ID 0): atunci doar BossName poate fi setat.
func LinksFor(a store.Agent, team []store.Agent) Links {
	r := store.RolesOf(a, team)
	l := Links{IsLead: r.IsLead, IsReviewer: r.IsReviewer}
	if a.ParentID != nil {
		if boss, ok := store.FindAgent(team, *a.ParentID); ok {
			l.BossName = boss.Name
		}
	}
	return l
}

// Member e un rand din "Echipa ta directa".
type Member struct {
	Name, Role, Responsibilities, Status string
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

const howLead = `Esti lider: ai o echipa directa, listata in mesajul turei. Sparge munca in task-uri mici, clare si verificabile ` +
	`si creeaza-le cu mcp__team__create_task, doar pentru agentii din echipa ta directa, alegand dupa campul "Ce ia" al fiecaruia. ` +
	`Nu face tu munca lor. Cand primesti rezultate, verifica-le (poti citi fisierele modificate) si apeleaza ` +
	`mcp__team__accept_task sau mcp__team__send_back cu feedback concret. Foloseste mcp__team__list_tasks ca sa vezi starea. ` +
	`Nu folosi alte liste de task-uri in afara de tool-urile mcp__team__.`

const howSubLead = `Cand primesti un task de la seful tau, sparge-l in sub-task-uri pentru echipa ta si opreste-te fara report_done. ` +
	`Cand task-ul revine la tine cu toate sub-task-urile gata, verifica rezultatul si apeleaza mcp__team__report_done ` +
	`cu rezumatul.`

const howWorker = `Lucreaza doar pe task-ul primit si nu modifica lucruri din afara lui. ` +
	`Cand termini, apeleaza mcp__team__report_done cu rezumatul cerut. ` +
	`Daca ceva e neclar sau blocat, apeleaza mcp__team__report_done explicand problema, in loc sa ghicesti.`

const howReviewer = `Verifici munca agentilor din "Revizuiesti munca lui" (din mesajul turei). Cand primesti un task de verificat, ` +
	`citeste rezultatul si fisierele schimbate, apoi apeleaza mcp__team__review_approve daca e bun, sau ` +
	`mcp__team__review_reject cu feedback concret: ce e gresit si ce trebuie schimbat. ` +
	`Citeste fisierele modificate ca sa verifici munca, nu te baza doar pe rezumat.`

const howSolo = `Vorbesti direct cu utilizatorul si nu ai echipa. Raspunde la mesajele lui si fa munca ceruta.`

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
	reports := "Raportezi utilizatorului."
	if !isRoot {
		reports = fmt.Sprintf("Raportezi lui %s.", l.BossName)
	}
	section(&b, "Rol", fmt.Sprintf("Esti %s, %s, in proiectul %s. %s", a.Name, role, p.Name, reports))
	section(&b, "Background", a.Background)
	section(&b, "Instructiuni", a.Instructions)

	done := strings.TrimSpace(a.DoneDef)
	if isRoot {
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

	// "Cum lucrezi" se compune din blocurile care se aplica agentului
	var how []string
	if l.IsLead {
		how = append(how, howLead)
		if !isRoot {
			how = append(how, howSubLead)
		}
	} else if !isRoot {
		how = append(how, howWorker)
	}
	if l.IsReviewer {
		how = append(how, howReviewer)
	}
	if isRoot && !l.IsLead && !l.IsReviewer {
		how = append(how, howSolo)
	}
	section(&b, "Cum lucrezi", strings.Join(how, "\n\n"))
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
	b.WriteString("Echipa ta directa:")
	for _, m := range members {
		b.WriteString("\n- " + m.Name)
		var parts []string
		for _, p := range []string{m.Role, m.Status} {
			if p != "" {
				parts = append(parts, p)
			}
		}
		if len(parts) > 0 {
			b.WriteString(": " + strings.Join(parts, ", "))
		}
		if m.Responsibilities != "" {
			b.WriteString("\n  Ce ia: " + m.Responsibilities)
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
		parts = append(parts, "Revizuiesti munca lui: "+strings.Join(c.Reviewees, ", "))
	}
	if len(c.OpenTasks) > 0 {
		var b strings.Builder
		b.WriteString("Task-urile tale deschise:")
		for _, o := range c.OpenTasks {
			progress := "fara sub-task-uri inca"
			if o.Total > 0 {
				progress = fmt.Sprintf("sub-task-uri gata: %d din %d", o.Done, o.Total)
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
		parts = append(parts, bullets("Sub-task-uri deja create pentru acest task (nu le crea din nou):", subtasks))
	}
	if f := strings.TrimSpace(t.Feedback); f != "" {
		parts = append(parts, "Feedback la incercarea anterioara:\n"+f)
	}
	if len(notes) > 0 {
		parts = append(parts, bullets("Notificari:", notes))
	}
	return strings.Join(parts, "\n\n")
}

// NotifyTurn trezeste un agent fara task, cu tot ce s-a strans de la ultimul lui run.
func NotifyTurn(c TurnContext, notes []string) string {
	parts := []string{}
	if ctx := Context(c); ctx != "" {
		parts = append(parts, ctx)
	}
	parts = append(parts, bullets("Notificari:", notes))
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
