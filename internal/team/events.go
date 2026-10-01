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
