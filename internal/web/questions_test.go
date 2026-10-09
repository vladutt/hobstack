package web

import (
	"encoding/json"
	"testing"

	"hobstack/internal/mcp"
)

func TestQuestionsAPI(t *testing.T) {
	e := setup(t)
	p := e.createProject(t, t.TempDir())
	lead := e.root(t, p.ID)
	e.tm.AskUser(mcp.Caller{ProjectID: p.ID, AgentID: lead.ID, RunID: "r"}, "Which DB?", []string{"Postgres", "MySQL"})

	_, raw := e.do(t, "GET", "/api/projects/"+itoa(p.ID), nil)
	var d struct {
		Questions []struct {
			ID      int64
			Text    string
			Options []string
		} `json:"questions"`
	}
	json.Unmarshal(raw, &d)
	if len(d.Questions) != 1 || d.Questions[0].Options[0] != "Postgres" {
		t.Fatalf("%s", raw)
	}
	_, raw = e.do(t, "GET", "/api/projects", nil)
	var cards []struct {
		OpenQuestions int `json:"openQuestions"`
	}
	json.Unmarshal(raw, &cards)
	if len(cards) != 1 || cards[0].OpenQuestions != 1 {
		t.Fatalf("%s", raw)
	}

	id := itoa(d.Questions[0].ID)
	if code, _ := e.do(t, "POST", "/api/questions/"+id+"/answer", map[string]any{"text": "  "}); code != 400 {
		t.Fatalf("raspuns gol: %d", code)
	}
	if code, _ := e.do(t, "POST", "/api/questions/"+id+"/answer", map[string]any{"text": "Postgres"}); code != 204 {
		t.Fatalf("answer: %d", code)
	}
	if code, _ := e.do(t, "POST", "/api/questions/"+id+"/answer", map[string]any{"text": "MySQL"}); code != 409 {
		t.Fatalf("a doua oara: %d", code)
	}
	if code, _ := e.do(t, "POST", "/api/questions/"+id+"/dismiss", nil); code != 409 {
		t.Fatalf("dismiss dupa raspuns: %d", code)
	}
	if code, _ := e.do(t, "POST", "/api/questions/99999/dismiss", nil); code != 404 {
		t.Fatalf("inexistenta: %d", code)
	}
}
