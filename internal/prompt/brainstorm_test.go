package prompt

import (
	"strings"
	"testing"
)

func TestBrainstormTurn(t *testing.T) {
	c := BrainstormContext{
		Topic: "Faster checkout", Description: "Make checkout quicker", Message: "Think about mobile",
		Files:    []FileRef{{Name: "mock.png", Path: "/tmp/p/mock.png"}},
		Picked:   []BrainstormIdea{{Author: "Ada", Kind: "idea", Title: "One-tap pay", Text: "Use wallets", Basis: "cart.go"}},
		QA:       []BrainstormQA{{Author: "Ada", Question: "Which market?", Answer: "EU"}},
		Unpicked: []string{"Loyalty points"}, Own: []BrainstormIdea{{Title: "Saved carts"}},
		QuestionsLeft: 0,
	}
	got := BrainstormTurn(c)
	for _, want := range []string{"Faster checkout", "Make checkout quicker", "Think about mobile", "/tmp/p/mock.png",
		"One-tap pay", "brainstorm_reply", "Do not ask more questions", "Loyalty points", "Saved carts", "Which market?", "EU"} {
		if !strings.Contains(got, want) {
			t.Errorf("lipseste %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "different idea") {
		t.Error("fara Another nu trebuia 'different idea'")
	}
	c.QuestionsLeft = 2
	c.Another = true
	got = BrainstormTurn(c)
	if strings.Contains(got, "Do not ask more questions") {
		t.Error("cu intrebari ramase nu trebuia interdictia")
	}
	if !strings.Contains(got, "different idea") {
		t.Error("lipseste 'different idea'")
	}
	// ideile nebune sunt marcate, cele obisnuite nu
	c.Picked = append(c.Picked, BrainstormIdea{Author: "Rook", Kind: "wild", Title: "Pay by voice"})
	got = BrainstormTurn(c)
	if !strings.Contains(got, "- Pay by voice (wild) (Rook)") {
		t.Errorf("ideea nebuna nu e marcata:\n%s", got)
	}
	if strings.Contains(got, "One-tap pay (wild)") {
		t.Error("o idee obisnuita marcata wild")
	}
}

func TestCombineTurn(t *testing.T) {
	got := BrainstormCombineTurn(BrainstormCombine{
		Topic: "Launch plan", Description: "How we launch",
		A: BrainstormIdea{Author: "Ana", Title: "Host", Text: "Self host it", Basis: "docs/a.md"},
		B: BrainstormIdea{Author: "Dan", Title: "Async", Text: "Queue everything", Basis: "worker.go"},
	})
	for _, w := range []string{"Launch plan", "How we launch", "Host", "(Ana)", "Self host it", "Basis: docs/a.md",
		"Async", "(Dan)", "Queue everything", "Basis: worker.go", "one coherent idea", "what you left out",
		"Do not ask questions", "exactly one brainstorm_reply call"} {
		if !strings.Contains(got, w) {
			t.Errorf("lipseste %q in:\n%s", w, got)
		}
	}
	for _, w := range []string{"Current message from the user", "different idea"} {
		if strings.Contains(got, w) {
			t.Errorf("nu trebuie sa contina %q", w)
		}
	}
}

func TestWriteIdeaCombinedFrom(t *testing.T) {
	got := BrainstormTurn(BrainstormContext{Topic: "T", Picked: []BrainstormIdea{
		{Author: "Dan", Title: "Title", CombinedFrom: "Host + Async"}}})
	if !strings.Contains(got, "- Title (combined from Host + Async) (Dan)") {
		t.Errorf("eticheta lipseste:\n%s", got)
	}
}
