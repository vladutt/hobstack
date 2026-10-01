package team

import (
	"log"

	"foreman/internal/store"
)

// maxEventText: lungimea maxima (in caractere) a textului unui eveniment.
const maxEventText = 200

// taskEvent adauga un pas in istoricul task-ului. Nu emite nimic separat (evenimentul
// "task" existent reincarca UI-ul); o eroare se logheaza si nu opreste fluxul.
func (t *Team) taskEvent(task store.Task, kind, actor, text string) {
	if r := []rune(text); len(r) > maxEventText {
		text = string(r[:maxEventText]) + "..."
	}
	e := store.TaskEvent{TaskID: task.ID, At: t.now(), Kind: kind, Actor: actor, Text: text}
	if err := t.st.AddTaskEvent(&e); err != nil {
		log.Printf("task %d: event: %v", task.ID, err)
	}
}

// maxEventBody: lungimea maxima (in caractere) a corpului unei comunicari; taierea e aici, nu in store.
const maxEventBody = 20000

// taskMsg adauga in istoric o comunicare (task, raport, feedback) cu destinatar si text complet.
func (t *Team) taskMsg(task store.Task, kind, from, to, text, body string) {
	if r := []rune(text); len(r) > maxEventText {
		text = string(r[:maxEventText]) + "..."
	}
	if r := []rune(body); len(r) > maxEventBody {
		body = string(r[:maxEventBody]) + " [...]"
	}
	e := store.TaskEvent{TaskID: task.ID, At: t.now(), Kind: kind, Actor: from, To: to, Text: text, Body: body}
	if err := t.st.AddTaskEvent(&e); err != nil {
		log.Printf("task %d: event: %v", task.ID, err)
	}
}

// Actorii evenimentelor care nu sunt agenti.
const (
	actorUser    = "You"     // actiune din board
	actorForeman = "Foreman" // automat (detector, blocare, scheduler)
)

// taskNote e un eveniment pe care il inregistreaza un helper comun (toReview,
// rejectTask, advance) imediat dupa salvare, inaintea evenimentelor lui proprii.
type taskNote struct{ kind, actor, text, to, body string }

// record inregistreaza n; un taskNote gol nu inregistreaza nimic.
func (t *Team) record(task store.Task, n taskNote) {
	if n.kind == "" {
		return
	}
	if n.body != "" {
		t.taskMsg(task, n.kind, n.actor, n.to, n.text, n.body)
		return
	}
	t.taskEvent(task, n.kind, n.actor, n.text)
}
