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
}
