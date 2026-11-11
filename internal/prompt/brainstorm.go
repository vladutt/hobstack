package prompt

import (
	"fmt"
	"strings"
)

// BrainstormIdea e o idee (a unui agent) pusa in tura de brainstorming.
type BrainstormIdea struct {
	Author, Kind, Title, Text, Basis string
	// CombinedFrom e "A + B" (titlurile surselor) pentru o idee nascuta din combinare.
	CombinedFrom string
}

// BrainstormQA e o intrebare a unui agent cu raspunsul utilizatorului.
type BrainstormQA struct {
	Author, Question, Answer string
}

// BrainstormContext e tot ce primeste agentul intr-o tura de brainstorming.
type BrainstormContext struct {
	Topic, Description, Message string
	Files                       []FileRef
	Picked                      []BrainstormIdea
	Unpicked                    []string
	Own                         []BrainstormIdea
	QA                          []BrainstormQA
	QuestionsLeft               int
	Another                     bool
}

// BrainstormTurn scrie tura unui agent intr-o sesiune de brainstorming.
func BrainstormTurn(c BrainstormContext) string {
	var b strings.Builder
	writeBrainstormBase(&b, c.Topic, c.Description, c.Files, c.Picked)
	if len(c.Unpicked) > 0 {
		b.WriteString("\nAlready proposed, not picked: do not repeat them:\n")
		for _, t := range c.Unpicked {
			fmt.Fprintf(&b, "- %s\n", t)
		}
	}
	if len(c.Own) > 0 {
		b.WriteString("\nWhat you proposed yourself so far:\n")
		for _, i := range c.Own {
			writeIdea(&b, i, false)
		}
	}
	if len(c.QA) > 0 {
		b.WriteString("\nQuestions and answers so far:\n")
		for _, q := range c.QA {
			if q.Author != "" {
				fmt.Fprintf(&b, "- %s asked: %s\n  Answer: %s\n", q.Author, q.Question, q.Answer)
			} else {
				fmt.Fprintf(&b, "- Question: %s\n  Answer: %s\n", q.Question, q.Answer)
			}
		}
	}
	if m := strings.TrimSpace(c.Message); m != "" {
		fmt.Fprintf(&b, "\nCurrent message from the user:\n%s\n", m)
	}
	b.WriteString("\nRules:\n")
	b.WriteString("- Start from the project and the attachments.\n")
	b.WriteString("- Answer from your own role.\n")
	b.WriteString("- Say in basis what you rely on (files, attachments, what you found in the project).\n")
	b.WriteString("- Ask a question only if the answer would change your idea.\n")
	b.WriteString("- Use kind wild only for truly bold ideas.\n")
	if c.QuestionsLeft <= 0 {
		b.WriteString("- Do not ask more questions; propose with what you know.\n")
	}
	if c.Another {
		b.WriteString("- Give a different idea from the ones you already gave.\n")
	}
	b.WriteString("- Reply with exactly one brainstorm_reply call.")
	return b.String()
}

// writeBrainstormBase scrie blocurile comune: subiectul, descrierea, atasamentele si ideile alese.
func writeBrainstormBase(b *strings.Builder, topic, description string, files []FileRef, picked []BrainstormIdea) {
	fmt.Fprintf(b, "This is a brainstorming session on the topic: %s\n", topic)
	b.WriteString("It is not work: do not modify any files and do not start tasks. You may read the code and the documents.\n")
	if d := strings.TrimSpace(description); d != "" {
		fmt.Fprintf(b, "\nTopic description:\n%s\n", d)
	}
	if len(files) > 0 {
		b.WriteString("\nAttachments (read them, including images):")
		for i, f := range files {
			if i == maxFiles {
				fmt.Fprintf(b, "\n- ... and %d more files", len(files)-maxFiles)
				break
			}
			fmt.Fprintf(b, "\n- %s (%s)", f.Path, f.Name)
		}
		b.WriteString("\n")
	}
	if len(picked) > 0 {
		b.WriteString("\nIdeas the user picked:\n")
		for _, i := range picked {
			writeIdea(b, i, true)
		}
	}
}

// BrainstormCombine e tura de combinare a doua idei intr-una singura.
type BrainstormCombine struct {
	Topic, Description string
	Files              []FileRef
	Picked             []BrainstormIdea
	A, B               BrainstormIdea
}

// BrainstormCombineTurn scrie tura prin care un agent combina doua idei.
func BrainstormCombineTurn(c BrainstormCombine) string {
	var b strings.Builder
	writeBrainstormBase(&b, c.Topic, c.Description, c.Files, c.Picked)
	b.WriteString("\nCombine these two ideas into one:\n")
	writeIdea(&b, c.A, true)
	writeIdea(&b, c.B, true)
	b.WriteString("\nRules:\n")
	b.WriteString("- Write one coherent idea that keeps the best of both and resolves where they conflict; it is not the two texts one after the other.\n")
	b.WriteString("- Say briefly in the text what you left out and why.\n")
	b.WriteString("- Say in basis what you rely on.\n")
	b.WriteString("- Use kind wild only if the combined idea is truly bold.\n")
	b.WriteString("- Do not ask questions.\n")
	b.WriteString("- Reply with exactly one brainstorm_reply call.")
	return b.String()
}

func writeIdea(b *strings.Builder, i BrainstormIdea, withAuthor bool) {
	head := i.Title
	if i.Kind == "wild" { // ideile nebune se vad ca atare
		head += " (wild)"
	}
	if i.CombinedFrom != "" {
		head += " (combined from " + i.CombinedFrom + ")"
	}
	if withAuthor && i.Author != "" {
		head += " (" + i.Author + ")"
	}
	fmt.Fprintf(b, "- %s\n", head)
	if i.Text != "" {
		fmt.Fprintf(b, "  %s\n", i.Text)
	}
	if i.Basis != "" {
		fmt.Fprintf(b, "  Basis: %s\n", i.Basis)
	}
}
