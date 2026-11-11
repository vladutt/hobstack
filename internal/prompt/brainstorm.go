package prompt

import (
	"fmt"
	"strings"
)

// BrainstormIdea e o idee (a unui agent) pusa in tura de brainstorming.
type BrainstormIdea struct {
	Author, Kind, Title, Text, Basis string
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
	fmt.Fprintf(&b, "This is a brainstorming session on the topic: %s\n", c.Topic)
	b.WriteString("It is not work: do not modify any files and do not start tasks. You may read the code and the documents.\n")
	if d := strings.TrimSpace(c.Description); d != "" {
		fmt.Fprintf(&b, "\nTopic description:\n%s\n", d)
	}
	if len(c.Files) > 0 {
		b.WriteString("\nAttachments (read them, including images):")
		for i, f := range c.Files {
			if i == maxFiles {
				fmt.Fprintf(&b, "\n- ... and %d more files", len(c.Files)-maxFiles)
				break
			}
			fmt.Fprintf(&b, "\n- %s (%s)", f.Path, f.Name)
		}
		b.WriteString("\n")
	}
	if len(c.Picked) > 0 {
		b.WriteString("\nIdeas the user picked:\n")
		for _, i := range c.Picked {
			writeIdea(&b, i, true)
		}
	}
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

func writeIdea(b *strings.Builder, i BrainstormIdea, withAuthor bool) {
	head := i.Title
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
