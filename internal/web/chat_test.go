package web

import (
	"encoding/json"
	"net/http"
	"testing"

	"hobstack/internal/store"
)

func TestNewChatAndChatView(t *testing.T) {
	e := setup(t)
	p := e.createProject(t, t.TempDir())
	w := e.addWorker(t, p.ID, "Rook")
	base := "/api/agents/" + itoa(w.ID)
	e.do(t, "POST", base+"/messages", map[string]any{"text": "vechi"})
	// mesajul inca nelivrat ramane vizibil; il marcam livrat ca sa fie "vechi"
	all, _ := e.st.ListMessages(w.ID)
	e.st.MarkDelivered([]int64{all[0].ID})

	code, raw := e.do(t, "POST", base+"/new-chat", nil)
	if code != http.StatusCreated {
		t.Fatalf("new-chat: %d %s", code, raw)
	}
	e.do(t, "POST", base+"/messages", map[string]any{"text": "nou"})

	var view struct {
		Messages []store.Message `json:"messages"`
		Earlier  int             `json:"earlier"`
	}
	_, raw = e.do(t, "GET", base+"/messages?chat=1", nil)
	if err := json.Unmarshal(raw, &view); err != nil {
		t.Fatalf("%v %s", err, raw)
	}
	if view.Earlier != 1 || len(view.Messages) != 2 || view.Messages[0].Text != "New chat started." || view.Messages[1].Text != "nou" {
		t.Fatalf("chat: %s", raw)
	}
	// fara ?chat=1: tot istoricul, ca inainte
	var msgs []store.Message
	_, raw = e.do(t, "GET", base+"/messages", nil)
	json.Unmarshal(raw, &msgs)
	if len(msgs) != 3 {
		t.Fatalf("istoric complet: %s", raw)
	}
	if code, _ := e.do(t, "POST", "/api/agents/999999/new-chat", nil); code != http.StatusNotFound {
		t.Fatalf("agent inexistent: %d", code)
	}
}
